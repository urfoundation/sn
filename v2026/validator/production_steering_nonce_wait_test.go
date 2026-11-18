//go:build linux || darwin

// The weight fallback signs with the validator's hotkey. Its transaction can
// take the nonce of the validator's signed pending commit, or sit at an earlier
// nonce the commit waits behind. These tests run the real steering loop and
// pending owner over the continuation fixture's persisted intent and canonical
// chain, with the node's pool answers decoded by the real gsrpc client. On
// 2026-11-08 ten such answers in a row ended the validator.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/gorilla/websocket"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The node's refusal of author_submitAndWatchExtrinsic, in its JSON-RPC wire
// shape, decoded by the real gsrpc client exactly as production receives it.
func productionPoolTestRefusal(t *testing.T, refusal map[string]any) error {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		var call struct {
			ID json.RawMessage `json:"id"`
		}
		if connection.ReadJSON(&call) == nil {
			_ = connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": refusal})
		}
	}))
	defer server.Close()
	client, err := gsrpcgeth.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.Subscribe(ctx, "author", "submitAndWatchExtrinsic", "unwatchExtrinsic", "extrinsicUpdate", make(chan types.ExtrinsicStatus), "0x0102")
	if err == nil {
		t.Fatal("the synthetic node admitted the extrinsic")
	}
	return err
}

// The pool's answers while the fallback's commit holds the validator's nonce:
// pooled first, then in a best block that is not final, after which the pool
// bans the exact bytes it dropped.
func productionPoolTestNonceTaken(t *testing.T) []error {
	t.Helper()
	return []error{
		productionPoolTestRefusal(t, map[string]any{"code": 1014, "message": "Priority is too low: (2147483648 vs 2147483648)", "data": "The transaction has too low priority to replace another transaction already in the pool."}),
		productionPoolTestRefusal(t, map[string]any{"code": 1010, "message": "Invalid Transaction", "data": "Transaction is outdated"}),
		productionPoolTestRefusal(t, map[string]any{"code": 1012, "message": "Transaction is temporarily banned"}),
	}
}

// The continuation fixture's persisted pending intent, its first broadcast's
// acknowledgement lost, signed at preparedNonce from system_accountNextIndex.
// The rebroadcast owner restores the intent's local duties before sending the
// same bytes again (restoreHeadEmaV2): it republishes the deposit audit and
// commits the head EMA. The fixture has no operator runtimes; these carry
// activations after the audited epoch, so the audit has nothing to publish,
// and a real bounded store takes the head EMA.
func newProductionNonceWaitFixture(t *testing.T, preparedNonce uint32) (*productionContinuationTestFixture, *productionContinuationNativeTestClient, *SteeringIntent) {
	t.Helper()
	fixture := newProductionContinuationTestFixtureWithStartup(t, nil, func(production *ownerRecycleProductionTestFixture, _ *productionContinuationTestFixture) {
		base := production.operator.measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
		previous := base.callContext
		base.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			if method == "system_accountNextIndex" {
				raw, err := json.Marshal(preparedNonce)
				if err != nil {
					return err
				}
				return json.Unmarshal(raw, target)
			}
			return previous(ctx, target, method, args...)
		}
	})
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	if pending.Prepared.AccountNonce != preparedNonce {
		t.Fatalf("the intent was signed at nonce %d, want %d", pending.Prepared.AccountNonce, preparedNonce)
	}
	fixture.runtime.runtimes = []*releaseOperatorRuntime{{measurement: &ReleaseMeasurementContext{NoID: 9}}, {measurement: &ReleaseMeasurementContext{NoID: 10}}}
	activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{Epoch: math.MaxUint32}}
	fixture.runtime.sources = map[uint64]*releaseAttemptUploadSourceV2{9: {activation: activation}, 10: {activation: activation}}
	ema, err := NewHeadEMAStoreV2(t.Context(), fixture.steerer.cfg.StateDir, headEMAStoreV2TestLimits())
	if err != nil {
		t.Fatal(err)
	}
	fixture.steerer.headEMA = ema
	fixture.runtime.progress = &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
	return fixture, native, pending
}

// Fresh preparation becomes reachable, and a stored native refusal for the
// failed intent marks the point past both chain snapshots and the retained
// intent's gates where the fresh preparation goes on to collect, decide and
// sign anew with a fresh nonce. The fixture has no attempt runtime to collect
// with; the existing successor path is unchanged.
func installProductionResignSentinel(fixture *productionContinuationTestFixture, failed *SteeringIntent, nativeEpoch uint64) {
	if fixture.runtime.preparation == nil {
		preparation := &releaseProductionPreparation{requested: make(chan struct{}), ready: make(chan struct{})}
		preparation.readyOnce.Do(func() { close(preparation.ready) })
		fixture.runtime.preparation = preparation
	}
	if fixture.runtime.nativeGapHeld == nil {
		fixture.runtime.nativeGapHeld = new(atomic.Pointer[productionNativeGapHold])
	}
	fixture.runtime.nativeGapHeld.Store(&productionNativeGapHold{lastEpoch: failed.SubnetEpoch, lastStatus: failed.Status, lastArtifactHash: failed.MeasurementArtifactHash, nativeEpoch: nativeEpoch})
}

// Polls of the real production loop over the real steering owner.
func runProductionNonceTestLoop(t *testing.T, fixture *productionContinuationTestFixture, polls int) error {
	t.Helper()
	seen := 0
	return runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
		return fixture.steerer.submitOnceV2(t.Context())
	}, func() bool {
		seen++
		return seen < polls
	}, fixture.runtime.progress, time.Now, releaseProductionCutWaitMinimumLimit)
}

// The fallback's commit holds the nonce of the validator's signed pending
// commit, pooled and then in a best block (the next index is past it), and the
// pool refuses every rebroadcast of its exact bytes, more times in a row than
// the failure limit. That is a wait, not ten hard errors and a panic. Once the
// fallback's commit is final, a different extrinsic consumed the nonce: the
// bytes can never land, the intent fails, and since the hotkey's row is not
// the decided one, the native epoch is prepared again, which signs with a
// fresh nonce.
func TestProductionFallbackNonceRefusalsAreAWaitNotAPanic(t *testing.T) {
	fixture, native, pending := newProductionNonceWaitFixture(t, 0)
	refusals := productionPoolTestNonceTaken(t)
	native.submitAnswer = func(count int) error { return refusals[count%len(refusals)] }
	native.nonceKVs[fixture.production.head] = pending.Prepared.AccountNonce
	native.nextIndex = func() uint32 { return pending.Prepared.AccountNonce + 1 }
	before, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	const polls = releaseSteeringFailureLimit + 2
	err = runProductionNonceTestLoop(t, fixture, polls)
	var limit *releaseSteeringFailureLimitError
	if errors.As(err, &limit) || err != nil {
		t.Fatalf("pool refusals of the pending bytes spent the steering failure budget (the 2026-11-08 panic): %v", err)
	}
	if len(native.broadcasts) != 1+polls || slices.ContainsFunc(native.broadcasts, func(sent string) bool { return sent != pending.Prepared.ExtrinsicHex }) {
		t.Fatalf("the wait did not rebroadcast exactly the original bytes each poll: %d sends", len(native.broadcasts))
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("a refused rebroadcast changed the pending intent: %v", err)
	}
	if steering := fixture.runtime.progress.value.Steering; steering == nil || steering.Outcome != "receipt_pending" || steering.NativeEpoch != pending.SubnetEpoch || !steering.EpochKnown {
		t.Fatalf("the wait was not reported as a pending receipt: %+v", steering)
	}
	// The fallback's commit is final at block 101: a different extrinsic took
	// the nonce, and the receipt scan through 101 holds no receipt of ours.
	fixture.production.head = 101
	native.nonceKVs[101] = pending.Prepared.AccountNonce + 1
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("the consumed nonce did not end the pending bytes: %v", err)
	}
	failed := fixture.restart(t)
	if failed.Status != "failed" || !strings.Contains(failed.Error, "consumed by a different finalized extrinsic by scanned block 101") || failed.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || len(native.broadcasts) != 1+polls {
		t.Fatalf("the consumed nonce was not recorded as the end of the original bytes: status=%s cause=%q sends=%d", failed.Status, failed.Error, len(native.broadcasts))
	}
	// The fallback's commit wrote the hotkey's weights at 101, but it has not
	// revealed: the row is still the older one, not the decided row.
	native.appliedAt = func(uint64) bool { return false }
	native.lastUpdateAt = func(number uint64) uint64 { return map[bool]uint64{true: 101, false: 90}[number > 100] }
	fixture.production.head = 102
	installProductionResignSentinel(fixture, failed, fixture.production.epoch)
	var resign *productionNativeGapHold
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &resign) {
		t.Fatalf("the failed native epoch was not prepared again with a fresh nonce: %v", err)
	}
	if len(native.broadcasts) != 1+polls {
		t.Fatal("the dead original bytes were sent again")
	}
}

// The validator signed at nonce 5 while the fallback's commit at nonce 4 was
// pooled but not final. Its bytes wait behind that commit: the finalized nonce
// lags the prepared one, with the commit still in the pool (next index 5), for
// more polls than the failure limit, which is a wait and sends nothing. When
// the fallback's commit is final the nonce is the validator's: the same bytes
// go out again, land, and finalize the intent.
func TestProductionNonceGapIsAWaitThenRecovers(t *testing.T) {
	fixture, native, pending := newProductionNonceWaitFixture(t, 5)
	native.nonceKVs[fixture.production.head] = 4
	native.nextIndex = func() uint32 { return 5 }
	const polls = releaseSteeringFailureLimit + 2
	err := runProductionNonceTestLoop(t, fixture, polls)
	var limit *releaseSteeringFailureLimitError
	if errors.As(err, &limit) || err != nil {
		t.Fatalf("a nonce gap spent the steering failure budget (the 2026-11-08 panic): %v", err)
	}
	if len(native.broadcasts) != 1 {
		t.Fatalf("bytes waiting behind an earlier nonce were rebroadcast: %d sends", len(native.broadcasts))
	}
	if steering := fixture.runtime.progress.value.Steering; steering == nil || steering.Outcome != "receipt_pending" || steering.NativeEpoch != pending.SubnetEpoch {
		t.Fatalf("the gap was not reported as a pending receipt: %+v", steering)
	}
	// The fallback's commit is final at 101; the next nonce is ours.
	fixture.production.head = 101
	native.nonceKVs[101] = 5
	var transport *productionSteeringReadWait
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transport) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex, pending.Prepared.ExtrinsicHex}) {
		t.Fatalf("the closed gap did not rebroadcast the original bytes: %d sends: %v", len(native.broadcasts), err)
	}
	// They land at 102.
	native.receiptNumber, fixture.production.head = 102, 103
	fixture.production.extrinsicsKVs = map[uint64][]string{102: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("the original bytes' receipt did not finalize the intent: %v", err)
	}
	finalized := fixture.restart(t)
	if finalized.Status != "finalized" || finalized.FinalizedBlock != 102 || finalized.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || finalized.CreatedAt != pending.CreatedAt || len(native.broadcasts) != 2 {
		t.Fatalf("the recovered intent changed its bytes or finality: status=%s block=%d sends=%d", finalized.Status, finalized.FinalizedBlock, len(native.broadcasts))
	}
}

// Still needed, at its boundaries. The fallback's commit took the nonce and is
// final. When its weights write is after the intent was prepared and the
// hotkey's row already equals the decided row, the validator does not sign the
// epoch again: every poll holds it, for longer than the failure limit, and
// nothing is sent. The next native epoch brings the next decision. A row that
// differs, or a matching row with no write after the preparation, is still
// needed and is prepared again.
func TestProductionFallbackEquivalentWeightsAreNotSignedAgain(t *testing.T) {
	fixture, native, pending := newProductionNonceWaitFixture(t, 0)
	fixture.production.head = 101
	native.nonceKVs[101] = pending.Prepared.AccountNonce + 1
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) {
		t.Fatalf("the consumed nonce did not end the pending bytes: %v", err)
	}
	failed := fixture.restart(t)
	if failed.Status != "failed" {
		t.Fatalf("the consumed nonce left the intent %s", failed.Status)
	}
	fixture.production.head = 102
	installProductionResignSentinel(fixture, failed, fixture.production.epoch)
	written := uint64(101)
	equal := true
	native.lastUpdateAt = func(number uint64) uint64 { return map[bool]uint64{true: written, false: 90}[number > 100] }
	native.appliedAt = func(uint64) bool { return equal }
	poll := func() error {
		t.Helper()
		return fixture.steerer.submitOnceV2(t.Context())
	}
	var resign *productionNativeGapHold
	if err := poll(); err == nil || errors.As(err, &resign) {
		t.Fatalf("the poll went on into the fresh preparation that signs the fallback's equivalent weights again: %v", err)
	}
	before, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	const polls = releaseSteeringFailureLimit + 2
	if err := runProductionNonceTestLoop(t, fixture, polls); err != nil {
		t.Fatalf("holding the epoch spent the failure budget: %v", err)
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(before, after) || len(native.broadcasts) != 1 {
		t.Fatalf("holding the epoch changed the intent or sent bytes: %v, %d sends", err, len(native.broadcasts))
	}
	if steering := fixture.runtime.progress.value.Steering; steering == nil || steering.Outcome != "epoch_wait" || steering.NativeEpoch != failed.SubnetEpoch || !steering.Current {
		t.Fatalf("the held epoch was not reported as waiting for the next epoch: %+v", steering)
	}
	for _, still := range []struct {
		name    string
		written uint64
		equal   bool
	}{
		{name: "a row that differs", written: 101, equal: false},
		{name: "no write after the preparation", written: 90, equal: true},
		{name: "a write at the preparation block", written: failed.Prepared.PreparedAtBlock, equal: true},
	} {
		written, equal = still.written, still.equal
		if err := poll(); !errors.As(err, &resign) {
			t.Fatalf("%s did not prepare the epoch again: %v", still.name, err)
		}
	}
	written, equal = 101, true
	fixture.production.epoch++
	installProductionResignSentinel(fixture, failed, fixture.production.epoch)
	if err := poll(); !errors.As(err, &resign) || resign.nativeEpoch != fixture.production.epoch {
		t.Fatalf("the next native epoch did not bring the next decision: %v", err)
	}
}

// A refusal that no nonce order explains is unchanged: the bytes cannot pay
// their fee, so each rebroadcast is a hard error and ten end the validator.
func TestProductionHardPoolRefusalStillSpendsTheBudget(t *testing.T) {
	fixture, native, pending := newProductionNonceWaitFixture(t, 0)
	payment := productionPoolTestRefusal(t, map[string]any{"code": 1010, "message": "Invalid Transaction", "data": "Inability to pay some fees (e.g. account balance too low)"})
	native.submitAnswer = func(int) error { return payment }
	native.nonceKVs[fixture.production.head] = pending.Prepared.AccountNonce
	err := runProductionNonceTestLoop(t, fixture, 3*releaseSteeringFailureLimit)
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || limit.failures != releaseSteeringFailureLimit || len(native.broadcasts) != 1+releaseSteeringFailureLimit {
		t.Fatalf("a genuinely hard pool refusal did not spend the failure budget: sends=%d err=%v", len(native.broadcasts), err)
	}
}

// What a node answers after that first refusal: it banned the invalid bytes,
// and each rebroadcast renews the ban, so every later answer is "temporarily
// banned". With nothing holding the nonce (the next index is still the
// prepared nonce) the ban is about the bytes alone; it stays hard, and ten
// still end the validator instead of a wait that never ends.
func TestProductionBannedBytesWithoutNonceEvidenceStayHard(t *testing.T) {
	fixture, native, pending := newProductionNonceWaitFixture(t, 0)
	payment := productionPoolTestRefusal(t, map[string]any{"code": 1010, "message": "Invalid Transaction", "data": "Inability to pay some fees (e.g. account balance too low)"})
	banned := productionPoolTestRefusal(t, map[string]any{"code": 1012, "message": "Transaction is temporarily banned"})
	native.submitAnswer = func(count int) error {
		if count == 2 {
			return payment
		}
		return banned
	}
	native.nonceKVs[fixture.production.head] = pending.Prepared.AccountNonce
	native.nextIndex = func() uint32 { return pending.Prepared.AccountNonce }
	err := runProductionNonceTestLoop(t, fixture, 3*releaseSteeringFailureLimit)
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || limit.failures != releaseSteeringFailureLimit || len(native.broadcasts) != 1+releaseSteeringFailureLimit {
		t.Fatalf("a ban on the bytes alone became an endless wait: sends=%d err=%v", len(native.broadcasts), err)
	}
}

// A gap with nothing at the earlier nonce, in the best chain or the pool (the
// next index is the finalized nonce), does not close by waiting; it stays hard.
func TestProductionNonceGapWithNothingPendingStaysHard(t *testing.T) {
	fixture, native, _ := newProductionNonceWaitFixture(t, 5)
	native.nonceKVs[fixture.production.head] = 4
	native.nextIndex = func() uint32 { return 4 }
	err := runProductionNonceTestLoop(t, fixture, 3*releaseSteeringFailureLimit)
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || limit.failures != releaseSteeringFailureLimit || len(native.broadcasts) != 1 || !strings.Contains(err.Error(), "steering nonce gap at scanned block 100: finalized 4, prepared 5") {
		t.Fatalf("a gap nothing can close did not spend the failure budget: sends=%d err=%v", len(native.broadcasts), err)
	}
}
