// Complete bodies bound negative receipt evidence to the same canonical
// block used by later nonce/expiry reads, even while the advertised head moves.
package crv4

import (
	"context"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

// The second block contains our exact bytes, but becomes advertised only after
// the first scan consumes its last body. It cannot retroactively widen absence.
func TestReceiptScanRetainsExactAbsenceBoundary(t *testing.T) {
	extrinsic := []byte{4, 0}
	hash := types.Hash(blake2b.Sum256(extrinsic))
	fixture := newReceiptScanTestFixture(t, [][][]byte{nil, {extrinsic}}, 0)
	fixture.through = 5
	fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
		if method == "chain_getBlock" && len(args) == 1 && args[0] == fixture.hashes[5].Hex() {
			fixture.through = 6
		}
		return false, nil
	}
	scan, err := fixture.chain.ScanFinalizedExtrinsic(t.Context(), hash, 5)
	if err != nil || scan.Receipt() != nil {
		t.Fatalf("first complete empty body: %v", err)
	}
	number, boundary, absent := scan.AbsenceBoundary()
	if !absent || number != 5 || boundary != fixture.hashes[5] {
		t.Fatal("receipt absence borrowed an unscanned later finalized head")
	}
	next, err := fixture.chain.ScanFinalizedExtrinsic(t.Context(), hash, 5)
	if err != nil || next.Receipt() == nil || next.Receipt().BlockHash != fixture.hashes[6] {
		t.Fatalf("subsequent scan lost the intervening exact transaction: %v", err)
	}
	if _, _, absent := next.AbsenceBoundary(); absent {
		t.Fatal("found transaction also supplied negative coverage")
	}
}

// A node behind the original preparation has no searched interval; it cannot
// prove absence merely because the scan loop happened to have zero iterations.
func TestReceiptScanBeforePreparationRemainsUnknown(t *testing.T) {
	fixture := newReceiptScanTestFixture(t, [][][]byte{nil}, 0)
	scan, err := fixture.chain.ScanFinalizedExtrinsic(t.Context(), types.Hash{7}, 6)
	if err != nil || scan.Receipt() != nil {
		t.Fatalf("behind-head scan: %v", err)
	}
	if _, _, absent := scan.AbsenceBoundary(); absent {
		t.Fatal("unobserved preparation block became authenticated absence")
	}
}
