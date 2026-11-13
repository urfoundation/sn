// Runtime ownership regressions use real checkpoint bytes and instance-local
// kernel facts. No daemon policy can be inferred from an absent reference.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Cancellation is delivered at the second explicit admission observation.
type monitorAdmissionCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Uint64
}

// The real cancellation channel closes before storage Require observes Err.
func (self *monitorAdmissionCancelContext) Err() error {
	if self.checks.Add(1) == 2 {
		self.cancel()
	}
	return self.Context.Err()
}

// Cancellation racing the next admission boundary is still a joined stop.
func TestMonitorCancellationAtStorageAdmissionClosesQuietly(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &monitorAdmissionCancelContext{Context: parent, cancel: cancel}
	var stdout, stderr bytes.Buffer
	if code := runChainMonitor(ctx, nil, monitorTestExpectation(), "/synthetic/unused/checkpoint", "", time.Hour, time.Hour, &stdout, &stderr, time.Now, monitorServiceHooks{}); code != 0 {
		t.Fatalf("canceled chain admission=%d: %s", code, stderr.String())
	}
	if parent.Err() != context.Canceled {
		t.Fatal("admission did not cause cancellation")
	}
}

// An already-joined cancellation must not be reclassified as storage corruption.
func TestMonitorCanceledBeforeStorageAdmissionClosesQuietly(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := runChainMonitor(ctx, nil, monitorTestExpectation(), "/synthetic/unused/checkpoint", "", time.Hour, time.Hour, &stdout, &stderr, time.Now, monitorServiceHooks{}); code != 0 {
		t.Fatalf("canceled chain admission=%d: %s", code, stderr.String())
	}
	if code := runMonitorServices(ctx, nil, monitorTestExpectation(), &monitorServicesPolicy{}, "/synthetic/unused/checkpoint", "", time.Hour, time.Hour, &stdout, &stderr, time.Now, monitorServiceHooks{}); code != 0 {
		t.Fatalf("canceled service admission=%d: %s", code, stderr.String())
	}
}

// The persistent runtime refuses before the first Rpc or checkpoint creation.
func TestMonitorPersistentLibraryRequiresExplicitVolume(t *testing.T) {
	root := mainnetPrivateTestDir(t)
	var stdout, stderr bytes.Buffer
	code := runChainMonitor(t.Context(), nil, monitorTestExpectation(), filepath.Join(root, "checkpoint.json"), "", time.Hour, time.Hour, &stdout, &stderr, time.Now, monitorServiceHooks{})
	entries, err := os.ReadDir(root)
	if code != 3 || err != nil || len(entries) != 0 || stdout.Len() != 0 {
		t.Fatalf("unguarded persistent entry code=%d files=%d err=%v stderr=%s", code, len(entries), err, stderr.String())
	}
}

// Capacity recovery stays on the same live owner and preserves acknowledged bytes.
func TestMonitorDurableReserveRecoveryRetainsSameOwner(t *testing.T) {
	root := mainnetPrivateTestDir(t)
	fixture := durablefixture.New(t, t.Context(), root)
	path := filepath.Join(root, "checkpoint.json")
	provisionMonitorTestCustody(t, path)
	store, err := openMonitorCheckpoint(path, monitorTestExpectation(), fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Unix(1700000000, 0).UTC()}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := store.directory.file
	fixture.Host.SetReserve(0, 0)
	state.lastNumber++
	if err := store.save(state); !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("reserve pressure changed identity", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused publication changed retained bytes", err)
	}
	fixture.Host.SetReserve(1024*1024, 1024)
	if err := store.save(state); err != nil || owner != store.directory.file {
		t.Fatal("same-owner recovery failed", err)
	}
	loaded, err := store.load()
	if err != nil || loaded.lastNumber != 101 {
		t.Fatal("recovered state differs", loaded, err)
	}
}

// A lost root poisons only its retained owner. The unaffected root stays live;
// a joined reopen of the original root retains its completed checkpoint.
func TestMonitorDurableReplacementRecoveryIsPerRoot(t *testing.T) {
	parent := mainnetPrivateTestDir(t)
	first, second := filepath.Join(parent, "first"), filepath.Join(parent, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := durablefixture.New(t, t.Context(), first, second)
	provisionMonitorTestCustody(t, filepath.Join(first, "checkpoint.json"))
	provisionMonitorTestCustody(t, filepath.Join(second, "checkpoint.json"))
	a, err := openMonitorCheckpoint(filepath.Join(first, "checkpoint.json"), monitorTestExpectation(), fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	b, err := openMonitorCheckpoint(filepath.Join(second, "checkpoint.json"), monitorTestExpectation(), fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Unix(1700000000, 0).UTC()}
	if err := errors.Join(a.save(state), b.save(state)); err != nil {
		t.Fatal(err)
	}
	retained := first + "-retained"
	if err := os.Rename(first, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(first, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.save(state); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("replacement root accepted", err)
	}
	if err := b.save(state); err != nil {
		t.Fatal("other root stopped", err)
	}
	entries, err := os.ReadDir(first)
	if err != nil || len(entries) != 0 {
		t.Fatal("replacement received custody", err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, first); err != nil {
		t.Fatal(err)
	}
	if err := a.save(state); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("invalidated owner was resurrected", err)
	}
	if err := a.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openMonitorCheckpoint(filepath.Join(first, "checkpoint.json"), monitorTestExpectation(), fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	loaded, err := reopened.load()
	if err != nil || loaded.lastNumber != state.lastNumber {
		t.Fatal("reopen lost completed custody", loaded, err)
	}
	if err := b.save(state); err != nil {
		t.Fatal("unrelated owner did not survive recovery", err)
	}
}
