// Public root startup controls use original prepared snapshots, real file
// descriptors and the existing local Rpc fixture. Faults only withhold reads.
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

// The finite original document is encoded before enrollment, independently of
// the command under test. Even malformed bytes have an original owned head.
type rootMonitorStartupFixture struct {
	checkpoint *monitorChainStartupFixture
	rpc        *rootRpcFixture
	policyPath string
	metrics    string
}

// Independent root and metrics namespaces share only a synthetic physical disk.
func newRootMonitorStartupFixture(t *testing.T, mode string) *rootMonitorStartupFixture {
	t.Helper()
	_, rpc := newRootFixture(t)
	outer := mainnetPrivateTestDir(t)
	root, metrics := filepath.Join(outer, "checkpoint"), filepath.Join(outer, "metrics")
	for _, path := range []string{root, metrics} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := &monitorChainStartupFixture{path: filepath.Join(root, "root.json"), stamp: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	if mode != "absent" {
		expected := identityExpectation{NativeChain: rpc.policy.NativeChain, GenesisHash: rpc.policy.GenesisHash, EvmChainId: rpc.policy.EvmChainId}
		store, err := openMonitorCheckpoint(checkpoint.path, expected)
		if err != nil {
			t.Fatal(err)
		}
		state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: checkpoint.stamp, lastSuccessAt: checkpoint.stamp}
		if err := errors.Join(store.save(state), store.close()); err != nil {
			t.Fatal(err)
		}
		checkpoint.raw, err = os.ReadFile(checkpoint.path)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "corrupt" {
			checkpoint.raw = bytes.Replace(checkpoint.raw, []byte(testFinalizedHash), []byte(testGenesisHash), 1)
			if err := os.WriteFile(checkpoint.path, checkpoint.raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		checkpoint.info, err = os.Stat(checkpoint.path)
		if err != nil {
			t.Fatal(err)
		}
	}
	provisionMonitorTestCustody(t, checkpoint.path)
	checkpoint.storage = durablefixture.New(t, t.Context(), root, metrics)
	return &rootMonitorStartupFixture{checkpoint: checkpoint, rpc: rpc, policyPath: rootTestPolicyFile(t, rpc.policy), metrics: filepath.Join(metrics, "root.prom")}
}

// Explicit policy and original namespace reach the same public CLI as a unit.
func (self *rootMonitorStartupFixture) arguments(url string) []string {
	return []string{"root-monitor", "--rpc", url, "--policy", self.policyPath, "--checkpoint", self.checkpoint.path, "--samples", "1"}
}

// A real unavailable filesystem observation happens before the old opener's
// refusal. Removing retries therefore reproduces exit3 with the fault observed.
func TestRootMonitorStartupPublicAdmissionReadRecovers(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	failing, faults, waits := true, 0, 0
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(file *os.File) error {
		if _, err := file.Stat(); err != nil {
			return err
		}
		if failing {
			faults++
			return syscall.EIO
		}
		return nil
	}}
	hooks := monitorServiceHooks{rpcWait: func(ctx context.Context, role string, delay time.Duration) error {
		waits++
		if role != "root" || delay != time.Second || faults == 0 || f.rpc.count("chain_getFinalizedHead") != 0 || ctx.Err() != nil {
			return fmt.Errorf("admission retry escaped original observations: %s %d %d %s", role, waits, faults, delay)
		}
		if err := f.checkpoint.observationError(); err != nil {
			return err
		}
		failing = false
		return nil
	}}
	code := runMainWithMonitorHooks(durablepath.WithHost(f.checkpoint.storage.Context, host), f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if code != 0 || waits != 1 || faults == 0 || f.rpc.count("chain_getFinalizedHead") != 4 {
		t.Fatal("actual root admission did not recover", code, waits, faults, f.rpc.count("chain_getFinalizedHead"))
	}
	f.checkpoint.unchanged(t)
}

// Once opened, the exact lock remains held throughout a failed head read.
func TestRootMonitorStartupPublicCheckpointReadRetainsOwner(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	var failing atomic.Bool
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(*os.File) error {
		if failing.Load() {
			return syscall.EIO
		}
		return nil
	}}
	var lock *os.File
	ctx, cancel := context.WithCancel(f.checkpoint.storage.Context)
	defer cancel()
	var callbackErr error
	waits := 0
	hooks := monitorServiceHooks{
		afterCheckpointOpen: func(_ context.Context, role string, file *os.File) {
			if role != "root" {
				callbackErr = fmt.Errorf("wrong owner %q", role)
				cancel()
				return
			}
			lock = file
			failing.Store(true)
		},
		rpcWait: func(context.Context, string, time.Duration) error {
			waits++
			if err := errors.Join(monitorChainStartupLockError(f.checkpoint.path, lock), f.checkpoint.observationError()); err != nil {
				return err
			}
			failing.Store(false)
			return nil
		},
	}
	code := runMainWithMonitorHooks(durablepath.WithHost(ctx, host), f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if code != 0 || waits != 1 || f.rpc.count("chain_getFinalizedHead") != 4 {
		t.Fatal("root head did not recover under its retained owner", code, waits)
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("returned root left its owner open", err)
	}
	f.checkpoint.unchanged(t)
}

// Completed checksum evidence dominates the earlier EIO instead of inheriting
// its retry classification. No Rpc observation occurs on this refused head.
func TestRootMonitorStartupPublicCorruptionDominatesEarlierRead(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "corrupt")
	server := rootFixtureServer(t, f.rpc)
	failing, waits := false, 0
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(*os.File) error {
		if failing {
			return syscall.EIO
		}
		return nil
	}}
	hooks := monitorServiceHooks{afterCheckpointOpen: func(context.Context, string, *os.File) { failing = true }, rpcWait: func(context.Context, string, time.Duration) error {
		waits++
		failing = false
		return nil
	}}
	code := runMainWithMonitorHooks(durablepath.WithHost(f.checkpoint.storage.Context, host), f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if code != 3 || waits != 1 || f.rpc.count("chain_getFinalizedHead") != 0 {
		t.Fatal("corrupt root custody was retried or observed", code, waits)
	}
	f.checkpoint.unchanged(t)
}

// A replacement with identical bytes still loses original inode custody.
func TestRootMonitorStartupPublicReplacedCheckpointRefusesFreshState(t *testing.T) {
	rootMonitorStartupLostCheckpoint(t, true)
}

// A missing retained name cannot become a prepared empty checkpoint.
func TestRootMonitorStartupPublicMissingCheckpointRefusesFreshState(t *testing.T) {
	rootMonitorStartupLostCheckpoint(t, false)
}

// Mutation is physical and follows the actual admitted owner, not a fake load.
func rootMonitorStartupLostCheckpoint(t *testing.T, replace bool) {
	t.Helper()
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	retained, waits := f.checkpoint.path+".original", 0
	ctx, cancel := context.WithCancel(f.checkpoint.storage.Context)
	defer cancel()
	var callbackErr error
	hooks := monitorServiceHooks{afterCheckpointOpen: func(context.Context, string, *os.File) {
		if err := os.Rename(f.checkpoint.path, retained); err != nil {
			callbackErr = err
			cancel()
			return
		}
		if replace {
			if err := os.WriteFile(f.checkpoint.path, f.checkpoint.raw, 0600); err != nil {
				callbackErr = err
				cancel()
			}
		}
	}, rpcWait: func(context.Context, string, time.Duration) error {
		waits++
		return errors.New("lost custody cannot retry")
	}}
	code := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if code != 3 || waits != 0 || f.rpc.count("chain_getFinalizedHead") != 0 {
		t.Fatal("lost root head was admitted", code, waits)
	}
	raw, err := os.ReadFile(retained)
	info, statErr := os.Stat(retained)
	if err != nil || statErr != nil || !bytes.Equal(raw, f.checkpoint.raw) || !os.SameFile(info, f.checkpoint.info) {
		t.Fatal("refusal mutated original custody", err, statErr)
	}
	if !replace {
		if _, err := os.Stat(f.checkpoint.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing retained head was recreated", err)
		}
	}
}

// A real prepared absent head is the permitted fresh path, independently of
// the retained-loss controls above. Its first actual sample is persisted.
func TestRootMonitorStartupPublicPreparedAbsentCheckpointSamples(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "absent")
	server := rootFixtureServer(t, f.rpc)
	code := runMainWithMonitorHooks(f.checkpoint.storage.Context, f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, monitorServiceHooks{})
	if code != 0 || f.rpc.count("chain_getFinalizedHead") != 4 {
		t.Fatal("prepared original absence did not sample", code)
	}
	raw, err := os.ReadFile(f.checkpoint.path)
	if err != nil || !bytes.Contains(raw, []byte(testFinalizedHash)) {
		t.Fatal("fresh sample was not durably retained", err)
	}
}

// Policy retries keep one real descriptor and its independently encoded bytes.
func TestRootMonitorStartupPublicPolicyReadRecoversSameDescriptor(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	reads, waits, closed := 0, 0, 0
	var original *os.File
	ctx := context.WithValue(f.checkpoint.storage.Context, rootMonitorPolicyObservationKey{}, func(_ context.Context, file *os.File) error {
		reads++
		if original == nil {
			original = file
		} else if original != file {
			return errors.New("policy retry replaced the original descriptor")
		}
		if reads == 1 {
			return syscall.EIO
		}
		return nil
	})
	ctx = context.WithValue(ctx, rootMonitorPolicyCloseObservationKey{}, func(file *os.File) error {
		closed++
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			return errors.Join(errors.New("policy remained open"), err)
		}
		return nil
	})
	hooks := monitorServiceHooks{rpcWait: func(context.Context, string, time.Duration) error { waits++; return nil }}
	code := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if code != 0 || reads != 2 || waits != 1 || closed != 1 || f.rpc.count("chain_getFinalizedHead") != 4 {
		t.Fatal("original policy did not recover", code, reads, waits, closed)
	}
	f.checkpoint.unchanged(t)
}

// A later completed decode cannot inherit a previous policy read's soft cause.
func TestRootMonitorStartupPublicInvalidPolicyDominatesEarlierRead(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	if err := os.WriteFile(f.policyPath, []byte(`{"schema":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	reads, waits := 0, 0
	ctx := context.WithValue(f.checkpoint.storage.Context, rootMonitorPolicyObservationKey{}, func(context.Context, *os.File) error {
		reads++
		if reads == 1 {
			return syscall.EIO
		}
		return nil
	})
	hooks := monitorServiceHooks{rpcWait: func(context.Context, string, time.Duration) error { waits++; return nil }}
	code := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if code != 2 || waits != 1 || reads != 2 || f.rpc.count("chain_getFinalizedHead") != 0 {
		t.Fatal("invalid policy inherited retry or read authority", code, waits, reads)
	}
	f.checkpoint.unchanged(t)
}

// One deadline covers both policy recovery and the subsequently retained head.
func TestRootMonitorStartupPublicDefaultBudgetIsSharedAcrossReads(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	// The actual diagnostic exporter reads the same clock concurrently.
	var elapsed atomic.Int64
	now := func() time.Time { return f.checkpoint.stamp.Add(time.Duration(elapsed.Load())) }
	policyReads, waits, failing := 0, 0, false
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(*os.File) error {
		if failing {
			return syscall.EIO
		}
		return nil
	}}
	ctx := context.WithValue(durablepath.WithHost(f.checkpoint.storage.Context, host), rootMonitorPolicyObservationKey{}, func(context.Context, *os.File) error {
		policyReads++
		if policyReads == 1 {
			return syscall.EIO
		}
		return nil
	})
	var lock *os.File
	hooks := monitorServiceHooks{afterCheckpointOpen: func(_ context.Context, _ string, file *os.File) { lock = file; failing = true }, rpcWait: func(_ context.Context, _ string, delay time.Duration) error {
		waits++
		if waits == 1 {
			elapsed.Add(int64(200 * time.Second))
		} else {
			if err := monitorChainStartupLockError(f.checkpoint.path, lock); err != nil {
				return err
			}
			elapsed.Add(int64(delay))
		}
		return nil
	}}
	code := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, io.Discard, now, hooks)
	if code != 1 || time.Duration(elapsed.Load()) != 300*time.Second || policyReads != 2 || waits < 2 || f.rpc.count("chain_getFinalizedHead") != 0 {
		t.Fatal("root startup reset or lost its original default window", code, time.Duration(elapsed.Load()), policyReads, waits)
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("exhausted startup retained its process owner", err)
	}
	f.checkpoint.unchanged(t)
}

// Exhaustion retains both the first actual EIO and the finite deadline cause.
func TestRootMonitorStartupPolicyBudgetRetainsOriginalCause(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	stamp, waited := f.checkpoint.stamp, time.Duration(0)
	ctx := context.WithValue(f.checkpoint.storage.Context, rootMonitorPolicyObservationKey{}, func(context.Context, *os.File) error { return syscall.EIO })
	hooks := monitorServiceHooks{rpcWait: func(_ context.Context, _ string, delay time.Duration) error {
		waited += delay
		stamp = stamp.Add(delay)
		return nil
	}}
	_, hash, err := readRootMonitorStartupPolicy(ctx, f.policyPath, stamp.Add(300*time.Second), func() time.Time { return stamp }, hooks, io.Discard)
	if hash != "" || !errors.Is(err, syscall.EIO) || !errors.Is(err, context.DeadlineExceeded) || !rootMonitorStartupPending(err) || waited != 300*time.Second || len(err.Error()) > 8192 {
		t.Fatal("policy exhaustion lost its original cause/bound", hash, err, waited)
	}
}

// A stopped caller joins the original read owner and keeps its first failure.
func TestRootMonitorStartupPolicyCancellationRetainsOriginalCause(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	parent, cancel := context.WithCancel(f.checkpoint.storage.Context)
	defer cancel()
	ctx := context.WithValue(parent, rootMonitorPolicyObservationKey{}, func(context.Context, *os.File) error { return syscall.EIO })
	var closed bool
	ctx = context.WithValue(ctx, rootMonitorPolicyCloseObservationKey{}, func(file *os.File) error {
		_, err := file.Stat()
		closed = errors.Is(err, os.ErrClosed)
		return nil
	})
	hooks := monitorServiceHooks{rpcWait: func(ctx context.Context, _ string, _ time.Duration) error { cancel(); return ctx.Err() }}
	_, _, err := readRootMonitorStartupPolicy(ctx, f.policyPath, f.checkpoint.stamp.Add(300*time.Second), func() time.Time { return f.checkpoint.stamp }, hooks, io.Discard)
	if !errors.Is(err, syscall.EIO) || !errors.Is(err, context.Canceled) || !closed || rootMonitorStartupExit(parent, err, 3) != 0 {
		t.Fatal("cancellation lost its cause or read owner", err, closed)
	}
}

// Cleanup ambiguity is not a reason to start another owner, even when the
// policy read itself completed and the reported cleanup errno is transient.
func TestRootMonitorStartupPublicPolicyCleanupRefusesRetry(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	closed := false
	ctx := context.WithValue(f.checkpoint.storage.Context, rootMonitorPolicyCloseObservationKey{}, func(file *os.File) error {
		_, err := file.Stat()
		closed = errors.Is(err, os.ErrClosed)
		return syscall.EIO
	})
	code := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, monitorServiceHooks{})
	if code != 3 || !closed || f.rpc.count("chain_getFinalizedHead") != 0 {
		t.Fatal("policy cleanup failure granted another owner", code, closed)
	}
	f.checkpoint.unchanged(t)
}

// A named replacement after an unavailable read cannot replace the already
// retained policy descriptor, even when the replacement bytes are identical.
func TestRootMonitorStartupPublicPolicyReplacementRefusesRetry(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	raw, err := os.ReadFile(f.policyPath)
	if err != nil {
		t.Fatal(err)
	}
	var original *os.File
	reads, waits := 0, 0
	ctx := context.WithValue(f.checkpoint.storage.Context, rootMonitorPolicyObservationKey{}, func(_ context.Context, file *os.File) error {
		reads++
		original = file
		return syscall.EIO
	})
	hooks := monitorServiceHooks{rpcWait: func(context.Context, string, time.Duration) error {
		waits++
		if waits != 1 {
			return errors.New("replaced policy was retried")
		}
		if err := os.Rename(f.policyPath, f.policyPath+".original"); err != nil {
			return err
		}
		return os.WriteFile(f.policyPath, raw, 0600)
	}}
	code := runMainWithMonitorHooks(ctx, f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if code != 2 || reads != 1 || waits != 1 || original == nil || f.rpc.count("chain_getFinalizedHead") != 0 {
		t.Fatal("replacement policy inherited original read authority", code, reads, waits)
	}
	if _, err := original.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("refused original policy descriptor remained open", err)
	}
	f.checkpoint.unchanged(t)
}

// Parent cancellation joins the actual retained checkpoint and never reaches
// Rpc or turns unavailable retained bytes into a fresh observation.
func TestRootMonitorStartupPublicCancellationJoinsCheckpointOwner(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	parent, cancel := context.WithCancel(f.checkpoint.storage.Context)
	defer cancel()
	failing, waits := false, 0
	var lock *os.File
	var callbackErr error
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(*os.File) error {
		if failing {
			return syscall.EIO
		}
		return nil
	}}
	hooks := monitorServiceHooks{afterCheckpointOpen: func(_ context.Context, _ string, file *os.File) { lock = file; failing = true }, rpcWait: func(ctx context.Context, _ string, _ time.Duration) error {
		waits++
		callbackErr = errors.Join(monitorChainStartupLockError(f.checkpoint.path, lock), f.checkpoint.observationError())
		cancel()
		return errors.Join(callbackErr, ctx.Err())
	}}
	code := runMainWithMonitorHooks(durablepath.WithHost(parent, host), f.arguments(server.URL), io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if code != 0 || waits != 1 || f.rpc.count("chain_getFinalizedHead") != 0 || lock == nil {
		t.Fatal("canceled root read did not join its original owner", code, waits)
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("canceled root retained its owner", err)
	}
	f.checkpoint.unchanged(t)
}

// A temporary publisher admission error cannot suppress telemetry for all
// remaining samples. The real late-admitted file owner closes exactly once.
func TestRootMonitorStartupPublicMetricsAdmissionRecoversIndependently(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	failing, faults, samples, metricCloses := true, 0, 0, 0
	var callbackErr error
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(file *os.File) error {
		if file.Name() == filepath.Dir(f.metrics) && failing {
			if _, err := file.Stat(); err != nil {
				return err
			}
			faults++
			return syscall.EIO
		}
		return nil
	}}
	hooks := monitorServiceHooks{afterEvent: func(context.Context, string) {
		samples++
		if samples == 1 {
			if faults == 0 {
				callbackErr = errors.New("metrics fault never reached an actual admission")
			}
			failing = false
		}
	}, afterClose: func(_ string, kind string, file *os.File) error {
		if kind == "metrics" {
			metricCloses++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.Join(errors.New("late publisher remained open"), err)
			}
		}
		return nil
	}, wait: func(context.Context, string, time.Duration) bool { return true }}
	args := append(f.arguments(server.URL), "--samples", "2", "--metrics-file", f.metrics, "--metrics-role", "root-a")
	code := runMainWithMonitorHooks(durablepath.WithHost(f.checkpoint.storage.Context, host), args, io.Discard, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if code != 0 || samples != 2 || metricCloses != 1 || faults == 0 || f.rpc.count("chain_getFinalizedHead") != 8 {
		t.Fatal("optional publisher did not recover beside actual root reads", code, samples, metricCloses, faults)
	}
	raw, err := os.ReadFile(f.metrics)
	if err != nil || !strings.Contains(string(raw), `sn_mainnet_root_monitor_current_observation{role="root-a"} 1`) {
		t.Fatal("late admitted metrics did not publish actual current observation", err, string(raw))
	}
	f.checkpoint.unchanged(t)
}

// An unknown joined refusal and descriptor loss never become a soft read.
func TestRootMonitorStartupMixedHardCausesRemainTerminal(t *testing.T) {
	for _, hard := range []error{errors.New("original policy refusal"), durablevolume.ErrIdentity, syscall.EBADF, os.ErrClosed, &monitorAdmissionRefusalError{cause: syscall.EIO}, &monitorAdmissionCleanupError{cause: context.Canceled}} {
		if rootMonitorStartupPending(errors.Join(syscall.EIO, hard)) {
			t.Fatalf("hard cause borrowed an EIO retry: %v", hard)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rootMonitorStartupExit(ctx, &monitorAdmissionCleanupError{cause: context.Canceled}, 2) != 3 {
		t.Fatal("cancellation hid unresolved cleanup")
	}
}

// Standard errno leaves implement Is themselves, but only this exact bounded
// set denotes unavailable reads; an arbitrary custom classifier gains nothing.
func TestRootMonitorStartupKnownReadCausesRemainRetryable(t *testing.T) {
	for _, cause := range []error{syscall.EIO, syscall.EAGAIN, syscall.EBUSY, syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM, syscall.ENOSPC, syscall.EDQUOT, syscall.EINTR, syscall.ETIMEDOUT, durablevolume.ErrBusy, durablevolume.ErrUnavailable, context.DeadlineExceeded, context.Canceled} {
		if !rootMonitorStartupPending(errors.Join(&os.PathError{Op: "read", Path: "synthetic-policy", Err: cause}, context.DeadlineExceeded)) {
			t.Fatalf("known bounded read cause became terminal: %v", cause)
		}
	}
}
