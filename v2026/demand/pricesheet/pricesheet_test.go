package pricesheet

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/protocol"
)

const (
	testChain  uint64 = 964
	testNetuid uint16 = 25
)

// launchSheet is the file published at launch, byte for byte.
const launchSheet = `sn: 25
# the alpha must be deposited as demand deposits per block (7 days)
#
# Initial period: no operator demand deposits are collected, so every rate is
# 0 alpha per GiB and 0 alpha per user. The tiers are kept for the shape of the
# schedule (an operator pays the best tier whose staked-alpha threshold it meets;
# the 0 tier applies to everyone) and will be filled in when deposits start.
tiered_per_block_demand_deposits:
  0:
    alpha_per_gib: 0
    alpha_per_user: 0
  1000:
    alpha_per_gib: 0
    alpha_per_user: 0
`

func testKey(t testing.TB) (*ecdsa.PrivateKey, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key, crypto.PubkeyToAddress(key.PublicKey)
}

func pricedTiers() []protocol.DepositTier {
	return []protocol.DepositTier{
		{MinConvictionRao: 0, RateNumeratorRaoPerGiB: 21_605_000, RateNumeratorRaoPerUser: 5_401_000, RateDenominator: 1},
		{MinConvictionRao: 1_000_000_000_000, RateNumeratorRaoPerGiB: 21_605_000, RateNumeratorRaoPerUser: 5_401_000, RateDenominator: 1},
	}
}

func signEntry(t testing.TB, key *ecdsa.PrivateKey, entry *Entry) {
	t.Helper()
	message, err := entry.Message(testChain, testNetuid)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(textHash(message), key)
	if err != nil {
		t.Fatal(err)
	}
	signature[crypto.RecoveryIDOffset] += 27 // as a hardware wallet returns it
	entry.Signature = signature
}

func sheetFile(t testing.TB, key *ecdsa.PrivateKey, epochs ...uint64) string {
	t.Helper()
	var file strings.Builder
	file.WriteString(`sn: 25
tiered_per_block_demand_deposits:
  0:
    alpha_per_gib: 0.021605
    alpha_per_user: 0.005401
  1000:
    alpha_per_gib: 0.021605
    alpha_per_user: 0.005401
signed_schedule:
`)
	for _, epoch := range epochs {
		entry := Entry{EffectiveEpoch: epoch, EpochCapRao: 5_000_000_000_000, Tiers: pricedTiers()}
		signEntry(t, key, &entry)
		fmt.Fprintf(&file, `  %d:
    epoch_cap_alpha: 5000
    signature: "0x%s"
    tiers:
      0:
        alpha_per_gib: 0.021605
        alpha_per_user: 0.005401
      1000:
        alpha_per_gib: 0.021605
        alpha_per_user: 0.005401
`, epoch, hex.EncodeToString(entry.Signature))
	}
	return file.String()
}

// The launch file parses, carries no signed price, and so leaves the signed policy
// in force.
func TestTheLaunchSheetLeavesThePolicyInForce(t *testing.T) {
	sheet, err := Parse([]byte(launchSheet))
	if err != nil {
		t.Fatal(err)
	}
	if sheet.Netuid != 25 || len(sheet.Entries) != 0 || len(sheet.Mirror) != 2 || sheet.At(1_000_000) != nil {
		t.Fatalf("%+v", sheet)
	}
	_, signer := testKey(t)
	if err := sheet.Verify(testChain, testNetuid, signer); err != nil {
		t.Fatal(err)
	}
	base := protocol.DepositPolicy{EpochCapRaoPerOperator: 10_000_000_000, TotalTestCampaignCapRao: 196_000_000_000, Tiers: []protocol.DepositTier{{RateDenominator: 1}}, ZeroRateAction: protocol.DepositZeroRateEqualDemand}
	effective := Effective(sheet, 9, base)
	if !effective.IsZeroPrice() || effective.EpochCapRaoPerOperator != base.EpochCapRaoPerOperator {
		t.Fatalf("%+v", effective)
	}
}

// An entry binds to its exact prices, in a line a person can read and check.
func TestTheSignedLineNamesThePrices(t *testing.T) {
	entry := Entry{EffectiveEpoch: 9, EpochCapRao: 5_000_000_000_000, Tiers: pricedTiers()}
	message, err := entry.Message(testChain, testNetuid)
	if err != nil {
		t.Fatal(err)
	}
	want := "UR price sheet v1: chain 964, sn 25, from epoch 9, cap 5000 alpha per epoch, tier 0 alpha 0.021605 per GiB 0.005401 per user, tier 1000 alpha 0.021605 per GiB 0.005401 per user"
	if message != want {
		t.Fatalf("got  %q\nwant %q", message, want)
	}
	for _, c := range []byte(message) {
		if c < 0x20 || c > 0x7e {
			t.Fatalf("a hardware wallet could not show byte %x", c)
		}
	}
	key, signer := testKey(t)
	signEntry(t, key, &entry)
	if err := entry.Verify(testChain, testNetuid, signer); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Entry){
		"rate": func(e *Entry) {
			e.Tiers = pricedTiers()
			e.Tiers[0].RateNumeratorRaoPerGiB++
			e.Tiers[1].RateNumeratorRaoPerGiB = e.Tiers[0].RateNumeratorRaoPerGiB
		},
		"user rate": func(e *Entry) { e.Tiers = pricedTiers(); e.Tiers[1].RateNumeratorRaoPerUser-- },
		"threshold": func(e *Entry) { e.Tiers = pricedTiers(); e.Tiers[1].MinConvictionRao++ },
		"cap":       func(e *Entry) { e.EpochCapRao++ },
		"epoch":     func(e *Entry) { e.EffectiveEpoch++ },
	} {
		changed := entry
		change(&changed)
		if err := changed.Verify(testChain, testNetuid, signer); err == nil {
			t.Fatal("a changed entry verified:", name)
		}
	}
	if err := entry.Verify(945, testNetuid, signer); err == nil {
		t.Fatal("an entry verified on another chain")
	}
	if err := entry.Verify(testChain, 26, signer); err == nil {
		t.Fatal("an entry verified on another subnet")
	}
	other, otherSigner := testKey(t)
	_ = other
	if err := entry.Verify(testChain, testNetuid, otherSigner); err == nil {
		t.Fatal("an entry verified under another signer")
	}
}

// A price applies from its effective epoch until a later one takes over.
func TestTheEntryInForce(t *testing.T) {
	key, signer := testKey(t)
	sheet, err := Parse([]byte(sheetFile(t, key, 9, 20)))
	if err != nil {
		t.Fatal(err)
	}
	if err := sheet.Verify(testChain, testNetuid, signer); err != nil {
		t.Fatal(err)
	}
	for epoch, want := range map[uint64]uint64{9: 9, 10: 9, 19: 9, 20: 20, 1_000: 20} {
		if got := sheet.At(epoch); got == nil || got.EffectiveEpoch != want {
			t.Fatalf("epoch %d: %+v", epoch, got)
		}
	}
	if sheet.At(8) != nil {
		t.Fatal("a price applied before its epoch")
	}
	if sheet.Newest().EffectiveEpoch != 20 {
		t.Fatal(sheet.Newest())
	}
}

// The effective policy prices through the shared formula with no precision lost.
func TestEffectivePolicyPricesExactly(t *testing.T) {
	key, _ := testKey(t)
	sheet, err := Parse([]byte(sheetFile(t, key, 9)))
	if err != nil {
		t.Fatal(err)
	}
	base := protocol.DepositPolicy{Unit: protocol.DepositUnitRaoPerGiB, EpochCapRaoPerOperator: 10_000_000_000, TotalTestCampaignCapRao: 196_000_000_000, Tiers: []protocol.DepositTier{{RateDenominator: 1}}, ZeroRateAction: protocol.DepositZeroRateEqualDemand}
	effective := Effective(sheet, 9, base)
	if effective.IsZeroPrice() || effective.ZeroRateAction != "" || effective.Unit != protocol.DepositUnitRaoPerGiBAndUser || effective.EpochCapRaoPerOperator != 5_000_000_000_000 {
		t.Fatalf("%+v", effective)
	}
	// 22,100 GiB and 218,278 users: 477,470.5 + 1,178,919.478 = 1,656.389... alpha.
	usage := uint64(22_100) << 30
	required, tier, err := protocol.RequiredDepositRao(usage, 218_278, big.NewInt(0), effective)
	if err != nil {
		t.Fatal(err)
	}
	want := 22_100*uint64(21_605_000) + 218_278*uint64(5_401_000)
	if required.Uint64() != want || tier.MinConvictionRao != 0 {
		t.Fatal(required, want, tier)
	}
	// A schedule of zeros signed later returns to the zero-price mode.
	zero := Entry{EffectiveEpoch: 30, EpochCapRao: 1, Tiers: []protocol.DepositTier{{RateDenominator: 1}}}
	if policy := zero.DepositPolicy(base); !policy.IsZeroPrice() || policy.ZeroRateAction != protocol.DepositZeroRateEqualDemand {
		t.Fatalf("%+v", policy)
	}
	// Before its epoch the base stays in force, untouched.
	if policy := Effective(sheet, 8, base); !policy.IsZeroPrice() || policy.EpochCapRaoPerOperator != base.EpochCapRaoPerOperator {
		t.Fatalf("%+v", policy)
	}
}

// Decimals convert exactly, including values a float would round, and those beyond
// a rao take a denominator.
func TestExactDecimals(t *testing.T) {
	parse := func(gib, user string) ([]protocol.DepositTier, error) {
		sheet, err := Parse(fmt.Appendf(nil, "sn: 25\ntiered_per_block_demand_deposits:\n  0:\n    alpha_per_gib: %s\n    alpha_per_user: %s\n", gib, user))
		if err != nil {
			return nil, err
		}
		return sheet.Mirror, nil
	}
	// uint64(0.000065*1e9) is 64,999 through a float.
	tiers, err := parse("0.000065", "0.0108025")
	if err != nil {
		t.Fatal(err)
	}
	if tiers[0].RateNumeratorRaoPerGiB != 65_000 || tiers[0].RateNumeratorRaoPerUser != 10_802_500 || tiers[0].RateDenominator != 1 {
		t.Fatalf("%+v", tiers[0])
	}
	tiers, err = parse("0.1234567891", "0.5")
	if err != nil {
		t.Fatal(err)
	}
	if tiers[0].RateNumeratorRaoPerGiB != 1_234_567_891 || tiers[0].RateNumeratorRaoPerUser != 5_000_000_000 || tiers[0].RateDenominator != 10 {
		t.Fatalf("%+v", tiers[0])
	}
	entry := Entry{EffectiveEpoch: 1, EpochCapRao: 1, Tiers: tiers}
	message, err := entry.Message(testChain, testNetuid)
	if err != nil || !strings.Contains(message, "0.1234567891 per GiB 0.5 per user") {
		t.Fatal(message, err)
	}
}

func TestRefusedFiles(t *testing.T) {
	key, _ := testKey(t)
	good := sheetFile(t, key, 9)
	if _, err := Parse([]byte(good)); err != nil {
		t.Fatal(err)
	}
	tier := func(value string) string {
		return "sn: 25\ntiered_per_block_demand_deposits:\n  0:\n    alpha_per_gib: " + value + "\n    alpha_per_user: 0\n"
	}
	for name, file := range map[string]string{
		"not yaml":            "sn: [",
		"no sn":               "tiered_per_block_demand_deposits: {}\n",
		"sn is text":          "sn: abc\n",
		"two documents":       "sn: 25\n---\nsn: 25\n",
		"quoted number":       tier(`"0.1"`),
		"underscore":          tier("1_000"),
		"exponent":            tier("1e3"),
		"hex":                 tier("0x3E8"),
		"leading plus":        tier("+1"),
		"leading dot":         tier(".5"),
		"negative":            tier("-1"),
		"null":                tier("null"),
		"missing rate":        "sn: 25\ntiered_per_block_demand_deposits:\n  0:\n    alpha_per_gib: 0\n",
		"typo in rate":        "sn: 25\ntiered_per_block_demand_deposits:\n  0:\n    alpha_per_gb: 0\n    alpha_per_user: 0\n",
		"repeated rate":       "sn: 25\ntiered_per_block_demand_deposits:\n  0:\n    alpha_per_gib: 0\n    alpha_per_gib: 1\n    alpha_per_user: 0\n",
		"unknown entry key":   strings.Replace(good, "epoch_cap_alpha: 5000", "epoch_cap_alpha: 5000\n    surprise: 1", 1),
		"no cap":              strings.Replace(good, "    epoch_cap_alpha: 5000\n", "", 1),
		"bad signature hex":   strings.Replace(good, `signature: "0x`, `signature: "zz`, 1),
		"rates rising":        strings.Replace(good, "      1000:\n        alpha_per_gib: 0.021605\n", "      1000:\n        alpha_per_gib: 0.03\n", 1),
		"partly priced":       strings.Replace(good, "      1000:\n        alpha_per_gib: 0.021605\n", "      1000:\n        alpha_per_gib: 0\n", 1),
		"tiers out of order":  "sn: 25\nsigned_schedule:\n  5:\n    epoch_cap_alpha: 1\n    signature: \"0x00\"\n    tiers:\n      10:\n        alpha_per_gib: 0\n        alpha_per_user: 0\n      0:\n        alpha_per_gib: 0\n        alpha_per_user: 0\n",
		"epochs out of order": "sn: 25\nsigned_schedule:\n  5:\n    epoch_cap_alpha: 1\n    signature: \"0x00\"\n    tiers:\n      0:\n        alpha_per_gib: 0\n        alpha_per_user: 0\n  4:\n    epoch_cap_alpha: 1\n    signature: \"0x00\"\n    tiers:\n      0:\n        alpha_per_gib: 0\n        alpha_per_user: 0\n",
	} {
		if _, err := Parse([]byte(file)); err == nil {
			t.Fatal("parsed", name)
		}
	}
	if _, err := Parse(make([]byte, maxSheetBytes+1)); err == nil {
		t.Fatal("parsed an oversized file")
	}
}

type memoryStore struct {
	raw     []byte
	loadErr error
	saveErr error
	saves   int
}

func (m *memoryStore) Load(context.Context) ([]byte, error) { return m.raw, m.loadErr }

func (m *memoryStore) Save(_ context.Context, raw []byte) error {
	m.saves++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.raw = raw
	return nil
}

// Only a verified file is taken; anything else leaves the last good one in force.
func TestSourceKeepsTheLastGoodSheet(t *testing.T) {
	ctx := context.Background()
	key, signer := testKey(t)
	attacker, _ := testKey(t)
	status, body := http.StatusOK, sheetFile(t, key, 9)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	store := &memoryStore{}
	source := NewSource(server.URL, testChain, testNetuid, signer, store)
	sheet, problem := source.Refresh(ctx)
	if problem != nil || sheet.At(9) == nil || len(store.raw) == 0 {
		t.Fatal(sheet, problem)
	}
	stay := func(name string) {
		t.Helper()
		sheet, problem := source.Refresh(ctx)
		if problem == nil {
			t.Fatal("no problem reported:", name)
		}
		if sheet == nil || sheet.At(9) == nil || sheet.Newest().EffectiveEpoch != 9 {
			t.Fatalf("%s changed the sheet in force: %+v", name, sheet)
		}
	}
	status = http.StatusInternalServerError
	stay("an outage")
	status, body = http.StatusOK, "<html>not yaml: ["
	stay("an error page")
	body = sheetFile(t, attacker, 9, 10)
	stay("a forged sheet")
	body = launchSheet
	stay("a rollback that drops a signed entry")
	body = sheetFile(t, key, 3, 9)
	stay("a well-signed entry added before one the reader holds")
	body = strings.Replace(launchSheet, "sn: 25", "sn: 26", 1)
	stay("a sheet for another subnet")
	// A newer signed entry is news, and survives a restart.
	body = sheetFile(t, key, 9, 20)
	sheet, problem = source.Refresh(ctx)
	if problem != nil || sheet.Newest().EffectiveEpoch != 20 {
		t.Fatal(sheet, problem)
	}
	status = http.StatusBadGateway
	restarted := NewSource(server.URL, testChain, testNetuid, signer, store)
	sheet, problem = restarted.Refresh(ctx)
	if problem == nil || sheet == nil || sheet.Newest().EffectiveEpoch != 20 {
		t.Fatal(sheet, problem)
	}
}

// A reader takes only entries after the ones it holds. An entry added among them, even
// well signed, would reprice epochs that reader has already decided.
func TestOnlyALaterEntryExtendsASheet(t *testing.T) {
	key, _ := testKey(t)
	parse := func(epochs ...uint64) *Sheet {
		t.Helper()
		sheet, err := Parse([]byte(sheetFile(t, key, epochs...)))
		if err != nil {
			t.Fatal(err)
		}
		return sheet
	}
	last := parse(9, 20)
	changed := parse(9, 20)
	changed.Entries[1].EpochCapRao++
	for name, c := range map[string]struct {
		next *Sheet
		want bool
	}{
		"the same sheet":        {parse(9, 20), true},
		"a later entry":         {parse(9, 20, 21), true},
		"an entry between":      {parse(9, 15, 20), false},
		"an entry before all":   {parse(3, 9, 20), false},
		"the newest dropped":    {parse(9), false},
		"the oldest dropped":    {parse(20), false},
		"an entry that changed": {changed, false},
	} {
		if got := Extends(c.next, last); got != c.want {
			t.Errorf("%s: Extends = %t, want %t", name, got, c.want)
		}
	}
	if !Extends(parse(9), nil) || !Extends(parse(9), &Sheet{}) {
		t.Error("a reader holding no entries takes any verified sheet")
	}
}

// With nothing ever verified and the file unavailable, the price is unknown rather
// than assumed.
func TestSourceWithNothingVerified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, signer := testKey(t)
	sheet, problem := NewSource(server.URL, testChain, testNetuid, signer, &memoryStore{}).Refresh(context.Background())
	if sheet != nil || problem == nil {
		t.Fatal(sheet, problem)
	}
}

// A stored copy that is corrupt, or signed by someone else, does not stop the fetch.
// The verified file replaces it, with the stored copy's fault reported once.
func TestACorruptStoredSheetIsReplacedByAVerifiedOne(t *testing.T) {
	ctx := context.Background()
	key, signer := testKey(t)
	attacker, _ := testKey(t)
	up := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !up {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, sheetFile(t, key, 9))
	}))
	defer server.Close()
	for name, stored := range map[string]string{
		"not yaml":     "sn: [",
		"forged":       sheetFile(t, attacker, 9),
		"another sn":   strings.Replace(launchSheet, "sn: 25", "sn: 26", 1),
		"another type": "{}",
	} {
		store := &memoryStore{raw: []byte(stored)}
		source := NewSource(server.URL, testChain, testNetuid, signer, store)
		up = false
		if sheet, problem := source.Refresh(ctx); sheet != nil || problem == nil {
			t.Fatalf("%s, site down: %v %v", name, sheet, problem)
		}
		up = true
		sheet, problem := source.Refresh(ctx)
		if sheet == nil || sheet.At(9) == nil || problem == nil || !strings.Contains(problem.Error(), "stored sheet") {
			t.Fatalf("%s: %v %v", name, sheet, problem)
		}
		if string(store.raw) != sheetFile(t, key, 9) {
			t.Fatalf("%s: the corrupt copy was not replaced", name)
		}
		if sheet, problem = source.Refresh(ctx); sheet == nil || problem != nil {
			t.Fatalf("%s, after the repair: %v %v", name, sheet, problem)
		}
	}
}

// A store that cannot be read may still hold a good copy. The verified file is used
// this time, but nothing overwrites the copy the store may still hold.
func TestAnUnreadableStoreIsNotOverwritten(t *testing.T) {
	key, signer := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, sheetFile(t, key, 9))
	}))
	defer server.Close()
	store := &memoryStore{raw: []byte("held"), loadErr: errors.New("connection reset")}
	sheet, problem := NewSource(server.URL, testChain, testNetuid, signer, store).Refresh(context.Background())
	if sheet == nil || sheet.At(9) == nil || problem == nil || !strings.Contains(problem.Error(), "connection reset") {
		t.Fatal(sheet, problem)
	}
	if store.saves != 0 || string(store.raw) != "held" {
		t.Fatalf("saved %d times over an unreadable store: %q", store.saves, store.raw)
	}
}

// A redirect leaves the published address and is not followed.
func TestSourceDoesNotFollowRedirects(t *testing.T) {
	key, signer := testKey(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, sheetFile(t, key, 9))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()
	sheet, problem := NewSource(server.URL, testChain, testNetuid, signer, nil).Refresh(context.Background())
	if sheet != nil || problem == nil {
		t.Fatal(sheet, problem)
	}
}

func TestSaveFailureStillUsesTheVerifiedSheet(t *testing.T) {
	key, signer := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, sheetFile(t, key, 9))
	}))
	defer server.Close()
	store := &memoryStore{saveErr: errors.New("disk full")}
	sheet, problem := NewSource(server.URL, testChain, testNetuid, signer, store).Refresh(context.Background())
	if sheet == nil || sheet.At(9) == nil || problem == nil {
		t.Fatal(sheet, problem)
	}
}

func TestMainnetSignerIsPinned(t *testing.T) {
	signer, ok := SignerFor(964, 25)
	if !ok || signer != common.HexToAddress("0x16C372dbBb24cd8473345ab13971E40814C8658F") {
		t.Fatal(signer, ok)
	}
	if _, ok := SignerFor(945, 25); ok {
		t.Fatal("testnet has a pinned signer")
	}
	if _, ok := SignerFor(964, 26); ok {
		t.Fatal("another subnet has a pinned signer")
	}
}
