//go:build linux

// Standard consumers may finish from n>0 even when a reader also reports an
// error. A revoked preparation read therefore admits none of that call's bytes.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The actual context is canceled after one real descriptor read, not by a fake
// data source. The file remains intact and its cursor must never be rewound.
func attemptPreparationInvalidatedReadFixture(t *testing.T, raw []byte) (*attemptPreparationReader, *os.File) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "public-reviewed-bytes")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return &attemptPreparationReader{File: file, ctx: &attemptPreparationCancelAtReadEnd{Context: ctx, cancel: cancel}}, file
}

// A complete-sized read cannot bypass the cancellation by satisfying ReadFull.
func TestAttemptPreparationReadFullRejectsPostReadCancellation(t *testing.T) {
	raw := []byte("exact reviewed public payload")
	reader, file := attemptPreparationInvalidatedReadFixture(t, raw)
	buffer := make([]byte, len(raw))
	n, err := io.ReadFull(reader, buffer)
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("ReadFull accepted bytes invalidated after the real read", n, err)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != int64(len(raw)) {
		t.Fatal("failed admission rewound the real file cursor", position, err)
	}
	retained, err := os.ReadFile(file.Name())
	if err != nil || !bytes.Equal(raw, retained) {
		t.Fatal("failed admission changed retained public bytes", err)
	}
}

// A complete JSON value must not let Decoder discard the guard error.
func TestAttemptPreparationJsonRejectsPostReadCancellation(t *testing.T) {
	reader, _ := attemptPreparationInvalidatedReadFixture(t, []byte(`{"reviewed":"public"}`))
	var value map[string]string
	if err := json.NewDecoder(reader).Decode(&value); !errors.Is(err, context.Canceled) || len(value) != 0 {
		t.Fatal("JSON accepted a complete value after read admission was revoked", value, err)
	}
}

// ReaderAt's final successful chunk still requires post-read admission.
func TestAttemptPreparationReadAtRejectsPostReadCancellation(t *testing.T) {
	raw := []byte("exact complete block")
	reader, _ := attemptPreparationInvalidatedReadFixture(t, raw)
	n, err := reader.ReadAt(make([]byte, len(raw)), 0)
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("ReadAt admitted a complete block after cancellation", n, err)
	}
}

// Cancellation between chunks invalidates the entire operation, not only the
// next chunk. No consumer may retain the earlier unacknowledged prefix.
func TestAttemptPreparationReadAtCancellationAdmitsNoPrefix(t *testing.T) {
	raw := bytes.Repeat([]byte("p"), 64*1024+8)
	reader, _ := attemptPreparationInvalidatedReadFixture(t, raw)
	n, err := reader.ReadAt(make([]byte, len(raw)), 0)
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("ReadAt returned an admitted prefix after cancellation", n, err)
	}
}
