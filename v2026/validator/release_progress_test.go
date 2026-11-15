//go:build linux || darwin

// Progress tests use real custody and settlement owners; scalar-only cases
// exercise operational aging without supplying a protocol admission verdict.
package validator

import (
	"context"
	"errors"
	"math/big"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A controllable clock avoids timing-based freshness and restart assertions.
type releaseProgressTestClock struct{ nanos atomic.Int64 }

// Clock changes are safe while the real publisher is running.
func (self *releaseProgressTestClock) set(value time.Time) { self.nanos.Store(value.UnixNano()) }

// No observation clock depends on a sleep or scheduler timing.
func (self *releaseProgressTestClock) now() time.Time { return time.Unix(0, self.nanos.Load()).UTC() }

// Only the output wire uses a synthetic source; real operation fixtures retain
// their own independent protocol configuration and admission paths.
func newReleaseProgressTest(t testing.TB) (*releaseProgress, *releaseProgressTestClock) {
	t.Helper()
	clock := &releaseProgressTestClock{}
	clock.set(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	now := clock.now().Format(time.RFC3339Nano)
	progress := &releaseProgress{now: clock.now, value: protocol.ValidatorProgress{
		Schema: protocol.ValidatorProgressSchema, InstanceId: strings.Repeat("3", 32), StartedAt: now, HeartbeatAt: now,
		Publisher: protocol.ValidatorPublicationObservation{Outcome: "starting"},
		Source: protocol.ValidatorProgressSource{ConfigHash: "sha256:" + strings.Repeat("1", 64),
			DeploymentId: "synthetic-progress", ValidatorId: 1, ChainId: 964, GenesisHash: "0x" + strings.Repeat("2", 64), Netuid: 25},
	}}
	return progress, clock
}

// Decode the actual exported shape, not a mutable internal pointer.
func releaseProgressTestSnapshot(t testing.TB, progress *releaseProgress) *protocol.ValidatorProgress {
	t.Helper()
	raw, err := progress.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	value, err := protocol.DecodeValidatorProgress(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// The real Current path releases the physical directory and active owner
// before its first successful operational observation becomes visible.
func TestReleaseProgressIntentObservationFollowsRealCustodyRelease(t *testing.T) {
	store := intentV2PublicationTest(t)
	progress, clock := newReleaseProgressTest(t)
	store.v2.runtime.progress = progress
	closed := false
	store.v2.referenceReadHooks.afterClose = func(file *os.File) error {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("observer ran before physical close", err)
		}
		if !store.v2.active.Load() {
			t.Fatal("reference custody escaped the actual intent owner")
		}
		closed = true
		return nil
	}
	progress.now = func() time.Time {
		if store.v2.active.Load() || !closed {
			t.Fatal("progress published before custody and active-owner release")
		}
		return clock.now()
	}
	intent, err := store.currentV2(t.Context())
	if err != nil || intent != nil {
		t.Fatal("real empty Current failed", err)
	}
	value := releaseProgressTestSnapshot(t, progress)
	if value.Intent == nil || !value.Intent.Current || value.Intent.LastSuccessAt == "" || value.Intent.Value != nil {
		t.Fatal("real successful empty read was not observed")
	}
	if _, err := os.Lstat(store.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only observation manufactured a protocol file", err)
	}
}

// A late Close error remains a core error and cannot refresh successful
// evidence. The optional observation retains the last good result and age.
func TestReleaseProgressIntentCloseFailureRetainsLastSuccess(t *testing.T) {
	store := intentV2PublicationTest(t)
	progress, clock := newReleaseProgressTest(t)
	store.v2.runtime.progress = progress
	if _, err := store.currentV2(t.Context()); err != nil {
		t.Fatal(err)
	}
	previous := releaseProgressTestSnapshot(t, progress)
	if previous.Intent == nil || !previous.Intent.Current {
		t.Fatal("real successful Current produced no last-good observation")
	}
	clock.set(clock.now().Add(time.Minute))
	cause := errors.New("synthetic late custody close")
	closed := false
	store.v2.referenceReadHooks.afterClose = func(file *os.File) error {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("fault replaced real Close", err)
		}
		closed = true
		return cause
	}
	if intent, err := store.currentV2(t.Context()); !errors.Is(err, cause) || intent != nil || !closed || store.v2.active.Load() {
		t.Fatal("late close failure was lost or retained ownership", err)
	}
	value := releaseProgressTestSnapshot(t, progress)
	if value.Intent.Current || value.Intent.LastSuccessAt != previous.Intent.LastSuccessAt || value.Intent.ObservedAt == previous.Intent.ObservedAt {
		t.Fatal("failed read refreshed or erased good evidence")
	}
}

// Repeated successful reads and error updates keep the original pending age;
// a delayed older callback cannot replace a later completed observation.
func TestReleaseProgressUnchangedIntentAndDelayedCallbackKeepAge(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	created := clock.now().Add(-time.Hour).Format(time.RFC3339Nano)
	intent := &protocol.ValidatorIntentProgress{ConfigHash: progress.value.Source.ConfigHash,
		VectorHash: "0x" + strings.Repeat("4", 64), NativeEpoch: 8, SettlementEpoch: 7,
		Status: "pending", CreatedAt: created, ProgressAt: created, PreparedAtBlock: 100, RevealBlock: 120}
	progress.observeIntent(1, intent, nil)
	clock.set(clock.now().Add(time.Minute))
	intent.ProgressAt = clock.now().Format(time.RFC3339Nano)
	progress.observeIntent(3, intent, nil)
	progress.observeIntent(2, nil, errors.New("synthetic delayed failure"))
	value := releaseProgressTestSnapshot(t, progress)
	if !value.Intent.Current || value.Intent.Value.ProgressAt != created || value.Intent.Value.CreatedAt != created || value.Intent.LastSuccessAt != value.HeartbeatAt {
		t.Fatal("repeated or stale observation reset pending age")
	}
	clock.set(clock.now().Add(time.Minute))
	progress.observeIntent(4, nil, errors.New("synthetic current outage"))
	failed := releaseProgressTestSnapshot(t, progress)
	if failed.Intent.Current || failed.Intent.Value.ProgressAt != created || failed.Intent.LastSuccessAt != value.Intent.LastSuccessAt {
		t.Fatal("outage erased old intent or fabricated progress")
	}
}

// A genuine signed sidecar remains bound to its original configuration after
// approved renewal. This tests the scalar projection, not a successful durable
// begin/update, whose complete nonempty fixture belongs to the PH03 slice.
func TestReleaseProgressProjectsOriginalSignedIntentAuthorityAfterRenewal(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, measurement.admission.approval, measurement.admission.private, true)
	intent.Status, intent.CreatedAt, intent.UpdatedAt = "pending", "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z"
	var err error
	intent.VectorHash, err = intent.computeVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	value, err := releaseIntentProgress(current, intent)
	if err != nil {
		t.Fatal(err)
	}
	originalSource, err := releaseProgressSource(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	currentSource, err := releaseProgressSource(current)
	if err != nil {
		t.Fatal(err)
	}
	if value.ConfigHash != originalSource.ConfigHash || value.ConfigHash == currentSource.ConfigHash || value.ProgressAt != intent.CreatedAt ||
		value.VectorHash != intent.VectorHash || value.RevealBlock != intent.Prepared.RevealBlock || value.PreparedAtBlock != intent.Prepared.PreparedAtBlock {
		t.Fatal("projection reinterpreted an original signed intent under current authority")
	}
}

// A genuine closure/publication advances the cursor. Reconciliation at the
// same cursor and a cold disk restart preserve its original progress time.
func TestReleaseProgressSettlementUsesRealDurableTransitionsAndRestart(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	progress, clock := newReleaseProgressTest(t)
	fixture.runtime.progress = progress
	progress.observeSettlement(progress.nextSequence(), fixture.runtime.progressSettlement(9), nil)
	progress.now = func() time.Time {
		select {
		case fixture.runtime.gate <- struct{}{}:
			<-fixture.runtime.gate
		default:
			t.Fatal("settlement observation retained the protocol gate")
		}
		return clock.now()
	}
	clock.set(clock.now().Add(time.Minute))
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	first := releaseProgressTestSnapshot(t, progress)
	if first.Settlement == nil || !first.Settlement.Current || first.Settlement.Epoch != 9 || first.Settlement.PendingPublications != 0 || first.Settlement.ProgressAt != first.HeartbeatAt {
		t.Fatal("actual durable transition was not observed")
	}
	clock.set(clock.now().Add(time.Minute))
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	repeated := releaseProgressTestSnapshot(t, progress)
	if repeated.Settlement.ProgressAt != first.Settlement.ProgressAt || repeated.Settlement.LastSuccessAt == first.Settlement.LastSuccessAt {
		t.Fatal("successful reconciliation was confused with cursor progress")
	}
	fixture.startup.reopen(t)
	fixture.start(t)
	restarted, restartClock := newReleaseProgressTest(t)
	restartClock.set(clock.now().Add(time.Minute))
	restarted.retain(repeated)
	fixture.runtime.progress = restarted
	retained := releaseProgressTestSnapshot(t, restarted)
	if retained.Settlement.Current || retained.Settlement.ProgressAt != first.Settlement.ProgressAt {
		t.Fatal("restart converted retained evidence into current success")
	}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	after := releaseProgressTestSnapshot(t, restarted)
	if !after.Settlement.Current || after.Settlement.ProgressAt != first.Settlement.ProgressAt || after.Settlement.Epoch != 9 {
		t.Fatal("real disk reconciliation reset the progress age")
	}
}

// Unknown native reads and classified receipt waits are independent of a
// fresh process heartbeat and never manufacture native or intent progress.
func TestReleaseProgressReadOutageAndClockRollbackPreserveEvidence(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	progress.observeNative(&crv4.EpochScheduleState{CurrentBlock: 100, SubnetEpochIndex: 8, PendingEpochAt: 120}, nil)
	progress.observeSteering(8, true, "receipt_pending", true)
	before := releaseProgressTestSnapshot(t, progress)
	clock.set(clock.now().Add(-time.Minute))
	progress.observeNative(nil, errors.New("synthetic read outage"))
	progress.observeSteering(0, false, "read_wait", false)
	after := releaseProgressTestSnapshot(t, progress)
	if after.Native.Current || after.Native.Block != 100 || after.Native.LastSuccessAt != before.Native.LastSuccessAt || after.Steering.Current || after.Steering.EpochKnown || after.Steering.LastSuccessAt != before.Steering.LastSuccessAt || after.Intent != nil {
		t.Fatal("unavailable data or clock rollback replaced prior evidence")
	}
	if after.HeartbeatAt >= before.HeartbeatAt {
		t.Fatal("clock rollback was hidden from independent monitoring")
	}
}

// A failed fresh read may race ahead of the publisher's bounded restart read.
// That ordering must preserve prior success without promoting the failed read.
func TestReleaseProgressRestartReadAfterOutageRetainsEveryLastGoodDomain(t *testing.T) {
	prior, clock := newReleaseProgressTest(t)
	created := clock.now().Add(-time.Hour).Format(time.RFC3339Nano)
	prior.observeIntent(prior.nextSequence(), &protocol.ValidatorIntentProgress{ConfigHash: prior.value.Source.ConfigHash,
		VectorHash: "0x" + strings.Repeat("4", 64), Status: "pending", CreatedAt: created, ProgressAt: created}, nil)
	prior.observeNative(&crv4.EpochScheduleState{CurrentBlock: 100, SubnetEpochIndex: 8}, nil)
	prior.observeSettlement(prior.nextSequence(), &protocol.ValidatorSettlementObservation{CursorKnown: true, Epoch: 9, TargetEpoch: 9}, nil)
	prior.observeSettlement(prior.nextSequence(), &protocol.ValidatorSettlementObservation{CursorKnown: true, Epoch: 10, TargetEpoch: 10}, nil)
	prior.observeSteering(8, true, "receipt_pending", true)
	prior.observePublication(true)
	previous := releaseProgressTestSnapshot(t, prior)
	progress, nextClock := newReleaseProgressTest(t)
	nextClock.set(clock.now().Add(time.Minute))
	cause := errors.New("synthetic initial read outage")
	progress.observeIntent(progress.nextSequence(), nil, cause)
	progress.observeNative(nil, cause)
	progress.observeSettlement(progress.nextSequence(), nil, cause)
	progress.observeSteering(0, false, "read_wait", false)
	progress.retain(previous)
	after := releaseProgressTestSnapshot(t, progress)
	if after.Intent.Current || after.Intent.Value == nil || after.Intent.Value.CreatedAt != created || after.Intent.LastSuccessAt != previous.Intent.LastSuccessAt ||
		after.Native.Current || after.Native.Block != 100 || after.Native.LastSuccessAt != previous.Native.LastSuccessAt ||
		after.Settlement.Current || !after.Settlement.CursorKnown || after.Settlement.Epoch != 10 || after.Settlement.ProgressAt != previous.Settlement.ProgressAt || after.Settlement.LastSuccessAt != previous.Settlement.LastSuccessAt ||
		after.Steering.Current || after.Steering.Outcome != "read_wait" || after.Steering.EpochKnown || after.Steering.LastSuccessAt != previous.Steering.LastSuccessAt ||
		after.Publisher.Outcome != "starting" || after.Publisher.LastSuccessAt != previous.Publisher.LastSuccessAt {
		t.Fatal("initial outage won the load race by erasing old good evidence")
	}
	if after.Intent.ObservedAt != after.HeartbeatAt || after.Native.ObservedAt != after.HeartbeatAt || after.Settlement.ObservedAt != after.HeartbeatAt {
		t.Fatal("retention overwrote the new failed observation time")
	}
}

// Fresh known-empty intent evidence wins over old pending work. At an unchanged
// durable cursor, a prior progress age survives even if startup observed first.
func TestReleaseProgressLateRetentionDoesNotReplaceFreshSuccessfulObservation(t *testing.T) {
	prior, clock := newReleaseProgressTest(t)
	created := clock.now().Add(-time.Hour).Format(time.RFC3339Nano)
	prior.observeIntent(prior.nextSequence(), &protocol.ValidatorIntentProgress{ConfigHash: prior.value.Source.ConfigHash,
		VectorHash: "0x" + strings.Repeat("4", 64), Status: "pending", CreatedAt: created, ProgressAt: created}, nil)
	prior.observeSettlement(prior.nextSequence(), &protocol.ValidatorSettlementObservation{CursorKnown: true, Epoch: 9, TargetEpoch: 9}, nil)
	prior.observeSettlement(prior.nextSequence(), &protocol.ValidatorSettlementObservation{CursorKnown: true, Epoch: 10, TargetEpoch: 10}, nil)
	previous := releaseProgressTestSnapshot(t, prior)
	progress, nextClock := newReleaseProgressTest(t)
	nextClock.set(clock.now().Add(time.Minute))
	progress.observeIntent(progress.nextSequence(), nil, nil)
	progress.observeSettlement(progress.nextSequence(), &protocol.ValidatorSettlementObservation{CursorKnown: true, Epoch: 10, TargetEpoch: 10}, nil)
	progress.retain(previous)
	after := releaseProgressTestSnapshot(t, progress)
	if !after.Intent.Current || after.Intent.Value != nil || after.Intent.LastSuccessAt != after.HeartbeatAt ||
		!after.Settlement.Current || after.Settlement.LastSuccessAt != after.HeartbeatAt || after.Settlement.ProgressAt != previous.Settlement.ProgressAt {
		t.Fatal("late retention replaced fresh observations or erased same-cursor age")
	}
}

// A progress option enters the real release command without altering its
// canceled-lifecycle refusal or starting an independent operation.
func TestReleaseProgressCommandOptionPreservesCoreAdmission(t *testing.T) {
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=synthetic.yml", "--progress-file=/synthetic/progress.json"})
	if err != nil {
		t.Fatal(err)
	}
	if value, err := opts.String("--progress-file"); err != nil || value != "/synthetic/progress.json" {
		t.Fatal("real command parser lost progress option", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := RunReleaseWithProgress(ctx, "absent-synthetic.yml", "/synthetic/progress.json"); !errors.Is(err, context.Canceled) {
		t.Fatal("optional progress changed canceled core admission", err)
	}
}
