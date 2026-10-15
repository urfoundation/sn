// `provider fleet sign` must print exactly the payload the ur.io Top 200 page
// pastes into POST /sn/head/binding, signed so the server's own parser and
// signature checks accept it, without ever printing seed material (support
// inbox 7934: a Linux CLI provider had no way to make the device signature).

package miner

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/sdk/v2026"
	servercontroller "github.com/urnetwork/server/v2026/controller"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

type fleetSignFixture struct {
	dir             string
	manifest        *protocol.FleetManifest
	manifestPath    string
	clientSeed      []byte
	hotkeySeed      []byte
	otherSeed       []byte
	clientSeedPath  string
	hotkeySeedPath  string
	otherSeedPath   string
	clientIdHex     string
	otherClientId   [16]byte
	otherClientHex  string
	clientPublicKey ed25519.PublicKey
}

func fleetSignSeed(start byte) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = start + byte(i)
	}
	return seed
}

func writeFleetSignFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newFleetSignFixture(t *testing.T) *fleetSignFixture {
	t.Helper()
	self := &fleetSignFixture{dir: t.TempDir()}
	self.clientSeed = fleetSignSeed(0x01)
	self.hotkeySeed = fleetSignSeed(0x41)
	self.otherSeed = fleetSignSeed(0x81)
	self.clientPublicKey = ed25519.NewKeyFromSeed(self.clientSeed).Public().(ed25519.PublicKey)
	hotkey, err := crv4.KeypairFromSeed([32]byte(self.hotkeySeed))
	if err != nil {
		t.Fatal(err)
	}
	clientId := [16]byte{0x01, 0x9b, 0x2e, 0x10, 0x5a, 0x7c, 0x4d, 0x3e, 0x8f, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd}
	self.otherClientId = [16]byte{0x01, 0x9b, 0x2e, 0x10, 0x5a, 0x7c, 0x4d, 0x3e, 0x8f, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xce}
	self.clientIdHex = "0x" + hex.EncodeToString(clientId[:])
	self.otherClientHex = "0x" + hex.EncodeToString(self.otherClientId[:])
	manifest := &protocol.FleetManifest{
		Schema:     protocol.FleetManifestSchema,
		ChainID:    945,
		Netuid:     25,
		Hotkey:     hotkey.PublicKey(),
		Generation: 3,
	}
	for i := range manifest.Coordinator {
		manifest.Coordinator[i] = 0xc0 + byte(i)
	}
	for i := range manifest.FleetID {
		manifest.FleetID[i] = 0xf0 ^ byte(i)
	}
	otherPublicKey := ed25519.NewKeyFromSeed(self.otherSeed).Public().(ed25519.PublicKey)
	manifest.Members = []protocol.FleetMember{
		{ClientID: clientId, ClientKey: [32]byte(self.clientPublicKey)},
		{ClientID: self.otherClientId, ClientKey: [32]byte(otherPublicKey)},
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	self.manifestPath = filepath.Join(self.dir, "fleet.json")
	writeFleetSignFile(t, self.manifestPath, canonical)
	// fleetCommand's own loader must accept the manifest the test signs from
	self.manifest, err = loadFleetManifest(self.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// raw client seed like ~/.urnetwork/.provider.key; hex hotkey seed
	self.clientSeedPath = filepath.Join(self.dir, ".provider.key")
	writeFleetSignFile(t, self.clientSeedPath, self.clientSeed)
	self.hotkeySeedPath = filepath.Join(self.dir, "hotkey.seed")
	writeFleetSignFile(t, self.hotkeySeedPath, []byte("0x"+hex.EncodeToString(self.hotkeySeed)+"\n"))
	self.otherSeedPath = filepath.Join(self.dir, "other.key")
	writeFleetSignFile(t, self.otherSeedPath, self.otherSeed)
	return self
}

// run parses argv with the production usage and runs `provider fleet sign`.
func (self *fleetSignFixture) run(t *testing.T, argv ...string) (string, string, error) {
	t.Helper()
	opts := parseArgsForTest(t, append([]string{"fleet", "sign", "--manifest=" + self.manifestPath, "--valid_from_epoch=100", "--valid_to_epoch=200"}, argv...))
	if sign, _ := opts.Bool("sign"); !sign {
		t.Fatal("usage did not select fleet sign")
	}
	var stdout, stderr bytes.Buffer
	err := fleetSign(opts, self.manifest, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// secrets are every encoding of seed and private key material the command
// could leak: raw bytes and hex of each seed and each Ed25519 private key.
func (self *fleetSignFixture) secrets() []string {
	var out []string
	for _, seed := range [][]byte{self.clientSeed, self.hotkeySeed, self.otherSeed} {
		out = append(out, string(seed), hex.EncodeToString(seed), strings.ToUpper(hex.EncodeToString(seed)))
	}
	for _, seed := range [][]byte{self.clientSeed, self.otherSeed} {
		private := ed25519.NewKeyFromSeed(seed)
		out = append(out, string(private), hex.EncodeToString(private))
	}
	return out
}

func (self *fleetSignFixture) assertNoSecrets(t *testing.T, outputs ...string) {
	t.Helper()
	for _, output := range outputs {
		lower := strings.ToLower(output)
		for _, secret := range self.secrets() {
			if strings.Contains(output, secret) || strings.Contains(lower, strings.ToLower(secret)) {
				t.Fatalf("output contains seed or private key material: %q", output)
			}
		}
	}
}

// fleetSignGolden is the payload for the fixture's seeds and binding fields.
// Ed25519 is deterministic, so client_signature is fixed too.
const fleetSignGolden = `{"binding":{"chain_id":945,"netuid":25,"coordinator":"0xc0c1c2c3c4c5c6c7c8c9cacbcccdcecfd0d1d2d3","fleet_id":"0xf0f1f2f3f4f5f6f7f8f9fafbfcfdfeffe0e1e2e3e4e5e6e7e8e9eaebecedeeef","hotkey":"0x0a1c77edb48519069bbb154bf088c7f17678839de458e0679370ae989af3f55c","client_id":"019b2e10-5a7c-4d3e-8f01-23456789abcd","client_key":"0x79b5562e8fe654f94078b112e8a98ba7901f853ae695bed7e0e3910bad049664","generation":3,"valid_from_epoch":100,"valid_to_epoch":200,"commitment_hash":"0x9e1d4fa4a979f45cf003f26d826837250b08b45b8e896f98316cfc60005d3471"},"client_signature":"0x8a3e5b990f1a280f0eef9455c81dfbfd251a5ac5a9bc021770cf0a4dee1b4aafab04762d6ca8b587c1e04642d46cff87844b89aefcaddf26700fdbc849efbc0a"}` + "\n"

// The printed payload is byte-for-byte the golden, is what `provider fleet
// bind` signs, and passes the server's POST /sn/head/binding parser and
// signature check (and the sdk's binding parser) unchanged.
func TestFleetSignGoldenPayloadVerifiesWithServerParser(t *testing.T) {
	fixture := newFleetSignFixture(t)
	stdout, stderr, err := fixture.run(t, "--client_id="+fixture.clientIdHex, "--client_seed_file="+fixture.clientSeedPath)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != fleetSignGolden {
		t.Fatalf("payload differs from the golden\n got: %s\nwant: %s", stdout, fleetSignGolden)
	}
	fixture.assertNoSecrets(t, stdout, stderr)

	// the page's parsePayload: one json object with binding and client_signature
	var page struct {
		Binding         map[string]any `json:"binding"`
		ClientSignature string         `json:"client_signature"`
		HotkeySignature *string        `json:"hotkey_signature"`
	}
	if err := json.Unmarshal([]byte(stdout), &page); err != nil || page.Binding == nil || page.ClientSignature == "" || page.HotkeySignature != nil {
		t.Fatalf("page would not parse the payload: %+v, %v", page, err)
	}

	// the server's request struct and binding parser, then its signature check
	var args servercontroller.SnHeadBindingArgs
	decoder := json.NewDecoder(strings.NewReader(stdout))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		t.Fatal(err)
	}
	binding, err := servercontroller.SnFleetBindingFromJson(&args.Binding)
	if err != nil {
		t.Fatal(err)
	}
	clientSignature, err := hex.DecodeString(strings.TrimPrefix(args.ClientSignature, "0x"))
	if err != nil || !binding.VerifyClient(clientSignature) {
		t.Fatalf("server rejects client_signature: %v", err)
	}

	// `provider fleet bind` builds and client-signs the identical binding
	bindOpts := parseArgsForTest(t, []string{"fleet", "bind", "--manifest=" + fixture.manifestPath, "--client_id=" + fixture.clientIdHex, "--client_seed_file=" + fixture.clientSeedPath, "--hotkey_seed_file=" + fixture.hotkeySeedPath, "--valid_from_epoch=100", "--valid_to_epoch=200", "--rpc=http://127.0.0.1:1", "--relayer_key_file=/nonexistent"})
	bindBinding, bindClientSignature, bindHotkeySignature, err := fleetBindingAndSign(bindOpts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if bindBinding != binding || !bytes.Equal(bindClientSignature, clientSignature) || !bindBinding.VerifyHotkey(bindHotkeySignature) {
		t.Fatal("fleet sign and fleet bind disagree on the binding or client signature")
	}

	// the sdk parser yields the same digest, which is the one on stderr
	bindingJson, _ := json.Marshal(args.Binding)
	sdkDigest, err := sdk.SnFleetBindingDigest(string(bindingJson))
	if err != nil {
		t.Fatal(err)
	}
	rawJson, _ := json.Marshal(page.Binding)
	if sdkDigestRaw, err := sdk.SnFleetBindingDigest(string(rawJson)); err != nil || sdkDigestRaw != sdkDigest {
		t.Fatalf("sdk parses the printed binding differently: %s, %v", sdkDigestRaw, err)
	}
	digest, _ := binding.Digest()
	if sdkDigest != "0x"+hex.EncodeToString(digest[:]) || !strings.Contains(stderr, "binding digest: "+sdkDigest+"\n") {
		t.Fatalf("digest mismatch: sdk %s, stderr %q", sdkDigest, stderr)
	}
}

// With the hotkey seed the payload carries a hotkey_signature the server
// verifies, the same value is printed for the page's hotkey field, and the
// member is found from the client key when --client_id is omitted.
func TestFleetSignWithHotkeyFindsMemberAndVerifies(t *testing.T) {
	fixture := newFleetSignFixture(t)
	stdout, stderr, err := fixture.run(t, "--client_seed_file="+fixture.clientSeedPath, "--hotkey_seed_file="+fixture.hotkeySeedPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.assertNoSecrets(t, stdout, stderr)
	if strings.Count(stdout, "\n") != 1 || !strings.HasPrefix(stdout, `{"binding":`) {
		t.Fatalf("stdout is not one payload line: %q", stdout)
	}
	var args servercontroller.SnHeadBindingArgs
	if err := json.Unmarshal([]byte(stdout), &args); err != nil {
		t.Fatal(err)
	}
	binding, err := servercontroller.SnFleetBindingFromJson(&args.Binding)
	if err != nil {
		t.Fatal(err)
	}
	if args.Binding.ClientId.String() != "019b2e10-5a7c-4d3e-8f01-23456789abcd" {
		t.Fatalf("wrong member selected: %s", args.Binding.ClientId)
	}
	clientSignature, _ := hex.DecodeString(strings.TrimPrefix(args.ClientSignature, "0x"))
	hotkeySignature, _ := hex.DecodeString(strings.TrimPrefix(args.HotkeySignature, "0x"))
	if !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
		t.Fatal("server rejects a signature")
	}
	if !regexp.MustCompile(`^0x[0-9a-f]{128}$`).MatchString(args.HotkeySignature) || !strings.Contains(stderr, "hotkey signature (already in the payload): "+args.HotkeySignature+"\n") {
		t.Fatalf("hotkey signature not printed for the page's hotkey field: %q", stderr)
	}
	// the client half without --client_id equals the golden's
	golden, _, err := fixture.run(t, "--client_id="+fixture.clientIdHex, "--client_seed_file="+fixture.clientSeedPath)
	if err != nil {
		t.Fatal(err)
	}
	var goldenArgs servercontroller.SnHeadBindingArgs
	if err := json.Unmarshal([]byte(golden), &goldenArgs); err != nil || goldenArgs.Binding != args.Binding || goldenArgs.ClientSignature != args.ClientSignature {
		t.Fatalf("member lookup by key built a different binding: %v", err)
	}
}

// With only the hotkey seed (signed on the machine that holds it), stdout is
// just the 0x signature the page's hotkey signature field takes.
func TestFleetSignHotkeyOnlyPrintsFieldValue(t *testing.T) {
	fixture := newFleetSignFixture(t)
	stdout, stderr, err := fixture.run(t, "--client_id="+fixture.clientIdHex, "--hotkey_seed_file="+fixture.hotkeySeedPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.assertNoSecrets(t, stdout, stderr)
	value := strings.TrimSuffix(stdout, "\n")
	if !regexp.MustCompile(`^0x[0-9a-f]{128}$`).MatchString(value) {
		t.Fatalf("stdout is not a bare hotkey signature: %q", stdout)
	}
	var args servercontroller.SnHeadBindingArgs
	if err := json.Unmarshal([]byte(strings.Replace(fleetSignGoldenFor(t, fixture), "\n", "", 1)), &args); err != nil {
		t.Fatal(err)
	}
	args.HotkeySignature = value
	binding, err := servercontroller.SnFleetBindingFromJson(&args.Binding)
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := hex.DecodeString(strings.TrimPrefix(args.HotkeySignature, "0x"))
	if !binding.VerifyHotkey(signature) {
		t.Fatal("server rejects the hotkey-only signature for the device's binding")
	}
}

func fleetSignGoldenFor(t *testing.T, fixture *fleetSignFixture) string {
	t.Helper()
	stdout, _, err := fixture.run(t, "--client_id="+fixture.clientIdHex, "--client_seed_file="+fixture.clientSeedPath)
	if err != nil {
		t.Fatal(err)
	}
	return stdout
}

// Missing and invalid inputs fail before anything is printed to stdout, and
// no error echoes seed material (including a malformed seed file's bytes).
func TestFleetSignRefusesMissingOrInvalidInputs(t *testing.T) {
	fixture := newFleetSignFixture(t)
	shortSeedPath := filepath.Join(fixture.dir, "short.key")
	// 31 bytes of the client seed, hex: a near-miss that must not be echoed
	writeFleetSignFile(t, shortSeedPath, []byte(hex.EncodeToString(fixture.clientSeed[:31])))
	shortHotkeyPath := filepath.Join(fixture.dir, "short-hotkey.seed")
	writeFleetSignFile(t, shortHotkeyPath, []byte(hex.EncodeToString(fixture.hotkeySeed[:31])))
	strangerSeedPath := filepath.Join(fixture.dir, "stranger.key")
	writeFleetSignFile(t, strangerSeedPath, fleetSignSeed(0xa1))
	for _, item := range []struct {
		name string
		argv []string
		want string
	}{
		{"no seed", []string{"--client_id=" + fixture.clientIdHex}, "needs --client_seed_file, --hotkey_seed_file or both"},
		{"hotkey only without client id", []string{"--hotkey_seed_file=" + fixture.hotkeySeedPath}, "--client_id is required"},
		{"client id not hex", []string{"--client_id=zz", "--client_seed_file=" + fixture.clientSeedPath}, "16-byte hex"},
		{"client id not in manifest", []string{"--client_id=0x" + strings.Repeat("ab", 16), "--client_seed_file=" + fixture.clientSeedPath}, "not in the manifest"},
		{"client seed of another member", []string{"--client_id=" + fixture.otherClientHex, "--client_seed_file=" + fixture.clientSeedPath}, "does not match binding"},
		{"client seed of no member", []string{"--client_seed_file=" + strangerSeedPath}, "no manifest member has the client key"},
		{"missing client seed file", []string{"--client_seed_file=" + filepath.Join(fixture.dir, "absent.key")}, "absent.key"},
		{"malformed client seed", []string{"--client_seed_file=" + shortSeedPath}, "expected raw or hex 32-byte Ed25519 seed"},
		{"wrong hotkey seed", []string{"--client_seed_file=" + fixture.clientSeedPath, "--hotkey_seed_file=" + fixture.otherSeedPath}, "hotkey seed does not match manifest"},
		{"malformed hotkey seed", []string{"--client_id=" + fixture.clientIdHex, "--hotkey_seed_file=" + shortHotkeyPath}, "seed must be 32 bytes"},
	} {
		t.Run(item.name, func(t *testing.T) {
			stdout, stderr, err := fixture.run(t, item.argv...)
			if err == nil || !strings.Contains(err.Error(), item.want) {
				t.Fatalf("err = %v, want %q", err, item.want)
			}
			if stdout != "" {
				t.Fatalf("stdout written on failure: %q", stdout)
			}
			fixture.assertNoSecrets(t, stdout, stderr, err.Error())
			for _, partial := range []string{hex.EncodeToString(fixture.clientSeed[:31]), hex.EncodeToString(fixture.hotkeySeed[:31])} {
				if strings.Contains(err.Error(), partial) {
					t.Fatalf("error echoes seed bytes: %v", err)
				}
			}
		})
	}

	// invalid epochs come from the shared binding builder
	opts := parseArgsForTest(t, []string{"fleet", "sign", "--manifest=" + fixture.manifestPath, "--client_seed_file=" + fixture.clientSeedPath, "--valid_from_epoch=200", "--valid_to_epoch=100"})
	var stdout bytes.Buffer
	if err := fleetSign(opts, fixture.manifest, &stdout, &bytes.Buffer{}); err == nil || stdout.Len() != 0 {
		t.Fatalf("inverted validity accepted: %v", err)
	}
	opts = parseArgsForTest(t, []string{"fleet", "sign", "--manifest=" + fixture.manifestPath, "--client_seed_file=" + fixture.clientSeedPath, "--valid_from_epoch=soon", "--valid_to_epoch=100"})
	if err := fleetSign(opts, fixture.manifest, &stdout, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--valid_from_epoch") || stdout.Len() != 0 {
		t.Fatalf("non-numeric epoch accepted: %v", err)
	}
}

// The usage requires the manifest and the validity window, and takes no RPC,
// relayer key or other network option.
func TestFleetSignUsageNeedsNoNetwork(t *testing.T) {
	parse := func(argv ...string) error {
		_, err := parseArgsForTestErr(argv)
		return err
	}
	if err := parse("fleet", "sign", "--client_seed_file=k", "--valid_from_epoch=1", "--valid_to_epoch=2"); err == nil {
		t.Fatal("fleet sign parsed without --manifest")
	}
	if err := parse("fleet", "sign", "--manifest=m", "--client_seed_file=k", "--valid_from_epoch=1"); err == nil {
		t.Fatal("fleet sign parsed without --valid_to_epoch")
	}
	for _, network := range []string{"--rpc=http://127.0.0.1:1", "--relayer_key_file=r", "--substrate=ws://127.0.0.1:1", "--api_url=https://api.example"} {
		if err := parse("fleet", "sign", "--manifest=m", "--client_seed_file=k", "--valid_from_epoch=1", "--valid_to_epoch=2", network); err == nil {
			t.Fatalf("fleet sign accepted network option %s", network)
		}
	}
}

func parseArgsForTestErr(argv []string) (docopt.Opts, error) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	return parser.ParseArgs(mainUsage(), argv, "test")
}
