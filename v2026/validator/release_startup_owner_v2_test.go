//go:build linux || darwin

// These controls join real semantic disk startup to the concrete V2 steerer.
// The native transport control refuses unreviewed bytes; it does not fabricate
// a successful measurement, prepared transaction or finalized receipt.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	substrateRpc "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urnetwork/connect/v2026"
)

// Existing real startup proves the activation signatures, exact native history,
// dormant disk census and protected destinations. Readers retain those origins.
func newReleaseStartupOwnerV2TestFixture(t *testing.T) (*releaseRuntimeV2TestFixture, *ReleaseSteerer) {
	t.Helper()
	fixture := newReleaseRuntimeV2TestFixture(t)
	contexts := make([]*ReleaseMeasurementContext, len(fixture.runtimes))
	for index, runtime := range fixture.runtimes {
		operator := fixture.runtime.cfg.Operators[index]
		reader, err := NewHTTPArtifactReader(operator.APIURL, fixture.runtime.cfg.DeploymentID, fixture.runtime.cfg.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		history, err := NewHTTPClientKeyHistoryReader(operator.APIURL, runtime.attemptUpload.writer.byJwt)
		if err != nil {
			t.Fatal(err)
		}
		runtime.measurement.Artifacts, runtime.measurement.ClientKeyHistory = reader, history
		// This routing fixture stops at actual native authentication. A later
		// client-key call must fail, never provide invented provider authority.
		runtime.measurement.ClientKey = func(connect.Id) ([32]byte, bool, error) {
			return [32]byte{}, false, errors.New("unexpected provider lookup after native refusal")
		}
		contexts[index] = runtime.measurement
	}
	steerer, err := newReleaseSteererV2(&fixture.startup.cfg, fixture.startup.chain, fixture.startup.nativeFixture.chain, fixture.hotkey, contexts, fixture.runtime)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, steerer
}

// Only the real transport adapter adds the URL required by the native client
// interface. CallContext and cancellation remain the library implementation.
type releaseStartupOwnerV2NativeHttpClient struct {
	*substrateRpc.Client
	endpoint string
}

func (self *releaseStartupOwnerV2NativeHttpClient) URL() string { return self.endpoint }

// The exact finalized hash and a deliberately unreviewed version are returned
// through Http. No state result or success/eligibility callback is injected.
func releaseStartupOwnerV2NativeRefusal(t *testing.T, fixture *releaseRuntimeV2TestFixture) func() []string {
	t.Helper()
	var stateLock sync.Mutex
	var methods []string
	block := types.Hash{0x41, 0x72}
	version := releaseRuntimeIdentityV2(&fixture.runtime.cfg).Version
	version.SpecVersion++
	endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			JsonRpc string            `json:"jsonrpc"`
			Id      json.RawMessage   `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.JsonRpc != "2.0" {
			t.Error("native owner used invalid Rpc framing")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		func() {
			stateLock.Lock()
			defer stateLock.Unlock()
			methods = append(methods, call.Method)
		}()
		var result any
		switch call.Method {
		case "chain_getFinalizedHead":
			if len(call.Params) != 0 {
				t.Error("finalized selection supplied unexpected parameters")
			}
			result = block.Hex()
		case "state_getRuntimeVersion":
			var selected string
			if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &selected) != nil || selected != block.Hex() {
				t.Error("submission did not authenticate its exact selected native hash")
			}
			result = version
		default:
			t.Errorf("unreviewed native runtime reached method %s", call.Method)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(struct {
			JsonRpc string          `json:"jsonrpc"`
			Id      json.RawMessage `json:"id"`
			Result  any             `json:"result"`
		}{JsonRpc: "2.0", Id: call.Id, Result: result}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(endpoint.Close)
	client, err := substrateRpc.DialContext(t.Context(), endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	old := fixture.startup.nativeFixture.chain.API.Client
	fixture.startup.nativeFixture.chain.API.Client = &releaseStartupOwnerV2NativeHttpClient{Client: client, endpoint: endpoint.URL}
	t.Cleanup(func() {
		fixture.startup.nativeFixture.chain.API.Client = old
		client.Close()
	})
	return func() []string {
		stateLock.Lock()
		defer stateLock.Unlock()
		return append([]string(nil), methods...)
	}
}

// This passes the corrected startup ownership boundary and executes the actual
// public SubmitOnce dispatch. Reviewed production pins still refuse this peer.
func TestReleaseStartupOwnerV2ConstructsAndRoutesActualSubmission(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	keys, err := readReleaseServerKeysV2(t.Context(), &fixture.runtime.cfg)
	if err != nil || !reflect.DeepEqual(keys, fixture.startup.keys) {
		t.Fatalf("actual server-key readback changed startup owners: %v", err)
	}
	if err := requireReleaseEvidenceV2Runtime(steerer); err != nil {
		t.Fatal(err)
	}
	if steerer.intents.v2.runtime != fixture.runtime || steerer.native != fixture.runtime.native || steerer.hotkey != fixture.runtime.hotkey {
		t.Fatal("V2 constructor substituted an intent, native or hotkey owner")
	}
	if current, err := steerer.intents.Current(); err != nil || current != nil {
		t.Fatalf("actual empty V2 intent custody: current=%v error=%v", current, err)
	}
	methods := releaseStartupOwnerV2NativeRefusal(t, fixture)
	priorMetadata, priorRuntime := steerer.native.Meta, steerer.native.Runtime
	err = steerer.SubmitOnce(t.Context())
	if err == nil || !strings.Contains(err.Error(), "authenticate native runtime before steering snapshot") || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("real submission did not retain native authority refusal: %v", err)
	}
	if got := methods(); !reflect.DeepEqual(got, []string{"chain_getFinalizedHead", "state_getRuntimeVersion"}) {
		t.Fatalf("real submission read transcript=%v", got)
	}
	if steerer.native.Meta != priorMetadata || steerer.native.Runtime != priorRuntime {
		t.Fatal("failed private signing view changed the shared native observer")
	}
	if _, err := os.Lstat(steerer.intents.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreviewed native state created an intent: %v", err)
	}
}

// Removing either half of the joined V2 owner is a refusal, not a switch into
// the legacy implementation. Each case starts with an actual constructed root.
func TestReleaseStartupOwnerV2RejectsLostOwnershipBeforeNativeIo(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	methods := releaseStartupOwnerV2NativeRefusal(t, fixture)
	originalIntentOwner := steerer.intents.v2
	// Hold the real store lock so a copied mutex deterministically cannot
	// masquerade as a fresh missing-owner fixture.
	steerer.intents.mu.Lock()
	defer steerer.intents.mu.Unlock()
	cases := []struct {
		name   string
		change func(*ReleaseSteerer)
	}{
		{name: "runtime missing", change: func(value *ReleaseSteerer) { value.runtimeV2 = nil }},
		{name: "runtime and intent absent", change: func(value *ReleaseSteerer) {
			value.runtimeV2, value.intents = nil, nil
		}},
		{name: "runtime and intent marker absent", change: func(value *ReleaseSteerer) {
			value.runtimeV2, value.intents = nil, &IntentStore{path: value.intents.path, stateDir: value.intents.stateDir}
		}},
		{name: "explicit V2 configuration only", change: func(value *ReleaseSteerer) {
			*value = ReleaseSteerer{cfg: value.cfg}
		}},
		{name: "unknown evidence schema only", change: func(value *ReleaseSteerer) {
			cfg := *value.cfg
			cfg.EvidenceV2.Schema = "synthetic-unrecognized-evidence"
			*value = ReleaseSteerer{cfg: &cfg}
		}},
		{name: "intent missing", change: func(value *ReleaseSteerer) { value.intents = nil }},
		{name: "intent owner missing", change: func(value *ReleaseSteerer) {
			value.intents = &IntentStore{path: value.intents.path, stateDir: value.intents.stateDir}
		}},
		{name: "head owner missing", change: func(value *ReleaseSteerer) { value.headEMA = nil }},
		{name: "foreign native owner", change: func(value *ReleaseSteerer) {
			copy := *value.native
			value.native = &copy
		}},
		{name: "source configuration changed", change: func(value *ReleaseSteerer) {
			copy := *value.cfg
			copy.DeploymentID += "-foreign"
			value.cfg = &copy
		}},
		{name: "measurement census absent", change: func(value *ReleaseSteerer) { value.contexts = nil }},
	}
	for _, testCase := range cases {
		value := *steerer
		testCase.change(&value)
		if value.intents != nil && value.intents != steerer.intents {
			if value.intents.v2 != nil || value.intents.path != steerer.intents.path || value.intents.stateDir != steerer.intents.stateDir {
				t.Fatalf("%s changed retained paths or copied the original V2 owner", testCase.name)
			}
			if !value.intents.mu.TryLock() {
				t.Fatalf("%s copied the original locked mutex", testCase.name)
			}
			value.intents.mu.Unlock()
		}
		if err := value.SubmitOnce(t.Context()); err == nil {
			t.Errorf("%s acquired submission ownership", testCase.name)
		}
		if got := methods(); len(got) != 0 {
			t.Fatalf("%s reached native I/O before ownership refusal: %v", testCase.name, got)
		}
		if steerer.intents.v2 != originalIntentOwner || steerer.runtimeV2 != fixture.runtime {
			t.Fatalf("%s changed the authenticated original owner", testCase.name)
		}
	}
}

// Only a genuinely absent V2 declaration may reach the compatibility path.
// Its ordinary native authority check remains real and refuses the same peer.
func TestReleaseStartupOwnerV2DoesNotUpgradeAnUndeclaredLegacySteerer(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	methods := releaseStartupOwnerV2NativeRefusal(t, fixture)
	cfg := *steerer.cfg
	cfg.EvidenceV2 = ReleaseEvidenceV2Config{}
	legacy := &ReleaseSteerer{cfg: &cfg, native: steerer.native}
	err := legacy.SubmitOnce(t.Context())
	if err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("undeclared legacy path lost its native authority check: %v", err)
	}
	if got := methods(); !reflect.DeepEqual(got, []string{"chain_getFinalizedHead", "state_getRuntimeVersion"}) {
		t.Fatalf("undeclared legacy transport transcript=%v", got)
	}
}

// The caller cannot attach a different approved-looking configuration to an
// already authenticated disk root or supply an incomplete measurement census.
func TestReleaseStartupOwnerV2ConstructorRejectsForeignInputs(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	methods := releaseStartupOwnerV2NativeRefusal(t, fixture)
	contexts := make([]*ReleaseMeasurementContext, 0, len(fixture.runtimes))
	for _, runtime := range fixture.runtimes {
		contexts = append(contexts, runtime.measurement)
	}
	cfg := fixture.startup.cfg
	cfg.DeploymentID += "-foreign"
	if value, err := newReleaseSteererV2(&cfg, steerer.chain, steerer.native, fixture.hotkey, contexts, fixture.runtime); err == nil || value != nil {
		t.Fatalf("foreign startup configuration accepted: %v", err)
	}
	if value, err := newReleaseSteererV2(&fixture.startup.cfg, steerer.chain, steerer.native, fixture.hotkey, contexts[:1], fixture.runtime); err == nil || value != nil {
		t.Fatalf("partial actual measurement census accepted: %v", err)
	}
	missingHistory := *contexts[0]
	missingHistory.ClientKeyHistory = nil
	contexts[0] = &missingHistory
	if value, err := newReleaseSteererV2(&fixture.startup.cfg, steerer.chain, steerer.native, fixture.hotkey, contexts, fixture.runtime); err == nil || value != nil {
		t.Fatalf("missing client-key history owner accepted: %v", err)
	}
	if got := methods(); len(got) != 0 {
		t.Fatalf("foreign startup inputs reached native I/O: %v", got)
	}
}

// Parent cancellation still clamps submission before the first Http request.
func TestReleaseStartupOwnerV2CanceledSubmissionDoesNotReadNative(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	methods := releaseStartupOwnerV2NativeRefusal(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := steerer.SubmitOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled actual submission: %v", err)
	}
	if got := methods(); len(got) != 0 {
		t.Fatalf("canceled submission reached native I/O: %v", got)
	}
}

// The public entrypoint advances past the removed fence through actual private
// seed custody and one real Evm request. The transport error prevents native
// dialing, credentials, operator workers and state creation.
func TestRunReleaseV2ReadsOwnedSeedAndActualEvmTransport(t *testing.T) {
	cfg := validReleaseConfig(t)
	private := filepath.Join(t.TempDir(), "seed-owner")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg.HotkeySeedFile = filepath.Join(private, "hotkey.seed")
	seed := [32]byte{0x52}
	if err := os.WriteFile(cfg.HotkeySeedFile, seed[:], 0o600); err != nil {
		t.Fatal(err)
	}
	var stateLock sync.Mutex
	var methods []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		func() {
			stateLock.Lock()
			defer stateLock.Unlock()
			methods = append(methods, call.Method)
		}()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-32000,\"message\":\"synthetic Evm admission refusal\"}}", call.Id)
	}))
	t.Cleanup(endpoint.Close)
	cfg.RPC = []string{endpoint.URL}
	err := RunRelease(t.Context(), writeReleaseConfig(t, cfg))
	if err == nil || !strings.Contains(err.Error(), "synthetic Evm admission refusal") {
		t.Fatalf("public root did not reach actual Evm transport: %v", err)
	}
	stateLock.Lock()
	got := append([]string(nil), methods...)
	stateLock.Unlock()
	if !reflect.DeepEqual(got, []string{"eth_chainId"}) {
		t.Fatalf("public startup transport transcript=%v", got)
	}
	retained, err := crv4.LoadSeedFile(cfg.HotkeySeedFile)
	if err != nil || retained != seed {
		t.Fatalf("public startup changed its original seed custody: %v", err)
	}
	for _, path := range []string{cfg.StateDir, cfg.Operators[0].StateDir, cfg.Operators[1].StateDir} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed Evm admission created state %s: %v", path, err)
		}
	}
}

// Absent evidence ownership remains a strict configuration failure, before
// even a missing seed can become the reason for refusal.
func TestRunReleaseV2RejectsAbsentEvidenceBeforeSeedOrState(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.EvidenceV2 = ReleaseEvidenceV2Config{}
	err := RunRelease(t.Context(), writeReleaseConfig(t, cfg))
	if err == nil || strings.Contains(err.Error(), "production hotkey seed") {
		t.Fatalf("missing V2 configuration reached seed custody: %v", err)
	}
	for _, path := range []string{cfg.StateDir, cfg.HotkeySeedFile} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid configuration created state %s: %v", path, err)
		}
	}
}

// Removing the old fence must not turn a missing lifecycle into a panic or
// let an already canceled root read configuration, seed or credential files.
func TestRunReleaseV2RefusesAbsentOrCanceledLifecycleBeforeInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent-validator.yml")
	if err := RunRelease(nil, path); err == nil || !strings.Contains(err.Error(), "lifecycle context is unavailable") {
		t.Fatalf("absent lifecycle reached configuration: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := RunRelease(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lifecycle reached configuration: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled entrypoint created configuration: %v", err)
	}
}
