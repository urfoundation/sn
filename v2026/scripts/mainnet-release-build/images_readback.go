// Verify the single-layer scratch image as data; never extract or execute it.
// Every OCI reference, compressed blob, diff ID and executable byte is checked.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
)

const maximumScratchArchiveBytes int64 = 512 * 1024 * 1024
const maximumScratchMetadataBytes int64 = 1024 * 1024
const ociIndexMediaType = "application/vnd.oci.image.index.v1+json"
const ociManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
const ociConfigMediaType = "application/vnd.oci.image.config.v1+json"
const ociLayerMediaType = "application/vnd.oci.image.layer.v1.tar+gzip"

// The digest refers to the runnable manifest, not an attestation-bearing index.
type scratchReadback struct {
	Id                    string `json:"id"`
	Platform              string `json:"platform"`
	ArchiveSha256         string `json:"archive_sha256"`
	PlatformDigest        string `json:"platform_digest"`
	ConfigDigest          string `json:"config_digest"`
	LayerDigest           string `json:"layer_digest"`
	LayerDiffId           string `json:"layer_diff_id"`
	ContainerBinary       string `json:"container_binary"`
	BinarySha256          string `json:"binary_sha256"`
	BinaryBytes           int64  `json:"binary_bytes"`
	RootfsVerified        bool   `json:"rootfs_verified"`
	SourceToImageVerified bool   `json:"source_to_image_verified"`
}

// Descriptor platform is cross-checked with configuration; all content is local.
type scratchDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Platform  struct {
		Os           string `json:"os"`
		Architecture string `json:"architecture"`
	} `json:"platform"`
}

// Indexes and manifests use disjoint descriptor fields and schema version two.
type scratchManifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	MediaType     string              `json:"mediaType"`
	Manifests     []scratchDescriptor `json:"manifests"`
	Config        scratchDescriptor   `json:"config"`
	Layers        []scratchDescriptor `json:"layers"`
}

// Only small JSON blobs are retained in memory; the layer is streamed twice.
type scratchBlob struct {
	Bytes int64
	Raw   []byte
}

// Only the configuration produced by the reviewed scratch recipe is admitted.
type scratchRuntimeConfig struct {
	User       string   `json:"User"`
	Entrypoint []string `json:"Entrypoint"`
	Cmd        []string `json:"Cmd"`
	Env        []string `json:"Env"`
	WorkingDir string   `json:"WorkingDir"`
}

// This reader deliberately rejects general multi-layer images and attestations.
func inspectScratchImage(path string, binary buildArtifact) (scratchReadback, error) {
	var result scratchReadback
	archive, err := buildFile(path)
	if err != nil {
		return result, err
	}
	if archive.Bytes > maximumScratchArchiveBytes || binary.Bytes <= 0 || binary.Bytes > maximumScratchArchiveBytes-1024*1024 {
		return result, errors.New("scratch image or binary exceeds bound")
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	reader := tar.NewReader(io.LimitReader(file, maximumScratchArchiveBytes+1))
	blobKVs := map[string]scratchBlob{}
	seenKVs := map[string]bool{}
	var indexRaw, layoutRaw []byte
	digestPattern := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for count := 0; ; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
		if count >= 16 || seenKVs[header.Name] || header.Size < 0 || header.Size > maximumScratchArchiveBytes {
			return result, errors.New("duplicate OCI path or archive bound exceeded")
		}
		seenKVs[header.Name] = true
		if header.Typeflag == tar.TypeDir && (header.Name == "blobs/" || header.Name == "blobs/sha256/" || header.Name == "blobs" || header.Name == "blobs/sha256") && header.Size == 0 {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Linkname != "" {
			return result, errors.New("OCI archive requires regular non-link files")
		}
		if header.Name == "index.json" || header.Name == "oci-layout" {
			if header.Size > maximumScratchMetadataBytes {
				return result, errors.New("OCI metadata exceeds bound")
			}
			raw, err := io.ReadAll(reader)
			if err != nil {
				return result, err
			}
			if header.Name == "index.json" {
				indexRaw = raw
			} else {
				layoutRaw = raw
			}
			continue
		}
		digest := "sha256:" + strings.TrimPrefix(header.Name, "blobs/sha256/")
		if !strings.HasPrefix(header.Name, "blobs/sha256/") || !digestPattern.MatchString(digest) {
			return result, errors.New("unexpected OCI archive path")
		}
		hasher := sha256.New()
		var raw bytes.Buffer
		writer := io.Writer(hasher)
		if header.Size <= maximumScratchMetadataBytes {
			writer = io.MultiWriter(hasher, &raw)
		}
		n, err := io.Copy(writer, reader)
		if err != nil || n != header.Size || digest != "sha256:"+hex.EncodeToString(hasher.Sum(nil)) {
			return result, errors.New("OCI blob content hash or length differs")
		}
		blobKVs[digest] = scratchBlob{Bytes: n, Raw: raw.Bytes()}
	}
	var layout struct {
		Version string `json:"imageLayoutVersion"`
	}
	if err := decodeImageJson(layoutRaw, &layout, true); err != nil || layout.Version != "1.0.0" {
		return result, errors.New("OCI layout version differs")
	}
	var index scratchManifest
	if err := decodeImageJson(indexRaw, &index, false); err != nil {
		return result, err
	}
	if index.SchemaVersion != 2 || (index.MediaType != "" && index.MediaType != ociIndexMediaType) || len(index.Manifests) != 1 || len(index.Layers) != 0 || index.Config.Digest != "" {
		return result, errors.New("scratch image needs exactly one OCI platform manifest")
	}
	validate := func(descriptor scratchDescriptor, mediaType string) (scratchBlob, error) {
		blob, ok := blobKVs[descriptor.Digest]
		if !ok || descriptor.MediaType != mediaType || descriptor.Size <= 0 || descriptor.Size != blob.Bytes {
			return blob, errors.New("OCI descriptor type, digest or size differs")
		}
		return blob, nil
	}
	platformDescriptor := index.Manifests[0]
	if platformDescriptor.Platform.Os != "linux" || platformDescriptor.Platform.Architecture != "amd64" {
		return result, errors.New("OCI descriptor platform differs")
	}
	platformBlob, err := validate(platformDescriptor, ociManifestMediaType)
	if err != nil {
		return result, err
	}
	var manifest scratchManifest
	if err := decodeImageJson(platformBlob.Raw, &manifest, false); err != nil {
		return result, err
	}
	if manifest.SchemaVersion != 2 || manifest.MediaType != ociManifestMediaType || len(manifest.Manifests) != 0 || len(manifest.Layers) != 1 || len(blobKVs) != 3 {
		return result, errors.New("scratch image requires one layer and no unreferenced blobs")
	}
	configBlob, err := validate(manifest.Config, ociConfigMediaType)
	if err != nil {
		return result, err
	}
	if _, err := validate(manifest.Layers[0], ociLayerMediaType); err != nil {
		return result, err
	}
	var config struct {
		Os           string          `json:"os"`
		Architecture string          `json:"architecture"`
		Config       json.RawMessage `json:"config"`
		Rootfs       struct {
			Type    string   `json:"type"`
			DiffIds []string `json:"diff_ids"`
		} `json:"rootfs"`
	}
	if err := decodeImageJson(configBlob.Raw, &config, false); err != nil {
		return result, err
	}
	var runtimeConfig scratchRuntimeConfig
	if err := decodeImageJson(config.Config, &runtimeConfig, true); err != nil {
		return result, err
	}
	if config.Os != "linux" || config.Architecture != "amd64" || runtimeConfig.User != "65532:65532" || !reflect.DeepEqual(runtimeConfig.Entrypoint, []string{"/competitionworker"}) || len(runtimeConfig.Cmd) != 0 || !reflect.DeepEqual(runtimeConfig.Env, []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}) || runtimeConfig.WorkingDir != "/" || config.Rootfs.Type != "layers" || len(config.Rootfs.DiffIds) != 1 || !digestPattern.MatchString(config.Rootfs.DiffIds[0]) {
		return result, errors.New("scratch runtime configuration or rootfs identity differs")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	reader = tar.NewReader(io.LimitReader(file, maximumScratchArchiveBytes+1))
	layerName := "blobs/sha256/" + strings.TrimPrefix(manifest.Layers[0].Digest, "sha256:")
	found := false
	for count := 0; count < 16; count++ {
		header, err := reader.Next()
		if err != nil {
			return result, fmt.Errorf("layer unavailable during readback: %w", err)
		}
		if header.Name == layerName {
			if err := verifyScratchLayer(reader, binary, config.Rootfs.DiffIds[0]); err != nil {
				return result, err
			}
			found = true
			break
		}
	}
	if !found {
		return result, errors.New("scratch layer not found")
	}
	after, err := buildFile(path)
	if err != nil || after.Sha256 != archive.Sha256 || after.Bytes != archive.Bytes {
		return result, errors.New("OCI archive changed during readback")
	}
	return scratchReadback{Id: scratchImageId, Platform: "linux/amd64", ArchiveSha256: archive.Sha256, PlatformDigest: platformDescriptor.Digest, ConfigDigest: manifest.Config.Digest, LayerDigest: manifest.Layers[0].Digest, LayerDiffId: config.Rootfs.DiffIds[0], ContainerBinary: "/competitionworker", BinarySha256: binary.Sha256, BinaryBytes: binary.Bytes, RootfsVerified: true, SourceToImageVerified: true}, nil
}

// Exact one-file rootfs excludes whiteouts, shadowing, links and extra payloads.
func verifyScratchLayer(compressed io.Reader, binary buildArtifact, diffId string) error {
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer reader.Close()
	hasher := sha256.New()
	limited := &io.LimitedReader{R: reader, N: binary.Bytes + 1024*1024}
	stream := io.TeeReader(limited, hasher)
	layer := tar.NewReader(stream)
	header, err := layer.Next()
	if err != nil {
		return err
	}
	if header.Name != "competitionworker" || header.Typeflag != tar.TypeReg || header.Linkname != "" || header.Size != binary.Bytes || header.Mode != 0755 || header.Uid != 0 || header.Gid != 0 || len(header.Xattrs) != 0 || len(header.PAXRecords) != 0 {
		return errors.New("scratch binary path, type, size, mode or ownership differs")
	}
	binaryHash := sha256.New()
	n, err := io.Copy(binaryHash, layer)
	if err != nil || n != binary.Bytes || "sha256:"+hex.EncodeToString(binaryHash.Sum(nil)) != binary.Sha256 {
		return errors.New("embedded scratch binary differs from parent composition")
	}
	if _, err := layer.Next(); !errors.Is(err, io.EOF) {
		return errors.New("scratch rootfs contains extra entries or a malformed tail")
	}
	// tar stops at its end marker; drain padding to authenticate the full diff ID
	// and force gzip footer verification while rejecting concealed payloads.
	buffer := make([]byte, 32*1024)
	for {
		n, err := stream.Read(buffer)
		for _, value := range buffer[:n] {
			if value != 0 {
				return errors.New("nonzero data after scratch layer end marker")
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if limited.N == 0 || "sha256:"+hex.EncodeToString(hasher.Sum(nil)) != diffId {
		return errors.New("scratch rootfs diff ID or decompressed bound differs")
	}
	return nil
}
