//go:build linux || darwin

// Native history fixtures retain canonical header bytes across epochs and
// preserve the activation-height hash boundary through the real startup reader.
package validator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// The actual Rpc response must commit to the selected number and complete
// Scale header; a height-only identity check cannot detect an invented digest.
func requireReleaseStartupNativeHeader(t *testing.T, native *releaseNativeValidatorTestFixture, number uint64, hash types.Hash) {
	t.Helper()
	native.ctx = t.Context()
	var raw json.RawMessage
	if err := native.chain.API.Client.CallContext(t.Context(), &raw, "chain_getHeader", hash.Hex()); err != nil {
		t.Fatal(err)
	}
	var quantity struct {
		Number string `json:"number"`
	}
	var header types.Header
	if err := json.Unmarshal(raw, &quantity); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	if quantity.Number != fmt.Sprintf("0x%x", number) || uint64(header.Number) != number || types.Hash(blake2b.Sum256(encoded)) != hash {
		t.Fatal("native header does not match its canonical observation")
	}
}

// Both operators seal actual ordinary cuts at several epochs, then historical
// reads revisit earlier points before the complete unchanged snapshots reopen.
func TestReleaseStartupV2AuthenticatesRetainedNativeEpochHeaders(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	journals := map[uint64][2]*releaseMeasurementInputJournal{}
	for _, epoch := range []uint64{1, 2, 7} {
		var pair [2]*releaseMeasurementInputJournal
		for index := range pair {
			pair[index] = fixture.ordinary(t, index, epoch, false)
		}
		journals[epoch] = pair
	}
	for index, point := range fixture.nativePoints {
		if point.epoch != uint64(index+1) || uint64(point.header.Number) != fixture.inputs[0].Context.Activation.NativeBlock+uint64(index) {
			t.Fatal("retained native header changed its original height or epoch")
		}
		if index > 0 && point.header.ParentHash != fixture.nativePoints[index-1].hash {
			t.Fatal("retained native header lost its authenticated predecessor")
		}
	}
	for _, epoch := range []uint64{7, 1, 2, 7} {
		for index, journal := range journals[epoch] {
			before := len(fixture.nativeFixture.calls)
			if err := authenticateReleaseStartupNativeV2Context(t.Context(), fixture.nativeFixture.chain, fixture.inputs[index].Context, journal, fixture.nativeFixture.expected, false); err != nil {
				t.Fatalf("retained native epoch %d operator %d was not authenticated: %v", epoch, index, err)
			}
			if len(fixture.nativeFixture.calls) <= before {
				t.Fatal("retained native epoch skipped its real schedule reader")
			}
			point := fixture.nativePoints[epoch-1]
			if journal.MeasurementInput.CutNativeBlock != uint64(point.header.Number) || journal.MeasurementInput.CutNativeBlockHash != point.hash.Hex() {
				t.Fatal("ordinary journal differs from its retained native header")
			}
			requireReleaseStartupNativeHeader(t, fixture.nativeFixture, journal.MeasurementInput.CutNativeBlock, point.hash)
		}
	}
	want := releaseStartupV2TestDiskImages(t, fixture.disk)
	fixture.reopen(t)
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatalf("actual multi-epoch native history did not reopen: %v", err)
	}
	for index, actual := range releaseStartupV2TestDiskImages(t, fixture.disk) {
		if !bytes.Equal(actual, want[index]) || fixture.disk.participants[index].Stats.egressGeneration != 4 {
			t.Fatal("native history replay changed its completed ordinary snapshots")
		}
	}
}

// A coherent alternate chain can satisfy the native reader at the same height.
// The independently authenticated activation hash must still reject that fork.
func TestReleaseStartupV2RejectsEqualHeightNativeHashSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	journal := fixture.ordinary(t, 0, 1, false)
	initial := fixture.inputs[0].Context
	if err := authenticateReleaseStartupNativeV2Context(t.Context(), fixture.nativeFixture.chain, initial, journal, fixture.nativeFixture.expected, false); err != nil {
		t.Fatalf("original activation-height native observation refused: %v", err)
	}
	header, hash := releaseReceiptTestHeader(t, types.Hash{0xbb}, initial.Activation.NativeBlock)
	if hash == types.Hash(initial.Activation.NativeHash) {
		t.Fatal("alternate native header equals its authenticated activation")
	}
	point := releaseStartupNativeTestPoint{header: header, hash: hash, epoch: 1}
	fixture.nativePoints[0] = point
	fixture.nativeHashPoints[hash.Hex()] = point
	journal.MeasurementInput.CutNativeBlockHash = hash.Hex()
	requireReleaseStartupNativeHeader(t, fixture.nativeFixture, initial.Activation.NativeBlock, hash)
	for _, retained := range []bool{false, true} {
		before := len(fixture.nativeFixture.calls)
		err := authenticateReleaseStartupNativeV2ContextWithRetainedHistory(t.Context(), fixture.nativeFixture.chain, initial, journal, fixture.nativeFixture.expected, false, retained)
		if err == nil || !strings.Contains(err.Error(), "startup ordinary native observation precedes authenticated activation") || len(fixture.nativeFixture.calls) != before {
			t.Fatalf("equal-height native hash substitution escaped activation custody (retained=%t): %v", retained, err)
		}
	}
}

// The adjacent publication fixture exposes its later native point over real
// local Http while preserving the observation named by its source artifact.
func TestValidatorEvidenceDepositAuditV2NativeHeaderMatchesSignedSource(t *testing.T) {
	t.Parallel()
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	native := fixture.base.startup.nativeFixture
	if native.blockNumber <= fixture.base.startup.inputs[0].Context.Activation.NativeBlock || fixture.artifact.NativeSnapshotHash != native.block.Hex() || fixture.artifact.NativeSnapshotBlock != native.blockNumber {
		t.Fatal("deposit audit fixture did not retain a distinct later native observation")
	}
	requireReleaseStartupNativeHeader(t, native, fixture.artifact.NativeSnapshotBlock, native.block)
}
