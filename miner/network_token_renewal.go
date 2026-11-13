package miner

// The provider keeps its network sign-in renewed (miner/PROVIDER-REGISTRATION.md,
// "Network sign-in renewal").
//
// Day to day a provider runs on its client tokens, which refresh themselves.
// The network token in the state directory's jwt is read again to register a
// client (a new slot, an interrupted registration), and under
// provide --all-operators the supervisor's hourly hotkey wallet upkeep sends
// it. It expires 30 days after its sign-in, so the provide process that owns
// the state directory renews it (POST /auth/network-refresh) at its point
// from 40% to 50% of its lifetime, never later than its half-life: the
// schedule the SDK uses for the apps' tokens. The point comes from a hash of
// the token's id, so each renewal has its own, spreading providers that
// renewed together (after an outage or a fleet restart) instead of keeping
// them in lockstep, and a restarted process takes the same point for the same
// token (owner decision, 2026-11-01). The renewal is written with
// clientauth.RenewNetworkToken, which keeps every revoked client blocked
// (clientauth/network_token.go).

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	mathrand "math/rand/v2"
	"net/http"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/clientauth"
)

// Tunes a networkTokenRenewer. A scheduled renewal falls at a point of the
// token's lifetime, from `iat` to `exp`, in the renewal window, taken from a
// hash of the token's id (networkTokenRenewalDraw): the window of the SDK's
// token refreshes (sdk
// device_token_manager.go, apiTokenRefreshSettings). The window ends at the
// half-life, so a token keeps at least half its life for the retries after a
// failed renewal. Every pass waits at least a second (run), so no wait below,
// however small or non-positive, makes the renewer spin.
type networkTokenRenewalSettings struct {
	// The start of the renewal window, as a fraction of the token's lifetime.
	// 0.4 spreads providers that renewed together over the last tenth of the
	// lifetime before the half-life (3 days of a 30-day token), for about 11%
	// more renewals than at exactly the half-life. Non-positive starts the
	// window at `iat`.
	RenewalWindowStart float64
	// The end of the renewal window, as a fraction of the token's lifetime: the
	// half-life, 0.5, which no token's point passes. At or below
	// RenewalWindowStart, every token renews at the window's start.
	RenewalWindowEnd float64
	// The least time from one renewal to the next scheduled one, like the
	// SDK's MinRefreshTimeout: a schedule derived from a server or clock value
	// needs a floor, or it is one change away from a hot loop. It wins over the
	// window: a lifetime shorter than twice the floor renews after the floor.
	// Non-positive is for tests only: nothing then spaces the renewals, so a
	// renewal that answers a token already due renews again without a wait.
	RenewalFloor time.Duration
	// The token file is read again at least this often, so a sign-in that
	// replaced it is scheduled within this long.
	MaximumWait time.Duration
	// How often a missing token, or one whose renewal stopped, is read again.
	RecheckTimeout time.Duration
	// A failed renewal is retried after at least MinimumRetryTimeout, plus a
	// jitter over an interval that doubles from RetryJitterBase with each
	// failure in a row, up to MaximumRetryJitter. The minimum keeps any
	// jitter, even none, from retrying in a loop.
	MinimumRetryTimeout time.Duration
	RetryJitterBase     time.Duration
	MaximumRetryJitter  time.Duration
	// After another 4xx (a server that predates the route answers 404).
	RefusedRetryTimeout time.Duration
	// Renewals that answer a token itself due at once are spaced from
	// RenewalFloor, doubling up to this.
	MaximumAnomalyTimeout time.Duration
}

func defaultNetworkTokenRenewalSettings() *networkTokenRenewalSettings {
	return &networkTokenRenewalSettings{
		RenewalWindowStart:    0.4,
		RenewalWindowEnd:      0.5,
		RenewalFloor:          5 * time.Minute,
		MaximumWait:           6 * time.Hour,
		RecheckTimeout:        10 * time.Minute,
		MinimumRetryTimeout:   10 * time.Second,
		RetryJitterBase:       30 * time.Second,
		MaximumRetryJitter:    15 * time.Minute,
		RefusedRetryTimeout:   24 * time.Hour,
		MaximumAnomalyTimeout: 7 * 24 * time.Hour,
	}
}

// The point of a token's lifetime that a window draw selects: draw 0 is the
// window's start and 1 its end. A draw outside [0, 1] is clamped, so no draw
// falls after the window's end.
func (self *networkTokenRenewalSettings) renewalFraction(draw float64) float64 {
	start := max(self.RenewalWindowStart, 0)
	end := max(self.RenewalWindowEnd, start)
	if !(0 <= draw) {
		// negative, or not a number
		draw = 0
	} else if 1 < draw {
		draw = 1
	}
	return min(start+(end-start)*draw, end)
}

// The point of a token in its renewal window, in [0, 1): a hash of the
// token's id (`jti`), else of its signature, else of its whole text (an
// unsigned token without an id, or text that is not a JWT), as a fraction, as
// the SDK takes it (jwtRefreshDraw). Points are uniform across tokens, each
// new token has its own, and a restarted process takes the same one; a point
// drawn afresh by each process renewed a token early whenever its new point
// had passed, in one burst on a fleet restart.
func networkTokenRenewalDraw(token string) float64 {
	seed := "token:" + token
	claims := gojwt.MapClaims{}
	if _, parts, err := gojwt.NewParser().ParseUnverified(token, claims); err == nil {
		if id, ok := claims["jti"].(string); ok && id != "" {
			seed = "jti:" + id
		} else if 3 <= len(parts) && parts[2] != "" {
			seed = "signature:" + parts[2]
		}
	}
	sum := sha256.Sum256([]byte(seed))
	// the top 53 bits, the precision of a float64 fraction
	return float64(binary.BigEndian.Uint64(sum[:8])>>11) / float64(uint64(1)<<53)
}

// Reports a token to renew now, while the server still accepts it: one
// without a readable expiration, or an expired one.
func networkTokenDueAtOnce(byJwt string, now time.Time) bool {
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(byJwt, claims); err != nil {
		return true
	}
	expiration, err := claims.GetExpirationTime()
	return err != nil || expiration == nil || !now.Before(expiration.Time)
}

// When the scheduled renewal of a network token falls, for a window draw in
// [0, 1]: now for a token due at once, and any
// other at its point of the lifetime from `iat` (from now without a
// sensible `iat`), which is in the past for a token read late in its life.
// The renewer holds each renewal to at least RenewalFloor after the previous
// one.
func networkTokenRenewalTime(byJwt string, now time.Time, draw float64, settings *networkTokenRenewalSettings) time.Time {
	if networkTokenDueAtOnce(byJwt, now) {
		return now
	}
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(byJwt, claims); err != nil {
		return now
	}
	expiration, err := claims.GetExpirationTime()
	if err != nil || expiration == nil {
		return now
	}
	start := now
	if issued, err := claims.GetIssuedAt(); err == nil && issued != nil && issued.Time.Before(expiration.Time) {
		start = issued.Time
	}
	lifetime := float64(expiration.Time.Sub(start))
	return start.Add(time.Duration(settings.renewalFraction(draw) * lifetime))
}

func networkTokenRenewalRetryTimeout(failures int, jitter func(time.Duration) time.Duration, settings *networkTokenRenewalSettings) time.Duration {
	interval := settings.RetryJitterBase
	for i := 1; i < failures && interval < settings.MaximumRetryJitter; i += 1 {
		interval *= 2
	}
	interval = min(interval, settings.MaximumRetryJitter)
	return settings.MinimumRetryTimeout + max(0, min(jitter(interval), interval))
}

func networkTokenRenewalAnomalyTimeout(consecutive int, settings *networkTokenRenewalSettings) time.Duration {
	timeout := settings.RenewalFloor
	for i := 1; i < consecutive && timeout < settings.MaximumAnomalyTimeout; i += 1 {
		timeout *= 2
	}
	return min(timeout, settings.MaximumAnomalyTimeout)
}

func networkTokenRenewalJitter(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	return time.Duration(mathrand.Int64N(int64(interval)))
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
	path     string
	refresh  func(ctx context.Context, byJwt string) (*sdk.RefreshJwtResult, error)
	output   *providerDiagnostics
	settings *networkTokenRenewalSettings
	clock    networkTokenRenewalClock
	jitter   func(time.Duration) time.Duration
	// a token's point in its renewal window, in [0, 1)
	// (networkTokenRenewalDraw)
	draw func(token string) float64
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
	// when the scheduled token's sign-in was last renewed; zero before its
	// first renewal
	lastRenewalTime time.Time
	// a renewal the server answered that the file has not taken yet (its
	// owner lock was held), and the token it renews
	pending    string
	pendingFor string
	// a rejected token whose move aside waits for the owner lock
	pendingQuarantine string
}

func newNetworkTokenRenewer(path string, refresh func(context.Context, string) (*sdk.RefreshJwtResult, error), output *providerDiagnostics) *networkTokenRenewer {
	return &networkTokenRenewer{
		path:     path,
		refresh:  refresh,
		output:   output,
		settings: defaultNetworkTokenRenewalSettings(),
		clock:    systemNetworkTokenRenewalClock{},
		jitter:   networkTokenRenewalJitter,
		draw:     networkTokenRenewalDraw,
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
		return self.settings.RecheckTimeout
	}
	if token == self.stoppedToken {
		return self.settings.RecheckTimeout
	}
	self.stoppedToken = ""
	if token != self.scheduledToken {
		// another sign-in replaced the file: schedule its token once (a token
		// without iat has a lifetime relative to the time it is read, which
		// would recede if it were read again on every pass)
		self.scheduledToken = token
		self.scheduledTime = networkTokenRenewalTime(token, now, self.draw(token), self.settings)
		self.failures, self.anomalies, self.notBefore = 0, 0, time.Time{}
		self.lastRenewalTime = time.Time{}
		self.pending, self.pendingFor = "", ""
		self.pendingQuarantine = ""
		if !clientauth.NetworkJwtRenewable(token) {
			// an API key does not expire, and nothing else is renewed
			return self.stop(token, providerNetworkTokenRenewalStopped, nil)
		}
	}
	due := self.renewalDue()
	if now.Before(due) {
		return min(due.Sub(now), self.settings.MaximumWait)
	}
	return self.renew(ctx, token, now)
}

// The scheduled token's next renewal: its point, held to the end of a
// failure's backoff or an anomaly's spacing, and to at least
// RenewalFloor after the previous renewal. A renewal answers a fresh token
// whose point is a window away, but one already past its point (a clock far
// ahead of the server's, a lifetime shorter than the floor) would be due again
// at once without the floor.
func (self *networkTokenRenewer) renewalDue() time.Time {
	due := self.scheduledTime
	if due.Before(self.notBefore) {
		due = self.notBefore
	}
	if !self.lastRenewalTime.IsZero() {
		if earliest := self.lastRenewalTime.Add(self.settings.RenewalFloor); due.Before(earliest) {
			due = earliest
		}
	}
	return due
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
		return self.retryAt(now, networkTokenRenewalRetryTimeout(self.failures, self.jitter, self.settings), err)
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
	return self.settings.RecheckTimeout
}

func (self *networkTokenRenewer) stop(token string, event providerDiagnosticEvent, err error) time.Duration {
	self.stoppedToken = token
	self.output.observe(event, 0, false, err, 0, nil)
	return self.settings.RecheckTimeout
}

func (self *networkTokenRenewer) retryAt(now time.Time, timeout time.Duration, err error) time.Duration {
	self.notBefore = now.Add(timeout)
	self.output.observe(providerNetworkTokenRenewalWait, 0, false, err, timeout, nil)
	return min(timeout, self.settings.MaximumWait)
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
			return self.retryAt(now, self.settings.RefusedRetryTimeout, err)
		case err != nil:
			self.failures += 1
			return self.retryAt(now, networkTokenRenewalRetryTimeout(self.failures, self.jitter, self.settings), err)
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
		return self.retryAt(now, networkTokenRenewalRetryTimeout(self.failures, self.jitter, self.settings), err)
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
	// the renewal has its own point
	self.scheduledToken = renewed
	self.scheduledTime = networkTokenRenewalTime(renewed, now, self.draw(renewed), self.settings)
	self.notBefore = time.Time{}
	self.lastRenewalTime = now
	if networkTokenDueAtOnce(renewed, now) {
		// a renewal itself due at once: no expiration, or a clock far ahead
		self.anomalies += 1
		self.notBefore = now.Add(networkTokenRenewalAnomalyTimeout(self.anomalies, self.settings))
	} else {
		self.anomalies = 0
	}
	return min(self.renewalDue().Sub(now), self.settings.MaximumWait)
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
