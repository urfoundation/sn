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
// confirmation stats). TakeEgressIpHashes() rotates those sets so the steerer can
// count each fleet's distinct routable IPs and split shared ones (§8.4). The
// head window is the native tempo, independent of the longer settlement epoch:
// it is atomically detached by the native steerer, not reset by Fold. The sets
// are ephemeral, are not a_min-gated (one verified hop proves an IP routable),
// and the durable smoothing is the steerer's per-UID score EMA.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/protocol"
)

// statsLatencyBuckets is the number of log2 latency buckets. Bucket i
// covers [2^(i-1), 2^i) ms (bucket 0 is < 1 ms); the last bucket is
// unbounded above (2^30 ms ≈ 12 days, far beyond any StepTimeout).
const statsLatencyBuckets = 31

// StatsConfig tunes the engine. Zero values take the documented defaults.
type StatsConfig struct {
	AMin             uint64  // minimum exposure to score a provider (§7.4); default 30
	Alpha            float64 // legacy/reporting EMA; default 0.1
	Z                float64 // legacy/reporting Wilson z; default 1.96 (95%)
	LatRefMs         float64 // legacy/reporting latency reference; default 4000
	AlphaNumerator   uint64  // exact production EMA numerator; default 1
	AlphaDenominator uint64  // exact production EMA denominator; default 10
	LatRefMillis     uint64  // exact production latency reference; default 4000
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
	if self.AlphaDenominator == 0 {
		self.AlphaNumerator = 1
		self.AlphaDenominator = 10
	}
	if self.AlphaNumerator > self.AlphaDenominator {
		self.AlphaNumerator = self.AlphaDenominator
	}
	if self.LatRefMillis == 0 {
		self.LatRefMillis = 4000
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
	Version                        int                          `json:"v"`
	SettlementEpoch                *uint64                      `json:"settlement_epoch,omitempty"`
	EgressGeneration               uint64                       `json:"egress_generation,omitempty"`
	AttemptLastAppliedSequence     uint64                       `json:"attempt_last_applied_sequence,omitempty"`
	AttemptSettlementFirstSequence uint64                       `json:"attempt_settlement_first_sequence,omitempty"`
	AttemptEgressFirstSequence     uint64                       `json:"attempt_egress_first_sequence,omitempty"`
	SettlementTransition           *AttemptSettlementTransition `json:"settlement_transition,omitempty"`
	Ema                            map[string]float64           `json:"ema"`
	EmaPPM                         map[string]uint32            `json:"ema_ppm,omitempty"`
	Window                         map[string]*ProviderWindow   `json:"window"`
	Egress                         map[string][]string          `json:"egress,omitempty"`
}

// StatsEngine aggregates per-provider counters and cross-epoch EMAs.
// Safe for concurrent use.
type StatsEngine struct {
	mu     sync.Mutex
	cfg    StatsConfig
	window map[connect.Id]*ProviderWindow
	ema    map[connect.Id]float64
	emaPPM map[connect.Id]uint32
	// egress is the per-provider set of distinct routable egress-IP-hashes seen
	// in the current native-tempo window (§11.1, D27). It is independent of the
	// settlement-quality Fold clock and remains ephemeral; the steerer persists
	// the derived per-fleet EMA across tempos.
	egress map[connect.Id]map[[32]byte]bool

	settlementEpoch      uint64
	settlementEpochKnown bool
	egressGeneration     uint64

	attemptLedger                  *AttemptLedger
	attemptLastAppliedSequence     uint64
	attemptSettlementFirstSequence uint64
	attemptEgressFirstSequence     uint64
	activeAttemptCount             uint64
	attemptCutPending              bool
	settlementTransition           *AttemptSettlementTransition
}

func NewStatsEngine(cfg StatsConfig) *StatsEngine {
	return &StatsEngine{
		cfg:    cfg.withDefaults(),
		window: map[connect.Id]*ProviderWindow{},
		ema:    map[connect.Id]float64{},
		emaPPM: map[connect.Id]uint32{},
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

// TakeEgressIpHashes atomically returns and rotates the native-tempo head
// window. Proofs recorded after the swap belong to the following tempo and
// cannot be erased by a concurrent copy-then-clear race.
func (self *StatsEngine) TakeEgressIpHashes() map[connect.Id]map[[32]byte]bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	out := make(map[connect.Id]map[[32]byte]bool, len(self.egress))
	for id, set := range self.egress {
		cp := make(map[[32]byte]bool, len(set))
		for hash := range set {
			cp[hash] = true
		}
		out[id] = cp
	}
	self.egress = map[connect.Id]map[[32]byte]bool{}
	return out
}

// ProviderIDs returns the union of window, EMA and egress identities. Binding
// tier exclusion is based on membership, not on whether a provider happened to
// expose a routable prefix in this window.
func (self *StatsEngine) ProviderIDs() []connect.Id {
	self.mu.Lock()
	defer self.mu.Unlock()
	set := map[connect.Id]bool{}
	for id := range self.window {
		set[id] = true
	}
	for id := range self.emaPPM {
		set[id] = true
	}
	for id := range self.egress {
		set[id] = true
	}
	out := make([]connect.Id, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LessThan(out[j]) })
	return out
}

// qualityRawLocked computes the documented composite for one window.
func (self *StatsEngine) qualityRawLocked(w *ProviderWindow) float64 {
	wilson := WilsonLower(w.Confirmations, w.Assignments, self.cfg.Z)
	p95 := w.Percentile(0.95)
	return wilson * (self.cfg.LatRefMs / (self.cfg.LatRefMs + p95))
}

// qualityRawPPMLocked is the release-1.0 score transform. The Wilson bound,
// latency penalty and EMA all use integer arithmetic, making the score stable
// across architectures and independent implementations.
func (self *StatsEngine) qualityRawPPMLocked(w *ProviderWindow) uint32 {
	reliability := uint64(protocol.ReliabilityPPM(w.Confirmations, w.Assignments, self.cfg.AMin))
	buckets := make([]uint64, statsLatencyBuckets)
	copy(buckets, w.LatencyBuckets[:])
	p95, err := releaseP95UpperMillis(buckets)
	if err != nil {
		return 0
	}
	denom := self.cfg.LatRefMillis + p95
	if denom == 0 {
		return 0
	}
	return uint32(reliability * self.cfg.LatRefMillis / denom)
}

func (self *StatsEngine) blendPPM(raw, prior uint32) uint32 {
	n, d := self.cfg.AlphaNumerator, self.cfg.AlphaDenominator
	if d == 0 {
		return raw
	}
	// Both operands are <= 1e6; the policy-sized rational cannot overflow.
	return uint32((n*uint64(raw) + (d-n)*uint64(prior)) / d)
}

// QualityPPM is the production per-provider quality map. Sparse providers
// carry a prior EMA but are never bootstrapped from fewer than a_min samples.
func (self *StatsEngine) QualityPPM() map[connect.Id]uint32 {
	self.mu.Lock()
	defer self.mu.Unlock()
	out := make(map[connect.Id]uint32, len(self.emaPPM))
	for id, value := range self.emaPPM {
		out[id] = value
	}
	for id, w := range self.window {
		if w.Assignments < self.cfg.AMin {
			continue
		}
		raw := self.qualityRawPPMLocked(w)
		if prior, ok := self.emaPPM[id]; ok {
			out[id] = self.blendPPM(raw, prior)
		} else {
			out[id] = raw
		}
	}
	return out
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
	self.foldWithLock()
}

// foldWithLock advances the quality EMA and clears one settlement window. The
// caller must hold the engine state lock.
func (self *StatsEngine) foldWithLock() {
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
		rawPPM := self.qualityRawPPMLocked(w)
		if prior, ok := self.emaPPM[id]; ok {
			self.emaPPM[id] = self.blendPPM(rawPPM, prior)
		} else {
			self.emaPPM[id] = rawPPM
		}
	}
	self.window = map[connect.Id]*ProviderWindow{}
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

// snapshotWithLock copies the complete durable statistics state. The caller
// must hold the engine state lock.
func (self *StatsEngine) snapshotWithLock() statsSnapshot {
	version := 4
	if self.attemptLedger != nil || self.attemptLastAppliedSequence != 0 || self.attemptSettlementFirstSequence != 0 || self.attemptEgressFirstSequence != 0 {
		version = 5
	}
	snapshot := statsSnapshot{
		Version: version, Ema: map[string]float64{}, EmaPPM: map[string]uint32{},
		Window: map[string]*ProviderWindow{}, Egress: map[string][]string{},
		EgressGeneration: self.egressGeneration, AttemptLastAppliedSequence: self.attemptLastAppliedSequence,
		AttemptSettlementFirstSequence: self.attemptSettlementFirstSequence, AttemptEgressFirstSequence: self.attemptEgressFirstSequence,
		SettlementTransition: self.settlementTransition,
	}
	if self.settlementEpochKnown {
		epoch := self.settlementEpoch
		snapshot.SettlementEpoch = &epoch
	}
	for id, v := range self.ema {
		snapshot.Ema[id.String()] = v
	}
	for id, v := range self.emaPPM {
		snapshot.EmaPPM[id.String()] = v
	}
	for id, w := range self.window {
		cp := *w
		snapshot.Window[id.String()] = &cp
	}
	for id, hashes := range self.egress {
		encoded := make([]string, 0, len(hashes))
		for hash := range hashes {
			encoded = append(encoded, fmt.Sprintf("0x%x", hash))
		}
		sort.Strings(encoded)
		snapshot.Egress[id.String()] = encoded
	}
	return snapshot
}

// encodeStatsSnapshot returns the durable human-readable state representation.
func encodeStatsSnapshot(snapshot statsSnapshot) ([]byte, error) {
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Save persists a snapshot to <dir>/stats.json.
func (self *StatsEngine) Save(dir string) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.saveWithLock(dir)
}

// saveWithLock serializes the exact current state while the caller holds the
// state lock. Keeping the lock through rename prevents an older periodic save
// from overwriting a newer epoch fold or native-window cut.
func (self *StatsEngine) saveWithLock(dir string) error {
	b, err := encodeStatsSnapshot(self.snapshotWithLock())
	if err != nil {
		return err
	}
	return atomicStateWrite(filepath.Join(dir, "stats.json"), b, 0o600)
}

// AdvanceSettlementEpoch durably applies at most one exact boundary fold. It
// holds the statistics lock through the atomic write so event recording cannot
// enter an epoch until that epoch owns its persisted state. A skipped epoch is
// unrecoverable from local counters and therefore fails closed.
func (self *StatsEngine) AdvanceSettlementEpoch(epoch uint64, dir string) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.settlementEpochKnown {
		if epoch < self.settlementEpoch {
			return fmt.Errorf("settlement epoch regressed from %d to %d", self.settlementEpoch, epoch)
		}
		if epoch == self.settlementEpoch {
			return nil
		}
		if self.settlementEpoch == ^uint64(0) || epoch != self.settlementEpoch+1 {
			return fmt.Errorf("settlement epoch jumped from %d to %d", self.settlementEpoch, epoch)
		}
	} else if len(self.window) != 0 {
		return errors.New("legacy statistics window has no settlement epoch ownership")
	}
	if self.attemptLedger != nil && self.settlementEpochKnown && epoch != self.settlementEpoch {
		return errors.New("attempt-backed settlement advancement requires the validator-wide coordinator")
	}
	if self.attemptLedger != nil {
		self.attemptCutPending = true
		if self.activeAttemptCount != 0 {
			return errAttemptCutPending
		}
	}
	priorWindow := self.window
	priorEMA := self.ema
	priorEMAPPM := self.emaPPM
	priorEgress := self.egress
	priorEgressGeneration := self.egressGeneration
	priorEpoch, priorKnown := self.settlementEpoch, self.settlementEpochKnown
	priorSettlementFirst := self.attemptSettlementFirstSequence
	priorEgressFirst := self.attemptEgressFirstSequence
	if self.settlementEpochKnown {
		self.window = cloneProviderWindows(self.window)
		self.ema = maps.Clone(self.ema)
		self.emaPPM = maps.Clone(self.emaPPM)
		self.foldWithLock()
	}
	self.settlementEpoch, self.settlementEpochKnown = epoch, true
	if self.attemptLedger != nil {
		if self.egressGeneration == ^uint64(0) {
			self.window, self.ema, self.emaPPM = priorWindow, priorEMA, priorEMAPPM
			self.settlementEpoch, self.settlementEpochKnown = priorEpoch, priorKnown
			return errors.New("release statistics egress generation overflow")
		}
		nextSequence := self.attemptLedger.LastSequence() + 1
		self.attemptSettlementFirstSequence = nextSequence
		self.attemptEgressFirstSequence = nextSequence
		self.egress = map[connect.Id]map[[32]byte]bool{}
		self.egressGeneration++
	}
	err := self.saveWithLock(dir)
	if err != nil {
		self.window, self.ema, self.emaPPM = priorWindow, priorEMA, priorEMAPPM
		self.egress, self.egressGeneration = priorEgress, priorEgressGeneration
		self.settlementEpoch, self.settlementEpochKnown = priorEpoch, priorKnown
		self.attemptSettlementFirstSequence, self.attemptEgressFirstSequence = priorSettlementFirst, priorEgressFirst
		return err
	}
	self.attemptCutPending = false
	return nil
}

// Load restores a snapshot from <dir>/stats.json; a missing file is a clean
// start. Corrupt, ambiguous or partially canonical state fails closed.
func (self *StatsEngine) Load(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "stats.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var snap statsSnapshot
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snap); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("statistics snapshot contains trailing JSON")
		}
		return err
	}
	if snap.Version < 1 || snap.Version > 5 {
		return fmt.Errorf("unsupported statistics snapshot version %d", snap.Version)
	}
	if snap.Version < 4 && len(snap.Egress) != 0 {
		return errors.New("legacy statistics egress window has no durable generation")
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if len(self.window) != 0 || len(self.ema) != 0 || len(self.emaPPM) != 0 || len(self.egress) != 0 || self.settlementEpochKnown || self.egressGeneration != 0 || self.settlementTransition != nil {
		return errors.New("statistics engine must be empty before loading state")
	}
	if snap.SettlementEpoch != nil {
		self.settlementEpoch = *snap.SettlementEpoch
		self.settlementEpochKnown = true
	}
	self.egressGeneration = snap.EgressGeneration
	self.attemptLastAppliedSequence = snap.AttemptLastAppliedSequence
	self.attemptSettlementFirstSequence = snap.AttemptSettlementFirstSequence
	self.attemptEgressFirstSequence = snap.AttemptEgressFirstSequence
	self.settlementTransition = snap.SettlementTransition
	if snap.Version < 5 && (self.attemptLastAppliedSequence != 0 || self.attemptSettlementFirstSequence != 0 || self.attemptEgressFirstSequence != 0) {
		return errors.New("legacy statistics snapshot contains attempt ledger cursors")
	}
	if snap.Version == 5 && (self.attemptSettlementFirstSequence == 0 || self.attemptEgressFirstSequence < self.attemptSettlementFirstSequence || self.attemptLastAppliedSequence+1 < self.attemptEgressFirstSequence) {
		return errors.New("statistics attempt ledger cursors are invalid")
	}
	for idStr, v := range snap.Ema {
		id, err := connect.ParseId(idStr)
		if err != nil || id.String() != idStr || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return fmt.Errorf("invalid persisted reporting EMA for provider %q", idStr)
		}
		self.ema[id] = v
	}
	for idStr, v := range snap.EmaPPM {
		id, err := connect.ParseId(idStr)
		if err != nil || id.String() != idStr || v > 1_000_000 {
			return fmt.Errorf("invalid persisted exact EMA for provider %q", idStr)
		}
		self.emaPPM[id] = v
	}
	// Deterministic migration from the pre-v2 reporting EMA. This occurs once;
	// all subsequent folds and snapshots use the exact integer representation.
	if len(snap.EmaPPM) == 0 {
		for id, v := range self.ema {
			if v <= 0 {
				self.emaPPM[id] = 0
			} else if v >= 1 {
				self.emaPPM[id] = 1_000_000
			} else {
				self.emaPPM[id] = uint32(math.Floor(v * 1_000_000))
			}
		}
	}
	for idStr, w := range snap.Window {
		id, err := connect.ParseId(idStr)
		if err != nil || id.String() != idStr || w == nil || w.Confirmations > w.Assignments {
			return fmt.Errorf("invalid persisted window for provider %q", idStr)
		}
		var samples uint64
		for _, count := range w.LatencyBuckets {
			if ^uint64(0)-samples < count {
				return fmt.Errorf("persisted latency samples overflow for provider %q", idStr)
			}
			samples += count
		}
		if samples != w.Confirmations {
			return fmt.Errorf("persisted latency samples differ from confirmations for provider %q", idStr)
		}
		cp := *w
		self.window[id] = &cp
	}
	for idStr, encodedHashes := range snap.Egress {
		id, err := connect.ParseId(idStr)
		if err != nil || id.String() != idStr {
			return fmt.Errorf("invalid persisted egress provider id %q", idStr)
		}
		prior := ""
		for _, encodedHash := range encodedHashes {
			if encodedHash <= prior {
				return fmt.Errorf("persisted egress hashes for %s are not strictly ordered", id)
			}
			hash, err := parseReleaseHex32("persisted egress hash", encodedHash, false)
			if err != nil {
				return err
			}
			if self.egress[id] == nil {
				self.egress[id] = map[[32]byte]bool{}
			}
			self.egress[id][hash] = true
			prior = encodedHash
		}
	}
	if self.settlementTransition != nil {
		if err := verifyAttemptSettlementTransitionForMeasurement(self.settlementTransition, self.releaseStatsMeasurementWithLock()); err != nil {
			return fmt.Errorf("persisted settlement transition: %w", err)
		}
	}
	return nil
}

// cloneProviderWindows makes a deep copy for transactional fold rollback.
func cloneProviderWindows(windows map[connect.Id]*ProviderWindow) map[connect.Id]*ProviderWindow {
	cloned := make(map[connect.Id]*ProviderWindow, len(windows))
	for id, window := range windows {
		if window == nil {
			continue
		}
		windowCopy := *window
		cloned[id] = &windowCopy
	}
	return cloned
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
