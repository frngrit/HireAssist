package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

const guidePath = "/job-openings/jo-1/candidates/cand-001/interview-guide"

type guideJSON struct {
	ID           string `json:"id"`
	JobOpeningID string `json:"job_opening_id"`
	CandidateID  string `json:"candidate_id"`
	Emphasis     string `json:"emphasis"`
	Source       string `json:"source"`
	Revision     int    `json:"revision"`
	Attribution  *struct {
		Provider string `json:"provider"`
	} `json:"attribution"`
	Questions []struct {
		Text      string `json:"text"`
		Criterion string `json:"criterion"`
		Evidence  string `json:"evidence"`
		Kind      string `json:"kind"`
	} `json:"questions"`
}

// withOpening returns a server that already holds jo-1, which is what UC-3 starts from.
func withOpening(t *testing.T) http.Handler {
	t.Helper()
	h := newServer()
	do(t, h, http.MethodPost, "/job-openings", validBody)
	return h
}

func TestGenerateInterviewGuide(t *testing.T) {
	h := withOpening(t)

	rec := do(t, h, http.MethodPost, guidePath, `{"emphasis": "more system design"}`)

	wantStatus(t, rec, http.StatusCreated)
	got := decode[guideJSON](t, rec)
	if got.ID != "ig-1" || got.CandidateID != "cand-001" || got.Revision != 1 || got.Source != "generated" {
		t.Errorf("body = %+v", got)
	}
	if got.Emphasis != "more system design" {
		t.Errorf("emphasis = %q", got.Emphasis)
	}
	// FR-3.3: every question says what it tests and where it came from.
	if len(got.Questions) == 0 || got.Questions[0].Criterion == "" || got.Questions[0].Kind == "" {
		t.Errorf("questions = %+v", got.Questions)
	}
	if got.Attribution == nil || got.Attribution.Provider == "" {
		t.Error("a generated guide should say which model wrote it (ADR-004)")
	}
}

func TestGenerateInterviewGuide_NoBodyIsFine(t *testing.T) {
	h := withOpening(t)

	rec := do(t, h, http.MethodPost, guidePath, "")

	wantStatus(t, rec, http.StatusCreated)
}

func TestGenerateInterviewGuide_UnknownOpeningIs404(t *testing.T) {
	rec := do(t, withOpening(t), http.MethodPost, "/job-openings/jo-404/candidates/cand-001/interview-guide", "")

	wantStatus(t, rec, http.StatusNotFound)
}

func TestGenerateInterviewGuide_SecondTimeIs409(t *testing.T) {
	h := withOpening(t)
	do(t, h, http.MethodPost, guidePath, "")

	rec := do(t, h, http.MethodPost, guidePath, "")

	wantStatus(t, rec, http.StatusConflict)
}

func TestGetInterviewGuide(t *testing.T) {
	h := withOpening(t)
	do(t, h, http.MethodPost, guidePath, "")

	rec := do(t, h, http.MethodGet, guidePath, "")

	wantStatus(t, rec, http.StatusOK)
	if got := decode[guideJSON](t, rec); got.JobOpeningID != "jo-1" {
		t.Errorf("body = %+v", got)
	}
}

func TestGetInterviewGuide_NoneYetIs404(t *testing.T) {
	rec := do(t, withOpening(t), http.MethodGet, guidePath, "")

	wantStatus(t, rec, http.StatusNotFound)
}

func TestRegenerateInterviewGuide(t *testing.T) {
	h := withOpening(t)
	do(t, h, http.MethodPost, guidePath, "")

	rec := do(t, h, http.MethodPost, guidePath+"/regenerate", `{"emphasis": "more behavioural"}`)

	wantStatus(t, rec, http.StatusOK)
	got := decode[guideJSON](t, rec)
	if got.Revision != 2 || got.Emphasis != "more behavioural" || got.Source != "generated" {
		t.Errorf("body = %+v", got)
	}
}

func TestReviseGuideQuestions(t *testing.T) {
	h := withOpening(t)
	do(t, h, http.MethodPost, guidePath, "")

	rec := do(t, h, http.MethodPut, guidePath+"/questions",
		`[{"text": "Walk me through the outage.", "criterion": "Kubernetes", "kind": "resume-claim"}]`)

	wantStatus(t, rec, http.StatusOK)
	got := decode[guideJSON](t, rec)
	if got.Source != "edited" || got.Revision != 2 || len(got.Questions) != 1 {
		t.Errorf("body = %+v", got)
	}
	if got.Attribution != nil {
		t.Error("an edited guide still claims a model wrote it")
	}
}

func TestReviseGuideQuestions_EmptySetIs422(t *testing.T) {
	h := withOpening(t)
	do(t, h, http.MethodPost, guidePath, "")

	rec := do(t, h, http.MethodPut, guidePath+"/questions", `[]`)

	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if e := decode[map[string]string](t, rec)["error"]; !strings.Contains(e, "no questions") {
		t.Errorf("error = %q, want it to say why", e)
	}
}

// A service UC-3 reads being down is 502, not 500 and not an empty guide: it is somebody
// else's failure, and the Recruiter can try again.
func TestGenerateInterviewGuide_UpstreamFailureIs502(t *testing.T) {
	h := newServerWithGenerator(failingGenerator{})
	do(t, h, http.MethodPost, "/job-openings", validBody)

	rec := do(t, h, http.MethodPost, guidePath, "")

	wantStatus(t, rec, http.StatusBadGateway)
}

type failingGenerator struct{}

func (failingGenerator) GenerateInterviewQuestions(context.Context, domain.CandidateProfile, []domain.Criterion, string) ([]domain.Question, domain.ModelAttribution, error) {
	return nil, domain.ModelAttribution{}, errors.New("the AI service is down")
}

// The candidate profile read is the other half, and it fails the same way.
func TestGenerateInterviewGuide_UnknownCandidateIs404(t *testing.T) {
	h := newServerWithProfiles(missingProfiles{})
	do(t, h, http.MethodPost, "/job-openings", validBody)

	rec := do(t, h, http.MethodPost, guidePath, "")

	wantStatus(t, rec, http.StatusNotFound)
}

type missingProfiles struct{}

func (missingProfiles) GetCandidateProfile(context.Context, string) (domain.CandidateProfile, error) {
	return domain.CandidateProfile{}, usecase.ErrCandidateNotFound
}
