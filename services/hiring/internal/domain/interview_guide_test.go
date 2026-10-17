package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

var (
	generatedAt = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	revisedAt   = time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	claude      = domain.ModelAttribution{Provider: "anthropic", Model: "a-model", PromptVersion: "uc3-1"}
)

func twoQuestions() []domain.Question {
	return []domain.Question{
		{Text: "What was the partitioning strategy?", Criterion: "Has shipped with Kafka", Evidence: "Kafka on the Wongnai project", Kind: domain.ResumeClaim},
		{Text: "Talk us through your largest Go project.", Criterion: "3+ years of Go", Kind: domain.MustHaveGap},
	}
}

func newGuide(t *testing.T) *domain.InterviewGuide {
	t.Helper()
	g, err := domain.NewInterviewGuide("ig-1", "jo-1", "cand-001", twoQuestions(), "", claude, generatedAt)
	if err != nil {
		t.Fatalf("NewInterviewGuide: %v", err)
	}
	return g
}

func TestNewInterviewGuide(t *testing.T) {
	g := newGuide(t)

	if g.Revision != 1 || g.Source != domain.Generated || g.Attribution != claude {
		t.Errorf("guide = %+v", g)
	}
	if !g.CreatedAt.Equal(generatedAt) || !g.UpdatedAt.Equal(generatedAt) {
		t.Errorf("created %s, updated %s", g.CreatedAt, g.UpdatedAt)
	}
}

func TestNewInterviewGuide_RefusesWhatCannotBeInterviewedFrom(t *testing.T) {
	cases := map[string][]domain.Question{
		"no questions at all":                 {},
		"a question with no text":             {{Criterion: "3+ years of Go", Kind: domain.RoleDepth}},
		"a question tagged with no criterion": {{Text: "Tell me about yourself.", Kind: domain.RoleDepth}},
		"a question of no known kind":         {{Text: "Why Kafka?", Criterion: "Has shipped with Kafka", Kind: "small-talk"}},
	}
	for name, questions := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := domain.NewInterviewGuide("ig-1", "jo-1", "cand-001", questions, "", claude, generatedAt)
			if !errors.Is(err, domain.ErrInvalidInterviewGuide) {
				t.Errorf("err = %v, want ErrInvalidInterviewGuide", err)
			}
		})
	}
}

func TestNewInterviewGuide_NeedsACandidateAndAnOpening(t *testing.T) {
	if _, err := domain.NewInterviewGuide("ig-1", "jo-1", "  ", twoQuestions(), "", claude, generatedAt); !errors.Is(err, domain.ErrInvalidInterviewGuide) {
		t.Errorf("err = %v, want ErrInvalidInterviewGuide", err)
	}
}

func TestRegenerate_KeepsTheGuideAndMovesTheRevision(t *testing.T) {
	g := newGuide(t)
	fresh := []domain.Question{{Text: "Design a rate limiter.", Criterion: "System design", Kind: domain.RoleDepth}}

	if err := g.Regenerate(fresh, "more system design", claude, revisedAt); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}

	if g.ID != "ig-1" || g.Revision != 2 || g.Emphasis != "more system design" || g.Source != domain.Generated {
		t.Errorf("guide = %+v", g)
	}
	if len(g.Questions) != 1 || !g.UpdatedAt.Equal(revisedAt) || !g.CreatedAt.Equal(generatedAt) {
		t.Errorf("guide = %+v", g)
	}
}

func TestReplaceQuestions_DropsTheModelAttribution(t *testing.T) {
	g := newGuide(t)
	theirs := []domain.Question{{Text: "Walk me through the Wongnai outage.", Criterion: "Has shipped with Kafka", Kind: domain.ResumeClaim}}

	if err := g.ReplaceQuestions(theirs, revisedAt); err != nil {
		t.Fatalf("ReplaceQuestions: %v", err)
	}

	// The questions are partly the Recruiter's now, so naming a model as their author
	// would be false (ADR-004 is about being able to account for what a model produced).
	if g.Attribution != (domain.ModelAttribution{}) {
		t.Errorf("attribution = %+v, want none", g.Attribution)
	}
	if g.Source != domain.Edited || g.Revision != 2 {
		t.Errorf("guide = %+v", g)
	}
}

func TestRejectedRevisionLeavesTheGuideAsItWas(t *testing.T) {
	g := newGuide(t)

	if err := g.ReplaceQuestions(nil, revisedAt); !errors.Is(err, domain.ErrInvalidInterviewGuide) {
		t.Fatalf("err = %v, want ErrInvalidInterviewGuide", err)
	}

	if len(g.Questions) != 2 || g.Revision != 1 || g.Source != domain.Generated {
		t.Errorf("an emptied guide survived a rejected revision: %+v", g)
	}
}

func TestQuestionsAreCopiedIn(t *testing.T) {
	questions := twoQuestions()
	g, err := domain.NewInterviewGuide("ig-1", "jo-1", "cand-001", questions, "", claude, generatedAt)
	if err != nil {
		t.Fatalf("NewInterviewGuide: %v", err)
	}

	questions[0].Text = "changed by the caller afterwards"

	if g.Questions[0].Text == questions[0].Text {
		t.Error("the guide shares its slice with the caller")
	}
}
