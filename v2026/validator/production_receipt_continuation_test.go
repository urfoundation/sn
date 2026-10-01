//go:build linux || darwin

// The actual V2 store, signed source and original native liability survive
// chunk outages, process-owner restart and cache eviction without re-signing.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The optional finite lookup avoids repeatedly constructing every predecessor
// header merely to select a synthetic RPC response. Headers still authenticate
// through the actual decoder and body trie before any cache entry is saved.
func productionReceiptContinuationTestCensus(native *productionContinuationNativeTestClient) {
	native.blockNumberKVs = map[string]uint64{}
	for number := uint64(100); number <= native.fixture.production.head; number++ {
		native.blockNumberKVs[native.fixture.production.block(number).Hex()] = number
	}
}

// A timeout in chunk two retains chunk one. A real new intent owner resumes it,
// an evicted cache rescans without touching custody, then later self inclusion
// resolves exactly the original once-broadcast transaction across native epochs.
func TestProductionReceiptChunkResumesDurableIntentAfterOutageAndEviction(t *testing.T) {
	fixture := newProductionContinuationTestFixtureThrough(t, 230)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	original, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.production.head, native.receiptNumber = 229, 230
	fixture.production.epoch++
	native.bodyReads, native.bodyErrorKVs = map[uint64]int{}, map[uint64]error{228: context.DeadlineExceeded}
	productionReceiptContinuationTestCensus(native)
	var wait *productionSteeringReadWait
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &wait) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late chunk outage became a terminal owner error: %v", err)
	}
	owner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := owner.load(t.Context())
	if err != nil || checkpoint == nil || checkpoint.Through != 227 {
		t.Fatalf("late chunk timeout discarded complete original prefix: %+v %v", checkpoint, err)
	}
	fixture.restart(t)
	delete(native.bodyErrorKVs, 228)
	native.afterBody = func(number uint64) {
		if number == 228 {
			if err := os.Remove(filepath.Join(owner.path, productionReceiptCheckpointName)); err != nil {
				t.Fatal(err)
			}
			native.afterBody = nil
		}
	}
	var pendingWait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &pendingWait) {
		t.Fatalf("resumed absence expired an immortal original signature: %v", err)
	}
	for number := uint64(100); number <= 227; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("durable restart repeated completed body %d: %d reads", number, native.bodyReads[number])
		}
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || len(native.broadcasts) != 1 || !slices.Equal(native.nonceReads, []uint64{229}) {
		t.Fatalf("cache progress rewrote custody or read a nonce outside coverage: %v, nonces=%v", err, native.nonceReads)
	}
	if err := os.Remove(filepath.Join(owner.path, productionReceiptCheckpointName)); err != nil {
		t.Fatal(err)
	}
	// Model a cold runtime as well as a new on-disk intent owner; the separate
	// public RunRelease qualification covers construction of the full service.
	fixture.runtime.receiptCache = &productionReceiptCacheState{}
	fixture.restart(t)
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &pendingWait) || native.bodyReads[100] != 2 {
		t.Fatalf("cache eviction stranded the original intent instead of rescanning: %v", err)
	}
	after, err = os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || len(native.broadcasts) != 1 {
		t.Fatal("cache eviction erased original signed progress")
	}
	fixture.production.head = 230
	fixture.production.extrinsicsKVs = map[uint64][]string{230: {pending.Prepared.ExtrinsicHex}}
	productionReceiptContinuationTestCensus(native)
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("extended original inclusion failed after cached absence: %v", err)
	}
	finalized := fixture.restart(t)
	if finalized.Status != "finalized" || finalized.FinalizedBlock != 230 || finalized.CreatedAt != pending.CreatedAt || finalized.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) || native.bodyReads[100] != 2 {
		t.Fatal("receipt extension lost the original signature, age or completed prefix")
	}
}

// Historical signing authority remains closed, but exact original absence plus
// same-boundary foreign nonce use can resolve its liability under approved reads.
func TestProductionReceiptHistoricalAuthorityResolvesForeignNonce(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	native.bodyReads = map[uint64]int{}
	originalOwner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	txHash, err := types.NewHashFromHexString(pending.Prepared.ExtrinsicHash)
	if err != nil {
		t.Fatal(err)
	}
	scan, err := fixture.steerer.native.ScanFinalizedExtrinsicRange(t.Context(), txHash, crv4.FinalizedExtrinsicScanRange{First: 100, MaximumBlocks: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := originalOwner.save(t.Context(), scan); err != nil {
		t.Fatal(err)
	}
	admission := fixture.production.operator.measurement.admission
	current, _ := productionAuthorityTestSuccessor(t, fixture.steerer.cfg, admission.approval, admission.private, false)
	fixture.runtime.cfg, fixture.runtime.history.cfg = *current, *current
	fixture.steerer.cfg = &fixture.runtime.cfg
	fixture.production.head = 101
	native.nonceKVs[101] = pending.Prepared.AccountNonce + 1
	fixture.restart(t)
	originalCfg, err := productionConfigForIntent(current, pending)
	if err != nil {
		t.Fatal(err)
	}
	renewedOwner, err := newProductionReceiptCheckpointOwner(fixture.steerer, originalCfg, pending)
	if err != nil || renewedOwner.scope != originalOwner.scope {
		t.Fatalf("current approval renewal discarded original cache identity: %v", err)
	}
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) {
		t.Fatalf("historical-only pending authority skipped same-boundary nonce resolution: %v", err)
	}
	failed := fixture.restart(t)
	if failed.Status != "failed" || !strings.Contains(failed.Error, "different finalized extrinsic by scanned block 101") || failed.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || failed.CreatedAt != pending.CreatedAt || failed.FinalizedBlock != 0 || !slices.Equal(native.nonceReads, []uint64{101}) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) || native.bodyReads[100] != 1 {
		t.Fatal("historical nonce resolution invented inclusion, changed bytes or granted replay authority")
	}
}
