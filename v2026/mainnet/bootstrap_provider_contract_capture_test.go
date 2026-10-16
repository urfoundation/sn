// Original request source selection is independent of old installation approval.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/urfoundation/sn/v2026/miner"
)

func newBootstrapProviderContractCaptureFixture(t *testing.T) (*bootstrapChainFixture, bootstrapProviderRoleRequest, string, string) {
	t.Helper()
	f, request, path, output := newBootstrapProviderWorkCaptureFixture(t)
	request.RequireOriginalContractCapture = true
	for index := range request.Providers {
		host := &request.Providers[index]
		work, err := miner.ReadProviderWorkCaptureProfile(t.Context(), host.WorkCaptureProfile.Path, host.WorkCaptureProfile.Sha256, true)
		if err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(host.StateDirectory, "original-contracts")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		owner := work.Providers[0]
		profile := miner.ProviderContractCaptureProfile{Schema: miner.ProviderContractCaptureSchema, ApiUrl: host.ApiUrl,
			Providers: []miner.ProviderContractCaptureOwner{{Slot: owner.Slot, ClientId: owner.ClientId, PublicKey: owner.PublicKey, Domain: owner.Domain, Directory: directory, SourceGeneration: [16]byte{byte(124 + index)}}}}
		profilePath := filepath.Join(filepath.Dir(path), host.Id+".original-contracts.json")
		bootstrapRootTestWrite(t, profilePath, profile)
		raw, err := os.ReadFile(profilePath)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		host.ContractCaptureProfile = &planFileReference{Path: profilePath, Sha256: "sha256:" + hex.EncodeToString(digest[:])}
	}
	bootstrapRootTestWrite(t, path, request)
	return f, request, path, output
}

func TestBootstrapProviderOriginalContractExportReachesRequiredMinerArguments(t *testing.T) {
	f, request, path, output := newBootstrapProviderContractCaptureFixture(t)
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, output), &stdout, &stderr); code != 0 {
		t.Fatal("original source provider export failed", code, stderr.String())
	}
	var result bootstrapProviderRoleConfig
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.RequireOriginalContractCapture || !result.RequireWholeWorkCapture || result.ActivationReady || result.NetworkEffects || len(result.ProviderDeclarations) != len(request.Providers) {
		t.Fatal("source export omitted a mandatory owner or granted runtime authority")
	}
	for index, launch := range result.ProviderDeclarations {
		ref := request.Providers[index].ContractCaptureProfile
		if !launch.OriginalContractCaptureConfigured || !reflect.DeepEqual(ref, launch.Host.ContractCaptureProfile) || !slices.Contains(launch.Arguments, "--original-contract-capture="+ref.Path) || !slices.Contains(launch.Arguments, "--original-contract-capture-sha256="+ref.Sha256) || !slices.Contains(launch.Arguments, "--require-original-contract-capture") {
			t.Fatal("actual provider arguments lost independently approved original source")
		}
		profile, err := miner.ReadProviderContractCaptureProfile(t.Context(), ref.Path, ref.Sha256, true)
		if err != nil || profile.Providers[0].Domain != launch.Domain || profile.ApiUrl != launch.Host.ApiUrl {
			t.Fatal("source export does not reach actual miner profile admission", err)
		}
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !reflect.DeepEqual(reads, f.contracts.counts) {
		t.Fatal("source role export changed original preparation or contacted chain")
	}
}

func TestBootstrapProviderOriginalContractExportRefusesMissingAuthorityBeforePublication(t *testing.T) {
	f, request, path, output := newBootstrapProviderContractCaptureFixture(t)
	request.Providers[1].ContractCaptureProfile = nil
	bootstrapRootTestWrite(t, path, request)
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, output), io.Discard, io.Discard); code != 2 {
		t.Fatal("source export guessed a missing independent provider approval", code)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatal("incomplete source roster published partial provider declarations", err)
	}
}
