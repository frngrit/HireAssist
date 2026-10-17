package aigrpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiv1 "github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc/gen/hireassist/ai/v1"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// deriveTimeout bounds one proposal. The AI Service gives a model 60 seconds per attempt; a
// proposal is one short answer.
const deriveTimeout = 60 * time.Second

// DeriveCriteria is `deriveCriteriaFromDescription()` from the SOC table (UC-1): it turns a
// free-text role description into a proposed title and weighted criteria for the Recruiter to
// review. A description the AI Service refuses is the Recruiter's to fix; anything else that
// goes wrong means it is unavailable, and the Recruiter enters criteria by hand (FR-1.5).
func (q *Questions) DeriveCriteria(ctx context.Context, description string, language domain.Language) (*domain.JobOpeningProposal, error) {
	ctx, cancel := context.WithTimeout(ctx, deriveTimeout)
	defer cancel()
	res, err := q.client.DeriveCriteriaFromDescription(ctx, &aiv1.DeriveCriteriaFromDescriptionRequest{
		Description: description,
		Language:    toWireLanguage(language),
	})
	if err != nil {
		if s := status.Convert(err); s.Code() == codes.InvalidArgument {
			return nil, fmt.Errorf("%w: %s", domain.ErrInvalidJobOpening, s.Message())
		}
		return nil, fmt.Errorf("%w: %w", usecase.ErrAIUnavailable, err)
	}
	return toProposal(res), nil
}

func toWireLanguage(l domain.Language) aiv1.Language {
	switch l {
	case domain.LanguageThai:
		return aiv1.Language_LANGUAGE_THAI
	case domain.LanguageEnglish:
		return aiv1.Language_LANGUAGE_ENGLISH
	}
	return aiv1.Language_LANGUAGE_UNSPECIFIED
}

// toProposal maps the answer onto the domain. Two differences need bridging: the contract marks
// a must-have with a flag where the domain names a classification, and it ignores a must-have's
// weight — sending 0 — where every domain criterion carries a positive one. A missing weight
// becomes 1, which the Recruiter can change before confirming.
func toProposal(r *aiv1.DeriveCriteriaFromDescriptionResponse) *domain.JobOpeningProposal {
	p := &domain.JobOpeningProposal{
		Title:         r.GetProposedTitle(),
		Criteria:      []domain.Criterion{},
		Uninterpreted: append([]string{}, r.GetUninterpreted()...),
		Attribution: domain.ModelAttribution{
			Provider:      r.GetAttribution().GetProvider(),
			Model:         r.GetAttribution().GetModel(),
			PromptVersion: r.GetAttribution().GetPromptVersion(),
		},
	}
	for _, c := range r.GetCriteria() {
		name := strings.TrimSpace(c.GetText())
		if name == "" {
			continue
		}
		classification := domain.NiceToHave
		if c.GetMustHave() {
			classification = domain.MustHave
		}
		p.Criteria = append(p.Criteria, domain.Criterion{
			Name: name, Classification: classification, Weight: max(int(c.GetWeight()), 1),
		})
	}
	return p
}
