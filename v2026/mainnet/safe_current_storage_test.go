// Exact published Safe facts are committed to native roots; malicious authority
// gets a genuine commitment too, so refusal cannot be a corrupt-proof accident.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/blake2b"
)

// Rebuilding the independent SDK's roots checks the test encoder before it can
// be used as a mutable fixture for full Safe account storage.
func TestSafeCurrentStorageFixtureMatchesSdkCommitments(t *testing.T) {
	count := 0
	for _, vector := range safeCurrentStorageOracle(t) {
		if vector.Layout != "layout1" {
			continue
		}
		count++
		entries := map[string][]byte{}
		for _, entry := range vector.Entries {
			entries[string(common.FromHex(entry.Key))] = common.FromHex(*entry.Value)
		}
		root, _ := safeCurrentTestTrie(t, entries)
		if common.Hash(root).Hex() != vector.Root {
			t.Fatal("test encoder does not reproduce the independent SDK root", vector.Name)
		}
	}
	if count != 9 {
		t.Fatal("SDK commitment census differs")
	}
}

// All four published releases/variants prove a complete nine-word account;
// nonce zero instead proves the absence of slot five, without weakening scope.
func TestSafeCurrentStorageProvesExactPublishedSafe(t *testing.T) {
	for _, profile := range safeExecutionTestProfiles {
		f := newSafeCurrentProofFixture(t, profile.version, profile.variant)
		witness := safeCurrentTestWitness(t, f.entries)
		before := rootObjectHash(witness)
		result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness)
		if err != nil || result == nil || len(result.Words) != 9 || !result.CompleteFinalizedStorage || result.DeploymentHistoryVerified || result.CompletePendingVerified || result.SendAuthorized {
			t.Fatal("clean exact published Safe proof failed or overclaimed authority", profile, err)
		}
		if rootObjectHash(witness) != before || len(f.scope.keys()) != 14 {
			t.Fatal("Safe proof mutated input or changed its bounded key census")
		}
		f.scope.Nonce = "0"
		slot := common.BigToHash(big.NewInt(5))
		delete(f.entries, string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot)))
		witness = safeCurrentTestWitness(t, f.entries)
		if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err != nil || len(result.Words) != 8 {
			t.Fatal("zero Safe nonce did not prove absent storage", err)
		}
	}
}

// Orphan mappings leave linked-list getters unchanged, yet actual published
// authorization recognizes them. Complete native storage must still refuse.
func TestSafeCurrentStorageRejectsOrphanAndExtraAuthority(t *testing.T) {
	f := newSafeCurrentProofFixture(t, "1.4.1", "Safe")
	for _, entry := range []struct {
		name    string
		slot    int64
		address common.Address
		getter  string
	}{{name: "orphan module", slot: 1, address: common.Address{19: 211}, getter: "isModuleEnabled"},
		{name: "orphan owner", slot: 2, address: common.Address{19: 212}, getter: "isOwner"}} {
		slot := crypto.Keccak256Hash(common.LeftPadBytes(entry.address[:], 32), common.LeftPadBytes(big.NewInt(entry.slot).Bytes(), 32))
		value := common.BigToHash(big.NewInt(1))
		f.oracle.state.SetState(f.scope.Safe, slot, value)
		input, err := f.oracle.oracleAbi.Pack(entry.getter, entry.address)
		if err != nil {
			t.Fatal(err)
		}
		output, _, err := runtime.Call(f.scope.Safe, input, &f.oracle.vm)
		if err != nil || common.BytesToHash(output) != value {
			t.Fatal("actual Safe did not recognize malicious orphan authority", entry.name, err)
		}
		entries := maps.Clone(f.entries)
		entries[string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot))] = value[:]
		witness := safeCurrentTestWitness(t, entries)
		if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil || !strings.Contains(err.Error(), "orphan") {
			t.Fatal("complete Safe proof accepted hidden orphan authority", entry.name, err)
		}
		f.oracle.state.SetState(f.scope.Safe, slot, common.Hash{})
	}
	for _, slot := range []common.Hash{common.BigToHash(big.NewInt(6)), common.BigToHash(big.NewInt(8)), common.Hash{0x81}, common.HexToHash("0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8")} {
		entries := maps.Clone(f.entries)
		value := common.Hash{31: 1}
		entries[string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot))] = value[:]
		witness := safeCurrentTestWitness(t, entries)
		if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil {
			t.Fatal("complete Safe proof accepted an extra nonzero word", slot)
		}
	}
	for _, change := range []string{"missing singleton", "wrong nonce", "owner cycle", "owner padding", "wrong native concat"} {
		entries := maps.Clone(f.entries)
		slot := common.Hash{}
		key := string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot))
		switch change {
		case "missing singleton":
			delete(entries, key)
		case "wrong nonce":
			slot = common.BigToHash(big.NewInt(5))
			value := common.BigToHash(big.NewInt(18))
			entries[string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot))] = value[:]
		case "owner cycle", "owner padding":
			owner := f.scope.Owners[0]
			slot = crypto.Keccak256Hash(common.LeftPadBytes(owner[:], 32), common.LeftPadBytes([]byte{2}, 32))
			value := common.BytesToHash(owner[:])
			if change == "owner padding" {
				value[0] = 1
			}
			entries[string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot))] = value[:]
		case "wrong native concat":
			badKey := []byte(key)
			badKey[len(badKey)-48] ^= 1
			entries[string(badKey)] = entries[key]
			delete(entries, key)
		}
		witness := safeCurrentTestWitness(t, entries)
		if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil {
			t.Fatal("complete Safe proof accepted changed storage semantics", change)
		}
	}
}

// Authenticated bytes with wrong code/metadata are still the wrong account;
// runtime identity is independently proven under the same native state root.
func TestSafeCurrentStorageRejectsCodeAndMetadataSubstitution(t *testing.T) {
	f := newSafeCurrentProofFixture(t, "1.5.0", "SafeL2")
	for _, address := range []common.Address{f.scope.Safe, f.scope.Singleton} {
		for _, item := range []string{"AccountCodes", "AccountCodesMetadata"} {
			entries := maps.Clone(f.entries)
			key := string(safeCurrentTestNativeKey(item, address, nil))
			changed := slices.Clone(entries[key])
			changed[len(changed)-1] ^= 1
			entries[key] = changed
			witness := safeCurrentTestWitness(t, entries)
			if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil || !strings.Contains(err.Error(), "code") {
				t.Fatal("complete Safe proof accepted swapped code or code metadata", address, item, err)
			}
			delete(entries, key)
			witness = safeCurrentTestWitness(t, entries)
			if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil {
				t.Fatal("complete Safe proof accepted absent code or metadata", address, item)
			}
		}
	}
	entries := maps.Clone(f.entries)
	entries[":code"] = []byte("synthetic substituted native runtime")
	witness := safeCurrentTestWitness(t, entries)
	if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil || !strings.Contains(err.Error(), "runtime artifact") {
		t.Fatal("complete Safe proof accepted an unapproved native runtime", err)
	}
}

// Root/header/body identities cannot be selected by the witness or moved to a
// different height. A proof from a later valid snapshot cannot satisfy the pin.
func TestSafeCurrentStorageRejectsRootAndSnapshotSubstitution(t *testing.T) {
	f := newSafeCurrentProofFixture(t, "1.4.1", "Safe")
	witness := safeCurrentTestWitness(t, f.entries)
	for _, change := range []string{"root", "parent", "number", "at", "body", "missing branch"} {
		changed := witness
		switch change {
		case "root":
			changed.Header.StateRoot = common.Hash{7}.Hex()
		case "parent":
			changed.Header.ParentHash = common.Hash{8}.Hex()
		case "number":
			changed.Header.Number = "0x2c0"
		case "at":
			changed.At = common.Hash{9}.Hex()
		case "body":
			entries := maps.Clone(f.entries)
			entries[":code"] = []byte("synthetic changed snapshot")
			changed.Nodes = safeCurrentTestWitness(t, entries).Nodes
		case "missing branch":
			changed.Nodes = slices.DeleteFunc(slices.Clone(witness.Nodes), func(node string) bool {
				raw := common.FromHex(node)
				return len(raw) > 32 && bytes.Contains(raw, f.scope.Owners[0][:]) && common.Hash(blake2b.Sum256(raw)).Hex() != witness.Header.StateRoot
			})
			if len(changed.Nodes) == len(witness.Nodes) {
				t.Fatal("Safe omitted branch fixture removed no node")
			}
		}
		if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, changed); err == nil || result != nil {
			t.Fatal("Safe proof accepted substituted root, header or snapshot", change)
		}
	}
	f.scope.Runtime.RuntimeVersion.StateVersion = 2
	if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil {
		t.Fatal("Safe proof admitted an unreviewed native trie layout")
	}
}

// The owned route only sees explicit hashes; canonicality is rechecked after
// proof verification. Ordinary tip advancement cannot force repeated restarts.
func TestSafeCurrentStorageCollectorRetainsCanonicalSnapshot(t *testing.T) {
	f := newSafeCurrentProofFixture(t, "1.4.1", "Safe")
	witness := safeCurrentTestWitness(t, f.entries)
	var stateLock sync.Mutex
	fault, calls, tip := "", []string{}, uint64(703)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stateLock.Lock()
		defer stateLock.Unlock()
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		calls, tip = append(calls, request.Method), tip+1
		var result any
		switch request.Method {
		case "chain_getHeader":
			if len(request.Params) != 1 || string(request.Params[0]) != `"`+witness.At+`"` {
				t.Error("header read escaped pinned hash")
			}
			result = witness.Header
		case "state_getReadProof":
			var keys []string
			if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &keys) != nil || !slices.Equal(keys, f.scope.keys()) || string(request.Params[1]) != `"`+witness.At+`"` {
				t.Error("proof read escaped exact account/hash scope")
			}
			at := witness.At
			if fault == "snapshot" {
				at = common.Hash{31: 33}.Hex()
			}
			result = map[string]any{"at": at, "proof": witness.Nodes}
		case "chain_getBlockHash":
			if len(request.Params) != 1 || string(request.Params[0]) != "703" {
				t.Error("canonical recheck escaped pinned height")
			}
			result = witness.At
			if fault == "reorg" {
				result = common.Hash{31: 34}.Hex()
			}
		default:
			t.Error("Safe proof collector issued an unscoped method", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	head := chainIdentity{Schema: identitySchema, ObservedAt: "synthetic fixed time", RpcUrl: server.URL, NativeChain: "synthetic-chain", NodeVersion: "synthetic-node",
		GenesisHash: common.Hash{31: 1}.Hex(), EvmChainId: 43119, FinalizedNumber: 703, FinalizedHash: witness.At,
		RuntimeSpec: uint64(f.scope.Runtime.RuntimeVersion.SpecVersion), RuntimeTx: uint64(f.scope.Runtime.RuntimeVersion.TransactionVersion), runtimeVersion: f.scope.Runtime.RuntimeVersion}
	if result, err := client.readSafeCurrentStorage(t.Context(), f.scope, head); err != nil || result == nil {
		t.Fatal("advancing finalized tip blocked pinned Safe proof", err)
	}
	stateLock.Lock()
	matched := slices.Equal(calls, []string{"chain_getHeader", "state_getReadProof", "chain_getBlockHash"}) && tip == 706
	stateLock.Unlock()
	if !matched {
		t.Fatal("Safe proof did not retain one snapshot while tip advanced")
	}
	for _, changed := range []string{"snapshot", "reorg"} {
		stateLock.Lock()
		fault = changed
		stateLock.Unlock()
		if result, err := client.readSafeCurrentStorage(t.Context(), f.scope, head); err == nil || result != nil {
			t.Fatal("Safe proof collector accepted changed snapshot or canonical hash", changed)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := client.readSafeCurrentStorage(ctx, f.scope, head); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal("canceled Safe proof collector returned an observation", err)
	}
	var ignored any
	if err := client.call(t.Context(), "state_getReadProof", []any{[]string{"0x" + hex.EncodeToString([]byte(":code"))}, witness.At}, &ignored); err == nil {
		t.Fatal("specialized native proof extended the general RPC allowlist")
	}
}
