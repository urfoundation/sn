package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const provisionalEpochActivityAssertionID = "provisional_epoch_validator_activity"

// Operational completion is separate from the unchanged strict result and
// failed assertion count. It never creates a release completion marker.
type provisionalEpochOutcome struct {
	Status             string   `json:"status"`
	Interruption       string   `json:"interruption,omitempty"`
	RequiredAssertions []string `json:"required_assertions"`
	BlockingAssertions []string `json:"blocking_assertions"`
	DeferredAssertions []string `json:"deferred_assertions"`
	BlockingAnomalies  []string `json:"blocking_anomalies"`
	DeferredAnomalies  []string `json:"deferred_anomalies"`
}

func provisionalEpochEnabled(cfg *ResolvedConfig, name string) bool {
	return name == "epoch" && provisionalResumeEnabled(cfg)
}

func provisionalEpochRequiredAssertions() []string {
	return []string{"contracts_installed", "native_custody_hotkeys", "operator_count", "policy_hash_matches", provisionalEpochActivityAssertionID, "public_identities", "rao_conservation", "required_finalized_epochs", "runtime_code_matches", "runtime_transfer_minimum_bound"}
}

func provisionalEpochDeferredAssertion(id string) bool {
	switch id {
	case "topology_healthy", "fleet_commitment_and_bindings", "operator_public_apis", "per_no_verify_coverage", "payout_artifacts_reconstruct", "validator_local_v2_receipts_finalized_and_applied", "validator_path_proofs_advance", anomalyGateAssertionID, "evidence_publication":
		return true
	}
	return false
}

// Unknown checks still block. All named core checks must be present and pass;
// metadata alone cannot turn a failed assertion into operational completion.
func provisionalEpochBlockingAssertions(assertions []AssertionRecord) []string {
	passed := map[string]bool{}
	blocking := map[string]bool{}
	for _, assertion := range assertions {
		passed[assertion.ID] = assertion.Passed
		if !assertion.Passed && !provisionalEpochDeferredAssertion(assertion.ID) {
			blocking[assertion.ID] = true
		}
	}
	for _, id := range provisionalEpochRequiredAssertions() {
		if !passed[id] {
			blocking[id] = true
		}
	}
	ids := make([]string, 0, len(blocking))
	for id := range blocking {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func scenarioAssertionsComplete(cfg *ResolvedConfig, definition scenarioDefinition, assertions []AssertionRecord) bool {
	if !provisionalEpochEnabled(cfg, definition.Name) {
		return assertionsPass(assertions)
	}
	return len(provisionalEpochBlockingAssertions(assertions)) == 0
}

func provisionalEpochDeferredAnomaly(anomaly ScenarioAnomaly) bool {
	if anomaly.Class == "assertion-failure" {
		return provisionalEpochDeferredAssertion(strings.TrimPrefix(anomaly.Source, "assertion:"))
	}
	switch anomaly.Class {
	case "deployment-health", "deployment-warning", "process-health", "process-exit", "process-topology", "unexpected-restart", "supervisor-restart", "operator-health", "operator-error", "validator-error", "claim-error", "claim-terminal-state", "voluntary-conviction-error", "governance-drill-error", "precompile-conformance-error", "dishonest-deposit-error":
		return true
	}
	return strings.HasPrefix(anomaly.Class, "process-log-")
}

func refreshProvisionalEpochOutcome(result *ScenarioResult) {
	if result.ProvisionalEpoch == nil || !result.Provisional || result.Name != "epoch" || result.FinalAcceptance == nil || *result.FinalAcceptance {
		return
	}
	outcome := &provisionalEpochOutcome{Status: "incomplete", Interruption: result.ProvisionalEpoch.Interruption, RequiredAssertions: provisionalEpochRequiredAssertions(), BlockingAssertions: provisionalEpochBlockingAssertions(result.Assertions), DeferredAssertions: []string{}, BlockingAnomalies: []string{}, DeferredAnomalies: []string{}}
	for index := range result.Assertions {
		assertion := &result.Assertions[index]
		assertion.DeferredToFinalAcceptance = provisionalEpochDeferredAssertion(assertion.ID)
		if !assertion.Passed && assertion.DeferredToFinalAcceptance {
			outcome.DeferredAssertions = append(outcome.DeferredAssertions, assertion.ID)
		}
	}
	if result.Anomalies != nil {
		for _, anomaly := range result.Anomalies.Entries {
			if provisionalEpochDeferredAnomaly(anomaly) {
				outcome.DeferredAnomalies = append(outcome.DeferredAnomalies, anomaly.ID)
			} else {
				outcome.BlockingAnomalies = append(outcome.BlockingAnomalies, anomaly.ID)
			}
		}
		if len(outcome.BlockingAssertions) == 0 && len(outcome.BlockingAnomalies) == 0 {
			outcome.Status = "complete"
		}
	}
	if outcome.Interruption != "" {
		outcome.Status = "interrupted"
	}
	result.ProvisionalEpoch = outcome
}

func markProvisionalEpochInterruption(result *ScenarioResult, err error) {
	if result.ProvisionalEpoch != nil && errors.Is(err, context.Canceled) {
		result.ProvisionalEpoch.Interruption = err.Error()
	}
}

func logProvisionalEpochFindings(assertions []AssertionRecord, previous map[string]string) {
	for _, assertion := range assertions {
		if assertion.DeferredToFinalAcceptance && !assertion.Passed && previous[assertion.ID] != assertion.Message {
			fmt.Fprintf(os.Stderr, "sim-testnet: provisional epoch observation; assertion=%s passed=false deferred_to_final_acceptance=true final_acceptance=false; %s\n", assertion.ID, assertion.Message)
			previous[assertion.ID] = assertion.Message
		}
	}
}

// Every validator must show fresh traffic in each operator domain or a new
// recorded native application. Empty/stale local V2 receipts are not a gate.
func provisionalEpochActivity(e *scenarioEvaluation) (bool, string) {
	if !provisionalEpochEnabled(e.Cfg, e.Definition.Name) || e.Start == nil || e.Current == nil || e.Cfg.Config.Topology.Validators < 1 || e.Cfg.Config.Topology.Operators < 1 || len(e.Start.Validators) != e.Cfg.Config.Topology.Validators || len(e.Current.Validators) != e.Cfg.Config.Topology.Validators {
		return false, "validator activity baseline or current census unavailable"
	}
	baseline := map[int]ValidatorObservation{}
	for _, validator := range e.Start.Validators {
		if validator.ValidatorID < 1 || validator.ValidatorID > e.Cfg.Config.Topology.Validators {
			return false, "invalid baseline validator identity"
		}
		baseline[validator.ValidatorID] = validator
	}
	seen := map[int]bool{}
	for _, validator := range e.Current.Validators {
		prior, present := baseline[validator.ValidatorID]
		if !present || seen[validator.ValidatorID] || len(baseline) != e.Cfg.Config.Topology.Validators {
			return false, "missing or duplicate validator activity identity"
		}
		seen[validator.ValidatorID] = true
		pathsAdvance := len(prior.PathProofCounts) == e.Cfg.Config.Topology.Operators && len(validator.PathProofCounts) == e.Cfg.Config.Topology.Operators
		for noID := 1; noID <= e.Cfg.Config.Topology.Operators; noID++ {
			before, beforeOK := prior.PathProofCounts[noID]
			after, afterOK := validator.PathProofCounts[noID]
			pathsAdvance = pathsAdvance && beforeOK && afterOK && before >= 0 && after > before
		}
		if !pathsAdvance && !provisionalEpochNativeActivity(e.Start, prior, validator) {
			return false, fmt.Sprintf("validator=%d has no fresh path proofs or recorded native application", validator.ValidatorID)
		}
	}
	return true, "every validator advanced all operator path proofs or recorded a fresh native application; final_acceptance=false"
}

func provisionalEpochNativeActivity(start *ScenarioObservation, prior, current ValidatorObservation) bool {
	local := current.LocalRuntimeIntents
	if current.Error != "" || local == nil || local.Scope != "local-runtime-observation" || local.FinalAcceptance || local.State != "observed" || local.Error != "" || !validSHA256ContentHash(local.StoreSHA256) || !validSHA256ContentHash(local.HandoffSHA256) {
		return false
	}
	// A finalized native baseline prevents a newly fetched historical receipt
	// from masquerading as activity during this run.
	if start.NativeRewards == nil || start.NativeRewardsError != "" || start.NativeRewards.FinalizedHead.Number == 0 || !validCanonicalHashHex(start.NativeRewards.FinalizedHead.Hash) {
		return false
	}
	cutoff := start.NativeRewards.FinalizedHead.Number
	if prior.LocalRuntimeIntents != nil {
		for _, receipt := range prior.LocalRuntimeIntents.Receipts {
			cutoff = max(cutoff, receipt.ApplicationBlock)
		}
	}
	for _, receipt := range local.Receipts {
		if receipt.Status == "applied" && receipt.FinalizedBlock != 0 && receipt.ApplicationBlock > cutoff && receipt.ApplicationBlock >= receipt.FinalizedBlock && validCanonicalHashHex(receipt.ExtrinsicHash) && validCanonicalHashHex(receipt.FinalizedBlockHash) && validCanonicalHashHex(receipt.ApplicationBlockHash) {
			return true
		}
	}
	return false
}

// Publication is best effort for this explicitly non-accepting observation.
// Cancellation never launches another request; local evidence is always saved.
func finishProvisionalEpochObservation(ctx context.Context, cfg *ResolvedConfig, stateDir, runDir string, result *ScenarioResult, start, current *ScenarioObservation, history []*ScenarioObservation, options scenarioRunOptions) (*ScenarioResult, error) {
	if options.Publish {
		publishErr := ctx.Err()
		if publishErr == nil {
			publishCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			copy := *result
			copy.PublishedEvidence = nil
			bundle := ScenarioEvidenceBundle{Schema: "urnetwork-sim-scenario-evidence-v1", Result: &copy, Observation: current, Analysis: analyzeScenarioObservation(cfg, current)}
			var published []PublishedEvidence
			published, publishErr = publishEvidence(publishCtx, cfg, options.Roles, stateDir, "scenario-bundle", result.RunID, bundle)
			if publishErr == nil {
				publishErr = verifyPublishedEvidenceOrigins(publishCtx, cfg, options.Roles, published)
			}
			cancel()
			result.PublishedEvidence = published
		}
		if publishErr != nil {
			result.Assertions = append(result.Assertions, AssertionRecord{ID: "evidence_publication", Passed: false, Message: fmt.Sprintf("provisional publication deferred: %v", publishErr), StartedAt: result.StartedAt, CompletedAt: options.Now().UTC().Format(time.RFC3339Nano), ObservationHash: current.ObservationHash})
		}
	}
	markProvisionalEpochInterruption(result, ctx.Err())
	completed, _ := time.Parse(time.RFC3339Nano, result.CompletedAt)
	attachScenarioAnomalyGate(result, completed, start, current, history...)
	var err error
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err == nil {
		err = writeScenarioOutputs(cfg, runDir, result, current)
	}
	if err != nil {
		return result, err
	}
	logProvisionalEpochFindings(result.Assertions, map[string]string{})
	fmt.Fprintf(os.Stderr, "sim-testnet: provisional epoch %s; failed_assertions=%d deferred_assertions=%d final_acceptance=false; evidence %s\n", result.ProvisionalEpoch.Status, result.FailedAssertionCount, len(result.ProvisionalEpoch.DeferredAssertions), filepath.Join(runDir, "result.json"))
	if result.ProvisionalEpoch.Status != "complete" {
		return result, fmt.Errorf("provisional epoch %s; evidence %s", result.ProvisionalEpoch.Status, filepath.Join(runDir, "result.json"))
	}
	return result, nil
}
