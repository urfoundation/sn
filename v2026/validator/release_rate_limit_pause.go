// An upstream that refuses for load sets the pace of the owner's next attempt.
// The pause never decides whether an outcome is a wait or a failure; the cause
// classifiers do that. It only keeps a retry from following a 429 at the poll
// interval, which on 2026-11-15 kept the load balancer's limit tripped.
package validator

import (
	"net/http"
	"reflect"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// The pause an attempt's error asks for before the next attempt: the longest
// Retry-After that a 429 or 503 refusal in the graph carried, cut to
// RetryAfterLimit, or RateLimitPause when a 429 carried none. Zero when no
// refusal in the graph was for load. A 503 without a hint keeps the ordinary
// pace, since an api instance's own unavailability clears quickly.
//
// The walk is bounded like the cause classifiers. It reads only this package's
// typed refusals and connect's typed status; no method of an unknown error but
// Unwrap is called.
func releaseRateLimitPause(err error, settings ReleaseSteeringSettings) time.Duration {
	hint := time.Duration(0)
	rateLimited := false
	remaining := 512
	var visit func(error, int)
	visit = func(cause error, depth int) {
		cause = releaseObservedValue(cause)
		if cause == nil || depth > 32 || remaining <= 0 {
			return
		}
		remaining--
		value := reflect.ValueOf(cause)
		switch value.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if value.IsNil() {
				return
			}
		}
		status, retryAfter := 0, time.Duration(0)
		switch refusal := cause.(type) {
		case *attemptStreamHttpStatusError:
			status, retryAfter = refusal.status, refusal.retryAfter
		case *releaseHttpGetStatusError:
			status, retryAfter = refusal.status, refusal.retryAfter
		case *clientKeyObservationHttpStatusError:
			status, retryAfter = refusal.status, refusal.retryAfter
		case *connect.HttpStatusError:
			status = refusal.StatusCode
		}
		if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
			hint = max(hint, retryAfter)
			rateLimited = rateLimited || status == http.StatusTooManyRequests
			return
		}
		switch wrapper := cause.(type) {
		case interface{ Unwrap() []error }:
			causes := wrapper.Unwrap()
			if len(causes) > 128 {
				return
			}
			for _, child := range causes {
				visit(child, depth+1)
			}
		case interface{ Unwrap() error }:
			visit(wrapper.Unwrap(), depth+1)
		}
	}
	visit(err, 0)
	if settings.RetryAfterLimit > 0 {
		hint = min(hint, settings.RetryAfterLimit)
	} else {
		hint = 0
	}
	if hint == 0 && rateLimited && settings.RateLimitPause > 0 {
		return settings.RateLimitPause
	}
	return hint
}
