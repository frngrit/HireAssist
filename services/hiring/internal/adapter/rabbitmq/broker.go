// Package rabbitmq is the message broker adapter. It carries the screening handover between the
// Hiring Service and the Resume Processing Service: Hiring publishes one screening.requested
// message per resume and consumes the screening.completed messages that come back. The contract
// — queues, bodies, headers — is services/resume-processing/MESSAGES.md.
package rabbitmq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

const (
	// correlationHeader carries the id that follows one request through both services' logs.
	correlationHeader = "x-correlation-id"

	prefetch       = 10
	confirmTimeout = 10 * time.Second
	maxBackoff     = 30 * time.Second
)

// Topology names the queues a Broker uses. Messages go through the default exchange, routed by
// queue name. DefaultTopology is the contract; a test uses names of its own, so its traffic
// never meets a running service's.
type Topology struct {
	// RequestedQueue and CompletedQueue are shared with Resume Processing: durable, with no
	// arguments, so either side may declare them.
	RequestedQueue string
	CompletedQueue string
	// DeadLetterQueue is Hiring's own: the results it refuses are kept there for a person to look
	// at, instead of being retried forever or dropped.
	DeadLetterQueue string
}

// DefaultTopology is the topology of MESSAGES.md.
var DefaultTopology = Topology{
	RequestedQueue:  "screening.requested",
	CompletedQueue:  "screening.completed",
	DeadLetterQueue: "screening.completed.dlq",
}

var (
	// ErrUnavailable means the broker could not take a request now. The request waits for the
	// relay's next pass.
	ErrUnavailable = errors.New("message broker unavailable")
	// ErrNoQueue means the broker had no queue to put the request in and handed it back. It is
	// not sent; it waits for the relay like any other.
	ErrNoQueue = errors.New("no queue takes screening requests")
)

// Results is the use case consumed results are handed to.
type Results interface {
	RecordScreeningResult(ctx context.Context, batchID, resumeID string, status domain.EntryStatus, r domain.ScreeningResult) error
}

// Broker keeps one connection to RabbitMQ, publishing screening requests and consuming
// screening results. It reconnects by itself; while it is disconnected, publishing fails at once
// with ErrUnavailable, so a request never waits on a broker that is down.
type Broker struct {
	url    string
	bucket string // where the resume files are, named in every request
	topo   Topology

	mu      sync.Mutex // one publish at a time; guards the two fields below
	pub     *amqp.Channel
	returns chan amqp.Return
}

// New returns a broker on DefaultTopology. It is not connected yet; Run connects it.
func New(url, bucket string) *Broker {
	return NewWithTopology(url, bucket, DefaultTopology)
}

// NewWithTopology returns a broker on the given queues.
func NewWithTopology(url, bucket string, topo Topology) *Broker {
	return &Broker{url: url, bucket: bucket, topo: topo}
}

// Run keeps a connection until ctx ends, reconnecting with backoff when it drops, and hands
// every screening result to results.
func (b *Broker) Run(ctx context.Context, results Results) {
	backoff := time.Second
	for {
		started := time.Now()
		err := b.session(ctx, results)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second // the connection had been up; start over
		}
		slog.Warn("RabbitMQ unavailable; reconnecting", "err", err, "in", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, maxBackoff)
	}
}

// session connects, declares the queues, and consumes until the connection drops.
func (b *Broker) session(ctx context.Context, results Results) error {
	conn, err := amqp.DialConfig(b.url, amqp.Config{
		Heartbeat:  10 * time.Second,
		Properties: amqp.Table{"connection_name": "hiring"},
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	consumer, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := DeclareQueues(consumer, b.topo); err != nil {
		return err
	}
	if err := consumer.Qos(prefetch, 0, false); err != nil {
		return err
	}
	deliveries, err := consumer.ConsumeWithContext(ctx, b.topo.CompletedQueue, "hiring", false, false, false, false, nil)
	if err != nil {
		return err
	}

	publisher, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := publisher.Confirm(false); err != nil {
		return err
	}
	b.setPublisher(publisher, publisher.NotifyReturn(make(chan amqp.Return, 16)))
	defer b.setPublisher(nil, nil)

	slog.Info("connected to RabbitMQ", "requests", b.topo.RequestedQueue, "results", b.topo.CompletedQueue)
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-closed:
			if err == nil {
				return errors.New("connection closed")
			}
			return err
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("the broker stopped delivering results")
			}
			b.handle(ctx, consumer, results, d)
		}
	}
}

func (b *Broker) setPublisher(ch *amqp.Channel, returns chan amqp.Return) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pub, b.returns = ch, returns
}

// DeclareQueues declares the queues of a topology: durable, with no arguments, as the contract
// requires of the shared two. Declaring is idempotent, so whichever service starts first creates
// them, and a request published before Resume Processing starts waits in its queue.
func DeclareQueues(ch *amqp.Channel, topo Topology) error {
	for _, q := range []string{topo.RequestedQueue, topo.CompletedQueue, topo.DeadLetterQueue} {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declaring queue %s: %w", q, err)
		}
	}
	return nil
}

// RequestScreening publishes the request to screen one resume and returns once the broker has
// confirmed it is stored (usecase.ScreeningRequester). The message is persistent, carries a
// correlation id in its body and its x-correlation-id header (ADR-005), and has the resume id
// as its message id; (batch_id, resume_id) is how Resume Processing recognises a repeat. It is
// published as mandatory: should no queue take it, the broker hands it back instead of dropping
// it, and this returns ErrNoQueue.
func (b *Broker) RequestScreening(ctx context.Context, batch *domain.ScreeningBatch, e *domain.ScreeningEntry) error {
	correlationID := newCorrelationID()
	body, err := json.Marshal(NewRequested(b.bucket, batch, e, correlationID))
	if err != nil {
		return fmt.Errorf("encoding the request for %s: %w", e.ResumeID, err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pub == nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()
	confirmation, err := b.pub.PublishWithDeferredConfirmWithContext(ctx, "", b.topo.RequestedQueue, true, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     e.ResumeID,
		CorrelationId: correlationID,
		Headers:       amqp.Table{correlationHeader: correlationID},
		AppId:         "hiring",
		Timestamp:     time.Now().UTC(),
		Body:          body,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	// The broker hands back an unroutable message before it confirms it, so any return is
	// already waiting by the time the confirmation arrives.
	if b.returned(e.ResumeID) {
		return ErrNoQueue
	}
	if !acked {
		return fmt.Errorf("%w: the broker refused the message", ErrUnavailable)
	}
	return nil
}

// returned drains the handed-back messages and reports whether one was this message.
func (b *Broker) returned(messageID string) bool {
	found := false
	for {
		select {
		case r := <-b.returns:
			found = found || r.MessageId == messageID
		default:
			return found
		}
	}
}

// handle records one screening result. A result is acknowledged once recorded, or when it
// repeats one already recorded. A result that can never be accepted is moved to the dead-letter
// queue. Any other failure — the database, most likely — puts it back to be delivered again.
func (b *Broker) handle(ctx context.Context, ch *amqp.Channel, results Results, d amqp.Delivery) {
	var m Completed
	if err := json.Unmarshal(d.Body, &m); err != nil {
		b.deadLetter(ctx, ch, d, fmt.Errorf("%w: %w", ErrMalformed, err))
		return
	}
	status, result, err := m.ToDomain()
	if err != nil {
		b.deadLetter(ctx, ch, d, err)
		return
	}
	switch err := results.RecordScreeningResult(ctx, m.BatchID, m.ResumeID, status, result); {
	case err == nil:
		slog.Info("screening result recorded", "batch", m.BatchID, "resume", m.ResumeID,
			"status", m.Status, "correlation_id", m.CorrelationID)
		_ = d.Ack(false)
	case errors.Is(err, domain.ErrAlreadyCompleted):
		slog.Info("repeated screening result ignored", "resume", m.ResumeID, "correlation_id", m.CorrelationID)
		_ = d.Ack(false)
	case errors.Is(err, domain.ErrInvalidScreening), errors.Is(err, usecase.ErrResumeNotFound):
		b.deadLetter(ctx, ch, d, err)
	default:
		slog.Warn("screening result not recorded; it will be delivered again", "resume", m.ResumeID, "err", err)
		select { // a pause, so a failure that lasts does not spin
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
		_ = d.Nack(false, true)
	}
}

// deadLetter keeps a result this service refuses in its own dead-letter queue, with the reason
// in a header, and acknowledges the original. The shared queue takes no dead-letter arguments,
// so the move is made here rather than by the broker. Should the copy fail, the original goes
// back to be tried again rather than being lost.
func (b *Broker) deadLetter(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, reason error) {
	slog.Error("screening result refused; moved to the dead-letter queue",
		"err", reason, "queue", b.topo.DeadLetterQueue, "message_id", d.MessageId)
	if err := ch.PublishWithContext(ctx, "", b.topo.DeadLetterQueue, false, false, amqp.Publishing{
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    d.MessageId,
		Headers:      amqp.Table{"x-refused-because": reason.Error(), correlationHeader: d.Headers[correlationHeader]},
		Timestamp:    time.Now().UTC(),
		Body:         d.Body,
	}); err != nil {
		slog.Error("could not keep the refused result; it will be delivered again", "err", err)
		_ = d.Nack(false, true)
		return
	}
	_ = d.Ack(false)
}

// newCorrelationID makes the id that follows one request through both services' logs.
func newCorrelationID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return hex.EncodeToString(b)
}
