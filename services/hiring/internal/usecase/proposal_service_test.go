package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// fakeDeriver answers with a fixed proposal and remembers what it was asked.
type fakeDeriver struct {
	description string
	language    domain.Language
	err         error
}

func (d *fakeDeriver) DeriveCriteria(_ context.Context, description string, language domain.Language) (*domain.JobOpeningProposal, error) {
	d.description, d.language = description, language
	if d.err != nil {
		return nil, d.err
	}
	return &domain.JobOpeningProposal{Title: "Backend Engineer", Criteria: validDraft().Criteria}, nil
}

// UC-1 steps 2–3: the description goes to the AI Service and its proposal comes back as is.
func TestProposeJobOpening(t *testing.T) {
	d := &fakeDeriver{}

	p, err := usecase.NewProposalService(d).ProposeJobOpening(context.Background(), "วิศวกร backend ใช้ Go 3 ปีขึ้นไป", domain.LanguageThai)

	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Backend Engineer" || d.language != domain.LanguageThai || d.description == "" {
		t.Errorf("proposal = %+v, asked %q in %q", p, d.description, d.language)
	}
}

// An empty description or an unknown language is refused before any model is called.
func TestProposeJobOpening_RefusesBadInputWithoutCallingTheModel(t *testing.T) {
	for _, in := range []struct{ description, language string }{{"   ", ""}, {"Backend engineer", "fr"}} {
		d := &fakeDeriver{}

		_, err := usecase.NewProposalService(d).ProposeJobOpening(context.Background(), in.description, domain.Language(in.language))

		if !errors.Is(err, domain.ErrInvalidJobOpening) || d.description != "" {
			t.Errorf("%+v: err = %v, model asked %q", in, err, d.description)
		}
	}
}

// FR-1.5: the AI Service being down is reported as such, so the Recruiter can enter criteria
// by hand.
func TestProposeJobOpening_AIUnavailable(t *testing.T) {
	d := &fakeDeriver{err: usecase.ErrAIUnavailable}

	_, err := usecase.NewProposalService(d).ProposeJobOpening(context.Background(), "Backend engineer", "")

	if !errors.Is(err, usecase.ErrAIUnavailable) {
		t.Errorf("err = %v, want ErrAIUnavailable", err)
	}
}
