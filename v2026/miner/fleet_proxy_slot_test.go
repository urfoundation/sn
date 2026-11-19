package miner

// `provider fleet` warns, and never refuses, when the client it binds is one
// of this host's proxy slots: a proxy slot shows no head-breadth unit
// (WHITEPAPER.md §8.4), so binding it only gives up its pool share. The proxy
// files and slot credentials are this host's, in the provider state directory
// and below it in operators/<domain>. Addresses are RFC 5737 documentation
// addresses.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// A proxy slot of the state directory, and one of an operator directory, are
// warned about; the direct slot and a client of no slot here are not.
func TestFleetWarnProxySlot(t *testing.T) {
	state := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", state)
	direct := [16]byte{0xd1}
	proxied := [16]byte{0xa1}
	operatorProxied := [16]byte{0xa2}
	elsewhere := [16]byte{0xe1}
	writeFleetSignFile(t, filepath.Join(state, ".provider.jwt"), []byte(fleetProxySlotTestJwt(t, direct)))
	writeFleetProxySlots(t, state, map[string][16]byte{"198.51.100.7:1080": proxied})
	writeFleetProxySlots(t, filepath.Join(state, "operators", "operator.example"), map[string][16]byte{"203.0.113.9:1080": operatorProxied})

	warning := func(clientId [16]byte) string {
		var out strings.Builder
		fleetWarnProxySlot(&out, clientId)
		return out.String()
	}
	for _, c := range []struct {
		name     string
		clientId [16]byte
		address  string
	}{
		{"a proxy slot", proxied, "198.51.100.7:1080"},
		{"an operator directory's proxy slot", operatorProxied, "203.0.113.9:1080"},
	} {
		out := warning(c.clientId)
		if !strings.HasPrefix(out, "warning: ") || !strings.Contains(out, "is a proxy slot of this host") || !strings.Contains(out, "no head-breadth unit") || !strings.Contains(out, c.address) {
			t.Errorf("%s: warning %q", c.name, out)
		}
	}
	for _, clientId := range [][16]byte{direct, elsewhere} {
		if out := warning(clientId); out != "" {
			t.Errorf("client 0x%x is no proxy slot, warned %q", clientId, out)
		}
	}
}

// `provider fleet sign` for a proxy slot warns on stderr and still prints its
// payload.
func TestFleetSignWarnsForAProxySlotAndStillSigns(t *testing.T) {
	fixture := newFleetSignFixture(t)
	state := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", state)

	stdout, stderr, err := fixture.run(t, "--client_id="+fixture.clientIdHex, "--client_seed_file="+fixture.clientSeedPath)
	if err != nil || stdout != fleetSignGolden || strings.Contains(stderr, "proxy slot") {
		t.Fatalf("a direct device: err %v, stdout %q, stderr %q", err, stdout, stderr)
	}

	clientId := fixture.manifest.Members[0].ClientID
	writeFleetProxySlots(t, state, map[string][16]byte{"198.51.100.7:1080": clientId})
	stdout, stderr, err = fixture.run(t, "--client_id="+fixture.clientIdHex, "--client_seed_file="+fixture.clientSeedPath)
	if err != nil || stdout != fleetSignGolden {
		t.Fatalf("a proxy slot was refused: err %v, stdout %q", err, stdout)
	}
	if !strings.Contains(stderr, "is a proxy slot of this host") {
		t.Fatalf("no warning for a proxy slot: stderr %q", stderr)
	}
	fixture.assertNoSecrets(t, stdout, stderr)
}
