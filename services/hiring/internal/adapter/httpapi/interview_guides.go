// The REST endpoints of UC-3: generating, reading and revising a candidate's interview guide.
// They sit under the job opening because that is what the guide is for — one candidate against
// one opening — and it keeps every hiring path behind the gateway's one `/job-openings` route.
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

// InterviewGuides is the use case port for UC-3.
type InterviewGuides interface {
	GenerateInterviewGuide(ctx context.Context, jobOpeningID, candidateID, emphasis string) (*domain.InterviewGuide, error)
	GetInterviewGuide(ctx context.Context, jobOpeningID, candidateID string) (*domain.InterviewGuide, error)
	RegenerateInterviewGuide(ctx context.Context, jobOpeningID, candidateID, emphasis string) (*domain.InterviewGuide, error)
	ReviseGuideQuestions(ctx context.Context, jobOpeningID, candidateID string, questions []domain.Question) (*domain.InterviewGuide, error)
}

// ---- wire format ----

type questionJSON struct {
	Text      string `json:"text"`
	Criterion string `json:"criterion"`
	Evidence  string `json:"evidence,omitempty"`
	Kind      string `json:"kind"`
}

type attributionJSON struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
}

type guideResponse struct {
	ID           string           `json:"id"`
	JobOpeningID string           `json:"job_opening_id"`
	CandidateID  string           `json:"candidate_id"`
	Questions    []questionJSON   `json:"questions"`
	Emphasis     string           `json:"emphasis,omitempty"`
	Source       string           `json:"source"`
	Attribution  *attributionJSON `json:"attribution"` // null once a Recruiter has edited the guide
	Revision     int              `json:"revision"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// emphasisRequest is the optional steer for a generation (FR-3.4).
type emphasisRequest struct {
	Emphasis string `json:"emphasis"`
}

func toQuestions(in []questionJSON) []domain.Question {
	out := make([]domain.Question, len(in))
	for i, q := range in {
		out[i] = domain.Question{
			Text:      q.Text,
			Criterion: q.Criterion,
			Evidence:  q.Evidence,
			Kind:      domain.QuestionKind(q.Kind),
		}
	}
	return out
}

func toGuideResponse(g domain.InterviewGuide) guideResponse {
	questions := make([]questionJSON, len(g.Questions))
	for i, q := range g.Questions {
		questions[i] = questionJSON{Text: q.Text, Criterion: q.Criterion, Evidence: q.Evidence, Kind: string(q.Kind)}
	}
	var attribution *attributionJSON
	if g.Attribution != (domain.ModelAttribution{}) {
		attribution = &attributionJSON{
			Provider:      g.Attribution.Provider,
			Model:         g.Attribution.Model,
			PromptVersion: g.Attribution.PromptVersion,
		}
	}
	return guideResponse{
		ID:           g.ID,
		JobOpeningID: g.JobOpeningID,
		CandidateID:  g.CandidateID,
		Questions:    questions,
		Emphasis:     g.Emphasis,
		Source:       string(g.Source),
		Attribution:  attribution,
		Revision:     g.Revision,
		CreatedAt:    g.CreatedAt.UTC(),
		UpdatedAt:    g.UpdatedAt.UTC(),
	}
}

// ---- endpoints ----

// generateGuide is `generateInterviewGuide()`: the first guide for this candidate (FR-3.1).
// A second call does not quietly replace it — regenerating is its own endpoint, so a stray
// click cannot throw away what the Recruiter has edited.
func (h handler) generateGuide(w http.ResponseWriter, r *http.Request) {
	emphasis, ok := readEmphasis(w, r)
	if !ok {
		return
	}
	guide, err := h.guides.GenerateInterviewGuide(r.Context(), r.PathValue("id"), r.PathValue("candidateId"), emphasis)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toGuideResponse(*guide))
}

// getGuide is `getInterviewGuide()`: the retained guide, reopened before the interview.
func (h handler) getGuide(w http.ResponseWriter, r *http.Request) {
	respondGuide(w)(h.guides.GetInterviewGuide(r.Context(), r.PathValue("id"), r.PathValue("candidateId")))
}

// regenerateGuide is `reviseGuide()` asking the model again, optionally with an emphasis such
// as "more system design" (UC-3 alternate flow 5a).
func (h handler) regenerateGuide(w http.ResponseWriter, r *http.Request) {
	emphasis, ok := readEmphasis(w, r)
	if !ok {
		return
	}
	respondGuide(w)(h.guides.RegenerateInterviewGuide(r.Context(), r.PathValue("id"), r.PathValue("candidateId"), emphasis))
}

// reviseGuideQuestions is `reviseGuide()` with the Recruiter's own questions: the whole set is
// sent, so adding, rewording, reordering and removing are all the same request (FR-3.4).
func (h handler) reviseGuideQuestions(w http.ResponseWriter, r *http.Request) {
	var req []questionJSON
	if !decodeBody(w, r, &req) {
		return
	}
	respondGuide(w)(h.guides.ReviseGuideQuestions(r.Context(), r.PathValue("id"), r.PathValue("candidateId"), toQuestions(req)))
}

// readEmphasis reads the optional body of a generate or regenerate request. No body at all is
// the common case — generate without a steer — so it is not an error.
func readEmphasis(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.ContentLength == 0 {
		return "", true
	}
	var req emphasisRequest
	if !decodeBody(w, r, &req) {
		return "", false
	}
	return req.Emphasis, true
}

// respondGuide writes a use case's guide result, or its error.
func respondGuide(w http.ResponseWriter) func(*domain.InterviewGuide, error) {
	return func(g *domain.InterviewGuide, err error) {
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toGuideResponse(*g))
	}
}
