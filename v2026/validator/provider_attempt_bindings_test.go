// Hash-pinned public calls retain original complete tuples at both boundaries;
// deterministic malformed/foreign rows cannot silently become eligible zeroes.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Generated ABI encoding and the real JSON-RPC reader are used by every case.
func providerAttemptBindingFixture(t *testing.T) (*ChainClient, ProviderAttemptBindingExpectation, *ProviderAttemptBindingOriginal) {
	t.Helper()
	code := []byte{0x60, 0x00, 0x50, 0x00}
	expected := ProviderAttemptBindingExpectation{Domain: protocol.ProviderAttemptDomain{ChainId: 945, GenesisHash: [32]byte{1}, Netuid: 17, Coordinator: [20]byte{2}, SettlementVault: [20]byte{3}, DeploymentIdHash: [32]byte{4}, PolicyHash: [32]byte{5}}, Epoch: 7, StartBlock: 123, StartHash: chainBatchTestBlockHash, EndBlock: 124, EndHash: chainBatchAdjacentBlockHash, CoordinatorRuntimeHash: [32]byte(crypto.Keccak256Hash(code)), ClientIds: [][16]byte{{15: 1}, {15: 2}}, MaxProviders: 8}
	contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	active := stabi.STCoordinatorBindingRecord{FleetId: [32]byte{6}, Hotkey: [32]byte{7}, ClientKey: [32]byte{8}, CommitmentHash: [32]byte{9}, Generation: 2, ValidFromEpoch: 7, ValidToEpoch: 9, Uid: 0}
	activeRaw, err := contractAbi.Methods["bindingAt"].Outputs.Pack(true, active)
	if err != nil {
		t.Fatal(err)
	}
	inactiveRaw, err := contractAbi.Methods["bindingAt"].Outputs.Pack(false, stabi.STCoordinatorBindingRecord{})
	if err != nil {
		t.Fatal(err)
	}
	original := &ProviderAttemptBindingOriginal{Domain: expected.Domain, Epoch: expected.Epoch, StartBlock: expected.StartBlock, StartHash: expected.StartHash, EndBlock: expected.EndBlock, EndHash: expected.EndHash, CoordinatorRuntimeHash: expected.CoordinatorRuntimeHash, ClientIds: append([][16]byte(nil), expected.ClientIds...), StartResponses: [][]byte{activeRaw, inactiveRaw}, EndResponses: [][]byte{inactiveRaw, inactiveRaw}}
	respond := func(request chainBatchRPCRequest) map[string]any {
		result := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		fail := func() map[string]any {
			result["error"] = map[string]any{"code": -32602, "message": "synthetic provider binding request differs"}
			return result
		}
		if request.Method == "eth_getBlockByHash" {
			var hash common.Hash
			if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &hash) != nil {
				return fail()
			}
			switch [32]byte(hash) {
			case expected.StartHash:
				result["result"] = chainBatchTestHeader
			case expected.EndHash:
				result["result"] = chainBatchAdjacentHeader
			default:
				return fail()
			}
			return result
		}
		if len(request.Params) != 2 {
			return fail()
		}
		var selector gethrpc.BlockNumberOrHash
		if json.Unmarshal(request.Params[1], &selector) != nil || selector.BlockHash == nil || !selector.RequireCanonical {
			return fail()
		}
		hash := [32]byte(*selector.BlockHash)
		if hash != expected.StartHash && hash != expected.EndHash {
			return fail()
		}
		if request.Method == "eth_getCode" {
			var address common.Address
			if json.Unmarshal(request.Params[0], &address) != nil || address != common.Address(expected.Domain.Coordinator) {
				return fail()
			}
			result["result"] = hexutil.Encode(code)
			return result
		}
		if request.Method != "eth_call" {
			return fail()
		}
		var call struct {
			To    common.Address `json:"to"`
			Data  hexutil.Bytes  `json:"data"`
			Input hexutil.Bytes  `json:"input"`
		}
		if json.Unmarshal(request.Params[0], &call) != nil || call.To != common.Address(expected.Domain.Coordinator) {
			return fail()
		}
		data := call.Data
		if len(data) == 0 {
			data = call.Input
		}
		for index, id := range expected.ClientIds {
			if bytes.Equal(data, stabi.NewSTCoordinator().PackBindingAt(id, new(big.Int).SetUint64(expected.Epoch))) {
				raw := original.StartResponses[index]
				if hash == expected.EndHash {
					raw = original.EndResponses[index]
				}
				result["result"] = hexutil.Encode(raw)
				return result
			}
		}
		return fail()
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if trimmed := bytes.TrimSpace(raw); len(trimmed) != 0 && trimmed[0] == '[' {
			var requests []chainBatchRPCRequest
			if err := json.Unmarshal(raw, &requests); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			results := make([]map[string]any, len(requests))
			for index, value := range requests {
				results[len(requests)-1-index] = respond(value)
			}
			_ = json.NewEncoder(writer).Encode(results)
		} else {
			var value chainBatchRPCRequest
			if err := json.Unmarshal(raw, &value); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(writer).Encode(respond(value))
		}
	}))
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	chain := &ChainClient{client: client, coordinator: stabi.NewSTCoordinator(), chainId: new(big.Int).SetUint64(expected.Domain.ChainId), contractAddr: common.Address(expected.Domain.Coordinator), release: true}
	return chain, expected, original
}

// A head active only at the start still excludes the epoch. A complete inactive
// tuple, including native uid zero, is distinct from an absent response.
func TestProviderAttemptBindingsReadsBothOriginalBoundaries(t *testing.T) {
	chain, expected, _ := providerAttemptBindingFixture(t)
	original, err := chain.ReadProviderAttemptBindings(t.Context(), expected)
	if err != nil || original == nil {
		t.Fatalf("actual original binding read failed: %v", err)
	}
	rows, hash, err := VerifyProviderAttemptBindings(t.Context(), original, expected)
	if err != nil || hash == ([32]byte{}) || len(rows) != 2 || !rows[0].HeadExcluded || rows[0].BindingGeneration != 2 || rows[1].HeadExcluded || rows[1].BindingGeneration != 0 {
		t.Fatalf("original start/close exclusion changed: %+v %v", rows, err)
	}
}

// A foreign configured contract/runtime never authenticates its returned rows.
func TestProviderAttemptBindingsRefusesForeignRuntime(t *testing.T) {
	chain, expected, _ := providerAttemptBindingFixture(t)
	expected.CoordinatorRuntimeHash[0] ^= 1
	if result, err := chain.ReadProviderAttemptBindings(t.Context(), expected); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("foreign coordinator executable was accepted: %+v %v", result, err)
	}
}

// Missing, repeated and re-ordered client rows cannot shrink the expected union.
func TestProviderAttemptBindingsRefusesIncompleteAndDuplicateCensus(t *testing.T) {
	_, expected, original := providerAttemptBindingFixture(t)
	original.EndResponses = original.EndResponses[:1]
	if rows, _, err := VerifyProviderAttemptBindings(t.Context(), original, expected); rows != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("missing binding row became inactive: %+v %v", rows, err)
	}
	expected.ClientIds[1] = expected.ClientIds[0]
	if rows, _, err := VerifyProviderAttemptBindings(t.Context(), original, expected); rows != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("duplicate expected client was accepted: %+v %v", rows, err)
	}
}

// Contradictory original active versions do not choose one convenient boundary.
func TestProviderAttemptBindingsRefusesChangedActiveGeneration(t *testing.T) {
	_, expected, original := providerAttemptBindingFixture(t)
	contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	state, err := stabi.NewSTCoordinator().UnpackBindingAt(original.StartResponses[0])
	if err != nil {
		t.Fatal(err)
	}
	state.Record.Generation++
	original.EndResponses[0], err = contractAbi.Methods["bindingAt"].Outputs.Pack(true, state.Record)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _, err := VerifyProviderAttemptBindings(t.Context(), original, expected); rows != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("contradictory active generations were merged: %+v %v", rows, err)
	}
}

// Canonical ABI and the exact original window are independent of RPC success.
func TestProviderAttemptBindingsRefusesMalformedAndCrossWindowOriginal(t *testing.T) {
	_, expected, original := providerAttemptBindingFixture(t)
	original.StartResponses[0] = append(bytes.Clone(original.StartResponses[0]), 0)
	if rows, _, err := VerifyProviderAttemptBindings(t.Context(), original, expected); rows != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("trailing binding ABI bytes were accepted: %+v %v", rows, err)
	}
	original.StartResponses[0] = original.StartResponses[0][:len(original.StartResponses[0])-1]
	original.Epoch++
	if rows, _, err := VerifyProviderAttemptBindings(t.Context(), original, expected); rows != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("foreign binding epoch was accepted: %+v %v", rows, err)
	}
}

// Cancellation retains its typed cause and a healthy same-authority retry works.
func TestProviderAttemptBindingsCanceledOwnerThenRecovery(t *testing.T) {
	chain, expected, _ := providerAttemptBindingFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if original, err := chain.ReadProviderAttemptBindings(ctx, expected); original != nil || !errors.Is(err, context.Canceled) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("canceled binding owner acquired integrity: %+v %v", original, err)
	}
	if original, err := chain.ReadProviderAttemptBindings(t.Context(), expected); original == nil || err != nil {
		t.Fatalf("healthy binding continuation failed: %v", err)
	}
}
