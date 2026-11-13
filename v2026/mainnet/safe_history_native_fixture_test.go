// Native trace fixtures use the pinned SDK's externally tagged JSON shape and
// independently committed parent tries. No fixture executes a live native node.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Native-only faults run at exact RPC boundaries; the underlying archive
// fixture retains its own finality, continuity and complete-body checks.
type safeHistoryNativeFixture struct {
	archive   *safeHistoryFixture
	parents   map[string]safeCurrentStorageWitness
	codes     map[string][]byte
	stateLock sync.Mutex
	counts    map[string]int
	fault     func(*http.Request, string, int, any) (any, error)
}

// Every child has different runtime bytes in its resulting state, so choosing
// the child instead of the actual execution parent deterministically fails.
func newSafeHistoryNativeFixture(t *testing.T) (*rpcClient, *safeHistoryNativeFixture) {
	t.Helper()
	return newSafeHistoryNativeSizedFixture(t, 0)
}

// Large valid runtime values exercise the aggregate witness bound independently
// of the per-proof and per-trace limits.
func newSafeHistoryNativeSizedFixture(t *testing.T, runtimeBytes int) (*rpcClient, *safeHistoryNativeFixture) {
	t.Helper()
	client, archive := newSafeHistoryFixture(t)
	fixture := &safeHistoryNativeFixture{archive: archive, parents: map[string]safeCurrentStorageWitness{}, codes: map[string][]byte{}, counts: map[string]int{}}
	parentHash := testGenesisHash
	for i := 0; i <= len(archive.witnesses); i++ {
		code := []byte(fmt.Sprintf("synthetic native runtime artifact %d with external trie value", i))
		if runtimeBytes > len(code) {
			code = append(code, bytes.Repeat([]byte{byte(i + 1)}, runtimeBytes-len(code))...)
		}
		root, nodes := safeCurrentTestTrie(t, map[string][]byte{":code": code, "unrelated-native-state": {1, 2, 3}})
		witness := safeHistoryWitness{Native: safeHistoryBoundary{Number: 99}, NativeHeader: rootReceiptHeader{
			ParentHash: parentHash, Number: "0x63", StateRoot: common.Hash(root).Hex(), ExtrinsicsRoot: common.Hash(root).Hex()}}
		witness.NativeHeader.Digest.Logs = []string{}
		if i > 0 {
			witness = archive.witnesses[i-1]
			witness.NativeHeader.ParentHash, witness.NativeHeader.StateRoot = parentHash, common.Hash(root).Hex()
		}
		safeHistoryTestSealNative(t, &witness)
		parentHash = witness.Native.Hash
		if i > 0 {
			archive.witnesses[i-1] = witness
		}
		fixture.parents[witness.Native.Hash] = safeCurrentStorageWitness{At: witness.Native.Hash, Header: witness.NativeHeader, Nodes: nodes}
		fixture.codes[witness.Native.Hash] = code
	}
	archive.scope.From, archive.scope.Through = archive.witnesses[0].Native, archive.witnesses[1].Native
	client.httpClient.Transport = roundTripFunc(fixture.roundTrip)
	return client, fixture
}

// Independent test key encoding supplies the exact native map prefixes. The
// fixture refuses wrong filters instead of echoing a production request back.
func (self *safeHistoryNativeFixture) filters() string {
	keys := []string{"3a636f6465", "3a65787472696e7369635f696e646578"}
	for _, item := range []string{"AccountCodes", "AccountCodesMetadata", "AccountStorages"} {
		keys = append(keys, hex.EncodeToString(safeCurrentTestNativeKey(item, common.HexToAddress(self.archive.scope.Safe), nil)))
	}
	return strings.Join(keys, ",")
}

// A synthetic native hook can write this key and a surrounding transaction can
// roll it back. Neither that transaction boundary nor a prefix removal is in
// the SDK's keyed trace response; these events are only retained observations.
func (self *safeHistoryNativeFixture) trace(block safeHistoryWitness) map[string]any {
	slot := common.HexToHash("0x42")
	key := hex.EncodeToString(safeCurrentTestNativeKey("AccountStorages", common.HexToAddress(self.archive.scope.Safe), &slot))
	event := func(method, key, value string) any {
		return map[string]any{"target": "state", "parentId": nil, "data": map[string]any{"stringValues": map[string]string{
			"method": method, "key": key, "value": value, "ext_id": "01000000"}}}
	}
	return map[string]any{"blockTrace": map[string]any{
		"blockHash": block.Native.Hash[2:], "parentHash": block.NativeHeader.ParentHash[2:], "tracingTargets": "state", "storageKeys": self.filters(), "methods": "",
		"spans":  []any{map[string]any{"id": 1, "parentId": nil, "name": "synthetic-host-span", "target": "state", "wasm": false}},
		"events": []any{event("Get", "3a636f6465", "Some(73796e746865746963)"), event("Put", key, "Some(01)"), event("Put", key, "None")},
	}}
}

// Only actual pinned SDK methods are exposed. Parent selection and exact
// trace arguments are checked before producing any caller-controlled fault.
func (self *safeHistoryNativeFixture) roundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var call struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		return nil, err
	}
	var result any
	switch call.Method {
	case "chain_getHeader":
		if len(call.Params) == 1 && call.Params[0] == self.archive.witnesses[0].NativeHeader.ParentHash {
			result = self.parents[call.Params[0].(string)].Header
		}
	case "state_getReadProof":
		if len(call.Params) != 2 {
			return nil, fmt.Errorf("native proof parameters differ")
		}
		keys, ok := call.Params[0].([]any)
		if !ok || len(keys) != 1 || keys[0] != "0x3a636f6465" {
			return nil, fmt.Errorf("native proof requested another key")
		}
		parent, ok := self.parents[call.Params[1].(string)]
		if !ok {
			return nil, fmt.Errorf("native proof requested an unknown parent")
		}
		result = map[string]any{"at": parent.At, "proof": parent.Nodes}
	case "state_traceBlock":
		if len(call.Params) != 4 || call.Params[1] != "state" || call.Params[2] != self.filters() || call.Params[3] != "" {
			return nil, fmt.Errorf("native trace changed explicit filters")
		}
		for _, block := range self.archive.witnesses {
			if call.Params[0] == block.Native.Hash {
				result = self.trace(block)
			}
		}
		if result == nil {
			return nil, fmt.Errorf("native trace requested an unknown block")
		}
	}
	if result == nil {
		return self.archive.roundTrip(request)
	}
	self.stateLock.Lock()
	self.counts[call.Method]++
	count := self.counts[call.Method]
	self.stateLock.Unlock()
	if self.fault != nil {
		result, err = self.fault(request, call.Method, count, result)
		if err != nil {
			return nil, err
		}
	}
	envelope := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
	if failure, ok := result.(mappingFixtureRpcError); ok {
		delete(envelope, "result")
		envelope["error"] = map[string]any{"code": failure.code, "message": "synthetic unavailable native archive method"}
	}
	raw, err = json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
}
