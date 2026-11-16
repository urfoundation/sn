package validator

import (
	"context"
	"errors"
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
		packetClient: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			record("packet-client")
			close(pumpRelease)
			return nil
		}),
		pumpDone: pumpDone,
		retireClient: func(context.Context) error {
			record("retire-client")
			return nil
		},
		generator: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			record("generator")
			return nil
		}),
	}
	attempt.addTransport(tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
		record("platform-transport")
		return nil
	}))
	if err := attempt.close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"cancel", "tun", "packet-client", "pump", "retire-client", "generator", "platform-transport"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("close order = %v, want %v", events, want)
	}
}

func TestTunnelAttemptCloseReportsIncompleteJoinAndContinuesRetirement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retired, generatorJoined, transportJoined := false, false, false
	retirementErr := errors.New("retirement failed")
	attempt := &tunnelAttempt{
		pumpDone: make(chan struct{}),
		retireClient: func(context.Context) error {
			retired = true
			return retirementErr
		},
		generator: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			generatorJoined = true
			return nil
		}),
	}
	attempt.addTransport(tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
		transportJoined = true
		return nil
	}))
	err := attempt.closeAndWait(ctx)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, retirementErr) {
		t.Fatalf("close error = %v, want pump cancellation and retirement failure", err)
	}
	if !retired || !generatorJoined || !transportJoined {
		t.Fatalf("cleanup stopped early: retired=%t generator=%t transport=%t", retired, generatorJoined, transportJoined)
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
		retireClient: func(context.Context) error {
			events = append(events, "retire-client")
			return nil
		},
		generator: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			events = append(events, "generator")
			return nil
		}),
	}
	if err := attempt.close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"cancel", "retire-client", "generator"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("partial close order = %v, want %v", events, want)
	}
}
