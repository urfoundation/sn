package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/stabi"
)

// ActionPostcondition is the durable, canonical proof behind a verified
// journal stage. It deliberately contains finalized checkpoints and observed
// values, not a wall-clock assertion that the executor returned nil.
type ActionPostcondition struct {
	Schema                        string         `json:"schema"`
	DeploymentID                  string         `json:"deployment_id"`
	PlanHash                      string         `json:"plan_hash"`
	ActionID                      string         `json:"action_id"`
	IntentHash                    string         `json:"intent_hash"`
	OperationalRPCMode            string         `json:"operational_rpc_mode"`
	IndependentRPC                bool           `json:"independent_rpc"`
	SubstrateFinalized            ChainHead      `json:"substrate_finalized"`
	EVMFinalized                  ChainHead      `json:"evm_finalized"`
	Observed                      map[string]any `json:"observed"`
	IndependentSubstrateFinalized ChainHead      `json:"independent_substrate_finalized"`
	IndependentEVMFinalized       ChainHead      `json:"independent_evm_finalized"`
	IndependentObserved           map[string]any `json:"independent_observed"`
}

func legacyPostconditionRelativePath(actionID string) (string, error) {
	if actionID == "" || strings.ContainsAny(actionID, `/\\`) || filepath.Base(actionID) != actionID {
		return "", fmt.Errorf("unsafe action id %q", actionID)
	}
	return filepath.ToSlash(filepath.Join("receipts", "postconditions", actionID+".json")), nil
}

// Scope new receipts by plan hash so a revised action cannot overwrite the
// durable evidence referenced by an ancestor journal entry.
func postconditionRelativePath(planHash, actionID string) (string, error) {
	if _, err := decodeHex32("postcondition plan hash", planHash); err != nil {
		return "", err
	}
	if _, err := legacyPostconditionRelativePath(actionID); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("receipts", "postconditions", stringsTrim0x(planHash), actionID+".json")), nil
}

func (e *Executor) verifyActionPostcondition(ctx context.Context, a Action) (*ActionPostcondition, error) {
	finalizedHash, finalizedNumber, err := e.substrate.finalizedHead()
	if err != nil {
		return nil, fmt.Errorf("substrate finalized checkpoint: %w", err)
	}
	evmHead, err := finalizedEVMHead(ctx, e.deployer.client)
	if err != nil {
		return nil, fmt.Errorf("EVM finalized checkpoint: %w", err)
	}
	observed, err := e.actionPostState(ctx, a, evmHead)
	if err != nil {
		return nil, err
	}
	operationalSubstrate := ChainHead{Number: finalizedNumber, Hash: finalizedHash.Hex()}
	if !independentRPCRequired(e.cfg) {
		// Public override mode intentionally names the same provider for both
		// routes. Reissuing every read cannot add independence and can trip the
		// provider's source-wide quota, so preserve a detached copy of the same
		// observation while recording IndependentRPC=false.
		independentObserved, cloneErr := cloneObservedPostState(observed)
		if cloneErr != nil {
			return nil, cloneErr
		}
		return &ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v1", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
			PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash,
			OperationalRPCMode: e.cfg.OperationalRPCMode, IndependentRPC: false,
			SubstrateFinalized: operationalSubstrate, EVMFinalized: evmHead, Observed: observed,
			IndependentSubstrateFinalized: operationalSubstrate, IndependentEVMFinalized: evmHead,
			IndependentObserved: independentObserved,
		}, nil
	}
	independentSubstrate, independentEVM, err := e.waitIndependentCheckpoints(
		ctx,
		operationalSubstrate,
		evmHead,
	)
	if err != nil {
		return nil, err
	}
	observer := e.independentReadExecutor()
	independentObserved, err := observer.actionPostState(ctx, a, independentEVM)
	if err != nil {
		return nil, fmt.Errorf("independent RPC postcondition: %w", err)
	}
	return &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v1", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
		PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash,
		OperationalRPCMode: e.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(e.cfg),
		SubstrateFinalized: ChainHead{Number: finalizedNumber, Hash: finalizedHash.Hex()}, EVMFinalized: evmHead,
		Observed: observed, IndependentSubstrateFinalized: independentSubstrate,
		IndependentEVMFinalized: independentEVM, IndependentObserved: independentObserved,
	}, nil
}

func cloneObservedPostState(observed map[string]any) (map[string]any, error) {
	if observed == nil {
		return nil, errors.New("action post-state observation is unavailable")
	}
	encoded, err := json.Marshal(observed)
	if err != nil {
		return nil, fmt.Errorf("encode shared-provider post-state: %w", err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil, fmt.Errorf("decode shared-provider post-state: %w", err)
	}
	return cloned, nil
}

func checkpointVisibility(operational, independent ChainHead, canonicalAtOperational string) (bool, error) {
	if operational.Number == 0 || operational.Hash == "" || independent.Hash == "" {
		return false, errors.New("checkpoint identity is incomplete")
	}
	if independent.Number < operational.Number {
		return false, nil
	}
	if !strings.EqualFold(canonicalAtOperational, operational.Hash) {
		return false, fmt.Errorf("independent RPC canonical hash %s at block %d differs from operational finalized hash %s", canonicalAtOperational, operational.Number, operational.Hash)
	}
	return true, nil
}

func (e *Executor) waitIndependentCheckpoints(ctx context.Context, operationalSubstrate, operationalEVM ChainHead) (ChainHead, ChainHead, error) {
	if e.independentSubstrate == nil || e.independentEVM == nil {
		return ChainHead{}, ChainHead{}, errors.New("independent RPC clients are unavailable")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		independentSubstrateHash, independentSubstrateNumber, substrateErr := e.independentSubstrate.finalizedHead()
		independentSubstrate := ChainHead{Number: independentSubstrateNumber, Hash: independentSubstrateHash.Hex()}
		substrateReady := false
		if substrateErr == nil && independentSubstrateNumber >= operationalSubstrate.Number {
			canonical, readErr := e.independentSubstrate.chain.API.RPC.Chain.GetBlockHash(operationalSubstrate.Number)
			if readErr != nil {
				substrateErr = readErr
			} else {
				substrateReady, substrateErr = checkpointVisibility(operationalSubstrate, independentSubstrate, canonical.Hex())
			}
		}

		independentEVM, evmErr := finalizedEVMHead(waitCtx, e.independentEVM)
		evmReady := false
		if evmErr == nil && independentEVM.Number >= operationalEVM.Number {
			var canonical string
			if readErr := e.independentEVM.Client().CallContext(waitCtx, &canonical, "chain_getBlockHash", operationalEVM.Number); readErr != nil {
				evmErr = readErr
			} else {
				evmReady, evmErr = checkpointVisibility(operationalEVM, independentEVM, canonical)
			}
		}
		if substrateErr != nil {
			return ChainHead{}, ChainHead{}, fmt.Errorf("independent Substrate checkpoint: %w", substrateErr)
		}
		if evmErr != nil {
			return ChainHead{}, ChainHead{}, fmt.Errorf("independent EVM checkpoint: %w", evmErr)
		}
		if substrateReady && evmReady {
			return independentSubstrate, independentEVM, nil
		}
		select {
		case <-waitCtx.Done():
			return ChainHead{}, ChainHead{}, fmt.Errorf("independent RPCs did not finalize the operational checkpoints within five minutes: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func verifyEVMCheckpoint(ctx context.Context, client *ethclient.Client, finalized, recorded ChainHead) error {
	if client == nil {
		return errors.New("EVM checkpoint client is unavailable")
	}
	if recorded.Number == 0 || recorded.Hash == "" {
		return errors.New("recorded EVM checkpoint identity is incomplete")
	}
	var canonical string
	if err := client.Client().CallContext(ctx, &canonical, "chain_getBlockHash", recorded.Number); err != nil {
		return err
	}
	ready, err := checkpointVisibility(recorded, finalized, canonical)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("recorded EVM block %d is not finalized (head %d)", recorded.Number, finalized.Number)
	}
	return nil
}

func receiptMatchesEvidence(finalized ChainHead, receipt *ethTypes.Receipt, transactionHash string, blockNumber uint64, blockHash string) bool {
	if _, ok := evidenceFixedHex(transactionHash, 32); !ok {
		return false
	}
	if _, ok := evidenceFixedHex(blockHash, 32); !ok || blockNumber == 0 || receipt == nil || receipt.Status != ethTypes.ReceiptStatusSuccessful || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() {
		return false
	}
	return receipt.TxHash == common.HexToHash(transactionHash) && receipt.BlockNumber.Uint64() == blockNumber &&
		strings.EqualFold(receipt.BlockHash.Hex(), blockHash) && receiptIsCanonicalAndFinalized(finalized.Number, receipt, blockHash)
}

func verifyFinalizedEVMReceipt(ctx context.Context, client *ethclient.Client, finalized ChainHead, transactionHash string, blockNumber uint64, blockHash string) (*ethTypes.Receipt, error) {
	if err := verifyEVMCheckpoint(ctx, client, finalized, ChainHead{Number: blockNumber, Hash: blockHash}); err != nil {
		return nil, err
	}
	receipt, err := client.TransactionReceipt(ctx, common.HexToHash(transactionHash))
	if err != nil {
		return nil, err
	}
	if !receiptMatchesEvidence(finalized, receipt, transactionHash, blockNumber, blockHash) {
		return nil, errors.New("EVM receipt does not match its canonical finalized evidence")
	}
	return receipt, nil
}

func cloneReadManager(manager *EVMTxManager, client *ethclient.Client) *EVMTxManager {
	if manager == nil {
		return nil
	}
	cloned := *manager
	cloned.client = client
	return &cloned
}

func (e *Executor) independentReadExecutor() *Executor {
	cloned := *e
	cloned.substrate = e.independentSubstrate
	cloned.deployer = cloneReadManager(e.deployer, e.independentEVM)
	cloned.owner = cloneReadManager(e.owner, e.independentEVM)
	cloned.guardian = cloneReadManager(e.guardian, e.independentEVM)
	cloned.oracle = cloneReadManager(e.oracle, e.independentEVM)
	cloned.keeper = cloneReadManager(e.keeper, e.independentEVM)
	cloned.deposits = make(map[int]*EVMTxManager, len(e.deposits))
	for id, manager := range e.deposits {
		cloned.deposits[id] = cloneReadManager(manager, e.independentEVM)
	}
	return &cloned
}

func (e *Executor) persistActionPostcondition(record *ActionPostcondition) (string, string, error) {
	relative, err := postconditionRelativePath(record.PlanHash, record.ActionID)
	if err != nil {
		return "", "", err
	}
	hash, err := canonicalHashHex(record)
	if err != nil {
		return "", "", err
	}
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := atomicWrite(filepath.Join(e.stateDir, filepath.FromSlash(relative)), append(b, '\n'), 0o600); err != nil {
		return "", "", err
	}
	return relative, hash, nil
}

func (e *Executor) verifyPersistedPostcondition(entry JournalEntry) error {
	if entry.PostconditionPath == "" {
		return errors.New("verified journal entry has no postcondition path")
	}
	wantPath, err := postconditionRelativePath(entry.PlanHash, entry.ActionID)
	legacyPath, legacyErr := legacyPostconditionRelativePath(entry.ActionID)
	if (err != nil || entry.PostconditionPath != wantPath) && (legacyErr != nil || entry.PostconditionPath != legacyPath) {
		return fmt.Errorf("postcondition path %q is not canonical", entry.PostconditionPath)
	}
	b, err := os.ReadFile(filepath.Join(e.stateDir, filepath.FromSlash(entry.PostconditionPath)))
	if err != nil {
		return err
	}
	var record ActionPostcondition
	if err := json.Unmarshal(b, &record); err != nil {
		return err
	}
	if record.Schema != "urnetwork-sim-action-postcondition-v1" || record.DeploymentID != e.cfg.Config.Deployment.DeploymentID || record.PlanHash != entry.PlanHash || !e.plan.allowedPlanHashes()[entry.PlanHash] || record.ActionID != entry.ActionID || record.IntentHash != entry.IntentHash || record.OperationalRPCMode != e.cfg.OperationalRPCMode || record.IndependentRPC != independentRPCRequired(e.cfg) {
		return errors.New("persisted action postcondition identity mismatch")
	}
	if record.IndependentSubstrateFinalized.Number == 0 || record.IndependentSubstrateFinalized.Hash == "" || record.IndependentEVMFinalized.Number == 0 || record.IndependentEVMFinalized.Hash == "" || record.IndependentObserved == nil {
		return errors.New("persisted action postcondition lacks independent RPC evidence")
	}
	got, err := canonicalHashHex(record)
	if err != nil {
		return err
	}
	if got != entry.PostconditionHash {
		return fmt.Errorf("persisted postcondition hash %s, journal requires %s", got, entry.PostconditionHash)
	}
	return nil
}

func lifecycleHyperparameterExpectation(cfg *ResolvedConfig, name string, productionVerified bool) (any, string, error) {
	if cfg == nil || cfg.Hyperparameters == nil {
		return nil, "", errors.New("hyperparameter configuration is unavailable")
	}
	want, ok := cfg.Hyperparameters.OwnerControlled[name]
	if !ok {
		return nil, "", fmt.Errorf("owner hyperparameter %s is unavailable", name)
	}
	if !productionVerified {
		return want, "", nil
	}
	want, ok = cfg.Hyperparameters.ProductionOwnerControlled[name]
	if !ok {
		return nil, "", fmt.Errorf("production owner hyperparameter %s is unavailable", name)
	}
	return want, "production.hyperparameter." + name, nil
}

func validateUnmutatedSetupTopology(current SubnetTopologyFacts, planned SetupFacts) error {
	owner, err := decodeHex32("planned subnet owner hotkey", planned.SubnetOwnerHotkey)
	if err != nil {
		return err
	}
	uidZero, err := decodeHex32("planned UID zero hotkey", planned.UIDZeroHotkey)
	if err != nil {
		return err
	}
	if current.UIDCount != planned.ExistingUIDCount || current.OwnerHotkey != owner || current.UIDZero != uidZero {
		return fmt.Errorf("subnet topology changed after planning: current count=%d owner=0x%x uid0=0x%x; planned count=%d owner=0x%x uid0=0x%x", current.UIDCount, current.OwnerHotkey, current.UIDZero, planned.ExistingUIDCount, owner, uidZero)
	}
	return nil
}

func validateUnmutatedExistingUIDs(current, planned []ExistingUIDFact) error {
	if len(current) != len(planned) {
		return fmt.Errorf("existing UID identity count changed after planning: current=%d planned=%d", len(current), len(planned))
	}
	for index := range planned {
		if current[index] != planned[index] {
			return fmt.Errorf("existing UID %d changed after planning: current=%+v planned=%+v", index, current[index], planned[index])
		}
	}
	return nil
}

func (e *Executor) actionPostState(ctx context.Context, a Action, evmHead ChainHead) (map[string]any, error) {
	state := map[string]any{"kind": a.Kind, "target": a.Target}
	set := func(key string, value any) (map[string]any, error) { state[key] = value; return state, nil }
	switch {
	case a.ID == "subnet.verify-owner":
		if err, owner := verifySubnetOwner(e.substrate.chain, e.cfg.Netuid, e.cfg.WalletPublic); err != nil {
			return nil, err
		} else {
			state["netuid"], state["owner"] = e.cfg.Netuid, owner
			if !e.registrationSetupProgressed() {
				topology, topologyErr := e.substrate.SubnetTopology()
				if topologyErr != nil {
					return nil, topologyErr
				}
				if topologyErr := validateUnmutatedSetupTopology(topology, e.plan.LiveFacts); topologyErr != nil {
					return nil, topologyErr
				}
				existing, existingErr := e.substrate.ExistingUIDFacts()
				if existingErr != nil {
					return nil, existingErr
				}
				if existingErr := validateUnmutatedExistingUIDs(existing, e.plan.LiveFacts.ExistingUIDs); existingErr != nil {
					return nil, existingErr
				}
				state["initial_uid_count"] = topology.UIDCount
				state["initial_owner_hotkey"] = "0x" + hex.EncodeToString(topology.OwnerHotkey[:])
				state["preserved_bootstrap_uids"] = len(existing)
			}
			return state, nil
		}
	case strings.HasPrefix(a.ID, "subnet.hyperparameter."):
		name := strings.TrimPrefix(a.ID, "subnet.hyperparameter.")
		productionActionID := "production.hyperparameter." + name
		want, supersededBy, expectationErr := lifecycleHyperparameterExpectation(e.cfg, name, e.actionVerified(productionActionID))
		if expectationErr != nil {
			return nil, expectationErr
		}
		got, err := e.substrate.ReadHyper(name)
		if err != nil || !hyperEqual(got, want, hyperShapes[name].Kind) {
			return nil, fmt.Errorf("hyperparameter %s postcondition is %v: %w", name, got, err)
		}
		state["name"], state["value"] = name, got
		if supersededBy != "" {
			state["superseded_by"] = supersededBy
		}
		return state, nil
	case strings.HasPrefix(a.ID, "production.hyperparameter."):
		name := strings.TrimPrefix(a.ID, "production.hyperparameter.")
		shape, ok := hyperShapes[name]
		want, canonical := e.cfg.Hyperparameters.ProductionOwnerControlled[name]
		if !ok || !canonical {
			return nil, fmt.Errorf("production hyperparameter %q is not canonical or supported", name)
		}
		got, err := e.substrate.ReadHyper(name)
		if err != nil || !hyperEqual(got, want, shape.Kind) {
			return nil, fmt.Errorf("production %s postcondition is %v: %w", name, got, err)
		}
		state["name"], state["value"] = name, got
		return state, nil
	case strings.HasPrefix(a.ID, "evm.fund-"):
		usableRao, err := evmFundingTerms(a, e.plan.LiveFacts.ExistentialDepositRao)
		if err != nil {
			return nil, err
		}
		addr := common.HexToAddress(a.Target)
		balance, err := e.deployer.client.BalanceAt(ctx, addr, new(big.Int).SetUint64(evmHead.Number))
		want := new(big.Int).Mul(new(big.Int).SetUint64(usableRao), new(big.Int).SetUint64(evmWeiPerRao))
		if err != nil || balance.Cmp(want) < 0 {
			return nil, fmt.Errorf("EVM funding postcondition balance=%v want>=%v: %w", balance, want, err)
		}
		state["address"], state["balance_wei"], state["minimum_wei"], state["existential_deposit_rao"] = addr.Hex(), balance.String(), want.String(), e.plan.LiveFacts.ExistentialDepositRao
		return state, nil
	case strings.HasPrefix(a.ID, "fleet.fund."):
		return e.verifySubstrateFunding(a, fleetColdkeyLabel(suffixInt(a.ID)), state)
	case strings.HasPrefix(a.ID, "fleet.fund-hotkey."):
		return e.verifySubstrateFunding(a, fleetHotkeyLabel(suffixInt(a.ID)), state)
	case strings.HasPrefix(a.ID, "churn.fund."):
		return e.verifySubstrateFunding(a, churnColdkeyLabel(suffixInt(a.ID)), state)
	case strings.HasPrefix(a.ID, "validator.fund."):
		return e.verifySubstrateFunding(a, fmt.Sprintf("validator-%d-coldkey", suffixInt(a.ID)), state)
	case strings.HasPrefix(a.ID, "fleet.register."):
		fleet := suffixInt(a.ID)
		if fleet > e.cfg.Config.Topology.HeadFleets {
			return e.verifyChallengerChurnPostcondition(fleet, state)
		}
		return e.verifyRegistration(fleetHotkeyLabel(fleet), fleetColdkeyLabel(fleet), common.Address{}, state)
	case strings.HasPrefix(a.ID, "churn.register."):
		churn := suffixInt(a.ID)
		challengerFleet := e.cfg.Config.Topology.HeadFleets + churn
		if churn <= e.cfg.Config.Topology.ChallengerFleets && e.actionVerified(fmt.Sprintf("fleet.register.%d", challengerFleet)) {
			return e.verifyPrunedChurnPostcondition(churn, state)
		}
		return e.verifyRegistration(churnHotkeyLabel(churn), churnColdkeyLabel(churn), common.Address{}, state)
	case strings.HasPrefix(a.ID, "validator.register."):
		validator := suffixInt(a.ID)
		return e.verifyRegistration(validatorHotkeyLabel(validator), fmt.Sprintf("validator-%d-coldkey", validator), common.Address{}, state)
	case strings.HasPrefix(a.ID, "operator.deposit.register."):
		operator := suffixInt(a.ID)
		signer, err := e.roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", operator))
		if err != nil {
			return nil, err
		}
		return e.verifyRegistration(fmt.Sprintf("operator-%d-deposit-hotkey", operator), "", signer, state)
	case a.ID == "validator.take-zero.1":
		hotkey, err := roleBytes32(e.roles, "reserve-hotkey")
		if err != nil {
			return nil, err
		}
		take, err := e.substrate.DelegateTake(hotkey)
		if err != nil || take != 0 {
			return nil, fmt.Errorf("reserve take=%d: %w", take, err)
		}
		return set("delegate_take", take)
	case strings.HasPrefix(a.ID, "alpha.transfer.operator-deposit."):
		return e.verifyAlphaTransfer(ctx, a, "operator-deposit", suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "alpha.transfer.validator."):
		return e.verifyAlphaTransfer(ctx, a, "validator", suffixInt(a.ID), state)
	case a.ID == "evm.reserve-sink" || a.ID == "evm.settlement-vault" || a.ID == "evm.coordinator-implementation" || a.ID == "evm.vault-register-escrow" || a.ID == "evm.coordinator-proxy" || a.ID == "evm.governance-drill-implementation" || a.ID == "evm.vault-fix-coordinator" || a.ID == "evm.sink-fix-recorder" || a.ID == "precompile.probe-deploy":
		return e.verifyDeploymentPostState(ctx, a, evmHead, state)
	case strings.HasPrefix(a.ID, "precompile."):
		return e.verifyPrecompileConformancePostState(ctx, a, evmHead, state)
	case strings.HasPrefix(a.ID, "governance."):
		return e.verifyGovernanceDrillPostState(ctx, a, state)
	case strings.HasPrefix(a.ID, "operator.register."):
		return e.verifyOperatorPostState(ctx, a, state)
	case strings.HasPrefix(a.ID, "fleet.commitment."):
		return e.verifyFleetCommitmentPostState(suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "fleet.mirror."):
		return e.verifyFleetMirrorPostState(ctx, suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "fleet.bind."):
		fleet, member, err := fleetBindingActionIndices(a.ID)
		if err != nil {
			return nil, err
		}
		return e.verifyFleetBindingPostState(ctx, fleet, member, state)
	case a.ID == "campaign.voluntary-conviction.1":
		return e.verifyVoluntaryConvictionPostState(ctx, evmHead, state)
	case a.ID == dishonestDepositActionID:
		return e.verifyDishonestDepositPostState(ctx, a, evmHead, state)
	case a.ID == "production.schedule-policy":
		return e.verifyProductionPolicyPostState(ctx, evmHead, state)
	case a.Kind == "budget-reserve":
		state["reserved"] = a.Spend
		state["approved_limits"] = e.plan.Limits
		return state, nil
	case a.ID == "config.render":
		state, err := e.verifyRenderedConfigs(state)
		if err != nil {
			return nil, err
		}
		count, err := e.verifyExactTopologyRoleSet(initialTopologyRoleLabels(e.cfg.Config.Topology))
		if err != nil {
			return nil, fmt.Errorf("pre-launch initial registration topology: %w", err)
		}
		state["initial_registered_roles"] = len(initialTopologyRoleLabels(e.cfg.Config.Topology))
		state["preserved_bootstrap_uids"] = len(e.plan.LiveFacts.ExistingUIDs)
		state["uid_count"] = count
		return state, nil
	case a.ID == "accounts.provision":
		for miner := 1; miner <= e.cfg.Config.Topology.Miners; miner++ {
			client := e.roles.Clients[fmt.Sprintf("miner-%d", miner)]
			if _, ok := evidenceFixedHex("0x"+client.ClientIDHex, 16); !ok {
				return nil, fmt.Errorf("miner-%d client id is not provisioned", miner)
			}
		}
		state["provisioned_miners"] = e.cfg.Config.Topology.Miners
		return state, nil
	case a.ID == "topology.launch":
		var supervisor SupervisorFile
		if err := readJSONFile(filepath.Join(e.stateDir, "supervisor.json"), &supervisor); err != nil {
			return nil, err
		}
		ready, err := supervisorReadyNow(e.stateDir, supervisor)
		if err != nil || !ready {
			return nil, fmt.Errorf("supervisor postcondition ready=%t: %w", ready, err)
		}
		state["supervisor_manifest_hash"], _ = canonicalHashHex(supervisor)
		state["processes"] = len(supervisor.Specs)
		return state, nil
	case a.ID == "churn.tournament-complete":
		return e.verifyChurnTournamentPostcondition(state)
	case strings.HasPrefix(a.ID, "operator.retire."):
		return e.verifyOperatorRetirementPostState(ctx, a, state)
	default:
		return nil, fmt.Errorf("no canonical postcondition verifier for %s", a.ID)
	}
}

func voluntaryConvictionEvidenceMatches(cfg *ResolvedConfig, plan *SetupPlan, evidence VoluntaryConvictionEvidence) error {
	if cfg == nil || plan == nil || len(plan.Roles.OperatorDepositSigners) == 0 {
		return errors.New("voluntary conviction approval context is incomplete")
	}
	amount := fmt.Sprint(cfg.Config.Scenarios.VoluntaryConvictionRao)
	if evidence.Schema != "urnetwork-voluntary-conviction-evidence-v1" || evidence.DeploymentID != cfg.Config.Deployment.DeploymentID || evidence.NoID != 1 ||
		evidence.AmountRao != amount || evidence.BeforeConvictionRao != "0" || evidence.AfterConvictionRao != amount ||
		!strings.EqualFold(evidence.Funder, plan.Roles.OperatorDepositSigners[0]) || !strings.EqualFold(evidence.PolicyHash, cfg.PolicyHash) {
		return errors.New("voluntary conviction evidence does not match the approved action")
	}
	if nonce, ok := new(big.Int).SetString(evidence.Nonce, 10); !ok || nonce.Sign() < 0 {
		return errors.New("voluntary conviction evidence has an invalid nonce")
	}
	if !validConformanceTransaction(evidence.TransactionHash, evidence.FinalizedHash, evidence.FinalizedBlock) {
		return errors.New("voluntary conviction evidence has an invalid transaction checkpoint")
	}
	return nil
}

func voluntaryConvictionReceiptMatches(receipt *ethTypes.Receipt, coordinator common.Address, evidence VoluntaryConvictionEvidence) error {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	event, ok := parsed.Events["ConvictionAdded"]
	if !ok {
		return errors.New("Coordinator ABI lacks ConvictionAdded")
	}
	for _, log := range receipt.Logs {
		if log.Address != coordinator || len(log.Topics) != 4 || log.Topics[0] != event.ID || log.Topics[1].Big().Cmp(big.NewInt(1)) != 0 ||
			!log.Topics[2].Big().IsUint64() || log.Topics[2].Big().Uint64() != evidence.Epoch || !strings.EqualFold(common.BytesToAddress(log.Topics[3].Bytes()[12:]).Hex(), evidence.Funder) {
			continue
		}
		values, unpackErr := event.Inputs.NonIndexed().Unpack(log.Data)
		if unpackErr != nil || len(values) != 3 {
			continue
		}
		amount, amountOK := values[0].(*big.Int)
		policyHash, policyOK := values[1].([32]byte)
		nonce, nonceOK := values[2].(*big.Int)
		if amountOK && policyOK && nonceOK && amount.String() == evidence.AmountRao &&
			strings.EqualFold("0x"+hex.EncodeToString(policyHash[:]), evidence.PolicyHash) && nonce.String() == evidence.Nonce {
			return nil
		}
	}
	return errors.New("finalized voluntary conviction receipt lacks the exact approved event")
}

func (e *Executor) verifyVoluntaryConvictionPostState(ctx context.Context, head ChainHead, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	var evidence VoluntaryConvictionEvidence
	if err := readJSONFile(filepath.Join(e.stateDir, "public", "voluntary-conviction.json"), &evidence); err != nil {
		return nil, err
	}
	if err := voluntaryConvictionEvidenceMatches(e.cfg, e.plan, evidence); err != nil {
		return nil, err
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, e.deployer.client, head, evidence.TransactionHash, evidence.FinalizedBlock, evidence.FinalizedHash)
	if err != nil {
		return nil, err
	}
	coordinatorAddress := e.payloads.Manifest.CoordinatorProxy
	if err := voluntaryConvictionReceiptMatches(receipt, coordinatorAddress, evidence); err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	epoch := new(big.Int).SetUint64(evidence.Epoch)
	added, err := rawCoordinatorCall(ctx, e.deployer, coordinatorAddress, coordinator.PackEpochConvictionAdded(epoch, big.NewInt(1)), coordinator.UnpackEpochConvictionAdded)
	if err != nil || added.String() != evidence.AmountRao {
		return nil, fmt.Errorf("finalized epoch conviction addition=%v, want %s: %w", added, evidence.AmountRao, err)
	}
	cumulative, err := rawCoordinatorCall(ctx, e.deployer, coordinatorAddress, coordinator.PackCumulativeConviction(big.NewInt(1)), coordinator.UnpackCumulativeConviction)
	want, _ := new(big.Int).SetString(evidence.AfterConvictionRao, 10)
	if err != nil || cumulative.Cmp(want) < 0 {
		return nil, fmt.Errorf("finalized cumulative conviction=%v, want at least %s: %w", cumulative, evidence.AfterConvictionRao, err)
	}
	state["epoch"], state["amount_rao"], state["transaction_hash"] = evidence.Epoch, evidence.AmountRao, evidence.TransactionHash
	state["cumulative_conviction_rao"] = cumulative.String()
	return state, nil
}

func productionPolicyEvidenceMatches(cfg *ResolvedConfig, evidence ProductionPolicyEvidence) bool {
	if cfg == nil {
		return false
	}
	p := cfg.Policy.ProductionCadence
	if p.AfterAcceleratedEpochs == ^uint64(0) {
		return false
	}
	return evidence.Schema == "urnetwork-production-policy-evidence-v1" && evidence.DeploymentID == cfg.Config.Deployment.DeploymentID &&
		strings.EqualFold(evidence.PolicyHash, cfg.PolicyHash) && evidence.ScheduledFromEpoch == p.AfterAcceleratedEpochs &&
		evidence.EffectiveEpoch == p.AfterAcceleratedEpochs+1 && evidence.EffectiveBlock != 0 &&
		evidence.PriorEpochBlocks == cfg.Policy.Settlement.EpochBlocks && evidence.EpochBlocks == p.EpochBlocks &&
		evidence.RootCommitWindowBlocks == p.RootCommitWindowBlocks && evidence.FinalizeOffsetBlocks == p.FinalizeOffsetBlocks &&
		evidence.CloseGraceBlocks == p.CloseGraceBlocks
}

func (e *Executor) verifyProductionPolicyPostState(ctx context.Context, head ChainHead, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	var evidence ProductionPolicyEvidence
	if err := readJSONFile(filepath.Join(e.stateDir, "public", "production-policy.json"), &evidence); err != nil {
		return nil, err
	}
	if !productionPolicyEvidenceMatches(e.cfg, evidence) {
		return nil, errors.New("production policy evidence does not match the approved cadence")
	}
	if evidence.TransactionHash != "" || evidence.FinalizedBlock != 0 || evidence.FinalizedBlockHash != "" {
		if _, err := verifyFinalizedEVMReceipt(ctx, e.owner.client, head, evidence.TransactionHash, evidence.FinalizedBlock, evidence.FinalizedBlockHash); err != nil {
			return nil, fmt.Errorf("production policy transaction: %w", err)
		}
	}
	coordinator := stabi.NewSTCoordinator()
	address := e.payloads.Manifest.CoordinatorProxy
	count, err := rawCoordinatorCall(ctx, e.owner, address, coordinator.PackPolicyCount(), coordinator.UnpackPolicyCount)
	if err != nil || count.Cmp(big.NewInt(2)) != 0 {
		return nil, fmt.Errorf("policy count=%v, want exactly 2: %w", count, err)
	}
	policy, err := rawCoordinatorCall(ctx, e.owner, address, coordinator.PackPolicyByIndex(big.NewInt(1)), coordinator.UnpackPolicyByIndex)
	if err != nil || !productionPolicyMatches(e.cfg, policy) || policy.EffectiveEpoch != evidence.EffectiveEpoch || policy.EffectiveBlock != evidence.EffectiveBlock {
		return nil, fmt.Errorf("finalized production policy does not match evidence: %w", err)
	}
	state["effective_epoch"], state["effective_block"], state["epoch_blocks"] = policy.EffectiveEpoch, policy.EffectiveBlock, policy.EpochBlocks
	state["policy_count"] = count.String()
	return state, nil
}

func retirementVersionMatches(version stabi.STCoordinatorOperatorVersion, effective uint64, hotkey [32]byte, depositSigner, rootSigner common.Address) bool {
	return !version.Active && version.EffectiveEpoch == effective && version.DepositHotkey == hotkey && version.DepositSigner == depositSigner && version.RootSigner == rootSigner
}

func (e *Executor) verifyOperatorRetirementPostState(ctx context.Context, action Action, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	noID, effective, hotkey, depositSigner, rootSigner, err := parseRetirementAction(action)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	version, err := rawCoordinatorCall(ctx, e.owner, e.payloads.Manifest.CoordinatorProxy,
		coordinator.PackOperatorAt(new(big.Int).SetUint64(noID), new(big.Int).SetUint64(effective)), coordinator.UnpackOperatorAt)
	if err != nil || !retirementVersionMatches(version, effective, hotkey, depositSigner, rootSigner) {
		return nil, fmt.Errorf("operator %d retirement at epoch %d does not match the approved finalized version: %w", noID, effective, err)
	}
	state["scheduled_operator"], state["effective_epoch"], state["active"] = noID, effective, false
	state["deposit_hotkey"], state["deposit_signer"], state["root_signer"] = "0x"+hex.EncodeToString(hotkey[:]), depositSigner.Hex(), rootSigner.Hex()
	return state, nil
}

func (e *Executor) verifySubstrateFunding(a Action, label string, state map[string]any) (map[string]any, error) {
	account, err := roleBytes32(e.roles, label)
	if err != nil {
		return nil, err
	}
	balance, err := e.substrate.FreeBalance(account)
	if err != nil || balance < a.Spend.TAORao {
		return nil, fmt.Errorf("%s balance=%d want>=%d: %w", label, balance, a.Spend.TAORao, err)
	}
	state["role"], state["account"], state["free_balance_rao"] = label, "0x"+hex.EncodeToString(account[:]), balance
	return state, nil
}

func (e *Executor) verifyRegistration(label, coldkeyLabel string, mirror common.Address, state map[string]any) (map[string]any, error) {
	hotkey, err := roleBytes32(e.roles, label)
	if err != nil {
		return nil, err
	}
	uid, found, err := e.substrate.UID(hotkey)
	if err != nil || !found {
		return nil, fmt.Errorf("%s native registration found=%t: %w", label, found, err)
	}
	var expected [32]byte
	if coldkeyLabel != "" {
		expected, err = roleBytes32(e.roles, coldkeyLabel)
	} else if mirror != (common.Address{}) {
		expected = ss58Mirror(mirror)
	} else {
		return nil, fmt.Errorf("%s registration has no expected coldkey", label)
	}
	owner, err := e.substrate.HotkeyOwner(hotkey)
	if err != nil {
		return nil, err
	}
	if err := validateHotkeyOwner(label, owner, expected); err != nil {
		return nil, err
	}
	state["role"], state["hotkey"], state["uid"] = label, "0x"+hex.EncodeToString(hotkey[:]), uid
	state["coldkey"] = "0x" + hex.EncodeToString(owner[:])
	return state, nil
}

func (e *Executor) verifyChallengerChurnPostcondition(fleet int, state map[string]any) (map[string]any, error) {
	result, err := e.readChallengerChurnState(fleet)
	if err != nil {
		return nil, err
	}
	if err := validateChallengerChurnPostState(result); err != nil {
		return nil, err
	}
	if _, err := e.verifyRegistration(fleetHotkeyLabel(fleet), fleetColdkeyLabel(fleet), common.Address{}, state); err != nil {
		return nil, err
	}
	state["replaced_churn"] = fleet - e.cfg.Config.Topology.HeadFleets
	state["replaced_uid"] = result.ExpectedUID
	state["uid_count"] = result.UIDCount
	return state, nil
}

func (e *Executor) verifyPrunedChurnPostcondition(churn int, state map[string]any) (map[string]any, error) {
	fleet := e.cfg.Config.Topology.HeadFleets + churn
	result, err := e.readChallengerChurnState(fleet)
	if err != nil {
		return nil, err
	}
	if err := validateChallengerChurnPostState(result); err != nil {
		return nil, err
	}
	state["role"] = churnHotkeyLabel(churn)
	state["status"] = "intentionally_pruned"
	state["replaced_by"] = fleetHotkeyLabel(fleet)
	state["uid"] = result.ExpectedUID
	return state, nil
}

func initialTopologyRoleLabels(topology TopologyConfig) []string {
	labels := make([]string, 0, 254)
	for churn := 1; churn <= topology.ChurnFloorUIDs; churn++ {
		labels = append(labels, churnHotkeyLabel(churn))
	}
	labels = append(labels, "escrow-hotkey")
	for operator := 1; operator <= topology.Operators; operator++ {
		labels = append(labels, fmt.Sprintf("operator-%d-deposit-hotkey", operator), fmt.Sprintf("operator-%d-pool-hotkey", operator))
	}
	for fleet := 1; fleet <= topology.HeadFleets; fleet++ {
		labels = append(labels, fleetHotkeyLabel(fleet))
	}
	for validator := 1; validator <= topology.Validators; validator++ {
		labels = append(labels, validatorHotkeyLabel(validator))
	}
	return labels
}

func tournamentTopologyRoleLabels(topology TopologyConfig) []string {
	labels := make([]string, 0, 254)
	for fleet := 1; fleet <= topology.fleetCandidates(); fleet++ {
		labels = append(labels, fleetHotkeyLabel(fleet))
	}
	for churn := topology.ChallengerFleets + 1; churn <= topology.ChurnFloorUIDs; churn++ {
		labels = append(labels, churnHotkeyLabel(churn))
	}
	labels = append(labels, "escrow-hotkey")
	for operator := 1; operator <= topology.Operators; operator++ {
		labels = append(labels, fmt.Sprintf("operator-%d-deposit-hotkey", operator), fmt.Sprintf("operator-%d-pool-hotkey", operator))
	}
	for validator := 1; validator <= topology.Validators; validator++ {
		labels = append(labels, validatorHotkeyLabel(validator))
	}
	return labels
}

func (e *Executor) verifyExactTopologyRoleSet(labels []string) (uint16, error) {
	uids := map[uint16]string{}
	for _, identity := range e.plan.LiveFacts.ExistingUIDs {
		hotkey, err := decodeHex32(fmt.Sprintf("bootstrap UID %d hotkey", identity.UID), identity.Hotkey)
		if err != nil {
			return 0, err
		}
		coldkey, err := decodeHex32(fmt.Sprintf("bootstrap UID %d coldkey", identity.UID), identity.Coldkey)
		if err != nil {
			return 0, err
		}
		uid, found, err := e.substrate.UID(hotkey)
		if err != nil || !found || uid != identity.UID {
			return 0, fmt.Errorf("bootstrap identity UID %d moved or disappeared: current=%d found=%t: %w", identity.UID, uid, found, err)
		}
		owner, err := e.substrate.HotkeyOwner(hotkey)
		if err != nil || owner != coldkey {
			return 0, fmt.Errorf("bootstrap identity UID %d coldkey changed: current=0x%x planned=0x%x: %w", identity.UID, owner, coldkey, err)
		}
		uids[uid] = fmt.Sprintf("bootstrap-uid-%d", identity.UID)
	}
	for _, label := range labels {
		hotkey, err := roleBytes32(e.roles, label)
		if err != nil {
			return 0, err
		}
		uid, found, err := e.substrate.UID(hotkey)
		if err != nil || !found {
			return 0, fmt.Errorf("planned identity %s is not live: %w", label, err)
		}
		if prior := uids[uid]; prior != "" {
			return 0, fmt.Errorf("planned identities %s and %s share UID %d", prior, label, uid)
		}
		uids[uid] = label
	}
	count, err := e.substrate.UIDCount()
	if err != nil {
		return 0, err
	}
	wantCount := hyperparameterUint64(e.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"])
	liveMaximum, err := e.substrate.ReadHyper("max_allowed_uids")
	if err != nil {
		return 0, err
	}
	if got := hyperparameterUint64(liveMaximum); got != wantCount {
		return 0, fmt.Errorf("live max_allowed_uids=%d, want approved value %d", got, wantCount)
	}
	if uint64(count) != wantCount || len(labels)+len(e.plan.LiveFacts.ExistingUIDs) != int(count) || len(uids) != int(count) {
		return 0, fmt.Errorf("exact topology controlled=%d bootstrap=%d unique=%d UID count=%d maximum=%d", len(labels), len(e.plan.LiveFacts.ExistingUIDs), len(uids), count, wantCount)
	}
	return count, nil
}

func (e *Executor) verifyChurnTournamentPostcondition(state map[string]any) (map[string]any, error) {
	for challenger := 1; challenger <= e.cfg.Config.Topology.ChallengerFleets; challenger++ {
		fleet := e.cfg.Config.Topology.HeadFleets + challenger
		result, err := e.readChallengerChurnState(fleet)
		if err != nil {
			return nil, err
		}
		if err := validateChallengerChurnPostState(result); err != nil {
			return nil, fmt.Errorf("challenger %d: %w", challenger, err)
		}
	}
	labels := tournamentTopologyRoleLabels(e.cfg.Config.Topology)
	count, err := e.verifyExactTopologyRoleSet(labels)
	if err != nil {
		return nil, err
	}
	state["candidate_fleets"] = e.cfg.Config.Topology.fleetCandidates()
	state["selected_slots"] = e.cfg.Config.Topology.HeadSlots
	state["pruned_churn_floor"] = e.cfg.Config.Topology.ChallengerFleets
	state["remaining_churn_floor"] = e.cfg.Config.Topology.ChurnFloorUIDs - e.cfg.Config.Topology.ChallengerFleets
	state["planned_live_identities"] = len(labels)
	state["preserved_bootstrap_uids"] = len(e.plan.LiveFacts.ExistingUIDs)
	state["uid_count"] = count
	return state, nil
}

func (e *Executor) verifyAlphaTransfer(ctx context.Context, a Action, kind string, index int, state map[string]any) (map[string]any, error) {
	var deployment *ContractDeployment
	if kind == "operator-deposit" {
		if err := e.ensurePayloads(ctx); err != nil {
			return nil, err
		}
		deployment = &e.payloads.Manifest
	}
	coldkey, hotkey, err := alphaTransferDestination(e.roles, deployment, kind, index)
	if err != nil {
		return nil, err
	}
	stake, err := e.readStakeFinalized(ctx, hotkey, coldkey)
	if err != nil || stake < a.Spend.AlphaRao {
		return nil, fmt.Errorf("%s %d stake=%d want>=%d: %w", kind, index, stake, a.Spend.AlphaRao, err)
	}
	state["stake_rao"], state["minimum_rao"] = stake, a.Spend.AlphaRao
	return state, nil
}

func (e *Executor) verifyDeploymentPostState(ctx context.Context, a Action, head ChainHead, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	p := e.payloads
	addresses := map[string]common.Address{"evm.reserve-sink": p.Manifest.ReserveSink, "evm.settlement-vault": p.Manifest.SettlementVault, "evm.coordinator-implementation": p.Manifest.CoordinatorImplementation, "evm.coordinator-proxy": p.Manifest.CoordinatorProxy, "evm.governance-drill-implementation": p.Manifest.GovernanceDrillImplementation, "precompile.probe-deploy": p.Manifest.PrecompileProbe}
	if address, ok := addresses[a.ID]; ok {
		code, err := e.deployer.client.CodeAt(ctx, address, new(big.Int).SetUint64(head.Number))
		if err != nil || string(code) != string(p.ExpectedRuntime[address]) {
			return nil, fmt.Errorf("runtime code mismatch at %s: %w", address, err)
		}
		state["address"], state["runtime_hash"] = address.Hex(), cryptoKeccak(code)
		return state, nil
	}
	if a.ID == "evm.vault-fix-coordinator" {
		contract := stabi.NewSTSettlementVault()
		got, err := rawCoordinatorCall(ctx, e.deployer, p.Manifest.SettlementVault, contract.PackCoordinator(), contract.UnpackCoordinator)
		if err != nil || got != p.Manifest.CoordinatorProxy {
			return nil, fmt.Errorf("vault coordinator=%s: %w", got, err)
		}
		state["vault"], state["coordinator"] = p.Manifest.SettlementVault.Hex(), got.Hex()
		return state, nil
	}
	if a.ID == "evm.vault-register-escrow" {
		contract := stabi.NewSTSettlementVault()
		registered, err := rawCoordinatorCall(ctx, e.deployer, p.Manifest.SettlementVault, contract.PackEscrowRegistered(), contract.UnpackEscrowRegistered)
		if err != nil || !registered {
			return nil, fmt.Errorf("vault escrow registration=%t: %w", registered, err)
		}
		hotkey, err := roleBytes32(e.roles, "escrow-hotkey")
		if err != nil {
			return nil, err
		}
		liveUID, found, err := e.substrate.UID(hotkey)
		if err != nil || !found {
			return nil, fmt.Errorf("vault escrow live UID=%d found=%t: %w", liveUID, found, err)
		}
		owner, err := e.substrate.HotkeyOwner(hotkey)
		if err != nil {
			return nil, err
		}
		if err := validateHotkeyOwner("vault escrow hotkey", owner, ss58Mirror(p.Manifest.SettlementVault)); err != nil {
			return nil, err
		}
		if err := e.verifyRegistrationBalances(ctx, a, p.Manifest.SettlementVault); err != nil {
			return nil, err
		}
		state["vault"], state["escrow_hotkey"], state["uid"] = p.Manifest.SettlementVault.Hex(), "0x"+hex.EncodeToString(hotkey[:]), liveUID
		state["coldkey"] = "0x" + hex.EncodeToString(owner[:])
		state["liquid_registration_balance_preserved"] = true
		return state, nil
	}
	contract := stabi.NewSTReserveSink()
	got, err := rawCoordinatorCall(ctx, e.deployer, p.Manifest.ReserveSink, contract.PackRecorder(), contract.UnpackRecorder)
	if err != nil || got != p.Manifest.CoordinatorProxy {
		return nil, fmt.Errorf("reserve recorder=%s: %w", got, err)
	}
	state["reserve_sink"], state["recorder"] = p.Manifest.ReserveSink.Hex(), got.Hex()
	return state, nil
}

func cryptoKeccak(value []byte) string {
	return common.BytesToHash(cryptoKeccakBytes(value)).Hex()
}

func cryptoKeccakBytes(value []byte) []byte {
	// Keccak256 is already used for release artifact identities; keep the
	// conversion isolated so postcondition JSON contains a stable 0x hash.
	return crypto.Keccak256(value)
}

func finalizedActionBlock(entries []JournalEntry, planHash string, action Action) (uint64, error) {
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.PlanHash == planHash && entry.ActionID == action.ID && entry.IntentHash == action.IntentHash && entry.Stage == StageFinalized {
			if entry.BlockNumber == 0 {
				break
			}
			return entry.BlockNumber, nil
		}
	}
	return 0, fmt.Errorf("action %s has no finalized transaction block", action.ID)
}

// Contract registration must consume only the runtime burn. The full ceiling
// is supplied to eliminate an in-flight price race, so every involved
// contract's liquid balance must be unchanged across the finalized block.
func (e *Executor) verifyRegistrationBalances(ctx context.Context, action Action, addresses ...common.Address) error {
	block, err := finalizedActionBlock(e.journal.Entries(), e.plan.PlanHash, action)
	if err != nil || block == 0 {
		return err
	}
	for _, address := range addresses {
		before, readErr := e.deployer.client.BalanceAt(ctx, address, new(big.Int).SetUint64(block-1))
		if readErr != nil {
			return fmt.Errorf("read %s registration pre-balance at %d: %w", address, block-1, readErr)
		}
		after, readErr := e.deployer.client.BalanceAt(ctx, address, new(big.Int).SetUint64(block))
		if readErr != nil {
			return fmt.Errorf("read %s registration post-balance at %d: %w", address, block, readErr)
		}
		if before.Cmp(after) != 0 {
			return fmt.Errorf("registration changed %s liquid balance from %s to %s", address, before, after)
		}
	}
	return nil
}

func (e *Executor) verifyOperatorPostState(ctx context.Context, action Action, state map[string]any) (map[string]any, error) {
	noID := suffixInt(action.ID)
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	address := e.payloads.Manifest.CoordinatorProxy
	epoch, err := rawCoordinatorCall(ctx, e.owner, address, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch)
	if err != nil {
		return nil, err
	}
	version, err := rawCoordinatorCall(ctx, e.owner, address, coordinator.PackOperatorAt(big.NewInt(int64(noID)), epoch), coordinator.UnpackOperatorAt)
	if err != nil {
		return nil, err
	}
	coldkey, _ := roleBytes32(e.roles, fmt.Sprintf("operator-%d-coldkey", noID))
	pool, _ := roleBytes32(e.roles, fmt.Sprintf("operator-%d-pool-hotkey", noID))
	deposit, _ := roleBytes32(e.roles, fmt.Sprintf("operator-%d-deposit-hotkey", noID))
	depositSigner, _ := e.roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", noID))
	rootSigner, _ := e.roles.EVMAddress(fmt.Sprintf("operator-%d-root", noID))
	if !version.Active || version.Coldkey != coldkey || version.PoolHotkey != pool || version.DepositHotkey != deposit || version.DepositSigner != depositSigner || version.RootSigner != rootSigner || version.EffectiveEpoch > epoch.Uint64() {
		return nil, fmt.Errorf("operator %d finalized registry postcondition mismatch", noID)
	}
	uid, found, err := e.substrate.UID(pool)
	if err != nil || !found {
		return nil, fmt.Errorf("operator %d pool UID missing: %w", noID, err)
	}
	owner, err := e.substrate.HotkeyOwner(pool)
	if err != nil {
		return nil, err
	}
	if err := validateHotkeyOwner(fmt.Sprintf("operator %d pool", noID), owner, ss58Mirror(e.payloads.Manifest.SettlementVault)); err != nil {
		return nil, err
	}
	if err := e.verifyRegistrationBalances(ctx, action, e.payloads.Manifest.CoordinatorProxy, e.payloads.Manifest.SettlementVault); err != nil {
		return nil, err
	}
	state["no_id"], state["effective_epoch"], state["pool_uid"], state["active"] = noID, version.EffectiveEpoch, uid, true
	state["pool_coldkey"] = "0x" + hex.EncodeToString(owner[:])
	state["liquid_registration_balances_preserved"] = true
	state["deposit_signer"], state["root_signer"] = depositSigner.Hex(), rootSigner.Hex()
	return state, nil
}

func (e *Executor) verifyFleetCommitmentPostState(fleet int, state map[string]any) (map[string]any, error) {
	evidence, err := loadFleetCommitmentEvidence(e.stateDir, fleet)
	if err != nil {
		return nil, err
	}
	hotkey, err := roleBytes32(e.roles, fleetHotkeyLabel(fleet))
	if err != nil {
		return nil, err
	}
	commitment, err := e.substrate.chain.FleetCommitmentFinalized(e.cfg.Netuid, hotkey)
	if err != nil {
		return nil, err
	}
	want, err := decodeHex32("fleet commitment", evidence.CommitmentHash)
	if err != nil || commitment.Hash != want || commitment.CommitmentBlock != evidence.CommitmentBlock {
		return nil, errors.New("native fleet commitment differs from its finalized evidence")
	}
	state["fleet"], state["commitment_hash"], state["commitment_block"] = fleet, evidence.CommitmentHash, commitment.CommitmentBlock
	return state, nil
}

func (e *Executor) verifyFleetMirrorPostState(ctx context.Context, fleet int, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	manifest, _, hash, err := fleetManifest(e.cfg, e.stateDir, e.roles, fleet)
	if err != nil {
		return nil, err
	}
	contract := stabi.NewSTCoordinator()
	got, err := rawCoordinatorCall(ctx, e.oracle, e.payloads.Manifest.CoordinatorProxy, contract.PackMirroredCommitments(manifest.Hotkey), contract.UnpackMirroredCommitments)
	if err != nil || got.CommitmentHash != hash || got.FinalizedBlock == 0 {
		return nil, fmt.Errorf("fleet %d mirror mismatch: %w", fleet, err)
	}
	state["fleet"], state["commitment_hash"], state["finalized_block"] = fleet, "0x"+hex.EncodeToString(hash[:]), got.FinalizedBlock
	return state, nil
}

func (e *Executor) verifyFleetBindingPostState(ctx context.Context, fleet, member int, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	var evidence FleetBindingEvidence
	if err := readJSONFile(filepath.Join(e.stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member)), &evidence); err != nil {
		return nil, err
	}
	clientIDBytes, ok := evidenceFixedHex(evidence.ClientID, 16)
	if !ok {
		return nil, errors.New("fleet binding evidence has invalid client id")
	}
	var clientID [16]byte
	copy(clientID[:], clientIDBytes)
	contract := stabi.NewSTCoordinator()
	got, err := rawCoordinatorCall(ctx, e.keeper, e.payloads.Manifest.CoordinatorProxy, contract.PackBindingAt(clientID, new(big.Int).SetUint64(evidence.ValidFromEpoch)), contract.UnpackBindingAt)
	if err != nil || !got.Active || got.Record.Uid != evidence.UID || got.Record.Generation != evidence.Generation {
		return nil, fmt.Errorf("fleet %d member %d binding postcondition mismatch: %w", fleet, member, err)
	}
	state["fleet"], state["member"], state["client_id"], state["uid"] = fleet, member, evidence.ClientID, evidence.UID
	return state, nil
}

func (e *Executor) verifyRenderedConfigs(state map[string]any) (map[string]any, error) {
	paths := []string{}
	for operator := 1; operator <= e.cfg.Config.Topology.Operators; operator++ {
		paths = append(paths, filepath.Join(e.stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", "st.yml"))
	}
	for validator := 1; validator <= e.cfg.Config.Topology.Validators; validator++ {
		paths = append(paths, filepath.Join(e.stateDir, "runtime", fmt.Sprintf("validator-%d", validator), "validator.yml"))
	}
	for miner := 1; miner <= e.cfg.Config.Topology.Miners; miner++ {
		paths = append(paths, filepath.Join(e.stateDir, "runtime", fmt.Sprintf("miner-%d", miner), "miner.yml"), filepath.Join(e.stateDir, "runtime", fmt.Sprintf("miner-%d", miner), "claim-daemon.yml"))
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("rendered config %s is absent or not private: %w", path, err)
		}
	}
	state["private_config_files"] = len(paths)
	return state, nil
}

func readJSONFile(path string, destination any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, destination); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
