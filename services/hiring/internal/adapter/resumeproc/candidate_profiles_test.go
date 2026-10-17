package resumeproc_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/resumeproc"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// The Resume Processing Service's answer, as its OpenAPI document describes it.
const profileBody = `{
  "id": "cand-001",
  "job_opening_id": "jo-1",
  "full_name": "Somchai Jaidee",
  "email": "somchai@example.com",
  "skills": ["Go", "Kafka"],
  "resume_text": "4 years as a backend developer in Bangkok",
  "created_at": "2026-10-01T08:00:00Z"
}`

func serving(t *testing.T, status int, body string) *resumeproc.CandidateProfiles {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/candidates/cand-001" {
			t.Errorf("asked for %q, want /candidates/cand-001", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return resumeproc.New(srv.URL, 2*time.Second)
}

func TestGetCandidateProfile(t *testing.T) {
	profile, err := serving(t, http.StatusOK, profileBody).GetCandidateProfile(context.Background(), "cand-001")
	if err != nil {
		t.Fatalf("GetCandidateProfile: %v", err)
	}

	if profile.CandidateID != "cand-001" || len(profile.Skills) != 2 {
		t.Errorf("profile = %+v", profile)
	}
	if profile.AdditionalText != "4 years as a backend developer in Bangkok" {
		t.Errorf("resume text = %q", profile.AdditionalText)
	}
}

// The name and the email address are in the answer and are deliberately not read: an interview
// guide is built from what a candidate can be asked about, and a copy of someone's contact
// details here would be a second place to erase them from.
func TestGetCandidateProfile_DoesNotCarryWhoTheCandidateIs(t *testing.T) {
	profile, err := serving(t, http.StatusOK, profileBody).GetCandidateProfile(context.Background(), "cand-001")
	if err != nil {
		t.Fatalf("GetCandidateProfile: %v", err)
	}

	for _, field := range append(profile.Skills, profile.AdditionalText, profile.CandidateID) {
		if field == "Somchai Jaidee" || field == "somchai@example.com" {
			t.Errorf("the profile carries %q", field)
		}
	}
}

func TestGetCandidateProfile_UnknownCandidate(t *testing.T) {
	_, err := serving(t, http.StatusNotFound, `{"error": "not found"}`).GetCandidateProfile(context.Background(), "cand-001")

	// Translated into the use case's own error, so the handler can answer 404 rather than 502.
	if !errors.Is(err, usecase.ErrCandidateNotFound) {
		t.Errorf("err = %v, want ErrCandidateNotFound", err)
	}
}

func TestGetCandidateProfile_ServiceFailure(t *testing.T) {
	_, err := serving(t, http.StatusInternalServerError, `{"error": "boom"}`).GetCandidateProfile(context.Background(), "cand-001")

	if err == nil || errors.Is(err, usecase.ErrCandidateNotFound) {
		t.Errorf("err = %v, want a plain failure", err)
	}
}

func TestGetCandidateProfile_UnreachableService(t *testing.T) {
	client := resumeproc.New("http://127.0.0.1:1", 500*time.Millisecond)

	if _, err := client.GetCandidateProfile(context.Background(), "cand-001"); err == nil {
		t.Error("an unreachable service was reported as a profile")
	}
}
