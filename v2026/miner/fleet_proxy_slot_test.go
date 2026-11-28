package miner

// A proxy slot counts like any other device (owner decision 2026-11-21, which
// withdrew the rule of 2026-11-19 that a proxy slot shows no head-breadth
// unit). So `provider fleet sign` signs for one of this host's proxy slots as
// for a direct device, with no warning. The proxy files and slot credentials
// are this host's, in the provider state directory. Addresses are RFC 5737
// documentation addresses.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// An unverified client credential naming clientId, as the slot files hold one.
func fleetProxySlotTestJwt(t *testing.T, clientId [16]byte) string {
	t.Helper()
	claims, err := json.Marshal(map[string]string{
		"client_id": connect.Id(clientId).String(),
		"device_id": connect.NewId().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + encode(claims) + "." + encode([]byte("unverified"))
}

// Writes a proxy file of servers and each slot's credential naming its client
// into directory.
func writeFleetProxySlots(t *testing.T, directory string, slots map[string][16]byte) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	proxyConfig := ProxyConfig{Servers: map[string]string{}}
	for address, clientId := range slots {
		proxyConfig.Servers[address] = "key"
		name := providerClientJwtName(&connect.ProxySettings{Network: "tcp", Address: address})
		writeFleetSignFile(t, filepath.Join(directory, name), []byte(fleetProxySlotTestJwt(t, clientId)+"\n"))
	}
	raw, err := json.Marshal(&proxyConfig)
	if err != nil {
		t.Fatal(err)
	}
	writeFleetSignFile(t, filepath.Join(directory, "proxy"), raw)
}

// `provider fleet sign` for a proxy slot prints its payload and nothing on
// stderr, as for a direct device.
func TestFleetSignTreatsAProxySlotLikeAnyDevice(t *testing.T) {
	fixture := newFleetSignFixture(t)
	state := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", state)

	direct, directErr, err := fixture.run(t, "--client_id="+fixture.clientIdHex, "--client_seed_file="+fixture.clientSeedPath)
	if err != nil || direct != fleetSignGolden {
		t.Fatalf("a direct device: err %v, stdout %q, stderr %q", err, direct, directErr)
	}

	clientId := fixture.manifest.Members[0].ClientID
	writeFleetProxySlots(t, state, map[string][16]byte{"198.51.100.7:1080": clientId})
	stdout, stderr, err := fixture.run(t, "--client_id="+fixture.clientIdHex, "--client_seed_file="+fixture.clientSeedPath)
	if err != nil || stdout != fleetSignGolden {
		t.Fatalf("a proxy slot: err %v, stdout %q", err, stdout)
	}
	if stderr != directErr {
		t.Fatalf("a proxy slot's stderr %q differs from a direct device's %q", stderr, directErr)
	}
	fixture.assertNoSecrets(t, stdout, stderr)
}
