// Synthetic endpoints exercise exact common-height custody through the real
// actor. Endpoint agreement cannot replace the requested block identity.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/protocol"
)

// The transport replaces only network I/O. Method selection, decoding, the
// common-height comparison and the complete actor verdict stay in production.
func adversaryRpcBlockTestActor(t *testing.T, shared bool, edit func(string, *rpcBlock)) (*rpcAdversary, *int) {
	t.Helper()
	cfg := &ResolvedConfig{Config: &HarnessConfig{}, Public: &PublicManifest{}, Policy: &protocol.Policy{}, Release: &ReleaseLock{}, Netuid: 7}
	cfg.OperationalEVM = "https://operational.example"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://public.example"
	if shared {
		cfg.ownedRPCAuthority = "192.0.2.10:19999"
		cfg.OperationalEVM = "http://" + cfg.ownedRPCAuthority
	}
	cfg.Release.Runtime = ReleaseRuntimeLock{SpecVersion: 1, TransactionVersion: 1, StateVersion: 1, CodeHash: "0x" + strings.Repeat("ef", 32)}
	requests := new(int)
	transport := adversaryGetTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != cfg.OperationalEVM && request.URL.String() != verificationEVMEndpoint(cfg) {
			return nil, fmt.Errorf("unexpected synthetic RPC route %s %s", request.Method, request.URL)
		}
		defer request.Body.Close()
		var call struct {
			Id     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			return nil, err
		}
		*requests++
		var result any
		switch call.Method {
		case "chain_getFinalizedHead":
			result = "0x" + strings.Repeat("cd", 32)
		case "chain_getHeader":
			result = rpcHeader{Number: "0x64"}
		case "eth_getBlockByNumber":
			block := rpcBlock{Number: "0x65", Hash: "0x" + strings.Repeat("ab", 32)}
			if len(call.Params) != 2 {
				return nil, fmt.Errorf("unexpected block parameters %s", call.Params)
			}
			if string(call.Params[0]) != `"latest"` {
				if string(call.Params[0]) != `"0x64"` {
					return nil, fmt.Errorf("common height was not pinned: %s", call.Params[0])
				}
				block.Number = "0x64"
				if edit != nil {
					edit(request.URL.Host, &block)
				}
			}
			result = block
		case "state_getRuntimeVersion":
			result = runtimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 1, TransactionVersion: 1, StateVersion: 1}
		case "state_getStorageHash":
			result = cfg.Release.Runtime.CodeHash
		case "eth_call":
			result = fmt.Sprintf("0x%064x", 123)
		default:
			return nil, fmt.Errorf("unexpected synthetic RPC method %s", call.Method)
		}
		raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
		if err != nil {
			return nil, err
		}
		return adversaryGetTestResponse(http.StatusOK, string(raw)), nil
	})
	actor := &rpcAdversary{cfg: cfg, http: &adversaryHTTP{gate: &adversaryRequestGate{now: time.Now}, timeout: time.Second, transportForTest: transport}}
	actor.commitRevealProbe = func(context.Context, *ResolvedConfig) (adversaryCommitRevealObservation, adversaryCommitRevealObservation, uint64, error) {
		left := adversaryCommitRevealObservation{Endpoint: "wss://operational.example", Finalized: 100, FinalizedHash: types.Hash{7}, Enabled: true, Tempo: 10, RevealPeriods: 1}
		right := left
		right.Endpoint = "wss://public.example"
		return left, right, 10, nil
	}
	return actor, requests
}

// Both distinct endpoints and an explicitly owned shared route retain their
// original authority while matching the exact requested finalized height.
func TestAdversaryRpcCommonBlockAcceptsExactHeight(t *testing.T) {
	for _, shared := range []bool{false, true} {
		actor, requests := adversaryRpcBlockTestActor(t, shared, func(host string, block *rpcBlock) {
			if host == "public.example" {
				block.Hash = "0x" + strings.Repeat("AB", 32)
			}
		})
		result := actor.Sample(t.Context(), adversaryAttackPhase, 2)
		if result.Outcome != adversaryOutcomeSuccess || result.Requests != 20 || *requests != 20 || result.Metrics["hash_disagreement_count"] != 0 {
			t.Fatalf("shared=%t exact common block failed: requests=%d result=%+v", shared, *requests, result)
		}
	}
}

// Identical wrong-height answers and a one-sided substitution are both hard
// failures before runtime or precompile observations can acquire authority.
func TestAdversaryRpcCommonBlockRejectsReturnedHeightDrift(t *testing.T) {
	for _, test := range []struct {
		host   string
		number string
	}{{number: "0x63"}, {number: "0x65"}, {host: "operational.example", number: "0x63"}, {host: "public.example", number: "0x65"}} {
		actor, requests := adversaryRpcBlockTestActor(t, false, func(host string, block *rpcBlock) {
			if test.host == "" || host == test.host {
				block.Number = test.number
			}
		})
		result := actor.Sample(t.Context(), adversaryAttackPhase, 2)
		if result.Outcome != adversaryOutcomeError || result.Requests != 8 || *requests != 8 || !strings.Contains(result.Detail, "common-height disagreement height=100") {
			t.Errorf("host=%q number=%s accepted a foreign common block: requests=%d result=%+v", test.host, test.number, *requests, result)
		}
	}
}

// Matching malformed hashes are not evidence that two endpoints agree on a
// block, even when their requested and returned block numbers match exactly.
func TestAdversaryRpcCommonBlockRejectsMalformedHashAgreement(t *testing.T) {
	for _, hash := range []string{strings.Repeat("ab", 32), "0x" + strings.Repeat("zz", 32)} {
		actor, requests := adversaryRpcBlockTestActor(t, false, func(_ string, block *rpcBlock) { block.Hash = hash })
		result := actor.Sample(t.Context(), adversaryAttackPhase, 2)
		if result.Outcome != adversaryOutcomeError || result.Requests != 8 || *requests != 8 {
			t.Errorf("malformed hash %q acquired common-block authority: requests=%d result=%+v", hash, *requests, result)
		}
	}
}

// The shared decoder also protects latest-block and live Merkle readers, with
// exact width and hexadecimal encoding rather than a length-only predicate.
func TestAdversaryRpcBlockDecoderRequiresCanonicalHash(t *testing.T) {
	for _, hash := range []string{"0x" + strings.Repeat("ab", 32), "0x" + strings.Repeat("AB", 32)} {
		raw, _ := json.Marshal(rpcBlock{Number: "0x64", Hash: hash})
		if block, number, err := decodeRPCBlock(rpcResponse{Result: raw}); err != nil || number != 100 || block.Hash != hash {
			t.Fatalf("valid hash rejected: block=%+v number=%d err=%v", block, number, err)
		}
	}
	for _, hash := range []string{strings.Repeat("ab", 32), "0x" + strings.Repeat("zz", 32), "0x" + strings.Repeat("ab", 31), "0x" + strings.Repeat("ab", 33)} {
		raw, _ := json.Marshal(rpcBlock{Number: "0x64", Hash: hash})
		if _, _, err := decodeRPCBlock(rpcResponse{Result: raw}); err == nil {
			t.Errorf("malformed block hash accepted: %q", hash)
		}
	}
}
