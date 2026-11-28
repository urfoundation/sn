// A transport body can return its final bytes and an error together. These
// controls run the real decoders and retry loop, including joined Close errors.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

type monitorProgressTerminalBody struct {
	raw      []byte
	readErr  error
	closeErr error
	closes   *atomic.Int32
}

func (self *monitorProgressTerminalBody) Read(target []byte) (int, error) {
	if len(self.raw) == 0 {
		return 0, io.EOF
	}
	n := copy(target, self.raw)
	self.raw = self.raw[n:]
	if len(self.raw) == 0 {
		return n, self.readErr
	}
	return n, nil
}

func (self *monitorProgressTerminalBody) Close() error { self.closes.Add(1); return self.closeErr }

func TestMonitorProgressBodyHardCausesDoNotBorrowRetryPermission(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, status := range []int{200, 429, 503} {
			for _, seam := range []string{"read", "close"} {
				if status != 200 && seam == "read" {
					continue
				}
				for _, cause := range []error{errors.New("synthetic permanent body refusal"), errors.Join(context.DeadlineExceeded, errors.New("synthetic permanent body refusal")), errors.Join(syscall.EIO, context.Canceled)} {
					fixture := newMonitorProgressReadFixture(t, kind)
					var closes atomic.Int32
					body := &monitorProgressTerminalBody{raw: fixture.body, closes: &closes}
					if seam == "read" {
						body.readErr = cause
					} else {
						body.closeErr = cause
					}
					calls, waits := 0, 0
					client := newMonitorProviderClient()
					client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
						calls++
						return &http.Response{StatusCode: status, Body: body}, nil
					})
					got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }})
					if got || code != "unavailable" || calls != 1 || closes.Load() != 1 || waits != 0 {
						t.Fatal("permanent body cause borrowed status or sibling retry permission", kind, status, seam, cause, got, code, calls, closes.Load(), waits)
					}
				}
			}
		}
	}
}

func TestMonitorProgressCompleteIdentityDominatesTerminalReadFault(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, cause := range []error{io.ErrUnexpectedEOF, context.DeadlineExceeded, errors.Join(syscall.EIO, errors.New("synthetic permanent read refusal"))} {
			fixture := newMonitorProgressReadFixture(t, kind)
			var raw []byte
			if kind == "provider" {
				var value protocol.ProviderProgress
				if err := json.Unmarshal(fixture.body, &value); err != nil {
					t.Fatal(err)
				}
				value.Source.ConfigHash = strings.Repeat("4", 64)
				raw, _ = json.Marshal(value)
			} else {
				var value protocol.ClaimProgress
				if err := json.Unmarshal(fixture.body, &value); err != nil {
					t.Fatal(err)
				}
				value.Member = "foreign-member"
				raw, _ = json.Marshal(value)
			}
			var closes atomic.Int32
			calls, waits := 0, 0
			client := newMonitorProviderClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: &monitorProgressTerminalBody{raw: raw, readErr: cause, closeErr: syscall.EIO, closes: &closes}}, nil
			})
			got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }})
			if !got || code != "identity" || calls != 1 || closes.Load() != 1 || waits != 0 {
				t.Fatal("complete foreign identity was hidden by final read or close error", kind, cause, got, code, calls, closes.Load(), waits)
			}
		}
	}
}

func TestMonitorProgressPartialTransientBodyRetainsOwnedRecovery(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, cause := range []error{io.ErrUnexpectedEOF, context.DeadlineExceeded, syscall.EIO} {
			fixture := newMonitorProgressReadFixture(t, kind)
			var closes atomic.Int32
			calls, waits := 0, 0
			client := newMonitorProviderClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				body := &monitorProgressTerminalBody{raw: fixture.body, closes: &closes}
				if calls == 1 {
					body.raw, body.readErr = fixture.body[:len(fixture.body)/2], cause
				}
				return &http.Response{StatusCode: 200, Body: body}, nil
			})
			got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error {
				waits++
				if closes.Load() != 1 {
					return errors.New("original partial body was not closed before retry")
				}
				return nil
			}})
			if !got || code != "ok" || calls != 2 || closes.Load() != 2 || waits != 1 {
				t.Fatal("partial transient body became authoritative invalid data or escaped its retry owner", kind, cause, got, code, calls, closes.Load(), waits)
			}
		}
	}
}

func TestMonitorProgressCloseIoRetainsRetryButNotMixedHardCause(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		var closes atomic.Int32
		calls, waits := 0, 0
		client := newMonitorProviderClient()
		client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			cause := error(nil)
			if calls == 1 {
				cause = syscall.EIO
			}
			return &http.Response{StatusCode: 200, Body: &monitorProgressCloseBody{Reader: bytes.NewReader(fixture.body), closes: &closes, err: cause}}, nil
		})
		got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return nil }})
		if !got || code != "ok" || calls != 2 || closes.Load() != 2 || waits != 1 {
			t.Fatal("known I/O close fault lost its bounded recovery", kind, got, code, calls, closes.Load(), waits)
		}
	}
}
