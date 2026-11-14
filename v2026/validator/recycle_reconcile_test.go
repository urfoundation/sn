//go:build linux || darwin

// Approval admission must fence fresh pending replay while leaving exact
// historical receipt verification under its original authority unchanged.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Both intent owners may search for the original transaction, but an absent
// receipt cannot turn this admission-only feature into new signing authority.
func TestOwnerRecycleMainnetPendingReplayRefusesBothIntentOwners(t *testing.T) {
	for _, compact := range []bool{false, true} {
		native := newReleaseNativeValidatorTestFixture(t)
		installReleaseHistoricalTestNative(t, native)
		original := native.chain.API.Client
		blockReads, sendCalls := 0, 0
		native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
			if strings.HasPrefix(method, "author_") {
				sendCalls++
				return errors.New("synthetic pending replay attempted a write")
			}
			if method == "chain_getBlock" {
				if len(args) != 1 || args[0] != native.block.Hex() {
					return errors.New("synthetic pending scan changed original hash")
				}
				blockReads++
				return setReleaseHistoricalTestResult(result, map[string]any{"block": map[string]any{"header": releaseReceiptTestHeaderWire(native.header), "extrinsics": []string{}}})
			}
			return original.CallContext(ctx, result, method, args...)
		}}
		prepared := testPreparedSubmission(t, 1, []uint16{1, 2})
		prepared.PreparedAtBlock, prepared.PreparedAtBlockHash = native.blockNumber, native.block.Hex()
		before, err := json.Marshal(prepared)
		if err != nil {
			t.Fatal(err)
		}
		cfg := runtime461ValidatorTestConfig()
		// Selecting mainnet cannot grant an old pending fixture a new send.
		cfg.ChainID = 964
		steerer := &ReleaseSteerer{cfg: &cfg, native: native.chain}
		intent := &SteeringIntent{Status: "pending", Prepared: prepared, SubnetEpoch: 1}
		metadata, runtime := native.chain.Meta, native.chain.Runtime
		if compact {
			_, err = steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1})
		} else {
			_, err = steerer.reconcilePending(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1})
		}
		if err == nil || !strings.Contains(err.Error(), "owner-recycle successor activation is blocked") || blockReads != 1 || sendCalls != 0 {
			t.Fatalf("compact=%v pending boundary: blocks=%d sends=%d error=%v", compact, blockReads, sendCalls, err)
		}
		after, err := json.Marshal(intent.Prepared)
		if err != nil || !bytes.Equal(before, after) || intent.Status != "pending" || native.chain.Meta != metadata || native.chain.Runtime != runtime {
			t.Fatalf("compact=%v refused replay changed original authority: %v", compact, err)
		}
	}
}

// The fence does not erase an independently verified original finalized
// receipt. This fixture proves legacy receipt custody, not mainnet activation.
func TestOwnerRecycleAdmissionPreservesOriginalReceiptRecovery(t *testing.T) {
	steerer, pending, receipt := releaseHistoricalReconcileLegacyPending(t)
	steerer.cfg.ChainID = 964
	before, err := json.Marshal(pending.Prepared)
	if err != nil {
		t.Fatal(err)
	}
	done, err := steerer.reconcilePending(t.Context(), pending, &crv4.EpochScheduleState{SubnetEpochIndex: pending.SubnetEpoch})
	if err != nil || !done {
		t.Fatalf("original receipt recovery: done=%v error=%v", done, err)
	}
	reopened, err := NewIntentStore(steerer.intents.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Current()
	if err != nil || current == nil || current.Status != "finalized" || current.FinalizedBlockHash != receipt.hash.Hex() || current.ExtrinsicHash != pending.Prepared.ExtrinsicHash {
		t.Fatalf("original receipt custody differs: %+v %v", current, err)
	}
	after, err := json.Marshal(current.Prepared)
	if err != nil || !bytes.Equal(before, after) || receipt.eventReads == 0 || receipt.runtimeReads == 0 || receipt.submissions != 0 || receipt.subscriptions != 0 {
		t.Fatalf("original receipt was changed or sent again: %+v %v", receipt, err)
	}
}
