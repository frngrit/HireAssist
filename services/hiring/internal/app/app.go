// Package app is the composition root: the one place that chooses concrete adapters and
// injects them into the use cases. Nothing else in the service constructs its own dependencies.
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/aigrpc"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/httpapi"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/memory"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/mongo"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/objectstore"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/postgres"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/rabbitmq"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/resumeproc"
	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/system"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// Config is the service's startup configuration, read from the environment by main. Only the
// databases are checked at startup. Every other dependency is reached when first needed, so the
// service starts — and serves what does not need it — while that dependency is down.
type Config struct {
	// DatabaseURL selects PostgreSQL (ADR-003). Empty means in-memory storage, lost on restart —
	// for tests and quick local runs only.
	DatabaseURL string
	// MongoURL selects MongoDB for interview guides (ADR-003). Empty means in memory, with the
	// same caveat.
	MongoURL string
	// MongoDatabase is the database within it.
	MongoDatabase string
	// ResumeProcessingURL is where candidate profiles are read from: the justification of a
	// screened resume (UC-2) and the profile a guide is built from (UC-3). Empty means a
	// screened candidate opens without the justification, and UC-3 reports the profile source
	// as unavailable rather than inventing one.
	ResumeProcessingURL string
	// AIServiceAddress is the AI Service's gRPC address (ADR-007), e.g. localhost:50051, for
	// proposals (UC-1) and interview questions (UC-3). Empty means neither can be made, and both
	// say so.
	AIServiceAddress string
	// RabbitMQURL reaches the broker carrying the screening handover. Empty means no broker:
	// batches are accepted, and their resumes wait unscreened until one is configured.
	RabbitMQURL string
	// ObjectStore is where resume files go. An empty endpoint keeps them in memory.
	ObjectStore objectstore.Config
	// RelayInterval is how often held-back screening requests are tried again. Zero means 5s.
	RelayInterval time.Duration
}

// upstreamTimeout bounds a call to another service. Generating questions is a model call, so
// it is the slow one; the rest is a profile read.
const upstreamTimeout = 60 * time.Second

// New assembles the Hiring Service. With a database it connects, verifies the connection and
// applies the schema before returning, so a bad database fails startup rather than the first
// request. With a broker it starts, for as long as ctx lasts, the consumer of screening results
// and the relay. The returned func stops them and releases what New acquired.
func New(ctx context.Context, cfg Config) (http.Handler, func(), error) {
	var closers []func()
	release := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	fail := func(err error) (http.Handler, func(), error) {
		release()
		return nil, nil, err
	}

	openingRepo, screeningRepo, closeDB, err := repositories(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, closeDB)

	// Interview guides are AI-derived documents, so they go to MongoDB rather than beside the
	// hiring record (ADR-003).
	var guideRepo usecase.InterviewGuideRepository = memory.NewInterviewGuideRepository()
	if cfg.MongoURL != "" {
		repo, disconnect, err := mongo.Connect(ctx, cfg.MongoURL, cfg.MongoDatabase)
		if err != nil {
			return fail(err)
		}
		guideRepo = repo
		closers = append(closers, disconnect)
	}

	files, err := resumeStore(cfg.ObjectStore)
	if err != nil {
		return fail(err)
	}

	// The services this one reads. None is required to start: the Hiring Service serves its own
	// operations without them, and an operation that needs one that is missing says so.
	var (
		profiles       usecase.CandidateProfiles = unavailable{what: "RESUME_PROCESSING_URL is not set"}
		justifications usecase.Justifications
	)
	if cfg.ResumeProcessingURL != "" {
		client := resumeproc.New(cfg.ResumeProcessingURL, upstreamTimeout)
		profiles, justifications = client, client
	}
	var (
		questions usecase.QuestionGenerator = unavailable{what: "AI_SERVICE_ADDRESS is not set"}
		deriver   usecase.CriteriaDeriver   = unavailable{what: "AI_SERVICE_ADDRESS is not set"}
	)
	if cfg.AIServiceAddress != "" {
		client, err := aigrpc.Dial(cfg.AIServiceAddress)
		if err != nil {
			return fail(err)
		}
		questions, deriver = client, client
		closers = append(closers, func() { _ = client.Close() })
	}
	var (
		requester usecase.ScreeningRequester = noBroker{}
		broker    *rabbitmq.Broker
	)
	if cfg.RabbitMQURL != "" {
		broker = rabbitmq.New(cfg.RabbitMQURL, cfg.ObjectStore.Bucket)
		requester = broker
	}

	clock, ids := system.Clock{}, system.RandomIDs{}
	screening := usecase.NewScreeningService(usecase.ScreeningDeps{
		Openings: openingRepo, Batches: screeningRepo, Files: files, Requester: requester,
		Justifications: justifications, Clock: clock, IDs: ids,
	})
	if broker != nil {
		background, stop := context.WithCancel(ctx)
		var wg sync.WaitGroup
		wg.Go(func() { broker.Run(background, screening) })
		wg.Go(func() { relay(background, screening, cmp.Or(cfg.RelayInterval, 5*time.Second)) })
		closers = append(closers, func() { stop(); wg.Wait() })
	}

	return httpapi.NewHandler(httpapi.Services{
		JobOpenings:     usecase.NewJobOpeningService(openingRepo, clock, ids),
		Proposals:       usecase.NewProposalService(deriver),
		Screening:       screening,
		InterviewGuides: usecase.NewInterviewGuideService(guideRepo, openingRepo, profiles, questions, clock, system.IDs{Prefix: "ig_"}),
	}), release, nil
}

// repositories returns the PostgreSQL repositories when a database is configured — connected,
// checked and migrated — or in-memory ones when it is not, and what releases them.
func repositories(ctx context.Context, url string) (usecase.JobOpeningRepository, usecase.ScreeningRepository, func(), error) {
	if url == "" {
		return memory.NewJobOpeningRepository(), memory.NewScreeningRepository(), func() { /* nothing to release */ }, nil
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("configuring database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, nil, fmt.Errorf("connecting to database: %w", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	return postgres.NewJobOpeningRepository(pool), postgres.NewScreeningRepository(pool), pool.Close, nil
}

// resumeStore returns object storage when it is configured, and memory when it is not.
func resumeStore(cfg objectstore.Config) (usecase.ResumeStore, error) {
	if cfg.Endpoint == "" {
		return memory.NewResumeStore(), nil
	}
	store, err := objectstore.New(cfg)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// relay sends, on a timer, the screening requests the broker could not take when their batch
// was submitted — the other half of the outbox. It reports a failure once, not on every tick.
func relay(ctx context.Context, svc *usecase.ScreeningService, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	lastErr := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sent, err := svc.PublishPendingRequests(ctx)
		if sent > 0 {
			slog.Info("held-back screening requests sent", "count", sent)
		}
		switch {
		case err != nil && ctx.Err() == nil && err.Error() != lastErr:
			slog.Warn("screening requests held back; retrying", "err", err)
			lastErr = err.Error()
		case err == nil:
			lastErr = ""
		}
	}
}

// unavailable stands in for a service this deployment was not told how to reach. It fails every
// call with the reason, which is more useful than a nil panic and more honest than an answer
// with nothing in it.
type unavailable struct{ what string }

func (u unavailable) GetCandidateProfile(context.Context, string) (domain.CandidateProfile, error) {
	return domain.CandidateProfile{}, errors.New(u.what)
}

func (u unavailable) GenerateInterviewQuestions(context.Context, domain.CandidateProfile, []domain.Criterion, string) ([]domain.Question, domain.ModelAttribution, error) {
	return nil, domain.ModelAttribution{}, errors.New(u.what)
}

func (u unavailable) DeriveCriteria(context.Context, string, domain.Language) (*domain.JobOpeningProposal, error) {
	return nil, fmt.Errorf("%w: %s", usecase.ErrAIUnavailable, u.what)
}

// noBroker stands in when no broker is configured: batches are accepted, and their requests
// wait for one.
type noBroker struct{}

func (noBroker) RequestScreening(context.Context, *domain.ScreeningBatch, *domain.ScreeningEntry) error {
	return errors.New("RABBITMQ_URL is not set, so screening requests cannot be sent")
}
