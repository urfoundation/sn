//go:build linux || darwin

package validator

// Mutable runtime projections cannot redefine the actual acquired cleanup
// census. These controls inspect real disk ledgers and actual native closes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type releaseEvidenceV2DiskCloseTestFixture struct {
	cfg     ReleaseConfig
	inputs  []releaseEvidenceV2ActivationInput
	disk    *releaseEvidenceV2DiskState
	ledgers []*AttemptLedger
	noIDs   []uint64
}

// A real authenticated bootstrap and real disk acquisition precede every
// mutation. Fallback cleanup also joins resources in causal failing overlays;
// the test bodies inspect each actual Close outcome before that fallback.
func newReleaseEvidenceV2DiskCloseTestFixture(t *testing.T) *releaseEvidenceV2DiskCloseTestFixture {
	t.Helper()
	base, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	disk, err := openReleaseEvidenceV2DiskState(t.Context(), &base.cfg, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &releaseEvidenceV2DiskCloseTestFixture{cfg: base.cfg, inputs: inputs, disk: disk}
	for _, participant := range disk.participants {
		fixture.ledgers = append(fixture.ledgers, participant.Ledger)
		fixture.noIDs = append(fixture.noIDs, participant.NoID)
	}
	t.Cleanup(func() {
		for _, ledger := range fixture.ledgers {
			_ = ledger.Close()
		}
	})
	return fixture
}

func requireReleaseEvidenceV2DiskClosed(t *testing.T, ledgers []*AttemptLedger, message string) {
	t.Helper()
	for _, ledger := range ledgers {
		if _, err := ledger.Head(); !errors.Is(err, errAttemptLedgerClosed) {
			t.Fatalf("%s: %v", message, err)
		}
	}
}

// This is the exact adjacent failure exposed by semantic startup's late
// destination-census refusal: a deleted projection entry still owns a ledger.
func TestReleaseEvidenceV2DiskCloseRetainsRemovedDestination(t *testing.T) {
	t.Parallel()
	fixture := newReleaseEvidenceV2DiskCloseTestFixture(t)
	delete(fixture.disk.states, fixture.noIDs[1])
	if err := fixture.disk.close(); err != nil {
		t.Fatal(err)
	}
	requireReleaseEvidenceV2DiskClosed(t, fixture.ledgers, "removed destination suppressed an acquired ledger close")
}

// Neither a shortened participant view nor a discarded map can erase the
// acquisition plan after startup has returned its real backend owners.
func TestReleaseEvidenceV2DiskCloseRetainsClearedProjectionCensus(t *testing.T) {
	t.Parallel()
	fixture := newReleaseEvidenceV2DiskCloseTestFixture(t)
	fixture.disk.states, fixture.disk.participants, fixture.disk.snapshots = nil, nil, nil
	if err := fixture.disk.close(); err != nil {
		t.Fatal(err)
	}
	requireReleaseEvidenceV2DiskClosed(t, fixture.ledgers, "cleared runtime views erased the actual close census")
}

// Capturing a slice of mutable state pointers is insufficient: those pointed-
// to ledger fields can be rebound while the original backend is still open.
func TestReleaseEvidenceV2DiskCloseOwnsLedgerValuesBeforeRetarget(t *testing.T) {
	t.Parallel()
	fixture := newReleaseEvidenceV2DiskCloseTestFixture(t)
	fixture.disk.states[fixture.noIDs[0]].ledger = fixture.ledgers[1]
	fixture.disk.participants[0].Ledger = fixture.ledgers[1]
	if err := fixture.disk.close(); err != nil {
		t.Fatal(err)
	}
	requireReleaseEvidenceV2DiskClosed(t, fixture.ledgers, "retargeted state pointer stole an acquired ledger close")
}

// A foreign real disk owner inserted into the public map remains its caller's
// resource; shutdown cannot acquire authority over it from that map entry.
func TestReleaseEvidenceV2DiskCloseDoesNotAcquireForeignLedger(t *testing.T) {
	t.Parallel()
	fixture := newReleaseEvidenceV2DiskCloseTestFixture(t)
	foreign, err := NewDiskAttemptLedger(t.Context(), newAttemptSettlementRuntimeV2TestStateDir(t), fixture.inputs[0].Context.InitialCut.Identity, strings.ToLower(fixture.cfg.Coordinator), fixture.inputs[0].PrivateKey, fixture.cfg.EvidenceV2.Bounds.Disk)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := foreign.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.disk.states[fixture.noIDs[len(fixture.noIDs)-1]+1] = &releaseAttemptState{ledger: foreign}
	if err := fixture.disk.close(); err != nil {
		t.Fatal(err)
	}
	requireReleaseEvidenceV2DiskClosed(t, fixture.ledgers, "foreign projection changed original cleanup")
	if _, err := foreign.Head(); err != nil {
		t.Fatalf("public map insertion acquired a foreign ledger close: %v", err)
	}
}

// The semantic startup's private Stats/proof wrapper borrows actual ledgers;
// copying those fields never transfers the root's exclusive Close ownership.
func TestReleaseEvidenceV2DiskCloseCannotClaimBorrowedProjection(t *testing.T) {
	t.Parallel()
	fixture := newReleaseEvidenceV2DiskCloseTestFixture(t)
	borrowed := &releaseEvidenceV2DiskState{states: map[uint64]*releaseAttemptState{}, participants: slices.Clone(fixture.disk.participants)}
	for noID, state := range fixture.disk.states {
		borrowed.states[noID] = state
	}
	if err := borrowed.close(); err != nil {
		t.Fatal(err)
	}
	for _, ledger := range fixture.ledgers {
		if _, err := ledger.Head(); err != nil {
			t.Fatalf("borrowed projection fields claimed root ledger cleanup: %v", err)
		}
	}
	if err := fixture.disk.close(); err != nil {
		t.Fatal(err)
	}
	requireReleaseEvidenceV2DiskClosed(t, fixture.ledgers, "root lost cleanup after private projection close")
}

// Closing a real retained native descriptor and a distinct backend lock file
// makes their subsequent actual Close calls fail. Both causes and every later
// member must survive, including the cached result returned by a second close.
func TestReleaseEvidenceV2DiskClosePreservesNativeAndBackendFailures(t *testing.T) {
	t.Parallel()
	fixture := newReleaseEvidenceV2DiskCloseTestFixture(t)
	if err := fixture.ledgers[0].directory.directory.Close(); err != nil {
		t.Fatal(err)
	}
	backend, ok := fixture.ledgers[1].disk.(*attemptRecordStore)
	if !ok || backend.disk.ownerFile == nil {
		t.Fatal("actual backend lock file is absent")
	}
	if err := backend.disk.ownerFile.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.disk.states = nil
	err := fixture.disk.close()
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("cleared projection discarded actual native/backend close errors: %v", err)
	}
	for _, noID := range fixture.noIDs {
		if !strings.Contains(err.Error(), fmt.Sprintf("validator no_id %d attempt ledger shutdown", noID)) {
			t.Fatalf("actual member close cause was dropped: %v", err)
		}
	}
	if repeated := fixture.disk.close(); repeated == nil || repeated.Error() != err.Error() || !errors.Is(repeated, os.ErrClosed) {
		t.Fatalf("idempotent close changed its joined native/backend cause: %v", repeated)
	}
	requireReleaseEvidenceV2DiskClosed(t, fixture.ledgers, "close failure skipped a real acquired backend")
}

// The real Walk first acquires ledger lifetime, then checks this synchronous
// context again when entering the real backend. Holding only that caller
// boundary leaves no state mutex or invented visitor/replay verdict behind.
type releaseEvidenceV2DiskCloseTestContext struct {
	context.Context
	checks  atomic.Uint64
	entered chan struct{}
	release chan struct{}
}

func (self *releaseEvidenceV2DiskCloseTestContext) Err() error {
	if self.checks.Add(1) == 2 {
		close(self.entered)
		<-self.release
	}
	return self.Context.Err()
}

// All concurrent callers join the same actual cancellation and admitted Walk,
// even when the runtime map no longer names that live operation's disk owner.
func TestReleaseEvidenceV2DiskCloseConcurrentlyJoinsActualAdmission(t *testing.T) {
	t.Parallel()
	fixture := newReleaseEvidenceV2DiskCloseTestFixture(t)
	ledger := fixture.ledgers[0]
	backend, ok := ledger.disk.(*attemptRecordStore)
	if !ok {
		t.Fatal("actual disk backend is absent")
	}
	ctx := &releaseEvidenceV2DiskCloseTestContext{Context: t.Context(), entered: make(chan struct{}), release: make(chan struct{})}
	walked := make(chan error, 1)
	closed := make(chan error, 3)
	var closers sync.WaitGroup
	var release sync.Once
	walkJoined := false
	unblock := func() { release.Do(func() { close(ctx.release) }) }
	defer func() {
		unblock()
		closers.Wait()
		if !walkJoined {
			<-walked
		}
	}()
	go func() {
		walked <- ledger.Walk(ctx, 1, 1, func(AttemptRecord) error { return errors.New("closed empty ledger unexpectedly visited a row") })
	}()
	<-ctx.entered
	fixture.disk.states = nil
	for range cap(closed) {
		closers.Add(1)
		go func() { defer closers.Done(); closed <- fixture.disk.close() }()
	}
	select {
	case <-ledger.ctx.Done():
	case err := <-closed:
		t.Fatalf("root close returned before canceling its admitted actual ledger: %v", err)
	}
	<-backend.ctx.Done()
	select {
	case err := <-closed:
		t.Fatalf("root close returned before its actual admitted Walk joined: %v", err)
	default:
	}
	unblock()
	for range cap(closed) {
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
	}
	walkErr := <-walked
	walkJoined = true
	if !errors.Is(walkErr, errAttemptRecordStoreClosed) && !errors.Is(walkErr, errAttemptLedgerClosed) {
		t.Fatalf("actual closed Walk returned another cause: %v", walkErr)
	}
	requireReleaseEvidenceV2DiskClosed(t, fixture.ledgers, "concurrent close left a real census member open")
}
