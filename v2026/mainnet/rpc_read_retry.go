// Public read endpoints can require a longer recovery interval than transport
// backoff. Retry hints delay reads within the existing total deadline only.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Integer/date Retry-After and the archive's JSON seconds hint may lengthen a
// transient HTTP retry. Conflicting hints choose the longer bounded delay;
// malformed hints retain ordinary backoff and cannot extend the deadline.
func rpcReadRetryDelay(header http.Header, body []byte, fallback time.Duration, now, deadline time.Time) time.Duration {
	maximum := max(time.Duration(0), deadline.Sub(now))
	delay := min(fallback, maximum)
	secondsDelay := func(value string) time.Duration {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return 0
		}
		if seconds > uint64(maximum/time.Second) {
			return maximum
		}
		return min(time.Duration(seconds)*time.Second, maximum)
	}
	for _, value := range header.Values("Retry-After") {
		value = strings.TrimSpace(value)
		delay = max(delay, secondsDelay(value))
		if at, err := http.ParseTime(value); err == nil {
			delay = max(delay, min(at.Sub(now), maximum))
		}
	}
	if protocol.ValidateUniqueJsonKeys(body) == nil {
		var hint struct {
			Seconds json.Number `json:"retry_after_seconds"`
		}
		if json.Unmarshal(body, &hint) == nil {
			delay = max(delay, secondsDelay(hint.Seconds.String()))
		}
	}
	return delay
}

// The context deadline and cancellation always win over a remote retry hint.
// A client may substitute the wait boundary in deterministic transport tests.
func waitRpcReadRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
