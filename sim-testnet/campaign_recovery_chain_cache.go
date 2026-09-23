// An invocation may reuse an exact authenticated recovery prefix. Every
// changed source, caller identity or appended link remains a fresh read.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
)

const scenarioCampaignRecoveryChainCacheBytes = maximumCampaignEvidenceRawFileBytes

var scenarioCampaignRecoveryChainCacheLock sync.Mutex

type scenarioCampaignRecoveryProjection struct {
	file       scenarioCampaignRecoveryFile
	raw        []byte
	payload    []byte
	policy     []byte
	historical bool
}

// Configuration copies share the invocation, but callers receive independent
// mutable attempts. The lock joins simultaneous readers without sharing their
// payload maps, slices or per-attempt runtime caches.
type scenarioCampaignRecoveryChainCache struct {
	stateLock   sync.Mutex
	contextHash string
	witnesses   []fleetCensusFileWitness
	projections []scenarioCampaignRecoveryProjection
}

type scenarioCampaignRecoveryChainRead func(*ResolvedConfig, string, *RoleSecrets, string, []scenarioCampaignRecoveryFile, []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error)

// Bind serialized policy and unexported invocation-only admission separately.
func scenarioCampaignRecoveryChainContext(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string) (string, error) {
	if cfg == nil || cfg.Config == nil || roles == nil || !provisionalResumeEnabled(cfg) {
		return "", errors.New("recovery chain cache has no invocation")
	}
	if err := validateScenarioCampaignLineageAdmission(cfg, planHash); err != nil {
		return "", err
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		return "", err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		return "", err
	}
	return canonicalHashHex(struct {
		Version                 string
		StateDir                string
		PlanHash                string
		Config                  *ResolvedConfig
		Roles                   *RoleSecrets
		DefinitionHash          string
		Invocation              *provisionalResumeRecord
		InvocationPath          string
		InvocationHash          string
		ReadOnlyAudit           bool
		RelayCapturePlanHash    string
		ProvisionalRpcAuthority string
		OwnedRpcAuthority       string
		AcceptedPlanHashes      []string
		Driver                  provisionalDriverProvenance
	}{"recovery-chain-v1", stateDir, planHash, cfg, roles, definitionHash, cfg.provisionalResume.Record,
		cfg.provisionalResume.RecordPath, cfg.provisionalResume.RecordHash, cfg.readOnlyAudit,
		cfg.relayCapturePlanHash, cfg.provisionalRPCAuthority, cfg.ownedRPCAuthority, cfg.provisionalResume.AcceptedPlanHashes, cfg.provisionalResume.Driver})
}

// Ctime detects same-size rewrites even when mtime is restored. Directory
// membership is inventoried separately so unrelated live writes stay harmless.
func scenarioCampaignRecoverySourceWitness(stateDir, name string, directory bool) (fleetCensusFileWitness, bool) {
	witness := fleetCensusFileWitness{Name: name}
	info, err := os.Lstat(filepath.Join(stateDir, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) && !directory {
		return witness, true
	}
	if err != nil || info.Mode().Perm()&0o022 != 0 || directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return witness, false
	}
	device, inode, uid, changed, err := evidenceRelayStartupFileIdentity(info)
	if err != nil || uid != uint32(os.Geteuid()) {
		return witness, false
	}
	witness.Size, witness.Mode, witness.Device, witness.Inode = info.Size(), uint32(info.Mode()), device, inode
	witness.ModifiedNanosecond, witness.ChangedNanosecond = info.ModTime().UnixNano(), changed
	if directory {
		witness.Size, witness.ModifiedNanosecond, witness.ChangedNanosecond = 0, 0, 0
	}
	return witness, true
}

// The existing conservative inventory covers original and failed-run evidence,
// including absent terminal markers. Add the active plan and complete journal:
// even a valid append invalidates this immutable invocation snapshot.
func scenarioCampaignRecoveryChainWitnesses(cfg *ResolvedConfig, stateDir, runId string) ([]fleetCensusFileWitness, bool) {
	probe := &scenarioCampaignAttempt{cfg: cfg, stateDir: stateDir, payload: scenarioCampaignAttemptPayload{RunID: runId}}
	witnesses, ok := scenarioCampaignRecoveryProofWitnesses(probe)
	if !ok {
		return nil, false
	}
	for _, name := range []string{"plan.json", "journal.jsonl", "campaign-attempts/production-soak.evidence.json"} {
		witness, ok := scenarioCampaignRecoverySourceWitness(stateDir, name, false)
		if !ok {
			return nil, false
		}
		witnesses = append(witnesses, witness)
	}
	return witnesses, true
}

// Check every positive and negative dependency of the retained prefix. New
// generations may add files, but cannot change any source of the older proof.
func scenarioCampaignRecoveryChainWitnessesMatch(stateDir string, witnesses []fleetCensusFileWitness) bool {
	if len(witnesses) == 0 {
		return false
	}
	for _, expected := range witnesses {
		actual, ok := scenarioCampaignRecoverySourceWitness(stateDir, expected.Name, os.FileMode(expected.Mode).IsDir())
		if !ok || actual != expected {
			return false
		}
	}
	return true
}

// Store only privately owned small envelopes, never mutable runtime attempts.
func scenarioCampaignRecoveryProject(records []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryProjection, error) {
	projections := make([]scenarioCampaignRecoveryProjection, 0, len(records))
	retainedBytes := 0
	for _, record := range records {
		if record.attempt == nil || len(record.raw) == 0 {
			return nil, errors.New("recovery chain projection has no authenticated envelope")
		}
		payload, err := json.Marshal(record.attempt.payload)
		if err != nil {
			return nil, err
		}
		policy, err := json.Marshal(record.attempt.cfg.Policy)
		if err != nil {
			return nil, err
		}
		retainedBytes += len(payload) + len(record.raw) + len(policy)
		if retainedBytes > scenarioCampaignRecoveryChainCacheBytes {
			return nil, errors.New("recovery chain projection exceeds its cache byte bound")
		}
		projections = append(projections, scenarioCampaignRecoveryProjection{file: record.file, raw: bytes.Clone(record.raw), payload: payload, policy: policy, historical: record.attempt.historicalEvidence})
	}
	return projections, nil
}

// Each consumer receives independent payload maps, slices and runtime caches.
func scenarioCampaignRecoveryExpand(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, projections []scenarioCampaignRecoveryProjection) ([]scenarioCampaignRecoveryRecord, error) {
	records := make([]scenarioCampaignRecoveryRecord, 0, len(projections))
	for _, projection := range projections {
		attempt := &scenarioCampaignAttempt{cfg: cfg, stateDir: stateDir, roles: roles, historicalEvidence: projection.historical}
		if err := json.Unmarshal(projection.payload, &attempt.payload); err != nil {
			return nil, err
		}
		if projection.historical {
			view := *cfg
			view.ConfigHash, view.readOnlyAudit = attempt.payload.ConfigHash, true
			view.PolicyHash = attempt.payload.PolicyHash
			view.Policy = nil
			if err := json.Unmarshal(projection.policy, &view.Policy); err != nil {
				return nil, err
			}
			attempt.cfg = &view
		}
		records = append(records, scenarioCampaignRecoveryRecord{file: projection.file, attempt: attempt, raw: bytes.Clone(projection.raw)})
	}
	return records, nil
}

// The small unsigned selector only excludes the current run's mutable output
// from the witness inventory. Full signature/lineage validation must establish
// the same run before any success can be stored or returned from this cache.
func scenarioCampaignRecoverySelectedRun(stateDir string, files []scenarioCampaignRecoveryFile) string {
	if len(files) == 0 {
		return ""
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, files[len(files)-1].relativePath, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return ""
	}
	var envelope ReleaseEvidenceEnvelope
	var payload scenarioCampaignAttemptPayload
	if decodeStrictJSONBytes(raw, &envelope) != nil || decodeStrictJSONBytes(envelope.Payload, &payload) != nil {
		return ""
	}
	return payload.RunID
}

// Cache successful immutable provisional prefixes only within this invocation.
// Failed, changed, over-budget and strict reads retain complete validation.
func readScenarioCampaignRecoveryChainMemo(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, files []scenarioCampaignRecoveryFile, read scenarioCampaignRecoveryChainRead) ([]scenarioCampaignRecoveryRecord, error) {
	if !provisionalResumeEnabled(cfg) || cfg.strictHistoryAdoption != nil || cfg.relayCapturePlanHash != "" || len(files) == 0 {
		return read(cfg, stateDir, roles, planHash, files, nil)
	}
	contextHash, err := scenarioCampaignRecoveryChainContext(cfg, stateDir, roles, planHash)
	if err != nil {
		return nil, err
	}
	scenarioCampaignRecoveryChainCacheLock.Lock()
	if cfg.provisionalResume.recoveryChain == nil {
		cfg.provisionalResume.recoveryChain = &scenarioCampaignRecoveryChainCache{}
	}
	cache := cfg.provisionalResume.recoveryChain
	scenarioCampaignRecoveryChainCacheLock.Unlock()
	cache.stateLock.Lock()
	defer cache.stateLock.Unlock()
	var prefix []scenarioCampaignRecoveryRecord
	if cache.contextHash == contextHash && len(cache.projections) != 0 && len(cache.projections) <= len(files) && scenarioCampaignRecoveryChainWitnessesMatch(stateDir, cache.witnesses) {
		matches := true
		for index, projection := range cache.projections {
			matches = matches && projection.file == files[index]
		}
		if matches {
			prefix, err = scenarioCampaignRecoveryExpand(cfg, stateDir, roles, cache.projections)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(prefix) == len(files) {
		contextAfter, contextErr := scenarioCampaignRecoveryChainContext(cfg, stateDir, roles, planHash)
		if contextErr == nil && contextHash == contextAfter && scenarioCampaignRecoveryChainWitnessesMatch(stateDir, cache.witnesses) {
			return prefix, nil
		}
		prefix = nil
	}
	cache.projections = nil
	selectedRun := scenarioCampaignRecoverySelectedRun(stateDir, files)
	before, safeBefore := scenarioCampaignRecoveryChainWitnesses(cfg, stateDir, selectedRun)
	// Do not bind a newly changed source snapshot to an older prefix simply
	// because the change landed between the lookup and the full inventory.
	if len(prefix) != 0 && !scenarioCampaignRecoveryChainWitnessesMatch(stateDir, cache.witnesses) {
		prefix = nil
	}
	records, err := read(cfg, stateDir, roles, planHash, files, prefix)
	if err != nil {
		return nil, err
	}
	contextAfter, contextErr := scenarioCampaignRecoveryChainContext(cfg, stateDir, roles, planHash)
	if len(prefix) != 0 && (contextErr != nil || contextHash != contextAfter || !scenarioCampaignRecoveryChainWitnessesMatch(stateDir, cache.witnesses)) {
		return nil, errors.New("recovery chain source or context changed during authenticated prefix reuse")
	}
	if len(records) != len(files) || records[len(records)-1].attempt.payload.RunID != selectedRun {
		return records, nil
	}
	after, safeAfter := scenarioCampaignRecoveryChainWitnesses(cfg, stateDir, selectedRun)
	contextAfter, contextErr = scenarioCampaignRecoveryChainContext(cfg, stateDir, roles, planHash)
	if safeBefore && safeAfter && contextErr == nil && contextHash == contextAfter && reflect.DeepEqual(before, after) {
		projections, err := scenarioCampaignRecoveryProject(records)
		if err == nil {
			cache.contextHash, cache.witnesses, cache.projections = contextHash, after, projections
		}
	}
	return records, nil
}
