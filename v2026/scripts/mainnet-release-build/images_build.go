// A separate receipt adds one offline scratch image to an immutable composition.
// Package-bearing recipes use the separate pinned local-input supplement.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const imageBuildSchema = "urnetwork-mainnet-scratch-image-build-v1"
const scratchImageId = "server-competitionworker"
const scratchImageDockerfile = "FROM scratch\nARG TARGETPLATFORM\nCOPY build/$TARGETPLATFORM/competitionworker /competitionworker\nUSER 65532:65532\nENTRYPOINT [\"/competitionworker\"]\n"
const imageSourcePolicy = `{"rules":[{"action":"DENY","selector":{"identifier":"docker-image://*"}},{"action":"DENY","selector":{"identifier":"http://*"}},{"action":"DENY","selector":{"identifier":"https://*"}},{"action":"DENY","selector":{"identifier":"git://*"}},{"action":"DENY","selector":{"identifier":"ssh://*"}}]}`

// Only a pinned local executable and Unix socket can build the fixed recipe.
type imageBuildConfig struct {
	Schema            string  `json:"schema"`
	CandidateManifest toolPin `json:"candidate_manifest"`
	Output            string  `json:"output"`
	Buildx            toolPin `json:"buildx"`
	DockerSocket      string  `json:"docker_socket"`
}

// The supplement cannot replace or promote its parent composition's claims.
type imageBuildReceipt struct {
	Schema                  string          `json:"schema"`
	CandidateId             string          `json:"candidate_id"`
	ParentManifestSha256    string          `json:"parent_manifest_sha256"`
	ParentContentHash       string          `json:"parent_content_hash"`
	ConfigSha256            string          `json:"config_sha256"`
	Image                   scratchReadback `json:"image"`
	MissingImages           []string        `json:"missing_images"`
	Artifacts               []buildArtifact `json:"artifacts"`
	SourceToImageVerified   bool            `json:"source_to_image_verified"`
	ReproducibilityVerified bool            `json:"reproducibility_verified"`
	ReleaseComplete         bool            `json:"release_complete"`
	DeploymentApproved      bool            `json:"deployment_approved"`
	Limitations             []string        `json:"limitations"`
	ContentHash             string          `json:"content_hash"`
}

// Strict JSON is shared by the new config, parent manifest and OCI metadata.
func decodeImageJson(raw []byte, target any, strict bool) error {
	if err := validateBuildJsonKeys(raw, strict); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if strict {
		decoder.DisallowUnknownFields()
	}
	return decoder.Decode(target)
}

// Existing physical paths may contain neither lexical aliases nor symlinks.
func physicalImagePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("image path must be canonical absolute")
	}
	physical, err := filepath.EvalSymlinks(path)
	if err != nil || physical != path {
		return errors.New("image path must be physical and existing")
	}
	return nil
}

// New output must be disjoint from the immutable parent candidate, in both directions.
func (self imageBuildConfig) validate() error {
	if self.Schema != imageBuildSchema {
		return errors.New("unsupported image build schema")
	}
	for _, path := range []string{self.CandidateManifest.Path, self.Buildx.Path, self.DockerSocket, filepath.Dir(self.Output)} {
		if err := physicalImagePath(path); err != nil {
			return err
		}
	}
	if !filepath.IsAbs(self.Output) || filepath.Clean(self.Output) != self.Output {
		return errors.New("image output must be canonical absolute")
	}
	parent := filepath.Dir(self.CandidateManifest.Path)
	for _, pair := range [][2]string{{parent, self.Output}, {self.Output, parent}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err != nil || relative == "." || filepath.IsLocal(relative) {
			return errors.New("image output must be disjoint from parent candidate")
		}
	}
	info, err := os.Lstat(self.DockerSocket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return errors.New("local Docker Unix socket required")
	}
	return verifyBuildTool(self.Buildx)
}

// No inherited credentials, proxy, remote builder or Docker configuration survive.
func imageBuildEnvironment(config imageBuildConfig) []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(config.Output, "home"), "TMPDIR=" + filepath.Join(config.Output, "tmp"), "DOCKER_CONFIG=" + filepath.Join(config.Output, "docker-config"), "BUILDX_CONFIG=" + filepath.Join(config.Output, "buildx-config"), "DOCKER_HOST=unix://" + config.DockerSocket, "BUILDX_BUILDER=default", "BUILDX_METADATA_PROVENANCE=disabled", "EXPERIMENTAL_BUILDKIT_SOURCE_POLICY=" + filepath.Join(config.Output, "inputs/source-policy.json")}
}

// Fixed scratch/COPY input and disabled scanner/frontend downloads bound offline scope.
func scratchImageArguments(epoch int64) []string {
	return []string{"build", "--builder", "default", "--no-cache", "--platform", "linux/amd64", "--network=none", "--progress=plain", "--build-arg", "SOURCE_DATE_EPOCH=" + strconv.FormatInt(epoch, 10), "--provenance=false", "--sbom=false", "--metadata-file", "images/competitionworker.metadata.json", "--output", "type=oci,dest=images/competitionworker.oci.tar,rewrite-timestamp=true", "contexts/competitionworker"}
}

// Check the external file pin and original domain-separated seal independently.
func readImageParent(pin toolPin) (buildManifest, []byte, error) {
	var parent buildManifest
	if err := physicalImagePath(pin.Path); err != nil {
		return parent, nil, err
	}
	raw, err := readBuildInput(pin.Path, 16*1024*1024)
	if err != nil {
		return parent, nil, err
	}
	hash := sha256.Sum256(raw)
	if pin.Sha256 != "sha256:"+hex.EncodeToString(hash[:]) {
		return parent, nil, errors.New("parent manifest file pin differs")
	}
	if err := decodeImageJson(raw, &parent, true); err != nil {
		return parent, nil, err
	}
	wanted := parent.ContentHash
	if parent.Schema != buildSchema || parent.Platform != "linux/amd64" || parent.SourceDateEpoch <= 0 {
		return parent, nil, errors.New("parent composition identity or platform differs")
	}
	if err := sealBuildManifest(&parent); err != nil {
		return parent, nil, err
	}
	if wanted != parent.ContentHash {
		return parent, nil, errors.New("parent composition content seal differs")
	}
	return parent, raw, nil
}

// All eight image inputs are checked; only the exact scratch recipe may execute.
func verifyImageInputs(directory string, parent buildManifest) (buildArtifact, error) {
	artifactKVs := map[string]buildArtifact{}
	for _, artifact := range parent.Artifacts {
		artifactKVs[artifact.Id] = artifact
	}
	var selected buildArtifact
	for _, image := range parent.Images {
		name := strings.TrimPrefix(image.Id, "server-")
		contextPath := "contexts/" + name
		containerBinary := "/usr/local/sbin/bringyour-" + name
		if image.Id == scratchImageId {
			containerBinary = "/competitionworker"
		}
		if image.ContextPath != contextPath || image.ContainerBinary != containerBinary {
			return selected, errors.New("parent image context or container path differs")
		}
		for _, expected := range []struct{ id, path, kind string }{{image.Id, "binaries/" + image.Id, "binary"}, {"image-" + name + "-binary", contextPath + "/build/linux/amd64/" + name, "image-input"}, {"image-" + name + "-dockerfile", contextPath + "/Dockerfile", "image-input"}} {
			artifact, ok := artifactKVs[expected.id]
			if !ok || artifact.Path != expected.path || artifact.Kind != expected.kind {
				return selected, errors.New("parent image input path or kind differs")
			}
			path := filepath.Join(directory, artifact.Path)
			if err := physicalImagePath(path); err != nil {
				return selected, err
			}
			actual, err := buildFile(path)
			if err != nil || actual.Sha256 != artifact.Sha256 || actual.Bytes != artifact.Bytes {
				return selected, fmt.Errorf("parent image input differs: %s", expected.id)
			}
		}
		binary := artifactKVs[image.Id]
		if binary.Bytes != artifactKVs["image-"+name+"-binary"].Bytes {
			return selected, errors.New("parent image binary lengths differ")
		}
		if image.Id == scratchImageId {
			selected = binary
			if selected.Bytes > maximumScratchArchiveBytes-1024*1024 {
				return selected, errors.New("scratch binary exceeds offline build bound")
			}
			raw, err := readBuildInput(filepath.Join(directory, contextPath, "Dockerfile"), 16*1024)
			if err != nil || string(raw) != scratchImageDockerfile {
				return selected, errors.New("offline build requires the exact reviewed scratch Dockerfile")
			}
		}
	}
	if selected.Id == "" {
		return selected, errors.New("scratch image absent from parent")
	}
	return selected, nil
}

// Failed executions leave a new directory and command exits, never a sealed receipt.
func executeImageConfig(ctx context.Context, path string) error {
	raw, err := readBuildInput(path, 1024*1024)
	if err != nil {
		return err
	}
	var config imageBuildConfig
	if err := decodeImageJson(raw, &config, true); err != nil {
		return err
	}
	return executeImageBuild(ctx, config, raw)
}

// Never execute untrusted proposed build arguments or modify the original output.
func executeImageBuild(ctx context.Context, config imageBuildConfig, rawConfig []byte) error {
	if err := config.validate(); err != nil {
		return err
	}
	parent, rawParent, err := readImageParent(config.CandidateManifest)
	if err != nil {
		return err
	}
	parentDirectory := filepath.Dir(config.CandidateManifest.Path)
	selected, err := verifyImageInputs(parentDirectory, parent)
	if err != nil {
		return err
	}
	if err := os.Mkdir(config.Output, 0700); err != nil {
		return fmt.Errorf("fresh image output required: %w", err)
	}
	for _, directory := range []string{"inputs", "logs", "images", "contexts/competitionworker/build/linux/amd64", "home", "tmp", "docker-config", "buildx-config"} {
		if err := os.MkdirAll(filepath.Join(config.Output, directory), 0700); err != nil {
			return err
		}
	}
	for _, input := range []struct {
		path string
		raw  []byte
	}{{"inputs/config.json", rawConfig}, {"inputs/parent-manifest.json", rawParent}, {"inputs/source-policy.json", []byte(imageSourcePolicy)}} {
		if err := writeBuildBytes(filepath.Join(config.Output, input.path), input.raw); err != nil {
			return err
		}
	}
	for _, relative := range []string{"contexts/competitionworker/Dockerfile", "contexts/competitionworker/build/linux/amd64/competitionworker"} {
		copied, err := copyBuildFile(filepath.Join(parentDirectory, relative), filepath.Join(config.Output, relative), 0755)
		if err != nil {
			return err
		}
		if strings.HasSuffix(relative, "/Dockerfile") {
			raw, err := readBuildInput(filepath.Join(config.Output, relative), 16*1024)
			if err != nil || string(raw) != scratchImageDockerfile {
				return errors.New("copied scratch recipe differs before execution")
			}
		} else if copied.Sha256 != selected.Sha256 || copied.Bytes != selected.Bytes {
			return errors.New("copied scratch binary differs before execution")
		}
	}
	environment := imageBuildEnvironment(config)
	if err := writeBuildJson(filepath.Join(config.Output, "inputs/environment.json"), environment); err != nil {
		return err
	}
	for _, command := range []struct {
		id   string
		args []string
	}{{"buildx-version", []string{"version"}}, {"builder-inspect", []string{"inspect", "default"}}, {"build-competitionworker", scratchImageArguments(parent.SourceDateEpoch)}} {
		if err := runBuildCommand(ctx, config.Output, command.id, config.Output, config.Buildx.Path, environment, command.args...); err != nil {
			return err
		}
	}
	readback, err := inspectScratchImage(filepath.Join(config.Output, "images/competitionworker.oci.tar"), selected)
	if err != nil {
		return err
	}
	rawMetadata, err := readBuildInput(filepath.Join(config.Output, "images/competitionworker.metadata.json"), 1024*1024)
	if err != nil {
		return err
	}
	var metadata struct {
		Digest       string `json:"containerimage.digest"`
		ConfigDigest string `json:"containerimage.config.digest"`
	}
	if err := decodeImageJson(rawMetadata, &metadata, false); err != nil {
		return err
	}
	if metadata.Digest != readback.PlatformDigest || metadata.ConfigDigest != readback.ConfigDigest {
		return errors.New("builder metadata does not match independent OCI readback")
	}
	if err := verifyBuildTool(config.Buildx); err != nil {
		return err
	}
	if _, _, err := readImageParent(config.CandidateManifest); err != nil {
		return err
	}
	if _, err := verifyImageInputs(parentDirectory, parent); err != nil {
		return err
	}
	for _, relative := range []string{"contexts/competitionworker/Dockerfile", "contexts/competitionworker/build/linux/amd64/competitionworker"} {
		original, err := buildFile(filepath.Join(parentDirectory, relative))
		if err != nil {
			return err
		}
		copied, err := buildFile(filepath.Join(config.Output, relative))
		if err != nil || copied.Sha256 != original.Sha256 || copied.Bytes != original.Bytes {
			return errors.New("copied image context changed during build")
		}
	}
	configHash := sha256.Sum256(rawConfig)
	receipt := imageBuildReceipt{Schema: imageBuildSchema, CandidateId: parent.CandidateId, ParentManifestSha256: config.CandidateManifest.Sha256, ParentContentHash: parent.ContentHash, ConfigSha256: "sha256:" + hex.EncodeToString(configHash[:]), Image: readback, MissingImages: []string{}, Artifacts: []buildArtifact{}, Limitations: []string{"Only the exact scratch competitionworker recipe is admitted; seven Ubuntu/remote-ADD recipes still lack offline input closure and current OCI readback.", "SBOM and provenance generation are explicitly disabled; attestation/scanner policy remains open.", "Archive/rootfs verification does not execute the application or qualify its runtime behavior.", "The local BuildKit service is recorded, not independently attested; independent builder, arm64 and reproducibility remain open.", "Parent composition, source qualification, runtime configuration, release completion and deployment approval are not promoted by this supplement."}}
	for _, image := range parent.Images {
		if image.Id != scratchImageId {
			receipt.MissingImages = append(receipt.MissingImages, image.Id)
		}
	}
	for _, directory := range []string{"inputs", "logs", "images", "contexts"} {
		if err := filepath.WalkDir(filepath.Join(config.Output, directory), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			artifact, err := buildFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(config.Output, path)
			if err != nil {
				return err
			}
			artifact.Id, artifact.Kind, artifact.Path = filepath.ToSlash(relative), "image-build-evidence", filepath.ToSlash(relative)
			receipt.Artifacts = append(receipt.Artifacts, artifact)
			return nil
		}); err != nil {
			return err
		}
	}
	if err := sealImageReceipt(&receipt); err != nil {
		return err
	}
	return writeBuildJson(filepath.Join(config.Output, "image-receipt.json"), receipt)
}

// A bounded supplement has one verified image, seven missing roles and no approval.
func sealImageReceipt(receipt *imageBuildReceipt) error {
	wanted := []string{}
	for _, role := range releaseRoles() {
		if role.Image && role.Id != scratchImageId {
			wanted = append(wanted, role.Id)
		}
	}
	if receipt.Schema != imageBuildSchema || receipt.SourceToImageVerified || receipt.ReproducibilityVerified || receipt.ReleaseComplete || receipt.DeploymentApproved || !reflect.DeepEqual(receipt.MissingImages, wanted) || receipt.Image.Id != scratchImageId || !receipt.Image.SourceToImageVerified || !receipt.Image.RootfsVerified {
		return errors.New("scratch image supplement census or authority differs")
	}
	digestPattern := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, digest := range []string{receipt.ParentManifestSha256, receipt.ParentContentHash, receipt.ConfigSha256, receipt.Image.ArchiveSha256, receipt.Image.PlatformDigest, receipt.Image.ConfigDigest, receipt.Image.LayerDigest, receipt.Image.LayerDiffId, receipt.Image.BinarySha256} {
		if !digestPattern.MatchString(digest) {
			return errors.New("image receipt digest differs")
		}
	}
	if receipt.CandidateId == "" || receipt.Image.Platform != "linux/amd64" || receipt.Image.ContainerBinary != "/competitionworker" || receipt.Image.BinaryBytes <= 0 {
		return errors.New("image receipt identity differs")
	}
	artifactKVs := map[string]buildArtifact{}
	for _, artifact := range receipt.Artifacts {
		if _, ok := artifactKVs[artifact.Id]; ok || artifact.Id != artifact.Path || artifact.Kind != "image-build-evidence" || !filepath.IsLocal(artifact.Path) || artifact.Path != filepath.ToSlash(filepath.Clean(artifact.Path)) || !digestPattern.MatchString(artifact.Sha256) || artifact.Bytes <= 0 || artifact.Bytes > maximumBuildFileBytes {
			return errors.New("image receipt artifact identity differs")
		}
		artifactKVs[artifact.Id] = artifact
	}
	for _, joined := range []struct{ path, digest string }{{"inputs/parent-manifest.json", receipt.ParentManifestSha256}, {"inputs/config.json", receipt.ConfigSha256}, {"images/competitionworker.oci.tar", receipt.Image.ArchiveSha256}, {"contexts/competitionworker/build/linux/amd64/competitionworker", receipt.Image.BinarySha256}} {
		if artifactKVs[joined.path].Sha256 != joined.digest {
			return errors.New("image receipt not joined to retained evidence")
		}
	}
	if artifactKVs["contexts/competitionworker/build/linux/amd64/competitionworker"].Bytes != receipt.Image.BinaryBytes {
		return errors.New("image receipt binary length differs")
	}
	recipeHash := sha256.Sum256([]byte(scratchImageDockerfile))
	if artifactKVs["contexts/competitionworker/Dockerfile"].Sha256 != "sha256:"+hex.EncodeToString(recipeHash[:]) {
		return errors.New("image receipt scratch recipe differs")
	}
	policyHash := sha256.Sum256([]byte(imageSourcePolicy))
	if artifactKVs["inputs/source-policy.json"].Sha256 != "sha256:"+hex.EncodeToString(policyHash[:]) {
		return errors.New("image receipt source policy differs")
	}
	sort.Slice(receipt.Artifacts, func(i, j int) bool { return receipt.Artifacts[i].Id < receipt.Artifacts[j].Id })
	receipt.ContentHash = ""
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(append([]byte(imageBuildSchema+"\x00"), raw...))
	receipt.ContentHash = "sha256:" + hex.EncodeToString(hash[:])
	return nil
}
