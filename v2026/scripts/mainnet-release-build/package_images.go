// Localize the seven reviewed recipes without changing installation or runtime
// instructions. The selected base and every package must already exist locally.
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
	"sort"
	"strconv"
	"strings"
)

const packageImageSchema = "urnetwork-mainnet-package-image-build-v1"
const packageBaseDigest = "sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea"
const packageBaseName = "ubuntu:24.04@" + packageBaseDigest
const packageLockDigest = "sha256:ba1b135dd9580dff581de8aa459c31cbce0dc3a58d9164f254acc5e351abe30e"

// Digests pin the whole recipe, including comments, frontend and runtime commands.
func packageRecipeDigests() map[string]string {
	return map[string]string{
		"api":        "sha256:b975aaab7d6bfec3c1c730c169e54ca4faab4bee0ba81e0d8e708050aaa6fba1",
		"taskworker": "sha256:8711c1714b4eb2238de0d6f6d71237c9fc476a52b70d8f38753784254d4cf2b1",
		"proxy":      "sha256:6ad5701e0651cbf2085e3e339c1e0ef76715eb52c51f235cfee522e34197da34",
		"connect":    "sha256:8ea83f8937eb262482c286d0d5143c7995dd70f6ec812bc9434dba878e0715d7",
		"alt":        "sha256:0e20d515e8ccd063d0f06ed9aa6a3a3d621a641064fc45cb39742497a4eb2b62",
		"gossip":     "sha256:899a0fae3dfb594d97da42526712948d3b1b4098578603d33dc6d55949fdd608",
		"mcp":        "sha256:1ca321a4c2c52d6fe44f0a5be01028932099686e501a83e3e2695b52f4537125",
	}
}

// Package pins use lock IDs; their hashes and lengths are independently locked.
type packageImageConfig struct {
	Schema            string             `json:"schema"`
	CandidateManifest toolPin            `json:"candidate_manifest"`
	Output            string             `json:"output"`
	Buildx            toolPin            `json:"buildx"`
	DockerSocket      string             `json:"docker_socket"`
	BaseLayout        string             `json:"base_layout"`
	Packages          map[string]toolPin `json:"packages"`
}

// The reviewed lock is the package identity boundary, not a package resolver.
type imagePackage struct {
	Id           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Architecture string   `json:"architecture"`
	Filename     string   `json:"filename"`
	Sha256       string   `json:"sha256"`
	Size         int64    `json:"size"`
	IndexIds     []string `json:"index_ids"`
}

// Index records remain retained in the exact original lock; no online fetch runs.
type imagePackageLock struct {
	Schema            string              `json:"schema"`
	BaseImage         string              `json:"base_image"`
	Snapshot          string              `json:"snapshot"`
	ArchiveSigningKey string              `json:"archive_signing_key"`
	Indexes           []json.RawMessage   `json:"indexes"`
	Packages          []imagePackage      `json:"packages"`
	Sets              map[string][]string `json:"sets"`
	Services          map[string]string   `json:"services"`
}

// Seven successful readbacks do not merge or promote a separate scratch receipt.
type packageImageReceipt struct {
	Schema                  string            `json:"schema"`
	CandidateId             string            `json:"candidate_id"`
	ParentManifestSha256    string            `json:"parent_manifest_sha256"`
	ParentContentHash       string            `json:"parent_content_hash"`
	ConfigSha256            string            `json:"config_sha256"`
	Base                    packageBase       `json:"base"`
	Images                  []packageReadback `json:"images"`
	MissingImages           []string          `json:"missing_images"`
	Artifacts               []buildArtifact   `json:"artifacts"`
	SourceToImageVerified   bool              `json:"source_to_image_verified"`
	ReproducibilityVerified bool              `json:"reproducibility_verified"`
	ReleaseComplete         bool              `json:"release_complete"`
	DeploymentApproved      bool              `json:"deployment_approved"`
	Limitations             []string          `json:"limitations"`
	ContentHash             string            `json:"content_hash"`
}

// Only source locations change: each remote checksum becomes a local chmod-0600
// copy, matching remote ADD permissions. All remaining bytes stay verbatim.
func localizePackageRecipe(raw []byte, expectedDigest, baseName string, packages []imagePackage, snapshot string) ([]byte, error) {
	if digestImageBytes(raw) != expectedDigest {
		return nil, errors.New("exact reviewed package recipe required")
	}
	replacementKVs := map[string]string{}
	for _, p := range packages {
		remote := "ADD --checksum=sha256:" + p.Sha256 + " " + snapshot + p.Filename + " /runtime-packages/" + p.Architecture + "/" + p.Name + ".deb"
		replacementKVs[remote] = "COPY --chmod=0600 packages/" + p.Id + ".deb /runtime-packages/" + p.Architecture + "/" + p.Name + ".deb"
	}
	lines := strings.Split(string(raw), "\n")
	fromCount := 0
	for i, line := range lines {
		if line == "FROM "+baseName || line == "FROM "+baseName+" AS runtime-packages" {
			lines[i] = strings.Replace(line, baseName, "offline-ubuntu", 1)
			fromCount++
			continue
		}
		if replacement, ok := replacementKVs[line]; ok {
			lines[i] = replacement
			delete(replacementKVs, line)
			continue
		}
		if strings.HasPrefix(line, "FROM ") || strings.HasPrefix(line, "ADD ") {
			return nil, errors.New("unclosed recipe source")
		}
	}
	if fromCount != 2 || len(replacementKVs) != 0 {
		return nil, errors.New("recipe package or base census differs")
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// Byte digests use the same prefixed SHA256 form as manifests and descriptors.
func digestImageBytes(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// The parent retains the lock and joins it to exact currently reviewed recipes.
func readPackageLock(directory string, parent buildManifest) (imagePackageLock, []byte, error) {
	var lock imagePackageLock
	for _, artifact := range parent.Artifacts {
		if artifact.Id != "runtime-packages.lock.json" {
			continue
		}
		if artifact.Path != "inputs/runtime-packages.lock.json" || artifact.Kind != "source-input" || artifact.Sha256 != packageLockDigest {
			return lock, nil, errors.New("reviewed package lock identity differs")
		}
		path := filepath.Join(directory, artifact.Path)
		if err := physicalImagePath(path); err != nil {
			return lock, nil, err
		}
		raw, err := readBuildInput(path, 1024*1024)
		if err != nil {
			return lock, nil, err
		}
		if digestImageBytes(raw) != artifact.Sha256 || int64(len(raw)) != artifact.Bytes {
			return lock, nil, errors.New("package lock bytes differ")
		}
		if err := decodeImageJson(raw, &lock, true); err != nil {
			return lock, nil, err
		}
		if lock.Schema != "urnetwork-server-runtime-packages-v1" || lock.BaseImage != packageBaseName || len(lock.Packages) != 40 || len(lock.Services) != 7 {
			return lock, nil, errors.New("package lock census differs")
		}
		return lock, raw, nil
	}
	return lock, nil, errors.New("parent package lock missing")
}

// Both architectures remain available to the unchanged stage; only amd64 runs.
func serviceImagePackages(lock imagePackageLock, name string) ([]imagePackage, error) {
	set, ok := lock.Services[name]
	if !ok {
		return nil, errors.New("package service missing")
	}
	byId := map[string]imagePackage{}
	for _, p := range lock.Packages {
		byId[p.Id] = p
	}
	packages := []imagePackage{}
	for _, id := range lock.Sets[set] {
		p, ok := byId[id]
		if !ok {
			return nil, errors.New("package set input missing")
		}
		packages = append(packages, p)
	}
	if len(packages) == 0 {
		return nil, errors.New("empty package set")
	}
	return packages, nil
}

// Missing, aliased or modified inputs fail before a builder command can start.
func verifyLocalPackages(config packageImageConfig, lock imagePackageLock) error {
	if len(config.Packages) != len(lock.Packages) {
		return errors.New("exact local package census required")
	}
	for _, p := range lock.Packages {
		pin, ok := config.Packages[p.Id]
		if !ok || pin.Sha256 != "sha256:"+p.Sha256 {
			return fmt.Errorf("local package pin differs: %s", p.Id)
		}
		if err := physicalImagePath(pin.Path); err != nil {
			return fmt.Errorf("local package %s: %w", p.Id, err)
		}
		actual, err := buildFile(pin.Path)
		if err != nil || actual.Sha256 != pin.Sha256 || actual.Bytes != p.Size {
			return fmt.Errorf("local package bytes differ: %s", p.Id)
		}
	}
	return nil
}

// One private OCI context supplies the base; remote sources remain denied.
func packageImageArguments(name string, epoch int64, basePath, platformDigest string) []string {
	return []string{"build", "--builder", "default", "--no-cache", "--platform", "linux/amd64", "--network=none", "--progress=plain", "--build-arg", "warp_env=mainnet-candidate", "--build-arg", "SOURCE_DATE_EPOCH=" + strconv.FormatInt(epoch, 10), "--provenance=false", "--sbom=false", "--build-context", "offline-ubuntu=oci-layout://" + basePath + "@" + platformDigest, "--metadata-file", "images/" + name + ".metadata.json", "--output", "type=oci,dest=images/" + name + ".oci.tar,rewrite-timestamp=true", "contexts/" + name}
}

// Parse before allocating output, then retain every attempted bounded build.
func executePackageImageConfig(ctx context.Context, path string) error {
	raw, err := readBuildInput(path, 1024*1024)
	if err != nil {
		return err
	}
	var config packageImageConfig
	if err := decodeImageJson(raw, &config, true); err != nil {
		return err
	}
	return executePackageImages(ctx, config, raw)
}

// The parent is immutable. Preparation freezes all local sources before execution.
func executePackageImages(ctx context.Context, config packageImageConfig, rawConfig []byte) error {
	if config.Schema != packageImageSchema {
		return errors.New("unsupported package image schema")
	}
	common := imageBuildConfig{Schema: imageBuildSchema, CandidateManifest: config.CandidateManifest, Output: config.Output, Buildx: config.Buildx, DockerSocket: config.DockerSocket}
	if err := common.validate(); err != nil {
		return err
	}
	if err := physicalImagePath(config.BaseLayout); err != nil {
		return err
	}
	for _, pair := range [][2]string{{config.Output, config.BaseLayout}, {config.BaseLayout, config.Output}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err != nil || relative == "." || filepath.IsLocal(relative) {
			return errors.New("base input must be disjoint from output")
		}
	}
	parent, rawParent, err := readImageParent(config.CandidateManifest)
	if err != nil {
		return err
	}
	parentDirectory := filepath.Dir(config.CandidateManifest.Path)
	if _, err := verifyImageInputs(parentDirectory, parent); err != nil {
		return err
	}
	if err := os.Mkdir(config.Output, 0700); err != nil {
		return fmt.Errorf("fresh package image output required: %w", err)
	}
	for _, dir := range []string{"inputs", "logs", "images", "contexts", "home", "tmp", "docker-config", "buildx-config"} {
		if err := os.MkdirAll(filepath.Join(config.Output, dir), 0700); err != nil {
			return err
		}
	}
	for _, input := range []struct {
		path string
		raw  []byte
	}{{path: "inputs/config.json", raw: rawConfig}, {path: "inputs/parent-manifest.json", raw: rawParent}, {path: "inputs/source-policy.json", raw: []byte(imageSourcePolicy)}} {
		if err := writeBuildBytes(filepath.Join(config.Output, input.path), input.raw); err != nil {
			return err
		}
	}
	lock, rawLock, err := readPackageLock(parentDirectory, parent)
	if err != nil {
		return err
	}
	if err := writeBuildBytes(filepath.Join(config.Output, "inputs/runtime-packages.lock.json"), rawLock); err != nil {
		return err
	}
	if err := verifyLocalPackages(config, lock); err != nil {
		return err
	}
	base, err := readPackageBase(config.BaseLayout, packageBaseDigest)
	if err != nil {
		return err
	}
	basePath := filepath.Join(config.Output, "inputs/base")
	if err := copyPackageBase(config.BaseLayout, basePath, base); err != nil {
		return err
	}
	binaries := map[string]buildArtifact{}
	for _, artifact := range parent.Artifacts {
		if artifact.Kind == "binary" {
			binaries[artifact.Id] = artifact
		}
	}
	recipes := packageRecipeDigests()
	for _, role := range releaseRoles() {
		if !role.Image || role.Id == scratchImageId {
			continue
		}
		name := strings.TrimPrefix(role.Id, "server-")
		packages, err := serviceImagePackages(lock, name)
		if err != nil {
			return err
		}
		original := filepath.Join(parentDirectory, "contexts", name, "Dockerfile")
		raw, err := readBuildInput(original, 64*1024)
		if err != nil {
			return err
		}
		localized, err := localizePackageRecipe(raw, recipes[name], lock.BaseImage, packages, lock.Snapshot)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		target := filepath.Join(config.Output, "contexts", name)
		for _, dir := range []string{"packages", "build/linux/amd64"} {
			if err := os.MkdirAll(filepath.Join(target, dir), 0700); err != nil {
				return err
			}
		}
		if err := writeBuildBytes(filepath.Join(target, "Dockerfile"), localized); err != nil {
			return err
		}
		if err := writeBuildBytes(filepath.Join(config.Output, "inputs", name+".Dockerfile"), raw); err != nil {
			return err
		}
		binary := binaries[role.Id]
		if binary.Bytes <= 0 || binary.Bytes > maximumScratchArchiveBytes {
			return errors.New("package binary outside bound")
		}
		copied, err := copyBuildFile(filepath.Join(parentDirectory, binary.Path), filepath.Join(target, "build/linux/amd64", name), 0755)
		if err != nil || copied.Sha256 != binary.Sha256 || copied.Bytes != binary.Bytes {
			return errors.New("copied package image binary differs")
		}
		for _, p := range packages {
			copied, err := copyBuildFile(config.Packages[p.Id].Path, filepath.Join(target, "packages", p.Id+".deb"), 0600)
			if err != nil || copied.Sha256 != "sha256:"+p.Sha256 || copied.Bytes != p.Size {
				return fmt.Errorf("copied package differs: %s", p.Id)
			}
		}
	}
	environment := imageBuildEnvironment(common)
	if err := writeBuildJson(filepath.Join(config.Output, "inputs/environment.json"), environment); err != nil {
		return err
	}
	// The input inventory is rehashed before each invocation and before sealing.
	frozen, err := packageEvidenceArtifacts(config.Output, []string{"inputs", "contexts"})
	if err != nil {
		return err
	}
	checkFrozen := func() error {
		actual, err := packageEvidenceArtifacts(config.Output, []string{"inputs", "contexts"})
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, frozen) {
			return errors.New("copied package image sources changed")
		}
		return nil
	}
	for _, command := range []struct {
		id   string
		args []string
	}{{id: "buildx-version", args: []string{"version"}}, {id: "builder-inspect", args: []string{"inspect", "default"}}} {
		if err := runBuildCommand(ctx, config.Output, command.id, config.Output, config.Buildx.Path, environment, command.args...); err != nil {
			return err
		}
	}
	receipt := packageImageReceipt{Schema: packageImageSchema, CandidateId: parent.CandidateId, ParentManifestSha256: config.CandidateManifest.Sha256, ParentContentHash: parent.ContentHash, ConfigSha256: digestImageBytes(rawConfig), Base: base, Images: []packageReadback{}, MissingImages: []string{scratchImageId}, Limitations: []string{
		"Only the seven exact reviewed Linux/amd64 package recipes are admitted. The scratch worker is covered by a separate supplement, not merged here.",
		"Offline closure covers fixed local OCI/package/context sources plus remote-source denial and network-none build steps; it does not attest daemon-wide network isolation.",
		"Package installation runs during the build; no application, image load, push or deployment runs. Runtime behavior and production configuration remain unqualified.",
		"SBOM and provenance collectors are disabled; scanner/attestation policy, durable input archive/restore, independent-builder reproduction and arm64 remain open.",
		"The parent composition and all aggregate release, reproducibility and deployment authority remain unchanged."}}
	for _, role := range releaseRoles() {
		if !role.Image || role.Id == scratchImageId {
			continue
		}
		name := strings.TrimPrefix(role.Id, "server-")
		if err := checkFrozen(); err != nil {
			return err
		}
		if err := verifyBuildTool(config.Buildx); err != nil {
			return err
		}
		if err := runBuildCommand(ctx, config.Output, "build-"+name, config.Output, config.Buildx.Path, environment, packageImageArguments(name, parent.SourceDateEpoch, basePath, base.Platform.Digest)...); err != nil {
			return err
		}
		packages, err := serviceImagePackages(lock, name)
		if err != nil {
			return err
		}
		readback, err := inspectPackageImage(filepath.Join(config.Output, "images", name+".oci.tar"), binaries[role.Id], base, packages)
		if err != nil {
			return fmt.Errorf("%s OCI readback: %w", name, err)
		}
		raw, err := readBuildInput(filepath.Join(config.Output, "images", name+".metadata.json"), 1024*1024)
		if err != nil {
			return err
		}
		var metadata struct {
			Digest       string `json:"containerimage.digest"`
			ConfigDigest string `json:"containerimage.config.digest"`
		}
		if err := validateBuildJsonKeys(raw, true); err != nil {
			return err
		}
		if err := decodeImageJson(raw, &metadata, false); err != nil {
			return err
		}
		if metadata.Digest != readback.PlatformDigest || metadata.ConfigDigest != readback.ConfigDigest {
			return errors.New("builder metadata differs from package image readback")
		}
		if err := writeBuildJson(filepath.Join(config.Output, "images", name+".readback.json"), readback); err != nil {
			return err
		}
		receipt.Images = append(receipt.Images, readback)
	}
	if err := checkFrozen(); err != nil {
		return err
	}
	if _, _, err := readImageParent(config.CandidateManifest); err != nil {
		return err
	}
	if _, err := verifyImageInputs(parentDirectory, parent); err != nil {
		return err
	}
	if _, _, err := readPackageLock(parentDirectory, parent); err != nil {
		return err
	}
	if err := verifyLocalPackages(config, lock); err != nil {
		return err
	}
	afterBase, err := readPackageBase(config.BaseLayout, packageBaseDigest)
	if err != nil || !reflect.DeepEqual(afterBase, base) {
		return errors.New("original base input changed")
	}
	if err := verifyBuildTool(config.Buildx); err != nil {
		return err
	}
	receipt.Artifacts, err = packageEvidenceArtifacts(config.Output, []string{"inputs", "contexts", "logs", "images"})
	if err != nil {
		return err
	}
	if err := sealPackageImageReceipt(&receipt); err != nil {
		return err
	}
	return writeBuildJson(filepath.Join(config.Output, "image-receipt.json"), receipt)
}

// Hash only physical regular evidence files, with deterministic ordering.
func packageEvidenceArtifacts(root string, directories []string) ([]buildArtifact, error) {
	artifacts := []buildArtifact{}
	for _, dir := range directories {
		if err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := physicalImagePath(path); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			artifact, err := buildFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			artifact.Id = filepath.ToSlash(relative)
			artifact.Path = artifact.Id
			artifact.Kind = "image-build-evidence"
			artifacts = append(artifacts, artifact)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Id < artifacts[j].Id })
	return artifacts, nil
}

// Validate the exact census and retained joins before the separate content seal.
func sealPackageImageReceipt(receipt *packageImageReceipt) error {
	if receipt.Schema != packageImageSchema || receipt.CandidateId == "" || receipt.SourceToImageVerified || receipt.ReproducibilityVerified || receipt.ReleaseComplete || receipt.DeploymentApproved || !reflect.DeepEqual(receipt.MissingImages, []string{scratchImageId}) || len(receipt.Images) != 7 || receipt.Base.IndexDigest != packageBaseDigest || len(receipt.Base.Layers) != 1 || len(receipt.Base.DiffIds) != 1 {
		return errors.New("package supplement census or authority differs")
	}
	artifacts := map[string]buildArtifact{}
	for _, artifact := range receipt.Artifacts {
		if _, ok := artifacts[artifact.Id]; ok || artifact.Id != artifact.Path || artifact.Kind != "image-build-evidence" || !filepath.IsLocal(artifact.Path) || artifact.Path != filepath.ToSlash(filepath.Clean(artifact.Path)) || !imageDigestPattern.MatchString(artifact.Sha256) || artifact.Bytes <= 0 || artifact.Bytes > maximumBuildFileBytes {
			return errors.New("package receipt artifact differs")
		}
		artifacts[artifact.Id] = artifact
	}
	joins := map[string]string{"inputs/parent-manifest.json": receipt.ParentManifestSha256, "inputs/config.json": receipt.ConfigSha256, "inputs/runtime-packages.lock.json": packageLockDigest, "inputs/source-policy.json": digestImageBytes([]byte(imageSourcePolicy)), "inputs/base/blobs/sha256/" + strings.TrimPrefix(receipt.Base.IndexDigest, "sha256:"): receipt.Base.IndexDigest}
	expected := []string{}
	for _, role := range releaseRoles() {
		if role.Image && role.Id != scratchImageId {
			expected = append(expected, role.Id)
		}
	}
	recipes := packageRecipeDigests()
	for i, image := range receipt.Images {
		name := strings.TrimPrefix(image.Id, "server-")
		if image.Id != expected[i] || image.Platform != "linux/amd64" || image.ContainerBinary != "/usr/local/sbin/bringyour-"+name || !image.SourceToImageVerified || !image.RootfsVerified || image.BinaryBytes <= 0 || image.RootfsEntries <= 0 || len(image.Layers) != 4 || len(image.DiffIds) != 4 || len(image.Packages) == 0 {
			return errors.New("package image result differs")
		}
		for _, digest := range append([]string{image.ArchiveSha256, image.PlatformDigest, image.ConfigDigest, image.BinarySha256, image.RootfsContentHash}, image.DiffIds...) {
			if !imageDigestPattern.MatchString(digest) {
				return errors.New("package image digest differs")
			}
		}
		if !reflect.DeepEqual(image.Layers[0], receipt.Base.Layers[0]) || image.DiffIds[0] != receipt.Base.DiffIds[0] {
			return errors.New("package base layer join differs")
		}
		joins["images/"+name+".oci.tar"] = image.ArchiveSha256
		joins["contexts/"+name+"/build/linux/amd64/"+name] = image.BinarySha256
		joins["inputs/"+name+".Dockerfile"] = recipes[name]
		if artifacts["contexts/"+name+"/build/linux/amd64/"+name].Bytes != image.BinaryBytes {
			return errors.New("package binary receipt length differs")
		}
	}
	for _, descriptor := range append([]packageDescriptor{receipt.Base.Platform, receipt.Base.Config}, receipt.Base.Layers...) {
		joins["inputs/base/blobs/sha256/"+strings.TrimPrefix(descriptor.Digest, "sha256:")] = descriptor.Digest
	}
	for path, digest := range joins {
		if !imageDigestPattern.MatchString(digest) || artifacts[path].Sha256 != digest {
			return fmt.Errorf("package receipt evidence join differs: %s", path)
		}
	}
	if !imageDigestPattern.MatchString(receipt.ParentContentHash) {
		return errors.New("package parent seal differs")
	}
	sort.Slice(receipt.Artifacts, func(i, j int) bool { return receipt.Artifacts[i].Id < receipt.Artifacts[j].Id })
	receipt.ContentHash = ""
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	receipt.ContentHash = digestImageBytes(append([]byte(packageImageSchema+"\x00"), raw...))
	return nil
}
