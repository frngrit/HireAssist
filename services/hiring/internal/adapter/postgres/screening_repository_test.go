package postgres_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/postgres"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

var _ usecase.ScreeningRepository = (*postgres.ScreeningRepository)(nil)

var submitted = time.Date(2026, 10, 16, 9, 0, 0, 123000, time.UTC)

// newScreeningRepo empties the database (newRepo truncates job openings, and every screening
// table with them) and stores an open job opening, jo-1, to submit against.
func newScreeningRepo(t *testing.T) (*postgres.ScreeningRepository, *domain.JobOpening) {
	t.Helper()
	openings := newRepo(t)
	o := opening("jo-1", "Backend Engineer")
	if err := openings.Save(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("HIRING_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return postgres.NewScreeningRepository(pool), o
}

func newScreeningBatch(t *testing.T, id string, o *domain.JobOpening, resumeIDs ...string) *domain.ScreeningBatch {
	t.Helper()
	entries := make([]domain.NewEntry, len(resumeIDs))
	for i, r := range resumeIDs {
		entries[i] = domain.NewEntry{ResumeID: r, FileName: r + ".pdf", ObjectKey: o.ID + "/" + id + "/" + r + ".pdf", SizeBytes: int64(100 + i)}
	}
	b, err := domain.NewScreeningBatch(id, o, entries, submitted)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A batch reads back exactly as it was saved: snapshot, entries in order, all pending.
func TestScreeningRepository_SaveAndFindBatch(t *testing.T) {
	repo, o := newScreeningRepo(t)
	b := newScreeningBatch(t, "sb-1", o, "rs-1", "rs-2")
	ctx := context.Background()

	if err := repo.SaveBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindBatch(ctx, "sb-1")

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Errorf("read back\n%+v\nwant\n%+v", got, b)
	}
	if _, err := repo.FindBatch(ctx, "sb-404"); !errors.Is(err, usecase.ErrBatchNotFound) {
		t.Errorf("unknown batch: err = %v", err)
	}
	if _, err := repo.FindBatchOfResume(ctx, "rs-404"); !errors.Is(err, usecase.ErrResumeNotFound) {
		t.Errorf("unknown resume: err = %v", err)
	}
}

// Everything decided about a resume survives the round trip: result, unmet must-haves,
// override, shortlist — and a resume that could not be screened keeps its reason and no score.
func TestScreeningRepository_SaveEntry(t *testing.T) {
	repo, o := newScreeningRepo(t)
	b := newScreeningBatch(t, "sb-1", o, "rs-1", "rs-2")
	ctx := context.Background()
	if err := repo.SaveBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	at := submitted.Add(time.Minute)
	if err := b.RecordResult("rs-1", domain.EntryScreened, domain.ScreeningResult{
		CandidateID: "cand-1", Score: 64, MeetsMustHaves: false, UnmetMustHaves: []int{0}, CompletedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Entry("rs-1").OverrideScore(80, at); err != nil {
		t.Fatal(err)
	}
	if err := b.Entry("rs-1").Shortlist(at); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordResult("rs-2", domain.EntryNeedsReview, domain.ScreeningResult{Reason: "scanned image", CompletedAt: at}); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"rs-1", "rs-2"} {
		if err := repo.SaveEntry(ctx, b.Entry(id)); err != nil {
			t.Fatalf("saving %s: %v", id, err)
		}
	}
	got, err := repo.FindBatchOfResume(ctx, "rs-2")

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Errorf("read back\n%+v\nwant\n%+v", got, b)
	}
}

// The outbox: unconfirmed requests are found oldest first, within the limit, until marked.
func TestScreeningRepository_UnrequestedUntilMarked(t *testing.T) {
	repo, o := newScreeningRepo(t)
	ctx := context.Background()
	for _, b := range []*domain.ScreeningBatch{newScreeningBatch(t, "sb-1", o, "rs-1", "rs-2"), newScreeningBatch(t, "sb-2", o, "rs-3")} {
		if err := repo.SaveBatch(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.MarkRequested(ctx, "rs-1", submitted); err != nil {
		t.Fatal(err)
	}

	all, err := repo.Unrequested(ctx, 10)
	limited, _ := repo.Unrequested(ctx, 1)

	if err != nil {
		t.Fatal(err)
	}
	if got := resumeIDs(all); !reflect.DeepEqual(got, []string{"rs-2", "rs-3"}) {
		t.Errorf("unrequested = %v", got)
	}
	if got := resumeIDs(limited); !reflect.DeepEqual(got, []string{"rs-2"}) {
		t.Errorf("unrequested, limit 1 = %v", got)
	}
	if batches, _ := repo.ListBatches(ctx, "jo-1"); len(batches) != 2 || batches[0].ID != "sb-1" {
		t.Errorf("batches of jo-1 = %+v", batches)
	}
}

func resumeIDs(batches []domain.ScreeningBatch) []string {
	ids := []string{}
	for _, b := range batches {
		for _, e := range b.Entries {
			ids = append(ids, e.ResumeID)
		}
	}
	return ids
}
