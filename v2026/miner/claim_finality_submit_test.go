package miner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// The shared submitter can return after an opening finality read. The claim
// owner must independently close its actual receipt before persisting success.
func TestClaimClockFreshSubmitClosesFinalityBeforePublication(t *testing.T) {
	for _, regress := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.finalized = 95
			fixture.entry = &ClaimQueueEntry{Epoch: 70, Status: "submitting", Attempts: 1}
			tags := 0
			fixture.reply = func(_ context.Context, method string, params []json.RawMessage, result any) (any, error) {
				if regress && method == "eth_getBlockByNumber" && string(params[0]) == `"0x5a"` {
					fixture.finalized = 70
				}
				if method == "eth_getBlockByNumber" && string(params[0]) == `"finalized"` {
					tags++
					if regress && tags == 3 {
						cancel()
					}
				}
				return result, nil
			}
		})
		store := newClaimQueueTestStore(t, fixture.cfg.StateDir)
		queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": fixture.entry}}
		admission := &claimAdmission{}
		err := submitClaimDirect(ctx, fixture.cfg, fakeClaimAPI{result: fixture.claim}, fixture.entry, store, queue, admission)
		if regress {
			if err == nil || fixture.entry.FinalizedBlock != 0 || !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "not finalized") {
				t.Fatalf("fresh claim published finality95→70 receipt90: entry=%+v error=%v", fixture.entry, err)
			}
		} else if err != nil || fixture.entry.FinalizedBlock != 90 || fixture.entry.ReceiptLogsHash == "" {
			t.Fatalf("stable fresh claim did not publish reconciled receipt: entry=%+v error=%v", fixture.entry, err)
		}
		retained, loadErr := store.load()
		_, _, _, sends := fixture.evidence()
		if loadErr != nil || len(sends) != 1 || sends[0] != fixture.entry.RawTxHex || retained.Entries["70"].RawTxHex != sends[0] || retained.Entries["70"].TxHash != fixture.entry.TxHash || admission.nonceMinimum() != 24 {
			t.Fatalf("fresh closing failure lost signed custody: load=%v sends=%v floor=%d", loadErr, sends, admission.nonceMinimum())
		}
	}
}

func TestClaimClockFreshSubmitAuthenticatesReconciledReceiptIdentityAndEvent(t *testing.T) {
	for _, changed := range []string{"identity", "event"} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.finalized = 95
			fixture.entry = &ClaimQueueEntry{Epoch: 70, Status: "submitting", Attempts: 1}
			fixture.reply = func(_ context.Context, method string, _ []json.RawMessage, result any) (any, error) {
				if method == "eth_getTransactionReceipt" && fixture.receiptReads == 2 {
					if changed == "identity" {
						fixture.receipt.TransactionIndex = 1
						fixture.receipt.Logs[0].TxIndex = 1
					} else {
						fixture.receipt.Logs[0].Topics[1] = common.Hash{0xee}
					}
				}
				return result, nil
			}
		})
		store := newClaimQueueTestStore(t, fixture.cfg.StateDir)
		queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": fixture.entry}}
		err := submitClaimDirect(t.Context(), fixture.cfg, fakeClaimAPI{result: fixture.claim}, fixture.entry, store, queue, &claimAdmission{})
		if err == nil || fixture.entry.FinalizedBlock != 0 || fixture.entry.ReceiptLogsHash != "" {
			t.Fatalf("fresh claim trusted original receipt over changed reconciled %s: %+v %v", changed, fixture.entry, err)
		}
		retained, loadErr := store.load()
		_, _, _, sends := fixture.evidence()
		if loadErr != nil || len(sends) != 1 || sends[0] != fixture.entry.RawTxHex || retained.Entries["70"].RawTxHex != sends[0] || retained.Entries["70"].FinalizedBlock != 0 {
			t.Fatalf("reconciled %s refusal lost original signed bytes: %v %v", changed, loadErr, sends)
		}
	}
}
