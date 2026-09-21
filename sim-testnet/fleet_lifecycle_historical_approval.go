// A provisional no-mutation lifecycle keeps its original approval and bytes.
// Reading that history never carries preparation or acceptance into a new run.
package main

import (
	"errors"
	"os"
)

type provisionalLifecycleAncestorApproval struct {
	CurrentPlanHash   string
	CurrentConfigHash string
	SourceRunId       string
	SourcePlan        *SetupPlan
}

// Archive provenance remains readable after the new acceptance starts. Unlike
// adoption, this check grants no permission to resume an old acceptance window.
func authenticateProvisionalLifecycleAncestor(attempt *scenarioCampaignAttempt, evidence *FleetLifecycleEvidence) (*SetupPlan, error) {
	if attempt == nil || evidence == nil || attempt.payload.Phase != "release-1.0" || attempt.payload.Recovery == nil ||
		evidence.ProvisionalBypass == nil || evidence.Stage != fleetLifecycleStageReleaseHandoff || evidence.RunID == "" ||
		evidence.RunID == attempt.payload.RunID || fleetLifecycleHasProductionState(evidence) {
		return nil, errors.New("provisional lifecycle has no distinct non-mutating release ancestor")
	}
	if err := validateFleetLifecycleProvisionalBypassAuthority(attempt.cfg, &SetupPlan{PlanHash: attempt.payload.PlanHash}); err != nil {
		return nil, err
	}
	ancestors, err := scenarioCampaignRecoveryAncestors(attempt, validateScenarioCampaignRecovery)
	if err != nil || !ancestors[evidence.RunID] {
		return nil, errors.Join(errors.New("provisional lifecycle run is not an authenticated recovery ancestor"), err)
	}
	if _, err := os.Lstat(scenarioCampaignAttemptPath(attempt.stateDir, "production-soak")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(errors.New("provisional lifecycle history has a production descendant"), err)
	}
	reader, err := newScenarioCampaignLineageReader(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash)
	if err != nil {
		return nil, err
	}
	files, err := scenarioCampaignRecoveryFiles(attempt.stateDir)
	if err != nil {
		return nil, err
	}
	paths := []string{scenarioCampaignSuccessorPath(attempt.stateDir)}
	for _, file := range files {
		paths = append(paths, file.path)
	}
	for _, path := range paths {
		prior, _, err := reader.read(path)
		if err != nil {
			return nil, err
		}
		if prior.payload.RunID != evidence.RunID {
			continue
		}
		if prior.payload.PlanHash != evidence.PlanHash {
			return nil, errors.New("provisional lifecycle changed its original run approval")
		}
		plan := reader.planKVs[prior.payload.PlanHash]
		if !scenarioCampaignLineagePlansMatch(reader.current, plan) ||
			!fleetLifecycleCanonicalEqual(reader.current.FleetLifecycleRenewal, plan.FleetLifecycleRenewal) ||
			!fleetLifecycleCanonicalEqual(evidence.Renewal, plan.FleetLifecycleRenewal) {
			return nil, errors.New("provisional lifecycle changed its approved custody, deployment, policy or renewal")
		}
		return plan, nil
	}
	return nil, errors.New("provisional lifecycle has no exact signed source run")
}

// Admission is established before acceptance, once per lifecycle owner. Later
// observation checks retain that immutable decoded approval while rechecking
// current authority, source identity, custody and the full no-mutation census.
func (self *liveFleetLifecycle) provisionalLifecycleEvidencePlan(phase string, evidence *FleetLifecycleEvidence) (*SetupPlan, error) {
	current := self.executor.plan
	if evidence.PlanHash == current.PlanHash && (phase != "release-1.0" || self.attempt == nil || self.attempt.payload.RunID == evidence.RunID) {
		return current, nil
	}
	if phase != "release-1.0" || self.attempt == nil || self.attempt.payload.PlanHash != current.PlanHash ||
		self.attempt.payload.ConfigHash != self.cfg.ConfigHash || self.attempt.payload.PolicyHash != self.cfg.PolicyHash ||
		current.ConfigHash != self.cfg.ConfigHash || current.PolicyHash != self.cfg.PolicyHash {
		return nil, errors.New("historical lifecycle is outside the exact current release approval")
	}
	if _, err := os.Lstat(scenarioCampaignAttemptPath(self.stateDir, "production-soak")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(errors.New("historical lifecycle has a production descendant"), err)
	}
	proof := self.retainedProvisionalApproval
	if proof == nil || proof.SourcePlan == nil || proof.CurrentPlanHash != current.PlanHash || proof.CurrentConfigHash != self.cfg.ConfigHash ||
		proof.SourceRunId != evidence.RunID || proof.SourcePlan.PlanHash != evidence.PlanHash {
		if err := validateScenarioCampaignRecoveryAncestor(self.attempt, evidence.RunID); err != nil {
			return nil, err
		}
		plan, err := authenticateProvisionalLifecycleAncestor(self.attempt, evidence)
		if err != nil {
			return nil, err
		}
		proof = &provisionalLifecycleAncestorApproval{
			CurrentPlanHash: current.PlanHash, CurrentConfigHash: self.cfg.ConfigHash,
			SourceRunId: evidence.RunID, SourcePlan: plan,
		}
		self.retainedProvisionalApproval = proof
	}
	if !scenarioCampaignLineagePlansMatch(current, proof.SourcePlan) ||
		!fleetLifecycleCanonicalEqual(current.FleetLifecycleRenewal, proof.SourcePlan.FleetLifecycleRenewal) ||
		!fleetLifecycleCanonicalEqual(evidence.Renewal, proof.SourcePlan.FleetLifecycleRenewal) {
		return nil, errors.New("historical lifecycle approval changed after admission")
	}
	return proof.SourcePlan, nil
}
