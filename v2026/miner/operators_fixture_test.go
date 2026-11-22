package miner

// A synthetic operator for the operator list commands: it serves the list
// itself, the wallet sign-in as hotkeyauth's tests model it, and the auth
// code and password logins of "provider auth".

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

const testAuthCode = "synthetic-auth-code"

type testWalletOperator struct {
	server *httptest.Server
	// accepted by the wallet sign-in; nil refuses every sign-in
	hotkey *crv4.Keypair

	stateLock  sync.Mutex
	listYaml   string
	listOnline bool
	// single-use: false until a signature consumes it
	challenges map[string]bool
	network    string
	passwords  []string
	// answers a password login without a network
	passwordNoNetwork bool
}

func newTestWalletOperator(t *testing.T, hotkey *crv4.Keypair) *testWalletOperator {
	t.Helper()
	self := &testWalletOperator{hotkey: hotkey, challenges: map[string]bool{}, listOnline: true}
	self.server = httptest.NewServer(self)
	t.Cleanup(self.server.Close)
	return self
}

// The operator itself, as a list entry; plaintext urls are listed only
// because the host is loopback.
func (self *testWalletOperator) operator(domain string) operatorlist.Operator {
	return operatorlist.Operator{
		Domain:     domain,
		ApiUrl:     self.server.URL,
		ConnectUrl: "ws" + strings.TrimPrefix(self.server.URL, "http"),
	}
}

func (self *testWalletOperator) listUrl() string {
	return self.server.URL + "/operators.yml"
}

// Serves operators as the published list and returns its digest.
func (self *testWalletOperator) setList(t *testing.T, operators ...operatorlist.Operator) string {
	t.Helper()
	raw := testOperatorListYaml(t, operators...)
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.listYaml = raw
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (self *testWalletOperator) setListOnline(online bool) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.listOnline = online
}

func (self *testWalletOperator) receivedPasswords() []string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]string(nil), self.passwords...)
}

func (self *testWalletOperator) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if request.URL.Path == "/operators.yml" {
		if !self.listOnline || self.listYaml == "" {
			http.Error(writer, "503 synthetic outage", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/yaml")
		_, _ = writer.Write([]byte(self.listYaml))
		return
	}
	var body struct {
		WalletAuth *struct {
			WalletAddress   string `json:"wallet_address"`
			WalletSignature string `json:"wallet_signature"`
			WalletMessage   string `json:"wallet_message"`
		} `json:"wallet_auth"`
		NetworkName string `json:"network_name"`
		AuthCode    string `json:"auth_code"`
		UserAuth    string `json:"user_auth"`
		Password    string `json:"password"`
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
	refuse := func(message string) {
		answer(http.StatusOK, map[string]any{"error": map[string]any{"message": message}})
	}
	// consumes the challenge, then checks the hotkey signed it in the
	// <Bytes> wrapper, as the operator does
	signedIn := func() bool {
		auth := body.WalletAuth
		if self.hotkey == nil || auth == nil || auth.WalletAddress != self.hotkey.Address() {
			return false
		}
		lines := strings.Split(auth.WalletMessage, "\n")
		if len(lines) != 3 {
			return false
		}
		challenge := strings.TrimPrefix(lines[1], "Challenge: ")
		if used, ok := self.challenges[challenge]; !ok || used {
			return false
		}
		self.challenges[challenge] = true
		signature, err := hex.DecodeString(strings.TrimPrefix(auth.WalletSignature, "0x"))
		return err == nil && self.hotkey.Verify([]byte("<Bytes>"+auth.WalletMessage+"</Bytes>"), signature)
	}
	switch request.URL.Path {
	case "/auth/wallet-challenge":
		challenge := fmt.Sprintf("SyntheticChallenge%04d", len(self.challenges))
		self.challenges[challenge] = false
		answer(http.StatusOK, map[string]any{"message_template": fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: %d", challenge, 1791244800)})
	case "/auth/login":
		switch {
		case !signedIn():
			refuse("invalid wallet sign-in")
		case self.network == "":
			answer(http.StatusOK, map[string]any{})
		default:
			answer(http.StatusOK, map[string]any{"network": map[string]any{"by_jwt": "jwt:" + self.network}})
		}
	case "/auth/network-create":
		if !signedIn() {
			answer(http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "invalid wallet sign-in"}})
			return
		}
		self.network = body.NetworkName
		answer(http.StatusOK, map[string]any{"network": map[string]any{"by_jwt": "jwt:" + self.network, "network_name": self.network}})
	case "/auth/code-login":
		if body.AuthCode != testAuthCode {
			refuse("invalid auth code")
			return
		}
		answer(http.StatusOK, map[string]any{"by_jwt": "jwt:code"})
	case "/auth/login-with-password":
		self.passwords = append(self.passwords, body.Password)
		if self.passwordNoNetwork {
			answer(http.StatusOK, map[string]any{})
			return
		}
		answer(http.StatusOK, map[string]any{"network": map[string]any{"by_jwt": "jwt:password", "name": "synthetic"}})
	default:
		http.NotFound(writer, request)
	}
}

// Synthetic operators under .example, with the urls the published list
// would carry.
func testListedOperator(domain string) operatorlist.Operator {
	return operatorlist.Operator{Domain: domain, ApiUrl: "https://api." + domain, ConnectUrl: "wss://connect." + domain}
}

// A valid published list, so every fixture passes the client's own checks.
func testOperatorListYaml(t *testing.T, operators ...operatorlist.Operator) string {
	t.Helper()
	var raw strings.Builder
	raw.WriteString("schema: " + operatorlist.Schema + "\noperators:\n")
	for _, operator := range operators {
		fmt.Fprintf(&raw, "  - domain: %s\n    api_url: %s\n    connect_url: %s\n", operator.Domain, operator.ApiUrl, operator.ConnectUrl)
	}
	if _, err := operatorlist.Parse([]byte(raw.String())); err != nil {
		t.Fatal(err)
	}
	return raw.String()
}

func testHotkey(t *testing.T, fill byte) *crv4.Keypair {
	t.Helper()
	var seed [32]byte
	for index := range seed {
		seed[index] = fill
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return hotkey
}
