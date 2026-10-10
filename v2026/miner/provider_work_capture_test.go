//go:build linux || darwin

// Launch controls exercise the actual provider argument parser, retained files,
// identity bindings and runner admission without creating production identities.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// All identities are deterministic synthetic values; the approver owns a
// different key from the provider and the outbox already has private custody.
func providerWorkCaptureFixture(t *testing.T) (ProviderWorkCaptureProfile, []byte) {
	t.Helper()
	seed := bytes.Repeat([]byte{91}, ed25519.SeedSize)
	providerKey := ed25519.NewKeyFromSeed(seed)
	requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{92}, ed25519.SeedSize))
	profile := ProviderWorkCaptureProfile{Schema: ProviderWorkCaptureSchema, ApiUrl: "https://operator.example", Providers: []ProviderWorkCaptureOwner{{Slot: "direct", ClientId: [16]byte{93}, Domain: providerCloseDomainFixture(), OutboxDirectory: providerRegistrationPrivateDir(t)}}}
	copy(profile.RequestPublicKey[:], requestKey[ed25519.SeedSize:])
	copy(profile.Providers[0].PublicKey[:], providerKey[ed25519.SeedSize:])
	return profile, seed
}

// The exact original bytes and digest follow the public CLI reference grammar.
func writeProviderWorkCaptureFixture(t *testing.T, profile ProviderWorkCaptureProfile) (string, string) {
	t.Helper()
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(providerRegistrationPrivateDir(t), "capture.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return path, "sha256:" + hex.EncodeToString(digest[:])
}

// The real command parser must retain all three complete-launch controls.
func TestProviderWholeWorkLaunchActualArguments(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, OptionsFirst: false}
	for _, prefix := range [][]string{{"provide"}, {"auth-provide", "synthetic-code"}} {
		args := append(prefix, "--whole-work-capture=/synthetic/capture.json", "--whole-work-capture-sha256=sha256:"+string(bytes.Repeat([]byte{'a'}, 64)), "--require-whole-work-capture")
		options, err := parser.ParseArgs(mainUsage(), args, "")
		if err != nil || options["--whole-work-capture"] != "/synthetic/capture.json" || options["--require-whole-work-capture"] != true {
			t.Fatal("actual provider parser lost whole-work launch policy", options, err)
		}
	}
}

// Refusal happens before durable client allocation, HTTP authentication or any
// live SDK owner; an absent optional profile still has the legacy nil meaning.
func TestProviderWholeWorkRequiredProfileRefusesBeforeRegistration(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	settings := fixture.settings(true)
	settings.requireWorkCapture = true
	ctx := context.WithValue(t.Context(), providerRegistrationHooksKey{}, providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error {
		return errors.New("synthetic barrier after actual forbidden registration")
	}})
	if err := settings.run(ctx, &providerRefusedWriter{}); err == nil {
		t.Fatal("required whole-work launch started without a complete profile")
	}
	posts, legacy, allocations, refreshes := fixture.counts()
	if posts != 0 || legacy != 0 || allocations != 0 || refreshes != 0 {
		t.Fatal("missing whole-work configuration crossed identity admission")
	}
	if _, err := os.Stat(filepath.Join(fixture.dir, ".provider.key")); !os.IsNotExist(err) {
		t.Fatal("missing capture profile created replacement client custody", err)
	}
	if profile, err := ReadProviderWorkCaptureProfile(t.Context(), "", "", false); err != nil || profile != nil {
		t.Fatal("legacy omission acquired whole-work authority", profile, err)
	}
	if instance, err := startSwarmMember(t.Context(), ProviderSwarmMember{RequireWorkCapture: true}, func(error) {}); err == nil || instance != nil {
		t.Fatal("swarm started wallet or SDK work before required capture admission")
	}
}

// Profile identity, complete domain and the retained seed reach the exact nested
// manager settings; a second launch receives independent mutable settings.
func TestProviderWholeWorkLaunchBindsActualSettingsAndRetainedKey(t *testing.T) {
	profile, seed := providerWorkCaptureFixture(t)
	path, digest := writeProviderWorkCaptureFixture(t, profile)
	loaded, err := ReadProviderWorkCaptureProfile(t.Context(), path, digest, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.validateRole(profile.ApiUrl, []string{"direct"}, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	first, second := ProviderDeviceSettings([32]byte{}), ProviderDeviceSettings([32]byte{})
	for _, settings := range []*sdk.DeviceLocalSettings{first, second} {
		settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, nil, nil)
		if err := loaded.apply(settings, "direct", connect.Id(profile.Providers[0].ClientId)); err != nil {
			t.Fatal(err)
		}
		capture := settings.ContractManagerSettings.OriginalWorkCapture
		domain, _ := profile.Providers[0].Domain.Digest()
		if capture == nil || capture.ApiUrl != profile.ApiUrl || capture.RequestPublicKey != profile.RequestPublicKey || capture.OutboxDirectory != profile.Providers[0].OutboxDirectory || settings.ContractManagerSettings.CloseReportDomainHash != domain || !settings.ClientKeyRegistrationRequired || !bytes.Equal(settings.KeyMaterial.GetClientKeySeed(), seed) {
			t.Fatal("actual SDK settings lost original profile or replaced retained key")
		}
	}
	first.ContractManagerSettings.OriginalWorkCapture.RequestPublicKey[0]++
	if second.ContractManagerSettings.OriginalWorkCapture.RequestPublicKey != profile.RequestPublicKey {
		t.Fatal("provider owners shared mutable whole-work settings")
	}
}

// Changed bytes, aliases, absent outboxes and canceled read ownership cannot
// reinterpret a reviewed profile or provision an empty replacement outbox.
func TestProviderWholeWorkLaunchRetainsExactProfileAndPrivateOutbox(t *testing.T) {
	profile, _ := providerWorkCaptureFixture(t)
	path, digest := writeProviderWorkCaptureFixture(t, profile)
	alias := filepath.Join(filepath.Dir(path), "alias.json")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadProviderWorkCaptureProfile(t.Context(), alias, digest, true); err == nil || value != nil {
		t.Fatal("whole-work launch followed a replacement profile link")
	}
	changed := profile
	changed.RequestPublicKey[0]++
	raw, _ := json.Marshal(changed)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadProviderWorkCaptureProfile(t.Context(), path, digest, true); err == nil || value != nil {
		t.Fatal("whole-work launch accepted changed original profile")
	}
	if err := os.Remove(profile.Providers[0].OutboxDirectory); err != nil {
		t.Fatal(err)
	}
	if err := profile.Validate(); err == nil {
		t.Fatal("whole-work launch created missing original outbox")
	}
	if _, err := os.Stat(profile.Providers[0].OutboxDirectory); !os.IsNotExist(err) {
		t.Fatal("whole-work launch replaced missing custody", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if value, err := ReadProviderWorkCaptureProfile(ctx, path, digest, true); err != context.Canceled || value != nil {
		t.Fatal("canceled launch read lost its owner", value, err)
	}
}

// No smaller roster, foreign operator, changed domain or replacement key can
// enable a complete provider role under the reviewed request authority.
func TestProviderWholeWorkLaunchRefusesRosterDomainAndIdentityChanges(t *testing.T) {
	profile, seed := providerWorkCaptureFixture(t)
	for _, role := range []struct {
		apiUrl string
		slots  []string
		domain [32]byte
	}{{apiUrl: profile.ApiUrl, slots: []string{}}, {apiUrl: profile.ApiUrl, slots: []string{"other"}}, {apiUrl: "https://foreign.example", slots: []string{"direct"}}, {apiUrl: profile.ApiUrl, slots: []string{"direct"}, domain: [32]byte{94}}} {
		if err := profile.validateRole(role.apiUrl, role.slots, role.domain); err == nil {
			t.Fatal("whole-work launch reinterpreted its complete original role", role)
		}
	}
	for _, actual := range []struct {
		seed []byte
		id   connect.Id
	}{{seed: seed, id: connect.Id{95}}, {seed: bytes.Repeat([]byte{96}, 32), id: connect.Id(profile.Providers[0].ClientId)}, {seed: nil, id: connect.Id(profile.Providers[0].ClientId)}} {
		settings := ProviderDeviceSettings([32]byte{})
		settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(actual.seed, nil, nil)
		if err := profile.apply(settings, "direct", actual.id); err == nil || settings.ContractManagerSettings.OriginalWorkCapture != nil {
			t.Fatal("whole-work launch substituted client identity or seed")
		}
	}
	profile.RequestPublicKey = profile.Providers[0].PublicKey
	if err := profile.Validate(); err == nil {
		t.Fatal("provider client became its own independent window approver")
	}
}
