//go:build linux || darwin

// A single validator measuring a single operator owns one provider lane whose
// only public replica is that operator's own origin. The cross-validator
// window and the economic source read it with the same complete replay.
package validator

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// The single lane's real complete/failed trails replay from its only origin.
// A locator naming another origin census is refused before any public read.
func TestProviderAttemptWindowSingleOperatorReplaysItsOnlyLane(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixtureForCensus(t, true, []byte{51}, []uint64{9})
	owner := fixture.owners[0]
	if len(fixture.owners) != 1 || len(owner.read.Origins) != 1 || !slices.Equal(owner.manifest.Origins, owner.read.Origins) || len(owner.manifest.Members) != 1 {
		t.Fatal("single-operator window fixture does not name exactly one origin and lane")
	}
	result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, fixture.options(t))
	if err != nil || result == nil || !result.CutCensusComplete || result.OwnedRequestsComplete || result.Validators != 1 || result.OperatorLanes != 1 || result.CompleteTrails != 1 || result.FailedTrails != 1 {
		t.Fatalf("single-lane original replay differs: %+v %v", result, err)
	}
	var assignments, confirmations uint64
	for _, row := range result.Providers {
		if row.NoId != 9 {
			t.Fatal("single-lane replay invented another operator's provider")
		}
		assignments += row.Assignments
		confirmations += row.Confirmations
	}
	if assignments != 8 || confirmations != 7 {
		t.Fatalf("single-lane failed exposure was omitted or double-counted: %d/%d", assignments, confirmations)
	}
	before := owner.fixture.counts()
	for _, origins := range [][]string{nil, {}, append(slices.Clone(owner.read.Origins), "https://padded.example")} {
		candidate := fixture.candidate
		candidate.Members = slices.Clone(fixture.candidate.Members)
		candidate.Members[0].Manifest.Origins = origins
		if result, err := VerifyProviderAttemptWindow(t.Context(), candidate, fixture.options(t)); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
			t.Fatalf("single-lane locator with %d origins admitted: %v", len(origins), err)
		}
	}
	if owner.fixture.counts() != before {
		t.Fatal("refused locator origin census performed public reads")
	}
}

// The economic source acquires and retains a single lane's originals from its
// only origin. Cold verification needs no live endpoint and matches exactly.
func TestProviderAttemptSourceSingleOperatorReplaysItsOnlyOrigin(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixtureForCensus(t, true, false, []byte{51}, []uint64{9})
	origins := fixture.response.Authority.Validators[0].Publication.Origins
	if len(fixture.base.owners) != 1 || len(fixture.response.Authority.Validators) != 1 || len(origins) != 1 || !slices.Equal(fixture.base.owners[0].manifest.Origins, origins) {
		t.Fatal("single-operator source fixture does not name exactly one origin")
	}
	raw, result, err := fixture.source.Read(t.Context(), fixture.artifact)
	if err != nil || result == nil || !result.CutCensusComplete || !result.OwnedRequestsComplete || result.Validators != 1 || result.OperatorLanes != 1 || result.FailedTrails != 1 || result.ReliabilityAMin == 0 || result.OriginalHash != sha256.Sum256(raw) {
		t.Fatalf("single-origin provider originals: %+v %v", result, err)
	}
	var original ProviderAttemptOriginal
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	if len(original.Objects) == 0 {
		t.Fatal("single-origin original retained no public object")
	}
	for _, object := range original.Objects {
		if object.Origin != origins[0] {
			t.Fatalf("single-origin original captured a foreign origin %q", object.Origin)
		}
	}
	fixture.stateLock.Lock()
	fixture.closed = true
	reads := fixture.reads
	fixture.stateLock.Unlock()
	cold, err := fixture.source.VerifyRetained(t.Context(), fixture.artifact, raw)
	if err != nil || cold == nil || cold.OriginalHash != result.OriginalHash || cold.WindowHash != result.WindowHash || cold.Validators != 1 || cold.OperatorLanes != 1 {
		t.Fatalf("single-origin cold replay changed: %+v %v", cold, err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if fixture.reads != reads {
		t.Fatal("single-origin cold replay consulted a live source")
	}
}

// Even a correctly re-signed selection cannot give a single lane an empty,
// padded or aliased origin census; each is an integrity refusal.
func TestProviderAttemptSourceSingleOperatorRefusesOtherOriginCensus(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixtureForCensus(t, false, false, []byte{51}, []uint64{9})
	original := slices.Clone(fixture.response.Authority.Validators[0].Publication.Origins)
	for _, origins := range [][]string{nil, {}, append(slices.Clone(original), "https://padded.example"), {original[0], original[0]}} {
		fixture.response.Authority.Validators[0].Publication.Origins = origins
		fixture.resign(t)
		if raw, result, err := fixture.source.Read(t.Context(), fixture.artifact); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || raw != nil || result != nil {
			t.Fatalf("single lane admitted a %d-origin census: %v", len(origins), err)
		}
	}
	fixture.response.Authority.Validators[0].Publication.Origins = original
	fixture.resign(t)
	if raw, result, err := fixture.source.Read(t.Context(), fixture.artifact); err != nil || raw == nil || result == nil || result.OperatorLanes != 1 {
		t.Fatalf("exact single-origin selection did not recover: %v", err)
	}
}
