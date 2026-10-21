//go:build linux || darwin

// Dynamic calls are selected by the approved journal and original signed
// request bytes. A prefix alone never makes an unknown action trustworthy.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Exact plan ownership prevents a predecessor request from being relabeled
// under the current release, even if its slot has the same textual name.
type evidenceRelayRequestKey struct {
	planHash string
	actionId string
}

// Reads each admitted source once, including failed and predecessor calls.
// The caller supplies either bounded state reads or sealed archive reads.
func evidenceRelayRequestsForJournal(plans map[string]*SetupPlan, entries []JournalEntry, read func(evidenceRelayRequestKey) ([]byte, error)) (map[evidenceRelayRequestKey][]byte, error) {
	if read == nil {
		return nil, errors.New("evidence relay request reader is absent")
	}
	result := make(map[evidenceRelayRequestKey][]byte)
	for _, entry := range entries {
		plan := plans[entry.PlanHash]
		if plan == nil || !strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) {
			continue
		}
		if entry.PlanHash != plan.PlanHash || entry.DeploymentID != plan.DeploymentID || !validCanonicalHashHex("0x"+strings.TrimPrefix(entry.ActionID, evidenceRelayActionPrefix)) {
			return nil, errors.New("evidence relay capture journal has a malformed request owner")
		}
		key := evidenceRelayRequestKey{planHash: entry.PlanHash, actionId: entry.ActionID}
		if _, found := result[key]; found {
			continue
		}
		raw, err := read(key)
		if err != nil {
			return nil, err
		}
		action, err := validateEvidenceRelayRequest(plan, entries, raw)
		if err != nil || action.ID != key.actionId {
			return nil, errors.Join(errors.New("evidence relay capture source differs from its admitted slot"), err)
		}
		result[key] = append([]byte(nil), raw...)
	}
	return result, nil
}

// There is no directory walk or guessed path. Journal names are validated
// before the immutable bounded reader opens the exact original request.
func evidenceRelayRequestsFromState(ctx context.Context, stateRoot string, plans map[string]*SetupPlan, entries []JournalEntry) (map[evidenceRelayRequestKey][]byte, error) {
	if ctx == nil || stateRoot == "" {
		return nil, errors.New("evidence relay capture state owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return evidenceRelayRequestsForJournal(plans, entries, func(key evidenceRelayRequestKey) ([]byte, error) {
		name := strings.TrimPrefix(key.actionId, evidenceRelayActionPrefix) + ".json"
		return validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(stateRoot, "evidence-relay", name), evidenceRelayActionBytes)
	})
}

// Checks every supplied source against all durable admissions before use.
// This prevents caller-supplied action maps from becoming new authority.
func evidenceRelayRequestActions(plans map[string]*SetupPlan, entries []JournalEntry, requests map[evidenceRelayRequestKey][]byte) (map[evidenceRelayRequestKey]Action, error) {
	result := make(map[evidenceRelayRequestKey]Action, len(requests))
	for key, raw := range requests {
		plan := plans[key.planHash]
		if plan == nil || plan.PlanHash != key.planHash {
			return nil, errors.New("evidence relay capture has an unapproved source plan")
		}
		action, err := validateEvidenceRelayRequest(plan, entries, raw)
		if err != nil || action.ID != key.actionId {
			return nil, errors.Join(errors.New("evidence relay capture source key differs"), err)
		}
		result[key] = action
	}
	for _, entry := range entries {
		if plans[entry.PlanHash] != nil && strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) {
			if _, found := result[evidenceRelayRequestKey{planHash: entry.PlanHash, actionId: entry.ActionID}]; !found {
				return nil, errors.New("evidence relay capture omits an admitted request")
			}
		}
	}
	return result, nil
}

// Static actions retain their existing exact plan lookup. Only an original,
// independently validated relay action can fill an otherwise missing entry.
func finalJournalActionWithRelay(plan *SetupPlan, entry JournalEntry, actions map[evidenceRelayRequestKey]Action) (Action, error) {
	if plan == nil {
		return Action{}, errors.New("historical release journal plan is absent")
	}
	if strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) {
		for _, planned := range plan.Actions {
			if planned.ID == entry.ActionID {
				return Action{}, errors.New("dynamic evidence relay action appeared in the static plan")
			}
		}
	}
	action, err := exactPlanActionByID(plan, entry.ActionID)
	if err == nil {
		if strings.HasPrefix(action.ID, evidenceRelayActionPrefix) {
			return Action{}, errors.New("dynamic evidence relay action appeared in the static plan")
		}
	} else {
		var found bool
		action, found = actions[evidenceRelayRequestKey{planHash: entry.PlanHash, actionId: entry.ActionID}]
		if !found {
			return Action{}, err
		}
	}
	if plan == nil || entry.PlanHash != plan.PlanHash || entry.DeploymentID != plan.DeploymentID || !actionAcceptsIntent(action, entry.IntentHash) {
		return Action{}, fmt.Errorf("historical release journal action %s is not approved", entry.ActionID)
	}
	return action, nil
}
