package validator

// Reads one already-owned metadata namespace without turning a disappearing
// occupied entry into initial absence. Directory lifetime remains caller-owned.

import (
	"context"
	"errors"
	"io"
	"os"
)

// Hooks expose only physical custody boundaries; no byte or verifier result
// can be replaced. The caller joins its own root and parent-directory closes.
type attemptPrivateMetadataReadIO struct {
	closeFile func(*os.File) error
	step      func(string) error
}

// A zero limit preserves existing local metadata's compatibility bound. Even
// then, read at most the admitted file size plus one, refusing concurrent growth.
func readAttemptPrivateMetadata(ctx context.Context, root *attemptPrivateDirectory, name string, limit uint64, physical attemptPrivateMetadataReadIO) (data []byte, exists bool, resultErr error) {
	if ctx == nil || root == nil || physical.closeFile == nil || !validAttemptPrivateLeaf(name) || limit > uint64(^uint64(0)>>1)-1 {
		return nil, false, errors.New("private metadata read ownership or bound is invalid")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			data = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := root.check(); err != nil {
		return nil, false, err
	}
	before, err := root.stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, root.check()
	}
	if err != nil {
		return nil, false, err
	}
	exists = true
	if !before.regular() || before.mode&0o077 != 0 || before.links != 1 || before.uid != uint32(os.Geteuid()) || before.size < 0 || before.size == int64(^uint64(0)>>1) || limit != 0 && uint64(before.size) > limit {
		return nil, true, errors.New("private metadata is not a bounded owned single-link regular file")
	}
	if physical.step != nil {
		if err := physical.step("before-open"); err != nil {
			return nil, true, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	file, err := root.openFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, true, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, physical.closeFile(file))
		afterClose, statErr := root.stat(name)
		if statErr != nil || before != afterClose {
			resultErr = errors.Join(resultErr, errors.New("private metadata changed through close"), statErr)
		}
		resultErr = errors.Join(resultErr, root.check())
		if resultErr != nil {
			data = nil
		}
	}()
	opened, err := statAttemptPrivateFile(file)
	if err != nil || before != opened {
		return nil, true, errors.Join(errors.New("private metadata changed during open"), err)
	}
	if physical.step != nil {
		if err := physical.step("after-open"); err != nil {
			return nil, true, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	data, err = io.ReadAll(io.LimitReader(file, before.size+1))
	if err != nil || int64(len(data)) != before.size {
		return nil, true, errors.Join(errors.New("private metadata read length changed"), err)
	}
	if physical.step != nil {
		if err := physical.step("after-read"); err != nil {
			return nil, true, err
		}
	}
	after, err := root.stat(name)
	if err != nil || before != after {
		return nil, true, errors.Join(errors.New("private metadata changed or disappeared during read"), err)
	}
	final, err := statAttemptPrivateFile(file)
	if err != nil || before != final {
		return nil, true, errors.Join(errors.New("private metadata descriptor changed during read"), err)
	}
	if err := root.check(); err != nil {
		return nil, true, err
	}
	return data, true, nil
}
