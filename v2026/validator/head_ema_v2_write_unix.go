//go:build linux || darwin

package validator

// V2 publication is descriptor-owned and conservatively fail-closed. An
// exclusive durable marker precedes every possible head publication. Exchange
// retains the actual displaced predecessor for verification; it is not an
// inode-conditional rename. Any uncertain phase keeps evidence and faults the
// store. Existing-marker refusal is not automatic crash reconciliation.

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const headEMAStoreV2Marker = ".head-ema-v2-commit"
const headEMAStoreV2Candidate = ".head-ema-v2-next"

// No loader may silently adopt an unresolved publication or competing writer.
// Occupied names include symlinks and non-regular files; no marker is followed.
func requireHeadEMAStoreV2NoMarker(directory *attemptPrivateDirectory) error {
	_, err := directory.stat(headEMAStoreV2Marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return errors.Join(errors.New("bounded head EMA has unresolved write evidence; explicit investigation is required"), err)
}

// A reopening may confirm the loader's physical owner, never replace it.
func sameHeadEMAStoreV2Directory(left, right attemptPrivateFileState) bool {
	return left.dev == right.dev && left.ino == right.ino && left.mode == right.mode && left.uid == right.uid
}

// Writes may change size/timestamps, never descriptor identity or permissions.
func sameHeadEMAStoreV2FileOwner(left, right attemptPrivateFileState) bool {
	return left.dev == right.dev && left.ino == right.ino && left.mode == right.mode && left.uid == right.uid && left.links == right.links
}

// Native rename legitimately changes ctime. Every other observed field and
// an independently reread exact content digest must still match the owner.
func sameHeadEMAStoreV2RenamedFile(left, right attemptPrivateFileState) bool {
	left.changeSeconds, left.changeNanoseconds = right.changeSeconds, right.changeNanoseconds
	return left == right
}

// Only the originally admitted physical directory may be opened for work.
func openHeadEMAStoreV2Directory(path string, namespace headEMAStoreV2Namespace) (*attemptPrivateDirectory, error) {
	directory, err := openAttemptPrivateDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !sameHeadEMAStoreV2Directory(directory.anchor, namespace.directory) {
		return nil, errors.Join(errors.New("bounded head EMA directory differs from its loaded owner"), directory.close())
	}
	return directory, nil
}

// Expected absence is allowed only when the loader originally witnessed it.
func checkHeadEMAStoreV2Leaf(directory *attemptPrivateDirectory, namespace headEMAStoreV2Namespace) error {
	if err := directory.check(); err != nil {
		return err
	}
	current, err := directory.stat("head-ema.json")
	if namespace.missing {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.Join(errors.New("bounded head EMA loaded absence changed"), err)
	}
	if err != nil || current != namespace.leaf {
		return errors.Join(errors.New("bounded head EMA loaded leaf changed"), err)
	}
	return nil
}

// Observers run only after the actual owned Close, and their causes are joined.
func closeHeadEMAStoreV2File(file **os.File, hooks headEMAStoreV2RuntimeHooks) error {
	if *file == nil {
		return nil
	}
	owned := *file
	*file = nil
	err := owned.Close()
	if hooks.afterClose != nil {
		err = errors.Join(err, hooks.afterClose(owned))
	}
	return err
}

// Read-only previews/idempotent commits validate both the current owner and
// the namespace after the real directory Close; they never create a marker.
func witnessHeadEMAStoreV2Runtime(ctx context.Context, path string, namespace headEMAStoreV2Namespace, hooks headEMAStoreV2RuntimeHooks) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := openHeadEMAStoreV2Directory(path, namespace)
	if err != nil {
		return err
	}
	defer func() {
		owned := directory.file
		resultErr = errors.Join(resultErr, directory.close())
		if hooks.afterClose != nil {
			resultErr = errors.Join(resultErr, hooks.afterClose(owned))
		}
		witness, err := openHeadEMAStoreV2Directory(path, namespace)
		if err == nil {
			err = errors.Join(checkHeadEMAStoreV2Leaf(witness, namespace), requireHeadEMAStoreV2NoMarker(witness), witness.close())
		}
		resultErr = errors.Join(resultErr, err, ctx.Err())
	}()
	if err := hooks.observe(ctx, "directory-opened", directory.file); err != nil {
		return err
	}
	return errors.Join(checkHeadEMAStoreV2Leaf(directory, namespace), requireHeadEMAStoreV2NoMarker(directory), ctx.Err())
}

// Digest a retained regular descriptor with a fixed admitted buffer, exact
// length and native state checks. No ReadAll allocation or pathname open occurs.
func hashHeadEMAStoreV2File(ctx context.Context, file *os.File, expected attemptPrivateFileState, limit uint64) ([32]byte, error) {
	var zero [32]byte
	if expected.size < 0 || uint64(expected.size) > limit || !expected.regular() {
		return zero, errors.New("bounded head EMA hash input exceeds its file allowance")
	}
	before, err := statAttemptPrivateFile(file)
	if err != nil || before != expected {
		return zero, errors.Join(errors.New("bounded head EMA hash descriptor changed"), err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return zero, err
	}
	hasher := sha256.New()
	var buffer [4096]byte
	var consumed int64
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		remaining := expected.size - consumed
		width := len(buffer)
		if remaining < int64(width) {
			width = int(remaining) + 1
		}
		count, readErr := file.Read(buffer[:width])
		consumed += int64(count)
		if consumed > expected.size {
			return zero, errors.New("bounded head EMA hash input grew")
		}
		if count != 0 {
			_, _ = hasher.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return zero, readErr
		}
		if count == 0 {
			return zero, io.ErrNoProgress
		}
	}
	after, err := statAttemptPrivateFile(file)
	if err != nil || after != expected || consumed != expected.size {
		return zero, errors.Join(errors.New("bounded head EMA hash input changed"), err)
	}
	var digest [32]byte
	copy(digest[:], hasher.Sum(digest[:0]))
	return digest, ctx.Err()
}

// A fixed private marker is acquired exclusively, never overwritten, normalized
// or interpreted as permission. It is synced before a candidate can publish.
func createHeadEMAStoreV2Marker(directory *attemptPrivateDirectory) (*os.File, attemptPrivateFileState, error) {
	file, err := directory.openFile(headEMAStoreV2Marker, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return nil, attemptPrivateFileState{}, err
	}
	state, err := statAttemptPrivateFile(file)
	if err != nil || !state.regular() || state.mode&0o777 != 0o600 || state.links != 1 || state.uid != uint32(os.Geteuid()) || state.size != 0 {
		return nil, attemptPrivateFileState{}, errors.Join(errors.New("bounded head EMA marker ownership differs"), err, file.Close())
	}
	return file, state, nil
}

// Only a still-exact owned name may be removed. Native unlink remains a
// namespace operation, not a compare-and-swap; a conflicting private actor
// causes the following witness to fail and the owner to retain a sticky fault.
func unlinkHeadEMAStoreV2Owned(directory *attemptPrivateDirectory, name string, expected attemptPrivateFileState) error {
	state, err := directory.stat(name)
	if err != nil || state != expected {
		return errors.Join(errors.New("bounded head EMA cleanup name changed"), err)
	}
	if err := unix.Unlinkat(int(directory.file.Fd()), name, 0); err != nil {
		return &os.PathError{Op: "unlinkat", Path: filepath.Join(directory.path, name), Err: err}
	}
	return nil
}

// After marker removal, a late cleanup failure recreates evidence exclusively
// when possible. Failure to restore is joined and never licenses this owner to
// continue; operators must retain both the original cause and restoration cause.
func restoreHeadEMAStoreV2Marker(path string, namespace headEMAStoreV2Namespace) (resultErr error) {
	directory, err := openHeadEMAStoreV2Directory(path, namespace)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.close()) }()
	file, _, err := createHeadEMAStoreV2Marker(directory)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close(), directory.file.Sync())
}

// The successful cleanup phase is deliberately unhooked: all externally
// observed real Close boundaries completed while the durable marker remained.
// A final native witness still checks committed head identity and marker absence.
func finishHeadEMAStoreV2Write(path string, namespace headEMAStoreV2Namespace, marker attemptPrivateFileState, displaced *attemptPrivateFileState) (resultErr error) {
	directory, err := openHeadEMAStoreV2Directory(path, namespace)
	if err != nil {
		return err
	}
	markerRemoved := false
	defer func() {
		resultErr = errors.Join(resultErr, directory.close())
		if resultErr != nil && markerRemoved {
			resultErr = errors.Join(resultErr, restoreHeadEMAStoreV2Marker(path, namespace))
		}
	}()
	if err := checkHeadEMAStoreV2Leaf(directory, namespace); err != nil {
		return err
	}
	if displaced != nil {
		if err := unlinkHeadEMAStoreV2Owned(directory, headEMAStoreV2Candidate, *displaced); err != nil {
			return err
		}
	}
	if err := directory.file.Sync(); err != nil {
		return err
	}
	if err := unlinkHeadEMAStoreV2Owned(directory, headEMAStoreV2Marker, marker); err != nil {
		return err
	}
	markerRemoved = true
	if err := directory.file.Sync(); err != nil {
		return err
	}
	if err := directory.close(); err != nil {
		return err
	}
	witness, err := openHeadEMAStoreV2Directory(path, namespace)
	if err != nil {
		return err
	}
	return errors.Join(checkHeadEMAStoreV2Leaf(witness, namespace), requireHeadEMAStoreV2NoMarker(witness), witness.close())
}

// All phases are native: marker -> Sync -> candidate/write/Sync/Close ->
// no-replace or exchange -> exact displaced/current verification -> directory
// Sync/Close/final observer witness -> unhooked cleanup/Sync/Close/final witness.
// A crash with retained marker is unresolved, never called successful recovery.
func writeHeadEMAStoreV2(ctx context.Context, path string, encoded []byte, prior headEMAStoreV2Namespace, limits HeadEMAStoreV2Limits, hooks headEMAStoreV2RuntimeHooks) (next headEMAStoreV2Namespace, uncertain bool, resultErr error) {
	if err := ctx.Err(); err != nil {
		return next, false, err
	}
	if uint64(len(encoded)) > limits.MaxFileBytes {
		return next, false, errors.New("bounded head EMA encoded output exceeds its file allowance")
	}
	directory, err := openHeadEMAStoreV2Directory(path, prior)
	if err != nil {
		return next, true, err
	}
	var markerFile, candidateFile, priorFile *os.File
	transactionStarted := false
	defer func() {
		resultErr = errors.Join(resultErr, closeHeadEMAStoreV2File(&candidateFile, hooks), closeHeadEMAStoreV2File(&markerFile, hooks), closeHeadEMAStoreV2File(&priorFile, hooks))
		if directory.file != nil {
			owned := directory.file
			resultErr = errors.Join(resultErr, directory.close())
			if hooks.afterClose != nil {
				resultErr = errors.Join(resultErr, hooks.afterClose(owned))
			}
		}
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			next = headEMAStoreV2Namespace{}
			uncertain = uncertain || transactionStarted
		}
	}()
	if err := hooks.observe(ctx, "directory-opened", directory.file); err != nil {
		return next, false, err
	}
	if err := checkHeadEMAStoreV2Leaf(directory, prior); err != nil {
		return next, true, err
	}
	if err := requireHeadEMAStoreV2NoMarker(directory); err != nil {
		return next, true, err
	}
	if !prior.missing {
		if err := hooks.observe(ctx, "predecessor-observed", nil); err != nil {
			return next, false, err
		}
		priorFile, err = directory.openFile("head-ema.json", unix.O_RDONLY, 0)
		if err != nil {
			return next, true, err
		}
		if err := hooks.observe(ctx, "predecessor-opened", priorFile); err != nil {
			return next, false, err
		}
		state, err := statAttemptPrivateFile(priorFile)
		if err != nil || state != prior.leaf {
			return next, true, errors.Join(errors.New("bounded head EMA predecessor changed before write"), err)
		}
	}
	markerFile, marker, err := createHeadEMAStoreV2Marker(directory)
	if err != nil {
		return next, true, err
	}
	// Once marker acquisition is possible, every failure preserves evidence.
	// Even prepublication cancellation is conservative unresolved state.
	uncertain = true
	transactionStarted = true
	if err := hooks.observe(ctx, "marker-created", markerFile); err != nil {
		return next, true, err
	}
	if err := markerFile.Sync(); err != nil {
		return next, true, err
	}
	if err := closeHeadEMAStoreV2File(&markerFile, hooks); err != nil {
		return next, true, err
	}
	if err := directory.file.Sync(); err != nil {
		return next, true, err
	}
	if err := hooks.observe(ctx, "marker-synced", directory.file); err != nil {
		return next, true, err
	}
	candidateFile, err = directory.openFile(headEMAStoreV2Candidate, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return next, true, err
	}
	if err := hooks.observe(ctx, "candidate-created", candidateFile); err != nil {
		return next, true, err
	}
	candidate, err := statAttemptPrivateFile(candidateFile)
	if err != nil || !candidate.regular() || candidate.mode&0o777 != 0o600 || candidate.uid != uint32(os.Geteuid()) || candidate.links != 1 || candidate.size != 0 {
		return next, true, errors.Join(errors.New("bounded head EMA candidate ownership differs"), err)
	}
	candidateOwner := candidate
	written, err := candidateFile.Write(encoded)
	if err != nil || written != len(encoded) {
		return next, true, errors.Join(io.ErrShortWrite, err)
	}
	if err := hooks.observe(ctx, "candidate-written", candidateFile); err != nil {
		return next, true, err
	}
	if err := candidateFile.Sync(); err != nil {
		return next, true, err
	}
	candidate, err = statAttemptPrivateFile(candidateFile)
	if err != nil || !sameHeadEMAStoreV2FileOwner(candidate, candidateOwner) || candidate.size != int64(len(encoded)) {
		return next, true, errors.Join(errors.New("bounded head EMA candidate write state differs"), err)
	}
	if err := hooks.observe(ctx, "candidate-synced", candidateFile); err != nil {
		return next, true, err
	}
	if err := closeHeadEMAStoreV2File(&candidateFile, hooks); err != nil {
		return next, true, err
	}
	if err := hooks.observe(ctx, "before-publish", directory.file); err != nil {
		return next, true, err
	}
	if err := checkHeadEMAStoreV2Leaf(directory, prior); err != nil {
		return next, true, err
	}
	currentMarker, markerErr := directory.stat(headEMAStoreV2Marker)
	currentCandidate, candidateErr := directory.stat(headEMAStoreV2Candidate)
	if markerErr != nil || candidateErr != nil || currentMarker != marker || currentCandidate != candidate {
		return next, true, errors.Join(errors.New("bounded head EMA publication owners changed"), markerErr, candidateErr)
	}
	// This observer forces the exact check-to-rename race in tests. Exchange
	// retains the actual predecessor; an ordinary Renameat would erase it.
	if err := hooks.observe(ctx, "publish-ready", directory.file); err != nil {
		return next, true, err
	}
	if err := publishHeadEMAStoreV2File(directory.file, headEMAStoreV2Candidate, "head-ema.json", !prior.missing); err != nil {
		return next, true, &os.LinkError{Op: "head-ema-publish", Old: filepath.Join(directory.path, headEMAStoreV2Candidate), New: path, Err: err}
	}
	if err := hooks.observe(ctx, "published", directory.file); err != nil {
		return next, true, err
	}
	published, err := directory.stat("head-ema.json")
	if err != nil || !sameHeadEMAStoreV2RenamedFile(published, candidate) {
		return next, true, errors.Join(errors.New("bounded head EMA published candidate differs"), err)
	}
	var displaced *attemptPrivateFileState
	if !prior.missing {
		state, err := directory.stat(headEMAStoreV2Candidate)
		if err != nil || !sameHeadEMAStoreV2RenamedFile(state, prior.leaf) {
			return next, true, errors.Join(errors.New("bounded head EMA exchanged predecessor differs from loaded authority"), err)
		}
		digest, err := hashHeadEMAStoreV2File(ctx, priorFile, state, limits.MaxFileBytes)
		if err != nil || digest != prior.digest {
			return next, true, errors.Join(errors.New("bounded head EMA exchanged predecessor bytes differ"), err)
		}
		displaced = &state
	}
	candidateFile, err = directory.openFile("head-ema.json", unix.O_RDONLY, 0)
	if err != nil {
		return next, true, err
	}
	digest, err := hashHeadEMAStoreV2File(ctx, candidateFile, published, limits.MaxFileBytes)
	if err != nil || digest != sha256.Sum256(encoded) {
		return next, true, errors.Join(errors.New("bounded head EMA published bytes differ"), err)
	}
	if err := closeHeadEMAStoreV2File(&candidateFile, hooks); err != nil {
		return next, true, err
	}
	if err := closeHeadEMAStoreV2File(&priorFile, hooks); err != nil {
		return next, true, err
	}
	next = headEMAStoreV2Namespace{directory: prior.directory, leaf: published, digest: digest}
	if err := checkHeadEMAStoreV2Leaf(directory, next); err != nil {
		return next, true, err
	}
	if displaced != nil {
		state, err := directory.stat(headEMAStoreV2Candidate)
		if err != nil || state != *displaced {
			return next, true, errors.Join(errors.New("bounded head EMA predecessor changed after Close"), err)
		}
	}
	if err := directory.file.Sync(); err != nil {
		return next, true, err
	}
	if err := hooks.observe(ctx, "directory-synced", directory.file); err != nil {
		return next, true, err
	}
	owned := directory.file
	if err := directory.close(); err != nil {
		return next, true, err
	}
	if hooks.afterClose != nil {
		if err := hooks.afterClose(owned); err != nil {
			return next, true, errors.Join(err, ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return next, true, err
	}
	// Cleanup reopens only the original physical authority and verifies the
	// exact post-publication head after all observer-controlled Close events.
	if err := finishHeadEMAStoreV2Write(path, next, marker, displaced); err != nil {
		return next, true, err
	}
	return next, false, nil
}
