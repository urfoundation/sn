// Real request contexts and body outcomes exercise per-route budget ownership;
// tests do not sleep or allocate the declared large evidence body.
package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Typed envelope bytes select time only; ordinary routes and overflow-sized
// declarations cannot enlarge the twenty-minute ceiling.
func TestEvidenceRequestBudgetTypedBoundary(t *testing.T) {
	for _, sample := range []struct {
		kind string
		size int64
		want time.Duration
	}{
		{campaignEvidenceFileKind, maximumCampaignEvidenceEnvelopeBytes, 30 * time.Second},
		{campaignEvidenceFileKind, maximumCampaignEvidenceEnvelopeBytes + 1, 159 * time.Second},
		{finalSemanticSupplementFileKind, 128 * 1024 * 1024, 166 * time.Second},
		{campaignEvidenceManifestKind, 128 * 1024 * 1024, 30 * time.Second},
		{campaignEvidenceFileKind, int64(^uint64(0) >> 1), evidenceLargeRequestMaximumTime},
	} {
		if got := evidenceRequestTimeout(sample.kind, sample.size); got != sample.want {
			t.Errorf("kind=%s bytes=%d budget=%s want=%s", sample.kind, sample.size, got, sample.want)
		}
	}
}

// A large immutable GET can outlive the ordinary client timeout without
// mutating that shared client. A shorter parent deadline always remains exact.
func TestEvidenceRequestBudgetKeepsClientAndParent(t *testing.T) {
	for _, parentBounded := range []bool{false, true} {
		ctx := t.Context()
		cancel := func() {}
		if parentBounded {
			ctx, cancel = context.WithTimeout(ctx, time.Second)
		}
		client := &http.Client{Timeout: 30 * time.Second}
		calls := 0
		client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			deadline, ok := request.Context().Deadline()
			if !ok {
				t.Error("request has no finite deadline")
			} else if parentBounded {
				parentDeadline, _ := ctx.Deadline()
				if deadline.After(parentDeadline) {
					t.Error("request enlarged its parent lifetime")
				}
			} else if time.Until(deadline) <= time.Minute || time.Until(deadline) > 167*time.Second {
				t.Error("large request inherited the ordinary thirty-second timeout", deadline)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("synthetic signed bytes")), Header: http.Header{}}, nil
		})
		probe := &liveScenarioProbe{client: client}
		raw, _, err := probe.getCampaignEvidenceWithWait(ctx, "https://evidence.example/sn/evidence?hash=synthetic", campaignEvidenceFileKind, 128*1024*1024, defaultCampaignEvidenceLimits(), func(context.Context, time.Duration) error { t.Error("unexpected retry"); return nil })
		cancel()
		if err != nil || string(raw) != "synthetic signed bytes" || calls != 1 || client.Timeout != 30*time.Second {
			t.Fatal("typed request changed its shared client or lost its body", calls, err)
		}
	}
}

// Timeouts and temporary statuses retry the same request under one finite
// owner. Permanent status and mixed integrity/transport errors stop immediately.
func TestEvidenceRequestBudgetRetriesOnlyEphemeralReads(t *testing.T) {
	for _, sample := range []struct {
		name        string
		status      int
		failure     error
		wantCalls   int
		wantSuccess bool
	}{
		{name: "timeout", failure: context.DeadlineExceeded, wantCalls: 3, wantSuccess: true},
		{name: "overload", status: http.StatusServiceUnavailable, wantCalls: 3, wantSuccess: true},
		{name: "permanent", status: http.StatusForbidden, wantCalls: 1},
		{name: "mixed", failure: errors.Join(io.ErrUnexpectedEOF, errors.New("synthetic invalid identity")), wantCalls: 1},
	} {
		calls, waits := 0, 0
		probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.URL.RawQuery != "hash=synthetic" || request.Method != http.MethodGet {
				t.Error("retry changed the immutable evidence request")
			}
			if calls < 3 && sample.failure != nil {
				return nil, sample.failure
			}
			status := http.StatusOK
			if calls < 3 && sample.status != 0 {
				status = sample.status
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("original")), Header: http.Header{}}, nil
		})}}
		raw, _, err := probe.getCampaignEvidenceWithWait(t.Context(), "https://evidence.example/sn/evidence?hash=synthetic", campaignEvidenceFileKind, 128*1024*1024, defaultCampaignEvidenceLimits(), func(ctx context.Context, delay time.Duration) error { waits++; return ctx.Err() })
		if calls != sample.wantCalls || waits != calls-1 || (err == nil) != sample.wantSuccess || sample.wantSuccess && string(raw) != "original" || !sample.wantSuccess && raw != nil {
			t.Errorf("%s calls=%d waits=%d err=%v body=%q", sample.name, calls, waits, err, raw)
		}
	}
}

// Exhaustion, cancellation and a one-byte overage retain no partial body and
// never turn the bounded exact-file request into an unbounded retry loop.
func TestEvidenceRequestBudgetExhaustionCancellationAndCapacity(t *testing.T) {
	for _, mode := range []string{"exhaustion", "cancellation", "capacity"} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if mode == "capacity" {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("12345")), Header: http.Header{}}, nil
			}
			return nil, context.DeadlineExceeded
		})}}
		raw, _, err := probe.getCampaignEvidenceWithWait(ctx, "https://evidence.example/sn/evidence", campaignEvidenceFileKind, 4, defaultCampaignEvidenceLimits(), func(ctx context.Context, _ time.Duration) error {
			if mode == "cancellation" {
				cancel()
			}
			return ctx.Err()
		})
		cancel()
		want := 1
		if mode == "exhaustion" {
			want = evidenceRequestMaximumAttempts
		}
		if err == nil || raw != nil || calls != want {
			t.Errorf("%s calls=%d body=%q err=%v", mode, calls, raw, err)
		}
	}
}
