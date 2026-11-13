// Release inventory tests use disposable clean Git repositories and synthetic
// artifacts to prove generation fencing, exact bytes and honest partial scope.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Source and build outputs live separately, mirroring detached SN plus pinned
// replacement repositories and ignored/external candidate artifact paths.
type releaseInventoryFixture struct {
	dir    string
	snDir  string
	lock   sourceLock
	config releaseInventoryConfig
}

// A real clean-source lock is used rather than a fabricated trusted checker.
func newReleaseInventoryFixture(t *testing.T) *releaseInventoryFixture {
	t.Helper()
	snDir, _, _ := sourceLockTestWorkspace(t)
	lock, err := buildSourceLock(context.Background(), snDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fixture := &releaseInventoryFixture{dir: dir, snDir: snDir, lock: lock,
		config: releaseInventoryConfig{Schema: releaseInventoryConfigSchema, CandidateId: "synthetic-release", SourceSnDir: snDir,
			SourceLock: planTestWrite(t, dir, "source-lock.json", lock), Artifacts: []releaseArtifactInput{}}}
	return fixture
}

// Explicit synthetic bytes exercise streaming and optional expected hashes.
func (self *releaseInventoryFixture) add(t *testing.T, id, category, content string) {
	t.Helper()
	path := filepath.Join(self.dir, id)
	mode := os.FileMode(0600)
	if category == "executable" {
		mode = 0700
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	input := releaseArtifactInput{Id: id, Category: category, Path: id, SourceLockContentHash: self.lock.ContentHash}
	if category == "image-identity" {
		input.ImageReference = "registry.example.test/synthetic@sha256:" + strings.Repeat("ab", 32)
	}
	self.config.Artifacts = append(self.config.Artifacts, input)
}

// The config hash includes exact input bytes; file output order is independent
// of mutable directory enumeration because only explicit declarations exist.
func (self *releaseInventoryFixture) write(t *testing.T) string {
	t.Helper()
	planTestWrite(t, self.dir, "inventory-config.json", self.config)
	return filepath.Join(self.dir, "inventory-config.json")
}

// Partial capture retains exact file/mode/source identities and stable seals,
// without converting a source lock or a single executable into release approval.
func TestReleaseInventoryPartialDeterministic(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	fixture.add(t, "z-mainnet", "executable", "synthetic executable bytes")
	fixture.add(t, "a-config", "config", "synthetic configuration bytes")
	path := fixture.write(t)
	first, err := buildReleaseInventory(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildReleaseInventory(context.Background(), path)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("identical inventory inputs changed: %v", err)
	}
	if first.ReleaseComplete || first.DeploymentApproved || first.ProvenanceProven || first.Status != "unapproved_candidate" || len(first.MissingCategories) != 6 || len(first.Categories) != 8 || len(first.Artifacts) != 2 ||
		first.Artifacts[0].Id != "a-config" || first.Artifacts[1].Mode != 0700 || first.SourceLockContentHash != fixture.lock.ContentHash {
		t.Fatalf("partial inventory concealed missing coverage or changed ordering: %+v", first)
	}
	for _, artifact := range first.Artifacts {
		raw, err := os.ReadFile(artifact.Path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if artifact.Bytes != int64(len(raw)) || artifact.Sha256 != "sha256:"+hex.EncodeToString(digest[:]) || artifact.SourceLockContentHash != fixture.lock.ContentHash || artifact.Status != "present_unvalidated" {
			t.Fatalf("artifact does not bind exact bytes/source: %+v", artifact)
		}
	}
	want := first.ContentHash
	first.ContentHash = ""
	raw, _ := json.Marshal(first)
	digest := sha256.Sum256(append([]byte(releaseInventorySchema+"\x00"), raw...))
	if want != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal("inventory content seal is not independently reproducible")
	}
}

// Even eight populated categories cannot claim complete roles, contract build
// provenance, migration deployment identity or production qualification.
func TestReleaseInventoryFullCategoriesStillUnapproved(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	for _, category := range releaseInventoryCategories() {
		fixture.add(t, category, category, "synthetic "+category+" bytes")
	}
	inventory, err := buildReleaseInventory(context.Background(), fixture.write(t))
	if err != nil {
		t.Fatal(err)
	}
	if inventory.ReleaseComplete || inventory.DeploymentApproved || inventory.ProvenanceProven || len(inventory.MissingCategories) != 0 || len(inventory.Artifacts) != 8 {
		t.Fatal("category coverage promoted itself into qualification")
	}
	for _, category := range inventory.Categories {
		if category.Status != "present_unvalidated" || category.Files != 1 {
			t.Fatalf("wrong category semantics: %+v", category)
		}
	}
}

// Lexical aliases, parent-directory symlinks and hardlinks cannot turn one
// retained file into multiple apparent artifact categories.
func TestReleaseInventoryRejectsDuplicateResolvedFiles(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	fixture.add(t, "config", "config", "one selected file")
	original := fixture.config.Artifacts[0]
	aliasDir := filepath.Join(fixture.dir, "alias-dir")
	if err := os.Symlink(fixture.dir, aliasDir); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(fixture.dir, "hardlink")
	if err := os.Link(filepath.Join(fixture.dir, original.Path), hardlink); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"./config", filepath.Join(aliasDir, "config"), hardlink} {
		second := original
		second.Id = "policy"
		second.Category = "policy"
		second.Path = alias
		fixture.config.Artifacts = []releaseArtifactInput{original, second}
		if _, err := buildReleaseInventory(context.Background(), fixture.write(t)); err == nil {
			t.Errorf("one file filled multiple categories through %q", alias)
		}
	}
}

// Current source is checked against the lock, including dirty changes and a
// clean successor dependency commit. Old artifact bindings cannot be migrated
// just by swapping the inventory's top-level source-lock reference.
func TestReleaseInventoryRejectsSourceDriftAndInheritedArtifacts(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	fixture.add(t, "mainnet", "executable", "synthetic retained binary")
	path := fixture.write(t)
	original, err := buildReleaseInventory(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	dependency := filepath.Join(filepath.Dir(fixture.snDir), "dependency")
	if err := os.WriteFile(filepath.Join(dependency, "dependency.go"), []byte("package dependency\n// successor source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildReleaseInventory(context.Background(), path); err == nil {
		t.Fatal("dirty dependency inherited the prior artifact inventory")
	}
	sourceLockTestGit(t, dependency, "add", "dependency.go")
	sourceLockTestGit(t, dependency, "-c", "commit.gpgsign=false", "commit", "-qm", "synthetic source successor")
	if _, err := buildReleaseInventory(context.Background(), path); err == nil {
		t.Fatal("clean successor dependency inherited the old source lock")
	}
	newLock, err := buildSourceLock(context.Background(), fixture.snDir)
	if err != nil {
		t.Fatal(err)
	}
	fixture.config.SourceLock = planTestWrite(t, fixture.dir, "source-lock.json", newLock)
	if _, err := buildReleaseInventory(context.Background(), fixture.write(t)); err == nil {
		t.Fatal("old artifact declaration silently inherited a new source generation")
	}
	fixture.config.Artifacts[0].SourceLockContentHash = newLock.ContentHash
	successor, err := buildReleaseInventory(context.Background(), fixture.write(t))
	if err != nil {
		t.Fatal(err)
	}
	if successor.ContentHash == original.ContentHash || successor.SourceLockContentHash == original.SourceLockContentHash || successor.Artifacts[0].Sha256 != original.Artifacts[0].Sha256 || successor.ProvenanceProven {
		t.Fatal("explicit source rebinding lost byte identity or invented provenance")
	}
}

// Changing an artifact after capture is forced directly between production
// capture and final verification; restored timestamps cannot hide byte drift.
func TestReleaseInventoryRechecksChangedArtifactBytes(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	fixture.add(t, "config", "config", "old bytes")
	input := fixture.config.Artifacts[0]
	path := fixture.write(t)
	expected, err := captureReleaseArtifact(context.Background(), input, path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(expected.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expected.Path, []byte("new bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(expected.Path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseArtifact(context.Background(), input, path, expected); err == nil {
		t.Fatal("same-size restored-mtime byte drift was accepted")
	}
	if err := os.Remove(expected.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, expected.Path); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseArtifact(context.Background(), input, path, expected); err == nil {
		t.Fatal("path replaced by a symlink was accepted")
	}
}

// The exact expected hash can reject stale on-disk output independently of the
// source binding. Config/lock JSON and category/image identities stay strict.
func TestReleaseInventoryRejectsInvalidDeclarations(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	fixture.add(t, "contract", "contract", "synthetic contract output")
	base := fixture.config
	for _, mutation := range []string{"category", "id", "source", "expected", "image", "substitution", "count", "duplicate"} {
		fixture.config = base
		fixture.config.Artifacts = append([]releaseArtifactInput(nil), base.Artifacts...)
		switch mutation {
		case "category":
			fixture.config.Artifacts[0].Category = "approved-contract"
		case "id":
			fixture.config.Artifacts[0].Id = "../contract"
		case "source":
			fixture.config.Artifacts[0].SourceLockContentHash = testGenesisHash
		case "expected":
			fixture.config.Artifacts[0].ExpectedSha256 = "sha256:" + strings.Repeat("ab", 32)
		case "image":
			fixture.config.Artifacts[0].Category = "image-identity"
			fixture.config.Artifacts[0].ImageReference = "registry.example.test/image:latest"
		case "substitution":
			fixture.config.Artifacts[0].Path = "$BUILD/contract.json"
		case "count":
			fixture.config.Artifacts = make([]releaseArtifactInput, maximumReleaseInventoryArtifacts+1)
		case "duplicate":
			fixture.config.Artifacts = append(fixture.config.Artifacts, fixture.config.Artifacts[0])
		}
		if _, err := buildReleaseInventory(context.Background(), fixture.write(t)); err == nil {
			t.Errorf("%s declaration admitted", mutation)
		}
	}
	for _, raw := range []string{`{"schema":"x","Schema":"y"}`, `{"schema":"x","approved":true}`, `{} {}`} {
		var config releaseInventoryConfig
		if err := decodePlanJson([]byte(raw), &config); err == nil {
			t.Errorf("ambiguous inventory config admitted: %s", raw)
		}
	}
}

// Bounds and cancellation are checked in the streaming path. Files are never
// executed, and FIFOs cannot turn local inventory into an unbounded wait.
func TestReleaseInventoryFileBoundsAndCancellation(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	fixture.add(t, "config", "config", "12345")
	input := fixture.config.Artifacts[0]
	path := fixture.write(t)
	if _, err := captureReleaseArtifact(context.Background(), input, path, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := captureReleaseArtifact(context.Background(), input, path, 4); err == nil {
		t.Fatal("oversized artifact admitted")
	}
	input.Category = "executable"
	if _, err := captureReleaseArtifact(context.Background(), input, path, 5); err == nil {
		t.Fatal("non-executable file declared executable")
	}
	input.Category = "config"
	fifo := filepath.Join(fixture.dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	input.Path = fifo
	if _, err := captureReleaseArtifact(context.Background(), input, path, 5); err == nil {
		t.Fatal("FIFO admitted as artifact")
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader := releaseInventoryReader{ctx: ctx, reader: strings.NewReader("bytes")}
	buffer := make([]byte, 1)
	if n, err := reader.Read(buffer); n != 1 || err != nil {
		t.Fatal("valid first read failed")
	}
	cancel()
	if _, err := reader.Read(buffer); err == nil {
		t.Fatal("canceled stream continued reading")
	}
	if _, err := buildReleaseInventory(ctx, path); err == nil {
		t.Fatal("canceled inventory proceeded")
	}
}

// Empty inventories emit all missing categories. CLI rejects execution flags,
// propagates output failure and never publishes a partial failed inventory.
func TestReleaseInventoryCommandMissingCategories(t *testing.T) {
	fixture := newReleaseInventoryFixture(t)
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), []string{"release-inventory", "--config", fixture.write(t)}, &stdout, &stderr)
	var inventory releaseInventory
	if err := json.Unmarshal(stdout.Bytes(), &inventory); err != nil || code != 0 || len(inventory.MissingCategories) != 8 || len(inventory.Artifacts) != 0 || inventory.ReleaseComplete {
		t.Fatalf("empty candidate claimed coverage: exit=%d err=%v stderr=%s", code, err, stderr.String())
	}
	for _, args := range [][]string{{"release-inventory"}, {"release-inventory", "--config", fixture.write(t), "--apply"}, {"release-inventory", "--rpc", "http://rpc.example"}} {
		stdout.Reset()
		stderr.Reset()
		if code := runMain(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("invalid inventory command emitted evidence: %v exit=%d", args, code)
		}
	}
	if code := runMain(context.Background(), []string{"release-inventory", "--config", fixture.write(t)}, ioFailureWriter{}, &stderr); code != 1 {
		t.Fatal("output failure reported inventory success")
	}
}
