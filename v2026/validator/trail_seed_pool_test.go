package validator

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// A settable clock and a round-robin draw make every pool decision exact.
type seedPoolTestClock struct {
	at time.Time
}

func (self *seedPoolTestClock) now() time.Time { return self.at }

func newSeedPoolForTest(self connect.Id, minimum int) (*trailSeedPool, *seedPoolTestClock) {
	clock := &seedPoolTestClock{at: time.Unix(1_800_000_000, 0)}
	next := 0
	pool := &trailSeedPool{
		self:    self,
		horizon: 2 * time.Hour,
		minimum: minimum,
		now:     clock.now,
		intn: func(n int) int {
			value := next % n
			next++
			return value
		},
		candidates: map[connect.Id]*trailSeedCandidate{},
	}
	return pool, clock
}

func seedPoolTestIDs(count int) []connect.Id {
	ids := make([]connect.Id, count)
	for index := range ids {
		ids[index] = connect.NewId()
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].LessThan(ids[j]) })
	return ids
}

func seedPoolEligible(pool *trailSeedPool, at time.Time) map[connect.Id]bool {
	pool.stateLock.Lock()
	defer pool.stateLock.Unlock()
	eligible := map[connect.Id]bool{}
	for hop, candidate := range pool.candidates {
		if at.Sub(candidate.proven) <= pool.horizon && !candidate.rest.After(at) {
			eligible[hop] = true
		}
	}
	return eligible
}

// Records the hop every SEED was sent through; EXTENDs carry a trail_id.
type seedRecordingTransport struct {
	inner TrailTransport

	stateLock sync.Mutex
	seeds     []connect.Id
}

func (self *seedRecordingTransport) PostVerify(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
	var envelope struct {
		TrailId *connect.Id `json:"trail_id"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.TrailId == nil {
		self.stateLock.Lock()
		self.seeds = append(self.seeds, hop)
		self.stateLock.Unlock()
	}
	return self.inner.PostVerify(ctx, hop, body)
}

func (self *seedRecordingTransport) sent() []connect.Id {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]connect.Id{}, self.seeds...)
}

// The engine keeps one step attempt so a failed seed costs one send.
func newSeedPoolTestEngine(server *mockVerifyServer, validatorKey ed25519.PrivateKey, clientID connect.Id, transport TrailTransport, picker SeedPicker, stats *StatsEngine) *TrailEngine {
	return NewTrailEngine(
		clientID,
		validatorKey,
		transport,
		NewStaticServerKeyRing(server.serverPublicKeys()),
		picker,
		stats,
		nil,
		func() uint64 { return 42 },
		TrailEngineConfig{M: 4, StepTimeout: 2 * time.Second, ExtendAttempts: 1, Pace: time.Millisecond, ConfirmedSeedHorizon: time.Hour},
	)
}

// The draw is uniform over the candidates in identity order, so a round-robin
// draw visits each exactly once. Below the minimum the pool answers nothing,
// which is what bounds how often any provider is left out of sampling.
func TestTrailSeedPoolDrawsUniformlyAmongProvenCandidates(t *testing.T) {
	self := connect.NewId()
	pool, clock := newSeedPoolForTest(self, 4)
	ids := seedPoolTestIDs(6)
	for _, id := range ids {
		pool.prove(id, clock.at)
	}
	pool.prove(self, clock.at)
	pool.prove(connect.Id{}, clock.at)
	if len(pool.candidates) != len(ids) {
		t.Fatalf("pool holds %d candidates, want %d without the validator or the zero identity", len(pool.candidates), len(ids))
	}
	for round := 0; round < 3; round++ {
		for index, want := range ids {
			got, ok := pool.pick(clock.at, pool.minimum)
			if !ok || got != want {
				t.Fatalf("round %d draw %d = %s/%t, want %s", round, index, got, ok, want)
			}
		}
	}

	small, clock := newSeedPoolForTest(self, 4)
	for _, id := range ids[:3] {
		small.prove(id, clock.at)
	}
	if got, ok := small.pick(clock.at, small.minimum); ok {
		t.Fatalf("pool below its minimum chose %s", got)
	}
	if _, ok := small.pick(clock.at, 1); !ok {
		t.Fatal("retry draw refused a known candidate")
	}
	empty, clock := newSeedPoolForTest(self, 4)
	if got, ok := empty.pick(clock.at, 1); ok {
		t.Fatalf("empty pool chose %s", got)
	}
	var disabled *trailSeedPool
	disabled.prove(ids[0], clock.at)
	disabled.fail(ids[0], clock.at)
	if got, ok := disabled.pick(clock.at, 1); ok {
		t.Fatalf("disabled pool chose %s", got)
	}
}

// A failed seed rests, twice as long after each consecutive failure and never
// longer than the horizon. A new proof returns it at once, and a provider with
// no proof inside the horizon is forgotten.
func TestTrailSeedPoolRestsFailedSeedsAndForgetsOldProofs(t *testing.T) {
	pool, clock := newSeedPoolForTest(connect.NewId(), 1)
	start := clock.at
	hop := connect.NewId()
	pool.fail(hop, start)
	if len(pool.candidates) != 0 {
		t.Fatal("a seed that never answered became a candidate")
	}
	pool.prove(hop, start)

	pool.fail(hop, start)
	if _, ok := pool.pick(start.Add(trailSeedRest-time.Second), 1); ok {
		t.Fatal("failed seed was chosen inside its first rest")
	}
	if got, ok := pool.pick(start.Add(trailSeedRest), 1); !ok || got != hop {
		t.Fatalf("seed after its first rest = %s/%t, want %s", got, ok, hop)
	}
	second := start.Add(trailSeedRest)
	pool.fail(hop, second)
	if _, ok := pool.pick(second.Add(2*trailSeedRest-time.Second), 1); ok {
		t.Fatal("twice-failed seed was chosen inside its doubled rest")
	}
	if _, ok := pool.pick(second.Add(2*trailSeedRest), 1); !ok {
		t.Fatal("twice-failed seed did not return after its doubled rest")
	}

	pool.fail(hop, second)
	pool.prove(hop, second.Add(time.Second))
	if got, ok := pool.pick(second.Add(time.Second), 1); !ok || got != hop {
		t.Fatalf("re-proven seed = %s/%t, want %s at once", got, ok, hop)
	}
	if candidate := pool.candidates[hop]; candidate.failures != 0 || !candidate.rest.IsZero() {
		t.Fatalf("re-proven seed kept failures=%d rest=%s", candidate.failures, candidate.rest)
	}

	proven := second.Add(time.Second)
	for failure := 0; failure < 40; failure++ {
		pool.fail(hop, proven)
	}
	if rest := pool.candidates[hop].rest.Sub(proven); rest != pool.horizon {
		t.Fatalf("repeated failures rest %s, want the %s horizon", rest, pool.horizon)
	}

	aging, clock := newSeedPoolForTest(connect.NewId(), 1)
	fresh := connect.NewId()
	aging.prove(fresh, clock.at)
	if got, ok := aging.pick(clock.at.Add(aging.horizon), 1); !ok || got != fresh {
		t.Fatalf("candidate at the horizon = %s/%t, want %s", got, ok, fresh)
	}
	if got, ok := aging.pick(clock.at.Add(aging.horizon+time.Second), 1); ok {
		t.Fatalf("candidate past the horizon was chosen: %s", got)
	}
	if len(aging.candidates) != 0 {
		t.Fatalf("pool kept %d candidates past the horizon", len(aging.candidates))
	}
}

// The oldest proof makes room, so memory stays bounded for any fleet size.
func TestTrailSeedPoolBoundsItsMemory(t *testing.T) {
	pool, clock := newSeedPoolForTest(connect.NewId(), 1)
	oldest := connect.NewId()
	pool.prove(oldest, clock.at)
	for index := 1; index < trailSeedPoolMaximum+8; index++ {
		pool.prove(connect.NewId(), clock.at.Add(time.Duration(index)*time.Millisecond))
	}
	if len(pool.candidates) != trailSeedPoolMaximum {
		t.Fatalf("pool holds %d candidates, want the %d maximum", len(pool.candidates), trailSeedPoolMaximum)
	}
	if _, kept := pool.candidates[oldest]; kept {
		t.Fatal("pool kept its oldest proof past the maximum")
	}
}

// Unit and single-trail callers have no SEED gate and keep their picker. The
// release gate enables the default horizon, and the current window's confirmed
// providers, never merely assigned ones, start the pool.
func TestTrailSeedPoolFollowsTheReleaseSeedGate(t *testing.T) {
	self := connect.NewId()
	if pool := newTrailSeedPool(TrailEngineConfig{}, self, nil); pool != nil {
		t.Fatal("a caller without the SEED gate got a seed pool")
	}
	if pool := newTrailSeedPool(TrailEngineConfig{SeedAttemptInterval: time.Second, ConfirmedSeedHorizon: -1}, self, nil); pool != nil {
		t.Fatal("a negative horizon did not disable the seed pool")
	}
	gated := newTrailSeedPool(TrailEngineConfig{SeedAttemptInterval: time.Second}, self, nil)
	if gated == nil || gated.horizon != trailSeedProofHorizon || gated.minimum != trailSeedPoolMinimum {
		t.Fatalf("release gate pool = %+v, want the default horizon and minimum", gated)
	}

	stats := NewStatsEngine(StatsConfig{})
	confirmed := seedPoolTestIDs(3)
	for _, id := range confirmed {
		stats.RecordAssignment(id)
		stats.RecordConfirmation(id, 40)
	}
	assignedOnly := connect.NewId()
	stats.RecordAssignment(assignedOnly)
	stats.RecordAssignment(self)
	stats.RecordConfirmation(self, 40)
	explicit := newTrailSeedPool(TrailEngineConfig{ConfirmedSeedHorizon: time.Minute}, self, stats)
	if explicit == nil || explicit.horizon != time.Minute {
		t.Fatalf("explicit horizon pool = %+v, want one minute", explicit)
	}
	if len(explicit.candidates) != len(confirmed) {
		t.Fatalf("pool started with %d candidates, want the %d confirmed providers", len(explicit.candidates), len(confirmed))
	}
	for _, id := range confirmed {
		if explicit.candidates[id] == nil {
			t.Fatalf("confirmed provider %s did not start the pool", id)
		}
	}
	if got := stats.confirmedProviderIDs(); len(got) != len(confirmed)+1 {
		t.Fatalf("window reports %d confirmed providers, want %d", len(got), len(confirmed)+1)
	}

	engine := NewTrailEngine(self, ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), nil, nil, nil, stats, nil, nil, TrailEngineConfig{})
	if engine.seedPool != nil {
		t.Fatal("an engine without the SEED gate got a seed pool")
	}
	engine = NewTrailEngine(self, ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), nil, nil, nil, stats, nil, nil, TrailEngineConfig{SeedAttemptInterval: time.Millisecond})
	if engine.seedPool == nil || len(engine.seedPool.candidates) != len(confirmed) {
		t.Fatal("a release-gated engine did not start its pool from the confirmed window")
	}
}

// With enough confirmed providers every trail seeds through one of them and
// discovery is never asked. Seeding leaves the seed's own statistics alone:
// it is still never an assignment or a confirmation (§7.6).
func TestTrailEngineSeedsFromConfirmedProviders(t *testing.T) {
	server, validatorKey, clientID := newMockVerifyServer(t, 40)
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	for _, provider := range server.providers[:20] {
		stats.RecordAssignment(provider)
		stats.RecordConfirmation(provider, 40)
	}
	discoveries := 0
	picker := func(context.Context) (connect.Id, error) {
		discoveries++
		return server.providers[39], nil
	}
	transport := &seedRecordingTransport{inner: server}
	engine := newSeedPoolTestEngine(server, validatorKey, clientID, transport, picker, stats)
	if engine.seedPool == nil || len(engine.seedPool.candidates) != 20 {
		t.Fatal("engine did not start its pool from the confirmed window")
	}

	for trail := 0; trail < 30; trail++ {
		eligible := seedPoolEligible(engine.seedPool, time.Now())
		before := map[connect.Id][2]uint64{}
		for _, provider := range server.providers {
			assignments, confirmations := stats.WindowCounts(provider)
			before[provider] = [2]uint64{assignments, confirmations}
		}
		record, err := engine.RunTrail(context.Background())
		if err != nil {
			t.Fatalf("trail %d: %v", trail, err)
		}
		seeds := transport.sent()
		seed := seeds[len(seeds)-1]
		if len(seeds) != trail+1 || !eligible[seed] {
			t.Fatalf("trail %d sent %d SEEDs, the last through %s; want one through a proven provider", trail, len(seeds), seed)
		}
		if record.Hops[0].ClientId != seed {
			t.Fatalf("trail %d proof seed = %s, want %s", trail, record.Hops[0].ClientId, seed)
		}
		assignments, confirmations := stats.WindowCounts(seed)
		if got := [2]uint64{assignments, confirmations}; got != before[seed] {
			t.Fatalf("trail %d changed its seed's statistics from %v to %v", trail, before[seed], got)
		}
	}
	if discoveries != 0 {
		t.Fatalf("discovery was asked %d times while the pool could choose", discoveries)
	}
}

// Below the minimum the trail seeds exactly as before, through discovery. The
// seed and each hop it confirms become candidates, and once enough are known
// the pool takes over.
func TestTrailEngineSeedFallsBackToDiscoveryBelowTheMinimum(t *testing.T) {
	server, validatorKey, clientID := newMockVerifyServer(t, 40)
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	discoveries := 0
	discovered := server.providers[0]
	picker := func(context.Context) (connect.Id, error) {
		discoveries++
		return discovered, nil
	}
	transport := &seedRecordingTransport{inner: server}
	engine := newSeedPoolTestEngine(server, validatorKey, clientID, transport, picker, stats)
	if engine.seedPool == nil || len(engine.seedPool.candidates) != 0 {
		t.Fatal("engine pool did not start empty")
	}

	pooled := 0
	for trail := 0; trail < 40; trail++ {
		eligible := seedPoolEligible(engine.seedPool, time.Now())
		calls := discoveries
		record, err := engine.RunTrail(context.Background())
		if err != nil {
			t.Fatalf("trail %d: %v", trail, err)
		}
		seeds := transport.sent()
		seed := seeds[len(seeds)-1]
		if len(eligible) < trailSeedPoolMinimum {
			if discoveries != calls+1 || seed != discovered {
				t.Fatalf("trail %d with %d candidates: discovery calls %d->%d, seed %s; want discovery's %s", trail, len(eligible), calls, discoveries, seed, discovered)
			}
		} else {
			pooled++
			if discoveries != calls || !eligible[seed] {
				t.Fatalf("trail %d with %d candidates: discovery calls %d->%d, seed %s; want a proven provider", trail, len(eligible), calls, discoveries, seed)
			}
		}
		after := seedPoolEligible(engine.seedPool, time.Now())
		for _, hop := range record.Hops {
			if !after[hop.ClientId] {
				t.Fatalf("trail %d: answered hop %s is not a candidate", trail, hop.ClientId)
			}
		}
	}
	if pooled == 0 || discoveries == 0 {
		t.Fatalf("pooled trails %d, discovery calls %d; want both paths exercised", pooled, discoveries)
	}
}

// A failed discovery gets one retry from the known entries, even below the
// minimum. With none known the trail fails as before, after a single
// discovery call. A canceled owner gets no retry.
func TestTrailEngineSeedPickRetriesOnceWhenDiscoveryFails(t *testing.T) {
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	discoveryErr := errors.New("synthetic discovery failure")
	discoveries := 0
	picker := func(context.Context) (connect.Id, error) {
		discoveries++
		return connect.Id{}, discoveryErr
	}
	transport := &seedRecordingTransport{inner: server}
	engine := newSeedPoolTestEngine(server, validatorKey, clientID, transport, picker, NewStatsEngine(StatsConfig{AMin: 1}))

	_, err := engine.RunTrail(context.Background())
	var trailErr *TrailError
	if !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorSeed || !errors.Is(err, discoveryErr) {
		t.Fatalf("trail without any known entry = %v, want the discovery failure as a seed error", err)
	}
	if discoveries != 1 || len(transport.sent()) != 0 {
		t.Fatalf("discovery calls %d and SEED sends %d, want 1 and 0", discoveries, len(transport.sent()))
	}

	known := map[connect.Id]bool{server.providers[3]: true, server.providers[4]: true}
	for hop := range known {
		engine.seedPool.prove(hop, time.Now())
	}
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatalf("trail with known entries after a discovery failure: %v", err)
	}
	seeds := transport.sent()
	if discoveries != 2 || len(seeds) != 1 || !known[seeds[0]] {
		t.Fatalf("discovery calls %d, SEEDs %v; want one more call and one SEED through a known entry", discoveries, seeds)
	}

	ctx, cancel := context.WithCancel(context.Background())
	canceling := newSeedPoolTestEngine(server, validatorKey, clientID, transport, func(owner context.Context) (connect.Id, error) {
		cancel()
		return connect.Id{}, owner.Err()
	}, NewStatsEngine(StatsConfig{AMin: 1}))
	canceling.seedPool.prove(server.providers[5], time.Now())
	if _, err := canceling.RunTrail(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery = %v, want the owner's cancellation", err)
	}
	if len(transport.sent()) != 1 {
		t.Fatalf("a canceled owner sent %d more SEEDs", len(transport.sent())-1)
	}
}

// A seed that took a SEED and returned no authenticated ASSIGN rests, whether
// it never answered or its answer failed verification. A trail canceled by its
// owner, or refused before it sent anything, leaves its seed alone.
func TestTrailEngineRestsOnlyASeedThatFailedAfterASend(t *testing.T) {
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	discover := func(context.Context) (connect.Id, error) {
		return connect.Id{}, errors.New("discovery is not expected")
	}
	dead := server.providers[2]

	engine := newSeedPoolTestEngine(server, validatorKey, clientID, server, discover, NewStatsEngine(StatsConfig{AMin: 1}))
	engine.seedPool.minimum = 1
	engine.seedPool.prove(dead, time.Now())
	server.failHops[dead] = true
	_, err := engine.RunTrail(context.Background())
	var trailErr *TrailError
	if !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorSeed || trailErr.Hop != dead {
		t.Fatalf("trail through a dead seed = %v, want a seed error naming it", err)
	}
	if candidate := engine.seedPool.candidates[dead]; candidate == nil || candidate.failures != 1 || !candidate.rest.After(time.Now()) {
		t.Fatalf("dead seed candidate = %+v, want one failure and a rest", candidate)
	}
	if got, ok := engine.seedPool.pick(time.Now(), 1); ok {
		t.Fatalf("resting seed %s was chosen again", got)
	}
	delete(server.failHops, dead)

	_, otherKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	forged := NewTrailEngine(clientID, validatorKey, server, NewStaticServerKeyRing(map[byte]ed25519.PublicKey{server.serverKeyId: otherKey.Public().(ed25519.PublicKey)}), discover, NewStatsEngine(StatsConfig{AMin: 1}), nil, nil,
		TrailEngineConfig{M: 4, StepTimeout: 2 * time.Second, ExtendAttempts: 1, Pace: time.Millisecond, ConfirmedSeedHorizon: time.Hour})
	forged.seedPool.minimum = 1
	unverified := server.providers[3]
	forged.seedPool.prove(unverified, time.Now())
	if _, err := forged.RunTrail(context.Background()); !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorProtocol {
		t.Fatalf("trail with an unverifiable ASSIGN = %v, want a protocol error", err)
	}
	if candidate := forged.seedPool.candidates[unverified]; candidate.failures != 1 {
		t.Fatalf("seed whose ASSIGN did not verify has %d failures, want 1", candidate.failures)
	}

	ctx, cancel := context.WithCancel(context.Background())
	canceling := newSeedPoolTestEngine(server, validatorKey, clientID, &failingTrailTransport{cancel: cancel, cancelAt: 1}, discover, NewStatsEngine(StatsConfig{AMin: 1}))
	canceling.seedPool.minimum = 1
	spared := server.providers[4]
	canceling.seedPool.prove(spared, time.Now())
	if _, err := canceling.RunTrail(ctx); err == nil {
		t.Fatal("trail over a failing transport unexpectedly succeeded")
	}
	if candidate := canceling.seedPool.candidates[spared]; candidate.failures != 0 || !candidate.rest.IsZero() {
		t.Fatalf("seed of a canceled trail = %+v, want no failure", candidate)
	}

	pool, clock := newSeedPoolForTest(clientID, 1)
	unsent := connect.NewId()
	pool.prove(unsent, clock.at)
	refusal := errors.New("refused before sending")
	try := &trailSeedTry{pool: pool, hop: unsent}
	if err := try.metered(func(context.Context) error { return refusal })(context.Background()); !errors.Is(err, refusal) || try.sends != 0 {
		t.Fatalf("refused send = %v with %d sends counted, want the refusal and none", err, try.sends)
	}
	try.finish(context.Background())
	if candidate := pool.candidates[unsent]; candidate.failures != 0 {
		t.Fatal("a seed that was never sent to was rested")
	}
	if err := try.metered(func(context.Context) error { return nil })(context.Background()); err != nil || try.sends != 1 {
		t.Fatalf("admitted send = %v with %d sends counted, want one", err, try.sends)
	}
	try.finish(context.Background())
	if candidate := pool.candidates[unsent]; candidate.failures != 1 {
		t.Fatal("a seed that failed after a send was not rested")
	}

	var disabled *trailSeedTry
	pace := func(context.Context) error { return refusal }
	if err := disabled.metered(pace)(context.Background()); !errors.Is(err, refusal) {
		t.Fatalf("disabled try changed the send gate: %v", err)
	}
	disabled.confirm()
	disabled.finish(context.Background())
}
