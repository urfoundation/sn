//go:build linux || darwin

// Virtual clocks and explicit barriers prove bounded reader ownership without
// real network timing. Real descriptor, hash, body and sink checks remain active.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// The immutable fixture stores only synthetic bytes. Mutable request counters
// are guarded because each of the two origin workers owns its transport.
type captureOriginsTestFixture struct {
	stateLock sync.Mutex
	objects   map[string][]byte
	reads     map[string]int
	rows      [][]byte
	cuts      []*AttemptCutV2
	options   ReleaseEvidenceV2CaptureOptions
	read      func(*http.Request, []byte) (io.ReadCloser, error)
}

// Repeated cuts share one prefix but retain distinct metadata censuses.
func newCaptureOriginsTestFixture(t *testing.T) *captureOriginsTestFixture {
	t.Helper()
	rows := [][]byte{[]byte("synthetic-one\n"), []byte("synthetic-two\n")}
	refs, objects := captureStreamTestReferences(t, AttemptStreamV2Records, rows, nil)
	self := &captureOriginsTestFixture{objects: objects, reads: map[string]int{}, rows: rows, options: ReleaseEvidenceV2CaptureOptions{
		Origins: [2]string{"https://left.example", "https://right.example"}, MaximumObjects: 64, MaximumBytes: 64 * 1024,
		ReuseCapturedStreams: true, ParallelStreamOrigins: true, RetryStreamReads: true,
	}}
	for _, ref := range []AttemptStreamV2Reference{refs[0], refs[1], refs[1]} {
		self.cuts = append(self.cuts, &AttemptCutV2{Records: ref, Proofs: AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}})
	}
	return self
}

// Only the exact configured endpoint and typed immutable route are served.
func (self *captureOriginsTestFixture) newReader(origin string, bounds AttemptCutV2Bounds) (*HTTPAttemptStreamV2Reader, error) {
	if origin != self.options.Origins[0] && origin != self.options.Origins[1] {
		return nil, errors.New("foreign synthetic origin")
	}
	reader, err := NewHTTPAttemptStreamV2Reader(origin, bounds)
	if err != nil {
		return nil, err
	}
	reader.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || len(request.URL.Query()) != 2 || request.Header.Get("Accept-Encoding") != "identity" {
			return nil, errors.New("changed synthetic immutable request")
		}
		kind, hash := request.URL.Query().Get("kind"), request.URL.Query().Get("hash")
		self.stateLock.Lock()
		self.reads[origin+"/"+kind+"/"+hash]++
		raw := bytes.Clone(self.objects[hash])
		self.stateLock.Unlock()
		body := io.ReadCloser(io.NopCloser(bytes.NewReader(raw)))
		if self.read != nil {
			body, err = self.read(request, raw)
			if err != nil {
				return nil, err
			}
		}
		contentType := "application/x-ndjson"
		if kind == "metadata" {
			contentType = "application/json"
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, ContentLength: int64(len(raw)), Body: body}, nil
	})
	return reader, nil
}

// Test assertions observe exact origin/type/hash requests after workers join.
func (self *captureOriginsTestFixture) count(origin, kind string, raw []byte) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.reads[origin+"/"+kind+"/"+attemptHex32(sha256.Sum256(raw))]
}

// Both data readers reach their barrier before either is released. A serial
// implementation deterministically reports only one arrival inside the bubble.
func TestReleaseCaptureOriginsParallelAndPrefixCustody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newCaptureOriginsTestFixture(t)
		arrived, release := make(chan string, 2), make(chan struct{})
		fixture.read = func(request *http.Request, raw []byte) (io.ReadCloser, error) {
			if request.URL.Query().Get("kind") == AttemptStreamV2Records && bytes.Equal(raw, fixture.rows[0]) {
				arrived <- request.URL.Host
				select {
				case <-release:
				case <-request.Context().Done():
					return nil, request.Context().Err()
				}
			}
			return io.NopCloser(bytes.NewReader(raw)), nil
		}
		retained := map[ReleaseEvidenceV2CaptureSource][]byte{}
		done := make(chan error, 1)
		go func() {
			done <- captureReleaseStreamOriginsV2(t.Context(), captureStreamTestBounds(), fixture.options, fixture.cuts, func(source ReleaseEvidenceV2CaptureSource, raw []byte) error {
				retained[source] = bytes.Clone(raw)
				return nil
			}, releaseCaptureStreamHooksV2{newReader: fixture.newReader})
		}()
		synctest.Wait()
		if len(arrived) != 2 {
			t.Errorf("independent origin readers did not overlap: arrivals=%d", len(arrived))
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		for _, origin := range fixture.options.Origins {
			for _, raw := range fixture.rows {
				source := ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: attemptHex32(sha256.Sum256(raw)), Origin: origin}
				if fixture.count(origin, source.Kind, raw) != 1 || !bytes.Equal(retained[source], raw) {
					t.Fatal("parallel capture lost origin custody or redownloaded a completed prefix")
				}
			}
		}
	})
}

// One permanent origin refusal still allows the other origin's complete tape
// to reach the sink. The combined result remains a failure with exact origin.
func TestReleaseCaptureOriginsRetainsIndependentSuccessAfterFailure(t *testing.T) {
	fixture := newCaptureOriginsTestFixture(t)
	fixture.read = func(request *http.Request, raw []byte) (io.ReadCloser, error) {
		if request.URL.Host == "left.example" && request.URL.Query().Get("kind") == AttemptStreamV2Records {
			return nil, errors.New("synthetic permanent origin failure")
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	retained := map[ReleaseEvidenceV2CaptureSource][]byte{}
	err := captureReleaseStreamOriginsV2(t.Context(), captureStreamTestBounds(), fixture.options, fixture.cuts, func(source ReleaseEvidenceV2CaptureSource, raw []byte) error {
		retained[source] = bytes.Clone(raw)
		return nil
	}, releaseCaptureStreamHooksV2{newReader: fixture.newReader})
	if err == nil || !strings.Contains(err.Error(), "https://left.example") {
		t.Fatalf("origin refusal disappeared: %v", err)
	}
	for _, raw := range fixture.rows {
		if !bytes.Equal(retained[ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: attemptHex32(sha256.Sum256(raw)), Origin: fixture.options.Origins[1]}], raw) {
			t.Fatal("one origin's failure discarded the independent successful tape")
		}
	}
}

// A truncated transport body is closed before an exact retry; completed prefix
// chunks and their origin witnesses survive while partial bytes never emit.
func TestReleaseCaptureStreamRetryRetainsCompletedPrefix(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newCaptureOriginsTestFixture(t)
		fixture.options.ParallelStreamOrigins = false
		opened, closed := 0, 0
		failed := false
		fixture.read = func(request *http.Request, raw []byte) (io.ReadCloser, error) {
			if opened != closed {
				return nil, errors.New("retry did not close its prior body")
			}
			opened++
			body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader(raw).Read, close: func() error { closed++; return nil }}
			if !failed && bytes.Equal(raw, fixture.rows[1]) {
				failed = true
				body.read = func(value []byte) (int, error) { return copy(value, raw[:3]), io.ErrUnexpectedEOF }
			}
			return body, nil
		}
		emitted := 0
		err := captureReleaseStreamOriginsV2(t.Context(), captureStreamTestBounds(), fixture.options, fixture.cuts, func(source ReleaseEvidenceV2CaptureSource, raw []byte) error {
			if source.Kind == AttemptStreamV2Records {
				emitted++
				if !bytes.Equal(raw, fixture.objects[source.Name]) {
					return errors.New("partial retry bytes acquired authority")
				}
			}
			return nil
		}, releaseCaptureStreamHooksV2{newReader: fixture.newReader})
		if err != nil || emitted != 4 || opened != closed || fixture.count(fixture.options.Origins[0], AttemptStreamV2Records, fixture.rows[0]) != 1 || fixture.count(fixture.options.Origins[0], AttemptStreamV2Records, fixture.rows[1]) != 2 {
			t.Fatalf("retry lost prefix/body custody: emitted=%d opened=%d closed=%d err=%v", emitted, opened, closed, err)
		}
	})
}

// Every attempt has a fresh one-minute context, but the complete read still
// stops at five minutes rather than multiplying a campaign's outer allowance.
func TestReleaseCaptureStreamRetryExhaustsFiniteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		capture := newReleaseCaptureStreamsV2(captureStreamTestBounds(), ReleaseEvidenceV2CaptureOptions{RetryStreamReads: true}, nil)
		var attempts []context.Context
		started := time.Now()
		_, err := capture.read(t.Context(), func(ctx context.Context) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Minute {
				return nil, errors.New("attempt lost one-minute bound")
			}
			for _, previous := range attempts {
				if previous == ctx || previous.Err() == nil {
					return nil, errors.New("attempt borrowed an unclosed context")
				}
			}
			attempts = append(attempts, ctx)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) || len(attempts) != 5 || time.Since(started) != 5*time.Minute {
			t.Fatalf("read budget changed: elapsed=%s attempts=%d err=%v", time.Since(started), len(attempts), err)
		}
	})
}

// Joined integrity/transport failures, changed hashes and failed sinks remain
// permanent. Diagnostic recovery cannot convert unavailable bytes to custody.
func TestReleaseCaptureStreamRetryRejectsIntegrityAndSinkFailures(t *testing.T) {
	for _, failure := range []string{"hash", "mixed", "close", "sink"} {
		fixture := newCaptureOriginsTestFixture(t)
		fixture.options.ParallelStreamOrigins = false
		fixture.read = func(request *http.Request, raw []byte) (io.ReadCloser, error) {
			if request.URL.Query().Get("kind") == AttemptStreamV2Records {
				if failure == "hash" {
					raw[0] ^= 1
				}
				if failure == "mixed" {
					return nil, errors.Join(context.DeadlineExceeded, errors.New("synthetic integrity failure"))
				}
				if failure == "close" {
					return &attemptStreamV2HTTPTestBody{read: bytes.NewReader(raw).Read, close: func() error { return errors.New("synthetic close failure") }}, nil
				}
			}
			return io.NopCloser(bytes.NewReader(raw)), nil
		}
		err := captureReleaseStreamOriginsV2(t.Context(), captureStreamTestBounds(), fixture.options, fixture.cuts, func(source ReleaseEvidenceV2CaptureSource, _ []byte) error {
			if failure == "sink" && source.Kind == AttemptStreamV2Records {
				return errors.New("synthetic durable sink failure")
			}
			return nil
		}, releaseCaptureStreamHooksV2{newReader: fixture.newReader, wait: func(context.Context, time.Duration) error { return errors.New("permanent failure retried") }})
		if err == nil || strings.Contains(err.Error(), "permanent failure retried") || fixture.count(fixture.options.Origins[0], AttemptStreamV2Records, fixture.rows[0]) != 1 {
			t.Fatalf("%s acquired another read or lost its failure: %v", failure, err)
		}
	}
}

// Caller cancellation releases both blocked readers and joins them before the
// capture owner returns. No background body can outlive the diagnostic check.
func TestReleaseCaptureOriginsCancellationJoinsReaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newCaptureOriginsTestFixture(t)
		var active, completed atomic.Int32
		fixture.read = func(request *http.Request, raw []byte) (io.ReadCloser, error) {
			if request.URL.Query().Get("kind") == AttemptStreamV2Records {
				active.Add(1)
				<-request.Context().Done()
				active.Add(-1)
				completed.Add(1)
				return nil, request.Context().Err()
			}
			return io.NopCloser(bytes.NewReader(raw)), nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- captureReleaseStreamOriginsV2(ctx, captureStreamTestBounds(), fixture.options, fixture.cuts, func(ReleaseEvidenceV2CaptureSource, []byte) error { return nil }, releaseCaptureStreamHooksV2{newReader: fixture.newReader})
		}()
		synctest.Wait()
		if active.Load() != 2 {
			t.Errorf("two origin readers not owned: active=%d", active.Load())
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) || active.Load() != 0 || completed.Load() != 2 {
			t.Fatalf("capture returned before joining readers: active=%d completed=%d err=%v", active.Load(), completed.Load(), err)
		}
	})
}

// Both workers spend one existing global archive budget, and a panicking sink
// still cancels and joins them rather than leaving blocked emission senders.
func TestReleaseCaptureOriginsKeepsGlobalBudgetAndSinkOwnership(t *testing.T) {
	for _, panicSink := range []bool{false, true} {
		fixture := newCaptureOriginsTestFixture(t)
		fixture.options.MaximumObjects = 5
		retained := 0
		budget, err := newReleaseEvidenceCaptureBudgetV2(t.Context(), fixture.options, func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error {
			retained++
			if panicSink {
				panic("synthetic sink interruption")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		err = captureReleaseStreamOriginsV2(t.Context(), captureStreamTestBounds(), fixture.options, fixture.cuts, budget.emit, releaseCaptureStreamHooksV2{newReader: fixture.newReader})
		if err == nil || retained > 5 || panicSink && !strings.Contains(err.Error(), "sink panicked") || !panicSink && !strings.Contains(err.Error(), "capacity") {
			t.Fatalf("parallel sink escaped its owner: panic=%t retained=%d err=%v", panicSink, retained, err)
		}
	}
}

// Strict defaults neither overlap origins nor retry a failed immutable read;
// diagnostics must opt in without changing other final-capture callers.
func TestReleaseCaptureOriginsDefaultKeepsStrictFailure(t *testing.T) {
	fixture := newCaptureOriginsTestFixture(t)
	fixture.options.ParallelStreamOrigins, fixture.options.RetryStreamReads, fixture.options.ReuseCapturedStreams = false, false, false
	fixture.read = func(request *http.Request, raw []byte) (io.ReadCloser, error) {
		if request.URL.Query().Get("kind") == AttemptStreamV2Records {
			return nil, context.DeadlineExceeded
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	err := captureReleaseStreamOriginsV2(t.Context(), captureStreamTestBounds(), fixture.options, fixture.cuts, func(ReleaseEvidenceV2CaptureSource, []byte) error { return nil }, releaseCaptureStreamHooksV2{newReader: fixture.newReader})
	if !errors.Is(err, context.DeadlineExceeded) || fixture.count(fixture.options.Origins[0], AttemptStreamV2Records, fixture.rows[0]) != 1 || fixture.count(fixture.options.Origins[1], AttemptStreamV2Records, fixture.rows[0]) != 0 {
		t.Fatalf("strict capture inherited diagnostic behavior: %v", err)
	}
}

// The retry owner honors typed server pacing within its existing operation
// deadline; neither a generic error string nor a new time budget controls it.
func TestReleaseCaptureStreamRetryKeepsTypedPacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		capture := newReleaseCaptureStreamsV2(captureStreamTestBounds(), ReleaseEvidenceV2CaptureOptions{RetryStreamReads: true}, nil)
		started, calls := time.Now(), 0
		raw, err := capture.read(t.Context(), func(context.Context) ([]byte, error) {
			calls++
			if calls == 1 {
				return nil, &attemptStreamHttpStatusError{status: http.StatusTooManyRequests, retryAfter: 75 * time.Second}
			}
			return []byte("synthetic complete body"), nil
		})
		if err != nil || calls != 2 || time.Since(started) != 75*time.Second || string(raw) != "synthetic complete body" {
			t.Fatalf("typed pacing changed: calls=%d elapsed=%s raw=%q err=%v", calls, time.Since(started), raw, err)
		}
	})
}
