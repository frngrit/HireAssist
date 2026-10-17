// Package system provides production implementations of the use cases' Clock and id ports.
package system

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Clock reads the wall clock, in UTC, to the microsecond — the precision PostgreSQL stores.
type Clock struct{}

func (Clock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// RandomIDs issues the ids of the hiring record: "jo_" for job openings, "sb_" for screening
// batches, "rs_" for resumes.
type RandomIDs struct{}

func (RandomIDs) NewID() string { return IDs{Prefix: "jo_"}.NewID() }

func (RandomIDs) NewBatchID() string { return IDs{Prefix: "sb_"}.NewID() }

func (RandomIDs) NewResumeID() string { return IDs{Prefix: "rs_"}.NewID() }

// IDs issues 128-bit random identifiers with a prefix naming what they identify, so an id seen
// in a log or a URL says what it belongs to.
type IDs struct{ Prefix string }

func (i IDs) NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return i.Prefix + hex.EncodeToString(b)
}
