// An upload's I/O owner measures progress instead of granting one fixed span.
// Until 2026-11-16 every upload had 30 seconds whatever its size. A 4.19 MB
// records object at the 100 KB/s a lossy path allowed needed about 42 seconds,
// so every such upload was cut off, its seal failed, and the epoch's intent
// finalized 38 to 68 minutes late. Now a request ends only when its body has
// not moved for the stall window while it is still being sent, or when the
// whole request outlasts its cap: the stall window plus its size at the floor
// rate, and never past a reserved upload's signed consent. A slow upload that
// keeps moving completes; a dead connection ends within the stall window, and
// either ending is an owned deadline the caller may retry.
package validator

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"sync/atomic"
	"time"
)

const (
	// The slowest whole upload accepted: a 4 MiB object gets about 4.6 minutes.
	attemptStreamV2HttpWriteFloorBytesPerSecond = 16 << 10
	// How often an owner looks at its body's progress.
	attemptStreamV2HttpWriteWatchInterval = time.Second
)

// Upload timing a test may shorten. The zero value is production's.
type attemptUploadTiming struct {
	stall               time.Duration
	interval            time.Duration
	floorBytesPerSecond int
}

func (self attemptUploadTiming) values() (time.Duration, time.Duration, int) {
	stall, interval, floor := self.stall, self.interval, self.floorBytesPerSecond
	if stall <= 0 {
		stall = attemptStreamV2HttpWriteIoTimeout
	}
	if interval <= 0 {
		interval = attemptStreamV2HttpWriteWatchInterval
	}
	if floor <= 0 {
		floor = attemptStreamV2HttpWriteFloorBytesPerSecond
	}
	return stall, interval, floor
}

// The whole request's cap for a body of size bytes.
func (self attemptUploadTiming) cap(size int) time.Duration {
	stall, _, floor := self.values()
	return stall + time.Duration(size)*time.Second/time.Duration(floor)
}

// When the transport last took bytes from the request body, and whether it has
// taken them all. A connection whose send buffer is full takes nothing, so this
// follows the network's progress.
type attemptUploadProgress struct {
	moved atomic.Int64
	sent  atomic.Bool
}

func (self *attemptUploadProgress) restart() {
	self.sent.Store(false)
	self.moved.Store(time.Now().UnixNano())
}

// The request body, which reports its reads to the request's progress.
type attemptUploadBody struct {
	reader   *bytes.Reader
	progress *attemptUploadProgress
}

func newAttemptUploadBody(data []byte) *attemptUploadBody {
	self := &attemptUploadBody{reader: bytes.NewReader(data), progress: &attemptUploadProgress{}}
	self.progress.restart()
	return self
}

func (self *attemptUploadBody) Read(value []byte) (int, error) {
	count, err := self.reader.Read(value)
	if count > 0 {
		self.progress.moved.Store(time.Now().UnixNano())
	}
	if err == io.EOF || self.reader.Len() == 0 {
		self.progress.sent.Store(true)
	}
	return count, err
}

// The same bytes from the start, for a transport that sends the request again
// on a fresh connection after a pooled one closed before taking any of it.
// The copy reports to the same progress, which starts over.
func (self *attemptUploadBody) rewind(data []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		self.progress.restart()
		return io.NopCloser(&attemptUploadBody{reader: bytes.NewReader(data), progress: self.progress}), nil
	}
}

// The upload's owner. Its context carries the cap as a deadline. A body that
// stops moving before it is fully sent ends the context with DeadlineExceeded
// as its cause; expired reports either ending while the caller is still alive.
type attemptUploadWatch struct {
	ctx     context.Context
	parent  context.Context
	stopped chan struct{}
	done    chan struct{}
	release func()
}

func watchAttemptUpload(parent context.Context, body *attemptUploadBody, limit time.Duration, timing attemptUploadTiming) *attemptUploadWatch {
	stall, interval, _ := timing.values()
	inner, cancel := context.WithCancelCause(parent)
	ctx, cancelDeadline := context.WithTimeout(inner, limit)
	self := &attemptUploadWatch{ctx: ctx, parent: parent, stopped: make(chan struct{}), done: make(chan struct{})}
	self.release = func() {
		cancelDeadline()
		cancel(nil)
	}
	go func() {
		defer close(self.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-self.stopped:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !body.progress.sent.Load() && time.Since(time.Unix(0, body.progress.moved.Load())) >= stall {
				cancel(context.DeadlineExceeded)
				return
			}
		}
	}()
	return self
}

// Stops watching and joins the watcher before releasing the context.
func (self *attemptUploadWatch) close() {
	close(self.stopped)
	<-self.done
	self.release()
}

// Whether the owner itself ended the request: its stall or its cap, never the
// caller's cancellation or deadline.
func (self *attemptUploadWatch) expired() bool {
	if self.parent.Err() != nil {
		return false
	}
	return self.ctx.Err() == context.DeadlineExceeded || context.Cause(self.ctx) == context.DeadlineExceeded
}

// The request's own ending as one owned deadline, whatever error the
// transport saw while the connection was torn down under it.
func (self *attemptUploadWatch) deadline(endpoint url.URL) error {
	return &url.Error{Op: "Post", URL: endpoint.String(), Err: context.DeadlineExceeded}
}
