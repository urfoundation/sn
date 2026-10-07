// Shared fakes for the operator-list tests. Lists, runners, timers and log
// lines are delivered on channels, so tests wait on actual events rather than
// time. All operators, keys and tokens are synthetic.
package validator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/operatorlist"
	"github.com/urfoundation/sn/v2026/ss58"
)

// Guards each wait on a real event; reaching it is a failure, never a pace.
const operatorTestTimeout = 30 * time.Second

func operatorTestOperator(name string) operatorlist.Operator {
	return operatorlist.Operator{Domain: name + ".example", ApiUrl: "https://api." + name + ".example", ConnectUrl: "wss://connect." + name + ".example"}
}

// Publishes lists the way the refresher does: one new immutable snapshot per
// change.
type operatorTestSource struct {
	value *connect.MonitorValue[*operatorlist.Snapshot]
}

func newOperatorTestSource() operatorTestSource {
	return operatorTestSource{value: connect.NewMonitorValue[*operatorlist.Snapshot](nil)}
}

func (self operatorTestSource) Snapshot() (*operatorlist.Snapshot, chan struct{}) {
	return self.value.Get()
}

func (self operatorTestSource) publish(operators ...operatorlist.Operator) {
	self.value.Set(&operatorlist.Snapshot{
		List:      operatorlist.List{Schema: operatorlist.Schema, Operators: operators},
		Sha256:    "sha256:" + strings.Repeat("ab", 32),
		FetchedAt: time.Date(2026, 10, 6, 11, 0, 0, 0, time.UTC),
	})
}

// One started runner. It runs until its context ends or the test fails it.
type operatorTestRun struct {
	operator operatorlist.Operator
	settings measurementRunSettings
	ctx      context.Context
	fail     chan error
	stopped  chan struct{}
}

type operatorTestRunner struct {
	runs chan *operatorTestRun
}

func newOperatorTestRunner() *operatorTestRunner {
	return &operatorTestRunner{runs: make(chan *operatorTestRun, 64)}
}

func (self *operatorTestRunner) run(ctx context.Context, operator operatorlist.Operator, settings measurementRunSettings) error {
	run := &operatorTestRun{operator: operator, settings: settings, ctx: ctx, fail: make(chan error, 1), stopped: make(chan struct{})}
	defer close(run.stopped)
	self.runs <- run
	select {
	case <-ctx.Done():
		return nil
	case err := <-run.fail:
		return err
	}
}

func (self *operatorTestRunner) next(t *testing.T) *operatorTestRun {
	t.Helper()
	select {
	case run := <-self.runs:
		return run
	case <-time.After(operatorTestTimeout):
		t.Fatal("no operator runner started")
		return nil
	}
}

// Valid only after the supervisor that started the runners has joined them.
func (self *operatorTestRunner) assertNoMoreRuns(t *testing.T) {
	t.Helper()
	select {
	case run := <-self.runs:
		t.Fatalf("unexpected runner for %s", run.operator.Domain)
	default:
	}
}

func waitOperatorTestStopped(t *testing.T, run *operatorTestRun) {
	t.Helper()
	select {
	case <-run.stopped:
	case <-time.After(operatorTestTimeout):
		t.Fatalf("runner for %s was not stopped", run.operator.Domain)
	}
}

// Each requested wait is handed to the test, which fires it after checking
// the delay. Firing advances the clock by that delay.
type operatorTestClock struct {
	stateLock sync.Mutex
	now       time.Time
	waits     chan operatorTestWait
}

type operatorTestWait struct {
	delay time.Duration
	fire  chan time.Time
}

func newOperatorTestClock() *operatorTestClock {
	return &operatorTestClock{now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), waits: make(chan operatorTestWait, 64)}
}

func (self *operatorTestClock) after(delay time.Duration) <-chan time.Time {
	wait := operatorTestWait{delay: delay, fire: make(chan time.Time, 1)}
	self.waits <- wait
	return wait.fire
}

func (self *operatorTestClock) Now() time.Time {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.now
}

func (self *operatorTestClock) advance(delay time.Duration) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.now = self.now.Add(delay)
}

func (self *operatorTestClock) next(t *testing.T) operatorTestWait {
	t.Helper()
	select {
	case wait := <-self.waits:
		return wait
	case <-time.After(operatorTestTimeout):
		t.Fatal("no wait was requested")
		return operatorTestWait{}
	}
}

func (self *operatorTestClock) fire(wait operatorTestWait) {
	self.advance(wait.delay)
	wait.fire <- self.Now()
}

type operatorTestLog struct {
	lines chan string
}

func newOperatorTestLog() *operatorTestLog {
	return &operatorTestLog{lines: make(chan string, 1024)}
}

func (self *operatorTestLog) log(line string) {
	select {
	case self.lines <- line:
	default:
	}
}

// The first line containing want, skipping others.
func (self *operatorTestLog) wait(t *testing.T, want string) string {
	t.Helper()
	timeout := time.After(operatorTestTimeout)
	for {
		select {
		case line := <-self.lines:
			if strings.Contains(line, want) {
				return line
			}
		case <-timeout:
			t.Fatalf("no log line contains %q", want)
			return ""
		}
	}
}

func writeOperatorTestJwt(t *testing.T, path string) {
	t.Helper()
	if err := clientauth.WriteToken(path, "synthetic-network-jwt"); err != nil {
		t.Fatal(err)
	}
}

func operatorTestHotkey(t *testing.T, fill byte) *crv4.Keypair {
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

func writeOperatorTestHotkeySeed(t *testing.T, path string, fill byte) {
	t.Helper()
	seed := strings.Repeat(hex.EncodeToString([]byte{fill}), 32)
	if err := os.WriteFile(path, []byte("0x"+seed+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// A private directory on a path without symlinks, which the seed and
// registration custody require.
func operatorTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Serves a list naming the given operators.
func newOperatorTestListServer(t *testing.T, operators ...operatorlist.Operator) *httptest.Server {
	t.Helper()
	raw := "schema: " + operatorlist.Schema + "\noperators:\n"
	for _, operator := range operators {
		raw += fmt.Sprintf("  - domain: %s\n    api_url: %s\n    connect_url: %s\n", operator.Domain, operator.ApiUrl, operator.ConnectUrl)
	}
	if _, err := operatorlist.Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/yaml")
		_, _ = writer.Write([]byte(raw))
	}))
	t.Cleanup(server.Close)
	return server
}

// A fake operator's wallet sign-in, modeled on hotkeyauth's test operator:
// single-use challenges, sr25519 signatures in the substrate context over the
// <Bytes>-wrapped message, and "jwt:" plus the network name as the JWT.
type operatorTestSignIn struct {
	stateLock  sync.Mutex
	challenges map[string]bool
	// wallet address to network name
	networks map[string]string
	created  int
}

func newOperatorTestSignIn(t *testing.T) (*operatorTestSignIn, *httptest.Server) {
	t.Helper()
	self := &operatorTestSignIn{challenges: map[string]bool{}, networks: map[string]string{}}
	server := httptest.NewServer(self)
	t.Cleanup(server.Close)
	return self, server
}

type operatorTestWalletAuth struct {
	WalletAddress   string `json:"wallet_address"`
	WalletSignature string `json:"wallet_signature"`
	WalletMessage   string `json:"wallet_message"`
	Blockchain      string `json:"blockchain"`
}

func (self *operatorTestSignIn) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var body struct {
		WalletAuth  *operatorTestWalletAuth `json:"wallet_auth"`
		NetworkName string                  `json:"network_name"`
		Terms       bool                    `json:"terms"`
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
		value := make([]byte, 24)
		_, _ = rand.Read(value)
		challenge := hex.EncodeToString(value)
		self.challenges[challenge] = false
		answer(http.StatusOK, map[string]any{"message_template": fmt.Sprintf("Sign in to URnetwork\nChallenge: %s\nTimestamp: %d", challenge, 1791244800)})
	case "/auth/login":
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
		if err := self.use(body.WalletAuth); err != nil || !body.Terms {
			answer(http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "401 wallet auth"}})
			return
		}
		if _, ok := self.networks[body.WalletAuth.WalletAddress]; ok {
			answer(http.StatusConflict, map[string]any{"error": map[string]any{"message": "user exists"}})
			return
		}
		self.networks[body.WalletAuth.WalletAddress] = body.NetworkName
		self.created++
		answer(http.StatusOK, map[string]any{"network": map[string]any{"by_jwt": "jwt:" + body.NetworkName, "network_name": body.NetworkName}})
	default:
		http.NotFound(writer, request)
	}
}

// Consumes the challenge and verifies the wrapped signature as an operator does.
func (self *operatorTestSignIn) use(auth *operatorTestWalletAuth) error {
	if auth == nil || auth.Blockchain != "TAO" {
		return errors.New("401 wallet auth")
	}
	lines := strings.Split(auth.WalletMessage, "\n")
	if len(lines) != 3 {
		return errors.New("400 invalid message format")
	}
	challenge := strings.TrimPrefix(lines[1], "Challenge: ")
	if used, ok := self.challenges[challenge]; !ok || used {
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
	ok, err := public.Verify(signature, schnorrkel.NewSigningContext([]byte("substrate"), []byte("<Bytes>"+auth.WalletMessage+"</Bytes>")))
	if err != nil || !ok {
		return errors.New("401 invalid signature")
	}
	return nil
}

func (self *operatorTestSignIn) networkCount() int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.created
}
