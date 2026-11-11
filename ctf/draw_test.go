package ctf

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ledger"
)

// §4.4: ρ_k is the first quicknet round whose time, 1692803367 + 3·(r − 1),
// is at least 60 s after the census block's.
func TestDrawRound(t *testing.T) {
	if RoundTime(1) != 1_692_803_367 || RoundTime(1000) != 1_692_806_364 {
		t.Fatal("round times")
	}
	for _, c := range []struct{ commit, round uint64 }{
		{0, 1},
		{1_692_803_307, 1}, // 60 s before genesis: round 1 is exactly 60 s later
		{1_692_803_308, 2},
		{1_692_806_302, 1000},
		{1_692_806_304, 1000}, // round 1000's time is 1692806364
		{1_692_806_305, 1001},
	} {
		if r := DrawRound(c.commit); r != c.round {
			t.Fatalf("commit at %d: round %d, want %d", c.commit, r, c.round)
		}
	}
	for commit := uint64(1_796_000_000); commit < 1_796_000_010; commit++ {
		r := DrawRound(commit)
		if RoundTime(r) < commit+DrawDelay || RoundTime(r-1) >= commit+DrawDelay {
			t.Fatalf("commit at %d: round %d at %d", commit, r, RoundTime(r))
		}
	}
	if DrawRound(^uint64(0)) == 0 {
		t.Fatal("overflow")
	}
}

// quicknetRound1000 is drand quicknet's real signature for round 1000
// (crv4/golden_test.go).
const quicknetRound1000 = "b44679b9a59af2ec876b1a6b1ad52ea9b1615fc3982b19576350f93447cb1125e342b73a8dd2bacbe47e4b6b63ed5e39"

// §4.4: a pulse verifies against quicknet's public key, and its randomness is
// the sha256 of its signature.
func TestQuicknetPulse(t *testing.T) {
	signature, _ := hex.DecodeString(quicknetRound1000)
	p := &Pulse{Round: 1000, Signature: signature}
	if err := p.Verify(); err != nil {
		t.Fatalf("quicknet round 1000: %v", err)
	}
	if p.Randomness() != sha256.Sum256(signature) {
		t.Fatal("randomness")
	}
	for name, bad := range map[string]*Pulse{
		"another round":     {Round: 1001, Signature: signature},
		"flipped bit":       {Round: 1000, Signature: flip(signature, 47, 1)},
		"short":             {Round: 1000, Signature: signature[:47]},
		"long":              {Round: 1000, Signature: append(bytes.Clone(signature), 0)},
		"not on curve":      {Round: 1000, Signature: flip(signature, 10, 0x40)},
		"point at infinity": {Round: 1000, Signature: append([]byte{0xc0}, make([]byte, 47)...)},
		"test key":          testPulse(1000),
	} {
		if err := bad.Verify(); !errors.Is(err, ErrPulse) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	withTestKey(t)
	if err := testPulse(1000).Verify(); err != nil {
		t.Fatalf("test key: %v", err)
	}
	if err := p.Verify(); !errors.Is(err, ErrPulse) {
		t.Fatal("quicknet's signature verified against the test key")
	}
}

func flip(b []byte, i int, mask byte) []byte {
	out := bytes.Clone(b)
	out[i] ^= mask
	return out
}

// §4.4: σ_k, the tickets and the member tickets are keccak256 of exactly the
// spec's byte strings, checked against go-ethereum's keccak and pinned.
func TestSeedAndTickets(t *testing.T) {
	census := ledger.RefOf([]byte("census"))
	var randomness [32]byte
	for i := range randomness {
		randomness[i] = byte(i)
	}
	seed := Seed(964, 25, PoolID(1), 8, census, randomness)
	var b []byte
	b = append(b, "urnetwork/ctf-draw/v1"...)
	b = binary.BigEndian.AppendUint64(b, 964)
	b = binary.BigEndian.AppendUint16(b, 25)
	b = binary.BigEndian.AppendUint64(b, 4_294_967_297)
	b = binary.BigEndian.AppendUint64(b, 8)
	b = append(b, census[:]...)
	b = append(b, randomness[:]...)
	if len(b) != 21+8+2+8+8+32+32 || [32]byte(crypto.Keccak256(b)) != seed {
		t.Fatal("seed")
	}
	unit := unitHash(7)
	ticket := Ticket(seed, RoleExtender, cc("is"), unit)
	if [32]byte(crypto.Keccak256(seed[:], []byte{2}, []byte("is"), unit[:])) != ticket {
		t.Fatal("ticket")
	}
	client := id(42)
	member := MemberTicket(seed, RoleProvider, unit, client)
	if [32]byte(crypto.Keccak256(seed[:], []byte{1}, unit[:], client[:])) != member {
		t.Fatal("member ticket")
	}
	for _, c := range []struct {
		name string
		got  [32]byte
		want string
	}{
		{"seed", seed, "e0868eedb9b604179233acd85df155ab53ae1bb83864a22dd3ee52d9466e437a"},
		{"ticket", ticket, "54c82875851dbc98ae0f0cb0fbd30390725f4a23062e338d28bac6f9fb6020f6"},
		{"member", member, "9301dfe6d8f6803020396f63260eca38a81774283b753df3aaa147fe1f3accc8"},
	} {
		if hex.EncodeToString(c.got[:]) != c.want {
			t.Errorf("%s %x, pinned %s", c.name, c.got, c.want)
		}
	}
	// Every input moves the seed.
	others := [][32]byte{
		Seed(965, 25, PoolID(1), 8, census, randomness),
		Seed(964, 26, PoolID(1), 8, census, randomness),
		Seed(964, 25, PoolID(2), 8, census, randomness),
		Seed(964, 25, PoolID(1), 9, census, randomness),
		Seed(964, 25, PoolID(1), 8, ledger.RefOf([]byte("other")), randomness),
		Seed(964, 25, PoolID(1), 8, census, sha256.Sum256(nil)),
	}
	for i, other := range others {
		if other == seed {
			t.Fatalf("input %d does not move the seed", i)
		}
	}
	if crv4.QuicknetDST != "BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_" || crv4.DrandPeriod != 3 || crv4.DrandGenesisTime != 1_692_803_367 {
		t.Fatal("quicknet constants")
	}
}

// drawOf draws candidates with a fixed seed.
func drawOf(t *testing.T, seed byte, bound map[[16]byte]bool, candidates ...Candidate) *Draw {
	t.Helper()
	objects, err := EncodeCensus(&Census{NoID: 1, Epoch: 8, Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCensus(objects.Header, objects.Objects)
	if err != nil {
		t.Fatal(err)
	}
	return DrawCensus(decoded.Candidates, [32]byte{seed}, bound)
}

// §4.4: with five units or fewer every unit wins, ranked by ticket; with
// more, the five smallest tickets win.
func TestDrawWinners(t *testing.T) {
	de := PotIndex(cc("de").Index(), RoleProvider)
	for n := 1; n <= 12; n++ {
		var candidates []Candidate
		for u := 1; u <= n; u++ {
			candidates = append(candidates, provider(u, "de", u))
		}
		d := drawOf(t, byte(n), nil, candidates...)
		if d.Units[de] != uint32(n) || len(d.Winners[de]) != min(n, Ranks) {
			t.Fatalf("%d units: %d units, %d winners", n, d.Units[de], len(d.Winners[de]))
		}
		var tickets [][32]byte
		for u := 1; u <= n; u++ {
			tickets = append(tickets, Ticket(d.Seed, RoleProvider, cc("de"), unitHash(u)))
		}
		slices.SortFunc(tickets, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
		for i, w := range d.Winners[de] {
			if w.Rank != i+1 || w.Ticket != tickets[i] || w.Ticket != Ticket(d.Seed, RoleProvider, cc("de"), w.Unit) || w.Role != RoleProvider || w.Country != cc("de") {
				t.Fatalf("%d units: winner %d %+v", n, i, w)
			}
		}
		for j := range d.Winners {
			if j != de && (len(d.Winners[j]) != 0 || d.Units[j] != 0) {
				t.Fatalf("pot %d drawn", j)
			}
		}
	}
}

// §4.2, §4.4: a unit enters its lowest client id's country, once per role;
// its member is the candidate with the smallest member ticket; a client may
// win in both roles.
func TestDrawUnitsAndMembers(t *testing.T) {
	d := drawOf(t, 1, nil,
		provider(5, "tm", 1), provider(3, "is", 1), provider(9, "de", 1), // lowest client 3: IS
		extender(5, "kp", 1), // the same unit and client, as an extender: KP
	)
	is, tm, kp := PotIndex(cc("is").Index(), RoleProvider), PotIndex(cc("tm").Index(), RoleProvider), PotIndex(cc("kp").Index(), RoleExtender)
	if d.Units[is] != 1 || d.Units[tm] != 0 || d.Units[kp] != 1 {
		t.Fatalf("units IS %d TM %d KP %d", d.Units[is], d.Units[tm], d.Units[kp])
	}
	w := d.Winners[is][0]
	best := id(3)
	bestTicket := MemberTicket(d.Seed, RoleProvider, unitHash(1), best)
	for _, n := range []int{5, 9} {
		if t2 := MemberTicket(d.Seed, RoleProvider, unitHash(1), id(n)); bytes.Compare(t2[:], bestTicket[:]) < 0 {
			best, bestTicket = id(n), t2
		}
	}
	if w.ClientID != best || w.MemberTicket != bestTicket || w.Country != cc("is") || w.Unit != unitHash(1) {
		t.Fatalf("winner %+v", w)
	}
	if e := d.Winners[kp][0]; e.ClientID != id(5) || e.Role != RoleExtender {
		t.Fatalf("extender winner %+v", e)
	}
}

// A candidate with a fleet binding is left out before the draw: it is never
// paid, and the unit's country falls to its next lowest client id's.
func TestDrawLeavesOutBoundCandidates(t *testing.T) {
	candidates := []Candidate{provider(3, "is", 1), provider(5, "tm", 1), provider(6, "us", 2)}
	d := drawOf(t, 2, map[[16]byte]bool{id(3): true, id(6): false}, candidates...)
	is, tm, us := PotIndex(cc("is").Index(), RoleProvider), PotIndex(cc("tm").Index(), RoleProvider), PotIndex(cc("us").Index(), RoleProvider)
	if d.Units[is] != 0 || d.Units[tm] != 1 || d.Units[us] != 1 || d.Winners[tm][0].ClientID != id(5) {
		t.Fatalf("units IS %d TM %d US %d", d.Units[is], d.Units[tm], d.Units[us])
	}
	if len(d.Bound) != 1 || d.Bound[0].ClientID != id(3) {
		t.Fatalf("bound %v", d.Bound)
	}
	d = drawOf(t, 2, map[[16]byte]bool{id(3): true, id(5): true}, candidates...)
	if d.Units[is] != 0 || d.Units[tm] != 0 || len(d.Bound) != 2 {
		t.Fatal("a unit of bound candidates only is drawn")
	}
}

// §4.4: taking the smallest tickets draws a uniform set of five units, and
// the member ticket a uniform member: over 4,000 seeds each of 10 units wins
// about half the time, and each of a unit's 4 members about a quarter.
func TestDrawUniform(t *testing.T) {
	var candidates []Candidate
	for u := 1; u <= 10; u++ {
		candidates = append(candidates, provider(u, "fr", u))
	}
	candidates = append(candidates, extender(21, "fr", 30), extender(22, "fr", 30), extender(23, "fr", 30), extender(24, "fr", 30))
	objects, err := EncodeCensus(&Census{NoID: 1, Epoch: 8, Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := DecodeCensus(objects.Header, objects.Objects)
	if err != nil {
		t.Fatal(err)
	}
	wins, members := map[[32]byte]int{}, map[[16]byte]int{}
	const seeds = 4000
	for s := range seeds {
		var seed [32]byte
		binary.BigEndian.PutUint64(seed[:], uint64(s))
		d := DrawCensus(rows.Candidates, seed, nil)
		for _, w := range d.Winners[PotIndex(cc("fr").Index(), RoleProvider)] {
			wins[w.Unit]++
		}
		members[d.Winners[PotIndex(cc("fr").Index(), RoleExtender)][0].ClientID]++
	}
	for u := 1; u <= 10; u++ {
		// Binomial(4000, 1/2): sd ≈ 32; allow 5 sd.
		if n := wins[unitHash(u)]; n < 2000-160 || n > 2000+160 {
			t.Fatalf("unit %d won %d of %d draws", u, n, seeds)
		}
	}
	for n := 21; n <= 24; n++ {
		// Binomial(4000, 1/4): sd ≈ 27; allow 5 sd.
		if m := members[id(n)]; m < 1000-140 || m > 1000+140 {
			t.Fatalf("member %d paid %d of %d draws", n, m, seeds)
		}
	}
}
