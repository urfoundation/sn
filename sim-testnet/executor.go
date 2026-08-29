package main

import (
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
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/crv4"
	minercomponent "github.com/urfoundation/sn/miner"
	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

type Executor struct {
	cfg                  *ResolvedConfig
	stateDir             string
	plan                 *SetupPlan
	journal              *Journal
	roles                *RoleSecrets
	substrate            *SubstrateManager
	independentSubstrate *SubstrateManager
	independentEVM       *ethclient.Client
	deployer, owner      *EVMTxManager
	guardian             *EVMTxManager
	oracle, keeper       *EVMTxManager
	deposits             map[int]*EVMTxManager
	payloads             *DeploymentPayloads
}

func NewExecutor(ctx context.Context, cfg *ResolvedConfig, stateDir string, p *SetupPlan, j *Journal, roles *RoleSecrets) (*Executor, error) {
	if err := validateExecutionRPCConfiguration(cfg); err != nil {
		return nil, fmt.Errorf("execution RPC configuration: %w", err)
	}
	s, err := DialSubstrateManager(cfg, stateDir, j)
	if err != nil {
		return nil, err
	}
	d, err := DialEVMTxManager(ctx, cfg, stateDir, j, roles, "deployer")
	if err != nil {
		s.Close()
		return nil, err
	}
	o, err := DialEVMTxManager(ctx, cfg, stateDir, j, roles, "testnet-owner")
	if err != nil {
		s.Close()
		d.Close()
		return nil, err
	}
	guardian, err := DialEVMTxManager(ctx, cfg, stateDir, j, roles, "guardian")
	if err != nil {
		s.Close()
		d.Close()
		o.Close()
		return nil, err
	}
	oracle, err := DialEVMTxManager(ctx, cfg, stateDir, j, roles, "commitment-oracle")
	if err != nil {
		s.Close()
		d.Close()
		o.Close()
		guardian.Close()
		return nil, err
	}
	keeper, err := DialEVMTxManager(ctx, cfg, stateDir, j, roles, "keeper")
	if err != nil {
		s.Close()
		d.Close()
		o.Close()
		guardian.Close()
		oracle.Close()
		return nil, err
	}
	deposits := map[int]*EVMTxManager{}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		manager, dialErr := DialEVMTxManager(ctx, cfg, stateDir, j, roles, fmt.Sprintf("operator-%d-deposit", i))
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
	e := &Executor{cfg: cfg, stateDir: stateDir, plan: p, journal: j, roles: roles, substrate: s, deployer: d, owner: o, guardian: guardian, oracle: oracle, keeper: keeper, deposits: deposits}
	if !independentRPCRequired(cfg) {
		return e, nil
	}
	e.independentSubstrate, err = DialIndependentSubstrateManager(cfg)
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("independent Substrate RPC: %w", err)
	}
	e.independentEVM, err = dialConfiguredEVMClient(ctx, cfg, cfg.Public.Chain.EVMPublicReadEndpoint)
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
		if !verified[action.ID+"\x00"+action.IntentHash] || action.Kind == "budget-reserve" {
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

// Contract registration receives the full approved ceiling. Runtime 451 burns
// the current rao price and the release contracts return any surplus.
func registrationFundingWei(limitRao uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(limitRao), big.NewInt(1_000_000_000))
}

func runMutation(ctx context.Context, cmd string, cfg *ResolvedConfig, stateDir string, o cliOptions) error {
	if cmd == "retire" {
		return runRetirement(ctx, cfg, stateDir, o)
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
	doctor := runDoctor(ctx, cfg, &doctorPlanBudget{Plan: p, Remaining: remaining})
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
		if err := preflightReleaseHost(stateDir, cfg); err != nil {
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
		return RunScenario(ctx, cfg, stateDir, o.Name, j, ex)
	}
	for _, a := range p.Actions {
		if err := ex.Execute(ctx, a); err != nil {
			return err
		}
		if a.ID == limitID {
			break
		}
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
	if p.Schema != "urnetwork-sim-plan-v3" || p.Release != "1.0" || p.ReleaseLockHash == "" || p.ReleaseLockHash != releaseLockHash || p.ResolvedInputsHash == "" || p.ResolvedInputsHash != resolvedHash || p.DeploymentID != cfg.Config.Deployment.DeploymentID || p.ChainID != testnetChainID || p.GenesisHash != testnetGenesis || p.Netuid != cfg.Netuid || p.ConfigHash != cfg.ConfigHash || p.PolicyHash != cfg.PolicyHash || p.Limits != configuredPlanLimits(cfg) || p.RegistrationBurnLimitRao != cfg.Config.Budgets.MaximumRegistrationBurnRao || p.NativeTransactionFeeLimitRao != cfg.Config.Budgets.MaximumNativeTransactionFeeRao || p.MaximumEVMFeePerGasWei != cfg.Config.Budgets.MaximumEVMFeePerGasWei || p.BootstrapBurnHalfLifeBlocks != bootstrapBurnHalfLife || p.ProductionBurnHalfLifeBlocks != productionBurnHalfLife {
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
	b, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		return nil, err
	}
	var p SetupPlan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("persisted setup plan: %w", err)
	}
	if p.Schema != "urnetwork-sim-plan-v1" && p.Schema != "urnetwork-sim-plan-v2" && p.Schema != "urnetwork-sim-plan-v3" {
		return nil, fmt.Errorf("persisted setup plan has unsupported schema %q", p.Schema)
	}
	want := p.PlanHash
	got, err := p.hash()
	if err != nil {
		return nil, err
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
	if prior, priorErr := readPersistedPlan(stateDir); priorErr == nil && prior.PlanHash != p.PlanHash {
		priorBytes, marshalErr := json.MarshalIndent(prior, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(prior.PlanHash)+".json"), append(priorBytes, '\n'), 0o600); err != nil {
			return err
		}
	} else if priorErr != nil && !errors.Is(priorErr, os.ErrNotExist) {
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
	default:
		return true
	}
}

func (e *Executor) consumedActionTransaction(action Action, verified JournalEntry) (JournalEntry, error) {
	if action.Kind != "substrate-extrinsic" {
		return JournalEntry{}, fmt.Errorf("consumed action %s is not a native extrinsic", action.ID)
	}
	if verified.Stage != StageVerified || verified.ActionID != action.ID || verified.IntentHash != action.IntentHash || !e.plan.allowedPlanHashes()[verified.PlanHash] {
		return JournalEntry{}, fmt.Errorf("consumed action %s has invalid verified plan evidence", action.ID)
	}
	transaction, ok := e.journal.LatestTransaction(verified.PlanHash, action.ID, action.IntentHash)
	if !ok || transaction.Stage != StageFinalized {
		return JournalEntry{}, fmt.Errorf("consumed action %s has no finalized transaction evidence", action.ID)
	}
	return transaction, nil
}

func (e *Executor) verifyConsumedActionHistory(action Action, verified JournalEntry) error {
	transaction, err := e.consumedActionTransaction(action, verified)
	if err != nil {
		return err
	}
	return e.verifySubstrateTransactionEvidence(
		ChainHead{Number: transaction.BlockNumber, Hash: transaction.BlockHash},
		transaction.TransactionHash,
	)
}

// Revalidate one terminal action using live state where it still holds, then
// fall back to its finalized transaction only for intentionally consumable
// point-in-time balances. This also supports convergence actions that adopted
// a balance already present and therefore have no transaction of their own.
func (e *Executor) verifyVerifiedActionState(ctx context.Context, action Action, verified JournalEntry) error {
	if err := e.verifyPersistedPostcondition(verified); err != nil {
		return fmt.Errorf("persisted postcondition: %w", err)
	}
	if actionRequiresCurrentPostcondition(action) {
		if _, err := e.verifyActionPostcondition(ctx, action); err != nil {
			return fmt.Errorf("current postcondition: %w", err)
		}
		return nil
	}
	_, currentErr := e.verifyActionPostcondition(ctx, action)
	if currentErr == nil {
		return nil
	}
	if historyErr := e.verifyConsumedActionHistory(action, verified); historyErr != nil {
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
		err, _ := verifySubnetOwner(e.substrate.chain, e.cfg.Netuid, e.cfg.WalletPublic)
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
	case strings.HasPrefix(a.ID, "production.hyperparameter."):
		return e.setProductionHyperparameter(ctx, a, strings.TrimPrefix(a.ID, "production.hyperparameter."))
	case strings.HasPrefix(a.ID, "operator.retire."):
		return e.scheduleOperatorRetirement(ctx, a)
	case strings.HasPrefix(a.ID, "evm.fund-"):
		return e.fundEVM(ctx, a)
	case strings.HasPrefix(a.ID, "operator.deposit.register."):
		return e.registerDepositHotkey(ctx, a, suffixInt(a.ID))
	case a.ID == "evm.reserve-sink" || a.ID == "evm.settlement-vault" || a.ID == "evm.coordinator-implementation" || a.ID == "evm.vault-register-escrow" || a.ID == "evm.coordinator-proxy" || a.ID == "evm.governance-drill-implementation" || a.ID == "evm.vault-fix-coordinator" || a.ID == "evm.sink-fix-recorder" || a.ID == "precompile.probe-deploy":
		return e.executeDeployment(ctx, a)
	case strings.HasPrefix(a.ID, "precompile."):
		return e.executePrecompileConformance(ctx, a)
	case strings.HasPrefix(a.ID, "governance."):
		return e.executeGovernanceDrillAction(ctx, a)
	case strings.HasPrefix(a.ID, "operator.register."):
		return e.registerOperator(ctx, a)
	case strings.HasPrefix(a.ID, "fleet.fund."):
		fleet := suffixInt(a.ID)
		return e.fundSubstrateRole(ctx, a, fleetColdkeyLabel(fleet))
	case strings.HasPrefix(a.ID, "fleet.fund-hotkey."):
		fleet := suffixInt(a.ID)
		return e.fundSubstrateRole(ctx, a, fleetHotkeyLabel(fleet))
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
		return e.mirrorFleetCommitment(ctx, a, suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "fleet.bind."):
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
	case strings.HasPrefix(a.ID, "alpha.transfer.operator-deposit."):
		return e.transferAlpha(ctx, a, "operator-deposit", suffixInt(a.ID))
	case strings.HasPrefix(a.ID, "alpha.transfer.validator."):
		return e.transferAlpha(ctx, a, "validator", suffixInt(a.ID))
	case a.ID == "campaign.voluntary-conviction.1":
		return e.addVoluntaryConviction(ctx, a)
	case a.ID == dishonestDepositActionID:
		return e.executeDishonestDeposit(ctx, a)
	case a.Kind == "budget-reserve":
		return nil
	case a.ID == "config.render":
		return RenderRuntimeConfigs(e.cfg, e.stateDir, e.roles)
	case a.ID == "topology.launch":
		return nil
	case a.ID == "churn.tournament-complete":
		return nil
	default:
		return fmt.Errorf("no executor for %s", a.ID)
	}
}

type ProductionPolicyEvidence struct {
	Schema                 string `json:"schema"`
	DeploymentID           string `json:"deployment_id"`
	PolicyHash             string `json:"policy_hash"`
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
	if p.AfterAcceleratedEpochs == ^uint64(0) {
		return false
	}
	return policy.PolicyHash == wantHash && policy.EffectiveEpoch == p.AfterAcceleratedEpochs+1 && policy.EffectiveBlock != 0 &&
		policy.EpochBlocks == p.EpochBlocks && policy.RootCommitWindowBlocks == p.RootCommitWindowBlocks &&
		policy.FinalizeOffsetBlocks == p.FinalizeOffsetBlocks && policy.CloseGraceBlocks == p.CloseGraceBlocks &&
		policy.ClaimTTLEpochs == cfg.Policy.Settlement.ClaimTTLEpochs && policy.ClaimGraceEpochs == cfg.Policy.Settlement.ClaimGraceEpochs &&
		policy.MaximumBindingValidityEpochs == cfg.Policy.Binding.MaximumValidityEpochs && policy.CommitmentMaxAgeBlocks == p.EpochBlocks*2 &&
		policy.EpochDepositCapRao.Cmp(new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator)) == 0 &&
		policy.CampaignDepositCapRao.Cmp(new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao)) == 0
}

// validateProductionScheduleEpoch distinguishes a first write from adoption
// after an interrupted write. A new schedule is approval-bound to the exact
// end of the accelerated campaign; an already canonical on-chain policy may be
// adopted later, but never before that gate was reached.
func validateProductionScheduleEpoch(currentEpoch, afterAcceleratedEpochs uint64, alreadyScheduled bool) error {
	if currentEpoch < afterAcceleratedEpochs {
		return fmt.Errorf("production cadence requires %d reconciled accelerated epochs; current epoch is %d", afterAcceleratedEpochs, currentEpoch)
	}
	if !alreadyScheduled && currentEpoch != afterAcceleratedEpochs {
		return fmt.Errorf("production cadence must be scheduled at exact epoch %d; current epoch %d exceeds the approved campaign", afterAcceleratedEpochs, currentEpoch)
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

func (e *Executor) scheduleProductionPolicy(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	address := e.payloads.Manifest.CoordinatorProxy
	epochValues, err := contractCall(ctx, e.owner.client, address, parsed, "currentEpoch")
	if err != nil || len(epochValues) != 1 {
		return fmt.Errorf("read current epoch: %w", err)
	}
	current, ok := epochValues[0].(*big.Int)
	if !ok || !current.IsUint64() {
		return fmt.Errorf("currentEpoch returned %T", epochValues[0])
	}
	currentEpoch := current.Uint64()
	p := e.cfg.Policy.ProductionCadence
	countValues, err := contractCall(ctx, e.owner.client, address, parsed, "policyCount")
	if err != nil || len(countValues) != 1 {
		return fmt.Errorf("read policy count: %w", err)
	}
	count, ok := countValues[0].(*big.Int)
	if !ok || !count.IsUint64() || count.Sign() == 0 {
		return fmt.Errorf("policyCount returned %T", countValues[0])
	}
	alreadyScheduled := count.Uint64() > 1
	if err := validateProductionScheduleEpoch(currentEpoch, p.AfterAcceleratedEpochs, alreadyScheduled); err != nil {
		return err
	}
	// A resumed command must adopt the one exact previously scheduled cadence;
	// any additional or different policy is an approval-breaking condition.
	if alreadyScheduled {
		if count.Uint64() != 2 {
			return fmt.Errorf("coordinator has %d policy versions, expected exactly initial plus production", count.Uint64())
		}
		values, readErr := contractCall(ctx, e.owner.client, address, parsed, "policyByIndex", big.NewInt(1))
		if readErr != nil {
			return readErr
		}
		scheduled, convertErr := coordinatorPolicy(values)
		if convertErr != nil || !productionPolicyMatches(e.cfg, scheduled) {
			return errors.New("existing second policy is not the canonical production cadence")
		}
		return e.writeProductionPolicyEvidence(scheduled, scheduled.EffectiveEpoch-1, nil)
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
	if currentEpoch == ^uint64(0) || p.EpochBlocks > ^uint64(0)/2 {
		return errors.New("production policy arithmetic overflows uint64")
	}
	hash, err := decodeHash(e.cfg.PolicyHash)
	if err != nil {
		return err
	}
	next := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: currentEpoch + 1,
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
	return e.writeProductionPolicyEvidence(post, currentEpoch, receipt)
}

func (e *Executor) writeProductionPolicyEvidence(policy stabi.STCoordinatorPolicySnapshot, scheduledFrom uint64, receipt *ethTypes.Receipt) error {
	evidence := ProductionPolicyEvidence{
		Schema: "urnetwork-production-policy-evidence-v1", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
		PolicyHash: e.cfg.PolicyHash, ScheduledFromEpoch: scheduledFrom, EffectiveEpoch: policy.EffectiveEpoch,
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

func (e *Executor) ensurePayloads(ctx context.Context) error {
	if e.payloads != nil {
		return nil
	}
	if existing, err := loadContractDeployment(e.stateDir); err == nil {
		p, err := buildDeploymentPayloads(e.cfg, e.roles, existing.InitialNonce)
		if err != nil {
			return err
		}
		if p.Manifest.ReserveSink != existing.ReserveSink || p.Manifest.CoordinatorProxy != existing.CoordinatorProxy || p.Manifest.GovernanceDrillImplementation != existing.GovernanceDrillImplementation || p.Manifest.PrecompileProbe != existing.PrecompileProbe {
			return fmt.Errorf("contract deployment manifest/address mismatch")
		}
		p.Manifest.DeployBlock = existing.DeployBlock
		p.Manifest.DeployBlockHash = existing.DeployBlockHash
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
	e.payloads = p
	return saveContractDeployment(e.stateDir, p.Manifest)
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
		addr = p.Manifest.PrecompileProbe
		data = p.PrecompileProbe
	case "evm.vault-fix-coordinator":
		addr = p.Manifest.SettlementVault
		to = &addr
		data = p.FixVault
	case "evm.sink-fix-recorder":
		addr = p.Manifest.ReserveSink
		to = &addr
		data = p.FixSink
	}
	if to == nil {
		head, headErr := finalizedEVMHead(ctx, e.deployer.client)
		if headErr != nil {
			return headErr
		}
		code, err := e.deployer.client.CodeAt(ctx, addr, new(big.Int).SetUint64(head.Number))
		if err == nil && len(code) > 0 {
			if string(code) != string(p.ExpectedRuntime[addr]) {
				return fmt.Errorf("unexpected existing code at %s", addr)
			}
			return nil
		}
	}
	receipt, err := e.deployer.Send(ctx, e.plan.PlanHash, a, to, value, data)
	if err != nil {
		return err
	}
	if to == nil && receipt.ContractAddress != addr {
		return fmt.Errorf("deployed %s, predicted %s", receipt.ContractAddress, addr)
	}
	if receipt.BlockNumber.Uint64() > p.Manifest.DeployBlock {
		p.Manifest.DeployBlock = receipt.BlockNumber.Uint64()
		p.Manifest.DeployBlockHash = receipt.BlockHash.Hex()
	}
	if a.ID == "evm.governance-drill-implementation" {
		deployed := make(map[common.Address][]byte, len(p.ExpectedRuntime)-1)
		for address, runtime := range p.ExpectedRuntime {
			if address != p.Manifest.PrecompileProbe {
				deployed[address] = runtime
			}
		}
		hashes, err := verifyRuntimeCode(ctx, e.deployer.client, deployed)
		if err != nil {
			return err
		}
		p.Manifest.RuntimeHashes = hashes
	}
	if a.ID == "precompile.probe-deploy" {
		hashes, err := verifyRuntimeCode(ctx, e.deployer.client, p.ExpectedRuntime)
		if err != nil {
			return err
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
	epochOut, err := contractCall(ctx, e.owner.client, e.payloads.Manifest.CoordinatorProxy, coordABI, "currentEpoch")
	if err != nil {
		return err
	}
	epoch := epochOut[0].(*big.Int).Uint64()
	cold, err := roleBytes32(e.roles, fmt.Sprintf("operator-%d-coldkey", n))
	if err != nil {
		return err
	}
	pool, err := roleBytes32(e.roles, fmt.Sprintf("operator-%d-pool-hotkey", n))
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
		return fmt.Errorf("read current epoch at finalized head: %w", err)
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
	prior, hasPrior := e.journal.LatestTransaction(e.plan.PlanHash, a.ID, a.IntentHash)
	if added, readErr := readAdded(head.Number, epoch); readErr != nil {
		return readErr
	} else if added.Sign() != 0 && !hasPrior {
		return fmt.Errorf("epoch %s unexpectedly already has %s voluntary conviction for no 1", epoch, added)
	}
	nonceValues, err := contractCallAt(ctx, manager.client, address, parsed, "nextDepositNonce", head.Number, big.NewInt(1))
	if err != nil || len(nonceValues) != 1 {
		return fmt.Errorf("read voluntary conviction nonce: %w", err)
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
			return fmt.Errorf("decode ConvictionAdded event: %w", unpackErr)
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
	readConviction := func(block uint64) (*big.Int, error) {
		values, readErr := contractCallAt(ctx, manager.client, address, parsed, "cumulativeConviction", block, big.NewInt(1))
		if readErr != nil || len(values) != 1 {
			return nil, fmt.Errorf("read cumulative conviction at block %d: %w", block, readErr)
		}
		value, valueOK := values[0].(*big.Int)
		if !valueOK {
			return nil, fmt.Errorf("cumulativeConviction returned %T", values[0])
		}
		return value, nil
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
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		return 0, err
	}
	head, err := finalizedEVMHead(ctx, e.deployer.client)
	if err != nil {
		return 0, err
	}
	values, err := contractCallAt(ctx, e.deployer.client, stakingPrecompileAddress, parsed, "getStake", head.Number, hotkey, coldkey, new(big.Int).SetUint64(uint64(e.cfg.Netuid)))
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
	current, err := e.readStakeFinalized(ctx, destinationHotkey, destinationColdkey)
	if err != nil {
		return err
	}
	if current >= a.Spend.AlphaRao {
		return nil
	}
	amount := a.Spend.AlphaRao - current
	call, err := e.substrate.TransferStakeAndHotkeyCall(destinationColdkey, source, destinationHotkey, amount)
	if err != nil {
		return err
	}
	if _, _, err := e.substrate.Send(ctx, e.plan.PlanHash, a, call); err != nil {
		return err
	}
	post, err := e.readStakeFinalized(ctx, destinationHotkey, destinationColdkey)
	if err != nil {
		return err
	}
	if post < a.Spend.AlphaRao {
		return fmt.Errorf("alpha transfer post-state %d, want at least %d", post, a.Spend.AlphaRao)
	}
	return nil
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
		return fmt.Errorf("runtime-451 would prune UID %d, not approved churn-floor UID %d", state.RuntimePruneUID, state.ExpectedUID)
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
	if e == nil || e.plan == nil || e.journal == nil {
		return JournalEntry{}, false
	}
	allowedPlans := e.plan.allowedPlanHashes()
	entries := e.journal.Entries()
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if allowedPlans[entry.PlanHash] && entry.ActionID == action.ID && entry.IntentHash == action.IntentHash && entry.Stage == StageVerified {
			return entry, true
		}
	}
	return JournalEntry{}, false
}

// Verify every exact ancestor intent before the revised plan performs its
// first mutation. This prevents a missing receipt, stale live postcondition,
// or unavailable finalized transaction from being discovered only after new
// funding has already been submitted.
func (e *Executor) verifyCarriedActionHistory(ctx context.Context) error {
	if e == nil || e.plan == nil || e.journal == nil {
		return errors.New("plan/journal is unavailable")
	}
	for _, action := range e.plan.Actions {
		entry, ok := e.verifiedActionEntry(action)
		if !ok || entry.PlanHash == e.plan.PlanHash {
			continue
		}
		if err := e.verifyVerifiedActionState(ctx, action, entry); err != nil {
			return fmt.Errorf("action %s: %w", action.ID, err)
		}
	}
	return nil
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

func (e *Executor) readChallengerChurnState(fleet int) (challengerChurnState, error) {
	challenger := fleet - e.cfg.Config.Topology.HeadFleets
	if challenger < 1 || challenger > e.cfg.Config.Topology.ChallengerFleets || challenger > e.cfg.Config.Topology.ChurnFloorUIDs {
		return challengerChurnState{}, fmt.Errorf("fleet %d is not a bounded challenger", fleet)
	}
	expectedUID, err := e.registrationPostconditionUID(fmt.Sprintf("churn.register.%d", challenger))
	if err != nil {
		return challengerChurnState{}, err
	}
	challengerHotkey, err := roleBytes32(e.roles, fleetHotkeyLabel(fleet))
	if err != nil {
		return challengerChurnState{}, err
	}
	churnHotkey, err := roleBytes32(e.roles, churnHotkeyLabel(challenger))
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
	pruneUID, err := e.substrate.Runtime451PruneCandidate()
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
	contracts, err := loadContractDeployment(stateDir)
	if err != nil {
		return err
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", i))
		vaultRoot := filepath.Join(root, "vault")
		if err := copyTree(filepath.Join(cfg.Repos.Vault, "local"), vaultRoot, 0o600); err != nil {
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
			"testnet-authority":                          workloadRPCAuthority(),
			"testnet-rpc-urls":                           []string{evmHTTP(workloadRPCAuthority())},
			"testnet-chain-id":                           testnetChainID,
			"testnet-genesis-hash":                       testnetGenesis,
			"testnet-deployment-id":                      cfg.Config.Deployment.DeploymentID,
			"testnet-policy-hash":                        cfg.PolicyHash,
			"testnet-coordinator-address":                contracts.CoordinatorProxy.Hex(),
			"testnet-settlement-vault-address":           contracts.SettlementVault.Hex(),
			"testnet-reserve-sink-address":               contracts.ReserveSink.Hex(),
			"testnet-deploy-block":                       contracts.DeployBlock,
			"testnet-netuid":                             cfg.Netuid,
			"testnet-no-id":                              i,
			"testnet-treasury-hotkey":                    "0x" + roles.Substrate[fmt.Sprintf("operator-%d-pool-hotkey", i)].PublicKeyHex,
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
		settings := []byte("env_vars:\n  URNETWORK_ST_PROFILE: testnet\n")
		if err := atomicWrite(filepath.Join(site, "settings.yml"), settings, 0o600); err != nil {
			return err
		}
	}
	return renderValidatorMinerConfigs(cfg, stateDir, roles, contracts)
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
	var value map[string]any
	if err := strictYAML(filepath.Join(cfg.Repos.Vault, "main", "minio.yml"), &value); err != nil {
		return err
	}
	if fmt.Sprint(value["bucket"]) != "blob" {
		return fmt.Errorf("minio.yml must select the blob bucket")
	}
	value["prefix"] = filepath.ToSlash(filepath.Join("blob", "sim-testnet", cfg.Config.Deployment.DeploymentID, fmt.Sprintf("operator-%d", operator)))
	b, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return atomicWrite(destination, b, 0o600)
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
	base := map[string]any{"schema_version": 1, "production": true, "release": "1.0", "deployment_id": cfg.Config.Deployment.DeploymentID, "chain_id": testnetChainID, "genesis_hash": testnetGenesis, "runtime_spec": cfg.Public.Chain.ExpectedRuntimeSpec, "netuid": cfg.Netuid, "coordinator": c.CoordinatorProxy.Hex(), "settlement_vault": c.SettlementVault.Hex(), "deploy_block": c.DeployBlock, "policy_hash": cfg.PolicyHash, "rpc": []string{evmHTTP(workloadRPCAuthority())}, "substrate": []string{substrateWS(workloadSubstrateRPCAuthority())}}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		v := cloneMap(base)
		v["validator_id"] = i
		v["state_dir"] = filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", i), "state")
		v["hotkey_seed_file"] = filepath.Join(stateDir, "secrets", fmt.Sprintf("validator-%d-hotkey.seed", i))
		v["controlled_no_ids"] = controlledNOIDsForValidator(i)
		v["trail_depth"] = cfg.Policy.Verify.TrailDepth
		v["poll_seconds"] = 2
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
			"poll_seconds":    10,
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
				ConnectURL: fmt.Sprintf("ws://127.0.0.1:%d", 19080+operator),
				StateDir:   filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", miner), "state"),
				Wallet:     roles.Substrate[fmt.Sprintf("miner-%d-payout", miner)].SS58, SourceIP: minerTestEgressSourceIP(miner),
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
			"connect_url":          fmt.Sprintf("ws://127.0.0.1:%d", 19080+i),
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
