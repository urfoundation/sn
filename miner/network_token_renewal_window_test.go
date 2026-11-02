//go:build linux || darwin

package miner

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// The renewal window (networkTokenRenewalSettings): each network token renews
// at its point from 40% to 50% of its lifetime, taken from a hash of its id,
// never later than its half-life, never sooner than the floor after the
// previous renewal. At exactly the half-life, providers that renewed together
// stayed in lockstep every cycle (owner decision, 2026-11-01).

// installs the window draws a test's renewals take, in order
func (self *renewalTestFixture) drawWindow(t *testing.T, draws ...float64) func() int {
	var drawLock sync.Mutex
	count := 0
	self.renewer.draw = func(string) float64 {
		drawLock.Lock()
		defer drawLock.Unlock()
		if len(draws) <= count {
			t.Errorf("window draw %d, but the test has %d", count+1, len(draws))
			return 0
		}
		count += 1
		return draws[count-1]
	}
	return func() int {
		drawLock.Lock()
		defer drawLock.Unlock()
		return count
	}
}

// renews the scheduled token with a fresh token of the lifetime, issued at
// the clock's now, which is persisted; returns the renewal and the next wait
func (self *renewalTestFixture) renewNow(t *testing.T, lifetime time.Duration, marker string) (string, time.Duration) {
	t.Helper()
	renewed := renewalTestJwt(t, self.clock.now, self.clock.now.Add(lifetime), marker)
	self.refresh.answer = renewalTestAnswer(renewed)
	calls := self.refresh.count()
	wait := self.renewer.pass(t.Context())
	if self.refresh.count() != calls+1 || self.token(t) != renewed {
		t.Fatalf("%s: not renewed (calls %d -> %d)", marker, calls, self.refresh.count())
	}
	if code, _ := self.event(t); code != "network_sign_in_renewed" {
		t.Fatalf("%s: event %s", marker, code)
	}
	return renewed, wait
}

// The owner's window, the 5-minute floor and the unchanged backoffs.
func TestNetworkTokenRenewalSettingsDefaults(t *testing.T) {
	settings := defaultNetworkTokenRenewalSettings()
	for name, c := range map[string]struct{ got, want any }{
		"window start":    {settings.RenewalWindowStart, 0.4},
		"window end":      {settings.RenewalWindowEnd, 0.5},
		"floor":           {settings.RenewalFloor, 5 * time.Minute},
		"maximum wait":    {settings.MaximumWait, 6 * time.Hour},
		"recheck":         {settings.RecheckTimeout, 10 * time.Minute},
		"minimum retry":   {settings.MinimumRetryTimeout, 10 * time.Second},
		"retry jitter":    {settings.RetryJitterBase, 30 * time.Second},
		"maximum retry":   {settings.MaximumRetryJitter, 15 * time.Minute},
		"refused retry":   {settings.RefusedRetryTimeout, 24 * time.Hour},
		"maximum anomaly": {settings.MaximumAnomalyTimeout, 7 * 24 * time.Hour},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
	renewer := newNetworkTokenRenewer("jwt", nil, nil)
	if *renewer.settings != *settings {
		t.Fatal("a renewer does not start from the defaults")
	}
}

// A draw maps linearly onto the window, and a draw out of range is clamped
// into it rather than past it.
func TestNetworkTokenRenewalTimeDrawsWithinTheWindow(t *testing.T) {
	now := renewalTestBase
	settings := defaultNetworkTokenRenewalSettings()
	for _, lifetime := range []time.Duration{30 * 24 * time.Hour, 24 * time.Hour, 10 * time.Minute} {
		token := renewalTestJwt(t, now, now.Add(lifetime), "window")
		windowStart := now.Add(time.Duration(0.4 * float64(lifetime)))
		halfLife := now.Add(lifetime / 2)
		for _, draw := range []float64{0, 0.25, 0.5, 0.75, 0.999999, 1} {
			got := networkTokenRenewalTime(token, now, draw, settings)
			want := time.Duration((0.4 + 0.1*draw) * float64(lifetime))
			if diff := got.Sub(now) - want; diff < -time.Microsecond || time.Microsecond < diff {
				t.Fatalf("lifetime %s draw %v: renews in %s, want %s", lifetime, draw, got.Sub(now), want)
			}
			if got.Before(windowStart) || got.After(halfLife) {
				t.Fatalf("lifetime %s draw %v: renews at %s, outside [%s, %s]", lifetime, draw, got, windowStart, halfLife)
			}
		}
		for _, draw := range []float64{-1, math.NaN(), math.Inf(-1)} {
			if got := networkTokenRenewalTime(token, now, draw, settings); !got.Equal(windowStart) {
				t.Fatalf("lifetime %s draw %v: renews at %s, want the window's start %s", lifetime, draw, got, windowStart)
			}
		}
		for _, draw := range []float64{1.5, math.Inf(1)} {
			if got := networkTokenRenewalTime(token, now, draw, settings); !got.Equal(halfLife) {
				t.Fatalf("lifetime %s draw %v: renews at %s, want the half-life %s", lifetime, draw, got, halfLife)
			}
		}
	}
}

// A network token with an id (`jti`), as the server mints each token.
func renewalTestJwtWithId(t *testing.T, issued time.Time, expires time.Time, id string) string {
	t.Helper()
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{
		"network_id": renewalTestNetworkId,
		"user_id":    renewalTestUserId,
		"iat":        issued.Unix(),
		"exp":        expires.Unix(),
		"jti":        id,
	}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// The production draw, with no hook installed, takes each token's point from
// its id: 2000 tokens issued in the same second spread over the whole window,
// every point in [40%, 50%) of the lifetime.
func TestNetworkTokenRenewalDefaultDrawStaysWithinTheWindow(t *testing.T) {
	now := renewalTestBase
	lifetime := 30 * 24 * time.Hour
	renewer := newNetworkTokenRenewer("jwt", nil, nil)
	windowStart := now.Add(time.Duration(0.4 * float64(lifetime)))
	halfLife := now.Add(lifetime / 2)
	earliest, latest := halfLife, windowStart
	for i := 0; i < 2000; i += 1 {
		token := renewalTestJwtWithId(t, now, now.Add(lifetime), fmt.Sprintf("00000000-0000-4000-8000-%012d", i))
		got := networkTokenRenewalTime(token, now, renewer.draw(token), renewer.settings)
		if got.Before(windowStart) || !got.Before(halfLife) {
			t.Fatalf("renews at %s, outside [%s, %s)", got, windowStart, halfLife)
		}
		if got.Before(earliest) {
			earliest = got
		}
		if latest.Before(got) {
			latest = got
		}
	}
	if latest.Sub(earliest) < 2*24*time.Hour+23*time.Hour {
		t.Fatalf("2000 tokens spread over only %s of the 3-day window", latest.Sub(earliest))
	}
}

// A token's point is a function of the token, the SDK's (jwtRefreshDraw): a
// restarted process takes the same point, tokens with other ids take other
// points, and claims beside the id do not move it. Points are uniform over
// tokens (chi-square of 10000 ids in 20 bins below 43.8, the 0.001 critical
// value for 19 degrees of freedom).
func TestNetworkTokenRenewalDrawIsTheTokens(t *testing.T) {
	now := renewalTestBase
	day := 24 * time.Hour
	token := renewalTestJwtWithId(t, now, now.Add(30*day), "id-1")
	if networkTokenRenewalDraw(token) != networkTokenRenewalDraw(token) {
		t.Fatal("one token took two points")
	}
	if networkTokenRenewalDraw(renewalTestJwtWithId(t, now, now.Add(30*day), "id-2")) == networkTokenRenewalDraw(token) {
		t.Fatal("tokens with different ids took the same point")
	}
	if networkTokenRenewalDraw(renewalTestJwtWithId(t, now.Add(time.Hour), now.Add(31*day), "id-1")) != networkTokenRenewalDraw(token) {
		t.Fatal("claims beside the id moved the point")
	}

	const tokenCount = 10000
	const binCount = 20
	bins := make([]int, binCount)
	for i := 0; i < tokenCount; i += 1 {
		draw := networkTokenRenewalDraw(renewalTestJwtWithId(t, now, now.Add(30*day), fmt.Sprintf("00000000-0000-4000-8000-%012d", i)))
		if draw < 0 || 1 <= draw {
			t.Fatalf("draw %v is outside [0, 1)", draw)
		}
		bins[int(draw*binCount)] += 1
	}
	expected := float64(tokenCount) / binCount
	chiSquare := 0.0
	for _, count := range bins {
		chiSquare += (float64(count) - expected) * (float64(count) - expected) / expected
	}
	if 43.8 < chiSquare {
		t.Fatalf("points are not uniform over tokens: chi-square %.1f, bins %v", chiSquare, bins)
	}
}

// A restarted provide process takes the same point for the token in the file,
// with the production draw: it renews a token 45% into its life exactly when
// the first process would have, at once if its point has passed and at the
// point otherwise. A point drawn afresh by each process made a fleet restart
// renew the tokens whose new point had passed in one burst, and frequent
// restarts renew early.
func TestNetworkTokenRenewerRestartKeepsTheTokensPoint(t *testing.T) {
	day := 24 * time.Hour
	pastCount := 0
	for i := 0; i < 8; i += 1 {
		// 13.5 of 30 days old: 45% of its lifetime
		signIn := renewalTestJwtWithId(t, renewalTestBase.Add(-13*day-12*time.Hour), renewalTestBase.Add(16*day+12*time.Hour), fmt.Sprintf("sign-in-%d", i))
		point := networkTokenRenewalTime(signIn, renewalTestBase, networkTokenRenewalDraw(signIn), defaultNetworkTokenRenewalSettings())
		fixture := newRenewalTestFixture(t, signIn)
		fixture.renewer.draw = networkTokenRenewalDraw
		fixture.refresh.answer = renewalTestAnswer(renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "renewed"))
		for _, process := range []string{"first", "restarted"} {
			renewer := fixture.renewer
			if process == "restarted" {
				renewer = newNetworkTokenRenewer(fixture.path, fixture.refresh.refresh, fixture.renewer.output)
				renewer.clock = fixture.clock
				renewer.jitter = func(time.Duration) time.Duration { return 0 }
			}
			if !point.After(renewalTestBase) {
				// past its point: the first process renews it; the file then
				// holds the renewal, which the restarted process schedules
				if process == "first" {
					renewer.pass(t.Context())
					fixture.event(t)
					if fixture.refresh.count() != 1 {
						t.Fatalf("token %d past its point was not renewed at once", i)
					}
					pastCount += 1
				}
				continue
			}
			renewer.pass(t.Context())
			if fixture.refresh.count() != 0 || !renewer.scheduledTime.Equal(point) {
				t.Fatalf("token %d, %s process: renews at %s after %d calls, want %s", i, process, renewer.scheduledTime, fixture.refresh.count(), point)
			}
		}
	}
	// half of the window is behind a token 45% into its life
	if pastCount == 0 || pastCount == 8 {
		t.Fatalf("%d of 8 tokens past their point; the points do not spread", pastCount)
	}
}

// Every renewal draws its own point: 30-day tokens renew 12, 15 and 13.5 days
// after their sign-in for draws 0, 1 and 0.5. A pass that changes nothing
// keeps the point it has.
func TestNetworkTokenRenewerRedrawsEachRenewal(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "sign-in")
	fixture := newRenewalTestFixture(t, signIn)
	drawCount := fixture.drawWindow(t, 0, 1, 0.5)

	for i := 0; i < 4; i += 1 {
		// passes every 6 hours read the same point
		fixture.renewer.pass(t.Context())
		fixture.clock.now = fixture.clock.now.Add(6 * time.Hour)
	}
	if !fixture.renewer.scheduledTime.Equal(renewalTestBase.Add(12*day)) || drawCount() != 1 || fixture.refresh.count() != 0 {
		t.Fatalf("the sign-in renews at %s after %d draws and %d calls", fixture.renewer.scheduledTime, drawCount(), fixture.refresh.count())
	}

	fixture.clock.now = renewalTestBase.Add(12 * day)
	_, wait := fixture.renewNow(t, 30*day, "first")
	if !fixture.renewer.scheduledTime.Equal(fixture.clock.now.Add(15*day)) || wait != fixture.renewer.settings.MaximumWait {
		t.Fatalf("the first renewal renews at %s (wait %s)", fixture.renewer.scheduledTime, wait)
	}
	fixture.clock.now = fixture.clock.now.Add(15 * day)
	fixture.renewNow(t, 30*day, "second")
	if !fixture.renewer.scheduledTime.Equal(fixture.clock.now.Add(13*day + 12*time.Hour)) {
		t.Fatalf("the second renewal renews at %s", fixture.renewer.scheduledTime)
	}
	if drawCount() != 3 || fixture.refresh.count() != 2 {
		t.Fatalf("%d draws for %d renewals", drawCount(), fixture.refresh.count())
	}
}

// A token read past its drawn point renews on the first pass; one read inside
// the window before its point renews at the point, never after the half-life.
func TestNetworkTokenRenewerTokenPastItsPointRenewsPromptly(t *testing.T) {
	day := 24 * time.Hour
	// 13.5 of 30 days old: 45% of its lifetime
	signIn := renewalTestJwt(t, renewalTestBase.Add(-13*day-12*time.Hour), renewalTestBase.Add(16*day+12*time.Hour), "sign-in")

	past := newRenewalTestFixture(t, signIn)
	past.drawWindow(t, 0, 0)
	past.renewNow(t, 30*day, "renewed")

	ahead := newRenewalTestFixture(t, signIn)
	ahead.drawWindow(t, 1, 1)
	if wait := ahead.renewer.pass(t.Context()); wait != ahead.renewer.settings.MaximumWait || ahead.refresh.count() != 0 {
		t.Fatalf("renewed before its point: wait %s, %d calls", wait, ahead.refresh.count())
	}
	halfLife := renewalTestBase.Add(day + 12*time.Hour)
	if !ahead.renewer.scheduledTime.Equal(halfLife) {
		t.Fatalf("renews at %s, want the half-life %s", ahead.renewer.scheduledTime, halfLife)
	}
	ahead.clock.now = halfLife.Add(-time.Second)
	if wait := ahead.renewer.pass(t.Context()); wait != time.Second || ahead.refresh.count() != 0 {
		t.Fatalf("a second before the half-life: wait %s, %d calls", wait, ahead.refresh.count())
	}
	ahead.clock.now = halfLife
	ahead.renewNow(t, 30*day, "renewed")
}

// A renewal that answers a token whose point is already due -- a lifetime
// shorter than the floor allows, or a clock far ahead of the server's -- does
// not renew again at once: renewals are spaced by the floor.
func TestNetworkTokenRenewerShortLifetimesAreSpaced(t *testing.T) {
	day := 24 * time.Hour
	for _, c := range []struct {
		lifetime time.Duration
		// how long before the clock's now each renewal was issued
		age time.Duration
	}{
		{lifetime: 2 * time.Minute},
		{lifetime: 2 * time.Second},
		{lifetime: 30 * day, age: 16 * day},
	} {
		signIn := renewalTestJwt(t, renewalTestBase.Add(-40*day), renewalTestBase.Add(-10*day), "expired")
		fixture := newRenewalTestFixture(t, signIn)
		fixture.drawWindow(t, 0, 0.5, 1, 0, 0.5)
		floor := fixture.renewer.settings.RenewalFloor
		for i := 0; i < 3; i += 1 {
			now := fixture.clock.now
			renewed := renewalTestJwt(t, now.Add(-c.age), now.Add(-c.age+c.lifetime), fmt.Sprintf("renewed %d", i))
			fixture.refresh.answer = renewalTestAnswer(renewed)
			wait := fixture.renewer.pass(t.Context())
			fixture.event(t)
			if fixture.refresh.count() != i+1 || wait != floor {
				t.Fatalf("lifetime %s age %s renewal %d: calls %d, wait %s, want %s", c.lifetime, c.age, i+1, fixture.refresh.count(), wait, floor)
			}
			// nothing before the floor
			fixture.clock.now = now.Add(floor - time.Second)
			if wait := fixture.renewer.pass(t.Context()); wait != time.Second || fixture.refresh.count() != i+1 {
				t.Fatalf("lifetime %s age %s: renewed again within the floor (wait %s)", c.lifetime, c.age, wait)
			}
			fixture.clock.now = now.Add(floor)
		}
	}
}

// The window ends at the half-life, and a draw out of range does not pass it.
func TestNetworkTokenRenewerNeverRenewsPastHalfLife(t *testing.T) {
	day := 24 * time.Hour
	signIn := renewalTestJwt(t, renewalTestBase, renewalTestBase.Add(30*day), "sign-in")
	fixture := newRenewalTestFixture(t, signIn)
	fixture.drawWindow(t, 1, 2, math.Inf(1), 1.5)
	fixture.renewer.pass(t.Context())
	for i := 0; i < 3; i += 1 {
		if !fixture.renewer.scheduledTime.Equal(fixture.clock.now.Add(15 * day)) {
			t.Fatalf("renewal %d renews at %s, after the half-life", i, fixture.renewer.scheduledTime)
		}
		fixture.clock.now = fixture.clock.now.Add(15 * day)
		fixture.renewNow(t, 30*day, fmt.Sprintf("renewed %d", i))
	}
	if !fixture.renewer.scheduledTime.Equal(fixture.clock.now.Add(15 * day)) {
		t.Fatalf("the last renewal renews at %s, after the half-life", fixture.renewer.scheduledTime)
	}
}
