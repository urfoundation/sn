//go:build linux

// The actual file-backed read adapters retain standard reader sentinels while
// preserving a cancellation observed immediately after the real read.
package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// Standard consumers require the original EOF sentinel after exact bytes end.
func TestAttemptPreparationReadersRetainStandardEof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retained")
	raw := []byte("original public bytes")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := &attemptPreparationReader{File: file, ctx: t.Context()}
	actual, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("standard reader failed exact public bytes", err)
	}
	buffer := make([]byte, len(raw)+8)
	n, err := reader.ReadAt(buffer, 0)
	if n != len(raw) || err != io.EOF || !bytes.Equal(buffer[:n], raw) {
		t.Fatal("ReaderAt short read lost exact EOF contract", n, err)
	}
	n, err = reader.Read(buffer)
	if n != 0 || err != io.EOF {
		t.Fatal("final EOF lost identity", n, err)
	}
}

// This context cancels the actual context at a named observation count; it
// never fabricates a successful read or bypasses any descriptor operation.
type attemptPreparationCancelAtReadEnd struct {
	context.Context
	observations atomic.Uint64
	cancel       context.CancelFunc
}

// The second actual admission observation cancels immediately after the read.
func (self *attemptPreparationCancelAtReadEnd) Err() error {
	if self.observations.Add(1) == 2 {
		self.cancel()
	}
	return self.Context.Err()
}

// A real post-read cancellation must remain visible alongside a short EOF read.
func TestAttemptPreparationReadersKeepCancellationAlongsideEof(t *testing.T) {
	for _, mode := range []string{"read", "read-at"} {
		func() {
			path := filepath.Join(t.TempDir(), "empty")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			checked := &attemptPreparationCancelAtReadEnd{Context: ctx, cancel: cancel}
			reader := &attemptPreparationReader{File: file, ctx: checked}
			buffer := make([]byte, 8)
			var n int
			if mode == "read" {
				n, err = reader.Read(buffer)
			} else {
				n, err = reader.ReadAt(buffer, 0)
			}
			if n != 0 || err == io.EOF || !errors.Is(err, io.EOF) || !errors.Is(err, context.Canceled) {
				t.Fatal("EOF concealed the real post-read cancellation", mode, n, err)
			}
		}()
	}
}
