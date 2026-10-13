// Synthetic OCI archives force failures without Docker, network or scheduling.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit headers expose rootfs path, ownership and replacement attacks.
type scratchTestEntry struct {
	Header tar.Header
	Raw    []byte
}

// A small independent archive producer can corrupt one boundary at a time.
type scratchTestFixture struct {
	Entries         []scratchTestEntry
	Config          map[string]any
	DiffId          string
	DescriptorDelta int64
	DescriptorArch  string
	DuplicatePath   bool
	CorruptBlob     bool
	MissingBlob     bool
	LayerTail       []byte
}

// Archive/tar writes test-owned bytes only; no filesystem extraction is used.
func scratchTestTar(t *testing.T, entries []scratchTestEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		if err := writer.WriteHeader(&entry.Header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.Raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// Digests are regenerated after intentional semantic changes, avoiding accidental
// reliance on a generic outer hash mismatch to reject a malformed inner image.
func scratchTestArchive(t *testing.T, mutate func(*scratchTestFixture)) (string, buildArtifact) {
	t.Helper()
	binary := []byte("synthetic executable bytes for offline image verification\n")
	fixture := scratchTestFixture{
		Entries:        []scratchTestEntry{{Header: tar.Header{Name: "competitionworker", Typeflag: tar.TypeReg, Size: int64(len(binary)), Mode: 0755}, Raw: binary}},
		Config:         map[string]any{"architecture": "amd64", "os": "linux", "config": map[string]any{"User": "65532:65532", "Entrypoint": []string{"/competitionworker"}, "Env": []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}, "WorkingDir": "/"}},
		DescriptorArch: "amd64",
	}
	if mutate != nil {
		mutate(&fixture)
	}
	layerRaw := append(scratchTestTar(t, fixture.Entries), fixture.LayerTail...)
	var compressed bytes.Buffer
	compressor := gzip.NewWriter(&compressed)
	if _, err := compressor.Write(layerRaw); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	if fixture.DiffId == "" {
		fixture.DiffId = buildFixtureDigest(string(layerRaw))
	}
	fixture.Config["rootfs"] = map[string]any{"type": "layers", "diff_ids": []string{fixture.DiffId}}
	marshal := func(value any) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	descriptor := func(raw []byte, mediaType string) map[string]any {
		return map[string]any{"mediaType": mediaType, "digest": buildFixtureDigest(string(raw)), "size": len(raw)}
	}
	configRaw := marshal(fixture.Config)
	manifestRaw := marshal(map[string]any{"schemaVersion": 2, "mediaType": ociManifestMediaType, "config": descriptor(configRaw, ociConfigMediaType), "layers": []any{descriptor(compressed.Bytes(), ociLayerMediaType)}})
	platform := descriptor(manifestRaw, ociManifestMediaType)
	platform["size"] = int64(len(manifestRaw)) + fixture.DescriptorDelta
	platform["platform"] = map[string]string{"architecture": fixture.DescriptorArch, "os": "linux"}
	indexRaw := marshal(map[string]any{"schemaVersion": 2, "mediaType": ociIndexMediaType, "manifests": []any{platform}})
	fileEntry := func(name string, raw []byte) scratchTestEntry {
		return scratchTestEntry{Header: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(raw))}, Raw: raw}
	}
	entries := []scratchTestEntry{fileEntry("oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`)), fileEntry("index.json", indexRaw)}
	for _, raw := range [][]byte{manifestRaw, configRaw, compressed.Bytes()} {
		entries = append(entries, fileEntry("blobs/sha256/"+strings.TrimPrefix(buildFixtureDigest(string(raw)), "sha256:"), raw))
	}
	if fixture.CorruptBlob {
		entries[4].Raw = bytes.Repeat([]byte("x"), len(entries[4].Raw))
	}
	if fixture.MissingBlob {
		entries = entries[:4]
	}
	if fixture.DuplicatePath {
		entries = append(entries, entries[1])
	}
	path := filepath.Join(t.TempDir(), "synthetic.oci.tar")
	if err := os.WriteFile(path, scratchTestTar(t, entries), 0600); err != nil {
		t.Fatal(err)
	}
	return path, buildArtifact{Id: scratchImageId, Sha256: buildFixtureDigest(string(binary)), Bytes: int64(len(binary))}
}

// The positive root proves the complete independent hash and rootfs path works.
func TestScratchImageReadbackBindsPlatformAndExecutable(t *testing.T) {
	path, binary := scratchTestArchive(t, nil)
	readback, err := inspectScratchImage(path, binary)
	if err != nil {
		t.Fatal(err)
	}
	if readback.BinarySha256 != binary.Sha256 || readback.BinaryBytes != binary.Bytes || !readback.RootfsVerified || !readback.SourceToImageVerified || readback.PlatformDigest == "" || readback.Platform != "linux/amd64" {
		t.Fatalf("readback lost exact binding: %+v", readback)
	}
}

// A valid OCI archive can still contain the wrong executable.
func TestScratchImageRejectsEmbeddedBinarySubstitution(t *testing.T) {
	path, binary := scratchTestArchive(t, nil)
	binary.Sha256 = buildFixtureDigest("other synthetic binary")
	if _, err := inspectScratchImage(path, binary); err == nil || !strings.Contains(err.Error(), "embedded scratch binary") {
		t.Fatalf("accepted substituted binary or failed at wrong boundary: %v", err)
	}
}

// Both OCI layers' compressed identity and their complete unpacked identity bind.
func TestScratchImageRejectsWrongLayerDiffId(t *testing.T) {
	path, binary := scratchTestArchive(t, func(f *scratchTestFixture) { f.DiffId = buildFixtureDigest("different rootfs") })
	if _, err := inspectScratchImage(path, binary); err == nil || !strings.Contains(err.Error(), "diff ID") {
		t.Fatalf("accepted wrong unpacked identity: %v", err)
	}
}

// The platform in an index is insufficient when its referenced config disagrees.
func TestScratchImageRejectsPlatformMismatchInEitherLocation(t *testing.T) {
	for _, location := range []string{"descriptor", "config"} {
		path, binary := scratchTestArchive(t, func(f *scratchTestFixture) {
			if location == "descriptor" {
				f.DescriptorArch = "arm64"
			} else {
				f.Config["architecture"] = "arm64"
			}
		})
		if _, err := inspectScratchImage(path, binary); err == nil {
			t.Fatalf("accepted %s architecture mismatch", location)
		}
	}
}

// Preserve the selected nonroot identity and actual entrypoint, not only bytes.
func TestScratchImageRejectsRuntimeConfigSubstitution(t *testing.T) {
	for _, field := range []string{"User", "Entrypoint", "Cmd", "Env", "WorkingDir", "Volumes"} {
		path, binary := scratchTestArchive(t, func(f *scratchTestFixture) {
			config := f.Config["config"].(map[string]any)
			if field == "User" || field == "WorkingDir" {
				config[field] = "0"
			} else {
				config[field] = []string{"/other-synthetic-command"}
			}
		})
		if _, err := inspectScratchImage(path, binary); err == nil {
			t.Fatalf("accepted runtime %s substitution", field)
		}
	}
}

// Fail closed for rootfs union/alias behavior rather than extracting on the host.
func TestScratchImageRejectsExtraEntriesAndShadowing(t *testing.T) {
	for _, name := range []string{"competitionworker", ".wh.competitionworker", "unselected-file"} {
		path, binary := scratchTestArchive(t, func(f *scratchTestFixture) {
			entry := f.Entries[0]
			entry.Header.Name = name
			f.Entries = append(f.Entries, entry)
		})
		if _, err := inspectScratchImage(path, binary); err == nil || !strings.Contains(err.Error(), "extra entries") {
			t.Fatalf("accepted extra/shadowing %q: %v", name, err)
		}
	}
}

// The exact rootfs path, file type, ownership and executable mode all matter.
func TestScratchImageRejectsUnsafeBinaryHeaders(t *testing.T) {
	for _, change := range []string{"path", "mode", "owner", "symlink", "hardlink", "pax"} {
		path, binary := scratchTestArchive(t, func(f *scratchTestFixture) {
			header := &f.Entries[0].Header
			switch change {
			case "path":
				header.Name = "../competitionworker"
			case "mode":
				header.Mode = 04755
			case "owner":
				header.Uid = 123
			case "symlink", "hardlink":
				header.Typeflag = tar.TypeSymlink
				if change == "hardlink" {
					header.Typeflag = tar.TypeLink
				}
				header.Linkname = "/synthetic-target"
				header.Size = 0
				f.Entries[0].Raw = nil
			case "pax":
				header.PAXRecords = map[string]string{"synthetic.attribute": "unexpected"}
			}
		})
		if _, err := inspectScratchImage(path, binary); err == nil || !strings.Contains(err.Error(), "ownership differs") {
			t.Fatalf("accepted unsafe %s header: %v", change, err)
		}
	}
}

// A valid gzip tail and rehashed diff ID cannot hide an additional tar payload.
func TestScratchImageRejectsPayloadAfterLayerEnd(t *testing.T) {
	path, binary := scratchTestArchive(t, func(f *scratchTestFixture) { f.LayerTail = []byte("hidden payload") })
	if _, err := inspectScratchImage(path, binary); err == nil || !strings.Contains(err.Error(), "nonzero data") {
		t.Fatalf("accepted concealed layer tail: %v", err)
	}
}

// The decompression limit is enforced during reading, before unbounded expansion.
func TestScratchImageBoundsExpandedLayer(t *testing.T) {
	path, binary := scratchTestArchive(t, func(f *scratchTestFixture) { f.LayerTail = make([]byte, 1024*1024) })
	if _, err := inspectScratchImage(path, binary); err == nil || !strings.Contains(err.Error(), "bound differs") {
		t.Fatalf("accepted oversized expanded layer: %v", err)
	}
}

// Duplicate paths, missing blobs and sizes are distinct from a content hash check.
func TestScratchImageRejectsArchiveReferenceCorruption(t *testing.T) {
	for _, change := range []string{"duplicate", "missing", "size", "hash"} {
		path, binary := scratchTestArchive(t, func(f *scratchTestFixture) {
			switch change {
			case "duplicate":
				f.DuplicatePath = true
			case "missing":
				f.MissingBlob = true
			case "size":
				f.DescriptorDelta = 1
			case "hash":
				f.CorruptBlob = true
			}
		})
		if _, err := inspectScratchImage(path, binary); err == nil {
			t.Fatalf("accepted %s OCI corruption", change)
		}
	}
}
