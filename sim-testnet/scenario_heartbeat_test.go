package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type blockedHeartbeatProbe struct {
	release <-chan struct{}
	head    ChainHead
}

func (p blockedHeartbeatProbe) Snapshot(context.Context) (*ScenarioObservation, error) {
	<-p.release
	return &ScenarioObservation{Schema: "urnetwork-sim-scenario-observation-v1"}, nil
}

func (p blockedHeartbeatProbe) FinalizedHead(context.Context) (ChainHead, error) {
	return p.head, nil
}

func TestWaitScenarioSnapshotAdvancesHeartbeatBeforeSlowObservation(t *testing.T) {
	release := make(chan struct{})
	probe := blockedHeartbeatProbe{release: release, head: ChainHead{Number: 42, Hash: "0xhead"}}
	heartbeat := make(chan ChainHead, 1)
	result := make(chan error, 1)
	go func() {
		_, err := waitScenarioSnapshot(t.Context(), probe, time.Millisecond, func(_ context.Context, head ChainHead) error {
			heartbeat <- head
			return nil
		})
		result <- err
	}()
	select {
	case got := <-heartbeat:
		if got != probe.head {
			t.Fatalf("heartbeat=%+v want=%+v", got, probe.head)
		}
	case <-time.After(time.Second):
		t.Fatal("slow snapshot prevented heartbeat")
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot did not complete")
	}
}

func TestWaitScenarioSnapshotReturnsHeartbeatFailure(t *testing.T) {
	release := make(chan struct{})
	probe := blockedHeartbeatProbe{release: release, head: ChainHead{Number: 42, Hash: "0xhead"}}
	want := errors.New("fault transition failed")
	_, err := waitScenarioSnapshot(t.Context(), probe, time.Millisecond, func(context.Context, ChainHead) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want=%v", err, want)
	}
	close(release)
}
