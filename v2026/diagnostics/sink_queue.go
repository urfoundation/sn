// A synchronous device (a Windows console or pipe) cannot honor a write
// context, so only a dedicated writer ever calls it. The exporter's write
// enqueues into a bounded queue and never waits on the device; Close drains
// for one write budget, then interrupts the writer's actual call and joins it.
package diagnostics

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"
)

const queuedRecords = 16
const queuedBytes = queuedRecords * MaximumRecordBytes
const interruptInterval = 10 * time.Millisecond

var errQueueFull = errors.New("diagnostic device queue is full")

// The device is owned by the queue. begin and write run on the writer's locked
// OS thread; interrupt may be called from any goroutine, repeatedly, and must
// end a write in progress on that thread. close runs after the writer joins.
type blockingDevice interface {
	begin() error
	write([]byte) (int, error)
	interrupt()
	close() error
}

// Delivery means accepted by the device writer's bounded queue. Full-queue and
// failed-device writes return zero bytes, so the exporter counts them dropped.
type queuedSink struct {
	stateLock sync.Mutex
	device    blockingDevice
	records   [][]byte
	bytes     int
	failed    error
	closing   bool
	aborted   bool
	dropped   uint64
	wake      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// The writer has started on its own thread before admission returns, so an
// interrupt never races the device's thread binding.
func newQueuedSink(device blockingDevice) (*queuedSink, error) {
	self := &queuedSink{device: device, wake: make(chan struct{}, 1), done: make(chan struct{})}
	started := make(chan error, 1)
	go self.run(started)
	if err := <-started; err != nil {
		<-self.done
		return nil, errors.Join(err, device.close())
	}
	return self, nil
}

// Never blocks: a record is copied into the queue or refused at once.
func (self *queuedSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.failed != nil {
		return 0, self.failed
	}
	if self.closing {
		return 0, errors.New("diagnostic device is closing")
	}
	if len(self.records) >= queuedRecords || self.bytes > queuedBytes-len(raw) {
		increment(&self.dropped, 1)
		return 0, errQueueFull
	}
	self.records = append(self.records, append([]byte(nil), raw...))
	self.bytes += len(raw)
	select {
	case self.wake <- struct{}{}:
	default:
	}
	return len(raw), nil
}

// The sole caller of the device. A failed or partial write disables the device
// so another record never splices into an ambiguous prefix.
func (self *queuedSink) run(started chan<- error) {
	defer close(self.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := self.device.begin(); err != nil {
		started <- err
		return
	}
	started <- nil
	for {
		self.stateLock.Lock()
		for len(self.records) == 0 && !self.closing {
			self.stateLock.Unlock()
			<-self.wake
			self.stateLock.Lock()
		}
		if len(self.records) == 0 || self.aborted {
			self.discardWithLock()
			self.stateLock.Unlock()
			return
		}
		raw := self.records[0]
		self.records[0] = nil
		self.records = self.records[1:]
		self.bytes -= len(raw)
		self.stateLock.Unlock()
		if err := self.writeAll(raw); err != nil {
			self.stateLock.Lock()
			self.failed = err
			increment(&self.dropped, 1)
			self.discardWithLock()
			self.stateLock.Unlock()
			return
		}
	}
}

// A short write resumes at its suffix; no progress is a failure.
func (self *queuedSink) writeAll(raw []byte) error {
	for written := 0; written < len(raw); {
		count, err := self.device.write(raw[written:])
		written += count
		if err != nil {
			return err
		}
		if count == 0 {
			return errors.New("diagnostic device accepted no bytes")
		}
	}
	return nil
}

func (self *queuedSink) discardWithLock() {
	increment(&self.dropped, uint64(len(self.records)))
	self.records, self.bytes = nil, 0
}

// Queued records get one write budget to reach the device. The writer is then
// aborted and its in-flight call interrupted until it joins; the device is
// released only afterwards, so no write outlives Close.
func (self *queuedSink) Close() error {
	self.closeOnce.Do(func() {
		self.stateLock.Lock()
		self.closing = true
		self.stateLock.Unlock()
		select {
		case self.wake <- struct{}{}:
		default:
		}
		timer := time.NewTimer(writeTimeout)
		defer timer.Stop()
		select {
		case <-self.done:
		case <-timer.C:
			self.stateLock.Lock()
			self.aborted = true
			self.stateLock.Unlock()
			for joined := false; !joined; {
				self.device.interrupt()
				select {
				case <-self.done:
					joined = true
				case <-time.After(interruptInterval):
				}
			}
		}
		self.closeErr = self.device.close()
	})
	return self.closeErr
}

// Records refused or discarded by the device queue, for tests and inspection.
func (self *queuedSink) droppedCount() uint64 {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.dropped
}
