// The device queue keeps the exporter's rules for a synchronous device: its
// write never waits on the device, overflow is counted, and Close interrupts
// and joins a blocked write before releasing the device.
package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// A device whose writes block until released or interrupted.
type stalledDevice struct {
	stateLock   sync.Mutex
	output      bytes.Buffer
	stalled     bool
	entered     chan struct{}
	release     chan struct{}
	interrupted chan struct{}
	cancelled   bool
	closed      bool
	writing     bool
}

func newStalledDevice(stalled bool) *stalledDevice {
	return &stalledDevice{stalled: stalled, entered: make(chan struct{}, 64), release: make(chan struct{}), interrupted: make(chan struct{})}
}

func (self *stalledDevice) begin() error { return nil }

func (self *stalledDevice) write(raw []byte) (int, error) {
	self.stateLock.Lock()
	stalled := self.stalled
	self.writing = true
	self.stateLock.Unlock()
	defer func() {
		self.stateLock.Lock()
		self.writing = false
		self.stateLock.Unlock()
	}()
	self.entered <- struct{}{}
	if stalled {
		select {
		case <-self.release:
		case <-self.interrupted:
			return 0, errors.New("operation aborted")
		}
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.output.Write(raw)
}

// Like CancelSynchronousIo, an interrupt only affects a write in progress.
func (self *stalledDevice) interrupt() {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.writing && !self.cancelled {
		self.cancelled = true
		close(self.interrupted)
	}
}

func (self *stalledDevice) close() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.writing {
		return errors.New("device closed during a write")
	}
	self.closed = true
	return nil
}

func TestQueuedSinkDeliversInOrder(t *testing.T) {
	device := newStalledDevice(false)
	sink, err := newQueuedSink(device)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []string{"one\n", "two\n"} {
		if count, err := sink.WriteContext(t.Context(), []byte(record)); err != nil || count != len(record) {
			t.Fatal(count, err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if got := device.output.String(); got != "one\ntwo\n" || !device.closed {
		t.Fatalf("output %q closed %v", got, device.closed)
	}
}

func TestQueuedSinkNeverWaitsAndCountsOverflow(t *testing.T) {
	device := newStalledDevice(true)
	sink, err := newQueuedSink(device)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	accepted := 0
	for index := 0; index < queuedRecords+4; index++ {
		if _, err := sink.WriteContext(t.Context(), []byte("record\n")); err == nil {
			accepted += 1
		} else if !errors.Is(err, errQueueFull) {
			t.Fatal(err)
		}
		if index == 0 {
			<-device.entered
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatal("write waited on the device", elapsed)
	}
	// One record is in the stalled write; the queue holds queuedRecords more.
	if accepted != queuedRecords+1 || sink.droppedCount() != 3 {
		t.Fatal("unexpected admission", accepted, sink.droppedCount())
	}
	close(device.release)
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedSinkCloseInterruptsBlockedWrite(t *testing.T) {
	device := newStalledDevice(true)
	sink, err := newQueuedSink(device)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink.WriteContext(t.Context(), []byte("stalled\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.WriteContext(t.Context(), []byte("queued\n")); err != nil {
		t.Fatal(err)
	}
	<-device.entered
	start := time.Now()
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > writeTimeout+time.Second {
		t.Fatal("close did not interrupt", elapsed)
	}
	if !device.closed || device.output.Len() != 0 {
		t.Fatal("device not joined before release", device.closed, device.output.String())
	}
	if sink.droppedCount() != 2 {
		t.Fatal("aborted records not counted", sink.droppedCount())
	}
	if _, err := sink.WriteContext(context.Background(), []byte("late\n")); err == nil {
		t.Fatal("closed sink accepted a record")
	}
}
