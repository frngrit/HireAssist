package rabbitmq_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/rabbitmq"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

var _ usecase.ScreeningRequester = (*rabbitmq.Broker)(nil)

// recordedResults collects what the consumer hands to the use case.
type recordedResults struct {
	mu   sync.Mutex
	got  []string
	seen chan string
}

func (r *recordedResults) RecordScreeningResult(_ context.Context, batchID, resumeID string, status domain.EntryStatus, _ domain.ScreeningResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if resumeID == "rs-unknown" {
		return usecase.ErrResumeNotFound
	}
	r.got = append(r.got, batchID+"/"+resumeID+"/"+string(status))
	r.seen <- resumeID
	return nil
}

// brokerURL names the RabbitMQ the test runs against; without it the test is skipped, so
// `go test ./...` needs no broker. `docker compose up -d rabbitmq` provides one.
func brokerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HIRING_TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("HIRING_TEST_RABBITMQ_URL not set; skipping RabbitMQ integration test")
	}
	return url
}

func channel(t *testing.T, url string) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// isolated returns queue names no running service uses, and deletes them after the test.
func isolated(t *testing.T, ch *amqp.Channel) rabbitmq.Topology {
	t.Helper()
	prefix := fmt.Sprintf("test-%d", time.Now().UnixNano())
	topo := rabbitmq.Topology{
		RequestedQueue:  prefix + ".screening.requested",
		CompletedQueue:  prefix + ".screening.completed",
		DeadLetterQueue: prefix + ".screening.completed.dlq",
	}
	t.Cleanup(func() {
		for _, q := range []string{topo.RequestedQueue, topo.CompletedQueue, topo.DeadLetterQueue} {
			_, _ = ch.QueueDelete(q, false, false, false)
		}
	})
	return topo
}

// waitConnected retries RequestScreening until the broker's first connection is up.
func waitConnected(t *testing.T, b *rabbitmq.Broker, batch *domain.ScreeningBatch) error {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := b.RequestScreening(context.Background(), batch, &batch.Entries[0])
		if !errors.Is(err, rabbitmq.ErrUnavailable) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The handover end to end through a real broker, on the contract's terms: a request waits in
// its queue even with nobody consuming, persistent and correlated; a result comes back to the
// use case; a result this service can never accept is kept in its dead-letter queue.
func TestBroker(t *testing.T) {
	url := brokerURL(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := channel(t, url)
	topo := isolated(t, ch)
	results := &recordedResults{seen: make(chan string, 4)}
	b := rabbitmq.NewWithTopology(url, "resumes", topo)
	go b.Run(ctx, results)

	// 1. The request is confirmed and waits in its queue, though nothing consumes it yet.
	if err := waitConnected(t, b, batch()); err != nil {
		t.Fatalf("requesting: %v", err)
	}
	msg, ok, err := ch.Get(topo.RequestedQueue, true)
	if err != nil || !ok {
		t.Fatalf("no request in %s: ok = %v, err = %v", topo.RequestedQueue, ok, err)
	}
	var req rabbitmq.Requested
	if err := json.Unmarshal(msg.Body, &req); err != nil || req.ResumeID != "rs-1" || req.File.Key != "jo-1/sb-1/rs-1.pdf" {
		t.Errorf("request = %+v, err %v", req, err)
	}
	if msg.DeliveryMode != amqp.Persistent || msg.ContentType != "application/json" ||
		req.CorrelationID == "" || msg.Headers["x-correlation-id"] != req.CorrelationID {
		t.Errorf("delivery mode %d, content type %q, correlation id %q in the body and %v in the header",
			msg.DeliveryMode, msg.ContentType, req.CorrelationID, msg.Headers["x-correlation-id"])
	}

	// 2. A result comes back to the use case.
	publish(t, ch, topo.CompletedQueue, `{"batch_id":"sb-1","resume_id":"rs-1","status":"unparsable","score":null,"error":"scanned"}`)
	select {
	case <-results.seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the result never reached the use case")
	}

	// 3. Results that can never be accepted are kept in the dead-letter queue, with the reason.
	publish(t, ch, topo.CompletedQueue, `not json`)
	publish(t, ch, topo.CompletedQueue, `{"batch_id":"sb-1","resume_id":"rs-unknown","status":"failed"}`)
	if n := waitForMessages(t, ch, topo.DeadLetterQueue, 2); n != 2 {
		t.Fatalf("dead-letter queue holds %d messages, want 2", n)
	}
	refused, _, _ := ch.Get(topo.DeadLetterQueue, true)
	if refused.Headers["x-refused-because"] == nil {
		t.Errorf("a refused result carries no reason: %v", refused.Headers)
	}
}

func publish(t *testing.T, ch *amqp.Channel, queue, body string) {
	t.Helper()
	if err := ch.PublishWithContext(context.Background(), "", queue, false, false,
		amqp.Publishing{ContentType: "application/json", Body: []byte(body)}); err != nil {
		t.Fatal(err)
	}
}

func waitForMessages(t *testing.T, ch *amqp.Channel, queue string, want int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		q, err := ch.QueueDeclarePassive(queue, true, false, false, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if q.Messages >= want || time.Now().After(deadline) {
			return q.Messages
		}
		time.Sleep(100 * time.Millisecond)
	}
}
