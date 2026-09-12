//go:build linux || darwin

// Dynamic relay records are original signed requests, not new plan actions.
// Live admission and historical capture derive the same finite approved call.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const evidenceRelayRequestSchema = "urnetwork-sim-evidence-relay-action-v2"

// The wire remains exactly the original admission artifact. No transaction
// signer or mutable account state is embedded in the validator's request.
type evidenceRelayRequestRecord struct {
	Schema   string                                                    `json:"schema"`
	PlanHash string                                                    `json:"plan_hash"`
	Action   Action                                                    `json:"action"`
	Evidence validatorcomponent.ValidatorEvidenceTransactionV2Expected `json:"evidence"`
}

// The caller has authenticated the approved plan, including historical wire
// hashes. Recheck each allowance dimension without relying on current config.
func evidenceRelayPlanAllowance(plan *SetupPlan, reserve Action) (uint64, uint64, uint64, error) {
	if plan == nil || !validCanonicalHashHex(plan.PlanHash) || plan.ValidatorEvidence == nil {
		return 0, 0, 0, errors.New("evidence relay approved plan is absent")
	}
	companion := plan.ValidatorEvidence
	deploymentHash, err := contractDeploymentIdentityHash(plan.Deployment)
	if err != nil {
		return 0, 0, 0, err
	}
	if companion.Address == (common.Address{}) || companion.RuntimeCodeHash == (common.Hash{}) ||
		companion.DeploymentID != plan.DeploymentID || companion.ChainID != plan.ChainID || companion.Netuid != plan.Netuid ||
		companion.GenesisHash.Hex() != plan.GenesisHash || companion.Coordinator != plan.Deployment.CoordinatorProxy || companion.SettlementVault != plan.Deployment.SettlementVault ||
		reserve.ID != evidenceRelayReserveId || reserve.Kind != "budget-reserve" || reserve.Target != companion.Address.Hex() ||
		len(reserve.Parameters) != 7 || reserve.Parameters["runtime_code_hash"] != companion.RuntimeCodeHash.Hex() || reserve.Parameters[deploymentManifestHashParameter] != deploymentHash {
		return 0, 0, 0, errors.New("evidence relay reserve differs from the approved deployment")
	}
	actualHash, err := actionIntentHash(reserve)
	if err != nil || actualHash != reserve.IntentHash {
		return 0, 0, 0, errors.Join(errors.New("evidence relay reserve changed after approval"), err)
	}
	amounts := make(map[string]uint64, 5)
	for _, name := range []string{"maximum_slots", evmMaximumGasUnitsParameter, evmMaximumFeePerGasParameter, "validators", "operators"} {
		encoded := reserve.Parameters[name]
		value, err := strconv.ParseUint(encoded, 10, 64)
		if err != nil || value == 0 || encoded != strconv.FormatUint(value, 10) {
			return 0, 0, 0, fmt.Errorf("evidence relay reserve has invalid %s", name)
		}
		amounts[name] = value
	}
	gasUnits, feePerGas, maximum := amounts[evmMaximumGasUnitsParameter], amounts[evmMaximumFeePerGasParameter], amounts["maximum_slots"]
	continued := plan.EvidenceRelayContinuation != nil && gasUnits == evidenceRelayContinuationGas && feePerGas == evidenceRelayContinuationFee && maximum == evidenceRelayContinuationSlots
	if gasUnits < 21_000 || feePerGas != plan.MaximumEVMFeePerGasWei && !continued {
		return 0, 0, 0, errors.New("evidence relay reserve gas or fee ceiling differs")
	}
	total, err := multiplyDecimalUint64(multiplyUint64Decimal(gasUnits, feePerGas), maximum)
	if err != nil || reserve.Spend.EVMGasWei != total || reserve.Spend.TAORao != 0 || reserve.Spend.AlphaRao != 0 || reserve.Spend.Registrations != 0 || reserve.Spend.SubnetCreations != 0 {
		return 0, 0, 0, errors.Join(errors.New("evidence relay reserve aggregate spend differs"), err)
	}
	return gasUnits, feePerGas, maximum, nil
}

// Derives one exact action from the approved reserve and both real source signatures.
// Callers separately own immutable plan and finalized activation authentication.
func buildEvidenceRelayAction(plan *SetupPlan, reserve Action, supplied validatorcomponent.ValidatorEvidenceTransactionV2Expected) (Action, error) {
	gasUnits, feePerGas, _, err := evidenceRelayPlanAllowance(plan, reserve)
	if err != nil {
		return Action{}, err
	}
	deploymentHash := reserve.Parameters[deploymentManifestHashParameter]
	companion := plan.ValidatorEvidence
	if supplied.Journal != companion.Address || common.Hash(supplied.RuntimeHash) != companion.RuntimeCodeHash {
		return Action{}, errors.New("evidence relay target or executable differs from the approved companion")
	}
	if supplied.Evidence.Schema != validatorcomponent.ValidatorEvidenceSignedV2Schema || supplied.Evidence.Header.Hotkey != supplied.Activation.Hotkey || supplied.Evidence.Header.NoID != supplied.Activation.NoID || supplied.Evidence.Header.VPK != supplied.Activation.VPK {
		return Action{}, errors.New("evidence relay signed source differs from its independent activation")
	}
	domain, err := supplied.Activation.EvidenceDomain()
	if err != nil {
		return Action{}, err
	}
	if domain.ChainID != companion.ChainID || domain.GenesisHash != [32]byte(companion.GenesisHash) || domain.Netuid != companion.Netuid || domain.Coordinator != [20]byte(companion.Coordinator) || domain.SettlementVault != [20]byte(companion.SettlementVault) || domain.DeploymentIDHash != [32]byte(companion.DeploymentIDHash) {
		return Action{}, errors.New("evidence relay source domain differs from the approved deployment")
	}
	if err := supplied.Evidence.Header.Verify(domain, supplied.Window, supplied.Evidence.VPKSignature, supplied.Evidence.HotkeySignature); err != nil {
		return Action{}, err
	}
	slot, err := supplied.Evidence.Header.SlotKey()
	if err != nil {
		return Action{}, err
	}
	digest, err := supplied.Evidence.Header.Digest()
	if err != nil {
		return Action{}, err
	}
	action := Action{ID: fmt.Sprintf("%s%x", evidenceRelayActionPrefix, slot), Kind: "evm-transaction", Target: companion.Address.Hex(),
		Description: "publish one exact validator-consented immutable evidence slot", DependsOn: []string{evidenceRelayReserveId},
		Parameters: map[string]string{deploymentManifestHashParameter: deploymentHash, "validator_evidence_slot": fmt.Sprintf("0x%x", slot), "validator_evidence_header_hash": fmt.Sprintf("0x%x", digest),
			evmMaximumGasUnitsParameter: reserve.Parameters[evmMaximumGasUnitsParameter], evmMaximumFeePerGasParameter: reserve.Parameters[evmMaximumFeePerGasParameter]},
		Spend: Spend{EVMGasWei: multiplyUint64Decimal(gasUnits, feePerGas)}}
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		return Action{}, err
	}
	return action, nil
}

// Authenticates an original bounded request against its approved plan and
// preceding durable debit. It proves admission, not receipt or chain finality.
func validateEvidenceRelayRequest(plan *SetupPlan, entries []JournalEntry, raw []byte) (Action, error) {
	if plan == nil || len(raw) == 0 || len(raw) > evidenceRelayActionBytes {
		return Action{}, errors.New("evidence relay request owner or byte bound is invalid")
	}
	var record evidenceRelayRequestRecord
	if err := decodeStrictJSONBytes(raw, &record); err != nil {
		return Action{}, err
	}
	if record.Schema != evidenceRelayRequestSchema || record.PlanHash != plan.PlanHash {
		return Action{}, errors.New("evidence relay request names another approved plan")
	}
	reserve, err := exactPlanActionByID(plan, evidenceRelayReserveId)
	if err != nil {
		return Action{}, err
	}
	gasUnits, feePerGas, maximum, err := evidenceRelayPlanAllowance(plan, reserve)
	if err != nil {
		return Action{}, err
	}
	action, err := buildEvidenceRelayAction(plan, reserve, record.Evidence)
	if err != nil {
		return Action{}, err
	}
	if record.Evidence.Relayer != (common.Address{}) || record.Evidence.SignedTransaction != nil ||
		record.Evidence.MaxGas != gasUnits || record.Evidence.MaxFeePerGas != feePerGas ||
		record.Evidence.MaxTransactionBytes == 0 || record.Evidence.MaxReceiptLogs == 0 {
		return Action{}, errors.New("evidence relay request changes original custody or approved bounds")
	}
	// Re-encoding the reconstructed action also rejects duplicate keys,
	// whitespace and alternate encodings the immutable writer never produced.
	expected := record
	expected.Action = action
	canonical, err := json.Marshal(expected)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Action{}, errors.Join(errors.New("evidence relay request is not the exact canonical approved action"), err)
	}
	for _, entry := range entries {
		if entry.PlanHash == plan.PlanHash && strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) && entry.DeploymentID != plan.DeploymentID {
			return Action{}, errors.New("evidence relay journal contains a foreign deployment admission")
		}
	}
	_, admitted, err := evidenceRelayAdmissionCount(entries, plan.PlanHash, action, maximum)
	if err != nil || !admitted {
		return Action{}, errors.Join(errors.New("evidence relay request lacks its original budget admission"), err)
	}
	return action, nil
}
