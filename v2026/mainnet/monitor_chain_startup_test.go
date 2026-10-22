// Chain startup controls retain actual checkpoint bytes and custody metadata.
// Faults withhold kernel observations; public commands still own every read,
// lock, Rpc request, publication and joined cancellation.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The original retained fixture is independently encoded before enrollment.
// A malformed fixture deliberately enrolls those exact bad document bytes.
type monitorChainStartupFixture struct {
	storage *durablefixture.Fixture
	path    string
	raw     []byte
	info    os.FileInfo
	stamp   time.Time
}

// The normal sample is unchanged, so a successful retry cannot hide a rewrite.
func newMonitorChainStartupFixture(t *testing.T, malformed bool) *monitorChainStartupFixture {
	t.Helper()
	root := mainnetPrivateTestDir(t)
	self := &monitorChainStartupFixture{path: filepath.Join(root, "chain.json"), stamp: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	store, err := openMonitorCheckpoint(self.path, monitorTestExpectation())
	if err != nil {
		t.Fatal(err)
	}
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: self.stamp, lastSuccessAt: self.stamp}
	if err := errors.Join(store.save(state), store.close()); err != nil {
		t.Fatal(err)
	}
	self.raw, err = os.ReadFile(self.path)
	if err != nil {
		t.Fatal(err)
	}
	if malformed {
		self.raw = bytes.Replace(self.raw, []byte(testFinalizedHash), []byte(testGenesisHash), 1)
		if err := os.WriteFile(self.path, self.raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	self.info, err = os.Stat(self.path)
	if err != nil {
		t.Fatal(err)
	}
	provisionMonitorTestCustody(t, self.path)
	self.storage = durablefixture.New(t, t.Context(), root)
	return self
}

// Checking bytes and the same inode distinguishes recovery from replacement.
func (self *monitorChainStartupFixture) unchanged(t *testing.T) {
	t.Helper()
	if err := self.observationError(); err != nil {
		t.Fatal(err)
	}
}

// Worker callbacks return observations to their joined test owner; they cannot
// call Fatal because Goexit would leave the synchronous supervisor waiting.
func (self *monitorChainStartupFixture) observationError() error {
	raw, err := os.ReadFile(self.path)
	if err != nil || !bytes.Equal(raw, self.raw) {
		return errors.Join(errors.New("startup changed original checkpoint bytes"), err)
	}
	info, err := os.Stat(self.path)
	if err != nil || !os.SameFile(self.info, info) {
		return errors.Join(errors.New("startup replaced original checkpoint inode"), err)
	}
	return nil
}

// Public parsing retains the exact prepared paths and a normal finite budget.
func (self *monitorChainStartupFixture) arguments(url string) []string {
	return []string{"monitor", "--rpc", url, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", self.path, "--retry-window", "300s", "--interval", "1h"}
}

// A second real flock cannot acquire the still-admitted checkpoint owner.
func monitorChainStartupRetainedLock(t *testing.T, path string, lock *os.File) {
	t.Helper()
	if err := monitorChainStartupLockError(path, lock); err != nil {
		t.Fatal(err)
	}
}

// This real-lock observer returns its error across a callback boundary.
func monitorChainStartupLockError(path string, lock *os.File) error {
	if lock == nil {
		return errors.New("startup omitted its retained lock")
	}
	if _, err := lock.Stat(); err != nil {
		return errors.Join(errors.New("retry closed the original lock"), err)
	}
	other, err := os.OpenFile(path+".lock", os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	conflict := syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	closeErr := other.Close()
	if !errors.Is(conflict, syscall.EAGAIN) || closeErr != nil {
		return errors.Join(errors.New("retry released original exclusive custody"), conflict, closeErr)
	}
	return nil
}

// A transient physical fact failure after admission recovers in the same owner.
// One actual sample authenticates both its opening and closing finalized head.
func TestMonitorChainStartupPublicReadFailureRecoversSameCheckpoint(t *testing.T) {
	f := newMonitorChainStartupFixture(t, false)
	server, reads := testRpcServerWithEvm(t, "0x3c4", "")
	parent, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	var failing atomic.Bool
	host := &compositionObservationHost{Host: f.storage.Host, observe: func(*os.File) error {
		if failing.Load() {
			return syscall.EIO
		}
		return nil
	}}
	ctx := durablepath.WithHost(parent, host)
	var lock *os.File
	var callbackErr error
	fail := func(err error) error {
		callbackErr = errors.Join(callbackErr, err)
		cancel()
		return err
	}
	waits, events := 0, 0
	hooks := monitorServiceHooks{
		afterCheckpointOpen: func(_ context.Context, role string, file *os.File) {
			if role != "chain" {
				fail(fmt.Errorf("unexpected chain owner %q", role))
				return
			}
			lock = file
			failing.Store(true)
		},
		rpcWait: func(waitCtx context.Context, role string, delay time.Duration) error {
			waits++
			if role != "chain" || waits != 1 || events != 0 || delay != time.Second || waitCtx.Err() != nil || reads("chain_getFinalizedHead") != 0 {
				return fail(fmt.Errorf("startup retry escaped its unpublished read boundary: %s %d %d %s %v", role, waits, events, delay, waitCtx.Err()))
			}
			if err := errors.Join(f.observationError(), monitorChainStartupLockError(f.path, lock)); err != nil {
				return fail(err)
			}
			failing.Store(false)
			return nil
		},
		afterEvent: func(_ context.Context, role string) {
			if role == "chain" {
				events++
				cancel()
			}
		},
	}
	var diagnostic bytes.Buffer
	exit := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, &diagnostic, func() time.Time { return f.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if exit != 0 || waits != 1 || events != 1 || reads("chain_getFinalizedHead") != 2 {
		t.Fatal("actual chain startup failed to recover", exit, waits, events, reads("chain_getFinalizedHead"), diagnostic.String())
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("returned monitor retained a live checkpoint owner", err)
	}
	f.unchanged(t)
}

// Earlier EIO evidence cannot turn a later checksum refusal into another retry.
func TestMonitorChainStartupPublicCorruptionDominatesEarlierReadFailure(t *testing.T) {
	f := newMonitorChainStartupFixture(t, true)
	server, reads := testRpcServerWithEvm(t, "0x3c4", "")
	var failing atomic.Bool
	host := &compositionObservationHost{Host: f.storage.Host, observe: func(*os.File) error {
		if failing.Load() {
			return syscall.EIO
		}
		return nil
	}}
	waits := 0
	var lock *os.File
	hooks := monitorServiceHooks{
		afterCheckpointOpen: func(_ context.Context, _ string, file *os.File) { lock = file; failing.Store(true) },
		rpcWait:             func(context.Context, string, time.Duration) error { waits++; failing.Store(false); return nil },
	}
	var diagnostic bytes.Buffer
	exit := runMainWithMonitorHooks(durablepath.WithHost(f.storage.Context, host), f.arguments(server.URL), io.Discard, &diagnostic, func() time.Time { return f.stamp }, hooks)
	if exit != 3 || waits != 1 || reads("chain_getFinalizedHead") != 0 {
		t.Fatal("retained corruption was retried or reached Rpc", exit, waits, diagnostic.String())
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("refused checkpoint owner was not joined", err)
	}
	f.unchanged(t)
}

// An observed replacement inode is terminal even when its bytes are identical.
func TestMonitorChainStartupPublicReplacedInodeRefusesFreshState(t *testing.T) {
	monitorChainStartupLostCustody(t, true)
}

// A missing retained checkpoint cannot become a newly empty checkpoint.
func TestMonitorChainStartupPublicMissingRetainedCheckpointRefusesFreshState(t *testing.T) {
	monitorChainStartupLostCustody(t, false)
}

// Both controls remove the admitted named inode after the real opener returns.
func monitorChainStartupLostCustody(t *testing.T, replace bool) {
	t.Helper()
	f := newMonitorChainStartupFixture(t, false)
	server, reads := testRpcServerWithEvm(t, "0x3c4", "")
	retained := f.path + ".retained"
	ctx, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	var callbackErr error
	waits := 0
	hooks := monitorServiceHooks{
		afterCheckpointOpen: func(context.Context, string, *os.File) {
			if err := os.Rename(f.path, retained); err != nil {
				callbackErr = err
				cancel()
				return
			}
			if replace {
				if err := os.WriteFile(f.path, f.raw, 0600); err != nil {
					callbackErr = err
					cancel()
				}
			}
		},
		rpcWait: func(context.Context, string, time.Duration) error {
			waits++
			return errors.New("invalid custody must not retry")
		},
	}
	var diagnostic bytes.Buffer
	exit := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, &diagnostic, func() time.Time { return f.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if exit != 3 || waits != 0 || reads("chain_getFinalizedHead") != 0 {
		t.Fatal("lost retained custody became a fresh observation", exit, waits, diagnostic.String())
	}
	raw, err := os.ReadFile(retained)
	info, statErr := os.Stat(retained)
	if err != nil || statErr != nil || !bytes.Equal(raw, f.raw) || !os.SameFile(f.info, info) {
		t.Fatal("refusal changed the original retained checkpoint", err, statErr)
	}
	if !replace {
		if _, err := os.Lstat(f.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing retained state was replaced", err)
		}
	}
}

// Advancing only the owned wait clock proves the normal 300-second bound without
// sleeping or making a scheduler delay stand in for a completed observation.
func TestMonitorChainStartupReadBudgetRetainsOriginalFailure(t *testing.T) {
	f := newMonitorChainStartupFixture(t, false)
	failing := false
	host := &compositionObservationHost{Host: f.storage.Host, observe: func(*os.File) error {
		if failing {
			return syscall.EIO
		}
		return nil
	}}
	store, err := openMonitorCheckpoint(f.path, monitorTestExpectation(), durablepath.WithHost(f.storage.Context, host))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	failing = true
	now, waited, waits := f.stamp, time.Duration(0), 0
	hooks := monitorServiceHooks{rpcWait: func(waitCtx context.Context, role string, delay time.Duration) error {
		waits++
		if role != "chain" || delay <= 0 || delay > 10*time.Second || waits > 64 || waitCtx.Err() != nil {
			return fmt.Errorf("read retry escaped its finite window: %s %s %d %v", role, delay, waits, waitCtx.Err())
		}
		waited += delay
		now = now.Add(delay)
		return nil
	}}
	state, err := loadMonitorChainCheckpoint(f.storage.Context, store, 300*time.Second, io.Discard, io.Discard, func() time.Time { return now }, hooks)
	if state != nil || !errors.Is(err, syscall.EIO) || !errors.Is(err, context.DeadlineExceeded) || !monitorStartupPending(err) || waited != 300*time.Second || waits < 2 || len(err.Error()) > 8192 {
		t.Fatal("read exhaustion lost its finite original cause", state, err, waited, waits)
	}
	monitorChainStartupRetainedLock(t, f.path, store.lock)
	f.unchanged(t)
}

// Cancellation returns both the original unavailable observation and the caller
// cause. Synchronous ownership remains available for the caller's joined close.
func TestMonitorChainStartupCancellationRetainsOriginalFailure(t *testing.T) {
	f := newMonitorChainStartupFixture(t, false)
	parent, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	failing := false
	host := &compositionObservationHost{Host: f.storage.Host, observe: func(*os.File) error {
		if failing {
			return syscall.EIO
		}
		return nil
	}}
	ctx := durablepath.WithHost(parent, host)
	store, err := openMonitorCheckpoint(f.path, monitorTestExpectation(), ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	failing = true
	waits := 0
	hooks := monitorServiceHooks{rpcWait: func(waitCtx context.Context, _ string, _ time.Duration) error {
		waits++
		cancel()
		return waitCtx.Err()
	}}
	state, err := loadMonitorChainCheckpoint(ctx, store, 300*time.Second, io.Discard, io.Discard, func() time.Time { return f.stamp }, hooks)
	if state != nil || !errors.Is(err, syscall.EIO) || !errors.Is(err, context.Canceled) || errors.Is(err, durablevolume.ErrIdentity) || waits != 1 {
		t.Fatal("cancellation erased or recast the original failure", state, err, waits)
	}
	lock := store.lock
	monitorChainStartupRetainedLock(t, f.path, lock)
	if err := store.close(); err != nil {
		t.Fatal("canceled owner did not join", err)
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("cancellation leaked a retained owner", err)
	}
	f.unchanged(t)
}

// The public caller joins a canceled unavailable read without retrying after
// shutdown, reaching Rpc, or changing the original retained checkpoint.
func TestMonitorChainStartupPublicCancellationJoinsReadOwner(t *testing.T) {
	f := newMonitorChainStartupFixture(t, false)
	parent, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	server, reads := testRpcServerWithEvm(t, "0x3c4", "")
	var failing atomic.Bool
	host := &compositionObservationHost{Host: f.storage.Host, observe: func(*os.File) error {
		if failing.Load() {
			return syscall.EIO
		}
		return nil
	}}
	var lock *os.File
	var callbackErr error
	waits := 0
	hooks := monitorServiceHooks{
		afterCheckpointOpen: func(_ context.Context, _ string, file *os.File) { lock = file; failing.Store(true) },
		rpcWait: func(waitCtx context.Context, _ string, _ time.Duration) error {
			waits++
			callbackErr = monitorChainStartupLockError(f.path, lock)
			cancel()
			return errors.Join(callbackErr, waitCtx.Err())
		},
	}
	var diagnostic bytes.Buffer
	exit := runMainWithMonitorHooks(durablepath.WithHost(parent, host), f.arguments(server.URL), io.Discard, &diagnostic, func() time.Time { return f.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if exit != 0 || waits != 1 || reads("chain_getFinalizedHead") != 0 {
		t.Fatal("canceled startup did not join its original read", exit, waits, diagnostic.String())
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("canceled command left a live physical owner", err)
	}
	f.unchanged(t)
}

// A ready declaration whose original head says absent admits the first actual
// sample. This is independent of the missing-retained refusal above.
func TestMonitorChainStartupPublicPreparedAbsentCheckpointSamples(t *testing.T) {
	root := mainnetPrivateTestDir(t)
	path := filepath.Join(root, "chain.json")
	provisionMonitorTestCustody(t, path)
	storage := durablefixture.New(t, t.Context(), root)
	ctx, cancel := context.WithCancel(storage.Context)
	defer cancel()
	server, reads := testRpcServerWithEvm(t, "0x3c4", "")
	f := &monitorChainStartupFixture{path: path, stamp: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	waits, events := 0, 0
	hooks := monitorServiceHooks{
		rpcWait: func(context.Context, string, time.Duration) error {
			waits++
			return errors.New("fresh checkpoint must admit directly")
		},
		afterEvent: func(context.Context, string) { events++; cancel() },
	}
	var diagnostic bytes.Buffer
	exit := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, &diagnostic, func() time.Time { return f.stamp }, hooks)
	if exit != 0 || waits != 0 || events != 1 || reads("chain_getFinalizedHead") != 2 {
		t.Fatal("prepared new checkpoint did not admit its actual sample", exit, waits, events, reads("chain_getFinalizedHead"), diagnostic.String())
	}
	store, err := openMonitorCheckpoint(path, monitorTestExpectation(), storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	state, loadErr := store.load()
	closeErr := store.close()
	if loadErr != nil || closeErr != nil || state == nil || state.lastNumber != 100 || state.lastHash != testFinalizedHash {
		t.Fatal("first sample omitted original retained state", state, loadErr, closeErr)
	}
}

// The public supervisor must keep validator samples flowing while chain startup
// waits. Releasing the exact barrier admits the chain's real first Rpc request.
func TestMonitorChainStartupPublicRetryPreservesIndependentPeer(t *testing.T) {
	f := newMonitorServicesFixture(t, "healthy")
	url, rpcEntered, rpcLeft := monitorServicesBlockedChain(t)
	entered, resume := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int32
	ctx := context.WithValue(t.Context(), monitorStartupObservationKey{}, func(_ context.Context, role string) error {
		if role == "chain" && attempts.Add(1) == 1 {
			return syscall.EIO
		}
		return nil
	})
	run := f.startWithContext(t, ctx, url, monitorServiceHooks{rpcWait: func(waitCtx context.Context, role string, _ time.Duration) error {
		if role != "chain" {
			return errors.New("unexpected independent retry owner")
		}
		close(entered)
		select {
		case <-resume:
			return nil
		case <-waitCtx.Done():
			return waitCtx.Err()
		}
	}})
	select {
	case <-entered:
	case <-rpcEntered:
		t.Fatal("chain reached Rpc before completing startup observation")
	case <-run.done:
		t.Fatal("supervisor ended before checkpoint retry", run.exit, run.stderr.String())
	case <-t.Context().Done():
		t.Fatal("canceled before checkpoint retry barrier")
	}
	for index := 0; index < 2; index++ {
		if index != 0 {
			run.again(t, "healthy")
		}
		event := monitorStartupNext(t, run)
		if event.Role != "healthy" || event.Publication != "published" || event.State == nil {
			t.Fatal("chain startup suppressed an independent actual sample", event)
		}
	}
	close(resume)
	select {
	case <-rpcEntered:
	case <-run.done:
		t.Fatal("recovered checkpoint did not reach actual Rpc", run.exit, run.stderr.String())
	case <-t.Context().Done():
		t.Fatal("canceled before recovered Rpc read")
	}
	run.cancel()
	<-run.done
	<-rpcLeft
	if run.exit != 0 || attempts.Load() != 2 || !strings.Contains(run.stderr.String(), syscall.EIO.Error()) {
		t.Fatal("chain retry failed to join or lost its observed cause", run.exit, attempts.Load(), run.stderr.String())
	}
}

// Omitting the monitor flag selects the normal five-minute read window at the
// actual CLI parser and client constructor, before a real successful Rpc read.
func TestMonitorChainStartupPublicDefaultReadBudgetIsThreeHundredSeconds(t *testing.T) {
	monitorChainStartupParsedBudget(t, "monitor", "", 300*time.Second)
}

// The explicit supported one-minute override is retained without normalization.
func TestMonitorChainStartupPublicExplicitReadBudgetIsPreserved(t *testing.T) {
	monitorChainStartupParsedBudget(t, "monitor", "60s", 60*time.Second)
}

// Inspect remains a separate finite command with its established default.
func TestMonitorChainStartupInspectDefaultReadBudgetIsUnchanged(t *testing.T) {
	monitorChainStartupParsedBudget(t, "inspect", "", 60*time.Second)
}

// The private observer receives immutable constructed settings only; it cannot
// supply a client, skip parsing, shorten a read, or manufacture its response.
func monitorChainStartupParsedBudget(t *testing.T, command, configured string, expected time.Duration) {
	t.Helper()
	f := newMonitorChainStartupFixture(t, false)
	server, reads := testRpcServerWithEvm(t, "0x3c4", "")
	ctx, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	args := []string{command, "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964"}
	if command == "monitor" {
		args = append(args, "--checkpoint", f.path)
	}
	if configured != "" {
		args = append(args, "--retry-window", configured)
	}
	constructed, events := 0, 0
	var callbackErr error
	hooks := monitorServiceHooks{
		afterRpcClient: func(observed string, budget time.Duration) {
			constructed++
			if observed != command || budget != expected {
				callbackErr = fmt.Errorf("actual parsed Rpc read budget differs: %s %s, expected %s", observed, budget, expected)
				cancel()
			}
		},
		afterEvent: func(context.Context, string) { events++; cancel() },
	}
	var diagnostic bytes.Buffer
	exit := runMainWithMonitorHooks(ctx, args, io.Discard, &diagnostic, func() time.Time { return f.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	wantEvents := 0
	if command == "monitor" {
		wantEvents = 1
	}
	if exit != 0 || constructed != 1 || events != wantEvents || reads("chain_getFinalizedHead") != 2 {
		t.Fatal("parsed read budget did not reach actual command observation", exit, constructed, events, reads("chain_getFinalizedHead"), diagnostic.String())
	}
	f.unchanged(t)
}
