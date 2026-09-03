package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/server"
	"github.com/urnetwork/server/startifact"
)

type finalSemanticSupplementTestFixture struct {
	cfg       *ResolvedConfig
	roles     *RoleSecrets
	stateDir  string
	runDir    string
	result    *ScenarioResult
	load      FinalArtifactLoader
	stores    map[int]server.BlobStore
	storeRoot string
}

func TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper(t *testing.T) {
	fixture := newFinalSemanticSupplementTestFixture(t)
	dependencies := fixture.dependencies()
	first, err := publishOrResumeFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publishOrResumeFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != second.ContentHash || first.Signature != second.Signature {
		t.Fatalf("idempotent supplement changed identity: %s / %s", first.ContentHash, second.ContentHash)
	}
	var payload FinalSemanticSupplementPayload
	if err := decodeStrictJSONBytes(first.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != finalSemanticSupplementStatus || payload.ScenarioCompleteHash == "" || payload.ScenarioEvidenceManifestHash == "" || payload.CaptureStatusHash == "" || payload.CollectedInputsHash == "" || payload.SemanticEvidenceHash == "" || payload.PublicTranscriptHash == "" || len(payload.Files) != 2 {
		t.Fatalf("semantic supplement payload is incomplete: %+v", payload)
	}
	for index, entry := range payload.Files {
		if index > 0 && entry.Path <= payload.Files[index-1].Path {
			t.Fatal("semantic supplement files are not canonical")
		}
		for operator := 1; operator <= fixture.cfg.Config.Topology.Operators; operator++ {
			key, err := startifact.EvidenceContentKey(fixture.stores[operator], entry.EnvelopeHash)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := fixture.stores[operator].Get(context.Background(), key)
			if err != nil {
				t.Fatalf("operator %d omitted semantic file %s: %v", operator, entry.Path, err)
			}
			_ = reader.Close()
		}
	}
	markdownPath := filepath.Join(fixture.runDir, finalSemanticMarkdownFilename)
	originalMarkdown, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markdownPath, []byte("substituted loose report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := validateFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, dependencies); err == nil || !strings.Contains(err.Error(), "FINAL.md") {
		t.Fatalf("substituted loose FINAL.md was accepted: %v", err)
	}
	if err := os.WriteFile(markdownPath, originalMarkdown, 0o644); err != nil {
		t.Fatal(err)
	}
	brokenStores := make(map[int]server.BlobStore, len(fixture.stores))
	for operator, store := range fixture.stores {
		brokenStores[operator] = store
	}
	brokenStores[2] = &fixtureFailureBlobStore{store: brokenStores[2], readErr: errors.New("injected semantic replica read failure")}
	broken := dependencies
	broken.Stores = func(operator int) (server.BlobStore, error) { return brokenStores[operator], nil }
	if _, err := validateFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, broken); err == nil || !strings.Contains(err.Error(), "injected semantic replica read failure") {
		t.Fatalf("unreadable operator replica was accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(fixture.runDir, finalSemanticEvidenceFilename)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.runDir, finalSemanticMarkdownFilename)); err != nil {
		t.Fatal(err)
	}
	if _, err := validateFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, dependencies); err != nil {
		t.Fatalf("owner-signed file envelopes did not validate without optional loose outputs: %v", err)
	}
}

func TestFinalSemanticSupplementFailedReplicaDoesNotCommitAndRetryReusesStage(t *testing.T) {
	fixture := newFinalSemanticSupplementTestFixture(t)
	goodSecond := fixture.stores[2]
	fixture.stores[2] = &fixtureFailureBlobStore{store: goodSecond, writeErr: errors.New("injected semantic replica write failure")}
	dependencies := fixture.dependencies()
	if _, err := publishOrResumeFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, dependencies); err == nil || !strings.Contains(err.Error(), "injected semantic replica write failure") {
		t.Fatalf("replica write failure was not propagated: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.runDir, finalSemanticSupplementFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed publication exposed semantic_verified marker: %v", err)
	}
	stagePath := filepath.Join(finalSemanticSupplementStageRoot(fixture.stateDir, fixture.result.RunID), finalSemanticSupplementStageFilename)
	var staged ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(stagePath, &staged); err != nil {
		t.Fatal(err)
	}
	fixture.stores[2] = goodSecond
	dependencies = fixture.dependencies()
	committed, err := publishOrResumeFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if committed.ContentHash != staged.ContentHash || committed.Signature != staged.Signature {
		t.Fatalf("retry replaced immutable staged supplement: staged=%s committed=%s", staged.ContentHash, committed.ContentHash)
	}
}

func TestFinalSemanticSupplementPartialOutputRecoveryPreservesEitherHalf(t *testing.T) {
	for _, name := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		t.Run(name, func(t *testing.T) {
			stateDir := t.TempDir()
			runID := "partial-recovery-run"
			runDir := filepath.Join(stateDir, "runs", runID)
			if err := os.MkdirAll(runDir, 0o700); err != nil {
				t.Fatal(err)
			}
			content := []byte("crash-left " + name + "\n")
			partialPath := filepath.Join(runDir, name)
			if err := os.WriteFile(partialPath, content, 0o644); err != nil {
				t.Fatal(err)
			}
			regenerated := 0
			regenerate := func() error {
				regenerated++
				if err := os.WriteFile(filepath.Join(runDir, finalSemanticEvidenceFilename), []byte("regenerated evidence\n"), 0o644); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(runDir, finalSemanticMarkdownFilename), []byte("regenerated markdown\n"), 0o644)
			}
			if err := recoverOrGenerateFinalSemanticOutputPair(context.Background(), stateDir, runDir, runID, regenerate); err != nil {
				t.Fatal(err)
			}
			if regenerated != 1 {
				t.Fatalf("partial output regeneration calls = %d, want 1", regenerated)
			}
			for _, output := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
				if exists, err := finalSemanticRegularFileExists(filepath.Join(runDir, output)); err != nil || !exists {
					t.Fatalf("regeneration did not restore %s: exists=%t err=%v", output, exists, err)
				}
			}
			digest := strings.TrimPrefix(bytesSHA256(content), "sha256:")
			preserved := filepath.Join(finalSemanticSupplementStageRoot(stateDir, runID), "recovery", name+"."+digest+".partial")
			got, err := os.ReadFile(preserved)
			if err != nil || string(got) != string(content) {
				t.Fatalf("partial output was not preserved exactly: %q %v", got, err)
			}
		})
	}
}

func TestFinalSemanticSupplementEnumeratesEveryDerivedOutput(t *testing.T) {
	runDir := t.TempDir()
	contents := map[string][]byte{
		finalSemanticEvidenceFilename:       []byte("evidence\n"),
		finalSemanticMarkdownFilename:       []byte("report\n"),
		"final-derived/a.json":              []byte("{\"a\":1}\n"),
		"final-derived/nested/receipt.json": []byte("{\"receipt\":true}\n"),
	}
	for name, content := range contents {
		path := filepath.Join(runDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := enumerateFinalSemanticRawFiles(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(contents) {
		t.Fatalf("semantic output census=%d, want %d", len(files), len(contents))
	}
	for index, file := range files {
		if index > 0 && file.Path <= files[index-1].Path {
			t.Fatalf("semantic output census is not sorted: %+v", files)
		}
		content, ok := contents[file.Path]
		if !ok || file.ContentHash != bytesSHA256(content) || string(file.Data) != string(content) {
			t.Fatalf("semantic output %q differs from exact derived bytes", file.Path)
		}
	}
}

func TestFinalSemanticSupplementTreatsLooseOutputsAsOptionalComparands(t *testing.T) {
	runDir := t.TempDir()
	expected := []finalSemanticRawFile{{Path: finalSemanticEvidenceFilename, ContentHash: bytesSHA256([]byte("signed\n")), Data: []byte("signed\n")}}
	loose, err := enumeratePresentFinalSemanticRawFiles(runDir)
	if err != nil || len(loose) != 0 || finalSemanticLooseRawFileMismatch(expected, loose) != "" {
		t.Fatalf("absent optional loose outputs were rejected: files=%v err=%v", loose, err)
	}
	path := filepath.Join(runDir, finalSemanticEvidenceFilename)
	if err := os.WriteFile(path, []byte("signed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loose, err = enumeratePresentFinalSemanticRawFiles(runDir)
	if err != nil || finalSemanticLooseRawFileMismatch(expected, loose) != "" {
		t.Fatalf("matching optional loose output was rejected: files=%v err=%v", loose, err)
	}
	if err := os.WriteFile(path, []byte("substituted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loose, err = enumeratePresentFinalSemanticRawFiles(runDir)
	if err != nil || finalSemanticLooseRawFileMismatch(expected, loose) != finalSemanticEvidenceFilename {
		t.Fatalf("substituted optional loose output was accepted: files=%v err=%v", loose, err)
	}
}

func TestFinalSemanticSupplementPartialOutputRecoveryHonorsCancellation(t *testing.T) {
	stateDir := t.TempDir()
	runID := "canceled-partial-recovery-run"
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	partialPath := filepath.Join(runDir, finalSemanticEvidenceFilename)
	content := []byte("preserve on cancellation\n")
	if err := os.WriteFile(partialPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	regenerated := false
	if err := recoverOrGenerateFinalSemanticOutputPair(ctx, stateDir, runDir, runID, func() error { regenerated = true; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled recovery did not stop promptly: %v", err)
	}
	if regenerated {
		t.Fatal("canceled recovery invoked regeneration")
	}
	got, err := os.ReadFile(partialPath)
	if err != nil || string(got) != string(content) {
		t.Fatalf("canceled recovery changed the partial output: %q %v", got, err)
	}
}

func TestFinalSemanticSupplementPublicationLockHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "publish.lock")
	held, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(held.Fd()), syscall.LOCK_UN) //nolint:errcheck
	waiting, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer waiting.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(entered)
		finished <- lockFinalSemanticSupplement(ctx, waiting)
	}()
	<-entered
	// The held descriptor must keep the waiter active for multiple retry
	// intervals. This distinguishes cancellation-aware nonblocking polling
	// from an entry-only context check followed by a blocking Flock.
	select {
	case err := <-finished:
		t.Fatalf("publication lock waiter returned before cancellation: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	started := time.Now()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled lock wait returned %v", err)
		}
		if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
			t.Fatalf("canceled lock wait took %s", elapsed)
		}
	case <-time.After(250 * time.Millisecond):
		// Release the fixture lock so a regressed blocking waiter cannot leak
		// after the test reports the failure.
		_ = syscall.Flock(int(held.Fd()), syscall.LOCK_UN)
		<-finished
		t.Fatal("publication lock waiter ignored cancellation")
	}
}

func TestValidateFinalSemanticSupplementDefaultCapturedStoresRequiresEveryReplica(t *testing.T) {
	fixture := newFinalSemanticSupplementTestFixture(t)
	if _, err := publishOrResumeFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, fixture.dependencies()); err != nil {
		t.Fatal(err)
	}
	broken := make(map[int]server.BlobStore, len(fixture.stores))
	for operator, store := range fixture.stores {
		broken[operator] = store
	}
	broken[2] = &fixtureFailureBlobStore{store: broken[2], readErr: errors.New("missing default semantic replica")}
	resolved := false
	dependencies := finalSemanticSupplementDependencies{
		Load: fixture.load,
		ResolveCapturedStores: func(context.Context, *ResolvedConfig, string, string) (map[int]server.BlobStore, error) {
			resolved = true
			return broken, nil
		},
	}
	if _, err := validateFinalSemanticSupplement(context.Background(), fixture.cfg, fixture.roles, fixture.stateDir, fixture.runDir, fixture.result, dependencies); err == nil || !strings.Contains(err.Error(), "missing default semantic replica") {
		t.Fatalf("missing default replica was accepted: %v", err)
	}
	if !resolved {
		t.Fatal("default validation did not resolve stores from the captured graph")
	}
}

func TestFinalSemanticSupplementRacePublicationResumeAndTamperCoverage(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "bounded-semantic-publication-race"
	payload := finalSemanticSupplementFilePayload{
		Schema: finalSemanticSupplementFileSchema, RunID: runID, Path: finalSemanticMarkdownFilename,
		ContentHash: bytesSHA256([]byte("# verified\n")), Size: uint64(len("# verified\n")), Data: []byte("# verified\n"),
	}
	envelope, err := signEvidence(cfg, finalSemanticSupplementFileKind, runID, payload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(t.TempDir(), prefix)
	}
	start := make(chan struct{})
	errorsByPublisher := make(chan error, 16)
	var wait sync.WaitGroup
	for publisher := 0; publisher < cap(errorsByPublisher); publisher++ {
		wait.Add(1)
		go func(publisher int) {
			defer wait.Done()
			<-start
			operator := publisher%cfg.Config.Topology.Operators + 1
			errorsByPublisher <- publishAndReadBackFinalSemanticEnvelope(context.Background(), stores[operator], envelope)
		}(publisher)
	}
	close(start)
	wait.Wait()
	close(errorsByPublisher)
	for err := range errorsByPublisher {
		if err != nil {
			t.Fatalf("concurrent idempotent publication failed: %v", err)
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		if err := publishAndReadBackFinalSemanticEnvelope(context.Background(), stores[operator], envelope); err != nil {
			t.Fatalf("operator %d resume publication failed: %v", operator, err)
		}
		if err := readBackFinalSemanticEnvelope(context.Background(), stores[operator], envelope); err != nil {
			t.Fatalf("operator %d resumed replica read-back failed: %v", operator, err)
		}
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(roles.EVM["testnet-owner"].PrivateKeyHex, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := *envelope
	tampered.Payload = append([]byte(nil), envelope.Payload...)
	tampered.Payload[len(tampered.Payload)-1] ^= 1
	if err := verifyFinalSemanticOwnerEnvelope(cfg, &tampered, &ownerKey.PublicKey, finalSemanticSupplementFileKind, runID); err == nil {
		t.Fatal("tampered owner-carried semantic file envelope was accepted")
	}
	prefix, err := operatorArtifactPrefix(cfg.Config, 1)
	if err != nil {
		t.Fatal(err)
	}
	missing := server.NewLocalBlobStore(t.TempDir(), prefix)
	if err := readBackFinalSemanticEnvelope(context.Background(), missing, envelope); err == nil {
		t.Fatal("missing semantic replica was accepted")
	}
}

func TestFinalSemanticSupplementStoreCensusRejectsMissingOperator(t *testing.T) {
	cfg := testResolvedConfig(t)
	prefix, err := operatorArtifactPrefix(cfg.Config, 1)
	if err != nil {
		t.Fatal(err)
	}
	stores := map[int]server.BlobStore{1: server.NewLocalBlobStore(t.TempDir(), prefix)}
	if err := validateFinalSemanticSupplementStores(cfg, stores); err == nil || !strings.Contains(err.Error(), "census") {
		t.Fatalf("incomplete semantic supplement store census was accepted: %v", err)
	}
}

func TestFinalSemanticCapturedDependenciesIgnoreConcurrentMutablePointers(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://substrate.example"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://evm.example"
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCapturedPublicationPlan(cfg, planBytes); err != nil {
		t.Fatal(err)
	}
	redirected := *cfg
	redirected.ObjectStoreHost = "redirected-object-store.invalid"
	if err := verifyFinalSemanticCapturedPublicationPlan(&redirected, planBytes); err == nil || !strings.Contains(err.Error(), "endpoint inputs") {
		t.Fatalf("mutable object-store redirect was accepted: %v", err)
	}
	capturedPublic := finalSemanticCapturedPublicFixture(t, cfg, roles)
	stateDir := t.TempDir()
	entries := make([]RuntimeConfigFile, 0, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		relative := filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("operator-%d", operator), "vault", "minio.yml"))
		raw := []byte(fmt.Sprintf("authority: 127.0.0.1:%d\naccess_key: access-%d\nsecret_key: secret-%d\nbucket: blob\ntls: false\nprefix: %s\n", 23900+operator, operator, operator, prefix))
		path := filepath.Join(stateDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, RuntimeConfigFile{Path: relative, SHA256: bytesSHA256(raw), Mode: "0600"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	capturedRuntime := &RuntimeConfigManifest{Schema: runtimeConfigManifestSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Files: entries}
	capturedRuntime.ManifestHash, err = runtimeConfigManifestHash(*capturedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	mutablePublic := filepath.Join(stateDir, "public.json")
	mutableRuntime := filepath.Join(stateDir, "runtime-config-manifest.json")
	stop := make(chan struct{})
	errCh := make(chan error, 1)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		for revision := 1; ; revision++ {
			select {
			case <-stop:
				return
			default:
			}
			wire := []byte(fmt.Sprintf("{\"unrelated_revision\":%d}\n", revision))
			if err := atomicWrite(mutablePublic, wire, 0o644); err != nil {
				errCh <- err
				return
			}
			if err := atomicWrite(mutableRuntime, wire, 0o600); err != nil {
				errCh <- err
				return
			}
		}
	}()
	for attempt := 0; attempt < 5; attempt++ {
		if factory, err := finalSemanticReaderFactoryFromCapturedFiles(cfg, capturedPublic); err != nil || factory == nil {
			close(stop)
			wait.Wait()
			t.Fatalf("captured reader was blocked by mutable pointers: %v", err)
		}
		stores, err := finalSemanticStoresFromCapturedRuntimeManifest(cfg, stateDir, capturedRuntime)
		if err != nil || len(stores) != cfg.Config.Topology.Operators {
			close(stop)
			wait.Wait()
			t.Fatalf("captured BlobStore config was blocked by mutable pointers: stores=%d err=%v", len(stores), err)
		}
	}
	close(stop)
	wait.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
	firstPath := filepath.Join(stateDir, "runtime", "operator-1", "vault", "minio.yml")
	if err := os.WriteFile(firstPath, []byte("substituted: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := finalSemanticStoresFromCapturedRuntimeManifest(cfg, stateDir, capturedRuntime); err == nil || !strings.Contains(err.Error(), "captured digest") {
		t.Fatalf("changed private BlobStore config was accepted: %v", err)
	}
}

func finalSemanticCapturedPublicFixture(t *testing.T, cfg *ResolvedConfig, roles *RoleSecrets) map[string][]byte {
	t.Helper()
	minimumEVM := 5 + 4*cfg.Config.Topology.Operators
	minimumSubstrate := 2 + 2*cfg.Config.Topology.fleetCandidates() + 2*cfg.Config.Topology.ChurnFloorUIDs + 3*cfg.Config.Topology.Operators + (2*cfg.Config.Topology.Validators - 1) + cfg.Config.Topology.Miners
	minimumClients := cfg.Config.Topology.Miners + cfg.Config.Topology.Validators*cfg.Config.Topology.Operators
	identities := struct {
		Schema       string                       `json:"schema"`
		DeploymentID string                       `json:"deployment_id"`
		EVM          map[string]string            `json:"evm"`
		Substrate    map[string]map[string]string `json:"substrate"`
		Clients      map[string]map[string]string `json:"clients"`
	}{Schema: "urnetwork-sim-public-identities-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, EVM: map[string]string{}, Substrate: map[string]map[string]string{}, Clients: map[string]map[string]string{}}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		identities.EVM[fmt.Sprintf("operator-%d-artifact", operator)] = roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)].Address
	}
	for index := len(identities.EVM); index < minimumEVM; index++ {
		identities.EVM[fmt.Sprintf("dummy-%04d", index)] = fmt.Sprintf("0x%040x", index+1)
	}
	for index := 0; index < minimumSubstrate; index++ {
		identities.Substrate[fmt.Sprintf("dummy-%04d", index)] = map[string]string{"public_key": finalTestHex(byte(index + 1))}
	}
	for index := 0; index < minimumClients; index++ {
		identities.Clients[fmt.Sprintf("dummy-%04d", index)] = map[string]string{"client_id": fmt.Sprintf("0x%032x", index+1), "client_key": fmt.Sprintf("0x%064x", index+1)}
	}
	identityBytes, err := json.Marshal(identities)
	if err != nil {
		t.Fatal(err)
	}
	operators := make([]PublicOperator, 0, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		operators = append(operators, PublicOperator{NoID: operator, APIURL: fmt.Sprintf("https://operator-%d.example", operator)})
	}
	public := &PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", Release: "1.0", DeploymentID: cfg.Config.Deployment.DeploymentID, Revision: 1,
		GeneratedAt: time.Unix(1_700_000_000, 0).UTC().Format(time.RFC3339Nano), ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash,
		RuntimeSpec: cfg.Public.Chain.ExpectedRuntimeSpec, TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: cfg.Public.Chain.ExpectedStateVersion,
		RuntimeCodeHash: cfg.Release.Runtime.CodeHash, RuntimeMetadataHash: cfg.Release.Runtime.MetadataHash,
		Netuid: cfg.Netuid, EVMRPC: cfg.Public.Chain.EVMPublicReadEndpoint, SubstrateRPC: cfg.Public.Chain.SubstratePublicReadEndpoint, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash,
		PlanHash: finalTestHex(0x44), Contracts: &ContractDeployment{DeploymentID: cfg.Config.Deployment.DeploymentID}, Identities: identityBytes,
		Operators: operators, Topology: cfg.Config.Topology,
	}
	publicBytes, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	manifestHash, err := canonicalHashHex(public)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"launch-foundation/public.json": publicBytes}
	locators := deploymentManifestLocatorDirectory{Schema: "urnetwork-public-manifest-locators-v1", ManifestHash: manifestHash, ManifestRevision: 1}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		envelope, err := signEvidence(cfg, "deployment-manifest", "", public, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		files[fmt.Sprintf("public/deployment-manifest.operator-%d.evidence.json", operator)] = finalSemanticSupplementEnvelopeJSON(t, envelope)
		locators.Locators = append(locators.Locators, deploymentManifestLocator{OperatorNoID: operator, ContentHash: envelope.ContentHash, URL: operators[operator-1].APIURL + "/sn/evidence?hash=" + envelope.ContentHash})
	}
	files["public/deployment-manifest.locators.json"] = finalSemanticSupplementJSON(t, locators)
	return files
}

func newFinalSemanticSupplementTestFixture(t *testing.T) *finalSemanticSupplementTestFixture {
	t.Helper()
	source, artifacts := finalSemanticFixture(t)
	cfg := testResolvedConfig(t)
	policy, err := finalSemanticFixturePolicy(&source, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Deployment.DeploymentID = source.DeploymentID
	cfg.Config.Scenarios.ShortEpochs = int(source.Window.EpochCount)
	cfg.Config.Topology.Operators = source.ExpectedOperators
	cfg.Config.Topology.Validators = source.ExpectedValidators
	cfg.Config.Topology.Miners = source.ExpectedMiners
	cfg.ConfigHash = source.ConfigHash
	cfg.Policy = policy
	cfg.PolicyHash = source.PolicyHash
	cfg.ChainID = source.ChainID
	cfg.Netuid = source.Netuid
	cfg.Public.Chain.GenesisHash = source.GenesisHash
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source.Deployment.GovernanceOwner = strings.ToLower(roles.EVM["testnet-owner"].Address)
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: source.RunID,
		DeploymentID: source.DeploymentID, Name: source.Phase, ConfigHash: source.ConfigHash,
		PolicyHash: source.PolicyHash, ChainID: source.ChainID, GenesisHash: source.GenesisHash, Netuid: source.Netuid,
		StartedAt: source.CampaignStartedAt, CompletedAt: source.CampaignCompletedAt,
		CampaignStartHead: source.EVMCampaignStartHead, StartHead: source.Window.BaselineHead,
		EndHead: source.EVMTerminalHead, StartEpoch: source.Window.BaselineEpoch,
		EndEpoch: source.Window.FirstEpoch + source.Window.EpochCount, AcceptanceWindow: &source.Window,
		Assertions: []AssertionRecord{}, ValueReconciliation: map[string]string{}, Result: "pass",
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	source.ResultHash = result.EvidenceHash
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", result.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	resultWire := finalSemanticSupplementJSON(t, result)
	if err := atomicWrite(filepath.Join(runDir, "result.json"), resultWire, 0o644); err != nil {
		t.Fatal(err)
	}
	collected := finalSemanticSupplementCollectedFixture(t, cfg, source, result, resultWire)
	collectedWire := finalSemanticSupplementJSON(t, collected)
	if err := atomicWrite(filepath.Join(runDir, "final-inputs", "manifest.json"), collectedWire, 0o644); err != nil {
		t.Fatal(err)
	}
	manifestLocator := FinalArtifactLocator{Kind: "final-semantic-input-manifest", URI: "final-inputs/manifest.json", ContentHash: bytesSHA256(collectedWire), SizeBytes: uint64(len(collectedWire))}
	capture := finalSemanticCaptureStatus(result, collected, manifestLocator)
	capture.EvidenceHash, err = finalSemanticCaptureStatusHash(capture)
	if err != nil {
		t.Fatal(err)
	}
	captureWire := finalSemanticSupplementJSON(t, capture)
	if err := atomicWrite(filepath.Join(runDir, finalSemanticCaptureStatusFilename), captureWire, 0o644); err != nil {
		t.Fatal(err)
	}
	rawFiles := map[string][]byte{
		"result.json": resultWire, "final-inputs/manifest.json": collectedWire,
		finalSemanticCaptureStatusFilename: captureWire,
	}
	fileHashes := make(map[string]string, len(rawFiles))
	entries := make([]campaignEvidenceFileEntry, 0, len(rawFiles))
	for name, data := range rawFiles {
		fileHashes[name] = bytesSHA256(data)
		entries = append(entries, campaignEvidenceFileEntry{Path: name, ContentHash: bytesSHA256(data), Size: uint64(len(data)), EnvelopeHash: bytesSHA256([]byte("owner-file-envelope:" + name))})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	bundleHash := bytesSHA256([]byte("scenario bundle payload"))
	manifestPayload := campaignEvidenceManifestPayload{
		Schema: campaignEvidenceManifestSchema, DeploymentID: source.DeploymentID, ChainID: source.ChainID,
		GenesisHash: strings.ToLower(source.GenesisHash), Netuid: source.Netuid, RunID: source.RunID,
		ResultHash: result.EvidenceHash, BundlePayloadHash: bundleHash, Files: entries,
	}
	owner := roles.EVM["testnet-owner"]
	manifest, err := signEvidence(cfg, campaignEvidenceManifestKind, result.RunID, manifestPayload, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, campaignEvidenceManifestFilename), finalSemanticSupplementEnvelopeJSON(t, manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{
		ResultHash: result.EvidenceHash, Files: fileHashes, BundlePayloadHash: bundleHash, EvidenceManifestHash: manifest.ContentHash,
	}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, "complete.json"), finalSemanticSupplementEnvelopeJSON(t, complete), 0o644); err != nil {
		t.Fatal(err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		commit, err := signEvidence(cfg, "scenario-complete-commit", result.RunID, complete, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(runDir, fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator)), finalSemanticSupplementEnvelopeJSON(t, commit), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	load := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, ok := artifacts[locator.URI]
		if !ok {
			return nil, fmt.Errorf("missing fixture artifact %s", locator.URI)
		}
		return append([]byte(nil), data...), nil
	}
	reader := func(_ context.Context, draft *FinalSemanticEvidence) (FinalSemanticChainReader, error) {
		return &finalTestChainReader{evidence: draft}, nil
	}
	if _, err := ProduceFinalSemanticOutputs(context.Background(), runDir, source, load, reader, func(string, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(stateDir, "object-store")
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(storeRoot, prefix)
	}
	return &finalSemanticSupplementTestFixture{cfg: cfg, roles: roles, stateDir: stateDir, runDir: runDir, result: result, load: load, stores: stores, storeRoot: storeRoot}
}

func (fixture *finalSemanticSupplementTestFixture) dependencies() finalSemanticSupplementDependencies {
	return finalSemanticSupplementDependencies{
		Load: fixture.load,
		Stores: func(operator int) (server.BlobStore, error) {
			return fixture.stores[operator], nil
		},
		VerifyRuntime: func(*ResolvedConfig, string) error { return nil },
	}
}

func finalSemanticSupplementCollectedFixture(t *testing.T, cfg *ResolvedConfig, source FinalSemanticEvidence, result *ScenarioResult, resultWire []byte) *FinalSemanticCollectedInputs {
	t.Helper()
	locator := func(kind, name string) FinalArtifactLocator {
		data := []byte(kind + ":" + name)
		return FinalArtifactLocator{Kind: kind, URI: name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	}
	collected := &FinalSemanticCollectedInputs{
		Schema: finalSemanticCollectedInputsSchema, Phase: result.Name, RunID: result.RunID,
		ResultHash: result.EvidenceHash, Window: source.Window, Policy: source.PolicyArtifact,
		ScenarioResult:      FinalArtifactLocator{Kind: "scenario-result-candidate", URI: "result.json", ContentHash: bytesSHA256(resultWire), SizeBytes: uint64(len(resultWire))},
		TerminalObservation: locator("scenario-terminal-observation", "final-inputs/terminal-observation.json"),
		ObservationHistory:  locator("scenario-observation-history", "final-inputs/observation-history.json"),
		ClosedInputBundles:  []FinalArtifactLocator{locator("closed-input-bundle", "final-inputs/bundles/closed.json")},
	}
	lastEpoch := source.Window.FirstEpoch + source.Window.EpochCount - 1
	for epoch := source.Window.FirstEpoch - 1; epoch <= lastEpoch; epoch++ {
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			name := fmt.Sprintf("final-inputs/payouts/no-%d-epoch-%d.json", noID, epoch)
			collected.Payouts = append(collected.Payouts, FinalCollectedPayoutArtifact{NoID: uint64(noID), Epoch: epoch, Artifact: locator("payout-artifact", name)})
		}
	}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		validator := FinalCollectedValidatorInputs{ValidatorID: uint64(validatorID), PathVPK: finalTestHex(byte(0x70 + validatorID)), IntentStore: locator("validator-steering-intent-store", fmt.Sprintf("final-inputs/validators/%d-intents.json", validatorID))}
		for index, epoch := 0, source.Window.FirstEpoch; epoch <= lastEpoch; index, epoch = index+1, epoch+1 {
			validator.Intents = append(validator.Intents, FinalCollectedValidatorIntent{
				Sequence: uint64(index + 1), SettlementEpoch: epoch, SubnetEpoch: epoch, Status: "applied", VectorHash: finalTestHex(byte(0x20 + validatorID + index)),
				Artifact:    locator("steering-intent", fmt.Sprintf("final-inputs/validators/%d-intent-%d.json", validatorID, epoch)),
				Measurement: locator("validator-release-measurement", fmt.Sprintf("final-inputs/validators/%d-measurement-%d.json", validatorID, epoch)),
				Envelope:    locator("validator-release-measurement-envelope", fmt.Sprintf("final-inputs/validators/%d-envelope-%d.json", validatorID, epoch)),
			})
		}
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			validator.Attempts = append(validator.Attempts, FinalCollectedValidatorAttempts{NoID: uint64(noID), RecordCount: 1, CompleteCount: 1, Artifact: locator("validator-attempt-records", fmt.Sprintf("final-inputs/validators/%d-attempts-%d.json", validatorID, noID))})
			validator.PathProofs = append(validator.PathProofs, FinalCollectedValidatorPathProof{NoID: uint64(noID), FirstEpoch: source.Window.FirstEpoch, LastEpoch: lastEpoch, ProofCount: source.Window.EpochCount, Artifact: locator("validator-path-proofs", fmt.Sprintf("final-inputs/validators/%d-proofs-%d.json", validatorID, noID))})
		}
		collected.Validators = append(collected.Validators, validator)
	}
	var err error
	collected.EvidenceHash, err = finalSemanticCollectedInputsHash(collected)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, collected); err != nil {
		t.Fatal(err)
	}
	return collected
}

func finalSemanticSupplementJSON(t *testing.T, value any) []byte {
	t.Helper()
	wire, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(wire, '\n')
}

func finalSemanticSupplementEnvelopeJSON(t *testing.T, value any) []byte {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append(wire, '\n')
}
