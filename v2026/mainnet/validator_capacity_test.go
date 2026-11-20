//go:build linux || darwin

// Actual public dispatch consumes exact signed config, stopped physical roots
// and retained ledger heads. Only synthetic kernel facts and keys are supplied.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The root contains two separately authenticated ledgers and the coordinator
// directory, exercising one shared physical lease without a self-deadlock.
type validatorCapacityCommandFixture struct {
	validator *bootstrapChainValidatorFixture
	storage   *durablefixture.Fixture
	request   validator.ProductionCapacityRequest
	root      string
	metadata  string
	path      string
}

func newValidatorCapacityCommandFixture(t *testing.T) *validatorCapacityCommandFixture {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	_, _, scope := newSubnetFixture(t)
	scope.RuntimeVersion.SpecName, scope.RuntimeVersion.TransactionVersion = "node-subtensor", 1
	chain := &bootstrapChainFixture{path: filepath.Join(parent, "synthetic-chain.json"), config: bootstrapChainConfig{DeploymentId: "synthetic-capacity-deployment"}}
	producer := newBootstrapChainValidatorFixture(t, chain, scope, 0)
	root := filepath.Dir(producer.config.StateDir)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(producer.config.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	request := validator.ProductionCapacityRequest{Schema: validator.ProductionCapacityRequestSchema,
		SuccessorApprovalPath: filepath.Join(parent, "successor-approval.json"), OriginalAuthorityPath: filepath.Join(parent, "original-authority.json"),
		FutureHistoryBytes: 2 * 1024 * 1024, FutureCaptureFiles: 32}
	for index, operator := range producer.config.Operators {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0xc1 + index)}, ed25519.SeedSize))
		identity := validator.AttemptLedgerIdentity{DeploymentID: producer.config.DeploymentID, ChainID: producer.config.ChainID, GenesisHash: producer.config.GenesisHash,
			Netuid: producer.config.Netuid, ValidatorID: producer.config.ValidatorID, ValidatorUID: uint16(index), NoID: operator.NoID,
			ValidatorVPK: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))}
		ledgerScope := validator.AttemptLedgerPreparationScope{Identity: identity, Coordinator: producer.config.Coordinator,
			Limits: producer.config.EvidenceV2.Bounds.Disk, ExpectedHead: validator.AttemptLedgerHead{Root: "0x" + strings.Repeat("00", 32)}}
		directory, err := os.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		census, buildErr := validator.BuildFreshAttemptLedgerPreparation(t.Context(), directory, filepath.Base(operator.StateDir), ledgerScope)
		if err := errors.Join(buildErr, directory.Close()); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(operator.StateDir)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, checkpointErr := validator.BuildAttemptLedgerPreparationCheckpoint(t.Context(), file, ledgerScope, census)
		if checkpointErr == nil {
			checkpointErr = durablesys.SetAttribute(int(file.Fd()), "user.urnetwork.attempt-ledger-custody", checkpoint, unix.XATTR_CREATE)
		}
		if err := errors.Join(checkpointErr, file.Sync(), file.Close()); err != nil {
			t.Fatal(err)
		}
		request.Sources = append(request.Sources, validator.ProductionCapacityInput{Ledger: ledgerScope, FutureRecords: 4, FutureTrails: 1,
			FutureRecordBytes: 4 * ledgerScope.Limits.MaxRecordBytes, FutureProofBytes: producer.config.EvidenceV2.Bounds.Replay.MaxProofBytes,
			FutureStorageBytes: 1024 * 1024, FutureStorageFiles: 16})
	}
	producer.publish(t)
	if _, err := validator.LoadReleaseConfig(producer.path); err != nil {
		t.Fatal("fixture did not load its exact original signed production config", err)
	}
	raw, err := os.ReadFile(producer.path)
	if err != nil {
		t.Fatal(err)
	}
	request.Config = validator.ReleaseEvidenceV2File{Path: producer.path, Bytes: uint64(len(raw)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(raw))}
	request.Bounds = producer.config.EvidenceV2.Bounds
	request.Bounds.Disk.MaxRecordCount *= 4
	request.Bounds.Disk.MaxTrailCount *= 4
	request.Bounds.Disk.MaxRawRecordBytes *= 4
	request.Bounds.Disk.MaxProofBytes *= 4
	request.Bounds.Disk.MaxStorageBytes *= 4
	request.Bounds.Disk.MaxStorageFiles *= 4
	request.Bounds.Replay.MaxTrails *= 4
	request.Bounds.Replay.MaxScratchBytes *= 4
	request.Bounds.Replay.MaxScratchFiles *= 4
	for _, stream := range []*validator.AttemptStreamV2Bounds{&request.Bounds.Cut.Records, &request.Bounds.Cut.Proofs} {
		stream.MaxItems *= 4
		stream.MaxDataBytes *= 4
		stream.MaxChunks = stream.MaxItems
		stream.MaxPages = stream.MaxChunks
	}
	request.Bounds.MaxHistoryBytes = 16 * 1024 * 1024
	request.Bounds.MaxCaptureFiles = producer.config.EvidenceV2.Bounds.CaptureFileLimit() * 2
	storage := durablefixture.New(t, t.Context(), root)
	request.Roots = []validator.ProductionCapacityRoot{{Path: root, FormerWriterFence: storageInspectionTestFence(t, storage, root),
		Limits: durablevolume.InventoryLimits{MaxEntries: 256, MaxBytes: 64 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 32, MaxOwnerAttributeBytes: 128 * 1024}}}
	return &validatorCapacityCommandFixture{validator: producer, storage: storage, request: request, root: root, metadata: parent, path: filepath.Join(parent, "capacity-request.json")}
}

// Rewriting an unsigned test request never modifies its independently signed
// original config, approval, physical root declaration or ledger checkpoint.
func (self *validatorCapacityCommandFixture) args(t *testing.T) []string {
	t.Helper()
	raw, err := json.Marshal(self.request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(self.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return []string{"validator-capacity-preview", "--request", self.path, "--request-sha256", durablefixture.Digest(raw)}
}

func TestValidatorCapacityPreviewBindsJoinedRetainedCensus(t *testing.T) {
	f := newValidatorCapacityCommandFixture(t)
	before := mainnetNamespaceTest(t, f.root)
	var first, second, diagnostic bytes.Buffer
	args := f.args(t)
	for _, output := range []*bytes.Buffer{&first, &second} {
		if code := runMain(f.storage.Context, args, output, &diagnostic); code != 0 {
			t.Fatal("public capacity preview refused original retained owners", code, diagnostic.String())
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("repeatable planning changed original custody or its unsigned draft")
	}
	var preview validator.ProductionCapacityPreview
	if err := json.Unmarshal(first.Bytes(), &preview); err != nil || preview.RestartAuthorized || len(preview.Ledgers) != 2 || len(preview.Physical) != 1 || preview.Config == nil {
		t.Fatal("preview lost joined physical/logical census or granted restart", err)
	}
	for _, path := range []string{f.request.SuccessorApprovalPath, f.request.OriginalAuthorityPath, f.validator.config.HotkeySeedFile} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("preview created authority or signing state", path, err)
		}
	}
	if err := os.WriteFile(preview.OriginalAuthority.Path, preview.OriginalAuthorityBytes, 0600); err != nil {
		t.Fatal(err)
	}
	// Only this explicit synthetic independent approval turns the draft into
	// loadable config. Its original economic body/window remains unchanged.
	oldApproval := f.validator.approval
	f.validator.config, f.validator.approval = *preview.Config, preview.Approval
	f.validator.path = filepath.Join(f.metadata, "signed-successor.yml")
	message, err := f.validator.approval.SigningMessage()
	if err != nil || "0x"+hex.EncodeToString(message) != preview.SigningBytes {
		t.Fatal("preview signing bytes differed from actual normalized config", err)
	}
	// Sign only the bytes emitted by the public command. Do not invoke the
	// fixture publish helper, which could rehash/normalize before signing and
	// conceal a proposal that changed through serialization.
	exportedMessage, err := hex.DecodeString(strings.TrimPrefix(preview.SigningBytes, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	f.validator.writeApproval(t, validator.OwnerRecycleApprovalEnvelope{Approval: preview.Approval,
		Signature: hex.EncodeToString(ed25519.Sign(f.validator.private, exportedMessage))})
	previewPath := filepath.Join(f.metadata, "original-preview.json")
	if err := os.WriteFile(previewPath, first.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	approvalRaw, err := os.ReadFile(f.validator.config.OwnerRecycleApproval.Approval.Path)
	if err != nil {
		t.Fatal(err)
	}
	completeArgs := []string{"validator-capacity-config", "--preview", previewPath, "--preview-sha256", monitorReadDigest(first.Bytes()),
		"--approval", f.validator.config.OwnerRecycleApproval.Approval.Path, "--approval-sha256", monitorReadDigest(approvalRaw), "--config-path", f.validator.path}
	var document bytes.Buffer
	if code := runMain(t.Context(), completeArgs, &document, &diagnostic); code != 0 {
		t.Fatal("public independent approval could not complete its exact config document", code, diagnostic.String())
	}
	if _, err := os.Lstat(f.validator.path); !os.IsNotExist(err) {
		t.Fatal("document completion wrote the requested path", err)
	}
	if err := os.WriteFile(f.validator.path, document.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := validator.LoadReleaseConfig(f.validator.path)
	if err != nil || loaded.ProductionCapacityRevision == nil || len(loaded.ProductionAuthorityHistory) != 1 {
		t.Fatal("public preview cannot become an independently approved resource successor", err)
	}
	loadedHash, err := validator.OwnerRecycleConfigHash(loaded)
	if err != nil || loadedHash != preview.Approval.ConfigHash {
		t.Fatal("wire reload changed the exact independently approved preview", err)
	}
	actual := f.validator.approval
	actual.ConfigHash, oldApproval.ConfigHash = [32]byte{}, [32]byte{}
	if !reflect.DeepEqual(actual, oldApproval) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("capacity preview changed original economic scope or owned bytes")
	}
}

// A public signature completes only its exact nominated document. Altering a
// view, economic field, predecessor, signer or byte pin never reaches stdout.
func TestValidatorCapacityConfigRefusesAuthorityDriftAndRetainsOutputCustody(t *testing.T) {
	f := newValidatorCapacityCommandFixture(t)
	before := mainnetNamespaceTest(t, f.root)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.storage.Context, f.args(t), &output, &diagnostic); code != 0 {
		t.Fatal("original public preview failed", code, diagnostic.String())
	}
	originalPreview := bytes.Clone(output.Bytes())
	var original validator.ProductionCapacityPreview
	if err := json.Unmarshal(originalPreview, &original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original.OriginalAuthority.Path, original.OriginalAuthorityBytes, 0600); err != nil {
		t.Fatal(err)
	}
	message, err := hex.DecodeString(strings.TrimPrefix(original.SigningBytes, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	envelope := validator.OwnerRecycleApprovalEnvelope{Approval: original.Approval, Signature: hex.EncodeToString(ed25519.Sign(f.validator.private, message))}
	// Use the same canonical envelope producer as the positive command path.
	// A valid completion must succeed before any negative fixture is trusted.
	f.validator.config = *original.Config
	f.validator.writeApproval(t, envelope)
	approvalPath := original.Config.OwnerRecycleApproval.Approval.Path
	approvalBytes, err := os.ReadFile(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	previewPath, configPath := filepath.Join(f.metadata, "complete-preview.json"), filepath.Join(f.metadata, "unwritten-config.yml")
	if err := os.WriteFile(previewPath, originalPreview, 0600); err != nil {
		t.Fatal(err)
	}
	validArgs := []string{"validator-capacity-config", "--preview", previewPath, "--preview-sha256", monitorReadDigest(originalPreview), "--approval", approvalPath, "--approval-sha256", monitorReadDigest(approvalBytes), "--config-path", configPath}
	output.Reset()
	diagnostic.Reset()
	if code := runMain(t.Context(), validArgs, &output, &diagnostic); code != 0 || output.Len() == 0 {
		t.Fatal("negative fixture never admitted its unchanged public baseline", code, diagnostic.String())
	}
	admittedPath := filepath.Join(f.metadata, "admitted-baseline.yml")
	if err := os.WriteFile(admittedPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validator.LoadReleaseConfig(admittedPath); err != nil {
		t.Fatal("negative fixture baseline did not pass the public config loader", err)
	}
	for _, fault := range []string{"view", "document", "message", "economic", "signer", "predecessor", "signature", "preview-digest", "approval-digest", "cancel", "short-output"} {
		var candidate validator.ProductionCapacityPreview
		if err := json.Unmarshal(originalPreview, &candidate); err != nil {
			t.Fatal(err)
		}
		approval := bytes.Clone(approvalBytes)
		expected := "capacity completion changes the reviewed document or signing bytes"
		switch fault {
		case "view":
			candidate.Config.PollSeconds++
		case "document":
			candidate.ConfigDocument += "\nunreviewed_field: true\n"
			expected = "field unreviewed_field not found"
		case "message":
			candidate.SigningBytes = "0x00"
		case "economic":
			candidate.Approval.MaximumOwnedHotkeys++
		case "signer":
			candidate.Config.OwnerRecycleApproval.Signer = "0x" + strings.Repeat("21", 32)
		case "predecessor":
			candidate.Config.ProductionAuthorityHistory = nil
		case "signature":
			bad := envelope
			bad.Signature = strings.Repeat("00", ed25519.SignatureSize)
			f.validator.writeApproval(t, bad)
			approval, err = os.ReadFile(approvalPath)
			if err != nil {
				t.Fatal(err)
			}
			expected = "approval signature differs"
		case "preview-digest", "approval-digest":
			expected = "plan reference exact file hash differs"
		case "cancel":
			expected = context.Canceled.Error()
		}
		previewBytes, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(previewPath, previewBytes, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(approvalPath, approval, 0600); err != nil {
			t.Fatal(err)
		}
		previewDigest, approvalDigest := monitorReadDigest(previewBytes), monitorReadDigest(approval)
		if fault == "preview-digest" {
			previewDigest = "sha256:" + strings.Repeat("12", 32)
		}
		if fault == "approval-digest" {
			approvalDigest = "sha256:" + strings.Repeat("12", 32)
		}
		args := []string{"validator-capacity-config", "--preview", previewPath, "--preview-sha256", previewDigest, "--approval", approvalPath, "--approval-sha256", approvalDigest, "--config-path", configPath}
		ctx, cancel := context.WithCancel(t.Context())
		if fault == "cancel" {
			cancel()
		}
		output.Reset()
		diagnostic.Reset()
		if fault == "short-output" {
			var short storageInspectionShortWriter
			if code := runMain(ctx, args, &short, &diagnostic); code != 1 || !strings.Contains(diagnostic.String(), io.ErrShortWrite.Error()) {
				t.Error("public completion did not report undelivered exact document", code, diagnostic.String())
			}
		} else if code := runMain(ctx, args, &output, &diagnostic); code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), expected) {
			t.Error("public completion refusal differs from the intended boundary", fault, code, diagnostic.String())
		}
		cancel()
		if _, err := os.Lstat(configPath); !os.IsNotExist(err) {
			t.Fatal("completion wrote its output pathname", fault, err)
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root)) {
			t.Fatal("completion changed original owned ledger history", fault)
		}
	}
}

func TestValidatorCapacityPreviewRequiresDeclarationAndExactInput(t *testing.T) {
	f := newValidatorCapacityCommandFixture(t)
	before := mainnetNamespaceTest(t, f.root)
	args := f.args(t)
	var output, diagnostic bytes.Buffer
	if code := runMain(t.Context(), args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "durable") {
		t.Fatal("actual command admitted absent physical authority", code, diagnostic.String())
	}
	args[len(args)-1] = "sha256:" + strings.Repeat("00", 32)
	diagnostic.Reset()
	if code := runMain(f.storage.Context, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "hash") && !strings.Contains(diagnostic.String(), "differ") {
		t.Fatal("actual command admitted unreviewed forecast bytes", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("refused preview modified retained state")
	}
}

func TestValidatorCapacityPreviewRefusesMissingCheckpointAndOwner(t *testing.T) {
	f := newValidatorCapacityCommandFixture(t)
	before := mainnetNamespaceTest(t, f.root)
	complete := f.request.Sources
	f.request.Sources = complete[:1]
	var output, diagnostic bytes.Buffer
	if code := runMain(f.storage.Context, f.args(t), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "complete") {
		t.Fatal("preview admitted only a subset of configured owners", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("incomplete owner census changed original custody")
	}
	f.request.Sources = complete
	operator := f.validator.config.Operators[1].StateDir
	if err := unix.Removexattr(operator, "user.urnetwork.attempt-ledger-custody"); err != nil {
		t.Fatal(err)
	}
	withoutCheckpoint := mainnetNamespaceTest(t, f.root)
	if reflect.DeepEqual(before, withoutCheckpoint) {
		t.Fatal("fixture did not remove the original checkpoint")
	}
	diagnostic.Reset()
	if code := runMain(f.storage.Context, f.args(t), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "original custody checkpoint") {
		t.Fatal("preview inferred fresh authority from missing original head", code, diagnostic.String())
	}
	if !reflect.DeepEqual(withoutCheckpoint, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("refused preview rewrote original ledger members")
	}
	owner, err := durablepath.OpenVolume(f.storage.Context, f.root, durablevolume.ReadWrite)
	if err != nil {
		t.Fatal("failed joined preview leaked a snapshot lease", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestValidatorCapacityPreviewRefusesActiveOwnerCancellationAndShortOutput(t *testing.T) {
	f := newValidatorCapacityCommandFixture(t)
	args := f.args(t)
	before := mainnetNamespaceTest(t, f.root)
	owner, err := durablepath.OpenVolume(f.storage.Context, f.root, durablevolume.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if preview, err := validator.BuildProductionCapacityPreview(f.storage.Context, f.request); !errors.Is(err, durablevolume.ErrBusy) || preview.Config != nil || preview.RestartAuthorized {
		t.Fatal("active owner was not refused by the typed snapshot admission", err)
	}
	code := runMain(f.storage.Context, args, &output, &diagnostic)
	closeErr := owner.Close()
	if code != 4 || output.Len() != 0 || diagnostic.Len() == 0 || closeErr != nil {
		t.Fatal("capacity planning observed an active writer or leaked its lease", code, diagnostic.String(), closeErr)
	}
	canceled, cancel := context.WithCancel(f.storage.Context)
	cancel()
	diagnostic.Reset()
	if code := runMain(canceled, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "canceled") {
		t.Fatal("canceled planning emitted a usable draft", code, diagnostic.String())
	}
	var short storageInspectionShortWriter
	diagnostic.Reset()
	if code := runMain(f.storage.Context, args, &short, &diagnostic); code != 1 || short.Len() == 0 || !strings.Contains(diagnostic.String(), "short write") {
		t.Fatal("incomplete output was treated as approved capacity", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("canceled or truncated preview changed original owners")
	}
}
