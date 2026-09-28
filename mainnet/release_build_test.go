// Release build tests compile disposable Go entry packages in real clean Git
// repositories. Exact file mutations force each boundary without running output.
package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The synthetic graph has the same forward/reverse module arrangement as SN
// and server. A nested alternative module makes resolution drift observable.
type releaseBuildFixture struct {
	dir    string
	snDir  string
	lock   sourceLock
	config releaseBuildConfig
}

// Nested files are created before the shared clean-Git fixture commits them.
func releaseBuildTestRepository(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	sourceLockTestRepository(t, dir, files)
}

// All executable bodies are visibly synthetic and import only the local test
// dependency and standard library. No network, database or credentials exist.
func newReleaseBuildFixture(t *testing.T, roles ...string) *releaseBuildFixture {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	dir := t.TempDir()
	sourceRoot := filepath.Join(dir, "source")
	dependencyFiles := map[string]string{
		"go.mod":                  "module example.test/release-dependency\n\ngo 1.26.5\n",
		"dependency.go":           "package dependency\nconst Value = \"synthetic-primary\"\n",
		"alternate/go.mod":        "module example.test/release-dependency\n\ngo 1.26.5\n",
		"alternate/dependency.go": "package dependency\nconst Value = \"synthetic-alternate\"\n",
	}
	releaseBuildTestRepository(t, filepath.Join(sourceRoot, "dependency"), dependencyFiles)
	mainSource := "package main\nimport (\n _ \"embed\"\n dependency \"example.test/release-dependency\"\n)\nvar Version string\n//go:embed payload.txt\nvar payload string\nfunc main() { println(dependency.Value, payload, Version) }\n"
	snFiles := map[string]string{
		"go.mod":     "module github.com/urfoundation/sn\n\ngo 1.26.5\n\nrequire (\nexample.test/release-dependency v0.0.0\ngithub.com/urnetwork/server v0.0.0\n)\nreplace example.test/release-dependency => ../dependency\nreplace github.com/urnetwork/server => ../server\n",
		"go.sum":     "",
		".gitignore": "mainnet/ignored.go\n",
	}
	for _, path := range []string{"mainnet", "cli/miner", "cli/validator"} {
		snFiles[path+"/main.go"] = mainSource
		snFiles[path+"/payload.txt"] = "synthetic payload\n"
	}
	snDir := filepath.Join(sourceRoot, "sn")
	releaseBuildTestRepository(t, snDir, snFiles)
	serverFiles := map[string]string{
		"go.mod": "module github.com/urnetwork/server\n\ngo 1.26.5\n\nrequire (\nexample.test/release-dependency v0.0.0\ngithub.com/urfoundation/sn v0.0.0\n)\nreplace example.test/release-dependency => ../dependency\nreplace github.com/urfoundation/sn => ../sn\n",
		"go.sum": "",
	}
	for _, path := range []string{"cli/api", "cli/taskworker", "cli/strecovery"} {
		serverFiles[path+"/main.go"] = mainSource
		serverFiles[path+"/payload.txt"] = "synthetic server payload\n"
	}
	releaseBuildTestRepository(t, filepath.Join(sourceRoot, "server"), serverFiles)
	if len(roles) == 0 {
		roles = []string{"sn-mainnet"}
	}
	fixture := &releaseBuildFixture{dir: dir, snDir: snDir, config: releaseBuildConfig{Schema: releaseBuildConfigSchema, CandidateId: "synthetic-release", SourceSnDir: snDir, OutputDir: filepath.Join(dir, "output"), Profile: releaseBuildProfile, Roles: roles}}
	fixture.relock(t)
	return fixture
}

// A changed fixture repository receives a real successor source lock.
func (self *releaseBuildFixture) relock(t *testing.T) {
	t.Helper()
	lock, err := buildSourceLock(context.Background(), self.snDir)
	if err != nil {
		t.Fatal(err)
	}
	self.lock = lock
	self.config.SourceLock = planTestWrite(t, self.dir, "source-lock.json", lock)
}

// Config paths live outside every source repository and are never compiler input.
func (self *releaseBuildFixture) write(t *testing.T) string {
	t.Helper()
	planTestWrite(t, self.dir, "build-config.json", self.config)
	return filepath.Join(self.dir, "build-config.json")
}

// Artifact admission tests still use the real compiler and package resolver.
func (self *releaseBuildFixture) builder(t *testing.T) *releaseBuilder {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(self.config.OutputDir, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	builder, err := newReleaseBuilder(context.Background(), self.config, self.lock)
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

// Appending after a complete ELF preserves readable Go build info while changing
// the actual executable identity. No executable is run by this helper.
func releaseBuildTestAppend(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("synthetic byte drift\n")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

// Real entry-package/build-info checks distinguish six roles and two source
// modules. Their successful inventory remains explicitly incomplete/unapproved.
func TestReleaseBuildSixRolesRetainCandidateScope(t *testing.T) {
	fixture := newReleaseBuildFixture(t, "sn-validator", "server-taskworker", "sn-mainnet", "server-strecovery", "sn-miner", "server-api")
	var output, stderr bytes.Buffer
	if code := runMain(context.Background(), []string{"release-build", "--config", fixture.write(t)}, &output, &stderr); code != 0 {
		t.Fatalf("release-build exit %d: %s", code, stderr.String())
	}
	var manifest releaseBuildManifest
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Roles) != 6 || !manifest.SameBuilderByteEquality || manifest.IndependentRebuildVerified || manifest.ReleaseComplete || manifest.DeploymentApproved || manifest.Status != "unapproved_build_candidate" {
		t.Fatalf("release build overstated selected candidate scope: %+v", manifest)
	}
	for _, result := range manifest.Roles {
		if result.First.Sha256 != result.Second.Sha256 || result.First.Path == result.Second.Path || result.BuildInfo.Package != result.Role.Module+strings.TrimPrefix(result.Role.Package, ".") {
			t.Fatalf("role has no exact repeated executable identity: %+v", result.Role)
		}
		raw, err := readPlanReference(context.Background(), fixture.write(t), result.Inputs, maximumReleaseBuildMetadataBytes)
		if err != nil {
			t.Fatal(err)
		}
		var inputs releaseBuildInputs
		if err := json.Unmarshal(raw, &inputs); err != nil {
			t.Fatal(err)
		}
		foundEmbed := false
		for _, pkg := range inputs.Packages {
			for _, file := range pkg.Files {
				if strings.HasSuffix(file.Path, "/payload.txt") {
					foundEmbed = true
				}
			}
		}
		if !foundEmbed || len(inputs.Modules) < 3 {
			t.Fatal("compiler closure omitted real local modules or embedded bytes")
		}
	}
	raw, err := readPlanReference(context.Background(), fixture.write(t), manifest.Inventory, maximumPlanManifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	var inventory releaseInventory
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.ReleaseComplete || inventory.ProvenanceProven || inventory.DeploymentApproved || len(inventory.MissingCategories) != 5 {
		t.Fatal("build inventory promoted three populated categories into release approval")
	}
	want := manifest.ContentHash
	manifest.ContentHash = ""
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if want != releaseBuildDigest(append([]byte(releaseBuildSchema+"\x00"), encoded...)) {
		t.Fatal("build manifest seal cannot be independently recomputed")
	}
	retained, err := os.ReadFile(filepath.Join(fixture.config.OutputDir, "build-manifest.json"))
	if err != nil || !bytes.Contains(retained, []byte(want)) {
		t.Fatalf("final build manifest was not retained: %v", err)
	}
}

// A clean Git status does not imply that ignored compiler inputs belong to the
// selected source lock. This reproduces the source-declaration-only gap.
func TestReleaseBuildRefusesIgnoredCompilerInput(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.snDir, "mainnet", "ignored.go"), []byte("package main\nvar ignoredSyntheticInput = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if status := sourceLockTestGit(t, fixture.snDir, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("fixture must be clean to Git: %s", status)
	}
	if _, err := buildReleaseCandidate(context.Background(), fixture.write(t), releaseBuildHooks{}); err == nil || !strings.Contains(err.Error(), "untracked in the locked source graph") {
		t.Fatalf("ignored compiler input received a source-locked release manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.config.OutputDir, "build-manifest.json")); !os.IsNotExist(err) {
		t.Fatal("refused build published a manifest")
	}
}

// Two regular executable files with matching build metadata still differ when
// a second-pass producer changes their actual bytes.
func TestReleaseBuildRefusesChangedRepeatedExecutable(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	hooks := releaseBuildHooks{afterBuild: func(pass int, role releaseBuildRole, path string) {
		if pass == 1 {
			releaseBuildTestAppend(t, path)
		}
	}}
	if _, err := buildReleaseCandidate(context.Background(), fixture.write(t), hooks); err == nil || !strings.Contains(err.Error(), "repeated bytes differ") {
		t.Fatalf("different repeated executable bytes were accepted: %v", err)
	}
}

// The second copy is evidence too; changing it after the comparison must fail
// even though only the first copy is selected for the release inventory.
func TestReleaseBuildRefusesLateArtifactDrift(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	hooks := releaseBuildHooks{beforeSeal: func() { releaseBuildTestAppend(t, filepath.Join(fixture.config.OutputDir, "second", "sn-mainnet")) }}
	if _, err := buildReleaseCandidate(context.Background(), fixture.write(t), hooks); err == nil || !strings.Contains(err.Error(), "final artifact fence") {
		t.Fatalf("late second-pass artifact drift was accepted: %v", err)
	}
}

// Git's assume-unchanged hint is not compiler custody. A deterministic mutation
// inside the second compile boundary is caught even if it would later disappear.
func TestReleaseBuildRefusesCleanGitCompilerByteDrift(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	path := filepath.Join(fixture.snDir, "mainnet", "payload.txt")
	sourceLockTestGit(t, fixture.snDir, "update-index", "--assume-unchanged", "mainnet/payload.txt")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hooks := releaseBuildHooks{afterBuild: func(pass int, role releaseBuildRole, path string) {
		if pass == 1 {
			if err := os.WriteFile(filepath.Join(fixture.snDir, "mainnet", "payload.txt"), []byte("synthetic changed embedded bytes\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}, beforeSeal: func() {
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := buildReleaseCandidate(context.Background(), fixture.write(t), hooks); err == nil || !strings.Contains(err.Error(), "compiler inputs changed during compilation") {
		t.Fatalf("clean-Git compiler byte drift during a build was accepted: %v", err)
	}
}

// A server replacement can be tracked and clean yet resolve the same module ID
// to a different physical directory than the top-level SN lock declares.
func TestReleaseBuildRefusesCompilerGraphAlias(t *testing.T) {
	fixture := newReleaseBuildFixture(t, "server-api")
	serverDir := filepath.Join(filepath.Dir(fixture.snDir), "server")
	path := filepath.Join(serverDir, "go.mod")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("=> ../dependency\n"), []byte("=> ../dependency/alternate\n"))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sourceLockTestGit(t, serverDir, "add", "go.mod")
	sourceLockTestGit(t, serverDir, "-c", "commit.gpgsign=false", "commit", "-qm", "synthetic alternate compiler resolution")
	fixture.relock(t)
	if _, err := buildReleaseCandidate(context.Background(), fixture.write(t), releaseBuildHooks{}); err == nil || !strings.Contains(err.Error(), "outside its locked physical path") {
		t.Fatalf("alternate physical compiler graph borrowed the SN source lock: %v", err)
	}
}

// A valid Go ELF from a different actual entry package cannot borrow the role
// identity from the filename or source-lock declaration.
func TestReleaseBuildRefusesExecutableRoleSubstitution(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	builder := fixture.builder(t)
	roles := releaseBuildRoles(fixture.lock)
	want, other := roles[0], roles[3]
	inputs, err := builder.capture(context.Background(), want)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.config.OutputDir, "substituted")
	if _, err := builder.command(context.Background(), other, time.Minute, append(builder.arguments(other), "-o", path, other.Package)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := builder.artifact(context.Background(), want, path, inputs); err == nil || !strings.Contains(err.Error(), "role, target, build profile or source revision differs") {
		t.Fatalf("wrong executable role was accepted: %v", err)
	}
}

// The expected source revision alone is insufficient: a non-trimmed binary is
// outside the selected reproducible profile even when its entry package matches.
func TestReleaseBuildRefusesWrongExecutableProfile(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	builder := fixture.builder(t)
	role := releaseBuildRoles(fixture.lock)[0]
	inputs, err := builder.capture(context.Background(), role)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.config.OutputDir, "untrimmed")
	if _, err := builder.command(context.Background(), role, time.Minute, "build", "-p=2", "-buildvcs=true", "-o", path, role.Package); err != nil {
		t.Fatal(err)
	}
	if _, _, err := builder.artifact(context.Background(), role, path, inputs); err == nil || !strings.Contains(err.Error(), "role, target, build profile or source revision differs") {
		t.Fatalf("untrimmed executable profile was accepted: %v", err)
	}
}

// The actual compiler emits (devel) for the empty local replacement version in
// go list. The initial literal comparison rejected every correct local build.
func TestReleaseBuildAcceptsGoLocalReplacementIdentity(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	builder := fixture.builder(t)
	role := releaseBuildRoles(fixture.lock)[0]
	inputs, err := builder.capture(context.Background(), role)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.config.OutputDir, "local-replacement")
	if _, err := builder.command(context.Background(), role, time.Minute, append(builder.arguments(role), "-o", path, role.Package)...); err != nil {
		t.Fatal(err)
	}
	actual, err := buildinfo.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	listed, embedded := false, false
	for _, module := range inputs.Modules {
		if module.Path == "example.test/release-dependency" && module.Replace != nil && module.Replace.Version == "" {
			listed = true
		}
	}
	for _, module := range actual.Deps {
		if module.Path == "example.test/release-dependency" && module.Replace != nil && module.Replace.Version == "(devel)" {
			embedded = true
		}
	}
	if !listed || !embedded {
		t.Fatal("real compiler fixture did not expose the local-version representation boundary")
	}
	if _, _, err := builder.artifact(context.Background(), role, path, inputs); err != nil {
		t.Fatalf("valid local replacement executable was refused: %v", err)
	}
}

// A retained Go metadata tuple cannot hide a changed ELF program header. The
// synthetic interpreter flag forces a refusal without executing the file.
func TestReleaseBuildRefusesDynamicExecutableHeader(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	builder := fixture.builder(t)
	role := releaseBuildRoles(fixture.lock)[0]
	inputs, err := builder.capture(context.Background(), role)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.config.OutputDir, "dynamic-header")
	if _, err := builder.command(context.Background(), role, time.Minute, append(builder.arguments(role), "-o", path, role.Package)...); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 64 {
		t.Fatal("compiler did not produce an ELF header")
	}
	offset := binary.LittleEndian.Uint64(raw[32:40])
	if offset > uint64(len(raw)-4) {
		t.Fatal("compiler produced an invalid ELF program table")
	}
	binary.LittleEndian.PutUint32(raw[offset:offset+4], 3)
	if err := os.WriteFile(path, raw, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := builder.artifact(context.Background(), role, path, inputs); err == nil || !strings.Contains(err.Error(), "dynamic loader outside the static ELF profile") {
		t.Fatalf("dynamic ELF header borrowed the static build profile: %v", err)
	}
}

// Identity and path admission happens before output ownership or compilation.
func TestReleaseBuildRejectsInvalidDeclarations(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	base := fixture.config
	invalid := []releaseBuildConfig{}
	changed := base
	changed.Schema = "synthetic-unknown"
	invalid = append(invalid, changed)
	changed = base
	changed.Profile = "linux-arm64-static-v1"
	invalid = append(invalid, changed)
	changed = base
	changed.Roles = []string{"server-operator"}
	invalid = append(invalid, changed)
	changed = base
	changed.Roles = []string{"sn-mainnet", "sn-mainnet"}
	invalid = append(invalid, changed)
	changed = base
	changed.Roles = nil
	invalid = append(invalid, changed)
	changed = base
	changed.OutputDir = filepath.Join(fixture.snDir, "output")
	invalid = append(invalid, changed)
	changed = base
	changed.SourceSnDir = "relative/sn"
	invalid = append(invalid, changed)
	for index, config := range invalid {
		if _, err := config.validate(fixture.lock); err == nil {
			t.Fatalf("invalid release declaration %d was accepted", index)
		}
	}
	if _, err := os.Stat(base.OutputDir); !os.IsNotExist(err) {
		t.Fatal("invalid declaration created output")
	}
}

// Existing output and symlink aliases cannot be claimed or overwritten.
func TestReleaseBuildRefusesExistingOrAliasedOutput(t *testing.T) {
	fixture := newReleaseBuildFixture(t)
	if err := os.Mkdir(fixture.config.OutputDir, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture.config.OutputDir, "retained")
	if err := os.WriteFile(marker, []byte("synthetic retained bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildReleaseCandidate(context.Background(), fixture.write(t), releaseBuildHooks{}); err == nil || !strings.Contains(err.Error(), "output must be new") {
		t.Fatalf("existing output was reused: %v", err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "synthetic retained bytes" {
		t.Fatalf("existing output changed: %v", err)
	}
	alias := filepath.Join(fixture.dir, "alias")
	if err := os.Symlink(fixture.config.OutputDir, alias); err != nil {
		t.Fatal(err)
	}
	fixture.config.OutputDir = filepath.Join(alias, "candidate")
	if _, err := fixture.config.validate(fixture.lock); err == nil {
		t.Fatal("symlink output parent was admitted")
	}
}

// Cancellation and bounded command output terminate ownership before a manifest
// exists. This exercises the production output writer rather than a fake verdict.
func TestReleaseBuildCancellationAndOutputBound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &releaseBuildOutput{cancel: cancel}
	if _, err := writer.Write(make([]byte, maximumReleaseBuildMetadataBytes+1)); err == nil || ctx.Err() == nil || writer.Len() != 0 {
		t.Fatal("oversized compiler output did not cancel at the memory bound")
	}
	fixture := newReleaseBuildFixture(t)
	if _, err := buildReleaseCandidate(ctx, fixture.write(t), releaseBuildHooks{}); err == nil {
		t.Fatal("canceled release build was accepted")
	}
	if _, err := os.Stat(fixture.config.OutputDir); !os.IsNotExist(err) {
		t.Fatal("canceled admission created output")
	}
}

// Command syntax is a local refusal; no source lookup or subprocess is needed.
func TestReleaseBuildCommandRejectsMalformedInput(t *testing.T) {
	for _, args := range [][]string{{"release-build"}, {"release-build", "--config", "synthetic.json", "--timeout", "0"}, {"release-build", "--config", "synthetic.json", "--timeout", "4h"}, {"release-build", "--config", "synthetic.json", "extra"}} {
		var output, stderr bytes.Buffer
		if code := runMain(context.Background(), args, &output, &stderr); code != 2 || output.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("malformed command did not refuse without a manifest: %v exit %d", args, code)
		}
	}
}
