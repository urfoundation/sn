package miner

// Published-member failures exercise public Run and the real refresh writer.
// Explicit cleanup barriers prove the callback never joins its own worker tree.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/clientauth"
)

// Keeps two actual admitted instance generations until the test triggers a
// production callback. Only member-two close is deliberately held at a barrier.
type swarmPublishedFixture struct {
	swarm        *ProviderSwarm
	config       ProviderSwarmConfig
	firstCtx     context.Context
	secondCtx    context.Context
	secondFailed func(error)
	firstCloses  atomic.Int32
	secondCloses atomic.Int32
	secondStarts atomic.Int32
	teardown     *swarmControlTestBarrier
	runDone      chan struct{}
	runErr       error
	cancel       context.CancelFunc
}

// The last initial operation is shared with Run, so completion is a positive
// barrier before the callback; there is no sleep-based readiness assumption.
func newSwarmPublishedFixture(t *testing.T) *swarmPublishedFixture {
	t.Helper()
	config := validProviderSwarmConfig(t)
	second := validProviderSwarmConfig(t).Members[0]
	second.ID, second.SourceIP = "miner-2", "127.64.0.2"
	config.Members = append(config.Members, second)
	swarm, err := NewProviderSwarm(&config)
	if err != nil {
		t.Fatal(err)
	}
	config.ListenAddress = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	self := &swarmPublishedFixture{swarm: swarm, config: config, teardown: newSwarmControlTestBarrier(t), runDone: make(chan struct{}), cancel: cancel}
	secondEntered := make(chan struct{})
	swarm.startMember = func(memberCtx context.Context, member ProviderSwarmMember, failed func(error)) (*providerSwarmInstance, error) {
		if member.ID == "miner-1" {
			self.firstCtx = memberCtx
			return &providerSwarmInstance{connectedOverride: func() bool { return true }, refreshSub: &swarmControlTestSub{close: func() { self.firstCloses.Add(1) }}}, nil
		}
		if self.secondStarts.Add(1) == 1 {
			self.secondCtx, self.secondFailed = memberCtx, failed
			close(secondEntered)
			return &providerSwarmInstance{connectedOverride: func() bool { return true }, refreshSub: &swarmControlTestSub{close: func() {
				self.secondCloses.Add(1)
				if memberCtx.Err() == nil {
					t.Error("member cleanup preceded its lifetime cancellation")
				}
				self.teardown.block()
			}}}, nil
		}
		return &providerSwarmInstance{connectedOverride: func() bool { return true }}, nil
	}
	go func() { self.runErr = swarm.Run(ctx); close(self.runDone) }()
	t.Cleanup(func() { cancel(); self.teardown.release(); waitSwarmControlTestSignal(t, self.runDone) })
	select {
	case <-secondEntered:
	case <-self.runDone:
		t.Fatal("run stopped before callback fixture admission", self.runErr)
	case <-time.After(5 * time.Second):
		t.Fatal("second member was not admitted")
	}
	if err := swarm.controlMember(ctx, "miner-2", true); err != nil {
		t.Fatal(err)
	}
	return self
}

// A failed replacement cannot corrupt the saved original token. The actual
// refresh callback must quarantine only its owner and return before its join.
func TestProviderSwarmRunQuarantinesJwtPersistenceFailure(t *testing.T) {
	self := newSwarmPublishedFixture(t)
	path := filepath.Join(self.config.Members[1].StateDir, ".provider.jwt")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	retained := path + ".retained"
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	callbackDone := make(chan struct{})
	go func() {
		swarmMemberJwtRefreshListener(path, self.secondFailed).JwtRefreshed("synthetic-refreshed-token")
		close(callbackDone)
	}()
	waitSwarmControlTestSignal(t, callbackDone)
	waitSwarmControlTestSignal(t, self.teardown.entered)
	if self.firstCtx.Err() != nil || self.firstCloses.Load() != 0 {
		t.Fatal("member JWT persistence failure canceled an unrelated healthy generation")
	}
	if self.secondCtx.Err() == nil || self.secondCloses.Load() != 1 {
		t.Fatal("failed member was not canceled before joined quarantine")
	}
	status := readSwarmControlTestStatus(t, self.swarm, "miner-2", "stopping")
	if status.Failure == "" {
		t.Fatal("quarantine hid the original local failure")
	}
	readSwarmControlTestStatus(t, self.swarm, "miner-1", "running")
	if raw, err := os.ReadFile(retained); err != nil || string(raw) != string(before) {
		t.Fatal("failed refresh changed original retained token", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, path); err != nil {
		t.Fatal(err)
	}
	waitCtx := &swarmControlWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		self.swarm.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/control/miner-2/enable", nil).WithContext(waitCtx))
		responseDone <- response
	}()
	waitSwarmControlTestSignal(t, waitCtx.waiting)
	if self.secondStarts.Load() != 1 {
		t.Fatal("replacement started before failed generation joined")
	}
	self.teardown.release()
	waitSwarmControlTestResponse(t, responseDone, http.StatusOK)
	self.secondFailed(&swarmMemberStorageError{cause: syscall.ENOSPC})
	if self.firstCtx.Err() != nil || self.firstCloses.Load() != 0 || self.secondStarts.Load() != 2 {
		t.Fatal("affected recovery or stale callback reset healthy membership")
	}
	readSwarmControlTestStatus(t, self.swarm, "miner-2", "running")
	self.cancel()
	waitSwarmControlTestSignal(t, self.runDone)
	if self.runErr != nil || self.firstCloses.Load() != 1 {
		t.Fatal("run did not join healthy generation exactly once", self.runErr, self.firstCloses.Load())
	}
}

// A temporary disk cause is still an uncertain local write, never permission
// for an automatic retry. Joined authentication failure must remain terminal.
func TestProviderSwarmRunPersistenceAndAuthenticationFailureIsTerminal(t *testing.T) {
	self := newSwarmPublishedFixture(t)
	self.secondFailed(errors.Join(&swarmMemberStorageError{cause: syscall.EIO}, errSwarmAuthenticationRejected))
	if self.firstCtx.Err() == nil {
		t.Fatal("joined authentication refusal was downgraded to local storage unavailability")
	}
	self.teardown.release()
	waitSwarmControlTestSignal(t, self.runDone)
	if !errors.Is(self.runErr, errSwarmAuthenticationRejected) || self.firstCloses.Load() != 1 {
		t.Fatal("hard authentication policy did not join the full swarm", self.runErr, self.firstCloses.Load())
	}
}

// The production listener's type and underlying syscall remain inspectable.
func TestProviderSwarmJwtRefreshPreservesStorageCause(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "token")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	var failure error
	var listener clientauth.JwtRefreshListenerFunc = swarmMemberJwtRefreshListener(path, func(err error) { failure = err })
	listener.JwtRefreshed("synthetic-token")
	var storage *swarmMemberStorageError
	var rename *os.LinkError
	if !errors.As(failure, &storage) || !errors.As(failure, &rename) || rename.Op != "rename" || rename.New != path || !errors.Is(failure, rename.Err) {
		t.Fatal("refresh did not retain actual local rename refusal", failure)
	}
}
