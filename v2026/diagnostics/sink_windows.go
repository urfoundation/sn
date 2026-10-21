//go:build windows

// Windows admits disk files directly. Console and synchronous pipe handles
// block without a context, so a dedicated writer thread owns them behind a
// bounded queue and Close ends its in-flight write with CancelSynchronousIo.
package diagnostics

import (
	"context"
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileModeInformationClass = 16

var procCancelSynchronousIo = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

// A disk file has no peer that can stall a write; the private duplicate
// shares the caller's file position and is closed only by the exporter.
type fileSink struct{ handle windows.Handle }

// Admission holds the original File's handle reference while duplicating it.
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
		original := windows.Handle(raw)
		var kind uint32
		if kind, admissionErr = windows.GetFileType(original); admissionErr != nil {
			return
		}
		switch kind {
		case windows.FILE_TYPE_DISK, windows.FILE_TYPE_CHAR:
		case windows.FILE_TYPE_PIPE:
			// An overlapped pipe has no synchronous write to cancel.
			if !synchronousHandle(original) {
				admissionErr = errors.New("diagnostic pipe is not synchronous")
				return
			}
		default:
			admissionErr = errors.New("diagnostic handle cannot promise a cancellable write")
			return
		}
		process := windows.CurrentProcess()
		var owned windows.Handle
		if admissionErr = windows.DuplicateHandle(process, original, process, &owned, 0, false, windows.DUPLICATE_SAME_ACCESS); admissionErr != nil {
			return
		}
		if kind == windows.FILE_TYPE_DISK {
			result = &fileSink{handle: owned}
			return
		}
		result, admissionErr = newQueuedSink(&deviceWriter{handle: owned})
	})
	if err = errors.Join(err, admissionErr); err != nil {
		if result != nil {
			err = errors.Join(err, result.Close())
		}
		return nil, err
	}
	return result, nil
}

// Records are at most MaximumRecordBytes; a short write resumes at its suffix.
func (self *fileSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	written := 0
	for written < len(raw) {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		var count uint32
		err := windows.WriteFile(self.handle, raw[written:], &count, nil)
		written += int(count)
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, errors.New("diagnostic file accepted no bytes")
		}
	}
	return written, nil
}

// The exporter joins its write before releasing this owned handle only.
func (self *fileSink) Close() error { return windows.CloseHandle(self.handle) }

// Synchronous handles report FILE_SYNCHRONOUS_IO_(NON)ALERT in their mode.
func synchronousHandle(handle windows.Handle) bool {
	var status windows.IO_STATUS_BLOCK
	var mode uint32
	if windows.NtQueryInformationFile(handle, &status, (*byte)(unsafe.Pointer(&mode)), uint32(unsafe.Sizeof(mode)), fileModeInformationClass) != nil {
		return false
	}
	return mode&(windows.FILE_SYNCHRONOUS_IO_ALERT|windows.FILE_SYNCHRONOUS_IO_NONALERT) != 0
}

// A console or synchronous pipe, written only from the queue's locked thread.
// The thread handle exists so Close can cancel that thread's blocked write.
type deviceWriter struct {
	handle windows.Handle
	thread windows.Handle
}

func (self *deviceWriter) begin() error {
	thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
	if err != nil {
		return err
	}
	self.thread = thread
	return nil
}

func (self *deviceWriter) write(raw []byte) (int, error) {
	var count uint32
	err := windows.WriteFile(self.handle, raw, &count, nil)
	return int(count), err
}

// ERROR_NOT_FOUND between writes is expected; the queue retries until joined.
func (self *deviceWriter) interrupt() {
	_, _, _ = procCancelSynchronousIo.Call(uintptr(self.thread))
}

// The original handle stays open; only the duplicate and thread are released.
func (self *deviceWriter) close() error {
	var err error
	if self.thread != 0 {
		err = windows.CloseHandle(self.thread)
	}
	return errors.Join(err, windows.CloseHandle(self.handle))
}
