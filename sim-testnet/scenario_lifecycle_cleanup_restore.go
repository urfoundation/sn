// Terminal filter cleanup can reconcile a completed removal after the active
// ledger is gone. The ordinary fault restore admission remains unchanged.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Only this cleanup path may adopt a retained exact removal receipt without
// an active ledger entry. It cannot remove an unrecorded live rule.
type scenarioLifecycleCleanupRestorer interface {
	restoreLifecycleCleanup(context.Context, scenarioFaultSpec) ([]FaultProcessEvidence, error)
}

// An absent active entry admits read-only reconciliation of a completed exact
// removal, never a new control side effect. Existing entries use ordinary restore.
func (self *liveScenarioFaultDriver) restoreLifecycleCleanup(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if ctx == nil || self == nil || self.cfg == nil || self.cfg.Policy == nil {
		return nil, errors.New("provisional lifecycle cleanup restore dependencies are absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected, err := releaseFleetLifecycleFaults(self.cfg, self.cfg.Policy.Settlement.EpochBlocks)
	if err != nil {
		return nil, err
	}
	wanted, err := canonicalHashHex(spec)
	if err != nil {
		return nil, err
	}
	matched := false
	for _, candidate := range expected {
		hash, err := canonicalHashHex(candidate)
		if err != nil {
			return nil, err
		}
		matched = matched || hash == wanted
	}
	if !matched {
		return nil, errors.New("provisional lifecycle cleanup restore is outside the exact two filters")
	}
	active, err := readActiveFaultFile(self.activePath())
	if err != nil {
		return nil, err
	}
	if _, err := activeFaultIndex(active, spec); err == nil {
		return self.Restore(ctx, spec)
	}
	if err := validateFaultActivation(active, spec); err != nil {
		return nil, fmt.Errorf("provisional lifecycle cleanup conflicts with active ownership: %w", err)
	}
	operator, target, rule, err := self.validatorViewRule(spec)
	if err != nil {
		return nil, err
	}
	identity, err := self.validatorViewFilterIdentity(operator)
	if err != nil {
		return nil, err
	}
	process, err := self.validatorViewProcess(operator, target)
	if err != nil {
		return nil, err
	}
	validatorViewFilterMu.Lock()
	defer validatorViewFilterMu.Unlock()
	raw, err := os.ReadFile(validatorViewRestoreReceiptPath(self.stateDir, operator, rule.RuleID))
	if err != nil {
		return nil, fmt.Errorf("provisional lifecycle cleanup requires its exact removal receipt: %w", err)
	}
	var receipt validatorViewRestoreReceipt
	if err := decodeStrictJSONBytes(raw, &receipt); err != nil {
		return nil, err
	}
	if err := validateValidatorViewRestoreReceipt(receipt, identity, rule); err != nil {
		return nil, err
	}
	raw, err = os.ReadFile(verifyAssignmentFilterPath(self.stateDir, operator))
	if err == nil {
		var filter validatorViewFilterFile
		if err := decodeStrictJSONBytes(raw, &filter); err != nil {
			return nil, err
		}
		if err := validateValidatorViewFilter(filter, identity); err != nil {
			return nil, err
		}
		for _, existing := range filter.Rules {
			if existing.RuleID == rule.RuleID {
				return nil, errors.New("provisional lifecycle cleanup receipt still has a live rule without active ownership")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []FaultProcessEvidence{process}, nil
}
