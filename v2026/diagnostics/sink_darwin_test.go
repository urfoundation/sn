//go:build darwin

// A macOS console is a pseudo-terminal; records must reach its reader.
package diagnostics

import (
	"bytes"
	"io"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Opens a pty pair; the master is read only after the exporter joins.
func openTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetInt(master, unix.TIOCPTYGRANT, 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetInt(master, unix.TIOCPTYUNLK, 0); err != nil {
		t.Fatal(err)
	}
	name := make([]byte, 128)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(master), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		t.Fatal(errno)
	}
	slave, err := os.OpenFile(string(name[:bytes.IndexByte(name, 0)]), os.O_WRONLY|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	reader := os.NewFile(uintptr(master), "synthetic-master")
	t.Cleanup(func() { slave.Close(); reader.Close() })
	return reader, slave
}

// The record reaches the terminal and the caller's descriptor flags are kept.
func TestExporterDeliversToTerminal(t *testing.T) {
	reader, terminal := openTerminal(t)
	before, err := unix.FcntlInt(terminal.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := New(t.Context(), terminal, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Offer("alpha", []byte("bounded record\n")) {
		t.Fatal("terminal refused", owner.Snapshot("alpha"))
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if state := owner.Snapshot("alpha"); state.Delivered != 1 {
		t.Fatal("terminal record not delivered", state)
	}
	after, err := unix.FcntlInt(terminal.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("original flags changed", before, after)
	}
	raw := make([]byte, len("bounded record"))
	if _, err := io.ReadFull(reader, raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "bounded record" {
		t.Fatalf("unexpected terminal output %q", raw)
	}
}

// An anonymous pipe has no independent reopen path and stays refused.
func TestExporterRefusesAnonymousPipe(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	owner, err := New(t.Context(), write, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if owner.Offer("alpha", []byte("record\n")) || owner.Snapshot("alpha").Outcome != "unavailable" {
		t.Fatal("anonymous pipe admitted")
	}
}
