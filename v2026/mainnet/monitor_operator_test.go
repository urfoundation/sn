package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/server/v2026/stmonitor"
	"github.com/urnetwork/server/v2026/stmonitor/testfixture"
)

func monitorOperatorTestPolicy() monitorOperatorPolicy {
	return monitorOperatorPolicy{Role: "operator-a", DatabaseFile: "/synthetic/database.url", DatabaseSha256: "sha256:" + strings.Repeat("1", 64), ExpectedSource: stmonitor.Source{Database: "synthetic", User: "observer", DeploymentId: "synthetic-deployment", ChainId: 964, GenesisHash: testGenesisHash, Coordinator: "0x" + strings.Repeat("2", 40), OperatorId: 1, Accounts: []string{"0x" + strings.Repeat("3", 40)}}, PendingWarningSeconds: 90, PendingCriticalSeconds: 120}
}
func monitorOperatorTestSnapshot(now time.Time, policy monitorOperatorPolicy) *stmonitor.Snapshot {
	return &stmonitor.Snapshot{Source: policy.ExpectedSource, DatabaseAt: now, Mirror: &stmonitor.Mirror{NextBlock: 101, BlockHash: "0x" + strings.Repeat("4", 64), UpdatedAt: now}, CensusHash: "sha256:" + strings.Repeat("5", 64)}
}
func monitorOperatorTestPending(now time.Time) *stmonitor.Pending {
	return &stmonitor.Pending{Id: "00000000-0000-0000-0000-000000000001", CreatedAt: now.Add(-time.Hour), UpdatedAt: now, Status: "uncertain", Account: "0x" + strings.Repeat("3", 40), Nonce: 7, TransactionHash: "0x" + strings.Repeat("6", 64)}
}

func TestMonitorOperatorFreshReadCannotRefreshLedgerProgress(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := monitorOperatorTestPolicy()
	state := &monitorOperatorState{}
	first := monitorOperatorTestSnapshot(now, policy)
	first.PendingIntents = 1
	first.SignedAttempts = 1
	first.OldestIntent = monitorOperatorTestPending(now)
	first.OldestIntent.Status = "signed"
	if err := state.observe(now, policy, first, "observed", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	original := state.Transactions.Latest.Id
	progress := state.ScanProgressAt
	later := monitorOperatorTestSnapshot(now.Add(6*time.Minute), policy)
	later.PendingIntents = 1
	later.SignedAttempts = 1
	pending := *first.OldestIntent
	pending.UpdatedAt = later.DatabaseAt
	later.OldestIntent = &pending
	if err := state.observe(later.DatabaseAt, policy, later, "observed", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	conditions := state.conditions(later.DatabaseAt, policy, 5*time.Minute)
	if state.Transactions.Latest.Id != original || conditions[2].Code != "aged" || conditions[2].Severity != "critical" || conditions[1].Code != "stalled" || !state.ScanProgressAt.Equal(progress) {
		t.Fatal("fresh database query reset ledger age or incident", state, conditions)
	}
	raw, err := renderMonitorOperatorMetrics(policy, state, "published", true, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"independent_rpc", "canonical_receipts_verified", "mirror_genesis_verified", "operator_authority_verified", "provider_readiness_known", "liabilities_known", "protocol_deadlines_known", "repair_authorized"} {
		if !bytes.Contains(raw, []byte("sn_mainnet_operator_"+name+"{role=\"operator-a\"} 0\n")) {
			t.Fatal("unobserved capability silently became known", name)
		}
	}
}

func TestMonitorOperatorOutageRestartPreservesDomainIncidents(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := monitorOperatorTestPolicy()
	directory := monitorMetricsTestDir(t)
	storage := durablefixture.New(t, t.Context(), directory)
	checkpointPath, _ := monitorOperatorPaths(filepath.Join(directory, "monitor.json"), filepath.Join(directory, "monitor.prom"), policy.Role)
	provisionMonitorTestCustody(t, checkpointPath)
	open := func() *monitorOperatorWorker {
		worker, err := openMonitorOperatorWorker(storage.Context, policy, identityExpectation{NativeChain: "fixture", GenesisHash: testGenesisHash, EvmChainId: 964}, filepath.Join(directory, "monitor.json"), filepath.Join(directory, "monitor.prom"), monitorServiceHooks{})
		if err != nil {
			t.Fatal(err)
		}
		return worker
	}
	worker := open()
	state := worker.state
	value := monitorOperatorTestSnapshot(now, policy)
	value.PendingIntents = 1
	value.SignedAttempts = 1
	value.UncertainIntents = 1
	value.OldestIntent = monitorOperatorTestPending(now)
	if err := state.observe(now, policy, value, "observed", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	txId := state.Transactions.Latest.Id
	if err := state.observe(now.Add(time.Minute), policy, nil, "unavailable", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	readId := state.Read.Latest.Id
	if err := worker.save(); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(worker.metrics.close(), worker.checkpoint.owner.close()); err != nil {
		t.Fatal(err)
	}
	worker = open()
	defer worker.metrics.close()
	defer worker.checkpoint.owner.close()
	state = worker.state
	if state.current || state.Read.Latest.Id != readId || state.Transactions.Latest.Id != txId {
		t.Fatal("restart invented a successful read or erased incidents", state)
	}
	if err := state.observe(now.Add(2*time.Minute), policy, nil, "unavailable", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if state.Read.Latest.Id != readId || state.Read.Latest.Observations != 2 || !state.Transactions.Latest.RecoveredAt.IsZero() {
		t.Fatal("outage restart reset first failure or resolved unknown work", state)
	}
	recovered := *value
	recovered.DatabaseAt = now.Add(3 * time.Minute)
	if err := state.observe(recovered.DatabaseAt, policy, &recovered, "observed", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if state.Read.Latest.RecoveredAt.IsZero() || !state.Transactions.Latest.RecoveredAt.IsZero() {
		t.Fatal("read recovery incorrectly recovered uncertain transaction", state)
	}
	empty := monitorOperatorTestSnapshot(now.Add(4*time.Minute), policy)
	if err := state.observe(empty.DatabaseAt, policy, empty, "observed", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if state.Transactions.Latest.RecoveredAt.IsZero() || state.Transactions.Latest.RecoveryHash == "" {
		t.Fatal("completed ledger census lost bounded recovery evidence", state)
	}
	recovered.DatabaseAt = now.Add(5 * time.Minute)
	recoveredMirror := *recovered.Mirror
	recoveredMirror.UpdatedAt = recovered.DatabaseAt
	recovered.Mirror = &recoveredMirror
	if err := state.observe(recovered.DatabaseAt, policy, &recovered, "observed", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if state.Transactions.Count != 2 || state.Transactions.First.Id != txId || state.Transactions.Latest.Id == txId {
		t.Fatal("recurrence lost original incident", state)
	}
	if err := worker.save(); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorOperatorRefusesChangedIdentityClocksAndOriginalAge(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := monitorOperatorTestPolicy()
	for _, change := range []struct {
		code   string
		mutate func(*stmonitor.Snapshot)
	}{
		{code: "identity", mutate: func(value *stmonitor.Snapshot) { value.Source.OperatorId = 2 }},
		{code: "clock", mutate: func(value *stmonitor.Snapshot) { value.DatabaseAt = now.Add(time.Hour) }},
		{code: "changed", mutate: func(value *stmonitor.Snapshot) { value.OldestIntent.CreatedAt = now }},
		{code: "changed", mutate: func(value *stmonitor.Snapshot) { value.Mirror.BlockHash = "0x" + strings.Repeat("9", 64) }},
	} {
		state := &monitorOperatorState{}
		original := monitorOperatorTestSnapshot(now, policy)
		original.PendingIntents = 1
		original.SignedAttempts = 1
		original.OldestIntent = monitorOperatorTestPending(now)
		if err := state.observe(now, policy, original, "observed", time.Hour); err != nil {
			t.Fatal(err)
		}
		candidate := monitorOperatorTestSnapshot(now, policy)
		candidate.PendingIntents = 1
		candidate.SignedAttempts = 1
		candidate.OldestIntent = monitorOperatorTestPending(now)
		change.mutate(candidate)
		if err := state.observe(now, policy, candidate, "observed", time.Hour); err != nil {
			t.Fatal(err)
		}
		if state.current || state.Record != original || state.ReadStatus != change.code {
			t.Fatal("changed operator evidence was accepted", change.code, state)
		}
	}
}

func TestMonitorOperatorCheckpointAndPolicySourceFence(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	storage := durablefixture.New(t, t.Context(), fixture.directory)
	policy := monitorOperatorTestPolicy()
	checkpointPath, _ := monitorOperatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	provisionMonitorTestCustody(t, checkpointPath)
	policy.DatabaseFile = filepath.Join(fixture.directory, "operator.url")
	fixture.policy.Operators = []monitorOperatorPolicy{policy}
	fixture.writePolicy(t)
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}
	if _, err := loadMonitorServices(t.Context(), fixture.policyPath, expected, fixture.checkpointPath, fixture.metricsPath); err != nil {
		t.Fatal("independent operator policy refused", err)
	}
	for _, mutate := range []func(*monitorOperatorPolicy){
		func(value *monitorOperatorPolicy) { value.Role = "alpha" },
		func(value *monitorOperatorPolicy) { value.DatabaseFile = fixture.metricsPath },
		func(value *monitorOperatorPolicy) { value.ExpectedSource.GenesisHash = "0x" + strings.Repeat("9", 64) },
		func(value *monitorOperatorPolicy) { value.DatabaseSha256 = "" },
	} {
		candidate := policy
		mutate(&candidate)
		fixture.policy.Operators = []monitorOperatorPolicy{candidate}
		fixture.writePolicy(t)
		if _, err := loadMonitorServices(t.Context(), fixture.policyPath, expected, fixture.checkpointPath, fixture.metricsPath); err == nil {
			t.Fatal("operator policy admitted changed source or shared output")
		}
	}
	worker, err := openMonitorOperatorWorker(storage.Context, policy, expected, fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	now := fixture.clock.now()
	if err := worker.state.observe(now, policy, nil, "unavailable", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := worker.save(); err != nil {
		t.Fatal(err)
	}
	checkpoint := worker.checkpoint.owner.path
	worker.metrics.close()
	worker.checkpoint.owner.close()
	raw, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var record monitorOperatorCheckpointRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	record.State.Read.Latest.Observations++
	raw, _ = json.Marshal(record)
	if err := os.WriteFile(checkpoint, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := openMonitorOperatorWorker(storage.Context, policy, expected, fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{}); err == nil {
		unexpected.metrics.close()
		unexpected.checkpoint.owner.close()
		t.Fatal("changed retained incident passed checkpoint fence")
	}
}

type monitorOperatorTestEvent struct {
	Schema      string                `json:"schema"`
	Role        string                `json:"role"`
	Publication string                `json:"publication"`
	State       *monitorOperatorState `json:"state"`
}
type monitorOperatorTestSink struct {
	events     chan monitorOperatorTestEvent
	validators chan string
}

func (self *monitorOperatorTestSink) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}
func (self *monitorOperatorTestSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var head struct {
		Schema string `json:"schema"`
		Role   string `json:"role"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return 0, err
	}
	if head.Schema == "urnetwork-mainnet-operator-event-v1" {
		var event monitorOperatorTestEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return 0, err
		}
		select {
		case self.events <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	if head.Schema == "urnetwork-mainnet-validator-event-v1" {
		select {
		case self.validators <- head.Role:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}

func startMonitorOperatorTest(t *testing.T, fixture *monitorServicesFixture, url string, hooks monitorServiceHooks) (context.CancelFunc, <-chan int, *monitorOperatorTestSink, chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	args := fixture.args(url)
	// Fatal fixture enrollment belongs to the test goroutine, before its worker
	// can make an RPC-entry or output barrier reachable.
	ctx = monitorTestStorageContext(t, ctx, args)
	sink := &monitorOperatorTestSink{events: make(chan monitorOperatorTestEvent, 4), validators: make(chan string, 4)}
	resume := make(chan struct{})
	done := make(chan int, 1)
	hooks.wait = func(ctx context.Context, role string, _ time.Duration) bool {
		if role == "operator-a" {
			select {
			case <-resume:
				return true
			case <-ctx.Done():
				return false
			}
		}
		<-ctx.Done()
		return false
	}
	go func() {
		defer close(done)
		done <- runMainWithMonitorHooks(ctx, args, sink, io.Discard, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { joinMonitorTestWorker(t, cancel, done) })
	return cancel, done, sink, resume
}

func operatorDatabaseFixture(t *testing.T) (*monitorServicesFixture, *testfixture.Fixture) {
	t.Helper()
	database := testfixture.New(t)
	database.Source.GenesisHash = testGenesisHash
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.clock.seconds.Store(database.Now.Unix())
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 1))
	raw := []byte(database.Dsn + "\n")
	path := filepath.Join(fixture.directory, "operator.url")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	policy := monitorOperatorTestPolicy()
	policy.ExpectedSource = database.Source
	policy.DatabaseFile = path
	policy.DatabaseSha256 = monitorReadDigest(raw)
	fixture.policy.Operators = []monitorOperatorPolicy{policy}
	fixture.writePolicy(t)
	return fixture, database
}

func TestMonitorOperatorCommandReadsProductionJournalAndRestartsOutage(t *testing.T) {
	fixture, database := operatorDatabaseFixture(t)
	database.Intent(t, 1, "uncertain", 1, database.Now.Add(-time.Hour))
	url, entered, left := monitorServicesBlockedChain(t)
	cancel, done, sink, resume := startMonitorOperatorTest(t, fixture, url, monitorServiceHooks{})
	<-entered
	event := <-sink.events
	if event.State == nil || event.State.ReadStatus != "observed" || event.State.Record.UncertainIntents != 1 || event.State.Transactions.Latest == nil {
		cancel()
		<-done
		t.Fatal("public command did not observe actual operator journal", event)
	}
	if role := <-sink.validators; role != "alpha" {
		t.Fatal("operator read suppressed validator peer", role)
	}
	txId := event.State.Transactions.Latest.Id
	// A missing protected credential file is an observation outage, not empty DB state.
	if err := os.Remove(fixture.policy.Operators[0].DatabaseFile); err != nil {
		t.Fatal(err)
	}
	fixture.clock.seconds.Add(1)
	resume <- struct{}{}
	event = <-sink.events
	if event.State.ReadStatus != "unavailable" || event.State.Read.Latest == nil || event.State.Transactions.Latest.Id != txId || !event.State.Transactions.Latest.RecoveredAt.IsZero() {
		t.Fatal("source outage erased pending incident", event)
	}
	readId := event.State.Read.Latest.Id
	cancel()
	if exit := <-done; exit != 0 {
		t.Fatal("command cancellation failed", exit)
	}
	<-left
	cancel, done, sink, _ = startMonitorOperatorTest(t, fixture, url, monitorServiceHooks{})
	event = <-sink.events
	if event.State.Read.Latest.Id != readId || event.State.Read.Latest.Observations != 2 || event.State.Transactions.Latest.Id != txId {
		t.Fatal("public command restart erased incident continuity", event)
	}
	cancel()
	if exit := <-done; exit != 0 {
		t.Fatal("restarted command did not join", exit)
	}
}

func TestMonitorOperatorCredentialFenceAndPublicationAmbiguity(t *testing.T) {
	fixture, database := operatorDatabaseFixture(t)
	database.Intent(t, 1, "prepared", 0, database.Now)
	policy := fixture.policy.Operators[0]
	if err := os.WriteFile(policy.DatabaseFile, []byte(database.Dsn+" \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if value, code := readMonitorOperator(t.Context(), policy, monitorServiceReadHooks{}); value != nil || code != "identity" {
		t.Fatal("swapped credential source passed independent digest", value, code)
	}
	if err := os.WriteFile(policy.DatabaseFile, []byte(database.Dsn+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	var once sync.Once
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == policy.Role && kind == "metrics" {
			once.Do(func() { err = errors.Join(err, errors.New("synthetic post-rename sync failure")) })
		}
		return err
	}}
	cancel, done, sink, resume := startMonitorOperatorTest(t, fixture, url, hooks)
	first := <-sink.events
	if first.Publication != "retrying" || !first.State.PublicationLastSuccessAt.IsZero() {
		t.Fatal("ambiguous metrics write claimed publication", first)
	}
	fixture.clock.seconds.Add(1)
	resume <- struct{}{}
	second := <-sink.events
	if second.Publication != "published" || second.State.PublicationLastSuccessAt.IsZero() {
		t.Fatal("owner failed to retry complete publication", second)
	}
	cancel()
	if exit := <-done; exit != 0 {
		t.Fatal("publication command did not join", exit)
	}
}

// The role census has separate explicit bounds; unrelated malformed-role errors
// cannot masquerade as coverage of an empty or over-capacity deployment policy.
func TestMonitorOperatorPolicyBoundsCoverCombinedAndOperatorOnlyCensus(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta")
	validators := append([]monitorValidatorPolicy(nil), fixture.policy.Validators...)
	var operators []monitorOperatorPolicy
	for index, role := range []string{"operator-a", "operator-b", "operator-c", "operator-d", "operator-e"} {
		policy := monitorOperatorTestPolicy()
		policy.Role = role
		policy.ExpectedSource.OperatorId = uint64(index + 1)
		policy.DatabaseFile = filepath.Join(fixture.directory, role+".url")
		operators = append(operators, policy)
	}
	for _, candidate := range []struct {
		validators int
		operators  int
		admitted   bool
	}{
		{validators: 0, operators: 1, admitted: true}, {validators: 0, operators: 4, admitted: true},
		{validators: 8, operators: 4, admitted: true}, {validators: 8, operators: 0, admitted: true},
		{validators: 0, operators: 0, admitted: false}, {validators: 0, operators: 5, admitted: false},
		{validators: 8, operators: 5, admitted: false}, {validators: 9, operators: 1, admitted: false},
	} {
		fixture.policy.Validators = append([]monitorValidatorPolicy(nil), validators[:min(candidate.validators, len(validators))]...)
		if candidate.validators > len(validators) {
			fixture.policy.Validators = append(fixture.policy.Validators, validators[0])
		}
		fixture.policy.Operators = append([]monitorOperatorPolicy(nil), operators[:candidate.operators]...)
		fixture.writePolicy(t)
		actual, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath)
		if candidate.admitted && (err != nil || actual == nil || len(actual.Validators) != candidate.validators || len(actual.Operators) != candidate.operators) {
			t.Fatal("bounded operator/validator census refused", candidate, err)
		}
		if !candidate.admitted && (actual != nil || !errors.Is(err, errMonitorServicesCensus)) {
			t.Fatal("unbounded or empty role census passed admission", candidate, err)
		}
	}
}
