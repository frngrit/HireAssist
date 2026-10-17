package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/memory"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

func aGuide(t *testing.T, candidateID string) *domain.InterviewGuide {
	t.Helper()
	g, err := domain.NewInterviewGuide("ig-"+candidateID, "jo-1", candidateID,
		[]domain.Question{{Text: "Why Kafka?", Criterion: "Kafka", Kind: domain.ResumeClaim}},
		"", domain.ModelAttribution{Provider: "anthropic"}, time.Now().UTC())
	if err != nil {
		t.Fatalf("building a guide: %v", err)
	}
	return g
}

func TestInterviewGuideRepository_SaveThenFind(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewInterviewGuideRepository()
	if err := repo.Save(ctx, aGuide(t, "cand-001")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	found, err := repo.FindByCandidate(ctx, "jo-1", "cand-001")
	if err != nil {
		t.Fatalf("FindByCandidate: %v", err)
	}
	if found.ID != "ig-cand-001" {
		t.Errorf("found %+v", found)
	}

	// A guide belongs to one candidate against one opening; another candidate has their own.
	if _, err := repo.FindByCandidate(ctx, "jo-1", "cand-002"); !errors.Is(err, usecase.ErrGuideNotFound) {
		t.Errorf("err = %v, want ErrGuideNotFound", err)
	}
}

func TestInterviewGuideRepository_SaveReplaces(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewInterviewGuideRepository()
	guide := aGuide(t, "cand-001")
	_ = repo.Save(ctx, guide)

	if err := guide.Regenerate([]domain.Question{{Text: "Design a queue.", Criterion: "Kafka", Kind: domain.RoleDepth}},
		"more system design", domain.ModelAttribution{Provider: "anthropic"}, time.Now().UTC()); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	_ = repo.Save(ctx, guide)

	found, err := repo.FindByCandidate(ctx, "jo-1", "cand-001")
	if err != nil {
		t.Fatalf("FindByCandidate: %v", err)
	}
	if found.Revision != 2 || len(found.Questions) != 1 || found.Questions[0].Text != "Design a queue." {
		t.Errorf("found %+v", found)
	}
}

func TestInterviewGuideRepository_StoresCopies(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewInterviewGuideRepository()
	guide := aGuide(t, "cand-001")
	_ = repo.Save(ctx, guide)

	guide.Questions[0].Text = "changed after saving"

	found, _ := repo.FindByCandidate(ctx, "jo-1", "cand-001")
	if found.Questions[0].Text == "changed after saving" {
		t.Error("the repository shares its questions with the caller")
	}
}
