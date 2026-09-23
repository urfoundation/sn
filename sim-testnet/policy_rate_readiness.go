// Rate activation admits no settlement credit. A complete signed source and a
// conservative native-floor preflight decide when a new interval may begin.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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
}

// Require the last complete epoch under the successor policy, covering every
// operator and every conviction tier at twice the immutable native minimum.
func validatePolicyRateReadiness(cfg *ResolvedConfig, contracts *ContractView, sources []PolicyRateSourceObservation, price *big.Int) error {
	if cfg == nil || cfg.Config == nil || cfg.previousPolicy == nil || validateFuturePolicyRateAmendment(cfg.previousPolicy, cfg.Policy) != nil || contracts == nil || contracts.PolicyHash != cfg.PolicyHash || contracts.Policy.EffectiveEpoch == 0 || contracts.CurrentEpoch <= contracts.Policy.EffectiveEpoch || price == nil || price.Sign() <= 0 || contracts.MinimumTransferRao == 0 || cfg.Policy.Deposit.UsageLagEpochs != 1 {
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
		for _, tier := range cfg.Policy.Deposit.Tiers {
			deposit, _, err := protocol.RequiredDepositRao(source.TotalUsageBytes, new(big.Int).SetUint64(tier.MinConvictionRao), cfg.Policy.Deposit)
			if err != nil {
				return err
			}
			// Ignoring the two-rao reserve rounding allowance is conservative.
			equivalent := new(big.Int).Quo(new(big.Int).Mul(deposit, price), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
			if equivalent.Cmp(minimum) < 0 {
				return fmt.Errorf("operator %d complete epoch %d bytes=%d gives %s tao rao at tier %d; require twice native minimum %s", source.NoId, source.Epoch, source.TotalUsageBytes, equivalent, tier.MinConvictionRao, minimum)
			}
		}
	}
	return nil
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
	if err := validatePolicyRateReadiness(self.cfg, contracts, result.Sources, price); err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Ready, result.Detail = true, "complete current-policy source epoch clears twice the native minimum at every rate tier"
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
		if current != nil && current.PolicyRateReadiness != nil && current.PolicyRateReadiness.Ready {
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
