package ctf

import (
	"context"
	"encoding/binary"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
)

// §5 bounds: a census of 100,000 candidates, about 5 MB, is encoded, drawn,
// built into a root and audited well inside the 600-block census window and
// the 1,200-block commit window; the times are logged. Candidates spread over
// 150 countries, both roles, in units of one to three clients.
func TestScaleCensus100k(t *testing.T) {
	if testing.Short() {
		t.Skip("scale")
	}
	withTestKey(t)
	ctx := context.Background()
	const candidates = 100_000
	random := rand.New(rand.NewPCG(100_000, 8))
	c := &Census{NoID: testNoID, Epoch: 8}
	coldkeys := make(map[[16]byte][32]byte, candidates)
	unit := 0
	for n := 1; n <= candidates; {
		role := RoleProvider
		if random.IntN(5) == 0 {
			role = RoleExtender
		}
		unit++
		family := uint8(FamilyIPv4)
		if unit%4 == 0 {
			family = FamilyIPv6
		}
		for range 1 + random.IntN(3) {
			if n > candidates {
				break
			}
			// Zipf-like: a few countries hold most candidates.
			country := CountryAt(int(random.ExpFloat64()*25) % 150)
			c.Candidates = append(c.Candidates, Candidate{Role: role, Country: country, Unit: unitHash(unit), Family: family, ClientID: id(n)})
			var coldkey [32]byte
			binary.BigEndian.PutUint64(coldkey[:8], uint64(1+n%40_000))
			coldkeys[id(n)] = coldkey
			n++
		}
	}

	start := time.Now()
	objects, err := EncodeCensus(c)
	if err != nil {
		t.Fatal(err)
	}
	encoded := time.Since(start)
	var censusBytes int
	for _, data := range objects.Objects {
		censusBytes += len(data)
	}
	commitTime := uint64(testTime)
	in := BuildInput{
		NoID: testNoID, ChainID: testChainID, Netuid: testNetuid, Epoch: 8, FirstEpoch: testFirst, CarryColdkey: carryKey,
		Epochs: []EpochInput{{
			Epoch: 8, Census: &CensusInput{Hash: objects.Hash, Block: 9_650_500, Time: commitTime, Objects: objects.Objects},
			Pulse: testPulse(DrawRound(commitTime)), Coldkeys: coldkeys,
		}},
		FundedThrough: map[uint64]uint256.Int{8: rao(119_583_893_572)},
	}
	start = time.Now()
	l, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	built := time.Since(start)
	e := l.Epochs[0]

	start = time.Now()
	res, err := Verify(ctx, VerifyInput{
		NoID: testNoID, ChainID: testChainID, Netuid: testNetuid, FirstEpoch: testFirst, CarryColdkey: carryKey,
		Pending: l.Commit, FundedThrough: in.FundedThrough,
		Epochs:  map[uint64]EpochFacts{8: {Census: &CensusCommitment{Hash: objects.Hash, Block: 9_650_500, Time: commitTime}, Pulse: in.Epochs[0].Pulse}},
		Fetcher: l.Objects, Custody: ledger.NewMemoryCustody(), MaxFetchBytes: ledger.DefaultMaxFetchBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	verified := time.Since(start)
	expectPass(t, res)

	var units, pots, winners int
	for j := range e.Pots {
		units += int(e.Pots[j].Units)
		if e.Pots[j].Units > 0 {
			pots++
		}
		winners += e.Pots[j].Winners
	}
	t.Logf("%d candidates in %d units over %d pots, %d census bytes; %d winners, %d accounts", e.Candidates, units, pots, censusBytes, winners, len(l.Accounts))
	t.Logf("encode %v, build %v, verify %v", encoded.Round(time.Millisecond), built.Round(time.Millisecond), verified.Round(time.Millisecond))
	if e.Candidates != candidates || len(e.Awards) != winners || censusBytes < 5_000_000 || censusBytes > 5_400_000 {
		t.Fatalf("candidates %d, awards %d, census bytes %d", e.Candidates, len(e.Awards), censusBytes)
	}
	if encoded+built+verified > time.Minute {
		t.Fatalf("encode %v, build %v, verify %v", encoded, built, verified)
	}
}
