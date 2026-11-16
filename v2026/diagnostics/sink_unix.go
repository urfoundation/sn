//go:build linux || darwin

// Unix descriptor adapters shared by the Linux and macOS admission rules. Each
// adapter owns a private descriptor; the caller's original fd is never closed.
package diagnostics

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// Only the single exporter worker writes or closes this private descriptor.
type descriptorSink struct {
	fd     int
	socket bool
}

// Every syscall is nonblocking. Short writes retain only this record's suffix;
// the deadline/cancellation returns the exact prefix count to prevent splicing.
func (self *descriptorSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	written := 0
	for written < len(raw) {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		var count int
		var err error
		if self.socket {
			count, err = unix.SendmsgN(self.fd, raw[written:], nil, nil, socketSendFlags)
		} else {
			count, err = unix.Write(self.fd, raw[written:])
		}
		if count > 0 {
			written += count
		}
		if err == nil && count > 0 {
			continue
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
			return written, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return written, ctx.Err()
		case <-timer.C:
		}
	}
	return written, nil
}

// The exporter joins its write before releasing this owned descriptor only.
func (self *descriptorSink) Close() error { return unix.Close(self.fd) }

// A regular file has no peer that can stall a write: each bounded record write
// completes against local storage, so cancellation is checked between chunks.
// The duplicate shares the caller's offset, so records interleave at record
// boundaries with the caller's own writes instead of overwriting them.
type fileSink struct{ fd int }

// Called with the original descriptor held by the caller's SyscallConn.
func newFileSink(raw uintptr) (ownedSink, error) {
	owned, err := unix.FcntlInt(raw, unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return &fileSink{fd: owned}, nil
}

// Records are at most MaximumRecordBytes; a short write resumes at its suffix.
func (self *fileSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	written := 0
	for written < len(raw) {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		count, err := unix.Write(self.fd, raw[written:])
		if count > 0 {
			written += count
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, errors.New("diagnostic file accepted no bytes")
		}
	}
	return written, nil
}

// The exporter joins its write before releasing this owned descriptor only.
func (self *fileSink) Close() error { return unix.Close(self.fd) }
