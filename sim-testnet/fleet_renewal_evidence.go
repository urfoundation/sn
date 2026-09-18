package main

import (
	"errors"
	"fmt"
	"os"
)

// A completed, journal-authenticated renewal replaces current observation
// paths at its exact activation boundary; all prior public files remain intact.
func fleetRenewalEvidenceDescriptors(cfg *ResolvedConfig, stateDir string, epoch uint64, prior []fleetLifecycleEvidenceDescriptor) ([]fleetLifecycleEvidenceDescriptor, error) {
	plan, err := readFleetCensusPlan(cfg, stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return prior, nil
	}
	if err != nil {
		return nil, err
	}
	return fleetRenewalEvidenceDescriptorsForPlan(cfg, stateDir, epoch, prior, plan)
}

// Reuse the lifecycle reader's authenticated plan and index the journal once
// for the entire renewal, rather than scanning it again for every member.
func fleetRenewalEvidenceDescriptorsForPlan(cfg *ResolvedConfig, stateDir string, epoch uint64, prior []fleetLifecycleEvidenceDescriptor, plan *SetupPlan) ([]fleetLifecycleEvidenceDescriptor, error) {
	if plan == nil {
		return prior, nil
	}
	if len(plan.FleetRenewals) == 0 {
		return prior, nil
	}
	if plan.DeploymentID != cfg.Config.Deployment.DeploymentID || plan.ChainID != cfg.ChainID || plan.Netuid != cfg.Netuid {
		return nil, errors.New("renewal observation belongs to a different deployment")
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return nil, err
	}
	var selected *FleetRenewal
	for index := range plan.FleetRenewals {
		renewal := &plan.FleetRenewals[index]
		if renewal.ValidFromEpoch <= epoch {
			selected = renewal
		}
	}
	if selected == nil {
		return prior, nil
	}
	if len(selected.Fleets) != len(prior) {
		return nil, errors.New("renewal observation has an incomplete candidate fleet set")
	}
	verifiedActionIds := exactVerifiedPlanActionIndex(plan, entries)
	result := make([]fleetLifecycleEvidenceDescriptor, 0, len(selected.Fleets))
	for _, fleet := range selected.Fleets {
		stem := fleetRenewalStem(selected.Round, fleet.Fleet)
		descriptor := fleetLifecycleEvidenceDescriptor{ManifestName: stem + ".json", CommitmentName: stem + ".commitment.json"}
		for index, member := range fleet.Members {
			actionID := fleetRenewalActionID(selected.Round, fleet.Fleet, "bind", index+1)
			if !verifiedActionIds[actionID] {
				return nil, fmt.Errorf("renewal binding %s is not postcondition-verified", actionID)
			}
			descriptor.BindingNames = append(descriptor.BindingNames, fmt.Sprintf("%s-member-%d.binding.json", stem, index+1))
			descriptor.MinerIDs = append(descriptor.MinerIDs, member.Miner)
		}
		result = append(result, descriptor)
	}
	return result, nil
}
