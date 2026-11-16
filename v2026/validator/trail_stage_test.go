//go:build linux || darwin

// Trail stages and their summary are observation-only: they read closed facts
// through the standard library's own wrappers and this package's types, never
// another error's methods, and print only closed labels and counts.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
	connectprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Counts method calls without blocking; fmt.Errorf formats it once at build.
type trailStageCountingProbe struct{ calls atomic.Uint64 }

func (self *trailStageCountingProbe) Error() string {
	self.calls.Add(1)
	return "synthetic private detail"
}
func (self *trailStageCountingProbe) Unwrap() error {
	self.calls.Add(1)
	return context.DeadlineExceeded
}

// Kinds, stages, closed causes, contract evidence, status and depth survive
// the engine's own fmt wrappers and the transport's joins.
func TestTrailFailureFactsReadStagesThroughWrappers(t *testing.T) {
	insufficient := connectprotocol.ContractError_InsufficientBalance
	for _, candidate := range []struct {
		name     string
		err      error
		kind     string
		stage    trailStage
		cause    string
		contract trailContractOutcome
		code     connectprotocol.ContractError
		status   int
		depth    int
	}{
		{name: "pick", err: &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("seed pick: %w", errors.New("no seed providers available")), stage: trailStagePick}, kind: "seed", stage: trailStagePick, cause: "unknown"},
		{name: "reserve", err: &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("seed attempt pace: %w", errAttemptCutPending), stage: trailStageReserve}, kind: "seed", stage: trailStageReserve, cause: "unknown"},
		{name: "lease", err: &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("step timeout after 1 attempts: %w", tunnelVerifyFailure(trailStageLease, context.DeadlineExceeded)), stage: trailStageSend}, kind: "seed", stage: trailStageLease, cause: "timeout"},
		{name: "contract refused", err: &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("step timeout after 3 attempts: %w", &tunnelVerifyError{stage: trailStageContract, contract: trailContractRefused, contractCode: insufficient, cause: fmt.Errorf("verify post via x: %w", &url.Error{Op: "Post", URL: "https://api.example/verify", Err: context.DeadlineExceeded})}), stage: trailStageSend}, kind: "seed", stage: trailStageContract, cause: "timeout", contract: trailContractRefused, code: insufficient},
		{name: "contract unanswered", err: &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("step timeout after 3 attempts: %w", &tunnelVerifyError{stage: trailStageContract, contract: trailContractUnanswered, cause: context.DeadlineExceeded}), stage: trailStageSend}, kind: "seed", stage: trailStageContract, cause: "timeout", contract: trailContractUnanswered},
		{name: "handshake joined with close", err: &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("step timeout: %w", errors.Join(&tunnelVerifyError{stage: trailStageHandshake, contract: trailContractGranted, cause: context.DeadlineExceeded}, errors.New("close tunnel netstack"))), stage: trailStageSend}, kind: "seed", stage: trailStageHandshake, cause: "unknown", contract: trailContractGranted},
		{name: "status keeps inner code", err: &TrailError{Kind: TrailErrorHop, Err: fmt.Errorf("step failed: %w", &tunnelVerifyError{stage: trailStageStatus, contract: trailContractGranted, cause: tunnelVerifyStatusFailure(http.StatusTooManyRequests, errors.New("verify post via x: http 429"))}), stage: trailStageSend, depth: 3}, kind: "hop", stage: trailStageStatus, cause: "unknown", contract: trailContractGranted, status: http.StatusTooManyRequests, depth: 3},
		{name: "protocol", err: &TrailError{Kind: TrailErrorProtocol, Err: errors.New("assign switched trail identity"), depth: 2}, kind: "protocol", stage: trailStageUnknown, cause: "unknown", depth: 2},
		{name: "unknown final joined timeout", err: &TrailError{Kind: TrailErrorUnknownOutcome, Err: errors.Join(context.DeadlineExceeded)}, kind: "unknown_final", stage: trailStageUnknown, cause: "timeout"},
		{name: "not a trail error", err: context.Canceled, kind: "unknown", stage: trailStageUnknown, cause: "canceled"},
	} {
		facts := trailFailureFactsOf(candidate.err)
		if facts.kind != candidate.kind || facts.stage != candidate.stage || facts.cause != candidate.cause || facts.contract != candidate.contract ||
			facts.contractCode != candidate.code || facts.status != candidate.status || facts.depth != candidate.depth {
			t.Fatalf("%s: got %+v", candidate.name, facts)
		}
	}
}

// Marking a transport failure keeps its text and every errors.Is target.
func TestTunnelVerifyErrorKeepsTextAndIdentity(t *testing.T) {
	cause := fmt.Errorf("verify post via x: %w", context.DeadlineExceeded)
	marked := tunnelVerifyFailure(trailStageHandshake, cause)
	if marked.Error() != cause.Error() || !errors.Is(marked, context.DeadlineExceeded) {
		t.Fatal("transport mark changed the error's text or identity")
	}
	status := tunnelVerifyStatusFailure(http.StatusInternalServerError, cause)
	if status.Error() != cause.Error() || !errors.Is(status, context.DeadlineExceeded) {
		t.Fatal("status mark changed the error's text or identity")
	}
	if tunnelVerifyFailure(trailStageLease, nil) != nil || tunnelVerifyStatusFailure(500, nil) != nil {
		t.Fatal("a nil cause became an error")
	}
	trail := &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("seed pick: %w", errors.New("no seed providers available")), stage: trailStagePick}
	if trail.Error() != "trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available" {
		t.Fatal("trail error text changed; the sim-testnet log gate matches it")
	}
}

// Reading facts never calls a foreign error's methods, even inside the
// engine's fmt wrappers and joins.
func TestTrailFailureFactsNeverCallForeignMethods(t *testing.T) {
	probe := &trailStageCountingProbe{}
	errs := []error{
		&TrailError{Kind: TrailErrorHop, Err: fmt.Errorf("step: %w", probe), stage: trailStageSend},
		&TrailError{Kind: TrailErrorSeed, Err: errors.Join(probe, &tunnelVerifyError{stage: trailStagePost, cause: probe}), stage: trailStageSend},
	}
	before := probe.calls.Load()
	for _, err := range errs {
		facts := trailFailureFactsOf(err)
		if facts.cause != "unknown" {
			t.Fatal("a foreign wrapper lent a closed cause")
		}
	}
	if probe.calls.Load() != before {
		t.Fatal("trail facts called a foreign error method")
	}
}

// The observation's evidence picks the transport stage.
func TestTunnelVerifyObservationStages(t *testing.T) {
	hop := connect.NewId()
	refused := connectprotocol.ContractError_InsufficientBalance
	for _, candidate := range []struct {
		name     string
		set      func(*tunnelVerifyObservation)
		stage    trailStage
		contract trailContractOutcome
	}{
		{name: "nothing started", set: func(*tunnelVerifyObservation) {}, stage: trailStageRegistration, contract: trailContractUnanswered},
		{name: "unanswered", set: func(o *tunnelVerifyObservation) { o.started = true }, stage: trailStageContract, contract: trailContractUnanswered},
		{name: "refused", set: func(o *tunnelVerifyObservation) {
			o.started = true
			o.observeContract(&connect.ContractStatus{Key: connect.ContractKey{Destination: connect.DestinationId(hop)}, Error: &refused})
		}, stage: trailStageContract, contract: trailContractRefused},
		{name: "granted", set: func(o *tunnelVerifyObservation) {
			o.started = true
			o.observeContract(&connect.ContractStatus{Key: connect.ContractKey{Destination: connect.DestinationId(hop)}, Error: &refused})
			o.observeContract(&connect.ContractStatus{Key: connect.ContractKey{Destination: connect.DestinationId(hop)}})
		}, stage: trailStageHandshake, contract: trailContractGranted},
		{name: "other hop and companion ignored", set: func(o *tunnelVerifyObservation) {
			o.started = true
			o.observeContract(&connect.ContractStatus{Key: connect.ContractKey{Destination: connect.DestinationId(connect.NewId())}})
			o.observeContract(&connect.ContractStatus{Key: connect.ContractKey{Destination: connect.DestinationId(hop), CompanionContract: true}})
		}, stage: trailStageContract, contract: trailContractUnanswered},
		{name: "dialed", set: func(o *tunnelVerifyObservation) { o.started, o.dialed = true, true }, stage: trailStagePost, contract: trailContractUnanswered},
		{name: "responded", set: func(o *tunnelVerifyObservation) { o.started, o.dialed, o.wrote, o.responded = true, true, true, true }, stage: trailStageStatus, contract: trailContractUnanswered},
	} {
		observation := newTunnelVerifyObservation(nil, hop)
		candidate.set(observation)
		marked, ok := observation.failure(context.DeadlineExceeded).(*tunnelVerifyError)
		if !ok || marked.stage != candidate.stage || marked.contract != candidate.contract {
			t.Fatalf("%s: got %+v", candidate.name, marked)
		}
		if candidate.contract == trailContractRefused && marked.contractCode != refused {
			t.Fatalf("%s: refusal code lost", candidate.name)
		}
		observation.close()
	}
	if newTunnelVerifyObservation(nil, hop).failure(nil) != nil {
		t.Fatal("a successful exchange was marked failed")
	}
}

// The HTTP trace sees a real exchange's progress, and a refused dial stays
// before the connection.
func TestTunnelVerifyObservationTracesHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	observation := newTunnelVerifyObservation(nil, connect.NewId())
	request, err := http.NewRequestWithContext(observation.context(t.Context()), http.MethodPost, server.URL+"/verify", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if marked := observation.failure(errors.New("http 429")).(*tunnelVerifyError); marked.stage != trailStageStatus {
		t.Fatalf("answered exchange read %s", marked.stage)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	address := closed.URL
	closed.Close()
	observation = newTunnelVerifyObservation(nil, connect.NewId())
	request, err = http.NewRequestWithContext(observation.context(t.Context()), http.MethodPost, address+"/verify", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("closed server answered")
	} else if marked := observation.failure(err).(*tunnelVerifyError); marked.stage != trailStageContract {
		t.Fatalf("refused dial read %s", marked.stage)
	}
}

// The real engine records the pick, reservation and transport stages.
func TestTrailEngineRecordsSeedAndHopStages(t *testing.T) {
	server, key, clientId := newMockVerifyServer(t, 16)
	engine, _, _ := newTestEngine(t, server, key, clientId, 8, nil)
	engine.pickSeed = func(context.Context) (connect.Id, error) {
		return connect.Id{}, errors.New("no seed providers available")
	}
	_, err := engine.RunTrail(t.Context())
	if facts := trailFailureFactsOf(err); facts.kind != "seed" || facts.stage != trailStagePick {
		t.Fatalf("pick failure read %+v", facts)
	}

	engine, _, _ = newTestEngine(t, server, key, clientId, 8, nil)
	engine.cfg.ExtendAttempts = 1
	engine.transport = trailStageTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
		return nil, &tunnelVerifyError{stage: trailStageContract, contract: trailContractUnanswered, cause: context.DeadlineExceeded}
	})
	_, err = engine.RunTrail(t.Context())
	if facts := trailFailureFactsOf(err); facts.kind != "seed" || facts.stage != trailStageContract || facts.cause != "timeout" || facts.depth != 0 {
		t.Fatalf("seed transport failure read %+v", facts)
	}

	engine, _, _ = newTestEngine(t, server, key, clientId, 8, nil)
	engine.cfg.ExtendAttempts = 1
	calls := 0
	engine.transport = trailStageTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
		calls++
		if calls <= 2 {
			return server.PostVerify(ctx, hop, raw)
		}
		return nil, &tunnelVerifyError{stage: trailStageHandshake, contract: trailContractGranted, cause: context.DeadlineExceeded}
	})
	_, err = engine.RunTrail(t.Context())
	if facts := trailFailureFactsOf(err); facts.kind != "hop" || facts.stage != trailStageHandshake || facts.contract != trailContractGranted || facts.depth != 2 {
		t.Fatalf("hop transport failure read %+v", facts)
	}
}

// A pending attempt cut refuses the seed's reservation, read as reserve.
func TestTrailEngineRecordsReservationStage(t *testing.T) {
	server, key, clientId := newMockVerifyServer(t, 16)
	engine, stats, _ := newTestEngine(t, server, key, clientId, 8, nil)
	stateDir := t.TempDir()
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	t.Cleanup(func() { _ = ledger.Close() })
	if err := stats.beginAttempt(42, ledger); err != nil {
		t.Fatal(err)
	}
	if _, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { return nil }); !errors.Is(err, errAttemptCutPending) {
		t.Fatal("the cut did not become pending", err)
	}
	defer stats.abortAttempt()
	_, err := engine.RunTrail(t.Context())
	if facts := trailFailureFactsOf(err); facts.kind != "seed" || facts.stage != trailStageReserve || !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("pending cut read %+v (%v)", facts, err)
	}
}

type trailStageTransport func(context.Context, connect.Id, []byte) ([]byte, error)

func (self trailStageTransport) PostVerify(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
	return self(ctx, hop, raw)
}

// One line per window with closed labels and counts only, starting a new
// window afterwards.
func TestTrailDiagnosticSummaryWindowsAndContent(t *testing.T) {
	summary := &trailDiagnosticSummary{}
	start := time.Unix(1_800_000_000, 0)
	insufficient := connectprotocol.ContractError_InsufficientBalance
	failures := []trailFailureFacts{
		{kind: "seed", stage: trailStagePick, cause: "unknown"},
		{kind: "seed", stage: trailStageReserve, cause: "unknown"},
		{kind: "seed", stage: trailStageContract, cause: "timeout", contract: trailContractRefused, contractCode: insufficient},
		{kind: "seed", stage: trailStageContract, cause: "timeout", contract: trailContractUnanswered},
		{kind: "seed", stage: trailStageHandshake, cause: "timeout", contract: trailContractGranted},
		{kind: "hop", stage: trailStageStatus, cause: "unknown", contract: trailContractGranted, status: 429, depth: 3},
		{kind: "protocol", stage: trailStageUnknown, cause: "unknown", depth: 1},
	}
	for index := range failures {
		if line := summary.observe(start.Add(time.Duration(index)*time.Second), &failures[index]); line != nil {
			t.Fatal("summary written before its window elapsed")
		}
	}
	line := summary.observe(start.Add(trailDiagnosticSummaryWindow), nil)
	if line == nil {
		t.Fatal("summary not written after its window")
	}
	text := string(line)
	for _, want := range []string{
		"validator trail summary: window_s=60 complete=1 failed=7",
		"seed:pick=1,reserve=1,contract=2,handshake=1",
		"hop:status=1", "protocol:unknown=1",
		"causes:timeout=3,unknown=4",
		"contracts:unanswered=1,granted=2,insufficient_balance=1",
		"http:429=1", "reached:0=5,1=1,3=1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary lacks %q: %s", want, text)
		}
	}
	if !regexp.MustCompile(`^validator trail summary:[a-z0-9_=:, ]+\n$`).MatchString(text) {
		t.Fatalf("summary carries text outside its closed labels: %q", text)
	}
	if summary.observe(start.Add(trailDiagnosticSummaryWindow+time.Second), nil) != nil {
		t.Fatal("the next window did not restart")
	}
}

// A window with every kind, stage, contract and status stays within 1 KiB.
func TestTrailDiagnosticSummaryIsBounded(t *testing.T) {
	summary := &trailDiagnosticSummary{}
	start := time.Unix(1_800_000_000, 0)
	for _, kind := range trailDiagnosticSummaryKinds {
		for stage := trailStage(0); stage < trailStageCount; stage++ {
			for code := 0; code < 8; code++ {
				facts := trailFailureFacts{kind: kind, stage: stage, cause: "timeout", contract: trailContractRefused, contractCode: connectprotocol.ContractError(code), status: 400 + code, depth: code}
				for range 3 {
					summary.observe(start, &facts)
				}
			}
		}
	}
	line := summary.observe(start.Add(trailDiagnosticSummaryWindow), nil)
	if len(line) > trailDiagnosticSummaryMaximumBytes || !strings.HasSuffix(string(line), "\n") || !strings.Contains(string(line), "truncated=true") {
		t.Fatalf("summary exceeded its bound: %d bytes", len(line))
	}
}

// Through the real owner, the summary follows the scalar records on the
// runtime domain once its window has elapsed.
func TestTrailDiagnosticSummaryReachesRuntimeDomain(t *testing.T) {
	var mutex sync.Mutex
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time {
		mutex.Lock()
		defer mutex.Unlock()
		return now
	}
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 4)}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), output, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	failure := &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("step timeout after 3 attempts: %w", &tunnelVerifyError{stage: trailStageHandshake, contract: trailContractGranted, cause: context.DeadlineExceeded}), stage: trailStageSend}
	observeTrailDiagnostic(ctx, trailDiagnosticFailed, failure, nil, false)
	<-output.written
	mutex.Lock()
	now = now.Add(trailDiagnosticSummaryWindow)
	mutex.Unlock()
	observeTrailDiagnostic(ctx, trailDiagnosticFailed, failure, nil, false)
	<-output.written
	<-output.written
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "trail_kind=seed cause=timeout") ||
		!strings.Contains(lines[2], "validator trail summary: window_s=60 complete=0 failed=2 seed:handshake=2 causes:timeout=2 contracts:granted=2 reached:0=2") {
		t.Fatalf("runtime domain lines: %q", lines)
	}
}
