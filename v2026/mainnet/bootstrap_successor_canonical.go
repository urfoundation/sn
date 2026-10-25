// The concrete adapter borrows one durable owner and its independently approved
// original route. All chain observations remain owned-RPC assertions.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

// Calls are serial while owner and all original marker locks remain held.
// No private key, endpoint discovery or automatic write retry exists here.
type bootstrapSuccessorCanonicalChain struct {
	owner               *bootstrapSuccessorExecutionStore
	chain               *evmOwnedChain
	approval            bootstrapSuccessorCanonicalApproval
	runtimeProfiles     []rootReceiptProfile
	runtimeRevisionHash string
	currentRevisionHash string
	currentPolicy       *bootstrapSuccessorSafeCurrentCapability
	currentProof        *safeCurrentStorageObservation
	provenance          bootstrapSuccessorSafeProvenanceAuthenticator
	plans               []evmCreatePlan
	records             []evmActionRecord
	locks               []*bootstrapContractReadinessMarker
	retained            *bootstrapChainReadinessState
	planHash            string
	authenticated       bool
	admittedSequence    uint16
	admitted            bool
	submittedSequence   uint16
	submitted           bool
	closed              bool
}

// Construction reopens exact original source inputs, holds every historical
// marker and retains the independent canonical authorization before networking.
func newBootstrapSuccessorCanonicalChain(ctx context.Context, owner *bootstrapSuccessorExecutionStore, approval bootstrapSuccessorCanonicalApproval) (_ *bootstrapSuccessorCanonicalChain, resultErr error) {
	return newBootstrapSuccessorCanonicalChainWithProvenance(ctx, owner, approval, nil)
}

// Only an explicitly supplied canonical-history capability can admit writes.
// The public constructor deliberately has no production provenance implementation.
func newBootstrapSuccessorCanonicalChainWithProvenance(ctx context.Context, owner *bootstrapSuccessorExecutionStore, approval bootstrapSuccessorCanonicalApproval, provenance bootstrapSuccessorSafeProvenanceAuthenticator, revisions ...bootstrapSuccessorRuntimeApproval) (_ *bootstrapSuccessorCanonicalChain, resultErr error) {
	return newBootstrapSuccessorCanonicalChainWithAuthorities(ctx, owner, approval, provenance, 0, nil, revisions...)
}

// The two policy routes remain distinct. Public current-only selection requires
// the v2 acceptance; the command separately checks its exact caller opt-in.
func newBootstrapSuccessorCanonicalChainWithAuthorities(ctx context.Context, owner *bootstrapSuccessorExecutionStore, approval bootstrapSuccessorCanonicalApproval, provenance bootstrapSuccessorSafeProvenanceAuthenticator, route bootstrapSuccessorSafeCurrentRoute, current []bootstrapSuccessorSafeCurrentRevisionApproval, revisions ...bootstrapSuccessorRuntimeApproval) (_ *bootstrapSuccessorCanonicalChain, resultErr error) {
	if ctx == nil || owner == nil || owner.closed {
		return nil, errors.New("successor canonical adapter requires retained execution ownership")
	}
	if route != 0 && route != bootstrapSuccessorSafeCurrentNativeRoute && route != bootstrapSuccessorSafeCurrentPublicRoute || provenance != nil && (route != 0 || len(current) != 0 || owner.safeCurrentHistory.hash() != "") {
		return nil, errors.New("successor canonical adapter cannot mix history and current-policy capabilities")
	}
	plan := owner.planCopy()
	preparationPlan := plan.Review.Preparation.Approval.Plan
	preparation, err := loadBootstrapChainPreparation(ctx, preparationPlan.OriginalConfig.Path)
	if err != nil || preparation.Plan.ConfigSha256 != preparationPlan.OriginalConfig.Sha256 ||
		preparation.Plan.ContentHash != preparationPlan.Proposal.Request.BootstrapPlanHash ||
		rootObjectHash(preparation.Contracts.Config) != preparationPlan.Proposal.OriginalConfigHash {
		return nil, errors.Join(errors.New("successor canonical adapter original signed source differs"), err)
	}
	_, plans, err := prepareBootstrapContractReadiness(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path)
	if err != nil || len(plans) != 8 {
		return nil, errors.Join(errors.New("successor canonical adapter requires all eight original projections"), err)
	}
	self := &bootstrapSuccessorCanonicalChain{owner: owner, approval: approval, provenance: provenance, planHash: plan.hash()}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	self.retained, err = openBootstrapChainReadinessState(ctx, preparation)
	if err != nil {
		return nil, err
	}
	if rootObjectHash(self.retained) != rootObjectHash(preparationPlan.Proposal.LocalPreparation) {
		return nil, errors.New("successor canonical original preparation custody differs from approved adoption")
	}
	for i, original := range plans {
		projection := copyEvmCreatePlan(original)
		projection.Prerequisites = append([]evmActionRecord(nil), self.records...)
		marker := rootObjectHash(original.Config) + "\n"
		if i > 0 {
			marker = rootObjectHash(struct{ ConfigHash, ActionId, PredecessorHash string }{
				ConfigHash: rootObjectHash(original.Config), ActionId: original.Config.Plan.Actions[i].Id,
				PredecessorHash: rootObjectHash(self.records[i-1])}) + "\n"
		}
		lock, err := openBootstrapContractReadinessMarker(filepath.Join(owner.local.path, bootstrapContractStateFile(i)), marker+bootstrapRootClaimComplete, ctx)
		if err != nil {
			return nil, err
		}
		self.locks = append(self.locks, lock)
		raw, err := owner.local.read(bootstrapContractStateFile(i))
		var record evmActionRecord
		if err == nil {
			err = decodePlanJson(raw, &record)
		}
		if err == nil {
			err = errors.Join(record.validateForAction(original.Config, i), validateEvmCreatePrerequisite(projection, record), validateEvmCreateCompletion(projection, record))
		}
		if err != nil || rootObjectHash(record) != preparationPlan.Proposal.AdoptedActions[i].CustodyHash || record.Receipt == nil || record.Receipt.Status != 1 {
			return nil, errors.Join(errors.New("successor canonical original full record differs"), err)
		}
		self.plans, self.records = append(self.plans, projection), append(self.records, record)
	}
	if err := owner.retainCanonicalAuthority(ctx, approval); err != nil {
		return nil, err
	}
	for _, revision := range revisions {
		if err := owner.retainRuntimeRevision(ctx, revision); err != nil {
			return nil, err
		}
	}
	for _, revision := range current {
		if err := owner.retainSafeCurrentRevision(ctx, revision); err != nil {
			return nil, err
		}
	}
	self.runtimeProfiles, self.runtimeRevisionHash = owner.runtimeProfiles(), owner.runtimeHistory.hash()
	self.currentRevisionHash = owner.safeCurrentHistory.hash()
	self.chain, err = newEvmOwnedChain(plans[0].Config)
	if err != nil {
		return nil, err
	}
	if err := self.selectCurrentPolicy(ctx, route); err != nil {
		return nil, err
	}
	return self, self.checkpoint(ctx, plan)
}

// Closing this adapter releases only its borrowed markers and HTTP resources.
// The caller closes the execution owner and original preparation locks later.
func (self *bootstrapSuccessorCanonicalChain) close() error {
	if self == nil || self.closed {
		return nil
	}
	self.closed = true
	var result error
	for i := len(self.locks) - 1; i >= 0; i-- {
		result = errors.Join(result, self.locks[i].close())
	}
	self.locks = nil
	result = errors.Join(result, self.retained.close())
	if self.chain != nil {
		self.chain.client.httpClient.CloseIdleConnections()
	}
	return result
}

// Every entry rechecks owner identity, authority and all exact local receipts.
// Immutable copies cannot hide replacement of on-disk custody between reads.
func (self *bootstrapSuccessorCanonicalChain) checkpoint(ctx context.Context, plan bootstrapSuccessorExecutionPlan) error {
	if ctx == nil || self == nil || self.closed || self.owner == nil || self.owner.closed || self.chain == nil || plan.hash() != self.planHash || len(self.records) != 8 || len(self.plans) != 8 {
		return errors.New("successor canonical adapter scope or lifetime differs")
	}
	if err := errors.Join(self.retained.checkpoint(ctx), self.owner.checkpoint("canonical-adapter-checkpoint"), self.approval.validate(ctx, plan)); err != nil {
		return err
	}
	if self.runtimeRevisionHash != self.owner.runtimeHistory.hash() {
		return errors.New("successor canonical adapter runtime authority changed during ownership")
	}
	if self.currentRevisionHash != self.owner.safeCurrentHistory.hash() {
		return errors.New("successor canonical adapter current-policy authority changed during ownership")
	}
	raw, err := self.owner.local.read(bootstrapSuccessorCanonicalFile)
	expected, encodeErr := json.Marshal(self.approval)
	if err != nil || encodeErr != nil || !bytes.Equal(raw, expected) || self.owner.canonicalAuthorityHash != rootObjectHash(self.approval) {
		return errors.Join(errors.New("successor canonical retained authorization differs"), err, encodeErr)
	}
	if len(self.locks) != len(self.records) {
		return errors.New("successor canonical original marker custody is incomplete")
	}
	for i, record := range self.records {
		if err := self.locks[i].checkpoint(); err != nil {
			return err
		}
		raw, err := self.owner.local.read(bootstrapContractStateFile(i))
		var current evmActionRecord
		if err == nil {
			err = decodePlanJson(raw, &current)
		}
		if err != nil || rootObjectHash(current) != rootObjectHash(record) {
			return errors.Join(errors.New("successor canonical original custody changed during ownership"), err)
		}
	}
	return self.retained.checkpoint(ctx)
}

// All eight canonical receipts and postconditions are reauthenticated through
// the existing historical adapter, with no re-signing or original action write.
func (self *bootstrapSuccessorCanonicalChain) authenticate(ctx context.Context, plan bootstrapSuccessorExecutionPlan) ([]string, error) {
	if err := self.checkpoint(ctx, plan); err != nil {
		return nil, err
	}
	self.authenticated, self.admitted = false, false
	seals := make([]string, 0, 8)
	for i, original := range self.plans {
		observation, err := self.chain.reconcile(ctx, original, self.records[i])
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				err = errors.Join(errRpcObservationUnavailable, err)
			}
			return nil, fmt.Errorf("successor canonical original receipt observation failed: %w", err)
		}
		if observation.Receipt == nil {
			return nil, fmt.Errorf("%w: successor canonical original receipt was not observed", errRpcObservationUnavailable)
		}
		if *observation.Receipt != *self.records[i].Receipt || observation.Status != original.completedStatus() {
			return nil, fmt.Errorf("%w: successor canonical original receipt or historical postcondition differs", errRpcIntegrity)
		}
		seals = append(seals, rootObjectHash(self.records[i]))
	}
	if err := self.checkpoint(ctx, plan); err != nil {
		return nil, err
	}
	self.authenticated = true
	return seals, nil
}

// One write requires this exact owner's newly counted attempt and a successful
// admission after that reservation. Uncertain HTTP replies never cause retries.
func (self *bootstrapSuccessorCanonicalChain) submit(ctx context.Context, plan bootstrapSuccessorExecutionPlan, signed []byte) error {
	if self != nil && self.owner != nil && (self.owner.safeCurrentHistory.hash() != "" || self.owner.safeCurrentHistory.pendingHash != "") {
		if err := self.currentPolicyReady(ctx, plan, self.owner.safeCurrentHistory.hash()); err != nil {
			return err
		}
	}
	if self == nil || self.provenance == nil && self.currentPolicy == nil {
		return errBootstrapSuccessorSafeProvenanceUnavailable
	}
	if err := self.checkpoint(ctx, plan); err != nil {
		return err
	}
	if !self.authenticated || !self.admitted || self.owner.last.Phase != "attempt-reserved" || self.admittedSequence != self.owner.last.Sequence ||
		self.owner.last.CanonicalAuthorityHash != rootObjectHash(self.approval) || self.owner.last.RuntimeRevisionHash != self.runtimeRevisionHash ||
		self.owner.last.SafeCurrentRevisionHash != self.currentRevisionHash || self.owner.safeCurrentHistory.pendingHash != "" ||
		self.owner.runtimeHistory.pendingHash != "" || self.submitted && self.submittedSequence == self.owner.last.Sequence {
		return errors.New("successor canonical write lacks a newly counted admitted authority")
	}
	retained, err := rootReceiptHex(plan.SignedRelayer, 128*1024)
	if err != nil || !bytes.Equal(retained, signed) {
		return errors.Join(errors.New("successor canonical write changed retained signed bytes"), err)
	}
	self.submitted, self.submittedSequence, self.admitted = true, self.owner.last.Sequence, false
	_, err = ownedSubmissionPost(ctx, self.chain.client, self.plans[0].Config.Plan.Route, "eth_sendRawTransaction", plan.SignedRelayer, plan.TransactionHash.Hex())
	return err
}
