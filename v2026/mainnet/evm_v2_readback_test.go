// Settlement v2 readback is exercised through real local HTTP against exact
// projections. A fault changes one answer; any other method fails the test.
package main

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The stub's answers, read keys and selectors, shared with its handler.
type evmV2ReadbackStub struct {
	stateLock sync.Mutex
	answers   map[string]string
	selectors map[string]bool
	fault     func(key string, selector any, value string) any
}

// One answer per contract fact: code, each getter and each storage word.
func evmV2ReadbackKey(method string, address common.Address, item string) string {
	return method + "|" + address.Hex() + "|" + item
}

// The stub serves exact facts of the given projections at every selector.
func newEvmV2ReadbackChain(t *testing.T, contracts ...*evmCreatePlan) (*evmOwnedChain, *evmV2ReadbackStub) {
	t.Helper()
	stub := &evmV2ReadbackStub{answers: map[string]string{}, selectors: map[string]bool{}}
	for _, contract := range contracts {
		stub.answers[evmV2ReadbackKey("eth_getCode", contract.Address, "")] = "0x" + hex.EncodeToString(contract.Runtime)
		for _, getter := range contract.Getters {
			stub.answers[evmV2ReadbackKey("eth_call", common.HexToAddress(getter.target(contract.Address)), getter.Data)] = getter.Expected
		}
		for _, word := range contract.Storage {
			stub.answers[evmV2ReadbackKey("eth_getStorageAt", contract.Address, word.Slot)] = word.Expected
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil || len(call.Params) == 0 {
			http.Error(writer, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		var key string
		switch call.Method {
		case "eth_getCode":
			key = evmV2ReadbackKey(call.Method, common.HexToAddress(call.Params[0].(string)), "")
		case "eth_call":
			input := call.Params[0].(map[string]any)
			key = evmV2ReadbackKey(call.Method, common.HexToAddress(input["to"].(string)), input["data"].(string))
		case "eth_getStorageAt":
			key = evmV2ReadbackKey(call.Method, common.HexToAddress(call.Params[0].(string)), call.Params[1].(string))
		}
		selector := call.Params[len(call.Params)-1]
		stub.stateLock.Lock()
		value, ok := stub.answers[key]
		raw, _ := json.Marshal(selector)
		stub.selectors[string(raw)] = true
		fault := stub.fault
		stub.stateLock.Unlock()
		if !ok {
			t.Errorf("unexpected readback %s", key)
			http.Error(writer, "unexpected readback", http.StatusBadRequest)
			return
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": 1, "result": value}
		if fault != nil {
			if changed := fault(key, selector, value); changed != nil {
				reply["result"] = changed
				if failure, isFailure := changed.(map[string]any); isFailure {
					delete(reply, "result")
					reply["error"] = failure
				}
			}
		}
		_ = json.NewEncoder(writer).Encode(reply)
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	return &evmOwnedChain{client: client}, stub
}

// A changed final hex digit is the smallest wrong answer.
func evmV2ReadbackFlip(value string) string {
	last := "1"
	if strings.HasSuffix(value, "1") {
		last = "2"
	}
	return value[:len(value)-1] + last
}

// The canonical inclusion selector used by receipt authentication.
var evmV2ReadbackBlock = map[string]any{"blockHash": common.Hash{7}.Hex(), "requireCanonical": true}

// SV-8, A7: completion reads back every getter the plan names for vault2 and
// STSettlement2, through both addresses, plus the reviewed storage words.
// Vault2's escrow flag, its getter and slot zero, is pinned from the escrow
// registration on, because the back-to-back registration can share vault2's
// block. Once registered, vault2's escrow position must show no collateral (A3).
func TestEvmV2ReadbackProjectionsNameEveryGetter(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	vault, implementation, proxy := graph[0].Address, graph[2].Address, graph[3].Address
	sender := f.config.Plan.Actions[0].Sender
	contract, settlement := stabi.NewSTSettlementVault2(), stabi.NewSTSettlement2()
	word := func(value int64) string { return common.BigToHash(big.NewInt(value)).Hex() }
	addressWord := func(address common.Address) string { return common.BytesToHash(address[:]).Hex() }
	call := func(data []byte) string { return "0x" + hex.EncodeToString(data) }
	vaultGetters := func(coordinator common.Address, registered bool) []contractGetter {
		getters := []contractGetter{
			{Data: call(contract.PackNetuid()), Expected: word(25)},
			{Data: call(contract.PackEscrowHotkey()), Expected: common.Hash(evmV2TestHotkey).Hex()},
			{Data: call(contract.PackSelfColdkey()), Expected: common.Hash(ss58.EvmMirrorPubkey(vault)).Hex()},
			{Data: call(contract.PackBootstrap()), Expected: addressWord(sender)},
			{Data: call(contract.PackDemandFeeWei()), Expected: common.BigToHash(evmV2TestFee).Hex()},
			{Data: call(contract.PackCHALLENGEBLOCKS()), Expected: word(14_400)},
			{Data: call(contract.PackCoordinator()), Expected: addressWord(coordinator)},
		}
		if registered {
			getters = append(getters, contractGetter{Data: call(contract.PackEscrowRegistered()), Expected: word(1)})
		}
		getters = append(getters, []contractGetter{
			{Data: call(contract.PackTotalCaptured()), Expected: word(0)},
			{Data: call(contract.PackTotalPaid()), Expected: word(0)},
			{Data: call(contract.PackEscrowAccounted()), Expected: word(0)},
			{Data: call(contract.PackConservationHolds()), Expected: word(1)},
		}...)
		if registered {
			getters = append(getters, contractGetter{To: "0x0000000000000000000000000000000000000805", Data: "0x" + hex.EncodeToString(crypto.Keccak256([]byte("getMinerCollateral(uint16,bytes32,bytes32)"))[:4]) + strings.TrimPrefix(word(25), "0x") + strings.TrimPrefix(common.Hash(evmV2TestHotkey).Hex(), "0x") + strings.TrimPrefix(common.Hash(ss58.EvmMirrorPubkey(vault)).Hex(), "0x"), Expected: "0x" + strings.Repeat("0", 320)})
		}
		return getters
	}
	slot := func(value int64) string { return common.BigToHash(big.NewInt(value)).Hex() }
	zero := (common.Hash{}).Hex()
	vaultStorage := func(coordinator common.Address, registered bool) []contractStorageWord {
		storage := []contractStorageWord{}
		if registered {
			packed := common.BytesToHash(coordinator[:])
			packed[11] = 1
			storage = append(storage, contractStorageWord{Slot: zero, Expected: packed.Hex()})
		}
		return append(storage, contractStorageWord{Slot: slot(1), Expected: zero})
	}
	for index, expected := range map[int]struct {
		getters []contractGetter
		storage []contractStorageWord
	}{0: {vaultGetters(common.Address{}, false), vaultStorage(common.Address{}, false)}, 1: {vaultGetters(common.Address{}, true), vaultStorage(common.Address{}, true)}, 4: {vaultGetters(proxy, true), vaultStorage(proxy, true)}} {
		if graph[index].Address != vault || rootObjectHash(graph[index].Getters) != rootObjectHash(expected.getters) || rootObjectHash(graph[index].Storage) != rootObjectHash(expected.storage) || crypto.Keccak256Hash(graph[index].Runtime) != crypto.Keccak256Hash(graph[0].Runtime) {
			t.Fatalf("vault2 projection %d: %+v %+v", index, graph[index].Getters, graph[index].Storage)
		}
	}
	addresses, _ := abi.NewType("address[]", "", nil)
	text, _ := abi.NewType("string", "", nil)
	list := func(values ...common.Address) string {
		raw, err := abi.Arguments{{Type: addresses}}.Pack(values)
		if err != nil {
			t.Fatal(err)
		}
		return "0x" + hex.EncodeToString(raw)
	}
	version, err := abi.Arguments{{Type: text}}.Pack("5.0.0")
	if err != nil {
		t.Fatal(err)
	}
	settlementGetters := func(vault, v1, owner common.Address, auditors string, quorum, setVersion int64) []contractGetter {
		return []contractGetter{
			{Data: call(settlement.PackVault()), Expected: addressWord(vault)},
			{Data: call(settlement.PackV1Coordinator()), Expected: addressWord(v1)},
			{Data: call(settlement.PackAuditors()), Expected: auditors},
			{Data: call(settlement.PackQuorum()), Expected: word(quorum)},
			{Data: call(settlement.PackAuditorSetVersion()), Expected: word(setVersion)},
			{Data: "0x8da5cb5b", Expected: addressWord(owner)},
			{Data: "0xad3cb1cc", Expected: "0x" + hex.EncodeToString(version)},
		}
	}
	implementationGetters := append(settlementGetters(common.Address{}, common.Address{}, common.Address{}, list(), 0, 0), contractGetter{Data: "0x52d1902d", Expected: "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"})
	implementationStorage := []contractStorageWord{{Slot: "0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00", Expected: "0x000000000000000000000000000000000000000000000000ffffffffffffffff"}, {Slot: "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", Expected: zero},
		{Slot: slot(0), Expected: zero}, {Slot: slot(1), Expected: zero}, {Slot: slot(2), Expected: zero}, {Slot: slot(3), Expected: zero}, {Slot: slot(4), Expected: zero}}
	if implementation != crypto.CreateAddress(sender, 2) || rootObjectHash(graph[2].Getters) != rootObjectHash(implementationGetters) || rootObjectHash(graph[2].Storage) != rootObjectHash(implementationStorage) {
		t.Fatalf("implementation projection: %+v %+v", graph[2].Getters, graph[2].Storage)
	}
	proxyStorage := []contractStorageWord{
		{Slot: "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", Expected: addressWord(implementation)},
		{Slot: "0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103", Expected: zero},
		{Slot: "0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50", Expected: zero},
		{Slot: "0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00", Expected: word(1)},
		{Slot: "0x9016d09d72d40fdae2fd8ceac6b6234c7706214fd39c1cd1e609a0528c199300", Expected: addressWord(evmV2TestOwner)},
		{Slot: slot(0), Expected: addressWord(evmV2TestV1)}, {Slot: slot(1), Expected: addressWord(vault)}, {Slot: slot(2), Expected: word(1)}, {Slot: slot(3), Expected: word(1)}, {Slot: slot(4), Expected: word(1)},
		{Slot: "0x405787fa12a823e0f2b7631cc41b3ba8828b3321ca811111fa75cd3aa3bb5ace", Expected: addressWord(evmV2TestAuditor)},
	}
	if proxy != crypto.CreateAddress(sender, 3) || rootObjectHash(graph[3].Getters) != rootObjectHash(settlementGetters(vault, evmV2TestV1, evmV2TestOwner, list(evmV2TestAuditor), 1, 1)) || rootObjectHash(graph[3].Storage) != rootObjectHash(proxyStorage) {
		t.Fatalf("proxy projection: %+v %+v", graph[3].Getters, graph[3].Storage)
	}
}

// SV-8, A7: every readback calls each argument-free view of the release ABI,
// except vault2's liveEscrowStake, which anyone can raise by transferring
// stake to the escrow position, vault2's escrowRegistered at its creation,
// which the back-to-back registration can set in the same block, and the
// proxy's proxiableUUID, which UUPS refuses through a proxy and the
// implementation's readback calls instead.
func TestEvmV2ReadbackCoversEveryArgumentFreeView(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	release := evmV2TestRelease(t)
	for index, check := range []struct {
		artifact string
		plan     *evmCreatePlan
		skipped  []string
	}{{"SettlementVault2", graph[0], []string{"liveEscrowStake", "escrowRegistered"}}, {"SettlementVault2", graph[1], []string{"liveEscrowStake"}}, {"SettlementVault2", graph[4], []string{"liveEscrowStake"}}, {"Settlement2", graph[2], nil}, {"Settlement2", graph[3], []string{"proxiableUUID"}}} {
		parsed, err := abi.JSON(strings.NewReader(evmV2TestArtifactNamed(t, release, check.artifact).Abi))
		if err != nil {
			t.Fatal(err)
		}
		read := map[string]bool{}
		for _, getter := range check.plan.Getters {
			if getter.To == "" {
				read[getter.Data] = true
			}
		}
		views := 0
		for name, method := range parsed.Methods {
			if !method.IsConstant() || len(method.Inputs) != 0 {
				continue
			}
			skipped := slices.Contains(check.skipped, name)
			if read["0x"+hex.EncodeToString(method.ID)] == skipped {
				t.Fatalf("readback %d of %s.%s differs from its skip list", index, check.artifact, name)
			}
			if !skipped {
				views++
			}
		}
		if views != len(read) {
			t.Fatalf("readback %d calls %d of %d views", index, len(read), views)
		}
	}
}

// SV-8: completion readback of vault2, the proxy and the implementation at
// one canonical selector passes only on exact facts; each mismatch is refused.
func TestEvmV2ReadbackRefusesEachMismatch(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	link := *graph[4]
	chain, stub := newEvmV2ReadbackChain(t, graph[4], graph[3], graph[2])
	complete := func() error {
		if err := chain.readback(t.Context(), link, evmV2ReadbackBlock); err != nil {
			return err
		}
		return chain.readbackSettlement2(t.Context(), link, evmV2ReadbackBlock)
	}
	if err := complete(); err != nil {
		t.Fatalf("exact completion readback refused: %v", err)
	}
	cases := 0
	for _, contract := range []*evmCreatePlan{graph[4], graph[3], graph[2]} {
		faults := map[string]string{evmV2ReadbackKey("eth_getCode", contract.Address, ""): contract.actionName() + " readback runtime differs"}
		for _, getter := range contract.Getters {
			faults[evmV2ReadbackKey("eth_call", common.HexToAddress(getter.target(contract.Address)), getter.Data)] = contract.actionName() + " readback getter " + getter.Data + " differs"
		}
		for _, word := range contract.Storage {
			faults[evmV2ReadbackKey("eth_getStorageAt", contract.Address, word.Slot)] = contract.actionName() + " readback storage slot " + word.Slot + " differs"
		}
		for target, expected := range faults {
			stub.fault = func(key string, _ any, value string) any {
				if key == target {
					return evmV2ReadbackFlip(value)
				}
				return nil
			}
			if err := complete(); err == nil || err.Error() != expected {
				t.Fatalf("readback mismatch %s escaped: %v", target, err)
			}
			cases++
		}
	}
	if cases != 1+13+2+1+7+11+1+8+7 {
		t.Fatalf("readback covered %d facts", cases)
	}
	for _, method := range []string{"eth_getCode", "eth_call", "eth_getStorageAt"} {
		stub.fault = func(key string, _ any, _ string) any {
			if strings.HasPrefix(key, method+"|") {
				return map[string]any{"code": -32602, "message": "synthetic archive gap"}
			}
			return nil
		}
		if err := complete(); err == nil || !strings.Contains(err.Error(), "synthetic archive gap") {
			t.Fatalf("missing %s archive fact completed readback: %v", method, err)
		}
	}
}

// SV-8: the binding's admission needs a registered, unbound vault2 and the
// exact proxy and implementation at both the canonical head and pending state.
// A coordinator that is already fixed prevents the send.
func TestEvmV2BindingAdmissionReadsBothSelectors(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	link := *graph[4]
	chain, stub := newEvmV2ReadbackChain(t, graph[1], graph[3], graph[2])
	if err := chain.admitVault2LinkTarget(t.Context(), link, evmV2ReadbackBlock); err != nil {
		t.Fatalf("unbound registered vault2 refused: %v", err)
	}
	block, _ := json.Marshal(evmV2ReadbackBlock)
	if !stub.selectors[string(block)] || !stub.selectors[`"pending"`] || len(stub.selectors) != 2 {
		t.Fatalf("admission selectors: %v", stub.selectors)
	}
	coordinator := "0x" + hex.EncodeToString(stabi.NewSTSettlementVault2().PackCoordinator())
	collateral := graph[1].Getters[len(graph[1].Getters)-1]
	for _, fault := range []struct {
		name, key, expected string
		pending             bool
		value               string
	}{
		{"bound", evmV2ReadbackKey("eth_call", link.Address, coordinator), "vault2 escrow registration readback getter " + coordinator + " differs", false, common.BytesToHash(graph[3].Address[:]).Hex()},
		{"pending-bound", evmV2ReadbackKey("eth_call", link.Address, coordinator), "vault2 escrow registration readback getter " + coordinator + " differs", true, common.BytesToHash(graph[3].Address[:]).Hex()},
		{"pending-proxy", evmV2ReadbackKey("eth_getCode", graph[3].Address, ""), "initialized settlement2 proxy readback runtime differs", true, "0x"},
		{"pending-implementation", evmV2ReadbackKey("eth_getStorageAt", graph[2].Address, contractInitializableSlot), "settlement2 implementation readback storage slot " + contractInitializableSlot + " differs", true, (common.Hash{}).Hex()},
		{"pending-collateral", evmV2ReadbackKey("eth_call", common.HexToAddress(contractStakingPrecompile), collateral.Data), "vault2 escrow registration readback getter " + collateral.Data + " differs", true, "0x" + strings.Repeat("0", 63) + "1" + strings.Repeat("0", 256)},
	} {
		stub.fault = func(key string, selector any, _ string) any {
			if key == fault.key && (selector == "pending") == fault.pending {
				return fault.value
			}
			return nil
		}
		if err := chain.admitVault2LinkTarget(t.Context(), link, evmV2ReadbackBlock); err == nil || err.Error() != fault.expected {
			t.Fatalf("%s admission escaped: %v", fault.name, err)
		}
	}
}

// SV-8: defensive refusals. Readback needs the approved proxy and
// implementation; admission needs the registered vault2 projection.
func TestEvmV2ReadbackRequiresApprovedProjections(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	chain, _ := newEvmV2ReadbackChain(t)
	for _, plan := range []evmCreatePlan{*graph[1], func() evmCreatePlan { plan := *graph[4]; plan.Coordinator = nil; return plan }()} {
		if err := chain.readbackSettlement2(t.Context(), plan, evmV2ReadbackBlock); err == nil || err.Error() != "settlement2 readback lacks its approved proxy and implementation" {
			t.Fatalf("readback without its projections: %v", err)
		}
	}
	link := *graph[4]
	link.Escrow = nil
	if err := chain.admitVault2LinkTarget(t.Context(), link, evmV2ReadbackBlock); err == nil || err.Error() != "vault2 binding lacks its registered vault2 projection" {
		t.Fatalf("admission without registered vault2: %v", err)
	}
}
