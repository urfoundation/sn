// Live operator observation retries retain the exact endpoint and byte bound.
// They do not retry decoding, authentication, or any state-changing request.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	scenarioOperatorReadTimeout           = 60 * time.Second
	scenarioOperatorReadRetryTimeout      = 300 * time.Second
	scenarioOperatorReadRetryDelay        = 250 * time.Millisecond
	scenarioOperatorReadMaximumRetryDelay = 5 * time.Second
)

// Every cause must independently denote a transient read failure. In
// particular, a joined status and malformed body cannot authorize retry.
func scenarioOperatorReadTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !scenarioOperatorReadTransient(cause) {
				return false
			}
		}
		return true
	}
	if status, ok := err.(*evidenceRequestStatusError); ok {
		switch status.status {
		case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	return scenarioSnapshotTransportError(err, true)
}

// This owner copies the client instead of extending its shared timeout. Each
// attempt gets fresh time inside one operation budget; cancellation always wins.
func (self *liveScenarioProbe) readOperatorSurface(ctx context.Context, endpoint string, maximum int64) scenarioOperatorRead {
	result := scenarioOperatorRead{}
	if self == nil || ctx == nil || maximum <= 0 {
		result.err = errors.New("operator surface read owner or byte bound is invalid")
		return result
	}
	client := self.client
	if client == nil {
		client = http.DefaultClient
	}
	owned := *client
	owned.Timeout = scenarioOperatorReadTimeout
	operationCtx, cancelOperation := context.WithTimeout(ctx, scenarioOperatorReadRetryTimeout)
	defer cancelOperation()
	delay := scenarioOperatorReadRetryDelay
	for {
		if err := operationCtx.Err(); err != nil {
			result.data, result.err = nil, errors.Join(result.err, err)
			return result
		}
		attemptCtx, cancelAttempt := context.WithTimeout(operationCtx, scenarioOperatorReadTimeout)
		result.attempts++
		result.data, result.status, result.err = getScenarioProbeWithClient(attemptCtx, &owned, endpoint, maximum)
		if result.err == nil {
			result.err = attemptCtx.Err()
		}
		cancelAttempt()
		transient := scenarioOperatorReadTransient(result.err)
		if transient {
			result.transientFailures++
		}
		if err := operationCtx.Err(); err != nil {
			result.data, result.err = nil, errors.Join(result.err, err)
			return result
		}
		if result.err == nil || !transient {
			return result
		}
		result.data = nil
		fmt.Fprintf(os.Stderr, "sim-testnet: operator observation GET transient attempt %d (timeout %s, total budget %s); retrying the same surface: %v\n", result.attempts, scenarioOperatorReadTimeout, scenarioOperatorReadRetryTimeout, result.err)
		if err := waitFinalSemanticRPCRetry(operationCtx, delay); err != nil {
			result.err = errors.Join(result.err, err)
			return result
		}
		delay = min(delay*2, scenarioOperatorReadMaximumRetryDelay)
	}
}
