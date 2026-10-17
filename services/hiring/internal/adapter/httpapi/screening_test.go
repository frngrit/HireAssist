package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// ---- uploads ----

type upload struct{ name, contentType, content string }

func pdfUpload(name string) upload {
	return upload{name, "application/pdf", "%PDF-1.7\n% " + name + "\n%%EOF"}
}

// multipartBody encodes files the way a browser uploads them, all in the "resumes" field.
func multipartBody(t *testing.T, files ...upload) (body, contentType string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="resumes"; filename=%q`, f.name))
		h.Set("Content-Type", f.contentType)
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(f.content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String(), w.FormDataContentType()
}

func doUpload(t *testing.T, h http.Handler, path string, files ...upload) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartBody(t, files...)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ---- response shapes ----

type batchJSON struct {
	ID          string `json:"id"`
	SubmittedAt string `json:"submitted_at"`
	Complete    bool   `json:"complete"`
	Counts      struct {
		Pending, Screened int
		NeedsReview       int `json:"needs_review"`
	} `json:"counts"`
	Entries []struct {
		ResumeID string `json:"resume_id"`
		FileName string `json:"file_name"`
		Status   string `json:"status"`
	} `json:"entries"`
	Rejected []struct {
		FileName string `json:"file_name"`
		Reason   string `json:"reason"`
	} `json:"rejected"`
}

type resultJSON struct {
	ResumeID       string   `json:"resume_id"`
	Status         string   `json:"status"`
	Score          *int     `json:"score"`
	OriginalScore  *int     `json:"original_score"`
	Qualified      *bool    `json:"qualified"`
	UnmetMustHaves []string `json:"unmet_must_haves"`
	Shortlisted    bool     `json:"shortlisted"`
	Reason         string   `json:"reason"`
}

type resultsJSON struct {
	Pending     int          `json:"pending"`
	Ranked      []resultJSON `json:"ranked"`
	NeedsReview []resultJSON `json:"needs_review"`
}

// ---- fixture: an opening with a submitted batch of two resumes ----

// withBatch creates jo-1 (validBody: must-have "3+ years of Go or Python", nice-to-have
// "Kubernetes") and submits rs-1 and rs-2 in batch sb-1.
func withBatch(t *testing.T) testServer {
	t.Helper()
	s := newTestServer(serverOptions{})
	wantStatus(t, do(t, s, http.MethodPost, "/job-openings", validBody), http.StatusCreated)
	wantStatus(t, doUpload(t, s, "/job-openings/jo-1/batches", pdfUpload("ploy.pdf"), pdfUpload("somchai.pdf")), http.StatusAccepted)
	return s
}

// record plays the Resume Processing Service answering through the broker.
func (s testServer) record(t *testing.T, resumeID string, status domain.EntryStatus, r domain.ScreeningResult) {
	t.Helper()
	if err := s.screening.RecordScreeningResult(context.Background(), "sb-1", resumeID, status, r); err != nil {
		t.Fatalf("recording %s: %v", resumeID, err)
	}
}

func meetsBoth(candidate string, score int) domain.ScreeningResult {
	return domain.ScreeningResult{CandidateID: candidate, Score: score, MeetsMustHaves: true}
}

func missesMustHave(candidate string, score int) domain.ScreeningResult {
	return domain.ScreeningResult{CandidateID: candidate, Score: score, MeetsMustHaves: false, UnmetMustHaves: []int{0}}
}

// ---- proposeJobOpening(): POST /job-openings/propose ----

// UC-1 steps 2–3: the proposal comes back for review, with the parts the model could not read.
func TestPropose(t *testing.T) {
	rec := do(t, newServer(), http.MethodPost, "/job-openings/propose", `{"description": "Backend engineer, 3+ years of Go.", "language": "en"}`)

	wantStatus(t, rec, http.StatusOK)
	got := decode[struct {
		Title         string        `json:"title"`
		Criteria      []interface{} `json:"criteria"`
		Uninterpreted []string      `json:"uninterpreted"`
		Attribution   struct{ Provider string }
	}](t, rec)
	if got.Title != "Backend Engineer" || len(got.Criteria) != 2 || len(got.Uninterpreted) != 1 || got.Attribution.Provider != "canned" {
		t.Errorf("body = %+v", got)
	}
}

// FR-1.5: the AI Service down is 502 — saying so, and nothing about where it lives.
func TestPropose_AIDownIs502WithoutDetail(t *testing.T) {
	s := newTestServer(serverOptions{deriver: stubDeriver{err: fmt.Errorf("%w: dial tcp 10.0.0.7:50051", usecase.ErrAIUnavailable)}})

	rec := do(t, s, http.MethodPost, "/job-openings/propose", `{"description": "Backend engineer"}`)

	wantStatus(t, rec, http.StatusBadGateway)
	if body := rec.Body.String(); strings.Contains(body, "10.0.0.7") || !strings.Contains(body, "by hand") {
		t.Errorf("body = %s", body)
	}
}

func TestPropose_EmptyDescriptionIs422(t *testing.T) {
	rec := do(t, newServer(), http.MethodPost, "/job-openings/propose", `{"description": "  "}`)

	wantStatus(t, rec, http.StatusUnprocessableEntity)
}

// ---- submitScreeningBatch(): POST /job-openings/{id}/batches ----

// FR-2.1, FR-2.2: 202 at once with the batch and where to follow it; a file that is not a PDF
// is left out and listed.
func TestSubmitBatch(t *testing.T) {
	s := newTestServer(serverOptions{})
	do(t, s, http.MethodPost, "/job-openings", validBody)

	rec := doUpload(t, s, "/job-openings/jo-1/batches",
		pdfUpload("ploy.pdf"), upload{"notes.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "PK\x03\x04"}, pdfUpload("somchai.pdf"))

	wantStatus(t, rec, http.StatusAccepted)
	if loc := rec.Header().Get("Location"); loc != "/job-openings/jo-1/batches/sb-1" {
		t.Errorf("Location = %q", loc)
	}
	got := decode[batchJSON](t, rec)
	if got.ID != "sb-1" || len(got.Entries) != 2 || got.Entries[0].Status != "pending" || got.Counts.Pending != 2 || got.Complete {
		t.Errorf("batch = %+v", got)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].FileName != "notes.docx" || got.Rejected[0].Reason != "not a PDF" {
		t.Errorf("rejected = %+v", got.Rejected)
	}
}

func TestSubmitBatch_MalformedUploadsAre400(t *testing.T) {
	s := newTestServer(serverOptions{})
	do(t, s, http.MethodPost, "/job-openings", validBody)

	if rec := do(t, s, http.MethodPost, "/job-openings/jo-1/batches", `{"resumes": []}`); rec.Code != http.StatusBadRequest {
		t.Errorf("JSON instead of multipart: status = %d", rec.Code)
	}
	body, contentType := multipartBody(t)
	req := httptest.NewRequest(http.MethodPost, "/job-openings/jo-1/batches", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no files: status = %d", rec.Code)
	}
}

// Only an open opening takes resumes (409); an unknown one is 404.
func TestSubmitBatch_OpeningNotOpenOrUnknown(t *testing.T) {
	s := newTestServer(serverOptions{})
	do(t, s, http.MethodPost, "/job-openings", validBody)
	do(t, s, http.MethodPost, "/job-openings/jo-1/pause", "")

	wantStatus(t, doUpload(t, s, "/job-openings/jo-1/batches", pdfUpload("a.pdf")), http.StatusConflict)
	wantStatus(t, doUpload(t, s, "/job-openings/jo-404/batches", pdfUpload("a.pdf")), http.StatusNotFound)
}

type brokenStore struct{}

func (brokenStore) StoreResume(context.Context, string, []byte) error {
	return errors.New("dial tcp 10.0.0.9:9000: connection refused")
}

// Storage down is 502, and the address of the store stays inside.
func TestSubmitBatch_StorageDownIs502WithoutDetail(t *testing.T) {
	s := newTestServer(serverOptions{files: brokenStore{}})
	do(t, s, http.MethodPost, "/job-openings", validBody)

	rec := doUpload(t, s, "/job-openings/jo-1/batches", pdfUpload("a.pdf"))

	wantStatus(t, rec, http.StatusBadGateway)
	if strings.Contains(rec.Body.String(), "10.0.0.9") {
		t.Errorf("body leaks internal detail: %s", rec.Body.String())
	}
}

// ---- getBatchStatus(): GET /job-openings/{id}/batches/{batchId} ----

func TestBatchStatus(t *testing.T) {
	s := withBatch(t)
	s.record(t, "rs-1", domain.EntryScreened, meetsBoth("cand-1", 80))

	rec := do(t, s, http.MethodGet, "/job-openings/jo-1/batches/sb-1", "")

	wantStatus(t, rec, http.StatusOK)
	got := decode[batchJSON](t, rec)
	if got.Counts.Screened != 1 || got.Counts.Pending != 1 || got.Complete || got.Rejected != nil {
		t.Errorf("batch = %+v", got)
	}
	wantStatus(t, do(t, s, http.MethodGet, "/job-openings/jo-1/batches/sb-404", ""), http.StatusNotFound)
}

// ---- getRankedResults(): GET /job-openings/{id}/results ----

// FR-2.5–FR-2.7: the qualified candidate ranks above a higher score that misses a must-have,
// which says which; unparsable resumes are listed apart.
func TestRankedResults(t *testing.T) {
	s := withBatch(t)
	wantStatus(t, doUpload(t, s, "/job-openings/jo-1/batches", pdfUpload("scan.pdf")), http.StatusAccepted)
	s.record(t, "rs-1", domain.EntryScreened, missesMustHave("cand-1", 92))
	s.record(t, "rs-2", domain.EntryScreened, meetsBoth("cand-2", 70))
	if err := s.screening.RecordScreeningResult(context.Background(), "sb-2", "rs-3", domain.EntryNeedsReview,
		domain.ScreeningResult{Reason: "scanned image with no text layer"}); err != nil {
		t.Fatal(err)
	}

	rec := do(t, s, http.MethodGet, "/job-openings/jo-1/results", "")

	wantStatus(t, rec, http.StatusOK)
	got := decode[resultsJSON](t, rec)
	if len(got.Ranked) != 2 || got.Ranked[0].ResumeID != "rs-2" || got.Ranked[1].ResumeID != "rs-1" {
		t.Fatalf("ranked = %+v", got.Ranked)
	}
	if notQualified := got.Ranked[1]; *notQualified.Qualified ||
		len(notQualified.UnmetMustHaves) != 1 || notQualified.UnmetMustHaves[0] != "3+ years of Go or Python" {
		t.Errorf("not-qualified candidate = %+v", notQualified)
	}
	if len(got.NeedsReview) != 1 || got.NeedsReview[0].Score != nil || got.NeedsReview[0].Reason == "" || got.Pending != 0 {
		t.Errorf("needs review = %+v, pending = %d", got.NeedsReview, got.Pending)
	}
}

// FR-2.6: filter by minimum score and by criterion; a nonsense minimum is 400.
func TestRankedResults_Filters(t *testing.T) {
	s := withBatch(t)
	s.record(t, "rs-1", domain.EntryScreened, missesMustHave("cand-1", 92))
	s.record(t, "rs-2", domain.EntryScreened, meetsBoth("cand-2", 70))

	high := decode[resultsJSON](t, do(t, s, http.MethodGet, "/job-openings/jo-1/results?min_score=80", ""))
	goDevs := decode[resultsJSON](t, do(t, s, http.MethodGet, "/job-openings/jo-1/results?criterion=3%2B+years+of+Go+or+Python", ""))

	if len(high.Ranked) != 1 || high.Ranked[0].ResumeID != "rs-1" {
		t.Errorf("min_score=80: %+v", high.Ranked)
	}
	if len(goDevs.Ranked) != 1 || goDevs.Ranked[0].ResumeID != "rs-2" {
		t.Errorf("criterion filter: %+v", goDevs.Ranked)
	}
	wantStatus(t, do(t, s, http.MethodGet, "/job-openings/jo-1/results?min_score=high", ""), http.StatusBadRequest)
}

// ---- getScreeningResult(): GET /job-openings/{id}/results/{resumeId} ----

// Opening a candidate shows the justification held by Resume Processing.
func TestScreeningResult(t *testing.T) {
	s := withBatch(t)
	s.record(t, "rs-1", domain.EntryScreened, meetsBoth("cand-1", 80))

	rec := do(t, s, http.MethodGet, "/job-openings/jo-1/results/rs-1", "")

	wantStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); !strings.Contains(body, `"text":"Six years of Go at two companies, for cand-1."`) ||
		!strings.Contains(body, `"criterion":"3+ years of Go or Python"`) || !strings.Contains(body, `"score":80`) {
		t.Errorf("body = %s", body)
	}
	wantStatus(t, do(t, s, http.MethodGet, "/job-openings/jo-1/results/rs-404", ""), http.StatusNotFound)
}

// ---- overrideScore() and shortlistCandidate() ----

// FR-2.8: the override is the score; the original is kept beside it.
func TestOverrideScore(t *testing.T) {
	s := withBatch(t)
	s.record(t, "rs-1", domain.EntryScreened, meetsBoth("cand-1", 60))

	rec := do(t, s, http.MethodPost, "/job-openings/jo-1/results/rs-1/override", `{"score": 85}`)

	wantStatus(t, rec, http.StatusOK)
	if got := decode[resultJSON](t, rec); *got.Score != 85 || *got.OriginalScore != 60 {
		t.Errorf("result = %+v", got)
	}
	wantStatus(t, do(t, s, http.MethodPost, "/job-openings/jo-1/results/rs-1/override", `{"score": 101}`), http.StatusUnprocessableEntity)
	wantStatus(t, do(t, s, http.MethodPost, "/job-openings/jo-1/results/rs-2/override", `{"score": 50}`), http.StatusConflict)
	wantStatus(t, do(t, s, http.MethodPost, "/job-openings/jo-1/results/rs-1/override", `{}`), http.StatusBadRequest)
}

// FR-2.9: a screened candidate is shortlisted once.
func TestShortlist(t *testing.T) {
	s := withBatch(t)
	s.record(t, "rs-1", domain.EntryScreened, meetsBoth("cand-1", 60))

	rec := do(t, s, http.MethodPost, "/job-openings/jo-1/results/rs-1/shortlist", "")

	wantStatus(t, rec, http.StatusOK)
	if got := decode[resultJSON](t, rec); !got.Shortlisted {
		t.Errorf("result = %+v", got)
	}
	wantStatus(t, do(t, s, http.MethodPost, "/job-openings/jo-1/results/rs-1/shortlist", ""), http.StatusConflict)
}
