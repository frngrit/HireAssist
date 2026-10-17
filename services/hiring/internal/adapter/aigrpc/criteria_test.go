package aigrpc_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc"
	aiv1 "github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc/gen/hireassist/ai/v1"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

var _ usecase.CriteriaDeriver = (*aigrpc.Questions)(nil)

// derivingAI is an AI Service that answers DeriveCriteriaFromDescription — a real gRPC server on
// the real contract, with only the model absent.
type derivingAI struct {
	aiv1.UnimplementedAiServiceServer
	got      *aiv1.DeriveCriteriaFromDescriptionRequest
	failWith error
}

func (f *derivingAI) DeriveCriteriaFromDescription(_ context.Context, req *aiv1.DeriveCriteriaFromDescriptionRequest) (*aiv1.DeriveCriteriaFromDescriptionResponse, error) {
	f.got = req
	if f.failWith != nil {
		return nil, f.failWith
	}
	return &aiv1.DeriveCriteriaFromDescriptionResponse{
		ProposedTitle: "Backend Engineer",
		Criteria: []*aiv1.Criterion{
			{Id: "c1", Text: "3+ years of Go or Python", MustHave: true, Weight: 0},
			{Id: "c2", Text: "Kafka or RabbitMQ in production", MustHave: false, Weight: 3},
			{Id: "c3", Text: "   ", Weight: 2},
		},
		Uninterpreted: []string{"English good enough for a standup"},
		Attribution:   &aiv1.ModelAttribution{Provider: "canned", Model: "canned-answers-no-model-was-called", PromptVersion: "v1"},
	}, nil
}

func servingDeriver(t *testing.T, ai *derivingAI) *aigrpc.Questions {
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

// The answer is mapped onto the domain: the must-have flag becomes a classification, a
// must-have's ignored weight becomes 1, and an empty criterion is dropped.
func TestDeriveCriteria(t *testing.T) {
	ai := &derivingAI{}

	p, err := servingDeriver(t, ai).DeriveCriteria(context.Background(), "วิศวกร backend", domain.LanguageThai)

	if err != nil {
		t.Fatal(err)
	}
	if ai.got.GetDescription() != "วิศวกร backend" || ai.got.GetLanguage() != aiv1.Language_LANGUAGE_THAI {
		t.Errorf("request = %v", ai.got)
	}
	want := []domain.Criterion{
		{Name: "3+ years of Go or Python", Classification: domain.MustHave, Weight: 1},
		{Name: "Kafka or RabbitMQ in production", Classification: domain.NiceToHave, Weight: 3},
	}
	if p.Title != "Backend Engineer" || len(p.Criteria) != 2 || p.Criteria[0] != want[0] || p.Criteria[1] != want[1] {
		t.Errorf("proposal = %+v", p)
	}
	if len(p.Uninterpreted) != 1 || p.Attribution.Provider != "canned" {
		t.Errorf("uninterpreted = %v, attribution = %+v", p.Uninterpreted, p.Attribution)
	}
}

// A description the AI Service refuses is the Recruiter's to fix; any other failure means the
// AI Service is unavailable, and the Recruiter enters criteria by hand.
func TestDeriveCriteria_Errors(t *testing.T) {
	for code, want := range map[codes.Code]error{
		codes.InvalidArgument:    domain.ErrInvalidJobOpening,
		codes.Unavailable:        usecase.ErrAIUnavailable,
		codes.FailedPrecondition: usecase.ErrAIUnavailable, // e.g. no model credential
	} {
		_, err := servingDeriver(t, &derivingAI{failWith: status.Error(code, "refused")}).DeriveCriteria(context.Background(), "Backend engineer", "")

		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", code, err, want)
		}
	}
}
