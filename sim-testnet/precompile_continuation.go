// Separates exact precompile transaction preparation from a native dividend
// wait. The release's existing writer resumes durable actions while ordinary
// observations keep running; no additional journal writer or approval is made.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const precompilePreparationScenario = "precompile-prepare"

var errPrecompileDividendPending = errors.New("precompile dividend is not finalized yet")

var errPrecompileRecoveryPending = errors.New("precompile move-position recovery remains pending")

// Confines a single dividend read to the continuation caller. The ordinary
// conformance command retains its existing bounded wait semantics.
type precompileDividendSinglePollKey struct{}

// Exact ordering prevents a new or misplaced action from being silently omitted
// by the preparation/continuation seam. The approved plan remains authoritative.
func splitPrecompileActions(plan *SetupPlan) ([]Action, []Action, error) {
	if plan == nil {
		return nil, nil, errors.New("precompile continuation requires an approved plan")
	}
	expected := []string{"precompile.probe-deploy", "precompile.commitment-write", "precompile.commitment-restore", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out"}
	var actions []Action
	for _, action := range plan.Actions {
		if !strings.HasPrefix(action.ID, "precompile.") {
			continue
		}
		if len(actions) >= len(expected) || action.ID != expected[len(actions)] {
			return nil, nil, fmt.Errorf("precompile continuation action %s is outside its exact approved order", action.ID)
		}
		actions = append(actions, action)
	}
	if len(actions) != len(expected) {
		return nil, nil, fmt.Errorf("precompile continuation has %d actions, want %d", len(actions), len(expected))
	}
	return actions[:8], actions[8:], nil
}

// Preparation uses the same durable executor as the full conformance command.
// Returning after snapshot releases the command's journal lock; it is not a pass.
func executePrecompilePreparation(ctx context.Context, plan *SetupPlan, execute func(context.Context, Action) error) error {
	preparation, _, err := splitPrecompileActions(plan)
	if err != nil {
		return err
	}
	if execute == nil {
		return errors.New("precompile preparation executor is unavailable")
	}
	for _, action := range preparation {
		if err := execute(ctx, action); err != nil {
			return err
		}
	}
	return nil
}

// Runs the approved preparation directly, without treating the known missing
// successor deployment in a pre-repair snapshot as a failed acceptance run.
func runPrecompilePreparation(ctx context.Context, cfg *ResolvedConfig, stateDir string, journal *Journal, executor *Executor) error {
	prepared, err := precompilePreparationOwner(cfg, stateDir, journal, executor)
	if err != nil {
		return err
	}
	if err := prepared.ensurePayloads(ctx); err != nil {
		return err
	}
	if err := executePrecompilePreparation(ctx, prepared.plan, prepared.Execute); err != nil {
		return err
	}
	evidence, err := loadPrecompileEvidence(stateDir)
	if err != nil {
		return err
	}
	if err := prepared.validatePrecompileEvidence(prepared.payloads.PrecompileProbeAddress, evidence); err != nil {
		return err
	}
	if evidence.Snapshot.SinceBlock == 0 || evidence.Snapshot.BaselineRao == 0 || !validConformanceTransaction(evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockHash, evidence.Snapshot.BlockNumber) {
		return errors.New("precompile preparation lacks its finalized snapshot")
	}
	return printResult("json", map[string]any{"command": "scenario", "name": precompilePreparationScenario, "plan_hash": prepared.plan.PlanHash, "status": "postcondition_verified", "prepared_actions": 8, "probe": evidence.ProbeAddress, "snapshot_block": evidence.Snapshot.SinceBlock, "evidence_hash": evidence.EvidenceHash, "dividend_pending": !precompileEvidenceComplete(evidence), "provisional": true, "final_acceptance": false}, nil)
}

// Borrows the command's already authenticated route and transaction managers.
// Standalone repair does not own a topology or a second executor to close; a
// supervised release retains its separate campaign egress handoff.
func precompilePreparationOwner(cfg *ResolvedConfig, stateDir string, journal *Journal, executor *Executor) (*Executor, error) {
	if !provisionalResumeEnabled(cfg) || journal == nil || executor == nil || executor.plan == nil || executor.roles == nil || executor.substrate == nil || executor.deployer == nil {
		return nil, errors.New("precompile preparation requires an approved provisional scenario writer")
	}
	if executor.auditAuthorizedConfig != cfg || executor.journal != journal || executor.stateDir != stateDir || cfg.provisionalResume.Record.PlanHash != executor.plan.PlanHash {
		return nil, errors.New("precompile preparation owner differs from the authenticated command")
	}
	if err := validateCampaignRPCTransport(cfg, executor.cfg); err != nil {
		return nil, fmt.Errorf("precompile preparation transport: %w", err)
	}
	return executor, nil
}

// Every continuation first authenticates the exact preparation frontier. An
// immature dividend performs only a read; completed dividend work is never
// repeated just because transfer finality or a later observation was interrupted.
func continuePrecompileActions(ctx context.Context, plan *SetupPlan, verified func(Action) (bool, error), observe func(context.Context) (bool, error), execute func(context.Context, Action) error) error {
	preparation, remaining, err := splitPrecompileActions(plan)
	if err != nil {
		return err
	}
	if verified == nil || observe == nil || execute == nil {
		return errors.New("precompile continuation capabilities are incomplete")
	}
	for _, action := range preparation {
		ok, err := verified(action)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	for index, action := range remaining {
		ok, err := verified(action)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if index == 0 {
			ready, err := observe(ctx)
			if err != nil || !ready {
				return err
			}
		}
		// The ready observation is advisory. Execution rereads and verifies the
		// real chain, and can remain pending if finality or the endpoint changes.
		if err := execute(context.WithValue(ctx, precompileDividendSinglePollKey{}, true), action); err != nil {
			return err
		}
	}
	return nil
}

// A deadline or immature dividend leaves proof pending and lets the observer
// continue. Integrity failures remain visible to the scenario's hard error path.
func precompileContinuationSnapshot(ctx context.Context, advance func(context.Context) error, observe func(context.Context) (*ScenarioObservation, error)) (*ScenarioObservation, error) {
	if advance != nil {
		if err := advance(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !precompileContinuationMayWait(err) {
				return nil, err
			}
			fmt.Fprintf(os.Stderr, "sim-testnet: precompile continuation pending; retained checkpoints preserved: %v\n", err)
		}
	}
	return observe(ctx)
}

// Every cause of a composite failure must be pending or a transient chain read.
// A wrapped or joined integrity/storage failure cannot borrow a pending label.
func precompileContinuationMayWait(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if err == errPrecompileDividendPending || err == errPrecompileRecoveryPending {
		return true
	}
	if _, fileError := err.(*os.PathError); fileError {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !precompileContinuationMayWait(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return precompileContinuationMayWait(cause)
		}
	}
	return historicalPreparationReadIsTransient(err)
}

// Checks one pinned dividend observation without sleeping or changing evidence.
// Snapshot substitution is an integrity failure, not a reason to wait longer.
func evaluatePrecompileDividend(evidence *PrecompileConformanceEvidence, tempo uint64, head ChainHead, baseline, current, since uint64) (PrecompileDividendStep, bool, error) {
	if evidence == nil || tempo == 0 || evidence.Snapshot.SinceBlock == 0 || evidence.Snapshot.BaselineRao == 0 || head.Number == 0 || !validCanonicalHashHex(head.Hash) {
		return PrecompileDividendStep{}, false, errors.New("precompile dividend observation identity is incomplete")
	}
	if baseline != evidence.Snapshot.BaselineRao || since != evidence.Snapshot.SinceBlock {
		return PrecompileDividendStep{}, false, errors.New("precompile dividend snapshot differs from its retained baseline")
	}
	if current < baseline {
		return PrecompileDividendStep{}, false, errors.New("precompile dividend observation lost retained probe stake")
	}
	if head.Number < since || head.Number-since < tempo || current <= baseline {
		return PrecompileDividendStep{}, false, nil
	}
	return PrecompileDividendStep{FinalizedHead: head, BaselineRao: baseline, CurrentRao: current, DeltaRao: current - baseline, SinceBlock: since}, true, nil
}

// A prepared deployment never satisfies final conformance: the complete exact
// conservation and positive dividend chain remains mandatory at interval end.
func precompileContinuationCompleteCheck() scenarioCheck {
	return scenarioCheck{ID: "precompile_conformance_complete", Check: func(e *scenarioEvaluation) (bool, string) {
		valid := e.Current.PrecompileConformanceValid && e.Current.PrecompileConformanceError == "" && precompileEvidenceComplete(e.Current.PrecompileConformance)
		return valid, fmt.Sprintf("valid=%t error=%s", valid, e.Current.PrecompileConformanceError)
	}}
}

// Runs only under the release runner's existing writer ownership. Pending reads
// do not write journal failures, while signed actions keep normal nonce, budget,
// receipt and postcondition enforcement and resume from their durable frontier.
func (self *Executor) advancePrecompileContinuation(ctx context.Context) error {
	if self == nil || self.cfg == nil || !provisionalResumeEnabled(self.cfg) || self.journal == nil || self.payloads == nil {
		return errors.New("precompile continuation requires the provisional release writer")
	}
	turnCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	verified := func(action Action) (bool, error) {
		entry, ok := self.verifiedActionEntry(action)
		if !ok {
			return false, nil
		}
		return true, self.authenticateProvisionalReceipt(action, entry)
	}
	observe := func(ctx context.Context) (bool, error) {
		evidence, err := loadPrecompileEvidence(self.stateDir)
		if err != nil {
			return false, err
		}
		if err := self.validatePrecompileEvidence(self.payloads.PrecompileProbeAddress, evidence); err != nil {
			return false, err
		}
		parsed, err := self.precompileABI()
		if err != nil {
			return false, err
		}
		sample, err := roleBytes32(self.roles, validatorHotkeyLabel(1))
		if err != nil {
			return false, err
		}
		tempoValue, err := self.substrate.ReadHyper("tempo")
		if err != nil {
			return false, err
		}
		tempo, ok := numericUint64(tempoValue)
		if !ok || tempo == 0 {
			return false, errors.New("precompile dividend tempo is invalid")
		}
		head, err := finalizedEVMHead(ctx, self.deployer.client)
		if err != nil {
			return false, err
		}
		baseline, current, since, err := readDividendAtFinalized(ctx, self.deployer.client, self.payloads.PrecompileProbeAddress, parsed, sample)
		if err != nil {
			return false, err
		}
		_, ready, err := evaluatePrecompileDividend(evidence, tempo, head, baseline, current, since)
		return ready, err
	}
	return continuePrecompileActions(turnCtx, self.plan, verified, observe, self.executePrecompileContinuationAction)
}

// Recovery readiness is checked before Execute journals an intent or reaches a
// signer. The already verified dividend remains durable while cleanup is pending.
func (self *Executor) executePrecompileContinuationAction(ctx context.Context, action Action) error {
	if action.ID == "precompile.transfer-out" {
		evidence, err := loadPrecompileEvidence(self.stateDir)
		if err != nil {
			return err
		}
		if err := self.validatePrecompileEvidence(self.payloads.PrecompileProbeAddress, evidence); err != nil {
			return err
		}
		if evidence.Back.FromAfterRao != 0 {
			if err := self.advancePrecompileRecovery(ctx, evidence); err != nil {
				return err
			}
			evidence, err = loadPrecompileEvidence(self.stateDir)
			if err != nil {
				return err
			}
		}
		if err := precompileTransferReadiness(evidence); err != nil {
			return err
		}
	}
	return self.Execute(ctx, action)
}

// Only an exactly recorded, receipt-accounted liability can be deferred. Lost
// principal, altered credits and missing evidence remain integrity failures.
func precompileTransferReadiness(evidence *PrecompileConformanceEvidence) error {
	if !precompileRoundTripAccounted(evidence) {
		return errors.New("precompile transfer has no accounted round trip")
	}
	if evidence.Recovery != nil {
		_, move, settled, err := precompileRecoveryPositions(evidence)
		if err != nil {
			return err
		}
		if !settled || move != 0 {
			return errPrecompileRecoveryPending
		}
		return nil
	}
	if evidence.Back.FromAfterRao != 0 {
		return fmt.Errorf("%w: outstanding_alpha_rao=%d", errPrecompileRecoveryPending, evidence.Back.FromAfterRao)
	}
	return nil
}
