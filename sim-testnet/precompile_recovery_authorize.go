// The approval command prepares a reviewable supplemental request and can
// publish its signatures while the release keeps sole ownership of the journal.
package main

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Publishing an authorization is separate from broadcasting either operation.
func validatePrecompileRecoveryOptions(command string, options cliOptions) error {
	if command != "probe-recovery" {
		if options.ProbeRecoveryBudget != "" || options.ProbeRecoveryBudgetSHA256 != "" || options.ProbeRecoveryExecute {
			return errors.New("probe recovery budget options require probe-recovery")
		}
		return nil
	}
	if !options.ProvisionalResume || !validCanonicalHashHex(options.PlanHash) || options.Detach || options.PrepareOnly || options.Name != "" {
		return errors.New("probe-recovery requires an exact provisional plan and absolute reviewed budget path with SHA256; --apply publishes authorization only")
	}
	if options.ProbeRecoveryExecute {
		if !options.Apply || options.ProbeRecoveryBudget != "" || options.ProbeRecoveryBudgetSHA256 != "" {
			return errors.New("probe recovery execution requires --apply and the already published authorization")
		}
	} else if options.ProbeRecoveryBudget == "" && options.ProbeRecoveryBudgetSHA256 == "" && !options.Apply {
		return nil
	} else if !filepath.IsAbs(options.ProbeRecoveryBudget) || !validCoordinatorRepairSHA256(options.ProbeRecoveryBudgetSHA256) {
		return errors.New("probe recovery authorization requires an absolute reviewed budget path and SHA256")
	}
	return nil
}

// Constructs only semantic authority. Current nonces and stake quotes belong
// to the existing writer immediately before each durable transaction is signed.
func newPrecompileRecoveryRequest(plan *SetupPlan, evidence *PrecompileConformanceEvidence, budget PrecompileRecoveryBudget) (PrecompileRecoveryRequest, error) {
	if plan == nil || plan.PrecompileProbeSuccessor == nil || evidence == nil || evidence.Back.FromAfterRao == 0 || !precompileRoundTripAccounted(evidence) || !validConformanceTransaction(evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockHash, evidence.Snapshot.BlockNumber) {
		return PrecompileRecoveryRequest{}, errors.New("probe recovery has no prepared outstanding liability")
	}
	basis, err := precompileRecoveryBasisHash(evidence)
	if err != nil {
		return PrecompileRecoveryRequest{}, err
	}
	budgetHash, err := canonicalHashHex(budget)
	if err != nil {
		return PrecompileRecoveryRequest{}, err
	}
	action, err := exactPlanActionByID(plan, "precompile.transfer-out")
	if err != nil {
		return PrecompileRecoveryRequest{}, err
	}
	fee := min(plan.MaximumEVMFeePerGasWei, uint64(100_000_000_000))
	return PrecompileRecoveryRequest{Schema: "urnetwork-precompile-recovery-v1", PlanHash: plan.PlanHash, ConfigHash: evidence.ConfigHash, DeploymentId: plan.DeploymentID, ChainId: plan.ChainID, GenesisHash: plan.GenesisHash, Netuid: plan.Netuid,
		Owner: common.HexToAddress(plan.Roles.Owner), Deployer: common.HexToAddress(plan.Roles.Deployer), Probe: approvedPrecompileProbe(plan), ProbeRuntimeHash: action.Parameters[precompileProbeRuntimeParameter], SampleHotkey: evidence.SampleHotkey, MoveHotkey: evidence.MoveHotkey, RecoveryColdkey: evidence.RecoveryColdkey, BasisHash: basis,
		OriginalEvidenceHash: evidence.EvidenceHash, ReseedTaoRao: plan.LiveFacts.ProbeTAORao, MaximumReseeds: precompileRecoveryMaximumReseeds, TopUpRao: precompileRecoveryTopUpRao, MaximumSteps: precompileRecoveryMaximumSteps, MaximumGasUnits: precompileRecoveryGasUnits, MaximumFeePerGasWei: fee, BudgetHash: budgetHash, Budget: budget}, nil
}

// This command never opens a transaction manager or the exclusive journal.
// The consumer authenticates the retained anchor and the exact signed request.
func runPrecompileRecoveryAuthorization(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) error {
	if err := validatePrecompileRecoveryOptions("probe-recovery", options); err != nil {
		return err
	}
	plan, err := loadInvocationPlan(cfg, stateDir, "probe-recovery", options)
	if err != nil {
		return err
	}
	if err := prepareProvisionalResume(ctx, cfg, stateDir, "probe-recovery", options, plan); err != nil {
		return err
	}
	evidence, err := loadPrecompileEvidence(stateDir)
	if err != nil {
		return err
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return err
	}
	if options.ProbeRecoveryBudget == "" {
		budget, err := newPrecompileRecoveryBudget(cfg, stateDir, plan, entries)
		return printResult(options.Format, budget, err)
	}
	owner := &Executor{cfg: cfg, plan: plan, stateDir: stateDir, journal: &Journal{entries: entries}}
	if err := owner.validatePrecompileEvidence(approvedPrecompileProbe(plan), evidence); err != nil {
		return err
	}
	raw, err := readCoordinatorRepairInput(options.ProbeRecoveryBudget, options.ProbeRecoveryBudgetSHA256)
	if err != nil {
		return err
	}
	var budget PrecompileRecoveryBudget
	if err := decodeExactCoordinatorRepairJSON(raw, &budget); err != nil {
		return err
	}
	request, err := newPrecompileRecoveryRequest(plan, evidence, budget)
	if err != nil {
		return err
	}
	var roles RoleSecrets
	if err := readJSONFile(filepath.Join(stateDir, "secrets", "roles.json"), &roles); err != nil {
		return err
	}
	if err := validateCoordinatorRepairSigningRoles(plan, &roles); err != nil {
		return err
	}
	authorization := PrecompileRecoveryAuthorization{Request: request}
	for _, roleName := range []string{coordinatorRepairOwnerRole, coordinatorRepairDeployerRole} {
		role, err := roles.EVMKey(roleName)
		if err != nil {
			return err
		}
		key, err := crypto.HexToECDSA(role.PrivateKeyHex)
		if err != nil {
			return err
		}
		hash, signature, err := coordinatorRepairSignature(request, key)
		if err != nil {
			return err
		}
		authorization.Hash = hash
		if roleName == coordinatorRepairOwnerRole {
			authorization.OwnerSignature = signature
		} else {
			authorization.DeployerSignature = signature
		}
	}
	if err := validatePrecompileRecoveryPlan(plan, evidence, &authorization); err != nil {
		return err
	}
	entries, err = readJournalEntries(stateDir)
	if err != nil {
		return err
	}
	found := false
	for _, entry := range entries {
		if entry.EntryHash == budget.JournalHash && entry.PlanHash == plan.PlanHash {
			found = true
			break
		}
	}
	if !found {
		return errors.New("probe recovery budget anchor is absent from retained journal")
	}
	if options.Apply {
		path := filepath.Join(stateDir, precompileRecoveryAuthorizationFilename)
		if err := validatePrecompileRecoveryArtifactPath(stateDir, path); err != nil {
			return err
		}
		if err := writeCoordinatorRepairFile(path, &authorization); err != nil {
			return err
		}
	}
	return printResult(options.Format, map[string]any{"command": "probe-recovery", "authorization": authorization, "published": options.Apply, "transactions_sent": 0, "plan_hash": plan.PlanHash}, nil)
}

func validatePrecompileRecoveryArtifactPath(stateDir, path string) error {
	if err := rejectFinalArtifactSymlinkComponents(stateDir, filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("probe recovery authorization path is not a regular file")
	}
	return nil
}

// A read-only proposal accounts for signed and queued liabilities before its
// explicit reserve suballocation is reviewed and signed. Execution rechecks
// current exposure so the saved observation never freezes a stale nonce/cap.
func newPrecompileRecoveryBudget(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry) (*PrecompileRecoveryBudget, error) {
	if plan == nil || plan.LiveFacts.ProbeTAORao == 0 || plan.MaximumEVMFeePerGasWei == 0 {
		return nil, errors.New("probe recovery budget has no retained limits")
	}
	reserve, err := exactPlanActionByID(plan, "campaign.evm-gas-reserve")
	if err != nil {
		return nil, err
	}
	reserved, err := reserve.Spend.EVMGasWei.Big()
	if err != nil {
		return nil, err
	}
	external, err := readFleetRenewalQueueTransactions(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	exposure, err := fleetRenewalCampaignExposure(stateDir, plan, entries, external)
	if err != nil {
		return nil, err
	}
	liability, err := exposure.Liability.Big()
	if err != nil {
		return nil, err
	}
	fee := min(plan.MaximumEVMFeePerGasWei, uint64(100_000_000_000))
	maximum := new(big.Int).Mul(new(big.Int).SetUint64(precompileRecoveryMaximumSteps*precompileRecoveryGasUnits), new(big.Int).SetUint64(fee))
	reseed := new(big.Int).Mul(new(big.Int).SetUint64(plan.LiveFacts.ProbeTAORao), big.NewInt(precompileRecoveryMaximumReseeds*1_000_000_000))
	maximum.Add(maximum, reseed)
	anchor := ""
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].PlanHash == plan.PlanHash && validCanonicalHashHex(entries[index].EntryHash) {
			anchor = entries[index].EntryHash
			break
		}
	}
	if anchor == "" {
		return nil, errors.New("probe recovery proposal has no current-plan journal anchor")
	}
	budget := &PrecompileRecoveryBudget{Schema: "urnetwork-precompile-recovery-budget-v1", PlanHash: plan.PlanHash, JournalHash: anchor, CampaignReserveWei: reserve.Spend.EVMGasWei, CommittedOrPendingMaxWei: DecimalUint(liability.String()), MaximumRecoveryWei: DecimalUint(maximum.String()), MaximumReseedWei: DecimalUint(reseed.String()), Verified: true}
	if err := allocatePrecompileRecoveryBudget(plan, budget, reserved, liability, maximum); err != nil {
		return nil, err
	}
	return budget, nil
}
