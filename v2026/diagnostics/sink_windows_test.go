//go:build windows

// A Windows provider in a console used to show nothing: console and pipe
// handles were refused. Records must reach a real console and pipe, and a
// pipe nobody reads must not stall Close.
package diagnostics

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")
var procAllocConsole = kernel32.NewProc("AllocConsole")
var procCreateConsoleScreenBuffer = kernel32.NewProc("CreateConsoleScreenBuffer")
var procReadConsoleOutputCharacterW = kernel32.NewProc("ReadConsoleOutputCharacterW")

// A private screen buffer is a real console handle that never shows on screen.
func openConsole(t *testing.T) *os.File {
	t.Helper()
	// Fails harmlessly when the test already has a console (an ssh conpty).
	procAllocConsole.Call()
	const consoleTextmodeBuffer = 1
	handle, _, err := procCreateConsoleScreenBuffer.Call(uintptr(windows.GENERIC_READ|windows.GENERIC_WRITE), uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE), 0, consoleTextmodeBuffer, 0)
	if windows.Handle(handle) == windows.InvalidHandle {
		t.Fatal("console screen buffer", err)
	}
	console := os.NewFile(handle, "synthetic-console")
	t.Cleanup(func() { console.Close() })
	return console
}

func TestExporterDeliversToConsole(t *testing.T) {
	console := openConsole(t)
	owner, err := New(t.Context(), console, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Offer("alpha", []byte("bounded record")) {
		t.Fatal("console refused", owner.Snapshot("alpha"))
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if state := owner.Snapshot("alpha"); state.Delivered != 1 || state.Dropped != 0 {
		t.Fatal("console record not delivered", state)
	}
	text := make([]uint16, len("bounded record"))
	var read uint32
	if ok, _, err := procReadConsoleOutputCharacterW.Call(console.Fd(), uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), 0, uintptr(unsafe.Pointer(&read))); ok == 0 {
		t.Fatal(err)
	}
	if got := windows.UTF16ToString(text[:read]); got != "bounded record" {
		t.Fatalf("unexpected console contents %q", got)
	}
	if _, err := console.Write([]byte(" caller")); err != nil {
		t.Fatal("caller handle closed by exporter", err)
	}
}

func TestExporterDeliversToPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	owner, err := New(t.Context(), writer, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Offer("alpha", []byte("bounded record\n")) {
		t.Fatal("pipe refused", owner.Snapshot("alpha"))
	}
	raw := make([]byte, len("bounded record\n"))
	if _, err := io.ReadFull(reader, raw); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "bounded record\n" || owner.Snapshot("alpha").Delivered != 1 {
		t.Fatalf("unexpected pipe output %q %v", raw, owner.Snapshot("alpha"))
	}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal("caller handle closed by exporter", err)
	}
}

// Nobody reads the pipe, so the device write blocks; Close must cancel it.
func TestExporterClosesBlockedPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	owner, err := New(t.Context(), writer, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	record := []byte(strings.Repeat("x", MaximumRecordBytes-1) + "\n")
	start := time.Now()
	for index := 0; index < 8; index++ {
		owner.Offer("alpha", record)
		owner.Offer("beta", record)
		time.Sleep(20 * time.Millisecond)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatal("offers waited on the pipe", elapsed)
	}
	start = time.Now()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatal("close did not cancel the blocked write", elapsed)
	}
	if state := owner.Snapshot("alpha"); state.Delivered == 0 {
		t.Fatal("no record reached the device queue", state)
	}
}
