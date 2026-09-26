// A failed provisional release can finish observation after its signed window
// and cleanup. Missing reads, pending claims and mutable margins still retry;
// only an irreversible accepted-epoch or retained log failure ends that wait.
package main

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// The caller has just persisted the complete observation and authenticated
// runtime checkpoint. Recheck that exact ownership before choosing to seal a
// failure; this is neither acceptance nor authority for a later mutation.
func scenarioProvisionalFailureCompletion(cfg *ResolvedConfig, definition scenarioDefinition, window *ScenarioAcceptanceWindow, current *ScenarioObservation, faults []ScenarioFaultRecord, assertions []AssertionRecord, options scenarioRunOptions) string {
	if !provisionalResumeEnabled(cfg) || !cfg.provisionalResume.Record.Provisional || cfg.readOnlyAudit || cfg.provisionalResume.Record.FinalAcceptance || definition.Name != "release-1.0" || options.Attempt == nil || window == nil || !scenarioAcceptanceIntervalObserved(window, current) || len(assertions) == 0 || !faultsComplete(faults) || options.FleetLifecycle != nil && !options.FleetLifecycle.Complete() {
		return ""
	}
	payload := &options.Attempt.payload
	boundary := payload.AcceptanceBoundary
	if boundary == nil || payload.Phase != definition.Name || !payload.PreparationComplete || payload.AcceptanceInvalidation != "" || payload.AcceptanceInvalidatedAt != "" || payload.ConfigHash != cfg.ConfigHash || payload.PolicyHash != cfg.PolicyHash || payload.PlanHash != cfg.provisionalResume.Record.PlanHash || boundary.AcceptanceWindow != *window || !reflect.DeepEqual(boundary.Faults, faults) {
		return ""
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil || definitionHash != boundary.ScenarioDefinitionHash || boundary.AdversarialMatrixHash != definition.AdversarialMatrixHash {
		return ""
	}
	head, epoch, hash, err := scenarioObservationIdentity(current)
	if err != nil || head != boundary.LastObservationHead || epoch != boundary.LastObservationEpoch || hash != boundary.LastObservationHash {
		return ""
	}
	processSessionId := options.ProcessSessionID
	if processSessionId == "" {
		processSessionId = scenarioProcessSessionID
	}
	if boundary.ProcessSessionID != processSessionId {
		return ""
	}
	if _, err := validateScenarioAttemptFaultRecords(definition, window, faults); err != nil {
		return ""
	}
	for _, fault := range faults {
		if fault.RestoredBlock > head.Number {
			return ""
		}
	}
	if terminal, _ := acceptedEpochsTerminal(current.Status.Contracts, window, cfg.Config.Topology.Operators); !terminal {
		return ""
	}
	// Ready is an acceptance sample quota, not worker shutdown. An already
	// impossible pass must still Stop/join these exact workers and report their
	// missing samples; waiting for Ready would reproduce the terminal hang.
	if options.Adversaries == nil {
		return ""
	}
	adversary := options.Adversaries.Snapshot()
	if adversary == nil || adversary.Status != "running" || adversary.MatrixHash != boundary.AdversarialMatrixHash || adversary.StartedAt != boundary.AdversaryStartedAt || adversary.HappyPathStartedAt != boundary.AdversaryHappyPathStartedAt {
		return ""
	}
	for _, assertion := range assertions {
		if assertion.ID == "payout_artifacts_enforce_one_tier" && !assertion.Passed && assertion.ObservationHash == hash {
			if reason := scenarioIrreversiblePayoutFailure(window, current); reason != "" {
				return reason
			}
		}
	}
	return scenarioIrreversibleProcessFailure(boundary.ProcessLogBoundaryHash, current.ProcessLogFindings)
}

// A finalized missed root cannot be supplied later. A signed artifact with
// invalid tier membership likewise cannot change while keeping its chain hash.
// Absent artifacts, low-usage readiness and unfinalized rows prove neither.
func scenarioIrreversiblePayoutFailure(window *ScenarioAcceptanceWindow, current *ScenarioObservation) string {
	end, ok := checkedAdd(window.FirstEpoch, window.EpochCount)
	if !ok {
		return ""
	}
	zero := "0x" + strings.Repeat("0", 64)
	seenEpochs := map[uint64]bool{}
	for _, epoch := range current.Status.Contracts.Epochs {
		if epoch.Epoch < window.FirstEpoch || epoch.Epoch >= end {
			continue
		}
		if seenEpochs[epoch.Epoch] {
			return ""
		}
		seenEpochs[epoch.Epoch] = true
	}
	for _, epoch := range current.Status.Contracts.Epochs {
		if epoch.Epoch < window.FirstEpoch || epoch.Epoch >= end {
			continue
		}
		for _, operator := range epoch.Operators {
			if operator.Status == 3 && operator.CommitBlock == 0 && operator.ArtifactHash == zero && operator.PayoutRoot == zero {
				return fmt.Sprintf("accepted epoch %d operator %d finalized RootMissed; the committed payout tier cannot recover", epoch.Epoch, operator.NoID)
			}
		}
	}
	for _, operator := range current.Operators {
		for _, row := range operator.PayoutTierArtifacts {
			if row.Epoch >= window.FirstEpoch && row.Epoch < end && row.NoId == operator.NoID && scenarioPayoutTierMatchesChain(row, current.Status.Contracts) && !scenarioPayoutTierValid(row) {
				return fmt.Sprintf("accepted epoch %d operator %d has immutable invalid payout tiers in %s", row.Epoch, row.NoId, row.ContentHash)
			}
		}
	}
	return ""
}

// Only the existing permanent strict transport counters qualify. Keep unknown
// classes and explicit recovery evidence out of this stopping rule. Reapply
// isolated-event allowances so a lone recoverable timeout cannot end a run.
func scenarioIrreversibleProcessFailure(scope string, findings []ProcessLogFinding) string {
	if !validCanonicalHashHex(scope) {
		return ""
	}
	scoped := make([]ProcessLogFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.AcceptanceScope == scope {
			scoped = append(scoped, finding)
		}
	}
	projectIsolatedProcessLogTlsTimeouts(scoped)
	projectIsolatedProcessLogExitGapTimeout(scoped)
	for _, finding := range scoped {
		// Scanner line hashes are bare hex. Add only the validator's domain
		// prefix; retained scanner bytes and strict classifications stay intact.
		if !finding.Blocking || finding.Disposition != "unexplained" || !slices.Contains([]string{"tls-handshake-timeout", "exit-gap-timeout"}, finding.Class) || finding.ProcessID == "" || finding.Count == 0 || finding.FirstOffset < 0 || finding.LastOffset < finding.FirstOffset || !validSHA256ContentHash("sha256:"+finding.FirstLineSHA256) || !validSHA256ContentHash("sha256:"+finding.LastLineSHA256) || len(finding.FaultIDs) != 0 || len(finding.FaultKinds) != 0 || finding.RecoveryStartedAt != "" || finding.RecoveryDeadlineAt != "" || finding.RecoveryObservedAt != "" || finding.RecoveryLineSHA256 != "" || finding.RecoveryLogAt != "" || finding.RecoveryOffset != 0 {
			continue
		}
		return fmt.Sprintf("accepted process log %s/%s/%s retains %d strict blocking occurrence(s), first line %s", finding.ProcessID, finding.Stream, finding.Class, finding.Count, finding.FirstLineSHA256)
	}
	return ""
}
