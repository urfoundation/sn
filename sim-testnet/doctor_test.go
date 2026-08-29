package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gsrpcTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type doctorRPCFixture struct {
	parsed       abi.ABI
	acceptBadSig bool
	wrongUID     bool
}

func (f *doctorRPCFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      json.RawMessage   `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(request.ID)}
	if request.Method != "eth_call" || len(request.Params) < 1 {
		response["error"] = map[string]any{"code": -32601, "message": "unsupported"}
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	var call struct {
		To    string `json:"to"`
		Data  string `json:"data"`
		Input string `json:"input"`
	}
	if err := json.Unmarshal(request.Params[0], &call); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	input := call.Input
	if input == "" {
		input = call.Data
	}
	data, err := hex.DecodeString(strings.TrimPrefix(input, "0x"))
	if err != nil || len(data) < 4 {
		response["error"] = map[string]any{"code": -32602, "message": "bad calldata"}
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	method, err := f.parsed.MethodById(data[:4])
	if err != nil {
		response["error"] = map[string]any{"code": -32602, "message": err.Error()}
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	var output []byte
	switch method.Name {
	case "verify":
		args, unpackErr := method.Inputs.Unpack(data[4:])
		if unpackErr != nil {
			err = unpackErr
			break
		}
		s := args[3].([32]byte)
		address := common.HexToAddress(call.To)
		want := doctorEdKAT[3]
		if address == common.HexToAddress("0x403") {
			want = doctorSrKAT[3]
		}
		accepted := s == want || f.acceptBadSig
		output, err = method.Outputs.Pack(accepted)
	case "getUidCount":
		output, err = method.Outputs.Pack(uint16(3))
	case "getHotkey":
		output, err = method.Outputs.Pack([32]byte{1})
	case "getColdkey":
		output, err = method.Outputs.Pack([32]byte{2})
	case "getUid":
		args, unpackErr := method.Inputs.Unpack(data[4:])
		if unpackErr != nil {
			err = unpackErr
			break
		}
		hotkey := args[1].([32]byte)
		exists := hotkey == ([32]byte{1})
		uid := uint16(0)
		if f.wrongUID && exists {
			uid = 1
		}
		output, err = method.Outputs.Pack(exists, uid)
	case "getStake":
		output, err = method.Outputs.Pack(big.NewInt(9))
	case "getNominatorMinRequiredStake":
		output, err = method.Outputs.Pack(big.NewInt(7))
	default:
		err = errors.New("unsupported fixture method")
	}
	if err != nil {
		response["error"] = map[string]any{"code": -32000, "message": err.Error()}
	} else {
		response["result"] = "0x" + hex.EncodeToString(output)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func newDoctorRPCFixture(t *testing.T) (*doctorRPCFixture, *ethclient.Client, func()) {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(doctorPrecompileABI))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &doctorRPCFixture{parsed: parsed}
	server := httptest.NewServer(fixture)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return fixture, client, func() {
		client.Close()
		server.Close()
	}
}

func TestDoctorReadOnlyPrecompileBattery(t *testing.T) {
	fixture, client, closeFixture := newDoctorRPCFixture(t)
	defer closeFixture()
	cfg := testResolvedConfig(t)
	ctx := context.Background()
	if err := checkDoctorSignaturePrecompiles(ctx, client, fixture.parsed, 10); err != nil {
		t.Fatal(err)
	}
	if detail, err := checkDoctorIdentityPrecompiles(ctx, client, fixture.parsed, 10, cfg); err != nil || !strings.Contains(detail, "uid_count=3") {
		t.Fatalf("identity battery: detail=%q err=%v", detail, err)
	}
	if detail, err := checkDoctorStakingPrecompile(ctx, client, fixture.parsed, 10, cfg); err != nil || !strings.Contains(detail, "nominator_minimum_rao=7") {
		t.Fatalf("staking battery: detail=%q err=%v", detail, err)
	}
}

func TestDoctorPrecompileBatteryRejectsBadControls(t *testing.T) {
	fixture, client, closeFixture := newDoctorRPCFixture(t)
	defer closeFixture()
	fixture.acceptBadSig = true
	if err := checkDoctorSignaturePrecompiles(context.Background(), client, fixture.parsed, 10); err == nil {
		t.Fatal("signature battery accepted a precompile that verifies tampered signatures")
	}
	fixture.acceptBadSig = false
	fixture.wrongUID = true
	if _, err := checkDoctorIdentityPrecompiles(context.Background(), client, fixture.parsed, 10, testResolvedConfig(t)); err == nil {
		t.Fatal("identity battery accepted a mismatched neuron UID")
	}
}

func TestIndependentRPCEndpointsMustBeDistinct(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://public-substrate.example:443"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://public-evm.example"
	if err := validateIndependentRPCEndpoints(cfg); err != nil {
		t.Fatal(err)
	}

	cfg.Public.Chain.SubstratePublicReadEndpoint = "ws://127.0.0.1:9944"
	if err := validateIndependentRPCEndpoints(cfg); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("private Substrate endpoint was accepted as independent: %v", err)
	}

	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://public-substrate.example:443"
	cfg.Public.Chain.EVMPublicReadEndpoint = "http://127.0.0.1:9944"
	if err := validateIndependentRPCEndpoints(cfg); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("private EVM endpoint was accepted as independent: %v", err)
	}
}

func TestPublicRPCOverrideCannotClaimIndependentBackendAssurance(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.LaunchInputs.PublicSubstrateRPCOverride = "wss://test.finney.opentensor.ai:443"
	cfg.Config.LaunchInputs.PublicEVMRPCOverride = "https://test.chain.opentensor.ai"
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.OperationalSubstrate = cfg.Config.LaunchInputs.PublicSubstrateRPCOverride
	cfg.OperationalEVM = cfg.Config.LaunchInputs.PublicEVMRPCOverride
	cfg.Public.Chain.SubstratePublicReadEndpoint = cfg.OperationalSubstrate
	cfg.Public.Chain.EVMPublicReadEndpoint = cfg.OperationalEVM
	if err := validateOperationalRPCRouting(cfg); err != nil {
		t.Fatal(err)
	}
	if independentRPCRequired(cfg) {
		t.Fatal("public override was treated as an independent RPC deployment")
	}
	if err := validateIndependentRPCEndpoints(cfg); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("shared public operational/verifier endpoints were accepted as independent: %v", err)
	}
}

func TestRuntimeCodeHashValidationIsExact(t *testing.T) {
	want := "0x" + strings.Repeat("ab", 32)
	if err := validateRuntimeCodeHash(want[:2]+strings.ToUpper(want[2:]), want); err != nil {
		t.Fatalf("case-equivalent runtime hash rejected: %v", err)
	}
	for _, observed := range []string{"", "0x12", "0x" + strings.Repeat("zz", 32), "0x" + strings.Repeat("cd", 32)} {
		if err := validateRuntimeCodeHash(observed, want); err == nil {
			t.Fatalf("invalid or drifting runtime hash %q was accepted", observed)
		}
	}
}

func TestFinalizedLogProbeUsesOneExactBoundedBlock(t *testing.T) {
	probe := finalizedLogProbe(7_826_184)
	if probe["fromBlock"] != "0x776b08" || probe["toBlock"] != "0x776b08" {
		t.Fatalf("log probe is not one exact finalized block: %+v", probe)
	}
	for _, value := range probe {
		if value == "latest" || value == "earliest" || value == "pending" {
			t.Fatalf("log probe contains symbolic range boundary: %+v", probe)
		}
	}
}

func TestSubstratePhysicalIdentityMustBeIndependent(t *testing.T) {
	addresses := []string{
		"/ip4/127.0.0.1/tcp/30333/ws/p2p/private-peer/p2p/private-peer",
		"/ip4/172.18.0.3/tcp/30333/ws/p2p/private-peer",
	}
	peers := peerIDsFromListenAddresses(addresses)
	if len(peers) != 1 || peers[0] != "private-peer" {
		t.Fatalf("physical peer extraction = %v", peers)
	}
	if err := disjointSubstratePeers(peers, []string{"public-peer"}); err != nil {
		t.Fatal(err)
	}
	if err := disjointSubstratePeers(peers, []string{"private-peer"}); err == nil || !strings.Contains(err.Error(), "same physical") {
		t.Fatalf("same physical RPC backend was accepted: %v", err)
	}
}

func TestHTTPHealthRequiresExactSuccessWithoutRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/minio/health/live" {
			http.NotFound(w, request)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := checkHTTPHealth(context.Background(), server.URL+"/minio/health/live"); err != nil {
		t.Fatal(err)
	}
	redirect := httptest.NewServer(http.RedirectHandler(server.URL+"/minio/health/live", http.StatusFound))
	defer redirect.Close()
	if err := checkHTTPHealth(context.Background(), redirect.URL); err == nil || !strings.Contains(err.Error(), "redirected") {
		t.Fatalf("redirecting artifact health endpoint was accepted: %v", err)
	}
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failure.Close()
	if err := checkHTTPHealth(context.Background(), failure.URL); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("unhealthy artifact endpoint was accepted: %v", err)
	}
}

func TestDockerDaemonCheckRejectsUnavailableServer(t *testing.T) {
	if _, err := dockerServerVersion(context.Background(), dockerCLI{Executable: "/bin/false"}); err == nil {
		t.Fatal("unavailable Docker daemon was accepted")
	}
}

func TestApprovedSetupFactsUseOnlyTheRemainingBudget(t *testing.T) {
	plan := &SetupPlan{LiveFacts: *testSetupFacts(), RegistrationBurnLimitRao: testSetupFacts().BurnRao + 10, Limits: Spend{TAORao: 100, AlphaRao: 100}}
	current := *testSetupFacts()
	current.WalletFreeTAORao = 20
	current.AlphaAvailableRao = 30
	if err := validateApprovedSetupFacts(plan, &current, Spend{TAORao: 20, AlphaRao: 30}); err != nil {
		t.Fatal(err)
	}
	if err := validateApprovedSetupFacts(plan, &current, Spend{TAORao: 21, AlphaRao: 30}); err == nil || !strings.Contains(err.Error(), "remaining") {
		t.Fatalf("insufficient remaining TAO was accepted: %v", err)
	}
	current = *testSetupFacts()
	current.BurnRao++
	if err := validateApprovedSetupFacts(plan, &current, Spend{Registrations: 1}); err != nil {
		t.Fatalf("moving burn below the reviewed limit was rejected: %v", err)
	}
	current.BurnRao = plan.RegistrationBurnLimitRao + 1
	if err := validateApprovedSetupFacts(plan, &current, Spend{Registrations: 1}); err == nil || !strings.Contains(err.Error(), "capped") {
		t.Fatalf("burn above the reviewed limit was accepted: %v", err)
	}
	if err := validateApprovedSetupFacts(plan, &current, Spend{}); err != nil {
		t.Fatalf("burn blocked a resume after every registration was verified: %v", err)
	}
}

func TestSubstrateReadinessAcceptsLiveCanonicalPeer(t *testing.T) {
	checkpointHash := gsrpcTypes.Hash{1}
	observation := substrateReadinessObservation{
		Health:               substrateHealth{Peers: 2, ShouldHavePeers: true},
		OperationalFinalized: 100, PublicFinalized: 102, Checkpoint: 100,
		OperationalCheckpointHash: checkpointHash, PublicCheckpointHash: checkpointHash,
	}
	if err := validateSubstrateReadiness(observation, 3); err != nil {
		t.Fatal(err)
	}
}

func TestSubnetActivationRequiresFinalizedEmissionAfterDelay(t *testing.T) {
	active := SubnetActivationState{
		SubtokenEnabled: true, NetworkRegisteredAt: 100, StartCallDelay: 20,
		FirstEmissionBlockNumber: 120, FinalizedBlock: 150,
	}
	if err := validateSubnetActivation(active); err != nil {
		t.Fatalf("valid activation rejected: %v", err)
	}
	for name, mutate := range map[string]func(*SubnetActivationState){
		"disabled":         func(state *SubnetActivationState) { state.SubtokenEnabled = false },
		"missing emission": func(state *SubnetActivationState) { state.FirstEmissionBlockNumber = 0 },
		"before delay":     func(state *SubnetActivationState) { state.FirstEmissionBlockNumber = 119 },
		"not finalized":    func(state *SubnetActivationState) { state.FirstEmissionBlockNumber = 151 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := active
			mutate(&changed)
			if err := validateSubnetActivation(changed); err == nil {
				t.Fatalf("invalid activation accepted: %+v", changed)
			}
		})
	}
}

func TestSubstrateReadinessRejectsPeerlessStaleNode(t *testing.T) {
	checkpointHash := gsrpcTypes.Hash{1}
	observation := substrateReadinessObservation{
		Health:               substrateHealth{ShouldHavePeers: true},
		OperationalFinalized: 100, PublicFinalized: 104, Checkpoint: 100,
		OperationalCheckpointHash: checkpointHash, PublicCheckpointHash: checkpointHash,
	}
	err := validateSubstrateReadiness(observation, 3)
	if err == nil || !strings.Contains(err.Error(), "zero connected peers") || !strings.Contains(err.Error(), "lags public by 4 blocks") {
		t.Fatalf("peerless stale node was accepted or incompletely diagnosed: %v", err)
	}
}

func TestSubstrateReadinessRejectsSyncingFork(t *testing.T) {
	observation := substrateReadinessObservation{
		Health:               substrateHealth{Peers: 2, IsSyncing: true, ShouldHavePeers: true},
		OperationalFinalized: 100, PublicFinalized: 100, Checkpoint: 100,
		OperationalCheckpointHash: gsrpcTypes.Hash{1}, PublicCheckpointHash: gsrpcTypes.Hash{2},
	}
	err := validateSubstrateReadiness(observation, 3)
	if err == nil || !strings.Contains(err.Error(), "still syncing") || !strings.Contains(err.Error(), "canonical hashes disagree") {
		t.Fatalf("syncing fork was accepted or incompletely diagnosed: %v", err)
	}
}

func TestDoctorFailurePreservesObservationDetail(t *testing.T) {
	report := DoctorReport{}
	report.add("rpc/substrate-operational-readiness", true, errors.New("node is still syncing"), "peers=2 operational_finalized=100 public_finalized=200")
	if len(report.Checks) != 1 || report.Checks[0].OK || !strings.Contains(report.Checks[0].Detail, "peers=2") || !strings.Contains(report.Checks[0].Detail, "node is still syncing") {
		t.Fatalf("failed check lost its observation detail: %+v", report.Checks)
	}
}

func TestDoctorHostPlatformGateFailsClosed(t *testing.T) {
	if err := validateHostPlatform("linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	for _, platform := range [][2]string{{"linux", "arm64"}, {"darwin", "amd64"}, {"windows", "amd64"}} {
		if err := validateHostPlatform(platform[0], platform[1]); err == nil {
			t.Errorf("unsupported platform %s/%s was accepted", platform[0], platform[1])
		}
	}
}

func TestBlobConfigRequiresApplicationCredentialsAndExactBucket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "minio.yml")
	valid := []byte("authority: minio:23900\ntls: false\nbucket: blob\naccess_key: application\nsecret_key: private\n")
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateBlobConfig(path); err != nil {
		t.Fatal(err)
	}
	missingCredentials := []byte("# access_key: comment-only\nauthority: minio:23900\nbucket: blob\n")
	if err := os.WriteFile(path, missingCredentials, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateBlobConfig(path); err == nil {
		t.Fatal("comment-only MinIO credentials were accepted")
	}
	wrongBucket := []byte("authority: minio:23900\nbucket: other\naccess_key: application\nsecret_key: private\n")
	if err := os.WriteFile(path, wrongBucket, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateBlobConfig(path); err == nil {
		t.Fatal("wrong MinIO bucket was accepted")
	}
}
