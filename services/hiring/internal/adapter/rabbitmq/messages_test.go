package rabbitmq_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/rabbitmq"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

func batch() *domain.ScreeningBatch {
	return &domain.ScreeningBatch{
		ID: "sb-1", JobOpeningID: "jo-1",
		Criteria: []domain.Criterion{
			{Name: "3+ years of Go", Classification: domain.MustHave, Weight: 5},
			{Name: "Kubernetes", Classification: domain.NiceToHave, Weight: 2},
			{Name: "English for standups", Classification: domain.MustHave, Weight: 1},
		},
		Entries: []domain.ScreeningEntry{{ResumeID: "rs-1", BatchID: "sb-1", FileName: "ploy.pdf", ObjectKey: "jo-1/sb-1/rs-1.pdf", SizeBytes: 1234}},
	}
}

// A request is the body MESSAGES.md shows: where the file is, and the criteria snapshot in the
// AI Service's shape.
func TestNewRequested(t *testing.T) {
	b := batch()

	got, _ := json.Marshal(rabbitmq.NewRequested("resumes", b, &b.Entries[0], "corr-1"))

	want := `{"batch_id":"sb-1","resume_id":"rs-1","job_opening_id":"jo-1",` +
		`"file":{"bucket":"resumes","key":"jo-1/sb-1/rs-1.pdf"},` +
		`"criteria":[{"id":"c1","text":"3+ years of Go","must_have":true,"weight":5},` +
		`{"id":"c2","text":"Kubernetes","must_have":false,"weight":2},` +
		`{"id":"c3","text":"English for standups","must_have":true,"weight":1}],` +
		`"correlation_id":"corr-1"}`
	if string(got) != want {
		t.Errorf("message =\n%s\nwant\n%s", got, want)
	}
}

func decodeCompleted(t *testing.T, body string) rabbitmq.Completed {
	t.Helper()
	var m rabbitmq.Completed
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A scored answer — the example in MESSAGES.md, not qualified this time — becomes a domain
// result. The justification it carries is not kept: it stays with Resume Processing.
func TestCompleted_Scored(t *testing.T) {
	m := decodeCompleted(t, `{"batch_id":"sb-1","resume_id":"rs-1","job_opening_id":"jo-1","candidate_id":"cand-001",
		"status":"scored","score":64,"meets_must_haves":false,"unmet_must_have_ids":["c1","c3"],
		"justification":"Five years of Java…","error":null,"correlation_id":"6f1c"}`)

	status, r, err := m.ToDomain()

	if err != nil {
		t.Fatal(err)
	}
	want := domain.ScreeningResult{CandidateID: "cand-001", Score: 64, MeetsMustHaves: false, UnmetMustHaves: []int{0, 2}}
	if status != domain.EntryScreened || !reflect.DeepEqual(r, want) {
		t.Errorf("status = %s, result = %+v", status, r)
	}
}

// Unparsable is a resume for manual review (FR-2.7); failed is screening giving up. Both keep
// the reason.
func TestCompleted_UnparsableAndFailed(t *testing.T) {
	for body, want := range map[string]domain.EntryStatus{
		`{"batch_id":"sb-1","resume_id":"rs-1","status":"unparsable","score":null,"error":"no text layer"}`: domain.EntryNeedsReview,
		`{"batch_id":"sb-1","resume_id":"rs-1","status":"failed","score":null,"error":"no text layer"}`:     domain.EntryFailed,
	} {
		status, r, err := decodeCompleted(t, body).ToDomain()

		if err != nil || status != want || r.Reason != "no text layer" {
			t.Errorf("%s: status = %s, result = %+v, err = %v", body, status, r, err)
		}
	}
}

// What the contract requires is checked here; a message missing it goes to the dead-letter queue.
func TestCompleted_Malformed(t *testing.T) {
	for name, body := range map[string]string{
		"no resume id":           `{"batch_id":"sb-1","status":"failed"}`,
		"unknown status":         `{"batch_id":"sb-1","resume_id":"rs-1","status":"rejected"}`,
		"scored without a score": `{"batch_id":"sb-1","resume_id":"rs-1","status":"scored","meets_must_haves":true}`,
		"an id this service never sent": `{"batch_id":"sb-1","resume_id":"rs-1","status":"scored","score":1,
			"meets_must_haves":false,"unmet_must_have_ids":["go-skills"]}`,
	} {
		_, _, err := decodeCompleted(t, body).ToDomain()

		if !errors.Is(err, rabbitmq.ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
}
