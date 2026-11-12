//go:build linux || darwin

// Capture the flag's split, entries, setting and CTF pools go through the same
// exact math, artifacts, chain fixtures and treasury row as the provider pools.
// Entries are signed with the test key that signs test price sheets, as the
// owner's key signs both on mainnet.
package validator

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/amendment"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// The shares every version 2 line states (CAPTURETHEFLAG.md §2.1).
var ctfTestShares = amendment.Shares{Pool: amendment.Ratio{Num: 4, Den: 10}, Ctf: amendment.Ratio{Num: 1, Den: 10}, Head: amendment.Ratio{Num: 5, Den: 10}}

// The carry contract a test line names; any nonzero address is a valid line.
var ctfTestCarry = common.HexToAddress("0x00000000000000000000000000000000000Ca77F")

// Test deployments sign amendments with the test price key, and nothing here
// reaches the real site. Set once, before any test runs; production keeps the
// pinned owner key.
func init() {
	signer := crypto.PubkeyToAddress(depositPriceTestKey.PublicKey)
	releaseAmendmentSigner = func(chainID uint64, netuid uint16) (common.Address, bool) {
		if chainID == depositPriceTestChainID && (netuid == 7 || netuid == 521) {
			return signer, true
		}
		return amendment.SignerFor(chainID, netuid)
	}
	releaseAmendmentURL = "http://127.0.0.1:9/amendments.yml"
}

// ctfTestEntry is operator noID's version 2 entry from fromEpoch, signed by key,
// or configured (unsigned) when key is nil.
func ctfTestEntry(t testing.TB, chainID uint64, netuid uint16, noID, fromEpoch uint64, key *ecdsa.PrivateKey) *amendment.Entry {
	t.Helper()
	entry := amendment.NewCaptureTheFlag(chainID, netuid, noID, fromEpoch, ctfTestCarry)
	if key != nil {
		if err := entry.Sign(key); err != nil {
			t.Fatal(err)
		}
		return entry
	}
	line, err := entry.Line()
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := amendment.ParseFallback(chainID, netuid, []string{line})
	if err != nil {
		t.Fatal(err)
	}
	return &fallback.Entries()[0]
}

func ctfTestWire(t testing.TB, entry *amendment.Entry) ReleaseAmendmentEntry {
	t.Helper()
	wire, err := newReleaseAmendmentEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func ctfTestRats(t testing.TB, scores []*big.Rat) []string {
	t.Helper()
	out := make([]string, len(scores))
	for index, score := range scores {
		out[index] = score.RatString()
	}
	return out
}

// Every cede case of CAPTURETHEFLAG.md §2.1 in order: a non-empty CTF channel
// takes exactly its share; pool and head share the rest 4 : 5, each ceding to
// the other; an empty CTF channel (no input, a mask, or no pool weight) gives
// its share to rule 2; and no pool or head weight is no row.
func TestBuildWeightVectorCtfCedeRules(t *testing.T) {
	t.Parallel()
	one := func(uid uint16, score int64) ExactWeightInput {
		return ExactWeightInput{UID: uid, Score: big.NewRat(score, 1)}
	}
	for _, test := range []struct {
		name             string
		pools, ctf, head []ExactWeightInput
		masked           map[uint16]bool
		uids             []uint16
		scores           []string
	}{
		{name: "no head fleet", pools: []ExactWeightInput{one(10, 1)}, ctf: []ExactWeightInput{one(11, 1)},
			uids: []uint16{10, 11}, scores: []string{"9/10", "1/10"}},
		{name: "head fleets weighted", pools: []ExactWeightInput{one(10, 1)}, ctf: []ExactWeightInput{one(11, 1)}, head: []ExactWeightInput{one(20, 1), one(21, 3)},
			uids: []uint16{10, 11, 20, 21}, scores: []string{"2/5", "1/10", "1/8", "3/8"}},
		{name: "empty CTF channel", pools: []ExactWeightInput{one(10, 1)}, head: []ExactWeightInput{one(20, 1)},
			uids: []uint16{10, 20}, scores: []string{"4/9", "5/9"}},
		{name: "empty CTF channel and no head fleet", pools: []ExactWeightInput{one(10, 1)},
			uids: []uint16{10}, scores: []string{"1"}},
		{name: "masked CTF pool", pools: []ExactWeightInput{one(10, 1)}, ctf: []ExactWeightInput{one(11, 1)}, head: []ExactWeightInput{one(20, 1)}, masked: map[uint16]bool{11: true},
			uids: []uint16{10, 20}, scores: []string{"4/9", "5/9"}},
		{name: "no pool weight empties the CTF channel", ctf: []ExactWeightInput{one(11, 1)}, head: []ExactWeightInput{one(20, 1)},
			uids: []uint16{20}, scores: []string{"1"}},
		{name: "masked pools empty the CTF channel", pools: []ExactWeightInput{one(10, 1)}, ctf: []ExactWeightInput{one(11, 1)}, head: []ExactWeightInput{one(20, 1)}, masked: map[uint16]bool{10: true},
			uids: []uint16{20}, scores: []string{"1"}},
		{name: "two operators divide both channels alike", pools: []ExactWeightInput{one(10, 1), one(12, 3)}, ctf: []ExactWeightInput{one(11, 1), one(13, 3)},
			uids: []uint16{10, 11, 12, 13}, scores: []string{"9/40", "1/40", "27/40", "3/40"}},
		{name: "a controlled operator masks both of its UIDs", pools: []ExactWeightInput{one(10, 1), one(12, 3)}, ctf: []ExactWeightInput{one(11, 1), one(13, 3)}, masked: map[uint16]bool{12: true, 13: true},
			uids: []uint16{10, 11}, scores: []string{"9/10", "1/10"}},
	} {
		uids, scores, err := BuildWeightVectorCtf(test.pools, test.ctf, test.head, ctfTestShares, test.masked)
		if err != nil || !slices.Equal(uids, test.uids) || !slices.Equal(ctfTestRats(t, scores), test.scores) {
			t.Fatalf("%s: %v %v, %v; want %v %v", test.name, uids, ctfTestRats(t, scores), err, test.uids, test.scores)
		}
	}
	if _, _, err := BuildWeightVectorCtf(nil, []ExactWeightInput{one(11, 1)}, nil, ctfTestShares, nil); !errors.Is(err, errNoPositiveUnmaskedWeights) {
		t.Fatalf("a CTF channel without pool or head weight made a row: %v", err)
	}
	for _, shares := range []amendment.Shares{
		{Pool: amendment.Ratio{Num: 4, Den: 10}, Ctf: amendment.Ratio{Num: 2, Den: 10}, Head: amendment.Ratio{Num: 5, Den: 10}},
		{Pool: amendment.Ratio{Num: 9, Den: 10}, Ctf: amendment.Ratio{Num: 1, Den: 10}, Head: amendment.Ratio{Num: 0, Den: 10}},
		{Pool: amendment.Ratio{Num: 4, Den: 0}, Ctf: amendment.Ratio{Num: 1, Den: 10}, Head: amendment.Ratio{Num: 5, Den: 10}},
		{},
	} {
		if _, _, err := BuildWeightVectorCtf([]ExactWeightInput{one(10, 1)}, nil, nil, shares, nil); err == nil {
			t.Fatalf("shares %+v were admitted", shares)
		}
	}
	// Theta's builder is unchanged: E_c alone replaces it.
	uids, scores, err := BuildWeightVectorExact([]ExactWeightInput{one(10, 1)}, []ExactWeightInput{one(20, 1)}, protocol.Rational{Numerator: 3, Denominator: 10}, nil)
	if err != nil || !slices.Equal(uids, []uint16{10, 20}) || !slices.Equal(ctfTestRats(t, scores), []string{"7/10", "3/10"}) {
		t.Fatalf("theta split changed: %v %v %v", uids, scores, err)
	}
}

// The treasury production row for every cede case: the provider row is 1/10
// and the two ur-reserve recipients 9/20 each, max-upscaled to u16 with no
// entry above the 32768 cap (CAPTURETHEFLAG.md §2.1, §11.2 item 5).
func TestCaptureTheFlagTreasuryRowsQuantize(t *testing.T) {
	t.Parallel()
	parent := recycleTestInput(t).ParentPolicy
	if parent.Steering.MaxWeightLimitU16 != 32768 {
		t.Fatalf("signed cap %d", parent.Steering.MaxWeightLimitU16)
	}
	one := func(uid uint16, score int64) ExactWeightInput {
		return ExactWeightInput{UID: uid, Score: big.NewRat(score, 1)}
	}
	owners := map[uint16]bool{90: true, 91: true}
	for _, test := range []struct {
		name               string
		ctf                bool
		pools, ctfs, heads []ExactWeightInput
		uids               []uint16
		values             []uint16
	}{
		{name: "today's row before E_c", pools: []ExactWeightInput{one(10, 1)},
			uids: []uint16{10, 90, 91}, values: []uint16{14563, 65535, 65535}},
		{name: "no head fleet: pool 9/100, CTF 1/100", ctf: true, pools: []ExactWeightInput{one(10, 1)}, ctfs: []ExactWeightInput{one(11, 1)},
			uids: []uint16{10, 11, 90, 91}, values: []uint16{13107, 1456, 65535, 65535}},
		{name: "head fleet weighted: pool 4/100, CTF 1/100, head 5/100", ctf: true, pools: []ExactWeightInput{one(10, 1)}, ctfs: []ExactWeightInput{one(11, 1)}, heads: []ExactWeightInput{one(20, 1)},
			uids: []uint16{10, 11, 20, 90, 91}, values: []uint16{5825, 1456, 7282, 65535, 65535}},
		{name: "empty CTF channel with a head fleet: pool and head 4 : 5", ctf: true, pools: []ExactWeightInput{one(10, 1)}, heads: []ExactWeightInput{one(20, 1)},
			uids: []uint16{10, 20, 90, 91}, values: []uint16{6473, 8091, 65535, 65535}},
		{name: "empty CTF channel and no head fleet", ctf: true, pools: []ExactWeightInput{one(10, 1)},
			uids: []uint16{10, 90, 91}, values: []uint16{14563, 65535, 65535}},
	} {
		var uids []uint16
		var scores []*big.Rat
		var err error
		if test.ctf {
			uids, scores, err = BuildWeightVectorCtf(test.pools, test.ctfs, test.heads, ctfTestShares, nil)
		} else {
			uids, scores, err = BuildWeightVectorExact(test.pools, test.heads, parent.Steering.Theta, nil)
		}
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		preview := &OwnerRecyclePreview{OwnerUids: []uint16{90, 91}}
		if err := completeOwnerRecycleRow(preview, parent, uids, scores, owners, false); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if !slices.Equal(preview.Uids, test.uids) || !slices.Equal(preview.WireValues, test.values) {
			t.Fatalf("%s: row %v %v, want %v %v", test.name, preview.Uids, preview.WireValues, test.uids, test.values)
		}
		var sum uint64
		for _, value := range preview.WireValues {
			sum += uint64(value)
		}
		for _, value := range preview.WireValues {
			if uint64(value)*crv4.U16Max > sum*uint64(parent.Steering.MaxWeightLimitU16) {
				t.Fatalf("%s: %d exceeds the cap of %d", test.name, value, sum)
			}
		}
		capped, err := crv4.ApplyMaxWeightLimitRational(preview.Scores, parent.Steering.MaxWeightLimitU16)
		if err != nil {
			t.Fatal(err)
		}
		wireUIDs, values, err := crv4.NormalizeRationalToU16(preview.Uids, capped)
		if err != nil || !slices.Equal(wireUIDs, preview.Uids) || !slices.Equal(values, preview.WireValues) {
			t.Fatalf("%s: the production cap changed the row: %v %v", test.name, values, err)
		}
		if provider := new(big.Rat).Sub(preview.WireProviderShare, preview.ProviderShareError); provider.Cmp(big.NewRat(1, 10)) != 0 {
			t.Fatalf("%s: provider share target %s", test.name, provider)
		}
	}
}

// ctfTestArtifact is the least artifact capture the flag is judged against:
// operators 9 and 10 active at UIDs 109 and 110 in settlement epoch 42.
func ctfTestArtifact() *ReleaseMeasurementArtifact {
	return &ReleaseMeasurementArtifact{ChainID: depositPriceTestChainID, Netuid: 521, SettlementEpoch: 42, Pools: []ReleasePoolMeasurement{
		{NoID: 9, UID: 109, PoolHotkey: releaseHex32([32]byte{0x61, 9})}, {NoID: 10, UID: 110, PoolHotkey: releaseHex32([32]byte{0x61, 10})},
	}}
}

func ctfTestPools() []ReleasePoolMeasurement {
	return []ReleasePoolMeasurement{{NoID: 9, UID: 209, PoolHotkey: releaseHex32([32]byte{0x62, 9})}, {NoID: 10, UID: 210, PoolHotkey: releaseHex32([32]byte{0x62, 10})}}
}

// Each operator's signed entry from epoch 8, and both CTF pools, supported.
func ctfTestBlock(t testing.TB) *ReleaseCaptureTheFlag {
	t.Helper()
	return &ReleaseCaptureTheFlag{Support: true, Pools: ctfTestPools(), Entries: []ReleaseAmendmentEntry{
		ctfTestWire(t, ctfTestEntry(t, depositPriceTestChainID, 521, 9, 8, depositPriceTestKey)),
		ctfTestWire(t, ctfTestEntry(t, depositPriceTestChainID, 521, 10, 8, depositPriceTestKey)),
	}}
}

// A carried block is authenticated in full: version 2 lines for the deployment,
// in force, signed by the pinned signer or configured, one per active operator,
// and CTF pools only for operators with an entry, at UIDs of their own, and
// only with support.
func TestVerifiedReleaseCaptureTheFlag(t *testing.T) {
	t.Parallel()
	other, err := crypto.HexToECDSA(strings.Repeat("b2", 32))
	if err != nil {
		t.Fatal(err)
	}
	artifact := ctfTestArtifact()
	if decision, err := verifiedReleaseCaptureTheFlag(artifact); err != nil || decision != nil {
		t.Fatalf("no capture the flag: %v %v", decision, err)
	}
	artifact.CaptureTheFlag = ctfTestBlock(t)
	decision, err := verifiedReleaseCaptureTheFlag(artifact)
	if err != nil || decision.shares != ctfTestShares || len(decision.entries) != 2 || decision.entries[10].FromEpoch != 8 || !reflect.DeepEqual(decision.pools[9], ctfTestPools()[0]) {
		t.Fatalf("signed capture the flag: %+v %v", decision, err)
	}
	configured := ctfTestBlock(t)
	configured.Entries[1] = ctfTestWire(t, ctfTestEntry(t, depositPriceTestChainID, 521, 10, 8, nil))
	if configured.Entries[1].Signature != "" {
		t.Fatal("a configured line carried a signature")
	}
	artifact.CaptureTheFlag = configured
	if decision, err := verifiedReleaseCaptureTheFlag(artifact); err != nil || decision.entries[10].Signature != nil {
		t.Fatalf("configured line: %v", err)
	}
	signed := func(noID, from uint64, key *ecdsa.PrivateKey) ReleaseAmendmentEntry {
		return ctfTestWire(t, ctfTestEntry(t, depositPriceTestChainID, 521, noID, from, key))
	}
	for _, test := range []struct {
		name   string
		change func(*ReleaseCaptureTheFlag)
	}{
		{name: "an entry from a later epoch", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries[0] = signed(9, 43, depositPriceTestKey) }},
		{name: "another signer", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries[0] = signed(9, 8, other) }},
		{name: "an uppercase signature", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries[0].Signature = strings.ToUpper(ctf.Entries[0].Signature) }},
		{name: "a signature without 0x", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries[0].Signature = ctf.Entries[0].Signature[2:] }},
		{name: "a changed line", change: func(ctf *ReleaseCaptureTheFlag) {
			ctf.Entries[0].Line = strings.Replace(ctf.Entries[0].Line, "from epoch 8", "from epoch 7", 1)
		}},
		{name: "a version 1 line", change: func(ctf *ReleaseCaptureTheFlag) {
			entry := amendment.NewExtenders(depositPriceTestChainID, 521, 9, 8)
			if err := entry.Sign(depositPriceTestKey); err != nil {
				t.Fatal(err)
			}
			ctf.Entries[0] = ctfTestWire(t, entry)
		}},
		{name: "another chain", change: func(ctf *ReleaseCaptureTheFlag) {
			ctf.Entries[0] = ctfTestWire(t, ctfTestEntry(t, depositPriceTestChainID+1, 521, 9, 8, depositPriceTestKey))
		}},
		{name: "an inactive operator", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries[1] = signed(11, 8, depositPriceTestKey) }},
		{name: "entries out of order", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries[0], ctf.Entries[1] = ctf.Entries[1], ctf.Entries[0] }},
		{name: "one operator twice", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries[1] = signed(9, 9, depositPriceTestKey) }},
		{name: "no entry", change: func(ctf *ReleaseCaptureTheFlag) {
			ctf.Entries, ctf.Pools = []ReleaseAmendmentEntry{}, []ReleasePoolMeasurement{}
		}},
		{name: "a pool without an entry", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Entries = ctf.Entries[:1] }},
		{name: "pools out of order", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Pools[0], ctf.Pools[1] = ctf.Pools[1], ctf.Pools[0] }},
		{name: "a provider pool's UID", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Pools[1].UID = 109 }},
		{name: "one UID for both CTF pools", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Pools[1].UID = 209 }},
		{name: "a noncanonical hotkey", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Pools[0].PoolHotkey = strings.ToUpper(ctf.Pools[0].PoolHotkey) }},
		{name: "pools without support", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Support = false }},
		{name: "pools as null", change: func(ctf *ReleaseCaptureTheFlag) { ctf.Pools = nil }},
	} {
		ctf := ctfTestBlock(t)
		test.change(ctf)
		artifact.CaptureTheFlag = ctf
		if decision, err := verifiedReleaseCaptureTheFlag(artifact); err == nil || decision != nil {
			t.Fatalf("%s was admitted", test.name)
		}
	}
	// Off resolves no CTF pool and still applies the entries' split.
	off := ctfTestBlock(t)
	off.Support, off.Pools = false, []ReleasePoolMeasurement{}
	artifact.CaptureTheFlag = off
	if decision, err := verifiedReleaseCaptureTheFlag(artifact); err != nil || decision.shares != ctfTestShares || len(decision.pools) != 0 {
		t.Fatalf("off: %+v %v", decision, err)
	}
	// A deployment with no pinned signer has only configured lines.
	unpinned := ctfTestArtifact()
	unpinned.ChainID = depositPriceTestChainID + 1
	unpinned.CaptureTheFlag = &ReleaseCaptureTheFlag{Support: true, Pools: []ReleasePoolMeasurement{}, Entries: []ReleaseAmendmentEntry{ctfTestWire(t, ctfTestEntry(t, unpinned.ChainID, 521, 9, 8, nil))}}
	if _, err := verifiedReleaseCaptureTheFlag(unpinned); err != nil {
		t.Fatalf("configured line without a pinned signer: %v", err)
	}
	unpinned.CaptureTheFlag.Entries[0] = ctfTestWire(t, ctfTestEntry(t, unpinned.ChainID, 521, 9, 8, depositPriceTestKey))
	if _, err := verifiedReleaseCaptureTheFlag(unpinned); err == nil || !strings.Contains(err.Error(), "no pinned signer") {
		t.Fatalf("a signature without a pinned signer: %v", err)
	}
}

// The CTF channel follows the validator's own pool decisions: an eligible pool
// gives its CTF pool its score, a controlled operator's mask covers both UIDs,
// and a pool without weight or without a CTF pool gives none.
func TestCaptureTheFlagInputsFollowPoolDecisions(t *testing.T) {
	t.Parallel()
	decision := &releaseCtfDecision{shares: ctfTestShares, pools: map[uint64]ReleasePoolMeasurement{9: ctfTestPools()[0], 10: ctfTestPools()[1]}}
	pools := []VerifiedReleasePool{
		{NoID: 8, UID: 108, Eligible: true, Score: big.NewRat(5, 1)},
		{NoID: 9, UID: 109, Eligible: true, Score: big.NewRat(2, 1)},
		{NoID: 10, UID: 110, Score: new(big.Rat)},
	}
	masked := map[uint16]bool{}
	inputs := decision.ctfInputs(pools, masked)
	if len(inputs) != 1 || inputs[0].UID != 209 || inputs[0].Score.Cmp(big.NewRat(2, 1)) != 0 || len(masked) != 0 {
		t.Fatalf("inputs %v masked %v", inputs, masked)
	}
	inputs[0].Score.SetInt64(7)
	if pools[1].Score.Cmp(big.NewRat(2, 1)) != 0 {
		t.Fatal("a CTF input shares its pool's score")
	}
	pools[2] = VerifiedReleasePool{NoID: 10, UID: 110, Controlled: true, Score: new(big.Rat)}
	if inputs := decision.ctfInputs(pools, masked); len(inputs) != 1 || !masked[210] {
		t.Fatalf("a controlled operator's CTF pool was not masked: %v %v", inputs, masked)
	}
	if inputs := (*releaseCtfDecision)(nil).ctfInputs(pools, masked); inputs != nil {
		t.Fatal("no capture the flag made a CTF channel")
	}
}

// ctfTestMeasurement is a genuine two-operator V2 measurement with capture the
// flag: both entries signed, both CTF pools supported and resolved.
func ctfTestMeasurement(t *testing.T, completed int) *releaseMeasurementV2TestFixture {
	t.Helper()
	fixture := newReleaseMeasurementV2TestFixture(t, completed)
	if fixture.artifact.ChainID != depositPriceTestChainID || fixture.artifact.Netuid != 521 || fixture.artifact.SettlementEpoch != 42 {
		t.Fatal("measurement fixture deployment changed")
	}
	fixture.artifact.CaptureTheFlag = ctfTestBlock(t)
	return fixture
}

func ctfTestOptions(t *testing.T, fixture *releaseMeasurementV2TestFixture, resolved []ReleasePoolMeasurement) ReleaseMeasurementV2Options {
	t.Helper()
	options := fixture.options(t)
	options.CtfPools = slices.Clone(resolved)
	if ctf := fixture.artifact.CaptureTheFlag; ctf != nil {
		options.CtfSupport = ctf.Support
	}
	return options
}

func ctfTestChannelSums(decision *VerifiedReleaseMeasurement) (pool, ctf, head *big.Rat) {
	pool, ctf, head = new(big.Rat), new(big.Rat), new(big.Rat)
	for index, uid := range decision.UIDs {
		switch {
		case uid == 109 || uid == 110:
			pool.Add(pool, decision.Scores[index])
		case uid == 209 || uid == 210:
			ctf.Add(ctf, decision.Scores[index])
		default:
			head.Add(head, decision.Scores[index])
		}
	}
	return pool, ctf, head
}

// A sealed V2 measurement with capture the flag replays to the same split:
// pool 4/10, CTF 1/10 and head 5/10 with head fleets weighted, each CTF pool at
// a quarter of its operator's pool weight. Replay is deterministic, and the CTF
// pools must be exactly those resolved at the snapshot that the entries and
// the setting select.
func TestCaptureTheFlagMeasurementV2Replay(t *testing.T) {
	t.Parallel()
	fixture := ctfTestMeasurement(t, 15)
	resolved := append(ctfTestPools(), ReleasePoolMeasurement{NoID: 11, UID: 211, PoolHotkey: releaseHex32([32]byte{0x62, 11})})
	encoded, hash, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, ctfTestOptions(t, fixture, resolved))
	if err != nil || hash != ReleaseMeasurementContentHash(encoded) {
		t.Fatalf("seal: %v", err)
	}
	if !strings.Contains(string(encoded), `"capture_the_flag":{"entries":[{"line":"UR settlement amendment v2: chain 945, sn 521, no 9, from epoch 8, capture the flag`) {
		t.Fatalf("the artifact does not carry its entries: %s", encoded)
	}
	decoded, first, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, ctfTestOptions(t, fixture, resolved))
	if err != nil || !reflect.DeepEqual(decoded, fixture.artifact) {
		t.Fatalf("decode: %v", err)
	}
	_, second, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, ctfTestOptions(t, fixture, resolved))
	if err != nil || !reflect.DeepEqual(first.Decision, second.Decision) {
		t.Fatalf("replay is not deterministic: %v", err)
	}
	decision := first.Decision
	if len(decision.SelectedHead) == 0 || len(decision.Ctf) != 2 {
		t.Fatalf("fixture lost its head fleets or CTF channel: %d %v", len(decision.SelectedHead), decision.Ctf)
	}
	pool, ctf, head := ctfTestChannelSums(decision)
	if pool.Cmp(big.NewRat(4, 10)) != 0 || ctf.Cmp(big.NewRat(1, 10)) != 0 || head.Cmp(big.NewRat(5, 10)) != 0 {
		t.Fatalf("channels pool %s ctf %s head %s", pool, ctf, head)
	}
	scores := map[uint16]*big.Rat{}
	for index, uid := range decision.UIDs {
		scores[uid] = decision.Scores[index]
	}
	for _, noID := range []uint16{9, 10} {
		if ratio := new(big.Rat).Quo(scores[200+noID], scores[100+noID]); ratio.Cmp(big.NewRat(1, 4)) != 0 {
			t.Fatalf("operator %d's CTF pool takes %s of its pool weight", noID, ratio)
		}
	}
	// The resolved census is the independent observation: a CTF pool it lacks,
	// or one at another UID, refuses the artifact.
	for _, wrong := range [][]ReleasePoolMeasurement{ctfTestPools()[:1], {ctfTestPools()[0], {NoID: 10, UID: 211, PoolHotkey: ctfTestPools()[1].PoolHotkey}}, nil} {
		if _, _, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, ctfTestOptions(t, fixture, wrong)); err == nil || !strings.Contains(err.Error(), "differ from the CTF pools resolved") {
			t.Fatalf("CTF pools %v were admitted: %v", wrong, err)
		}
	}
	// A legacy transcript cannot carry capture the flag.
	legacy := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	legacy.Schema = ReleaseMeasurementSchema
	for index := range legacy.Inputs {
		legacy.Inputs[index].AttemptCutV2 = nil
	}
	if _, err := VerifyReleaseMeasurementArtifact(legacy); err == nil || !strings.Contains(err.Error(), "rejects capture the flag") {
		t.Fatalf("legacy measurement admitted capture the flag: %v", err)
	}
}

// Off keeps the entries' split with an empty CTF channel: pool and head take
// the row 4 : 5, and a resolved CTF pool is not weighted. An artifact without
// capture the flag keeps theta and the bytes it always had, whatever resolves.
func TestCaptureTheFlagSettingOffAndAbsence(t *testing.T) {
	t.Parallel()
	fixture := ctfTestMeasurement(t, 15)
	cfg := &ReleaseConfig{Ctf: ReleaseCtfOff}
	entries := []*amendment.Entry{ctfTestEntry(t, depositPriceTestChainID, 521, 9, 8, depositPriceTestKey), ctfTestEntry(t, depositPriceTestChainID, 521, 10, 8, depositPriceTestKey)}
	off, err := newReleaseCaptureTheFlag(cfg, entries, ctfTestPools())
	if err != nil || off.Support || len(off.Pools) != 0 || off.Pools == nil || len(off.Entries) != 2 {
		t.Fatalf("off: %+v %v", off, err)
	}
	fixture.artifact.CaptureTheFlag = off
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, ctfTestOptions(t, fixture, ctfTestPools()))
	if err != nil {
		t.Fatal(err)
	}
	pool, ctf, head := ctfTestChannelSums(result.Decision)
	if pool.Cmp(big.NewRat(4, 9)) != 0 || ctf.Sign() != 0 || head.Cmp(big.NewRat(5, 9)) != 0 || len(result.Decision.Ctf) != 0 {
		t.Fatalf("off: pool %s ctf %s head %s", pool, ctf, head)
	}
	cfg.Ctf = ""
	supported, err := newReleaseCaptureTheFlag(cfg, entries, ctfTestPools())
	if err != nil || !supported.Support || !reflect.DeepEqual(supported, ctfTestBlock(t)) {
		t.Fatalf("an absent setting is not support: %+v %v", supported, err)
	}
	if none, err := newReleaseCaptureTheFlag(cfg, nil, ctfTestPools()); err != nil || none != nil {
		t.Fatalf("no entry in force made capture the flag: %+v %v", none, err)
	}
	plain := newReleaseMeasurementV2TestFixture(t, 15)
	encoded, _, err := SealReleaseMeasurementArtifactV2(t.Context(), plain.artifact, ctfTestOptions(t, plain, ctfTestPools()))
	if err != nil || strings.Contains(string(encoded), "capture_the_flag") {
		t.Fatalf("an artifact without capture the flag changed: %v", err)
	}
	_, result, err = DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, ctfTestOptions(t, plain, ctfTestPools()))
	if err != nil || !reflect.DeepEqual(result.Decision, plain.want) {
		t.Fatalf("theta's decision changed: %v", err)
	}
}

// One deployment's amendments: a signed entry in force from its first epoch,
// for its operator only; nothing before a file was ever verified; the last
// verified file through an outage; and a configured line only where no signed
// entry of its kind is in force.
func TestReleaseAmendmentsReader(t *testing.T) {
	t.Parallel()
	entry := ctfTestEntry(t, depositPriceTestChainID, 521, 9, 8, depositPriceTestKey)
	line, err := entry.Line()
	if err != nil {
		t.Fatal(err)
	}
	file := fmt.Sprintf("sn: 521\nsigned_amendments:\n  - line: %q\n    signature: \"0x%x\"\n", line, entry.Signature)
	var down atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			http.Error(writer, "synthetic outage", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte(file))
	}))
	t.Cleanup(server.Close)
	cfg := &ReleaseConfig{ChainID: depositPriceTestChainID, Netuid: 521, StateDir: t.TempDir()}
	fallbackLine, err := amendment.NewCaptureTheFlag(depositPriceTestChainID, 521, 10, 9, ctfTestCarry).Line()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AmendmentFallback = []string{fallbackLine}
	reader, err := newReleaseAmendments(cfg)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := releaseAmendmentSigner(cfg.ChainID, cfg.Netuid)
	reader.source = amendment.NewSource(server.URL, cfg.ChainID, cfg.Netuid, signer, &releaseAmendmentStore{path: t.TempDir() + "/amendments.yml"})
	at := func(epoch uint64) []string {
		t.Helper()
		entries, err := reader.captureTheFlagAt([]uint64{9, 10}, epoch)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, entry := range entries {
			out = append(out, fmt.Sprintf("%d from %d configured %t", entry.NoID, entry.FromEpoch, entry.Configured()))
		}
		return out
	}
	if got := at(8); got != nil {
		t.Fatalf("an unknown file put %v in force", got)
	}
	reader.refresh(t.Context())
	if got := at(7); got != nil {
		t.Fatalf("epoch 7: %v", got)
	}
	if got, want := at(8), []string{"9 from 8 configured false"}; !slices.Equal(got, want) {
		t.Fatalf("epoch 8: %v, want %v", got, want)
	}
	if got, want := at(9), []string{"9 from 8 configured false", "10 from 9 configured true"}; !slices.Equal(got, want) {
		t.Fatalf("epoch 9: %v, want %v", got, want)
	}
	// Within the cache interval nothing is read; after it an outage keeps the file.
	down.Store(true)
	reader.refresh(t.Context())
	reader.now = func() time.Time { return time.Now().Add(2 * releaseAmendmentCacheTTL) }
	reader.refresh(t.Context())
	if got := at(8); len(got) != 1 {
		t.Fatalf("an outage dropped the verified file: %v", got)
	}
	// Without a pinned signer only configured lines can be in force.
	unpinned := &ReleaseConfig{ChainID: depositPriceTestChainID + 1, Netuid: 521, StateDir: t.TempDir()}
	unpinnedLine, err := amendment.NewCaptureTheFlag(unpinned.ChainID, 521, 9, 8, ctfTestCarry).Line()
	if err != nil {
		t.Fatal(err)
	}
	unpinned.AmendmentFallback = []string{unpinnedLine}
	reader, err = newReleaseAmendments(unpinned)
	if err != nil || reader.source != nil {
		t.Fatalf("unpinned reader: %v", err)
	}
	if entries, err := reader.captureTheFlagAt([]uint64{9, 10}, 8); err != nil || len(entries) != 1 || !entries[0].Configured() {
		t.Fatalf("unpinned: %v %v", entries, err)
	}
	if entries, err := (*releaseAmendments)(nil).captureTheFlagAt([]uint64{9}, 8); err != nil || entries != nil {
		t.Fatal("legacy steering read an amendment")
	}
}

// The setting is support, off or absent, which is support. Configured lines
// must parse for the deployment. Neither changes the hash of a config without
// them.
func TestReleaseCaptureTheFlagConfig(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", ReleaseCtfSupport, ReleaseCtfOff} {
		if err := validateReleaseCtfSetting(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"on", "Support", "OFF", "true"} {
		if err := validateReleaseCtfSetting(value); err == nil {
			t.Fatalf("%q was admitted", value)
		}
	}
	if !releaseCtfSupported(&ReleaseConfig{}) || !releaseCtfSupported(&ReleaseConfig{Ctf: ReleaseCtfSupport}) || releaseCtfSupported(&ReleaseConfig{Ctf: ReleaseCtfOff}) {
		t.Fatal("support is not the default")
	}
	cfg := validReleaseConfig(t)
	if raw, err := json.Marshal(cfg); err != nil || strings.Contains(string(raw), `"ctf"`) || strings.Contains(string(raw), `"amendment_fallback"`) {
		t.Fatalf("a config without the settings changed its hashed bytes: %v", err)
	}
	line, err := amendment.NewCaptureTheFlag(cfg.ChainID, cfg.Netuid, 1, 8, ctfTestCarry).Line()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		lines []string
		ok    bool
	}{
		{name: "a line for this deployment", lines: []string{line}, ok: true},
		{name: "another subnet", lines: []string{strings.Replace(line, fmt.Sprintf("sn %d", cfg.Netuid), fmt.Sprintf("sn %d", cfg.Netuid+1), 1)}},
		{name: "not a line", lines: []string{"capture the flag from epoch 8"}},
		{name: "the same start twice", lines: []string{line, line}},
	} {
		changed := cfg
		changed.AmendmentFallback = test.lines
		if _, err := releaseAmendmentFallback(&changed); (err == nil) != test.ok {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

// ctfSettlementAt's answer for one operator.
type releaseCtfTestIdentity struct {
	vault      common.Address
	hotkey     [32]byte
	firstEpoch uint64
}

// The resolver fixture's CTF pool hotkey, which holds UID 4 once a test puts
// it in the metagraph.
var ctfTestPoolHotkey = chainBatchHotkey(7)

// registerCtfPool is registerCtfPool's outcome: vault2's pool 2^32 + noID and
// STSettlement2's ctfSettlementAt answer.
func (self *settlementV2ResolverTestFixture) registerCtfPool(noID, firstEpoch uint64) {
	poolID, _ := amendment.CtfPoolID(noID)
	self.pools[poolID] = stabi.ISTSettlementVault2Pool{Hotkey: ctfTestPoolHotkey, FirstEpoch: firstEpoch, Registered: true}
	self.ctfIdentities[noID] = releaseCtfTestIdentity{vault: self.addresses.Vault, hotkey: ctfTestPoolHotkey, firstEpoch: firstEpoch}
}

// ctfReplay is the CTF pools the historical decision observation resolves.
func (self *settlementV2ResolverTestFixture) ctfReplay(t *testing.T, addresses SettlementV2Addresses) ([]ReleasePoolMeasurement, error) {
	t.Helper()
	query := self.query
	query.settlementV2, query.ctf = addresses, true
	observed, err := self.chain.readReleaseDecisionChainV2Context(t.Context(), query)
	if err != nil {
		return nil, err
	}
	return observed.ctfPools, nil
}

// ctfLive is the CTF pools live steering resolves at its snapshot.
func (self *settlementV2ResolverTestFixture) ctfLive(t *testing.T, addresses SettlementV2Addresses) ([]ReleasePoolMeasurement, error) {
	t.Helper()
	snapshot := self.snapshot()
	return self.chain.releaseCtfPoolsContext(t.Context(), addresses, snapshot.BlockNumber, snapshot.BlockHash, snapshot.BlockNumber, snapshot.BlockHash, snapshot.Epoch.Uint64(), settlementV2TestV1Pools(), self.hotkeyUIDs())
}

// CTF pool resolution (CAPTURETHEFLAG.md §2.3): live steering and replay
// resolve the same CTF pool from vault2's record and ctfSettlementAt, and leave
// an operator's CTF channel empty for a pool that is unregistered, starts after
// the epoch or was pruned. ctfSettlementAt is read only once vault2 holds the
// pool, and one that differs from vault2 stops the pass.
func TestCaptureTheFlagPoolResolution(t *testing.T) {
	t.Parallel()
	resolvedPool := []ReleasePoolMeasurement{{NoID: 1, UID: 4, PoolHotkey: releaseHex32(ctfTestPoolHotkey)}}
	for _, test := range []struct {
		name     string
		setup    func(*settlementV2ResolverTestFixture)
		want     []ReleasePoolMeasurement
		ctfReads int
		fault    string
	}{
		{name: "unregistered", setup: func(*settlementV2ResolverTestFixture) {}},
		{name: "first epoch in the future", setup: func(fixture *settlementV2ResolverTestFixture) { fixture.registerCtfPool(1, 2) }},
		{name: "registered", setup: func(fixture *settlementV2ResolverTestFixture) { fixture.registerCtfPool(1, 1) }, want: resolvedPool, ctfReads: 2},
		{name: "pruned", setup: func(fixture *settlementV2ResolverTestFixture) {
			fixture.registerCtfPool(1, 1)
			fixture.setMetagraph(chainBatchHotkey(0), chainBatchHotkey(1), chainBatchHotkey(2), settlementV2TestPoolHotkey)
		}, ctfReads: 2},
		{name: "another hotkey", setup: func(fixture *settlementV2ResolverTestFixture) {
			fixture.registerCtfPool(1, 1)
			identity := fixture.ctfIdentities[1]
			identity.hotkey = chainBatchHotkey(8)
			fixture.ctfIdentities[1] = identity
		}, fault: "ctfSettlementAt differs from vault2's immutable CTF pool"},
		{name: "another vault", setup: func(fixture *settlementV2ResolverTestFixture) {
			fixture.registerCtfPool(1, 1)
			identity := fixture.ctfIdentities[1]
			identity.vault = fixture.addresses.Settlement
			fixture.ctfIdentities[1] = identity
		}, fault: "ctfSettlementAt differs"},
		{name: "another first epoch", setup: func(fixture *settlementV2ResolverTestFixture) {
			fixture.registerCtfPool(1, 1)
			identity := fixture.ctfIdentities[1]
			identity.firstEpoch = 0
			fixture.ctfIdentities[1] = identity
		}, fault: "ctfSettlementAt differs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2ResolverTestFixture(t)
			fixture.setMetagraph(chainBatchHotkey(0), chainBatchHotkey(1), chainBatchHotkey(2), settlementV2TestPoolHotkey, ctfTestPoolHotkey)
			test.setup(fixture)
			live, liveErr := fixture.ctfLive(t, fixture.addresses)
			replayed, replayErr := fixture.ctfReplay(t, fixture.addresses)
			if test.fault != "" {
				if liveErr == nil || replayErr == nil || !strings.Contains(liveErr.Error(), test.fault) || !strings.Contains(replayErr.Error(), test.fault) {
					t.Fatalf("live %v and replay %v admitted a redirected CTF pool", liveErr, replayErr)
				}
				return
			}
			if liveErr != nil || replayErr != nil || !slices.Equal(live, test.want) || !slices.Equal(replayed, live) {
				t.Fatalf("live %+v %v and replayed %+v %v, want %+v", live, liveErr, replayed, replayErr, test.want)
			}
			if reads := fixture.v2Reads("ctfSettlementAt"); len(reads) != test.ctfReads {
				t.Fatalf("ctfSettlementAt reads %v, want %d", reads, test.ctfReads)
			}
		})
	}
	// Without settlement v2 nothing is read and no CTF pool exists.
	fixture := newSettlementV2ResolverTestFixture(t)
	fixture.registerCtfPool(1, 1)
	if live, err := fixture.ctfLive(t, SettlementV2Addresses{}); err != nil || live != nil || len(fixture.v2Reads("pools")) != 0 {
		t.Fatalf("no settlement v2: %v %v", live, err)
	}
	// An operator id with no CTF pool id has none.
	if _, found, err := fixture.chain.releaseCtfPoolHotkeyContext(t.Context(), fixture.addresses, 0, [32]byte{}, 0, [32]byte{}, 1<<32, 1); err != nil || found {
		t.Fatalf("operator 2^32 resolved a CTF pool: %v", err)
	}
}

// The treasury production row of a decision with capture the flag, from the
// independently observed census: each CTF pool is a provider of the row,
// checked like a provider pool, and the row is pool 9/100, CTF 1/100 and the
// two ur-reserve recipients 9/20 each. A CTF pool at a recipient's UID, or at a
// UID its hotkey does not hold, refuses the row.
func TestCaptureTheFlagTreasuryMeasuredRow(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, _ := fixture.stage(t)
	artifact := cloneReleaseMeasurementArtifact(t, fixture.operator.measurement.provider.artifact)
	registrations := map[uint16]OwnerRecycleRegistration{}
	for _, registration := range stage.authority.observation.Snapshot.Registrations {
		registrations[registration.Uid] = registration
	}
	recipients := stage.proof.Row.TreasuryUids
	const ctfUID = 50
	if _, spare := registrations[ctfUID]; !spare || slices.Contains(recipients, ctfUID) || artifact.Pools[0].UID != 109 {
		t.Fatal("treasury fixture census changed")
	}
	block := func(uid uint16, hotkey [32]byte) *ReleaseCaptureTheFlag {
		return &ReleaseCaptureTheFlag{Support: true, Entries: []ReleaseAmendmentEntry{ctfTestWire(t, ctfTestEntry(t, artifact.ChainID, artifact.Netuid, 9, 8, nil))},
			Pools: []ReleasePoolMeasurement{{NoID: 9, UID: uid, PoolHotkey: releaseHex32(hotkey)}}}
	}
	masked := map[uint16]bool{artifact.SelfUID: true}
	uids, scores, err := BuildWeightVectorCtf([]ExactWeightInput{{UID: 109, Score: big.NewRat(3, 1)}}, []ExactWeightInput{{UID: ctfUID, Score: big.NewRat(3, 1)}}, nil, ctfTestShares, masked)
	if err != nil {
		t.Fatal(err)
	}
	decision := &VerifiedReleaseMeasurement{UIDs: uids, Scores: scores, MaskedUIDs: []uint16{artifact.SelfUID}}
	artifact.CaptureTheFlag = block(ctfUID, registrations[ctfUID].Hotkey)
	row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, decision)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint16]uint16{recipients[0]: 65535, recipients[1]: 65535, ctfUID: 1456, 109: 13107}
	if len(row.Uids) != len(want) || row.Fallback != "" || !slices.Equal(row.TreasuryUids, recipients) {
		t.Fatalf("row %+v", row)
	}
	for index, uid := range row.Uids {
		if row.Values[index] != want[uid] {
			t.Fatalf("uid %d value %d, want %d: %+v", uid, row.Values[index], want[uid], row)
		}
	}
	for name, ctf := range map[string]*ReleaseCaptureTheFlag{
		"a recipient's UID":         block(recipients[0], registrations[recipients[0]].Hotkey),
		"another hotkey at its UID": block(ctfUID, registrations[ctfUID+1].Hotkey),
	} {
		artifact.CaptureTheFlag = ctf
		if row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, decision); err == nil || len(row.Uids) != 0 {
			t.Fatalf("%s was admitted to the treasury row", name)
		}
	}
}

// A decision's capture the flag is its configuration's: it declares the
// configuration's ctf setting, and stands on an unsigned line only where the
// configuration names that exact line. Replay finds both in the decision's own
// configuration, so neither a changed setting nor a line nobody signed or
// configured can steer a replayed row.
func TestCaptureTheFlagDecisionStandsOnItsConfiguration(t *testing.T) {
	t.Parallel()
	configured := ctfTestWire(t, ctfTestEntry(t, depositPriceTestChainID, 521, 10, 8, nil))
	artifact := ctfTestArtifact()
	artifact.CaptureTheFlag = ctfTestBlock(t)
	artifact.CaptureTheFlag.Entries[1] = configured
	for _, test := range []struct {
		name     string
		support  bool
		fallback []string
		ok       bool
	}{
		{name: "the configuration's setting and line", support: true, fallback: []string{configured.Line}, ok: true},
		{name: "another setting", support: false, fallback: []string{configured.Line}},
		{name: "no configured line", support: true},
		{name: "another configured line", support: true, fallback: []string{strings.Replace(configured.Line, "from epoch 8", "from epoch 9", 1)}},
	} {
		if err := verifyReleaseCtfConfiguration(artifact, test.support, test.fallback); (err == nil) != test.ok {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
	if err := verifyReleaseCtfConfiguration(ctfTestArtifact(), false, nil); err != nil {
		t.Fatalf("a decision without capture the flag: %v", err)
	}
	// The full V2 verifier refuses either before any replay work.
	fixture := ctfTestMeasurement(t, 2)
	fixture.artifact.CaptureTheFlag.Entries[1] = ctfTestWire(t, ctfTestEntry(t, depositPriceTestChainID, 521, 10, 8, nil))
	options := ctfTestOptions(t, fixture, ctfTestPools())
	options.CtfSupport = false
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, options); err == nil || !strings.Contains(err.Error(), "another ctf setting") {
		t.Fatalf("another setting was replayed: %v", err)
	}
	options = ctfTestOptions(t, fixture, ctfTestPools())
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, options); err == nil || !strings.Contains(err.Error(), "unsigned line its configuration does not name") {
		t.Fatalf("an unconfigured line was replayed: %v", err)
	}
	options = ctfTestOptions(t, fixture, ctfTestPools())
	options.AmendmentFallback = []string{fixture.artifact.CaptureTheFlag.Entries[1].Line}
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, options); err != nil {
		t.Fatalf("the configured line was refused: %v", err)
	}
}
