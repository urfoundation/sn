package miner

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"strings"
	"testing"
)

// Numbered70 remains canonical in every case. Only the separately reported
// finality frontier changes while the entitlement or claimed state is read.
func TestClaimClockFinalizedTagClosesStateAuthority(t *testing.T) {
	for _, closing := range []uint64{60, 95} {
		for _, rootMismatch := range []bool{false, true} {
			fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
				fixture.reply = func(_ context.Context, method string, _ []json.RawMessage, result any) (any, error) {
					if method == "eth_call" {
						fixture.finalized = closing
						if rootMismatch {
							return "0x" + strings.Repeat("00", 32*7), nil
						}
					}
					return result, nil
				}
			})
			claimed, err := queryClaimedFinalized(t.Context(), fixture.cfg, fixture.claim)
			var mismatch *claimArtifactRootMismatchError
			if closing == 60 {
				if claimed || err == nil || !strings.Contains(err.Error(), "regressed") || errors.As(err, &mismatch) {
					t.Fatalf("regressed finality authorized state or correctness mismatch: mismatch=%t claimed=%t error=%v", rootMismatch, claimed, err)
				}
			} else if rootMismatch {
				if claimed || !errors.As(err, &mismatch) {
					t.Fatalf("advanced finality lost stable original root mismatch: %t %v", claimed, err)
				}
			} else if err != nil || !claimed {
				t.Fatalf("ordinary finality advancement stranded original EVM70 state: %t %v", claimed, err)
			}
		}
	}
}

// Both numbered95 and receipt90 retain their exact hashes. Receipt90 must
// remain unresolved if the finality tag moves below it during canonical reads.
func TestClaimClockFinalizedTagClosesReceiptAuthority(t *testing.T) {
	for _, closing := range []uint64{70, 100} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.finalized = 95
			fixture.reply = func(_ context.Context, method string, params []json.RawMessage, result any) (any, error) {
				if method == "eth_getBlockByNumber" && string(params[0]) == `"0x5a"` {
					fixture.finalized = closing
				}
				return result, nil
			}
		})
		receipt, err := finalizedClaimReceipt(t.Context(), fixture.cfg, fixture.entry.TxHash, big.NewInt(945))
		if closing == 70 {
			if receipt != nil || err == nil || !strings.Contains(err.Error(), "regressed") {
				t.Fatalf("canonical receipt90 survived finality95→70: %+v %v", receipt, err)
			}
		} else if err != nil || receipt == nil || receipt.BlockNumber.Uint64() != 90 {
			t.Fatalf("normal finality95→100 lost original receipt: %+v %v", receipt, err)
		}
	}
}

func TestClaimClockFinalizedTagClosesReplayAuthority(t *testing.T) {
	for _, closing := range []uint64{60, 95} {
		for _, consumed := range []bool{false, true} {
			fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
				if consumed {
					fixture.nonce = 24
				}
				fixture.reply = func(_ context.Context, method string, _ []json.RawMessage, result any) (any, error) {
					if method == "eth_call" || consumed && method == "eth_getTransactionCount" {
						fixture.finalized = closing
					}
					return result, nil
				}
			})
			tx, _, from, err := authenticateSignedClaim(fixture.cfg, fixture.entry)
			if err != nil {
				t.Fatal(err)
			}
			before := *fixture.entry
			got, err := rebroadcastSignedClaimTest(t, t.Context(), fixture.cfg, tx, from)
			_, _, _, sends := fixture.evidence()
			if closing == 60 {
				if got || err == nil || !strings.Contains(err.Error(), "regressed") || len(sends) != 0 {
					t.Fatalf("regressed frontier authorized replay/nonce disposition: consumed=%t got=%t error=%v sends=%v", consumed, got, err, sends)
				}
			} else if err != nil || got != consumed || consumed && len(sends) != 0 || !consumed && !slices.Equal(sends, []string{before.RawTxHex}) {
				t.Fatalf("advance changed exact replay: consumed=%t got=%t error=%v sends=%v", consumed, got, err, sends)
			}
			if *fixture.entry != before {
				t.Fatal("finality closure changed original signed custody")
			}
		}
	}
}

func TestClaimClockClosingTagOutageRemainsTransient(t *testing.T) {
	for _, rootMismatch := range []bool{false, true} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.reply = func(_ context.Context, method string, params []json.RawMessage, result any) (any, error) {
				if method == "eth_call" && rootMismatch {
					return "0x" + strings.Repeat("00", 32*7), nil
				}
				if method == "eth_getBlockByNumber" && string(params[0]) == `"finalized"` && len(fixture.stateSelectors) > 0 {
					return nil, errors.New("synthetic finalized-tag outage")
				}
				return result, nil
			}
		})
		claimed, err := queryClaimedFinalized(t.Context(), fixture.cfg, fixture.claim)
		var mismatch *claimArtifactRootMismatchError
		if claimed || err == nil || !strings.Contains(err.Error(), "synthetic finalized-tag outage") || strings.Contains(err.Error(), "not canonical") || errors.As(err, &mismatch) {
			t.Fatalf("unavailable finality published state/correctness failure: mismatch=%t claimed=%t error=%v", rootMismatch, claimed, err)
		}
	}
}
