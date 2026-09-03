package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFinalClaimQueueCapturePreservesEveryMinerOutcome(t *testing.T) {
	stateDir := t.TempDir()
	want := map[string]string{}
	for minerID, status := range []string{"finalized", "uncertain", "pending"} {
		name := filepath.ToSlash(filepath.Join("runtime", "miner-"+string(rune('1'+minerID)), "claims", "claim-queue.json"))
		payload := `{"schema":"urnetwork-miner-claim-queue-v1","entries":[{"status":"` + status + `","signed_raw_tx":"0x` + strings.Repeat(string(rune('a'+minerID)), 64) + `"}]}`
		writeValidatorStateTestFile(t, filepath.Join(stateDir, filepath.FromSlash(name)), payload)
		want[name] = payload
	}
	entries, err := finalCollectedNamedEntries(stateDir, finalClaimQueueNames(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Fatalf("claim queue count = %d, want %d", len(entries), len(want))
	}
	for _, entry := range entries {
		if string(entry.Data) != want[entry.Path] || entry.ContentHash != bytesSHA256(entry.Data) || entry.SizeBytes != uint64(len(entry.Data)) {
			t.Fatalf("claim queue %s was not preserved exactly", entry.Path)
		}
	}
	bundle := FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: "claim-runtime", Files: entries}
	encoded, err := json.Marshal(&bundle)
	if err != nil {
		t.Fatal(err)
	}
	if references, err := campaignArtifactReferences(map[string][]byte{"bundle.json": encoded}); err != nil || len(references) != 0 {
		t.Fatalf("bundle entries collided with artifact locator discovery: references=%v error=%v", references, err)
	}
}

func TestFinalCollectedBundleSecretScanDecodesEmbeddedBytes(t *testing.T) {
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "run")
	secret := "fixture-secret-material-123456"
	entry := FinalCollectedFileBundleEntry{Path: "claim-queue.json", ContentHash: bytesSHA256([]byte(secret)), SizeBytes: uint64(len(secret)), Data: []byte(secret)}
	if _, err := persistFinalCollectedBundleChunks(runDir, "claim-runtime", []FinalCollectedFileBundleEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if err := scanEvidenceSecrets(stateDir, runDir, nil, secret); err == nil || !strings.Contains(err.Error(), "claim-queue.json") {
		t.Fatalf("embedded secret scan = %v", err)
	}
}

func TestFinalSemanticPublicCaptureExcludesReplicatedSupplementStaging(t *testing.T) {
	root := t.TempDir()
	writeValidatorStateTestFile(t, filepath.Join(root, "deployment-manifest.json"), `{"deployment":"captured"}`)
	staged := filepath.Join(root, finalSemanticSupplementArchiveDir, "release-run", "files", "carried.evidence.json")
	writeValidatorStateTestFile(t, staged, `{"data":"already copied by prior-phase lineage"}`)
	entries, err := finalCollectedDirectoryEntries(root, finalSemanticPublicCapturePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "deployment-manifest.json" {
		t.Fatalf("public capture duplicated semantic supplement staging: %+v", entries)
	}
}

func TestFinalSemanticLaunchFoundationCapturesRuntimeConfigManifest(t *testing.T) {
	names := finalSemanticLaunchFoundationNames()
	count := 0
	for _, name := range names {
		if name == "runtime-config-manifest.json" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("launch-foundation runtime-config-manifest.json count=%d, names=%v", count, names)
	}
}

func TestFinalCollectedFileEntryRejectsSymlinkComponentsAndSameSizeMutation(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	realFile := filepath.Join(realDirectory, "value.json")
	writeValidatorStateTestFile(t, realFile, `{"value":"one"}`)
	entry, err := finalCollectedFileEntry(root, "real/value.json")
	if err != nil || string(entry.Data) != `{"value":"one"}` {
		t.Fatalf("regular capture = %+v, %v", entry, err)
	}
	if err := os.Symlink(filepath.Join("real", "value.json"), filepath.Join(root, "leaf.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := finalCollectedFileEntry(root, "leaf.json"); err == nil {
		t.Fatal("captured a symlink leaf")
	}
	if err := os.Symlink("real", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := finalCollectedFileEntry(root, "alias/value.json"); err == nil {
		t.Fatal("captured through a symlink directory")
	}
	before, err := os.Stat(realFile)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	writeValidatorStateTestFile(t, realFile, `{"value":"two"}`)
	if err := os.Chtimes(realFile, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(realFile)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || sameFinalCollectedFileState(before, after) {
		t.Fatal("same-size in-place mutation retained a capture-stable file identity")
	}
}

func TestFinalContractCleanupCaptureUsesAcceptedSupervisorGeneration(t *testing.T) {
	stateDir := t.TempDir()
	cutoff := time.Unix(1_700_000_000, 456).UTC()
	supervisor := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: 99, SupervisorStartTimeTicks: 123,
		ManifestHash: "0x" + strings.Repeat("ab", 32), ContractCleanupCutoff: cutoff.Format(time.RFC3339Nano),
		Processes: []ProcessState{
			{ID: "operator-1-taskworker", Role: "operator-taskworker", Identity: "no:1", PID: 101, Healthy: true},
			{ID: "operator-2-taskworker", Role: "operator-taskworker", Identity: "no:2", PID: 102, Healthy: true},
		},
	}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), supervisor); err != nil {
		t.Fatal(err)
	}
	generation := "1700000000000000456"
	for noID := 1; noID <= 2; noID++ {
		base := filepath.Join(stateDir, "processes", "operator-"+string(rune('0'+noID))+"-taskworker-contract-cleanup-")
		writeValidatorStateTestFile(t, base+generation+".json", `{"schema":"urnetwork-sim-server-contract-cleanup-v1","converged":true}`)
		writeValidatorStateTestFile(t, base+generation+".log", "accepted cleanup\n")
		writeValidatorStateTestFile(t, base+"1699999999999999999.json", `{"schema":"urnetwork-sim-server-contract-cleanup-v1","converged":false}`)
		writeValidatorStateTestFile(t, base+"1699999999999999999.log", "failed old cleanup\n")
	}
	entries, err := finalContractCleanupEntries(stateDir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("accepted cleanup file count = %d, want 4", len(entries))
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Path, generation) || strings.Contains(string(entry.Data), "failed old cleanup") {
			t.Fatalf("captured wrong cleanup generation: %+v", entry)
		}
	}
}

func TestFinalContractCleanupCaptureRejectsMissingAcceptedResult(t *testing.T) {
	stateDir := t.TempDir()
	cutoff := time.Unix(1_700_000_000, 456).UTC()
	supervisor := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", ContractCleanupCutoff: cutoff.Format(time.RFC3339Nano),
		Processes: []ProcessState{{ID: "operator-1-taskworker", Role: "operator-taskworker", Identity: "no:1", PID: 101, Healthy: true}},
	}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), supervisor); err != nil {
		t.Fatal(err)
	}
	if _, err := finalContractCleanupEntries(stateDir, 1); err == nil || !os.IsNotExist(err) {
		t.Fatalf("missing accepted cleanup = %v", err)
	}
}
