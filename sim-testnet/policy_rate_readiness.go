// Rate activation admits no settlement credit. A complete signed source and a
// conservative native-floor preflight decide when a new interval may begin.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/urfoundation/sn/protocol"
)

// Only signature-verified, current-policy, closed-epoch artifacts populate this
// field. The full immutable artifact remains in the existing public history.
type PolicyRateSourceObservation struct {
	NoId            uint64 `json:"no_id"`
	Epoch           uint64 `json:"epoch"`
	PolicyHash      string `json:"policy_hash"`
	ContentHash     string `json:"content_hash"`
	TotalUsageBytes uint64 `json:"total_usage_bytes"`
	TotalUsers      uint64 `json:"total_users,omitempty"`
}

// The same finalized EVM head pins the active policy, native floor and price.
// This preliminary margin never replaces exact pre-sign deposit validation.
type PolicyRateReadinessObservation struct {
	Ready                 bool                          `json:"ready"`
	Detail                string                        `json:"detail"`
	Head                  ChainHead                     `json:"head"`
	AlphaPriceWei         string                        `json:"alpha_price_wei"`
	MinimumTransferTaoRao uint64                        `json:"minimum_transfer_tao_rao"`
	Sources               []PolicyRateSourceObservation `json:"sources"`
	ProvisionalLowUsage   *PolicyRateLowUsageDeferral   `json:"provisional_low_usage,omitempty"`
}

// The measured shortfall remains evidence; it grants no deposit or epoch credit.
type PolicyRateUsageShortfall struct {
	NoId             uint64 `json:"no_id"`
	Epoch            uint64 `json:"epoch"`
	TotalUsageBytes  uint64 `json:"total_usage_bytes"`
	TotalUsers       uint64 `json:"total_users,omitempty"`
	MinConvictionRao uint64 `json:"min_conviction_rao"`
	EquivalentTaoRao string `json:"equivalent_tao_rao"`
	RequiredTaoRao   string `json:"required_tao_rao"`
}

// Only an explicit provisional release may start traffic with this diagnostic.
type PolicyRateLowUsageDeferral struct {
	Scope           string                     `json:"scope"`
	Provisional     bool                       `json:"provisional"`
	FinalAcceptance bool                       `json:"final_acceptance"`
	Shortfalls      []PolicyRateUsageShortfall `json:"shortfalls"`
}

// Source authentication completes before this typed, deferrable failure exists.
type policyRateLowUsageError struct {
	shortfalls []PolicyRateUsageShortfall
}

// Preserve the first precise margin diagnostic, with every tier retained above.
func (self *policyRateLowUsageError) Error() string {
	first := self.shortfalls[0]
	return fmt.Sprintf("operator %d complete epoch %d bytes=%d users=%d gives %s tao rao at tier %d; require twice native minimum %s", first.NoId, first.Epoch, first.TotalUsageBytes, first.TotalUsers, first.EquivalentTaoRao, first.MinConvictionRao, first.RequiredTaoRao)
}

// Require the last complete epoch under the successor policy, covering every
// operator and every conviction tier at twice the immutable native minimum.
func validatePolicyRateReadiness(cfg *ResolvedConfig, contracts *ContractView, sources []PolicyRateSourceObservation, price *big.Int) error {
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || cfg.previousPolicy == nil || validateFuturePolicyRateAmendment(cfg.previousPolicy, cfg.Policy) != nil || contracts == nil || contracts.PolicyHash != cfg.PolicyHash || contracts.Policy.EffectiveEpoch == 0 || contracts.CurrentEpoch <= contracts.Policy.EffectiveEpoch || price == nil || price.Sign() <= 0 || contracts.MinimumTransferRao == 0 || cfg.Policy.Deposit.UsageLagEpochs != 1 {
		return errors.New("rate amendment requires an active exact policy and one complete lagged source epoch")
	}
	if len(sources) != cfg.Config.Topology.Operators {
		return errors.New("rate amendment lacks a complete usage source for every operator")
	}
	minimum := new(big.Int).Mul(new(big.Int).SetUint64(contracts.MinimumTransferRao), big.NewInt(2))
	seenKVs := map[uint64]bool{}
	for _, source := range sources {
		if source.NoId == 0 || source.NoId > uint64(cfg.Config.Topology.Operators) || seenKVs[source.NoId] || source.Epoch != contracts.CurrentEpoch-1 || source.Epoch < contracts.Policy.EffectiveEpoch || source.PolicyHash != cfg.PolicyHash || !validSHA256ContentHash(source.ContentHash) {
			return errors.New("rate amendment source has a foreign operator, policy, epoch or content identity")
		}
		seenKVs[source.NoId] = true
	}
	var shortfalls []PolicyRateUsageShortfall
	for _, source := range sources {
		for _, tier := range cfg.Policy.Deposit.Tiers {
			deposit, _, err := protocol.RequiredDepositRao(source.TotalUsageBytes, source.TotalUsers, new(big.Int).SetUint64(tier.MinConvictionRao), cfg.Policy.Deposit)
			if err != nil {
				return err
			}
			// Ignoring the two-rao reserve rounding allowance is conservative.
			equivalent := new(big.Int).Quo(new(big.Int).Mul(deposit, price), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
			if equivalent.Cmp(minimum) < 0 {
				shortfalls = append(shortfalls, PolicyRateUsageShortfall{NoId: source.NoId, Epoch: source.Epoch, TotalUsageBytes: source.TotalUsageBytes, TotalUsers: source.TotalUsers, MinConvictionRao: tier.MinConvictionRao, EquivalentTaoRao: equivalent.String(), RequiredTaoRao: minimum.String()})
			}
		}
	}
	if len(shortfalls) != 0 {
		return &policyRateLowUsageError{shortfalls: shortfalls}
	}
	return nil
}

// Recheck the signed, chain-matching source census and structural membership.
// A separate recorded cohort expectation is allowed, never a malformed member.
func provisionalPolicyRateLowUsage(cfg *ResolvedConfig, proof *PolicyRateReadinessObservation, operators []OperatorObservation, lowUsage *policyRateLowUsageError) *PolicyRateLowUsageDeferral {
	if !provisionalResumeEnabled(cfg) || cfg.readOnlyAudit || cfg.Config == nil || proof == nil || lowUsage == nil || len(lowUsage.shortfalls) == 0 {
		return nil
	}
	record := cfg.provisionalResume.Record
	if !record.Provisional || record.FinalAcceptance || record.ReadOnly || record.Command != "scenario" || record.Scenario != "release-1.0" || len(operators) != cfg.Config.Topology.Operators || len(proof.Sources) != len(operators) {
		return nil
	}
	sourcesKVs := map[uint64]PolicyRateSourceObservation{}
	for _, source := range proof.Sources {
		sourcesKVs[source.NoId] = source
	}
	for _, operator := range operators {
		source, ok := sourcesKVs[uint64(operator.NoID)]
		if !ok || operator.Error != "" || !operator.Healthy || operator.RateSource == nil || *operator.RateSource != source || operator.ValidArtifacts == 0 || operator.MatchingArtifacts == 0 || !slices.Contains(operator.ArtifactHashes, source.ContentHash) || operator.LatestArtifactEpoch != source.Epoch || operator.LatestArtifactHash != source.ContentHash {
			return nil
		}
		if !operator.TierMembershipValid {
			cohort := operator.ProvisionalPayoutCohort
			if cohort == nil || cohort.Scope != "configured-release-cohort-expectation" || !cohort.Provisional || cohort.FinalAcceptance || cohort.Error == "" {
				return nil
			}
		}
		delete(sourcesKVs, source.NoId)
	}
	return &PolicyRateLowUsageDeferral{Scope: "complete-source-native-floor-margin", Provisional: true, FinalAcceptance: false, Shortfalls: slices.Clone(lowUsage.shortfalls)}
}

// Both admission gates replay the exact pinned proof. Ready remains false for
// provisional low usage; strict execution and final semantic acceptance do not
// consume this deferral, and actual epoch deposit checks remain mandatory.
func validateScenarioPolicyRateAdmission(cfg *ResolvedConfig, observation *ScenarioObservation) error {
	if observation == nil || observation.Status == nil || observation.Status.Contracts == nil || observation.PolicyRateReadiness == nil {
		return errors.New("rate amendment acceptance lacks its exact complete usage source and native-floor margin")
	}
	proof, contracts := observation.PolicyRateReadiness, observation.Status.Contracts
	if proof.Head != contracts.FinalizedHead || proof.MinimumTransferTaoRao != contracts.MinimumTransferRao {
		return errors.New("rate amendment acceptance source head or native floor differs")
	}
	price, ok := new(big.Int).SetString(proof.AlphaPriceWei, 10)
	if !ok || price.String() != proof.AlphaPriceWei {
		return errors.New("rate amendment acceptance alpha price is malformed")
	}
	err := validatePolicyRateReadiness(cfg, contracts, proof.Sources, price)
	if err == nil {
		if !proof.Ready || proof.ProvisionalLowUsage != nil {
			return errors.New("rate amendment readiness outcome differs from its exact margin")
		}
		return nil
	}
	var lowUsage *policyRateLowUsageError
	if proof.Ready || !errors.As(err, &lowUsage) || proof.Detail != err.Error() {
		return err
	}
	want := provisionalPolicyRateLowUsage(cfg, proof, observation.Operators, lowUsage)
	got := proof.ProvisionalLowUsage
	if want == nil || got == nil || got.Scope != want.Scope || !got.Provisional || got.FinalAcceptance || !slices.Equal(got.Shortfalls, want.Shortfalls) {
		return err
	}
	return nil
}

// Persist honest readiness and, only when authorized, a typed startup deferral.
func completePolicyRateReadiness(cfg *ResolvedConfig, contracts *ContractView, operators []OperatorObservation, result *PolicyRateReadinessObservation, price *big.Int) {
	result.Ready, result.ProvisionalLowUsage = false, nil
	if err := validatePolicyRateReadiness(cfg, contracts, result.Sources, price); err != nil {
		result.Detail = err.Error()
		var lowUsage *policyRateLowUsageError
		if errors.As(err, &lowUsage) {
			result.ProvisionalLowUsage = provisionalPolicyRateLowUsage(cfg, result, operators, lowUsage)
		}
		return
	}
	result.Ready, result.Detail = true, "complete current-policy source epoch clears twice the native minimum at every rate tier"
}

// Reads only one pinned precompile value after the normal signed-artifact
// census. Missing usage or temporary availability is persisted as not ready.
func (self *liveScenarioProbe) observePolicyRateReadiness(ctx context.Context, contracts *ContractView, operators []OperatorObservation) *PolicyRateReadinessObservation {
	result := &PolicyRateReadinessObservation{}
	if contracts == nil {
		result.Detail = "rate readiness contract state unavailable"
		return result
	}
	result.Head, result.MinimumTransferTaoRao = contracts.FinalizedHead, contracts.MinimumTransferRao
	for _, operator := range operators {
		if operator.Error != "" {
			result.Detail = "rate readiness operator observation: " + operator.Error
			return result
		}
		if operator.RateSource != nil {
			result.Sources = append(result.Sources, *operator.RateSource)
		}
	}
	client, err := dialConfiguredEVMClient(ctx, self.cfg, self.cfg.OperationalEVM)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	defer client.Close()
	parsed, err := abi.JSON(strings.NewReader(alphaPricePrecompileABI))
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	values, err := contractCallAt(ctx, client, alphaPricePrecompileAddress, parsed, "getAlphaPrice", contracts.FinalizedHead.Number, self.cfg.Netuid)
	if err != nil || len(values) != 1 {
		result.Detail = fmt.Sprint("rate readiness pinned alpha price: ", err)
		return result
	}
	price, ok := values[0].(*big.Int)
	if !ok || price == nil {
		result.Detail = "rate readiness alpha price type differs"
		return result
	}
	result.AlphaPriceWei = price.String()
	completePolicyRateReadiness(self.cfg, contracts, operators, result, price)
	return result
}

// Ordinary observation and the independent service continue while a bounded
// three-epoch readiness owner waits. No fault or accepted epoch starts here.
func waitScenarioPolicyRateReadiness(ctx context.Context, cfg *ResolvedConfig, phase string, current *ScenarioObservation, probe scenarioProbe, poll time.Duration, retain func(*ScenarioObservation) error) (*ScenarioObservation, error) {
	if cfg.previousPolicy == nil || phase != "release-1.0" {
		return current, nil
	}
	blocks, ok := checkedMul(cfg.Policy.Settlement.EpochBlocks, 3)
	if !ok {
		return current, errors.New("rate readiness duration overflows")
	}
	duration, err := scenarioNativeBlockDurationV2(cfg, blocks)
	if err != nil {
		return current, err
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	for {
		if validateScenarioPolicyRateAdmission(cfg, current) == nil {
			return current, nil
		}
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		case <-time.After(max(poll, time.Second)):
		}
		next, err := waitScenarioSnapshot(ctx, probe, poll, nil)
		if err != nil {
			return current, fmt.Errorf("rate amendment source readiness: %w", err)
		}
		current = next
		if err := retain(current); err != nil {
			return current, err
		}
	}
}
