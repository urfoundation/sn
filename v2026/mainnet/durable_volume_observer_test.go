// Actual root command controls retain real files, locks and snapshot heads.
// Only kernel observation errors and completed Rpc boundaries are injected.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A caller-local observer can fail a fact lookup but cannot supply file bytes,
// custody identity, a successful sample or a replacement admission verdict.
type compositionObservationHost struct {
	durablevolume.Host
	observe func(*os.File) error
}

func (self *compositionObservationHost) Filesystem(file *os.File) (durablevolume.Filesystem, error) {
	if err := self.observe(file); err != nil {
		return durablevolume.Filesystem{}, err
	}
	return self.Host.Filesystem(file)
}

// The signed passive fixture publishes the same actual runtime input used by
// runMain. No service manager, signing operation or live endpoint is invoked.
func compositionPassiveArguments(t *testing.T, fixture *bootstrapChainFixture) []string {
	t.Helper()
	fixture.result(t, "apply")
	config := rootPassiveRuntimeConfig{Schema: rootPassiveRuntimeSchema, Root: fixture.config.Root, Role: *fixture.config.RootValidator}
	reference := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(fixture.path), "observer-runtime.json"), config)
	return []string{"root-passive-service", "run", "--config", reference.Path, "--accept-runtime-sha256", reference.Sha256}
}

// Counts are read under the fixture's original lock after/while real HTTP
// handlers run. Storage checks occur only on the command's joined owner.
func compositionRpcReads(fixture *rootRpcFixture) int {
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	return fixture.methodCounts["chain_getFinalizedHead"]
}

// Valid public admission must fence an acknowledged checkpoint across close
// and reopen even when the original root and marker remain physically intact.
func TestDurableCompositionDeclaredRootObserverRetainsCompletedCheckpoint(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	root := t.TempDir()
	storage := durablefixture.New(t, t.Context(), root)
	path := filepath.Join(root, "root-checkpoint.json")
	provisionMonitorTestCustody(t, path)
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--checkpoint", path}
	var output, diagnostics bytes.Buffer
	if code := runRootCommand(storage.Context, args, &output, &diagnostics); code != 0 {
		t.Fatal("declared direct observer did not retain its first sample", code, diagnostics.String())
	}
	original, err := os.ReadFile(path)
	if err != nil || len(original) == 0 {
		t.Fatal("missing completed checkpoint", err)
	}
	retained := path + ".retained"
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	diagnostics.Reset()
	if code := runRootCommand(storage.Context, args, &output, &diagnostics); code != 3 || output.Len() != 0 {
		t.Fatal("declared restart replaced lost acknowledged custody", code, output.String(), diagnostics.String())
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refusal created a fresh checkpoint", err)
	}
	if raw, err := os.ReadFile(retained); err != nil || !bytes.Equal(original, raw) {
		t.Fatal("refusal altered retained original bytes", err)
	}
	if err := os.Rename(retained, path); err != nil {
		t.Fatal(err)
	}
	if code := runRootCommand(storage.Context, args, io.Discard, io.Discard); code != 0 {
		t.Fatal("joined public owner could not reopen exact original custody", code)
	}
}

// The first preparation FD and the later checkpoint FD remain owned through
// a recoverable failure before Rpc. The completed sample is published once.
func TestDurableCompositionPassiveObservationRetriesBeforeRpc(t *testing.T) {
	compositionPreparationObservationRetry(t, false, false, false)
}

// A full Rpc observation cannot be published while its preparation facts are
// unavailable; the same retained descriptors may admit it after recovery.
func TestDurableCompositionPassiveObservationRetriesAfterRpc(t *testing.T) {
	compositionPreparationObservationRetry(t, true, false, false)
}

// Persistent unavailability has both an elapsed deadline and a finite attempt
// ceiling. The accelerated owned wait cannot turn it into an unbounded loop.
func TestDurableCompositionPassiveObservationRetryBudgetIsFinite(t *testing.T) {
	compositionPreparationObservationRetry(t, false, true, false)
}

// Cancellation at the owned retry wait joins both retained descriptors before
// a later invocation can acquire and inspect their unchanged original bytes.
func TestDurableCompositionPassiveObservationCancellationJoinsOwners(t *testing.T) {
	compositionPreparationObservationRetry(t, false, false, true)
}

// This is a classification invariant for an already-classified joined error,
// not a claim that the real kernel emits a particular mixed fault. Proven loss
// must remain terminal even if a separate observation was unavailable too.
func TestDurableCompositionPassiveJoinedIdentityRemainsTerminal(t *testing.T) {
	fixture := newBootstrapRootPassiveFixture(t)
	compositionPassiveArguments(t, fixture)
	ctx := fixture.storageContext(t.Context())
	reader, err := openRootPassivePreparation(ctx, fixture.root.plan)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	reader.reader.storage.failed = errors.Join(durablevolume.ErrIdentity, durablevolume.ErrUnavailable)
	service := fixture.root.plan.PassiveService
	args := []string{"root-monitor", "--rpc", service.RpcUrl, "--policy", fixture.root.plan.ServiceInput.Path, "--checkpoint", service.CheckpointPath}
	reads := compositionRpcReads(fixture.census)
	if code := runRootCommandWithPolicy(ctx, args, io.Discard, io.Discard, time.Now, monitorServiceHooks{}, &service.Policy, rootObjectHash(service.Policy), reader); code != 3 || compositionRpcReads(fixture.census) != reads {
		t.Fatal("classified identity loss became a retryable observer exit", code)
	}
}

// The homogeneous cases vary only the exact observation boundary or terminal
// policy. Real admission, snapshot reads, Rpc and close remain unchanged.
func compositionPreparationObservationRetry(t *testing.T, afterRpc, exhausted, canceled bool) {
	t.Helper()
	fixture := newBootstrapRootPassiveFixture(t)
	args := compositionPassiveArguments(t, fixture)
	beforeReads := compositionRpcReads(fixture.census)
	preparationPath := filepath.Join(fixture.root.plan.RunDirectory, bootstrapRootProgressFile)
	before, err := os.ReadFile(preparationPath)
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	var preparationFile, checkpointFile *os.File
	failed, recovered := false, false
	host := &compositionObservationHost{Host: fixture.root.storage.Host}
	host.observe = func(file *os.File) error {
		if preparationFile == nil {
			preparationFile = file
		} else if file != preparationFile && checkpointFile == nil {
			checkpointFile = file
		}
		boundary := checkpointFile != nil
		if afterRpc {
			boundary = compositionRpcReads(fixture.census) > beforeReads
		}
		if file == preparationFile && boundary && !recovered {
			failed = true
			return unix.EIO
		}
		return nil
	}
	ctx := durablepath.WithHost(fixture.storageContext(parent), host)
	waits, events := 0, 0
	hooks := monitorServiceHooks{
		wait: func(waitCtx context.Context, role string, delay time.Duration) bool {
			waits++
			if !failed || events != 0 || role != "root" || delay <= 0 || delay > 30*time.Second || waits > 64 {
				t.Fatal("retry escaped its finite unpublished owner boundary", failed, events, role, delay, waits)
			}
			if waitCtx.Err() != nil || preparationFile == nil || checkpointFile == nil {
				t.Fatal("retry discarded an admitted owner", waitCtx.Err())
			}
			if _, err := preparationFile.Stat(); err != nil {
				t.Fatal("retry closed preparation prematurely", err)
			}
			if _, err := checkpointFile.Stat(); err != nil {
				t.Fatal("retry closed checkpoint prematurely", err)
			}
			if _, err := os.Lstat(fixture.root.plan.PassiveService.CheckpointPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unavailable preparation published a checkpoint", err)
			}
			if canceled {
				cancel()
				return false
			}
			if !exhausted {
				recovered = true
			}
			return true
		},
		afterEvent: func(context.Context, string) { events++ },
	}
	var output, diagnostics bytes.Buffer
	code := runMainWithMonitorHooks(ctx, args, &output, &diagnostics, time.Now, hooks)
	wantCode, wantWaits, wantEvents := 0, 1, 1
	if exhausted {
		wantCode, wantWaits, wantEvents = 1, 64, 1
	}
	if canceled {
		wantEvents = 0
	}
	if code != wantCode || waits != wantWaits || events != wantEvents || !failed {
		t.Fatal("public preparation recovery differs", code, waits, events, failed, diagnostics.String())
	}
	if preparationFile == nil || checkpointFile == nil {
		t.Fatal("public command did not own both physical roots")
	}
	for _, file := range []*os.File{preparationFile, checkpointFile} {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("returned command left a physical owner live", err)
		}
	}
	if raw, err := os.ReadFile(preparationPath); err != nil || !bytes.Equal(before, raw) {
		t.Fatal("observation recovery changed prepared custody", err)
	}
	if exhausted {
		var event rootMonitorEvent
		if err := json.Unmarshal(output.Bytes(), &event); err != nil || event.Status != "storage-unavailable" || event.Observation != nil || event.Snapshot != nil || compositionRpcReads(fixture.census) != beforeReads {
			t.Fatal("exhausted finite sample invented freshness or omitted its outage", err, output.String())
		}
	} else if wantEvents == 1 {
		var event rootMonitorEvent
		if err := json.Unmarshal(output.Bytes(), &event); err != nil || event.Status != "ready" || event.Observation == nil {
			t.Fatal("recovered actual observation was not published", err, output.String())
		}
	} else if output.Len() != 0 || compositionRpcReads(fixture.census) != beforeReads {
		t.Fatal("unavailable/canceled preparation reached Rpc or publication", output.String())
	}
	// Only after the previous synchronous command has joined, reopen the
	// original prepared root with an uncanceled context and unchanged facts.
	reader, err := openRootPassivePreparation(fixture.storageContext(t.Context()), fixture.root.plan)
	if err != nil {
		t.Fatal("joined preparation could not be reopened", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

// A successful network observation cannot hide a missing retained local head,
// marker or record. The unrelated owner still writes after this role stops.
func TestDurableCompositionPassivePreparationLossDuringRpcStopsOnlyOwner(t *testing.T) {
	for _, lost := range []string{"head", "marker", "record"} {
		fixture := newBootstrapRootPassiveFixture(t)
		args := compositionPassiveArguments(t, fixture)
		beforeReads := compositionRpcReads(fixture.census)
		path := filepath.Join(fixture.root.plan.RunDirectory, bootstrapRootProgressFile)
		peerRoot := t.TempDir()
		peerFixture := durablefixture.New(t, t.Context(), peerRoot)
		peerPath := filepath.Join(peerRoot, "peer.json")
		provisionMonitorTestCustody(t, peerPath)
		peer, err := openMonitorCheckpoint(peerPath, monitorTestExpectation(), peerFixture.Context)
		if err != nil {
			t.Fatal(err)
		}
		defer peer.close()
		changed := false
		host := &compositionObservationHost{Host: fixture.root.storage.Host}
		host.observe = func(*os.File) error {
			if changed || compositionRpcReads(fixture.census) == beforeReads {
				return nil
			}
			changed = true
			if lost == "head" {
				file, err := os.Open(path + ".lock")
				if err != nil {
					t.Fatal(err)
				}
				err = unix.Fremovexattr(int(file.Fd()), durablehead.Attribute("mainnet-bootstrap-root", filepath.Base(path)))
				if err := errors.Join(err, file.Close()); err != nil {
					t.Fatal(err)
				}
			} else {
				member := path
				if lost == "marker" {
					member += ".lock"
				}
				if err := os.Rename(member, member+".retained"); err != nil {
					t.Fatal(err)
				}
			}
			return nil
		}
		ctx := durablepath.WithHost(fixture.storageContext(t.Context()), host)
		var output, diagnostics bytes.Buffer
		if code := runMain(ctx, args, &output, &diagnostics); code != 3 || !changed || output.Len() != 0 {
			t.Fatal("loss during read published or failed to stop its owner", lost, code, changed, output.String(), diagnostics.String())
		}
		if _, err := os.Lstat(fixture.root.plan.PassiveService.CheckpointPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("lost preparation advanced observer custody", lost, err)
		}
		state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Unix(1700000000, 0).UTC()}
		if err := peer.save(state); err != nil || ctx.Err() != nil {
			t.Fatal("affected observer stopped its peer", lost, err, ctx.Err())
		}
	}
}

// Passive preparation is a shared read owner, including under write reserve
// exhaustion/read-only facts. It neither upgrades the lock nor changes bytes.
func TestDurableCompositionPassiveReadersSharePreparationWithoutWriteReserve(t *testing.T) {
	fixture := newBootstrapRootPassiveFixture(t)
	compositionPassiveArguments(t, fixture)
	path := filepath.Join(fixture.root.plan.RunDirectory, bootstrapRootProgressFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.root.storage.Host.SetReserve(0, 0)
	fixture.root.storage.Host.SetReadOnly(true)
	ctx := fixture.storageContext(t.Context())
	first, err := openRootPassivePreparation(ctx, fixture.root.plan)
	if err != nil {
		t.Fatal("passive reader required write reserve", err)
	}
	defer first.Close()
	second, err := openRootPassivePreparation(ctx, fixture.root.plan)
	if err != nil {
		t.Fatal("shared preparation readers did not compose", err)
	}
	defer second.Close()
	if err := errors.Join(first.check(), second.check()); err != nil {
		t.Fatal("read admission failed under write-only pressure", err)
	}
	file, err := os.Open(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
		t.Fatal("passive readers lost their retained shared marker", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
		t.Fatal("closing one reader released its peer", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("joined readers retained ownership", err)
	}
	if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(before, raw) {
		t.Fatal("read-only composition mutated preparation", err)
	}
}
