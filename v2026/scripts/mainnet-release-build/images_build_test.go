// Exercise parent custody, offline authority and failure retention without Docker.
package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// encoding/json accepts case-folded field aliases, so exact-key duplicates alone
// do not establish an unambiguous config boundary. Unicode aliases fail too.
func TestImageJsonRejectsCaseAliasKeys(t *testing.T) {
	for _, raw := range []string{`{"schema":"first","Schema":"second"}`, `{"candidate_manifest":{"path":"first","PATH":"second"}}`, `{"schema":"first","ſchema":"second"}`} {
		var config imageBuildConfig
		if err := decodeImageJson([]byte(raw), &config, true); err == nil || !strings.Contains(err.Error(), "duplicate JSON member") {
			t.Fatalf("accepted configuration alias: %s: %v", raw, err)
		}
		if err := validateBuildJsonKeys([]byte(raw), true); err == nil {
			t.Fatalf("source-build config also accepted alias: %s", raw)
		}
	}
}

// Parent fixtures preserve the full census while using only synthetic identities.
func imageTestParent(t *testing.T, recipe string) (toolPin, buildManifest) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "candidate")
	parent := buildFixtureManifest()
	parent.Schema, parent.CandidateId, parent.Platform, parent.SourceDateEpoch = buildSchema, "synthetic-candidate", "linux/amd64", 1
	for i := range parent.Artifacts {
		artifact := &parent.Artifacts[i]
		var raw []byte
		if artifact.Kind == "binary" {
			raw = []byte("synthetic binary " + artifact.Id)
		} else if artifact.Kind == "image-input" {
			name := strings.TrimPrefix(artifact.Id, "image-")
			if strings.HasSuffix(name, "-binary") {
				name = strings.TrimSuffix(name, "-binary")
				artifact.Path = "contexts/" + name + "/build/linux/amd64/" + name
				raw = []byte("synthetic binary server-" + name)
			} else {
				name = strings.TrimSuffix(name, "-dockerfile")
				raw = []byte("FROM synthetic.example/runtime\n")
				if name == "competitionworker" {
					raw = []byte(recipe)
				}
			}
		} else {
			continue
		}
		artifact.Sha256, artifact.Bytes = buildFixtureDigest(string(raw)), int64(len(raw))
		path := filepath.Join(root, artifact.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for i := range parent.Images {
		image := &parent.Images[i]
		name := strings.TrimPrefix(image.Id, "server-")
		image.ContextPath, image.ContainerBinary = "contexts/"+name, "/usr/local/sbin/bringyour-"+name
		if image.Id == scratchImageId {
			image.ContainerBinary = "/competitionworker"
		}
		for _, artifact := range parent.Artifacts {
			if artifact.Id == image.Id {
				image.BinarySha256 = artifact.Sha256
			}
			if artifact.Id == "image-"+name+"-dockerfile" {
				image.DockerfileSha256 = artifact.Sha256
			}
		}
	}
	if err := sealBuildManifest(&parent); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "manifest.json")
	if err := writeBuildJson(path, parent); err != nil {
		t.Fatal(err)
	}
	artifact, err := buildFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return toolPin{Path: path, Sha256: artifact.Sha256}, parent
}

// Both the supplied file pin and the original domain-separated seal must hold.
func TestImageBuildRejectsParentPinAndSealSubstitution(t *testing.T) {
	pin, _ := imageTestParent(t, scratchImageDockerfile)
	if _, _, err := readImageParent(pin); err != nil {
		t.Fatal(err)
	}
	wrong := pin
	wrong.Sha256 = buildFixtureDigest("other parent")
	if _, _, err := readImageParent(wrong); err == nil {
		t.Fatal("accepted wrong external parent pin")
	}
	raw, err := os.ReadFile(pin.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "synthetic-candidate", "changed-candidate", 1))
	if err := os.WriteFile(pin.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	pin.Sha256 = buildFixtureDigest(string(raw))
	if _, _, err := readImageParent(pin); err == nil || !strings.Contains(err.Error(), "content seal") {
		t.Fatalf("accepted resealed file with stale parent content seal: %v", err)
	}
}

// Verify every role even when this bounded increment only builds one of them.
func TestImageBuildChecksAllEightPreparedInputs(t *testing.T) {
	for _, role := range releaseRoles() {
		if !role.Image {
			continue
		}
		pin, parent := imageTestParent(t, scratchImageDockerfile)
		root := filepath.Dir(pin.Path)
		if _, err := verifyImageInputs(root, parent); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "contexts", strings.TrimPrefix(role.Id, "server-"), "Dockerfile")
		if err := os.WriteFile(path, []byte("changed synthetic recipe"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := verifyImageInputs(root, parent); err == nil {
			t.Fatalf("accepted mutated %s input", role.Id)
		}
	}
}

// An authenticated manifest cannot authorize a broader recipe through this seam.
func TestImageBuildRefusesRecipeExpansion(t *testing.T) {
	for _, recipe := range []string{"# syntax=synthetic.example/frontend\n" + scratchImageDockerfile, scratchImageDockerfile + "RUN true\n", strings.Replace(scratchImageDockerfile, "FROM scratch", "FROM synthetic.example/base", 1)} {
		pin, parent := imageTestParent(t, recipe)
		if _, err := verifyImageInputs(filepath.Dir(pin.Path), parent); err == nil || !strings.Contains(err.Error(), "exact reviewed scratch") {
			t.Fatalf("accepted expanded recipe: %v", err)
		}
	}
}

// Files reached through a directory alias must not impersonate frozen artifacts.
func TestImageBuildRejectsSymlinkedContextDirectory(t *testing.T) {
	pin, parent := imageTestParent(t, scratchImageDockerfile)
	root := filepath.Dir(pin.Path)
	path := filepath.Join(root, "contexts", "competitionworker")
	other := filepath.Join(root, "moved-context")
	if err := os.Rename(path, other); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyImageInputs(root, parent); err == nil || !strings.Contains(err.Error(), "physical") {
		t.Fatalf("accepted aliased input: %v", err)
	}
}

// Commands are constructed internally with no inherited daemon, frontend or auth.
func TestImageBuildEnvironmentAndArgumentsStayOffline(t *testing.T) {
	for key, value := range map[string]string{"DOCKER_HOST": "tcp://daemon.example:2376", "BUILDX_BUILDER": "remote", "DOCKER_CONFIG": "/synthetic/credentials", "HTTP_PROXY": "https://proxy.example", "EXPERIMENTAL_BUILDKIT_SOURCE_POLICY": "/synthetic/allow.json", "LD_PRELOAD": "/synthetic/inject.so"} {
		t.Setenv(key, value)
	}
	config := imageBuildConfig{Output: "/synthetic/output", DockerSocket: "/synthetic/daemon.sock"}
	environment := imageBuildEnvironment(config)
	values := map[string]string{}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		if _, ok := values[key]; ok {
			t.Fatalf("duplicate environment %s", key)
		}
		values[key] = value
	}
	if values["DOCKER_HOST"] != "unix:///synthetic/daemon.sock" || values["BUILDX_BUILDER"] != "default" || values["DOCKER_CONFIG"] != "/synthetic/output/docker-config" || values["EXPERIMENTAL_BUILDKIT_SOURCE_POLICY"] != "/synthetic/output/inputs/source-policy.json" {
		t.Fatal("ambient Docker authority survived")
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "LD_PRELOAD", "DOCKER_CONTEXT", "BUILDKIT_HOST"} {
		if _, ok := values[key]; ok {
			t.Fatalf("ambient %s survived", key)
		}
	}
	arguments := strings.Join(scratchImageArguments(7), " ")
	for _, expected := range []string{"--builder default", "--no-cache", "--platform linux/amd64", "--network=none", "SOURCE_DATE_EPOCH=7", "--provenance=false", "--sbom=false", "type=oci,", "rewrite-timestamp=true"} {
		if !strings.Contains(arguments, expected) {
			t.Fatalf("missing offline constraint %q", expected)
		}
	}
	for _, forbidden := range []string{"--push", "--load", "--tag", "--allow", "--secret", "--ssh", "--pull", "--build-context"} {
		if strings.Contains(arguments, forbidden) {
			t.Fatalf("expanded image authority: %s", forbidden)
		}
	}
}

// No successful seal can be emitted after a real child process exits nonzero.
func TestImageBuildFailureRetainsCommandAndNeverSeals(t *testing.T) {
	pin, _ := imageTestParent(t, scratchImageDockerfile)
	root := t.TempDir()
	socketPath := filepath.Join(root, "daemon.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	tool, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	tool, err = filepath.EvalSymlinks(tool)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := buildFile(tool)
	if err != nil {
		t.Fatal(err)
	}
	config := imageBuildConfig{Schema: imageBuildSchema, CandidateManifest: pin, Output: filepath.Join(root, "output"), DockerSocket: socketPath, Buildx: toolPin{Path: tool, Sha256: artifact.Sha256}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeImageBuild(context.Background(), config, raw); err == nil {
		t.Fatal("accepted failed builder")
	}
	if _, err := os.Stat(filepath.Join(config.Output, "image-receipt.json")); !os.IsNotExist(err) {
		t.Fatalf("failed build emitted a receipt: %v", err)
	}
	exit, err := os.ReadFile(filepath.Join(config.Output, "logs/buildx-version.exit"))
	if err != nil || string(exit) != "1\n" {
		t.Fatalf("missing failed command exit: %q %v", exit, err)
	}
	if _, err := os.Stat(filepath.Join(config.Output, "logs/buildx-version.command.json")); err != nil {
		t.Fatal(err)
	}
	if err := executeImageBuild(context.Background(), config, raw); err == nil || !strings.Contains(err.Error(), "fresh image output") {
		t.Fatalf("failed evidence overwritten: %v", err)
	}
}

// Census and authority are preserved even when a caller directly tries to seal.
func TestImageReceiptRejectsApprovalAndIncompleteCensus(t *testing.T) {
	for _, claim := range []string{"aggregate-image", "reproducibility", "release", "deployment", "missing-role", "unverified-rootfs", "unverified-binary"} {
		receipt := imageBuildReceipt{Schema: imageBuildSchema, Image: scratchReadback{Id: scratchImageId, RootfsVerified: true, SourceToImageVerified: true}}
		for _, role := range releaseRoles() {
			if role.Image && role.Id != scratchImageId {
				receipt.MissingImages = append(receipt.MissingImages, role.Id)
			}
		}
		switch claim {
		case "aggregate-image":
			receipt.SourceToImageVerified = true
		case "reproducibility":
			receipt.ReproducibilityVerified = true
		case "release":
			receipt.ReleaseComplete = true
		case "deployment":
			receipt.DeploymentApproved = true
		case "missing-role":
			receipt.MissingImages = receipt.MissingImages[:6]
		case "unverified-rootfs":
			receipt.Image.RootfsVerified = false
		case "unverified-binary":
			receipt.Image.SourceToImageVerified = false
		}
		if err := sealImageReceipt(&receipt); err == nil || !strings.Contains(err.Error(), "census or authority") {
			t.Fatalf("accepted %s or failed at unrelated boundary: %v", claim, err)
		}
	}
}

// Positive receipt construction joins independently verified bytes to custody.
func imageTestReceipt(t *testing.T) imageBuildReceipt {
	t.Helper()
	path, binary := scratchTestArchive(t, nil)
	readback, err := inspectScratchImage(path, binary)
	if err != nil {
		t.Fatal(err)
	}
	receipt := imageBuildReceipt{Schema: imageBuildSchema, CandidateId: "synthetic-candidate", ParentManifestSha256: buildFixtureDigest("synthetic parent manifest"), ParentContentHash: buildFixtureDigest("synthetic parent seal"), ConfigSha256: buildFixtureDigest("synthetic config"), Image: readback}
	for _, role := range releaseRoles() {
		if role.Image && role.Id != scratchImageId {
			receipt.MissingImages = append(receipt.MissingImages, role.Id)
		}
	}
	for path, digest := range map[string]string{"inputs/parent-manifest.json": receipt.ParentManifestSha256, "inputs/config.json": receipt.ConfigSha256, "images/competitionworker.oci.tar": readback.ArchiveSha256, "contexts/competitionworker/build/linux/amd64/competitionworker": readback.BinarySha256, "contexts/competitionworker/Dockerfile": buildFixtureDigest(scratchImageDockerfile), "inputs/source-policy.json": buildFixtureDigest(imageSourcePolicy)} {
		receipt.Artifacts = append(receipt.Artifacts, buildArtifact{Id: path, Kind: "image-build-evidence", Path: path, Sha256: digest, Bytes: binary.Bytes})
	}
	return receipt
}

// The sidecar seal is stable while all complete-release flags remain false.
func TestImageReceiptSealsBoundSupplementOnly(t *testing.T) {
	receipt := imageTestReceipt(t)
	if err := sealImageReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	wanted := receipt.ContentHash
	if err := sealImageReceipt(&receipt); err != nil || receipt.ContentHash != wanted || wanted == "" {
		t.Fatalf("unstable supplement seal: %v", err)
	}
	if receipt.ReleaseComplete || receipt.DeploymentApproved || receipt.ReproducibilityVerified || receipt.SourceToImageVerified || len(receipt.MissingImages) != 7 {
		t.Fatal("partial image receipt promoted release authority")
	}
}

// Every retained path joining a verified identity must be present and immutable.
func TestImageReceiptRejectsDetachedEvidence(t *testing.T) {
	for _, path := range []string{"inputs/parent-manifest.json", "inputs/config.json", "images/competitionworker.oci.tar", "contexts/competitionworker/build/linux/amd64/competitionworker", "contexts/competitionworker/Dockerfile", "inputs/source-policy.json"} {
		receipt := imageTestReceipt(t)
		for i := range receipt.Artifacts {
			if receipt.Artifacts[i].Path == path {
				receipt.Artifacts[i].Sha256 = buildFixtureDigest("substituted evidence")
			}
		}
		if err := sealImageReceipt(&receipt); err == nil {
			t.Fatalf("accepted detached %s", path)
		}
	}
}
