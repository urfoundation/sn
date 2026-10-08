// Real original pending bytes and actual canonical empty block scans produce
// the wait result. Deterministic loop callbacks then test lifecycle ownership.
package validator

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The fixture returns no transaction from actual block bodies; it does not
// replace the receipt finder or fabricate inclusion/expiry/dispatch success.
func productionAuthorityPendingTest(t *testing.T, changeRuntime bool) (*ReleaseSteerer, *SteeringIntent, *ownerRecycleProductionTestFixture) {
	t.Helper()
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	intent.Status = "pending"
	measurement := fixture.operator.measurement
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, measurement.admission.approval, measurement.admission.private, changeRuntime)
	if changeRuntime {
		productionAuthorityTestUpgrade(t, fixture, current)
	} else {
		fixture.head = 101
	}
	client := measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	public := fixture.hotkey.PublicKey()
	accountKey, err := types.CreateStorageKey(fixture.metadata, "System", "Account", public[:])
	if err != nil {
		t.Fatal(err)
	}
	// The reviewed Subtensor account uses four u32 counters, three u64
	// balances and u128 flags. Preserve the original pending nonce exactly.
	account := make([]byte, 4*4+3*8+16)
	binary.LittleEndian.PutUint32(account, intent.Prepared.AccountNonce)
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "state_getStorage" && len(args) == 2 && args[0] == accountKey.Hex() {
			if args[1] != fixture.block(101).Hex() {
				t.Fatal("original pending nonce escaped the authenticated receipt coverage boundary")
			}
			raw, err := json.Marshal(codec.HexEncodeToString(account))
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, target)
		}
		if method == "chain_getBlock" {
			if len(args) != 1 || args[0] != fixture.block(100).Hex() && args[0] != fixture.block(101).Hex() {
				t.Fatal("original receipt scan escaped the retained finalized range")
			}
			number := uint64(100)
			if args[0] == fixture.block(101).Hex() {
				number = 101
			}
			raw, err := json.Marshal(map[string]any{"block": map[string]any{"header": releaseReceiptTestHeaderWire(fixture.header(number)), "extrinsics": []string{}}})
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, target)
		}
		if strings.HasPrefix(method, "author_") {
			t.Fatal("old pending authority attempted rebroadcast")
		}
		return original(ctx, target, method, args...)
	}
	return &ReleaseSteerer{cfg: current, native: measurement.admission.chain, hotkey: fixture.hotkey,
		productionReadHooks: releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error { return context.DeadlineExceeded }}}, intent, fixture
}

// Both a same-artifact renewal and an upgraded artifact keep the actual receipt
// polling alive beyond the fatal retry ceiling, without completing the epoch.
func TestProductionAuthorityHistoryPendingWaitKeepsLoopAlive(t *testing.T) {
	for _, changeRuntime := range []bool{false, true} {
		steerer, intent, _ := productionAuthorityPendingTest(t, changeRuntime)
		originalHash, originalPrepared := intent.Prepared.ExtrinsicHash, intent.Prepared
		attempts := 0
		err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) { return intent.SubnetEpoch, nil }, func() error {
			attempts++
			resolved, err := steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: intent.SubnetEpoch})
			var pending *productionPendingReconciliation
			if resolved || !errors.As(err, &pending) || pending.nativeEpoch != intent.SubnetEpoch || pending.extrinsicHash != originalHash {
				t.Fatalf("retained original lost receipt-only wait: resolved=%t error=%v", resolved, err)
			}
			if !strings.Contains(err.Error(), originalHash) || !strings.Contains(err.Error(), "remains pending") || !strings.Contains(err.Error(), "without rebroadcast") {
				t.Fatal("pending observation lost its transaction identity or visible wait reason")
			}
			return err
		}, func() bool { return attempts < releaseSteeringFailureLimit+3 })
		if err != nil || attempts != releaseSteeringFailureLimit+3 || intent.Status != "pending" || intent.Prepared != originalPrepared || intent.Prepared.ExtrinsicHash != originalHash {
			t.Fatalf("renewal wait spent failures, suppressed polling or changed signed bytes: upgraded=%t attempts=%d error=%v", changeRuntime, attempts, err)
		}
	}
}

// Crossing a boundary invokes reconciliation again; it cannot infer success
// from the old wait. Its next real error remains observable to the owner.
func TestProductionAuthorityHistoryPendingWaitReconcilesNextEpoch(t *testing.T) {
	steerer, intent, _ := productionAuthorityPendingTest(t, false)
	_, pending := steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: intent.SubnetEpoch})
	var typed *productionPendingReconciliation
	if !errors.As(pending, &typed) {
		t.Fatal(pending)
	}
	reads, attempts := 0, 0
	canary := errors.New("synthetic next epoch reconciliation still needs receipt evidence")
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) {
		reads++
		if reads > 2 {
			return intent.SubnetEpoch + 1, nil
		}
		return intent.SubnetEpoch, nil
	}, func() error {
		attempts++
		if attempts > 2 {
			return canary
		}
		return pending
	}, func() bool { return reads < 3 })
	if !errors.Is(err, canary) || reads != 3 || attempts != 3 || intent.Status != "pending" {
		t.Fatalf("pending boundary invented completion or prevented reconciliation: reads=%d attempts=%d error=%v", reads, attempts, err)
	}
}

// A typed wait is not a wildcard. Mixed integrity, wrong epoch, prior hard
// causes and cancellation retain the loop's original failure/owner semantics.
func TestProductionAuthorityHistoryPendingWaitPreservesHardFailures(t *testing.T) {
	steerer, intent, _ := productionAuthorityPendingTest(t, false)
	_, pending := steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: intent.SubnetEpoch})
	var typed *productionPendingReconciliation
	if !errors.As(pending, &typed) {
		t.Fatal(pending)
	}
	broken := errors.New("synthetic unrelated durable integrity failure")
	for _, fault := range []string{"joined", "wrong-epoch", "prior"} {
		attempts := 0
		epoch := intent.SubnetEpoch
		if fault == "wrong-epoch" {
			epoch++
		}
		err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) { return epoch, nil }, func() error {
			attempts++
			if fault == "joined" {
				return errors.Join(pending, broken)
			}
			if fault == "wrong-epoch" || fault == "prior" && attempts > 1 && attempts <= 4 {
				return pending
			}
			return broken
		}, func() bool { return attempts < releaseSteeringFailureLimit+5 })
		want := releaseSteeringFailureLimit
		if fault == "prior" {
			want += 3
		}
		if err == nil || !strings.Contains(err.Error(), "10 consecutive attempts") || attempts != want || fault != "wrong-epoch" && !errors.Is(err, broken) {
			t.Fatalf("%s wait lost hard failure accounting: attempts=%d error=%v", fault, attempts, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads, attempts := 0, 0
	err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) { reads++; return intent.SubnetEpoch, nil }, func() error { attempts++; return pending }, func() bool { cancel(); return true })
	if err != nil || reads != 1 || attempts != 1 {
		t.Fatalf("cancelled wait admitted another operation: reads=%d attempts=%d error=%v", reads, attempts, err)
	}
}

// A real receipt timeout after a successful empty scan is still unresolved
// observation. A later epoch must retry that scan rather than fabricate expiry,
// spend the failure budget or kill the independently running proof workers.
func TestProductionAuthorityHistoryPendingReceiptTimeoutKeepsObservation(t *testing.T) {
	steerer, intent, fixture := productionAuthorityPendingTest(t, false)
	client := steerer.native.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	scanErr := error(nil)
	faultReads := 0
	cachedBodyReads := 0
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "chain_getBlock" && scanErr == nil {
			cachedBodyReads++
		}
		if method == "chain_getBlock" && scanErr != nil {
			if len(args) != 1 || args[0] != fixture.block(102).Hex() {
				t.Fatal("receipt fault was not reached at the new uncached canonical block")
			}
			faultReads++
			return scanErr
		}
		return original(ctx, target, method, args...)
	}
	reads, attempts := 0, 0
	currentEpoch := intent.SubnetEpoch
	originalHash := intent.Prepared.ExtrinsicHash
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) {
		reads++
		if reads > 2 {
			currentEpoch = intent.SubnetEpoch + 1
		}
		return currentEpoch, nil
	}, func() error {
		attempts++
		if attempts > 1 {
			// The original complete absence through101 is intentionally reused.
			// Only new canonical evidence can exercise a later body outage.
			fixture.head = 102
			scanErr = context.DeadlineExceeded
		}
		resolved, err := steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: currentEpoch})
		var pending *productionPendingReconciliation
		if resolved || !errors.As(err, &pending) || pending.nativeEpoch != currentEpoch || attempts > 1 && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("actual interrupted receipt scan lost its wait/cause: attempts=%d error=%v", attempts, err)
		}
		return err
	}, func() bool { return reads < releaseSteeringFailureLimit+3 })
	if err != nil || reads != releaseSteeringFailureLimit+3 || attempts != reads || faultReads < attempts-1 || cachedBodyReads != 2 || intent.Status != "pending" || intent.Prepared.ExtrinsicHash != originalHash {
		t.Fatalf("receipt outage lost original progress across boundary: reads=%d attempts=%d error=%v", reads, attempts, err)
	}
	broken := errors.New("synthetic receipt integrity failure")
	for _, cause := range []error{errors.Join(context.DeadlineExceeded, broken), context.Canceled} {
		scanErr = cause
		before := faultReads
		_, err := steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: currentEpoch})
		var pending *productionPendingReconciliation
		if !errors.Is(err, cause) || errors.As(err, &pending) || faultReads != before+1 {
			t.Fatalf("receipt wait masked independent failure/cancellation: %v", err)
		}
	}
}
