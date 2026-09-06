package main

// Observe the actual cold fixture's independent work boundaries without
// replacing any signed artifact construction or sharing mutable test inputs.

import (
	"slices"
	"testing"
)

const (
	finalSemanticFixtureValidatorLanes     = "validator-lanes"
	finalSemanticFixtureTerminalClosures   = "terminal-closures"
	finalSemanticFixtureGenerationCalldata = "generation-calldata"
)

// A batch names the exact contiguous job slots admitted before their join.
// Observers belong to the constructor call and run on its owning goroutine.
type finalSemanticFixtureWorkBatch struct {
	stage string
	first int
	count int
}

// Construction reports work without granting the observer any artifact,
// signing-key, verification-result or dispatch authority.
type finalSemanticFixtureWorkObserver func(finalSemanticFixtureWorkBatch)

// Keep the observation at each real dispatch boundary; nil is the ordinary
// fixture path and must not change work or error behavior.
func observeFinalSemanticFixtureWork(observer finalSemanticFixtureWorkObserver, stage string, first, count int) {
	if observer != nil {
		observer(finalSemanticFixtureWorkBatch{stage: stage, first: first, count: count})
	}
}

// Read only the already-published census, with no backing-array alias to the
// existing cold graph's immutable metadata and no out-of-band initialization.
func finalSemanticFixtureWorkBatches() []finalSemanticFixtureWorkBatch {
	finalSemanticFixtureCache.stateLock.Lock()
	defer finalSemanticFixtureCache.stateLock.Unlock()
	return append([]finalSemanticFixtureWorkBatch(nil), finalSemanticFixtureCache.workBatches...)
}

// The same complete constructor used by the cold snapshot and durable-wire
// controls must admit independent lanes before joining them. These counts
// measure dispatch, not wall time, and retain all 32 original real jobs plus
// all 202 independent fleet preparations.
func TestFinalSemanticFixtureDispatchesIndependentWork(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	if source.ExpectedMiners != 1000 || source.ExpectedCandidates != 202 || source.ExpectedHeadSlots != 200 || len(source.Validators) != 2 {
		t.Fatal("cold dispatch observation lost the complete release population")
	}
	for _, validator := range source.Validators {
		if len(validator.Cycles) != 5 {
			t.Fatalf("validator %d has %d cycles, want all five", validator.ValidatorID, len(validator.Cycles))
		}
	}
	// These wires contain only deterministic measurements, Ed25519 records
	// and ordered proof projections. Envelope/intent signatures are excluded.
	type byteReference struct {
		validatorID uint64
		noID        uint64
		firstEpoch  uint64
		lastEpoch   uint64
		locator     FinalArtifactLocator
	}
	referenceKVs := make(map[string]byteReference, 24)
	addReference := func(reference byteReference) {
		if prior, found := referenceKVs[reference.locator.URI]; found && prior != reference {
			t.Fatalf("deterministic fixture reference %s has conflicting identities", reference.locator.URI)
		}
		referenceKVs[reference.locator.URI] = reference
	}
	for _, validator := range source.Validators {
		for _, cycle := range validator.Cycles {
			addReference(byteReference{validatorID: validator.ValidatorID, firstEpoch: cycle.SettlementEpoch, lastEpoch: cycle.SettlementEpoch, locator: cycle.MeasurementArtifact})
		}
	}
	for _, proof := range source.PathProofs {
		addReference(byteReference{validatorID: proof.ValidatorID, noID: proof.NoID, firstEpoch: proof.FirstEpoch, lastEpoch: proof.LastEpoch, locator: proof.Artifact})
		for _, closure := range proof.SettlementClosures {
			addReference(byteReference{validatorID: proof.ValidatorID, firstEpoch: closure.Epoch, lastEpoch: closure.Epoch, locator: closure.Artifact})
		}
	}
	kindCounts := map[string]int{}
	uriNames := make([]string, 0, len(referenceKVs))
	for uri, reference := range referenceKVs {
		kindCounts[reference.locator.Kind]++
		uriNames = append(uriNames, uri)
	}
	if len(referenceKVs) != 24 || len(kindCounts) != 3 || kindCounts["validator-release-measurement"] != 10 || kindCounts["validator-settlement-closure"] != 10 || kindCounts["validator-path-proofs"] != 4 {
		t.Fatalf("deterministic fixture reference census=%v/%d, want measurements=10 closures=10 proofs=4", kindCounts, len(referenceKVs))
	}
	slices.Sort(uriNames)
	for _, uri := range uriNames {
		reference := referenceKVs[uri]
		data, found := artifacts[uri]
		hash := bytesSHA256(data)
		if !found || len(data) == 0 || reference.locator.SizeBytes != uint64(len(data)) || reference.locator.ContentHash != hash {
			t.Fatalf("deterministic fixture reference %s differs from its complete raw bytes", uri)
		}
		t.Logf("cold fixture deterministic-reference deployment=%q chain=%d genesis=%s netuid=%d policy=%s kind=%q validator=%d uid=%d no=%d first=%d last=%d uri=%q bytes=%d hash=%s", source.DeploymentID, source.ChainID, source.GenesisHash, source.Netuid, source.PolicyHash, reference.locator.Kind, reference.validatorID, finalValidatorUID(&source, reference.validatorID), reference.noID, reference.firstEpoch, reference.lastEpoch, uri, len(data), hash)
	}
	batches := finalSemanticFixtureWorkBatches()
	type stageCensus struct {
		jobs  int
		width int
	}
	census := map[string]stageCensus{}
	for _, batch := range batches {
		prior := census[batch.stage]
		if batch.count <= 0 || batch.first != prior.jobs {
			t.Fatalf("cold fixture dispatch %s starts at %d with %d jobs after %d", batch.stage, batch.first, batch.count, prior.jobs)
		}
		census[batch.stage] = stageCensus{jobs: prior.jobs + batch.count, width: max(prior.width, batch.count)}
	}
	if len(census) != 4 {
		t.Fatalf("cold fixture dispatch observed %d stages, want all four", len(census))
	}
	for _, want := range []struct {
		stage string
		jobs  int
		width int
	}{
		{stage: finalSemanticFixtureValidatorLanes, jobs: 2, width: 2},
		{stage: finalSemanticFixtureTerminalClosures, jobs: 10, width: 4},
		{stage: finalSemanticFixtureGenerationCalldata, jobs: 20, width: 4},
		{stage: finalSemanticFixtureFleetAssembly, jobs: 202, width: 4},
	} {
		got := census[want.stage]
		if got.jobs != want.jobs {
			t.Fatalf("cold fixture %s executed %d jobs, want all %d", want.stage, got.jobs, want.jobs)
		}
		if got.width != want.width {
			t.Errorf("cold fixture %s dispatch width=%d, want %d independent jobs", want.stage, got.width, want.width)
		}
	}
}

// Callers cannot rewrite the observed constructor's dispatch census or alter
// another reader while the complete fixture remains shared as immutable wire.
func TestFinalSemanticFixtureDispatchObservationIsDetached(t *testing.T) {
	t.Parallel()
	_, _ = finalSemanticFixture(t)
	first, second := finalSemanticFixtureWorkBatches(), finalSemanticFixtureWorkBatches()
	if len(first) == 0 || !slices.Equal(first, second) {
		t.Fatal("cold fixture dispatch readers did not receive the same complete census")
	}
	first[0].stage = "mutated-reader"
	first[0].first++
	first[0].count++
	third := finalSemanticFixtureWorkBatches()
	if !slices.Equal(second, third) {
		t.Fatal("cold fixture dispatch reader mutated the published census")
	}
}
