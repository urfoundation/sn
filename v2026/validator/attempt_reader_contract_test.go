//go:build linux || darwin

// Standard stream consumers require an unwrapped EOF when the retained spool
// remains healthy. A post-read custody or cancellation failure stays visible.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newAttemptSpoolReaderTest(t *testing.T, ctx context.Context, raw []byte) (*attemptCutV2SealScratch, *attemptCutV2SealSpool) {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	scratch, err := newAttemptCutV2SealScratch(ctx, filepath.Join(parent, "scratch"), attemptCutV2SealHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = scratch.close()
		for _, spool := range scratch.spools {
			if spool != nil {
				if _, err := spool.file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Error("spool cleanup did not join the actual descriptor", err)
				}
			}
		}
	})
	spool, err := scratch.openSpool(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := spool.Write(raw); err != nil || n != len(raw) {
		t.Fatal("spool preparation changed original bytes", n, err)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return scratch, spool
}

func TestAttemptSpoolReadAllCompletesOriginalBytes(t *testing.T) {
	raw := []byte("original bounded descriptor bytes")
	scratch, spool := newAttemptSpoolReaderTest(t, t.Context(), raw)
	actual, err := io.ReadAll(spool)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("standard ReadAll refused complete retained spool: %q %v", actual, err)
	}
	if err := scratch.close(); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptSpoolCopyCompletesOriginalBytes(t *testing.T) {
	raw := []byte("original bounded descriptor bytes")
	scratch, spool := newAttemptSpoolReaderTest(t, t.Context(), raw)
	var output bytes.Buffer
	n, err := io.Copy(&output, spool)
	if err != nil || n != int64(len(raw)) || !bytes.Equal(output.Bytes(), raw) {
		t.Fatalf("standard Copy refused complete retained spool: n=%d err=%v", n, err)
	}
	if err := scratch.close(); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptSpoolReadPreservesExactEof(t *testing.T) {
	raw := []byte("original bounded descriptor bytes")
	scratch, spool := newAttemptSpoolReaderTest(t, t.Context(), raw)
	buffer := make([]byte, len(raw)+8)
	if n, err := spool.Read(buffer); n != len(raw) || err != nil || !bytes.Equal(buffer[:n], raw) {
		t.Fatal("short read changed actual spool bytes", n, err)
	}
	if n, err := spool.Read(buffer); n != 0 || err != io.EOF {
		t.Fatalf("healthy guarded EOF lost reader sentinel identity: n=%d err=%v", n, err)
	}
	if err := scratch.close(); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptSpoolJsonDecoderPreservesTerminalEof(t *testing.T) {
	scratch, spool := newAttemptSpoolReaderTest(t, t.Context(), []byte(`{"descriptor":"original"}`))
	decoder := json.NewDecoder(spool)
	var value map[string]string
	if err := decoder.Decode(&value); err != nil || value["descriptor"] != "original" {
		t.Fatal("decoder lost complete original spool JSON", value, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("decoder terminal EOF lost reader sentinel identity", err)
	}
	if err := scratch.close(); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptSpoolPostReadCancellationPreservesBothCauses(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scratch, spool := newAttemptSpoolReaderTest(t, ctx, nil)
	called := false
	scratch.hooks.Step = func(operation, name string) error {
		if operation == "after-spool-read" {
			called = true
			cancel()
		}
		return nil
	}
	_, err := io.ReadAll(spool)
	if !called || err == nil || err == io.EOF || !errors.Is(err, io.EOF) || !errors.Is(err, context.Canceled) {
		t.Fatal("EOF hid actual post-read cancellation", called, err)
	}
}

func TestAttemptSpoolPostReadReplacementPreservesOriginalBytes(t *testing.T) {
	scratch, spool := newAttemptSpoolReaderTest(t, t.Context(), nil)
	called := false
	scratch.hooks.Step = func(operation, name string) error {
		if operation != "after-spool-read" {
			return nil
		}
		called = true
		path := filepath.Join(scratch.path, name)
		if err := os.Rename(path, path+".retained"); err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(path, []byte("unrelated replacement"), 0600)
	}
	_, err := io.ReadAll(spool)
	if !called || err == nil || err == io.EOF || !errors.Is(err, io.EOF) || !strings.Contains(err.Error(), "spool inode or bound changed") {
		t.Fatal("EOF hid actual post-read retained inode loss", called, err)
	}
	retained, err := os.ReadFile(filepath.Join(scratch.path, "records.descriptors.retained"))
	if err != nil || len(retained) != 0 {
		t.Fatal("post-read refusal modified original completed bytes", retained, err)
	}
}
