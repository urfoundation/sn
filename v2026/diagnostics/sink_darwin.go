//go:build darwin

// macOS admits regular files and terminals. A terminal is reopened by its device
// path with independent nonblocking flags (darwin /dev/fd shares the caller's
// open file description, so it cannot be used). Anonymous pipes and sockets have
// no independent reopen path and remain refused.
package diagnostics

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Darwin has no MSG_NOSIGNAL; sockets are never admitted here.
const socketSendFlags = unix.MSG_DONTWAIT

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
		var original unix.Stat_t
		if admissionErr = unix.Fstat(int(raw), &original); admissionErr != nil {
			return
		}
		switch original.Mode & unix.S_IFMT {
		case unix.S_IFREG:
			result, admissionErr = newFileSink(raw)
		case unix.S_IFCHR:
			result, admissionErr = newTerminalSink(raw, &original)
		default:
			admissionErr = errors.New("diagnostic descriptor cannot be reopened with independent nonblocking flags")
		}
	})
	if err = errors.Join(err, admissionErr); err != nil {
		if result != nil {
			err = errors.Join(err, result.Close())
		}
		return nil, err
	}
	return result, nil
}

// /dev/null discards; any other character device must be a terminal whose
// reopened path names the same device.
func newTerminalSink(raw uintptr, original *unix.Stat_t) (ownedSink, error) {
	var null unix.Stat_t
	if unix.Stat("/dev/null", &null) == nil && original.Rdev == null.Rdev {
		return &memorySink{discarded: true}, nil
	}
	if _, err := unix.IoctlGetTermios(int(raw), unix.TIOCGETA); err != nil {
		return nil, err
	}
	buffer := make([]byte, unix.PathMax)
	_, err := unix.FcntlInt(raw, unix.F_GETPATH, int(uintptr(unsafe.Pointer(&buffer[0]))))
	runtime.KeepAlive(buffer)
	if err != nil {
		return nil, err
	}
	path := string(buffer[:max(bytes.IndexByte(buffer, 0), 0)])
	owned, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	var current unix.Stat_t
	err = unix.Fstat(owned, &current)
	if err == nil && (original.Dev != current.Dev || original.Ino != current.Ino || original.Mode != current.Mode || original.Rdev != current.Rdev) {
		err = errors.New("diagnostic descriptor identity changed")
	}
	if err != nil {
		return nil, errors.Join(err, unix.Close(owned))
	}
	return &descriptorSink{fd: owned}, nil
}
