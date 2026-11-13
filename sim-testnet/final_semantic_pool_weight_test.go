// Pool weight tests work every expected value by hand from WHITEPAPER 8.1,
// never from the code under test:
//
//	owed           = bytes*rate_gib(tier)/GiB + users*rate_user(tier)
//	deposit        = min(floor(owed), cap)
//	implied_demand = (bytes*rate_gib(0)/GiB + users*rate_user(0)) * min(1, cap/owed)
//	raw_score      = implied_demand * clamp(Q)
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

const (
	finalPoolWeightTestGiB   = uint64(1) << 30
	finalPoolWeightTestEpoch = uint64(10)
	// The formula every row carried before WHITEPAPER 8.1 priced audited usage.
	finalPoolWeightTestFormerFormula = "floor(usage_bytes*rate_numerator/(2^30*rate_denominator)); implied_usage=deposit/rate; raw_score=implied_usage*q"
)

// finalPoolWeightTestRow is the release fixture's compliant row for settlement
// epoch 10 under the testnet schedule, with a complete payout and deposit
// lineage. 1 GiB and no users at the baseline tier of 1,000,000 rao per GiB
// owe 1,000,000 rao, far under the 10 alpha cap, and imply a demand of
// 1,000,000 rao; quality 500,000 ppm clamps to 750,000 ppm, so q is 3/4 and
// the raw score is 1,000,000 * 3/4 = 750,000.
func finalPoolWeightTestRow() (FinalSemanticEvidence, FinalPoolWeightEvidence) {
	contentHash := bytesSHA256([]byte("pool weight payout"))
	return FinalSemanticEvidence{Window: ScenarioAcceptanceWindow{TerminalBlock: 1_000}}, FinalPoolWeightEvidence{
		NoID: 1, UID: 11, SourceEpoch: finalPoolWeightTestEpoch - 1, UsageBytes: finalPoolWeightTestGiB,
		ConvictionBeforeRao: "1000", RateNumeratorRaoPerGiB: 1_000_000, RateDenominator: 1,
		BaselineRateNumeratorRaoPerGiB: 1_000_000, BaselineRateDenominator: 1,
		EpochDepositCapRao: "10000000000", RequiredDepositRao: "1000000", ObservedDepositRao: "1000000",
		QualityPPM: 500_000, QualityFactor: FinalRational{Numerator: "3", Denominator: "4"},
		ImpliedDemand: FinalRational{Numerator: "1000000", Denominator: "1"}, RawScore: FinalRational{Numerator: "750000", Denominator: "1"},
		Formula: finalDepositFormula, AuditStatus: validatorpkg.DepositAuditCompliant, AuditCompliant: true, AuditDisposition: "pool_weight_eligible",
		ArtifactContentHash: contentHash, ArtifactHash: "0x" + strings.TrimPrefix(contentHash, "sha256:"), PayoutRoot: finalTestHex(0x21),
		ArtifactSigner: "0x1111111111111111111111111111111111111111", RootCommitter: "0x2222222222222222222222222222222222222222", RootSigner: "0x2222222222222222222222222222222222222222",
		SourceStartBlock: 70, SourceStartHash: finalTestHex(70), SourceEndBlock: 79, SourceEndHash: finalTestHex(79),
		RootCommitBlock: 80, ObservedAtBlock: 101, ArtifactDeadlineBlock: 109,
		PayoutArtifact: FinalArtifactLocator{Kind: "payout-artifact", URI: "final-derived/payout-9-1.json", ContentHash: contentHash, SizeBytes: 18},
		DepositReceipt: FinalEVMReceipt{
			TransactionHash: finalTestHex(0x31), Block: ChainHead{Number: 90, Hash: finalTestHex(90)}, Status: "success", LogsHash: finalTestHex(0x32),
			Proof: FinalArtifactLocator{Kind: "evm-receipt", URI: "final-derived/deposit-1.json", ContentHash: bytesSHA256([]byte("deposit-1")), SizeBytes: 9},
		},
		AppliedWeight: 1,
	}
}

func finalPoolWeightTestRational(numerator, denominator string) FinalRational {
	return FinalRational{Numerator: numerator, Denominator: denominator}
}

// verifyFinalPoolWeightTestRow verifies a row for the test epoch under the
// testnet quality clamp of [750,000, 1,000,000] ppm.
func verifyFinalPoolWeightTestRow(evidence *FinalSemanticEvidence, row *FinalPoolWeightEvidence) (*big.Rat, bool, error) {
	return verifyFinalPoolWeight(evidence, finalPoolWeightTestEpoch, 750_000, 1_000_000, row)
}

// finalPoolWeightTestRefusal requires exactly one refusal text.
func finalPoolWeightTestRefusal(t *testing.T, name string, row FinalPoolWeightEvidence, want string) {
	t.Helper()
	evidence, _ := finalPoolWeightTestRow()
	score, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row)
	if err == nil || err.Error() != want || score != nil || eligible {
		t.Errorf("%s: score=%v eligible=%t error=%v, want exactly %q", name, score, eligible, err, want)
	}
}

// The verifier prices the audited bytes and users at the conviction-zero tier,
// truncates the demand as the cap truncated the deposit, and applies clamped
// quality. Every case states its arithmetic; none is copied from an output.
func TestFinalSemanticPoolWeightPricesAuditedUsageAtTheConvictionZeroTier(t *testing.T) {
	for _, test := range []struct {
		name                       string
		bytes, users               uint64
		gib, user, denominator     uint64
		baseGiB, baseUser, baseDen uint64
		capRao, required           string
		qualityPPM                 uint32
		q, demand, score           FinalRational
	}{
		{
			// The fixture row: 1 GiB * 1,000,000 = 1,000,000 owed and implied; * 3/4.
			name: "baseline tier", bytes: finalPoolWeightTestGiB, gib: 1_000_000, denominator: 1, baseGiB: 1_000_000, baseDen: 1,
			capRao: "10000000000", required: "1000000", qualityPPM: 500_000,
			q: finalPoolWeightTestRational("3", "4"), demand: finalPoolWeightTestRational("1000000", "1"), score: finalPoolWeightTestRational("750000", "1"),
		},
		{
			// A conviction discount: 1 GiB owes 800,000 at its own tier and still
			// implies 1,000,000 at the baseline, so it scores as the row above.
			name: "discounted tier pays less and weighs the same", bytes: finalPoolWeightTestGiB, gib: 800_000, denominator: 1, baseGiB: 1_000_000, baseDen: 1,
			capRao: "10000000000", required: "800000", qualityPPM: 500_000,
			q: finalPoolWeightTestRational("3", "4"), demand: finalPoolWeightTestRational("1000000", "1"), score: finalPoolWeightTestRational("750000", "1"),
		},
		{
			// 3.5 GiB and 7 users: owed 3.5*800,000 + 7*8 = 2,800,056; implied
			// 3.5*1,000,000 + 7*10 = 3,500,070; * 9/10 = 3,150,063.
			name: "bytes and users", bytes: 3*finalPoolWeightTestGiB + finalPoolWeightTestGiB/2, users: 7, gib: 800_000, user: 8, denominator: 1, baseGiB: 1_000_000, baseUser: 10, baseDen: 1,
			capRao: "10000000000", required: "2800056", qualityPPM: 900_000,
			q: finalPoolWeightTestRational("9", "10"), demand: finalPoolWeightTestRational("3500070", "1"), score: finalPoolWeightTestRational("3150063", "1"),
		},
		{
			// 2^30 + 12,345 bytes owe 1,000,011.497..., floored to 1,000,011. The
			// demand keeps the fraction: 1,073,754,169 * 1,000,000 / 2^30 reduces
			// by 64 to 16,777,408,890,625 / 16,777,216; * 3/4 is
			// 50,332,226,671,875 / 67,108,864.
			name: "demand keeps the fraction the deposit floors away", bytes: finalPoolWeightTestGiB + 12_345, gib: 1_000_000, denominator: 1, baseGiB: 1_000_000, baseDen: 1,
			capRao: "10000000000", required: "1000011", qualityPPM: 500_000,
			q: finalPoolWeightTestRational("3", "4"), demand: finalPoolWeightTestRational("16777408890625", "16777216"), score: finalPoolWeightTestRational("50332226671875", "67108864"),
		},
		{
			// 20,000 GiB owe 16e9 at 800,000, above the 1e10 cap. The deposit is
			// the cap and the demand 20,000 * 1,000,000 * 1e10/16e9 = 12.5e9.
			name: "cap truncates demand as it truncated the deposit", bytes: 20_000 * finalPoolWeightTestGiB, gib: 800_000, denominator: 1, baseGiB: 1_000_000, baseDen: 1,
			capRao: "10000000000", required: "10000000000", qualityPPM: 1_000_000,
			q: finalPoolWeightTestRational("1", "1"), demand: finalPoolWeightTestRational("12500000000", "1"), score: finalPoolWeightTestRational("12500000000", "1"),
		},
		{
			// Shared denominators: 8 GiB and 4 users owe 8*3/4 + 4*5/4 = 11, above
			// a cap of 10; the baseline prices 8*5/6 + 4*8/6 = 12, truncated to
			// 12 * 10/11 = 120/11; * 4/5 = 96/11.
			name: "both components over shared denominators under the cap", bytes: 8 * finalPoolWeightTestGiB, users: 4, gib: 3, user: 5, denominator: 4, baseGiB: 5, baseUser: 8, baseDen: 6,
			capRao: "10", required: "10", qualityPPM: 800_000,
			q: finalPoolWeightTestRational("4", "5"), demand: finalPoolWeightTestRational("120", "11"), score: finalPoolWeightTestRational("96", "11"),
		},
	} {
		evidence, row := finalPoolWeightTestRow()
		row.UsageBytes, row.Users = test.bytes, test.users
		row.RateNumeratorRaoPerGiB, row.RateNumeratorRaoPerUser, row.RateDenominator = test.gib, test.user, test.denominator
		row.BaselineRateNumeratorRaoPerGiB, row.BaselineRateNumeratorRaoPerUser, row.BaselineRateDenominator = test.baseGiB, test.baseUser, test.baseDen
		row.EpochDepositCapRao, row.RequiredDepositRao, row.ObservedDepositRao = test.capRao, test.required, test.required
		row.QualityPPM, row.QualityFactor, row.ImpliedDemand, row.RawScore = test.qualityPPM, test.q, test.demand, test.score
		want, ok := new(big.Rat).SetString(test.score.Numerator + "/" + test.score.Denominator)
		if !ok {
			t.Fatalf("%s: worked score is not a rational", test.name)
		}
		score, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row)
		if err != nil || !eligible || score == nil || score.Cmp(want) != 0 {
			t.Errorf("%s: score=%v eligible=%t error=%v, want %s", test.name, score, eligible, err, want.RatString())
		}
	}
}

// A row names the formula its numbers follow. One sealed under deposit/rate,
// or naming none, is refused before any arithmetic.
func TestFinalSemanticPoolWeightRefusesAnotherFormula(t *testing.T) {
	for _, formula := range []string{finalPoolWeightTestFormerFormula, ""} {
		_, row := finalPoolWeightTestRow()
		row.Formula = formula
		finalPoolWeightTestRefusal(t, "formula "+formula, row, "pool weight formula is not the current implied-demand formula")
	}
}

// The strict decoder that reads a sealed object refuses the former row at its
// implied_usage_gib field; a lenient reader reaches the formula refusal.
func TestFinalSemanticPoolWeightRefusesRowSealedUnderDepositOverRate(t *testing.T) {
	former := []byte(`{"no_id":1,"uid":11,"source_epoch":9,"usage_bytes":1073741824,"conviction_before_rao":"1000","rate_numerator_rao_per_gib":1000000,"rate_denominator":1,"epoch_deposit_cap_rao":"10000000000","required_deposit_rao":"1000000","observed_deposit_rao":"1000000","quality_ppm":500000,"q":{"numerator":"3","denominator":"4"},"implied_usage_gib":{"numerator":"1","denominator":"1"},"raw_score":{"numerator":"3","denominator":"4"},"formula":"` + finalPoolWeightTestFormerFormula + `","audit_status":"compliant","audit_compliant":true,"audit_disposition":"pool_weight_eligible"}`)
	sealed := []byte(`{"validators":[{"crv4_cycles":[{"pools":[` + string(former) + `]}]}]}`)
	const wantDecode = `decode signed final semantic evidence: json: unknown field "implied_usage_gib"`
	if _, err := validateFinalSemanticOutputFiles(map[string][]byte{finalSemanticEvidenceFilename: sealed}); err == nil || err.Error() != wantDecode {
		t.Fatalf("sealed former row error=%v, want exactly %q", err, wantDecode)
	}
	var row FinalPoolWeightEvidence
	if err := json.Unmarshal(former, &row); err != nil {
		t.Fatal(err)
	}
	if row.ImpliedDemand != (FinalRational{}) || row.BaselineRateNumeratorRaoPerGiB != 0 || row.BaselineRateDenominator != 0 {
		t.Fatalf("former row gained an implied demand or baseline rate: %+v", row)
	}
	finalPoolWeightTestRefusal(t, "former row", row, "pool weight formula is not the current implied-demand formula")
}

// WHITEPAPER 8.1 gives every pool an implied demand of exactly 1 while the
// price is zero. The release campaign does not admit that mode: its audit
// names no payout artifact, signer, boundary or usage for a row to bind.
func TestFinalSemanticPoolWeightRefusesZeroPriceAudit(t *testing.T) {
	const want = "zero-price deposit audit is not admitted: the release campaign binds every pool weight to an audited deposit"
	audit := validatorpkg.ZeroPriceDepositAudit(finalPoolWeightTestEpoch, finalPoolWeightTestEpoch-1, 1, big.NewInt(0), big.NewInt(1_000))
	audit.ObservedAtBlock, audit.ArtifactDeadlineBlock = 101, 109
	_, priced := finalPoolWeightTestRow()
	// The row a zero-price audit projects to: implied demand 1, raw score 1 * 3/4.
	row := FinalPoolWeightEvidence{
		NoID: audit.NoID, UID: priced.UID, SourceEpoch: audit.SourceEpoch, ConvictionBeforeRao: audit.ConvictionBeforeRao,
		BaselineRateDenominator: 1, EpochDepositCapRao: priced.EpochDepositCapRao, RequiredDepositRao: audit.RequiredDepositRao, ObservedDepositRao: audit.ObservedDepositRao,
		QualityPPM: 500_000, QualityFactor: finalPoolWeightTestRational("3", "4"), ImpliedDemand: finalPoolWeightTestRational("1", "1"), RawScore: finalPoolWeightTestRational("3", "4"),
		Formula: finalDepositFormula, AuditStatus: audit.Status, AuditCompliant: audit.Compliant, AuditDisposition: audit.Disposition,
		ObservedAtBlock: audit.ObservedAtBlock, ArtifactDeadlineBlock: audit.ArtifactDeadlineBlock, AppliedWeight: 1,
	}
	if audit.Status != validatorpkg.DepositAuditZeroPrice || finalDepositAuditFromPool(finalPoolWeightTestEpoch, &row) != audit {
		t.Fatalf("row is not the validator's zero-price audit: %+v", audit)
	}
	finalPoolWeightTestRefusal(t, "zero-price audit", row, want)
	// The status alone decides: a complete priced lineage relabelled zero-price is refused too.
	priced.AuditStatus, priced.AuditDisposition = validatorpkg.DepositAuditZeroPrice, validatorpkg.DepositDispositionZeroPrice
	finalPoolWeightTestRefusal(t, "relabelled priced row", priced, want)
}

// Each missing input of the deposit and demand arithmetic is refused before a
// zero rate or denominator can reach it.
func TestFinalSemanticPoolWeightRefusesIncompleteDepositLineage(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*FinalPoolWeightEvidence)
	}{
		{name: "source epoch is not the previous epoch", edit: func(row *FinalPoolWeightEvidence) { row.SourceEpoch = finalPoolWeightTestEpoch - 2 }},
		{name: "no audited usage", edit: func(row *FinalPoolWeightEvidence) { row.UsageBytes = 0 }},
		{name: "own tier prices no bytes", edit: func(row *FinalPoolWeightEvidence) { row.RateNumeratorRaoPerGiB = 0 }},
		{name: "own tier has no denominator", edit: func(row *FinalPoolWeightEvidence) { row.RateDenominator = 0 }},
		{name: "baseline tier prices no bytes", edit: func(row *FinalPoolWeightEvidence) { row.BaselineRateNumeratorRaoPerGiB = 0 }},
		{name: "baseline tier has no denominator", edit: func(row *FinalPoolWeightEvidence) { row.BaselineRateDenominator = 0 }},
	} {
		_, row := finalPoolWeightTestRow()
		test.edit(&row)
		finalPoolWeightTestRefusal(t, test.name, row, "deposit/rate lineage is incomplete")
	}
}

// The required deposit is one floor over bytes and users at the pool's own
// tier, then the cap. An amount that drops the users, rounds up or ignores the
// cap is refused.
func TestFinalSemanticPoolWeightRequiredDepositCountsUsersAndTheCap(t *testing.T) {
	for _, test := range []struct {
		name     string
		edit     func(*FinalPoolWeightEvidence)
		required string
	}{
		{
			// 3.5 GiB * 800,000 alone is 2,800,000; the 7 users add 7 * 8 = 56.
			name: "users omitted", required: "2800000", edit: func(row *FinalPoolWeightEvidence) {
				row.UsageBytes, row.Users = 3*finalPoolWeightTestGiB+finalPoolWeightTestGiB/2, 7
				row.RateNumeratorRaoPerGiB, row.RateNumeratorRaoPerUser, row.BaselineRateNumeratorRaoPerUser = 800_000, 8, 10
			},
		},
		{
			// 2^30 + 12,345 bytes owe 1,000,011.497...; the deposit is the floor.
			name: "rounded up", required: "1000012", edit: func(row *FinalPoolWeightEvidence) { row.UsageBytes = finalPoolWeightTestGiB + 12_345 },
		},
		{
			// 20,000 GiB * 800,000 = 16e9 is above the 1e10 cap.
			name: "cap ignored", required: "16000000000", edit: func(row *FinalPoolWeightEvidence) {
				row.UsageBytes, row.RateNumeratorRaoPerGiB = 20_000*finalPoolWeightTestGiB, 800_000
			},
		},
	} {
		_, row := finalPoolWeightTestRow()
		test.edit(&row)
		row.RequiredDepositRao, row.ObservedDepositRao = test.required, test.required
		finalPoolWeightTestRefusal(t, test.name, row, "required deposit does not match exact floor-and-cap formula")
	}
}

// The demand is the audited usage at the baseline rate. The former
// deposit/rate value, the pool's own discounted rate, the floored deposit and
// an untruncated demand are each refused.
func TestFinalSemanticPoolWeightRefusesDemandNotPricedAtTheConvictionZeroTier(t *testing.T) {
	for _, test := range []struct {
		name          string
		edit          func(*FinalPoolWeightEvidence)
		demand, score FinalRational
	}{
		{
			// deposit/rate = 1,000,000 / 1,000,000 = 1 GiB, scored 3/4.
			name: "deposit over rate in GiB", edit: func(*FinalPoolWeightEvidence) {},
			demand: finalPoolWeightTestRational("1", "1"), score: finalPoolWeightTestRational("3", "4"),
		},
		{
			// 1 GiB at the pool's own 800,000 instead of the baseline 1,000,000.
			name: "priced at the discounted tier", edit: func(row *FinalPoolWeightEvidence) {
				row.RateNumeratorRaoPerGiB, row.RequiredDepositRao, row.ObservedDepositRao = 800_000, "800000", "800000"
			},
			demand: finalPoolWeightTestRational("800000", "1"), score: finalPoolWeightTestRational("600000", "1"),
		},
		{
			// The floored deposit 1,000,011 instead of 16,777,408,890,625 / 16,777,216.
			name: "floored deposit", edit: func(row *FinalPoolWeightEvidence) {
				row.UsageBytes, row.RequiredDepositRao, row.ObservedDepositRao = finalPoolWeightTestGiB+12_345, "1000011", "1000011"
			},
			demand: finalPoolWeightTestRational("1000011", "1"), score: finalPoolWeightTestRational("3000033", "4"),
		},
		{
			// 20,000 GiB * 1,000,000 = 2e10, without the 1e10/16e9 truncation.
			name: "not truncated with the capped deposit", edit: func(row *FinalPoolWeightEvidence) {
				row.UsageBytes, row.RateNumeratorRaoPerGiB = 20_000*finalPoolWeightTestGiB, 800_000
				row.RequiredDepositRao, row.ObservedDepositRao = "10000000000", "10000000000"
			},
			demand: finalPoolWeightTestRational("20000000000", "1"), score: finalPoolWeightTestRational("15000000000", "1"),
		},
	} {
		_, row := finalPoolWeightTestRow()
		test.edit(&row)
		row.ImpliedDemand, row.RawScore = test.demand, test.score
		finalPoolWeightTestRefusal(t, test.name, row, "implied demand does not equal audited usage priced at the conviction-zero tier")
	}
}

// The raw score is the implied demand times clamped quality and nothing else.
func TestFinalSemanticPoolWeightRefusesScoreOtherThanDemandTimesQuality(t *testing.T) {
	for _, test := range []struct {
		name  string
		score FinalRational
	}{
		{name: "former deposit over rate score", score: finalPoolWeightTestRational("3", "4")},
		{name: "demand without quality", score: finalPoolWeightTestRational("1000000", "1")},
		// 500,000 ppm is below the 750,000 ppm floor; unclamped it would halve the demand.
		{name: "unclamped quality", score: finalPoolWeightTestRational("500000", "1")},
	} {
		_, row := finalPoolWeightTestRow()
		row.RawScore = test.score
		finalPoolWeightTestRefusal(t, test.name, row, "pool raw score does not equal implied demand times Q")
	}
}

// An underpaid pool is retained as an explicit zero: a mismatch audit with its
// one-line reason, less deposited than required, and no quality, implied
// demand, score or applied weight. Anything else on the rejected row is
// refused.
func TestFinalSemanticPoolWeightRejectedAuditIsAnExactZero(t *testing.T) {
	zero := finalPoolWeightTestRational("0", "1")
	rejected := func() FinalPoolWeightEvidence {
		_, row := finalPoolWeightTestRow()
		row.AuditCompliant, row.AuditStatus, row.AuditDisposition, row.AuditError = false, validatorpkg.DepositAuditMismatch, "zero_pool_weight", "observed deposit is below exact demand"
		row.ObservedDepositRao, row.QualityPPM, row.QualityFactor, row.ImpliedDemand, row.RawScore, row.AppliedWeight = "999999", 0, zero, zero, zero, 0
		return row
	}
	evidence, _ := finalPoolWeightTestRow()
	row := rejected()
	score, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row)
	if err != nil || eligible || score == nil || score.Sign() != 0 {
		t.Fatalf("underpaid row score=%v eligible=%t error=%v", score, eligible, err)
	}
	for _, test := range []struct {
		name string
		edit func(*FinalPoolWeightEvidence)
	}{
		{name: "another failed status", edit: func(row *FinalPoolWeightEvidence) { row.AuditStatus = validatorpkg.DepositAuditInvalid }},
		{name: "eligible disposition", edit: func(row *FinalPoolWeightEvidence) { row.AuditDisposition = "pool_weight_eligible" }},
		{name: "no reason", edit: func(row *FinalPoolWeightEvidence) { row.AuditError = "" }},
		{name: "multi-line reason", edit: func(row *FinalPoolWeightEvidence) { row.AuditError = "observed deposit\nis below exact demand" }},
		{name: "fully paid", edit: func(row *FinalPoolWeightEvidence) { row.ObservedDepositRao = row.RequiredDepositRao }},
		{name: "applied weight", edit: func(row *FinalPoolWeightEvidence) { row.AppliedWeight = 1 }},
		{name: "quality", edit: func(row *FinalPoolWeightEvidence) { row.QualityPPM = 500_000 }},
		{name: "quality factor", edit: func(row *FinalPoolWeightEvidence) { row.QualityFactor = finalPoolWeightTestRational("3", "4") }},
		{name: "implied demand", edit: func(row *FinalPoolWeightEvidence) { row.ImpliedDemand = finalPoolWeightTestRational("1000000", "1") }},
		{name: "raw score", edit: func(row *FinalPoolWeightEvidence) { row.RawScore = finalPoolWeightTestRational("750000", "1") }},
	} {
		row := rejected()
		test.edit(&row)
		finalPoolWeightTestRefusal(t, test.name, row, "rejected deposit audit is not an exact underpayment/zero-pool disposition")
	}
}

// Every other field of a row has its own refusal before the pool can score:
// the amounts, the payout artifact's content address, root and authorities,
// block lineage, locator, deposit receipt, audit status, quality, demand and
// score encodings.
func TestFinalSemanticPoolWeightRefusesEachMalformedField(t *testing.T) {
	const (
		lineage      = "payout artifact boundary, commitment, observation, or deadline lineage is invalid"
		notCompliant = "deposit audit is not compliant and pool-weight eligible"
		outsideRange = "operator deposit is failed or outside [1,1000]"
	)
	for _, test := range []struct {
		name string
		edit func(*FinalPoolWeightEvidence)
		want string
	}{
		{name: "conviction", edit: func(row *FinalPoolWeightEvidence) { row.ConvictionBeforeRao = "01000" }, want: `conviction before "01000" is not a canonical non-negative integer`},
		{name: "zero cap", edit: func(row *FinalPoolWeightEvidence) { row.EpochDepositCapRao = "0" }, want: "deposit epoch cap is zero"},
		{name: "required amount", edit: func(row *FinalPoolWeightEvidence) { row.RequiredDepositRao = "-1" }, want: `required deposit "-1" is not a canonical non-negative integer`},
		{name: "observed amount", edit: func(row *FinalPoolWeightEvidence) { row.ObservedDepositRao = "1e6" }, want: `observed deposit "1e6" is not a canonical non-negative integer`},
		{name: "artifact content hash", edit: func(row *FinalPoolWeightEvidence) {
			row.ArtifactContentHash = strings.TrimPrefix(row.ArtifactContentHash, "sha256:")
		}, want: "signed payout artifact content hash is not canonical sha256"},
		{name: "committed artifact hash", edit: func(row *FinalPoolWeightEvidence) { row.ArtifactHash = strings.ToUpper(row.ArtifactHash) }, want: "committed payout artifact hash is not canonical lowercase 32-byte hex"},
		{name: "content address", edit: func(row *FinalPoolWeightEvidence) { row.ArtifactHash = finalTestHex(0x99) }, want: "payout artifact content address does not match committed artifact hash"},
		{name: "payout root", edit: func(row *FinalPoolWeightEvidence) { row.PayoutRoot = "0x21" }, want: "committed payout root is not 32-byte hex"},
		{name: "artifact signer", edit: func(row *FinalPoolWeightEvidence) { row.ArtifactSigner = "0x0000000000000000000000000000000000000000" }, want: "artifact signer is not a canonical operator authority"},
		{name: "root committer", edit: func(row *FinalPoolWeightEvidence) { row.RootCommitter = strings.ToUpper(row.RootCommitter) }, want: "root committer is not a canonical operator authority"},
		{name: "root signer", edit: func(row *FinalPoolWeightEvidence) { row.RootSigner = "0x22" }, want: "root signer is not a canonical operator authority"},
		{name: "committer is not the root signer", edit: func(row *FinalPoolWeightEvidence) { row.RootCommitter = row.ArtifactSigner }, want: lineage},
		{name: "no source start", edit: func(row *FinalPoolWeightEvidence) { row.SourceStartBlock = 0 }, want: lineage},
		{name: "source ends before it starts", edit: func(row *FinalPoolWeightEvidence) { row.SourceEndBlock = row.SourceStartBlock - 1 }, want: lineage},
		{name: "root committed before the source ended", edit: func(row *FinalPoolWeightEvidence) { row.RootCommitBlock = row.SourceEndBlock - 1 }, want: lineage},
		{name: "observed before the root commitment", edit: func(row *FinalPoolWeightEvidence) { row.ObservedAtBlock = row.RootCommitBlock - 1 }, want: lineage},
		{name: "observed after the artifact deadline", edit: func(row *FinalPoolWeightEvidence) { row.ArtifactDeadlineBlock = row.ObservedAtBlock - 1 }, want: lineage},
		{name: "source start hash", edit: func(row *FinalPoolWeightEvidence) { row.SourceStartHash = "" }, want: "payout source start hash is not canonical lowercase 32-byte hex"},
		{name: "source end hash", edit: func(row *FinalPoolWeightEvidence) { row.SourceEndHash = "" }, want: "payout source end hash is not canonical lowercase 32-byte hex"},
		{name: "payout artifact locator", edit: func(row *FinalPoolWeightEvidence) { row.PayoutArtifact.Kind = "evm-receipt" }, want: "signed payout artifact kind/size is incomplete"},
		{name: "failed deposit receipt", edit: func(row *FinalPoolWeightEvidence) { row.DepositReceipt.Status = "failed" }, want: outsideRange},
		{name: "deposit after the terminal block", edit: func(row *FinalPoolWeightEvidence) { row.DepositReceipt.Block.Number = 1_001 }, want: outsideRange},
		{name: "compliant row with a mismatch status", edit: func(row *FinalPoolWeightEvidence) { row.AuditStatus = validatorpkg.DepositAuditMismatch }, want: notCompliant},
		{name: "compliant row with a zero-weight disposition", edit: func(row *FinalPoolWeightEvidence) { row.AuditDisposition = "zero_pool_weight" }, want: notCompliant},
		{name: "compliant row with an audit error", edit: func(row *FinalPoolWeightEvidence) { row.AuditError = "unexpected" }, want: notCompliant},
		{name: "compliant row that overpaid", edit: func(row *FinalPoolWeightEvidence) { row.ObservedDepositRao = "1000001" }, want: notCompliant},
		{name: "quality above one", edit: func(row *FinalPoolWeightEvidence) { row.QualityPPM = 1_000_001 }, want: "quality PPM is outside [0,1000000]"},
		{name: "quality factor", edit: func(row *FinalPoolWeightEvidence) { row.QualityFactor = finalPoolWeightTestRational("1", "2") }, want: "quality factor does not match quality PPM"},
		{name: "zero demand", edit: func(row *FinalPoolWeightEvidence) { row.ImpliedDemand = finalPoolWeightTestRational("0", "1") }, want: "implied demand numerator is zero"},
		{name: "unreduced demand", edit: func(row *FinalPoolWeightEvidence) { row.ImpliedDemand = finalPoolWeightTestRational("2000000", "2") }, want: "implied demand is not canonically reduced"},
		{name: "zero score", edit: func(row *FinalPoolWeightEvidence) { row.RawScore = finalPoolWeightTestRational("0", "1") }, want: "pool raw score does not equal implied demand times Q"},
		{name: "zero score with another denominator", edit: func(row *FinalPoolWeightEvidence) { row.RawScore = finalPoolWeightTestRational("0", "2") }, want: "pool raw score numerator is zero"},
	} {
		_, row := finalPoolWeightTestRow()
		test.edit(&row)
		finalPoolWeightTestRefusal(t, test.name, row, test.want)
	}
}

// Quality is clamped to the cycle's bounds on both sides before it scales the
// demand, and an unclamped factor is refused.
func TestFinalSemanticPoolWeightClampsQualityToTheCycleBounds(t *testing.T) {
	for _, test := range []struct {
		name       string
		qualityPPM uint32
		q, score   FinalRational
	}{
		// 500,000 ppm under the 750,000 ppm floor: 1,000,000 * 3/4.
		{name: "floor", qualityPPM: 500_000, q: finalPoolWeightTestRational("3", "4"), score: finalPoolWeightTestRational("750000", "1")},
		// 800,000 ppm inside the bounds: 1,000,000 * 4/5.
		{name: "inside", qualityPPM: 800_000, q: finalPoolWeightTestRational("4", "5"), score: finalPoolWeightTestRational("800000", "1")},
		// 950,000 ppm over a 900,000 ppm ceiling: 1,000,000 * 9/10.
		{name: "ceiling", qualityPPM: 950_000, q: finalPoolWeightTestRational("9", "10"), score: finalPoolWeightTestRational("900000", "1")},
	} {
		evidence, row := finalPoolWeightTestRow()
		row.QualityPPM, row.QualityFactor, row.RawScore = test.qualityPPM, test.q, test.score
		want, _ := new(big.Rat).SetString(test.score.Numerator + "/" + test.score.Denominator)
		score, eligible, err := verifyFinalPoolWeight(&evidence, finalPoolWeightTestEpoch, 750_000, 900_000, &row)
		if err != nil || !eligible || score.Cmp(want) != 0 {
			t.Errorf("%s: score=%v eligible=%t error=%v, want %s", test.name, score, eligible, err, want.RatString())
		}
	}
	// The measured 950,000 ppm itself, 19/20, is above the ceiling.
	evidence, row := finalPoolWeightTestRow()
	row.QualityPPM, row.QualityFactor, row.RawScore = 950_000, finalPoolWeightTestRational("19", "20"), finalPoolWeightTestRational("950000", "1")
	if _, _, err := verifyFinalPoolWeight(&evidence, finalPoolWeightTestEpoch, 750_000, 900_000, &row); err == nil || err.Error() != "quality factor does not match quality PPM" {
		t.Errorf("unclamped quality: %v", err)
	}
}

// A compliant pool without measured quality declares q = 0 and scores zero:
// 8.1 weighs it implied_demand * Q_n with Q_n = 0, before any clamp. Its
// demand is still the audited usage at the baseline. The clamped factor, a
// positive score or a zero in another form is refused.
func TestFinalSemanticPoolWeightRefusesQualityOtherThanTheClampOrZero(t *testing.T) {
	zero := finalPoolWeightTestRational("0", "1")
	withoutQuality := func() FinalPoolWeightEvidence {
		_, row := finalPoolWeightTestRow()
		row.QualityPPM, row.QualityFactor, row.RawScore = 0, zero, zero
		return row
	}
	evidence, _ := finalPoolWeightTestRow()
	row := withoutQuality()
	if score, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row); err != nil || !eligible || score == nil || score.Sign() != 0 {
		t.Fatalf("pool without quality: score=%v eligible=%t error=%v", score, eligible, err)
	}
	for _, test := range []struct {
		name string
		edit func(*FinalPoolWeightEvidence)
		want string
	}{
		{
			// The 750,000 ppm floor applied to no measurement: 1,000,000 * 3/4.
			name: "clamped factor", edit: func(row *FinalPoolWeightEvidence) {
				row.QualityFactor, row.RawScore = finalPoolWeightTestRational("3", "4"), finalPoolWeightTestRational("750000", "1")
			}, want: "quality factor does not match quality PPM",
		},
		{name: "positive score", edit: func(row *FinalPoolWeightEvidence) { row.RawScore = finalPoolWeightTestRational("750000", "1") }, want: "pool raw score does not equal implied demand times Q"},
		{name: "zero factor over another denominator", edit: func(row *FinalPoolWeightEvidence) { row.QualityFactor = finalPoolWeightTestRational("0", "4") }, want: "quality factor does not match quality PPM"},
		{name: "zero score over another denominator", edit: func(row *FinalPoolWeightEvidence) { row.RawScore = finalPoolWeightTestRational("0", "4") }, want: "pool raw score numerator is zero"},
		// The pool's own 800,000 rao tier instead of the 1,000,000 baseline.
		{name: "demand not priced at the baseline", edit: func(row *FinalPoolWeightEvidence) { row.ImpliedDemand = finalPoolWeightTestRational("800000", "1") }, want: "implied demand does not equal audited usage priced at the conviction-zero tier"},
	} {
		row := withoutQuality()
		test.edit(&row)
		finalPoolWeightTestRefusal(t, test.name, row, test.want)
	}
}

// A pool's native reward is expected positive when some validator's cycle
// weighs it, with a compliant audit and measured quality. A compliant pool
// without quality, like an underpaid one, is expected to earn nothing.
func TestFinalSemanticPoolWeightRewardExpectationFollowsWeightedPools(t *testing.T) {
	pool := func(noID uint64, compliant bool, qualityPPM uint32) FinalPoolWeightEvidence {
		return FinalPoolWeightEvidence{NoID: noID, AuditCompliant: compliant, QualityPPM: qualityPPM}
	}
	cycles := func(pools ...FinalPoolWeightEvidence) []FinalCRv4Cycle {
		return []FinalCRv4Cycle{{SettlementEpoch: finalPoolWeightTestEpoch, Pools: pools}}
	}
	source := &FinalSemanticEvidence{
		Pools: []FinalPoolUIDEvidence{{NoID: 1}, {NoID: 2}, {NoID: 3}, {NoID: 4}},
		Validators: []FinalValidatorIdentityEvidence{
			{ValidatorID: 1, Cycles: cycles(pool(1, true, 500_000), pool(2, true, 0), pool(3, false, 0), pool(4, true, 0))},
			{ValidatorID: 2, Cycles: cycles(pool(1, true, 0), pool(2, true, 0), pool(3, false, 0), pool(4, true, 800_000))},
		},
	}
	heads, pools := finalSemanticRewardExpectation(source, finalPoolWeightTestEpoch)
	want := map[uint64]string{1: "positive", 2: "zero", 3: "zero", 4: "positive"}
	if heads == nil || len(heads) != 0 || !maps.Equal(pools, want) {
		t.Fatalf("reward expectations heads=%v pools=%v, want pools %v", heads, pools, want)
	}
}

// finalPoolWeightTestPolicyEvidence wraps one row in the least evidence the
// canonical policy comparison reads.
func finalPoolWeightTestPolicyEvidence(policy *protocol.Policy, row FinalPoolWeightEvidence) *FinalSemanticEvidence {
	return &FinalSemanticEvidence{
		Phase:      "release-1.0",
		Window:     ScenarioAcceptanceWindow{EpochBlocks: policy.Settlement.EpochBlocks, FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks},
		Deployment: FinalContractDeploymentEvidence{VaultMinimumClaimTTLBlocks: policy.Settlement.EpochBlocks * policy.Settlement.ClaimTTLEpochs},
		Validators: []FinalValidatorIdentityEvidence{{ValidatorID: 1, Cycles: []FinalCRv4Cycle{{
			SettlementEpoch:   finalPoolWeightTestEpoch,
			Theta:             FinalRational{Numerator: strconv.FormatUint(policy.Steering.Theta.Numerator, 10), Denominator: strconv.FormatUint(policy.Steering.Theta.Denominator, 10)},
			QualityMinimumPPM: policy.Steering.QualityTransform.MinimumPPM, QualityMaximumPPM: policy.Steering.QualityTransform.MaximumPPM,
			MaximumHeadFleets: policy.Steering.MaximumHeadFleets, MaxWeightLimitU16: policy.Steering.MaxWeightLimitU16,
			Pools: []FinalPoolWeightEvidence{row},
		}}}},
	}
}

// The baseline rate a row declares is the canonical policy's conviction-zero
// tier exactly. A row that prices its demand at its own discounted tier is
// internally consistent, so only this comparison can refuse it.
func TestFinalSemanticPoolWeightBaselineRateIsTheCanonicalPolicyTierZero(t *testing.T) {
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// The worked rows below assume the testnet schedule and cap.
	if len(policy.Deposit.Tiers) != 3 || policy.Deposit.Tiers[0] != (protocol.DepositTier{RateNumeratorRaoPerGiB: 1_000_000, RateDenominator: 1}) ||
		policy.Deposit.Tiers[1] != (protocol.DepositTier{MinConvictionRao: 1_000_000_000, RateNumeratorRaoPerGiB: 800_000, RateDenominator: 1}) || policy.Deposit.EpochCapRaoPerOperator != 10_000_000_000 {
		t.Fatalf("testnet deposit schedule changed: %+v", policy.Deposit)
	}
	// 1 alpha of conviction selects the 800,000 tier: 1 GiB owes 800,000 and
	// implies 1,000,000 at the baseline; * 3/4 = 750,000.
	evidence, discounted := finalPoolWeightTestRow()
	discounted.ConvictionBeforeRao, discounted.RateNumeratorRaoPerGiB, discounted.RequiredDepositRao, discounted.ObservedDepositRao = "1000000000", 800_000, "800000", "800000"
	if _, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &discounted); err != nil || !eligible {
		t.Fatalf("discounted row eligible=%t error=%v", eligible, err)
	}
	if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(policy, discounted), policy); err != nil {
		t.Fatalf("canonical discounted row was refused: %v", err)
	}
	const wantBaseline = "validator 1 epoch 10 pool 1 baseline deposit rate does not match the canonical policy's conviction-zero tier"
	for _, test := range []struct {
		name string
		edit func(*FinalPoolWeightEvidence)
	}{
		{
			// Priced at its own tier: implied 800,000, * 3/4 = 600,000.
			name: "own discounted tier as the baseline", edit: func(row *FinalPoolWeightEvidence) {
				row.BaselineRateNumeratorRaoPerGiB = 800_000
				row.ImpliedDemand, row.RawScore = finalPoolWeightTestRational("800000", "1"), finalPoolWeightTestRational("600000", "1")
			},
		},
		// 2,000,000 / 2 is the same rate and another tier.
		{name: "equal rate over another denominator", edit: func(row *FinalPoolWeightEvidence) {
			row.BaselineRateNumeratorRaoPerGiB, row.BaselineRateDenominator = 2_000_000, 2
		}},
		// The testnet schedule prices no users, so the demand is unchanged with none.
		{name: "user rate the policy does not publish", edit: func(row *FinalPoolWeightEvidence) { row.BaselineRateNumeratorRaoPerUser = 10 }},
	} {
		row := discounted
		test.edit(&row)
		if _, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row); err != nil || !eligible {
			t.Errorf("%s: row must be internally consistent: eligible=%t error=%v", test.name, eligible, err)
		}
		if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(policy, row), policy); err == nil || err.Error() != wantBaseline {
			t.Errorf("%s: error=%v, want exactly %q", test.name, err, wantBaseline)
		}
	}
	// The pool's own tier is still bound first: 1 alpha of conviction does not pay the baseline rate.
	row := discounted
	row.RateNumeratorRaoPerGiB, row.RequiredDepositRao, row.ObservedDepositRao = 1_000_000, "1000000", "1000000"
	if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(policy, row), policy); err == nil || err.Error() != "validator 1 epoch 10 pool 1 deposit inputs do not match canonical policy" {
		t.Errorf("baseline rate at a discounted conviction: %v", err)
	}

	// A schedule that also prices users: 10, 8 and 6 rao per user by tier.
	policy.Deposit.Unit = protocol.DepositUnitRaoPerGiBAndUser
	policy.Deposit.Tiers = append([]protocol.DepositTier(nil), policy.Deposit.Tiers...)
	for index, perUser := range []uint64{10, 8, 6} {
		policy.Deposit.Tiers[index].RateNumeratorRaoPerUser = perUser
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	// 3.5 GiB and 7 users at 1 alpha of conviction: owed 2,800,056, implied
	// 3,500,070, * 3/4 = 2,625,052.5 = 5,250,105 / 2.
	users := discounted
	users.UsageBytes, users.Users, users.RateNumeratorRaoPerUser, users.BaselineRateNumeratorRaoPerUser = 3*finalPoolWeightTestGiB+finalPoolWeightTestGiB/2, 7, 8, 10
	users.RequiredDepositRao, users.ObservedDepositRao = "2800056", "2800056"
	users.ImpliedDemand, users.RawScore = finalPoolWeightTestRational("3500070", "1"), finalPoolWeightTestRational("5250105", "2")
	if _, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &users); err != nil || !eligible {
		t.Fatalf("two-component row eligible=%t error=%v", eligible, err)
	}
	if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(policy, users), policy); err != nil {
		t.Fatalf("canonical two-component row was refused: %v", err)
	}
	// The own tier's 8 rao per user as the baseline: implied 3,500,056, * 3/4 = 2,625,042.
	users.BaselineRateNumeratorRaoPerUser = 8
	users.ImpliedDemand, users.RawScore = finalPoolWeightTestRational("3500056", "1"), finalPoolWeightTestRational("2625042", "1")
	if _, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &users); err != nil || !eligible {
		t.Fatalf("mispriced two-component row must be internally consistent: eligible=%t error=%v", eligible, err)
	}
	if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(policy, users), policy); err == nil || err.Error() != wantBaseline {
		t.Errorf("own user rate as the baseline: error=%v, want exactly %q", err, wantBaseline)
	}
}

// The canonical policy fixes every deposit input of a row: its conviction
// encoding, required amount, own tier's rates and denominator, and the cap.
func TestFinalSemanticPoolWeightDepositInputsAreTheCanonicalPolicyTier(t *testing.T) {
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, canonical := finalPoolWeightTestRow()
	if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(policy, canonical), policy); err != nil {
		t.Fatalf("canonical row was refused: %v", err)
	}
	const differs = "validator 1 epoch 10 pool 1 deposit inputs do not match canonical policy"
	for _, test := range []struct {
		name string
		edit func(*FinalPoolWeightEvidence)
		want string
	}{
		{name: "conviction encoding", edit: func(row *FinalPoolWeightEvidence) { row.ConvictionBeforeRao = "1e3" }, want: `conviction before "1e3" is not a canonical non-negative integer`},
		{name: "required amount", edit: func(row *FinalPoolWeightEvidence) { row.RequiredDepositRao = "1000001" }, want: differs},
		{name: "per-GiB rate", edit: func(row *FinalPoolWeightEvidence) { row.RateNumeratorRaoPerGiB = 800_000 }, want: differs},
		{name: "per-user rate the policy does not publish", edit: func(row *FinalPoolWeightEvidence) { row.RateNumeratorRaoPerUser = 8 }, want: differs},
		{name: "rate denominator", edit: func(row *FinalPoolWeightEvidence) { row.RateNumeratorRaoPerGiB, row.RateDenominator = 2_000_000, 2 }, want: differs},
		{name: "epoch cap", edit: func(row *FinalPoolWeightEvidence) { row.EpochDepositCapRao = "9999999999" }, want: differs},
	} {
		row := canonical
		test.edit(&row)
		if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(policy, row), policy); err == nil || err.Error() != test.want {
			t.Errorf("%s: error=%v, want exactly %q", test.name, err, test.want)
		}
	}
}

// A dishonest-deposit penalty cycle is not an accepted cycle, so its rows meet
// the canonical policy themselves: the own tier, the cap, the clamp, theta and
// the baseline rates. Each mispriced row below is consistent on its own and is
// refused only by the policy.
func TestFinalSemanticPoolWeightPenaltyCycleRowsMeetTheCanonicalPolicy(t *testing.T) {
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// 1 alpha of conviction selects the 800,000 tier: 1 GiB owes 800,000 and
	// implies 1,000,000 at the baseline; * 3/4 = 750,000.
	_, discounted := finalPoolWeightTestRow()
	discounted.ConvictionBeforeRao, discounted.RateNumeratorRaoPerGiB, discounted.RequiredDepositRao, discounted.ObservedDepositRao = "1000000000", 800_000, "800000", "800000"
	withPenalty := func(edit func(*FinalCRv4Cycle)) *FinalSemanticEvidence {
		evidence := finalPoolWeightTestPolicyEvidence(policy, discounted)
		evidence.Phase = "production-soak"
		evidence.Window = ScenarioAcceptanceWindow{EpochBlocks: policy.ProductionCadence.EpochBlocks, FinalizeOffsetBlocks: policy.ProductionCadence.FinalizeOffsetBlocks}
		penalty := evidence.Validators[0].Cycles[0]
		penalty.SettlementEpoch--
		penalty.Pools = []FinalPoolWeightEvidence{discounted}
		edit(&penalty)
		evidence.DishonestDeposit = &FinalDishonestDepositEvidence{Penalties: []FinalDishonestDepositDecision{{ValidatorID: 1, Cycle: penalty}}}
		return evidence
	}
	if err := verifyFinalPolicyInputs(withPenalty(func(*FinalCRv4Cycle) {}), policy); err != nil {
		t.Fatalf("canonical penalty cycle was refused: %v", err)
	}
	evidence, _ := finalPoolWeightTestRow()
	const (
		deposit  = "validator 1 dishonest-deposit penalty epoch 9 pool 1 deposit inputs do not match canonical policy"
		baseline = "validator 1 dishonest-deposit penalty epoch 9 pool 1 baseline deposit rate does not match the canonical policy's conviction-zero tier"
		steering = "validator 1 dishonest-deposit penalty epoch 9 steering parameters do not match canonical policy"
	)
	for _, test := range []struct {
		name string
		edit func(*FinalCRv4Cycle)
		want string
	}{
		{
			// The review's case: priced at its own tier (implied 800,000, * 3/4 =
			// 600,000) under a cap one rao below the policy's 10,000,000,000.
			name: "own-tier baseline under a foreign cap", edit: func(cycle *FinalCRv4Cycle) {
				row := &cycle.Pools[0]
				row.BaselineRateNumeratorRaoPerGiB, row.EpochDepositCapRao = 800_000, "9999999999"
				row.ImpliedDemand, row.RawScore = finalPoolWeightTestRational("800000", "1"), finalPoolWeightTestRational("600000", "1")
			}, want: deposit,
		},
		{
			name: "own-tier baseline", edit: func(cycle *FinalCRv4Cycle) {
				row := &cycle.Pools[0]
				row.BaselineRateNumeratorRaoPerGiB = 800_000
				row.ImpliedDemand, row.RawScore = finalPoolWeightTestRational("800000", "1"), finalPoolWeightTestRational("600000", "1")
			}, want: baseline,
		},
		{
			// The baseline rate at 1 alpha of conviction, which pays 800,000.
			name: "baseline rate as the own tier", edit: func(cycle *FinalCRv4Cycle) {
				row := &cycle.Pools[0]
				row.RateNumeratorRaoPerGiB, row.RequiredDepositRao, row.ObservedDepositRao = 1_000_000, "1000000", "1000000"
			}, want: deposit,
		},
		{name: "theta", edit: func(cycle *FinalCRv4Cycle) { cycle.Theta = finalPoolWeightTestRational("1", "2") }, want: steering},
		{name: "quality floor", edit: func(cycle *FinalCRv4Cycle) { cycle.QualityMinimumPPM = 500_000 }, want: steering},
		{name: "quality ceiling", edit: func(cycle *FinalCRv4Cycle) { cycle.QualityMaximumPPM = 900_000 }, want: steering},
	} {
		mispriced := withPenalty(test.edit)
		row := mispriced.DishonestDeposit.Penalties[0].Cycle.Pools[0]
		if _, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row); err != nil || !eligible {
			t.Errorf("%s: penalty row must be internally consistent: eligible=%t error=%v", test.name, eligible, err)
		}
		if err := verifyFinalPolicyInputs(mispriced, policy); err == nil || err.Error() != test.want {
			t.Errorf("%s: error=%v, want exactly %q", test.name, err, test.want)
		}
	}
}

// finalPoolWeightBuilderTestFixture is everything buildPoolWeight reads for
// three operators whose pool decisions come from a measurement the validator
// itself sealed and verified.
type finalPoolWeightBuilderTestFixture struct {
	archive *finalSemanticArchive
	policy  *protocol.Policy
	cycle   *FinalCRv4Cycle
	intent  *validatorpkg.SteeringIntent
	events  *finalSemanticEventIndex
	pools   []validatorpkg.VerifiedReleasePool
}

// finalPoolWeightBuilderTestRows are worked by hand under the testnet schedule
// with users also priced at 10, 8 and 6 rao by tier, and a 1e10 rao cap:
//
//	1: 1 GiB at conviction 1,000 pays the baseline 1,000,000: owed and implied
//	   1,000,000; 500,000 ppm clamps to 3/4; score 750,000.
//	2: 3.5 GiB and 7 users at 1 alpha of conviction pay 800,000 and 8: owed
//	   2,800,056; implied 3,500,070; 9/10; score 3,150,063.
//	3: 20,000 GiB at 1 alpha of conviction owe 16e9, capped to 1e10; implied
//	   2e10 * 1e10/16e9 = 12.5e9; quality 1; score 12.5e9.
type finalPoolWeightBuilderTestRow struct {
	bytes, users     uint64
	conviction       string
	gib, user        uint64
	required         uint64
	qualityPPM       uint32
	q, demand, score FinalRational
}

var finalPoolWeightBuilderTestRows = []finalPoolWeightBuilderTestRow{
	{bytes: finalPoolWeightTestGiB, conviction: "1000", gib: 1_000_000, user: 10, required: 1_000_000, qualityPPM: 500_000, q: finalPoolWeightTestRational("3", "4"), demand: finalPoolWeightTestRational("1000000", "1"), score: finalPoolWeightTestRational("750000", "1")},
	{bytes: 3*finalPoolWeightTestGiB + finalPoolWeightTestGiB/2, users: 7, conviction: "1000000000", gib: 800_000, user: 8, required: 2_800_056, qualityPPM: 900_000, q: finalPoolWeightTestRational("9", "10"), demand: finalPoolWeightTestRational("3500070", "1"), score: finalPoolWeightTestRational("3150063", "1")},
	{bytes: 20_000 * finalPoolWeightTestGiB, conviction: "1000000000", gib: 800_000, user: 8, required: 10_000_000_000, qualityPPM: 1_000_000, q: finalPoolWeightTestRational("1", "1"), demand: finalPoolWeightTestRational("12500000000", "1"), score: finalPoolWeightTestRational("12500000000", "1")},
}

// newFinalPoolWeightBuilderTestFixture signs one payout artifact per row,
// records its exact Deposit event, and has the validator seal a pools-only
// measurement over the resulting compliant audits. One tail provider per
// operator carries the pool quality as its prior.
func newFinalPoolWeightBuilderTestFixture(t *testing.T, rows []finalPoolWeightBuilderTestRow) *finalPoolWeightBuilderTestFixture {
	t.Helper()
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	policy.Deposit.Unit = protocol.DepositUnitRaoPerGiBAndUser
	policy.Deposit.Tiers = append([]protocol.DepositTier(nil), policy.Deposit.Tiers...)
	for index, perUser := range []uint64{10, 8, 6} {
		policy.Deposit.Tiers[index].RateNumeratorRaoPerUser = perUser
	}
	policyHash, err := policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	const sourceEpoch, snapshotBlock = finalPoolWeightTestEpoch - 1, uint64(60)
	coordinator, vault := common.HexToAddress("0x1111111111111111111111111111111111111111"), common.HexToAddress("0x2222222222222222222222222222222222222222")
	measurement := &validatorpkg.ReleaseMeasurementArtifact{
		Schema: validatorpkg.ReleaseMeasurementSchema, DeploymentID: "pool-weight-builder", ChainID: 945, GenesisHash: finalTestHex(5),
		Coordinator: strings.ToLower(coordinator.Hex()), SettlementVault: strings.ToLower(vault.Hex()), ValidatorID: 1, Netuid: 521, SubnetEpoch: 7,
		NativeSnapshotBlock: 100, NativeSnapshotHash: finalTestHex(100), EVMSnapshotBlock: snapshotBlock, EVMSnapshotHash: finalTestHex(byte(snapshotBlock)),
		SettlementEpoch: finalPoolWeightTestEpoch, PolicyHash: policyHash, Policy: *policy, ControlledNOIDs: []uint64{},
		Inputs: []validatorpkg.ReleaseMeasurementInput{}, Bindings: []validatorpkg.ReleaseBindingMeasurement{}, HeadEMA: []validatorpkg.HeadEMAMeasurement{},
		Pools: []validatorpkg.ReleasePoolMeasurement{}, DepositAudits: []validatorpkg.DepositAudit{}, SelfUID: 3,
	}
	archive := &finalSemanticArchive{runRoot: t.TempDir(), collected: &FinalSemanticCollectedInputs{}, files: map[string][]byte{}}
	logs := make([]finalCanonicalEVMLog, 0, len(rows))
	serverKeys := make([]ed25519.PrivateKey, 0, len(rows))
	weighted := 0
	zero := finalTestHex(0)
	for index, row := range rows {
		if row.qualityPPM != 0 {
			weighted++
		}
		noID := uint64(index + 1)
		var clientID connect.Id
		clientID[0], clientID[15] = 1, byte(noID)
		providers := []payoutartifact.ProviderInput{{
			ClientID: [16]byte(clientID), NetworkID: [16]byte{3, byte(noID)}, Coldkey: [32]byte{5, byte(noID)}, UsageBytes: row.bytes,
			Assignments: policy.Verify.ReliabilityAMin, Confirmations: policy.Verify.ReliabilityAMin, Eligible: true,
		}}
		payout, err := payoutartifact.Build(payoutartifact.BuildInput{
			DeploymentID: measurement.DeploymentID, GenesisHash: measurement.GenesisHash, PolicyHash: policyHash, ChainID: measurement.ChainID, Netuid: measurement.Netuid,
			Coordinator: coordinator, SettlementVault: vault, Epoch: sourceEpoch, NoID: noID,
			Start: payoutartifact.Boundary{Number: 30, Hash: finalTestHex(30)}, End: payoutartifact.Boundary{Number: 39, Hash: finalTestHex(39)},
			OperatorSnapshotHash: finalPayoutOperatorSnapshotHash(noID, sourceEpoch, policyHash), FleetSnapshotHash: finalPayoutFleetSnapshotHash(providers),
			Providers: providers, TotalUsers: row.users, ReliabilityAMin: policy.Verify.ReliabilityAMin, CreatedAt: time.Unix(1_700_000_000+int64(noID), 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		payoutKey, err := crypto.ToECDSA(bytes.Repeat([]byte{byte(0x10 + noID)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		if err := payoutartifact.Sign(payout, payoutKey); err != nil {
			t.Fatal(err)
		}
		payoutBytes, err := payoutartifact.Bytes(payout)
		if err != nil {
			t.Fatal(err)
		}
		locator := FinalArtifactLocator{Kind: "payout-artifact", URI: fmt.Sprintf("payouts/%d-%d.json", sourceEpoch, noID), ContentHash: bytesSHA256(payoutBytes), SizeBytes: uint64(len(payoutBytes))}
		archive.files[locator.URI] = payoutBytes
		archive.collected.Payouts = append(archive.collected.Payouts, FinalCollectedPayoutArtifact{NoID: noID, Epoch: sourceEpoch, Artifact: locator})
		logs = append(logs, finalSemanticBuilderDepositLog(t, noID, finalPoolWeightTestEpoch, row.required, 20+noID, 0, byte(0x40+noID)))
		serverKeys = append(serverKeys, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x50 + noID)}, ed25519.SeedSize)))

		required := strconv.FormatUint(row.required, 10)
		rootSigner := strings.ToLower(common.BytesToAddress([]byte{0x70, byte(noID)}).Hex())
		measurement.DepositAudits = append(measurement.DepositAudits, validatorpkg.DepositAudit{
			NoID: noID, Epoch: finalPoolWeightTestEpoch, SourceEpoch: sourceEpoch,
			ArtifactHash: payout.ContentHash, CommittedArtifactHash: "0x" + strings.TrimPrefix(payout.ContentHash, "sha256:"), PayoutRoot: "0x" + hex.EncodeToString(payout.PayoutRoot[:]),
			ArtifactSigner: strings.ToLower(payout.Signer.Hex()), RootCommitter: rootSigner, RootSigner: rootSigner,
			SourceStartBlock: payout.Start.Number, SourceStartHash: payout.Start.Hash, SourceEndBlock: payout.End.Number, SourceEndHash: payout.End.Hash,
			RootCommitBlock: payout.End.Number + 1, ObservedAtBlock: snapshotBlock, ArtifactDeadlineBlock: snapshotBlock + 8,
			UsageBytes: row.bytes, Users: row.users, ConvictionBeforeRao: row.conviction,
			RateNumeratorRaoPerGiB: row.gib, RateNumeratorRaoPerUser: row.user, RateDenominator: 1, RequiredDepositRao: required, ObservedDepositRao: required,
			Status: validatorpkg.DepositAuditCompliant, Compliant: true, Disposition: "pool_weight_eligible",
		})
		measurement.Inputs = append(measurement.Inputs, validatorpkg.ReleaseMeasurementInput{
			NoID: noID, SettlementEpoch: finalPoolWeightTestEpoch, CutNativeBlock: 99, CutNativeBlockHash: finalTestHex(99), CutEVMSnapshotBlock: snapshotBlock, CutEVMSnapshotHash: measurement.EVMSnapshotHash,
			Stats: validatorpkg.ReleaseStatsMeasurement{
				Config:    validatorpkg.ReleaseStatsConfig{AMin: policy.Verify.ReliabilityAMin, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000},
				Providers: []validatorpkg.ReleaseProviderMeasurement{{ClientID: clientID.String(), LatencyBuckets: make([]uint64, 31), HasPriorQuality: true, PriorQualityPPM: row.qualityPPM}},
			},
		})
		measurement.Bindings = append(measurement.Bindings, validatorpkg.ReleaseBindingMeasurement{NoID: noID, ClientID: clientID.String(), FleetID: zero, Hotkey: zero, ClientKey: zero, LocalClientKey: zero, CommitmentHash: zero})
		measurement.Pools = append(measurement.Pools, validatorpkg.ReleasePoolMeasurement{NoID: noID, UID: uint16(10 + noID), PoolHotkey: finalTestHex(byte(0x60 + noID))})
	}
	attachFinalAttemptCuts(t, measurement, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize)), serverKeys, map[uint64]*finalAttemptFixtureLedger{}, nil)
	_, _, verified, err := validatorpkg.SealReleaseMeasurementArtifact(measurement)
	if err != nil {
		t.Fatalf("validator refused the pools-only measurement: %v", err)
	}
	// A pool without measured quality scores zero and gets no weight.
	if len(verified.Pools) != len(rows) || len(verified.UIDs) != weighted {
		t.Fatalf("validator decided %d pools over UIDs %v", len(verified.Pools), verified.UIDs)
	}
	events, err := indexFinalSemanticEvents(finalSemanticBuilderDepositSnapshot(logs))
	if err != nil {
		t.Fatal(err)
	}
	intent := &validatorpkg.SteeringIntent{ValidatorID: 1, SubnetEpoch: measurement.SubnetEpoch, SettlementEpoch: finalPoolWeightTestEpoch, UIDs: verified.UIDs}
	for index, score := range verified.Scores {
		intent.Values = append(intent.Values, uint16(100*(index+1)))
		intent.Scores = append(intent.Scores, validatorpkg.RationalJSON{Numerator: score.Num().String(), Denominator: score.Denom().String()})
	}
	return &finalPoolWeightBuilderTestFixture{
		archive: archive, policy: policy, intent: intent, events: events, pools: verified.Pools,
		cycle: &FinalCRv4Cycle{SettlementEpoch: finalPoolWeightTestEpoch, QualityMinimumPPM: policy.Steering.QualityTransform.MinimumPPM, QualityMaximumPPM: policy.Steering.QualityTransform.MaximumPPM},
	}
}

// The builder's row carries the validator's score and the demand priced here.
// For a baseline, a discounted two-component and a capped pool the validator's
// measurement, this package's verifier and the hand-worked values all agree,
// and the row reproduces the validator's signed audit exactly.
func TestFinalSemanticPoolWeightBuilderRowsAgreeWithTheValidatorAndTheVerifier(t *testing.T) {
	fixture := newFinalPoolWeightBuilderTestFixture(t, finalPoolWeightBuilderTestRows)
	evidence, _ := finalPoolWeightTestRow()
	baseline := protocol.DepositTier{RateNumeratorRaoPerGiB: 1_000_000, RateNumeratorRaoPerUser: 10, RateDenominator: 1}
	for index, want := range finalPoolWeightBuilderTestRows {
		pool := fixture.pools[index]
		if validatorScore := finalRationalFromBig(pool.Score); validatorScore != want.score || !pool.Eligible || pool.QualityPPM != want.qualityPPM {
			t.Fatalf("operator %d validator score=%+v quality=%d eligible=%t, want worked score %+v", pool.NoID, validatorScore, pool.QualityPPM, pool.Eligible, want.score)
		}
		row, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, pool, fixture.intent, fixture.events)
		if err != nil {
			t.Fatalf("operator %d: %v", pool.NoID, err)
		}
		if row.ImpliedDemand != want.demand || row.RawScore != want.score || row.QualityFactor != want.q || row.RequiredDepositRao != strconv.FormatUint(want.required, 10) || row.Formula != finalDepositFormula {
			t.Errorf("operator %d row demand=%+v score=%+v q=%+v required=%s", pool.NoID, row.ImpliedDemand, row.RawScore, row.QualityFactor, row.RequiredDepositRao)
		}
		if row.BaselineRateNumeratorRaoPerGiB != baseline.RateNumeratorRaoPerGiB || row.BaselineRateNumeratorRaoPerUser != baseline.RateNumeratorRaoPerUser || row.BaselineRateDenominator != baseline.RateDenominator ||
			row.RateNumeratorRaoPerGiB != want.gib || row.RateNumeratorRaoPerUser != want.user || row.Users != want.users || row.UsageBytes != want.bytes || row.EpochDepositCapRao != "10000000000" {
			t.Errorf("operator %d row rates or usage differ from the signed policy and audit: %+v", pool.NoID, row)
		}
		if row.AppliedWeight != fixture.intent.Values[index] || row.NoID != pool.NoID || row.UID != pool.UID || row.QualityPPM != pool.QualityPPM {
			t.Errorf("operator %d row no/UID/quality/applied weight=%d/%d/%d/%d", pool.NoID, row.NoID, row.UID, row.QualityPPM, row.AppliedWeight)
		}
		wantScore, _ := new(big.Rat).SetString(want.score.Numerator + "/" + want.score.Denominator)
		score, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row)
		if err != nil || !eligible || score.Cmp(wantScore) != 0 {
			t.Errorf("operator %d verifier score=%v eligible=%t error=%v, want %s", pool.NoID, score, eligible, err, wantScore.RatString())
		}
		if audit := finalDepositAuditFromPool(finalPoolWeightTestEpoch, &row); audit != pool.Audit {
			t.Errorf("operator %d row does not reproduce the validator's audit:\nrow   %+v\naudit %+v", pool.NoID, audit, pool.Audit)
		}
		if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(fixture.policy, row), fixture.policy); err != nil {
			t.Errorf("operator %d row differs from the canonical policy: %v", pool.NoID, err)
		}
	}
}

// The builder copies the validator's score without judging it. A validator
// still scoring deposit/rate would hand it 2,800,056/800,000 * 9/10 =
// 3,150,063/1,000,000 for operator 2, and the verifier refuses that row.
func TestFinalSemanticPoolWeightBuilderCannotCarryADepositOverRateScore(t *testing.T) {
	fixture := newFinalPoolWeightBuilderTestFixture(t, finalPoolWeightBuilderTestRows)
	pool := fixture.pools[1]
	pool.Score = big.NewRat(3_150_063, 1_000_000)
	row, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, pool, fixture.intent, fixture.events)
	if err != nil {
		t.Fatal(err)
	}
	if row.RawScore != finalPoolWeightTestRational("3150063", "1000000") || row.ImpliedDemand != finalPoolWeightTestRational("3500070", "1") {
		t.Fatalf("row score=%+v demand=%+v", row.RawScore, row.ImpliedDemand)
	}
	finalPoolWeightTestRefusal(t, "deposit over rate score", row, "pool raw score does not equal implied demand times Q")
}

// A zero-price audit names no payout artifact and requires no deposit, so the
// builder has nothing to bind and says so before looking for either.
func TestFinalSemanticPoolWeightBuilderRefusesZeroPriceAudit(t *testing.T) {
	fixture := newFinalPoolWeightBuilderTestFixture(t, finalPoolWeightBuilderTestRows)
	pool := fixture.pools[0]
	pool.Audit = validatorpkg.ZeroPriceDepositAudit(finalPoolWeightTestEpoch, finalPoolWeightTestEpoch-1, 1, big.NewInt(0), big.NewInt(1_000))
	const want = "operator 1 zero-price deposit audit has no payout artifact or deposit receipt to bind"
	if _, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, pool, fixture.intent, fixture.events); err == nil || err.Error() != want {
		t.Fatalf("zero-price audit error=%v, want exactly %q", err, want)
	}
}

// Without a cycle, an intent, a cap or a conviction-zero tier there is no
// demand to price.
func TestFinalSemanticPoolWeightBuilderRefusesIncompleteContext(t *testing.T) {
	fixture := newFinalPoolWeightBuilderTestFixture(t, finalPoolWeightBuilderTestRows)
	deposit := fixture.policy.Deposit
	noCap, noTiers, noDenominator := deposit, deposit, deposit
	noCap.EpochCapRaoPerOperator = 0
	noTiers.Tiers = nil
	noDenominator.Tiers = append([]protocol.DepositTier(nil), deposit.Tiers...)
	noDenominator.Tiers[0].RateDenominator = 0
	for _, test := range []struct {
		name    string
		cycle   *FinalCRv4Cycle
		deposit protocol.DepositPolicy
		intent  *validatorpkg.SteeringIntent
	}{
		{name: "no cycle", deposit: deposit, intent: fixture.intent},
		{name: "no intent", cycle: fixture.cycle, deposit: deposit},
		{name: "no cap", cycle: fixture.cycle, deposit: noCap, intent: fixture.intent},
		{name: "no tiers", cycle: fixture.cycle, deposit: noTiers, intent: fixture.intent},
		{name: "baseline tier without a denominator", cycle: fixture.cycle, deposit: noDenominator, intent: fixture.intent},
	} {
		if _, err := fixture.archive.buildPoolWeight(test.cycle, test.deposit, fixture.pools[0], test.intent, fixture.events); err == nil || err.Error() != "pool weight construction context is incomplete" {
			t.Errorf("%s: %v", test.name, err)
		}
	}
}

// An eligible decision whose audit has no rate denominator cannot be priced.
func TestFinalSemanticPoolWeightBuilderRefusesAuditWithoutARateDenominator(t *testing.T) {
	fixture := newFinalPoolWeightBuilderTestFixture(t, finalPoolWeightBuilderTestRows)
	pool := fixture.pools[0]
	pool.Audit.RateDenominator = 0
	if _, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, pool, fixture.intent, fixture.events); err == nil || err.Error() != "signed deposit audit rate is invalid" {
		t.Fatalf("audit without a rate denominator: %v", err)
	}
}

// The audited bytes and users a row prices are the signed payout artifact's.
func TestFinalSemanticPoolWeightBuilderBindsThePayoutArtifactUsageAndUsers(t *testing.T) {
	fixture := newFinalPoolWeightBuilderTestFixture(t, finalPoolWeightBuilderTestRows)
	const differs = "operator 2 source epoch 9 payout differs from signed deposit audit"
	for _, test := range []struct {
		name string
		edit func(*validatorpkg.DepositAudit)
	}{
		{name: "usage bytes", edit: func(audit *validatorpkg.DepositAudit) { audit.UsageBytes++ }},
		{name: "users", edit: func(audit *validatorpkg.DepositAudit) { audit.Users++ }},
	} {
		pool := fixture.pools[1]
		test.edit(&pool.Audit)
		if _, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, pool, fixture.intent, fixture.events); err == nil || err.Error() != differs {
			t.Errorf("%s: error=%v, want exactly %q", test.name, err, differs)
		}
	}
	fixture.archive.collected.Payouts = nil
	if _, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, fixture.pools[1], fixture.intent, fixture.events); err == nil || err.Error() != "operator 2 source epoch 9 payout artifact is absent" {
		t.Errorf("absent payout artifact: %v", err)
	}
}

// The payout bytes, the intent's vectors and the Deposit event are each read
// before a row is declared, and each refuses on its own.
func TestFinalSemanticPoolWeightBuilderRefusesUnreadablePayoutIntentOrDeposit(t *testing.T) {
	fixture := newFinalPoolWeightBuilderTestFixture(t, finalPoolWeightBuilderTestRows)
	build := func(pool validatorpkg.VerifiedReleasePool, intent *validatorpkg.SteeringIntent) error {
		_, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, pool, intent, fixture.events)
		return err
	}
	pool := fixture.pools[1]
	shortIntent := *fixture.intent
	shortIntent.Values = shortIntent.Values[:2]
	if err := build(pool, &shortIntent); err == nil || err.Error() != "signed intent weight vectors have different lengths" {
		t.Errorf("short intent vector: %v", err)
	}
	// One rao less than the 2,800,056 the Deposit event records.
	underpaid := pool
	underpaid.Audit.ObservedDepositRao = "2800055"
	if err := build(underpaid, fixture.intent); err == nil || err.Error() != "operator 2 epoch 10 captured Deposit prefix does not equal signed observed amount 2800055 at block 60" {
		t.Errorf("deposit the events do not record: %v", err)
	}
	const uri = "payouts/9-2.json"
	fixture.archive.files[uri] = []byte("{}")
	if err := build(pool, fixture.intent); err == nil || err.Error() != "decode operator 2 source epoch 9 payout: invalid artifact schema/hash" {
		t.Errorf("payout bytes that are not an artifact: %v", err)
	}
	delete(fixture.archive.files, uri)
	if err := build(pool, fixture.intent); err == nil || err.Error() != "closed semantic graph is missing payouts/9-2.json" {
		t.Errorf("missing payout bytes: %v", err)
	}
}

// The validator decides a compliant pool without measured quality eligible at
// score 0 and gives it no weight. The builder declares that decision with
// q = 0 and the 8.1 demand, and the verifier accepts it at score 0.
func TestFinalSemanticPoolWeightCompliantPoolWithoutQualityScoresZero(t *testing.T) {
	zero := finalPoolWeightTestRational("0", "1")
	rows := []finalPoolWeightBuilderTestRow{
		// 1 GiB at the baseline still implies 1,000,000 rao, times Q_n = 0.
		{bytes: finalPoolWeightTestGiB, conviction: "1000", gib: 1_000_000, user: 10, required: 1_000_000, qualityPPM: 0, q: zero, demand: finalPoolWeightTestRational("1000000", "1"), score: zero},
		finalPoolWeightBuilderTestRows[1],
	}
	fixture := newFinalPoolWeightBuilderTestFixture(t, rows)
	pool := fixture.pools[0]
	if !pool.Eligible || pool.QualityPPM != 0 || pool.Score.Sign() != 0 || pool.Audit.Status != validatorpkg.DepositAuditCompliant || slices.Contains(fixture.intent.UIDs, pool.UID) {
		t.Fatalf("validator decision %+v over UIDs %v, want an eligible zero score without weight", pool, fixture.intent.UIDs)
	}
	row, err := fixture.archive.buildPoolWeight(fixture.cycle, fixture.policy.Deposit, pool, fixture.intent, fixture.events)
	if err != nil {
		t.Fatal(err)
	}
	if row.QualityPPM != 0 || row.QualityFactor != zero || row.ImpliedDemand != rows[0].demand || row.RawScore != zero || row.AppliedWeight != 0 || finalPoolWeighted(&row) {
		t.Fatalf("row q=%+v demand=%+v score=%+v applied=%d", row.QualityFactor, row.ImpliedDemand, row.RawScore, row.AppliedWeight)
	}
	evidence, _ := finalPoolWeightTestRow()
	score, eligible, err := verifyFinalPoolWeightTestRow(&evidence, &row)
	if err != nil || !eligible || score == nil || score.Sign() != 0 {
		t.Fatalf("verifier score=%v eligible=%t error=%v, want 0", score, eligible, err)
	}
	if audit := finalDepositAuditFromPool(finalPoolWeightTestEpoch, &row); audit != pool.Audit {
		t.Errorf("row does not reproduce the validator's audit:\nrow   %+v\naudit %+v", audit, pool.Audit)
	}
	if err := verifyFinalPolicyInputs(finalPoolWeightTestPolicyEvidence(fixture.policy, row), fixture.policy); err != nil {
		t.Errorf("row differs from the canonical policy: %v", err)
	}
}

// A cycle whose compliant pool has no measured quality verifies with that pool
// absent from the vector and the whole pool share on the other pool; the
// cycle check used to require weight for every compliant pool.
func TestFinalSemanticPoolWeightCycleOmitsAPoolWithoutQuality(t *testing.T) {
	t.Parallel()
	source, _ := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	pools, err := verifyFinalPools(draft)
	if err != nil {
		t.Fatal(err)
	}
	validator := &draft.Validators[0]
	cycle := &validator.Cycles[0]
	zero := finalPoolWeightTestRational("0", "1")
	pool := &cycle.Pools[0]
	pool.QualityPPM, pool.QualityFactor, pool.RawScore = 0, zero, zero
	finalSemanticBuilderReweightCycle(t, cycle)
	other := cycle.Pools[1]
	if pool.AppliedWeight != 0 || other.AppliedWeight == 0 || cycle.RealizedPoolValue != uint64(other.AppliedWeight) {
		t.Fatalf("reweighted pools applied %d and %d of %d", pool.AppliedWeight, other.AppliedWeight, cycle.RealizedPoolValue)
	}
	for _, submitted := range cycle.Submitted {
		if submitted.UID == pool.UID {
			t.Fatalf("pool UID %d without quality was submitted with %d", pool.UID, submitted.Value)
		}
	}
	if err := verifyFinalCRv4Cycle(draft, validator.ValidatorID, validator.UID, cycle, pools); err != nil {
		t.Fatalf("cycle with a compliant pool without quality: %v", err)
	}
}
