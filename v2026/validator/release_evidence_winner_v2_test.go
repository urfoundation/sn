//go:build linux || darwin

// Winner recovery exercises actual signed transaction bytes, exact event
// queries and the shared canonical receipt/state verifier through real HTTP.
package validator

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// A relay needs neither our transaction nor our account's gas allowance to
// recover the real permissionless winner, including a batched forwarder or
// constructor that executes the same immutable companion commitment.
func TestValidatorEvidenceWinnerV2FindsDirectForwardedAndConstructorPublication(t *testing.T) {
	for _, path := range []string{"", "winner-forwarded", "winner-created", "winner-other-slot"} {
		fixture := newEvidenceTransactionV2Fixture(t, path, nil)
		expected := fixture.expected
		expected.SignedTransaction, expected.Relayer = nil, common.Address{}
		expected.MaxGas, expected.MaxFeePerGas = 0, 0
		result, err := fixture.chain.FindValidatorEvidenceSlotWinnerV2Context(t.Context(), expected)
		if err != nil || result == nil {
			t.Fatalf("%s permissionless winner: %v", path, err)
		}
		if result.Receipt.TxHash != fixture.transaction.Hash() || !bytes.Equal(result.SignedTransaction, fixture.expected.SignedTransaction) || result.Publication.Header != expected.Evidence.Header || result.Publication.PublishedBlock != 1101 || fixture.requestCount("eth_getLogs") != 1 || fixture.requestCount("eth_getTransactionByHash") != 1 || fixture.requestCount("eth_getTransactionByBlockHashAndIndex") != 1 {
			t.Fatalf("%s winner lost its real event, signed transaction or canonical inclusion", path)
		}
		if path != "" {
			if result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected); err == nil || result != nil {
				t.Fatalf("%s third-party route escaped the owned direct-send envelope", path)
			}
		}
	}
}

// Absence is actionable only after exact domain, activation and canonical
// epoch geometry checks. Conflicting state and malformed endpoints are errors.
func TestValidatorEvidenceWinnerV2DistinguishesAbsentFromConflictingAuthority(t *testing.T) {
	absent := newEvidenceTransactionV2Fixture(t, "stored-absent", nil)
	if result, err := absent.chain.FindValidatorEvidenceSlotWinnerV2Context(t.Context(), absent.expected); result != nil || !errors.Is(err, ErrValidatorEvidenceAbsent) {
		t.Fatalf("canonical absence classification: result=%v error=%v", result, err)
	}
	if absent.requestCount("eth_getLogs") != 0 || absent.requestCount("eth_getTransactionByHash") != 0 {
		t.Fatal("absent slot guessed a transaction")
	}
	for _, fault := range []string{"stored-header", "stored-publication", "runtime", "window", "window-suffix", "event-missing", "event-duplicate", "event-suffix", "event-padding", "event-topic", "event-count", "event-removed", "event-block", "event-transaction", "event-index", "event-address", "receipt-hash", "receipt-number", "receipt-zero-number", "receipt-zero-hash", "receipt-type", "receipt-create", "receipt-gas", "receipt-cumulative", "receipt-price", "receipt-missing-price", "receipt-reverted", "transaction-other", "transaction-absent", "winner-pending", "inclusion-reorg", "finalized-reorg"} {
		fixture := newEvidenceTransactionV2Fixture(t, fault, nil)
		result, err := fixture.chain.FindValidatorEvidenceSlotWinnerV2Context(t.Context(), fixture.expected)
		if result != nil || err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) {
			t.Fatalf("%s became an accepted winner or actionable absence: result=%v error=%v", fault, result, err)
		}
	}
}

// An invalid public consent cannot trigger event discovery or a transaction
// lookup even though this test endpoint has a matching stored commitment.
func TestValidatorEvidenceWinnerV2RefusesInvalidConsentBeforeLookup(t *testing.T) {
	fixture := newEvidenceTransactionV2Fixture(t, "", nil)
	expected := fixture.expected
	expected.Evidence.HotkeySignature = bytes.Clone(expected.Evidence.HotkeySignature)
	expected.Evidence.HotkeySignature[0] ^= 1
	if result, err := fixture.chain.FindValidatorEvidenceSlotWinnerV2Context(t.Context(), expected); result != nil || err == nil {
		t.Fatal("invalid consent reached winner acceptance")
	}
	if fixture.requestCount("eth_getLogs") != 0 || fixture.requestCount("eth_getTransactionByHash") != 0 || fixture.requestCount("eth_getCode") != 0 {
		t.Fatal("invalid consent reached chain authority reads")
	}
}
