//go:build linux || darwin

package validator

// Real activation and a signed terminal precede every ordinary-window input.
// Mutation controls alter retained bytes, never a signature or replay verdict.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Two genuine M8 trails occupy sixteen of the existing 128 disk records.
// Restart authority stays pinned to the closed terminal, while ordinary
// authority advances independently to the real next finalized window.
func newReleaseStatsV2ActivatedTestFixture(t *testing.T) (*attemptSettlementRuntimeV2TestFixture, *releaseStatsV2RuntimeTestFixture, AttemptSettlementV2Options) {
	t.Helper()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil || closure == nil || closure.Transitions[0].Cut.RecordCount != 8 {
		t.Fatalf("actual terminal prerequisite: %v", err)
	}
	authority := fixture.options(t).Authority
	fixture.nextWindow(t, closure)
	fixture.trails(t, 0, 1, 0)
	for _, participant := range fixture.participants {
		if err := participant.Stats.Save(participant.StateDir); err != nil {
			t.Fatal(err)
		}
	}
	participant := fixture.participants[0]
	ordinary := releaseStatsV2RuntimeTestFixtureFor(t, fixture.fixtures[0], participant.Stats, participant.Ledger)
	snapshot := participant.Stats.snapshotStats()
	if snapshot.Version != 6 || snapshot.AttemptV2 == nil || snapshot.AttemptV2.Terminal == nil || snapshot.AttemptV2.Activation != ordinary.options.Activation || snapshot.AttemptV2.SettlementPriorRoot != closure.Transitions[0].Cut.Root || snapshot.AttemptLastAppliedSequence != 16 || snapshot.AttemptSettlementFirstSequence != 9 || snapshot.AttemptEgressFirstSequence != 9 || snapshot.EgressGeneration != 2 {
		t.Fatal("ordinary control lacks actual retained activation, terminal or successor prefix")
	}
	return fixture, ordinary, authority
}

// Corruption is installed as a separately owned value, never by mutating the
// immutable terminal shared by Stats clones. Its local shape remains valid;
// only independent activation/ledger authority can reject these inputs.
func alterReleaseStatsV2ActivatedTestState(t *testing.T, stats *StatsEngine, field string) {
	t.Helper()
	owner := stats.lockStatsWrite("ordinary-activation-test-mutation")
	defer owner.release()
	candidate := owner.clone()
	if candidate.attemptV2 == nil || candidate.attemptV2.Terminal == nil {
		t.Fatal("retained mutation lacks a real terminal")
	}
	state := *candidate.attemptV2
	terminal := *state.Terminal
	state.Terminal = &terminal
	switch field {
	case "activation":
		state.Activation.Hotkey[0] ^= 1
		state.Terminal.Cut.Context.Activation = state.Activation
	case "prior-root":
		root, err := canonicalAttemptHex32("retained test prior root", state.SettlementPriorRoot, false)
		if err != nil {
			t.Fatal(err)
		}
		root[0] ^= 1
		root[31] = 1
		state.SettlementPriorRoot = attemptHex32(root)
		state.Terminal.Cut.Root = state.SettlementPriorRoot
	default:
		t.Fatalf("unknown retained mutation %q", field)
	}
	candidate.attemptV2 = &state
	if _, err := encodeStatsSnapshot(candidate.snapshotStats()); err != nil {
		t.Fatalf("corruption must reach independent authority, not a local shape refusal: %v", err)
	}
	owner.publish(candidate)
}

// No object, journal or snapshot publication precedes the shared binding
// check. A refused detach may retain its existing drain reservation only.
func assertReleaseStatsV2ActivatedTestDetachRefusal(t *testing.T, fixture *releaseStatsV2RuntimeTestFixture, options releaseStatsV2Options, reason string) {
	t.Helper()
	before := fixture.stats.snapshotStats()
	path := filepath.Join(fixture.dir, "stats.json")
	durable, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reads, writes, publications := fixture.objects.reads, fixture.objects.writes, 0
	measurement, cut, err := fixture.stats.detachReleaseStatsMeasurementV2(t.Context(), fixture.dir, fixture.source.expected.Boundary, options, func(ReleaseStatsMeasurement, AttemptCutV2) error {
		publications++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), reason) || cut != nil || !reflect.DeepEqual(measurement, ReleaseStatsMeasurement{}) || publications != 0 || fixture.objects.reads != reads || fixture.objects.writes != writes || !reflect.DeepEqual(before, fixture.stats.snapshotStats()) {
		t.Fatalf("mismatched ordinary authority reached publication: %v; publications=%d reads=%d writes=%d", err, publications, fixture.objects.reads-reads, fixture.objects.writes-writes)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, durable) {
		t.Fatalf("refused authority changed the durable snapshot: %v", err)
	}
}

// Real Save retains the supplied history but does not authenticate it.
// Fresh public recovery must refuse corrupted authority before attachment or
// even a same-byte replacement of any participant's physical snapshot.
func assertReleaseStatsV2ActivatedTestRestartRefusal(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture, authority AttemptSettlementV2Options) {
	t.Helper()
	images := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	infos := make([]os.FileInfo, len(fixture.participants))
	for index, participant := range fixture.participants {
		var err error
		infos[index], err = os.Lstat(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, authority, runtimeAttemptSettlementV2TestPersistence()); err == nil {
		t.Fatal("restart authenticated altered retained history")
	}
	for index, participant := range restarted {
		path := filepath.Join(participant.StateDir, "stats.json")
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, images[index]) {
			t.Fatalf("refused startup changed no_id %d snapshot: %v", participant.NoID, err)
		}
		info, err := os.Lstat(path)
		if err != nil || !os.SameFile(info, infos[index]) || participant.Stats.attemptLedger != nil || participant.Stats.attemptV2 != nil {
			t.Fatalf("refused startup replaced or attached no_id %d: %v", participant.NoID, err)
		}
	}
}

// An ordinary cut clears only its own egress generation. The real signed
// terminal, activation and prior root survive Save and full batch restart.
func TestReleaseStatsV2RuntimeActivatedTerminalOrdinarySaveRestart(t *testing.T) {
	t.Parallel()
	fixture, ordinary, authority := newReleaseStatsV2ActivatedTestFixture(t)
	before := ordinary.stats.snapshotStats()
	path := filepath.Join(ordinaryInputCommitTestDir(t), "input.json")
	var captured releaseStatsV2RuntimeTestInput
	_, cut, err := ordinary.stats.detachReleaseStatsMeasurementV2(t.Context(), ordinary.dir, ordinary.source.expected.Boundary, ordinary.fresh(t), releaseStatsV2RuntimeTestCapture(path, &captured))
	if err != nil || cut == nil || cut.RecordCount != 8 || cut.Context.Activation != before.AttemptV2.Activation || cut.Context.PriorRoot != before.AttemptV2.SettlementPriorRoot || cut.Context.FirstSequence != 9 || cut.LastSequence != 16 {
		t.Fatalf("ordinary cut did not retain actual terminal authority: %v", err)
	}
	if err := ordinary.stats.Save(ordinary.dir); err != nil {
		t.Fatal(err)
	}
	after := ordinary.stats.snapshotStats()
	if !reflect.DeepEqual(after.AttemptV2, before.AttemptV2) || !reflect.DeepEqual(after.Window, before.Window) || !reflect.DeepEqual(after.EmaPPM, before.EmaPPM) || len(after.Egress) != 0 || after.EgressGeneration != before.EgressGeneration+1 || after.AttemptSettlementFirstSequence != before.AttemptSettlementFirstSequence || after.AttemptEgressFirstSequence != 17 {
		t.Fatal("ordinary Save changed terminal authority or settlement counters")
	}
	encoded, err := readReleaseMeasurementInputV2(path, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	var journal releaseStatsV2RuntimeTestInput
	if err := json.Unmarshal(encoded, &journal); err != nil || !reflect.DeepEqual(journal, captured) {
		t.Fatalf("actual ordinary journal differs: %v", err)
	}
	loaded := NewStatsEngine(ordinary.stats.cfg)
	if err := loaded.Load(ordinary.dir); err != nil || !reflect.DeepEqual(loaded.snapshotStats(), after) || !loaded.attemptCutPending || !loaded.attemptSettlementCutPending {
		t.Fatalf("plain Load lost retained state or opened admission: %v", err)
	}
	if err := loaded.AttachAttemptLedger(ordinary.ledger, ordinary.dir); err == nil || loaded.attemptLedger != nil {
		t.Fatal("legacy attachment activated compact state")
	}
	reads := ordinary.objects.reads
	if err := loaded.reconcileReleaseStatsCutV2(t.Context(), ordinary.dir, journal.Stats, journal.Cut, ordinary.fresh(t)); !errors.Is(err, errAttemptCutPending) || ordinary.objects.reads != reads {
		t.Fatalf("plain Load entered ordinary recovery before full activation: %v", err)
	}
	images := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(images, runtimeAttemptSettlementV2TestImages(t, restarted)) {
		t.Fatal("full restart changed ordinary counters or retained terminal")
	}
	options := ordinary.fresh(t)
	options.Activation.Hotkey[0] ^= 1
	if err := restarted[0].Stats.reconcileReleaseStatsCutV2(t.Context(), ordinary.dir, journal.Stats, journal.Cut, options); err == nil || ordinary.objects.reads != reads {
		t.Fatal("restart journal supplied its own activation authority")
	}
	if err := restarted[0].Stats.reconcileReleaseStatsCutV2(t.Context(), ordinary.dir, journal.Stats, journal.Cut, ordinary.fresh(t)); err != nil || ordinary.objects.reads == reads || !reflect.DeepEqual(images, runtimeAttemptSettlementV2TestImages(t, restarted)) {
		t.Fatalf("healthy advanced-generation journal did not replay as an exact no-op: %v", err)
	}
}

// Well-shaped independent options cannot reseal the same actual rows under
// another hotkey or activation commitment while retaining the original state.
func TestReleaseStatsV2RuntimeActivatedRejectsIndependentActivationBeforePublication(t *testing.T) {
	t.Parallel()
	_, ordinary, _ := newReleaseStatsV2ActivatedTestFixture(t)
	for _, field := range []string{"hotkey", "activation-hash"} {
		options := ordinary.fresh(t)
		if field == "hotkey" {
			options.Activation.Hotkey[0] ^= 1
		} else {
			options.Activation.Domain.ActivationHash[0] ^= 1
		}
		assertReleaseStatsV2ActivatedTestDetachRefusal(t, ordinary, options, "retained activation differs")
	}
}

// A locally consistent alternate activation in actual Save bytes is still
// unauthorized, both for live ordinary publication and fresh public restart.
func TestReleaseStatsV2RuntimeActivatedRejectsRetainedActivationBeforePublication(t *testing.T) {
	t.Parallel()
	fixture, ordinary, authority := newReleaseStatsV2ActivatedTestFixture(t)
	alterReleaseStatsV2ActivatedTestState(t, ordinary.stats, "activation")
	if err := ordinary.stats.Save(ordinary.dir); err != nil {
		t.Fatal(err)
	}
	assertReleaseStatsV2ActivatedTestDetachRefusal(t, ordinary, ordinary.fresh(t), "retained activation differs")
	assertReleaseStatsV2ActivatedTestRestartRefusal(t, fixture, authority)
}

// Reconstructing a genuine ledger root must detect a differing retained root,
// not silently repair the ordinary context while preserving corrupt history.
func TestReleaseStatsV2RuntimeActivatedRejectsRetainedSettlementPriorRootBeforePublication(t *testing.T) {
	t.Parallel()
	fixture, ordinary, authority := newReleaseStatsV2ActivatedTestFixture(t)
	alterReleaseStatsV2ActivatedTestState(t, ordinary.stats, "prior-root")
	if err := ordinary.stats.Save(ordinary.dir); err != nil {
		t.Fatal(err)
	}
	assertReleaseStatsV2ActivatedTestDetachRefusal(t, ordinary, ordinary.fresh(t), "retained settlement root differs")
	assertReleaseStatsV2ActivatedTestRestartRefusal(t, fixture, authority)
}

// Both an unconsumed journal and an already consumed journal must bind the
// retained authority before external replay or the advanced-generation no-op.
func TestReleaseStatsV2RuntimeActivatedRejectsRetainedAuthorityDuringReconcile(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"activation", "prior-root"} {
		for _, advanced := range []bool{false, true} {
			_, ordinary, _ := newReleaseStatsV2ActivatedTestFixture(t)
			path := filepath.Join(ordinaryInputCommitTestDir(t), "input.json")
			var captured releaseStatsV2RuntimeTestInput
			persist := releaseStatsV2RuntimeTestCapture(path, &captured)
			cause := errors.New("stop after actual journal before ordinary save")
			_, cut, err := ordinary.stats.detachReleaseStatsMeasurementV2(t.Context(), ordinary.dir, ordinary.source.expected.Boundary, ordinary.fresh(t), func(measurement ReleaseStatsMeasurement, cut AttemptCutV2) error {
				if err := persist(measurement, cut); err != nil {
					return err
				}
				if !advanced {
					return cause
				}
				return nil
			})
			if advanced && (err != nil || cut == nil) || !advanced && (!errors.Is(err, cause) || cut != nil) || captured.Cut.LastSequence != 16 {
				t.Fatalf("actual journal generation prerequisite (%s, advanced=%t): %v", field, advanced, err)
			}
			alterReleaseStatsV2ActivatedTestState(t, ordinary.stats, field)
			if err := ordinary.stats.Save(ordinary.dir); err != nil {
				t.Fatal(err)
			}
			before := ordinary.stats.snapshotStats()
			reads, writes := ordinary.objects.reads, ordinary.objects.writes
			err = ordinary.stats.reconcileReleaseStatsCutV2(t.Context(), ordinary.dir, captured.Stats, captured.Cut, ordinary.fresh(t))
			if err == nil || !strings.Contains(err.Error(), "compact native retained") || ordinary.objects.reads != reads || ordinary.objects.writes != writes || !reflect.DeepEqual(before, ordinary.stats.snapshotStats()) {
				t.Fatalf("reconciliation ignored retained authority (%s, advanced=%t): %v", field, advanced, err)
			}
			expected, err := encodeStatsSnapshot(before)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(filepath.Join(ordinary.dir, "stats.json"))
			if err != nil || !bytes.Equal(expected, actual) {
				t.Fatalf("refused reconciliation changed snapshot (%s, advanced=%t): %v", field, advanced, err)
			}
		}
	}
}
