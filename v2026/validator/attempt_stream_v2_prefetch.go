//go:build linux || darwin

package validator

// A replay may fetch the next few data chunks of one stream while it verifies
// the current one. Fetching decides nothing: each chunk's bytes still reach
// walkAttemptStreamV2Chunk one at a time in descriptor order, which applies the
// same row, byte-count, EOF and content-hash checks to them as to a streamed
// body. Only the time at which the bytes are requested changes.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// Fetches a release replay keeps in flight beyond the chunk it is verifying.
const attemptStreamV2PrefetchDepth = 4

// The chunk bytes fetched ahead plus the chunk being verified never exceed
// this (each buffer also holds its one EOF-probe byte). A chunk bound that
// allows fewer than two keeps the sequential reader.
const attemptStreamV2PrefetchBytes = 20 << 20

type attemptStreamV2Prefetch struct {
	ctx     context.Context
	cancel  context.CancelFunc
	kind    string
	open    AttemptStreamV2DataOpener
	depth   int
	queued  []AttemptStreamV2Chunk
	started []*attemptStreamV2PrefetchChunk
	workers sync.WaitGroup
}

// Written by its one fetch before done closes; read by the replay after.
type attemptStreamV2PrefetchChunk struct {
	hash string
	size uint64
	done chan struct{}
	data []byte
	err  error
}

// A depth below one, or a chunk bound too large for the byte budget, keeps
// the original opener unchanged.
func newAttemptStreamV2Prefetch(ctx context.Context, kind string, open AttemptStreamV2DataOpener, depth int, maxChunkBytes uint64) *attemptStreamV2Prefetch {
	self := &attemptStreamV2Prefetch{kind: kind, open: open}
	if depth < 1 || maxChunkBytes == 0 || attemptStreamV2PrefetchBytes/maxChunkBytes < 2 {
		return self
	}
	self.depth = int(min(uint64(depth), attemptStreamV2PrefetchBytes/maxChunkBytes-1))
	self.ctx, self.cancel = context.WithCancel(ctx)
	return self
}

// Receives one page's decoded descriptors, in order, before the walker checks
// and visits them. A descriptor the walker then refuses is never verified;
// its fetch is cancelled and joined by close.
func (self *attemptStreamV2Prefetch) page(chunks []AttemptStreamV2Chunk) {
	if self.depth == 0 {
		return
	}
	self.queued = append(self.queued, chunks...)
	self.fill()
}

func (self *attemptStreamV2Prefetch) fill() {
	for len(self.started) < self.depth && len(self.queued) > 0 {
		chunk := &attemptStreamV2PrefetchChunk{hash: self.queued[0].ContentHash, size: self.queued[0].DataBytes, done: make(chan struct{})}
		self.queued = self.queued[1:]
		self.started = append(self.started, chunk)
		self.workers.Add(1)
		go self.fetch(chunk)
	}
}

// Reads at most the authenticated length plus one byte, as the streamed
// verifier does, and always closes what it opened.
func (self *attemptStreamV2Prefetch) fetch(chunk *attemptStreamV2PrefetchChunk) {
	defer self.workers.Done()
	defer close(chunk.done)
	reader, err := self.open(self.ctx, self.kind, chunk.hash, chunk.size)
	if reader == nil {
		if err == nil {
			err = errors.New("compact attempt chunk reader is nil")
		}
		chunk.err = err
		return
	}
	if err == nil {
		chunk.data, err = readAttemptStreamV2Prefetch(reader, chunk.size+1)
	}
	chunk.err = errors.Join(err, reader.Close())
}

// io.ReadAll over io.LimitReader(reader, limit) in one allocation of limit
// bytes: a plain EOF ends the chunk, and any other error is returned as is.
func readAttemptStreamV2Prefetch(reader io.Reader, limit uint64) ([]byte, error) {
	data := make([]byte, 0, limit)
	for uint64(len(data)) < limit {
		count, err := reader.Read(data[len(data):limit])
		if count < 0 || count > int(limit)-len(data) {
			return nil, errors.New("compact attempt chunk reader returned an invalid count")
		}
		data = data[:len(data)+count]
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return data, err
		}
	}
	return data, nil
}

// The replay's data opener. It hands out exactly the next scheduled chunk;
// any other request means the walker and the schedule disagree and fails.
func (self *attemptStreamV2Prefetch) Open(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
	if self.depth == 0 {
		return self.open(ctx, kind, hash, size)
	}
	if len(self.started) == 0 || kind != self.kind || self.started[0].hash != hash || self.started[0].size != size {
		return nil, errors.New("compact attempt prefetch differs from its descriptor order")
	}
	chunk := self.started[0]
	self.started = self.started[1:]
	<-chunk.done
	self.fill()
	if chunk.err != nil {
		return nil, chunk.err
	}
	return io.NopCloser(bytes.NewReader(chunk.data)), nil
}

// Cancels fetches the replay no longer needs and waits for every one, so no
// request or body outlives the replay that scheduled it.
func (self *attemptStreamV2Prefetch) close() {
	if self.depth == 0 {
		return
	}
	self.cancel()
	self.workers.Wait()
}
