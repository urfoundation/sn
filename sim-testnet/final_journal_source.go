// Final capture owns a finite journal image independently of ordinary proof
// files. Streaming reads preserve its exact raw bytes and the existing schema.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const finalJournalReadChunkBytes = 64 * 1024

// Hooks expose actual read boundaries in deterministic source-integrity tests.
type finalJournalReadHooks struct {
	read     func(io.Reader, []byte) (int, error)
	captured func()
}

// Only the fixed journal slot receives this capacity. Callers still decode the
// complete hash chain before using any entry as deployment or relay authority.
func readFinalJournalSourceContext(ctx context.Context, stateRoot string) ([]byte, error) {
	return readFinalJournalSourceWithHooks(ctx, stateRoot, maximumFinalJournalBytes, finalJournalReadHooks{})
}

// Capture the initial finite descriptor range. Later appends do not replace the
// selected image; rewrites, truncation, unsafe paths and replacement still fail.
func readFinalJournalSourceWithHooks(ctx context.Context, stateRoot string, maximum int64, hooks finalJournalReadHooks) (raw []byte, resultErr error) {
	if ctx == nil || maximum <= 0 || maximum > maximumFinalJournalBytes {
		return nil, errors.New("final journal read context or capacity is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := openFinalCollectedFile(stateRoot, "journal.jsonl")
	if err != nil {
		return nil, fmt.Errorf("open final journal source: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			raw = nil
		}
	}()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximum {
		return nil, errors.New("final journal source is empty, nonregular or exceeds its dedicated capacity")
	}
	var body bytes.Buffer
	body.Grow(int(before.Size()))
	digest := sha256.New()
	if err := streamFinalJournalRange(ctx, io.MultiWriter(&body, digest), file, before.Size(), hooks.read); err != nil {
		return nil, fmt.Errorf("read final journal source: %w", err)
	}
	if body.Bytes()[body.Len()-1] != '\n' {
		return nil, errors.New("final journal source does not end at a durable record boundary")
	}
	if hooks.captured != nil {
		hooks.captured()
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	confirmed := sha256.New()
	if err := streamFinalJournalRange(ctx, confirmed, file, before.Size(), hooks.read); err != nil {
		return nil, fmt.Errorf("confirm final journal source: %w", err)
	}
	if !bytes.Equal(digest.Sum(nil), confirmed.Sum(nil)) {
		return nil, errors.New("final journal source bytes changed while reading")
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || after.Size() < before.Size() || after.Size() == before.Size() && !sameFinalCollectedFileState(before, after) {
		return nil, errors.New("final journal source changed or was truncated while reading")
	}
	current, err := openFinalCollectedFile(stateRoot, "journal.jsonl")
	if err != nil {
		return nil, fmt.Errorf("confirm final journal source path: %w", err)
	}
	currentInfo, statErr := current.Stat()
	if err := errors.Join(statErr, current.Close()); err != nil {
		return nil, err
	}
	if !os.SameFile(before, currentInfo) {
		return nil, errors.New("final journal source path was replaced while reading")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

// The only allocations are the explicitly bounded image and this fixed buffer;
// a short source or an owner cancellation never returns a partial commitment.
func streamFinalJournalRange(ctx context.Context, target io.Writer, source io.Reader, remaining int64, read func(io.Reader, []byte) (int, error)) error {
	if read == nil {
		read = io.ReadFull
	}
	buffer := make([]byte, finalJournalReadChunkBytes)
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := buffer[:min(int64(len(buffer)), remaining)]
		n, err := read(source, chunk)
		if err != nil {
			return err
		}
		if n != len(chunk) {
			return io.ErrUnexpectedEOF
		}
		written, err := target.Write(chunk)
		if err != nil {
			return err
		}
		if written != len(chunk) {
			return io.ErrShortWrite
		}
		remaining -= int64(n)
	}
	return ctx.Err()
}

// The foundation retains its original logical path and raw content hash.
func finalCollectedJournalEntryContext(ctx context.Context, stateRoot string) (FinalCollectedFileBundleEntry, error) {
	raw, err := readFinalJournalSourceContext(ctx, stateRoot)
	if err != nil {
		return FinalCollectedFileBundleEntry{}, err
	}
	if _, err := decodeFinalSemanticJournalBytes(raw); err != nil {
		return FinalCollectedFileBundleEntry{}, err
	}
	if err := ctx.Err(); err != nil {
		return FinalCollectedFileBundleEntry{}, err
	}
	return FinalCollectedFileBundleEntry{Path: "journal.jsonl", ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw)), Data: raw}, nil
}

// Only the exact relay journal source gets its dedicated content-addressed
// owner. Other capture bytes retain the existing generic proof path and cap.
func finalValidatorSourcePathV2(kind, name, origin, hash string) (string, error) {
	if !validSHA256String(hash) {
		return "", errors.New("compact source hash is malformed")
	}
	digest := strings.TrimPrefix(hash, "sha256:")
	if kind == "relay-journal" {
		if name != "journal.jsonl" || origin != "" {
			return "", errors.New("compact relay journal source has a foreign identity")
		}
		return "final-inputs/validators/v2/journals/" + digest + ".jsonl", nil
	}
	return "final-inputs/validators/v2/" + digest + ".bin", nil
}
