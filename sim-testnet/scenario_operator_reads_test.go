package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type scenarioOperatorTestTransport func(*http.Request) (*http.Response, error)

func (f scenarioOperatorTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// A barrier requires the complete four-read budget to enter before any read
// may finish. Serial requests would deadlock; work is measured without timing
// thresholds, apart from the test's deadlock guard.
func TestScenarioOperatorReadsBoundParallelWorkAndKeepResponseOwners(t *testing.T) {
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	defer close(release)
	var active, maximum, calls atomic.Int32
	probe := &liveScenarioProbe{client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); count > previous && !maximum.CompareAndSwap(previous, count); previous = maximum.Load() {
		}
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		code := http.StatusOK
		if request.URL.Host == "operator-2" && request.URL.Path == "/verify/stats" {
			code = http.StatusNotFound
		}
		identity := *request.URL
		query := identity.Query()
		query.Del("from")
		query.Del("to")
		identity.RawQuery = query.Encode()
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(identity.Host + identity.RequestURI())), Header: http.Header{}}, nil
	})}}
	bases := []string{"http://operator-1", "http://operator-2", "http://operator-3"}
	done := make(chan []scenarioOperatorSurfaces, 1)
	go func() { done <- probe.readOperatorSurfaces(t.Context(), bases) }()
	for index := 0; index < scenarioOperatorReadConcurrency; index++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("operator observations serialized independent requests")
		}
	}
	// Release all jobs without closing twice on an early failure.
	for index := 0; index < len(bases)*scenarioOperatorSurfaceCount; index++ {
		release <- struct{}{}
	}
	var result []scenarioOperatorSurfaces
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("operator reads did not finish")
	}
	if maximum.Load() != scenarioOperatorReadConcurrency || calls.Load() != int32(len(bases)*scenarioOperatorSurfaceCount) || active.Load() != 0 {
		t.Fatalf("unbounded or incomplete work: maximum=%d calls=%d active=%d", maximum.Load(), calls.Load(), active.Load())
	}
	paths := []string{"/status", "/verify/keys", "/verify/stats?limit=100000", "/verify/proofs?limit=10000"}
	for operator, surfaces := range result {
		for surface, response := range surfaces {
			want := fmt.Sprintf("operator-%d%s", operator+1, paths[surface])
			if string(response.data) != want {
				t.Fatalf("response changed owner: got %q want %q", response.data, want)
			}
			failed := operator == 1 && surface == scenarioOperatorStats
			if (response.err != nil) != failed || failed && response.status != http.StatusNotFound {
				t.Fatalf("response lost its original failure: %+v", response)
			}
		}
	}
}

func TestScenarioOperatorReadsCancelAllSurfacesWithoutRetainedSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{}, scenarioOperatorReadConcurrency)
	probe := &liveScenarioProbe{client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}}
	done := make(chan []scenarioOperatorSurfaces, 1)
	go func() { done <- probe.readOperatorSurfaces(ctx, []string{"http://operator-1", "http://operator-2"}) }()
	for index := 0; index < scenarioOperatorReadConcurrency; index++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("operator reads did not enter their concurrent budget")
		}
	}
	cancel()
	select {
	case result := <-done:
		for _, surfaces := range result {
			for _, response := range surfaces {
				if !errors.Is(response.err, context.Canceled) || response.status != 0 || response.data != nil {
					t.Fatalf("canceled observation became a success: %+v", response)
				}
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation left an operator worker running")
	}
}

func TestScenarioOperatorReadsObserveFreshResponsesAcrossSnapshots(t *testing.T) {
	var calls atomic.Int32
	probe := &liveScenarioProbe{client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(request.URL.String())), Header: http.Header{}}, nil
	})}}
	for snapshot := 1; snapshot <= 2; snapshot++ {
		responses := probe.readOperatorSurfaces(t.Context(), []string{"http://operator-1"})
		if calls.Load() != int32(snapshot*scenarioOperatorSurfaceCount) || len(responses) != 1 {
			t.Fatal("a successive snapshot reused a live endpoint response")
		}
	}
}

func TestScenarioOperatorReadsKeepTimeoutsVisibleInSuccessiveObservations(t *testing.T) {
	synctest.Test(t, testScenarioOperatorReadsKeepTimeoutsVisible)
}

// Virtual time exhausts the complete read budget without scheduler timing.
func testScenarioOperatorReadsKeepTimeoutsVisible(t *testing.T) {
	cfg := testResolvedConfig(t)
	var fail atomic.Bool
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	probe := &liveScenarioProbe{cfg: cfg, stateDir: t.TempDir(), client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
		body := "{}"
		switch request.URL.Path {
		case "/verify/keys":
			body = fmt.Sprintf(`{"keys":[{"server_key_id":1,"public_key":%q}]}`, key)
		case "/verify/stats":
			if fail.Load() {
				return nil, context.DeadlineExceeded
			}
			body = `{"schema":"urnetwork-verify-stats-index-v1","rows":[{"assignments":3,"confirmations":2}]}`
		case "/verify/proofs":
			if fail.Load() {
				return nil, context.DeadlineExceeded
			}
			body = `{"schema":"urnetwork-verify-proof-index-v1","rows":[{"server_key_id":1}]}`
		case "/sn/artifacts":
			body = `{"schema":"urnetwork-payout-artifact-history-v1","objects":[]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}}
	for snapshot := 0; snapshot < 2; snapshot++ {
		fail.Store(snapshot == 1)
		operators := probe.inspectOperators(t.Context(), nil, nil, nil)
		if len(operators) != cfg.Config.Topology.Operators {
			t.Fatal("snapshot lost an operator")
		}
		for index, operator := range operators {
			if operator.NoID != index+1 || !operator.Healthy || len(operator.VerifyKeys) != 1 {
				t.Fatalf("response identity or healthy independent surfaces changed: %+v", operator)
			}
			if snapshot == 0 {
				if operator.Error != "" || operator.StatsRows != 1 || operator.ProofRows != 1 || operator.Assignments != 3 || operator.Confirmations != 2 {
					t.Fatalf("successful surface was not projected: %+v", operator)
				}
			} else if !strings.Contains(operator.Error, "stats:") || !strings.Contains(operator.Error, "proofs:") || !strings.Contains(operator.Error, context.DeadlineExceeded.Error()) || operator.StatsRows != 0 || operator.ProofRows != 0 || operator.Assignments != 0 || operator.Confirmations != 0 {
				t.Fatalf("timeouts borrowed stale counters or lost their diagnostic: %+v", operator)
			}
		}
	}
}
