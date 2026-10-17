package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/api"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/httpapi"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/memory"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC) }

type fixedID struct{}

func (fixedID) NewID() string { return "jo-1" }

// stubProfiles stands in for the Resume Processing Service: UC-3 reads a profile it does not
// own, and these tests are about this service's half of the exchange.
type stubProfiles struct{}

func (stubProfiles) GetCandidateProfile(_ context.Context, candidateID string) (domain.CandidateProfile, error) {
	return domain.CandidateProfile{CandidateID: candidateID, Skills: []string{"Go", "Kubernetes"}}, nil
}

// stubJustifications stands in for the same service's justification of a score (UC-2).
type stubJustifications struct{}

func (stubJustifications) GetJustification(_ context.Context, candidateID, _, _ string) (domain.Justification, error) {
	return domain.Justification{
		Text:     "Six years of Go at two companies, for " + candidateID + ".",
		Evidence: []domain.CriterionEvidence{{Criterion: 0, Met: true, Score: 90, Evidence: "Go at Wongnai and LINE MAN"}},
	}, nil
}

// stubGenerator stands in for the AI Service's interview questions.
type stubGenerator struct{}

func (stubGenerator) GenerateInterviewQuestions(_ context.Context, _ domain.CandidateProfile, criteria []domain.Criterion, _ string) ([]domain.Question, domain.ModelAttribution, error) {
	return []domain.Question{{
			Text:      "You list Kubernetes — what did you actually operate?",
			Criterion: criteria[len(criteria)-1].Name,
			Evidence:  "Kubernetes on the resume",
			Kind:      domain.ResumeClaim,
		}},
		domain.ModelAttribution{Provider: "anthropic", Model: "a-model", PromptVersion: "uc3-1"},
		nil
}

// stubDeriver stands in for the AI Service's proposals: it proposes from any description, or
// fails with err.
type stubDeriver struct{ err error }

func (d stubDeriver) DeriveCriteria(context.Context, string, domain.Language) (*domain.JobOpeningProposal, error) {
	if d.err != nil {
		return nil, d.err
	}
	return &domain.JobOpeningProposal{
		Title: "Backend Engineer",
		Criteria: []domain.Criterion{
			{Name: "3+ years of Go or Python", Classification: domain.MustHave, Weight: 1},
			{Name: "Kafka or RabbitMQ in production", Classification: domain.NiceToHave, Weight: 3},
		},
		Uninterpreted: []string{"English good enough for a standup"},
		Attribution:   domain.ModelAttribution{Provider: "canned", Model: "canned-answers-no-model-was-called", PromptVersion: "v1"},
	}, nil
}

// acceptingBroker takes every screening request.
type acceptingBroker struct{}

func (acceptingBroker) RequestScreening(context.Context, *domain.ScreeningBatch, *domain.ScreeningEntry) error {
	return nil
}

type guideID struct{}

func (guideID) NewID() string { return "ig-1" }

type sequentialScreeningIDs struct{ batches, resumes int }

func (g *sequentialScreeningIDs) NewBatchID() string {
	g.batches++
	return fmt.Sprintf("sb-%d", g.batches)
}

func (g *sequentialScreeningIDs) NewResumeID() string {
	g.resumes++
	return fmt.Sprintf("rs-%d", g.resumes)
}

// serverOptions replaces one stand-in; the zero value uses them all.
type serverOptions struct {
	deriver   usecase.CriteriaDeriver   // default stubDeriver{}
	files     usecase.ResumeStore       // default in memory
	profiles  usecase.CandidateProfiles // default stubProfiles{}
	generator usecase.QuestionGenerator // default stubGenerator{}
}

// testServer is the handler over in-memory adapters, with the screening use case exposed so a
// test can play Resume Processing and record results.
type testServer struct {
	http.Handler
	screening *usecase.ScreeningService
}

func newTestServer(opts serverOptions) testServer {
	if opts.deriver == nil {
		opts.deriver = stubDeriver{}
	}
	if opts.files == nil {
		opts.files = memory.NewResumeStore()
	}
	if opts.profiles == nil {
		opts.profiles = stubProfiles{}
	}
	if opts.generator == nil {
		opts.generator = stubGenerator{}
	}
	openings := memory.NewJobOpeningRepository()
	screening := usecase.NewScreeningService(usecase.ScreeningDeps{
		Openings: openings, Batches: memory.NewScreeningRepository(), Files: opts.files,
		Requester: acceptingBroker{}, Justifications: stubJustifications{}, Clock: fixedClock{}, IDs: &sequentialScreeningIDs{},
	})
	guides := usecase.NewInterviewGuideService(
		memory.NewInterviewGuideRepository(), openings, opts.profiles, opts.generator, fixedClock{}, guideID{})
	return testServer{
		Handler: httpapi.NewHandler(httpapi.Services{
			JobOpenings:     usecase.NewJobOpeningService(openings, fixedClock{}, fixedID{}),
			Proposals:       usecase.NewProposalService(opts.deriver),
			Screening:       screening,
			InterviewGuides: guides,
		}),
		screening: screening,
	}
}

func newServer() http.Handler { return newTestServer(serverOptions{}) }

func newServerWithGenerator(generator usecase.QuestionGenerator) http.Handler {
	return newTestServer(serverOptions{generator: generator})
}

func newServerWithProfiles(profiles usecase.CandidateProfiles) http.Handler {
	return newTestServer(serverOptions{profiles: profiles})
}

const validBody = `{
  "title": "Backend Engineer",
  "description": "Backend engineer, 3+ years, Go or Python, must have shipped with Kafka.",
  "criteria": [
    {"name": "3+ years of Go or Python", "classification": "must-have", "weight": 5},
    {"name": "Kubernetes", "classification": "nice-to-have", "weight": 2}
  ],
  "expected_time_to_fill_days": 45
}`

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type openingJSON struct {
	ID                     string `json:"id"`
	Title                  string `json:"title"`
	Description            string `json:"description"`
	Status                 string `json:"status"`
	Outcome                string `json:"outcome"`
	OpenedAt               string `json:"opened_at"`
	ExpectedTimeToFillDays int    `json:"expected_time_to_fill_days"`
	Criteria               []struct {
		Name           string `json:"name"`
		Classification string `json:"classification"`
		Weight         int    `json:"weight"`
	} `json:"criteria"`
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body.String())
	}
}

// ---- createJobOpening(): POST /job-openings ----

// UC-1: the browser posts the confirmed opening and gets it back, open, with its location.
func TestPostJobOpening_Creates(t *testing.T) {
	rec := do(t, newServer(), http.MethodPost, "/job-openings", validBody)

	wantStatus(t, rec, http.StatusCreated)
	if loc := rec.Header().Get("Location"); loc != "/job-openings/jo-1" {
		t.Errorf("Location = %q", loc)
	}
	got := decode[openingJSON](t, rec)
	if got.ID != "jo-1" || got.Status != "open" || got.OpenedAt != "2026-09-26T10:00:00Z" ||
		got.ExpectedTimeToFillDays != 45 || len(got.Criteria) != 2 ||
		got.Criteria[0].Classification != "must-have" {
		t.Errorf("body = %+v", got)
	}
}

// A body that breaks a business rule is 422, with a reason the Recruiter can act on.
func TestPostJobOpening_BusinessRuleViolationIs422(t *testing.T) {
	body := strings.Replace(validBody, `"expected_time_to_fill_days": 45`, `"expected_time_to_fill_days": 0`, 1)

	rec := do(t, newServer(), http.MethodPost, "/job-openings", body)

	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if e := decode[map[string]string](t, rec)["error"]; !strings.Contains(e, "time-to-fill") {
		t.Errorf("error = %q, want it to name the broken rule", e)
	}
}

// A body that is not valid JSON is 400.
func TestPostJobOpening_MalformedJSONIs400(t *testing.T) {
	rec := do(t, newServer(), http.MethodPost, "/job-openings", `{"title": `)

	wantStatus(t, rec, http.StatusBadRequest)
}

// ---- getJobOpening() and listing ----

func TestGetJobOpening(t *testing.T) {
	h := newServer()
	do(t, h, http.MethodPost, "/job-openings", validBody)

	rec := do(t, h, http.MethodGet, "/job-openings/jo-1", "")

	wantStatus(t, rec, http.StatusOK)
	if got := decode[openingJSON](t, rec); got.Title != "Backend Engineer" {
		t.Errorf("body = %+v", got)
	}
}

func TestGetJobOpening_UnknownIs404(t *testing.T) {
	rec := do(t, newServer(), http.MethodGet, "/job-openings/jo-404", "")

	wantStatus(t, rec, http.StatusNotFound)
}

// An empty list is an empty JSON array, not null, so the browser can iterate it.
func TestListJobOpenings(t *testing.T) {
	h := newServer()
	empty := do(t, h, http.MethodGet, "/job-openings", "")
	wantStatus(t, empty, http.StatusOK)
	if strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Errorf("empty list body = %s, want []", empty.Body.String())
	}

	do(t, h, http.MethodPost, "/job-openings", validBody)
	rec := do(t, h, http.MethodGet, "/job-openings", "")

	if got := decode[[]openingJSON](t, rec); len(got) != 1 || got[0].ID != "jo-1" {
		t.Errorf("list = %+v", got)
	}
}

// ---- reviseScreeningCriteria(): PUT /job-openings/{id}/criteria ----

func TestPutCriteria_Revises(t *testing.T) {
	h := newServer()
	do(t, h, http.MethodPost, "/job-openings", validBody)

	rec := do(t, h, http.MethodPut, "/job-openings/jo-1/criteria",
		`[{"name": "English for daily standups", "classification": "nice-to-have", "weight": 2}]`)

	wantStatus(t, rec, http.StatusOK)
	if got := decode[openingJSON](t, rec); len(got.Criteria) != 1 || got.Criteria[0].Name != "English for daily standups" {
		t.Errorf("body = %+v", got)
	}
}

// ---- pauseOpening(), resumeOpening(), closeOpening() ----

// FR-1.8 end to end: pause, resume, close as filled.
func TestLifecycleEndpoints(t *testing.T) {
	h := newServer()
	do(t, h, http.MethodPost, "/job-openings", validBody)

	steps := []struct{ path, body, wantStatus, wantOutcome string }{
		{"/job-openings/jo-1/pause", "", "paused", ""},
		{"/job-openings/jo-1/resume", "", "open", ""},
		{"/job-openings/jo-1/close", `{"outcome": "filled"}`, "closed", "filled"},
	}
	for _, s := range steps {
		rec := do(t, h, http.MethodPost, s.path, s.body)
		wantStatus(t, rec, http.StatusOK)
		if got := decode[openingJSON](t, rec); got.Status != s.wantStatus || got.Outcome != s.wantOutcome {
			t.Errorf("%s: status/outcome = %q/%q, want %q/%q", s.path, got.Status, got.Outcome, s.wantStatus, s.wantOutcome)
		}
	}
}

// FR-1.9: acting on a closed opening conflicts with its state — 409.
func TestLifecycleEndpoints_ClosedOpeningIs409(t *testing.T) {
	h := newServer()
	do(t, h, http.MethodPost, "/job-openings", validBody)
	do(t, h, http.MethodPost, "/job-openings/jo-1/close", `{"outcome": "cancelled"}`)

	rec := do(t, h, http.MethodPost, "/job-openings/jo-1/pause", "")

	wantStatus(t, rec, http.StatusConflict)
}

// An outcome other than filled or cancelled is a business-rule violation — 422.
func TestCloseEndpoint_UnknownOutcomeIs422(t *testing.T) {
	h := newServer()
	do(t, h, http.MethodPost, "/job-openings", validBody)

	rec := do(t, h, http.MethodPost, "/job-openings/jo-1/close", `{"outcome": "abandoned"}`)

	wantStatus(t, rec, http.StatusUnprocessableEntity)
}

// ---- failures and operations ----

type failingService struct {
	httpapi.JobOpenings
	httpapi.InterviewGuides
}

func (failingService) ListJobOpenings(context.Context) ([]domain.JobOpening, error) {
	return nil, errors.New("pq: connection refused to 10.0.0.5:5432")
}

// An unexpected failure is 500 and does not leak infrastructure detail to the browser.
func TestUnexpectedErrorIs500WithoutDetail(t *testing.T) {
	rec := do(t, httpapi.NewHandler(httpapi.Services{JobOpenings: failingService{}}), http.MethodGet, "/job-openings", "")

	wantStatus(t, rec, http.StatusInternalServerError)
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("body leaks internal detail: %s", rec.Body.String())
	}
}

// ADR-005 rule 4: every service exposes a health endpoint for the platform.
func TestHealthz(t *testing.T) {
	rec := do(t, newServer(), http.MethodGet, "/healthz", "")

	wantStatus(t, rec, http.StatusOK)
}

// ---- API documentation ----

// The service publishes its own contract, byte for byte the one it is tested against.
func TestOpenAPISpecIsServed(t *testing.T) {
	rec := do(t, newServer(), http.MethodGet, "/openapi.yaml", "")

	wantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	if rec.Body.String() != string(api.Spec) {
		t.Error("served spec differs from the embedded contract")
	}
}

// A developer can browse and try the API in Swagger UI, which loads the served spec.
func TestSwaggerUIIsServed(t *testing.T) {
	rec := do(t, newServer(), http.MethodGet, "/docs", "")

	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "swagger-ui") || !strings.Contains(body, `url: "/openapi.yaml"`) {
		t.Errorf("/docs does not load Swagger UI against /openapi.yaml:\n%s", body)
	}
}
