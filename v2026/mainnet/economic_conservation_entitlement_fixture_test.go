// The public fixture retains actual transaction/receipt tries and canonical
// payout artifacts. The coordinator's committer is not the artifact signer.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/stabi"
)

type economicEntitlementFixture struct {
	source        *economicConservationFixture
	coordinator   *abi.ABI
	address       common.Address
	rootSigner    common.Address
	code          []byte
	artifact      *payoutartifact.Artifact
	committedRoot [32]byte
	committedHash [32]byte
	raw           []byte
	artifactReads atomic.Uint64
	missing       atomic.Bool
	wrongSigner   atomic.Bool
	wrongRoot     atomic.Bool
	unauthorized  atomic.Bool
	artifactHook  func(http.ResponseWriter, *http.Request) bool
}

func newEconomicEntitlementFixture(t *testing.T, usage ...uint64) *economicEntitlementFixture {
	t.Helper()
	return configureEconomicEntitlementFixture(t, newEconomicConservationFixture(t, false), usage...)
}

// This setup runs before any public owner starts. Rehash changed receipts and
// regenerate exact native mappings/admissions; never replace a checked result.
func configureEconomicEntitlementFixture(t *testing.T, source *economicConservationFixture, usage ...uint64) *economicEntitlementFixture {
	t.Helper()
	f := &economicEntitlementFixture{source: source, address: common.HexToAddress("0x0000000000000000000000000000000000004567"), rootSigner: common.HexToAddress("0x0000000000000000000000000000000000006789"), code: []byte{0x60, 0x01, 0x60, 0x00, 0xf3}}
	var err error
	f.coordinator, err = stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	vault := source.vault
	first := uint64(6)
	if len(usage) != 0 {
		first = usage[0]
	}
	if first == 0 || first >= 100 {
		t.Fatal("invalid synthetic share fixture")
	}
	providers := []payoutartifact.ProviderInput{{ClientID: [16]byte{1}, Coldkey: [32]byte(common.HexToHash(vault.policy.Coldkeys[0])), UsageBytes: first, Assignments: 1, Confirmations: 1, Eligible: true}}
	count := uint64(2)
	if len(usage) > 1 {
		count = usage[1]
	}
	if count < 2 || count > 32 || count-1 > 100-first {
		t.Fatal("invalid synthetic populated census")
	}
	remaining := 100 - first
	for index := uint64(1); index < count; index++ {
		amount := remaining / (count - index)
		remaining -= amount
		coldkey := common.HexToHash("0x" + strings.Repeat("75", 31) + fmt.Sprintf("%02x", index))
		providers = append(providers, payoutartifact.ProviderInput{ClientID: [16]byte{byte(index + 1)}, Coldkey: [32]byte(coldkey), UsageBytes: amount, Assignments: 1, Confirmations: 1, Eligible: true})
	}
	f.artifact, err = payoutartifact.Build(payoutartifact.BuildInput{
		DeploymentID: "synthetic-original-entitlement", GenesisHash: vault.policy.Network.GenesisHash, ChainID: vault.policy.Network.EvmChainId, Netuid: vault.policy.Netuid,
		Coordinator: f.address, SettlementVault: common.HexToAddress(vault.policy.Address), Epoch: 3, NoID: 1,
		PolicyHash: "0x" + strings.Repeat("52", 32), Start: payoutartifact.Boundary{Number: 0, Hash: vault.blocks[0].header.Hash().Hex()}, End: payoutartifact.Boundary{Number: 10, Hash: vault.blocks[10].header.Hash().Hex()},
		OperatorSnapshotHash: "sha256:" + strings.Repeat("53", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("54", 32), ReliabilityAMin: 1, CreatedAt: source.now,
		Providers: providers,
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.Repeat("26", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(f.artifact, key); err != nil {
		t.Fatal(err)
	}
	f.raw, err = payoutartifact.Bytes(f.artifact)
	if err != nil {
		t.Fatal(err)
	}
	if f.artifact.Signer == f.rootSigner {
		t.Fatal("fixture must exercise distinct artifact and commitment authorities")
	}
	artifactHash := [32]byte(common.HexToHash(strings.TrimPrefix(f.artifact.ContentHash, "sha256:")))
	f.committedRoot, f.committedHash = f.artifact.PayoutRoot, artifactHash
	economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
		switch number {
		case 11:
			receipt.Logs = append(receipt.Logs, monitorEvmTestLog(t, f.coordinator, f.address, "OperatorRootCommitted", big.NewInt(3), big.NewInt(1), f.artifact.PayoutRoot, artifactHash, f.rootSigner))
		case 12:
			receipt.Logs[0] = monitorEvmTestLog(t, vault.contract, common.HexToAddress(vault.policy.Address), "EntitlementFinalized", big.NewInt(3), big.NewInt(1), f.artifact.PayoutRoot, artifactHash, big.NewInt(50), uint64(900))
			receipt.Logs = append(receipt.Logs, monitorEvmTestLog(t, f.coordinator, f.address, "OperatorEpochFinalized", big.NewInt(3), big.NewInt(1), true))
		}
	})
	economicConservationTestNativeMapping(t, source.native, vault)
	nativeExecutionTestConfigure(t, source.native, nil)
	source.policy.Native.Observation = source.native.policy
	source.policy.Vault = vault.policy
	source.policy.Vault.BatchBlocks = 1
	artifactServer := httptest.NewServer(http.HandlerFunc(f.serveArtifact))
	t.Cleanup(artifactServer.Close)
	server := httptest.NewServer(http.HandlerFunc(f.serveRpc))
	t.Cleanup(server.Close)
	vault.url = server.URL
	source.policy.EntitlementSources = &economicConservationEntitlementPolicy{Sources: []economicConservationEntitlementSource{{PoolId: "1", Endpoint: artifactServer.URL, DeploymentId: f.artifact.DeploymentID, Coordinator: f.address.Hex(), CoordinatorCodeHash: crypto.Keccak256Hash(f.code).Hex()}}}
	source.writePolicy(t)
	return f
}

func (self *economicEntitlementFixture) serveArtifact(w http.ResponseWriter, request *http.Request) {
	self.artifactReads.Add(1)
	if self.artifactHook != nil && self.artifactHook(w, request) {
		return
	}
	if self.unauthorized.Load() {
		http.Error(w, "original object authorization", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var output []byte
	switch request.URL.Path {
	case "/sn/artifacts":
		if request.Method != http.MethodGet || request.URL.Query().Get("epoch") != "3" || request.URL.Query().Get("no_id") != "1" || request.URL.Query().Get("netuid") != "25" || request.URL.Query().Get("deployment_id") != self.artifact.DeploymentID {
			http.Error(w, "original artifact scope", 400)
			return
		}
		objects := []map[string]any{}
		if !self.missing.Load() {
			objects = append(objects, map[string]any{"key": "blob/operator/st/v1/history/" + self.artifact.DeploymentID + "/25/3/1/" + strings.TrimPrefix(self.artifact.ContentHash, "sha256:") + ".json", "size": len(self.raw), "content_hash": self.artifact.ContentHash})
		}
		var err error
		output, err = json.Marshal(map[string]any{"schema": "urnetwork-payout-artifact-history-v1", "objects": objects, "more": false, "next_after": ""})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	case "/sn/artifact":
		if request.URL.Query().Get("hash") != self.artifact.ContentHash {
			http.Error(w, "original artifact hash", 400)
			return
		}
		output = self.raw
	default:
		http.NotFound(w, request)
		return
	}
	if _, err := w.Write(output); err != nil {
		return
	}
}

// Only coordinator calls are intercepted. Complete blocks, raw headers,
// transactions and receipts go through the same real original vault fixture.
func (self *economicEntitlementFixture) serveRpc(w http.ResponseWriter, request *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var call struct {
		Id     any    `json:"id"`
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	address := ""
	if len(call.Params) == 2 {
		if call.Method == "eth_getCode" {
			address, _ = call.Params[0].(string)
		} else if call.Method == "eth_call" {
			if input, ok := call.Params[0].(map[string]any); ok {
				address, _ = input["to"].(string)
			}
		}
	}
	if address != self.address.Hex() {
		request.Body = io.NopCloser(bytes.NewReader(raw))
		self.source.vault.serve(w, request)
		return
	}
	selector, ok := call.Params[1].(map[string]any)
	if !ok || selector["requireCanonical"] != true {
		http.Error(w, "unpinned coordinator read", 400)
		return
	}
	hash, ok := selector["blockHash"].(string)
	if !ok || self.source.vault.byHash[hash] == nil {
		http.Error(w, "unknown original coordinator block", 400)
		return
	}
	result := hexutil.Encode(self.code)
	if call.Method == "eth_call" {
		input := call.Params[0].(map[string]any)
		dataText, ok := input["data"].(string)
		if !ok {
			http.Error(w, "missing call bytes", 400)
			return
		}
		data, err := hexutil.Decode(dataText)
		if err != nil || len(data) < 4 {
			http.Error(w, "invalid call bytes", 400)
			return
		}
		method, err := self.coordinator.MethodById(data[:4])
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if _, err := method.Inputs.Unpack(data[4:]); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var values []any
		switch method.Name {
		case "rootCommitments":
			root := self.committedRoot
			if self.wrongRoot.Load() {
				root[0] ^= 1
			}
			values = []any{root, self.committedHash, self.rootSigner, uint64(11)}
		case "operatorAt":
			signer := self.rootSigner
			if self.wrongSigner.Load() {
				signer = self.artifact.Signer
			}
			values = []any{stabi.STCoordinatorOperatorVersion{Coldkey: [32]byte(common.HexToHash("0x" + strings.Repeat("35", 32))), RootSigner: signer, EffectiveEpoch: 1, Active: true}}
		case "policyAt":
			// ABI uint256 values are concrete zeroes, never absent pointers.
			values = []any{stabi.STCoordinatorPolicySnapshot{PolicyHash: [32]byte(common.HexToHash(self.artifact.PolicyHash)), RootCommitWindowBlocks: 10, EpochDepositCapRao: big.NewInt(0), CampaignDepositCapRao: big.NewInt(0)}}
		case "settlementVault":
			values = []any{common.HexToAddress(self.source.policy.Vault.Address)}
		case "epochStartBlock":
			values = []any{big.NewInt(0)}
		case "epochEndBlock":
			values = []any{big.NewInt(10)}
		default:
			http.Error(w, fmt.Sprint("unselected original getter ", method.Name), 400)
			return
		}
		encoded, err := method.Outputs.Pack(values...)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		result = hexutil.Encode(encoded)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}); err != nil {
		return
	}
}
