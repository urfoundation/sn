//go:build linux

// Linux journal sockets use per-call nonblocking writes. Pipe/terminal handles
// are reopened with independent flags; the caller's original fd never changes.
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Only the single exporter worker writes or closes this private descriptor.
type descriptorSink struct {
	fd     int
	socket bool
}

// Admission holds the original File's descriptor reference while inspecting
// and duplicating it, preventing close/reuse from selecting an unrelated sink.
func newDescriptorSink(file *os.File) (ownedSink, error) {
	if file == nil {
		return nil, errors.New("diagnostic file is absent")
	}
	connection, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	var result ownedSink
	var admissionErr error
	err = connection.Control(func(raw uintptr) {
		fd := int(raw)
		var original unix.Stat_t
		if admissionErr = unix.Fstat(fd, &original); admissionErr != nil {
			return
		}
		switch original.Mode & unix.S_IFMT {
		case unix.S_IFSOCK:
			var kind int
			kind, admissionErr = unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
			if admissionErr != nil {
				return
			}
			if kind != unix.SOCK_STREAM {
				admissionErr = errors.New("diagnostic socket is not a stream")
				return
			}
			var owned int
			owned, admissionErr = unix.FcntlInt(raw, unix.F_DUPFD_CLOEXEC, 0)
			if admissionErr == nil {
				result = &descriptorSink{fd: owned, socket: true}
			}
			return
		case unix.S_IFCHR:
			if uint64(original.Rdev) == unix.Mkdev(1, 3) {
				result = &memorySink{discarded: true}
				return
			}
			if _, admissionErr = unix.IoctlGetTermios(fd, unix.TCGETS); admissionErr != nil {
				return
			}
		case unix.S_IFIFO:
		default:
			admissionErr = errors.New("diagnostic file cannot promise nonblocking writes")
			return
		}
		var owned int
		owned, admissionErr = unix.Open(fmt.Sprintf("/proc/self/fd/%d", fd), unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if admissionErr != nil {
			return
		}
		var current unix.Stat_t
		admissionErr = unix.Fstat(owned, &current)
		if admissionErr == nil && (original.Dev != current.Dev || original.Ino != current.Ino || original.Mode != current.Mode || original.Rdev != current.Rdev) {
			admissionErr = errors.New("diagnostic descriptor identity changed")
		}
		if admissionErr != nil {
			admissionErr = errors.Join(admissionErr, unix.Close(owned))
			return
		}
		result = &descriptorSink{fd: owned}
	})
	if err = errors.Join(err, admissionErr); err != nil {
		if result != nil {
			err = errors.Join(err, result.Close())
		}
		return nil, err
	}
	return result, nil
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
			count, err = unix.SendmsgN(self.fd, raw[written:], nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
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
