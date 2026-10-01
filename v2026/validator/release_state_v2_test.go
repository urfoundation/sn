//go:build linux || darwin

// Actual configured activation files and chain readers precede real disk
// acquisition. Failure controls observe owned backends, not fake close flags.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Provision only the independently configured private state roots. The actual
// bootstrap authenticates signatures/native identity and initial EVM views.
func newReleaseEvidenceV2DiskTestFixture(t *testing.T) (*releaseInitialBoundaryV2TestFixture, []releaseEvidenceV2ActivationInput) {
	t.Helper()
	fixture := newReleaseInitialBoundaryV2TestFixture(t, "")
	inputs := fixture.inputs(t)
	if history, err := authenticateReleaseEvidenceV2InitialHistory(t.Context(), &fixture.cfg, fixture.chain, inputs, nil); err != nil || history != nil {
		t.Fatalf("actual pristine activation/boundary authority: %v", err)
	}
	for _, path := range append([]string{fixture.cfg.StateDir}, fixture.cfg.Operators[0].StateDir, fixture.cfg.Operators[1].StateDir) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return fixture, inputs
}

// Pristine acquisition keeps the historical UID and existing disk limits.
// Even a completely successful load has no attached or active Stats engine.
func TestReleaseEvidenceV2DiskRealPristineOwnership(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	owned, err := openReleaseEvidenceV2DiskState(t.Context(), &fixture.cfg, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owned.close(); err != nil {
			t.Error(err)
		}
	})
	if len(owned.states) != 2 || len(owned.participants) != 2 || owned.initialHistory != nil || owned.journalPresent {
		t.Fatal("pristine acquisition lost complete membership or invented history")
	}
	for index, participant := range owned.participants {
		state := owned.states[participant.NoID]
		store, disk := participant.Ledger.disk.(*attemptRecordStore)
		if !disk || store == nil || state.ledger != participant.Ledger || participant.Ledger.identity != inputs[index].Context.InitialCut.Identity || participant.Stats != state.stats || participant.Stats.attemptLedger != nil || participant.Stats.attemptV2 != nil || participant.Stats.settlementEpochKnown || owned.snapshots[index] != nil || owned.snapshotPresent[index] {
			t.Fatal("pristine load changed identity or attached statistics")
		}
		head, err := participant.Ledger.Head()
		if err != nil || head.LastSequence != 0 || head.Root != zeroAttemptHash() {
			t.Fatalf("actual pristine durable head: %+v/%v", head, err)
		}
		if _, err := os.Lstat(filepath.Join(participant.StateDir, "stats.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("mere load created Stats: %v", err)
		}
	}
	if err := owned.close(); err != nil {
		t.Fatal(err)
	}
	if err := owned.close(); err != nil {
		t.Fatal(err)
	}
	for _, participant := range owned.participants {
		if _, err := participant.Ledger.Head(); err == nil {
			t.Fatal("closed disk owner still accepts a head read")
		}
	}
}

// A canonical existing snapshot remains an untrusted detached candidate;
// opening may not rewrite it or install it in the public Stats object.
func TestReleaseEvidenceV2DiskReadsBoundedDetachedSnapshots(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	want := make([][]byte, len(inputs))
	for index, operator := range fixture.cfg.Operators {
		stats := NewStatsEngine(StatsConfig{AMin: fixture.cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis})
		if err := stats.Save(operator.StateDir); err != nil {
			t.Fatal(err)
		}
		var err error
		want[index], err = os.ReadFile(filepath.Join(operator.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	owned, err := openReleaseEvidenceV2DiskState(t.Context(), &fixture.cfg, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owned.close(); err != nil {
			t.Error(err)
		}
	}()
	for index, participant := range owned.participants {
		if !owned.snapshotPresent[index] || owned.snapshots[index] == nil || owned.snapshots[index] == participant.Stats || !bytes.Equal(owned.snapshotBytes[index], want[index]) || participant.Stats.attemptLedger != nil {
			t.Fatal("load discarded candidate bytes or published attachment")
		}
		after, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil || !bytes.Equal(after, want[index]) {
			t.Fatalf("candidate load changed durable image: %v", err)
		}
	}
}

// Every key is checked before the first imported-prefix receipt is written.
func TestReleaseEvidenceV2DiskRejectsChangedKeyBeforeOpening(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	inputs[1].PrivateKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x79}, ed25519.SeedSize))
	opened := false
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(uint64, *AttemptLedger) { opened = true }})
	if err == nil || owned != nil || opened {
		t.Fatalf("changed key reached ledger acquisition: %v", err)
	}
	for _, operator := range fixture.cfg.Operators {
		if _, err := os.Lstat(filepath.Join(operator.StateDir, attemptLedgerImportName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("key refusal created import state: %v", err)
		}
	}
}

// Keeping an old in-memory key cannot conceal changed configured seed bytes.
func TestReleaseEvidenceV2DiskRechecksConfiguredKeyFile(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	if err := os.WriteFile(fixture.cfg.Operators[1].ClientKeySeedFile, bytes.Repeat([]byte{0x7a}, ed25519.SeedSize), 0o600); err != nil {
		t.Fatal(err)
	}
	owned, err := openReleaseEvidenceV2DiskState(t.Context(), &fixture.cfg, inputs, nil)
	if err == nil || owned != nil || !strings.Contains(err.Error(), "differs from configured private input") {
		t.Fatalf("cached key replaced current file authority: %v", err)
	}
	for _, operator := range fixture.cfg.Operators {
		if _, err := os.Lstat(filepath.Join(operator.StateDir, attemptLedgerImportName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("key-file refusal created import state: %v", err)
		}
	}
}

// The already authenticated loader result does not waive byte-reference checks.
func TestReleaseEvidenceV2DiskRechecksHistoryBeforeOpening(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	for index := range inputs {
		inputs[index].HistoryBytes = append(bytes.Clone(inputs[index].HistoryBytes), '\n')
	}
	owned, err := openReleaseEvidenceV2DiskState(t.Context(), &fixture.cfg, inputs, nil)
	if err == nil || owned != nil || !strings.Contains(err.Error(), "bytes differ from the configured reference") {
		t.Fatalf("changed complete history reached disk acquisition: %v", err)
	}
	for _, operator := range fixture.cfg.Operators {
		if _, err := os.Lstat(filepath.Join(operator.StateDir, attemptLedgerImportName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("history refusal created import state: %v", err)
		}
	}
}

// The second member's oversized snapshot closes both actual acquired ledgers;
// absence, close failure and capacity failure are never converted into a reset.
func TestReleaseEvidenceV2DiskSnapshotCapacityClosesAllOwners(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	path := filepath.Join(fixture.cfg.Operators[1].StateDir, "stats.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(int64(fixture.cfg.EvidenceV2.Bounds.Persistence.MaxSnapshotBytes)+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	var stateLock sync.Mutex
	ledgers := map[uint64]*AttemptLedger{}
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(noID uint64, ledger *AttemptLedger) {
		stateLock.Lock()
		defer stateLock.Unlock()
		ledgers[noID] = ledger
	}})
	if err == nil || owned != nil || !strings.Contains(err.Error(), "bounded") || len(ledgers) != 2 {
		t.Fatalf("oversized late member did not reject complete ownership: %v", err)
	}
	for _, ledger := range ledgers {
		if _, err := ledger.Head(); err == nil {
			t.Fatal("late capacity refusal leaked an acquired disk owner")
		}
	}
	info, statErr := os.Stat(path)
	if statErr != nil || uint64(info.Size()) != fixture.cfg.EvidenceV2.Bounds.Persistence.MaxSnapshotBytes+1 {
		t.Fatalf("refusal reset the oversized snapshot: %v", statErr)
	}
}

// The independent journal bound is enforced before an operator snapshot read.
func TestReleaseEvidenceV2DiskJournalCapacityPrecedesSnapshots(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	path := attemptSettlementTransactionV2Path(fixture.cfg.StateDir)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(int64(fixture.cfg.EvidenceV2.Bounds.Persistence.MaxJournalBytes)+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	reads := 0
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{beforeRead: func(uint64) { reads++ }})
	if err == nil || owned != nil || !strings.Contains(err.Error(), "bounded") || reads != 0 {
		t.Fatalf("oversized journal crossed its read admission: %v/%d", err, reads)
	}
	info, statErr := os.Stat(path)
	if statErr != nil || uint64(info.Size()) != fixture.cfg.EvidenceV2.Bounds.Persistence.MaxJournalBytes+1 {
		t.Fatalf("refusal reset the oversized journal: %v", statErr)
	}
}

// Explicit barriers prove both independent owners exist before cancellation;
// return cannot precede their callbacks, worker joins or real backend Close.
func TestReleaseEvidenceV2DiskCancellationJoinsBothAcquisitions(t *testing.T) {
	prior := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(prior)
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opened := make(chan *AttemptLedger, 2)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		owned, err := openReleaseEvidenceV2DiskStateWithObserver(ctx, &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(_ uint64, ledger *AttemptLedger) { opened <- ledger; <-release }})
		if owned != nil {
			err = errors.Join(err, errors.New("canceled startup published disk owners"), owned.close())
		}
		done <- err
	}()
	first, second := <-opened, <-opened
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquisition lost its cause: %v", err)
	}
	for _, ledger := range []*AttemptLedger{first, second} {
		if _, err := ledger.Head(); err == nil {
			t.Fatal("canceled acquisition returned before closing an owner")
		}
	}
}

// A nonreturning observer cannot be mistaken for successful acquisition merely
// because the backend was assigned before its worker left via runtime.Goexit.
func TestReleaseEvidenceV2DiskLostCompletionClosesActualOwners(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	var stateLock sync.Mutex
	ledgers := []*AttemptLedger{}
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(_ uint64, ledger *AttemptLedger) {
		stateLock.Lock()
		ledgers = append(ledgers, ledger)
		stateLock.Unlock()
		runtime.Goexit()
	}})
	if err == nil || owned != nil || !strings.Contains(err.Error(), "acquisition did not complete") || len(ledgers) == 0 {
		t.Fatalf("lost worker completion was accepted: %v", err)
	}
	for _, ledger := range ledgers {
		if _, err := ledger.Head(); err == nil {
			t.Fatal("nonreturning acquisition leaked a real disk owner")
		}
	}
}

// A late read cannot retarget an earlier operator directory and still publish
// the complete batch. Keep both physical copies for forensic inspection.
func TestReleaseEvidenceV2DiskLateDirectoryRetargetClosesAllOwners(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	first := fixture.cfg.Operators[0].StateDir
	var stateLock sync.Mutex
	ledgers := []*AttemptLedger{}
	var movedErr error
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{
		opened: func(_ uint64, ledger *AttemptLedger) {
			stateLock.Lock()
			defer stateLock.Unlock()
			ledgers = append(ledgers, ledger)
		},
		beforeRead: func(noID uint64) {
			if noID == inputs[1].Config.NoID {
				movedErr = os.Rename(first, first+"-retained")
				if movedErr == nil {
					movedErr = os.Mkdir(first, 0o700)
				}
			}
		},
	})
	if movedErr != nil {
		t.Fatal(movedErr)
	}
	if err == nil || owned != nil || len(ledgers) != 2 {
		t.Fatalf("retargeted prior member published a batch: %v", err)
	}
	for _, ledger := range ledgers {
		if _, err := ledger.Head(); err == nil {
			t.Fatal("retarget refusal leaked an acquired disk owner")
		}
	}
}

// Pre-cancel cannot even create the first ledger's import provenance.
func TestReleaseEvidenceV2DiskPrecancelHasNoAcquisition(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	owned, err := openReleaseEvidenceV2DiskState(ctx, &fixture.cfg, inputs, nil)
	if !errors.Is(err, context.Canceled) || owned != nil {
		t.Fatalf("precancel: %v", err)
	}
	for _, operator := range fixture.cfg.Operators {
		if _, err := os.Lstat(filepath.Join(operator.StateDir, attemptLedgerImportName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("precancel created import state: %v", err)
		}
	}
}

// One worker orders the first callback before the later constructor. Mutating
// the borrowed key bytes cannot change a key already admitted for that member.
func TestReleaseEvidenceV2DiskOwnsLaterSigningKeyBeforeOpening(t *testing.T) {
	prior := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prior)
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	first, second := inputs[0].Config.NoID, inputs[1].Config.NoID
	want := bytes.Clone(inputs[1].PrivateKey)
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(noID uint64, _ *AttemptLedger) {
		if noID == first {
			clear(inputs[1].PrivateKey)
		}
	}})
	if owned != nil {
		t.Cleanup(func() {
			if err := owned.close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil || owned == nil || !bytes.Equal(owned.states[second].ledger.vsk, want) {
		t.Fatalf("borrowed key mutation changed the admitted signer: %v", err)
	}
}

// The final acquisition callback runs before publication and cleanup routing.
// Member labels stay owned even when the caller collapses its borrowed census.
func TestReleaseEvidenceV2DiskOwnsMemberCensusBeforeCallbacks(t *testing.T) {
	prior := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prior)
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	first, second := inputs[0].Config.NoID, inputs[1].Config.NoID
	var ledgers []*AttemptLedger
	t.Cleanup(func() {
		for _, ledger := range ledgers {
			if err := ledger.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(noID uint64, ledger *AttemptLedger) {
		ledgers = append(ledgers, ledger)
		if noID == second {
			inputs[0].Config.NoID = second
		}
	}})
	if owned != nil {
		t.Cleanup(func() {
			if err := owned.close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil || owned == nil || len(owned.states) != 2 || len(owned.participants) != 2 || owned.states[first] == nil || owned.states[second] == nil || owned.participants[0].NoID != first || owned.participants[1].NoID != second {
		t.Fatalf("borrowed operator census changed acquired owners: %v", err)
	}
	if err := owned.close(); err != nil {
		t.Fatal(err)
	}
	for _, ledger := range ledgers {
		if _, err := ledger.Head(); err == nil {
			t.Fatal("owned census close lost an acquired ledger")
		}
	}
}

// The same alias cannot suppress cleanup when a later physical read fails.
// Emergency test cleanup runs only after checking the real owner's result.
func TestReleaseEvidenceV2DiskBorrowedCensusFailureClosesEveryLedger(t *testing.T) {
	prior := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prior)
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	second := inputs[1].Config.NoID
	path := filepath.Join(fixture.cfg.Operators[1].StateDir, "stats.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(int64(fixture.cfg.EvidenceV2.Bounds.Persistence.MaxSnapshotBytes)+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	var ledgers []*AttemptLedger
	t.Cleanup(func() {
		for _, ledger := range ledgers {
			if err := ledger.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(noID uint64, ledger *AttemptLedger) {
		ledgers = append(ledgers, ledger)
		if noID == second {
			inputs[0].Config.NoID = second
		}
	}})
	if owned != nil {
		t.Cleanup(func() {
			if err := owned.close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err == nil || owned != nil || len(ledgers) != 2 {
		t.Fatalf("late read refusal did not retain the complete acquired census: %v", err)
	}
	for _, ledger := range ledgers {
		if _, err := ledger.Head(); err == nil {
			t.Fatal("borrowed census hid an acquired ledger from failure cleanup")
		}
	}
	if !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("borrowed census changed the admitted late read failure: %v", err)
	}
}

// A later constructor uses the same admitted finite disk allowance even after
// the first callback changes the caller's mutable configuration value.
func TestReleaseEvidenceV2DiskOwnsDiskBoundsBeforeOpening(t *testing.T) {
	prior := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prior)
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	first := inputs[0].Config.NoID
	want := fixture.cfg.EvidenceV2.Bounds.Disk
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(noID uint64, _ *AttemptLedger) {
		if noID == first {
			fixture.cfg.EvidenceV2.Bounds.Disk.MaxStorageFiles = 0
		}
	}})
	if owned != nil {
		t.Cleanup(func() {
			if err := owned.close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil || owned == nil {
		t.Fatalf("borrowed disk bounds changed later acquisition: %v", err)
	}
	for _, participant := range owned.participants {
		if participant.Ledger.diskLimits != want {
			t.Fatal("acquisition retained changed disk bounds")
		}
	}
}

// Snapshot allocation and decoding consume one admitted allowance. A callback
// cannot replace it between ledger acquisition and the actual retained read.
func TestReleaseEvidenceV2DiskOwnsPersistenceBoundsBeforeReads(t *testing.T) {
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	for _, operator := range fixture.cfg.Operators {
		stats := NewStatsEngine(StatsConfig{AMin: fixture.cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis})
		if err := stats.Save(operator.StateDir); err != nil {
			t.Fatal(err)
		}
	}
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{beforeRead: func(uint64) {
		fixture.cfg.EvidenceV2.Bounds.Persistence.MaxSnapshotBytes = 1
	}})
	if owned != nil {
		t.Cleanup(func() {
			if err := owned.close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil || owned == nil {
		t.Fatalf("borrowed snapshot bound changed retained reads: %v", err)
	}
	for index := range owned.participants {
		if !owned.snapshotPresent[index] || owned.snapshots[index] == nil {
			t.Fatal("owned snapshot allowance lost a complete candidate")
		}
	}
}

// Namespace and scoring values are admitted before any external callback.
// Path mutation cannot reroute the journal owner or change a later Stats engine.
func TestReleaseEvidenceV2DiskOwnsNamespaceAndScoringBeforeOpening(t *testing.T) {
	prior := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prior)
	fixture, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	first := inputs[0].Config.NoID
	want := fixture.cfg.Policy.Verify.ReliabilityAMin
	owned, err := openReleaseEvidenceV2DiskStateWithObserver(t.Context(), &fixture.cfg, inputs, nil, releaseEvidenceV2DiskObserver{opened: func(noID uint64, _ *AttemptLedger) {
		if noID == first {
			fixture.cfg.StateDir += "-changed"
			fixture.cfg.Coordinator = "0x" + strings.Repeat("7b", 20)
			fixture.cfg.Policy.Verify.ReliabilityAMin++
		}
	}})
	if owned != nil {
		t.Cleanup(func() {
			if err := owned.close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil || owned == nil {
		t.Fatalf("borrowed namespace or scoring changed acquired state: %v", err)
	}
	for _, participant := range owned.participants {
		if participant.Stats.cfg.AMin != want {
			t.Fatal("later Stats engine adopted changed scoring authority")
		}
	}
}
