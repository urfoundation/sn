//go:build linux || darwin

package validator

// Startup owns native directory/file custody until all closes and final
// namespace witnesses complete. Loading never creates or rewrites state.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Observers expose real local boundaries, never replacement file contents or
// acceptance results. Close observers run after the actual owned Close.
type headEMAStoreV2LoadHooks struct {
	step           func(string, *os.File) error
	afterClose     func(*os.File) error
	beforeRational func() error
}

// No callback executes while a Stats or EMA state mutex is held.
func (self headEMAStoreV2LoadHooks) observe(operation string, file *os.File) error {
	if self.step != nil {
		return self.step(operation, file)
	}
	return nil
}

// The existing legacy loader is unchanged. This opt-in loader requires an
// already provisioned private physical directory and explicit trusted limits.
// Bounded startup is not activation, a bounded commit API or history authority.
func NewHeadEMAStoreV2(ctx context.Context, stateDir string, limits HeadEMAStoreV2Limits) (*HeadEMAStore, error) {
	return newHeadEMAStoreV2(ctx, stateDir, limits, headEMAStoreV2LoadHooks{})
}

// Admission errors and cancellation cannot publish a partially decoded store.
func newHeadEMAStoreV2(ctx context.Context, stateDir string, limits HeadEMAStoreV2Limits, hooks headEMAStoreV2LoadHooks) (result *HeadEMAStore, resultErr error) {
	if ctx == nil {
		return nil, errors.New("bounded head EMA context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || filepath.Dir(stateDir) == stateDir {
		return nil, errors.New("bounded head EMA requires a canonical absolute non-root physical state directory")
	}
	path := filepath.Join(stateDir, "head-ema.json")
	minimum := uint64(len(path)) + headEMAStoreV2FixedControlBytes()
	if minimum > limits.MaxControlBytes {
		return nil, errors.New("bounded head EMA owner exceeds its control allowance")
	}
	encoded, missing, err := readHeadEMAStoreV2(ctx, stateDir, limits.MaxFileBytes, hooks)
	if err != nil {
		return nil, err
	}
	if missing {
		return &HeadEMAStore{path: path, values: map[string]headEMAEntry{}}, nil
	}
	return decodeHeadEMAStoreV2(ctx, path, encoded, limits, hooks.beforeRational)
}

// Exact leaf state includes full-width modification/change timestamps. Clean
// initial absence remains absence through the last directory-close observer.
func checkHeadEMAStoreV2Witness(directory *attemptPrivateDirectory, before *attemptPrivateFileState, missing bool) error {
	if err := directory.check(); err != nil {
		return err
	}
	if before == nil && !missing {
		return nil
	}
	current, err := directory.stat("head-ema.json")
	if missing {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.Join(errors.New("bounded head EMA initial absence changed during acquisition"), err)
	}
	if err != nil || current != *before {
		return errors.Join(errors.New("bounded head EMA leaf changed during acquisition"), err)
	}
	return nil
}

// A nonblocking no-follow open precedes fstat, so replacing an observed file
// with a FIFO cannot make the type check wait for a peer. Every acquired handle
// is closed even if the caller cancels or a real boundary observer fails.
func readHeadEMAStoreV2(ctx context.Context, stateDir string, limit uint64, hooks headEMAStoreV2LoadHooks) (encoded []byte, missing bool, resultErr error) {
	if ctx == nil {
		return nil, false, errors.New("bounded head EMA context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if limit == 0 || limit > maxHeadEMAStoreV2Bytes {
		return nil, false, errors.New("bounded head EMA file allowance is invalid")
	}
	directory, err := openAttemptPrivateDirectory(stateDir)
	if err != nil {
		return nil, false, fmt.Errorf("bounded head EMA requires a provisioned physical path without symlinked ancestors: %w", err)
	}
	var file *os.File
	var before *attemptPrivateFileState
	defer func() {
		if file != nil {
			resultErr = errors.Join(resultErr, file.Close())
			if hooks.afterClose != nil {
				resultErr = errors.Join(resultErr, hooks.afterClose(file))
			}
		}
		resultErr = errors.Join(resultErr, checkHeadEMAStoreV2Witness(directory, before, missing))
		ownedFile, anchor := directory.file, directory.anchor
		resultErr = errors.Join(resultErr, directory.close())
		if hooks.afterClose != nil {
			resultErr = errors.Join(resultErr, hooks.afterClose(ownedFile))
		}
		// This final witness has no observer recursion. It validates changes
		// caused by either owned real Close boundary before returning bytes.
		witness, witnessErr := openAttemptPrivateDirectory(stateDir)
		if witnessErr == nil {
			if witness.anchor.dev != anchor.dev || witness.anchor.ino != anchor.ino || witness.anchor.mode != anchor.mode || witness.anchor.uid != anchor.uid {
				witnessErr = errors.New("bounded head EMA directory changed after Close")
			} else {
				witnessErr = checkHeadEMAStoreV2Witness(witness, before, missing)
			}
			witnessErr = errors.Join(witnessErr, witness.close())
		}
		resultErr = errors.Join(resultErr, witnessErr, ctx.Err())
		if resultErr != nil {
			encoded, missing = nil, false
		}
	}()
	if directory.anchor.mode&0o077 != 0 || directory.anchor.uid != uint32(os.Geteuid()) {
		return nil, false, errors.New("bounded head EMA state directory is not private to its owner")
	}
	if err := hooks.observe("directory-opened", directory.file); err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	state, err := directory.stat("head-ema.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	before = &state
	if !state.regular() || state.mode&0o077 != 0 || state.uid != uint32(os.Geteuid()) || state.size < 0 || uint64(state.size) > limit {
		return nil, false, errors.New("bounded head EMA file is not a bounded private regular file")
	}
	if err := hooks.observe("leaf-observed", nil); err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := directory.check(); err != nil {
		return nil, false, err
	}
	file, err = directory.openFile("head-ema.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, false, err
	}
	if err := hooks.observe("leaf-opened", file); err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	opened, err := statAttemptPrivateFile(file)
	if err != nil || opened != state {
		return nil, false, errors.Join(errors.New("bounded head EMA file changed before read"), err)
	}
	encoded, err = io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	if err := hooks.observe("leaf-read", file); err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	after, err := statAttemptPrivateFile(file)
	if err != nil || after != state || uint64(len(encoded)) > limit || int64(len(encoded)) != state.size {
		return nil, false, errors.Join(errors.New("bounded head EMA file changed during bounded read"), err)
	}
	return encoded, false, nil
}
