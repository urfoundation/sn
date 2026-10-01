// Real commands and signed durable recovery records own every checkpoint.
// Faults replace raw RPC evidence, never a receipt or persistence verdict.
package miner

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Produces a real uncertain native send. Restart must find exactly those
// bytes; a second signature, nonce allocation or broadcast is a regression.
func fleetRecoveryReceiptTestPending(t *testing.T, update bool) (*fleetMainnetTestFixture, *fleetRecoveryRecord) {
	t.Helper()
	fixture := newFleetMainnetTestFixture(t)
	fixture.stateLock.Lock()
	fixture.nativeDropAck = true
	if update {
		fixture.nativeRuntimeUpdateAt = 101
		if err := fixture.rebuildNativeBlocksWithLock(); err != nil {
			fixture.stateLock.Unlock()
			t.Fatal(err)
		}
	}
	fixture.stateLock.Unlock()
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("uncertain original send reported success")
	}
	record := fleetRecoveryTestRecord(t)
	if record.Stage != "may_have_sent" || record.ScanNumber != 0 {
		t.Fatal("uncertain original bytes were not retained before receipt recovery")
	}
	return fixture, record
}

// Reopens the actual locked store and uses the original retained native signer.
// This only shortens the scan range; all RPC/body/signature/fsync work is real.
func fleetRecoveryReceiptTestRange(t *testing.T, fixture *fleetMainnetTestFixture, maxBlocks uint64) error {
	t.Helper()
	store, err := openFleetRecoveryStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	record := store.records[0]
	authority, _, err := record.authority()
	if err != nil {
		t.Fatal(err)
	}
	key := fleetRecoveryReceiptTestKey(t, fixture)
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFleetNative(chain)
	return fleetRecoveryResumeNativeRange(t.Context(), store, record, fleetRecoverySigner{native: key}, authority, chain, true, maxBlocks)
}

// Uses only the generated test key already selected by the real command.
func fleetRecoveryReceiptTestKey(t *testing.T, fixture *fleetMainnetTestFixture) *crv4.Keypair {
	t.Helper()
	seed, err := crv4.LoadSeedFile(fleetOpt(fixture.opts, "--hotkey_seed_file"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// An explicit empty vector at a genuinely nonempty canonical block cannot
// checkpoint absence, including after a fresh command reopens the record.
func TestFleetRecoveryNativeTruncatedBodyNeverAdvancesCheckpoint(t *testing.T) {
	fixture, original := fleetRecoveryReceiptTestPending(t, false)
	fixture.stateLock.Lock()
	fixture.nativeBodyOverrides[102] = map[string]any{"block": map[string]any{"header": fixture.nativeHeaderWireWithLock(102), "extrinsics": []string{}}}
	fixture.stateLock.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		err := fleetPublish(fixture.opts, fixture.manifest)
		retained := fleetRecoveryTestRecord(t)
		if retained.ScanNumber != original.ScanNumber || retained.ScanHash != original.ScanHash || retained.Stage != original.Stage || retained.TxHash != original.TxHash || !bytes.Equal(retained.Raw, original.Raw) {
			t.Fatal("truncated body advanced or replaced the original durable attempt")
		}
		if err == nil || !strings.Contains(err.Error(), "ordered trie commitment") {
			t.Fatalf("truncated body lost its concrete commitment failure: %v", err)
		}
	}
	fixture.stateLock.Lock()
	reads101, reads102 := fixture.nativeBodyReads[101], fixture.nativeBodyReads[102]
	delete(fixture.nativeBodyOverrides, 102)
	fixture.stateLock.Unlock()
	if reads101 != 2 || reads102 != 2 {
		t.Fatalf("restart skipped unproved body evidence: %d / %d", reads101, reads102)
	}
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	retained := fleetRecoveryTestRecord(t)
	if retained.Stage != "finalized" || retained.NativeReceipt == nil || retained.NativeReceipt.BlockNumber != 102 || !bytes.Equal(retained.Raw, original.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("system_accountNextIndex") != 1 {
		t.Fatal("restored complete body did not recover the original inclusion")
	}
}

// Missing fields and a malformed entry after the matching transaction retain
// unknown outcome; the latter cannot bypass validation by matching early.
func TestFleetRecoveryNativeIncompleteBodyNeverAdvancesPastEvidence(t *testing.T) {
	for _, fault := range []string{"null", "missing-header", "missing-vector", "malformed-tail", "wrong-header"} {
		fixture, original := fleetRecoveryReceiptTestPending(t, false)
		fixture.stateLock.Lock()
		body := map[string]any{"header": fixture.nativeHeaderWireWithLock(102), "extrinsics": []string{fixture.nativeSigned}}
		var response any = map[string]any{"block": body}
		switch fault {
		case "null":
			response = nil
		case "missing-header":
			delete(body, "header")
		case "missing-vector":
			delete(body, "extrinsics")
		case "malformed-tail":
			body["extrinsics"] = []string{fixture.nativeSigned, "not-hex"}
		case "wrong-header":
			body["header"] = fixture.nativeHeaderWireWithLock(101)
		}
		fixture.nativeBodyOverrides[102] = response
		fixture.stateLock.Unlock()
		wantScan := uint64(0)
		if fault == "null" || fault == "missing-header" || fault == "missing-vector" {
			wantScan = 101
		}
		for attempt := 0; attempt < 2; attempt++ {
			if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
				t.Fatalf("%s incomplete evidence became a receipt", fault)
			}
			retained := fleetRecoveryTestRecord(t)
			if retained.ScanNumber != wantScan || retained.Stage != original.Stage || !bytes.Equal(retained.Raw, original.Raw) {
				t.Fatalf("%s changed the complete prefix boundary: got %d, want %d", fault, retained.ScanNumber, wantScan)
			}
		}
		if fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("system_accountNextIndex") != 1 {
			t.Fatalf("%s signed or submitted again", fault)
		}
	}
}

// The old implementation drops digest tag8 when re-encoding SDK headers.
// Shared raw admission must pass a real scan/checkpoint/restart through it.
func TestFleetRecoveryNativeRuntimeUpdateDigestPreservesProgress(t *testing.T) {
	fixture, original := fleetRecoveryReceiptTestPending(t, true)
	if err := fleetRecoveryReceiptTestRange(t, fixture, 1); err == nil || !strings.Contains(err.Error(), "checkpointed through 101") {
		t.Fatalf("runtime-update header rejected canonical prefix: %v", err)
	}
	checkpoint := fleetRecoveryTestRecord(t)
	if checkpoint.ScanProof != fleetRecoveryNativeScanProof || checkpoint.ScanNumber != 101 || checkpoint.ScanHash != fixture.nativeBlocks[101] {
		t.Fatal("complete update-block absence did not acquire its semantic proof")
	}
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	reads101 := fixture.nativeBodyReads[101]
	fixture.stateLock.Unlock()
	retained := fleetRecoveryTestRecord(t)
	if reads101 != 1 || retained.Stage != "finalized" || !bytes.Equal(retained.Raw, original.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatal("qualified prefix was reread, lost or used to replace signed bytes")
	}
}

// Old cursors are authenticated custody but not authenticated absence. One
// actual rescan may replace them with a smaller qualified prefix, then resume.
func TestFleetRecoveryNativeLegacyScanProofMigratesOnce(t *testing.T) {
	fixture, original := fleetRecoveryReceiptTestPending(t, false)
	fixture.stateLock.Lock()
	fixture.finalizedNumber = 104
	fixture.stateLock.Unlock()
	signer := fleetRecoverySigner{native: fleetRecoveryReceiptTestKey(t, fixture)}
	store, err := openFleetRecoveryStore()
	if err != nil {
		t.Fatal(err)
	}
	legacy := *store.records[0]
	legacy.ScanNumber, legacy.ScanHash = 104, fixture.nativeBlocks[104]
	if err := store.put(&legacy, signer); err != nil {
		store.close()
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if err := fleetRecoveryReceiptTestRange(t, fixture, 1); err == nil || !strings.Contains(err.Error(), "checkpointed through 101") {
		t.Fatalf("legacy absence was reused instead of re-authenticated: %v", err)
	}
	checkpoint := fleetRecoveryTestRecord(t)
	if checkpoint.ScanNumber != 101 || checkpoint.ScanProof != fleetRecoveryNativeScanProof || !bytes.Equal(checkpoint.Raw, original.Raw) {
		t.Fatal("legacy proof migration did not retain exact original bytes")
	}
	for _, fault := range []string{"downgrade", "unknown", "rollback", "other-attempt"} {
		candidate := *checkpoint
		switch fault {
		case "downgrade":
			candidate.ScanProof = ""
		case "unknown":
			candidate.ScanProof = "synthetic-unsupported-proof"
		case "rollback":
			candidate.ScanNumber, candidate.ScanHash = 100, fixture.head
		case "other-attempt":
			candidate.Raw = bytes.Clone(candidate.Raw)
			candidate.Raw[len(candidate.Raw)-1] ^= 1
		}
		if err := signer.sign(&candidate); err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(candidate.validate(), fleetRecoveryAdvance(checkpoint, &candidate)); err == nil {
			t.Fatalf("%s changed qualified original scan proof", fault)
		}
	}
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	retained := fleetRecoveryTestRecord(t)
	if retained.Stage != "finalized" || retained.NativeReceipt == nil || retained.NativeReceipt.BlockNumber != 102 || fixture.count("author_submitAndWatchExtrinsic") != 1 || !bytes.Equal(retained.Raw, original.Raw) {
		t.Fatal("migrated prefix stranded or replaced the original receipt")
	}
}
