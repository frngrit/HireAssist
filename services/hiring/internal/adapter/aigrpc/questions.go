// Package aigrpc asks the AI Service for interview questions over gRPC (ADR-007). It is the
// only place in this service that speaks to a model's owner, and the only place that knows the
// shape of the `.proto`: the use case sees domain terms on both sides.
package aigrpc

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	aiv1 "github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc/gen/hireassist/ai/v1"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
)

// Questions is the client. Close it when the service shuts down.
type Questions struct {
	conn   *grpc.ClientConn
	client aiv1.AiServiceClient
}

// Dial prepares a client for the AI Service at target, e.g. localhost:50051. The connection is
// made lazily, so the AI Service being down does not stop this service from starting — it is a
// dependency of one use case, not of the whole service (ADR-001).
//
// The channel is not encrypted: both ends sit inside the cluster, where TLS is the platform's
// to provide (ADR-001 puts TLS termination at the gateway).
func Dial(target string) (*Questions, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("configuring the AI Service client: %w", err)
	}
	return &Questions{conn: conn, client: aiv1.NewAiServiceClient(conn)}, nil
}

// Close releases the connection.
func (q *Questions) Close() error { return q.conn.Close() }

// GenerateInterviewQuestions is `generateInterviewQuestions()` from the SOC table.
//
// Criteria have no identifier of their own in this service — they are an ordered list on the
// job opening — so one is made from the position for the length of this call, and the answer's
// criterion ids are turned back into the names the Recruiter wrote (FR-3.3).
func (q *Questions) GenerateInterviewQuestions(ctx context.Context, profile domain.CandidateProfile, criteria []domain.Criterion, emphasis string) ([]domain.Question, domain.ModelAttribution, error) {
	names := make(map[string]string, len(criteria))
	wire := make([]*aiv1.Criterion, len(criteria))
	for i, c := range criteria {
		id := strconv.Itoa(i + 1)
		names[id] = c.Name
		wire[i] = &aiv1.Criterion{
			Id:       id,
			Text:     c.Name,
			MustHave: c.Classification == domain.MustHave,
			Weight:   uint32(c.Weight),
		}
	}

	res, err := q.client.GenerateInterviewQuestions(ctx, &aiv1.GenerateInterviewQuestionsRequest{
		Profile:  toWireProfile(profile),
		Criteria: wire,
		Emphasis: emphasis,
	})
	if err != nil {
		return nil, domain.ModelAttribution{}, err
	}

	questions := make([]domain.Question, 0, len(res.GetQuestions()))
	for _, got := range res.GetQuestions() {
		name, known := names[got.GetCriterionId()]
		if !known {
			// A question tagged with a criterion this opening does not have cannot tell the
			// interviewer why it is being asked, which is what FR-3.3 is for. Dropping it is
			// better than showing a tag that means nothing.
			continue
		}
		questions = append(questions, domain.Question{
			Text:      got.GetText(),
			Criterion: name,
			Evidence:  got.GetSourceEvidence(),
			Kind:      toDomainKind(got.GetKind()),
		})
	}

	attribution := res.GetAttribution()
	return questions, domain.ModelAttribution{
		Provider:      attribution.GetProvider(),
		Model:         attribution.GetModel(),
		PromptVersion: attribution.GetPromptVersion(),
	}, nil
}

func toWireProfile(p domain.CandidateProfile) *aiv1.CandidateProfile {
	work := make([]*aiv1.WorkExperience, len(p.WorkHistory))
	for i, w := range p.WorkHistory {
		work[i] = &aiv1.WorkExperience{
			Title:        w.Title,
			Organisation: w.Organisation,
			Start:        w.Start,
			End:          w.End,
			Summary:      w.Summary,
		}
	}
	education := make([]*aiv1.Education, len(p.Education))
	for i, e := range p.Education {
		education[i] = &aiv1.Education{
			Qualification: e.Qualification,
			Institution:   e.Institution,
			Completed:     e.Completed,
		}
	}
	return &aiv1.CandidateProfile{
		CandidateId:    p.CandidateID,
		WorkHistory:    work,
		Skills:         p.Skills,
		Education:      education,
		AdditionalText: p.AdditionalText,
	}
}

// toDomainKind maps the three kinds FR-3.2 asks a guide to cover. An unspecified kind is read
// as role depth: the question is still usable, and the alternative is discarding it.
func toDomainKind(kind aiv1.QuestionKind) domain.QuestionKind {
	switch kind {
	case aiv1.QuestionKind_QUESTION_KIND_RESUME_CLAIM:
		return domain.ResumeClaim
	case aiv1.QuestionKind_QUESTION_KIND_MUST_HAVE_GAP:
		return domain.MustHaveGap
	default:
		return domain.RoleDepth
	}
}
