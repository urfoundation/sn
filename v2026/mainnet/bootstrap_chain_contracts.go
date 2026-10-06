// Contract prerequisites expose the approved installation scope and original
// retained progress. No observation grants Safe authority or changes custody.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const bootstrapChainContractReadinessSchema = "urnetwork-mainnet-bootstrap-chain-contract-readiness-v1"

// Receipt observations are historical local custody, never a current canonical
// audit. An approved envelope alone does not verify the final Safe operation.
type bootstrapChainContractAction struct {
	Id                  string            `json:"id"`
	Approved            bool              `json:"approved"`
	SemanticsVerified   bool              `json:"semantics_verified"`
	ExecutorImplemented bool              `json:"executor_implemented"`
	CustodyStatus       string            `json:"custody_status"`
	JournalHash         string            `json:"journal_hash,omitempty"`
	CustodyHash         string            `json:"custody_hash,omitempty"`
	TransactionHash     string            `json:"transaction_hash,omitempty"`
	Attempts            uint8             `json:"attempts"`
	ReceiptObservation  string            `json:"receipt_observation,omitempty"`
	Receipt             *evmCreateReceipt `json:"receipt,omitempty"`
}

// A successor must adopt completed custody, not resubmit it. These requirements
// describe an unimplemented authority transition; they are not an unsigned plan.
type bootstrapContractSuccessorRequirements struct {
	Implemented           bool     `json:"implemented"`
	PredecessorPlanHash   string   `json:"predecessor_plan_hash"`
	PredecessorConfigHash string   `json:"predecessor_config_hash"`
	UnfinishedActions     []string `json:"unfinished_actions"`
	RequiredBindings      []string `json:"required_bindings"`
}

// Plan-only and retained-custody inspections share one report. False authority
// fields remain explicit even when every implemented action has a receipt.
type bootstrapChainContractReadiness struct {
	Schema                           string                                 `json:"schema"`
	PlanHash                         string                                 `json:"plan_hash"`
	ContractPlanHash                 string                                 `json:"contract_plan_hash"`
	Scope                            string                                 `json:"scope"`
	Status                           string                                 `json:"status"`
	PlanInspectionComplete           bool                                   `json:"plan_inspection_complete"`
	CustodyInspectionComplete        bool                                   `json:"custody_inspection_complete"`
	LocalPreparation                 *bootstrapChainReadinessState          `json:"local_preparation,omitempty"`
	Actions                          []bootstrapChainContractAction         `json:"actions"`
	OriginalMaximumAttempts          uint8                                  `json:"original_maximum_attempts"`
	MinimumFreshInstallationAttempts uint16                                 `json:"minimum_fresh_installation_attempts"`
	RetainedAttempts                 uint16                                 `json:"retained_attempts"`
	RemainingOriginalAttempts        *uint16                                `json:"remaining_original_attempts,omitempty"`
	RetainedCompletedActions         uint8                                  `json:"retained_completed_actions"`
	SuccessorRequirements            bootstrapContractSuccessorRequirements `json:"successor_requirements"`
	Blockers                         []string                               `json:"installation_blockers"`
	PendingChainPhases               []string                               `json:"pending_chain_phases"`
	CurrentChainVerified             bool                                   `json:"current_chain_verified"`
	SafeAuthorityVerified            bool                                   `json:"safe_authority_verified"`
	Signing                          bool                                   `json:"signing"`
	NetworkEffects                   bool                                   `json:"network_effects"`
	InstallationComplete             bool                                   `json:"installation_complete"`
	ActivationReady                  bool                                   `json:"activation_ready"`
	ContentHash                      string                                 `json:"content_hash"`
}

// The existing finite graph remains unchanged. Only its eight implemented
// projections are selected; a missing or unimplemented anchor is a blocker.
func prepareBootstrapContractReadiness(ctx context.Context, reserve evmCreatePlan, configPath string) (bootstrapChainContractReadiness, []evmCreatePlan, error) {
	var result bootstrapChainContractReadiness
	if ctx == nil {
		return result, nil, errors.New("contract readiness context is absent")
	}
	if err := errors.Join(ctx.Err(), reserve.Config.validate()); err != nil {
		return result, nil, err
	}
	config := reserve.Config
	last := min(len(config.Plan.Actions), 8) - 1
	selected, err := selectEvmCreatePlan(ctx, reserve, config.Plan.Actions[last].Id, configPath)
	if err != nil {
		return result, nil, err
	}
	if err := selected.validateSelection(); err != nil {
		return result, nil, err
	}
	plans := make([]evmCreatePlan, last+1)
	for i := range plans {
		if i == last {
			plans[i] = selected
		} else {
			plans[i] = selected.priorPlan(i)
		}
	}
	result = bootstrapChainContractReadiness{Schema: bootstrapChainContractReadinessSchema,
		ContractPlanHash: config.Plan.hash(), Scope: "approved-plan", Status: "blocked", PlanInspectionComplete: true,
		OriginalMaximumAttempts: config.Plan.MaximumAttempts, MinimumFreshInstallationAttempts: 9,
		PendingChainPhases: bootstrapChainPendingPhases(),
		SuccessorRequirements: bootstrapContractSuccessorRequirements{PredecessorPlanHash: config.Plan.hash(), PredecessorConfigHash: rootObjectHash(config),
			RequiredBindings: []string{"INDEPENDENT_SUCCESSOR_DOMAIN_AND_SIGNATURE", "ORIGINAL_PLAN_CONFIG_AND_CUSTODY_SEALS", "COMPLETED_RECEIPT_ADOPTION_WITHOUT_REPLAY", "UNFINISHED_SIGNED_NONCE_RECONCILIATION", "UNCHANGED_ORIGINAL_ENVELOPES_AND_SIGNATURES", "UNFINISHED_SENDS_AND_APPROVED_RETRY_MARGIN", "CUMULATIVE_ATTEMPTS_AND_LIFETIME_VALUE_PLUS_GAS", "ADDED_ACTION_AUTHORITY_AND_DISTINCT_SAFE_RELAYER_CUSTODY"}},
		Blockers: []string{"NINE_FRESH_INSTALLATION_SENDS_EXCEED_ORIGINAL_ATTEMPT_CAP", "SIGNED_SUCCESSOR_PREFIX_ADOPTION_UNIMPLEMENTED", "SAFE_ANCHOR_EXECUTOR_UNIMPLEMENTED", "SAFE_INNER_AUTHORITY_NONCE_AND_SIGNATURES_UNVERIFIED", "SAFE_RUNTIME_AND_OWNER_PROVENANCE_UNVERIFIED", "RELAYER_CUSTODY_AND_FUNDING_UNVERIFIED", "CURRENT_CONTRACT_STATE_UNVERIFIED", "SOURCE_TO_BYTECODE_AND_RUNTIME_PROVENANCE_UNVERIFIED", "GLOBAL_SIGNING_CUSTODY_FENCE_UNVERIFIED"}}
	if len(config.Plan.Actions) < 9 {
		result.Blockers = append(result.Blockers, "FULL_INSTALLATION_ACTION_APPROVAL_MISSING")
	}
	if len(config.Plan.Actions) > int(config.Plan.MaximumAttempts) {
		result.Blockers = append(result.Blockers, "APPROVED_PREFIX_INITIAL_SENDS_EXCEED_ORIGINAL_ATTEMPT_CAP")
	}
	for i, id := range []string{"reserve-create", "vault-create", "coordinator-create", "escrow-register", "proxy-create", "reserve-link", "vault-link", "evidence-create", "evidence-anchor"} {
		status := "not-inspected"
		if i >= len(config.Plan.Actions) {
			status = "not-approved"
		} else if i == 8 {
			status = "sealed-reservation-only"
		}
		result.Actions = append(result.Actions, bootstrapChainContractAction{Id: id, Approved: i < len(config.Plan.Actions), SemanticsVerified: i < len(plans), ExecutorImplemented: i < 8, CustodyStatus: status})
		result.SuccessorRequirements.UnfinishedActions = append(result.SuccessorRequirements.UnfinishedActions, id)
	}
	return result, plans, ctx.Err()
}

// New inspection paths cannot borrow another bootstrap or validator owner's
// namespace. This admission is separate from unchanged v1/v2/v3 plan hashes.
func validateBootstrapContractReadinessPaths(preparation bootstrapChainPreparation, plans []evmCreatePlan) error {
	seen := map[string]bool{}
	paths := append([]string(nil), preparation.protectedPaths()...)
	for i := 1; i < len(plans); i++ {
		paths = append(paths, filepath.Join(preparation.Plan.Config.RunDirectory, bootstrapContractStateFile(i)))
	}
	for _, path := range paths {
		if seen[path] || seen[path+".lock"] {
			return errors.New("contract readiness journal aliases another bootstrap owner")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	c := preparation.Plan.Config
	for _, path := range []string{preparation.Plan.ConfigPath, c.OwnerTrimPolicy.Path, c.OwnerTrimPlan.Path, c.Contracts.Path, c.Root.Path, c.Validators[0].Config.Path, c.Validators[1].Config.Path, preparation.Contracts.Config.Plan.Artifacts.Path, preparation.Root.ServiceInput.Path, c.RootValidator.Approval.Path} {
		if seen[path] {
			return errors.New("contract readiness journal aliases an approved input")
		}
		seen[path] = true
	}
	return validateBootstrapChainValidatorPaths(preparation.Plan.ValidatorInspections, seen)
}

// Planning precedes custody and signing. Retained inspection requires the exact
// accepted v3 preparation and borrows its original five journals read-only.
func runBootstrapChainContractCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "contract-current-state" {
		return runBootstrapContractCurrentCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "contract-installation-receipts" {
		return runBootstrapContractReceiptCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "contract-role-plan" {
		return runBootstrapContractRoleCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "contract-successor-execution-") {
		return runBootstrapSuccessorExecutionCommand(ctx, args, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "contract-successor-safe-review" {
		return runBootstrapSuccessorSafeReviewCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "contract-successor-preview" || args[0] == "contract-successor-prepare" || args[0] == "contract-successor-resume") {
		return runBootstrapSuccessorPreparationCommand(ctx, args, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "contract-successor-plan" {
		return runBootstrapContractSuccessorCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "contract-plan" && args[0] != "contract-readiness" {
		fmt.Fprintln(stderr, "bootstrap-chain requires contract-plan, contract-readiness, contract-successor-plan, contract-successor-preview/prepare/resume or contract-successor-safe-review")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet("bootstrap-chain "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "canonical private chain preparation config")
	runDirectory := flags.String("run-dir", "", "exact retained private custody directory")
	accepted := flags.String("accept-plan-hash", "", "original accepted v3 preparation hash")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *configPath == "" ||
		command == "contract-plan" && (*runDirectory != "" || *accepted != "") || command == "contract-readiness" && (*runDirectory == "" || !planSha256(*accepted)) {
		fmt.Fprintln(stderr, "contract-plan needs --config; contract-readiness also needs --run-dir and --accept-plan-hash")
		return 2
	}
	preparation, err := loadBootstrapChainPreparation(ctx, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "contract readiness preparation inputs:", err)
		return 2
	}
	if !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) {
		fmt.Fprintln(stderr, "contract readiness requires accepted v3 scope; original v1/v2 custody remains resumable")
		return 3
	}
	if command == "contract-readiness" && (*accepted != preparation.Plan.ContentHash || *runDirectory != preparation.Plan.Config.RunDirectory) {
		fmt.Fprintln(stderr, "contract readiness accepted plan or run directory differs; no journal opened")
		return 3
	}
	result, plans, err := prepareBootstrapContractReadiness(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path)
	if err == nil {
		err = validateBootstrapContractReadinessPaths(preparation, plans)
	}
	if err != nil {
		fmt.Fprintln(stderr, "contract readiness approved action projections:", err)
		return 2
	}
	result.PlanHash = preparation.Plan.ContentHash
	if command == "contract-readiness" {
		result.Scope, result.Status = "retained-custody", "unresolved"
		retained, openErr := openBootstrapChainReadinessState(ctx, preparation)
		if openErr != nil {
			err = openErr
			result.Blockers = append(result.Blockers, "ORIGINAL_PREPARED_CUSTODY_UNRESOLVED")
		} else {
			result.LocalPreparation = retained
			err = inspectBootstrapContractCustody(ctx, plans, &result)
			err = errors.Join(err, retained.checkpoint(ctx), retained.close())
		}
		if err != nil {
			result.Status, result.CustodyInspectionComplete, result.RemainingOriginalAttempts = "unresolved", false, nil
			result.Blockers = append(result.Blockers, "RETAINED_CONTRACT_CUSTODY_UNRESOLVED")
		}
	}
	result.ContentHash = rootObjectHash(result)
	if outputErr := json.NewEncoder(stdout).Encode(result); outputErr != nil {
		fmt.Fprintln(stderr, "contract readiness output:", outputErr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "contract readiness unresolved; preserve original custody:", err)
		return 1
	}
	return 3
}
