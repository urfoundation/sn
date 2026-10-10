// Real pipe copying must enter every owned output guard. Promoted ReaderFrom
// methods can otherwise bypass Write limits, callbacks and active cancellation.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durableinspect"
)

// Only the test executable recognizes this synthetic process protocol. It
// emits public bytes and remains alive until the actual caller cancels it.
func init() {
	var parts []string
	if len(os.Args) == 4 && os.Args[1] == "--synthetic-command-output" {
		parts = os.Args[2:]
	} else if len(os.Args) == 4 && os.Args[1] == "-I" && os.Args[2] == "-c" && strings.HasPrefix(os.Args[3], "synthetic-command-output ") {
		parts = strings.Fields(os.Args[3])[1:]
	}
	if len(parts) != 2 {
		return
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil || count < 1 || count > sourceLockMaximumModuleBytes+1 {
		os.Exit(21)
	}
	writer := os.Stdout
	if parts[0] == "stderr" {
		writer = os.Stderr
	} else if parts[0] != "stdout" {
		os.Exit(22)
	}
	chunk := bytes.Repeat([]byte{'x'}, 32*1024)
	for count > 0 {
		n := min(count, len(chunk))
		if _, err := writer.Write(chunk[:n]); err != nil {
			break
		}
		count -= n
	}
	time.Sleep(24 * time.Hour)
	os.Exit(23)
}

func TestCommandPipeCopyCannotBypassAnyOutputLimit(t *testing.T) {
	type outputCase struct {
		name    string
		maximum int
		open    func(context.CancelFunc) (io.Writer, func() int)
	}
	cases := []outputCase{
		{name: "historical", maximum: 64, open: func(cancel context.CancelFunc) (io.Writer, func() int) {
			out := &historicalReplayOutput{maximum: 64, cancel: cancel}
			return out, out.buffer.Len
		}},
		{name: "owner-adapter", maximum: ownerSigningReplyLimit, open: func(cancel context.CancelFunc) (io.Writer, func() int) {
			out := &ownerSigningBoundedOutput{cancel: cancel}
			return out, out.buffer.Len
		}},
		{name: "storage-inspection", maximum: durableinspect.MaximumReportBytes, open: func(cancel context.CancelFunc) (io.Writer, func() int) {
			out := &serviceStorageInspectionOutput{cancel: cancel}
			return out, out.buffer.Len
		}},
		{name: "repair", maximum: 32 * 1024, open: func(cancel context.CancelFunc) (io.Writer, func() int) {
			out := &repairValidatorOutput{cancel: cancel}
			return out, out.buffer.Len
		}},
		{name: "source-lock", maximum: sourceLockMaximumModuleBytes, open: func(cancel context.CancelFunc) (io.Writer, func() int) {
			out := &sourceLockOutput{cancel: cancel}
			return out, out.buffer.Len
		}},
	}
	for _, item := range cases {
		ctx, cancel := context.WithCancel(t.Context())
		output, retained := item.open(cancel)
		reader, writer, err := os.Pipe()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := writer.Write(bytes.Repeat([]byte{'x'}, item.maximum+1))
			done <- errors.Join(err, writer.Close())
		}()
		_, copyErr := io.Copy(output, reader)
		closeErr := reader.Close()
		<-done // closing the real read pipe joins a writer refused mid-stream
		if copyErr == nil || ctx.Err() != context.Canceled || retained() > item.maximum || closeErr != nil {
			t.Errorf("%s pipe copy bypassed output guard: err=%v owner=%v retained=%d close=%v", item.name, copyErr, ctx.Err(), retained(), closeErr)
		}
		cancel()
	}
}

func TestHistoricalReplayPipeCopyPreservesCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	output := historicalReplayOutput{maximum: 64, cancel: cancel, read: cancel}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("partial-public-output"))
		done <- errors.Join(err, writer.Close())
	}()
	_, copyErr := io.Copy(&output, reader)
	closeErr := reader.Close()
	writeErr := <-done
	if err := errors.Join(copyErr, closeErr, writeErr); err != nil || ctx.Err() != context.Canceled || output.buffer.String() != "partial-public-output" {
		t.Fatal("real pipe copy skipped guarded output callback", err, ctx.Err(), output.buffer.String())
	}
}

func TestRepairCommandOutputOverflowCancelsActualChild(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		result, runErr := executeRepairValidatorCommand(ctx, path, []string{"--synthetic-command-output", stream, strconv.Itoa(32*1024 + 1)})
		ownerErr := ctx.Err()
		cancel()
		if runErr == nil || !strings.Contains(runErr.Error(), "output exceeds") || ownerErr != nil || result != nil {
			t.Fatal("repair output overflow waited for caller expiry or published bytes", stream, runErr, ownerErr, len(result))
		}
	}
}

func TestSourceLockOutputBoundAppliesBeforeCollection(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := sourceLockCommand(t.Context(), t.TempDir(), path, "--synthetic-command-output", "stdout", strconv.Itoa(sourceLockMaximumModuleBytes+1))
	if runErr == nil || !strings.Contains(runErr.Error(), "resource bound") || errors.Is(runErr, context.DeadlineExceeded) || result != nil {
		t.Fatal("source-lock accumulated output until deadline instead of enforcing its guard", runErr, len(result))
	}
}

func TestOwnerAdapterOutputOverflowCancelsActualBoundary(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		helper := []byte("synthetic-command-output " + stream + " " + strconv.Itoa(ownerSigningReplyLimit+1))
		helperPath := filepath.Join(directory, "synthetic-adapter-"+stream)
		if err := os.WriteFile(helperPath, helper, 0600); err != nil {
			t.Fatal(err)
		}
		config := ownerSigningDeviceConfig{PythonPath: path, HelperPath: helperPath, HelperHash: monitorReadDigest(helper)}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		result, runErr := runOwnerLedgerAdapter(ctx, config, ownerSigningAdapterInput{Schema: ownerSigningAdapterSchema, Mode: "synthetic-no-device"})
		ownerErr := ctx.Err()
		cancel()
		if runErr == nil || !strings.Contains(runErr.Error(), "output exceeds bound") || errors.Is(runErr, context.DeadlineExceeded) || ownerErr != nil || result != (ownerSigningAdapterResult{}) {
			t.Fatal("owner adapter output bypassed actual boundary", stream, runErr, ownerErr, result)
		}
	}
}
