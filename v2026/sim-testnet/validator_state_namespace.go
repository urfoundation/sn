package main

// Legacy validator statistics predate the signed attempt ledger and cannot be
// migrated honestly: their raw assignment, failure, latency and path inputs no
// longer exist. Runtime rendering therefore quarantines the entire legacy
// validator state and starts a fresh namespace before any release process can
// consume it. Signed namespaces are never reset or rewritten. Possible disk
// authority is refused until a separate disk-aware activation can verify it.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	validatorAttemptStateResetSchema        = "urnetwork-sim-validator-attempt-state-reset-v1"
	validatorAttemptStateResetJournalSchema = "urnetwork-sim-validator-attempt-state-reset-journal-v1"
	validatorAttemptStateResetJournalName   = ".attempt-state-reset.json"

	validatorStateResetAfterJournal = "after-journal"
	validatorStateResetAfterRename  = "after-rename"
	validatorStateResetAfterMkdir   = "after-fresh-mkdir"
	validatorStateResetAfterReceipt = "after-receipt"
)

type validatorAttemptStateReset struct {
	Schema       string `json:"schema"`
	DeploymentID string `json:"deployment_id"`
	ValidatorID  int    `json:"validator_id"`
	SourceHash   string `json:"source_hash"`
	FileCount    int    `json:"file_count"`
	Reason       string `json:"reason"`
	Complete     bool   `json:"complete"`
}

// validatorAttemptStateResetJournal is private runtime coordination. The
// public receipt deliberately omits ArchiveName so private legacy material is
// neither located nor published; the journal binds the exact archive during
// crash recovery.
type validatorAttemptStateResetJournal struct {
	Schema       string `json:"schema"`
	DeploymentID string `json:"deployment_id"`
	ValidatorID  int    `json:"validator_id"`
	Operators    int    `json:"operators"`
	SourceHash   string `json:"source_hash"`
	FileCount    int    `json:"file_count"`
	ArchiveName  string `json:"archive_name"`
	Status       string `json:"status"`
}

type validatorStateResetHook func(stage string) error

// Checks every configured namespace without creating or changing artifacts.
// Callers retain writer exclusion; mutation paths repeat this preflight.
func preflightSignedAttemptStateNamespaces(cfg *ResolvedConfig, stateDir string) error {
	if cfg == nil || cfg.Config == nil || stateDir == "" || cfg.Config.Topology.Validators < 1 || cfg.Config.Topology.Operators < 1 {
		return errors.New("validator attempt-state namespace inputs are incomplete")
	}
	// Find protected current or archived authority in every configured validator
	// before the first reset. Writers must already be stopped; this is not a lock.
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		if err := preflightSignedAttemptStateNamespace(cfg.Config.Deployment.DeploymentID, stateDir, validatorID, cfg.Config.Topology.Operators); err != nil {
			return err
		}
	}
	return nil
}

// Archives unsigned state only at the original namespace preparation stage.
func prepareSignedAttemptStateNamespaces(cfg *ResolvedConfig, stateDir string) error {
	if err := preflightSignedAttemptStateNamespaces(cfg, stateDir); err != nil {
		return err
	}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		if err := prepareSignedAttemptStateNamespace(cfg.Config.Deployment.DeploymentID, stateDir, validatorID, cfg.Config.Topology.Operators); err != nil {
			return err
		}
	}
	return nil
}

func prepareSignedAttemptStateNamespace(deploymentID, stateDir string, validatorID, operators int) error {
	return prepareSignedAttemptStateNamespaceWithHook(deploymentID, stateDir, validatorID, operators, nil)
}

func prepareSignedAttemptStateNamespaceWithHook(deploymentID, stateDir string, validatorID, operators int, hook validatorStateResetHook) error {
	if err := preflightSignedAttemptStateNamespace(deploymentID, stateDir, validatorID, operators); err != nil {
		return err
	}
	root := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID))
	state := filepath.Join(root, "state")
	journalPath := filepath.Join(root, validatorAttemptStateResetJournalName)
	if _, err := os.Lstat(journalPath); err == nil {
		if err := requireValidatorStateStopped(stateDir, validatorID); err != nil {
			return err
		}
		if err := reconcileValidatorAttemptStateReset(deploymentID, stateDir, root, state, journalPath, validatorID, operators, hook); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	legacy, signed, err := classifyValidatorAttemptState(state, operators)
	if err != nil {
		return fmt.Errorf("validator %d attempt-state classification: %w", validatorID, err)
	}
	if !legacy {
		return nil
	}
	if signed {
		return fmt.Errorf("validator %d mixes signed and legacy unsigned measurement state", validatorID)
	}
	if err := requireValidatorStateStopped(stateDir, validatorID); err != nil {
		return err
	}
	sourceHash, fileCount, err := hashValidatorStateTree(state)
	if err != nil {
		return fmt.Errorf("validator %d legacy state hash: %w", validatorID, err)
	}
	archiveName := validatorLegacyArchiveName(sourceHash)
	journal := validatorAttemptStateResetJournal{
		Schema: validatorAttemptStateResetJournalSchema, DeploymentID: deploymentID, ValidatorID: validatorID,
		Operators: operators, SourceHash: sourceHash, FileCount: fileCount, ArchiveName: archiveName, Status: "pending",
	}
	if err := persistValidatorAttemptStateResetJournal(journalPath, &journal); err != nil {
		return fmt.Errorf("persist validator %d state reset journal: %w", validatorID, err)
	}
	if err := runValidatorStateResetHook(hook, validatorStateResetAfterJournal); err != nil {
		return err
	}
	return reconcileValidatorAttemptStateReset(deploymentID, stateDir, root, state, journalPath, validatorID, operators, hook)
}

// Inspect current state and any old recovery archive without creating a
// journal, receipt or fresh directory. Per-namespace mutation rechecks it.
func preflightSignedAttemptStateNamespace(deploymentID, stateDir string, validatorID, operators int) error {
	if deploymentID == "" || stateDir == "" || validatorID < 1 || operators < 1 {
		return errors.New("validator attempt-state reset identity is incomplete")
	}
	root := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID))
	state := filepath.Join(root, "state")
	legacy, signed, err := classifyValidatorAttemptState(state, operators)
	if err != nil {
		return fmt.Errorf("validator %d attempt-state classification: %w", validatorID, err)
	}
	if legacy && signed {
		return fmt.Errorf("validator %d mixes signed and legacy unsigned measurement state", validatorID)
	}
	journalPath := filepath.Join(root, validatorAttemptStateResetJournalName)
	if _, err := os.Lstat(journalPath); err == nil {
		if err := requireValidatorStateStopped(stateDir, validatorID); err != nil {
			return err
		}
		_, _, err := inspectValidatorAttemptStateReset(deploymentID, root, state, journalPath, validatorID, operators)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if legacy {
		return requireValidatorStateStopped(stateDir, validatorID)
	}
	return nil
}

func reconcileValidatorAttemptStateReset(deploymentID, stateDir, root, state, journalPath string, validatorID, operators int, hook validatorStateResetHook) error {
	journal, archiveExists, err := inspectValidatorAttemptStateReset(deploymentID, root, state, journalPath, validatorID, operators)
	if err != nil {
		return err
	}
	archive := filepath.Join(root, journal.ArchiveName)
	if !archiveExists {
		if err := os.Rename(state, archive); err != nil {
			return fmt.Errorf("quarantine validator %d legacy unsigned state: %w", validatorID, err)
		}
		if err := runValidatorStateResetHook(hook, validatorStateResetAfterRename); err != nil {
			return err
		}
	}
	if err := verifyValidatorLegacyArchive(archive, journal.SourceHash, journal.FileCount); err != nil {
		return fmt.Errorf("validator %d state reset archive: %w", validatorID, err)
	}
	if exists, err := regularDirectoryExists(state); err != nil {
		return fmt.Errorf("validator %d fresh state namespace: %w", validatorID, err)
	} else if !exists {
		if err := os.MkdirAll(state, 0o700); err != nil {
			return fmt.Errorf("create validator %d signed state namespace: %w", validatorID, err)
		}
		if err := runValidatorStateResetHook(hook, validatorStateResetAfterMkdir); err != nil {
			return err
		}
	} else if journal.Status == "pending" {
		empty, err := directoryTreeEmpty(state)
		if err != nil {
			return fmt.Errorf("validator %d fresh state namespace: %w", validatorID, err)
		}
		if !empty {
			return fmt.Errorf("validator %d pending state reset has nonempty fresh namespace", validatorID)
		}
	}
	receipt := validatorAttemptStateReset{
		Schema: validatorAttemptStateResetSchema, DeploymentID: deploymentID, ValidatorID: validatorID,
		SourceHash: journal.SourceHash, FileCount: journal.FileCount, Reason: "legacy statistics have no signed attempt-ledger authority", Complete: true,
	}
	receiptPath := validatorAttemptStateResetReceiptPath(stateDir, validatorID, journal.SourceHash)
	if err := persistValidatorAttemptStateResetReceipt(receiptPath, &receipt); err != nil {
		return fmt.Errorf("persist validator %d state reset receipt: %w", validatorID, err)
	}
	if journal.Status == "pending" {
		if err := runValidatorStateResetHook(hook, validatorStateResetAfterReceipt); err != nil {
			return err
		}
		journal.Status = "complete"
		if err := persistValidatorAttemptStateResetJournal(journalPath, journal); err != nil {
			return fmt.Errorf("complete validator %d state reset journal: %w", validatorID, err)
		}
	}
	return nil
}

// An old exact hash is not reset authority. Both pending sources and existing
// archives must still be exclusively unsigned before recovery changes state.
func inspectValidatorAttemptStateReset(deploymentID, root, state, journalPath string, validatorID, operators int) (*validatorAttemptStateResetJournal, bool, error) {
	var journal validatorAttemptStateResetJournal
	if err := decodeStrictJSONFile(journalPath, &journal); err != nil {
		return nil, false, fmt.Errorf("decode validator %d state reset journal: %w", validatorID, err)
	}
	if err := validateValidatorAttemptStateResetJournal(&journal, deploymentID, validatorID, operators); err != nil {
		return nil, false, err
	}
	archive := filepath.Join(root, journal.ArchiveName)
	if !pathWithinRoot(root, archive) {
		return nil, false, fmt.Errorf("validator %d state reset archive escapes runtime root", validatorID)
	}
	legacy, signed, err := classifyValidatorAttemptState(state, operators)
	if err != nil {
		return nil, false, fmt.Errorf("validator %d state reset source: %w", validatorID, err)
	}
	if legacy && signed {
		return nil, false, fmt.Errorf("validator %d mixes signed and legacy unsigned measurement state", validatorID)
	}
	archiveExists, err := regularDirectoryExists(archive)
	if err != nil {
		return nil, false, fmt.Errorf("validator %d state reset archive: %w", validatorID, err)
	}
	if !archiveExists {
		if journal.Status == "complete" {
			return nil, false, fmt.Errorf("validator %d completed state reset archive is missing", validatorID)
		}
		if !legacy || signed {
			return nil, false, fmt.Errorf("validator %d pending state reset source is not exclusively legacy unsigned state", validatorID)
		}
		hash, count, hashErr := hashValidatorStateTree(state)
		if hashErr != nil || hash != journal.SourceHash || count != journal.FileCount {
			return nil, false, fmt.Errorf("validator %d pending state reset source differs from journal", validatorID)
		}
		return &journal, false, nil
	}
	archiveLegacy, archiveSigned, err := classifyValidatorAttemptState(archive, operators)
	if err != nil {
		return nil, false, fmt.Errorf("validator %d state reset archive: %w", validatorID, err)
	}
	if !archiveLegacy || archiveSigned {
		return nil, false, fmt.Errorf("validator %d state reset archive is not exclusively legacy unsigned state", validatorID)
	}
	if err := verifyValidatorLegacyArchive(archive, journal.SourceHash, journal.FileCount); err != nil {
		return nil, false, fmt.Errorf("validator %d state reset archive: %w", validatorID, err)
	}
	if journal.Status == "pending" {
		if exists, err := regularDirectoryExists(state); err != nil {
			return nil, false, fmt.Errorf("validator %d fresh state namespace: %w", validatorID, err)
		} else if exists {
			empty, err := directoryTreeEmpty(state)
			if err != nil {
				return nil, false, fmt.Errorf("validator %d fresh state namespace: %w", validatorID, err)
			}
			if !empty {
				return nil, false, fmt.Errorf("validator %d pending state reset has nonempty fresh namespace", validatorID)
			}
		}
	}
	return &journal, true, nil
}

func validateValidatorAttemptStateResetJournal(journal *validatorAttemptStateResetJournal, deploymentID string, validatorID, operators int) error {
	if journal == nil || journal.Schema != validatorAttemptStateResetJournalSchema || journal.DeploymentID != deploymentID || journal.ValidatorID != validatorID || journal.Operators != operators || !validSHA256ContentHash(journal.SourceHash) || journal.FileCount < 1 || journal.ArchiveName != validatorLegacyArchiveName(journal.SourceHash) || (journal.Status != "pending" && journal.Status != "complete") {
		return fmt.Errorf("validator %d state reset journal is incomplete or differs from this deployment", validatorID)
	}
	return nil
}

func validatorLegacyArchiveName(sourceHash string) string {
	return "state-legacy-unsigned-" + strings.TrimPrefix(sourceHash, "sha256:")
}

func validatorAttemptStateResetReceiptPath(stateDir string, validatorID int, sourceHash string) string {
	return filepath.Join(stateDir, "receipts", "validator-state-namespaces", fmt.Sprintf("validator-%d-%s.json", validatorID, strings.TrimPrefix(sourceHash, "sha256:")))
}

func persistValidatorAttemptStateResetJournal(path string, journal *validatorAttemptStateResetJournal) error {
	wire, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(wire, '\n'), 0o600)
}

func persistValidatorAttemptStateResetReceipt(path string, receipt *validatorAttemptStateReset) error {
	wire, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	wire = append(wire, '\n')
	current, err := os.ReadFile(path)
	if err == nil {
		if !bytes.Equal(current, wire) {
			return errors.New("existing validator attempt-state reset receipt differs")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(path, wire, 0o644)
}

func runValidatorStateResetHook(hook validatorStateResetHook, stage string) error {
	if hook == nil {
		return nil
	}
	if err := hook(stage); err != nil {
		return fmt.Errorf("validator state reset interrupted %s: %w", stage, err)
	}
	return nil
}

func regularDirectoryExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("%s is not a regular directory", path)
	}
	return true, nil
}

func directoryTreeEmpty(root string) (bool, error) {
	empty := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", path)
		}
		if !entry.IsDir() {
			empty = false
		}
		return nil
	})
	return empty, err
}

func verifyValidatorLegacyArchive(archive, sourceHash string, fileCount int) error {
	hash, count, err := hashValidatorStateTree(archive)
	if err != nil {
		return err
	}
	if hash != sourceHash || count != fileCount || filepath.Base(archive) != validatorLegacyArchiveName(hash) {
		return errors.New("legacy archive hash, count, or deterministic name differs")
	}
	return nil
}

func classifyValidatorAttemptState(state string, operators int) (legacy, signed bool, err error) {
	if operators < 1 {
		return false, false, errors.New("operator count is invalid")
	}
	if _, statErr := os.Lstat(state); errors.Is(statErr, os.ErrNotExist) {
		return false, false, nil
	} else if statErr != nil {
		return false, false, statErr
	}
	ledgerPresent := make(map[int]bool, operators)
	dynamicFiles := 0
	unexpectedLedger := false
	err = filepath.WalkDir(state, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// These are the disk ledger's store and durable import/ready markers.
		// Presence protects even malformed, empty, misplaced or aliased state;
		// do not descend into a database or infer unsigned history from JSONL.
		switch entry.Name() {
		case "attempt-ledger.records", "attempt-ledger-import.json", "attempt-ledger-import.json.tmp", "attempt-ledger-ready.json", "attempt-ledger-ready.json.tmp":
			return fmt.Errorf("%s is protected disk attempt-ledger state; explicit disk-aware activation is required", path)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		relative, err := filepath.Rel(state, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if validatorAttemptStateStaticCredential(relative) {
			return nil
		}
		dynamicFiles++
		parts := strings.Split(relative, "/")
		if entry.Name() == "attempt-ledger.jsonl" {
			if len(parts) != 3 || parts[0] != "operators" {
				unexpectedLedger = true
				return nil
			}
			noID, parseErr := strconv.Atoi(strings.TrimPrefix(parts[1], "no-"))
			if parseErr == nil && parts[1] == fmt.Sprintf("no-%d", noID) && noID >= 1 && noID <= operators {
				info, infoErr := entry.Info()
				if infoErr != nil {
					return infoErr
				}
				if info.Size() > 0 {
					ledgerPresent[noID] = true
				}
			} else {
				unexpectedLedger = true
			}
		}
		return nil
	})
	if err != nil {
		return false, false, err
	}
	ledgerCount := len(ledgerPresent)
	signed = ledgerCount != 0 || unexpectedLedger
	// A measurement namespace is accepted only when every configured operator
	// has a nonempty signed ledger. Any other dynamic/unknown file tree is
	// legacy or incomplete and can never be silently blessed. Out-of-census
	// ledgers are possible signed authority too, never a reset permission.
	legacy = dynamicFiles != 0 && (ledgerCount != operators || unexpectedLedger)
	return legacy, signed, nil
}

func validatorAttemptStateStaticCredential(relative string) bool {
	switch relative {
	case ".validator.key", ".validator.jwt", "evm.key", "hotkey.seed":
		return true
	}
	parts := strings.Split(relative, "/")
	if len(parts) != 3 || parts[0] != "operators" || !strings.HasPrefix(parts[1], "no-") {
		return false
	}
	switch parts[2] {
	case "client.key", "client.jwt", "network.jwt":
		return true
	default:
		return false
	}
}

func requireValidatorStateStopped(stateDir string, validatorID int) error {
	path := filepath.Join(stateDir, "supervisor.state.json")
	var state SupervisorState
	if err := readJSONFile(path, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, process := range state.Processes {
		if process.ID == fmt.Sprintf("validator-%d", validatorID) && process.PID > 1 {
			return fmt.Errorf("validator %d legacy state cannot be quarantined while supervisor records pid %d", validatorID, process.PID)
		}
	}
	return nil
}

func hashValidatorStateTree(root string) (string, int, error) {
	type item struct {
		path string
		data []byte
	}
	items := []item{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy validator state path %s is a symlink", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("legacy validator state path %s is not regular", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		items = append(items, item{path: filepath.ToSlash(relative), data: data})
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	if len(items) == 0 {
		return "", 0, errors.New("legacy validator state is empty")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	hash := sha256.New()
	for _, item := range items {
		_, _ = hash.Write([]byte(item.path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(item.data)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), len(items), nil
}
