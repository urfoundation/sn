package main

// steer.go — the validator's second job (VALIDATOR.md §0.5/§11, WHITEPAPER
// §8.5/§10): each tempo, turn the per-provider statistics into ONE u16
// weight vector over miner UIDs and commit it under CRv4.
//
// Two-channel vector with the governance head share θ (§11.3), v0.4 (D25+D27):
//
//	head[u] = score(u)                       normalized so Σ head = θ
//	pool[n] = implied_usage_n × Q_n          normalized so Σ pool = 1 − θ
//	w = head ⊕ pool               → max_weight_limit cap → u16 → CRv4 commit
//
// where the TAIL is implied-usage × quality (D25) and the HEAD is each fleet's
// split-adjusted count of distinct routable egress-IP-hashes (D27):
//
//	implied_usage_n = epoch_deposit_n / rate(tier(conviction_n))   // §8.1
//	score(u)        = Σ_{h ∈ IPs(u)} 1 / claim(h)                  // §8.4
//
// with epoch_deposit_n / conviction_n summed from the on-chain Deposited event
// log (§7.5 — the contract stores no deposit ledger under D25), rate(·) from the
// published tier→rate schedule (§7.3, validator config), IPs(u) the union of a
// fleet's client_ids' routable egress-IP-hashes, and claim(h) the number of
// fleets sharing hash h (a shared hash splits equally — §8.4).
//
// v1 scope decisions (documented):
//   - HEAD TIER (§11.2/§11.4): the head is populated from the on-chain
//     client_id ⇄ hotkey binding. For each measured provider the validator
//     resolves its 32-byte client Ed25519 key (ckey) — via the /key API — and
//     reads headClientIdToHotkey(ckey); a non-zero hotkey that resolves to a
//     live metagraph UID makes the provider a member of that head UID's FLEET.
//     A fleet's weight is its split-adjusted routable-IP score (D27), EMA-
//     smoothed across tempos, NOT its providers' quality. Resolution FAILS
//     CLOSED: an unbound ckey or a hotkey with no live UID is skipped (never
//     weight a dead/absent UID). A bound provider is in the head tier ONLY —
//     it is excluded from the pool aggregation (symmetric with the server
//     dropping it from the pool payoutRoot). With no live head bindings
//     BuildWeightVector still cedes θ to the pools (never strand the head share).
//   - Q_n AGGREGATION (D-9): usage-weighted mean of per-provider q_p, with
//     the validator's own server-assigned exposure counts a_p as the usage
//     weights (the platform's billed-usage series is not client-visible).
//   - PROVIDER → NO ATTRIBUTION: not resolvable locally in v1 (needs an
//     operator-scoped provider census). Every pool receives the same global
//     Q_n, so pool weights reduce to ∝ D_n × globalQ — exactly the
//     single-NO bootstrap degeneracy PLAN.md §10 accepts for testnet. The
//     QualityAggregator seam is where a real per-NO roll-up lands.
//   - UID RESOLUTION: operator minerHotkey → UID via the metagraph
//     precompile scan (0x802 getUidCount/getHotkey — what IMetagraph
//     offers; there is no reverse lookup), falling back to the contract's
//     stored minerUid when the precompile is unavailable.
//   - SELF-MASK (§11.3): the validator's own hotkey UID is zeroed if it
//     ever appears among miner UIDs. The mask FAILS CLOSED (HF-4): a
//     metagraph read error reuses the last definitive answer, and with no
//     definitive answer yet the tempo's commit is skipped — the validator
//     never submits a vector it cannot prove is self-masked.

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/urnetwork/connect"

	"github.com/urnetwork/sn/crv4"
)

// PoolWeightInput is one NO pool's steering inputs for a tempo. The pool term
// is implied_usage × quality (D25): implied_usage = epoch_deposit / rate(tier),
// so a NO staked into a lower-rate tier posts less α for the same weight — the
// conviction stake is a discount, not a penalty, and the weight still tracks
// real revenue-backed usage rather than raw deposit (§8.1).
type PoolWeightInput struct {
	NoId         *big.Int
	Uid          uint16
	ImpliedUsage float64 // epoch_deposit_n / rate(tier(conviction_n)) (§8.1)
	Quality      float64 // Q_n
}

// HeadWeightInput is one top-level miner (fleet) UID's steering input. The head
// weight is the fleet's split-adjusted routable-IP score (D27), not quality.
type HeadWeightInput struct {
	Uid   uint16
	Score float64 // score(u) = Σ_{h ∈ IPs(u)} 1/claim(h), EMA-smoothed (§8.4)
}

// RateTier is one step of the published deposit-rate schedule (§7.3): a NO whose
// conviction (cumulative locked α, §7.2) is ≥ MinConviction pays Rate — the α it
// must deposit per unit of real usage. Higher tiers (more conviction) pay lower
// rates, so a committed NO needs less up-front α to signal the same usage.
type RateTier struct {
	MinConviction *big.Int // inclusive lower bound on cumulative locked α (rao)
	Rate          float64  // α per unit of usage at this tier; floored above zero
}

// RateSchedule is the ordered tier→rate schedule (§7.3), a published governance
// parameter read by validators (never consumed by the contract, §7.1). The zero
// tier (MinConviction 0) is the baseline (full) rate; rate(conviction) picks the
// highest tier whose MinConviction ≤ conviction. Rates are floored above zero
// (§7.3 — a zero rate would make any deposit imply unbounded usage, §8.1).
type RateSchedule struct {
	Tiers []RateTier // sorted ascending by MinConviction; Tiers[0].MinConviction = 0
}

// rateFloor is the smallest deposit rate the schedule will apply — the "floored
// above zero" guard (§7.3): implied_usage = deposit / rate stays finite even if a
// tier is (mis)configured to zero.
const rateFloor = 1e-9

// alphaRao is one α expressed in rao (the deposit event's base unit, 1e9 rao/α).
const alphaRao = 1_000_000_000

// DefaultRateSchedule is the in-code default (§7.3) until governance publishes
// one into config: a baseline rate at zero conviction that halves as a NO locks
// progressively more conviction stake. The absolute rate is a reference unit —
// only the ratios between tiers (and to epoch_deposit) matter for the weights.
func DefaultRateSchedule() RateSchedule {
	rao := func(alpha int64) *big.Int { return new(big.Int).Mul(big.NewInt(alpha), big.NewInt(alphaRao)) }
	return RateSchedule{Tiers: []RateTier{
		{MinConviction: big.NewInt(0), Rate: 1.0},   // zero tier = baseline
		{MinConviction: rao(1_000), Rate: 0.8},      // ≥ 1k α locked
		{MinConviction: rao(10_000), Rate: 0.6},     // ≥ 10k α locked
		{MinConviction: rao(100_000), Rate: 0.4},    // ≥ 100k α locked
		{MinConviction: rao(1_000_000), Rate: 0.25}, // ≥ 1M α locked
	}}
}

// Rate returns rate(tier(conviction)): the rate of the highest tier whose
// MinConviction ≤ conviction, floored above zero (§7.3). An empty schedule or a
// conviction below every tier falls back to the baseline rateFloor⁺.
func (self RateSchedule) Rate(conviction *big.Int) float64 {
	rate := 0.0
	for _, tier := range self.Tiers {
		if tier.MinConviction == nil {
			continue
		}
		if conviction.Cmp(tier.MinConviction) >= 0 {
			rate = tier.Rate // tiers ascending → last match is the highest tier
		}
	}
	if rate < rateFloor {
		return rateFloor
	}
	return rate
}

// sortTiers orders the schedule ascending by MinConviction so Rate's last-match
// scan is correct regardless of the config's declaration order.
func (self RateSchedule) sortTiers() RateSchedule {
	tiers := append([]RateTier(nil), self.Tiers...)
	sort.Slice(tiers, func(a, b int) bool {
		ca, cb := tiers[a].MinConviction, tiers[b].MinConviction
		if ca == nil {
			return cb != nil
		}
		if cb == nil {
			return false
		}
		return ca.Cmp(cb) < 0
	})
	return RateSchedule{Tiers: tiers}
}

// ClientKeyFunc resolves a measured provider's connect.Id (the platform
// client UUID the stats are keyed by) to its 32-byte client Ed25519 key
// (ckey) — the value the head binding stores as clientId. ok is false when
// the provider has no published key (skip it; it cannot be a bound head).
type ClientKeyFunc func(id connect.Id) (ckey [32]byte, ok bool, err error)

// HeadBindingReader resolves a provider ckey to its live head UID. It reads
// the on-chain head binding and maps the bound hotkey to a metagraph UID.
type HeadBindingReader interface {
	// HeadUid returns (uid, true) when ckey is bound to a hotkey that is a
	// live metagraph UID, and (0, false) when ckey is unbound OR the bound
	// hotkey is not a live UID — fail closed, never weight a dead/absent UID.
	HeadUid(ckey [32]byte) (uid uint16, ok bool, err error)
}

// chainHeadBindings is the production HeadBindingReader: headClientIdToHotkey
// (chain.go) for the ckey→hotkey binding, then the metagraph scan
// (FindUidByHotkey) for hotkey→live UID. Per-ckey eth_call at steer time is
// the simplest correct read; the measured set is bounded (max_uids ≤ 256) and
// the validator caches ckeys across tempos, so this stays a small number of
// round-trips per cycle. A multicall batch is a later optimization.
type chainHeadBindings struct {
	chain  *ChainClient
	netuid uint16
}

// NewChainHeadBindings wires a HeadBindingReader to the live chain client.
func NewChainHeadBindings(chain *ChainClient, netuid uint16) HeadBindingReader {
	return &chainHeadBindings{chain: chain, netuid: netuid}
}

func (self *chainHeadBindings) HeadUid(ckey [32]byte) (uint16, bool, error) {
	hotkey, err := self.chain.HeadClientIdToHotkey(ckey)
	if err != nil {
		return 0, false, err
	}
	if hotkey == ([32]byte{}) {
		return 0, false, nil // ckey not bound to any top-level miner
	}
	uid, found, err := self.chain.FindUidByHotkey(self.netuid, hotkey)
	if err != nil {
		return 0, false, err
	}
	if !found {
		return 0, false, nil // bound hotkey is not a live UID → fail closed
	}
	return uid, true, nil
}

// BuildWeightVector combines the two channels under the θ split (§11.3)
// into (uids, scores) for crv4.SubmitWeightsCRv4 (which applies
// max_weight_limit and u16 normalization). When one side is empty or sums
// to zero, the other side receives the whole weight (never strand a share
// on an empty tier). selfUid, when non-nil, is zeroed (self-mask).
func BuildWeightVector(
	pools []PoolWeightInput,
	head []HeadWeightInput,
	theta float64,
	selfUid *uint16,
) (uids []uint16, scores []float64, err error) {
	if theta < 0 || 1 < theta {
		return nil, nil, fmt.Errorf("theta %f outside [0, 1]", theta)
	}

	poolScores := map[uint16]float64{}
	poolSum := 0.0
	for _, pool := range pools {
		if pool.ImpliedUsage <= 0 || pool.Quality <= 0 {
			continue
		}
		score := pool.ImpliedUsage * pool.Quality
		if score <= 0 {
			continue
		}
		poolScores[pool.Uid] += score
		poolSum += score
	}

	headSums := map[uint16]float64{}
	headSum := 0.0
	for _, entry := range head {
		if entry.Score <= 0 {
			continue
		}
		headSums[entry.Uid] += entry.Score
		headSum += entry.Score
	}

	// Effective shares: an empty/zero side cedes its share to the other.
	headShare := theta
	poolShare := 1 - theta
	if headSum == 0 {
		headShare = 0
		poolShare = 1
	}
	if poolSum == 0 {
		poolShare = 0
		if headSum > 0 {
			headShare = 1
		}
	}

	combined := map[uint16]float64{}
	for uid, score := range poolScores {
		if poolSum > 0 {
			combined[uid] += poolShare * score / poolSum
		}
	}
	for uid, score := range headSums {
		if headSum > 0 {
			combined[uid] += headShare * score / headSum
		}
	}
	if selfUid != nil {
		delete(combined, *selfUid)
	}
	if len(combined) == 0 {
		return nil, nil, fmt.Errorf("no positive weights to set")
	}

	uids = make([]uint16, 0, len(combined))
	for uid := range combined {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(a, b int) bool { return uids[a] < uids[b] })
	scores = make([]float64, len(uids))
	for i, uid := range uids {
		scores[i] = combined[uid]
	}
	return uids, scores, nil
}

// QualityAggregator rolls per-provider quality up to a pool scalar Q_n
// (D-9 seam; see the package comment for the v1 posture).
type QualityAggregator interface {
	// PoolQuality returns Q_n for noId given the current per-provider
	// quality and exposure maps.
	PoolQuality(noId *big.Int, quality map[connect.Id]float64, exposure map[connect.Id]uint64) float64
}

// GlobalMeanQuality is the v1 aggregator: the exposure-weighted mean of all
// measured providers, identical for every NO (documented limitation).
type GlobalMeanQuality struct{}

func (GlobalMeanQuality) PoolQuality(_ *big.Int, quality map[connect.Id]float64, exposure map[connect.Id]uint64) float64 {
	var weightedSum, weightSum float64
	for id, q := range quality {
		w := float64(exposure[id])
		if w == 0 {
			w = 1 // EMA-only providers (no exposure this window) count once
		}
		weightedSum += q * w
		weightSum += w
	}
	if weightSum == 0 {
		return 0
	}
	return weightedSum / weightSum
}

// SteerConfig configures the steering loop.
type SteerConfig struct {
	Netuid        uint16
	Theta         float64
	TempoBlocks   uint64  // 0 = read the subnet tempo from chain
	BlockTimeSecs float64 // substrate block seconds (12 mainnet, 0.25 fast testnet)
	VersionKey    uint64
	SubstrateUrls []string
	// Rates is the published tier→rate schedule (§7.3, D25) the pool's implied
	// usage divides by. Empty ⇒ DefaultRateSchedule(); a governance-published
	// schedule is loaded here (the config seam).
	Rates RateSchedule
}

// Steerer runs the per-tempo weight pipeline.
type Steerer struct {
	chain      *ChainClient
	stats      *StatsEngine
	aggregator QualityAggregator
	hotkey     *crv4.Keypair
	cfg        SteerConfig

	// Head tier (§11.4). Both nil ⇒ the head is empty and the whole weight
	// goes to the pools (BuildWeightVector cedes θ). Set via SetHeadBindings.
	clientKey    ClientKeyFunc
	headBindings HeadBindingReader
	ckeyCache    map[connect.Id][32]byte // positive cache; ckeys are immutable

	// Self-mask cache (§11.3, HF-4): the last definitive metagraph answer
	// for the validator's own UID, reused when a tempo's read errors.
	selfMask selfMask

	// headScoreEma is the per-fleet-UID cross-tempo EMA of the split-adjusted
	// routable-IP score (§11.1, D27) — the head weight thrash-damper.
	headScoreEma map[uint16]float64

	// deposits caches the all-time per-NO conviction (cumulative locked α, §7.2)
	// summed from the Deposited event log, extended incrementally each tempo so
	// the validator never re-scans the whole chain (§8.1).
	deposits depositLedger

	epochSeen       bool
	lastFoldedEpoch uint64
}

// depositLedger caches the all-time per-NO conviction summed from the Deposited
// event log (§7.5), extended incrementally: each tempo only the blocks past
// scannedThrough are scanned and folded in, so the expensive genesis-to-tip scan
// happens once. The open epoch's deposits are read separately (a fresh, epoch-
// windowed scan) because they are a bounded window, not a growing cumulant.
type depositLedger struct {
	conviction     DepositSums // noId → cumulative all-time α (rao)
	scannedThrough uint64      // highest block folded into conviction (0 = none)
	started        bool
}

// SetHeadBindings enables head-tier steering: clientKey resolves a measured
// provider to its ckey, bindings resolves a ckey to its live head UID. Until
// this is called the head stays empty (v1 pools-only behavior). Both must be
// non-nil to take effect.
func (self *Steerer) SetHeadBindings(clientKey ClientKeyFunc, bindings HeadBindingReader) {
	self.clientKey = clientKey
	self.headBindings = bindings
	self.ckeyCache = map[connect.Id][32]byte{}
}

func NewSteerer(chain *ChainClient, stats *StatsEngine, hotkey *crv4.Keypair, cfg SteerConfig) *Steerer {
	if cfg.BlockTimeSecs == 0 {
		cfg.BlockTimeSecs = 12.0
	}
	if cfg.Theta == 0 {
		cfg.Theta = 0.3
	}
	if len(cfg.Rates.Tiers) == 0 {
		cfg.Rates = DefaultRateSchedule()
	}
	cfg.Rates = cfg.Rates.sortTiers() // Rate's last-match scan needs ascending tiers
	return &Steerer{
		chain:        chain,
		stats:        stats,
		aggregator:   GlobalMeanQuality{},
		hotkey:       hotkey,
		cfg:          cfg,
		headScoreEma: map[uint16]float64{},
		deposits:     depositLedger{conviction: DepositSums{}},
	}
}

// resolveHeadFleets maps the measured providers to their head UIDs via the
// on-chain binding and aggregates each fleet's routable egress-IP-hashes (§8.4,
// D27). The candidate set is every provider the validator measured — the quality
// keys (so a bound provider is dropped from the pools even with no egress this
// window — a client_id earns in exactly one tier) UNION the egress keys (so a
// routable provider below a_min still credits its fleet's IP breadth). Returns
// the per-fleet hash sets and the set of bound provider ids so the caller
// excludes them from the pool aggregation. Providers are visited in a
// deterministic order (stable logs + tests); one with no published ckey, an
// unbound ckey, or a bound-but-dead UID is skipped (fail closed), and a
// per-provider read error is logged and skipped — one bad provider never fails
// the whole steer.
func resolveHeadFleets(
	quality map[connect.Id]float64,
	egress map[connect.Id]map[[32]byte]bool,
	clientKey ClientKeyFunc,
	bindings HeadBindingReader,
) (fleets map[uint16]map[[32]byte]bool, bound map[connect.Id]bool) {
	fleets = map[uint16]map[[32]byte]bool{}
	bound = map[connect.Id]bool{}

	seen := map[connect.Id]bool{}
	ids := make([]connect.Id, 0, len(quality)+len(egress))
	for id := range quality {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for id := range egress {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a].LessThan(ids[b]) })

	for _, id := range ids {
		ckey, ok, err := clientKey(id)
		if err != nil {
			fmt.Printf("steer: head ckey lookup failed for provider %s (%v) — skipping\n", id, err)
			continue
		}
		if !ok {
			continue // no published ckey → cannot be a bound head
		}
		uid, ok, err := bindings.HeadUid(ckey)
		if err != nil {
			fmt.Printf("steer: head binding read failed for provider %s (%v) — skipping\n", id, err)
			continue
		}
		if !ok {
			continue // unbound or bound-but-dead UID → fail closed
		}
		// The provider is a member of fleet `uid`: in the head tier only
		// (excluded from the pools, §8.4). Union its routable egress-IP-hashes
		// into the fleet — a member with no egress this window still binds the
		// fleet, contributing no IPs.
		bound[id] = true
		set := fleets[uid]
		if set == nil {
			set = map[[32]byte]bool{}
			fleets[uid] = set
		}
		for h := range egress[id] {
			set[h] = true
		}
	}
	return fleets, bound
}

// headScores computes each fleet's split-adjusted routable-IP score (§8.4, D27):
// claim(h) = number of fleets whose IP set contains h; score(u) = Σ_{h ∈ IPs(u)}
// 1/claim(h). A hash shared by k fleets contributes 1/k to each, so overlapping
// exit-IP pools cannot be double-counted to inflate rank (two fleets sharing one
// hash each get 0.5). Pure — the split test pins it.
func headScores(fleets map[uint16]map[[32]byte]bool) map[uint16]float64 {
	claim := map[[32]byte]int{}
	for _, set := range fleets {
		for h := range set {
			claim[h]++
		}
	}
	scores := map[uint16]float64{}
	for uid, set := range fleets {
		var score float64
		for h := range set {
			score += 1.0 / float64(claim[h])
		}
		scores[uid] = score
	}
	return scores
}

// headScoreAlpha is the cross-tempo EMA weight for the head routable-IP score
// (§11.1 — the score is smoothed so a fleet's breadth doesn't thrash on one
// noisy tempo). Mirrors the stats quality EMA (α ≈ 0.1).
const headScoreAlpha = 0.1

// headScorePrune drops an EMA that has decayed to noise, so a fleet that stops
// presenting routable IPs eventually leaves the head vector entirely.
const headScorePrune = 1e-6

// foldHeadScores blends this tempo's raw split-adjusted scores into the per-UID
// EMA and returns the smoothed head inputs (§11.1, D27). A UID seen for the
// first time seeds at its raw score (no cold-start decay — the split test then
// reads exactly score(u)); a UID absent this tempo decays toward zero (a fleet
// whose routable breadth shrinks loses head weight); an EMA below headScorePrune
// is dropped. The EMA is committed every tempo, not only at epoch boundaries
// like the quality EMA, because the head weight is score itself, not a windowed
// liveness ratio ("EMA-smooth the score across tempos"). The one-tempo dip when
// the egress window resets at an epoch boundary is damped by α and recovered as
// the next tempo's trails repopulate the window.
func (self *Steerer) foldHeadScores(raw map[uint16]float64) []HeadWeightInput {
	if self.headScoreEma == nil {
		self.headScoreEma = map[uint16]float64{}
	}
	// Union of UIDs seen this tempo with the carried EMA keys, so a fleet that
	// vanished this tempo still decays (never keeps a stale weight forever).
	uids := map[uint16]bool{}
	for uid := range raw {
		uids[uid] = true
	}
	for uid := range self.headScoreEma {
		uids[uid] = true
	}
	head := make([]HeadWeightInput, 0, len(uids))
	for uid := range uids {
		r := raw[uid] // 0 when the fleet had no routable IPs this tempo
		if prev, ok := self.headScoreEma[uid]; ok {
			self.headScoreEma[uid] = headScoreAlpha*r + (1-headScoreAlpha)*prev
		} else {
			self.headScoreEma[uid] = r // seed on first sight
		}
		if self.headScoreEma[uid] < headScorePrune {
			delete(self.headScoreEma, uid)
			continue
		}
		head = append(head, HeadWeightInput{Uid: uid, Score: self.headScoreEma[uid]})
	}
	sort.Slice(head, func(a, b int) bool { return head[a].Uid < head[b].Uid })
	return head
}

// gatherHead resolves the head tier from the on-chain bindings and this tempo's
// routable-IP observations: it maps each measured provider to its fleet UID,
// unions the fleet's distinct egress-IP-hashes, splits any hash shared between
// fleets, and EMA-smooths the resulting per-fleet score across tempos (§8.4/
// §11.1, D27). ckeys are cached across tempos (immutable identity keys). Returns
// (nil, nil) when head steering is not configured — the pools then receive the
// whole weight, exactly as before head bindings existed.
func (self *Steerer) gatherHead(
	quality map[connect.Id]float64,
	egress map[connect.Id]map[[32]byte]bool,
) ([]HeadWeightInput, map[connect.Id]bool) {
	if self.clientKey == nil || self.headBindings == nil {
		return nil, nil
	}
	fleets, bound := resolveHeadFleets(quality, egress, self.cachedClientKey, self.headBindings)
	head := self.foldHeadScores(headScores(fleets))
	return head, bound
}

// cachedClientKey wraps the injected ClientKeyFunc with a positive cache:
// a provider's ckey is fetched at most once (the /key API is unauthenticated
// and the key is long-lived), so steady-state steering only re-reads the
// bindings, not the keys.
func (self *Steerer) cachedClientKey(id connect.Id) ([32]byte, bool, error) {
	if ckey, ok := self.ckeyCache[id]; ok {
		return ckey, true, nil
	}
	ckey, ok, err := self.clientKey(id)
	if err != nil || !ok {
		return ckey, ok, err
	}
	self.ckeyCache[id] = ckey
	return ckey, true, nil
}

// gatherDeposits reads, straight from the Deposited event log (§7.5, D25 — the
// contract stores no deposit ledger), the two per-NO deposit aggregates the pool
// weight needs: the OPEN EPOCH's deposits (the demand-signal numerator, a fresh
// epoch-windowed scan) and the ALL-TIME conviction (the tier input, an
// incrementally cached cumulant — only blocks past the last scan are read, so
// the genesis-to-tip walk happens once). Returns the epoch and conviction sums.
func (self *Steerer) gatherDeposits(epoch *big.Int) (epochDeposits DepositSums, conviction DepositSums, err error) {
	tip, err := self.chain.BlockNumber()
	if err != nil {
		return nil, nil, fmt.Errorf("block number: %w", err)
	}

	// All-time conviction (§7.2 — cumulative locked α across every epoch sets the
	// tier). Extend the cache to the tip; the first tempo scans from genesis.
	if self.deposits.conviction == nil {
		self.deposits.conviction = DepositSums{}
	}
	from := uint64(0)
	if self.deposits.started {
		from = self.deposits.scannedThrough + 1
	}
	if from <= tip {
		delta, err := self.chain.DepositedSums(from, tip, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("conviction scan [%d,%d]: %w", from, tip, err)
		}
		for noId, amount := range delta {
			if self.deposits.conviction[noId] == nil {
				self.deposits.conviction[noId] = new(big.Int)
			}
			self.deposits.conviction[noId].Add(self.deposits.conviction[noId], amount)
		}
	}
	self.deposits.scannedThrough = tip
	self.deposits.started = true

	// Open epoch's deposits (§8.1 the demand signal): a fresh scan bounded by the
	// epoch's block window and filtered to this epoch (topic1) — a bounded window,
	// not a cumulant, so it is re-read each tempo rather than cached.
	start := uint64(0)
	if s, err := self.chain.EpochStartBlock(); err != nil {
		fmt.Printf("steer: epochStartBlock read failed (%v) — scanning the open epoch from genesis (epoch topic filter keeps it exact)\n", err)
	} else {
		start = s
	}
	epochDeposits, err = self.chain.DepositedSums(start, tip, epoch)
	if err != nil {
		return nil, nil, fmt.Errorf("epoch deposit scan [%d,%d]: %w", start, tip, err)
	}
	return epochDeposits, self.deposits.conviction, nil
}

// gatherPools reads the operator registry from the contract, resolves each
// pool's UID (metagraph scan, stored-uid fallback), and computes each pool's
// implied usage from the Deposited event log + the tier→rate schedule (D25):
// implied_usage = epoch_deposit / rate(tier(conviction)). quality/exposure are
// the per-provider maps with any head-bound providers already excluded (they are
// weighted in the head tier, not the pools).
func (self *Steerer) gatherPools(quality map[connect.Id]float64, exposure map[connect.Id]uint64) ([]PoolWeightInput, error) {
	epoch, err := self.chain.Epoch()
	if err != nil {
		return nil, err
	}
	epochDeposits, conviction, err := self.gatherDeposits(epoch)
	if err != nil {
		return nil, err
	}
	count, err := self.chain.OperatorCount()
	if err != nil {
		return nil, err
	}

	var pools []PoolWeightInput
	n := count.Int64()
	for i := int64(0); i < n; i++ {
		noId, err := self.chain.OperatorIds(big.NewInt(i))
		if err != nil {
			return nil, err
		}
		operator, err := self.chain.Operators(noId)
		if err != nil {
			return nil, err
		}
		if !operator.Active {
			continue
		}

		uid := operator.MinerUid
		if resolvedUid, found, err := self.chain.FindUidByHotkey(self.cfg.Netuid, operator.MinerHotkey); err != nil {
			fmt.Printf("steer: metagraph uid scan failed for noId %s (%v) — using stored minerUid %d\n", noId, err, uid)
		} else if found {
			uid = resolvedUid
		} else {
			fmt.Printf("steer: minerHotkey of noId %s not in the metagraph — skipping pool\n", noId)
			continue
		}

		// implied_usage = epoch_deposit / rate(tier(conviction)) (§8.1, D25): a
		// NO on a lower-rate tier posts less α for the same implied usage, so the
		// stake is a discount, not a penalty; a NO with no deposit this epoch
		// carries no demand signal (weight 0 regardless of quality).
		impliedUsage := 0.0
		if epochDeposit, _ := new(big.Float).SetInt(epochDeposits.Get(noId)).Float64(); epochDeposit > 0 {
			impliedUsage = epochDeposit / self.cfg.Rates.Rate(conviction.Get(noId))
		}

		pools = append(pools, PoolWeightInput{
			NoId:         noId,
			Uid:          uid,
			ImpliedUsage: impliedUsage,
			Quality:      self.aggregator.PoolQuality(noId, quality, exposure),
		})
	}
	return pools, nil
}

// selfMask carries the self-mask fail-closed state across tempos (§11.3,
// HF-4). A metagraph read is *definitive* when it returns without error:
// found refreshes the cached UID, not-found clears it (the hotkey is
// provably unregistered, and masking a stale UID would wrongly zero
// whoever holds that UID now). A read *error* is not definitive: the last
// definitive answer is reused, and with none the resolve fails — the
// caller must skip the commit rather than submit a potentially
// self-weighted vector.
type selfMask struct {
	cached *uint16
	known  bool
}

// resolve folds one metagraph read result into the mask state and returns
// the UID to mask (nil = nothing to mask).
func (self *selfMask) resolve(uid uint16, found bool, readErr error) (*uint16, error) {
	if readErr != nil {
		if self.known {
			return self.cached, nil
		}
		return nil, fmt.Errorf("self-uid read failed with no cached answer: %w", readErr)
	}
	if found {
		u := uid
		self.cached = &u
	} else {
		self.cached = nil
	}
	self.known = true
	return self.cached, nil
}

// selfUid resolves the validator's own hotkey UID for the self-mask,
// failing CLOSED on metagraph flakiness (HF-4): an error with no prior
// definitive answer aborts the tempo instead of steering unmasked.
func (self *Steerer) selfUid() (*uint16, error) {
	if self.hotkey == nil {
		return nil, nil
	}
	uid, found, err := self.chain.FindUidByHotkey(self.cfg.Netuid, self.hotkey.PublicKey())
	return self.selfMask.resolve(uid, found, err)
}

// SubmitOnce runs one full steering iteration: gather → build → CRv4
// commit through the first answering substrate endpoint.
func (self *Steerer) SubmitOnce(ctx context.Context) error {
	// Fold the stats window into the cross-epoch EMA at epoch boundaries.
	if epoch, err := self.chain.Epoch(); err == nil {
		e := epoch.Uint64()
		if !self.epochSeen {
			self.epochSeen = true
			self.lastFoldedEpoch = e
		} else if e > self.lastFoldedEpoch {
			self.stats.Fold()
			self.lastFoldedEpoch = e
		}
	}

	// Head tier first: the measured providers that resolve to a live bound UID
	// form fleets steered on their split-adjusted routable-IP score (D27), and
	// are excluded from the pool aggregation (symmetric with the server dropping
	// them from the pool payoutRoot).
	quality := self.stats.Quality()
	exposure := self.stats.Exposure()
	egress := self.stats.EgressIpHashes()
	head, bound := self.gatherHead(quality, egress)
	for id := range bound {
		delete(quality, id)
		delete(exposure, id)
	}
	if len(head) > 0 {
		fmt.Printf("steer: %d head fleet uids (split-adjusted routable-IP score), %d bound providers excluded from pools\n", len(head), len(bound))
	}

	pools, err := self.gatherPools(quality, exposure)
	if err != nil {
		return fmt.Errorf("gather pools: %w", err)
	}
	selfUid, err := self.selfUid()
	if err != nil {
		// HF-4: fail closed — better to miss a tempo (CRv4 keeps the last
		// committed vector active) than to steer without the self-mask.
		return fmt.Errorf("self-mask (skipping this tempo's commit): %w", err)
	}
	uids, scores, err := BuildWeightVector(pools, head, self.cfg.Theta, selfUid)
	if err != nil {
		return fmt.Errorf("build weights: %w", err)
	}

	var errs []error
	for _, wsUrl := range self.cfg.SubstrateUrls {
		substrate, err := crv4.DialChain(wsUrl)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", wsUrl, err))
			continue
		}
		result, err := crv4.SubmitWeightsCRv4(ctx, substrate, self.hotkey, self.cfg.Netuid, uids, scores, crv4.SubmitOptions{
			VersionKey:    self.cfg.VersionKey,
			BlockTimeSecs: self.cfg.BlockTimeSecs,
		})
		substrate.API.Client.Close() // fresh dial per tempo; never leak the ws
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", wsUrl, err))
			continue
		}
		fmt.Printf("steer: committed weights for %d uids (tx %s, reveal round %d, reveal block %d)\n",
			len(result.Uids), result.TxHash.Hex(), result.RevealRound, result.RevealBlock)
		return nil
	}
	return fmt.Errorf("crv4 commit failed on every substrate endpoint: %w", errors.Join(errs...))
}

// tempoBlocks resolves the steering cadence: the flag, else the on-chain
// subnet tempo (+1 matches the chain's tempo+1 step interval).
func (self *Steerer) tempoBlocks() uint64 {
	if self.cfg.TempoBlocks > 0 {
		return self.cfg.TempoBlocks
	}
	for _, wsUrl := range self.cfg.SubstrateUrls {
		substrate, err := crv4.DialChain(wsUrl)
		if err != nil {
			continue
		}
		tempo, err := substrate.Tempo(self.cfg.Netuid)
		substrate.API.Client.Close()
		if err == nil && tempo > 0 {
			return uint64(tempo) + 1
		}
	}
	return 360 // ~72 min at 12s blocks
}

// Run submits weights every tempo until ctx is done. Failures are logged
// and retried at the next tick (a missed tempo is recoverable; CRv4 keeps
// the last committed vector active).
func (self *Steerer) Run(ctx context.Context) {
	interval := time.Duration(float64(self.tempoBlocks()) * self.cfg.BlockTimeSecs * float64(time.Second))
	if interval <= 0 {
		interval = 72 * time.Minute
	}
	fmt.Printf("steer: submitting weights every %s (netuid %d, theta %.2f)\n", interval, self.cfg.Netuid, self.cfg.Theta)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := self.SubmitOnce(ctx); err != nil {
			fmt.Printf("steer: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
