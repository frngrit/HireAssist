package httpapi_test

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/frngrit/HireAssist/services/hiring/api"
)

// ADR-005 rule 1: the OpenAPI document is authoritative, and a contract test proves the
// server still matches it. Every request below is sent to the real handler; the request (when
// it is meant to be valid) and every response are validated against the spec, and a status
// code the spec does not document fails the test.
func TestHandlerMatchesOpenAPIContract(t *testing.T) {
	ctx := context.Background()
	doc, err := openapi3.NewLoader().LoadFromData(api.Spec)
	if err != nil {
		t.Fatalf("loading spec: %v", err)
	}
	if err := doc.Validate(ctx); err != nil {
		t.Fatalf("spec is not valid OpenAPI: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("building router: %v", err)
	}

	h := newServer()
	covered := map[string]bool{}

	scenarios := []struct {
		name, method, path, body string
		invalidRequest           bool // the request breaks the spec on purpose; only the response is checked
		wantStatus               int
	}{
		{"create", "POST", "/job-openings", validBody, false, 201},
		{"create, rule broken", "POST", "/job-openings", strings.Replace(validBody, `"weight": 5`, `"weight": 0`, 1), true, 422},
		{"create, malformed", "POST", "/job-openings", `{"title": `, true, 400},
		{"list", "GET", "/job-openings", "", false, 200},
		{"get", "GET", "/job-openings/jo-1", "", false, 200},
		{"get, unknown", "GET", "/job-openings/jo-404", "", false, 404},
		{"revise criteria", "PUT", "/job-openings/jo-1/criteria", `[{"name": "Go", "classification": "must-have", "weight": 3}]`, false, 200},
		{"pause", "POST", "/job-openings/jo-1/pause", "", false, 200},
		{"pause, already paused", "POST", "/job-openings/jo-1/pause", "", false, 409},
		{"resume", "POST", "/job-openings/jo-1/resume", "", false, 200},

		// UC-3, on the opening created above.
		{"generate guide", "POST", "/job-openings/jo-1/candidates/cand-001/interview-guide", `{"emphasis": "more system design"}`, false, 201},
		{"generate guide, already one", "POST", "/job-openings/jo-1/candidates/cand-001/interview-guide", "", false, 409},
		{"get guide", "GET", "/job-openings/jo-1/candidates/cand-001/interview-guide", "", false, 200},
		{"get guide, none yet", "GET", "/job-openings/jo-1/candidates/cand-404/interview-guide", "", false, 404},
		{"regenerate guide", "POST", "/job-openings/jo-1/candidates/cand-001/interview-guide/regenerate", `{"emphasis": "more behavioural"}`, false, 200},
		{"revise questions", "PUT", "/job-openings/jo-1/candidates/cand-001/interview-guide/questions", `[{"text": "Walk me through the outage.", "criterion": "Kubernetes", "kind": "resume-claim"}]`, false, 200},
		{"revise questions, none left", "PUT", "/job-openings/jo-1/candidates/cand-001/interview-guide/questions", `[]`, true, 422},
		{"record a note", "PUT", "/job-openings/jo-1/candidates/cand-001/interview-guide/questions/q1/note", `{"note": "Answered well on partitioning; vague on rebalancing."}`, false, 200},
		{"record a note, no such question", "PUT", "/job-openings/jo-1/candidates/cand-001/interview-guide/questions/q9/note", `{"note": "…"}`, false, 404},
		{"close, unknown outcome", "POST", "/job-openings/jo-1/close", `{"outcome": "abandoned"}`, true, 422},
		{"close", "POST", "/job-openings/jo-1/close", `{"outcome": "filled"}`, false, 200},
		{"revise criteria, closed", "PUT", "/job-openings/jo-1/criteria", `[{"name": "Go", "classification": "must-have", "weight": 3}]`, false, 409},
		{"resume, unknown", "POST", "/job-openings/jo-404/resume", "", false, 404},
		{"health", "GET", "/healthz", "", false, 200},
	}

	for _, s := range scenarios {
		req := httptest.NewRequest(s.method, s.path, strings.NewReader(s.body))
		if s.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		route, pathParams, err := router.FindRoute(req)
		if err != nil {
			t.Errorf("%s: %s %s is not in the spec: %v", s.name, s.method, s.path, err)
			continue
		}
		covered[route.Method+" "+route.Path] = true
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
	}

	// Nothing in the spec may go untested: a documented operation the server does not
	// serve is as much a broken contract as an undocumented one it does.
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			if !covered[method+" "+path] {
				t.Errorf("spec operation %s %s is not exercised by this test", method, path)
			}
		}
	}
}
