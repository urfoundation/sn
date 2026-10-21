// Recovery records written before the fleet tag must neither swallow the
// tagged republish nor wedge the journal (SN25 validator mirror incident,
// 2026-10-15). fleetRecoveryTestLegacyIntent turns a current publish intent
// into its pre-tag form (fleet_recovery_format_shim_test.go).
package miner

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A prepared pre-tag publication: one-field Sha256 info signed by the hotkey
// at nonce 0, recorded under the pre-tag intent.
func fleetRecoveryTestLegacyPrepared(t *testing.T, fixture *fleetMainnetTestFixture) (*fleetRecoveryRecord, fleetRecoverySigner) {
	t.Helper()
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := fleetRecoveryNewIntent("publish", authority, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	intent = fleetRecoveryTestLegacyIntent(intent)
	key, err := snchain.LoadKeypairFile(fleetOpt(fixture.opts, "--hotkey_seed_file"))
	if err != nil {
		t.Fatal(err)
	}
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFleetNative(chain)
	view, err := authority.viewAt(t.Context(), chain, fixture.head)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := fixture.manifest.CommitmentHash()
	// The pre-tag fleet call had exactly the Sha256 bytes the source call
	// still writes.
	call, err := view.NewSetSourceCommitmentCall(fixture.manifest.Netuid, hash)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := snchain.EncodeSignedCall(view, key.Ring, call, 0)
	if err != nil {
		t.Fatal(err)
	}
	record := fleetRecoveryPrepared(intent, authority, fixture.head, 100)
	record.NativeSigner, record.Raw, record.TxHash = key.PublicKey(), raw, snchain.ExtrinsicHash(raw).Hex()
	return record, fleetRecoverySigner{native: key}
}

func fleetRecoveryTestPut(t *testing.T, fixture *fleetMainnetTestFixture, signer fleetRecoverySigner, records ...*fleetRecoveryRecord) {
	t.Helper()
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	for _, record := range records {
		if err := store.put(record, signer); err != nil {
			t.Fatal(err)
		}
	}
}

func fleetRecoveryTestRecords(t *testing.T, fixture *fleetMainnetTestFixture) []*fleetRecoveryRecord {
	t.Helper()
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	return store.records
}

func fleetRecoveryTestTaggedInfo(t *testing.T, fixture *fleetMainnetTestFixture) []byte {
	hash, _ := fixture.manifest.CommitmentHash()
	info, err := crv4.EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// A miner that published before the tag holds a finalized publish record.
// Republishing on the new release must send a tagged Raw40 write, not report
// "already finalized".
func TestFleetRecoveryRepublishAfterFinalizedLegacyPublishSendsTaggedWrite(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	legacy, signer := fleetRecoveryTestLegacyPrepared(t, fixture)
	finalized := *legacy
	finalized.Stage, finalized.Outcome, finalized.Succeeded = "finalized", "commitment written at native block 101", true
	finalized.NativeReceipt = &crv4.FinalizedExtrinsic{ExtrinsicHash: types.Hash(snchain.ExtrinsicHash(legacy.Raw)), BlockHash: types.Hash{0x65}, BlockNumber: 101}
	fleetRecoveryTestPut(t, fixture, signer, legacy, &finalized)

	if err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	sent := fixture.nativeSigned
	submits := fixture.calls["author_submitAndWatchExtrinsic"]
	fixture.stateLock.Unlock()
	raw, err := codec.HexDecodeString(sent)
	if submits != 1 || err != nil || !bytes.Contains(raw, fleetRecoveryTestTaggedInfo(t, fixture)) {
		t.Fatalf("republish sent %d writes, tagged=%t", submits, err == nil && bytes.Contains(raw, fleetRecoveryTestTaggedInfo(t, fixture)))
	}
	records := fleetRecoveryTestRecords(t, fixture)
	if len(records) != 2 || records[0].Id != legacy.Id || records[1].Stage != "finalized" || !records[1].Succeeded || records[1].Intent.Action != "publish" {
		t.Fatalf("records %d", len(records))
	}
	// A second run finds the tagged publication finalized and sends nothing.
	if err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil || fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatalf("second republish: %v, %d writes", err, fixture.count("author_submitAndWatchExtrinsic"))
	}
}

// An unresolved pre-tag publication that landed is closed as terminal by the
// legacy decoder, the tagged write follows, and nothing stays blocked.
func TestFleetRecoveryUnresolvedLegacyPublishIsClosedAndUnblocks(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	legacy, signer := fleetRecoveryTestLegacyPrepared(t, fixture)
	sent := *legacy
	sent.Stage = "may_have_sent"
	fleetRecoveryTestPut(t, fixture, signer, legacy, &sent)

	// The legacy write landed at block 102 and the slot holds its one-field
	// Sha256 registration; the next write lands at 103.
	hash, _ := fixture.manifest.CommitmentHash()
	info, err := crv4.EncodeSourceCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	commitmentKey, err := types.CreateStorageKey(metadata, "Commitments", "CommitmentOf", snchain.NetuidArg(25), fixture.manifest.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(metadata, "Commitments", "LastCommitment", snchain.NetuidArg(25), fixture.manifest.Hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.nativeLanded = map[uint64]string{102: codec.HexEncodeToString(legacy.Raw)}
	fixture.nativeReceiptNumber, fixture.finalizedNumber, fixture.nativeNonce = 103, 102, 1
	fixture.storage[commitmentKey.Hex()] = codec.HexEncodeToString(append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(nil, 1), 102), info...))
	fixture.storage[lastKey.Hex()] = codec.HexEncodeToString(binary.LittleEndian.AppendUint32(nil, 102))
	fixture.storage[fixture.nativeEventsKey] = fixture.nativeEvents
	if err := fixture.rebuildNativeBlocksWithLock(); err != nil {
		fixture.stateLock.Unlock()
		t.Fatal(err)
	}
	fixture.stateLock.Unlock()

	if err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	records := fleetRecoveryTestRecords(t, fixture)
	if len(records) != 2 {
		t.Fatalf("records %d", len(records))
	}
	closed, tagged := records[0], records[1]
	if closed.Id != legacy.Id || closed.Stage != "finalized" || closed.NativeReceipt == nil || closed.NativeReceipt.BlockNumber != 102 || !bytes.Contains([]byte(closed.Outcome), []byte("legacy untagged")) {
		t.Fatalf("legacy record stage %s outcome %q", closed.Stage, closed.Outcome)
	}
	raw, err := codec.HexDecodeString(fixture.nativeSigned)
	if tagged.Stage != "finalized" || !tagged.Succeeded || err != nil || !bytes.Contains(raw, fleetRecoveryTestTaggedInfo(t, fixture)) || fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatalf("tagged record stage %s, %d writes", tagged.Stage, fixture.count("author_submitAndWatchExtrinsic"))
	}
	// Bind, revoke and publish are no longer blocked by the old publication.
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"bind", "revoke", "publish"} {
		intent, err := fleetRecoveryNewIntent(action, authority, fixture.manifest)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.find(intent); err != nil {
			t.Fatalf("%s blocked: %v", action, err)
		}
	}
}
