package main

import (
	"errors"
	"fmt"
	"strings"
)

// A policy revision does not rewrite the one-shot conviction event. Resolve
// its exact finalized and verified action from the active approval's ancestry;
// the observer still checks the canonical receipt and current conviction.
func voluntaryConvictionObservationSourcePlan(cfg *ResolvedConfig, current *SetupPlan, entries []JournalEntry, evidence VoluntaryConvictionEvidence, load func(string) (*SetupPlan, error)) (*SetupPlan, error) {
	if cfg == nil || cfg.Config == nil || current == nil || load == nil ||
		current.DeploymentID != cfg.Config.Deployment.DeploymentID || current.ConfigHash != cfg.ConfigHash ||
		current.PolicyHash != cfg.PolicyHash || current.ChainID != cfg.ChainID || current.Netuid != cfg.Netuid {
		return nil, errors.New("voluntary conviction active approval differs from the configured deployment")
	}
	var action *Action
	for index := range current.Actions {
		if current.Actions[index].ID == voluntaryConvictionActionID {
			if action != nil {
				return nil, errors.New("voluntary conviction active approval repeats its action")
			}
			action = &current.Actions[index]
		}
	}
	if action == nil {
		return nil, errors.New("voluntary conviction active approval has no action")
	}
	allowed := current.allowedPlanHashes()
	for _, finalized := range entries {
		if finalized.Stage != StageFinalized || finalized.DeploymentID != current.DeploymentID ||
			finalized.ActionID != action.ID || !allowed[finalized.PlanHash] || !actionAcceptsIntent(*action, finalized.IntentHash) ||
			!strings.EqualFold(finalized.TransactionHash, evidence.TransactionHash) || finalized.BlockNumber != evidence.FinalizedBlock || !strings.EqualFold(finalized.BlockHash, evidence.FinalizedHash) {
			continue
		}
		for _, verified := range entries {
			if verified.Stage != StageVerified || verified.DeploymentID != finalized.DeploymentID ||
				verified.PlanHash != finalized.PlanHash || verified.ActionID != finalized.ActionID || verified.IntentHash != finalized.IntentHash || verified.Sequence <= finalized.Sequence {
				continue
			}
			source, err := load(verified.PlanHash)
			if err != nil {
				return nil, fmt.Errorf("voluntary conviction source approval: %w", err)
			}
			if _, err := exactCarriedVoluntaryConvictionSourceAction(current, source, *action, verified); err != nil {
				return nil, err
			}
			if err := voluntaryConvictionEvidenceMatches(cfg, source, evidence); err != nil {
				return nil, err
			}
			return source, nil
		}
	}
	return nil, errors.New("voluntary conviction lacks its exact finalized and verified action in the approved lineage")
}
