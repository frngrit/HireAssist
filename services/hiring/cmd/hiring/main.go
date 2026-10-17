// Command hiring runs the Hiring Service's HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/objectstore"
	"github.com/frngrit/HireAssist/services/hiring/internal/app"
)

func main() {
	// Configuration comes from the environment (ADR-005 rule 4).
	addr := ":" + envOr("PORT", "8080")
	cfg := app.Config{
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		MongoURL:            os.Getenv("MONGO_URL"),
		MongoDatabase:       envOr("MONGO_DATABASE", "hiring"),
		ResumeProcessingURL: os.Getenv("RESUME_PROCESSING_URL"),
		AIServiceAddress:    os.Getenv("AI_SERVICE_ADDRESS"),
		RabbitMQURL:         os.Getenv("RABBITMQ_URL"),
		ObjectStore: objectstore.Config{
			Endpoint:  os.Getenv("OBJECT_STORE_ENDPOINT"),
			AccessKey: os.Getenv("OBJECT_STORE_ACCESS_KEY"),
			SecretKey: os.Getenv("OBJECT_STORE_SECRET_KEY"),
			Bucket:    envOr("OBJECT_STORE_BUCKET", "resumes"),
			UseSSL:    os.Getenv("OBJECT_STORE_USE_SSL") == "true",
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, closeService, err := app.New(ctx, cfg)
	if err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer closeService()
	warnAboutWhatIsMissing(cfg)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("hiring service listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown failed", "err", err)
	}
}

// warnAboutWhatIsMissing says, once at startup, what the service does without each dependency
// that is not configured.
func warnAboutWhatIsMissing(cfg app.Config) {
	for _, d := range []struct {
		configured bool
		without    string
	}{
		{cfg.DatabaseURL != "", "DATABASE_URL not set: storing job openings and batches in memory, lost on restart"},
		{cfg.MongoURL != "", "MONGO_URL not set: storing interview guides in memory, lost on restart"},
		{cfg.ObjectStore.Endpoint != "", "OBJECT_STORE_ENDPOINT not set: keeping resume files in memory"},
		{cfg.RabbitMQURL != "", "RABBITMQ_URL not set: batches are accepted, but no resume is screened"},
		{cfg.AIServiceAddress != "", "AI_SERVICE_ADDRESS not set: proposing a job opening and generating an interview guide answer 502"},
		{cfg.ResumeProcessingURL != "", "RESUME_PROCESSING_URL not set: generating an interview guide answers 502, and a screened candidate opens without the justification"},
	} {
		if !d.configured {
			slog.Warn(d.without)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
