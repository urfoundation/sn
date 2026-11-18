// Root observation startup retains the original policy descriptor and checkpoint
// owner while bounded unavailable reads recover. No retry provisions custody.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// An instance-local observation barrier follows real file reads and cannot
// provide policy bytes or successful authority. No public input installs it.
type rootMonitorPolicyObservationKey struct{}

// Cleanup controls observe the already closed descriptor and can only refuse
// successful cleanup; existing checkpoint/metrics hooks keep their own scope.
type rootMonitorPolicyCloseObservationKey struct{}

// Every actual cause must be a known read/resource failure. Typed refusal,
// cleanup, uncertainty, unknown joins, cycles and custom Is/As cannot retry.
func rootMonitorStartupPending(err error) bool {
	nodes := 0
	var visit func(error, int) bool
	visit = func(cause error, depth int) bool {
		nodes++
		if cause == nil || depth >= 32 || nodes > 128 {
			return false
		}
		value := reflect.ValueOf(cause)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return false
			}
		}
		switch cause.(type) {
		case *monitorAdmissionRefusalError, *monitorAdmissionCleanupError, *monitorOutputOwnershipError:
			return false
		}
		if cause == context.Canceled || cause == context.DeadlineExceeded || cause == durablevolume.ErrBusy || cause == durablevolume.ErrUnavailable {
			return true
		}
		if errno, ok := cause.(syscall.Errno); ok {
			switch errno {
			case syscall.EAGAIN, syscall.EBUSY, syscall.EIO, syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM, syscall.ENOSPC, syscall.EDQUOT, syscall.EINTR, syscall.ETIMEDOUT:
				return true
			}
			return false
		}
		// syscall.Errno has its own standard Is method. Admit only the exact
		// trusted leaves above before rejecting arbitrary custom classifiers.
		switch cause.(type) {
		case interface{ Is(error) bool }, interface{ As(any) bool }:
			return false
		}
		if many, ok := cause.(interface{ Unwrap() []error }); ok {
			children := many.Unwrap()
			if len(children) == 0 || len(children) > 128-nodes {
				return false
			}
			for _, child := range children {
				if !visit(child, depth+1) {
					return false
				}
			}
			return true
		}
		if one, ok := cause.(interface{ Unwrap() error }); ok {
			return visit(one.Unwrap(), depth+1)
		}
		return false
	}
	return visit(err, 0)
}

// Parent cancellation remains a normal joined stop only when all observed
// causes are soft. Earlier read causes survive; hard joined facts stay terminal.
func rootMonitorStartupExit(ctx context.Context, err error, hard int) int {
	if _, cleanup := err.(*monitorAdmissionCleanupError); cleanup {
		return 3
	}
	pending := rootMonitorStartupPending(err)
	if pending && ctx.Err() != nil {
		return 0
	}
	if pending {
		return 1
	}
	return hard
}

// The caller supplies one shared startup deadline, so policy, admission and
// checkpoint load cannot each reset the original 300-second default window.
// All reads and waits remain synchronous until their owner can safely close.
func retryRootMonitorStartup(ctx context.Context, deadline time.Time, now func() time.Time, hooks monitorServiceHooks, diagnostic io.Writer, observe func() error) error {
	backoff := time.Second
	var firstErr, lastErr error
	for attempt := 0; attempt < 128 && ctx.Err() == nil && now().Before(deadline); attempt++ {
		err := observe()
		if err == nil && ctx.Err() == nil && now().Before(deadline) {
			return nil
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			lastErr = err
			if !rootMonitorStartupPending(err) {
				return &monitorAdmissionRefusalError{cause: errors.Join(firstErr, lastErr)}
			}
		}
		if ctx.Err() != nil || !now().Before(deadline) {
			break
		}
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			break
		}
		fmt.Fprintln(diagnostic, "root startup observation unavailable; retrying original custody")
		wait := min(backoff, remaining)
		var waitErr error
		if hooks.rpcWait != nil {
			waitErr = hooks.rpcWait(ctx, "root", wait)
		} else {
			waitErr = waitRpcReadRetry(ctx, wait)
		}
		if waitErr != nil {
			return errors.Join(firstErr, lastErr, waitErr)
		}
		backoff = min(2*backoff, 10*time.Second)
	}
	cause := ctx.Err()
	if cause == nil {
		cause = context.DeadlineExceeded
	}
	return errors.Join(firstErr, lastErr, cause)
}

// Each retry reads the same opened regular inode from offset zero. A successful
// observation of changed metadata/name or malformed policy is a hard refusal.
// A failed close dominates any preceding soft read and prevents a new owner.
func readRootMonitorStartupPolicy(ctx context.Context, path string, deadline time.Time, now func() time.Time, hooks monitorServiceHooks, diagnostic io.Writer) (policy rootValidatorPolicy, hash string, resultErr error) {
	var file *os.File
	var original os.FileInfo
	defer func() {
		if file != nil {
			closeErr := file.Close()
			if after, ok := ctx.Value(rootMonitorPolicyCloseObservationKey{}).(func(*os.File) error); ok && after != nil {
				closeErr = errors.Join(closeErr, after(file))
			}
			if closeErr != nil {
				resultErr = monitorAdmissionFailure(resultErr, closeErr)
			}
		}
	}()
	resultErr = retryRootMonitorStartup(ctx, deadline, now, hooks, diagnostic, func() error {
		if file == nil {
			fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			file = os.NewFile(uintptr(fd), path)
		}
		before, err := file.Stat()
		if err != nil {
			return err
		}
		if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxRpcReplyBytes {
			return errors.New("root policy is not a bounded regular file")
		}
		if original == nil {
			original = before
		} else if !rootMonitorSamePolicyFile(original, before) {
			return errors.New("root policy changed before its original read")
		}
		raw, readErr := io.ReadAll(io.NewSectionReader(file, 0, maxRpcReplyBytes+1))
		after, statErr := file.Stat()
		named, nameErr := os.Lstat(path)
		if len(raw) > maxRpcReplyBytes || readErr == nil && int64(len(raw)) != original.Size() || statErr == nil && !rootMonitorSamePolicyFile(original, after) || nameErr == nil && !rootMonitorSamePolicyFile(original, named) {
			return &monitorAdmissionRefusalError{cause: errors.Join(errors.New("root policy changed during its original read"), readErr, statErr, nameErr)}
		}
		if err := errors.Join(readErr, statErr, nameErr, ctx.Err()); err != nil {
			return err
		}
		if after, ok := ctx.Value(rootMonitorPolicyObservationKey{}).(func(context.Context, *os.File) error); ok && after != nil {
			if err := after(ctx, file); err != nil {
				return err
			}
		}
		if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
			return err
		}
		var observed rootValidatorPolicy
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&observed); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return errors.New("root policy has trailing JSON")
		}
		if err := observed.validate(); err != nil {
			return err
		}
		policy, hash = observed, monitorReadDigest(raw)
		return nil
	})
	return policy, hash, resultErr
}

// Only a completed physical observation can prove changed original policy.
func rootMonitorSamePolicyFile(original, observed os.FileInfo) bool {
	return observed.Mode().IsRegular() && os.SameFile(original, observed) && original.Mode() == observed.Mode() && original.Size() == observed.Size() && original.ModTime().Equal(observed.ModTime())
}
