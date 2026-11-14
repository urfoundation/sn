// Unsealed inventories remain separate from authenticated committed cuts and
// cannot be erased by a later partial admission or a rehashed projection.
package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/validator"
)

// Synthetic projection fields test custody only; the validator package uses
// real signed producer databases to qualify the corresponding source reader.
func validatorActivationUnsealedTestCheckpoint(f *validatorActivationFixture) validatorActivationCommittedCheckpoint {
	checkpoint := validatorActivationCommittedTestCheckpoint(f, 0, validatorActivationHealthTestCheckpoint(f, 0))
	checkpoint.Proof.Unsealed = &validator.ProductionBootstrapUnsealedObservation{Schema: validator.ProductionBootstrapUnsealedSchema, CensusHash: "sha256:" + strings.Repeat("c", 64), SourceCount: 15, SourceBytes: 1024, IntentFilePresent: true, IntentFileHash: "sha256:" + strings.Repeat("d", 64)}
	for _, prefix := range checkpoint.Proof.Prefixes {
		checkpoint.Proof.Unsealed.Ledgers = append(checkpoint.Proof.Unsealed.Ledgers, validator.ProductionBootstrapUnsealedLedger{NoId: prefix.NoId,
			Head: validator.AttemptLedgerHead{LastSequence: prefix.LastSequence + 1, Root: "0x" + strings.Repeat("a", 64), RecordBytes: 100, TrailCount: 1}, UnsealedRecords: 1, PendingTrails: 1, PendingHash: "sha256:" + strings.Repeat("b", 64)})
	}
	checkpoint.Proof.ContentHash = ""
	checkpoint.Proof.ContentHash = rootObjectHash(checkpoint.Proof)
	return checkpoint
}

// The original clock and complete liability survive both a later role's read
// failure and reopening. Public start still has no activation capability.
func TestValidatorActivationUnsealedRetainsPartialCheckpoint(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(f.chain.storageContext(t.Context()), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := validatorActivationUnsealedTestCheckpoint(f)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	result, code, detail := f.command(t.Context(), "admit-committed", nil)
	if code != 3 || result.Status != "source-refused" || len(result.CommittedCheckpoints) != 2 || !reflect.DeepEqual(result.CommittedCheckpoints[0], &checkpoint) || result.CommittedCheckpoints[1] != nil {
		t.Fatal("partial read erased signed ledger liability", code, detail)
	}
	status, code, detail := f.command(t.Context(), "status", nil)
	if code != 0 || !reflect.DeepEqual(status.CommittedCheckpoints, result.CommittedCheckpoints) {
		t.Fatal("reopened liability differs", code, detail)
	}
	result, code, detail = f.command(t.Context(), "start", nil)
	if code != 3 || result.Status != "activation-authority-unavailable" || result.Operations != status.Operations || f.starts != [2]int{} {
		t.Fatal("unsealed inventory authorized a public start", code, detail)
	}
	if !slices.Contains(validatorActivationCommittedOpenGates(), "UNSEALED_LEDGER_AND_INTENT_STATE_UNVERIFIED") {
		t.Fatal("local signed ledger inventory was promoted to complete historical intent authority")
	}
}

// Checksums cannot shorten a lifetime, reset an empty intent boundary, change
// pending work under the same head or drop the new scope on re-observation.
func TestValidatorActivationUnsealedRejectsRetainedRegression(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(f.chain.storageContext(t.Context()), f.approval, f.key, false, f.now)
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
	for _, fault := range []string{"scope", "intent", "intent-hash", "root", "bytes", "trails", "pending", "pending-hash", "census", "schema"} {
		next := original
		inventory := *original.Proof.Unsealed
		inventory.Ledgers = slices.Clone(inventory.Ledgers)
		next.Proof.Unsealed, next.Proof.PreviousHash = &inventory, original.Proof.ContentHash
		switch fault {
		case "scope":
			next.Proof.Unsealed = nil
		case "intent":
			inventory.IntentFilePresent, inventory.IntentFileHash = false, ""
		case "intent-hash":
			inventory.IntentFileHash = "sha256:" + strings.Repeat("e", 64)
		case "root":
			inventory.Ledgers[0].Head.Root = "0x" + strings.Repeat("f", 64)
		case "bytes":
			inventory.Ledgers[0].Head.RecordBytes--
		case "trails":
			inventory.Ledgers[0].Head.TrailCount--
		case "pending":
			inventory.Ledgers[0].PendingTrails = 0
		case "pending-hash":
			inventory.Ledgers[0].PendingHash = "sha256:" + strings.Repeat("f", 64)
		case "census":
			inventory.SourceCount = 8193
		case "schema":
			inventory.Schema = "unsupported"
		}
		next.Proof.ContentHash = ""
		next.Proof.ContentHash = rootObjectHash(next.Proof)
		if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, next); err == nil {
			t.Fatal("retained unsealed liability changed", fault)
		}
	}
	retained, err := store.load(t.Context())
	if err != nil || !reflect.DeepEqual(retained.CommittedCheckpoints[0], &original) {
		t.Fatal("refusal replaced complete prior liability", err)
	}
}
