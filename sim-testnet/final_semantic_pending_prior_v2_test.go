//go:build linux || darwin

// These fixtures create real immutable raw files and owner signatures without
// pretending their small opaque measurement bodies are valid semantic proofs.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/urnetwork/server/v2026"
)

// The ordinary signed campaign fixture supplies the complete actual handoff.
// Only its structural source census is converted to compact capture form.
type finalPendingPriorV2TestFixture struct {
	cfg          *ResolvedConfig
	stateRoot    string
	priorRunRoot string
	runRoot      string
	result       *ScenarioResult
	priorResult  *ScenarioResult
	prior        *FinalCollectedPriorPhaseInputs
	current      *FinalSemanticCollectedInputs
	loaded       map[string][]byte
	closure      *finalSemanticOriginalClosure
}

// No native submission or semantic fixture is used. Actual signed originals
// are published and read through the existing isolated disk-store boundary.
func newFinalPendingPriorV2TestFixture(t *testing.T) *finalPendingPriorV2TestFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	stateRoot := t.TempDir()
	configureRuntimeEvidenceV2Test(t, cfg, stateRoot)
	// The original helper emits legacy shape only. Keep the already hashed
	// approved config identity while assembling, then convert before closing.
	evidence := cfg.Config.ValidatorEvidenceV2
	cfg.Config.ValidatorEvidenceV2 = nil
	priorResult, roles, priorRunRoot := writeReleaseCampaignFixture(t, cfg, stateRoot, 26, 32)
	cfg.Config.ValidatorEvidenceV2 = evidence
	var inputs FinalSemanticCollectedInputs
	if err := decodeStrictJSONFile(filepath.Join(priorRunRoot, "final-inputs", "manifest.json"), &inputs); err != nil {
		t.Fatal(err)
	}
	_, shape, shapeRoot := newFinalCaptureV2ShapeTestFixture(t)
	for index := range inputs.Validators {
		validator := &inputs.Validators[index]
		compact := *shape.Validators[0].EvidenceV2
		compact.Sources = append([]FinalCollectedValidatorSourceV2(nil), compact.Sources...)
		for sourceIndex := range compact.Sources {
			source := &compact.Sources[sourceIndex]
			raw, err := os.ReadFile(filepath.Join(shapeRoot, filepath.FromSlash(source.Artifact.URI)))
			if err != nil {
				t.Fatal(err)
			}
			source.Artifact, err = persistFinalCollectedArtifact(priorRunRoot, source.Artifact.Kind, source.Artifact.URI, raw)
			if err != nil {
				t.Fatal(err)
			}
		}
		compact.Closures = append([]FinalCollectedSettlementClosure(nil), validator.SettlementClosures...)
		for closureIndex := range compact.Closures {
			compact.Closures[closureIndex].Artifact.Kind = "validator-evidence-v2-source"
		}
		validator.EvidenceV2 = &compact
		validator.Attempts, validator.PathProofs, validator.SettlementClosures = nil, nil, nil
	}
	snapshot := FinalCollectedChainSnapshot{Schema: finalCollectedChainSnapshotSchema, Phase: "release-1.0", RunID: priorResult.RunID, DeploymentID: priorResult.DeploymentID, EVMHead: priorResult.EndHead, NativeHead: ChainHead{Number: 900, Hash: finalTestHex(0x71)}, NativeHeads: []ChainHead{{Number: 900, Hash: finalTestHex(0x71)}}}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	bundles, err := persistFinalCollectedBundleChunks(priorRunRoot, "live-chain", []FinalCollectedFileBundleEntry{{Path: "live-chain/final-chain-snapshot.json", ContentHash: bytesSHA256(snapshotRaw), SizeBytes: uint64(len(snapshotRaw)), Data: snapshotRaw}})
	if err != nil {
		t.Fatal(err)
	}
	inputs.ClosedInputBundles = append(inputs.ClosedInputBundles, bundles...)
	sort.Slice(inputs.ClosedInputBundles, func(i, j int) bool { return inputs.ClosedInputBundles[i].URI < inputs.ClosedInputBundles[j].URI })
	inputs.EvidenceHash, err = finalSemanticCollectedInputsHash(&inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, &inputs); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(priorRunRoot, "final-inputs", "manifest.json"), &inputs); err != nil {
		t.Fatal(err)
	}
	inputRaw, err := os.ReadFile(filepath.Join(priorRunRoot, "final-inputs", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	inputLocator := FinalArtifactLocator{Kind: "final-semantic-input-manifest", URI: "final-inputs/manifest.json", ContentHash: bytesSHA256(inputRaw), SizeBytes: uint64(len(inputRaw))}
	capture := finalSemanticCaptureStatus(priorResult, &inputs, inputLocator)
	capture.EvidenceHash, err = finalSemanticCaptureStatusHash(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(priorRunRoot, finalSemanticCaptureStatusFilename), capture); err != nil {
		t.Fatal(err)
	}
	hashes, err := evidenceFileHashes(priorRunRoot, cfg.Config.Topology.Operators)
	if err != nil {
		t.Fatal(err)
	}
	// The existing runtime receipt authenticates exact source files and both
	// store namespaces. Only transport is supplied by real local disk stores.
	cfg.Repos.Vault = t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg.Repos.Vault, "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	expected, err := expectedRuntimeConfigFiles(cfg, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	for name, mode := range expected {
		path := filepath.Join(stateRoot, filepath.FromSlash(name))
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if err := atomicWrite(path, []byte(name+"\n"), mode); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if err := writeRuntimeConfigManifest(cfg, stateRoot); err != nil {
		t.Fatal(err)
	}
	stores := map[int]server.BlobStore{}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(filepath.Join(stateRoot, "object-store"), prefix)
	}
	factory := func(operator int) (server.BlobStore, error) { return stores[operator], nil }
	bundleHash := bytesSHA256([]byte("opaque-original-test-bundle"))
	manifest, err := publishCampaignEvidenceArchive(t.Context(), cfg, roles, stateRoot, priorResult.RunID, priorResult.EvidenceHash, bundleHash, hashes, factory)
	if err != nil {
		t.Fatal(err)
	}
	completionPayload := scenarioCompletePayload{ResultHash: priorResult.EvidenceHash, Files: hashes, BundlePayloadHash: bundleHash, EvidenceManifestHash: manifest.ContentHash, LifecycleHandoff: priorResult.LifecycleHandoff}
	complete, err := signEvidence(cfg, "scenario-complete", priorResult.RunID, completionPayload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(priorRunRoot, "complete.json"), complete); err != nil {
		t.Fatal(err)
	}
	gate, err := validateReleaseCampaignComplete(cfg, roles, priorRunRoot, priorResult)
	if err != nil {
		t.Fatal(err)
	}
	result := &ScenarioResult{Name: "production-soak", RunID: "compact-pending-production", StartedAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano), DeploymentID: priorResult.DeploymentID, ConfigHash: priorResult.ConfigHash, PolicyHash: priorResult.PolicyHash, ChainID: priorResult.ChainID, GenesisHash: priorResult.GenesisHash, Netuid: priorResult.Netuid, PriorRelease: gate}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(stateRoot, "runs", result.RunID)
	prior, err := collectFinalPriorPhaseInputsWithStoresContext(t.Context(), cfg, stateRoot, runRoot, result, factory)
	if err != nil {
		t.Fatal(err)
	}
	loaded := map[string][]byte{}
	for _, locator := range finalCollectedPriorLocators(prior) {
		raw, err := os.ReadFile(filepath.Join(runRoot, filepath.FromSlash(locator.URI)))
		if err != nil {
			t.Fatal(err)
		}
		loaded[locator.URI], loaded[locator.Kind] = raw, raw
	}
	resultRaw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	current := &FinalSemanticCollectedInputs{Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash, Window: ScenarioAcceptanceWindow{StartBlock: prior.Window.TerminalBlock + 1}, PriorPhase: prior, Validators: inputs.Validators, ScenarioResult: FinalArtifactLocator{Kind: "scenario-result-candidate", URI: "current-result.json", ContentHash: bytesSHA256(resultRaw), SizeBytes: uint64(len(resultRaw))}}
	loaded[current.ScenarioResult.URI] = resultRaw
	if err := verifyFinalCollectedPriorPhase(prior, current); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalCollectedPendingPriorBinding(cfg, current, loaded); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalCollectedPriorPhaseBytes(cfg, prior, loaded); err != nil {
		t.Fatal(err)
	}
	closure := &finalSemanticOriginalClosure{complete: complete, manifest: manifest, capture: capture, collected: &inputs}
	return &finalPendingPriorV2TestFixture{cfg: cfg, stateRoot: stateRoot, priorRunRoot: priorRunRoot, runRoot: runRoot, result: result, priorResult: priorResult, prior: prior, current: current, loaded: loaded, closure: closure}
}

// The exact producer formerly blocked on absent semantic output. It now closes
// original authority and nothing in this test can satisfy the semantic builder.
func TestFinalCaptureV2PendingPriorClosesOriginalAuthority(t *testing.T) {
	t.Parallel()
	fixture := newFinalPendingPriorV2TestFixture(t)
	if fixture.prior.SemanticStatus != finalSemanticCapturePendingStatus || fixture.prior.SemanticSupplement != (FinalArtifactLocator{}) || len(fixture.prior.SemanticFileEnvelopes) != 0 {
		t.Fatal("capture invented a predecessor semantic verdict")
	}
	for _, name := range []string{finalSemanticSupplementFilename, finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		if _, err := os.Lstat(filepath.Join(fixture.priorRunRoot, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pending capture needed semantic output %s: %v", name, err)
		}
	}
	if err := requireFinalSemanticReplayV2(fixture.current); !isFinalSemanticAnalysisPending(err) {
		t.Fatalf("raw capture became final acceptance: %v", err)
	}
}

// Rehashing copied bytes cannot replace any member of the owner-signed graph.
func TestFinalCaptureV2PendingPriorRejectsRehashedSourceAndMissingCensus(t *testing.T) {
	t.Parallel()
	fixture := newFinalPendingPriorV2TestFixture(t)
	for _, kind := range []string{"result", "capture", "live-chain"} {
		prior := *fixture.prior
		prior.LiveChainBundles = append([]FinalArtifactLocator(nil), prior.LiveChainBundles...)
		loaded := make(map[string][]byte, len(fixture.loaded))
		for name, raw := range fixture.loaded {
			loaded[name] = raw
		}
		var locator *FinalArtifactLocator
		switch kind {
		case "result":
			locator = &prior.ScenarioResult
		case "capture":
			locator = &prior.CaptureStatus
		case "live-chain":
			locator = &prior.LiveChainBundles[0]
		}
		raw := append(append([]byte(nil), loaded[locator.URI]...), '\n')
		locator.SizeBytes, locator.ContentHash = uint64(len(raw)), bytesSHA256(raw)
		loaded[locator.URI], loaded[locator.Kind] = raw, raw
		if err := verifyFinalCollectedPriorPhaseBytes(fixture.cfg, &prior, loaded); err == nil || isFinalSemanticAnalysisPending(err) {
			t.Errorf("rehashing prior %s bypassed original authority: %v", kind, err)
		}
	}
	prior := *fixture.prior
	prior.LiveChainBundles = nil
	if err := verifyFinalCollectedPriorPhaseBytes(fixture.cfg, &prior, fixture.loaded); err == nil || isFinalSemanticAnalysisPending(err) {
		t.Fatalf("missing native terminal census was pending: %v", err)
	}
}

// A valid but different completion is not the current phase's signed gate.
func TestFinalCaptureV2PendingPriorRejectsWrongHandoffAndSemanticRelabel(t *testing.T) {
	t.Parallel()
	fixture := newFinalPendingPriorV2TestFixture(t)
	result := *fixture.result
	gate := *result.PriorRelease
	gate.CompleteContentHash = bytesSHA256([]byte("another valid-looking closure"))
	result.PriorRelease = &gate
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(map[string][]byte, len(fixture.loaded))
	for name, source := range fixture.loaded {
		loaded[name] = source
	}
	loaded[fixture.current.ScenarioResult.URI] = raw
	if err := verifyFinalCollectedPendingPriorBinding(fixture.cfg, fixture.current, loaded); err == nil || isFinalSemanticAnalysisPending(err) {
		t.Fatalf("changed current handoff was deferred: %v", err)
	}
	for _, status := range []string{"semantic_verified", ""} {
		prior := *fixture.prior
		prior.SemanticStatus = status
		if err := verifyFinalCollectedPriorPhase(&prior, fixture.current); err == nil {
			t.Errorf("pending prior relabeled %q without semantic proof", status)
		}
	}
	current := *fixture.current
	current.Validators = []FinalCollectedValidatorInputs{{ValidatorID: 1}}
	if err := verifyFinalCollectedPriorPhase(fixture.prior, &current); err == nil {
		t.Fatal("legacy capture gained a pending semantic bypass")
	}
}

// The existing staging owner retains a finite immutable job, never FINAL or
// semantic_verified. A changed source job cannot overwrite the first capture.
func TestFinalCaptureV2PendingJobIsImmutableAndNeverAccepted(t *testing.T) {
	t.Parallel()
	fixture := newFinalPendingPriorV2TestFixture(t)
	if err := os.MkdirAll(finalSemanticSupplementStageRoot(fixture.stateRoot, fixture.priorResult.RunID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := retainFinalSemanticPendingJob(t.Context(), fixture.stateRoot, fixture.priorResult, fixture.closure); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(finalSemanticSupplementStageRoot(fixture.stateRoot, fixture.priorResult.RunID), finalSemanticPendingJobFilename)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var job finalSemanticPendingJob
	if err := decodeStrictJSONBytes(original, &job); err != nil || job.Status != finalSemanticCapturePendingStatus || job.ScenarioCompleteHash != fixture.closure.complete.ContentHash || job.CollectedInputsHash != fixture.closure.collected.EvidenceHash {
		t.Fatalf("pending job lost exact closure: %+v %v", job, err)
	}
	if err := retainFinalSemanticPendingJob(t.Context(), fixture.stateRoot, fixture.priorResult, fixture.closure); err != nil {
		t.Fatal(err)
	}
	closure := *fixture.closure
	complete := *closure.complete
	complete.ContentHash = bytesSHA256([]byte("changed closure"))
	closure.complete = &complete
	if err := retainFinalSemanticPendingJob(t.Context(), fixture.stateRoot, fixture.priorResult, &closure); err == nil {
		t.Fatal("pending job overwrote an immutable predecessor")
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatalf("pending job changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.priorRunRoot, finalSemanticSupplementFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending job was accepted: %v", err)
	}
}

// Pre-canceled analysis cannot leave a pending descriptor behind.
func TestFinalCaptureV2PendingJobCancellationCreatesNoMarker(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	root := t.TempDir()
	err := retainFinalSemanticPendingJob(ctx, root, &ScenarioResult{}, &finalSemanticOriginalClosure{complete: &ReleaseEvidenceEnvelope{}, manifest: &ReleaseEvidenceEnvelope{}, capture: &FinalSemanticCaptureStatus{}, collected: &FinalSemanticCollectedInputs{}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pending job changed state: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled pending job left output: %v %v", entries, err)
	}
}

// Source admission authenticates the exact prior gate before creating any
// destination artifact, independently of semantic interpreter availability.
func TestFinalCaptureV2PendingPriorRejectsWrongGateBeforeWrites(t *testing.T) {
	t.Parallel()
	fixture := newFinalPendingPriorV2TestFixture(t)
	result := *fixture.result
	gate := *result.PriorRelease
	gate.CompleteContentHash = bytesSHA256([]byte("foreign predecessor"))
	result.PriorRelease = &gate
	destination := t.TempDir()
	_, err := collectFinalPriorPhaseInputs(fixture.cfg, fixture.stateRoot, destination, &result)
	if err == nil || isFinalSemanticAnalysisPending(err) {
		t.Fatalf("foreign gate was accepted or deferred: %v", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid prior gate wrote partial raw capture: %v %v", entries, err)
	}
}

// The actual generic artifact walker sees every original locator, and no
// fabricated empty semantic locator is required to publish the pending graph.
func TestFinalCaptureV2PendingPriorArtifactCensusHasNoSemanticOutputs(t *testing.T) {
	t.Parallel()
	fixture := newFinalPendingPriorV2TestFixture(t)
	raw, err := json.Marshal(fixture.prior)
	if err != nil {
		t.Fatal(err)
	}
	references, err := campaignArtifactReferences(map[string][]byte{"prior.json": raw})
	if err != nil {
		t.Fatal(err)
	}
	want := finalCollectedPriorLocators(fixture.prior)
	if len(references) != len(want) {
		t.Fatalf("pending authority locator census=%d want=%d", len(references), len(want))
	}
	for _, locator := range want {
		reference, found := references[locator.URI]
		if !found || reference.Kind != locator.Kind || reference.ContentHash != locator.ContentHash || reference.Size != locator.SizeBytes {
			t.Fatalf("pending source vanished from public archive: %+v", locator)
		}
	}
}
