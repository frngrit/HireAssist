// Package resumeproc reads candidate profiles from the Resume Processing Service over REST
// (ADR-001), the one piece of UC-3 that Hiring does not own.
//
// Hiring reads the profile and stores no copy: a second copy is a second place a person's data
// must be erased from, and a second copy that can disagree with the first (SOC table).
package resumeproc

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// maxProfileBytes caps what is read from the other service. A resume's text is large; a
// response far larger than this is a misconfiguration, not a profile.
const maxProfileBytes = 4 << 20

// CandidateProfiles is the client. Build one with New.
type CandidateProfiles struct {
	baseURL string
	client  *http.Client
}

// New returns a client for a Resume Processing Service at baseURL, e.g. http://localhost:3000.
func New(baseURL string, timeout time.Duration) *CandidateProfiles {
	return &CandidateProfiles{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

// profileJSON is the part of that service's CandidateProfile schema this use case needs.
//
// The name and the email address are deliberately not read. A guide is built from what a
// candidate can be asked about, and the fewer places a person's contact details travel, the
// fewer places they must be erased from (ADR-004, PDPA).
type profileJSON struct {
	ID          string               `json:"id"`
	Skills      []string             `json:"skills"`
	WorkHistory []workExperienceJSON `json:"work_history"`
	Education   []educationJSON      `json:"education"`
	// AdditionalText is the resume text that did not fit the structured fields.
	AdditionalText string `json:"additional_text"`
	// ResumeText is the whole extracted text. A profile the pipeline produced has the
	// structured fields and additional_text; one created by hand has only this.
	ResumeText string `json:"resume_text"`
}

type workExperienceJSON struct {
	Title        string `json:"title"`
	Organisation string `json:"organisation"`
	Start        string `json:"start"`
	End          string `json:"end"`
	Summary      string `json:"summary"`
}

type educationJSON struct {
	Qualification string `json:"qualification"`
	Institution   string `json:"institution"`
	Completed     string `json:"completed"`
}

// GetCandidateProfile is `getCandidateProfile()` from the SOC table.
func (c *CandidateProfiles) GetCandidateProfile(ctx context.Context, candidateID string) (domain.CandidateProfile, error) {
	endpoint := c.baseURL + "/candidates/" + url.PathEscape(candidateID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return domain.CandidateProfile{}, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.client.Do(req)
	if err != nil {
		return domain.CandidateProfile{}, fmt.Errorf("calling the resume processing service: %w", err)
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusNotFound:
		return domain.CandidateProfile{}, usecase.ErrCandidateNotFound
	case res.StatusCode != http.StatusOK:
		return domain.CandidateProfile{}, fmt.Errorf("resume processing service answered %s", res.Status)
	}

	var body profileJSON
	if err := json.NewDecoder(io.LimitReader(res.Body, maxProfileBytes)).Decode(&body); err != nil {
		return domain.CandidateProfile{}, fmt.Errorf("reading the candidate profile: %w", err)
	}
	if body.ID == "" {
		return domain.CandidateProfile{}, errors.New("the candidate profile carries no id")
	}
	return domain.CandidateProfile{
		CandidateID: body.ID,
		Skills:      body.Skills,
		WorkHistory: toWorkHistory(body.WorkHistory),
		Education:   toEducation(body.Education),
		// Prefer the leftover text: alongside the structured fields, sending the whole resume
		// again would say everything twice. A profile created by hand has no structured
		// fields and no leftover, so its resume text is all there is.
		AdditionalText: cmp.Or(body.AdditionalText, body.ResumeText),
	}, nil
}

func toWorkHistory(in []workExperienceJSON) []domain.WorkExperience {
	out := make([]domain.WorkExperience, len(in))
	for i, w := range in {
		out[i] = domain.WorkExperience{
			Title:        w.Title,
			Organisation: w.Organisation,
			Start:        w.Start,
			End:          w.End,
			Summary:      w.Summary,
		}
	}
	return out
}

func toEducation(in []educationJSON) []domain.Qualification {
	out := make([]domain.Qualification, len(in))
	for i, e := range in {
		out[i] = domain.Qualification{
			Qualification: e.Qualification,
			Institution:   e.Institution,
			Completed:     e.Completed,
		}
	}
	return out
}
