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
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, closeService, err := app.New(ctx, cfg)
	if err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer closeService()
	if cfg.DatabaseURL == "" {
		slog.Warn("DATABASE_URL not set: storing job openings in memory, lost on restart")
	}
	if cfg.MongoURL == "" {
		slog.Warn("MONGO_URL not set: storing interview guides in memory, lost on restart")
	}
	if cfg.ResumeProcessingURL == "" || cfg.AIServiceAddress == "" {
		slog.Warn("UC-3 cannot generate a guide until both services it reads are configured",
			"resume_processing_url", cfg.ResumeProcessingURL, "ai_service_address", cfg.AIServiceAddress)
	}

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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
