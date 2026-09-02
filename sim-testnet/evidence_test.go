package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestReleaseEvidenceSignVerifyAndTamper(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	role := roles.EVM["operator-1-artifact"]
	envelope, err := signEvidence(cfg, "unit", "run-1", map[string]any{"answer": 42}, role)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.HexToECDSA(role.PrivateKeyHex)
	if err := verifyEvidence(envelope, &key.PublicKey); err != nil {
		t.Fatal(err)
	}
	tampered := *envelope
	tampered.Payload = json.RawMessage(`{"answer":43}`)
	if err := verifyEvidence(&tampered, &key.PublicKey); err == nil {
		t.Fatal("tampered evidence verified")
	}
	if !strings.HasPrefix(envelope.ContentHash, "sha256:") || envelope.Signer != common.HexToAddress(role.Address) {
		t.Fatalf("envelope identity = %+v", envelope)
	}
}

func TestLocalEvidenceRetainsVersionedDeploymentManifestHistory(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	role := roles.EVM["operator-1-artifact"]
	dir := t.TempDir()
	path := filepath.Join(dir, "public", "deployment-manifest.operator-1.evidence.json")
	first, _, err := prepareLocalEvidence(cfg, dir, path, "deployment-manifest", "", map[string]any{"revision": 1}, role, 1)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	identical, _, err := prepareLocalEvidence(cfg, dir, path, "deployment-manifest", "", map[string]any{"revision": 1}, role, 1)
	if err != nil {
		t.Fatal(err)
	}
	if identical.ContentHash != first.ContentHash {
		t.Fatal("idempotent deployment evidence was re-signed")
	}
	second, _, err := prepareLocalEvidence(cfg, dir, path, "deployment-manifest", "", map[string]any{"revision": 2}, role, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.ContentHash == first.ContentHash {
		t.Fatal("revised deployment evidence retained the old content hash")
	}
	archive := filepath.Join(dir, "public", "deployment-manifest-history", strings.TrimPrefix(first.ContentHash, "sha256:")+".operator-1.evidence.json")
	archived, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if string(archived) != string(firstBytes) {
		t.Fatal("superseded signed deployment evidence was not retained byte-for-byte")
	}
	currentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
	if err := verifyEvidence(second, &key.PublicKey); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareLocalEvidence(cfg, dir, path, "deployment-manifest", "", map[string]any{"revision": 2}, role, 1); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(currentBytes) {
		t.Fatal("idempotent revised deployment evidence changed bytes")
	}

	ordinaryPath := filepath.Join(dir, "runs", "run-1", "scenario.operator-1.evidence.json")
	if _, _, err := prepareLocalEvidence(cfg, dir, ordinaryPath, "scenario-bundle", "run-1", map[string]any{"value": 1}, role, 1); err != nil {
		t.Fatal(err)
	}
	ordinaryBefore, _ := os.ReadFile(ordinaryPath)
	if _, _, err := prepareLocalEvidence(cfg, dir, ordinaryPath, "scenario-bundle", "run-1", map[string]any{"value": 2}, role, 1); err == nil || !strings.Contains(err.Error(), "immutable local evidence") {
		t.Fatalf("ordinary evidence mutation was accepted: %v", err)
	}
	ordinaryAfter, _ := os.ReadFile(ordinaryPath)
	if string(ordinaryAfter) != string(ordinaryBefore) {
		t.Fatal("rejected ordinary evidence mutation changed the current file")
	}
}

func TestVerifyPublishedEvidenceOriginChecksContentSignerAndHistory(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := signEvidence(cfg, "deployment-manifest", "", map[string]any{"release": "1.0"}, roles.EVM["operator-1-artifact"])
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(envelope)
	historyHasObject := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sn/evidence":
			_, _ = w.Write(encoded)
		case "/sn/evidence/history":
			key := "different.json"
			if historyHasObject {
				key = "history/" + strings.TrimPrefix(envelope.ContentHash, "sha256:") + ".json"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": "urnetwork-release-evidence-history-v1", "objects": []map[string]string{{"key": key}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	published := PublishedEvidence{ContentHash: envelope.ContentHash}
	if err := verifyPublishedEvidenceOrigin(context.Background(), cfg, roles, 1, server.URL, published); err != nil {
		t.Fatal(err)
	}
	historyHasObject = false
	if err := verifyPublishedEvidenceOrigin(context.Background(), cfg, roles, 1, server.URL, published); err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("missing public history object was accepted: %v", err)
	}
	tampered := append([]byte(nil), encoded...)
	tampered[len(tampered)-2] ^= 1
	encoded = tampered
	if err := verifyPublishedEvidenceOrigin(context.Background(), cfg, roles, 1, server.URL, published); err == nil {
		t.Fatal("tampered public evidence was accepted")
	}
}

func TestPublicDeploymentManifestIsPortableAndIdempotent(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.ConfigPath = "/machine-specific/repo/sim-testnet/testnet.yml"
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.OperationalSubstrate = "wss://test.substrate.example"
	cfg.OperationalEVM = "https://test.chain.example"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.example"
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://test.substrate.example"
	dir := t.TempDir()
	deployment := ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, ReserveSink: common.HexToAddress("0x0000000000000000000000000000000000000011"), SettlementVault: common.HexToAddress("0x0000000000000000000000000000000000000022"), CoordinatorImplementation: common.HexToAddress("0x0000000000000000000000000000000000000033"), CoordinatorProxy: common.HexToAddress("0x0000000000000000000000000000000000000044"), RuntimeHashes: map[string]string{}}
	if err := saveContractDeployment(dir, deployment); err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	identities, _ := json.Marshal(roles.Public())
	if err := atomicWrite(filepath.Join(dir, "public", "identities.json"), identities, 0o644); err != nil {
		t.Fatal(err)
	}
	names := []string{"voluntary-conviction.json"}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		names = append(names, fmt.Sprintf("fleet-%d.json", fleet), fmt.Sprintf("fleet-%d.commitment.json", fleet))
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			names = append(names, fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
		}
	}
	for _, name := range names {
		if err := atomicWrite(filepath.Join(dir, "public", name), []byte("{\"evidence\":true}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("12", 32)}
	first, err := writePublicDeploymentManifest(cfg, dir, plan)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "public.json"))
	second, err := writePublicDeploymentManifest(cfg, dir, plan)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "public.json"))
	if first.GeneratedAt != second.GeneratedAt || string(before) != string(after) {
		t.Fatal("idempotent manifest changed bytes")
	}
	if first.Revision != 1 || first.PreviousManifestHash != "" {
		t.Fatalf("initial manifest revision = %d predecessor = %q", first.Revision, first.PreviousManifestHash)
	}
	if first.EVMRPC == "" || first.SubstrateRPC == "" {
		t.Fatalf("public RPC endpoints missing: %+v", first)
	}
	if first.OperationalRPCMode != rpcModePublicOverride || first.IndependentRPC || first.OperationalEVMRPC != cfg.OperationalEVM || first.OperationalSubstrateRPC != cfg.OperationalSubstrate {
		t.Fatalf("public override assurance was overstated in manifest: %+v", first)
	}
	wantEvidence := 1 + cfg.Config.Topology.fleetCandidates()*(2+cfg.Config.Topology.ClientsPerHeadFleet)
	if len(first.SetupEvidence) != wantEvidence || first.Operators[0].APIURL != "https://no1.example" || strings.Contains(first.Operators[0].APIURL, "127.0.0.1") {
		t.Fatalf("manifest is not independently reachable/complete: %+v", first)
	}
	for name, command := range first.Commands {
		if strings.Contains(command, "/machine-specific") || strings.Contains(command, dir) {
			t.Fatalf("command %s is host-specific: %s", name, command)
		}
	}
	// Exercise the deployed pre-revision encoding: revision zero must hash as
	// the exact legacy schema, without synthesizing a JSON revision field.
	legacyBefore := bytes.Replace(before, []byte("  \"revision\": 1,\n"), nil, 1)
	if bytes.Equal(legacyBefore, before) {
		t.Fatal("test fixture did not remove the revision field")
	}
	if err := atomicWrite(filepath.Join(dir, "public.json"), legacyBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	var legacy PublicDeploymentManifest
	if err := json.Unmarshal(legacyBefore, &legacy); err != nil {
		t.Fatal(err)
	}
	firstHash, err := canonicalHashHex(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	locatorBytes := []byte("{\"schema\":\"urnetwork-public-manifest-locators-v1\",\"locators\":[]}\n")
	if err := atomicWrite(filepath.Join(dir, "public", "deployment-manifest.locators.json"), locatorBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	oldPlanHash := plan.PlanHash
	cfg.ConfigHash = "0x" + strings.Repeat("34", 32)
	cfg.PolicyHash = "0x" + strings.Repeat("56", 32)
	cfg.Release.Dependencies["redis"] = "redis:8-alpine@sha256:" + strings.Repeat("7", 64)
	plan.PlanHash = "0x" + strings.Repeat("78", 32)
	plan.PriorPlanHashes = []string{oldPlanHash}
	revised, err := writePublicDeploymentManifest(cfg, dir, plan)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Revision != 2 || revised.PreviousManifestHash != firstHash || revised.ConfigHash != cfg.ConfigHash || revised.PolicyHash != cfg.PolicyHash || revised.PlanHash != plan.PlanHash {
		t.Fatalf("revised manifest linkage = %+v", revised)
	}
	archivePath := filepath.Join(dir, "public", "deployment-manifests", stringsTrim0x(firstHash)+".json")
	archived, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(archived) != string(legacyBefore) {
		t.Fatal("superseded public manifest was not retained byte-for-byte")
	}
	var archivedManifest PublicDeploymentManifest
	if err := json.Unmarshal(archived, &archivedManifest); err != nil {
		t.Fatal(err)
	}
	archivedHash, err := canonicalHashHex(&archivedManifest)
	if err != nil || archivedHash != revised.PreviousManifestHash {
		t.Fatalf("archived legacy predecessor hash = %s, want %s: %v", archivedHash, revised.PreviousManifestHash, err)
	}
	locatorArchive := filepath.Join(dir, "public", "deployment-manifests", stringsTrim0x(firstHash)+".locators.json")
	archivedLocators, err := os.ReadFile(locatorArchive)
	if err != nil || !bytes.Equal(archivedLocators, locatorBytes) {
		t.Fatalf("archived deployment locators = %q, %v", archivedLocators, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "public", "deployment-manifest.locators.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded active locators survived revision: %v", err)
	}
	revisedBytes, _ := os.ReadFile(filepath.Join(dir, "public.json"))
	again, err := writePublicDeploymentManifest(cfg, dir, plan)
	if err != nil {
		t.Fatal(err)
	}
	againBytes, _ := os.ReadFile(filepath.Join(dir, "public.json"))
	if again.Revision != revised.Revision || string(revisedBytes) != string(againBytes) {
		t.Fatal("idempotent revised manifest changed bytes")
	}
	if err := atomicWrite(archivePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writePublicDeploymentManifest(cfg, dir, plan); err == nil || !strings.Contains(err.Error(), "predecessor revision") {
		t.Fatalf("tampered manifest history was accepted: %v", err)
	}
	if err := atomicWrite(archivePath, archived, 0o644); err != nil {
		t.Fatal(err)
	}
	revisionConfigHash := cfg.ConfigHash
	cfg.ConfigHash = "0x" + strings.Repeat("9a", 32)
	if _, err := writePublicDeploymentManifest(cfg, dir, plan); err == nil || !strings.Contains(err.Error(), "without a new setup plan") {
		t.Fatalf("same-plan manifest mutation was accepted: %v", err)
	}
	unchanged, _ := os.ReadFile(filepath.Join(dir, "public.json"))
	if !bytes.Equal(unchanged, revisedBytes) {
		t.Fatal("rejected same-plan mutation changed public.json")
	}
	plan.PlanHash = "0x" + strings.Repeat("ab", 32)
	plan.PriorPlanHashes = []string{oldPlanHash}
	if _, err := writePublicDeploymentManifest(cfg, dir, plan); err == nil || !strings.Contains(err.Error(), "outside the approved revision lineage") {
		t.Fatalf("unrelated manifest revision was accepted: %v", err)
	}
	unchanged, _ = os.ReadFile(filepath.Join(dir, "public.json"))
	if !bytes.Equal(unchanged, revisedBytes) {
		t.Fatal("rejected unrelated revision changed public.json")
	}
	cfg.ConfigHash = revisionConfigHash
	plan.PlanHash = revised.PlanHash
	plan.PriorPlanHashes = []string{oldPlanHash}
	cfg.Netuid++
	if _, err := writePublicDeploymentManifest(cfg, dir, plan); err == nil || !strings.Contains(err.Error(), "does not match this deployment") {
		t.Fatalf("immutable deployment identity mutation was accepted: %v", err)
	}
}

func TestEvidenceFileHashesExcludeCompletionMarker(t *testing.T) {
	dir := t.TempDir()
	if err := atomicWrite(filepath.Join(dir, "result.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "complete.json"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hashes, err := evidenceFileHashes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hashes["result.json"] == "" || hashes["complete.json"] != "" {
		t.Fatalf("hash index = %v", hashes)
	}
}
