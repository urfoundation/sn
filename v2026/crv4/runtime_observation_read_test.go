// Actual public readers repeat complete pinned views after a transport change;
// shared deadline, hard evidence and original caller policy remain unchanged.
package crv4

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestRuntimeObservationScheduleLateReplacementRestartsWholeQuery(t *testing.T) {
	fixture, query := newValidatorScheduleTestFixture(t)
	identity := fixture.identity
	client := newRuntimeTransportTestClient(identity.chain.API.Client)
	identity.chain.API.Client = client
	metadata, runtime := identity.chain.Meta, identity.chain.Runtime
	epochReads, waits := 0, 0
	identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && identity.keyNames[args[0].(string)] == "SubnetEpochIndex" {
			epochReads++
			if epochReads == 1 {
				client.generation.Add(1)
			}
		}
	}
	var budgets []time.Duration
	identity.chain.runtimeObservationRead.withTimeout = func(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
		budgets = append(budgets, budget)
		return context.WithTimeout(ctx, budget)
	}
	peer, peerQuery := newValidatorScheduleTestFixture(t)
	identity.chain.runtimeObservationRead.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits != 1 {
			return errors.New("unexpected repeated schedule expiry")
		}
		observed, err := ReadValidatorScheduleAtContext(peer.identity.ctx, peer.identity.chain, peerQuery, peer.identity.allowed...)
		if err != nil || observed.SubnetEpochIndex != 77 {
			t.Fatalf("independent schedule peer stopped: %+v %v", observed, err)
		}
		return ctx.Err()
	}
	observed, err := ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
	if err != nil || waits != 1 || epochReads != 2 || fixture.runtimeCalls != 2 || observed.Stake.Identity.BlockHash != query.BlockHash || observed.SubnetEpochIndex != 77 ||
		!slices.Equal(budgets, []time.Duration{300 * time.Second}) || identity.chain.Meta != metadata || identity.chain.Runtime != runtime {
		t.Fatalf("late schedule reconnect did not repeat one complete owned query: epochs=%d stake=%d waits=%d budgets=%v observed=%+v err=%v", epochReads, fixture.runtimeCalls, waits, budgets, observed, err)
	}
}

func TestRuntimeObservationCheckpointLateReplacementRepeatsParentAndChild(t *testing.T) {
	fixture, query, _ := newEVMCheckpointTestFixture(t)
	client := newRuntimeTransportTestClient(fixture.chain.API.Client)
	fixture.chain.API.Client = client
	prior := fixture.hook
	parent := fixture.headers[query.NativeHash.Hex()].ParentHash
	parentReads, waits := 0, 0
	fixture.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		if method == "state_getStorage" && args[1] == parent.Hex() {
			parentReads++
			if parentReads == 1 {
				client.generation.Add(1)
			}
		}
		return prior(ctx, result, method, args...)
	}
	fixture.chain.runtimeObservationRead.wait = func(context.Context, time.Duration) error { waits++; return nil }
	observed, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
	if err != nil || waits != 1 || parentReads != 2 || len(fixture.storageCalls) != 4 || observed.Query != query || observed.NativeParentHash != parent {
		t.Fatalf("checkpoint did not repeat complete original parent/child evidence: waits=%d parents=%d storage=%v observed=%+v err=%v", waits, parentReads, fixture.storageCalls, observed, err)
	}
}

func TestRuntimeObservationCensusDiscardsExpiredProjection(t *testing.T) {
	fixture := newValidatorStakeTestFixture(t)
	identity := fixture.identity
	client := newRuntimeTransportTestClient(identity.chain.API.Client)
	identity.chain.API.Client = client
	runtimeReads, waits := 0, 0
	prior := identity.hook
	identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		if method == "state_call" {
			runtimeReads++
			if runtimeReads == 1 {
				client.generation.Add(1)
			}
		}
		return prior(ctx, result, method, args...)
	}
	identity.chain.runtimeObservationRead.wait = func(context.Context, time.Duration) error { waits++; return nil }
	observed, err := ReadValidatorStakeCensusAtContext(identity.ctx, identity.chain, identity.query, identity.allowed...)
	if err != nil || waits != 1 || runtimeReads != 2 || len(observed.Entries) != int(observed.Selected.Identity.SubnetUIDs) || observed.Selected.TotalStakeRao != 150 {
		t.Fatalf("census did not discard and repeat expired projection: waits=%d reads=%d observed=%+v err=%v", waits, runtimeReads, observed, err)
	}
}

func TestRuntimeObservationWaitCancellationJoinsOriginalOwner(t *testing.T) {
	fixture, query := newValidatorScheduleTestFixture(t)
	identity := fixture.identity
	ctx, cancel := context.WithCancel(identity.ctx)
	defer cancel()
	identity.ctx = ctx
	client := newRuntimeTransportTestClient(identity.chain.API.Client)
	identity.chain.API.Client = client
	identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && identity.keyNames[args[0].(string)] == "SubnetEpochIndex" {
			client.generation.Add(1)
		}
	}
	waits, callsAtWait := 0, 0
	var owned context.Context
	identity.chain.runtimeObservationRead.wait = func(ctx context.Context, _ time.Duration) error {
		owned = ctx
		waits++
		callsAtWait = len(identity.calls)
		cancel()
		return ctx.Err()
	}
	observed, err := ReadValidatorScheduleAtContext(ctx, identity.chain, query, identity.allowed...)
	if observed != (ValidatorScheduleObservation{}) || !errors.Is(err, context.Canceled) || runtimeObservationMayRepeat(err) || waits != 1 || len(identity.calls) != callsAtWait || owned == nil || owned.Err() != context.Canceled {
		t.Fatalf("canceled reobservation lost owner or published partial schedule: waits=%d calls=%d/%d observed=%+v err=%v", waits, callsAtWait, len(identity.calls), observed, err)
	}
}

func TestRuntimeObservationDeadlineAndOriginalAllowlistDoNotRenew(t *testing.T) {
	fixture, query := newValidatorScheduleTestFixture(t)
	identity := fixture.identity
	ctx, cancel := context.WithTimeout(identity.ctx, 75*time.Second)
	defer cancel()
	identity.ctx = ctx
	parentDeadline, _ := ctx.Deadline()
	client := newRuntimeTransportTestClient(identity.chain.API.Client)
	identity.chain.API.Client = client
	identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && identity.keyNames[args[0].(string)] == "SubnetEpochIndex" {
			client.generation.Add(1)
		}
	}
	allowed := append([]RuntimeArtifactIdentity(nil), identity.allowed...)
	budgets, waits := 0, 0
	identity.chain.runtimeObservationRead.withTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		budgets++
		if duration != 300*time.Second {
			t.Fatal("whole-read owner lost its default total")
		}
		return context.WithTimeout(ctx, duration)
	}
	identity.chain.runtimeObservationRead.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		deadline, ok := ctx.Deadline()
		if !ok || deadline != parentDeadline {
			t.Fatal("reobservation extended original parent deadline")
		}
		allowed[0].Version.SpecVersion++
		if waits == 1 {
			return nil
		}
		return context.DeadlineExceeded
	}
	observed, err := ReadValidatorScheduleAtContext(ctx, identity.chain, query, allowed...)
	if observed != (ValidatorScheduleObservation{}) || !errors.Is(err, context.DeadlineExceeded) || waits != 2 || budgets != 1 || fixture.runtimeCalls != 2 {
		t.Fatalf("reobservation replaced original policy or renewed its total budget: waits=%d budgets=%d reads=%d err=%v", waits, budgets, fixture.runtimeCalls, err)
	}
}

func TestRuntimeObservationActualContradictionDominatesLateReplacement(t *testing.T) {
	fixture, query := newValidatorScheduleTestFixture(t)
	identity := fixture.identity
	client := newRuntimeTransportTestClient(identity.chain.API.Client)
	identity.chain.API.Client = client
	identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && identity.keyNames[args[0].(string)] == "SubnetEpochIndex" {
			client.generation.Add(1)
			changed := identity.blockHashes[query.BlockNumber]
			changed[0] ^= 1
			identity.blockHashes[query.BlockNumber] = changed
		}
	}
	waits := 0
	identity.chain.runtimeObservationRead.wait = func(context.Context, time.Duration) error {
		waits++
		return errors.New("unexpected contradiction retry")
	}
	observed, err := ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
	if err == nil || runtimeObservationMayRepeat(err) || waits != 0 || observed != (ValidatorScheduleObservation{}) {
		t.Fatalf("returned canonical contradiction became reconnect retry: waits=%d observed=%+v err=%v", waits, observed, err)
	}
}

// Custom Is/As are not consulted by this private cause policy.
type runtimeObservationCauseTest struct{ causes []error }

func (self *runtimeObservationCauseTest) Error() string   { return "synthetic observation cause" }
func (self *runtimeObservationCauseTest) Unwrap() []error { return self.causes }
func (self *runtimeObservationCauseTest) Is(error) bool   { panic("unexpected custom Is") }
func (self *runtimeObservationCauseTest) As(any) bool     { panic("unexpected custom As") }

func TestRuntimeObservationRetryRequiresBoundedPureExpiry(t *testing.T) {
	expired := &runtimeTransportObservationError{}
	cycle := &runtimeObservationCauseTest{}
	cycle.causes = []error{cycle}
	wide := &runtimeObservationCauseTest{causes: make([]error, 129)}
	for index := range wide.causes {
		wide.causes[index] = expired
	}
	var deep error = expired
	for index := 0; index < 34; index++ {
		deep = &runtimeObservationCauseTest{causes: []error{deep}}
	}
	for _, err := range []error{nil, cycle, wide, deep, &runtimeObservationCauseTest{causes: []error{nil, nil}}, errors.Join(expired, errors.New("actual identity conflict")), errors.Join(expired, context.Canceled)} {
		if runtimeObservationMayRepeat(err) {
			t.Fatalf("incomplete or hard graph acquired observation retry: %T", err)
		}
	}
	if !runtimeObservationMayRepeat(&runtimeObservationCauseTest{causes: []error{expired, expired}}) {
		t.Fatal("complete pure expiry lost retry")
	}
}

// A nonnil error interface can still carry no error value. It must not acquire
// authority merely because its concrete type has retry methods.
func TestRuntimeObservationTypedNilExpiryCannotAuthorizeRetry(t *testing.T) {
	var absent *runtimeTransportObservationError
	if runtimeObservationMayRepeat(absent) || runtimeObservationMayRepeat(errors.Join(&runtimeTransportObservationError{}, absent)) {
		t.Fatal("typed nil expiry acquired retry authority")
	}
}

// The nil receiver deliberately panics on Unwrap; rejecting it must happen
// before any custom method is invoked, including nested joined causes.
func TestRuntimeObservationTypedNilWrapperDoesNotDispatch(t *testing.T) {
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("typed nil wrapper dispatched a custom method: %v", value)
		}
	}()
	var absent *runtimeObservationCauseTest
	if runtimeObservationMayRepeat(absent) || runtimeObservationMayRepeat(errors.Join(&runtimeTransportObservationError{}, absent)) {
		t.Fatal("typed nil wrapper acquired retry authority")
	}
}
