package solana

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	cold     = "46yocBWGMD1vB9auwVhN2kvu3cm6qcX6DLZHr4r78PTG"
	coldUSDC = "EaNwoRodQMACVf7cRMpEjQW7Dz4RG3PwDrX921tj2MZL"
	usdcMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	taoMint  = "taoC6xyv2v8tDLcev4uaGUgV4vdQsWJrGft2kcBRrBY"
	oftProg  = "tao3RyGP8XiiWQKmBzkiULmPoMewWjq65b46H4rTAQQ"
	// The taker of the unsigned Jupiter fixture (a TAO holder) and its TAO account.
	taker    = "FkaLnX17cXZGyeu3kZGdHCNdFMJJzBrPPYVvd18B3MZp"
	takerTAO = "FsgxcF73ogFNM9aX7QQ2sYj3zfNasFG8AAPYnyPiepUm"
	someSig  = "4ZxhSXHej2GWW3JJf8KoioUBmWsYhrNnuRporMMwDxyQg4t743grpE3y5RhNfkZLD4p4JwmFToQ8ju6pcpDD5zeU"
)

func TestBase58(t *testing.T) {
	for _, c := range []struct {
		hex  []byte
		text string
	}{
		{make([]byte, 32), "11111111111111111111111111111111"},
		{[]byte{0, 0, 1}, "112"},
		{[]byte{0xff}, "5Q"},
		{[]byte("hello world"), "StV1DL6CwTryKyV"},
	} {
		if got := Base58Encode(c.hex); got != c.text {
			t.Errorf("encode %x = %s, want %s", c.hex, got, c.text)
		}
		got, err := Base58Decode(c.text)
		if err != nil || !bytes.Equal(got, c.hex) {
			t.Errorf("decode %s = %x %v", c.text, got, err)
		}
	}
	for _, bad := range []string{"", "0", "O", "I", "l", "abc+"} {
		if _, err := Base58Decode(bad); err == nil {
			t.Errorf("%q decoded", bad)
		}
	}
	if _, err := ParsePublicKey("1111"); err == nil {
		t.Error("a short address parsed")
	}
}

func TestPublicKeyText(t *testing.T) {
	p := MustPublicKey(cold)
	raw, err := json.Marshal(map[PublicKey]PublicKey{p: TokenProgram})
	if err != nil || string(raw) != `{"`+cold+`":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"}` {
		t.Fatalf("%s %v", raw, err)
	}
	var back map[PublicKey]PublicKey
	if err := json.Unmarshal(raw, &back); err != nil || back[p] != TokenProgram {
		t.Fatalf("%v %v", back, err)
	}
	if !(PublicKey{}).IsZero() || p.IsZero() {
		t.Fatal("IsZero")
	}
}

func TestCurveAndProgramAddresses(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if !IsOnCurve([32]byte(pub)) || PublicKeyOf(priv) != PublicKey(pub) {
		t.Fatal("a real public key must be on the curve")
	}
	if !IsOnCurve(MustPublicKey(cold)) || !IsOnCurve(MustPublicKey(taker)) {
		t.Fatal("wallets are on the curve")
	}
	if IsOnCurve(MustPublicKey(coldUSDC)) {
		t.Fatal("an associated token account is a PDA, off the curve")
	}
	ata, err := AssociatedTokenAddress(MustPublicKey(cold), MustPublicKey(usdcMint))
	if err != nil || ata.String() != coldUSDC {
		t.Fatalf("ATA %s %v", ata, err)
	}
	// An upgradeable program's ProgramData is the PDA of [program] under the loader.
	programData, _, err := FindProgramAddress([][]byte{MustPublicKey(oftProg).Bytes()}, MustPublicKey("BPFLoaderUpgradeab1e11111111111111111111111"))
	if err != nil || programData.String() != "26qm8uHNMRMMju6AzWfje66XCVfAXaJYHRLEM8Gr4opX" {
		t.Fatalf("program data %s %v", programData, err)
	}
	if _, _, err := FindProgramAddress([][]byte{make([]byte, 33)}, TokenProgram); err == nil {
		t.Fatal("a 33-byte seed was accepted")
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func fixtureTx(t *testing.T, name string) (string, []byte) {
	t.Helper()
	var f struct {
		Signature   string `json:"signature"`
		Transaction string `json:"transaction"`
	}
	if err := json.Unmarshal(fixture(t, name), &f); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(f.Transaction)
	if err != nil {
		t.Fatal(err)
	}
	return f.Signature, raw
}

func TestParseJupiterTransaction(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fixture(t, "jupiter_unsigned_v0.b64"))))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Version != 0 || tx.NumRequiredSignatures != 1 || len(tx.StaticKeys) != 7 || tx.FeePayer().String() != taker {
		t.Fatalf("version %d signers %d keys %d payer %s", tx.Version, tx.NumRequiredSignatures, len(tx.StaticKeys), tx.FeePayer())
	}
	if signers := tx.RequiredSigners(); len(signers) != 1 || signers[0].String() != taker {
		t.Fatalf("signers %v", signers)
	}
	programs, ok := tx.ProgramIDs()
	if !ok || len(programs) != 3 || programs[0] != ComputeBudgetProgram || programs[2].String() != "JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4" {
		t.Fatalf("programs %v %v", programs, ok)
	}
	found := false
	for _, k := range tx.StaticKeys {
		found = found || k.String() == coldUSDC
	}
	if !found {
		t.Fatal("the receiver's USDC account is a static key")
	}
	if !bytes.Equal(tx.Serialize(), raw) {
		t.Fatal("serialize must round-trip byte for byte")
	}
	if tx.SignedBy(MustPublicKey(taker)) || tx.ID() != strings.Repeat("1", 64) {
		t.Fatalf("an unsigned slot: %v %s", tx.SignedBy(MustPublicKey(taker)), tx.ID())
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if err := tx.Sign(other); err == nil {
		t.Fatal("a key that is not a required signer must not sign")
	}
}

func TestParseSignedTransactions(t *testing.T) {
	for _, c := range []struct {
		file    string
		version int
	}{{"signed_v0.json", 0}} {
		signature, raw := fixtureTx(t, c.file)
		tx, err := ParseTransaction(raw)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if tx.Version != c.version || tx.ID() != signature || !bytes.Equal(tx.Serialize(), raw) {
			t.Fatalf("%s: version %d id %s", c.file, tx.Version, tx.ID())
		}
		if !tx.SignedBy(tx.FeePayer()) {
			t.Fatalf("%s: the fee payer's real signature must verify", c.file)
		}
		tx.Message[len(tx.Message)-1] ^= 1
		if tx.SignedBy(tx.FeePayer()) {
			t.Fatalf("%s: a changed message must not verify", c.file)
		}
	}
}

// A legacy transaction parses and signs as a v0 one does.
func TestParseLegacyTransaction(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	program := MustPublicKey(oftProg)
	message := []byte{1, 0, 1, 2}
	message = append(message, public...)
	message = append(message, program[:]...)
	message = append(message, make([]byte, 32)...)
	message = append(message, 1, 1, 1, 0, 1, 7)
	raw := append([]byte{1}, make([]byte, 64)...)
	raw = append(raw, message...)
	tx, err := ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Version != -1 || tx.NumRequiredSignatures != 1 || len(tx.StaticKeys) != 2 || !bytes.Equal(tx.Serialize(), raw) {
		t.Fatalf("%+v", tx)
	}
	if err := tx.Sign(private); err != nil {
		t.Fatal(err)
	}
	if !tx.SignedBy(tx.FeePayer()) {
		t.Fatal("the fee payer's signature must verify")
	}
	tx.Message[len(tx.Message)-1] ^= 1
	if tx.SignedBy(tx.FeePayer()) {
		t.Fatal("a changed message must not verify")
	}
}

func TestRefusedTransactions(t *testing.T) {
	_, v1 := fixtureTx(t, "v1.json")
	if _, err := ParseTransaction(v1); err == nil || !strings.Contains(err.Error(), "0x81") {
		t.Fatalf("v1 must be refused: %v", err)
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fixture(t, "jupiter_unsigned_v0.b64"))))
	for name, bad := range map[string][]byte{
		"empty":     nil,
		"truncated": raw[:len(raw)-1],
		"trailing":  append(append([]byte(nil), raw...), 0),
		"v2 prefix": func() []byte { b := append([]byte(nil), raw...); b[65] = 0x82; return b }(),
		"two slots": append([]byte{2}, append(make([]byte, 64), raw[1:]...)...),
	} {
		if _, err := ParseTransaction(bad); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// legacyTx builds a minimal legacy transaction paid by payer.
func legacyTx(payer PublicKey) []byte {
	msg := []byte{1, 0, 1} // one signer, one read-only unsigned key
	msg = appendCompactU16(msg, 2)
	msg = append(msg, payer[:]...)
	msg = append(msg, SystemProgram[:]...)
	msg = append(msg, make([]byte, 32)...) // blockhash
	msg = appendCompactU16(msg, 1)
	msg = append(msg, 1) // program index
	msg = appendCompactU16(msg, 1)
	msg = append(msg, 0)
	msg = appendCompactU16(msg, 0)
	return append(append(appendCompactU16(nil, 1), make([]byte, 64)...), msg...)
}

func TestSign(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	tx, err := ParseTransaction(legacyTx(PublicKeyOf(key)))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Sign(key); err != nil || !tx.SignedBy(PublicKeyOf(key)) {
		t.Fatalf("sign: %v", err)
	}
	again, err := ParseTransaction(tx.Serialize())
	if err != nil || !again.SignedBy(PublicKeyOf(key)) || again.ID() != tx.ID() {
		t.Fatalf("a signed transaction must survive serialization: %v", err)
	}
	if err := tx.Sign(key[:10]); err == nil {
		t.Fatal("a malformed key signed")
	}
}

type rpcRequest struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// fakeRPC answers JSON-RPC calls from testdata/rpc.json, keyed by method and,
// for getAccountInfo, by the account read.
type fakeRPC struct {
	t        *testing.T
	results  map[string]json.RawMessage
	mu       sync.Mutex
	requests []rpcRequest
	// override answers one method with a raw HTTP status and body.
	override map[string]func(n int) (int, string)
}

func newFakeRPC(t *testing.T) (*fakeRPC, *Client) {
	f := &fakeRPC{t: t, override: map[string]func(int) (int, string){}}
	if err := json.Unmarshal(fixture(t, "rpc.json"), &f.results); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	c := NewClient(server.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return f, c
}

func (f *fakeRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
		f.t.Errorf("bad request %s %s", r.Method, body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	n := 0
	for _, prior := range f.requests {
		if prior.Method == req.Method {
			n++
		}
	}
	f.mu.Unlock()
	if o := f.override[req.Method]; o != nil {
		if status, text := o(n); status != 0 {
			w.WriteHeader(status)
			io.WriteString(w, text)
			return
		}
	}
	key := req.Method
	if req.Method == "getAccountInfo" {
		var address string
		json.Unmarshal(req.Params[0], &address)
		key += ":" + map[string]string{
			oftProg: "program", "26qm8uHNMRMMju6AzWfje66XCVfAXaJYHRLEM8Gr4opX": "programdata",
			taoMint: "mint", coldUSDC: "token",
		}[address]
	}
	result, ok := f.results[key]
	if !ok {
		result = json.RawMessage(`{"context":{"slot":1},"value":null}`)
		if req.Method == "getTransaction" || req.Method == "getBlockHeight" {
			result = json.RawMessage(`null`)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":`+string(result)+`}`)
}

func (f *fakeRPC) last(method string) rpcRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Method == method {
			return f.requests[i]
		}
	}
	f.t.Fatalf("no %s request", method)
	return rpcRequest{}
}

func TestClientReads(t *testing.T) {
	_, c := newFakeRPC(t)
	ctx := context.Background()
	if height, err := c.BlockHeight(ctx); err != nil || height != 437953911 {
		t.Fatalf("height %d %v", height, err)
	}
	if lamports, err := c.Balance(ctx, MustPublicKey(cold)); err != nil || lamports != 0 {
		t.Fatalf("balance %d %v", lamports, err)
	}
	account, err := c.TokenAccount(ctx, MustPublicKey(coldUSDC))
	if err != nil || account.Mint.String() != usdcMint || account.Owner.String() != cold || account.Amount != 1_075_220_000 || account.Decimals != 6 || account.State != "initialized" || account.Delegate != nil {
		t.Fatalf("token account %+v %v", account, err)
	}
	if missing, err := c.TokenAccount(ctx, MustPublicKey(takerTAO)); missing != nil || err != nil {
		t.Fatalf("a missing account is (nil, nil): %+v %v", missing, err)
	}
	if acct, err := c.Account(ctx, MustPublicKey(oftProg)); err != nil || !acct.Executable || acct.Owner != MustPublicKey("BPFLoaderUpgradeab1e11111111111111111111111") || len(acct.Data) != 36 {
		t.Fatalf("raw account %+v %v", acct, err)
	}
}

// An account's data can come back in either of jsonParsed's shapes, and what cannot be
// read counts as data, since the funder's gates want an empty wallet.
func TestSimulatedAccountDataLen(t *testing.T) {
	for name, test := range map[string]struct {
		data string
		want int
	}{
		"empty, as a wallet is": {`["","base64"]`, 0},
		"bytes":                 {`["AQID","base64"]`, 3},
		"a parsed nonce":        {`{"program":"nonce","parsed":{"type":"initialized"},"space":80}`, 80},
		"parsed with no space":  {`{"program":"nonce","parsed":{}}`, len(`{"program":"nonce","parsed":{}}`)},
		"another encoding":      {`["abc","base58"]`, len(`["abc","base58"]`)},
		"bad base64":            {`["%%%","base64"]`, len(`["%%%","base64"]`)},
	} {
		account := &jsonAccount{Data: json.RawMessage(test.data)}
		if got := account.dataLen(); got != test.want {
			t.Errorf("%s: %d, want %d", name, got, test.want)
		}
	}
}

func TestSimulate(t *testing.T) {
	f, c := newFakeRPC(t)
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fixture(t, "jupiter_unsigned_v0.b64"))))
	accounts := []PublicKey{MustPublicKey(takerTAO), MustPublicKey(coldUSDC), MustPublicKey(taker), MustPublicKey("11111111111111111111111111111112")}
	sim, err := c.Simulate(context.Background(), raw, accounts)
	if err != nil || sim.Err != nil || sim.UnitsConsumed != 39557 || len(sim.Accounts) != 4 {
		t.Fatalf("%+v %v", sim, err)
	}
	tao, usdc, payer := sim.Accounts[0], sim.Accounts[1], sim.Accounts[2]
	if !tao.Exists || tao.Token == nil || tao.Token.Mint.String() != taoMint || tao.Token.Owner.String() != taker || tao.Token.Amount != 17_003_885_626 {
		t.Fatalf("taker TAO after: %+v %+v", tao, tao.Token)
	}
	if usdc.Token == nil || usdc.Token.Owner.String() != cold || usdc.Token.Amount != 1_604_085_491 {
		t.Fatalf("receiver USDC after: %+v", usdc.Token)
	}
	if !payer.Exists || payer.Token != nil || payer.Owner != SystemProgram || payer.Lamports == 0 || payer.DataLen != 0 {
		t.Fatalf("payer %+v", payer)
	}
	req := f.last("simulateTransaction")
	var tx string
	json.Unmarshal(req.Params[0], &tx)
	var config map[string]any
	json.Unmarshal(req.Params[1], &config)
	if tx != base64.StdEncoding.EncodeToString(raw) || config["sigVerify"] != false || config["replaceRecentBlockhash"] != false || config["encoding"] != "base64" || config["commitment"] != "processed" {
		t.Fatalf("config %v", config)
	}
	if a := config["accounts"].(map[string]any); a["encoding"] != "jsonParsed" || len(a["addresses"].([]any)) != 4 {
		t.Fatalf("accounts config %v", a)
	}

	// A transaction that fails before executing reports no accounts.
	f.results["simulateTransaction"] = json.RawMessage(`{"context":{"slot":1},"value":{"err":"BlockhashNotFound","logs":[],"unitsConsumed":0,"accounts":[null,null,null,null]}}`)
	sim, err = c.Simulate(context.Background(), raw, accounts)
	if err != nil || string(sim.Err) != `"BlockhashNotFound"` || sim.Accounts[0].Exists || sim.Accounts[1].Token != nil {
		t.Fatalf("%+v %v", sim, err)
	}
	f.results["simulateTransaction"] = json.RawMessage(`{"context":{"slot":1},"value":{"err":{"InstructionError":[2,{"Custom":6001}]},"logs":["x"],"accounts":[null]}}`)
	if _, err := c.Simulate(context.Background(), raw, accounts); err == nil {
		t.Fatal("an account count mismatch must fail")
	}
}

func TestStatuses(t *testing.T) {
	f, c := newFakeRPC(t)
	ctx := context.Background()
	statuses, err := c.SignatureStatuses(ctx, []string{someSig, strings.Repeat("1", 64)})
	if err != nil || len(statuses) != 2 || statuses[1] != nil || statuses[0].ConfirmationStatus != "finalized" || statuses[0].Err != nil || statuses[0].Slot != 459828144 {
		t.Fatalf("%+v %v", statuses, err)
	}
	if !strings.Contains(string(f.last("getSignatureStatuses").Params[1]), `"searchTransactionHistory":true`) {
		t.Fatal("history search")
	}
}

func TestErrorsAndRetry(t *testing.T) {
	f, c := newFakeRPC(t)
	ctx := context.Background()
	f.override["getSignatureStatuses"] = func(int) (int, string) {
		return 200, `{"jsonrpc":"2.0","id":1,"error":{"code":-32015,"message":"Transaction version (1) is not supported"}}`
	}
	var rpcErr *RPCError
	if _, err := c.SignatureStatuses(ctx, []string{someSig}); !errors.As(err, &rpcErr) || rpcErr.Code != -32015 {
		t.Fatalf("rpc error: %v", err)
	}
	f.override["getBalance"] = func(n int) (int, string) {
		if n == 1 {
			return 429, "slow down"
		}
		return 0, ""
	}
	if _, err := c.Balance(ctx, MustPublicKey(cold)); err != nil {
		t.Fatalf("one 429 is retried: %v", err)
	}
	f.override["getBlockHeight"] = func(int) (int, string) { return 400, "bad" }
	var httpErr *HTTPError
	if _, err := c.BlockHeight(ctx); !errors.As(err, &httpErr) || httpErr.Status != 400 {
		t.Fatalf("http error: %v", err)
	}
	count := 0
	for _, r := range f.requests {
		if r.Method == "getBlockHeight" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("a 400 is not retried: %d requests", count)
	}
	f.override["getBlockHeight"] = func(int) (int, string) { return 503, "down" }
	if _, err := c.BlockHeight(ctx); !errors.As(err, &httpErr) || httpErr.Status != 503 {
		t.Fatalf("a repeated 503 fails: %v", err)
	}
}

func TestRedactKeepsOnlySchemeAndHost(t *testing.T) {
	for endpoint, want := range map[string]string{
		"https://mainnet.helius-rpc.com/?api-key=QUERYKEY": "https://mainnet.helius-rpc.com",
		"https://x.solana-mainnet.quiknode.pro/PATHKEY/":   "https://x.solana-mainnet.quiknode.pro",
		"https://user:PASSWORD@rpc.example.com/v1":         "https://rpc.example.com",
		"https://api.mainnet-beta.solana.com":              "https://api.mainnet-beta.solana.com",
		"":                                                 "(endpoint)",
		"https://rpc.example.com/\x7f?api-key=QUERYKEY":    "(endpoint)",
	} {
		if got := Redact(endpoint); got != want {
			t.Errorf("Redact(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

func TestErrorsNeverCarryTheEndpointsKey(t *testing.T) {
	for _, endpoint := range []string{
		"http://127.0.0.1:1/PATHKEY/?api-key=QUERYKEY", // nothing listens: a transport error
		"http://127.0.0.1:1/\x7f?api-key=QUERYKEY",     // unparseable
	} {
		c := NewClient(endpoint)
		c.sleep = func(context.Context, time.Duration) error { return nil }
		_, err := c.BlockHeight(context.Background())
		if err == nil || strings.Contains(err.Error(), "KEY") {
			t.Fatalf("%q: %v", endpoint, err)
		}
	}
}

// A compiled v0 message orders its keys as the runtime requires, and tables take
// what they can without taking a signer or an invoked program.
func TestCompileV0(t *testing.T) {
	payer := MustPublicKey(cold)
	program := MustPublicKey(oftProg)
	writableStatic := MustPublicKey(taoMint)
	readonlyStatic := MustPublicKey(usdcMint)
	loadedWritable := MustPublicKey(coldUSDC)
	loadedReadonly := MustPublicKey(takerTAO)
	table := &LookupTable{Address: MustPublicKey(taker), Addresses: []PublicKey{loadedReadonly, program, loadedWritable, payer}}
	blockhash := [32]byte{7}
	tx, err := CompileV0(payer, []Instruction{{Program: program, Data: []byte{1, 2}, Accounts: []AccountMeta{
		{Key: payer, Signer: true, Writable: true},
		{Key: readonlyStatic},
		{Key: loadedWritable, Writable: true},
		{Key: writableStatic, Writable: true},
		{Key: loadedReadonly},
	}}}, blockhash, []*LookupTable{table})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Version != 0 || tx.FeePayer() != payer || tx.NumRequiredSignatures != 1 || tx.RecentBlockhash != blockhash {
		t.Fatalf("%+v", tx)
	}
	want := []PublicKey{payer, writableStatic, program, readonlyStatic}
	if len(tx.StaticKeys) != len(want) {
		t.Fatalf("static keys %v", tx.StaticKeys)
	}
	for i := range want {
		if tx.StaticKeys[i] != want[i] {
			t.Fatalf("static key %d: %s", i, tx.StaticKeys[i])
		}
	}
	resolved, err := tx.ResolveInstructions([]*LookupTable{table})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Program != program || !bytes.Equal(resolved[0].Data, []byte{1, 2}) {
		t.Fatalf("%+v", resolved)
	}
	for i, account := range []AccountMeta{
		{Key: payer, Signer: true, Writable: true},
		{Key: readonlyStatic},
		{Key: loadedWritable, Writable: true},
		{Key: writableStatic, Writable: true},
		{Key: loadedReadonly},
	} {
		if resolved[0].Accounts[i] != account {
			t.Fatalf("account %d: got %+v want %+v", i, resolved[0].Accounts[i], account)
		}
	}
	if _, err := tx.ResolveInstructions(nil); err == nil {
		t.Fatal("resolved without the table")
	}
	if _, err := CompileV0(payer, []Instruction{{Program: program, Data: make([]byte, MaxTransactionBytes)}}, blockhash, nil); err == nil {
		t.Fatal("compiled a transaction larger than a packet")
	}
}

func TestParseLookupTable(t *testing.T) {
	address := MustPublicKey(taker)
	data := make([]byte, lookupTableHeader+64)
	copy(data[lookupTableHeader+32:], MustPublicKey(cold).Bytes())
	table, err := ParseLookupTable(address, data)
	if err != nil || len(table.Addresses) != 2 || table.Addresses[1] != MustPublicKey(cold) {
		t.Fatalf("%+v %v", table, err)
	}
	if _, err := ParseLookupTable(address, data[:lookupTableHeader+10]); err == nil {
		t.Fatal("a misaligned table parsed")
	}
}

func TestBlockhashAndSend(t *testing.T) {
	f, c := newFakeRPC(t)
	ctx := context.Background()
	f.results["getLatestBlockhash"] = json.RawMessage(`{"context":{"slot":1},"value":{"blockhash":"` + Base58Encode(make([]byte, 32)) + `","lastValidBlockHeight":77}}`)
	hash, err := c.LatestBlockhash(ctx)
	if err != nil || hash.LastValidBlockHeight != 77 || hash.Hash != ([32]byte{}) {
		t.Fatalf("%+v %v", hash, err)
	}
	f.results["sendTransaction"] = json.RawMessage(`"sig"`)
	signature, err := c.SendTransaction(ctx, []byte{1})
	if err != nil || signature != "sig" {
		t.Fatal(signature, err)
	}
	// A broadcast whose outcome is unknown is not repeated by the client.
	calls := 0
	f.override["sendTransaction"] = func(int) (int, string) {
		calls++
		return 503, "down"
	}
	if _, err := c.SendTransaction(ctx, []byte{1}); err == nil || calls != 1 {
		t.Fatalf("calls %d err %v", calls, err)
	}
}
