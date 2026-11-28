// Missing wire evidence is a typed unknown outcome; complete contradictory
// commitments retain their original hard failure and cannot certify absence.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Every omission is injected at the physical reader, including a null
// finalized head or height lookup before any body has been observed.
func TestReceiptUnavailablePreservesUnknownScan(t *testing.T) {
	for _, selected := range []string{"head", "canonical", "header", "header-number", "header-logs", "block", "vector"} {
		fixture := newReceiptScanTestFixture(t, [][][]byte{{[]byte{1, 2}}}, 0)
		injected := false
		fixture.fault = func(_ context.Context, target any, method string, _ ...any) (bool, error) {
			matches := selected == "head" && method == "chain_getFinalizedHead" || selected == "canonical" && method == "chain_getBlockHash" || (selected == "header" || selected == "header-number" || selected == "header-logs") && method == "chain_getHeader" || (selected == "block" || selected == "vector") && method == "chain_getBlock"
			if !matches {
				return false, nil
			}
			injected = true
			var result any
			if selected == "header-number" || selected == "header-logs" {
				var header map[string]any
				if err := receiptTestAssign(&header, receiptTestHeaderWire(fixture.headers[5])); err != nil {
					return true, err
				}
				if selected == "header-number" {
					delete(header, "number")
				} else {
					header["digest"] = map[string]any{"logs": nil}
				}
				result = header
			} else if selected == "vector" {
				result = map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(fixture.headers[5])}}
			}
			return true, receiptTestAssign(target, result)
		}
		receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), types.Hash{99}, 5)
		var missing *ReceiptEvidenceUnavailableError
		if !injected || receipt != nil || found || !errors.As(err, &missing) {
			t.Fatalf("%s did not retain typed unknown receipt: %+v %t %v", selected, receipt, found, err)
		}
		fixture.fault = nil
		if receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), types.Hash{99}, 5); err != nil || found || receipt != nil {
			t.Fatalf("%s complete evidence failed after read recovered: %+v %t %v", selected, receipt, found, err)
		}
	}
}

// A malformed or contradicting returned value cannot become an unavailable
// result just because the corrected reader now represents omissions explicitly.
func TestReceiptUnavailableDoesNotHideCommitmentContradiction(t *testing.T) {
	for _, selected := range []string{"short-hash", "changed-header", "truncated-vector", "malformed-json"} {
		fixture := newReceiptScanTestFixture(t, [][][]byte{{[]byte{1, 2}}}, 0)
		fixture.fault = func(_ context.Context, target any, method string, _ ...any) (bool, error) {
			if selected == "short-hash" && method == "chain_getFinalizedHead" {
				return true, receiptTestAssign(target, "0x01")
			}
			if selected == "changed-header" && method == "chain_getHeader" {
				header := fixture.headers[5]
				header.StateRoot[0] ^= 1
				return true, receiptTestAssign(target, receiptTestHeaderWire(header))
			}
			if method == "chain_getBlock" {
				switch selected {
				case "truncated-vector":
					return true, receiptTestAssign(target, map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(fixture.headers[5]), "extrinsics": []string{}}})
				case "malformed-json":
					*(target.(*json.RawMessage)) = []byte("{malformed")
					return true, nil
				}
			}
			return false, nil
		}
		receipt, found, err := fixture.chain.LocateFinalizedExtrinsic(t.Context(), types.Hash{99}, 5)
		var missing *ReceiptEvidenceUnavailableError
		if err == nil || errors.As(err, &missing) || receipt != nil || found {
			t.Fatalf("%s contradiction became unavailable or successful absence: %+v %t %v", selected, receipt, found, err)
		}
	}
}
