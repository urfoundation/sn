package miner

// The provider keeps its network sign-in renewed (miner/PROVIDER-REGISTRATION.md,
// "Network sign-in renewal").
//
// Day to day a provider runs on its client tokens, which refresh themselves.
// The network token in the state directory's jwt is read again to register a
// client (a new slot, an interrupted registration), and under
// provide --all-operators the supervisor's hourly hotkey wallet upkeep sends
// it. It expires 30 days after its sign-in, so the provide process that owns
// the state directory renews it at its half-life (POST /auth/network-refresh),
// on the schedule the SDK uses for the apps' sign-in tokens. The renewal is
// written with clientauth.RenewNetworkToken, which keeps every revoked client
// blocked (clientauth/network_token.go).

import (
	"context"
	"errors"
	mathrand "math/rand"
	"net/http"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
)

const (
	// the earliest scheduled renewal after the token's half-life is read,
	// like the SDK's minRefreshTimeout: a schedule derived from a server or
	// clock value needs a floor, or it is one change away from a hot loop
	networkTokenRenewalFloor = 5 * time.Minute
	// the token file is read again at least this often, so a sign-in that
	// replaced it is scheduled within this long
	networkTokenRenewalMaximumWait = 6 * time.Hour
	// how often a missing token, or one whose renewal stopped, is read again
	networkTokenRenewalRecheck = 10 * time.Minute
	// a failed renewal is retried after at least this, plus jitter over an
	// interval that doubles from the base to the maximum
	networkTokenRenewalMinimumRetry = 10 * time.Second
	networkTokenRenewalJitterBase   = 30 * time.Second
	networkTokenRenewalMaximumRetry = 15 * time.Minute
	// another 4xx (a server that predates the route answers 404)
	networkTokenRenewalRefusedRetry = 24 * time.Hour
	// renewals that answer a token itself due at once are spaced from the
	// floor, doubling up to this
	networkTokenRenewalMaximumAnomaly = 7 * 24 * time.Hour
)

// networkTokenRenewalTimeout is the delay until the scheduled renewal of a
// network token, where 0 means now. A token without a readable expiration or
// an expired one is renewed now, while the server still accepts it; any other
// is renewed at its half-life, never sooner than the floor.
func networkTokenRenewalTimeout(byJwt string, now time.Time) time.Duration {
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(byJwt, claims); err != nil {
		return 0
	}
	expiration, err := claims.GetExpirationTime()
	if err != nil || expiration == nil || !now.Before(expiration.Time) {
		return 0
	}
	start := now
	if issued, err := claims.GetIssuedAt(); err == nil && issued != nil && issued.Time.Before(expiration.Time) {
		start = issued.Time
	}
	return max(start.Add(expiration.Time.Sub(start)/2).Sub(now), networkTokenRenewalFloor)
}

func networkTokenRenewalRetryTimeout(failures int, jitter func(time.Duration) time.Duration) time.Duration {
	interval := networkTokenRenewalJitterBase
	for i := 1; i < failures && interval < networkTokenRenewalMaximumRetry; i += 1 {
		interval *= 2
	}
	interval = min(interval, networkTokenRenewalMaximumRetry)
	return networkTokenRenewalMinimumRetry + max(0, min(jitter(interval), interval))
}

func networkTokenRenewalAnomalyTimeout(consecutive int) time.Duration {
	timeout := networkTokenRenewalFloor
	for i := 1; i < consecutive && timeout < networkTokenRenewalMaximumAnomaly; i += 1 {
		timeout *= 2
	}
	return min(timeout, networkTokenRenewalMaximumAnomaly)
}

func networkTokenRenewalJitter(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	return time.Duration(mathrand.Int63n(int64(interval)))
}

// networkTokenRenewalRefused is a 4xx other than a confirmed rejection (401)
// and the transient 408, 425 and 429: asking again soon will not change it.
func networkTokenRenewalRefused(err error) bool {
	var unavailable *sdk.ClientControlUnavailableError
	if errors.As(err, &unavailable) {
		return false
	}
	var status *connect.HttpStatusError
	if !errors.As(err, &status) || status.StatusCode < 400 || 500 <= status.StatusCode {
		return false
	}
	switch status.StatusCode {
	case http.StatusUnauthorized, http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return false
	}
	return true
}

type networkTokenRenewalClock interface {
	Now() time.Time
	// After starts a timer of d; stop releases it.
	After(d time.Duration) (c <-chan time.Time, stop func())
}

type systemNetworkTokenRenewalClock struct{}

func (systemNetworkTokenRenewalClock) Now() time.Time { return time.Now() }

func (systemNetworkTokenRenewalClock) After(d time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(d)
	return timer.C, func() { timer.Stop() }
}

// networkTokenRenewer renews one state directory's network token file.
type networkTokenRenewer struct {
	path    string
	refresh func(ctx context.Context, byJwt string) (*sdk.RefreshJwtResult, error)
	output  *providerDiagnostics
	clock   networkTokenRenewalClock
	jitter  func(time.Duration) time.Duration
	// tests observe each completed pass and its wait
	afterPass func(wait time.Duration)
	// set a token the renewal call rejects (401) aside instead of stopping,
	// for an operator child whose supervisor signs in again with the hotkey
	// (--quarantine-rejected-sign-in); onQuarantined then ends the child
	quarantineRejected bool
	onQuarantined      func(clientauth.NetworkTokenQuarantine)

	// owned by run
	// the token the schedule was read for, and its renewal time
	scheduledToken string
	scheduledTime  time.Time
	// a token whose renewal stopped: rejected, refused, answered with another
	// identity, or not writable; it is left alone until the file changes
	stoppedToken string
	// failed renewals in a row, and renewals in a row due at once
	failures  int
	anomalies int
	// the earliest next renewal after a failure or an anomalous renewal
	notBefore time.Time
	// a renewal the server answered that the file has not taken yet (its
	// owner lock was held), and the token it renews
	pending    string
	pendingFor string
	// a rejected token whose move aside waits for the owner lock
	pendingQuarantine string
}

func newNetworkTokenRenewer(path string, refresh func(context.Context, string) (*sdk.RefreshJwtResult, error), output *providerDiagnostics) *networkTokenRenewer {
	return &networkTokenRenewer{
		path:    path,
		refresh: refresh,
		output:  output,
		clock:   systemNetworkTokenRenewalClock{},
		jitter:  networkTokenRenewalJitter,
	}
}

func (self *networkTokenRenewer) run(ctx context.Context) {
	for {
		wait := self.pass(ctx)
		if ctx.Err() != nil {
			return
		}
		// every pass answers a wait of at least a second; the floor holds even
		// if a future path forgets to
		wait = max(wait, time.Second)
		if self.afterPass != nil {
			self.afterPass(wait)
		}
		timeout, stop := self.clock.After(wait)
		select {
		case <-ctx.Done():
			stop()
			return
		case <-timeout:
		}
	}
}

// pass reads the token file, renews the token when it is due, and answers how
// long to wait before the next pass.
func (self *networkTokenRenewer) pass(ctx context.Context) time.Duration {
	now := self.clock.Now()
	token, err := clientauth.ReadToken(self.path)
	if err != nil {
		// awaiting auth, or unreadable: nothing to renew
		return networkTokenRenewalRecheck
	}
	if token == self.stoppedToken {
		return networkTokenRenewalRecheck
	}
	self.stoppedToken = ""
	if token != self.scheduledToken {
		// another sign-in replaced the file: schedule its token once (a token
		// without iat has a half-life relative to the time it is read, which
		// would recede if it were read again on every pass)
		self.scheduledToken = token
		self.scheduledTime = now.Add(networkTokenRenewalTimeout(token, now))
		self.failures, self.anomalies, self.notBefore = 0, 0, time.Time{}
		self.pending, self.pendingFor = "", ""
		self.pendingQuarantine = ""
		if !clientauth.NetworkJwtRenewable(token) {
			// an API key does not expire, and nothing else is renewed
			return self.stop(token, providerNetworkTokenRenewalStopped, nil)
		}
	}
	due := self.scheduledTime
	if due.Before(self.notBefore) {
		due = self.notBefore
	}
	if now.Before(due) {
		return min(due.Sub(now), networkTokenRenewalMaximumWait)
	}
	return self.renew(ctx, token, now)
}

// quarantine sets a token the renewal call rejected aside, so the supervisor
// signs in again with the hotkey (clientauth.QuarantineNetworkToken). A sign-in
// that replaced the token first is never set aside.
func (self *networkTokenRenewer) quarantine(token string, now time.Time, rejection error) time.Duration {
	quarantined, ok, err := clientauth.QuarantineNetworkToken(self.path, token, now)
	switch {
	case errors.Is(err, clientauth.ErrNetworkTokenBusy):
		// a sign-in or a registration holds the owner lock: only the move is
		// retried, and a sign-in that lands first wins
		self.pendingQuarantine = token
		self.failures += 1
		return self.retryAt(now, networkTokenRenewalRetryTimeout(self.failures, self.jitter), err)
	case err != nil:
		// the custody refuses the move: renewal stops, as without quarantine
		self.pendingQuarantine = ""
		return self.stop(token, providerNetworkSignInRejected, rejection)
	case !ok:
		// a sign-in replaced the token first; the next pass reads it
		self.pendingQuarantine = ""
		return time.Second
	}
	self.pendingQuarantine = ""
	self.stoppedToken = token
	self.output.observeFile(providerNetworkSignInQuarantined, 0, false, rejection, 0, nil, quarantined.Path)
	if self.onQuarantined != nil {
		self.onQuarantined(quarantined)
	}
	return networkTokenRenewalRecheck
}

func (self *networkTokenRenewer) stop(token string, event providerDiagnosticEvent, err error) time.Duration {
	self.stoppedToken = token
	self.output.observe(event, 0, false, err, 0, nil)
	return networkTokenRenewalRecheck
}

func (self *networkTokenRenewer) retryAt(now time.Time, timeout time.Duration, err error) time.Duration {
	self.notBefore = now.Add(timeout)
	self.output.observe(providerNetworkTokenRenewalWait, 0, false, err, timeout, nil)
	return min(timeout, networkTokenRenewalMaximumWait)
}

func (self *networkTokenRenewer) renew(ctx context.Context, token string, now time.Time) time.Duration {
	if self.pendingQuarantine == token {
		// the server already rejected it; only the move is left
		return self.quarantine(token, now, nil)
	}
	renewed := ""
	if self.pending != "" && self.pendingFor == token {
		// the server already answered; only the write is left
		renewed = self.pending
	} else {
		result, err := self.refresh(ctx, token)
		if ctx.Err() != nil {
			return 0
		}
		now = self.clock.Now()
		switch {
		case err != nil && sdk.ConfirmedClientRefreshRejection(err):
			// rotated credentials, a removed network, or an expiration the
			// server enforces: only a new sign-in resolves it
			if self.quarantineRejected {
				return self.quarantine(token, now, err)
			}
			return self.stop(token, providerNetworkSignInRejected, err)
		case err != nil && networkTokenRenewalRefused(err):
			self.failures = 0
			return self.retryAt(now, networkTokenRenewalRefusedRetry, err)
		case err != nil:
			self.failures += 1
			return self.retryAt(now, networkTokenRenewalRetryTimeout(self.failures, self.jitter), err)
		case result == nil || result.Error != nil:
			// a refusal the server repeats for this token, which stays
			return self.stop(token, providerNetworkTokenRenewalStopped, nil)
		}
		if err := clientauth.ValidateRenewedNetworkJwt(token, result.ByJwt); err != nil {
			return self.stop(token, providerNetworkTokenRenewalStopped, err)
		}
		renewed = result.ByJwt
	}

	written, err := clientauth.RenewNetworkToken(self.path, token, renewed)
	if errors.Is(err, clientauth.ErrNetworkTokenBusy) {
		// a registration borrows the token; the renewal stays valid for
		// its whole life, so only the write is retried
		self.pending, self.pendingFor = renewed, token
		self.failures += 1
		return self.retryAt(now, networkTokenRenewalRetryTimeout(self.failures, self.jitter), err)
	}
	self.pending, self.pendingFor = "", ""
	if err != nil {
		// the file's custody refuses the write (mode, owner, link)
		return self.stop(token, providerNetworkTokenRenewalStopped, err)
	}
	if !written {
		// another sign-in replaced the file first; the next pass reads it
		return time.Second
	}
	self.failures = 0
	self.output.observe(providerNetworkTokenRenewed, 0, false, nil, 0, nil)
	self.scheduledToken = renewed
	timeout := networkTokenRenewalTimeout(renewed, now)
	self.scheduledTime = now.Add(timeout)
	self.notBefore = time.Time{}
	if timeout == 0 {
		// a renewal itself due at once: no expiration, or a clock far ahead
		self.anomalies += 1
		self.notBefore = now.Add(networkTokenRenewalAnomalyTimeout(self.anomalies))
		return min(self.notBefore.Sub(now), networkTokenRenewalMaximumWait)
	}
	self.anomalies = 0
	return min(timeout, networkTokenRenewalMaximumWait)
}

// networkTokenRenewalHooks observe the renewer one run starts. Hooks belong to
// one explicitly supplied run context, never global state.
type networkTokenRenewalHooks struct {
	afterStart func(*networkTokenRenewer)
}

type networkTokenRenewalHooksKey struct{}

// startNetworkTokenRenewal renews the state directory's network token for as
// long as the run lasts. Its requests use the first provider's transport, the
// direct one or the first proxy, so the renewal reaches the API the way that
// provider's registration does. The returned stop joins the renewer and its
// API.
func startNetworkTokenRenewal(ctx context.Context, apiUrl string, proxySettings *connect.ProxySettings, dialer *connect.DialContextSettings, networkJwtPath string, output *providerDiagnostics, quarantineRejected bool, onQuarantined func(clientauth.NetworkTokenQuarantine)) (stop func() error) {
	renewalCtx, cancel := context.WithCancel(ctx)
	strategySettings := connect.DefaultClientStrategySettings()
	strategySettings.ProxySettings = proxySettings
	strategySettings.DialContextSettings = dialer
	strategy := connect.NewClientStrategy(renewalCtx, strategySettings)
	api := sdk.NewApi(renewalCtx, strategy, apiUrl)
	renewer := newNetworkTokenRenewer(networkJwtPath, api.NetworkRefreshSyncWithContextAndJwt, output)
	renewer.quarantineRejected = quarantineRejected
	renewer.onQuarantined = onQuarantined
	if hooks, ok := ctx.Value(networkTokenRenewalHooksKey{}).(networkTokenRenewalHooks); ok && hooks.afterStart != nil {
		hooks.afterStart(renewer)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		connect.HandleError(func() { renewer.run(renewalCtx) })
	}()
	return func() error {
		cancel()
		<-done
		err := api.CloseAndWait(context.Background())
		strategy.Close()
		return err
	}
}
