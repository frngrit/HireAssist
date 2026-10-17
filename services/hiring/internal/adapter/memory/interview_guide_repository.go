package memory

import (
	"context"
	"sync"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// InterviewGuideRepository keeps interview guides in memory, for local runs and tests. The real
// one is MongoDB (ADR-003).
type InterviewGuideRepository struct {
	mu     sync.RWMutex
	byPair map[guideKey]domain.InterviewGuide
}

// guideKey is what identifies a guide to a reader: one candidate against one job opening.
type guideKey struct{ jobOpeningID, candidateID string }

// NewInterviewGuideRepository returns an empty repository.
func NewInterviewGuideRepository() *InterviewGuideRepository {
	return &InterviewGuideRepository{byPair: map[guideKey]domain.InterviewGuide{}}
}

// Save inserts a guide or replaces the one for the same candidate and opening.
func (r *InterviewGuideRepository) Save(_ context.Context, g *domain.InterviewGuide) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byPair[guideKey{g.JobOpeningID, g.CandidateID}] = cloneGuide(*g)
	return nil
}

// FindByCandidate returns a copy of the guide, or usecase.ErrGuideNotFound.
func (r *InterviewGuideRepository) FindByCandidate(_ context.Context, jobOpeningID, candidateID string) (*domain.InterviewGuide, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.byPair[guideKey{jobOpeningID, candidateID}]
	if !ok {
		return nil, usecase.ErrGuideNotFound
	}
	c := cloneGuide(g)
	return &c, nil
}

func cloneGuide(g domain.InterviewGuide) domain.InterviewGuide {
	g.Questions = append([]domain.Question(nil), g.Questions...)
	return g
}
