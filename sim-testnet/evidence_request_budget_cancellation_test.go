// Response close and retry callbacks force cancellation at each request-owner
// boundary. Failed attempts retain their causes without admitting more bytes.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Cancellation at the completed close must preserve every read/close cause on
// successful and retryable statuses, with no partial body or second request.
func TestEvidenceRequestBudgetJoinsCanceledBodyFailures(t *testing.T) {
	t.Parallel()
	readErr := errors.New("synthetic evidence read failure")
	closeErr := errors.New("synthetic evidence close failure")
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		for _, sample := range []struct {
			name     string
			readErr  error
			closeErr error
		}{
			{name: "read", readErr: readErr},
			{name: "close", closeErr: closeErr},
			{name: "read-and-close", readErr: readErr, closeErr: closeErr},
		} {
			ctx, cancel := context.WithCancel(t.Context())
			body := &campaignReadbackBodyFailureV2{
				ReadCloser: io.NopCloser(strings.NewReader("synthetic evidence")),
				readErr:    sample.readErr,
				closeErr:   sample.closeErr,
				onClose:    cancel,
			}
			requests, waits := 0, 0
			probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: status, Header: make(http.Header), Request: request, Body: body}, nil
			})}}
			raw, code, err := probe.getCampaignEvidenceWithWait(ctx, "https://evidence.example/sn/evidence?hash=synthetic", campaignEvidenceFileKind, 32, defaultCampaignEvidenceLimits(), func(context.Context, time.Duration) error {
				waits++
				return errors.New("unexpected evidence retry")
			})
			cancel()
			if raw != nil || code != status || !errors.Is(err, context.Canceled) || sample.readErr != nil && !errors.Is(err, sample.readErr) || sample.closeErr != nil && !errors.Is(err, sample.closeErr) || requests != 1 || waits != 0 || body.readBytes == 0 || body.closes != 1 {
				t.Errorf("%s status=%d lost canceled response ownership: raw=%q code=%d err=%v requests=%d waits=%d body=%+v", sample.name, status, raw, code, err, requests, waits, body)
			}
		}
	}
}

// A transient close failure remains owned while a retry waits. The wait's
// error and cancellation cannot replace it, even if the delay reports success.
func TestEvidenceRequestBudgetJoinsInterruptedRetryCloseFailure(t *testing.T) {
	t.Parallel()
	closeErr := fmt.Errorf("synthetic transient evidence close failure: %w", io.ErrUnexpectedEOF)
	waitErr := errors.New("synthetic evidence retry wait failure")
	for _, sample := range []struct {
		name     string
		canceled bool
		waitErr  error
	}{
		{name: "during-wait", canceled: true, waitErr: context.Canceled},
		{name: "before-next-attempt", canceled: true},
		{name: "wait-failure", waitErr: waitErr},
		{name: "wait-failure-with-cancellation", canceled: true, waitErr: waitErr},
	} {
		ctx, cancel := context.WithCancel(t.Context())
		body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader("synthetic evidence")), closeErr: closeErr}
		requests, waits := 0, 0
		probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request, Body: body}, nil
		})}}
		raw, status, err := probe.getCampaignEvidenceWithWait(ctx, "https://evidence.example/sn/evidence?hash=synthetic", campaignEvidenceFileKind, 32, defaultCampaignEvidenceLimits(), func(context.Context, time.Duration) error {
			waits++
			if body.closes != 1 {
				t.Error("retry wait started before joining response close")
			}
			if sample.canceled {
				cancel()
			}
			return sample.waitErr
		})
		cancel()
		if raw != nil || status != http.StatusOK || !errors.Is(err, closeErr) || sample.waitErr != nil && !errors.Is(err, sample.waitErr) || sample.canceled && !errors.Is(err, context.Canceled) || requests != 1 || waits != 1 || body.readBytes == 0 || body.closes != 1 {
			t.Errorf("%s lost interrupted retry ownership: raw=%q status=%d err=%v requests=%d waits=%d body=%+v", sample.name, raw, status, err, requests, waits, body)
		}
	}
}

// A temporary status or body failure cannot make a joined permanent read or
// close error retryable; malformed evidence never reaches another attempt.
func TestEvidenceRequestBudgetRejectsMixedBodyFailures(t *testing.T) {
	t.Parallel()
	permanentErr := errors.New("synthetic invalid evidence response")
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		for _, sample := range []struct {
			name     string
			readErr  error
			closeErr error
		}{
			{name: "permanent-read", readErr: permanentErr, closeErr: io.ErrUnexpectedEOF},
			{name: "permanent-close", readErr: io.ErrUnexpectedEOF, closeErr: permanentErr},
		} {
			body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader("synthetic evidence")), readErr: sample.readErr, closeErr: sample.closeErr}
			requests, waits := 0, 0
			probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: status, Header: make(http.Header), Request: request, Body: body}, nil
			})}}
			raw, code, err := probe.getCampaignEvidenceWithWait(t.Context(), "https://evidence.example/sn/evidence?hash=synthetic", campaignEvidenceFileKind, 32, defaultCampaignEvidenceLimits(), func(context.Context, time.Duration) error {
				waits++
				return errors.New("unexpected evidence retry")
			})
			if raw != nil || code != status || !errors.Is(err, sample.readErr) || !errors.Is(err, sample.closeErr) || requests != 1 || waits != 0 || body.readBytes == 0 || body.closes != 1 {
				t.Errorf("%s status=%d retried mixed response failure: raw=%q code=%d err=%v requests=%d waits=%d body=%+v", sample.name, status, raw, code, err, requests, waits, body)
			}
		}
	}
}
