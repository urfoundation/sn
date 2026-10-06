// This adapter owns one real, numbered post of retained bytes. It requires a
// separate best-effort policy and cannot implement the strict window enforcer.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"time"
)

// Not safe for concurrent use. The original store's exclusive physical custody
// and a consumed post reservation fence every effect, including direct calls.
type ownerTrimBestEffortChain struct {
	*ownerTrimCanonicalChain
	store       *ownerTrimStore
	approval    ownerTrimBestEffortApproval
	approvalKey string
}

// The public caller must provide both independently trusted keys on every
// restart. A retained policy never authorizes itself, and cannot be replaced.
func newOwnerTrimBestEffortChain(store *ownerTrimStore, approval ownerTrimBestEffortApproval, key string) (*ownerTrimBestEffortChain, error) {
	if store == nil {
		return nil, errors.New("owner trim best-effort submission requires original physical custody")
	}
	record, err := store.load()
	if err != nil {
		return nil, err
	}
	if err := store.validateBestEffortApproval(approval, key, record); err != nil {
		return nil, err
	}
	if record.Submission == nil {
		if record.Reconciliation != nil || record.Signature == "" || record.Broadcasts != approval.InitialBroadcasts {
			return nil, errors.New("owner trim best-effort policy cannot adopt terminal, unsigned or changed attempt custody")
		}
		record.Submission = &ownerTrimBestEffortSubmissionRecord{Approval: approval, ApprovalKey: key, SubmittedBroadcasts: record.Broadcasts}
		record.ContentHash = ""
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			return nil, err
		}
	} else if record.Submission.ApprovalKey != key || rootObjectHash(record.Submission.Approval) != rootObjectHash(approval) {
		return nil, errors.New("owner trim original best-effort policy cannot be replaced or renewed")
	}
	chain, err := newOwnerTrimCanonicalChain(store.config, store.key, store.policy, nil)
	if err != nil {
		return nil, err
	}
	// Decouple the risk list from caller-owned slices after validation.
	approval.ResidualRisks = append([]string(nil), approval.ResidualRisks...)
	return &ownerTrimBestEffortChain{ownerTrimCanonicalChain: chain, store: store, approval: approval, approvalKey: key}, nil
}

// Only this fresh domain may accept explicitly reviewed residual risk. Every
// observed census, owner, protected generation, runtime and nonce must still fit.
func (self *ownerTrimBestEffortChain) authorize(ctx context.Context, config ownerTrimExecutionConfig, evidence ownerTrimActionReconciliation) error {
	if ctx == nil || self == nil || self.store == nil || rootObjectHash(config) != rootObjectHash(self.config) {
		return errors.New("owner trim best-effort admission has no exact retained action")
	}
	record, err := self.store.load()
	if err != nil {
		return err
	}
	if err := self.validateRetainedPolicy(record); err != nil {
		return err
	}
	action, observation := config.Action, evidence.Observation
	raw, _ := hex.DecodeString(record.RawExtrinsic[2:])
	if err := evidence.validate(action, raw); err != nil {
		return err
	}
	if observation.FinalizedNumber < record.LastFinalized || observation.FinalizedNumber == record.LastFinalized && record.LastFinalizedHash != "" && observation.FinalizedHash != record.LastFinalizedHash ||
		observation.FinalizedNumber > record.LastFinalized && observation.FinalizedHash == record.LastFinalizedHash {
		return errors.New("owner trim best-effort admission regressed or contradicted retained finalized continuity")
	}
	if evidence.Receipt != nil || record.Reconciliation != nil || observation.AccountNonce == nil || *observation.AccountNonce != action.Nonce ||
		observation.FinalizedNumber < self.approval.ValidFromBlock || observation.FinalizedNumber > self.approval.ValidThroughBlock ||
		observation.Census == nil || observation.Issue != "" {
		return errors.New("owner trim best-effort admission lacks current nonce, census or approved original window")
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	guard, err := self.client.readOwnerTrimGuard(operationCtx, self.store.policy, action.PolicyHash, self.store.review, "recheck")
	if err != nil {
		return err
	}
	matches := guard.ObservationMatches
	if !matches && self.approval.RegistrationPolicy == ownerTrimAcceptRegistrationRisk && len(guard.ComparisonBlockers) != 0 {
		before, after := self.store.review.Census.Observation, guard.CurrentCensus.Observation
		matches = before.RegistrationAllowed == after.RegistrationAllowed && before.PowRegistrationAllowed == after.PowRegistrationAllowed
		for _, blocker := range guard.ComparisonBlockers {
			if blocker != "OWNER_TRIM_COMPETING_REGISTRATION_OR_REENTRY_NOT_FENCED" {
				matches = false
			}
		}
	}
	if !matches || guard.BaselineCensusHash != self.approval.BaselineCensusHash ||
		guard.CurrentCensus.Observation.Identity.FinalizedNumber != observation.FinalizedNumber ||
		guard.CurrentCensus.Observation.Identity.FinalizedHash != observation.FinalizedHash {
		return errors.New("owner trim best-effort current selection, protected generations or finalized census changed")
	}
	previous := observation.Census.Observation
	sealed, err := sealSubnetPreview(previous)
	if err != nil || sealed.ContentHash != observation.Census.ContentHash {
		return errors.New("owner trim best-effort original canonical census seal differs")
	}
	previous.Identity.ObservedAt = guard.CurrentCensus.Observation.Identity.ObservedAt
	if rootObjectHash(previous) != rootObjectHash(guard.CurrentCensus.Observation) {
		return errors.New("owner trim best-effort same-block censuses contradict")
	}
	window, err := self.readCurrentPredicates(operationCtx, action, observation, ownerTrimCurrentWindow{})
	if err != nil {
		return err
	}
	for _, blocker := range window.Blockers {
		if blocker != "OWNER_TRIM_PUBLIC_SUBNET_PRUNING_NOT_FENCED_THROUGH_EXPIRY" || self.approval.PublicPruningPolicy != ownerTrimAcceptPruningRisk {
			return errors.New("owner trim best-effort current nonce, proxy or public-pruning predicates refuse submission")
		}
	}
	_, err = self.store.load()
	return errors.Join(err, operationCtx.Err())
}

// Policy bytes and their independent trust input survive every local boundary.
func (self *ownerTrimBestEffortChain) validateRetainedPolicy(record ownerTrimRecord) error {
	if err := self.store.validateBestEffortApproval(self.approval, self.approvalKey, record); err != nil {
		return err
	}
	if record.Submission == nil || record.Submission.ApprovalKey != self.approvalKey || rootObjectHash(record.Submission.Approval) != rootObjectHash(self.approval) {
		return errors.New("owner trim original submission policy custody changed")
	}
	return nil
}

// A lost acknowledgement consumes the reservation. Only the outer owner can
// reconcile and reserve the next bounded attempt, using identical signed bytes.
func (self *ownerTrimBestEffortChain) submit(ctx context.Context, config ownerTrimExecutionConfig, raw []byte) error {
	if ctx == nil || self == nil || self.store == nil || rootObjectHash(config) != rootObjectHash(self.config) || len(raw) == 0 {
		return errors.New("owner trim best-effort post requires exact retained action and context")
	}
	if err := ownerTrimSignedAction(config.Action, raw); err != nil {
		return err
	}
	record, err := self.store.load()
	if err != nil {
		return err
	}
	if err := self.validateRetainedPolicy(record); err != nil {
		return err
	}
	if record.Phase != "pending" || record.RawExtrinsic != "0x"+hex.EncodeToString(raw) || record.Broadcasts <= record.Submission.SubmittedBroadcasts {
		return errors.New("owner trim post has no unconsumed original numbered reservation")
	}
	evidence, err := self.reconcile(ctx, config.Action, raw)
	if err != nil {
		return err
	}
	if ownerTrimTerminalPhase(config.Action, evidence, true) != "" {
		return nil
	}
	if err := self.authorize(ctx, config, evidence); err != nil {
		return err
	}
	if err := self.network(ctx); err != nil {
		return err
	}
	// Count before any post, including errors and cancellation. The real store
	// checks the exact predecessor and physical marker again after publication.
	record.Submission.SubmittedBroadcasts = record.Broadcasts
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := self.store.save(record); err != nil {
		return err
	}
	var canonical, latest string
	observation := evidence.Observation
	if err := self.client.call(ctx, "chain_getBlockHash", []any{observation.FinalizedNumber}, &canonical); err != nil {
		return err
	}
	if err := self.client.call(ctx, "chain_getFinalizedHead", []any{}, &latest); err != nil {
		return err
	}
	if canonical != observation.FinalizedHash || latest != observation.FinalizedHash {
		return errors.New("owner trim admitted finalized head changed before the retained post")
	}
	if _, err := self.store.load(); err != nil {
		return err
	}
	_, err = ownedSubmissionPost(ctx, self.client, config.Route, "author_submitExtrinsic", record.RawExtrinsic, record.ExtrinsicHash)
	return err
}
