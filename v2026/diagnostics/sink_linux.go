//go:build linux

// Linux journal sockets use per-call nonblocking writes. Pipe/terminal handles
// are reopened with independent flags; the caller's original fd never changes.
// Regular files (nohup, systemd file:, CI captures) use the storage-bounded
// file adapter shared with other platforms.
package diagnostics

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

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
		case unix.S_IFREG:
			result, admissionErr = newFileSink(raw)
			return
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

// Linux suppresses SIGPIPE per call; the private fd is never the original 1/2.
const socketSendFlags = unix.MSG_DONTWAIT | unix.MSG_NOSIGNAL
