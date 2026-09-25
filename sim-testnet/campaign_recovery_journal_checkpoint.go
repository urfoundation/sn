// Operational recovery retains bounded journal validation state for one
// invocation. Final audits continue to replay the complete source independently.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
)

const scenarioCampaignJournalCheckpointBytes = maximumCampaignEvidenceRawFileBytes

var scenarioCampaignJournalCheckpointLock sync.Mutex

// No mutable journal writer or raw records are retained. Exact source bytes
// authenticate these detached action witnesses before any suffix can use them.
type scenarioCampaignJournalCheckpoint struct {
	info    os.FileInfo
	cut     scenarioCampaignJournalCut
	count   uint64
	journal *Journal
}

// Configuration copies share this invocation, with serialized readers. Failed
// appends leave the last successful checkpoint intact for a subsequent repair.
type scenarioCampaignJournalCache struct {
	stateLock   sync.Mutex
	contextHash string
	checkpoint  *scenarioCampaignJournalCheckpoint
	// Count actual semantic work in deterministic incremental-replay tests.
	validated func()
}

// Bind invocation and chain authority without historical policy projections or
// RPC transport aliases: neither changes the journal's record validation rules.
func scenarioCampaignJournalContext(cfg *ResolvedConfig, stateDir string) (string, bool) {
	if !provisionalResumeEnabled(cfg) || cfg.Config == nil || cfg.strictHistoryAdoption != nil || cfg.relayCapturePlanHash != "" {
		return "", false
	}
	invocation := cfg.provisionalResume
	if !invocation.Record.Provisional || invocation.Record.FinalAcceptance || invocation.Record.ReadOnly {
		return "", false
	}
	genesisHash := ""
	if cfg.Public != nil {
		genesisHash = cfg.Public.Chain.GenesisHash
	}
	hash, err := canonicalHashHex(struct {
		Version            string
		StateDir           string
		DeploymentId       string
		ChainId            uint64
		GenesisHash        string
		Netuid             uint16
		Invocation         *provisionalResumeRecord
		InvocationPath     string
		InvocationHash     string
		Driver             provisionalDriverProvenance
		AcceptedPlanHashes []string
	}{
		Version: "campaign-journal-checkpoint-v1", StateDir: stateDir,
		DeploymentId: cfg.Config.Deployment.DeploymentID, ChainId: cfg.ChainID, GenesisHash: genesisHash, Netuid: cfg.Netuid,
		Invocation: invocation.Record, InvocationPath: invocation.RecordPath, InvocationHash: invocation.RecordHash,
		Driver: invocation.Driver, AcceptedPlanHashes: invocation.AcceptedPlanHashes,
	})
	return hash, err == nil
}

// Memoization is an optimization of provisional continuation, never authority
// to accept a journal. Strict or unavailable invocation context uses full replay.
func readScenarioCampaignJournalSnapshotMemo(cfg *ResolvedConfig, stateDir string, expected *scenarioCampaignJournalCut, visit func(JournalEntry)) (scenarioCampaignJournalCut, scenarioCampaignJournalCut, error) {
	contextHash, enabled := scenarioCampaignJournalContext(cfg, stateDir)
	if !enabled {
		return readScenarioCampaignJournalSnapshot(stateDir, expected, visit)
	}
	scenarioCampaignJournalCheckpointLock.Lock()
	if cfg.provisionalResume.recoveryJournal == nil {
		cfg.provisionalResume.recoveryJournal = &scenarioCampaignJournalCache{}
	}
	cache := cfg.provisionalResume.recoveryJournal
	scenarioCampaignJournalCheckpointLock.Unlock()
	cache.stateLock.Lock()
	defer cache.stateLock.Unlock()
	if cache.contextHash != contextHash {
		cache.contextHash, cache.checkpoint = contextHash, nil
	}
	cut, snapshot, next, err := readScenarioCampaignJournalCheckpoint(stateDir, expected, visit, cache.checkpoint, true, cache.validated)
	if err == nil {
		if after, enabled := scenarioCampaignJournalContext(cfg, stateDir); !enabled || after != contextHash {
			return scenarioCampaignJournalCut{}, scenarioCampaignJournalCut{}, errors.New("campaign recovery journal invocation changed while reading")
		}
		// Oversized witness inventories simply stop memoizing; the streaming
		// reader and its existing per-record bound remain authoritative.
		cache.checkpoint = next
	}
	return cut, snapshot, err
}

// Never mutate a successful prefix while testing an untrusted appended suffix.
func cloneScenarioCampaignJournalWitnesses(source *Journal) *Journal {
	journal := &Journal{lastHash: source.lastHash, deploymentID: source.deploymentID, validationKVs: make(map[journalActionKey][]JournalEntry, len(source.validationKVs))}
	for key, entries := range source.validationKVs {
		journal.validationKVs[key] = slices.Clone(entries)
	}
	return journal
}

// Conservative allocation accounting includes map, slice and struct overhead;
// diagnostic text is already removed and the source file has no total byte cap.
func scenarioCampaignJournalWitnessBytes(journal *Journal) uint64 {
	retained := uint64(512 + len(journal.lastHash) + len(journal.deploymentID))
	for key, entries := range journal.validationKVs {
		retained += uint64(256 + len(key.planHash) + len(key.actionId))
		for _, entry := range entries {
			retained += 1024
			for _, value := range []string{entry.Schema, entry.Time, entry.DeploymentID, entry.PlanHash, entry.ActionID, entry.IntentHash,
				string(entry.Stage), entry.Signer, entry.Nonce, entry.TransactionHash, entry.BlockHash, entry.RecoveryBlockHash,
				entry.PostconditionHash, entry.PostconditionPath, entry.Error, entry.PreviousHash, entry.EntryHash} {
				retained += uint64(len(value))
			}
		}
		if retained > scenarioCampaignJournalCheckpointBytes {
			break
		}
	}
	return retained
}

// A visitor needs the full historical record including diagnostics, but never
// receives the suffix as predecessor authority or a mutable cached witness.
func visitScenarioCampaignJournalPrefix(reader io.Reader, visit func(JournalEntry)) error {
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 64*1024), maximumJournalRecordBytes)
	var count uint64
	for scan.Scan() {
		count++
		var entry JournalEntry
		if err := json.Unmarshal(scan.Bytes(), &entry); err != nil {
			return fmt.Errorf("campaign recovery journal retained prefix line %d: %w", count, err)
		}
		visit(entry)
	}
	return scan.Err()
}
