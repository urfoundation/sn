// Bounded scans expose only complete canonical chunks. A continuation joins
// the exact preceding hash and never turns a failed partial body into absence.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

// A late timeout preserves every complete body, including a partial chunk;
// a subsequent request starts at its boundary and finds the original bytes.
func TestReceiptScanChunkPreservesCompletedPrefix(t *testing.T) {
	extrinsic := []byte{4, 0}
	hash := types.Hash(blake2b.Sum256(extrinsic))
	fixture := newReceiptScanTestFixture(t, [][][]byte{nil, nil, nil, {extrinsic}}, 0)
	reads := map[uint64]int{}
	fail := true
	fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
		if method == "chain_getBlock" {
			for number, block := range fixture.hashes {
				if args[0] == block.Hex() {
					reads[number]++
					if number == 8 && fail {
						return true, context.DeadlineExceeded
					}
				}
			}
		}
		return false, nil
	}
	first, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), hash, FinalizedExtrinsicScanRange{First: 5, MaximumBlocks: 2})
	if err != nil {
		t.Fatal(err)
	}
	number, boundary, absent := first.AbsenceBoundary()
	head, headHash := first.FinalizedBoundary()
	if !absent || number != 6 || boundary != fixture.hashes[6] || head != 8 || headHash != fixture.hashes[8] || first.ReachedFinalizedBoundary() {
		t.Fatal("bounded coverage borrowed the unscanned finality boundary")
	}
	rangeInput := FinalizedExtrinsicScanRange{First: number + 1, PreviousHash: boundary, MaximumBlocks: 2}
	failed, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), hash, rangeInput)
	partial, partialHash, covered := failed.AbsenceBoundary()
	if !errors.Is(err, context.DeadlineExceeded) || !covered || partial != 7 || partialHash != fixture.hashes[7] || failed.ReachedFinalizedBoundary() {
		t.Fatalf("late timeout discarded completed partial prefix: %+v %v", failed, err)
	}
	fail = false
	rangeInput.First, rangeInput.PreviousHash = partial+1, partialHash
	recovered, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), hash, rangeInput)
	if err != nil || recovered.Receipt() == nil || recovered.Receipt().BlockNumber != 8 {
		t.Fatalf("chunk restart lost the exact retained inclusion: %v", err)
	}
	if reads[5] != 1 || reads[6] != 1 || reads[7] != 1 || reads[8] != 2 {
		t.Fatalf("chunk restart repeated completed prefix or skipped interrupted evidence: %v", reads)
	}
}

// Incomplete successful responses remain unknown. A previously completed
// chunk is a separate witness, not permission to accept its incomplete child.
func TestReceiptScanChunkMissingBodyStopsBeforeIncompleteEvidence(t *testing.T) {
	fixture := newReceiptScanTestFixture(t, [][][]byte{nil, nil}, 0)
	fixture.bodies[6] = []byte(`{"block":{"header":null,"extrinsics":[]}}`)
	scan, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), types.Hash{7}, FinalizedExtrinsicScanRange{First: 5, MaximumBlocks: 2})
	var unavailable *ReceiptEvidenceUnavailableError
	number, hash, covered := scan.AbsenceBoundary()
	if !errors.As(err, &unavailable) || !covered || number != 5 || hash != fixture.hashes[5] || scan.ReachedFinalizedBoundary() {
		t.Fatalf("incomplete body changed the complete prefix boundary: %+v %v", scan, err)
	}
}

// Actual body contradictions and mixed errors never export partial absence.
// A caller cannot use a transient sibling to disguise an integrity incident.
func TestReceiptScanChunkContradictionNeverExportsPartialPrefix(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		fixture := newReceiptScanTestFixture(t, [][][]byte{nil, nil}, 0)
		if mixed {
			fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
				if method == "chain_getBlock" && args[0] == fixture.hashes[6].Hex() {
					return true, errors.Join(context.DeadlineExceeded, errors.New("independent physical integrity failure"))
				}
				return false, nil
			}
		} else {
			encoded, err := json.Marshal(map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(fixture.headers[5]), "extrinsics": []string{}}})
			if err != nil {
				t.Fatal(err)
			}
			fixture.bodies[6] = encoded
		}
		scan, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), types.Hash{7}, FinalizedExtrinsicScanRange{First: 5, MaximumBlocks: 2})
		if err == nil || scan != nil {
			t.Fatalf("hard contradiction exported partial absence (mixed=%t): %+v %v", mixed, scan, err)
		}
	}
}

// A valid hash at the wrong height, an uncanonical previous hash and a valid
// body's contradictory parent all reject continuation before negative proof.
func TestReceiptScanChunkRequiresExactPredecessor(t *testing.T) {
	for _, fault := range []string{"height", "canonical", "parent"} {
		fixture := newReceiptScanTestFixture(t, [][][]byte{nil, nil, nil}, 0)
		previous := fixture.hashes[5]
		switch fault {
		case "height":
			previous = fixture.hashes[6]
		case "canonical":
			fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
				if method == "chain_getBlockHash" && args[0] == uint64(5) {
					return true, receiptTestAssign(target, types.Hash{9}.Hex())
				}
				return false, nil
			}
		case "parent":
			header, hash := receiptTestHeader(t, types.Hash{0xee}, 6, nil, 0)
			fixture.headers[6], fixture.hashes[6] = header, hash
			raw, err := json.Marshal(map[string]any{"block": map[string]any{"header": receiptTestHeaderWire(header), "extrinsics": []string{}}})
			if err != nil {
				t.Fatal(err)
			}
			fixture.bodies[6] = raw
		}
		if scan, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), types.Hash{7}, FinalizedExtrinsicScanRange{First: 6, PreviousHash: previous, MaximumBlocks: 1}); err == nil || scan != nil {
			t.Fatalf("%s contradictory predecessor supplied coverage: %v", fault, err)
		}
	}
}

// Chunk bounds are fixed semantics, while a behind endpoint supplies no proof
// and cannot invalidate an already retained complete prefix.
func TestReceiptScanChunkBoundsAndBehindHead(t *testing.T) {
	fixture := newReceiptScanTestFixture(t, [][][]byte{nil}, 0)
	for _, input := range []FinalizedExtrinsicScanRange{
		{First: 5},
		{First: 5, MaximumBlocks: ReceiptScanChunkBlockLimit + 1},
		{First: 0, PreviousHash: types.Hash{1}, MaximumBlocks: 1},
	} {
		if _, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), types.Hash{7}, input); err == nil {
			t.Fatal("invalid bounded range was accepted")
		}
	}
	if len(fixture.calls) != 0 {
		t.Fatal("invalid range opened RPC reads")
	}
	scan, err := fixture.chain.ScanFinalizedExtrinsicRange(t.Context(), types.Hash{7}, FinalizedExtrinsicScanRange{First: 6, PreviousHash: fixture.hashes[5], MaximumBlocks: 1})
	if err != nil || scan.ReachedFinalizedBoundary() {
		t.Fatalf("behind-head continuation acquired new coverage: %v", err)
	}
	if _, _, covered := scan.AbsenceBoundary(); covered {
		t.Fatal("zero-iteration scan supplied absence")
	}
}
