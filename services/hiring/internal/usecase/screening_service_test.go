package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/memory"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// ---- test doubles for the screening ports ----

type screeningIDs struct{ batches, resumes int }

func (g *screeningIDs) NewBatchID() string {
	g.batches++
	return fmt.Sprintf("sb-%d", g.batches)
}

func (g *screeningIDs) NewResumeID() string {
	g.resumes++
	return fmt.Sprintf("rs-%d", g.resumes)
}

// fakeBroker records the requests it was given, and refuses them while down.
type fakeBroker struct {
	requested []string
	down      bool
}

func (b *fakeBroker) RequestScreening(_ context.Context, _ *domain.ScreeningBatch, e *domain.ScreeningEntry) error {
	if b.down {
		return errors.New("connection refused")
	}
	b.requested = append(b.requested, e.ResumeID)
	return nil
}

type brokenStore struct{}

func (brokenStore) StoreResume(context.Context, string, []byte) error {
	return errors.New("dial tcp 10.0.0.9:9000: connection refused")
}

// fakeJustifications plays Resume Processing's getCandidateProfile(), or fails with err.
type fakeJustifications struct{ err error }

func (j fakeJustifications) GetJustification(_ context.Context, candidateID, _, _ string) (domain.Justification, error) {
	if j.err != nil {
		return domain.Justification{}, j.err
	}
	return domain.Justification{Text: "6 years of Go, for " + candidateID,
		Evidence: []domain.CriterionEvidence{{Criterion: 0, Met: true, Score: 90, Evidence: "Go at two companies"}}}, nil
}

// ---- fixture ----

type screeningFixture struct {
	openings *usecase.JobOpeningService
	svc      *usecase.ScreeningService
	batches  *memory.ScreeningRepository
	files    *memory.ResumeStore
	broker   *fakeBroker
	opening  *domain.JobOpening
}

// newScreeningFixture wires the screening use cases to in-memory adapters, with one open job
// opening (jo-1) whose criteria are a must-have (0) and a nice-to-have (1).
func newScreeningFixture(t *testing.T, change func(*usecase.ScreeningDeps)) *screeningFixture {
	t.Helper()
	repo := memory.NewJobOpeningRepository()
	f := &screeningFixture{
		openings: usecase.NewJobOpeningService(repo, fixedClock{now}, &sequentialIDs{}),
		batches:  memory.NewScreeningRepository(),
		files:    memory.NewResumeStore(),
		broker:   &fakeBroker{},
	}
	deps := usecase.ScreeningDeps{
		Openings: repo, Batches: f.batches, Files: f.files, Requester: f.broker,
		Justifications: fakeJustifications{}, Clock: fixedClock{now}, IDs: &screeningIDs{},
	}
	if change != nil {
		change(&deps)
	}
	f.svc = usecase.NewScreeningService(deps)
	opening, err := f.openings.CreateJobOpening(context.Background(), validDraft())
	if err != nil {
		t.Fatal(err)
	}
	f.opening = opening
	return f
}

func pdf(name string) usecase.UploadedFile {
	return usecase.UploadedFile{Name: name, Content: []byte("%PDF-1.7\n" + name)}
}

func (f *screeningFixture) submit(t *testing.T, files ...usecase.UploadedFile) *domain.ScreeningBatch {
	t.Helper()
	b, _, err := f.svc.SubmitScreeningBatch(context.Background(), f.opening.ID, files)
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	return b
}

func (f *screeningFixture) record(t *testing.T, batchID, resumeID string, status domain.EntryStatus, r domain.ScreeningResult) {
	t.Helper()
	if err := f.svc.RecordScreeningResult(context.Background(), batchID, resumeID, status, r); err != nil {
		t.Fatalf("recording %s: %v", resumeID, err)
	}
}

func scored(candidate string, score int) domain.ScreeningResult {
	return domain.ScreeningResult{CandidateID: candidate, Score: score, MeetsMustHaves: true}
}

// ---- submitScreeningBatch() ----

// UC-2 steps 1–2, FR-2.1, FR-2.2: PDFs are stored and recorded as pending, each is handed to
// screening, and the call returns without waiting for any of them. A file that is not a PDF is
// left out and reported, not fatal to the batch.
func TestSubmitScreeningBatch(t *testing.T) {
	f := newScreeningFixture(t, nil)

	b, rejected, err := f.svc.SubmitScreeningBatch(context.Background(), f.opening.ID, []usecase.UploadedFile{
		pdf("ploy.pdf"),
		{Name: `C:\Users\hr\Desktop\notes.docx`, Content: []byte("PK\x03\x04 a word document")},
		pdf("somchai.pdf"),
	})

	if err != nil {
		t.Fatal(err)
	}
	if b.ID != "sb-1" || len(b.Entries) != 2 || b.Entries[0].Status != domain.EntryPending || len(b.Criteria) != 2 {
		t.Fatalf("batch = %+v", b)
	}
	if len(rejected) != 1 || rejected[0].Name != "notes.docx" || rejected[0].Reason != "not a PDF" {
		t.Errorf("rejected = %+v", rejected)
	}
	if content, ok := f.files.Resume("jo-1/sb-1/rs-1.pdf"); !ok || !strings.HasPrefix(string(content), "%PDF-") {
		t.Errorf("stored file = %q, %v", content, ok)
	}
	if strings.Join(f.broker.requested, ",") != "rs-1,rs-2" {
		t.Errorf("requested = %v", f.broker.requested)
	}
	if left, _ := f.batches.Unrequested(context.Background(), 10); len(left) != 0 {
		t.Errorf("confirmed requests are still unrequested: %+v", left)
	}
}

// The broker being down does not lose the batch: it is accepted, its requests wait, and the
// relay's next pass sends them — once (NFR-10).
func TestSubmitScreeningBatch_BrokerDownHoldsRequestsForTheRelay(t *testing.T) {
	f := newScreeningFixture(t, nil)
	f.broker.down = true

	b := f.submit(t, pdf("a.pdf"), pdf("b.pdf"))
	if len(f.broker.requested) != 0 || len(b.Entries) != 2 {
		t.Fatalf("requested while down: %v", f.broker.requested)
	}

	f.broker.down = false
	sent, err := f.svc.PublishPendingRequests(context.Background())
	if err != nil || sent != 2 {
		t.Fatalf("relay pass: sent %d, err %v", sent, err)
	}
	again, _ := f.svc.PublishPendingRequests(context.Background())
	if again != 0 || len(f.broker.requested) != 2 {
		t.Errorf("second pass sent %d more; requested = %v", again, f.broker.requested)
	}
}

// Only an open job opening takes resumes; an unknown one is not found.
func TestSubmitScreeningBatch_OpeningMustExistAndBeOpen(t *testing.T) {
	f := newScreeningFixture(t, nil)
	if _, _, err := f.svc.SubmitScreeningBatch(context.Background(), "jo-404", []usecase.UploadedFile{pdf("a.pdf")}); !errors.Is(err, usecase.ErrNotFound) {
		t.Errorf("unknown opening: err = %v", err)
	}

	if _, err := f.openings.PauseOpening(context.Background(), f.opening.ID); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.svc.SubmitScreeningBatch(context.Background(), f.opening.ID, []usecase.UploadedFile{pdf("a.pdf")})

	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("paused opening: err = %v, want ErrInvalidTransition", err)
	}
}

// A batch in which nothing can be screened is refused, saying why for every file.
func TestSubmitScreeningBatch_NothingScreenable(t *testing.T) {
	f := newScreeningFixture(t, nil)

	_, _, err := f.svc.SubmitScreeningBatch(context.Background(), f.opening.ID, []usecase.UploadedFile{
		{Name: "photo.png", Content: []byte("\x89PNG")},
		{Name: "empty.pdf"},
		{Name: "huge.pdf", Content: make([]byte, usecase.MaxResumeBytes+1)},
	})

	if !errors.Is(err, domain.ErrInvalidScreening) {
		t.Fatalf("err = %v, want ErrInvalidScreening", err)
	}
	for _, want := range []string{"photo.png: not a PDF", "empty.pdf: the file is empty", "huge.pdf: larger than 10 MB"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

func TestSubmitScreeningBatch_TooManyFiles(t *testing.T) {
	f := newScreeningFixture(t, nil)
	files := make([]usecase.UploadedFile, usecase.MaxResumesPerBatch+1)
	for i := range files {
		files[i] = pdf(fmt.Sprintf("%d.pdf", i))
	}

	_, _, err := f.svc.SubmitScreeningBatch(context.Background(), f.opening.ID, files)

	if !errors.Is(err, domain.ErrInvalidScreening) {
		t.Errorf("err = %v, want ErrInvalidScreening", err)
	}
}

// Storage failing refuses the batch and records nothing of it.
func TestSubmitScreeningBatch_StorageDownRecordsNothing(t *testing.T) {
	f := newScreeningFixture(t, func(d *usecase.ScreeningDeps) { d.Files = brokenStore{} })

	_, _, err := f.svc.SubmitScreeningBatch(context.Background(), f.opening.ID, []usecase.UploadedFile{pdf("a.pdf")})

	if !errors.Is(err, usecase.ErrStorageUnavailable) {
		t.Fatalf("err = %v, want ErrStorageUnavailable", err)
	}
	if batches, _ := f.batches.ListBatches(context.Background(), f.opening.ID); len(batches) != 0 {
		t.Errorf("a refused batch was recorded: %+v", batches)
	}
}

// ---- recording results: the consuming side of the handover ----

func TestRecordScreeningResult(t *testing.T) {
	f := newScreeningFixture(t, nil)
	b := f.submit(t, pdf("a.pdf"))

	f.record(t, b.ID, "rs-1", domain.EntryScreened, scored("cand-1", 77))

	stored, _ := f.svc.GetBatchStatus(context.Background(), f.opening.ID, b.ID)
	e := stored.Entry("rs-1")
	if e.Status != domain.EntryScreened || e.Result.Score != 77 || !e.Result.CompletedAt.Equal(now) {
		t.Errorf("entry = %+v, result = %+v", e, e.Result)
	}
	if !stored.Complete() {
		t.Error("batch not complete with its only resume screened")
	}
}

// The broker delivers at least once: a repeat says so and changes nothing.
func TestRecordScreeningResult_RepeatChangesNothing(t *testing.T) {
	f := newScreeningFixture(t, nil)
	b := f.submit(t, pdf("a.pdf"))
	f.record(t, b.ID, "rs-1", domain.EntryScreened, scored("cand-1", 77))

	err := f.svc.RecordScreeningResult(context.Background(), b.ID, "rs-1", domain.EntryScreened, scored("cand-1", 10))

	if !errors.Is(err, domain.ErrAlreadyCompleted) {
		t.Errorf("err = %v, want ErrAlreadyCompleted", err)
	}
	if stored, _ := f.svc.GetBatchStatus(context.Background(), f.opening.ID, b.ID); stored.Entry("rs-1").Result.Score != 77 {
		t.Error("a repeat overwrote the first answer")
	}
}

func TestRecordScreeningResult_UnknownOrMismatched(t *testing.T) {
	f := newScreeningFixture(t, nil)
	b := f.submit(t, pdf("a.pdf"))

	if err := f.svc.RecordScreeningResult(context.Background(), b.ID, "rs-404", domain.EntryScreened, scored("c", 1)); !errors.Is(err, usecase.ErrResumeNotFound) {
		t.Errorf("unknown resume: err = %v", err)
	}
	if err := f.svc.RecordScreeningResult(context.Background(), "sb-other", "rs-1", domain.EntryScreened, scored("c", 1)); !errors.Is(err, domain.ErrInvalidScreening) {
		t.Errorf("wrong batch: err = %v", err)
	}
}

// ---- reading results ----

// FR-2.6, FR-2.7: the ranking spans batches, unparsable resumes are listed apart, and resumes
// still being screened are counted.
func TestGetRankedResults(t *testing.T) {
	f := newScreeningFixture(t, nil)
	first := f.submit(t, pdf("a.pdf"), pdf("b.pdf"))
	second := f.submit(t, pdf("c.pdf"), pdf("d.pdf"))
	f.record(t, first.ID, "rs-1", domain.EntryScreened, scored("cand-1", 60))
	f.record(t, first.ID, "rs-2", domain.EntryNeedsReview, domain.ScreeningResult{Reason: "scanned image"})
	f.record(t, second.ID, "rs-3", domain.EntryScreened, scored("cand-3", 90))

	res, err := f.svc.GetRankedResults(context.Background(), f.opening.ID, domain.RankFilter{})

	if err != nil {
		t.Fatal(err)
	}
	if len(res.Ranked) != 2 || res.Ranked[0].Entry.ResumeID != "rs-3" || res.Ranked[1].Entry.ResumeID != "rs-1" {
		t.Errorf("ranked = %+v", res.Ranked)
	}
	if len(res.NeedsReview) != 1 || res.NeedsReview[0].Entry.Result.Reason != "scanned image" || res.Pending != 1 {
		t.Errorf("needs review = %+v, pending = %d", res.NeedsReview, res.Pending)
	}
	if _, err := f.svc.GetRankedResults(context.Background(), "jo-404", domain.RankFilter{}); !errors.Is(err, usecase.ErrNotFound) {
		t.Errorf("unknown opening: err = %v", err)
	}
}

// Opening one candidate reads the justification from Resume Processing — and still opens, with
// no justification, when Resume Processing does not answer.
func TestGetScreeningResult(t *testing.T) {
	for name, justifications := range map[string]usecase.Justifications{
		"justification read":     fakeJustifications{},
		"Resume Processing down": fakeJustifications{err: errors.New("connection refused")},
	} {
		f := newScreeningFixture(t, func(d *usecase.ScreeningDeps) { d.Justifications = justifications })
		b := f.submit(t, pdf("a.pdf"))
		f.record(t, b.ID, "rs-1", domain.EntryScreened, scored("cand-1", 70))

		c, err := f.svc.GetScreeningResult(context.Background(), f.opening.ID, "rs-1")

		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wantJustification := name == "justification read"
		if (c.Justification != nil) != wantJustification || c.Entry.Result.Score != 70 {
			t.Errorf("%s: justification = %+v, entry = %+v", name, c.Justification, c.Entry)
		}
	}
}

// A resume is reached only through the job opening it was submitted to.
func TestScreening_ResumeOfAnotherOpeningIsNotFound(t *testing.T) {
	f := newScreeningFixture(t, nil)
	b := f.submit(t, pdf("a.pdf"))

	if _, err := f.svc.GetScreeningResult(context.Background(), "jo-other", "rs-1"); !errors.Is(err, usecase.ErrResumeNotFound) {
		t.Errorf("result: err = %v", err)
	}
	if _, err := f.svc.GetBatchStatus(context.Background(), "jo-other", b.ID); !errors.Is(err, usecase.ErrBatchNotFound) {
		t.Errorf("batch: err = %v", err)
	}
}

// ---- the Recruiter's decisions ----

// FR-2.8, FR-2.9: an override and a shortlist are saved, and the original score survives.
func TestOverrideAndShortlistAreSaved(t *testing.T) {
	f := newScreeningFixture(t, nil)
	b := f.submit(t, pdf("a.pdf"))
	f.record(t, b.ID, "rs-1", domain.EntryScreened, scored("cand-1", 70))

	if _, err := f.svc.OverrideScore(context.Background(), f.opening.ID, "rs-1", 88); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ShortlistCandidate(context.Background(), f.opening.ID, "rs-1"); err != nil {
		t.Fatal(err)
	}

	stored, _ := f.svc.GetBatchStatus(context.Background(), f.opening.ID, b.ID)
	e := stored.Entry("rs-1")
	if e.Score() != 88 || e.Result.Score != 70 || !e.ShortlistedAt.Equal(now) {
		t.Errorf("entry = %+v", e)
	}
	if _, err := f.svc.ShortlistCandidate(context.Background(), f.opening.ID, "rs-1"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("second shortlist: err = %v", err)
	}
}
