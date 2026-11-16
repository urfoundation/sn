//go:build linux || darwin

package validator

// The real ordinary runtime writes and recovers compact private inputs. Full
// M8 cryptographic replay is retained; separate byte/path controls test storage
// and decoder contracts without pretending to be new signed production trails.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// Native/EVM observations and domain pins are independent fixture inputs.
// The journal cap reuses the existing explicit one-megabyte artifact-test cap;
// it is not a production default or an increase to any runtime allowance.
type releaseMeasurementInputV2TestFixture struct {
	runtime    *releaseStatsV2RuntimeTestFixture
	steerer    *ReleaseSteerer
	snapshot   *ReleaseSnapshot
	options    releaseMeasurementInputV2Options
	nativeHash string
}

// Real Stats, ledger, key, proof and policy ownership are inherited unchanged.
func newReleaseMeasurementInputV2TestFixture(t *testing.T) *releaseMeasurementInputV2TestFixture {
	t.Helper()
	runtime := newReleaseStatsV2RuntimeTestFixture(t, 1, 0)
	identity, domain := runtime.source.expected.Identity, runtime.source.expected.Activation.Domain
	cfg := &ReleaseConfig{DeploymentID: identity.DeploymentID, ChainID: identity.ChainID, GenesisHash: identity.GenesisHash,
		Coordinator: fmt.Sprintf("0x%x", domain.Coordinator), SettlementVault: fmt.Sprintf("0x%x", domain.SettlementVault),
		ValidatorID: identity.ValidatorID, Netuid: identity.Netuid, PolicyHash: attemptHex32(domain.PolicyHash), Policy: runtime.source.policy,
		StateDir: newAttemptLedgerDiskTestStateDir(t), Operators: []OperatorConfig{{NoID: identity.NoID, StateDir: runtime.dir}}}
	steerer := &ReleaseSteerer{cfg: cfg, contexts: map[uint64]*ReleaseMeasurementContext{identity.NoID: {NoID: identity.NoID, Stats: runtime.stats}}}
	boundary := runtime.source.expected.Boundary
	evmHash, err := parseReleaseHex32("fixture EVM hash", boundary.EVMBlockHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &releaseMeasurementInputV2TestFixture{runtime: runtime, steerer: steerer,
		snapshot: &ReleaseSnapshot{BlockNumber: boundary.EVMBlock, BlockHash: evmHash, Epoch: new(big.Int).SetUint64(boundary.SettlementEpoch)},
		options:  releaseMeasurementInputV2Options{Stats: runtime.options, MaxJournalBytes: 1024 * 1024}, nativeHash: attemptHex32([32]byte{0x29})}
}

// Every replay receives fresh independent scratch, including same-epoch retry.
func (self *releaseMeasurementInputV2TestFixture) fresh(t *testing.T) releaseMeasurementInputV2Options {
	t.Helper()
	options := self.options
	options.Stats = self.runtime.fresh(t)
	return options
}

// The ordinary caller supplies decision coordinates independently of its file.
func (self *releaseMeasurementInputV2TestFixture) detach(t *testing.T) ReleaseMeasurementInput {
	t.Helper()
	input, err := self.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, self.nativeHash, self.snapshot, self.fresh(t))
	if err != nil {
		t.Fatal(err)
	}
	return input
}

// Restart closes and reopens actual disk ownership, then loads and attaches
// through the existing Stats startup path before journal reconciliation.
func (self *releaseMeasurementInputV2TestFixture) restart(t *testing.T) {
	t.Helper()
	if err := self.runtime.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewDiskAttemptLedger(t.Context(), self.runtime.dir, self.runtime.source.expected.Identity, attemptLedgerDiskTestCoordinator, self.runtime.source.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	stats := NewStatsEngine(self.runtime.stats.cfg)
	if err := stats.Load(self.runtime.dir); err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, self.runtime.dir); err != nil {
		t.Fatal(err)
	}
	self.runtime.stats, self.runtime.ledger = stats, ledger
	self.steerer.contexts[9].Stats = stats
}

// Restart returns the same signed input bytes and generation, with a fresh
// full replay and no reseal, journal replacement or additional rotation.
func TestReleaseMeasurementInputV2RuntimeRestartsExactGenuineInput(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RuntimeRestartsExactGenuineInput")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	input := fixture.detach(t)
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
	before, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if input.AttemptCutV2 == nil || input.AttemptCutV2.RecordCount != 8 || input.AttemptCutV2.CompleteCount != 1 || input.Stats.AttemptCut != nil || input.Stats.SettlementTransition != nil || input.EgressGeneration != fixture.runtime.initialEgressGeneration || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration+1 {
		t.Fatal("ordinary journal lacks actual compact M8 evidence")
	}
	journal, err := decodeReleaseMeasurementInputV2(t.Context(), before, fixture.fresh(t))
	if err != nil || journal.Schema != releaseMeasurementInputV2Schema || !reflect.DeepEqual(journal.MeasurementInput, input) {
		t.Fatalf("canonical compact private input: %v", err)
	}
	writes, reads := fixture.runtime.objects.writes, fixture.runtime.objects.reads
	fixture.restart(t)
	restarted := fixture.detach(t)
	after, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) || !bytes.Equal(before, after) || !reflect.DeepEqual(restarted, input) || fixture.runtime.objects.writes != writes || fixture.runtime.objects.reads <= reads || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration+1 {
		t.Fatalf("restart changed immutable evidence or repeated rotation: %v", err)
	}
}

// A genuine journal precedes a real snapshot rename failure. The outer caller
// receives no partial input; restart recovers the committed journal instead
// of sealing a second native window or losing the original egress.
func TestReleaseMeasurementInputV2RuntimeRecoversActualRenameFailure(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RuntimeRecoversActualRenameFailure")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	restore := obstructReleaseStatsV2RuntimeTestSnapshot(t, fixture.runtime.dir)
	input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t))
	var rename *os.LinkError
	if !errors.As(err, &rename) || rename.Op != "rename" || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration || !fixture.runtime.stats.attemptCutPending {
		t.Fatalf("real journal-first failure escaped: %v", err)
	}
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
	before, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeReleaseMeasurementInputV2(t.Context(), before, fixture.fresh(t))
	if err != nil || journal.MeasurementInput.AttemptCutV2.LastSequence != 8 {
		t.Fatalf("actual durable input missing: %v", err)
	}
	writes := fixture.runtime.objects.writes
	restore()
	fixture.restart(t)
	recovered := fixture.detach(t)
	after, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(recovered, journal.MeasurementInput) || fixture.runtime.objects.writes != writes || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration+1 || fixture.runtime.stats.attemptCutPending {
		t.Fatalf("journal-first restart did not reconcile once: %v", err)
	}
}

// V1 evidence is an actual replayable signed cut, not a sentinel file. The
// disjoint compact filename leaves its bytes and inode intact in the same
// subnet epoch, and neither decoder silently accepts the other schema.
func TestReleaseMeasurementInputV2RuntimePreservesV1JournalNamespace(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RuntimePreservesV1JournalNamespace")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	legacy, err := NewAttemptLedger(newAttemptLedgerDiskTestStateDir(t), fixture.runtime.source.expected.Identity, fixture.runtime.source.key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	for _, record := range fixture.runtime.source.recordTs {
		if _, err := legacy.AppendContext(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	cut, err := legacy.BuildCut(fixture.runtime.source.expected.Boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw := fixture.runtime.stats.currentReleaseStatsMeasurement()
	raw.AttemptCut = cut
	cfg := fixture.steerer.cfg
	old := &releaseMeasurementInputJournal{Schema: releaseMeasurementInputSchema, DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash,
		Coordinator: cfg.Coordinator, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, SubnetEpoch: 7, PolicyHash: cfg.PolicyHash,
		MeasurementInput: ReleaseMeasurementInput{NoID: 9, SettlementEpoch: 42, CutNativeBlock: 200, CutNativeBlockHash: fixture.nativeHash, CutEVMSnapshotBlock: fixture.snapshot.BlockNumber, CutEVMSnapshotHash: releaseHex32(fixture.snapshot.BlockHash), EgressGeneration: 1, Stats: raw}}
	encoded, err := canonicalReleaseMeasurementInputBytes(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeReleaseMeasurementInput(encoded); err != nil {
		t.Fatalf("genuine v1 history is not replayable: %v", err)
	}
	oldPath := releaseMeasurementInputPath(cfg.StateDir, 7, 9)
	if err := atomicStateWrite(oldPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.detach(t)
	preserved, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.Lstat(oldPath)
	if err != nil || !bytes.Equal(encoded, preserved) || !os.SameFile(info, current) {
		t.Fatalf("compact input rewrote v1 history: %v", err)
	}
	compact, err := readReleaseMeasurementInputV2(releaseMeasurementInputV2Path(cfg.StateDir, 7, 9), fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeReleaseMeasurementInput(compact); err == nil {
		t.Fatal("v1 decoder accepted compact journal")
	}
	if _, err := decodeReleaseMeasurementInputV2(t.Context(), encoded, fixture.fresh(t)); err == nil {
		t.Fatal("compact decoder materialized legacy journal history")
	}
}

// Two physical writers race different canonical inputs at one immutable
// name. Exactly one may commit; identical retry acknowledges that same inode.
// This is a storage contract control over genuine captured evidence bytes.
func TestReleaseMeasurementInputV2ImmutablePublicationConflict(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2ImmutablePublicationConflict")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	fixture.detach(t)
	first, err := readReleaseMeasurementInputV2(releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9), fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeReleaseMeasurementInputV2(t.Context(), first, fixture.fresh(t))
	if err != nil {
		t.Fatal(err)
	}
	journal.SubnetEpoch++
	second, err := canonicalReleaseMeasurementInputBytes(journal)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(newAttemptLedgerDiskTestStateDir(t), "input.json")
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(2)
	results := make(chan error, 2)
	for _, raw := range [][]byte{first, second} {
		go func(raw []byte) {
			ready.Done()
			<-start
			results <- writeReleaseMeasurementInputV2(path, raw, fixture.options.MaxJournalBytes)
		}(raw)
	}
	ready.Wait()
	close(start)
	left, right := <-results, <-results
	if (left == nil) == (right == nil) {
		t.Fatalf("immutable race must have exactly one winner: %v / %v", left, right)
	}
	winner, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil || !bytes.Equal(winner, first) && !bytes.Equal(winner, second) {
		t.Fatalf("race produced partial or invented bytes: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeReleaseMeasurementInputV2(path, winner, fixture.options.MaxJournalBytes); err != nil {
		t.Fatal(err)
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		t.Fatalf("identical retry replaced committed inode: %v", err)
	}
}

// Admission refuses unsafe or oversize inodes before any journal allocation.
func TestReleaseMeasurementInputV2RejectsUnsafeBoundedFiles(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RejectsUnsafeBoundedFiles")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	fixture.detach(t)
	encoded, err := readReleaseMeasurementInputV2(releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9), fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink", "public", "directory", "oversize", "zero-bound", "overflow-bound"} {
		dir := newAttemptLedgerDiskTestStateDir(t)
		path, limit := filepath.Join(dir, "input.json"), fixture.options.MaxJournalBytes
		switch kind {
		case "directory":
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			target := filepath.Join(dir, "target.json")
			if err := os.WriteFile(target, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		default:
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "public" {
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "oversize" {
				limit = uint64(len(encoded)) - 1
			}
			if kind == "zero-bound" {
				limit = 0
			}
			if kind == "overflow-bound" {
				limit = ^uint64(0)
			}
		}
		if raw, err := readReleaseMeasurementInputV2(path, limit); err == nil || raw != nil {
			t.Fatalf("unsafe %s input was admitted: %v", kind, err)
		}
	}
}

// Raw-only decoding cannot recursively build legacy evidence, accept a
// partial JSON prefix, or hide unknown fields behind otherwise valid bytes.
func TestReleaseMeasurementInputV2RejectsMalformedAndLegacyGraphs(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RejectsMalformedAndLegacyGraphs")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	fixture.detach(t)
	encoded, err := readReleaseMeasurementInputV2(releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9), fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"legacy-cut", "legacy-transition", "unknown", "truncated", "trailing", "noncanonical"} {
		changed := bytes.Clone(encoded)
		switch kind {
		case "legacy-cut", "legacy-transition", "unknown":
			var outer map[string]json.RawMessage
			if err := json.Unmarshal(changed, &outer); err != nil {
				t.Fatal(err)
			}
			if kind == "unknown" {
				outer["unexpected"] = json.RawMessage("true")
			} else {
				var input, stats map[string]json.RawMessage
				if err := json.Unmarshal(outer["measurement_input"], &input); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(input["stats"], &stats); err != nil {
					t.Fatal(err)
				}
				field := "attempt_cut"
				if kind == "legacy-transition" {
					field = "settlement_transition"
				}
				stats[field] = json.RawMessage("{}")
				input["stats"], err = json.Marshal(stats)
				if err != nil {
					t.Fatal(err)
				}
				outer["measurement_input"], err = json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
			}
			changed, err = json.Marshal(outer)
			if err != nil {
				t.Fatal(err)
			}
		case "truncated":
			changed = changed[:len(changed)/2]
		case "trailing":
			changed = append(changed, []byte("{}")...)
		case "noncanonical":
			changed = append([]byte(" "), changed...)
		}
		reads := fixture.runtime.objects.reads
		if journal, err := decodeReleaseMeasurementInputV2(t.Context(), changed, fixture.fresh(t)); err == nil || journal != nil || fixture.runtime.objects.reads != reads {
			t.Fatalf("%s compact wire was accepted or reached object I/O: %v", kind, err)
		}
	}
}

// Altering outer labels cannot reinterpret the unchanged genuinely signed
// cut. These explicit error-contract controls preserve live Stats ownership.
func TestReleaseMeasurementInputV2RejectsConflictingJournalLabels(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RejectsConflictingJournalLabels")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	fixture.detach(t)
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
	encoded, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.runtime.stats.snapshotStats()
	for _, edit := range []func(*releaseMeasurementInputJournal){
		func(j *releaseMeasurementInputJournal) { j.DeploymentID += "-other" },
		func(j *releaseMeasurementInputJournal) { j.ChainID++ },
		func(j *releaseMeasurementInputJournal) { j.GenesisHash = attemptHex32([32]byte{0x33}) },
		func(j *releaseMeasurementInputJournal) { j.Coordinator = "0x2222222222222222222222222222222222222222" },
		func(j *releaseMeasurementInputJournal) { j.ValidatorID++ },
		func(j *releaseMeasurementInputJournal) { j.Netuid++ },
		func(j *releaseMeasurementInputJournal) { j.SubnetEpoch++ },
		func(j *releaseMeasurementInputJournal) { j.PolicyHash = attemptHex32([32]byte{0x34}) },
		func(j *releaseMeasurementInputJournal) { j.MeasurementInput.NoID++ },
		func(j *releaseMeasurementInputJournal) { j.MeasurementInput.SettlementEpoch++ },
		func(j *releaseMeasurementInputJournal) { j.MeasurementInput.EgressGeneration++ },
		func(j *releaseMeasurementInputJournal) { j.MeasurementInput.CutNativeBlock++ },
		func(j *releaseMeasurementInputJournal) {
			j.MeasurementInput.CutNativeBlockHash = attemptHex32([32]byte{0x35})
		},
		func(j *releaseMeasurementInputJournal) {
			j.MeasurementInput.CutEVMSnapshotHash = attemptHex32([32]byte{0x36})
		},
	} {
		journal, err := decodeReleaseMeasurementInputV2(t.Context(), encoded, fixture.fresh(t))
		if err != nil {
			t.Fatal(err)
		}
		edit(journal)
		changed, err := canonicalReleaseMeasurementInputBytes(journal)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		reads := fixture.runtime.objects.reads
		if input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t)); err == nil || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) {
			t.Fatalf("conflicting label escaped admission: %v", err)
		}
		if fixture.runtime.objects.reads != reads || !reflect.DeepEqual(fixture.runtime.stats.snapshotStats(), before) {
			t.Fatal("outer label changed live ownership or reached object replay")
		}
	}
}

// A valid independently signed cut still belongs to its original activation;
// changing caller pins cannot authorize another migration generation.
func TestReleaseMeasurementInputV2RejectsAlternateActivationPins(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RejectsAlternateActivationPins")
		}
	})
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	fixture.detach(t)
	before, reads := fixture.runtime.stats.snapshotStats(), fixture.runtime.objects.reads
	options := fixture.fresh(t)
	options.Stats.Activation.Domain.ActivationHash[0] ^= 1
	input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, options)
	if err == nil || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) || fixture.runtime.objects.reads != reads || !reflect.DeepEqual(before, fixture.runtime.stats.snapshotStats()) {
		t.Fatalf("journal chose a different activation: %v", err)
	}
}

// Config identity/domain mismatch is rejected before the immutable writer,
// even though the bound local ledger can produce a valid signed M8 cut.
func TestReleaseMeasurementInputV2RejectsForeignConfigAtFirstPublication(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2RejectsForeignConfigAtFirstPublication")
		}
	})
	for _, edit := range []func(*ReleaseConfig){
		func(cfg *ReleaseConfig) { cfg.DeploymentID += "-other" },
		func(cfg *ReleaseConfig) { cfg.Coordinator = "0x2222222222222222222222222222222222222222" },
		func(cfg *ReleaseConfig) { cfg.SettlementVault = "0x3333333333333333333333333333333333333333" },
		func(cfg *ReleaseConfig) { cfg.PolicyHash = attemptHex32([32]byte{0x77}) },
	} {
		fixture := newReleaseMeasurementInputV2TestFixture(t)
		edit(fixture.steerer.cfg)
		input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t))
		if err == nil || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration {
			t.Fatalf("foreign config published a compact window: %v", err)
		}
		if _, err := os.Lstat(releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("foreign config left an input journal: %v", err)
		}
	}
}

// Missing bounds, unowned relative roots and cancellation stop before object
// work. An admitted but too-small finite byte allowance never rotates Stats.
func TestReleaseMeasurementInputV2HonorsAdmissionAndByteBounds(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-V2-RUNTIME-v1 PASS TestReleaseMeasurementInputV2HonorsAdmissionAndByteBounds")
		}
	})
	for _, kind := range []string{"missing-bound", "relative-root", "canceled", "small-bound"} {
		fixture := newReleaseMeasurementInputV2TestFixture(t)
		options := fixture.fresh(t)
		ctx, cancel := context.WithCancel(t.Context())
		switch kind {
		case "missing-bound":
			options.MaxJournalBytes = 0
		case "relative-root":
			fixture.steerer.cfg.StateDir = "relative-state"
		case "canceled":
			cancel()
		case "small-bound":
			options.MaxJournalBytes = 1
		}
		input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(ctx, 9, 7, 200, fixture.nativeHash, fixture.snapshot, options)
		cancel()
		if err == nil || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration {
			t.Fatalf("%s crossed journal admission: %v", kind, err)
		}
		if kind != "small-bound" && fixture.runtime.objects.writes != 0 {
			t.Fatalf("%s reached object writes", kind)
		}
	}
}
