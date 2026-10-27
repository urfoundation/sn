// The offline command must produce reviewable custody without a signing key,
// and its accepted plan must initialize the actual production ledger owner.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// All identities and filesystem observations are synthetic; publication,
// descriptors, permissions, xattrs and production reopening remain real.
type storagePreparationCommandFixture struct {
	ctx         context.Context
	storage     *durablefixture.Fixture
	root        string
	metadata    string
	requestPath string
	requestHash string
	identity    validator.AttemptLedgerIdentity
	limits      validator.AttemptLedgerDiskLimits
	key         ed25519.PrivateKey
}

func newStoragePreparationCommandFixture(t *testing.T) *storagePreparationCommandFixture {
	t.Helper()
	parent := t.TempDir()
	return newStoragePreparationCommandFixtureAt(t, parent)
}

// A caller-owned scratch parent also supports deliberately bounded pathname
// fixtures; it never retargets any previously provisioned root or declaration.
func newStoragePreparationCommandFixtureAt(t *testing.T, parent string) *storagePreparationCommandFixture {
	t.Helper()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, name := range []string{"existing-observation-root", "target", "metadata", "staging"} {
		paths[name] = filepath.Join(parent, name)
		if err := os.Mkdir(paths[name], 0700); err != nil {
			t.Fatal(err)
		}
	}
	physical := durablefixture.New(t, t.Context(), paths["existing-observation-root"])
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{61}, ed25519.SeedSize))
	identity := validator.AttemptLedgerIdentity{DeploymentID: "synthetic-offline-validator", ChainID: 964, GenesisHash: "0x" + strings.Repeat("71", 32),
		Netuid: 25, ValidatorID: 2, ValidatorUID: 0, NoID: 3, ValidatorVPK: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))}
	limits := validator.AttemptLedgerDiskLimits{MaxRecordBytes: 256 * 1024, MaxRecordCount: 64, MaxTrailCount: 64, MaxRawRecordBytes: 8 * 1024 * 1024,
		MaxStorageBytes: 16 * 1024 * 1024, MaxStorageFiles: 64, MaxLegacyBytes: 8 * 1024 * 1024, MaxProofBytes: 8 * 1024 * 1024}
	var root syscall.Stat_t
	if err := syscall.Stat(paths["target"], &root); err != nil {
		t.Fatal(err)
	}
	fencePath := filepath.Join(paths["metadata"], "former-writer.json")
	fence, err := json.Marshal(map[string]any{"schema": "urnetwork-storage-preparation-fence-v1", "root_path": paths["target"],
		"root_inode": root.Ino, "purpose": "fresh", "former_writers_stopped": true, "no_previous_owner_state": true,
		"evidence": "synthetic fixture has no former writer; no live host or service"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fencePath, fence, 0600); err != nil {
		t.Fatal(err)
	}
	volume := declaration.Volumes[0]
	request, err := json.Marshal(map[string]any{
		"schema": "urnetwork-storage-preparation-request-v1", "purpose": "fresh", "scope": "daemon",
		"mount_path": volume.MountPath, "filesystem_uuid": volume.FilesystemUuid, "filesystem_type": volume.FilesystemType,
		"min_available_bytes": uint64(1024 * 1024), "min_available_inodes": uint64(64),
		"root_path": paths["target"], "marker_path": filepath.Join(paths["metadata"], "identity"), "lease_path": filepath.Join(paths["metadata"], "root.lease"),
		"declaration_path": filepath.Join(paths["metadata"], "durable-volumes.json"), "control_path": filepath.Join(paths["metadata"], "preparation.jsonl"),
		"staging_directory": paths["staging"], "former_writer_fence": durablevolume.Reference{Path: fencePath, Sha256: durablefixture.Digest(fence)},
		"limits": map[string]uint64{"max_entries": 80, "max_bytes": 32 * 1024 * 1024, "max_depth": 4, "max_owner_attributes": 8, "max_owner_attribute_bytes": 32 * 1024, "max_plan_bytes": 1024 * 1024},
		"owners": []any{map[string]any{"kind": "validator-attempt-ledger", "relative_path": ".", "purpose": "fresh", "inputs": map[string]any{
			"identity": identity, "coordinator": "0x" + strings.Repeat("23", 20), "limits": limits,
			"expected_head": validator.AttemptLedgerHead{Root: "0x" + strings.Repeat("00", 32)}, "legacy": nil}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(paths["metadata"], "request.json")
	if err := os.WriteFile(requestPath, request, 0600); err != nil {
		t.Fatal(err)
	}
	return &storagePreparationCommandFixture{ctx: physical.Context, storage: physical, root: paths["target"], metadata: paths["metadata"],
		requestPath: requestPath, requestHash: durablefixture.Digest(request), identity: identity, limits: limits, key: key}
}

// Plan output is a public exact artifact. It must leave the target empty and
// must not contain the private key later used only to prove runtime admission.
func (self *storagePreparationCommandFixture) plan(t *testing.T) (string, string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	code := runMain(self.ctx, []string{"storage-prepare", "plan", "--request", self.requestPath, "--request-sha256", self.requestHash}, &output, &diagnostic)
	if code != 0 {
		t.Fatalf("offline preparation command refused valid public plan: exit=%d %s", code, diagnostic.String())
	}
	var plan struct {
		Schema            string `json:"schema"`
		RequestSha256     string `json:"request_sha256"`
		RestartAuthorized bool   `json:"restart_authorized"`
	}
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil || plan.Schema != "urnetwork-storage-preparation-plan-v1" || plan.RequestSha256 != self.requestHash || plan.RestartAuthorized {
		t.Fatal("plan lost exact public scope or granted restart", err)
	}
	entries, err := os.ReadDir(self.root)
	if err != nil || len(entries) != 0 || bytes.Contains(output.Bytes(), []byte(hex.EncodeToString(self.key))) {
		t.Fatal("plan mutated target custody or exposed a private key", err)
	}
	path := filepath.Join(self.metadata, "reviewed-plan.json")
	if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path, durablefixture.Digest(output.Bytes())
}

func TestStoragePreparationCommandInitializesActualValidatorLedger(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	path, hash := f.plan(t)
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic)
	if code != 0 {
		t.Fatalf("approved preparation did not initialize custody: exit=%d %s", code, diagnostic.String())
	}
	var result struct {
		Schema            string                  `json:"schema"`
		Declaration       durablevolume.Reference `json:"declaration"`
		RestartAuthorized bool                    `json:"restart_authorized"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Schema != "urnetwork-storage-preparation-result-v1" || result.RestartAuthorized || result.Declaration.Path != filepath.Join(f.metadata, "durable-volumes.json") {
		t.Fatal("preparation did not return its exact non-activating declaration", err)
	}
	ctx := durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), f.storage.Host)
	ledger, err := validator.NewDiskAttemptLedger(ctx, f.root, f.identity, "0x"+strings.Repeat("23", 20), f.key, f.limits)
	if err != nil {
		t.Fatal("offline public preparation cannot open the actual guarded ledger", err)
	}
	head, err := ledger.Head()
	if closeErr := ledger.Close(); err != nil || closeErr != nil || head != (validator.AttemptLedgerHead{Root: "0x" + strings.Repeat("00", 32)}) {
		t.Fatal("fresh prepared ledger changed its exact empty head", err, closeErr)
	}
}

func TestStoragePreparationCommandRequiresExactAcceptedPlan(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	path, _ := f.plan(t)
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", "sha256:" + strings.Repeat("00", 32)}, &output, &diagnostic)
	entries, err := os.ReadDir(f.root)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "plan bytes differ") || err != nil || len(entries) != 0 {
		t.Fatal("unapproved plan changed target custody or returned success", code, diagnostic.String(), err)
	}
	for _, name := range []string{"identity", "root.lease", "preparation.jsonl", "durable-volumes.json"} {
		if _, err := os.Lstat(filepath.Join(f.metadata, name)); !os.IsNotExist(err) {
			t.Fatal("unapproved plan created deployment metadata", name, err)
		}
	}
}
