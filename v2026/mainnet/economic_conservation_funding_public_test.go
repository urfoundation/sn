// These public controls derive capture composition from original Wasm and
// retained vault receipt evidence. Artifact/commitment servers supply explicit
// synthetic authority only; no fixture amounts are injected into the result.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	substrate "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/crypto/blake2b"
)

// Original code moves all twenty units, but only six are native earnings.
func TestEconomicFundingPublicActualCaptureDoesNotEarnOpeningStock(t *testing.T) {
	f, contract := newEconomicConservationCaptureFixture(t, "capture", false, nil)
	summary := f.sample(t, monitorServiceHooks{})
	capture := economicCaptureTestWitness(t, f, summary)
	if summary.Funding == nil || summary.Funding.OriginalCaptures != 1 || summary.Funding.OriginalClaims != 0 || summary.Funding.OriginalPayments != 0 || contract.escrowAfter != "20" || capture.PrincipalEffects.LiquidEarnings != "6" {
		t.Fatal("original execution did not reach public funding composition", summary)
	}
	economicFundingTestRange(t, summary.Funding.Captured, "20", "6", "6", "14", "14", true)
	if summary.Funding.CapitalSubsidyAuthorized || summary.Funding.NoNonIncomeProviderCredit != nil || summary.TargetMet != nil || summary.Conformance.CompleteEvidence {
		t.Fatal("capturing original stock authorized paying it as native income", summary)
	}
}

// Actual deposit, withdrawal and refund host operations remain separate even
// when their net effect could be described by a convenient single amount.
func TestEconomicFundingPublicActualWithdrawalKeepsSourceBoundsThroughArchive(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture-causes", false, nil)
	first := f.sample(t, monitorServiceHooks{})
	capture := economicCaptureTestWitness(t, f, first)
	if capture.PrincipalEffects.Deposits != "5" || capture.PrincipalEffects.Withdrawals != "3" || capture.PrincipalEffects.Refunds != "2" {
		t.Fatal("fixture omitted original stake causes", capture)
	}
	economicFundingTestRange(t, first.Funding.Captured, "24", "3", "6", "18", "21", true)
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public original funding archive", code, issue)
	}
	state := f.source.state(t)
	if len(state.Captures) != 0 || state.Archive.Counts.CausalCaptures != 1 {
		t.Fatal("funding control did not retire its actual causal capture", state)
	}
	second := f.sample(t, monitorServiceHooks{})
	if second.Funding.Captured != first.Funding.Captured || second.Funding.OriginalCaptures != 1 || second.TargetMet != nil || second.Funding.CapitalSubsidyAuthorized {
		t.Fatal("cold funding admission changed original fungible source bounds", first.Funding, second)
	}
}

// A same-block second deposit and capture is not a second native earning.
func TestEconomicFundingPublicSameBlockDepositCannotRepeatNativeIncome(t *testing.T) {
	f, _, _ := newEconomicCaptureSequenceFixture(t, true)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.CausallyJoinedCaptures != 2 || summary.Funding == nil || summary.Funding.OriginalCaptures != 2 {
		t.Fatal("two original capture ordinals did not reach funding", summary)
	}
	economicFundingTestRange(t, summary.Funding.Captured, "25", "6", "6", "19", "19", true)
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	reopened := f.sample(t, monitorServiceHooks{})
	if reopened.Funding.Captured != summary.Funding.Captured || reopened.Funding.OriginalCaptures != 2 {
		t.Fatal("cold same-block funding earned the original deposit again", reopened)
	}
}

// The second original block can earn again; it cannot restore consumed opening
// stock merely because the first capture now resides in a cold checkpoint.
func TestEconomicFundingPublicNextBlockKeepsOriginalIncomeAcrossRetirement(t *testing.T) {
	f, producer, _ := newEconomicCaptureSequenceFixture(t, false)
	first := f.sample(t, monitorServiceHooks{})
	economicFundingTestRange(t, first.Funding.Captured, "20", "6", "6", "14", "14", true)
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	economicCaptureSequenceAdvance(producer)
	second := f.sample(t, monitorServiceHooks{})
	if second.Funding.OriginalCaptures != 2 || second.NativeCursor.Number != 102 || second.CausallyJoinedCaptures != 2 {
		t.Fatal("next original earning interval did not join cold funding", second)
	}
	economicFundingTestRange(t, second.Funding.Captured, "26", "12", "12", "14", "14", true)
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	third := f.sample(t, monitorServiceHooks{})
	if third.Funding.Captured != second.Funding.Captured || third.Funding.OriginalCaptures != 2 || third.TargetMet != nil {
		t.Fatal("next-block archive lost original earning or repeated opening stock", third)
	}
}

// Configure before owner preparation: actual native capture code receives the
// exact receipt transaction hash. The receipt fixture selects a 99% leaf so its
// later payment must consume some of the fourteen units of original stock.
func newEconomicFundingEntitlementFixture(t *testing.T) (*economicConservationArchiveFixture, *economicEntitlementFixture) {
	t.Helper()
	entitlement := newEconomicEntitlementFixture(t, 99)
	vault := entitlement.source.vault
	vault.policy.CaptureIdentity = true
	vault.fixtureGetters = map[string][]any{
		"pools":        {[32]byte(common.HexToHash("0x" + strings.Repeat("11", 32))), uint16(1), true},
		"selfColdkey":  {[32]byte(common.HexToHash("0x" + strings.Repeat("33", 32)))},
		"escrowHotkey": {[32]byte(common.HexToHash("0x" + strings.Repeat("55", 32)))},
	}
	for _, number := range []uint64{12, 13} {
		vault.blocks[number].snapshot = monitorEvmVaultSnapshot("120", "61", "0", "59", "79", "0", "0", vault.policy.Coldkeys[0])
	}
	economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
		if number != 12 {
			return
		}
		sender := common.HexToAddress(vault.policy.FeePayers[0])
		receipt.Logs[1] = monitorEvmTestLog(t, vault.contract, common.HexToAddress(vault.policy.Address), "Claimed", big.NewInt(3), big.NewInt(1), [32]byte(common.HexToHash(vault.policy.Coldkeys[0])), big.NewInt(9900), big.NewInt(49), sender)
		receipt.Logs[2] = monitorEvmTestLog(t, vault.contract, common.HexToAddress(vault.policy.Address), "ClaimPaid", [32]byte(common.HexToHash(vault.policy.Coldkeys[0])), big.NewInt(61), sender)
	})
	// Keep the original one-block observation boundary while rebinding the
	// fixture identity; otherwise the first read skips the missed-root page.
	vault.policy.BatchBlocks = entitlement.source.policy.Vault.BatchBlocks
	entitlement.source.policy.Vault = vault.policy
	f, _ := economicConservationPrincipalFixtureWithSource(t, entitlement.source, "capture", true, func(job *historicalReplayJob) {
		if len(job.ExtrinsicsHex) != 1 {
			t.Fatal("original capture fixture changed body census")
		}
		job.ExtrinsicsHex[0] = nativeExecutionTestHex(append(rootCompact(32), vault.blocks[11].transactions[0].Hash().Bytes()...))
	}, nil)
	return f, entitlement
}

// The sequence export already proves an empty original pool followed by six
// newly earned units and a complete capture. Rebase only its synthetic header
// numbers before admission; original code, trie nodes and both state roots stay
// exact. The owned capture/replay executables still reproduce the full result.
func economicFundingIncomeJob(t *testing.T) historicalReplayJob {
	t.Helper()
	directory := os.Getenv("URNETWORK_NATIVE_VAULT_CAPTURE_SEQUENCE_FIXTURE_DIR")
	if directory == "" {
		t.Fatal("income funding requires the actual original sequence export")
	}
	raw, _, err := readPlanFile(t.Context(), filepath.Join(directory, "principal-capture-next-block-102.json"), historicalNativeJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := decodePlanJson(raw, &job); err != nil || len(job.ExtrinsicsHex) != 1 || !job.PrincipalEffects {
		t.Fatal("original income-only capture job absent", err)
	}
	for index, encoded := range []string{job.ParentHeaderHex, job.ChildHeaderHex} {
		raw, err := historicalReplayHex(encoded, 64*1024)
		if err != nil {
			t.Fatal(err)
		}
		var header substrate.Header
		if err := codec.Decode(raw, &header); err != nil || uint64(header.Number) != uint64(101+index) {
			t.Fatal("original second capture header differs", err)
		}
		header.Number = substrate.BlockNumber(100 + index)
		if index == 1 {
			header.ParentHash = substrate.Hash(job.ParentHash)
		}
		raw, err = codec.Encode(header)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			job.ParentHeaderHex, job.ParentHash = nativeExecutionTestHex(raw), historicalReplayDigest(blake2b.Sum256(raw))
		} else {
			job.ChildHeaderHex, job.ChildHash = nativeExecutionTestHex(raw), historicalReplayDigest(blake2b.Sum256(raw))
		}
	}
	return job
}

// Legitimate earned funds may cross a missed-root boundary and pay a provider.
// Unknown independent measurements/finality/fee coverage still withhold the
// full target predicate; they do not convert proven earnings into capital.
func TestEconomicFundingPublicOriginalIncomeFundsLaterAuthorizedPayment(t *testing.T) {
	entitlement := newEconomicEntitlementFixture(t, 50)
	vault := entitlement.source.vault
	vault.policy.CaptureIdentity = true
	vault.fixtureGetters = map[string][]any{
		"pools":        {[32]byte(common.HexToHash("0x" + strings.Repeat("11", 32))), uint16(1), true},
		"selfColdkey":  {[32]byte(common.HexToHash("0x" + strings.Repeat("33", 32)))},
		"escrowHotkey": {[32]byte(common.HexToHash("0x" + strings.Repeat("55", 32)))},
	}
	for _, number := range []uint64{0, 10} {
		vault.blocks[number].snapshot = monitorEvmVaultSnapshot("0", "0", "0", "0", "0", "0", "0", vault.policy.Coldkeys[0])
	}
	vault.blocks[11].snapshot = monitorEvmVaultSnapshot("6", "0", "0", "6", "6", "6", "0", vault.policy.Coldkeys[0])
	vault.blocks[11].funded["2/1"] = "6"
	for _, number := range []uint64{12, 13} {
		vault.blocks[number].snapshot = monitorEvmVaultSnapshot("6", "3", "0", "3", "3", "0", "0", vault.policy.Coldkeys[0])
	}
	economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
		sender, address := common.HexToAddress(vault.policy.FeePayers[0]), common.HexToAddress(vault.policy.Address)
		switch number {
		case 11:
			commitment := receipt.Logs[len(receipt.Logs)-1]
			receipt.Logs = []*types.Log{
				monitorEvmTestLog(t, vault.contract, address, "EmissionCaptured", big.NewInt(2), big.NewInt(1), [32]byte(common.HexToHash("0x"+strings.Repeat("11", 32))), big.NewInt(6)),
				monitorEvmTestLog(t, vault.contract, address, "RootMissed", big.NewInt(2), big.NewInt(1), big.NewInt(6)),
				commitment,
			}
		case 12:
			receipt.Logs[0] = monitorEvmTestLog(t, vault.contract, address, "EntitlementFinalized", big.NewInt(3), big.NewInt(1), entitlement.committedRoot, entitlement.committedHash, big.NewInt(6), uint64(900))
			receipt.Logs[1] = monitorEvmTestLog(t, vault.contract, address, "Claimed", big.NewInt(3), big.NewInt(1), [32]byte(common.HexToHash(vault.policy.Coldkeys[0])), big.NewInt(5000), big.NewInt(3), sender)
			receipt.Logs[2] = monitorEvmTestLog(t, vault.contract, address, "ClaimPaid", [32]byte(common.HexToHash(vault.policy.Coldkeys[0])), big.NewInt(3), sender)
		}
	})
	// Keep the original one-block observation boundary while rebinding the
	// fixture identity; otherwise the first read skips the missed-root page.
	vault.policy.BatchBlocks = entitlement.source.policy.Vault.BatchBlocks
	entitlement.source.policy.Vault = vault.policy
	// This case observes actual contract credit/payment directly, without
	// borrowing the older fixture's unrelated epoch-one daemon receipt.
	entitlement.source.policy.Claims = nil
	job := economicFundingIncomeJob(t)
	f, _ := economicConservationPrincipalFixtureWithSource(t, entitlement.source, "capture", true, func(selected *historicalReplayJob) {
		*selected = job
		selected.ExtrinsicsHex[0] = nativeExecutionTestHex(append(rootCompact(32), vault.blocks[11].transactions[0].Hash().Bytes()...))
	}, nil)
	first := f.sample(t, monitorServiceHooks{})
	if first.OpeningPrincipalAlpha == nil || *first.OpeningPrincipalAlpha != "0" || first.CausallyJoinedCaptures != 1 {
		t.Fatal("income-only fixture did not prove original empty pool and real capture", first)
	}
	economicFundingTestRange(t, first.Funding.Captured, "6", "6", "6", "0", "0", true)
	second := f.sample(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if record.Census == nil || record.Census.LeafObligationsAlpha != "6" || record.Census.FloorResidueAlpha != "0" || record.Funded != "0" || record.Total == nil || *record.Total != "6" || entitlement.artifactReads.Load() != 2 || second.Funding.NoNonIncomeProviderCredit == nil || !*second.Funding.NoNonIncomeProviderCredit {
		t.Fatal("actual original earnings did not finance later authorized payment", second, record)
	}
	economicFundingTestRange(t, second.Funding.Accepted, "3", "3", "3", "0", "0", true)
	economicFundingTestRange(t, second.Funding.Paid, "3", "3", "3", "0", "0", true)
	if second.TargetMet != nil || second.Conformance.CompleteEvidence || second.OriginalEntitlements.ProviderMeasurementsAuthenticated || second.OriginalEntitlements.IndependentFinalityAuthenticated || second.ActualNativeOutcomeVerified || second.ActivationReady {
		t.Fatal("income proof silently granted independent measurements, finality or fees", second)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	third := f.sample(t, monitorServiceHooks{})
	if third.Funding.Captured != second.Funding.Captured || third.Funding.Paid != second.Funding.Paid || third.Funding.NoNonIncomeProviderCredit == nil || !*third.Funding.NoNonIncomeProviderCredit || third.TargetMet != nil || entitlement.artifactReads.Load() != 2 {
		t.Fatal("cold original income became unknown or was counted again", third)
	}
}

// Original epoch two has twenty captured units; epoch three has no new capture
// yet finalizes fifty with thirty unknown opening carry. At least thirteen of
// its accepted forty-nine units are necessarily non-income.
func TestEconomicFundingPublicMissedRootPaysOriginalStockWithoutEarningItAgain(t *testing.T) {
	f, entitlement := newEconomicFundingEntitlementFixture(t)
	first := f.sample(t, monitorServiceHooks{})
	if first.VaultCursor.Number != 11 || first.CausallyJoinedCaptures != 1 || entitlement.artifactReads.Load() != 0 {
		t.Fatal("original missed-root source page did not complete", first)
	}
	economicFundingTestRange(t, first.Funding.Captured, "20", "6", "6", "14", "14", true)
	second := f.sample(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if record.Census == nil || record.Census.RootSigner != entitlement.rootSigner.Hex() || record.Census.Artifact.Signer == entitlement.rootSigner || record.Funded != "0" || record.Total == nil || *record.Total != "50" || record.Census.LeafObligationsAlpha != "49" || record.Census.FloorResidueAlpha != "1" || len(record.Sources) != 3 || record.Sources[2].Id != "2/1" || record.Sources[2].Kind != "root-missed" || entitlement.artifactReads.Load() != 2 {
		t.Fatal("funding lost original authorized root, source epoch, leaf or residue", record)
	}
	economicFundingTestRange(t, second.Funding.Captured, "20", "6", "6", "14", "14", true)
	economicFundingTestRange(t, second.Funding.Accepted, "56", "5", "43", "13", "51", false)
	economicFundingTestRange(t, second.Funding.Paid, "61", "5", "48", "13", "56", false)
	if second.Funding.OriginalCaptures != 1 || second.Funding.OriginalClaims != 2 || second.Funding.OriginalPayments != 1 || second.Funding.NoNonIncomeProviderCredit == nil || *second.Funding.NoNonIncomeProviderCredit || second.TargetMet == nil || *second.TargetMet || second.Funding.CapitalSubsidyAuthorized || second.Conformance.CompleteEvidence || second.ActualNativeOutcomeVerified || second.ActivationReady {
		t.Fatal("original capital-funded payment became compliant native income", second)
	}
	if second.OriginalEntitlements.NativeIncomeFundingAlpha == nil || *second.OriginalEntitlements.NativeIncomeFundingAlpha != "6" || second.OriginalEntitlements.CapitalFundingAlpha == nil || *second.OriginalEntitlements.CapitalFundingAlpha != "14" {
		t.Fatal("authorized payout root was substituted for original source composition", second)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("original capital/funding archive", code, issue)
	}
	state := f.source.state(t)
	if len(state.Captures) != 0 || len(state.Claims) != 0 || len(state.Payments) != 0 || state.Archive.Counts.Claims != 2 || economicEntitlementRecord(t, f.source).CensusReference == nil {
		t.Fatal("funding control did not retire its real source, claims and census", state)
	}
	third := f.sample(t, monitorServiceHooks{})
	if third.Funding.Captured != second.Funding.Captured || third.Funding.Accepted != second.Funding.Accepted || third.Funding.Paid != second.Funding.Paid || third.Funding.OriginalClaims != 2 || third.TargetMet == nil || *third.TargetMet || entitlement.artifactReads.Load() != 2 {
		t.Fatal("cold missed-root funding lost original capital consumption", third)
	}
}

// A durable write followed by lost acknowledgment cannot recapture the same
// income, repeat a paid leaf, or drop the original capital contradiction.
func TestEconomicFundingPublicLostAckRetainsPaidSourceComposition(t *testing.T) {
	f, entitlement := newEconomicFundingEntitlementFixture(t)
	f.sample(t, monitorServiceHooks{})
	var failed atomic.Bool
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == economicConservationRole && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
			return errors.Join(err, syscall.EIO)
		}
		return err
	}})
	state := f.source.state(t)
	if code != 3 || output.Len() != 0 || !failed.Load() || len(state.Payments) != 1 || state.Payments[0].Event.Values["amount"] != "61" || entitlement.artifactReads.Load() != 2 {
		t.Fatal("lost ACK did not reach original paid checkpoint publication", code, diagnostic.String(), state)
	}
	original := economicEntitlementRecord(t, f.source).censusHash()
	reopened := f.sample(t, monitorServiceHooks{})
	again := f.sample(t, monitorServiceHooks{})
	if reopened.TargetMet == nil || *reopened.TargetMet || again.TargetMet == nil || *again.TargetMet || !reflect.DeepEqual(reopened.Funding, again.Funding) || reopened.Funding.OriginalCaptures != 1 || reopened.Funding.OriginalPayments != 1 || entitlement.artifactReads.Load() != 2 || economicEntitlementRecord(t, f.source).censusHash() != original {
		t.Fatal("checkpoint retry repeated payment or erased original funding", reopened, again)
	}
}

// The public reader still checks original execution after a forged checkpoint
// is self-sealed; aggregate equality cannot relabel opening stock as earnings.
func TestEconomicFundingPublicSelfSealedCaptureCannotAuthorizeCapitalIncome(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, nil)
	f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	state.Captures[0].PrincipalEffects.OpeningStock = "0"
	state.Captures[0].PrincipalEffects.LiquidEarnings = "20"
	state.Captures[0].OpeningPrincipalAlpha = &state.Captures[0].PrincipalEffects.OpeningStock
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := f.source.policy.openHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(append(raw, '\n'), nil), owner.close()); err != nil {
		t.Fatal(err)
	}
	reads := f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || f.source.claimReads.Load() != reads || !strings.Contains(diagnostic.String(), "economic capture causal join differs from original native execution and receipt") {
		t.Fatal("self-sealed equal total replaced original stock with income", code, diagnostic.String())
	}
}

// Complete actual allocation arithmetic can establish the split observation;
// it does not also authenticate unrelated measurements, fees or finality.
func TestEconomicFundingPublicCompleteYumaComparesOriginalTenNinety(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.Conformance == nil || summary.Conformance.NativeSplitWithinTolerance == nil || !*summary.Conformance.NativeSplitWithinTolerance || summary.Conformance.ProviderDeviationNumerator == nil || *summary.Conformance.ProviderDeviationNumerator != "-8" || summary.Conformance.OwnerDeviationNumerator == nil || *summary.Conformance.OwnerDeviationNumerator != "8" || summary.Conformance.NativeSplitTolerance == nil || summary.Yuma == nil || *summary.Yuma.MinerDenominator != "98" {
		t.Fatal("original Yuma denominator did not drive actual native split", summary)
	}
	if summary.TargetMet != nil || summary.Conformance.CompleteEvidence || summary.ActualNativeOutcomeVerified || summary.ActivationReady || len(summary.MissingEvidence) == 0 {
		t.Fatal("selected native arithmetic fabricated whole economic authority", summary)
	}
}
