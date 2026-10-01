// Current-state fixtures retain real original deployments while faults target
// only the later snapshot. No Safe capability or production route is installed.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"math/big"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Trace actual HTTP selectors and request deadlines without changing responses.
type bootstrapContractCurrentTransport struct {
	stateLock sync.Mutex
	base      http.RoundTripper
	blocks    []any
	deadlines []time.Time
}

// Each admitted code/getter/slot read must carry the same exact canonical hash
// selector and receive its own finite budget under the caller's cancellation.
func (self *bootstrapContractCurrentTransport) RoundTrip(request *http.Request) (*http.Response, error) {
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
	if call.Method == "eth_getCode" || call.Method == "eth_call" || call.Method == "eth_getStorageAt" {
		deadline, _ := request.Context().Deadline()
		self.stateLock.Lock()
		self.blocks = append(self.blocks, call.Params[len(call.Params)-1])
		self.deadlines = append(self.deadlines, deadline)
		self.stateLock.Unlock()
	}
	return self.base.RoundTrip(request)
}

// Later EVM epochs change the clock, never the original policy's start block.
func TestBootstrapContractCurrentClockUsesSelectedEvmHeight(t *testing.T) {
	f, evidence := newBootstrapContractRoleFixture(t)
	plan := evidence.priorPlan(4)
	original := append([]contractGetter(nil), plan.Getters...)
	receipt := evmCreateReceipt{BlockNumber: 12345, NativeNumber: 90000}
	evmBlock := receipt.BlockNumber + 2*plan.ProxyConstructor.ApprovedPolicy.EpochBlocks + 17
	getters, err := bootstrapContractCurrentGetters(plan, receipt, evmBlock)
	if err != nil {
		t.Fatal(err)
	}
	binding := stabi.NewSTCoordinator()
	values := map[string]string{}
	for _, getter := range getters {
		values[getter.Data] = getter.Expected
	}
	if values["0x"+hex.EncodeToString(binding.PackCurrentEpoch())] != common.BigToHash(big.NewInt(2)).Hex() ||
		values["0x"+hex.EncodeToString(binding.PackEpochStartBlock(big.NewInt(0)))] != common.BigToHash(new(big.Int).SetUint64(receipt.BlockNumber)).Hex() ||
		!reflect.DeepEqual(original, plan.Getters) || !maps.Equal(f.contracts.counts, map[string]int{}) {
		t.Fatal("current bootstrap clock lost its EVM snapshot or original policy height")
	}
	if _, err := bootstrapContractCurrentGetters(plan, receipt, receipt.BlockNumber-1); err == nil {
		t.Fatal("current bootstrap state admitted a predeployment EVM snapshot")
	}
	plan.ProxyConstructor.ApprovedPolicy.EpochBlocks = 0
	if _, err := bootstrapContractCurrentGetters(plan, receipt, evmBlock); err == nil {
		t.Fatal("current bootstrap clock admitted an absent policy period")
	}
}

// Admission cannot repair absent history, substitute signed scope or import any
// signing/send flag. These refusals precede current or historical chain reads.
func TestBootstrapContractCurrentCommandRequiresOriginalReadScope(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	f.result(t, "apply")
	before, counts := f.journals(t), maps.Clone(f.contracts.counts)
	args := []string{"bootstrap-chain", "contract-current-state", "--config", f.path, "--run-dir", f.config.RunDirectory,
		"--accept-plan-hash", f.preparation.Plan.ContentHash, "--online"}
	for _, suffix := range [][]string{nil, {"--submit"}, {"--signed-transaction", "synthetic-absent"}, {"--pending"},
		{"--online=false"}, {"--accept-plan-hash", "sha256:" + strings.Repeat("f", 64)}, {"--run-dir", t.TempDir()}} {
		var stdout, stderr bytes.Buffer
		if code := runMain(t.Context(), append(append([]string(nil), args...), suffix...), &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatal("current contract command admitted absent or changed read authority", suffix, code, stderr.String())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := runMain(ctx, args, io.Discard, io.Discard); code != 2 {
		t.Fatal("canceled current contract observation reported success", code)
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !maps.Equal(counts, f.contracts.counts) {
		t.Fatal("current contract preflight changed custody or contacted the chain")
	}
}

// One genuine graph covers the public composition, immutable proof snapshot,
// per-read budgets, all five accounts' guards, scoped pointer observation and a
// changed final mapping. Faults never replace a historical inclusion or journal.
func TestBootstrapContractCurrentCommandChecksPinnedBootstrapState(t *testing.T) {
	f, evidence := newBootstrapContractRoleFixture(t)
	bootstrapSuccessorCommandTestComplete(t, f)
	before := bootstrapContractTestJournals(t, f.config.RunDirectory)
	maps.Copy(before, f.journals(t))
	chain := f.contracts
	baseOverride := chain.override
	setFault := func(fault func(string, []any, any) any) {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		chain.override = func(method string, params []any, result any) any {
			result = baseOverride(method, params, result)
			if method == "chain_getFinalizedHead" {
				chain.advanceEmpty()
				result = chain.hashes[chain.head]
			}
			if fault != nil {
				return fault(method, params, result)
			}
			return result
		}
	}
	setFault(nil)
	args := []string{"bootstrap-chain", "contract-current-state", "--config", f.path, "--run-dir", f.config.RunDirectory,
		"--accept-plan-hash", f.preparation.Plan.ContentHash, "--online"}
	var stdout, stderr bytes.Buffer
	if code := runMain(t.Context(), args, &stdout, &stderr); code != 0 {
		t.Fatal("public current bootstrap observation refused advancing head", code, stderr.String())
	}
	var result bootstrapContractCurrentAdmission
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	seal := result.ContentHash
	result.ContentHash = ""
	snapshot, err := sealFinalizedMapping(result.Snapshot.Mapping)
	if err != nil || snapshot.ContentHash != result.Snapshot.ContentHash || seal != rootObjectHash(result) ||
		result.Schema != bootstrapContractCurrentSchema || result.Profile != "original-five-account-bootstrap-fields-v1" ||
		result.StateAuthority != "owned-rpc-assertion" || !result.CurrentBootstrapStateMatches || len(result.Fields.Accounts) != 5 ||
		!result.HistoricalPrefix.CanonicalReceiptsVerified || len(result.HistoricalPrefix.Actions) != 8 || result.HistoricalPrefix.RetainedAttempts != 8 ||
		result.Fields.PolicyStartBlock != result.HistoricalPrefix.Actions[4].Receipt.BlockNumber || result.Fields.EvidenceState != "unset" ||
		result.Fields.EvidencePointer != (common.Address{}) || result.CompleteStorageVerified || result.EvidenceAnchorVerified ||
		result.SafeAuthorityVerified || result.InstallationComplete || result.ActivationReady || result.NetworkEffects ||
		result.Snapshot.Mapping.RuntimeSourceProven || !reflect.DeepEqual(result.PendingChainPhases, bootstrapChainPendingPhases()) ||
		result.Snapshot.Mapping.Identity.FinalizedNumber >= result.CheckedThroughNativeBlock ||
		result.Snapshot.Mapping.Identity.FinalizedHash == result.CheckedThroughNativeHash {
		t.Fatal("current bootstrap report changed its proof block or granted wider authority", result, err)
	}
	scope, err := openBootstrapContractReceiptScope(t.Context(), f.path, f.config.RunDirectory, f.preparation.Plan.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.close() })
	transport := &bootstrapContractCurrentTransport{base: scope.chain.client.httpClient.Transport}
	scope.chain.client.httpClient.Transport = transport
	if fields, err := scope.currentFields(t.Context(), result.Snapshot.Mapping); err != nil || rootObjectHash(fields) != rootObjectHash(result.Fields) {
		t.Fatal("current bootstrap snapshot changed under later finality", err)
	}
	func() {
		transport.stateLock.Lock()
		defer transport.stateLock.Unlock()
		if len(transport.blocks) < 50 || len(transport.blocks) != len(transport.deadlines) {
			t.Fatal("current bootstrap read census is incomplete", len(transport.blocks))
		}
		for i, selected := range transport.blocks {
			block, ok := selected.(map[string]any)
			if !ok || len(block) != 2 || block["blockHash"] != result.Snapshot.Mapping.EvmHeader.Hash || block["requireCanonical"] != true {
				t.Fatal("current bootstrap reads escaped their exact canonical snapshot", selected)
			}
			if transport.deadlines[i].IsZero() || i > 0 && !transport.deadlines[i].After(transport.deadlines[i-1]) {
				t.Fatal("current bootstrap reads shared an aggregate retry budget", i)
			}
		}
	}()
	proxy := evidence.priorPlan(4)
	coordinator := stabi.NewSTCoordinator()
	targets := []struct {
		name    string
		method  string
		address common.Address
		key     string
		value   string
	}{
		{name: "owner getter", method: "eth_call", address: proxy.Address, key: "0x" + hex.EncodeToString(coordinator.PackOwner()), value: (common.Hash{}).Hex()},
		{name: "implementation slot", method: "eth_getStorageAt", address: proxy.Address, key: proxy.Storage[0].Slot, value: (common.Hash{}).Hex()},
		{name: "admin slot", method: "eth_getStorageAt", address: proxy.Address, key: proxy.Storage[1].Slot, value: common.HexToHash("0x01").Hex()},
		{name: "policy getter", method: "eth_call", address: proxy.Address, key: "0x" + hex.EncodeToString(coordinator.PackPolicyByIndex(big.NewInt(0))), value: "0x"},
		{name: "clock getter", method: "eth_call", address: proxy.Address, key: "0x" + hex.EncodeToString(coordinator.PackCurrentEpoch()), value: common.HexToHash("0xffffff").Hex()},
		{name: "activity getter", method: "eth_call", address: proxy.Address, key: "0x" + hex.EncodeToString(coordinator.PackOperatorCount()), value: common.HexToHash("0x01").Hex()},
		{name: "vault coordinator slot", method: "eth_getStorageAt", address: evidence.priorPlan(6).Address, key: evidence.priorPlan(6).Storage[0].Slot, value: (common.Hash{}).Hex()},
		{name: "reserve recorder", method: "eth_call", address: evidence.priorPlan(5).Address, key: "0x" + hex.EncodeToString(stabi.NewSTReserveSink().PackRecorder()), value: (common.Hash{}).Hex()},
		{name: "evidence domain", method: "eth_call", address: evidence.Address, key: "0x" + hex.EncodeToString(stabi.NewSTValidatorEvidence().PackGenesisHash()), value: (common.Hash{}).Hex()},
		{name: "foreign evidence pointer", method: "eth_call", address: proxy.Address, key: "0x" + hex.EncodeToString(coordinator.PackValidatorEvidence()), value: common.HexToHash("0x42").Hex()},
	}
	for _, index := range []int{2, 4, 5, 6, 7} {
		targets = append(targets, struct {
			name    string
			method  string
			address common.Address
			key     string
			value   string
		}{name: "runtime " + scope.plans[index].Config.Plan.Actions[index].Id, method: "eth_getCode", address: scope.plans[index].Address, value: "0x00"})
	}
	for _, target := range targets {
		hit := false
		setFault(func(method string, params []any, output any) any {
			if method != target.method {
				return output
			}
			block, ok := params[len(params)-1].(map[string]any)
			if !ok || block["blockHash"] != result.Snapshot.Mapping.EvmHeader.Hash {
				return output
			}
			address, key := "", ""
			if method == "eth_call" {
				call := params[0].(map[string]any)
				address, key = call["to"].(string), call["data"].(string)
			} else {
				address = params[0].(string)
				if method == "eth_getStorageAt" {
					key = params[1].(string)
				}
			}
			if common.HexToAddress(address) == target.address && key == target.key {
				hit = true
				return target.value
			}
			return output
		})
		_, err := scope.currentFields(t.Context(), result.Snapshot.Mapping)
		chain.stateLock.Lock()
		observed := hit
		chain.stateLock.Unlock()
		if !observed || err == nil {
			t.Fatal("current bootstrap admission accepted "+target.name, observed, err)
		}
	}
	setFault(nil)
	// A later real local owner call changes only the expected pointer. It has no
	// supplied anchor receipt or Safe provenance, so observation grants neither.
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		vm := chain.vm
		vm.Origin, vm.GasLimit = proxy.ProxyConstructor.Owner, 2_000_000
		if _, _, err := runtime.Call(proxy.Address, coordinator.PackFixValidatorEvidence(evidence.Address), &vm); err != nil {
			t.Fatal("local expected evidence pointer", err)
		}
		chain.advanceEmpty()
	}()
	head, err := scope.chain.client.readIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := scope.chain.client.readFinalizedMappingAtIdentity(t.Context(), head)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := scope.currentFields(t.Context(), mapping)
	if err != nil || fields.EvidencePointer != evidence.Address || fields.EvidenceState != "expected-journal-observed" {
		t.Fatal("current expected pointer was erased or treated as history", fields, err)
	}
	if err := scope.close(); err != nil {
		t.Fatal(err)
	}
	// Change the corroborating EVM mapping only after the final current-state
	// slot read. Original receipts and both native heads remain unchanged.
	armed := false
	setFault(func(method string, params []any, output any) any {
		if method == "eth_getStorageAt" && params[0] == evidence.Address.Hex() && params[1] == evidence.Storage[1].Slot {
			if block, ok := params[2].(map[string]any); ok && block["blockHash"] != scope.records[7].Receipt.BlockHash {
				armed = true
			}
		}
		if armed && method == "eth_getBlockByNumber" {
			block := maps.Clone(output.(map[string]any))
			block["hash"] = "0x" + strings.Repeat("ef", 32)
			return block
		}
		return output
	})
	stdout.Reset()
	stderr.Reset()
	code := runMain(t.Context(), args, &stdout, &stderr)
	chain.stateLock.Lock()
	observed := armed
	chain.stateLock.Unlock()
	if !observed || code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "selected mapping changed") {
		t.Fatal("current bootstrap admission ignored changed final mapping", observed, code, stderr.String())
	}
	setFault(nil)
	after := bootstrapContractTestJournals(t, f.config.RunDirectory)
	maps.Copy(after, f.journals(t))
	chain.stateLock.Lock()
	writes := len(chain.writes)
	chain.stateLock.Unlock()
	if !reflect.DeepEqual(before, after) || writes != 8 {
		t.Fatal("current bootstrap observation changed original custody or submitted", writes)
	}
}
