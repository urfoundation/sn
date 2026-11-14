//go:build linux || darwin

package validator

// Force custody loss after actual file reads, then observe the acceptance
// boundary of standard consumers and the production descriptor reader.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The instance hook runs synchronously after the real retained fd was read.
func armAttemptSpoolAcceptanceFault(t *testing.T, scratch *attemptCutV2SealScratch, cancel context.CancelFunc, replace bool, onRead int) *int {
	t.Helper()
	reads := new(int)
	scratch.hooks.Step = func(operation, name string) error {
		if operation != "after-spool-read" {
			return nil
		}
		*reads++
		if *reads != onRead {
			return nil
		}
		if !replace {
			cancel()
			return nil
		}
		path := filepath.Join(scratch.path, name)
		if err := os.Rename(path, path+".retained"); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("unrelated replacement"), 0600)
	}
	return reads
}

// Read refusal must preserve the physical bytes and actual fd position.
func checkAttemptSpoolAcceptanceFault(t *testing.T, scratch *attemptCutV2SealScratch, spool *attemptCutV2SealSpool, raw []byte, replace bool, err error) {
	t.Helper()
	if replace {
		if err == nil || !strings.Contains(err.Error(), "spool inode or bound changed") {
			t.Fatal("actual named-inode refusal was lost", err)
		}
	} else if !errors.Is(err, context.Canceled) {
		t.Fatal("actual post-read cancellation was lost", err)
	}
	name := spool.name
	if replace {
		name += ".retained"
	}
	actual, readErr := os.ReadFile(filepath.Join(scratch.path, name))
	if readErr != nil || !bytes.Equal(actual, raw) || spool.position != uint64(len(raw)) {
		t.Fatal("refused read changed original bytes or actual fd position", readErr, spool.position)
	}
}

// A full buffer with a nonnil error is still success to io.ReadFull.
func testAttemptSpoolReadFullRefusal(t *testing.T, replace bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	raw := []byte("original bounded descriptor bytes")
	scratch, spool := newAttemptSpoolReaderTest(t, ctx, raw)
	reads := armAttemptSpoolAcceptanceFault(t, scratch, cancel, replace, 1)
	buffer := make([]byte, len(raw))
	n, err := io.ReadFull(spool, buffer)
	if *reads != 1 || err == nil || n != 0 {
		t.Fatalf("ReadFull accepted a complete buffer after actual post-read refusal: reads=%d n=%d err=%v", *reads, n, err)
	}
	checkAttemptSpoolAcceptanceFault(t, scratch, spool, raw, replace, err)
}

func TestAttemptSpoolReadFullRejectsCanceledCompleteBuffer(t *testing.T) {
	testAttemptSpoolReadFullRefusal(t, false)
}

func TestAttemptSpoolReadFullRejectsReplacedCompleteBuffer(t *testing.T) {
	testAttemptSpoolReadFullRefusal(t, true)
}

// One valid JSON value can be decoded before a deferred reader error.
func testAttemptSpoolJsonAcceptanceRefusal(t *testing.T, replace bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	raw := []byte(`{"descriptor":"original"}`)
	scratch, spool := newAttemptSpoolReaderTest(t, ctx, raw)
	reads := armAttemptSpoolAcceptanceFault(t, scratch, cancel, replace, 1)
	var value map[string]string
	err := json.NewDecoder(spool).Decode(&value)
	if *reads != 1 || err == nil || value != nil {
		t.Fatalf("JSON decoder accepted a complete value after actual post-read refusal: reads=%d value=%v err=%v", *reads, value, err)
	}
	checkAttemptSpoolAcceptanceFault(t, scratch, spool, raw, replace, err)
}

func TestAttemptSpoolJsonRejectsCanceledCompleteValue(t *testing.T) {
	testAttemptSpoolJsonAcceptanceRefusal(t, false)
}

func TestAttemptSpoolJsonRejectsReplacedCompleteValue(t *testing.T) {
	testAttemptSpoolJsonAcceptanceRefusal(t, true)
}

// Earlier admitted bytes remain partial; a later EOF cannot hide custody loss.
func testAttemptSpoolPartialEofRefusal(t *testing.T, replace bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	raw := []byte("original bounded descriptor bytes")
	scratch, spool := newAttemptSpoolReaderTest(t, ctx, raw)
	reads := armAttemptSpoolAcceptanceFault(t, scratch, cancel, replace, 2)
	buffer := make([]byte, len(raw)+1)
	n, err := io.ReadFull(spool, buffer)
	if *reads != 2 || n != len(raw) || !bytes.Equal(buffer[:n], raw) || err == io.EOF || !errors.Is(err, io.EOF) {
		t.Fatal("partial ReadFull lost admitted prefix or terminal EOF refusal", *reads, n, err)
	}
	checkAttemptSpoolAcceptanceFault(t, scratch, spool, raw, replace, err)
}

func TestAttemptSpoolPartialEofRetainsCancellation(t *testing.T) {
	testAttemptSpoolPartialEofRefusal(t, false)
}

func TestAttemptSpoolPartialEofRetainsReplacement(t *testing.T) {
	testAttemptSpoolPartialEofRefusal(t, true)
}

// The existing production consumer already checks errors before decoding.
func testAttemptSpoolDescriptorReaderRefusal(t *testing.T, replace bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	raw := make([]byte, attemptStreamV2DescriptorBytes)
	scratch, spool := newAttemptSpoolReaderTest(t, ctx, raw)
	reads := armAttemptSpoolAcceptanceFault(t, scratch, cancel, replace, 1)
	writer := attemptStreamV2Writer{ctx: ctx, options: AttemptStreamV2WriteOptions{Spool: spool}}
	chunk, previous, err := writer.readDescriptor(0, sha256.Sum256(raw))
	if *reads != 1 || err == nil || chunk != (AttemptStreamV2Chunk{}) || previous != ([32]byte{}) {
		t.Fatal("production descriptor reader accepted invalidated bytes", *reads, chunk, err)
	}
	checkAttemptSpoolAcceptanceFault(t, scratch, spool, raw, replace, err)
}

func TestAttemptSpoolDescriptorReaderRejectsCancellation(t *testing.T) {
	testAttemptSpoolDescriptorReaderRefusal(t, false)
}

func TestAttemptSpoolDescriptorReaderRejectsReplacement(t *testing.T) {
	testAttemptSpoolDescriptorReaderRefusal(t, true)
}
