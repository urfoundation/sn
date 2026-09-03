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
	"github.com/urnetwork/server"
	"github.com/urnetwork/server/startifact"
)

type forbiddenScenarioCommitSupervisedAPITransport struct {
	calls int
}

func (transport *forbiddenScenarioCommitSupervisedAPITransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return nil, errors.New("scenario completion commit attempted the supervised API")
}

type failingPutBlobStore struct {
	server.BlobStore
	err error
}

func (store *failingPutBlobStore) Put(context.Context, string, string, string) error {
	return store.err
}

func scenarioCompletionTestEnvelope(t *testing.T, cfg *ResolvedConfig, roles *RoleSecrets, runID string) (*ReleaseEvidenceEnvelope, []byte) {
	t.Helper()
	complete, err := signEvidence(cfg, "scenario-complete", runID, scenarioCompletePayload{
		ResultHash:           "0x" + strings.Repeat("11", 32),
		Files:                map[string]string{"result.json": "sha256:" + strings.Repeat("22", 32)},
		BundlePayloadHash:    "sha256:" + strings.Repeat("33", 32),
		EvidenceManifestHash: "sha256:" + strings.Repeat("44", 32),
	}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(complete, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return complete, append(encoded, '\n')
}

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

func TestRenderedOperatorEvidenceStoreUsesExactIsolatedPrefix(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Artifacts.MinioPrefix = "blob/sim-testnet-light/${deployment_id}"
	stateDir := t.TempDir()
	vaultDir := filepath.Join(stateDir, "runtime", "operator-1", "vault")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	prefix, err := operatorArtifactPrefix(cfg.Config, 1)
	if err != nil {
		t.Fatal(err)
	}
	config := []byte("authority: \"{{ env:BRINGYOUR_MINIO_HOSTNAME }}:23900\"\ntls: false\nbucket: blob\naccess_key: test-access\nsecret_key: test-secret\nprefix: " + prefix + "\n")
	path := filepath.Join(vaultDir, "minio.yml")
	if err := atomicWrite(path, config, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := renderedOperatorEvidenceStore(cfg, stateDir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if store.Authority() != cfg.ObjectStoreHost+":23900" || store.Bucket() != "blob" || store.Prefix() != prefix {
		t.Fatalf("direct operator store = authority %q bucket %q prefix %q", store.Authority(), store.Bucket(), store.Prefix())
	}
	config = []byte("authority: \"{{ env:BRINGYOUR_MINIO_HOSTNAME }}:23900\"\ntls: false\nbucket: blob\naccess_key: test-access\nsecret_key: test-secret\nprefix: blob/sim-testnet/wrong/operator-1\n")
	if err := atomicWrite(path, config, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := renderedOperatorEvidenceStore(cfg, stateDir, 1); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Fatalf("wrong rendered operator prefix was accepted: %v", err)
	}
}

func TestScenarioCompletionCommitsUseDirectStoresWithoutSupervisedAPIHTTP(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	// Production soak rotates this authenticated runtime input after setup. The
	// completion boundary must reauthenticate MinIO without rejecting that
	// approved live transition.
	verifyPath := filepath.Join(stateDir, "runtime", "operator-1", "vault", "verify.yml")
	if err := atomicWrite(verifyPath, []byte("approved live verify-key rotation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "direct-completion-success"
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	complete, encoded := scenarioCompletionTestEnvelope(t, cfg, roles, runID)
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(filepath.Join(stateDir, "object-store"), prefix)
	}
	transport := &forbiddenScenarioCommitSupervisedAPITransport{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	failureID, err := commitPublishedScenarioCompletion(context.Background(), cfg, roles, stateDir, runID, complete, encoded, func(operator int) (server.BlobStore, error) {
		return stores[operator], nil
	})
	if err != nil || failureID != "" {
		t.Fatalf("direct scenario completion commit = stage %q error %v", failureID, err)
	}
	if transport.calls != 0 {
		t.Fatalf("scenario completion commit made %d supervised API requests", transport.calls)
	}
	written, err := os.ReadFile(filepath.Join(runDir, "complete.json"))
	if err != nil || !bytes.Equal(written, encoded) {
		t.Fatalf("local completion marker = %q, %v", written, err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		var commit ReleaseEvidenceEnvelope
		if err := decodeStrictJSONFile(filepath.Join(runDir, fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator)), &commit); err != nil {
			t.Fatal(err)
		}
		prefix, err := startifact.EvidenceHistoryPrefix(stores[operator], cfg.Config.Deployment.DeploymentID, cfg.Netuid, "scenario-complete-commit")
		if err != nil {
			t.Fatal(err)
		}
		objects, err := stores[operator].List(context.Background(), prefix)
		if err != nil || len(objects) != 1 || !strings.Contains(objects[0].Key, strings.TrimPrefix(commit.ContentHash, "sha256:")) {
			t.Fatalf("operator %d direct completion history = %+v, %v", operator, objects, err)
		}
	}
}

func TestCampaignEvidenceArchivePublishesEveryRawFileThroughDirectStores(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "direct-campaign-archive"
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rawFiles := map[string][]byte{
		"result.json":      []byte(`{"schema":"urnetwork-sim-scenario-result-v1"}` + "\n"),
		"nested/proof.bin": {0x00, 0x01, 0x02, 0xff},
	}
	hashes := make(map[string]string, len(rawFiles))
	for name, raw := range rawFiles {
		if err := atomicWrite(filepath.Join(runDir, filepath.FromSlash(name)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		hashes[name] = bytesSHA256(raw)
	}
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(filepath.Join(stateDir, "object-store"), prefix)
	}
	transport := &forbiddenScenarioCommitSupervisedAPITransport{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	manifestEnvelope, err := publishCampaignEvidenceArchive(context.Background(), cfg, roles, stateDir, runID, "0x"+strings.Repeat("11", 32), "sha256:"+strings.Repeat("22", 32), hashes, func(operator int) (server.BlobStore, error) {
		return stores[operator], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if transport.calls != 0 {
		t.Fatalf("campaign evidence archive made %d supervised API requests", transport.calls)
	}
	manifest, err := decodeCampaignEvidenceManifest(manifestEnvelope)
	if err != nil || len(manifest.Files) != len(rawFiles) {
		t.Fatalf("campaign evidence manifest = %+v, %v", manifest, err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		for _, hash := range append([]string{manifestEnvelope.ContentHash}, manifestFileEnvelopeHashes(manifest.Files)...) {
			key, err := startifact.EvidenceContentKey(stores[operator], hash)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := stores[operator].Get(context.Background(), key)
			if err != nil {
				t.Fatalf("operator %d missing campaign object %s: %v", operator, hash, err)
			}
			_ = reader.Close()
		}
	}
}

func manifestFileEnvelopeHashes(entries []campaignEvidenceFileEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.EnvelopeHash)
	}
	return result
}

func TestCampaignEvidenceArchiveFailureCannotExposeLocalCompletion(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "failed-campaign-archive"
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema":"urnetwork-sim-scenario-result-v1"}` + "\n")
	if err := atomicWrite(filepath.Join(runDir, "result.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(filepath.Join(stateDir, "object-store"), prefix)
	}
	stores[2] = &failingPutBlobStore{BlobStore: stores[2], err: errors.New("injected archive-store failure")}
	_, err = publishCampaignEvidenceArchive(context.Background(), cfg, roles, stateDir, runID, "0x"+strings.Repeat("11", 32), "sha256:"+strings.Repeat("22", 32), map[string]string{"result.json": bytesSHA256(raw)}, func(operator int) (server.BlobStore, error) {
		return stores[operator], nil
	})
	if err == nil || !strings.Contains(err.Error(), "injected archive-store failure") {
		t.Fatalf("campaign evidence archive failure = %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed campaign archive exposed local completion: %v", err)
	}
}

func TestCampaignEvidenceManifestRejectsUnsafeAndDuplicatePaths(t *testing.T) {
	hash := "sha256:" + strings.Repeat("11", 32)
	for _, name := range []string{"../result.json", "/result.json", "nested/../result.json", `nested\result.json`, "complete.json", campaignEvidenceManifestFilename, "scenario-complete-commit.operator-1.evidence.json"} {
		if err := validateCampaignEvidencePath(name); err == nil {
			t.Errorf("unsafe or reserved campaign evidence path %q was accepted", name)
		}
	}
	entries := []campaignEvidenceFileEntry{
		{Path: "result.json", ContentHash: hash, EnvelopeHash: hash},
		{Path: "result.json", ContentHash: hash, EnvelopeHash: hash},
	}
	if _, err := campaignEvidenceManifestFiles(entries); err == nil {
		t.Fatal("duplicate campaign evidence path was accepted")
	}
}

func TestCampaignArtifactLocatorAllowsOnlyExactContentHashQuery(t *testing.T) {
	hash := "sha256:" + strings.Repeat("11", 32)
	for _, uri := range []string{
		"https://operator.example/sn/artifact?hash=" + hash,
		"https://operator.example/sn/artifact?hash=sha256%3A" + strings.Repeat("11", 32),
	} {
		raw, err := json.Marshal(FinalArtifactLocator{Kind: "proof", URI: uri, ContentHash: hash, SizeBytes: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := campaignArtifactReferences(map[string][]byte{"locator.json": raw}); err != nil {
			t.Errorf("exact content-addressed locator %q rejected: %v", uri, err)
		}
	}
	for _, uri := range []string{
		"https://operator.example/sn/artifact?token=secret",
		"https://operator.example/sn/artifact?hash=sha256:" + strings.Repeat("22", 32),
		"https://operator.example/sn/artifact?hash=" + hash + "&extra=1",
	} {
		raw, err := json.Marshal(FinalArtifactLocator{Kind: "proof", URI: uri, ContentHash: hash, SizeBytes: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := campaignArtifactReferences(map[string][]byte{"locator.json": raw}); err == nil {
			t.Errorf("noncanonical locator query %q was accepted", uri)
		}
	}
}

func TestCampaignEvidenceGraphHardBoundsAndCycles(t *testing.T) {
	hash := "sha256:" + strings.Repeat("11", 32)
	t.Run("object count", func(t *testing.T) {
		entries := make([]campaignEvidenceFileEntry, maximumCampaignEvidenceObjects+1)
		for index := range entries {
			entries[index] = campaignEvidenceFileEntry{Path: fmt.Sprintf("objects/%05d.bin", index), ContentHash: hash, EnvelopeHash: hash}
		}
		if _, err := campaignEvidenceEntryFiles(entries); err == nil || !strings.Contains(err.Error(), "maximum") {
			t.Fatalf("oversized object set = %v", err)
		}
	})
	t.Run("aggregate bytes", func(t *testing.T) {
		entries := make([]campaignEvidenceFileEntry, maximumCampaignEvidenceAggregateBytes/maximumCampaignEvidenceRawFileBytes+1)
		for index := range entries {
			entries[index] = campaignEvidenceFileEntry{Path: fmt.Sprintf("objects/%05d.bin", index), ContentHash: hash, EnvelopeHash: hash, Size: maximumCampaignEvidenceRawFileBytes}
		}
		if _, err := campaignEvidenceEntryFiles(entries); err == nil || !strings.Contains(err.Error(), "aggregate") {
			t.Fatalf("oversized aggregate = %v", err)
		}
	})
	t.Run("JSON depth", func(t *testing.T) {
		raw := []byte(strings.Repeat("[", maximumCampaignEvidenceJSONDepth+2) + "0" + strings.Repeat("]", maximumCampaignEvidenceJSONDepth+2))
		if _, err := campaignArtifactReferences(map[string][]byte{"deep.json": raw}); err == nil || !strings.Contains(err.Error(), "maximum depth") {
			t.Fatalf("deep JSON graph = %v", err)
		}
	})
	t.Run("reference depth", func(t *testing.T) {
		edges := map[string]map[string]bool{}
		for index := 0; index <= maximumCampaignEvidenceJSONDepth; index++ {
			edges[fmt.Sprintf("node-%d", index)] = map[string]bool{fmt.Sprintf("node-%d", index+1): true}
		}
		if err := validateCampaignArtifactGraph(edges); err == nil || !strings.Contains(err.Error(), "maximum depth") {
			t.Fatalf("deep reference graph = %v", err)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		edges := map[string]map[string]bool{"a": {"b": true}, "b": {"a": true}}
		if err := validateCampaignArtifactGraph(edges); err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("cyclic reference graph = %v", err)
		}
	})
	t.Run("locator count", func(t *testing.T) {
		references := make(map[string]campaignArtifactReference, maximumCampaignEvidenceObjects)
		for index := 0; index < maximumCampaignEvidenceObjects; index++ {
			references[fmt.Sprintf("objects/%05d.bin", index)] = campaignArtifactReference{Kind: "proof", URI: fmt.Sprintf("objects/%05d.bin", index), ContentHash: hash, Size: 1}
		}
		addition := map[string]campaignArtifactReference{"objects/overflow.bin": {Kind: "proof", URI: "objects/overflow.bin", ContentHash: hash, Size: 1}}
		if err := mergeCampaignArtifactReferences(references, addition); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversized locator set = %v", err)
		}
	})
	t.Run("combined root and locator count", func(t *testing.T) {
		references := make(map[string]campaignArtifactReference, maximumCampaignEvidenceObjects)
		for index := 0; index < maximumCampaignEvidenceObjects; index++ {
			uri := fmt.Sprintf("objects/%05d.bin", index)
			references[uri] = campaignArtifactReference{Kind: "proof", URI: uri, ContentHash: hash, Size: 1}
		}
		if err := validateCampaignArtifactObjectCount(1, references); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversized combined graph = %v", err)
		}
	})
}

func TestCampaignEvidenceReadCannotFollowSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := atomicWrite(filepath.Join(outside, "secret.json"), []byte(`{"schema":"outside"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "public")); err != nil {
		t.Fatal(err)
	}
	if _, err := readCampaignEvidenceRegularFile(root, "public/secret.json"); err == nil {
		t.Fatal("campaign evidence reader followed a symlink outside its authenticated root")
	}
}

func TestDirectScenarioCompletionFailureLeavesNoLocalComplete(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "direct-completion-failure"
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	complete, encoded := scenarioCompletionTestEnvelope(t, cfg, roles, runID)
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(filepath.Join(stateDir, "object-store"), prefix)
	}
	stores[2] = &failingPutBlobStore{BlobStore: stores[2], err: errors.New("injected direct-store failure")}
	failureID, err := commitPublishedScenarioCompletion(context.Background(), cfg, roles, stateDir, runID, complete, encoded, func(operator int) (server.BlobStore, error) {
		return stores[operator], nil
	})
	if err == nil || failureID != "complete_evidence_publication" || !strings.Contains(err.Error(), "injected direct-store failure") {
		t.Fatalf("direct-store failure = stage %q error %v", failureID, err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed direct publication exposed a local complete marker: %v", err)
	}
	prefix, err := operatorArtifactPrefix(cfg.Config, 2)
	if err != nil {
		t.Fatal(err)
	}
	stores[2] = server.NewLocalBlobStore(filepath.Join(stateDir, "object-store"), prefix)
	failureID, err = commitPublishedScenarioCompletion(context.Background(), cfg, roles, stateDir, runID, complete, encoded, func(operator int) (server.BlobStore, error) {
		return stores[operator], nil
	})
	if err != nil || failureID != "" {
		t.Fatalf("direct publication retry = stage %q error %v", failureID, err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); err != nil {
		t.Fatalf("successful direct publication retry did not expose local completion: %v", err)
	}
}

func TestScenarioCompletionRejectsTamperedRenderedConfigBeforeOpeningStores(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "tampered-completion-config"
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	complete, encoded := scenarioCompletionTestEnvelope(t, cfg, roles, runID)
	path := filepath.Join(stateDir, "runtime", "operator-1", "vault", "minio.yml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, append(contents, []byte("tampered: true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	storeCalls := 0
	failureID, err := commitPublishedScenarioCompletion(context.Background(), cfg, roles, stateDir, runID, complete, encoded, func(int) (server.BlobStore, error) {
		storeCalls++
		return nil, errors.New("store factory must not be called")
	})
	if err == nil || failureID != "complete_evidence_publication" || !strings.Contains(err.Error(), "differs from its manifest") {
		t.Fatalf("tampered runtime config = stage %q error %v", failureID, err)
	}
	if storeCalls != 0 {
		t.Fatalf("tampered runtime config opened %d stores", storeCalls)
	}
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tampered runtime config exposed local completion: %v", err)
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

// Builds the post-provisioning identity and envelope state shared by archival
// interruption tests.
func deploymentPublicationArchiveFixture(t *testing.T) (*PublicDeploymentManifest, *ReleaseEvidenceEnvelope, []byte) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for label, role := range roles.Clients {
		role.ClientIDHex = strings.Repeat("02", 16)
		roles.Clients[label] = role
	}
	identities, err := json.Marshal(roles.Public())
	if err != nil {
		t.Fatal(err)
	}
	prior := &PublicDeploymentManifest{
		Schema:              "urnetwork-sim-public-deployment-v1",
		Release:             "1.0",
		DeploymentID:        cfg.Config.Deployment.DeploymentID,
		Revision:            1,
		ChainID:             cfg.ChainID,
		GenesisHash:         cfg.Public.Chain.GenesisHash,
		RuntimeSpec:         cfg.Public.Chain.ExpectedRuntimeSpec,
		TransactionVersion:  cfg.Public.Chain.ExpectedTransactionVersion,
		StateVersion:        cfg.Public.Chain.ExpectedStateVersion,
		RuntimeCodeHash:     cfg.Release.Runtime.CodeHash,
		RuntimeMetadataHash: cfg.Release.Runtime.MetadataHash,
		Netuid:              cfg.Netuid,
		Identities:          identities,
		Topology:            cfg.Config.Topology,
		Operators: []PublicOperator{
			{NoID: 1, APIURL: cfg.OperatorAPIOrigins[0]},
			{NoID: 2, APIURL: cfg.OperatorAPIOrigins[1]},
		},
	}
	envelope, err := signEvidence(cfg, "deployment-manifest", "", prior, roles.EVM["operator-1-artifact"])
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	return prior, envelope, encoded
}

func deploymentPublicationEnvelope(t *testing.T, cfg *ResolvedConfig, roles *RoleSecrets, prior *PublicDeploymentManifest, operator int) (*ReleaseEvidenceEnvelope, []byte) {
	t.Helper()
	envelope, err := signEvidence(cfg, "deployment-manifest", "", prior, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
	if err != nil {
		t.Fatal(err)
	}
	exact, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return envelope, append(exact, '\n')
}

func completeDeploymentLocatorFixture(t *testing.T, prior *PublicDeploymentManifest) (*ResolvedConfig, *RoleSecrets, string, map[int]*ReleaseEvidenceEnvelope, map[int][]byte, []byte) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	priorHash, err := canonicalHashHex(prior)
	if err != nil {
		t.Fatal(err)
	}
	envelopes := make(map[int]*ReleaseEvidenceEnvelope, prior.Topology.Operators)
	exact := make(map[int][]byte, prior.Topology.Operators)
	directory := deploymentManifestLocatorDirectory{
		Schema: "urnetwork-public-manifest-locators-v1", ManifestHash: priorHash,
		ManifestRevision: effectivePublicManifestRevision(prior), PreviousManifestHash: prior.PreviousManifestHash,
	}
	for operator := 1; operator <= prior.Topology.Operators; operator++ {
		envelopes[operator], exact[operator] = deploymentPublicationEnvelope(t, cfg, roles, prior, operator)
		directory.Locators = append(directory.Locators, deploymentManifestLocator{
			OperatorNoID: operator, ContentHash: envelopes[operator].ContentHash,
			URL: strings.TrimSuffix(prior.Operators[operator-1].APIURL, "/") + "/sn/evidence?hash=" + envelopes[operator].ContentHash,
		})
	}
	locatorBytes, err := json.Marshal(directory)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, roles, priorHash, envelopes, exact, append(locatorBytes, '\n')
}

func TestArchiveCurrentDeploymentPublicationRecoversPartialUnlocatedSet(t *testing.T) {
	prior, envelope, encoded := deploymentPublicationArchiveFixture(t)
	dir := t.TempDir()
	active := filepath.Join(dir, "public", "deployment-manifest.operator-1.evidence.json")
	if err := atomicWrite(active, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	priorHash, err := canonicalHashHex(prior)
	if err != nil {
		t.Fatal(err)
	}
	if err := archiveCurrentDeploymentPublication(dir, prior, priorHash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial active envelope survived archival: %v", err)
	}
	archive := filepath.Join(dir, "public", "deployment-manifest-history", strings.TrimPrefix(envelope.ContentHash, "sha256:")+".operator-1.evidence.json")
	got, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(got, encoded) {
		t.Fatalf("partial publication archive mismatch: %v", err)
	}
}

func TestArchiveCurrentDeploymentPublicationRejectsPartialLocatedSet(t *testing.T) {
	prior, envelope, encoded := deploymentPublicationArchiveFixture(t)
	dir := t.TempDir()
	active := filepath.Join(dir, "public", "deployment-manifest.operator-1.evidence.json")
	if err := atomicWrite(active, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	priorHash, err := canonicalHashHex(prior)
	if err != nil {
		t.Fatal(err)
	}
	locators, err := json.Marshal(map[string]any{
		"schema":            "urnetwork-public-manifest-locators-v1",
		"manifest_hash":     priorHash,
		"manifest_revision": uint64(1),
		"locators": []map[string]any{{
			"operator_no_id": 1,
			"content_hash":   envelope.ContentHash,
			"url":            strings.TrimSuffix(prior.Operators[0].APIURL, "/") + "/sn/evidence?hash=" + envelope.ContentHash,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	locatorPath := filepath.Join(dir, "public", "deployment-manifest.locators.json")
	if err := atomicWrite(locatorPath, append(locators, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := archiveCurrentDeploymentPublication(dir, prior, priorHash); err == nil || !strings.Contains(err.Error(), "incomplete signed envelope set") {
		t.Fatalf("located partial publication was accepted: %v", err)
	}
	if got, err := os.ReadFile(active); err != nil || !bytes.Equal(got, encoded) {
		t.Fatalf("rejected active envelope changed: %v", err)
	}
	if _, err := os.Stat(locatorPath); err != nil {
		t.Fatalf("rejected locator pointer changed: %v", err)
	}
}

func TestArchiveCurrentDeploymentPublicationValidatesCompleteLocatorMetadata(t *testing.T) {
	prior, _, _ := deploymentPublicationArchiveFixture(t)
	_, _, priorHash, _, envelopeBytes, locatorBytes := completeDeploymentLocatorFixture(t, prior)
	var baseline deploymentManifestLocatorDirectory
	if err := json.Unmarshal(locatorBytes, &baseline); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*deploymentManifestLocatorDirectory)
	}{
		{name: "manifest-hash", mutate: func(value *deploymentManifestLocatorDirectory) { value.ManifestHash = "0x" + strings.Repeat("ab", 32) }},
		{name: "manifest-revision", mutate: func(value *deploymentManifestLocatorDirectory) { value.ManifestRevision++ }},
		{name: "previous-manifest", mutate: func(value *deploymentManifestLocatorDirectory) {
			value.PreviousManifestHash = "0x" + strings.Repeat("cd", 32)
		}},
		{name: "operator-url", mutate: func(value *deploymentManifestLocatorDirectory) {
			value.Locators[0].URL = "https://attacker.invalid/evidence"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for operator, exact := range envelopeBytes {
				if err := atomicWrite(filepath.Join(dir, "public", fmt.Sprintf("deployment-manifest.operator-%d.evidence.json", operator)), exact, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			value := baseline
			value.Locators = append([]deploymentManifestLocator(nil), baseline.Locators...)
			test.mutate(&value)
			mutated, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			locatorPath := filepath.Join(dir, "public", "deployment-manifest.locators.json")
			if err := atomicWrite(locatorPath, append(mutated, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := archiveCurrentDeploymentPublication(dir, prior, priorHash); err == nil {
				t.Fatal("invalid locator metadata was archived")
			}
			if got, err := os.ReadFile(locatorPath); err != nil || !bytes.Equal(got, append(mutated, '\n')) {
				t.Fatalf("rejected locator pointer changed: %v", err)
			}
		})
	}
}

func TestArchiveCurrentDeploymentPublicationRecoversOnlyCompleteArchivedPublication(t *testing.T) {
	prior, _, _ := deploymentPublicationArchiveFixture(t)
	_, _, priorHash, envelopes, envelopeBytes, locatorBytes := completeDeploymentLocatorFixture(t, prior)
	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		for operator, exact := range envelopeBytes {
			activePath := filepath.Join(dir, "public", fmt.Sprintf("deployment-manifest.operator-%d.evidence.json", operator))
			if err := atomicWrite(activePath, exact, 0o644); err != nil {
				t.Fatal(err)
			}
			archiveName := strings.TrimPrefix(envelopes[operator].ContentHash, "sha256:") + fmt.Sprintf(".operator-%d.evidence.json", operator)
			if err := writeImmutableEvidenceArchive(filepath.Join(dir, "public", "deployment-manifest-history", archiveName), exact); err != nil {
				t.Fatal(err)
			}
		}
		locatorArchive := filepath.Join(dir, "public", "deployment-manifests", stringsTrim0x(priorHash)+".locators.json")
		if err := writeImmutableEvidenceArchive(locatorArchive, locatorBytes); err != nil {
			t.Fatal(err)
		}
		// Model a crash after the current locator and the first active envelope
		// were durably unlinked.
		if err := os.Remove(filepath.Join(dir, "public", "deployment-manifest.operator-1.evidence.json")); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("complete", func(t *testing.T) {
		dir := setup(t)
		if err := archiveCurrentDeploymentPublication(dir, prior, priorHash); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "public", "deployment-manifest.operator-2.evidence.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remaining active envelope survived recovery: %v", err)
		}
	})
	t.Run("missing-archived-envelope", func(t *testing.T) {
		dir := setup(t)
		archiveName := strings.TrimPrefix(envelopes[1].ContentHash, "sha256:") + ".operator-1.evidence.json"
		if err := os.Remove(filepath.Join(dir, "public", "deployment-manifest-history", archiveName)); err != nil {
			t.Fatal(err)
		}
		if err := archiveCurrentDeploymentPublication(dir, prior, priorHash); err == nil {
			t.Fatal("recovery accepted a missing archived envelope")
		}
		if _, err := os.Stat(filepath.Join(dir, "public", "deployment-manifest.operator-2.evidence.json")); err != nil {
			t.Fatalf("failed recovery changed remaining active envelope: %v", err)
		}
	})
	t.Run("tampered-archived-locator", func(t *testing.T) {
		dir := setup(t)
		locatorArchive := filepath.Join(dir, "public", "deployment-manifests", stringsTrim0x(priorHash)+".locators.json")
		var value deploymentManifestLocatorDirectory
		if err := json.Unmarshal(locatorBytes, &value); err != nil {
			t.Fatal(err)
		}
		value.Locators[1].URL = "https://attacker.invalid/evidence"
		if err := writePublicJSON(locatorArchive, value); err != nil {
			t.Fatal(err)
		}
		if err := archiveCurrentDeploymentPublication(dir, prior, priorHash); err == nil {
			t.Fatal("recovery accepted a tampered archived locator")
		}
	})
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
	for label, role := range roles.Clients {
		// The public deployment manifest is written after provisioning has
		// assigned every client ID. Reproduce that completed lifecycle here so
		// revision archival can validate the embedded signer directory.
		role.ClientIDHex = strings.Repeat("01", 16)
		roles.Clients[label] = role
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
	if len(first.SetupEvidence) != wantEvidence || first.EvidenceTransportProfile != publicEvidenceTransportHTTPS || first.Operators[0].APIURL != "https://no1.example" || strings.Contains(first.Operators[0].APIURL, "127.0.0.1") {
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
	envelopeBytes := map[int][]byte{}
	locatorEntries := make([]map[string]any, 0, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		envelope, err := signEvidence(cfg, "deployment-manifest", "", &legacy, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		envelopeBytes[operator] = append(encoded, '\n')
		path := filepath.Join(dir, "public", fmt.Sprintf("deployment-manifest.operator-%d.evidence.json", operator))
		if err := atomicWrite(path, envelopeBytes[operator], 0o644); err != nil {
			t.Fatal(err)
		}
		locatorEntries = append(locatorEntries, map[string]any{"operator_no_id": operator, "content_hash": envelope.ContentHash})
	}
	for index := range locatorEntries {
		contentHash := locatorEntries[index]["content_hash"].(string)
		locatorEntries[index]["url"] = strings.TrimSuffix(cfg.OperatorAPIOrigins[index], "/") + "/sn/evidence?hash=" + contentHash
	}
	locatorBytes, err := json.Marshal(map[string]any{
		"schema":                 "urnetwork-public-manifest-locators-v1",
		"manifest_hash":          firstHash,
		"manifest_revision":      uint64(1),
		"previous_manifest_hash": "",
		"locators":               locatorEntries,
	})
	if err != nil {
		t.Fatal(err)
	}
	locatorBytes = append(locatorBytes, '\n')
	if err := atomicWrite(filepath.Join(dir, "public", "deployment-manifest.locators.json"), locatorBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	oldPlanHash := plan.PlanHash
	cfg.ConfigHash = "0x" + strings.Repeat("34", 32)
	cfg.PolicyHash = "0x" + strings.Repeat("56", 32)
	cfg.Release.Dependencies["redis"] = "redis:8-alpine@sha256:" + strings.Repeat("7", 64)
	plan.PlanHash = "0x" + strings.Repeat("78", 32)
	plan.PriorPlanHashes = []string{oldPlanHash}
	var tampered ReleaseEvidenceEnvelope
	if err := json.Unmarshal(envelopeBytes[2], &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Signature = "0x" + strings.Repeat("00", 65)
	tamperedBytes, err := json.Marshal(&tampered)
	if err != nil {
		t.Fatal(err)
	}
	tamperedBytes = append(tamperedBytes, '\n')
	operatorTwoPath := filepath.Join(dir, "public", "deployment-manifest.operator-2.evidence.json")
	if err := atomicWrite(operatorTwoPath, tamperedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writePublicDeploymentManifest(cfg, dir, plan); err == nil || !strings.Contains(err.Error(), "operator 2 evidence is invalid") {
		t.Fatalf("tampered prior signed evidence was accepted: %v", err)
	}
	currentAfterRejection, _ := os.ReadFile(filepath.Join(dir, "public.json"))
	locatorsAfterRejection, _ := os.ReadFile(filepath.Join(dir, "public", "deployment-manifest.locators.json"))
	if !bytes.Equal(currentAfterRejection, legacyBefore) || !bytes.Equal(locatorsAfterRejection, locatorBytes) {
		t.Fatal("rejected prior signed evidence changed an active publication pointer")
	}
	if err := atomicWrite(operatorTwoPath, envelopeBytes[2], 0o644); err != nil {
		t.Fatal(err)
	}
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
	for operator, exact := range envelopeBytes {
		activePath := filepath.Join(dir, "public", fmt.Sprintf("deployment-manifest.operator-%d.evidence.json", operator))
		if _, err := os.Stat(activePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("superseded operator %d active evidence survived revision: %v", operator, err)
		}
		var envelope ReleaseEvidenceEnvelope
		if err := json.Unmarshal(exact, &envelope); err != nil {
			t.Fatal(err)
		}
		archivePath := filepath.Join(dir, "public", "deployment-manifest-history", strings.TrimPrefix(envelope.ContentHash, "sha256:")+fmt.Sprintf(".operator-%d.evidence.json", operator))
		archivedEnvelope, err := os.ReadFile(archivePath)
		if err != nil || !bytes.Equal(archivedEnvelope, exact) {
			t.Fatalf("archived operator %d evidence mismatch: %v", operator, err)
		}
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

func TestEvidenceFileHashesExcludeOnlyExactRootCompletionFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"result.json":                    "result\n",
		"complete.json":                  "root marker\n",
		campaignEvidenceManifestFilename: "root manifest\n",
		"scenario-complete-commit.operator-1.evidence.json":        "operator one\n",
		"scenario-complete-commit.operator-2.evidence.json":        "operator two\n",
		"scenario-complete-commit.operator-3.evidence.json":        "extra operator\n",
		"scenario-complete-commit.operator-extra.evidence.json":    "malformed operator\n",
		"nested/complete.json":                                     "nested marker\n",
		"nested/scenario-complete-commit.operator-1.evidence.json": "nested operator\n",
	}
	for name, contents := range files {
		if err := atomicWrite(filepath.Join(dir, filepath.FromSlash(name)), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hashes, err := evidenceFileHashes(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []string{"complete.json", campaignEvidenceManifestFilename, "scenario-complete-commit.operator-1.evidence.json", "scenario-complete-commit.operator-2.evidence.json"} {
		if hashes[excluded] != "" {
			t.Errorf("exact completion file %s was hashed", excluded)
		}
	}
	for _, included := range []string{"result.json", "scenario-complete-commit.operator-3.evidence.json", "scenario-complete-commit.operator-extra.evidence.json", "nested/complete.json", "nested/scenario-complete-commit.operator-1.evidence.json"} {
		if hashes[included] == "" {
			t.Errorf("non-exact completion-like file %s was omitted: %v", included, hashes)
		}
	}
}
