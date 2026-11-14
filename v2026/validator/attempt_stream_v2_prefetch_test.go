//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

// Counts concurrent data requests and their closes. The objects are the
// sealed cut's immutable bytes; only the served copy can be changed.
type attemptStreamV2PrefetchTestOpener struct {
	objects  *attemptCutV2SealTestObjects
	serve    func(kind, hash string, raw []byte) ([]byte, error)
	block    func(ctx context.Context, hash string)
	lock     sync.Mutex
	active   int
	calls    int
	inFlight int
	most     int
	opens    int
	closes   int
}

func (self *attemptStreamV2PrefetchTestOpener) open(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
	self.lock.Lock()
	self.active++
	self.calls++
	self.inFlight++
	self.most = max(self.most, self.inFlight)
	call := self.calls
	self.lock.Unlock()
	finish := func(opened bool) {
		self.lock.Lock()
		defer self.lock.Unlock()
		self.active--
		if !opened {
			self.inFlight--
		} else {
			self.opens++
		}
	}
	// Uneven latency lets later fetches complete before earlier ones.
	time.Sleep(time.Duration(3-call%3) * time.Millisecond)
	if self.block != nil {
		self.block(ctx, hash)
	}
	if err := ctx.Err(); err != nil {
		finish(false)
		return nil, err
	}
	raw, exists := self.objects.dataKVs[kind][hash]
	if !exists || uint64(len(raw)) != size {
		finish(false)
		return nil, errors.New("prefetch test object is missing or resized")
	}
	raw = bytes.Clone(raw)
	if self.serve != nil {
		var err error
		if raw, err = self.serve(kind, hash, raw); err != nil {
			finish(false)
			return nil, err
		}
	}
	finish(true)
	return &attemptCutV2SealTestReader{Reader: bytes.NewReader(raw), close: func() error {
		self.lock.Lock()
		defer self.lock.Unlock()
		self.inFlight--
		self.closes++
		return nil
	}}, nil
}

func (self *attemptStreamV2PrefetchTestOpener) settled(t *testing.T) {
	t.Helper()
	self.lock.Lock()
	defer self.lock.Unlock()
	if self.active != 0 || self.inFlight != 0 || self.opens != self.closes {
		t.Fatalf("a fetch outlived its replay: active=%d inFlight=%d opens=%d closes=%d", self.active, self.inFlight, self.opens, self.closes)
	}
}

// A real M8 cut over many chunks and several descriptor pages.
func newAttemptStreamV2PrefetchTestCut(t *testing.T) (*attemptCutV2SealTestFixture, AttemptCutV2SealOptions, *attemptCutV2SealTestObjects, *AttemptCutV2, AttemptCutV2ReplayResult) {
	t.Helper()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 8, 2)
	fixture.bounds.Records.MaxDescriptorsPerPage, fixture.bounds.Proofs.MaxDescriptorsPerPage = 8, 8
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Records.ChunkCount < 8 || verified.Records.PageCount < 2 {
		t.Fatalf("prefetch cut is too small to overlap fetches: %+v", verified.Records)
	}
	return fixture, options, objects, cut, verified
}

func replayAttemptStreamV2PrefetchTest(t *testing.T, ctx context.Context, fixture *attemptCutV2SealTestFixture, options AttemptCutV2SealOptions, cut *AttemptCutV2, opener *attemptStreamV2PrefetchTestOpener, prefetch int) (AttemptCutV2ReplayResult, []uint64, error) {
	t.Helper()
	var visited []uint64
	result, err := ReplayAttemptCutV2WithPolicy(ctx, *cut, cut.Context, fixture.policy, fixture.bounds, AttemptCutV2ReplayOptions{Bounds: options.ReplayBounds, ScratchDirectory: filepath.Join(t.TempDir(), "replay"), ServerKeys: options.ServerKeys, ReadMetadata: options.ReadMetadata, OpenData: opener.open, VisitRecord: func(record AttemptRecord) error {
		visited = append(visited, record.Sequence)
		return nil
	}, prefetch: prefetch})
	return result, visited, err
}

// Prefetching changes only when chunks are requested: the verdict, the
// record order and every request's close are those of the sequential replay.
func TestAttemptCutV2ReplayPrefetchKeepsOrderAndVerdict(t *testing.T) {
	fixture, options, objects, cut, verified := newAttemptStreamV2PrefetchTestCut(t)
	sequential := &attemptStreamV2PrefetchTestOpener{objects: objects}
	want, wantVisits, err := replayAttemptStreamV2PrefetchTest(t, t.Context(), fixture, options, cut, sequential, 0)
	if err != nil || want != verified || len(wantVisits) != len(fixture.recordTs) || !slices.IsSorted(wantVisits) || sequential.most != 1 {
		t.Fatalf("sequential replay result=%+v visits=%d most=%d: %v", want, len(wantVisits), sequential.most, err)
	}
	sequential.settled(t)
	prefetched := &attemptStreamV2PrefetchTestOpener{objects: objects}
	got, gotVisits, err := replayAttemptStreamV2PrefetchTest(t, t.Context(), fixture, options, cut, prefetched, attemptStreamV2PrefetchDepth)
	if err != nil || got != want || !slices.Equal(gotVisits, wantVisits) {
		t.Fatalf("prefetch changed the replay: result=%+v want=%+v visits equal=%v: %v", got, want, slices.Equal(gotVisits, wantVisits), err)
	}
	if prefetched.most < 2 || prefetched.most > attemptStreamV2PrefetchDepth || prefetched.opens != sequential.opens {
		t.Fatalf("prefetch concurrency or request count: most=%d opens=%d sequential=%d", prefetched.most, prefetched.opens, sequential.opens)
	}
	prefetched.settled(t)
}

// A changed or missing chunk is refused exactly as the sequential replay
// refuses it, and no fetch survives the refusal.
func TestAttemptCutV2ReplayPrefetchRefusesWhatSequentialRefuses(t *testing.T) {
	fixture, options, objects, cut, _ := newAttemptStreamV2PrefetchTestCut(t)
	var target string
	for hash := range objects.dataKVs[AttemptStreamV2Records] {
		target = hash
		break
	}
	missing := errors.New("synthetic missing chunk")
	for name, serve := range map[string]func(kind, hash string, raw []byte) ([]byte, error){
		"changed": func(kind, hash string, raw []byte) ([]byte, error) {
			if hash == target {
				raw[len(raw)/2] ^= 1
			}
			return raw, nil
		},
		"missing": func(kind, hash string, raw []byte) ([]byte, error) {
			if hash == target {
				return nil, missing
			}
			return raw, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			sequential := &attemptStreamV2PrefetchTestOpener{objects: objects, serve: serve}
			_, wantVisits, wantErr := replayAttemptStreamV2PrefetchTest(t, t.Context(), fixture, options, cut, sequential, 0)
			prefetched := &attemptStreamV2PrefetchTestOpener{objects: objects, serve: serve}
			got, gotVisits, gotErr := replayAttemptStreamV2PrefetchTest(t, t.Context(), fixture, options, cut, prefetched, attemptStreamV2PrefetchDepth)
			if wantErr == nil || gotErr == nil || gotErr.Error() != wantErr.Error() || got != (AttemptCutV2ReplayResult{}) || !slices.Equal(gotVisits, wantVisits) {
				t.Fatalf("prefetch refusal differs: sequential=%v prefetch=%v visits %d/%d", wantErr, gotErr, len(gotVisits), len(wantVisits))
			}
			if name == "missing" && (!errors.Is(gotErr, missing) || !errors.Is(wantErr, missing)) {
				t.Fatalf("missing chunk lost its cause: %v", gotErr)
			}
			sequential.settled(t)
			prefetched.settled(t)
		})
	}
}

// Cancellation while fetches are blocked returns only after every fetch has
// returned and every opened body has closed.
func TestAttemptCutV2ReplayPrefetchCancellationJoinsFetches(t *testing.T) {
	fixture, options, objects, cut, _ := newAttemptStreamV2PrefetchTestCut(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var once sync.Once
	blocked := make(chan struct{})
	var calls int
	var lock sync.Mutex
	opener := &attemptStreamV2PrefetchTestOpener{objects: objects, block: func(ctx context.Context, hash string) {
		lock.Lock()
		calls++
		late := calls > 2
		lock.Unlock()
		if late {
			once.Do(func() { close(blocked) })
			<-ctx.Done()
		}
	}}
	go func() {
		<-blocked
		cancel()
	}()
	result, _, err := replayAttemptStreamV2PrefetchTest(t, ctx, fixture, options, cut, opener, attemptStreamV2PrefetchDepth)
	if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("cancelled prefetch replay: result=%+v: %v", result, err)
	}
	opener.settled(t)
}

// The schedule hands out only the next descriptor's chunk, and a chunk bound
// too large for the byte budget keeps the sequential opener.
func TestAttemptStreamV2PrefetchKeepsDescriptorOrderAndBudget(t *testing.T) {
	data := map[string][]byte{"a": []byte("first\n"), "b": []byte("second\n")}
	var lock sync.Mutex
	var requested []string
	open := func(_ context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		lock.Lock()
		requested = append(requested, hash)
		lock.Unlock()
		return io.NopCloser(bytes.NewReader(data[hash])), nil
	}
	prefetch := newAttemptStreamV2Prefetch(t.Context(), AttemptStreamV2Records, open, attemptStreamV2PrefetchDepth, 4<<20)
	if prefetch.depth != attemptStreamV2PrefetchDepth {
		t.Fatalf("4 MiB chunks prefetch %d, want %d", prefetch.depth, attemptStreamV2PrefetchDepth)
	}
	prefetch.page([]AttemptStreamV2Chunk{{ContentHash: "a", DataBytes: 6}, {ContentHash: "b", DataBytes: 7}})
	if _, err := prefetch.Open(t.Context(), AttemptStreamV2Records, "b", 7); err == nil {
		t.Fatal("prefetch served a chunk out of descriptor order")
	}
	prefetch.close()
	if sequential := newAttemptStreamV2Prefetch(t.Context(), AttemptStreamV2Records, open, attemptStreamV2PrefetchDepth, 16<<20); sequential.depth != 0 {
		t.Fatalf("16 MiB chunks prefetch %d, want sequential", sequential.depth)
	}
	if bounded := newAttemptStreamV2Prefetch(t.Context(), AttemptStreamV2Records, open, attemptStreamV2PrefetchDepth, 8<<20); bounded.depth != 1 {
		t.Fatalf("8 MiB chunks prefetch %d, want 1", bounded.depth)
	}
	ordered := newAttemptStreamV2Prefetch(t.Context(), AttemptStreamV2Records, open, attemptStreamV2PrefetchDepth, 4<<20)
	defer ordered.close()
	ordered.page([]AttemptStreamV2Chunk{{ContentHash: "a", DataBytes: 6}, {ContentHash: "b", DataBytes: 7}})
	for _, chunk := range []struct {
		hash string
		size uint64
	}{{"a", 6}, {"b", 7}} {
		reader, err := ordered.Open(t.Context(), AttemptStreamV2Records, chunk.hash, chunk.size)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(raw, data[chunk.hash]) || reader.Close() != nil {
			t.Fatalf("prefetched %s = %q: %v", chunk.hash, raw, err)
		}
	}
	lock.Lock()
	defer lock.Unlock()
	if slices.Sort(requested); !reflect.DeepEqual(requested, []string{"a", "a", "b", "b"}) {
		t.Fatalf("each scheduled chunk is requested once per schedule: %v", requested)
	}
}

// Through the real typed HTTP reader, which proves each body's length and hash
// at EOF, a prefetched replay accepts exactly the sequential verdict and
// refuses a changed served proof exactly when the sequential replay does.
func TestAttemptStreamV2HTTPPrefetchReplayMatchesSequential(t *testing.T) {
	fixture, options, archive := newAttemptStreamV2HTTPReplayFixture(t, false)
	cut, verified, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	replay := func(prefetch int) (AttemptCutV2ReplayResult, error) {
		return ReplayAttemptCutV2WithPolicy(t.Context(), *cut, cut.Context, fixture.policy, fixture.bounds, AttemptCutV2ReplayOptions{Bounds: options.ReplayBounds, ScratchDirectory: filepath.Join(t.TempDir(), "replay"), ServerKeys: options.ServerKeys, ReadMetadata: options.ReadMetadata, OpenData: options.OpenData, prefetch: prefetch})
	}
	for _, prefetch := range []int{0, attemptStreamV2PrefetchDepth} {
		if result, err := replay(prefetch); err != nil || result != verified {
			t.Fatalf("HTTP replay with prefetch %d = %+v: %v", prefetch, result, err)
		}
	}
	archive.stateLock.Lock()
	archive.corruptProof = true
	archive.stateLock.Unlock()
	for _, prefetch := range []int{0, attemptStreamV2PrefetchDepth} {
		if result, err := replay(prefetch); err == nil || result != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("changed served proof accepted with prefetch %d: %+v", prefetch, result)
		}
	}
}

// The single-allocation read returns what io.ReadAll over the same limit
// returns, including data with EOF and a plain error, in limit bytes.
func TestAttemptStreamV2PrefetchReadMatchesLimitedReadAll(t *testing.T) {
	failure := errors.New("synthetic body failure")
	for name, source := range map[string]func() io.Reader{
		"exact":    func() io.Reader { return bytes.NewReader([]byte("0123456789")) },
		"suffix":   func() io.Reader { return bytes.NewReader([]byte("0123456789ab")) },
		"short":    func() io.Reader { return bytes.NewReader([]byte("0123")) },
		"one byte": func() io.Reader { return iotest.OneByteReader(bytes.NewReader([]byte("0123456789ab"))) },
		"data eof": func() io.Reader { return iotest.DataErrReader(bytes.NewReader([]byte("0123456789"))) },
		"truncated": func() io.Reader {
			return io.MultiReader(bytes.NewReader([]byte("0123")), iotest.ErrReader(io.ErrUnexpectedEOF))
		},
		"failure": func() io.Reader { return io.MultiReader(bytes.NewReader([]byte("01")), iotest.ErrReader(failure)) },
	} {
		want, wantErr := io.ReadAll(io.LimitReader(source(), 11))
		got, gotErr := readAttemptStreamV2Prefetch(source(), 11)
		if !bytes.Equal(got, want) || gotErr != wantErr || cap(got) != 11 {
			t.Fatalf("%s: read %q/%v cap %d, want %q/%v", name, got, gotErr, cap(got), want, wantErr)
		}
	}
}
