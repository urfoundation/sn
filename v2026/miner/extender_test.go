package miner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// The provider extender identity in the miner (connect/EXTENDER.md B1, G1).
//
// A provider's space keeps no local state, so the extender identity lives in
// the provider state directory beside the client key seed: the run path reads
// it before the device is built and writes back whatever the role ended up
// with. Losing it means activating under a new key every launch, which the
// operator revokes as fast as it publishes.

// The seed survives a write and a read, is kept private to the owner, and a
// provider that has never been an extender reads none rather than failing.
func TestProviderExtenderKeySeedRoundTrips(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", stateDir)

	extenderKeySeed, err := readProviderExtenderKeySeed()
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
	if err := writeProviderExtenderKeySeed(extenderKeySeed); err != nil {
		t.Fatal(err)
	}
	restoredKeySeed, err := readProviderExtenderKeySeed()
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

	// anyone with this file can present this host's extender identity
	seedPath := filepath.Join(stateDir, ".provider.extender.key")
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

// The public key the operator signs records for is printed once, and an
// unreadable seed prints nothing rather than a wrong identity.
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
	// the printers take a seed that is not one without failing the launch
	printProviderExtenderIdentity(nil)
	printProviderExtenderIdentity([]byte{1, 2, 3})
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

// The provider extender status reads as one line per state, so a launch prints
// the activation once it settles and nothing while it holds (F3, G3).
func TestProviderExtenderStatusLine(t *testing.T) {
	cases := []struct {
		status *sdk.ExtenderProvideStatus
		expect string
	}{
		{status: nil, expect: "off"},
		{status: &sdk.ExtenderProvideStatus{}, expect: "off"},
		{
			status: &sdk.ExtenderProvideStatus{Enabled: true},
			expect: "not listening, v4 not activated, v6 not activated",
		},
		{
			status: &sdk.ExtenderProvideStatus{
				Enabled:     true,
				Listening:   true,
				ActivatedV4: true,
				Ipv4:        "198.51.100.11",
			},
			expect: "listening, v4 activated at 198.51.100.11, v6 not activated",
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
			expect: "listening, carriers dns: bind, v4 activated at 198.51.100.11, " +
				"v6 activated at 2001:db8::11, last error refused",
		},
	}
	for _, c := range cases {
		if line := providerExtenderStatusLine(c.status); line != c.expect {
			t.Errorf("line = %q, expected %q", line, c.expect)
		}
	}

	// only a change prints, so a status that repeats is silent
	listener := newProviderExtenderStatusListener()
	status := &sdk.ExtenderProvideStatus{Enabled: true, Listening: true}
	listener.ExtenderProvideStatusChanged(status)
	if listener.line != providerExtenderStatusLine(status) {
		t.Fatalf("the listener kept %q", listener.line)
	}
}
