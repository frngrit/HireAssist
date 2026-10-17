package httpapi_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/frngrit/HireAssist/services/hiring/api"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// Browsers label an uploaded PDF application/pdf. The validator decodes a part by its media
// type and knows no decoder for that one, so it is read as the binary file the spec says it is.
func init() {
	openapi3filter.RegisterBodyDecoder("application/pdf", openapi3filter.FileBodyDecoder)
}

// ADR-005 rule 1: the OpenAPI document is authoritative, and a contract test proves the
// server still matches it. Every request below is sent to the real handler; the request (when
// it is meant to be valid) and every response are validated against the spec, and a status
// code the spec does not document fails the test.
func TestHandlerMatchesOpenAPIContract(t *testing.T) {
	doc, router := loadContract(t)

	server := newTestServer(serverOptions{})
	aiDown := newTestServer(serverOptions{deriver: stubDeriver{err: usecase.ErrAIUnavailable}})

	twoPDFs, twoPDFsType := multipartBody(t, pdfUpload("ploy.pdf"), pdfUpload("somchai.pdf"))
	onePDF, onePDFType := multipartBody(t, pdfUpload("a.pdf"))
	notAPDF, notAPDFType := multipartBody(t, upload{"notes.bin", "application/octet-stream", "not a resume"})
	// Resume Processing answers for the batch: rs-1 screened, rs-2 unparsable.
	answer := func() {
		ctx := context.Background()
		if err := server.screening.RecordScreeningResult(ctx, "sb-1", "rs-1", domain.EntryScreened, meetsBoth("cand-1", 80)); err != nil {
			t.Fatal(err)
		}
		if err := server.screening.RecordScreeningResult(ctx, "sb-1", "rs-2", domain.EntryNeedsReview, domain.ScreeningResult{Reason: "scanned image"}); err != nil {
			t.Fatal(err)
		}
	}

	scenarios := []contractScenario{
		{name: "create", method: "POST", path: "/job-openings", body: validBody, wantStatus: 201},
		{name: "create, rule broken", method: "POST", path: "/job-openings", body: strings.Replace(validBody, `"weight": 5`, `"weight": 0`, 1), invalidRequest: true, wantStatus: 422},
		{name: "create, malformed", method: "POST", path: "/job-openings", body: `{"title": `, invalidRequest: true, wantStatus: 400},
		{name: "list", method: "GET", path: "/job-openings", wantStatus: 200},
		{name: "get", method: "GET", path: "/job-openings/jo-1", wantStatus: 200},
		{name: "get, unknown", method: "GET", path: "/job-openings/jo-404", wantStatus: 404},
		{name: "revise criteria", method: "PUT", path: "/job-openings/jo-1/criteria", body: `[{"name": "Go", "classification": "must-have", "weight": 3}, {"name": "Kubernetes", "classification": "nice-to-have", "weight": 1}]`, wantStatus: 200},
		{name: "pause", method: "POST", path: "/job-openings/jo-1/pause", wantStatus: 200},
		{name: "pause, already paused", method: "POST", path: "/job-openings/jo-1/pause", wantStatus: 409},
		{name: "submit batch, paused", method: "POST", path: "/job-openings/jo-1/batches", body: onePDF, contentType: onePDFType, wantStatus: 409},
		{name: "resume", method: "POST", path: "/job-openings/jo-1/resume", wantStatus: 200},

		{name: "propose", method: "POST", path: "/job-openings/propose", body: `{"description": "Backend engineer, 3+ years of Go.", "language": "en"}`, wantStatus: 200},
		{name: "propose, no description", method: "POST", path: "/job-openings/propose", body: `{"description": ""}`, invalidRequest: true, wantStatus: 422},
		{name: "propose, AI down", method: "POST", path: "/job-openings/propose", body: `{"description": "Backend engineer"}`, handler: aiDown, wantStatus: 502},

		{name: "submit batch", method: "POST", path: "/job-openings/jo-1/batches", body: twoPDFs, contentType: twoPDFsType, wantStatus: 202},
		{name: "submit batch, not multipart", method: "POST", path: "/job-openings/jo-1/batches", body: `{"resumes": []}`, invalidRequest: true, wantStatus: 400},
		{name: "submit batch, unknown opening", method: "POST", path: "/job-openings/jo-404/batches", body: onePDF, contentType: onePDFType, wantStatus: 404},
		{name: "submit batch, nothing screenable", method: "POST", path: "/job-openings/jo-1/batches", body: notAPDF, contentType: notAPDFType, wantStatus: 422},
		{name: "batch status", method: "GET", path: "/job-openings/jo-1/batches/sb-1", wantStatus: 200},
		{name: "batch status, unknown", method: "GET", path: "/job-openings/jo-1/batches/sb-404", wantStatus: 404},
		{name: "ranked results", method: "GET", path: "/job-openings/jo-1/results?min_score=10&criterion=Go", wantStatus: 200, before: answer},
		{name: "ranked results, minimum off the scale", method: "GET", path: "/job-openings/jo-1/results?min_score=101", invalidRequest: true, wantStatus: 400},
		{name: "ranked results, unknown opening", method: "GET", path: "/job-openings/jo-404/results", wantStatus: 404},
		{name: "screening result", method: "GET", path: "/job-openings/jo-1/results/rs-1", wantStatus: 200},
		{name: "screening result, not screened", method: "GET", path: "/job-openings/jo-1/results/rs-2", wantStatus: 200},
		{name: "screening result, unknown", method: "GET", path: "/job-openings/jo-1/results/rs-404", wantStatus: 404},
		{name: "override", method: "POST", path: "/job-openings/jo-1/results/rs-1/override", body: `{"score": 85}`, wantStatus: 200},
		{name: "override, off the scale", method: "POST", path: "/job-openings/jo-1/results/rs-1/override", body: `{"score": 101}`, invalidRequest: true, wantStatus: 422},
		{name: "override, not screened", method: "POST", path: "/job-openings/jo-1/results/rs-2/override", body: `{"score": 50}`, wantStatus: 409},
		{name: "override, malformed", method: "POST", path: "/job-openings/jo-1/results/rs-1/override", body: `{"score": `, invalidRequest: true, wantStatus: 400},
		{name: "shortlist", method: "POST", path: "/job-openings/jo-1/results/rs-1/shortlist", wantStatus: 200},
		{name: "shortlist, again", method: "POST", path: "/job-openings/jo-1/results/rs-1/shortlist", wantStatus: 409},
		{name: "shortlist, unknown", method: "POST", path: "/job-openings/jo-1/results/rs-404/shortlist", wantStatus: 404},

		// UC-3, on the opening created above.
		{name: "generate guide", method: "POST", path: "/job-openings/jo-1/candidates/cand-001/interview-guide", body: `{"emphasis": "more system design"}`, wantStatus: 201},
		{name: "generate guide, already one", method: "POST", path: "/job-openings/jo-1/candidates/cand-001/interview-guide", wantStatus: 409},
		{name: "get guide", method: "GET", path: "/job-openings/jo-1/candidates/cand-001/interview-guide", wantStatus: 200},
		{name: "get guide, none yet", method: "GET", path: "/job-openings/jo-1/candidates/cand-404/interview-guide", wantStatus: 404},
		{name: "regenerate guide", method: "POST", path: "/job-openings/jo-1/candidates/cand-001/interview-guide/regenerate", body: `{"emphasis": "more behavioural"}`, wantStatus: 200},
		{name: "revise questions", method: "PUT", path: "/job-openings/jo-1/candidates/cand-001/interview-guide/questions", body: `[{"text": "Walk me through the outage.", "criterion": "Kubernetes", "kind": "resume-claim"}]`, wantStatus: 200},
		{name: "revise questions, none left", method: "PUT", path: "/job-openings/jo-1/candidates/cand-001/interview-guide/questions", body: `[]`, invalidRequest: true, wantStatus: 422},
		{name: "record a note", method: "PUT", path: "/job-openings/jo-1/candidates/cand-001/interview-guide/questions/q1/note", body: `{"note": "Answered well on partitioning; vague on rebalancing."}`, wantStatus: 200},
		{name: "record a note, no such question", method: "PUT", path: "/job-openings/jo-1/candidates/cand-001/interview-guide/questions/q9/note", body: `{"note": "…"}`, wantStatus: 404},

		{name: "close, unknown outcome", method: "POST", path: "/job-openings/jo-1/close", body: `{"outcome": "abandoned"}`, invalidRequest: true, wantStatus: 422},
		{name: "close", method: "POST", path: "/job-openings/jo-1/close", body: `{"outcome": "filled"}`, wantStatus: 200},
		{name: "revise criteria, closed", method: "PUT", path: "/job-openings/jo-1/criteria", body: `[{"name": "Go", "classification": "must-have", "weight": 3}]`, wantStatus: 409},
		{name: "resume, unknown", method: "POST", path: "/job-openings/jo-404/resume", wantStatus: 404},
		{name: "health", method: "GET", path: "/healthz", wantStatus: 200},
	}

	covered := map[string]bool{}
	for _, s := range scenarios {
		if s.before != nil {
			s.before()
		}
		h := s.handler
		if h == nil {
			h = server
		}
		if op := exercise(t, router, h, s); op != "" {
			covered[op] = true
		}
	}
	assertEveryOperationCovered(t, doc, covered)
}

// contractScenario is one request sent through the real handler, and the status it must get.
type contractScenario struct {
	name, method, path, body string
	invalidRequest           bool // the request breaks the spec on purpose; only the response is checked
	wantStatus               int
	contentType              string       // default application/json when there is a body
	handler                  http.Handler // default: the scenario list's server
	before                   func()       // runs first, to set up what the request needs
}

// loadContract loads the embedded spec, checks it is valid OpenAPI, and builds a router over it.
func loadContract(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(api.Spec)
	if err != nil {
		t.Fatalf("loading spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("spec is not valid OpenAPI: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("building router: %v", err)
	}
	return doc, router
}

// exercise sends one scenario to the handler, validating the request — unless it breaks the
// spec on purpose — and the response. It returns the spec operation the request hit, or "" when
// the request is not in the spec.
func exercise(t *testing.T, router routers.Router, h http.Handler, s contractScenario) string {
	t.Helper()
	ctx := context.Background()
	req := httptest.NewRequest(s.method, s.path, strings.NewReader(s.body))
	switch {
	case s.contentType != "":
		req.Header.Set("Content-Type", s.contentType)
	case s.body != "":
		req.Header.Set("Content-Type", "application/json")
	}
	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Errorf("%s: %s %s is not in the spec: %v", s.name, s.method, s.path, err)
		return ""
	}
	reqInput := &openapi3filter.RequestValidationInput{
		Request: req, PathParams: pathParams, Route: route,
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if !s.invalidRequest {
		if err := openapi3filter.ValidateRequest(ctx, reqInput); err != nil {
			t.Errorf("%s: request does not match the spec: %v", s.name, err)
		}
		req.Body = io.NopCloser(strings.NewReader(s.body)) // validation consumed it
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != s.wantStatus {
		t.Errorf("%s: status = %d, want %d; body = %s", s.name, rec.Code, s.wantStatus, rec.Body.String())
	}
	if err := openapi3filter.ValidateResponse(ctx, &openapi3filter.ResponseValidationInput{
		RequestValidationInput: reqInput,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options:                reqInput.Options,
	}); err != nil {
		t.Errorf("%s: response does not match the spec: %v", s.name, err)
	}
	return route.Method + " " + route.Path
}

// assertEveryOperationCovered fails for any spec operation no scenario exercised: a documented
// operation the server does not serve is as much a broken contract as an undocumented one it does.
func assertEveryOperationCovered(t *testing.T, doc *openapi3.T, covered map[string]bool) {
	t.Helper()
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			if !covered[method+" "+path] {
				t.Errorf("spec operation %s %s is not exercised by this test", method, path)
			}
		}
	}
}
