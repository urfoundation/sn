// Actual publication, joined close and subsequent source-read barriers expose
// role-local storage continuation without a synthetic successful checkpoint.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Terminal notification follows both real descriptor closes. The healthy role
// remains live while another opener proves the stopped role released its locks.
func TestMonitorStorageStoppedRoleReleasesOwnersBeforePeerStops(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	closed := make(chan string, 2)
	terminal := make(chan int, 1)
	hooks := monitorServiceHooks{afterClose: func(role, kind string, file *os.File) error {
		if role == "alpha" {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("terminal observer preceded descriptor close")
			}
			closed <- kind
		}
		return nil
	}, afterWorker: func(role string, exit int) {
		if role == "alpha" {
			terminal <- exit
		}
	}}
	run := fixture.start(t, url, hooks)
	run.next(t)
	run.next(t)
	checkpoint, metrics := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	if err := os.Rename(metrics, metrics+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(metrics+".retained", metrics); err != nil {
		t.Fatal(err)
	}
	run.again(t, "alpha")
	if event := run.next(t); event.Role != "alpha" || event.Publication != "ownership-error" {
		t.Fatal("lost exporter did not stop its own role", event)
	}
	if exit := <-terminal; exit != 3 || len(closed) != 2 {
		t.Fatal("stopped role retained its actual owners", exit, len(closed))
	}
	if first, second := <-closed, <-closed; first != "metrics" || second != "checkpoint" {
		t.Fatal("terminal cleanup omitted an owner", first, second)
	}
	for _, path := range []string{metrics + ".lock", checkpoint + ".lock"} {
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
		if err != nil {
			t.Fatal(err)
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err := errors.Join(err, file.Close()); err != nil {
			t.Fatal("stopped role lock was not released", path, err)
		}
	}
	run.again(t, "beta")
	if event := run.next(t); event.Role != "beta" || event.Publication != "published" {
		t.Fatal("joined stopped role suppressed its healthy peer", event)
	}
	run.cancel()
	<-run.done
	if run.exit != 3 {
		t.Fatal("terminal role result was lost", run.exit, run.stderr.String())
	}
}

// The database-backed role obeys the same terminal ownership boundary. Its
// reader and publishers join while the validator completes another real sample.
func TestMonitorStorageStoppedOperatorReleasesOwnersBeforePeerStops(t *testing.T) {
	fixture, _ := operatorDatabaseFixture(t)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := &monitorOperatorTestSink{events: make(chan monitorOperatorTestEvent, 4), validators: make(chan string, 4)}
	resume := map[string]chan struct{}{"alpha": make(chan struct{}), "operator-a": make(chan struct{})}
	terminal := make(chan int, 1)
	hooks := monitorServiceHooks{afterWorker: func(role string, exit int) {
		if role == "operator-a" {
			terminal <- exit
		}
	}, wait: func(ctx context.Context, role string, _ time.Duration) bool {
		select {
		case <-resume[role]:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	done := make(chan int, 1)
	go func() {
		defer close(done)
		done <- runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), sink, nil, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { cancel(); <-done })
	if event := <-sink.events; event.Publication != "published" {
		t.Fatal("operator fixture did not publish its real journal observation", event)
	}
	<-sink.validators
	checkpoint, metrics := monitorOperatorPaths(fixture.checkpointPath, fixture.metricsPath, "operator-a")
	if err := os.Rename(metrics, metrics+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(metrics+".retained", metrics); err != nil {
		t.Fatal(err)
	}
	resume["operator-a"] <- struct{}{}
	if event := <-sink.events; event.Publication != "ownership-error" {
		t.Fatal("operator publisher loss did not stop its own role", event)
	}
	if exit := <-terminal; exit != 3 {
		t.Fatal("operator terminal exit differs", exit)
	}
	for _, path := range []string{metrics + ".lock", checkpoint + ".lock"} {
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
		if err != nil {
			t.Fatal(err)
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err := errors.Join(err, file.Close()); err != nil {
			t.Fatal("stopped operator retained its lock", path, err)
		}
	}
	resume["alpha"] <- struct{}{}
	if role := <-sink.validators; role != "alpha" {
		t.Fatal("stopped operator suppressed validator publication", role)
	}
	cancel()
	if exit := <-done; exit != 3 {
		t.Fatal("operator terminal result disappeared", exit)
	}
}

func TestMonitorStorageCheckpointLostAckReopensExactBytesAndPreservesPeer(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	var failed sync.Once
	var closes atomic.Int32
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == "alpha" && kind == "checkpoint" {
			failed.Do(func() { err = errors.Join(err, syscall.EIO) })
		}
		return err
	}, afterClose: func(role, kind string, file *os.File) error {
		if role == "alpha" && kind == "checkpoint" {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("checkpoint reopened before old descriptor joined")
			}
			closes.Add(1)
		}
		return nil
	}}
	run := fixture.start(t, url, hooks)
	seen := map[string]monitorServiceEvent{}
	for len(seen) < 2 {
		event := run.next(t)
		seen[event.Role] = event
	}
	first := seen["alpha"]
	if first.Publication != "retrying" || !first.State.PublicationLastSuccessAt.IsZero() || seen["beta"].Publication != "published" {
		t.Fatal("uncertain publication fabricated success or stopped peer", seen)
	}
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("real publication did not retain original bytes")
	}
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	run.again(t, "beta")
	if event := run.next(t); event.Role != "beta" || event.Publication != "published" {
		t.Fatal("stopped owner suppressed peer", event)
	}
	run.again(t, "alpha")
	recovered := run.next(t)
	if recovered.Role != "alpha" || recovered.Publication != "published" || recovered.State.ReadStatus != "missing" || recovered.State.Record == nil || !recovered.State.LastReadSuccessAt.Equal(first.State.LastReadSuccessAt) || closes.Load() != 1 {
		t.Fatal("exact pending continuation lost retained source", recovered)
	}
	after, err := os.ReadFile(path)
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("resumed owner did not publish its new missing-source observation", err)
	}
	run.cancel()
	<-run.done
	if run.exit != 0 {
		t.Fatal("continued owner did not join", run.exit, run.stderr.String())
	}
}

func TestMonitorStorageCheckpointRecoveryBudgetIsPerRole(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	url, _, _ := monitorServicesBlockedChain(t)
	var closes atomic.Int32
	closed := make(chan struct{}, 8)
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == "alpha" && kind == "checkpoint" {
			return errors.Join(err, syscall.EIO)
		}
		return err
	}, afterClose: func(role, kind string, file *os.File) error {
		if role == "alpha" && kind == "checkpoint" {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return err
			}
			closes.Add(1)
			closed <- struct{}{}
		}
		return nil
	}}
	run := fixture.start(t, url, hooks)
	seen := map[string]monitorServiceEvent{}
	for len(seen) < 2 {
		event := run.next(t)
		seen[event.Role] = event
	}
	if seen["alpha"].Publication != "retrying" {
		t.Fatal("lost acknowledgement became success", seen)
	}
	<-closed
	for range monitorStorageReconciliations {
		run.again(t, "alpha")
		if event := run.next(t); event.Role != "alpha" || event.Publication != "retrying" {
			t.Fatal("recovery attempted to acknowledge uncertainty", event)
		}
		<-closed
	}
	if closes.Load() != monitorStorageReconciliations+1 {
		t.Fatal("reconciliation generations were unbounded", closes.Load())
	}
	run.again(t, "beta")
	if event := run.next(t); event.Role != "beta" || event.Publication != "published" {
		t.Fatal("exhausted owner stopped peer", event)
	}
	run.cancel()
	<-run.done
	if run.exit != 3 {
		t.Fatal("exhausted recovery failed to report stopped role", run.exit, run.stderr.String())
	}
}

func TestMonitorStorageReserveRecoversOnSameOwner(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	provisionMonitorTestCustody(t, fixture.checkpointPath)
	for _, role := range fixture.policy.Validators {
		path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, role.Role)
		provisionMonitorTestCustody(t, path)
	}
	volume := durablefixture.New(t, t.Context(), fixture.directory)
	var closes atomic.Int32
	run := fixture.startWithContext(t, volume.Context, url, monitorServiceHooks{afterClose: func(role, kind string, _ *os.File) error {
		if role == "alpha" && kind == "checkpoint" {
			closes.Add(1)
		}
		return nil
	}})
	first := run.next(t)
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	volume.Host.SetReserve(0, 0)
	run.again(t, "alpha")
	if event := run.next(t); event.Publication != "retrying" {
		t.Fatal("full volume was acknowledged", event)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || closes.Load() != 0 {
		t.Fatal("reserve pressure replaced owner or mutated checkpoint", err, closes.Load())
	}
	volume.Host.SetReserve(1<<40, 1<<30)
	run.again(t, "alpha")
	event := run.next(t)
	if event.Publication != "published" || !event.State.LastReadSuccessAt.Equal(first.State.LastReadSuccessAt) || closes.Load() != 0 {
		t.Fatal("same owner could not recover reserve", event, closes.Load())
	}
	run.cancel()
	<-run.done
	if run.exit != 0 {
		t.Fatal("reserve recovery failed", run.exit, run.stderr.String())
	}
}

// The real retained head classifies a missing leaf as identity loss even though
// its wrapped syscall also satisfies os.IsNotExist. No reader may call it fresh.
func TestMonitorStorageLostMemberCannotBecomeFreshThroughWrappedAbsence(t *testing.T) {
	root := mainnetPrivateTestDir(t)
	volume := durablefixture.New(t, t.Context(), root)
	path := filepath.Join(root, "monitor.json")
	provisionMonitorTestCustody(t, path)
	store, err := openMonitorCheckpoint(path, monitorTestExpectation(), volume.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Unix(1700000000, 0).UTC()}
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	_, err = store.directory.read(filepath.Base(path), maxRpcReplyBytes, true)
	if !errors.Is(err, durablevolume.ErrIdentity) || monitorCheckpointAbsent(err) {
		t.Fatal("lost retained leaf was treated as pristine", err)
	}
	if err := os.Rename(path+".retained", path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("invalidated owner accepted restored leaf", err)
	}
}

// A context-aware event sink observes real public-command publication.
type monitorStorageChainWriter struct{ events chan monitorEvent }

func (self *monitorStorageChainWriter) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}
func (self *monitorStorageChainWriter) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var event monitorEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	select {
	case self.events <- event:
		return len(raw), nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Complete post-sync bytes must reopen under the same finite controller. The
// original retained finality remains required before a new observation can win.
func TestMonitorStorageChainLostAckReopensOriginalCheckpoint(t *testing.T) {
	server, _ := testRpcServerWithEvm(t, "0x3c4", "")
	root := mainnetPrivateTestDir(t)
	volume := durablefixture.New(t, t.Context(), root)
	path := filepath.Join(root, "monitor.json")
	provisionMonitorTestCustody(t, path)
	ctx, cancel := context.WithCancel(volume.Context)
	defer cancel()
	sink := &monitorStorageChainWriter{events: make(chan monitorEvent, 8)}
	resume, closed, done := make(chan struct{}), make(chan struct{}, 1), make(chan int, 1)
	var failed sync.Once
	var stderr bytes.Buffer
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == "chain" && kind == "checkpoint" {
			failed.Do(func() { err = errors.Join(err, syscall.EIO) })
		}
		return err
	}, afterClose: func(role, kind string, file *os.File) error {
		if role == "chain" && kind == "checkpoint" {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("old chain owner was not joined")
			}
			closed <- struct{}{}
		}
		return nil
	}, wait: func(ctx context.Context, _ string, _ time.Duration) bool {
		select {
		case <-resume:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	args := []string{"monitor", "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", path, "--interval", "1h"}
	go func() { done <- runMainWithMonitorHooks(ctx, args, sink, &stderr, time.Now, hooks) }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	first := <-sink.events
	if first.Status != "checkpoint-error" {
		t.Fatal("uncertain chain checkpoint became success", first)
	}
	<-closed
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var retained monitorCheckpointRecord
	if err := json.Unmarshal(raw, &retained); err != nil || retained.FinalizedHash != testFinalizedHash || retained.FinalizedAt != 100 {
		t.Fatal("real complete checkpoint missing", retained, err)
	}
	resume <- struct{}{}
	second := <-sink.events
	if second.Status != "ok" {
		t.Fatal("chain did not reconcile exact retained checkpoint", second)
	}
	cancel()
	exit := <-done
	joined = true
	if exit != 0 {
		t.Fatal("recovered chain failed to join", exit, stderr.String())
	}
	opened, err := openMonitorCheckpoint(path, monitorTestExpectation(), volume.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.close()
	state, err := opened.load()
	if err != nil || state.lastHash != testFinalizedHash || state.lastNumber != 100 {
		t.Fatal("recovery lost original finality", state, err)
	}
}

// Reserve recovery does not discard the owner generation. This is observed at
// actual public command publication, with completed bytes unchanged on refusal.
func TestMonitorStorageChainReserveRetainsOwner(t *testing.T) {
	server, _ := testRpcServerWithEvm(t, "0x3c4", "")
	root := mainnetPrivateTestDir(t)
	volume := durablefixture.New(t, t.Context(), root)
	path := filepath.Join(root, "monitor.json")
	provisionMonitorTestCustody(t, path)
	ctx, cancel := context.WithCancel(volume.Context)
	defer cancel()
	sink := &monitorStorageChainWriter{events: make(chan monitorEvent, 8)}
	resume, done := make(chan struct{}), make(chan int, 1)
	var closes atomic.Int32
	var stderr bytes.Buffer
	hooks := monitorServiceHooks{afterClose: func(_, kind string, _ *os.File) error {
		if kind == "checkpoint" {
			closes.Add(1)
		}
		return nil
	}, wait: func(ctx context.Context, _ string, _ time.Duration) bool {
		select {
		case <-resume:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	args := []string{"monitor", "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--checkpoint", path, "--interval", "1h"}
	go func() { done <- runMainWithMonitorHooks(ctx, args, sink, &stderr, time.Now, hooks) }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	if event := <-sink.events; event.Status != "ok" {
		t.Fatal(event)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	volume.Host.SetReserve(0, 0)
	resume <- struct{}{}
	if event := <-sink.events; event.Status != "checkpoint-error" {
		t.Fatal("pressure did not refuse publication", event)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || closes.Load() != 0 {
		t.Fatal("pressure discarded retained generation", err, closes.Load())
	}
	volume.Host.SetReserve(1<<40, 1<<30)
	resume <- struct{}{}
	if event := <-sink.events; event.Status != "ok" || closes.Load() != 0 {
		t.Fatal("same chain owner could not recover", event, closes.Load())
	}
	cancel()
	exit := <-done
	joined = true
	if exit != 0 {
		t.Fatal("chain continuation failed", exit, stderr.String())
	}
}

// The operator reads an owned disposable PostgreSQL journal. A lost local
// checkpoint acknowledgement must retain the original unresolved transaction
// incident while its credential source becomes temporarily unavailable.
func TestMonitorStorageOperatorLostAckRetainsIncident(t *testing.T) {
	fixture, database := operatorDatabaseFixture(t)
	database.Intent(t, 1, "uncertain", 1, database.Now.Add(-time.Hour))
	url, _, _ := monitorServicesBlockedChain(t)
	var failed sync.Once
	var closes atomic.Int32
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == "operator-a" && kind == "checkpoint" {
			failed.Do(func() { err = errors.Join(err, syscall.EIO) })
		}
		return err
	}, afterClose: func(role, kind string, file *os.File) error {
		if role == "operator-a" && kind == "checkpoint" {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("operator reopened unjoined owner")
			}
			closes.Add(1)
		}
		return nil
	}}
	cancel, done, sink, resume := startMonitorOperatorTest(t, fixture, url, hooks)
	first := <-sink.events
	if first.Publication != "retrying" || first.State == nil || first.State.Transactions.Latest == nil || !first.State.PublicationLastSuccessAt.IsZero() {
		t.Fatal("uncertain operator snapshot fabricated acknowledgement", first)
	}
	if role := <-sink.validators; role != "alpha" {
		t.Fatal("operator checkpoint failure stopped validator peer", role)
	}
	incident := first.State.Transactions.Latest.Id
	if err := os.Remove(fixture.policy.Operators[0].DatabaseFile); err != nil {
		t.Fatal(err)
	}
	resume <- struct{}{}
	second := <-sink.events
	if second.Publication != "published" || second.State.ReadStatus != "unavailable" || second.State.Transactions.Latest == nil || second.State.Transactions.Latest.Id != incident || closes.Load() != 1 {
		t.Fatal("operator continuation lost retained incident", second, closes.Load())
	}
	cancel()
	if exit := <-done; exit != 0 {
		t.Fatal("continued operator failed joined cancellation", exit)
	}
}
