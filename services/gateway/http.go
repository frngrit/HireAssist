// HTTP and configuration helpers. The JSON error shape matches the Identity Service's, so a
// caller sees one error format whether the gateway answered or passed the request on.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

// writeJSON sends v as an indented JSON body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		slog.Error("encode response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(append(body, '\n'))
}

// errorResponse is the body of every error the gateway itself answers with.
type errorResponse struct {
	Error string `json:"error"`
}

// writeError sends {"error": msg}. A cause is logged but never sent: at the gateway it names
// an internal address or a token's innards, neither of which a caller should be told about.
func writeError(w http.ResponseWriter, status int, msg string, cause error) {
	if cause != nil {
		slog.Error(msg, "error", cause)
	}
	writeJSON(w, status, errorResponse{Error: msg})
}

// logRequests writes one JSON line per request: method, path, status and duration.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
		)
	})
}

// statusRecorder remembers the status code a handler writes.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// envOr returns the environment variable, or fallback when it is unset or empty.
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// envInt and envFloat fall back rather than failing: a mistyped limit should not stop the
// gateway starting, and the value it used is in the startup log.
func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil {
		return fallback
	}
	return value
}
