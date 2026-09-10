package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/stabi"
)

type bootstrapPolicyPostconditionObservation struct {
	block   uint64
	current uint64
	active  stabi.STCoordinatorPolicySnapshot
	latest  stabi.STCoordinatorPolicySnapshot
}

// Each observation is a distinct pinned chain snapshot. The fixture accepts
// only its declared read-only calldata, target, and block combinations.
func bootstrapPolicyPostconditionExecutor(t *testing.T, cfg *ResolvedConfig, observations ...bootstrapPolicyPostconditionObservation) (*Executor, Action) {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	proxy := common.HexToAddress("0x1000000000000000000000000000000000000001")
	outputs := map[string]map[string]string{}
	for _, observation := range observations {
		calls := map[string]string{}
		addPolicyRevisionRPCOutput(t, calls, parsed, "currentEpoch", nil, new(big.Int).SetUint64(observation.current))
		addPolicyRevisionRPCOutput(t, calls, parsed, "policyCount", nil, big.NewInt(2))
		addPolicyRevisionRPCOutput(t, calls, parsed, "policyAt", []any{new(big.Int).SetUint64(observation.current)}, observation.active)
		addPolicyRevisionRPCOutput(t, calls, parsed, "policyByIndex", []any{big.NewInt(1)}, observation.latest)
		outputs[fmt.Sprintf("0x%x", observation.block)] = calls
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		var envelope struct {
			To    string `json:"to"`
			Data  string `json:"data"`
			Input string `json:"input"`
		}
		var block string
		var result string
		if call.Method == "eth_call" && len(call.Params) == 2 && json.Unmarshal(call.Params[0], &envelope) == nil && json.Unmarshal(call.Params[1], &block) == nil && strings.EqualFold(envelope.To, proxy.Hex()) {
			data := envelope.Data
			if data == "" {
				data = envelope.Input
			}
			result = outputs[block][strings.ToLower(data)]
		}
		if result == "" {
			response["error"] = map[string]any{"code": -32602, "message": "unplanned bootstrap policy read"}
		} else {
			response["result"] = result
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode bootstrap policy response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &Executor{cfg: cfg, owner: &EvmTxManager{client: client}, payloads: &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: proxy}}}, Action{ID: "policy.schedule-bootstrap", Kind: "evm-transaction", Target: proxy.Hex()}
}

func bootstrapPolicyPostconditionSnapshot(t *testing.T, cfg *ResolvedConfig) stabi.STCoordinatorPolicySnapshot {
	t.Helper()
	hash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	return stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: 50, EffectiveBlock: 120,
		EpochBlocks: cfg.Policy.Settlement.EpochBlocks, RootCommitWindowBlocks: cfg.Policy.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: cfg.Policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: cfg.Policy.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs: cfg.Policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: cfg.Policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: cfg.Policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: cfg.Policy.Settlement.EpochBlocks * 2,
		EpochDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao),
	}
}

// The same canonical schedule remains valid at activation and on a later
// resume; only its current-epoch/active observations change.
func TestBootstrapPolicySchedulePostconditionSurvivesActivation(t *testing.T) {
	cfg := testResolvedConfig(t)
	scheduled := bootstrapPolicyPostconditionSnapshot(t, cfg)
	prior := scheduled
	prior.PolicyHash[0] ^= 1
	prior.EffectiveEpoch, prior.EffectiveBlock = 1, 1
	executor, action := bootstrapPolicyPostconditionExecutor(t, cfg,
		bootstrapPolicyPostconditionObservation{100, 49, prior, scheduled},
		bootstrapPolicyPostconditionObservation{120, 50, scheduled, scheduled},
		bootstrapPolicyPostconditionObservation{60720, 252, scheduled, scheduled},
	)
	var pending map[string]any
	for _, phase := range []struct {
		name    string
		block   uint64
		current uint64
		active  bool
	}{{"pending", 100, 49, false}, {"activation", 120, 50, true}, {"later_resume", 60720, 252, true}} {
		t.Run(phase.name, func(t *testing.T) {
			head := ChainHead{Number: phase.block, Hash: fmt.Sprintf("0x%064x", phase.block)}
			state, err := executor.actionPostState(context.Background(), action, head)
			if err != nil {
				t.Fatalf("canonical bootstrap schedule rejected during %s: %v", phase.name, err)
			}
			if state["active"] != phase.active || state["current_epoch"] != phase.current || state["policy_count"] != uint64(2) || state["policy_hash"] != cfg.PolicyHash {
				t.Fatalf("incorrect bootstrap observation: %+v", state)
			}
			for key, expected := range map[string]any{
				"scheduled_policy_index": uint64(1), "scheduled_policy_hash": strings.ToLower(cfg.PolicyHash),
				"scheduled_policy_effective_epoch": scheduled.EffectiveEpoch, "scheduled_policy_effective_block": scheduled.EffectiveBlock,
			} {
				if !reflect.DeepEqual(state[key], expected) || (pending != nil && !reflect.DeepEqual(state[key], pending[key])) {
					t.Fatalf("canonical schedule field %s changed: got=%v expected=%v", key, state[key], expected)
				}
			}
			if phase.name == "pending" {
				pending = state
			}
			if err := executor.verifyCurrentActionPostState(context.Background(), action, &head); err != nil {
				t.Fatalf("resume current postcondition rejected during %s: %v", phase.name, err)
			}
			await := action
			await.ID = "policy.await-bootstrap"
			if err := executor.verifyCurrentActionPostState(context.Background(), await, &head); (err == nil) != phase.active {
				t.Fatalf("await accepted pending or rejected active bootstrap: active=%t error=%v", phase.active, err)
			}
		})
	}
}

// Accepting an activated policy must not admit a foreign latest schedule,
// mutated locked fields, or a stale schedule whose policy is not active.
func TestBootstrapPolicySchedulePostconditionRejectsForeignOrStaleState(t *testing.T) {
	for _, mutation := range []struct {
		name  string
		apply func(*stabi.STCoordinatorPolicySnapshot, *stabi.STCoordinatorPolicySnapshot)
	}{
		{"latest_hash", func(_, latest *stabi.STCoordinatorPolicySnapshot) { latest.PolicyHash[0] ^= 1 }},
		{"latest_epoch_blocks", func(_, latest *stabi.STCoordinatorPolicySnapshot) { latest.EpochBlocks++ }},
		{"latest_epoch_cap", func(_, latest *stabi.STCoordinatorPolicySnapshot) {
			latest.EpochDepositCapRao = new(big.Int).Add(latest.EpochDepositCapRao, big.NewInt(1))
		}},
		{"latest_campaign_cap", func(_, latest *stabi.STCoordinatorPolicySnapshot) {
			latest.CampaignDepositCapRao = new(big.Int).Add(latest.CampaignDepositCapRao, big.NewInt(1))
		}},
		{"latest_effective_block", func(_, latest *stabi.STCoordinatorPolicySnapshot) { latest.EffectiveBlock = 0 }},
		{"inactive_stale_schedule", func(active, _ *stabi.STCoordinatorPolicySnapshot) { active.PolicyHash[0] ^= 1 }},
		{"active_locked_field", func(active, _ *stabi.STCoordinatorPolicySnapshot) { active.FinalizeOffsetBlocks++ }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			cfg := testResolvedConfig(t)
			active, latest := bootstrapPolicyPostconditionSnapshot(t, cfg), bootstrapPolicyPostconditionSnapshot(t, cfg)
			mutation.apply(&active, &latest)
			executor, action := bootstrapPolicyPostconditionExecutor(t, cfg, bootstrapPolicyPostconditionObservation{60720, 252, active, latest})
			head := ChainHead{Number: 60720, Hash: fmt.Sprintf("0x%064x", 60720)}
			if err := executor.verifyCurrentActionPostState(context.Background(), action, &head); err == nil {
				t.Fatal("accepted foreign, mutated, or inactive stale bootstrap schedule")
			}
		})
	}
}
