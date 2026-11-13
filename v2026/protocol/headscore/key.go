package headscore

import (
	"fmt"
	"strings"
)

// FleetKey is one fleet identity: a fleet's binding generation at the UID its
// hotkey holds. Every raw score, EMA value and transcript record belongs to
// one identity. A new generation, or the UID changing hands, is a new identity
// that starts afresh.
//
// The fields have no JSON tags on purpose: the validator persists this struct
// in its EMA state and measurement transcripts with these exact field names.
type FleetKey struct {
	FleetID    [32]byte
	Hotkey     [32]byte
	Generation uint64
	UID        uint16
}

// String is the identity's canonical text: lowercase hex fleet id and hotkey,
// then the decimal generation and UID, separated by colons. Fold records,
// persisted state and transcripts are ordered by it.
func (k FleetKey) String() string {
	return fmt.Sprintf("%x:%x:%d:%d", k.FleetID, k.Hotkey, k.Generation, k.UID)
}

// CompareKeys is the canonical identity order: the byte order of String. It
// returns a negative number when a comes first.
func CompareKeys(a, b FleetKey) int {
	return strings.Compare(a.String(), b.String())
}
