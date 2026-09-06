//go:build linux || darwin

package validator

// These codec fixtures exercise the pinned encoder and checked on-disk reader.
// Whole decoded copies are test oracles only, never production admission state.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/golang/snappy"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// A single real private file owns its original bytes through final Close.
func newAttemptAdmissionBlockTestFile(t *testing.T, ctx context.Context, encoded []byte, compression byte) (*attemptStoreAdmissionReader, *attemptStoreAdmissionFile, attemptAdmissionBlockHandle) {
	t.Helper()
	raw := append([]byte(nil), encoded...)
	raw = append(raw, compression)
	var checksum [4]byte
	binary.LittleEndian.PutUint32(checksum[:], util.NewCRC(raw).Value())
	raw = append(raw, checksum[:]...)
	return newAttemptAdmissionBlockTestRaw(t, ctx, raw)
}

// Raw negative framing is written before its own first descriptor attestation.
func newAttemptAdmissionBlockTestRaw(t *testing.T, ctx context.Context, raw []byte) (*attemptStoreAdmissionReader, *attemptStoreAdmissionFile, attemptAdmissionBlockHandle) {
	t.Helper()
	if len(raw) < 5 {
		t.Fatal("raw block test has no trailer")
	}
	path := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "000001.ldb"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	disk, err := openAttemptRecordStoreInspection(ctx, path, attemptRecordStoreBounds{MaxStorageBytes: 8 * 1024 * 1024, MaxStorageFiles: 8}, attemptRecordStoreHooks{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(); err != nil {
			t.Errorf("codec disk close: %v", err)
		}
	})
	file, err := disk.inspectFile("000001.ldb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Cancellation is an expected operation result, not a leaked descriptor.
		if err := file.Close(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, os.ErrClosed) {
			t.Errorf("codec file close: %v", err)
		}
	})
	return &attemptStoreAdmissionReader{ctx: ctx, disk: disk}, file, attemptAdmissionBlockHandle{length: uint64(len(raw) - 5)}
}

// Pure Go and assembly share the pinned encoder's 64 KiB segmentation loop.
func TestAttemptAdmissionSnappyMatchesPinnedEncoder(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, 1, 63, 64, 1024, 65535, 65536, 131077} {
		for _, repeated := range []bool{false, true} {
			plain := make([]byte, size)
			state := uint32(17)
			for index := range plain {
				state = state*1664525 + 1013904223
				plain[index] = byte(state >> 24)
				if repeated {
					plain[index] = byte(index % 31)
				}
			}
			encoded := snappy.Encode(nil, plain)
			reader, file, handle := newAttemptAdmissionBlockTestFile(t, context.Background(), encoded, 1)
			block, err := reader.block(file, handle)
			if err != nil {
				t.Fatalf("size=%d repeated=%t: %v", size, repeated, err)
			}
			got, readErr := io.ReadAll(io.LimitReader(block.output, int64(size)+1))
			block.release()
			if readErr != nil || !bytes.Equal(got, plain) || reader.activeBlocks != 0 {
				t.Fatalf("pinned decoder size=%d repeated=%t active=%d: %v", size, repeated, reader.activeBlocks, readErr)
			}
		}
	}
}

// Generic Snappy permits this copy, but the owned pinned encoder cannot emit
// it. Refusal is explicit and does not allocate its full backreference range.
func TestAttemptAdmissionSnappyRefusesGenericLongDistance(t *testing.T) {
	t.Parallel()
	plain := bytes.Repeat([]byte{'x'}, attemptAdmissionWindow)
	encoded := binary.AppendUvarint(nil, uint64(len(plain)+1))
	encoded = append(encoded, 61<<2, 0xff, 0xff)
	encoded = append(encoded, plain...)
	encoded = append(encoded, 3, 0, 0, 1, 0)
	native, err := snappy.Decode(nil, encoded)
	if err != nil || len(native) != len(plain)+1 || native[len(plain)] != 'x' {
		t.Fatalf("generic reference is not genuinely valid Snappy: %v", err)
	}
	reader, file, handle := newAttemptAdmissionBlockTestFile(t, context.Background(), encoded, 1)
	block, err := reader.block(file, handle)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, block.output)
	block.release()
	if err == nil || !strings.Contains(err.Error(), "unsupported Snappy copy distance") || reader.activeBlocks != 0 {
		t.Fatalf("unsupported producer window was accepted: %v", err)
	}
}

// The checksum is independently correct for each malformed payload; a CRC
// failure cannot accidentally stand in for the intended framing refusal.
func TestAttemptAdmissionSnappyRejectsMalformedFraming(t *testing.T) {
	t.Parallel()
	variants := [][]byte{
		{0x80},                      // truncated decoded length
		{1, 4, 'x', 'y'},            // literal exceeds declared length
		{1, 2, 0, 0},                // zero-distance copy
		{1, 2, 1, 0},                // copy before any decoded byte
		{1, 0, 'x', 0},              // trailing encoded tag
		{2, 0, 'x'},                 // short decoded body
		{1, 0xfc, 0xff, 0xff, 0xff}, // truncated four-byte literal length
	}
	for index, encoded := range variants {
		reader, file, handle := newAttemptAdmissionBlockTestFile(t, context.Background(), encoded, 1)
		block, err := reader.block(file, handle)
		if err == nil {
			_, err = io.Copy(io.Discard, block.output)
			block.release()
		}
		if err == nil || reader.activeBlocks != 0 {
			t.Fatalf("malformed payload %d was accepted or leaked its slot: %v", index, err)
		}
	}
}

// Correct framing with an independently wrong CRC must fail checksum itself,
// not an inode-change guard or an unrelated decoded-format prerequisite.
func TestAttemptAdmissionBlockChecksPhysicalChecksum(t *testing.T) {
	t.Parallel()
	raw := append([]byte("actual framed bytes"), 0)
	var checksum [4]byte
	binary.LittleEndian.PutUint32(checksum[:], util.NewCRC(raw).Value()^1)
	raw = append(raw, checksum[:]...)
	reader, file, handle := newAttemptAdmissionBlockTestRaw(t, context.Background(), raw)
	block, err := reader.block(file, handle)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, block.output)
	block.release()
	if err == nil || !strings.Contains(err.Error(), "block checksum differs") || reader.activeBlocks != 0 {
		t.Fatalf("physical CRC mismatch did not reach its own boundary: %v", err)
	}
}

// A canceled decoder cannot produce a further byte or retain a pool slot.
func TestAttemptAdmissionBlockCancellationReleasesOwnership(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, file, handle := newAttemptAdmissionBlockTestFile(t, ctx, snappy.Encode(nil, bytes.Repeat([]byte{'a'}, 3*attemptAdmissionWindow)), 1)
	block, err := reader.block(file, handle)
	if err != nil {
		t.Fatal(err)
	}
	var first [1]byte
	if _, err := block.Read(first[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	n, err := block.Read(first[:])
	block.release()
	if n != 0 || !errors.Is(err, context.Canceled) || reader.activeBlocks != 0 {
		t.Fatalf("canceled decoder produced bytes or leaked ownership: n=%d active=%d err=%v", n, reader.activeBlocks, err)
	}
}

// A failed pre-close observer cannot prevent closing the real descriptor.
func TestAttemptAdmissionFileCloseStillClosesAfterHookFailure(t *testing.T) {
	t.Parallel()
	reader, file, _ := newAttemptAdmissionBlockTestFile(t, context.Background(), []byte{'x'}, 0)
	failure := errors.New("checked file pre-close sentinel")
	reader.disk.hooks.Step = func(operation, name string) error {
		if operation == "admission-before-close" {
			return failure
		}
		return nil
	}
	err := file.Close()
	_, statErr := file.file.Stat()
	if !errors.Is(err, failure) || !errors.Is(statErr, os.ErrClosed) {
		t.Fatalf("pre-close failure suppressed actual descriptor close: close=%v stat=%v", err, statErr)
	}
}

// Restart offsets point to real unshared entry starts, not merely in-range
// integers. Every payload has a separately correct physical CRC.
func TestAttemptAdmissionBlockChecksRestartAndTypedBounds(t *testing.T) {
	t.Parallel()
	valid := []byte{0, 1, 1, 'a', 'x', 0, 0, 0, 0, 1, 0, 0, 0}
	variants := []struct {
		name string
		raw  []byte
		good bool
	}{
		{name: "valid", raw: valid, good: true},
		{name: "empty", raw: []byte{0, 0, 0, 0, 1, 0, 0, 0}, good: true},
		{name: "nonzero-first", raw: []byte{0, 1, 1, 'a', 'x', 1, 0, 0, 0, 1, 0, 0, 0}},
		{name: "zero-count", raw: []byte{0, 1, 1, 'a', 'x', 0, 0, 0, 0, 0, 0, 0, 0}},
		{name: "excessive-key", raw: []byte{0, 2, 0, 'a', 'b', 0, 0, 0, 0, 1, 0, 0, 0}},
		{name: "excessive-value", raw: []byte{0, 1, 2, 'a', 'x', 'y', 0, 0, 0, 0, 1, 0, 0, 0}},
		{name: "restart-inside-entry", raw: []byte{0, 1, 1, 'a', 'x', 0, 1, 1, 'b', 'y', 0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0}},
	}
	for _, variant := range variants {
		reader, file, handle := newAttemptAdmissionBlockTestFile(t, context.Background(), variant.raw, 0)
		err := reader.walkBlock(file, handle, 1, 1, false, nil)
		if (err == nil) != variant.good || reader.activeBlocks != 0 {
			t.Fatalf("restart variant %s good=%t active=%d: %v", variant.name, variant.good, reader.activeBlocks, err)
		}
	}
}

// Repeated table passes reuse all fixed windows and both fixed I/O buffers.
func TestAttemptAdmissionBlockPoolIsHistoryIndependent(t *testing.T) {
	t.Parallel()
	reader, file, handle := newAttemptAdmissionBlockTestFile(t, context.Background(), snappy.Encode(nil, bytes.Repeat([]byte{'b'}, 2*attemptAdmissionWindow)), 1)
	for pass := 0; pass < 16; pass++ {
		var blocks [4]*attemptStoreAdmissionBlock
		for index := range blocks {
			var err error
			blocks[index], err = reader.block(file, handle)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := reader.block(file, handle); err == nil {
			t.Fatal("fifth simultaneous block owner was admitted")
		}
		for _, block := range blocks {
			if _, err := io.Copy(io.Discard, block.output); err != nil {
				t.Fatal(err)
			}
			block.release()
			if block.input.Size() != attemptAdmissionBuffer || block.output.Size() != attemptAdmissionBuffer || len(block.window) != attemptAdmissionWindow {
				t.Fatal("decoder buffers grew with decoded history")
			}
		}
	}
	if reader.maximumBlocks != 4 || reader.activeBlocks != 0 || reader.decodedBytes != 16*4*2*attemptAdmissionWindow {
		t.Fatalf("pool work/ownership census differs: max=%d active=%d bytes=%d", reader.maximumBlocks, reader.activeBlocks, reader.decodedBytes)
	}
}

// A step that began during opening must recheck the current lifecycle after
// an external hook, not a stale caller context captured before publication.
func TestAttemptAdmissionCompletedOpeningDoesNotCancelQueuedStep(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, _, _ := newAttemptAdmissionBlockTestFile(t, ctx, []byte{'x'}, 0)
	if err := reader.disk.promoteInspection(); err != nil {
		t.Fatal(err)
	}
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	reader.disk.hooks.Step = func(operation, name string) error {
		if operation == "queued-opening-step" {
			close(reached)
			<-release
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- reader.disk.step("queued-opening-step", "") }()
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			<-done
		}
	})
	select {
	case <-reached:
	case err := <-done:
		joined = true
		t.Fatalf("queued step missed its actual boundary: %v", err)
	}
	if err := reader.disk.finishOpening(); err != nil {
		t.Fatal(err)
	}
	cancel()
	unblock()
	err := <-done
	joined = true
	if err != nil {
		t.Fatalf("published owner inherited stale opening cancellation: %v", err)
	}
}
