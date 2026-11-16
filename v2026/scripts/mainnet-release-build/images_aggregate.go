// Compose pinned local evidence by replaying its byte and OCI checks. No tool,
// daemon, network, source checkout, application or original receipt is mutated.
package main

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const imageAggregateSchema = "urnetwork-mainnet-image-aggregate-v1"

// External file pins authenticate the original parent and exactly two typed
// supplements. All artifact paths stay relative to their own receipt directory.
type imageAggregateConfig struct {
	Schema            string    `json:"schema"`
	CandidateManifest toolPin   `json:"candidate_manifest"`
	Supplements       []toolPin `json:"supplements"`
	Output            string    `json:"output"`
}

// The original bytes and content seal are both retained; no input is resealed.
type imageAggregateInput struct {
	Schema        string  `json:"schema"`
	File          toolPin `json:"file"`
	ContentHash   string  `json:"content_hash"`
	ArtifactCount int     `json:"artifact_count"`
}

// Each OCI identity joins one original receipt and one parent binary/recipe.
// The retained typed receipt supplies full layer, package and rootfs details.
type imageAggregateImage struct {
	Id                    string        `json:"id"`
	SupplementSha256      string        `json:"supplement_sha256"`
	Binary                buildArtifact `json:"binary"`
	DockerfileSha256      string        `json:"dockerfile_sha256"`
	Archive               buildArtifact `json:"archive"`
	PlatformDigest        string        `json:"platform_digest"`
	ConfigDigest          string        `json:"config_digest"`
	ContainerBinary       string        `json:"container_binary"`
	RootfsVerified        bool          `json:"rootfs_verified"`
	SourceToImageVerified bool          `json:"source_to_image_verified"`
}

// Only local source-to-image coverage advances. A content hash is an integrity
// seal, not a signature, independent reproduction or release authorization.
type imageAggregateReceipt struct {
	Schema                  string                `json:"schema"`
	CandidateId             string                `json:"candidate_id"`
	Platform                string                `json:"platform"`
	ConfigSha256            string                `json:"config_sha256"`
	Parent                  imageAggregateInput   `json:"parent"`
	Repositories            []repositoryPin       `json:"repositories"`
	Supplements             []imageAggregateInput `json:"supplements"`
	Images                  []imageAggregateImage `json:"images"`
	MissingImages           []string              `json:"missing_images"`
	Artifacts               []buildArtifact       `json:"artifacts"`
	SourceToImageVerified   bool                  `json:"source_to_image_verified"`
	ReproducibilityVerified bool                  `json:"reproducibility_verified"`
	ReleaseComplete         bool                  `json:"release_complete"`
	DeploymentApproved      bool                  `json:"deployment_approved"`
	Limitations             []string              `json:"limitations"`
	ContentHash             string                `json:"content_hash"`
}

// One parsed owner keeps schema-specific evidence separate until verification.
type imageAggregateSupplement struct {
	input     imageAggregateInput
	raw       []byte
	artifacts []buildArtifact
	scratch   *imageBuildReceipt
	packages  *packageImageReceipt
}

// Refuse aliases and output overlap before creating even an attempted result.
func (self imageAggregateConfig) validate() error {
	if self.Schema != imageAggregateSchema || len(self.Supplements) != 2 || !filepath.IsAbs(self.Output) || filepath.Clean(self.Output) != self.Output {
		return errors.New("aggregate requires a canonical output and exactly two supplements")
	}
	if err := physicalImagePath(filepath.Dir(self.Output)); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, pin := range append([]toolPin{self.CandidateManifest}, self.Supplements...) {
		if seen[pin.Path] || !imageDigestPattern.MatchString(pin.Sha256) {
			return errors.New("duplicate aggregate input or invalid file pin")
		}
		seen[pin.Path] = true
		if err := physicalImagePath(pin.Path); err != nil {
			return err
		}
		root := filepath.Dir(pin.Path)
		for _, pair := range [][2]string{{root, self.Output}, {self.Output, root}} {
			relative, err := filepath.Rel(pair[0], pair[1])
			if err != nil || relative == "." || filepath.IsLocal(relative) {
				return errors.New("aggregate output must be disjoint from every input directory")
			}
		}
	}
	return nil
}

// Every named artifact, including non-image binaries, contracts and logs, must
// still match. A symlink anywhere in its path cannot redirect the source graph.
func verifyAggregateArtifacts(ctx context.Context, directory string, artifacts []buildArtifact) error {
	if len(artifacts) == 0 || len(artifacts) > 4096 {
		return errors.New("aggregate artifact count outside bound")
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	var total int64
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if artifact.Id == "" || ids[artifact.Id] || paths[artifact.Path] || !filepath.IsLocal(artifact.Path) || filepath.ToSlash(filepath.Clean(artifact.Path)) != artifact.Path || artifact.Bytes <= 0 || artifact.Bytes > maximumBuildFileBytes || !imageDigestPattern.MatchString(artifact.Sha256) {
			return errors.New("aggregate artifact identity differs")
		}
		ids[artifact.Id], paths[artifact.Path] = true, true
		total += artifact.Bytes
		if total > 16*1024*1024*1024 {
			return errors.New("aggregate artifact bytes outside bound")
		}
		path := filepath.Join(directory, artifact.Path)
		if err := physicalImagePath(path); err != nil {
			return fmt.Errorf("artifact %s: %w", artifact.Id, err)
		}
		actual, err := buildFile(path)
		if err != nil {
			return fmt.Errorf("aggregate artifact %s: %w", artifact.Id, err)
		}
		if actual.Sha256 != artifact.Sha256 || actual.Bytes != artifact.Bytes {
			return fmt.Errorf("aggregate artifact bytes differ: %s", artifact.Id)
		}
	}
	return ctx.Err()
}

// Read only evidence already admitted to an owner's artifact inventory.
func readAggregateArtifact(directory, path string, artifacts []buildArtifact, limit int64) ([]byte, error) {
	for _, artifact := range artifacts {
		if artifact.Path == path {
			if err := physicalImagePath(filepath.Join(directory, path)); err != nil {
				return nil, err
			}
			raw, err := readBuildInput(filepath.Join(directory, path), limit)
			if err != nil {
				return nil, err
			}
			if digestImageBytes(raw) != artifact.Sha256 || int64(len(raw)) != artifact.Bytes {
				return nil, fmt.Errorf("aggregate retained input differs: %s", path)
			}
			return raw, nil
		}
	}
	return nil, fmt.Errorf("aggregate retained input missing: %s", path)
}

// Parent source identities must also match the retained config and actual Go
// executable metadata. Neither source paths nor tool paths are executed here.
func verifyAggregateParent(directory string, parent buildManifest) error {
	raw, err := readAggregateArtifact(directory, "inputs/config.json", parent.Artifacts, 1024*1024)
	if err != nil {
		return err
	}
	var config buildConfig
	if err := decodeImageJson(raw, &config, true); err != nil {
		return err
	}
	if err := config.validate(); err != nil {
		return err
	}
	catalog := config.ContractCatalog
	if catalog == "" {
		catalog = "retained"
	}
	if digestImageBytes(raw) != parent.ConfigSha256 || config.CandidateId != parent.CandidateId || config.Version != parent.Version || config.SourceDateEpoch != parent.SourceDateEpoch || catalog != parent.ContractCatalog || !reflect.DeepEqual(config.Repositories, parent.Repositories) {
		return errors.New("aggregate parent source/config binding differs")
	}
	repositories := map[string]repositoryPin{}
	for _, repo := range parent.Repositories {
		repositories[repo.Name] = repo
	}
	for _, role := range parent.Roles {
		for _, artifact := range parent.Artifacts {
			if artifact.Id != role.Id {
				continue
			}
			info, err := buildinfo.ReadFile(filepath.Join(directory, artifact.Path))
			if err != nil {
				return err
			}
			if err := validateBuildInfo(info, role, repositories[role.Repository]); err != nil {
				return err
			}
			if err := validateLinkedBuildModules(info, parent.Modules[role.Repository]); err != nil {
				return err
			}
		}
	}
	_, err = verifyImageInputs(directory, parent)
	return err
}

// File and domain-separated content pins are independent; accepting a new file
// hash cannot legitimize a stale seal or a receipt for another parent.
func readAggregateSupplement(pin toolPin, parentPin toolPin, parent buildManifest) (imageAggregateSupplement, error) {
	var result imageAggregateSupplement
	if err := physicalImagePath(pin.Path); err != nil {
		return result, err
	}
	raw, err := readBuildInput(pin.Path, 16*1024*1024)
	if err != nil {
		return result, err
	}
	if digestImageBytes(raw) != pin.Sha256 {
		return result, errors.New("aggregate supplement file pin differs")
	}
	var identity struct {
		Schema string `json:"schema"`
	}
	if err := decodeImageJson(raw, &identity, false); err != nil {
		return result, err
	}
	var candidate, parentHash, parentContent, content string
	switch identity.Schema {
	case imageBuildSchema:
		var receipt imageBuildReceipt
		if err := decodeImageJson(raw, &receipt, true); err != nil {
			return result, err
		}
		content = receipt.ContentHash
		if err := sealImageReceipt(&receipt); err != nil {
			return result, err
		}
		if content != receipt.ContentHash {
			return result, errors.New("aggregate scratch content seal differs")
		}
		candidate, parentHash, parentContent = receipt.CandidateId, receipt.ParentManifestSha256, receipt.ParentContentHash
		result.scratch, result.artifacts = &receipt, receipt.Artifacts
	case packageImageSchema:
		var receipt packageImageReceipt
		if err := decodeImageJson(raw, &receipt, true); err != nil {
			return result, err
		}
		content = receipt.ContentHash
		if err := sealPackageImageReceipt(&receipt); err != nil {
			return result, err
		}
		if content != receipt.ContentHash {
			return result, errors.New("aggregate package content seal differs")
		}
		candidate, parentHash, parentContent = receipt.CandidateId, receipt.ParentManifestSha256, receipt.ParentContentHash
		result.packages, result.artifacts = &receipt, receipt.Artifacts
	default:
		return result, errors.New("unsupported aggregate supplement schema")
	}
	if candidate != parent.CandidateId || parentHash != parentPin.Sha256 || parentContent != parent.ContentHash {
		return result, errors.New("aggregate supplement belongs to another parent")
	}
	result.input = imageAggregateInput{Schema: identity.Schema, File: pin, ContentHash: content, ArtifactCount: len(result.artifacts)}
	result.raw = raw
	return result, nil
}

// Fixed command and metadata joins are rechecked as data, never as authority to
// run a historical executable or contact a historical Docker socket.
func verifyAggregateImageEvidence(directory, name string, artifacts []buildArtifact, config imageBuildConfig, arguments []string, platformDigest, configDigest string) error {
	read := func(path string) ([]byte, error) { return readAggregateArtifact(directory, path, artifacts, 1024*1024) }
	raw, err := read("logs/build-" + name + ".command.json")
	if err != nil {
		return err
	}
	var command struct {
		Directory string   `json:"directory"`
		Tool      string   `json:"tool"`
		Arguments []string `json:"arguments"`
	}
	if err := decodeImageJson(raw, &command, true); err != nil {
		return err
	}
	if command.Directory != config.Output || command.Tool != config.Buildx.Path || !reflect.DeepEqual(command.Arguments, arguments) {
		return errors.New("aggregate image build command differs")
	}
	raw, err = read("logs/build-" + name + ".exit")
	if err != nil {
		return err
	}
	if string(raw) != "0\n" {
		return errors.New("aggregate image build did not succeed")
	}
	raw, err = read("images/" + name + ".metadata.json")
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
	if metadata.Digest != platformDigest || metadata.ConfigDigest != configDigest {
		return errors.New("aggregate builder metadata differs from OCI readback")
	}
	raw, err = read("inputs/environment.json")
	if err != nil {
		return err
	}
	var environment []string
	if err := decodeImageJson(raw, &environment, true); err != nil {
		return err
	}
	if !reflect.DeepEqual(environment, imageBuildEnvironment(config)) {
		return errors.New("aggregate build environment differs")
	}
	return nil
}

// Replay scratch verification against the parent's binary rather than trusting
// the receipt's stored true flags, hash labels, or another candidate's image.
func verifyAggregateScratch(directory string, receipt imageBuildReceipt, binary buildArtifact) (scratchReadback, error) {
	actual, err := inspectScratchImage(filepath.Join(directory, "images/competitionworker.oci.tar"), binary)
	if err == nil && !reflect.DeepEqual(actual, receipt.Image) {
		err = errors.New("aggregate scratch OCI readback differs from receipt")
	}
	return actual, err
}

// Replayed package readback verifies the complete rootfs and every layer using
// the authenticated retained base and exact parent's package lock and binary.
func verifyAggregatePackage(directory string, receipt packageReadback, binary buildArtifact, base packageBase, packages []imagePackage) (packageReadback, error) {
	name := strings.TrimPrefix(binary.Id, "server-")
	actual, err := inspectPackageImage(filepath.Join(directory, "images", name+".oci.tar"), binary, base, packages)
	if err == nil && !reflect.DeepEqual(actual, receipt) {
		err = errors.New("aggregate package OCI readback differs from receipt")
	}
	return actual, err
}

// Typed readbacks become one exact union only after original inputs, localized
// recipes, package payloads, runtime config and source binary joins are replayed.
func verifyAggregateSupplement(ctx context.Context, supplement imageAggregateSupplement, parent buildManifest, lock imagePackageLock, parentDirectory string) ([]imageAggregateImage, error) {
	directory := filepath.Dir(supplement.input.File.Path)
	read := func(path string, limit int64) ([]byte, error) {
		return readAggregateArtifact(directory, path, supplement.artifacts, limit)
	}
	raw, err := read("inputs/config.json", 1024*1024)
	if err != nil {
		return nil, err
	}
	var config imageBuildConfig
	var base packageBase
	var parentHash string
	if supplement.scratch != nil {
		parentHash = supplement.scratch.ParentManifestSha256
		if err := decodeImageJson(raw, &config, true); err != nil {
			return nil, err
		}
		if config.Schema != imageBuildSchema || digestImageBytes(raw) != supplement.scratch.ConfigSha256 {
			return nil, errors.New("aggregate scratch config differs")
		}
	} else {
		parentHash = supplement.packages.ParentManifestSha256
		var packageConfig packageImageConfig
		if err := decodeImageJson(raw, &packageConfig, true); err != nil {
			return nil, err
		}
		if packageConfig.Schema != packageImageSchema || digestImageBytes(raw) != supplement.packages.ConfigSha256 {
			return nil, errors.New("aggregate package config differs")
		}
		config = imageBuildConfig{Schema: imageBuildSchema, CandidateManifest: packageConfig.CandidateManifest, Output: packageConfig.Output, Buildx: packageConfig.Buildx, DockerSocket: packageConfig.DockerSocket}
		base, err = readPackageBase(filepath.Join(directory, "inputs/base"), packageBaseDigest)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(base, supplement.packages.Base) {
			return nil, errors.New("aggregate retained base differs from receipt")
		}
	}
	if config.CandidateManifest.Sha256 != parentHash {
		return nil, errors.New("aggregate supplement config parent differs")
	}
	images := []imageAggregateImage{}
	for _, image := range parent.Images {
		if (image.Id == scratchImageId) != (supplement.scratch != nil) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(image.Id, "server-")
		var binary, archive buildArtifact
		for _, artifact := range parent.Artifacts {
			if artifact.Id == image.Id {
				binary = artifact
			}
		}
		for _, artifact := range supplement.artifacts {
			if artifact.Path == "images/"+name+".oci.tar" {
				archive = artifact
			}
		}
		result := imageAggregateImage{Id: image.Id, SupplementSha256: supplement.input.File.Sha256, Binary: binary, DockerfileSha256: image.DockerfileSha256, Archive: archive, ContainerBinary: image.ContainerBinary}
		arguments := scratchImageArguments(parent.SourceDateEpoch)
		if supplement.scratch != nil {
			actual, err := verifyAggregateScratch(directory, *supplement.scratch, binary)
			if err != nil {
				return nil, err
			}
			result.PlatformDigest, result.ConfigDigest = actual.PlatformDigest, actual.ConfigDigest
		} else {
			packages, err := serviceImagePackages(lock, name)
			if err != nil {
				return nil, err
			}
			original, err := readAggregateArtifact(parentDirectory, "contexts/"+name+"/Dockerfile", parent.Artifacts, 64*1024)
			if err != nil {
				return nil, err
			}
			localized, err := localizePackageRecipe(original, packageRecipeDigests()[name], lock.BaseImage, packages, lock.Snapshot)
			if err != nil {
				return nil, err
			}
			raw, err := read("contexts/"+name+"/Dockerfile", 64*1024)
			if err != nil {
				return nil, err
			}
			if string(raw) != string(localized) {
				return nil, errors.New("aggregate localized recipe differs")
			}
			for _, p := range packages {
				path := "contexts/" + name + "/packages/" + p.Id + ".deb"
				found := false
				for _, artifact := range supplement.artifacts {
					if artifact.Path == path && artifact.Sha256 == "sha256:"+p.Sha256 && artifact.Bytes == p.Size {
						found = true
					}
				}
				if !found {
					return nil, fmt.Errorf("aggregate package payload differs: %s", p.Id)
				}
			}
			var recorded packageReadback
			for _, candidate := range supplement.packages.Images {
				if candidate.Id == image.Id {
					recorded = candidate
				}
			}
			actual, err := verifyAggregatePackage(directory, recorded, binary, base, packages)
			if err != nil {
				return nil, err
			}
			result.PlatformDigest, result.ConfigDigest = actual.PlatformDigest, actual.ConfigDigest
			arguments = packageImageArguments(name, parent.SourceDateEpoch, filepath.Join(config.Output, "inputs/base"), base.Platform.Digest)
			raw, err = read("images/"+name+".readback.json", 1024*1024)
			if err != nil {
				return nil, err
			}
			var retained packageReadback
			if err := decodeImageJson(raw, &retained, true); err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(retained, actual) {
				return nil, errors.New("aggregate retained package readback differs")
			}
		}
		if err := verifyAggregateImageEvidence(directory, name, supplement.artifacts, config, arguments, result.PlatformDigest, result.ConfigDigest); err != nil {
			return nil, err
		}
		result.RootfsVerified, result.SourceToImageVerified = true, true
		images = append(images, result)
	}
	return images, nil
}

// Exact census and explicit false authority are mandatory even at seal time.
func sealImageAggregate(receipt *imageAggregateReceipt) error {
	if receipt.Schema != imageAggregateSchema || receipt.CandidateId == "" || receipt.Platform != "linux/amd64" || !receipt.SourceToImageVerified || receipt.ReproducibilityVerified || receipt.ReleaseComplete || receipt.DeploymentApproved || len(receipt.MissingImages) != 0 || len(receipt.Images) != 8 || len(receipt.Supplements) != 2 {
		return errors.New("aggregate census or authority differs")
	}
	schemas := map[string]string{}
	for _, input := range receipt.Supplements {
		if input.Schema != imageBuildSchema && input.Schema != packageImageSchema || schemas[input.Schema] != "" || !imageDigestPattern.MatchString(input.File.Sha256) || !imageDigestPattern.MatchString(input.ContentHash) {
			return errors.New("aggregate duplicate or invalid supplement")
		}
		schemas[input.Schema] = input.File.Sha256
	}
	wanted := map[string]bool{}
	for _, role := range releaseRoles() {
		if role.Image {
			wanted[role.Id] = true
		}
	}
	for _, image := range receipt.Images {
		schema := packageImageSchema
		name := strings.TrimPrefix(image.Id, "server-")
		containerBinary := "/usr/local/sbin/bringyour-" + name
		if image.Id == scratchImageId {
			schema = imageBuildSchema
			containerBinary = "/competitionworker"
		}
		if !wanted[image.Id] || image.SupplementSha256 != schemas[schema] || image.Binary.Id != image.Id || image.Binary.Kind != "binary" || image.Binary.Bytes <= 0 || image.Archive.Bytes <= 0 || !image.RootfsVerified || !image.SourceToImageVerified {
			return errors.New("aggregate image union or source binding differs")
		}
		if image.Binary.Path != "binaries/"+image.Id || image.Archive.Path != "images/"+name+".oci.tar" || image.Archive.Id != image.Archive.Path || image.Archive.Kind != "image-build-evidence" || image.ContainerBinary != containerBinary {
			return errors.New("aggregate image path binding differs")
		}
		delete(wanted, image.Id)
		for _, digest := range []string{image.Binary.Sha256, image.DockerfileSha256, image.Archive.Sha256, image.PlatformDigest, image.ConfigDigest} {
			if !imageDigestPattern.MatchString(digest) {
				return errors.New("aggregate image digest differs")
			}
		}
	}
	if receipt.Parent.Schema != buildSchema || !imageDigestPattern.MatchString(receipt.Parent.File.Sha256) || !imageDigestPattern.MatchString(receipt.Parent.ContentHash) || !imageDigestPattern.MatchString(receipt.ConfigSha256) {
		return errors.New("aggregate parent/config pin differs")
	}
	joins := map[string]string{"inputs/config.json": receipt.ConfigSha256, "inputs/parent-manifest.json": receipt.Parent.File.Sha256, "inputs/package-image-receipt.json": schemas[packageImageSchema], "inputs/scratch-image-receipt.json": schemas[imageBuildSchema]}
	if len(receipt.Artifacts) != len(joins) {
		return errors.New("aggregate metadata artifact census differs")
	}
	for _, artifact := range receipt.Artifacts {
		if artifact.Id != artifact.Path || artifact.Kind != "image-build-evidence" || artifact.Bytes <= 0 || artifact.Bytes > 16*1024*1024 || joins[artifact.Path] != artifact.Sha256 {
			return errors.New("aggregate retained metadata binding differs")
		}
		delete(joins, artifact.Path)
	}
	sort.Slice(receipt.Artifacts, func(i, j int) bool { return receipt.Artifacts[i].Id < receipt.Artifacts[j].Id })
	sort.Slice(receipt.Supplements, func(i, j int) bool { return receipt.Supplements[i].Schema < receipt.Supplements[j].Schema })
	sort.Slice(receipt.Images, func(i, j int) bool { return receipt.Images[i].Id < receipt.Images[j].Id })
	receipt.ContentHash = ""
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	receipt.ContentHash = digestImageBytes(append([]byte(imageAggregateSchema+"\x00"), raw...))
	return nil
}

// Strict config admission has no executable or network capability to configure.
func executeImageAggregateConfig(ctx context.Context, path string) error {
	raw, err := readBuildInput(path, 1024*1024)
	if err != nil {
		return err
	}
	var config imageAggregateConfig
	if err := decodeImageJson(raw, &config, true); err != nil {
		return err
	}
	return executeImageAggregate(ctx, config, raw)
}

// A fresh disjoint output retains pinned metadata; failed checks cannot emit a
// sealed aggregate. The final fence rehashes every original artifact and receipt.
func executeImageAggregate(ctx context.Context, config imageAggregateConfig, rawConfig []byte) error {
	if err := config.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Mkdir(config.Output, 0700); err != nil {
		return fmt.Errorf("fresh aggregate output required: %w", err)
	}
	if err := os.Mkdir(filepath.Join(config.Output, "inputs"), 0700); err != nil {
		return err
	}
	if err := writeBuildBytes(filepath.Join(config.Output, "inputs/config.json"), rawConfig); err != nil {
		return err
	}
	parent, rawParent, err := readImageParent(config.CandidateManifest)
	if err != nil {
		return err
	}
	parentDirectory := filepath.Dir(config.CandidateManifest.Path)
	if err := verifyAggregateArtifacts(ctx, parentDirectory, parent.Artifacts); err != nil {
		return err
	}
	if err := verifyAggregateParent(parentDirectory, parent); err != nil {
		return err
	}
	lock, _, err := readPackageLock(parentDirectory, parent)
	if err != nil {
		return err
	}
	receipt := imageAggregateReceipt{Schema: imageAggregateSchema, CandidateId: parent.CandidateId, Platform: parent.Platform, ConfigSha256: digestImageBytes(rawConfig), Parent: imageAggregateInput{Schema: parent.Schema, File: config.CandidateManifest, ContentHash: parent.ContentHash, ArtifactCount: len(parent.Artifacts)}, Repositories: parent.Repositories, MissingImages: []string{}, SourceToImageVerified: true, Limitations: []string{
		"Only the complete local Linux/amd64 source-to-image linkage is verified by rehashing pinned evidence and replaying OCI/rootfs readback.",
		"The original parent and supplements retain their original claims; this separate integrity seal is not a signature or deployment authorization.",
		"Independent builder reproduction, arm64, runtime behavior, production configuration, durable archive/restore, SBOM, scanner and attestation policy remain unqualified.",
	}}
	if err := writeBuildBytes(filepath.Join(config.Output, "inputs/parent-manifest.json"), rawParent); err != nil {
		return err
	}
	supplements := []imageAggregateSupplement{}
	schemas := map[string]bool{}
	for _, pin := range config.Supplements {
		supplement, err := readAggregateSupplement(pin, config.CandidateManifest, parent)
		if err != nil {
			return err
		}
		if schemas[supplement.input.Schema] {
			return errors.New("duplicate aggregate supplement schema")
		}
		schemas[supplement.input.Schema] = true
		if err := verifyAggregateArtifacts(ctx, filepath.Dir(pin.Path), supplement.artifacts); err != nil {
			return err
		}
		images, err := verifyAggregateSupplement(ctx, supplement, parent, lock, parentDirectory)
		if err != nil {
			return err
		}
		receipt.Images = append(receipt.Images, images...)
		receipt.Supplements = append(receipt.Supplements, supplement.input)
		supplements = append(supplements, supplement)
		name := "package-image-receipt.json"
		if supplement.scratch != nil {
			name = "scratch-image-receipt.json"
		}
		if err := writeBuildBytes(filepath.Join(config.Output, "inputs", name), supplement.raw); err != nil {
			return err
		}
	}
	if _, _, err := readImageParent(config.CandidateManifest); err != nil {
		return err
	}
	if err := verifyAggregateArtifacts(ctx, parentDirectory, parent.Artifacts); err != nil {
		return err
	}
	for _, supplement := range supplements {
		if _, err := readAggregateSupplement(supplement.input.File, config.CandidateManifest, parent); err != nil {
			return err
		}
		if err := verifyAggregateArtifacts(ctx, filepath.Dir(supplement.input.File.Path), supplement.artifacts); err != nil {
			return err
		}
	}
	receipt.Artifacts, err = packageEvidenceArtifacts(config.Output, []string{"inputs"})
	if err != nil {
		return err
	}
	if err := sealImageAggregate(&receipt); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeBuildJson(filepath.Join(config.Output, "image-aggregate.json"), receipt)
}
