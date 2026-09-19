package main

// Launch preparation collects independent failures before the one action
// gate. It may prepare local inputs and services, but never submits an action.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type launchPreparationReport struct {
	Schema               string        `json:"schema"`
	Command              string        `json:"command"`
	PlanHash             string        `json:"plan_hash"`
	PrepareOnly          bool          `json:"prepare_only"`
	StoppedBeforeActions bool          `json:"stopped_before_actions"`
	Doctor               *DoctorReport `json:"doctor,omitempty"`
	Checks               []Check       `json:"checks"`
	Ready                bool          `json:"ready"`
}

func validateLaunchPreparationOptions(command string, options cliOptions) error {
	if !options.PrepareOnly {
		return nil
	}
	if command != "setup" && command != "launch" && command != "resume" {
		return errors.New("--prepare-only requires setup, launch or resume")
	}
	if !options.Apply || !validCanonicalHashHex(options.PlanHash) {
		return errors.New("--prepare-only requires --apply and the exact approved --plan-hash for reversible host preparation")
	}
	return nil
}

// Each named stage keeps its full error, including ordered action failures.
func (self *launchPreparationReport) add(name string, err error) bool {
	check := Check{Name: name, OK: err == nil, Hard: true}
	if err != nil {
		check.Detail = err.Error()
		self.Ready = false
	}
	self.Checks = append(self.Checks, check)
	return err == nil
}

func (self *launchPreparationReport) blocked(name string, dependencies ...string) {
	self.add(name, fmt.Errorf("blocked by %s", strings.Join(dependencies, ", ")))
}

func (self *launchPreparationReport) Error() error {
	var failures []error
	for _, check := range self.Checks {
		if check.Hard && !check.OK {
			failures = append(failures, fmt.Errorf("%s: %s", check.Name, check.Detail))
		}
	}
	return errors.Join(failures...)
}

// Completed setup rendering must not make a future launch-only history
// adoption a prerequisite of an already approved repair transaction.
func collectLaunchRuntimePreparation(report *launchPreparationReport, command string, executor *Executor) {
	if report == nil || executor == nil || executor.plan == nil {
		return
	}
	if command != "launch" && command != "resume" {
		action, err := executor.planAction("config.render")
		if err != nil {
			report.Checks = append(report.Checks, Check{Name: "launch-runtime-inputs", Hard: false, Detail: "deferred to launch/resume: config.render is absent"})
			return
		}
		if _, verified := executor.verifiedActionEntry(action); verified {
			report.Checks = append(report.Checks, Check{Name: "launch-runtime-inputs", Hard: false, Detail: "deferred to launch/resume: config.render is already verified; future namespace/adoption authority does not block approved setup repair"})
			return
		}
		var pending []string
		for _, dependency := range action.DependsOn {
			if !executor.actionVerified(dependency) {
				pending = append(pending, dependency)
			}
		}
		if len(pending) != 0 {
			report.Checks = append(report.Checks, Check{Name: "launch-runtime-inputs", Hard: false, Detail: "blocked until approved setup prerequisites complete: " + strings.Join(pending, ", ")})
			return
		}
	}
	cfg, stateDir := executor.cfg, executor.stateDir
	report.add("signed-attempt-namespaces", preflightSignedAttemptStateNamespaces(cfg, stateDir))
	report.add("runtime-evidence-references", preflightRuntimeEvidenceV2(cfg, stateDir))
	report.add("operator-api-origins", validateRuntimeOperatorApiOrigins(cfg))
	contracts, err := loadContractDeployment(stateDir)
	if !report.add("runtime-deployment-inputs", err) {
		report.blocked("reserved-attempt-uploads", "runtime-deployment-inputs")
		return
	}
	_, err = runtimeReservedAttemptUploads(cfg, stateDir, contracts)
	report.add("reserved-attempt-uploads", err)
}

// A successful preparation-only run also stops here. The action callback is
// reachable only after every prerequisite passed and apply was requested.
func finishLaunchPreparation(report *launchPreparationReport, publish func(*launchPreparationReport, error) error, apply func() error) error {
	if report == nil || publish == nil || apply == nil {
		return errors.New("launch preparation gate is incomplete")
	}
	err := report.Error()
	if err != nil || report.PrepareOnly {
		report.StoppedBeforeActions = true
		return publish(report, err)
	}
	return apply()
}

// Transaction dispatch deliberately keeps runOrderedConcurrentAudits. This
// read-only counterpart completes every independent batch in canonical order;
// cancellation prevents new reads and records the remaining work as blocked.
func collectOrderedReadOnlyAudits(ctx context.Context, count, workers int, audit func(int) error) []error {
	if ctx == nil || count < 0 || workers <= 0 || audit == nil {
		return []error{errors.New("read-only audit configuration is invalid")}
	}
	errs := make([]error, count)
	for first := 0; first < count; first += workers {
		last := min(first+workers, count)
		var wait sync.WaitGroup
		for index := first; index < last; index++ {
			if err := ctx.Err(); err != nil {
				errs[index] = fmt.Errorf("audit %d blocked by canceled preparation: %w", index, err)
				continue
			}
			wait.Add(1)
			go func(index int) { defer wait.Done(); errs[index] = audit(index) }(index)
		}
		wait.Wait()
	}
	return errs
}

// Even an approved deployment envelope may need a canonical nonce for role
// promotion or a CREATE receipt for event-boundary reconciliation.
func (self *Executor) preparationPayloadReadersError() error {
	if self == nil || self.deployer == nil || self.deployer.client == nil {
		return errors.New("blocked by deployer EVM reader")
	}
	if self.preparationIncomplete && independentRPCRequired(self.cfg) && self.independentEVM == nil {
		return errors.New("blocked by independent EVM reader")
	}
	return nil
}

// Open each independent read owner even if another owner is unavailable. A
// partial executor is useful only for preparation; the aggregate failure must
// pass through finishLaunchPreparation before any transaction callback.
func newLaunchPreparationExecutor(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, journal *Journal, roles *RoleSecrets) (*Executor, error) {
	runtimeCfg := cfg
	if cfg != nil && cfg.provisionalRPCAuthority != "" {
		var err error
		runtimeCfg, err = campaignRPCConfig(cfg)
		if err != nil {
			return nil, err
		}
	}
	if err := validateOwnedRPCPlan(cfg, plan); err != nil {
		return nil, err
	}
	if err := validateRuntimeConfigIdentityPlan(cfg, plan); err != nil {
		return nil, err
	}
	if err := validateExecutionRPCConfiguration(cfg); err != nil {
		return nil, fmt.Errorf("execution RPC configuration: %w", err)
	}
	if err := validateCampaignRPCTransport(cfg, runtimeCfg); err != nil {
		return nil, err
	}
	self := &Executor{cfg: runtimeCfg, stateDir: stateDir, plan: plan, journal: journal, roles: roles, deposits: map[int]*EvmTxManager{}, auditAuthorizedConfig: cfg}
	var failures []error
	var err error
	self.substrate, err = DialSubstrateManagerContext(ctx, runtimeCfg, stateDir, journal)
	if err != nil {
		failures = append(failures, fmt.Errorf("operational native reader: %w", err))
	}
	if independentRPCRequired(runtimeCfg) {
		self.independentSubstrate, err = DialIndependentSubstrateManager(runtimeCfg)
		if err != nil {
			failures = append(failures, fmt.Errorf("independent native reader: %w", err))
		}
		self.independentEVM, err = dialConfiguredEVMClient(ctx, runtimeCfg, verificationEVMEndpoint(runtimeCfg))
		if err == nil {
			chainId, chainErr := self.independentEVM.ChainID(ctx)
			if chainErr != nil {
				err = chainErr
			} else if chainId.Uint64() != testnetChainID {
				err = fmt.Errorf("independent EVM chain id=%d, want %d", chainId.Uint64(), testnetChainID)
			}
			if err != nil {
				self.independentEVM.Close()
				self.independentEVM = nil
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("independent EVM reader: %w", err))
		}
	}
	if roles == nil {
		failures = append(failures, errors.New("EVM readers blocked by role secrets"))
		self.preparationIncomplete = true
		return self, errors.Join(failures...)
	}
	for _, role := range []struct {
		name   string
		target **EvmTxManager
	}{
		{name: "deployer", target: &self.deployer}, {name: "testnet-owner", target: &self.owner},
		{name: "guardian", target: &self.guardian}, {name: "commitment-oracle", target: &self.oracle}, {name: "keeper", target: &self.keeper},
	} {
		*role.target, err = DialEvmTxManager(ctx, runtimeCfg, stateDir, journal, roles, role.name)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s EVM reader: %w", role.name, err))
		}
	}
	for operator := 1; operator <= runtimeCfg.Config.Topology.Operators; operator++ {
		role := fmt.Sprintf("operator-%d-deposit", operator)
		manager, err := DialEvmTxManager(ctx, runtimeCfg, stateDir, journal, roles, role)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s EVM reader: %w", role, err))
			continue
		}
		self.deposits[operator] = manager
	}
	self.preparationIncomplete = len(failures) != 0
	return self, errors.Join(failures...)
}
