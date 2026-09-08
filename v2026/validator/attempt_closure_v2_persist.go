//go:build linux || darwin

package validator

// Compact metadata stays in retained native directories through callbacks.
// Every close and final durability error prevents successful journal release.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"golang.org/x/sys/unix"
)

// Hooks replace only physical boundaries, never authentication. Actual writes
// receive retained directories rather than reopening callback-mutable paths.
type attemptSettlementV2IO struct {
	writeSnapshot func(*attemptPrivateDirectory, string, []byte) error
	writeJournal  func(*attemptPrivateDirectory, string, []byte) error
	syncDirectory func(*os.File) error
	closeFile     func(*os.File) error
	closeRoot     func(*attemptPrivateDirectory) error
	removeJournal func(*attemptPrivateDirectory, string) error
	step          func(string) error
	roots         map[string]*attemptPrivateDirectory
	ctx           context.Context
	witnesses     *attemptSettlementV2WitnessRegistry
}

// No bound or permission default is invented by physical persistence.
func attemptSettlementV2PhysicalIO() attemptSettlementV2IO {
	return attemptSettlementV2IO{writeSnapshot: writeAttemptSettlementV2OwnedState, writeJournal: writeAttemptSettlementV2OwnedState, syncDirectory: func(file *os.File) error { return file.Sync() }, closeFile: func(file *os.File) error { return file.Close() }, closeRoot: func(root *attemptPrivateDirectory) error { return root.close() }, removeJournal: removeAttemptSettlementV2OwnedTransaction}
}

// All declared physical operations are required, including late closes.
func (self attemptSettlementV2IO) validate() error {
	if self.writeSnapshot == nil || self.writeJournal == nil || self.syncDirectory == nil || self.closeFile == nil || self.closeRoot == nil || self.removeJournal == nil {
		return errors.New("compact settlement durability operations are incomplete")
	}
	return nil
}

// Native directory admission also pins private ownership before any write.
func openAttemptSettlementV2Root(path string) (*attemptPrivateDirectory, error) {
	root, err := openAttemptPrivateDirectory(path)
	if err != nil {
		return nil, err
	}
	if root.anchor.mode&0o077 != 0 || root.anchor.uid != uint32(os.Geteuid()) {
		return nil, errors.Join(errors.New("compact settlement state directory is not private and owned"), root.close())
	}
	if err := root.check(); err != nil {
		return nil, errors.Join(err, root.close())
	}
	return root, nil
}

// Acquire all durable owners before seal/replay/write callbacks. The operation
// joins every retained descriptor, including partial-acquisition failure.
func retainAttemptSettlementV2Roots(coordinator string, participants []AttemptSettlementRuntimeV2Participant, physical attemptSettlementV2IO) (attemptSettlementV2IO, error) {
	physical = physical.withWitnesses(physical.context())
	physical.roots = map[string]*attemptPrivateDirectory{}
	paths := []string{coordinator}
	for _, participant := range participants {
		paths = append(paths, participant.StateDir)
	}
	for _, path := range paths {
		root, err := openAttemptSettlementV2Root(path)
		if err != nil {
			return physical, errors.Join(err, physical.closeRoots())
		}
		physical.roots[path] = root
		if err := physical.retainWitness(root); err != nil {
			return physical, errors.Join(err, physical.closeRoots())
		}
	}
	// Explicit legacy recovery/migration must finish before this independent
	// coordinator can activate, advance or attach any compact participant.
	if _, err := physical.roots[coordinator].stat("settlement-transaction.json"); err == nil {
		return physical, errors.Join(errors.New("compact runtime cannot bypass a pending legacy settlement journal"), physical.closeRoots())
	} else if !errors.Is(err, os.ErrNotExist) {
		return physical, errors.Join(err, physical.closeRoots())
	}
	if err := physical.witnessLeaf(physical.roots[coordinator], "settlement-transaction.json", attemptSettlementV2LeafWitness{}); err != nil {
		return physical, errors.Join(err, physical.closeRoots())
	}
	for _, participant := range participants {
		if participant.Ledger.directory == nil {
			return physical, errors.Join(errors.New("compact participant lost its physical directory owner"), physical.closeRoots())
		}
		opened, err := physical.roots[participant.StateDir].file.Stat()
		if err != nil || !os.SameFile(opened, participant.Ledger.directory.anchor) {
			return physical, errors.Join(errors.New("compact participant directory differs from retained ledger"), err, physical.closeRoots())
		}
	}
	return physical, nil
}

// No early return abandons later cleanup after an earlier close fails.
func (self attemptSettlementV2IO) closeRoots() error {
	var resultErr error
	paths := make([]string, 0, len(self.roots))
	for path := range self.roots {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for index := len(paths) - 1; index >= 0; index-- {
		path := paths[index]
		resultErr = errors.Join(resultErr, self.closeOwnedRoot(self.roots[path]))
		delete(self.roots, path)
	}
	if self.witnesses != nil {
		for _, root := range self.witnesses.owners {
			resultErr = errors.Join(resultErr, self.closeOwnedRoot(root))
		}
		resultErr = errors.Join(resultErr, self.witnesses.closeErr)
	}
	return errors.Join(resultErr, self.finishWitnesses())
}

// Namespace checks surround external boundaries, never state mutexes.
func (self attemptSettlementV2IO) checkRoots() error {
	var resultErr error
	for _, root := range self.roots {
		resultErr = errors.Join(resultErr, root.check())
	}
	return resultErr
}

// Individual reads cannot close a borrowed transaction owner.
func (self attemptSettlementV2IO) openRoot(path string) (*attemptPrivateDirectory, func() error, error) {
	if root := self.roots[path]; root != nil {
		if err := root.check(); err != nil {
			return nil, nil, err
		}
		return root, func() error { return nil }, nil
	}
	root, err := openAttemptSettlementV2Root(path)
	if err != nil {
		return nil, nil, err
	}
	if err := self.retainWitness(root); err != nil {
		return nil, nil, errors.Join(err, self.closeOwnedRoot(root))
	}
	return root, func() error { return self.closeOwnedRoot(root) }, nil
}

// Mutable files retain atomic replacement without pathname-selected redirects.
func writeAttemptSettlementV2OwnedState(root *attemptPrivateDirectory, name string, data []byte) (resultErr error) {
	if root == nil || name != "stats.json" && name != "settlement-transaction-v2.json" {
		return errors.New("compact mutable state target is not owned metadata")
	}
	if err := root.check(); err != nil {
		return err
	}
	before, err := root.stat(name)
	if err == nil {
		if !before.regular() || before.mode&0o077 != 0 || before.links != 1 || before.uid != uint32(os.Geteuid()) {
			return errors.New("compact mutable state target is aliased or not private")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".state-" + hex.EncodeToString(nonce[:])
	file, err := root.openFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := unix.Unlinkat(int(root.file.Fd()), temporary, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	written, writeErr := file.Write(data)
	if written != len(data) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := root.check(); err != nil {
		return err
	}
	if err := unix.Renameat(int(root.file.Fd()), temporary, int(root.file.Fd()), name); err != nil {
		return &os.LinkError{Op: "renameat", Old: temporary, New: name, Err: err}
	}
	return errors.Join(root.file.Sync(), root.check())
}

// Standalone setup callers get the same writer and joined directory close.
func writeAttemptSettlementV2State(path string, data []byte) (resultErr error) {
	if err := validateAttemptSettlementV2Paths([]string{filepath.Dir(path)}); err != nil {
		return err
	}
	physical := attemptSettlementV2PhysicalIO().withWitnesses(context.Background())
	root, release, err := physical.openRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release(), physical.closeRoots()) }()
	// Setup supplies already encoded bytes, not a runtime persistence allowance.
	return physical.writeImage(root, filepath.Base(path), data, uint64(len(data)), physical.writeSnapshot)
}

// V2 never consumes or overwrites the v1 immutable export namespace.
func AttemptSettlementClosureV2Path(coordinator string, epoch uint64) string {
	return filepath.Join(coordinator, "settlement-closures-v2", strconv.FormatUint(epoch, 10)+".json")
}

// Native child acquisition cannot follow or block on a replaced directory.
func openAttemptSettlementV2ClosureDirectory(coordinator string, create bool, physical attemptSettlementV2IO) (result *attemptPrivateDirectory, resultErr error) {
	return openAttemptSettlementOwnedClosureDirectory(coordinator, "settlement-closures-v2", create, physical)
}

// The only alternate child is the historical v1 closure namespace, used by
// retained-owner legacy recovery without changing its signed byte contract.
func openAttemptSettlementOwnedClosureDirectory(coordinator, name string, create bool, physical attemptSettlementV2IO) (result *attemptPrivateDirectory, resultErr error) {
	if name != "settlement-closures-v2" && name != "settlement-closures" {
		return nil, errors.New("settlement closure namespace is not owned")
	}
	if name == "settlement-closures-v2" {
		if err := validateAttemptSettlementV2Paths([]string{coordinator}); err != nil {
			return nil, err
		}
	}
	parent, release, err := physical.openRoot(coordinator)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, release())
		if resultErr != nil && result != nil {
			resultErr = errors.Join(resultErr, physical.closeOwnedRoot(result))
			result = nil
		}
	}()
	if create {
		if err := unix.Mkdirat(int(parent.file.Fd()), name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := physical.syncDirectory(parent.file); err != nil {
			return nil, err
		}
	}
	before, err := parent.stat(name)
	if err != nil || !before.directory() || before.mode&0o077 != 0 || before.uid != uint32(os.Geteuid()) {
		return nil, errors.Join(errors.New("compact closure directory is not private and owned"), err)
	}
	if physical.step != nil {
		if err := physical.step("before-closure-directory-open"); err != nil {
			return nil, err
		}
	}
	file, err := parent.openFile(name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	opened, err := statAttemptPrivateFile(file)
	if err != nil || before.dev != opened.dev || before.ino != opened.ino || before.mode != opened.mode || before.uid != opened.uid {
		return nil, errors.Join(errors.New("compact closure directory changed during open"), err, physical.closeFile(file))
	}
	child := &attemptPrivateDirectory{file: file, path: filepath.Join(parent.path, name), anchor: opened}
	if err := errors.Join(physical.retainWitness(child), parent.check(), child.check()); err != nil {
		return nil, errors.Join(err, physical.closeOwnedRoot(child))
	}
	return child, nil
}

// Late ENOENT cannot grant creation after an occupied entry was observed.
func readAttemptSettlementV2File(root *attemptPrivateDirectory, name string, limit uint64, physical attemptSettlementV2IO) ([]byte, bool, error) {
	data, exists, expected, err := readAttemptSettlementV2FileState(root, name, limit, physical)
	if err != nil {
		return nil, exists, err
	}
	if err := physical.witnessLeaf(root, name, expected); err != nil {
		return nil, exists, err
	}
	return data, exists, nil
}

// Stable bounded bytes and metadata do not themselves authorize replacing a
// retained witness. Writers must first prove their admitted exact postimage.
func readAttemptSettlementV2FileState(root *attemptPrivateDirectory, name string, limit uint64, physical attemptSettlementV2IO) ([]byte, bool, attemptSettlementV2LeafWitness, error) {
	if err := validateAttemptSettlementV2MetadataLimit(limit); err != nil {
		return nil, false, attemptSettlementV2LeafWitness{}, err
	}
	if root == nil || !validAttemptPrivateLeaf(name) {
		return nil, false, attemptSettlementV2LeafWitness{}, errors.New("compact metadata read ownership is invalid")
	}
	before, statErr := root.stat(name)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, false, attemptSettlementV2LeafWitness{}, statErr
	}
	expected := attemptSettlementV2LeafWitness{state: before, exists: statErr == nil}
	data, exists, err := readAttemptPrivateMetadata(physical.context(), root, name, limit, attemptPrivateMetadataReadIO{closeFile: physical.closeFile, step: physical.step})
	if err != nil {
		return nil, exists, attemptSettlementV2LeafWitness{}, err
	}
	after, afterErr := root.stat(name)
	if exists != expected.exists || exists && (afterErr != nil || after != before) || !exists && !errors.Is(afterErr, os.ErrNotExist) {
		return nil, exists, attemptSettlementV2LeafWitness{}, errors.Join(errors.New("compact metadata witness changed around read"), afterErr)
	}
	return data, exists, expected, nil
}

// Existing exact bytes are resynced; new bytes use native no-replace rename.
func publishAttemptSettlementClosureV2(coordinator string, epoch uint64, encoded []byte, maxBytes uint64, physical attemptSettlementV2IO) (resultErr error) {
	return publishAttemptSettlementOwnedClosure(coordinator, "settlement-closures-v2", epoch, encoded, maxBytes, physical)
}

// Both fixed namespaces share native publication and close semantics only;
// the caller still supplies and verifies its distinct schema and authority.
func publishAttemptSettlementOwnedClosure(coordinator, namespace string, epoch uint64, encoded []byte, maxBytes uint64, physical attemptSettlementV2IO) (resultErr error) {
	if err := validateAttemptSettlementV2MetadataLimit(maxBytes); err != nil {
		return err
	}
	if uint64(len(encoded)) > maxBytes {
		return errors.New("compact settlement closure exceeds its existing byte bound")
	}
	if physical.witnesses == nil && physical.roots == nil {
		physical = physical.withWitnesses(physical.context())
		defer func() { resultErr = errors.Join(resultErr, physical.closeRoots()) }()
	}
	root, err := openAttemptSettlementOwnedClosureDirectory(coordinator, namespace, true, physical)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, physical.closeOwnedRoot(root)) }()
	name := strconv.FormatUint(epoch, 10) + ".json"
	prior, exists, err := readAttemptSettlementV2File(root, name, maxBytes, physical)
	if err != nil {
		return err
	}
	if exists {
		if !bytes.Equal(prior, encoded) {
			return errors.New("compact immutable settlement closure differs from the transaction")
		}
		return errors.Join(physical.syncDirectory(root.file), root.check())
	}
	transition, err := physical.admitLeafTransition(root, name)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".closure-" + hex.EncodeToString(nonce[:])
	file, err := root.openFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return err
	}
	defer func() {
		if err := unix.Unlinkat(int(root.file.Fd()), temporary, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	written, writeErr := file.Write(encoded)
	if written != len(encoded) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), physical.closeFile(file)); err != nil {
		return err
	}
	if err := root.check(); err != nil {
		return err
	}
	if err := renameAttemptSettlementClosure(int(root.file.Fd()), temporary, name); err != nil {
		return fmt.Errorf("publish immutable compact settlement closure: %w", err)
	}
	if err := errors.Join(physical.syncDirectory(root.file), root.check()); err != nil {
		return err
	}
	actual, exists, expected, err := readAttemptSettlementV2FileState(root, name, maxBytes, physical)
	if err != nil || !exists || !bytes.Equal(actual, encoded) {
		return errors.Join(errors.New("compact published closure differs from its exact image"), err)
	}
	return physical.commitLeafTransition(root, transition, expected)
}

// Missing journal is idempotent, but parent sync remains mandatory.
func removeAttemptSettlementV2OwnedTransaction(root *attemptPrivateDirectory, name string) error {
	if root == nil || name != "settlement-transaction-v2.json" {
		return errors.New("compact transaction removal name is invalid")
	}
	if err := root.check(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(root.file.Fd()), name, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return errors.Join(root.file.Sync(), root.check())
}

// Standalone setup also joins its physical directory owner.
func removeAttemptSettlementTransactionV2(path string) (resultErr error) {
	if err := validateAttemptSettlementV2Paths([]string{filepath.Dir(path)}); err != nil {
		return err
	}
	physical := attemptSettlementV2PhysicalIO().withWitnesses(context.Background())
	root, release, err := physical.openRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release(), physical.closeRoots()) }()
	return physical.removeOwnedJournal(root, filepath.Base(path))
}

// Full independent replay precedes a successful public closure result.
func ReadAttemptSettlementClosureV2(ctx context.Context, coordinator string, epoch uint64, authority AttemptSettlementV2Options) (closure *AttemptSettlementClosureV2, verified VerifiedAttemptSettlementV2, resultErr error) {
	return readAttemptSettlementClosureV2(ctx, coordinator, epoch, authority, attemptSettlementV2PhysicalIO())
}

// Physical seams still perform the real read and independent complete replay.
func readAttemptSettlementClosureV2(ctx context.Context, coordinator string, epoch uint64, authority AttemptSettlementV2Options, physical attemptSettlementV2IO) (closure *AttemptSettlementClosureV2, verified VerifiedAttemptSettlementV2, resultErr error) {
	if ctx == nil {
		return nil, verified, errors.New("compact settlement read context is nil")
	}
	if err := validateAttemptSettlementV2Paths([]string{coordinator}); err != nil {
		return nil, verified, err
	}
	for _, operator := range authority.Operators {
		if err := validateAttemptSettlementV2Paths([]string{operator.Measurement.Replay.ScratchDirectory}); err != nil {
			return nil, verified, err
		}
	}
	if err := physical.validate(); err != nil {
		return nil, verified, err
	}
	if err := validateAttemptSettlementV2MetadataLimit(authority.MaxClosureBytes); err != nil {
		return nil, verified, err
	}
	owned, err := ownAttemptSettlementV2Options(ctx, authority)
	if err != nil {
		return nil, verified, err
	}
	physical = physical.withWitnesses(ctx)
	defer func() {
		resultErr = errors.Join(resultErr, physical.closeRoots(), ctx.Err())
		if resultErr != nil {
			closure, verified = nil, VerifiedAttemptSettlementV2{}
		}
	}()
	root, err := openAttemptSettlementV2ClosureDirectory(coordinator, false, physical)
	if err != nil {
		return nil, verified, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.check(), physical.closeOwnedRoot(root)) }()
	encoded, exists, err := readAttemptSettlementV2File(root, strconv.FormatUint(epoch, 10)+".json", owned.MaxClosureBytes, physical)
	if err != nil || !exists {
		return nil, verified, errors.Join(errors.New("compact immutable closure is missing or unreadable"), err)
	}
	closure, verified, err = DecodeAttemptSettlementClosureV2(ctx, encoded, owned)
	if err != nil || closure.Epoch != epoch {
		return nil, VerifiedAttemptSettlementV2{}, errors.Join(errors.New("compact closure epoch or replay differs"), err)
	}
	return closure, verified, nil
}
