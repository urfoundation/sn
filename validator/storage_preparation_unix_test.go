//go:build linux || darwin

// The real public plan/apply command prepares parent and ledger directories.
// Only mount/UUID observations are synthetic; all file and database I/O is real.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/internal/durablefixture"
	"github.com/urnetwork/connect/durablevolume"
)

// Before this owner existed, preparing only a child ledger left its parent
// unavailable to the production lifecycle. One declaration must admit both.
func TestValidatorStoragePreparationOpensActualParentAndLedger(t *testing.T) {
	ctx, request, root, scope, key := validatorRuntimePreparationFixture(t)
	var output, diagnostic bytes.Buffer
	if code := runValidatorStoragePrepare(ctx, []string{"plan", "--request=" + request.Path, "--request-sha256=" + request.Sha256}, &output, &diagnostic); code != 0 {
		t.Fatalf("public plan failed: %s", diagnostic.String())
	}
	planRaw := bytes.Clone(output.Bytes())
	planPath := filepath.Join(filepath.Dir(request.Path), "accepted-plan.json")
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code := runValidatorStoragePrepare(ctx, []string{"apply", "--plan=" + planPath, "--plan-sha256=" + validatorPreparationDigest(planRaw)}, &output, &diagnostic); code != 0 {
		t.Fatalf("public apply failed: %s", diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized || result.Declaration.Path == "" {
		t.Fatalf("storage result: %v %s", err, output.String())
	}
	ctx = durablevolume.WithReference(ctx, result.Declaration)
	parent, err := openValidatorDurableDirectory(ctx, filepath.Join(root, "state"), durablevolume.ReadWrite, false)
	if err != nil || parent == nil {
		t.Fatalf("prepared parent is unavailable to actual runtime owner: %v", err)
	}
	defer parent.Close()
	entry := scope.Ledgers[0]
	opState := filepath.Join(root, entry.Directory)
	directory, err := os.Open(opState)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := InspectAttemptLedgerPreparation(ctx, directory, entry.Ledger)
	if err != nil || observed.Head != entry.Ledger.ExpectedHead {
		t.Fatalf("prepared child identity or head changed: %v", err)
	}
	expected, err := BuildAttemptLedgerPreparationCheckpoint(ctx, directory, entry.Ledger, observed)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := readAttemptLedgerCustodyAttribute(directory)
	if err != nil || !bytes.Equal(checkpoint, expected) {
		t.Fatalf("prepared child has no actual target-inode checkpoint: %v", err)
	}
	directory.Close()
	ledger, err := NewDiskAttemptLedger(ctx, opState, entry.Ledger.Identity, entry.Ledger.Coordinator, key, entry.Ledger.Limits)
	if err != nil {
		t.Fatalf("prepared child cannot open the actual production disk ledger: %v", err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"evidence/replay", "evidence/seal"} {
		directory, err := openValidatorDurableDirectory(ctx, filepath.Join(root, path), durablevolume.ReadWrite, false)
		if err != nil {
			t.Fatalf("prepared scratch is unavailable: %v", err)
		}
		directory.Close()
	}
}

// A zero-owner request and a foreign owner cannot stand in for this tree.
func TestValidatorStoragePreparationRefusesForeignOwner(t *testing.T) {
	ctx, request, _, _, _ := validatorRuntimePreparationFixture(t)
	raw, err := os.ReadFile(request.Path)
	if err != nil {
		t.Fatal(err)
	}
	var changed durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Owners[0].Kind = "synthetic-foreign-owner"
	raw, err = json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := runValidatorStoragePrepare(ctx, []string{"plan", "--request=" + request.Path, "--request-sha256=" + validatorPreparationDigest(raw)}, &output, &diagnostic); code == 0 {
		t.Fatal("foreign owner was admitted")
	}
}

// Traversal, parent omissions and hidden descendants are refused before staging.
func TestValidatorStoragePreparationRejectsAmbiguousPaths(t *testing.T) {
	_, _, _, original, _ := validatorRuntimePreparationFixture(t)
	for _, replacement := range []string{"../escape", "/absolute", "state/no-3/hidden", "state/../alias"} {
		scope := original
		scope.Directories = append(slices.Clone(scope.Directories), replacement)
		slices.Sort(scope.Directories)
		if err := scope.validate(); err == nil {
			t.Fatalf("invalid directory %s was accepted", replacement)
		}
	}
}

// The whole tree's public request binds the same portable scope later opened
// by production. Its mount seam never substitutes for filesystem operations.
func validatorRuntimePreparationFixture(t *testing.T) (context.Context, durablevolume.Reference, string, validatorRuntimePreparationScope, ed25519.PrivateKey) {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	observedRoot := filepath.Join(parent, "observed")
	root := filepath.Join(parent, "runtime")
	metadata := filepath.Join(parent, "preparation")
	staging := filepath.Join(parent, "staging")
	for _, path := range []string{observedRoot, root, metadata, staging} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	physical := durablefixture.New(t, t.Context(), observedRoot)
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x38}, ed25519.SeedSize))
	identity := AttemptLedgerIdentity{DeploymentID: "synthetic-third-party", ChainID: 964, GenesisHash: "0x" + strings.Repeat("36", 32),
		Netuid: 25, ValidatorID: 4, ValidatorUID: 0, NoID: 3, ValidatorVPK: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))}
	limits := AttemptLedgerDiskLimits{MaxRecordBytes: 256 * 1024, MaxRecordCount: 64, MaxTrailCount: 64, MaxRawRecordBytes: 8 * 1024 * 1024,
		MaxStorageBytes: 16 * 1024 * 1024, MaxStorageFiles: 64, MaxLegacyBytes: 8 * 1024 * 1024, MaxProofBytes: 8 * 1024 * 1024}
	scope := validatorRuntimePreparationScope{Schema: validatorRuntimePreparationSchema,
		Directories: []string{"evidence", "evidence/replay", "evidence/seal", "state", "state/no-3"},
		Ledgers: []validatorRuntimePreparationLedger{{Directory: "state/no-3", Ledger: AttemptLedgerPreparationScope{
			Identity: identity, Coordinator: "0x" + strings.Repeat("27", 20), Limits: limits, ExpectedHead: AttemptLedgerHead{Root: zeroAttemptHash()}}}}}
	input, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(root, &stat); err != nil {
		t.Fatal(err)
	}
	fence := durablevolume.PreparationFence{Schema: "urnetwork-storage-preparation-fence-v1", RootPath: root, RootInode: stat.Ino,
		Purpose: "fresh", FormerWritersStopped: true, NoPreviousOwnerState: true, Evidence: "synthetic fixture owns a new empty runtime root"}
	fenceRaw, err := json.Marshal(fence)
	if err != nil {
		t.Fatal(err)
	}
	fencePath := filepath.Join(metadata, "fresh.json")
	if err := os.WriteFile(fencePath, fenceRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	volume := declaration.Volumes[0]
	request := durablevolume.PreparationRequest{Schema: "urnetwork-storage-preparation-request-v1", Purpose: "fresh", Scope: "daemon",
		MountPath: volume.MountPath, FilesystemUuid: volume.FilesystemUuid, FilesystemType: volume.FilesystemType,
		MinAvailableBytes: 1024 * 1024, MinAvailableInodes: 64, RootPath: root,
		MarkerPath: filepath.Join(metadata, "marker"), LeasePath: filepath.Join(metadata, "lease"), DeclarationPath: filepath.Join(metadata, "volumes.json"),
		ControlPath: filepath.Join(metadata, "journal"), StagingDirectory: staging,
		FormerWriterFence: durablevolume.Reference{Path: fencePath, Sha256: validatorPreparationDigest(fenceRaw)},
		Limits: durablevolume.PreparationLimits{MaxEntries: 1000, MaxBytes: 64 * 1024 * 1024, MaxDepth: 16, MaxOwnerAttributes: 128,
			MaxOwnerAttributeBytes: 128 * 4096, MaxPlanBytes: 4 * 1024 * 1024},
		Owners: []durablevolume.PreparationOwner{{Kind: validatorRuntimePreparationKind, RelativePath: ".", Purpose: "fresh", Inputs: input}}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(metadata, "request.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return physical.Context, durablevolume.Reference{Path: path, Sha256: validatorPreparationDigest(raw)}, root, scope, key
}
