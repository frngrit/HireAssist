// Package mongo stores interview guides in MongoDB. ADR-003 puts AI-derived documents there
// and keeps the relational store for the hiring record itself: a guide is generated text whose
// shape changes as the prompt does, and nothing joins across it.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

// collectionName holds one document per candidate per job opening.
const collectionName = "interview_guides"

// InterviewGuideRepository is the MongoDB adapter for the interview guide port.
type InterviewGuideRepository struct {
	guides *mongo.Collection
}

// Connect opens the connection, checks it, and makes sure the index exists, so a database that
// cannot be reached fails startup rather than the first generated guide. The returned func
// disconnects.
func Connect(ctx context.Context, uri, database string) (*InterviewGuideRepository, func(), error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, nil, fmt.Errorf("configuring MongoDB: %w", err)
	}
	disconnect := func() { _ = client.Disconnect(context.Background()) }

	if err := client.Ping(ctx, nil); err != nil {
		disconnect()
		return nil, nil, fmt.Errorf("connecting to MongoDB: %w", err)
	}

	guides := client.Database(database).Collection(collectionName)
	// A candidate has one guide per opening; regenerating revises it. The database is what
	// makes that true even if two requests arrive at once.
	_, err = guides.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "job_opening_id", Value: 1}, {Key: "candidate_id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("one_guide_per_candidate_per_opening"),
	})
	if err != nil {
		disconnect()
		return nil, nil, fmt.Errorf("creating the interview guide index: %w", err)
	}
	return &InterviewGuideRepository{guides: guides}, disconnect, nil
}

// guideDoc is one stored guide. The field names are the document's, not Go's, so what is in the
// database can be read without the code beside it.
type guideDoc struct {
	ID           string         `bson:"_id"`
	JobOpeningID string         `bson:"job_opening_id"`
	CandidateID  string         `bson:"candidate_id"`
	Questions    []questionDoc  `bson:"questions"`
	Emphasis     string         `bson:"emphasis,omitempty"`
	Source       string         `bson:"source"`
	Attribution  attributionDoc `bson:"attribution"`
	Revision     int            `bson:"revision"`
	CreatedAt    time.Time      `bson:"created_at"`
	UpdatedAt    time.Time      `bson:"updated_at"`
}

type questionDoc struct {
	ID        string `bson:"id"`
	Text      string `bson:"text"`
	Criterion string `bson:"criterion"`
	Evidence  string `bson:"evidence,omitempty"`
	Kind      string `bson:"kind"`
	// Note is what the interviewer wrote against the question afterwards (FR-3.5).
	Note string `bson:"note,omitempty"`
}

// attributionDoc records which model wrote the questions (ADR-004). A guide the Recruiter
// edited has none, and the empty document says so.
type attributionDoc struct {
	Provider      string `bson:"provider,omitempty"`
	Model         string `bson:"model,omitempty"`
	PromptVersion string `bson:"prompt_version,omitempty"`
}

// Save writes the guide, replacing the one for the same candidate and opening.
func (r *InterviewGuideRepository) Save(ctx context.Context, g *domain.InterviewGuide) error {
	filter := bson.D{{Key: "job_opening_id", Value: g.JobOpeningID}, {Key: "candidate_id", Value: g.CandidateID}}
	_, err := r.guides.ReplaceOne(ctx, filter, toDoc(g), options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("writing the interview guide: %w", err)
	}
	return nil
}

// FindByCandidate returns the stored guide, or usecase.ErrGuideNotFound.
func (r *InterviewGuideRepository) FindByCandidate(ctx context.Context, jobOpeningID, candidateID string) (*domain.InterviewGuide, error) {
	filter := bson.D{{Key: "job_opening_id", Value: jobOpeningID}, {Key: "candidate_id", Value: candidateID}}

	var doc guideDoc
	err := r.guides.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, usecase.ErrGuideNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the interview guide: %w", err)
	}
	return toDomain(doc), nil
}

func toDoc(g *domain.InterviewGuide) guideDoc {
	questions := make([]questionDoc, len(g.Questions))
	for i, q := range g.Questions {
		questions[i] = questionDoc{ID: q.ID, Text: q.Text, Criterion: q.Criterion, Evidence: q.Evidence, Kind: string(q.Kind), Note: q.Note}
	}
	return guideDoc{
		ID:           g.ID,
		JobOpeningID: g.JobOpeningID,
		CandidateID:  g.CandidateID,
		Questions:    questions,
		Emphasis:     g.Emphasis,
		Source:       string(g.Source),
		Attribution: attributionDoc{
			Provider:      g.Attribution.Provider,
			Model:         g.Attribution.Model,
			PromptVersion: g.Attribution.PromptVersion,
		},
		Revision:  g.Revision,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	}
}

func toDomain(doc guideDoc) *domain.InterviewGuide {
	questions := make([]domain.Question, len(doc.Questions))
	for i, q := range doc.Questions {
		questions[i] = domain.Question{
			ID:        q.ID,
			Text:      q.Text,
			Criterion: q.Criterion,
			Evidence:  q.Evidence,
			Kind:      domain.QuestionKind(q.Kind),
			Note:      q.Note,
		}
	}
	return &domain.InterviewGuide{
		ID:           doc.ID,
		JobOpeningID: doc.JobOpeningID,
		CandidateID:  doc.CandidateID,
		Questions:    questions,
		Emphasis:     doc.Emphasis,
		Source:       domain.GuideSource(doc.Source),
		Attribution: domain.ModelAttribution{
			Provider:      doc.Attribution.Provider,
			Model:         doc.Attribution.Model,
			PromptVersion: doc.Attribution.PromptVersion,
		},
		Revision: doc.Revision,
		// Mongo stores milliseconds; UTC so a guide reads the same wherever it is opened.
		CreatedAt: doc.CreatedAt.UTC(),
		UpdatedAt: doc.UpdatedAt.UTC(),
	}
}
