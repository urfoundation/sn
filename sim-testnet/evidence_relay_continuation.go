//go:build linux || darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const evidenceRelayContinuationSchema = "urnetwork-sim-evidence-relay-continuation-v2"
const evidenceRelayContinuationSlots uint64 = 512
const evidenceRelayContinuationGas uint64 = 1_000_000
const evidenceRelayContinuationFee uint64 = 50_000_000_000

// One approval repartitions the original allowance. It never replaces an
// activation, forgives an admitted transaction, resets a ledger or moves its
// end on restart. Capture is a request; apply and runtime replay its sources.
type EvidenceRelayContinuation struct {
	Schema                 string                                                      `json:"schema"`
	SourcePlanHash         string                                                      `json:"source_plan_hash"`
	ConfigHash             string                                                      `json:"config_hash"`
	ActivationPlanHash     string                                                      `json:"activation_plan_hash"`
	PreparedSHA256         string                                                      `json:"prepared_sha256"`
	CompletedSHA256        string                                                      `json:"completed_sha256"`
	JournalHash            string                                                      `json:"journal_hash"`
	OriginalReserve        Action                                                      `json:"original_reserve"`
	EVMHead                ChainHead                                                   `json:"evm_head"`
	NativeHead             ChainHead                                                   `json:"native_head"`
	SettlementEpoch        uint64                                                      `json:"settlement_epoch"`
	NativeEpoch            uint64                                                      `json:"native_epoch"`
	EndBlock               uint64                                                      `json:"end_block"`
	EndSettlementEpoch     uint64                                                      `json:"end_settlement_epoch"`
	EndNativeEpoch         uint64                                                      `json:"end_native_epoch"`
	RequiredWorkBlocks     uint64                                                      `json:"required_work_blocks"`
	HistoricalLiabilityWei DecimalUint                                                 `json:"historical_liability_wei"`
	NewSlots               uint64                                                      `json:"new_slots"`
	Sources                []EvidenceRelayContinuationSource                           `json:"sources"`
	Retained               []validatorcomponent.ValidatorEvidenceTransactionV2Expected `json:"retained"`
	Debits                 []EvidenceRelayContinuationDebit                            `json:"debits"`
	Nonces                 []FleetRenewalNonce                                         `json:"nonces"`
	TransactionsSHA256     string                                                      `json:"transactions_sha256"`
}

type EvidenceRelayContinuationSource struct {
	ValidatorID         uint64                                          `json:"validator_id"`
	NoID                uint64                                          `json:"no_id"`
	CoordinatorStateDir string                                          `json:"coordinator_state_dir"`
	IntentPrefixSHA256  string                                          `json:"intent_prefix_sha256"`
	IntentPrefixCount   uint64                                          `json:"intent_prefix_count"`
	LastNativeEpoch     uint64                                          `json:"last_native_epoch"`
	LastArtifactHash    string                                          `json:"last_artifact_hash"`
	Activation          protocol.ValidatorEvidenceActivation            `json:"activation"`
	Capacity            validatorcomponent.StoppedAttemptLedgerCapacity `json:"capacity"`
}

type EvidenceRelayContinuationDebit struct {
	PlanHash      string      `json:"plan_hash"`
	ActionID      string      `json:"action_id"`
	RequestSHA256 string      `json:"request_sha256"`
	AllowanceWei  DecimalUint `json:"allowance_wei"`
}

func evidenceRelayContinuationNewSlots(reserve DecimalUint, debits []EvidenceRelayContinuationDebit) (uint64, DecimalUint, error) {
	remaining, ok := new(big.Int).SetString(string(reserve), 10)
	if !ok || remaining.Sign() <= 0 {
		return 0, "", errors.New("relay continuation reserve is invalid")
	}
	liability := new(big.Int)
	seen := map[string]bool{}
	for _, debit := range debits {
		amount, ok := new(big.Int).SetString(string(debit.AllowanceWei), 10)
		if !ok || amount.Sign() <= 0 || !validCanonicalHashHex(debit.PlanHash) || !validCanonicalHashHex("0x"+stringsTrimRelayPrefix(debit.ActionID)) || seen[debit.ActionID] {
			return 0, "", errors.New("relay continuation original debit identity or allowance differs")
		}
		seen[debit.ActionID] = true
		liability.Add(liability, amount)
	}
	remaining.Sub(remaining, liability)
	if remaining.Sign() < 0 {
		return 0, "", errors.New("relay continuation original liabilities exceed their retained reserve")
	}
	remaining.Div(remaining, new(big.Int).SetUint64(evidenceRelayContinuationGas*evidenceRelayContinuationFee))
	if !remaining.IsUint64() || remaining.Uint64() > evidenceRelayContinuationSlots {
		return 0, "", errors.New("relay continuation new slot allowance differs")
	}
	return remaining.Uint64(), DecimalUint(liability.String()), nil
}

func stringsTrimRelayPrefix(id string) string {
	if len(id) <= len(evidenceRelayActionPrefix) || id[:len(evidenceRelayActionPrefix)] != evidenceRelayActionPrefix {
		return ""
	}
	return id[len(evidenceRelayActionPrefix):]
}

func evidenceRelayContinuationCeil(value, divisor uint64) (uint64, error) {
	if divisor == 0 {
		return 0, errors.New("relay continuation has a zero cadence")
	}
	result := value / divisor
	if value%divisor != 0 {
		result++
	}
	return result, nil
}

func (c *EvidenceRelayContinuation) validateClocks(work evidenceRelayWork) error {
	if c == nil || c.EVMHead.Number == 0 || c.NativeHead.Number == 0 || !validCanonicalHashHex(c.EVMHead.Hash) || !validCanonicalHashHex(c.NativeHead.Hash) || c.EndBlock <= c.EVMHead.Number || c.NativeEpoch == 0 {
		return errors.New("relay continuation has incomplete canonical clock anchors")
	}
	required, err := work.remaining("release-1.0", false)
	if err != nil || c.RequiredWorkBlocks != required || c.EndBlock-c.EVMHead.Number < required {
		return errors.Join(errors.New("relay continuation cannot fit the unchanged complete campaign work"), err)
	}
	closed, err := evidenceRelayContinuationCeil(c.EndBlock-c.EVMHead.Number, work.settlementCadence)
	if err != nil {
		return err
	}
	native, err := evidenceRelayContinuationCeil(c.EndBlock-c.EVMHead.Number, work.nativeCadence)
	if err != nil {
		return err
	}
	endEpoch, closedOK := checkedAdd(c.SettlementEpoch, closed)
	endNative, nativeOK := checkedAdd(c.NativeEpoch, native)
	if !closedOK || !nativeOK || c.EndSettlementEpoch != endEpoch || c.EndNativeEpoch != endNative {
		return errors.New("relay continuation changed its fixed independent-clock runway")
	}
	return nil
}

// Every possible closed census from the original activation is reserved,
// including missing historical publications. Actual old audits are charged
// individually. Future native buckets reserve one subject each; any additional
// subject at that same source/native epoch consumes another slot immediately.
func (c *EvidenceRelayContinuation) requiredSubjects(headers map[[32]byte]protocol.ValidatorEvidenceHeader, candidate *protocol.ValidatorEvidenceHeader) (uint64, error) {
	if c == nil || len(c.Sources) == 0 || c.EndNativeEpoch < c.NativeEpoch {
		return 0, errors.New("relay continuation source forecast is absent")
	}
	var required uint64
	add := func(value uint64) error {
		next, ok := checkedAdd(required, value)
		if !ok {
			return errors.New("relay continuation subject forecast overflows")
		}
		required = next
		return nil
	}
	for _, source := range c.Sources {
		if c.EndSettlementEpoch < source.Activation.Domain.Epoch {
			return 0, errors.New("relay continuation predates original activation")
		}
		if err := add(c.EndSettlementEpoch - source.Activation.Domain.Epoch + 1); err != nil {
			return 0, err
		}
		if err := add(c.EndNativeEpoch - c.NativeEpoch + 1); err != nil {
			return 0, err
		}
	}
	type nativeSource struct {
		hotkey       [32]byte
		noID, native uint64
	}
	seen := map[nativeSource]bool{}
	count := func(header protocol.ValidatorEvidenceHeader) error {
		if header.Kind != protocol.ValidatorEvidenceDepositAudit {
			return nil
		}
		key := nativeSource{header.Hotkey, header.NoID, header.Subject.NativeEpoch}
		if header.Subject.NativeEpoch < c.NativeEpoch || seen[key] {
			return add(1)
		}
		seen[key] = true
		return nil
	}
	for _, header := range headers {
		if err := count(header); err != nil {
			return 0, err
		}
	}
	if candidate != nil {
		slot, err := candidate.SlotKey()
		if err != nil {
			return 0, err
		}
		if _, found := headers[slot]; !found {
			if err := count(*candidate); err != nil {
				return 0, err
			}
		}
	}
	return required, nil
}

func appendEvidenceRelayContinuationPlan(base *SetupPlan, c EvidenceRelayContinuation) (*SetupPlan, error) {
	if base == nil || base.EvidenceRelayContinuation != nil || c.SourcePlanHash != base.PlanHash || c.ConfigHash != base.ConfigHash {
		return nil, errors.New("relay continuation must extend the exact original approved source once")
	}
	original, err := exactPlanActionByID(base, evidenceRelayReserveId)
	if err != nil || !reflect.DeepEqual(original, c.OriginalReserve) {
		return nil, errors.Join(errors.New("relay continuation replaced its original reserve"), err)
	}
	gas, fee, maximum, err := evidenceRelayPlanAllowance(base, original)
	if err != nil || gas != evidenceRelayContinuationGas || fee != 2*evidenceRelayContinuationFee || maximum != evidenceRelayContinuationSlots/2 {
		return nil, errors.Join(errors.New("relay continuation is outside the retained256-slot100gwei approval"), err)
	}
	raw, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	var plan SetupPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, err
	}
	plan.EvidenceRelayContinuation = &c
	plan.PriorPlanHashes = append(plan.PriorPlanHashes, base.PlanHash)
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != evidenceRelayReserveId {
			continue
		}
		action.Parameters["maximum_slots"] = strconv.FormatUint(evidenceRelayContinuationSlots, 10)
		action.Parameters[evmMaximumFeePerGasParameter] = strconv.FormatUint(evidenceRelayContinuationFee, 10)
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return nil, err
		}
	}
	plan.PlanHash = ""
	plan.PlanHash, err = plan.hash()
	if err != nil {
		return nil, err
	}
	if err := validatePlanBudget(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func validateEvidenceRelayContinuationPlan(plan *SetupPlan) error {
	if plan == nil {
		return errors.New("relay continuation plan is absent")
	}
	c := plan.EvidenceRelayContinuation
	if c == nil {
		return nil
	}
	if err := validateEvidenceRelayContinuationBudget(plan); err != nil {
		return err
	}
	if c.Schema != evidenceRelayContinuationSchema || c.SourcePlanHash == plan.PlanHash || !plan.allowedPlanHashes()[c.SourcePlanHash] || !plan.allowedPlanHashes()[c.ActivationPlanHash] || c.ConfigHash != plan.ConfigHash || !validCanonicalHashHex(c.JournalHash) || len(c.Sources) != 4 || len(c.Retained) > int(evidenceRelayContinuationSlots) || len(c.Debits) > int(evidenceRelayContinuationSlots/2) {
		return errors.New("relay continuation changed its source approval or exact four-source census")
	}
	for _, hash := range []string{c.PreparedSHA256, c.CompletedSHA256, c.TransactionsSHA256} {
		if !validSHA256String(hash) {
			return errors.New("relay continuation lost its immutable input digest")
		}
	}
	reserve, err := exactPlanActionByID(plan, evidenceRelayReserveId)
	if err != nil {
		return err
	}
	gas, fee, slots, err := evidenceRelayPlanAllowance(plan, reserve)
	if err != nil || gas != evidenceRelayContinuationGas || fee != evidenceRelayContinuationFee || slots != evidenceRelayContinuationSlots || reserve.Spend != c.OriginalReserve.Spend {
		return errors.Join(errors.New("relay continuation changed the existing aggregate monetary allowance"), err)
	}
	newSlots, liability, err := evidenceRelayContinuationNewSlots(c.OriginalReserve.Spend.EVMGasWei, c.Debits)
	if err != nil || c.NewSlots != newSlots || c.HistoricalLiabilityWei != liability {
		return errors.Join(errors.New("relay continuation failed to preserve original higher-fee liabilities"), err)
	}
	seen := map[evidenceRelayHorizonSource]bool{}
	for index, source := range c.Sources {
		key := evidenceRelayHorizonSource{source.Activation.Hotkey, source.NoID}
		if source.ValidatorID != uint64(index/2+1) || source.NoID != uint64(index%2+1) || source.Activation.NoID != source.NoID || seen[key] || !filepath.IsAbs(source.CoordinatorStateDir) || filepath.Clean(source.CoordinatorStateDir) != source.CoordinatorStateDir || filepath.Base(source.CoordinatorStateDir) != "coordinator-state-v2" || !validSHA256String(source.IntentPrefixSHA256) || source.Capacity.Identity.ValidatorID != source.ValidatorID || source.Capacity.Identity.NoID != source.NoID || source.Capacity.Identity.ValidatorVPK != fmt.Sprintf("0x%x", source.Activation.VPK) || source.Capacity.Identity.DeploymentID != plan.DeploymentID {
			return errors.New("relay continuation changed an original activation, namespace or signed counter owner")
		}
		seen[key] = true
	}
	headers := map[[32]byte]protocol.ValidatorEvidenceHeader{}
	previous := ""
	for _, request := range c.Retained {
		action, err := buildEvidenceRelayAction(plan, reserve, request)
		if err != nil {
			return err
		}
		if action.ID <= previous {
			return errors.New("relay continuation retained census is not exact unique slot order")
		}
		previous = action.ID
		key := evidenceRelayHorizonSource{request.Activation.Hotkey, request.Activation.NoID}
		if !seen[key] {
			return errors.New("relay continuation retained request names an unknown source")
		}
		for _, source := range c.Sources {
			if source.Activation.Hotkey == key.hotkey && source.NoID == key.noId && source.Activation != request.Activation {
				return errors.New("relay continuation retained request replaced original activation")
			}
		}
		slot, err := request.Evidence.Header.SlotKey()
		if err != nil {
			return err
		}
		headers[slot] = request.Evidence.Header
	}
	required, err := c.requiredSubjects(headers, nil)
	if err != nil || required > uint64(len(c.Debits))+c.NewSlots {
		return errors.Join(errors.New("relay continuation cannot reserve all original and future subjects"), err)
	}
	return nil
}

func validSHA256String(value string) bool {
	return len(value) == 71 && value[:7] == "sha256:" && validCanonicalHashHex("0x"+value[7:])
}

// The immutable pending census is authenticated at plan/phase entry. Per-slot
// admission rechecks only its finite money/owner terms and the actual request;
// it does not replay every prior source signature for each new transaction.
func validateEvidenceRelayContinuationBudget(plan *SetupPlan) error {
	if plan == nil || plan.EvidenceRelayContinuation == nil {
		return errors.New("relay continuation budget owner is absent")
	}
	c := plan.EvidenceRelayContinuation
	if c.Schema != evidenceRelayContinuationSchema || c.ConfigHash != plan.ConfigHash || c.SourcePlanHash == plan.PlanHash || !plan.allowedPlanHashes()[c.SourcePlanHash] || plan.MaximumEVMFeePerGasWei != 2*evidenceRelayContinuationFee {
		return errors.New("relay continuation budget changed original approval identity")
	}
	original := *plan
	original.PlanHash = c.SourcePlanHash
	original.EvidenceRelayContinuation = nil
	gas, fee, maximum, err := evidenceRelayPlanAllowance(&original, c.OriginalReserve)
	if err != nil || gas != evidenceRelayContinuationGas || fee != 2*evidenceRelayContinuationFee || maximum != evidenceRelayContinuationSlots/2 {
		return errors.Join(errors.New("relay continuation budget changed original reserve dimensions"), err)
	}
	reserve, err := exactPlanActionByID(plan, evidenceRelayReserveId)
	if err != nil {
		return err
	}
	gas, fee, maximum, err = evidenceRelayPlanAllowance(plan, reserve)
	if err != nil || gas != evidenceRelayContinuationGas || fee != evidenceRelayContinuationFee || maximum != evidenceRelayContinuationSlots || reserve.Spend != c.OriginalReserve.Spend {
		return errors.Join(errors.New("relay continuation budget changed aggregate monetary authority"), err)
	}
	newSlots, liability, err := evidenceRelayContinuationNewSlots(c.OriginalReserve.Spend.EVMGasWei, c.Debits)
	if err != nil || newSlots != c.NewSlots || liability != c.HistoricalLiabilityWei {
		return errors.Join(errors.New("relay continuation budget omitted original liabilities"), err)
	}
	return nil
}

func carryEvidenceRelayContinuationRevision(revised, prior *SetupPlan) error {
	if prior == nil || prior.EvidenceRelayContinuation == nil {
		return nil
	}
	raw, err := json.Marshal(prior.EvidenceRelayContinuation)
	if err != nil {
		return err
	}
	var c EvidenceRelayContinuation
	if err := json.Unmarshal(raw, &c); err != nil {
		return err
	}
	reserve, err := exactPlanActionByID(prior, evidenceRelayReserveId)
	if err != nil {
		return err
	}
	revised.EvidenceRelayContinuation = &c
	for index := range revised.Actions {
		if revised.Actions[index].ID == reserve.ID {
			revised.Actions[index] = reserve
			return nil
		}
	}
	return errors.New("relay continuation revision removed its retained reserve")
}
