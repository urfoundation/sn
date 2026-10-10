// Original host exports bind independently supplied capture authority to the
// actual provider arguments; no bootstrap approval is repurposed as a new key.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/urfoundation/sn/v2026/miner"
)

// Every profile has a separately retained synthetic request key, provider key,
// complete original domain and private outbox; none is derived from plan signers.
func newBootstrapProviderWorkCaptureFixture(t *testing.T) (*bootstrapChainFixture, bootstrapProviderRoleRequest, string, string) {
	t.Helper()
	f, request, path, output := newBootstrapProviderRoleFixture(t)
	original, err := loadBootstrapProviderRoleConfig(f.storageContext(t.Context()), f.path, path, output)
	if err != nil {
		t.Fatal(err)
	}
	request.RequireWholeWorkCapture = true
	for index := range request.Providers {
		host := &request.Providers[index]
		outbox := filepath.Join(host.StateDirectory, "whole-work")
		if err := os.MkdirAll(outbox, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(outbox, 0700); err != nil {
			t.Fatal(err)
		}
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(111 + index)}, ed25519.SeedSize))
		requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{115}, ed25519.SeedSize))
		profile := miner.ProviderWorkCaptureProfile{Schema: miner.ProviderWorkCaptureSchema, ApiUrl: host.ApiUrl,
			Providers: []miner.ProviderWorkCaptureOwner{{Slot: "direct", ClientId: [16]byte{byte(116 + index)}, Domain: original.ProviderDeclarations[index].Domain, OutboxDirectory: outbox}}}
		copy(profile.Providers[0].PublicKey[:], key[ed25519.SeedSize:])
		copy(profile.RequestPublicKey[:], requestKey[ed25519.SeedSize:])
		profilePath := filepath.Join(filepath.Dir(path), host.Id+".capture.json")
		bootstrapRootTestWrite(t, profilePath, profile)
		raw, err := os.ReadFile(profilePath)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		host.WorkCaptureProfile = &planFileReference{Path: profilePath, Sha256: "sha256:" + hex.EncodeToString(digest[:])}
	}
	bootstrapRootTestWrite(t, path, request)
	return f, request, path, output
}

// The public launch command emits the exact profile path, digest and mandatory
// flag consumed by the miner; custody and chain observations remain unchanged.
func TestBootstrapProviderWholeWorkExportReachesRequiredMinerArguments(t *testing.T) {
	f, request, path, output := newBootstrapProviderWorkCaptureFixture(t)
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, output), &stdout, &stderr); code != 0 {
		t.Fatal("complete whole-work provider export failed", code, stderr.String())
	}
	var result bootstrapProviderRoleConfig
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.RequireWholeWorkCapture || result.ActivationReady || result.NetworkEffects || len(result.ProviderDeclarations) != len(request.Providers) {
		t.Fatal("capture configuration became activation authority or omitted an original host")
	}
	for index, launch := range result.ProviderDeclarations {
		ref := request.Providers[index].WorkCaptureProfile
		if !launch.WholeWorkCaptureConfigured || !reflect.DeepEqual(launch.Host.WorkCaptureProfile, ref) || !slices.Contains(launch.Arguments, "--whole-work-capture="+ref.Path) || !slices.Contains(launch.Arguments, "--whole-work-capture-sha256="+ref.Sha256) || !slices.Contains(launch.Arguments, "--require-whole-work-capture") {
			t.Fatal("provider export lost mandatory exact capture profile binding")
		}
		profile, err := miner.ReadProviderWorkCaptureProfile(t.Context(), ref.Path, ref.Sha256, true)
		if err != nil || profile.Providers[0].Domain != launch.Domain || profile.ApiUrl != launch.Host.ApiUrl {
			t.Fatal("exported profile does not reach actual miner admission", err)
		}
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !reflect.DeepEqual(reads, f.contracts.counts) {
		t.Fatal("capture export changed original preparation or contacted chain")
	}
}

// Requiring complete evidence cannot silently omit one host's request authority
// or borrow the old installation approval for its missing profile.
func TestBootstrapProviderWholeWorkExportRefusesMissingAuthorityBeforePublication(t *testing.T) {
	f, request, path, output := newBootstrapProviderWorkCaptureFixture(t)
	request.Providers[1].WorkCaptureProfile = nil
	bootstrapRootTestWrite(t, path, request)
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, output), io.Discard, io.Discard); code != 2 {
		t.Fatal("complete provider export guessed missing independent capture authority", code)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatal("incomplete capture roster published partial provider launch files", err)
	}
}

// A correctly hashed external profile still cannot relabel the independently
// approved installation domain or create a capture route at a foreign operator.
func TestBootstrapProviderWholeWorkExportRefusesForeignReviewedProfile(t *testing.T) {
	f, request, path, output := newBootstrapProviderWorkCaptureFixture(t)
	ref := request.Providers[0].WorkCaptureProfile
	raw, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	var original miner.ProviderWorkCaptureProfile
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"domain", "operator", "slot"} {
		profile := original
		profile.Providers = append([]miner.ProviderWorkCaptureOwner(nil), original.Providers...)
		switch change {
		case "domain":
			profile.Providers[0].Domain.NoID++
		case "operator":
			profile.ApiUrl = "https://foreign.example"
		case "slot":
			profile.Providers[0].Slot = "foreign"
		}
		bootstrapRootTestWrite(t, ref.Path, profile)
		changed, err := os.ReadFile(ref.Path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(changed)
		ref.Sha256 = "sha256:" + hex.EncodeToString(digest[:])
		bootstrapRootTestWrite(t, path, request)
		if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, output), io.Discard, io.Discard); code != 2 {
			t.Fatal("whole-work profile changed original installation role", change, code)
		}
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatal("foreign capture profile published launch files", err)
	}
}
