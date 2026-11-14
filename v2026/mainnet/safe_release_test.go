// Published public release artifacts are the production verifier inputs. Tests
// modify local copies only and never introduce an account or signing identity.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// The maintained public archives keep the command fixture independent of a
// network, package installer, compiler executable or regenerated test bytecode.
func safeReleaseTestInputs(t *testing.T, version, variant string) (safeReleasePin, string, map[string][]byte) {
	t.Helper()
	profile, err := loadSafeReleasePin(version, variant)
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs(filepath.Join("safe-release", profile.ArchiveName))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	members, err := readSafeReleaseMembers(t.Context(), raw, profile, variant)
	if err != nil {
		t.Fatal(err)
	}
	return profile, path, members
}

// Four explicit selections must retain distinct published runtime identities
// and prove no account, authority, rebuild or execution capability.
func TestSafeReleaseCommandVerifiesPinnedVersionsAndVariants(t *testing.T) {
	cases := []struct {
		version, variant, singletonHash, proxyHash string
		sourceCount                                int
	}{
		{version: "1.4.1", variant: "Safe", singletonHash: "0x1fe2df852ba3299d6534ef416eefa406e56ced995bca886ab7a553e6d0c5e1c4", proxyHash: "0xd7d408ebcd99b2b70be43e20253d6d92a8ea8fab29bd3be7f55b10032331fb4c", sourceCount: 42},
		{version: "1.4.1", variant: "SafeL2", singletonHash: "0xb1f926978a0f44a2c0ec8fe822418ae969bd8c3f18d61e5103100339894f81ff", proxyHash: "0xd7d408ebcd99b2b70be43e20253d6d92a8ea8fab29bd3be7f55b10032331fb4c", sourceCount: 42},
		{version: "1.5.0", variant: "Safe", singletonHash: "0xdda019cbd7c867a533a2a86e5c53434fdc50b13122b5a5ddb4a8df61b31c20f2", proxyHash: "0x4e381985ca68b3e5d27b4425fa581c19cf33146d3f887a3cfca96f55528ea46f", sourceCount: 66},
		{version: "1.5.0", variant: "SafeL2", singletonHash: "0x180193227186ccb85316c94db1f0d156ed932b14712cfaac78901899178572dc", proxyHash: "0x4e381985ca68b3e5d27b4425fa581c19cf33146d3f887a3cfca96f55528ea46f", sourceCount: 66},
	}
	for _, c := range cases {
		profile, path, _ := safeReleaseTestInputs(t, c.version, c.variant)
		var stdout, stderr bytes.Buffer
		args := []string{"safe-release-verify", "--version", c.version, "--variant", c.variant, "--archive", path}
		if code := runMain(t.Context(), args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("published profile command rejected %s/%s: %d %s", c.version, c.variant, code, stderr.String())
		}
		var result safeReleaseVerification
		if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Schema != safeReleaseVerificationSchema || result.Status != "published-release-artifacts-verified" || result.Version != c.version || result.Variant != c.variant || result.ArchiveSha256 != profile.ArchiveSha256 || result.SourceCommit != profile.SourceCommit || result.SourceTree != profile.SourceTree || result.SourceFileCount != c.sourceCount || result.SolcLongVersion != "0.7.6+commit.7338295f" || len(result.Artifacts) != 2 || !result.ArtifactIntegrityVerified || !result.PublishedBuildInputsVerified {
			t.Fatalf("published profile result lost exact artifact/provenance scope: %+v", result)
		}
		if result.IndependentRebuildVerified || result.CurrentChainVerified || result.SafeAddressVerified || result.InitializerOwnerBindingVerified || result.SafeAuthorityVerified || result.ApprovalSigningPayloadProvided || result.Executable || result.NetworkEffects || result.InstallationComplete || result.ActivationReady || !slices.Contains(result.RequiredPrerequisites, "SELECTED_SAFE_EQUALS_RETAINED_INITIALIZER_OWNER_OR_SEPARATE_AUTHORIZED_MIGRATION") {
			t.Fatal("offline Safe artifact verification inferred account authority or execution")
		}
		for _, artifact := range result.Artifacts {
			if artifact.Pin.Name == "SafeProxy" {
				if artifact.Pin.RuntimeKeccak256 != c.proxyHash || len(artifact.StorageSlots) != 1 || artifact.StorageSlots[0].Label != "singleton" || artifact.StorageSlots[0].Slot != "0" {
					t.Fatal("published proxy runtime or singleton slot differs")
				}
			} else if artifact.Pin.Name != c.variant || artifact.Pin.RuntimeKeccak256 != c.singletonHash || len(artifact.StorageSlots) != 9 || artifact.StorageSlots[5].Label != "nonce" || artifact.StorageSlots[5].Slot != "5" {
				t.Fatal("published singleton variant/runtime/nonce differs")
			}
		}
		seal := result.ContentHash
		result.ContentHash = ""
		if seal != rootObjectHash(result) {
			t.Fatal("published profile output seal differs")
		}
	}
}

// Mutated artifacts deliberately receive only a new outer record pin, so these
// assertions independently exercise immutable code/ABI/variant admission.
func TestSafeReleaseArtifactBindsCodeAndAbiBeyondOuterPin(t *testing.T) {
	profile, _, members := safeReleaseTestInputs(t, "1.4.1", "Safe")
	pin := profile.Artifacts[0]
	var original safeReleaseArtifact
	if err := json.Unmarshal(members[pin.ArchivePath], &original); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, diagnostic string
		mutate           func(*safeReleaseArtifact)
	}{
		{name: "variant", diagnostic: "identity", mutate: func(a *safeReleaseArtifact) { a.Name = "SafeL2" }},
		{name: "runtime", diagnostic: "bytecode", mutate: func(a *safeReleaseArtifact) { a.Runtime = "0x00" + a.Runtime[4:] }},
		{name: "creation", diagnostic: "bytecode", mutate: func(a *safeReleaseArtifact) { a.Creation = "0x00" + a.Creation[4:] }},
		{name: "ABI", diagnostic: "ABI", mutate: func(a *safeReleaseArtifact) { a.Abi = json.RawMessage("[]") }},
	}
	for _, c := range cases {
		changed := original
		c.mutate(&changed)
		raw, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		outer := pin
		outer.ArtifactSha256 = safeReleaseHash(raw)
		if _, err := verifySafeReleaseArtifact(raw, outer); err == nil || !strings.Contains(err.Error(), c.diagnostic) {
			t.Fatalf("independent Safe %s pin was bypassed: %v", c.name, err)
		}
	}
}

// Compiler settings, tag sources, locked dependency inputs and emitted code
// remain independently pinned after a surrounding build-info digest changes.
func TestSafeReleaseBuildBindsCompilerSourceAndOutput(t *testing.T) {
	profile, _, members := safeReleaseTestInputs(t, "1.4.1", "Safe")
	baseline := members[profile.BuildInfoPath]
	cases := []struct {
		name, diagnostic string
		mutate           func(*safeReleaseBuild, map[string][]byte)
	}{
		{name: "compiler", diagnostic: "compiler/settings", mutate: func(b *safeReleaseBuild, _ map[string][]byte) { b.SolcLongVersion = "0.7.6+synthetic-other-build" }},
		{name: "settings", diagnostic: "compiler/settings", mutate: func(b *safeReleaseBuild, _ map[string][]byte) { b.Input.Settings = json.RawMessage("{}") }},
		{name: "tag source", diagnostic: "source/tag/archive", mutate: func(b *safeReleaseBuild, m map[string][]byte) {
			s := b.Input.Sources["contracts/Safe.sol"]
			s.Content += "\n// synthetic changed source\n"
			b.Input.Sources["contracts/Safe.sol"] = s
			m["package/contracts/Safe.sol"] = []byte(s.Content)
		}},
		{name: "dependency", diagnostic: "dependency source", mutate: func(b *safeReleaseBuild, _ map[string][]byte) {
			s := b.Input.Sources["@openzeppelin/contracts/math/SafeMath.sol"]
			s.Content += "\n// synthetic changed dependency\n"
			b.Input.Sources["@openzeppelin/contracts/math/SafeMath.sol"] = s
		}},
		{name: "source census", diagnostic: "source census", mutate: func(b *safeReleaseBuild, _ map[string][]byte) { delete(b.Input.Sources, "contracts/Safe.sol") }},
		{name: "compiler output", diagnostic: "compiler output", mutate: func(b *safeReleaseBuild, _ map[string][]byte) {
			output := b.Output.Contracts["contracts/Safe.sol"]["Safe"]
			output.Evm.DeployedBytecode.Object = "00" + output.Evm.DeployedBytecode.Object[2:]
			b.Output.Contracts["contracts/Safe.sol"]["Safe"] = output
		}},
	}
	for _, c := range cases {
		var build safeReleaseBuild
		if err := json.Unmarshal(baseline, &build); err != nil {
			t.Fatal(err)
		}
		changed := maps.Clone(members)
		c.mutate(&build, changed)
		raw, err := json.Marshal(build)
		if err != nil {
			t.Fatal(err)
		}
		outer := profile
		outer.BuildInfoSha256 = safeReleaseHash(raw)
		changed[profile.BuildInfoPath] = raw
		if _, err := verifySafeReleaseBuild(changed, outer, "Safe"); err == nil || !strings.Contains(err.Error(), c.diagnostic) {
			t.Fatalf("independent Safe %s provenance was bypassed: %v", c.name, err)
		}
	}
}

// A matching outer layout digest does not permit owner/nonce/singleton slots
// to migrate silently under either currently supported compiler profile.
func TestSafeReleaseStorageBindsSingletonOwnerAndNonceSlots(t *testing.T) {
	profile, _, members := safeReleaseTestInputs(t, "1.5.0", "Safe")
	var build safeReleaseBuild
	if err := json.Unmarshal(members[profile.BuildInfoPath], &build); err != nil {
		t.Fatal(err)
	}
	pin := profile.Artifacts[0]
	original := build.Output.Contracts[pin.SourceName][pin.Name].StorageLayout
	for _, index := range []int{0, 2, 3, 4, 5} {
		var layout struct {
			Storage []safeReleaseStorageSlot `json:"storage"`
		}
		if err := json.Unmarshal(original, &layout); err != nil {
			t.Fatal(err)
		}
		layout.Storage[index].Slot = "99"
		raw, err := json.Marshal(layout)
		if err != nil {
			t.Fatal(err)
		}
		changed := pin
		changed.StorageLayoutSha256, err = safeReleaseJsonHash(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifySafeReleaseStorage(raw, changed); err == nil || !strings.Contains(err.Error(), "storage semantics") {
			t.Fatalf("Safe singleton/owner/nonce slot change was admitted at %d: %v", index, err)
		}
	}
}

// Missing, linked, malformed and expanded inputs fail without extraction even
// if an internal parser caller supplies their outer hash as a synthetic pin.
func TestSafeReleaseArchiveBoundsRequiredMembers(t *testing.T) {
	profile, _, members := safeReleaseTestInputs(t, "1.4.1", "Safe")
	for _, change := range []string{"missing", "duplicate", "link", "oversized", "repacked"} {
		var compressed bytes.Buffer
		zip := gzip.NewWriter(&compressed)
		writer := tar.NewWriter(zip)
		write := func(name string, raw []byte, kind byte, size int64) {
			t.Helper()
			header := &tar.Header{Name: name, Mode: 0600, Size: size, Typeflag: kind}
			if kind == tar.TypeSymlink {
				header.Linkname = "synthetic-target"
			}
			if err := writer.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if kind == tar.TypeReg && size == int64(len(raw)) {
				if _, err := writer.Write(raw); err != nil {
					t.Fatal(err)
				}
			}
		}
		if change == "oversized" {
			write("synthetic-oversized", nil, tar.TypeReg, maximumSafeReleaseMemberBytes+1)
		} else {
			for _, name := range slices.Sorted(maps.Keys(members)) {
				if name == "package/contracts/Safe.sol" && change == "missing" {
					continue
				}
				kind := byte(tar.TypeReg)
				value := members[name]
				if name == "package/contracts/Safe.sol" && change == "link" {
					kind = tar.TypeSymlink
					value = nil
				}
				write(name, value, kind, int64(len(value)))
				if name == "package/contracts/Safe.sol" && change == "duplicate" {
					write(name, value, kind, int64(len(value)))
				}
			}
		}
		closeErr := writer.Close()
		if change != "oversized" && closeErr != nil {
			t.Fatal(closeErr)
		}
		if err := zip.Close(); err != nil {
			t.Fatal(err)
		}
		changed := profile
		if change != "repacked" {
			changed.ArchiveSha256 = safeReleaseHash(compressed.Bytes())
		}
		if _, err := readSafeReleaseMembers(t.Context(), compressed.Bytes(), changed, "Safe"); err == nil {
			t.Fatal("Safe bounded archive admitted", change)
		}
	}
}

// The public input boundary denies authority flags and unpinned replacement
// files, and cancellation/output failures emit no successful authority result.
func TestSafeReleaseCommandRejectsAuthorityAndFileSubstitution(t *testing.T) {
	_, path, _ := safeReleaseTestInputs(t, "1.4.1", "Safe")
	base := []string{"safe-release-verify", "--version", "1.4.1", "--variant", "Safe", "--archive", path}
	for _, extra := range [][]string{{"--rpc", "https://rpc.example"}, {"--safe", "0xsynthetic"}, {"--sign"}, {"--submit"}, {"--run-dir", t.TempDir()}, {"--version", "1.3.0"}, {"--variant", "safe"}, {"extra"}} {
		var stdout, stderr bytes.Buffer
		if code := runMain(t.Context(), append(slices.Clone(base), extra...), &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatal("Safe artifact command acquired account/execution scope", extra, code)
		}
	}
	directory := t.TempDir()
	symlink := filepath.Join(directory, "linked.tgz")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(directory, "special.tgz")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(directory, "changed.tgz")
	if err := os.WriteFile(changed, []byte("synthetic unpinned archive"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{symlink, fifo, changed, directory} {
		var stdout, stderr bytes.Buffer
		args := slices.Clone(base)
		args[len(args)-1] = candidate
		if code := runMain(t.Context(), args, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
			t.Fatal("Safe artifact command admitted substituted local input", candidate, code)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := runMain(ctx, base, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
		t.Fatal("canceled Safe artifact observation succeeded", code)
	}
	if code := runMain(t.Context(), base, ioFailureWriter{}, io.Discard); code != 1 {
		t.Fatal("Safe artifact output failure was lost", code)
	}
}
