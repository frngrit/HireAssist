package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// maxUploadBytes bounds one batch upload. One file is bounded by usecase.MaxResumeBytes.
const maxUploadBytes = 100 << 20

// ---- wire format ----

type proposeRequest struct {
	Description string `json:"description"`
	Language    string `json:"language,omitempty"`
}

type proposalResponse struct {
	Title         string          `json:"title"`
	Criteria      []criterionJSON `json:"criteria"`
	Uninterpreted []string        `json:"uninterpreted"`
	Attribution   attributionJSON `json:"attribution"`
}

type countsJSON struct {
	Pending     int `json:"pending"`
	Screened    int `json:"screened"`
	NeedsReview int `json:"needs_review"`
	Failed      int `json:"failed"`
}

type entryJSON struct {
	ResumeID string `json:"resume_id"`
	FileName string `json:"file_name"`
	Status   string `json:"status"`
}

type rejectedJSON struct {
	FileName string `json:"file_name"`
	Reason   string `json:"reason"`
}

type batchResponse struct {
	ID           string         `json:"id"`
	JobOpeningID string         `json:"job_opening_id"`
	SubmittedAt  time.Time      `json:"submitted_at"`
	Complete     bool           `json:"complete"`
	Counts       countsJSON     `json:"counts"`
	Entries      []entryJSON    `json:"entries"`
	Rejected     []rejectedJSON `json:"rejected,omitempty"` // only in the answer to the upload itself
}

// resultJSON is one resume as the Recruiter sees it. Score fields appear only once it is
// screened; reason only when it could not be.
type resultJSON struct {
	ResumeID       string     `json:"resume_id"`
	BatchID        string     `json:"batch_id"`
	FileName       string     `json:"file_name"`
	Status         string     `json:"status"`
	CandidateID    string     `json:"candidate_id,omitempty"`
	Score          *int       `json:"score,omitempty"`
	OriginalScore  *int       `json:"original_score,omitempty"`
	OverriddenAt   *time.Time `json:"overridden_at,omitempty"`
	Qualified      *bool      `json:"qualified,omitempty"`
	UnmetMustHaves []string   `json:"unmet_must_haves,omitempty"`
	Shortlisted    bool       `json:"shortlisted"`
	ShortlistedAt  *time.Time `json:"shortlisted_at,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type resultsResponse struct {
	JobOpeningID string       `json:"job_opening_id"`
	Pending      int          `json:"pending"`
	Ranked       []resultJSON `json:"ranked"`
	NeedsReview  []resultJSON `json:"needs_review"`
}

type evidenceJSON struct {
	Criterion      string `json:"criterion"`
	Classification string `json:"classification"`
	Met            bool   `json:"met"`
	Score          int    `json:"score"`
	Evidence       string `json:"evidence"`
}

type justificationJSON struct {
	Text     string         `json:"text"`
	Evidence []evidenceJSON `json:"evidence"`
}

type candidateResponse struct {
	Result resultJSON `json:"result"`
	// Justification is read from Resume Processing; null until the resume is screened, or when
	// that service did not answer.
	Justification *justificationJSON `json:"justification"`
}

type overrideRequest struct {
	Score *int `json:"score"`
}

func toProposalResponse(p *domain.JobOpeningProposal) proposalResponse {
	criteria := make([]criterionJSON, len(p.Criteria))
	for i, c := range p.Criteria {
		criteria[i] = criterionJSON{Name: c.Name, Classification: string(c.Classification), Weight: c.Weight}
	}
	uninterpreted := append([]string{}, p.Uninterpreted...)
	return proposalResponse{
		Title:         p.Title,
		Criteria:      criteria,
		Uninterpreted: uninterpreted,
		Attribution: attributionJSON{
			Provider: p.Attribution.Provider, Model: p.Attribution.Model, PromptVersion: p.Attribution.PromptVersion,
		},
	}
}

func toBatchResponse(b *domain.ScreeningBatch, rejected []usecase.RejectedFile) batchResponse {
	out := batchResponse{
		ID: b.ID, JobOpeningID: b.JobOpeningID, SubmittedAt: b.SubmittedAt.UTC(), Complete: b.Complete(),
		Entries: make([]entryJSON, len(b.Entries)),
	}
	for i, e := range b.Entries {
		out.Entries[i] = entryJSON{ResumeID: e.ResumeID, FileName: e.FileName, Status: string(e.Status)}
		switch e.Status {
		case domain.EntryPending:
			out.Counts.Pending++
		case domain.EntryScreened:
			out.Counts.Screened++
		case domain.EntryNeedsReview:
			out.Counts.NeedsReview++
		case domain.EntryFailed:
			out.Counts.Failed++
		}
	}
	for _, r := range rejected {
		out.Rejected = append(out.Rejected, rejectedJSON{FileName: r.Name, Reason: r.Reason})
	}
	return out
}

func toResult(r domain.Ranked) resultJSON {
	b, e := r.Batch, r.Entry
	out := resultJSON{
		ResumeID: e.ResumeID, BatchID: e.BatchID, FileName: e.FileName, Status: string(e.Status),
		Shortlisted: !e.ShortlistedAt.IsZero(), ShortlistedAt: utc(e.ShortlistedAt),
	}
	res := e.Result
	if res == nil {
		return out
	}
	out.CandidateID, out.Reason, out.CompletedAt = res.CandidateID, res.Reason, utc(res.CompletedAt)
	if e.Status != domain.EntryScreened {
		return out
	}
	score, qualified := e.Score(), res.MeetsMustHaves
	out.Score, out.Qualified = &score, &qualified
	out.UnmetMustHaves = b.UnmetMustHaves(e)
	if e.Override != nil {
		original := res.Score
		out.OriginalScore, out.OverriddenAt = &original, utc(e.Override.At)
	}
	return out
}

// toJustification names each verdict's criterion from the batch's snapshot, which the use case
// has already checked every verdict belongs to.
func toJustification(b *domain.ScreeningBatch, j *domain.Justification) *justificationJSON {
	if j == nil {
		return nil
	}
	out := &justificationJSON{Text: j.Text, Evidence: make([]evidenceJSON, len(j.Evidence))}
	for i, ev := range j.Evidence {
		c := b.Criteria[ev.Criterion]
		out.Evidence[i] = evidenceJSON{
			Criterion: c.Name, Classification: string(c.Classification), Met: ev.Met, Score: ev.Score, Evidence: ev.Evidence,
		}
	}
	return out
}

func toResults(rs []domain.Ranked) []resultJSON {
	out := make([]resultJSON, len(rs))
	for i, r := range rs {
		out[i] = toResult(r)
	}
	return out
}

// utc returns the time in UTC, or nil for the zero time — a moment that has not happened.
func utc(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

// ---- endpoints ----

func (h handler) propose(w http.ResponseWriter, r *http.Request) {
	var req proposeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	p, err := h.proposals.ProposeJobOpening(r.Context(), req.Description, domain.Language(req.Language))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProposalResponse(p))
}

func (h handler) submitBatch(w http.ResponseWriter, r *http.Request) {
	files, ok := readResumes(w, r)
	if !ok {
		return
	}
	b, rejected, err := h.screening.SubmitScreeningBatch(r.Context(), r.PathValue("id"), files)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/job-openings/"+b.JobOpeningID+"/batches/"+b.ID)
	writeJSON(w, http.StatusAccepted, toBatchResponse(b, rejected))
}

func (h handler) batchStatus(w http.ResponseWriter, r *http.Request) {
	b, err := h.screening.GetBatchStatus(r.Context(), r.PathValue("id"), r.PathValue("batchId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toBatchResponse(b, nil))
}

func (h handler) rankedResults(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.RankFilter{Criterion: q.Get("criterion")}
	if v := q.Get("min_score"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > domain.MaxScore {
			badRequest(w, fmt.Sprintf("min_score must be a whole number from 0 to %d", domain.MaxScore))
			return
		}
		f.MinScore = n
	}
	res, err := h.screening.GetRankedResults(r.Context(), r.PathValue("id"), f)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resultsResponse{
		JobOpeningID: r.PathValue("id"),
		Pending:      res.Pending,
		Ranked:       toResults(res.Ranked),
		NeedsReview:  toResults(res.NeedsReview),
	})
}

func (h handler) screeningResult(w http.ResponseWriter, r *http.Request) {
	c, err := h.screening.GetScreeningResult(r.Context(), r.PathValue("id"), r.PathValue("resumeId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, candidateResponse{Result: toResult(c.Ranked), Justification: toJustification(c.Batch, c.Justification)})
}

func (h handler) overrideScore(w http.ResponseWriter, r *http.Request) {
	var req overrideRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Score == nil {
		badRequest(w, "score is required")
		return
	}
	respondResult(w)(h.screening.OverrideScore(r.Context(), r.PathValue("id"), r.PathValue("resumeId"), *req.Score))
}

func (h handler) shortlist(w http.ResponseWriter, r *http.Request) {
	respondResult(w)(h.screening.ShortlistCandidate(r.Context(), r.PathValue("id"), r.PathValue("resumeId")))
}

// ---- helpers ----

// respondResult writes a decision's result, or its error.
func respondResult(w http.ResponseWriter) func(*domain.Ranked, error) {
	return func(r *domain.Ranked, err error) {
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toResult(*r))
	}
}

// readResumes reads the files of a multipart upload, one per "resumes" field. Each is read only
// to one byte past the size limit, so an oversized file costs no more memory than an allowed
// one; the use case reports it as too large.
func readResumes(w http.ResponseWriter, r *http.Request) ([]usecase.UploadedFile, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		badRequest(w, `send the resumes as multipart/form-data, one file per "resumes" field`)
		return nil, false
	}
	files := []usecase.UploadedFile{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			uploadError(w, err)
			return nil, false
		}
		if part.FormName() != "resumes" {
			badRequest(w, fmt.Sprintf(`unexpected form field %q; send the files in "resumes"`, part.FormName()))
			return nil, false
		}
		content, err := io.ReadAll(io.LimitReader(part, usecase.MaxResumeBytes+1))
		if err != nil {
			uploadError(w, err)
			return nil, false
		}
		files = append(files, usecase.UploadedFile{Name: part.FileName(), Content: content})
	}
	if len(files) == 0 {
		badRequest(w, `no files in the "resumes" field`)
		return nil, false
	}
	return files, true
}

// uploadError reports an upload that could not be read: too large as a whole, or malformed.
func uploadError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("the upload is larger than %d MB; split it into smaller batches", maxUploadBytes>>20),
		})
		return
	}
	badRequest(w, "malformed multipart body: "+err.Error())
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}
