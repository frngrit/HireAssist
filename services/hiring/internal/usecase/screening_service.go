package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

var (
	// ErrBatchNotFound means the job opening has no screening batch with that id.
	ErrBatchNotFound = errors.New("screening batch not found")
	// ErrResumeNotFound means the job opening has no resume with that id.
	ErrResumeNotFound = errors.New("resume not found")
	// ErrStorageUnavailable means the resume files could not be stored, so the batch was not
	// accepted. Nothing of it was recorded; the Recruiter can submit it again.
	ErrStorageUnavailable = errors.New("resume storage is unavailable; submit the batch again shortly")
)

const (
	// MaxResumeBytes bounds one resume file.
	MaxResumeBytes = 10 << 20
	// MaxResumesPerBatch bounds one upload. The proposal expects 20–200 applications per
	// opening, so a large opening arrives in a few batches.
	MaxResumesPerBatch = 100
	// publishLimit bounds how many requests one publishing pass sends.
	publishLimit = 500
)

// ScreeningRepository stores screening batches. Implemented by an adapter.
type ScreeningRepository interface {
	// SaveBatch stores a new batch: its criteria snapshot and its entries.
	SaveBatch(ctx context.Context, b *domain.ScreeningBatch) error
	// FindBatch returns a batch with every entry, or ErrBatchNotFound.
	FindBatch(ctx context.Context, id string) (*domain.ScreeningBatch, error)
	// FindBatchOfResume returns the batch a resume belongs to, or ErrResumeNotFound.
	FindBatchOfResume(ctx context.Context, resumeID string) (*domain.ScreeningBatch, error)
	// ListBatches returns a job opening's batches in submission order.
	ListBatches(ctx context.Context, jobOpeningID string) ([]domain.ScreeningBatch, error)
	// SaveEntry stores what has changed about one resume: its status and result, the
	// Recruiter's override and shortlist.
	SaveEntry(ctx context.Context, e *domain.ScreeningEntry) error
	// Unrequested returns, oldest first, batches holding resumes whose screening request has not
	// been sent — each batch carrying only those entries, at most limit in all.
	Unrequested(ctx context.Context, limit int) ([]domain.ScreeningBatch, error)
	// MarkRequested records that the broker confirmed a resume's screening request.
	MarkRequested(ctx context.Context, resumeID string, at time.Time) error
}

// ResumeStore keeps uploaded resume files in object storage (ADR-003). Implemented by an adapter.
type ResumeStore interface {
	StoreResume(ctx context.Context, key string, content []byte) error
}

// ScreeningRequester hands one resume to Resume Processing to be screened, by publishing a
// screening request to the message broker. It returns only once the broker has taken it.
type ScreeningRequester interface {
	RequestScreening(ctx context.Context, b *domain.ScreeningBatch, e *domain.ScreeningEntry) error
}

// Justifications reads, from Resume Processing, why one resume was scored as it was: the written
// justification and the evidence behind each verdict (getCandidateProfile(), FR-2.5).
type Justifications interface {
	GetJustification(ctx context.Context, candidateID, batchID, resumeID string) (domain.Justification, error)
}

// ScreeningIDs issues identifiers for batches and the resumes in them.
type ScreeningIDs interface {
	NewBatchID() string
	NewResumeID() string
}

// UploadedFile is one file as the Recruiter uploaded it.
type UploadedFile struct {
	Name    string
	Content []byte
}

// RejectedFile is an uploaded file left out of the batch, and why (UC-2 step 2).
type RejectedFile struct {
	Name   string
	Reason string
}

// Results is the screening picture of one job opening (FR-2.6, FR-2.7).
type Results struct {
	Ranked      []domain.Ranked // screened, best first
	NeedsReview []domain.Ranked // could not be screened, in submission order
	Pending     int             // still being screened; the ranking fills in as they finish
}

// ScreenedCandidate is one resume as the Recruiter opens it: Hiring's record of it, and the
// justification for its score, read from Resume Processing.
type ScreenedCandidate struct {
	domain.Ranked
	// Justification is nil until the resume is screened, or when Resume Processing did not
	// answer. The ranking never waits on it (NFR-03); only opening one candidate does.
	Justification *domain.Justification
}

// ScreeningDeps is what the screening use cases depend on.
type ScreeningDeps struct {
	Openings  JobOpeningRepository
	Batches   ScreeningRepository
	Files     ResumeStore
	Requester ScreeningRequester
	// Justifications is optional: without it a candidate opens without the justification.
	Justifications Justifications
	Clock          Clock
	IDs            ScreeningIDs
}

// ScreeningService implements the screening operations of the Hiring Service (UC-2).
type ScreeningService struct {
	deps ScreeningDeps
	// publishing lets one pass publish at a time, so two passes never send the same resume.
	publishing sync.Mutex
}

// NewScreeningService wires the service to its dependencies.
func NewScreeningService(deps ScreeningDeps) *ScreeningService {
	return &ScreeningService{deps: deps}
}

// SubmitScreeningBatch accepts a batch of resumes against an open job opening and returns at
// once with the batch, without waiting for any of them to be screened (UC-2 steps 1–2, FR-2.1,
// FR-2.2). Files that are not PDFs are left out and reported; the rest are stored, recorded as
// pending, and handed to screening.
func (s *ScreeningService) SubmitScreeningBatch(ctx context.Context, openingID string, files []UploadedFile) (*domain.ScreeningBatch, []RejectedFile, error) {
	opening, err := s.deps.Openings.FindByID(ctx, openingID)
	if err != nil {
		return nil, nil, err
	}
	if err := opening.AcceptsResumes(); err != nil {
		return nil, nil, err
	}
	if len(files) > MaxResumesPerBatch {
		return nil, nil, fmt.Errorf("%w: at most %d resumes per batch; split the upload", domain.ErrInvalidScreening, MaxResumesPerBatch)
	}

	batchID := s.deps.IDs.NewBatchID()
	entries := []domain.NewEntry{}
	rejected := []RejectedFile{}
	for _, f := range files {
		name := cleanFileName(f.Name)
		if reason := rejectReason(f); reason != "" {
			rejected = append(rejected, RejectedFile{Name: name, Reason: reason})
			continue
		}
		resumeID := s.deps.IDs.NewResumeID()
		key := opening.ID + "/" + batchID + "/" + resumeID + ".pdf"
		// Files first, rows second: a recorded resume always has its file.
		if err := s.deps.Files.StoreResume(ctx, key, f.Content); err != nil {
			return nil, nil, fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
		}
		entries = append(entries, domain.NewEntry{ResumeID: resumeID, FileName: name, ObjectKey: key, SizeBytes: int64(len(f.Content))})
	}
	if len(entries) == 0 && len(rejected) > 0 {
		return nil, nil, fmt.Errorf("%w: no file can be screened — %s", domain.ErrInvalidScreening, describe(rejected))
	}

	batch, err := domain.NewScreeningBatch(batchID, opening, entries, s.deps.Clock.Now())
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Batches.SaveBatch(ctx, batch); err != nil {
		return nil, nil, fmt.Errorf("saving screening batch: %w", err)
	}
	// The batch is accepted once it is saved. Sending the requests can fail without losing it:
	// they stay unrequested, and the relay sends them on its next pass.
	if _, err := s.PublishPendingRequests(ctx); err != nil {
		slog.Warn("screening requests held back; the relay will send them", "batch", batch.ID, "err", err)
	}
	return batch, rejected, nil
}

// PublishPendingRequests hands every resume still waiting to Resume Processing, oldest first,
// and returns how many it sent. It is the only way requests leave the service: submitting a
// batch calls it, and so does the relay on a timer, which is what delivers the requests a broker
// outage held back. A resume is marked requested only after the broker confirms it, so a crash
// in between sends it again — at least once, never zero times (NFR-10); Resume Processing
// ignores the repeat.
func (s *ScreeningService) PublishPendingRequests(ctx context.Context) (int, error) {
	s.publishing.Lock()
	defer s.publishing.Unlock()

	batches, err := s.deps.Batches.Unrequested(ctx, publishLimit)
	if err != nil {
		return 0, fmt.Errorf("finding unrequested resumes: %w", err)
	}
	sent := 0
	for bi := range batches {
		b := &batches[bi]
		for ei := range b.Entries {
			e := &b.Entries[ei]
			if err := s.deps.Requester.RequestScreening(ctx, b, e); err != nil {
				return sent, fmt.Errorf("requesting screening of %s: %w", e.ResumeID, err)
			}
			if err := s.deps.Batches.MarkRequested(ctx, e.ResumeID, s.deps.Clock.Now()); err != nil {
				return sent, fmt.Errorf("marking %s requested: %w", e.ResumeID, err)
			}
			sent++
		}
	}
	return sent, nil
}

// RecordScreeningResult stores what Resume Processing concluded about one resume (FR-2.5,
// FR-2.7) — the consuming side of the screening handover, replacing reportScreeningResult().
// A repeat of an answer already recorded returns domain.ErrAlreadyCompleted and changes nothing.
func (s *ScreeningService) RecordScreeningResult(ctx context.Context, batchID, resumeID string, status domain.EntryStatus, r domain.ScreeningResult) error {
	b, err := s.deps.Batches.FindBatchOfResume(ctx, resumeID)
	if err != nil {
		return err
	}
	if b.ID != batchID {
		return fmt.Errorf("%w: resume %s belongs to batch %s, not %s", domain.ErrInvalidScreening, resumeID, b.ID, batchID)
	}
	// Recorded at arrival, by this service's clock, which also orders ties in the ranking.
	r.CompletedAt = s.deps.Clock.Now()
	if err := b.RecordResult(resumeID, status, r); err != nil {
		return err
	}
	if err := s.deps.Batches.SaveEntry(ctx, b.Entry(resumeID)); err != nil {
		return fmt.Errorf("saving the result of %s: %w", resumeID, err)
	}
	return nil
}

// GetBatchStatus returns a batch with where each of its resumes stands (getBatchStatus()).
func (s *ScreeningService) GetBatchStatus(ctx context.Context, openingID, batchID string) (*domain.ScreeningBatch, error) {
	b, err := s.deps.Batches.FindBatch(ctx, batchID)
	if err != nil {
		return nil, err
	}
	if b.JobOpeningID != openingID {
		return nil, ErrBatchNotFound
	}
	return b, nil
}

// GetRankedResults ranks the screened resumes of a job opening across all its batches, and
// lists those that need manual review (getRankedResults(), FR-2.6, FR-2.7). It reads only this
// service's own records, so it never waits on another service (NFR-03).
func (s *ScreeningService) GetRankedResults(ctx context.Context, openingID string, f domain.RankFilter) (*Results, error) {
	if _, err := s.deps.Openings.FindByID(ctx, openingID); err != nil {
		return nil, err
	}
	batches, err := s.deps.Batches.ListBatches(ctx, openingID)
	if err != nil {
		return nil, fmt.Errorf("loading screening batches: %w", err)
	}
	res := &Results{Ranked: domain.Rank(batches, f), NeedsReview: []domain.Ranked{}}
	for bi := range batches {
		for ei := range batches[bi].Entries {
			switch e := &batches[bi].Entries[ei]; e.Status {
			case domain.EntryPending:
				res.Pending++
			case domain.EntryNeedsReview, domain.EntryFailed:
				res.NeedsReview = append(res.NeedsReview, domain.Ranked{Batch: &batches[bi], Entry: e})
			}
		}
	}
	return res, nil
}

// GetScreeningResult opens one resume: Hiring's record of it, and — once it is screened — the
// justification for its score, from Resume Processing. Resume Processing being down costs the
// justification, not the record.
func (s *ScreeningService) GetScreeningResult(ctx context.Context, openingID, resumeID string) (*ScreenedCandidate, error) {
	b, e, err := s.entry(ctx, openingID, resumeID)
	if err != nil {
		return nil, err
	}
	c := &ScreenedCandidate{Ranked: domain.Ranked{Batch: b, Entry: e}}
	if e.Status == domain.EntryScreened && s.deps.Justifications != nil {
		j, err := s.deps.Justifications.GetJustification(ctx, e.Result.CandidateID, b.ID, e.ResumeID)
		if err != nil {
			slog.Warn("justification unavailable", "resume", resumeID, "candidate", e.Result.CandidateID, "err", err)
		} else {
			// A verdict on a criterion the batch does not have cannot be shown against one.
			j.Evidence = slices.DeleteFunc(j.Evidence, func(ev domain.CriterionEvidence) bool {
				return ev.Criterion < 0 || ev.Criterion >= len(b.Criteria)
			})
			c.Justification = &j
		}
	}
	return c, nil
}

// OverrideScore replaces a screened resume's score with the Recruiter's, keeping the original
// beside it (overrideScore(), FR-2.8, UC-2 flow 6a).
func (s *ScreeningService) OverrideScore(ctx context.Context, openingID, resumeID string, score int) (*domain.Ranked, error) {
	return s.decide(ctx, openingID, resumeID, func(e *domain.ScreeningEntry) error {
		return e.OverrideScore(score, s.deps.Clock.Now())
	})
}

// ShortlistCandidate marks a screened candidate as worth interviewing (shortlistCandidate(),
// FR-2.9).
func (s *ScreeningService) ShortlistCandidate(ctx context.Context, openingID, resumeID string) (*domain.Ranked, error) {
	return s.decide(ctx, openingID, resumeID, func(e *domain.ScreeningEntry) error {
		return e.Shortlist(s.deps.Clock.Now())
	})
}

// decide applies a Recruiter's decision to one resume and saves it only if it was allowed.
func (s *ScreeningService) decide(ctx context.Context, openingID, resumeID string, decision func(*domain.ScreeningEntry) error) (*domain.Ranked, error) {
	b, e, err := s.entry(ctx, openingID, resumeID)
	if err != nil {
		return nil, err
	}
	if err := decision(e); err != nil {
		return nil, err
	}
	if err := s.deps.Batches.SaveEntry(ctx, e); err != nil {
		return nil, fmt.Errorf("saving resume %s: %w", resumeID, err)
	}
	return &domain.Ranked{Batch: b, Entry: e}, nil
}

// entry finds a resume of the job opening, and the batch it belongs to.
func (s *ScreeningService) entry(ctx context.Context, openingID, resumeID string) (*domain.ScreeningBatch, *domain.ScreeningEntry, error) {
	b, err := s.deps.Batches.FindBatchOfResume(ctx, resumeID)
	if err != nil {
		return nil, nil, err
	}
	if b.JobOpeningID != openingID {
		return nil, nil, ErrResumeNotFound
	}
	return b, b.Entry(resumeID), nil
}

// cleanFileName keeps the base name of an uploaded file — some browsers send a whole path —
// bounded in length.
func cleanFileName(name string) string {
	name = strings.TrimSpace(name[strings.LastIndexAny(name, `/\`)+1:])
	if name == "" {
		return "unnamed.pdf"
	}
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	return name
}

// rejectReason says why an uploaded file cannot be screened, or "" when it can.
func rejectReason(f UploadedFile) string {
	switch {
	case len(f.Content) == 0:
		return "the file is empty"
	case len(f.Content) > MaxResumeBytes:
		return fmt.Sprintf("larger than %d MB", MaxResumeBytes>>20)
	case !isPDF(f.Content):
		return "not a PDF"
	}
	return ""
}

// isPDF looks for the PDF header, which the format allows anywhere in the first kilobyte.
func isPDF(content []byte) bool {
	return bytes.Contains(content[:min(len(content), 1024)], []byte("%PDF-"))
}

func describe(rejected []RejectedFile) string {
	parts := make([]string, len(rejected))
	for i, r := range rejected {
		parts[i] = r.Name + ": " + r.Reason
	}
	return strings.Join(parts, "; ")
}
