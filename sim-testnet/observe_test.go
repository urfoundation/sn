package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestFinalOperatorManifestOriginsResolveIndependentSignedPayloads(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	identities, err := json.Marshal(roles.Public())
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		EvidenceTransportProfile: publicEvidenceTransportHTTPS, Identities: identities, Topology: cfg.Config.Topology,
		Operators: []PublicOperator{
			{NoID: 1, APIURL: "https://operator-1.example"},
			{NoID: 2, APIURL: "https://operator-2.example"},
		},
	}
	objects := map[string][]byte{}
	origins := make([]FinalOperatorEvidenceOrigin, 2)
	for operator := 1; operator <= 2; operator++ {
		envelope, err := signEvidence(cfg, "deployment-manifest", "", public, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		host := fmt.Sprintf("operator-%d.example", operator)
		objects[host] = encoded
		origins[operator-1] = FinalOperatorEvidenceOrigin{OperatorNoID: operator, ManifestURI: "https://" + host + "/sn/evidence?hash=" + envelope.ContentHash}
	}
	calls := map[string]int{}
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls[request.URL.Host]++
		encoded := objects[request.URL.Host]
		if len(encoded) == 0 {
			return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(encoded)), Request: request}, nil
	})}}
	verification := &FinalPublicChainVerification{EvidenceURI: origins[0].ManifestURI, OperatorEvidenceOrigins: origins}
	if err := probe.authenticateFinalOperatorManifestOrigins(context.Background(), public, verification); err != nil {
		t.Fatalf("two independently signed deployment manifests: %v", err)
	}
	if calls["operator-1.example"] != 1 || calls["operator-2.example"] != 1 {
		t.Fatalf("independent origin reads = %+v, want one per operator", calls)
	}

	different := *public
	different.PlanHash = "0x" + strings.Repeat("ab", 32)
	tampered, err := signEvidence(cfg, "deployment-manifest", "", &different, roles.EVM["operator-2-artifact"])
	if err != nil {
		t.Fatal(err)
	}
	objects["operator-2.example"], err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	verification.OperatorEvidenceOrigins[1].ManifestURI = "https://operator-2.example/sn/evidence?hash=" + tampered.ContentHash
	if err := probe.authenticateFinalOperatorManifestOrigins(context.Background(), public, verification); err == nil {
		t.Fatal("operator 2 independently signed a different deployment graph and was accepted")
	}
}

func TestFinalOperatorSignedArchiveGraphRequiresByteIdenticalReplicas(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		Operators: []PublicOperator{
			{NoID: 1, APIURL: "https://operator-1.example"},
			{NoID: 2, APIURL: "https://operator-2.example"},
		},
	}
	envelope, err := signEvidence(cfg, campaignEvidenceManifestKind, "release-run", map[string]string{"schema": campaignEvidenceManifestSchema}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string][]byte{"operator-1.example": canonical, "operator-2.example": reencoded}
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(objects[request.URL.Host])), Request: request}, nil
	})}}
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, envelope.ContentHash, envelope.Kind, envelope.RunID, envelope.Signer.Hex()); err == nil || !strings.Contains(err.Error(), "differs between operator replicas") {
		t.Fatalf("semantically equal but byte-different signed archive replicas were accepted: %v", err)
	}
	objects["operator-2.example"] = canonical
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, envelope.ContentHash, envelope.Kind, envelope.RunID, envelope.Signer.Hex()); err != nil {
		t.Fatalf("byte-identical signed archive replicas were rejected: %v", err)
	}
}

// A stale state file from a prior supervisor generation cannot represent a
// healthy topology, even when every recorded child still says healthy.
func TestStatusRejectsStaleSupervisorGeneration(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks + 1,
		Processes: []ProcessState{{ID: "validator-1", PID: os.Getpid(), Healthy: true}},
	}
	path := filepath.Join(dir, "supervisor.state.json")
	if err := writePublicJSON(path, state); err != nil {
		t.Fatal(err)
	}
	status, err := Status(context.Background(), cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if status.Healthy || !strings.Contains(strings.Join(status.Warnings, "\n"), "start time changed") {
		t.Fatalf("stale supervisor status = %+v", status)
	}

	state.SupervisorStartTimeTicks = ticks
	if err := writePublicJSON(path, state); err != nil {
		t.Fatal(err)
	}
	status, err = Status(context.Background(), cfg, dir)
	if err != nil || !status.Healthy || len(status.Warnings) != 0 {
		t.Fatalf("live supervisor status = %+v, %v", status, err)
	}
}

func TestCallUint64AcceptsABIUint64AndBoundedUint256(t *testing.T) {
	for _, value := range []any{uint64(100_000), big.NewInt(100_000)} {
		got, err := callUint64(value, nil)
		if err != nil || got != 100_000 {
			t.Fatalf("callUint64(%T) = %d, %v", value, got, err)
		}
	}
}

func TestCallUint64RejectsMalformedAndPropagatesCallFailure(t *testing.T) {
	for _, value := range []any{nil, int64(1), big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 65)} {
		if _, err := callUint64(value, nil); err == nil {
			t.Fatalf("callUint64 accepted %T(%v)", value, value)
		}
	}
	want := errors.New("rpc failed")
	if _, err := callUint64(uint64(1), want); !errors.Is(err, want) {
		t.Fatalf("call failure = %v, want %v", err, want)
	}
}

func TestExtractPolicyHashFromABITuple(t *testing.T) {
	tuple := struct {
		PolicyHash [32]byte
		Epoch      uint64
	}{Epoch: 1}
	for i := range tuple.PolicyHash {
		tuple.PolicyHash[i] = byte(i)
	}
	got := extractFirstBytes32([]any{tuple})
	want := "0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	if got != want {
		t.Fatalf("policy hash = %s, want %s", got, want)
	}
}

func TestCampaignEvidenceOwnerUsesPinnedHistoricalEVMBlock(t *testing.T) {
	cfg := testResolvedConfig(t)
	owner := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	terminalHash := "0x" + strings.Repeat("ab", 32)
	finalizedHash := "0x" + strings.Repeat("cd", 32)
	requestedHistorical := false
	archiveMissing := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			JSONRPC string            `json:"jsonrpc"`
			ID      json.RawMessage   `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode historical EVM request: %v", err)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "eth_chainId":
			response["result"] = fmt.Sprintf("0x%x", cfg.ChainID)
		case "eth_getBlockByNumber":
			var selector string
			if len(request.Params) == 0 || json.Unmarshal(request.Params[0], &selector) != nil {
				t.Errorf("invalid historical block request: %+v", request.Params)
				return
			}
			switch selector {
			case "finalized":
				response["result"] = map[string]any{"number": "0x64", "hash": finalizedHash}
			case "0x2a":
				requestedHistorical = true
				if archiveMissing {
					response["result"] = nil
				} else {
					response["result"] = map[string]any{"number": "0x2a", "hash": terminalHash}
				}
			default:
				t.Errorf("owner lookup requested unpinned block %q", selector)
				return
			}
		case "eth_call":
			var selector string
			if len(request.Params) != 2 || json.Unmarshal(request.Params[1], &selector) != nil || selector != "0x2a" {
				t.Errorf("owner eth_call was not pinned to terminal block: %+v", request.Params)
				return
			}
			response["result"] = "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(owner.Hex()), "0x")
		default:
			t.Errorf("unexpected historical EVM method %q", request.Method)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode historical EVM response: %v", err)
		}
	}))
	defer server.Close()
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, EVMRPC: server.URL,
		Contracts: &ContractDeployment{DeploymentID: cfg.Config.Deployment.DeploymentID, CoordinatorProxy: common.HexToAddress("0x9999999999999999999999999999999999999999")},
	}
	probe := &liveScenarioProbe{cfg: cfg, client: server.Client()}
	result := &ScenarioResult{EndHead: ChainHead{Number: 42, Hash: terminalHash}}
	got, err := probe.resolveCampaignEvidenceOwner(context.Background(), public, result)
	if err != nil || got != owner || !requestedHistorical {
		t.Fatalf("historical campaign owner = %s, requested=%t, err=%v", got, requestedHistorical, err)
	}
	archiveMissing = true
	if _, err := probe.resolveCampaignEvidenceOwner(context.Background(), public, result); err == nil || !strings.Contains(err.Error(), "historical EVM archive") {
		t.Fatalf("missing historical block did not surface archive requirement: %v", err)
	}
}

func TestCampaignArtifactOriginsAreAllowlistedAndRedirectsRejected(t *testing.T) {
	public := &PublicDeploymentManifest{EvidenceTransportProfile: publicEvidenceTransportHTTPS, Operators: []PublicOperator{{NoID: 1, APIURL: "https://operator.example"}}}
	allowed, err := campaignArtifactAllowedOrigins(public, "https://manifest.example/sn/evidence?hash=sha256:"+strings.Repeat("11", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{"https://operator.example/sn/artifact/object", "https://manifest.example/object"} {
		if err := validateCampaignArtifactOrigin(uri, allowed); err != nil {
			t.Errorf("allowlisted artifact %q rejected: %v", uri, err)
		}
	}
	for _, uri := range []string{"https://attacker.example/object", "https://127.0.0.1/object", "https://localhost/object"} {
		if err := validateCampaignArtifactOrigin(uri, allowed); err == nil {
			t.Errorf("unauthorized artifact origin %q was accepted", uri)
		}
	}
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls++
		_, _ = w.Write([]byte("private target"))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	probe := &liveScenarioProbe{client: redirect.Client()}
	if _, status, err := probe.get(context.Background(), redirect.URL, 1024); err == nil || status != http.StatusFound {
		t.Fatalf("redirect response = HTTP %d, %v", status, err)
	}
	if targetCalls != 0 {
		t.Fatalf("campaign evidence client followed redirect to target %d times", targetCalls)
	}
	validDirectory := &PublicDeploymentManifest{Operators: []PublicOperator{
		{NoID: 1, APIURL: "https://operator-1.example", VerifyURL: "https://operator-1.example/verify", HistoryURL: "https://operator-1.example/sn/evidence/history"},
		{NoID: 2, APIURL: "https://operator-2.example/", VerifyURL: "https://operator-2.example/verify", HistoryURL: "https://operator-2.example/sn/evidence/history"},
	}}
	if err := validatePublicCampaignOperatorOrigins(validDirectory); err != nil {
		t.Fatalf("public HTTPS operator directory rejected: %v", err)
	}
	for name, mutate := range map[string]func(*PublicDeploymentManifest){
		"insecure API": func(value *PublicDeploymentManifest) { value.Operators[0].APIURL = "http://operator-1.example" },
		"cross-origin history": func(value *PublicDeploymentManifest) {
			value.Operators[0].HistoryURL = "https://attacker.example/sn/evidence/history"
		},
		"duplicate origin": func(value *PublicDeploymentManifest) {
			value.Operators[1].APIURL = value.Operators[0].APIURL
			value.Operators[1].HistoryURL = value.Operators[0].HistoryURL
		},
	} {
		copy := *validDirectory
		copy.Operators = append([]PublicOperator(nil), validDirectory.Operators...)
		mutate(&copy)
		if err := validatePublicCampaignOperatorOrigins(&copy); err == nil {
			t.Fatalf("public operator directory with %s was accepted", name)
		}
	}
}

func TestCampaignFinalSemanticEvidenceRequiresExactlyOneClosedObject(t *testing.T) {
	if finalSemanticRaceEnabled {
		t.Skip("the complete 1,000-miner semantic graph runs in the normal gate; bounded artifact-cache and publication races are tested separately")
	}
	cfg := testResolvedConfig(t)
	source, artifacts := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(semantic)
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := RenderFinalSemanticEvidenceMarkdown(semantic)
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", DeploymentID: semantic.DeploymentID, PlanHash: semantic.PlanHash,
		ConfigHash: semantic.ConfigHash, PolicyHash: semantic.PolicyHash, GenesisHash: semantic.GenesisHash, ChainID: semantic.ChainID, Netuid: semantic.Netuid,
		Topology: cfg.Config.Topology,
		Contracts: &ContractDeployment{
			CoordinatorProxy: common.HexToAddress(semantic.Deployment.CoordinatorProxy), CoordinatorImplementation: common.HexToAddress(semantic.Deployment.CoordinatorImplementation),
			SettlementVault: common.HexToAddress(semantic.Deployment.SettlementVault), ReserveSink: common.HexToAddress(semantic.Deployment.ReserveSink),
		},
	}
	window := semantic.Window
	bundle := &ScenarioEvidenceBundle{Result: &ScenarioResult{
		Name: semantic.Phase, RunID: semantic.RunID, EvidenceHash: semantic.ResultHash, DeploymentID: semantic.DeploymentID, ConfigHash: semantic.ConfigHash, PolicyHash: semantic.PolicyHash,
		ChainID: semantic.ChainID, GenesisHash: semantic.GenesisHash, Netuid: semantic.Netuid, EndHead: semantic.EVMTerminalHead, AcceptanceWindow: &window,
	}}
	verifyCalls := 0
	probe := &liveScenarioProbe{
		publicManifestURI: semantic.PublicVerification.EvidenceURI,
		finalSemanticVerify: func(ctx context.Context, _ *PublicDeploymentManifest, evidence *FinalSemanticEvidence, _ string) error {
			verifyCalls++
			return VerifyFinalSemanticEvidenceOnChain(ctx, evidence, &finalTestChainReader{evidence: evidence})
		},
	}
	owner := semantic.Deployment.GovernanceOwner
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, artifacts, nil); err == nil || !strings.Contains(err.Error(), "0 final semantic") {
		t.Fatalf("campaign without final semantic evidence = %v", err)
	}
	files := map[string][]byte{"final-semantic-evidence.json": encoded, finalSemanticMarkdownFilename: markdown}
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, files, artifacts, nil); err != nil {
		t.Fatalf("one closed final semantic object = %v", err)
	}
	if verifyCalls != 1 {
		t.Fatalf("public archive verifier calls = %d, want 1", verifyCalls)
	}
	defaultProbe := &liveScenarioProbe{publicManifestURI: semantic.PublicVerification.EvidenceURI}
	if _, err := defaultProbe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, files, artifacts, nil); err == nil || !strings.Contains(err.Error(), "public manifest hash") {
		t.Fatalf("semantic evidence detached from the authenticated public manifest was accepted: %v", err)
	}
	tamperedMarkdownFiles := map[string][]byte{"final-semantic-evidence.json": encoded, finalSemanticMarkdownFilename: []byte("# unbound report\n")}
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, tamperedMarkdownFiles, artifacts, nil); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unbound FINAL.md = %v", err)
	}
	missingArtifacts := make(map[string][]byte, len(artifacts)-1)
	missing := semantic.Validators[0].Cycles[0].IntentArtifact.URI
	for name, raw := range artifacts {
		if name != missing {
			missingArtifacts[name] = raw
		}
	}
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, files, missingArtifacts, nil); err == nil || !strings.Contains(err.Error(), "authenticated campaign graph") {
		t.Fatalf("semantic evidence with missing external artifact = %v", err)
	}
	duplicates := make(map[string][]byte, len(artifacts)+1)
	for name, raw := range artifacts {
		duplicates[name] = raw
	}
	duplicates["duplicate-final-semantic-evidence.json"] = encoded
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, files, duplicates, nil); err == nil || !strings.Contains(err.Error(), "2 final semantic") {
		t.Fatalf("campaign with duplicate final semantic evidence = %v", err)
	}
}

func publicPhaseLineageFixture(t *testing.T) (*authenticatedPublicScenarioCandidate, *authenticatedPublicScenarioCandidate) {
	t.Helper()
	releaseWindow := &ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 90, Hash: "0x" + strings.Repeat("10", 32)}, TerminalBlock: 120, EpochCount: 5}
	productionWindow := &ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 200, Hash: "0x" + strings.Repeat("20", 32)}, TerminalBlock: 260, EpochCount: 3}
	releaseResult := &ScenarioResult{
		Name: "release-1.0", RunID: "release-run", EvidenceHash: "0x" + strings.Repeat("31", 32), DeploymentID: "deployment",
		ConfigHash: "0x" + strings.Repeat("32", 32), PolicyHash: "0x" + strings.Repeat("33", 32), GenesisHash: "0x" + strings.Repeat("34", 32), ChainID: 945, Netuid: 521,
		CompletedAt: "2026-09-02T01:00:00Z", EndHead: ChainHead{Number: 120, Hash: "0x" + strings.Repeat("35", 32)}, AcceptanceWindow: releaseWindow,
	}
	productionResult := &ScenarioResult{
		Name: "production-soak", RunID: "production-run", EvidenceHash: "0x" + strings.Repeat("41", 32), DeploymentID: releaseResult.DeploymentID,
		ConfigHash: releaseResult.ConfigHash, PolicyHash: releaseResult.PolicyHash, GenesisHash: releaseResult.GenesisHash, ChainID: releaseResult.ChainID, Netuid: releaseResult.Netuid,
		StartedAt: "2026-09-02T02:00:00Z", EndHead: ChainHead{Number: 260, Hash: "0x" + strings.Repeat("42", 32)}, AcceptanceWindow: productionWindow,
	}
	releasePayload := scenarioCompletePayload{
		ResultHash: releaseResult.EvidenceHash, BundlePayloadHash: "sha256:" + strings.Repeat("51", 32), EvidenceManifestHash: "sha256:" + strings.Repeat("52", 32), Files: map[string]string{"result.json": "sha256:" + strings.Repeat("53", 32)},
	}
	releaseComplete := &ReleaseEvidenceEnvelope{RunID: releaseResult.RunID, ContentHash: "sha256:" + strings.Repeat("54", 32), Signature: "0xrelease", Payload: json.RawMessage(`{"result":"release"}`)}
	releaseCompleteBytes, err := json.Marshal(releaseComplete)
	if err != nil {
		t.Fatal(err)
	}
	releaseManifest := &ReleaseEvidenceEnvelope{RunID: releaseResult.RunID, ContentHash: releasePayload.EvidenceManifestHash, Signature: "0xmanifest", Payload: json.RawMessage(`{"manifest":"release"}`)}
	nativeTerminal := ChainHead{Number: 119, Hash: "0x" + strings.Repeat("55", 32)}
	releaseSemantic := &FinalSemanticEvidence{Phase: "release-1.0", NativeTerminalHead: nativeTerminal}
	priorBinding := &FinalPriorPhaseBinding{
		RunID: releaseResult.RunID, ResultHash: releaseResult.EvidenceHash, OwnerCompletionEnvelopeHash: releaseComplete.ContentHash, EvidenceManifestEnvelopeHash: releaseManifest.ContentHash, AcceptanceWindow: *releaseWindow,
		TerminalNativeHead: nativeTerminal, TerminalEVMHead: releaseResult.EndHead,
	}
	productionSemantic := &FinalSemanticEvidence{Phase: "production-soak", PriorPhase: priorBinding}
	prior := &authenticatedPublicScenarioCandidate{
		candidate:  &publicScenarioCandidate{bundle: &ScenarioEvidenceBundle{Result: releaseResult}, payload: releasePayload.BundlePayloadHash, signers: map[int]bool{1: true, 2: true}},
		completion: &publicScenarioCompletionCandidate{envelope: releaseComplete, payload: releasePayload, payloadBytes: releaseCompleteBytes, operators: map[int]bool{1: true, 2: true}},
		semantic:   &authenticatedCampaignSemantic{Evidence: releaseSemantic, EvidenceManifest: releaseManifest},
	}
	current := &authenticatedPublicScenarioCandidate{
		candidate:  &publicScenarioCandidate{bundle: &ScenarioEvidenceBundle{Result: productionResult}},
		completion: &publicScenarioCompletionCandidate{envelope: &ReleaseEvidenceEnvelope{RunID: productionResult.RunID}},
		semantic: &authenticatedCampaignSemantic{
			Evidence: productionSemantic, PriorCompletion: releaseComplete, PriorPayload: &releasePayload, PriorManifest: releaseManifest,
		},
	}
	return current, prior
}

func TestAuthenticatedPublicProductionRequiresExactReleaseLineage(t *testing.T) {
	current, prior := publicPhaseLineageFixture(t)
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err != nil {
		t.Fatalf("exact release lineage rejected: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	current.semantic.Evidence.PriorPhase.ResultHash = "0x" + strings.Repeat("ff", 32)
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("wrong release result hash accepted: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	current.semantic.Evidence.PriorPhase.OwnerCompletionEnvelopeHash = "sha256:" + strings.Repeat("ee", 32)
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("wrong release completion envelope hash accepted: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	current.semantic.Evidence.PriorPhase.EvidenceManifestEnvelopeHash = "sha256:" + strings.Repeat("ed", 32)
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("wrong release manifest envelope hash accepted: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	current.semantic.PriorManifest = &ReleaseEvidenceEnvelope{ContentHash: prior.semantic.EvidenceManifest.ContentHash, Signature: "0xsubstituted"}
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "objects do not match") {
		t.Fatalf("substituted release manifest accepted: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	current.semantic.Evidence.PriorPhase.TerminalNativeHead.Number++
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "checkpoints") {
		t.Fatalf("wrong release terminal head accepted: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	current.semantic.Evidence.PriorPhase = nil
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "lacks") {
		t.Fatalf("missing release binding accepted: %v", err)
	}
}

func TestPriorPhaseArtifactsRequireExactSignedEnvelopeHashes(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "release-run"
	resultHash := "0x" + strings.Repeat("71", 32)
	bundleHash := "sha256:" + strings.Repeat("72", 32)
	fileHash := "sha256:" + strings.Repeat("73", 32)
	fileEnvelopeHash := "sha256:" + strings.Repeat("74", 32)
	manifestPayload := campaignEvidenceManifestPayload{
		Schema: campaignEvidenceManifestSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		RunID: runID, ResultHash: resultHash, BundlePayloadHash: bundleHash,
		Files: []campaignEvidenceFileEntry{{Path: "result.json", ContentHash: fileHash, Size: 1, EnvelopeHash: fileEnvelopeHash}},
	}
	manifestEnvelope, err := signEvidence(cfg, campaignEvidenceManifestKind, runID, manifestPayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	completionPayload := scenarioCompletePayload{
		ResultHash: resultHash, BundlePayloadHash: bundleHash, EvidenceManifestHash: manifestEnvelope.ContentHash,
		Files: map[string]string{"result.json": fileHash},
	}
	completionEnvelope, err := signEvidence(cfg, "scenario-complete", runID, completionPayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	completionBytes, err := json.Marshal(completionEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.Marshal(manifestEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	semantic := &FinalSemanticEvidence{PriorPhase: &FinalPriorPhaseBinding{
		RunID: runID, ResultHash: resultHash,
		OwnerCompletionEnvelopeHash: completionEnvelope.ContentHash, EvidenceManifestEnvelopeHash: manifestEnvelope.ContentHash,
		Completion:       FinalArtifactLocator{URI: "prior/complete.json"},
		EvidenceManifest: FinalArtifactLocator{URI: "prior/manifest.json"},
	}}
	public := &PublicDeploymentManifest{DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid}
	files := map[string][]byte{semantic.PriorPhase.Completion.URI: completionBytes, semantic.PriorPhase.EvidenceManifest.URI: manifestBytes}
	if _, _, _, err := authenticatePriorPhaseArtifacts(public, semantic, files); err != nil {
		t.Fatalf("exact prior phase envelopes rejected: %v", err)
	}

	for name, mutate := range map[string]func(*FinalPriorPhaseBinding){
		"completion": func(binding *FinalPriorPhaseBinding) {
			binding.OwnerCompletionEnvelopeHash = "sha256:" + strings.Repeat("ee", 32)
		},
		"manifest": func(binding *FinalPriorPhaseBinding) {
			binding.EvidenceManifestEnvelopeHash = "sha256:" + strings.Repeat("ed", 32)
		},
	} {
		changed := *semantic.PriorPhase
		mutate(&changed)
		if _, _, _, err := authenticatePriorPhaseArtifacts(public, &FinalSemanticEvidence{PriorPhase: &changed}, files); err == nil {
			t.Fatalf("wrong prior %s envelope hash accepted", name)
		}
	}
	tampered := make(map[string][]byte, len(files))
	for name, raw := range files {
		tampered[name] = append([]byte(nil), raw...)
	}
	tampered[semantic.PriorPhase.Completion.URI][len(tampered[semantic.PriorPhase.Completion.URI])/2] ^= 1
	if _, _, _, err := authenticatePriorPhaseArtifacts(public, semantic, tampered); err == nil {
		t.Fatal("tampered prior owner completion envelope accepted")
	}
}

func TestAuthenticatedPublicCampaignSelectionFailsClosedOnAmbiguity(t *testing.T) {
	candidate := func(runID, payload, completion string, completed time.Time) *authenticatedPublicScenarioCandidate {
		return &authenticatedPublicScenarioCandidate{
			candidate: &publicScenarioCandidate{
				bundle:  &ScenarioEvidenceBundle{Result: &ScenarioResult{RunID: runID}},
				payload: payload,
				time:    completed,
			},
			completion: &publicScenarioCompletionCandidate{envelope: &ReleaseEvidenceEnvelope{ContentHash: completion, Signature: "0xsig"}},
		}
	}
	boundary := time.Unix(1_700_000_000, 0).UTC()
	first := candidate("run", "sha256:first", "sha256:completion-first", boundary)
	if selected, err := selectAuthenticatedPublicCampaign(nil, first, "run"); err != nil || selected != first {
		t.Fatalf("first exact campaign selection = %p, %v", selected, err)
	}
	for name, alternate := range map[string]*authenticatedPublicScenarioCandidate{
		"later signed bundle": candidate("run", "sha256:second", "sha256:completion-second", boundary.Add(time.Second)),
		"second completion":   candidate("run", "sha256:first", "sha256:completion-second", boundary),
	} {
		if _, err := selectAuthenticatedPublicCampaign(first, alternate, "run"); err == nil || !strings.Contains(err.Error(), "multiple authenticated") {
			t.Fatalf("%s accepted for an exact run: %v", name, err)
		}
	}
	equalBoundary := candidate("other-run", "sha256:other", "sha256:other-completion", boundary)
	if _, err := selectAuthenticatedPublicCampaign(first, equalBoundary, ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("duplicate latest completion boundary accepted: %v", err)
	}
}

func TestScenarioFinalSemanticSourceBindsRunResultAndPhase(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	window := ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 10, Hash: finalTestHex(10)}, TerminalBlock: 20, EpochCount: 1}
	terminal := ChainHead{Number: 20, Hash: finalTestHex(20)}
	result := &ScenarioResult{
		Name: "release-1.0", RunID: "release-run", EvidenceHash: finalTestHex(1),
		DeploymentID: cfg.Config.Deployment.DeploymentID, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash,
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		CampaignStartHead: ChainHead{Number: 5, Hash: finalTestHex(5)}, EndHead: terminal, AcceptanceWindow: &window,
	}
	source := FinalSemanticEvidence{
		Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash,
		DeploymentID: result.DeploymentID, PlanHash: finalTestHex(2), ConfigHash: result.ConfigHash, PolicyHash: result.PolicyHash,
		ChainID: result.ChainID, GenesisHash: result.GenesisHash, Netuid: result.Netuid,
		Window: window, EVMCampaignStartHead: result.CampaignStartHead, EVMTerminalHead: terminal,
		ExpectedOperators: cfg.Config.Topology.Operators, ExpectedValidators: cfg.Config.Topology.Validators, ExpectedMiners: cfg.Config.Topology.Miners,
		ExpectedCandidates: cfg.Config.Topology.HeadFleets + cfg.Config.Topology.ChallengerFleets, ExpectedHeadSlots: cfg.Config.Topology.HeadSlots,
		Deployment: FinalContractDeploymentEvidence{GovernanceOwner: roles.EVM["testnet-owner"].Address},
	}
	if err := validateScenarioFinalSemanticSource(cfg, roles, result, &source); err != nil {
		t.Fatalf("exact semantic campaign source rejected: %v", err)
	}
	for name, mutate := range map[string]func(*FinalSemanticEvidence){
		"phase":       func(value *FinalSemanticEvidence) { value.Phase = "production-soak" },
		"run id":      func(value *FinalSemanticEvidence) { value.RunID = "other-run" },
		"result hash": func(value *FinalSemanticEvidence) { value.ResultHash = finalTestHex(0xee) },
	} {
		changed := source
		mutate(&changed)
		if err := validateScenarioFinalSemanticSource(cfg, roles, result, &changed); err == nil || !strings.Contains(err.Error(), "does not bind") {
			t.Fatalf("wrong semantic source %s accepted: %v", name, err)
		}
	}
}

func TestPublicProductionPredecessorMustExistAtEveryOperator(t *testing.T) {
	current, prior := publicPhaseLineageFixture(t)
	encoded, err := json.Marshal(current.semantic.PriorCompletion)
	if err != nil {
		t.Fatal(err)
	}
	completionKey := bytesSHA256(encoded)
	candidateKey := prior.candidate.bundle.Result.RunID + "\x00" + prior.completion.payload.BundlePayloadHash
	candidates := map[string]*publicScenarioCandidate{candidateKey: prior.candidate}
	completions := map[string]*publicScenarioCompletionCandidate{completionKey: prior.completion}
	if _, _, err := findPublicCampaignPredecessor(current.semantic, candidates, completions, 2); err != nil {
		t.Fatalf("replicated predecessor rejected: %v", err)
	}

	delete(completions, completionKey)
	if _, _, err := findPublicCampaignPredecessor(current.semantic, candidates, completions, 2); err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("missing release completion history accepted: %v", err)
	}
	completions[completionKey] = prior.completion
	delete(prior.completion.operators, 2)
	if _, _, err := findPublicCampaignPredecessor(current.semantic, candidates, completions, 2); err == nil || !strings.Contains(err.Error(), "every operator") {
		t.Fatalf("partially replicated release completion accepted: %v", err)
	}
	prior.completion.operators[2] = true
	delete(prior.candidate.signers, 2)
	if _, _, err := findPublicCampaignPredecessor(current.semantic, candidates, completions, 2); err == nil || !strings.Contains(err.Error(), "every operator") {
		t.Fatalf("partially replicated release bundle accepted: %v", err)
	}
}

func TestJournalSummaryUsesLatestActionStage(t *testing.T) {
	entries := []JournalEntry{
		{ActionID: "a", Stage: StageIntent, EntryHash: "h1"},
		{ActionID: "b", Stage: StageVerified, EntryHash: "h2"},
		{ActionID: "a", Stage: StageVerified, EntryHash: "h3"},
	}
	summary := summarizeJournal(entries)
	if summary.Entries != 3 || summary.LastHash != "h3" || !summary.Actions["a"] || !summary.Actions["b"] || summary.LatestByStage[string(StageVerified)] != 2 {
		t.Fatalf("journal summary = %+v", summary)
	}
}

func TestDecodeHashRejectsZeroLengthAndMalformedValues(t *testing.T) {
	if _, err := decodeHash("0x" + strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "0x01", "0x" + strings.Repeat("zz", 32)} {
		if _, err := decodeHash(value); err == nil {
			t.Fatalf("malformed hash %q accepted", value)
		}
	}
}

func TestPublicScenarioBundleRequiresReplicatedOwnerCompletionCommit(t *testing.T) {
	if finalSemanticRaceEnabled {
		t.Skip("the complete 1,000-miner semantic graph runs in the normal gate; bounded replica, commit, and concurrent-publication races are tested separately")
	}
	cfg := testResolvedConfig(t)
	semanticSource, semanticArtifacts := finalSemanticFixture(t)
	cfg.Config.Deployment.DeploymentID = semanticSource.DeploymentID
	cfg.ConfigHash = semanticSource.ConfigHash
	cfg.PolicyHash = semanticSource.PolicyHash
	cfg.Public.Chain.GenesisHash = semanticSource.GenesisHash
	cfg.ChainID = semanticSource.ChainID
	cfg.Netuid = semanticSource.Netuid
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for label, role := range roles.Clients {
		role.ClientIDHex = strings.Repeat("01", 16)
		roles.Clients[label] = role
	}
	identities, err := json.Marshal(roles.Public())
	if err != nil {
		t.Fatal(err)
	}
	semanticSource.Deployment.GovernanceOwner = strings.ToLower(roles.EVM["testnet-owner"].Address)
	semanticWire, err := json.Marshal(semanticSource)
	if err != nil {
		t.Fatal(err)
	}
	semanticWire = bytes.ReplaceAll(semanticWire, []byte(`"artifacts/`), []byte(`"public/final-artifacts/`))
	if err := json.Unmarshal(semanticWire, &semanticSource); err != nil {
		t.Fatal(err)
	}
	renamedSemanticArtifacts := make(map[string][]byte, len(semanticArtifacts))
	for name, raw := range semanticArtifacts {
		renamedSemanticArtifacts["public/final-"+name] = raw
	}
	semanticArtifacts = renamedSemanticArtifacts
	window := semanticSource.Window
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: "committed-run", DeploymentID: cfg.Config.Deployment.DeploymentID,
		Name: "release-1.0", ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID,
		GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, CompletedAt: time.Now().UTC().Format(time.RFC3339Nano), Result: "pass",
		EndHead: semanticSource.EVMTerminalHead, AcceptanceWindow: &window, Assertions: []AssertionRecord{},
		Anomalies:   &ScenarioAnomalyLedger{Schema: "urnetwork-sim-anomaly-ledger-v1", Release: "1.0", RunID: "committed-run", Status: "clean", Entries: []ScenarioAnomaly{}},
		Adversaries: &AdversaryCampaignEvidence{Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", Actors: []AdversaryActorEvidence{}, Vectors: []AdversaryVectorEvidence{}},
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	semanticSource.Phase = result.Name
	semanticSource.RunID = result.RunID
	semanticSource.ResultHash = result.EvidenceHash
	semanticDraft, err := BuildFinalSemanticEvidence(semanticSource)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := SealFinalSemanticEvidenceOnChain(context.Background(), semanticDraft, &finalTestChainReader{evidence: semanticDraft})
	if err != nil {
		t.Fatal(err)
	}
	observation := &ScenarioObservation{Schema: "urnetwork-sim-scenario-observation-v1", ObservationHash: "observation"}
	analysis := &AnalysisReport{Schema: "urnetwork-sim-analysis-v1", Release: "1.0", DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ObservationHash: observation.ObservationHash}
	bundle := ScenarioEvidenceBundle{Schema: "urnetwork-sim-scenario-evidence-v1", Result: result, Observation: observation, Analysis: analysis}
	bundlePayload, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string][]byte{}
	bundleHashes := make([]string, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		envelope, err := signEvidence(cfg, "scenario-bundle", result.RunID, bundle, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(envelope)
		objects[envelope.ContentHash] = encoded
		bundleHashes[operator-1] = envelope.ContentHash
	}
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", result.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{
		"result.json": result, "assertions.json": assertionFile{Schema: "urnetwork-sim-assertions-v1", Assertions: result.Assertions},
		"anomalies.json": result.Anomalies, "adversaries.json": result.Adversaries, "analysis.json": analysis,
	} {
		if err := writePublicJSON(filepath.Join(runDir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	observationBytes, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, "observations.jsonl"), append(observationBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, "analysis.html"), []byte("<!doctype html><title>analysis</title>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, "junit.xml"), []byte("<?xml version=\"1.0\"?><testsuite></testsuite>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "final-semantic-evidence.json"), semantic); err != nil {
		t.Fatal(err)
	}
	semanticMarkdown, err := RenderFinalSemanticEvidenceMarkdown(semantic)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, finalSemanticMarkdownFilename), semanticMarkdown, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, raw := range semanticArtifacts {
		if err := atomicWrite(filepath.Join(stateDir, filepath.FromSlash(name)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	receiptPath := "receipts/external-receipt.json"
	nestedProofPath := "public/nested-proof.json"
	nestedProofBytes := []byte(`{"schema":"urnetwork-test-proof-v1","value":"verified"}` + "\n")
	if err := atomicWrite(filepath.Join(stateDir, filepath.FromSlash(nestedProofPath)), nestedProofBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	receiptBytes, err := json.Marshal(map[string]any{
		"schema": "urnetwork-test-receipt-v1", "status": "success",
		"proof": FinalArtifactLocator{Kind: "native-ownership", URI: nestedProofPath, ContentHash: bytesSHA256(nestedProofBytes), SizeBytes: uint64(len(nestedProofBytes))},
	})
	if err != nil {
		t.Fatal(err)
	}
	receiptBytes = append(receiptBytes, '\n')
	if err := atomicWrite(filepath.Join(stateDir, filepath.FromSlash(receiptPath)), receiptBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "artifact-references.json"), map[string]any{
		"schema":  "urnetwork-test-artifact-references-v1",
		"receipt": FinalArtifactLocator{Kind: "evm-receipt", URI: receiptPath, ContentHash: bytesSHA256(receiptBytes), SizeBytes: uint64(len(receiptBytes))},
	}); err != nil {
		t.Fatal(err)
	}
	fileHashes, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := prepareCampaignEvidenceArchive(cfg, roles, stateDir, result.RunID, result.EvidenceHash, bytesSHA256(bundlePayload), fileHashes)
	if err != nil {
		t.Fatal(err)
	}
	for _, envelope := range append(append([]*ReleaseEvidenceEnvelope(nil), archive.Files...), archive.Manifest) {
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		objects[envelope.ContentHash] = encoded
	}
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{
		ResultHash: result.EvidenceHash, BundlePayloadHash: bytesSHA256(bundlePayload), Files: fileHashes, EvidenceManifestHash: archive.Manifest.ContentHash,
	}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	completeBytes, _ := json.Marshal(complete)
	if len(completeBytes) == 0 {
		t.Fatal("owner completion envelope did not encode")
	}
	objects[complete.ContentHash] = completeBytes
	commitHashes := make([]string, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		commit, err := signEvidence(cfg, "scenario-complete-commit", result.RunID, complete, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(commit)
		objects[commit.ContentHash] = encoded
		commitHashes[operator-1] = commit.ContentHash
	}
	commitVisible := map[int]bool{}
	directOwnerVisible := true
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operator := 0
		if strings.HasPrefix(r.URL.Path, "/operator-1/") {
			operator = 1
		} else if strings.HasPrefix(r.URL.Path, "/operator-2/") {
			operator = 2
		}
		switch {
		case operator != 0 && strings.HasSuffix(r.URL.Path, "/sn/evidence/history"):
			var keys []map[string]string
			switch r.URL.Query().Get("kind") {
			case "scenario-bundle":
				keys = append(keys, map[string]string{"key": "history/" + strings.TrimPrefix(bundleHashes[operator-1], "sha256:") + ".json"})
			case "scenario-complete-commit":
				if directOwnerVisible {
					keys = append(keys, map[string]string{"key": "history/" + strings.TrimPrefix(complete.ContentHash, "sha256:") + ".json"})
				}
				if commitVisible[operator] {
					keys = append(keys, map[string]string{"key": "history/" + strings.TrimPrefix(commitHashes[operator-1], "sha256:") + ".json"})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": "urnetwork-release-evidence-history-v1", "objects": keys})
		case operator != 0 && strings.HasSuffix(r.URL.Path, "/sn/evidence"):
			encoded := objects[r.URL.Query().Get("hash")]
			if len(encoded) == 0 {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(encoded)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, serverPort, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	testTransport := server.Client().Transport.(*http.Transport).Clone()
	testTransport.TLSClientConfig = testTransport.TLSClientConfig.Clone()
	testTransport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // isolated httptest listener
	testTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	testClient := &http.Client{Transport: testTransport}
	t.Cleanup(testClient.CloseIdleConnections)
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash,
		Schema: "urnetwork-sim-public-deployment-v1", Netuid: cfg.Netuid, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, PlanHash: semantic.PlanHash,
		EvidenceTransportProfile: publicEvidenceTransportHTTPS,
		EVMRPC:                   "https://evm.example/rpc", SubstrateRPC: "wss://substrate.example/rpc", Identities: identities, Topology: cfg.Config.Topology,
		Contracts: &ContractDeployment{
			Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
			CoordinatorProxy: common.HexToAddress(semantic.Deployment.CoordinatorProxy), CoordinatorImplementation: common.HexToAddress(semantic.Deployment.CoordinatorImplementation),
			SettlementVault: common.HexToAddress(semantic.Deployment.SettlementVault), ReserveSink: common.HexToAddress(semantic.Deployment.ReserveSink),
		},
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		base := fmt.Sprintf("https://example.com:%s/operator-%d", serverPort, operator)
		public.Operators = append(public.Operators, PublicOperator{NoID: operator, APIURL: base, HistoryURL: base + "/sn/evidence/history"})
	}
	probe := &liveScenarioProbe{
		cfg: cfg, client: testClient, trustedEvidenceOwner: common.HexToAddress(roles.EVM["testnet-owner"].Address),
		publicManifestURI: semantic.PublicVerification.EvidenceURI,
		finalSemanticVerify: func(ctx context.Context, _ *PublicDeploymentManifest, evidence *FinalSemanticEvidence, _ string) error {
			return VerifyFinalSemanticEvidenceOnChain(ctx, evidence, &finalTestChainReader{evidence: evidence})
		},
	}
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("direct owner envelope bypassed the per-operator completion commit")
	}
	directOwnerVisible = false
	commitVisible[1] = true
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("completion commit present at only one operator was accepted")
	}
	commitVisible[2] = true
	wantOwner := probe.trustedEvidenceOwner
	probe.trustedEvidenceOwner = common.HexToAddress(roles.EVM["operator-1-artifact"].Address)
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("self-declared owner signer bypassed the finalized coordinator owner")
	}
	probe.trustedEvidenceOwner = wantOwner
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("syntactic pass without the approved scenario assertions was accepted")
	}
	probe.campaignResultVerify = func(*ResolvedConfig, *ScenarioResult, string) error { return nil }
	manifestBytes := objects[archive.Manifest.ContentHash]
	delete(objects, archive.Manifest.ContentHash)
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("completion with a missing campaign manifest was accepted")
	}
	objects[archive.Manifest.ContentHash] = manifestBytes
	resultEnvelopeHash := ""
	for _, entry := range mustCampaignManifest(t, archive.Manifest).Files {
		if entry.Path == "result.json" {
			resultEnvelopeHash = entry.EnvelopeHash
		}
	}
	if resultEnvelopeHash == "" {
		t.Fatal("campaign manifest has no result object")
	}
	resultEnvelopeBytes := objects[resultEnvelopeHash]
	delete(objects, resultEnvelopeHash)
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("syntactic hash for a nonexistent result object was accepted")
	}
	tamperedResultEnvelope := append([]byte(nil), resultEnvelopeBytes...)
	tamperedResultEnvelope[len(tamperedResultEnvelope)/2] ^= 1
	objects[resultEnvelopeHash] = tamperedResultEnvelope
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("tampered campaign evidence object was accepted")
	}
	objects[resultEnvelopeHash] = resultEnvelopeBytes
	referenceEnvelopeHash := ""
	referenceEntries := mustCampaignManifest(t, archive.Manifest).References
	if len(referenceEntries) < 2 {
		t.Fatalf("campaign manifest recursively archived %d references, want at least the receipt and its nested proof", len(referenceEntries))
	}
	nestedProofArchived := false
	for _, entry := range referenceEntries {
		if entry.Path == receiptPath {
			referenceEnvelopeHash = entry.EnvelopeHash
		}
		if entry.Path == nestedProofPath {
			nestedProofArchived = true
		}
	}
	if referenceEnvelopeHash == "" || !nestedProofArchived {
		t.Fatal("campaign manifest has no external receipt or recursively referenced proof object")
	}
	referenceEnvelopeBytes := objects[referenceEnvelopeHash]
	delete(objects, referenceEnvelopeHash)
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("completion with an unavailable referenced external receipt was accepted")
	}
	tamperedReferenceEnvelope := append([]byte(nil), referenceEnvelopeBytes...)
	tamperedReferenceEnvelope[len(tamperedReferenceEnvelope)/2] ^= 1
	objects[referenceEnvelopeHash] = tamperedReferenceEnvelope
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("completion with a tampered referenced external receipt was accepted")
	}
	objects[referenceEnvelopeHash] = referenceEnvelopeBytes
	got, err := probe.fetchLatestScenarioBundle(context.Background(), public)
	if err != nil || got.Result == nil || got.Result.RunID != result.RunID {
		t.Fatalf("replicated committed scenario bundle = %+v, %v", got, err)
	}
}

func mustCampaignManifest(t testing.TB, envelope *ReleaseEvidenceEnvelope) *campaignEvidenceManifestPayload {
	t.Helper()
	manifest, err := decodeCampaignEvidenceManifest(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
