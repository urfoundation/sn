// Authenticated evidence routes own finite read retries and byte-scaled request
// time. The shared HTTP client and ordinary probe timeout remain unchanged.
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
	evidenceRequestMaximumAttempts  = 3
	evidenceLargeRequestMaximumTime = 20 * time.Minute
	evidenceRequestRetryDelay       = 250 * time.Millisecond
)

// The server reserves two minutes plus one second per started 8 MiB of the
// admitted envelope. Add thirty seconds for the client round trip, then cap
// the whole request owner at twenty minutes. Ordinary reads keep thirty seconds.
func evidenceRequestTimeout(kind string, maximum int64) time.Duration {
	if maximum <= maximumCampaignEvidenceEnvelopeBytes || kind != campaignEvidenceFileKind && kind != finalSemanticSupplementFileKind {
		return 30 * time.Second
	}
	const quantum int64 = 8 * 1024 * 1024
	seconds := maximum / quantum
	if maximum%quantum != 0 {
		seconds++
	}
	if seconds >= int64((evidenceLargeRequestMaximumTime-150*time.Second)/time.Second) {
		return evidenceLargeRequestMaximumTime
	}
	return 150*time.Second + time.Duration(seconds)*time.Second
}

// The exact route/hash, finite body limit and parent deadline survive every
// retry. Parsing and identity checks occur after this transport-only owner.
func (self *liveScenarioProbe) getCampaignEvidence(ctx context.Context, endpoint, kind string, maximum int64, limits campaignEvidenceLimits) ([]byte, int, error) {
	return self.getCampaignEvidenceWithWait(ctx, endpoint, kind, maximum, limits, waitFinalSemanticRPCRetry)
}

// An invocation-local wait seam permits deterministic cancellation/exhaustion
// tests without replacing transport, decoding or authentication results. An
// interrupted retry retains the last request's read/close failure.
func (self *liveScenarioProbe) getCampaignEvidenceWithWait(ctx context.Context, endpoint, kind string, maximum int64, limits campaignEvidenceLimits, wait func(context.Context, time.Duration) error) ([]byte, int, error) {
	if self == nil || ctx == nil || wait == nil || maximum <= 0 || uint64(maximum) > limits.maximumEnvelopeBytes() {
		return nil, 0, errors.New("campaign evidence request authority is invalid")
	}
	if err := limits.validate(); err != nil {
		return nil, 0, err
	}
	client := self.client
	if client == nil {
		client = http.DefaultClient
	}
	owned := *client
	owned.Timeout = evidenceRequestTimeout(kind, maximum)
	if owned.Timeout == 30*time.Second && client.Timeout > 0 {
		owned.Timeout = min(owned.Timeout, client.Timeout)
	}
	budget := min(evidenceLargeRequestMaximumTime, time.Duration(evidenceRequestMaximumAttempts)*owned.Timeout)
	requestCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var status int
	var failure error
	for attempt := 1; attempt <= evidenceRequestMaximumAttempts; attempt++ {
		if err := requestCtx.Err(); err != nil {
			return nil, status, errors.Join(failure, err)
		}
		raw, code, err := getScenarioProbeWithClient(requestCtx, &owned, endpoint, maximum)
		status, failure = code, err
		if err := requestCtx.Err(); err != nil {
			return nil, status, errors.Join(failure, err)
		}
		if failure == nil {
			return raw, status, nil
		}
		transient := scenarioSnapshotTransportError(failure, true)
		var response *evidenceRequestStatusError
		if errors.As(failure, &response) {
			switch response.status {
			case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				transient = true
			}
		}
		if !transient || attempt == evidenceRequestMaximumAttempts {
			return nil, status, failure
		}
		fmt.Fprintf(os.Stderr, "sim-testnet: campaign evidence GET transient attempt %d/%d; retrying the same route: %v\n", attempt, evidenceRequestMaximumAttempts, failure)
		if err := wait(requestCtx, evidenceRequestRetryDelay*time.Duration(attempt)); err != nil {
			return nil, status, errors.Join(failure, err, requestCtx.Err())
		}
	}
	return nil, status, failure
}

// A status is retryable only after its complete bounded body and Close succeed.
type evidenceRequestStatusError struct{ status int }

// Preserve the existing human-readable status diagnostic.
func (self *evidenceRequestStatusError) Error() string { return fmt.Sprintf("HTTP %d", self.status) }
