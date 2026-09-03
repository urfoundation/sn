package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFinalSemanticOutputsRemainOutsideClosedCampaignFileSet(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "release-run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source, artifacts := finalSemanticFixture(t)
	result := &ScenarioResult{Name: source.Phase, RunID: source.RunID}
	terminal := &ScenarioObservation{Schema: "urnetwork-sim-scenario-observation-v1"}
	history := []*ScenarioObservation{terminal}
	options := scenarioRunOptions{
		BuildFinalSemantic: func(_ context.Context, got *ResolvedConfig, gotStateDir, gotRunDir string, gotResult *ScenarioResult, gotTerminal *ScenarioObservation, gotHistory []*ScenarioObservation) (*FinalSemanticEvidence, error) {
			if got != cfg || gotStateDir != stateDir || gotRunDir != runDir || gotResult != result || gotTerminal != terminal || len(gotHistory) != 1 {
				return nil, errors.New("semantic builder received different campaign inputs")
			}
			copy := source
			return &copy, nil
		},
		FinalSemanticArtifacts: func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
			data, ok := artifacts[locator.URI]
			if !ok {
				return nil, errors.New("fixture artifact is absent")
			}
			return append([]byte(nil), data...), nil
		},
		FinalSemanticReader: func(_ context.Context, draft *FinalSemanticEvidence) (FinalSemanticChainReader, error) {
			return &finalTestChainReader{evidence: draft}, nil
		},
	}
	if err := produceFinalSemanticCampaignOutputs(context.Background(), cfg, stateDir, runDir, result, terminal, history, options); err != nil {
		t.Fatal(err)
	}
	hashes, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		if _, included := hashes[name]; included {
			t.Fatalf("post-capture semantic output %s changed the closed campaign file set: %v", name, hashes)
		}
	}
	// The asynchronous semantic supplement signs these exact derived bytes in
	// a second owner envelope. They must therefore remain discoverable by its
	// complete census even though the original capture manifest is immutable.
	derived, err := enumerateFinalSemanticRawFiles(runDir)
	if err != nil {
		t.Fatal(err)
	}
	derivedHashes := make(map[string]string, len(derived))
	for _, file := range derived {
		derivedHashes[file.Path] = file.ContentHash
	}
	for _, name := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		if !validSHA256ContentHash(derivedHashes[name]) {
			t.Fatalf("semantic supplement census omits %s: %v", name, derivedHashes)
		}
	}
}

func TestFinalSemanticBuilderFailureCreatesNoLooseOutputs(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "release-run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	expected := errors.New("closed capture is inconsistent")
	options := scenarioRunOptions{BuildFinalSemantic: func(context.Context, *ResolvedConfig, string, string, *ScenarioResult, *ScenarioObservation, []*ScenarioObservation) (*FinalSemanticEvidence, error) {
		return nil, expected
	}}
	err := produceFinalSemanticCampaignOutputs(context.Background(), cfg, stateDir, runDir, &ScenarioResult{}, &ScenarioObservation{}, []*ScenarioObservation{{}}, options)
	if !errors.Is(err, expected) {
		t.Fatalf("semantic builder failure = %v", err)
	}
	for _, name := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		if _, statErr := os.Lstat(filepath.Join(runDir, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed semantic build created %s: %v", name, statErr)
		}
	}
}
