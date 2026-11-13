package hotkeyauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ss58"
)

// Serves the operator's wallet sign-in with the same challenge format and
// signature check as the server: single-use challenges, sr25519 in the
// substrate context over the raw or the <Bytes>-wrapped message.
type testOperator struct {
	stateLock  sync.Mutex
	challenges map[string]bool
	// wallet address to network name
	networks map[string]string
	taken    map[string]bool
	// set to make every challenge something other than a sign-in challenge
	hostileChallenge string
	// the wallet already has a network the next time a create arrives
	createRace bool
	signedRaw  int
	signedWrap int
	logins     int
}

func newTestOperator() *testOperator {
	return &testOperator{challenges: map[string]bool{}, networks: map[string]string{}, taken: map[string]bool{}}
}

func (self *testOperator) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var body struct {
		WalletAddress string      `json:"wallet_address"`
		Blockchain    string      `json:"blockchain"`
		WalletAuth    *walletAuth `json:"wallet_auth"`
		NetworkName   string      `json:"network_name"`
		Terms         bool        `json:"terms"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		http.Error(writer, "400 bad json", http.StatusBadRequest)
		return
	}
	answer := func(status int, value any) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(value)
	}
	switch request.URL.Path {
	case "/auth/wallet-challenge":
		if body.Blockchain != Blockchain {
			answer(http.StatusOK, map[string]any{"error": map[string]any{"message": "unsupported blockchain"}})
			return
		}
		value := make([]byte, 32)
		_, _ = rand.Read(value)
		challenge := base64.StdEncoding.EncodeToString(value)
		self.challenges[challenge] = false
		message := fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: %d", challenge, 1791244800)
		if self.hostileChallenge != "" {
			message = self.hostileChallenge
		}
		answer(http.StatusOK, map[string]any{"challenge": challenge, "timestamp": 1791244800, "expires_in": 300, "message_template": message})
	case "/auth/login":
		self.logins++
		if err := self.use(body.WalletAuth); err != nil {
			answer(http.StatusOK, map[string]any{"error": map[string]any{"message": err.Error()}})
			return
		}
		if name, ok := self.networks[body.WalletAuth.WalletAddress]; ok {
			answer(http.StatusOK, map[string]any{"network": map[string]any{"by_jwt": "jwt:" + name}})
			return
		}
		answer(http.StatusOK, map[string]any{"wallet_auth": body.WalletAuth})
	case "/auth/network-create":
		if !body.Terms {
			answer(http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "terms"}})
			return
		}
		if self.taken[body.NetworkName] {
			answer(http.StatusConflict, map[string]any{"error": map[string]any{"message": "Network name not available."}})
			return
		}
		if err := self.use(body.WalletAuth); err != nil {
			answer(http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": err.Error()}})
			return
		}
		if self.createRace {
			self.createRace = false
			self.networks[body.WalletAuth.WalletAddress] = body.NetworkName + "-raced"
		}
		if _, ok := self.networks[body.WalletAuth.WalletAddress]; ok {
			answer(http.StatusConflict, map[string]any{"error": map[string]any{"message": "user exists"}})
			return
		}
		self.networks[body.WalletAuth.WalletAddress] = body.NetworkName
		self.taken[body.NetworkName] = true
		answer(http.StatusOK, map[string]any{"network": map[string]any{"by_jwt": "jwt:" + body.NetworkName, "network_name": body.NetworkName}})
	default:
		http.NotFound(writer, request)
	}
}

// Consumes the challenge and verifies the signature as the operator does.
func (self *testOperator) use(auth *walletAuth) error {
	if auth == nil || auth.Blockchain != Blockchain {
		return errors.New("401 wallet auth")
	}
	lines := strings.Split(auth.WalletMessage, "\n")
	if len(lines) != 3 {
		return errors.New("400 invalid message format")
	}
	challenge := strings.TrimPrefix(lines[1], "Challenge: ")
	used, ok := self.challenges[challenge]
	if !ok || used {
		return errors.New("401 invalid wallet challenge")
	}
	self.challenges[challenge] = true
	publicKey, err := ss58.DecodeWithPrefix(auth.WalletAddress, ss58.BittensorPrefix)
	if err != nil {
		return err
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(auth.WalletSignature, "0x"))
	if err != nil || len(raw) != 64 {
		return errors.New("400 invalid signature encoding")
	}
	public := &schnorrkel.PublicKey{}
	if err := public.Decode(publicKey); err != nil {
		return err
	}
	signature := &schnorrkel.Signature{}
	if err := signature.Decode([64]byte(raw)); err != nil {
		return err
	}
	verify := func(message string) bool {
		ok, err := public.Verify(signature, schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
		return err == nil && ok
	}
	switch {
	case verify("<Bytes>" + auth.WalletMessage + "</Bytes>"):
		self.signedWrap++
	case verify(auth.WalletMessage):
		self.signedRaw++
	default:
		return errors.New("401 invalid signature")
	}
	return nil
}

func testHotkey(t *testing.T, fill byte) *crv4.Keypair {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = fill
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return hotkey
}

func TestSignInCreatesTheNetworkOnceAndThenLogsIn(t *testing.T) {
	operator := newTestOperator()
	server := httptest.NewServer(operator)
	defer server.Close()
	hotkey := testHotkey(t, 7)
	created, err := SignIn(t.Context(), Settings{ApiUrl: server.URL + "/", Hotkey: hotkey})
	if err != nil {
		t.Fatal(err)
	}
	name := NetworkName(hotkey.PublicKey())
	if !created.Created || created.ByJwt != "jwt:"+name {
		t.Fatalf("first sign-in did not create the hotkey's network: %+v", created)
	}
	again, err := SignIn(t.Context(), Settings{ApiUrl: server.URL, Hotkey: hotkey})
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.ByJwt != created.ByJwt {
		t.Fatalf("second sign-in did not log in to the same network: %+v", again)
	}
	// login and create at the first sign-in, login at the second
	if operator.signedRaw != 0 || operator.signedWrap != 3 {
		t.Fatalf("challenges were not all signed in the <Bytes> wrapper: raw %d wrapped %d", operator.signedRaw, operator.signedWrap)
	}
}

func TestSignInTriesTheNextNameWhenANameIsTaken(t *testing.T) {
	operator := newTestOperator()
	hotkey := testHotkey(t, 9)
	name := NetworkName(hotkey.PublicKey())
	operator.taken[name] = true
	operator.taken[name+"-2"] = true
	server := httptest.NewServer(operator)
	defer server.Close()
	network, err := SignIn(t.Context(), Settings{ApiUrl: server.URL, Hotkey: hotkey})
	if err != nil {
		t.Fatal(err)
	}
	if !network.Created || network.ByJwt != "jwt:"+name+"-3" {
		t.Fatalf("taken names were not skipped: %+v", network)
	}
}

func TestSignInLogsInAfterAConcurrentCreate(t *testing.T) {
	operator := newTestOperator()
	operator.createRace = true
	server := httptest.NewServer(operator)
	defer server.Close()
	hotkey := testHotkey(t, 11)
	network, err := SignIn(t.Context(), Settings{ApiUrl: server.URL, Hotkey: hotkey, NetworkName: "miner-one"})
	if err != nil {
		t.Fatal(err)
	}
	if network.Created || network.ByJwt != "jwt:miner-one-raced" {
		t.Fatalf("raced create did not resolve to the existing network: %+v", network)
	}
}

func TestSignInNeverSignsAnotherStatement(t *testing.T) {
	for _, hostile := range []string{
		"Approve URnetwork hotkey wallet mapping\n{}",
		"Sign in to URnetwork\nChallenge: c2hvcnQ=\nTimestamp: 1",
		"Sign in to URnetwork\nChallenge: AAAAAAAAAAAAAAAAAAAAAAAA\nTimestamp: 01",
		"Sign in to URnetwork\nChallenge: AAAAAAAAAAAAAAAAAAAAAAAA\nTimestamp: 1\n",
		"Sign in to URnetwork\nChallenge: AAAA AAAAAAAAAAAAAAAAAAAA\nTimestamp: 1",
		"\x07\x00\x01",
	} {
		operator := newTestOperator()
		operator.hostileChallenge = hostile
		server := httptest.NewServer(operator)
		_, err := SignIn(t.Context(), Settings{ApiUrl: server.URL, Hotkey: testHotkey(t, 13)})
		server.Close()
		if !errors.Is(err, ErrRefused) || operator.logins != 0 {
			t.Errorf("hostile challenge %q was signed or submitted: %v", hostile, err)
		}
	}
}

func TestSignInReportsTheOperatorRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "429 too many wallet challenges", http.StatusTooManyRequests)
	}))
	defer server.Close()
	_, err := SignIn(t.Context(), Settings{ApiUrl: server.URL, Hotkey: testHotkey(t, 15)})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "429") {
		t.Fatalf("operator refusal not reported: %v", err)
	}
}

func TestNetworkNameFollowsTheOperatorNameRules(t *testing.T) {
	name := NetworkName(testHotkey(t, 17).PublicKey())
	if len(name) < 5 || len(name) > 50 || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		t.Fatalf("network name %q breaks the operator name rules", name)
	}
	if other := NetworkName(testHotkey(t, 19).PublicKey()); other == name {
		t.Fatal("two hotkeys share a network name")
	}
}
