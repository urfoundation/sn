package main

// Historical readers admit a corrective transaction only through the exact
// owner-signed carry and its finalized original journal row.
import (
	"errors"
	"fmt"
)

// Builds one action lookup shared by the capture, receipt and timeline readers.
// An original repair is never inferred from an action prefix or proxy address.
func finalHistoricalJournalActions(current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, requests map[evidenceRelayRequestKey][]byte) (map[evidenceRelayRequestKey]Action, error) {
	actions, err := evidenceRelayRequestActions(plans, entries, requests)
	if err != nil {
		return nil, err
	}
	repairs, err := finalCoordinatorRepairCarriedActions(current, plans, entries)
	if err != nil {
		return nil, err
	}
	for key, action := range repairs {
		if _, found := actions[key]; found {
			return nil, fmt.Errorf("historical corrective action %s conflicts with a relay request", action.ID)
		}
		actions[key] = action
	}
	return actions, nil
}

// The offline verifier can authenticate a repair independently of unrelated
// relay requests, whose original bytes are checked by their own reader.
func finalCoordinatorRepairCarriedActions(current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry) (map[evidenceRelayRequestKey]Action, error) {
	actions := make(map[evidenceRelayRequestKey]Action)
	if current == nil || current.CoordinatorRepairCarry == nil {
		return actions, nil
	}
	if err := validateCoordinatorRepairCarryPlan(current); err != nil {
		return nil, err
	}
	carry := current.CoordinatorRepairCarry
	source := plans[carry.Request.Request.PlanHash]
	if source == nil || source.CoordinatorRepairCarry != nil || source.PlanHash != carry.Request.Request.PlanHash {
		return nil, errors.New("historical corrective source plan is absent")
	}
	if err := validateCoordinatorRepairRequest(source, &carry.Request); err != nil {
		return nil, err
	}
	for _, pair := range []struct {
		action Action
		final  JournalEntry
	}{
		{action: carry.Request.Request.Deploy, final: carry.Result.Result.Deploy},
		{action: carry.Request.Request.Activate, final: carry.Result.Result.Activate},
	} {
		matches := 0
		for _, entry := range entries {
			if entry.PlanHash != source.PlanHash || entry.ActionID != pair.action.ID || entry.Stage != StageFinalized {
				continue
			}
			if entry != pair.final {
				return nil, fmt.Errorf("historical corrective action %s differs from signed finalization", pair.action.ID)
			}
			matches++
		}
		if matches != 1 {
			return nil, fmt.Errorf("historical corrective action %s has %d exact finalizations", pair.action.ID, matches)
		}
		key := evidenceRelayRequestKey{planHash: source.PlanHash, actionId: pair.action.ID}
		actions[key] = pair.action
	}
	return actions, nil
}
