package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A failed multi-hour interval can retain more than the ordinary 32 MiB
// artifact ceiling. Recovery binds its complete bytes without adopting the
// unsigned suffix or changing the original file.
func TestScenarioCampaignRecoveryBindsLargeObservationHistory(t *testing.T) {
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	path := filepath.Join(runDir, "observations.jsonl")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	block := bytes.Repeat([]byte{' '}, 1024*1024)
	for range 33 {
		if _, err := file.Write(block); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.ToSlash(filepath.Join("runs", prior.payload.RunID, "observations.jsonl"))
	hash, size, err := hashCampaignObservationHistory(fixture.stateDir, name)
	if err != nil || size <= maximumCampaignEvidenceRawFileBytes {
		t.Fatalf("large historical observation hash: size=%d err=%v", size, err)
	}
	recovery, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.payload.Recovery == nil || recovery.payload.Recovery.PriorObservationLogBytes != size || recovery.payload.Recovery.PriorObservationLogSha256 != hash {
		t.Fatalf("large historical log was not bound exactly: %+v", recovery.payload.Recovery)
	}
	if _, _, _, current, _, err := prior.loadAuthenticatedRecoveryRuntimeForensics(runDir); err != nil || current.ObservationHash != prior.payload.AcceptanceBoundary.LastObservationHash {
		t.Fatalf("large unsigned suffix was adopted or rejected: current=%+v err=%v", current, err)
	}
	if info, err := os.Stat(path); err != nil || uint64(info.Size()) != size {
		t.Fatalf("large historical log changed: %v", err)
	}
}

func TestCampaignObservationHistoryRejectsOversizeAndSymlink(t *testing.T) {
	stateDir := t.TempDir()
	name := "runs/large/observations.jsonl"
	path := filepath.Join(stateDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maximumCampaignObservationHistoryBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := hashCampaignObservationHistory(stateDir, name); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatalf("oversize historical log accepted: %v", err)
	}
	if _, err := readCampaignObservationHistory(stateDir, name); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatalf("oversize historical log loaded: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(stateDir, "outside"), path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := hashCampaignObservationHistory(stateDir, name); err == nil {
		t.Fatal("symlink historical log accepted")
	}
}
