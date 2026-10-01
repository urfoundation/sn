package miner

// sn_wallet_test.go — the signed `provider wallet set` (sn_wallet.go) against
// a fake api: challenge → sign → set, the externally-signed form, the seed /
// address mismatch refusal, the unsigned fallback's error, and the printed
// challenge. Plus the signing-format test that pins the CLI to what the
// server verifies (server/model/auth_bittensor.go VerifyBittensorSignature,
// covered on that side by TestVerifyBittensorSignatureKnownKeypairCliFormat
// with the same known keypair).

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Alice, the substrate dev account: a published sr25519 vector (mini secret,
// public key, ss58 under prefix 42), shared with
// server/model/auth_bittensor_test.go and ur.io's tests/sr25519.test.mjs.
const (
	testAliceSeedHex      = "e5be9a5092b81bca64be81d212e7f2f9eba183bb7a90954f7b76361f6edb5c0a"
	testAlicePublicKeyHex = "d43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d"
	testAliceAddress      = "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"
	// server/model FormatWalletAuthChallengeMessage with a fixed challenge
	testChallengeMessage = "Sign in to URnetwork\nChallenge: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nTimestamp: 1700000000"
)

func testAliceKeypair(t *testing.T) *crv4.Keypair {
	t.Helper()
	keypair, err := crv4.KeypairFromSeedHex(testAliceSeedHex)
	if err != nil {
		t.Fatal(err)
	}
	return keypair
}

// writeTestSeedFile provisions a seed file the way an operator does: a
// private regular file under a private directory (crv4.LoadSeedFile refuses
// anything looser).
func writeTestSeedFile(t *testing.T, content string) string {
	t.Helper()
	// crv4 refuses symlinked path components; macOS temp dirs live under
	// /var -> /private/var
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "keys")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "coldkey.seed")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// setTestNetworkJwt points the provider state dir at a temp dir holding the
// network jwt `provider auth` would have written.
func setTestNetworkJwt(t *testing.T, jwt string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "jwt"), []byte(jwt+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeSnApi stands in for the two routes a wallet set uses.
type fakeSnApi struct {
	server *httptest.Server

	mu                sync.Mutex
	challengeRequests []map[string]any
	challengeAuth     []string
	walletRequests    []map[string]any
	walletAuth        []string
	// walletError, when set, is the server's refusal of the set
	walletError string
}

func newFakeSnApi(t *testing.T) *fakeSnApi {
	t.Helper()
	api := &fakeSnApi{}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		api.mu.Lock()
		defer api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /auth/wallet-challenge":
			api.challengeRequests = append(api.challengeRequests, request)
			api.challengeAuth = append(api.challengeAuth, r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"challenge":        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
				"timestamp":        1700000000,
				"expires_in":       300,
				"message_template": testChallengeMessage,
			})
		case "POST /sn/wallet":
			api.walletRequests = append(api.walletRequests, request)
			api.walletAuth = append(api.walletAuth, r.Header.Get("Authorization"))
			if api.walletError != "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": api.walletError}})
				return
			}
			_, _ = w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.server.Close)
	return api
}

func testClientStrategy(t *testing.T) (context.Context, *connect.ClientStrategy) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	t.Cleanup(func() {
		clientStrategy.Close()
		cancel()
	})
	return ctx, clientStrategy
}

func stringField(t *testing.T, request map[string]any, key string) string {
	t.Helper()
	value, _ := request[key].(string)
	return value
}

// TestSnWalletSigningFormat pins the bytes the CLI signs and the encoding it
// sends, exactly what the server verifies: an sr25519 signature in the
// "substrate" signing context over the <Bytes>…</Bytes>-wrapped UTF-8
// challenge text (the polkadot-js signRaw "bytes" form), as 0x-prefixed hex
// of 64 bytes; and the key derivation subkey / polkadot-js / btcli use, so the
// address an operator sees in btcli is the one the seed file derives here.
func TestSnWalletSigningFormat(t *testing.T) {
	keypair := testAliceKeypair(t)
	if keypair.Address() != testAliceAddress {
		t.Fatalf("Alice derives %s, want %s", keypair.Address(), testAliceAddress)
	}
	publicKey := keypair.PublicKey()
	if hex.EncodeToString(publicKey[:]) != testAlicePublicKeyHex {
		t.Fatalf("Alice public key %x", publicKey)
	}

	if got := string(snWrapBytes(testChallengeMessage)); got != "<Bytes>"+testChallengeMessage+"</Bytes>" {
		t.Fatalf("wrapped bytes %q", got)
	}

	signatureHex, err := snSignWalletChallenge(keypair, testChallengeMessage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(signatureHex, "0x") || len(signatureHex) != 2+128 {
		t.Fatalf("signature hex %q: want 0x + 128 hex chars", signatureHex)
	}
	signature, err := hex.DecodeString(signatureHex[2:])
	if err != nil || len(signature) != 64 {
		t.Fatalf("signature decode: %v (%d bytes)", err, len(signature))
	}
	// schnorrkel's signature marker, required by the server's Signature.Decode
	if signature[63]&0x80 == 0 {
		t.Fatalf("signature lacks the schnorrkel marker bit")
	}
	// verifies over the wrapped form (the server tries it first) ...
	if !keypair.Verify(snWrapBytes(testChallengeMessage), signature) {
		t.Fatalf("signature does not verify over the <Bytes>-wrapped challenge")
	}
	// ... and not over the raw text or another challenge: the two payloads are
	// distinct, which is why the server checks both forms
	if keypair.Verify([]byte(testChallengeMessage), signature) {
		t.Fatalf("a wrapped-form signature verified over the raw text")
	}
	if keypair.Verify(snWrapBytes(strings.Replace(testChallengeMessage, "1700000000", "1700000001", 1)), signature) {
		t.Fatalf("signature verified over a different challenge")
	}
	// sr25519 signatures are randomized: a second signature differs and still verifies
	again, err := snSignWalletChallenge(keypair, testChallengeMessage)
	if err != nil {
		t.Fatal(err)
	}
	if again == signatureHex {
		t.Fatalf("two sr25519 signatures were identical")
	}
	againBytes, _ := hex.DecodeString(again[2:])
	if !keypair.Verify(snWrapBytes(testChallengeMessage), againBytes) {
		t.Fatalf("second signature does not verify")
	}
}

func TestSnWalletMessageAndSignatureForms(t *testing.T) {
	oneLine := snEscapeMessage(testChallengeMessage)
	if strings.Contains(oneLine, "\n") || !strings.Contains(oneLine, `\n`) {
		t.Fatalf("escaped message %q", oneLine)
	}
	if snUnescapeMessage(oneLine) != testChallengeMessage {
		t.Fatalf("escape round trip: %q", snUnescapeMessage(oneLine))
	}
	// a message passed with real newlines is left alone
	if snUnescapeMessage(testChallengeMessage) != testChallengeMessage {
		t.Fatalf("multi-line message changed")
	}

	raw := strings.Repeat("ab", 64)
	for _, in := range []string{raw, "0x" + raw, "0X" + raw, "  " + raw + "\n"} {
		got, err := snNormalizeSignatureHex(in)
		if err != nil || got != "0x"+raw {
			t.Fatalf("normalize %q: %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "0x", "zz", raw[:126], raw + "ab", "0x" + raw + "0"} {
		if _, err := snNormalizeSignatureHex(bad); err == nil {
			t.Fatalf("normalize %q: no error", bad)
		}
	}
}

func TestSnWalletProofFromOpts(t *testing.T) {
	proof, err := snWalletProofFromOpts(parseArgsForTest(t, []string{"wallet", "set", testAliceAddress}))
	if err != nil || !proof.unsigned() {
		t.Fatalf("bare set: %+v, %v", proof, err)
	}
	proof, err = snWalletProofFromOpts(parseArgsForTest(t, []string{"wallet", "set", testAliceAddress, "--coldkey_seed_file=/k"}))
	if err != nil || proof.SeedFile != "/k" || proof.unsigned() {
		t.Fatalf("seed file: %+v, %v", proof, err)
	}
	proof, err = snWalletProofFromOpts(parseArgsForTest(t, []string{"wallet", "set", testAliceAddress, "--message=m", "--signature=s"}))
	if err != nil || proof.Message != "m" || proof.Signature != "s" || proof.unsigned() {
		t.Fatalf("message+signature: %+v, %v", proof, err)
	}
	// the option grammar refuses a lone --message or --signature and the two
	// forms together; the Go-side guard says the same
	var nilProof *snWalletProof
	if !nilProof.unsigned() {
		t.Fatalf("nil proof is not unsigned")
	}
	if _, err := snWalletProofFromOpts(map[string]any{"--message": "m", "--signature": nil}); err == nil {
		t.Fatalf("lone --message accepted")
	}
	if _, err := snWalletProofFromOpts(map[string]any{"--coldkey_seed_file": "/k", "--message": "m", "--signature": "s"}); err == nil {
		t.Fatalf("seed file with message accepted")
	}
}

func TestSnSetWalletSignsChallengeWithSeedFile(t *testing.T) {
	setTestNetworkJwt(t, "network-jwt")
	api := newFakeSnApi(t)
	ctx, clientStrategy := testClientStrategy(t)
	keypair := testAliceKeypair(t)
	// the hex form with a 0x prefix and a trailing newline, as `subkey` prints it
	seedFile := writeTestSeedFile(t, "0x"+testAliceSeedHex+"\n")

	err := snSetWallet(ctx, clientStrategy, api.server.URL, testAliceAddress, &snWalletProof{SeedFile: seedFile})
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.challengeRequests) != 1 || len(api.walletRequests) != 1 {
		t.Fatalf("requests: %d challenge, %d wallet", len(api.challengeRequests), len(api.walletRequests))
	}
	// the challenge request is the ur.io bridge's: TAO, purpose connect,
	// pinned to the coldkey, on the public route without the jwt
	challenge := api.challengeRequests[0]
	if stringField(t, challenge, "blockchain") != "TAO" || stringField(t, challenge, "purpose") != "connect" || stringField(t, challenge, "wallet_address") != testAliceAddress {
		t.Fatalf("challenge request %v", challenge)
	}
	if api.challengeAuth[0] != "" {
		t.Fatalf("challenge request carried Authorization %q", api.challengeAuth[0])
	}
	// the set carries the address, the exact message and the signature over it,
	// under the network jwt
	set := api.walletRequests[0]
	if stringField(t, set, "coldkey_ss58") != testAliceAddress {
		t.Fatalf("set coldkey %v", set["coldkey_ss58"])
	}
	if stringField(t, set, "message") != testChallengeMessage {
		t.Fatalf("set message %q", set["message"])
	}
	signatureHex := stringField(t, set, "signature")
	if !strings.HasPrefix(signatureHex, "0x") || len(signatureHex) != 130 {
		t.Fatalf("set signature %q", signatureHex)
	}
	signature, err := hex.DecodeString(signatureHex[2:])
	if err != nil {
		t.Fatal(err)
	}
	if !keypair.Verify(snWrapBytes(testChallengeMessage), signature) {
		t.Fatalf("posted signature does not verify over the wrapped challenge")
	}
	if api.walletAuth[0] != "Bearer network-jwt" {
		t.Fatalf("set Authorization %q", api.walletAuth[0])
	}
	if _, present := set["client_id"]; present {
		t.Fatalf("set carried a client_id: %v", set["client_id"])
	}
}

func TestSnSetWalletWithExternalSignature(t *testing.T) {
	setTestNetworkJwt(t, "network-jwt")
	api := newFakeSnApi(t)
	ctx, clientStrategy := testClientStrategy(t)
	keypair := testAliceKeypair(t)

	// signed elsewhere over the raw text (btcli style), bare hex; the message
	// passed in its one-line form
	signature, err := keypair.Sign([]byte(testChallengeMessage))
	if err != nil {
		t.Fatal(err)
	}
	proof := &snWalletProof{Message: snEscapeMessage(testChallengeMessage), Signature: hex.EncodeToString(signature)}
	if err := snSetWallet(ctx, clientStrategy, api.server.URL, testAliceAddress, proof); err != nil {
		t.Fatalf("set: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.challengeRequests) != 0 {
		t.Fatalf("an external signature fetched a challenge")
	}
	if len(api.walletRequests) != 1 {
		t.Fatalf("wallet requests: %d", len(api.walletRequests))
	}
	set := api.walletRequests[0]
	if stringField(t, set, "message") != testChallengeMessage {
		t.Fatalf("set message %q", set["message"])
	}
	if stringField(t, set, "signature") != "0x"+hex.EncodeToString(signature) {
		t.Fatalf("set signature %q", set["signature"])
	}
	if stringField(t, set, "coldkey_ss58") != testAliceAddress {
		t.Fatalf("set coldkey %v", set["coldkey_ss58"])
	}
}

func TestSnSetWalletRefusesSeedThatDoesNotDeriveTheColdkey(t *testing.T) {
	setTestNetworkJwt(t, "network-jwt")
	api := newFakeSnApi(t)
	ctx, clientStrategy := testClientStrategy(t)
	otherSeed := strings.Repeat("11", 32)
	seedFile := writeTestSeedFile(t, otherSeed)
	other, err := crv4.KeypairFromSeedHex(otherSeed)
	if err != nil {
		t.Fatal(err)
	}

	err = snSetWallet(ctx, clientStrategy, api.server.URL, testAliceAddress, &snWalletProof{SeedFile: seedFile})
	if err == nil || !strings.Contains(err.Error(), "derives "+other.Address()+", not "+testAliceAddress) {
		t.Fatalf("mismatch error: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.challengeRequests) != 0 || len(api.walletRequests) != 0 {
		t.Fatalf("a mismatched seed reached the api: %d challenge, %d wallet", len(api.challengeRequests), len(api.walletRequests))
	}

	// a missing seed file is an error, never created
	missing := filepath.Join(filepath.Dir(seedFile), "absent.seed")
	err = snSetWallet(ctx, clientStrategy, api.server.URL, testAliceAddress, &snWalletProof{SeedFile: missing})
	if err == nil {
		t.Fatalf("missing seed file accepted")
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("missing seed file was created")
	}
}

func TestSnSetWalletUnsignedFallbackExplainsMain(t *testing.T) {
	setTestNetworkJwt(t, "network-jwt")
	api := newFakeSnApi(t)
	api.walletError = "A coldkey signature over the wallet challenge is required."
	ctx, clientStrategy := testClientStrategy(t)

	err := snSetWallet(ctx, clientStrategy, api.server.URL, testAliceAddress, nil)
	if err == nil {
		t.Fatalf("unsigned set against main succeeded")
	}
	if !strings.Contains(err.Error(), api.walletError) || !strings.Contains(err.Error(), "--coldkey_seed_file") || !strings.Contains(err.Error(), "provider wallet challenge") {
		t.Fatalf("unsigned refusal lacks the guidance: %v", err)
	}
	api.mu.Lock()
	if len(api.challengeRequests) != 0 || len(api.walletRequests) != 1 {
		api.mu.Unlock()
		t.Fatalf("requests: %d challenge, %d wallet", len(api.challengeRequests), len(api.walletRequests))
	}
	set := api.walletRequests[0]
	if _, present := set["signature"]; present {
		t.Fatalf("unsigned set carried a signature")
	}
	if _, present := set["message"]; present {
		t.Fatalf("unsigned set carried a message")
	}
	if stringField(t, set, "coldkey_ss58") != testAliceAddress {
		t.Fatalf("set coldkey %v", set["coldkey_ss58"])
	}

	// a deployment with wallet_allow_unsigned still accepts it (explicit
	// fallback); one client strategy per command, as the CLI does
	api.walletError = ""
	api.mu.Unlock()
	ctx, clientStrategy = testClientStrategy(t)
	if err := snSetWallet(ctx, clientStrategy, api.server.URL, testAliceAddress, &snWalletProof{}); err != nil {
		t.Fatalf("unsigned set on a permissive deployment: %v", err)
	}
}

func TestSnPrintWalletChallenge(t *testing.T) {
	api := newFakeSnApi(t)
	ctx, clientStrategy := testClientStrategy(t)
	var out bytes.Buffer
	if err := snPrintWalletChallenge(ctx, clientStrategy, api.server.URL, testAliceAddress, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"----- message -----\n" + testChallengeMessage + "\n----- end -----\n",
		"message bytes (hex): 0x" + hex.EncodeToString([]byte(testChallengeMessage)),
		"expires in 300 seconds",
		`"substrate" signing context`,
		"<Bytes>...</Bytes>",
		"provider wallet set " + testAliceAddress + " --message='" + snEscapeMessage(testChallengeMessage) + "' --signature=0x",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("challenge text lacks %q:\n%s", want, text)
		}
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.challengeRequests) != 1 || stringField(t, api.challengeRequests[0], "wallet_address") != testAliceAddress {
		t.Fatalf("challenge requests %v", api.challengeRequests)
	}

	// an invalid address never reaches the api
	if err := snPrintWalletChallenge(ctx, clientStrategy, api.server.URL, "not-an-address", &out); err == nil {
		t.Fatalf("invalid address accepted")
	}
	if len(api.challengeRequests) != 1 {
		t.Fatalf("invalid address reached the api")
	}
}
