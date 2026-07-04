package validator

// Weight-pipeline tests (v0.4, D25+D27): the TAIL is implied_usage × quality
// and the HEAD is each fleet's split-adjusted routable-IP score → θ split →
// normalize → u16 vector via the crv4 helpers (the same code path
// SubmitWeightsCRv4 applies).

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/urnetwork/connect"

	"github.com/urnetwork/sn/crv4"
)

// stubHeadReader is an injected HeadBindingReader for the head-tier tests: a
// ckey present in live resolves to a live UID; absent ckeys are unbound OR
// bound-but-dead (both fail closed to ok=false, exactly as chainHeadBindings
// collapses them); a ckey in err returns a read error.
type stubHeadReader struct {
	live map[[32]byte]uint16
	err  map[[32]byte]error
}

func (self stubHeadReader) HeadUid(ckey [32]byte) (uint16, bool, error) {
	if e, ok := self.err[ckey]; ok {
		return 0, false, e
	}
	uid, ok := self.live[ckey]
	return uid, ok, nil
}

// ckey builds a distinct 32-byte client key from a tag byte.
func ckey(tag byte) [32]byte {
	var c [32]byte
	c[0] = tag
	return c
}

// iphash builds a distinct (non-zero) 32-byte egress-IP-hash from a tag byte.
func iphash(tag byte) [32]byte {
	var h [32]byte
	h[0] = tag
	h[31] = 0x01 // never the zero hash (which RecordEgressHash ignores)
	return h
}

func TestBuildWeightVectorPoolsOnly(t *testing.T) {
	pools := []PoolWeightInput{
		{NoId: big.NewInt(1), Uid: 10, ImpliedUsage: 1_000_000, Quality: 0.8},
		{NoId: big.NewInt(2), Uid: 11, ImpliedUsage: 3_000_000, Quality: 0.4},
		{NoId: big.NewInt(3), Uid: 12, ImpliedUsage: 0, Quality: 0.9}, // no usage → no weight
	}
	// Head empty (no bound fleets): the pools receive the FULL weight — θ is not
	// stranded on an empty tier.
	uids, scores, err := BuildWeightVector(pools, nil, 0.3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(uids) != 2 || uids[0] != 10 || uids[1] != 11 {
		t.Fatalf("uids %v", uids)
	}
	// implied_usage×Q: 0.8e6 vs 1.2e6 → 0.4 vs 0.6 of the whole.
	if math.Abs(scores[0]-0.4) > 1e-12 || math.Abs(scores[1]-0.6) > 1e-12 {
		t.Fatalf("scores %v, want [0.4 0.6]", scores)
	}
	sum := scores[0] + scores[1]
	if math.Abs(sum-1.0) > 1e-12 {
		t.Fatalf("Σ scores = %f", sum)
	}
}

func TestBuildWeightVectorThetaSplit(t *testing.T) {
	pools := []PoolWeightInput{
		{NoId: big.NewInt(1), Uid: 10, ImpliedUsage: 100, Quality: 1.0},
		{NoId: big.NewInt(2), Uid: 11, ImpliedUsage: 300, Quality: 1.0},
	}
	head := []HeadWeightInput{
		{Uid: 20, Score: 0.5},
		{Uid: 21, Score: 1.5},
	}
	uids, scores, err := BuildWeightVector(pools, head, 0.3, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint16]float64{}
	for i, uid := range uids {
		got[uid] = scores[i]
	}
	// Pools share 0.7 in ratio 1:3; head shares 0.3 in ratio 1:3.
	want := map[uint16]float64{
		10: 0.7 * 0.25,
		11: 0.7 * 0.75,
		20: 0.3 * 0.25,
		21: 0.3 * 0.75,
	}
	for uid, w := range want {
		if math.Abs(got[uid]-w) > 1e-12 {
			t.Fatalf("uid %d: %f, want %f", uid, got[uid], w)
		}
	}
}

func TestBuildWeightVectorSelfMask(t *testing.T) {
	selfUid := uint16(11)
	pools := []PoolWeightInput{
		{NoId: big.NewInt(1), Uid: 10, ImpliedUsage: 100, Quality: 1.0},
		{NoId: big.NewInt(2), Uid: 11, ImpliedUsage: 300, Quality: 1.0},
	}
	uids, _, err := BuildWeightVector(pools, nil, 0.3, &selfUid)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range uids {
		if uid == selfUid {
			t.Fatal("self uid not masked")
		}
	}
}

// TestSelfMaskFailsClosed (HF-4): the self-mask never silently degrades to
// "no mask" on metagraph flakiness — an error reuses the last definitive
// answer, and with none the resolve errors (the tempo's commit is skipped).
// A definitive not-found clears the mask (a stale UID would wrongly zero
// whoever holds it now).
func TestSelfMaskFailsClosed(t *testing.T) {
	m := &selfMask{}

	// read error before any definitive answer → error (skip the commit)
	if _, err := m.resolve(0, false, errors.New("metagraph down")); err == nil {
		t.Fatal("read error with no cached answer must fail closed")
	}

	// definitive found → masked and cached
	uid, err := m.resolve(11, true, nil)
	if err != nil || uid == nil || *uid != 11 {
		t.Fatalf("resolve(found 11) = %v, %v", uid, err)
	}

	// read error after a definitive answer → the cached UID, no error
	uid, err = m.resolve(0, false, errors.New("metagraph down"))
	if err != nil || uid == nil || *uid != 11 {
		t.Fatalf("resolve(error, cached 11) = %v, %v", uid, err)
	}

	// definitive not-found → mask cleared (provably unregistered)
	uid, err = m.resolve(0, false, nil)
	if err != nil || uid != nil {
		t.Fatalf("resolve(not found) = %v, %v, want nil mask", uid, err)
	}

	// read error after the definitive not-found → still nil, still no error
	uid, err = m.resolve(0, false, errors.New("metagraph down"))
	if err != nil || uid != nil {
		t.Fatalf("resolve(error, cached nil) = %v, %v, want nil mask", uid, err)
	}
}

func TestBuildWeightVectorEmpty(t *testing.T) {
	if _, _, err := BuildWeightVector(nil, nil, 0.3, nil); err == nil {
		t.Fatal("empty inputs must error")
	}
	pools := []PoolWeightInput{{NoId: big.NewInt(1), Uid: 1, ImpliedUsage: 1, Quality: 0}}
	if _, _, err := BuildWeightVector(pools, nil, 0.3, nil); err == nil {
		t.Fatal("all-zero weights must error")
	}
}

// TestWeightPipelineToU16 runs the vector through the crv4 cap + u16
// normalization exactly as SubmitWeightsCRv4 does (reusing the helpers, not
// re-testing their internals): max → 65535, cap applied.
func TestWeightPipelineToU16(t *testing.T) {
	pools := []PoolWeightInput{
		{NoId: big.NewInt(1), Uid: 3, ImpliedUsage: 500, Quality: 0.9},
		{NoId: big.NewInt(2), Uid: 7, ImpliedUsage: 100, Quality: 0.9},
		{NoId: big.NewInt(3), Uid: 9, ImpliedUsage: 400, Quality: 0.45},
	}
	uids, scores, err := BuildWeightVector(pools, nil, 0.3, nil)
	if err != nil {
		t.Fatal(err)
	}
	capped, err := crv4.ApplyMaxWeightLimit(scores, crv4.U16Max)
	if err != nil {
		t.Fatal(err)
	}
	u16uids, u16vals, err := crv4.NormalizeToU16(uids, capped)
	if err != nil {
		t.Fatal(err)
	}
	if len(u16uids) != 3 {
		t.Fatalf("u16 uids %v", u16uids)
	}
	// Largest score (uid 3: 500×0.9=450) maps to 65535; uid 9
	// (400×0.45=180) maps to round(65535×180/450) = 26214;
	// uid 7 (100×0.9=90) → round(65535×90/450) = 13107.
	byUid := map[uint16]uint16{}
	for i, uid := range u16uids {
		byUid[uid] = u16vals[i]
	}
	if byUid[3] != 65535 {
		t.Fatalf("max uid weight %d, want 65535", byUid[3])
	}
	if byUid[9] != 26214 {
		t.Fatalf("uid 9 weight %d, want 26214", byUid[9])
	}
	if byUid[7] != 13107 {
		t.Fatalf("uid 7 weight %d, want 13107", byUid[7])
	}

	// With a max weight limit, the cap redistributes before u16 mapping —
	// assert the capped max ratio holds after normalization.
	cappedLimited, err := crv4.ApplyMaxWeightLimit(scores, 32768) // ≈ half
	if err != nil {
		t.Fatal(err)
	}
	_, limitedVals, err := crv4.NormalizeToU16(uids, cappedLimited)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	var max float64
	for _, v := range limitedVals {
		sum += float64(v)
		if float64(v) > max {
			max = float64(v)
		}
	}
	ratio := max / sum
	wantMaxRatio := 32768.0 / 65535.0
	if ratio > wantMaxRatio+1e-3 {
		t.Fatalf("max weight ratio %f exceeds the cap %f", ratio, wantMaxRatio)
	}
}

func TestGlobalMeanQualityAggregator(t *testing.T) {
	a := connect.NewId()
	b := connect.NewId()
	quality := map[connect.Id]float64{a: 1.0, b: 0.5}
	exposure := map[connect.Id]uint64{a: 30, b: 10}
	got := GlobalMeanQuality{}.PoolQuality(big.NewInt(1), quality, exposure)
	want := (1.0*30 + 0.5*10) / 40
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("Q_n = %f, want %f (exposure-weighted mean)", got, want)
	}
	// EMA-only providers (no window exposure) still count with weight 1.
	c := connect.NewId()
	quality[c] = 0.2
	got = GlobalMeanQuality{}.PoolQuality(big.NewInt(1), quality, exposure)
	want = (1.0*30 + 0.5*10 + 0.2*1) / 41
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("Q_n = %f, want %f", got, want)
	}
	if (GlobalMeanQuality{}).PoolQuality(big.NewInt(1), nil, nil) != 0 {
		t.Fatal("empty quality must aggregate to 0")
	}
}

// TestRateScheduleTiers pins the tier→rate lookup (§7.3): the highest tier whose
// MinConviction ≤ conviction wins, the zero tier is the baseline, rates are
// floored above zero, and implied_usage = deposit / rate makes a lower-rate
// (higher-conviction) NO earn the same weight from less α — the discount (D25).
func TestRateScheduleTiers(t *testing.T) {
	s := DefaultRateSchedule().sortTiers()
	alpha := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(alphaRao)) }
	cases := []struct {
		conv *big.Int
		want float64
	}{
		{big.NewInt(0), 1.0},     // zero tier = baseline
		{alpha(500), 1.0},        // below tier 1 → baseline
		{alpha(1_000), 0.8},      // exactly tier 1
		{alpha(50_000), 0.6},     // tier 2
		{alpha(200_000), 0.4},    // tier 3
		{alpha(5_000_000), 0.25}, // top tier
	}
	for _, tc := range cases {
		if got := s.Rate(tc.conv); math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("Rate(%s) = %f, want %f", tc.conv, got, tc.want)
		}
	}

	// A (mis)configured zero-rate tier is floored, so implied usage stays finite.
	z := RateSchedule{Tiers: []RateTier{{MinConviction: big.NewInt(0), Rate: 0}}}
	if got := z.Rate(big.NewInt(0)); math.Abs(got-rateFloor) > 1e-18 {
		t.Fatalf("zero-rate tier floored to %g, want %g", got, rateFloor)
	}

	// implied_usage = deposit / rate: 1000α at the baseline rate (1.0) implies the
	// same usage as 250α at the top tier (0.25) — the discount, not a penalty.
	baseUsage := 1000.0 / s.Rate(big.NewInt(0))
	topUsage := 250.0 / s.Rate(alpha(5_000_000))
	if math.Abs(baseUsage-topUsage) > 1e-9 {
		t.Fatalf("implied usage baseline %f vs top tier %f — should match (discount)", baseUsage, topUsage)
	}
}

// TestHeadScoresSplit pins the D27 split: claim(h) = #fleets sharing hash h, and
// score(u) = Σ_{h ∈ IPs(u)} 1/claim(h). Two fleets sharing ONE hash each get
// exactly 0.5; a unique hash is worth the full 1.0.
func TestHeadScoresSplit(t *testing.T) {
	hA, hB, hC := iphash(1), iphash(2), iphash(3)

	// Fleets 20 and 21 both route hA (shared → 0.5 each); 20 also routes hB and
	// 21 also routes hC (unique → +1.0 each).
	fleets := map[uint16]map[[32]byte]bool{
		20: {hA: true, hB: true},
		21: {hA: true, hC: true},
	}
	scores := headScores(fleets)
	if math.Abs(scores[20]-1.5) > 1e-12 || math.Abs(scores[21]-1.5) > 1e-12 {
		t.Fatalf("scores %v, want {20:1.5, 21:1.5}", scores)
	}

	// Two fleets sharing exactly one hash: 0.5 each (the headline split example).
	shared := headScores(map[uint16]map[[32]byte]bool{30: {hA: true}, 31: {hA: true}})
	if math.Abs(shared[30]-0.5) > 1e-12 || math.Abs(shared[31]-0.5) > 1e-12 {
		t.Fatalf("shared-hash scores %v, want {30:0.5, 31:0.5}", shared)
	}

	// A hash claimed by a single fleet is worth the full 1.0.
	solo := headScores(map[uint16]map[[32]byte]bool{40: {hA: true}})
	if math.Abs(solo[40]-1.0) > 1e-12 {
		t.Fatalf("solo score %f, want 1.0", solo[40])
	}
}

// TestResolveHeadFleets: measured providers map to fleets via the binding, each
// fleet's routable egress-IP-hashes union across its bound client_ids, and
// dead/unbound/keyless providers fail closed (never attributed).
func TestResolveHeadFleets(t *testing.T) {
	pA := connect.NewId()       // ckA → uid 5, egress {h1,h2}
	pB := connect.NewId()       // ckB → uid 5 (same fleet), egress {h2,h3}
	pC := connect.NewId()       // ckC → uid 7, egress {h1} (shares h1 with fleet 5)
	pDead := connect.NewId()    // ck known but not a live uid (reader ok=false)
	pUnbound := connect.NewId() // ck known but unbound (reader ok=false)
	pNoKey := connect.NewId()   // no published ckey (resolver ok=false)

	h1, h2, h3 := iphash(1), iphash(2), iphash(3)
	egress := map[connect.Id]map[[32]byte]bool{
		pA:    {h1: true, h2: true},
		pB:    {h2: true, h3: true},
		pC:    {h1: true},
		pDead: {iphash(9): true}, // egress present but not bound → must NOT count
	}
	// quality carries pUnbound/pNoKey (no egress) to prove the candidate set is
	// quality ∪ egress and keyless/unbound providers are skipped for pools too.
	quality := map[connect.Id]float64{pA: 0.5, pC: 0.5, pUnbound: 0.5, pNoKey: 0.5, pDead: 0.5}

	ckA, ckB, ckC, ckDead, ckUnbound := ckey(1), ckey(2), ckey(3), ckey(4), ckey(5)
	clientKey := func(id connect.Id) ([32]byte, bool, error) {
		switch id {
		case pA:
			return ckA, true, nil
		case pB:
			return ckB, true, nil
		case pC:
			return ckC, true, nil
		case pDead:
			return ckDead, true, nil
		case pUnbound:
			return ckUnbound, true, nil
		default:
			return [32]byte{}, false, nil // pNoKey
		}
	}
	// Only ckA/ckB/ckC resolve; ckDead/ckUnbound absent → ok=false.
	reader := stubHeadReader{live: map[[32]byte]uint16{ckA: 5, ckB: 5, ckC: 7}}

	fleets, bound := resolveHeadFleets(quality, egress, clientKey, reader)

	// Fleet 5 = union of pA {h1,h2} and pB {h2,h3} = {h1,h2,h3}; fleet 7 = {h1}.
	if len(fleets) != 2 {
		t.Fatalf("fleets %v, want exactly uid 5 and uid 7 (pDead's hash excluded)", fleets)
	}
	if len(fleets[5]) != 3 || !fleets[5][h1] || !fleets[5][h2] || !fleets[5][h3] {
		t.Fatalf("fleet 5 hashes %v, want {h1,h2,h3} (union across bound client_ids)", fleets[5])
	}
	if len(fleets[7]) != 1 || !fleets[7][h1] {
		t.Fatalf("fleet 7 hashes %v, want {h1}", fleets[7])
	}
	if !bound[pA] || !bound[pB] || !bound[pC] {
		t.Fatalf("bound %v, want pA,pB,pC", bound)
	}
	for _, id := range []connect.Id{pDead, pUnbound, pNoKey} {
		if bound[id] {
			t.Fatalf("provider %s must not be head-bound (fail closed)", id)
		}
	}

	// The shared h1 (fleets 5 and 7) splits 0.5 each: fleet 5 = 0.5+1+1 = 2.5,
	// fleet 7 = 0.5.
	scores := headScores(fleets)
	if math.Abs(scores[5]-2.5) > 1e-12 || math.Abs(scores[7]-0.5) > 1e-12 {
		t.Fatalf("scores %v, want {5:2.5, 7:0.5} (h1 split across fleets)", scores)
	}
}

// TestResolveHeadFleetsReadErrorSkipsProvider: a per-provider read error skips
// that provider without failing the whole steer.
func TestResolveHeadFleetsReadErrorSkipsProvider(t *testing.T) {
	good := connect.NewId()
	bad := connect.NewId()
	h := iphash(1)
	egress := map[connect.Id]map[[32]byte]bool{good: {h: true}, bad: {iphash(2): true}}
	quality := map[connect.Id]float64{good: 0.8, bad: 0.9}
	ckGood, ckBad := ckey(1), ckey(2)
	clientKey := func(id connect.Id) ([32]byte, bool, error) {
		if id == good {
			return ckGood, true, nil
		}
		return ckBad, true, nil
	}
	reader := stubHeadReader{
		live: map[[32]byte]uint16{ckGood: 3},
		err:  map[[32]byte]error{ckBad: errors.New("rpc down")},
	}
	fleets, bound := resolveHeadFleets(quality, egress, clientKey, reader)
	if len(fleets) != 1 || len(fleets[3]) != 1 || !fleets[3][h] {
		t.Fatalf("fleets %v, want only uid 3 with {h}", fleets)
	}
	if len(bound) != 1 || !bound[good] {
		t.Fatalf("bound %v, want only the good provider", bound)
	}
}

// TestHeadTierScoreAndPoolExclusion runs the full head path: resolveHeadFleets →
// headScores → exclude bound providers from the pool aggregation (as SubmitOnce
// does) → BuildWeightVector. Asserts the head is weighted on the routable-IP
// score, the θ split is correct, and the bound fleets do not leak into pool Q_n.
func TestHeadTierScoreAndPoolExclusion(t *testing.T) {
	hp1 := connect.NewId() // fleet uid 20, egress {h1}          → score 1.0
	hp2 := connect.NewId() // fleet uid 21, egress {h2,h3}       → score 2.0
	pp := connect.NewId()  // stays a pool provider, Q_p 0.9
	h1, h2, h3 := iphash(1), iphash(2), iphash(3)
	quality := map[connect.Id]float64{hp1: 0.5, hp2: 1.5, pp: 0.9}
	exposure := map[connect.Id]uint64{hp1: 100, hp2: 100, pp: 50}
	egress := map[connect.Id]map[[32]byte]bool{hp1: {h1: true}, hp2: {h2: true, h3: true}}

	ck1, ck2 := ckey(1), ckey(2)
	clientKey := func(id connect.Id) ([32]byte, bool, error) {
		switch id {
		case hp1:
			return ck1, true, nil
		case hp2:
			return ck2, true, nil
		default:
			return [32]byte{}, false, nil // pp has no head binding
		}
	}
	reader := stubHeadReader{live: map[[32]byte]uint16{ck1: 20, ck2: 21}}

	fleets, bound := resolveHeadFleets(quality, egress, clientKey, reader)
	for id := range bound { // SubmitOnce's exclusion step
		delete(quality, id)
		delete(exposure, id)
	}

	qn := GlobalMeanQuality{}.PoolQuality(big.NewInt(1), quality, exposure)
	if math.Abs(qn-0.9) > 1e-12 {
		t.Fatalf("pool Q_n = %f, want 0.9 (head fleets excluded from aggregation)", qn)
	}

	scores := headScores(fleets)
	head := []HeadWeightInput{{Uid: 20, Score: scores[20]}, {Uid: 21, Score: scores[21]}}
	pools := []PoolWeightInput{{NoId: big.NewInt(1), Uid: 10, ImpliedUsage: 1000, Quality: qn}}
	uids, weights, err := BuildWeightVector(pools, head, 0.3, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint16]float64{}
	for i, u := range uids {
		got[u] = weights[i]
	}
	// head shares θ=0.3 in ratio 1.0:2.0 (=1:2); the single pool takes 1−θ.
	want := map[uint16]float64{20: 0.3 * (1.0 / 3.0), 21: 0.3 * (2.0 / 3.0), 10: 0.7}
	for u, w := range want {
		if math.Abs(got[u]-w) > 1e-12 {
			t.Fatalf("uid %d = %f, want %f", u, got[u], w)
		}
	}
}

// TestHeadEmptyCedesToPools: with no live bindings the head is empty and the
// pools receive the whole weight — θ is never stranded.
func TestHeadEmptyCedesToPools(t *testing.T) {
	p := connect.NewId()
	quality := map[connect.Id]float64{p: 0.8}
	egress := map[connect.Id]map[[32]byte]bool{p: {iphash(1): true}}
	clientKey := func(connect.Id) ([32]byte, bool, error) { return [32]byte{}, false, nil }
	fleets, bound := resolveHeadFleets(quality, egress, clientKey, stubHeadReader{})
	if len(fleets) != 0 || len(bound) != 0 {
		t.Fatalf("expected empty head, got fleets=%v bound=%v", fleets, bound)
	}
	pools := []PoolWeightInput{
		{NoId: big.NewInt(1), Uid: 10, ImpliedUsage: 100, Quality: 1.0},
		{NoId: big.NewInt(2), Uid: 11, ImpliedUsage: 300, Quality: 1.0},
	}
	uids, scores, err := BuildWeightVector(pools, nil, 0.3, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint16]float64{}
	for i, u := range uids {
		got[u] = scores[i]
	}
	if math.Abs(got[10]-0.25) > 1e-12 || math.Abs(got[11]-0.75) > 1e-12 {
		t.Fatalf("pools-only weights %v, want {10:0.25, 11:0.75} (θ ceded)", got)
	}
}

// TestFoldHeadScoresEma: the per-fleet score EMA seeds at the raw score on first
// sight, decays a fleet that stops presenting routable IPs, and prunes a decayed
// EMA out of the head vector entirely (§11.1, D27).
func TestFoldHeadScoresEma(t *testing.T) {
	s := &Steerer{}

	// First sight seeds at the raw score (no cold-start decay).
	head := s.foldHeadScores(map[uint16]float64{20: 1.0, 21: 2.0})
	got := map[uint16]float64{}
	for _, h := range head {
		got[h.Uid] = h.Score
	}
	if math.Abs(got[20]-1.0) > 1e-12 || math.Abs(got[21]-2.0) > 1e-12 {
		t.Fatalf("seeded head %v, want {20:1.0, 21:2.0}", got)
	}

	// A fleet absent this tempo decays: ema ← α·0 + (1−α)·prev; a fleet holding
	// its raw score is unchanged.
	head = s.foldHeadScores(map[uint16]float64{20: 1.0}) // 21 absent
	got = map[uint16]float64{}
	for _, h := range head {
		got[h.Uid] = h.Score
	}
	if math.Abs(got[20]-1.0) > 1e-12 {
		t.Fatalf("held fleet 20 = %f, want 1.0", got[20])
	}
	want21 := (1 - headScoreAlpha) * 2.0
	if math.Abs(got[21]-want21) > 1e-12 {
		t.Fatalf("decayed fleet 21 = %f, want %f", got[21], want21)
	}

	// An EMA decayed below the prune floor leaves the head vector and the state.
	s.headScoreEma[21] = headScorePrune / 2
	head = s.foldHeadScores(map[uint16]float64{20: 1.0})
	for _, h := range head {
		if h.Uid == 21 {
			t.Fatalf("pruned fleet 21 still present: %v", head)
		}
	}
	if _, ok := s.headScoreEma[21]; ok {
		t.Fatal("pruned fleet 21 not deleted from the EMA state")
	}
}

// TestGatherHeadDisabled: until SetHeadBindings is called the head is empty
// (the pre-head-tier behavior is preserved).
func TestGatherHeadDisabled(t *testing.T) {
	s := &Steerer{}
	head, bound := s.gatherHead(
		map[connect.Id]float64{connect.NewId(): 0.9},
		map[connect.Id]map[[32]byte]bool{},
	)
	if head != nil || bound != nil {
		t.Fatalf("head must stay empty until SetHeadBindings: head=%v bound=%v", head, bound)
	}
}

// TestCachedClientKeyCachesPositive: a resolved ckey is fetched once and
// reused across tempos.
func TestCachedClientKeyCachesPositive(t *testing.T) {
	id := connect.NewId()
	ck := ckey(7)
	calls := 0
	s := &Steerer{}
	s.SetHeadBindings(
		func(connect.Id) ([32]byte, bool, error) { calls++; return ck, true, nil },
		stubHeadReader{},
	)
	for i := 0; i < 3; i++ {
		got, ok, err := s.cachedClientKey(id)
		if err != nil || !ok || got != ck {
			t.Fatalf("cachedClientKey = %x %v %v", got, ok, err)
		}
	}
	if calls != 1 {
		t.Fatalf("underlying resolver called %d times, want 1 (cached)", calls)
	}
}
