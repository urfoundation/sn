//go:build linux || darwin

// Relay actions consume a finite approved allowance through the existing
// deployment journal. Admission reserves a full call even if it later fails;
// retries of that exact slot do not create a second spend allowance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const evidenceRelayReserveId = "campaign.evidence-relay-reserve"
const evidenceRelayActionPrefix = "evidence.relay."
const evidenceRelayActionBytes = 256 * 1024

// These are explicit testnet execution allowances, not protocol defaults.
// Closed censuses and later audit subjects share this one aggregate ceiling.
type evidenceRelayConfig struct {
	MaxSlots uint64 `yaml:"max_slots" json:"max_slots"`
	GasUnits uint64 `yaml:"gas_units" json:"gas_units"`
}

// A missing relay configuration is valid only when V2 itself is not enabled.
func evidenceRelayMaximumGas(cfg *ResolvedConfig) (DecimalUint, error) {
	if cfg == nil || cfg.Config == nil {
		return "", errors.New("evidence relay has no configuration")
	}
	value := cfg.Config.ValidatorEvidenceRelay
	if len(cfg.Config.ValidatorEvidenceV2) == 0 {
		if value != (evidenceRelayConfig{}) {
			return "", errors.New("evidence relay budget requires configured V2 sources")
		}
		return decimalUint64(0), nil
	}
	if value.MaxSlots == 0 || value.GasUnits < 21_000 || cfg.Config.Budgets.MaximumEVMFeePerGasWei == 0 {
		return "", errors.New("evidence relay requires explicit slot, gas and fee ceilings")
	}
	return multiplyDecimalUint64(multiplyUint64Decimal(value.GasUnits, cfg.Config.Budgets.MaximumEVMFeePerGasWei), value.MaxSlots)
}

// The approved reserve binds both contract identity and each dimension of the
// allowance. Its spend is subtracted before the remaining campaign gas split.
func buildEvidenceRelayReserve(cfg *ResolvedConfig, companion *ValidatorEvidenceDeployment, dependencies []string) (Action, error) {
	amount, err := evidenceRelayMaximumGas(cfg)
	if err != nil {
		return Action{}, err
	}
	if amount.IsZero() {
		return Action{}, nil
	}
	if companion == nil || companion.Address == (common.Address{}) || companion.RuntimeCodeHash == (common.Hash{}) {
		return Action{}, errors.New("evidence relay reserve has no pinned companion")
	}
	return Action{ID: evidenceRelayReserveId, Kind: "budget-reserve", Target: companion.Address.Hex(),
		Description: "reserve the finite keeper allowance for closed and later-audit validator evidence slots",
		Parameters: map[string]string{"maximum_slots": strconv.FormatUint(cfg.Config.ValidatorEvidenceRelay.MaxSlots, 10),
			evmMaximumGasUnitsParameter:  strconv.FormatUint(cfg.Config.ValidatorEvidenceRelay.GasUnits, 10),
			evmMaximumFeePerGasParameter: strconv.FormatUint(cfg.Config.Budgets.MaximumEVMFeePerGasWei, 10),
			"runtime_code_hash":          companion.RuntimeCodeHash.Hex(), "validators": strconv.Itoa(cfg.Config.Topology.Validators), "operators": strconv.Itoa(cfg.Config.Topology.Operators)},
		Spend: Spend{EVMGasWei: amount}, DependsOn: append([]string(nil), dependencies...)}, nil
}

// Count distinct durable admissions, not successful transactions or their
// latest stages. A failed or third-party-won slot cannot refund this ceiling.
func evidenceRelayAdmissionCount(entries []JournalEntry, planHash string, action Action, maximum uint64) (uint64, bool, error) {
	if !validCanonicalHashHex(planHash) || maximum == 0 || !strings.HasPrefix(action.ID, evidenceRelayActionPrefix) || !validCanonicalHashHex("0x"+strings.TrimPrefix(action.ID, evidenceRelayActionPrefix)) || !validCanonicalHashHex(action.IntentHash) {
		return 0, false, errors.New("evidence relay admission owner is invalid")
	}
	intents := map[string]string{}
	admitted := map[string]bool{}
	for _, entry := range entries {
		if entry.PlanHash != planHash || !strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) {
			continue
		}
		if !validCanonicalHashHex("0x"+strings.TrimPrefix(entry.ActionID, evidenceRelayActionPrefix)) || !validCanonicalHashHex(entry.IntentHash) {
			return 0, false, errors.New("evidence relay journal contains a malformed slot or intent")
		}
		if prior, found := intents[entry.ActionID]; found && prior != entry.IntentHash {
			return 0, false, errors.New("evidence relay journal changes a slot's durable action")
		}
		if entry.Stage != StageIntent && !admitted[entry.ActionID] {
			return 0, false, errors.New("evidence relay transaction has no preceding budget admission")
		}
		intents[entry.ActionID] = entry.IntentHash
		admitted[entry.ActionID] = admitted[entry.ActionID] || entry.Stage == StageIntent
		if uint64(len(intents)) > maximum {
			return 0, false, errors.New("evidence relay durable admissions exceed the approved ceiling")
		}
	}
	if previous, found := intents[action.ID]; found {
		if previous != action.IntentHash {
			return 0, false, errors.New("evidence relay retry changes its original action")
		}
		return uint64(len(intents)), true, nil
	}
	if uint64(len(intents)) >= maximum {
		return 0, false, errors.New("evidence relay has exhausted its approved slot allowance")
	}
	return uint64(len(intents)), false, nil
}

// The campaign owns this method serially under the existing deployment lock.
// Only its one relay worker may admit dynamic slots; account nonce ownership
// remains in EvmTxManager and is shared with the ordinary keeper actions.
func (self *Executor) admitEvidenceRelayAction(ctx context.Context, supplied validatorcomponent.ValidatorEvidenceTransactionV2Expected) (Action, error) {
	if ctx == nil || ctx.Err() != nil {
		return Action{}, errors.New("evidence relay admission context is absent or canceled")
	}
	if self == nil || self.cfg == nil || self.plan == nil || self.journal == nil || self.plan.ValidatorEvidence == nil {
		return Action{}, errors.New("evidence relay admission has no approved deployment owner")
	}
	reserve, err := buildEvidenceRelayReserve(self.cfg, self.plan.ValidatorEvidence, nil)
	if err != nil || reserve.ID == "" {
		return Action{}, errors.Join(errors.New("evidence relay has no approved reserve"), err)
	}
	var actual *Action
	for index := range self.plan.Actions {
		if self.plan.Actions[index].ID == reserve.ID {
			if actual != nil {
				return Action{}, errors.New("evidence relay reserve is duplicated")
			}
			actual = &self.plan.Actions[index]
		}
	}
	if actual == nil {
		return Action{}, errors.New("evidence relay reserve is absent from the approved plan")
	}
	if self.plan.EvidenceRelayContinuation!=nil {
		if err:=validateEvidenceRelayContinuationBudget(self.plan);err!=nil { return Action{},err }
		if self.cfg.ConfigHash!=self.plan.EvidenceRelayContinuation.ConfigHash || reserve.Spend!=actual.Spend { return Action{},errors.New("relay continuation changed the original configured monetary reserve") }
		reserve=*actual
		reserve.Parameters=maps.Clone(actual.Parameters)
	}
	reserve.DependsOn = append([]string(nil), actual.DependsOn...)
	deploymentHash, err := contractDeploymentIdentityHash(self.plan.Deployment)
	if err != nil {
		return Action{}, err
	}
	reserve.Parameters[deploymentManifestHashParameter] = deploymentHash
	reserve.IntentHash, err = actionIntentHash(reserve)
	if err != nil || reserve.IntentHash != actual.IntentHash {
		return Action{}, errors.Join(errors.New("evidence relay reserve differs from its approved dimensions"), err)
	}
	actualHash, err := actionIntentHash(*actual)
	if err != nil || actualHash != actual.IntentHash {
		return Action{}, errors.Join(errors.New("evidence relay reserve content changed after approval"), err)
	}
	action, err := buildEvidenceRelayAction(self.plan, reserve, supplied)
	if err != nil {
		return Action{}, err
	}
	entries,maximum,err:=self.evidenceRelayAdmissionEntries()
	if err!=nil { return Action{},err }
	_, admitted, err := evidenceRelayAdmissionCount(entries, self.plan.PlanHash, action, maximum)
	if err != nil {
		return Action{}, err
	}
	// Retain the actual signed request before its first durable budget debit.
	// This is an action artifact, not another nonce or account state store.
	record := evidenceRelayRequestRecord{Schema: evidenceRelayRequestSchema, PlanHash: self.plan.PlanHash, Action: action, Evidence: supplied}
	record.Evidence.SignedTransaction = nil
	record.Evidence.Relayer = common.Address{}
	record.Evidence.MaxGas, record.Evidence.MaxFeePerGas, _,err = evidenceRelayPlanAllowance(self.plan,reserve)
	if err!=nil { return Action{},err }
	raw, err := json.Marshal(record)
	if err != nil {
		return Action{}, err
	}
	path := filepath.Join(self.stateDir, "evidence-relay", strings.TrimPrefix(action.ID, evidenceRelayActionPrefix)+".json")
	if admitted {
		prior, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, path, evidenceRelayActionBytes)
		if err != nil {
			return Action{}, err
		}
		if string(prior) != string(raw) {
			return Action{}, errors.New("evidence relay retry differs from its original retained request")
		}
	}
	if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, path, raw, evidenceRelayActionBytes); err != nil {
		return Action{}, err
	}
	if !admitted {
		if err := self.journal.Append(JournalEntry{DeploymentID: self.cfg.Config.Deployment.DeploymentID, PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
			return Action{}, err
		}
	}
	return action, ctx.Err()
}
