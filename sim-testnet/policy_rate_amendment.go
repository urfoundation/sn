// A rate amendment preserves settlement geometry and every existing custody
// liability. Its two complete policy documents remain part of plan approval.
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/urfoundation/sn/protocol"
)

const policyRateAmendmentSchema = "urnetwork-sim-policy-rate-amendment-v1"

// The predecessor approval and both hashes are independently reconstructed;
// the coordinator assigns the future effective epoch when scheduling.
type PolicyRateAmendment struct {
	Schema        string          `json:"schema"`
	PriorPlanHash string          `json:"prior_plan_hash"`
	Previous      protocol.Policy `json:"previous_policy"`
	Next          protocol.Policy `json:"next_policy"`
}

// Share the exact governed transition with independent runtime consumers.
func validateFuturePolicyRateAmendment(previous, next *protocol.Policy) error {
	return protocol.ValidateTestnetRateAmendment(previous, next)
}

// A later software approval may carry the same amendment but cannot replace
// its original predecessor or policy documents.
func validatePolicyRateAmendmentPlan(plan *SetupPlan) error {
	if plan == nil || plan.PolicyRateAmendment == nil {
		return errors.New("rate amendment approval is absent")
	}
	amendment := plan.PolicyRateAmendment
	if amendment.Schema != policyRateAmendmentSchema || !validCanonicalHashHex(amendment.PriorPlanHash) || !plan.allowedPlanHashes()[amendment.PriorPlanHash] {
		return errors.New("rate amendment lacks its exact predecessor approval")
	}
	if err := validateFuturePolicyRateAmendment(&amendment.Previous, &amendment.Next); err != nil {
		return err
	}
	hash, err := amendment.Next.HashHex()
	if err != nil || !strings.EqualFold(hash, plan.PolicyHash) {
		return errors.Join(errors.New("rate amendment successor policy differs from the approval"), err)
	}
	return nil
}

// Historical admission is confined to approved ancestors whose exact old
// policy is retained by the amendment. It never changes current validation.
func policyRateAmendmentAllowsAncestor(current, prior *SetupPlan) bool {
	if current == nil || prior == nil || validatePolicyRateAmendmentPlan(current) != nil || !current.allowedPlanHashes()[prior.PlanHash] {
		return false
	}
	hash, err := current.PolicyRateAmendment.Previous.HashHex()
	return err == nil && strings.EqualFold(hash, prior.PolicyHash)
}

// Runtime configuration receives an owned copy after the approved plan and
// current policy agree. A historical activation cannot substitute live policy.
func configWithPolicyRateAmendment(cfg *ResolvedConfig, plan *SetupPlan) (*ResolvedConfig, error) {
	if cfg == nil || cfg.Policy == nil || plan == nil || cfg.PolicyHash != plan.PolicyHash {
		return nil, errors.New("rate amendment runtime configuration differs from approval")
	}
	if err := validatePolicyRateAmendmentPlan(plan); err != nil {
		return nil, err
	}
	hash, err := cfg.Policy.HashHex()
	if err != nil || hash != cfg.PolicyHash {
		return nil, errors.Join(errors.New("rate amendment current policy document differs"), err)
	}
	copy := *cfg
	previous := plan.PolicyRateAmendment.Previous
	previous.Deposit.Tiers = append([]protocol.DepositTier(nil), previous.Deposit.Tiers...)
	copy.previousPolicy = &previous
	return &copy, nil
}

// Immutable history may retain the approved predecessor hash. Current
// decisions and deposits still compare only cfg.PolicyHash at their own epoch.
func policyRateAmendmentHistoryHash(cfg *ResolvedConfig, hash string) bool {
	if cfg == nil {
		return false
	}
	if strings.EqualFold(hash, cfg.PolicyHash) {
		return true
	}
	if cfg.previousPolicy == nil || validateFuturePolicyRateAmendment(cfg.previousPolicy, cfg.Policy) != nil {
		return false
	}
	previousHash, err := cfg.previousPolicy.HashHex()
	return err == nil && strings.EqualFold(hash, previousHash)
}

// Funding ceilings and custody roles are identical in the exact rate-only
// amendment, so an original campaign allocation retains its signed hash.
func policyRateAmendmentFundingHash(plan *SetupPlan, hash string) bool {
	if plan == nil {
		return false
	}
	if strings.EqualFold(hash, plan.PolicyHash) {
		return true
	}
	if validatePolicyRateAmendmentPlan(plan) != nil {
		return false
	}
	previousHash, err := plan.PolicyRateAmendment.Previous.HashHex()
	return err == nil && strings.EqualFold(hash, previousHash)
}

// Only the reviewed future schedule and its read-only activation wait may
// cross live setup adoption. Transaction recovery and budgets remain ordinary.
func policyRateAmendmentSetupRepair(plan *SetupPlan, action Action) bool {
	if validatePolicyRateAmendmentPlan(plan) != nil || action.Target != plan.Deployment.CoordinatorProxy.Hex() || action.Parameters["policy_hash"] != plan.PolicyHash {
		return false
	}
	if action.ID == "policy.await-bootstrap" {
		return action.Kind == "evm-read" && spendIsZero(action.Spend)
	}
	return action.ID == "policy.schedule-bootstrap" && action.Kind == "evm-transaction" && !action.Spend.EVMGasWei.IsZero() &&
		action.Spend.TAORao == 0 && action.Spend.AlphaRao == 0 && action.Spend.Registrations == 0 && action.Spend.SubnetCreations == 0
}

// The activation signatures and deployed constructor calldata retain their
// original policy forever. Only a durably completed source may be carried.
func preservePolicyRateAmendmentSetup(revised, prior *SetupPlan, entries []JournalEntry) error {
	if !policyRateAmendmentAllowsAncestor(revised, prior) && (revised == nil || prior == nil || revised.PolicyHash != prior.PolicyHash || validatePolicyRateAmendmentPlan(revised) != nil) {
		return errors.New("rate amendment setup lacks exact original policy lineage")
	}
	if err := preserveVerifiedBaselineDeploymentActions(revised, prior, entries); err != nil {
		return err
	}
	verifiedKVs := map[string]bool{}
	for _, entry := range entries {
		if prior.allowedPlanHashes()[entry.PlanHash] && entry.Stage == StageVerified {
			verifiedKVs[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	for index := range revised.Actions {
		action := &revised.Actions[index]
		if !strings.HasPrefix(action.ID, "evidence.activate.") && action.ID != runtimeEvidenceActivationBoundaryActionId {
			continue
		}
		original, err := exactPlanActionByID(prior, action.ID)
		if err != nil || !verifiedKVs[original.ID+"\x00"+original.IntentHash] {
			return errors.Join(fmt.Errorf("rate amendment requires completed original activation %s", action.ID), err)
		}
		*action = original
	}
	return nil
}
