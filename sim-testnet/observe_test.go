package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	cfg := testResolvedConfig(t)
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
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: "committed-run", DeploymentID: cfg.Config.Deployment.DeploymentID,
		Name: "release-1.0", ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID,
		GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, CompletedAt: time.Now().UTC().Format(time.RFC3339Nano), Result: "pass",
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	bundle := ScenarioEvidenceBundle{Schema: "urnetwork-sim-scenario-evidence-v1", Result: result, Observation: &ScenarioObservation{ObservationHash: "observation"}}
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
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{
		ResultHash: result.EvidenceHash, BundlePayloadHash: bytesSHA256(bundlePayload), Files: map[string]string{"result.json": "sha256:" + strings.Repeat("12", 32)},
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash,
		Netuid: cfg.Netuid, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Identities: identities, Topology: cfg.Config.Topology,
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		base := fmt.Sprintf("%s/operator-%d", server.URL, operator)
		public.Operators = append(public.Operators, PublicOperator{NoID: operator, APIURL: base, HistoryURL: base + "/sn/evidence/history"})
	}
	probe := &liveScenarioProbe{cfg: cfg, client: server.Client()}
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("direct owner envelope bypassed the per-operator completion commit")
	}
	directOwnerVisible = false
	commitVisible[1] = true
	if _, err := probe.fetchLatestScenarioBundle(context.Background(), public); err == nil {
		t.Fatal("completion commit present at only one operator was accepted")
	}
	commitVisible[2] = true
	got, err := probe.fetchLatestScenarioBundle(context.Background(), public)
	if err != nil || got.Result == nil || got.Result.RunID != result.RunID {
		t.Fatalf("replicated committed scenario bundle = %+v, %v", got, err)
	}
}
