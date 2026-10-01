// Operational inputs are bounded protected regular files. Every read owns its
// descriptors through a final identity check and the actual closes.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Closed read classifications contain neither a source path nor a raw error.
type monitorServiceReadError struct {
	code  string
	cause error
}

// Callers log the classification; this observer never interprets remote errors.
func (self *monitorServiceReadError) Error() string { return self.code }

// Policy admission retains real causes without emitting source paths in roles.
func (self *monitorServiceReadError) Unwrap() error { return self.cause }

// Tests observe real file phases and may fail only after an actual close.
type monitorServiceReadHooks struct {
	afterRead  func(*os.File) error
	afterClose func(*os.File) error
}

// Finite reads refuse aliases, special files, oversized data and replacements
// within a read. Atomic replacement between separate samples remains ordinary.
func readMonitorServiceFile(ctx context.Context, path string, limit int64, private bool, hooks monitorServiceReadHooks) (raw []byte, resultErr error) {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || limit < 1 || limit > 128*1024 {
		return nil, &monitorServiceReadError{code: "invalid"}
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			raw = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	classify := func(err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return &monitorServiceReadError{code: "missing", cause: err}
		}
		if errors.Is(err, syscall.ELOOP) {
			return &monitorServiceReadError{code: "invalid", cause: err}
		}
		return &monitorServiceReadError{code: "unavailable", cause: err}
	}
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, classify(err)
	}
	if resolved != directory {
		return nil, &monitorServiceReadError{code: "invalid"}
	}
	parent, err := os.Lstat(directory)
	if err != nil {
		return nil, classify(err)
	}
	if !parent.IsDir() || parent.Mode().Perm()&0022 != 0 {
		return nil, &monitorServiceReadError{code: "invalid"}
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, classify(err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() {
		closeErr := file.Close()
		if hooks.afterClose != nil {
			closeErr = errors.Join(closeErr, hooks.afterClose(file))
		}
		if closeErr != nil {
			resultErr = errors.Join(resultErr, &monitorServiceReadError{code: "unavailable", cause: closeErr})
			raw = nil
		}
	}()
	opened, err := file.Stat()
	if err != nil {
		return nil, classify(err)
	}
	permissions := os.FileMode(0022)
	if private {
		permissions = 0077
	}
	if !opened.Mode().IsRegular() || opened.Mode().Perm()&permissions != 0 || opened.Size() < 1 || opened.Size() > limit {
		return nil, &monitorServiceReadError{code: "invalid"}
	}
	raw, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, classify(err)
	}
	if hooks.afterRead != nil {
		if err := hooks.afterRead(file); err != nil {
			return nil, &monitorServiceReadError{code: "unavailable", cause: err}
		}
	}
	after, err := file.Stat()
	named, nameErr := os.Lstat(path)
	parentAfter, parentErr := os.Lstat(directory)
	if err != nil || nameErr != nil || parentErr != nil || !os.SameFile(parent, parentAfter) || parent.Mode() != parentAfter.Mode() ||
		!os.SameFile(opened, after) || !os.SameFile(opened, named) || opened.Mode() != after.Mode() || opened.Mode() != named.Mode() ||
		opened.Size() != int64(len(raw)) || opened.Size() != after.Size() || opened.ModTime() != after.ModTime() || len(raw) > int(limit) {
		return nil, &monitorServiceReadError{code: "changed", cause: errors.Join(err, nameErr, parentErr)}
	}
	return raw, nil
}

// Source matching is independent of the candidate record and of any retained
// intent configuration. Original intent hashes remain operational evidence.
func readMonitorValidatorProgress(ctx context.Context, expected monitorValidatorPolicy, hooks monitorServiceReadHooks) (*protocol.ValidatorProgress, string) {
	raw, err := readMonitorServiceFile(ctx, expected.ProgressFile, protocol.MaxValidatorProgressBytes, false, hooks)
	if err != nil {
		var readErr *monitorServiceReadError
		if errors.As(err, &readErr) {
			return nil, readErr.code
		}
		return nil, "unavailable"
	}
	value, err := protocol.DecodeValidatorProgress(raw)
	if err != nil {
		return nil, "invalid"
	}
	if value.Source != expected.ExpectedSource {
		return nil, "identity"
	}
	return value, "ok"
}
