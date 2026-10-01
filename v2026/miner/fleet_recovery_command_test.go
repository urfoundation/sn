// Command-level recovery tests force retry boundaries through real local Rpc
// transports, with no external identity or transaction.
package miner

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A second invocation must recover the original publication, not pay again.
func TestFleetRecoveryPublishCommandDoesNotSignAgain(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	for i := 0; i < 2; i++ {
		if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
			t.Fatal(err)
		}
	}
	if got := fixture.count("author_submitAndWatchExtrinsic"); got != 1 {
		t.Fatalf("restart repeated native publication: %d broadcasts", got)
	}
}

// Binding consent and the relayer nonce belong to one durable operation.
func TestFleetRecoveryBindCommandDoesNotSignAgain(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	for i := 0; i < 2; i++ {
		if err := fleetBind(fixture.opts, fixture.manifest); err != nil {
			t.Fatal(err)
		}
	}
	if got := fixture.count("eth_sendRawTransaction"); got != 1 {
		t.Fatalf("restart repeated EVM binding: %d broadcasts", got)
	}
}

// Revocation has the same uncertain-send liability as binding.
func TestFleetRecoveryRevokeCommandDoesNotSignAgain(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	for i := 0; i < 2; i++ {
		if err := fleetRevoke(fixture.opts, fixture.manifest); err != nil {
			t.Fatal(err)
		}
	}
	if got := fixture.count("eth_sendRawTransaction"); got != 1 {
		t.Fatalf("restart repeated EVM revocation: %d broadcasts", got)
	}
}

// Transport loss after acceptance leaves the original publication durable;
// a fresh process recovers the canonical original without signing or sending.
func TestFleetRecoveryPublishAcceptedWithoutAcknowledgment(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	fixture.stateLock.Lock()
	fixture.nativeDropAck = true
	fixture.stateLock.Unlock()
	if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("lost acknowledgment reported success")
	}
	original := fleetRecoveryTestRecord(t)
	if original.Stage != "may_have_sent" {
		t.Fatal("uncertain transaction not retained")
	}
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	recovered := fleetRecoveryTestRecord(t)
	if recovered.Stage != "finalized" || recovered.TxHash != original.TxHash || !bytes.Equal(recovered.Raw, original.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("system_accountNextIndex") != 1 {
		t.Fatal("restart replaced the accepted publication")
	}
}

// Registration cannot return an unjournaled 'already registered' shortcut
// when its original signed receipt still needs historical reconciliation.
func TestFleetRecoveryRegisterAcceptedWithoutAcknowledgment(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, true)}
	fixture.opts["--apply"] = true
	fixture.stateLock.Lock()
	fixture.nativeDropAck = true
	fixture.stateLock.Unlock()
	if err := fleetRegister(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("lost acknowledgment reported success")
	}
	original := fleetRecoveryTestRecord(t)
	fixture.opts["--burn_limit_rao"] = "999"
	fixture.opts["--fee_limit_rao"] = "999"
	if err := fleetRegister(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	recovered := fleetRecoveryTestRecord(t)
	if recovered.Stage != "finalized" || recovered.NativeReceipt == nil || !strings.Contains(recovered.Outcome, "uid 7") || recovered.TxHash != original.TxHash || fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("payment_queryInfo") != 1 {
		t.Fatal("registration recovery lost original receipt/economics")
	}
}

// A prepared transaction survives a crash before the first network write.
// Replay sends byte-for-byte the durable transaction with the original nonce.
func TestFleetRecoveryPublishPreparedRestartReplaysExactBytes(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	record, signer := fleetRecoveryTestPrepared(t, fixture)
	store, err := openFleetRecoveryStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.put(record, signer); err != nil {
		t.Fatal(err)
	}
	store.close()
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	sent := fixture.nativeSigned
	fixture.stateLock.Unlock()
	if sent != codec.HexEncodeToString(record.Raw) || fixture.count("system_accountNextIndex") != 0 || fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatal("prepared restart generated a new signature or nonce")
	}
}

// A present account with trailing bytes cannot authorize replay using only
// a decoded prefix. The original transaction must remain locally unresolved.
func TestFleetRecoveryNativeMalformedAccountNeverReplays(t *testing.T) {
	for _, length := range []int{57, 80} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
		record, signer := fleetRecoveryTestPrepared(t, fixture)
		store, err := openFleetRecoveryStore()
		if err != nil {
			t.Fatal(err)
		}
		if err := store.put(record, signer); err != nil {
			t.Fatal(err)
		}
		store.close()
		metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.metadata)
		if err != nil {
			t.Fatal(err)
		}
		key, err := types.CreateStorageKey(metadata, "System", "Account", record.NativeSigner[:])
		if err != nil {
			t.Fatal(err)
		}
		fixture.stateLock.Lock()
		fixture.storage[key.Hex()] = codec.HexEncodeToString(make([]byte, length))
		fixture.stateLock.Unlock()
		if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
			t.Errorf("account length %d authorized replay", length)
		}
		retained := fleetRecoveryTestRecord(t)
		if retained.Stage != "prepared" || retained.TxHash != record.TxHash || !bytes.Equal(retained.Raw, record.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 0 || fixture.count("system_accountNextIndex") != 0 {
			t.Errorf("account length %d advanced the retained transaction", length)
		}
	}
}

// A later runtime upgrade does not erase the historical authority that
// actually executed the original transaction.
func TestFleetRecoveryNativeHistoricalReceiptAfterCurrentUpgrade(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	fixture.stateLock.Lock()
	fixture.nativeDropAck = true
	fixture.stateLock.Unlock()
	if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("lost acknowledgment reported success")
	}
	fixture.stateLock.Lock()
	for _, number := range []uint64{100, 101, 102} {
		fixture.historicalVersions[fixture.nativeBlocks[number].Hex()] = fixture.version
	}
	fixture.version.SpecVersion++
	fixture.finalizedNumber = 103
	fixture.stateLock.Unlock()
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fleetRecoveryTestRecord(t).Stage != "finalized" || fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatal("current upgrade prevented original historical recovery")
	}
}

// Pruned blocks and consumed nonces are unresolved, never absence or approval
// to register/publish again using a replacement nonce.
func TestFleetRecoveryNativeMissingHistoryOrConsumedNonceNeverResigns(t *testing.T) {
	for _, missing := range []bool{true, false} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
		fixture.stateLock.Lock()
		fixture.nativeDropAck = true
		fixture.stateLock.Unlock()
		if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
			t.Fatal("lost acknowledgment reported success")
		}
		original := fleetRecoveryTestRecord(t)
		fixture.stateLock.Lock()
		fixture.nativeBlockMissing = missing
		fixture.nativeBroadcast = false
		fixture.stateLock.Unlock()
		if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
			t.Fatal("unresolved history authorized replacement")
		}
		retained := fleetRecoveryTestRecord(t)
		if retained.TxHash != original.TxHash || !bytes.Equal(retained.Raw, original.Raw) || retained.Stage == "finalized" || fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("system_accountNextIndex") != 1 {
			t.Fatal("unresolved native liability replaced")
		}
	}
}

// An explicit server error after accepting the EVM transaction is ambiguous.
// Both command paths recover the original receipt without new consent or nonce.
func TestFleetRecoveryEvmAcceptedWithoutAcknowledgment(t *testing.T) {
	for _, action := range []string{"bind", "revoke"} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--dry-run"] = false
		fixture.stateLock.Lock()
		fixture.rpcFailure = func(method string) error {
			if method == "eth_sendRawTransaction" {
				return errors.New("synthetic accepted but acknowledgment lost")
			}
			return nil
		}
		fixture.stateLock.Unlock()
		command := fleetBind
		if action == "revoke" {
			command = fleetRevoke
		}
		if err := command(fixture.opts, fixture.manifest); err == nil {
			t.Fatal("uncertain send reported success")
		}
		original := fleetRecoveryTestRecord(t)
		if err := os.Remove(fleetOpt(fixture.opts, "--client_seed_file")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(fleetOpt(fixture.opts, "--hotkey_seed_file")); err != nil {
			t.Fatal(err)
		}
		if err := command(fixture.opts, fixture.manifest); err != nil {
			t.Fatal(err)
		}
		retained := fleetRecoveryTestRecord(t)
		if retained.Stage != "finalized" || retained.Mapping == nil || retained.Mapping.Query.NativeNumber != 102 || retained.TxHash != original.TxHash || !bytes.Equal(retained.Raw, original.Raw) || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_gasPrice") != 1 {
			t.Fatalf("%s restart replaced original signed intent", action)
		}
	}
}

// Exact historical inclusion can finish under its original approved artifact
// even while current state has moved to an unapproved runtime.
func TestFleetRecoveryEvmHistoricalReceiptAfterCurrentUpgrade(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	fixture.stateLock.Lock()
	fixture.rpcFailure = func(method string) error {
		if method == "eth_sendRawTransaction" {
			return errors.New("synthetic lost acknowledgment")
		}
		return nil
	}
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("uncertain send reported success")
	}
	fixture.stateLock.Lock()
	for _, number := range []uint64{100, 101, 102} {
		fixture.historicalVersions[fixture.nativeBlocks[number].Hex()] = fixture.version
	}
	fixture.version.SpecVersion++
	fixture.finalizedNumber = 103
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fleetRecoveryTestRecord(t).Stage != "finalized" || fixture.count("eth_sendRawTransaction") != 1 {
		t.Fatal("historical original transaction was not recovered")
	}
}

// An upgrade at execution remains inadmissible after restart, even if a new
// CLI document independently approves that new runtime for future signing.
func TestFleetRecoveryUpgradeAtReceiptRetainsOriginalAuthority(t *testing.T) {
	for _, action := range []string{"publish", "bind", "revoke"} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--dry-run"] = false
		command := fleetBind
		sendMethod := "eth_sendRawTransaction"
		if action == "publish" {
			command = fleetPublish
			sendMethod = "author_submitAndWatchExtrinsic"
			fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
		}
		if action == "revoke" {
			command = fleetRevoke
		}
		fixture.stateLock.Lock()
		fixture.hook = func(method string) {
			if method == sendMethod {
				fixture.version.SpecVersion++
			}
		}
		fixture.stateLock.Unlock()
		if err := command(fixture.opts, fixture.manifest); err == nil {
			t.Fatal("upgraded receipt inherited original approval")
		}
		original := fleetRecoveryTestRecord(t)
		fixture.stateLock.Lock()
		fixture.authority.RuntimeVersion = fixture.version
		fixture.stateLock.Unlock()
		fixture.writeAuthority(t)
		if err := command(fixture.opts, fixture.manifest); err == nil {
			t.Fatal("new approval rewrote original signed authority")
		}
		retained := fleetRecoveryTestRecord(t)
		if retained.Stage == "finalized" || retained.AuthoritySha256 != original.AuthoritySha256 || retained.TxHash != original.TxHash || !bytes.Equal(retained.Raw, original.Raw) || fixture.count(sendMethod) != 1 {
			t.Fatalf("%s replaced original uncertain transaction", action)
		}
	}
}

// Neither matching EVM/native heights nor a historical map entry by itself
// establishes execution. Missing mapping or insertion at the parent fails.
func TestFleetRecoveryEvmRequiresFirstNativeInsertion(t *testing.T) {
	for _, parent := range []bool{false, true} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--dry-run"] = false
		fixture.stateLock.Lock()
		fixture.missingMapping, fixture.mappingAtParent = !parent, parent
		fixture.stateLock.Unlock()
		if err := fleetBind(fixture.opts, fixture.manifest); err == nil {
			t.Fatal("unproved native/EVM mapping accepted")
		}
		original := fleetRecoveryTestRecord(t)
		if err := fleetBind(fixture.opts, fixture.manifest); err == nil {
			t.Fatal("restart ignored missing mapping")
		}
		if retained := fleetRecoveryTestRecord(t); retained.Stage == "finalized" || retained.TxHash != original.TxHash || fixture.count("eth_sendRawTransaction") != 1 {
			t.Fatal("missing mapping erased uncertain transaction")
		}
	}
}

// A receipt that disappeared after its nonce was consumed never permits a
// new nonce or signature, regardless of fee/gas observations.
func TestFleetRecoveryEvmConsumedNonceWithoutReceipt(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	fixture.stateLock.Lock()
	fixture.rpcFailure = func(method string) error {
		if method == "eth_sendRawTransaction" {
			return errors.New("synthetic lost acknowledgment")
		}
		return nil
	}
	fixture.stateLock.Unlock()
	if err := fleetRevoke(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("uncertain send reported success")
	}
	fixture.stateLock.Lock()
	fixture.evmReceipt = nil
	fixture.stateLock.Unlock()
	if err := fleetRevoke(fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "nonce is not available") {
		t.Fatalf("consumed nonce: %v", err)
	}
	if fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_gasPrice") != 1 || fleetRecoveryTestRecord(t).Stage == "finalized" {
		t.Fatal("consumed nonce authorized replacement")
	}
}

// Actual command entry honors exclusive custody before touching missing keys.
func TestFleetRecoveryCommandRejectsSecondOwnerBeforeSigning(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	store, err := openFleetRecoveryStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	fixture.opts["--hotkey_seed_file"] = "missing-synthetic.seed"
	if err := fleetPublish(fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "exclusive owner") {
		t.Fatalf("second command owner: %v", err)
	}
	if fixture.count("state_getRuntimeVersion") != 0 {
		t.Fatal("second owner reached Rpc")
	}
}

// The actual send barrier reads an authenticated, fsynced uncertain record;
// publication and EVM submission cannot race ahead of local custody.
func TestFleetRecoveryActualSendHasDurableSignedRecord(t *testing.T) {
	for _, native := range []bool{true, false} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--dry-run"] = false
		command := fleetBind
		method := "eth_sendRawTransaction"
		if native {
			command = fleetPublish
			method = "author_submitAndWatchExtrinsic"
			fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
		}
		state, _ := providerStateDir()
		seen := false
		fixture.stateLock.Lock()
		fixture.hook = func(observed string) {
			if observed != method {
				return
			}
			raw, err := os.ReadFile(filepath.Join(state, "fleet-mainnet-recovery", "journal.json"))
			var journal fleetRecoveryJournal
			if err != nil || json.Unmarshal(raw, &journal) != nil || len(journal.Records) != 1 || journal.Records[0].validate() != nil || journal.Records[0].Stage != "may_have_sent" {
				t.Error("send outran signed durable custody")
				return
			}
			seen = true
		}
		fixture.stateLock.Unlock()
		if err := command(fixture.opts, fixture.manifest); err != nil {
			t.Fatal(err)
		}
		fixture.stateLock.Lock()
		observed := seen
		fixture.stateLock.Unlock()
		if !observed {
			t.Fatal("actual send barrier absent")
		}
	}
}

// A receipt may have a different EVM height: first-insertion storage proof,
// not numerical equality, establishes its finalized native execution block.
func TestFleetRecoveryEvmDifferentHeightRequiresAuthenticatedMapping(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	fixture.stateLock.Lock()
	fixture.evmBlockNumber = 101
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	record := fleetRecoveryTestRecord(t)
	if record.Mapping == nil || record.Mapping.Query.NativeNumber != 102 || record.Mapping.Query.EVMNumber != 101 {
		t.Fatal("numerical height assumption replaced native mapping")
	}
}

// A runtime change at the explicit prepared/broadcast boundary leaves an
// unsent transaction; restoring its original authority replays its exact bytes.
func TestFleetRecoveryEvmPreparedRestartReplaysExactBytes(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	state, _ := providerStateDir()
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method != "state_getRuntimeVersion" {
			return
		}
		raw, err := os.ReadFile(filepath.Join(state, "fleet-mainnet-recovery", "journal.json"))
		var journal fleetRecoveryJournal
		if err == nil && json.Unmarshal(raw, &journal) == nil && len(journal.Records) == 1 && journal.Records[0].Stage == "prepared" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("prepared/broadcast upgrade admitted")
	}
	original := fleetRecoveryTestRecord(t)
	if original.Stage != "prepared" || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("prepared failure lost exact unsent transaction")
	}
	fixture.stateLock.Lock()
	fixture.hook = nil
	fixture.version = fixture.authority.RuntimeVersion
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	recovered := fleetRecoveryTestRecord(t)
	if recovered.Stage != "finalized" || !bytes.Equal(recovered.Raw, original.Raw) || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_gasPrice") != 1 {
		t.Fatal("prepared EVM replay signed a replacement")
	}
}

// Missing/forged event identity or incomplete readback keeps exact bytes
// pending. Receipt status alone never attests a bind/revoke operation.
func TestFleetRecoveryEvmMalformedOutcomeNeverCompletes(t *testing.T) {
	for _, kind := range []string{"event-absent", "event-wrong-target", "event-wrong-epoch", "native-not-finalized", "wrong-inclusion"} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--dry-run"] = false
		fixture.stateLock.Lock()
		fixture.after = func(method string) {
			if method != "eth_sendRawTransaction" {
				return
			}
			switch kind {
			case "event-absent":
				fixture.evmReceipt.Logs = fixture.evmReceipt.Logs[:0]
			case "event-wrong-target":
				fixture.evmReceipt.Logs[0].Address[0]++
			case "event-wrong-epoch":
				fixture.evmReceipt.Logs[0].Data[len(fixture.evmReceipt.Logs[0].Data)-1]++
			case "native-not-finalized":
				fixture.finalizedNumber = 101
			case "wrong-inclusion":
				fixture.evmSigned = nil
			}
		}
		fixture.stateLock.Unlock()
		if err := fleetBind(fixture.opts, fixture.manifest); err == nil {
			t.Fatalf("%s accepted", kind)
		}
		if err := fleetBind(fixture.opts, fixture.manifest); err == nil {
			t.Fatalf("%s accepted on restart", kind)
		}
		if fleetRecoveryTestRecord(t).Stage == "finalized" || fixture.count("eth_sendRawTransaction") != 1 {
			t.Fatalf("%s caused replacement or false completion", kind)
		}
	}
}

// A bounded scan saves canonical absence progress, then the actual command
// resumes after that checkpoint and recovers the original later inclusion.
func TestFleetRecoveryNativeScanResumesDurableCanonicalProgress(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	fixture.stateLock.Lock()
	fixture.nativeDropAck = true
	fixture.stateLock.Unlock()
	if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("uncertain send reported success")
	}
	store, err := openFleetRecoveryStore()
	if err != nil {
		t.Fatal(err)
	}
	record := store.records[0]
	authority, _, err := record.authority()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := crv4.LoadSeedFile(fleetOpt(fixture.opts, "--hotkey_seed_file"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	err = fleetRecoveryResumeNativeRange(t.Context(), store, record, fleetRecoverySigner{native: key}, authority, chain, true, 1)
	closeFleetNative(chain)
	store.close()
	if err == nil || !strings.Contains(err.Error(), "checkpointed through 101") {
		t.Fatalf("finite range: %v", err)
	}
	checkpoint := fleetRecoveryTestRecord(t)
	if checkpoint.ScanNumber != 101 || checkpoint.ScanHash != fixture.nativeBlocks[101] || checkpoint.Stage == "finalized" {
		t.Fatal("archive cursor not durable")
	}
	if err := fleetPublish(fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fleetRecoveryTestRecord(t).Stage != "finalized" || fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatal("resumed scan resent the original transaction")
	}
}

// Finalized dispatch failure is a terminal original result, not permission to
// sign the same operation at a fresh nonce after a process restart.
func TestFleetRecoveryNativeFinalizedFailureNeverResigns(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.nativeDispatchFailure = true
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	if err := fleetPublish(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("dispatch failure reported success")
	}
	for i := 0; i < 2; i++ {
		if err := fleetPublish(fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "finalized with failure") {
			t.Fatalf("terminal failure lost: %v", err)
		}
	}
	record := fleetRecoveryTestRecord(t)
	if record.Stage != "finalized" || record.Succeeded || record.NativeReceipt == nil || fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("system_accountNextIndex") != 1 {
		t.Fatal("failed native operation was replaced")
	}
}

// A reverted EVM receipt still needs exact native mapping and transaction
// inclusion. Once proved, retry reports the same terminal failure offline.
func TestFleetRecoveryEvmFinalizedFailureNeverResigns(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	fixture.stateLock.Lock()
	fixture.after = func(method string) {
		if method == "eth_sendRawTransaction" {
			fixture.evmReceipt.Status = 0
			fixture.evmReceipt.Logs = fixture.evmReceipt.Logs[:0]
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetRevoke(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("reverted EVM transaction reported success")
	}
	for i := 0; i < 2; i++ {
		if err := fleetRevoke(fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "finalized with failure") {
			t.Fatalf("terminal EVM failure lost: %v", err)
		}
	}
	record := fleetRecoveryTestRecord(t)
	if record.Stage != "finalized" || record.Succeeded || record.Mapping == nil || record.EvmReceipt == nil || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_gasPrice") != 1 {
		t.Fatal("reverted EVM operation was replaced")
	}
}
