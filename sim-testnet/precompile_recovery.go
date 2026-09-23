// A separately signed recovery authorization bounds extra probe operations
// without replacing the running campaign's setup plan or earlier receipts.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const precompileRecoveryAuthorizationFilename = "public/precompile-recovery-authorization.json"
const precompileRecoveryActionPrefix = "repair.precompile-residual."
const precompileRecoveryMaximumSteps = 8
const precompileRecoveryMaximumReseeds = 2
const precompileRecoveryTopUpRao uint64 = 1_000_000_000
const precompileRecoveryGasUnits uint64 = 500_000

// The source document is hashed and signed by both budget and custody owners.
// An exhausted campaign reserve may gain only its exact approved shortfall.
type PrecompileRecoveryBudget struct {
	Schema                   string                                `json:"schema"`
	PlanHash                 string                                `json:"plan_hash"`
	JournalHash              string                                `json:"journal_hash"`
	CampaignReserveWei       DecimalUint                           `json:"campaign_reserve_wei"`
	CommittedOrPendingMaxWei DecimalUint                           `json:"committed_or_pending_max_wei"`
	AvailableWei             DecimalUint                           `json:"available_wei"`
	MaximumRecoveryWei       DecimalUint                           `json:"maximum_recovery_wei"`
	MaximumReseedWei         DecimalUint                           `json:"maximum_reseed_wei"`
	Verified                 bool                                  `json:"verified"`
	Supplemental             *PrecompileRecoverySupplementalBudget `json:"supplemental,omitempty"`
}

// The immutable basis stops authorization from migrating to another probe,
// snapshot or round trip. Dividend maturity is checked before any operation.
type PrecompileRecoveryRequest struct {
	Schema               string                         `json:"schema"`
	PlanHash             string                         `json:"plan_hash"`
	ConfigHash           string                         `json:"config_hash"`
	DeploymentId         string                         `json:"deployment_id"`
	ChainId              uint64                         `json:"chain_id"`
	GenesisHash          string                         `json:"genesis_hash"`
	Netuid               uint16                         `json:"netuid"`
	Owner                common.Address                 `json:"owner"`
	Deployer             common.Address                 `json:"deployer"`
	Probe                common.Address                 `json:"probe"`
	ProbeRuntimeHash     string                         `json:"probe_runtime_hash"`
	SampleHotkey         string                         `json:"sample_hotkey"`
	MoveHotkey           string                         `json:"move_hotkey"`
	RecoveryColdkey      string                         `json:"recovery_coldkey"`
	BasisHash            string                         `json:"basis_hash"`
	OriginalEvidenceHash string                         `json:"original_evidence_hash"`
	ReseedTaoRao         uint64                         `json:"reseed_tao_rao"`
	MaximumReseeds       uint64                         `json:"maximum_reseeds"`
	TopUpRao             uint64                         `json:"top_up_rao"`
	MaximumSteps         uint64                         `json:"maximum_steps"`
	MaximumGasUnits      uint64                         `json:"maximum_gas_units"`
	MaximumFeePerGasWei  uint64                         `json:"maximum_fee_per_gas_wei"`
	BudgetHash           string                         `json:"budget_hash"`
	Budget               PrecompileRecoveryBudget       `json:"budget"`
	GasRevision          *PrecompileRecoveryGasRevision `json:"gas_revision,omitempty"`
}

type PrecompileRecoveryAuthorization struct {
	Request           PrecompileRecoveryRequest `json:"request"`
	Hash              string                    `json:"hash"`
	OwnerSignature    string                    `json:"owner_signature"`
	DeployerSignature string                    `json:"deployer_signature"`
}

// Quotes are read-only minima, while Move records the actual call-local values.
// Their positive differences are recorded explicitly rather than called rounding.
type PrecompileRecoveryStep struct {
	Operation            string               `json:"operation"`
	Action               Action               `json:"action"`
	QuoteHead            ChainHead            `json:"quote_head"`
	QuoteSourceRao       uint64               `json:"quote_source_rao"`
	QuoteDestinationRao  uint64               `json:"quote_destination_rao"`
	SourceCreditRao      uint64               `json:"source_credit_rao,omitempty"`
	DestinationCreditRao uint64               `json:"destination_credit_rao,omitempty"`
	Move                 PrecompileMoveStep   `json:"move"`
	Seed                 *PrecompileValueStep `json:"seed,omitempty"`
}

type PrecompileRecoveryEvidence struct {
	Authorization                      PrecompileRecoveryAuthorization `json:"authorization"`
	Steps                              []PrecompileRecoveryStep        `json:"steps"`
	SampleTransferQuoteHead            ChainHead                       `json:"sample_transfer_quote_head"`
	SampleTransferSourceQuoteRao       uint64                          `json:"sample_transfer_source_quote_rao"`
	SampleTransferDestinationQuoteRao  uint64                          `json:"sample_transfer_destination_quote_rao"`
	SampleTransferInclusionCreditRao   uint64                          `json:"sample_transfer_inclusion_credit_rao,omitempty"`
	SampleTransferDestinationCreditRao uint64                          `json:"sample_transfer_destination_credit_rao,omitempty"`
	SampleTransferCreditRao            uint64                          `json:"sample_transfer_credit_rao,omitempty"`
}

// Source identity is the existing authenticated conformance record, excluding
// later dividend, transfer and recovery fields which must evolve independently.
func precompileRecoveryBasisHash(evidence *PrecompileConformanceEvidence) (string, error) {
	if evidence == nil {
		return "", errors.New("probe recovery basis is absent")
	}
	copy := *evidence
	copy.Dividend, copy.Transfer = PrecompileDividendStep{}, PrecompileTransferStep{}
	copy.Recovery, copy.Complete, copy.EvidenceHash = nil, false, ""
	return canonicalHashHex(copy)
}

// Checks the self-contained custody signatures and finite operation bounds.
// The live owner additionally binds the budget signer and allowance to its plan.
func validatePrecompileRecoveryAuthorization(evidence *PrecompileConformanceEvidence, authorization *PrecompileRecoveryAuthorization) error {
	if evidence == nil || authorization == nil || !precompileRoundTripAccounted(evidence) {
		return errors.New("probe recovery requires an accounted source round trip")
	}
	r := authorization.Request
	if err := validatePrecompileRecoveryGasRevision(evidence, authorization); err != nil {
		return err
	}
	if !validCanonicalHashHex(r.OriginalEvidenceHash) || r.ReseedTaoRao == 0 || r.MaximumReseeds != precompileRecoveryMaximumReseeds {
		return errors.New("probe recovery has no original evidence or bounded reseed authority")
	}
	basis, err := precompileRecoveryBasisHash(evidence)
	if err != nil || !validCanonicalHashHex(r.PlanHash) || r.ConfigHash != evidence.ConfigHash || r.DeploymentId != evidence.DeploymentID || r.ChainId != evidence.ChainID || r.GenesisHash != evidence.GenesisHash || r.Netuid != evidence.Netuid || r.Deployer != common.HexToAddress(evidence.Owner) || r.Probe != common.HexToAddress(evidence.ProbeAddress) || r.SampleHotkey != evidence.SampleHotkey || r.MoveHotkey != evidence.MoveHotkey || r.RecoveryColdkey != evidence.RecoveryColdkey || r.BasisHash != basis || !validCanonicalHashHex(r.ProbeRuntimeHash) || r.TopUpRao != precompileRecoveryTopUpRao || r.MaximumSteps != precompileRecoveryMaximumSteps || r.MaximumFeePerGasWei == 0 || r.MaximumFeePerGasWei > 100_000_000_000 || !validConformanceTransaction(evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockHash, evidence.Snapshot.BlockNumber) {
		return errors.New("probe recovery changed its source identity or finite bounds")
	}
	budgetHash, err := canonicalHashHex(r.Budget)
	if err != nil || budgetHash != r.BudgetHash || !r.Budget.Verified || r.Budget.PlanHash != r.PlanHash || !validCanonicalHashHex(r.Budget.JournalHash) {
		return errors.New("probe recovery budget identity differs")
	}
	reserved, err := precompileRecoveryEffectiveReserve(r.Budget)
	if err != nil {
		return err
	}
	committed, err := r.Budget.CommittedOrPendingMaxWei.Big()
	if err != nil {
		return err
	}
	available, err := r.Budget.AvailableWei.Big()
	if err != nil {
		return err
	}
	maximum, err := r.Budget.MaximumRecoveryWei.Big()
	if err != nil {
		return err
	}
	want := new(big.Int).Mul(new(big.Int).SetUint64(r.MaximumSteps*r.MaximumGasUnits), new(big.Int).SetUint64(r.MaximumFeePerGasWei))
	reseed, err := r.Budget.MaximumReseedWei.Big()
	if err != nil {
		return err
	}
	wantReseed := new(big.Int).Mul(new(big.Int).SetUint64(r.ReseedTaoRao), new(big.Int).SetUint64(r.MaximumReseeds))
	wantReseed.Mul(wantReseed, big.NewInt(1_000_000_000))
	if reseed.Cmp(wantReseed) != 0 {
		return errors.New("probe recovery reseed budget differs from explicit finite authority")
	}
	want.Add(want, reseed)
	if maximum.Cmp(want) != 0 || new(big.Int).Add(committed, available).Cmp(reserved) != 0 || available.Cmp(maximum) < 0 {
		return errors.New("probe recovery exceeds its explicit reserve suballocation")
	}
	if err := verifyCoordinatorRepairSignature(r, authorization.Hash, authorization.OwnerSignature, r.Owner); err != nil {
		return err
	}
	return verifyCoordinatorRepairSignature(r, authorization.Hash, authorization.DeployerSignature, r.Deployer)
}

// This supplemental authority is confined to its exact retained plan. A later
// plan needs an explicit migration of its remaining reserve and custody proof.
func validatePrecompileRecoveryPlan(plan *SetupPlan, evidence *PrecompileConformanceEvidence, authorization *PrecompileRecoveryAuthorization) error {
	if err := validatePrecompileRecoveryAuthorization(evidence, authorization); err != nil {
		return err
	}
	r := authorization.Request
	if plan == nil || plan.PlanHash != r.PlanHash || !strings.EqualFold(plan.Roles.Owner, r.Owner.Hex()) || !strings.EqualFold(plan.Roles.Deployer, r.Deployer.Hex()) || approvedPrecompileProbe(plan) != r.Probe || plan.MaximumEVMFeePerGasWei < r.MaximumFeePerGasWei || plan.LiveFacts.ProbeTAORao != r.ReseedTaoRao {
		return errors.New("probe recovery is outside the approved plan custody")
	}
	deploy, err := exactPlanActionByID(plan, "precompile.transfer-out")
	if err != nil || deploy.Parameters[precompileProbeRuntimeParameter] != r.ProbeRuntimeHash {
		return errors.New("probe recovery changed the approved immutable runtime")
	}
	if plan.PlanHash == r.PlanHash {
		reserve, err := exactPlanActionByID(plan, "campaign.evm-gas-reserve")
		if err != nil || reserve.Kind != "budget-reserve" || reserve.Spend.EVMGasWei != r.Budget.CampaignReserveWei {
			return errors.New("probe recovery budget differs from the retained campaign reserve")
		}
	}
	if err := validatePrecompileRecoverySupplementalPlan(plan, r.Budget); err != nil {
		return err
	}
	return nil
}

// Dynamic amounts are confined to the two original stake positions. Nonce is
// bound when the existing writer signs, then the exact signed bytes are durable.
func precompileRecoveryAction(authorization *PrecompileRecoveryAuthorization, index int, step PrecompileRecoveryStep) (Action, []byte, error) {
	if authorization == nil || index < 0 || uint64(index) >= authorization.Request.MaximumSteps {
		return Action{}, nil, errors.New("probe recovery operation exceeds its approved count")
	}
	r := authorization.Request
	if r.GasRevision != nil && index == 0 {
		prior := r.GasRevision.Evidence.Recovery
		if prior == nil || len(prior.Steps) != 1 || step.Operation != prior.Steps[0].Operation || step.QuoteHead != prior.Steps[0].QuoteHead || step.QuoteSourceRao != prior.Steps[0].QuoteSourceRao || step.QuoteDestinationRao != prior.Steps[0].QuoteDestinationRao || step.Move.AmountRao != prior.Steps[0].Move.AmountRao {
			return Action{}, nil, errors.New("probe gas revision changed the original unsigned custody operation")
		}
	}
	if (step.Operation == "reseed-sample") != (step.Seed != nil) {
		return Action{}, nil, errors.New("probe recovery step mixes seed and move evidence")
	}
	sample, err := decodeHex32("recovery sample", r.SampleHotkey)
	if err != nil {
		return Action{}, nil, err
	}
	move, err := decodeHex32("recovery move", r.MoveHotkey)
	if err != nil {
		return Action{}, nil, err
	}
	recovery, err := decodeHex32("recovery recipient", r.RecoveryColdkey)
	if err != nil {
		return Action{}, nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		return Action{}, nil, err
	}
	var data []byte
	switch step.Operation {
	case "top-up":
		if step.Move.AmountRao != r.TopUpRao || step.QuoteSourceRao < r.TopUpRao || step.QuoteDestinationRao == 0 {
			return Action{}, nil, errors.New("probe top-up changed its existing-custody scope")
		}
		data, err = parsed.Pack("moveRoundTrip", sample, move, new(big.Int).SetUint64(step.Move.AmountRao))
	case "recover-move", "recover-sample":
		if step.Move.AmountRao == 0 || step.Move.AmountRao != step.QuoteSourceRao {
			return Action{}, nil, errors.New("probe recovery is not the exact quoted move position")
		}
		hotkey := move
		if step.Operation == "recover-sample" {
			hotkey = sample
		}
		data, err = parsed.Pack("transferOut", recovery, hotkey, new(big.Int).SetUint64(step.Move.AmountRao))
	case "reseed-sample":
		if step.Seed == nil || step.Seed.TAORao != r.ReseedTaoRao || step.Seed.BeforeRao != step.QuoteSourceRao || step.Move != (PrecompileMoveStep{}) || step.QuoteDestinationRao != 0 {
			return Action{}, nil, errors.New("probe reseed changed its fixed amount or sample custody")
		}
		value := new(big.Int).Mul(new(big.Int).SetUint64(r.ReseedTaoRao), big.NewInt(1_000_000_000))
		if step.Seed.ValueWei != value.String() {
			return Action{}, nil, errors.New("probe reseed changed its exact value conversion")
		}
		data, err = parsed.Pack("seedFromTao", sample, new(big.Int).SetUint64(r.ReseedTaoRao))
	default:
		return Action{}, nil, errors.New("probe recovery operation is not authorized")
	}
	if err != nil {
		return Action{}, nil, err
	}
	prefix := precompileRecoveryActionPrefix
	if r.GasRevision != nil {
		prefix += "v2."
	}
	action := Action{ID: fmt.Sprintf("%s%d", prefix, index+1), Kind: "evm-transaction", Target: r.Probe.Hex(), Description: "bounded recovery of the probe's explicitly retained stake liability", Parameters: map[string]string{
		"recovery_authorization_hash": authorization.Hash, "recovery_operation": step.Operation,
		"recovery_signer": r.Deployer.Hex(), "recovery_data_hash": crypto.Keccak256Hash(data).Hex(),
		"recovery_quote_block": strconv.FormatUint(step.QuoteHead.Number, 10), "recovery_quote_hash": step.QuoteHead.Hash,
		"recovery_source_quote_rao": strconv.FormatUint(step.QuoteSourceRao, 10), "recovery_destination_quote_rao": strconv.FormatUint(step.QuoteDestinationRao, 10),
		evmMaximumGasUnitsParameter: strconv.FormatUint(r.MaximumGasUnits, 10), evmMaximumFeePerGasParameter: strconv.FormatUint(r.MaximumFeePerGasWei, 10),
	}, Spend: Spend{EVMGasWei: multiplyUint64Decimal(r.MaximumGasUnits, r.MaximumFeePerGasWei)}}
	action.Parameters["recovery_value_wei"] = "0"
	if step.Operation == "reseed-sample" {
		action.Parameters["recovery_value_wei"] = step.Seed.ValueWei
		action.Spend.TAORao = r.ReseedTaoRao
	}
	action.IntentHash, err = actionIntentHash(action)
	return action, data, err
}

// The specialized semantic envelope avoids freezing an unsigned nonce while
// retaining exact signer, target, bounded value and calldata restrictions.
func validatePrecompileRecoveryTransactionFields(action Action, signer common.Address, to *common.Address, value *big.Int, data []byte) error {
	if !strings.HasPrefix(action.ID, precompileRecoveryActionPrefix) {
		return nil
	}
	if action.Kind != "evm-transaction" || signer != common.HexToAddress(action.Parameters["recovery_signer"]) || to == nil || *to != common.HexToAddress(action.Target) || value == nil || value.String() != action.Parameters["recovery_value_wei"] || !validCanonicalHashHex(action.Parameters["recovery_authorization_hash"]) || crypto.Keccak256Hash(data).Hex() != action.Parameters["recovery_data_hash"] {
		return errors.New("probe recovery transaction differs from its approved semantic envelope")
	}
	return nil
}

// Saved bytes remain bound before rebroadcast as well as during final replay.
// Receipt-only enforcement would discover an over-budget call after spending.
func validatePrecompileRecoverySignedBounds(action Action, transaction *types.Transaction) error {
	if !strings.HasPrefix(action.ID, precompileRecoveryActionPrefix) {
		return nil
	}
	gas, fee, err := evmActionFeeEnvelope(action)
	if err != nil {
		return err
	}
	if transaction == nil || transaction.Gas() == 0 || transaction.Gas() > gas || transaction.GasFeeCap().Sign() < 0 || transaction.GasFeeCap().Cmp(new(big.Int).SetUint64(fee)) > 0 {
		return errors.New("probe recovery signed transaction exceeded its approved gas or fee cap")
	}
	return nil
}

// Exact struct encoding, including quoted minima, is part of each action's
// retained evidence; no caller can replace a signed step with a fresh quote.
func precompileRecoveryActionEqual(left, right Action) bool {
	a, aErr := json.Marshal(left)
	b, bErr := json.Marshal(right)
	return aErr == nil && bErr == nil && bytes.Equal(a, b)
}
