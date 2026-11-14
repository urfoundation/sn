//go:build linux

// Physical descriptor tests fill real pipes/sockets and preserve original flags.
package diagnostics

import (
	"context"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// The barrier observes entry into the real nonblocking descriptor adapter.
type observedDescriptor struct {
	ownedSink
	entered chan struct{}
}

// No result or physical write is replaced by the barrier.
func (self *observedDescriptor) WriteContext(ctx context.Context, raw []byte) (int, error) {
	close(self.entered)
	return self.ownedSink.WriteContext(ctx, raw)
}

// EAGAIN is an actual filled buffer boundary, not a timing assertion.
func fillDescriptor(t *testing.T, sink *descriptorSink) {
	t.Helper()
	raw := make([]byte, 4096)
	for {
		var err error
		if sink.socket {
			_, err = unix.SendmsgN(sink.fd, raw, nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		} else {
			_, err = unix.Write(sink.fd, raw)
		}
		if errors.Is(err, unix.EAGAIN) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// File.Fd may adjust Go's polling mode; inspect flags through its held raw
// descriptor so the test cannot repair a flag mutation before detecting it.
func descriptorFlags(t *testing.T, file *os.File) int {
	t.Helper()
	connection, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	var readErr error
	err = connection.Control(func(fd uintptr) { flags, readErr = unix.FcntlInt(fd, unix.F_GETFL, 0) })
	if err := errors.Join(err, readErr); err != nil {
		t.Fatal(err)
	}
	return flags
}

// Both supported daemon destinations interrupt their actual full-buffer write.
func TestExporterFullDescriptorsCancelAndPreserveOriginalFlags(t *testing.T) {
	for _, socket := range []bool{false, true} {
		var read, write *os.File
		if socket {
			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			read, write = os.NewFile(uintptr(fds[0]), "synthetic-reader"), os.NewFile(uintptr(fds[1]), "synthetic-writer")
		} else {
			var err error
			read, write, err = os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
		}
		before := descriptorFlags(t, write)
		owner, err := New(t.Context(), write, []string{"alpha"})
		if err != nil {
			t.Fatal(err)
		}
		sink, ok := owner.sink.(*descriptorSink)
		if !ok {
			t.Fatal("physical descriptor was not admitted")
		}
		fillDescriptor(t, sink)
		entered := make(chan struct{})
		owner.sink = &observedDescriptor{ownedSink: sink, entered: entered}
		owner.Offer("alpha", []byte("bounded record\n"))
		<-entered
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		after := descriptorFlags(t, write)
		if before != after {
			t.Fatal("original flags changed", before, after)
		}
		var stat unix.Stat_t
		if !errors.Is(unix.Fstat(sink.fd, &stat), unix.EBADF) {
			t.Fatal("private descriptor remains owned")
		}
		if state := owner.Snapshot("alpha"); state.Delivered != 0 || state.Dropped != 1 || state.Unavailable != 1 {
			t.Fatal("full destination claimed delivery", state)
		}
		if err := errors.Join(read.Close(), write.Close()); err != nil {
			t.Fatal(err)
		}
	}
}
