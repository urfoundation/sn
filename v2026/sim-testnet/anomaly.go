package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
)

const anomalyGateAssertionID = "anomaly_ledger_clean"

// ScenarioAnomaly is deliberately append-only evidence. A failing run leaves
// every entry open; root-cause and disposition fields are filled in the
// mainnet-readiness dossier only after a minimized reproduction, regression
// test, and clean rerun exist.
type ScenarioAnomaly struct {
	ID              string `json:"id"`
	Class           string `json:"class"`
	Severity        string `json:"severity"`
	Source          string `json:"source"`
	Summary         string `json:"summary"`
	FirstObservedAt string `json:"first_observed_at"`
	ObservationHash string `json:"observation_hash,omitempty"`
	Status          string `json:"status"`
	RootCause       string `json:"root_cause,omitempty"`
	Disposition     string `json:"disposition,omitempty"`
	Regression      string `json:"regression,omitempty"`
	ResolvedByRun   string `json:"resolved_by_run,omitempty"`
}

type ScenarioAnomalyLedger struct {
	Schema      string            `json:"schema"`
	Release     string            `json:"release"`
	RunID       string            `json:"run_id"`
	GeneratedAt string            `json:"generated_at"`
	Status      string            `json:"status"`
	Entries     []ScenarioAnomaly `json:"entries"`
}

type anomalyCollector struct {
	when            string
	observationHash string
	entries         map[string]ScenarioAnomaly
}

func (c *anomalyCollector) add(class, severity, source, summary, observedAt string) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	if observedAt == "" {
		observedAt = c.when
	}
	key := class + "\x00" + source + "\x00" + summary
	digest := sha256.Sum256([]byte(key))
	id := "ANOM-" + hex.EncodeToString(digest[:12])
	if _, exists := c.entries[id]; exists {
		return
	}
	c.entries[id] = ScenarioAnomaly{
		ID: id, Class: class, Severity: severity, Source: source,
		Summary: summary, FirstObservedAt: observedAt,
		ObservationHash: c.observationHash, Status: "open",
	}
}

func processStates(observation *ScenarioObservation) map[string]ProcessState {
	states := map[string]ProcessState{}
	if observation == nil || observation.Status == nil || observation.Status.Supervisor == nil {
		return states
	}
	for _, process := range observation.Status.Supervisor.Processes {
		states[process.ID] = process
	}
	return states
}

func buildScenarioAnomalyLedger(runID string, generatedAt time.Time, start, current *ScenarioObservation, assertions []AssertionRecord, faults []ScenarioFaultRecord, adversaries *AdversaryCampaignEvidence, history ...*ScenarioObservation) *ScenarioAnomalyLedger {
	when := generatedAt.UTC().Format(time.RFC3339Nano)
	observations := make([]*ScenarioObservation, 0, len(history)+2)
	for _, observation := range history {
		if observation != nil {
			observations = append(observations, observation)
		}
	}
	if len(observations) == 0 {
		if start != nil {
			observations = append(observations, start)
		}
		if current != nil && current != start {
			observations = append(observations, current)
		}
	}
	if start == nil && len(observations) != 0 {
		start = observations[0]
	}
	if current == nil && len(observations) != 0 {
		current = observations[len(observations)-1]
	}
	observationHash := ""
	if current != nil {
		observationHash = current.ObservationHash
	}
	collector := &anomalyCollector{when: when, observationHash: observationHash, entries: map[string]ScenarioAnomaly{}}

	for _, assertion := range assertions {
		if assertion.ID != anomalyGateAssertionID && !assertion.Passed {
			collector.add("assertion-failure", "critical", "assertion:"+assertion.ID, assertion.Message, assertion.CompletedAt)
		}
	}

	// Inspect every observation, not just the terminal snapshot. A warning or
	// component failure that self-recovers is still an anomaly requiring a root
	// cause; successful retry cannot erase it from a release run.
	var previousContracts *ContractView
	for _, observation := range observations {
		collector.observationHash = observation.ObservationHash
		expectedTargets := map[string]bool{}
		for _, target := range observation.ExpectedFaultTargets {
			expectedTargets[target] = true
		}
		if observation.Status != nil {
			status := observation.Status
			if !status.Healthy && len(expectedTargets) == 0 {
				collector.add("deployment-health", "critical", "deployment-status", "deployment status is unhealthy", observation.ObservedAt)
			}
			for index, warning := range status.Warnings {
				collector.add("deployment-warning", "high", fmt.Sprintf("deployment-status:warning:%d", index), warning, observation.ObservedAt)
			}
			if status.Supervisor != nil {
				for _, process := range status.Supervisor.Processes {
					if (!process.Healthy || process.PID <= 1) && !expectedTargets[process.ID] {
						collector.add("process-health", "critical", "process:"+process.ID, fmt.Sprintf("healthy=%t pid=%d", process.Healthy, process.PID), observation.ObservedAt)
					}
					if process.ExitError != "" && !expectedTargets[process.ID] {
						collector.add("process-exit", "critical", "process:"+process.ID, process.ExitError, observation.ObservedAt)
					}
				}
			}
			if status.Contracts == nil {
				collector.add("contract-observation-gap", "critical", "contracts", "contract state is unavailable", observation.ObservedAt)
			} else {
				contracts := status.Contracts
				if contracts.Deployment == nil {
					collector.add("contract-deployment-drift", "critical", "contracts:deployment", "contract deployment metadata is unavailable", observation.ObservedAt)
				}
				if !contracts.RuntimeCodeMatches {
					collector.add("contract-runtime-drift", "critical", "contracts:runtime-code", "deployed runtime code no longer matches the release lock", observation.ObservedAt)
				}
				captured, capturedOK := new(big.Int).SetString(contracts.TotalCaptured, 10)
				paid, paidOK := new(big.Int).SetString(contracts.TotalPaid, 10)
				escrow, escrowOK := new(big.Int).SetString(contracts.EscrowAccounted, 10)
				pending, pendingOK := new(big.Int).SetString(contracts.PendingFunding, 10)
				outstanding, outstandingOK := new(big.Int).SetString(contracts.Outstanding, 10)
				liveEscrow, liveEscrowOK := new(big.Int).SetString(contracts.LiveEscrowStake, 10)
				conservationOK := capturedOK && paidOK && escrowOK && pendingOK && outstandingOK && liveEscrowOK && captured.Sign() >= 0 && paid.Sign() >= 0 && escrow.Sign() >= 0 && pending.Sign() >= 0 && outstanding.Sign() >= 0 && liveEscrow.Sign() >= 0 && captured.Cmp(new(big.Int).Add(paid, escrow)) == 0 && escrow.Cmp(new(big.Int).Add(pending, outstanding)) == 0 && liveEscrow.Cmp(escrow) >= 0
				if !contracts.ConservationHolds || !conservationOK {
					collector.add("value-conservation", "critical", "contracts:rao-conservation", fmt.Sprintf("flag=%t captured=%q paid=%q escrow=%q pending=%q outstanding=%q live=%q", contracts.ConservationHolds, contracts.TotalCaptured, contracts.TotalPaid, contracts.EscrowAccounted, contracts.PendingFunding, contracts.Outstanding, contracts.LiveEscrowStake), observation.ObservedAt)
				}
				if status.PolicyHash != "" && !strings.EqualFold(status.PolicyHash, contracts.PolicyHash) {
					collector.add("contract-policy-drift", "critical", "contracts:policy", fmt.Sprintf("status=%s contract=%s", status.PolicyHash, contracts.PolicyHash), observation.ObservedAt)
				}
				if previousContracts != nil {
					if contracts.FinalizedHead.Number < previousContracts.FinalizedHead.Number {
						collector.add("finality-regression", "critical", "contracts:finalized-head", fmt.Sprintf("previous=%d/%s current=%d/%s", previousContracts.FinalizedHead.Number, previousContracts.FinalizedHead.Hash, contracts.FinalizedHead.Number, contracts.FinalizedHead.Hash), observation.ObservedAt)
					}
					if contracts.FinalizedHead.Number == previousContracts.FinalizedHead.Number && previousContracts.FinalizedHead.Hash != "" && contracts.FinalizedHead.Hash != "" && !strings.EqualFold(contracts.FinalizedHead.Hash, previousContracts.FinalizedHead.Hash) {
						collector.add("finality-equivocation", "critical", "contracts:finalized-head", fmt.Sprintf("block=%d previous_hash=%s current_hash=%s", contracts.FinalizedHead.Number, previousContracts.FinalizedHead.Hash, contracts.FinalizedHead.Hash), observation.ObservedAt)
					}
					if contracts.CurrentEpoch < previousContracts.CurrentEpoch {
						collector.add("epoch-regression", "critical", "contracts:epoch", fmt.Sprintf("previous=%d current=%d", previousContracts.CurrentEpoch, contracts.CurrentEpoch), observation.ObservedAt)
					}
					if contracts.OperatorCount != previousContracts.OperatorCount {
						collector.add("operator-topology-drift", "critical", "contracts:operator-count", fmt.Sprintf("previous=%d current=%d", previousContracts.OperatorCount, contracts.OperatorCount), observation.ObservedAt)
					}
				}
				previousContracts = contracts
			}
		} else {
			collector.add("deployment-observation-gap", "critical", "deployment-status", "deployment status is unavailable", observation.ObservedAt)
		}

		for _, operator := range observation.Operators {
			source := fmt.Sprintf("operator:%d", operator.NoID)
			expected := expectedTargets[fmt.Sprintf("operator-%d-api", operator.NoID)]
			if !operator.Healthy && !expected {
				collector.add("operator-health", "critical", source, fmt.Sprintf("API %s is unhealthy (HTTP %d)", operator.APIURL, operator.StatusCode), observation.ObservedAt)
			}
			if !expected {
				collector.add("operator-error", "critical", source, operator.Error, observation.ObservedAt)
			}
		}
		for _, validator := range observation.Validators {
			if !expectedTargets[fmt.Sprintf("validator-%d", validator.ValidatorID)] {
				collector.add("validator-error", "critical", fmt.Sprintf("validator:%d", validator.ValidatorID), validator.Error, observation.ObservedAt)
			}
		}
		for _, claim := range observation.Claims {
			source := fmt.Sprintf("claim:min%d:no%d", claim.MinerID, claim.NoID)
			expected := expectedTargets[fmt.Sprintf("miner-%d", claim.MinerID)] || expectedTargets[fmt.Sprintf("claim-relayer-%d", claim.NoID)]
			if !expected {
				collector.add("claim-error", "critical", source, claim.Error, observation.ObservedAt)
			}
			if (claim.Uncertain != 0 || claim.Failed != 0) && !expected {
				collector.add("claim-terminal-state", "critical", source, fmt.Sprintf("uncertain=%d failed=%d", claim.Uncertain, claim.Failed), observation.ObservedAt)
			}
		}
		collector.add("native-custody-error", "critical", "native-custody", observation.NativeCustodyError, observation.ObservedAt)
		collector.add("voluntary-conviction-error", "critical", "voluntary-conviction", observation.VoluntaryConvictionError, observation.ObservedAt)
		collector.add("governance-drill-error", "critical", "governance-drill", observation.GovernanceDrillError, observation.ObservedAt)
		collector.add("precompile-conformance-error", "critical", "precompile-conformance", observation.PrecompileConformanceError, observation.ObservedAt)
		collector.add("dishonest-deposit-error", "critical", "dishonest-deposit", observation.DishonestDepositError, observation.ObservedAt)
	}
	collector.observationHash = observationHash

	expectedRestarts := map[string]int{}
	for _, fault := range faults {
		if fault.Status != "restored" || fault.Error != "" {
			collector.add("fault-incomplete", "critical", "fault:"+fault.ID, fmt.Sprintf("kind=%s status=%s error=%s", fault.Kind, fault.Status, fault.Error), when)
		}
		if fault.Kind == "process-restart" && fault.AppliedBlock != 0 {
			for _, target := range fault.Targets {
				expectedRestarts[target]++
			}
		}
	}
	startProcesses, currentProcesses := processStates(start), processStates(current)
	if len(startProcesses) != 0 || len(currentProcesses) != 0 {
		for id, before := range startProcesses {
			after, present := currentProcesses[id]
			if !present {
				collector.add("process-topology", "critical", "process:"+id, "process disappeared during scenario", when)
				continue
			}
			delta := after.Restarts - before.Restarts
			if delta != expectedRestarts[id] {
				collector.add("unexpected-restart", "critical", "process:"+id, fmt.Sprintf("restart_delta=%d scheduled=%d start=%d end=%d", delta, expectedRestarts[id], before.Restarts, after.Restarts), when)
			}
		}
		for id := range currentProcesses {
			if _, present := startProcesses[id]; !present {
				collector.add("process-topology", "critical", "process:"+id, "process appeared during scenario", when)
			}
		}
	}

	if adversaries != nil {
		for _, actor := range adversaries.Actors {
			if actor.Errors != 0 {
				collector.add("adversary-actor-error", "critical", "adversary:"+actor.ID, fmt.Sprintf("errors=%d error_rate_ppm=%d last_detail=%s", actor.Errors, actor.ErrorRatePPM, actor.LastDetail), when)
			}
		}
		for _, vector := range adversaries.Vectors {
			if vector.Status == "fail" {
				collector.add("adversary-vector-failure", "critical", "adversary-vector:"+vector.ID, fmt.Sprintf("coverage=%s samples=%d errors=%d p99_ms=%d", vector.ConcurrentCoverage, vector.SampleFloor, vector.Errors, vector.MaximumP99LatencyMilliseconds), when)
			}
		}
	}

	entries := make([]ScenarioAnomaly, 0, len(collector.entries))
	for _, entry := range collector.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	status := "clean"
	if len(entries) != 0 {
		status = "open"
	}
	return &ScenarioAnomalyLedger{Schema: "urnetwork-sim-anomaly-ledger-v1", Release: "1.0", RunID: runID, GeneratedAt: when, Status: status, Entries: entries}
}

func attachScenarioAnomalyGate(result *ScenarioResult, generatedAt time.Time, start, current *ScenarioObservation, history ...*ScenarioObservation) {
	assertions := result.Assertions[:0]
	for _, assertion := range result.Assertions {
		if assertion.ID != anomalyGateAssertionID {
			assertions = append(assertions, assertion)
		}
	}
	result.Assertions = assertions
	result.Anomalies = buildScenarioAnomalyLedger(result.RunID, generatedAt, start, current, result.Assertions, result.Faults, result.Adversaries, history...)
	observationHash := ""
	if current != nil {
		observationHash = current.ObservationHash
	}
	startedAt := result.StartedAt
	started, err := time.Parse(time.RFC3339Nano, result.StartedAt)
	if err != nil {
		started = generatedAt
	}
	message := "no unexpected anomalies"
	if len(result.Anomalies.Entries) != 0 {
		message = fmt.Sprintf("%d open anomalies; see anomalies.json", len(result.Anomalies.Entries))
	}
	result.Assertions = append(result.Assertions, AssertionRecord{
		ID: anomalyGateAssertionID, Passed: len(result.Anomalies.Entries) == 0, Message: message,
		StartedAt: startedAt, CompletedAt: generatedAt.UTC().Format(time.RFC3339Nano),
		DurationSeconds: generatedAt.Sub(started).Seconds(), ObservationHash: observationHash,
	})
	sort.Slice(result.Assertions, func(i, j int) bool { return result.Assertions[i].ID < result.Assertions[j].ID })
	result.AssertionCount = len(result.Assertions)
	result.FailedAssertionCount = 0
	for _, assertion := range result.Assertions {
		if !assertion.Passed {
			result.FailedAssertionCount++
		}
	}
	result.Result = "pass"
	if result.FailedAssertionCount != 0 {
		result.Result = "fail"
	}
}
