// Voluntary-conviction recovery keeps the one-time tier-boundary mutation
// idempotent across plan revisions and binds any recovered duplicate exactly.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	voluntaryConvictionActionID                      = "campaign.voluntary-conviction.1"
	voluntaryConvictionReconciliationActionID        = "campaign.reconcile-voluntary-conviction.1"
	voluntaryRecoveryDuplicatePlanHashParameter      = "duplicate_plan_hash"
	voluntaryRecoveryDuplicateIntentHashParameter    = "duplicate_intent_hash"
	voluntaryRecoveryDuplicateTransactionParameter   = "duplicate_transaction_hash"
	voluntaryRecoveryDuplicateBlockParameter         = "duplicate_block"
	voluntaryRecoveryDuplicateBlockHashParameter     = "duplicate_block_hash"
	voluntaryRecoveryDuplicateEpochParameter         = "duplicate_epoch"
	voluntaryRecoveryDuplicateNonceParameter         = "duplicate_nonce"
	voluntaryRecoveryOriginalPlanHashParameter       = "original_plan_hash"
	voluntaryRecoveryOriginalIntentHashParameter     = "original_intent_hash"
	voluntaryRecoveryOriginalTransactionParameter    = "original_transaction_hash"
	voluntaryRecoveryOriginalBlockParameter          = "original_block"
	voluntaryRecoveryOriginalBlockHashParameter      = "original_block_hash"
	voluntaryRecoveryOriginalEpochParameter          = "original_epoch"
	voluntaryRecoveryOriginalNonceParameter          = "original_nonce"
	voluntaryRecoveryAmountParameter                 = "amount_rao"
	voluntaryRecoveryCumulativeBeforeParameter       = "cumulative_before_rao"
	voluntaryRecoveryCumulativeAfterParameter        = "cumulative_after_rao"
	voluntaryRecoveryOperatorPrincipalAfterParameter = "operator_principal_after_rao"
	voluntaryRecoveryFunderParameter                 = "funder"
	voluntaryRecoveryPolicyHashParameter             = "policy_hash"
	voluntaryRecoverySupersededGasBeforeParameter    = "superseded_evm_gas_before_wei"
	voluntaryRecoveryDuplicateMaximumGasParameter    = "duplicate_maximum_evm_gas_wei"
	voluntaryRecoverySupersededGasAfterParameter     = "superseded_evm_gas_after_wei"
	voluntaryRecoveryRepairActionParameter           = "repair_action_id"
)

// Captures the exact event fields emitted by one finalized conviction call.
type voluntaryConvictionEvent struct {
	NoID       *big.Int
	Epoch      *big.Int
	Funder     common.Address
	Amount     *big.Int
	PolicyHash [32]byte
	Nonce      *big.Int
}

// Carries the authenticated facts needed to adopt exactly one successful
// duplicate without allowing a different successful transaction through the
// generic revision guard.
type voluntaryConvictionDuplicateRecovery struct {
	DuplicateTransaction      planRevisionTransaction
	DuplicateAction           Action
	OriginalAction            Action
	OriginalPlanHash          string
	OriginalIntentHash        string
	OriginalPlanPolicyHash    string
	DuplicatePlanPolicyHash   string
	OriginalEvidence          VoluntaryConvictionEvidence
	DuplicateEpoch            uint64
	DuplicateNonce            string
	Funder                    string
	PolicyHash                string
	AmountRao                 uint64
	CumulativeBeforeRao       uint64
	CumulativeAfterRao        uint64
	OperatorPrincipalAfterRao uint64
	SupersededGasBefore       DecimalUint
	AlreadyPlanned            bool
}

// Rejects a new logical intent after any cumulative conviction has already
// been recorded. An exact current-intent resume is allowed because its
// transaction manager recovers immutable bytes instead of signing again.
func validateVoluntaryConvictionPrestate(cumulative *big.Int, resumed bool) error {
	if cumulative == nil || cumulative.Sign() < 0 {
		return errors.New("voluntary conviction cumulative prestate is invalid")
	}
	if !resumed && cumulative.Sign() != 0 {
		return fmt.Errorf("voluntary conviction cumulative prestate is %s, want zero before a new intent", cumulative)
	}
	return nil
}

// Decode exactly one matching event. Multiple matching logs are rejected so
// one receipt cannot be interpreted as a single bounded mutation ambiguously.
func decodeVoluntaryConvictionEvent(parsed abi.ABI, receipt *ethTypes.Receipt, coordinator common.Address, noID uint64) (voluntaryConvictionEvent, error) {
	if receipt == nil {
		return voluntaryConvictionEvent{}, errors.New("voluntary conviction receipt is unavailable")
	}
	event, ok := parsed.Events["ConvictionAdded"]
	if !ok {
		return voluntaryConvictionEvent{}, errors.New("Coordinator ABI lacks ConvictionAdded")
	}
	var result voluntaryConvictionEvent
	matches := 0
	for _, log := range receipt.Logs {
		if log.Address != coordinator || len(log.Topics) != 4 || log.Topics[0] != event.ID || !log.Topics[1].Big().IsUint64() || log.Topics[1].Big().Uint64() != noID {
			continue
		}
		values, err := event.Inputs.NonIndexed().Unpack(log.Data)
		if err != nil || len(values) != 3 {
			return voluntaryConvictionEvent{}, stateMismatchError(err, "decode ConvictionAdded event returned %d values", len(values))
		}
		amount, amountOK := values[0].(*big.Int)
		policyHash, policyOK := values[1].([32]byte)
		nonce, nonceOK := values[2].(*big.Int)
		if !amountOK || !policyOK || !nonceOK || !log.Topics[2].Big().IsUint64() {
			return voluntaryConvictionEvent{}, errors.New("ConvictionAdded event has unexpected ABI values")
		}
		matches++
		result = voluntaryConvictionEvent{
			NoID: new(big.Int).Set(log.Topics[1].Big()), Epoch: new(big.Int).Set(log.Topics[2].Big()),
			Funder: common.BytesToAddress(log.Topics[3].Bytes()[12:]), Amount: new(big.Int).Set(amount),
			PolicyHash: policyHash, Nonce: new(big.Int).Set(nonce),
		}
	}
	if matches != 1 {
		return voluntaryConvictionEvent{}, fmt.Errorf("finalized voluntary conviction receipt has %d matching events, want exactly one", matches)
	}
	return result, nil
}

// Proves the semantic identity and bounded accounting of the only recoverable
// duplicate class. Chain finality and receipt membership are checked by the
// caller before these immutable facts are accepted.
func validateVoluntaryConvictionDuplicateRecovery(cfg *ResolvedConfig, recovery voluntaryConvictionDuplicateRecovery) error {
	if cfg == nil || cfg.Config == nil || len(recovery.DuplicateAction.Parameters) == 0 || len(recovery.OriginalAction.Parameters) == 0 {
		return errors.New("voluntary conviction duplicate recovery context is incomplete")
	}
	transaction := recovery.DuplicateTransaction
	duplicateIntent, duplicateIntentErr := actionIntentHash(recovery.DuplicateAction)
	originalIntent, originalIntentErr := actionIntentHash(recovery.OriginalAction)
	if transaction.ActionID != voluntaryConvictionActionID || recovery.DuplicateAction.ID != voluntaryConvictionActionID || recovery.OriginalAction.ID != voluntaryConvictionActionID ||
		duplicateIntentErr != nil || originalIntentErr != nil || duplicateIntent != recovery.DuplicateAction.IntentHash || originalIntent != recovery.OriginalAction.IntentHash ||
		recovery.DuplicateAction.IntentHash != transaction.IntentHash || recovery.OriginalAction.IntentHash != recovery.OriginalIntentHash || !sameEVMTransactionExceptGasUnits(recovery.OriginalAction, recovery.DuplicateAction) {
		return errors.New("duplicate recovery actions are not the same voluntary conviction apart from gas units")
	}
	if recovery.DuplicateAction.Parameters["no_id"] != "1" || recovery.OriginalAction.Parameters["no_id"] != "1" ||
		recovery.DuplicateAction.Parameters["amount_rao"] != strconv.FormatUint(recovery.AmountRao, 10) || recovery.OriginalAction.Parameters["amount_rao"] != strconv.FormatUint(recovery.AmountRao, 10) {
		return errors.New("duplicate recovery action does not bind no 1 and its exact amount")
	}
	if recovery.AmountRao == 0 || recovery.AmountRao != cfg.Config.Scenarios.VoluntaryConvictionRao {
		return errors.New("duplicate recovery amount differs from the configured voluntary conviction")
	}
	wantAfter, ok := checkedMul(recovery.AmountRao, 2)
	if !ok || recovery.CumulativeBeforeRao != recovery.AmountRao || recovery.CumulativeAfterRao != wantAfter || recovery.OperatorPrincipalAfterRao != wantAfter {
		return errors.New("duplicate recovery does not bind the exact one-to-two conviction and reserve-principal transition")
	}
	if recovery.OriginalEvidence.NoID != 1 || recovery.OriginalEvidence.AmountRao != strconv.FormatUint(recovery.AmountRao, 10) ||
		recovery.OriginalEvidence.BeforeConvictionRao != "0" || recovery.OriginalEvidence.AfterConvictionRao != strconv.FormatUint(recovery.AmountRao, 10) ||
		recovery.OriginalEvidence.Epoch > recovery.DuplicateEpoch || recovery.OriginalEvidence.FinalizedBlock >= transaction.BlockNumber {
		return errors.New("duplicate recovery original evidence is not the prior zero-to-one transition")
	}
	originalNonce, originalOK := new(big.Int).SetString(recovery.OriginalEvidence.Nonce, 10)
	duplicateNonce, duplicateOK := new(big.Int).SetString(recovery.DuplicateNonce, 10)
	if !originalOK || !duplicateOK || originalNonce.Sign() < 0 || duplicateNonce.Cmp(new(big.Int).Add(originalNonce, big.NewInt(1))) != 0 {
		return errors.New("duplicate recovery nonces are not consecutive")
	}
	if !common.IsHexAddress(recovery.Funder) || !strings.EqualFold(recovery.Funder, recovery.OriginalEvidence.Funder) ||
		!strings.EqualFold(recovery.PolicyHash, recovery.OriginalEvidence.PolicyHash) ||
		!strings.EqualFold(recovery.PolicyHash, recovery.OriginalPlanPolicyHash) || !strings.EqualFold(recovery.PolicyHash, recovery.DuplicatePlanPolicyHash) {
		return errors.New("duplicate recovery signer or policy differs from the approved original")
	}
	for name, value := range map[string]string{
		"duplicate plan hash": transaction.PlanHash, "duplicate intent hash": transaction.IntentHash,
		"duplicate transaction hash": transaction.TransactionHash, "duplicate block hash": transaction.BlockHash,
		"original plan hash": recovery.OriginalPlanHash, "original intent hash": recovery.OriginalIntentHash,
		"original transaction hash": recovery.OriginalEvidence.TransactionHash, "original block hash": recovery.OriginalEvidence.FinalizedHash,
		"original plan policy hash": recovery.OriginalPlanPolicyHash, "duplicate plan policy hash": recovery.DuplicatePlanPolicyHash,
	} {
		if _, err := decodeHex32(name, value); err != nil {
			return err
		}
	}
	if transaction.BlockNumber == 0 || transaction.BlockHash == "" || recovery.OriginalEvidence.FinalizedBlock == 0 {
		return errors.New("duplicate recovery has an incomplete finalized checkpoint")
	}
	if recovery.DuplicateAction.Spend.EVMGasWei.IsZero() {
		return errors.New("duplicate recovery has no bounded gas accounting")
	}
	if _, err := recovery.SupersededGasBefore.Big(); err != nil {
		return fmt.Errorf("decode duplicate recovery prior gas accounting: %w", err)
	}
	return nil
}

// Permit the fresh planner's base custody dependency to converge onto the
// exact verified repair that already preceded the authenticated original
// conviction. No other executable or dependency difference is recoverable.
func validateVoluntaryConvictionFreshDependency(revised, prior *SetupPlan, entries []JournalEntry, original, fresh Action) error {
	if sameEVMTransactionExceptGasUnits(original, fresh) {
		return nil
	}
	const baseTransferID = "alpha.transfer.operator-deposit.1"
	if revised == nil || prior == nil || len(original.DependsOn) != len(fresh.DependsOn) {
		return errors.New("fresh voluntary conviction differs from the recovered original beyond gas units")
	}
	difference := -1
	for index := range original.DependsOn {
		if original.DependsOn[index] == fresh.DependsOn[index] {
			continue
		}
		if difference >= 0 {
			return errors.New("fresh voluntary conviction has multiple changed dependencies")
		}
		difference = index
	}
	if difference < 0 || fresh.DependsOn[difference] != baseTransferID {
		return errors.New("fresh voluntary conviction does not differ by its base custody dependency")
	}
	repairID := original.DependsOn[difference]
	kind, operator, err := alphaTransferTargetFromActionID(repairID)
	if err != nil || kind != "operator-deposit" || operator != 1 || !strings.HasPrefix(repairID, "alpha.repair.operator-deposit.1") {
		return stateMismatchError(err, "recovered voluntary-conviction dependency %q is not an operator-1 repair", repairID)
	}
	// Authenticate the base action from the same approved ancestry as the
	// repair. A fresh plan may legitimately rebuild that action from newer live
	// facts and therefore assign it a different intent before the verified
	// alpha-carry pass restores the ancestor. Using the fresh intent here would
	// reject the very verified repair this exception is meant to preserve.
	var baseTransfer *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID != baseTransferID {
			continue
		}
		if baseTransfer != nil {
			return errors.New("prior plan has duplicate operator-1 base transfers")
		}
		copy := prior.Actions[index]
		baseTransfer = &copy
	}
	var repair *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID != repairID {
			continue
		}
		if repair != nil {
			return errors.New("prior plan has duplicate voluntary-conviction custody repairs")
		}
		copy := prior.Actions[index]
		repair = &copy
	}
	if baseTransfer == nil || repair == nil || baseTransfer.Kind != "substrate-extrinsic" || repair.Kind != "substrate-extrinsic" || repair.Target != baseTransfer.Target || repair.Spend.AlphaRao == 0 || repair.Spend.TAORao != 0 || !repair.Spend.EVMGasWei.IsZero() || repair.Spend.Registrations != 0 || repair.Spend.SubnetCreations != 0 ||
		repair.Parameters[alphaRepairForActionParameter] != baseTransferID || !slices.Equal(repair.DependsOn, []string{baseTransferID}) {
		return errors.New("recovered voluntary-conviction custody repair is not rooted in the exact base transfer")
	}
	repairIntent, err := actionIntentHash(*repair)
	if err != nil || repairIntent != repair.IntentHash {
		return stateMismatchError(err, "recovered voluntary-conviction custody repair intent is invalid")
	}
	baseVerified, repairVerified := false, false
	for _, entry := range entries {
		if !prior.allowedPlanHashes()[entry.PlanHash] || entry.Stage != StageVerified {
			continue
		}
		baseVerified = baseVerified || entry.ActionID == baseTransfer.ID && entry.IntentHash == baseTransfer.IntentHash
		repairVerified = repairVerified || entry.ActionID == repair.ID && entry.IntentHash == repair.IntentHash
	}
	if !baseVerified || !repairVerified {
		return errors.New("recovered voluntary-conviction base transfer or custody repair lacks exact verification")
	}
	aligned := fresh
	aligned.DependsOn = append([]string(nil), fresh.DependsOn...)
	aligned.DependsOn[difference] = repairID
	if !sameEVMTransactionExceptGasUnits(original, aligned) {
		return errors.New("fresh voluntary conviction differs from the recovered original beyond its verified custody repair")
	}
	return nil
}

// Load one hash-authenticated plan only from the approved ancestry.
func loadVoluntaryConvictionLineagePlan(stateDir string, prior *SetupPlan, planHash string) (*SetupPlan, error) {
	if prior == nil || !prior.allowedPlanHashes()[planHash] {
		return nil, fmt.Errorf("plan %s is outside the approved voluntary-conviction lineage", planHash)
	}
	if strings.EqualFold(prior.PlanHash, planHash) {
		return prior, nil
	}
	plan, err := readPersistedPlanFile(filepath.Join(stateDir, "plans", stringsTrim0x(planHash)+".json"))
	if err != nil {
		return nil, fmt.Errorf("read voluntary-conviction ancestor plan %s: %w", planHash, err)
	}
	if !strings.EqualFold(plan.PlanHash, planHash) {
		return nil, fmt.Errorf("voluntary-conviction ancestor identity %s differs from %s", plan.PlanHash, planHash)
	}
	return plan, nil
}

// Resolve one exact action intent without accepting a same-id sibling.
func exactVoluntaryConvictionPlanAction(plan *SetupPlan, intentHash string) (Action, error) {
	if plan == nil {
		return Action{}, errors.New("voluntary-conviction plan is unavailable")
	}
	var result *Action
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != voluntaryConvictionActionID || action.IntentHash != intentHash {
			continue
		}
		if result != nil {
			return Action{}, errors.New("plan has duplicate exact voluntary-conviction actions")
		}
		copy := *action
		result = &copy
	}
	if result == nil {
		return Action{}, fmt.Errorf("plan %s has no voluntary-conviction intent %s", plan.PlanHash, intentHash)
	}
	return *result, nil
}

// Identify an earlier recovery envelope and whether its no-broadcast
// postcondition was durably verified.
func priorVoluntaryConvictionRecovery(prior *SetupPlan, entries []JournalEntry, transaction planRevisionTransaction) (Action, bool, bool, error) {
	if prior == nil {
		return Action{}, false, false, errors.New("prior recovery plan is unavailable")
	}
	for _, action := range prior.Actions {
		if action.ID != voluntaryConvictionReconciliationActionID {
			continue
		}
		if action.Kind != "evm-reconciliation" || action.Parameters[voluntaryRecoveryDuplicatePlanHashParameter] != transaction.PlanHash ||
			action.Parameters[voluntaryRecoveryDuplicateIntentHashParameter] != transaction.IntentHash ||
			!strings.EqualFold(action.Parameters[voluntaryRecoveryDuplicateTransactionParameter], transaction.TransactionHash) ||
			action.Parameters[voluntaryRecoveryDuplicateBlockParameter] != strconv.FormatUint(transaction.BlockNumber, 10) ||
			!strings.EqualFold(action.Parameters[voluntaryRecoveryDuplicateBlockHashParameter], transaction.BlockHash) {
			return Action{}, false, false, errors.New("prior voluntary-conviction recovery does not bind the pending duplicate")
		}
		verified := false
		for _, entry := range entries {
			if prior.allowedPlanHashes()[entry.PlanHash] && entry.ActionID == action.ID && entry.IntentHash == action.IntentHash && entry.Stage == StageVerified {
				verified = true
			}
		}
		return action, true, verified, nil
	}
	return Action{}, false, false, nil
}

// Verify the signed call itself, not only a same-shaped event emitted by the
// approved coordinator. This binds signer, calldata nonce/deadline, value, and
// both gas ceilings to the source plan.
func validateDuplicateVoluntaryConvictionTransaction(cfg *ResolvedConfig, plan *SetupPlan, action Action, signed *ethTypes.Transaction, receipt *ethTypes.Receipt, event voluntaryConvictionEvent) error {
	if cfg == nil || plan == nil || signed == nil || receipt == nil || len(plan.Roles.OperatorDepositSigners) == 0 {
		return errors.New("duplicate voluntary-conviction transaction context is incomplete")
	}
	coordinator := plan.Deployment.CoordinatorProxy
	if signed.To() == nil || *signed.To() != coordinator || signed.Value().Sign() != 0 || signed.ChainId().Cmp(new(big.Int).SetUint64(cfg.ChainID)) != 0 {
		return errors.New("duplicate voluntary-conviction transaction destination, value, or chain differs from approval")
	}
	signer := ethTypes.LatestSignerForChainID(signed.ChainId())
	from, err := ethTypes.Sender(signer, signed)
	if err != nil || !strings.EqualFold(from.Hex(), plan.Roles.OperatorDepositSigners[0]) || from != event.Funder {
		return stateMismatchError(err, "duplicate voluntary-conviction signer %s differs from approved %s", from, plan.Roles.OperatorDepositSigners[0])
	}
	maximumGasUnits, err := strconv.ParseUint(action.Parameters[evmMaximumGasUnitsParameter], 10, 64)
	if err != nil || signed.Gas() > maximumGasUnits {
		return stateMismatchError(err, "duplicate voluntary-conviction gas units %d exceed %d", signed.Gas(), maximumGasUnits)
	}
	maximumFee, err := strconv.ParseUint(action.Parameters[evmMaximumFeePerGasParameter], 10, 64)
	if err != nil || !new(big.Int).SetUint64(maximumFee).IsUint64() || signed.GasFeeCap().Cmp(new(big.Int).SetUint64(maximumFee)) > 0 || signed.GasTipCap().Cmp(new(big.Int).SetUint64(maximumFee)) > 0 {
		return stateMismatchError(err, "duplicate voluntary-conviction fee envelope exceeds %d", maximumFee)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	method := parsed.Methods["addConviction"]
	data := signed.Data()
	if len(data) < 4 || !strings.EqualFold(hex.EncodeToString(data[:4]), hex.EncodeToString(method.ID)) {
		return errors.New("duplicate voluntary-conviction transaction does not call addConviction")
	}
	values, err := method.Inputs.Unpack(data[4:])
	if err != nil || len(values) != 4 {
		return stateMismatchError(err, "decode duplicate addConviction returned %d values", len(values))
	}
	noID, noIDOK := values[0].(*big.Int)
	amount, amountOK := values[1].(*big.Int)
	nonce, nonceOK := values[2].(*big.Int)
	deadline, deadlineOK := values[3].(uint64)
	wantAmount := new(big.Int).SetUint64(cfg.Config.Scenarios.VoluntaryConvictionRao)
	if !noIDOK || !amountOK || !nonceOK || !deadlineOK || noID.Cmp(big.NewInt(1)) != 0 || amount.Cmp(wantAmount) != 0 || nonce.Cmp(event.Nonce) != 0 || receipt.BlockNumber == nil || receipt.BlockNumber.Uint64() > deadline {
		return errors.New("duplicate addConviction calldata differs from its exact finalized event or deadline")
	}
	return nil
}

// Read one uint256 contract result at an exact EVM checkpoint.
func voluntaryConvictionUintAt(ctx context.Context, client *ethclient.Client, address common.Address, parsed abi.ABI, method string, block uint64, args ...any) (uint64, error) {
	values, err := contractCallAt(ctx, client, address, parsed, method, block, args...)
	if err != nil || len(values) != 1 {
		return 0, stateMismatchError(err, "%s at block %d returned %d values", method, block, len(values))
	}
	value, ok := values[0].(*big.Int)
	if !ok || !value.IsUint64() {
		return 0, fmt.Errorf("%s at block %d returned %T", method, block, values[0])
	}
	return value.Uint64(), nil
}

// Authenticate the one recoverable live incident from signed bytes, both
// receipts, journal lineage, and exact historical contract state.
func detectVoluntaryConvictionDuplicateRecovery(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry, client *ethclient.Client, signed *ethTypes.Transaction, transaction planRevisionTransaction) (voluntaryConvictionDuplicateRecovery, error) {
	if transaction.ActionID != voluntaryConvictionActionID || transaction.BlockNumber == 0 || transaction.BlockHash == "" {
		return voluntaryConvictionDuplicateRecovery{}, errors.New("successful unverified EVM transaction is not a recoverable voluntary conviction")
	}
	duplicatePlan, err := loadVoluntaryConvictionLineagePlan(stateDir, prior, transaction.PlanHash)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	duplicateAction, err := exactVoluntaryConvictionPlanAction(duplicatePlan, transaction.IntentHash)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	var originalEvidence VoluntaryConvictionEvidence
	if err := readJSONFile(filepath.Join(stateDir, "public", "voluntary-conviction.json"), &originalEvidence); err != nil {
		return voluntaryConvictionDuplicateRecovery{}, fmt.Errorf("read original voluntary-conviction evidence: %w", err)
	}
	var originalFinalized JournalEntry
	foundFinalized, foundVerified := false, false
	for _, entry := range entries {
		if !prior.allowedPlanHashes()[entry.PlanHash] || entry.ActionID != voluntaryConvictionActionID {
			continue
		}
		if entry.Stage == StageFinalized && strings.EqualFold(entry.TransactionHash, originalEvidence.TransactionHash) && entry.BlockNumber == originalEvidence.FinalizedBlock && strings.EqualFold(entry.BlockHash, originalEvidence.FinalizedHash) {
			originalFinalized, foundFinalized = entry, true
		}
	}
	if foundFinalized {
		for _, entry := range entries {
			if entry.PlanHash == originalFinalized.PlanHash && entry.ActionID == originalFinalized.ActionID && entry.IntentHash == originalFinalized.IntentHash && entry.Stage == StageVerified {
				foundVerified = true
			}
		}
	}
	if !foundFinalized || !foundVerified {
		return voluntaryConvictionDuplicateRecovery{}, errors.New("original voluntary conviction lacks exact finalized and verified journal evidence")
	}
	originalPlan, err := loadVoluntaryConvictionLineagePlan(stateDir, prior, originalFinalized.PlanHash)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	if err := voluntaryConvictionEvidenceMatches(cfg, originalPlan, originalEvidence); err != nil {
		return voluntaryConvictionDuplicateRecovery{}, fmt.Errorf("original voluntary-conviction evidence: %w", err)
	}
	if !strings.EqualFold(duplicatePlan.PolicyHash, originalPlan.PolicyHash) {
		return voluntaryConvictionDuplicateRecovery{}, errors.New("duplicate voluntary-conviction plan changed the historical policy")
	}
	originalAction, err := exactVoluntaryConvictionPlanAction(originalPlan, originalFinalized.IntentHash)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	duplicateReceipt, err := verifyFinalizedEVMReceipt(ctx, client, head, transaction.TransactionHash, transaction.BlockNumber, transaction.BlockHash)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, fmt.Errorf("duplicate voluntary-conviction receipt: %w", err)
	}
	originalReceipt, err := verifyFinalizedEVMReceipt(ctx, client, head, originalEvidence.TransactionHash, originalEvidence.FinalizedBlock, originalEvidence.FinalizedHash)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, fmt.Errorf("original voluntary-conviction receipt: %w", err)
	}
	if err := voluntaryConvictionReceiptMatches(originalReceipt, prior.Deployment.CoordinatorProxy, originalEvidence); err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	coordinatorABI, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	event, err := decodeVoluntaryConvictionEvent(coordinatorABI, duplicateReceipt, prior.Deployment.CoordinatorProxy, 1)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	if err := validateDuplicateVoluntaryConvictionTransaction(cfg, duplicatePlan, duplicateAction, signed, duplicateReceipt, event); err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	wantAmount := cfg.Config.Scenarios.VoluntaryConvictionRao
	wantAfter, ok := checkedMul(wantAmount, 2)
	if !ok || !event.Amount.IsUint64() || event.Amount.Uint64() != wantAmount || !event.Nonce.IsUint64() || !strings.EqualFold(event.Funder.Hex(), prior.Roles.OperatorDepositSigners[0]) || !strings.EqualFold("0x"+hex.EncodeToString(event.PolicyHash[:]), duplicatePlan.PolicyHash) {
		return voluntaryConvictionDuplicateRecovery{}, errors.New("duplicate ConvictionAdded event differs from the approved amount, signer, policy, or nonce")
	}
	priorRecovery, alreadyPlanned, recoveryVerified, err := priorVoluntaryConvictionRecovery(prior, entries, transaction)
	if err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	before, after, principalAfter := wantAmount, wantAfter, wantAfter
	if !recoveryVerified {
		if transaction.BlockNumber <= 1 {
			return voluntaryConvictionDuplicateRecovery{}, errors.New("duplicate conviction block has no historical parent")
		}
		before, err = voluntaryConvictionUintAt(ctx, client, prior.Deployment.CoordinatorProxy, coordinatorABI, "cumulativeConviction", transaction.BlockNumber-1, big.NewInt(1))
		if err != nil {
			return voluntaryConvictionDuplicateRecovery{}, err
		}
		after, err = voluntaryConvictionUintAt(ctx, client, prior.Deployment.CoordinatorProxy, coordinatorABI, "cumulativeConviction", transaction.BlockNumber, big.NewInt(1))
		if err != nil {
			return voluntaryConvictionDuplicateRecovery{}, err
		}
		reserveABI, parseErr := abi.JSON(strings.NewReader(ReserveSinkABI))
		if parseErr != nil {
			return voluntaryConvictionDuplicateRecovery{}, parseErr
		}
		principalAfter, err = voluntaryConvictionUintAt(ctx, client, prior.Deployment.ReserveSink, reserveABI, "operatorPrincipal", transaction.BlockNumber, big.NewInt(1))
		if err != nil {
			return voluntaryConvictionDuplicateRecovery{}, err
		}
	} else if priorRecovery.Parameters[voluntaryRecoveryCumulativeAfterParameter] != strconv.FormatUint(wantAfter, 10) {
		return voluntaryConvictionDuplicateRecovery{}, errors.New("verified duplicate recovery does not retain the exact cumulative bound")
	}
	current, err := voluntaryConvictionUintAt(ctx, client, prior.Deployment.CoordinatorProxy, coordinatorABI, "cumulativeConviction", head.Number, big.NewInt(1))
	if err != nil || current < wantAfter {
		return voluntaryConvictionDuplicateRecovery{}, stateMismatchError(err, "current cumulative conviction=%d want>=%d", current, wantAfter)
	}
	if !recoveryVerified && current != wantAfter {
		return voluntaryConvictionDuplicateRecovery{}, fmt.Errorf("unreconciled cumulative conviction=%d, want exactly %d", current, wantAfter)
	}
	supersededBefore := prior.SupersededSpend.EVMGasWei
	if alreadyPlanned {
		supersededBefore = DecimalUint(priorRecovery.Parameters[voluntaryRecoverySupersededGasBeforeParameter])
	}
	recovery := voluntaryConvictionDuplicateRecovery{
		DuplicateTransaction: transaction, DuplicateAction: duplicateAction, OriginalAction: originalAction,
		OriginalPlanHash: originalFinalized.PlanHash, OriginalIntentHash: originalFinalized.IntentHash,
		OriginalPlanPolicyHash: originalPlan.PolicyHash, DuplicatePlanPolicyHash: duplicatePlan.PolicyHash, OriginalEvidence: originalEvidence,
		DuplicateEpoch: event.Epoch.Uint64(), DuplicateNonce: event.Nonce.String(), Funder: event.Funder.Hex(),
		PolicyHash: "0x" + hex.EncodeToString(event.PolicyHash[:]), AmountRao: wantAmount,
		CumulativeBeforeRao: before, CumulativeAfterRao: after, OperatorPrincipalAfterRao: principalAfter,
		SupersededGasBefore: supersededBefore, AlreadyPlanned: alreadyPlanned,
	}
	if err := validateVoluntaryConvictionDuplicateRecovery(cfg, recovery); err != nil {
		return voluntaryConvictionDuplicateRecovery{}, err
	}
	return recovery, nil
}

// Derive the exact cumulative gas ceiling represented by the recovered
// duplicate. Maximum approval, not actual gas used, is carried forward.
func voluntaryConvictionRecoveryGasAfter(recovery voluntaryConvictionDuplicateRecovery) (DecimalUint, error) {
	return addDecimalUint(recovery.SupersededGasBefore, recovery.DuplicateAction.Spend.EVMGasWei)
}

// Derive a transfer that restores the post-voluntary campaign floor after the
// accidental extra reserve call. One destination-share rao is added to the
// call's two source/runtime rounding allowances.
func voluntaryConvictionRecoveryAlphaTerms(cfg *ResolvedConfig, plan *SetupPlan) (uint64, uint64, uint64, error) {
	if cfg == nil || plan == nil {
		return 0, 0, 0, errors.New("voluntary-conviction alpha recovery plan is unavailable")
	}
	var transfer *Action
	for index := range plan.Actions {
		if plan.Actions[index].ID == "alpha.transfer.operator-deposit.1" {
			transfer = &plan.Actions[index]
			break
		}
	}
	if transfer == nil {
		return 0, 0, 0, errors.New("operator-1 campaign transfer is unavailable")
	}
	campaign, campaignErr := strconv.ParseUint(transfer.Parameters["campaign_requirement_rao"], 10, 64)
	reserveCalls, callsErr := strconv.ParseUint(transfer.Parameters["reserve_calls"], 10, 64)
	minimumTransfer, minimumErr := strconv.ParseUint(transfer.Parameters["minimum_alpha_at_approved_price_rao"], 10, 64)
	amount := cfg.Config.Scenarios.VoluntaryConvictionRao
	if campaignErr != nil || callsErr != nil || minimumErr != nil || campaign < amount || reserveCalls == 0 || minimumTransfer == 0 {
		return 0, 0, 0, errors.New("operator-1 campaign transfer lacks exact recovery economics")
	}
	remainingAllowance, ok := checkedMul(reserveCalls-1, reserveRoundingAllowancePerCallRao)
	if !ok {
		return 0, 0, 0, errors.New("voluntary-conviction remaining reserve allowance overflows")
	}
	minimumDestination, ok := checkedAdd(campaign-amount, remainingAllowance)
	if !ok || minimumDestination == 0 {
		return 0, 0, 0, errors.New("voluntary-conviction recovery destination overflows")
	}
	repairAmount, ok := checkedAdd(amount, reserveRoundingAllowancePerCallRao)
	if ok {
		repairAmount, ok = checkedAdd(repairAmount, alphaTransferDestinationRoundingAllowance)
	}
	if !ok || repairAmount < minimumTransfer {
		return 0, 0, 0, errors.New("voluntary-conviction repair is below the runtime transfer minimum")
	}
	return repairAmount, minimumDestination, minimumTransfer, nil
}

// Allocate the next deterministic repair sequence without colliding with a
// prior runtime-envelope repair.
func nextVoluntaryConvictionRepairActionID(plans ...*SetupPlan) (string, error) {
	maximumSequence := 1
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		for _, action := range plan.Actions {
			if !strings.HasPrefix(action.ID, "alpha.repair.operator-deposit.") {
				continue
			}
			_, index, err := alphaTransferTargetFromActionID(action.ID)
			if err != nil {
				return "", stateMismatchError(err, "invalid operator repair action %s", action.ID)
			}
			if index != 1 {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(action.ID, "alpha.repair.operator-deposit."), ".")
			sequence := 1
			if len(parts) == 2 {
				sequence, err = strconv.Atoi(parts[1])
				if err != nil {
					return "", err
				}
			}
			if sequence > maximumSequence {
				maximumSequence = sequence
			}
		}
	}
	return fmt.Sprintf("alpha.repair.operator-deposit.1.%d", maximumSequence+1), nil
}

// Load a previously verified duplicate-conviction repair without rebuilding it
// under a later policy or transfer snapshot.
func authenticatedPriorVoluntaryConvictionRepair(prior *SetupPlan, entries []JournalEntry, reconciliation Action) (Action, error) {
	if prior == nil || reconciliation.ID != voluntaryConvictionReconciliationActionID {
		return Action{}, errors.New("prior voluntary-conviction repair context is unavailable")
	}
	repairID := reconciliation.Parameters[voluntaryRecoveryRepairActionParameter]
	amount, amountErr := strconv.ParseUint(reconciliation.Parameters[voluntaryRecoveryAmountParameter], 10, 64)
	wantAmount, addOK := checkedAdd(amount, reserveRoundingAllowancePerCallRao)
	if addOK {
		wantAmount, addOK = checkedAdd(wantAmount, alphaTransferDestinationRoundingAllowance)
	}
	if repairID == "" || amountErr != nil || amount == 0 || !addOK {
		return Action{}, errors.New("prior voluntary-conviction repair identity or amount is invalid")
	}
	var repair *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID != repairID {
			continue
		}
		if repair != nil {
			return Action{}, errors.New("prior voluntary-conviction recovery has duplicate repair actions")
		}
		copy := prior.Actions[index]
		repair = &copy
	}
	if repair == nil || repair.Kind != "substrate-extrinsic" || repair.Target != reconciliation.Target || repair.Spend.AlphaRao != wantAmount || repair.Spend.TAORao != 0 ||
		!repair.Spend.EVMGasWei.IsZero() || repair.Spend.Registrations != 0 || repair.Spend.SubnetCreations != 0 ||
		repair.Parameters[alphaRepairForActionParameter] != reconciliation.ID || !strings.EqualFold(repair.Parameters["campaign_policy_hash"], reconciliation.Parameters[voluntaryRecoveryPolicyHashParameter]) ||
		!strings.EqualFold(repair.Parameters[deploymentManifestHashParameter], reconciliation.Parameters[deploymentManifestHashParameter]) || !slices.Equal(repair.DependsOn, []string{reconciliation.ID}) {
		return Action{}, errors.New("prior voluntary-conviction repair does not bind its exact historical recovery")
	}
	intent, err := actionIntentHash(*repair)
	if err != nil || intent != repair.IntentHash {
		return Action{}, stateMismatchError(err, "prior voluntary-conviction repair intent is invalid")
	}
	verified := false
	for _, entry := range entries {
		if prior.allowedPlanHashes()[entry.PlanHash] && entry.ActionID == repair.ID && entry.IntentHash == repair.IntentHash && entry.Stage == StageVerified {
			verified = true
		}
	}
	if !verified {
		return Action{}, errors.New("prior voluntary-conviction repair lacks exact verified journal evidence")
	}
	return *repair, nil
}

// Restore the exact verified custody repair needed by the historical original
// conviction before that one-shot action replaces the fresh representation.
func carryVerifiedVoluntaryConvictionCustodyDependency(revised, prior *SetupPlan, entries []JournalEntry, original Action) error {
	if revised == nil || prior == nil || original.ID != voluntaryConvictionActionID {
		return errors.New("voluntary-conviction custody dependency context is unavailable")
	}
	existing := make(map[string]bool, len(revised.Actions))
	for _, action := range revised.Actions {
		existing[action.ID] = true
	}
	missing := ""
	for _, dependency := range original.DependsOn {
		if existing[dependency] {
			continue
		}
		if missing != "" {
			return errors.New("voluntary conviction has multiple missing historical custody dependencies")
		}
		missing = dependency
	}
	if missing == "" {
		return nil
	}
	kind, operator, err := alphaTransferTargetFromActionID(missing)
	if err != nil || kind != "operator-deposit" || operator != 1 || !strings.HasPrefix(missing, "alpha.repair.operator-deposit.1") {
		return stateMismatchError(err, "missing voluntary-conviction dependency %q is not a bounded operator-1 repair", missing)
	}
	var repair *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID != missing {
			continue
		}
		if repair != nil {
			return errors.New("prior plan has duplicate voluntary-conviction dependency repairs")
		}
		copy := prior.Actions[index]
		repair = &copy
	}
	if repair == nil || repair.Kind != "substrate-extrinsic" || repair.Spend.AlphaRao == 0 || repair.Parameters[alphaRepairForActionParameter] != "alpha.transfer.operator-deposit.1" ||
		!slices.Equal(repair.DependsOn, []string{"alpha.transfer.operator-deposit.1"}) {
		return errors.New("missing voluntary-conviction dependency is not the exact historical custody repair")
	}
	intent, err := actionIntentHash(*repair)
	if err != nil || intent != repair.IntentHash {
		return stateMismatchError(err, "historical voluntary-conviction repair %s intent is invalid", repair.ID)
	}
	verified := false
	for _, entry := range entries {
		if prior.allowedPlanHashes()[entry.PlanHash] && entry.ActionID == repair.ID && entry.IntentHash == repair.IntentHash && entry.Stage == StageVerified {
			verified = true
		}
	}
	if !verified {
		return errors.New("missing voluntary-conviction dependency lacks exact verified journal evidence")
	}
	baseIndex := -1
	for index := range revised.Actions {
		if revised.Actions[index].ID == repair.DependsOn[0] {
			baseIndex = index
			break
		}
	}
	if baseIndex < 0 {
		return errors.New("fresh plan lacks the voluntary-conviction repair base transfer")
	}
	revised.Actions = append(revised.Actions[:baseIndex+1], append([]Action{*repair}, revised.Actions[baseIndex+1:]...)...)
	return nil
}

// Insert the no-broadcast reconciliation, one bounded alpha repair, and a
// barrier on the first unverified fleet-setup mutation. Already verified
// native commitment intents are left byte-for-byte unchanged.
func applyVoluntaryConvictionDuplicateRecovery(cfg *ResolvedConfig, revised, prior *SetupPlan, entries []JournalEntry, recovery voluntaryConvictionDuplicateRecovery) error {
	if revised == nil || prior == nil {
		return errors.New("voluntary-conviction recovery plans are unavailable")
	}
	if err := validateVoluntaryConvictionDuplicateRecovery(cfg, recovery); err != nil {
		return err
	}
	gasAfter, err := voluntaryConvictionRecoveryGasAfter(recovery)
	if err != nil {
		return err
	}
	repairID, err := nextVoluntaryConvictionRepairActionID(revised, prior)
	if err != nil {
		return err
	}
	priorRecovery, priorPlanned, _, err := priorVoluntaryConvictionRecovery(prior, entries, recovery.DuplicateTransaction)
	if err != nil {
		return err
	}
	if priorPlanned {
		repairID = priorRecovery.Parameters[voluntaryRecoveryRepairActionParameter]
		if repairID == "" {
			return errors.New("prior voluntary-conviction recovery has no repair action")
		}
	}
	parameters := map[string]string{
		deploymentManifestHashParameter:                  recovery.DuplicateAction.Parameters[deploymentManifestHashParameter],
		voluntaryRecoveryDuplicatePlanHashParameter:      recovery.DuplicateTransaction.PlanHash,
		voluntaryRecoveryDuplicateIntentHashParameter:    recovery.DuplicateTransaction.IntentHash,
		voluntaryRecoveryDuplicateTransactionParameter:   recovery.DuplicateTransaction.TransactionHash,
		voluntaryRecoveryDuplicateBlockParameter:         strconv.FormatUint(recovery.DuplicateTransaction.BlockNumber, 10),
		voluntaryRecoveryDuplicateBlockHashParameter:     recovery.DuplicateTransaction.BlockHash,
		voluntaryRecoveryDuplicateEpochParameter:         strconv.FormatUint(recovery.DuplicateEpoch, 10),
		voluntaryRecoveryDuplicateNonceParameter:         recovery.DuplicateNonce,
		voluntaryRecoveryOriginalPlanHashParameter:       recovery.OriginalPlanHash,
		voluntaryRecoveryOriginalIntentHashParameter:     recovery.OriginalIntentHash,
		voluntaryRecoveryOriginalTransactionParameter:    recovery.OriginalEvidence.TransactionHash,
		voluntaryRecoveryOriginalBlockParameter:          strconv.FormatUint(recovery.OriginalEvidence.FinalizedBlock, 10),
		voluntaryRecoveryOriginalBlockHashParameter:      recovery.OriginalEvidence.FinalizedHash,
		voluntaryRecoveryOriginalEpochParameter:          strconv.FormatUint(recovery.OriginalEvidence.Epoch, 10),
		voluntaryRecoveryOriginalNonceParameter:          recovery.OriginalEvidence.Nonce,
		voluntaryRecoveryAmountParameter:                 strconv.FormatUint(recovery.AmountRao, 10),
		voluntaryRecoveryCumulativeBeforeParameter:       strconv.FormatUint(recovery.CumulativeBeforeRao, 10),
		voluntaryRecoveryCumulativeAfterParameter:        strconv.FormatUint(recovery.CumulativeAfterRao, 10),
		voluntaryRecoveryOperatorPrincipalAfterParameter: strconv.FormatUint(recovery.OperatorPrincipalAfterRao, 10),
		voluntaryRecoveryFunderParameter:                 recovery.Funder,
		voluntaryRecoveryPolicyHashParameter:             recovery.PolicyHash,
		voluntaryRecoverySupersededGasBeforeParameter:    recovery.SupersededGasBefore.String(),
		voluntaryRecoveryDuplicateMaximumGasParameter:    recovery.DuplicateAction.Spend.EVMGasWei.String(),
		voluntaryRecoverySupersededGasAfterParameter:     gasAfter.String(),
		voluntaryRecoveryRepairActionParameter:           repairID,
	}
	reconciliation := Action{
		ID: voluntaryConvictionReconciliationActionID, Kind: "evm-reconciliation", Target: revised.Roles.OperatorDepositSigners[0],
		Description: "reconcile one exact finalized duplicate voluntary conviction without broadcasting another transaction",
		Parameters:  parameters, DependsOn: []string{voluntaryConvictionActionID},
	}
	reconciliation.IntentHash, err = actionIntentHash(reconciliation)
	if err != nil {
		return err
	}
	var repair Action
	if priorPlanned {
		if priorRecovery.IntentHash != reconciliation.IntentHash {
			return errors.New("prior voluntary-conviction recovery differs from deterministic reconstruction")
		}
		repair, err = authenticatedPriorVoluntaryConvictionRepair(prior, entries, priorRecovery)
		if err != nil {
			return err
		}
		reconciliation = priorRecovery
	} else {
		repairAmount, minimumDestination, minimumTransfer, termsErr := voluntaryConvictionRecoveryAlphaTerms(cfg, revised)
		if termsErr != nil {
			return termsErr
		}
		repairParameters := alphaTransferActionParameters(repairAmount, 0, minimumTransfer, &revised.LiveFacts, revised.AlphaTransferMarginBPS)
		repairParameters[alphaRepairForActionParameter] = reconciliation.ID
		repairParameters[alphaRepairMinimumDestinationParameter] = strconv.FormatUint(minimumDestination, 10)
		repairParameters["campaign_policy_hash"] = recovery.PolicyHash
		repairParameters[deploymentManifestHashParameter] = parameters[deploymentManifestHashParameter]
		repair = Action{
			ID: repairID, Kind: "substrate-extrinsic", Target: reconciliation.Target,
			Description: "restore the exact operator-1 campaign floor consumed by the reconciled duplicate conviction",
			Parameters:  repairParameters, Spend: Spend{AlphaRao: repairAmount}, DependsOn: []string{reconciliation.ID},
		}
		repair.IntentHash, err = actionIntentHash(repair)
		if err != nil {
			return err
		}
	}
	voluntaryIndex := -1
	for index, action := range revised.Actions {
		if action.ID == reconciliation.ID || action.ID == repair.ID {
			return fmt.Errorf("fresh plan unexpectedly contains recovery action %s", action.ID)
		}
		if action.ID == voluntaryConvictionActionID {
			if voluntaryIndex >= 0 {
				return errors.New("fresh plan has duplicate voluntary convictions")
			}
			voluntaryIndex = index
		}
	}
	if voluntaryIndex < 0 {
		return errors.New("fresh plan has no voluntary conviction to reconcile")
	}
	// Recovery authenticates the exact original zero-to-one transaction. Keep
	// that action verbatim instead of relying on a later generic gas-ceiling
	// carry pass; rebuilding it would create a new logical intent after the
	// cumulative conviction is already nonzero.
	if err := validateVoluntaryConvictionFreshDependency(revised, prior, entries, recovery.OriginalAction, revised.Actions[voluntaryIndex]); err != nil {
		return err
	}
	if err := carryVerifiedVoluntaryConvictionCustodyDependency(revised, prior, entries, recovery.OriginalAction); err != nil {
		return err
	}
	for index := range revised.Actions {
		if revised.Actions[index].ID == voluntaryConvictionActionID {
			voluntaryIndex = index
			break
		}
	}
	revised.Actions[voluntaryIndex] = recovery.OriginalAction
	revised.Actions = append(revised.Actions[:voluntaryIndex+1], append([]Action{reconciliation, repair}, revised.Actions[voluntaryIndex+1:]...)...)
	barrierID := "fleet.mirror.1"
	for _, action := range revised.Actions {
		if action.ID == "fleet.refresh.deploy-batcher" {
			barrierID = action.ID
			break
		}
	}
	foundBarrier := false
	for index := range revised.Actions {
		action := &revised.Actions[index]
		if action.ID == barrierID {
			if !slices.Contains(action.DependsOn, repair.ID) {
				action.DependsOn = append(action.DependsOn, repair.ID)
			}
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				return err
			}
			foundBarrier = true
		}
	}
	if !foundBarrier {
		return errors.New("fresh plan has no fleet-setup recovery barrier")
	}
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	return err
}

// Validate the complete immutable envelope of the no-broadcast action. The
// source plan hashes are already authenticated by the enclosing plan lineage.
func validateVoluntaryConvictionReconciliationAction(plan *SetupPlan, action Action, priorPlanHashes map[string]bool) (string, error) {
	if plan == nil || action.ID != voluntaryConvictionReconciliationActionID || action.Kind != "evm-reconciliation" || len(plan.Roles.OperatorDepositSigners) == 0 {
		return "", errors.New("voluntary-conviction reconciliation identity is invalid")
	}
	if !strings.EqualFold(action.Target, plan.Roles.OperatorDepositSigners[0]) || !spendIsZero(action.Spend) || !slices.Equal(action.DependsOn, []string{voluntaryConvictionActionID}) ||
		action.Parameters[deploymentManifestHashParameter] == "" || !strings.EqualFold(action.Parameters[voluntaryRecoveryFunderParameter], plan.Roles.OperatorDepositSigners[0]) {
		return "", errors.New("voluntary-conviction reconciliation target, spend, dependency, or signer is invalid")
	}
	if _, err := decodeHex32(voluntaryRecoveryPolicyHashParameter, action.Parameters[voluntaryRecoveryPolicyHashParameter]); err != nil {
		return "", err
	}
	for _, field := range []string{
		voluntaryRecoveryDuplicatePlanHashParameter, voluntaryRecoveryDuplicateIntentHashParameter, voluntaryRecoveryDuplicateTransactionParameter,
		voluntaryRecoveryDuplicateBlockHashParameter, voluntaryRecoveryOriginalPlanHashParameter, voluntaryRecoveryOriginalIntentHashParameter,
		voluntaryRecoveryOriginalTransactionParameter, voluntaryRecoveryOriginalBlockHashParameter,
	} {
		if _, err := decodeHex32(field, action.Parameters[field]); err != nil {
			return "", err
		}
	}
	if !priorPlanHashes[action.Parameters[voluntaryRecoveryDuplicatePlanHashParameter]] || !priorPlanHashes[action.Parameters[voluntaryRecoveryOriginalPlanHashParameter]] {
		return "", errors.New("voluntary-conviction reconciliation references a foreign source plan")
	}
	duplicateBlock, duplicateBlockErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryDuplicateBlockParameter], 10, 64)
	originalBlock, originalBlockErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryOriginalBlockParameter], 10, 64)
	duplicateEpoch, duplicateEpochErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryDuplicateEpochParameter], 10, 64)
	originalEpoch, originalEpochErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryOriginalEpochParameter], 10, 64)
	amount, amountErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryAmountParameter], 10, 64)
	before, beforeErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryCumulativeBeforeParameter], 10, 64)
	after, afterErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryCumulativeAfterParameter], 10, 64)
	principalAfter, principalErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryOperatorPrincipalAfterParameter], 10, 64)
	originalNonce, originalNonceOK := new(big.Int).SetString(action.Parameters[voluntaryRecoveryOriginalNonceParameter], 10)
	duplicateNonce, duplicateNonceOK := new(big.Int).SetString(action.Parameters[voluntaryRecoveryDuplicateNonceParameter], 10)
	wantAfter, multiplyOK := checkedMul(amount, 2)
	if duplicateBlockErr != nil || originalBlockErr != nil || duplicateEpochErr != nil || originalEpochErr != nil || amountErr != nil || beforeErr != nil || afterErr != nil || principalErr != nil ||
		duplicateBlock == 0 || originalBlock == 0 || originalBlock >= duplicateBlock || originalEpoch > duplicateEpoch || amount == 0 || before != amount || !multiplyOK || after != wantAfter || principalAfter != wantAfter ||
		!originalNonceOK || !duplicateNonceOK || originalNonce.Sign() < 0 || duplicateNonce.Cmp(new(big.Int).Add(originalNonce, big.NewInt(1))) != 0 {
		return "", errors.New("voluntary-conviction reconciliation transition, checkpoint, epoch, or nonce is invalid")
	}
	var voluntary *Action
	for index := range plan.Actions {
		if plan.Actions[index].ID == voluntaryConvictionActionID {
			voluntary = &plan.Actions[index]
			break
		}
	}
	if voluntary == nil || voluntary.Parameters["no_id"] != "1" || voluntary.Parameters["amount_rao"] != strconv.FormatUint(amount, 10) ||
		voluntary.Parameters[deploymentManifestHashParameter] != action.Parameters[deploymentManifestHashParameter] {
		return "", errors.New("voluntary-conviction reconciliation differs from the active original action")
	}
	if plan.Schema == currentSetupPlanSchema && voluntary.IntentHash != action.Parameters[voluntaryRecoveryOriginalIntentHashParameter] {
		return "", errors.New("voluntary-conviction reconciliation does not retain the authenticated original intent")
	}
	gasBefore, beforeGasErr := parseDecimalUint(action.Parameters[voluntaryRecoverySupersededGasBeforeParameter])
	duplicateGas, duplicateGasErr := parseDecimalUint(action.Parameters[voluntaryRecoveryDuplicateMaximumGasParameter])
	gasAfter, afterGasErr := parseDecimalUint(action.Parameters[voluntaryRecoverySupersededGasAfterParameter])
	computedAfter, addErr := addDecimalUint(gasBefore, duplicateGas)
	comparison, compareErr := plan.SupersededSpend.EVMGasWei.Cmp(gasAfter)
	if beforeGasErr != nil || duplicateGasErr != nil || afterGasErr != nil || addErr != nil || duplicateGas.IsZero() || computedAfter != gasAfter || compareErr != nil || comparison < 0 {
		return "", errors.New("voluntary-conviction reconciliation does not bind cumulative duplicate gas approval")
	}
	repairID := action.Parameters[voluntaryRecoveryRepairActionParameter]
	if repairID == "" {
		return "", errors.New("voluntary-conviction reconciliation has no repair barrier")
	}
	return repairID, nil
}

// Require both the successful duplicate checkpoint and the verified original
// checkpoint from the exact plan/action intents bound into the reconciliation.
func hasVoluntaryConvictionReconciliationJournalEvidence(plan *SetupPlan, action Action, entries []JournalEntry) bool {
	if plan == nil || action.ID != voluntaryConvictionReconciliationActionID {
		return false
	}
	duplicateBlock, duplicateErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryDuplicateBlockParameter], 10, 64)
	originalBlock, originalErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryOriginalBlockParameter], 10, 64)
	if duplicateErr != nil || originalErr != nil || duplicateBlock == 0 || originalBlock == 0 {
		return false
	}
	duplicateFinalized, originalFinalized, originalVerified := false, false, false
	for _, entry := range entries {
		if !plan.allowedPlanHashes()[entry.PlanHash] || entry.ActionID != voluntaryConvictionActionID {
			continue
		}
		if entry.PlanHash == action.Parameters[voluntaryRecoveryDuplicatePlanHashParameter] && entry.IntentHash == action.Parameters[voluntaryRecoveryDuplicateIntentHashParameter] && entry.Stage == StageFinalized &&
			strings.EqualFold(entry.TransactionHash, action.Parameters[voluntaryRecoveryDuplicateTransactionParameter]) && entry.BlockNumber == duplicateBlock && strings.EqualFold(entry.BlockHash, action.Parameters[voluntaryRecoveryDuplicateBlockHashParameter]) {
			duplicateFinalized = true
		}
		if entry.PlanHash == action.Parameters[voluntaryRecoveryOriginalPlanHashParameter] && entry.IntentHash == action.Parameters[voluntaryRecoveryOriginalIntentHashParameter] {
			if entry.Stage == StageFinalized && strings.EqualFold(entry.TransactionHash, action.Parameters[voluntaryRecoveryOriginalTransactionParameter]) && entry.BlockNumber == originalBlock && strings.EqualFold(entry.BlockHash, action.Parameters[voluntaryRecoveryOriginalBlockHashParameter]) {
				originalFinalized = true
			}
			originalVerified = originalVerified || entry.Stage == StageVerified
		}
	}
	return duplicateFinalized && originalFinalized && originalVerified
}

// Re-observe both canonical receipts and the monotonic live state. Exact
// one-to-two values are bound in the action; later campaign deposits may only
// increase reserve principal, never invalidate the historical recovery.
func (e *Executor) verifyVoluntaryConvictionReconciliationPostState(ctx context.Context, action Action, head ChainHead, state map[string]any) (map[string]any, error) {
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	if !hasVoluntaryConvictionReconciliationJournalEvidence(e.plan, action, e.journal.Entries()) {
		return nil, errors.New("voluntary-conviction reconciliation lacks exact ancestor journal evidence")
	}
	var original VoluntaryConvictionEvidence
	if err := readJSONFile(filepath.Join(e.stateDir, "public", "voluntary-conviction.json"), &original); err != nil {
		return nil, err
	}
	originalPlan, err := loadVoluntaryConvictionLineagePlan(e.stateDir, e.plan, action.Parameters[voluntaryRecoveryOriginalPlanHashParameter])
	if err != nil {
		return nil, err
	}
	duplicatePlan, err := loadVoluntaryConvictionLineagePlan(e.stateDir, e.plan, action.Parameters[voluntaryRecoveryDuplicatePlanHashParameter])
	if err != nil {
		return nil, err
	}
	historicalPolicyHash := action.Parameters[voluntaryRecoveryPolicyHashParameter]
	if !strings.EqualFold(originalPlan.PolicyHash, historicalPolicyHash) || !strings.EqualFold(duplicatePlan.PolicyHash, historicalPolicyHash) {
		return nil, errors.New("voluntary-conviction reconciliation policy differs from its source plans")
	}
	if err := voluntaryConvictionEvidenceMatches(e.cfg, originalPlan, original); err != nil {
		return nil, err
	}
	if original.TransactionHash != action.Parameters[voluntaryRecoveryOriginalTransactionParameter] || original.FinalizedHash != action.Parameters[voluntaryRecoveryOriginalBlockHashParameter] ||
		strconv.FormatUint(original.FinalizedBlock, 10) != action.Parameters[voluntaryRecoveryOriginalBlockParameter] || strconv.FormatUint(original.Epoch, 10) != action.Parameters[voluntaryRecoveryOriginalEpochParameter] ||
		original.Nonce != action.Parameters[voluntaryRecoveryOriginalNonceParameter] {
		return nil, errors.New("persisted original voluntary-conviction evidence differs from reconciliation")
	}
	client := e.deployer.client
	originalReceipt, err := verifyFinalizedEVMReceipt(ctx, client, head, original.TransactionHash, original.FinalizedBlock, original.FinalizedHash)
	if err != nil {
		return nil, err
	}
	coordinator := e.payloads.Manifest.CoordinatorProxy
	if err := voluntaryConvictionReceiptMatches(originalReceipt, coordinator, original); err != nil {
		return nil, err
	}
	duplicateBlock, _ := strconv.ParseUint(action.Parameters[voluntaryRecoveryDuplicateBlockParameter], 10, 64)
	duplicateEpoch, _ := strconv.ParseUint(action.Parameters[voluntaryRecoveryDuplicateEpochParameter], 10, 64)
	duplicateReceipt, err := verifyFinalizedEVMReceipt(ctx, client, head, action.Parameters[voluntaryRecoveryDuplicateTransactionParameter], duplicateBlock, action.Parameters[voluntaryRecoveryDuplicateBlockHashParameter])
	if err != nil {
		return nil, err
	}
	duplicate := VoluntaryConvictionEvidence{
		Schema: "urnetwork-voluntary-conviction-evidence-v1", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
		NoID: 1, Epoch: duplicateEpoch, AmountRao: action.Parameters[voluntaryRecoveryAmountParameter],
		BeforeConvictionRao: action.Parameters[voluntaryRecoveryCumulativeBeforeParameter], AfterConvictionRao: action.Parameters[voluntaryRecoveryCumulativeAfterParameter],
		Nonce: action.Parameters[voluntaryRecoveryDuplicateNonceParameter], Funder: action.Parameters[voluntaryRecoveryFunderParameter], PolicyHash: action.Parameters[voluntaryRecoveryPolicyHashParameter],
		TransactionHash: action.Parameters[voluntaryRecoveryDuplicateTransactionParameter], FinalizedBlock: duplicateBlock, FinalizedHash: action.Parameters[voluntaryRecoveryDuplicateBlockHashParameter],
	}
	if err := voluntaryConvictionReceiptMatches(duplicateReceipt, coordinator, duplicate); err != nil {
		return nil, err
	}
	coordinatorABI, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, err
	}
	cumulative, err := voluntaryConvictionUintAt(ctx, client, coordinator, coordinatorABI, "cumulativeConviction", head.Number, big.NewInt(1))
	wantCumulative, parseErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryCumulativeAfterParameter], 10, 64)
	if err != nil || parseErr != nil || cumulative < wantCumulative {
		return nil, stateMismatchError(errors.Join(err, parseErr), "reconciled cumulative conviction=%d want>=%d", cumulative, wantCumulative)
	}
	reserveABI, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		return nil, err
	}
	principal, err := voluntaryConvictionUintAt(ctx, client, e.payloads.Manifest.ReserveSink, reserveABI, "operatorPrincipal", head.Number, big.NewInt(1))
	wantPrincipal, parseErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryOperatorPrincipalAfterParameter], 10, 64)
	if err != nil || parseErr != nil || principal < wantPrincipal {
		return nil, stateMismatchError(errors.Join(err, parseErr), "reconciled operator principal=%d want>=%d", principal, wantPrincipal)
	}
	state["original_transaction_hash"] = original.TransactionHash
	state["duplicate_transaction_hash"] = duplicate.TransactionHash
	state["original_block"] = original.FinalizedBlock
	state["duplicate_block"] = duplicate.FinalizedBlock
	state["amount_rao"] = duplicate.AmountRao
	state["cumulative_conviction_rao"] = strconv.FormatUint(cumulative, 10)
	state["operator_principal_rao"] = strconv.FormatUint(principal, 10)
	state["repair_action_id"] = action.Parameters[voluntaryRecoveryRepairActionParameter]
	return state, nil
}

// Execute no mutation; the action succeeds only by independently re-proving
// the already-finalized bounded incident.
func (e *Executor) reconcileDuplicateVoluntaryConviction(ctx context.Context, action Action) error {
	head, err := finalizedEVMHead(ctx, e.deployer.client)
	if err != nil {
		return err
	}
	_, err = e.verifyVoluntaryConvictionReconciliationPostState(ctx, action, head, map[string]any{})
	return err
}
