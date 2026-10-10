// Four-layer synthetic images test complete readback and isolated corruptions.
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

// Mutations are resealed so tests reach the intended inner verification boundary.
type packageTestImage struct {
	Layers        [][]scratchTestEntry
	Runtime       map[string]any
	WrongDiff     bool
	WrongSize     bool
	ExtraBlob     bool
	WrongPlatform bool
}

// Generate a tiny package-bearing image with the same runtime and layer grammar.
func packageTestArchive(t *testing.T, mutate func(*packageTestImage)) (string, buildArtifact, packageBase, []imagePackage) {
	t.Helper()
	file := func(name string, raw []byte, mode int64) scratchTestEntry {
		return scratchTestEntry{Header: tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(raw)), Mode: mode}, Raw: raw}
	}
	executable := []byte("synthetic package service executable")
	expectedBinary := buildArtifact{Id: "server-api", Bytes: int64(len(executable)), Sha256: digestImageBytes(executable)}
	baseEntries := []scratchTestEntry{}
	for _, name := range []string{"etc", "etc/ssl", "etc/ssl/certs", "usr", "usr/bin", "usr/local", "usr/local/sbin", "var", "var/lib", "var/lib/dpkg", "var/log", "var/cache", "var/cache/ldconfig", "root"} {
		baseEntries = append(baseEntries, scratchTestEntry{Header: tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0755}})
	}
	baseEntries = append(baseEntries, file("usr/bin/openssl", []byte("synthetic openssl"), 0755))
	status := []byte("Package: synthetic\nStatus: install ok installed\nArchitecture: all\nVersion: 1.0\nConffiles:\n\n")
	fixture := packageTestImage{Layers: [][]scratchTestEntry{baseEntries, {file("var/lib/dpkg/status", status, 0644), file("etc/ld.so.cache", []byte("synthetic linker cache"), 0644), file("etc/ssl/certs/ca-certificates.crt", []byte("synthetic CA bundle"), 0644)}, {file("usr/local/sbin/bringyour-api", executable, 0755)}, {{Header: tar.Header{Name: "root/.aws", Typeflag: tar.TypeSymlink, Mode: 0777, Linkname: "/srv/warp/vault/.aws"}}}}, Runtime: map[string]any{"Env": []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "WARP_ENV=mainnet-candidate"}, "Cmd": []string{"/usr/local/sbin/bringyour-api", "-p", "80"}, "Labels": map[string]string{"org.opencontainers.image.version": "24.04"}, "StopSignal": "SIGTERM", "ArgsEscaped": true}}
	if mutate != nil {
		mutate(&fixture)
	}
	marshal := func(value any) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	entries := []scratchTestEntry{file("oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`), 0644)}
	blob := func(raw []byte, media string) packageDescriptor {
		digest := digestImageBytes(raw)
		entries = append(entries, file("blobs/sha256/"+strings.TrimPrefix(digest, "sha256:"), raw, 0644))
		return packageDescriptor{MediaType: media, Digest: digest, Size: int64(len(raw))}
	}
	descriptors := []packageDescriptor{}
	diffs := []string{}
	for _, layer := range fixture.Layers {
		raw := scratchTestTar(t, layer)
		diffs = append(diffs, digestImageBytes(raw))
		var compressed bytes.Buffer
		g := gzip.NewWriter(&compressed)
		if _, err := g.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := g.Close(); err != nil {
			t.Fatal(err)
		}
		descriptors = append(descriptors, blob(compressed.Bytes(), ociLayerMediaType))
	}
	base := packageBase{Layers: []packageDescriptor{descriptors[0]}, DiffIds: []string{diffs[0]}}
	if fixture.WrongDiff {
		diffs[2] = digestImageBytes([]byte("wrong binary diff"))
	}
	config := blob(marshal(map[string]any{"os": "linux", "architecture": "amd64", "config": fixture.Runtime, "rootfs": map[string]any{"type": "layers", "diff_ids": diffs}}), ociConfigMediaType)
	if fixture.WrongSize {
		descriptors[2].Size++
	}
	platform := blob(marshal(packageManifest{SchemaVersion: 2, MediaType: ociManifestMediaType, Config: config, Layers: descriptors}), ociManifestMediaType)
	platform.Platform = &packagePlatform{Os: "linux", Architecture: "amd64"}
	if fixture.WrongPlatform {
		platform.Platform.Architecture = "arm64"
	}
	entries = append(entries, file("index.json", marshal(packageManifest{SchemaVersion: 2, MediaType: ociIndexMediaType, Manifests: []packageDescriptor{platform}}), 0644))
	if fixture.ExtraBlob {
		blob([]byte("unreferenced payload"), ociLayerMediaType)
	}
	path := filepath.Join(t.TempDir(), "synthetic.oci.tar")
	if err := os.WriteFile(path, scratchTestTar(t, entries), 0600); err != nil {
		t.Fatal(err)
	}
	return path, expectedBinary, base, []imagePackage{{Id: "synthetic_all", Name: "synthetic", Architecture: "all", Version: "1.0"}}
}

// This root includes the BuildKit ArgsEscaped field and an empty Deb822 field.
func TestPackageImageReadbackBindsBasePackagesAndExecutable(t *testing.T) {
	path, binary, base, packages := packageTestArchive(t, nil)
	result, err := inspectPackageImage(path, binary, base, packages)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RootfsVerified || !result.SourceToImageVerified || result.BinarySha256 != binary.Sha256 || len(result.Layers) != 4 || len(result.Packages) != 1 || result.RootfsEntries == 0 || result.RootfsContentHash == "" {
		t.Fatal("incomplete readback")
	}
}

// Runtime substitutions preserve hashes but may not change the selected command.
func TestPackageImageRejectsRuntimeAndPlatformSubstitution(t *testing.T) {
	for _, mutate := range []func(*packageTestImage){
		func(f *packageTestImage) { f.Runtime["User"] = "65532" },
		func(f *packageTestImage) { f.Runtime["Cmd"] = []string{"/other"} },
		func(f *packageTestImage) { f.Runtime["Entrypoint"] = []string{"/other"} },
		func(f *packageTestImage) { f.Runtime["Env"] = []string{"WARP_ENV=other"} },
		func(f *packageTestImage) { f.Runtime["WorkingDir"] = "/tmp" },
		func(f *packageTestImage) { f.Runtime["ArgsEscaped"] = false },
		func(f *packageTestImage) { f.Runtime["StopSignal"] = "SIGKILL" },
		func(f *packageTestImage) { f.Runtime["Healthcheck"] = map[string]any{"Test": []string{"CMD", "true"}} },
		func(f *packageTestImage) { f.WrongPlatform = true },
	} {
		path, binary, base, packages := packageTestArchive(t, mutate)
		if _, err := inspectPackageImage(path, binary, base, packages); err == nil {
			t.Fatal("accepted runtime substitution")
		}
	}
}

// Blob length, full diff identity and retained base layers are separate checks.
func TestPackageImageRejectsBaseLayerDescriptorAndDiffChanges(t *testing.T) {
	for _, mutate := range []func(*packageTestImage){func(f *packageTestImage) { f.WrongSize = true }, func(f *packageTestImage) { f.WrongDiff = true }, func(f *packageTestImage) { f.ExtraBlob = true }} {
		path, binary, base, packages := packageTestArchive(t, mutate)
		if _, err := inspectPackageImage(path, binary, base, packages); err == nil {
			t.Fatal("accepted bad layer reference")
		}
	}
	path, binary, base, packages := packageTestArchive(t, nil)
	base.Layers[0].Digest = digestImageBytes([]byte("other base"))
	if _, err := inspectPackageImage(path, binary, base, packages); err == nil || !strings.Contains(err.Error(), "base layer") {
		t.Fatalf("accepted other base: %v", err)
	}
}

// Later layer shadowing, ownership and deletion must be checked in the union.
func TestPackageImageRejectsFinalBinaryShadowingAndWhiteouts(t *testing.T) {
	for _, mutate := range []func(*packageTestImage){
		func(f *packageTestImage) { f.Layers[2][0].Raw[0] = 'X' },
		func(f *packageTestImage) { f.Layers[2][0].Header.Uid = 9 },
		func(f *packageTestImage) { f.Layers[2][0].Header.Mode = 0644 },
		func(f *packageTestImage) {
			f.Layers[3] = append(f.Layers[3], scratchTestEntry{Header: tar.Header{Name: "usr/local/sbin/.wh.bringyour-api", Typeflag: tar.TypeReg}})
		},
		func(f *packageTestImage) {
			f.Layers[3] = append(f.Layers[3], scratchTestEntry{Header: tar.Header{Name: "usr/local/sbin/bringyour-api", Typeflag: tar.TypeSymlink, Linkname: "other"}})
		},
		func(f *packageTestImage) { f.Layers[3][0].Header.Linkname = "/other" },
	} {
		path, binary, base, packages := packageTestArchive(t, mutate)
		if _, err := inspectPackageImage(path, binary, base, packages); err == nil {
			t.Fatal("accepted final rootfs substitution")
		}
	}
}

// Successful dpkg output cannot hide missing or mismatched final package status.
func TestPackageImageRejectsInstalledVersionAndRetainedBuildInputs(t *testing.T) {
	path, binary, base, packages := packageTestArchive(t, nil)
	packages[0].Version = "2.0"
	if _, err := inspectPackageImage(path, binary, base, packages); err == nil || !strings.Contains(err.Error(), "installed package") {
		t.Fatalf("accepted wrong package version: %v", err)
	}
	for _, name := range []string{"runtime-packages", "var/log/dpkg.log", "var/cache/ldconfig/aux-cache"} {
		path, binary, base, packages := packageTestArchive(t, func(f *packageTestImage) {
			if name == "runtime-packages" {
				f.Layers[3] = append(f.Layers[3], scratchTestEntry{Header: tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0755}})
			} else {
				f.Layers[3] = append(f.Layers[3], scratchTestEntry{Header: tar.Header{Name: name, Typeflag: tar.TypeReg}})
			}
		})
		if _, err := inspectPackageImage(path, binary, base, packages); err == nil || !strings.Contains(err.Error(), "build-only") {
			t.Fatalf("accepted retained build input %s or failed at wrong boundary: %v", name, err)
		}
	}
}

// Unicode paths use only the two pax keys whose decoded value we verify.
func TestPackageLayerAcceptsUnicodePaxPaths(t *testing.T) {
	raw, diff := packageTestLayer(t, []*tar.Header{{Name: "synthetic-ő", Typeflag: tar.TypeReg, Mode: 0644, Size: 4}, {Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "synthetic-ő"}}, [][]byte{[]byte("data"), nil})
	filesystem := map[string]packageRootEntry{}
	if err := applyPackageLayer(bytes.NewReader(raw), diff, filesystem); err != nil {
		t.Fatal(err)
	}
	if filesystem["synthetic-ő"].Sha256 != digestImageBytes([]byte("data")) || filesystem["alias"].Link != "synthetic-ő" {
		t.Fatal("Unicode path binding differs")
	}
}
