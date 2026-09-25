// Synthetic readers force large-file resource bounds, read failure and owner
// cancellation without allocating a production-sized executable in the test.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fileHashTestReader struct {
	read func([]byte) (int, error)
}

func (self *fileHashTestReader) Read(buffer []byte) (int, error) {
	return self.read(buffer)
}

// A grow-to-fit read fails deterministically at the reader boundary. Exact
// digest equality also covers short final chunks and zero-length files.
func TestFileSha256StreamsBoundedChunks(t *testing.T) {
	for _, size := range []int{0, 1, fileHashBufferBytes, 4*fileHashBufferBytes + 17} {
		payload := bytes.Repeat([]byte{0x39}, size)
		source := bytes.NewReader(payload)
		reads := 0
		reader := &fileHashTestReader{read: func(buffer []byte) (int, error) {
			reads++
			if len(buffer) > fileHashBufferBytes {
				return 0, fmt.Errorf("unbounded executable read: %d bytes", len(buffer))
			}
			return source.Read(buffer)
		}}
		hash, err := streamSHA256Context(t.Context(), reader)
		want := fmt.Sprintf("sha256:%x", sha256.Sum256(payload))
		if err != nil || hash != want || reads < (size+fileHashBufferBytes-1)/fileHashBufferBytes {
			t.Fatalf("size=%d hash=%s want=%s reads=%d: %v", size, hash, want, reads, err)
		}
	}
}

// A read can return bytes together with failure. No hash of that partial prefix
// may be presented as authenticated file content.
func TestFileSha256PreservesReadFailure(t *testing.T) {
	want := errors.New("synthetic file read failure")
	reads := 0
	reader := &fileHashTestReader{read: func(buffer []byte) (int, error) {
		reads++
		return copy(buffer, "synthetic partial executable"), want
	}}
	hash, err := streamSHA256Context(t.Context(), reader)
	if hash != "" || !errors.Is(err, want) || reads != 1 {
		t.Fatalf("partial hash=%s reads=%d error=%v", hash, reads, err)
	}
}

// Cancellation after one read is an explicit barrier, independent of the
// machine's throughput or test timeouts. Even data plus EOF cannot hide it.
func TestFileSha256CancellationStopsFurtherReads(t *testing.T) {
	for _, readErr := range []error{nil, io.EOF} {
		ctx, cancel := context.WithCancel(t.Context())
		reads := 0
		reader := &fileHashTestReader{read: func(buffer []byte) (int, error) {
			reads++
			cancel()
			return copy(buffer, "synthetic executable prefix"), readErr
		}}
		hash, err := streamSHA256Context(ctx, reader)
		cancel()
		if hash != "" || !errors.Is(err, context.Canceled) || reads != 1 {
			t.Fatalf("read error=%v hash=%s reads=%d error=%v", readErr, hash, reads, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	hash, err := fileSHA256Context(ctx, filepath.Join(t.TempDir(), "absent-executable"))
	if hash != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled owner attempted new file read: hash=%s error=%v", hash, err)
	}
}

// Path, size and modification time are not content authority. Equal-size
// rewrites with restored timestamps and atomic replacements are always read.
func TestFileSha256RechecksChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-executable")
	modifiedAt := time.Unix(100, 0)
	for index, payload := range []string{"synthetic-first", "synthetic-other", "synthetic-third"} {
		writePath := path
		if index == 2 {
			writePath = path + ".replacement"
		}
		if err := os.WriteFile(writePath, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(writePath, modifiedAt, modifiedAt); err != nil {
			t.Fatal(err)
		}
		if writePath != path {
			if err := os.Rename(writePath, path); err != nil {
				t.Fatal(err)
			}
		}
		hash, err := fileSHA256(path)
		want := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(payload)))
		if err != nil || hash != want {
			t.Fatalf("rewrite=%d hash=%s want=%s error=%v", index, hash, want, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if hash, err := fileSHA256(path); hash != "" || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted executable retained hash=%s error=%v", hash, err)
	}
}

// The shared file reader preserves exact evidence bytes, exclusions and static
// configuration mode/refusal semantics while bounding every hash-only read.
func TestFileSha256EvidenceAndConfigContracts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic-config.yml")
	payload := bytes.Repeat([]byte("synthetic fixture data\n"), fileHashBufferBytes/4)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("sha256:%x", sha256.Sum256(payload))
	digest, mode, err := runtimeConfigFileDigest(path)
	if err != nil || digest != want || mode != 0o600 {
		t.Fatalf("config digest=%s mode=%o error=%v", digest, mode, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "complete.json"), []byte("excluded root completion"), 0o600); err != nil {
		t.Fatal(err)
	}
	hashes, err := evidenceFileHashes(dir, 2)
	if err != nil || len(hashes) != 1 || hashes["synthetic-config.yml"] != want {
		t.Fatalf("evidence hash contract changed: %+v error=%v", hashes, err)
	}
	link := filepath.Join(t.TempDir(), "synthetic-symlink.yml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if digest, _, err := runtimeConfigFileDigest(link); err == nil || digest != "" {
		t.Fatalf("streaming followed a forbidden config symlink: digest=%s error=%v", digest, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if digest, _, err := runtimeConfigFileDigest(path); digest != "" || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("streaming hid missing config: digest=%s error=%v", digest, err)
	}
}
