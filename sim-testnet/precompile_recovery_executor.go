// Supplemental custody cleanup authenticates its own exact authority before
// opening the sole sender. Deployment reconstruction remains a final audit;
// it cannot prevent reconciliation of an already signed repair transaction.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// Local authentication precedes every connection. The returned owner has no
// payloads or transaction manager until the immutable on-chain probe is checked.
func preparePrecompileRecoveryExecutor(ctx context.Context, authorizedCfg, runtimeCfg *ResolvedConfig, stateDir string, plan *SetupPlan, journal *Journal, roles *RoleSecrets, evidence *PrecompileConformanceEvidence) (*Executor, error) {
	if ctx == nil || journal == nil || evidence == nil || evidence.Recovery == nil {
		return nil, errors.New("probe recovery executor has no exact authority or journal owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateOwnedRPCPlan(authorizedCfg, plan); err != nil {
		return nil, err
	}
	if err := validateRuntimeConfigIdentityPlan(authorizedCfg, plan); err != nil {
		return nil, err
	}
	if err := validateExecutionRPCConfiguration(authorizedCfg); err != nil {
		return nil, fmt.Errorf("probe recovery RPC configuration: %w", err)
	}
	if err := validateCampaignRPCTransport(authorizedCfg, runtimeCfg); err != nil {
		return nil, err
	}
	if err := validateCoordinatorRepairSigningRoles(plan, roles); err != nil {
		return nil, err
	}
	self := &Executor{cfg: runtimeCfg, stateDir: stateDir, plan: plan, planActions: planActionIndex(plan), planActionsOwner: plan, journal: journal, roles: roles, auditAuthorizedConfig: authorizedCfg, precompileRecoveryOnly: true}
	if err := self.validatePrecompileEvidence(approvedPrecompileProbe(plan), evidence); err != nil {
		return nil, err
	}
	if err := validatePrecompileRecoveryPlan(plan, evidence, &evidence.Recovery.Authorization); err != nil {
		return nil, err
	}
	if err := validatePrecompileRecoveryGasRevisionJournal(plan, evidence, journal.Entries()); err != nil {
		return nil, err
	}
	for label, expected := range map[string]string{validatorHotkeyLabel(1): evidence.SampleHotkey, validatorHotkeyLabel(2): evidence.MoveHotkey, fleetColdkeyLabel(1): evidence.RecoveryColdkey} {
		key, err := roleBytes32(roles, label)
		if err != nil || !strings.EqualFold(expected, hexBytesValue(key[:])) {
			return nil, stateMismatchError(err, "probe recovery retained custody role %s changed", label)
		}
	}
	anchor := evidence.Recovery.Authorization.Request.Budget.JournalHash
	found := false
	for _, entry := range journal.Entries() {
		if entry.EntryHash == anchor && entry.PlanHash == plan.PlanHash {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("probe recovery executor lost its signed journal anchor")
	}
	if evidence.Dividend.DeltaRao == 0 {
		if len(evidence.Recovery.Steps) != 0 || evidence.Transfer.TransactionHash != "" {
			return nil, errors.New("probe recovery operations precede the dividend checkpoint")
		}
	} else if _, _, _, err := precompileRecoveryPositions(evidence); err != nil {
		return nil, err
	}
	return self, nil
}

// Opens only the deployer and native read connection required by this repair.
// No other setup signer, deployment audit, or nonce census runs at admission.
func newPrecompileRecoveryExecutor(ctx context.Context, authorizedCfg, runtimeCfg *ResolvedConfig, stateDir string, plan *SetupPlan, journal *Journal, roles *RoleSecrets, evidence *PrecompileConformanceEvidence) (*Executor, error) {
	self, err := preparePrecompileRecoveryExecutor(ctx, authorizedCfg, runtimeCfg, stateDir, plan, journal, roles, evidence)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			self.Close()
		}
	}()
	self.substrate, err = DialSubstrateManagerContext(ctx, runtimeCfg, stateDir, journal)
	if err != nil {
		return nil, err
	}
	self.deployer, err = DialEvmTxManager(ctx, runtimeCfg, stateDir, journal, roles, "deployer")
	if err != nil {
		return nil, err
	}
	if independentRPCRequired(runtimeCfg) {
		self.independentSubstrate, err = DialIndependentSubstrateManager(runtimeCfg)
		if err != nil {
			return nil, err
		}
		self.independentEVM, err = dialConfiguredEVMClient(ctx, runtimeCfg, verificationEVMEndpoint(runtimeCfg))
		if err != nil {
			return nil, err
		}
		chainId, err := self.independentEVM.ChainID(ctx)
		if err != nil || chainId == nil || !chainId.IsUint64() || chainId.Uint64() != plan.ChainID {
			return nil, stateMismatchError(err, "probe recovery independent chain identity changed")
		}
	}
	if err := self.authenticatePrecompileRecoveryRuntime(ctx, evidence); err != nil {
		return nil, err
	}
	opened = true
	return self, nil
}

// The signed runtime includes the probe's immutable owner and subnet. Checking
// it needs no finalized transaction-prefix census, so a saved pending call can
// reach the durable sender and finish its original nonce before strict replay.
func (self *Executor) authenticatePrecompileRecoveryRuntime(ctx context.Context, evidence *PrecompileConformanceEvidence) error {
	if self == nil || !self.precompileRecoveryOnly || self.deployer == nil || self.deployer.client == nil || evidence == nil || evidence.Recovery == nil {
		return errors.New("probe recovery runtime authentication is incomplete")
	}
	if err := validatePrecompileRecoveryPlan(self.plan, evidence, &evidence.Recovery.Authorization); err != nil {
		return err
	}
	head, err := finalizedEVMHead(ctx, self.deployer.client)
	if err != nil {
		return err
	}
	request := evidence.Recovery.Authorization.Request
	code, err := self.deployer.client.CodeAt(ctx, request.Probe, new(big.Int).SetUint64(head.Number))
	if err != nil || len(code) == 0 || crypto.Keccak256Hash(code).Hex() != request.ProbeRuntimeHash {
		return stateMismatchError(err, "probe recovery immutable runtime differs from its signed authority")
	}
	if self.independentEVM != nil {
		if err := verifyEVMCheckpoint(ctx, self.independentEVM, head, head); err != nil {
			return err
		}
		code, err := self.independentEVM.CodeAt(ctx, request.Probe, new(big.Int).SetUint64(head.Number))
		if err != nil || len(code) == 0 || crypto.Keccak256Hash(code).Hex() != request.ProbeRuntimeHash {
			return stateMismatchError(err, "probe recovery independent immutable runtime changed")
		}
	}
	self.payloads = &DeploymentPayloads{PrecompileProbeAddress: request.Probe}
	return nil
}

// A partial payload is never general setup authority. Both dispatch entry
// points restrict this owner before any journal append or transaction attempt.
func (self *Executor) validatePrecompileRecoveryDispatch(action Action) error {
	if self == nil || !self.precompileRecoveryOnly {
		return nil
	}
	if self.payloads == nil {
		return errors.New("probe recovery runtime has not been authenticated")
	}
	if strings.HasPrefix(action.ID, precompileRecoveryActionPrefix) {
		evidence, err := loadPrecompileEvidence(self.stateDir)
		if err != nil {
			return err
		}
		_, err = self.precompileRecoveryStepForAction(action, evidence)
		return err
	}
	if action.ID == "precompile.dividend" || action.ID == "precompile.transfer-out" {
		approved, err := exactPlanActionByID(self.plan, action.ID)
		if err == nil && precompileRecoveryActionEqual(approved, action) {
			return nil
		}
	}
	return errors.New("probe recovery executor cannot dispatch an unrelated or changed setup action")
}
