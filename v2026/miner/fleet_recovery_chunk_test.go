// Real command/store restarts retain signed complete-body chunks after a later
// RPC response is unavailable, without another signature or nonce allocation.
package miner

import (
	"bytes"
	"errors"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Future canonical blocks are selected before the original send. No block
// already observed as finalized is rewritten to manufacture later inclusion.
func fleetRecoveryChunkTestPending(t *testing.T) (*fleetMainnetTestFixture, *fleetRecoveryRecord) {
	t.Helper()
	fixture := newFleetMainnetTestFixture(t)
	fixture.stateLock.Lock()
	fixture.nativeDropAck = true
	fixture.nativeReceiptNumber, fixture.nativeThrough = 230, 230
	fixture.nativeRuntimeUpdateAt = 101
	err := fixture.rebuildNativeBlocksWithLock()
	fixture.stateLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("original uncertain send unexpectedly completed")
	}
	original := fleetRecoveryTestRecord(t)
	if original.Stage != "may_have_sent" || original.StartNumber != 100 || original.ScanNumber != 0 {
		t.Fatal("original signature did not retain its actual pre-send boundary")
	}
	return fixture, original
}

// The prior full-range writer lost all 128 successful reads at the late null
// response. Both command restarts now start after the exact signed prefix.
func TestFleetRecoveryNativeChunkSurvivesLateUnavailableBody(t *testing.T) {
	fixture, original := fleetRecoveryChunkTestPending(t)
	fixture.stateLock.Lock()
	fixture.nativeBodyOverrides[229] = nil
	fixture.stateLock.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		err := fleetPublish(fixture.opts, fixture.manifest)
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("late absent body lost its unavailable cause: %v", err)
		}
		checkpoint := fleetRecoveryTestRecord(t)
		if checkpoint.ScanNumber != 228 || checkpoint.ScanHash != fixture.nativeBlocks[228] || checkpoint.ScanProof != fleetRecoveryNativeScanProof || checkpoint.Stage != original.Stage || !bytes.Equal(checkpoint.Raw, original.Raw) || checkpoint.TxHash != original.TxHash {
			t.Fatalf("late read discarded completed signed prefix: scanned=%d proof=%s", checkpoint.ScanNumber, checkpoint.ScanProof)
		}
	}
	fixture.stateLock.Lock()
	for number := uint64(101); number <= 228; number++ {
		if fixture.nativeBodyReads[number] != 1 {
			t.Errorf("completed block %d replayed %d times", number, fixture.nativeBodyReads[number])
		}
	}
	delete(fixture.nativeBodyOverrides, 229)
	fixture.stateLock.Unlock()
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	final := fleetRecoveryTestRecord(t)
	if final.Stage != "finalized" || final.NativeReceipt == nil || final.NativeReceipt.BlockNumber != 230 || !bytes.Equal(final.Raw, original.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("system_accountNextIndex") != 1 {
		t.Fatal("chunk restart lost exact original inclusion or allocated another send")
	}
	fixture.stateLock.Lock()
	reads101, reads228 := fixture.nativeBodyReads[101], fixture.nativeBodyReads[228]
	fixture.stateLock.Unlock()
	if reads101 != 1 || reads228 != 1 {
		t.Fatal("final receipt recovery replayed completed prefix bodies")
	}
}

// Even a chunk that never reaches its 128-block target retains its admitted
// prefix. Repeated unavailable reads cannot force a full prefix replay.
func TestFleetRecoveryNativeInterruptedChunkKeepsPartialProgress(t *testing.T) {
	fixture, original := fleetRecoveryChunkTestPending(t)
	fixture.stateLock.Lock()
	fixture.nativeBodyOverrides[150] = nil
	fixture.stateLock.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		err := fleetPublish(fixture.opts, fixture.manifest)
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("interrupted chunk lost its original unavailable read: %v", err)
		}
		checkpoint := fleetRecoveryTestRecord(t)
		if checkpoint.ScanNumber != 149 || checkpoint.ScanHash != fixture.nativeBlocks[149] || checkpoint.ScanProof != fleetRecoveryNativeScanProof || checkpoint.Stage != original.Stage || !bytes.Equal(checkpoint.Raw, original.Raw) {
			t.Fatalf("interrupted chunk discarded its admitted partial prefix: scanned=%d proof=%s", checkpoint.ScanNumber, checkpoint.ScanProof)
		}
	}
	fixture.stateLock.Lock()
	for number := uint64(101); number <= 149; number++ {
		if fixture.nativeBodyReads[number] != 1 {
			t.Errorf("interrupted recovery repeated admitted block %d: %d", number, fixture.nativeBodyReads[number])
		}
	}
	delete(fixture.nativeBodyOverrides, 150)
	fixture.stateLock.Unlock()
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	final := fleetRecoveryTestRecord(t)
	if final.Stage != "finalized" || final.NativeReceipt == nil || final.NativeReceipt.BlockNumber != 230 || !bytes.Equal(final.Raw, original.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("system_accountNextIndex") != 1 {
		t.Fatal("partial progress changed or duplicated the original signed attempt")
	}
}

// A behind endpoint cannot invalidate signed prefix progress or borrow a newer
// account nonce. It remains unresolved until canonical finality catches up.
func TestFleetRecoveryNativeChunkBehindHeadKeepsCheckpoint(t *testing.T) {
	fixture, original := fleetRecoveryChunkTestPending(t)
	if err := fleetRecoveryReceiptTestRange(t, fixture, crv4.ReceiptScanChunkBlockLimit); err == nil {
		t.Fatal("bounded prefix unexpectedly resolved future inclusion")
	}
	checkpoint := fleetRecoveryTestRecord(t)
	fixture.stateLock.Lock()
	fixture.finalizedNumber = 100
	fixture.stateLock.Unlock()
	err := fleetRecoveryReceiptTestRange(t, fixture, crv4.ReceiptScanChunkBlockLimit)
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("behind head became a permanent contradiction: %v", err)
	}
	retained := fleetRecoveryTestRecord(t)
	if retained.ScanNumber != checkpoint.ScanNumber || retained.ScanHash != checkpoint.ScanHash || !bytes.Equal(retained.Raw, original.Raw) || retained.Stage != original.Stage {
		t.Fatal("behind endpoint erased the authenticated original prefix")
	}
}
