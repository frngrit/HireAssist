package domain_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

var submittedAt = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// A batch of two resumes against the opening of validCriteria(): criterion 0 is the must-have
// "3+ years of Go", criterion 1 the nice-to-have "Kubernetes experience".
func newBatch(t *testing.T) *domain.ScreeningBatch {
	t.Helper()
	b, err := domain.NewScreeningBatch("sb-1", openOpening(t), []domain.NewEntry{
		{ResumeID: "rs-1", FileName: "ploy.pdf", ObjectKey: "jo-1/sb-1/rs-1.pdf", SizeBytes: 100},
		{ResumeID: "rs-2", FileName: "somchai.pdf", ObjectKey: "jo-1/sb-1/rs-2.pdf", SizeBytes: 200},
	}, submittedAt)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// qualified is a scored result meeting the must-have.
func qualified(score int, at time.Time) domain.ScreeningResult {
	return domain.ScreeningResult{CandidateID: "cand-1", Score: score, MeetsMustHaves: true, CompletedAt: at}
}

// notQualified is a scored result missing the must-have, criterion 0.
func notQualified(score int, at time.Time) domain.ScreeningResult {
	return domain.ScreeningResult{CandidateID: "cand-2", Score: score, MeetsMustHaves: false, UnmetMustHaves: []int{0}, CompletedAt: at}
}

func screen(t *testing.T, b *domain.ScreeningBatch, resumeID string, r domain.ScreeningResult) {
	t.Helper()
	if err := b.RecordResult(resumeID, domain.EntryScreened, r); err != nil {
		t.Fatalf("recording %s: %v", resumeID, err)
	}
}

// ---- submitting a batch ----

// UC-2 step 2: every accepted resume starts pending, and the batch keeps its own copy of the
// criteria, so revising the opening later does not change what the batch is judged against.
func TestNewScreeningBatch_PendingEntriesAndACriteriaSnapshot(t *testing.T) {
	opening := openOpening(t)

	b, err := domain.NewScreeningBatch("sb-1", opening, []domain.NewEntry{{ResumeID: "rs-1", FileName: "a.pdf"}}, submittedAt)
	if err != nil {
		t.Fatal(err)
	}
	opening.Criteria[0].Name = "revised after submission"

	if b.Entries[0].Status != domain.EntryPending || b.Entries[0].BatchID != "sb-1" || b.JobOpeningID != "jo-1" {
		t.Errorf("entry = %+v", b.Entries[0])
	}
	if b.Criteria[0].Name != "3+ years of Go" {
		t.Errorf("snapshot changed with the opening: %q", b.Criteria[0].Name)
	}
	if b.Complete() {
		t.Error("a batch with a pending resume is complete")
	}
}

// Only an open opening takes resumes: paused is on hold and closed is final (FR-1.9).
func TestNewScreeningBatch_OpeningMustBeOpen(t *testing.T) {
	for _, change := range []func(*domain.JobOpening) error{
		(*domain.JobOpening).Pause,
		func(o *domain.JobOpening) error { return o.Close(domain.OutcomeCancelled) },
	} {
		opening := openOpening(t)
		if err := change(opening); err != nil {
			t.Fatal(err)
		}

		_, err := domain.NewScreeningBatch("sb-1", opening, []domain.NewEntry{{ResumeID: "rs-1"}}, submittedAt)

		if !errors.Is(err, domain.ErrInvalidTransition) {
			t.Errorf("%s opening: err = %v, want ErrInvalidTransition", opening.Status, err)
		}
	}
}

func TestNewScreeningBatch_NeedsAResume(t *testing.T) {
	_, err := domain.NewScreeningBatch("sb-1", openOpening(t), nil, submittedAt)

	if !errors.Is(err, domain.ErrInvalidScreening) {
		t.Errorf("err = %v, want ErrInvalidScreening", err)
	}
}

// ---- recording results ----

// FR-2.5: a scored result is kept with its score, must-have check and per-criterion verdicts.
func TestRecordResult_Screened(t *testing.T) {
	b := newBatch(t)

	screen(t, b, "rs-2", notQualified(40, submittedAt))

	e := b.Entry("rs-2")
	if e.Status != domain.EntryScreened || e.Result.Score != 40 || e.Result.MeetsMustHaves || e.Result.CandidateID != "cand-2" {
		t.Errorf("entry = %+v, result = %+v", e, e.Result)
	}
	if got := b.UnmetMustHaves(e); !reflect.DeepEqual(got, []string{"3+ years of Go"}) {
		t.Errorf("unmet must-haves = %v", got)
	}
}

// FR-2.7: a resume that cannot be parsed keeps its reason and no score.
func TestRecordResult_NeedsReviewKeepsOnlyTheReason(t *testing.T) {
	b := newBatch(t)

	err := b.RecordResult("rs-1", domain.EntryNeedsReview, domain.ScreeningResult{Score: 80, Reason: "  scanned image, no text layer  "})

	if err != nil {
		t.Fatal(err)
	}
	if r := b.Entry("rs-1").Result; r.Reason != "scanned image, no text layer" || r.Score != 0 {
		t.Errorf("result = %+v", r)
	}
}

// The broker delivers at least once, so the same answer can arrive twice. The first stands —
// and with it anything the Recruiter has done since.
func TestRecordResult_SecondAnswerIsRefused(t *testing.T) {
	b := newBatch(t)
	screen(t, b, "rs-1", qualified(70, submittedAt))
	if err := b.Entry("rs-1").OverrideScore(95, submittedAt); err != nil {
		t.Fatal(err)
	}

	err := b.RecordResult("rs-1", domain.EntryFailed, domain.ScreeningResult{Reason: "late duplicate"})

	if !errors.Is(err, domain.ErrAlreadyCompleted) {
		t.Errorf("err = %v, want ErrAlreadyCompleted", err)
	}
	if e := b.Entry("rs-1"); e.Status != domain.EntryScreened || e.Score() != 95 {
		t.Errorf("entry changed by a duplicate: %+v", e)
	}
}

// FR-2.5 guards the shape of a scored result, so a broken message cannot reach the ranking.
func TestRecordResult_RejectsResultsThatBreakFR25(t *testing.T) {
	cases := map[string]domain.ScreeningResult{
		"no candidate id":                      {Score: 50, MeetsMustHaves: true},
		"score above the scale":                {CandidateID: "c", Score: 101, MeetsMustHaves: true},
		"negative score":                       {CandidateID: "c", Score: -1, MeetsMustHaves: true},
		"unmet criterion the batch lacks":      {CandidateID: "c", Score: 50, UnmetMustHaves: []int{7}},
		"a nice-to-have named as a must-have":  {CandidateID: "c", Score: 50, UnmetMustHaves: []int{1}},
		"the same must-have twice":             {CandidateID: "c", Score: 50, UnmetMustHaves: []int{0, 0}},
		"qualified despite an unmet must-have": {CandidateID: "c", Score: 50, MeetsMustHaves: true, UnmetMustHaves: []int{0}},
		"not qualified without saying why":     {CandidateID: "c", Score: 50, MeetsMustHaves: false},
	}
	for name, r := range cases {
		b := newBatch(t)

		err := b.RecordResult("rs-1", domain.EntryScreened, r)

		if !errors.Is(err, domain.ErrInvalidScreening) {
			t.Errorf("%s: err = %v, want ErrInvalidScreening", name, err)
		}
		if b.Entry("rs-1").Status != domain.EntryPending {
			t.Errorf("%s: a refused result changed the entry", name)
		}
	}
}

func TestRecordResult_UnknownResumeOrOutcome(t *testing.T) {
	b := newBatch(t)

	if err := b.RecordResult("rs-404", domain.EntryScreened, qualified(50, submittedAt)); !errors.Is(err, domain.ErrInvalidScreening) {
		t.Errorf("unknown resume: err = %v", err)
	}
	if err := b.RecordResult("rs-1", domain.EntryPending, domain.ScreeningResult{}); !errors.Is(err, domain.ErrInvalidScreening) {
		t.Errorf("pending as an outcome: err = %v", err)
	}
}

func TestComplete_OnceEveryResumeHasAnAnswer(t *testing.T) {
	b := newBatch(t)
	screen(t, b, "rs-1", qualified(70, submittedAt))
	if b.Complete() {
		t.Fatal("complete with one resume pending")
	}

	if err := b.RecordResult("rs-2", domain.EntryFailed, domain.ScreeningResult{Reason: "retries exhausted"}); err != nil {
		t.Fatal(err)
	}

	if !b.Complete() {
		t.Error("not complete with every resume answered")
	}
}

// ---- the Recruiter's decisions ----

// FR-2.8: the override is what the ranking uses; the original is kept beside it.
func TestOverrideScore_KeepsTheOriginal(t *testing.T) {
	b := newBatch(t)
	screen(t, b, "rs-1", qualified(70, submittedAt))
	e := b.Entry("rs-1")

	if err := e.OverrideScore(85, submittedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if e.Score() != 85 || e.Result.Score != 70 || !e.Override.At.Equal(submittedAt.Add(time.Hour)) {
		t.Errorf("score = %d, original = %d, override = %+v", e.Score(), e.Result.Score, e.Override)
	}
}

func TestOverrideScore_OnlyAScreenedResumeAndOnTheScale(t *testing.T) {
	b := newBatch(t)
	if err := b.Entry("rs-1").OverrideScore(50, submittedAt); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("pending resume: err = %v, want ErrInvalidTransition", err)
	}

	screen(t, b, "rs-1", qualified(70, submittedAt))
	if err := b.Entry("rs-1").OverrideScore(150, submittedAt); !errors.Is(err, domain.ErrInvalidScreening) {
		t.Errorf("score 150: err = %v, want ErrInvalidScreening", err)
	}
}

// FR-2.9: a screened candidate is shortlisted once; a pending resume cannot be.
func TestShortlist(t *testing.T) {
	b := newBatch(t)
	if err := b.Entry("rs-1").Shortlist(submittedAt); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("pending resume: err = %v, want ErrInvalidTransition", err)
	}

	screen(t, b, "rs-1", qualified(70, submittedAt))
	if err := b.Entry("rs-1").Shortlist(submittedAt); err != nil {
		t.Fatal(err)
	}
	if err := b.Entry("rs-1").Shortlist(submittedAt); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("second shortlist: err = %v, want ErrInvalidTransition", err)
	}
}

// ---- ranking ----

// FR-2.6: qualified candidates first, then by score — the override counting — then by who was
// answered first. Unanswered and unparsable resumes are not ranked (FR-2.7).
func TestRank(t *testing.T) {
	opening := openOpening(t)
	entries := []domain.NewEntry{{ResumeID: "a"}, {ResumeID: "b"}, {ResumeID: "c"}, {ResumeID: "d"}, {ResumeID: "e"}, {ResumeID: "f"}}
	b, err := domain.NewScreeningBatch("sb-1", opening, entries, submittedAt)
	if err != nil {
		t.Fatal(err)
	}
	screen(t, b, "a", notQualified(99, submittedAt))                 // not qualified, however high
	screen(t, b, "b", qualified(60, submittedAt))                    // overridden up to 90 below
	screen(t, b, "c", qualified(80, submittedAt.Add(2*time.Minute))) // ties with d, answered later
	screen(t, b, "d", qualified(80, submittedAt.Add(time.Minute)))
	if err := b.RecordResult("e", domain.EntryNeedsReview, domain.ScreeningResult{Reason: "scanned"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Entry("b").OverrideScore(90, submittedAt); err != nil {
		t.Fatal(err)
	}

	got := ids(domain.Rank([]domain.ScreeningBatch{*b}, domain.RankFilter{}))

	if want := []string{"b", "d", "c", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ranking = %v, want %v", got, want)
	}
}

// FR-2.6: the list filters by minimum score and by an individual must-have. Screening reports
// nice-to-haves only through the score, so filtering on one matches nobody.
func TestRank_Filters(t *testing.T) {
	b := newBatch(t)
	screen(t, b, "rs-1", qualified(70, submittedAt))
	screen(t, b, "rs-2", notQualified(40, submittedAt))
	batches := []domain.ScreeningBatch{*b}

	if got := ids(domain.Rank(batches, domain.RankFilter{MinScore: 50})); !reflect.DeepEqual(got, []string{"rs-1"}) {
		t.Errorf("min score 50: %v", got)
	}
	if got := ids(domain.Rank(batches, domain.RankFilter{Criterion: "3+ YEARS OF GO"})); !reflect.DeepEqual(got, []string{"rs-1"}) {
		t.Errorf("criterion filter, any case: %v", got)
	}
	if got := ids(domain.Rank(batches, domain.RankFilter{Criterion: "Kubernetes experience"})); len(got) != 0 {
		t.Errorf("a nice-to-have filter: %v, want nobody", got)
	}
}

func ids(ranked []domain.Ranked) []string {
	out := []string{}
	for _, r := range ranked {
		out = append(out, r.Entry.ResumeID)
	}
	return out
}
