package main

// An already applied rate amendment may restart its retained processes. Every
// changed action must have its own authenticated completed receipt; this path
// neither applies the amendment nor grants authority to dispatch pending work.
import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func (self *Executor) authenticateRetainedPolicyRateAmendment(ctx context.Context, source *SetupPlan) (bool, error) {
	plan := self.plan
	if source == nil || plan.PolicyRateAmendment == nil || plan.PolicyRateAmendment.PriorPlanHash != source.PlanHash {
		return false, nil
	}
	if !policyRateAmendmentAllowsAncestor(plan, source) || self.cfg.PolicyHash != plan.PolicyHash {
		return false, errors.New("retained rate amendment changed its exact policy source")
	}
	if err := validatePlanBudget(plan); err != nil {
		return false, err
	}
	wantPrior := append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
	if !slices.Equal(plan.PriorPlanHashes, wantPrior) {
		return false, errors.New("retained rate amendment changed its approval lineage")
	}
	// Only the recorded policy revision, finalized planning facts, and its
	// receipt-accounted action/spend delta may differ. Custody, deployment,
	// allowances, source capacity and every unrelated approval stay exact.
	normalized := *plan
	normalized.ConfigHash, normalized.PolicyHash = source.ConfigHash, source.PolicyHash
	normalized.PolicyRateAmendment, normalized.PriorPlanHashes = source.PolicyRateAmendment, source.PriorPlanHashes
	normalized.PlanHash, normalized.GeneratedAt = source.PlanHash, source.GeneratedAt
	normalized.LiveFacts = source.LiveFacts
	normalized.Actions, normalized.MaximumSpend, normalized.SupersededSpend = source.Actions, source.MaximumSpend, source.SupersededSpend
	if !evidenceRelayContinuationSameJSON(&normalized, source) {
		return false, errors.New("retained rate amendment changed immutable custody or allowance")
	}
	entries := self.journal.Entries()
	verified := newCarriedPreparationIndex(plan, entries)
	readPost := self.carriedPreparationPostconditionReader(ctx, readValidatorEvidenceHistoricalPlan)
	authenticate := func(action Action) error {
		entry, ok := verified.find(action, true)
		if !ok {
			return fmt.Errorf("retained rate amendment action %s is not completed", action.ID)
		}
		return self.authenticateProvisionalReceiptWithReader(action, entry, readPost)
	}
	original := planActionIndex(source)
	current := planActionIndex(plan)
	if len(original) != len(source.Actions) || len(current) != len(plan.Actions) {
		return false, errors.New("retained rate amendment has duplicate actions")
	}
	for _, action := range plan.Actions {
		old, present := original[action.ID]
		if present && reflect.DeepEqual(old, action) {
			continue
		}
		allowed := policyRateAmendmentSetupRepair(plan, action) ||
			action.ID == "topology.launch" && action.Kind == "local" && spendIsZero(action.Spend) ||
			(action.ID == "config.render" || action.ID == "validator.reserve-majority" || strings.HasPrefix(action.ID, "alpha.repair.validator.1.")) && provisionalLiveSetupRepair(action)
		if !allowed {
			return false, fmt.Errorf("retained rate amendment changed unrelated action %s", action.ID)
		}
		if err := authenticate(action); err != nil {
			return false, err
		}
	}
	for _, action := range source.Actions {
		if _, present := current[action.ID]; present {
			continue
		}
		if !strings.HasPrefix(action.ID, "alpha.repair.validator.1.") || !provisionalLiveSetupRepair(action) {
			return false, fmt.Errorf("retained rate amendment removed unrelated action %s", action.ID)
		}
		if _, completed := verified.find(action, true); completed {
			if err := authenticate(action); err != nil {
				return false, err
			}
			continue
		}
		// A later approved reserve tranche may replace a source repair which
		// never started. Its absence is not a completed transfer: require no
		// journal reference at any stage, including foreign approvals and
		// accepted aliases, and retain the current reserve verification barrier.
		if action.Parameters[alphaRepairReserveShareParameter] != "true" {
			return false, fmt.Errorf("retained rate amendment removed incomplete non-reserve repair %s", action.ID)
		}
		for _, entry := range entries {
			if entry.ActionID == action.ID || actionAcceptsIntent(action, entry.IntentHash) {
				return false, fmt.Errorf("retained rate amendment removed repair %s has execution evidence", action.ID)
			}
		}
		reserve, present := current["validator.reserve-majority"]
		if !present || !provisionalLiveSetupRepair(reserve) {
			return false, errors.New("retained rate amendment has no current reserve verification")
		}
		if err := authenticate(reserve); err != nil {
			return false, err
		}
	}
	if !slices.Equal(entries, self.journal.Entries()) {
		return false, errors.New("retained rate amendment journal changed during authentication")
	}
	return true, ctx.Err()
}
