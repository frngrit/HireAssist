package memory

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// ScreeningRepository keeps screening batches in memory. It is safe for concurrent use, and
// stores and returns copies so that a change reaches the store only through its methods.
type ScreeningRepository struct {
	mu      sync.RWMutex
	batches map[string]domain.ScreeningBatch
	order   []string
	batchOf map[string]string // resume id → batch id
}

// NewScreeningRepository returns an empty repository.
func NewScreeningRepository() *ScreeningRepository {
	return &ScreeningRepository{batches: map[string]domain.ScreeningBatch{}, batchOf: map[string]string{}}
}

// SaveBatch stores a new batch.
func (r *ScreeningRepository) SaveBatch(_ context.Context, b *domain.ScreeningBatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.batches[b.ID]; exists {
		return fmt.Errorf("screening batch %s already exists", b.ID)
	}
	r.batches[b.ID] = cloneBatch(*b)
	r.order = append(r.order, b.ID)
	for _, e := range b.Entries {
		r.batchOf[e.ResumeID] = b.ID
	}
	return nil
}

// FindBatch returns a copy of the batch, or usecase.ErrBatchNotFound.
func (r *ScreeningRepository) FindBatch(_ context.Context, id string) (*domain.ScreeningBatch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.batches[id]
	if !ok {
		return nil, usecase.ErrBatchNotFound
	}
	c := cloneBatch(b)
	return &c, nil
}

// FindBatchOfResume returns a copy of the batch holding the resume, or usecase.ErrResumeNotFound.
func (r *ScreeningRepository) FindBatchOfResume(ctx context.Context, resumeID string) (*domain.ScreeningBatch, error) {
	r.mu.RLock()
	id, ok := r.batchOf[resumeID]
	r.mu.RUnlock()
	if !ok {
		return nil, usecase.ErrResumeNotFound
	}
	return r.FindBatch(ctx, id)
}

// ListBatches returns copies of a job opening's batches in submission order.
func (r *ScreeningRepository) ListBatches(_ context.Context, jobOpeningID string) ([]domain.ScreeningBatch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []domain.ScreeningBatch{}
	for _, id := range r.order {
		if b := r.batches[id]; b.JobOpeningID == jobOpeningID {
			out = append(out, cloneBatch(b))
		}
	}
	return out, nil
}

// SaveEntry replaces the stored copy of one resume's entry.
func (r *ScreeningRepository) SaveEntry(_ context.Context, e *domain.ScreeningEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update(e.ResumeID, func(stored *domain.ScreeningEntry) { *stored = cloneEntry(*e) })
}

// Unrequested returns copies of the batches holding resumes not yet requested, oldest first,
// each with only those entries, at most limit in all.
func (r *ScreeningRepository) Unrequested(_ context.Context, limit int) ([]domain.ScreeningBatch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []domain.ScreeningBatch{}
	for _, id := range r.order {
		b := cloneBatch(r.batches[id])
		b.Entries = slices.DeleteFunc(b.Entries, func(e domain.ScreeningEntry) bool {
			return e.Status != domain.EntryPending || !e.RequestedAt.IsZero()
		})
		if len(b.Entries) > limit {
			b.Entries = b.Entries[:limit]
		}
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

// MarkRequested records when a resume's screening request was confirmed.
func (r *ScreeningRepository) MarkRequested(_ context.Context, resumeID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update(resumeID, func(stored *domain.ScreeningEntry) { stored.RequestedAt = at })
}

// update applies change to the stored entry of a resume. The caller holds the write lock.
func (r *ScreeningRepository) update(resumeID string, change func(*domain.ScreeningEntry)) error {
	b, ok := r.batches[r.batchOf[resumeID]]
	if !ok {
		return usecase.ErrResumeNotFound
	}
	for i := range b.Entries {
		if b.Entries[i].ResumeID == resumeID {
			change(&b.Entries[i])
			return nil
		}
	}
	return usecase.ErrResumeNotFound
}

// cloneBatch copies a batch deeply enough that the copy shares nothing mutable with it.
func cloneBatch(b domain.ScreeningBatch) domain.ScreeningBatch {
	b.Criteria = slices.Clone(b.Criteria)
	entries := make([]domain.ScreeningEntry, len(b.Entries))
	for i, e := range b.Entries {
		entries[i] = cloneEntry(e)
	}
	b.Entries = entries
	return b
}

func cloneEntry(e domain.ScreeningEntry) domain.ScreeningEntry {
	if e.Result != nil {
		r := *e.Result
		r.UnmetMustHaves = slices.Clone(r.UnmetMustHaves)
		e.Result = &r
	}
	if e.Override != nil {
		o := *e.Override
		e.Override = &o
	}
	return e
}
