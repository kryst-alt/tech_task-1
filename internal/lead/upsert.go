package lead

import (
	"strings"

	"github.com/google/uuid"
)

// Fields are the partner-supplied attributes we store and keep up to date.
type Fields struct {
	Name, Email, Company, Source string
}

// Incoming is a lead as received from the partner.
type Incoming struct {
	ExternalID string
	Fields
}

// Record is a stored lead as far as the upsert decision needs it.
type Record struct {
	ID uuid.UUID
	Fields
}

type Action int

const (
	Create Action = iota
	Update
	Unchanged
	Conflict
)

func (a Action) String() string {
	return [...]string{"create", "update", "unchanged", "conflict"}[a]
}

// Decide picks what to do with an incoming lead given the stored leads that
// share its external ID and its email. Either may be nil. Update and Unchanged
// return the record to act on.
func Decide(byID, byEmail *Record, in Incoming) (Action, *Record) {
	target := byID
	if target == nil {
		target = byEmail
	}
	switch {
	case target == nil:
		return Create, nil
	case byID != nil && byEmail != nil && byID.ID != byEmail.ID:
		return Conflict, nil
	case sameFields(target.Fields, in.Fields):
		return Unchanged, target
	default:
		return Update, target
	}
}

func sameFields(a, b Fields) bool {
	return a.Name == b.Name && strings.EqualFold(a.Email, b.Email) && a.Company == b.Company && a.Source == b.Source
}
