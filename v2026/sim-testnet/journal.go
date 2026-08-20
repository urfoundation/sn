package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type JournalStage string

const (
	StageIntent    JournalStage = "intent"
	StageBroadcast JournalStage = "broadcast"
	StageIncluded  JournalStage = "included"
	StageFinalized JournalStage = "finalized"
	StageVerified  JournalStage = "postcondition_verified"
	StageFailed    JournalStage = "failed"
)

type JournalEntry struct {
	Schema            string       `json:"schema"`
	Sequence          uint64       `json:"sequence"`
	Time              string       `json:"time"`
	DeploymentID      string       `json:"deployment_id"`
	PlanHash          string       `json:"plan_hash"`
	ActionID          string       `json:"action_id"`
	IntentHash        string       `json:"intent_hash"`
	Stage             JournalStage `json:"stage"`
	Signer            string       `json:"signer,omitempty"`
	Nonce             string       `json:"nonce,omitempty"`
	TransactionHash   string       `json:"transaction_hash,omitempty"`
	BlockNumber       uint64       `json:"block_number,omitempty"`
	BlockHash         string       `json:"block_hash,omitempty"`
	RecoveryBlock     uint64       `json:"recovery_block,omitempty"`
	RecoveryBlockHash string       `json:"recovery_block_hash,omitempty"`
	PostconditionHash string       `json:"postcondition_hash,omitempty"`
	PostconditionPath string       `json:"postcondition_path,omitempty"`
	Error             string       `json:"error,omitempty"`
	PreviousHash      string       `json:"previous_hash,omitempty"`
	EntryHash         string       `json:"entry_hash"`
}
type Journal struct {
	mu           sync.Mutex
	file         *os.File
	lock         *os.File
	path         string
	entries      []JournalEntry
	lastHash     string
	deploymentID string
}

func OpenJournal(stateDir string) (*Journal, error) {
	if err := ensurePrivateDir(stateDir); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(stateDir, "deployment.lock")
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lf.Close()
		return nil, fmt.Errorf("deployment is locked by another process: %w", err)
	}
	path := filepath.Join(stateDir, "journal.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		lf.Close()
		return nil, err
	}
	j := &Journal{file: f, lock: lf, path: path}
	if err := j.load(); err != nil {
		j.Close()
		return nil, err
	}
	return j, nil
}
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	var errs []error
	if j.file != nil {
		errs = append(errs, j.file.Close())
		j.file = nil
	}
	if j.lock != nil {
		_ = syscall.Flock(int(j.lock.Fd()), syscall.LOCK_UN)
		errs = append(errs, j.lock.Close())
		j.lock = nil
	}
	return errors.Join(errs...)
}
func (j *Journal) load() error {
	if _, err := j.file.Seek(0, 0); err != nil {
		return err
	}
	scan := bufio.NewScanner(j.file)
	scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scan.Scan() {
		var e JournalEntry
		if err := json.Unmarshal(scan.Bytes(), &e); err != nil {
			return fmt.Errorf("journal line %d: %w", len(j.entries)+1, err)
		}
		want := e.EntryHash
		e.EntryHash = ""
		h, err := canonicalHashHex(e)
		if err != nil {
			return err
		}
		if want != h {
			return fmt.Errorf("journal line %d hash mismatch", len(j.entries)+1)
		}
		if e.PreviousHash != j.lastHash {
			return fmt.Errorf("journal line %d chain mismatch", len(j.entries)+1)
		}
		if err := j.validateEntry(e); err != nil {
			return fmt.Errorf("journal line %d: %w", len(j.entries)+1, err)
		}
		e.EntryHash = want
		j.entries = append(j.entries, e)
		j.lastHash = want
	}
	if err := scan.Err(); err != nil {
		return err
	}
	_, err := j.file.Seek(0, 2)
	return err
}

func (j *Journal) validateEntry(e JournalEntry) error {
	if e.DeploymentID == "" || e.PlanHash == "" || e.ActionID == "" || e.IntentHash == "" {
		return errors.New("journal entry identity is incomplete")
	}
	if j.deploymentID != "" && e.DeploymentID != j.deploymentID {
		return fmt.Errorf("deployment id %q does not match %q", e.DeploymentID, j.deploymentID)
	}
	switch e.Stage {
	case StageIntent, StageFailed:
	case StageBroadcast:
		if e.Signer == "" || e.Nonce == "" || e.TransactionHash == "" {
			return errors.New("broadcast entry is incomplete")
		}
		if e.RecoveryBlock == 0 || e.RecoveryBlockHash == "" {
			return errors.New("broadcast entry has no finalized recovery checkpoint")
		}
	case StageIncluded, StageFinalized:
		if e.TransactionHash == "" || e.BlockNumber == 0 || e.BlockHash == "" {
			return fmt.Errorf("%s entry is incomplete", e.Stage)
		}
	case StageVerified:
		if e.PostconditionHash == "" || e.PostconditionPath == "" {
			return errors.New("verified entry has no postcondition hash/path")
		}
		wantPath, err := postconditionRelativePath(e.ActionID)
		if err != nil || e.PostconditionPath != wantPath {
			return errors.New("verified entry has a noncanonical postcondition path")
		}
	default:
		return fmt.Errorf("unknown journal stage %q", e.Stage)
	}
	for _, prior := range j.entries {
		if prior.ActionID == e.ActionID && prior.IntentHash == e.IntentHash && prior.PlanHash != e.PlanHash {
			return errors.New("one action intent cannot cross plan hashes")
		}
		if prior.ActionID == e.ActionID && prior.IntentHash == e.IntentHash && prior.TransactionHash != "" && e.TransactionHash != "" && prior.TransactionHash != e.TransactionHash {
			return errors.New("one action intent cannot use multiple transactions")
		}
		if prior.ActionID == e.ActionID && prior.IntentHash == e.IntentHash && prior.Stage == StageBroadcast && e.Stage == StageBroadcast && (prior.Signer != e.Signer || prior.Nonce != e.Nonce || prior.RecoveryBlock != e.RecoveryBlock || prior.RecoveryBlockHash != e.RecoveryBlockHash) {
			return errors.New("replayed broadcast metadata does not match original intent")
		}
	}
	if j.deploymentID == "" {
		j.deploymentID = e.DeploymentID
	}
	return nil
}

func (j *Journal) Append(e JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.validateEntry(e); err != nil {
		return err
	}
	e.Schema = "urnetwork-sim-journal-v1"
	e.Sequence = uint64(len(j.entries) + 1)
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	e.PreviousHash = j.lastHash
	e.EntryHash = ""
	h, err := canonicalHashHex(e)
	if err != nil {
		return err
	}
	e.EntryHash = h
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := j.file.Write(b); err != nil {
		return err
	}
	if err := j.file.Sync(); err != nil {
		return err
	}
	j.entries = append(j.entries, e)
	j.lastHash = h
	return nil
}
func (j *Journal) Entries() []JournalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]JournalEntry(nil), j.entries...)
}
func (j *Journal) LastStage(actionID, intentHash, planHash string) (JournalEntry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.entries) - 1; i >= 0; i-- {
		if j.entries[i].ActionID == actionID && j.entries[i].IntentHash == intentHash && j.entries[i].PlanHash == planHash {
			return j.entries[i], true
		}
	}
	return JournalEntry{}, false
}

// LatestTransaction returns the newest broadcast/inclusion/finality record for
// one exact action intent. A resumed Executor appends a fresh intent marker
// before entering its transaction manager, so LastStage alone would hide the
// already-broadcast transaction and could allocate a second nonce.
func (j *Journal) LatestTransaction(actionID, intentHash string) (JournalEntry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.entries) - 1; i >= 0; i-- {
		entry := j.entries[i]
		if entry.ActionID == actionID && entry.IntentHash == intentHash && entry.TransactionHash != "" {
			return entry, true
		}
	}
	return JournalEntry{}, false
}
