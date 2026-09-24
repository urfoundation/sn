//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// Rollover authenticates a distinct activation subplan. Its connection owner
// needs no deployment payloads, setup signers, or historical deployer replay;
// ordinary launch and final audit retain those independent checks.
func preparePolicyRolloverExecutorV2(ctx context.Context, authorized, runtime *ResolvedConfig, stateDir string, plan *SetupPlan, journal *Journal, roles *RoleSecrets) (*Executor, error) {
	if ctx == nil || authorized == nil || authorized.Config == nil || runtime == nil || plan == nil || plan.ValidatorEvidence == nil || journal == nil || roles == nil {
		return nil, errors.New("policy rollover executor authority is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateOwnedRPCPlan(authorized, plan); err != nil {
		return nil, err
	}
	if err := validateRuntimeConfigIdentityPlan(authorized, plan); err != nil {
		return nil, err
	}
	if err := validateExecutionRPCConfiguration(authorized); err != nil {
		return nil, fmt.Errorf("policy rollover RPC configuration: %w", err)
	}
	if err := validateCampaignRPCTransport(authorized, runtime); err != nil {
		return nil, err
	}
	if plan.ConfigHash != authorized.ConfigHash || plan.PolicyHash != authorized.PolicyHash || plan.DeploymentID != authorized.Config.Deployment.DeploymentID || plan.ChainID != testnetChainID || plan.Netuid != authorized.Netuid || roles.Schema != "urnetwork-sim-role-secrets-v1" || roles.DeploymentID != plan.DeploymentID {
		return nil, errors.New("policy rollover executor changed its approved deployment, policy or roles")
	}
	keeper, err := roles.EVMAddress("keeper")
	if err != nil || keeper != common.HexToAddress(plan.Roles.Keeper) || keeper == (common.Address{}) {
		return nil, errors.Join(errors.New("policy rollover keeper differs from approved deployment role"), err)
	}
	return &Executor{cfg: runtime, auditAuthorizedConfig: authorized, stateDir: stateDir, plan: plan, planActions: planActionIndex(plan), planActionsOwner: plan, journal: journal, roles: roles}, nil
}

// The command reauthenticates its canonical snapshots, budget, dual consents
// and exact finalized journal contract state before using this retained keeper.
func newPolicyRolloverExecutorV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, journal *Journal, roles *RoleSecrets) (*Executor, error) {
	runtime := cfg
	var err error
	if cfg != nil && cfg.provisionalRPCAuthority != "" {
		runtime, err = campaignRPCConfig(cfg)
		if err != nil {
			return nil, err
		}
	}
	self, err := preparePolicyRolloverExecutorV2(ctx, cfg, runtime, stateDir, plan, journal, roles)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			self.Close()
		}
	}()
	self.substrate, err = DialSubstrateManagerContext(ctx, runtime, stateDir, journal)
	if err != nil {
		return nil, err
	}
	self.keeper, err = DialEvmTxManager(ctx, runtime, stateDir, journal, roles, "keeper")
	if err != nil {
		return nil, err
	}
	if independentRPCRequired(runtime) {
		self.independentSubstrate, err = DialIndependentSubstrateManager(runtime)
		if err != nil {
			return nil, err
		}
		self.independentEVM, err = dialConfiguredEVMClient(ctx, runtime, verificationEVMEndpoint(runtime))
		if err != nil {
			return nil, err
		}
		chainID, err := self.independentEVM.ChainID(ctx)
		if err != nil || chainID == nil || !chainID.IsUint64() || chainID.Uint64() != plan.ChainID {
			return nil, errors.Join(errors.New("policy rollover independent chain identity changed"), err)
		}
	}
	opened = true
	return self, nil
}
