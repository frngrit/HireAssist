// Package app is the composition root: the one place that chooses concrete adapters and
// injects them into the use cases. Nothing else in the service constructs its own dependencies.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/httpapi"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/memory"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/mongo"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/postgres"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/resumeproc"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/system"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// Config is the service's startup configuration, read from the environment by main.
type Config struct {
	// DatabaseURL selects PostgreSQL (ADR-003). Empty means in-memory storage, lost on restart —
	// for tests and quick local runs only.
	DatabaseURL string
	// MongoURL selects MongoDB for interview guides (ADR-003). Empty means in memory, with the
	// same caveat.
	MongoURL string
	// MongoDatabase is the database within it.
	MongoDatabase string
	// ResumeProcessingURL is where candidate profiles are read from (UC-3). Empty means UC-3
	// reports the profile source as unavailable rather than inventing one.
	ResumeProcessingURL string
	// AIServiceAddress is the AI Service's gRPC address (ADR-007), e.g. localhost:50051. Empty
	// means UC-3 cannot generate questions, and says so.
	AIServiceAddress string
}

// upstreamTimeout bounds a call to another service. Generating questions is a model call, so
// it is the slow one; the rest of UC-3 is a profile read.
const upstreamTimeout = 60 * time.Second

// New assembles the Hiring Service. With a database it connects, verifies the connection and
// applies the schema before returning, so a bad database fails startup rather than the first
// request. The returned func releases what New acquired.
func New(ctx context.Context, cfg Config) (http.Handler, func(), error) {
	var (
		repo    usecase.JobOpeningRepository = memory.NewJobOpeningRepository()
		release                              = func() {}
	)
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, fmt.Errorf("configuring database: %w", err)
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, nil, fmt.Errorf("connecting to database: %w", err)
		}
		if err := postgres.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, nil, err
		}
		repo, release = postgres.NewJobOpeningRepository(pool), pool.Close
	}

	// Interview guides are AI-derived documents, so they go to MongoDB rather than beside the
	// hiring record (ADR-003).
	var guideRepo usecase.InterviewGuideRepository = memory.NewInterviewGuideRepository()
	if cfg.MongoURL != "" {
		repo, disconnect, err := mongo.Connect(ctx, cfg.MongoURL, cfg.MongoDatabase)
		if err != nil {
			release()
			return nil, nil, err
		}
		guideRepo = repo
		release = both(release, disconnect)
	}

	// The two services UC-3 needs. Neither is required to start: the Hiring Service serves its
	// own operations without them, and UC-3 answers 502 while one is missing rather than the
	// whole service refusing to run.
	var profiles usecase.CandidateProfiles = unavailable{what: "RESUME_PROCESSING_URL is not set"}
	if cfg.ResumeProcessingURL != "" {
		profiles = resumeproc.New(cfg.ResumeProcessingURL, upstreamTimeout)
	}
	var questions usecase.QuestionGenerator = unavailable{what: "AI_SERVICE_ADDRESS is not set"}
	if cfg.AIServiceAddress != "" {
		client, err := aigrpc.Dial(cfg.AIServiceAddress)
		if err != nil {
			release()
			return nil, nil, err
		}
		questions = client
		release = both(release, func() { _ = client.Close() })
	}

	clock, ids := system.Clock{}, system.RandomIDs{}
	openings := usecase.NewJobOpeningService(repo, clock, ids)
	guides := usecase.NewInterviewGuideService(guideRepo, repo, profiles, questions, clock, system.IDs{Prefix: "ig_"})
	return httpapi.NewHandler(openings, guides), release, nil
}

// both runs two release funcs, in reverse order of acquisition.
func both(first, second func()) func() {
	return func() {
		second()
		first()
	}
}

// unavailable stands in for a service this deployment was not told how to reach. It fails every
// call with the reason, which is more useful than a nil panic and more honest than a guide with
// no questions in it.
type unavailable struct{ what string }

func (u unavailable) GetCandidateProfile(context.Context, string) (domain.CandidateProfile, error) {
	return domain.CandidateProfile{}, errors.New(u.what)
}

func (u unavailable) GenerateInterviewQuestions(context.Context, domain.CandidateProfile, []domain.Criterion, string) ([]domain.Question, domain.ModelAttribution, error) {
	return nil, domain.ModelAttribution{}, errors.New(u.what)
}
