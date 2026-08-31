package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
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
	EVMHashDomain                 string         `json:"evm_hash_domain,omitempty"`
	Observed                      map[string]any `json:"observed"`
	IndependentSubstrateFinalized ChainHead      `json:"independent_substrate_finalized"`
	IndependentEVMFinalized       ChainHead      `json:"independent_evm_finalized"`
	IndependentEVMHashDomain      string         `json:"independent_evm_hash_domain,omitempty"`
	IndependentObserved           map[string]any `json:"independent_observed"`
}

// stateMismatchError preserves an underlying read/decoding error when one is
// present and emits a plain semantic mismatch otherwise. Formatting a nil
// cause with %w produces a misleading "%!w(<nil>)" artifact in durable
// diagnostics, exactly where operators need an unambiguous failure reason.
func stateMismatchError(cause error, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if cause != nil {
		return fmt.Errorf("%s: %w", message, cause)
	}
	return errors.New(message)
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
	if a.Parameters["batch_installed"] == "true" && (strings.HasPrefix(a.ID, "fleet.mirror.") || strings.HasPrefix(a.ID, "fleet.bind.")) {
		return e.verifyFleetInstallAliasPostcondition(a)
	}
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
			Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
			PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash,
			OperationalRPCMode: e.cfg.OperationalRPCMode, IndependentRPC: false,
			SubstrateFinalized: operationalSubstrate, EVMFinalized: evmHead, EVMHashDomain: "evm-rpc", Observed: observed,
			IndependentSubstrateFinalized: operationalSubstrate, IndependentEVMFinalized: evmHead,
			IndependentEVMHashDomain: "evm-rpc", IndependentObserved: independentObserved,
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
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
		PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash,
		OperationalRPCMode: e.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(e.cfg),
		SubstrateFinalized: ChainHead{Number: finalizedNumber, Hash: finalizedHash.Hex()}, EVMFinalized: evmHead, EVMHashDomain: "evm-rpc",
		Observed: observed, IndependentSubstrateFinalized: independentSubstrate,
		IndependentEVMFinalized: independentEVM, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: independentObserved,
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
			if canonical, readErr := canonicalEVMBlockHash(waitCtx, ethEVMBlockReader{client: e.independentEVM}, operationalEVM.Number); readErr != nil {
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

func verifyEVMCheckpointFromReader(ctx context.Context, reader evmBlockReader, finalized, recorded ChainHead) error {
	if reader == nil {
		return errors.New("EVM checkpoint reader is unavailable")
	}
	if recorded.Number == 0 || recorded.Hash == "" {
		return errors.New("recorded EVM checkpoint identity is incomplete")
	}
	canonical, err := canonicalEVMBlockHash(ctx, reader, recorded.Number)
	if err != nil {
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

func verifyEVMCheckpoint(ctx context.Context, client *ethclient.Client, finalized, recorded ChainHead) error {
	if client == nil {
		return errors.New("EVM checkpoint client is unavailable")
	}
	return verifyEVMCheckpointFromReader(ctx, ethEVMBlockReader{client: client}, finalized, recorded)
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

// Decode the durable representation while retaining exact JSON numbers inside
// dynamic observation maps. A second value or an unknown field is never part
// of one authenticated receipt.
func decodeActionPostcondition(raw []byte) (*ActionPostcondition, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var record ActionPostcondition
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("action postcondition has a trailing JSON value")
		}
		return nil, err
	}
	return &record, nil
}

// Round-trip dynamic values before hashing so struct field order and decoded
// map order cannot give the on-disk receipt a different digest from memory.
func durableActionPostcondition(record *ActionPostcondition) (*ActionPostcondition, error) {
	if record == nil {
		return nil, errors.New("action postcondition is unavailable")
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return decodeActionPostcondition(encoded)
}

// V1-v3 receipts were hashed before serialization. Registration-balance
// slices therefore used struct field order on the operational observation,
// while the shared-provider clone used decoded map order. Retype only that
// historical shape so existing append-only journal evidence remains usable.
func legacyRegistrationBalancePostconditionHash(record *ActionPostcondition) (string, bool, error) {
	legacy, err := durableActionPostcondition(record)
	if err != nil {
		return "", false, err
	}
	retype := func(observed map[string]any) (bool, error) {
		value, ok := observed["registration_balances"]
		if !ok {
			return false, nil
		}
		encoded, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			return false, encodeErr
		}
		var balances []registrationBalanceObservation
		if decodeErr := json.Unmarshal(encoded, &balances); decodeErr != nil || len(balances) == 0 {
			if decodeErr == nil {
				decodeErr = errors.New("registration balance evidence is empty")
			}
			return false, decodeErr
		}
		observed["registration_balances"] = balances
		return true, nil
	}
	applied, err := retype(legacy.Observed)
	if err != nil || !applied {
		return "", applied, err
	}
	if legacy.IndependentRPC {
		if applied, err = retype(legacy.IndependentObserved); err != nil || !applied {
			return "", applied, err
		}
	}
	hash, err := canonicalHashHex(legacy)
	return hash, true, err
}

func (e *Executor) persistActionPostcondition(record *ActionPostcondition) (string, string, error) {
	if record == nil {
		return "", "", errors.New("action postcondition is unavailable")
	}
	relative, err := postconditionRelativePath(record.PlanHash, record.ActionID)
	if err != nil {
		return "", "", err
	}
	durable, err := durableActionPostcondition(record)
	if err != nil {
		return "", "", err
	}
	hash, err := canonicalHashHex(durable)
	if err != nil {
		return "", "", err
	}
	b, err := json.MarshalIndent(durable, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := atomicWrite(filepath.Join(e.stateDir, filepath.FromSlash(relative)), append(b, '\n'), 0o600); err != nil {
		return "", "", err
	}
	return relative, hash, nil
}

// readPersistedPostcondition authenticates the durable receipt named by one
// verified journal entry and returns the exact decoded evidence. Callers that
// need to replay a point-in-time assertion must use this value rather than
// resolving a receipt by action ID, because revised plans may contain the same
// action and intent while retaining distinct historical checkpoints.
func (e *Executor) readPersistedPostcondition(entry JournalEntry) (*ActionPostcondition, error) {
	if entry.PostconditionPath == "" {
		return nil, errors.New("verified journal entry has no postcondition path")
	}
	wantPath, err := postconditionRelativePath(entry.PlanHash, entry.ActionID)
	legacyPath, legacyErr := legacyPostconditionRelativePath(entry.ActionID)
	if (err != nil || entry.PostconditionPath != wantPath) && (legacyErr != nil || entry.PostconditionPath != legacyPath) {
		return nil, fmt.Errorf("postcondition path %q is not canonical", entry.PostconditionPath)
	}
	b, err := os.ReadFile(filepath.Join(e.stateDir, filepath.FromSlash(entry.PostconditionPath)))
	if err != nil {
		return nil, err
	}
	record, err := decodeActionPostcondition(b)
	if err != nil {
		return nil, err
	}
	if (record.Schema != "urnetwork-sim-action-postcondition-v1" && record.Schema != "urnetwork-sim-action-postcondition-v2" && record.Schema != "urnetwork-sim-action-postcondition-v3" && record.Schema != "urnetwork-sim-action-postcondition-v4") || record.DeploymentID != e.cfg.Config.Deployment.DeploymentID || record.PlanHash != entry.PlanHash || !e.plan.allowedPlanHashes()[entry.PlanHash] || record.ActionID != entry.ActionID || record.IntentHash != entry.IntentHash || record.OperationalRPCMode != e.cfg.OperationalRPCMode || record.IndependentRPC != independentRPCRequired(e.cfg) {
		return nil, errors.New("persisted action postcondition identity mismatch")
	}
	if record.Schema == "urnetwork-sim-action-postcondition-v2" && (record.EVMHashDomain != "ethereum" || record.IndependentEVMHashDomain != "ethereum") {
		return nil, errors.New("persisted legacy action postcondition has an invalid EVM hash domain")
	}
	if (record.Schema == "urnetwork-sim-action-postcondition-v3" || record.Schema == "urnetwork-sim-action-postcondition-v4") && (record.EVMHashDomain != "evm-rpc" || record.IndependentEVMHashDomain != "evm-rpc") {
		return nil, errors.New("persisted action postcondition has an invalid EVM hash domain")
	}
	if record.IndependentSubstrateFinalized.Number == 0 || record.IndependentSubstrateFinalized.Hash == "" || record.IndependentEVMFinalized.Number == 0 || record.IndependentEVMFinalized.Hash == "" || record.IndependentObserved == nil {
		return nil, errors.New("persisted action postcondition lacks independent RPC evidence")
	}
	got, err := canonicalHashHex(record)
	if err != nil {
		return nil, err
	}
	if got != entry.PostconditionHash {
		legacyHash, applicable, legacyErr := legacyRegistrationBalancePostconditionHash(record)
		if record.Schema == "urnetwork-sim-action-postcondition-v4" || legacyErr != nil || !applicable || legacyHash != entry.PostconditionHash {
			return nil, fmt.Errorf("persisted postcondition hash %s, journal requires %s", got, entry.PostconditionHash)
		}
	}
	return record, nil
}

func (e *Executor) verifyPersistedPostcondition(entry JournalEntry) error {
	_, err := e.readPersistedPostcondition(entry)
	return err
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
	if evmHead.Number != 0 || evmHead.Hash != "" {
		var err error
		ctx, err = withFinalizedEVMHead(ctx, evmHead)
		if err != nil {
			return nil, err
		}
	}
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
			return nil, stateMismatchError(err, "hyperparameter %s postcondition is %v", name, got)
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
			return nil, stateMismatchError(err, "production %s postcondition is %v", name, got)
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
		if err != nil {
			return nil, fmt.Errorf("EVM funding postcondition balance at block %d: %w", evmHead.Number, err)
		}
		if balance.Cmp(want) < 0 {
			return nil, fmt.Errorf("EVM funding postcondition balance=%v want>=%v at block %d", balance, want, evmHead.Number)
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
		if replacement, replaced := e.expectedReplacementForChurn(churn); replaced {
			return e.verifyPrunedChurnPostcondition(churn, replacement, state)
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
			return nil, stateMismatchError(err, "reserve take=%d", take)
		}
		return set("delegate_take", take)
	case strings.HasPrefix(a.ID, "alpha.transfer.operator-deposit."):
		return e.verifyAlphaTransfer(ctx, a, "operator-deposit", suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "alpha.transfer.validator."):
		return e.verifyAlphaTransfer(ctx, a, "validator", suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "alpha.repair."):
		kind, index, err := alphaTransferTargetFromActionID(a.ID)
		if err != nil {
			return nil, err
		}
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
		targetShareBPS, minimumShareBPS, reserveShareRepair, err := reserveShareRepairTerms(a)
		if err != nil {
			return nil, err
		}
		if reserveShareRepair {
			state, err = e.verifyAlphaTransfer(ctx, a, kind, index, state)
			if err != nil {
				return nil, err
			}
			block, err := e.finalizedRegistrationActionBlock(a)
			if err != nil {
				return nil, err
			}
			snapshot, err := e.substrate.RegisteredAlphaSnapshotAtBlock(block)
			if err != nil {
				return nil, err
			}
			reserveAlpha, registered := snapshot.ByHotkey[hotkey]
			if !registered || !alphaShareMeets(snapshot.TotalAlphaRao, reserveAlpha, targetShareBPS) {
				return nil, fmt.Errorf("alpha repair %s finalized reserve %d does not meet %d bps of registered alpha %d", a.ID, reserveAlpha, targetShareBPS, snapshot.TotalAlphaRao)
			}
			state["reserve_alpha_rao_at_transfer"] = reserveAlpha
			state["registered_alpha_rao_at_transfer"] = snapshot.TotalAlphaRao
			state["reserve_target_share_bps"] = targetShareBPS
			state["reserve_minimum_share_bps"] = minimumShareBPS
			state["reserve_snapshot_block"] = snapshot.FinalizedBlock
			state["reserve_snapshot_block_hash"] = snapshot.FinalizedHash
			state["cumulative_alpha_limit_rao"] = a.Parameters[alphaRepairCumulativeLimitParameter]
			return state, nil
		}
		minimum, err := e.recoveredAlphaMinimumStake(ctx, a, hotkey, coldkey)
		if err != nil {
			return nil, err
		}
		stake, err := e.readStakeFinalized(ctx, hotkey, coldkey)
		if err != nil {
			return nil, err
		}
		planHash, intentHash := e.plan.PlanHash, a.IntentHash
		if verified, ok := e.verifiedActionEntry(a); ok {
			planHash, intentHash = verified.PlanHash, verified.IntentHash
		}
		_, hasTransaction := e.journal.LatestTransaction(planHash, a.ID, intentHash)
		verifyTransfer, dispositionErr := alphaRepairPostconditionRequiresTransaction(stake, minimum, hasTransaction)
		if dispositionErr != nil {
			return nil, fmt.Errorf("alpha repair %s: %w", a.ID, dispositionErr)
		}
		if verifyTransfer {
			state, err = e.verifyAlphaTransfer(ctx, a, kind, index, state)
			if err != nil {
				return nil, err
			}
		} else {
			state["converged_without_transfer"] = true
			state["stake_rao"] = stake
		}
		if stake < minimum {
			return nil, fmt.Errorf("alpha repair %s stake=%d want>=%d", a.ID, stake, minimum)
		}
		state["minimum_repaired_stake_rao"] = minimum
		return state, nil
	case a.ID == "validator.reserve-majority":
		snapshot, reserveAlpha, err := e.reserveValidatorMajoritySnapshot()
		if err != nil {
			return nil, err
		}
		state["reserve_alpha_rao"] = reserveAlpha
		state["registered_alpha_rao"] = snapshot.TotalAlphaRao
		state["minimum_share_bps"] = e.cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS
		state["finalized_block"] = snapshot.FinalizedBlock
		state["finalized_block_hash"] = snapshot.FinalizedHash
		return state, nil
	case a.ID == "evm.reserve-sink" || a.ID == "evm.settlement-vault" || a.ID == "evm.coordinator-implementation" || a.ID == "evm.vault-register-escrow" || a.ID == "evm.coordinator-proxy" || a.ID == "evm.governance-drill-implementation" || a.ID == "evm.vault-fix-coordinator" || a.ID == "evm.sink-fix-recorder" || a.ID == "precompile.probe-deploy" || a.ID == "evm.coordinator-upgrade-implementation" || a.ID == "fleet.refresh.deploy-batcher":
		return e.verifyDeploymentPostState(ctx, a, evmHead, state)
	case a.ID == "fleet.refresh.oracle-activate" || a.ID == "fleet.refresh.oracle-await-active" || a.ID == "fleet.refresh.oracle-restore" || a.ID == "fleet.refresh.oracle-await-restored":
		return e.verifyFleetRefreshOraclePostState(ctx, a, evmHead, state)
	case strings.HasPrefix(a.ID, "fleet.refresh.commitment."):
		return e.verifyFleetRefreshCommitmentPostState(a, suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "fleet.refresh.batch."):
		return e.verifyFleetRefreshBatchPostState(ctx, a, evmHead, state)
	case strings.HasPrefix(a.ID, "fleet.install.batch."):
		return e.verifyFleetInstallBatchPostState(ctx, a, evmHead, state)
	case a.ID == "evm.coordinator-upgrade-activate":
		if err := e.ensurePayloads(ctx); err != nil {
			return nil, err
		}
		implementation, err := implementationAt(ctx, e.owner, e.payloads.Manifest.CoordinatorProxy, evmHead.Number)
		if err != nil || implementation != e.payloads.CoordinatorUpgrade.Implementation {
			return nil, stateMismatchError(err, "coordinator implementation=%s", implementation)
		}
		state["proxy"] = e.payloads.Manifest.CoordinatorProxy.Hex()
		state["implementation"] = implementation.Hex()
		state["runtime_hash"] = e.payloads.CoordinatorUpgrade.RuntimeCodeHash
		return state, nil
	case a.ID == "policy.schedule-bootstrap" || a.ID == "policy.await-bootstrap":
		current, count, active, err := e.bootstrapPolicyState(ctx, evmHead.Number)
		if err != nil {
			return nil, err
		}
		matched := bootstrapPolicyMatches(e.cfg, active)
		if !matched && a.ID == "policy.schedule-bootstrap" && count > 1 {
			parsed, parseErr := abi.JSON(strings.NewReader(CoordinatorABI))
			if parseErr != nil {
				return nil, parseErr
			}
			values, readErr := contractCallAt(ctx, e.owner.client, e.payloads.Manifest.CoordinatorProxy, parsed, "policyByIndex", evmHead.Number, new(big.Int).SetUint64(count-1))
			if readErr != nil {
				return nil, readErr
			}
			scheduled, convertErr := coordinatorPolicy(values)
			matched = convertErr == nil && scheduled.EffectiveEpoch > current && bootstrapPolicyMatches(e.cfg, scheduled)
		}
		if !matched {
			return nil, errors.New("locked bootstrap policy is neither active nor canonically scheduled")
		}
		state["current_epoch"] = current
		state["policy_count"] = count
		state["policy_hash"] = e.cfg.PolicyHash
		state["active"] = bootstrapPolicyMatches(e.cfg, active)
		return state, nil
	case strings.HasPrefix(a.ID, "precompile."):
		return e.verifyPrecompileConformancePostState(ctx, a, evmHead, state)
	case strings.HasPrefix(a.ID, "governance."):
		return e.verifyGovernanceDrillPostState(ctx, a, state)
	case strings.HasPrefix(a.ID, "operator.register."):
		return e.verifyOperatorPostState(ctx, a, state)
	case strings.HasPrefix(a.ID, "fleet.commitment."):
		return e.verifyFleetCommitmentPostState(a, suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "fleet.mirror."):
		if a.Parameters["batch_installed"] == "true" {
			_, _, observed, err := e.verifyFleetInstallAliasState(a, state)
			return observed, err
		}
		return e.verifyFleetMirrorPostState(ctx, suffixInt(a.ID), state)
	case strings.HasPrefix(a.ID, "fleet.bind."):
		if a.Parameters["batch_installed"] == "true" {
			_, _, observed, err := e.verifyFleetInstallAliasState(a, state)
			return observed, err
		}
		fleet, member, err := fleetBindingActionIndices(a.ID)
		if err != nil {
			return nil, err
		}
		return e.verifyFleetBindingPostState(ctx, fleet, member, state)
	case a.ID == "campaign.voluntary-conviction.1":
		return e.verifyVoluntaryConvictionPostState(ctx, evmHead, state)
	case a.ID == voluntaryConvictionReconciliationActionID:
		return e.verifyVoluntaryConvictionReconciliationPostState(ctx, a, evmHead, state)
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
		labels := initialTopologyRoleLabels(e.cfg.Config.Topology, e.plan.Deployment.RegistrationRoleGeneration)
		count, err := e.verifyExactTopologyRoleSet(labels)
		if err != nil {
			return nil, fmt.Errorf("pre-launch initial registration topology: %w", err)
		}
		state["initial_registered_roles"] = len(labels)
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
			return nil, stateMismatchError(err, "supervisor postcondition ready=%t", ready)
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
		return nil, stateMismatchError(err, "finalized epoch conviction addition=%v, want %s", added, evidence.AmountRao)
	}
	cumulative, err := rawCoordinatorCall(ctx, e.deployer, coordinatorAddress, coordinator.PackCumulativeConviction(big.NewInt(1)), coordinator.UnpackCumulativeConviction)
	want, _ := new(big.Int).SetString(evidence.AfterConvictionRao, 10)
	if err != nil || cumulative.Cmp(want) < 0 {
		return nil, stateMismatchError(err, "finalized cumulative conviction=%v, want at least %s", cumulative, evidence.AfterConvictionRao)
	}
	state["epoch"], state["amount_rao"], state["transaction_hash"] = evidence.Epoch, evidence.AmountRao, evidence.TransactionHash
	state["cumulative_conviction_rao"] = cumulative.String()
	return state, nil
}

func productionPolicyEvidenceMatches(cfg *ResolvedConfig, evidence ProductionPolicyEvidence, gate *ReleaseCampaignGate) bool {
	if cfg == nil || gate == nil {
		return false
	}
	p := cfg.Policy.ProductionCadence
	if evidence.ScheduledFromEpoch == ^uint64(0) {
		return false
	}
	return evidence.Schema == "urnetwork-production-policy-evidence-v2" && evidence.DeploymentID == cfg.Config.Deployment.DeploymentID &&
		strings.EqualFold(evidence.PolicyHash, cfg.PolicyHash) && evidence.ReleaseRunID == gate.RunID &&
		strings.EqualFold(evidence.ReleaseResultHash, gate.ResultHash) && strings.EqualFold(evidence.ReleaseCompleteHash, gate.CompleteContentHash) &&
		evidence.CampaignStartEpoch == gate.StartEpoch && evidence.CampaignEndEpoch == gate.EndEpoch && evidence.ScheduledFromEpoch >= gate.EndEpoch &&
		evidence.EffectiveEpoch == evidence.ScheduledFromEpoch+1 && evidence.EffectiveBlock != 0 &&
		evidence.PriorEpochBlocks == cfg.Policy.Settlement.EpochBlocks && evidence.EpochBlocks == p.EpochBlocks &&
		evidence.RootCommitWindowBlocks == p.RootCommitWindowBlocks && evidence.FinalizeOffsetBlocks == p.FinalizeOffsetBlocks &&
		evidence.CloseGraceBlocks == p.CloseGraceBlocks && validConformanceTransaction(evidence.TransactionHash, evidence.FinalizedBlockHash, evidence.FinalizedBlock)
}

func (e *Executor) verifyProductionPolicyPostState(ctx context.Context, head ChainHead, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	var evidence ProductionPolicyEvidence
	if err := readJSONFile(filepath.Join(e.stateDir, "public", "production-policy.json"), &evidence); err != nil {
		return nil, err
	}
	gate, err := loadReleaseCampaignGate(e.cfg, e.stateDir, e.roles)
	if err != nil {
		return nil, fmt.Errorf("production release gate: %w", err)
	}
	if !productionPolicyEvidenceMatches(e.cfg, evidence, gate) {
		return nil, errors.New("production policy evidence does not match the approved cadence")
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, e.owner.client, head, evidence.TransactionHash, evidence.FinalizedBlock, evidence.FinalizedBlockHash)
	if err != nil {
		return nil, fmt.Errorf("production policy transaction: %w", err)
	}
	coordinator := stabi.NewSTCoordinator()
	address := e.payloads.Manifest.CoordinatorProxy
	count, err := rawCoordinatorCall(ctx, e.owner, address, coordinator.PackPolicyCount(), coordinator.UnpackPolicyCount)
	if err != nil || !count.IsUint64() || count.Uint64() < 2 || count.Uint64() > 3 {
		return nil, stateMismatchError(err, "policy count=%v, want fresh or migrated release history", count)
	}
	lastIndex := new(big.Int).Sub(new(big.Int).Set(count), big.NewInt(1))
	policy, err := rawCoordinatorCall(ctx, e.owner, address, coordinator.PackPolicyByIndex(lastIndex), coordinator.UnpackPolicyByIndex)
	if err != nil || !productionPolicyMatches(e.cfg, policy) || policy.EffectiveEpoch != evidence.EffectiveEpoch || policy.EffectiveBlock != evidence.EffectiveBlock {
		return nil, stateMismatchError(err, "finalized production policy does not match evidence")
	}
	if err := productionPolicyReceiptMatches(receipt, address, policy, count.Uint64()-1); err != nil {
		return nil, err
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
		return nil, stateMismatchError(err, "operator %d retirement at epoch %d does not match the approved finalized version", noID, effective)
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
		return nil, stateMismatchError(err, "%s balance=%d want>=%d", label, balance, a.Spend.TAORao)
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
		return nil, stateMismatchError(err, "%s native registration found=%t", label, found)
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
	challenger := fleet - e.cfg.Config.Topology.HeadFleets
	churn, err := churnIndexForChallenger(e.cfg.Config.Topology, e.plan.Deployment.RegistrationRoleGeneration, challenger)
	if err != nil {
		return nil, err
	}
	state["replaced_churn"] = churn
	state["replaced_uid"] = result.ExpectedUID
	state["uid_count"] = result.UIDCount
	return state, nil
}

func (e *Executor) verifyPrunedChurnPostcondition(churn int, replacementLabel string, state map[string]any) (map[string]any, error) {
	result, err := e.readRegistrationReplacementState(churn, replacementLabel)
	if err != nil {
		return nil, err
	}
	if err := validateRegistrationReplacementPostState(result); err != nil {
		return nil, err
	}
	state["role"] = churnHotkeyLabel(churn)
	state["status"] = "intentionally_pruned"
	state["replaced_by"] = replacementLabel
	state["uid"] = result.ExpectedUID
	return state, nil
}

func (e *Executor) verifyContractRegistrationReplacementPostcondition(action Action, replacementLabel string, registration int, state map[string]any) error {
	generation := e.plan.Deployment.RegistrationRoleGeneration
	if generation == 0 {
		if action.Parameters["registration_role_generation"] != "0" || action.Parameters["expected_replaced_churn"] != "" {
			return errors.New("generation-zero contract registration has replacement parameters")
		}
		return nil
	}
	churn, err := churnIndexForContractRegistration(e.cfg.Config.Topology, generation, registration)
	if err != nil {
		return err
	}
	if action.Parameters["registration_role_generation"] != strconv.FormatUint(generation, 10) || action.Parameters["expected_replaced_churn"] != strconv.Itoa(churn) {
		return errors.New("contract registration postcondition differs from the approved generation and churn UID")
	}
	result, err := e.readRegistrationReplacementState(churn, replacementLabel)
	if err != nil {
		return err
	}
	if err := validateRegistrationReplacementPostState(result); err != nil {
		return err
	}
	state["registration_role_generation"] = generation
	state["replaced_churn"] = churn
	state["replaced_uid"] = result.ExpectedUID
	return nil
}

func (e *Executor) expectedReplacementForChurn(churn int) (string, bool) {
	generation := e.plan.Deployment.RegistrationRoleGeneration
	count := contractRegistrationRoleCount(e.cfg.Config.Topology)
	for completed := uint64(1); completed < generation; completed++ {
		offset := int(completed-1) * count
		if churn > offset && churn <= offset+count {
			return contractRegistrationRoleLabels(e.cfg.Config.Topology, completed)[churn-offset-1], true
		}
	}
	if generation > 0 {
		offset := int(generation-1) * count
		if churn > offset && churn <= offset+count {
			ordinal := churn - offset
			actionID := "evm.vault-register-escrow"
			if ordinal > 1 {
				actionID = fmt.Sprintf("operator.register.%d", ordinal-1)
			}
			if e.actionVerified(actionID) {
				return contractRegistrationRoleLabels(e.cfg.Config.Topology, generation)[ordinal-1], true
			}
		}
	}
	challengerOffset := int(generation) * count
	if churn > challengerOffset && churn <= challengerOffset+e.cfg.Config.Topology.ChallengerFleets {
		challenger := churn - challengerOffset
		fleet := e.cfg.Config.Topology.HeadFleets + challenger
		if e.actionVerified(fmt.Sprintf("fleet.register.%d", fleet)) {
			return fleetHotkeyLabel(fleet), true
		}
	}
	return "", false
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
			return 0, stateMismatchError(err, "bootstrap identity UID %d moved or disappeared: current=%d found=%t", identity.UID, uid, found)
		}
		owner, err := e.substrate.HotkeyOwner(hotkey)
		if err != nil || owner != coldkey {
			return 0, stateMismatchError(err, "bootstrap identity UID %d coldkey changed: current=0x%x planned=0x%x", identity.UID, owner, coldkey)
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
			return 0, stateMismatchError(err, "planned identity %s is not live", label)
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
	labels := tournamentTopologyRoleLabels(e.cfg.Config.Topology, e.plan.Deployment.RegistrationRoleGeneration)
	count, err := e.verifyExactTopologyRoleSet(labels)
	if err != nil {
		return nil, err
	}
	state["candidate_fleets"] = e.cfg.Config.Topology.fleetCandidates()
	state["selected_slots"] = e.cfg.Config.Topology.HeadSlots
	state["pruned_churn_floor"] = e.cfg.Config.Topology.ChallengerFleets
	contractReplacements := int(e.plan.Deployment.RegistrationRoleGeneration) * contractRegistrationRoleCount(e.cfg.Config.Topology)
	state["contract_generation_replacements"] = contractReplacements
	state["remaining_churn_floor"] = e.cfg.Config.Topology.ChurnFloorUIDs - contractReplacements - e.cfg.Config.Topology.ChallengerFleets
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
	block, err := e.finalizedRegistrationActionBlock(a)
	if err != nil {
		return nil, err
	}
	before, after, credited, err := e.verifyAlphaTransferDeltaAtBlock(ctx, a, hotkey, coldkey, block)
	if err != nil {
		return nil, err
	}
	stake, err := e.readStakeFinalized(ctx, hotkey, coldkey)
	if err != nil || stake < after {
		return nil, stateMismatchError(err, "%s %d current stake=%d want>=%d finalized transfer stake", kind, index, stake, after)
	}
	shortfall, _ := alphaTransferRoundingShortfall(a)
	state["stake_rao"], state["exact_transfer_rao"] = stake, a.Spend.AlphaRao
	state["transfer_parent_stake_rao"], state["transfer_finalized_stake_rao"] = before, after
	state["credited_transfer_rao"], state["maximum_destination_rounding_shortfall_rao"] = credited, shortfall
	state["transfer_block"] = block
	if a.Parameters["runtime_default_min_transfer_tao_rao"] != "" {
		state["runtime_default_min_transfer_tao_rao"] = a.Parameters["runtime_default_min_transfer_tao_rao"]
	} else {
		state["runtime_initial_min_stake_tao_rao"] = a.Parameters["runtime_initial_min_stake_tao_rao"]
	}
	state["approved_alpha_price_q9"] = a.Parameters["approved_alpha_price_q9"]
	state["minimum_tao_equivalent_margin_bps"] = a.Parameters["minimum_tao_equivalent_margin_bps"]
	return state, nil
}

func (e *Executor) verifyDeploymentPostState(ctx context.Context, a Action, head ChainHead, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	p := e.payloads
	addresses := map[string]common.Address{"evm.reserve-sink": p.Manifest.ReserveSink, "evm.settlement-vault": p.Manifest.SettlementVault, "evm.coordinator-implementation": p.Manifest.CoordinatorImplementation, "evm.coordinator-proxy": p.Manifest.CoordinatorProxy, "evm.governance-drill-implementation": p.Manifest.GovernanceDrillImplementation, "precompile.probe-deploy": p.Manifest.PrecompileProbe, "evm.coordinator-upgrade-implementation": p.CoordinatorUpgrade.Implementation, "fleet.refresh.deploy-batcher": p.FleetBatcherAddress}
	if address, ok := addresses[a.ID]; ok {
		code, err := e.deployer.client.CodeAt(ctx, address, new(big.Int).SetUint64(head.Number))
		expected := p.ExpectedRuntime[address]
		if a.ID == "fleet.refresh.deploy-batcher" {
			expected = p.FleetBatcherRuntime
		}
		if err != nil || len(expected) > 0 && string(code) != string(expected) {
			return nil, stateMismatchError(err, "runtime code mismatch at %s", address)
		}
		wantHash := p.Manifest.RuntimeHashes[address.Hex()]
		if address == p.CoordinatorUpgrade.Implementation {
			wantHash = p.CoordinatorUpgrade.RuntimeCodeHash
		}
		if address == p.FleetBatcherAddress {
			wantHash = cryptoKeccak(p.FleetBatcherRuntime)
		}
		if len(expected) == 0 && (wantHash == "" || !strings.EqualFold(cryptoKeccak(code), wantHash)) {
			return nil, fmt.Errorf("runtime hash mismatch at %s", address)
		}
		state["address"], state["runtime_hash"] = address.Hex(), cryptoKeccak(code)
		if a.ID == "evm.settlement-vault" {
			contract := stabi.NewSTSettlementVault()
			minimum, callErr := rawCoordinatorCall(ctx, e.deployer, address, contract.PackMinimumTransferTaoRao(), contract.UnpackMinimumTransferTaoRao)
			if callErr != nil || minimum != e.cfg.Public.Chain.ExpectedDefaultMinTransferRao {
				return nil, stateMismatchError(callErr, "vault minimumTransferTaoRao=%d want=%d", minimum, e.cfg.Public.Chain.ExpectedDefaultMinTransferRao)
			}
			state["minimum_transfer_tao_rao"] = minimum
		}
		return state, nil
	}
	if a.ID == "evm.vault-fix-coordinator" {
		contract := stabi.NewSTSettlementVault()
		got, err := rawCoordinatorCall(ctx, e.deployer, p.Manifest.SettlementVault, contract.PackCoordinator(), contract.UnpackCoordinator)
		if err != nil || got != p.Manifest.CoordinatorProxy {
			return nil, stateMismatchError(err, "vault coordinator=%s", got)
		}
		state["vault"], state["coordinator"] = p.Manifest.SettlementVault.Hex(), got.Hex()
		return state, nil
	}
	if a.ID == "evm.vault-register-escrow" {
		contract := stabi.NewSTSettlementVault()
		registered, err := rawCoordinatorCall(ctx, e.deployer, p.Manifest.SettlementVault, contract.PackEscrowRegistered(), contract.UnpackEscrowRegistered)
		if err != nil || !registered {
			return nil, stateMismatchError(err, "vault escrow registration=%t", registered)
		}
		hotkey, err := roleBytes32(e.roles, escrowHotkeyLabelForGeneration(p.Manifest.RegistrationRoleGeneration))
		if err != nil {
			return nil, err
		}
		liveUID, found, err := e.substrate.UID(hotkey)
		if err != nil || !found {
			return nil, stateMismatchError(err, "vault escrow live UID=%d found=%t", liveUID, found)
		}
		owner, err := e.substrate.HotkeyOwner(hotkey)
		if err != nil {
			return nil, err
		}
		if err := validateHotkeyOwner("vault escrow hotkey", owner, ss58Mirror(p.Manifest.SettlementVault)); err != nil {
			return nil, err
		}
		if err := e.verifyContractRegistrationReplacementPostcondition(a, escrowHotkeyLabelForGeneration(p.Manifest.RegistrationRoleGeneration), 1, state); err != nil {
			return nil, fmt.Errorf("vault escrow replacement: %w", err)
		}
		balances, err := e.verifyRegistrationBalances(ctx, a, p.Manifest.SettlementVault)
		if err != nil {
			return nil, err
		}
		state["vault"], state["escrow_hotkey"], state["uid"] = p.Manifest.SettlementVault.Hex(), "0x"+hex.EncodeToString(hotkey[:]), liveUID
		state["coldkey"] = "0x" + hex.EncodeToString(owner[:])
		state["liquid_registration_balance_preserved"] = true
		state["registration_balances"] = balances
		return state, nil
	}
	contract := stabi.NewSTReserveSink()
	got, err := rawCoordinatorCall(ctx, e.deployer, p.Manifest.ReserveSink, contract.PackRecorder(), contract.UnpackRecorder)
	if err != nil || got != p.Manifest.CoordinatorProxy {
		return nil, stateMismatchError(err, "reserve recorder=%s", got)
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

// Use the verified ancestor that owns a carried action; before the first
// verification, only the active plan may supply its transaction checkpoint.
func (e *Executor) finalizedRegistrationActionBlock(action Action) (uint64, error) {
	if e == nil || e.plan == nil || e.journal == nil {
		return 0, errors.New("registration transaction evidence is unavailable")
	}
	if action.Kind == "substrate-reconciliation" {
		block, err := strconv.ParseUint(action.Parameters[alphaRecoveryBlockParameter], 10, 64)
		if err != nil || block == 0 || !hasFinalizedAlphaRecoveryEvidence(e.plan, action, e.journal.Entries()) {
			return 0, fmt.Errorf("action %s has no exact finalized recovery block", action.ID)
		}
		return block, nil
	}
	planHash := e.plan.PlanHash
	if verified, ok := e.verifiedActionEntry(action); ok {
		planHash = verified.PlanHash
	}
	return finalizedActionBlock(e.journal.Entries(), planHash, action)
}

// Records both EVM-reducible and native-free views across one registration.
// Runtime 452 may retain at most one hidden existential deposit, but may not
// consume an existing balance or retain any new liquid surplus.
type registrationBalanceObservation struct {
	Address         string `json:"address"`
	EVMBeforeWei    string `json:"evm_before_wei"`
	EVMAfterWei     string `json:"evm_after_wei"`
	NativeBeforeRao uint64 `json:"native_before_rao"`
	NativeAfterRao  uint64 `json:"native_after_rao"`
}

func expectedRuntime452EVMBalance(freeRao, existentialDepositRao uint64) *big.Int {
	if freeRao <= existentialDepositRao {
		return new(big.Int)
	}
	return new(big.Int).Mul(new(big.Int).SetUint64(freeRao-existentialDepositRao), new(big.Int).SetUint64(evmWeiPerRao))
}

func validateRegistrationBalanceObservation(observation registrationBalanceObservation, existentialDepositRao uint64) error {
	before, beforeOK := new(big.Int).SetString(observation.EVMBeforeWei, 10)
	after, afterOK := new(big.Int).SetString(observation.EVMAfterWei, 10)
	if existentialDepositRao == 0 || !beforeOK || !afterOK || before.Sign() < 0 || after.Sign() < 0 {
		return errors.New("registration balance observation is malformed")
	}
	if before.Cmp(expectedRuntime452EVMBalance(observation.NativeBeforeRao, existentialDepositRao)) != 0 || after.Cmp(expectedRuntime452EVMBalance(observation.NativeAfterRao, existentialDepositRao)) != 0 {
		return fmt.Errorf("registration balance views disagree for %s", observation.Address)
	}
	if before.Cmp(after) != 0 {
		return fmt.Errorf("registration changed %s liquid balance from %s to %s", observation.Address, before, after)
	}
	if observation.NativeAfterRao < observation.NativeBeforeRao || observation.NativeAfterRao-observation.NativeBeforeRao > existentialDepositRao {
		return fmt.Errorf("registration changed %s native free balance from %d to %d beyond one existential deposit", observation.Address, observation.NativeBeforeRao, observation.NativeAfterRao)
	}
	return nil
}

func (e *Executor) verifyRegistrationBalances(ctx context.Context, action Action, addresses ...common.Address) ([]registrationBalanceObservation, error) {
	block, err := e.finalizedRegistrationActionBlock(action)
	if err != nil || block == 0 {
		return nil, err
	}
	observations := make([]registrationBalanceObservation, 0, len(addresses))
	for _, address := range addresses {
		before, readErr := e.deployer.client.BalanceAt(ctx, address, new(big.Int).SetUint64(block-1))
		if readErr != nil {
			return nil, fmt.Errorf("read %s registration pre-balance at %d: %w", address, block-1, readErr)
		}
		after, readErr := e.deployer.client.BalanceAt(ctx, address, new(big.Int).SetUint64(block))
		if readErr != nil {
			return nil, fmt.Errorf("read %s registration post-balance at %d: %w", address, block, readErr)
		}
		mirror := ss58Mirror(address)
		nativeBefore, readErr := e.substrate.FreeBalanceAtBlock(mirror, block-1)
		if readErr != nil {
			return nil, fmt.Errorf("read %s registration pre-free-balance at %d: %w", address, block-1, readErr)
		}
		nativeAfter, readErr := e.substrate.FreeBalanceAtBlock(mirror, block)
		if readErr != nil {
			return nil, fmt.Errorf("read %s registration post-free-balance at %d: %w", address, block, readErr)
		}
		observation := registrationBalanceObservation{Address: address.Hex(), EVMBeforeWei: before.String(), EVMAfterWei: after.String(), NativeBeforeRao: nativeBefore, NativeAfterRao: nativeAfter}
		if err := validateRegistrationBalanceObservation(observation, e.plan.LiveFacts.ExistentialDepositRao); err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	return observations, nil
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
	pool, _ := roleBytes32(e.roles, operatorPoolHotkeyLabelForGeneration(noID, e.payloads.Manifest.RegistrationRoleGeneration))
	deposit, _ := roleBytes32(e.roles, fmt.Sprintf("operator-%d-deposit-hotkey", noID))
	depositSigner, _ := e.roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", noID))
	rootSigner, _ := e.roles.EVMAddress(fmt.Sprintf("operator-%d-root", noID))
	if !version.Active || version.Coldkey != coldkey || version.PoolHotkey != pool || version.DepositHotkey != deposit || version.DepositSigner != depositSigner || version.RootSigner != rootSigner || version.EffectiveEpoch > epoch.Uint64() {
		return nil, fmt.Errorf("operator %d finalized registry postcondition mismatch", noID)
	}
	uid, found, err := e.substrate.UID(pool)
	if err != nil || !found {
		return nil, stateMismatchError(err, "operator %d pool UID missing", noID)
	}
	owner, err := e.substrate.HotkeyOwner(pool)
	if err != nil {
		return nil, err
	}
	if err := validateHotkeyOwner(fmt.Sprintf("operator %d pool", noID), owner, ss58Mirror(e.payloads.Manifest.SettlementVault)); err != nil {
		return nil, err
	}
	if err := e.verifyContractRegistrationReplacementPostcondition(action, operatorPoolHotkeyLabelForGeneration(noID, e.payloads.Manifest.RegistrationRoleGeneration), noID+1, state); err != nil {
		return nil, fmt.Errorf("operator %d pool replacement: %w", noID, err)
	}
	balances, err := e.verifyRegistrationBalances(ctx, action, e.payloads.Manifest.CoordinatorProxy, e.payloads.Manifest.SettlementVault)
	if err != nil {
		return nil, err
	}
	state["no_id"], state["effective_epoch"], state["pool_uid"], state["active"] = noID, version.EffectiveEpoch, uid, true
	state["pool_coldkey"] = "0x" + hex.EncodeToString(owner[:])
	state["liquid_registration_balances_preserved"] = true
	state["registration_balances"] = balances
	state["deposit_signer"], state["root_signer"] = depositSigner.Hex(), rootSigner.Hex()
	return state, nil
}

func (e *Executor) verifyFleetCommitmentPostState(action Action, fleet int, state map[string]any) (map[string]any, error) {
	_, commitmentHash, evidence, _, err := e.validatedFleetCommitmentGeneration(fleet, 1)
	if err != nil {
		return nil, err
	}
	if err := validateFleetCommitmentRecoveryEvidence(action, evidence); err != nil {
		return nil, err
	}
	state["fleet"], state["commitment_hash"], state["commitment_block"] = fleet, "0x"+hex.EncodeToString(commitmentHash[:]), evidence.CommitmentBlock
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
	evidence, err := loadFleetCommitmentEvidence(e.stateDir, fleet)
	if err != nil {
		return nil, err
	}
	finalizedBlockHash, err := decodeHex32("fleet commitment finalized block", evidence.FinalizedBlockHash)
	if err != nil {
		return nil, err
	}
	contract := stabi.NewSTCoordinator()
	got, err := rawCoordinatorCall(ctx, e.oracle, e.payloads.Manifest.CoordinatorProxy, contract.PackMirroredCommitments(manifest.Hotkey), contract.UnpackMirroredCommitments)
	if err != nil || !fleetMirrorMatches(got, hash, evidence.CommitmentBlock, finalizedBlockHash) {
		return nil, stateMismatchError(err, "fleet %d mirror mismatch", fleet)
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
		return nil, stateMismatchError(err, "fleet %d member %d binding postcondition mismatch", fleet, member)
	}
	state["fleet"], state["member"], state["client_id"], state["uid"] = fleet, member, evidence.ClientID, evidence.UID
	return state, nil
}

func (e *Executor) verifyRenderedConfigs(state map[string]any) (map[string]any, error) {
	if err := validateOperatorConfigOverlays(e.cfg, e.stateDir); err != nil {
		return nil, err
	}
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
			return nil, stateMismatchError(err, "rendered config %s is absent or not private", path)
		}
	}
	state["private_config_files"] = len(paths)
	state["operator_config_overlay"] = operatorConfigOverlayVersion
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
