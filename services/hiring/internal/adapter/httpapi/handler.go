// Package httpapi is the REST adapter for the Hiring Service's job opening operations.
// It translates HTTP to use case calls and business errors to status codes; it holds no rules.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// JobOpenings is the use case port the handler drives.
type JobOpenings interface {
	CreateJobOpening(ctx context.Context, draft domain.JobOpeningDraft) (*domain.JobOpening, error)
	GetJobOpening(ctx context.Context, id string) (*domain.JobOpening, error)
	ListJobOpenings(ctx context.Context) ([]domain.JobOpening, error)
	ReviseScreeningCriteria(ctx context.Context, id string, criteria []domain.Criterion) (*domain.JobOpening, error)
	PauseOpening(ctx context.Context, id string) (*domain.JobOpening, error)
	ResumeOpening(ctx context.Context, id string) (*domain.JobOpening, error)
	CloseOpening(ctx context.Context, id string, outcome domain.Outcome) (*domain.JobOpening, error)
}

const (
	day          = 24 * time.Hour
	maxBodyBytes = 1 << 20
)

type handler struct {
	svc    JobOpenings
	guides InterviewGuides
}

// NewHandler routes the endpoints to the injected use cases.
func NewHandler(svc JobOpenings, guides InterviewGuides) http.Handler {
	h := handler{svc: svc, guides: guides}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /job-openings", h.create)
	mux.HandleFunc("GET /job-openings", h.list)
	mux.HandleFunc("GET /job-openings/{id}", h.get)
	mux.HandleFunc("PUT /job-openings/{id}/criteria", h.reviseCriteria)
	mux.HandleFunc("POST /job-openings/{id}/pause", h.pause)
	mux.HandleFunc("POST /job-openings/{id}/resume", h.resume)
	mux.HandleFunc("POST /job-openings/{id}/close", h.close)

	// UC-3, under the opening the guide is for.
	const guidePath = "/job-openings/{id}/candidates/{candidateId}/interview-guide"
	mux.HandleFunc("POST "+guidePath, h.generateGuide)                    // generateInterviewGuide()
	mux.HandleFunc("GET "+guidePath, h.getGuide)                          // getInterviewGuide()
	mux.HandleFunc("POST "+guidePath+"/regenerate", h.regenerateGuide)    // reviseGuide(), asking again
	mux.HandleFunc("PUT "+guidePath+"/questions", h.reviseGuideQuestions) // reviseGuide(), the Recruiter's own
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /openapi.yaml", serveSpec)
	mux.HandleFunc("GET /docs", serveSwaggerUI)
	return mux
}

// ---- wire format ----

type criterionJSON struct {
	Name           string `json:"name"`
	Classification string `json:"classification"`
	Weight         int    `json:"weight"`
}

type createRequest struct {
	Title                  string          `json:"title"`
	Description            string          `json:"description"`
	Criteria               []criterionJSON `json:"criteria"`
	ExpectedTimeToFillDays int             `json:"expected_time_to_fill_days"`
}

type closeRequest struct {
	Outcome string `json:"outcome"`
}

type openingResponse struct {
	ID                     string          `json:"id"`
	Title                  string          `json:"title"`
	Description            string          `json:"description"`
	Criteria               []criterionJSON `json:"criteria"`
	ExpectedTimeToFillDays int             `json:"expected_time_to_fill_days"`
	Status                 string          `json:"status"`
	Outcome                string          `json:"outcome,omitempty"`
	OpenedAt               time.Time       `json:"opened_at"`
}

func toCriteria(in []criterionJSON) []domain.Criterion {
	out := make([]domain.Criterion, len(in))
	for i, c := range in {
		out[i] = domain.Criterion{Name: c.Name, Classification: domain.Classification(c.Classification), Weight: c.Weight}
	}
	return out
}

func toResponse(o domain.JobOpening) openingResponse {
	criteria := make([]criterionJSON, len(o.Criteria))
	for i, c := range o.Criteria {
		criteria[i] = criterionJSON{Name: c.Name, Classification: string(c.Classification), Weight: c.Weight}
	}
	return openingResponse{
		ID:                     o.ID,
		Title:                  o.Title,
		Description:            o.Description,
		Criteria:               criteria,
		ExpectedTimeToFillDays: int(o.ExpectedTimeToFill / day),
		Status:                 string(o.Status),
		Outcome:                string(o.Outcome),
		OpenedAt:               o.OpenedAt.UTC(),
	}
}

// ---- endpoints ----

func (h handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decodeBody(w, r, &req) {
		return
	}
	created, err := h.svc.CreateJobOpening(r.Context(), domain.JobOpeningDraft{
		Title:              req.Title,
		Description:        req.Description,
		Criteria:           toCriteria(req.Criteria),
		ExpectedTimeToFill: time.Duration(req.ExpectedTimeToFillDays) * day,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/job-openings/"+created.ID)
	writeJSON(w, http.StatusCreated, toResponse(*created))
}

func (h handler) list(w http.ResponseWriter, r *http.Request) {
	openings, err := h.svc.ListJobOpenings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]openingResponse, len(openings))
	for i, o := range openings {
		out[i] = toResponse(o)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h handler) get(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK)(h.svc.GetJobOpening(r.Context(), r.PathValue("id")))
}

func (h handler) reviseCriteria(w http.ResponseWriter, r *http.Request) {
	var req []criterionJSON
	if !decodeBody(w, r, &req) {
		return
	}
	respond(w, http.StatusOK)(h.svc.ReviseScreeningCriteria(r.Context(), r.PathValue("id"), toCriteria(req)))
}

func (h handler) pause(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK)(h.svc.PauseOpening(r.Context(), r.PathValue("id")))
}

func (h handler) resume(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK)(h.svc.ResumeOpening(r.Context(), r.PathValue("id")))
}

func (h handler) close(w http.ResponseWriter, r *http.Request) {
	var req closeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	respond(w, http.StatusOK)(h.svc.CloseOpening(r.Context(), r.PathValue("id"), domain.Outcome(req.Outcome)))
}

// ---- helpers ----

// respond writes a use case's single-opening result, or its error.
func respond(w http.ResponseWriter, status int) func(*domain.JobOpening, error) {
	return func(o *domain.JobOpening, err error) {
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, status, toResponse(*o))
	}
}

// decodeBody reads a JSON body strictly; on failure it writes 400 and returns false.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request body: " + err.Error()})
		return false
	}
	return true
}

// writeError maps business errors to status codes. Anything unexpected is logged and
// reported as 500 without detail, so infrastructure internals never reach the browser.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, usecase.ErrNotFound),
		errors.Is(err, usecase.ErrGuideNotFound),
		errors.Is(err, usecase.ErrCandidateNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrInvalidJobOpening), errors.Is(err, domain.ErrInvalidInterviewGuide):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrInvalidTransition), errors.Is(err, usecase.ErrGuideExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, usecase.ErrUpstreamUnavailable):
		// The Resume Processing Service or the AI Service could not answer. It is their
		// failure, not a bad request, and an empty guide would hide it.
		slog.Error("UC-3 upstream failed", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		slog.Error("unexpected error", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
