// Real service runs retain original custody through optional output faults.
// Barriers join actual readers and writers; no detached publisher supplies success.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Only deterministic fixture cadence is replaced; every journal and port is real.
func rootOutputTestRun(t *testing.T, owner *rootServiceOwner, steps uint32, writer io.Writer) *rootServiceOutput {
	t.Helper()
	output, err := newRootServiceOutput(t.Context(), writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(t.Context(), steps, time.Second, output); err != nil {
		t.Fatal("run failed")
	}
	return output
}

// Full, blocked and disconnected logs do not prevent the existing approved
// action from retaining one signature, one broadcast and its original receipt.
func TestRootOutputRunFaultsPreserveOriginalActionAndRestart(t *testing.T) {
	for _, mode := range []string{"blocked", "full", "disconnected"} {
		fixture := newRootServiceFixture(t)
		owner, store := fixture.open(t, true, fixture.ports())
		var sink io.Writer
		var entered, left chan struct{}
		switch mode {
		case "blocked":
			entered, left = make(chan struct{}), make(chan struct{})
			sink = &monitorBlockedOutput{entered: entered, left: left}
		case "full":
			sink = bytes.NewBuffer(make([]byte, 1024*1024))
		case "disconnected":
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { writer.Close() })
			sink = writer
		}
		waits := 0
		owner.wait = func(ctx context.Context, _ time.Duration) bool {
			waits++
			if waits == 1 && entered != nil {
				select {
				case <-entered:
				case <-ctx.Done():
					return false
				}
			}
			return ctx.Err() == nil
		}
		output := rootOutputTestRun(t, owner, 3, sink)
		if left != nil {
			<-left
		}
		state := output.snapshot()
		if state.Delivered != 0 || state.Dropped == 0 {
			t.Fatal("optional output fault disappeared")
		}
		original, err := store.load()
		if err != nil || original.Phase != "active" || original.Action.Phase != "pending" || original.Action.Broadcasts != 1 || original.Action.RawExtrinsic == "" || fixture.signer.signs != 1 || len(fixture.submitter.intents) != 1 {
			t.Fatal("output fault changed independent action progress")
		}
		fixture.finalize(t, store, 10)
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		owner, store = fixture.open(t, false, fixture.ports())
		rootOutputTestRun(t, owner, 1, nil)
		completed, err := store.load()
		if err != nil || completed.Phase != "complete" || completed.Action.RawExtrinsic != original.Action.RawExtrinsic || completed.Action.Signature != original.Action.Signature || completed.Action.Broadcasts != 1 || completed.Observations != 1 {
			t.Fatal("restart replaced original custody or action allowance")
		}
		rootOutputTestRun(t, owner, 1, nil)
		if fixture.signer.signs != 1 || len(fixture.submitter.intents) != 1 || fixture.observer.calls != 1 {
			t.Fatal("completed action was replayed by output recovery")
		}
	}
}

// The formatting probe retains identity and lets a causal root fail normally,
// so one forbidden call cannot abort the remaining independent controls.
type rootOutputFormattingProbe struct{ calls atomic.Uint64 }

// Only an unintended diagnostic conversion increments this counter.
func (self *rootOutputFormattingProbe) Error() string {
	self.calls.Add(1)
	return "synthetic read cause"
}

// Existing command fixtures retain their independent no-formatting sentinel.
type rootOutputUnformattableError struct{}

// Those unchanged command controls do not restore the Run formatter mutation.
func (*rootOutputUnformattableError) Error() string { panic("root output formatted arbitrary error") }

// A fault around the actual durable decision commit retains ambiguity exactly.
type rootOutputFaultStore struct {
	store  rootServiceStorage
	cause  error
	commit bool
	cancel context.CancelFunc
	saves  int
	failAt int
}

// All reads still use the physical authenticated journal.
func (self *rootOutputFaultStore) load() (rootServiceRecord, error) { return self.store.load() }

// The selected write is the existing atomic decision or signing-intent boundary.
func (self *rootOutputFaultStore) save(value rootServiceRecord) error {
	self.saves++
	if self.saves != self.failAt {
		return self.store.save(value)
	}
	if self.commit {
		if err := self.store.save(value); err != nil {
			return err
		}
	}
	if self.cancel != nil {
		self.cancel()
	}
	return self.cause
}

// Hard custody causes survive optional output and simultaneous cancellation;
// neither before-commit nor after-commit uncertainty creates an extra signature.
func TestRootOutputRunRetainsHardCustodyCause(t *testing.T) {
	for _, failAt := range []int{2, 3} {
		for _, commit := range []bool{false, true} {
			for _, canceled := range []bool{false, true} {
				fixture := newRootServiceFixture(t)
				_, store := fixture.open(t, true, fixture.ports())
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cause := errors.New("synthetic required custody failure")
				fault := &rootOutputFaultStore{store: store, cause: cause, commit: commit, failAt: failAt}
				if canceled {
					fault.cancel = cancel
				}
				owner, err := newRootServiceOwner(fixture.config, fault, fixture.ports())
				if err != nil {
					t.Fatal(err)
				}
				owner.wait = func(context.Context, time.Duration) bool {
					if fault.saves >= failAt {
						t.Error("hard custody failure entered retry cadence")
					}
					return true
				}
				output, err := newRootServiceOutput(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				err = owner.Run(ctx, 5, time.Second, output)
				if !errors.Is(err, cause) || canceled && !errors.Is(err, context.Canceled) || fault.saves != failAt || fixture.signer.signs != 0 || len(fixture.submitter.intents) != 0 {
					t.Fatal("original custody failure was softened or replaced")
				}
				record, err := store.load()
				if err != nil || (record.Phase == "active") != (commit || failAt == 3) || record.Action.RawExtrinsic != "" || record.Action.Broadcasts != 0 {
					t.Fatal("output altered ambiguous durable state")
				}
			}
		}
	}
}

// A real failed observation reaches the supervisor, while its arbitrary Error
// method never runs inside either publication or the supervisor itself.
func TestRootOutputRunNeverFormatsReadCause(t *testing.T) {
	fixture := newRootServiceFixture(t)
	cause := &rootOutputFormattingProbe{}
	fixture.observer.err = cause
	owner, _ := fixture.open(t, true, fixture.ports())
	output, err := newRootServiceOutput(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = owner.Run(t.Context(), 1, time.Second, output)
	if cause.calls.Load() != 0 {
		t.Fatal("root output formatted arbitrary error")
	}
	if !errors.Is(err, cause) || fixture.observer.calls != 1 || fixture.signer.signs != 0 {
		t.Fatal("read error identity or bounded work changed")
	}
}

// Invalid run arguments still consume and join an already-admitted exporter.
func TestRootOutputRunEarlyReturnJoinsOwnedWriter(t *testing.T) {
	fixture := newRootServiceFixture(t)
	owner, _ := fixture.open(t, true, fixture.ports())
	sink := &monitorBlockedOutput{entered: make(chan struct{}), left: make(chan struct{})}
	output, err := newRootServiceOutput(t.Context(), sink)
	if err != nil {
		t.Fatal(err)
	}
	output.offer(rootServiceEvent{Status: "pending"}, false)
	<-sink.entered
	if err := owner.Run(t.Context(), 0, time.Second, output); err == nil {
		t.Fatal("invalid run admitted")
	}
	<-sink.left
	if output.exporter.Offer("root", []byte("late")) {
		t.Fatal("early return left diagnostic owner live")
	}
	if fixture.observer.calls != 0 || fixture.signer.signs != 0 {
		t.Fatal("invalid run consumed action work")
	}
}
