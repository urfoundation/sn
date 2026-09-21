// Only complete immutable proof inputs can cross a compatible runner
// revision. Current state, local evidence and canonical checks stay outside
// the cache, and the prior approval must belong to the admitted plan lineage.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

type historicalAuditPlanReaderKey struct{}

// Source plans are immutable within one read-only reconciliation, exactly as
// for carried receipt admission. No success or source decoder survives a new
// invocation through this in-memory helper.
type historicalAuditPlanReader struct {
	stateLock  sync.Mutex
	readSource func(string, string) (*SetupPlan, error)
	plans      map[string]*SetupPlan
	pending    map[string]*historicalAuditPlanRead
}

type historicalAuditPlanRead struct {
	done chan struct{}
	plan *SetupPlan
	err  error
}

func (self *historicalAuditPlanReader) read(ctx context.Context, stateDir, hash string) (*SetupPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := stateDir + "\x00" + hash
	self.stateLock.Lock()
	if plan := self.plans[key]; plan != nil {
		self.stateLock.Unlock()
		return plan, ctx.Err()
	}
	if pending := self.pending[key]; pending != nil {
		self.stateLock.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending.done:
			return pending.plan, errors.Join(pending.err, ctx.Err())
		}
	}
	pending := &historicalAuditPlanRead{done: make(chan struct{})}
	if self.pending == nil {
		self.pending = map[string]*historicalAuditPlanRead{}
	}
	self.pending[key] = pending
	self.stateLock.Unlock()
	// File decoding runs outside the lock. Other source plans and canceled
	// waiters do not wait for unrelated archival I/O.
	plan, err := self.readSource(stateDir, hash)
	err = errors.Join(err, ctx.Err())
	if err == nil && (plan == nil || plan.PlanHash != hash) {
		err = errors.New("historical audit source plan identity differs")
	}
	self.stateLock.Lock()
	if err == nil {
		if self.plans == nil {
			self.plans = map[string]*SetupPlan{}
		}
		self.plans[key] = plan
		pending.plan = plan
	}
	pending.err = err
	delete(self.pending, key)
	close(pending.done)
	self.stateLock.Unlock()
	return pending.plan, err
}

// Fleet callers bind every selector, expected value and decoder input. Native
// callers bind the exact block, transaction, observer and reviewed runtimes.
// These immutable comparisons do not depend on unrelated runner source.
// A new verifier contract must change historicalAuditCacheVerifierVersion.
func (self *Executor) historicalAuditCompatibilityHash(cfg *ResolvedConfig, kind string, input any) (string, bool) {
	if kind != "fleet-install-pinned-state-v1" && kind != "fleet-install-pinned-group-v1" && kind != historicalFleetGenerationOneCacheKind && kind != historicalNativeExtrinsicCacheKind {
		return "", false
	}
	if self == nil || self.plan == nil || cfg == nil || cfg.Release == nil || cfg.Config == nil ||
		!validCanonicalHashHex(self.plan.PlanHash) || !validCanonicalHashHex(self.plan.ReleaseLockHash) ||
		self.plan.DeploymentID != cfg.Config.Deployment.DeploymentID || self.plan.ChainID != cfg.ChainID || self.plan.Netuid != cfg.Netuid || self.plan.Owner != cfg.WalletPublic {
		return "", false
	}
	if kind == historicalNativeExtrinsicCacheKind {
		proofInput, ok := input.(historicalNativeExtrinsicCacheInput)
		if !ok || provisionalResumeEnabled(self.cfg) {
			return "", false
		}
		expected, err := nativeHistoryCacheInput(cfg, proofInput.Recorded, proofInput.Transaction, proofInput.Observer)
		if err != nil || expected != proofInput {
			return "", false
		}
	} else {
		wire, err := json.Marshal(input)
		if err != nil {
			return "", false
		}
		var identity struct {
			Action Action `json:"action"`
		}
		if json.Unmarshal(wire, &identity) != nil || identity.Action.ID == "" {
			return "", false
		}
		actionHash, err := canonicalHashHex(identity.Action)
		if err != nil {
			return "", false
		}
		matched := 0
		for _, action := range self.plan.Actions {
			if action.ID != identity.Action.ID {
				continue
			}
			approvedHash, err := canonicalHashHex(action)
			if err != nil || approvedHash != actionHash {
				return "", false
			}
			matched++
		}
		if matched != 1 {
			return "", false
		}
	}
	// Retain the complete release contract except the two runner build pins.
	// Runtime, ABI, dependencies, protocol and every other repository pin stay
	// exact. Plan/action approval is checked separately for each candidate.
	release := *cfg.Release
	release.Repositories = map[string]any{}
	for name, value := range cfg.Release.Repositories {
		if name != "sn_go_source_hash" && name != "sn_audited_base_commit" {
			release.Repositories[name] = value
		}
	}
	compatibleConfig := *cfg
	compatibleConfig.Release = &release
	compatiblePlan := *self.plan
	compatiblePlan.PlanHash, compatiblePlan.ReleaseLockHash, compatiblePlan.ResolvedInputsHash = "", "", ""
	compatiblePlan.PriorPlanHashes = nil
	compatibleExecutor := *self
	compatibleExecutor.plan = &compatiblePlan
	hash, err := compatibleExecutor.historicalAuditContextHash(&compatibleConfig)
	return hash, err == nil
}

// Exact-context old proofs can be authenticated and enriched. Their opaque
// context hash alone never grants permission to cross an approval boundary.
func (self *Executor) readHistoricalAuditCompatibleSuccess(ctx context.Context, entry *historicalAuditCacheEntry, legacyKey [32]byte) bool {
	exact := *entry
	exact.proof.ApprovalPlanHash, exact.proof.ApprovalReleaseLockHash, exact.proof.CompatibilityHash = "", "", ""
	nameHash, err := canonicalHashHex(exact.proof)
	if err != nil {
		return false
	}
	exact.name = strings.TrimPrefix(nameHash, "0x") + ".json"
	if exact.readSuccess() || exact.readCompatibleSuccess(ctx, legacyKey) {
		return ctx.Err() == nil
	}
	if entry.proof.CompatibilityHash == "" {
		return false
	}
	indexKey := historicalAuditDescendantIndexKey{stateDir: entry.stateDir, contextHash: entry.proof.ContextHash, compatibilityHash: entry.proof.CompatibilityHash}
	value, _ := historicalAuditDescendantIndexes.LoadOrStore(indexKey, &historicalAuditDescendantIndex{})
	for _, proof := range value.(*historicalAuditDescendantIndex).candidates(ctx, entry) {
		if !self.historicalAuditApprovalCompatible(ctx, proof) {
			continue
		}
		candidate := *entry
		candidate.proof = proof
		nameHash, err := canonicalHashHex(proof)
		if err != nil {
			continue
		}
		candidate.name = strings.TrimPrefix(nameHash, "0x") + ".json"
		// The index is only a hint. Reopen the private file and authenticate
		// every byte against the exact input and original approval metadata.
		if candidate.readSuccess() && ctx.Err() == nil {
			return true
		}
	}
	return false
}

func (self *Executor) historicalAuditApprovalCompatible(ctx context.Context, proof historicalAuditCacheProof) bool {
	if ctx.Err() != nil || !self.plan.allowedPlanHashes()[proof.ApprovalPlanHash] {
		return false
	}
	if proof.ApprovalPlanHash == self.plan.PlanHash {
		return proof.ApprovalReleaseLockHash == self.plan.ReleaseLockHash
	}
	source, err := readHistoricalAuditSourcePlan(ctx, self.stateDir, proof.ApprovalPlanHash)
	return err == nil && source != nil && source.PlanHash == proof.ApprovalPlanHash && source.ReleaseLockHash == proof.ApprovalReleaseLockHash &&
		source.DeploymentID == self.plan.DeploymentID && source.ChainID == self.plan.ChainID && source.GenesisHash == self.plan.GenesisHash && source.Netuid == self.plan.Netuid && source.Owner == self.plan.Owner
}

// Share authenticated source decoding within one reconciliation. Ordinary
// calls and later reconciliations still reopen and authenticate the archive.
func readHistoricalAuditSourcePlan(ctx context.Context, stateDir, hash string) (*SetupPlan, error) {
	if ctx == nil {
		return nil, errors.New("historical audit source context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sources, ok := ctx.Value(historicalAuditPlanReaderKey{}).(*historicalAuditPlanReader); ok {
		return sources.read(ctx, stateDir, hash)
	}
	return readValidatorEvidenceHistoricalPlan(stateDir, hash)
}

type historicalAuditDescendantIndexKey struct{ stateDir, contextHash, compatibilityHash string }

type historicalAuditDescendantIndex struct {
	stateLock sync.Mutex
	complete  bool
	proofs    map[string][]historicalAuditCacheProof
}

var historicalAuditDescendantIndexes sync.Map

// Scan at most once per current context and compatibility domain. New current
// successes are addressed directly, while each selected ancestor is reopened
// and its approval revalidated. A canceled or incomplete scan is retryable.
func (self *historicalAuditDescendantIndex) candidates(ctx context.Context, entry *historicalAuditCacheEntry) []historicalAuditCacheProof {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if !self.complete {
		directory, err := openHistoricalAuditCacheDirectory(entry.stateDir, false)
		if err != nil {
			return nil
		}
		defer directory.Close()
		proofs := map[string][]historicalAuditCacheProof{}
		count := 0
		for {
			files, readErr := directory.ReadDir(256)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return nil
			}
			for _, file := range files {
				count++
				if ctx.Err() != nil || count > historicalAuditCacheLegacyMaximumEntries {
					return nil
				}
				if !strings.HasSuffix(file.Name(), ".json") {
					continue
				}
				envelope, ok := readHistoricalAuditCacheEnvelope(directory, file.Name())
				proof := envelope.Proof
				if !ok || proof.Schema != entry.proof.Schema || proof.VerifierVersion != entry.proof.VerifierVersion || proof.CompatibilityHash != entry.proof.CompatibilityHash ||
					!proof.Success || proof.ExecutableSHA256 != "" || !validCanonicalHashHex(proof.ApprovalPlanHash) || !validCanonicalHashHex(proof.ApprovalReleaseLockHash) {
					continue
				}
				nameHash, err := canonicalHashHex(proof)
				if err != nil || strings.TrimPrefix(nameHash, "0x")+".json" != file.Name() {
					continue
				}
				key := proof.Kind + "\x00" + proof.InputHash
				proofs[key] = append(proofs[key], proof)
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
		}
		self.proofs, self.complete = proofs, true
	}
	return self.proofs[entry.proof.Kind+"\x00"+entry.proof.InputHash]
}
