package domain

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidScreening means a batch, or a screening result, would break a business rule.
	ErrInvalidScreening = errors.New("invalid screening")
	// ErrAlreadyCompleted means the resume already has a result. Results arrive through a broker
	// that delivers at least once, so a repeat is expected rather than a fault.
	ErrAlreadyCompleted = errors.New("screening already completed")
)

// MaxScore is the top of the scale screening scores on; the bottom is 0.
const MaxScore = 100

// EntryStatus is where one resume of a screening batch stands.
type EntryStatus string

const (
	// EntryPending: handed to screening, no answer yet.
	EntryPending EntryStatus = "pending"
	// EntryScreened: scored, and so ranked.
	EntryScreened EntryStatus = "screened"
	// EntryNeedsReview: could not be parsed or scored — a scanned image, a corrupt file. It is
	// left out of the ranking rather than given a score (FR-2.7).
	EntryNeedsReview EntryStatus = "needs_review"
	// EntryFailed: screening gave up once its retries ran out (FR-2.7).
	EntryFailed EntryStatus = "failed"
)

// AcceptsResumes reports whether resumes may be screened against the job opening. Only an open
// one takes them: a paused opening is on hold, and a closed one is final (FR-1.9).
func (j *JobOpening) AcceptsResumes() error {
	if j.Status != StatusOpen {
		return fmt.Errorf("%w: cannot screen resumes against a %s job opening", ErrInvalidTransition, j.Status)
	}
	return nil
}

// NewEntry describes one resume joining a batch.
type NewEntry struct {
	ResumeID  string
	FileName  string // as uploaded; shown to the Recruiter
	ObjectKey string // where the file is stored
	SizeBytes int64
}

// ScreeningBatch is a set of resumes submitted together against one job opening (UC-2).
type ScreeningBatch struct {
	ID           string
	JobOpeningID string
	SubmittedAt  time.Time
	// Criteria is the snapshot taken at submission. Every resume in the batch is judged against
	// these, even if the opening's criteria are revised while it runs (NFR-15).
	Criteria []Criterion
	Entries  []ScreeningEntry
}

// ScreeningEntry is one resume in a batch, and everything decided about it.
type ScreeningEntry struct {
	ResumeID  string
	BatchID   string
	FileName  string
	ObjectKey string
	SizeBytes int64
	Status    EntryStatus
	// RequestedAt is when the broker confirmed the request to screen this resume. Zero means the
	// request is still waiting to be sent.
	RequestedAt   time.Time
	Result        *ScreeningResult // nil while pending
	Override      *ScoreOverride   // the Recruiter's correction, if any (FR-2.8)
	ShortlistedAt time.Time        // zero unless shortlisted (FR-2.9)
}

// ScreeningResult is what screening concluded about one resume (FR-2.5): the score, the
// must-have check, and which must-haves were unmet. The written justification and the evidence
// behind each verdict are not part of it: they are AI-derived documents Resume Processing holds,
// read when the Recruiter opens the candidate (ADR-003).
type ScreeningResult struct {
	CandidateID    string // assigned by Resume Processing, which owns candidate identity
	Score          int    // 0–100; screened resumes only
	MeetsMustHaves bool
	UnmetMustHaves []int  // positions, in the batch's snapshot, of the must-haves judged unmet
	Reason         string // why the resume needs review, or failed
	CompletedAt    time.Time
}

// Justification is why screening scored a resume as it did (FR-2.5): the written reasoning and
// the evidence behind each criterion's verdict. Resume Processing holds it; Hiring reads it when
// a Recruiter opens the candidate, and keeps no copy (ADR-003).
type Justification struct {
	Text     string
	Evidence []CriterionEvidence
}

// CriterionEvidence is screening's verdict on one criterion of the snapshot, and the words in
// the resume it rests on.
type CriterionEvidence struct {
	Criterion int // position in the batch's criteria snapshot
	Met       bool
	Score     int
	Evidence  string
}

// ScoreOverride is a Recruiter's correction of a screening score. The original stays in the
// result beside it, so screening quality can be reviewed later (FR-2.8).
type ScoreOverride struct {
	Score int
	At    time.Time
}

// NewScreeningBatch accepts resumes for screening against a job opening (UC-2 steps 1–2,
// FR-2.1), taking the snapshot of its criteria that the batch will be judged against.
func NewScreeningBatch(id string, opening *JobOpening, entries []NewEntry, submittedAt time.Time) (*ScreeningBatch, error) {
	if err := opening.AcceptsResumes(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: a batch needs at least one resume", ErrInvalidScreening)
	}
	b := &ScreeningBatch{
		ID:           id,
		JobOpeningID: opening.ID,
		SubmittedAt:  submittedAt,
		Criteria:     append([]Criterion(nil), opening.Criteria...),
		Entries:      make([]ScreeningEntry, len(entries)),
	}
	for i, e := range entries {
		b.Entries[i] = ScreeningEntry{
			ResumeID:  e.ResumeID,
			BatchID:   id,
			FileName:  e.FileName,
			ObjectKey: e.ObjectKey,
			SizeBytes: e.SizeBytes,
			Status:    EntryPending,
		}
	}
	return b, nil
}

// Entry returns the batch's entry for a resume, or nil.
func (b *ScreeningBatch) Entry(resumeID string) *ScreeningEntry {
	for i := range b.Entries {
		if b.Entries[i].ResumeID == resumeID {
			return &b.Entries[i]
		}
	}
	return nil
}

// Complete reports whether every resume in the batch has a final answer.
func (b *ScreeningBatch) Complete() bool {
	for _, e := range b.Entries {
		if e.Status == EntryPending {
			return false
		}
	}
	return true
}

// RecordResult stores screening's answer for one pending resume (FR-2.5, FR-2.7). A second
// answer for the same resume is refused with ErrAlreadyCompleted: the first stands, and nothing
// the Recruiter has done since — an override, a shortlist — is overwritten.
func (b *ScreeningBatch) RecordResult(resumeID string, status EntryStatus, r ScreeningResult) error {
	e := b.Entry(resumeID)
	if e == nil {
		return fmt.Errorf("%w: resume %s is not in batch %s", ErrInvalidScreening, resumeID, b.ID)
	}
	if e.Status != EntryPending {
		return fmt.Errorf("%w: resume %s is already %s", ErrAlreadyCompleted, resumeID, e.Status)
	}
	switch status {
	case EntryScreened:
		if err := b.checkScreened(r); err != nil {
			return err
		}
		r.Reason = ""
	case EntryNeedsReview, EntryFailed:
		r = ScreeningResult{CandidateID: r.CandidateID, Reason: strings.TrimSpace(r.Reason), CompletedAt: r.CompletedAt}
	default:
		return fmt.Errorf("%w: unknown screening outcome %q", ErrInvalidScreening, status)
	}
	r.UnmetMustHaves = slices.Clone(r.UnmetMustHaves)
	e.Status, e.Result = status, &r
	return nil
}

// checkScreened enforces FR-2.5 on a scored result: a score on the scale, and — exactly when
// the candidate is not qualified — the must-haves that caused it, each a must-have the batch was
// judged against.
func (b *ScreeningBatch) checkScreened(r ScreeningResult) error {
	if r.CandidateID == "" {
		return fmt.Errorf("%w: a screened resume needs the candidate id Resume Processing assigned", ErrInvalidScreening)
	}
	if r.Score < 0 || r.Score > MaxScore {
		return fmt.Errorf("%w: score %d is outside 0–%d", ErrInvalidScreening, r.Score, MaxScore)
	}
	seen := map[int]bool{}
	for _, pos := range r.UnmetMustHaves {
		if pos < 0 || pos >= len(b.Criteria) || b.Criteria[pos].Classification != MustHave {
			return fmt.Errorf("%w: criterion %d is not a must-have of batch %s", ErrInvalidScreening, pos, b.ID)
		}
		if seen[pos] {
			return fmt.Errorf("%w: must-have %d listed twice", ErrInvalidScreening, pos)
		}
		seen[pos] = true
	}
	if r.MeetsMustHaves && len(r.UnmetMustHaves) > 0 {
		return fmt.Errorf("%w: marked as meeting every must-have, yet a must-have is unmet", ErrInvalidScreening)
	}
	if !r.MeetsMustHaves && len(r.UnmetMustHaves) == 0 {
		return fmt.Errorf("%w: marked not qualified without the must-have that caused it", ErrInvalidScreening)
	}
	return nil
}

// UnmetMustHaves names the must-have criteria a screened resume was judged not to meet — why it
// is marked not qualified (FR-2.5).
func (b *ScreeningBatch) UnmetMustHaves(e *ScreeningEntry) []string {
	names := []string{}
	if e.Result == nil {
		return names
	}
	for _, pos := range e.Result.UnmetMustHaves {
		names = append(names, b.Criteria[pos].Name)
	}
	return names
}

// CriterionID names a criterion of a batch's snapshot the way the screening messages carry it —
// c1, c2, … by position — so a verdict that comes back can be matched to what it judges.
func CriterionID(position int) string { return "c" + strconv.Itoa(position+1) }

// ParseCriterionID reads an id made by CriterionID back into a position.
func ParseCriterionID(id string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "c"))
	if !strings.HasPrefix(id, "c") || err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}

// CriterionPosition finds the batch's criterion of this name, in any case.
func (b *ScreeningBatch) CriterionPosition(name string) (int, bool) {
	for i, c := range b.Criteria {
		if strings.EqualFold(c.Name, strings.TrimSpace(name)) {
			return i, true
		}
	}
	return 0, false
}

// Meets reports whether a screened resume was judged to meet the batch's must-have of this name:
// it does unless screening named it unmet. Screening reports must-haves one by one and the
// nice-to-haves only through the score, so a nice-to-have, or a name the batch does not have,
// never matches.
func (b *ScreeningBatch) Meets(e *ScreeningEntry, criterion string) bool {
	pos, ok := b.CriterionPosition(criterion)
	if !ok || e.Result == nil || b.Criteria[pos].Classification != MustHave {
		return false
	}
	return !slices.Contains(e.Result.UnmetMustHaves, pos)
}

// Score is the score the ranking uses: the Recruiter's override when there is one, otherwise
// screening's own.
func (e *ScreeningEntry) Score() int {
	if e.Override != nil {
		return e.Override.Score
	}
	if e.Result != nil {
		return e.Result.Score
	}
	return 0
}

// OverrideScore replaces a screened resume's score with the Recruiter's own, keeping the
// original beside it (FR-2.8).
func (e *ScreeningEntry) OverrideScore(score int, at time.Time) error {
	if e.Status != EntryScreened {
		return fmt.Errorf("%w: a %s resume has no score to override", ErrInvalidTransition, e.Status)
	}
	if score < 0 || score > MaxScore {
		return fmt.Errorf("%w: score %d is outside 0–%d", ErrInvalidScreening, score, MaxScore)
	}
	e.Override = &ScoreOverride{Score: score, At: at}
	return nil
}

// Shortlist marks a screened candidate as worth interviewing (FR-2.9). Nothing rejects: a
// candidate neither shortlisted nor marked not qualified simply stays undecided.
func (e *ScreeningEntry) Shortlist(at time.Time) error {
	if e.Status != EntryScreened {
		return fmt.Errorf("%w: only a screened candidate can be shortlisted, and this resume is %s", ErrInvalidTransition, e.Status)
	}
	if !e.ShortlistedAt.IsZero() {
		return fmt.Errorf("%w: the candidate is already shortlisted", ErrInvalidTransition)
	}
	e.ShortlistedAt = at
	return nil
}

// Ranked is one resume in the ranked list, with the batch it was judged in.
type Ranked struct {
	Batch *ScreeningBatch
	Entry *ScreeningEntry
}

// RankFilter narrows the ranked list (FR-2.6). The zero value keeps everything.
type RankFilter struct {
	MinScore  int
	Criterion string // keep only resumes judged to meet the must-have of this name
}

// Rank orders the screened resumes of the batches for the Recruiter (FR-2.6). Those who meet
// every must-have come first — a must-have is a gate, not a weight — then the higher score, the
// Recruiter's override standing in for the original, then the earlier answer. Resumes still
// pending, needing review or failed are not ranked (FR-2.7).
func Rank(batches []ScreeningBatch, f RankFilter) []Ranked {
	ranked := []Ranked{}
	for bi := range batches {
		b := &batches[bi]
		for ei := range b.Entries {
			e := &b.Entries[ei]
			if e.Status != EntryScreened || e.Score() < f.MinScore {
				continue
			}
			if f.Criterion != "" && !b.Meets(e, f.Criterion) {
				continue
			}
			ranked = append(ranked, Ranked{Batch: b, Entry: e})
		}
	}
	slices.SortStableFunc(ranked, func(x, y Ranked) int {
		if xq, yq := x.Entry.Result.MeetsMustHaves, y.Entry.Result.MeetsMustHaves; xq != yq {
			if xq {
				return -1
			}
			return 1
		}
		if d := y.Entry.Score() - x.Entry.Score(); d != 0 {
			return d
		}
		return x.Entry.Result.CompletedAt.Compare(y.Entry.Result.CompletedAt)
	})
	return ranked
}
