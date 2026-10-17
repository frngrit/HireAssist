package rabbitmq

import (
	"errors"
	"fmt"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

// The bodies of the screening handover, as services/resume-processing/MESSAGES.md defines them.
// That file is the contract between the two services: it changes there first, in one pull
// request both owners approve.

// ErrMalformed means a message does not follow the contract and can never be accepted.
var ErrMalformed = errors.New("malformed screening message")

// Requested is a screening.requested message: one resume to screen. It names where the file is
// and carries the criteria snapshot to judge it against (NFR-15) — never the file itself, and
// nothing about the candidate.
type Requested struct {
	BatchID       string      `json:"batch_id"`
	ResumeID      string      `json:"resume_id"`
	JobOpeningID  string      `json:"job_opening_id"`
	File          File        `json:"file"`
	Criteria      []Criterion `json:"criteria"`
	CorrelationID string      `json:"correlation_id"`
}

// File says where a resume is stored in object storage.
type File struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

// Criterion has the shape of the AI Service contract's Criterion, so Resume Processing can pass
// the snapshot to ScoreAgainstCriteria as it is.
type Criterion struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	MustHave bool   `json:"must_have"`
	Weight   int    `json:"weight"`
}

// The three outcomes a screening.completed message reports.
const (
	statusScored     = "scored"
	statusUnparsable = "unparsable"
	statusFailed     = "failed"
)

// Completed is a screening.completed message: Resume Processing's answer for one resume.
type Completed struct {
	BatchID          string   `json:"batch_id"`
	ResumeID         string   `json:"resume_id"`
	JobOpeningID     string   `json:"job_opening_id"`
	CandidateID      string   `json:"candidate_id"`
	Status           string   `json:"status"`
	Score            *int     `json:"score"`
	MeetsMustHaves   *bool    `json:"meets_must_haves"`
	UnmetMustHaveIDs []string `json:"unmet_must_have_ids"`
	// Justification arrives too, and is deliberately not kept: it is an AI-derived document that
	// stays with Resume Processing, read when a Recruiter opens the candidate (ADR-003).
	Justification *string `json:"justification"`
	// Error says why a resume is unparsable, or why screening failed.
	Error         *string `json:"error"`
	CorrelationID string  `json:"correlation_id"`
}

// NewRequested builds the request to screen one resume of a batch.
func NewRequested(bucket string, b *domain.ScreeningBatch, e *domain.ScreeningEntry, correlationID string) Requested {
	criteria := make([]Criterion, len(b.Criteria))
	for i, c := range b.Criteria {
		criteria[i] = Criterion{ID: domain.CriterionID(i), Text: c.Name, MustHave: c.Classification == domain.MustHave, Weight: c.Weight}
	}
	return Requested{
		BatchID: b.ID, ResumeID: e.ResumeID, JobOpeningID: b.JobOpeningID,
		File:          File{Bucket: bucket, Key: e.ObjectKey},
		Criteria:      criteria,
		CorrelationID: correlationID,
	}
}

// ToDomain reads an answer into the domain. Whether it is acceptable — a score on the scale,
// unmet must-haves the batch has — is the domain's to judge; this checks only that the message
// says what the contract requires.
func (m Completed) ToDomain() (domain.EntryStatus, domain.ScreeningResult, error) {
	if m.BatchID == "" || m.ResumeID == "" {
		return "", domain.ScreeningResult{}, fmt.Errorf("%w: batch_id and resume_id are required", ErrMalformed)
	}
	r := domain.ScreeningResult{CandidateID: m.CandidateID}
	switch m.Status {
	case statusUnparsable, statusFailed:
		if m.Error != nil {
			r.Reason = *m.Error
		}
		if m.Status == statusUnparsable {
			return domain.EntryNeedsReview, r, nil
		}
		return domain.EntryFailed, r, nil
	case statusScored:
	default:
		return "", r, fmt.Errorf("%w: unknown status %q", ErrMalformed, m.Status)
	}
	if m.Score == nil || m.MeetsMustHaves == nil {
		return "", r, fmt.Errorf("%w: a scored resume needs score and meets_must_haves", ErrMalformed)
	}
	r.Score, r.MeetsMustHaves = *m.Score, *m.MeetsMustHaves
	for _, id := range m.UnmetMustHaveIDs {
		pos, ok := domain.ParseCriterionID(id)
		if !ok {
			return "", r, fmt.Errorf("%w: criterion id %q is not one this service sent", ErrMalformed, id)
		}
		r.UnmetMustHaves = append(r.UnmetMustHaves, pos)
	}
	return domain.EntryScreened, r, nil
}
