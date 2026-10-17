package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// ScreeningRepository stores screening batches in PostgreSQL: a batch with its criteria snapshot,
// and one row per resume. It is safe for concurrent use; two saves of the same resume are
// last-write-wins.
type ScreeningRepository struct {
	pool *pgxpool.Pool
}

// NewScreeningRepository returns a repository over an open connection pool.
func NewScreeningRepository(pool *pgxpool.Pool) *ScreeningRepository {
	return &ScreeningRepository{pool: pool}
}

// SaveBatch inserts a new batch, its snapshot and its entries in one transaction.
func (r *ScreeningRepository) SaveBatch(ctx context.Context, b *domain.ScreeningBatch) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO screening_batches (id, job_opening_id, submitted_at) VALUES ($1, $2, $3)`,
			b.ID, b.JobOpeningID, b.SubmittedAt,
		); err != nil {
			return fmt.Errorf("saving screening batch %s: %w", b.ID, err)
		}
		criteria := make([][]any, len(b.Criteria))
		for i, c := range b.Criteria {
			criteria[i] = []any{b.ID, i, c.Name, string(c.Classification), c.Weight}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"screening_batch_criteria"},
			[]string{"batch_id", "position", "name", "classification", "weight"}, pgx.CopyFromRows(criteria),
		); err != nil {
			return fmt.Errorf("saving the criteria snapshot of %s: %w", b.ID, err)
		}
		entries := make([][]any, len(b.Entries))
		for i, e := range b.Entries {
			entries[i] = []any{e.ResumeID, b.ID, i, e.FileName, e.ObjectKey, e.SizeBytes, string(e.Status)}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"screening_entries"},
			[]string{"resume_id", "batch_id", "position", "file_name", "object_key", "size_bytes", "status"}, pgx.CopyFromRows(entries),
		); err != nil {
			return fmt.Errorf("saving the resumes of %s: %w", b.ID, err)
		}
		return nil
	})
}

// FindBatch returns the batch, or usecase.ErrBatchNotFound.
func (r *ScreeningRepository) FindBatch(ctx context.Context, id string) (*domain.ScreeningBatch, error) {
	return r.findOne(ctx, usecase.ErrBatchNotFound, `WHERE b.id = $1`, id)
}

// FindBatchOfResume returns the batch holding the resume, or usecase.ErrResumeNotFound.
func (r *ScreeningRepository) FindBatchOfResume(ctx context.Context, resumeID string) (*domain.ScreeningBatch, error) {
	return r.findOne(ctx, usecase.ErrResumeNotFound,
		`WHERE b.id = (SELECT batch_id FROM screening_entries WHERE resume_id = $1)`, resumeID)
}

// ListBatches returns a job opening's batches in submission order.
func (r *ScreeningRepository) ListBatches(ctx context.Context, jobOpeningID string) ([]domain.ScreeningBatch, error) {
	return r.load(ctx, `WHERE b.job_opening_id = $1`, jobOpeningID)
}

// Unrequested returns, oldest first, the batches holding resumes whose request has not been
// confirmed, each with only those entries, at most limit in all.
func (r *ScreeningRepository) Unrequested(ctx context.Context, limit int) ([]domain.ScreeningBatch, error) {
	batches, err := r.load(ctx, `WHERE b.id IN (
		SELECT batch_id FROM screening_entries WHERE status = 'pending' AND requested_at IS NULL)`)
	if err != nil {
		return nil, err
	}
	out := []domain.ScreeningBatch{}
	for _, b := range batches {
		b.Entries = slices.DeleteFunc(b.Entries, func(e domain.ScreeningEntry) bool {
			return e.Status != domain.EntryPending || !e.RequestedAt.IsZero()
		})
		b.Entries = b.Entries[:min(len(b.Entries), limit)]
		if len(b.Entries) > 0 {
			out = append(out, b)
			limit -= len(b.Entries)
		}
		if limit == 0 {
			break
		}
	}
	return out, nil
}

// SaveEntry stores one resume's status, result, override and shortlist.
func (r *ScreeningRepository) SaveEntry(ctx context.Context, e *domain.ScreeningEntry) error {
	var (
		candidate, reason             *string
		score, overrideScore          *int
		meets                         *bool
		completed, overridden, listed *time.Time
		unmet                         = []int32{}
	)
	if res := e.Result; res != nil {
		candidate, reason, completed = nonEmpty(res.CandidateID), nonEmpty(res.Reason), nonZero(res.CompletedAt)
		if e.Status == domain.EntryScreened {
			score, meets = &res.Score, &res.MeetsMustHaves
		}
		for _, pos := range res.UnmetMustHaves {
			unmet = append(unmet, int32(pos))
		}
	}
	if o := e.Override; o != nil {
		overrideScore, overridden = &o.Score, &o.At
	}
	listed = nonZero(e.ShortlistedAt)

	tag, err := r.pool.Exec(ctx, `
		UPDATE screening_entries SET
			status = $2, candidate_id = $3, score = $4, meets_must_haves = $5, unmet_must_haves = $6,
			reason = $7, completed_at = $8, override_score = $9, overridden_at = $10, shortlisted_at = $11
		WHERE resume_id = $1`,
		e.ResumeID, string(e.Status), candidate, score, meets, unmet, reason, completed, overrideScore, overridden, listed)
	if err != nil {
		return fmt.Errorf("saving resume %s: %w", e.ResumeID, err)
	}
	if tag.RowsAffected() == 0 {
		return usecase.ErrResumeNotFound
	}
	return nil
}

// MarkRequested records when the broker confirmed a resume's screening request.
func (r *ScreeningRepository) MarkRequested(ctx context.Context, resumeID string, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE screening_entries SET requested_at = $2 WHERE resume_id = $1`, resumeID, at)
	if err != nil {
		return fmt.Errorf("marking %s requested: %w", resumeID, err)
	}
	if tag.RowsAffected() == 0 {
		return usecase.ErrResumeNotFound
	}
	return nil
}

func (r *ScreeningRepository) findOne(ctx context.Context, notFound error, where string, args ...any) (*domain.ScreeningBatch, error) {
	batches, err := r.load(ctx, where, args...)
	if err != nil {
		return nil, err
	}
	if len(batches) == 0 {
		return nil, notFound
	}
	return &batches[0], nil
}

// load reads the batches matching where — a clause over screening_batches b — with their
// snapshots and entries, in three queries whatever the number of batches.
func (r *ScreeningRepository) load(ctx context.Context, where string, args ...any) ([]domain.ScreeningBatch, error) {
	rows, err := r.pool.Query(ctx, `SELECT b.id, b.job_opening_id, b.submitted_at FROM screening_batches b `+where+` ORDER BY b.seq`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading screening batches: %w", err)
	}
	batches, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ScreeningBatch, error) {
		var b domain.ScreeningBatch
		err := row.Scan(&b.ID, &b.JobOpeningID, &b.SubmittedAt)
		b.SubmittedAt = b.SubmittedAt.UTC()
		return b, err
	})
	if err != nil || len(batches) == 0 {
		return batches, err
	}
	ids := make([]string, len(batches))
	byID := map[string]*domain.ScreeningBatch{}
	for i := range batches {
		ids[i] = batches[i].ID
		byID[batches[i].ID] = &batches[i]
	}
	if err := r.loadCriteria(ctx, ids, byID); err != nil {
		return nil, err
	}
	if err := r.loadEntries(ctx, ids, byID); err != nil {
		return nil, err
	}
	return batches, nil
}

func (r *ScreeningRepository) loadCriteria(ctx context.Context, ids []string, byID map[string]*domain.ScreeningBatch) error {
	rows, err := r.pool.Query(ctx, `
		SELECT batch_id, name, classification, weight FROM screening_batch_criteria
		WHERE batch_id = ANY($1) ORDER BY batch_id, position`, ids)
	if err != nil {
		return fmt.Errorf("loading criteria snapshots: %w", err)
	}
	var (
		batchID string
		c       domain.Criterion
	)
	_, err = pgx.ForEachRow(rows, []any{&batchID, &c.Name, &c.Classification, &c.Weight}, func() error {
		byID[batchID].Criteria = append(byID[batchID].Criteria, c)
		return nil
	})
	if err != nil {
		return fmt.Errorf("loading criteria snapshots: %w", err)
	}
	return nil
}

func (r *ScreeningRepository) loadEntries(ctx context.Context, ids []string, byID map[string]*domain.ScreeningBatch) error {
	rows, err := r.pool.Query(ctx, `
		SELECT resume_id, batch_id, file_name, object_key, size_bytes, status, requested_at,
		       candidate_id, score, meets_must_haves, unmet_must_haves, reason, completed_at,
		       override_score, overridden_at, shortlisted_at
		FROM screening_entries WHERE batch_id = ANY($1) ORDER BY batch_id, position`, ids)
	if err != nil {
		return fmt.Errorf("loading resumes: %w", err)
	}
	var (
		e                                        domain.ScreeningEntry
		requested, completed, overridden, listed *time.Time
		candidate, reason                        *string
		score, overrideScore                     *int
		meets                                    *bool
		unmet                                    []int32
	)
	_, err = pgx.ForEachRow(rows, []any{&e.ResumeID, &e.BatchID, &e.FileName, &e.ObjectKey, &e.SizeBytes, &e.Status, &requested,
		&candidate, &score, &meets, &unmet, &reason, &completed, &overrideScore, &overridden, &listed}, func() error {
		entry := e
		entry.RequestedAt, entry.ShortlistedAt = utcOrZero(requested), utcOrZero(listed)
		if entry.Status != domain.EntryPending {
			entry.Result = &domain.ScreeningResult{
				CandidateID: deref(candidate), Score: deref(score), MeetsMustHaves: deref(meets),
				UnmetMustHaves: positions(unmet), Reason: deref(reason), CompletedAt: utcOrZero(completed),
			}
		}
		if overrideScore != nil {
			entry.Override = &domain.ScoreOverride{Score: *overrideScore, At: utcOrZero(overridden)}
		}
		b := byID[entry.BatchID]
		b.Entries = append(b.Entries, entry)
		return nil
	})
	if err != nil {
		return fmt.Errorf("loading resumes: %w", err)
	}
	return nil
}

// positions converts stored positions back, with none as nil — the domain's own empty value.
func positions(stored []int32) []int {
	if len(stored) == 0 {
		return nil
	}
	out := make([]int, len(stored))
	for i, p := range stored {
		out[i] = int(p)
	}
	return out
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func utcOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
