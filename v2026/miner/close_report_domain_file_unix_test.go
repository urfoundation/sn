//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// Real kernel special files exercise optional input admission without a writer.
package miner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The FIFO has no writer for the entire bounded call. A failed implementation
// is unblocked for cleanup only after its own deadline diagnostic is retained.
func TestProviderCloseReportFifoWithoutWriterNeverBlocksProviding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domain-fifo")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		domain [32]byte
		err    error
	}
	finished := make(chan result, 1)
	go func() {
		domain, err := readProviderCloseReportDomain(path)
		finished <- result{domain: domain, err: err}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	select {
	case value := <-finished:
		if value.domain != ([32]byte{}) || value.err == nil || !strings.Contains(value.err.Error(), "bounded regular file") {
			t.Fatal("nonregular optional domain acquired authority", value)
		}
	case <-ctx.Done():
		// Opening both ends never waits. Closing this owned descriptor releases the
		// buggy blocking reader; the timeout remains a failure, never a success proof.
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			_ = unix.Close(fd)
		}
		join, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case <-finished:
		case <-join.Done():
			t.Error("refused FIFO reader did not join")
		}
		t.Fatal("optional FIFO blocked provider setup without a writer")
	}
}

// Public domain files need no private permissions. Directories, devices and
// symlink aliases remain unsigned, including a symlink to a legitimate document.
func TestProviderCloseReportNonregularAndSymlinkInputsStayUnsigned(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "public-domain.json")
	domain := providerCloseDomainFixture()
	raw, err := json.Marshal(domain)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	want, _ := domain.Digest()
	if got, err := readProviderCloseReportDomain(path); err != nil || got != want {
		t.Fatal("public regular domain required secret-file authority", got, err)
	}
	alias := filepath.Join(directory, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{directory, alias, "/dev/null"} {
		if got, err := readProviderCloseReportDomain(bad); err == nil || got != ([32]byte{}) {
			t.Fatal("special or symbolic optional source acquired signing authority", bad, got, err)
		}
	}
}

// Complete document and bounded admission are required even for readable regular
// files; neither a valid prefix nor duplicated field can authorize a domain.
func TestProviderCloseReportRegularInputRequiresCompleteDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domain.json")
	raw, err := json.Marshal(providerCloseDomainFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte(nil), raw...), []byte(` {}`)...), []byte(`{"chain_id":945,"chain_id":945}`), []byte(strings.Repeat(" ", 16*1024+1))} {
		if err := os.WriteFile(path, bad, 0644); err != nil {
			t.Fatal(err)
		}
		if got, err := readProviderCloseReportDomain(path); err == nil || got != ([32]byte{}) {
			t.Fatal("incomplete or over-capacity public domain acquired authority", got, err)
		}
	}
}
