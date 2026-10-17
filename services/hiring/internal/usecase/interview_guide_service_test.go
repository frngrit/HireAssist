package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/memory"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// ---- test doubles for the two services UC-3 reads ----

type fakeProfiles struct {
	profile domain.CandidateProfile
	err     error
	asked   []string
}

func (f *fakeProfiles) GetCandidateProfile(_ context.Context, candidateID string) (domain.CandidateProfile, error) {
	f.asked = append(f.asked, candidateID)
	if f.err != nil {
		return domain.CandidateProfile{}, f.err
	}
	return f.profile, nil
}

type fakeGenerator struct {
	questions   []domain.Question
	attribution domain.ModelAttribution
	err         error
	gotCriteria []domain.Criterion
	gotEmphasis string
	gotProfile  domain.CandidateProfile
	timesAsked  int
}

func (f *fakeGenerator) GenerateInterviewQuestions(_ context.Context, profile domain.CandidateProfile, criteria []domain.Criterion, emphasis string) ([]domain.Question, domain.ModelAttribution, error) {
	f.timesAsked++
	f.gotProfile, f.gotCriteria, f.gotEmphasis = profile, criteria, emphasis
	if f.err != nil {
		return nil, domain.ModelAttribution{}, f.err
	}
	return f.questions, f.attribution, nil
}

type guideClock struct{}

func (guideClock) Now() time.Time { return time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC) }

type guideIDs struct{ n int }

func (g *guideIDs) NewID() string { g.n++; return "ig-1" }

// fixture builds the service with an opening already stored, the way UC-3 finds it.
func fixture(t *testing.T) (*usecase.InterviewGuideService, *fakeProfiles, *fakeGenerator, usecase.InterviewGuideRepository) {
	t.Helper()
	openings := memory.NewJobOpeningRepository()
	opening, err := domain.NewJobOpening("jo-1", domain.JobOpeningDraft{
		Title: "Backend Engineer",
		Criteria: []domain.Criterion{
			{Name: "3+ years of Go", Classification: domain.MustHave, Weight: 5},
			{Name: "Kafka", Classification: domain.NiceToHave, Weight: 2},
		},
		ExpectedTimeToFill: 30 * 24 * time.Hour,
	}, guideClock{}.Now())
	if err != nil {
		t.Fatalf("building the opening: %v", err)
	}
	if err := openings.Save(context.Background(), opening); err != nil {
		t.Fatalf("saving the opening: %v", err)
	}

	profiles := &fakeProfiles{profile: domain.CandidateProfile{CandidateID: "cand-001", Skills: []string{"Go", "Kafka"}}}
	generator := &fakeGenerator{
		questions:   []domain.Question{{Text: "Partitioning strategy?", Criterion: "Kafka", Kind: domain.ResumeClaim}},
		attribution: domain.ModelAttribution{Provider: "anthropic", Model: "a-model", PromptVersion: "uc3-1"},
	}
	guides := memory.NewInterviewGuideRepository()
	svc := usecase.NewInterviewGuideService(guides, openings, profiles, generator, guideClock{}, &guideIDs{})
	return svc, profiles, generator, guides
}

func TestGenerateInterviewGuide(t *testing.T) {
	svc, profiles, generator, guides := fixture(t)

	guide, err := svc.GenerateInterviewGuide(context.Background(), "jo-1", "cand-001", "")
	if err != nil {
		t.Fatalf("GenerateInterviewGuide: %v", err)
	}

	if guide.ID != "ig-1" || guide.Revision != 1 || guide.CandidateID != "cand-001" {
		t.Errorf("guide = %+v", guide)
	}
	if len(profiles.asked) != 1 || profiles.asked[0] != "cand-001" {
		t.Errorf("profiles asked for %v", profiles.asked)
	}
	// The opening's criteria go to the model: the guide is the intersection of the resume and
	// the job, not a reading of the resume alone (UC-3).
	if len(generator.gotCriteria) != 2 || generator.gotProfile.CandidateID != "cand-001" {
		t.Errorf("generator got criteria %v, profile %+v", generator.gotCriteria, generator.gotProfile)
	}
	// FR-3.1: retained, so it can be reopened rather than generated again.
	if _, err := guides.FindByCandidate(context.Background(), "jo-1", "cand-001"); err != nil {
		t.Errorf("the guide was not retained: %v", err)
	}
}

func TestGenerateInterviewGuide_UnknownOpeningIsNotFound(t *testing.T) {
	svc, _, generator, _ := fixture(t)

	_, err := svc.GenerateInterviewGuide(context.Background(), "jo-404", "cand-001", "")

	if !errors.Is(err, usecase.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if generator.timesAsked != 0 {
		t.Error("the model was called for an opening that does not exist")
	}
}

func TestGenerateInterviewGuide_SecondTimeIsRefused(t *testing.T) {
	svc, _, _, _ := fixture(t)
	if _, err := svc.GenerateInterviewGuide(context.Background(), "jo-1", "cand-001", ""); err != nil {
		t.Fatalf("first generation: %v", err)
	}

	_, err := svc.GenerateInterviewGuide(context.Background(), "jo-1", "cand-001", "")

	// Silently replacing it would throw away whatever the Recruiter had edited.
	if !errors.Is(err, usecase.ErrGuideExists) {
		t.Errorf("err = %v, want ErrGuideExists", err)
	}
}

func TestGenerateInterviewGuide_ReportsAnUnreachableService(t *testing.T) {
	svc, profiles, _, _ := fixture(t)
	profiles.err = errors.New("connection refused")

	_, err := svc.GenerateInterviewGuide(context.Background(), "jo-1", "cand-001", "")

	if !errors.Is(err, usecase.ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestGenerateInterviewGuide_UnknownCandidateIsNotFound(t *testing.T) {
	svc, profiles, _, _ := fixture(t)
	profiles.err = usecase.ErrCandidateNotFound

	_, err := svc.GenerateInterviewGuide(context.Background(), "jo-1", "cand-404", "")

	// A candidate the other service has never heard of is the caller's mistake, not an outage.
	if !errors.Is(err, usecase.ErrCandidateNotFound) {
		t.Errorf("err = %v, want ErrCandidateNotFound", err)
	}
}

func TestRegenerateInterviewGuide_CarriesTheEmphasis(t *testing.T) {
	svc, _, generator, _ := fixture(t)
	if _, err := svc.GenerateInterviewGuide(context.Background(), "jo-1", "cand-001", ""); err != nil {
		t.Fatalf("first generation: %v", err)
	}
	generator.questions = []domain.Question{{Text: "Design a rate limiter.", Criterion: "3+ years of Go", Kind: domain.RoleDepth}}

	guide, err := svc.RegenerateInterviewGuide(context.Background(), "jo-1", "cand-001", "more system design")
	if err != nil {
		t.Fatalf("RegenerateInterviewGuide: %v", err)
	}

	if generator.gotEmphasis != "more system design" {
		t.Errorf("the emphasis did not reach the model: %q", generator.gotEmphasis)
	}
	if guide.Revision != 2 || guide.ID != "ig-1" || guide.Questions[0].Text != "Design a rate limiter." {
		t.Errorf("guide = %+v", guide)
	}
}

func TestReviseGuideQuestions_CallsNoModel(t *testing.T) {
	svc, _, generator, _ := fixture(t)
	if _, err := svc.GenerateInterviewGuide(context.Background(), "jo-1", "cand-001", ""); err != nil {
		t.Fatalf("first generation: %v", err)
	}
	asked := generator.timesAsked

	guide, err := svc.ReviseGuideQuestions(context.Background(), "jo-1", "cand-001", []domain.Question{
		{Text: "A question the Recruiter wrote.", Criterion: "Kafka", Kind: domain.ResumeClaim},
	})
	if err != nil {
		t.Fatalf("ReviseGuideQuestions: %v", err)
	}

	if generator.timesAsked != asked {
		t.Error("editing the questions called the model")
	}
	if guide.Source != domain.Edited || guide.Attribution != (domain.ModelAttribution{}) {
		t.Errorf("guide = %+v", guide)
	}
}

func TestGetInterviewGuide_BeforeOneExists(t *testing.T) {
	svc, _, _, _ := fixture(t)

	_, err := svc.GetInterviewGuide(context.Background(), "jo-1", "cand-001")

	if !errors.Is(err, usecase.ErrGuideNotFound) {
		t.Errorf("err = %v, want ErrGuideNotFound", err)
	}
}
