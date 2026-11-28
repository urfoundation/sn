package crv4

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hotkey as btcli writes it: unencrypted JSON whose secretSeed is the
// sr25519 mini secret, beside the public fields that seed derives.
func aliceBittensorKeyfile(overrides map[string]string) string {
	fields := map[string]string{
		"accountId":    `"0x` + alicePubkeyHex + `"`,
		"publicKey":    `"0x` + alicePubkeyHex + `"`,
		"privateKey":   `"0x` + strings.Repeat("ab", 64) + `"`,
		"secretPhrase": `"bottom drive obey lake curtain smoke basket hold race lonely fit walk"`,
		"secretSeed":   `"0x` + aliceSeedHex + `"`,
		"ss58Address":  `"` + aliceAddress42 + `"`,
	}
	for name, value := range overrides {
		if value == "" {
			delete(fields, name)
		} else {
			fields[name] = value
		}
	}
	parts := []string{}
	for _, name := range []string{"accountId", "publicKey", "privateKey", "secretPhrase", "secretSeed", "ss58Address"} {
		if value, ok := fields[name]; ok {
			parts = append(parts, `"`+name+`": `+value)
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// The wallet's own hotkey file is the seed file: its secretSeed signs, and its
// public fields must name the same key. Before, only raw and hex seeds loaded.
func TestSeedFileAcceptsBittensorKeyfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wallet", "hotkeys")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "default")
	if err := os.WriteFile(path, []byte(aliceBittensorKeyfile(nil)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seed, err := LoadSeedFile(path)
	if err != nil {
		t.Fatalf("Bittensor keyfile: %v", err)
	}
	keypair, err := KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	if keypair.Address() != aliceAddress42 {
		t.Fatalf("keyfile derived %s, want alice", keypair.Address())
	}
}

// A keyfile that cannot sign, or whose public fields name another key, is
// refused with a cause the operator can act on, never loaded as some key.
func TestSeedFileRefusesUnusableBittensorKeyfiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	other := strings.Repeat("11", 32)
	for name, test := range map[string]struct {
		contents string
		want     string
	}{
		"encrypted":        {contents: "$NACL" + strings.Repeat("x", 64), want: "encrypted Bittensor keyfile"},
		"legacy encrypted": {contents: "gAAAAAB" + strings.Repeat("y", 64), want: "encrypted Bittensor keyfile"},
		"public only":      {contents: aliceBittensorKeyfile(map[string]string{"secretSeed": "", "privateKey": "", "secretPhrase": ""}), want: "has no secretSeed"},
		"other public key": {contents: aliceBittensorKeyfile(map[string]string{"publicKey": `"0x` + other + `"`}), want: "publicKey differs"},
		"other account":    {contents: aliceBittensorKeyfile(map[string]string{"accountId": `"0x` + other + `"`}), want: "accountId differs"},
		"other address":    {contents: aliceBittensorKeyfile(map[string]string{"ss58Address": `"5FHneW46xGXgs5mUiveU4sbTyGBzmstUspZC92UhjJM694ty"`}), want: "ss58Address differs"},
		"short seed":       {contents: aliceBittensorKeyfile(map[string]string{"secretSeed": `"0x1234"`}), want: "secretSeed"},
		"malformed":        {contents: `{"secretSeed": `, want: "not a readable Bittensor keyfile"},
		"two documents":    {contents: aliceBittensorKeyfile(nil) + aliceBittensorKeyfile(nil), want: "more than one"},
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if seed, err := LoadSeedFile(path); err == nil || !strings.Contains(err.Error(), test.want) || seed != ([32]byte{}) {
			t.Errorf("%s: got %x, %v; want an error naming %q", name, seed, err, test.want)
		}
	}
}
