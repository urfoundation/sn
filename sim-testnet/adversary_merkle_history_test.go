// Deterministic sample interruption and identity changes exercise the real signed
// artifact verifier while avoiding wall-clock or network scheduling assumptions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/payoutartifact"
)

// Owns synthetic signed bodies and their explicit read ordering for one probe.
type merkleHistoryFixture struct {
	cfg        *ResolvedConfig
	deployment *ContractDeployment
	cache      liveMerkleHistoryCache
	keys       []string
	bodyKVs    map[string][]byte
	reads      []string
}

// Gives each history member a real signature, hash and distinct payout epoch.
func newMerkleHistoryFixture(t *testing.T) *merkleHistoryFixture {
	t.Helper()
	fixture := &merkleHistoryFixture{cfg: testResolvedConfig(t), deployment: &ContractDeployment{CoordinatorProxy: common.HexToAddress("0x100"), SettlementVault: common.HexToAddress("0x200")}, bodyKVs: map[string][]byte{}}
	for epoch := uint64(1); epoch <= 3; epoch++ {
		fixture.add(t, epoch, epoch)
	}
	return fixture
}

// Produces an independently authenticated history entry; a changed salt can
// deliberately introduce two signed claims about one epoch for negative tests.
func (self *merkleHistoryFixture) add(t *testing.T, epoch, salt uint64) {
	t.Helper()
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{
		DeploymentID: self.cfg.Config.Deployment.DeploymentID, GenesisHash: self.cfg.Public.Chain.GenesisHash,
		PolicyHash: self.cfg.PolicyHash, ChainID: self.cfg.ChainID, Netuid: self.cfg.Netuid,
		Coordinator: self.deployment.CoordinatorProxy, SettlementVault: self.deployment.SettlementVault,
		Epoch: epoch, NoID: 1,
		Start:                payoutartifact.Boundary{Number: 10, Hash: "0x" + strings.Repeat("01", 32)},
		End:                  payoutartifact.Boundary{Number: 20, Hash: "0x" + strings.Repeat("02", 32)},
		OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("20", 32),
		Providers:       []payoutartifact.ProviderInput{{ClientID: [16]byte{1}, NetworkID: [16]byte{2}, Coldkey: [32]byte{3}, UsageBytes: 100 + salt, Assignments: 8, Confirmations: 8, Eligible: true}},
		ReliabilityAMin: 8, CreatedAt: time.Date(2026, 1, 1, 0, 0, int(salt), 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ethcrypto.HexToECDSA(strings.Repeat("11", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimPrefix(artifact.ContentHash, "sha256:")
	self.keys = append(self.keys, hash+".json")
	self.bodyKVs[hash] = body
}

// Records exactly which immutable objects a sample asks the source to return.
func (self *merkleHistoryFixture) get(ctx context.Context, hash string) ([]byte, error) {
	self.reads = append(self.reads, hash)
	return self.bodyKVs[hash], ctx.Err()
}

// Calls the production selector with a generation owned by the test transition.
func (self *merkleHistoryFixture) selectArtifact(generation string, get func(context.Context, string) ([]byte, error)) (*payoutArtifact, error) {
	return selectLiveMerkleArtifact(context.Background(), self.cfg, self.deployment, "https://operator.example", 1, self.keys, get, &self.cache, generation)
}

// A deadline after one authenticated body keeps that prefix for the next sample;
// completion still requires the remaining history and a fresh latest body.
func TestLiveMerkleHistoryResumesVerifiedPrefixAfterDeadline(t *testing.T) {
	fixture := newMerkleHistoryFixture(t)
	get := func(ctx context.Context, hash string) ([]byte, error) {
		if len(fixture.reads) == 1 {
			return nil, context.DeadlineExceeded
		}
		return fixture.get(ctx, hash)
	}
	if artifact, err := fixture.selectArtifact("owner-a", get); !errors.Is(err, context.DeadlineExceeded) || artifact != nil || len(fixture.cache.epochKVs) != 1 {
		t.Fatalf("interrupted artifact=%v err=%v retained=%d", artifact, err, len(fixture.cache.epochKVs))
	}
	fixture.reads = nil
	artifact, err := fixture.selectArtifact("owner-a", fixture.get)
	if err != nil || artifact.Epoch != 3 || len(fixture.reads) != 2 || len(fixture.cache.epochKVs) != 3 {
		t.Fatalf("resumed artifact=%v err=%v reads=%v retained=%d", artifact, err, fixture.reads, len(fixture.cache.epochKVs))
	}
	fixture.reads = nil
	artifact, err = fixture.selectArtifact("owner-a", fixture.get)
	if err != nil || artifact.Epoch != 3 || len(fixture.reads) != 1 || fixture.reads[0]+".json" != fixture.keys[2] {
		t.Fatalf("latest artifact was not freshly read: artifact=%v err=%v reads=%v", artifact, err, fixture.reads)
	}
}

// A new process generation, missing owner, or identity scope cannot reuse any
// historical entry even when the immutable filenames remain unchanged.
func TestLiveMerkleHistoryInvalidatesGenerationAndIdentity(t *testing.T) {
	fixture := newMerkleHistoryFixture(t)
	for _, generation := range []string{"owner-a", "owner-b", "", "", "owner-c"} {
		fixture.reads = nil
		if _, err := fixture.selectArtifact(generation, fixture.get); err != nil || len(fixture.reads) != 3 {
			t.Fatalf("generation=%q err=%v reads=%v", generation, err, fixture.reads)
		}
	}
	fixture.cfg.PolicyHash = "0x" + strings.Repeat("ab", 32)
	fixture.reads = nil
	if _, err := fixture.selectArtifact("owner-c", fixture.get); err == nil || !strings.Contains(err.Error(), "identity") || len(fixture.reads) != 1 || len(fixture.cache.epochKVs) != 0 {
		t.Fatalf("changed policy reused metadata: err=%v reads=%v retained=%d", err, fixture.reads, len(fixture.cache.epochKVs))
	}
}

// Current body corruption cannot be hidden behind its previously verified hash.
func TestLiveMerkleHistoryRevalidatesLatestPayload(t *testing.T) {
	fixture := newMerkleHistoryFixture(t)
	if _, err := fixture.selectArtifact("owner-a", fixture.get); err != nil {
		t.Fatal(err)
	}
	latestHash := strings.TrimSuffix(fixture.keys[2], ".json")
	fixture.bodyKVs[latestHash] = fixture.bodyKVs[strings.TrimSuffix(fixture.keys[0], ".json")]
	fixture.reads = nil
	if _, err := fixture.selectArtifact("owner-a", fixture.get); err == nil || !strings.Contains(err.Error(), "identity") || len(fixture.reads) != 1 || fixture.reads[0] != latestHash {
		t.Fatalf("substituted latest body accepted: err=%v reads=%v", err, fixture.reads)
	}
}

// New members are authenticated and compared with retained epochs before a
// latest artifact can pass; a valid signature does not permit equivocation.
func TestLiveMerkleHistoryRejectsNewEquivocation(t *testing.T) {
	fixture := newMerkleHistoryFixture(t)
	if _, err := fixture.selectArtifact("owner-a", fixture.get); err != nil {
		t.Fatal(err)
	}
	fixture.add(t, 2, 99)
	fixture.reads = nil
	if _, err := fixture.selectArtifact("owner-a", fixture.get); err == nil || !strings.Contains(err.Error(), "equivocated") || len(fixture.reads) != 1 {
		t.Fatalf("new equivocation accepted: err=%v reads=%v", err, fixture.reads)
	}
}

// Retention and incoming history each have their own finite bound. A failed
// body cannot enter the cache, and an oversized history performs no fetches.
func TestLiveMerkleHistoryBoundsAndUnauthenticatedBodies(t *testing.T) {
	fixture := newMerkleHistoryFixture(t)
	firstHash := strings.TrimSuffix(fixture.keys[0], ".json")
	goodBody := fixture.bodyKVs[firstHash]
	fixture.bodyKVs[firstHash] = []byte(`{"content_hash":"sha256:invalid"}`)
	if _, err := fixture.selectArtifact("owner-a", fixture.get); err == nil || len(fixture.cache.epochKVs) != 0 {
		t.Fatalf("unauthenticated body retained: err=%v entries=%d", err, len(fixture.cache.epochKVs))
	}
	fixture.bodyKVs[firstHash] = goodBody
	for index := 0; index < maximumPayoutArtifactHistoryKeys; index++ {
		fixture.cache.epochKVs[fmt.Sprintf("%064x", index)] = uint64(index)
	}
	if _, err := fixture.selectArtifact("owner-a", fixture.get); err != nil || len(fixture.cache.epochKVs) != maximumPayoutArtifactHistoryKeys {
		t.Fatalf("retention bound changed: err=%v entries=%d", err, len(fixture.cache.epochKVs))
	}
	fixture.keys = make([]string, maximumPayoutArtifactHistoryKeys+1)
	fixture.reads = nil
	if _, err := fixture.selectArtifact("owner-a", fixture.get); err == nil || len(fixture.reads) != 0 {
		t.Fatalf("oversized history admitted: err=%v reads=%v", err, fixture.reads)
	}
}

// Only a matching durable manifest and process generation grants cache reuse.
func TestLiveMerkleHistorySourceGenerationRequiresExactOwner(t *testing.T) {
	stateDir := t.TempDir()
	spec := ProcessSpec{ID: "operator-1-api", Role: "operator-api", Identity: "no:1", Command: "/synthetic/operator"}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "synthetic-deployment", Specs: []ProcessSpec{spec}}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: manifestHash, Processes: []ProcessState{{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: 101, StartedAt: "2026-01-01T00:00:00Z"}}}
	write := func() {
		t.Helper()
		if err := writePublicJSON(filepath.Join(stateDir, "supervisor.json"), manifest); err != nil {
			t.Fatal(err)
		}
		if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), state); err != nil {
			t.Fatal(err)
		}
	}
	if generation := liveMerkleSourceGeneration(stateDir, 1); generation != "" {
		t.Fatalf("absent owner granted generation %q", generation)
	}
	write()
	first := liveMerkleSourceGeneration(stateDir, 1)
	if first == "" {
		t.Fatal("matching owner did not grant generation")
	}
	state.Processes[0].Restarts++
	write()
	if next := liveMerkleSourceGeneration(stateDir, 1); next == "" || next == first {
		t.Fatalf("restart reused generation: first=%q next=%q", first, next)
	}
	state.Processes[0].Identity = "no:2"
	write()
	if generation := liveMerkleSourceGeneration(stateDir, 1); generation != "" {
		t.Fatalf("replaced owner granted generation %q", generation)
	}
	state.Processes[0].Identity = spec.Identity
	state.ManifestHash = "sha256:unrelated"
	write()
	if generation := liveMerkleSourceGeneration(stateDir, 1); generation != "" {
		t.Fatalf("unmatched manifest granted generation %q", generation)
	}
}

// A restart during the actual fresh history request discards all cached metadata
// before any finalized-state call or proof can be reported successful.
func TestLiveMerkleHistoryProbeRejectsSourceTurnover(t *testing.T) {
	fixture := newMerkleHistoryFixture(t)
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, *fixture.deployment); err != nil {
		t.Fatal(err)
	}
	spec := ProcessSpec{ID: "operator-1-api", Role: "operator-api", Identity: "no:1", Command: "/synthetic/operator"}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", Specs: []ProcessSpec{spec}}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: manifestHash, Processes: []ProcessState{{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: 101, StartedAt: "2026-01-01T00:00:00Z"}}}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.selectArtifact(liveMerkleSourceGeneration(stateDir, 1), fixture.get); err != nil {
		t.Fatal(err)
	}
	history := payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1"}
	for index, key := range fixture.keys {
		hash := strings.TrimSuffix(key, ".json")
		history.Objects = append(history.Objects, payoutArtifactHistoryObject{Key: fmt.Sprintf("blob/operator-1/st/v1/history/%s/%d/%d/1/%s", fixture.cfg.Config.Deployment.DeploymentID, fixture.cfg.Netuid, index+1, key), Size: int64(len(fixture.bodyKVs[hash])), ContentHash: "sha256:" + hash})
	}
	historyBytes, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	rpcCalls := 0
	client := adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "rpc.example" {
			rpcCalls++
			return nil, errors.New("source turnover must not reach the vault")
		}
		if request.URL.Path == "/sn/artifacts" {
			state.Processes[0].Restarts++
			if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), state); err != nil {
				return nil, err
			}
			return adversaryGetTestResponse(http.StatusOK, string(historyBytes)), nil
		}
		return adversaryGetTestResponse(http.StatusOK, string(fixture.bodyKVs[strings.TrimPrefix(request.URL.Query().Get("hash"), "sha256:")])), nil
	})
	_, err = liveInvalidMerkleProofProbeWithHistory(context.Background(), fixture.cfg, stateDir, "https://operator.example", "https://rpc.example", 1, client, client, 1, &fixture.cache)
	if !errors.Is(err, errLiveMerkleEvidenceUnavailable) || len(fixture.cache.epochKVs) != 0 || rpcCalls != 0 {
		t.Fatalf("source turnover granted proof: err=%v retained=%d rpc=%d", err, len(fixture.cache.epochKVs), rpcCalls)
	}
}

// Exhausting a sample deadline is retried without claiming the final proof;
// integrity errors and explicit cancellation cannot become successful evidence.
func TestLiveMerkleHistoryDeadlineRetriesWithoutWeakeningIntegrity(t *testing.T) {
	if !liveMerkleRetryable(fmt.Errorf("history read: %w", context.DeadlineExceeded), false) {
		t.Fatal("transient sample deadline did not retry")
	}
	for _, err := range []error{errors.New("payout artifact identity mismatch"), errors.New("context deadline exceeded"), context.Canceled} {
		if liveMerkleRetryable(err, false) {
			t.Fatalf("hard error admitted as retry: %v", err)
		}
	}
}
