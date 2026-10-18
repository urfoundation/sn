// The host serializes every multisig step through the original fixed owner-trim
// journal. Each step transition is durable before any later effect, and only
// the latest step can progress; earlier steps are settled history.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
)

// Signatory-local choices for one later step. Every other field is copied from
// the original approved operation and the retained first-approval timepoint.
type ownerTrimMultisigStepTemplate struct {
	Operation      string `json:"operation"`
	Signatory      string `json:"signatory_account_id"`
	DerivationPath string `json:"signer_derivation_path"`
	Nonce          uint32 `json:"nonce"`
	BirthBlock     uint64 `json:"birth_block"`
	BirthHash      string `json:"birth_hash"`
	Period         uint64 `json:"mortal_period"`
	FeeReserveRao  uint64 `json:"fee_reserve_rao"`
	MaxBroadcasts  uint8  `json:"max_broadcasts"`
	MaxRefTime     uint64 `json:"max_ref_time"`
	MaxProofSize   uint64 `json:"max_proof_size"`
}

// An unsigned later-step config for independent approval. The final approval
// belongs to another signatory; cancellation belongs to the original depositor.
func ownerTrimMultisigStepConfig(anchor ownerTrimExecutionConfig, template ownerTrimMultisigStepTemplate, timepoint treasuryTimepoint) (ownerTrimExecutionConfig, error) {
	first := anchor.Action.Multisig
	if first == nil || first.kind() != "first" || template.Operation != "as_multi" && template.Operation != "cancel_as_multi" ||
		(template.Operation == "cancel_as_multi") != (template.Signatory == first.Signatory) {
		return ownerTrimExecutionConfig{}, errors.New("owner trim later multisig step requires as_multi by another signatory or cancel_as_multi by the original depositor")
	}
	multisig := first.clone()
	multisig.Signatory, multisig.Operation, multisig.Timepoint = template.Signatory, template.Operation, &timepoint
	multisig.MaxRefTime, multisig.MaxProofSize, multisig.DepositLimitRao = template.MaxRefTime, template.MaxProofSize, 0
	config := anchor
	config.Signature = ""
	action := anchor.Action
	action.Multisig = &multisig
	action.Nonce, action.BirthBlock, action.BirthHash, action.Period = template.Nonce, template.BirthBlock, template.BirthHash, template.Period
	action.FeeReserveRao, action.MaxBroadcasts, action.DerivationPath = template.FeeReserveRao, template.MaxBroadcasts, template.DerivationPath
	action.Call, action.Payload, action.RequestHash = "", "", ""
	config.Action = action
	return config, nil
}

// Not safe for concurrent use. The original store's exclusive physical custody
// serializes this owner against every other local trim command.
type ownerTrimMultisigOwner struct {
	store  *ownerTrimStore
	config ownerTrimExecutionConfig
	key    string
}

// The anchor record and its ordered steps, fully validated by the store.
func (self *ownerTrimMultisigOwner) load() (ownerTrimRecord, []ownerTrimRecord, error) {
	record, err := self.store.load()
	if err != nil {
		return record, nil, err
	}
	if record.Config.Action.Multisig == nil {
		return record, nil, errors.New("owner trim journal is not a native multisig operation")
	}
	steps, err := ownerTrimMultisigSteps(record)
	return record, steps, err
}

// Seal one replaced or appended step; the store revalidates the full sequence.
func (self *ownerTrimMultisigOwner) persist(record ownerTrimRecord, steps []ownerTrimRecord, index int, step ownerTrimRecord) error {
	step.ContentHash = ""
	step.ContentHash = rootObjectHash(step)
	steps = slices.Clone(steps)
	if index == len(steps) {
		steps = append(steps, step)
	} else {
		steps[index] = step
	}
	record.Phase, record.Multisig = "multisig", &ownerTrimMultisigRecord{Steps: steps}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	return self.store.save(record)
}

// The explicit selector must name the latest step, never settled history.
func ownerTrimMultisigLatest(steps []ownerTrimRecord, index int) (ownerTrimRecord, error) {
	if index < 0 || index != len(steps)-1 {
		return ownerTrimRecord{}, errors.New("owner trim multisig step selector must name the latest retained step")
	}
	return steps[index], nil
}

// Record one independently approved later step for the original open operation.
// Reapplying the identical latest step is idempotent after a lost output.
func (self *ownerTrimMultisigOwner) appendStep(config ownerTrimExecutionConfig) error {
	record, steps, err := self.load()
	if err != nil {
		return err
	}
	if err := config.validate(self.key); err != nil {
		return err
	}
	if len(steps) > 1 && rootObjectHash(steps[len(steps)-1].Config) == rootObjectHash(config) {
		return nil
	}
	state, err := ownerTrimMultisigSequence(self.config, self.key, steps)
	if err != nil {
		return err
	}
	multisig := config.Action.Multisig
	if !state.Open || !state.Settled || multisig == nil || multisig.kind() == "first" || multisig.Timepoint == nil || *multisig.Timepoint != *state.Timepoint ||
		ownerTrimMultisigOperation(config) != ownerTrimMultisigOperation(self.config) || len(steps) >= ownerTrimMultisigStepLimit {
		return errors.New("owner trim multisig step requires the original open operation, its exact timepoint and settled earlier steps")
	}
	step := ownerTrimRecord{Schema: ownerTrimRecordSchema, Config: config, ApprovalKey: self.key, Phase: "reserved"}
	return self.persist(record, steps, len(steps), step)
}

// Only the original reply for the latest step's exact request is retained.
// Reimport is idempotent; another signature can never replace retained bytes.
func (self *ownerTrimMultisigOwner) importReply(index int, request ownerSigningRequest, reply ownerSigningReply) error {
	record, steps, err := self.load()
	if err != nil {
		return err
	}
	step, err := ownerTrimMultisigLatest(steps, index)
	if err != nil {
		return err
	}
	if rootObjectHash(request.Config) != rootObjectHash(step.Config) {
		return errors.New("owner trim multisig reply request names another step, signatory or approval")
	}
	signature, err := reply.validate(request)
	if err != nil {
		return err
	}
	signed, err := step.Config.Action.signed(signature)
	if err != nil {
		return err
	}
	if step.Signature != "" {
		if step.Signature != hex.EncodeToString(signature) {
			return errors.New("owner trim multisig reply cannot replace the step's original signature")
		}
		return nil
	}
	if step.Phase != "reserved" {
		return errors.New("owner trim multisig reply cannot resolve a settled or unknown step")
	}
	step.Phase, step.Signature = "signed", hex.EncodeToString(signature)
	step.RawExtrinsic, step.ExtrinsicHash = "0x"+hex.EncodeToString(signed), rootExtrinsicHash(signed)
	return self.persist(record, steps, index, step)
}

// The unsigned submission policy template binds this step's exact signed bytes.
func (self *ownerTrimMultisigOwner) approvalTemplate(index int) (ownerTrimBestEffortApproval, error) {
	_, steps, err := self.load()
	if err != nil {
		return ownerTrimBestEffortApproval{}, err
	}
	step, err := ownerTrimMultisigLatest(steps, index)
	if err != nil {
		return ownerTrimBestEffortApproval{}, err
	}
	action := step.Config.Action
	if step.Signature == "" || step.Reconciliation != nil || step.Submission != nil || step.Broadcasts >= action.MaxBroadcasts || step.LastFinalized >= action.BirthBlock+action.Period {
		return ownerTrimBestEffortApproval{}, errors.New("owner trim multisig planning requires a signed unsettled step without a retained submission policy")
	}
	return ownerTrimBestEffortApproval{Schema: ownerTrimBestEffortApprovalSchema, ConfigHash: rootObjectHash(step.Config), ExtrinsicHash: step.ExtrinsicHash,
		Runtime: action.Runtime, BaselineCensusHash: self.store.review.Census.ContentHash, ProtectedScopeHash: ownerTrimBestEffortProtectedScope(self.store.review),
		InitialBroadcasts: step.Broadcasts, ValidFromBlock: max(action.BirthBlock, step.LastFinalized), ValidThroughBlock: action.BirthBlock + action.Period - 1,
		MultisigStep: action.Multisig.kind()}, nil
}

// A step policy must name this step's signed bytes and the original review.
func (self *ownerTrimMultisigOwner) validateApproval(step ownerTrimRecord, approval ownerTrimBestEffortApproval, key string) error {
	if step.Signature == "" {
		return errors.New("owner trim multisig submission requires the step's original signature")
	}
	if err := approval.validate(step.Config, key, step.ExtrinsicHash); err != nil {
		return err
	}
	if approval.BaselineCensusHash != self.store.review.Census.ContentHash || approval.ProtectedScopeHash != ownerTrimBestEffortProtectedScope(self.store.review) {
		return errors.New("owner trim multisig submission approval names another original census or protected-generation scope")
	}
	return nil
}

// Read-only canonical reconciliation of the latest step. A settled step is
// reread only to add a missing census; its native outcome cannot change.
func (self *ownerTrimMultisigOwner) reconcile(ctx context.Context, chain *ownerTrimCanonicalChain, index int) (ownerTrimRecord, ownerTrimActionReconciliation, error) {
	var evidence ownerTrimActionReconciliation
	record, steps, err := self.load()
	if err != nil {
		return ownerTrimRecord{}, evidence, err
	}
	step, err := ownerTrimMultisigLatest(steps, index)
	if err != nil {
		return step, evidence, err
	}
	if ctx == nil || chain == nil || rootObjectHash(chain.config) != rootObjectHash(step.Config) {
		return step, evidence, errors.New("owner trim multisig reconciliation requires the exact step route and context")
	}
	// An executed trim can still gain its whole-block census after a read gap.
	if prior := step.Reconciliation; prior != nil && ownerTrimMultisigSettled(step.Phase) &&
		(step.Phase != "executed" || prior.Census != nil && prior.Census.Issue == "" && prior.Census.Correspondence != nil) {
		return step, *prior, ctx.Err()
	}
	action := step.Config.Action
	raw, _ := hex.DecodeString(strings.TrimPrefix(step.RawExtrinsic, "0x"))
	evidence, err = chain.reconcile(ctx, action, raw)
	if err != nil {
		return step, evidence, err
	}
	if err := evidence.validate(action, raw); err != nil {
		return step, evidence, err
	}
	o := evidence.Observation
	if o.FinalizedNumber < step.LastFinalized || o.FinalizedNumber == step.LastFinalized && step.LastFinalizedHash != "" && o.FinalizedHash != step.LastFinalizedHash ||
		o.FinalizedNumber > step.LastFinalized && o.FinalizedHash == step.LastFinalizedHash {
		return step, evidence, errors.New("owner trim multisig finalized continuity changed")
	}
	if prior := step.Reconciliation; prior != nil {
		priorReadback, readback := (*ownerTrimMultisigReadback)(nil), (*ownerTrimMultisigReadback)(nil)
		if prior.Multisig != nil && evidence.Multisig != nil {
			priorReadback, readback = prior.Multisig.Readback, evidence.Multisig.Readback
		}
		if rootObjectHash(prior.Receipt) != rootObjectHash(evidence.Receipt) || priorReadback != nil && rootObjectHash(priorReadback) != rootObjectHash(readback) {
			return step, evidence, errors.New("owner trim multisig continuation changed the original receipt or inclusion readback")
		}
	}
	step.LastFinalized, step.LastFinalizedHash = o.FinalizedNumber, o.FinalizedHash
	if phase := ownerTrimMultisigPhase(action, evidence, len(raw) != 0); phase != "" {
		if ownerTrimMultisigSettled(step.Phase) && phase != step.Phase {
			return step, evidence, errors.New("owner trim multisig settled outcome cannot change on reread")
		}
		step.Phase, step.Reconciliation = phase, &evidence
	}
	if err := self.persist(record, steps, index, step); err != nil {
		return step, evidence, err
	}
	return step, evidence, ctx.Err()
}

// One numbered post of the latest step's retained exact bytes after canonical
// reconciliation and fresh admission. The attempt is durable before transport,
// and a lost acknowledgement consumes it; the next invocation reconciles first.
func (self *ownerTrimMultisigOwner) submit(ctx context.Context, chain *ownerTrimCanonicalChain, index int, approval ownerTrimBestEffortApproval, approvalKey string) error {
	if ctx == nil || chain == nil {
		return errors.New("owner trim multisig submission requires context and the exact step route")
	}
	record, steps, err := self.load()
	if err != nil {
		return err
	}
	step, err := ownerTrimMultisigLatest(steps, index)
	if err != nil {
		return err
	}
	if err := self.validateApproval(step, approval, approvalKey); err != nil {
		return err
	}
	if step.Submission == nil {
		if step.Reconciliation != nil || step.Broadcasts != approval.InitialBroadcasts {
			return errors.New("owner trim multisig submission policy cannot adopt settled or changed attempt custody")
		}
		step.Submission = &ownerTrimBestEffortSubmissionRecord{Approval: approval, ApprovalKey: approvalKey, SubmittedBroadcasts: step.Broadcasts}
		if err := self.persist(record, steps, index, step); err != nil {
			return err
		}
	} else if step.Submission.ApprovalKey != approvalKey || rootObjectHash(step.Submission.Approval) != rootObjectHash(approval) {
		return errors.New("owner trim multisig original submission policy cannot be replaced or renewed")
	}
	step, evidence, err := self.reconcile(ctx, chain, index)
	if err != nil {
		return err
	}
	if step.Reconciliation != nil || step.Phase != "signed" && step.Phase != "pending" {
		return ctx.Err()
	}
	action := step.Config.Action
	if step.Broadcasts >= action.MaxBroadcasts {
		return errors.New("owner trim multisig step broadcast allowance exhausted; receipt recovery remains available")
	}
	if err := chain.admitMultisigStep(ctx, self.store.review, self.config.Action.Multisig.Signatory, step, approval, evidence); err != nil {
		return err
	}
	if err := chain.network(ctx); err != nil {
		return err
	}
	record, steps, err = self.load()
	if err != nil {
		return err
	}
	if step, err = ownerTrimMultisigLatest(steps, index); err != nil {
		return err
	}
	if step.Reconciliation != nil || step.Submission == nil || step.Broadcasts >= action.MaxBroadcasts {
		return errors.New("owner trim multisig step changed during admission")
	}
	// Count before any post, including errors and cancellation.
	step.Broadcasts++
	step.Phase, step.Submission.SubmittedBroadcasts = "pending", step.Broadcasts
	if err := self.persist(record, steps, index, step); err != nil {
		return err
	}
	if err := chain.multisigHeadUnchanged(ctx, evidence.Observation.FinalizedNumber, evidence.Observation.FinalizedHash); err != nil {
		return err
	}
	if _, _, err := self.load(); err != nil {
		return err
	}
	_, err = ownedSubmissionPost(ctx, chain.client, step.Config.Route, "author_submitExtrinsic", step.RawExtrinsic, step.ExtrinsicHash)
	return err
}

// One step's operator view. It never claims a full reset or activation.
type ownerTrimMultisigStepStatus struct {
	Index                int     `json:"index"`
	Kind                 string  `json:"kind"`
	Signatory            string  `json:"signatory_account_id"`
	Phase                string  `json:"phase"`
	Status               string  `json:"status"`
	Broadcasts           uint8   `json:"broadcasts"`
	ActualFeeRao         *uint64 `json:"actual_fee_rao,omitempty"`
	FeeReserveExceeded   bool    `json:"fee_reserve_exceeded"`
	InnerError           string  `json:"inner_error,omitempty"`
	GenerationReconciled bool    `json:"generation_correspondence_observed"`
	ResidualOldCount     *uint16 `json:"residual_old_generation_count,omitempty"`
}

// The operation view: owner, signer set, timepoint and every retained step.
type ownerTrimMultisigStatus struct {
	OwnerAccount       string                        `json:"owner_account_id"`
	Threshold          uint16                        `json:"threshold"`
	Signatories        []string                      `json:"signatories"`
	Depositor          string                        `json:"depositor"`
	CallHash           string                        `json:"inner_call_hash"`
	Timepoint          *treasuryTimepoint            `json:"timepoint,omitempty"`
	OperationOpen      bool                          `json:"operation_open"`
	ClosedBy           string                        `json:"closed_by,omitempty"`
	NextStepPlannable  bool                          `json:"next_step_plannable"`
	Steps              []ownerTrimMultisigStepStatus `json:"steps"`
	FullResetCompleted bool                          `json:"full_reset_completed"`
	ActivationReady    bool                          `json:"activation_ready"`
}

// Only an executed final approval with matching whole-block correspondence is
// the reviewed partial-trim success; every other outcome stays explicit.
func ownerTrimMultisigSummary(config ownerTrimExecutionConfig, key string, steps []ownerTrimRecord) (ownerTrimMultisigStatus, error) {
	multisig := config.Action.Multisig
	if multisig == nil {
		return ownerTrimMultisigStatus{}, errors.New("owner trim journal is not a native multisig operation")
	}
	state, err := ownerTrimMultisigSequence(config, key, steps)
	if err != nil {
		return ownerTrimMultisigStatus{}, err
	}
	result := ownerTrimMultisigStatus{OwnerAccount: multisig.AccountId, Threshold: multisig.Threshold, Signatories: slices.Clone(multisig.Signatories),
		Depositor: multisig.Signatory, CallHash: multisig.CallHash, Timepoint: state.Timepoint, OperationOpen: state.Open, ClosedBy: state.Closed,
		NextStepPlannable: state.Open && state.Settled && len(steps) < ownerTrimMultisigStepLimit, Steps: []ownerTrimMultisigStepStatus{}}
	for index, step := range steps {
		action := step.Config.Action
		view := ownerTrimMultisigStepStatus{Index: index, Kind: action.Multisig.kind(), Signatory: action.Multisig.Signatory, Phase: step.Phase,
			Status: step.Phase, Broadcasts: step.Broadcasts}
		evidence := step.Reconciliation
		if evidence != nil && evidence.Receipt != nil {
			fee := evidence.Receipt.ActualFeeRao
			view.ActualFeeRao, view.FeeReserveExceeded = &fee, fee > action.FeeReserveRao
			if evidence.Multisig != nil {
				view.InnerError = evidence.Multisig.Dispatch.InnerError
			}
		}
		switch step.Phase {
		case "reserved", "signed", "pending":
			view.Status = "pending"
		case "finalized-readback-pending":
			view.Status = "transaction-finalized-readback-pending"
		case "approval-recorded":
			view.Status = "first-approval-recorded-awaiting-final-approval"
		case "inner-dispatch-failed":
			view.Status = "multisig-executed-inner-trim-failed"
		case "cancelled":
			view.Status = "cancelled-deposit-released"
		case "executed":
			view.Status = "multisig-executed-generation-outcome-unresolved"
			if census := evidence.Census; census != nil && census.Issue == "" && census.Correspondence != nil {
				view.Status = "multisig-executed-generation-conflict"
				if census.Correspondence.Matches {
					view.GenerationReconciled, view.Status = true, "multisig-executed-partial-trim-correspondence-observed"
				}
				count := uint16(len(census.Correspondence.ResidualOld))
				view.ResidualOldCount = &count
			}
		}
		result.Steps = append(result.Steps, view)
	}
	return result, nil
}
