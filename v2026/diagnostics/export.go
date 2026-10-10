// Diagnostic export is best effort and instance-owned. Producers offer complete
// bounded records without waiting for their destination; cancellation joins the
// sole writer, whose sink must honor its context without detached I/O.
package diagnostics

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"time"
)

const MaximumDomains = 16
const MaximumRecordBytes = 16 * 1024
const RecordsPerDomain = 2
const writeTimeout = 250 * time.Millisecond
const retryDelay = time.Second

// Embeddings must implement actual cancellation, not abandon a Write goroutine.
// Records are immutable during this call; the caller retains sink ownership.
type ContextWriter interface {
	WriteContext(context.Context, []byte) (int, error)
}

// This adapter owns only its descriptors, never the caller's original writer.
type ownedSink interface {
	ContextWriter
	Close() error
}

// Values describe this process instance only. Delivery is a completed sink
// write, not an alert acknowledgment or protocol observation. Counters saturate.
type Snapshot struct {
	Outcome       string `json:"outcome"`
	Delivered     uint64 `json:"delivered"`
	Dropped       uint64 `json:"dropped"`
	DroppedBytes  uint64 `json:"dropped_bytes"`
	Unavailable   uint64 `json:"unavailable"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
}

// Each domain has independent finite capacity; a noisy role cannot take another
// role's slots. Only the exporter worker removes entries or invokes its sink.
type domain struct {
	queue [][]byte
	bytes int
	state Snapshot
}

// Methods are safe for concurrent use. Close is idempotent and joins the sole
// writer. Neither a producer nor the lifecycle lock performs destination I/O.
type Exporter struct {
	stateLock   sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	wake        chan struct{}
	sink        ownedSink
	domains     []domain
	names       map[string]int
	next        int
	closed      bool
	closing     bool
	unavailable bool
	closeErr    error
	now         func() time.Time
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        func(context.Context, time.Duration) bool
	afterWrite  func()
}

// Sink refusal is operational state, not constructor failure. Unknown Writers
// are never called. Domain names are a supplied finite census, not log content.
func New(ctx context.Context, writer io.Writer, names []string) (*Exporter, error) {
	return NewWithClock(ctx, writer, names, time.Now)
}

// An explicit instance clock makes acknowledgment timestamps testable without
// changing real I/O deadlines or sharing a mutable clock between exporters.
func NewWithClock(ctx context.Context, writer io.Writer, names []string, now func() time.Time) (*Exporter, error) {
	if ctx == nil || now == nil || len(names) == 0 || len(names) > MaximumDomains {
		return nil, errors.New("diagnostic context or domain census is invalid")
	}
	indices := make(map[string]int, len(names))
	for index, name := range names {
		if !validDomain(name) {
			return nil, errors.New("diagnostic domain is invalid")
		}
		if _, exists := indices[name]; exists {
			return nil, errors.New("diagnostic domain is duplicated")
		}
		indices[name] = index
	}
	// The parent explicitly closes this owner after its workers and cleanup
	// diagnostics finish. A canceled sampling context must not discard them
	// before the bounded final drain; Close supplies the actual write cancel.
	owned, cancel := context.WithCancel(context.WithoutCancel(ctx))
	self := &Exporter{ctx: owned, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), domains: make([]domain, len(names)), names: indices, now: now, withTimeout: context.WithTimeout, wait: waitDiagnostic}
	var err error
	self.sink, err = adaptSink(writer)
	self.unavailable = err != nil
	for index := range self.domains {
		self.domains[index].state.Outcome = "starting"
		if self.unavailable {
			self.domains[index].state.Outcome = "unavailable"
		}
	}
	go self.run()
	return self, nil
}

// Error retries remain owned and cancelable, with a fixed per-instance budget.
func waitDiagnostic(ctx context.Context, duration time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(duration):
		return true
	}
}

// Labels originate in reviewed configuration and never contain arbitrary text.
func validDomain(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for index, character := range []byte(value) {
		if character >= 'a' && character <= 'z' || index > 0 && (character >= '0' && character <= '9' || character == '_' || character == '-') {
			continue
		}
		return false
	}
	return true
}

// Saturation preserves monotonic counters instead of making overflow healthy.
func increment(value *uint64, amount uint64) {
	if math.MaxUint64-*value < amount {
		*value = math.MaxUint64
	} else {
		*value += amount
	}
}

// Rejection is counted before copying; even an oversized caller buffer cannot
// expand exporter memory. Successful offer means queued, never delivered.
func (self *Exporter) Offer(name string, raw []byte) bool {
	if self == nil {
		return false
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	index, exists := self.names[name]
	if !exists {
		return false
	}
	entry := &self.domains[index]
	if self.closed || self.closing || self.ctx.Err() != nil || self.unavailable || len(raw) == 0 || len(raw) > MaximumRecordBytes || len(entry.queue) >= RecordsPerDomain || entry.bytes > RecordsPerDomain*MaximumRecordBytes-len(raw) {
		increment(&entry.state.Dropped, 1)
		increment(&entry.state.DroppedBytes, uint64(len(raw)))
		return false
	}
	entry.queue = append(entry.queue, append([]byte(nil), raw...))
	entry.bytes += len(raw)
	select {
	case self.wake <- struct{}{}:
	default:
	}
	return true
}

// A copied scalar snapshot cannot change underneath JSON encoding or retention.
func (self *Exporter) Snapshot(name string) Snapshot {
	if self == nil {
		return Snapshot{Outcome: "unavailable"}
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if index, exists := self.names[name]; exists {
		return self.domains[index].state
	}
	return Snapshot{Outcome: "unavailable"}
}

// Writers expose queue admission, not downstream acknowledgment. Operational
// consumers must read Snapshot; a full diagnostic queue never stops core work.
func (self *Exporter) Writer(name string) io.Writer { return &domainWriter{owner: self, name: name} }

// A small immutable routing handle never retains caller-owned record bytes.
type domainWriter struct {
	owner *Exporter
	name  string
}

// io.Writer compatibility is limited to diagnostic producers already bounded
// by their own record contract. Offer counts every refused record explicitly.
func (self *domainWriter) Write(raw []byte) (int, error) {
	self.owner.Offer(self.name, raw)
	return len(raw), nil
}

// The writer itself is the telemetry source, avoiding mutable global state or
// an unrelated worker's counters when a caller composes several destinations.
func (self *domainWriter) DiagnosticSnapshot() Snapshot { return self.owner.Snapshot(self.name) }

// Round-robin selection bounds one role's effect on other queued records.
func (self *Exporter) take() (int, []byte) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	for count := 0; count < len(self.domains); count++ {
		index := (self.next + count) % len(self.domains)
		entry := &self.domains[index]
		if len(entry.queue) == 0 {
			continue
		}
		raw := entry.queue[0]
		entry.queue[0] = nil
		entry.queue = entry.queue[1:]
		entry.bytes -= len(raw)
		self.next = (index + 1) % len(self.domains)
		return index, raw
	}
	return 0, nil
}

// A partial failed record disables the destination: the next JSON record must
// not splice into its prefix. Zero-byte failures remain retryable and bounded.
func (self *Exporter) completed(index int, raw []byte, count int, err error) {
	var successAt string
	if err == nil && count == len(raw) {
		successAt = self.now().UTC().Format(time.RFC3339Nano)
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	entry := &self.domains[index]
	if err == nil && count == len(raw) {
		increment(&entry.state.Delivered, 1)
		entry.state.Outcome, entry.state.LastSuccessAt = "delivered", successAt
		return
	}
	increment(&entry.state.Dropped, 1)
	increment(&entry.state.DroppedBytes, uint64(len(raw)))
	increment(&entry.state.Unavailable, 1)
	entry.state.Outcome = "retrying"
	if count != 0 {
		self.unavailable = true
		for i := range self.domains {
			self.domains[i].state.Outcome = "unavailable"
		}
		self.discardWithLock()
	}
}

// Disposal occurs under the short memory lock, after the actual writer joins.
func (self *Exporter) discardWithLock() {
	for index := range self.domains {
		entry := &self.domains[index]
		increment(&entry.state.Dropped, uint64(len(entry.queue)))
		increment(&entry.state.DroppedBytes, uint64(entry.bytes))
		entry.queue, entry.bytes = nil, 0
	}
}

// One worker invokes only admitted cancellable sinks. No goroutine is spawned
// per write, and cancellation closes descriptors after the final write returns.
func (self *Exporter) run() {
	defer close(self.done)
	var active []byte
	var activeIndex int
	var activeCancel context.CancelFunc
	defer func() {
		// A supplied context sink may panic or end its goroutine. Unknown
		// partial effects disable this destination without crashing core work.
		_ = recover()
		if activeCancel != nil {
			activeCancel()
		}
		if active != nil {
			self.completed(activeIndex, active, 1, errors.New("diagnostic sink ended during a write"))
		}
		var err error
		if self.sink != nil {
			err = self.sink.Close()
		}
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.closed, self.closeErr = true, err
		self.discardWithLock()
		for index := range self.domains {
			self.domains[index].state.Outcome = "unavailable"
		}
		if err != nil {
			for index := range self.domains {
				self.domains[index].state.Outcome = "unavailable"
				increment(&self.domains[index].state.Unavailable, 1)
			}
		}
	}()
	for self.ctx.Err() == nil {
		index, raw := self.take()
		if raw == nil {
			self.stateLock.Lock()
			closing := self.closing
			self.stateLock.Unlock()
			if closing {
				return
			}
			select {
			case <-self.ctx.Done():
				return
			case <-self.wake:
				continue
			}
		}
		attempt, cancel := self.withTimeout(self.ctx, writeTimeout)
		activeCancel = cancel
		active, activeIndex = raw, index
		count, err := self.sink.WriteContext(attempt, raw)
		cancel()
		activeCancel = nil
		if count != len(raw) && err == nil {
			err = io.ErrShortWrite
		}
		self.completed(index, raw, count, err)
		active = nil
		if self.afterWrite != nil {
			self.afterWrite()
		}
		if err != nil {
			if !self.wait(self.ctx, retryDelay) {
				return
			}
		}
	}
}

// Close cancels the write context and all retry waits, then joins descriptor
// cleanup. It cannot abandon an operation that might later write to the sink.
func (self *Exporter) Close() error {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	self.closing = true
	self.stateLock.Unlock()
	select {
	case self.wake <- struct{}{}:
	default:
	}
	// At most one write budget drains final records; a blocked destination is
	// then canceled and joined, not abandoned for a background flush.
	timer := time.NewTimer(writeTimeout)
	select {
	case <-self.done:
		timer.Stop()
	case <-timer.C:
		self.cancel()
		<-self.done
	}
	self.cancel()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.closeErr
}
