package main

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpccodec "github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

type finalSemanticPinnedEVMRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

// finalSemanticPinnedEVMFixture is a narrow eth_call/getCode/getStorageAt
// archive surface. It rejects any selector other than the canonical block
// hash, allowing the state test to prove that each returned exchange was read
// at one immutable head.
type finalSemanticPinnedEVMFixture struct {
	t           *testing.T
	head        ChainHead
	proxy       string
	slot        string
	code        map[string]string
	callResults map[string]string

	mu    sync.Mutex
	calls []finalSemanticPinnedEVMRequest
}

func finalSemanticEVMCallKey(address string, data []byte) string {
	return strings.ToLower(address) + "|" + strings.ToLower(hexutil.Encode(data))
}

func (f *finalSemanticPinnedEVMFixture) setCallResult(t testing.TB, contractABI *abi.ABI, method, address string, data []byte, values ...any) {
	t.Helper()
	encoded, err := contractABI.Methods[method].Outputs.Pack(values...)
	if err != nil {
		t.Fatalf("pack %s output: %v", method, err)
	}
	f.mu.Lock()
	f.callResults[finalSemanticEVMCallKey(address, data)] = hexutil.Encode(encoded)
	f.mu.Unlock()
}

func (f *finalSemanticPinnedEVMFixture) setRawCallResult(address string, data []byte, result string) {
	f.mu.Lock()
	f.callResults[finalSemanticEVMCallKey(address, data)] = result
	f.mu.Unlock()
}

func (f *finalSemanticPinnedEVMFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	f.t.Helper()
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		f.t.Errorf("read pinned EVM request: %v", err)
		return
	}
	var call finalSemanticPinnedEVMRequest
	if err := json.Unmarshal(body, &call); err != nil {
		f.t.Errorf("decode pinned EVM request: %v", err)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()

	result := ""
	var selector finalEVMBlockSelector
	switch call.Method {
	case "eth_getCode":
		if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil {
			f.t.Errorf("invalid eth_getCode params: %s", call.Params)
			break
		}
		var address string
		if json.Unmarshal(call.Params[0], &address) != nil {
			f.t.Errorf("invalid eth_getCode address: %s", call.Params[0])
			break
		}
		result = f.code[strings.ToLower(address)]
	case "eth_getStorageAt":
		if len(call.Params) != 3 || json.Unmarshal(call.Params[2], &selector) != nil {
			f.t.Errorf("invalid eth_getStorageAt params: %s", call.Params)
			break
		}
		var address, slot string
		if json.Unmarshal(call.Params[0], &address) != nil || json.Unmarshal(call.Params[1], &slot) != nil || !strings.EqualFold(address, f.proxy) || !strings.EqualFold(slot, f.slot) {
			f.t.Errorf("unexpected implementation slot query: %s", call.Params)
			break
		}
		result = hexutil.Encode(common.LeftPadBytes(common.HexToAddress(f.proxy).Bytes(), common.HashLength))
		// The fixture factory replaces this proxy placeholder with the active
		// implementation before the first request.
		f.mu.Lock()
		if configured := f.callResults["implementation-slot"]; configured != "" {
			result = configured
		}
		f.mu.Unlock()
	case "eth_call":
		if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil {
			f.t.Errorf("invalid eth_call params: %s", call.Params)
			break
		}
		var message struct {
			To   string `json:"to"`
			Data string `json:"data"`
		}
		if json.Unmarshal(call.Params[0], &message) != nil {
			f.t.Errorf("invalid eth_call message: %s", call.Params[0])
			break
		}
		f.mu.Lock()
		result = f.callResults[strings.ToLower(message.To)+"|"+strings.ToLower(message.Data)]
		f.mu.Unlock()
	default:
		f.t.Errorf("unexpected pinned EVM method %q", call.Method)
	}
	if selector.BlockHash != f.head.Hash || !selector.RequireCanonical {
		f.t.Errorf("%s selector=%+v, want canonical %s", call.Method, selector, f.head.Hash)
	}
	response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
	if result == "" {
		response["error"] = map[string]any{"code": -32602, "message": "unknown fixture request"}
	} else {
		response["result"] = result
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		f.t.Errorf("encode pinned EVM response: %v", err)
	}
}

type finalSemanticContractFixture struct {
	fixture     *finalSemanticPinnedEVMFixture
	reader      *PublicFinalSemanticChainReader
	server      *httptest.Server
	coordinator *stabi.STCoordinator
	vault       *stabi.STSettlementVault
	reserve     *stabi.STReserveSink
	coordABI    *abi.ABI
	vaultABI    *abi.ABI
	reserveABI  *abi.ABI
	want        FinalContractDeploymentState
}

func newFinalSemanticContractFixture(t *testing.T) *finalSemanticContractFixture {
	t.Helper()
	addresses := struct {
		proxy, implementation, vault, reserve common.Address
		bootstrap, drill, probe, batcher      common.Address
		owner, guardian, activeGuardian       common.Address
		oracle, activeOracle                  common.Address
	}{
		proxy:          common.HexToAddress("0x1000000000000000000000000000000000000001"),
		implementation: common.HexToAddress("0x2000000000000000000000000000000000000002"),
		vault:          common.HexToAddress("0x3000000000000000000000000000000000000003"),
		reserve:        common.HexToAddress("0x4000000000000000000000000000000000000004"),
		bootstrap:      common.HexToAddress("0xa00000000000000000000000000000000000000a"),
		drill:          common.HexToAddress("0xb00000000000000000000000000000000000000b"),
		probe:          common.HexToAddress("0xc00000000000000000000000000000000000000c"),
		batcher:        common.HexToAddress("0xd00000000000000000000000000000000000000d"),
		owner:          common.HexToAddress("0x5000000000000000000000000000000000000005"),
		guardian:       common.HexToAddress("0x6000000000000000000000000000000000000006"),
		activeGuardian: common.HexToAddress("0x7000000000000000000000000000000000000007"),
		oracle:         common.HexToAddress("0x8000000000000000000000000000000000000008"),
		activeOracle:   common.HexToAddress("0x9000000000000000000000000000000000000009"),
	}
	head := ChainHead{Number: 123, Hash: "0x" + strings.Repeat("ab", 32)}
	slot := "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"
	codeBytes := map[string][]byte{
		strings.ToLower(addresses.proxy.Hex()):          {0x60, 0x01},
		strings.ToLower(addresses.implementation.Hex()): {0x60, 0x02},
		strings.ToLower(addresses.vault.Hex()):          {0x60, 0x03},
		strings.ToLower(addresses.reserve.Hex()):        {0x60, 0x04},
		strings.ToLower(addresses.bootstrap.Hex()):      {0x60, 0x05},
		strings.ToLower(addresses.drill.Hex()):          {0x60, 0x06},
		strings.ToLower(addresses.probe.Hex()):          {0x60, 0x07},
		strings.ToLower(addresses.batcher.Hex()):        {0x60, 0x08},
	}
	fixture := &finalSemanticPinnedEVMFixture{
		t: t, head: head, proxy: addresses.proxy.Hex(), slot: slot,
		code: map[string]string{}, callResults: map[string]string{},
	}
	for address, code := range codeBytes {
		fixture.code[address] = hexutil.Encode(code)
	}
	fixture.callResults["implementation-slot"] = hexutil.Encode(common.LeftPadBytes(addresses.implementation.Bytes(), common.HashLength))

	coordinator := stabi.NewSTCoordinator()
	vault := stabi.NewSTSettlementVault()
	reserve := stabi.NewSTReserveSink()
	coordABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	vaultABI, err := stabi.STSettlementVaultMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	reserveABI, err := stabi.STReserveSinkMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	netuid := uint16(41)
	coordinatorSelf := ss58.EvmMirrorPubkey(addresses.proxy)
	vaultSelf := ss58.EvmMirrorPubkey(addresses.vault)
	reserveSelf := ss58.EvmMirrorPubkey(addresses.reserve)
	escrowHotkey := [32]byte{0xea, 0x01}
	reserveHotkey := [32]byte{0xeb, 0x02}
	policyHash := [32]byte{0xcc, 0x03}
	policy := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: policyHash, EffectiveEpoch: 10, EffectiveBlock: 100, EpochBlocks: 20,
		EpochDepositCapRao: big.NewInt(1), CampaignDepositCapRao: big.NewInt(2),
	}

	fixture.setCallResult(t, coordABI, "owner", addresses.proxy.Hex(), coordinator.PackOwner(), addresses.owner)
	fixture.setCallResult(t, coordABI, "netuid", addresses.proxy.Hex(), coordinator.PackNetuid(), netuid)
	fixture.setCallResult(t, coordABI, "selfColdkey", addresses.proxy.Hex(), coordinator.PackSelfColdkey(), coordinatorSelf)
	fixture.setCallResult(t, coordABI, "settlementVault", addresses.proxy.Hex(), coordinator.PackSettlementVault(), addresses.vault)
	fixture.setCallResult(t, coordABI, "reserveSink", addresses.proxy.Hex(), coordinator.PackReserveSink(), addresses.reserve)
	fixture.setCallResult(t, coordABI, "guardian", addresses.proxy.Hex(), coordinator.PackGuardian(), addresses.guardian)
	fixture.setCallResult(t, coordABI, "activeGuardian", addresses.proxy.Hex(), coordinator.PackActiveGuardian(), addresses.activeGuardian)
	fixture.setCallResult(t, coordABI, "paused", addresses.proxy.Hex(), coordinator.PackPaused(), false)
	fixture.setCallResult(t, coordABI, "commitmentOracle", addresses.proxy.Hex(), coordinator.PackCommitmentOracle(), addresses.oracle)
	fixture.setCallResult(t, coordABI, "activeCommitmentOracle", addresses.proxy.Hex(), coordinator.PackActiveCommitmentOracle(), addresses.activeOracle)
	fixture.setCallResult(t, vaultABI, "coordinator", addresses.vault.Hex(), vault.PackCoordinator(), addresses.proxy)
	fixture.setCallResult(t, vaultABI, "netuid", addresses.vault.Hex(), vault.PackNetuid(), netuid)
	fixture.setCallResult(t, vaultABI, "selfColdkey", addresses.vault.Hex(), vault.PackSelfColdkey(), vaultSelf)
	fixture.setCallResult(t, vaultABI, "escrowHotkey", addresses.vault.Hex(), vault.PackEscrowHotkey(), escrowHotkey)
	fixture.setCallResult(t, vaultABI, "escrowRegistered", addresses.vault.Hex(), vault.PackEscrowRegistered(), true)
	fixture.setCallResult(t, vaultABI, "minimumClaimTTLBlocks", addresses.vault.Hex(), vault.PackMinimumClaimTTLBlocks(), uint64(1200))
	fixture.setCallResult(t, vaultABI, "minimumTransferTaoRao", addresses.vault.Hex(), vault.PackMinimumTransferTaoRao(), uint64(100_000))
	fixture.setCallResult(t, vaultABI, "totalCaptured", addresses.vault.Hex(), vault.PackTotalCaptured(), big.NewInt(1_000))
	fixture.setCallResult(t, vaultABI, "totalPaid", addresses.vault.Hex(), vault.PackTotalPaid(), big.NewInt(600))
	fixture.setCallResult(t, vaultABI, "escrowAccounted", addresses.vault.Hex(), vault.PackEscrowAccounted(), big.NewInt(400))
	fixture.setCallResult(t, vaultABI, "pendingFunding", addresses.vault.Hex(), vault.PackPendingFunding(), big.NewInt(150))
	fixture.setCallResult(t, vaultABI, "outstandingLiability", addresses.vault.Hex(), vault.PackOutstandingLiability(), big.NewInt(250))
	fixture.setCallResult(t, vaultABI, "liveEscrowStake", addresses.vault.Hex(), vault.PackLiveEscrowStake(), big.NewInt(425))
	fixture.setCallResult(t, reserveABI, "recorder", addresses.reserve.Hex(), reserve.PackRecorder(), addresses.proxy)
	fixture.setCallResult(t, reserveABI, "netuid", addresses.reserve.Hex(), reserve.PackNetuid(), netuid)
	fixture.setCallResult(t, reserveABI, "selfColdkey", addresses.reserve.Hex(), reserve.PackSelfColdkey(), reserveSelf)
	fixture.setCallResult(t, reserveABI, "reserveHotkey", addresses.reserve.Hex(), reserve.PackReserveHotkey(), reserveHotkey)
	fixture.setCallResult(t, coordABI, "policyCount", addresses.proxy.Hex(), coordinator.PackPolicyCount(), big.NewInt(2))
	fixture.setCallResult(t, coordABI, "policyAt", addresses.proxy.Hex(), coordinator.PackPolicyAt(big.NewInt(10)), policy)

	runtimeHash := func(address common.Address) string {
		return strings.ToLower(crypto.Keccak256Hash(codeBytes[strings.ToLower(address.Hex())]).Hex())
	}
	runtimeRoots := []FinalReleaseRuntimeRoot{
		{Name: "coordinator_bootstrap_implementation", Address: strings.ToLower(addresses.bootstrap.Hex()), RuntimeCodeHash: runtimeHash(addresses.bootstrap), ReleaseRuntimeHash: finalTestHex(0x61)},
		{Name: "coordinator_proxy", Address: strings.ToLower(addresses.proxy.Hex()), RuntimeCodeHash: runtimeHash(addresses.proxy), ReleaseRuntimeHash: finalTestHex(0x62)},
		{Name: "coordinator_upgrade_implementation", Address: strings.ToLower(addresses.implementation.Hex()), RuntimeCodeHash: runtimeHash(addresses.implementation), ReleaseRuntimeHash: finalTestHex(0x63)},
		{Name: "fleet_batcher", Address: strings.ToLower(addresses.batcher.Hex()), RuntimeCodeHash: runtimeHash(addresses.batcher), ReleaseRuntimeHash: finalTestHex(0x64)},
		{Name: "governance_drill_implementation", Address: strings.ToLower(addresses.drill.Hex()), RuntimeCodeHash: runtimeHash(addresses.drill), ReleaseRuntimeHash: finalTestHex(0x65)},
		{Name: "precompile_probe", Address: strings.ToLower(addresses.probe.Hex()), RuntimeCodeHash: runtimeHash(addresses.probe), ReleaseRuntimeHash: finalTestHex(0x66)},
		{Name: "reserve_sink", Address: strings.ToLower(addresses.reserve.Hex()), RuntimeCodeHash: runtimeHash(addresses.reserve), ReleaseRuntimeHash: finalTestHex(0x67)},
		{Name: "settlement_vault", Address: strings.ToLower(addresses.vault.Hex()), RuntimeCodeHash: runtimeHash(addresses.vault), ReleaseRuntimeHash: finalTestHex(0x68)},
	}
	evidence := &FinalSemanticEvidence{
		Netuid: netuid,
		Window: ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 1},
		Deployment: FinalContractDeploymentEvidence{
			CoordinatorProxy: addresses.proxy.Hex(), CoordinatorImplementation: addresses.implementation.Hex(),
			SettlementVault: addresses.vault.Hex(), ReserveSink: addresses.reserve.Hex(), ERC1967ImplementationSlot: slot,
			CoordinatorProxyCodeHash: runtimeHash(addresses.proxy), ImplementationCodeHash: runtimeHash(addresses.implementation),
			SettlementVaultCodeHash: runtimeHash(addresses.vault), ReserveSinkCodeHash: runtimeHash(addresses.reserve), RuntimeRoots: runtimeRoots,
		},
	}
	server := httptest.NewServer(fixture)
	client, err := rpc.DialHTTP(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	reader := &PublicFinalSemanticChainReader{evidence: evidence, evm: client, evmRetry: immediateFinalSemanticRetryPolicy()}
	want := FinalContractDeploymentState{
		CoordinatorProxy: addresses.proxy.Hex(), CoordinatorImplementation: addresses.implementation.Hex(), SettlementVault: addresses.vault.Hex(), ReserveSink: addresses.reserve.Hex(),
		GovernanceOwner: strings.ToLower(addresses.owner.Hex()), CoordinatorNetuid: netuid, CoordinatorSelfColdkey: "0x" + common.Bytes2Hex(coordinatorSelf[:]),
		CoordinatorSettlementVault: strings.ToLower(addresses.vault.Hex()), CoordinatorReserveSink: strings.ToLower(addresses.reserve.Hex()),
		CoordinatorGuardian: strings.ToLower(addresses.guardian.Hex()), CoordinatorActiveGuardian: strings.ToLower(addresses.activeGuardian.Hex()), CoordinatorPaused: false,
		CoordinatorCommitmentOracle: strings.ToLower(addresses.oracle.Hex()), CoordinatorActiveCommitmentOracle: strings.ToLower(addresses.activeOracle.Hex()),
		VaultCoordinator: strings.ToLower(addresses.proxy.Hex()), VaultNetuid: netuid, VaultSelfColdkey: "0x" + common.Bytes2Hex(vaultSelf[:]),
		VaultEscrowHotkey: "0x" + common.Bytes2Hex(escrowHotkey[:]), VaultEscrowRegistered: true, VaultMinimumClaimTTLBlocks: 1200, VaultMinimumTransferTaoRao: 100_000,
		ReserveRecorder: strings.ToLower(addresses.proxy.Hex()), ReserveNetuid: netuid, ReserveSelfColdkey: "0x" + common.Bytes2Hex(reserveSelf[:]), ReserveHotkey: "0x" + common.Bytes2Hex(reserveHotkey[:]),
		CoordinatorProxyCodeHash:   strings.ToLower(crypto.Keccak256Hash(codeBytes[strings.ToLower(addresses.proxy.Hex())]).Hex()),
		ImplementationCodeHash:     strings.ToLower(crypto.Keccak256Hash(codeBytes[strings.ToLower(addresses.implementation.Hex())]).Hex()),
		SettlementVaultCodeHash:    strings.ToLower(crypto.Keccak256Hash(codeBytes[strings.ToLower(addresses.vault.Hex())]).Hex()),
		ReserveSinkCodeHash:        strings.ToLower(crypto.Keccak256Hash(codeBytes[strings.ToLower(addresses.reserve.Hex())]).Hex()),
		ObservedImplementationSlot: fixture.callResults["implementation-slot"], PolicyHash: "0x" + common.Bytes2Hex(policyHash[:]),
		PolicyVersion: 2, PolicyEffectiveEpoch: 10, PolicyEffectiveBlock: 100, Block: head,
	}
	return &finalSemanticContractFixture{
		fixture: fixture, reader: reader, server: server, coordinator: coordinator, vault: vault, reserve: reserve,
		coordABI: coordABI, vaultABI: vaultABI, reserveABI: reserveABI, want: want,
	}
}

func (f *finalSemanticContractFixture) close() {
	f.reader.evm.Close()
	f.server.Close()
}

func TestPublicFinalSemanticContractDeploymentReturnsCompletePinnedCustodyState(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	got, exchanges, err := f.reader.ContractDeployment(context.Background(), f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	if got != f.want {
		t.Fatalf("contract state\n got: %+v\nwant: %+v", got, f.want)
	}
	if len(exchanges) != 28 {
		t.Fatalf("exchanges=%d, want 28", len(exchanges))
	}
	for index, exchange := range exchanges {
		if exchange.Chain != "evm" || exchange.PinnedHead != f.want.Block || len(exchange.Result) == 0 {
			t.Fatalf("exchange %d is incomplete or unpinned: %+v", index, exchange)
		}
	}
	f.fixture.mu.Lock()
	callCount := len(f.fixture.calls)
	f.fixture.mu.Unlock()
	if callCount != len(exchanges) {
		t.Fatalf("RPC calls=%d exchanges=%d", callCount, len(exchanges))
	}
}

func TestPublicFinalSemanticContractDeploymentRejectsAdjacentSubstitution(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*finalSemanticContractFixture)
		want   string
	}{
		{
			name: "coordinator vault points to reserve",
			mutate: func(f *finalSemanticContractFixture) {
				f.fixture.setCallResult(t, f.coordABI, "settlementVault", f.want.CoordinatorProxy, f.coordinator.PackSettlementVault(), common.HexToAddress(f.want.ReserveSink))
			},
			want: "custody address linkage",
		},
		{
			name: "vault points to adjacent owner",
			mutate: func(f *finalSemanticContractFixture) {
				f.fixture.setCallResult(t, f.vaultABI, "coordinator", f.want.SettlementVault, f.vault.PackCoordinator(), common.HexToAddress(f.want.GovernanceOwner))
			},
			want: "custody address linkage",
		},
		{
			name: "reserve recorder points to vault",
			mutate: func(f *finalSemanticContractFixture) {
				f.fixture.setCallResult(t, f.reserveABI, "recorder", f.want.ReserveSink, f.reserve.PackRecorder(), common.HexToAddress(f.want.SettlementVault))
			},
			want: "custody address linkage",
		},
		{
			name: "vault netuid substituted",
			mutate: func(f *finalSemanticContractFixture) {
				f.fixture.setCallResult(t, f.vaultABI, "netuid", f.want.SettlementVault, f.vault.PackNetuid(), f.want.VaultNetuid+1)
			},
			want: "do not match deployment netuid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFinalSemanticContractFixture(t)
			defer f.close()
			test.mutate(f)
			if _, _, err := f.reader.ContractDeployment(context.Background(), f.want.Block); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("substitution error=%v, want %q", err, test.want)
			}
		})
	}
}

func TestPublicFinalSemanticContractDeploymentRejectsMalformedGetter(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	f.fixture.setRawCallResult(f.want.SettlementVault, f.vault.PackMinimumTransferTaoRao(), "0x01")
	if _, _, err := f.reader.ContractDeployment(context.Background(), f.want.Block); err == nil || !strings.Contains(err.Error(), "decode vault minimum transfer") {
		t.Fatalf("malformed getter error=%v", err)
	}
}

func TestPublicFinalSemanticSettlementVaultStateIsCompleteAndPinned(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	want := FinalSettlementVaultChainState{
		TotalCapturedRao: "1000", TotalPaidRao: "600", EscrowAccountedRao: "400",
		PendingFundingRao: "150", OutstandingLiabilityRao: "250", LiveEscrowStakeRao: "425", Block: f.want.Block,
	}
	got, exchanges, err := f.reader.SettlementVaultState(context.Background(), f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("settlement-vault state\n got: %+v\nwant: %+v", got, want)
	}
	if len(exchanges) != 6 {
		t.Fatalf("settlement-vault exchanges=%d, want 6", len(exchanges))
	}
	for index, exchange := range exchanges {
		if exchange.Chain != "evm" || exchange.PinnedHead != f.want.Block || len(exchange.Result) == 0 {
			t.Fatalf("settlement-vault exchange %d is incomplete or unpinned: %+v", index, exchange)
		}
	}
}

func TestPublicFinalSemanticSettlementVaultStateRejectsMalformedGetter(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	f.fixture.setRawCallResult(f.want.SettlementVault, f.vault.PackOutstandingLiability(), "0x01")
	if _, _, err := f.reader.SettlementVaultState(context.Background(), f.want.Block); err == nil || !strings.Contains(err.Error(), "decode settlement-vault outstanding liability") {
		t.Fatalf("malformed settlement-vault getter error=%v", err)
	}
}

// Contract epoch zero remains a valid block-pinned deposit and authority query.
func TestPublicFinalSemanticEpochZeroCoordinatorReadersArePinned(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	operatorID := uint64(1)
	coldkey := [32]byte{0xa1, 0x01}
	poolHotkey := [32]byte{0xa2, 0x02}
	depositHotkey := [32]byte{0xa3, 0x03}
	operator := stabi.STCoordinatorOperatorVersion{
		Coldkey: coldkey, PoolHotkey: poolHotkey, DepositHotkey: depositHotkey,
		DepositSigner: common.HexToAddress("0x5000000000000000000000000000000000000005"), RootSigner: common.HexToAddress("0x6000000000000000000000000000000000000006"),
		EffectiveEpoch: 0, Active: true,
	}
	f.fixture.setCallResult(t, f.coordABI, "epochDeposits", f.want.CoordinatorProxy, f.coordinator.PackEpochDeposits(big.NewInt(0), new(big.Int).SetUint64(operatorID)), big.NewInt(17))
	f.fixture.setCallResult(t, f.coordABI, "epochConvictionAdded", f.want.CoordinatorProxy, f.coordinator.PackEpochConvictionAdded(big.NewInt(0), new(big.Int).SetUint64(operatorID)), big.NewInt(19))
	f.fixture.setCallResult(t, f.coordABI, "operatorVersionCount", f.want.CoordinatorProxy, f.coordinator.PackOperatorVersionCount(new(big.Int).SetUint64(operatorID)), big.NewInt(2))
	f.fixture.setCallResult(t, f.coordABI, "operatorAt", f.want.CoordinatorProxy, f.coordinator.PackOperatorAt(new(big.Int).SetUint64(operatorID), big.NewInt(0)), operator)

	deposit, depositExchanges, err := f.reader.EpochDeposit(context.Background(), 0, operatorID, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	if deposit != (FinalEpochDepositChainState{Epoch: 0, NoID: operatorID, AmountRao: "17", Block: f.want.Block}) || len(depositExchanges) != 1 || depositExchanges[0].PinnedHead != f.want.Block {
		t.Fatalf("epoch-zero deposit=%+v exchanges=%+v", deposit, depositExchanges)
	}
	added, addedExchanges, err := f.reader.EpochConvictionAdded(context.Background(), 0, operatorID, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	if added != (FinalEpochConvictionAddedChainState{Epoch: 0, NoID: operatorID, AmountRao: "19", Block: f.want.Block}) || len(addedExchanges) != 1 || addedExchanges[0].PinnedHead != f.want.Block {
		t.Fatalf("epoch-zero conviction increment=%+v exchanges=%+v", added, addedExchanges)
	}
	coldkeySS58, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	poolHotkeySS58, err := ss58.Encode(poolHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	depositHotkeySS58, err := ss58.Encode(depositHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	version, versionExchanges, err := f.reader.OperatorVersion(context.Background(), operatorID, 0, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	want := FinalOperatorVersionChainState{
		NoID: operatorID, VersionCount: 2, Coldkey: coldkeySS58, PoolHotkey: poolHotkeySS58, DepositHotkey: depositHotkeySS58,
		DepositSigner: strings.ToLower(operator.DepositSigner.Hex()), RootSigner: strings.ToLower(operator.RootSigner.Hex()), EffectiveEpoch: 0, Active: true, Block: f.want.Block,
	}
	if version != want || len(versionExchanges) != 2 || versionExchanges[0].PinnedHead != f.want.Block || versionExchanges[1].PinnedHead != f.want.Block {
		t.Fatalf("epoch-zero operator version=%+v exchanges=%+v want=%+v", version, versionExchanges, want)
	}
}

// Merkle entitlements, claim-once keys, and accumulated credit share one head.
func TestPublicFinalSemanticVaultClaimAndCreditArePinned(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	coldkey := [32]byte{0xb1, 0x01}
	coldkeyText := "0x" + common.Bytes2Hex(coldkey[:])
	payoutLeafText, err := finalSemanticVaultPayoutLeaf(coldkeyText, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	claimKeyText, err := finalSemanticVaultClaimKey(1, coldkeyText)
	if err != nil {
		t.Fatal(err)
	}
	payoutLeafHash := common.HexToHash(payoutLeafText)
	claimKeyHash := common.HexToHash(claimKeyText)
	var payoutLeaf, claimKey [32]byte
	copy(payoutLeaf[:], payoutLeafHash[:])
	copy(claimKey[:], claimKeyHash[:])
	f.fixture.setCallResult(t, f.vaultABI, "payoutLeaf", f.want.SettlementVault, f.vault.PackPayoutLeaf(coldkey, big.NewInt(10_000)), payoutLeaf)
	f.fixture.setCallResult(t, f.vaultABI, "leafClaimed", f.want.SettlementVault, f.vault.PackLeafClaimed(big.NewInt(0), claimKey), true)
	f.fixture.setCallResult(t, f.vaultABI, "claimCredit", f.want.SettlementVault, f.vault.PackClaimCredit(coldkey), big.NewInt(23))
	claim, claimExchanges, err := f.reader.VaultClaim(context.Background(), 0, 1, coldkeyText, 10_000, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	wantClaim := FinalVaultClaimChainState{Epoch: 0, NoID: 1, Coldkey: strings.ToLower(coldkeyText), ShareBPS: 10_000, PayoutLeaf: strings.ToLower(payoutLeafText), ClaimKey: strings.ToLower(claimKeyText), LeafClaimed: true, Block: f.want.Block}
	if claim != wantClaim || len(claimExchanges) != 2 || claimExchanges[0].PinnedHead != f.want.Block || claimExchanges[1].PinnedHead != f.want.Block {
		t.Fatalf("vault claim=%+v exchanges=%+v want=%+v", claim, claimExchanges, wantClaim)
	}
	credit, creditExchanges, err := f.reader.VaultClaimCredit(context.Background(), coldkeyText, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	wantCredit := FinalVaultClaimCreditChainState{Coldkey: strings.ToLower(coldkeyText), CreditRao: "23", Block: f.want.Block}
	if credit != wantCredit || len(creditExchanges) != 1 || creditExchanges[0].PinnedHead != f.want.Block {
		t.Fatalf("vault claim credit=%+v exchanges=%+v want=%+v", credit, creditExchanges, wantCredit)
	}
}

// Conviction, reserve principal, carry, and proxy runtime cannot mix snapshots.
func TestPublicFinalSemanticPerOperatorEVMAuditsArePinned(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	operatorID := uint64(1)
	f.fixture.setCallResult(t, f.coordABI, "cumulativeConviction", f.want.CoordinatorProxy, f.coordinator.PackCumulativeConviction(new(big.Int).SetUint64(operatorID)), big.NewInt(31))
	f.fixture.setCallResult(t, f.reserveABI, "operatorPrincipal", f.want.ReserveSink, f.reserve.PackOperatorPrincipal(new(big.Int).SetUint64(operatorID)), big.NewInt(31))
	f.fixture.setCallResult(t, f.vaultABI, "carry", f.want.SettlementVault, f.vault.PackCarry(new(big.Int).SetUint64(operatorID)), big.NewInt(7))
	conviction, convictionExchanges, err := f.reader.CoordinatorConviction(context.Background(), operatorID, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	if conviction != (FinalCoordinatorConvictionChainState{NoID: operatorID, ConvictionRao: "31", Block: f.want.Block}) || len(convictionExchanges) != 1 || convictionExchanges[0].PinnedHead != f.want.Block {
		t.Fatalf("coordinator conviction=%+v exchanges=%+v", conviction, convictionExchanges)
	}
	principal, principalExchanges, err := f.reader.ReserveOperatorPrincipal(context.Background(), operatorID, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	if principal != (FinalReserveOperatorPrincipalChainState{NoID: operatorID, PrincipalRao: "31", Block: f.want.Block}) || len(principalExchanges) != 1 || principalExchanges[0].PinnedHead != f.want.Block {
		t.Fatalf("reserve principal=%+v exchanges=%+v", principal, principalExchanges)
	}
	carry, carryExchanges, err := f.reader.VaultCarry(context.Background(), operatorID, f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	if carry != (FinalVaultCarryChainState{NoID: operatorID, CarryRao: "7", Block: f.want.Block}) || len(carryExchanges) != 1 || carryExchanges[0].PinnedHead != f.want.Block {
		t.Fatalf("vault carry=%+v exchanges=%+v", carry, carryExchanges)
	}
	runtime, runtimeExchanges, err := f.reader.CoordinatorRuntime(context.Background(), f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	wantRuntime := FinalCoordinatorRuntimeChainState{
		CoordinatorProxy: f.want.CoordinatorProxy, CoordinatorImplementation: strings.ToLower(f.want.CoordinatorImplementation),
		ObservedImplementationSlot: strings.ToLower(f.want.ObservedImplementationSlot), ProxyCodeHash: f.want.CoordinatorProxyCodeHash,
		ImplementationCodeHash: f.want.ImplementationCodeHash, RuntimeRoots: append([]FinalReleaseRuntimeRoot(nil), f.reader.evidence.Deployment.RuntimeRoots...), Block: f.want.Block,
	}
	if !finalJSONEqual(runtime, wantRuntime) || len(runtimeExchanges) != 1+len(f.reader.evidence.Deployment.RuntimeRoots) || runtimeExchanges[0].PinnedHead != f.want.Block || runtimeExchanges[1].PinnedHead != f.want.Block || runtimeExchanges[len(runtimeExchanges)-1].PinnedHead != f.want.Block {
		t.Fatalf("coordinator runtime=%+v exchanges=%+v want=%+v", runtime, runtimeExchanges, wantRuntime)
	}
}

// A malformed historical authority cardinality fails closed before selection.
func TestPublicFinalSemanticOperatorVersionRejectsMalformedHistoricalState(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	operatorID := uint64(1)
	f.fixture.setRawCallResult(f.want.CoordinatorProxy, f.coordinator.PackOperatorVersionCount(new(big.Int).SetUint64(operatorID)), "0x01")
	if _, _, err := f.reader.OperatorVersion(context.Background(), operatorID, 0, f.want.Block); err == nil || !strings.Contains(err.Error(), "decode operator version count") {
		t.Fatalf("malformed operator count error=%v", err)
	}
}

func TestFinalNativeRewardRejectsHotkeySubstitution(t *testing.T) {
	expectedBytes := [32]byte{0x41, 0x42}
	expected := "0x" + common.Bytes2Hex(expectedBytes[:])
	if err := validateFinalNativeRewardHotkey(7, expected, expectedBytes[:]); err != nil {
		t.Fatalf("exact reward hotkey rejected: %v", err)
	}
	substitute := expectedBytes
	substitute[31]++
	if err := validateFinalNativeRewardHotkey(7, expected, substitute[:]); err == nil || !strings.Contains(err.Error(), "UID 7 hotkey") {
		t.Fatalf("substituted reward hotkey error=%v", err)
	}
}

func TestFinalNativeRewardRequiresExplicitWellFormedStake(t *testing.T) {
	encoded, err := gsrpccodec.Encode(gsrpctypes.U64(123456))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(gsrpccodec.HexEncodeToString(encoded))
	if err != nil {
		t.Fatal(err)
	}
	var stake gsrpctypes.U64
	if err := decodeFinalRequiredSubstrateStorage("SubtensorModule", "TotalHotkeyAlpha", raw, &stake); err != nil || uint64(stake) != 123456 {
		t.Fatalf("explicit stake=%d error=%v", stake, err)
	}
	for _, test := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"missing", json.RawMessage(`null`)},
		{"empty", json.RawMessage(`""`)},
		{"empty hex", json.RawMessage(`"0x"`)},
		{"malformed hex", json.RawMessage(`"0xzz"`)},
		{"malformed SCALE", json.RawMessage(`"0x01"`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var value gsrpctypes.U64
			if err := decodeFinalRequiredSubstrateStorage("SubtensorModule", "TotalHotkeyAlpha", test.raw, &value); err == nil {
				t.Fatalf("malformed stake %s was accepted as %d", test.raw, value)
			}
		})
	}
}

func TestPublicFinalSemanticNativeOwnerStakeUsesPinnedStakingPrecompile(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		t.Fatal(err)
	}
	hotkey := [32]byte{0xa1, 0x01}
	coldkey := [32]byte{0xb2, 0x02}
	data, err := parsed.Pack("getStake", hotkey, coldkey, new(big.Int).SetUint64(uint64(f.reader.evidence.Netuid)))
	if err != nil {
		t.Fatal(err)
	}
	f.fixture.setCallResult(t, &parsed, "getStake", stakingPrecompileAddress.Hex(), data, big.NewInt(123_456))
	got, exchanges, err := f.reader.NativeOwnerStake(context.Background(), "0x"+common.Bytes2Hex(hotkey[:]), "0x"+common.Bytes2Hex(coldkey[:]), f.want.Block)
	if err != nil {
		t.Fatal(err)
	}
	want := FinalNativeOwnerStakeState{HotkeyPublicKey: "0x" + common.Bytes2Hex(hotkey[:]), ColdkeyPublicKey: "0x" + common.Bytes2Hex(coldkey[:]), StakeRao: "123456", Block: f.want.Block}
	if got != want || len(exchanges) != 1 || exchanges[0].PinnedHead != f.want.Block {
		t.Fatalf("owner stake got=%+v exchanges=%+v", got, exchanges)
	}
}

func TestPublicFinalSemanticNativeOwnerStakeRejectsAdjacentAndMalformedInputs(t *testing.T) {
	f := newFinalSemanticContractFixture(t)
	defer f.close()
	hotkey := "0x" + strings.Repeat("a1", 32)
	coldkey := "0x" + strings.Repeat("b2", 32)
	if _, _, err := f.reader.NativeOwnerStake(context.Background(), hotkey[:len(hotkey)-2], coldkey, f.want.Block); err == nil {
		t.Fatal("short owner hotkey was accepted")
	}
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		t.Fatal(err)
	}
	hotkeyBytes, _ := decodeHex32("hotkey", hotkey)
	coldkeyBytes, _ := decodeHex32("coldkey", coldkey)
	data, err := parsed.Pack("getStake", hotkeyBytes, coldkeyBytes, new(big.Int).SetUint64(uint64(f.reader.evidence.Netuid)))
	if err != nil {
		t.Fatal(err)
	}
	f.fixture.setRawCallResult(stakingPrecompileAddress.Hex(), data, "0x01")
	if _, _, err := f.reader.NativeOwnerStake(context.Background(), hotkey, coldkey, f.want.Block); err == nil || !strings.Contains(err.Error(), "decode native owner getStake") {
		t.Fatalf("malformed getStake error=%v", err)
	}
}
