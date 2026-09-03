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
	"time"

	gsrpcTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	serverst "github.com/urnetwork/server/v2026/st"
)

// The diagnostic request must exercise the same three-address, one-thousand
// inclusive block shape as Server at ordinary and uint64-boundary heads.
func TestFinalizedEVMEventLogProbeMatchesProductionWindow(t *testing.T) {
	maxUint64 := ^uint64(0)
	cases := []struct {
		name     string
		head     uint64
		wantFrom uint64
		wantSize uint64
	}{
		{name: "genesis", head: 0, wantFrom: 0, wantSize: 1},
		{name: "first full window", head: 999, wantFrom: 0, wantSize: 1000},
		{name: "bounded window", head: 1000, wantFrom: 1, wantSize: 1000},
		{name: "maximum head", head: maxUint64, wantFrom: maxUint64 - 999, wantSize: 1000},
	}
	for _, c := range cases {
		probe := finalizedEVMEventLogProbe(c.head)
		if probe.FromBlock == nil || probe.ToBlock == nil {
			t.Errorf("%s probe omitted a block boundary", c.name)
			continue
		}
		fromBlock, toBlock := probe.FromBlock.Uint64(), probe.ToBlock.Uint64()
		if fromBlock != c.wantFrom || toBlock != c.head || toBlock-fromBlock+1 != c.wantSize {
			t.Errorf("%s range=%d..%d size=%d, want %d..%d size=%d", c.name, fromBlock, toBlock, toBlock-fromBlock+1, c.wantFrom, c.head, c.wantSize)
		}
		if len(probe.Addresses) != 3 || c.wantSize > serverst.EventLogBlockRange {
			t.Errorf("%s addresses=%d size=%d, want three and at most %d", c.name, len(probe.Addresses), c.wantSize, serverst.EventLogBlockRange)
		}
	}
}

func TestReleaseRequiredToolsIncludeCapabilityInstallerAndNoninteractivePrivilegeBoundary(t *testing.T) {
	root := strings.Join(releaseRequiredTools(0), ",")
	if root != "go,git,docker,setcap,getcap" {
		t.Fatalf("root release tools = %q", root)
	}
	nonroot := strings.Join(releaseRequiredTools(1_000), ",")
	if nonroot != "go,git,docker,setcap,getcap,sudo" {
		t.Fatalf("nonroot release tools = %q", nonroot)
	}
}

func TestReleaseUDPBufferLimitsRequireBothQuicDirections(t *testing.T) {
	readLimits := func(values map[string]string) (releaseUDPBufferLimits, error) {
		return readReleaseUDPBufferLimits(func(path string) ([]byte, error) {
			value, ok := values[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return []byte(value), nil
		})
	}
	valid := map[string]string{
		"/proc/sys/net/core/rmem_max": "7340032\n",
		"/proc/sys/net/core/wmem_max": "16777216\n",
	}
	limits, err := readLimits(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseUDPBufferLimits(limits); err != nil {
		t.Fatalf("exact QUIC receive floor was rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{name: "receive below floor", values: map[string]string{"/proc/sys/net/core/rmem_max": "7340031", "/proc/sys/net/core/wmem_max": "16777216"}, want: "rmem_max"},
		{name: "send below floor", values: map[string]string{"/proc/sys/net/core/rmem_max": "16777216", "/proc/sys/net/core/wmem_max": "7340031"}, want: "wmem_max"},
		{name: "malformed receive", values: map[string]string{"/proc/sys/net/core/rmem_max": "7 MiB", "/proc/sys/net/core/wmem_max": "16777216"}, want: "parse"},
		{name: "missing send", values: map[string]string{"/proc/sys/net/core/rmem_max": "16777216"}, want: "send buffer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits, readErr := readLimits(test.values)
			if readErr == nil {
				readErr = validateReleaseUDPBufferLimits(limits)
			}
			if readErr == nil || !strings.Contains(readErr.Error(), test.want) {
				t.Fatalf("adjacent invalid UDP limits error=%v", readErr)
			}
		})
	}
}

func TestDoctorPlanBudgetForStateUsesJournaledProgressAcrossReleaseDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := doctorPlanBudgetForState(cfg, t.TempDir())
	if err != nil || fresh != nil {
		t.Fatalf("fresh doctor budget=%+v error=%v", fresh, err)
	}
	registration := actionByID(t, plan, "churn.register.1")
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	postconditionPath, err := postconditionRelativePath(plan.PlanHash, registration.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: plan.PlanHash,
		ActionID: registration.ID, IntentHash: registration.IntentHash, Stage: StageVerified,
		PostconditionHash: "0xpostcondition", PostconditionPath: postconditionPath,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	driftedRelease := *cfg.Release
	driftedRelease.Release = "1.0-runtime-drift"
	cfg.Release = &driftedRelease
	approved, err := doctorPlanBudgetForState(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if approved == nil || approved.Plan.PlanHash != plan.PlanHash || approved.Remaining.Registrations != plan.MaximumSpend.Registrations-1 {
		t.Fatalf("partial doctor budget=%+v", approved)
	}
	journalPath := filepath.Join(stateDir, "journal.jsonl")
	tampered, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered = []byte(strings.Replace(string(tampered), `"stage":"postcondition_verified"`, `"stage":"failed"`, 1))
	if err := os.WriteFile(journalPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := doctorPlanBudgetForState(cfg, stateDir); err == nil {
		t.Fatal("doctor accepted a tampered partial-progress journal")
	}
}

func TestDoctorDeploymentLineageRejectsExistingTopologyWithWrongStateDir(t *testing.T) {
	cfg := testResolvedConfig(t)
	wrongStateDir := filepath.Join(t.TempDir(), "unrelated-run")
	approved, err := doctorPlanBudgetForState(cfg, wrongStateDir)
	if err != nil || approved != nil {
		t.Fatalf("wrong state directory approval=%+v error=%v", approved, err)
	}
	err = validateDoctorDeploymentLineage(approved != nil, freshPlanningBootstrapUIDCount+1)
	if err == nil || !strings.Contains(err.Error(), "existing deployment requires the authoritative state-dir or authenticated import") {
		t.Fatalf("existing deployment without authoritative state was accepted: %v", err)
	}
}

func TestDoctorDeploymentLineageAllowsPristineTopologyWithEmptyStateDir(t *testing.T) {
	cfg := testResolvedConfig(t)
	approved, err := doctorPlanBudgetForState(cfg, t.TempDir())
	if err != nil || approved != nil {
		t.Fatalf("empty state directory approval=%+v error=%v", approved, err)
	}
	if err := validateDoctorDeploymentLineage(approved != nil, freshPlanningBootstrapUIDCount); err != nil {
		t.Fatalf("pristine topology with empty state directory was rejected: %v", err)
	}
}

func TestApprovedDoctorFactsAcceptExactPartialPrefixAndRejectAdjacentDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	approved := &doctorPlanBudget{Plan: plan, Remaining: plan.MaximumSpend, StateDir: t.TempDir()}
	current := partialRevisionFacts(t, cfg, cfg.Config.Topology.ChurnFloorUIDs)
	if err := validateApprovedDoctorFacts(cfg, approved, current); err != nil {
		t.Fatalf("journaled partial registration prefix was rejected: %v", err)
	}
	drifted := *current
	drifted.ExistingUIDs = append([]ExistingUIDFact(nil), current.ExistingUIDs...)
	drifted.ExistingUIDs[len(drifted.ExistingUIDs)-1].Hotkey = "0x" + strings.Repeat("ab", 32)
	if err := validateApprovedDoctorFacts(cfg, approved, &drifted); err == nil || !strings.Contains(err.Error(), "expected registration prefix") {
		t.Fatalf("adjacent unapproved UID drift was accepted: %v", err)
	}
}

// A state-version upgrade changes storage encoding without necessarily moving
// spec or transaction versions, so it is an independent launch boundary.
func TestDoctorRejectsFinalizedRuntimeStateVersionDrift(t *testing.T) {
	valid := doctorFinalizedRuntimeVersion{
		SpecName: "node-subtensor", SpecVersion: 453, TransactionVersion: 1, StateVersion: 1,
	}
	if err := validateFinalizedRuntimeVersion(valid, 453, 1, 1); err != nil {
		t.Fatalf("reviewed finalized runtime was rejected: %v", err)
	}
	drifted := valid
	drifted.StateVersion = 2
	if err := validateFinalizedRuntimeVersion(drifted, 453, 1, 1); err == nil || !strings.Contains(err.Error(), "state version") {
		t.Fatalf("finalized state-version drift was accepted: %v", err)
	}
}

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
	if err := validateExecutionRPCConfiguration(cfg); err != nil {
		t.Fatalf("public override could not enter its explicitly non-independent execution mode: %v", err)
	}
}

func TestSameRPCEndpointNormalizesOnlyEquivalentAuthorities(t *testing.T) {
	if !sameRPCEndpoint(" WSS://EXAMPLE.test:443 ", "wss://example.test:443/") {
		t.Fatal("equivalent RPC URLs were not recognized")
	}
	for _, pair := range [][2]string{
		{"wss://example.test/a", "wss://example.test/b"},
		{"wss://example.test", "https://example.test"},
		{"wss://user@example.test", "wss://example.test"},
		{"wss://example.test?q=1", "wss://example.test?q=2"},
		{"", ""},
	} {
		if sameRPCEndpoint(pair[0], pair[1]) {
			t.Fatalf("distinct or invalid RPC URLs were aliased: %q %q", pair[0], pair[1])
		}
	}
}

func TestSameEndpointCheckAliasesPreserveFailureAndExcludeUnmappedProbes(t *testing.T) {
	report := DoctorReport{Checks: []Check{{Name: "before", OK: true, Hard: true}}}
	start := len(report.Checks)
	report.Checks = append(report.Checks,
		Check{Name: "rpc/substrate-operational", OK: false, Hard: true, Detail: "reset"},
		Check{Name: "rpc/operational-eth_getLogs", OK: true, Hard: true, Detail: "bounded"},
	)
	aliasSameEndpointChecks(&report, start, map[string]string{"rpc/substrate-operational": "rpc/substrate-public"})
	if len(report.Checks) != 4 {
		t.Fatalf("checks=%+v", report.Checks)
	}
	alias := report.Checks[3]
	if alias.Name != "rpc/substrate-public" || alias.OK || !alias.Hard || !strings.Contains(alias.Detail, "same_endpoint_observation=true") {
		t.Fatalf("alias did not retain source result exactly: %+v", alias)
	}
	for _, check := range report.Checks[3:] {
		if check.Name == "rpc/public-eth_getLogs" {
			t.Fatalf("operational-only probe was aliased: %+v", check)
		}
	}
}

func TestCountMissingStorageKeysAtRequiresCompleteFinalizedBatch(t *testing.T) {
	block := gsrpcTypes.NewHash([]byte{9})
	keys := []gsrpcTypes.StorageKey{{1}, {2}, {3}}
	changes := []gsrpcTypes.StorageChangeSet{{Block: block, Changes: []gsrpcTypes.KeyValueOption{
		{StorageKey: keys[0], HasStorageData: true, StorageData: gsrpcTypes.StorageDataRaw{0}},
		{StorageKey: keys[1], HasStorageData: false},
		{StorageKey: keys[2], HasStorageData: true, StorageData: gsrpcTypes.StorageDataRaw{2}},
	}}}
	missing, err := countMissingStorageKeysAt(keys, changes, block)
	if err != nil || missing != 1 {
		t.Fatalf("missing=%d err=%v", missing, err)
	}

	tests := []struct {
		name    string
		keys    []gsrpcTypes.StorageKey
		changes []gsrpcTypes.StorageChangeSet
	}{
		{name: "empty request", keys: nil, changes: changes},
		{name: "duplicate request", keys: []gsrpcTypes.StorageKey{{1}, {1}}, changes: changes},
		{name: "no change set", keys: keys},
		{name: "wrong block", keys: keys, changes: []gsrpcTypes.StorageChangeSet{{Block: gsrpcTypes.NewHash([]byte{8}), Changes: changes[0].Changes}}},
		{name: "missing response key", keys: keys, changes: []gsrpcTypes.StorageChangeSet{{Block: block, Changes: changes[0].Changes[:2]}}},
		{name: "duplicate response key", keys: keys, changes: []gsrpcTypes.StorageChangeSet{{Block: block, Changes: []gsrpcTypes.KeyValueOption{changes[0].Changes[0], changes[0].Changes[0], changes[0].Changes[2]}}}},
		{name: "unrequested response key", keys: keys, changes: []gsrpcTypes.StorageChangeSet{{Block: block, Changes: []gsrpcTypes.KeyValueOption{changes[0].Changes[0], changes[0].Changes[1], {StorageKey: gsrpcTypes.StorageKey{4}}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := countMissingStorageKeysAt(test.keys, test.changes, block); err == nil {
				t.Fatal("malformed storage batch was accepted")
			}
		})
	}
}

func TestExecutionRPCConfigurationKeepsPrivateIndependenceHard(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://public-substrate.example:443"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://public-evm.example"
	cfg.Public.Chain.SubstratePublicReadEndpoint = cfg.OperationalSubstrate
	if err := validateExecutionRPCConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("private execution accepted a shared Substrate observer: %v", err)
	}

	cfg = testResolvedConfig(t)
	cfg.Config.LaunchInputs.PublicSubstrateRPCOverride = "wss://configured.example:443"
	cfg.Config.LaunchInputs.PublicEVMRPCOverride = "https://configured.example"
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.OperationalSubstrate = "wss://tampered.example:443"
	cfg.OperationalEVM = "https://configured.example"
	if err := validateExecutionRPCConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered public operational route was accepted: %v", err)
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
	plan := &SetupPlan{Schema: "urnetwork-sim-plan-v1", LiveFacts: *testSetupFacts(), RegistrationBurnLimitRao: testSetupFacts().BurnRao + 10, Limits: Spend{TAORao: 100, AlphaRao: 100}}
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
	current.ExistentialDepositRao++
	if err := validateApprovedSetupFacts(plan, &current, Spend{}); err == nil || !strings.Contains(err.Error(), "existential deposit") {
		t.Fatalf("runtime existential-deposit drift was accepted: %v", err)
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

func TestApprovedSetupFactsRejectsSourceRemainderAndTransferabilityLoss(t *testing.T) {
	plan := &SetupPlan{
		Schema: "urnetwork-sim-plan-v5", LiveFacts: *testSetupFacts(),
		RegistrationBurnLimitRao:     testSetupFacts().BurnRao + 10,
		MinimumSourceRemainingRao:    2_000_000_000_000,
		BootstrapBurnHalfLifeBlocks:  1,
		ProductionBurnHalfLifeBlocks: 360,
		Limits:                       Spend{TAORao: 100_000_000_000, AlphaRao: 20_000_000_000_000},
	}
	current := *testSetupFacts()
	current.AlphaAvailableRao = plan.MinimumSourceRemainingRao + 9
	if err := validateApprovedSetupFacts(plan, &current, Spend{AlphaRao: 10}); err == nil || !strings.Contains(err.Error(), "minimum remainder") {
		t.Fatalf("source remainder loss was accepted: %v", err)
	}
	current = *testSetupFacts()
	current.AlphaTransferableRao = 9
	if err := validateApprovedSetupFacts(plan, &current, Spend{AlphaRao: 10}); err == nil || !strings.Contains(err.Error(), "transferable") {
		t.Fatalf("lock/collateral capacity loss was accepted: %v", err)
	}
}

func TestApprovedSetupFactsRebindPreV5AlphaMinimumOnlyThroughRevision(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	plan.Schema = "urnetwork-sim-plan-v4"
	plan.AlphaTransferMarginBPS = 0
	plan.MinimumSourceRemainingRao = 0
	plan.LiveFacts.InitialMinStakeRao = 0
	current := *testSetupFacts()
	if err := validateApprovedSetupFacts(plan, &current, plan.MaximumSpend); err != nil {
		t.Fatalf("pre-v5 ancestor could not reach revision: %v", err)
	}
	current.InitialMinStakeRao = 0
	if err := validateApprovedSetupFacts(plan, &current, plan.MaximumSpend); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing live transfer minimum was accepted: %v", err)
	}
	plan.Schema = "urnetwork-sim-plan-v5"
	current.InitialMinStakeRao = testSetupFacts().InitialMinStakeRao
	if err := validateApprovedSetupFacts(plan, &current, plan.MaximumSpend); err == nil || !strings.Contains(err.Error(), "changed from approved") {
		t.Fatalf("v5 plan without an approved transfer minimum was accepted: %v", err)
	}
}

func TestApprovedSetupFactsV8BindsDefaultMinTransfer(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	approved := *testSetupFacts()
	plan, err := buildPlan(cfg, &approved, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	current := approved
	if err := validateApprovedSetupFacts(plan, &current, Spend{}); err != nil {
		t.Fatalf("unchanged runtime DefaultMinTransfer was rejected: %v", err)
	}
	current.DefaultMinTransferRao++
	if err := validateApprovedSetupFacts(plan, &current, Spend{}); err == nil || !strings.Contains(err.Error(), "DefaultMinTransfer changed") {
		t.Fatalf("runtime DefaultMinTransfer drift was accepted: %v", err)
	}
	current = approved
	current.DefaultMinTransferRao = 0
	if err := validateApprovedSetupFacts(plan, &current, Spend{}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing runtime DefaultMinTransfer was accepted: %v", err)
	}
}

func TestApprovedSetupFactsBindRegistrationEconomicsAcrossLifecycle(t *testing.T) {
	approved := *testSetupFacts()
	plan := &SetupPlan{
		Schema: "urnetwork-sim-plan-v2", LiveFacts: approved,
		RegistrationBurnLimitRao: 1_000_000, NativeTransactionFeeLimitRao: 3_000_000,
		BootstrapBurnHalfLifeBlocks: 1, ProductionBurnHalfLifeBlocks: 360,
	}
	current := approved
	current.BurnHalfLifeBlocks = 1
	if err := validateApprovedSetupFacts(plan, &current, Spend{Registrations: 1}); err != nil {
		t.Fatalf("approved bootstrap half-life was rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*SetupFacts)
		want   string
	}{
		{name: "minimum burn drift", mutate: func(facts *SetupFacts) { facts.MinBurnRao++ }, want: "registration economics changed"},
		{name: "maximum burn drift", mutate: func(facts *SetupFacts) { facts.MaxBurnRao-- }, want: "registration economics changed"},
		{name: "multiplier drift", mutate: func(facts *SetupFacts) { facts.BurnIncreaseMultQ64 = "18446744073709551616" }, want: "registration economics changed"},
		{name: "half-life outside lifecycle", mutate: func(facts *SetupFacts) { facts.BurnHalfLifeBlocks = 17 }, want: "outside the approved lifecycle"},
	}
	for _, test := range tests {
		current := approved
		test.mutate(&current)
		if err := validateApprovedSetupFacts(plan, &current, Spend{Registrations: 1}); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error=%v, want substring %q", test.name, err, test.want)
		}
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
