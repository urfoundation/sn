package main

import (
	"context"
	"encoding/json"
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
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
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
	if first.EVMRPC == "" || first.SubstrateRPC == "" {
		t.Fatalf("public RPC endpoints missing: %+v", first)
	}
	wantEvidence := 1 + cfg.Config.Topology.HeadFleets*(2+cfg.Config.Topology.ClientsPerHeadFleet)
	if len(first.SetupEvidence) != wantEvidence || first.Operators[0].APIURL != "https://no1.example" || strings.Contains(first.Operators[0].APIURL, "127.0.0.1") {
		t.Fatalf("manifest is not independently reachable/complete: %+v", first)
	}
	for name, command := range first.Commands {
		if strings.Contains(command, "/machine-specific") || strings.Contains(command, dir) {
			t.Fatalf("command %s is host-specific: %s", name, command)
		}
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
