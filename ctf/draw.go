package ctf

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"golang.org/x/crypto/sha3"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ledger"
)

const (
	// DrawDomain is the domain of the draw's seed (§4.4).
	DrawDomain = "urnetwork/ctf-draw/v1"
	// DrawDelay is the least time, in seconds, from the census commitment's
	// block to the drand round that seeds the draw (§4.4).
	DrawDelay = 60
	// PulseSignatureBytes is the size of a quicknet signature: a compressed
	// BLS12-381 G1 point.
	PulseSignatureBytes = 48
)

// ErrPulse is returned for a drand pulse that is not quicknet's for its
// round: a signature that is not a compressed G1 point in the subgroup, or
// that does not verify against quicknet's public key.
var ErrPulse = errors.New("ctf: drand pulse does not verify against quicknet's key")

var quicknetPublicKey bls12381.G2Affine

func init() {
	raw, err := hex.DecodeString(crv4.QuicknetPublicKeyHex)
	if err != nil {
		panic("ctf: quicknet public key hex: " + err.Error())
	}
	if _, err := quicknetPublicKey.SetBytes(raw); err != nil {
		panic("ctf: quicknet public key point: " + err.Error())
	}
}

// RoundTime returns the time, in unix seconds, of quicknet round r >= 1:
// 1692803367 + 3·(r − 1).
func RoundTime(r uint64) uint64 {
	return crv4.DrandGenesisTime + crv4.DrandPeriod*(r-1)
}

// DrawRound returns ρ_k, the first quicknet round whose time is at least
// DrawDelay seconds after commitTime, the unix time in seconds of the census
// commitment's block B_k (its EVM header timestamp).
func DrawRound(commitTime uint64) uint64 {
	t := commitTime + DrawDelay
	if commitTime > math.MaxUint64-DrawDelay {
		t = math.MaxUint64
	}
	if t <= crv4.DrandGenesisTime {
		return 1
	}
	return 1 + (t-crv4.DrandGenesisTime+crv4.DrandPeriod-1)/crv4.DrandPeriod
}

// Pulse is a drand quicknet pulse: a round and its BLS signature, from the
// chain's Drand pallet or any relay.
type Pulse struct {
	Round     uint64
	Signature []byte
}

// Verify checks the signature against quicknet's public key: e(σ, g2) =
// e(H(sha256(round)), pk), with H quicknet's hash to G1 (crv4.QuicknetDST).
func (p *Pulse) Verify() error {
	if len(p.Signature) != PulseSignatureBytes {
		return fmt.Errorf("%w: round %d: %d signature bytes", ErrPulse, p.Round, len(p.Signature))
	}
	var signature bls12381.G1Affine
	if _, err := signature.SetBytes(p.Signature); err != nil {
		return fmt.Errorf("%w: round %d: %v", ErrPulse, p.Round, err)
	}
	message, err := bls12381.HashToG1(crv4.RoundIdentity(p.Round), []byte(crv4.QuicknetDST))
	if err != nil {
		return fmt.Errorf("%w: round %d: %v", ErrPulse, p.Round, err)
	}
	_, _, _, g2 := bls12381.Generators()
	var negG2 bls12381.G2Affine
	negG2.Neg(&g2)
	ok, err := bls12381.PairingCheck([]bls12381.G1Affine{signature, message}, []bls12381.G2Affine{negG2, quicknetPublicKey})
	if err != nil || !ok {
		return fmt.Errorf("%w: round %d", ErrPulse, p.Round)
	}
	return nil
}

// Randomness returns the pulse's randomness, sha256 of its signature.
func (p *Pulse) Randomness() [32]byte {
	return sha256.Sum256(p.Signature)
}

// Seed returns σ_k = keccak256("urnetwork/ctf-draw/v1" ‖ chain_id:u64 ‖
// netuid:u16 ‖ pool_id:u64 ‖ k:u64 ‖ censusHash ‖ rand_k), integers
// big-endian (§4.4).
func Seed(chainID uint64, netuid uint16, poolID, epoch uint64, censusHash ledger.Ref, randomness [32]byte) [32]byte {
	b := make([]byte, 0, len(DrawDomain)+8+2+8+8+32+32)
	b = append(b, DrawDomain...)
	b = binary.BigEndian.AppendUint64(b, chainID)
	b = binary.BigEndian.AppendUint16(b, netuid)
	b = binary.BigEndian.AppendUint64(b, poolID)
	b = binary.BigEndian.AppendUint64(b, epoch)
	b = append(b, censusHash[:]...)
	b = append(b, randomness[:]...)
	return keccak(b)
}

// Ticket returns a unit's ticket in its draw, keccak256(σ_k ‖ role:u8 ‖
// country:2 ‖ unit:32) (§4.4).
func Ticket(seed [32]byte, role Role, country Country, unit [32]byte) [32]byte {
	var b [32 + 1 + 2 + 32]byte
	copy(b[:32], seed[:])
	b[32] = byte(role)
	copy(b[33:35], country[:])
	copy(b[35:], unit[:])
	return keccak(b[:])
}

// MemberTicket returns a candidate's ticket within its unit, keccak256(σ_k
// ‖ role:u8 ‖ unit:32 ‖ client_id:16) (§4.4).
func MemberTicket(seed [32]byte, role Role, unit [32]byte, clientID [16]byte) [32]byte {
	var b [32 + 1 + 32 + 16]byte
	copy(b[:32], seed[:])
	b[32] = byte(role)
	copy(b[33:65], unit[:])
	copy(b[65:], clientID[:])
	return keccak(b[:])
}

func keccak(b []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// Winner is a winning unit of a draw and its member, the candidate paid.
type Winner struct {
	Role    Role
	Country Country
	// Rank is 1 to 5, by ticket ascending.
	Rank   int
	Unit   [32]byte
	Family uint8
	Ticket [32]byte
	// ClientID is the member: the unit's candidate with the smallest member
	// ticket.
	ClientID     [16]byte
	MemberTicket [32]byte
}

// Draw is an epoch's draw (§4.4).
type Draw struct {
	Seed [32]byte
	// Units holds, per pot, the units in its draw, |U_{c,r}|.
	Units [NumPots]uint32
	// Winners holds, per pot, the min(5, |U_{c,r}|) units with the smallest
	// tickets, rank 1 first.
	Winners [NumPots][]Winner
	// Bound lists the census's candidates with an active fleet binding at
	// either boundary, which the draw leaves out, in census order. A correct
	// census lists none (P5, X5).
	Bound []Candidate
}

// DrawCensus runs the draw over a census's candidates, which DecodeCensusRows
// or EncodeCensus checked, with seed σ_k. bound maps client ids to an active
// fleet binding at either boundary of the epoch (ledger.HeadOracle): a
// candidate mapped to true is left out first, so a head-bound client is never
// paid and never decides a unit's country. Each remaining unit enters, per
// role, the draw of its lowest client id's country (§4.2); its ticket ranks it
// there, and in each winning unit the candidate with the smallest member
// ticket is paid. Ties, which need a keccak collision, go to the lower unit
// or client id.
func DrawCensus(candidates []Candidate, seed [32]byte, bound map[[16]byte]bool) *Draw {
	type unitKey struct {
		role Role
		unit [32]byte
	}
	type unitDraw struct {
		key    unitKey
		family uint8
		lowest [16]byte
		// country is the lowest client id's country.
		country Country
		pot     int
		ticket  [32]byte
		// rank is the unit's rank in its pot's draw, or 0.
		rank int
	}
	d := &Draw{Seed: seed}
	index := make(map[unitKey]int)
	var units []unitDraw
	for i := range candidates {
		c := &candidates[i]
		if !c.Role.Valid() || !c.Country.InL() {
			// Not a census row; checked censuses have none.
			continue
		}
		if bound[c.ClientID] {
			d.Bound = append(d.Bound, *c)
			continue
		}
		key := unitKey{c.Role, c.Unit}
		j, ok := index[key]
		if !ok {
			index[key] = len(units)
			units = append(units, unitDraw{key: key, family: c.Family, lowest: c.ClientID, country: c.Country})
			continue
		}
		if bytes.Compare(c.ClientID[:], units[j].lowest[:]) < 0 {
			units[j].lowest, units[j].country = c.ClientID, c.Country
		}
	}

	// top[pot] holds the pot's best units so far, by (ticket, unit).
	var top [NumPots][Ranks]int
	var count [NumPots]int
	better := func(a, b *unitDraw) bool {
		if c := bytes.Compare(a.ticket[:], b.ticket[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(a.key.unit[:], b.key.unit[:]) < 0
	}
	for j := range units {
		u := &units[j]
		u.pot = PotIndex(u.country.Index(), u.key.role)
		u.ticket = Ticket(seed, u.key.role, u.country, u.key.unit)
		d.Units[u.pot]++
		best := &top[u.pot]
		n := count[u.pot]
		if n == Ranks && !better(u, &units[best[n-1]]) {
			continue
		}
		if n < Ranks {
			n++
			count[u.pot] = n
		}
		i := n - 1
		for i > 0 && better(u, &units[best[i-1]]) {
			best[i] = best[i-1]
			i--
		}
		best[i] = j
	}
	for pot := range top {
		if count[pot] == 0 {
			continue
		}
		d.Winners[pot] = make([]Winner, count[pot])
		for i := range count[pot] {
			u := &units[top[pot][i]]
			u.rank = i + 1
			d.Winners[pot][i] = Winner{Role: u.key.role, Country: u.country, Rank: u.rank, Unit: u.key.unit, Family: u.family, Ticket: u.ticket}
		}
	}

	// Members: the smallest member ticket among each winning unit's
	// remaining candidates.
	found := make(map[unitKey]bool)
	for i := range candidates {
		c := &candidates[i]
		if !c.Role.Valid() || !c.Country.InL() || bound[c.ClientID] {
			continue
		}
		key := unitKey{c.Role, c.Unit}
		u := &units[index[key]]
		if u.rank == 0 {
			continue
		}
		w := &d.Winners[u.pot][u.rank-1]
		t := MemberTicket(seed, c.Role, c.Unit, c.ClientID)
		if !found[key] || bytes.Compare(t[:], w.MemberTicket[:]) < 0 || (t == w.MemberTicket && bytes.Compare(c.ClientID[:], w.ClientID[:]) < 0) {
			found[key] = true
			w.ClientID, w.MemberTicket = c.ClientID, t
		}
	}
	return d
}
