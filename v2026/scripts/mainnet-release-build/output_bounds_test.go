// Copy and actual command-pipe regressions keep metadata bounds on Write.
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strconv"
	"testing"
	"time"
)

// Hiding the source's WriterTo selects the destination's ReaderFrom whenever
// it is accidentally promoted, exactly the faulty fast-path capability.
func TestReleaseBuildBufferCopyRejectsExcessMetadata(t *testing.T) {
	output := &buildBuffer{limit: 8}
	input := struct{ io.Reader }{Reader: bytes.NewReader([]byte("123456789"))}
	n, err := io.Copy(output, input)
	if err == nil || n > 8 || output.Len() > 8 {
		t.Fatal("copy bypassed the metadata write bound", n, output.Len(), err)
	}
}

// The exact boundary remains admissible through the same copy dispatch.
func TestReleaseBuildBufferCopyPreservesExactLimit(t *testing.T) {
	output := &buildBuffer{limit: 8}
	input := struct{ io.Reader }{Reader: bytes.NewReader([]byte("12345678"))}
	if n, err := io.Copy(output, input); err != nil || n != 8 || string(output.Bytes()) != "12345678" {
		t.Fatal("copy changed exact bounded metadata", n, string(output.Bytes()), err)
	}
}

// This finite child exists only for the selected test binary's pipe fixture.
// An ordinary invocation returns without spawning work or skipping a test.
func TestReleaseBuildQueryOutputChild(t *testing.T) {
	raw := os.Getenv("URNETWORK_TEST_BUILD_QUERY_OUTPUT_BYTES")
	if raw == "" {
		return
	}
	remaining, err := strconv.Atoi(raw)
	if err != nil || remaining < 1 || remaining > 16*1024*1024+1 {
		os.Exit(2)
	}
	chunk := bytes.Repeat([]byte{0x62}, 32*1024)
	for remaining > 0 {
		n, err := os.Stdout.Write(chunk[:min(remaining, len(chunk))])
		if err != nil {
			os.Exit(0)
		}
		remaining -= n
	}
	os.Exit(0)
}

// Exercise os/exec's real stdout pipe, not only a direct writer call.
func TestReleaseBuildQueryRejectsOversizedChildMetadata(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	t.Setenv("URNETWORK_TEST_BUILD_QUERY_OUTPUT_BYTES", "16777217")
	output, err := buildQuery(ctx, t.TempDir(), os.Environ(), executable, "-test.run=^TestReleaseBuildQueryOutputChild$")
	if err == nil || output != nil {
		t.Fatal("actual child pipe bypassed the metadata output bound", len(output), err)
	}
}

// A small successful child remains usable after the output wrapper changes.
func TestReleaseBuildQueryPreservesBoundedChildMetadata(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	t.Setenv("URNETWORK_TEST_BUILD_QUERY_OUTPUT_BYTES", "8")
	output, err := buildQuery(ctx, t.TempDir(), os.Environ(), executable, "-test.run=^TestReleaseBuildQueryOutputChild$")
	if err != nil || !bytes.Equal(output, bytes.Repeat([]byte{0x62}, 8)) {
		t.Fatal("actual child pipe changed bounded metadata", string(output), err)
	}
}
