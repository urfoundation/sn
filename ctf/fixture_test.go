package ctf

import (
	"context"
	"encoding/binary"
	"errors"
	"maps"
	"math/big"
	"slices"
	"testing"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ledger"
)

const (
	testNoID    = 1
	testChainID = 964
	testNetuid  = 25
	testFirst   = 8
	// testTime is a census commitment time; testRound is its draw round.
	testTime = 1_796_408_000
)

var testRound = DrawRound(testTime)

// id returns client id n; ids order as their n.
func id(n int) [16]byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[8:], uint64(n))
	return b
}

// key returns coldkey n; coldkeys order as their n.
func key(n int) [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[:8], uint64(n))
	b[31] = 0x5a
	return b
}

// unitHash returns unit n, as a keyed prefix hash would be: nonzero.
func unitHash(n int) [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[24:], uint64(n))
	b[0] = 0x0b
	return b
}

// carryKey is the carry contract's coldkey in the tests.
var carryKey = func() (k [32]byte) {
	for i := range k {
		k[i] = 0xca
	}
	return k
}()

func cc(code string) Country {
	c, ok := ParseCountry(code)
	if !ok {
		panic("not in L: " + code)
	}
	return c
}

func rao(v uint64) uint256.Int { return *uint256.NewInt(v) }

func raoDec(s string) uint256.Int {
	v, err := uint256.FromDecimal(s)
	if err != nil {
		panic(err)
	}
	return *v
}

// provider and extender return a candidate of client n in unit u.
func provider(n int, country string, u int) Candidate {
	return Candidate{Role: RoleProvider, Country: cc(country), Unit: unitHash(u), Family: FamilyIPv4, ClientID: id(n)}
}

func extender(n int, country string, u int) Candidate {
	return Candidate{Role: RoleExtender, Country: cc(country), Unit: unitHash(u), Family: FamilyIPv4, ClientID: id(n)}
}

// The scenario tests sign pulses with a test key, swapped in for quicknet's.
var (
	testSecret    = new(big.Int).SetUint64(0x5eed_c7f0_0d)
	testPublicKey = func() bls12381.G2Affine {
		_, _, _, g2 := bls12381.Generators()
		var pk bls12381.G2Affine
		pk.ScalarMultiplication(&g2, testSecret)
		return pk
	}()
)

// withTestKey makes the test key quicknet's for the test.
func withTestKey(t testing.TB) {
	t.Helper()
	saved := quicknetPublicKey
	quicknetPublicKey = testPublicKey
	t.Cleanup(func() { quicknetPublicKey = saved })
}

// testPulse signs round with the test key.
func testPulse(round uint64) *Pulse {
	h, err := bls12381.HashToG1(crv4.RoundIdentity(round), []byte(crv4.QuicknetDST))
	if err != nil {
		panic(err)
	}
	var s bls12381.G1Affine
	s.ScalarMultiplication(&h, testSecret)
	b := s.Bytes()
	return &Pulse{Round: round, Signature: b[:]}
}

// funded returns fundedThrough snapshots from epoch, value pairs.
func funded(pairs ...uint64) map[uint64]uint256.Int {
	out := map[uint64]uint256.Int{}
	for i := 0; i < len(pairs); i += 2 {
		out[pairs[i]] = rao(pairs[i+1])
	}
	return out
}

// heads is a HeadOracle: per epoch, candidates mapped to an active binding.
type heads map[uint64]map[[16]byte]bool

func (h heads) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	return h[epoch], nil
}

// committed is an encoded census and its chain record.
type committed struct {
	objects *CensusObjects
	time    uint64
	block   uint64
}

func commit(t testing.TB, c *Census, time uint64) *committed {
	t.Helper()
	objects, err := EncodeCensus(c)
	if err != nil {
		t.Fatal(err)
	}
	return &committed{objects: objects, time: time, block: 9_600_000 + c.Epoch}
}

func (c *committed) input() *CensusInput {
	return &CensusInput{Hash: c.objects.Hash, Block: c.block, Time: c.time, Objects: maps.Clone(c.objects.Objects)}
}

func (c *committed) facts() *CensusCommitment {
	return &CensusCommitment{Hash: c.objects.Hash, Block: c.block, Time: c.time}
}

// coldkeysOf maps every candidate to coldkey(client).
func coldkeysOf(coldkey func(n uint64) [32]byte, censuses ...*Census) map[[16]byte][32]byte {
	out := map[[16]byte][32]byte{}
	for _, c := range censuses {
		for _, r := range c.Candidates {
			out[r.ClientID] = coldkey(binary.BigEndian.Uint64(r.ClientID[8:]))
		}
	}
	return out
}

// sharedColdkeys gives clients 1 and 2 one coldkey, and every other client
// its own.
func sharedColdkeys(n uint64) [32]byte {
	if n == 2 {
		return key(1)
	}
	return key(int(n))
}

// epochCensus is the scenario's census for epoch k: providers and extenders
// in DE, IS, TM and US, with a unit of two countries (TM, IS), a client in
// both roles, and k shifting membership a little between epochs.
func epochCensus(k uint64) *Census {
	c := &Census{NoID: testNoID, Epoch: k, ExcusedSamples: []uint16{3, 4}, Excluded: []Exclusion{
		{Country: cc("de"), Role: RoleProvider, Reason: ReasonPresence, Count: 3},
		{Country: CountryUnknown, Role: RoleExtender, Reason: ReasonNoLocation, Count: 1},
	}}
	c.Candidates = []Candidate{
		provider(1, "de", 1), provider(2, "de", 1), provider(3, "de", 2), provider(4, "de", 3),
		provider(5, "de", 4), provider(6, "de", 5), provider(7, "de", 6), provider(8, "de", 7),
		provider(10, "is", 10),
		provider(11, "tm", 11), provider(12, "is", 11),
		extender(1, "de", 1), extender(20, "de", 20),
		extender(21, "us", 21), extender(22, "us", 22), extender(23, "us", 23), extender(24, "us", 24), extender(25, "us", 25),
	}
	if k%2 == 1 {
		c.Candidates = append(c.Candidates, provider(30+int(k), "de", 30+int(k)), extender(31+int(k), "kp", 31+int(k)))
	}
	return c
}

// scenario is a CTF pool's history: the first root F covers epochs 8 and 9,
// the first drawn and the second without a census; the validator audited F
// and it is finalized. The pending root P covers 10, where the carry is late
// and pot shares wait as pending awards, and 11, where it is back and they
// are paid.
type scenario struct {
	f, p     *Ledger
	fIn, pIn BuildInput
	census   map[uint64]*committed
	custody  *ledger.MemoryCustody
	heads    heads
	funded   map[uint64]uint256.Int
	recycled map[uint64]uint256.Int
}

const (
	scenarioFresh8  = 2490*1_000_000 + 7
	scenarioFresh9  = 2490 * 400_000
	scenarioFresh10 = 2490*600_000 + 2489
	scenarioFresh11 = 2490 * 700_000
)

func newScenario(t *testing.T) *scenario {
	t.Helper()
	withTestKey(t)
	ctx := context.Background()
	s := &scenario{custody: ledger.NewMemoryCustody(), heads: heads{}, census: map[uint64]*committed{}, recycled: map[uint64]uint256.Int{}}
	for _, k := range []uint64{8, 10, 11} {
		s.census[k] = commit(t, epochCensus(k), testTime+k*604_800)
	}
	s.funded = funded(8, scenarioFresh8, 9, scenarioFresh8+scenarioFresh9)
	s.fIn = BuildInput{
		NoID: testNoID, ChainID: testChainID, Netuid: testNetuid, Epoch: 9, FirstEpoch: testFirst, CarryColdkey: carryKey,
		Epochs: []EpochInput{
			s.epochInput(8),
			s.epochInput(9),
		},
		FundedThrough: s.funded,
	}
	var err error
	if s.f, err = Build(ctx, s.fIn); err != nil {
		t.Fatal(err)
	}
	// The validator audited F while it was pending.
	res, err := Verify(ctx, s.verifyInput(s.f.Commit, nil, s.f.Objects))
	if err != nil || res.Err != nil {
		t.Fatalf("auditing F: %v %v %v", err, res.Check, res.Err)
	}

	// The carry contract claimed F's carry account and returned it in epoch
	// 11: epoch 10's carry is late.
	owed := s.f.State.Obligations()
	s.recycled[11] = owed
	s.funded[10] = rao(scenarioFresh8 + scenarioFresh9 + scenarioFresh10)
	var t11 uint256.Int
	t11.AddUint64(&owed, scenarioFresh8+scenarioFresh9+scenarioFresh10+scenarioFresh11)
	s.funded[11] = t11
	s.pIn = s.buildInput(11)
	if s.p, err = Build(ctx, s.pIn); err != nil {
		t.Fatal(err)
	}
	return s
}

// epochInput is epoch k's build input.
func (s *scenario) epochInput(k uint64) EpochInput {
	input := EpochInput{Epoch: k, Recycled: s.recycled[k], Bound: s.heads[k]}
	if c := s.census[k]; c != nil {
		input.Census = c.input()
		input.Pulse = testPulse(DrawRound(c.time))
		input.Coldkeys = coldkeysOf(sharedColdkeys, epochCensus(k))
	}
	return input
}

// buildInput builds the root for epoch e over F.
func (s *scenario) buildInput(e uint64) BuildInput {
	in := BuildInput{
		NoID: testNoID, ChainID: testChainID, Netuid: testNetuid, Epoch: e, FirstEpoch: testFirst, CarryColdkey: carryKey,
		Previous:      &Previous{Root: s.f.Commit, Accounts: s.f.Accounts, State: s.f.State},
		FundedThrough: s.funded,
	}
	for k := uint64(10); k <= e; k++ {
		in.Epochs = append(in.Epochs, s.epochInput(k))
	}
	return in
}

// facts returns the chain's facts for epochs from … to.
func (s *scenario) facts(from, to uint64) map[uint64]EpochFacts {
	out := map[uint64]EpochFacts{}
	for k := from; k <= to; k++ {
		f := EpochFacts{Recycled: s.recycled[k]}
		if c := s.census[k]; c != nil {
			f.Census = c.facts()
			f.Pulse = testPulse(DrawRound(c.time))
		}
		out[k] = f
	}
	return out
}

// verifyInput is the auditor's input for a pending root over previous.
func (s *scenario) verifyInput(pending ledger.Root, previous *ledger.Root, objects ledger.Objects) VerifyInput {
	from := uint64(testFirst)
	if previous != nil {
		from = previous.Epoch + 1
	}
	return VerifyInput{
		NoID: testNoID, ChainID: testChainID, Netuid: testNetuid, FirstEpoch: testFirst, CarryColdkey: carryKey,
		Pending: pending, Previous: previous, FundedThrough: s.funded, Epochs: s.facts(from, pending.Epoch),
		Fetcher: objects, Custody: s.custody, Heads: s.heads, MaxFetchBytes: ledger.DefaultMaxFetchBytes,
	}
}

// verify audits a pending root over F with the scenario's custody.
func (s *scenario) verify(t testing.TB, pending ledger.Root, objects ledger.Objects) *Result {
	t.Helper()
	res, err := Verify(context.Background(), s.verifyInput(pending, &s.f.Commit, objects))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return res
}

func expectPass(t testing.TB, res *Result) {
	t.Helper()
	if res.Err != nil || res.Check != CheckNone || res.Action() != ledger.ActionApprove {
		t.Fatalf("expected a pass, got check %v action %v: %v", res.Check, res.Action(), res.Err)
	}
}

func expectFailure(t testing.TB, res *Result, check Check, want error) {
	t.Helper()
	if res.Check != check || !errors.Is(res.Err, want) {
		t.Fatalf("expected %v with %v, got %v: %v", check, want, res.Check, res.Err)
	}
}

// publication is a pending root's published objects, open to mutation.
type publication struct {
	header  *Header
	objects ledger.Objects
	commit  ledger.Root
}

func publish(t testing.TB, l *Ledger) *publication {
	t.Helper()
	h, err := DecodeHeader(l.Header)
	if err != nil {
		t.Fatal(err)
	}
	h.Epochs = slices.Clone(h.Epochs)
	return &publication{header: h, objects: maps.Clone(l.Objects), commit: l.Commit}
}

// put publishes an object and returns its reference.
func (pub *publication) put(data []byte) ledger.Ref {
	ref := ledger.RefOf(data)
	pub.objects[ref] = data
	return ref
}

// seal encodes the header, publishes it and returns the commit naming it.
func (pub *publication) seal(t testing.TB) ledger.Root {
	t.Helper()
	data, err := EncodeHeader(pub.header)
	if err != nil {
		t.Fatal(err)
	}
	return pub.sealBytes(data)
}

func (pub *publication) sealBytes(data []byte) ledger.Root {
	commit := pub.commit
	commit.LedgerHash = pub.put(data)
	return commit
}
