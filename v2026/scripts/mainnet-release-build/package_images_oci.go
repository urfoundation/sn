// Authenticate the pinned local base and bounded multi-layer image archives.
// Rootfs inspection streams data into a logical filesystem, never onto the host.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

const maximumPackageArchiveBytes int64 = 1024 * 1024 * 1024

var imageDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Embedded or external descriptor content cannot expand this local source graph.
type packageDescriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	Platform     *packagePlatform  `json:"platform,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Urls         []string          `json:"urls,omitempty"`
	Data         string            `json:"data,omitempty"`
	ArtifactType string            `json:"artifactType,omitempty"`
}

// The selected target has neither variants nor optional operating-system features.
type packagePlatform struct {
	Os           string   `json:"os"`
	Architecture string   `json:"architecture"`
	Variant      string   `json:"variant,omitempty"`
	OsVersion    string   `json:"os.version,omitempty"`
	OsFeatures   []string `json:"os.features,omitempty"`
}

// The same bounded descriptor grammar covers the index and its single manifest.
type packageManifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	MediaType     string              `json:"mediaType"`
	Manifests     []packageDescriptor `json:"manifests,omitempty"`
	Config        packageDescriptor   `json:"config"`
	Layers        []packageDescriptor `json:"layers,omitempty"`
	Annotations   map[string]string   `json:"annotations,omitempty"`
	Subject       *packageDescriptor  `json:"subject,omitempty"`
	ArtifactType  string              `json:"artifactType,omitempty"`
}

// Base membership is authenticated by the original multi-platform index digest.
type packageBase struct {
	IndexDigest string              `json:"index_digest"`
	Platform    packageDescriptor   `json:"platform"`
	Config      packageDescriptor   `json:"config"`
	Layers      []packageDescriptor `json:"layers"`
	DiffIds     []string            `json:"diff_ids"`
}

// Configuration fields outside the reviewed runtime contract fail closed.
type packageRuntime struct {
	User        string            `json:"User,omitempty"`
	Env         []string          `json:"Env"`
	Cmd         []string          `json:"Cmd"`
	Entrypoint  []string          `json:"Entrypoint,omitempty"`
	WorkingDir  string            `json:"WorkingDir,omitempty"`
	Labels      map[string]string `json:"Labels"`
	StopSignal  string            `json:"StopSignal,omitempty"`
	ArgsEscaped bool              `json:"ArgsEscaped,omitempty"`
}

// Historical base config contains extra Docker bookkeeping, read separately.
type packageImageIdentity struct {
	Os           string          `json:"os"`
	Architecture string          `json:"architecture"`
	Variant      string          `json:"variant"`
	Config       json.RawMessage `json:"config"`
	Rootfs       struct {
		Type    string   `json:"type"`
		DiffIds []string `json:"diff_ids"`
	} `json:"rootfs"`
}

// The final data-only proof binds every layer and the complete logical rootfs.
type packageReadback struct {
	Id                    string              `json:"id"`
	Platform              string              `json:"platform"`
	ArchiveSha256         string              `json:"archive_sha256"`
	PlatformDigest        string              `json:"platform_digest"`
	ConfigDigest          string              `json:"config_digest"`
	Layers                []packageDescriptor `json:"layers"`
	DiffIds               []string            `json:"diff_ids"`
	ContainerBinary       string              `json:"container_binary"`
	BinarySha256          string              `json:"binary_sha256"`
	BinaryBytes           int64               `json:"binary_bytes"`
	Packages              []imagePackage      `json:"packages"`
	RootfsEntries         int                 `json:"rootfs_entries"`
	RootfsContentHash     string              `json:"rootfs_content_hash"`
	RootfsVerified        bool                `json:"rootfs_verified"`
	SourceToImageVerified bool                `json:"source_to_image_verified"`
}

// Descriptor references are never followed to URLs or decoded inline content.
func validatePackageDescriptor(descriptor packageDescriptor, mediaType string) error {
	if descriptor.MediaType != mediaType || !imageDigestPattern.MatchString(descriptor.Digest) || descriptor.Size <= 0 || descriptor.Size > maximumPackageArchiveBytes || len(descriptor.Urls) != 0 || descriptor.Data != "" || descriptor.ArtifactType != "" {
		return errors.New("local OCI descriptor identity differs")
	}
	return nil
}

// A base directory may contain other platforms, but only this authenticated chain
// is copied to output or provided to the builder. No missing blob is downloaded.
func readPackageBase(directory, indexDigest string) (packageBase, error) {
	var result packageBase
	if !imageDigestPattern.MatchString(indexDigest) {
		return result, errors.New("base index digest differs")
	}
	read := func(digest string, size int64) ([]byte, error) {
		path := filepath.Join(directory, "blobs/sha256", strings.TrimPrefix(digest, "sha256:"))
		if !imageDigestPattern.MatchString(digest) {
			return nil, errors.New("base blob digest differs")
		}
		if err := physicalImagePath(path); err != nil {
			return nil, err
		}
		file, err := buildFile(path)
		if err != nil {
			return nil, err
		}
		if file.Sha256 != digest || size > 0 && file.Bytes != size {
			return nil, errors.New("base blob hash or length differs")
		}
		if file.Bytes > maximumPackageArchiveBytes {
			return nil, errors.New("base blob exceeds bound")
		}
		if file.Bytes > maximumScratchMetadataBytes {
			return nil, nil
		}
		return readBuildInput(path, maximumScratchMetadataBytes)
	}
	raw, err := read(indexDigest, 0)
	if err != nil {
		return result, err
	}
	var index packageManifest
	if err := decodeImageJson(raw, &index, true); err != nil {
		return result, err
	}
	if index.SchemaVersion != 2 || index.MediaType != ociIndexMediaType || len(index.Manifests) == 0 || len(index.Manifests) > 32 || len(index.Layers) != 0 || index.Config.Digest != "" || index.Subject != nil || index.ArtifactType != "" {
		return result, errors.New("base index shape differs")
	}
	matches := 0
	for _, descriptor := range index.Manifests {
		if descriptor.Platform != nil && descriptor.Platform.Os == "linux" && descriptor.Platform.Architecture == "amd64" {
			if !reflect.DeepEqual(descriptor.Platform, &packagePlatform{Os: "linux", Architecture: "amd64"}) {
				return result, errors.New("base target variant differs")
			}
			if err := validatePackageDescriptor(descriptor, ociManifestMediaType); err != nil {
				return result, err
			}
			result.Platform = descriptor
			matches++
		}
	}
	if matches != 1 {
		return result, errors.New("base needs exactly one amd64 descriptor")
	}
	raw, err = read(result.Platform.Digest, result.Platform.Size)
	if err != nil {
		return result, err
	}
	var manifest packageManifest
	if err := decodeImageJson(raw, &manifest, true); err != nil {
		return result, err
	}
	if manifest.SchemaVersion != 2 || manifest.MediaType != ociManifestMediaType || len(manifest.Manifests) != 0 || len(manifest.Layers) != 1 || manifest.Subject != nil || manifest.ArtifactType != "" {
		return result, errors.New("reviewed base requires one layer")
	}
	if err := validatePackageDescriptor(manifest.Config, ociConfigMediaType); err != nil {
		return result, err
	}
	raw, err = read(manifest.Config.Digest, manifest.Config.Size)
	if err != nil {
		return result, err
	}
	var identity packageImageIdentity
	if err := validateBuildJsonKeys(raw, true); err != nil {
		return result, err
	}
	if err := decodeImageJson(raw, &identity, false); err != nil {
		return result, err
	}
	if identity.Os != "linux" || identity.Architecture != "amd64" || identity.Variant != "" || identity.Rootfs.Type != "layers" || len(identity.Rootfs.DiffIds) != 1 || !imageDigestPattern.MatchString(identity.Rootfs.DiffIds[0]) {
		return result, errors.New("base platform or diff ID differs")
	}
	var runtime struct {
		OnBuild []string `json:"OnBuild"`
	}
	if err := decodeImageJson(identity.Config, &runtime, false); err != nil || len(runtime.OnBuild) != 0 {
		return result, errors.New("base build triggers forbidden")
	}
	for _, layer := range manifest.Layers {
		if err := validatePackageDescriptor(layer, ociLayerMediaType); err != nil {
			return result, err
		}
		if _, err := read(layer.Digest, layer.Size); err != nil {
			return result, err
		}
	}
	result.IndexDigest = indexDigest
	result.Config = manifest.Config
	result.Layers = manifest.Layers
	result.DiffIds = identity.Rootfs.DiffIds
	return result, nil
}

// The layout passed to buildx contains exactly the reviewed selected chain.
func copyPackageBase(source, target string, base packageBase) error {
	if err := os.MkdirAll(filepath.Join(target, "blobs/sha256"), 0700); err != nil {
		return err
	}
	descriptors := append([]packageDescriptor{{Digest: base.IndexDigest}, base.Platform, base.Config}, base.Layers...)
	for _, descriptor := range descriptors {
		relative := filepath.Join("blobs/sha256", strings.TrimPrefix(descriptor.Digest, "sha256:"))
		actual, err := copyBuildFile(filepath.Join(source, relative), filepath.Join(target, relative), 0600)
		if err != nil || actual.Sha256 != descriptor.Digest || descriptor.Size > 0 && actual.Bytes != descriptor.Size {
			return errors.New("copied base differs")
		}
	}
	if err := writeBuildJson(filepath.Join(target, "index.json"), packageManifest{SchemaVersion: 2, MediaType: ociIndexMediaType, Manifests: []packageDescriptor{base.Platform}}); err != nil {
		return err
	}
	return writeBuildBytes(filepath.Join(target, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`))
}

// Layer content remains on disk, with authenticated offsets for bounded rereads.
type packageBlob struct {
	Bytes  int64
	Offset int64
	Raw    []byte
}

// Scan the outer tar once; reject ambiguous paths, types, hashes and hidden tails.
func readPackageArchive(file *os.File) (map[string]packageBlob, []byte, error) {
	blobs := map[string]packageBlob{}
	seen := map[string]bool{}
	var indexRaw, layoutRaw []byte
	limited := &io.LimitedReader{R: file, N: maximumPackageArchiveBytes + 1}
	reader := tar.NewReader(limited)
	for count := 0; ; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if count >= 64 || seen[header.Name] || header.Size < 0 || header.Size > maximumPackageArchiveBytes || len(header.PAXRecords) != 0 || len(header.Xattrs) != 0 {
			return nil, nil, errors.New("OCI archive duplicate path or bound")
		}
		seen[header.Name] = true
		if header.Typeflag == tar.TypeDir && (header.Name == "blobs/" || header.Name == "blobs/sha256/") && header.Size == 0 {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Linkname != "" {
			return nil, nil, errors.New("OCI archive requires regular files")
		}
		if header.Name == "index.json" || header.Name == "oci-layout" {
			if header.Size > maximumScratchMetadataBytes {
				return nil, nil, errors.New("OCI metadata bound")
			}
			raw, err := io.ReadAll(reader)
			if err != nil {
				return nil, nil, err
			}
			if header.Name == "index.json" {
				indexRaw = raw
			} else {
				layoutRaw = raw
			}
			continue
		}
		digest := "sha256:" + strings.TrimPrefix(header.Name, "blobs/sha256/")
		if !strings.HasPrefix(header.Name, "blobs/sha256/") || !imageDigestPattern.MatchString(digest) {
			return nil, nil, errors.New("unexpected OCI path")
		}
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, nil, err
		}
		hash := sha256.New()
		var raw bytes.Buffer
		writer := io.Writer(hash)
		if header.Size <= maximumScratchMetadataBytes {
			writer = io.MultiWriter(hash, &raw)
		}
		n, err := io.Copy(writer, reader)
		if err != nil || n != header.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != digest {
			return nil, nil, errors.New("OCI blob hash or length differs")
		}
		blobs[digest] = packageBlob{Bytes: n, Offset: offset, Raw: raw.Bytes()}
	}
	if err := drainImagePadding(limited); err != nil {
		return nil, nil, err
	}
	if limited.N == 0 {
		return nil, nil, errors.New("OCI archive exceeds bound")
	}
	var layout struct {
		Version string `json:"imageLayoutVersion"`
	}
	if err := decodeImageJson(layoutRaw, &layout, true); err != nil || layout.Version != "1.0.0" {
		return nil, nil, errors.New("OCI layout differs")
	}
	return blobs, indexRaw, nil
}

// Tar end markers may be followed only by zero padding, within the stream bound.
func drainImagePadding(reader io.Reader) error {
	buffer := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buffer)
		for _, value := range buffer[:n] {
			if value != 0 {
				return errors.New("nonzero data after tar end marker")
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Authenticate config, all layers and final filesystem before declaring a match.
func inspectPackageImage(path string, binary buildArtifact, base packageBase, packages []imagePackage) (packageReadback, error) {
	var result packageReadback
	archive, err := buildFile(path)
	if err != nil {
		return result, err
	}
	if archive.Bytes > maximumPackageArchiveBytes || binary.Bytes <= 0 || binary.Bytes > maximumScratchArchiveBytes {
		return result, errors.New("package image size bound")
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	blobs, indexRaw, err := readPackageArchive(file)
	if err != nil {
		return result, err
	}
	validate := func(descriptor packageDescriptor, mediaType string) (packageBlob, error) {
		if err := validatePackageDescriptor(descriptor, mediaType); err != nil {
			return packageBlob{}, err
		}
		blob, ok := blobs[descriptor.Digest]
		if !ok || blob.Bytes != descriptor.Size {
			return blob, errors.New("OCI descriptor size or blob differs")
		}
		return blob, nil
	}
	var index packageManifest
	if err := decodeImageJson(indexRaw, &index, true); err != nil {
		return result, err
	}
	if index.SchemaVersion != 2 || index.MediaType != ociIndexMediaType || len(index.Manifests) != 1 || len(index.Layers) != 0 || index.Config.Digest != "" || index.Subject != nil || index.ArtifactType != "" {
		return result, errors.New("one package image platform required")
	}
	platform := index.Manifests[0]
	if !reflect.DeepEqual(platform.Platform, &packagePlatform{Os: "linux", Architecture: "amd64"}) {
		return result, errors.New("package descriptor platform differs")
	}
	platformBlob, err := validate(platform, ociManifestMediaType)
	if err != nil {
		return result, err
	}
	var manifest packageManifest
	if err := decodeImageJson(platformBlob.Raw, &manifest, true); err != nil {
		return result, err
	}
	if manifest.SchemaVersion != 2 || manifest.MediaType != ociManifestMediaType || len(manifest.Manifests) != 0 || len(manifest.Layers) != 4 || len(blobs) != 6 || manifest.Subject != nil || manifest.ArtifactType != "" {
		return result, errors.New("package image requires base, installation, binary and symlink layers only")
	}
	configBlob, err := validate(manifest.Config, ociConfigMediaType)
	if err != nil {
		return result, err
	}
	var identity packageImageIdentity
	if err := validateBuildJsonKeys(configBlob.Raw, true); err != nil {
		return result, err
	}
	if err := decodeImageJson(configBlob.Raw, &identity, false); err != nil {
		return result, err
	}
	var runtime packageRuntime
	if err := decodeImageJson(identity.Config, &runtime, true); err != nil {
		return result, err
	}
	name := strings.TrimPrefix(binary.Id, "server-")
	if _, ok := packageRecipeDigests()[name]; !ok {
		return result, errors.New("unknown package image role")
	}
	cmd := []string{"/usr/local/sbin/bringyour-" + name}
	if name != "proxy" {
		cmd = append(cmd, "-p", "80")
	}
	expected := packageRuntime{Env: []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "WARP_ENV=mainnet-candidate"}, Cmd: cmd, Labels: map[string]string{"org.opencontainers.image.version": "24.04"}, StopSignal: "SIGTERM", ArgsEscaped: true}
	if identity.Os != "linux" || identity.Architecture != "amd64" || identity.Variant != "" || !reflect.DeepEqual(runtime, expected) || identity.Rootfs.Type != "layers" || len(identity.Rootfs.DiffIds) != 4 {
		return result, errors.New("package runtime configuration differs")
	}
	if len(base.Layers) != 1 || len(base.DiffIds) != 1 || !reflect.DeepEqual(manifest.Layers[0], base.Layers[0]) || identity.Rootfs.DiffIds[0] != base.DiffIds[0] {
		return result, errors.New("package image base layer differs")
	}
	filesystem := map[string]packageRootEntry{}
	for i, descriptor := range manifest.Layers {
		blob, err := validate(descriptor, ociLayerMediaType)
		if err != nil {
			return result, err
		}
		if !imageDigestPattern.MatchString(identity.Rootfs.DiffIds[i]) {
			return result, errors.New("package diff ID differs")
		}
		if err := applyPackageLayer(io.NewSectionReader(file, blob.Offset, blob.Bytes), identity.Rootfs.DiffIds[i], filesystem); err != nil {
			return result, fmt.Errorf("layer %d: %w", i, err)
		}
	}
	installed, err := verifyPackageRootfs(filesystem, binary, packages)
	if err != nil {
		return result, err
	}
	rootHash, err := packageRootHash(filesystem)
	if err != nil {
		return result, err
	}
	after, err := buildFile(path)
	if err != nil || after.Sha256 != archive.Sha256 || after.Bytes != archive.Bytes {
		return result, errors.New("package archive changed during readback")
	}
	return packageReadback{Id: binary.Id, Platform: "linux/amd64", ArchiveSha256: archive.Sha256, PlatformDigest: platform.Digest, ConfigDigest: manifest.Config.Digest, Layers: manifest.Layers, DiffIds: identity.Rootfs.DiffIds, ContainerBinary: cmd[0], BinarySha256: binary.Sha256, BinaryBytes: binary.Bytes, Packages: installed, RootfsEntries: len(filesystem), RootfsContentHash: rootHash, RootfsVerified: true, SourceToImageVerified: true}, nil
}
