//go:build linux || darwin

package miner

import (
	"bytes"
	"context"
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

	gojwt "github.com/golang-jwt/jwt/v5"
	"golang.org/x/sys/unix"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
)

var renewalTestBase = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

const (
	renewalTestNetworkId = "00000000-0000-0000-0000-000000000301"
	renewalTestUserId    = "00000000-0000-0000-0000-000000000401"
)

func renewalTestJwt(t *testing.T, issued time.Time, expires time.Time, marker string) string {
	t.Helper()
	claims := gojwt.MapClaims{"network_id": renewalTestNetworkId, "user_id": renewalTestUserId, "marker": marker}
	if !issued.IsZero() {
		claims["iat"] = issued.Unix()
	}
	if !expires.IsZero() {
		claims["exp"] = expires.Unix()
	}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// a state directory without symlinked components, which the token custody
// requires (macOS TMPDIR is under the /var symlink)
func renewalTestStateDir(t *testing.T) string {
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

type renewalTestClock struct{ now time.Time }

func (self *renewalTestClock) Now() time.Time { return self.now }

func (self *renewalTestClock) After(time.Duration) (<-chan time.Time, func()) {
	c := make(chan time.Time, 1)
	c <- self.now
	return c, func() {}
}

type renewalTestRefresh struct {
	stateLock sync.Mutex
	calls     []string
	answer    func(byJwt string) (*sdk.RefreshJwtResult, error)
}

func (self *renewalTestRefresh) refresh(_ context.Context, byJwt string) (*sdk.RefreshJwtResult, error) {
	self.stateLock.Lock()
	self.calls = append(self.calls, byJwt)
	answer := self.answer
	self.stateLock.Unlock()
	return answer(byJwt)
}

func (self *renewalTestRefresh) count() int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return len(self.calls)
}

type renewalTestFixture struct {
	path    string
	clock   *renewalTestClock
	refresh *renewalTestRefresh
	sink    *providerDiagnosticRecorder
	renewer *networkTokenRenewer
}

func newRenewalTestFixture(t *testing.T, token string) *renewalTestFixture {
	t.Helper()
	dir := renewalTestStateDir(t)
	self := &renewalTestFixture{
		path:    filepath.Join(dir, "jwt"),
		clock:   &renewalTestClock{now: renewalTestBase},
		refresh: &renewalTestRefresh{},
		sink:    &providerDiagnosticRecorder{records: make(chan []byte, 64)},
	}
	if token != "" {
		if err := clientauth.WriteNetworkToken(self.path, token); err != nil {
			t.Fatal(err)
		}
	}
	self.renewer = newNetworkTokenRenewer(self.path, self.refresh.refresh, newProviderDiagnosticTestOwner(t, self.sink))
	self.renewer.clock = self.clock
	self.renewer.jitter = func(time.Duration) time.Duration { return 0 }
	return self
}

func (self *renewalTestFixture) token(t *testing.T) string {
	t.Helper()
	token, err := clientauth.ReadToken(self.path)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// the next diagnostic record's code and guidance
func (self *renewalTestFixture) event(t *testing.T) (code string, guidance string) {
	t.Helper()
	var record struct {
		Code     string `json:"event"`
		Guidance string `json:"guidance"`
		RetryMs  uint64 `json:"retry_ms"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(self.sink.next(t)), &record); err != nil {
		t.Fatal(err)
	}
	return record.Code, record.Guidance
}

func (self *renewalTestFixture) noEvent(t *testing.T) {
	t.Helper()
	select {
	case raw := <-self.sink.records:
		t.Fatalf("unexpected diagnostic %s", raw)
	case <-time.After(50 * time.Millisecond):
	}
}

func renewalTestAnswer(byJwt string) func(string) (*sdk.RefreshJwtResult, error) {
	return func(string) (*sdk.RefreshJwtResult, error) { return &sdk.RefreshJwtResult{ByJwt: byJwt}, nil }
}

func TestNetworkTokenRenewalTimeout(t *testing.T) {
	day := 24 * time.Hour
	now := renewalTestBase
	for name, c := range map[string]struct {
		token string
		want  time.Duration
	}{
		"half-life":      {renewalTestJwt(t, now, now.Add(30*day), ""), 15 * day},
		"past half-life": {renewalTestJwt(t, now.Add(-20*day), now.Add(10*day), ""), networkTokenRenewalFloor},
		"near expiry":    {renewalTestJwt(t, now.Add(-30*day), now.Add(time.Minute), ""), networkTokenRenewalFloor},
		"expired":        {renewalTestJwt(t, now.Add(-40*day), now.Add(-10*day), ""), 0},
		"no expiration":  {renewalTestJwt(t, now, time.Time{}, ""), 0},
		"no issued time": {renewalTestJwt(t, time.Time{}, now.Add(30*day), ""), 15 * day},
		"issued after":   {renewalTestJwt(t, now.Add(40*day), now.Add(30*day), ""), 15 * day},
		"not a jwt":      {"network-jwt", 0},
		"short lifetime": {renewalTestJwt(t, now, now.Add(4*time.Minute), ""), networkTokenRenewalFloor},
	} {
		if got := networkTokenRenewalTimeout(c.token, now); got != c.want {
			t.Errorf("%s: timeout = %s, want %s", name, got, c.want)
		}
	}
}

func TestNetworkTokenRenewalBackoffsAreBoundedAndFloored(t *testing.T) {
	full := func(interval time.Duration) time.Duration { return interval }
	none := func(time.Duration) time.Duration { return 0 }
	wild := func(time.Duration) time.Duration { return -time.Hour }
	previous := time.Duration(0)
	for failures := 1; failures <= 64; failures++ {
		got := networkTokenRenewalRetryTimeout(failures, full)
		if got < previous || got > networkTokenRenewalMinimumRetry+networkTokenRenewalMaximumRetry {
			t.Fatalf("failures %d: retry %s after %s", failures, got, previous)
		}
		previous = got
		if floor := networkTokenRenewalRetryTimeout(failures, none); floor != networkTokenRenewalMinimumRetry {
			t.Fatalf("failures %d: retry without jitter = %s", failures, floor)
		}
		if negative := networkTokenRenewalRetryTimeout(failures, wild); negative != networkTokenRenewalMinimumRetry {
			t.Fatalf("failures %d: a negative jitter shortened the retry to %s", failures, negative)
		}
	}
	if networkTokenRenewalRetryTimeout(1, full) != networkTokenRenewalMinimumRetry+networkTokenRenewalJitterBase {
		t.Fatal("the first retry interval is not the jitter base")
	}
	if networkTokenRenewalAnomalyTimeout(1) != networkTokenRenewalFloor || networkTokenRenewalAnomalyTimeout(2) != 2*networkTokenRenewalFloor || networkTokenRenewalAnomalyTimeout(64) != networkTokenRenewalMaximumAnomaly {
		t.Fatal("anomaly spacing does not double from the floor to its bound")
	}
	for i := 0; i < 64; i++ {
		if jitter := networkTokenRenewalJitter(time.Minute); jitter < 0 || jitter >= time.Minute {
			t.Fatalf("jitter %s outside [0, 1m)", jitter)
		}
	}
	if networkTokenRenewalJitter(0) != 0 {
		t.Fatal("jitter of an empty interval")
	}
}

func TestNetworkTokenRenewalRefusedClassification(t *testing.T) {
	for status, refused := range map[int]bool{
		http.StatusNotFound: true, http.StatusForbidden: true, http.StatusBadRequest: true, http.StatusMethodNotAllowed: true,
		http.StatusUnauthorized: false, http.StatusRequestTimeout: false, http.StatusTooEarly: false, http.StatusTooManyRequests: false,
		http.StatusServiceUnavailable: false, http.StatusFound: false,
	} {
		if got := networkTokenRenewalRefused(fmt.Errorf("wrapped: %w", &connect.HttpStatusError{StatusCode: status})); got != refused {
			t.Errorf("status %d: refused = %t", status, got)
		}
	}
	if networkTokenRenewalRefused(errors.New("dial failed")) {
		t.Error("a transport error was refused")
	}
}

func TestNetworkTokenRenewerRenewsAtTheHalfLifeAndPersists(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "sign-in")
	fixture := newRenewalTestFixture(t, signIn)
	renewed := renewalTestJwt(t, renewalTestBase.Add(15*day), renewalTestBase.Add(45*day), "renewed")
	fixture.refresh.answer = renewalTestAnswer(renewed)

	if wait := fixture.renewer.pass(t.Context()); wait != networkTokenRenewalMaximumWait || fixture.refresh.count() != 0 {
		t.Fatalf("a fresh sign-in renewed early: wait %s, %d calls", wait, fixture.refresh.count())
	}
	fixture.clock.now = renewalTestBase.Add(15*day - time.Hour)
	fixture.renewer.pass(t.Context())
	if fixture.refresh.count() != 0 {
		t.Fatal("renewed before the half-life")
	}
	fixture.clock.now = renewalTestBase.Add(15 * day)
	wait := fixture.renewer.pass(t.Context())
	if fixture.refresh.count() != 1 || fixture.refresh.calls[0] != signIn {
		t.Fatalf("renewal calls = %d", fixture.refresh.count())
	}
	if got := fixture.token(t); got != renewed {
		t.Fatal("the renewal was not persisted")
	}
	if code, _ := fixture.event(t); code != "network_sign_in_renewed" {
		t.Fatalf("event = %s", code)
	}
	if wait != networkTokenRenewalMaximumWait || !fixture.renewer.scheduledTime.Equal(fixture.clock.now.Add(15*day)) {
		t.Fatalf("next renewal at %s (wait %s)", fixture.renewer.scheduledTime, wait)
	}
	// the renewal is not read as another sign-in
	fixture.renewer.pass(t.Context())
	if fixture.refresh.count() != 1 {
		t.Fatal("the renewer renewed its own renewal at once")
	}
}

func TestNetworkTokenRenewerRenewsExpiredAndLegacyAtOnce(t *testing.T) {
	day := 24 * time.Hour
	for name, signIn := range map[string]string{
		"expired":       renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired"),
		"no expiration": renewalTestJwt(t, renewalTestBase.Add(-40*day), time.Time{}, "legacy"),
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newRenewalTestFixture(t, signIn)
			renewed := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "renewed")
			fixture.refresh.answer = renewalTestAnswer(renewed)
			fixture.renewer.pass(t.Context())
			if fixture.refresh.count() != 1 || fixture.token(t) != renewed {
				t.Fatalf("%s token was not renewed at once", name)
			}
		})
	}
}

// A rejected sign-in names the recovery, and the renewer leaves the token
// alone until a new sign-in replaces it.
func TestNetworkTokenRenewerStopsOnARejectedSignIn(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
	fixture := newRenewalTestFixture(t, signIn)
	fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
		return nil, &connect.HttpStatusError{StatusCode: http.StatusUnauthorized}
	}
	if wait := fixture.renewer.pass(t.Context()); wait != networkTokenRenewalRecheck {
		t.Fatalf("wait after a rejection = %s", wait)
	}
	code, guidance := fixture.event(t)
	if code != "network_sign_in_rejected" || !strings.Contains(guidance, "`provider auth`") || !strings.Contains(guidance, "--operator=<domain>") {
		t.Fatalf("event %s guidance %q", code, guidance)
	}
	for range 5 {
		fixture.clock.now = fixture.clock.now.Add(time.Hour)
		fixture.renewer.pass(t.Context())
	}
	if fixture.refresh.count() != 1 {
		t.Fatalf("a rejected sign-in was sent again (%d calls)", fixture.refresh.count())
	}
	fixture.noEvent(t)

	// a new sign-in is scheduled and renewed on its own terms
	newSignIn := renewalTestJwt(t, fixture.clock.now.Add(-20*day), fixture.clock.now.Add(10*day), "new sign-in")
	if err := clientauth.WriteNetworkToken(fixture.path, newSignIn); err != nil {
		t.Fatal(err)
	}
	renewed := renewalTestJwt(t, fixture.clock.now, fixture.clock.now.Add(30*day), "renewed")
	fixture.refresh.answer = renewalTestAnswer(renewed)
	fixture.renewer.pass(t.Context())
	if fixture.token(t) == newSignIn {
		// past its half-life it is renewed at the floor
		fixture.clock.now = fixture.clock.now.Add(networkTokenRenewalFloor)
		fixture.renewer.pass(t.Context())
	}
	if fixture.token(t) != renewed || fixture.refresh.calls[len(fixture.refresh.calls)-1] != newSignIn {
		t.Fatal("the new sign-in was not renewed")
	}
}

func TestNetworkTokenRenewerStopsOnARefusalOrAnotherIdentity(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
	for name, answer := range map[string]func(string) (*sdk.RefreshJwtResult, error){
		"refusal": func(string) (*sdk.RefreshJwtResult, error) {
			return &sdk.RefreshJwtResult{Error: &sdk.RefreshJwtResultError{Message: "refused"}}, nil
		},
		"another network": renewalTestAnswer(func() string {
			token, _ := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000302", "user_id": renewalTestUserId}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
			return token
		}()),
		"a client token": renewalTestAnswer(func() string {
			token, _ := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": renewalTestNetworkId, "user_id": renewalTestUserId, "client_id": "00000000-0000-0000-0000-000000000101", "device_id": "00000000-0000-0000-0000-000000000202"}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
			return token
		}()),
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newRenewalTestFixture(t, signIn)
			fixture.refresh.answer = answer
			fixture.renewer.pass(t.Context())
			if code, guidance := fixture.event(t); code != "network_sign_in_renewal_stopped" || !strings.Contains(guidance, "`provider auth`") {
				t.Fatalf("event %s guidance %q", code, guidance)
			}
			fixture.clock.now = fixture.clock.now.Add(time.Hour)
			fixture.renewer.pass(t.Context())
			if fixture.refresh.count() != 1 || fixture.token(t) != signIn {
				t.Fatalf("%s: calls %d, token replaced=%t", name, fixture.refresh.count(), fixture.token(t) != signIn)
			}
		})
	}
}

func TestNetworkTokenRenewerBacksOffFailures(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
	fixture := newRenewalTestFixture(t, signIn)
	fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
		return nil, &sdk.ClientControlUnavailableError{}
	}
	previous := time.Duration(0)
	for attempt := 1; attempt <= 8; attempt++ {
		wait := fixture.renewer.pass(t.Context())
		if fixture.refresh.count() != attempt {
			t.Fatalf("attempt %d: calls %d", attempt, fixture.refresh.count())
		}
		if code, _ := fixture.event(t); code != "network_sign_in_renewal_retry" {
			t.Fatalf("event %s", code)
		}
		if wait < networkTokenRenewalMinimumRetry || wait < previous {
			t.Fatalf("attempt %d: wait %s after %s", attempt, wait, previous)
		}
		// a pass before the retry time sends nothing
		fixture.clock.now = fixture.clock.now.Add(wait - time.Second)
		fixture.renewer.pass(t.Context())
		if fixture.refresh.count() != attempt {
			t.Fatal("retried before its backoff")
		}
		fixture.clock.now = fixture.clock.now.Add(time.Second)
		previous = wait
	}

	// a refused route (a server that predates it) waits a day
	fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
		return nil, &connect.HttpStatusError{StatusCode: http.StatusNotFound}
	}
	calls := fixture.refresh.count()
	fixture.renewer.pass(t.Context())
	fixture.event(t)
	for range 3 {
		fixture.clock.now = fixture.clock.now.Add(6 * time.Hour)
		fixture.renewer.pass(t.Context())
	}
	if fixture.refresh.count() != calls+1 {
		t.Fatalf("a refused renewal was retried within a day (%d calls)", fixture.refresh.count()-calls)
	}
	fixture.clock.now = fixture.clock.now.Add(6 * time.Hour)
	fixture.renewer.pass(t.Context())
	if fixture.refresh.count() != calls+2 {
		t.Fatal("a refused renewal was not retried after a day")
	}
}

// A held owner lock defers only the write: the server's answer is kept and
// written later, without asking the server again.
func TestNetworkTokenRenewerRetriesOnlyTheWriteWhileBusy(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
	fixture := newRenewalTestFixture(t, signIn)
	renewed := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "renewed")
	fixture.refresh.answer = renewalTestAnswer(renewed)
	lock, err := os.OpenFile(fixture.path+".registration.lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	wait := fixture.renewer.pass(t.Context())
	if code, _ := fixture.event(t); code != "network_sign_in_renewal_retry" || wait < networkTokenRenewalMinimumRetry || fixture.token(t) != signIn {
		t.Fatalf("busy renewal: event %s wait %s", code, wait)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.clock.now = fixture.clock.now.Add(wait)
	fixture.renewer.pass(t.Context())
	if fixture.refresh.count() != 1 || fixture.token(t) != renewed {
		t.Fatalf("the deferred write: calls %d, renewed=%t", fixture.refresh.count(), fixture.token(t) == renewed)
	}
}

// A sign-in that replaces the file during the request wins; the renewal of
// the old token is discarded.
func TestNetworkTokenRenewerDiscardsARenewalAfterAnotherSignIn(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
	fixture := newRenewalTestFixture(t, signIn)
	explicit := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "explicit")
	renewed := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "renewed")
	fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
		if err := clientauth.WriteNetworkToken(fixture.path, explicit); err != nil {
			t.Error(err)
		}
		return &sdk.RefreshJwtResult{ByJwt: renewed}, nil
	}
	if wait := fixture.renewer.pass(t.Context()); wait != time.Second {
		t.Fatalf("wait after a lost renewal = %s", wait)
	}
	if fixture.token(t) != explicit {
		t.Fatal("a renewal overwrote the newer sign-in")
	}
	fixture.renewer.pass(t.Context())
	if fixture.renewer.scheduledToken != explicit || fixture.refresh.count() != 1 {
		t.Fatal("the newer sign-in was not scheduled on its own terms")
	}
}

// A renewal that answers a token itself due at once is spaced, doubling from
// the floor, so neither a server nor a clock can make renewal a loop.
func TestNetworkTokenRenewerSpacesRenewalsDueAtOnce(t *testing.T) {
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*24*time.Hour), time.Time{}, "legacy")
	fixture := newRenewalTestFixture(t, signIn)
	count := 0
	fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
		count++
		return &sdk.RefreshJwtResult{ByJwt: renewalTestJwt(t, fixture.clock.now, time.Time{}, fmt.Sprintf("renewed %d", count))}, nil
	}
	want := networkTokenRenewalFloor
	for renewal := 1; renewal <= 6; renewal++ {
		wait := fixture.renewer.pass(t.Context())
		fixture.event(t)
		if fixture.refresh.count() != renewal || wait != min(want, networkTokenRenewalMaximumWait) {
			t.Fatalf("renewal %d: calls %d wait %s, want %s", renewal, fixture.refresh.count(), wait, want)
		}
		fixture.clock.now = fixture.clock.now.Add(want - time.Second)
		fixture.renewer.pass(t.Context())
		if fixture.refresh.count() != renewal {
			t.Fatal("a renewal due at once was renewed before its spacing")
		}
		fixture.clock.now = fixture.clock.now.Add(time.Second)
		want *= 2
	}
}

func TestNetworkTokenRenewerLeavesApiKeysAndMissingFilesAlone(t *testing.T) {
	fixture := newRenewalTestFixture(t, "")
	if wait := fixture.renewer.pass(t.Context()); wait != networkTokenRenewalRecheck || fixture.refresh.count() != 0 {
		t.Fatalf("missing file: wait %s calls %d", wait, fixture.refresh.count())
	}
	fixture.noEvent(t)
	if err := clientauth.WriteNetworkToken(fixture.path, "urn_"+strings.Repeat("a", 52)); err != nil {
		t.Fatal(err)
	}
	fixture.renewer.pass(t.Context())
	if code, _ := fixture.event(t); code != "network_sign_in_renewal_stopped" {
		t.Fatalf("api key event %s", code)
	}
	for range 3 {
		fixture.clock.now = fixture.clock.now.Add(time.Hour)
		fixture.renewer.pass(t.Context())
	}
	fixture.noEvent(t)
	if fixture.refresh.count() != 0 {
		t.Fatal("an api key was sent for renewal")
	}
}

// The loop waits at least a second on every path and stops with its context.
func TestNetworkTokenRenewerRunNeverHotLoops(t *testing.T) {
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*24*time.Hour), time.Time{}, "legacy")
	fixture := newRenewalTestFixture(t, signIn)
	fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
		return nil, &sdk.ClientControlUnavailableError{}
	}
	ctx, cancel := context.WithCancel(t.Context())
	waits := make(chan time.Duration, 1024)
	fixture.renewer.afterPass = func(wait time.Duration) {
		waits <- wait
		fixture.clock.now = fixture.clock.now.Add(wait)
		if len(waits) >= 32 {
			cancel()
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fixture.renewer.run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the renewer did not stop with its context")
	}
	close(waits)
	for wait := range waits {
		if wait < time.Second {
			t.Fatalf("a pass waited %s", wait)
		}
	}
	for len(fixture.sink.records) > 0 {
		<-fixture.sink.records
	}
}

// The run's renewer reaches a real API over the first provider's transport,
// sends the network token to /auth/network-refresh, and persists the renewal.
func TestStartNetworkTokenRenewalRenewsOverTheApi(t *testing.T) {
	day := 24 * time.Hour
	now := time.Now()
	signIn := renewalTestJwt(t, now.Add(-40*day), now.Add(-10*day), "expired")
	renewed := renewalTestJwt(t, now, now.Add(30*day), "renewed")
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/hello":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/auth/network-refresh":
			requests <- r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"by_jwt":%q}`, renewed)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := renewalTestStateDir(t)
	path := filepath.Join(dir, "jwt")
	if err := clientauth.WriteNetworkToken(path, signIn); err != nil {
		t.Fatal(err)
	}
	sink := &providerDiagnosticRecorder{records: make(chan []byte, 16)}
	output := newProviderDiagnosticTestOwner(t, sink)
	started := make(chan *networkTokenRenewer, 1)
	ctx := context.WithValue(t.Context(), networkTokenRenewalHooksKey{}, networkTokenRenewalHooks{afterStart: func(renewer *networkTokenRenewer) { started <- renewer }})
	stop := startNetworkTokenRenewal(ctx, server.URL, nil, nil, path, output, false, nil)
	if renewer := <-started; renewer.path != path {
		t.Fatalf("renewer path %s", renewer.path)
	}
	select {
	case authorization := <-requests:
		if authorization != "Bearer "+signIn {
			t.Fatalf("authorization = %q", authorization)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the renewal request did not reach the API")
	}
	if !bytes.Contains(sink.next(t), []byte(`"event":"network_sign_in_renewed"`)) {
		t.Fatal("the renewal was not reported")
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if token, err := clientauth.ReadToken(path); err != nil || token != renewed {
		t.Fatalf("token after the run's renewal: %v", err)
	}
}

func TestProviderAuthenticationRecoveryEventNamesARejectedSignIn(t *testing.T) {
	rejected := fmt.Errorf("provider client identity requires recovery: %w", &clientauth.NetworkCredentialRejectedError{})
	if providerAuthenticationRecoveryEvent(rejected) != providerNetworkSignInRejected {
		t.Fatal("a rejected sign-in was reported as custody recovery")
	}
	if providerAuthenticationRecoveryEvent(errors.New("custody")) != providerStartupRecoveryRequired {
		t.Fatal("custody recovery lost its event")
	}
	sink := &providerDiagnosticRecorder{records: make(chan []byte, 8)}
	output := newProviderDiagnosticTestOwner(t, sink)
	for event, code := range map[providerDiagnosticEvent]string{
		providerNetworkSignInRejected:      "network_sign_in_rejected",
		providerNetworkTokenRenewed:        "network_sign_in_renewed",
		providerNetworkTokenRenewalWait:    "network_sign_in_renewal_retry",
		providerNetworkTokenRenewalStopped: "network_sign_in_renewal_stopped",
	} {
		output.observe(event, 0, false, nil, time.Minute, nil)
		raw := sink.next(t)
		if !bytes.Contains(raw, []byte(`"domain":"authentication"`)) || !bytes.Contains(raw, []byte(`"event":"`+code+`"`)) {
			t.Fatalf("event %d: %s", event, raw)
		}
		hasGuidance := bytes.Contains(raw, []byte(`"guidance"`))
		if wantGuidance := event == providerNetworkSignInRejected || event == providerNetworkTokenRenewalStopped; hasGuidance != wantGuidance {
			t.Fatalf("event %s guidance present=%t", code, hasGuidance)
		}
	}
}

// An operator child of provide --all-operators --auto-register sets a sign-in
// the renewal call rejects aside, names the file, and ends: the supervisor
// signs in again. The rejected token is never sent again.
func TestNetworkTokenRenewerQuarantinesARejectedSignIn(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
	fixture := newRenewalTestFixture(t, signIn)
	var quarantines []clientauth.NetworkTokenQuarantine
	fixture.renewer.quarantineRejected = true
	fixture.renewer.onQuarantined = func(quarantine clientauth.NetworkTokenQuarantine) {
		quarantines = append(quarantines, quarantine)
	}
	fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
		return nil, &connect.HttpStatusError{StatusCode: http.StatusUnauthorized}
	}
	fixture.renewer.pass(t.Context())
	if len(quarantines) != 1 {
		t.Fatalf("quarantines = %d", len(quarantines))
	}
	raw := bytes.TrimSpace(fixture.sink.next(t))
	var record struct {
		Event    string `json:"event"`
		Guidance string `json:"guidance"`
		File     string `json:"file"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record.Event != "network_sign_in_quarantined" || record.File != quarantines[0].Path || !strings.Contains(record.Guidance, quarantines[0].Path) || !strings.Contains(record.Guidance, "signs in again with the hotkey") || bytes.Contains(raw, []byte(signIn)) {
		t.Fatalf("diagnostic %s", raw)
	}
	if _, err := os.Stat(fixture.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the rejected sign-in is still in place: %v", err)
	}
	if token, _ := clientauth.ReadToken(quarantines[0].Path); token != signIn {
		t.Fatal("the set-aside file does not hold the rejected sign-in")
	}
	for range 3 {
		fixture.clock.now = fixture.clock.now.Add(time.Hour)
		fixture.renewer.pass(t.Context())
	}
	if fixture.refresh.count() != 1 || len(quarantines) != 1 {
		t.Fatalf("after the quarantine: %d calls, %d quarantines", fixture.refresh.count(), len(quarantines))
	}
	fixture.noEvent(t)
}

// Only a confirmed rejection of the renewal call sets the sign-in aside;
// transient failures, other statuses, refusals and another identity keep
// their own handling, and without the flag a rejection stops as before.
func TestNetworkTokenRenewerQuarantinesOnlyARejection(t *testing.T) {
	day := 24 * time.Hour
	for name, c := range map[string]struct {
		quarantine bool
		answer     func(string) (*sdk.RefreshJwtResult, error)
		event      string
	}{
		"unavailable": {true, func(string) (*sdk.RefreshJwtResult, error) { return nil, &sdk.ClientControlUnavailableError{} }, "network_sign_in_renewal_retry"},
		"not found": {true, func(string) (*sdk.RefreshJwtResult, error) {
			return nil, &connect.HttpStatusError{StatusCode: http.StatusNotFound}
		}, "network_sign_in_renewal_retry"},
		"forbidden": {true, func(string) (*sdk.RefreshJwtResult, error) {
			return nil, &connect.HttpStatusError{StatusCode: http.StatusForbidden}
		}, "network_sign_in_renewal_retry"},
		"refusal": {true, func(string) (*sdk.RefreshJwtResult, error) {
			return &sdk.RefreshJwtResult{Error: &sdk.RefreshJwtResultError{Message: "refused"}}, nil
		}, "network_sign_in_renewal_stopped"},
		"another identity": {true, func(string) (*sdk.RefreshJwtResult, error) {
			token, _ := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000302", "user_id": renewalTestUserId}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
			return &sdk.RefreshJwtResult{ByJwt: token}, nil
		}, "network_sign_in_renewal_stopped"},
		"rejected without the flag": {false, func(string) (*sdk.RefreshJwtResult, error) {
			return nil, &connect.HttpStatusError{StatusCode: http.StatusUnauthorized}
		}, "network_sign_in_rejected"},
	} {
		t.Run(name, func(t *testing.T) {
			signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
			fixture := newRenewalTestFixture(t, signIn)
			fixture.renewer.quarantineRejected = c.quarantine
			fixture.renewer.onQuarantined = func(clientauth.NetworkTokenQuarantine) {
				t.Error("set aside a sign-in that was not rejected under the flag")
			}
			fixture.refresh.answer = c.answer
			fixture.renewer.pass(t.Context())
			if code, _ := fixture.event(t); code != c.event {
				t.Fatalf("event %s, want %s", code, c.event)
			}
			if token, err := clientauth.ReadToken(fixture.path); err != nil || token != signIn {
				t.Fatalf("the sign-in moved: %v", err)
			}
			if _, ok, err := clientauth.LatestNetworkTokenQuarantine(fixture.path); err != nil || ok {
				t.Fatalf("a set-aside file appeared: ok=%t err=%v", ok, err)
			}
		})
	}
}

// A held owner lock defers only the move; a sign-in that lands meanwhile wins
// and is never set aside.
func TestNetworkTokenRenewerQuarantineWaitsForTheOwnerLock(t *testing.T) {
	day := 24 * time.Hour
	for _, signInMeanwhile := range []bool{false, true} {
		t.Run(fmt.Sprintf("sign-in meanwhile %t", signInMeanwhile), func(t *testing.T) {
			signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
			fixture := newRenewalTestFixture(t, signIn)
			quarantined := 0
			fixture.renewer.quarantineRejected = true
			fixture.renewer.onQuarantined = func(clientauth.NetworkTokenQuarantine) { quarantined++ }
			fixture.refresh.answer = func(string) (*sdk.RefreshJwtResult, error) {
				return nil, &connect.HttpStatusError{StatusCode: http.StatusUnauthorized}
			}
			lock, err := os.OpenFile(fixture.path+".registration.lock", os.O_RDWR|os.O_CREATE, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			wait := fixture.renewer.pass(t.Context())
			if code, _ := fixture.event(t); code != "network_sign_in_renewal_retry" || quarantined != 0 {
				t.Fatalf("busy quarantine: event %s, quarantined %d", code, quarantined)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			newSignIn := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "new sign-in")
			if signInMeanwhile {
				if err := clientauth.WriteNetworkToken(fixture.path, newSignIn); err != nil {
					t.Fatal(err)
				}
			}
			fixture.clock.now = fixture.clock.now.Add(wait)
			fixture.renewer.pass(t.Context())
			if fixture.refresh.count() != 1 {
				t.Fatalf("the rejected sign-in was sent again (%d calls)", fixture.refresh.count())
			}
			_, set, _ := clientauth.LatestNetworkTokenQuarantine(fixture.path)
			token, _ := clientauth.ReadToken(fixture.path)
			if signInMeanwhile {
				if set || quarantined != 0 || token != newSignIn {
					t.Fatalf("a newer sign-in was set aside: set=%t quarantined=%d", set, quarantined)
				}
			} else if !set || quarantined != 1 || token != "" {
				t.Fatalf("the deferred move: set=%t quarantined=%d", set, quarantined)
			}
		})
	}
}

// The run's renewer, under the flag, sets the sign-in a real API rejects
// aside and tells the run.
func TestStartNetworkTokenRenewalQuarantinesOverTheApi(t *testing.T) {
	day := 24 * time.Hour
	now := time.Now()
	signIn := renewalTestJwt(t, now.Add(-40*day), now.Add(-10*day), "expired")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			w.WriteHeader(http.StatusOK)
		case "/auth/network-refresh":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := renewalTestStateDir(t)
	path := filepath.Join(dir, "jwt")
	if err := clientauth.WriteNetworkToken(path, signIn); err != nil {
		t.Fatal(err)
	}
	output := newProviderDiagnosticTestOwner(t, &providerDiagnosticRecorder{records: make(chan []byte, 16)})
	quarantined := make(chan clientauth.NetworkTokenQuarantine, 1)
	stop := startNetworkTokenRenewal(t.Context(), server.URL, nil, nil, path, output, true, func(quarantine clientauth.NetworkTokenQuarantine) { quarantined <- quarantine })
	select {
	case quarantine := <-quarantined:
		if token, _ := clientauth.ReadToken(quarantine.Path); token != signIn {
			t.Fatal("the set-aside file does not hold the rejected sign-in")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the rejected sign-in was not set aside")
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

func TestProviderRunExitCodeNamesAQuarantine(t *testing.T) {
	if code, ok := providerRunExitCode(errors.Join(errors.New("worker"), errProviderNetworkSignInQuarantined)); !ok || code != providerQuarantinedExitCode {
		t.Fatalf("quarantine exit = %d %t", code, ok)
	}
	for _, err := range []error{nil, errors.New("worker"), context.Canceled} {
		if _, ok := providerRunExitCode(err); ok {
			t.Fatalf("%v asked for an exit code", err)
		}
	}
}
