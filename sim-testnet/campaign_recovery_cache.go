// Recovery ancestry reuses one authenticated local proof per attempt. Mutable
// journal appends and live acceptance checks remain outside the cached proof.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// A pointer allows existing attempt value copies without copying a mutex. The
// initializer lock protects only lazy allocation; each proof has its own lock.
var scenarioCampaignRecoveryProofLock sync.Mutex

type scenarioCampaignRecoveryProofCache struct {
	stateLock     sync.Mutex
	contextHash   string
	witnesses     []fleetCensusFileWitness
	ancestorsKVs  map[string]bool
	journalPrefix []byte
}

// Changing caller-owned configuration, custody, payload or matrix geometry
// invalidates the proof even if the archived sources remain unchanged.
func scenarioCampaignRecoveryProofContext(attempt *scenarioCampaignAttempt) (string, error) {
	if attempt == nil || attempt.cfg == nil || attempt.cfg.Config == nil || attempt.roles == nil {
		return "", errors.New("campaign recovery proof has no context")
	}
	definition, err := scenarioDefinitionFor(attempt.cfg, "release-1.0")
	if err != nil {
		return "", err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		StateDir       string
		Config         *ResolvedConfig
		Owner          EVMRoleSecret
		Payload        scenarioCampaignAttemptPayload
		DefinitionHash string
		Provisional    bool
	}{attempt.stateDir, attempt.cfg, attempt.roles.EVM["testnet-owner"], attempt.payload, definitionHash, provisionalResumeEnabled(attempt.cfg)})
	if err != nil {
		return "", err
	}
	return bytesSHA256(raw), nil
}

// A deliberately conservative inventory covers every possible archived plan,
// attempt and historical run source, including absent completion/start markers.
// It never reads large observation or plan payloads on a cache hit.
func scenarioCampaignRecoveryProofWitnesses(attempt *scenarioCampaignAttempt) ([]fleetCensusFileWitness, bool) {
	var witnesses []fleetCensusFileWitness
	add := func(name string, directory bool) bool {
		info, err := os.Lstat(filepath.Join(attempt.stateDir, filepath.FromSlash(name)))
		if errors.Is(err, os.ErrNotExist) && !directory {
			witnesses = append(witnesses, fleetCensusFileWitness{Name: name})
			return true
		}
		if err != nil || info.Mode().Perm()&0o022 != 0 || directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
			return false
		}
		device, inode, uid, changed, err := evidenceRelayStartupFileIdentity(info)
		if err != nil || uid != uint32(os.Geteuid()) {
			return false
		}
		witness := fleetCensusFileWitness{Name: name, Size: info.Size(), Mode: uint32(info.Mode()), Device: device, Inode: inode,
			ModifiedNanosecond: info.ModTime().UnixNano(), ChangedNanosecond: changed}
		if directory {
			// The inventory below witnesses membership. Atomic writes of unrelated
			// live files must not invalidate every immutable historical proof.
			witness.Size, witness.ModifiedNanosecond, witness.ChangedNanosecond = 0, 0, 0
		}
		witnesses = append(witnesses, witness)
		return true
	}
	if !add(".", true) {
		return nil, false
	}
	for _, directory := range []string{"campaign-attempts", "plans", "runs"} {
		if !add(directory, true) {
			return nil, false
		}
		entries, err := os.ReadDir(filepath.Join(attempt.stateDir, directory))
		if err != nil {
			return nil, false
		}
		for _, entry := range entries {
			name := filepath.ToSlash(filepath.Join(directory, entry.Name()))
			if directory == "runs" {
				if entry.Name() == attempt.payload.RunID {
					continue
				}
				if !add(name, true) {
					return nil, false
				}
				for _, source := range []string{"result.json", "observations.jsonl", processLogEvidenceFilename, scenarioCampaignStartFilename, "complete.json", scenarioLifecycleHandoffFilename} {
					if !add(name+"/"+source, false) {
						return nil, false
					}
				}
			} else if directory == "plans" || strings.HasSuffix(entry.Name(), ".evidence.json") {
				if !add(name, false) {
					return nil, false
				}
			}
		}
	}
	return witnesses, true
}

// Read the exact bounded journal through the same safe source reader. Every
// hit revalidates its current hash chain, including newly appended records.
func scenarioCampaignRecoveryProofJournal(stateDir string, prefix []byte) bool {
	if len(prefix) == 0 {
		return true
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "journal.jsonl", maximumCampaignEvidenceRawFileBytes)
	if err != nil || !bytes.HasPrefix(raw, prefix) {
		return false
	}
	journal := &Journal{}
	return journal.loadReader(bytes.NewReader(raw)) == nil
}

// The full validator establishes all signed links once. Reading the already
// authenticated small envelopes then extracts ancestor identities without a
// second plan/observation traversal; before/after witnesses bind those reads.
func scenarioCampaignRecoveryAncestorSources(attempt *scenarioCampaignAttempt) (map[string]bool, bool, error) {
	root, _, err := readScenarioCampaignAttemptAt(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, "release-1.0", scenarioCampaignSuccessorPath(attempt.stateDir))
	if err != nil {
		return nil, false, err
	}
	ancestorsKVs := map[string]bool{root.payload.RunID: true}
	usesJournal := attempt.payload.Recovery.PriorJournalBytes != 0
	files, err := scenarioCampaignRecoveryFiles(attempt.stateDir)
	if err != nil {
		return nil, false, err
	}
	for _, file := range files {
		prior, _, err := readScenarioCampaignAttemptAt(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, "release-1.0", file.path)
		if err != nil {
			return nil, false, err
		}
		if prior.payload.Recovery == nil {
			return nil, false, errors.New("campaign recovery ancestor has no recovery lineage")
		}
		usesJournal = usesJournal || prior.payload.Recovery.PriorJournalBytes != 0
		if prior.payload.RunID == attempt.payload.RunID {
			break
		}
		ancestorsKVs[prior.payload.RunID] = true
	}
	return ancestorsKVs, usesJournal, nil
}

// Failures never populate a cache. Any changed witness returns to the complete
// validator rather than treating an obsolete proof as current acceptance.
func scenarioCampaignRecoveryAncestors(attempt *scenarioCampaignAttempt, validate func(*scenarioCampaignAttempt) error) (map[string]bool, error) {
	if attempt == nil || attempt.payload.Recovery == nil {
		return nil, errors.New("campaign recovery has no exact signed predecessor")
	}
	scenarioCampaignRecoveryProofLock.Lock()
	if attempt.recoveryProof == nil {
		attempt.recoveryProof = &scenarioCampaignRecoveryProofCache{}
	}
	cache := attempt.recoveryProof
	scenarioCampaignRecoveryProofLock.Unlock()
	cache.stateLock.Lock()
	defer cache.stateLock.Unlock()
	contextHash, contextErr := scenarioCampaignRecoveryProofContext(attempt)
	before, safeBefore := scenarioCampaignRecoveryProofWitnesses(attempt)
	if contextErr == nil && safeBefore && cache.contextHash == contextHash && cache.ancestorsKVs != nil && reflect.DeepEqual(before, cache.witnesses) && scenarioCampaignRecoveryProofJournal(attempt.stateDir, cache.journalPrefix) {
		after, safeAfter := scenarioCampaignRecoveryProofWitnesses(attempt)
		if safeAfter && reflect.DeepEqual(before, after) {
			return maps.Clone(cache.ancestorsKVs), nil
		}
	}
	cache.ancestorsKVs = nil
	journalBefore, journalErr := readValidatorEvidenceHistoricalFile(attempt.stateDir, "journal.jsonl", maximumCampaignEvidenceRawFileBytes)
	if err := validate(attempt); err != nil {
		return nil, err
	}
	ancestorsKVs, usesJournal, err := scenarioCampaignRecoveryAncestorSources(attempt)
	if err != nil {
		return nil, err
	}
	after, safeAfter := scenarioCampaignRecoveryProofWitnesses(attempt)
	contextAfter, contextAfterErr := scenarioCampaignRecoveryProofContext(attempt)
	stable := contextErr == nil && contextAfterErr == nil && contextHash == contextAfter && safeBefore && safeAfter && reflect.DeepEqual(before, after)
	var journalPrefix []byte
	if usesJournal {
		journalAfter, afterErr := readValidatorEvidenceHistoricalFile(attempt.stateDir, "journal.jsonl", maximumCampaignEvidenceRawFileBytes)
		stable = stable && journalErr == nil && afterErr == nil && len(journalBefore) != 0 && bytes.Equal(journalBefore, journalAfter)
		journalPrefix = journalAfter
	}
	if stable {
		cache.contextHash, cache.witnesses, cache.ancestorsKVs, cache.journalPrefix = contextHash, after, ancestorsKVs, journalPrefix
	}
	return maps.Clone(ancestorsKVs), nil
}
