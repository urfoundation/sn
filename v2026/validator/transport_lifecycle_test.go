package validator

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

type tunnelAttemptCloseAndWaitFunc func(context.Context) error

func (self tunnelAttemptCloseAndWaitFunc) CloseAndWait(ctx context.Context) error {
	return self(ctx)
}

type tunnelAttemptTunCloseFunc func() error

func (self tunnelAttemptTunCloseFunc) Close() error {
	return self()
}

// The read pump may be inside a multi-client send when shutdown begins. Its
// owner closes the multi-client first, joins the pump, and only then retires
// generator-owned clients.
func TestTunnelAttemptCloseJoinsPumpBeforeGenerator(t *testing.T) {
	var stateLock sync.Mutex
	events := []string{}
	record := func(event string) {
		stateLock.Lock()
		events = append(events, event)
		stateLock.Unlock()
	}
	pumpRelease := make(chan struct{})
	pumpDone := make(chan struct{})
	go func() {
		<-pumpRelease
		record("pump")
		close(pumpDone)
	}()
	attempt := &tunnelAttempt{
		cancel: func() {
			record("cancel")
		},
		tun: tunnelAttemptTunCloseFunc(func() error {
			record("tun")
			return nil
		}),
		multiClient: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			record("multi-client")
			close(pumpRelease)
			return nil
		}),
		pumpDone: pumpDone,
		generator: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			record("generator")
			return nil
		}),
	}
	if err := attempt.close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"cancel", "tun", "multi-client", "pump", "generator"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("close order = %v, want %v", events, want)
	}
}

// A failure before the TUN or multi-client exists still retires the API
// generator; otherwise every failed trail attempt leaks its discovery core.
func TestTunnelAttemptCloseReleasesPartialConstruction(t *testing.T) {
	events := []string{}
	attempt := &tunnelAttempt{
		cancel: func() {
			events = append(events, "cancel")
		},
		generator: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			events = append(events, "generator")
			return nil
		}),
	}
	if err := attempt.close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"cancel", "generator"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("partial close order = %v, want %v", events, want)
	}
}
