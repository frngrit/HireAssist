package aigrpc_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc"
	aiv1 "github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc/gen/hireassist/ai/v1"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// Compile-time check: the adapter satisfies the use case's port.
var _ usecase.QuestionGenerator = (*aigrpc.Questions)(nil)

// fakeAI is an AI Service that records what it was asked and answers what the test tells it
// to. It is a real gRPC server speaking the real contract, so the generated stubs and the
// mapping either side of them are exercised; only the model is absent.
type fakeAI struct {
	aiv1.UnimplementedAiServiceServer
	got      *aiv1.GenerateInterviewQuestionsRequest
	answer   *aiv1.GenerateInterviewQuestionsResponse
	failWith error
}

func (f *fakeAI) GenerateInterviewQuestions(_ context.Context, req *aiv1.GenerateInterviewQuestionsRequest) (*aiv1.GenerateInterviewQuestionsResponse, error) {
	f.got = req
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.answer, nil
}

func serving(t *testing.T, ai *fakeAI) *aigrpc.Questions {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	server := grpc.NewServer()
	aiv1.RegisterAiServiceServer(server, ai)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	client, err := aigrpc.Dial(listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

var criteria = []domain.Criterion{
	{Name: "3+ years of Go", Classification: domain.MustHave, Weight: 5},
	{Name: "Kafka", Classification: domain.NiceToHave, Weight: 2},
}

func TestGenerateInterviewQuestions(t *testing.T) {
	ai := &fakeAI{answer: &aiv1.GenerateInterviewQuestionsResponse{
		Questions: []*aiv1.InterviewQuestion{
			{Text: "Largest Go project?", CriterionId: "1", SourceEvidence: "side projects only", Kind: aiv1.QuestionKind_QUESTION_KIND_MUST_HAVE_GAP},
			{Text: "Partitioning strategy?", CriterionId: "2", SourceEvidence: "Kafka on the resume", Kind: aiv1.QuestionKind_QUESTION_KIND_RESUME_CLAIM},
		},
		Attribution: &aiv1.ModelAttribution{Provider: "anthropic", Model: "a-model", PromptVersion: "uc3-1"},
	}}
	client := serving(t, ai)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	questions, attribution, err := client.GenerateInterviewQuestions(ctx,
		domain.CandidateProfile{CandidateID: "cand-001", Skills: []string{"Go"}}, criteria, "more system design")
	if err != nil {
		t.Fatalf("GenerateInterviewQuestions: %v", err)
	}

	// What was sent: the criteria, with must-have carried across, and the emphasis.
	if len(ai.got.GetCriteria()) != 2 || !ai.got.GetCriteria()[0].GetMustHave() || ai.got.GetCriteria()[1].GetMustHave() {
		t.Errorf("criteria sent = %+v", ai.got.GetCriteria())
	}
	if ai.got.GetEmphasis() != "more system design" || ai.got.GetProfile().GetCandidateId() != "cand-001" {
		t.Errorf("request = %+v", ai.got)
	}

	// What came back: criterion ids turned into the names the Recruiter wrote (FR-3.3).
	if len(questions) != 2 {
		t.Fatalf("got %d questions, want 2", len(questions))
	}
	if questions[0].Criterion != "3+ years of Go" || questions[0].Kind != domain.MustHaveGap {
		t.Errorf("first question = %+v", questions[0])
	}
	if questions[1].Criterion != "Kafka" || questions[1].Evidence != "Kafka on the resume" {
		t.Errorf("second question = %+v", questions[1])
	}
	if attribution.Provider != "anthropic" || attribution.PromptVersion != "uc3-1" {
		t.Errorf("attribution = %+v", attribution)
	}
}

// A question tagged with a criterion this opening does not have cannot tell the interviewer
// why it is being asked, so it is dropped rather than shown with a meaningless tag.
func TestGenerateInterviewQuestions_DropsAQuestionTaggedWithAnUnknownCriterion(t *testing.T) {
	client := serving(t, &fakeAI{answer: &aiv1.GenerateInterviewQuestionsResponse{
		Questions: []*aiv1.InterviewQuestion{
			{Text: "Tell me about yourself.", CriterionId: "99", Kind: aiv1.QuestionKind_QUESTION_KIND_ROLE_DEPTH},
			{Text: "Largest Go project?", CriterionId: "1", Kind: aiv1.QuestionKind_QUESTION_KIND_MUST_HAVE_GAP},
		},
	}})

	questions, _, err := client.GenerateInterviewQuestions(context.Background(), domain.CandidateProfile{}, criteria, "")
	if err != nil {
		t.Fatalf("GenerateInterviewQuestions: %v", err)
	}

	if len(questions) != 1 || questions[0].Criterion != "3+ years of Go" {
		t.Errorf("questions = %+v", questions)
	}
}

// An unspecified kind is read as role depth: the question is still usable.
func TestGenerateInterviewQuestions_UnspecifiedKind(t *testing.T) {
	client := serving(t, &fakeAI{answer: &aiv1.GenerateInterviewQuestionsResponse{
		Questions: []*aiv1.InterviewQuestion{{Text: "Design a queue.", CriterionId: "2"}},
	}})

	questions, _, err := client.GenerateInterviewQuestions(context.Background(), domain.CandidateProfile{}, criteria, "")
	if err != nil {
		t.Fatalf("GenerateInterviewQuestions: %v", err)
	}
	if len(questions) != 1 || questions[0].Kind != domain.RoleDepth {
		t.Errorf("questions = %+v", questions)
	}
}

func TestGenerateInterviewQuestions_ServiceFailure(t *testing.T) {
	client := serving(t, &fakeAI{failWith: errors.New("the model is unreachable")})

	if _, _, err := client.GenerateInterviewQuestions(context.Background(), domain.CandidateProfile{}, criteria, ""); err == nil {
		t.Error("a failing AI Service was reported as questions")
	}
}
