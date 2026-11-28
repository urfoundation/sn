//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Empty real intent history isolates the native publication mechanism without
// fabricating a measurement/native authority result. Nonempty paths always
// enter the production runtime replay owner before they reach this writer.
func intentV2PublicationTest(t testing.TB) *IntentStore {
	t.Helper()
	stateDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &releaseRuntimeV2{ctx: t.Context(), cfg: ReleaseConfig{StateDir: stateDir}}
	runtime.cfg.EvidenceV2.Bounds.MaxHistoryBytes = 1024 * 1024
	runtime.cfg.EvidenceV2.Bounds.MaxControlBytes = 1024 * 1024
	return &IntentStore{path: filepath.Join(stateDir, "steering-intents.json"), stateDir: stateDir, v2: &releaseIntentV2Owner{ctx: t.Context(), runtime: runtime}}
}

func TestIntentV2NativePublicationCreatesThenExchangesExactBytes(t *testing.T) {
	store := intentV2PublicationTest(t)
	first, err := store.readV2(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first.encoded != nil {
		t.Fatal("fresh state was not genuinely absent")
	}
	if err := store.writeV2(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := first.custody.close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.readV2(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second.file.History = []SteeringIntent{}
	if err := store.writeV2(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if err := second.custody.close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("native exchange did not publish the changed canonical bytes")
	}
	for _, name := range []string{releaseIntentV2Marker, releaseIntentV2Candidate} {
		if _, err := os.Lstat(filepath.Join(store.stateDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("successful publication retained unresolved %s: %v", name, err)
		}
	}
	third, err := store.readV2(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := third.custody.close(); err != nil {
		t.Fatal(err)
	}
}

func TestIntentV2UnresolvedNativeMarkerIsPreserved(t *testing.T) {
	store := intentV2PublicationTest(t)
	marker := filepath.Join(store.stateDir, releaseIntentV2Marker)
	want := []byte("actual-unresolved-preimage\n")
	if err := os.WriteFile(marker, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if read, err := store.readV2(t.Context()); err == nil || read != nil {
		if read != nil {
			_ = read.custody.close()
		}
		t.Fatal("unresolved publication was treated as empty history")
	}
	got, err := os.ReadFile(marker)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("unresolved evidence changed: %q %v", got, err)
	}
}

func TestIntentV2ChangedPredecessorCannotBeOverwritten(t *testing.T) {
	store := intentV2PublicationTest(t)
	first, err := store.readV2(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.writeV2(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := first.custody.close(); err != nil {
		t.Fatal(err)
	}
	second, err := store.readV2(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	conflict := []byte("actual changed user predecessor\n")
	if err := os.WriteFile(store.path, conflict, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.writeV2(t.Context(), second); err == nil {
		t.Fatal("changed predecessor was overwritten")
	}
	if err := second.custody.close(); err == nil {
		t.Fatal("late predecessor custody change was discarded")
	}
	actual, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(actual, conflict) {
		t.Fatalf("conflicting predecessor was not preserved: %q %v", actual, err)
	}
}

func TestIntentV2HistoryWireBoundPrecedesDecode(t *testing.T) {
	store := intentV2PublicationTest(t)
	store.v2.runtime.cfg.EvidenceV2.Bounds.MaxHistoryBytes = 16
	if err := os.WriteFile(store.path, bytes.Repeat([]byte(" "), 17), 0o600); err != nil {
		t.Fatal(err)
	}
	if read, err := store.readV2(t.Context()); err == nil || read != nil {
		if read != nil {
			_ = read.custody.close()
		}
		t.Fatal("over-bound intent history reached decode")
	}
}

// Repeated full verifiers must not revisit the retained first replay. Both
// ordinary and terminal paths stay private to each traversal and value owner.
func TestIntentV2RepeatedReplayOwnsFreshOrdinaryAndTerminalNamespaces(t *testing.T) {
	store := intentV2PublicationTest(t)
	runtime := store.v2.runtime
	runtime.cfg.EvidenceV2.Operators = []ReleaseEvidenceV2OperatorConfig{{NoID: 1, ReplayScratchRoot: store.stateDir}}
	occupied := filepath.Join(store.stateDir, "retained-first-replay")
	if err := os.Mkdir(occupied, 0o700); err != nil {
		t.Fatal(err)
	}
	expected := AttemptCutV2Context{}
	expected.Identity.NoID = 1
	measurement := AttemptCutV2MeasurementOptions{Replay: AttemptCutV2ReplayOptions{ScratchDirectory: occupied}}
	options := ReleaseMeasurementV2Options{MaxOperators: 1,
		Operators:  map[uint64]ReleaseMeasurementV2OperatorOptions{1: {Expected: expected, Measurement: measurement}},
		Settlement: &AttemptSettlementV2Options{Operators: map[uint64]AttemptSettlementV2OperatorOptions{1: {Expected: expected, Measurement: measurement}}},
	}
	first, err := runtime.measurementReplayOptionsV2(t.Context(), options, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.measurementReplayOptionsV2(t.Context(), first, "second")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{occupied: true}
	for _, value := range []ReleaseMeasurementV2Options{first, second} {
		for _, path := range []string{value.Operators[1].Measurement.Replay.ScratchDirectory, value.Settlement.Operators[1].Measurement.Replay.ScratchDirectory} {
			if seen[path] || filepath.Dir(path) != store.stateDir {
				t.Fatalf("replay path reused or escaped its owned root: %s", path)
			}
			seen[path] = true
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fresh verifier path is already occupied: %v", err)
			}
		}
	}
	if options.Operators[1].Measurement.Replay.ScratchDirectory != occupied || options.Settlement.Operators[1].Measurement.Replay.ScratchDirectory != occupied {
		t.Fatal("refresh mutated the preceding verifier's options")
	}
	if info, err := os.Lstat(occupied); err != nil || !info.IsDir() {
		t.Fatalf("retained prior replay was changed: %v", err)
	}
}

func TestIntentV2CancelledReplayPublishesNoNamespaceOptions(t *testing.T) {
	store := intentV2PublicationTest(t)
	runtime := store.v2.runtime
	runtime.cfg.EvidenceV2.Operators = []ReleaseEvidenceV2OperatorConfig{{NoID: 1, ReplayScratchRoot: store.stateDir}}
	expected := AttemptCutV2Context{}
	expected.Identity.NoID = 1
	options := ReleaseMeasurementV2Options{MaxOperators: 1, Operators: map[uint64]ReleaseMeasurementV2OperatorOptions{1: {Expected: expected}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	actual, err := runtime.measurementReplayOptionsV2(ctx, options, "cancelled")
	if !errors.Is(err, context.Canceled) || actual.Operators != nil || actual.Settlement != nil {
		t.Fatalf("cancelled replay published options: %#v %v", actual, err)
	}
}

// A zero control owner cannot borrow from a larger aggregate history owner,
// even when the intent file is genuinely absent.
func TestIntentV2MissingControlOwnerRefusesBeforePublication(t *testing.T) {
	store := intentV2PublicationTest(t)
	store.v2.runtime.cfg.EvidenceV2.Bounds.MaxControlBytes = 0
	if read, err := store.readV2(t.Context()); err == nil || read != nil {
		if read != nil {
			_ = read.custody.close()
		}
		t.Fatal("missing control owner admitted an intent read")
	}
	entries, err := os.ReadDir(store.stateDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing control owner mutated intent namespace: %v %v", entries, err)
	}
}

// A finite control allowance likewise cannot replace the aggregate history
// owner. Both limits are explicit inputs to the real intent reader.
func TestIntentV2MissingHistoryOwnerRefusesBeforePublication(t *testing.T) {
	store := intentV2PublicationTest(t)
	store.v2.runtime.cfg.EvidenceV2.Bounds.MaxHistoryBytes = 0
	if read, err := store.readV2(t.Context()); err == nil || read != nil {
		if read != nil {
			_ = read.custody.close()
		}
		t.Fatal("missing history owner admitted an intent read")
	}
	entries, err := os.ReadDir(store.stateDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing history owner mutated intent namespace: %v %v", entries, err)
	}
}
