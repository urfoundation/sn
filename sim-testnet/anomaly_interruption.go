// An interrupted campaign cannot claim acceptance. Its untriggered tests are
// consequences of the recorded stop, not independent production defects.
package main

import "strings"

// Only reporting metadata changes. Every assertion, raw fault/vector and
// anomaly stays retained, and the unchanged final gate still rejects the run.
func annotateInterruptedScenarioAnomalies(result *ScenarioResult) {
	if result == nil || result.Anomalies == nil || result.AcceptanceWindow == nil || result.EndHead.Number == 0 || result.EndHead.Number >= result.AcceptanceWindow.TerminalBlock {
		return
	}
	interrupted := false
	for _, assertion := range result.Assertions {
		interrupted = interrupted || assertion.ID == "acceptance_interval_observed" && !assertion.Passed
	}
	if !interrupted {
		return
	}
	rootCause := ""
	for _, anomaly := range result.Anomalies.Entries {
		if anomaly.Source == "assertion:scenario_context" {
			rootCause = anomaly.ID
			break
		}
	}
	if rootCause == "" {
		return
	}
	pendingKVs := map[string]bool{}
	firstTrigger := uint64(0)
	for _, fault := range result.Faults {
		if fault.PreAcceptance {
			continue
		}
		if fault.TriggerBlock == 0 || fault.AppliedBlock != 0 || fault.RestoredBlock != 0 || fault.ArmedBlock != 0 {
			return
		}
		if firstTrigger == 0 || fault.TriggerBlock < firstTrigger {
			firstTrigger = fault.TriggerBlock
		}
		if fault.Status == "pending" && fault.Error == "" {
			pendingKVs["fault:"+fault.ID] = true
		}
	}
	if firstTrigger == 0 || result.EndHead.Number >= firstTrigger {
		return
	}
	if result.Adversaries != nil {
		actorKVs := map[string]AdversaryActorEvidence{}
		for _, actor := range result.Adversaries.Actors {
			actorKVs[actor.ID] = actor
		}
		for _, vector := range result.Adversaries.Vectors {
			unexercised := vector.Status == "fail" && vector.SampleFloor == 0 && vector.Errors == 0 && len(vector.ActorIDs) != 0
			for _, actorId := range vector.ActorIDs {
				actor, exists := actorKVs[actorId]
				unexercised = unexercised && exists && actor.Samples == 0 && actor.ControlSamples == 0 && actor.AttackSamples == 0 && actor.Successful == 0 && actor.ExpectedRejections == 0 && actor.Errors == 0 && actor.Requests == 0 && len(actor.Metrics) == 0
			}
			if unexercised {
				pendingKVs["adversary-vector:"+vector.ID] = true
			}
		}
	}
	for index := range result.Anomalies.Entries {
		entry := &result.Anomalies.Entries[index]
		if !pendingKVs[entry.Source] || entry.Class != "fault-incomplete" && entry.Class != "adversary-vector-failure" {
			continue
		}
		entry.Severity, entry.Status = "info", "derived"
		entry.DerivedFrom = rootCause
		entry.Disposition = "not-exercised-before-interruption"
		entry.RootCause = "campaign stopped before the first scheduled trigger; " + strings.TrimPrefix(entry.Source, "fault:") + " was not exercised"
	}
}
