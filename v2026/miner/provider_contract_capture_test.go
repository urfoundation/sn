//go:build linux || darwin

// Independent profile and actual launch controls retain original identity and
// refuse incomplete source custody before provider allocation or SDK creation.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

func providerContractCaptureFixture(t *testing.T) (ProviderContractCaptureProfile, []byte) {
	t.Helper()
	seed := bytes.Repeat([]byte{111}, ed25519.SeedSize)
	key := ed25519.NewKeyFromSeed(seed)
	owner := ProviderContractCaptureOwner{Slot: "direct", ClientId: [16]byte{112}, Domain: providerCloseDomainFixture(), Directory: providerRegistrationPrivateDir(t), SourceGeneration: [16]byte{113}}
	copy(owner.PublicKey[:], key[ed25519.SeedSize:])
	return ProviderContractCaptureProfile{Schema: ProviderContractCaptureSchema, ApiUrl: "https://operator.example", Providers: []ProviderContractCaptureOwner{owner}}, seed
}

func writeProviderContractCaptureFixture(t *testing.T, profile ProviderContractCaptureProfile) (string, string) {
	t.Helper()
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(providerRegistrationPrivateDir(t), "original-contracts.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, portableProviderWorkCaptureDigest(raw)
}

func TestProviderOriginalContractProfilePreservesApprovedSourceWithoutLiveDirectory(t *testing.T) {
	profile, _ := providerContractCaptureFixture(t)
	if err := os.Remove(profile.Providers[0].Directory); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := DecodeProviderContractCaptureProfile(t.Context(), raw, portableProviderWorkCaptureDigest(raw))
	if err != nil || loaded == nil || !reflect.DeepEqual(*loaded, profile) {
		t.Fatal("portable source decoder changed independently approved identity", loaded, err)
	}
	if err := loaded.Validate(); err == nil {
		t.Fatal("portable source scope became live original custody")
	}
	if _, err := os.Lstat(profile.Providers[0].Directory); !os.IsNotExist(err) {
		t.Fatal("profile decoder recreated missing original source", err)
	}
	if value, err := ReadProviderContractCaptureProfile(t.Context(), "", "", false); err != nil || value != nil {
		t.Fatal("legacy omission acquired original source authority", value, err)
	}
}

func TestProviderOriginalContractProfileRefusesAuthorityAndNamespaceChanges(t *testing.T) {
	profile, _ := providerContractCaptureFixture(t)
	original, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ProviderContractCaptureProfile){
		func(value *ProviderContractCaptureProfile) { value.Schema += "-other" },
		func(value *ProviderContractCaptureProfile) { value.ApiUrl += "/route" },
		func(value *ProviderContractCaptureProfile) { value.Providers[0].PublicKey = [32]byte{} },
		func(value *ProviderContractCaptureProfile) { value.Providers[0].ClientId = [16]byte{} },
		func(value *ProviderContractCaptureProfile) { value.Providers[0].SourceGeneration = [16]byte{} },
		func(value *ProviderContractCaptureProfile) { value.Providers[0].Domain.GenesisHash = [32]byte{} },
		func(value *ProviderContractCaptureProfile) { value.Providers[0].Directory = "/" },
		func(value *ProviderContractCaptureProfile) {
			value.Providers = append(value.Providers, value.Providers[0])
		},
	} {
		changed := profile
		changed.Providers = append([]ProviderContractCaptureOwner(nil), profile.Providers...)
		change(&changed)
		raw, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if value, err := DecodeProviderContractCaptureProfile(t.Context(), raw, portableProviderWorkCaptureDigest(raw)); err == nil || value != nil {
			t.Fatal("original source admitted incomplete authority", value, err)
		}
	}
	for _, raw := range [][]byte{
		bytes.Replace(original, []byte(`"slot":"direct"`), []byte(`"slot":"direct","SLOT":"other"`), 1),
		append([]byte(`{"generation":[114],`), original[1:]...),
		append(bytes.Clone(original), []byte(` {}`)...),
	} {
		if value, err := DecodeProviderContractCaptureProfile(t.Context(), raw, portableProviderWorkCaptureDigest(raw)); err == nil || value != nil {
			t.Fatal("original source accepted inferred or duplicate generation authority", value, err)
		}
	}
	if value, err := DecodeProviderContractCaptureProfile(t.Context(), append(bytes.Clone(original), '\n'), portableProviderWorkCaptureDigest(original)); err == nil || value != nil {
		t.Fatal("original source accepted changed exact profile bytes", value, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if value, err := DecodeProviderContractCaptureProfile(ctx, original, portableProviderWorkCaptureDigest(original)); !errors.Is(err, context.Canceled) || value != nil {
		t.Fatal("source profile discarded owner cancellation", value, err)
	}
}

func TestProviderOriginalContractLaunchActualArgumentsAndRequiredRefusal(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, OptionsFirst: false}
	for _, prefix := range [][]string{{"provide"}, {"auth-provide", "synthetic-code"}} {
		arguments := append(prefix, "--original-contract-capture=/synthetic/original-contracts.json", "--original-contract-capture-sha256=sha256:"+string(bytes.Repeat([]byte{'a'}, 64)), "--require-original-contract-capture")
		options, err := parser.ParseArgs(mainUsage(), arguments, "")
		if err != nil || options["--original-contract-capture"] != "/synthetic/original-contracts.json" || options["--require-original-contract-capture"] != true {
			t.Fatal("actual provider command lost explicit source policy", options, err)
		}
	}
	fixture := newProviderRegistrationFixture(t)
	settings := fixture.settings(true)
	settings.requireContractCapture = true
	if err := settings.run(t.Context(), &providerRefusedWriter{}); err == nil {
		t.Fatal("required source started without independently approved capture profile")
	}
	posts, legacy, allocations, refreshes := fixture.counts()
	if posts != 0 || legacy != 0 || allocations != 0 || refreshes != 0 {
		t.Fatal("missing original source profile crossed registration boundary")
	}
	if instance, err := startSwarmMember(t.Context(), ProviderSwarmMember{RequireContractCapture: true}, func(error) {}); err == nil || instance != nil {
		t.Fatal("swarm started without mandatory original contract profile")
	}
}

func TestProviderOriginalContractLaunchBindsActualSettingsAndRefusesKeyOrScopeChanges(t *testing.T) {
	profile, seed := providerContractCaptureFixture(t)
	path, digest := writeProviderContractCaptureFixture(t, profile)
	loaded, err := ReadProviderContractCaptureProfile(t.Context(), path, digest, true)
	if err != nil {
		t.Fatal(err)
	}
	settings := ProviderDeviceSettings([32]byte{})
	settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, nil, nil)
	if err := loaded.apply(settings, "direct", connect.Id(profile.Providers[0].ClientId)); err != nil {
		t.Fatal(err)
	}
	capture := settings.ContractManagerSettings.OriginalContractCapture
	domain, _ := profile.Providers[0].Domain.Digest()
	if capture == nil || capture.Directory != profile.Providers[0].Directory || capture.PublicKey != profile.Providers[0].PublicKey || capture.SourceGeneration != profile.Providers[0].SourceGeneration || settings.ContractManagerSettings.CloseReportDomainHash != domain || !settings.ClientKeyRegistrationRequired || !bytes.Equal(seed, settings.KeyMaterial.GetClientKeySeed()) {
		t.Fatal("actual SDK original source settings changed approved custody", capture)
	}
	settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(bytes.Repeat([]byte{115}, ed25519.SeedSize), nil, nil)
	if err := loaded.apply(settings, "direct", connect.Id(profile.Providers[0].ClientId)); err == nil {
		t.Fatal("original source profile substituted its retained provider key")
	}
	work := &ProviderWorkCaptureProfile{ApiUrl: profile.ApiUrl, Providers: []ProviderWorkCaptureOwner{{Slot: "direct", ClientId: profile.Providers[0].ClientId, PublicKey: profile.Providers[0].PublicKey, Domain: profile.Providers[0].Domain, OutboxDirectory: profile.Providers[0].Directory}}}
	if err := loaded.validateRole(profile.ApiUrl, []string{"direct"}, domain, work); err == nil {
		t.Fatal("original source shares the complete-cut outbox namespace")
	}
	if err := loaded.validateRole(profile.ApiUrl, []string{"direct", "proxy-0"}, domain, nil); err == nil {
		t.Fatal("partial source roster became complete actual role")
	}
}

func TestProviderOriginalContractActualConstructorRefusesUnpreparedSource(t *testing.T) {
	profile, seed := providerContractCaptureFixture(t)
	settings := ProviderDeviceSettings([32]byte{})
	settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, nil, nil)
	device, err := newProviderDeviceLocal(t.Context(), nil, nil, "", "synthetic unprepared contract source", settings, nil, "direct", connect.Id(profile.Providers[0].ClientId), &profile)
	if device != nil || !errors.Is(err, connect.ErrOriginalContractStoreIdentity) {
		t.Fatal("actual constructor accepted an unprepared original source", device, err)
	}
	entries, readErr := os.ReadDir(profile.Providers[0].Directory)
	if readErr != nil || len(entries) != 0 {
		t.Fatal("failed source admission created replacement birth or index", entries, readErr)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	settings.ContractManagerSettings.OriginalContractCapture = nil
	device, err = newProviderDeviceLocal(ctx, nil, nil, "", "synthetic canceled contract source", settings, nil, "direct", connect.Id(profile.Providers[0].ClientId), &profile)
	if device != nil || !errors.Is(err, context.Canceled) || settings.ContractManagerSettings.OriginalContractCapture != nil {
		t.Fatal("canceled source constructor changed original ownership", device, err)
	}
}
