package system_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/system"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

var (
	_ usecase.Clock       = system.Clock{}
	_ usecase.IDGenerator = system.RandomIDs{}
)

// Opening dates are recorded in UTC, so every service and report agrees on the date.
func TestClock_NowIsCurrentUTC(t *testing.T) {
	before := time.Now()

	got := system.Clock{}.Now()

	if got.Location() != time.UTC {
		t.Errorf("Location = %v, want UTC", got.Location())
	}
	if got.Before(before.Add(-time.Second)) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("Now = %v, not the current time", got)
	}
}

// Times carry no more precision than PostgreSQL stores, so an opening reads back exactly as
// it was created.
func TestClock_NowIsWholeMicroseconds(t *testing.T) {
	got := system.Clock{}.Now()

	if got.Nanosecond()%1000 != 0 {
		t.Errorf("Now = %v, has sub-microsecond precision", got)
	}
}

// Job opening ids are unique and unguessable: they appear in URLs, and a sequential id
// would let anyone enumerate every opening.
func TestRandomIDs_UniqueAndWellFormed(t *testing.T) {
	format := regexp.MustCompile(`^jo_[0-9a-f]{32}$`)
	seen := map[string]bool{}

	for i := 0; i < 1000; i++ {
		id := system.RandomIDs{}.NewID()
		if !format.MatchString(id) {
			t.Fatalf("id %q does not match %s", id, format)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

var _ usecase.ScreeningIDs = system.RandomIDs{}

// Batch and resume ids follow the same rule, each with its own prefix.
func TestRandomIDs_BatchAndResumeIDs(t *testing.T) {
	for prefix, newID := range map[string]func() string{
		"sb": system.RandomIDs{}.NewBatchID,
		"rs": system.RandomIDs{}.NewResumeID,
	} {
		format := regexp.MustCompile(`^` + prefix + `_[0-9a-f]{32}$`)
		if a, b := newID(), newID(); !format.MatchString(a) || a == b {
			t.Errorf("%s ids %q and %q: want two different ids matching %s", prefix, a, b, format)
		}
	}
}
