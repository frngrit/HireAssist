// Command fake-screener stands in for the Resume Processing Service when it is not running: it
// takes every screening.requested message and answers with a screening.completed one, on the
// terms of services/resume-processing/MESSAGES.md, so UC-2 can be run end to end on its own.
// Its answers are invented — derived from a hash of the resume id, never from a model — and it
// is for development only.
//
//	go run ./cmd/fake-screener
//
// It reads from the same shared queue the real service does, so never run the two together:
// they would take turns with the requests. A resume whose file contains the word "scan" is
// answered unparsable, to show how a resume that cannot be read is kept out of the ranking
// (FR-2.7). FAKE_SCREENER_DELAY (default 2s) spaces the answers out, so the ranking can be seen
// filling in.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/rabbitmq"
)

// name identifies the fake to the broker: its connection, its consumer, its messages.
const name = "fake-screener"

func main() {
	delay, err := time.ParseDuration(envOr("FAKE_SCREENER_DELAY", "2s"))
	if err != nil {
		slog.Error("FAKE_SCREENER_DELAY is not a duration", "err", err)
		os.Exit(1)
	}
	files, err := minio.New(envOr("OBJECT_STORE_ENDPOINT", "localhost:9000"), &minio.Options{
		Creds: credentials.NewStaticV4(envOr("OBJECT_STORE_ACCESS_KEY", "hireassist"), envOr("OBJECT_STORE_SECRET_KEY", "hireassist-local"), ""),
	})
	if err != nil {
		slog.Error("object storage settings", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, envOr("RABBITMQ_URL", "amqp://hireassist:hireassist@localhost:5672/"), files, delay); err != nil {
		slog.Error("fake screener stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, url string, files *minio.Client, delay time.Duration) error {
	conn, err := amqp.DialConfig(url, amqp.Config{Properties: amqp.Table{"connection_name": name}})
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	topo := rabbitmq.DefaultTopology
	if err := rabbitmq.DeclareQueues(ch, topo); err != nil {
		return err
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return err
	}
	requests, err := ch.ConsumeWithContext(ctx, topo.RequestedQueue, name, false, false, false, false, nil)
	if err != nil {
		return err
	}
	slog.Info("fake screener ready: answers are invented, nothing is screened", "queue", topo.RequestedQueue)

	for d := range requests {
		var req rabbitmq.Requested
		if err := json.Unmarshal(d.Body, &req); err != nil {
			slog.Error("not a screening request; dropped", "err", err)
			_ = d.Nack(false, false)
			continue
		}
		select { // look busy, so the ranking visibly fills in
		case <-ctx.Done():
			return nil
		case <-time.After(delay/2 + rand.N(delay+1)):
		}
		answer := invent(req, looksScanned(ctx, files, req.File))
		body, err := json.Marshal(answer)
		if err != nil {
			return err
		}
		if err := ch.PublishWithContext(ctx, "", topo.CompletedQueue, false, false, amqp.Publishing{
			ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: req.ResumeID,
			Headers: amqp.Table{"x-correlation-id": req.CorrelationID}, AppId: name, Timestamp: time.Now().UTC(), Body: body,
		}); err != nil {
			return err
		}
		_ = d.Ack(false)
		slog.Info("answered", "resume", req.ResumeID, "status", answer.Status, "correlation_id", req.CorrelationID)
	}
	return nil
}

// looksScanned stands in for finding no text layer: the demo's "scanned" resumes say so.
func looksScanned(ctx context.Context, files *minio.Client, f rabbitmq.File) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	obj, err := files.GetObject(ctx, f.Bucket, f.Key, minio.GetObjectOptions{})
	if err != nil {
		return false
	}
	defer obj.Close()
	content, err := io.ReadAll(io.LimitReader(obj, 1<<20))
	return err == nil && bytes.Contains(bytes.ToLower(content), []byte("scan"))
}

// invent makes up an answer for one request. The same resume always gets the same answer.
func invent(req rabbitmq.Requested, scanned bool) rabbitmq.Completed {
	answer := rabbitmq.Completed{BatchID: req.BatchID, ResumeID: req.ResumeID, JobOpeningID: req.JobOpeningID, CorrelationID: req.CorrelationID}
	if scanned {
		why := "the PDF has no readable text (invented by the fake screener)"
		answer.Status, answer.Error = "unparsable", &why
		return answer
	}
	totalWeight, weighted := 0, 0
	answer.UnmetMustHaveIDs = []string{}
	for _, c := range req.Criteria {
		h := hash(req.ResumeID + c.ID)
		met := h%100 < 70
		score := int(h % 40)
		if met {
			score = 60 + int(h%41)
		}
		if !met && c.MustHave {
			answer.UnmetMustHaveIDs = append(answer.UnmetMustHaveIDs, c.ID)
		}
		totalWeight += c.Weight
		weighted += c.Weight * score
	}
	score, meets := 0, len(answer.UnmetMustHaveIDs) == 0
	if totalWeight > 0 {
		score = weighted / totalWeight
	}
	justification := fmt.Sprintf("Invented by the fake screener: %d of %d must-haves met.", countMustHaves(req)-len(answer.UnmetMustHaveIDs), countMustHaves(req))
	answer.Status, answer.CandidateID = "scored", fmt.Sprintf("cand-%03d", hash(req.ResumeID)%1000)
	answer.Score, answer.MeetsMustHaves, answer.Justification = &score, &meets, &justification
	return answer
}

func countMustHaves(req rabbitmq.Requested) int {
	n := 0
	for _, c := range req.Criteria {
		if c.MustHave {
			n++
		}
	}
	return n
}

func hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
