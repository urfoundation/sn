// These controls retain the real client timeout and transport. The six-second
// server delay represents actual response latency beyond the former five-second
// limit; barriers and complete response checks establish every other ordering.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestMonitorProgressActualClientClipsAttemptToOriginalOwner(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, ownerBudget := range []time.Duration{20 * time.Second, 300 * time.Second} {
			fixture := newMonitorProgressReadFixture(t, kind)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(fixture.body) }))
			client := newMonitorProviderClient()
			transport := client.Transport
			ctx, cancel := context.WithTimeout(t.Context(), ownerBudget)
			ownerDeadline, _ := ctx.Deadline()
			calls := 0
			var remaining time.Duration
			client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				deadline, present := request.Context().Deadline()
				remaining = time.Until(deadline)
				if !present || deadline.After(ownerDeadline) || remaining < min(ownerBudget, time.Minute)-time.Second || remaining > time.Minute {
					return nil, errors.New("actual progress request lost its usable attempt or parent deadline")
				}
				return transport.RoundTrip(request)
			})
			got, code := fixture.read(ctx, client, source.URL, monitorProgressReadClock{})
			cancel()
			client.CloseIdleConnections()
			source.Close()
			if !got || code != "ok" || calls != 1 {
				t.Fatal("actual client could not complete within its clipped read window", kind, ownerBudget, remaining, got, code, calls)
			}
		}
	}
}

// Real server latency crosses the old timer. A short-attempt failure ends at
// the owned retry seam, so it cannot be hidden by a second faster response.
func monitorProgressSlowResponse(t *testing.T, partial bool) {
	t.Helper()
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		var calls atomic.Int32
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			raw := fixture.body
			if partial {
				_, _ = w.Write(raw[:len(raw)/2])
				w.(http.Flusher).Flush()
				raw = raw[len(raw)/2:]
			}
			latency := time.NewTimer(6 * time.Second)
			defer latency.Stop()
			select {
			case <-latency.C:
				_, _ = w.Write(raw)
			case <-request.Context().Done():
			}
		}))
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		waits := 0
		got, code := fixture.read(ctx, newMonitorProviderClient(), source.URL, monitorProgressReadClock{wait: func(context.Context, time.Duration) error {
			waits++
			return context.DeadlineExceeded
		}})
		cancel()
		source.Close()
		if !got || code != "ok" || calls.Load() != 1 || waits != 0 {
			t.Fatal("healthy slow response was repeatedly cut below the owned read budget", kind, partial, got, code, calls.Load(), waits)
		}
	}
}

func TestMonitorProgressSlowHeadersCompleteOneAttempt(t *testing.T) {
	monitorProgressSlowResponse(t, false)
}

func TestMonitorProgressSlowPartialBodyCompletesOneAttempt(t *testing.T) {
	monitorProgressSlowResponse(t, true)
}

// The first admitted body byte and synchronous Close are explicit barriers.
type monitorProgressObservedBody struct {
	io.ReadCloser
	once   sync.Once
	read   chan struct{}
	closes *atomic.Int32
}

func (self *monitorProgressObservedBody) Read(raw []byte) (int, error) {
	n, err := self.ReadCloser.Read(raw)
	if n > 0 {
		self.once.Do(func() { close(self.read) })
	}
	return n, err
}

func (self *monitorProgressObservedBody) Close() error {
	err := self.ReadCloser.Close()
	self.closes.Add(1)
	return err
}

func TestMonitorProgressPartialBodyCancellationJoinsAttempt(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		var calls, closes atomic.Int32
		serverJoined := make(chan struct{})
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			defer close(serverJoined)
			calls.Add(1)
			_, _ = w.Write(fixture.body[:len(fixture.body)/2])
			w.(http.Flusher).Flush()
			<-request.Context().Done()
		}))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		read := make(chan struct{})
		client := newMonitorProviderClient()
		transport := client.Transport
		client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response, err := transport.RoundTrip(request)
			if response != nil && response.Body != nil {
				response.Body = &monitorProgressObservedBody{ReadCloser: response.Body, read: read, closes: &closes}
			}
			return response, err
		})
		waits := 0
		done := make(chan string, 1)
		go func() {
			got, code := fixture.read(ctx, client, source.URL, monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }})
			if got {
				code = "invented-partial-sample"
			}
			done <- code
		}()
		select {
		case <-read:
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatal("real partial progress body was not read")
		}
		cancel()
		select {
		case code := <-done:
			if code != "unavailable" || calls.Load() != 1 || closes.Load() != 1 || waits != 0 {
				t.Fatal("canceled partial body escaped its original request", kind, code, calls.Load(), closes.Load(), waits)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("real partial progress read did not join cancellation")
		}
		<-serverJoined
		source.Close()
	}
}

func TestMonitorProgressOwnedWorkersUseActualClientReadWindow(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorServicesFixture(t)
		var raw []byte
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) }))
		provider := monitorProviderTestValue(fixture.clock.now())
		claim := monitorClaimTestValue(fixture.clock.now())
		providerPolicy := monitorProviderTestPolicy(provider, source.URL+"/provider-progress")
		claimPolicy := monitorClaimTestPolicy(t, claim, source.URL+"/claim-progress", fixture.clock.now())
		if kind == "provider" {
			raw, _ = json.Marshal(provider)
			fixture.policy.Providers = []monitorProviderPolicy{providerPolicy}
		} else {
			raw, _ = json.Marshal(claim)
			fixture.policy.Claims = []monitorClaimPolicy{claimPolicy}
		}
		fixture.writePolicy(t)
		ctx, cancel := context.WithCancel(monitorTestStorageContext(t, t.Context(), fixture.args("https://rpc.example")))
		defer cancel()
		calls := 0
		wrap := func(client *http.Client) {
			transport := client.Transport
			client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				deadline, present := request.Context().Deadline()
				if !present || time.Until(deadline) < 30*time.Second {
					return nil, errors.New("owned worker still slices a healthy read below thirty seconds")
				}
				return transport.RoundTrip(request)
			})
		}
		hooks := monitorServiceHooks{afterEvent: func(context.Context, string) { cancel() }}
		var output bytes.Buffer
		exit := 0
		if kind == "provider" {
			worker, err := openMonitorProviderWorker(ctx, providerPolicy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, hooks)
			if err != nil {
				t.Fatal(err)
			}
			wrap(worker.client)
			exit = worker.run(ctx, time.Minute, &output, io.Discard, fixture.clock.now, hooks)
			if err := worker.close(hooks); err != nil {
				t.Fatal(err)
			}
		} else {
			worker, err := openMonitorClaimWorker(ctx, claimPolicy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, hooks)
			if err != nil {
				t.Fatal(err)
			}
			wrap(worker.client)
			exit = worker.run(ctx, time.Minute, &output, io.Discard, fixture.clock.now, hooks)
			if err := worker.close(hooks); err != nil {
				t.Fatal(err)
			}
		}
		source.Close()
		var event monitorProgressRetryEvent
		if err := json.Unmarshal(output.Bytes(), &event); err != nil {
			t.Fatal(err, output.String())
		}
		if exit != 0 || calls != 1 || !event.Current || !event.CheckpointCurrent || event.Status != "ok" {
			t.Fatal("owned worker could not publish using actual client attempt budget", kind, exit, calls, event)
		}
	}
}

func TestMonitorProgressTypedNetworkOutagesRecoverWithinOwner(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, cause := range []error{syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ENETDOWN, syscall.ENETRESET, syscall.ECONNABORTED, &net.DNSError{Name: "monitor.example", IsTemporary: true}} {
			fixture := newMonitorProgressReadFixture(t, kind)
			calls, waits := 0, 0
			client := newMonitorProviderClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: cause}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(fixture.body))}, nil
			})
			got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return nil }})
			if !got || code != "ok" || calls != 2 || waits != 1 {
				t.Fatal("typed transient network outage escaped owned retry", kind, cause, got, code, calls, waits)
			}
		}
	}
}

func TestMonitorProgressPermanentDnsDominatesTemporaryCauses(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, cause := range []error{&net.DNSError{Name: "absent.example", IsNotFound: true, IsTemporary: true, UnwrapErr: context.DeadlineExceeded}, errors.Join(syscall.ENETUNREACH, &net.DNSError{Name: "absent.example", IsNotFound: true}), errors.Join(syscall.ECONNABORTED, context.Canceled)} {
			fixture := newMonitorProgressReadFixture(t, kind)
			calls, waits := 0, 0
			client := newMonitorProviderClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, cause })
			got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }})
			if got || code != "unavailable" || calls != 1 || waits != 0 {
				t.Fatal("permanent DNS or cancellation borrowed temporary retry permission", kind, cause, got, code, calls, waits)
			}
		}
	}
}
