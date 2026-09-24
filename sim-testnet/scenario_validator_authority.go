package main

import (
	"context"
	"errors"
	"fmt"
)

// Local generation, intent and proof readers authenticate the original plan.
// The transport derivative remains on the probe for chain reads and never
// becomes persisted approval or a substitute for the original source files.
func (self *liveScenarioProbe) inspectValidators(ctx context.Context, operators []OperatorObservation) ([]ValidatorObservation, error) {
	if ctx == nil || self == nil || self.cfg == nil || self.cfg.Config == nil {
		return nil, errors.New("scenario validator observation has no configuration owner")
	}
	authorized := self.cfg
	if self.authorizedCfg != nil {
		if err := validateCampaignRPCTransport(self.authorizedCfg, self.cfg); err != nil {
			return nil, fmt.Errorf("scenario validator observation transport: %w", err)
		}
		authorized = self.authorizedCfg
	}
	observations := make([]ValidatorObservation, 0, authorized.Config.Topology.Validators)
	for validatorId := 1; validatorId <= authorized.Config.Topology.Validators; validatorId++ {
		var observation ValidatorObservation
		if provisionalResumeEnabled(authorized) {
			observation = inspectProvisionalValidatorIntent(ctx, authorized, self.stateDir, validatorId)
		} else if finalUsesEvidenceV2(authorized) {
			// Campaign routing only changes EVM transport. Strict native reads
			// keep their identical operational Substrate route.
			observation = inspectValidatorIntentV2(ctx, authorized, self.stateDir, validatorId)
		} else {
			observation = inspectValidatorIntent(self.stateDir, validatorId, authorized.Config.Topology.HeadSlots, authorized.Config.Topology.fleetCandidates())
		}
		if self.pathProofs == nil {
			self.pathProofs = newDurableScenarioPathProofCache(authorized, self.stateDir)
		}
		var err error
		observation.PathProofCounts, err = inspectValidatorPathProofsCached(ctx, authorized, self.stateDir, validatorId, operators, self.pathProofs)
		if err != nil {
			if observation.Error == "" {
				observation.Error = err.Error()
			} else {
				observation.Error += "; " + err.Error()
			}
		}
		observations = append(observations, observation)
	}
	return observations, nil
}
