//go:build linux || darwin

// Successor custody is append-only: the original handoff stays immutable and
// each fsynced verified journal row selects its exact versioned postcondition.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

var errPolicyRolloverLocatorMissing = errors.New("committed rollover lost its original handoff locator")

// An absent original locator means no activation only when the durable journal
// agrees. Removing a committed selector can never revert to original inputs.
func requirePolicyRolloverInitialAbsenceV2(stateDir string) error {
	entries, err := readJournalEntries(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.ActionID == "evidence.policy-rollover-handoff" && entry.Stage == StageVerified {
			return errPolicyRolloverLocatorMissing
		}
	}
	return nil
}

// A reference binds the actual authenticated base bytes, never an overlay.
func policyRolloverHandoffReferenceV2(ctx context.Context, handoff *policyRolloverHandoffV2, limit uint64) (validatorcomponent.ReleaseEvidenceV2File, error) {
	if handoff == nil || handoff.sourcePath == "" {
		return validatorcomponent.ReleaseEvidenceV2File{}, errors.New("rollover predecessor has no authenticated locator")
	}
	raw, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, handoff.sourcePath, limit)
	if err != nil || bytesSHA256(raw) != handoff.sourceSHA256 {
		return validatorcomponent.ReleaseEvidenceV2File{}, errors.Join(errors.New("rollover predecessor bytes changed"), err)
	}
	return policyRolloverFile(handoff.sourcePath, raw), nil
}

// Plan validation binds the retained member/config census. Mutation separately
// proves this exact version is still active before any publication or activation.
func validatePolicyRolloverPreviousHandoffV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *policyRolloverPlanV2) error {
	if plan.PreviousHandoff == nil {
		return nil
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, *plan.PreviousHandoff, limit)
	if err != nil {
		return err
	}
	var prior policyRolloverHandoffV2
	if err := decodeStrictJSONBytes(raw, &prior); err != nil {
		return err
	}
	action, err := policyRolloverHandoffActionV2(&policyRolloverPlanV2{PlanHash: prior.PlanHash, DeploymentID: prior.DeploymentID})
	if err != nil {
		return err
	}
	relative, err := postconditionRelativePath(prior.PlanHash, action.ID)
	if err != nil {
		return err
	}
	expected := filepath.Join(stateDir, relative)
	if prior.PreviousHandoff == nil {
		expected = filepath.Join(policyRolloverRoot(stateDir), "handoff.json")
	}
	if plan.PreviousHandoff.Path != expected || prior.Schema != policyRolloverHandoffV2Schema || !prior.Activated || prior.LedgerContinuityClaimed || prior.SourceRoleOverlay != nil || prior.DeploymentID != plan.DeploymentID || prior.Generation >= plan.Generation || prior.CutoffEpoch >= plan.Epoch || len(prior.Validators) != 2 || len(prior.Members) != 4 {
		return errors.New("rollover predecessor changes its immutable locator, generation or cutoff")
	}
	for index, checkpoint := range plan.Validators {
		previous := prior.Validators[index]
		if checkpoint.ValidatorID != previous.ValidatorID || checkpoint.StateDir != previous.StateDir || checkpoint.Config.SHA256 != previous.Config.SHA256 || checkpoint.Config.Bytes != previous.Config.Bytes || len(checkpoint.PreviousActivations) != 2 {
			return errors.New("rollover checkpoint differs from its approved predecessor config or state")
		}
		for memberIndex, activation := range checkpoint.PreviousActivations {
			if activation != prior.Members[index*2+memberIndex].Activation {
				return errors.New("rollover checkpoint changed the preceding activation census")
			}
		}
	}
	return nil
}

// Readers follow only verified activation receipts in journal order. Staged
// files and intents never select a generation; a torn or foreign receipt fails.
func readPolicyRolloverSuccessorsV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, original []byte, entries []JournalEntry) (*policyRolloverHandoffV2, error) {
	path := filepath.Join(policyRolloverRoot(stateDir), "handoff.json")
	current, err := authenticatePolicyRolloverHandoffV2(ctx, cfg, stateDir, base, path, original, entries)
	if err != nil {
		return nil, err
	}
	if current.PreviousHandoff != nil {
		return nil, errors.New("original rollover handoff cannot contain a predecessor")
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	originalPlan := current.PlanHash
	seen := map[string]bool{originalPlan: true}
	originalSeen := false
	for _, entry := range entries {
		if entry.ActionID != "evidence.policy-rollover-handoff" || entry.Stage != StageVerified {
			continue
		}
		if entry.PlanHash == originalPlan && !originalSeen {
			originalSeen = true
			continue
		}
		if !originalSeen || seen[entry.PlanHash] || entry.DeploymentID != base.DeploymentID {
			return nil, errors.New("rollover activation receipt is duplicated or out of order")
		}
		relative, err := postconditionRelativePath(entry.PlanHash, entry.ActionID)
		if err != nil || relative != entry.PostconditionPath {
			return nil, errors.Join(errors.New("rollover successor receipt changed its versioned locator"), err)
		}
		path := filepath.Join(stateDir, relative)
		var next policyRolloverHandoffV2
		raw, err := readRuntimeEvidenceSetupV2(ctx, path, limit, &next)
		if err != nil {
			return nil, err
		}
		reference, err := policyRolloverHandoffReferenceV2(ctx, current, limit)
		if err != nil || next.PreviousHandoff == nil || *next.PreviousHandoff != reference || next.Generation <= current.Generation || next.CutoffEpoch <= current.CutoffEpoch {
			return nil, errors.Join(errors.New("rollover successor receipt does not extend the exact active predecessor"), err)
		}
		selected, err := authenticatePolicyRolloverHandoffV2(ctx, cfg, stateDir, base, path, raw, entries)
		if err != nil {
			return nil, err
		}
		selected.predecessor = current
		current = selected
		seen[entry.PlanHash] = true
	}
	return current, ctx.Err()
}

// Exact retries may already own the active version, but cannot replace another
// successor or borrow a different predecessor's original source waiver.
func validatePolicyRolloverActivePredecessorV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, plan *policyRolloverPlanV2) error {
	active, err := readBasePolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if errors.Is(err, errPolicyRolloverLocatorMissing) && plan.PreviousHandoff == nil {
		// The first activation historically committed before writing its locator.
		// Only that exact receipt, with no successor, may finish a crashed write.
		entries, readErr := readJournalEntries(stateDir)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if entry.ActionID == "evidence.policy-rollover-handoff" && entry.Stage == StageVerified && entry.PlanHash != plan.PlanHash {
				return err
			}
		}
		action, actionErr := policyRolloverHandoffActionV2(plan)
		if actionErr != nil {
			return actionErr
		}
		_, receipt := policyRolloverPriorV2(plan, action, entries)
		if receipt == nil {
			return err
		}
		limit, limitErr := runtimeEvidenceProvisionLimit(cfg)
		if limitErr != nil {
			return limitErr
		}
		relative, pathErr := postconditionRelativePath(plan.PlanHash, action.ID)
		if pathErr != nil || relative != receipt.PostconditionPath {
			return errors.Join(err, pathErr)
		}
		path := filepath.Join(stateDir, relative)
		raw, readErr := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, path, limit)
		if readErr != nil {
			return readErr
		}
		recovered, readErr := authenticatePolicyRolloverHandoffV2(ctx, cfg, stateDir, base, path, raw, entries)
		if readErr != nil || recovered == nil || recovered.PlanHash != plan.PlanHash || recovered.PreviousHandoff != nil {
			return errors.Join(err, readErr)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if active != nil && active.PlanHash == plan.PlanHash {
		active = active.predecessor
	}
	if plan.PreviousHandoff == nil {
		if active != nil {
			return errors.New("fresh rollover approval omitted the active predecessor")
		}
		return nil
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	reference, err := policyRolloverHandoffReferenceV2(ctx, active, limit)
	if err != nil || reference != *plan.PreviousHandoff {
		return errors.Join(errors.New("rollover active predecessor changed after approval"), err)
	}
	return nil
}

// The immutable postcondition precedes the fsynced commit row. Successors need
// no mutable pointer; original activation retains its compatible legacy file.
func activatePolicyRolloverHandoffV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, plan *policyRolloverPlanV2, handoff *policyRolloverHandoffV2, journal *Journal, limit uint64) error {
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return err
	}
	for id := 1; id <= 2; id++ {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return err
		}
	}
	if err := validatePolicyRolloverPlanV2(cfg, base, stateDir, plan); err != nil {
		return err
	}
	if err := validatePolicyRolloverActivePredecessorV2(ctx, cfg, stateDir, base, plan); err != nil {
		return err
	}
	if handoff == nil || handoff.PlanHash != plan.PlanHash || handoff.Generation != plan.Generation || handoff.CutoffEpoch != plan.Epoch || handoff.FirstFullEpoch != plan.Epoch+1 || !reflect.DeepEqual(handoff.PreviousHandoff, plan.PreviousHandoff) {
		return errors.New("rollover activation differs from its exact approved generation")
	}
	handoff.Activated = true
	action, err := policyRolloverHandoffActionV2(plan)
	if err != nil {
		return err
	}
	_, prior := policyRolloverPriorV2(plan, action, journal.Entries())
	if prior == nil {
		if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
			return err
		}
	}
	relative, err := postconditionRelativePath(plan.PlanHash, action.ID)
	if err != nil {
		return err
	}
	path := filepath.Join(stateDir, relative)
	raw, err := writeRuntimeEvidenceSetupV2(ctx, path, handoff, limit)
	if err != nil {
		return err
	}
	preview := append(journal.Entries(), JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: relative, PostconditionHash: policyRolloverFile(path, raw).SHA256})
	if _, err := authenticatePolicyRolloverHandoffV2(ctx, cfg, stateDir, base, path, raw, preview); err != nil {
		return err
	}
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return err
	}
	for id := 1; id <= 2; id++ {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return err
		}
	}
	if err := persistPolicyRolloverPostconditionV2(ctx, plan, journal, action, handoff, limit); err != nil {
		return err
	}
	if plan.PreviousHandoff == nil {
		if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(policyRolloverRoot(stateDir), "handoff.json"), handoff, limit); err != nil {
			return err
		}
	}
	return nil
}

// The root-first order keeps all retained namespaces available to custody and
// relay readers while process and public-key selectors use the final member.
func (self *policyRolloverHandoffV2) generations() []*policyRolloverHandoffV2 {
	var result []*policyRolloverHandoffV2
	for next := self; next != nil; next = next.predecessor {
		result = append(result, next)
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}
