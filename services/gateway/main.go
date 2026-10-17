// Command gateway runs the HireAssist API Gateway: the single entry point for user traffic
// (ADR-001). It owns no domain data. It verifies the access token, enforces the role a route
// requires, rate-limits, and forwards the request to the service that owns it.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// config holds everything the service reads from environment variables.
type config struct {
	port        string
	identityURL string
	hiringURL   string
	// publicKeys is the ES256 verification material, PEM-encoded, as described in auth.go.
	// The gateway verifies every access token itself and never calls the Identity Service to
	// check one (ADR-009).
	publicKeys string
	rateLimit  float64 // requests per second allowed to one caller
	rateBurst  int     // how many may arrive at once
	// cookieSecure marks the refresh cookie Secure. On by default; off only for plain-HTTP
	// local runs, where a browser may refuse a Secure cookie.
	cookieSecure bool
}

func loadConfig() config {
	return config{
		port:         envOr("PORT", "8000"),
		identityURL:  envOr("IDENTITY_URL", "http://localhost:8081"),
		hiringURL:    envOr("HIRING_URL", "http://localhost:8080"),
		publicKeys:   os.Getenv("JWT_PUBLIC_KEYS"),
		rateLimit:    envFloat("RATE_LIMIT_PER_SECOND", 20),
		rateBurst:    envInt("RATE_LIMIT_BURST", 40),
		cookieSecure: os.Getenv("SESSION_COOKIE_SECURE") != "false",
	}
}

func main() {
	// One JSON object per log line (ADR-005 rule 3).
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

// run builds the handler and blocks until Ctrl+C or SIGTERM, then shuts down gracefully.
func run() error {
	cfg := loadConfig()

	identity, err := url.Parse(cfg.identityURL)
	if err != nil {
		return fmt.Errorf("bad IDENTITY_URL: %w", err)
	}
	hiring, err := url.Parse(cfg.hiringURL)
	if err != nil {
		return fmt.Errorf("bad HIRING_URL: %w", err)
	}

	// An empty key set is allowed so the gateway can be run before the Identity Service can
	// sign anything. It then rejects every protected route rather than letting one through,
	// and says so loudly at startup.
	keys, err := parsePublicKeys(cfg.publicKeys)
	if err != nil {
		return fmt.Errorf("bad JWT_PUBLIC_KEYS: %w", err)
	}
	if len(keys) == 0 {
		slog.Warn("no JWT_PUBLIC_KEYS configured; every protected route will answer 401")
	}

	gw := &gateway{
		routes:   routeTable(identity, hiring),
		verifier: &verifier{keys: keys},
		limiter:  newRateLimiter(cfg.rateLimit, cfg.rateBurst),
		cookies:  sessionCookies{secure: cfg.cookieSecure},
	}

	server := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           logRequests(gw.handler()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Screening batches are uploads, and an interview guide waits on a model call, so the
		// write timeout is well above the per-request budget the other services work to.
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("gateway listening",
			"port", cfg.port, "identity", cfg.identityURL, "hiring", cfg.hiringURL,
			"verification_keys", len(keys))
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("start server: %w", err)
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down server: %w", err)
	}
	slog.Info("stopped")
	return nil
}
