//go:build linux || darwin

// The approved plan and the campaign proxy are deliberately different
// identities. Actual fixed files, source signatures and Http clients exercise
// that separation without binding a shared fixed port or using a live endpoint.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The private-node profile preserves its independently approved read origin.
// Its actual reverse proxy is ephemeral; the canonical workload transport is
// still the exact production campaign derivative, never an arbitrary override.
type evidenceRelayLaunchRuntimeTestFixture struct {
	base        *runtimeEvidenceProvisionV2TestFixture
	executor    *Executor
	requests    atomic.Uint64
	forwarded   atomic.Uint64
	headStarted chan struct{}
	headExited  chan struct{}
	planBytes   []byte
}

// Original signed setup files and an actual reverse proxy own separate
// configuration identities; the head barrier gives cancellation a real read.
func newEvidenceRelayLaunchRuntimeTestFixture(t *testing.T, nullHead bool, finalized ...uint64) *evidenceRelayLaunchRuntimeTestFixture {
	t.Helper()
	if len(finalized) > 1 || len(finalized) == 1 && (nullHead || finalized[0] == 0) {
		t.Fatal("fixture finalized response is ambiguous")
	}
	self := &evidenceRelayLaunchRuntimeTestFixture{headStarted: make(chan struct{}, 1), headExited: make(chan struct{}, 1)}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		self.requests.Add(1)
		var call evidenceRelayRpcRequest
		raw, err := io.ReadAll(io.LimitReader(request.Body, 64*1024))
		if err != nil || json.Unmarshal(raw, &call) != nil || len(call.Id) == 0 || request.Method != http.MethodPost {
			t.Errorf("relay used an invalid actual Rpc request: %v", err)
			http.Error(writer, "invalid fixture request", http.StatusBadRequest)
			return
		}
		var value any
		switch call.Method {
		case "eth_chainId":
			value = fmt.Sprintf("0x%x", testnetChainID)
		case "eth_getBlockByNumber":
			if len(call.Params) != 2 || string(call.Params[0]) != `"finalized"` || string(call.Params[1]) != "false" {
				t.Error("relay did not read the real finalized observer")
				http.Error(writer, "invalid observer", http.StatusBadRequest)
				return
			}
			self.headStarted <- struct{}{}
			defer func() { self.headExited <- struct{}{} }()
			if len(finalized) == 1 {
				value = map[string]any{"number": fmt.Sprintf("0x%x", finalized[0]), "hash": fmt.Sprintf("0x%064x", uint64(0x91))}
			} else if !nullHead {
				<-request.Context().Done()
				return
			}
		default:
			t.Errorf("relay sent unexpected Rpc method %s before any public slot existed", call.Method)
			http.Error(writer, "unexpected fixture method", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": value}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(upstream.Close)
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		self.forwarded.Add(1)
		proxy.ServeHTTP(writer, request)
	}))
	t.Cleanup(endpoint.Close)
	self.base = newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, func(cfg *ResolvedConfig) {
		cfg.Authority = "https://approved-workload.example"
		cfg.OperationalSubstrate = "wss://approved-native.example"
		cfg.OperationalEVM = "https://approved-workload.example"
		cfg.Public.Chain.EVMPublicReadEndpoint = endpoint.URL
	})
	base := self.base
	owner := &Executor{cfg: base.cfg, plan: base.plan, roles: base.roles, stateDir: base.stateDir}
	if err := owner.retainRuntimeEvidenceInputsV2(t.Context(), base.prepared, base.preparedBytes, base.completed); err != nil {
		t.Fatal(err)
	}
	self.planBytes, err = os.ReadFile(filepath.Join(base.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := campaignRPCConfig(base.cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, err := ethclient.DialContext(t.Context(), endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	// This fixture stops at the first real Evm read, before the separately
	// authenticated native horizon. It never claims funded readiness.
	self.executor = &Executor{cfg: runtime, plan: base.plan, roles: base.roles, stateDir: base.stateDir,
		substrate: &SubstrateManager{chain: &crv4.Chain{}}, keeper: &EvmTxManager{client: client}, independentEVM: client}
	return self
}

// Before the fix the original proxy derivative reaches the unchanged plan
// checker and fails its endpoint hash before any real chain client can open.
func TestEvidenceRelayRuntimeCanonicalProxyKeepsApprovedPlanAndJoins(t *testing.T) {
	fixture := newEvidenceRelayLaunchRuntimeTestFixture(t, false)
	base := fixture.base
	if fixture.executor.cfg.OperationalEVM == base.cfg.OperationalEVM {
		t.Fatal("fixture omitted the actual campaign transport rewrite")
	}
	if _, err := runtimeEvidenceV2ResolvedConfig(fixture.executor.cfg, base.stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("the source-derived pre-fix root no longer refuses rewritten approval: %v", err)
	}
	failures := make(chan error, 1)
	worker, err := newEvidenceRelayRuntime(t.Context(), base.cfg, fixture.executor, "release-1.0", false, func(err error) { failures <- err })
	if err != nil {
		t.Fatalf("real relay constructor confused canonical approval and proxy transport: %v", err)
	}
	t.Cleanup(func() {
		if err := worker.Close(); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-fixture.headStarted:
	case err := <-failures:
		t.Fatalf("actual worker failed before its observer: %v", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if len(worker.sources) != 2 || len(worker.sources[0].activations) != 2 || len(worker.sources[1].activations) != 2 || worker.origins != [2]string{base.cfg.OperatorAPIOrigins[0], base.cfg.OperatorAPIOrigins[1]} {
		t.Fatal("proxy startup lost the complete original four-source census")
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fixture.headExited:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	select {
	case err := <-failures:
		t.Fatalf("owned cancellation became a real failure: %v", err)
	default:
	}
	if fixture.requests.Load() != 2 || fixture.forwarded.Load() != 2 {
		t.Fatal("constructor/worker did not traverse the actual proxy exactly once per read")
	}
	after, err := os.ReadFile(filepath.Join(base.stateDir, "plan.json"))
	if err != nil || !bytes.Equal(after, fixture.planBytes) {
		t.Fatal("transport startup rewrote approved plan bytes", err)
	}
	if _, err := loadPersistedPlan(base.cfg, base.stateDir); err != nil {
		t.Fatal(err)
	}
}

// The public-override derivative is checked before any dial. An intentionally
// absent chain owner is the next refusal, not a plan hash mismatch or a waiver.
func TestEvidenceRelayRuntimePublicDerivativeDoesNotBecomeApproval(t *testing.T) {
	fixture := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, func(cfg *ResolvedConfig) {
		cfg.OperationalRPCMode = rpcModePublicOverride
		cfg.OperationalEVM = "https://approved-public.example"
		cfg.Public.Chain.EVMPublicReadEndpoint = cfg.OperationalEVM
	})
	owner := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := owner.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	runtime, err := campaignRPCConfig(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Config.LaunchInputs.PublicEVMMaximumRequestsPerMinute != 0 || runtime.OperationalEVM == fixture.cfg.OperationalEVM {
		t.Fatal("fixture lost the actual public proxy derivative")
	}
	owner.cfg = runtime
	worker, err := newEvidenceRelayRuntime(t.Context(), fixture.cfg, owner, "release-1.0", false, func(error) { t.Error("refused construction launched work") })
	if worker != nil || err == nil || !strings.Contains(err.Error(), "validator evidence activation runtime owners are incomplete") {
		t.Fatalf("public proxy failed before independently authenticating the original fixed files: %v", err)
	}
	if _, err := loadPersistedPlan(runtime, fixture.stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatal("runtime transport became approved plan authority", err)
	}
}

// A local endpoint is not authority merely because it is loopback. Changed
// canonical origins still fail the original persisted-plan identity check.
func TestEvidenceRelayRuntimeRejectsTransportAndApprovalSubstitution(t *testing.T) {
	fixture := newEvidenceRelayLaunchRuntimeTestFixture(t, false)
	for _, fault := range []string{"transport", "quota", "approved-endpoint", "approved-origin"} {
		approved := *fixture.base.cfg
		if fault == "approved-endpoint" {
			approved.OperationalEVM = "https://unapproved-workload.example"
		}
		if fault == "approved-origin" {
			approved.OperatorAPIOrigins = []string{"https://changed-operator.example", fixture.base.cfg.OperatorAPIOrigins[1]}
		}
		runtime, err := campaignRPCConfig(&approved)
		if err != nil {
			t.Fatal(err)
		}
		if fault == "transport" {
			runtime.OperationalEVM = "http://127.0.0.1:1"
		}
		if fault == "quota" {
			runtime.Config.LaunchInputs.PublicEVMMaximumRequestsPerMinute++
		}
		executor := *fixture.executor
		executor.cfg = runtime
		worker, err := newEvidenceRelayRuntime(t.Context(), &approved, &executor, "release-1.0", false, func(error) { t.Error("refused authority launched work") })
		if err == nil || worker != nil {
			t.Fatalf("%s substitution reached the worker", fault)
		}
		if strings.HasPrefix(fault, "approved-") && !errors.Is(err, errPersistedPlanIdentityMismatch) {
			t.Fatalf("%s bypassed original plan authentication: %v", fault, err)
		}
	}
	if fixture.requests.Load() != 0 || fixture.forwarded.Load() != 0 {
		t.Fatal("refused transport or approval initiated external work")
	}
}

// A changed original consent fails before any transport owner can start.
func TestEvidenceRelayRuntimeRejectsChangedOriginalSourceBeforeIo(t *testing.T) {
	fixture := newEvidenceRelayLaunchRuntimeTestFixture(t, false)
	paths, _, _ := runtimeEvidenceV2Paths(fixture.base.stateDir, 2, 2)
	if err := os.WriteFile(paths[2], bytes.Repeat([]byte{0x19}, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	worker, err := newEvidenceRelayRuntime(t.Context(), fixture.base.cfg, fixture.executor, "release-1.0", false, func(error) { t.Error("changed source launched work") })
	if err == nil || worker != nil {
		t.Fatal("changed original hotkey consent reached the worker")
	}
	if fixture.requests.Load() != 0 || fixture.forwarded.Load() != 0 {
		t.Fatal("invalid original file performed proxy I/O")
	}
}

// A real null finalized response is not an empty publication or owned cancel.
func TestEvidenceRelayRuntimeRetainsRealProxyFailure(t *testing.T) {
	fixture := newEvidenceRelayLaunchRuntimeTestFixture(t, true)
	failures := make(chan error, 1)
	worker, err := newEvidenceRelayRuntime(t.Context(), fixture.base.cfg, fixture.executor, "release-1.0", false, func(err error) { failures <- err })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	select {
	case failure := <-failures:
		if !strings.Contains(failure.Error(), "finalized EVM head is empty or invalid") {
			t.Fatalf("real observer failure changed classification: %v", failure)
		}
	case <-t.Context().Done():
		_ = worker.Close()
		t.Fatal(t.Context().Err())
	}
	if err := worker.Close(); err == nil || !strings.Contains(err.Error(), "finalized EVM head is empty or invalid") {
		t.Fatal("join discarded a real proxy failure", err)
	}
	if fixture.requests.Load() != 2 || fixture.forwarded.Load() != 2 {
		t.Fatal("real failure did not traverse the complete actual transport")
	}
}
