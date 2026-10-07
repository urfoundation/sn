//go:build linux || darwin

package validator

// Restart controls keep genuine M8 rows and exact persistent images. Mutated
// snapshots are refusal inputs, never fake accepted replay or fold results.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Positive prior quality survives a real disk reopen and complete authority
// replay; reporting floats and exact policy quality remain independent.
func TestAttemptSettlementRuntimeV2RecoveryPreservesRealPositivePrior(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 15, 1)
	fixture.trails(t, 1, 1, 0)
	closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil || len(closure.Transitions[0].PostFold) == 0 {
		t.Fatalf("real positive prior prerequisite: %v", err)
	}
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, restarted)) {
		t.Fatal("restart refolded or changed real prior history")
	}
}

// A genuinely empty next terminal carries, rather than refolds away, the
// prior quality. The first signed stream still uses exactly122/16 capacity.
func TestAttemptSettlementRuntimeV2EmptySuccessorPreservesRealPrior(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 15, 1)
	first, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil || len(first.Transitions[0].PostFold) == 0 {
		t.Fatalf("real positive prior prerequisite: %v", err)
	}
	fixture.nextWindow(t, first)
	next, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 44, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	for index, terminal := range next.Transitions {
		if terminal.Cut.RecordCount != 0 || !reflect.DeepEqual(terminal.PostFold, first.Transitions[index].PostFold) || terminal.Cut.Context.PriorRoot != first.Transitions[index].Cut.Root {
			t.Fatal("empty real successor lost exact terminal prior")
		}
	}
}

// Before any terminal, startup recovers an actual completed M8 suffix that
// was never checkpointed by Save. No synthetic counter projection is used.
func TestAttemptSettlementRuntimeV2RecoveryAppliesRealUncheckpointedSuffix(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	fixture.trails(t, 1, 1, 0)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	for index, participant := range fixture.participants {
		disk, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil || bytes.Equal(disk, before[index]) {
			t.Fatalf("uncheckpointed actual suffix prerequisite: %v", err)
		}
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, restarted)) {
		t.Fatal("restart did not apply exact actual M8 suffix")
	}
}

// The latest complete terminal and a genuinely nonempty next settlement
// coexist on restart. Historical replay authority stays pinned to the terminal.
func TestAttemptSettlementRuntimeV2RecoveryPreservesRealNonemptySuccessor(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	first, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	authority := fixture.options(t).Authority
	fixture.nextWindow(t, first)
	fixture.trails(t, 0, 1, 0)
	fixture.trails(t, 1, 1, 0)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	for index, participant := range fixture.participants {
		head, err := participant.Ledger.Head()
		if err != nil || head.LastSequence != first.Transitions[index].Cut.LastSequence+8 || len(participant.Stats.window) == 0 {
			t.Fatalf("real signed nonempty successor prerequisite: %v", err)
		}
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, restarted)) {
		t.Fatal("nonempty successor or carried terminal changed")
	}
}

// Same epoch and generation are insufficient: unknown counters are not one
// of the exact original, live-preimage or postimage journal hashes.
func TestAttemptSettlementRuntimeV2RecoveryRejectsUnknownSameGenerationCounters(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	transaction := fixture.partialWrite(t)
	var snapshot statsSnapshot
	if err := json.Unmarshal(transaction.Snapshots[1].PreImageJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	id := fixture.fixtures[1].server.providers[1].String()
	snapshot.Window[id] = &ProviderWindow{Assignments: 1}
	changed, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(transaction.Snapshots[1].StatsPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := fixture.reopen(t)
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected recovery write")
	}
	if err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err == nil || writes != 0 {
		t.Fatalf("unknown same-generation image was accepted: %v/%d", err, writes)
	}
}

// V6 cannot be downgraded, stripped of its activation or given a mutated
// local successor cursor. These are all codec refusals before publication.
func TestAttemptSettlementRuntimeV2SnapshotRejectsDowngradeOmissionAndMutation(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	for _, variant := range []string{"downgrade", "omission", "activation", "root", "cursor", "unknown-version"} {
		raw := runtimeAttemptSettlementV2TestImages(t, fixture.participants)[0]
		var snapshot statsSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			t.Fatal(err)
		}
		switch variant {
		case "downgrade":
			snapshot.Version = 5
		case "omission":
			snapshot.AttemptV2 = nil
		case "activation":
			snapshot.AttemptV2.Activation.FirstSequence++
		case "root":
			snapshot.AttemptV2.SettlementPriorRoot = attemptHex32([32]byte{77})
		case "cursor":
			snapshot.AttemptEgressFirstSequence++
		case "unknown-version":
			snapshot.Version = 7
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		stats := NewStatsEngine(fixture.participants[0].Stats.cfg)
		if err := stats.loadStatsSnapshotOwned(encoded); err == nil {
			t.Fatalf("%s altered v6 state was accepted", variant)
		}
	}
}

// Removing one whole activation field and claiming v5 still fails independent
// startup authority, even though the historical v5 codec remains available.
func TestAttemptSettlementRuntimeV2RecoveryRejectsStrippedV6Snapshot(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	snapshot := fixture.participants[1].Stats.snapshotStats()
	snapshot.Version, snapshot.AttemptV2 = 5, nil
	encoded, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(filepath.Join(fixture.participants[1].StateDir, "stats.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := fixture.reopen(t)
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected downgrade write")
	}
	if err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err == nil || writes != 0 {
		t.Fatalf("stripped activation was reset or repaired: %v/%d", err, writes)
	}
	for _, participant := range restarted {
		if participant.Stats.attemptLedger != nil {
			t.Fatal("refused downgrade partially attached startup")
		}
	}
}

// Even the last physical owner close precedes startup publication. The
// injected error is a contract control after an actual close, not device I/O.
func TestAttemptSettlementRuntimeV2RecoveryLateCloseLeavesAllUnattached(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	restarted := fixture.reopen(t)
	physical := attemptSettlementV2PhysicalIO()
	cause := errors.New("post-close startup contract refusal")
	closes := 0
	physical.closeRoot = func(root *attemptPrivateDirectory) error { closes++; return errors.Join(root.close(), cause) }
	if err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); !errors.Is(err, cause) || closes != 3 {
		t.Fatalf("late startup close was lost or owners abandoned: %v/%d", err, closes)
	}
	for _, participant := range restarted {
		if participant.Stats.attemptLedger != nil || participant.Stats.attemptV2 != nil {
			t.Fatal("late close admitted a partial startup owner")
		}
	}
}

// Capturing an original hash is not permission to bless previously corrupted
// same-generation counters; the older disk image must also replay its prefix.
func TestAttemptSettlementRuntimeV2RejectsCorruptOriginalCheckpointBeforeJournal(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	participant := fixture.participants[0]
	snapshot := participant.Stats.snapshotStats()
	snapshot.Window[fixture.fixtures[0].server.providers[1].String()] = &ProviderWindow{Assignments: 1}
	corrupt, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(participant.StateDir, "stats.json")
	if err := atomicStateWrite(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.trails(t, 0, 1, 0)
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeJournal = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected corrupt checkpoint journal")
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || writes != 0 {
		t.Fatalf("corrupt original was blessed by a new hash: %v/%d", err, writes)
	}
	disk, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(disk, corrupt) {
		t.Fatalf("refusal silently repaired unknown disk counters: %v", err)
	}
	fixture.assertReserved(t, 43)
}

// Actual pending recovery writes a signed validator-error terminal only after
// authenticating the complete configured operator set's uncheckpointed tails.
func TestAttemptSettlementRuntimeV2RecoveryFinishesRealPendingM8Prefixes(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	restarted := fixture.pendingRestart(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	for _, participant := range restarted {
		head, err := participant.Ledger.Head()
		if err != nil || head.LastSequence != 2 || participant.Stats.attemptLastAppliedSequence != 2 || participant.Stats.attemptLedger != participant.Ledger || participant.Stats.attemptCutPending {
			t.Fatalf("actual pending recovery did not attach exact two-row prefix: %v", err)
		}
		if err := participant.Ledger.Walk(t.Context(), 2, 2, func(record AttemptRecord) error {
			if record.Disposition != AttemptDispositionValidatorError || record.M != 8 {
				return errors.New("recovery did not append the actual pending outcome")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// A later operator's real receipt fails the independently supplied key set.
// The first operator's valid pending row must not be recovered prematurely.
func TestAttemptSettlementRuntimeV2PendingCensusAuthenticatesAllBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	restarted := fixture.pendingRestart(t)
	authority := fixture.options(t).Authority
	operator := authority.Operators[11]
	for version, key := range operator.Measurement.Replay.ServerKeys {
		changed := append([]byte(nil), key...)
		changed[0] ^= 1
		operator.Measurement.Replay.ServerKeys[version] = changed
	}
	authority.Operators[11] = operator
	physical := attemptSettlementV2PhysicalIO()
	writes, removals := 0, 0
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected pending snapshot")
	}
	physical.removeJournal = func(*attemptPrivateDirectory, string) error {
		removals++
		return errors.New("unexpected pending removal")
	}
	if err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, authority, runtimeAttemptSettlementV2TestPersistence(), physical); err == nil || writes != 0 || removals != 0 {
		t.Fatalf("incomplete pending authentication entered persistence: %v/%d/%d", err, writes, removals)
	}
	for _, participant := range restarted {
		head, err := participant.Ledger.Head()
		if err != nil || head.LastSequence != 1 || participant.Stats.attemptLedger != nil {
			t.Fatalf("earlier valid pending row was appended or attached: %v", err)
		}
	}
}

// A no-journal activation retry cannot declare success over an older missing
// v6 marker, even when its in-memory activated prefix still looks correct.
func TestAttemptSettlementRuntimeV2CurrentActivationRejectsDifferentDiskImage(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	participant := fixture.participants[1]
	snapshot := participant.Stats.snapshotStats()
	snapshot.Version, snapshot.AttemptV2 = 5, nil
	changed, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(participant.StateDir, "stats.json")
	if err := atomicStateWrite(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err == nil {
		t.Fatal("current activation silently blessed a different disk image")
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("current activation refolded or reset live state")
	}
	disk, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(disk, changed) {
		t.Fatalf("current activation overwrote unknown disk state: %v", err)
	}
	fixture.assertReserved(t, 42)
}

// These four actual files share private signed-disk factories, not process
// state. Removing even one first admission recreates the expensive serial
// prefix; inspect that executable source rather than timing its scheduler.
func TestAttemptSettlementRuntimeV2AdditionalPrivateFixturesEnterParallelBeforeWork(t *testing.T) {
	t.Parallel()
	for _, source := range []struct {
		path  string
		roots int
	}{
		{path: "attempt_settlement_v2_recovery_test.go", roots: 13},
		{path: "attempt_settlement_v2_alias_test.go", roots: 4},
		{path: "attempt_settlement_v2_persistence_bounds_test.go", roots: 18},
		{path: "attempt_settlement_v2_path_encoding_test.go", roots: 3},
	} {
		encoded, err := os.ReadFile(source.path)
		if err != nil {
			t.Fatal(err)
		}
		count, err := verifySettlementFixtureParallelAdmission(source.path, encoded)
		if err != nil || count != source.roots {
			t.Fatalf("%s parallel admission or complete root census changed: roots=%d error=%v", source.path, count, err)
		}
		serial := bytes.Replace(encoded, []byte("\n\tt.Parallel()\n"), []byte("\n"), 1)
		if bytes.Equal(encoded, serial) {
			t.Fatal("actual source lost its removable first parallel admission", source.path)
		}
		if _, err := verifySettlementFixtureParallelAdmission(source.path, serial); err == nil {
			t.Fatal("an actual first root could return to a serial prefix", source.path)
		}
	}
}
