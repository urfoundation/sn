// Synthetic receipt mutations force each aggregation boundary without Docker,
// network access, sleeps, production identities or an application execution.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Test-only rewrites deliberately recompute the outer file pin so inner seals
// and source joins, rather than a stale external checksum, reject substitutions.
func aggregateTestWrite(t *testing.T, path string, value any) toolPin {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return toolPin{Path: path, Sha256: digestImageBytes(raw)}
}

// A real scratch OCI inspection supplies the supplement's image fields; parent
// identity is synthetic but is joined with both of the original seal formats.
func aggregateTestSupplement(t *testing.T) (toolPin, buildManifest, toolPin, imageBuildReceipt) {
	t.Helper()
	parentPin, parent := imageTestParent(t, scratchImageDockerfile)
	receipt := imageTestReceipt(t)
	receipt.ParentManifestSha256, receipt.ParentContentHash = parentPin.Sha256, parent.ContentHash
	receipt.CandidateId = parent.CandidateId
	for i := range receipt.Artifacts {
		if receipt.Artifacts[i].Path == "inputs/parent-manifest.json" {
			receipt.Artifacts[i].Sha256 = parentPin.Sha256
		}
	}
	if err := sealImageReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	pin := aggregateTestWrite(t, filepath.Join(t.TempDir(), "receipt.json"), receipt)
	return parentPin, parent, pin, receipt
}

// A new external checksum cannot legitimize a stale inner content seal.
func TestImageAggregateAuthenticatesFileAndContentSeparately(t *testing.T) {
	parentPin, parent, pin, receipt := aggregateTestSupplement(t)
	if _, err := readAggregateSupplement(pin, parentPin, parent); err != nil {
		t.Fatal(err)
	}
	receipt.Limitations = append(receipt.Limitations, "synthetic changed evidence")
	changedPin := aggregateTestWrite(t, pin.Path, receipt)
	if _, err := readAggregateSupplement(pin, parentPin, parent); err == nil || !strings.Contains(err.Error(), "file pin") {
		t.Fatalf("file substitution escaped its boundary: %v", err)
	}
	if _, err := readAggregateSupplement(changedPin, parentPin, parent); err == nil || !strings.Contains(err.Error(), "content seal") {
		t.Fatalf("stale seal escaped its boundary: %v", err)
	}
}

// Every parent identity must agree even if the other receipt has a valid new
// domain-separated seal and the caller explicitly supplies its new file hash.
func TestImageAggregateRejectsResealedMixedParents(t *testing.T) {
	for _, field := range []string{"candidate", "file", "content"} {
		parentPin, parent, pin, receipt := aggregateTestSupplement(t)
		switch field {
		case "candidate":
			receipt.CandidateId = "synthetic-other-candidate"
		case "file":
			receipt.ParentManifestSha256 = buildFixtureDigest("synthetic other manifest")
			for i := range receipt.Artifacts {
				if receipt.Artifacts[i].Path == "inputs/parent-manifest.json" {
					receipt.Artifacts[i].Sha256 = receipt.ParentManifestSha256
				}
			}
		case "content":
			receipt.ParentContentHash = buildFixtureDigest("synthetic other content")
		}
		if err := sealImageReceipt(&receipt); err != nil {
			t.Fatal(err)
		}
		pin = aggregateTestWrite(t, pin.Path, receipt)
		if _, err := readAggregateSupplement(pin, parentPin, parent); err == nil || !strings.Contains(err.Error(), "another parent") {
			t.Fatalf("accepted %s or missed parent boundary: %v", field, err)
		}
	}
}

// Artifact verification covers unrelated retained evidence as well as images;
// duplicate IDs/paths, absent bytes and parent-directory aliases cannot hide it.
func TestImageAggregateRehashesAllArtifactPaths(t *testing.T) {
	for _, fault := range []string{"tamper", "missing", "symlink", "duplicate-id", "duplicate-path", "traversal", "size"} {
		root := t.TempDir()
		path := filepath.Join(root, "inputs", "non-image-source")
		if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		raw := []byte("synthetic retained source")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		artifact := buildArtifact{Id: "source", Kind: "source-input", Path: "inputs/non-image-source", Sha256: digestImageBytes(raw), Bytes: int64(len(raw))}
		artifacts := []buildArtifact{artifact}
		if err := verifyAggregateArtifacts(t.Context(), root, artifacts); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "tamper":
			if err := os.WriteFile(path, []byte("synthetic replaced bytes"), 0600); err != nil {
				t.Fatal(err)
			}
		case "missing":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Rename(filepath.Join(root, "inputs"), filepath.Join(root, "other")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "other"), filepath.Join(root, "inputs")); err != nil {
				t.Fatal(err)
			}
		case "duplicate-id":
			artifacts = append(artifacts, artifact)
		case "duplicate-path":
			artifact.Id = "other"
			artifacts = append(artifacts, artifact)
		case "traversal":
			artifacts[0].Path = "inputs/../inputs/non-image-source"
		case "size":
			artifacts[0].Bytes++
		}
		if err := verifyAggregateArtifacts(t.Context(), root, artifacts); err == nil {
			t.Fatalf("accepted artifact %s", fault)
		}
	}
}

// A stored true flag cannot replace independent inspection, and using a binary
// from another candidate must fail even when the OCI and receipt match each other.
func TestImageAggregateReplaysScratchAgainstParentBinary(t *testing.T) {
	path, binary := scratchTestArchive(t, nil)
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := copyBuildFile(path, filepath.Join(directory, "images/competitionworker.oci.tar"), 0600); err != nil {
		t.Fatal(err)
	}
	readback, err := inspectScratchImage(path, binary)
	if err != nil {
		t.Fatal(err)
	}
	receipt := imageBuildReceipt{Image: readback}
	if _, err := verifyAggregateScratch(directory, receipt, binary); err != nil {
		t.Fatal(err)
	}
	receipt.Image.ConfigDigest = buildFixtureDigest("synthetic forged config")
	if _, err := verifyAggregateScratch(directory, receipt, binary); err == nil || !strings.Contains(err.Error(), "readback differs") {
		t.Fatalf("accepted forged stored readback: %v", err)
	}
	binary.Sha256 = buildFixtureDigest("synthetic other parent binary")
	if _, err := verifyAggregateScratch(directory, imageBuildReceipt{Image: readback}, binary); err == nil {
		t.Fatal("accepted foreign parent executable")
	}
}

// Package replay compares complete rootfs details, not merely the platform hash
// or seven green per-image flags in the stored supplement.
func TestImageAggregateReplaysPackageRootfsAndParentBinary(t *testing.T) {
	path, binary, base, packages := packageTestArchive(t, nil)
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := copyBuildFile(path, filepath.Join(directory, "images/api.oci.tar"), 0600); err != nil {
		t.Fatal(err)
	}
	readback, err := inspectPackageImage(path, binary, base, packages)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyAggregatePackage(directory, readback, binary, base, packages); err != nil {
		t.Fatal(err)
	}
	changed := readback
	changed.RootfsContentHash = buildFixtureDigest("synthetic forged rootfs")
	if _, err := verifyAggregatePackage(directory, changed, binary, base, packages); err == nil || !strings.Contains(err.Error(), "readback differs") {
		t.Fatalf("accepted forged rootfs readback: %v", err)
	}
	binary.Sha256 = buildFixtureDigest("synthetic other parent executable")
	if _, err := verifyAggregatePackage(directory, readback, binary, base, packages); err == nil {
		t.Fatal("accepted package image from foreign parent")
	}
}

// This is only a seal-boundary fixture; actual image bytes are exercised by the
// preceding replay tests and the separate full real-candidate qualification.
func aggregateTestReceipt() imageAggregateReceipt {
	packageHash, scratchHash := buildFixtureDigest("synthetic package"), buildFixtureDigest("synthetic scratch")
	receipt := imageAggregateReceipt{Schema: imageAggregateSchema, CandidateId: "synthetic-candidate", Platform: "linux/amd64", ConfigSha256: buildFixtureDigest("synthetic config"), Parent: imageAggregateInput{Schema: buildSchema, File: toolPin{Sha256: buildFixtureDigest("synthetic parent")}, ContentHash: buildFixtureDigest("synthetic parent content")}, Supplements: []imageAggregateInput{{Schema: imageBuildSchema, File: toolPin{Sha256: scratchHash}, ContentHash: buildFixtureDigest("synthetic scratch content")}, {Schema: packageImageSchema, File: toolPin{Sha256: packageHash}, ContentHash: buildFixtureDigest("synthetic package content")}}, MissingImages: []string{}, SourceToImageVerified: true}
	for _, role := range releaseRoles() {
		if !role.Image {
			continue
		}
		name := strings.TrimPrefix(role.Id, "server-")
		pin := packageHash
		container := "/usr/local/sbin/bringyour-" + name
		if role.Id == scratchImageId {
			pin = scratchHash
			container = "/competitionworker"
		}
		receipt.Images = append(receipt.Images, imageAggregateImage{Id: role.Id, SupplementSha256: pin, Binary: buildArtifact{Id: role.Id, Kind: "binary", Path: "binaries/" + role.Id, Sha256: buildFixtureDigest("synthetic binary " + name), Bytes: 1}, Archive: buildArtifact{Id: "images/" + name + ".oci.tar", Path: "images/" + name + ".oci.tar", Kind: "image-build-evidence", Sha256: buildFixtureDigest("synthetic archive " + name), Bytes: 1}, DockerfileSha256: buildFixtureDigest("synthetic recipe " + name), PlatformDigest: buildFixtureDigest("synthetic platform " + name), ConfigDigest: buildFixtureDigest("synthetic config " + name), ContainerBinary: container, RootfsVerified: true, SourceToImageVerified: true})
	}
	for path, digest := range map[string]string{"inputs/config.json": receipt.ConfigSha256, "inputs/parent-manifest.json": receipt.Parent.File.Sha256, "inputs/package-image-receipt.json": packageHash, "inputs/scratch-image-receipt.json": scratchHash} {
		receipt.Artifacts = append(receipt.Artifacts, buildArtifact{Id: path, Path: path, Kind: "image-build-evidence", Sha256: digest, Bytes: 1})
	}
	return receipt
}

// Ordering is canonical while the unsupported authorities remain explicitly false.
func TestImageAggregateSealsOnlyCompleteLocalCoverage(t *testing.T) {
	receipt := aggregateTestReceipt()
	if err := sealImageAggregate(&receipt); err != nil {
		t.Fatal(err)
	}
	expected := receipt.ContentHash
	receipt.Images[0], receipt.Images[7] = receipt.Images[7], receipt.Images[0]
	receipt.Supplements[0], receipt.Supplements[1] = receipt.Supplements[1], receipt.Supplements[0]
	if err := sealImageAggregate(&receipt); err != nil || receipt.ContentHash != expected {
		t.Fatalf("aggregate seal changed with presentation order: %v", err)
	}
	if receipt.ReproducibilityVerified || receipt.ReleaseComplete || receipt.DeploymentApproved || !receipt.SourceToImageVerified || len(receipt.MissingImages) != 0 {
		t.Fatal("aggregate authority differs")
	}
}

// Duplicate coverage, missing roles and detached evidence cannot be promoted by
// a caller that bypasses orchestration and directly requests a new content seal.
func TestImageAggregateRejectsIncompleteUnionAndAuthority(t *testing.T) {
	for _, fault := range []string{"missing", "duplicate", "foreign", "supplement", "unverified", "rootfs", "missing-list", "reproducibility", "release", "deployment", "metadata"} {
		receipt := aggregateTestReceipt()
		switch fault {
		case "missing":
			receipt.Images = receipt.Images[:7]
		case "duplicate":
			receipt.Images[1] = receipt.Images[0]
		case "foreign":
			receipt.Images[0].Binary.Id = "server-other"
		case "supplement":
			receipt.Supplements[1] = receipt.Supplements[0]
		case "unverified":
			receipt.Images[0].SourceToImageVerified = false
		case "rootfs":
			receipt.Images[0].RootfsVerified = false
		case "missing-list":
			receipt.MissingImages = []string{scratchImageId}
		case "reproducibility":
			receipt.ReproducibilityVerified = true
		case "release":
			receipt.ReleaseComplete = true
		case "deployment":
			receipt.DeploymentApproved = true
		case "metadata":
			receipt.Artifacts[0].Sha256 = buildFixtureDigest("synthetic detached metadata")
		}
		if err := sealImageAggregate(&receipt); err == nil {
			t.Fatalf("accepted aggregate %s", fault)
		}
	}
}

// No tool/network fields are available, and case aliases cannot hide duplicate pins.
func TestImageAggregateConfigRejectsCapabilitiesAndAmbiguousJson(t *testing.T) {
	for _, raw := range []string{`{"schema":"a","Schema":"b"}`, `{"candidate_manifest":{"path":"a","PATH":"b"}}`, `{"buildx":{"path":"/synthetic-tool"}}`, `{"deployment_approved":true}`, `{} {}`} {
		var config imageAggregateConfig
		if err := decodeImageJson([]byte(raw), &config, true); err == nil {
			t.Fatalf("accepted unsupported aggregate config: %s", raw)
		}
	}
}

// A failed original-parent check retains only a fresh attempt. Reuse cannot
// overwrite it, and original input bytes remain intact after every failure.
func TestImageAggregateFailureIsNonMutatingAndCannotOverwrite(t *testing.T) {
	root := t.TempDir()
	parent := aggregateTestWrite(t, filepath.Join(root, "parent/manifest.json"), map[string]string{"schema": "synthetic invalid parent"})
	a := aggregateTestWrite(t, filepath.Join(root, "scratch/receipt.json"), map[string]string{"schema": imageBuildSchema})
	b := aggregateTestWrite(t, filepath.Join(root, "package/receipt.json"), map[string]string{"schema": packageImageSchema})
	config := imageAggregateConfig{Schema: imageAggregateSchema, CandidateManifest: parent, Supplements: []toolPin{a, b}, Output: filepath.Join(root, "output")}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeImageAggregate(t.Context(), config, raw); err == nil {
		t.Fatal("accepted invalid parent")
	}
	if _, err := os.Stat(filepath.Join(config.Output, "image-aggregate.json")); !os.IsNotExist(err) {
		t.Fatal("failed aggregation emitted a seal")
	}
	if err := executeImageAggregate(t.Context(), config, raw); err == nil || !strings.Contains(err.Error(), "fresh aggregate output") {
		t.Fatalf("retry overwrote output: %v", err)
	}
	for _, pin := range []toolPin{parent, a, b} {
		actual, err := buildFile(pin.Path)
		if err != nil || actual.Sha256 != pin.Sha256 {
			t.Fatalf("input mutated: %v", err)
		}
	}
	config.Output = filepath.Join(root, "parent/nested")
	if err := config.validate(); err == nil {
		t.Fatal("accepted output inside parent")
	}
	config.Output = filepath.Join(root, "another-output")
	config.Supplements = []toolPin{a, a}
	if err := config.validate(); err == nil {
		t.Fatal("accepted duplicate supplement path")
	}
}

// Cancellation is checked before reading any artifact and cannot turn partial
// work into a passing verification or create a fresh output directory.
func TestImageAggregateArtifactCancellationRefusesPartialResult(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := verifyAggregateArtifacts(ctx, t.TempDir(), []buildArtifact{{Id: "synthetic"}})
	if err != context.Canceled {
		t.Fatalf("canceled artifact read returned %v", err)
	}
}

// Fixed build command evidence is data-only, but must reproduce the reviewed
// arguments and successful exit before any image gains aggregate coverage.
func TestImageAggregateBuildEvidenceRejectsResealedCommandDrift(t *testing.T) {
	root := t.TempDir()
	config := imageBuildConfig{Output: "/synthetic-build", Buildx: toolPin{Path: "/synthetic-tools/buildx"}, DockerSocket: "/synthetic-daemon.sock"}
	args := scratchImageArguments(1)
	platform, digest := buildFixtureDigest("synthetic platform"), buildFixtureDigest("synthetic config")
	command := struct {
		Directory string   `json:"directory"`
		Tool      string   `json:"tool"`
		Arguments []string `json:"arguments"`
	}{Directory: config.Output, Tool: config.Buildx.Path, Arguments: args}
	write := func(path string, value any) { aggregateTestWrite(t, filepath.Join(root, path), value) }
	write("logs/build-competitionworker.command.json", command)
	if err := os.WriteFile(filepath.Join(root, "logs/build-competitionworker.exit"), []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	write("images/competitionworker.metadata.json", map[string]string{"containerimage.digest": platform, "containerimage.config.digest": digest})
	write("inputs/environment.json", imageBuildEnvironment(config))
	artifacts, err := packageEvidenceArtifacts(root, []string{"logs", "images", "inputs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAggregateImageEvidence(root, "competitionworker", artifacts, config, args, platform, digest); err != nil {
		t.Fatal(err)
	}
	command.Arguments = append(append([]string{}, args...), "--push")
	write("logs/build-competitionworker.command.json", command)
	artifacts, err = packageEvidenceArtifacts(root, []string{"logs", "images", "inputs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAggregateImageEvidence(root, "competitionworker", artifacts, config, args, platform, digest); err == nil || !strings.Contains(err.Error(), "command differs") {
		t.Fatalf("accepted resealed command drift: %v", err)
	}
	if reflect.DeepEqual(command.Arguments, args) {
		t.Fatal("control did not change command")
	}
	command.Arguments = args
	write("logs/build-competitionworker.command.json", command)
	ambiguous := `{"containerimage.digest":"` + platform + `","Containerimage.Digest":"` + platform + `","containerimage.config.digest":"` + digest + `"}`
	if err := os.WriteFile(filepath.Join(root, "images/competitionworker.metadata.json"), []byte(ambiguous), 0600); err != nil {
		t.Fatal(err)
	}
	artifacts, err = packageEvidenceArtifacts(root, []string{"logs", "images", "inputs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAggregateImageEvidence(root, "competitionworker", artifacts, config, args, platform, digest); err == nil || !strings.Contains(err.Error(), "duplicate JSON") {
		t.Fatalf("accepted resealed metadata aliases: %v", err)
	}
}
