package onchain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/ledger"
)

// The package's tests never sleep between v2Get attempts; the tests below
// record the waits instead.
func init() {
	v2GetSleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
}

// v2GetFixture answers each request with the next of its answers, repeating
// the last, and records how often it was asked and what v2Get waited.
type v2GetFixture struct {
	server   *httptest.Server
	mu       sync.Mutex
	paths    []string
	answers  []func(http.ResponseWriter)
	waits    []time.Duration
	interval func(context.Context, time.Duration) error
}

func newV2GetFixture(t *testing.T, answers ...func(http.ResponseWriter)) *v2GetFixture {
	t.Helper()
	f := &v2GetFixture{answers: answers}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		i := len(f.paths)
		f.paths = append(f.paths, r.URL.RequestURI())
		f.mu.Unlock()
		f.answers[min(i, len(f.answers)-1)](w)
	}))
	t.Cleanup(f.server.Close)
	previous := v2GetSleep
	v2GetSleep = func(ctx context.Context, d time.Duration) error {
		f.mu.Lock()
		f.waits = append(f.waits, d)
		interval := f.interval
		f.mu.Unlock()
		if interval != nil {
			return interval(ctx, d)
		}
		return ctx.Err()
	}
	t.Cleanup(func() { v2GetSleep = previous })
	return f
}

func v2GetStatus(code int, header ...string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		http.Error(w, http.StatusText(code), code)
	}
}

func v2GetBody(data []byte) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { _, _ = w.Write(data) }
}

func (f *v2GetFixture) get(ctx context.Context) (string, error) {
	u, err := url.Parse(f.server.URL + "/sn/ledger/object?hash=x")
	if err != nil {
		return "", err
	}
	body, err := v2Get(ctx, u)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	return string(data), err
}

func (f *v2GetFixture) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

// A 429 from the load balancer's per-source rate limit is not the endpoint
// withholding the object: the read waits a jittered, doubling backoff and
// tries again.
func TestV2GetRetriesARateLimitedRead(t *testing.T) {
	f := newV2GetFixture(t, v2GetStatus(http.StatusTooManyRequests), v2GetStatus(http.StatusTooManyRequests), v2GetBody([]byte("object")))
	got, err := f.get(context.Background())
	if err != nil || got != "object" || f.requests() != 3 {
		t.Fatalf("got %q, %v after %d requests", got, err, f.requests())
	}
	if len(f.waits) != 2 || f.waits[0] < v2GetFirstDelay/2 || 3*v2GetFirstDelay/2 < f.waits[0] || f.waits[1] < v2GetFirstDelay || 3*v2GetFirstDelay < f.waits[1] {
		t.Fatalf("waits %v, want about 1 s then about 2 s", f.waits)
	}
}

// A reply's Retry-After decides the wait, in seconds or as an HTTP date, and
// is capped.
func TestV2GetWaitsTheRetryAfter(t *testing.T) {
	for _, c := range []struct {
		name     string
		after    string
		min, max time.Duration
	}{
		{"seconds", "7", 7 * time.Second, 7 * time.Second},
		{"date", time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat), 0, 3 * time.Second},
		{"capped", "600", v2GetMaxRetryAfter, v2GetMaxRetryAfter},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newV2GetFixture(t, v2GetStatus(http.StatusTooManyRequests, "Retry-After", c.after), v2GetBody([]byte("object")))
			if got, err := f.get(context.Background()); err != nil || got != "object" {
				t.Fatalf("got %q, %v", got, err)
			}
			if len(f.waits) != 1 || f.waits[0] < c.min || c.max < f.waits[0] {
				t.Fatalf("waits %v, want between %v and %v", f.waits, c.min, c.max)
			}
		})
	}
}

// A transient server refusal is retried like a rate limit.
func TestV2GetRetriesTransientServerRefusals(t *testing.T) {
	f := newV2GetFixture(t, v2GetStatus(http.StatusServiceUnavailable), v2GetStatus(http.StatusBadGateway), v2GetStatus(http.StatusGatewayTimeout),
		v2GetStatus(http.StatusInternalServerError), v2GetStatus(http.StatusRequestTimeout), v2GetBody([]byte("object")))
	if got, err := f.get(context.Background()); err != nil || got != "object" || f.requests() != 6 {
		t.Fatalf("got %q, %v after %d requests", got, err, f.requests())
	}
}

// After its attempts the read fails with the last refusal, as one read failed
// before.
func TestV2GetGivesUpAfterItsAttempts(t *testing.T) {
	f := newV2GetFixture(t, v2GetStatus(http.StatusTooManyRequests))
	_, err := f.get(context.Background())
	if err == nil || !strings.HasSuffix(err.Error(), ": 429 Too Many Requests") || f.requests() != v2GetAttempts || len(f.waits) != v2GetAttempts-1 {
		t.Fatalf("err %v after %d requests and waits %v", err, f.requests(), f.waits)
	}
	for _, wait := range f.waits {
		if 3*v2GetMaxDelay/2 < wait {
			t.Fatalf("wait %v above the cap", wait)
		}
	}
}

// A 404 is the endpoint not serving the object, which the check acts on: it
// returns at once, as does any other refusal that does not ask to be retried.
func TestV2GetReturnsOtherRefusalsAtOnce(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusBadRequest, http.StatusForbidden} {
		f := newV2GetFixture(t, v2GetStatus(code))
		_, err := f.get(context.Background())
		if err == nil || !strings.HasSuffix(err.Error(), fmt.Sprintf(": %d %s", code, http.StatusText(code))) || f.requests() != 1 || len(f.waits) != 0 {
			t.Fatalf("%d: err %v after %d requests and waits %v", code, err, f.requests(), f.waits)
		}
	}
}

// No wait runs past the read's deadline: a call with too little time left
// fails with the refusal instead of waiting.
func TestV2GetStopsAtTheDeadline(t *testing.T) {
	f := newV2GetFixture(t, v2GetStatus(http.StatusTooManyRequests, "Retry-After", "30"), v2GetBody([]byte("object")))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := f.get(ctx)
	if err == nil || !strings.HasSuffix(err.Error(), ": 429 Too Many Requests") || f.requests() != 1 || len(f.waits) != 0 {
		t.Fatalf("err %v after %d requests and waits %v", err, f.requests(), f.waits)
	}
}

// A read canceled while it waits returns the refusal joined with the cause.
func TestV2GetStopsWhenCanceledWhileWaiting(t *testing.T) {
	f := newV2GetFixture(t, v2GetStatus(http.StatusTooManyRequests), v2GetBody([]byte("object")))
	ctx, cancel := context.WithCancel(context.Background())
	f.interval = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	_, err := f.get(ctx)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), ": 429 Too Many Requests") || f.requests() != 1 {
		t.Fatalf("err %v after %d requests", err, f.requests())
	}
}

// The watcher's object read, as `snclaim v2 check` fetches through v2Objects,
// survives a rate limit and still checks the bytes against the reference.
func TestV2ObjectsReadSurvivesARateLimit(t *testing.T) {
	data := []byte(`{"ledger":"object"}`)
	ref := ledger.RefOf(data)
	f := newV2GetFixture(t, v2GetStatus(http.StatusTooManyRequests), v2GetBody(data))
	api, err := url.Parse(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v2ReadObject(context.Background(), v2Objects{api: api}, ref, 1<<20)
	if err != nil || string(got) != string(data) || f.requests() != 2 {
		t.Fatalf("got %q, %v after %d requests", got, err, f.requests())
	}
	want := "/sn/ledger/object?" + url.Values{"hash": {ref.String()}}.Encode()
	if f.paths[0] != want || f.paths[1] != want {
		t.Fatalf("paths %v, want %s twice", f.paths, want)
	}
}
