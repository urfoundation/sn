package main

// dishonest_deposit.go drives one real, bounded operator-deposit fault on the
// public testnet. The operator taskworker is paused immediately before a fresh
// production-cadence epoch, its scoped signer posts a runtime-valid 50-percent
// underpayment, and the real
// validators must independently exclude that pool once the prior signed usage
// artifact is available. The taskworker is always resumed; a later exact
// deposit must restore eligibility.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

const (
	dishonestDepositRao             = uint64(5_000_000_000)
	dishonestDepositActionID        = "campaign.dishonest-deposit.2"
	dishonestDepositTransactionV1   = "urnetwork-dishonest-deposit-transaction-v1"
	dishonestDepositEvidenceV1      = "urnetwork-dishonest-deposit-evidence-v1"
	dishonestDepositEvidenceName    = "dishonest-deposit.json"
	dishonestDepositTransactionName = "dishonest-deposit-transaction.json"
)

type DishonestDepositTransactionEvidence struct {
	Schema             string `json:"schema"`
	DeploymentID       string `json:"deployment_id"`
	NoID               uint64 `json:"no_id"`
	Epoch              uint64 `json:"epoch"`
	AmountRao          string `json:"amount_rao"`
	Nonce              string `json:"nonce"`
	Funder             string `json:"funder"`
	PolicyHash         string `json:"policy_hash"`
	TransactionHash    string `json:"transaction_hash"`
	FinalizedBlock     uint64 `json:"finalized_block"`
	FinalizedBlockHash string `json:"finalized_block_hash"`
}

type DishonestDepositValidatorEvidence struct {
	ValidatorID          int                       `json:"validator_id"`
	SubnetEpoch          uint64                    `json:"subnet_epoch"`
	VectorHash           string                    `json:"vector_hash"`
	ApplicationBlock     uint64                    `json:"application_block"`
	ApplicationBlockHash string                    `json:"application_block_hash"`
	PoolUID              uint16                    `json:"pool_uid"`
	PoolMasked           bool                      `json:"pool_masked"`
	PoolPresent          bool                      `json:"pool_present"`
	PoolWeight           uint16                    `json:"pool_weight"`
	Audit                validatorpkg.DepositAudit `json:"audit"`
}

type DishonestDepositEvidence struct {
	Schema       string                              `json:"schema"`
	DeploymentID string                              `json:"deployment_id"`
	Netuid       uint16                              `json:"netuid"`
	Transaction  DishonestDepositTransactionEvidence `json:"transaction"`
	Validators   []DishonestDepositValidatorEvidence `json:"validators"`
}

func dishonestDepositTransactionPath(stateDir string) string {
	return filepath.Join(stateDir, "receipts", dishonestDepositTransactionName)
}

func dishonestDepositEvidencePath(stateDir string) string {
	return filepath.Join(stateDir, "public", dishonestDepositEvidenceName)
}

func decodeStrictJSONFile(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON has a trailing value")
		}
		return err
	}
	return nil
}

func dishonestDepositAction(plan *SetupPlan) (Action, error) {
	if plan == nil {
		return Action{}, errors.New("approved plan is unavailable")
	}
	for _, action := range plan.Actions {
		if action.ID == dishonestDepositActionID {
			return action, nil
		}
	}
	return Action{}, fmt.Errorf("approved plan has no %s action", dishonestDepositActionID)
}

func dishonestDepositParameters(action Action) (uint64, *big.Int, error) {
	if action.ID != dishonestDepositActionID || action.Kind != "evm-transaction" || action.Target != "no:2" || action.Parameters["target_epoch"] != "next_fresh_production_epoch" {
		return 0, nil, errors.New("dishonest-deposit action identity is invalid")
	}
	allowed := map[string]bool{"no_id": true, "amount_rao": true, "target_epoch": true, "reserve_runtime_share_transitions": true, "reserve_rounding_allowance_rao": true, evmMaximumGasUnitsParameter: true, evmMaximumFeePerGasParameter: true, deploymentManifestHashParameter: true}
	for key := range action.Parameters {
		if !allowed[key] {
			return 0, nil, fmt.Errorf("dishonest-deposit action has unknown parameter %q", key)
		}
	}
	transitions, transitionsErr := strconv.ParseUint(action.Parameters["reserve_runtime_share_transitions"], 10, 64)
	allowance, allowanceErr := strconv.ParseUint(action.Parameters["reserve_rounding_allowance_rao"], 10, 64)
	if transitionsErr != nil || allowanceErr != nil || transitions != reserveRuntimeShareTransitionCount || allowance != reserveRoundingAllowancePerCallRao {
		return 0, nil, errors.New("dishonest-deposit action has an invalid runtime reserve envelope")
	}
	_, hasGasUnits := action.Parameters[evmMaximumGasUnitsParameter]
	_, hasFeePerGas := action.Parameters[evmMaximumFeePerGasParameter]
	manifestHash, hasManifest := action.Parameters[deploymentManifestHashParameter]
	if hasGasUnits != hasFeePerGas || hasGasUnits != hasManifest || len(action.Parameters) != 5 && len(action.Parameters) != 8 {
		return 0, nil, errors.New("dishonest-deposit action has an incomplete EVM fee or deployment envelope")
	}
	if hasGasUnits {
		if _, _, err := evmActionFeeEnvelope(action); err != nil {
			return 0, nil, err
		}
		if _, err := decodeHex32("dishonest-deposit deployment manifest hash", manifestHash); err != nil {
			return 0, nil, err
		}
	}
	noID, err := strconv.ParseUint(action.Parameters["no_id"], 10, 64)
	if err != nil || noID != 2 {
		return 0, nil, errors.New("dishonest-deposit action no_id must be 2")
	}
	amount, ok := new(big.Int).SetString(action.Parameters["amount_rao"], 10)
	if !ok || amount.Cmp(new(big.Int).SetUint64(dishonestDepositRao)) != 0 {
		return 0, nil, fmt.Errorf("dishonest-deposit action amount must be exactly %d rao", dishonestDepositRao)
	}
	return noID, amount, nil
}

func coordinatorBigInt(values []any, method string) (*big.Int, error) {
	if len(values) != 1 {
		return nil, fmt.Errorf("%s returned %d values", method, len(values))
	}
	value, ok := values[0].(*big.Int)
	if !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("%s returned %T", method, values[0])
	}
	return value, nil
}

func parseDishonestDepositReceipt(cfg *ResolvedConfig, action Action, receipt *ethTypes.Receipt, parsed abi.ABI, coordinator, expectedFunder common.Address) (*DishonestDepositTransactionEvidence, error) {
	noID, amount, err := dishonestDepositParameters(action)
	if err != nil {
		return nil, err
	}
	if receipt == nil || receipt.Status != ethTypes.ReceiptStatusSuccessful || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() {
		return nil, errors.New("dishonest deposit receipt is not a successful uint64 inclusion")
	}
	event, ok := parsed.Events["Deposit"]
	if !ok {
		return nil, errors.New("coordinator ABI has no Deposit event")
	}
	for _, log := range receipt.Logs {
		if log.Address != coordinator || len(log.Topics) != 4 || log.Topics[0] != event.ID || log.Topics[1].Big().Cmp(new(big.Int).SetUint64(noID)) != 0 || common.BytesToAddress(log.Topics[3].Bytes()[12:]) != expectedFunder {
			continue
		}
		values, unpackErr := event.Inputs.NonIndexed().Unpack(log.Data)
		if unpackErr != nil || len(values) != 3 {
			continue
		}
		eventAmount, amountOK := values[0].(*big.Int)
		policyHash, policyOK := values[1].([32]byte)
		nonce, nonceOK := values[2].(*big.Int)
		epoch := log.Topics[2].Big()
		if !amountOK || !policyOK || !nonceOK || !epoch.IsUint64() || eventAmount.Cmp(amount) != 0 || !strings.EqualFold("0x"+hex.EncodeToString(policyHash[:]), cfg.PolicyHash) {
			continue
		}
		return &DishonestDepositTransactionEvidence{
			Schema: dishonestDepositTransactionV1, DeploymentID: cfg.Config.Deployment.DeploymentID,
			NoID: noID, Epoch: epoch.Uint64(), AmountRao: amount.String(), Nonce: nonce.String(),
			Funder: expectedFunder.Hex(), PolicyHash: cfg.PolicyHash, TransactionHash: receipt.TxHash.Hex(),
			FinalizedBlock: receipt.BlockNumber.Uint64(), FinalizedBlockHash: receipt.BlockHash.Hex(),
		}, nil
	}
	return nil, errors.New("finalized receipt lacks the exact dishonest Deposit event")
}

func (e *Executor) executeDishonestDeposit(ctx context.Context, action Action) error {
	if e == nil || e.payloads == nil {
		return errors.New("dishonest deposit requires installed deployment payloads")
	}
	noID, amount, err := dishonestDepositParameters(action)
	if err != nil {
		return err
	}
	manager := e.deposits[int(noID)]
	if manager == nil {
		return fmt.Errorf("operator %d deposit transaction manager is missing", noID)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	address := e.payloads.Manifest.CoordinatorProxy
	funder, err := e.roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", noID))
	if err != nil {
		return err
	}
	if prior, ok := e.journal.LatestTransaction(e.plan.PlanHash, action.ID, action.IntentHash); ok {
		receipt, sendErr := manager.Send(ctx, e.plan.PlanHash, action, &address, new(big.Int), nil)
		if sendErr != nil {
			return sendErr
		}
		evidence, parseErr := parseDishonestDepositReceipt(e.cfg, action, receipt, parsed, address, funder)
		if parseErr != nil {
			return fmt.Errorf("resume %s: %w", prior.TransactionHash, parseErr)
		}
		return writePublicJSON(dishonestDepositTransactionPath(e.stateDir), evidence)
	}
	head, err := finalizedEVMHead(ctx, manager.client)
	if err != nil {
		return err
	}
	epochValues, err := contractCallAt(ctx, manager.client, address, parsed, "currentEpoch", head.Number)
	if err != nil {
		return err
	}
	epoch, err := coordinatorBigInt(epochValues, "currentEpoch")
	if err != nil || !epoch.IsUint64() {
		return stateMismatchError(err, "dishonest deposit current epoch is not uint64")
	}
	depositedValues, err := contractCallAt(ctx, manager.client, address, parsed, "epochDeposits", head.Number, epoch, new(big.Int).SetUint64(noID))
	if err != nil {
		return err
	}
	deposited, err := coordinatorBigInt(depositedValues, "epochDeposits")
	if err != nil || deposited.Sign() != 0 {
		return stateMismatchError(err, "target epoch %d already has operator %d deposit %v", epoch.Uint64(), noID, deposited)
	}
	nonceValues, err := contractCallAt(ctx, manager.client, address, parsed, "nextDepositNonce", head.Number, new(big.Int).SetUint64(noID))
	if err != nil {
		return err
	}
	nonce, err := coordinatorBigInt(nonceValues, "nextDepositNonce")
	if err != nil {
		return err
	}
	if head.Number > math.MaxUint64-e.cfg.Policy.Settlement.RootCommitWindowBlocks {
		return errors.New("dishonest deposit deadline overflows uint64")
	}
	deadline := head.Number + e.cfg.Policy.Settlement.RootCommitWindowBlocks
	data, err := parsed.Pack("deposit", new(big.Int).SetUint64(noID), amount, nonce, deadline)
	if err != nil {
		return err
	}
	receipt, err := manager.Send(ctx, e.plan.PlanHash, action, &address, new(big.Int), data)
	if err != nil {
		return err
	}
	evidence, err := parseDishonestDepositReceipt(e.cfg, action, receipt, parsed, address, funder)
	if err != nil {
		return err
	}
	if evidence.Epoch != epoch.Uint64() || evidence.Nonce != nonce.String() {
		return fmt.Errorf("dishonest deposit finalized in epoch/nonce %d/%s, intended %d/%s", evidence.Epoch, evidence.Nonce, epoch.Uint64(), nonce)
	}
	return writePublicJSON(dishonestDepositTransactionPath(e.stateDir), evidence)
}

func (e *Executor) verifyDishonestDepositPostState(ctx context.Context, action Action, evmHead ChainHead, state map[string]any) (map[string]any, error) {
	noID, amount, err := dishonestDepositParameters(action)
	if err != nil {
		return nil, err
	}
	var evidence DishonestDepositTransactionEvidence
	if err := decodeStrictJSONFile(dishonestDepositTransactionPath(e.stateDir), &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != dishonestDepositTransactionV1 || evidence.DeploymentID != e.cfg.Config.Deployment.DeploymentID || evidence.NoID != noID || evidence.AmountRao != amount.String() || !strings.EqualFold(evidence.PolicyHash, e.cfg.PolicyHash) {
		return nil, errors.New("dishonest deposit transaction evidence identity mismatch")
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, e.deployer.client, evmHead, evidence.TransactionHash, evidence.FinalizedBlock, evidence.FinalizedBlockHash)
	if err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, err
	}
	funder, err := e.roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", noID))
	if err != nil {
		return nil, err
	}
	reconstructed, err := parseDishonestDepositReceipt(e.cfg, action, receipt, parsed, e.payloads.Manifest.CoordinatorProxy, funder)
	if err != nil || *reconstructed != evidence {
		return nil, stateMismatchError(err, "dishonest deposit receipt reconstruction mismatch")
	}
	address := e.payloads.Manifest.CoordinatorProxy
	values, err := contractCallAt(ctx, e.deployer.client, address, parsed, "epochDeposits", evmHead.Number, new(big.Int).SetUint64(evidence.Epoch), new(big.Int).SetUint64(noID))
	if err != nil {
		return nil, err
	}
	deposited, err := coordinatorBigInt(values, "epochDeposits")
	if err != nil || deposited.Cmp(amount) != 0 {
		return nil, stateMismatchError(err, "dishonest epoch deposit=%v, want %s", deposited, amount)
	}
	state["no_id"], state["epoch"], state["amount_rao"] = noID, evidence.Epoch, evidence.AmountRao
	state["transaction_hash"], state["finalized_block"], state["finalized_block_hash"] = evidence.TransactionHash, evidence.FinalizedBlock, evidence.FinalizedBlockHash
	return state, nil
}

func readValidatorIntentFile(stateDir string, validatorID int) ([]validatorpkg.SteeringIntent, error) {
	var store struct {
		Schema  string                        `json:"schema"`
		Current *validatorpkg.SteeringIntent  `json:"current"`
		History []validatorpkg.SteeringIntent `json:"history"`
	}
	path := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "steering-intents.json")
	if err := decodeStrictJSONFile(path, &store); err != nil {
		return nil, err
	}
	if store.Schema != "urnetwork-validator-steering-intent-v4" {
		return nil, fmt.Errorf("validator %d intent schema %q", validatorID, store.Schema)
	}
	all := append([]validatorpkg.SteeringIntent(nil), store.History...)
	if store.Current != nil {
		all = append(all, *store.Current)
	}
	for index := range all {
		if err := all[index].VerifyVectorHash(); err != nil {
			return nil, fmt.Errorf("validator %d intent %d: %w", validatorID, index, err)
		}
	}
	return all, nil
}

func validatorDishonestDepositEvidence(stateDir string, validatorID int, epoch, noID uint64, poolUID uint16) (*DishonestDepositValidatorEvidence, error) {
	intents, err := readValidatorIntentFile(stateDir, validatorID)
	if err != nil {
		return nil, err
	}
	for index := len(intents) - 1; index >= 0; index-- {
		intent := intents[index]
		if intent.Status != "applied" || intent.SettlementEpoch != epoch {
			continue
		}
		var audit *validatorpkg.DepositAudit
		for auditIndex := range intent.DepositAudits {
			if intent.DepositAudits[auditIndex].NoID == noID {
				copy := intent.DepositAudits[auditIndex]
				audit = &copy
				break
			}
		}
		if audit == nil || audit.Status != validatorpkg.DepositAuditMismatch || audit.Compliant || audit.Disposition != "zero_pool_weight" || audit.ObservedDepositRao != "1" || audit.RequiredDepositRao == "" || audit.RequiredDepositRao == audit.ObservedDepositRao || audit.ArtifactHash == "" {
			continue
		}
		result := &DishonestDepositValidatorEvidence{
			ValidatorID: validatorID, SubnetEpoch: intent.SubnetEpoch, VectorHash: intent.VectorHash,
			ApplicationBlock: intent.ApplicationBlock, ApplicationBlockHash: intent.ApplicationBlockHash,
			PoolUID: poolUID, PoolMasked: slices.Contains(intent.MaskedUIDs, poolUID), Audit: *audit,
		}
		for uidIndex, uid := range intent.UIDs {
			if uid == poolUID {
				result.PoolPresent = true
				if uidIndex < len(intent.Values) {
					result.PoolWeight = intent.Values[uidIndex]
				}
			}
		}
		if result.PoolPresent || result.PoolWeight != 0 || result.ApplicationBlock == 0 || result.ApplicationBlockHash == "" {
			return nil, fmt.Errorf("validator %d did not exclude dishonest pool UID %d", validatorID, poolUID)
		}
		return result, nil
	}
	return nil, fmt.Errorf("validator %d has no applied mismatch audit for settlement epoch %d", validatorID, epoch)
}

func waitForDishonestDepositAudits(ctx context.Context, cfg *ResolvedConfig, stateDir string, transaction DishonestDepositTransactionEvidence, poolUID uint16) (*DishonestDepositEvidence, error) {
	deadlineBlocks := cfg.Policy.ProductionCadence.RootCommitWindowBlocks + 2*hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"])
	if deadlineBlocks < cfg.Policy.ProductionCadence.EpochBlocks {
		deadlineBlocks = cfg.Policy.ProductionCadence.EpochBlocks
	}
	timeout := time.Duration(deadlineBlocks*cfg.Public.Chain.ExpectedBlockSeconds+120) * time.Second
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastErrors []string
	for {
		validators := make([]DishonestDepositValidatorEvidence, 0, cfg.Config.Topology.Validators)
		lastErrors = lastErrors[:0]
		for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
			evidence, err := validatorDishonestDepositEvidence(stateDir, validatorID, transaction.Epoch, transaction.NoID, poolUID)
			if err != nil {
				lastErrors = append(lastErrors, err.Error())
				continue
			}
			validators = append(validators, *evidence)
		}
		if len(validators) == cfg.Config.Topology.Validators {
			unmasked := false
			for _, validator := range validators {
				unmasked = unmasked || !validator.PoolMasked
			}
			if !unmasked {
				return nil, errors.New("every validator affiliation-masked the dishonest operator pool")
			}
			return &DishonestDepositEvidence{
				Schema: dishonestDepositEvidenceV1, DeploymentID: cfg.Config.Deployment.DeploymentID,
				Netuid: cfg.Netuid, Transaction: transaction, Validators: validators,
			}, nil
		}
		select {
		case <-waitCtx.Done():
			return nil, fmt.Errorf("validators did not apply the dishonest-deposit penalty before the bounded deadline: %s: %w", strings.Join(lastErrors, "; "), waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func readProductionEpochState(ctx context.Context, executor *Executor) (ChainHead, uint64, uint64, error) {
	if executor == nil || executor.payloads == nil {
		return ChainHead{}, 0, 0, errors.New("production epoch read requires installed deployment payloads")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return ChainHead{}, 0, 0, err
	}
	head, err := finalizedEVMHead(ctx, executor.deployer.client)
	if err != nil {
		return ChainHead{}, 0, 0, err
	}
	address := executor.payloads.Manifest.CoordinatorProxy
	currentValues, err := contractCallAt(ctx, executor.deployer.client, address, parsed, "currentEpoch", head.Number)
	if err != nil {
		return ChainHead{}, 0, 0, err
	}
	current, err := coordinatorBigInt(currentValues, "currentEpoch")
	if err != nil || !current.IsUint64() {
		return ChainHead{}, 0, 0, stateMismatchError(err, "production current epoch is not uint64")
	}
	endValues, err := contractCallAt(ctx, executor.deployer.client, address, parsed, "epochEndBlock", head.Number, current)
	if err != nil {
		return ChainHead{}, 0, 0, err
	}
	end, err := coordinatorBigInt(endValues, "epochEndBlock")
	if err != nil || !end.IsUint64() {
		return ChainHead{}, 0, 0, stateMismatchError(err, "production epoch end is not uint64")
	}
	return head, current.Uint64(), end.Uint64(), nil
}

func waitForFreshProductionBoundary(ctx context.Context, executor *Executor) (uint64, error) {
	lead := uint64(3)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		head, epoch, end, err := readProductionEpochState(ctx, executor)
		if err != nil {
			return 0, err
		}
		if end <= head.Number || end-head.Number <= lead {
			if epoch == math.MaxUint64 {
				return 0, errors.New("production epoch overflows uint64")
			}
			return epoch + 1, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForEpoch(ctx context.Context, executor *Executor, target uint64) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		_, current, _, err := readProductionEpochState(ctx, executor)
		if err != nil {
			return err
		}
		if current == target {
			return nil
		}
		if current > target {
			return fmt.Errorf("contract advanced to epoch %d before target deposit epoch %d", current, target)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func verifyProductionPolicyAtEpoch(ctx context.Context, executor *Executor, epoch uint64) error {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	head, current, _, err := readProductionEpochState(ctx, executor)
	if err != nil {
		return err
	}
	if current != epoch {
		return fmt.Errorf("production policy check is at epoch %d, want %d", current, epoch)
	}
	values, err := contractCallAt(ctx, executor.deployer.client, executor.payloads.Manifest.CoordinatorProxy, parsed, "policyAt", head.Number, new(big.Int).SetUint64(epoch))
	if err != nil {
		return err
	}
	policy, err := coordinatorPolicy(values)
	if err != nil {
		return err
	}
	if !productionPolicyMatches(executor.cfg, policy) || policy.EffectiveEpoch > epoch {
		return fmt.Errorf("epoch %d does not use the release-locked production cadence", epoch)
	}
	return nil
}

func waitProcessHealthy(ctx context.Context, driver *liveScenarioFaultDriver, id string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		states, _, err := driver.processSnapshot()
		if err == nil && states[id].PID > 1 && states[id].Healthy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func operatorPoolUID(ctx context.Context, cfg *ResolvedConfig, stateDir string, noID uint64) (uint16, error) {
	status, err := Status(ctx, cfg, stateDir)
	if err != nil || status.Contracts == nil {
		return 0, stateMismatchError(err, "read operator pool: contract state is unavailable")
	}
	for _, operator := range status.Contracts.Operators {
		if operator.NoID == noID && operator.PoolLive {
			return operator.PoolUID, nil
		}
	}
	return 0, fmt.Errorf("operator %d has no live pool UID", noID)
}

func runDishonestDepositPhase(ctx context.Context, cfg *ResolvedConfig, stateDir string, executor *Executor) error {
	if _, err := os.Stat(dishonestDepositEvidencePath(stateDir)); err == nil {
		status, statusErr := Status(ctx, cfg, stateDir)
		if statusErr != nil {
			return fmt.Errorf("revalidate prior dishonest-deposit evidence: %w", statusErr)
		}
		if status.Contracts == nil {
			return errors.New("revalidate prior dishonest-deposit evidence: contract state is unavailable")
		}
		_, valid, detail := inspectDishonestDepositEvidence(ctx, cfg, stateDir, status.Contracts)
		if !valid {
			return fmt.Errorf("prior dishonest-deposit evidence is invalid: %s", detail)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	action, err := dishonestDepositAction(executor.plan)
	if err != nil {
		return err
	}
	noID, _, err := dishonestDepositParameters(action)
	if err != nil {
		return err
	}
	poolUID, err := operatorPoolUID(ctx, cfg, stateDir, noID)
	if err != nil {
		return err
	}
	var transaction DishonestDepositTransactionEvidence
	if err := decodeStrictJSONFile(dishonestDepositTransactionPath(stateDir), &transaction); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		driver := &liveScenarioFaultDriver{stateDir: stateDir, cfg: cfg}
		target, err := waitForFreshProductionBoundary(ctx, executor)
		if err != nil {
			return err
		}
		fault := scenarioFaultSpec{ID: "dishonest-deposit-taskworker-fence", Kind: "process-pause", Targets: []string{fmt.Sprintf("operator-%d-taskworker", noID)}}
		if _, err := driver.Apply(ctx, fault); err != nil {
			return fmt.Errorf("pause honest operator deposit scheduler: %w", err)
		}
		restored := false
		defer func() {
			if !restored {
				_, _ = driver.Restore(context.Background(), fault)
			}
		}()
		if err := waitForEpoch(ctx, executor, target); err != nil {
			return err
		}
		if err := verifyProductionPolicyAtEpoch(ctx, executor, target); err != nil {
			return err
		}
		if err := executor.Execute(ctx, action); err != nil {
			return err
		}
		if _, err := driver.Restore(ctx, fault); err != nil {
			return fmt.Errorf("resume operator deposit scheduler: %w", err)
		}
		restored = true
		healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := waitProcessHealthy(healthCtx, driver, fmt.Sprintf("operator-%d-taskworker", noID)); err != nil {
			return fmt.Errorf("operator deposit scheduler did not recover: %w", err)
		}
		if err := decodeStrictJSONFile(dishonestDepositTransactionPath(stateDir), &transaction); err != nil {
			return err
		}
		if transaction.Epoch != target {
			return fmt.Errorf("dishonest deposit landed in epoch %d, fenced target was %d", transaction.Epoch, target)
		}
	}
	evidence, err := waitForDishonestDepositAudits(ctx, cfg, stateDir, transaction, poolUID)
	if err != nil {
		return err
	}
	return writePublicJSON(dishonestDepositEvidencePath(stateDir), evidence)
}

func inspectDishonestDepositEvidence(ctx context.Context, cfg *ResolvedConfig, stateDir string, contracts *ContractView) (*DishonestDepositEvidence, bool, string) {
	var evidence DishonestDepositEvidence
	if err := decodeStrictJSONFile(dishonestDepositEvidencePath(stateDir), &evidence); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, ""
		}
		return nil, false, err.Error()
	}
	if contracts == nil || contracts.Deployment == nil {
		return &evidence, false, "contract deployment is unavailable"
	}
	transaction := evidence.Transaction
	if evidence.Schema != dishonestDepositEvidenceV1 || evidence.DeploymentID != cfg.Config.Deployment.DeploymentID || evidence.Netuid != cfg.Netuid || transaction.Schema != dishonestDepositTransactionV1 || transaction.DeploymentID != evidence.DeploymentID || transaction.NoID != 2 || transaction.AmountRao != strconv.FormatUint(cfg.Config.Scenarios.DishonestDepositRao, 10) || !strings.EqualFold(transaction.PolicyHash, cfg.PolicyHash) || len(evidence.Validators) != cfg.Config.Topology.Validators {
		return &evidence, false, "dishonest-deposit evidence identity mismatch"
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return &evidence, false, err.Error()
	}
	defer client.Close()
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return &evidence, false, err.Error()
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, client, head, transaction.TransactionHash, transaction.FinalizedBlock, transaction.FinalizedBlockHash)
	if err != nil {
		return &evidence, false, err.Error()
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return &evidence, false, err.Error()
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil || len(roles.OperatorDepositSigners) < 2 {
		return &evidence, false, "operator-2 deposit signer is unavailable"
	}
	action := Action{ID: dishonestDepositActionID, Kind: "evm-transaction", Target: "no:2", Parameters: map[string]string{"no_id": "2", "amount_rao": strconv.FormatUint(cfg.Config.Scenarios.DishonestDepositRao, 10), "target_epoch": "next_fresh_production_epoch"}}
	reconstructed, err := parseDishonestDepositReceipt(cfg, action, receipt, parsed, contracts.Deployment.CoordinatorProxy, common.HexToAddress(roles.OperatorDepositSigners[1]))
	if err != nil || *reconstructed != transaction {
		return &evidence, false, fmt.Sprintf("dishonest deposit transaction reconstruction mismatch: %v", err)
	}
	values, err := contractCallAt(ctx, client, contracts.Deployment.CoordinatorProxy, parsed, "epochDeposits", head.Number, new(big.Int).SetUint64(transaction.Epoch), new(big.Int).SetUint64(transaction.NoID))
	if err != nil {
		return &evidence, false, err.Error()
	}
	deposited, err := coordinatorBigInt(values, "epochDeposits")
	if err != nil || deposited.Cmp(big.NewInt(1)) != 0 {
		return &evidence, false, fmt.Sprintf("dishonest epoch deposit=%v: %v", deposited, err)
	}
	poolUID := uint16(0)
	poolFound := false
	for _, operator := range contracts.Operators {
		if operator.NoID == transaction.NoID && operator.PoolLive {
			poolUID, poolFound = operator.PoolUID, true
			break
		}
	}
	if !poolFound {
		return &evidence, false, "dishonest operator pool is not live"
	}
	unmasked := false
	seen := map[int]bool{}
	for _, recorded := range evidence.Validators {
		if recorded.ValidatorID < 1 || recorded.ValidatorID > cfg.Config.Topology.Validators || seen[recorded.ValidatorID] || recorded.PoolUID != poolUID {
			return &evidence, false, "dishonest-deposit validator evidence identity mismatch"
		}
		seen[recorded.ValidatorID] = true
		actual, readErr := validatorDishonestDepositEvidence(stateDir, recorded.ValidatorID, transaction.Epoch, transaction.NoID, poolUID)
		if readErr != nil || *actual != recorded {
			return &evidence, false, fmt.Sprintf("validator %d dishonest-deposit evidence mismatch: %v", recorded.ValidatorID, readErr)
		}
		unmasked = unmasked || !recorded.PoolMasked
	}
	if len(seen) != cfg.Config.Topology.Validators || !unmasked {
		return &evidence, false, "dishonest-deposit penalty lacks an unaffiliated validator"
	}
	return &evidence, true, ""
}
