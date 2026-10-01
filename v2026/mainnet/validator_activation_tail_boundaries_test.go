// Tail-boundary custody preserves prior inventory scope and later partial
// failures without promoting historical views to public launch authority.
package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/validator"
)

// These projection fixtures qualify journal semantics; validator tests derive
// the corresponding boundary census from actual signed producer databases.
func validatorActivationTailBoundaryTestCheckpoint(f *validatorActivationFixture) validatorActivationCommittedCheckpoint {
	checkpoint := validatorActivationUnsealedTestCheckpoint(f)
	for i := range checkpoint.Proof.Unsealed.Ledgers {
		checkpoint.Proof.Unsealed.Ledgers[i].TailBoundaryProof = &validator.ProductionBootstrapUnsealedBoundaryProof{
			Schema: validator.ProductionBootstrapUnsealedBoundarySchema, HistoricalSources: true,
			Boundaries: []validator.ProductionBootstrapUnsealedBoundary{{Boundary: validator.AttemptBoundary{SettlementEpoch: checkpoint.Proof.Prefixes[i].Epoch, EVMBlock: checkpoint.Proof.EvmBlock, EVMBlockHash: checkpoint.Proof.EvmHash}, Records: 1}},
		}
	}
	checkpoint.Proof.ContentHash = ""
	checkpoint.Proof.ContentHash = rootObjectHash(checkpoint.Proof)
	return checkpoint
}

// A later role/read refusal retains exact completed boundaries, original
// clocks and unsealed liabilities through journal reopen and public status.
func TestValidatorActivationTailBoundariesRetainPartialCheckpoint(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := validatorActivationTailBoundaryTestCheckpoint(f)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	result, code, detail := f.command(t.Context(), "admit-committed", nil)
	if code != 3 || result.Status != "source-refused" || len(result.CommittedCheckpoints) != 2 || !reflect.DeepEqual(result.CommittedCheckpoints[0], &checkpoint) || result.CommittedCheckpoints[1] != nil {
		t.Fatal("later refusal erased completed tail boundaries", code, detail)
	}
	status, code, detail := f.command(t.Context(), "status", nil)
	if code != 0 || !reflect.DeepEqual(status.CommittedCheckpoints, result.CommittedCheckpoints) {
		t.Fatal("reopened tail-boundary checkpoint differs", code, detail)
	}
	result, code, detail = f.command(t.Context(), "start", nil)
	if code != 3 || result.Status != "activation-authority-unavailable" || result.Operations != status.Operations || f.starts != [2]int{} || !slices.Contains(validatorActivationCommittedOpenGates(), "UNSEALED_LEDGER_AND_INTENT_STATE_UNVERIFIED") {
		t.Fatal("historical tail boundaries granted intent or launch authority", code, detail)
	}
}

// Earlier inventory-only custody can acquire new authority. Once acquired,
// neither a rehash nor a plausible census may erase or alter an unchanged tail.
func TestValidatorActivationTailBoundariesRejectRetainedRegression(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := validatorActivationUnsealedTestCheckpoint(f)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, original); err != nil {
		t.Fatal(err)
	}
	upgraded := validatorActivationTailBoundaryTestCheckpoint(f)
	upgraded.Proof.PreviousHash, upgraded.Proof.ContentHash = original.Proof.ContentHash, ""
	upgraded.Proof.ContentHash = rootObjectHash(upgraded.Proof)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, upgraded); err != nil {
		t.Fatal("original inventory could not acquire independently observed boundaries", err)
	}
	for _, fault := range []string{"scope", "count", "empty", "hash", "block", "unobserved", "schema"} {
		next := upgraded
		inventory := *next.Proof.Unsealed
		inventory.Ledgers = slices.Clone(inventory.Ledgers)
		proof := *inventory.Ledgers[0].TailBoundaryProof
		proof.Boundaries = slices.Clone(proof.Boundaries)
		inventory.Ledgers[0].TailBoundaryProof = &proof
		next.Proof.Unsealed = &inventory
		next.Proof.PreviousHash = upgraded.Proof.ContentHash
		switch fault {
		case "scope":
			inventory.Ledgers[0].TailBoundaryProof = nil
			inventory.Ledgers[1].TailBoundaryProof = nil
		case "count":
			proof.Boundaries[0].Records++
		case "empty":
			proof.Boundaries = nil
		case "hash":
			proof.Boundaries[0].Boundary.EVMBlockHash = "0x" + strings.Repeat("f", 64)
		case "block":
			proof.Boundaries[0].Boundary.EVMBlock--
		case "unobserved":
			proof.HistoricalSources = false
		case "schema":
			proof.Schema = "unsupported"
		}
		next.Proof.ContentHash = ""
		next.Proof.ContentHash = rootObjectHash(next.Proof)
		if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, next); err == nil {
			t.Fatal("retained historical tail boundary changed", fault)
		}
	}
	retained, err := store.load(t.Context())
	if err != nil || !reflect.DeepEqual(retained.CommittedCheckpoints[0], &upgraded) {
		t.Fatal("refusal replaced complete historical custody", err)
	}
}

// A later real committed cut may consume a formerly unsealed tail. Historical
// scope remains explicit while its complete tail-boundary census becomes empty.
func TestValidatorActivationTailBoundariesAdvanceCommittedCut(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := validatorActivationTailBoundaryTestCheckpoint(f)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, original); err != nil {
		t.Fatal(err)
	}
	next := original
	next.Proof.Prefixes = slices.Clone(next.Proof.Prefixes)
	inventory := *next.Proof.Unsealed
	inventory.Ledgers = slices.Clone(inventory.Ledgers)
	for i := range inventory.Ledgers {
		ledger := &inventory.Ledgers[i]
		// The original unresolved trail must first gain its terminal record.
		ledger.Head.LastSequence++
		ledger.Head.Root = "0x" + strings.Repeat("e", 64)
		ledger.Head.RecordBytes += 50
		next.Proof.Prefixes[i].LastSequence, next.Proof.Prefixes[i].Root = ledger.Head.LastSequence, ledger.Head.Root
		ledger.UnsealedRecords, ledger.PendingTrails, ledger.PendingHash = 0, 0, validator.ReleaseMeasurementContentHash(nil)
		ledger.TailBoundaryProof = &validator.ProductionBootstrapUnsealedBoundaryProof{Schema: validator.ProductionBootstrapUnsealedBoundarySchema, HistoricalSources: true, Boundaries: []validator.ProductionBootstrapUnsealedBoundary{}}
	}
	next.Proof.Unsealed, next.Proof.PreviousHash, next.Proof.ContentHash = &inventory, original.Proof.ContentHash, ""
	next.Proof.ContentHash = rootObjectHash(next.Proof)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, next); err != nil {
		t.Fatal("completed cut could not consume authenticated tail liability", err)
	}
}
