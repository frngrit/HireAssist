package resumeproc_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/resumeproc"
	"github.com/frngrit/HireAssist/services/hiring/internal/domain"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

var _ usecase.Justifications = (*resumeproc.CandidateProfiles)(nil)

// A candidate screened twice, by two batches. Synthetic.
const screenedBody = `{
  "id": "cand-001",
  "skills": ["Go"],
  "screenings": [
    {"batch_id": "sb-2", "resume_id": "rs-9", "status": "scored", "score": 40,
     "justification": "A later batch.", "assessments": []},
    {"batch_id": "sb-1", "resume_id": "rs-1", "status": "scored", "score": 78,
     "justification": "Six years of Go at two companies; no Kafka in production.",
     "assessments": [
       {"criterion_id": "c1", "met": true, "score": 90, "evidence": "Go at Wongnai and LINE MAN"},
       {"criterion_id": "c2", "met": false, "score": 20, "evidence": "no message broker mentioned"},
       {"criterion_id": "go-skills", "met": true, "score": 90, "evidence": "an id this service never sent"}
     ]}
  ]
}`

// The screening of the resume asked about, out of every screening of the candidate, with each
// verdict matched to the criterion it judges.
func TestGetJustification(t *testing.T) {
	j, err := serving(t, http.StatusOK, screenedBody).GetJustification(context.Background(), "cand-001", "sb-1", "rs-1")

	if err != nil {
		t.Fatal(err)
	}
	want := domain.Justification{
		Text: "Six years of Go at two companies; no Kafka in production.",
		Evidence: []domain.CriterionEvidence{
			{Criterion: 0, Met: true, Score: 90, Evidence: "Go at Wongnai and LINE MAN"},
			{Criterion: 1, Met: false, Score: 20, Evidence: "no message broker mentioned"},
		},
	}
	if !reflect.DeepEqual(j, want) {
		t.Errorf("justification =\n%+v\nwant\n%+v", j, want)
	}
}

func TestGetJustification_NotThere(t *testing.T) {
	if _, err := serving(t, http.StatusOK, screenedBody).GetJustification(context.Background(), "cand-001", "sb-1", "rs-404"); !errors.Is(err, resumeproc.ErrNoScreening) {
		t.Errorf("unscreened resume: err = %v", err)
	}
	if _, err := serving(t, http.StatusNotFound, `{}`).GetJustification(context.Background(), "cand-001", "sb-1", "rs-1"); !errors.Is(err, usecase.ErrCandidateNotFound) {
		t.Errorf("unknown candidate: err = %v", err)
	}
}
