package main

// Historical receipts retain the assurance of their original approval. An
// owned strict continuation may authenticate that original label, but every
// live replay still uses the current strict executor and independent reader.
import (
	"errors"
	"fmt"
)

func ownedRPCUpgradesHistoricalAssurance(cfg *ResolvedConfig, record *ActionPostcondition) bool {
	return cfg != nil && cfg.ownedRPCAuthority != "" && cfg.OperationalRPCMode == rpcModePrivateAuthority && !provisionalResumeEnabled(cfg) && record != nil && record.OperationalRPCMode == rpcModePublicOverride && !record.IndependentRPC
}

func historicalPostconditionRPCIdentity(stateDir string, cfg *ResolvedConfig, scope *SetupPlan, record *ActionPostcondition) error {
	if cfg == nil || cfg.Config == nil || scope == nil || record == nil {
		return errors.New("historical RPC assurance context is incomplete")
	}
	if record.OperationalRPCMode == cfg.OperationalRPCMode && record.IndependentRPC == independentRPCRequired(cfg) {
		return nil
	}
	if !ownedRPCUpgradesHistoricalAssurance(cfg, record) || cfg.provisionalRPCAuthority != "" || !scope.allowedPlanHashes()[record.PlanHash] {
		return errors.New("historical postcondition RPC assurance differs from its approved source")
	}
	// A campaign executor uses the exact owned EVM hop, while its authorization
	// and every native read remain bound to the literal LAN node.
	if cfg.OperationalEVM != "http://"+cfg.ownedRPCAuthority && cfg.OperationalEVM != "http://"+campaignEVMAuthority() {
		return errors.New("historical RPC reader substituted its owned EVM transport")
	}
	canonical := *cfg
	canonical.OperationalEVM = "http://" + cfg.ownedRPCAuthority
	if err := validateExecutionRPCConfiguration(&canonical); err != nil {
		return err
	}
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, record.PlanHash)
	if err != nil {
		return fmt.Errorf("historical RPC original plan: %w", err)
	}
	if source.PlanHash != record.PlanHash || source.DeploymentID != scope.DeploymentID || source.DeploymentID != cfg.Config.Deployment.DeploymentID || source.ChainID != cfg.ChainID || source.GenesisHash != testnetGenesis || source.Netuid != cfg.Netuid || source.Owner != cfg.WalletPublic || source.OwnedRPCAuthority != "" {
		return errors.New("historical RPC source is not the same deployment's original public approval")
	}
	// The source's own immutable input hash authenticates its original route.
	// Reconstruct only from retained current identity inputs and that source's
	// approved limits; no supplied receipt label can choose its own endpoint.
	original := canonical
	original.ownedRPCAuthority = ""
	original.OperationalSubstrate, original.OperationalEVM, original.OperationalRPCMode, err = resolveOperationalRPCs(original.Authority, original.Config.LaunchInputs.PublicSubstrateRPCOverride, original.Config.LaunchInputs.PublicEVMRPCOverride)
	if err != nil {
		return err
	}
	original.MaximumTAORao, original.MaximumAlphaRao, original.MaximumEVMGasWei = source.Limits.TAORao, source.Limits.AlphaRao, source.Limits.EVMGasWei
	resolvedHash, err := resolvedInputsHash(&original)
	if err != nil || resolvedHash != source.ResolvedInputsHash || original.OperationalRPCMode != record.OperationalRPCMode {
		return errors.Join(fmt.Errorf("historical RPC authority cannot reproduce source %s resolved inputs", source.PlanHash), err)
	}
	if record.SubstrateFinalized != record.IndependentSubstrateFinalized || record.EVMFinalized != record.IndependentEVMFinalized || !finalJSONEqual(record.Observed, record.IndependentObserved) {
		return errors.New("historical public RPC receipt does not preserve its original shared observation")
	}
	return nil
}
