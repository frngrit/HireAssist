package mongo_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/mongo"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// Compile-time check: the adapter satisfies the use case's port.
var _ usecase.InterviewGuideRepository = (*mongo.InterviewGuideRepository)(nil)

// newRepo connects to the MongoDB named by HIRING_TEST_MONGO_URL. Without the variable the
// test is skipped, so `go test ./...` needs no database; `docker compose up -d mongo` provides
// one. It uses its own database, never the one a running service writes to.
func newRepo(t *testing.T) *mongo.InterviewGuideRepository {
	t.Helper()
	uri := os.Getenv("HIRING_TEST_MONGO_URL")
	if uri == "" {
		t.Skip("HIRING_TEST_MONGO_URL not set; skipping MongoDB integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo, disconnect, err := mongo.Connect(ctx, uri, "hiring_test")
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(disconnect)
	return repo
}

func guide(t *testing.T, candidateID string) *domain.InterviewGuide {
	t.Helper()
	g, err := domain.NewInterviewGuide("ig-"+candidateID, "jo-1", candidateID, []domain.Question{
		{Text: "You list Kafka — what was the partitioning strategy?", Criterion: "Kafka", Evidence: "Kafka on the resume", Kind: domain.ResumeClaim},
		{Text: "Talk us through your largest Go project.", Criterion: "3+ years of Go", Kind: domain.MustHaveGap},
	}, "more system design", domain.ModelAttribution{Provider: "anthropic", Model: "a-model", PromptVersion: "uc3-1"},
		time.Now().UTC().Truncate(time.Millisecond))
	if err != nil {
		t.Fatalf("building a guide: %v", err)
	}
	return g
}

func TestSaveThenFind(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	original := guide(t, "cand-save-find")

	if err := repo.Save(ctx, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	found, err := repo.FindByCandidate(ctx, "jo-1", "cand-save-find")
	if err != nil {
		t.Fatalf("FindByCandidate: %v", err)
	}

	if found.ID != original.ID || found.Emphasis != original.Emphasis || found.Attribution != original.Attribution {
		t.Errorf("found %+v, want %+v", found, original)
	}
	if len(found.Questions) != 2 || found.Questions[0].Kind != domain.ResumeClaim || found.Questions[1].Evidence != "" {
		t.Errorf("questions = %+v", found.Questions)
	}
	if !found.CreatedAt.Equal(original.CreatedAt.Truncate(time.Millisecond)) {
		t.Errorf("created at %s, want %s", found.CreatedAt, original.CreatedAt)
	}
}

func TestSaveReplacesTheCandidatesGuide(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	g := guide(t, "cand-replace")
	if err := repo.Save(ctx, g); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := g.ReplaceQuestions([]domain.Question{
		{Text: "A question the Recruiter wrote.", Criterion: "Kafka", Kind: domain.RoleDepth},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("ReplaceQuestions: %v", err)
	}
	if err := repo.Save(ctx, g); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	found, err := repo.FindByCandidate(ctx, "jo-1", "cand-replace")
	if err != nil {
		t.Fatalf("FindByCandidate: %v", err)
	}
	if found.Revision != 2 || len(found.Questions) != 1 || found.Source != domain.Edited {
		t.Errorf("found %+v", found)
	}
	// An edited guide claims no model wrote it.
	if found.Attribution != (domain.ModelAttribution{}) {
		t.Errorf("attribution = %+v, want none", found.Attribution)
	}
}

func TestANoteSurvivesTheRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	g := guide(t, "cand-note")
	if err := g.RecordNote("q2", "Only side projects; honest about it.", time.Now().UTC()); err != nil {
		t.Fatalf("RecordNote: %v", err)
	}
	if err := repo.Save(ctx, g); err != nil {
		t.Fatalf("Save: %v", err)
	}

	found, err := repo.FindByCandidate(ctx, "jo-1", "cand-note")
	if err != nil {
		t.Fatalf("FindByCandidate: %v", err)
	}
	if found.Questions[1].ID != "q2" || found.Questions[1].Note != "Only side projects; honest about it." {
		t.Errorf("questions = %+v", found.Questions)
	}
}

func TestFindByCandidate_NoneYet(t *testing.T) {
	_, err := newRepo(t).FindByCandidate(context.Background(), "jo-1", "cand-nobody")

	if !errors.Is(err, usecase.ErrGuideNotFound) {
		t.Errorf("err = %v, want ErrGuideNotFound", err)
	}
}
