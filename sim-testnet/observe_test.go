package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
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
	// Deployment manifests are published after provisioning assigns every
	// client ID. Complete that lifecycle before encoding the signer directory.
	clientLabels := make([]string, 0, len(roles.Clients))
	for label := range roles.Clients {
		clientLabels = append(clientLabels, label)
	}
	sort.Strings(clientLabels)
	for index, label := range clientLabels {
		role := roles.Clients[label]
		role.ClientIDHex = fmt.Sprintf("%032x", index+1)
		roles.Clients[label] = role
	}
	identities, err := json.Marshal(roles.Public())
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		RuntimeSpec: cfg.Public.Chain.ExpectedRuntimeSpec, TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: cfg.Public.Chain.ExpectedStateVersion,
		RuntimeCodeHash: cfg.Release.Runtime.CodeHash, RuntimeMetadataHash: cfg.Release.Runtime.MetadataHash,
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
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, envelope.ContentHash, envelope.Kind, envelope.RunID, envelope.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes); err == nil || !strings.Contains(err.Error(), "differs between operator replicas") {
		t.Fatalf("semantically equal but byte-different signed archive replicas were accepted: %v", err)
	}
	objects["operator-2.example"] = canonical
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, envelope.ContentHash, envelope.Kind, envelope.RunID, envelope.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes); err != nil {
		t.Fatalf("byte-identical signed archive replicas were rejected: %v", err)
	}
}

func TestCampaignFileEnvelopeReadLimitIsDerivedFromSignedSize(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const runID = "bounded-file-run"
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: runID, Scope: "run", Path: "tiny.json", ContentHash: bytesSHA256([]byte{'x'}), Size: 1, Data: []byte{'x'}}
	envelope, err := signEvidence(cfg, campaignEvidenceFileKind, runID, payload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := maximumCampaignFileEnvelopeBytes(payload.Size)
	if err != nil || limit >= maximumCampaignEvidenceEnvelopeBytes {
		t.Fatalf("tiny file envelope limit=%d error=%v", limit, err)
	}
	response := encoded
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(response)), Request: request}, nil
	})}}
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		Operators: []PublicOperator{{NoID: 1, APIURL: "https://operator.example"}},
	}
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, envelope.ContentHash, envelope.Kind, runID, envelope.Signer.Hex(), limit); err != nil {
		t.Fatalf("valid size-bound file envelope rejected: %v", err)
	}
	response = bytes.Repeat([]byte{'x'}, int(limit)+1)
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, envelope.ContentHash, envelope.Kind, runID, envelope.Signer.Hex(), limit); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized carrier for a one-byte file was accepted: %v", err)
	}
	if _, err := maximumCampaignFileEnvelopeBytes(maximumCampaignEvidenceRawFileBytes); err != nil {
		t.Fatalf("maximum valid raw file has no safe envelope bound: %v", err)
	}
	if _, err := maximumCampaignFileEnvelopeBytes(maximumCampaignEvidenceRawFileBytes + 1); err == nil {
		t.Fatal("oversized raw file received an envelope bound")
	}
}

func TestCampaignEvidenceAggregatePreflightPreventsFileFetch(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const runID = "aggregate-preflight-run"
	entries := make([]campaignEvidenceFileEntry, 9)
	files := make(map[string]string, len(entries))
	for index := range entries {
		name := fmt.Sprintf("file-%02d.json", index)
		entries[index] = campaignEvidenceFileEntry{
			Path: name, ContentHash: fmt.Sprintf("sha256:%064x", index+1), Size: maximumCampaignEvidenceRawFileBytes,
			EnvelopeHash: fmt.Sprintf("sha256:%064x", index+100),
		}
		files[name] = entries[index].ContentHash
	}
	manifestPayload := campaignEvidenceManifestPayload{
		Schema: campaignEvidenceManifestSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, RunID: runID, ResultHash: finalTestHex(73),
		BundlePayloadHash: "sha256:" + strings.Repeat("74", 32), Files: entries,
	}
	manifest, err := signEvidence(cfg, campaignEvidenceManifestKind, runID, manifestPayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	objectGets := 0
	probe := &liveScenarioProbe{cfg: cfg, client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		objectGets++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(manifestBytes)), Request: request}, nil
	})}}
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		Operators: []PublicOperator{{NoID: 1, APIURL: "https://operator.example"}},
	}
	complete := &ReleaseEvidenceEnvelope{RunID: runID}
	completion := scenarioCompletePayload{ResultHash: manifestPayload.ResultHash, BundlePayloadHash: manifestPayload.BundlePayloadHash, EvidenceManifestHash: manifest.ContentHash, Files: files}
	bundle := &ScenarioEvidenceBundle{Result: &ScenarioResult{RunID: runID}}
	if _, err := probe.verifyPublicCampaignEvidence(context.Background(), public, roles.EVM["testnet-owner"].Address, complete, completion, bundle); err == nil || !strings.Contains(err.Error(), "aggregate") {
		t.Fatalf("over-aggregate manifest reached file fetches: %v", err)
	}
	if objectGets != 1 {
		t.Fatalf("over-aggregate manifest caused %d object GETs, want manifest only", objectGets)
	}
}

func TestCampaignEvidenceCombinedAggregatePreflightPreventsSemanticFileFetch(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const runID = "semantic-aggregate-preflight-run"
	result := &ScenarioResult{Name: "release-1.0", RunID: runID, EvidenceHash: finalTestHex(75)}
	complete, err := signEvidence(cfg, "scenario-complete", runID, map[string]string{"schema": "complete"}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := signEvidence(cfg, campaignEvidenceManifestKind, runID, map[string]string{"schema": campaignEvidenceManifestSchema}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	payload := FinalSemanticSupplementPayload{
		Schema: finalSemanticSupplementSchema, Status: finalSemanticSupplementStatus, Phase: result.Name, RunID: runID,
		ResultHash: result.EvidenceHash, ScenarioCompleteHash: complete.ContentHash, ScenarioEvidenceManifestHash: manifest.ContentHash,
		CaptureStatusHash: finalTestHex(76), CollectedInputsHash: finalTestHex(77), SemanticEvidenceHash: finalTestHex(78), PublicTranscriptHash: finalTestHex(79),
		Files: []FinalSemanticSupplementFile{
			{Path: finalSemanticMarkdownFilename, ContentHash: "sha256:" + strings.Repeat("81", 32), Size: 1, EnvelopeHash: "sha256:" + strings.Repeat("82", 32)},
			{Path: finalSemanticEvidenceFilename, ContentHash: "sha256:" + strings.Repeat("83", 32), Size: 1, EnvelopeHash: "sha256:" + strings.Repeat("84", 32)},
		},
	}
	supplement, err := signEvidence(cfg, finalSemanticSupplementKind, runID, payload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	supplementBytes, err := json.Marshal(supplement)
	if err != nil {
		t.Fatal(err)
	}
	objectGets := 0
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		objectGets++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(supplementBytes)), Request: request}, nil
	})}}
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		Operators: []PublicOperator{{NoID: 1, APIURL: "https://operator.example"}},
	}
	if _, _, err := probe.authenticateReplicatedFinalSemanticSupplement(context.Background(), public, roles.EVM["testnet-owner"].Address, supplement.ContentHash, complete, manifest, result, payload.CaptureStatusHash, payload.CollectedInputsHash, 1); err == nil || !strings.Contains(err.Error(), "aggregate") {
		t.Fatalf("combined base/supplement aggregate overflow was accepted: %v", err)
	}
	if objectGets != 1 {
		t.Fatalf("combined aggregate overflow caused %d object GETs, want supplement marker only", objectGets)
	}
}

func TestCampaignEvidenceClosureRejectsSignedPostCaptureReference(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	entry := func(name, hash string) campaignEvidenceFileEntry {
		return campaignEvidenceFileEntry{Path: name, ContentHash: hash, Size: 1, EnvelopeHash: "sha256:" + strings.Repeat("22", 32)}
	}
	payload := campaignEvidenceManifestPayload{
		Schema: campaignEvidenceManifestSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, RunID: "release-run", ResultHash: finalTestHex(0x11),
		BundlePayloadHash: "sha256:" + strings.Repeat("33", 32),
		Files:             []campaignEvidenceFileEntry{entry("result.json", "sha256:"+strings.Repeat("44", 32))},
		References:        []campaignEvidenceFileEntry{entry("final-derived/forged.json", "sha256:"+strings.Repeat("55", 32))},
	}
	envelope, err := signEvidence(cfg, campaignEvidenceManifestKind, payload.RunID, payload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCampaignEvidenceManifest(envelope); err == nil || !strings.Contains(err.Error(), "post-capture") {
		t.Fatalf("signed post-capture reference entered the closed archive: %v", err)
	}
}

func TestSemanticSupplementHistoryCapPreventsEvidenceFanout(t *testing.T) {
	objects := make([]map[string]string, maximumCampaignEvidenceObjects+1)
	for index := range objects {
		objects[index] = map[string]string{"key": fmt.Sprintf("history/%064x.json", index+1)}
	}
	history, err := json.Marshal(map[string]any{"schema": "urnetwork-release-evidence-history-v1", "objects": objects})
	if err != nil {
		t.Fatal(err)
	}
	evidenceFetches := 0
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/sn/evidence") {
			evidenceFetches++
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(history)), Request: request}, nil
	})}}
	public := &PublicDeploymentManifest{DeploymentID: "deployment", Netuid: 1, Operators: []PublicOperator{
		{NoID: 1, HistoryURL: "https://operator-1.example/sn/evidence/history"},
		{NoID: 2, HistoryURL: "https://operator-2.example/sn/evidence/history"},
	}}
	if _, err := probe.discoverReplicatedFinalSemanticSupplementHashes(context.Background(), public, "release-run"); err == nil || !strings.Contains(err.Error(), "exceeds 8") {
		t.Fatalf("over-cap semantic supplement history was accepted: %v", err)
	}
	if evidenceFetches != 0 {
		t.Fatalf("over-cap semantic supplement history triggered %d evidence-object fetches", evidenceFetches)
	}
}

func TestSemanticSupplementPerRunCandidateCapPreventsEvidenceFanout(t *testing.T) {
	objects := make([]map[string]string, maximumFinalSemanticSupplementCandidatesPerRun+1)
	for index := range objects {
		objects[index] = map[string]string{"key": fmt.Sprintf("prefix/st/v1/evidence/history/deployment/1/%s/release-run/%064x.json", finalSemanticSupplementKind, index+1)}
	}
	history, err := json.Marshal(map[string]any{"schema": "urnetwork-release-evidence-history-v1", "objects": objects})
	if err != nil {
		t.Fatal(err)
	}
	evidenceFetches := 0
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/sn/evidence") {
			evidenceFetches++
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(history)), Request: request}, nil
	})}}
	public := &PublicDeploymentManifest{DeploymentID: "deployment", Netuid: 1, Operators: []PublicOperator{
		{NoID: 1, HistoryURL: "https://operator-1.example/sn/evidence/history"},
		{NoID: 2, HistoryURL: "https://operator-2.example/sn/evidence/history"},
	}}
	if _, err := probe.discoverReplicatedFinalSemanticSupplementHashes(context.Background(), public, "release-run"); err == nil || !strings.Contains(err.Error(), "exceeds 8") {
		t.Fatalf("over-cap per-run semantic supplement history was accepted: %v", err)
	}
	if evidenceFetches != 0 {
		t.Fatalf("over-cap per-run semantic supplement history triggered %d evidence-object fetches", evidenceFetches)
	}
}

func TestSemanticSupplementRunScopedHistoryRejectsPaginationAndOffPrefix(t *testing.T) {
	hash := strings.Repeat("61", 32)
	responses := map[string][]byte{}
	evidenceFetches := 0
	queries := 0
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/sn/evidence") {
			evidenceFetches++
		}
		if strings.HasSuffix(request.URL.Path, "/sn/evidence/history") {
			queries++
			if request.URL.Query().Get("run_id") != "release-run" || request.URL.Query().Get("limit") != fmt.Sprint(maximumFinalSemanticSupplementCandidatesPerRun) {
				t.Fatalf("semantic history query = %q", request.URL.RawQuery)
			}
		}
		body := responses[request.URL.Host]
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}}
	public := &PublicDeploymentManifest{DeploymentID: "deployment", Netuid: 1, Operators: []PublicOperator{
		{NoID: 1, HistoryURL: "https://operator-1.example/sn/evidence/history"},
		{NoID: 2, HistoryURL: "https://operator-2.example/sn/evidence/history"},
	}}
	page, err := json.Marshal(map[string]any{
		"schema": "urnetwork-release-evidence-history-v1", "more": true,
		"next_after": "prefix/st/v1/evidence/history/deployment/1/" + finalSemanticSupplementKind + "/release-run/" + hash + ".json",
		"objects":    []map[string]string{{"key": "prefix/st/v1/evidence/history/deployment/1/" + finalSemanticSupplementKind + "/release-run/" + hash + ".json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	responses["operator-1.example"], responses["operator-2.example"] = page, page
	if _, err := probe.discoverReplicatedFinalSemanticSupplementHashes(context.Background(), public, "release-run"); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("paginated semantic history was accepted: %v", err)
	}
	offPrefix, err := json.Marshal(map[string]any{
		"schema": "urnetwork-release-evidence-history-v1", "more": false, "next_after": "",
		"objects": []map[string]string{{"key": "prefix/st/v1/evidence/history/deployment/1/" + finalSemanticSupplementKind + "/other-run/" + hash + ".json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	responses["operator-1.example"], responses["operator-2.example"] = offPrefix, offPrefix
	if _, err := probe.discoverReplicatedFinalSemanticSupplementHashes(context.Background(), public, "release-run"); err == nil || !strings.Contains(err.Error(), "outside the requested run") {
		t.Fatalf("off-prefix semantic history was accepted: %v", err)
	}
	duplicate, err := json.Marshal(map[string]any{
		"schema": "urnetwork-release-evidence-history-v1", "more": false, "next_after": "",
		"objects": []map[string]string{
			{"key": "prefix/st/v1/evidence/history/deployment/1/" + finalSemanticSupplementKind + "/release-run/" + hash + ".json"},
			{"key": "prefix/st/v1/evidence/history/deployment/1/" + finalSemanticSupplementKind + "/release-run/" + hash + ".json"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	responses["operator-1.example"], responses["operator-2.example"] = duplicate, duplicate
	if _, err := probe.discoverReplicatedFinalSemanticSupplementHashes(context.Background(), public, "release-run"); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Fatalf("duplicate semantic supplement history was accepted: %v", err)
	}
	if evidenceFetches != 0 || queries != 3 {
		t.Fatalf("rejected semantic histories made evidence=%d history=%d requests, want 0/3", evidenceFetches, queries)
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
	validDirectory := &PublicDeploymentManifest{
		DeploymentID: "deployment", Netuid: 1, Topology: TopologyConfig{Operators: 2},
		Operators: []PublicOperator{
			{NoID: 1, APIURL: "https://operator-1.example", VerifyURL: "https://operator-1.example/verify", HistoryURL: "https://operator-1.example/sn/evidence/history"},
			{NoID: 2, APIURL: "https://operator-2.example/", VerifyURL: "https://operator-2.example/verify", HistoryURL: "https://operator-2.example/sn/evidence/history"},
		},
	}
	bindPublicHistoryEndpointsForTest(t, validDirectory)
	if err := validatePublicCampaignOperatorOrigins(validDirectory); err != nil {
		t.Fatalf("public HTTPS operator directory rejected: %v", err)
	}
	mutations := []struct {
		name   string
		mutate func(*PublicDeploymentManifest)
	}{
		{name: "insecure API", mutate: func(value *PublicDeploymentManifest) { value.Operators[0].APIURL = "http://operator-1.example" }},
		{name: "cross-origin history", mutate: func(value *PublicDeploymentManifest) {
			value.Operators[0].HistoryURL = "https://attacker.example/sn/evidence/history"
		}},
		{name: "duplicate origin", mutate: func(value *PublicDeploymentManifest) {
			value.Operators[1].APIURL = value.Operators[0].APIURL
			value.Operators[1].HistoryURL = value.Operators[0].HistoryURL
		}},
		{name: "missing artifact locator", mutate: func(value *PublicDeploymentManifest) { value.ArtifactStores = value.ArtifactStores[:1] }},
		{name: "missing evidence locator", mutate: func(value *PublicDeploymentManifest) { value.EvidenceStores = value.EvidenceStores[:1] }},
		{name: "duplicate artifact locator", mutate: func(value *PublicDeploymentManifest) { value.ArtifactStores[1] = value.ArtifactStores[0] }},
		{name: "duplicate evidence locator", mutate: func(value *PublicDeploymentManifest) { value.EvidenceStores[1] = value.EvidenceStores[0] }},
		{name: "tampered artifact origin", mutate: func(value *PublicDeploymentManifest) {
			value.ArtifactStores[0] = strings.Replace(value.ArtifactStores[0], "operator-1.example", "attacker.example", 1)
		}},
		{name: "extra artifact query", mutate: func(value *PublicDeploymentManifest) { value.ArtifactStores[0] += "&after=forged" }},
		{name: "extra evidence query", mutate: func(value *PublicDeploymentManifest) { value.EvidenceStores[0] += "&after=forged" }},
		{name: "missing analyze command", mutate: func(value *PublicDeploymentManifest) { delete(value.Commands, "analyze") }},
		{name: "legacy analyze with current locators", mutate: func(value *PublicDeploymentManifest) {
			value.Commands["analyze"] = legacyPublicManifestAnalyzeCommand
		}},
	}
	for _, mutation := range mutations {
		candidate := *validDirectory
		candidate.Operators = append([]PublicOperator(nil), validDirectory.Operators...)
		candidate.ArtifactStores = append([]string(nil), validDirectory.ArtifactStores...)
		candidate.EvidenceStores = append([]string(nil), validDirectory.EvidenceStores...)
		candidate.Commands = make(map[string]string, len(validDirectory.Commands))
		for name, command := range validDirectory.Commands {
			candidate.Commands[name] = command
		}
		mutation.mutate(&candidate)
		if err := validatePublicCampaignOperatorOrigins(&candidate); err == nil {
			t.Fatalf("public operator directory with %s was accepted", mutation.name)
		}
	}
}

// finalSemanticPublicManifestFixture builds the exact public transport and
// deployment identity which the sealed semantic transcript authenticates.
func finalSemanticPublicManifestFixture(t *testing.T, cfg *ResolvedConfig, evidence *FinalSemanticEvidence, reader *finalTestChainReader) *PublicDeploymentManifest {
	t.Helper()
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Release == nil || evidence == nil || reader == nil {
		t.Fatal("final semantic public manifest fixture context is incomplete")
	}
	substrateRPC, evmRPC, _ := reader.Endpoints()
	releaseLockHash, err := canonicalHashHex(cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", Release: "1.0", Revision: 1, GeneratedAt: evidence.CampaignCompletedAt,
		DeploymentID: evidence.DeploymentID, PlanHash: evidence.PlanHash, ConfigHash: evidence.ConfigHash, PolicyHash: evidence.PolicyHash,
		GenesisHash: evidence.GenesisHash, ChainID: evidence.ChainID, Netuid: evidence.Netuid, Topology: cfg.Config.Topology,
		RuntimeSpec: cfg.Public.Chain.ExpectedRuntimeSpec, TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: cfg.Public.Chain.ExpectedStateVersion,
		RuntimeCodeHash: cfg.Release.Runtime.CodeHash, RuntimeMetadataHash: cfg.Release.Runtime.MetadataHash, ReleaseLockHash: releaseLockHash,
		EVMRPC: evmRPC, SubstrateRPC: substrateRPC, OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
		EvidenceTransportProfile: publicEvidenceTransportHTTPS, Commands: map[string]string{},
		Contracts: &ContractDeployment{
			Schema: "urnetwork-contract-deployment-v1", DeploymentID: evidence.DeploymentID,
			CoordinatorProxy: common.HexToAddress(evidence.Deployment.CoordinatorProxy), CoordinatorImplementation: common.HexToAddress(evidence.Deployment.CoordinatorImplementation),
			SettlementVault: common.HexToAddress(evidence.Deployment.SettlementVault), ReserveSink: common.HexToAddress(evidence.Deployment.ReserveSink),
		},
	}
	for _, origin := range reader.OperatorEvidenceOrigins() {
		apiURL, err := publicEvidenceOrigin(origin.ManifestURI, public.EvidenceTransportProfile, public.ChainID, public.GenesisHash)
		if err != nil {
			t.Fatal(err)
		}
		public.Operators = append(public.Operators, PublicOperator{
			NoID: origin.OperatorNoID, APIURL: apiURL, VerifyURL: apiURL + "/verify", HistoryURL: apiURL + "/sn/evidence/history",
		})
	}
	bindPublicHistoryEndpointsForTest(t, public)
	if err := validatePublicCampaignOperatorOrigins(public); err != nil {
		t.Fatalf("final semantic public manifest fixture: %v", err)
	}
	allowed, err := campaignArtifactAllowedOrigins(public, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range reader.OperatorEvidenceOrigins() {
		if err := validateCampaignArtifactOrigin(origin.ManifestURI, allowed); err != nil {
			t.Fatalf("final semantic operator %d fixture origin: %v", origin.OperatorNoID, err)
		}
	}
	reader.publicManifestHash, err = canonicalHashHex(public)
	if err != nil {
		t.Fatal(err)
	}
	return public
}

func TestCampaignFinalSemanticEvidenceRequiresExactlyOneClosedObject(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	source, artifacts := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	reader := &finalTestChainReader{evidence: draft}
	public := finalSemanticPublicManifestFixture(t, cfg, draft, reader)
	semantic, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(semantic, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	markdown, err := RenderFinalSemanticEvidenceMarkdown(semantic)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.PublicVerification == nil || semantic.PublicVerification.PublicManifestHash != reader.publicManifestHash || semantic.PublicVerification.SubstrateRPC != public.SubstrateRPC || semantic.PublicVerification.EVMRPC != public.EVMRPC || semantic.PublicVerification.EvidenceTransportProfile != public.EvidenceTransportProfile {
		t.Fatal("sealed semantic fixture does not bind its exact public manifest and transport")
	}
	window := semantic.Window
	bundle := &ScenarioEvidenceBundle{Result: &ScenarioResult{
		Name: semantic.Phase, RunID: semantic.RunID, EvidenceHash: semantic.ResultHash, DeploymentID: semantic.DeploymentID, ConfigHash: semantic.ConfigHash, PolicyHash: semantic.PolicyHash,
		ChainID: semantic.ChainID, GenesisHash: semantic.GenesisHash, Netuid: semantic.Netuid, StartedAt: semantic.CampaignStartedAt, CompletedAt: semantic.CampaignCompletedAt,
		CampaignStartHead: semantic.EVMCampaignStartHead, EndHead: semantic.EVMTerminalHead, AcceptanceWindow: &window,
		LifecycleHandoff: &ScenarioLifecycleHandoff{
			Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: semantic.RunID, Stage: fleetLifecycleStageReleaseHandoff,
			File: scenarioLifecycleHandoffFilename, ContentHash: semantic.FleetLifecycle.ReleaseHandoffHash, SizeBytes: semantic.FleetLifecycle.ReleaseHandoffSize,
		},
	}}
	verifyCalls := 0
	probe := &liveScenarioProbe{
		publicManifestURI: semantic.PublicVerification.EvidenceURI,
		finalSemanticVerify: func(ctx context.Context, _ *PublicDeploymentManifest, evidence *FinalSemanticEvidence, _ string) error {
			verifyCalls++
			return VerifyFinalSemanticEvidenceOnChain(ctx, evidence, &finalTestChainReader{evidence: evidence, publicManifestHash: reader.publicManifestHash})
		},
	}
	owner := semantic.Deployment.GovernanceOwner
	cloneFiles := func(source map[string][]byte) map[string][]byte {
		clone := make(map[string][]byte, len(source))
		for name, raw := range source {
			clone[name] = raw
		}
		return clone
	}
	// The fixture builder retains intermediate artifacts which are deliberately
	// absent from the sealed graph. Reconstruct the authenticated scopes from
	// the graph's exact locator census, just as the production collector does.
	semanticReferences := map[string]campaignArtifactReference{}
	if err := collectCampaignArtifactReferences(encoded, semanticReferences, 0); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	referencedFiles := map[string][]byte{}
	for name, reference := range semanticReferences {
		raw, ok := artifacts[name]
		if !ok || uint64(len(raw)) != reference.Size || !strings.EqualFold(bytesSHA256(raw), reference.ContentHash) {
			t.Fatalf("sealed semantic fixture artifact %q is unavailable or differs from its locator", name)
		}
		if strings.HasPrefix(name, "final-derived/") {
			files[name] = raw
		} else {
			referencedFiles[name] = raw
		}
	}
	missingSemanticFiles := cloneFiles(files)
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, missingSemanticFiles, referencedFiles, nil); err == nil || !strings.Contains(err.Error(), "final semantic") {
		t.Fatalf("campaign without final semantic evidence = %v", err)
	}
	files[finalSemanticEvidenceFilename] = encoded
	files[finalSemanticMarkdownFilename] = markdown
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, files, referencedFiles, nil); err != nil {
		t.Fatalf("one closed final semantic object = %v", err)
	}
	if verifyCalls != 1 {
		t.Fatalf("public archive verifier calls = %d, want 1", verifyCalls)
	}
	for name, mutate := range map[string]func(*FinalSemanticEvidence){
		"campaign start head": func(value *FinalSemanticEvidence) { value.EVMCampaignStartHead.Hash = finalTestHex(0xfe) },
		"campaign start time": func(value *FinalSemanticEvidence) {
			started, parseErr := time.Parse(time.RFC3339Nano, value.CampaignStartedAt)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			value.CampaignStartedAt = started.Add(time.Second).UTC().Format(time.RFC3339Nano)
		},
		"campaign completion time": func(value *FinalSemanticEvidence) {
			completed, parseErr := time.Parse(time.RFC3339Nano, value.CampaignCompletedAt)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			value.CampaignCompletedAt = completed.Add(-time.Second).UTC().Format(time.RFC3339Nano)
		},
	} {
		mutatedSource := source
		mutate(&mutatedSource)
		mutatedDraft, err := BuildFinalSemanticEvidence(mutatedSource)
		if err != nil {
			t.Fatalf("build internally valid %s mutation: %v", name, err)
		}
		mutated, err := SealFinalSemanticEvidenceOnChain(context.Background(), mutatedDraft, &finalTestChainReader{evidence: mutatedDraft, publicManifestHash: reader.publicManifestHash})
		if err != nil {
			t.Fatalf("seal internally valid %s mutation: %v", name, err)
		}
		mutatedJSON, err := json.MarshalIndent(mutated, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		mutatedJSON = append(mutatedJSON, '\n')
		mutatedMarkdown, err := RenderFinalSemanticEvidenceMarkdown(mutated)
		if err != nil {
			t.Fatal(err)
		}
		mutatedFiles := cloneFiles(files)
		mutatedFiles[finalSemanticEvidenceFilename] = mutatedJSON
		mutatedFiles[finalSemanticMarkdownFilename] = mutatedMarkdown
		if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, mutatedFiles, referencedFiles, nil); err == nil || !strings.Contains(err.Error(), "does not bind") {
			t.Fatalf("signed semantic %s mutation detached from its scenario result was accepted: %v", name, err)
		}
	}
	noncanonical, err := json.Marshal(semantic)
	if err != nil {
		t.Fatal(err)
	}
	noncanonicalFiles := cloneFiles(files)
	noncanonicalFiles[finalSemanticEvidenceFilename] = noncanonical
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, noncanonicalFiles, referencedFiles, nil); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("signed noncanonical semantic JSON was accepted: %v", err)
	}
	unreferencedDerived := make(map[string][]byte, len(files)+1)
	for name, raw := range files {
		unreferencedDerived[name] = raw
	}
	unreferencedDerived["final-derived/unreferenced.json"] = []byte("{\"schema\":\"urnetwork-unreferenced-v1\"}\n")
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, unreferencedDerived, referencedFiles, nil); err == nil || !strings.Contains(err.Error(), "unreferenced derived") {
		t.Fatalf("unreferenced signed final-derived file was accepted: %v", err)
	}
	collidingArtifacts := cloneFiles(referencedFiles)
	collidingArtifacts[finalSemanticMarkdownFilename] = markdown
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, files, collidingArtifacts, nil); err == nil || !strings.Contains(err.Error(), "multiple evidence scopes") {
		t.Fatalf("cross-scope semantic URI collision was accepted: %v", err)
	}
	detachedPublic := *public
	generatedAt, err := time.Parse(time.RFC3339Nano, public.GeneratedAt)
	if err != nil {
		t.Fatal(err)
	}
	detachedPublic.GeneratedAt = generatedAt.Add(time.Nanosecond).UTC().Format(time.RFC3339Nano)
	if detachedHash, err := canonicalHashHex(&detachedPublic); err != nil || detachedHash == semantic.PublicVerification.PublicManifestHash {
		t.Fatalf("detached public manifest did not change only its canonical hash: hash=%q err=%v", detachedHash, err)
	}
	defaultProbe := &liveScenarioProbe{publicManifestURI: semantic.PublicVerification.EvidenceURI}
	if _, err := defaultProbe.verifyCampaignFinalSemanticEvidence(context.Background(), &detachedPublic, owner, bundle, nil, files, referencedFiles, nil); err == nil || !strings.Contains(err.Error(), "public manifest hash") {
		t.Fatalf("semantic evidence detached from the authenticated public manifest was accepted: %v", err)
	}
	tamperedMarkdownFiles := cloneFiles(files)
	tamperedMarkdownFiles[finalSemanticMarkdownFilename] = []byte("# unbound report\n")
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, tamperedMarkdownFiles, referencedFiles, nil); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unbound FINAL.md = %v", err)
	}
	missingArtifacts := cloneFiles(files)
	missing := semantic.Validators[0].Cycles[0].IntentArtifact.URI
	delete(missingArtifacts, missing)
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, missingArtifacts, referencedFiles, nil); err == nil || !strings.Contains(err.Error(), "differs from semantic locator") {
		t.Fatalf("semantic evidence with missing external artifact = %v", err)
	}
	duplicates := cloneFiles(referencedFiles)
	duplicates["duplicate-final-semantic-evidence.json"] = encoded
	if _, err := probe.verifyCampaignFinalSemanticEvidence(context.Background(), public, owner, bundle, nil, files, duplicates, nil); err == nil || !strings.Contains(err.Error(), "unexpected final semantic") {
		t.Fatalf("campaign with duplicate final semantic evidence = %v", err)
	}
}

func TestCampaignFinalSemanticObjectCensusAllowsOnlyBoundPriorReport(t *testing.T) {
	current := []byte(`{"schema":"` + finalSemanticEvidenceSchema + `"}`)
	prior := []byte(`{"schema":"` + finalSemanticEvidenceSchema + `","phase":"release-1.0"}`)
	priorPath := "final-derived/prior-release/final-semantic-evidence.json"
	semantic := &FinalSemanticEvidence{PriorPhase: &FinalPriorPhaseBinding{SemanticEvidence: FinalArtifactLocator{URI: priorPath}}}
	files := map[string][]byte{finalSemanticEvidenceFilename: current, priorPath: prior}
	if err := validateCampaignFinalSemanticObjectCensus(semantic, files); err != nil {
		t.Fatalf("current and bound prior semantic reports were rejected: %v", err)
	}
	files["final-derived/unexpected/final-semantic-evidence.json"] = prior
	if err := validateCampaignFinalSemanticObjectCensus(semantic, files); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("unbound third semantic report was accepted: %v", err)
	}
	delete(files, "final-derived/unexpected/final-semantic-evidence.json")
	delete(files, priorPath)
	if err := validateCampaignFinalSemanticObjectCensus(semantic, files); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing bound prior semantic report was accepted: %v", err)
	}
}

func publicPhaseLineageFixture(t *testing.T) (*authenticatedPublicScenarioCandidate, *authenticatedPublicScenarioCandidate) {
	t.Helper()
	releaseWindow := &ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 90, Hash: "0x" + strings.Repeat("10", 32)}, TerminalBlock: 120, EpochCount: 5}
	productionWindow := &ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 200, Hash: "0x" + strings.Repeat("20", 32)}, TerminalBlock: 260, EpochCount: 3}
	handoffBytes := []byte("authenticated release lifecycle handoff")
	handoff := &ScenarioLifecycleHandoff{
		Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: "release-run", Stage: fleetLifecycleStageReleaseHandoff,
		File: scenarioLifecycleHandoffFilename, ContentHash: bytesSHA256(handoffBytes), SizeBytes: uint64(len(handoffBytes)),
	}
	releaseResult := &ScenarioResult{
		Name: "release-1.0", RunID: "release-run", EvidenceHash: "0x" + strings.Repeat("31", 32), DeploymentID: "deployment",
		ConfigHash: "0x" + strings.Repeat("32", 32), PolicyHash: "0x" + strings.Repeat("33", 32), GenesisHash: "0x" + strings.Repeat("34", 32), ChainID: 945, Netuid: 521,
		CompletedAt: "2026-09-02T01:00:00Z", EndHead: ChainHead{Number: 120, Hash: "0x" + strings.Repeat("35", 32)}, StartEpoch: 25, EndEpoch: 30, AcceptanceWindow: releaseWindow, LifecycleHandoff: handoff,
	}
	releaseComplete := &ReleaseEvidenceEnvelope{RunID: releaseResult.RunID, ContentHash: "sha256:" + strings.Repeat("54", 32), Signature: "0xrelease", Payload: json.RawMessage(`{"result":"release"}`)}
	gate := &ReleaseCampaignGate{
		Schema: releaseCampaignGateSchema, RunID: releaseResult.RunID, ResultHash: releaseResult.EvidenceHash, CompleteContentHash: releaseComplete.ContentHash,
		StartEpoch: releaseResult.StartEpoch, EndEpoch: releaseResult.EndEpoch, LifecycleHandoff: *handoff,
	}
	productionResult := &ScenarioResult{
		Name: "production-soak", RunID: "production-run", EvidenceHash: "0x" + strings.Repeat("41", 32), DeploymentID: releaseResult.DeploymentID,
		ConfigHash: releaseResult.ConfigHash, PolicyHash: releaseResult.PolicyHash, GenesisHash: releaseResult.GenesisHash, ChainID: releaseResult.ChainID, Netuid: releaseResult.Netuid,
		StartedAt: "2026-09-02T02:00:00Z", EndHead: ChainHead{Number: 260, Hash: "0x" + strings.Repeat("42", 32)}, AcceptanceWindow: productionWindow, PriorRelease: gate,
	}
	releasePayload := scenarioCompletePayload{
		ResultHash: releaseResult.EvidenceHash, BundlePayloadHash: "sha256:" + strings.Repeat("51", 32), EvidenceManifestHash: "sha256:" + strings.Repeat("52", 32), Files: map[string]string{"result.json": "sha256:" + strings.Repeat("53", 32)},
		LifecycleHandoff: handoff,
	}
	releaseCompleteBytes, err := json.Marshal(releaseComplete)
	if err != nil {
		t.Fatal(err)
	}
	releaseManifest := &ReleaseEvidenceEnvelope{RunID: releaseResult.RunID, ContentHash: releasePayload.EvidenceManifestHash, Signature: "0xmanifest", Payload: json.RawMessage(`{"manifest":"release"}`)}
	nativeTerminal := ChainHead{Number: 119, Hash: "0x" + strings.Repeat("55", 32)}
	releaseSemantic := &FinalSemanticEvidence{Phase: "release-1.0", NativeTerminalHead: nativeTerminal, FleetLifecycle: &FinalFleetLifecycleEvidence{ReleaseHandoffHash: handoff.ContentHash, ReleaseHandoffSize: handoff.SizeBytes}}
	priorBinding := &FinalPriorPhaseBinding{
		RunID: releaseResult.RunID, ResultHash: releaseResult.EvidenceHash, OwnerCompletionEnvelopeHash: releaseComplete.ContentHash, EvidenceManifestEnvelopeHash: releaseManifest.ContentHash, AcceptanceWindow: *releaseWindow,
		TerminalNativeHead: nativeTerminal, TerminalEVMHead: releaseResult.EndHead,
	}
	productionSemantic := &FinalSemanticEvidence{Phase: "production-soak", PriorPhase: priorBinding, FleetLifecycle: &FinalFleetLifecycleEvidence{ReleaseHandoffHash: handoff.ContentHash, ReleaseHandoffSize: handoff.SizeBytes}}
	prior := &authenticatedPublicScenarioCandidate{
		candidate:  &publicScenarioCandidate{bundle: &ScenarioEvidenceBundle{Result: releaseResult}, payload: releasePayload.BundlePayloadHash, signers: map[int]bool{1: true, 2: true}},
		completion: &publicScenarioCompletionCandidate{envelope: releaseComplete, payload: releasePayload, payloadBytes: releaseCompleteBytes, operators: map[int]bool{1: true, 2: true}},
		semantic: &authenticatedCampaignSemantic{
			Evidence: releaseSemantic, EvidenceManifest: releaseManifest,
			Artifacts: map[string][]byte{handoff.File: handoffBytes},
		},
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

	current, prior = publicPhaseLineageFixture(t)
	current.candidate.bundle.Result.PriorRelease.EndEpoch++
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("release gate detached from authenticated prior result accepted: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	prior.semantic.Artifacts[scenarioLifecycleHandoffFilename] = []byte("substituted lifecycle handoff")
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "handoff bytes") {
		t.Fatalf("substituted release lifecycle handoff bytes accepted: %v", err)
	}

	current, prior = publicPhaseLineageFixture(t)
	changed := *prior.completion.payload.LifecycleHandoff
	changed.SizeBytes++
	prior.completion.payload.LifecycleHandoff = &changed
	if err := validateAuthenticatedPublicPhaseLineage(current, prior); err == nil || !strings.Contains(err.Error(), "completion lineage") {
		t.Fatalf("prior completion with divergent lifecycle handoff accepted: %v", err)
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
		StartedAt: time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), CompletedAt: time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		CampaignStartHead: ChainHead{Number: 5, Hash: finalTestHex(5)}, EndHead: terminal, AcceptanceWindow: &window,
	}
	handoff := &ScenarioLifecycleHandoff{
		Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: result.RunID, Stage: fleetLifecycleStageReleaseHandoff,
		File: scenarioLifecycleHandoffFilename, ContentHash: "sha256:" + strings.Repeat("31", 32), SizeBytes: 128,
	}
	result.LifecycleHandoff = handoff
	source := FinalSemanticEvidence{
		Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash,
		CampaignStartedAt: result.StartedAt, CampaignCompletedAt: result.CompletedAt,
		DeploymentID: result.DeploymentID, PlanHash: finalTestHex(2), ConfigHash: result.ConfigHash, PolicyHash: result.PolicyHash,
		ChainID: result.ChainID, GenesisHash: result.GenesisHash, Netuid: result.Netuid,
		Window: window, EVMCampaignStartHead: result.CampaignStartHead, EVMTerminalHead: terminal,
		ExpectedOperators: cfg.Config.Topology.Operators, ExpectedValidators: cfg.Config.Topology.Validators, ExpectedMiners: cfg.Config.Topology.Miners,
		ExpectedCandidates: cfg.Config.Topology.HeadFleets + cfg.Config.Topology.ChallengerFleets, ExpectedHeadSlots: cfg.Config.Topology.HeadSlots,
		Deployment:     FinalContractDeploymentEvidence{GovernanceOwner: roles.EVM["testnet-owner"].Address},
		FleetLifecycle: &FinalFleetLifecycleEvidence{ReleaseHandoffHash: handoff.ContentHash, ReleaseHandoffSize: handoff.SizeBytes},
	}
	if err := validateScenarioFinalSemanticSource(cfg, roles, result, &source); err != nil {
		t.Fatalf("exact semantic campaign source rejected: %v", err)
	}
	for name, mutate := range map[string]func(*FinalSemanticEvidence){
		"phase":       func(value *FinalSemanticEvidence) { value.Phase = "production-soak" },
		"run id":      func(value *FinalSemanticEvidence) { value.RunID = "other-run" },
		"result hash": func(value *FinalSemanticEvidence) { value.ResultHash = finalTestHex(0xee) },
		"campaign start": func(value *FinalSemanticEvidence) {
			value.CampaignStartedAt = time.Date(2026, 9, 1, 1, 0, 1, 0, time.UTC).Format(time.RFC3339Nano)
		},
		"campaign end": func(value *FinalSemanticEvidence) {
			value.CampaignCompletedAt = time.Date(2026, 9, 1, 2, 0, 1, 0, time.UTC).Format(time.RFC3339Nano)
		},
		"start head": func(value *FinalSemanticEvidence) { value.EVMCampaignStartHead.Number++ },
		"lifecycle handoff": func(value *FinalSemanticEvidence) {
			value.FleetLifecycle = &FinalFleetLifecycleEvidence{ReleaseHandoffHash: "sha256:" + strings.Repeat("32", 32), ReleaseHandoffSize: handoff.SizeBytes}
		},
	} {
		changed := source
		mutate(&changed)
		if err := validateScenarioFinalSemanticSource(cfg, roles, result, &changed); err == nil || !strings.Contains(err.Error(), "does not bind") {
			t.Fatalf("wrong semantic source %s accepted: %v", name, err)
		}
	}
}

func TestScenarioFinalSemanticSourceBindsProductionReleaseLineage(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	window := ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 100, Hash: finalTestHex(10)}, BaselineEpoch: 30, FirstEpoch: 31, EpochCount: 2, TerminalBlock: 120}
	priorWindow := ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 10, Hash: finalTestHex(11)}, BaselineEpoch: 5, FirstEpoch: 6, EpochCount: 3, TerminalBlock: 20}
	handoff := ScenarioLifecycleHandoff{
		Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: "release-run", Stage: fleetLifecycleStageReleaseHandoff,
		File: scenarioLifecycleHandoffFilename, ContentHash: "sha256:" + strings.Repeat("41", 32), SizeBytes: 256,
	}
	gate := &ReleaseCampaignGate{
		Schema: releaseCampaignGateSchema, RunID: handoff.ReleaseRunID, ResultHash: finalTestHex(42), CompleteContentHash: "sha256:" + strings.Repeat("43", 32),
		StartEpoch: priorWindow.BaselineEpoch, EndEpoch: priorWindow.BaselineEpoch + priorWindow.EpochCount, LifecycleHandoff: handoff,
	}
	result := &ScenarioResult{
		Name: "production-soak", RunID: "production-run", EvidenceHash: finalTestHex(1), DeploymentID: cfg.Config.Deployment.DeploymentID,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: "2026-09-01T03:00:00Z", CompletedAt: "2026-09-01T04:00:00Z", CampaignStartHead: ChainHead{Number: 90, Hash: finalTestHex(12)}, EndHead: ChainHead{Number: 120, Hash: finalTestHex(13)},
		AcceptanceWindow: &window, PriorRelease: gate,
	}
	source := FinalSemanticEvidence{
		Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash, CampaignStartedAt: result.StartedAt, CampaignCompletedAt: result.CompletedAt,
		DeploymentID: result.DeploymentID, PlanHash: finalTestHex(2), ConfigHash: result.ConfigHash, PolicyHash: result.PolicyHash, ChainID: result.ChainID, GenesisHash: result.GenesisHash, Netuid: result.Netuid,
		Window: window, EVMCampaignStartHead: result.CampaignStartHead, EVMTerminalHead: result.EndHead,
		ExpectedOperators: cfg.Config.Topology.Operators, ExpectedValidators: cfg.Config.Topology.Validators, ExpectedMiners: cfg.Config.Topology.Miners,
		ExpectedCandidates: cfg.Config.Topology.HeadFleets + cfg.Config.Topology.ChallengerFleets, ExpectedHeadSlots: cfg.Config.Topology.HeadSlots,
		Deployment:     FinalContractDeploymentEvidence{GovernanceOwner: roles.EVM["testnet-owner"].Address},
		PriorPhase:     &FinalPriorPhaseBinding{RunID: gate.RunID, ResultHash: gate.ResultHash, OwnerCompletionEnvelopeHash: gate.CompleteContentHash, AcceptanceWindow: priorWindow},
		FleetLifecycle: &FinalFleetLifecycleEvidence{ReleaseHandoffHash: handoff.ContentHash, ReleaseHandoffSize: handoff.SizeBytes},
	}
	if err := validateScenarioFinalSemanticSource(cfg, roles, result, &source); err != nil {
		t.Fatalf("exact production release lineage rejected: %v", err)
	}
	for name, mutate := range map[string]func(*FinalSemanticEvidence){
		"prior result": func(value *FinalSemanticEvidence) { value.PriorPhase.ResultHash = finalTestHex(0xee) },
		"prior completion": func(value *FinalSemanticEvidence) {
			value.PriorPhase.OwnerCompletionEnvelopeHash = "sha256:" + strings.Repeat("ee", 32)
		},
		"prior window":    func(value *FinalSemanticEvidence) { value.PriorPhase.AcceptanceWindow.BaselineEpoch++ },
		"release handoff": func(value *FinalSemanticEvidence) { value.FleetLifecycle.ReleaseHandoffSize++ },
	} {
		changed := source
		prior := *source.PriorPhase
		lifecycle := *source.FleetLifecycle
		changed.PriorPhase = &prior
		changed.FleetLifecycle = &lifecycle
		mutate(&changed)
		if err := validateScenarioFinalSemanticSource(cfg, roles, result, &changed); err == nil || !strings.Contains(err.Error(), "lineage") {
			t.Fatalf("divergent production %s lineage was accepted: %v", name, err)
		}
	}
}

func TestScenarioCompletionLineageBindsSignedResult(t *testing.T) {
	handoff := &ScenarioLifecycleHandoff{Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: "release-run", Stage: fleetLifecycleStageReleaseHandoff, File: scenarioLifecycleHandoffFilename, ContentHash: "sha256:" + strings.Repeat("51", 32), SizeBytes: 1}
	release := &ScenarioResult{Name: "release-1.0", LifecycleHandoff: handoff}
	releaseCompletion := &scenarioCompletePayload{LifecycleHandoff: handoff}
	if err := validateScenarioCompletionLineage(release, releaseCompletion); err != nil {
		t.Fatalf("exact release completion lineage rejected: %v", err)
	}
	changedHandoff := *handoff
	changedHandoff.SizeBytes++
	releaseCompletion.LifecycleHandoff = &changedHandoff
	if err := validateScenarioCompletionLineage(release, releaseCompletion); err == nil {
		t.Fatal("release completion with a different lifecycle handoff was accepted")
	}
	gate := &ReleaseCampaignGate{Schema: releaseCampaignGateSchema, RunID: "release-run", ResultHash: finalTestHex(52), CompleteContentHash: "sha256:" + strings.Repeat("53", 32), StartEpoch: 1, EndEpoch: 2, LifecycleHandoff: *handoff}
	production := &ScenarioResult{Name: "production-soak", PriorRelease: gate}
	productionCompletion := &scenarioCompletePayload{PriorRelease: gate}
	if err := validateScenarioCompletionLineage(production, productionCompletion); err != nil {
		t.Fatalf("exact production completion lineage rejected: %v", err)
	}
	changedGate := *gate
	changedGate.CompleteContentHash = "sha256:" + strings.Repeat("54", 32)
	productionCompletion.PriorRelease = &changedGate
	if err := validateScenarioCompletionLineage(production, productionCompletion); err == nil {
		t.Fatal("production completion with a different release gate was accepted")
	}
}

func TestScenarioArchiveLineageBindsRawHandoffAndExactResultBytes(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, runDir := writeScenarioCampaignFixture(t, cfg, stateDir, "release-1.0", 9, 15)
	owner := roles.EVM["testnet-owner"]
	var complete ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(filepath.Join(runDir, "complete.json"), &complete); err != nil {
		t.Fatal(err)
	}
	var completion scenarioCompletePayload
	if err := decodeStrictJSONBytes(complete.Payload, &completion); err != nil {
		t.Fatal(err)
	}
	lifecycleRaw, err := os.ReadFile(filepath.Join(runDir, result.LifecycleHandoff.File))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{result.LifecycleHandoff.File: lifecycleRaw}
	if err := validateScenarioArchiveLineage(cfg, result, &completion, files); err != nil {
		t.Fatalf("exact archived lifecycle handoff rejected: %v", err)
	}
	delete(files, result.LifecycleHandoff.File)
	if err := validateScenarioArchiveLineage(cfg, result, &completion, files); err == nil || !strings.Contains(err.Error(), "omits") {
		t.Fatalf("missing archived lifecycle handoff accepted: %v", err)
	}
	files[result.LifecycleHandoff.File] = append(append([]byte(nil), lifecycleRaw...), ' ')
	if err := validateScenarioArchiveLineage(cfg, result, &completion, files); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("changed archived lifecycle handoff accepted: %v", err)
	}
	production := &ScenarioResult{Name: "production-soak"}
	productionCompletion := scenarioCompletePayload{}
	if err := validateScenarioArchiveLineage(cfg, production, &productionCompletion, files); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale release handoff in production archive accepted: %v", err)
	}

	files[result.LifecycleHandoff.File] = lifecycleRaw
	analysis := &AnalysisReport{Schema: "urnetwork-sim-analysis-v1"}
	for _, name := range []string{"result.json", "assertions.json", "anomalies.json", "adversaries.json", "observations.jsonl", scenarioCampaignStartFilename, "final-inputs/manifest.json", finalSemanticCaptureStatusFilename} {
		files[name], err = os.ReadFile(filepath.Join(runDir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
	}
	files["analysis.json"], _ = json.Marshal(analysis)
	files["analysis.html"] = []byte("<!doctype html>\n")
	files["junit.xml"] = []byte("<testsuite></testsuite>\n")
	bundle := &ScenarioEvidenceBundle{
		Result: result, Observation: &ScenarioObservation{Schema: "urnetwork-sim-scenario-observation-v1"}, Analysis: analysis,
	}
	if err := validateCampaignEvidenceSemantics(cfg, owner.Address, files, completion, bundle); err != nil {
		t.Fatalf("exact archived result rejected: %v", err)
	}
	originalStart := append([]byte(nil), files[scenarioCampaignStartFilename]...)
	delete(files, scenarioCampaignStartFilename)
	if err := validateCampaignEvidenceSemantics(cfg, owner.Address, files, completion, bundle); err == nil || !strings.Contains(err.Error(), "required file") {
		t.Fatalf("public archive without campaign start marker accepted: %v", err)
	}
	files[scenarioCampaignStartFilename] = originalStart
	var marker ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(originalStart, &marker); err != nil {
		t.Fatal(err)
	}
	var markerPayload scenarioCampaignAttemptPayload
	if err := decodeStrictJSONBytes(marker.Payload, &markerPayload); err != nil {
		t.Fatal(err)
	}
	markerPayload.AcceptanceBoundary.CampaignStartHead.Hash = finalTestHex(72)
	resignedMarker, err := signEvidence(cfg, scenarioCampaignAttemptEvidenceKind, result.RunID, markerPayload, owner)
	if err != nil {
		t.Fatal(err)
	}
	files[scenarioCampaignStartFilename], err = json.Marshal(resignedMarker)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCampaignEvidenceSemantics(cfg, owner.Address, files, completion, bundle); err == nil || !strings.Contains(err.Error(), "differs from its completed result boundary") {
		t.Fatalf("public archive with re-signed divergent campaign start marker accepted: %v", err)
	}
	files[scenarioCampaignStartFilename] = originalStart
	changedResult := *result
	changedResult.PublishedEvidence = []PublishedEvidence{{ContentHash: "sha256:" + strings.Repeat("62", 32)}}
	changedBundle := *bundle
	changedBundle.Result = &changedResult
	if err := validateCampaignEvidenceSemantics(cfg, owner.Address, files, completion, &changedBundle); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("archive and bundle differing only in published evidence were accepted: %v", err)
	}
}

func TestCampaignEvidenceHistoryRejectsDuplicateKeysBeforeFetch(t *testing.T) {
	public := &PublicDeploymentManifest{DeploymentID: "deployment", Netuid: 1}
	hash := strings.Repeat("63", 32)
	key := "fixture/st/v1/evidence/history/deployment/1/scenario-bundle/release.1/" + hash + ".json"
	if _, err := campaignEvidenceHistoryHashes(public, "scenario-bundle", "release.1", []string{key, key}); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Fatalf("duplicate run-scoped history keys were accepted: %v", err)
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

// rebindFinalSemanticReleaseRunFixture moves a detached release fixture to a
// new run while preserving every byte-addressed lifecycle handoff binding.
func rebindFinalSemanticReleaseRunFixture(t *testing.T, source *FinalSemanticEvidence, artifacts map[string][]byte, runID string) ([]byte, *ScenarioLifecycleHandoff) {
	t.Helper()
	if source == nil || source.Phase != "release-1.0" || source.FleetLifecycle == nil || source.RunID == "" || runID == "" || runID == source.RunID {
		t.Fatal("final semantic release-run fixture context is incomplete")
	}
	lifecycle := source.FleetLifecycle
	lineageBytes, ok := artifacts[lifecycle.LineageArtifact.URI]
	if !ok || len(lineageBytes) == 0 {
		t.Fatalf("final semantic lifecycle lineage %q is unavailable", lifecycle.LineageArtifact.URI)
	}
	if lifecycle.LineageArtifact.ContentHash != bytesSHA256(lineageBytes) || lifecycle.LineageArtifact.SizeBytes != uint64(len(lineageBytes)) {
		t.Fatal("final semantic lifecycle lineage locator differs from its bytes")
	}
	var lineage finalFleetLifecycleLineageArtifact
	if err := decodeStrictJSONBytes(lineageBytes, &lineage); err != nil {
		t.Fatalf("decode final semantic lifecycle lineage: %v", err)
	}
	if lineage.Schema != finalFleetLifecycleLineageSchema || lineage.DeploymentID != source.DeploymentID || lineage.PlanHash != source.PlanHash || lineage.RunID != source.RunID || lifecycle.State.RunID != source.RunID {
		t.Fatal("final semantic lifecycle lineage does not match its source run")
	}
	originalStateBytes, err := fleetLifecycleCanonicalBytes(&lifecycle.State)
	if err != nil {
		t.Fatal(err)
	}
	stateFile := -1
	for index := range lineage.Files {
		file := &lineage.Files[index]
		if file.Path != "public/fleet-lifecycle.json" {
			continue
		}
		if stateFile != -1 || file.ContentHash != bytesSHA256(file.Data) || file.SizeBytes != uint64(len(file.Data)) || !bytes.Equal(file.Data, originalStateBytes) {
			t.Fatal("final semantic lifecycle lineage has a duplicate or detached terminal state")
		}
		stateFile = index
	}
	if stateFile == -1 || lifecycle.ReleaseHandoffHash != bytesSHA256(originalStateBytes) || lifecycle.ReleaseHandoffSize != uint64(len(originalStateBytes)) {
		t.Fatal("final semantic lifecycle handoff is not bound to its original terminal state")
	}

	source.RunID = runID
	lifecycle.State.RunID = runID
	stateBytes, err := fleetLifecycleCanonicalBytes(&lifecycle.State)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.ReleaseHandoffHash = bytesSHA256(stateBytes)
	lifecycle.ReleaseHandoffSize = uint64(len(stateBytes))
	lineage.RunID = runID
	lineage.Files[stateFile].Data = append([]byte(nil), stateBytes...)
	lineage.Files[stateFile].ContentHash = lifecycle.ReleaseHandoffHash
	lineage.Files[stateFile].SizeBytes = lifecycle.ReleaseHandoffSize
	replaceFinalSemanticFixtureArtifact(t, &lifecycle.LineageArtifact, artifacts, lineage)
	return stateBytes, &ScenarioLifecycleHandoff{
		Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: runID, Stage: fleetLifecycleStageReleaseHandoff,
		File: scenarioLifecycleHandoffFilename, ContentHash: lifecycle.ReleaseHandoffHash, SizeBytes: lifecycle.ReleaseHandoffSize,
	}
}

// writePublicFinalSemanticClosureFixture writes a valid pre-semantic capture
// graph while preserving the same closed/archive boundary as production.
func writePublicFinalSemanticClosureFixture(t *testing.T, cfg *ResolvedConfig, stateDir, runDir string, semantic FinalSemanticEvidence, result *ScenarioResult, artifacts map[string][]byte) (*FinalSemanticCollectedInputs, *FinalSemanticCaptureStatus) {
	t.Helper()
	resultWire, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	policyBytes, ok := artifacts[semantic.PolicyArtifact.URI]
	if !ok {
		t.Fatalf("semantic policy artifact %q is unavailable", semantic.PolicyArtifact.URI)
	}
	const collectedPolicyPath = "final-inputs/policy.json"
	collectedSource := semantic
	collectedSource.PolicyArtifact = FinalArtifactLocator{Kind: "policy", URI: collectedPolicyPath, ContentHash: bytesSHA256(policyBytes), SizeBytes: uint64(len(policyBytes))}
	collected := finalSemanticSupplementCollectedFixture(t, cfg, collectedSource, result, resultWire)
	matrixBytes, adversariesBytes := finalSemanticSupplementAdversarialArtifacts(t, cfg, result)
	collectedWire := finalSemanticSupplementJSON(t, collected)
	if err := atomicWrite(filepath.Join(runDir, "final-inputs", "manifest.json"), collectedWire, 0o644); err != nil {
		t.Fatal(err)
	}
	references := map[string]campaignArtifactReference{}
	if err := collectCampaignArtifactReferences(collectedWire, references, 0); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(references))
	for name := range references {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		reference := references[name]
		if name == "result.json" {
			continue
		}
		raw, ok := artifacts[name]
		if name == collectedPolicyPath {
			raw, ok = policyBytes, true
		}
		if name == collected.AdversarialMatrix.URI {
			raw, ok = matrixBytes, true
		}
		if name == collected.Adversaries.URI {
			raw, ok = adversariesBytes, true
		}
		if reference.Kind == "closed-input-bundle" {
			raw, ok = finalSemanticSupplementFixtureClosedBundleBytes(t), true
		}
		if !ok {
			raw = finalSemanticSupplementFixtureArtifactBytes(t, reference.Kind, name)
		}
		if uint64(len(raw)) != reference.Size || !strings.EqualFold(bytesSHA256(raw), reference.ContentHash) {
			t.Fatalf("collected-input fixture artifact %q does not match its locator", name)
		}
		root := stateDir
		if strings.HasPrefix(name, "final-inputs/") {
			root = runDir
		}
		if err := atomicWrite(filepath.Join(root, filepath.FromSlash(name)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifestLocator := FinalArtifactLocator{Kind: "final-semantic-input-manifest", URI: "final-inputs/manifest.json", ContentHash: bytesSHA256(collectedWire), SizeBytes: uint64(len(collectedWire))}
	capture := finalSemanticCaptureStatus(result, collected, manifestLocator)
	capture.EvidenceHash, err = finalSemanticCaptureStatusHash(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, finalSemanticCaptureStatusFilename), finalSemanticSupplementJSON(t, capture), 0o644); err != nil {
		t.Fatal(err)
	}
	return collected, capture
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
	t.Parallel()
	setupStarted := time.Now()
	setupPhaseStarted := setupStarted
	// Keep setup costs distinct from the independently timed replay views.
	logSetupPhase := func(phase string) {
		now := time.Now()
		t.Logf("public replay setup %q completed in %s (elapsed %s)", phase, now.Sub(setupPhaseStarted).Round(time.Millisecond), now.Sub(setupStarted).Round(time.Millisecond))
		setupPhaseStarted = now
	}
	cfg := testResolvedConfig(t)
	semanticSource, semanticArtifacts := finalSemanticFixture(t)
	logSetupPhase("detached release-scale fixture")
	cfg.Config.Deployment.DeploymentID = semanticSource.DeploymentID
	cfg.Config.Scenarios.ShortEpochs = int(semanticSource.Window.EpochCount)
	cfg.Config.Topology.Operators = semanticSource.ExpectedOperators
	cfg.Config.Topology.Validators = semanticSource.ExpectedValidators
	cfg.Config.Topology.Miners = semanticSource.ExpectedMiners
	cfg.ConfigHash = semanticSource.ConfigHash
	policy, err := finalSemanticFixturePolicy(&semanticSource, semanticArtifacts)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Policy = policy
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
	logSetupPhase("detached identities")
	semanticSource.Deployment.GovernanceOwner = strings.ToLower(roles.EVM["testnet-owner"].Address)
	result, lifecycleHandoffBytes, campaignStartBytes := finalSemanticCampaignResultFixture(t, cfg, roles, &semanticSource, semanticArtifacts)
	lifecycleHandoff := result.LifecycleHandoff
	if semanticSource.RunID != result.RunID || semanticSource.FleetLifecycle == nil || semanticSource.FleetLifecycle.State.RunID != result.RunID {
		t.Fatal("rebound semantic source does not consistently use the committed run")
	}
	logSetupPhase("committed campaign graph")
	semanticDraft, err := BuildFinalSemanticEvidence(semanticSource)
	if err != nil {
		t.Fatal(err)
	}
	logSetupPhase("semantic evidence build")
	semantic, err := SealFinalSemanticEvidenceOnChain(context.Background(), semanticDraft, &finalTestChainReader{evidence: semanticDraft})
	if err != nil {
		t.Fatal(err)
	}
	if semantic.FleetLifecycle == nil || semantic.FleetLifecycle.State.RunID != result.RunID || semantic.FleetLifecycle.ReleaseHandoffHash != lifecycleHandoff.ContentHash || semantic.FleetLifecycle.ReleaseHandoffSize != lifecycleHandoff.SizeBytes {
		t.Fatal("sealed semantic evidence detached from the committed lifecycle handoff")
	}
	logSetupPhase("public transcript seal")
	reserveBytes, ok := semanticArtifacts[semantic.Reserve.Artifact.URI]
	if !ok {
		t.Fatalf("sealed semantic reserve artifact %q is unavailable", semantic.Reserve.Artifact.URI)
	}
	if err := verifyFinalReserveArtifact(semantic, reserveBytes); err != nil {
		t.Fatalf("sealed semantic reserve artifact fixture is internally inconsistent: %v", err)
	}
	if err := VerifyFinalSemanticArtifacts(context.Background(), semantic, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		raw, exists := semanticArtifacts[locator.URI]
		if !exists {
			return nil, fmt.Errorf("fixture artifact %s is missing", locator.URI)
		}
		return append([]byte(nil), raw...), nil
	}); err != nil {
		t.Fatalf("sealed semantic artifact fixture is internally inconsistent: %v", err)
	}
	logSetupPhase("complete signed artifact verification")
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
	if err := atomicWrite(filepath.Join(runDir, lifecycleHandoff.File), lifecycleHandoffBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, scenarioCampaignStartFilename), campaignStartBytes, 0o644); err != nil {
		t.Fatal(err)
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
	semanticBytes, err := json.Marshal(semantic)
	if err != nil {
		t.Fatal(err)
	}
	semanticReferences := map[string]campaignArtifactReference{}
	if err := collectCampaignArtifactReferences(semanticBytes, semanticReferences, 0); err != nil {
		t.Fatal(err)
	}
	referencedSemanticArtifacts := make(map[string][]byte, len(semanticReferences))
	for name, reference := range semanticReferences {
		if !strings.HasPrefix(name, "final-derived/") {
			continue
		}
		raw, ok := semanticArtifacts[name]
		if !ok || uint64(len(raw)) != reference.Size || !strings.EqualFold(bytesSHA256(raw), reference.ContentHash) {
			t.Fatalf("sealed semantic artifact %q is unavailable or differs from its locator", name)
		}
		referencedSemanticArtifacts[name] = raw
	}
	semanticArtifacts = referencedSemanticArtifacts
	for name, raw := range semanticArtifacts {
		if err := atomicWrite(filepath.Join(runDir, filepath.FromSlash(name)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	logSetupPhase("semantic file publication")
	collected, capture := writePublicFinalSemanticClosureFixture(t, cfg, stateDir, runDir, *semantic, result, semanticArtifacts)
	logSetupPhase("closed input capture")
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
	if _, exists := fileHashes[finalSemanticEvidenceFilename]; exists {
		t.Fatal("post-capture semantic evidence entered the closed scenario file census")
	}
	if _, exists := fileHashes[finalSemanticMarkdownFilename]; exists {
		t.Fatal("post-capture semantic markdown entered the closed scenario file census")
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
	logSetupPhase("signed campaign archive")
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{
		ResultHash: result.EvidenceHash, BundlePayloadHash: bytesSHA256(bundlePayload), Files: fileHashes, EvidenceManifestHash: archive.Manifest.ContentHash,
		LifecycleHandoff: lifecycleHandoff,
	}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	completeBytes, _ := json.Marshal(complete)
	if len(completeBytes) == 0 {
		t.Fatal("owner completion envelope did not encode")
	}
	objects[complete.ContentHash] = completeBytes
	rawSemanticFiles, err := enumerateFinalSemanticRawFiles(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rawSemanticFiles) != len(semanticArtifacts)+2 {
		t.Fatalf("semantic file census = %d, want every %d derived artifacts and both final outputs", len(rawSemanticFiles), len(semanticArtifacts))
	}
	// Each canonical path owns a distinct production staging file. Prepare and
	// encode all of them with bounded workers, then join before signing their
	// common supplement so the causal fence and complete file order stay exact.
	semanticFileEnvelopes := make([]*ReleaseEvidenceEnvelope, len(rawSemanticFiles))
	semanticFileEntries := make([]FinalSemanticSupplementFile, len(rawSemanticFiles))
	semanticFileBytes := make([][]byte, len(rawSemanticFiles))
	fileCases := make([]finalSemanticTestCase, 0, len(rawSemanticFiles))
	for index, raw := range rawSemanticFiles {
		if index > 0 && rawSemanticFiles[index-1].Path >= raw.Path {
			t.Fatalf("semantic staging paths are not strictly ordered and unique at %s", raw.Path)
		}
		fileCases = append(fileCases, finalSemanticTestCase{name: raw.Path, verify: func(context.Context) error {
			envelopes, entries, err := prepareFinalSemanticSupplementFiles(cfg, stateDir, result.RunID, roles.EVM["testnet-owner"], rawSemanticFiles[index:index+1])
			if err != nil {
				return err
			}
			if len(envelopes) != 1 || len(entries) != 1 || entries[0].Path != raw.Path {
				return fmt.Errorf("production supplement preparation changed the single-file census for %s", raw.Path)
			}
			encoded, err := json.Marshal(envelopes[0])
			if err != nil {
				return err
			}
			semanticFileEnvelopes[index], semanticFileEntries[index], semanticFileBytes[index] = envelopes[0], entries[0], encoded
			return nil
		}})
	}
	for index, err := range runFinalSemanticTestCases(context.Background(), fileCases) {
		if err != nil {
			t.Fatalf("prepare complete signed supplement file %s: %v", fileCases[index].name, err)
		}
	}
	logSetupPhase("signed and encoded semantic supplement files")
	supplementPayload := FinalSemanticSupplementPayload{
		Schema: finalSemanticSupplementSchema, Status: finalSemanticSupplementStatus,
		Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash,
		ScenarioCompleteHash: complete.ContentHash, ScenarioEvidenceManifestHash: archive.Manifest.ContentHash,
		CaptureStatusHash: capture.EvidenceHash, CollectedInputsHash: collected.EvidenceHash,
		SemanticEvidenceHash: semantic.EvidenceHash, PublicTranscriptHash: semantic.PublicVerification.TranscriptHash,
		Files: semanticFileEntries,
	}
	if err := validateFinalSemanticSupplementFileManifest(&supplementPayload); err != nil {
		t.Fatalf("complete prepared semantic supplement census: %v", err)
	}
	supplement, err := signEvidence(cfg, finalSemanticSupplementKind, result.RunID, supplementPayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	for index, envelope := range semanticFileEnvelopes {
		objects[envelope.ContentHash] = semanticFileBytes[index]
	}
	supplementBytes, err := json.Marshal(supplement)
	if err != nil {
		t.Fatal(err)
	}
	objects[supplement.ContentHash] = supplementBytes
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
	logSetupPhase("signed semantic supplement and completion replicas")
	commitVisible := map[int]bool{}
	supplementHistory := map[int][]string{}
	objectOverrides := map[int]map[string][]byte{1: {}, 2: {}}
	directOwnerVisible := true
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash,
		Schema: "urnetwork-sim-public-deployment-v1", Netuid: cfg.Netuid, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, PlanHash: semantic.PlanHash,
		RuntimeSpec: cfg.Public.Chain.ExpectedRuntimeSpec, TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: cfg.Public.Chain.ExpectedStateVersion,
		RuntimeCodeHash: cfg.Release.Runtime.CodeHash, RuntimeMetadataHash: cfg.Release.Runtime.MetadataHash,
		EvidenceTransportProfile: publicEvidenceTransportHTTPS,
		EVMRPC:                   "https://evm.example/rpc", SubstrateRPC: "wss://substrate.example/rpc", Identities: identities, Topology: cfg.Config.Topology,
		Contracts: &ContractDeployment{
			Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
			CoordinatorProxy: common.HexToAddress(semantic.Deployment.CoordinatorProxy), CoordinatorImplementation: common.HexToAddress(semantic.Deployment.CoordinatorImplementation),
			SettlementVault: common.HexToAddress(semantic.Deployment.SettlementVault), ReserveSink: common.HexToAddress(semantic.Deployment.ReserveSink),
		},
	}
	trustedOwner := common.HexToAddress(roles.EVM["testnet-owner"].Address)
	wantResultHash := result.EvidenceHash
	wantResultError := ""
	var cases []finalSemanticTestCase
	queueCase := func(name, wantError, missingHash string) {
		caseBundleHashes := append([]string(nil), bundleHashes...)
		view := (finalPublicScenarioTestView{
			objects: objects, commitVisible: commitVisible, supplementHistory: supplementHistory,
			objectOverrides: objectOverrides, directOwnerVisible: directOwnerVisible,
			trustedEvidenceOwner: trustedOwner, wantResultHash: wantResultHash, wantResultError: wantResultError,
			wantError: wantError, missingHash: missingHash,
		}).snapshot()
		cases = append(cases, finalSemanticTestCase{name: name, verify: func(ctx context.Context) error {
			started := time.Now()
			err := verifyFinalPublicScenarioTestView(ctx, cfg, public, result.RunID, result.Name, semantic.PublicVerification.EvidenceURI, complete.ContentHash, caseBundleHashes, commitHashes, view)
			t.Logf("public replay %q completed in %s: %v", name, time.Since(started).Round(time.Millisecond), err)
			return err
		}})
	}
	const missingCompletion = "no scenario bundle has byte-identical operator signatures and a replicated owner completion commit"
	queueCase("direct owner envelope bypassed the per-operator completion commit", missingCompletion, "")
	directOwnerVisible = false
	commitVisible[1] = true
	queueCase("completion commit present at only one operator was accepted", missingCompletion, "")
	commitVisible[2] = true
	wantOwner := trustedOwner
	trustedOwner = common.HexToAddress(roles.EVM["operator-1-artifact"].Address)
	queueCase("self-declared owner signer bypassed the finalized coordinator owner", "scenario completion owner does not match the coordinator owner at the campaign terminal block", "")
	trustedOwner = wantOwner
	// The shared fixture is now a fully valid result. Remove one approved
	// assertion and re-sign this view so it reaches the real assertion check,
	// with all remaining assertions, counts and content hashes still valid.
	definition, err := scenarioDefinitionFor(cfg, result.Name)
	if err != nil || len(definition.Checks) == 0 {
		t.Fatalf("approved scenario checks are unavailable: %v", err)
	}
	missingAssertionId := definition.Checks[0].ID
	missingAssertionResult := *result
	missingAssertionResult.Assertions = make([]AssertionRecord, 0, len(result.Assertions)-1)
	for _, assertion := range result.Assertions {
		if assertion.ID != missingAssertionId {
			missingAssertionResult.Assertions = append(missingAssertionResult.Assertions, assertion)
		}
	}
	if len(missingAssertionResult.Assertions) != len(result.Assertions)-1 {
		t.Fatalf("approved assertion %q was not present exactly once", missingAssertionId)
	}
	missingAssertionResult.AssertionCount = len(missingAssertionResult.Assertions)
	missingAssertionResult.EvidenceHash, err = canonicalScenarioResultHash(&missingAssertionResult)
	if err != nil {
		t.Fatal(err)
	}
	missingAssertionBundle := bundle
	missingAssertionBundle.Result = &missingAssertionResult
	validBundleHashes := bundleHashes
	bundleHashes = make([]string, len(validBundleHashes))
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		envelope, err := signEvidence(cfg, "scenario-bundle", result.RunID, missingAssertionBundle, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		objects[envelope.ContentHash] = encoded
		bundleHashes[operator-1] = envelope.ContentHash
	}
	wantResultHash = missingAssertionResult.EvidenceHash
	wantResultError = "release campaign is missing approved assertion " + missingAssertionId
	queueCase("syntactic pass without the approved scenario assertions was accepted", missingCompletion, "")
	for _, hash := range bundleHashes {
		delete(objects, hash)
	}
	bundleHashes = validBundleHashes
	wantResultHash, wantResultError = result.EvidenceHash, ""
	manifestBytes := objects[archive.Manifest.ContentHash]
	delete(objects, archive.Manifest.ContentHash)
	queueCase("completion with a missing campaign manifest was accepted", "operator 1 campaign evidence "+archive.Manifest.ContentHash+": HTTP 404", archive.Manifest.ContentHash)
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
	queueCase("syntactic hash for a nonexistent result object was accepted", "operator 1 campaign evidence "+resultEnvelopeHash+": HTTP 404", resultEnvelopeHash)
	tamperedResultEnvelope := append([]byte(nil), resultEnvelopeBytes...)
	tamperedResultEnvelope[len(tamperedResultEnvelope)/2] ^= 1
	objects[resultEnvelopeHash] = tamperedResultEnvelope
	queueCase("tampered campaign evidence object was accepted", "operator 1 returned invalid campaign evidence "+resultEnvelopeHash, "")
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
	queueCase("completion with an unavailable referenced external receipt was accepted", "operator 1 campaign evidence "+referenceEnvelopeHash+": HTTP 404", referenceEnvelopeHash)
	tamperedReferenceEnvelope := append([]byte(nil), referenceEnvelopeBytes...)
	tamperedReferenceEnvelope[len(tamperedReferenceEnvelope)/2] ^= 1
	objects[referenceEnvelopeHash] = tamperedReferenceEnvelope
	queueCase("completion with a tampered referenced external receipt was accepted", "operator 1 returned invalid campaign evidence "+referenceEnvelopeHash, "")
	objects[referenceEnvelopeHash] = referenceEnvelopeBytes
	queueCase("closed campaign without a semantic supplement was accepted", "no semantic supplement is discoverable at every operator", "")
	supplementHistory[1] = []string{supplement.ContentHash}
	queueCase("semantic supplement visible at only one operator was accepted", "no semantic supplement is discoverable at every operator", "")
	supplementHistory[2] = []string{supplement.ContentHash}
	supplementBytes = objects[supplement.ContentHash]
	tamperedSupplement := append([]byte(nil), supplementBytes...)
	tamperedSupplement[len(tamperedSupplement)/2] ^= 1
	objectOverrides[2][supplement.ContentHash] = tamperedSupplement
	queueCase("semantic supplement differing between operator replicas was accepted", "operator 2 returned invalid campaign evidence "+supplement.ContentHash, "")
	delete(objectOverrides[2], supplement.ContentHash)
	semanticFileHash := semanticFileEntries[0].EnvelopeHash
	objectOverrides[2][semanticFileHash] = nil
	queueCase("semantic supplement with a file missing at one operator was accepted", "operator 2 campaign evidence "+semanticFileHash+": HTTP 404", semanticFileHash)
	tamperedSemanticFile := append([]byte(nil), objects[semanticFileHash]...)
	tamperedSemanticFile[len(tamperedSemanticFile)/2] ^= 1
	objectOverrides[2][semanticFileHash] = tamperedSemanticFile
	queueCase("semantic supplement with a tampered file replica was accepted", "operator 2 returned invalid campaign evidence "+semanticFileHash, "")
	delete(objectOverrides[2], semanticFileHash)
	uppercaseEntryPayload := supplementPayload
	uppercaseEntryPayload.Files = append([]FinalSemanticSupplementFile(nil), supplementPayload.Files...)
	uppercaseEntryPayload.Files[0].ContentHash = strings.ToUpper(uppercaseEntryPayload.Files[0].ContentHash)
	uppercaseEntrySupplement, err := signEvidence(cfg, finalSemanticSupplementKind, result.RunID, uppercaseEntryPayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	uppercaseEntryBytes, err := json.Marshal(uppercaseEntrySupplement)
	if err != nil {
		t.Fatal(err)
	}
	objects[uppercaseEntrySupplement.ContentHash] = uppercaseEntryBytes
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		supplementHistory[operator] = []string{uppercaseEntrySupplement.ContentHash}
	}
	queueCase("semantic supplement with non-exact file content hash casing was accepted", fmt.Sprintf("semantic supplement file %q payload differs from its manifest", supplementPayload.Files[0].Path), "")
	ambiguousFilePayload := supplementPayload
	ambiguousFilePayload.Files = append(append([]FinalSemanticSupplementFile(nil), supplementPayload.Files...), supplementPayload.Files[0])
	ambiguousFileSupplement, err := signEvidence(cfg, finalSemanticSupplementKind, result.RunID, ambiguousFilePayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	ambiguousFileBytes, err := json.Marshal(ambiguousFileSupplement)
	if err != nil {
		t.Fatal(err)
	}
	objects[ambiguousFileSupplement.ContentHash] = ambiguousFileBytes
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		supplementHistory[operator] = []string{ambiguousFileSupplement.ContentHash}
	}
	queueCase("semantic supplement with an ambiguous file census was accepted", "semantic supplement file manifest is invalid", "")
	alternatePayload := supplementPayload
	alternatePayload.Files = append([]FinalSemanticSupplementFile(nil), supplementPayload.Files...)
	alternatePayload.ScenarioCompleteHash = strings.ToUpper(alternatePayload.ScenarioCompleteHash)
	alternate, err := signEvidence(cfg, finalSemanticSupplementKind, result.RunID, alternatePayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	alternateBytes, err := json.Marshal(alternate)
	if err != nil {
		t.Fatal(err)
	}
	objects[alternate.ContentHash] = alternateBytes
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		supplementHistory[operator] = []string{supplement.ContentHash, alternate.ContentHash}
	}
	queueCase("multiple fully replicated semantic supplements were accepted", "public campaign has multiple fully replicated semantic supplements for one owner completion", "")
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		supplementHistory[operator] = []string{supplement.ContentHash}
	}
	queueCase("replicated committed scenario bundle", "", "")
	if len(cases) != 18 {
		t.Fatalf("public replay case census = %d, want every 17 rejection cases and the accepted graph", len(cases))
	}
	logSetupPhase("all eighteen detached replay views")
	for index, err := range runFinalSemanticTestCases(context.Background(), cases) {
		if err != nil {
			t.Errorf("%s: %v", cases[index].name, err)
		}
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
