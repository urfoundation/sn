//go:build linux || darwin

package main

// A cap-only review cannot change actions or assert new chain/release facts.
// Exact provisional setup adoption authenticates its retained receipts later;
// strict execution and final acceptance still require current release checks.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
)

// This read-only option pins its source. It cannot be combined with an apply,
// launch, or provisional runtime invocation that could make new assertions.
func validateAllowanceOnlyOptions(command string, options cliOptions) error {
	if !options.AllowanceOnly {
		return nil
	}
	if command != "plan" || options.Apply || options.ProvisionalResume || options.Detach || options.PrepareOnly || !validCanonicalHashHex(options.PlanHash) {
		return errors.New("--allowance-only requires read-only plan and the exact active --plan-hash")
	}
	return nil
}

// Authenticate the source with its original two limits and every other current
// operational input unchanged. No RPC observation is needed to raise a cap
// while preserving the exact already approved outflows and transaction intents.
func buildAllowanceOnlyPlan(ctx context.Context, cfg *ResolvedConfig, stateDir, sourceHash string) (*SetupPlan, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || !validCanonicalHashHex(sourceHash) {
		return nil, errors.New("allowance review requires its configured exact source approval")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Config.Deployment.Network != "bittensor-testnet" || cfg.Config.Deployment.Subnet != "existing" || cfg.ChainID != testnetChainID {
		return nil, errors.New("allowance-only review is restricted to retained testnet custody")
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "plan.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	source, err := decodePersistedPlanWire(raw)
	if err != nil || source.PlanHash != sourceHash {
		return nil, errors.Join(errors.New("allowance review source differs from its exact active hash"), err)
	}
	target := configuredPlanLimits(cfg)
	comparison, err := target.EVMGasWei.Cmp(source.Limits.EVMGasWei)
	if err != nil || comparison < 0 || target.TAORao < source.Limits.TAORao || target.AlphaRao != source.Limits.AlphaRao || target.Registrations != source.Limits.Registrations || target.SubnetCreations != source.Limits.SubnetCreations {
		return nil, errors.Join(errors.New("allowance-only review may raise only the EVM and total TAO caps"), err)
	}
	original := *cfg
	original.MaximumEVMGasWei = source.Limits.EVMGasWei
	original.MaximumTAORao = source.Limits.TAORao
	// This checks resolved RPC/wallet identity, configuration, policy, custody,
	// current artifact semantics and all existing monetary/action constraints.
	source, err = loadPlanIdentityBytes(&original, raw, true)
	if err != nil {
		return nil, err
	}
	if source.Limits == target {
		return source, nil
	}
	allocation, err := planRevisionEVMFundingAllocation(cfg, source)
	if err != nil {
		return nil, err
	}
	plan := *source
	plan.Limits = target
	plan.EVMFundingAllocationWei = allocation
	plan.PriorPlanHashes = append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
	plan.ResolvedInputsHash, err = resolvedInputsHash(cfg)
	if err != nil {
		return nil, err
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		return nil, err
	}
	wire, err := json.Marshal(&plan)
	if err != nil {
		return nil, err
	}
	// The same exact-identity loader used by provisional setup must accept the
	// reviewed result. It retains the original release; it grants no acceptance.
	if _, err := loadPlanIdentityBytes(cfg, wire, true); err != nil {
		return nil, err
	}
	latest, err := readValidatorEvidenceHistoricalFile(stateDir, "plan.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil || !bytes.Equal(latest, raw) || ctx.Err() != nil {
		return nil, errors.Join(errors.New("allowance review source changed during construction"), err, ctx.Err())
	}
	return &plan, nil
}
