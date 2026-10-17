package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

// What can go wrong that is not the database's fault.
var (
	// ErrGuideNotFound means no guide has been generated for this candidate and opening yet.
	ErrGuideNotFound = errors.New("interview guide not found")
	// ErrGuideExists means one already has been. Regenerating is a revision, not a second guide.
	ErrGuideExists = errors.New("an interview guide already exists for this candidate; revise it instead")
	// ErrCandidateNotFound means Resume Processing has no profile under that candidate id.
	ErrCandidateNotFound = errors.New("candidate profile not found")
	// ErrUpstreamUnavailable means a service this one needs could not be reached or failed.
	// UC-3 cannot be served without both of them, and saying so beats a blank guide.
	ErrUpstreamUnavailable = errors.New("a service needed to prepare the guide is unavailable")
)

// InterviewGuideRepository stores interview guides. They are AI-derived documents, so ADR-003
// puts them in MongoDB rather than in the relational system of record.
type InterviewGuideRepository interface {
	Save(ctx context.Context, guide *domain.InterviewGuide) error
	// FindByCandidate returns the guide for one candidate against one opening, or
	// ErrGuideNotFound. A candidate has at most one guide per opening; regenerating revises it.
	FindByCandidate(ctx context.Context, jobOpeningID, candidateID string) (*domain.InterviewGuide, error)
}

// CandidateProfiles reads a candidate's profile from the Resume Processing Service, which owns
// it. Hiring reads it at the moment it needs it and stores no copy (SOC table).
type CandidateProfiles interface {
	GetCandidateProfile(ctx context.Context, candidateID string) (domain.CandidateProfile, error)
}

// QuestionGenerator asks the AI Service for interview questions. It is the only model call in
// this use case, and it is reached over gRPC (ADR-007).
type QuestionGenerator interface {
	GenerateInterviewQuestions(ctx context.Context, profile domain.CandidateProfile, criteria []domain.Criterion, emphasis string) ([]domain.Question, domain.ModelAttribution, error)
}

// InterviewGuideService implements UC-3: generating, revising and retrieving the guide for one
// candidate against one job opening.
type InterviewGuideService struct {
	guides    InterviewGuideRepository
	openings  JobOpeningRepository
	profiles  CandidateProfiles
	questions QuestionGenerator
	clock     Clock
	ids       IDGenerator
}

// NewInterviewGuideService wires the use case to its ports.
func NewInterviewGuideService(
	guides InterviewGuideRepository,
	openings JobOpeningRepository,
	profiles CandidateProfiles,
	questions QuestionGenerator,
	clock Clock,
	ids IDGenerator,
) *InterviewGuideService {
	return &InterviewGuideService{guides: guides, openings: openings, profiles: profiles, questions: questions, clock: clock, ids: ids}
}

// GenerateInterviewGuide prepares the first guide for a candidate (FR-3.1): it reads the job
// opening's criteria, reads the candidate's profile from Resume Processing, asks the AI Service
// for questions covering resume claims, must-have gaps and role depth, and retains the result.
//
// A guide that already exists is not replaced silently — ErrGuideExists points at revising,
// which keeps the Recruiter's edits from disappearing behind a stray second click.
func (s *InterviewGuideService) GenerateInterviewGuide(ctx context.Context, jobOpeningID, candidateID, emphasis string) (*domain.InterviewGuide, error) {
	opening, err := s.openings.FindByID(ctx, jobOpeningID)
	if err != nil {
		return nil, err
	}
	switch _, err := s.guides.FindByCandidate(ctx, jobOpeningID, candidateID); {
	case err == nil:
		return nil, ErrGuideExists
	case !errors.Is(err, ErrGuideNotFound):
		return nil, err
	}

	questions, attribution, err := s.ask(ctx, opening, candidateID, emphasis)
	if err != nil {
		return nil, err
	}
	guide, err := domain.NewInterviewGuide(s.ids.NewID(), jobOpeningID, candidateID, questions, emphasis, attribution, s.clock.Now())
	if err != nil {
		return nil, err
	}
	if err := s.guides.Save(ctx, guide); err != nil {
		return nil, fmt.Errorf("saving interview guide: %w", err)
	}
	return guide, nil
}

// GetInterviewGuide returns the retained guide, so it can be reopened before the interview
// rather than generated again (FR-3.1).
func (s *InterviewGuideService) GetInterviewGuide(ctx context.Context, jobOpeningID, candidateID string) (*domain.InterviewGuide, error) {
	return s.guides.FindByCandidate(ctx, jobOpeningID, candidateID)
}

// RegenerateInterviewGuide asks for a new set of questions, optionally with an emphasis such as
// "more system design" (FR-3.4, UC-3 alternate flow 5a).
func (s *InterviewGuideService) RegenerateInterviewGuide(ctx context.Context, jobOpeningID, candidateID, emphasis string) (*domain.InterviewGuide, error) {
	opening, err := s.openings.FindByID(ctx, jobOpeningID)
	if err != nil {
		return nil, err
	}
	guide, err := s.guides.FindByCandidate(ctx, jobOpeningID, candidateID)
	if err != nil {
		return nil, err
	}
	questions, attribution, err := s.ask(ctx, opening, candidateID, emphasis)
	if err != nil {
		return nil, err
	}
	if err := guide.Regenerate(questions, emphasis, attribution, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.guides.Save(ctx, guide); err != nil {
		return nil, fmt.Errorf("saving interview guide: %w", err)
	}
	return guide, nil
}

// RecordInterviewNote is `recordInterviewNote()` from the SOC table: what the interviewer
// wrote against one question, kept with the question so the evidence stays attached to the
// criterion it was meant to test (FR-3.5).
func (s *InterviewGuideService) RecordInterviewNote(ctx context.Context, jobOpeningID, candidateID, questionID, note string) (*domain.InterviewGuide, error) {
	guide, err := s.guides.FindByCandidate(ctx, jobOpeningID, candidateID)
	if err != nil {
		return nil, err
	}
	if err := guide.RecordNote(questionID, note, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.guides.Save(ctx, guide); err != nil {
		return nil, fmt.Errorf("saving interview guide: %w", err)
	}
	return guide, nil
}

// ReviseGuideQuestions stores the Recruiter's own set of questions — added, reworded, reordered
// or removed (FR-3.4). No model is called: these questions are theirs.
func (s *InterviewGuideService) ReviseGuideQuestions(ctx context.Context, jobOpeningID, candidateID string, questions []domain.Question) (*domain.InterviewGuide, error) {
	guide, err := s.guides.FindByCandidate(ctx, jobOpeningID, candidateID)
	if err != nil {
		return nil, err
	}
	if err := guide.ReplaceQuestions(questions, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.guides.Save(ctx, guide); err != nil {
		return nil, fmt.Errorf("saving interview guide: %w", err)
	}
	return guide, nil
}

// ask reads the candidate's profile and turns it, with the opening's criteria, into questions.
// Both calls leave this service: a failure in either is reported as such rather than hidden
// behind an empty guide.
func (s *InterviewGuideService) ask(ctx context.Context, opening *domain.JobOpening, candidateID, emphasis string) ([]domain.Question, domain.ModelAttribution, error) {
	profile, err := s.profiles.GetCandidateProfile(ctx, candidateID)
	if err != nil {
		if errors.Is(err, ErrCandidateNotFound) {
			return nil, domain.ModelAttribution{}, err
		}
		return nil, domain.ModelAttribution{}, fmt.Errorf("%w: reading the candidate profile: %w", ErrUpstreamUnavailable, err)
	}
	questions, attribution, err := s.questions.GenerateInterviewQuestions(ctx, profile, opening.Criteria, emphasis)
	if err != nil {
		return nil, domain.ModelAttribution{}, fmt.Errorf("%w: generating the questions: %w", ErrUpstreamUnavailable, err)
	}
	return questions, attribution, nil
}
