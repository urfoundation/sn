// Fleet cleanup's historical pre-state must retain its explicit EVM parent
// identity across capture, crash recovery, and postcondition validation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/stabi"
)

// Replays the real finalized cleanup validator with exact manifest, journal,
// receipt, event, and historical binding states around a synthetic parent.
func syntheticFleetCleanupFixture(t *testing.T, failure string) (*Executor, Action, FleetLifecycleCleanupEvidence) {
	t.Helper()
	cfg := testResolvedConfig(t)
	root := filepath.Join(t.TempDir(), "state")
	if err := ensurePrivateDir(root); err != nil {
		t.Fatal(err)
	}
	variant, err := fleetLifecycleVariantFor(fleetLifecycleVariantTargetTakeover)
	if err != nil {
		t.Fatal(err)
	}
	roles := &RoleSecrets{DeploymentID: cfg.Config.Deployment.DeploymentID, Substrate: map[string]SubstrateRoleSecret{variant.HotkeyLabel: {PublicKeyHex: strings.Repeat("11", 32)}}, Clients: map[string]ClientRoleSecret{}}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(cfg, variant.Fleet, member)
		label := fmt.Sprintf("miner-%d", miner)
		roles.Clients[label] = ClientRoleSecret{Label: label, ClientIDHex: fmt.Sprintf("%032x", miner), PublicKeyHex: fmt.Sprintf("%064x", miner)}
	}
	deployment := ContractDeployment{DeploymentID: roles.DeploymentID, CoordinatorProxy: common.HexToAddress("0x1234")}
	if err := writePublicJSON(filepath.Join(root, "public", "contracts.json"), deployment); err != nil {
		t.Fatal(err)
	}
	manifest, _, _, err := fleetLifecycleVariantManifest(cfg, root, roles, variant)
	if err != nil {
		t.Fatal(err)
	}
	member := manifest.Members[0]
	actionID, err := fleetLifecycleCleanupActionID(variant.Name, 1)
	if err != nil {
		t.Fatal(err)
	}
	action := Action{ID: actionID, Kind: "evm-transaction", Target: fmt.Sprintf("miner:%d", fleetMemberMinerIndex(cfg, variant.Fleet, 1)), IntentHash: finalTestHex(0x44)}
	plan := &SetupPlan{PlanHash: finalTestHex(0x55), DeploymentID: roles.DeploymentID, Actions: []Action{action}, Deployment: deployment}
	parent := ChainHead{Number: 9, Hash: finalTestHex(0x19)}
	inclusion := ChainHead{Number: 10, Hash: finalTestHex(0x20)}
	evidence := FleetLifecycleCleanupEvidence{
		Schema: "urnetwork-sim-fleet-binding-cleanup-v2", DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		ClientID: fleetLifecycleHex16(member.ClientID), FleetID: fleetLifecycleHex(manifest.FleetID), Generation: manifest.Generation,
		CleanedAtEpoch: 5, MemberCountBefore: uint64(len(manifest.Members)), MemberCountAfter: uint64(len(manifest.Members) - 1),
		TransactionHash: finalTestHex(0x66), BeforeBlock: parent, BlockNumber: inclusion.Number, BlockHash: inclusion.Hash,
	}
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.TransactionHash, BlockNumber: inclusion.Number, BlockHash: inclusion.Hash, RecoveryBlock: parent.Number, RecoveryBlockHash: parent.Hash}); err != nil {
		t.Fatal(err)
	}
	var clientTopic common.Hash
	copy(clientTopic[:], member.ClientID[:])
	receipt := &types.Receipt{
		TxHash: common.HexToHash(evidence.TransactionHash), Status: types.ReceiptStatusSuccessful, BlockNumber: new(big.Int).SetUint64(inclusion.Number), BlockHash: common.HexToHash(inclusion.Hash),
		Logs: []*types.Log{{Address: deployment.CoordinatorProxy, Topics: []common.Hash{crypto.Keccak256Hash([]byte("FleetBindingCleaned(bytes16,uint64)")), clientTopic, common.BigToHash(new(big.Int).SetUint64(evidence.CleanedAtEpoch))}, BlockNumber: inclusion.Number, BlockHash: common.HexToHash(inclusion.Hash), TxHash: common.HexToHash(evidence.TransactionHash)}},
	}
	contractABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	before := stabi.STCoordinatorBindingRecord{FleetId: manifest.FleetID, Hotkey: manifest.Hotkey, ClientKey: member.ClientKey, Generation: manifest.Generation}
	after := before
	after.Cleaned, after.CleanedAtEpoch = true, evidence.CleanedAtEpoch
	beforeBytes, err := contractABI.Methods["getFleetBinding"].Outputs.Pack(before)
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, err := contractABI.Methods["getFleetBinding"].Outputs.Pack(after)
	if err != nil {
		t.Fatal(err)
	}
	parentBlock := syntheticIdentityTestBlock(t, parent)
	inclusionBlock := syntheticIdentityTestBlock(t, inclusion)
	replacement := syntheticIdentityTestBlock(t, ChainHead{Number: parent.Number, Hash: finalTestHex(0x29)})
	var stateReads atomic.Int64
	endpoint := syntheticIdentityTestRPC(t, func(method string, parameters []json.RawMessage) (any, error) {
		switch method {
		case "eth_getBlockByNumber":
			if len(parameters) != 2 {
				return nil, fmt.Errorf("invalid block lookup parameters %s", parameters)
			}
			if string(parameters[0]) != `"0x9"` {
				return inclusionBlock, nil
			}
			switch failure {
			case "null":
				return nil, nil
			case "wrong number":
				return inclusionBlock, nil
			case "zero hash":
				return map[string]any{"number": "0x9", "hash": common.Hash{}.Hex()}, nil
			case "wrong hash":
				return replacement, nil
			case "reorg":
				if stateReads.Load() == 4 {
					return replacement, nil
				}
			}
			return parentBlock, nil
		case "eth_getTransactionReceipt":
			return receipt, nil
		case "eth_call":
			stateReads.Add(1)
			var argument struct {
				Input hexutil.Bytes `json:"input"`
			}
			if len(parameters) != 2 || json.Unmarshal(parameters[0], &argument) != nil || len(argument.Input) < 4 {
				return nil, fmt.Errorf("invalid historical cleanup arguments %s", parameters)
			}
			prior := string(parameters[1]) == `"0x9"`
			if !prior && string(parameters[1]) != `"0xa"` {
				return nil, fmt.Errorf("wrong historical cleanup height %s", parameters[1])
			}
			if bytes.Equal(argument.Input[:4], contractABI.Methods["getFleetBinding"].ID) {
				if prior {
					return hexutil.Encode(beforeBytes), nil
				}
				return hexutil.Encode(afterBytes), nil
			}
			if bytes.Equal(argument.Input[:4], contractABI.Methods["fleetMemberCount"].ID) {
				count := evidence.MemberCountAfter
				if prior {
					count = evidence.MemberCountBefore
				}
				return "0x" + fmt.Sprintf("%064x", count), nil
			}
			return nil, fmt.Errorf("unknown historical cleanup call %x", argument.Input)
		default:
			return nil, fmt.Errorf("unexpected fleet cleanup method %s", method)
		}
	})
	client, err := ethclient.Dial(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &Executor{cfg: cfg, stateDir: root, roles: roles, plan: plan, keeper: &EVMTxManager{client: client, journal: journal}}, action, evidence
}

// The complete cleanup verifier accepts a canonical synthetic parent hash.
func TestSyntheticEVMFleetCleanupPreservesParentRPCIdentity(t *testing.T) {
	executor, action, evidence := syntheticFleetCleanupFixture(t, "")
	if err := executor.validateFleetLifecycleCleanupAction(context.Background(), action, fleetLifecycleVariantTargetTakeover, 1, evidence); err != nil {
		t.Fatalf("synthetic fleet cleanup parent: %v", err)
	}
}

// A substituted, absent, or malformed parent cannot authorize cleanup proof.
func TestSyntheticEVMFleetCleanupRejectsParentRPCFailures(t *testing.T) {
	for _, failure := range []string{"null", "wrong number", "zero hash", "wrong hash"} {
		executor, action, evidence := syntheticFleetCleanupFixture(t, failure)
		if err := executor.validateFleetLifecycleCleanupAction(context.Background(), action, fleetLifecycleVariantTargetTakeover, 1, evidence); err == nil {
			t.Errorf("accepted %s cleanup parent", failure)
		}
	}
}

// The final pre-state read deterministically triggers a same-height fork.
func TestSyntheticEVMFleetCleanupRejectsReorgDuringStateReads(t *testing.T) {
	executor, action, evidence := syntheticFleetCleanupFixture(t, "reorg")
	if err := executor.validateFleetLifecycleCleanupAction(context.Background(), action, fleetLifecycleVariantTargetTakeover, 1, evidence); err == nil || !strings.Contains(err.Error(), "parent changed during capture") {
		t.Fatalf("reorged cleanup parent error=%v", err)
	}
}
