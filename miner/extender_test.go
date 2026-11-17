package miner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

// The provider extender identity in the miner (connect/EXTENDER.md B1, G1).
//
// A provider's space keeps no local state, so the extender identity lives in
// the provider state directory beside the client key seed, one file per gossip
// node, named by the node's client id: the run path reads it before the
// device is built and writes back whatever the role ended up with. Losing it
// means activating under a new key every launch, which the operator revokes as
// fast as it publishes.

// The seed survives a write and a read, is kept private to the owner, and a
// node that has never run reads none rather than failing.
func TestProviderExtenderKeySeedRoundTrips(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", stateDir)
	clientId := connect.NewId()

	extenderKeySeed, err := readProviderExtenderKeySeed(clientId)
	if err != nil {
		t.Fatalf("a fresh install failed to read: %s", err)
	}
	if extenderKeySeed != nil {
		t.Fatalf("a fresh install read %v, expected no seed", extenderKeySeed)
	}

	extenderKeySeed, err = connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeProviderExtenderKeySeed(clientId, extenderKeySeed); err != nil {
		t.Fatal(err)
	}
	restoredKeySeed, err := readProviderExtenderKeySeed(clientId)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredKeySeed, extenderKeySeed) {
		t.Fatalf("read %v, expected the seed that was written", restoredKeySeed)
	}
	// the seed rides the device's key material, which is how the space runs
	// under the identity this file names
	keyMaterial := sdk.NewDeviceLocalKeyMaterial(nil, nil, nil)
	keyMaterial.SetExtenderKeySeed(restoredKeySeed)
	if !bytes.Equal(keyMaterial.GetExtenderKeySeed(), extenderKeySeed) {
		t.Fatal("the key material did not carry the seed")
	}

	// anyone with this file can present the node's extender identity
	seedPath := filepath.Join(stateDir, ".provider.extender."+clientId.String()+".key")
	info, err := os.Stat(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("mode = %o, expected 0600", mode)
	}
	// it is its own file: the client key seed beside it is a different
	// identity and must not be overwritten by it
	clientKeySeedBytes, err := os.ReadFile(filepath.Join(stateDir, ".provider.key"))
	if err == nil {
		t.Fatalf("the extender seed was written as the client key seed: %v", clientKeySeedBytes)
	}
}

// Each node's key is its own: one node's file is never another's, and the
// install's shared `.provider.extender.key` goes to the first node that reads
// a key, renamed to that node's file, and to no other node after it.
func TestProviderExtenderKeySeedIsKeptPerNode(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", stateDir)
	firstClientId, secondClientId := connect.NewId(), connect.NewId()
	legacySeed, err := connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(stateDir, ".provider.extender.key")
	if err := os.WriteFile(legacyPath, legacySeed, 0600); err != nil {
		t.Fatal(err)
	}

	firstSeed, err := readProviderExtenderKeySeed(firstClientId)
	if err != nil || !bytes.Equal(firstSeed, legacySeed) {
		t.Fatal("the first node did not adopt the install's key", err)
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the adopted key is still there for another node", err)
	}
	info, err := os.Stat(filepath.Join(stateDir, ".provider.extender."+firstClientId.String()+".key"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("the adopted key is not the first node's private file", err)
	}
	secondSeed, err := readProviderExtenderKeySeed(secondClientId)
	if err != nil || secondSeed != nil {
		t.Fatal("a second node read a key it never had", err)
	}
	if again, err := readProviderExtenderKeySeed(firstClientId); err != nil || !bytes.Equal(again, legacySeed) {
		t.Fatal("the first node's key did not hold", err)
	}

	// a second node's own key leaves the first's alone
	ownSeed, err := connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeProviderExtenderKeySeed(secondClientId, ownSeed); err != nil {
		t.Fatal(err)
	}
	if again, err := readProviderExtenderKeySeed(firstClientId); err != nil || !bytes.Equal(again, legacySeed) {
		t.Fatal("a second node's key replaced the first's", err)
	}
	if again, err := readProviderExtenderKeySeed(secondClientId); err != nil || !bytes.Equal(again, ownSeed) {
		t.Fatal("the second node's key did not hold", err)
	}
}

// Only the slot whose space runs the gossip node writes an extender key, and
// under that node's own client id: a slot that runs no node writes none, and
// a run never writes the shared file again.
func TestProviderAuxiliaryKeyMaterialWritesOnlyTheNodesKey(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", stateDir)
	owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
	extenderKeySeed, err := connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	keyMaterial := sdk.NewDeviceLocalKeyMaterial(nil, nil, nil)
	keyMaterial.SetExtenderKeySeed(extenderKeySeed)

	persistProviderAuxiliaryKeyMaterial(owner, 1, keyMaterial, connect.Id{})
	names, err := filepath.Glob(filepath.Join(stateDir, ".provider.extender*"))
	if err != nil || len(names) != 0 {
		t.Fatalf("a slot that runs no node wrote %v", names)
	}

	clientId := connect.NewId()
	persistProviderAuxiliaryKeyMaterial(owner, 0, keyMaterial, clientId)
	names, err = filepath.Glob(filepath.Join(stateDir, ".provider.extender*"))
	if err != nil || len(names) != 1 || filepath.Base(names[0]) != ".provider.extender."+clientId.String()+".key" {
		t.Fatalf("the node wrote %v, expected its own key file", names)
	}
	if seed, err := readProviderExtenderKeySeed(clientId); err != nil || !bytes.Equal(seed, extenderKeySeed) {
		t.Fatal("the node's key did not round trip", err)
	}
}

// The device's key material names the measurement key file in the provider
// state directory beside the extender seed, so the space, which keeps no local
// state, serves the lag.dev measurement api under one key across launches
// (lag.dev/PLAN.md C4). Without it no provider ever asked for a certificate.
// A slot that runs no node carries neither.
func TestProviderDeviceKeyMaterialNamesTheMeasurementKeyFile(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", stateDir)
	extenderKeySeed, err := connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	keyMaterial := providerDeviceKeyMaterial(nil, nil, nil, extenderKeySeed, true)
	if !bytes.Equal(keyMaterial.GetExtenderKeySeed(), extenderKeySeed) {
		t.Fatal("the key material did not carry the seed")
	}
	expected := filepath.Join(stateDir, ".provider.extender.measurement.key")
	if got := keyMaterial.GetExtenderMeasurementKeyFile(); got != expected {
		t.Fatalf("measurement key file = %q, expected %q", got, expected)
	}

	keyMaterial = providerDeviceKeyMaterial(nil, nil, nil, extenderKeySeed, false)
	if keyMaterial.GetExtenderKeySeed() != nil || keyMaterial.GetExtenderMeasurementKeyFile() != "" {
		t.Fatal("a slot that runs no node carries an extender identity")
	}
}

// A persisted seed still derives its identity; diagnostic observation does not
// expose it or make malformed material a successful identity observation.
func TestProviderExtenderIdentityIsReadableFromTheSeed(t *testing.T) {
	extenderKeySeed, err := connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := connect.ExtenderPublicKeyFromSeed(extenderKeySeed)
	if err != nil {
		t.Fatalf("the persisted seed does not derive a public key: %s", err)
	}
	if len(publicKey) == 0 {
		t.Fatal("the persisted seed derives an empty public key")
	}
	observeProviderExtenderIdentity(nil, 0, nil)
	observeProviderExtenderIdentity(nil, 0, []byte{1, 2, 3})
}

// Every swarm member runs with the provider extender role off: one process
// holds one extender identity and binds the carrier ports once (G1, G2).
func TestProviderSwarmMemberDisablesTheExtenderRole(t *testing.T) {
	if !sdk.DefaultDeviceLocalSettings().ProvideExtenderEnabled {
		t.Fatal("the sdk default is off, so the swarm setting proves nothing")
	}
	member := ProviderSwarmMember{ID: "miner-1", DNSPumpHost: "127.0.0.1"}
	deviceSettings := swarmMemberDeviceSettings(member, nil, nil)
	if deviceSettings.ProvideExtenderEnabled {
		t.Fatal("a swarm member runs the provider extender role")
	}
	if !deviceSettings.ClientSettings.ClientKeyRegistrationRequired {
		t.Fatal("a swarm member no longer requires client key registration")
	}
	if deviceSettings.DnsPumpHost != member.DNSPumpHost {
		t.Fatalf("dns pump host = %q, expected the member's", deviceSettings.DnsPumpHost)
	}
}

// One slot of a provider run asks for the provider extender role (G1, G2): every
// slot has the same extender key under a client of its own, and each slot that
// activated it took the operator's row from the one before. The other slots keep
// everything else the miner sets on a provider's device.
func TestProviderSlotDeviceSettingsAskForTheExtenderRoleInTheExtenderSlot(t *testing.T) {
	if !sdk.DefaultDeviceLocalSettings().ProvideExtenderEnabled {
		t.Fatal("the sdk default is off, so the slot setting proves nothing")
	}
	for _, domainHash := range [][32]byte{{}, {71}} {
		if !providerSlotDeviceSettings(domainHash, true).ProvideExtenderEnabled {
			t.Fatalf("domain %x: the extender slot does not run the extender role", domainHash[:1])
		}
		settings := providerSlotDeviceSettings(domainHash, false)
		if settings.ProvideExtenderEnabled {
			t.Fatalf("domain %x: another slot runs the extender role", domainHash[:1])
		}
		if !settings.AllowProvider {
			t.Fatalf("domain %x: a slot without the role does not provide", domainHash[:1])
		}
		if settings.ClientSettings.ContractManagerSettings.CloseReportDomainHash != domainHash {
			t.Fatalf("domain %x: a slot without the role lost its close report domain", domainHash[:1])
		}
	}
}

// The miner's provider extender tries udp 53 for its dns carrier beside udp
// 4053, which the sdk default leaves off for every app (connect/EXTENDER.md
// L2). Only the device settings the miner builds opt in.
func TestProviderDeviceSettingsOptTheExtenderInToTheDnsPrivilegedPort(t *testing.T) {
	if sdk.DefaultDeviceLocalSettings().ProvideExtenderDnsPrivilegedPort {
		t.Fatal("the sdk default binds 53, so the miner setting proves nothing")
	}
	for _, domainHash := range [][32]byte{{}, {71}} {
		settings := ProviderDeviceSettings(domainHash)
		if !settings.ProvideExtenderDnsPrivilegedPort {
			t.Fatalf("domain %x: the miner's extender does not try 53", domainHash[:1])
		}
		if !settings.ProvideExtenderEnabled {
			t.Fatalf("domain %x: the miner's extender role is off", domainHash[:1])
		}
	}
}

// Callback state keeps closed scalar facts, including unknown versus disabled.
// Repeated facts do not depend on arbitrary SDK error/address text.
func TestProviderExtenderStatusLine(t *testing.T) {
	cases := []struct {
		status *sdk.ExtenderProvideStatus
		expect providerExtenderObservation
	}{
		{status: nil, expect: providerExtenderObservation{}},
		{status: &sdk.ExtenderProvideStatus{}, expect: providerExtenderObservation{Known: true}},
		{
			status: &sdk.ExtenderProvideStatus{Enabled: true},
			expect: providerExtenderObservation{Known: true, Enabled: true},
		},
		{
			status: &sdk.ExtenderProvideStatus{
				Enabled:     true,
				Listening:   true,
				ActivatedV4: true,
				Ipv4:        "198.51.100.11",
			},
			expect: providerExtenderObservation{Known: true, Enabled: true, Listening: true, ActivatedV4: true},
		},
		{
			status: &sdk.ExtenderProvideStatus{
				Enabled:             true,
				Listening:           true,
				ListenError:         "dns: bind",
				ActivatedV4:         true,
				Ipv4:                "198.51.100.11",
				ActivatedV6:         true,
				Ipv6:                "2001:db8::11",
				LastActivationError: "refused",
			},
			expect: providerExtenderObservation{Known: true, Enabled: true, Listening: true, ListenFailed: true, ActivatedV4: true, ActivatedV6: true, ActivationFailed: true},
		},
	}
	for _, c := range cases {
		listener := newProviderExtenderStatusListener(nil, 0)
		listener.ExtenderProvideStatusChanged(c.status)
		if listener.last != c.expect {
			t.Errorf("scalar observation = %+v, expected %+v", listener.last, c.expect)
		}
	}
}
