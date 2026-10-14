// Deterministic source-closure controls use synthetic packages and filesystem data.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Preflight failures are durable too, and an existing attempt is never retried.
func TestPackageBuildMissingClosureRetainsAttempt(t *testing.T) {
	pin, _ := imageTestParent(t, scratchImageDockerfile)
	root := t.TempDir()
	socket := filepath.Join(root, "daemon.sock")
	listener, err := net.Listen("unix", socket)
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
	config := packageImageConfig{Schema: packageImageSchema, CandidateManifest: pin, Output: filepath.Join(root, "attempt"), Buildx: toolPin{Path: tool, Sha256: artifact.Sha256}, DockerSocket: socket, BaseLayout: t.TempDir(), Packages: map[string]toolPin{}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := executePackageImages(context.Background(), config, raw); err == nil {
		t.Fatal("built with missing closure")
	}
	if retained, err := os.ReadFile(filepath.Join(config.Output, "inputs/config.json")); err != nil || string(retained) != string(raw) {
		t.Fatal("failed attempt config lost")
	}
	if _, err := os.Stat(filepath.Join(config.Output, "image-receipt.json")); !os.IsNotExist(err) {
		t.Fatal("failure emitted a receipt")
	}
	if err := executePackageImages(context.Background(), config, raw); err == nil || !strings.Contains(err.Error(), "fresh package image output required") {
		t.Fatalf("retry overwrote attempt: %v", err)
	}
}

// A declared tar payload can exceed limits even when compression is tiny.
func TestPackageLayerRejectsOversizedEntryBeforeReadingPayload(t *testing.T) {
	var uncompressed bytes.Buffer
	writer := tar.NewWriter(&uncompressed)
	if err := writer.WriteHeader(&tar.Header{Name: "oversized", Typeflag: tar.TypeReg, Mode: 0644, Size: maximumPackageLayerBytes + 1}); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	g := gzip.NewWriter(&compressed)
	if _, err := g.Write(uncompressed.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if err := applyPackageLayer(bytes.NewReader(compressed.Bytes()), digestImageBytes(uncompressed.Bytes()), map[string]packageRootEntry{}); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatalf("accepted oversize payload: %v", err)
	}
}

// Empty and falsely promoted receipts must return errors without indexing panic.
func TestPackageReceiptRejectsIncompleteCensusAndAuthority(t *testing.T) {
	for _, receipt := range []packageImageReceipt{{}, {Schema: packageImageSchema, CandidateId: "synthetic", Images: make([]packageReadback, 7), MissingImages: []string{scratchImageId}, Base: packageBase{IndexDigest: packageBaseDigest}}, {Schema: packageImageSchema, DeploymentApproved: true}, {Schema: packageImageSchema, ReleaseComplete: true}, {Schema: packageImageSchema, SourceToImageVerified: true}, {Schema: packageImageSchema, ReproducibilityVerified: true}} {
		if err := sealPackageImageReceipt(&receipt); err == nil {
			t.Fatal("accepted incomplete or promoted receipt")
		}
	}
}

// A complete synthetic receipt allows each authority guard to be tested alone.
func TestPackageReceiptPreservesSevenOnlyScope(t *testing.T) {
	path, binary, base, packages := packageTestArchive(t, nil)
	readback, err := inspectPackageImage(path, binary, base, packages)
	if err != nil {
		t.Fatal(err)
	}
	base.IndexDigest = packageBaseDigest
	base.Platform = packageDescriptor{MediaType: ociManifestMediaType, Digest: digestImageBytes([]byte("base manifest")), Size: 10}
	base.Config = packageDescriptor{MediaType: ociConfigMediaType, Digest: digestImageBytes([]byte("base config")), Size: 10}
	receipt := packageImageReceipt{Schema: packageImageSchema, CandidateId: "synthetic", ParentManifestSha256: digestImageBytes([]byte("parent")), ParentContentHash: digestImageBytes([]byte("parent seal")), ConfigSha256: digestImageBytes([]byte("config")), Base: base, MissingImages: []string{scratchImageId}}
	artifacts := map[string]string{"inputs/parent-manifest.json": receipt.ParentManifestSha256, "inputs/config.json": receipt.ConfigSha256, "inputs/runtime-packages.lock.json": packageLockDigest, "inputs/source-policy.json": digestImageBytes([]byte(imageSourcePolicy)), "inputs/base/blobs/sha256/" + strings.TrimPrefix(base.IndexDigest, "sha256:"): base.IndexDigest}
	for _, d := range append([]packageDescriptor{base.Platform, base.Config}, base.Layers...) {
		artifacts["inputs/base/blobs/sha256/"+strings.TrimPrefix(d.Digest, "sha256:")] = d.Digest
	}
	for _, role := range releaseRoles() {
		if role.Image && role.Id != scratchImageId {
			name := strings.TrimPrefix(role.Id, "server-")
			image := readback
			image.Id = role.Id
			image.ContainerBinary = "/usr/local/sbin/bringyour-" + name
			receipt.Images = append(receipt.Images, image)
			artifacts["images/"+name+".oci.tar"] = image.ArchiveSha256
			artifacts["contexts/"+name+"/build/linux/amd64/"+name] = image.BinarySha256
			artifacts["inputs/"+name+".Dockerfile"] = packageRecipeDigests()[name]
		}
	}
	for path, digest := range artifacts {
		size := int64(10)
		if strings.Contains(path, "/build/linux/amd64/") {
			size = binary.Bytes
		}
		receipt.Artifacts = append(receipt.Artifacts, buildArtifact{Id: path, Path: path, Kind: "image-build-evidence", Sha256: digest, Bytes: size})
	}
	if err := sealPackageImageReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*packageImageReceipt){
		func(r *packageImageReceipt) { r.SourceToImageVerified = true }, func(r *packageImageReceipt) { r.ReproducibilityVerified = true }, func(r *packageImageReceipt) { r.ReleaseComplete = true }, func(r *packageImageReceipt) { r.DeploymentApproved = true }, func(r *packageImageReceipt) { r.MissingImages = nil }, func(r *packageImageReceipt) { r.Images[1] = r.Images[0] }, func(r *packageImageReceipt) { r.Images[0].BinarySha256 = digestImageBytes([]byte("substituted")) }, func(r *packageImageReceipt) { r.Images[0].BinaryBytes++ }, func(r *packageImageReceipt) { r.Base.DiffIds[0] = digestImageBytes([]byte("different base root")) },
	} {
		var changed packageImageReceipt
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		mutate(&changed)
		if err := sealPackageImageReceipt(&changed); err == nil {
			t.Fatal("accepted isolated receipt substitution")
		}
	}
}

// Source substitution must preserve every installation and runtime byte.
func TestPackageRecipeLocalizesOnlyPinnedSources(t *testing.T) {
	p := imagePackage{Id: "sample_all", Name: "sample", Architecture: "all", Filename: "pool/sample.deb", Sha256: strings.Repeat("1", 64)}
	base := "base.example/runtime@sha256:" + strings.Repeat("2", 64)
	raw := []byte("FROM " + base + " AS runtime-packages\nADD --checksum=sha256:" + p.Sha256 + " https://packages.example/" + p.Filename + " /runtime-packages/all/sample.deb\nFROM " + base + "\nRUN --network=none echo synthetic\nCMD [\"synthetic\"]\n")
	actual, err := localizePackageRecipe(raw, digestImageBytes(raw), base, []imagePackage{p}, "https://packages.example/")
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.ReplaceAll(string(raw), base, "offline-ubuntu")
	expected = strings.Replace(expected, "ADD --checksum=sha256:"+p.Sha256+" https://packages.example/"+p.Filename, "COPY --chmod=0600 packages/sample_all.deb", 1)
	if string(actual) != expected {
		t.Fatal("changed installation or runtime instructions")
	}
}

// A parent pin cannot widen the independently reviewed recipe allowlist.
func TestPackageRecipeRejectsExpandedGraph(t *testing.T) {
	raw := []byte("FROM synthetic.example/base AS runtime-packages\nFROM synthetic.example/base\nRUN --network=none echo unreviewed-command\n")
	if _, err := localizePackageRecipe(raw, packageRecipeDigests()["api"], "synthetic.example/base", nil, ""); err == nil {
		t.Fatal("accepted unreviewed recipe")
	}
	for _, raw := range []string{"FROM expected\nFROM expected\nADD https://payload.example/a /a\n", "FROM expected\nFROM changed\n", "FROM expected\n"} {
		if _, err := localizePackageRecipe([]byte(raw), digestImageBytes([]byte(raw)), "expected", nil, ""); err == nil {
			t.Fatalf("accepted unclosed source %q", raw)
		}
	}
}

// Exact local hashes and sizes are mandatory; missing inputs have no fallback.
func TestPackageLocalInputsRejectMissingChangedAndAliasedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.deb")
	raw := []byte("synthetic package")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	p := imagePackage{Id: "sample_all", Sha256: strings.TrimPrefix(digestImageBytes(raw), "sha256:"), Size: int64(len(raw))}
	lock := imagePackageLock{Packages: []imagePackage{p}}
	config := packageImageConfig{Packages: map[string]toolPin{p.Id: {Path: path, Sha256: "sha256:" + p.Sha256}}}
	if err := verifyLocalPackages(config, lock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed package!!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyLocalPackages(config, lock); err == nil {
		t.Fatal("accepted changed package")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := verifyLocalPackages(config, lock); err == nil {
		t.Fatal("accepted missing package")
	}
	other := filepath.Join(root, "other.deb")
	if err := os.WriteFile(other, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if err := verifyLocalPackages(config, lock); err == nil {
		t.Fatal("accepted aliased package")
	}
	delete(config.Packages, p.Id)
	if err := verifyLocalPackages(config, lock); err == nil {
		t.Fatal("accepted incomplete package census")
	}
}

// Offline arguments cannot be widened by the caller or ambient environment.
func TestPackageImageArgumentsBoundSourcesAndAuthority(t *testing.T) {
	args := strings.Join(packageImageArguments("api", 7, "/synthetic/base", digestImageBytes([]byte("base"))), " ")
	for _, required := range []string{"--network=none", "--no-cache", "--provenance=false", "--sbom=false", "warp_env=mainnet-candidate", "SOURCE_DATE_EPOCH=7", "offline-ubuntu=oci-layout:///synthetic/base@sha256:", "rewrite-timestamp=true", "--platform linux/amd64"} {
		if !strings.Contains(args, required) {
			t.Fatalf("missing %s", required)
		}
	}
	for _, forbidden := range []string{"--push", "--load", "--tag", "--allow", "--secret", "--ssh", "--pull", "docker-image://"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("unexpected %s", forbidden)
		}
	}
}

// Layer fixtures retain exact gzip and full uncompressed diff identities.
func packageTestLayer(t *testing.T, headers []*tar.Header, payloads [][]byte) ([]byte, string) {
	t.Helper()
	var uncompressed bytes.Buffer
	writer := tar.NewWriter(&uncompressed)
	for i, header := range headers {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if len(payloads) > i && len(payloads[i]) > 0 {
			if _, err := writer.Write(payloads[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	g := gzip.NewWriter(&compressed)
	if _, err := g.Write(uncompressed.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes(), digestImageBytes(uncompressed.Bytes())
}

// A valid whiteout hides a lower file; new same-layer files remain visible.
func TestPackageLayerWhiteoutAndOpaqueSemantics(t *testing.T) {
	for _, opaque := range []bool{false, true} {
		filesystem := map[string]packageRootEntry{"dir": {Kind: tar.TypeDir}, "dir/lower": {Kind: tar.TypeReg}, "dir/keep": {Kind: tar.TypeReg}}
		marker := "dir/.wh.lower"
		if opaque {
			marker = "dir/.wh..wh..opq"
		}
		raw, diff := packageTestLayer(t, []*tar.Header{{Name: "dir/new", Typeflag: tar.TypeReg, Mode: 0644, Size: 3}, {Name: marker, Typeflag: tar.TypeReg, Mode: 0600}}, [][]byte{[]byte("new"), nil})
		if err := applyPackageLayer(bytes.NewReader(raw), diff, filesystem); err != nil {
			t.Fatal(err)
		}
		if _, ok := filesystem["dir/lower"]; ok {
			t.Fatal("lower file survived whiteout")
		}
		if filesystem["dir/new"].Sha256 != digestImageBytes([]byte("new")) {
			t.Fatal("new file lost to whiteout")
		}
		_, kept := filesystem["dir/keep"]
		if kept == opaque {
			t.Fatal("opaque directory semantics differ")
		}
	}
}

// Link parents and path aliases are refused before a layer can change the map.
func TestPackageLayerRejectsShadowTraversalAndDuplicates(t *testing.T) {
	cases := [][]*tar.Header{
		{{Name: "../escape", Typeflag: tar.TypeReg}},
		{{Name: "dir/../escape", Typeflag: tar.TypeReg}},
		{{Name: "duplicate", Typeflag: tar.TypeReg}, {Name: "duplicate", Typeflag: tar.TypeReg}},
		{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "other"}, {Name: "link/child", Typeflag: tar.TypeReg}},
		{{Name: ".wh.victim", Typeflag: tar.TypeSymlink, Linkname: "other"}},
		{{Name: "device", Typeflag: tar.TypeChar}},
		{{Name: "file", Typeflag: tar.TypeReg, PAXRecords: map[string]string{"SCHILY.xattr.synthetic": "value"}}},
	}
	for i, headers := range cases {
		raw, diff := packageTestLayer(t, headers, nil)
		if err := applyPackageLayer(bytes.NewReader(raw), diff, map[string]packageRootEntry{}); err == nil {
			t.Fatalf("accepted malformed layer %d", i)
		}
	}
}

// Base hardlinks are modeled only when the prior target and inode metadata agree.
func TestPackageLayerHardlinksAndDiffIdentity(t *testing.T) {
	raw, diff := packageTestLayer(t, []*tar.Header{{Name: "target", Typeflag: tar.TypeReg, Mode: 0755, Size: 4}, {Name: "alias", Typeflag: tar.TypeLink, Mode: 0755, Linkname: "target"}}, [][]byte{[]byte("data"), nil})
	filesystem := map[string]packageRootEntry{}
	if err := applyPackageLayer(bytes.NewReader(raw), diff, filesystem); err != nil {
		t.Fatal(err)
	}
	if filesystem["alias"].Sha256 != filesystem["target"].Sha256 {
		t.Fatal("hardlink content differs")
	}
	changed, changedDiff := packageTestLayer(t, []*tar.Header{{Name: "target", Typeflag: tar.TypeReg, Mode: 0755, Size: 4}}, [][]byte{[]byte("next")})
	if err := applyPackageLayer(bytes.NewReader(changed), changedDiff, filesystem); err == nil || !strings.Contains(err.Error(), "hardlink target") {
		t.Fatalf("approximated hardlink replacement: %v", err)
	}
	if err := applyPackageLayer(bytes.NewReader(raw), digestImageBytes([]byte("wrong")), map[string]packageRootEntry{}); err == nil {
		t.Fatal("accepted wrong diff ID")
	}
	raw, diff = packageTestLayer(t, []*tar.Header{{Name: "alias", Typeflag: tar.TypeLink, Mode: 0755, Linkname: "missing"}}, nil)
	if err := applyPackageLayer(bytes.NewReader(raw), diff, map[string]packageRootEntry{}); err == nil {
		t.Fatal("accepted missing hardlink target")
	}
}

// Database errors cannot be hidden behind a successful builder process exit.
func TestPackageStatusRejectsDuplicateAndUnconfiguredPackages(t *testing.T) {
	good := "Package: synthetic\nStatus: install ok installed\nArchitecture: amd64\nVersion: 1.0\nDescription: synthetic\n continuation\n\n"
	got, err := parsePackageStatus([]byte(good))
	if err != nil || got["synthetic"]["Version"] != "1.0" {
		t.Fatalf("valid database: %v", err)
	}
	for _, bad := range []string{good + good, strings.Replace(good, "install ok installed", "install ok unpacked", 1), strings.Replace(good, "Version: 1.0", "Version: 1.0\nVersion: 2.0", 1), strings.Replace(good, "Version: 1.0", "Version: 1.0\n changed", 1), " orphan\n", strings.Replace(good, "Package: synthetic\n", "", 1)} {
		if _, err := parsePackageStatus([]byte(bad)); err == nil {
			t.Fatalf("accepted bad database %q", bad)
		}
	}
}

// Sorted rootfs identities are stable but change with content, links or ownership.
func TestPackageRootHashCoversEveryInode(t *testing.T) {
	a := map[string]packageRootEntry{"a": {Kind: tar.TypeReg, Sha256: digestImageBytes([]byte("a"))}, "b": {Kind: tar.TypeDir, Mode: 0755}}
	b := map[string]packageRootEntry{"b": a["b"], "a": a["a"]}
	first, err := packageRootHash(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := packageRootHash(b)
	if err != nil || first != second {
		t.Fatal("unstable root identity")
	}
	entry := b["b"]
	entry.Uid = 9
	b["b"] = entry
	second, err = packageRootHash(b)
	if err != nil || first == second {
		t.Fatal("ownership omitted from root identity")
	}
}

// All added runtime fields are strict, including ambiguous case-folded aliases.
func TestPackageRuntimeRejectsExtraAuthorityAndJsonAliases(t *testing.T) {
	for _, raw := range []string{`{"User":"root","user":"other"}`, `{"OnBuild":["RUN true"]}`, `{"Volumes":{"/usr":{}}}`, `{"Healthcheck":{"Test":["CMD","true"]}}`} {
		var runtime packageRuntime
		if err := decodeImageJson([]byte(raw), &runtime, true); err == nil {
			t.Fatalf("accepted runtime expansion %s", raw)
		}
	}
	var config packageImageConfig
	if err := decodeImageJson([]byte(`{"packages":{"sample":{"path":"first","PATH":"second"}}}`), &config, true); err == nil {
		t.Fatal("accepted package pin alias")
	}
}

// OCI fixtures preserve descriptor joins, with a synthetic local index and layer.
func packageTestBase(t *testing.T) (string, packageBase) {
	t.Helper()
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "blobs/sha256"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(raw []byte, media string) packageDescriptor {
		digest := digestImageBytes(raw)
		if err := os.WriteFile(filepath.Join(directory, "blobs/sha256", strings.TrimPrefix(digest, "sha256:")), raw, 0600); err != nil {
			t.Fatal(err)
		}
		return packageDescriptor{MediaType: media, Digest: digest, Size: int64(len(raw))}
	}
	layer, diff := packageTestLayer(t, []*tar.Header{{Name: "synthetic", Typeflag: tar.TypeReg, Mode: 0644, Size: 4}}, [][]byte{[]byte("base")})
	configRaw, _ := json.Marshal(map[string]any{"os": "linux", "architecture": "amd64", "config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": []string{diff}}})
	manifest := packageManifest{SchemaVersion: 2, MediaType: ociManifestMediaType, Config: write(configRaw, ociConfigMediaType), Layers: []packageDescriptor{write(layer, ociLayerMediaType)}}
	raw, _ := json.Marshal(manifest)
	platform := write(raw, ociManifestMediaType)
	platform.Platform = &packagePlatform{Os: "linux", Architecture: "amd64"}
	raw, _ = json.Marshal(packageManifest{SchemaVersion: 2, MediaType: ociIndexMediaType, Manifests: []packageDescriptor{platform}})
	index := write(raw, ociIndexMediaType)
	base, err := readPackageBase(directory, index.Digest)
	if err != nil {
		t.Fatal(err)
	}
	return directory, base
}

// Neither a cached tag nor an unrelated config/layer proves membership in the pin.
func TestPackageBaseAuthenticatesIndexMembershipAndEveryBlob(t *testing.T) {
	directory, base := packageTestBase(t)
	copied := filepath.Join(t.TempDir(), "base")
	if err := copyPackageBase(directory, copied, base); err != nil {
		t.Fatal(err)
	}
	again, err := readPackageBase(copied, base.IndexDigest)
	if err != nil || !reflect.DeepEqual(again, base) {
		t.Fatalf("copied closure differs: %v", err)
	}
	for _, descriptor := range []packageDescriptor{base.Platform, base.Config, base.Layers[0]} {
		path := filepath.Join(directory, "blobs/sha256", strings.TrimPrefix(descriptor.Digest, "sha256:"))
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readPackageBase(directory, base.IndexDigest); err == nil {
			t.Fatal("accepted tampered base blob")
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readPackageBase(directory, digestImageBytes([]byte("unrelated index"))); err == nil {
		t.Fatal("accepted unrelated index")
	}
}

// External and inline descriptor payloads are not part of this offline boundary.
func TestPackageDescriptorRejectsExternalDataAndWrongSizes(t *testing.T) {
	base := packageDescriptor{MediaType: ociLayerMediaType, Digest: digestImageBytes([]byte("data")), Size: 4}
	for _, change := range []func(*packageDescriptor){func(d *packageDescriptor) { d.Urls = []string{"https://blob.example/data"} }, func(d *packageDescriptor) { d.Data = "ZGF0YQ==" }, func(d *packageDescriptor) { d.Size = 0 }, func(d *packageDescriptor) { d.Digest = "sha256:short" }, func(d *packageDescriptor) { d.MediaType = ociConfigMediaType }} {
		d := base
		change(&d)
		if err := validatePackageDescriptor(d, ociLayerMediaType); err == nil {
			t.Fatal("accepted unbounded descriptor")
		}
	}
}
