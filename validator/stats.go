package validator

// stats.go — per-provider statistics (VALIDATOR.md §7, §11.1).
//
// The engine counts only SERVER-ASSIGNED hops: an exposure (a) is recorded
// when an ASSIGN names the provider as the pending next hop, a confirmation
// (c) when the EXTEND through it succeeds. The validator-chosen seed hop is
// never recorded — the trail engine simply has no record call for it — which
// implements the §7.6 seed exclusion structurally (the rule that also stops
// a top miner farming its own measurement). Failures are implicit: f = a − c
// (per-transition attribution, §7.2 — the pending hop that never confirmed).
//
// Two signals per provider (§7.3):
//   - liveness: Wilson-score lower bound on c/a (never the raw ratio),
//   - latency: percentiles recovered from log2-spaced millisecond buckets
//     (memory-bounded; the same bucket idea as the server's Redis HINCRBY
//     histogram).
//
// The composite quality q_p (documented v1 formula; WHITEPAPER §11.1 leaves
// the composition open):
//
//	q_raw = WilsonLower(c, a, z=1.96) × latRef / (latRef + p95_ms)
//
// bounded in (0, 1], monotone in liveness and decreasing in tail latency
// (latRef default 4000 ms). Providers with a < a_min in the current window
// are not scored this window (§7.4). Across epochs each provider's quality
// is EMA-smoothed with α = 0.1 (§11.1): Fold() applies
// ema ← α·q_raw + (1−α)·ema at each epoch boundary; between folds Quality()
// previews the same blend so steering inside an epoch already reflects
// fresh data without double-counting it at the fold.
//
// Latency here is the validator's own request round-trip through the hop
// (POST sent → response read). The server keeps its own authoritative
// assigned_at → confirmed_at stamps (§3.4); the local measurement is the
// steering signal available client-side.
//
// Head routable-IP breadth (§11.1, D27). Alongside the quality signals the
// engine accumulates, per provider, the set of distinct egress-IP-hashes it
// served on VERIFIED trail hops (RecordEgressHash, fed from the signed FINAL
// proof — server-assigned hops only, seed excluded, §7.6, symmetric with the
// confirmation stats). EgressIpHashes() exposes those sets so the steerer can
// count each fleet's distinct routable IPs and split shared ones (§8.4). The
// sets are windowed like the counters (reset at Fold) and left ephemeral — they
// are not a_min-gated (one verified hop proves an IP routable) and the durable
// smoothing is the steerer's per-UID score EMA, not this window.

import (
	"encoding/json"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/urnetwork/connect"
)

// statsLatencyBuckets is the number of log2 latency buckets. Bucket i
// covers [2^(i-1), 2^i) ms (bucket 0 is < 1 ms); the last bucket is
// unbounded above (2^30 ms ≈ 12 days, far beyond any StepTimeout).
const statsLatencyBuckets = 31

// StatsConfig tunes the engine. Zero values take the documented defaults.
type StatsConfig struct {
	AMin     uint64  // minimum exposure to score a provider (§7.4); default 30
	Alpha    float64 // cross-epoch EMA weight (§11.1); default 0.1
	Z        float64 // Wilson z; default 1.96 (95%)
	LatRefMs float64 // latency reference for the quality composite; default 4000
}

func (self StatsConfig) withDefaults() StatsConfig {
	if self.AMin == 0 {
		self.AMin = 30
	}
	if self.Alpha == 0 {
		self.Alpha = 0.1
	}
	if self.Z == 0 {
		self.Z = 1.96
	}
	if self.LatRefMs == 0 {
		self.LatRefMs = 4000
	}
	return self
}

// ProviderWindow is one provider's counters for the current epoch window.
type ProviderWindow struct {
	Assignments    uint64                      `json:"a"`
	Confirmations  uint64                      `json:"c"`
	LatencyBuckets [statsLatencyBuckets]uint64 `json:"lat"`
}

// latencyBucket maps a millisecond sample to its log2 bucket.
func latencyBucket(ms float64) int {
	if ms < 1 {
		return 0
	}
	b := bits.Len64(uint64(ms)) // 2^(b-1) <= ms < 2^b
	if b >= statsLatencyBuckets {
		b = statsLatencyBuckets - 1
	}
	return b
}

// bucketUpperMs is the conservative (upper edge) latency of bucket i.
func bucketUpperMs(i int) float64 {
	return float64(uint64(1) << uint(i))
}

// Percentile returns the p-quantile (p in [0,1]) of the window's recorded
// latencies as the upper edge of the covering bucket — a conservative
// estimate. Returns 0 when no latency was recorded.
func (self *ProviderWindow) Percentile(p float64) float64 {
	var total uint64
	for _, n := range self.LatencyBuckets {
		total += n
	}
	if total == 0 {
		return 0
	}
	rank := uint64(math.Ceil(p * float64(total)))
	if rank == 0 {
		rank = 1
	}
	var cum uint64
	for i, n := range self.LatencyBuckets {
		cum += n
		if cum >= rank {
			return bucketUpperMs(i)
		}
	}
	return bucketUpperMs(statsLatencyBuckets - 1)
}

// WilsonLower is the Wilson score interval lower bound for c successes out
// of a trials at confidence z (§7.3 — report the interval, not the ratio).
// Returns 0 when a == 0.
func WilsonLower(c uint64, a uint64, z float64) float64 {
	if a == 0 {
		return 0
	}
	n := float64(a)
	p := float64(c) / n
	z2 := z * z
	denom := 1 + z2/n
	center := p + z2/(2*n)
	margin := z * math.Sqrt(p*(1-p)/n+z2/(4*n*n))
	lower := (center - margin) / denom
	if lower < 0 {
		return 0
	}
	return lower
}

// statsSnapshot is the persisted form (state_dir/stats.json).
type statsSnapshot struct {
	Version int                        `json:"v"`
	Ema     map[string]float64         `json:"ema"`
	Window  map[string]*ProviderWindow `json:"window"`
}

// StatsEngine aggregates per-provider counters and cross-epoch EMAs.
// Safe for concurrent use.
type StatsEngine struct {
	mu     sync.Mutex
	cfg    StatsConfig
	window map[connect.Id]*ProviderWindow
	ema    map[connect.Id]float64
	// egress is the per-provider set of distinct routable egress-IP-hashes seen
	// this window (§11.1, D27 — the head routable-IP score). Reset at Fold,
	// ephemeral (not persisted): it rebuilds from fresh trails, and the steerer
	// EMA-smooths the derived per-fleet score across tempos.
	egress map[connect.Id]map[[32]byte]bool
}

func NewStatsEngine(cfg StatsConfig) *StatsEngine {
	return &StatsEngine{
		cfg:    cfg.withDefaults(),
		window: map[connect.Id]*ProviderWindow{},
		ema:    map[connect.Id]float64{},
		egress: map[connect.Id]map[[32]byte]bool{},
	}
}

func (self *StatsEngine) windowFor(hop connect.Id) *ProviderWindow {
	w, ok := self.window[hop]
	if !ok {
		w = &ProviderWindow{}
		self.window[hop] = w
	}
	return w
}

// RecordAssignment records a server-assigned exposure for hop (§7.2 a_Y).
// Call it when an ASSIGN names hop as the pending next hop — never for the
// validator-chosen seed (§7.6).
func (self *StatsEngine) RecordAssignment(hop connect.Id) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.windowFor(hop).Assignments++
}

// RecordConfirmation records a confirmed step into hop with the locally
// measured round-trip latency (§7.5 — record per step, at confirmation
// time, so an abandoned trail keeps the slow hop's sample).
func (self *StatsEngine) RecordConfirmation(hop connect.Id, latencyMs float64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	w := self.windowFor(hop)
	w.Confirmations++
	w.LatencyBuckets[latencyBucket(latencyMs)]++
}

// RecordEgressHash records that hop served a verified trail hop from the given
// egress-IP-hash (§8.4/§11.1, D27 — the head routable-IP score). Call it per
// server-assigned hop of a VERIFIED FINAL proof, never for the seed (§7.6) — the
// same server-assigned-only, seed-excluded rule the confirmation stats follow.
// The zero hash (an unstamped hop) is ignored.
func (self *StatsEngine) RecordEgressHash(hop connect.Id, egressHash [32]byte) {
	if egressHash == ([32]byte{}) {
		return
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	set, ok := self.egress[hop]
	if !ok {
		set = map[[32]byte]bool{}
		self.egress[hop] = set
	}
	set[egressHash] = true
}

// EgressIpHashes returns a copy of the per-provider egress-IP-hash sets seen this
// window (§11.1, D27). The steerer unions these across a fleet's client_ids and
// splits any hash shared between fleets to score routable-IP breadth (§8.4). Not
// a_min-gated: a single verified hop makes an IP routable.
func (self *StatsEngine) EgressIpHashes() map[connect.Id]map[[32]byte]bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	out := make(map[connect.Id]map[[32]byte]bool, len(self.egress))
	for id, set := range self.egress {
		cp := make(map[[32]byte]bool, len(set))
		for h := range set {
			cp[h] = true
		}
		out[id] = cp
	}
	return out
}

// qualityRawLocked computes the documented composite for one window.
func (self *StatsEngine) qualityRawLocked(w *ProviderWindow) float64 {
	wilson := WilsonLower(w.Confirmations, w.Assignments, self.cfg.Z)
	p95 := w.Percentile(0.95)
	return wilson * (self.cfg.LatRefMs / (self.cfg.LatRefMs + p95))
}

// Quality returns the current per-provider quality map q_p: the cross-epoch
// EMA blended with the current window (preview of the next fold). Providers
// below a_min in the current window contribute their EMA unchanged; a
// provider with neither an EMA nor a scoreable window is omitted (§7.4 —
// never report sparse providers).
func (self *StatsEngine) Quality() map[connect.Id]float64 {
	self.mu.Lock()
	defer self.mu.Unlock()
	out := map[connect.Id]float64{}
	for id, ema := range self.ema {
		out[id] = ema
	}
	for id, w := range self.window {
		if w.Assignments < self.cfg.AMin {
			continue
		}
		raw := self.qualityRawLocked(w)
		if ema, ok := self.ema[id]; ok {
			out[id] = self.cfg.Alpha*raw + (1-self.cfg.Alpha)*ema
		} else {
			out[id] = raw
		}
	}
	return out
}

// Exposure returns the current window's assignment counts (used as the
// usage weights of the v1 Q_n aggregation, D-9).
func (self *StatsEngine) Exposure() map[connect.Id]uint64 {
	self.mu.Lock()
	defer self.mu.Unlock()
	out := map[connect.Id]uint64{}
	for id, w := range self.window {
		out[id] = w.Assignments
	}
	return out
}

// Fold applies the cross-epoch EMA (§11.1) and resets the window. Call at
// contract epoch boundaries. Providers below a_min carry their EMA forward
// untouched (one sparse epoch does not decay an established provider).
func (self *StatsEngine) Fold() {
	self.mu.Lock()
	defer self.mu.Unlock()
	for id, w := range self.window {
		if w.Assignments < self.cfg.AMin {
			continue
		}
		raw := self.qualityRawLocked(w)
		if ema, ok := self.ema[id]; ok {
			self.ema[id] = self.cfg.Alpha*raw + (1-self.cfg.Alpha)*ema
		} else {
			self.ema[id] = raw
		}
	}
	self.window = map[connect.Id]*ProviderWindow{}
	// The egress-IP-hash sets are windowed too (§11.1): the per-fleet score is
	// recomputed from the fresh window each epoch and EMA-smoothed by the steerer.
	self.egress = map[connect.Id]map[[32]byte]bool{}
}

// WindowCounts returns (a, c) for one provider — test/diagnostic hook.
func (self *StatsEngine) WindowCounts(hop connect.Id) (uint64, uint64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	w, ok := self.window[hop]
	if !ok {
		return 0, 0
	}
	return w.Assignments, w.Confirmations
}

// Save persists a snapshot to <dir>/stats.json.
func (self *StatsEngine) Save(dir string) error {
	self.mu.Lock()
	snap := statsSnapshot{
		Version: 1,
		Ema:     map[string]float64{},
		Window:  map[string]*ProviderWindow{},
	}
	for id, v := range self.ema {
		snap.Ema[id.String()] = v
	}
	for id, w := range self.window {
		cp := *w
		snap.Window[id.String()] = &cp
	}
	self.mu.Unlock()

	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".stats.json.tmp")
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "stats.json"))
}

// Load restores a snapshot from <dir>/stats.json; a missing file is a clean
// start. Unparseable ids are skipped.
func (self *StatsEngine) Load(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "stats.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var snap statsSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	for idStr, v := range snap.Ema {
		if id, err := connect.ParseId(idStr); err == nil {
			self.ema[id] = v
		}
	}
	for idStr, w := range snap.Window {
		if id, err := connect.ParseId(idStr); err == nil && w != nil {
			cp := *w
			self.window[id] = &cp
		}
	}
	return nil
}

// SortedQuality returns Quality() as a deterministic slice (by id) — used
// by status output and tests.
func (self *StatsEngine) SortedQuality() []struct {
	Id      connect.Id
	Quality float64
} {
	q := self.Quality()
	ids := make([]connect.Id, 0, len(q))
	for id := range q {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a].LessThan(ids[b]) })
	out := make([]struct {
		Id      connect.Id
		Quality float64
	}, len(ids))
	for i, id := range ids {
		out[i].Id = id
		out[i].Quality = q[id]
	}
	return out
}
