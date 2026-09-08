package validator

// Closed settlement evidence is an immutable export of the already signed
// all-operator transaction, not a reconstruction from the proof projection.
// Publication precedes journal removal, so recovery completes the same export.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const AttemptSettlementClosureSchema = "urnetwork-validator-settlement-closure-v1"

// Every member signs the same complete participant census and terminal block.
type AttemptSettlementClosure struct {
	Schema      string                         `json:"schema"`
	Epoch       uint64                         `json:"epoch"`
	Transitions []*AttemptSettlementTransition `json:"transitions"`
}

// Compares exact native write timestamps and alias count without narrowing
// either supported operating system's timestamp range through a conversion.
type attemptSettlementClosureFileState struct {
	links             uint64
	changeSeconds     int64
	changeNanoseconds int64
	modifySeconds     int64
	modifyNanoseconds int64
}

// Uses a closed epoch, not the successor's native measurement cadence.
func AttemptSettlementClosurePath(coordinatorStateDir string, epoch uint64) string {
	return filepath.Join(coordinatorStateDir, "settlement-closures", strconv.FormatUint(epoch, 10)+".json")
}

// Rejects noncanonical encodings before authenticating the existing signatures.
func DecodeAttemptSettlementClosure(data []byte) (*AttemptSettlementClosure, error) {
	return decodeAttemptSettlementClosureWithCutVerifier(data, verifyAttemptLedgerCut)
}

// Preserves exact canonical decoding and batch validation with a private
// operation-owned cut primitive.
func decodeAttemptSettlementClosureWithCutVerifier(data []byte, verifyCut attemptLedgerCutVerifier) (*AttemptSettlementClosure, error) {
	var closure AttemptSettlementClosure
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&closure); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("settlement closure contains trailing JSON")
	}
	canonical, err := json.Marshal(&closure)
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) {
		return nil, errors.New("settlement closure is not canonical")
	}
	if closure.Schema != AttemptSettlementClosureSchema || len(closure.Transitions) == 0 {
		return nil, errors.New("settlement closure identity is incomplete")
	}
	if err := verifyAttemptSettlementBatchWithCutVerifier(closure.Transitions, verifyCut); err != nil {
		return nil, err
	}
	for index, transition := range closure.Transitions {
		if transition.FromBoundary.SettlementEpoch != closure.Epoch || index > 0 && transition.Identity.NoID <= closure.Transitions[index-1].Identity.NoID {
			return nil, errors.New("settlement closure epoch or participant order differs")
		}
	}
	return &closure, nil
}

// Authenticates every server assignment and proof as well as the signed
// settlement batch. Callers still bind the returned validator/operator domain.
func DecodeAttemptSettlementClosureWithServerKeys(data []byte, serverKeys map[uint64]map[byte]ed25519.PublicKey) (*AttemptSettlementClosure, error) {
	return decodeAttemptSettlementClosureWithServerKeysAndVerifier(data, serverKeys, verifyAttemptLedgerCut)
}

// Supplies full server authentication at the statistics pass's existing cut
// boundary, retaining every batch/counter/signature check without a second cut
// pass. Public validator-only decoders keep their original verification mode.
func decodeAttemptSettlementClosureWithServerKeysAndVerifier(data []byte, serverKeys map[uint64]map[byte]ed25519.PublicKey, verifyCut attemptLedgerCutVerifier) (*AttemptSettlementClosure, error) {
	return decodeAttemptSettlementClosureWithCutVerifier(data, func(cut *AttemptLedgerCut, vpk ed25519.PublicKey, _ map[byte]ed25519.PublicKey, _ bool) error {
		if cut == nil {
			return errors.New("settlement closure attempt cut is absent")
		}
		keys := serverKeys[cut.Identity.NoID]
		if len(keys) == 0 {
			return errors.New("settlement closure server-key history is unavailable")
		}
		return verifyCut(cut, vpk, keys, true)
	})
}

// Returns the exact journal-owned signed batch; pristine initialization has no
// previous epoch to close. A partially missing batch is never initialization.
func attemptSettlementClosureFromTransaction(transaction *attemptSettlementTransaction) (*AttemptSettlementClosure, error) {
	closure := &AttemptSettlementClosure{Schema: AttemptSettlementClosureSchema}
	for _, snapshot := range transaction.Snapshots {
		var stats statsSnapshot
		if err := json.Unmarshal(snapshot.StatsJSON, &stats); err != nil {
			return nil, err
		}
		if stats.SettlementTransition != nil {
			if stats.SettlementTransition.ToEpoch != transaction.Epoch || stats.SettlementTransition.Identity.NoID != snapshot.NoID {
				return nil, errors.New("settlement closure differs from transaction ownership")
			}
			closure.Transitions = append(closure.Transitions, stats.SettlementTransition)
		}
	}
	if len(closure.Transitions) == 0 {
		return nil, nil
	}
	if len(closure.Transitions) != len(transaction.Snapshots) || transaction.Epoch == 0 {
		return nil, errors.New("settlement closure omits a transaction participant")
	}
	closure.Epoch = transaction.Epoch - 1
	if err := VerifyAttemptSettlementBatch(closure.Transitions); err != nil {
		return nil, err
	}
	return closure, nil
}

// Opens an anchored private directory and rejects symlinked ancestry. Root's
// descriptor-relative operations retain confinement after the path is opened.
func openAttemptSettlementClosureDirectory(coordinatorStateDir string, create bool) (*os.Root, error) {
	return openAttemptSettlementClosureDirectoryWithSync(coordinatorStateDir, create, func(directory *os.File) error { return directory.Sync() })
}

// Injects only durability operations; tests retain real descriptor confinement.
func openAttemptSettlementClosureDirectoryWithSync(coordinatorStateDir string, create bool, syncDirectory func(*os.File) error) (*os.Root, error) {
	if syncDirectory == nil {
		return nil, errors.New("settlement closure directory sync is nil")
	}
	if !filepath.IsAbs(coordinatorStateDir) || filepath.Clean(coordinatorStateDir) != coordinatorStateDir {
		return nil, errors.New("settlement closure state path is not absolute and clean")
	}
	resolved, err := filepath.EvalSymlinks(coordinatorStateDir)
	if err != nil || resolved != coordinatorStateDir {
		return nil, errors.New("settlement closure state path contains a symlink or is unavailable")
	}
	root, err := os.OpenRoot(coordinatorStateDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	parent, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	parentInfo, err := parent.Stat()
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("settlement closure state directory is not private")
	}
	if create {
		if err := root.Mkdir("settlement-closures", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		// Persist the directory entry before a journal can be removed. This
		// also completes an interrupted first mkdir on an idempotent retry.
		if err := syncDirectory(parent); err != nil {
			return nil, err
		}
	}
	info, err := root.Lstat("settlement-closures")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("settlement closure directory is not private and unlinked")
	}
	child, err := root.OpenRoot("settlement-closures")
	if err != nil {
		return nil, err
	}
	opened, err := child.Open(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	openedInfo, statErr := opened.Stat()
	closeErr := opened.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(info, openedInfo) {
		child.Close()
		return nil, errors.New("settlement closure directory identity changed during open")
	}
	return child, nil
}

// Owns one immutable regular-file snapshot and rejects hardlink aliases.
func readAttemptSettlementClosure(root *os.Root, name string) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	state, ok := attemptSettlementClosureFileMetadata(before)
	if !before.Mode().IsRegular() || before.Mode().Perm()&0o077 != 0 || !ok || state.links != 1 {
		return nil, errors.New("settlement closure is not a private single-link regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, errors.New("settlement closure file identity changed during open")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return nil, errors.New("settlement closure changed during read")
	}
	afterState, ok := attemptSettlementClosureFileMetadata(after)
	if !ok || afterState != state {
		return nil, errors.New("settlement closure link or write state changed during read")
	}
	return data, nil
}

// Loads producer-owned bytes; callers still authenticate their expected domain,
// validator key and configured operator census after decoding the signed batch.
func ReadAttemptSettlementClosure(coordinatorStateDir string, epoch uint64) ([]byte, error) {
	root, err := openAttemptSettlementClosureDirectory(coordinatorStateDir, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readAttemptSettlementClosure(root, strconv.FormatUint(epoch, 10)+".json")
}

// Atomically publishes without replacing any prior pathname. The no-replace
// rename also avoids exposing a partial file or a transient hardlink alias.
func publishAttemptSettlementClosure(coordinatorStateDir string, transaction *attemptSettlementTransaction) error {
	return publishAttemptSettlementClosureWithSync(coordinatorStateDir, transaction, func(directory *os.File) error { return directory.Sync() })
}

// The parent entry is durable before publication, and the child rename before
// journal removal. The hook exists solely to force either crash boundary.
func publishAttemptSettlementClosureWithSync(coordinatorStateDir string, transaction *attemptSettlementTransaction, syncDirectory func(*os.File) error) error {
	if syncDirectory == nil {
		return errors.New("settlement closure directory sync is nil")
	}
	closure, err := attemptSettlementClosureFromTransaction(transaction)
	if err != nil || closure == nil {
		return err
	}
	encoded, err := json.Marshal(closure)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	root, err := openAttemptSettlementClosureDirectoryWithSync(coordinatorStateDir, true, syncDirectory)
	if err != nil {
		return err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	name := strconv.FormatUint(closure.Epoch, 10) + ".json"
	if prior, err := readAttemptSettlementClosure(root, name); err == nil {
		if !bytes.Equal(prior, encoded) {
			return errors.New("settlement closure already exists with different bytes")
		}
		// A prior rename may have succeeded before its directory sync failed.
		// Retry that durability boundary before permitting journal removal.
		return syncDirectory(directory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".closure-" + hex.EncodeToString(nonce[:])
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	_, writeErr := file.Write(encoded)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := renameAttemptSettlementClosure(int(directory.Fd()), temporary, name); err != nil {
		return fmt.Errorf("publish immutable settlement closure: %w", err)
	}
	return syncDirectory(directory)
}
