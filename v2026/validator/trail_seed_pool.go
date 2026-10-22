package validator

// trail_seed_pool.go — entry providers this validator has seen answer.
//
// The seed hop is the validator's own pick of a working entry and is never
// measured (VALIDATOR.md §4.1, §7.6). Only the M−1 server-assigned hops are
// samples, and the server draws them without regard to the seed beyond leaving
// it out of its own trail (§5.1). Discovery returns providers the server lists
// as online, many of which no longer answer, so a trail spends a whole step
// timeout on a dead entry before it can measure anything.
//
// The pool remembers the providers through whose egress this process received
// a server-authenticated answer, as a seed or as a confirmed hop, and seeds
// from them while enough are known. It is process-local and never persisted,
// and it changes no policy, config, state or wire format.

import (
	"context"
	mathrand "math/rand"
	"sort"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026"
)

const (
	// A provider stays a candidate this long after its last authenticated
	// answer through this process.
	trailSeedProofHorizon = 2 * time.Hour
	// With fewer candidates the pool defers to discovery. A seed is left out of
	// its own trail's sampling (§5.1), so this bounds any provider's loss of
	// assignments to 1/16 while the pool chooses.
	trailSeedPoolMinimum = 16
	// A seed that failed rests this long, doubling per consecutive failure, so
	// a dead provider is not chosen again on the next trail while a live one
	// that failed for the validator's own reasons soon returns.
	trailSeedRest = 10 * time.Minute
	// Bounds memory however many providers confirm hops.
	trailSeedPoolMaximum = 4096
)

// One provider's standing as an entry. A new proof clears its failures.
type trailSeedCandidate struct {
	proven   time.Time
	rest     time.Time
	failures int
}

// Safe for the engine's concurrent workers. The clock and draw are seams so
// tests stay deterministic; production uses time.Now and math/rand like the
// discovery picker.
type trailSeedPool struct {
	self    connect.Id
	horizon time.Duration
	minimum int
	now     func() time.Time
	intn    func(int) int

	stateLock  sync.Mutex
	candidates map[connect.Id]*trailSeedCandidate
}

// A zero horizon follows the release runner's SEED gate, so single-trail
// callers and unit tests keep their picker's exact choice; a negative horizon
// disables the pool. The current window's confirmed providers start the pool,
// because a restarted validator would otherwise seed blind until it had
// confirmed hops again.
func newTrailSeedPool(cfg TrailEngineConfig, self connect.Id, stats *StatsEngine) *trailSeedPool {
	horizon := cfg.ConfirmedSeedHorizon
	if horizon == 0 && cfg.SeedAttemptInterval > 0 {
		horizon = trailSeedProofHorizon
	}
	if horizon <= 0 {
		return nil
	}
	pool := &trailSeedPool{
		self:       self,
		horizon:    horizon,
		minimum:    trailSeedPoolMinimum,
		now:        time.Now,
		intn:       mathrand.Intn,
		candidates: map[connect.Id]*trailSeedCandidate{},
	}
	if stats != nil {
		at := pool.now()
		for _, hop := range stats.confirmedProviderIDs() {
			pool.prove(hop, at)
		}
	}
	return pool
}

// Providers with a confirmation in the current window, in identity order.
func (self *StatsEngine) confirmedProviderIDs() []connect.Id {
	self.mu.Lock()
	defer self.mu.Unlock()
	out := make([]connect.Id, 0, len(self.window))
	for id, window := range self.window {
		if window != nil && window.Confirmations > 0 {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LessThan(out[j]) })
	return out
}

// Records an authenticated answer through hop's egress. The validator itself
// and the zero identity are never candidates.
func (self *trailSeedPool) prove(hop connect.Id, at time.Time) {
	if self == nil || hop == (connect.Id{}) || hop == self.self {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	candidate := self.candidates[hop]
	if candidate == nil {
		if len(self.candidates) >= trailSeedPoolMaximum {
			self.dropOldestWithLock()
		}
		candidate = &trailSeedCandidate{}
		self.candidates[hop] = candidate
	}
	if at.After(candidate.proven) {
		candidate.proven = at
	}
	candidate.rest, candidate.failures = time.Time{}, 0
}

// Rests a known candidate after a seed attempt through it failed. A provider
// that discovery supplied and that never answered is not tracked at all.
func (self *trailSeedPool) fail(hop connect.Id, at time.Time) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	candidate := self.candidates[hop]
	if candidate == nil {
		return
	}
	if candidate.failures < 16 {
		candidate.failures++
	}
	rest := trailSeedRest << (candidate.failures - 1)
	if rest > self.horizon {
		rest = self.horizon
	}
	candidate.rest = at.Add(rest)
}

// Draws uniformly among the candidates that are within the horizon and not
// resting, in identity order so an injected draw is reproducible. It answers
// false when fewer than minimum qualify.
func (self *trailSeedPool) pick(at time.Time, minimum int) (connect.Id, bool) {
	if self == nil {
		return connect.Id{}, false
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	eligible := make([]connect.Id, 0, len(self.candidates))
	for hop, candidate := range self.candidates {
		if at.Sub(candidate.proven) > self.horizon {
			delete(self.candidates, hop)
			continue
		}
		if candidate.rest.After(at) {
			continue
		}
		eligible = append(eligible, hop)
	}
	if len(eligible) == 0 || len(eligible) < minimum {
		return connect.Id{}, false
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].LessThan(eligible[j]) })
	return eligible[self.intn(len(eligible))], true
}

func (self *trailSeedPool) dropOldestWithLock() {
	var oldest connect.Id
	found := false
	for hop, candidate := range self.candidates {
		if !found || candidate.proven.Before(self.candidates[oldest].proven) {
			oldest, found = hop, true
		}
	}
	if found {
		delete(self.candidates, oldest)
	}
}

// Prefers a provider this process has seen answer and otherwise asks
// discovery, as before. When discovery itself fails, one retry draws from
// whatever candidates remain, so a discovery outage does not stop trails that
// a known entry could still start. There is no loop: a trail makes at most one
// discovery call and one retry draw.
func (self *TrailEngine) pickTrailSeed(ctx context.Context) (connect.Id, error) {
	pool := self.seedPool
	if pool == nil {
		return self.pickSeed(ctx)
	}
	if hop, ok := pool.pick(pool.now(), pool.minimum); ok {
		return hop, nil
	}
	hop, err := self.pickSeed(ctx)
	if err == nil || ctx.Err() != nil {
		return hop, err
	}
	if retry, ok := pool.pick(pool.now(), 1); ok {
		return retry, nil
	}
	return connect.Id{}, err
}

// A server-authenticated response confirmed hop, so its egress answers now.
func (self *TrailEngine) observeConfirmedHop(hop connect.Id) {
	if pool := self.seedPool; pool != nil {
		pool.prove(hop, pool.now())
	}
}

// One trail's use of its seed. Nil when the pool is disabled, so legacy
// callers run exactly the code they ran before.
type trailSeedTry struct {
	pool   *trailSeedPool
	hop    connect.Id
	sends  int
	proven bool
}

func (self *TrailEngine) beginSeedTry(hop connect.Id) *trailSeedTry {
	if self.seedPool == nil {
		return nil
	}
	return &trailSeedTry{pool: self.seedPool, hop: hop}
}

// Counts the SEED sends that pacing, attempt reservation and the request
// journal admitted. A trail refused before it sent anything, for example while
// an epoch cut is pending, says nothing about its seed.
func (self *trailSeedTry) metered(before func(context.Context) error) func(context.Context) error {
	if self == nil {
		return before
	}
	return func(ctx context.Context) error {
		if err := before(ctx); err != nil {
			return err
		}
		self.sends++
		return nil
	}
}

// The first ASSIGN verified with this seed as its confirmed hop.
func (self *trailSeedTry) confirm() {
	if self == nil {
		return
	}
	self.proven = true
	self.pool.prove(self.hop, self.pool.now())
}

// Rests the seed when a SEED was sent through it and no authenticated ASSIGN
// came back. The owner's cancellation is not the seed's failure.
func (self *trailSeedTry) finish(ctx context.Context) {
	if self == nil || self.proven || self.sends == 0 || ctx.Err() != nil {
		return
	}
	self.pool.fail(self.hop, self.pool.now())
}
