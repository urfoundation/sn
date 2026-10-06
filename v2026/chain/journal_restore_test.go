//go:build linux

// The bounded restore reader cannot return admitted bytes after cancellation
// or a failed exact-size check, even if the underlying read filled its buffer.
package chain

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The reader observes context before admission, before its one small read and
// at return. Cancellation at that final boundary preserves the original file.
type nativeRestoreFinalCancellation struct {
	context.Context
	observations int
}

// This deterministic instance is used synchronously by one bounded reader.
func (self *nativeRestoreFinalCancellation) Err() error {
	self.observations++
	if self.observations >= 3 {
		return context.Canceled
	}
	return nil
}

// Fixture-owned directories are private regardless of the invoking umask.
func nativeRestoreReaderFixture(t *testing.T, raw []byte) (*os.File, durablevolume.PreparationFile) {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "record"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root, durablevolume.PreparationFile{Path: "record", Kind: "file", Mode: 0600, Bytes: uint64(len(raw)), Sha256: "sha256:" + nativeDigest(raw)}
}

// Exact end-of-file and empty payloads are ordinary successful bounded reads.
func TestNativeRestoreReaderAcceptsExactAndEmptyPayloads(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("synthetic exact original bytes")} {
		root, expected := nativeRestoreReaderFixture(t, raw)
		actual, _, err := nativeRestoreRead(t.Context(), root, "record", expected)
		if err != nil || !bytes.Equal(actual, raw) {
			t.Fatal("ordinary exact restore read lost bytes or wrapped eof", err)
		}
	}
}

// A complete physical read never overrules a later cancellation or claims
// short original custody as valid by returning its available prefix.
func TestNativeRestoreReaderRefusesUnadmittedBytes(t *testing.T) {
	raw := []byte("synthetic full retained payload")
	root, expected := nativeRestoreReaderFixture(t, raw)
	ctx := &nativeRestoreFinalCancellation{Context: t.Context()}
	actual, _, err := nativeRestoreRead(ctx, root, "record", expected)
	if !errors.Is(err, context.Canceled) || len(actual) != 0 || ctx.observations != 3 {
		t.Fatal("post-read cancellation admitted bytes", len(actual), ctx.observations, err)
	}
	retained, err := os.ReadFile(filepath.Join(root.Name(), "record"))
	if err != nil || !bytes.Equal(retained, raw) {
		t.Fatal("refused read changed original payload", err)
	}
	expected.Bytes++
	actual, _, err = nativeRestoreRead(t.Context(), root, "record", expected)
	if !errors.Is(err, durablevolume.ErrIdentity) || len(actual) != 0 {
		t.Fatal("short original payload admitted a prefix", len(actual), err)
	}
}
