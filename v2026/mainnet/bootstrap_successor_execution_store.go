// One execution claim binds original adoption before either nonce can be used.
// Immutable event intents survive missing replies and interrupted publication.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// The original five preparation locks are held by the caller throughout this
// serial owner's lifetime. The root reader is exclusively locked before the
// registry; no method is safe for concurrent calls on this owner.
type bootstrapSuccessorExecutionStore struct {
	approval               bootstrapSuccessorExecutionApproval
	profile                *safeExecutionProfile
	reader                 *bootstrapSuccessorPreparationStore
	local                  *bootstrapSuccessorExecutionDirectory
	registry               *bootstrapSuccessorExecutionDirectory
	last                   bootstrapSuccessorExecutionEvent
	pending                string
	pendingOutcomeHash     string
	closed                 bool
	canonicalAuthorityHash string
	canonicalAuthority     *bootstrapSuccessorCanonicalApproval
	runtimeHistory         bootstrapSuccessorRuntimeHistory
	safeCurrentHistory     bootstrapSuccessorSafeCurrentHistory
	registryRebind         *bootstrapSuccessorRegistryRebindApproval
	localRebind            *bootstrapSuccessorLocalRebindApproval
	deferredLocalRebind    bool
}

// Global within the approved physical registry, these keys deliberately use
// separate domains. Changing the outer nonce never releases the Safe nonce.
func (self bootstrapSuccessorExecutionPlan) nonceNames() []string {
	return []string{
		"safe-inner-" + strings.TrimPrefix(rootObjectHash(struct {
			Chain uint64
			Safe  string
			Nonce string
		}{Chain: mainnetEvmChainId, Safe: self.Review.Transaction.Safe.Hex(), Nonce: self.Review.Transaction.Nonce}), "sha256:") + ".json",
		"relayer-outer-" + strings.TrimPrefix(rootObjectHash(struct {
			Chain  uint64
			Sender string
			Nonce  uint64
		}{Chain: mainnetEvmChainId, Sender: self.Review.Relayer.Sender.Hex(), Nonce: self.Review.Relayer.Nonce}), "sha256:") + ".json",
	}
}

// A nonce claim includes the exact approval, root and both signed identities.
// The independently approved registry path/physical identity remains in Plan.
func (self bootstrapSuccessorExecutionStore) nonceBytes(name string) []byte {
	raw, _ := json.Marshal(struct {
		Schema          string                         `json:"schema"`
		Domain          string                         `json:"domain"`
		ApprovalHash    string                         `json:"approval_hash"`
		OriginalRoot    string                         `json:"original_root"`
		PhysicalRoot    bootstrapSuccessorRootIdentity `json:"physical_root"`
		SafeDigest      string                         `json:"safe_digest"`
		TransactionHash string                         `json:"transaction_hash"`
	}{Schema: "urnetwork-mainnet-successor-nonce-claim-v1", Domain: name, ApprovalHash: rootObjectHash(self.approval),
		OriginalRoot: self.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory, PhysicalRoot: self.approval.Plan.Review.Preparation.Approval.Plan.Root,
		SafeDigest: self.approval.Plan.Review.Transaction.Digest.Hex(), TransactionHash: self.approval.Plan.TransactionHash.Hex()})
	return raw
}

// Fresh ownership requires an unused fixed root claim. Recovery requires that
// same claim or its durable hash-named stage; missing custody cannot renew it.
func openBootstrapSuccessorExecutionStore(ctx context.Context, expected bootstrapSuccessorExecutionPlan, approval bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, create bool, hook func(string) error) (_ *bootstrapSuccessorExecutionStore, resultErr error) {
	return openBootstrapSuccessorExecutionStoreWithRegistryRebind(ctx, expected, approval, profile, create, hook, nil)
}

// A physical adoption is explicit and resume-only. The original approval and
// every nonce claim remain in their original byte/signature domains.
func openBootstrapSuccessorExecutionStoreWithRegistryRebind(ctx context.Context, expected bootstrapSuccessorExecutionPlan, approval bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, create bool, hook func(string) error, rebind *bootstrapSuccessorRegistryRebindApproval) (_ *bootstrapSuccessorExecutionStore, resultErr error) {
	return openBootstrapSuccessorExecutionStoreWithPhysicalRebind(ctx, expected, approval, profile, create, hook, rebind, nil)
}

// Local and registry coordinates have distinct approval domains; both preserve
// the same original claim. Neither approval permits fresh ownership.
func openBootstrapSuccessorExecutionStoreWithPhysicalRebind(ctx context.Context, expected bootstrapSuccessorExecutionPlan, approval bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, create bool, hook func(string) error, rebind *bootstrapSuccessorRegistryRebindApproval, localRebind *bootstrapSuccessorLocalRebindApproval) (_ *bootstrapSuccessorExecutionStore, resultErr error) {
	if ctx == nil {
		return nil, errors.New("successor execution context is absent")
	}
	if err := errors.Join(ctx.Err(), approval.validate(expected, profile)); err != nil {
		return nil, err
	}
	var retainedRebind *bootstrapSuccessorRegistryRebindApproval
	var retainedLocalRebind *bootstrapSuccessorLocalRebindApproval
	var inspection *bootstrapSuccessorLocalInspection
	if localRebind != nil {
		if create {
			return nil, errors.New("successor local rebind cannot create a fresh execution claim")
		}
		if err := localRebind.validate(ctx, approval, profile); err != nil {
			return nil, err
		}
		copy := *localRebind
		retainedLocalRebind = &copy
		inspection = &bootstrapSuccessorLocalInspection{preparationHash: rootObjectHash(approval.Plan.Review.Preparation.Approval.Plan), physical: copy.Plan.RestoredLocal, writer: true}
	}
	if rebind != nil {
		if create {
			return nil, errors.New("successor registry rebind cannot create a fresh execution claim")
		}
		if err := rebind.validate(ctx, approval, profile); err != nil {
			return nil, err
		}
		copy := *rebind
		retainedRebind = &copy
	}
	raw, err := json.Marshal(approval)
	if err != nil || len(raw) > maximumBootstrapSuccessorExecutionBytes {
		return nil, errors.Join(errors.New("successor execution approval exceeds its byte bound"), err)
	}
	var copied bootstrapSuccessorExecutionApproval
	if err := decodePlanJson(raw, &copied); err != nil {
		return nil, err
	}
	self := &bootstrapSuccessorExecutionStore{approval: copied, profile: profile, registryRebind: retainedRebind, localRebind: retainedLocalRebind}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	p := copied.Plan.Review.Preparation.Approval.Plan
	var preparation bootstrapSuccessorPreparationRecord
	self.reader, preparation, err = openBootstrapSuccessorPreparationReaderRebound(ctx, p, true, nil, inspection)
	if err != nil || rootObjectHash(preparation) != rootObjectHash(copied.Plan.Review.Preparation) {
		return nil, errors.Join(errors.New("successor execution original preparation differs"), err)
	}
	claim := rootObjectHash(copied)
	self.local = &bootstrapSuccessorExecutionDirectory{storage: self.reader.storage, members: self.reader.members, ctx: ctx, path: p.Proposal.OriginalRunDirectory, root: p.Root, file: self.reader.directory, claim: claim, hook: hook}
	if inspection != nil {
		self.local.root = inspection.physical
	}
	registryRoot := copied.Plan.Registry
	if retainedRebind != nil {
		registryRoot = retainedRebind.Plan.RestoredRegistry
	}
	self.registry, err = openBootstrapSuccessorExecutionDirectory(ctx, copied.Plan.Request.RegistryDirectory, registryRoot, claim, hook)
	if err != nil {
		return nil, err
	}
	registryNames, err := self.registry.names()
	if err != nil {
		return nil, err
	}
	for _, name := range registryNames {
		if !strings.HasPrefix(name, "safe-inner-") && !strings.HasPrefix(name, "relayer-outer-") &&
			!strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix+"safe-inner-") && !strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix+"relayer-outer-") {
			return nil, errors.New("successor nonce registry is not a dedicated custody directory")
		}
	}
	if err := self.local.checkpoint("execution-owner-acquired"); err != nil {
		return nil, err
	}
	names, err := self.local.names()
	if err != nil {
		return nil, err
	}
	claimName, readyName := bootstrapSuccessorExecutionPrefix+".claim", bootstrapSuccessorExecutionPrefix+".ready"
	claimPresent, namespacePresent, ready := false, false, false
	if pending := self.local.members.census.Pending; pending != nil {
		namespacePresent = true
		claimPresent = !pending.Append && pending.Name == claimName && pending.Stage == self.local.stageName(claimName, "claim") && pending.Sha256 == safeReleaseHash(raw)
	}
	for _, name := range names {
		if strings.HasPrefix(name, bootstrapSuccessorExecutionPrefix) || strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix) {
			namespacePresent = true
			if name == claimName || name == self.local.stageName(claimName, "claim") {
				claimPresent = true
			}
			if strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix) && !strings.Contains(name, "-"+strings.TrimPrefix(claim, "sha256:")+"-") {
				return nil, errors.New("successor execution root retains another staged claimant")
			}
		}
		ready = ready || name == readyName
	}
	if create && namespacePresent || !create && !claimPresent {
		return nil, errors.New("successor execution requires unused custody for claim or its exact retained claim for resume")
	}
	if retainedRebind != nil || retainedLocalRebind != nil {
		if !ready {
			return nil, errors.New("successor physical rebind requires completed original claim and nonce custody")
		}
		if pending := self.local.members.census.Pending; pending != nil && !(retainedRebind != nil && pending.Name == bootstrapSuccessorRegistryRebindFile) && !(retainedLocalRebind != nil && pending.Name == bootstrapSuccessorLocalRebindFile) &&
			!(retainedLocalRebind != nil && planSha256(retainedLocalRebind.Plan.PendingOutcomeSha256) && pending.Sha256 == retainedLocalRebind.Plan.PendingOutcomeSha256) {
			return nil, errors.New("successor physical rebind requires a completed original local publication checkpoint")
		}
	}
	if ready {
		// A complete owner can never recreate a missing adoption or nonce claim.
		for _, item := range []struct {
			directory *bootstrapSuccessorExecutionDirectory
			name      string
			expected  []byte
		}{{directory: self.local, name: claimName, expected: raw}, {directory: self.local, name: readyName, expected: []byte(claim + "\n")}} {
			retained, err := item.directory.read(item.name)
			if err != nil || !bytes.Equal(retained, item.expected) {
				return nil, errors.Join(errors.New("successor execution completed claim is missing or changed"), err)
			}
		}
		for _, name := range copied.Plan.nonceNames() {
			retained, err := self.registry.read(name)
			if err != nil || !bytes.Equal(retained, self.nonceBytes(name)) {
				return nil, errors.Join(errors.New("successor execution completed nonce custody is missing or changed"), err)
			}
		}
		if _, err := self.local.read(bootstrapSuccessorExecutionEventName(0) + ".json"); err != nil {
			return nil, errors.Join(errors.New("successor execution completed adoption is missing"), err)
		}
	}
	if pending := self.local.members.census.Pending; (retainedRebind != nil || retainedLocalRebind != nil) && pending != nil && (pending.Name == bootstrapSuccessorRegistryRebindFile || pending.Name == bootstrapSuccessorLocalRebindFile) {
		// Only this previously retained exact rebind intent may be completed
		// before inspecting history. It cannot displace an original outcome.
		var err error
		if self.local.members.census.Pending.Name == bootstrapSuccessorLocalRebindFile {
			err = self.retainLocalRebind()
		} else {
			err = self.retainRegistryRebind()
		}
		if err != nil {
			return nil, err
		}
	}
	if err := self.local.publish(claimName, "claim", raw); err != nil {
		return nil, err
	}
	for _, name := range copied.Plan.nonceNames() {
		if err := self.registry.publish(name, "nonce", self.nonceBytes(name)); err != nil {
			return nil, err
		}
	}
	if err := self.local.resumePending(); err != nil {
		return nil, err
	}
	if !ready {
		initial := bootstrapSuccessorExecutionEvent{Schema: bootstrapSuccessorExecutionEventSchema, ApprovalHash: claim, Phase: "adopted",
			PreviousHash: claim, CumulativeAttempts: p.Proposal.Budget.RetainedAttempts, ReservedLifetimeWei: copied.Plan.Review.Relayer.CumulativeLiabilityWei}
		initial.ContentHash = rootObjectHash(initial)
		if err := self.publishEvent(initial); err != nil {
			return nil, err
		}
		if err := self.local.publish(readyName, "ready", []byte(claim+"\n")); err != nil {
			return nil, err
		}
	}
	if err := self.loadEvents(); err != nil {
		return nil, err
	}
	if pending := self.local.members.census.Pending; pending != nil && retainedLocalRebind != nil {
		self.deferredLocalRebind = true
		if err := self.checkDeferredLocalRebind(); err != nil {
			return nil, err
		}
	}
	if err := self.retainRegistryRebind(); err != nil {
		return nil, err
	}
	if err := self.retainLocalRebind(); err != nil {
		return nil, err
	}
	return self, self.checkpoint("execution-ready")
}

// Every event has a separate full write-ahead intent. A missing final event is
// recoverable only from that exact intent, never by creating a fresh counter.
func bootstrapSuccessorExecutionEventName(sequence uint16) string {
	return fmt.Sprintf("%s-%03d", bootstrapSuccessorExecutionPrefix, sequence)
}

// Publication errors terminate this owner. A caller must reopen all authority
// and reconstruct retained state before attempting another transition.
func (self *bootstrapSuccessorExecutionStore) publishEvent(event bootstrapSuccessorExecutionEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	name := bootstrapSuccessorExecutionEventName(event.Sequence)
	if err := self.local.publish(name+".intent", event.Phase, raw); err != nil {
		return err
	}
	return self.local.publish(name+".json", event.Phase, raw)
}

// A partial attempt intent is deterministic and resumes conservatively as a
// counted attempt. Partial outcome intents wait for exact canonical readback;
// their existence forbids any send, even if the transaction is not yet found.
func (self *bootstrapSuccessorExecutionStore) loadEvents() error {
	names, err := self.local.names()
	if err != nil {
		return err
	}
	if raw, err := self.local.read(bootstrapSuccessorCanonicalFile); err == nil {
		var authority bootstrapSuccessorCanonicalApproval
		if err := decodePlanJson(raw, &authority); err != nil {
			return err
		}
		if err := authority.validate(self.local.ctx, self.planCopy()); err != nil {
			return err
		}
		self.canonicalAuthorityHash = rootObjectHash(authority)
		self.canonicalAuthority = &authority
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	allowed := map[string]bool{bootstrapSuccessorExecutionPrefix + ".claim": true, bootstrapSuccessorExecutionPrefix + ".ready": true,
		bootstrapSuccessorCanonicalFile: true, self.local.stageName(bootstrapSuccessorCanonicalFile, "canonical-authority"): true}
	if err := self.includePhysicalRebindReceipts(allowed, true); err != nil {
		return err
	}
	self.runtimeHistory, err = self.readRuntimeHistory(self.local.ctx, names, allowed)
	if err != nil {
		return err
	}
	self.safeCurrentHistory, err = self.readSafeCurrentHistory(self.local.ctx, names, allowed)
	if err != nil {
		return err
	}
	for sequence := uint16(0); sequence < 32; sequence++ {
		name := bootstrapSuccessorExecutionEventName(sequence)
		raw, err := self.local.read(name + ".intent")
		if errors.Is(err, os.ErrNotExist) {
			if sequence == 0 {
				return errors.New("successor execution lost its adoption intent")
			}
			for _, phase := range []string{"attempt-reserved", "installed", "outer-reverted"} {
				stage := self.local.stageName(name+".intent", phase)
				for _, candidate := range names {
					if candidate == stage {
						if self.pending != "" {
							return errors.New("successor execution has conflicting partial event intents")
						}
						self.pending = phase
						partial, err := self.local.read(stage)
						if err != nil {
							return err
						}
						self.pendingOutcomeHash = safeReleaseHash(partial)
						allowed[stage] = true
					}
				}
			}
			if self.pending == "attempt-reserved" {
				if self.runtimeHistory.pendingHash != "" || self.safeCurrentHistory.pendingHash != "" {
					return errors.New("successor partial attempt overlaps incomplete runtime or current-policy authority")
				}
				event := self.attemptEvent()
				if err := event.validate(self.approval, self.profile, &self.last); err != nil {
					return err
				}
				if err := self.publishEvent(event); err != nil {
					return err
				}
				self.last, self.pending, self.pendingOutcomeHash = event, "", ""
			}
			break
		}
		if err != nil {
			return err
		}
		var event bootstrapSuccessorExecutionEvent
		if err := decodePlanJson(raw, &event); err != nil {
			return err
		}
		canonical, err := json.Marshal(event)
		var previous *bootstrapSuccessorExecutionEvent
		if sequence > 0 {
			previous = &self.last
		}
		if err != nil || !bytes.Equal(raw, canonical) || event.Sequence != sequence {
			return errors.Join(errors.New("successor execution event encoding or sequence differs"), err)
		}
		if err := event.validate(self.approval, self.profile, previous); err != nil {
			return err
		}
		if event.CanonicalAuthorityHash != "" && event.CanonicalAuthorityHash != self.canonicalAuthorityHash {
			return errors.New("successor counted event lost its canonical authorization")
		}
		if err := self.runtimeHistory.validateEvent(event, previous); err != nil {
			return err
		}
		if err := self.safeCurrentHistory.validateEvent(event, previous, self.runtimeHistory); err != nil {
			return err
		}
		if err := self.publishEvent(event); err != nil {
			return err
		}
		allowed[name+".intent"], allowed[name+".json"] = true, true
		// These stages might have been consumed during recovery of exact bytes.
		allowed[self.local.stageName(name+".intent", event.Phase)] = true
		allowed[self.local.stageName(name+".json", event.Phase)] = true
		self.last = event
	}
	for _, name := range names {
		if (strings.HasPrefix(name, bootstrapSuccessorExecutionPrefix) || strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix)) && !allowed[name] {
			return errors.New("successor execution has a gap, orphan, unknown stage or extra event")
		}
	}
	return nil
}

// A durable terminal record is immutable. Attempts retain one maximum outer
// liability across all identical rebroadcasts because the sender/nonce is fixed.
func (self *bootstrapSuccessorExecutionStore) append(event bootstrapSuccessorExecutionEvent) error {
	if err := self.checkpoint("event-admission"); err != nil {
		return err
	}
	if err := event.validate(self.approval, self.profile, &self.last); err != nil {
		return err
	}
	if err := self.runtimeHistory.validateEvent(event, &self.last); err != nil {
		return err
	}
	if event.RuntimeRevisionHash != self.runtimeHistory.hash() || event.Phase == "attempt-reserved" && self.runtimeHistory.pendingHash != "" {
		return errors.New("successor execution transition lacks complete current runtime authority")
	}
	if err := self.safeCurrentHistory.validateEvent(event, &self.last, self.runtimeHistory); err != nil {
		return err
	}
	if event.Phase == "attempt-reserved" && (event.SafeCurrentRevisionHash != self.safeCurrentHistory.hash() || self.safeCurrentHistory.pendingHash != "") {
		return errors.New("successor execution transition lacks complete current-policy authority")
	}
	if self.pending != "" && self.pending != event.Phase {
		return errors.New("successor execution pending outcome cannot be replaced by another transition")
	}
	// Receipt log slices belong to the adapter until copied into this owner.
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var copied bootstrapSuccessorExecutionEvent
	if err := decodePlanJson(raw, &copied); err != nil {
		return err
	}
	if err := self.publishEvent(copied); err != nil {
		return errors.Join(err, self.close())
	}
	self.last, self.pending, self.pendingOutcomeHash = copied, "", ""
	if err := self.completeDeferredLocalRebind(); err != nil {
		return err
	}
	return errors.Join(self.checkpointExecutionHistory(), self.checkpointRuntimeHistory(), self.checkpointSafeCurrentHistory())
}

// Check original preparation, both directories and durable nonce claims again
// at send boundaries. Registry corruption never frees a signature or allowance.
func (self *bootstrapSuccessorExecutionStore) checkpoint(stage string) error {
	if self == nil || self.closed {
		return errors.New("successor execution owner is closed")
	}
	if err := errors.Join(self.reader.checkpoint(stage), self.local.checkpoint(stage), self.registry.checkpoint(stage)); err != nil {
		return err
	}
	for _, name := range self.approval.Plan.nonceNames() {
		raw, err := self.registry.read(name)
		if err != nil || !bytes.Equal(raw, self.nonceBytes(name)) {
			return errors.Join(errors.New("successor execution nonce custody changed"), err)
		}
	}
	return errors.Join(self.checkpointExecutionHistory(), self.checkpointRuntimeHistory(), self.checkpointSafeCurrentHistory())
}

// Release registry, then the borrowed original directory. Original preparation
// marker locks remain the caller's responsibility until this method finishes.
func (self *bootstrapSuccessorExecutionStore) close() error {
	if self == nil || self.closed {
		return nil
	}
	self.closed = true
	err := self.registry.close()
	if self.reader != nil {
		err = errors.Join(err, self.reader.close())
	}
	if self.local != nil {
		self.local.file = nil
	}
	return err
}
