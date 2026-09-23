// Historical campaign evidence belongs to its original approval. Only an
// explicit provisional continuation may inspect an approved ancestor under its
// original configuration hash; that read never admits it as the current run.
package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/urfoundation/sn/protocol"
)

// An authenticated older chain needs a new, separately signed recovery. This
// marker is returned only after the full retained chain has been validated.
var errScenarioCampaignHistoricalLineage = errors.New("scenario campaign belongs to an authenticated prior approval and requires a new recovery")

// One read traversal shares immutable decoded approvals. It is not a durable
// cache and is confined to the phase lock or the caller's proof witness fence.
type scenarioCampaignLineageReader struct {
	cfg      *ResolvedConfig
	stateDir string
	roles    *RoleSecrets
	planHash string
	current  *SetupPlan
	plans    *scenarioCampaignPlanLookup
}

// Ordinary current-attempt reads remain strict. Historical admission requires
// the exact current provisional approval, including its operational inputs.
func newScenarioCampaignLineageReader(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string) (*scenarioCampaignLineageReader, error) {
	self := &scenarioCampaignLineageReader{cfg: cfg, stateDir: stateDir, roles: roles, planHash: planHash, plans: &scenarioCampaignPlanLookup{stateDir: stateDir}}
	if !provisionalResumeEnabled(cfg) {
		return self, nil
	}
	if err := validateScenarioCampaignLineageAdmission(cfg, planHash); err != nil {
		return nil, err
	}
	current, err := loadRuntimePersistedPlan(cfg, stateDir)
	if err != nil || current.PlanHash != planHash {
		return nil, errors.Join(errors.New("historical campaign lineage current approval is unavailable"), err)
	}
	self.current = current
	return self, nil
}

// Invocation-only approval fields are not exported in ResolvedConfig JSON;
// check them before both cold reads and warm proof-cache hits.
func validateScenarioCampaignLineageAdmission(cfg *ResolvedConfig, planHash string) error {
	if provisionalResumeEnabled(cfg) && (cfg.provisionalResume.Record.PlanHash != planHash || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance) {
		return errors.New("historical campaign lineage requires the exact provisional non-accepting approval")
	}
	return nil
}

// Approved changes may replace software, limits and configuration. Chain,
// custody, governed policy and installed contracts must describe the same run.
func scenarioCampaignLineagePlansMatch(current, prior *SetupPlan) bool {
	return current != nil && prior != nil && (current.PlanHash == prior.PlanHash || current.allowedPlanHashes()[prior.PlanHash]) &&
		current.DeploymentID == prior.DeploymentID && current.ChainID == prior.ChainID && current.Netuid == prior.Netuid &&
		current.GenesisHash == prior.GenesisHash && current.Owner == prior.Owner && (current.PolicyHash == prior.PolicyHash || policyRateAmendmentAllowsAncestor(current, prior)) &&
		reflect.DeepEqual(current.Roles, prior.Roles) && contractDeploymentAddressesEqual(current.Deployment, prior.Deployment)
}

// The unsigned selector chooses only an approved plan to try. The ordinary
// signed reader then authenticates the complete envelope against that plan.
func (self *scenarioCampaignLineageReader) read(path string) (*scenarioCampaignAttempt, []byte, error) {
	if self.current == nil {
		return readScenarioCampaignAttemptAt(self.cfg, self.stateDir, self.roles, self.planHash, "release-1.0", path)
	}
	relative, err := filepath.Rel(self.stateDir, path)
	if err != nil {
		return nil, nil, err
	}
	raw, err := readValidatorEvidenceHistoricalFile(self.stateDir, filepath.ToSlash(relative), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, nil, err
	}
	var envelope ReleaseEvidenceEnvelope
	var payload scenarioCampaignAttemptPayload
	if err := decodeStrictJSONBytes(raw, &envelope); err != nil {
		return nil, nil, err
	}
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return nil, nil, err
	}
	if payload.PlanHash != self.planHash && !self.current.allowedPlanHashes()[payload.PlanHash] {
		return nil, nil, errors.New("historical campaign plan is not an approved ancestor")
	}
	plan, _, err := self.plans.read(self.stateDir, payload.PlanHash)
	if err != nil {
		return nil, nil, err
	}
	if !scenarioCampaignLineagePlansMatch(self.current, plan) || payload.ConfigHash != plan.ConfigHash || payload.PolicyHash != plan.PolicyHash {
		return nil, nil, errors.New("historical campaign differs from its approved configuration, policy, deployment or custody")
	}
	cfg := self.cfg
	if plan.PlanHash != self.planHash {
		if payload.AcceptanceBoundary != nil && (payload.AcceptanceInvalidatedAt == "" || payload.AcceptanceInvalidation == "") {
			return nil, nil, errors.New("historical campaign acceptance is not terminally invalidated")
		}
		view := *self.cfg
		view.ConfigHash = plan.ConfigHash
		view.PolicyHash = plan.PolicyHash
		if self.current.PolicyHash != plan.PolicyHash {
			policy := self.current.PolicyRateAmendment.Previous
			policy.Deposit.Tiers = append([]protocol.DepositTier(nil), policy.Deposit.Tiers...)
			view.Policy = &policy
		}
		view.readOnlyAudit = true
		cfg = &view
	}
	return readScenarioCampaignAttemptAtContext(cfg, self.stateDir, self.roles, plan.PlanHash, "release-1.0", path, plan.PlanHash != self.planHash)
}

// A signed failed boundary is an archived definition commitment, not a claim
// that today's checks passed. Validate its original fault geometry and state
// without rebuilding a different schedule from the current executable.
func validateHistoricalScenarioCampaignFaults(window *ScenarioAcceptanceWindow, records []ScenarioFaultRecord) error {
	seenKVs := map[string]bool{}
	for _, record := range records {
		if record.ID == "" || record.Kind == "" || len(record.Targets) == 0 || seenKVs[record.ID] || record.TriggerBlock <= window.StartBlock || record.RestoreBlock <= record.TriggerBlock || (record.RestoreCondition == "" && record.MinimumDurationBlocks != 0) || (record.RestoreCondition != "" && record.MinimumDurationBlocks == 0) || record.MinimumDurationBlocks > record.RestoreBlock-record.TriggerBlock {
			return fmt.Errorf("historical campaign fault %q has invalid signed schedule geometry", record.ID)
		}
		seenKVs[record.ID] = true
		if err := validateScenarioCampaignFaultState(window, record); err != nil {
			return err
		}
	}
	return nil
}

// Every cross-approval edge must move forward in its own signed plan lineage,
// even if the current approval lists both endpoints among its ancestors.
func validateScenarioCampaignLineageEdge(attempt, prior *scenarioCampaignAttempt) error {
	if attempt == nil || prior == nil {
		return errors.New("campaign recovery lineage edge is absent")
	}
	return validateScenarioCampaignLineageEdgeWithPlans(attempt, prior, &scenarioCampaignPlanLookup{stateDir: attempt.stateDir})
}

// The same authenticated approvals serve the signed envelope and its edge.
func validateScenarioCampaignLineageEdgeWithPlans(attempt, prior *scenarioCampaignAttempt, plans *scenarioCampaignPlanLookup) (resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, plans.check()) }()
	if attempt == nil || prior == nil {
		return errors.New("campaign recovery lineage edge is absent")
	}
	if attempt.payload.PlanHash == prior.payload.PlanHash {
		if attempt.payload.ConfigHash != prior.payload.ConfigHash || attempt.payload.PolicyHash != prior.payload.PolicyHash {
			return errors.New("campaign recovery mixed configuration or policy within one approval")
		}
		return nil
	}
	current, _, err := plans.read(attempt.stateDir, attempt.payload.PlanHash)
	if err != nil {
		return err
	}
	previous, _, err := plans.read(attempt.stateDir, prior.payload.PlanHash)
	if err != nil {
		return err
	}
	if !scenarioCampaignLineagePlansMatch(current, previous) || current.ConfigHash != attempt.cfg.ConfigHash || current.PolicyHash != attempt.cfg.PolicyHash || previous.ConfigHash != prior.payload.ConfigHash || previous.PolicyHash != prior.payload.PolicyHash {
		return fmt.Errorf("campaign recovery does not follow compatible approved lineage %s -> %s", prior.payload.PlanHash, attempt.payload.PlanHash)
	}
	return nil
}
