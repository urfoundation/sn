package validator

// Data uploads in flight beside the stream that builds them. A seal's objects
// went one at a time until 2026-11-16: even on a healthy path its records took
// 29.9 seconds, and a lossy path multiplied that by each object's wait.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Counts the data uploads running at once, and how many had finished when the
// first page or manifest was written. The fixture's own sinks run under a lock.
type attemptStreamV2InFlightObserver struct {
	stateLock       sync.Mutex
	calls           int
	active          int
	peak            int
	completed       int
	pagesStarted    bool
	completedBefore int
	activeBefore    int
}

func (self *attemptStreamV2InFlightObserver) wrap(fixture *attemptStreamV2WriteFixture, hold func(ctx context.Context, call int) error) {
	data, metadata := fixture.options.WriteData, fixture.options.WriteMetadata
	fixture.options.WriteData = func(ctx context.Context, hash string, raw []byte) error {
		self.stateLock.Lock()
		call := self.calls
		self.calls++
		self.active++
		self.peak = max(self.peak, self.active)
		self.stateLock.Unlock()
		defer func() {
			self.stateLock.Lock()
			self.active--
			self.stateLock.Unlock()
		}()
		if err := hold(ctx, call); err != nil {
			return err
		}
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if err := data(ctx, hash, raw); err != nil {
			return err
		}
		self.completed++
		return nil
	}
	fixture.options.WriteMetadata = func(ctx context.Context, hash string, raw []byte) error {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if !self.pagesStarted {
			self.pagesStarted = true
			self.completedBefore, self.activeBefore = self.completed, self.active
		}
		return metadata(ctx, hash, raw)
	}
}

func (self *attemptStreamV2InFlightObserver) snapshot() (calls, active, peak, completed int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.calls, self.active, self.peak, self.completed
}

// Twelve rows of 15 and 16 bytes in 30-byte chunks: eight data objects.
var attemptStreamV2InFlightSequences = []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

// The first uploads wait for each other, so the stream can only finish if
// they ran at once. No more than InFlight ever run, every one finishes before
// any page names it, and the reference is the one-at-a-time stream's.
func TestAttemptStreamV2WriteUploadsInFlightTogether(t *testing.T) {
	sequential := newAttemptStreamV2WriteFixture(t)
	want, wantCensus, err := WriteAttemptStreamV2(t.Context(), AttemptStreamV2Records, attemptStreamV2WriteRows(attemptStreamV2InFlightSequences...), sequential.options)
	if err != nil || wantCensus.ChunkCount != 8 {
		t.Fatalf("one-at-a-time stream: %+v %v", wantCensus, err)
	}
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.InFlight = 3
	observer := &attemptStreamV2InFlightObserver{}
	together := make(chan struct{})
	var once sync.Once
	observer.wrap(fixture, func(ctx context.Context, call int) error {
		if _, active, _, _ := observer.snapshot(); active == fixture.options.InFlight {
			once.Do(func() { close(together) })
		}
		select {
		case <-together:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return errors.New("uploads never ran at once")
		}
	})
	reference, census, err := WriteAttemptStreamV2(t.Context(), AttemptStreamV2Records, attemptStreamV2WriteRows(attemptStreamV2InFlightSequences...), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	calls, active, peak, completed := observer.snapshot()
	if reference != want || census != wantCensus {
		t.Fatalf("uploads in flight changed the stream: %+v %+v, want %+v %+v", reference, census, want, wantCensus)
	}
	if calls != 8 || completed != 8 || active != 0 || peak != fixture.options.InFlight {
		t.Fatalf("calls=%d completed=%d active=%d peak=%d, want 8 uploads at most %d at once", calls, completed, active, peak, fixture.options.InFlight)
	}
	if observer.completedBefore != 8 || observer.activeBefore != 0 {
		t.Fatalf("a page was written with %d of 8 uploads done and %d running", observer.completedBefore, observer.activeBefore)
	}
	fixture.verify(t, AttemptStreamV2Records, reference, census, attemptStreamV2InFlightSequences)
}

// Zero or one in flight never uploads two objects at once.
func TestAttemptStreamV2WriteOneInFlightUploadsInTurn(t *testing.T) {
	for _, inFlight := range []int{0, 1} {
		fixture := newAttemptStreamV2WriteFixture(t)
		fixture.options.InFlight = inFlight
		observer := &attemptStreamV2InFlightObserver{}
		observer.wrap(fixture, func(context.Context, int) error {
			time.Sleep(5 * time.Millisecond)
			return nil
		})
		reference, census, err := WriteAttemptStreamV2(t.Context(), AttemptStreamV2Records, attemptStreamV2WriteRows(attemptStreamV2InFlightSequences...), fixture.options)
		if _, _, peak, completed := observer.snapshot(); err != nil || peak != 1 || completed != 8 {
			t.Fatalf("in flight %d: peak=%d completed=%d err=%v", inFlight, peak, completed, err)
		}
		fixture.verify(t, AttemptStreamV2Records, reference, census, attemptStreamV2InFlightSequences)
	}
}

// One failed upload cancels the others, which are joined before the stream
// returns its failure and no reference. No page or manifest is written.
func TestAttemptStreamV2WriteFailedUploadCancelsTheRest(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.InFlight = 3
	failure := errors.New("deterministic upload failure")
	observer := &attemptStreamV2InFlightObserver{}
	var canceled sync.WaitGroup
	canceled.Add(2)
	observer.wrap(fixture, func(ctx context.Context, call int) error {
		if call == 2 {
			return failure
		}
		if call < 2 {
			<-ctx.Done()
			canceled.Done()
			return ctx.Err()
		}
		return errors.New("an upload started after a failure")
	})
	reference, census, err := WriteAttemptStreamV2(t.Context(), AttemptStreamV2Records, attemptStreamV2WriteRows(attemptStreamV2InFlightSequences...), fixture.options)
	calls, active, _, completed := observer.snapshot()
	if !errors.Is(err, failure) || errors.Is(err, context.Canceled) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) {
		t.Fatalf("a failed upload was accepted or hidden: %v", err)
	}
	if calls != 3 || active != 0 || completed != 0 || observer.pagesStarted || len(fixture.metaKVs) != 0 {
		t.Fatalf("calls=%d active=%d completed=%d pages=%t", calls, active, completed, observer.pagesStarted)
	}
	canceled.Wait()
}

// A source that fails while uploads run gets its own error back, after every
// upload saw the cancellation and returned.
func TestAttemptStreamV2WriteSourceFailureJoinsUploads(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.InFlight = 3
	failure := errors.New("source interrupted while uploads ran")
	observer := &attemptStreamV2InFlightObserver{}
	started := make(chan struct{}, 8)
	observer.wrap(fixture, func(ctx context.Context, _ int) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	source := func(ctx context.Context, visit func(uint64, []byte) error) error {
		if err := attemptStreamV2WriteRows(1, 2, 3, 4, 5)(ctx, visit); err != nil {
			return err
		}
		<-started
		<-started
		return failure
	}
	reference, _, err := WriteAttemptStreamV2(t.Context(), AttemptStreamV2Records, source, fixture.options)
	if _, active, _, _ := observer.snapshot(); !errors.Is(err, failure) || reference != (AttemptStreamV2Reference{}) || active != 0 || len(fixture.metaKVs) != 0 {
		t.Fatalf("active=%d err=%v", active, err)
	}
}

// The caller's cancellation ends uploads in flight, and the stream returns it.
func TestAttemptStreamV2WriteCancellationEndsUploadsInFlight(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.InFlight = 3
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observer := &attemptStreamV2InFlightObserver{}
	observer.wrap(fixture, func(ctx context.Context, call int) error {
		if call == 1 {
			cancel()
		}
		<-ctx.Done()
		return ctx.Err()
	})
	reference, _, err := WriteAttemptStreamV2(ctx, AttemptStreamV2Records, attemptStreamV2WriteRows(attemptStreamV2InFlightSequences...), fixture.options)
	if _, active, _, _ := observer.snapshot(); !errors.Is(err, context.Canceled) || reference != (AttemptStreamV2Reference{}) || active != 0 {
		t.Fatalf("active=%d err=%v", active, err)
	}
}

// One in flight builds beside its upload: the first chunk's upload waits until
// the walk has gone on to row 5, past the next chunk's first rows, which a
// stream that uploads each chunk before building the next never does.
func TestAttemptStreamV2WriteOneInFlightBuildsBesideItsUpload(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.InFlight = 1
	visited := make(chan uint64, len(attemptStreamV2InFlightSequences))
	observer := &attemptStreamV2InFlightObserver{}
	observer.wrap(fixture, func(ctx context.Context, call int) error {
		if call != 0 {
			return nil
		}
		for {
			select {
			case sequence := <-visited:
				if 5 <= sequence {
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return errors.New("the walk waited for the upload")
			}
		}
	})
	source := func(ctx context.Context, visit func(uint64, []byte) error) error {
		return attemptStreamV2WriteRows(attemptStreamV2InFlightSequences...)(ctx, func(sequence uint64, row []byte) error {
			visited <- sequence
			return visit(sequence, row)
		})
	}
	reference, census, err := WriteAttemptStreamV2(t.Context(), AttemptStreamV2Records, source, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if _, active, peak, completed := observer.snapshot(); peak != 1 || completed != 8 || active != 0 || observer.completedBefore != 8 {
		t.Fatalf("peak=%d completed=%d active=%d before pages=%d", peak, completed, active, observer.completedBefore)
	}
	fixture.verify(t, AttemptStreamV2Records, reference, census, attemptStreamV2InFlightSequences)
}
