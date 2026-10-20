// Immutable stream refusals retain structured status and bounded server pacing.
// Lifecycle owners decide whether to retry; transport never repeats a write.
package validator

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Only transport status controls retryability. Untrusted response text cannot
// turn an authorization, content conflict or malformed acknowledgement into
// a transient failure.
type attemptStreamHttpStatusError struct {
	status     int
	upload     bool
	detail     string
	retryAfter time.Duration
}

// Keeps existing operator diagnostics without granting response text authority.
func (self *attemptStreamHttpStatusError) Error() string {
	if !self.upload {
		return fmt.Sprintf("attempt stream HTTP response status is %d", self.status)
	}
	if self.detail == "" {
		return fmt.Sprintf("attempt upload response status is %d", self.status)
	}
	return fmt.Sprintf("attempt upload response status is %d: %q", self.status, self.detail)
}

// Reserved upload counters reset within one hour. Honor one positive integer
// server hint up to that bound; malformed or duplicate hints use normal pacing.
func attemptStreamHttpRetryAfter(header http.Header) time.Duration {
	values := header.Values("Retry-After")
	if len(values) != 1 {
		return 0
	}
	value := strings.TrimSpace(values[0])
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil || seconds == 0 || strconv.FormatUint(seconds, 10) != value {
		return 0
	}
	return time.Duration(min(seconds, uint64(3600))) * time.Second
}

// A replica failure can join several independent capacity refusals. Wait for
// the longest admitted hint while retaining the existing finite attempt count.
func releaseSnapshotRetryDelayForError(err error) time.Duration {
	delay := releaseSnapshotStartupRetryDelay
	var observe func(error)
	observe = func(cause error) {
		if status, ok := cause.(*attemptStreamHttpStatusError); ok {
			delay = max(delay, min(status.retryAfter, time.Hour))
		}
		if joined, ok := cause.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				observe(child)
			}
		} else if wrapped, ok := cause.(interface{ Unwrap() error }); ok {
			observe(wrapped.Unwrap())
		}
	}
	observe(err)
	return delay
}
