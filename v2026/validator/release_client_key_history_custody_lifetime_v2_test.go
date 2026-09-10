//go:build linux || darwin

// A short Rpc ownership scope must not cancel the independently retained
// startup file set. Parent cancellation still owns both complete lifetimes.
package validator

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Real Http capture, actual chain authentication and the real private reader
// remain in the fixture; no callback grants a key or accepts file custody.
type releaseClientKeyCustodyLifetimeV2Fixture struct {
	source      *releaseClientKeyAuthorityV2TestFixture
	marker      context.Context
	reads       *releaseClientKeyAuthorityV2Reads
	custody     *releaseEvidenceV2StartupReferences
	path        string
	contentHash string
}

// The returned custody already owns the original signed capture through the
// real retained-recovery consumer, under the caller's enclosing parent.
func newReleaseClientKeyCustodyLifetimeV2Fixture(t *testing.T, parent context.Context) *releaseClientKeyCustodyLifetimeV2Fixture {
	t.Helper()
	source := newReleaseClientKeyAuthorityV2TestFixture(t)
	source.cfg.StateDir = newReleaseHeadV2TestStateDir(t)
	ctx, capture := source.owner(t, parent, 4*1024*1024)
	reader := newReleaseHeadV2ClientKeyReader(t, &source.cfg, source.domain.NoID)
	registration, hash, size, err := captureReleaseClientKeyV2(ctx, source.chain, reader, source.cfg.StateDir, source.domain, source.request, releaseClientKeyHistoryTestResponseBytes, 4*1024*1024)
	if err != nil || registration.ClientID != source.request.ClientID || hash == "" || size == 0 {
		t.Fatalf("actual original capture failed: %v", err)
	}
	if err := capture.finish(nil); err != nil {
		t.Fatal(err)
	}
	marker, reads := source.owner(t, parent, 4*1024*1024)
	custody := &releaseEvidenceV2StartupReferences{remaining: 4 * 1024 * 1024}
	t.Cleanup(func() { _ = custody.close() })
	registration, err = readRetainedReleaseClientKeyV2(marker, source.chain, custody, source.cfg.StateDir, source.domain, source.request, hash, releaseClientKeyHistoryTestResponseBytes)
	if err != nil || registration.ClientID != source.request.ClientID || registration.PublicKey != ([32]byte{0x31}) || len(custody.owners) != 1 {
		t.Fatalf("actual retained source failed: %v", err)
	}
	path, err := releaseClientKeyCaptureV2Path(source.cfg.StateDir, source.domain, source.request)
	if err != nil {
		t.Fatal(err)
	}
	return &releaseClientKeyCustodyLifetimeV2Fixture{source: source, marker: marker, reads: reads, custody: custody, path: path, contentHash: hash}
}

// This is the production startup ordering: complete short Rpc verification,
// then independently retain/check the same source files for later publication.
func TestReleaseClientKeyAuthorityV2CustodySurvivesRpcScopeCompletion(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyCustodyLifetimeV2Fixture(t, t.Context())
	if err := fixture.reads.finish(nil); err != nil {
		t.Fatal(err)
	}
	if fixture.marker.Err() != nil || !errors.Is(fixture.reads.ctx.Err(), context.Canceled) {
		t.Fatal("Rpc completion cancelled the enclosing custody or left its own owner alive")
	}
	if err := fixture.custody.check(); err != nil {
		t.Fatal("short Rpc completion invalidated the actual retained descriptor", err)
	}
	encoded, err := fixture.custody.read(fixture.marker, fixture.path, releaseClientKeyHistoryTestResponseBytes, false)
	if err != nil || ReleaseMeasurementContentHash(encoded) != fixture.contentHash {
		t.Fatal("later retained source no longer has its exact original bytes", err)
	}
	if err := errors.Join(fixture.custody.check(), fixture.custody.close()); err != nil {
		t.Fatal(err)
	}
}

// Parent cancellation remains fatal even after the short Rpc owner completed;
// retaining its context value cannot turn cancellation into successful custody.
func TestReleaseClientKeyAuthorityV2ParentCancellationStillInvalidatesCustody(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := newReleaseClientKeyCustodyLifetimeV2Fixture(t, parent)
	if err := fixture.reads.finish(nil); err != nil {
		t.Fatal(err)
	}
	if err := fixture.custody.check(); err != nil {
		t.Fatal("custody ended before its actual parent", err)
	}
	cancel()
	markerErr, custodyErr := fixture.marker.Err(), fixture.custody.check()
	if !errors.Is(markerErr, context.Canceled) || !errors.Is(custodyErr, context.Canceled) {
		t.Fatal("parent cancellation failed to invalidate retained source custody", markerErr, custodyErr)
	}
	files := make([]*os.File, len(fixture.custody.owners))
	for index, owner := range fixture.custody.owners {
		files[index] = owner.directory.file
	}
	// Close reports physical cleanup errors independently of cancellation;
	// callers join both before returning an authenticated result.
	if err := fixture.custody.close(); err != nil {
		t.Fatal("cancellation prevented successful physical custody release", err)
	}
	if !fixture.custody.closed {
		t.Fatal("cancelled custody remained open")
	}
	for index, owner := range fixture.custody.owners {
		if _, err := files[index].Stat(); !owner.closed || owner.directory.file != nil || !errors.Is(err, os.ErrClosed) {
			t.Fatal("cancelled custody retained an actual descriptor", index, err)
		}
	}
	if err := fixture.custody.close(); err != nil {
		t.Fatal("repeated custody close changed the original cleanup result", err)
	}
	if err := fixture.custody.check(); err == nil {
		t.Fatal("closed cancelled custody regained authority")
	}
	ownerCount, remaining := len(fixture.custody.owners), fixture.custody.remaining
	encoded, err := fixture.custody.read(fixture.marker, fixture.path, releaseClientKeyHistoryTestResponseBytes, false)
	if err == nil || encoded != nil || len(fixture.custody.owners) != ownerCount || fixture.custody.remaining != remaining {
		t.Fatal("closed cancelled custody reacquired a source or byte allowance", err)
	}
}

// Cancellation during a real Close cannot skip the remaining descriptors or
// erase a late cleanup failure; repeated Close preserves that same result.
func TestReleaseClientKeyAuthorityV2ParentCancellationPreservesLateCustodyCloseError(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := newReleaseClientKeyCustodyLifetimeV2Fixture(t, parent)
	if err := fixture.reads.finish(nil); err != nil {
		t.Fatal(err)
	}
	encoded, err := fixture.custody.read(fixture.marker, fixture.path, releaseClientKeyHistoryTestResponseBytes, false)
	if err != nil || ReleaseMeasurementContentHash(encoded) != fixture.contentHash || len(fixture.custody.owners) != 2 {
		t.Fatal("second real descriptor failed to retain the original signed capture", err)
	}
	lateCloseErr := errors.New("test-only error after actual custody Close")
	closeCalls := 0
	first := fixture.custody.owners[0]
	last := fixture.custody.owners[1]
	files := []*os.File{first.directory.file, last.directory.file}
	last.hooks.afterClose = func(file *os.File) error {
		closeCalls++
		_, closedErr := file.Stat()
		_, liveErr := first.directory.file.Stat()
		if !errors.Is(closedErr, os.ErrClosed) || first.closed || liveErr != nil {
			t.Error("late failure did not follow the first actual reverse-order Close", closedErr, liveErr)
		}
		cancel()
		return lateCloseErr
	}
	closeErr := fixture.custody.close()
	if !errors.Is(closeErr, lateCloseErr) || closeCalls != 1 || !errors.Is(fixture.marker.Err(), context.Canceled) {
		t.Fatal("cancelled custody lost its actual cleanup failure", closeCalls, closeErr, fixture.marker.Err())
	}
	for index, owner := range fixture.custody.owners {
		if _, err := files[index].Stat(); !owner.closed || owner.directory.file != nil || !errors.Is(err, os.ErrClosed) {
			t.Fatal("late cancellation or failure skipped an owned descriptor", index, err)
		}
	}
	if repeatErr := fixture.custody.close(); repeatErr != closeErr || closeCalls != 1 {
		t.Fatal("repeated Close reran cleanup or replaced its retained error", closeCalls, repeatErr)
	}
	if err := fixture.custody.check(); err == nil {
		t.Fatal("failed and cancelled custody regained authority")
	}
	ownerCount, remaining := len(fixture.custody.owners), fixture.custody.remaining
	encoded, err = fixture.custody.read(fixture.marker, fixture.path, releaseClientKeyHistoryTestResponseBytes, false)
	if err == nil || encoded != nil || len(fixture.custody.owners) != ownerCount || fixture.custody.remaining != remaining {
		t.Fatal("failed cleanup silently reopened cancelled custody", err)
	}
}

// A live parent marker is not live Rpc authority: closed-owner checks precede
// map reuse, body admission, new Rpc work and nested owner construction.
func TestReleaseClientKeyAuthorityV2ClosedRpcScopeCannotReacquireAuthority(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyCustodyLifetimeV2Fixture(t, t.Context())
	if err := fixture.reads.finish(nil); err != nil {
		t.Fatal(err)
	}
	before := fixture.source.rpc.counts()
	if signer, err := fixture.source.chain.readReleaseClientKeyAuthorityV2(fixture.marker, fixture.source.domain, fixture.source.request.DecisionBoundary); err == nil || signer != (common.Address{}) {
		t.Fatal("closed scope reused a cached signer", err)
	}
	if _, err := releaseClientKeyCaptureV2Maximum(fixture.marker, releaseClientKeyHistoryTestResponseBytes); err == nil {
		t.Fatal("closed scope allocated a response allowance")
	}
	encoded := releaseClientKeyTestResponse(t, fixture.source.cfg.DeploymentID, fixture.source.domain, fixture.source.request, [][32]byte{{0x31}})
	if registration, err := verifyReleaseClientKeyCaptureV2(fixture.marker, fixture.source.chain, encoded, releaseClientKeyHistoryTestResponseBytes, fixture.source.domain, fixture.source.request, false); err == nil || registration != (protocol.ClientKeyRegistration{}) {
		t.Fatal("closed scope authenticated a new client response", err)
	}
	if _, _, err := newReleaseClientKeyAuthorityV2Reads(fixture.marker, fixture.source.chain, &fixture.source.cfg, &fixture.source.artifact, fixture.source.hotkey, &releaseHeadV2Budget{limit: 4 * 1024 * 1024}); err == nil {
		t.Fatal("closed marker silently acquired a nested authority scope")
	}
	after := fixture.source.rpc.counts()
	if len(after) != len(before) {
		t.Fatal("closed scope reached a new Rpc method")
	}
	for method, count := range before {
		if after[method] != count {
			t.Fatal("closed scope reached actual Rpc", method, count, after[method])
		}
	}
	if err := errors.Join(fixture.marker.Err(), fixture.custody.check(), fixture.custody.close()); err != nil {
		t.Fatal("closed Rpc scope damaged its independent physical custody", err)
	}
}

// The private Rpc cancel still interrupts and joins an actual pending leader,
// while its enclosing retained file set remains physically owned and usable.
func TestReleaseClientKeyAuthorityV2RpcCancellationJoinsLeaderWithoutEndingCustody(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyCustodyLifetimeV2Fixture(t, t.Context())
	barrier := &releaseClientKeyAuthorityV2RpcBarrier{method: "eth_call", entered: make(chan struct{}), release: make(chan struct{}), left: make(chan struct{})}
	fixture.source.rpc.stateLock.Lock()
	fixture.source.rpc.barrier = barrier
	fixture.source.rpc.stateLock.Unlock()
	done := make(chan error, 1)
	go func() {
		signer, err := fixture.source.chain.readReleaseClientKeyAuthorityV2(fixture.marker, fixture.source.domain, fixture.source.prior)
		if signer != (common.Address{}) {
			err = errors.Join(err, errors.New("unfinished leader returned authority"))
		}
		done <- err
	}()
	select {
	case <-barrier.entered:
	case err := <-done:
		t.Fatal("actual new-boundary leader ended before its transport barrier", err)
	}
	if err := fixture.reads.finish(nil); err == nil {
		t.Fatal("unfinished Rpc scope completed successfully")
	}
	fixture.reads.stateLock.Lock()
	inflight, closed := fixture.reads.inflight, fixture.reads.closed
	fixture.reads.stateLock.Unlock()
	if inflight != 0 || !closed {
		t.Fatal("Rpc scope returned before discharging its leader")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("actual Rpc leader did not join its cancellation", err)
	}
	<-barrier.left
	if err := errors.Join(fixture.marker.Err(), fixture.custody.check(), fixture.custody.close()); err != nil {
		t.Fatal("Rpc cancellation escaped into its longer-lived physical custody", err)
	}
}
