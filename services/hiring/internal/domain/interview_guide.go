package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidInterviewGuide means an interview guide would break a business rule.
var ErrInvalidInterviewGuide = errors.New("invalid interview guide")

// QuestionKind is what a question is for (FR-3.2). A guide is expected to cover all three, but
// which ones a particular candidate needs is the model's judgement, not a rule here.
type QuestionKind string

const (
	// ResumeClaim probes something the resume asserts.
	ResumeClaim QuestionKind = "resume-claim"
	// MustHaveGap addresses a must-have criterion the resume did not evidence.
	MustHaveGap QuestionKind = "must-have-gap"
	// RoleDepth is role-specific depth drawn from the criteria.
	RoleDepth QuestionKind = "role-depth"
)

// Question is one interview question, tagged with what it tests and where it came from, so the
// interviewer knows why it is being asked (FR-3.3).
type Question struct {
	Text string
	// Criterion is the name of the criterion this question tests, as the job opening spells it.
	Criterion string
	// Evidence is the resume claim or gap the question was derived from. A question about a
	// must-have the resume never mentions has none, which is itself the point of asking.
	Evidence string
	Kind     QuestionKind
}

// ModelAttribution identifies which model produced a guide, so one shown to a Recruiter months
// ago can still be accounted for (ADR-004). A guide the Recruiter edited by hand has none.
type ModelAttribution struct {
	Provider      string
	Model         string
	PromptVersion string
}

// GuideSource says where the current set of questions came from.
type GuideSource string

const (
	// Generated means the questions are the model's, unchanged since.
	Generated GuideSource = "generated"
	// Edited means a Recruiter added, changed or removed questions afterwards (FR-3.4).
	Edited GuideSource = "edited"
)

// InterviewGuide is the retained set of questions for one candidate against one job opening
// (FR-3.1). It belongs to the Hiring Service; the candidate's profile does not.
type InterviewGuide struct {
	ID           string
	JobOpeningID string
	CandidateID  string
	Questions    []Question
	// Emphasis is the steer the last generation was asked for, empty when none was given
	// (FR-3.4, UC-3 alternate flow 5a).
	Emphasis    string
	Source      GuideSource
	Attribution ModelAttribution
	// Revision counts how many times the guide has been generated or edited, starting at 1.
	Revision  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewInterviewGuide records the first generation of a guide.
func NewInterviewGuide(id, jobOpeningID, candidateID string, questions []Question, emphasis string, attribution ModelAttribution, at time.Time) (*InterviewGuide, error) {
	if strings.TrimSpace(jobOpeningID) == "" || strings.TrimSpace(candidateID) == "" {
		return nil, fmt.Errorf("%w: a guide belongs to one candidate and one job opening", ErrInvalidInterviewGuide)
	}
	if err := validateQuestions(questions); err != nil {
		return nil, err
	}
	return &InterviewGuide{
		ID:           id,
		JobOpeningID: jobOpeningID,
		CandidateID:  candidateID,
		Questions:    append([]Question(nil), questions...),
		Emphasis:     strings.TrimSpace(emphasis),
		Source:       Generated,
		Attribution:  attribution,
		Revision:     1,
		CreatedAt:    at,
		UpdatedAt:    at,
	}, nil
}

// Regenerate replaces every question with a newly generated set, optionally with an emphasis
// (FR-3.4). The guide keeps its id, so what was retained for this candidate stays one thing.
func (g *InterviewGuide) Regenerate(questions []Question, emphasis string, attribution ModelAttribution, at time.Time) error {
	if err := validateQuestions(questions); err != nil {
		return err
	}
	g.Questions = append([]Question(nil), questions...)
	g.Emphasis = strings.TrimSpace(emphasis)
	g.Source = Generated
	g.Attribution = attribution
	g.Revision++
	g.UpdatedAt = at
	return nil
}

// ReplaceQuestions takes the Recruiter's own set — questions added, reworded or removed
// (FR-3.4). The model attribution goes with the generated text it described: what is in the
// guide now is partly the Recruiter's, and claiming a model wrote it would be false.
func (g *InterviewGuide) ReplaceQuestions(questions []Question, at time.Time) error {
	if err := validateQuestions(questions); err != nil {
		return err
	}
	g.Questions = append([]Question(nil), questions...)
	g.Source = Edited
	g.Attribution = ModelAttribution{}
	g.Revision++
	g.UpdatedAt = at
	return nil
}

// validateQuestions enforces what every question must carry.
func validateQuestions(questions []Question) error {
	if len(questions) == 0 {
		return fmt.Errorf("%w: a guide with no questions is not a guide", ErrInvalidInterviewGuide)
	}
	for i, q := range questions {
		if strings.TrimSpace(q.Text) == "" {
			return fmt.Errorf("%w: question %d has no text", ErrInvalidInterviewGuide, i+1)
		}
		// FR-3.3: an untagged question leaves the interviewer guessing why it is being asked,
		// which is the thing this use case exists to fix.
		if strings.TrimSpace(q.Criterion) == "" {
			return fmt.Errorf("%w: question %d names no criterion", ErrInvalidInterviewGuide, i+1)
		}
		switch q.Kind {
		case ResumeClaim, MustHaveGap, RoleDepth:
		default:
			return fmt.Errorf("%w: question %d has unknown kind %q", ErrInvalidInterviewGuide, i+1, q.Kind)
		}
	}
	return nil
}

// CandidateProfile is the part of a candidate the Hiring Service reads to prepare an interview.
// Resume Processing owns it; Hiring reads it and never stores it (SOC table).
//
// It carries no name, contact detail or any other attribute excluded from scoring: the guide is
// built from what the candidate can be asked about, not from who they are.
type CandidateProfile struct {
	CandidateID string
	Skills      []string
	WorkHistory []WorkExperience
	Education   []Qualification
	// AdditionalText is resume text that did not fit the fields above.
	AdditionalText string
}

// WorkExperience is one position from a resume.
type WorkExperience struct {
	Title        string
	Organisation string
	Start        string // as it appears on the resume: a year, a month, a date
	End          string // empty while current
	Summary      string
}

// Qualification is one entry of a candidate's education.
type Qualification struct {
	Qualification string
	Institution   string
	Completed     string
}
