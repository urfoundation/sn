package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

// Add a per-element failure to the existing bounded-transport fixture. Both
// observers still receive real JSON-RPC requests over local HTTP connections.
type fleetHistoryCacheRPC struct {
	*fleetHistoryBatchRPC
	failureBlock uint64
}

func (self *fleetHistoryCacheRPC) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if self.failureBlock == 0 {
		self.fleetHistoryBatchRPC.ServeHTTP(writer, request)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		self.t.Errorf("read fault-injection request: %v", err)
		return
	}
	request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	self.fleetHistoryBatchRPC.ServeHTTP(recorder, request)
	var calls []fleetHistoryBatchRPCRequest
	var responses []map[string]any
	if json.Unmarshal(body, &calls) == nil && len(calls) > 0 && calls[0].Method == "eth_call" && json.Unmarshal(recorder.Body.Bytes(), &responses) == nil {
		for index, call := range calls {
			if string(call.Params[1]) == fmt.Sprintf("%q", hexutil.EncodeUint64(self.failureBlock)) {
				delete(responses[index], "result")
				responses[index]["error"] = map[string]any{"code": -32000, "message": "fixture historical call failed"}
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(responses); err != nil {
			self.t.Errorf("encode fault-injection response: %v", err)
		}
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	if _, err := writer.Write(recorder.Body.Bytes()); err != nil {
		self.t.Errorf("write fixture response: %v", err)
	}
}

type historicalFleetCacheFixture struct {
	executor                 *Executor
	calls                    []historicalFleetGenerationOneCall
	operational, independent *fleetHistoryCacheRPC
}

func newHistoricalFleetCacheFixture(t *testing.T, count int, independent bool) historicalFleetCacheFixture {
	t.Helper()
	fixture := historicalFleetCacheFixture{
		operational: &fleetHistoryCacheRPC{fleetHistoryBatchRPC: &fleetHistoryBatchRPC{t: t, finalizedBlock: 1_000}},
		independent: &fleetHistoryCacheRPC{fleetHistoryBatchRPC: &fleetHistoryBatchRPC{t: t, finalizedBlock: 1_000}},
	}
	dial := func(handler http.Handler) (*ethclient.Client, string) {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := rpc.DialHTTP(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		return ethclient.NewClient(client), server.URL
	}
	operationalClient, operationalURL := dial(fixture.operational)
	independentClient, independentURL := dial(fixture.independent)
	cfg := testResolvedConfig(t)
	cfg.OperationalEVM = operationalURL
	cfg.Public.Chain.EVMPublicReadEndpoint = independentURL
	if !independent {
		cfg.OperationalRPCMode = rpcModePublicOverride
		cfg.Public.Chain.EVMPublicReadEndpoint = operationalURL
	}
	planHash := "0x" + strings.Repeat("88", 32)
	fixture.executor = &Executor{
		cfg: cfg, stateDir: t.TempDir(), plan: &SetupPlan{PlanHash: planHash},
		deployer: &EvmTxManager{client: operationalClient}, independentEVM: independentClient,
	}
	if err := os.Chmod(fixture.executor.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.calls = make([]historicalFleetGenerationOneCall, count)
	for index := range fixture.calls {
		// Two actions share each checkpoint; sixty distinct heights in the
		// 120-action case require two header batches and three state batches.
		block := uint64(100 + index/2)
		comparisonBlock := block
		if independent {
			comparisonBlock += 200
		}
		observed := map[string]any{"index": index}
		fixture.calls[index] = historicalFleetGenerationOneCall{
			action: Action{ID: fmt.Sprintf("fleet.bind.%d.1", index+1)},
			entry: JournalEntry{
				PlanHash: planHash, ActionID: fmt.Sprintf("fleet.bind.%d.1", index+1),
				IntentHash: fmt.Sprintf("0x%064x", index+1), PostconditionHash: fmt.Sprintf("0x%064x", index+10_000),
			},
			record: &ActionPostcondition{
				EVMFinalized: ChainHead{Number: block, Hash: fleetHistoryBatchBlockHash(block)}, Observed: observed,
				IndependentEVMFinalized: ChainHead{Number: comparisonBlock, Hash: fleetHistoryBatchBlockHash(comparisonBlock)},
				IndependentObserved:     observed,
			},
			address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			data:    []byte{byte(index)}, expectation: map[string]any{"result": 1, "index": index},
			observe: func(output []byte) (map[string]any, error) {
				if !bytes.Equal(output, []byte{1}) {
					return nil, fmt.Errorf("unexpected historical output %x", output)
				}
				return observed, nil
			},
		}
	}
	return fixture
}

// Construct a new executor without carrying either process-local success
// map. Every warm proof must therefore come from the authenticated disk cache.
func restartHistoricalFleetCacheExecutor(executor *Executor) *Executor {
	return &Executor{
		cfg: executor.cfg, stateDir: executor.stateDir, plan: executor.plan, journal: executor.journal,
		roles: executor.roles, payloads: executor.payloads, deployer: executor.deployer,
		oracle: executor.oracle, keeper: executor.keeper, independentEVM: executor.independentEVM,
		auditAuthorizedConfig: executor.auditAuthorizedConfig,
	}
}

func TestHistoricalFleetCacheReusesAcrossExecutorsWithBatchedFreshCheckpoints(t *testing.T) {
	for _, independent := range []bool{false, true} {
		t.Run(fmt.Sprintf("independent=%t", independent), func(t *testing.T) {
			fixture := newHistoricalFleetCacheFixture(t, 120, independent)
			keys, err := fixture.executor.verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls)
			if err != nil || len(keys) != len(fixture.calls) {
				t.Fatalf("cold verification keys=%d err=%v", len(keys), err)
			}
			observers := []*fleetHistoryCacheRPC{fixture.operational}
			if independent {
				observers = append(observers, fixture.independent)
			}
			for _, observer := range observers {
				if observer.httpRequests != 6 || observer.blockBatchRequests != 2 || observer.contractBatchRequests != 3 || len(observer.blockSelectors) != 60 || len(observer.contractSelectors) != 120 {
					t.Fatalf("cold RPC count HTTP/header/contract=%d/%d/%d selectors=%d/%d", observer.httpRequests, observer.blockBatchRequests, observer.contractBatchRequests, len(observer.blockSelectors), len(observer.contractSelectors))
				}
				observer.finalizedBlock++
				observer.contractResult = "0x02"
			}
			keys, err = restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls)
			if err != nil || len(keys) != len(fixture.calls) {
				t.Fatalf("warm verification keys=%d err=%v", len(keys), err)
			}
			for _, observer := range observers {
				if observer.httpRequests != 9 || observer.blockBatchRequests != 4 || observer.contractBatchRequests != 3 || len(observer.blockSelectors) != 120 || len(observer.contractSelectors) != 120 {
					t.Fatalf("warm RPC count HTTP/header/contract=%d/%d/%d selectors=%d/%d", observer.httpRequests, observer.blockBatchRequests, observer.contractBatchRequests, len(observer.blockSelectors), len(observer.contractSelectors))
				}
			}
			if !independent && fixture.independent.httpRequests != 0 {
				t.Fatalf("shared provider used independent route %d times", fixture.independent.httpRequests)
			}
		})
	}
}

func TestHistoricalFleetCacheWarmCheckpointFailuresRemainFatal(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(historicalFleetCacheFixture)
		want   string
	}{
		{name: "operational canonical hash", mutate: func(f historicalFleetCacheFixture) { f.operational.tamperedBlock = 100 }, want: "canonical hash"},
		{name: "independent canonical hash", mutate: func(f historicalFleetCacheFixture) { f.independent.tamperedBlock = 300 }, want: "canonical hash"},
		{name: "operational finalized head", mutate: func(f historicalFleetCacheFixture) { f.operational.finalizedBlock = 99 }, want: "not finalized"},
		{name: "independent finalized head", mutate: func(f historicalFleetCacheFixture) { f.independent.finalizedBlock = 299 }, want: "not finalized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHistoricalFleetCacheFixture(t, 2, true)
			if _, err := fixture.executor.verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls); err != nil {
				t.Fatal(err)
			}
			test.mutate(fixture)
			keys, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls)
			if err == nil || !strings.Contains(err.Error(), test.want) || len(keys) != 0 {
				t.Fatalf("warm checkpoint keys=%v err=%v, want %s", keys, err, test.want)
			}
			if fixture.operational.contractBatchRequests != 1 || fixture.independent.contractBatchRequests != 1 {
				t.Fatal("state replay occurred after the checkpoint failure")
			}
		})
	}
}

func TestHistoricalFleetCacheWarmCallsStillRecheckDerivedAliases(t *testing.T) {
	fixture := newHistoricalFleetCacheFixture(t, 2, false)
	alias := &fixture.calls[1]
	alias.data, alias.expectation = nil, nil
	localReads := 0
	localValid := true
	alias.observe = func([]byte) (map[string]any, error) {
		localReads++
		if !localValid {
			return nil, fmt.Errorf("derived alias local evidence changed")
		}
		return map[string]any{"index": 1}, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		keys, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls)
		if err != nil || len(keys) != 2 {
			t.Fatalf("attempt %d keys=%v err=%v", attempt, keys, err)
		}
	}
	if localReads != 2 || fixture.operational.contractBatchRequests != 1 {
		t.Fatalf("warm immutable/local reads=%d/%d, want 1/2", fixture.operational.contractBatchRequests, localReads)
	}
	localValid = false
	keys, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls)
	if err == nil || !strings.Contains(err.Error(), "local evidence changed") || len(keys) != 0 {
		t.Fatalf("mutated local alias inherited warm keys=%v err=%v", keys, err)
	}
	if localReads != 3 || fixture.operational.contractBatchRequests != 1 || fixture.operational.blockBatchRequests != 3 {
		t.Fatalf("warm alias local/contract/checkpoint counts=%d/%d/%d", localReads, fixture.operational.contractBatchRequests, fixture.operational.blockBatchRequests)
	}
}

func TestHistoricalFleetCachePersistsOnlyDualObserverSuccessBeforeLaterFailure(t *testing.T) {
	fixture := newHistoricalFleetCacheFixture(t, 3, true)
	// The third action has its own checkpoint, so the first two succeed while
	// this independent JSON-RPC element fails in the very same HTTP batch.
	fixture.independent.failureBlock = fixture.calls[2].record.IndependentEVMFinalized.Number
	keys, err := fixture.executor.verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls)
	if err == nil || !strings.Contains(err.Error(), "independent historical fleet call") || len(keys) != 0 {
		t.Fatalf("failed initial audit keys=%v err=%v", keys, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			fixture.independent.failureBlock = 0
		}
		beforeOperational := len(fixture.operational.contractSelectors)
		beforeIndependent := len(fixture.independent.contractSelectors)
		_, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls)
		if (err != nil) != (attempt == 0) {
			t.Fatalf("retry %d error=%v", attempt, err)
		}
		if len(fixture.operational.contractSelectors)-beforeOperational != 1 || len(fixture.independent.contractSelectors)-beforeIndependent != 1 {
			t.Fatalf("retry %d did not retain earlier dual-observer successes", attempt)
		}
		if fixture.operational.contractSelectors[beforeOperational] != 101 || fixture.independent.contractSelectors[beforeIndependent] != 301 {
			t.Fatal("retry replayed a previously successful action")
		}
	}
	before := fixture.operational.contractBatchRequests + fixture.independent.contractBatchRequests
	if _, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls); err != nil {
		t.Fatal(err)
	}
	if fixture.operational.contractBatchRequests+fixture.independent.contractBatchRequests != before {
		t.Fatal("fully warm dual-observer audit repeated contract reads")
	}
}

func TestHistoricalFleetCacheBindsExactReplayInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*historicalFleetGenerationOneCall)
	}{
		{name: "calldata", mutate: func(c *historicalFleetGenerationOneCall) { c.data = []byte{0x42} }},
		{name: "target", mutate: func(c *historicalFleetGenerationOneCall) { c.address[0] ^= 1 }},
		{name: "decoder evidence", mutate: func(c *historicalFleetGenerationOneCall) { c.expectation = map[string]any{"result": 2} }},
		{name: "action intent", mutate: func(c *historicalFleetGenerationOneCall) { c.action.IntentHash = "changed" }},
		{name: "verified receipt", mutate: func(c *historicalFleetGenerationOneCall) { c.entry.PostconditionHash = "changed" }},
		{name: "recorded observation", mutate: func(c *historicalFleetGenerationOneCall) {
			c.record.Observed = map[string]any{"index": 9}
			c.record.IndependentObserved = c.record.Observed
		}},
		{name: "historical block", mutate: func(c *historicalFleetGenerationOneCall) {
			c.record.EVMFinalized = ChainHead{Number: 101, Hash: fleetHistoryBatchBlockHash(101)}
			c.record.IndependentEVMFinalized = c.record.EVMFinalized
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHistoricalFleetCacheFixture(t, 1, false)
			if _, err := fixture.executor.verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls); err != nil {
				t.Fatal(err)
			}
			test.mutate(&fixture.calls[0])
			fixture.operational.contractResult = "0x02"
			if _, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls); err == nil {
				t.Fatal("changed replay input inherited an old success")
			}
			if fixture.operational.contractBatchRequests != 2 {
				t.Fatal("changed replay input was not replayed")
			}
		})
	}
}

func TestHistoricalFleetCacheTamperedDiskEntryReplays(t *testing.T) {
	fixture := newHistoricalFleetCacheFixture(t, 1, false)
	if _, err := fixture.executor.verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls); err != nil {
		t.Fatal(err)
	}
	var entries []string
	if err := filepath.WalkDir(fixture.executor.stateDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			entries = append(entries, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("durable cache entries=%v, want one", entries)
	}
	if err := os.WriteFile(entries[0], []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.operational.contractResult = "0x02"
	if _, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyHistoricalFleetGenerationOneCalls(context.Background(), fixture.calls); err == nil {
		t.Fatal("tampered disk entry suppressed a failing historical replay")
	}
	if fixture.operational.contractBatchRequests != 2 {
		t.Fatal("tampered cache did not cause a replay")
	}
}

func newHistoricalFleetBindingCacheFixture(t *testing.T) (historicalFleetCacheFixture, carriedActionAudit, FleetBindingEvidence) {
	t.Helper()
	fixture := newHistoricalFleetCacheFixture(t, 1, false)
	source := newFleetGenerationOneSupersessionFixture(t, "legacy-binding")
	fixture.executor.cfg.Config.Topology = source.cfg.Config.Topology
	fixture.executor.plan.PriorPlanHashes = []string{source.sourceEntry.PlanHash}
	fixture.executor.plan.Actions = []Action{source.sourceAction, source.installAction, source.refreshAction}
	fixture.executor.keeper = fixture.executor.deployer
	fixture.executor.payloads = &DeploymentPayloads{Manifest: ContractDeployment{
		CoordinatorProxy: common.HexToAddress("0x1234567890123456789012345678901234567890"),
	}}
	evidence := FleetBindingEvidence{
		Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: "0x" + strings.Repeat("77", 16),
		Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 33, UID: 7,
	}
	if err := os.MkdirAll(filepath.Join(fixture.executor.stateDir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(fixture.executor.stateDir, "public", "fleet-1-member-1.binding.json"), evidence); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		record *ActionPostcondition
		entry  *JournalEntry
	}{
		{record: source.sourceRecord, entry: &source.sourceEntry},
		{record: source.installRecord, entry: &source.installEntry},
		{record: source.refreshRecord, entry: &source.refreshEntry},
	} {
		item.record.OperationalRPCMode = fixture.executor.cfg.OperationalRPCMode
		item.record.IndependentRPC = false
		item.record.EVMFinalized.Hash = fleetHistoryBatchBlockHash(item.record.EVMFinalized.Number)
		item.record.IndependentEVMFinalized = item.record.EVMFinalized
		path, hash, err := fixture.executor.persistActionPostcondition(item.record)
		if err != nil {
			t.Fatal(err)
		}
		item.entry.PostconditionPath, item.entry.PostconditionHash = path, hash
	}
	fixture.executor.journal = &Journal{entries: []JournalEntry{source.sourceEntry, source.installEntry, source.refreshEntry}}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	output, err := parsed.Methods["bindingAt"].Outputs.Pack(true, stabi.STCoordinatorBindingRecord{Generation: 1, Uid: 7})
	if err != nil {
		t.Fatal(err)
	}
	fixture.operational.contractResult = hexutil.Encode(output)
	return fixture, carriedActionAudit{action: source.sourceAction, entry: source.sourceEntry, record: source.sourceRecord}, evidence
}

func TestHistoricalFleetCacheRechecksLocalDecoderEvidenceAndSourceReceipts(t *testing.T) {
	fixture, audit, evidence := newHistoricalFleetBindingCacheFixture(t)
	audits := []carriedActionAudit{audit}
	if _, err := fixture.executor.verifyCarriedFleetGenerationOneHistory(context.Background(), audits); err != nil {
		t.Fatal(err)
	}
	if _, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyCarriedFleetGenerationOneHistory(context.Background(), audits); err != nil {
		t.Fatal(err)
	}
	if fixture.operational.contractBatchRequests != 1 {
		t.Fatal("exact carried binding did not reuse its historical replay")
	}
	// Generation affects the decoder but is absent from both bindingAt's
	// calldata and the legacy compact observation. It must still invalidate.
	evidence.Generation = 2
	if err := writePublicJSON(filepath.Join(fixture.executor.stateDir, "public", "fleet-1-member-1.binding.json"), evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyCarriedFleetGenerationOneHistory(context.Background(), audits); err == nil || !strings.Contains(err.Error(), "binding mismatch") {
		t.Fatalf("changed decoder evidence error=%v", err)
	}
	if fixture.operational.contractBatchRequests != 2 {
		t.Fatal("changed local generation did not force historical replay")
	}
	audit.record.Observed["uid"] = 8
	audit.record.IndependentObserved["uid"] = 8
	before := fixture.operational.httpRequests
	if _, err := restartHistoricalFleetCacheExecutor(fixture.executor).verifyCarriedFleetGenerationOneHistory(context.Background(), []carriedActionAudit{audit}); err == nil || !strings.Contains(err.Error(), "receipt hash") {
		t.Fatalf("changed source receipt error=%v", err)
	}
	if fixture.operational.httpRequests != before {
		t.Fatal("tampered local receipt reached the RPC")
	}
}

func TestHistoricalFleetCacheCannotBypassSuccessorOrCurrentStateChecks(t *testing.T) {
	fixture, audit, _ := newHistoricalFleetBindingCacheFixture(t)
	if _, err := fixture.executor.verifyCarriedFleetGenerationOneHistory(context.Background(), []carriedActionAudit{audit}); err != nil {
		t.Fatal(err)
	}
	restarted := restartHistoricalFleetCacheExecutor(fixture.executor)
	refresh := restarted.journal.entries[2]
	refreshPath := filepath.Join(restarted.stateDir, filepath.FromSlash(refresh.PostconditionPath))
	if err := os.WriteFile(refreshPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := fixture.operational.httpRequests
	if _, err := restarted.verifyCarriedFleetGenerationOneHistory(context.Background(), []carriedActionAudit{audit}); err == nil || !strings.Contains(err.Error(), "refresh postcondition") {
		t.Fatalf("tampered successor receipt error=%v", err)
	}
	if fixture.operational.httpRequests != before {
		t.Fatal("tampered successor reached the RPC")
	}
	// Without a verified refresh the same source is a current-state assertion.
	// Its existing durable historical entry must not install a carried key.
	restarted.journal = &Journal{entries: append([]JournalEntry(nil), restarted.journal.entries[:2]...)}
	keys, err := restarted.verifyCarriedFleetGenerationOneHistory(context.Background(), []carriedActionAudit{audit})
	if err != nil || len(keys) != 0 {
		t.Fatalf("source without a successor inherited historical keys=%v err=%v", keys, err)
	}
	restarted.carriedFleetHistoryKeys = keys
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	head := ChainHead{Number: 1_000, Hash: fleetHistoryBatchBlockHash(1_000)}
	err = restarted.verifyVerifiedActionStateWithRecord(cancelled, audit.action, audit.entry, audit.record, &head, nil)
	if err == nil || !strings.Contains(err.Error(), "current postcondition") || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("current binding state was not rechecked: %v", err)
	}
}
