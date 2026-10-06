//go:build linux

// The largest accepted history must be read before the first public sample.
// Real access events expose progress without skipping hash or custody checks;
// the public sample still proves that all admission and chain checks finished.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type monitorNativeAdmissionObservation struct {
	count int
	err   error
}

// The reader has one owner and joins on every return. A 300s bound is only a
// liveness backstop; readiness is the positive access census, never elapsed time.
func monitorNativeObserveAdmission(t *testing.T, references []monitorHistoryReference) func(*monitorEconomicTestRun) {
	t.Helper()
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "synthetic-native-admission-events")
	watchNames := map[int]string{}
	for _, reference := range references {
		watch, err := unix.InotifyAddWatch(fd, reference.Path, unix.IN_ACCESS|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_ATTRIB)
		if err != nil {
			t.Fatal(errors.Join(err, file.Close()))
		}
		if _, present := watchNames[watch]; present {
			t.Fatal(errors.Join(errors.New("archive admission watches alias one inode"), file.Close()))
		}
		watchNames[watch] = reference.Path
	}
	observations := make(chan monitorNativeAdmissionObservation, len(references)+1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		seen := map[int]bool{}
		buffer := make([]byte, 64*1024)
		for {
			n, err := file.Read(buffer)
			if err != nil {
				observations <- monitorNativeAdmissionObservation{count: len(seen), err: err}
				return
			}
			for offset := 0; offset < n; {
				if n-offset < unix.SizeofInotifyEvent {
					observations <- monitorNativeAdmissionObservation{count: len(seen), err: errors.New("partial inotify event")}
					return
				}
				watch := int(int32(binary.NativeEndian.Uint32(buffer[offset : offset+4])))
				mask := binary.NativeEndian.Uint32(buffer[offset+4 : offset+8])
				size := int(binary.NativeEndian.Uint32(buffer[offset+12 : offset+16]))
				if size > n-offset-unix.SizeofInotifyEvent {
					observations <- monitorNativeAdmissionObservation{count: len(seen), err: errors.New("oversized inotify event")}
					return
				}
				offset += unix.SizeofInotifyEvent + size
				if err := monitorNativeAdmissionWatchStatus(watch, mask, watchNames); err != nil {
					observations <- monitorNativeAdmissionObservation{count: len(seen), err: err}
					return
				}
				if mask&unix.IN_ACCESS != 0 && !seen[watch] {
					seen[watch] = true
					observations <- monitorNativeAdmissionObservation{count: len(seen)}
					if len(seen) == len(references) {
						return
					}
				}
			}
		}
	}()
	closed := false
	closeObserver := func() {
		if closed {
			return
		}
		closed = true
		if err := monitorNativeAdmissionJoin(file, joined); err != nil {
			t.Error("archive admission observer cleanup failed", err)
		}
	}
	t.Cleanup(closeObserver)
	return func(run *monitorEconomicTestRun) {
		t.Helper()
		defer closeObserver()
		started := time.Now()
		count, err := monitorNativeWaitAdmission(t.Context(), observations, run.done, len(references))
		if err != nil {
			if errors.Is(err, errMonitorNativeAdmissionWorkerExited) {
				t.Fatal("public native worker returned before complete archive admission", count, len(references), run.exit, run.diagnostic.String())
			}
			t.Fatal("actual archive admission read failed", count, len(references), err)
		}
		t.Logf("actual native startup read all %d retained segments in %s before sample assertion", count, time.Since(started))
	}
}

var errMonitorNativeAdmissionWorkerExited = errors.New("public native worker returned before complete archive admission")

// Cancellation and early exit remain distinguishable from access-count progress.
func monitorNativeWaitAdmission(ctx context.Context, observations <-chan monitorNativeAdmissionObservation, done <-chan struct{}, maximum int) (int, error) {
	count := 0
	budget := time.After(300 * time.Second)
	for count < maximum {
		select {
		case observation := <-observations:
			count = observation.count
			if observation.err != nil {
				return count, observation.err
			}
		case <-done:
			return count, errMonitorNativeAdmissionWorkerExited
		case <-ctx.Done():
			return count, ctx.Err()
		case <-budget:
			return count, errors.New("archive admission progress budget exhausted")
		}
	}
	return count, nil
}

// Overflow and unknown/replaced inodes are rejected before any progress count.
func monitorNativeAdmissionWatchStatus(watch int, mask uint32, names map[int]string) error {
	if mask&(unix.IN_Q_OVERFLOW|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_IGNORED) != 0 {
		return fmt.Errorf("archive custody access census invalidated: %x", mask)
	}
	if _, present := names[watch]; !present {
		return errors.New("unknown archive access watch")
	}
	return nil
}

// Cleanup always joins even when descriptor close reports an error.
func monitorNativeAdmissionJoin(file io.Closer, joined <-chan struct{}) error {
	err := file.Close()
	<-joined
	return err
}

func TestMonitorNativeAdmissionRefusesLostAndUnknownWatchEvents(t *testing.T) {
	names := map[int]string{1: "synthetic-original"}
	if err := monitorNativeAdmissionWatchStatus(1, unix.IN_ACCESS, names); err != nil {
		t.Fatal("positive access refused", err)
	}
	for _, mask := range []uint32{unix.IN_Q_OVERFLOW, unix.IN_DELETE_SELF, unix.IN_MOVE_SELF, unix.IN_IGNORED} {
		if err := monitorNativeAdmissionWatchStatus(1, mask|unix.IN_ACCESS, names); err == nil {
			t.Fatal("lost custody counted as progress", mask)
		}
	}
	if err := monitorNativeAdmissionWatchStatus(2, unix.IN_ACCESS, names); err == nil {
		t.Fatal("foreign inode counted as progress")
	}
}

// The close callback releases an actually blocked pipe reader. Its causal
// cleanup error must survive while the reader's completion is joined.
type monitorNativeAdmissionCloseFailure struct {
	reader *io.PipeReader
	cause  error
}

func (self monitorNativeAdmissionCloseFailure) Close() error {
	return errors.Join(self.reader.Close(), self.cause)
}

func TestMonitorNativeAdmissionCleanupJoinsAndRetainsCloseFailure(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	})
	entered, joined := make(chan struct{}), make(chan struct{})
	readResult := make(chan error, 1)
	go func() {
		defer close(joined)
		close(entered)
		var raw [1]byte
		_, err := reader.Read(raw[:])
		readResult <- err
	}()
	<-entered
	cause := errors.New("synthetic cleanup acknowledgement failure")
	if err := monitorNativeAdmissionJoin(monitorNativeAdmissionCloseFailure{reader: reader, cause: cause}, joined); !errors.Is(err, cause) {
		t.Fatal("cleanup failure discarded", err)
	}
	if err := <-readResult; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("cleanup did not cancel and join original reader", err)
	}
}

func TestMonitorNativeAdmissionCancellationCannotBecomeReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	count, err := monitorNativeWaitAdmission(ctx, make(chan monitorNativeAdmissionObservation), make(chan struct{}), 512)
	if count != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation became complete admission", count, err)
	}
}

func TestMonitorNativeAdmissionEarlyExitCannotBecomeReadiness(t *testing.T) {
	done := make(chan struct{})
	close(done)
	count, err := monitorNativeWaitAdmission(t.Context(), make(chan monitorNativeAdmissionObservation), done, 512)
	if count != 0 || !errors.Is(err, errMonitorNativeAdmissionWorkerExited) {
		t.Fatal("early worker exit became complete admission", count, err)
	}
}
