package onchain

// v2_get.go — the reads of the operator's public ledger endpoints, GET
// /sn/ledger/account and GET /sn/ledger/object, with the retry of a refusal
// that asks to be tried again.

import (
	"context"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// v2GetAttempts bounds the attempts of one read.
	v2GetAttempts = 6
	// v2GetFirstDelay is the first backoff, doubled after each refusal up to
	// v2GetMaxDelay and jittered by half either way.
	v2GetFirstDelay = time.Second
	v2GetMaxDelay   = 30 * time.Second
	// v2GetMaxRetryAfter caps the wait a reply's Retry-After asks for.
	v2GetMaxRetryAfter = 60 * time.Second
	// v2GetDrainLimit bounds the bytes of a refusal read before closing it,
	// so its connection can be reused.
	v2GetDrainLimit = 64 << 10
)

// v2GetSleep waits d, or until ctx ends. Tests replace it.
var v2GetSleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// v2Get opens the body of a 200 answer to GET u. The request is built
// from the parsed URL, which leaves nothing to refuse.
//
// A refusal that asks to be tried again later is retried: 429, which the
// load balancer's per-source rate limit answers when a source's reads arrive
// faster than its budget refills (a check's object reads can, and so can
// other traffic sharing the source's address), and 408, 425, 500, 502, 503 and
// 504. Each retry waits the reply's Retry-After when it has one, else a
// jittered exponential backoff; there are at most v2GetAttempts attempts, and
// no wait runs past ctx's deadline. Any other status returns at once, a 404
// in particular, which is the endpoint not serving the object, as does a
// transport error.
func v2Get(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	delay := v2GetFirstDelay
	for attempt := 1; ; attempt++ {
		response, err := v2HTTP.Do((&http.Request{Method: http.MethodGet, URL: u, Header: http.Header{}}).WithContext(ctx))
		if err != nil {
			return nil, err
		}
		if response.StatusCode == http.StatusOK {
			return response.Body, nil
		}
		wait, retry := v2GetRetry(response, delay)
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, v2GetDrainLimit))
		response.Body.Close()
		refused := fmt.Errorf("GET %s: %s", u, response.Status)
		if !retry || attempt == v2GetAttempts {
			return nil, refused
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= wait {
			return nil, refused
		}
		if err := v2GetSleep(ctx, wait); err != nil {
			return nil, errors.Join(refused, err)
		}
		delay = min(2*delay, v2GetMaxDelay)
	}
}

// v2GetRetry reports whether a refusal asks to be retried and how long to
// wait first: its Retry-After, in seconds or as an HTTP date and capped at
// v2GetMaxRetryAfter, else delay jittered to between half and one and a half
// times itself.
func v2GetRetry(response *http.Response, delay time.Duration) (time.Duration, bool) {
	switch response.StatusCode {
	case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusTooEarly,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
	default:
		return 0, false
	}
	if after := strings.TrimSpace(response.Header.Get("Retry-After")); after != "" {
		if seconds, err := strconv.Atoi(after); err == nil && 0 <= seconds {
			return min(time.Duration(seconds)*time.Second, v2GetMaxRetryAfter), true
		}
		if at, err := http.ParseTime(after); err == nil {
			return min(max(time.Until(at), 0), v2GetMaxRetryAfter), true
		}
	}
	return delay/2 + time.Duration(mrand.Int64N(int64(delay)+1)), true
}
