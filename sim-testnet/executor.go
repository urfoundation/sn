package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/crv4"
	minercomponent "github.com/urfoundation/sn/miner"
	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

type Executor struct {
	cfg                     *ResolvedConfig
	stateDir                string
	plan                    *SetupPlan
	journal                 *Journal
	roles                   *RoleSecrets
	substrate               *SubstrateManager
	independentSubstrate    *SubstrateManager
	independentEVM          *ethclient.Client
	deployer, owner         *EVMTxManager
	guardian                *EVMTxManager
	oracle, keeper          *EVMTxManager
	deposits                map[int]*EVMTxManager
	payloads                *DeploymentPayloads
	releaseGate             *ReleaseCampaignGate
	carriedVerificationKeys map[string]bool
	carriedFleetHistoryKeys map[string]bool
}

// NewExecutor opens transaction managers only against the canonical endpoint
// selection which was validated and hashed into the approved plan.
func NewExecutor(ctx context.Context, cfg *ResolvedConfig, stateDir string, p *SetupPlan, j *Journal, roles *RoleSecrets) (*Executor, error) {
	return newExecutorWithTransport(ctx, cfg, cfg, stateDir, p, j, roles)
}

// NewCampaignExecutor retains the canonical endpoint authorization check but
// sends live-topology EVM traffic through the simulator-owned aggregate gate.
func NewCampaignExecutor(ctx context.Context, cfg *ResolvedConfig, stateDir string, p *SetupPlan, j *Journal, roles *RoleSecrets) (*Executor, *ResolvedConfig, error) {
	runtimeCfg, err := campaignRPCConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	executor, err := newExecutorWithTransport(ctx, cfg, runtimeCfg, stateDir, p, j, roles)
	if err != nil {
		return nil, nil, err
	}
	return executor, runtimeCfg, nil
}

// newExecutorWithTransport separates immutable route authorization from the
// local transport hop used during a supervised campaign.
func newExecutorWithTransport(ctx context.Context, authorizedCfg, runtimeCfg *ResolvedConfig, stateDir string, p *SetupPlan, j *Journal, roles *RoleSecrets) (*Executor, error) {
	if err := validateExecutionRPCConfiguration(authorizedCfg); err != nil {
		return nil, fmt.Errorf("execution RPC configuration: %w", err)
	}
	if err := validateCampaignRPCTransport(authorizedCfg, runtimeCfg); err != nil {
		return nil, err
	}
	s, err := DialSubstrateManager(runtimeCfg, stateDir, j)
	if err != nil {
		return nil, err
	}
	d, err := DialEVMTxManager(ctx, runtimeCfg, stateDir, j, roles, "deployer")
	if err != nil {
		s.Close()
		return nil, err
	}
	o, err := DialEVMTxManager(ctx, runtimeCfg, stateDir, j, roles, "testnet-owner")
	if err != nil {
		s.Close()
		d.Close()
		return nil, err
	}
	guardian, err := DialEVMTxManager(ctx, runtimeCfg, stateDir, j, roles, "guardian")
	if err != nil {
		s.Close()
		d.Close()
		o.Close()
		return nil, err
	}
	oracle, err := DialEVMTxManager(ctx, runtimeCfg, stateDir, j, roles, "commitment-oracle")
	if err != nil {
		s.Close()
		d.Close()
		o.Close()
		guardian.Close()
		return nil, err
	}
	keeper, err := DialEVMTxManager(ctx, runtimeCfg, stateDir, j, roles, "keeper")
	if err != nil {
		s.Close()
		d.Close()
		o.Close()
		guardian.Close()
		oracle.Close()
		return nil, err
	}
	deposits := map[int]*EVMTxManager{}
	for i := 1; i <= runtimeCfg.Config.Topology.Operators; i++ {
		manager, dialErr := DialEVMTxManager(ctx, runtimeCfg, stateDir, j, roles, fmt.Sprintf("operator-%d-deposit", i))
		if dialErr != nil {
			for _, opened := range deposits {
				opened.Close()
			}
			s.Close()
			d.Close()
			o.Close()
			guardian.Close()
			oracle.Close()
			keeper.Close()
			return nil, dialErr
		}
		deposits[i] = manager
	}
	e := &Executor{cfg: runtimeCfg, stateDir: stateDir, plan: p, journal: j, roles: roles, substrate: s, deployer: d, owner: o, guardian: guardian, oracle: oracle, keeper: keeper, deposits: deposits}
	if !independentRPCRequired(runtimeCfg) {
		if err := e.ensurePayloads(ctx); err != nil {
			e.Close()
			return nil, fmt.Errorf("contract deployment preflight: %w", err)
		}
		return e, nil
	}
	e.independentSubstrate, err = DialIndependentSubstrateManager(runtimeCfg)
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("independent Substrate RPC: %w", err)
	}
	e.independentEVM, err = dialConfiguredEVMClient(ctx, runtimeCfg, runtimeCfg.Public.Chain.EVMPublicReadEndpoint)
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("independent EVM RPC: %w", err)
	}
	independentChainID, err := e.independentEVM.ChainID(ctx)
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("independent EVM RPC chain id: %w", err)
	}
	if independentChainID.Uint64() != testnetChainID {
		e.Close()
		return nil, fmt.Errorf("independent EVM RPC chain id=%d, want %d", independentChainID.Uint64(), testnetChainID)
	}
	if err := e.ensurePayloads(ctx); err != nil {
		e.Close()
		return nil, fmt.Errorf("contract deployment preflight: %w", err)
	}
	return e, nil
}
func (e *Executor) Close() {
	for _, manager := range e.deposits {
		manager.Close()
	}
	if e.keeper != nil {
		e.keeper.Close()
	}
	if e.oracle != nil {
		e.oracle.Close()
	}
	if e.guardian != nil {
		e.guardian.Close()
	}
	if e.owner != nil {
		e.owner.Close()
	}
	if e.deployer != nil {
		e.deployer.Close()
	}
	if e.substrate != nil {
		e.substrate.Close()
	}
	if e.independentSubstrate != nil {
		e.independentSubstrate.Close()
	}
	if e.independentEVM != nil {
		e.independentEVM.Close()
	}
}

// Identify commands which operate the live server topology.
func requiresManagedDependencies(command string) bool {
	switch command {
	case "launch", "resume", "scenario":
		return true
	default:
		return false
	}
}

// Identify commands which can create or replace supervised processes.
func requiresReleaseBinaries(command string) bool {
	return command == "launch" || command == "resume"
}

// Subtract each exact terminal action once while retaining future campaign and
// retirement reserves which are not setup transactions themselves.
func remainingPlanSpend(plan *SetupPlan, entries []JournalEntry) (Spend, error) {
	if plan == nil {
		return Spend{}, errors.New("approved plan is unavailable")
	}
	remaining := plan.MaximumSpend
	verified := map[string]bool{}
	allowedPlans := plan.allowedPlanHashes()
	for _, entry := range entries {
		if allowedPlans[entry.PlanHash] && entry.Stage == StageVerified {
			verified[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	for _, action := range plan.Actions {
		verifiedAction := verified[action.ID+"\x00"+action.IntentHash]
		for _, accepted := range action.AcceptedPriorIntentHashes {
			verifiedAction = verifiedAction || verified[action.ID+"\x00"+accepted]
		}
		if action.Kind == "substrate-reconciliation" {
			verifiedAction = verifiedAction || hasFinalizedAlphaRecoveryEvidence(plan, action, entries)
		}
		if !verifiedAction || action.Kind == "budget-reserve" {
			continue
		}
		gasComparison, gasErr := action.Spend.EVMGasWei.Cmp(remaining.EVMGasWei)
		if gasErr != nil || action.Spend.TAORao > remaining.TAORao || action.Spend.AlphaRao > remaining.AlphaRao || gasComparison > 0 || action.Spend.Registrations > remaining.Registrations || action.Spend.SubnetCreations > remaining.SubnetCreations {
			return Spend{}, fmt.Errorf("verified action %s spend exceeds the approved remaining budget", action.ID)
		}
		remaining.TAORao -= action.Spend.TAORao
		remaining.AlphaRao -= action.Spend.AlphaRao
		remaining.EVMGasWei, gasErr = subtractDecimalUint(remaining.EVMGasWei, action.Spend.EVMGasWei)
		if gasErr != nil {
			return Spend{}, fmt.Errorf("subtract verified action %s gas spend: %w", action.ID, gasErr)
		}
		remaining.Registrations -= action.Spend.Registrations
		remaining.SubnetCreations -= action.Spend.SubnetCreations
	}
	return remaining, nil
}

func hasFinalizedAlphaRecoveryEvidence(plan *SetupPlan, action Action, entries []JournalEntry) bool {
	if plan == nil || action.Kind != "substrate-reconciliation" {
		return false
	}
	planHash := action.Parameters[alphaRecoveryPlanHashParameter]
	intentHash := action.Parameters[alphaRecoveryIntentHashParameter]
	transactionHash := action.Parameters[alphaRecoveryTransactionHashParameter]
	block, err := strconv.ParseUint(action.Parameters[alphaRecoveryBlockParameter], 10, 64)
	if err != nil || block == 0 || !plan.allowedPlanHashes()[planHash] || intentHash == "" || transactionHash == "" {
		return false
	}
	for _, entry := range entries {
		if entry.PlanHash == planHash && entry.ActionID == action.ID && entry.IntentHash == intentHash && entry.Stage == StageFinalized && strings.EqualFold(entry.TransactionHash, transactionHash) && entry.BlockNumber == block && strings.EqualFold(entry.BlockHash, action.Parameters[alphaRecoveryBlockHashParameter]) {
			return true
		}
	}
	return false
}

// Extract the exact reviewed ceiling from both the plan and action. Every
// registration intent carries it so a runtime price move cannot widen an old
// approval.
func registrationBurnLimit(plan *SetupPlan, action Action) (uint64, error) {
	if plan == nil || plan.RegistrationBurnLimitRao == 0 || action.Spend.Registrations == 0 {
		return 0, errors.New("registration action has no approved burn limit")
	}
	limit, err := strconv.ParseUint(action.Parameters["maximum_burn_rao"], 10, 64)
	if err != nil || limit != plan.RegistrationBurnLimitRao {
		return 0, fmt.Errorf("registration action %s burn limit does not match its approved plan", action.ID)
	}
	return limit, nil
}

// Read the current auction price and enforce the reviewed limit immediately
// before constructing a native or EVM registration transaction.
func (e *Executor) boundedRegistrationBurn(action Action) (uint64, uint64, error) {
	limit, err := registrationBurnLimit(e.plan, action)
	if err != nil {
		return 0, 0, err
	}
	burn, err := e.readBurn()
	if err != nil {
		return 0, 0, err
	}
	if burn > limit {
		return 0, limit, fmt.Errorf("live registration burn %d exceeds approved limit %d", burn, limit)
	}
	return burn, limit, nil
}

// Contract registration receives the full approved ceiling. Runtime 454 burns
// the current rao price and the release contracts return any surplus.
func registrationFundingWei(limitRao uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(limitRao), big.NewInt(1_000_000_000))
}

// Classify one explicitly approved fleet-commitment concurrency group. The
// reserved parameter is rejected on every other action kind or identifier.
func fleetCommitmentParallelIdentity(action Action) (string, string, bool, error) {
	group := action.Parameters[fleetCommitmentParallelGroupParameter]
	if group == "" {
		return "", "", false, nil
	}
	if strings.TrimSpace(group) != group || len(group) > 64 || action.Kind != "substrate-extrinsic" {
		return "", "", false, fmt.Errorf("action %s has an invalid fleet commitment parallel group", action.ID)
	}
	kind := ""
	switch {
	case strings.HasPrefix(action.ID, "fleet.commitment."):
		kind = "install"
	case strings.HasPrefix(action.ID, "fleet.refresh.commitment."):
		kind = "refresh"
	default:
		return "", "", false, fmt.Errorf("action %s cannot use a fleet commitment parallel group", action.ID)
	}
	return group, kind, true, nil
}

// Locate one contiguous, bounded commitment group and reject internal
// dependencies which would make concurrent execution race its own DAG.
func fleetCommitmentParallelRange(actions []Action, start int) (int, bool, error) {
	if start < 0 || start >= len(actions) {
		return start, false, errors.New("fleet commitment parallel range start is out of bounds")
	}
	group, kind, grouped, err := fleetCommitmentParallelIdentity(actions[start])
	if err != nil || !grouped {
		return start + 1, false, err
	}
	end := start
	ids := map[string]bool{}
	for end < len(actions) {
		candidateGroup, candidateKind, candidateGrouped, candidateErr := fleetCommitmentParallelIdentity(actions[end])
		if candidateErr != nil {
			return end, false, candidateErr
		}
		if !candidateGrouped || candidateGroup != group {
			break
		}
		if candidateKind != kind {
			return end, false, fmt.Errorf("fleet commitment parallel group %s mixes install and refresh generations", group)
		}
		ids[actions[end].ID] = true
		end++
	}
	if end-start == 0 || end-start > fleetRefreshBatchSize {
		return end, false, fmt.Errorf("fleet commitment parallel group %s has %d actions", group, end-start)
	}
	for index := start; index < end; index++ {
		for _, dependency := range actions[index].DependsOn {
			if ids[dependency] {
				return end, false, fmt.Errorf("fleet commitment parallel action %s depends on group member %s", actions[index].ID, dependency)
			}
		}
	}
	return end, true, nil
}

// Validate every concurrency marker once while the complete action graph is
// still read-only. A group name may occur in only one contiguous range.
func validateFleetCommitmentParallelGroups(actions []Action) error {
	seen := map[string]bool{}
	for index := 0; index < len(actions); {
		group, _, grouped, err := fleetCommitmentParallelIdentity(actions[index])
		if err != nil {
			return err
		}
		if !grouped {
			index++
			continue
		}
		if seen[group] {
			return fmt.Errorf("fleet commitment parallel group %s is non-contiguous or duplicated", group)
		}
		end, _, err := fleetCommitmentParallelRange(actions, index)
		if err != nil {
			return err
		}
		seen[group] = true
		index = end
	}
	return nil
}

// Execute the setup prefix while allowing only independently signed fleet
// commitments to use bounded concurrency. Every action keeps its own journal
// state, postcondition, fee ceiling, and deterministic resume path.
func executeSetupActions(ctx context.Context, executor *Executor, actions []Action, limitID string) error {
	if executor == nil {
		return errors.New("setup action executor is unavailable")
	}
	for index := 0; index < len(actions); {
		end, grouped, err := fleetCommitmentParallelRange(actions, index)
		if err != nil {
			return err
		}
		if grouped {
			if err := runOrderedConcurrentAudits(end-index, fleetCommitmentParallelWorkers, func(offset int) error {
				return executor.Execute(ctx, actions[index+offset])
			}); err != nil {
				return err
			}
		} else {
			if err := executor.Execute(ctx, actions[index]); err != nil {
				return err
			}
		}
		for completed := index; completed < end; completed++ {
			if actions[completed].ID == limitID {
				return nil
			}
		}
		index = end
	}
	return nil
}

func runMutation(ctx context.Context, cmd string, cfg *ResolvedConfig, stateDir string, o cliOptions) error {
	if cmd == "retire" {
		return runRetirement(ctx, cfg, stateDir, o)
	}
	if cmd == "scenario" {
		names := []string{o.Name}
		if o.Name == releaseCandidateCampaignName {
			names = []string{"release-1.0", "production-soak"}
		}
		for _, name := range names {
			if _, err := scenarioDefinitionFor(cfg, name); err != nil {
				return err
			}
		}
	}
	p, planErr := loadPersistedPlan(cfg, stateDir)
	if !o.Apply {
		if planErr != nil {
			p, planErr = BuildPlanForState(ctx, cfg, stateDir)
		}
		if planErr != nil {
			return planErr
		}
		return printResult(o.Format, map[string]any{"dry_run": true, "command": cmd, "plan": p, "apply_command": fmt.Sprintf("sim-testnet %s --config %s --apply --plan-hash %s", cmd, cfg.ConfigPath, p.PlanHash)}, nil)
	}
	if err := ensurePrivateDir(stateDir); err != nil {
		return err
	}
	j, err := OpenJournal(stateDir)
	if err != nil {
		return err
	}
	defer j.Close()
	entries := j.Entries()
	if mayRefreshPersistedPlan(planErr, entries) {
		p, planErr = BuildPlan(ctx, cfg)
	} else if errors.Is(planErr, errPersistedPlanIdentityMismatch) {
		prior, priorErr := readPersistedPlan(stateDir)
		if priorErr != nil {
			return priorErr
		}
		p, planErr = BuildPlanRevision(ctx, cfg, stateDir, prior, entries)
	} else if planErr == nil {
		revisionRequired, revisionErr := fleetCommitmentRecoveryRequired(ctx, cfg, stateDir, p, entries)
		if revisionErr != nil {
			return revisionErr
		}
		if revisionRequired {
			p, planErr = BuildPlanRevision(ctx, cfg, stateDir, p, entries)
		}
	}
	if planErr != nil {
		return planErr
	}
	if err := requireApproved(true, o.PlanHash, p.PlanHash); err != nil {
		return err
	}
	remaining, err := remainingPlanSpend(p, entries)
	if err != nil {
		return err
	}
	// Approval is necessary but not sufficient: every apply re-runs all live
	// safety checks against the exact unverified spend so a persisted plan
	// cannot bypass changed RPCs, services, facts, repository locks, or host
	// readiness, while an honest partial deployment remains resumable.
	doctor := runDoctor(ctx, cfg, &doctorPlanBudget{Plan: p, Remaining: remaining, StateDir: stateDir})
	if err := doctor.Error(); err != nil {
		return fmt.Errorf("doctor must pass immediately before apply: %w", err)
	}
	roles, err := LoadOrWriteRoleSecrets(cfg, stateDir)
	if err != nil {
		return err
	}
	if err := writeRunInputs(cfg, stateDir, p, roles); err != nil {
		return err
	}
	// Finish all reversible host preflight before opening a transaction-capable
	// executor. In particular, a missing Docker daemon or a broken build must
	// never be discovered after contracts or registrations have been written.
	if requiresManagedDependencies(cmd) {
		if err := ensureOperatorConfigOverlays(cfg, stateDir); err != nil {
			return fmt.Errorf("prepare operator config overlays: %w", err)
		}
		if err := startDependencies(ctx, cfg); err != nil {
			return err
		}
	}
	var bins map[string]string
	if requiresReleaseBinaries(cmd) {
		bins, err = buildReleaseBinaries(ctx, cfg, stateDir)
		if err != nil {
			return err
		}
	}
	if cmd == "launch" || cmd == "resume" {
		if err := preflightReleaseHost(ctx, stateDir, cfg, bins); err != nil {
			return fmt.Errorf("release host preflight: %w", err)
		}
	}
	ex, err := NewExecutor(ctx, cfg, stateDir, p, j, roles)
	if err != nil {
		return err
	}
	defer ex.Close()
	if err := ex.verifyCarriedActionHistory(ctx); err != nil {
		return fmt.Errorf("carried plan history preflight: %w", err)
	}
	// Chain/environment setup always stops at the disabled configuration
	// boundary. LaunchDeployment then starts temporary operator APIs, provisions
	// their server-assigned client identities, anchors the fleet, and only then
	// starts the persistent topology.
	limitID := "config.render"
	if cmd == "scenario" {
		if o.Name == releaseCandidateCampaignName {
			return runReleaseCandidateCampaign(ctx, cfg, stateDir, j, ex, roles, runScenarioCampaignAttempt)
		}
		return RunScenario(ctx, cfg, stateDir, o.Name, j, ex)
	}
	if err := executeSetupActions(ctx, ex, p.Actions, limitID); err != nil {
		return err
	}
	if cmd == "launch" || cmd == "resume" {
		if err := LaunchDeployment(ctx, cfg, stateDir, p, roles, ex, bins, o.Detach); err != nil {
			return err
		}
		if cmd == "launch" {
			// M0B is a hard gate for every live topology. It deploys and
			// recovers only the approved dust probe, then publishes signed
			// evidence before even the smoke scenario can pass.
			if err := RunScenario(ctx, cfg, stateDir, "precompile-conformance", j, ex); err != nil {
				return err
			}
			if err := RunScenario(ctx, cfg, stateDir, cfg.Config.Scenarios.Launch, j, ex); err != nil {
				return err
			}
		}
	}
	result := map[string]any{"schema": "urnetwork-sim-command-result-v1", "command": cmd, "deployment_id": cfg.Config.Deployment.DeploymentID, "plan_hash": p.PlanHash, "state_dir": stateDir, "status_command": fmt.Sprintf("sim-testnet status --config %s --state-dir %s", cfg.ConfigPath, stateDir)}
	return printResult(o.Format, result, nil)
}

var errPersistedPlanIdentityMismatch = errors.New("persisted setup plan does not match the current release/configuration")

// A failed host preflight may have written deterministic role/build inputs but
// no transaction intent. In that one recoverable state, a new locked release
// may replace the stale plan. Any journal entry or malformed/tampered plan keeps
// fail-closed resume semantics.
func mayRefreshPersistedPlan(planErr error, entries []JournalEntry) bool {
	if len(entries) != 0 {
		return false
	}
	return errors.Is(planErr, os.ErrNotExist) || errors.Is(planErr, errPersistedPlanIdentityMismatch)
}

func loadPersistedPlan(cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	p, err := readPersistedPlan(stateDir)
	if err != nil {
		return nil, err
	}
	if cfg.Release == nil {
		return nil, fmt.Errorf("current release lock is unavailable")
	}
	releaseLockHash, err := canonicalHashHex(cfg.Release)
	if err != nil {
		return nil, fmt.Errorf("hash current release lock: %w", err)
	}
	resolvedHash, err := resolvedInputsHash(cfg)
	if err != nil {
		return nil, fmt.Errorf("hash current resolved launch inputs: %w", err)
	}
	bootstrapBurnHalfLife := uint16(hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["burn_half_life"]))
	productionBurnHalfLife := uint16(hyperparameterUint64(cfg.Hyperparameters.ProductionOwnerControlled["burn_half_life"]))
	if p.Schema != currentSetupPlanSchema || p.Release != "1.0" || p.ReleaseLockHash == "" || p.ReleaseLockHash != releaseLockHash || p.ResolvedInputsHash == "" || p.ResolvedInputsHash != resolvedHash || p.DeploymentID != cfg.Config.Deployment.DeploymentID || p.ChainID != testnetChainID || p.GenesisHash != testnetGenesis || p.Netuid != cfg.Netuid || p.ConfigHash != cfg.ConfigHash || p.PolicyHash != cfg.PolicyHash || p.Limits != configuredPlanLimits(cfg) || p.RegistrationBurnLimitRao != cfg.Config.Budgets.MaximumRegistrationBurnRao || p.NativeTransactionFeeLimitRao != cfg.Config.Budgets.MaximumNativeTransactionFeeRao || p.MaximumEVMFeePerGasWei != cfg.Config.Budgets.MaximumEVMFeePerGasWei || p.AlphaTransferMarginBPS != cfg.Config.AlphaTransfers.MinimumTAOEquivalentMarginBPS || p.MinimumSourceRemainingRao != cfg.Config.ValidatorBootstrap.MinimumSourceRemainingAlphaRao || p.BootstrapBurnHalfLifeBlocks != bootstrapBurnHalfLife || p.ProductionBurnHalfLifeBlocks != productionBurnHalfLife {
		return nil, errPersistedPlanIdentityMismatch
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		return nil, err
	}
	if roleHash, _ := canonicalHashHex(roles); roleHash == "" {
		return nil, fmt.Errorf("could not hash current public roles")
	} else if persistedRoleHash, _ := canonicalHashHex(p.Roles); roleHash != persistedRoleHash {
		return nil, fmt.Errorf("persisted setup roles do not match deterministic role derivation")
	}
	return p, nil
}

// Decode a stored plan against its own canonical hash. Current release/config
// identity is checked separately so a valid ancestor can seed an explicit
// revision without making a corrupted or hand-edited file refreshable.
func readPersistedPlan(stateDir string) (*SetupPlan, error) {
	return readPersistedPlanFile(filepath.Join(stateDir, "plan.json"))
}

// Authenticate one stored plan snapshot against the canonical hash encoded in
// that file. Ancestor recovery uses the same decoder as the active plan so a
// hand-edited history file cannot authorize a carried transaction.
func readPersistedPlanFile(path string) (*SetupPlan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodePersistedPlanBytes(b)
}

// Authenticate one already-read wire image so archival can preserve the exact
// reviewed bytes without a read/decode/re-read race or a struct re-marshal.
func decodePersistedPlanBytes(b []byte) (*SetupPlan, error) {
	var p SetupPlan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("persisted setup plan: %w", err)
	}
	if !supportedSetupPlanSchema(p.Schema) {
		return nil, fmt.Errorf("persisted setup plan has unsupported schema %q", p.Schema)
	}
	want := p.PlanHash
	got, err := persistedSetupPlanHash(b, p.Schema)
	if err != nil {
		return nil, err
	}
	if want != "" && got != want {
		legacy, applicable, legacyErr := legacyArchivedSetupPlanHash(b, p.Schema)
		if legacyErr != nil {
			return nil, legacyErr
		}
		if applicable && legacy == want {
			got = legacy
		}
	}
	if want == "" || got != want {
		return nil, fmt.Errorf("persisted setup plan hash mismatch: got %s want %s", got, want)
	}
	if err := validatePlanBudget(&p); err != nil {
		return nil, fmt.Errorf("persisted setup plan: %w", err)
	}
	return &p, nil
}

func writeRunInputs(cfg *ResolvedConfig, stateDir string, p *SetupPlan, roles *RoleSecrets) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	planBytes := append(b, '\n')
	priorPath := filepath.Join(stateDir, "plan.json")
	priorBytes, priorErr := os.ReadFile(priorPath)
	if priorErr == nil {
		prior, decodeErr := decodePersistedPlanBytes(priorBytes)
		if decodeErr != nil {
			return decodeErr
		}
		if prior.PlanHash != p.PlanHash {
			if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(prior.PlanHash)+".json"), priorBytes, 0o600); err != nil {
				return err
			}
		}
	} else if !errors.Is(priorErr, os.ErrNotExist) {
		return priorErr
	}
	if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(p.PlanHash)+".json"), planBytes, 0o600); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), planBytes, 0o600); err != nil {
		return err
	}
	redacted := map[string]any{"schema": "urnetwork-sim-effective-config-v1", "config": cfg.Config, "chain_id": cfg.ChainID, "netuid": cfg.Netuid, "private_authority": redactURL(cfg.Authority), "operational_rpc_mode": cfg.OperationalRPCMode, "operational_substrate_rpc": redactURL(cfg.OperationalSubstrate), "operational_evm_rpc": redactURL(cfg.OperationalEVM), "wallet_public": cfg.WalletPublic, "policy_hash": cfg.PolicyHash, "config_hash": cfg.ConfigHash, "resolved_inputs_hash": p.ResolvedInputsHash, "release_lock_hash": p.ReleaseLockHash}
	y, err := yaml.Marshal(redacted)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(stateDir, "config.redacted.yml"), y, 0o600); err != nil {
		return err
	}
	pub, _ := json.MarshalIndent(roles.Public(), "", "  ")
	return atomicWrite(filepath.Join(stateDir, "public", "identities.json"), append(pub, '\n'), 0o644)
}

// Some setup actions prove a point-in-time transfer whose balance is
// intentionally consumed by a later approved action. Requiring the original
// balance again on resume would make a correct deployment non-resumable. All
// other actions retain live revalidation; this allowlist must stay narrow.
func actionRequiresCurrentPostcondition(action Action) bool {
	switch {
	case strings.HasPrefix(action.ID, "evm.fund-"):
		return false
	case strings.HasPrefix(action.ID, "fleet.fund."):
		return false
	case strings.HasPrefix(action.ID, "fleet.fund-hotkey."):
		return false
	case strings.HasPrefix(action.ID, "churn.fund."):
		return false
	case strings.HasPrefix(action.ID, "validator.fund."):
		return false
	case strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit."):
		return false
	case strings.HasPrefix(action.ID, "alpha.repair."):
		return false
	default:
		return true
	}
}

func (e *Executor) consumedActionTransaction(action Action, verified JournalEntry) (JournalEntry, error) {
	if action.Kind != "substrate-extrinsic" {
		return JournalEntry{}, fmt.Errorf("consumed action %s is not a native extrinsic", action.ID)
	}
	if verified.Stage != StageVerified || verified.ActionID != action.ID || !actionAcceptsIntent(action, verified.IntentHash) || !e.plan.allowedPlanHashes()[verified.PlanHash] {
		return JournalEntry{}, fmt.Errorf("consumed action %s has invalid verified plan evidence", action.ID)
	}
	transaction, ok := e.journal.LatestTransaction(verified.PlanHash, action.ID, verified.IntentHash)
	if !ok || transaction.Stage != StageFinalized {
		return JournalEntry{}, fmt.Errorf("consumed action %s has no finalized transaction evidence", action.ID)
	}
	return transaction, nil
}

func observedPostconditionMatches(recorded, replayed map[string]any) error {
	if recorded == nil || replayed == nil {
		return errors.New("historical postcondition observation is unavailable")
	}
	recordedHash, err := canonicalHashHex(recorded)
	if err != nil {
		return fmt.Errorf("hash recorded historical observation: %w", err)
	}
	replayedHash, err := canonicalHashHex(replayed)
	if err != nil {
		return fmt.Errorf("hash replayed historical observation: %w", err)
	}
	if replayedHash != recordedHash {
		return fmt.Errorf("historical postcondition replay hash %s differs from recorded %s", replayedHash, recordedHash)
	}
	return nil
}

// Replays an EVM-dependent point-in-time assertion after a later approved
// action intentionally supersedes its live state. V3+ receipts bind the exact
// EVM-RPC hash domain, so both finalized observations must remain canonical
// and byte-equivalent to their persisted values.
func (e *Executor) verifyHistoricalEVMPostcondition(ctx context.Context, action Action, record *ActionPostcondition) error {
	if record == nil || (record.Schema != "urnetwork-sim-action-postcondition-v3" && record.Schema != "urnetwork-sim-action-postcondition-v4") {
		return errors.New("historical EVM action has no replayable v3+ postcondition")
	}
	if e == nil || e.plan == nil || record.ActionID != action.ID || !actionAcceptsIntent(action, record.IntentHash) || !e.plan.allowedPlanHashes()[record.PlanHash] {
		return errors.New("historical EVM postcondition is outside the approved action lineage")
	}
	if e.deployer == nil || e.deployer.client == nil {
		return errors.New("operational EVM history client is unavailable")
	}
	finalized, err := finalizedEVMHead(ctx, e.deployer.client)
	if err != nil {
		return fmt.Errorf("operational finalized EVM head: %w", err)
	}
	if err := verifyEVMCheckpoint(ctx, e.deployer.client, finalized, record.EVMFinalized); err != nil {
		return fmt.Errorf("operational historical EVM checkpoint: %w", err)
	}
	replayed, err := e.actionPostState(ctx, action, record.EVMFinalized)
	if err != nil {
		return fmt.Errorf("operational historical EVM state: %w", err)
	}
	if err := observedPostconditionMatches(record.Observed, replayed); err != nil {
		return fmt.Errorf("operational historical EVM state: %w", err)
	}

	if !independentRPCRequired(e.cfg) {
		if record.IndependentEVMFinalized.Number != record.EVMFinalized.Number || !strings.EqualFold(record.IndependentEVMFinalized.Hash, record.EVMFinalized.Hash) {
			return errors.New("shared-provider historical EVM checkpoints differ")
		}
		if err := observedPostconditionMatches(record.Observed, record.IndependentObserved); err != nil {
			return fmt.Errorf("shared-provider historical EVM clone: %w", err)
		}
		return nil
	}
	if e.independentEVM == nil {
		return errors.New("independent EVM history client is unavailable")
	}
	observer := e.independentReadExecutor()
	independentFinalized, err := finalizedEVMHead(ctx, e.independentEVM)
	if err != nil {
		return fmt.Errorf("independent finalized EVM head: %w", err)
	}
	if err := verifyEVMCheckpoint(ctx, e.independentEVM, independentFinalized, record.IndependentEVMFinalized); err != nil {
		return fmt.Errorf("independent historical EVM checkpoint: %w", err)
	}
	independentReplayed, err := observer.actionPostState(ctx, action, record.IndependentEVMFinalized)
	if err != nil {
		return fmt.Errorf("independent historical EVM state: %w", err)
	}
	if err := observedPostconditionMatches(record.IndependentObserved, independentReplayed); err != nil {
		return fmt.Errorf("independent historical EVM state: %w", err)
	}
	return nil
}

// A funding action can converge without broadcasting when an earlier plan
// already left enough native value at its EVM mirror. Once an approved
// descendant spends that value, neither the live balance nor a transaction
// belonging to the converged action can prove the point-in-time assertion.
func (e *Executor) verifyConsumedEVMFundingPostcondition(ctx context.Context, action Action, record *ActionPostcondition) error {
	return e.verifyHistoricalEVMPostcondition(ctx, action, record)
}

func (e *Executor) verifyConsumedActionHistory(ctx context.Context, action Action, verified JournalEntry, record *ActionPostcondition, sharedNativeHead *ChainHead) error {
	transaction, err := e.consumedActionTransaction(action, verified)
	if err == nil {
		if sharedNativeHead != nil {
			return e.verifySubstrateTransactionEvidenceAtHead(
				ctx,
				ChainHead{Number: transaction.BlockNumber, Hash: transaction.BlockHash},
				transaction.TransactionHash,
				*sharedNativeHead,
			)
		}
		return e.verifySubstrateTransactionEvidence(ctx,
			ChainHead{Number: transaction.BlockNumber, Hash: transaction.BlockHash},
			transaction.TransactionHash,
		)
	}
	transactionErr := err
	if action.Kind == "substrate-extrinsic" && strings.HasPrefix(action.ID, "evm.fund-") {
		if replayErr := e.verifyConsumedEVMFundingPostcondition(ctx, action, record); replayErr == nil {
			return nil
		} else {
			return fmt.Errorf("finalized transaction evidence: %v; finalized historical EVM postcondition: %w", transactionErr, replayErr)
		}
	}
	return transactionErr
}

// Revalidate one terminal action using live state where it still holds, then
// fall back to its finalized transaction only for intentionally consumable
// point-in-time balances. This also supports convergence actions that adopted
// a balance already present and therefore have no transaction of their own.
func (e *Executor) verifyVerifiedActionState(ctx context.Context, action Action, verified JournalEntry) error {
	record, err := e.readPersistedPostcondition(verified)
	if err != nil {
		return fmt.Errorf("persisted postcondition: %w", err)
	}
	return e.verifyVerifiedActionStateWithRecord(ctx, action, verified, record, nil, nil)
}

// These postconditions are wholly local or native-chain observations. Keep
// the allowlist explicit and conservative: an unfamiliar future action uses
// an EVM checkpoint until its verifier is classified deliberately.
func actionPostStateRequiresEVMCheckpoint(action Action) bool {
	switch {
	case action.ID == "subnet.verify-owner":
		return false
	case strings.HasPrefix(action.ID, "subnet.hyperparameter."):
		return false
	case strings.HasPrefix(action.ID, "production.hyperparameter."):
		return false
	case strings.HasPrefix(action.ID, "fleet.fund."):
		return false
	case strings.HasPrefix(action.ID, "fleet.fund-hotkey."):
		return false
	case strings.HasPrefix(action.ID, "churn.fund."):
		return false
	case strings.HasPrefix(action.ID, "validator.fund."):
		return false
	case strings.HasPrefix(action.ID, "fleet.register."):
		return false
	case strings.HasPrefix(action.ID, "churn.register."):
		return false
	case strings.HasPrefix(action.ID, "validator.register."):
		return false
	case strings.HasPrefix(action.ID, "operator.deposit.register."):
		return false
	case strings.HasPrefix(action.ID, "validator.take-zero."):
		return false
	case action.ID == "validator.reserve-majority":
		return false
	case strings.HasPrefix(action.ID, "fleet.commitment."):
		return false
	case strings.HasPrefix(action.ID, "fleet.refresh.commitment."):
		return false
	case action.Parameters["batch_installed"] == "true" && (strings.HasPrefix(action.ID, "fleet.mirror.") || strings.HasPrefix(action.ID, "fleet.bind.")):
		return false
	case action.Kind == "budget-reserve":
		return false
	case action.ID == "config.render":
		return false
	case action.ID == "accounts.provision":
		return false
	case action.ID == "topology.launch":
		return false
	case action.ID == "churn.tournament-complete":
		return false
	default:
		return true
	}
}

// Revalidate only the semantic state. The receipt already authenticates the
// original dual-chain checkpoints, so resolving two new heads for every
// carried action adds no evidence. EVM-dependent actions share one immutable
// checkpoint for the entire preflight; native/local actions make no EVM call.
func (e *Executor) verifyCurrentActionPostState(ctx context.Context, action Action, sharedEVMHead *ChainHead) error {
	evmHead := ChainHead{}
	if actionPostStateRequiresEVMCheckpoint(action) {
		if sharedEVMHead != nil {
			evmHead = *sharedEVMHead
		} else {
			if e == nil || e.deployer == nil || e.deployer.client == nil {
				return errors.New("EVM postcondition client is unavailable")
			}
			var err error
			evmHead, err = finalizedEVMHead(ctx, e.deployer.client)
			if err != nil {
				return fmt.Errorf("EVM finalized checkpoint: %w", err)
			}
		}
		if evmHead.Number == 0 || evmHead.Hash == "" {
			return errors.New("EVM finalized checkpoint identity is incomplete")
		}
	}
	_, err := e.actionPostState(ctx, action, evmHead)
	return err
}

func planBoundFleetBatchRange(cfg *ResolvedConfig, action Action) (int, int, error) {
	batch := suffixInt(action.ID)
	switch {
	case strings.HasPrefix(action.ID, "fleet.install.batch."):
		return fleetInstallActionRange(cfg, action, batch)
	case strings.HasPrefix(action.ID, "fleet.refresh.batch."):
		return fleetRefreshActionRange(cfg, action, batch)
	default:
		return 0, 0, fmt.Errorf("action %s has no plan-bound fleet preparation", action.ID)
	}
}

// Resolve the exact archived action which created a plan-bound prepared
// calldata artifact. Current live state is still re-read, but the immutable
// preparation must be authenticated against its source plan rather than a
// later revision hash. Install and refresh use the same fail-closed rule.
func exactCarriedFleetBatchSourceAction(cfg *ResolvedConfig, currentPlan, sourcePlan *SetupPlan, current Action, verified JournalEntry) (Action, error) {
	if cfg == nil || currentPlan == nil || sourcePlan == nil || verified.Stage != StageVerified || verified.PlanHash == "" || verified.IntentHash == "" {
		return Action{}, errors.New("carried fleet batch source context is incomplete")
	}
	if sourcePlan.PlanHash != verified.PlanHash || !currentPlan.allowedPlanHashes()[verified.PlanHash] || verified.ActionID != current.ID || !actionAcceptsIntent(current, verified.IntentHash) {
		return Action{}, errors.New("carried fleet batch source is outside the approved action lineage")
	}
	var source *Action
	for index := range sourcePlan.Actions {
		candidate := &sourcePlan.Actions[index]
		if candidate.ID != verified.ActionID || candidate.IntentHash != verified.IntentHash {
			continue
		}
		if source != nil {
			return Action{}, errors.New("carried fleet batch source plan has duplicate exact actions")
		}
		copy := *candidate
		source = &copy
	}
	if source == nil {
		return Action{}, errors.New("carried fleet batch source plan has no exact action")
	}
	sourceTarget, sourceTargetErr := fleetBatcherAddressForAction(*source)
	currentTarget, currentTargetErr := fleetBatcherAddressForAction(current)
	if source.Kind != current.Kind || sourceTargetErr != nil || currentTargetErr != nil || sourceTarget != currentTarget {
		return Action{}, errors.New("carried fleet batch source kind or target differs from the active action")
	}
	currentFirst, currentLast, err := planBoundFleetBatchRange(cfg, current)
	if err != nil {
		return Action{}, err
	}
	sourceFirst, sourceLast, err := planBoundFleetBatchRange(cfg, *source)
	if err != nil || sourceFirst != currentFirst || sourceLast != currentLast {
		return Action{}, stateMismatchError(err, "carried fleet batch source range differs from the active action")
	}
	if sourcePlan.DeploymentID != currentPlan.DeploymentID || sourcePlan.ChainID != currentPlan.ChainID || sourcePlan.Netuid != currentPlan.Netuid || !contractDeploymentAddressesEqual(sourcePlan.Deployment, currentPlan.Deployment) {
		return Action{}, errors.New("carried fleet batch source deployment differs from the active lineage")
	}
	return *source, nil
}

func (e *Executor) carriedFleetBatchSourceExecutor(action Action, verified JournalEntry) (*Executor, Action, error) {
	if e == nil || e.plan == nil {
		return nil, Action{}, errors.New("carried fleet batch executor is unavailable")
	}
	if verified.PlanHash == e.plan.PlanHash {
		return e, action, nil
	}
	sourcePlan, err := readPersistedPlanFile(filepath.Join(e.stateDir, "plans", stringsTrim0x(verified.PlanHash)+".json"))
	if err != nil {
		return nil, Action{}, fmt.Errorf("read carried fleet batch source plan: %w", err)
	}
	sourceAction, err := exactCarriedFleetBatchSourceAction(e.cfg, e.plan, sourcePlan, action, verified)
	if err != nil {
		return nil, Action{}, err
	}
	sourceExecutor := *e
	sourceExecutor.plan = sourcePlan
	return &sourceExecutor, sourceAction, nil
}

// A generation-2 refresh intentionally supersedes the generation-1 mirror and
// binding versions installed by the batch with the same canonical range. Only
// that exact verified successor permits historical replay of the install.
func (e *Executor) fleetInstallBatchSuperseded(action Action) (bool, error) {
	if !strings.HasPrefix(action.ID, "fleet.install.batch.") {
		return false, nil
	}
	if e == nil || e.cfg == nil || e.plan == nil || e.journal == nil {
		return false, errors.New("fleet install successor context is unavailable")
	}
	batch := suffixInt(action.ID)
	if batch < 1 || action.ID != fmt.Sprintf("fleet.install.batch.%d", batch) {
		return false, fmt.Errorf("fleet install action %q is not canonical", action.ID)
	}
	installFirst, installLast, err := fleetInstallActionRange(e.cfg, action, batch)
	if err != nil {
		return false, err
	}
	refresh, err := e.planAction(fmt.Sprintf("fleet.refresh.batch.%d", batch))
	if err != nil {
		return false, err
	}
	refreshFirst, refreshLast, err := fleetRefreshActionRange(e.cfg, refresh, batch)
	if err != nil {
		return false, err
	}
	if refreshFirst != installFirst || refreshLast != installLast {
		return false, fmt.Errorf("fleet install batch %d range %d-%d differs from refresh range %d-%d", batch, installFirst, installLast, refreshFirst, refreshLast)
	}
	_, verified := e.verifiedActionEntry(refresh)
	return verified, nil
}

func (e *Executor) verifyVerifiedActionStateWithRecord(ctx context.Context, action Action, verified JournalEntry, record *ActionPostcondition, sharedEVMHead, sharedNativeHead *ChainHead) error {
	if e.carriedFleetHistoryKeys[carriedVerificationKey(verified)] {
		if record == nil || action.ID != verified.ActionID || record.PlanHash != verified.PlanHash || record.ActionID != verified.ActionID || record.IntentHash != verified.IntentHash || !actionAcceptsIntent(action, verified.IntentHash) {
			return errors.New("batched historical fleet receipt identity mismatch")
		}
		hash, err := canonicalHashHex(record)
		if err != nil || hash != verified.PostconditionHash {
			return stateMismatchError(err, "batched historical fleet receipt hash %s differs from verified %s", hash, verified.PostconditionHash)
		}
		return nil
	}
	verifier, verifiedAction := e, action
	if verified.PlanHash != e.plan.PlanHash && (strings.HasPrefix(action.ID, "fleet.install.batch.") || strings.HasPrefix(action.ID, "fleet.refresh.batch.")) {
		var err error
		verifier, verifiedAction, err = e.carriedFleetBatchSourceExecutor(action, verified)
		if err != nil {
			return fmt.Errorf("carried fleet batch source: %w", err)
		}
	}
	if verified.PlanHash != e.plan.PlanHash && action.ID == voluntaryConvictionActionID {
		var err error
		verifier, verifiedAction, err = e.carriedVoluntaryConvictionSourceExecutor(action, verified)
		if err != nil {
			return fmt.Errorf("carried voluntary-conviction source: %w", err)
		}
	}
	generationOneSuperseded, err := e.fleetGenerationOneActionSuperseded(action, verified, record)
	if err != nil {
		return fmt.Errorf("fleet generation-1 successor: %w", err)
	}
	if generationOneSuperseded {
		if err := verifier.verifyHistoricalEVMPostcondition(ctx, verifiedAction, record); err != nil {
			return fmt.Errorf("historical fleet generation-1 postcondition: %w", err)
		}
		return nil
	}
	oracleSuperseded, err := e.fleetRefreshOracleActivationSuperseded(action, verified, record)
	if err != nil {
		return fmt.Errorf("fleet refresh oracle successor: %w", err)
	}
	if oracleSuperseded {
		if err := verifier.verifyHistoricalEVMPostcondition(ctx, verifiedAction, record); err != nil {
			return fmt.Errorf("historical fleet refresh oracle postcondition: %w", err)
		}
		return nil
	}
	// Point-in-time native transfers with exact finalized transaction evidence
	// are expected to be consumed. Prove their receipt directly instead of
	// first issuing a live balance read whose failure is the normal path.
	if !actionRequiresCurrentPostcondition(verifiedAction) {
		if _, transactionErr := verifier.consumedActionTransaction(verifiedAction, verified); transactionErr == nil {
			if historyErr := verifier.verifyConsumedActionHistory(ctx, verifiedAction, verified, record, sharedNativeHead); historyErr != nil {
				return fmt.Errorf("historical postcondition: %w", historyErr)
			}
			return nil
		}
	}
	if actionRequiresCurrentPostcondition(action) {
		if err := verifier.verifyCurrentActionPostState(ctx, verifiedAction, sharedEVMHead); err != nil {
			return fmt.Errorf("current postcondition: %w", err)
		}
		return nil
	}
	currentErr := verifier.verifyCurrentActionPostState(ctx, verifiedAction, sharedEVMHead)
	if currentErr == nil {
		return nil
	}
	if historyErr := verifier.verifyConsumedActionHistory(ctx, verifiedAction, verified, record, sharedNativeHead); historyErr != nil {
		return fmt.Errorf("current postcondition: %v; historical postcondition: %w", currentErr, historyErr)
	}
	return nil
}

type initialRegistrationPlanPosition struct {
	Applicable         bool
	PriorRegistrations uint64
	TotalRegistrations uint64
	PriorActionIDs     []string
	LaterActionIDs     []string
}

// Locate one setup registration relative to the topology launch barrier. The
// two challenger registrations deliberately occur after that barrier and have
// their own exact runtime-prune precondition.
func initialRegistrationPosition(plan *SetupPlan, actionID string) (initialRegistrationPlanPosition, error) {
	if plan == nil {
		return initialRegistrationPlanPosition{}, errors.New("approved plan is unavailable")
	}
	topologyIndex, actionIndex := -1, -1
	for index, action := range plan.Actions {
		if action.ID == "topology.launch" {
			if topologyIndex >= 0 {
				return initialRegistrationPlanPosition{}, errors.New("approved plan contains multiple topology.launch actions")
			}
			topologyIndex = index
		}
		if action.ID == actionID {
			if actionIndex >= 0 {
				return initialRegistrationPlanPosition{}, fmt.Errorf("approved plan contains multiple %s actions", actionID)
			}
			actionIndex = index
		}
	}
	if topologyIndex < 0 {
		return initialRegistrationPlanPosition{}, errors.New("approved plan has no topology.launch action")
	}
	if actionIndex < 0 {
		return initialRegistrationPlanPosition{}, fmt.Errorf("approved plan has no action %s", actionID)
	}
	if plan.Deployment.RegistrationRoleGeneration > 0 && (actionID == "evm.vault-register-escrow" || strings.HasPrefix(actionID, "operator.register.")) {
		// A replacement generation starts only after the prior deterministic
		// topology is full. These actions have stronger per-UID runtime-prune,
		// global-owner, and exact in-place replacement checks of their own.
		return initialRegistrationPlanPosition{}, nil
	}
	if actionIndex >= topologyIndex || plan.Actions[actionIndex].Spend.Registrations == 0 {
		return initialRegistrationPlanPosition{}, nil
	}
	position := initialRegistrationPlanPosition{Applicable: true}
	for index := 0; index < topologyIndex; index++ {
		action := plan.Actions[index]
		if action.Spend.Registrations == 0 {
			continue
		}
		registrations := uint64(action.Spend.Registrations)
		position.TotalRegistrations += registrations
		switch {
		case index < actionIndex:
			position.PriorRegistrations += registrations
			position.PriorActionIDs = append(position.PriorActionIDs, action.ID)
		case index > actionIndex:
			position.LaterActionIDs = append(position.LaterActionIDs, action.ID)
		}
	}
	return position, nil
}

type initialRegistrationPreState struct {
	ExistingUIDs               uint64
	PriorRegistrations         uint64
	CurrentActionRegistrations uint64
	TotalRegistrations         uint64
	CurrentUIDs                uint64
	MaximumUIDs                uint64
	PriorActionsVerified       bool
	LaterActionsUnverified     bool
	CurrentActionObserved      bool
}

func validateInitialRegistrationPreState(state initialRegistrationPreState) error {
	if state.CurrentActionRegistrations != 1 {
		return fmt.Errorf("initial registration must consume exactly one UID, got %d", state.CurrentActionRegistrations)
	}
	if state.MaximumUIDs == 0 || state.ExistingUIDs+state.TotalRegistrations != state.MaximumUIDs {
		return fmt.Errorf("approved initial topology existing=%d registrations=%d does not exactly fill maximum=%d", state.ExistingUIDs, state.TotalRegistrations, state.MaximumUIDs)
	}
	if !state.PriorActionsVerified {
		return errors.New("an earlier initial registration is not postcondition-verified")
	}
	if !state.LaterActionsUnverified {
		return errors.New("a later initial registration was verified out of order")
	}
	want := state.ExistingUIDs + state.PriorRegistrations
	if state.CurrentActionObserved {
		if state.CurrentUIDs != want+state.CurrentActionRegistrations {
			return fmt.Errorf("resumed initial registration UID count=%d, want exact post-state %d", state.CurrentUIDs, want+state.CurrentActionRegistrations)
		}
		return nil
	}
	if state.CurrentUIDs != want {
		return fmt.Errorf("initial registration UID count=%d, want exact pre-state %d", state.CurrentUIDs, want)
	}
	if state.CurrentUIDs >= state.MaximumUIDs {
		return fmt.Errorf("initial registration cannot start at full capacity %d", state.MaximumUIDs)
	}
	return nil
}

// Fail closed immediately before each initial registration if another actor
// changed subnet capacity or if a setup action ran out of order. A transaction
// that was already broadcast before a crash may be accepted only when its exact
// role postcondition is now observable; the journal then lets the transaction
// manager resume without allocating a second nonce.
func (e *Executor) verifyInitialRegistrationPreState(ctx context.Context, action Action) error {
	position, err := initialRegistrationPosition(e.plan, action.ID)
	if err != nil || !position.Applicable {
		return err
	}
	priorVerified := true
	for _, id := range position.PriorActionIDs {
		if !e.actionVerified(id) {
			priorVerified = false
			break
		}
	}
	laterUnverified := true
	for _, id := range position.LaterActionIDs {
		if e.actionVerified(id) {
			laterUnverified = false
			break
		}
	}
	count, err := e.substrate.UIDCount()
	if err != nil {
		return fmt.Errorf("read finalized UID count: %w", err)
	}
	liveMaximum, err := e.substrate.ReadHyper("max_allowed_uids")
	if err != nil {
		return fmt.Errorf("read finalized max_allowed_uids: %w", err)
	}
	maximum := hyperparameterUint64(liveMaximum)
	approvedMaximum := hyperparameterUint64(e.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"])
	if maximum == 0 || maximum != approvedMaximum {
		return fmt.Errorf("live max_allowed_uids=%d does not match approved value %d", maximum, approvedMaximum)
	}
	wantPreState := uint64(e.plan.LiveFacts.ExistingUIDCount) + position.PriorRegistrations
	_, hasTransaction := e.journal.LatestTransaction(e.plan.PlanHash, action.ID, action.IntentHash)
	currentObserved := hasTransaction && uint64(count) == wantPreState+uint64(action.Spend.Registrations)
	if currentObserved {
		if _, err := e.verifyActionPostcondition(ctx, action); err != nil {
			return fmt.Errorf("resumed registration has one added UID but not its exact approved identity: %w", err)
		}
	}
	return validateInitialRegistrationPreState(initialRegistrationPreState{
		ExistingUIDs: uint64(e.plan.LiveFacts.ExistingUIDCount), PriorRegistrations: position.PriorRegistrations,
		CurrentActionRegistrations: uint64(action.Spend.Registrations), TotalRegistrations: position.TotalRegistrations,
		CurrentUIDs: uint64(count), MaximumUIDs: maximum, PriorActionsVerified: priorVerified,
		LaterActionsUnverified: laterUnverified, CurrentActionObserved: currentObserved,
	})
}

func (e *Executor) Execute(ctx context.Context, a Action) error {
	if err := e.verifyActionDependencies(a); err != nil {
		return fmt.Errorf("action %s dependencies: %w", a.ID, err)
	}
	if prior, ok := e.verifiedActionEntry(a); ok {
		if prior.PlanHash != e.plan.PlanHash && e.carriedVerificationKeys[carriedVerificationKey(prior)] {
			return nil
		}
		if err := e.verifyVerifiedActionState(ctx, a, prior); err != nil {
			return fmt.Errorf("action %s: %w", a.ID, err)
		}
		return nil
	}
	if a.Spend.Registrations > 0 {
		if err := e.verifyInitialRegistrationPreState(ctx, a); err != nil {
			return fmt.Errorf("action %s registration precondition: %w", a.ID, err)
		}
	}
	if err := e.journal.Append(JournalEntry{DeploymentID: e.cfg.Config.Deployment.DeploymentID, PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageIntent}); err != nil {
		return err
	}
	err := e.execute(ctx, a)
	if err != nil {
		_ = e.journal.Append(JournalEntry{DeploymentID: e.cfg.Config.Deployment.DeploymentID, PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageFailed, Error: redactText(err.Error(), e.cfg.WalletSecret, e.cfg.WalletMaterial, e.cfg.WalletPasswordSecret, e.cfg.WalletPassword)})
		return fmt.Errorf("action %s: %w", a.ID, err)
	}
	post, err := e.verifyActionPostcondition(ctx, a)
	if err != nil {
		_ = e.journal.Append(JournalEntry{DeploymentID: e.cfg.Config.Deployment.DeploymentID, PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageFailed, Error: redactText(err.Error(), e.cfg.WalletSecret, e.cfg.WalletMaterial, e.cfg.WalletPasswordSecret, e.cfg.WalletPassword)})
		return fmt.Errorf("action %s postcondition: %w", a.ID, err)
	}
	path, hash, err := e.persistActionPostcondition(post)
	if err != nil {
		return fmt.Errorf("action %s persist postcondition: %w", a.ID, err)
	}
	return e.journal.Append(JournalEntry{DeploymentID: e.cfg.Config.Deployment.DeploymentID, PlanHash: e.plan.PlanHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageVerified, PostconditionHash: hash, PostconditionPath: path})
}

// Ordinary generation-1/2 fleet funding always follows the registered fleet
// signer. Lifecycle takeover identities have separately named, separately
// budgeted actions and must never redirect the setup funding action.
func fleetHotkeyFundingRole(cfg *ResolvedConfig, action Action) (string, error) {
	if cfg == nil || cfg.Config == nil || !strings.HasPrefix(action.ID, "fleet.fund-hotkey.") || action.Kind != "substrate-extrinsic" {
		return "", errors.New("ordinary fleet hotkey funding action is invalid")
	}
	fleet := suffixInt(action.ID)
	if fleet < 1 {
		return "", errors.New("ordinary fleet hotkey funding index is invalid")
	}
	wantTarget := fmt.Sprintf("head-fleet-hotkey:%d", fleet)
	if fleet > cfg.Config.Topology.HeadFleets {
		wantTarget = fmt.Sprintf("challenger-fleet-hotkey:%d", fleet)
	}
	if action.Target != wantTarget {
		return "", fmt.Errorf("ordinary fleet hotkey funding target=%q, want %q", action.Target, wantTarget)
	}
	return fleetHotkeyLabel(fleet), nil
}

func (e *Executor) verifyActionDependencies(action Action) error {
	if e.plan == nil || e.journal == nil {
		return errors.New("plan/journal is unavailable")
	}
	planned := make(map[string]Action, len(e.plan.Actions))
	for _, candidate := range e.plan.Actions {
		if _, exists := planned[candidate.ID]; exists {
			return fmt.Errorf("plan contains duplicate action %s", candidate.ID)
		}
		planned[candidate.ID] = candidate
	}
	for _, dependencyID := range action.DependsOn {
		dependency, ok := planned[dependencyID]
		if !ok {
			return fmt.Errorf("dependency %s is absent from the approved plan", dependencyID)
		}
		if _, ok := e.verifiedActionEntry(dependency); !ok {
			return fmt.Errorf("dependency %s is not postcondition-verified", dependencyID)
		}
	}
	return nil
}

func (e *Executor) execute(ctx context.Context, a Action) error {
	switch {
	case a.ID == "subnet.verify-owner":
		err, _ := verifySubnetOwner(e.substrate.chain, e.cfg, e.cfg.WalletPublic)
		return err
	case strings.HasPrefix(a.ID, "subnet.hyperparameter."):
		name := strings.TrimPrefix(a.ID, "subnet.hyperparameter.")
		got, err := e.substrate.ReadHyper(name)
		if err != nil {
			return err
		}
		want := e.cfg.Hyperparameters.OwnerControlled[name]
		if hyperEqual(got, want, hyperShapes[name].Kind) {
			return nil
		}
		call, err := e.substrate.HyperCall(name, want)
		if err != nil {
			return err
		}
		if _, _, err = e.substrate.Send(ctx, e.plan.PlanHash, a, call); err != nil {
			return err
		}
		got, err = e.substrate.ReadHyper(name)
		if err != nil {
			return err
		}
		if !hyperEqual(got, want, hyperShapes[name].Kind) {
			return fmt.Errorf("post-state %v, want %v", got, want)
		}
		return nil
	case a.ID == "production.schedule-policy":
		return e.scheduleProductionPolicy(ctx, a)
	case a.ID == "evm.coordinator-upgrade-activate":
		return e.activateCoordinatorUpgrade(ctx, a)
	case a.ID == "policy.schedule-bootstrap":
		return e.scheduleBootstrapPolicy(ctx, a)
	case a.ID == "policy.await-bootstrap":
		return e.awaitBootstrapPolicy(ctx)
	case strings.HasPrefix(a.ID, "production.hyperparameter."):
		return e.setProductionHyperparameter(ctx, a, strings.TrimPrefix(a.ID, "production.hyperparameter."))
	case strings.HasPrefix(a.ID, "operator.retire."):
		return e.scheduleOperatorRetirement(ctx, a)
	case strings.HasPrefix(a.ID, "evm.fund-"):
		return e.fundEVM(ctx, a)
	case strings.HasPrefix(a.ID, "operator.deposit.register."):
		return e.registerDepositHotkey(ctx, a, suffixInt(a.ID))
	case a.ID == "evm.reserve-sink" || a.ID == "evm.settlement-vault" || a.ID == "evm.coordinator-implementation" || a.ID == "evm.vault-register-escrow" || a.ID == "evm.coordinator-proxy" || a.ID == "evm.governance-drill-implementation" || a.ID == "evm.vault-fix-coordinator" || a.ID == "evm.sink-fix-recorder" || a.ID == "precompile.probe-deploy" || a.ID == "evm.coordinator-upgrade-implementation" || a.ID == "fleet.refresh.deploy-batcher":
		return e.executeDeployment(ctx, a)
	case a.ID == "fleet.refresh.oracle-activate" || a.ID == "fleet.refresh.oracle-restore":
		return e.scheduleFleetRefreshOracle(ctx, a)
	case a.ID == "fleet.refresh.oracle-await-active" || a.ID == "fleet.refresh.oracle-await-restored":
		return e.awaitFleetRefreshOracle(ctx, a)
	case strings.HasPrefix(a.ID, "fleet.refresh.commitment."):
		return e.publishFleetRefreshCommitment(ctx, a, suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "fleet.refresh.batch."):
		return e.refreshFleetBatch(ctx, a, suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "fleet.install.batch."):
		return e.installFleetBatch(ctx, a, suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "precompile."):
		return e.executePrecompileConformance(ctx, a)
	case strings.HasPrefix(a.ID, "governance."):
		return e.executeGovernanceDrillAction(ctx, a)
	case strings.HasPrefix(a.ID, "operator.register."):
		return e.registerOperator(ctx, a)
	case strings.HasPrefix(a.ID, "lifecycle."):
		return e.executeFleetLifecycleAction(ctx, a)
	case strings.HasPrefix(a.ID, "fleet.fund."):
		fleet := suffixInt(a.ID)
		return e.fundSubstrateRole(ctx, a, fleetColdkeyLabel(fleet))
	case strings.HasPrefix(a.ID, "fleet.fund-hotkey."):
		role, err := fleetHotkeyFundingRole(e.cfg, a)
		if err != nil {
			return err
		}
		return e.fundSubstrateRole(ctx, a, role)
	case strings.HasPrefix(a.ID, "fleet.register."):
		fleet := suffixInt(a.ID)
		if fleet > e.cfg.Config.Topology.HeadFleets {
			return e.registerChallenger(ctx, a, fleet)
		}
		return e.registerNative(ctx, a, fleetColdkeyLabel(fleet), fleetHotkeyLabel(fleet))
	case strings.HasPrefix(a.ID, "churn.fund."):
		churn := suffixInt(a.ID)
		return e.fundSubstrateRole(ctx, a, churnColdkeyLabel(churn))
	case strings.HasPrefix(a.ID, "churn.register."):
		churn := suffixInt(a.ID)
		return e.registerNative(ctx, a, churnColdkeyLabel(churn), churnHotkeyLabel(churn))
	case strings.HasPrefix(a.ID, "fleet.commitment."):
		return e.publishFleetCommitment(ctx, a, suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "fleet.mirror."):
		if a.Parameters["batch_installed"] == "true" {
			_, _, _, err := e.verifyFleetInstallAliasState(a, map[string]any{})
			return err
		}
		return e.mirrorFleetCommitment(ctx, a, suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "fleet.bind."):
		if a.Parameters["batch_installed"] == "true" {
			_, _, _, err := e.verifyFleetInstallAliasState(a, map[string]any{})
			return err
		}
		fleet, member, err := fleetBindingActionIndices(a.ID)
		if err != nil {
			return err
		}
		return e.bindFleetMember(ctx, a, fleet, member)
	case a.ID == "accounts.provision":
		return nil
	case strings.HasPrefix(a.ID, "validator.fund."):
		n := suffixInt(a.ID)
		return e.fundSubstrateRole(ctx, a, fmt.Sprintf("validator-%d-coldkey", n))
	case strings.HasPrefix(a.ID, "validator.register."):
		n := suffixInt(a.ID)
		return e.registerNative(ctx, a, fmt.Sprintf("validator-%d-coldkey", n), validatorHotkeyLabel(n))
	case a.ID == "validator.take-zero.1":
		return e.setReserveTakeZero(ctx, a)
	case a.Kind == "substrate-reconciliation" && strings.HasPrefix(a.ID, "alpha.transfer."):
		return e.reconcileAlphaTransfer(ctx, a)
	case strings.HasPrefix(a.ID, "alpha.repair."):
		kind, index, err := alphaTransferTargetFromActionID(a.ID)
		if err != nil {
			return err
		}
		return e.repairAlphaTransfer(ctx, a, kind, index)
	case strings.HasPrefix(a.ID, "alpha.transfer.operator-deposit."):
		return e.transferAlpha(ctx, a, "operator-deposit", suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "alpha.transfer.validator."):
		return e.transferAlpha(ctx, a, "validator", suffixInt(a.ID))
	case a.ID == "validator.reserve-majority":
		return e.verifyReserveValidatorMajority()
	case a.ID == "campaign.voluntary-conviction.1":
		return e.addVoluntaryConviction(ctx, a)
	case a.ID == voluntaryConvictionReconciliationActionID:
		return e.reconcileDuplicateVoluntaryConviction(ctx, a)
	case a.ID == dishonestDepositActionID:
		return e.executeDishonestDeposit(ctx, a)
	case a.Kind == "budget-reserve":
		return nil
	case a.ID == "config.render":
		if err := preflightSignedAttemptStateNamespaces(e.cfg, e.stateDir); err != nil {
			return err
		}
		if err := e.ensurePayloads(ctx); err != nil {
			return err
		}
		return RenderRuntimeConfigs(e.cfg, e.stateDir, e.roles)
	case a.ID == "topology.launch":
		return nil
	case a.ID == "churn.tournament-complete":
		return nil
	default:
		return fmt.Errorf("no executor for %s", a.ID)
	}
}

func bootstrapPolicyMatches(cfg *ResolvedConfig, policy stabi.STCoordinatorPolicySnapshot) bool {
	if cfg == nil || policy.EpochDepositCapRao == nil || policy.CampaignDepositCapRao == nil {
		return false
	}
	hash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		return false
	}
	return policy.PolicyHash == hash && policy.EffectiveBlock != 0 &&
		policy.EpochBlocks == cfg.Policy.Settlement.EpochBlocks &&
		policy.RootCommitWindowBlocks == cfg.Policy.Settlement.RootCommitWindowBlocks &&
		policy.FinalizeOffsetBlocks == cfg.Policy.Settlement.FinalizeOffsetBlocks &&
		policy.CloseGraceBlocks == cfg.Policy.Settlement.CloseGraceBlocks &&
		policy.ClaimTTLEpochs == cfg.Policy.Settlement.ClaimTTLEpochs &&
		policy.ClaimGraceEpochs == cfg.Policy.Settlement.ClaimGraceEpochs &&
		policy.MaximumBindingValidityEpochs == cfg.Policy.Binding.MaximumValidityEpochs &&
		policy.CommitmentMaxAgeBlocks == cfg.Policy.Settlement.EpochBlocks*2 &&
		policy.EpochDepositCapRao.Cmp(new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator)) == 0 &&
		policy.CampaignDepositCapRao.Cmp(new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao)) == 0
}

func coordinatorUpgradeActivationBaseline(plan *SetupPlan, payloads *DeploymentPayloads) (common.Address, string, error) {
	if plan == nil || payloads == nil {
		return common.Address{}, "", errors.New("coordinator activation baseline is unavailable")
	}
	address := payloads.Manifest.CoordinatorImplementation
	hashes, err := normalizedDeploymentRuntimeHashes(payloads.Manifest)
	if err != nil {
		return common.Address{}, "", err
	}
	runtimeHash := hashes[address]
	if plan.CoordinatorUpgradeBaseline.isRepeated() {
		if !common.IsHexAddress(plan.CoordinatorUpgradeBaseline.ActiveImplementation) {
			return common.Address{}, "", errors.New("repeated coordinator activation has no valid active implementation")
		}
		address = common.HexToAddress(plan.CoordinatorUpgradeBaseline.ActiveImplementation)
		runtimeHash = plan.CoordinatorUpgradeBaseline.ActiveImplementationHash
	}
	if address == (common.Address{}) || address == payloads.CoordinatorUpgrade.Implementation {
		return common.Address{}, "", errors.New("coordinator activation baseline is empty or self-referential")
	}
	if _, err := decodeHex32("coordinator activation baseline runtime", runtimeHash); err != nil {
		return common.Address{}, "", err
	}
	return address, runtimeHash, nil
}

func validateCoordinatorUpgradeActivationPrestate(active common.Address, activeCode []byte, upgrade CoordinatorUpgrade, baselineAddress common.Address, baselineHash string) (bool, error) {
	if active == upgrade.Implementation {
		return true, nil
	}
	if active != baselineAddress {
		return false, fmt.Errorf("coordinator proxy implementation %s is neither baseline %s nor approved upgrade", active, baselineAddress)
	}
	got := crypto.Keccak256Hash(activeCode).Hex()
	if len(activeCode) == 0 || !strings.EqualFold(got, baselineHash) {
		return false, fmt.Errorf("coordinator activation baseline runtime=%s want=%s", got, baselineHash)
	}
	return false, nil
}

func (e *Executor) activateCoordinatorUpgrade(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	upgrade := e.payloads.CoordinatorUpgrade
	if a.Parameters["implementation"] != upgrade.Implementation.Hex() || !strings.EqualFold(a.Parameters["runtime_code_hash"], upgrade.RuntimeCodeHash) {
		return errors.New("coordinator upgrade action does not bind the approved implementation")
	}
	head, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return err
	}
	code, err := codeAtCanonicalHead(ctx, e.owner, upgrade.Implementation, head)
	if err != nil || !strings.EqualFold(crypto.Keccak256Hash(code).Hex(), upgrade.RuntimeCodeHash) {
		return stateMismatchError(err, "coordinator upgrade runtime mismatch at %s", upgrade.Implementation)
	}
	proxy := e.payloads.Manifest.CoordinatorProxy
	active, err := implementationAt(ctx, e.owner, proxy, head)
	if err != nil {
		return err
	}
	baselineAddress, baselineHash, err := coordinatorUpgradeActivationBaseline(e.plan, e.payloads)
	if err != nil {
		return err
	}
	activeCode, err := codeAtCanonicalHead(ctx, e.owner, active, head)
	if err != nil {
		return err
	}
	alreadyActive, err := validateCoordinatorUpgradeActivationPrestate(active, activeCode, upgrade, baselineAddress, baselineHash)
	if err != nil {
		return err
	}
	if alreadyActive {
		return nil
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	data, err := parsed.Pack("upgradeToAndCall", upgrade.Implementation, []byte{})
	if err != nil {
		return err
	}
	if _, err := e.owner.Send(ctx, e.plan.PlanHash, a, &proxy, big.NewInt(0), data); err != nil {
		return err
	}
	postHead, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return err
	}
	active, err = implementationAt(ctx, e.owner, proxy, postHead)
	if err != nil || active != upgrade.Implementation {
		return stateMismatchError(err, "coordinator proxy implementation=%s want=%s", active, upgrade.Implementation)
	}
	return nil
}

func (e *Executor) bootstrapPolicyState(ctx context.Context, block uint64) (uint64, uint64, stabi.STCoordinatorPolicySnapshot, error) {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return 0, 0, stabi.STCoordinatorPolicySnapshot{}, err
	}
	proxy := e.payloads.Manifest.CoordinatorProxy
	currentValues, err := contractCallAt(ctx, e.owner.client, proxy, parsed, "currentEpoch", block)
	if err != nil || len(currentValues) != 1 {
		return 0, 0, stabi.STCoordinatorPolicySnapshot{}, stateMismatchError(err, "bootstrap currentEpoch returned %d values", len(currentValues))
	}
	current, ok := currentValues[0].(*big.Int)
	if !ok || !current.IsUint64() {
		return 0, 0, stabi.STCoordinatorPolicySnapshot{}, fmt.Errorf("bootstrap currentEpoch returned %T", currentValues[0])
	}
	countValues, err := contractCallAt(ctx, e.owner.client, proxy, parsed, "policyCount", block)
	if err != nil || len(countValues) != 1 {
		return 0, 0, stabi.STCoordinatorPolicySnapshot{}, stateMismatchError(err, "bootstrap policyCount returned %d values", len(countValues))
	}
	count, ok := countValues[0].(*big.Int)
	if !ok || !count.IsUint64() || count.Sign() == 0 {
		return 0, 0, stabi.STCoordinatorPolicySnapshot{}, fmt.Errorf("bootstrap policyCount returned %T", countValues[0])
	}
	policyValues, err := contractCallAt(ctx, e.owner.client, proxy, parsed, "policyAt", block, current)
	if err != nil {
		return 0, 0, stabi.STCoordinatorPolicySnapshot{}, err
	}
	policy, err := coordinatorPolicy(policyValues)
	return current.Uint64(), count.Uint64(), policy, err
}

type bootstrapPolicyMigrationOperator struct {
	ID, Principal, Conviction, NextDepositNonce *big.Int
}

type bootstrapPolicyMigrationObservation struct {
	CampaignReserved, ReservePrincipal, ReserveLiveStake *big.Int
	Vault                                                map[string]*big.Int
	Operators                                            []bootstrapPolicyMigrationOperator
}

// Compare the complete stopped-topology accounting surface with the exact
// history-derived pre-campaign balance. A policy acceleration may follow a
// verified voluntary conviction, so requiring an empty reserve is incorrect;
// every nonzero rao and nonce must instead be explained by authenticated
// lineage evidence.
func validateBootstrapPolicyMigrationObservation(expected policyRevisionReserveAccounting, operatorCount int, observed bootstrapPolicyMigrationObservation) error {
	if operatorCount <= 0 || len(observed.Operators) != operatorCount {
		return fmt.Errorf("policy migration operator count=%d, want %d", len(observed.Operators), operatorCount)
	}
	wantReserved := new(big.Int).SetUint64(expected.CampaignReservedRao)
	if observed.CampaignReserved == nil || observed.CampaignReserved.Cmp(wantReserved) != 0 {
		return fmt.Errorf("policy migration campaignReserved=%v, want authenticated %s", observed.CampaignReserved, wantReserved)
	}
	if observed.ReservePrincipal == nil || observed.ReservePrincipal.Cmp(wantReserved) != 0 {
		return fmt.Errorf("policy migration reserve principal=%v, want authenticated %s", observed.ReservePrincipal, wantReserved)
	}
	if observed.ReserveLiveStake == nil || observed.ReserveLiveStake.Cmp(wantReserved) < 0 {
		return fmt.Errorf("policy migration reserve liveStake=%v does not back principal %s", observed.ReserveLiveStake, wantReserved)
	}
	for _, field := range []string{"totalCaptured", "totalPaid", "escrowAccounted", "pendingFunding", "outstandingLiability"} {
		value, ok := observed.Vault[field]
		if !ok || value == nil || value.Sign() != 0 {
			return fmt.Errorf("policy migration vault %s=%v, want zero", field, value)
		}
	}
	for index, operator := range observed.Operators {
		noID := index + 1
		wantPrincipal := uint64(0)
		if noID == 1 {
			wantPrincipal = expected.CampaignReservedRao
		}
		wantNonce := expected.NextDepositNonces[noID]
		if operator.ID == nil || !operator.ID.IsUint64() || operator.ID.Uint64() != uint64(noID) {
			return fmt.Errorf("policy migration operatorIdAt(%d)=%v, want %d", index, operator.ID, noID)
		}
		wantPrincipalValue := new(big.Int).SetUint64(wantPrincipal)
		if operator.Principal == nil || operator.Principal.Cmp(wantPrincipalValue) != 0 {
			return fmt.Errorf("policy migration operator %d principal=%v, want authenticated %d", noID, operator.Principal, wantPrincipal)
		}
		if operator.Conviction == nil || operator.Conviction.Cmp(wantPrincipalValue) != 0 {
			return fmt.Errorf("policy migration operator %d cumulative conviction=%v, want authenticated %d", noID, operator.Conviction, wantPrincipal)
		}
		wantNonceValue := new(big.Int).SetUint64(wantNonce)
		if operator.NextDepositNonce == nil || operator.NextDepositNonce.Cmp(wantNonceValue) != 0 {
			return fmt.Errorf("policy migration operator %d nonce=%v, want authenticated %d", noID, operator.NextDepositNonce, wantNonce)
		}
	}
	return nil
}

// Select zero accounting for a pristine deployment, or reconstruct the exact
// nonzero balance from a verified voluntary-conviction lineage. Merely finding
// a reconciliation action or finalized mutation is enough to require the full
// authenticator; missing evidence cannot fall back to an empty-state check.
func (e *Executor) expectedBootstrapPolicyMigrationAccounting() (policyRevisionReserveAccounting, error) {
	if e == nil || e.cfg == nil || e.plan == nil || e.journal == nil {
		return policyRevisionReserveAccounting{}, errors.New("policy migration accounting context is incomplete")
	}
	entries := e.journal.Entries()
	required := false
	for _, action := range e.plan.Actions {
		if action.ID == voluntaryConvictionReconciliationActionID {
			required = true
			break
		}
	}
	if !required {
		allowed := e.plan.allowedPlanHashes()
		for _, entry := range entries {
			if allowed[entry.PlanHash] && entry.ActionID == voluntaryConvictionActionID && (entry.Stage == StageFinalized || entry.Stage == StageVerified) {
				required = true
				break
			}
		}
	}
	if !required {
		return policyRevisionReserveAccounting{NextDepositNonces: map[int]uint64{}}, nil
	}
	return authenticatedPolicyRevisionReserveAccounting(e.cfg, e.stateDir, e.plan, entries, planRevisionRecoveries{})
}

func (e *Executor) verifyBootstrapPolicyMigrationState(ctx context.Context, block uint64) error {
	coordinator, vault, reserve := stabi.NewSTCoordinator(), stabi.NewSTSettlementVault(), stabi.NewSTReserveSink()
	proxy := e.payloads.Manifest.CoordinatorProxy
	expected, err := e.expectedBootstrapPolicyMigrationAccounting()
	if err != nil {
		return err
	}
	reserved, err := rawCoordinatorCallAt(ctx, e.owner, proxy, coordinator.PackCampaignReserved(), coordinator.UnpackCampaignReserved, block)
	if err != nil {
		return stateMismatchError(err, "policy migration campaignReserved")
	}
	operatorCount, err := rawCoordinatorCallAt(ctx, e.owner, proxy, coordinator.PackOperatorCount(), coordinator.UnpackOperatorCount, block)
	if err != nil || operatorCount == nil || !operatorCount.IsUint64() || operatorCount.Uint64() != uint64(e.cfg.Config.Topology.Operators) {
		return stateMismatchError(err, "policy migration operatorCount=%v, want %d", operatorCount, e.cfg.Config.Topology.Operators)
	}
	observation := bootstrapPolicyMigrationObservation{
		CampaignReserved: reserved,
		Vault:            map[string]*big.Int{},
		Operators:        make([]bootstrapPolicyMigrationOperator, e.cfg.Config.Topology.Operators),
	}
	observation.ReservePrincipal, err = rawCoordinatorCallAt(ctx, e.owner, e.payloads.Manifest.ReserveSink, reserve.PackPrincipal(), reserve.UnpackPrincipal, block)
	if err != nil {
		return stateMismatchError(err, "policy migration reserve principal")
	}
	observation.ReserveLiveStake, err = rawCoordinatorCallAt(ctx, e.owner, e.payloads.Manifest.ReserveSink, reserve.PackLiveStake(), reserve.UnpackLiveStake, block)
	if err != nil {
		return stateMismatchError(err, "policy migration reserve liveStake")
	}
	vaultReads := []struct {
		name   string
		pack   []byte
		unpack func([]byte) (*big.Int, error)
	}{
		{"totalCaptured", vault.PackTotalCaptured(), vault.UnpackTotalCaptured},
		{"totalPaid", vault.PackTotalPaid(), vault.UnpackTotalPaid},
		{"escrowAccounted", vault.PackEscrowAccounted(), vault.UnpackEscrowAccounted},
		{"pendingFunding", vault.PackPendingFunding(), vault.UnpackPendingFunding},
		{"outstandingLiability", vault.PackOutstandingLiability(), vault.UnpackOutstandingLiability},
	}
	for _, read := range vaultReads {
		value, readErr := rawCoordinatorCallAt(ctx, e.owner, e.payloads.Manifest.SettlementVault, read.pack, read.unpack, block)
		if readErr != nil {
			return stateMismatchError(readErr, "policy migration vault %s", read.name)
		}
		observation.Vault[read.name] = value
	}
	for noID := 1; noID <= e.cfg.Config.Topology.Operators; noID++ {
		id := big.NewInt(int64(noID))
		operator := &observation.Operators[noID-1]
		operator.ID, err = rawCoordinatorCallAt(ctx, e.owner, proxy, coordinator.PackOperatorIdAt(big.NewInt(int64(noID-1))), coordinator.UnpackOperatorIdAt, block)
		if err != nil {
			return stateMismatchError(err, "policy migration operatorIdAt(%d)", noID-1)
		}
		operator.Principal, err = rawCoordinatorCallAt(ctx, e.owner, e.payloads.Manifest.ReserveSink, reserve.PackOperatorPrincipal(id), reserve.UnpackOperatorPrincipal, block)
		if err != nil {
			return stateMismatchError(err, "policy migration operator %d principal", noID)
		}
		operator.Conviction, err = rawCoordinatorCallAt(ctx, e.owner, proxy, coordinator.PackCumulativeConviction(id), coordinator.UnpackCumulativeConviction, block)
		if err != nil {
			return stateMismatchError(err, "policy migration operator %d cumulative conviction", noID)
		}
		operator.NextDepositNonce, err = rawCoordinatorCallAt(ctx, e.owner, proxy, coordinator.PackNextDepositNonce(id), coordinator.UnpackNextDepositNonce, block)
		if err != nil {
			return stateMismatchError(err, "policy migration operator %d nonce", noID)
		}
	}
	return validateBootstrapPolicyMigrationObservation(expected, e.cfg.Config.Topology.Operators, observation)
}

func (e *Executor) scheduleBootstrapPolicy(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	head, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return err
	}
	current, count, active, err := e.bootstrapPolicyState(ctx, head.Number)
	if err != nil {
		return err
	}
	if bootstrapPolicyMatches(e.cfg, active) {
		return nil
	}
	if err := e.verifyBootstrapPolicyMigrationState(ctx, head.Number); err != nil {
		return err
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	proxy := e.payloads.Manifest.CoordinatorProxy
	if count > 1 {
		lastValues, readErr := contractCallAt(ctx, e.owner.client, proxy, parsed, "policyByIndex", head.Number, new(big.Int).SetUint64(count-1))
		if readErr != nil {
			return readErr
		}
		last, convertErr := coordinatorPolicy(lastValues)
		if convertErr != nil || !bootstrapPolicyMatches(e.cfg, last) || last.EffectiveEpoch <= current {
			return errors.New("coordinator has a foreign policy scheduled before bootstrap migration")
		}
		return nil
	}
	if e.cfg.Policy.Settlement.EpochBlocks > ^uint64(0)/2 {
		return errors.New("bootstrap policy arithmetic overflows uint64")
	}
	window, err := waitFutureEpochTransactionWindow(ctx, e.owner, proxy, stabi.NewSTCoordinator())
	if err != nil {
		return err
	}
	hash, err := decodeHash(e.cfg.PolicyHash)
	if err != nil {
		return err
	}
	next := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: window.EffectiveEpoch,
		EpochBlocks:                  e.cfg.Policy.Settlement.EpochBlocks,
		RootCommitWindowBlocks:       e.cfg.Policy.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks:         e.cfg.Policy.Settlement.FinalizeOffsetBlocks,
		CloseGraceBlocks:             e.cfg.Policy.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs:               e.cfg.Policy.Settlement.ClaimTTLEpochs,
		ClaimGraceEpochs:             e.cfg.Policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: e.cfg.Policy.Binding.MaximumValidityEpochs,
		CommitmentMaxAgeBlocks:       e.cfg.Policy.Settlement.EpochBlocks * 2,
		EpochDepositCapRao:           new(big.Int).SetUint64(e.cfg.Policy.Deposit.EpochCapRaoPerOperator),
		CampaignDepositCapRao:        new(big.Int).SetUint64(e.cfg.Policy.Deposit.TotalTestCampaignCapRao),
	}
	data, err := parsed.Pack("schedulePolicy", next)
	if err != nil {
		return err
	}
	if _, err := e.owner.Send(ctx, e.plan.PlanHash, a, &proxy, big.NewInt(0), data); err != nil {
		return err
	}
	return nil
}

func (e *Executor) awaitBootstrapPolicy(ctx context.Context) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()
	for {
		head, err := finalizedEVMHead(ctx, e.owner.client)
		if err != nil {
			return err
		}
		_, _, active, err := e.bootstrapPolicyState(ctx, head.Number)
		if err != nil {
			return err
		}
		if bootstrapPolicyMatches(e.cfg, active) {
			return nil
		}
		if err := e.verifyBootstrapPolicyMigrationState(ctx, head.Number); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for bootstrap policy activation: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

type ProductionPolicyEvidence struct {
	Schema                 string `json:"schema"`
	DeploymentID           string `json:"deployment_id"`
	PolicyHash             string `json:"policy_hash"`
	ReleaseRunID           string `json:"release_run_id"`
	ReleaseResultHash      string `json:"release_result_hash"`
	ReleaseCompleteHash    string `json:"release_complete_hash"`
	ReleaseHandoffHash     string `json:"release_handoff_hash"`
	ReleaseHandoffSize     uint64 `json:"release_handoff_size_bytes"`
	CampaignStartEpoch     uint64 `json:"campaign_start_epoch"`
	CampaignEndEpoch       uint64 `json:"campaign_end_epoch"`
	ScheduledFromEpoch     uint64 `json:"scheduled_from_epoch"`
	EffectiveEpoch         uint64 `json:"effective_epoch"`
	EffectiveBlock         uint64 `json:"effective_block"`
	PriorEpochBlocks       uint64 `json:"prior_epoch_blocks"`
	EpochBlocks            uint64 `json:"epoch_blocks"`
	RootCommitWindowBlocks uint64 `json:"root_commit_window_blocks"`
	FinalizeOffsetBlocks   uint64 `json:"finalize_offset_blocks"`
	CloseGraceBlocks       uint64 `json:"close_grace_blocks"`
	TransactionHash        string `json:"transaction_hash,omitempty"`
	FinalizedBlock         uint64 `json:"finalized_block,omitempty"`
	FinalizedBlockHash     string `json:"finalized_block_hash,omitempty"`
}

func (e *Executor) boundProductionReleaseGate() (*ReleaseCampaignGate, error) {
	if e == nil || e.cfg == nil || e.plan == nil || e.roles == nil {
		return nil, errors.New("production release gate context is incomplete")
	}
	var gate *ReleaseCampaignGate
	if e.releaseGate != nil {
		copyGate := *e.releaseGate
		gate = &copyGate
	} else {
		attempt, err := readScenarioCampaignAttempt(e.cfg, e.stateDir, e.roles, e.plan.PlanHash, "production-soak")
		if err != nil {
			return nil, fmt.Errorf("read durable production attempt: %w", err)
		}
		if attempt.payload.PriorRelease == nil {
			return nil, errors.New("durable production attempt has no release gate")
		}
		copyGate := *attempt.payload.PriorRelease
		gate = &copyGate
	}
	if _, _, err := validateExactReleaseCampaignGate(e.cfg, e.stateDir, e.roles, gate); err != nil {
		return nil, err
	}
	return gate, nil
}

func coordinatorPolicy(values []any) (stabi.STCoordinatorPolicySnapshot, error) {
	if len(values) != 1 {
		return stabi.STCoordinatorPolicySnapshot{}, fmt.Errorf("policy getter returned %d values", len(values))
	}
	converted, ok := abi.ConvertType(values[0], new(stabi.STCoordinatorPolicySnapshot)).(*stabi.STCoordinatorPolicySnapshot)
	if !ok || converted == nil || converted.EpochDepositCapRao == nil || converted.CampaignDepositCapRao == nil {
		return stabi.STCoordinatorPolicySnapshot{}, fmt.Errorf("policy getter returned %T", values[0])
	}
	return *converted, nil
}

func productionPolicyMatches(cfg *ResolvedConfig, policy stabi.STCoordinatorPolicySnapshot) bool {
	if cfg == nil || policy.EpochDepositCapRao == nil || policy.CampaignDepositCapRao == nil {
		return false
	}
	wantHash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		return false
	}
	p := cfg.Policy.ProductionCadence
	return policy.PolicyHash == wantHash && policy.EffectiveEpoch != 0 && policy.EffectiveBlock != 0 &&
		policy.EpochBlocks == p.EpochBlocks && policy.RootCommitWindowBlocks == p.RootCommitWindowBlocks &&
		policy.FinalizeOffsetBlocks == p.FinalizeOffsetBlocks && policy.CloseGraceBlocks == p.CloseGraceBlocks &&
		policy.ClaimTTLEpochs == cfg.Policy.Settlement.ClaimTTLEpochs && policy.ClaimGraceEpochs == cfg.Policy.Settlement.ClaimGraceEpochs &&
		policy.MaximumBindingValidityEpochs == cfg.Policy.Binding.MaximumValidityEpochs && policy.CommitmentMaxAgeBlocks == p.EpochBlocks*2 &&
		policy.EpochDepositCapRao.Cmp(new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator)) == 0 &&
		policy.CampaignDepositCapRao.Cmp(new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao)) == 0
}

// validateProductionScheduleEpoch gates both a first write and interrupted
// adoption on the authenticated live campaign. Setup epochs are irrelevant;
// a canonical schedule may be written later, but its effective epoch must be
// strictly after the release evidence boundary.
func validateProductionScheduleEpoch(currentEpoch, campaignEndEpoch, scheduledEffectiveEpoch uint64) error {
	if currentEpoch < campaignEndEpoch {
		return fmt.Errorf("production cadence requires release campaign through epoch %d; current epoch is %d", campaignEndEpoch, currentEpoch)
	}
	if scheduledEffectiveEpoch != 0 && scheduledEffectiveEpoch <= campaignEndEpoch {
		return fmt.Errorf("production cadence effective epoch %d does not follow release campaign epoch %d", scheduledEffectiveEpoch, campaignEndEpoch)
	}
	return nil
}

func policySnapshotEqual(a, b stabi.STCoordinatorPolicySnapshot) bool {
	return a.PolicyHash == b.PolicyHash && a.EffectiveEpoch == b.EffectiveEpoch && a.EffectiveBlock == b.EffectiveBlock &&
		a.EpochBlocks == b.EpochBlocks && a.RootCommitWindowBlocks == b.RootCommitWindowBlocks && a.FinalizeOffsetBlocks == b.FinalizeOffsetBlocks &&
		a.CloseGraceBlocks == b.CloseGraceBlocks && a.ClaimTTLEpochs == b.ClaimTTLEpochs && a.ClaimGraceEpochs == b.ClaimGraceEpochs &&
		a.MaximumBindingValidityEpochs == b.MaximumBindingValidityEpochs && a.CommitmentMaxAgeBlocks == b.CommitmentMaxAgeBlocks &&
		a.EpochDepositCapRao != nil && b.EpochDepositCapRao != nil && a.EpochDepositCapRao.Cmp(b.EpochDepositCapRao) == 0 &&
		a.CampaignDepositCapRao != nil && b.CampaignDepositCapRao != nil && a.CampaignDepositCapRao.Cmp(b.CampaignDepositCapRao) == 0
}

func productionPolicyReceiptMatches(receipt *ethTypes.Receipt, coordinator common.Address, policy stabi.STCoordinatorPolicySnapshot, index uint64) error {
	if receipt == nil || receipt.Status != ethTypes.ReceiptStatusSuccessful {
		return errors.New("production policy receipt is not successful")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	event, ok := parsed.Events["PolicyScheduled"]
	if !ok {
		return errors.New("Coordinator ABI lacks PolicyScheduled")
	}
	for _, log := range receipt.Logs {
		if log.Address != coordinator || len(log.Topics) != 4 || log.Topics[0] != event.ID || !log.Topics[1].Big().IsUint64() || log.Topics[1].Big().Uint64() != index || log.Topics[2] != common.BytesToHash(policy.PolicyHash[:]) || !log.Topics[3].Big().IsUint64() || log.Topics[3].Big().Uint64() != policy.EffectiveEpoch {
			continue
		}
		values, unpackErr := event.Inputs.NonIndexed().Unpack(log.Data)
		if unpackErr == nil && len(values) == 1 {
			if effectiveBlock, blockOK := values[0].(uint64); blockOK && effectiveBlock == policy.EffectiveBlock {
				return nil
			}
		}
	}
	return errors.New("finalized production receipt lacks the exact PolicyScheduled event")
}

func validateProductionPolicyTransaction(transaction *ethTypes.Transaction, chainID *big.Int, signer, coordinator common.Address, data []byte) error {
	if transaction == nil || chainID == nil || chainID.Sign() <= 0 || signer == (common.Address{}) || coordinator == (common.Address{}) || len(data) == 0 {
		return errors.New("production policy transaction approval context is incomplete")
	}
	observedSigner, err := ethTypes.Sender(ethTypes.LatestSignerForChainID(chainID), transaction)
	if err != nil {
		return err
	}
	if observedSigner != signer || transaction.To() == nil || *transaction.To() != coordinator || transaction.Value().Sign() != 0 || !bytes.Equal(transaction.Data(), data) {
		return errors.New("persisted production policy transaction does not match the approved owner, target, value, and snapshot")
	}
	return nil
}

// Recover only the exact approved schedule transaction after a process dies
// between finality and evidence persistence. A structurally similar policy
// written out of band is never adopted.
func (e *Executor) recoverProductionPolicyReceipt(ctx context.Context, action Action, parsed abi.ABI, coordinator common.Address, policy stabi.STCoordinatorPolicySnapshot, index uint64) (*ethTypes.Receipt, error) {
	if e == nil || e.plan == nil || e.journal == nil || e.owner == nil {
		return nil, errors.New("production policy recovery context is incomplete")
	}
	var finalized *JournalEntry
	allowedPlans := e.plan.allowedPlanHashes()
	entries := e.journal.Entries()
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if allowedPlans[entry.PlanHash] && entry.ActionID == action.ID && actionAcceptsIntent(action, entry.IntentHash) && entry.Stage == StageFinalized {
			copy := entry
			finalized = &copy
			break
		}
	}
	if finalized == nil || !validConformanceTransaction(finalized.TransactionHash, finalized.BlockHash, finalized.BlockNumber) {
		return nil, errors.New("canonical production policy has no approved finalized transaction")
	}
	raw, err := os.ReadFile(filepath.Join(e.stateDir, "transactions", stringsTrim0x(finalized.TransactionHash)+".rlp"))
	if err != nil {
		return nil, err
	}
	var transaction ethTypes.Transaction
	if err := transaction.UnmarshalBinary(raw); err != nil || !strings.EqualFold(transaction.Hash().Hex(), finalized.TransactionHash) {
		return nil, stateMismatchError(err, "persisted production policy transaction hash mismatch")
	}
	wantSigner := crypto.PubkeyToAddress(e.owner.key.PublicKey)
	input := policy
	input.EffectiveBlock = 0
	wantData, err := parsed.Pack("schedulePolicy", input)
	if err != nil {
		return nil, err
	}
	if err := validateProductionPolicyTransaction(&transaction, e.owner.chainID, wantSigner, coordinator, wantData); err != nil {
		return nil, err
	}
	head, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return nil, err
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, e.owner.client, head, finalized.TransactionHash, finalized.BlockNumber, finalized.BlockHash)
	if err != nil {
		return nil, err
	}
	if err := productionPolicyReceiptMatches(receipt, coordinator, policy, index); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (e *Executor) scheduleProductionPolicy(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	address := e.payloads.Manifest.CoordinatorProxy
	gate, err := e.boundProductionReleaseGate()
	if err != nil {
		return fmt.Errorf("production release gate: %w", err)
	}
	epochValues, err := contractCall(ctx, e.owner.client, address, parsed, "currentEpoch")
	if err != nil || len(epochValues) != 1 {
		return stateMismatchError(err, "read current epoch returned %d values", len(epochValues))
	}
	current, ok := epochValues[0].(*big.Int)
	if !ok || !current.IsUint64() {
		return fmt.Errorf("currentEpoch returned %T", epochValues[0])
	}
	currentEpoch := current.Uint64()
	p := e.cfg.Policy.ProductionCadence
	countValues, err := contractCall(ctx, e.owner.client, address, parsed, "policyCount")
	if err != nil || len(countValues) != 1 {
		return stateMismatchError(err, "read policy count returned %d values", len(countValues))
	}
	count, ok := countValues[0].(*big.Int)
	if !ok || !count.IsUint64() || count.Sign() == 0 {
		return fmt.Errorf("policyCount returned %T", countValues[0])
	}
	lastIndex := new(big.Int).Sub(new(big.Int).Set(count), big.NewInt(1))
	lastValues, err := contractCall(ctx, e.owner.client, address, parsed, "policyByIndex", lastIndex)
	if err != nil {
		return err
	}
	lastPolicy, err := coordinatorPolicy(lastValues)
	if err != nil {
		return err
	}
	alreadyScheduled := productionPolicyMatches(e.cfg, lastPolicy)
	scheduledEffectiveEpoch := uint64(0)
	if alreadyScheduled {
		scheduledEffectiveEpoch = lastPolicy.EffectiveEpoch
	}
	if err := validateProductionScheduleEpoch(currentEpoch, gate.EndEpoch, scheduledEffectiveEpoch); err != nil {
		return err
	}
	if alreadyScheduled {
		evidencePath := filepath.Join(e.stateDir, "public", "production-policy.json")
		var evidence ProductionPolicyEvidence
		if readErr := decodeStrictJSONFile(evidencePath, &evidence); readErr == nil {
			if !productionPolicyEvidenceMatches(e.cfg, evidence, gate) || evidence.EffectiveEpoch != lastPolicy.EffectiveEpoch || evidence.EffectiveBlock != lastPolicy.EffectiveBlock {
				return errors.New("existing production policy evidence does not match the canonical schedule")
			}
			return nil
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return fmt.Errorf("read production policy evidence: %w", readErr)
		}
		receipt, recoverErr := e.recoverProductionPolicyReceipt(ctx, a, parsed, address, lastPolicy, count.Uint64()-1)
		if recoverErr != nil {
			return recoverErr
		}
		return e.writeProductionPolicyEvidence(lastPolicy, lastPolicy.EffectiveEpoch-1, gate, receipt)
	}
	// A migrated deployment has one historical policy plus the canonical
	// accelerated snapshot. A fresh deployment has only the latter. Anything
	// else is an unreviewed policy history.
	if count.Uint64() > 2 || !bootstrapPolicyMatches(e.cfg, lastPolicy) {
		return fmt.Errorf("coordinator has an unreviewed %d-version policy history before production", count.Uint64())
	}
	priorValues, err := contractCall(ctx, e.owner.client, address, parsed, "policyAt", current)
	if err != nil {
		return err
	}
	prior, err := coordinatorPolicy(priorValues)
	if err != nil {
		return err
	}
	if prior.EpochBlocks != e.cfg.Policy.Settlement.EpochBlocks || prior.RootCommitWindowBlocks != e.cfg.Policy.Settlement.RootCommitWindowBlocks || prior.FinalizeOffsetBlocks != e.cfg.Policy.Settlement.FinalizeOffsetBlocks || prior.CloseGraceBlocks != e.cfg.Policy.Settlement.CloseGraceBlocks {
		return errors.New("active accelerated policy is not canonical; refusing production transition")
	}
	if p.EpochBlocks > ^uint64(0)/2 {
		return errors.New("production policy arithmetic overflows uint64")
	}
	window, err := waitFutureEpochTransactionWindow(ctx, e.owner, address, stabi.NewSTCoordinator())
	if err != nil {
		return err
	}
	if err := validateProductionScheduleEpoch(window.CurrentEpoch, gate.EndEpoch, 0); err != nil {
		return err
	}
	hash, err := decodeHash(e.cfg.PolicyHash)
	if err != nil {
		return err
	}
	next := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: window.EffectiveEpoch,
		EpochBlocks: p.EpochBlocks, RootCommitWindowBlocks: p.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: p.FinalizeOffsetBlocks, CloseGraceBlocks: p.CloseGraceBlocks,
		ClaimTTLEpochs: e.cfg.Policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: e.cfg.Policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: e.cfg.Policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: p.EpochBlocks * 2,
		EpochDepositCapRao: new(big.Int).SetUint64(e.cfg.Policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(e.cfg.Policy.Deposit.TotalTestCampaignCapRao),
	}
	data, err := parsed.Pack("schedulePolicy", next)
	if err != nil {
		return err
	}
	receipt, err := e.owner.Send(ctx, e.plan.PlanHash, a, &address, big.NewInt(0), data)
	if err != nil {
		return err
	}
	postValues, err := contractCall(ctx, e.owner.client, address, parsed, "policyAt", new(big.Int).SetUint64(next.EffectiveEpoch))
	if err != nil {
		return err
	}
	post, err := coordinatorPolicy(postValues)
	if err != nil || !productionPolicyMatches(e.cfg, post) || post.EffectiveEpoch != next.EffectiveEpoch {
		return errors.New("production policy post-state does not match the approved cadence")
	}
	// Scheduling must not mutate the current short epoch snapshot.
	currentValues, err := contractCall(ctx, e.owner.client, address, parsed, "policyAt", current)
	if err != nil {
		return err
	}
	currentPost, err := coordinatorPolicy(currentValues)
	if err != nil || !policySnapshotEqual(currentPost, prior) {
		return errors.New("production scheduling changed the active accelerated policy")
	}
	return e.writeProductionPolicyEvidence(post, window.CurrentEpoch, gate, receipt)
}

func (e *Executor) writeProductionPolicyEvidence(policy stabi.STCoordinatorPolicySnapshot, scheduledFrom uint64, gate *ReleaseCampaignGate, receipt *ethTypes.Receipt) error {
	if gate == nil {
		return errors.New("production policy evidence requires the release campaign gate")
	}
	evidence := ProductionPolicyEvidence{
		Schema: "urnetwork-production-policy-evidence-v2", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
		PolicyHash: e.cfg.PolicyHash, ReleaseRunID: gate.RunID, ReleaseResultHash: gate.ResultHash, ReleaseCompleteHash: gate.CompleteContentHash,
		ReleaseHandoffHash: gate.LifecycleHandoff.ContentHash, ReleaseHandoffSize: gate.LifecycleHandoff.SizeBytes,
		CampaignStartEpoch: gate.StartEpoch, CampaignEndEpoch: gate.EndEpoch, ScheduledFromEpoch: scheduledFrom, EffectiveEpoch: policy.EffectiveEpoch,
		EffectiveBlock: policy.EffectiveBlock, PriorEpochBlocks: e.cfg.Policy.Settlement.EpochBlocks,
		EpochBlocks: policy.EpochBlocks, RootCommitWindowBlocks: policy.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: policy.FinalizeOffsetBlocks, CloseGraceBlocks: policy.CloseGraceBlocks,
	}
	if receipt != nil {
		evidence.TransactionHash = receipt.TxHash.Hex()
		evidence.FinalizedBlock = receipt.BlockNumber.Uint64()
		evidence.FinalizedBlockHash = receipt.BlockHash.Hex()
	}
	return writePublicJSON(filepath.Join(e.stateDir, "public", "production-policy.json"), evidence)
}

func (e *Executor) setProductionHyperparameter(ctx context.Context, a Action, name string) error {
	want, ok := e.cfg.Hyperparameters.ProductionOwnerControlled[name]
	shape, supported := hyperShapes[name]
	if !ok || !supported {
		return fmt.Errorf("production hyperparameter %q is not canonical or supported", name)
	}
	got, err := e.substrate.ReadHyper(name)
	if err != nil {
		return err
	}
	if hyperEqual(got, want, shape.Kind) {
		return nil
	}
	call, err := e.substrate.HyperCall(name, want)
	if err != nil {
		return err
	}
	if _, _, err = e.substrate.Send(ctx, e.plan.PlanHash, a, call); err != nil {
		return err
	}
	got, err = e.substrate.ReadHyper(name)
	if err != nil {
		return err
	}
	if !hyperEqual(got, want, shape.Kind) {
		return fmt.Errorf("production %s post-state %v, want %v", name, got, want)
	}
	return nil
}

func suffixInt(id string) int {
	parts := strings.Split(id, ".")
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}

func fleetBindingActionIndices(id string) (int, int, error) {
	parts := strings.Split(id, ".")
	if len(parts) != 4 || parts[0] != "fleet" || parts[1] != "bind" {
		return 0, 0, fmt.Errorf("invalid fleet binding action %q", id)
	}
	fleet, err := strconv.Atoi(parts[2])
	if err != nil || fleet < 1 {
		return 0, 0, fmt.Errorf("invalid fleet index in %q", id)
	}
	member, err := strconv.Atoi(parts[3])
	if err != nil || member < 1 {
		return 0, 0, fmt.Errorf("invalid fleet member in %q", id)
	}
	return fleet, member, nil
}
func redactText(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "REDACTED")
		}
	}
	return s
}

func (e *Executor) fundEVM(ctx context.Context, a Action) error {
	if !common.IsHexAddress(a.Target) {
		return fmt.Errorf("EVM funding action %s has invalid target %q", a.ID, a.Target)
	}
	usableRao, err := evmFundingTerms(a, e.plan.LiveFacts.ExistentialDepositRao)
	if err != nil {
		return err
	}
	addr := common.HexToAddress(a.Target)
	mirror := ss58Mirror(addr)
	client := e.deployer.client
	_, bal, freeRao, err := e.evmMirrorFundingBalances(ctx, addr, mirror)
	if err != nil {
		return err
	}
	deltaRao, want, err := evmFundingDelta(usableRao, e.plan.LiveFacts.ExistentialDepositRao, freeRao, bal)
	if err != nil {
		return fmt.Errorf("EVM role %s funding precondition: %w", addr, err)
	}
	if deltaRao == 0 {
		return nil
	}
	call, err := e.substrate.FundCall(mirror, deltaRao)
	if err != nil {
		return err
	}
	_, transactionBlock, err := e.substrate.Send(ctx, e.plan.PlanHash, a, call)
	if err != nil {
		return err
	}
	if err := waitForFinalizedEVMBlock(ctx, client, transactionBlock); err != nil {
		return err
	}
	post, err := client.BalanceAt(ctx, addr, new(big.Int).SetUint64(transactionBlock))
	if err != nil {
		return err
	}
	if post.Cmp(want) < 0 {
		return fmt.Errorf("EVM role %s finalized usable balance %s at block %d, want at least %s (existential_deposit_rao=%d)", addr, post, transactionBlock, want, e.plan.LiveFacts.ExistentialDepositRao)
	}
	return nil
}

const evmWeiPerRao = uint64(1_000_000_000)

// evmFundingDelta returns the maximum native transfer needed to leave the
// requested usable balance visible through Ethereum. A newly created mirror
// account needs the runtime existential deposit once; an existing account
// already carries it, including a partially funded account from a retry.
func evmFundingDelta(usableRao, existentialDepositRao, currentFreeRao uint64, currentEVMWei *big.Int) (uint64, *big.Int, error) {
	if usableRao == 0 || existentialDepositRao == 0 {
		return 0, nil, errors.New("usable EVM balance and existential deposit must be nonzero")
	}
	if currentEVMWei == nil || currentEVMWei.Sign() < 0 {
		return 0, nil, errors.New("current EVM balance is unavailable or negative")
	}
	if currentFreeRao > 0 && currentFreeRao < existentialDepositRao {
		return 0, nil, fmt.Errorf("existing mirror free balance %d rao is below existential deposit %d", currentFreeRao, existentialDepositRao)
	}
	maximumNativeWei := new(big.Int).Mul(new(big.Int).SetUint64(currentFreeRao), new(big.Int).SetUint64(evmWeiPerRao))
	if currentEVMWei.Cmp(maximumNativeWei) > 0 {
		return 0, nil, fmt.Errorf("EVM balance %s exceeds mirror free balance %d rao", currentEVMWei, currentFreeRao)
	}
	targetWei := new(big.Int).Mul(new(big.Int).SetUint64(usableRao), new(big.Int).SetUint64(evmWeiPerRao))
	if currentEVMWei.Cmp(targetWei) >= 0 {
		return 0, targetWei, nil
	}
	missingWei := new(big.Int).Sub(targetWei, currentEVMWei)
	missingRao := new(big.Int).Add(missingWei, new(big.Int).SetUint64(evmWeiPerRao-1))
	missingRao.Div(missingRao, new(big.Int).SetUint64(evmWeiPerRao))
	if !missingRao.IsUint64() {
		return 0, nil, fmt.Errorf("EVM funding delta %s rao exceeds uint64", missingRao)
	}
	deltaRao := missingRao.Uint64()
	if currentFreeRao == 0 {
		var ok bool
		deltaRao, ok = checkedAdd(deltaRao, existentialDepositRao)
		if !ok {
			return 0, nil, errors.New("EVM funding delta plus existential deposit overflows uint64")
		}
	}
	maximumTransferRao, ok := checkedAdd(usableRao, existentialDepositRao)
	if !ok || deltaRao > maximumTransferRao {
		return 0, nil, fmt.Errorf("EVM funding delta %d exceeds approved maximum", deltaRao)
	}
	return deltaRao, targetWei, nil
}

func (e *Executor) evmMirrorFundingBalances(ctx context.Context, address common.Address, mirror [32]byte) (uint64, *big.Int, uint64, error) {
	evmHead, err := finalizedEVMHead(ctx, e.deployer.client)
	if err != nil {
		return 0, nil, 0, err
	}
	_, nativeHead, err := e.substrate.finalizedHead()
	if err != nil {
		return 0, nil, 0, err
	}
	block := min(evmHead.Number, nativeHead)
	balance, err := e.deployer.client.BalanceAt(ctx, address, new(big.Int).SetUint64(block))
	if err != nil {
		return 0, nil, 0, err
	}
	freeRao, err := e.substrate.FreeBalanceAtBlock(mirror, block)
	if err != nil {
		return 0, nil, 0, err
	}
	return block, balance, freeRao, nil
}

func waitForFinalizedEVMBlock(ctx context.Context, client *ethclient.Client, target uint64) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		head, err := finalizedEVMHead(ctx, client)
		if err != nil {
			return err
		}
		if head.Number >= target {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for EVM finalized block %d: %w", target, ctx.Err())
		case <-ticker.C:
		}
	}
}
func ss58Mirror(addr common.Address) [32]byte { return ss58.EvmMirrorPubkey(addr) }

// Migrates the active legacy manifest only after its proxy CREATE receipt and
// both observation checkpoints remain canonical. Signed public chronology is
// never rewritten; a later approved publication retains it as a predecessor.
func (self *Executor) reconcileContractDeploymentEventBoundary(ctx context.Context, manifest *ContractDeployment) (bool, error) {
	if self == nil || self.plan == nil || self.journal == nil || self.deployer == nil || self.deployer.client == nil || manifest == nil {
		return false, errors.New("active contract deployment event-boundary reconciliation is unavailable")
	}
	reconciled := *manifest
	changed, err := reconcileContractDeploymentEventBoundary(ctx, ethEVMReceiptFinalityReader{client: self.deployer.client}, &reconciled, self.journal.Entries(), self.plan)
	if err != nil {
		return false, err
	}
	if self.independentEVM != nil {
		if _, err := reconcileContractDeploymentEventBoundary(ctx, ethEVMReceiptFinalityReader{client: self.independentEVM}, &reconciled, self.journal.Entries(), self.plan); err != nil {
			return false, fmt.Errorf("independent coordinator event-boundary reconciliation: %w", err)
		}
	}
	if !changed {
		return false, nil
	}
	if err := saveContractDeployment(self.stateDir, reconciled); err != nil {
		return false, err
	}
	*manifest = reconciled
	return true, nil
}

// Builds or authenticates the deterministic deployment payloads retained by
// one execution run, migrating an active legacy event boundary when needed.
func (e *Executor) ensurePayloads(ctx context.Context) error {
	if e.payloads != nil {
		return nil
	}
	if e.plan != nil && planUsesContractDeploymentEnvelope(e.plan.Schema) {
		planned := contractDeploymentIdentity(e.plan.Deployment)
		p, err := buildDeploymentPayloadsWithRegistrationGeneration(e.cfg, e.roles, planned.InitialNonce, planned.RegistrationRoleGeneration)
		if err != nil {
			return err
		}
		if planUsesCoordinatorUpgradeEnvelope(e.plan.Schema) {
			if err := configureCoordinatorUpgradeNonce(p, e.plan.CoordinatorUpgrade.DeployerNonce); err != nil {
				return fmt.Errorf("build approved coordinator upgrade payload: %w", err)
			}
		}
		if e.plan.CoordinatorUpgradeBaseline.Schema == "urnetwork-coordinator-upgrade-baseline-v4" {
			if err := configurePrecompileProbeNonce(p, e.plan.CoordinatorUpgradeBaseline.ReplacementPrecompileProbeNonce); err != nil {
				return fmt.Errorf("build approved replacement probe payload: %w", err)
			}
		}
		builtHash, err := contractDeploymentIdentityHash(p.Manifest)
		if err != nil {
			return err
		}
		plannedHash, err := contractDeploymentIdentityHash(planned)
		if err != nil {
			return err
		}
		if !e.plan.CoordinatorUpgradeBaseline.isZero() {
			if err := validateCoordinatorUpgradeBaselineRelease(e.plan.CoordinatorUpgradeBaseline, planned, p.Manifest, p.CoordinatorUpgrade); err != nil {
				return fmt.Errorf("approved coordinator upgrade baseline: %w", err)
			}
			if err := validateCoordinatorUpgradePayloadBaseline(e.plan.CoordinatorUpgradeBaseline, planned, p); err != nil {
				return fmt.Errorf("approved coordinator executable baseline: %w", err)
			}
		}
		if builtHash != plannedHash {
			if e.plan.CoordinatorUpgradeBaseline.isZero() {
				if !contractDeploymentUpgradeBaselineCompatible(planned, p.Manifest) {
					return fmt.Errorf("approved contract deployment does not match this release payload: approved=%s built=%s", plannedHash, builtHash)
				}
			}
			builtManifest := p.Manifest
			p.Manifest = planned
			plannedHashes, plannedErr := normalizedDeploymentRuntimeHashes(planned)
			builtHashes, builtErr := normalizedDeploymentRuntimeHashes(builtManifest)
			if plannedErr != nil || builtErr != nil {
				return errors.New("approved coordinator upgrade has invalid runtime hashes")
			}
			for address, plannedRuntimeHash := range plannedHashes {
				if !strings.EqualFold(plannedRuntimeHash, builtHashes[address]) {
					p.ExpectedRuntime[address] = nil
				}
			}
		}
		if e.plan.CoordinatorUpgrade != p.CoordinatorUpgrade {
			return errors.New("approved coordinator upgrade does not match this release payload")
		}
		if existing, loadErr := loadContractDeployment(e.stateDir); loadErr == nil {
			activeCompatible := contractDeploymentAddressesEqual(*existing, planned) && contractDeploymentRuntimeHashesCompatible(*existing, planned)
			if !activeCompatible && existing.RegistrationRoleGeneration != planned.RegistrationRoleGeneration {
				head, headErr := finalizedEVMHead(ctx, e.deployer.client)
				if headErr != nil {
					return headErr
				}
				nonce, nonceErr := e.deployer.client.NonceAt(ctx, p.Deployer, new(big.Int).SetUint64(head.Number))
				if nonceErr != nil {
					return nonceErr
				}
				activeCompatible = validateRegistrationRoleGenerationPromotion(e.cfg, e.plan, *existing, planned, nonce, e.journal.Entries()) == nil
			}
			if activeCompatible {
				if _, err := e.reconcileContractDeploymentEventBoundary(ctx, existing); err != nil {
					return fmt.Errorf("active contract deployment event boundary: %w", err)
				}
				p.Manifest.DeployBlock = existing.DeployBlock
				p.Manifest.DeployBlockHash = existing.DeployBlockHash
				p.Manifest.CoordinatorEventStartBlock = existing.CoordinatorEventStartBlock
				p.Manifest.CoordinatorEventStartBlockHash = existing.CoordinatorEventStartBlockHash
				p.Manifest.RuntimeHashes = planned.RuntimeHashes
				e.payloads = p
				return nil
			}
			existingHash, hashErr := canonicalHashHex(*existing)
			if hashErr != nil {
				return hashErr
			}
			approvedSuperseded := false
			for _, superseded := range e.plan.SupersededDeployments {
				supersededHash, supersededErr := canonicalHashHex(superseded)
				if supersededErr != nil {
					return supersededErr
				}
				if supersededHash == existingHash {
					approvedSuperseded = true
					break
				}
			}
			if !approvedSuperseded {
				return fmt.Errorf("existing contract deployment %s is neither active nor approved for supersession", existingHash)
			}
			archivePath := filepath.Join(e.stateDir, "public", "deployments", stringsTrim0x(existingHash)+".json")
			archive, marshalErr := json.MarshalIndent(existing, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			if err := atomicWrite(archivePath, append(archive, '\n'), 0o644); err != nil {
				return fmt.Errorf("archive superseded contract deployment: %w", err)
			}
		} else if !errors.Is(loadErr, os.ErrNotExist) {
			return loadErr
		}
		if _, err := e.reconcileContractDeploymentEventBoundary(ctx, &p.Manifest); err != nil {
			return fmt.Errorf("new contract deployment event boundary: %w", err)
		}
		if err := saveContractDeployment(e.stateDir, p.Manifest); err != nil {
			return err
		}
		e.payloads = p
		return nil
	}
	if existing, err := loadContractDeployment(e.stateDir); err == nil {
		p, err := buildDeploymentPayloadsWithRegistrationGeneration(e.cfg, e.roles, existing.InitialNonce, existing.RegistrationRoleGeneration)
		if err != nil {
			return err
		}
		if p.Manifest.ReserveSink != existing.ReserveSink || p.Manifest.CoordinatorProxy != existing.CoordinatorProxy || p.Manifest.GovernanceDrillImplementation != existing.GovernanceDrillImplementation || p.Manifest.PrecompileProbe != existing.PrecompileProbe {
			return fmt.Errorf("contract deployment manifest/address mismatch")
		}
		if _, err := e.reconcileContractDeploymentEventBoundary(ctx, existing); err != nil {
			return fmt.Errorf("active contract deployment event boundary: %w", err)
		}
		p.Manifest.DeployBlock = existing.DeployBlock
		p.Manifest.DeployBlockHash = existing.DeployBlockHash
		p.Manifest.CoordinatorEventStartBlock = existing.CoordinatorEventStartBlock
		p.Manifest.CoordinatorEventStartBlockHash = existing.CoordinatorEventStartBlockHash
		p.Manifest.RuntimeHashes = existing.RuntimeHashes
		e.payloads = p
		return nil
	}
	nonce, err := e.deployer.PendingNonce(ctx)
	if err != nil {
		return err
	}
	p, err := buildDeploymentPayloads(e.cfg, e.roles, nonce)
	if err != nil {
		return err
	}
	if _, err := e.reconcileContractDeploymentEventBoundary(ctx, &p.Manifest); err != nil {
		return fmt.Errorf("new contract deployment event boundary: %w", err)
	}
	if err := saveContractDeployment(e.stateDir, p.Manifest); err != nil {
		return err
	}
	e.payloads = p
	return nil
}
func (e *Executor) executeDeployment(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	p := e.payloads
	var addr common.Address
	var to *common.Address
	var data []byte
	value := big.NewInt(0)
	switch a.ID {
	case "evm.reserve-sink":
		addr = p.Manifest.ReserveSink
		data = p.Reserve
	case "evm.settlement-vault":
		addr = p.Manifest.SettlementVault
		data = p.Vault
	case "evm.coordinator-implementation":
		addr = p.Manifest.CoordinatorImplementation
		data = p.Implementation
	case "evm.vault-register-escrow":
		_, limit, err := e.boundedRegistrationBurn(a)
		if err != nil {
			return err
		}
		if err := e.verifyContractRegistrationReplacementPrecondition(a, escrowHotkeyLabelForGeneration(p.Manifest.RegistrationRoleGeneration), 1); err != nil {
			return fmt.Errorf("vault escrow replacement precondition: %w", err)
		}
		addr = p.Manifest.SettlementVault
		to = &addr
		data = p.RegisterEscrow
		value = registrationFundingWei(limit)
	case "evm.coordinator-proxy":
		addr = p.Manifest.CoordinatorProxy
		data = p.Proxy
	case "evm.governance-drill-implementation":
		addr = p.Manifest.GovernanceDrillImplementation
		data = p.GovernanceDrill
	case "precompile.probe-deploy":
		addr = p.PrecompileProbeAddress
		data = p.PrecompileProbe
	case "evm.coordinator-upgrade-implementation":
		addr = p.CoordinatorUpgrade.Implementation
		data = p.UpgradeImplementation
	case "fleet.refresh.deploy-batcher":
		addr = p.FleetBatcherAddress
		data = p.FleetBatcher
	case "evm.vault-fix-coordinator":
		addr = p.Manifest.SettlementVault
		to = &addr
		data = p.FixVault
	case "evm.sink-fix-recorder":
		addr = p.Manifest.ReserveSink
		to = &addr
		data = p.FixSink
	}
	replacementProbe := a.ID == "precompile.probe-deploy" && p.PrecompileProbeAddress != p.Manifest.PrecompileProbe
	if to == nil {
		head, headErr := finalizedEVMHead(ctx, e.deployer.client)
		if headErr != nil {
			return headErr
		}
		code, err := e.deployer.client.CodeAt(ctx, addr, new(big.Int).SetUint64(head.Number))
		if err != nil {
			return fmt.Errorf("observe existing code at %s: %w", addr, err)
		}
		if len(code) > 0 {
			expected := p.ExpectedRuntime[addr]
			if a.ID == "fleet.refresh.deploy-batcher" {
				expected = p.FleetBatcherRuntime
			}
			if len(expected) > 0 && string(code) != string(expected) {
				return fmt.Errorf("unexpected existing code at %s", addr)
			}
			if len(expected) == 0 {
				want := p.Manifest.RuntimeHashes[addr.Hex()]
				if want == "" || !strings.EqualFold(crypto.Keccak256Hash(code).Hex(), want) {
					return fmt.Errorf("unexpected existing runtime hash at %s", addr)
				}
			}
			receipt, err := finalizedContractCreationReceipt(ctx, ethEVMReceiptFinalityReader{client: e.deployer.client}, head, p.Manifest.DeploymentID, a, addr, e.journal.Entries(), e.plan.allowedPlanHashes())
			if err != nil {
				return err
			}
			if receipt == nil {
				return fmt.Errorf("existing code at %s has no matching finalized CREATE receipt", addr)
			}
			if e.independentEVM != nil {
				independentHead, err := finalizedEVMHead(ctx, e.independentEVM)
				if err != nil {
					return err
				}
				if _, err := finalizedContractCreationReceipt(ctx, ethEVMReceiptFinalityReader{client: e.independentEVM}, independentHead, p.Manifest.DeploymentID, a, addr, e.journal.Entries(), e.plan.allowedPlanHashes()); err != nil {
					return fmt.Errorf("independent CREATE receipt: %w", err)
				}
			}
			if err := recordContractDeploymentReceipt(&p.Manifest, a.ID, receipt, replacementProbe); err != nil {
				return err
			}
			return saveContractDeployment(e.stateDir, p.Manifest)
		}
	}
	receipt, err := e.deployer.Send(ctx, e.plan.PlanHash, a, to, value, data)
	if err != nil {
		return err
	}
	if receipt == nil {
		return errors.New("deployment transaction returned no receipt")
	}
	if to == nil && receipt.ContractAddress != addr {
		return fmt.Errorf("deployed %s, predicted %s", receipt.ContractAddress, addr)
	}
	if err := recordContractDeploymentReceipt(&p.Manifest, a.ID, receipt, replacementProbe); err != nil {
		return err
	}
	if a.ID == "evm.governance-drill-implementation" {
		deployed := make(map[common.Address][]byte, len(p.ExpectedRuntime)-2)
		for address, runtime := range p.ExpectedRuntime {
			if address != p.PrecompileProbeAddress && address != p.CoordinatorUpgrade.Implementation && len(runtime) > 0 {
				deployed[address] = runtime
			}
		}
		hashes, err := verifyRuntimeCode(ctx, e.deployer.client, deployed)
		if err != nil {
			return err
		}
		for address, runtime := range p.ExpectedRuntime {
			if len(runtime) == 0 && address != p.CoordinatorUpgrade.Implementation {
				hashes[address.Hex()] = e.plan.Deployment.RuntimeHashes[address.Hex()]
			}
		}
		p.Manifest.RuntimeHashes = hashes
	}
	if a.ID == "precompile.probe-deploy" {
		if replacementProbe {
			expected := p.ExpectedRuntime[p.PrecompileProbeAddress]
			if len(expected) == 0 {
				return errors.New("replacement precompile probe runtime is unavailable")
			}
			if _, err := verifyRuntimeCode(ctx, e.deployer.client, map[common.Address][]byte{p.PrecompileProbeAddress: expected}); err != nil {
				return err
			}
			// The disposable replacement is authenticated by the v4 upgrade
			// baseline. The persisted deployment remains the immutable six-
			// contract custody identity used by prior value-bearing actions.
			return saveContractDeployment(e.stateDir, p.Manifest)
		}
		baseRuntime := make(map[common.Address][]byte, len(p.ExpectedRuntime)-1)
		for address, runtime := range p.ExpectedRuntime {
			if address != p.CoordinatorUpgrade.Implementation && len(runtime) > 0 {
				baseRuntime[address] = runtime
			}
		}
		hashes, err := verifyRuntimeCode(ctx, e.deployer.client, baseRuntime)
		if err != nil {
			return err
		}
		for address, runtime := range p.ExpectedRuntime {
			if len(runtime) == 0 && address != p.CoordinatorUpgrade.Implementation {
				hashes[address.Hex()] = e.plan.Deployment.RuntimeHashes[address.Hex()]
			}
		}
		p.Manifest.RuntimeHashes = hashes
	}
	return saveContractDeployment(e.stateDir, p.Manifest)
}

func (e *Executor) registerOperator(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	n := suffixInt(a.ID)
	coordABI, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	countOut, err := contractCall(ctx, e.owner.client, e.payloads.Manifest.CoordinatorProxy, coordABI, "operatorVersionCount", big.NewInt(int64(n)))
	if err == nil && countOut[0].(*big.Int).Sign() > 0 {
		return nil
	}
	if err := e.verifyContractRegistrationReplacementPrecondition(a, operatorPoolHotkeyLabelForGeneration(n, e.payloads.Manifest.RegistrationRoleGeneration), n+1); err != nil {
		return fmt.Errorf("operator %d pool replacement precondition: %w", n, err)
	}
	window, err := waitFutureEpochTransactionWindow(ctx, e.owner, e.payloads.Manifest.CoordinatorProxy, stabi.NewSTCoordinator())
	if err != nil {
		return err
	}
	epoch := window.CurrentEpoch
	cold, err := roleBytes32(e.roles, fmt.Sprintf("operator-%d-coldkey", n))
	if err != nil {
		return err
	}
	pool, err := roleBytes32(e.roles, operatorPoolHotkeyLabelForGeneration(n, e.payloads.Manifest.RegistrationRoleGeneration))
	if err != nil {
		return err
	}
	deposit, err := roleBytes32(e.roles, fmt.Sprintf("operator-%d-deposit-hotkey", n))
	if err != nil {
		return err
	}
	depositSigner, _ := e.roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", n))
	rootSigner, _ := e.roles.EVMAddress(fmt.Sprintf("operator-%d-root", n))
	_, limit, err := e.boundedRegistrationBurn(a)
	if err != nil {
		return err
	}
	data, err := coordABI.Pack("registerOperator", big.NewInt(int64(n)), cold, pool, deposit, depositSigner, rootSigner, epoch, limit)
	if err != nil {
		return err
	}
	value := registrationFundingWei(limit)
	addr := e.payloads.Manifest.CoordinatorProxy
	_, err = e.owner.Send(ctx, e.plan.PlanHash, a, &addr, value, data)
	return err
}

type VoluntaryConvictionEvidence struct {
	Schema              string `json:"schema"`
	DeploymentID        string `json:"deployment_id"`
	NoID                uint64 `json:"no_id"`
	Epoch               uint64 `json:"epoch"`
	AmountRao           string `json:"amount_rao"`
	BeforeConvictionRao string `json:"before_conviction_rao"`
	AfterConvictionRao  string `json:"after_conviction_rao"`
	Nonce               string `json:"nonce"`
	Funder              string `json:"funder"`
	PolicyHash          string `json:"policy_hash"`
	TransactionHash     string `json:"transaction_hash"`
	FinalizedBlock      uint64 `json:"finalized_block"`
	FinalizedHash       string `json:"finalized_block_hash"`
}

func (e *Executor) addVoluntaryConviction(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	manager := e.deposits[1]
	if manager == nil {
		return errors.New("operator 1 deposit transaction manager is missing")
	}
	amount := new(big.Int).SetUint64(e.cfg.Config.Scenarios.VoluntaryConvictionRao)
	if amount.Sign() == 0 {
		return errors.New("voluntary conviction amount is zero")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	address := e.payloads.Manifest.CoordinatorProxy
	head, err := finalizedEVMHead(ctx, manager.client)
	if err != nil {
		return err
	}
	epochValues, err := contractCallAt(ctx, manager.client, address, parsed, "currentEpoch", head.Number)
	if err != nil || len(epochValues) != 1 {
		return stateMismatchError(err, "read current epoch at finalized head returned %d values", len(epochValues))
	}
	epoch, ok := epochValues[0].(*big.Int)
	if !ok || !epoch.IsUint64() {
		return fmt.Errorf("currentEpoch returned %T", epochValues[0])
	}
	readAdded := func(block uint64, atEpoch *big.Int) (*big.Int, error) {
		values, readErr := contractCallAt(ctx, manager.client, address, parsed, "epochConvictionAdded", block, atEpoch, big.NewInt(1))
		if readErr != nil {
			return nil, readErr
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("epochConvictionAdded returned %d values", len(values))
		}
		value, valueOK := values[0].(*big.Int)
		if !valueOK {
			return nil, fmt.Errorf("epochConvictionAdded returned %T", values[0])
		}
		return value, nil
	}
	readConviction := func(block uint64) (*big.Int, error) {
		values, readErr := contractCallAt(ctx, manager.client, address, parsed, "cumulativeConviction", block, big.NewInt(1))
		if readErr != nil || len(values) != 1 {
			return nil, stateMismatchError(readErr, "read cumulative conviction at block %d returned %d values", block, len(values))
		}
		value, valueOK := values[0].(*big.Int)
		if !valueOK {
			return nil, fmt.Errorf("cumulativeConviction returned %T", values[0])
		}
		return value, nil
	}
	prior, hasPrior := e.journal.LatestTransaction(e.plan.PlanHash, a.ID, a.IntentHash)
	cumulative, err := readConviction(head.Number)
	if err != nil {
		return err
	}
	if err := validateVoluntaryConvictionPrestate(cumulative, hasPrior); err != nil {
		return err
	}
	if added, readErr := readAdded(head.Number, epoch); readErr != nil {
		return readErr
	} else if added.Sign() != 0 && !hasPrior {
		return fmt.Errorf("epoch %s unexpectedly already has %s voluntary conviction for no 1", epoch, added)
	}
	nonceValues, err := contractCallAt(ctx, manager.client, address, parsed, "nextDepositNonce", head.Number, big.NewInt(1))
	if err != nil || len(nonceValues) != 1 {
		return stateMismatchError(err, "read voluntary conviction nonce returned %d values", len(nonceValues))
	}
	nonce, ok := nonceValues[0].(*big.Int)
	if !ok || nonce.Sign() < 0 {
		return fmt.Errorf("nextDepositNonce returned %T", nonceValues[0])
	}
	if hasPrior {
		priorNonce, parseErr := new(big.Int).SetString(prior.Nonce, 10)
		if !parseErr || priorNonce.Sign() < 0 {
			return fmt.Errorf("persisted voluntary conviction nonce %q is invalid", prior.Nonce)
		}
		nonce = priorNonce
	}
	if head.Number > ^uint64(0)-128 {
		return errors.New("voluntary conviction deadline overflows uint64")
	}
	data, err := parsed.Pack("addConviction", big.NewInt(1), amount, nonce, head.Number+128)
	if err != nil {
		return err
	}
	receipt, err := manager.Send(ctx, e.plan.PlanHash, a, &address, big.NewInt(0), data)
	if err != nil {
		return err
	}
	event := parsed.Events["ConvictionAdded"]
	var eventEpoch, eventNonce *big.Int
	var eventAmount *big.Int
	var eventFunder common.Address
	var eventPolicy [32]byte
	for _, log := range receipt.Logs {
		if log.Address != address || len(log.Topics) != 4 || log.Topics[0] != event.ID {
			continue
		}
		values, unpackErr := event.Inputs.NonIndexed().Unpack(log.Data)
		if unpackErr != nil || len(values) != 3 {
			return stateMismatchError(unpackErr, "decode ConvictionAdded event returned %d values", len(values))
		}
		loggedAmount, amountOK := values[0].(*big.Int)
		loggedPolicy, policyOK := values[1].([32]byte)
		loggedNonce, nonceOK := values[2].(*big.Int)
		if !amountOK || !policyOK || !nonceOK {
			return fmt.Errorf("ConvictionAdded event has unexpected ABI values")
		}
		if log.Topics[1].Big().Cmp(big.NewInt(1)) != 0 {
			continue
		}
		eventEpoch, eventAmount, eventPolicy, eventNonce = log.Topics[2].Big(), loggedAmount, loggedPolicy, loggedNonce
		eventFunder = common.BytesToAddress(log.Topics[3].Bytes()[12:])
		break
	}
	if eventEpoch == nil || !eventEpoch.IsUint64() || eventAmount.Cmp(amount) != 0 || eventNonce.Cmp(nonce) != 0 {
		return errors.New("finalized voluntary conviction receipt lacks the exact ConvictionAdded event")
	}
	expectedFunder := common.HexToAddress(e.plan.Roles.OperatorDepositSigners[0])
	policyHash, err := decodeHash(e.cfg.PolicyHash)
	if err != nil {
		return err
	}
	if eventFunder != expectedFunder || eventPolicy != policyHash {
		return errors.New("voluntary conviction event identity or policy hash mismatch")
	}
	if receipt.BlockNumber.Sign() == 0 {
		return errors.New("voluntary conviction cannot be verified before block zero")
	}
	before, err := readConviction(receipt.BlockNumber.Uint64() - 1)
	if err != nil {
		return err
	}
	after, err := readConviction(receipt.BlockNumber.Uint64())
	if err != nil {
		return err
	}
	if before.Sign() != 0 || after.Cmp(amount) != 0 {
		return fmt.Errorf("voluntary conviction transition is %s -> %s, want 0 -> %s", before, after, amount)
	}
	postHead, err := finalizedEVMHead(ctx, manager.client)
	if err != nil {
		return err
	}
	added, err := readAdded(postHead.Number, eventEpoch)
	if err != nil {
		return err
	}
	if added.Cmp(amount) != 0 {
		return fmt.Errorf("finalized voluntary conviction is %s, want exactly %s", added, amount)
	}
	evidence := VoluntaryConvictionEvidence{
		Schema: "urnetwork-voluntary-conviction-evidence-v1", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
		NoID: 1, Epoch: eventEpoch.Uint64(), AmountRao: eventAmount.String(), BeforeConvictionRao: before.String(), AfterConvictionRao: after.String(), Nonce: eventNonce.String(),
		Funder: eventFunder.Hex(), PolicyHash: "0x" + hex.EncodeToString(eventPolicy[:]),
		TransactionHash: receipt.TxHash.Hex(), FinalizedBlock: receipt.BlockNumber.Uint64(), FinalizedHash: receipt.BlockHash.Hex(),
	}
	return writePublicJSON(filepath.Join(e.stateDir, "public", "voluntary-conviction.json"), evidence)
}
func (e *Executor) readBurn() (uint64, error) {
	key, err := types.CreateStorageKey(e.substrate.chain.Meta, "SubtensorModule", "Burn", netuidArg(e.cfg.Netuid))
	if err != nil {
		return 0, err
	}
	var v types.U64
	finalized, _, err := e.substrate.finalizedHead()
	if err != nil {
		return 0, err
	}
	err = readRequiredStorageAt(e.substrate.chain, key, crv4.PalletName, "Burn", &v, finalized)
	return uint64(v), err
}
func contractCall(ctx context.Context, c *ethclient.Client, address common.Address, a abi.ABI, method string, args ...any) ([]any, error) {
	head, err := finalizedEVMHead(ctx, c)
	if err != nil {
		return nil, err
	}
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	out, err := c.CallContract(ctx, ethereum.CallMsg{To: &address, Data: data}, new(big.Int).SetUint64(head.Number))
	if err != nil {
		return nil, err
	}
	return a.Unpack(method, out)
}

func (e *Executor) fundSubstrateRole(ctx context.Context, a Action, label string) error {
	dest, err := roleBytes32(e.roles, label)
	if err != nil {
		return err
	}
	current, err := e.substrate.FreeBalance(dest)
	if err != nil {
		return err
	}
	if current >= a.Spend.TAORao {
		return nil
	}
	call, err := e.substrate.FundCall(dest, a.Spend.TAORao-current)
	if err != nil {
		return err
	}
	if _, _, err = e.substrate.Send(ctx, e.plan.PlanHash, a, call); err != nil {
		return err
	}
	post, err := e.substrate.FreeBalance(dest)
	if err != nil {
		return err
	}
	if post < a.Spend.TAORao {
		return fmt.Errorf("substrate role %s finalized balance %d, want at least %d", label, post, a.Spend.TAORao)
	}
	return nil
}

const neuronSetupABI = `[{"type":"function","name":"registerLimit","inputs":[{"name":"netuid","type":"uint16"},{"name":"hotkey","type":"bytes32"},{"name":"limitPrice","type":"uint64"}],"outputs":[],"stateMutability":"payable"},{"type":"function","name":"getUid","inputs":[{"name":"netuid","type":"uint16"},{"name":"hotkey","type":"bytes32"}],"outputs":[{"name":"exists","type":"bool"},{"name":"uid","type":"uint16"}],"stateMutability":"view"}]`

var neuronPrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000000804")

// Subtensor's neuron precompile burns from the caller's funded SS58 mirror;
// native value sent to the precompile is not the burn payment.
func buildNeuronRegistrationTransaction(parsed abi.ABI, netuid uint16, hotkey [32]byte, limit uint64) ([]byte, *big.Int, error) {
	data, err := parsed.Pack("registerLimit", netuid, hotkey, limit)
	return data, new(big.Int), err
}

func (e *Executor) registerDepositHotkey(ctx context.Context, a Action, operator int) error {
	manager := e.deposits[operator]
	if manager == nil {
		return fmt.Errorf("operator %d deposit transaction manager is missing", operator)
	}
	hotkey, err := roleBytes32(e.roles, fmt.Sprintf("operator-%d-deposit-hotkey", operator))
	if err != nil {
		return err
	}
	depositSigner, err := e.roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", operator))
	if err != nil {
		return err
	}
	expectedColdkey := ss58Mirror(depositSigner)
	parsed, err := abi.JSON(strings.NewReader(neuronSetupABI))
	if err != nil {
		return err
	}
	readUID := func() (bool, uint16, error) {
		values, readErr := contractCall(ctx, manager.client, neuronPrecompileAddress, parsed, "getUid", e.cfg.Netuid, hotkey)
		if readErr != nil {
			return false, 0, readErr
		}
		if len(values) != 2 {
			return false, 0, fmt.Errorf("getUid returned %d values", len(values))
		}
		exists, ok := values[0].(bool)
		if !ok {
			return false, 0, fmt.Errorf("getUid exists has type %T", values[0])
		}
		uid, ok := values[1].(uint16)
		if !ok {
			return false, 0, fmt.Errorf("getUid uid has type %T", values[1])
		}
		return exists, uid, nil
	}
	if exists, _, readErr := readUID(); readErr == nil && exists {
		owner, ownerErr := e.substrate.HotkeyOwner(hotkey)
		if ownerErr != nil {
			return ownerErr
		}
		return validateHotkeyOwner("operator deposit hotkey", owner, expectedColdkey)
	}
	_, limit, err := e.boundedRegistrationBurn(a)
	if err != nil {
		return err
	}
	data, value, err := buildNeuronRegistrationTransaction(parsed, e.cfg.Netuid, hotkey, limit)
	if err != nil {
		return err
	}
	// The runtime deducts the burn from the EVM caller's SS58 mirror. The role
	// was funded up to the approved ceiling; msg.value must remain zero or it
	// would be transferred to the precompile before the runtime dispatch.
	if _, err := manager.Send(ctx, e.plan.PlanHash, a, &neuronPrecompileAddress, value, data); err != nil {
		return err
	}
	exists, _, err := readUID()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("deposit hotkey was not registered after finalized transaction")
	}
	owner, err := e.substrate.HotkeyOwner(hotkey)
	if err != nil {
		return err
	}
	return validateHotkeyOwner("operator deposit hotkey", owner, expectedColdkey)
}

func (e *Executor) readStakeFinalized(ctx context.Context, hotkey, coldkey [32]byte) (uint64, error) {
	head, err := finalizedEVMHead(ctx, e.deployer.client)
	if err != nil {
		return 0, err
	}
	return e.readStakeAt(ctx, head.Number, hotkey, coldkey)
}

// Read one position from the staking precompile at an exact chain block so a
// source-capacity decision cannot mix finalized checkpoints.
func (e *Executor) readStakeAt(ctx context.Context, block uint64, hotkey, coldkey [32]byte) (uint64, error) {
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		return 0, err
	}
	values, err := contractCallAt(ctx, e.deployer.client, stakingPrecompileAddress, parsed, "getStake", block, hotkey, coldkey, new(big.Int).SetUint64(uint64(e.cfg.Netuid)))
	if err != nil {
		return 0, err
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("getStake returned %d values", len(values))
	}
	stake, ok := values[0].(*big.Int)
	if !ok || !stake.IsUint64() {
		return 0, fmt.Errorf("getStake returned %T or an oversized value", values[0])
	}
	return stake.Uint64(), nil
}

type liveAlphaTransferEconomics struct {
	Snapshot                    RegisteredAlphaSnapshot
	DefaultMinTransferRao       uint64
	AlphaPriceQ9                uint64
	SourcePositionRao           uint64
	SourceColdkeyTotalRao       uint64
	SourceStoredLockRao         uint64
	SourcePositionCollateralRao uint64
	SourceColdkeyCollateralRao  uint64
	SourceTransferableRao       uint64
}

func (e *Executor) readLiveAlphaTransferEconomics(ctx context.Context) (liveAlphaTransferEconomics, error) {
	var result liveAlphaTransferEconomics
	snapshot, err := e.substrate.RegisteredAlphaSnapshot()
	if err != nil {
		return result, fmt.Errorf("read registered alpha snapshot: %w", err)
	}
	finalizedHash, err := types.NewHashFromHexString(snapshot.FinalizedHash)
	if err != nil {
		return result, fmt.Errorf("decode alpha-transfer snapshot hash: %w", err)
	}
	result.DefaultMinTransferRao, err = readRuntimeDefaultMinTransferAt(
		e.substrate.chain, e.cfg, finalizedHash,
	)
	if err != nil {
		return result, fmt.Errorf("read runtime DefaultMinTransfer: %w", err)
	}
	parsed, err := abi.JSON(strings.NewReader(alphaPricePrecompileABI))
	if err != nil {
		return result, err
	}
	data, err := parsed.Pack("getAlphaPrice", e.cfg.Netuid)
	if err != nil {
		return result, err
	}
	raw, err := e.deployer.client.CallContract(ctx, ethereum.CallMsg{To: &alphaPricePrecompileAddress, Data: data}, new(big.Int).SetUint64(snapshot.FinalizedBlock))
	if err != nil {
		return result, fmt.Errorf("read live alpha price at finalized block %d: %w", snapshot.FinalizedBlock, err)
	}
	values, err := parsed.Unpack("getAlphaPrice", raw)
	if err != nil || len(values) != 1 {
		return result, stateMismatchError(err, "decode live alpha price returned %d values", len(values))
	}
	price, ok := values[0].(*big.Int)
	if !ok {
		return result, fmt.Errorf("live alpha price returned %T", values[0])
	}
	result.AlphaPriceQ9, err = decodeAlphaPriceQ9(price)
	if err != nil {
		return result, err
	}
	source, err := decodeHex32("approved alpha source hotkey", e.plan.LiveFacts.AlphaSourceHotkey)
	if err != nil {
		return result, err
	}
	wallet, err := ss58.DecodeWithPrefix(e.cfg.WalletPublic, ss58.BittensorPrefix)
	if err != nil {
		return result, fmt.Errorf("decode alpha source coldkey: %w", err)
	}
	var coldkey [32]byte
	copy(coldkey[:], wallet[:])
	restrictions, err := e.substrate.AlphaTransferSourceRestrictions(snapshot, coldkey, source)
	if err != nil {
		return result, fmt.Errorf("read live alpha source restrictions: %w", err)
	}
	for _, hotkey := range restrictions.StakingHotkeys {
		stake, stakeErr := e.readStakeAt(ctx, snapshot.FinalizedBlock, hotkey, coldkey)
		if stakeErr != nil {
			return result, fmt.Errorf("read live alpha source position 0x%x: %w", hotkey, stakeErr)
		}
		var addOK bool
		result.SourceColdkeyTotalRao, addOK = checkedAdd(result.SourceColdkeyTotalRao, stake)
		if !addOK {
			return result, errors.New("live alpha source coldkey total exceeds uint64")
		}
		if hotkey == source {
			result.SourcePositionRao = stake
		}
	}
	if result.SourcePositionRao == 0 {
		return result, errors.New("approved alpha source position is empty")
	}
	result.SourceStoredLockRao = restrictions.StoredLockRao
	result.SourcePositionCollateralRao = restrictions.PositionCollateralRao
	result.SourceColdkeyCollateralRao = restrictions.ColdkeyCollateralRao
	result.SourceTransferableRao, err = alphaTransferCapacity(
		result.SourcePositionRao,
		result.SourceColdkeyTotalRao,
		result.SourceStoredLockRao,
		result.SourcePositionCollateralRao,
		result.SourceColdkeyCollateralRao,
	)
	if err != nil {
		return result, fmt.Errorf("derive live alpha source capacity: %w", err)
	}
	result.Snapshot = snapshot
	return result, nil
}

func (e *Executor) validateAlphaTransferPrebroadcast(ctx context.Context, a Action, destinationHotkey [32]byte, reserve bool) (liveAlphaTransferEconomics, error) {
	var empty liveAlphaTransferEconomics
	live, err := e.readLiveAlphaTransferEconomics(ctx)
	if err != nil {
		return empty, err
	}
	source, err := decodeHex32("approved alpha source hotkey", e.plan.LiveFacts.AlphaSourceHotkey)
	if err != nil {
		return empty, err
	}
	requiredShareBPS := e.cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS
	targetShareBPS, minimumShareBPS, reserveShareRepair, termsErr := reserveShareRepairTerms(a)
	if termsErr != nil {
		return empty, termsErr
	}
	if reserveShareRepair {
		maximumTranche, trancheErr := strconv.ParseUint(a.Parameters[alphaRepairMaximumTrancheParameter], 10, 64)
		cumulativeLimit, limitErr := strconv.ParseUint(a.Parameters[alphaRepairCumulativeLimitParameter], 10, 64)
		if !reserve || trancheErr != nil || limitErr != nil || maximumTranche != e.cfg.Config.ValidatorBootstrap.MaximumReserveRepairAlphaRao || a.Spend.AlphaRao > maximumTranche || cumulativeLimit > e.cfg.MaximumAlphaRao || targetShareBPS != e.cfg.Config.ValidatorBootstrap.ReserveTargetShareBPS || minimumShareBPS != requiredShareBPS {
			return empty, fmt.Errorf("alpha repair %s reserve-share bounds differ from the active configuration", a.ID)
		}
		requiredShareBPS = targetShareBPS
	}
	if err := validateAlphaTransferAtSnapshot(a, source, destinationHotkey, live, e.plan.AlphaTransferMarginBPS, requiredShareBPS, e.plan.MinimumSourceRemainingRao, reserve); err != nil {
		return empty, err
	}
	return live, nil
}

func validateAlphaTransferAtSnapshot(a Action, source, destination [32]byte, live liveAlphaTransferEconomics, marginBPS, reserveMinimumShareBPS uint16, minimumSourceRemainingRao uint64, reserve bool) error {
	exact, err := strconv.ParseUint(a.Parameters["exact_amount_rao"], 10, 64)
	if err != nil || exact == 0 || exact != a.Spend.AlphaRao {
		return fmt.Errorf("alpha transfer %s has an invalid exact amount", a.ID)
	}
	approvedDefaultMinTransfer, err := strconv.ParseUint(a.Parameters["runtime_default_min_transfer_tao_rao"], 10, 64)
	if err != nil || approvedDefaultMinTransfer == 0 {
		return fmt.Errorf("alpha transfer %s has an invalid approved runtime DefaultMinTransfer", a.ID)
	}
	if live.DefaultMinTransferRao != approvedDefaultMinTransfer {
		return fmt.Errorf("alpha transfer %s stopped before signing: runtime DefaultMinTransfer changed from approved %d to %d TAO rao", a.ID, approvedDefaultMinTransfer, live.DefaultMinTransferRao)
	}
	minimum, err := minimumAlphaTransferRao(live.DefaultMinTransferRao, live.AlphaPriceQ9, marginBPS)
	if err != nil {
		return err
	}
	if exact < minimum {
		equivalent, _ := alphaTransferTAOEquivalentRao(exact, live.AlphaPriceQ9)
		return fmt.Errorf("alpha transfer %s stopped before signing: exact amount %d has %d TAO rao equivalent at price %d, below DefaultMinTransfer %d plus %d bps margin (minimum alpha %d)", a.ID, exact, equivalent, live.AlphaPriceQ9, live.DefaultMinTransferRao, marginBPS, minimum)
	}
	if exact > live.SourceTransferableRao {
		return fmt.Errorf("alpha transfer %s stopped before signing: exact amount %d exceeds source transferable alpha %d (position=%d coldkey_total=%d stored_lock=%d position_collateral=%d coldkey_collateral=%d)", a.ID, exact, live.SourceTransferableRao, live.SourcePositionRao, live.SourceColdkeyTotalRao, live.SourceStoredLockRao, live.SourcePositionCollateralRao, live.SourceColdkeyCollateralRao)
	}
	if minimumSourceRemainingRao == 0 || exact > live.SourcePositionRao || live.SourcePositionRao-exact < minimumSourceRemainingRao {
		return fmt.Errorf("alpha transfer %s stopped before signing: source position %d minus exact amount %d would violate minimum remainder %d", a.ID, live.SourcePositionRao, exact, minimumSourceRemainingRao)
	}
	if _, ok := live.Snapshot.ByHotkey[source]; !ok {
		return errors.New("approved alpha source is no longer a registered hotkey")
	}
	destinationStake, ok := live.Snapshot.ByHotkey[destination]
	if !ok {
		return fmt.Errorf("alpha transfer destination 0x%x is not registered", destination)
	}
	if reserve {
		shortfall, shortfallErr := alphaTransferRoundingShortfall(a)
		if shortfallErr != nil || exact <= shortfall {
			return stateMismatchError(shortfallErr, "reserve transfer %s has no bounded minimum credit", a.ID)
		}
		finalStake, addOK := checkedAdd(destinationStake, exact-shortfall)
		if !addOK || !alphaShareMeets(live.Snapshot.TotalAlphaRao, finalStake, reserveMinimumShareBPS) {
			return fmt.Errorf("reserve transfer stopped before signing: planned minimum finalized stake %d+(%d-%d) does not retain %d bps of registered alpha %d", destinationStake, exact, shortfall, reserveMinimumShareBPS, live.Snapshot.TotalAlphaRao)
		}
	}
	return nil
}

func (e *Executor) reserveValidatorShareSnapshot(requiredShareBPS uint16) (RegisteredAlphaSnapshot, uint64, error) {
	if requiredShareBPS == 0 {
		return RegisteredAlphaSnapshot{}, 0, errors.New("reserve-validator share requirement is zero")
	}
	snapshot, err := e.substrate.RegisteredAlphaSnapshot()
	if err != nil {
		return RegisteredAlphaSnapshot{}, 0, err
	}
	reserve, err := roleBytes32(e.roles, validatorHotkeyLabel(1))
	if err != nil {
		return RegisteredAlphaSnapshot{}, 0, err
	}
	reserveAlpha, ok := snapshot.ByHotkey[reserve]
	if !ok || !alphaShareMeets(snapshot.TotalAlphaRao, reserveAlpha, requiredShareBPS) {
		return RegisteredAlphaSnapshot{}, 0, fmt.Errorf("reserve validator alpha %d does not meet %d bps of registered alpha %d", reserveAlpha, requiredShareBPS, snapshot.TotalAlphaRao)
	}
	return snapshot, reserveAlpha, nil
}

func (e *Executor) reserveValidatorMajoritySnapshot() (RegisteredAlphaSnapshot, uint64, error) {
	return e.reserveValidatorShareSnapshot(e.cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS)
}

func (e *Executor) verifyReserveValidatorMajority() error {
	_, _, err := e.reserveValidatorMajoritySnapshot()
	return err
}

func (e *Executor) transferAlpha(ctx context.Context, a Action, targetKind string, index int) error {
	source, err := decodeHex32("approved alpha source hotkey", e.plan.LiveFacts.AlphaSourceHotkey)
	if err != nil {
		return err
	}
	var deployment *ContractDeployment
	if targetKind == "operator-deposit" {
		if err := e.ensurePayloads(ctx); err != nil {
			return err
		}
		deployment = &e.payloads.Manifest
	}
	destinationColdkey, destinationHotkey, err := alphaTransferDestination(e.roles, deployment, targetKind, index)
	if err != nil {
		return err
	}
	// A transaction already recorded for this exact current-plan intent must be
	// recovered from its immutable bytes and historical checkpoint. Re-running
	// live prebroadcast checks would incorrectly treat its already-mutated source
	// and destination as a second transfer pre-state.
	if _, resumed := e.journal.LatestTransaction(e.plan.PlanHash, a.ID, a.IntentHash); !resumed {
		if _, err := e.validateAlphaTransferPrebroadcast(ctx, a, destinationHotkey, targetKind == "validator" && index == 1); err != nil {
			return err
		}
	}
	amount := a.Spend.AlphaRao
	call, err := e.substrate.TransferStakeAndHotkeyCall(destinationColdkey, source, destinationHotkey, amount)
	if err != nil {
		return err
	}
	_, transactionBlock, err := e.substrate.Send(ctx, e.plan.PlanHash, a, call)
	if err != nil {
		return err
	}
	_, _, _, err = e.verifyAlphaTransferDeltaAtBlock(ctx, a, destinationHotkey, destinationColdkey, transactionBlock)
	return err
}

func (e *Executor) reconcileAlphaTransfer(ctx context.Context, action Action) error {
	if !hasFinalizedAlphaRecoveryEvidence(e.plan, action, e.journal.Entries()) {
		return fmt.Errorf("alpha reconciliation %s has no exact finalized ancestor evidence", action.ID)
	}
	block, err := strconv.ParseUint(action.Parameters[alphaRecoveryBlockParameter], 10, 64)
	if err != nil || block == 0 {
		return fmt.Errorf("alpha reconciliation %s has invalid recovery block", action.ID)
	}
	if err := e.verifySubstrateTransactionEvidence(ctx, ChainHead{Number: block, Hash: action.Parameters[alphaRecoveryBlockHashParameter]}, action.Parameters[alphaRecoveryTransactionHashParameter]); err != nil {
		return fmt.Errorf("alpha reconciliation %s canonical transaction: %w", action.ID, err)
	}
	kind, index, err := alphaTransferTargetFromActionID(action.ID)
	if err != nil {
		return err
	}
	var deployment *ContractDeployment
	if kind == "operator-deposit" {
		if err := e.ensurePayloads(ctx); err != nil {
			return err
		}
		deployment = &e.payloads.Manifest
	}
	coldkey, hotkey, err := alphaTransferDestination(e.roles, deployment, kind, index)
	if err != nil {
		return err
	}
	_, _, _, err = e.verifyAlphaTransferDeltaAtBlock(ctx, action, hotkey, coldkey, block)
	return err
}

func (e *Executor) recoveredAlphaMinimumStake(ctx context.Context, repair Action, hotkey, coldkey [32]byte) (uint64, error) {
	linked, err := e.planAction(repair.Parameters[alphaRepairForActionParameter])
	if err != nil || linked.Target != repair.Target {
		return 0, stateMismatchError(err, "alpha repair %s has no matching linked action", repair.ID)
	}
	if absolute := repair.Parameters[alphaRepairMinimumDestinationParameter]; absolute != "" {
		minimum, parseErr := strconv.ParseUint(absolute, 10, 64)
		if parseErr != nil || minimum == 0 {
			return 0, fmt.Errorf("alpha repair %s has invalid absolute minimum stake", repair.ID)
		}
		return minimum, nil
	}
	if linked.Kind != "substrate-reconciliation" {
		return 0, fmt.Errorf("alpha repair %s has no reconciliation action", repair.ID)
	}
	block, err := strconv.ParseUint(linked.Parameters[alphaRecoveryBlockParameter], 10, 64)
	if err != nil || block <= 1 {
		return 0, fmt.Errorf("alpha repair %s has invalid recovery block", repair.ID)
	}
	before, err := e.readStakeAt(ctx, block-1, hotkey, coldkey)
	if err != nil {
		return 0, err
	}
	increment, err := strconv.ParseUint(repair.Parameters[alphaRepairMinimumIncrementParameter], 10, 64)
	if err != nil || increment == 0 {
		return 0, fmt.Errorf("alpha repair %s has invalid minimum increment", repair.ID)
	}
	minimum, ok := checkedAdd(before, increment)
	if !ok {
		return 0, fmt.Errorf("alpha repair %s minimum destination stake overflows", repair.ID)
	}
	return minimum, nil
}

func (e *Executor) repairAlphaTransfer(ctx context.Context, action Action, kind string, index int) error {
	var deployment *ContractDeployment
	if kind == "operator-deposit" {
		if err := e.ensurePayloads(ctx); err != nil {
			return err
		}
		deployment = &e.payloads.Manifest
	}
	coldkey, hotkey, err := alphaTransferDestination(e.roles, deployment, kind, index)
	if err != nil {
		return err
	}
	targetShareBPS, minimumShareBPS, reserveShareRepair, err := reserveShareRepairTerms(action)
	if err != nil {
		return err
	}
	if reserveShareRepair {
		if kind != "validator" || index != 1 || targetShareBPS != e.cfg.Config.ValidatorBootstrap.ReserveTargetShareBPS || minimumShareBPS != e.cfg.Config.ValidatorBootstrap.ReserveMinimumShareBPS {
			return fmt.Errorf("alpha repair %s reserve-share bounds differ from the active reserve validator", action.ID)
		}
		if err := e.transferAlpha(ctx, action, kind, index); err != nil {
			return err
		}
		_, _, err = e.reserveValidatorShareSnapshot(targetShareBPS)
		return err
	}
	minimum, err := e.recoveredAlphaMinimumStake(ctx, action, hotkey, coldkey)
	if err != nil {
		return err
	}
	current, err := e.readStakeFinalized(ctx, hotkey, coldkey)
	if err != nil {
		return err
	}
	minimumCredit, err := alphaTransferMinimumCreditRao(action.Spend.AlphaRao)
	if err != nil {
		return err
	}
	_, resumed := e.journal.LatestTransaction(e.plan.PlanHash, action.ID, action.IntentHash)
	skip, err := alphaRepairPrebroadcast(current, minimum, minimumCredit, resumed)
	if err != nil {
		return fmt.Errorf("alpha repair %s: %w", action.ID, err)
	}
	if skip {
		return nil
	}
	if err := e.transferAlpha(ctx, action, kind, index); err != nil {
		return err
	}
	post, err := e.readStakeFinalized(ctx, hotkey, coldkey)
	if err != nil || post < minimum {
		return stateMismatchError(err, "alpha repair %s stake=%d want>=%d", action.ID, post, minimum)
	}
	return nil
}

// Prove the destination share entitlement at the parent and inclusion blocks.
// This is both the live postcondition and the crash-recovery path; it never
// derives a pre-state from an already-finalized current balance.
func (e *Executor) verifyAlphaTransferDeltaAtBlock(ctx context.Context, action Action, hotkey, coldkey [32]byte, block uint64) (uint64, uint64, uint64, error) {
	if block <= 1 {
		return 0, 0, 0, fmt.Errorf("alpha transfer %s has invalid finalized block %d", action.ID, block)
	}
	before, err := e.readStakeAt(ctx, block-1, hotkey, coldkey)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read alpha transfer %s parent stake at %d: %w", action.ID, block-1, err)
	}
	after, err := e.readStakeAt(ctx, block, hotkey, coldkey)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read alpha transfer %s finalized stake at %d: %w", action.ID, block, err)
	}
	shortfall, err := alphaTransferRoundingShortfall(action)
	if err != nil {
		return 0, 0, 0, err
	}
	credited, err := alphaTransferCreditedRao(before, after, action.Spend.AlphaRao, shortfall)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("alpha transfer %s finalized delta: %w", action.ID, err)
	}
	return before, after, credited, nil
}

func alphaTransferDestination(roles *RoleSecrets, deployment *ContractDeployment, targetKind string, index int) ([32]byte, [32]byte, error) {
	var coldkey, hotkey [32]byte
	var err error
	switch targetKind {
	case "operator-deposit":
		if deployment == nil || deployment.CoordinatorProxy == (common.Address{}) {
			return coldkey, hotkey, errors.New("coordinator deployment is required for deposit custody")
		}
		// _reserve executes in the coordinator proxy context, so StakingV2
		// can only move stake owned by this mirror. The deposit signer is an
		// authorization role; it must never be confused with the custodian.
		coldkey = ss58Mirror(deployment.CoordinatorProxy)
		hotkey, err = roleBytes32(roles, fmt.Sprintf("operator-%d-deposit-hotkey", index))
	case "validator":
		coldkey, err = roleBytes32(roles, fmt.Sprintf("validator-%d-coldkey", index))
		if err == nil {
			hotkey, err = roleBytes32(roles, validatorHotkeyLabel(index))
		}
	default:
		err = fmt.Errorf("unsupported alpha transfer target %q", targetKind)
	}
	return coldkey, hotkey, err
}
func (e *Executor) registerNative(ctx context.Context, a Action, coldLabel, hotLabel string) error {
	hot, err := roleBytes32(e.roles, hotLabel)
	if err != nil {
		return err
	}
	expectedColdkey, err := roleBytes32(e.roles, coldLabel)
	if err != nil {
		return err
	}
	if _, ok, err := e.substrate.UID(hot); err == nil && ok {
		owner, ownerErr := e.substrate.HotkeyOwner(hot)
		if ownerErr != nil {
			return ownerErr
		}
		return validateHotkeyOwner(hotLabel, owner, expectedColdkey)
	}
	signer, err := e.substrate.RoleSigner(e.roles, coldLabel)
	if err != nil {
		return err
	}
	_, limit, err := e.boundedRegistrationBurn(a)
	if err != nil {
		return err
	}
	call, err := e.substrate.BurnRegisterLimitCall(hot, limit)
	if err != nil {
		return err
	}
	_, _, err = e.substrate.SendAs(ctx, e.plan.PlanHash, a, call, signer)
	if err != nil {
		return err
	}
	_, ok, err := e.substrate.UID(hot)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("hotkey not registered after finalized extrinsic")
	}
	owner, err := e.substrate.HotkeyOwner(hot)
	if err != nil {
		return err
	}
	return validateHotkeyOwner(hotLabel, owner, expectedColdkey)
}

type challengerChurnState struct {
	ExpectedUID     uint16
	RuntimePruneUID uint16
	ChallengerUID   uint16
	ChallengerFound bool
	ChurnUID        uint16
	ChurnFound      bool
	UIDCount        uint16
	MaximumUIDs     uint16
}

type registrationReplacementState struct {
	ExpectedUID      uint16
	RuntimePruneUID  uint16
	ReplacementUID   uint16
	ReplacementFound bool
	ChurnUID         uint16
	ChurnFound       bool
	UIDCount         uint16
	MaximumUIDs      uint16
}

func validateRegistrationReplacementPreState(state registrationReplacementState) error {
	if state.MaximumUIDs == 0 || state.UIDCount != state.MaximumUIDs {
		return fmt.Errorf("contract registration replacement requires a full subnet: count=%d maximum=%d", state.UIDCount, state.MaximumUIDs)
	}
	if state.ReplacementFound {
		return errors.New("contract registration replacement hotkey is already registered")
	}
	if !state.ChurnFound || state.ChurnUID != state.ExpectedUID {
		return fmt.Errorf("expected churn-floor UID %d is not live at that UID", state.ExpectedUID)
	}
	if state.RuntimePruneUID != state.ExpectedUID {
		return fmt.Errorf("runtime-454 would prune UID %d, not approved churn-floor UID %d", state.RuntimePruneUID, state.ExpectedUID)
	}
	return nil
}

func validateRegistrationReplacementPostState(state registrationReplacementState) error {
	if state.MaximumUIDs == 0 || state.UIDCount != state.MaximumUIDs {
		return fmt.Errorf("contract registration replacement changed subnet capacity: count=%d maximum=%d", state.UIDCount, state.MaximumUIDs)
	}
	if !state.ReplacementFound || state.ReplacementUID != state.ExpectedUID {
		return fmt.Errorf("contract registration replacement UID=%d found=%t, want UID %d", state.ReplacementUID, state.ReplacementFound, state.ExpectedUID)
	}
	if state.ChurnFound {
		return fmt.Errorf("replaced churn-floor hotkey remains live at UID %d", state.ChurnUID)
	}
	return nil
}

func validateChallengerChurnPreState(state challengerChurnState) error {
	if state.MaximumUIDs == 0 || state.UIDCount != state.MaximumUIDs {
		return fmt.Errorf("challenger registration requires a full subnet: count=%d maximum=%d", state.UIDCount, state.MaximumUIDs)
	}
	if state.ChallengerFound {
		return errors.New("challenger is already registered in pre-state")
	}
	if !state.ChurnFound || state.ChurnUID != state.ExpectedUID {
		return fmt.Errorf("expected churn-floor UID %d is not live at that UID", state.ExpectedUID)
	}
	if state.RuntimePruneUID != state.ExpectedUID {
		return fmt.Errorf("runtime-454 would prune UID %d, not approved churn-floor UID %d", state.RuntimePruneUID, state.ExpectedUID)
	}
	return nil
}

func validateChallengerChurnPostState(state challengerChurnState) error {
	if state.MaximumUIDs == 0 || state.UIDCount != state.MaximumUIDs {
		return fmt.Errorf("challenger replacement changed subnet capacity: count=%d maximum=%d", state.UIDCount, state.MaximumUIDs)
	}
	if !state.ChallengerFound || state.ChallengerUID != state.ExpectedUID {
		return fmt.Errorf("challenger UID=%d found=%t, want replaced UID %d", state.ChallengerUID, state.ChallengerFound, state.ExpectedUID)
	}
	if state.ChurnFound {
		return fmt.Errorf("churn-floor hotkey remains live at UID %d", state.ChurnUID)
	}
	return nil
}

func (e *Executor) planAction(id string) (Action, error) {
	if e.plan == nil {
		return Action{}, errors.New("approved plan is unavailable")
	}
	for _, action := range e.plan.Actions {
		if action.ID == id {
			return action, nil
		}
	}
	return Action{}, fmt.Errorf("approved plan has no action %s", id)
}

func (e *Executor) actionVerified(id string) bool {
	action, err := e.planAction(id)
	if err != nil || e.journal == nil {
		return false
	}
	_, ok := e.verifiedActionEntry(action)
	return ok
}

// Resolve an exact verified intent from the active plan or one explicitly
// named ancestor. The caller still revalidates the durable and, where needed,
// current postcondition before relying on it.
func (e *Executor) verifiedActionEntry(action Action) (JournalEntry, bool) {
	return e.verifiedActionEntryForScope(action, false)
}

// Resolve terminal lineage evidence, optionally exposing an ancestor topology
// only so its stopped-generation receipt can be authenticated as history.
func (e *Executor) verifiedActionEntryForScope(action Action, includeAncestorTopology bool) (JournalEntry, bool) {
	if e == nil || e.plan == nil || e.journal == nil {
		return JournalEntry{}, false
	}
	allowedPlans := e.plan.allowedPlanHashes()
	entries := e.journal.Entries()
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		// Topology readiness is an ephemeral live postcondition intentionally
		// destroyed by stop or host reboot. Never let an ancestor plan authorize
		// the active plan's launch lifecycle: LaunchDeployment must start the
		// current binaries, pass readiness, and record a current-plan receipt.
		if !includeAncestorTopology && action.ID == "topology.launch" && entry.PlanHash != e.plan.PlanHash {
			continue
		}
		if allowedPlans[entry.PlanHash] && entry.ActionID == action.ID && actionAcceptsIntent(action, entry.IntentHash) && entry.Stage == StageVerified {
			return entry, true
		}
	}
	return JournalEntry{}, false
}

const (
	carriedActionVerificationWorkers = 8
	carriedActionVerificationTimeout = 5 * time.Minute
	carriedActionProgressInterval    = 50
)

// Execute independent read-only audits in bounded concurrent batches, but
// report the first failure in canonical plan order. A failed batch prevents
// later work from reaching a rate-limited public archive after the release is
// already known to be invalid.
func runOrderedConcurrentAudits(count, workers int, audit func(int) error) error {
	if count < 0 || workers <= 0 || audit == nil {
		return errors.New("concurrent audit configuration is invalid")
	}
	if count == 0 {
		return nil
	}
	if workers > count {
		workers = count
	}
	for first := 0; first < count; first += workers {
		last := first + workers
		if last > count {
			last = count
		}
		errs := make([]error, last-first)
		var wait sync.WaitGroup
		wait.Add(last - first)
		for index := first; index < last; index++ {
			index := index
			go func() {
				defer wait.Done()
				errs[index-first] = audit(index)
			}()
		}
		wait.Wait()
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Verify every exact ancestor intent before the revised plan performs its
// first mutation. This prevents a missing receipt, stale live postcondition,
// or unavailable finalized transaction from being discovered only after new
// funding has already been submitted.
func (e *Executor) verifyCarriedActionHistory(ctx context.Context) error {
	if e == nil || e.plan == nil || e.journal == nil {
		return errors.New("plan/journal is unavailable")
	}
	audits := make([]carriedActionAudit, 0)
	for _, action := range e.plan.Actions {
		entry, ok := e.verifiedActionEntry(action)
		if !ok && action.ID == "topology.launch" {
			// The stopped ancestor generation cannot satisfy current liveness, but
			// its durable receipt remains part of the approved history and must not
			// disappear or change unnoticed. Authenticate it without adding it to
			// the live-verification cache; LaunchDeployment will create and verify
			// the current generation before any dependent action can execute.
			entry, ok = e.verifiedActionEntryForScope(action, true)
			if ok && entry.PlanHash != e.plan.PlanHash {
				if _, err := e.readPersistedPostcondition(entry); err != nil {
					return fmt.Errorf("action %s: persisted ancestor process receipt: %w", action.ID, err)
				}
			}
			continue
		}
		if !ok || entry.PlanHash == e.plan.PlanHash {
			continue
		}
		record, err := e.readPersistedPostcondition(entry)
		if err != nil {
			return fmt.Errorf("action %s: persisted postcondition: %w", action.ID, err)
		}
		audits = append(audits, carriedActionAudit{action: action, entry: entry, record: record})
	}
	// Contract postcondition readers share the immutable payload cache. Resolve
	// it once before workers start so the cache and deployment manifest are
	// never initialized concurrently.
	if len(audits) > 0 && planUsesContractDeploymentEnvelope(e.plan.Schema) {
		if err := e.ensurePayloads(ctx); err != nil {
			return fmt.Errorf("prepare carried contract payloads: %w", err)
		}
	}
	fleetHistoryKeys, err := e.verifyCarriedFleetGenerationOneHistory(ctx, audits)
	if err != nil {
		return fmt.Errorf("prepare carried fleet history: %w", err)
	}
	e.carriedFleetHistoryKeys = fleetHistoryKeys
	defer func() { e.carriedFleetHistoryKeys = nil }()
	var sharedEVMHead *ChainHead
	for _, audit := range audits {
		if !actionPostStateRequiresEVMCheckpoint(audit.action) {
			continue
		}
		if e.deployer == nil || e.deployer.client == nil {
			return errors.New("prepare carried EVM checkpoint: EVM postcondition client is unavailable")
		}
		head, err := finalizedEVMHead(ctx, e.deployer.client)
		if err != nil {
			return fmt.Errorf("prepare carried EVM checkpoint: %w", err)
		}
		sharedEVMHead = &head
		break
	}
	var sharedNativeHead *ChainHead
	for _, audit := range audits {
		if actionRequiresCurrentPostcondition(audit.action) {
			continue
		}
		if _, transactionErr := e.consumedActionTransaction(audit.action, audit.entry); transactionErr == nil {
			if e.substrate == nil {
				return errors.New("prepare carried native checkpoint: Substrate postcondition client is unavailable")
			}
			nativeHash, nativeNumber, err := e.substrate.finalizedHeadContext(ctx)
			if err != nil {
				return fmt.Errorf("prepare carried native checkpoint: %w", err)
			}
			sharedNativeHead = &ChainHead{Number: nativeNumber, Hash: nativeHash.Hex()}
			break
		}
	}
	var completed atomic.Uint64
	if err := runOrderedConcurrentAudits(len(audits), carriedActionVerificationWorkers, func(index int) error {
		audit := audits[index]
		auditCtx, cancel := context.WithTimeout(ctx, carriedActionVerificationTimeout)
		defer cancel()
		err := e.verifyVerifiedActionStateWithRecord(auditCtx, audit.action, audit.entry, audit.record, sharedEVMHead, sharedNativeHead)
		count := completed.Add(1)
		if count%carriedActionProgressInterval == 0 || count == uint64(len(audits)) {
			fmt.Fprintf(os.Stderr, "sim-testnet: carried action audit %d/%d\n", count, len(audits))
		}
		if err != nil {
			return fmt.Errorf("action %s: %w", audit.action.ID, err)
		}
		return nil
	}); err != nil {
		return err
	}
	verifiedKeys := make(map[string]bool, len(audits))
	for _, audit := range audits {
		verifiedKeys[carriedVerificationKey(audit.entry)] = true
	}
	e.carriedVerificationKeys = verifiedKeys
	return nil
}

// Bind the in-memory audit cache to the immutable verified journal evidence.
// It lives only for one executor process and never substitutes for a resume.
func carriedVerificationKey(entry JournalEntry) string {
	return entry.PlanHash + "\x00" + entry.ActionID + "\x00" + entry.IntentHash + "\x00" + entry.PostconditionHash
}

func (e *Executor) registrationSetupProgressed() bool {
	if e == nil || e.plan == nil {
		return false
	}
	for _, action := range e.plan.Actions {
		if action.Spend.Registrations > 0 && e.actionVerified(action.ID) {
			return true
		}
	}
	return false
}

func observedPostconditionUID(value any) (uint16, error) {
	var uid uint64
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed < 0 || typed > math.MaxUint16 || math.Trunc(typed) != typed {
			return 0, fmt.Errorf("postcondition UID is not an unsigned integer")
		}
		uid = uint64(typed)
	case json.Number:
		parsed, err := strconv.ParseUint(string(typed), 10, 16)
		if err != nil {
			return 0, fmt.Errorf("postcondition UID: %w", err)
		}
		uid = parsed
	case uint16:
		uid = uint64(typed)
	case uint64:
		uid = typed
	default:
		return 0, fmt.Errorf("postcondition UID has type %T", value)
	}
	if uid > uint64(^uint16(0)) {
		return 0, fmt.Errorf("postcondition UID %d exceeds uint16", uid)
	}
	return uint16(uid), nil
}

func (e *Executor) registrationPostconditionUID(actionID string) (uint16, error) {
	action, err := e.planAction(actionID)
	if err != nil {
		return 0, err
	}
	entry, ok := e.verifiedActionEntry(action)
	if !ok {
		return 0, fmt.Errorf("registration action %s is not verified", actionID)
	}
	if err := e.verifyPersistedPostcondition(entry); err != nil {
		return 0, err
	}
	var record ActionPostcondition
	if err := readJSONFile(filepath.Join(e.stateDir, filepath.FromSlash(entry.PostconditionPath)), &record); err != nil {
		return 0, err
	}
	return observedPostconditionUID(record.Observed["uid"])
}

func (e *Executor) readRegistrationReplacementState(churn int, replacementLabel string) (registrationReplacementState, error) {
	if churn < 1 || churn > e.cfg.Config.Topology.ChurnFloorUIDs || replacementLabel == "" {
		return registrationReplacementState{}, errors.New("registration replacement identity is out of range")
	}
	expectedUID, err := e.registrationPostconditionUID(fmt.Sprintf("churn.register.%d", churn))
	if err != nil {
		return registrationReplacementState{}, err
	}
	replacementHotkey, err := roleBytes32(e.roles, replacementLabel)
	if err != nil {
		return registrationReplacementState{}, err
	}
	churnHotkey, err := roleBytes32(e.roles, churnHotkeyLabel(churn))
	if err != nil {
		return registrationReplacementState{}, err
	}
	replacementUID, replacementFound, err := e.substrate.UID(replacementHotkey)
	if err != nil {
		return registrationReplacementState{}, err
	}
	churnUID, churnFound, err := e.substrate.UID(churnHotkey)
	if err != nil {
		return registrationReplacementState{}, err
	}
	count, err := e.substrate.UIDCount()
	if err != nil {
		return registrationReplacementState{}, err
	}
	maximum := hyperparameterUint64(e.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"])
	if maximum == 0 || maximum > uint64(^uint16(0)) {
		return registrationReplacementState{}, fmt.Errorf("approved max_allowed_uids %d is invalid", maximum)
	}
	pruneUID, err := e.substrate.Runtime453PruneCandidate()
	if err != nil {
		return registrationReplacementState{}, err
	}
	return registrationReplacementState{
		ExpectedUID: expectedUID, RuntimePruneUID: pruneUID, ReplacementUID: replacementUID, ReplacementFound: replacementFound,
		ChurnUID: churnUID, ChurnFound: churnFound, UIDCount: count, MaximumUIDs: uint16(maximum),
	}, nil
}

func (e *Executor) verifyContractRegistrationReplacementPrecondition(action Action, replacementLabel string, registration int) error {
	generation := e.plan.Deployment.RegistrationRoleGeneration
	if generation == 0 {
		if action.Parameters["registration_role_generation"] != "0" || action.Parameters["expected_replaced_churn"] != "" {
			return errors.New("generation-zero contract registration has replacement parameters")
		}
		return nil
	}
	churn, err := churnIndexForContractRegistration(e.cfg.Config.Topology, generation, registration)
	if err != nil {
		return err
	}
	if action.Parameters["registration_role_generation"] != strconv.FormatUint(generation, 10) || action.Parameters["expected_replaced_churn"] != strconv.Itoa(churn) {
		return errors.New("contract registration replacement parameters differ from the approved generation and churn UID")
	}
	state, err := e.readRegistrationReplacementState(churn, replacementLabel)
	if err != nil {
		return err
	}
	if err := validateRegistrationReplacementPreState(state); err != nil {
		return err
	}
	hotkey, err := roleBytes32(e.roles, replacementLabel)
	if err != nil {
		return err
	}
	owner, err := e.substrate.HotkeyOwner(hotkey)
	if err != nil {
		return err
	}
	if owner != ([32]byte{}) {
		return fmt.Errorf("replacement role %s already has global coldkey owner 0x%x", replacementLabel, owner)
	}
	return nil
}

func (e *Executor) readChallengerChurnState(fleet int) (challengerChurnState, error) {
	challenger := fleet - e.cfg.Config.Topology.HeadFleets
	if challenger < 1 || challenger > e.cfg.Config.Topology.ChallengerFleets || challenger > e.cfg.Config.Topology.ChurnFloorUIDs {
		return challengerChurnState{}, fmt.Errorf("fleet %d is not a bounded challenger", fleet)
	}
	churn, err := churnIndexForChallenger(e.cfg.Config.Topology, e.plan.Deployment.RegistrationRoleGeneration, challenger)
	if err != nil {
		return challengerChurnState{}, err
	}
	expectedUID, err := e.registrationPostconditionUID(fmt.Sprintf("churn.register.%d", churn))
	if err != nil {
		return challengerChurnState{}, err
	}
	challengerHotkey, err := roleBytes32(e.roles, fleetHotkeyLabel(fleet))
	if err != nil {
		return challengerChurnState{}, err
	}
	churnHotkey, err := roleBytes32(e.roles, churnHotkeyLabel(churn))
	if err != nil {
		return challengerChurnState{}, err
	}
	challengerUID, challengerFound, err := e.substrate.UID(challengerHotkey)
	if err != nil {
		return challengerChurnState{}, err
	}
	churnUID, churnFound, err := e.substrate.UID(churnHotkey)
	if err != nil {
		return challengerChurnState{}, err
	}
	count, err := e.substrate.UIDCount()
	if err != nil {
		return challengerChurnState{}, err
	}
	maximum := hyperparameterUint64(e.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"])
	if maximum == 0 || maximum > uint64(^uint16(0)) {
		return challengerChurnState{}, fmt.Errorf("approved max_allowed_uids %d is invalid", maximum)
	}
	pruneUID, err := e.substrate.Runtime453PruneCandidate()
	if err != nil {
		return challengerChurnState{}, err
	}
	return challengerChurnState{
		ExpectedUID: expectedUID, RuntimePruneUID: pruneUID, ChallengerUID: challengerUID, ChallengerFound: challengerFound,
		ChurnUID: churnUID, ChurnFound: churnFound, UIDCount: count, MaximumUIDs: uint16(maximum),
	}, nil
}

func (e *Executor) registerChallenger(ctx context.Context, action Action, fleet int) error {
	state, err := e.readChallengerChurnState(fleet)
	if err != nil {
		return err
	}
	if state.ChallengerFound {
		return validateChallengerChurnPostState(state)
	}
	if err := validateChallengerChurnPreState(state); err != nil {
		return err
	}
	return e.registerNative(ctx, action, fleetColdkeyLabel(fleet), fleetHotkeyLabel(fleet))
}

func (e *Executor) setReserveTakeZero(ctx context.Context, a Action) error {
	hotkey, err := roleBytes32(e.roles, "reserve-hotkey")
	if err != nil {
		return err
	}
	take, err := e.substrate.DelegateTake(hotkey)
	if err != nil {
		return err
	}
	if take == 0 {
		return nil
	}
	call, err := e.substrate.DecreaseTakeCall(hotkey, 0)
	if err != nil {
		return err
	}
	signer, err := e.substrate.RoleSigner(e.roles, "validator-1-coldkey")
	if err != nil {
		return err
	}
	if _, _, err := e.substrate.SendAs(ctx, e.plan.PlanHash, a, call, signer); err != nil {
		return err
	}
	post, err := e.substrate.DelegateTake(hotkey)
	if err != nil {
		return err
	}
	if post != 0 {
		return fmt.Errorf("reserve validator delegate take is %d, require zero", post)
	}
	return nil
}

func RenderRuntimeConfigs(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) error {
	if cfg == nil || cfg.Public == nil {
		return errors.New("runtime config public manifest is unavailable")
	}
	publicRPCURL := strings.TrimSpace(cfg.Public.Chain.EVMPublicReadEndpoint)
	if publicRPCURL == "" || publicRPCURL != cfg.Public.Chain.EVMPublicReadEndpoint {
		return errors.New("runtime config public testnet EVM RPC URL is missing or non-canonical")
	}
	contracts, err := loadContractDeployment(stateDir)
	if err != nil {
		return err
	}
	eventSyncBlock, err := contractDeploymentEventSyncBlock(contracts)
	if err != nil {
		return err
	}
	if err := preflightSignedAttemptStateNamespaces(cfg, stateDir); err != nil {
		return err
	}
	if err := ensureOperatorConfigOverlays(cfg, stateDir); err != nil {
		return err
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", i))
		vaultRoot := filepath.Join(root, "vault")
		if err := copyTree(filepath.Join(cfg.Repos.Vault, "local"), vaultRoot, 0o600); err != nil {
			return err
		}
		if err := renderOperatorProviderEgressProbeIsolation(root); err != nil {
			return err
		}
		if err := renderOperatorConnectTLS(cfg, stateDir, i); err != nil {
			return err
		}
		deposit := roles.EVM[fmt.Sprintf("operator-%d-deposit", i)].PrivateKeyHex
		rootKey := roles.EVM[fmt.Sprintf("operator-%d-root", i)].PrivateKeyHex
		artifactKey := roles.EVM[fmt.Sprintf("operator-%d-artifact", i)].PrivateKeyHex
		depositHotkey := "0x" + roles.Substrate[fmt.Sprintf("operator-%d-deposit-hotkey", i)].PublicKeyHex
		// Testnet services are intentionally rendered with only testnet-prefixed
		// values. The server loader must never be able to fall through to a
		// mainnet signer or address when URNETWORK_ST_PROFILE=testnet.
		st := map[string]any{
			"profile":                                    "testnet",
			"testnet-enabled":                            true,
			"testnet-wallet-allow-unsigned":              false,
			"testnet-public-rpc-url":                     publicRPCURL,
			"testnet-authority":                          workloadRPCAuthority(),
			"testnet-rpc-urls":                           []string{evmHTTP(workloadRPCAuthority())},
			"testnet-chain-id":                           testnetChainID,
			"testnet-genesis-hash":                       testnetGenesis,
			"testnet-deployment-id":                      cfg.Config.Deployment.DeploymentID,
			"testnet-policy-hash":                        cfg.PolicyHash,
			"testnet-coordinator-address":                contracts.CoordinatorProxy.Hex(),
			"testnet-settlement-vault-address":           contracts.SettlementVault.Hex(),
			"testnet-reserve-sink-address":               contracts.ReserveSink.Hex(),
			"testnet-deploy-block":                       eventSyncBlock,
			"testnet-netuid":                             cfg.Netuid,
			"testnet-no-id":                              i,
			"testnet-treasury-hotkey":                    "0x" + roles.Substrate[operatorPoolHotkeyLabelForGeneration(i, contracts.RegistrationRoleGeneration)].PublicKeyHex,
			"testnet-deposit-hotkey":                     depositHotkey,
			"testnet-deposit-key":                        deposit,
			"testnet-root-key":                           rootKey,
			"testnet-artifact-key":                       artifactKey,
			"testnet-deposit-rate-numerator-rao-per-gib": cfg.Policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB,
			"testnet-deposit-rate-denominator":           cfg.Policy.Deposit.Tiers[0].RateDenominator,
			"testnet-deposit-tiers":                      cfg.Policy.Deposit.Tiers,
			"testnet-deposit-epoch-cap-rao":              cfg.Policy.Deposit.EpochCapRaoPerOperator,
			"testnet-reliability-a-min":                  cfg.Policy.Verify.ReliabilityAMin,
			"testnet-block-seconds":                      cfg.Public.Chain.ExpectedBlockSeconds,
		}
		b, _ := yaml.Marshal(st)
		if err := atomicWrite(filepath.Join(vaultRoot, "st.yml"), b, 0o600); err != nil {
			return err
		}
		pgSeed := derive32(cfg, fmt.Sprintf("dependency/postgres-%d", i))
		pgPassword := hex.EncodeToString(pgSeed[:])
		pg := map[string]any{"authority": "{{ env:BRINGYOUR_POSTGRES_HOSTNAME }}:5432", "user": "bringyour", "password": pgPassword, "db": "bringyour"}
		b, _ = yaml.Marshal(pg)
		if err := atomicWrite(filepath.Join(vaultRoot, "pg.yml"), b, 0o600); err != nil {
			return err
		}
		// Maintenance migrations connect to the same isolated container.
		if err := atomicWrite(filepath.Join(vaultRoot, "pg_maintenance.yml"), b, 0o600); err != nil {
			return err
		}
		b, _, err = operatorVerifyConfig(cfg, i, false)
		if err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(vaultRoot, "verify.yml"), b, 0o600); err != nil {
			return err
		}
		if err := renderOperatorBlobConfig(cfg, i, filepath.Join(vaultRoot, "minio.yml")); err != nil {
			return err
		}
		site := filepath.Join(root, "site")
		_ = os.MkdirAll(site, 0o700)
		settings, err := yaml.Marshal(operatorSimulationSiteSettings())
		if err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(site, "settings.yml"), settings, 0o600); err != nil {
			return err
		}
	}
	if err := renderValidatorMinerConfigs(cfg, stateDir, roles, contracts); err != nil {
		return err
	}
	return writeRuntimeConfigManifest(cfg, stateDir)
}

// Disable the production provider-probe fleet and remove its ingest
// credential. Sim-testnet exercises provider traffic through its own bounded
// miner/validator scenarios; a background prober would be uncontrolled
// external load and must remain inert even if the source local vault changes.
func renderOperatorProviderEgressProbeIsolation(operatorRoot string) error {
	secretPath := filepath.Join(operatorRoot, "vault", "provider_egress.yml")
	if info, err := os.Lstat(secretPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("operator provider egress secret path is not a regular file")
		}
		if err := os.Remove(secretPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	config, err := yaml.Marshal(struct {
		Enabled bool `yaml:"enabled"`
	}{Enabled: false})
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(operatorRoot, "config", "provider_egress_probe.yml"), config, 0o600)
}

// Supply deterministic location metadata for simulator-owned loopback peers.
// The raw source addresses remain distinct, so subnet-diversity accounting is
// still exercised; only the packaged public-IP database lookup is replaced.
func operatorSimulationSiteSettings() map[string]any {
	return map[string]any{
		"env_vars": map[string]string{"URNETWORK_ST_PROFILE": "testnet"},
		"ip_overrides": []map[string]any{{
			"subnet": "127.0.0.0/8", "continent_code": "na", "continent": "North America",
			"country_code": "us", "country": "United States", "region": "Sim Testnet",
			"city": "Sim Testnet", "latitude": 37.7749, "longitude": -122.4194,
			"timezone": "America/Los_Angeles", "hosting": false, "privacy": false, "virtual": false,
		}},
	}
}

func operatorVerifyConfig(cfg *ResolvedConfig, operator int, rotated bool) ([]byte, map[byte]string, error) {
	if cfg == nil || operator < 1 || operator > cfg.Config.Topology.Operators {
		return nil, nil, errors.New("invalid operator verify config request")
	}
	seed0 := derive32(cfg, fmt.Sprintf("server/operator-%d/verify", operator))
	seed1 := derive32(cfg, fmt.Sprintf("server/operator-%d/verify-rotation-1", operator))
	keys := []map[string]any{{"server_key_id": 0, "seed": base64.StdEncoding.EncodeToString(seed0[:])}}
	if rotated {
		keys = []map[string]any{
			{"server_key_id": 1, "seed": base64.StdEncoding.EncodeToString(seed1[:])},
			{"server_key_id": 0, "seed": base64.StdEncoding.EncodeToString(seed0[:])},
		}
	}
	egressHashKey := derive32(cfg, "verify/egress-hash-key")
	value := map[string]any{
		"profile":         "testnet",
		"policy_hash":     cfg.PolicyHash,
		"egress_hash_key": base64.StdEncoding.EncodeToString(egressHashKey[:]),
		"settings":        cfg.Policy.Verify,
		"keys":            keys,
	}
	b, err := yaml.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	public := map[byte]string{0: "0x" + hex.EncodeToString(ed25519.NewKeyFromSeed(seed0[:]).Public().(ed25519.PublicKey))}
	if rotated {
		public[1] = "0x" + hex.EncodeToString(ed25519.NewKeyFromSeed(seed1[:]).Public().(ed25519.PublicKey))
	}
	return b, public, nil
}

func renderOperatorBlobConfig(cfg *ResolvedConfig, operator int, destination string) error {
	prefix, err := operatorArtifactPrefix(cfg.Config, operator)
	if err != nil {
		return err
	}
	var value map[string]any
	if err := strictYAML(filepath.Join(cfg.Repos.Vault, "main", "minio.yml"), &value); err != nil {
		return err
	}
	if fmt.Sprint(value["bucket"]) != "blob" {
		return fmt.Errorf("minio.yml must select the blob bucket")
	}
	value["prefix"] = prefix
	b, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return atomicWrite(destination, b, 0o600)
}

// Reserve public-provider capacity for operator settlement and provider claim
// transactions. A private operational node keeps the faster block-cadence
// observation path.
func validatorPollSeconds(cfg *ResolvedConfig) int {
	if cfg != nil && cfg.OperationalRPCMode == rpcModePublicOverride {
		return 60
	}
	seconds := uint64(15)
	if cfg != nil && cfg.Public != nil && cfg.Public.Chain.ExpectedBlockSeconds > seconds {
		seconds = cfg.Public.Chain.ExpectedBlockSeconds
	}
	return int(min(seconds, uint64(60)))
}

// Avoids 1,000 claim workers continuously rechecking a constrained shared
// public endpoint while preserving the faster private-node development path.
func claimPollSeconds(cfg *ResolvedConfig) int {
	if cfg != nil && cfg.OperationalRPCMode == rpcModePublicOverride {
		return 60
	}
	return 10
}

func evmHTTP(authority string) string { _, h, _ := authorityURLs(authority); return h }
func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWrite(dst, b, mode)
}
func copyTree(src, dst string, mode os.FileMode) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyFile(path, target, mode)
	})
}

func renderValidatorMinerConfigs(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, c *ContractDeployment) error {
	if err := prepareSignedAttemptStateNamespaces(cfg, stateDir); err != nil {
		return err
	}
	eventSyncBlock, err := contractDeploymentEventSyncBlock(c)
	if err != nil {
		return err
	}
	base := map[string]any{"schema_version": 1, "production": true, "release": "1.0", "deployment_id": cfg.Config.Deployment.DeploymentID, "chain_id": testnetChainID, "genesis_hash": testnetGenesis, "runtime_spec": cfg.Public.Chain.ExpectedRuntimeSpec, "transaction_version": cfg.Public.Chain.ExpectedTransactionVersion, "state_version": cfg.Public.Chain.ExpectedStateVersion, "runtime_code_hash": cfg.Release.Runtime.CodeHash, "runtime_metadata_hash": cfg.Release.Runtime.MetadataHash, "netuid": cfg.Netuid, "coordinator": c.CoordinatorProxy.Hex(), "settlement_vault": c.SettlementVault.Hex(), "deploy_block": eventSyncBlock, "policy_hash": cfg.PolicyHash, "rpc": []string{evmHTTP(workloadRPCAuthority())}, "substrate": []string{substrateWS(workloadSubstrateRPCAuthority())}}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		v := cloneMap(base)
		v["validator_id"] = i
		v["state_dir"] = filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", i), "state")
		v["hotkey_seed_file"] = filepath.Join(stateDir, "secrets", fmt.Sprintf("validator-%d-hotkey.seed", i))
		v["controlled_no_ids"] = controlledNOIDsForValidator(i)
		v["trail_depth"] = cfg.Policy.Verify.TrailDepth
		v["poll_seconds"] = validatorPollSeconds(cfg)
		v["version_key"] = hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["weights_version_key"])
		v["policy"] = cfg.Policy
		v["operators"] = operatorDirectory(cfg, stateDir, roles, i)
		b, _ := yaml.Marshal(v)
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", i), "validator.yml")
		if err := atomicWrite(path, b, 0o600); err != nil {
			return err
		}
		seed, _ := hex.DecodeString(roles.Substrate[validatorHotkeyLabel(i)].SeedHex)
		if err := atomicWrite(v["hotkey_seed_file"].(string), append([]byte("0x"), []byte(hex.EncodeToString(seed))...), 0o600); err != nil {
			return err
		}
		for op := 1; op <= cfg.Config.Topology.Operators; op++ {
			label := fmt.Sprintf("validator-%d-no-%d", i, op)
			clientSeed, err := hex.DecodeString(roles.Clients[label].SeedHex)
			if err != nil || len(clientSeed) != 32 {
				return fmt.Errorf("%s client seed is invalid", label)
			}
			clientSeedPath := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", i), "state", "operators", fmt.Sprintf("no-%d", op), "client.key")
			if err := atomicWrite(clientSeedPath, clientSeed, 0o600); err != nil {
				return err
			}
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		claimKey, ok := roles.EVM[fmt.Sprintf("operator-%d-claim-relayer", operator)]
		if !ok || claimKey.PrivateKeyHex == "" {
			return fmt.Errorf("operator %d claim relayer key is missing", operator)
		}
		claimKeyPath := filepath.Join(stateDir, "secrets", fmt.Sprintf("operator-%d-claim-relayer.key", operator))
		if err := atomicWrite(claimKeyPath, []byte("0x"+claimKey.PrivateKeyHex+"\n"), 0o600); err != nil {
			return err
		}
	}
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		v := cloneMap(base)
		v["miner_id"] = i
		v["operator_no_id"] = operatorForMiner(cfg, i)
		v["state_dir"] = filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", i), "state")
		v["client_id"] = "0x" + roles.Clients[fmt.Sprintf("miner-%d", i)].ClientIDHex
		v["client_key_seed_ref"] = "runtime-secret://roles.json#clients/miner"
		v["payout_coldkey"] = "0x" + roles.Substrate[fmt.Sprintf("miner-%d-payout", i)].PublicKeyHex
		b, _ := yaml.Marshal(v)
		if err := atomicWrite(filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", i), "miner.yml"), b, 0o600); err != nil {
			return err
		}
		operator := v["operator_no_id"].(int)
		claimKeyPath := filepath.Join(stateDir, "secrets", fmt.Sprintf("operator-%d-claim-relayer.key", operator))
		claim := map[string]any{
			"schema_version":  1,
			"release":         "1.0",
			"api_url":         fmt.Sprintf("http://127.0.0.1:%d", 18080+v["operator_no_id"].(int)),
			"rpc":             []string{evmHTTP(workloadRPCAuthority())},
			"key_file":        claimKeyPath,
			"jwt_file":        filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", i), "state", "jwt"),
			"state_dir":       filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", i), "claims"),
			"poll_seconds":    claimPollSeconds(cfg),
			"lookback_epochs": cfg.Policy.Settlement.ClaimTTLEpochs + cfg.Policy.Settlement.ClaimGraceEpochs + 1,
		}
		b, _ = yaml.Marshal(claim)
		if err := atomicWrite(filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", i), "claim-daemon.yml"), b, 0o600); err != nil {
			return err
		}
	}
	minersPerSwarm := cfg.Config.Topology.Miners / cfg.Config.Topology.MinerSwarmProcesses
	for swarm := 1; swarm <= cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		config := minercomponent.ProviderSwarmConfig{
			Schema: minercomponent.ProviderSwarmSchema, ListenAddress: fmt.Sprintf("127.0.0.1:%d", 21080+swarm),
		}
		first := (swarm-1)*minersPerSwarm + 1
		last := swarm * minersPerSwarm
		for miner := first; miner <= last; miner++ {
			operator := operatorForMiner(cfg, miner)
			config.Members = append(config.Members, minercomponent.ProviderSwarmMember{
				ID: fmt.Sprintf("miner-%d", miner), APIURL: fmt.Sprintf("http://127.0.0.1:%d", 18080+operator),
				ConnectURL:  fmt.Sprintf("ws://%s:%d", operatorConnectHostIP(operator), 19080+operator),
				DNSPumpHost: operatorConnectHostIP(operator),
				StateDir:    filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", miner), "state"),
				Wallet:      roles.Substrate[fmt.Sprintf("miner-%d-payout", miner)].SS58, SourceIP: minerTestEgressSourceIP(miner),
			})
		}
		b, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-swarm-%d", swarm), "swarm.json")
		if err := atomicWrite(path, append(b, '\n'), 0o600); err != nil {
			return err
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		config := minercomponent.ClaimSwarmConfig{Schema: minercomponent.ClaimSwarmSchema, ListenAddress: fmt.Sprintf("127.0.0.1:%d", 22080+operator)}
		for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
			if operatorForMiner(cfg, miner) != operator {
				continue
			}
			config.Members = append(config.Members, minercomponent.ClaimSwarmMember{ID: fmt.Sprintf("miner-%d", miner), ConfigPath: filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", miner), "claim-daemon.yml")})
		}
		if len(config.Members) == 0 {
			return fmt.Errorf("operator %d claim swarm has no miners", operator)
		}
		b, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("claim-relayer-%d", operator), "swarm.json")
		if err := atomicWrite(path, append(b, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// Validator 1 deliberately represents the section 8.3 affiliated-validator
// adversary: it operates NO 1 and therefore must mask that pool and any head
// fleet observed through that NO. Validator 2 remains independent and proves
// that the complete two-NO vector is still produced by an unaffiliated seat.
func controlledNOIDsForValidator(validatorID int) []uint64 {
	if validatorID == 1 {
		return []uint64{1}
	}
	return []uint64{}
}
func operatorDirectory(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, validatorID int) []map[string]any {
	out := make([]map[string]any, 0, cfg.Config.Topology.Operators)
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "operators", fmt.Sprintf("no-%d", i))
		out = append(out, map[string]any{
			"no_id":                i,
			"api_url":              fmt.Sprintf("http://127.0.0.1:%d", 18080+i),
			"connect_url":          fmt.Sprintf("ws://%s:%d", operatorConnectHostIP(i), 19080+i),
			"artifact_signer":      roles.EVM[fmt.Sprintf("operator-%d-artifact", i)].Address,
			"state_dir":            root,
			"network_jwt_file":     filepath.Join(root, "network.jwt"),
			"client_jwt_file":      filepath.Join(root, "client.jwt"),
			"client_key_seed_file": filepath.Join(root, "client.key"),
			"concurrency":          4,
		})
	}
	return out
}

func hyperparameterUint64(value any) uint64 {
	switch v := value.(type) {
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	case uint:
		return uint64(v)
	case int:
		if v >= 0 {
			return uint64(v)
		}
	case int8:
		if v >= 0 {
			return uint64(v)
		}
	case int16:
		if v >= 0 {
			return uint64(v)
		}
	case int32:
		if v >= 0 {
			return uint64(v)
		}
	case int64:
		if v >= 0 {
			return uint64(v)
		}
	case float64:
		if v >= 0 {
			return uint64(v)
		}
	}
	return 0
}
func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func substrateWS(authority string) string { w, _, _ := authorityURLs(authority); return w }
