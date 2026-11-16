package miner

// Public startup must isolate temporary member admission failures while
// retaining already running members and the existing explicit control path.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// Forces a third member's admission after a failed second member. That public
// Run boundary proves progress without relying on an absence timeout.
func checkSwarmStartupRetainsHealthyMembers(t *testing.T, temporary error) {
	t.Helper()
	config := validProviderSwarmConfig(t)
	for _, memberSpec := range []struct{ id, source string }{
		{id: "miner-2", source: "127.64.0.2"},
		{id: "miner-3", source: "127.64.0.3"},
	} {
		member := validProviderSwarmConfig(t).Members[0]
		member.ID, member.SourceIP = memberSpec.id, memberSpec.source
		config.Members = append(config.Members, member)
	}
	swarm, err := NewProviderSwarm(&config)
	if err != nil {
		t.Fatal(err)
	}
	config.ListenAddress = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	third := newSwarmControlTestBarrier(t)
	var firstCtx context.Context
	var firstCloses atomic.Int32
	var firstStarts atomic.Int32
	var secondStarts atomic.Int32
	swarm.startMember = func(memberCtx context.Context, member ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		switch member.ID {
		case "miner-1":
			firstStarts.Add(1)
			firstCtx = memberCtx
			return &providerSwarmInstance{
				connectedOverride: func() bool { return true },
				refreshSub:        &swarmControlTestSub{close: func() { firstCloses.Add(1) }},
			}, nil
		case "miner-2":
			if secondStarts.Add(1) == 1 {
				return nil, temporary
			}
			return &providerSwarmInstance{connectedOverride: func() bool { return true }}, nil
		case "miner-3":
			third.block()
			return &providerSwarmInstance{connectedOverride: func() bool { return true }}, nil
		default:
			return nil, errors.New("unexpected synthetic member")
		}
	}
	runDone := make(chan struct{})
	var runErr error
	go func() {
		runErr = swarm.Run(ctx)
		close(runDone)
	}()
	t.Cleanup(func() {
		cancel()
		third.release()
		waitSwarmControlTestSignal(t, runDone)
	})
	select {
	case <-third.entered:
	case <-runDone:
		t.Fatalf("temporary startup failure stopped healthy members before later admission: %v (healthy closes=%d)", runErr, firstCloses.Load())
	case <-time.After(5 * time.Second):
		t.Fatal("run did not reach later-member admission")
	}
	if firstCtx == nil || firstCtx.Err() != nil || firstCloses.Load() != 0 || firstStarts.Load() != 1 {
		t.Fatal("temporary startup failure reset the retained healthy generation")
	}
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	readSwarmControlTestStatus(t, swarm, "miner-2", "failed")
	status := swarm.status()
	if status.Configured != 3 || status.Running != 1 || status.Failures["miner-2"] == "" {
		t.Fatalf("partial readiness was hidden: %+v", status)
	}
	response := httptest.NewRecorder()
	swarm.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/control/miner-2/enable", nil))
	if response.Code != http.StatusOK || secondStarts.Load() != 2 {
		t.Fatalf("public member recovery failed: code=%d attempts=%d body=%s", response.Code, secondStarts.Load(), response.Body.String())
	}
	if firstCtx.Err() != nil || firstCloses.Load() != 0 || firstStarts.Load() != 1 {
		t.Fatal("recovering the affected member restarted the healthy generation")
	}
	third.release()
	if err := swarm.controlMember(ctx, "miner-3", true); err != nil {
		t.Fatal(err)
	}
	status = swarm.status()
	if status.Running != 3 || len(status.Failures) != 0 || len(status.Disabled) != 0 {
		t.Fatalf("recovered member readiness was not published: %+v", status)
	}
	cancel()
	waitSwarmControlTestSignal(t, runDone)
	if runErr != nil || firstCloses.Load() != 1 {
		t.Fatalf("run did not join retained healthy member: err=%v closes=%d", runErr, firstCloses.Load())
	}
}

// Typed upstream unavailability must not collapse unrelated successful starts.
func TestProviderSwarmRunRetainsHealthyMembersAfterTemporaryStartupFailure(t *testing.T) {
	checkSwarmStartupRetainsHealthyMembers(t, &connect.HttpStatusError{StatusCode: http.StatusServiceUnavailable})
}

// An owned member startup deadline has the same isolation boundary as a 503.
func TestProviderSwarmRunRetainsHealthyMembersAfterStartupDeadline(t *testing.T) {
	checkSwarmStartupRetainsHealthyMembers(t, context.DeadlineExceeded)
}
