//go:build linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
)

const (
	evidenceRelayOwnerPlanCacheEntries = 8
	evidenceRelayOwnerPlanCacheBytes   = 2 * maximumCampaignEvidenceRawFileBytes
)

type evidenceRelayOwnerPlanKey struct {
	contextHash string
	stateDir    string
	planHash    string
	wireHash    [sha256.Size]byte
}

type evidenceRelayOwnerPlanEntry struct {
	plan     *SetupPlan
	rawBytes int
}

// Only the relay worker owns this process-local cache. Decoded plans stay
// private and read-only; callers receive freshly decoded request actions.
// Historical validation depends on the archived plan's embedded artifacts,
// never the current build, finalized chain state or a request's journal debit.
type evidenceRelayOwnerPlanCache struct {
	entryKVs map[evidenceRelayOwnerPlanKey]evidenceRelayOwnerPlanEntry
	order    []evidenceRelayOwnerPlanKey
	rawBytes int
}

// Bind the original approval, current operational configuration and retained
// source commitments. Driver rebuilds cannot alter any of these authorities.
func (self *Executor) evidenceRelayOwnerContextHash() (string, error) {
	if self == nil || self.cfg == nil || self.cfg.Config == nil || self.plan == nil || !provisionalResumeEnabled(self.cfg) {
		return "", errors.New("relay owner cache has no provisional approval")
	}
	provenance := self.cfg.provisionalResume.Record
	if !provenance.Provisional || provenance.FinalAcceptance || provenance.PlanHash != self.plan.PlanHash {
		return "", errors.New("relay owner cache differs from its exact non-accepting approval")
	}
	cfg := self.auditAuthorizedConfig
	if cfg == nil {
		cfg = self.cfg
	}
	return canonicalHashHex(struct {
		Version          string
		PlanHash         string
		PlanConfigHash   string
		ReleaseLockHash  string
		ResolvedInputs   string
		PriorPlanHashes  []string
		Continuation     *EvidenceRelayContinuation
		EvidenceSource   *ValidatorEvidenceSource
		EvidenceCarry    *ValidatorEvidenceCarry
		Deployment       ContractDeployment
		Evidence         *ValidatorEvidenceDeployment
		Config           *HarnessConfig
		ConfigHash       string
		PolicyHash       string
		Public           *PublicManifest
		Release          *ReleaseLock
		ProvisionalPlan  string
		ProvisionalInput string
	}{
		Version: "relay-owner-plan-v1", PlanHash: self.plan.PlanHash, PlanConfigHash: self.plan.ConfigHash,
		ReleaseLockHash: self.plan.ReleaseLockHash, ResolvedInputs: self.plan.ResolvedInputsHash,
		PriorPlanHashes: self.plan.PriorPlanHashes, Continuation: self.plan.EvidenceRelayContinuation,
		EvidenceSource: self.plan.ValidatorEvidenceSource, EvidenceCarry: self.plan.ValidatorEvidenceCarry,
		Deployment: self.plan.Deployment, Evidence: self.plan.ValidatorEvidence,
		Config: cfg.Config, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Public: cfg.Public, Release: cfg.Release,
		ProvisionalPlan: self.cfg.provisionalResume.Record.PlanHash, ProvisionalInput: self.cfg.provisionalResume.Record.ConfigHash,
	})
}

// Fresh request bytes, signatures, deployment identity and journal ownership
// are always checked by the common reader. Strict acceptance uses no cache.
func (self *Executor) readRetainedEvidenceRelayRequest(ctx context.Context, entries []JournalEntry, actionId string) (*SetupPlan, evidenceRelayRequestRecord, []byte, error) {
	if !provisionalResumeEnabled(self.cfg) || !self.cfg.provisionalResume.Record.Provisional || self.cfg.provisionalResume.Record.FinalAcceptance {
		return readOwnedEvidenceRelayRequest(ctx, self.stateDir, self.plan, entries, actionId)
	}
	contextHash, err := self.evidenceRelayOwnerContextHash()
	if err != nil {
		return nil, evidenceRelayRequestRecord{}, nil, err
	}
	if self.evidenceRelayOwnerPlans == nil {
		self.evidenceRelayOwnerPlans = &evidenceRelayOwnerPlanCache{}
	}
	return readOwnedEvidenceRelayRequestWithOwner(ctx, self.stateDir, self.plan, entries, actionId, func(stateDir, planHash string) (*SetupPlan, error) {
		return self.evidenceRelayOwnerPlans.read(ctx, stateDir, contextHash, planHash, nil)
	})
}

// Every lookup freshly reads the bounded, no-follow historical file. The
// private descriptor hook only observes actual reads in deterministic tests.
func (self *evidenceRelayOwnerPlanCache) read(ctx context.Context, stateDir, contextHash, planHash string, opened func(*os.File) error) (*SetupPlan, error) {
	if self == nil || ctx == nil || !validCanonicalHashHex(contextHash) || !validCanonicalHashHex(planHash) {
		return nil, errors.New("relay historical owner cache identity is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := readValidatorEvidenceHistoricalFileObserved(stateDir, "plans/"+stringsTrim0x(planHash)+".json", maximumCampaignEvidenceRawFileBytes, opened)
	if err != nil {
		return nil, err
	}
	return self.decode(ctx, evidenceRelayOwnerPlanKey{contextHash: contextHash, stateDir: stateDir, planHash: planHash}, raw, func(raw []byte) (*SetupPlan, error) {
		return decodePersistedPlanBytesForHistory(raw, true)
	})
}

func (self *evidenceRelayOwnerPlanCache) decode(ctx context.Context, key evidenceRelayOwnerPlanKey, raw []byte, decode func([]byte) (*SetupPlan, error)) (*SetupPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > maximumCampaignEvidenceRawFileBytes {
		return nil, errors.New("relay historical owner cache bytes are incomplete or changed")
	}
	key.wireHash = sha256.Sum256(raw)
	if entry, ok := self.entryKVs[key]; ok {
		return entry.plan, nil
	}
	plan, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if plan == nil || plan.PlanHash != key.planHash || !plan.validatorEvidenceHistorical {
		return nil, errors.New("relay historical owner differs from its exact archived approval")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if self.entryKVs == nil {
		self.entryKVs = map[evidenceRelayOwnerPlanKey]evidenceRelayOwnerPlanEntry{}
	}
	for len(self.order) >= evidenceRelayOwnerPlanCacheEntries || self.rawBytes+len(raw) > evidenceRelayOwnerPlanCacheBytes {
		oldest := self.order[0]
		self.rawBytes -= self.entryKVs[oldest].rawBytes
		delete(self.entryKVs, oldest)
		self.order = self.order[1:]
	}
	self.entryKVs[key] = evidenceRelayOwnerPlanEntry{plan: plan, rawBytes: len(raw)}
	self.order = append(self.order, key)
	self.rawBytes += len(raw)
	return plan, nil
}
