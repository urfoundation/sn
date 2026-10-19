//go:build linux || darwin

// A one-operator deployment's bootstrap checkpoints prove that operator's
// complete census: its approved prefix, its committed replay at its only
// public origin, and its unsealed tail. No checkpoint can add or drop a member.
package validator

import (
	"bytes"
	"os"
	"slices"
	"testing"
)

// The approved prefix replays the only signed history without network reads.
// A padded single-operator or truncated two-operator observation is refused.
func TestProductionBootstrapPrefixReplaysSingleOperatorCensus(t *testing.T) {
	f := newReleaseBootstrapV2TestFixture(t, releaseSingleOperatorV2TestConfig(t))
	observed := productionBootstrapPrefixTestInputs(t, f)
	before := f.calls()
	inputs, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, observed)
	if err != nil || len(inputs) != 1 {
		t.Fatalf("single-operator prefix inputs: %v", err)
	}
	got, err := replayProductionBootstrapPrefix(t.Context(), &f.cfg, inputs, nil, observed)
	if err != nil || got == nil || len(got.Prefixes) != 1 || got.Prefixes[0].NoId != f.cfg.Operators[0].NoID || got.Prefixes[0].LastSequence != 0 || got.Prefixes[0].Root != zeroAttemptHash() || f.calls() != before {
		t.Fatal("single-operator approved prefix differs", got, err)
	}
	padded := observed
	padded.Operators = append(slices.Clone(observed.Operators), observed.Operators[0])
	if inputs, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, padded); err == nil || inputs != nil || f.calls() != before {
		t.Fatal("single-operator prefix admitted a padded operator observation")
	}
	pair := newReleaseBootstrapV2TestFixture(t)
	truncated := productionBootstrapPrefixTestInputs(t, pair)
	truncated.Operators = truncated.Operators[:1]
	if inputs, err := readProductionBootstrapPrefixInputs(t.Context(), &pair.cfg, truncated); err == nil || inputs != nil {
		t.Fatal("two-operator prefix admitted a truncated observation")
	}
}

// Capture and complete archive replay read the only origin's tapes. The
// committed checkpoint keeps exactly one prefix, and neither the approved nor
// a previous checkpoint can carry another member.
func TestProductionBootstrapCommittedCapturesAndReplaysSingleOrigin(t *testing.T) {
	f, observed, approved, reads := productionBootstrapCommittedTestFixture(t, releaseSingleOperatorV2TestConfig(t))
	if len(f.options.Origins) != 1 || len(observed.Operators) != 1 || len(approved.Prefixes) != 1 {
		t.Fatal("single-operator committed fixture does not have exactly one member")
	}
	before := releaseArchiveV2TestPrivateFiles(t, f.options.Config.StateDir)
	history, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), f.options.Config.StateDir, f.options.Config.EvidenceV2.Bounds, uint32(os.Geteuid()), 8192, true, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := history.close(); err != nil {
			t.Error(err)
		}
	}()
	options, err := captureProductionBootstrapCommitted(t.Context(), f.options.Config, observed, history, f.options.ScratchRoot)
	if err != nil || !slices.Equal(options.Origins, f.options.Origins) {
		t.Fatalf("single-origin committed capture: %v", err)
	}
	for _, source := range options.Sources {
		if source.Source.Origin != "" && source.Source.Origin != options.Origins[0] {
			t.Fatalf("single-origin capture retained a foreign origin: %+v", source.Source)
		}
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	got, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, approved, nil, uint32(os.Geteuid()))
	if err != nil || got == nil || len(got.Prefixes) != 1 || got.ApprovedPrefixHash != approved.ContentHash || reads[0].Load() < 3 || reads[1].Load() != 0 {
		t.Fatal("single-origin committed replay scope differs", got, err)
	}
	cursor := archive.history.current[got.Prefixes[0].NoId]
	if got.Prefixes[0].LastSequence <= approved.Prefixes[0].LastSequence || got.Prefixes[0].Root != cursor.lastRoot || got.Prefixes[0].Generation != cursor.generation || len(archive.history.terminals) != 2 {
		t.Fatal("single-origin projection bypassed actual current replay", got.Prefixes[0])
	}
	if !releaseArchiveV2TestSameFiles(before, releaseArchiveV2TestPrivateFiles(t, f.options.Config.StateDir)) {
		t.Fatal("single-origin committed replay changed live state")
	}
	got.HistoricalSources = true
	got.ContentHash = productionBootstrapPrefixHash(*got)
	if _, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, approved, got, uint32(os.Geteuid())); err != nil {
		t.Fatal("retained identical single-operator checkpoint", err)
	}
	previous := *got
	previous.Prefixes = append(slices.Clone(got.Prefixes), got.Prefixes[0])
	previous.ContentHash = ""
	previous.ContentHash = productionBootstrapPrefixHash(previous)
	if result, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, approved, &previous, uint32(os.Geteuid())); result != nil || err == nil {
		t.Fatal("single-operator checkpoint extended a padded previous census")
	}
	padded := approved
	padded.Prefixes = append(slices.Clone(approved.Prefixes), approved.Prefixes[0])
	padded.ContentHash = ""
	padded.ContentHash = productionBootstrapPrefixHash(padded)
	if result, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, padded, nil, uint32(os.Geteuid())); result != nil || err == nil {
		t.Fatal("single-operator checkpoint extended a padded approved census")
	}
}

// The only origin must authenticate the exact record and proof bodies; with
// no second replica, a corrupted single tape refuses the complete capture.
func TestProductionBootstrapCommittedRejectsOnlyOriginSubstitution(t *testing.T) {
	for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		f, observed, _, reads := productionBootstrapCommittedTestFixture(t, releaseSingleOperatorV2TestConfig(t))
		changed := false
		for source, raw := range f.files {
			if source.Kind == kind && source.Origin == f.startup.replicas[0].Origin {
				value := bytes.Clone(raw)
				value[0] ^= 1
				f.files[source] = value
				changed = true
			}
		}
		if !changed {
			t.Fatal("single-operator fixture lacks its only-origin tape", kind)
		}
		history, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), f.options.Config.StateDir, f.options.Config.EvidenceV2.Bounds, uint32(os.Geteuid()), 8192, true, releaseMeasurementInputV2ReadHooks{})
		if err != nil {
			t.Fatal(err)
		}
		_, captureErr := captureProductionBootstrapCommitted(t.Context(), f.options.Config, observed, history, f.options.ScratchRoot)
		if err := history.close(); err != nil {
			t.Fatal(err)
		}
		if captureErr == nil || reads[0].Load() < 2 || reads[1].Load() != 0 {
			t.Fatal("only-origin substitution admitted", kind, captureErr)
		}
	}
}

// The single operator's unsealed tail, including its unfinished trail, is
// retained as liability and its boundaries are authenticated. The inventory
// validates against exactly that one committed prefix and no other census.
func TestProductionBootstrapUnsealedSingleOperatorAuthenticatesTail(t *testing.T) {
	f, archive, current, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t, releaseSingleOperatorV2TestConfig(t))
	if len(current.Prefixes) != 1 || len(inventory.Ledgers) != 1 || inventory.Ledgers[0].UnsealedRecords != 9 || inventory.Ledgers[0].PendingTrails != 1 {
		t.Fatal("single-operator unsealed tail or pending liability differs", inventory)
	}
	if err := inventory.Validate(current.Prefixes); err != nil {
		t.Fatal(err)
	}
	rpcFixture := &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}
	chain := productionBootstrapUnsealedBoundaryTestChain(t, rpcFixture, nil)
	got, err := owner.authenticateTailBoundaries(t.Context(), archive, inventory, chain)
	if err != nil || got == nil || rpcFixture.operatorReads.Load() != 2 {
		t.Fatal("single-operator historical tail census", got, rpcFixture.operatorReads.Load(), err)
	}
	proof := got.Ledgers[0].TailBoundaryProof
	if proof == nil || !proof.HistoricalSources || len(proof.Boundaries) != 2 || proof.Boundaries[0].Records != 8 || proof.Boundaries[1].Records != 1 {
		t.Fatal("single-operator boundary proof lost signed tail coverage", got.Ledgers[0])
	}
	if err := got.Validate(current.Prefixes); err != nil {
		t.Fatal(err)
	}
	for _, prefixes := range [][]ProductionBootstrapOperatorPrefix{nil, append(slices.Clone(current.Prefixes), current.Prefixes[0])} {
		if err := got.Validate(prefixes); err == nil {
			t.Fatalf("single-ledger inventory validated against %d prefixes", len(prefixes))
		}
	}
	padded := *got
	padded.Ledgers = append(slices.Clone(got.Ledgers), got.Ledgers[0])
	if err := padded.Validate(current.Prefixes); err == nil {
		t.Fatal("padded unsealed inventory validated against one prefix")
	}
}
