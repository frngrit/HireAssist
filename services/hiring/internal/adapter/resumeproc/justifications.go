package resumeproc

import (
	"context"
	"errors"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

// ErrNoScreening means Resume Processing holds the candidate but no screening of this resume.
var ErrNoScreening = errors.New("the resume processing service has no screening of this resume")

// screeningJSON is one screening result in a candidate's document, as that service's OpenAPI
// document describes it.
type screeningJSON struct {
	BatchID       string           `json:"batch_id"`
	ResumeID      string           `json:"resume_id"`
	Assessments   []assessmentJSON `json:"assessments"`
	Justification *string          `json:"justification"`
}

type assessmentJSON struct {
	CriterionID string `json:"criterion_id"`
	Met         bool   `json:"met"`
	Score       int    `json:"score"`
	Evidence    string `json:"evidence"`
}

// GetJustification is `getCandidateProfile()` read for UC-2: why one resume of one batch was
// scored as it was — the written justification, and the evidence behind each verdict (FR-2.5).
// Hiring shows it when a Recruiter opens the candidate and stores no copy (ADR-003).
func (c *CandidateProfiles) GetJustification(ctx context.Context, candidateID, batchID, resumeID string) (domain.Justification, error) {
	body, err := c.fetch(ctx, candidateID)
	if err != nil {
		return domain.Justification{}, err
	}
	for _, s := range body.Screenings {
		if s.BatchID != batchID || s.ResumeID != resumeID {
			continue
		}
		j := domain.Justification{}
		if s.Justification != nil {
			j.Text = *s.Justification
		}
		for _, a := range s.Assessments {
			pos, ok := domain.ParseCriterionID(a.CriterionID)
			if !ok {
				continue // a verdict on a criterion this service never sent cannot be shown against one
			}
			j.Evidence = append(j.Evidence, domain.CriterionEvidence{Criterion: pos, Met: a.Met, Score: a.Score, Evidence: a.Evidence})
		}
		return j, nil
	}
	return domain.Justification{}, ErrNoScreening
}
