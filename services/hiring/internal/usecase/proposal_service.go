package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

// ErrAIUnavailable means the AI Service could not answer. Proposing is the only job opening
// operation that needs it: the Recruiter can still enter criteria by hand and create the opening
// (FR-1.5, UC-1 flow 4a).
var ErrAIUnavailable = errors.New("the AI Service is unavailable; enter the criteria by hand instead")

// CriteriaDeriver turns a free-text role description into a proposal. Implemented by the AI
// Service adapter (ADR-004, ADR-007).
type CriteriaDeriver interface {
	DeriveCriteria(ctx context.Context, description string, language domain.Language) (*domain.JobOpeningProposal, error)
}

// ProposalService implements proposeJobOpening().
type ProposalService struct {
	deriver CriteriaDeriver
}

// NewProposalService wires the service to the AI Service adapter.
func NewProposalService(deriver CriteriaDeriver) *ProposalService {
	return &ProposalService{deriver: deriver}
}

// ProposeJobOpening derives a title and weighted criteria from a role description, in Thai or
// English, for the Recruiter to review (UC-1 steps 2–3, FR-1.1–FR-1.3). It stores nothing: an
// abandoned proposal leaves no trace, and the reviewed result is created by CreateJobOpening.
func (s *ProposalService) ProposeJobOpening(ctx context.Context, description string, language domain.Language) (*domain.JobOpeningProposal, error) {
	if strings.TrimSpace(description) == "" {
		return nil, fmt.Errorf("%w: a description is required to propose from", domain.ErrInvalidJobOpening)
	}
	if language != "" && language != domain.LanguageThai && language != domain.LanguageEnglish {
		return nil, fmt.Errorf("%w: language must be th or en", domain.ErrInvalidJobOpening)
	}
	return s.deriver.DeriveCriteria(ctx, description, language)
}
