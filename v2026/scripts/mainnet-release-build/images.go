// Prepare every selected image from exact binary and source recipe bytes.
// OCI construction uses a separate bounded scratch supplement; the prepared
// context manifest itself never claims image verification or reproducibility.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type buildImage struct {
	Id                    string   `json:"id"`
	ContextPath           string   `json:"context_path"`
	BinarySha256          string   `json:"binary_sha256"`
	DockerfileSha256      string   `json:"dockerfile_sha256"`
	ContainerBinary       string   `json:"container_binary"`
	BuildArguments        []string `json:"build_arguments"`
	PlatformDigest        string   `json:"platform_digest"`
	SourceToImageVerified bool     `json:"source_to_image_verified"`
}

// Copy with exact declared permissions and an independent output hash. Creation
// remains private; the descriptor fixes the final mode independently of umask.
func copyBuildFile(source, target string, mode os.FileMode) (buildArtifact, error) {
	expected, err := buildFile(source)
	if err != nil {
		return buildArtifact{}, err
	}
	input, err := os.Open(source)
	if err != nil {
		return buildArtifact{}, err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return buildArtifact{}, err
	}
	n, copyErr := io.Copy(output, io.LimitReader(input, maximumBuildFileBytes+1))
	if copyErr == nil {
		copyErr = output.Chmod(mode)
	}
	if err := errors.Join(copyErr, output.Sync(), output.Close()); err != nil {
		return buildArtifact{}, err
	}
	actual, err := buildFile(target)
	if err != nil {
		return buildArtifact{}, err
	}
	after, err := buildFile(source)
	if err != nil {
		return buildArtifact{}, err
	}
	if n != expected.Bytes || actual.Sha256 != expected.Sha256 || after.Sha256 != expected.Sha256 {
		return buildArtifact{}, errors.New("release input changed during copy")
	}
	return actual, nil
}

// All eight contexts retain the production Dockerfile verbatim, and every
// proposed local OCI build requests epoch rewriting, including scratch worker.
func prepareBuildImages(config buildConfig, binaries map[string]buildArtifact) ([]buildImage, []buildArtifact, error) {
	images := []buildImage{}
	artifacts := []buildArtifact{}
	for _, role := range releaseRoles() {
		if !role.Image {
			continue
		}
		name := strings.TrimPrefix(role.Id, "server-")
		relative := filepath.Join("contexts", name)
		contextPath := filepath.Join(config.Output, relative)
		if err := os.MkdirAll(filepath.Join(contextPath, "build/linux/amd64"), 0700); err != nil {
			return nil, nil, err
		}
		binary, err := copyBuildFile(filepath.Join(config.Output, binaries[role.Id].Path), filepath.Join(contextPath, "build/linux/amd64", name), 0755)
		if err != nil {
			return nil, nil, err
		}
		binary.Id, binary.Kind, binary.Path = "image-"+name+"-binary", "image-input", filepath.ToSlash(filepath.Join(relative, "build/linux/amd64", name))
		artifacts = append(artifacts, binary)
		recipe, err := copyBuildFile(filepath.Join(config.Workspace, "server/cli", name, "Dockerfile"), filepath.Join(contextPath, "Dockerfile"), 0600)
		if err != nil {
			return nil, nil, err
		}
		recipe.Id, recipe.Kind, recipe.Path = "image-"+name+"-dockerfile", "image-input", filepath.ToSlash(filepath.Join(relative, "Dockerfile"))
		artifacts = append(artifacts, recipe)
		containerBinary := "/usr/local/sbin/bringyour-" + name
		if name == "competitionworker" {
			containerBinary = "/competitionworker"
		}
		images = append(images, buildImage{Id: role.Id, ContextPath: filepath.ToSlash(relative), BinarySha256: binary.Sha256, DockerfileSha256: recipe.Sha256, ContainerBinary: containerBinary, BuildArguments: []string{"buildx", "build", "--no-cache", "--platform", "linux/amd64", "--network=none", "--build-arg", "warp_env=mainnet-candidate", "--build-arg", "SOURCE_DATE_EPOCH=" + strconv.FormatInt(config.SourceDateEpoch, 10), "--provenance=mode=max", "--sbom=true", "--metadata-file", filepath.Join("images", name+".metadata.json"), "--output", "type=oci,dest=" + filepath.Join("images", name+".oci.tar") + ",rewrite-timestamp=true", filepath.ToSlash(relative)}})
	}
	return images, artifacts, nil
}

// Contexts are evidence of selected inputs, never substitutes for built images.
func validateImageCensus(images []buildImage, missing []string, artifacts map[string]buildArtifact) error {
	wanted := map[string]bool{}
	expectedMissing := []string{}
	for _, role := range releaseRoles() {
		if role.Image {
			wanted[role.Id] = true
			expectedMissing = append(expectedMissing, role.Id)
		}
	}
	if len(images) != len(wanted) || !reflect.DeepEqual(missing, expectedMissing) {
		return errors.New("image census or unresolved-image list differs")
	}
	for _, image := range images {
		if !wanted[image.Id] {
			return errors.New("duplicate or unexpected image context")
		}
		delete(wanted, image.Id)
		name := strings.TrimPrefix(image.Id, "server-")
		binary, binaryOk := artifacts[image.Id]
		contextBinary, contextOk := artifacts["image-"+name+"-binary"]
		recipe, recipeOk := artifacts["image-"+name+"-dockerfile"]
		if !binaryOk || !contextOk || !recipeOk || binary.Sha256 != image.BinarySha256 || contextBinary.Sha256 != image.BinarySha256 || recipe.Sha256 != image.DockerfileSha256 {
			return fmt.Errorf("%s context is not bound to the selected binary/recipe", image.Id)
		}
		if image.PlatformDigest != "" || image.SourceToImageVerified {
			return errors.New("prepared context cannot claim an OCI image")
		}
	}
	return nil
}
