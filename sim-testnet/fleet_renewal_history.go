package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The renewal authorizes a new generation only after every predecessor is
// pinned in its signed snapshot. Previous fleet postconditions therefore replay
// at their own canonical receipt checkpoint. Other setup actions keep their
// existing live-state requirements.
func fleetRenewalHistoricalActionFleets(cfg *ResolvedConfig, action Action) ([]int, error) {
	if isFleetRenewalAction(action) {
		return nil, nil
	}
	id := action.ID
	if strings.HasPrefix(id, "fleet.refresh.batch.") || strings.HasPrefix(id, "fleet.install.batch.") {
		batch := suffixInt(id)
		if batch < 1 {
			return nil, errors.New("renewed historical batch index is invalid")
		}
		first, last := (batch-1)*fleetRefreshBatchSize+1, min(batch*fleetRefreshBatchSize, cfg.Config.Topology.HeadFleets)
		if first > last {
			return nil, errors.New("renewed historical batch range is invalid")
		}
		result := []int{}
		for fleet := first; fleet <= last; fleet++ {
			result = append(result, fleet)
		}
		return result, nil
	}
	for _, prefix := range []string{"fleet.commitment.", "fleet.refresh.commitment.", "fleet.mirror."} {
		if strings.HasPrefix(id, prefix) {
			fleet, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
			if err != nil || fleet < 1 || fleet > cfg.Config.Topology.fleetCandidates() {
				return nil, errors.New("renewed historical fleet index is invalid")
			}
			return []int{fleet}, nil
		}
	}
	if strings.HasPrefix(id, "fleet.bind.") {
		fleet, _, err := fleetBindingActionIndices(id)
		if err != nil || fleet < 1 || fleet > cfg.Config.Topology.fleetCandidates() {
			return nil, errors.New("renewed historical member index is invalid")
		}
		return []int{fleet}, nil
	}
	for _, family := range []struct {
		prefix string
		fleet  int
	}{{"lifecycle.prepare.target.", fleetLifecycleTargetFleet}, {"lifecycle.prepare.companion.", fleetLifecycleCompanionFleet}, {"lifecycle.provider.", fleetLifecycleTargetFleet}, {"lifecycle.terminal.", fleetLifecycleCompanionFleet}} {
		if strings.HasPrefix(id, family.prefix) {
			suffix := strings.TrimPrefix(id, family.prefix)
			if suffix == "commitment" || suffix == "mirror" || suffix == "installed" || strings.HasPrefix(suffix, "bind.") {
				return []int{family.fleet}, nil
			}
		}
	}
	return nil, nil
}

func (e *Executor) fleetRenewalHistoricalSource(action Action, verified JournalEntry, record *ActionPostcondition) (*Executor, bool, error) {
	if len(e.plan.FleetRenewals) == 0 {
		return nil, false, nil
	}
	fleets, err := fleetRenewalHistoricalActionFleets(e.cfg, action)
	if err != nil || len(fleets) == 0 {
		return nil, false, err
	}
	if verified.Stage != StageVerified || record == nil || record.PlanHash != verified.PlanHash || record.ActionID != action.ID || record.IntentHash != action.IntentHash {
		return nil, false, errors.New("renewed historical action lacks its exact source receipt")
	}
	hash, err := canonicalHashHex(record)
	if err != nil || hash != verified.PostconditionHash {
		return nil, false, errors.New("renewed historical postcondition hash differs from journal")
	}
	for _, renewal := range e.plan.FleetRenewals {
		complete := true
		for _, fleet := range fleets {
			if fleet > len(renewal.Fleets) {
				complete = false
				break
			}
			for _, operation := range []string{"commitment", "mirror", "bind"} {
				members := []int{0}
				if operation == "bind" {
					members = nil
					for index := range renewal.Fleets[fleet-1].Members {
						members = append(members, index+1)
					}
				}
				for _, member := range members {
					id := fleetRenewalActionID(renewal.Round, fleet, operation, member)
					next, err := e.planAction(id)
					if err != nil {
						return nil, false, err
					}
					entry, ok := e.verifiedActionEntry(next)
					if !ok || entry.Sequence <= verified.Sequence {
						complete = false
						break
					}
					if _, err := e.readPersistedPostcondition(entry); err != nil {
						return nil, false, fmt.Errorf("renewal successor %s: %w", id, err)
					}
					if _, err := e.fleetRenewalFinalized(next); err != nil {
						return nil, false, err
					}
				}
				if !complete {
					break
				}
			}
			if !complete {
				break
			}
		}
		if !complete {
			continue
		}
		source, err := readValidatorEvidenceHistoricalPlan(e.stateDir, verified.PlanHash)
		if err != nil {
			return nil, false, err
		}
		if !e.plan.allowedPlanHashes()[source.PlanHash] || source.PlanHash != verified.PlanHash {
			return nil, false, errors.New("renewed historical source is outside approved ancestry")
		}
		priorAction, err := exactPlanActionByID(source, action.ID)
		if err != nil || priorAction.IntentHash != action.IntentHash {
			return nil, false, errors.New("renewed historical action differs from approved ancestry")
		}
		copy := *e
		copy.plan = source
		return &copy, true, nil
	}
	return nil, false, nil
}
