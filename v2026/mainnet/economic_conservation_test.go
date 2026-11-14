package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestEconomicConservationPublicRetainsOriginalEarningsCarryAndAggregatePayment(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	first, code, issue := f.run(t, monitorServiceHooks{})
	if code != 0 || !first.NativeCurrent || !first.VaultCurrent || first.NativeCursor.Number != 102 || first.VaultCursor.Number != 11 || first.Execution == nil || first.Execution.MinerAllocation != "100" || first.Execution.ProviderEntitlement != "9" || first.Execution.OwnerRecycled != "89" || first.TailGrossAlpha == nil || *first.TailGrossAlpha != "9" || first.RewardCollateralAlpha == nil || *first.RewardCollateralAlpha != "3" || first.MatchedCaptures != 1 || first.MatchedReceipts != 1 {
		t.Fatal("actual public sources did not retain original earning/capture/Claim identities", code, issue, first)
	}
	state := f.state(t)
	if len(state.Lots) != 2 || len(state.Captures) != 1 || state.Captures[0].KnownLiquidAlpha == nil || *state.Captures[0].KnownLiquidAlpha != "6" || *state.Captures[0].AmountDifferenceAlpha != "14" || state.Captures[0].Native.Number != 101 || len(state.Captures[0].Lots) != 1 || state.Captures[0].Lots[0] != state.Lots[0].Id {
		t.Fatal("capture borrowed a gross/collateral amount, timestamp or payment epoch", state)
	}
	firstLot, firstCapture := state.Lots[0].Id, state.Captures[0].Id
	second, code, issue := f.run(t, monitorServiceHooks{})
	state = f.state(t)
	if code != 0 || second.VaultCursor.Number != 12 || second.AggregatePayments != 1 || second.VaultState == nil || second.VaultState.Counters["totalPaid"] != "15" || second.Execution == nil || second.Execution.ProviderEntitlement != "9" || len(state.Lots) != 2 || state.Lots[0].Id != firstLot || state.Captures[0].Id != firstCapture {
		t.Fatal("restart recounted a payment as earning or lost earlier native lots", code, issue, second, state)
	}
	if err := state.index(); err != nil {
		t.Fatal(err)
	}
	missed, paid := state.entitlement("2", "1"), state.entitlement("3", "1")
	if missed.Status != "root-missed" || missed.Funded != "20" || len(missed.Sources) != 1 || missed.Sources[0].Id != firstCapture || paid.Funded != "0" || paid.Total == nil || *paid.Total != "50" || len(paid.Sources) != 3 || paid.Sources[2].Kind != "root-missed" || paid.Sources[2].Id != missed.Id {
		t.Fatal("zero-funded later epoch lost original capture and opening carry", missed, paid)
	}
	if len(state.Payments) != 1 || state.Payments[0].Credit.Opening != "5" || len(state.Payments[0].Credit.Claims) != 2 || state.Payments[0].Status != "aggregate-credit-observed" || len(state.Receipts) != 1 || state.Receipts[0].Observation.PaymentStatus != "deferred" || state.Receipts[0].Observation.UnpaidCreditRao != "12" {
		t.Fatal("aggregate payment rewrote the original receipt or omitted old credit", state.Payments, state.Receipts)
	}
	if second.TargetMet != nil || second.OpeningPrincipalAlpha != nil || second.NativeFeeWithdrawalRao != nil || second.NativeFeeRefundRao != nil || second.FullQuantizationToleranceAlpha != nil || second.ActualNativeOutcomeVerified || second.ActivationReady {
		t.Fatal("observed amounts invented economic authority", second)
	}
}

func TestEconomicConservationPublicLostAckAndShortOutputReplayOnlyOriginalFacts(t *testing.T) {
	for _, mode := range []string{"checkpoint-ack", "output"} {
		f := newEconomicConservationFixture(t, false)
		var diagnostic bytes.Buffer
		var closed atomic.Int32
		hooks := monitorServiceHooks{afterClose: func(role, kind string, file *os.File) error {
			if role == economicConservationRole && kind == "checkpoint" {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("checkpoint survived join")
				}
				closed.Add(1)
			}
			return nil
		}}
		var output io.Writer = &bytes.Buffer{}
		if mode == "checkpoint-ack" {
			hooks.syncDirectory = func(_, _ string, file *os.File) error { return errors.Join(file.Sync(), syscall.EIO) }
		} else {
			output = economicConservationShortWriter{}
		}
		code := runMonitorStorageTestWithHooks(t, t.Context(), f.args(t), output, &diagnostic, func() time.Time { return f.now }, hooks)
		if code != 3 || closed.Load() != 1 {
			t.Fatal("uncertain original publication was acknowledged or owner leaked", mode, code, closed.Load(), diagnostic.String())
		}
		before := f.state(t)
		if before.Native.Cursor.Number != 102 || before.Vault.Cursor.Number != 11 || len(before.Captures) != 1 {
			t.Fatal("lost acknowledgment fixture did not reach real publication", mode, before)
		}
		summary, code, issue := f.run(t, monitorServiceHooks{})
		after := f.state(t)
		if code != 0 || summary.VaultCursor.Number != 12 || len(after.Captures) != 1 || len(after.Lots) != len(before.Lots) || after.Captures[0].Id != before.Captures[0].Id || len(after.Payments) != 1 {
			t.Fatal("restart duplicated or lost original financial facts", mode, code, issue, after)
		}
	}
}

type economicConservationShortWriter struct{}

func (economicConservationShortWriter) Write(raw []byte) (int, error) { return len(raw) - 1, nil }

func TestEconomicConservationPublicOutageRetainsOneDomainAndRecoversOriginalPage(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	f.policy.Vault.BatchBlocks = 3
	f.writePolicy(t)
	f.vault.unavailable.Store(true)
	var waits atomic.Int32
	summary, code, issue := f.run(t, monitorServiceHooks{rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
		deadline, ok := ctx.Deadline()
		if role != f.policy.Vault.Role || !ok || time.Until(deadline) < time.Minute || time.Until(deadline) > 300*time.Second {
			return errors.New("one logical read budget was lost")
		}
		waits.Add(1)
		return context.DeadlineExceeded
	}})
	if code != 3 || !summary.NativeCurrent || summary.VaultCurrent || summary.NativeCursor.Number != 102 || summary.VaultCursor != f.policy.Vault.From || waits.Load() != 1 || summary.VaultState != nil || summary.VaultIssue == "" {
		t.Fatal("partial vault read replaced native progress or fabricated zero", code, issue, summary, waits.Load())
	}
	before := f.state(t)
	if len(before.Captures) != 0 || len(before.Lots) != 2 || before.OpeningVault != nil {
		t.Fatal("uncompleted vault page escaped into custody", before)
	}
	f.vault.unavailable.Store(false)
	summary, code, issue = f.run(t, monitorServiceHooks{})
	after := f.state(t)
	if code != 0 || summary.VaultCursor.Number != 13 || len(after.Captures) != 1 || len(after.Payments) != 1 || after.Lots[0].Id != before.Lots[0].Id || summary.MatchedReceipts != 1 {
		t.Fatal("same original page could not recover after bounded outage", code, issue, summary)
	}
}

func TestEconomicConservationPublicEqualAggregateRecipientForgeryFailsCustody(t *testing.T) {
	f := newEconomicConservationFixture(t, true)
	summary, code, issue := f.run(t, monitorServiceHooks{})
	if code != 0 || summary.Execution == nil || summary.Execution.ProviderEntitlement != "98" || summary.DirectGrossAlpha == nil || *summary.DirectGrossAlpha != "89" {
		t.Fatal("two actual admitted recipient baseline failed", code, issue, summary)
	}
	state := f.state(t)
	if len(state.Lots) != 2 || !state.Lots[0].Effect.Provider || !state.Lots[1].Effect.Provider {
		t.Fatal("forgery needs two real provider effects", state.Lots)
	}
	// Redistribute one unit while leaving gross/collateral aggregate totals,
	// original identities and the legacy aggregate hash unchanged. A companion
	// hash alone cannot supply authority; retained inode/bytes must refuse it.
	state.Lots[0].Effect.Gross, state.Lots[0].Effect.Liquid = "10", "7"
	state.Lots[1].Effect.Gross, state.Lots[1].Effect.Liquid = "88", "88"
	for index := range state.Lots {
		state.Lots[index].Id = economicConservationLotId(state.Lots[index].ProjectionHash, state.Lots[index].Effect)
	}
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.checkpoint, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	countReads := func() int {
		f.native.chain.stateLock.Lock()
		defer f.native.chain.stateLock.Unlock()
		total := 0
		for _, count := range f.native.chain.counts {
			total += count
		}
		return total
	}
	before := countReads()
	_, code, issue = f.run(t, monitorServiceHooks{})
	if code != 3 || issue == "" {
		t.Fatal("self-sealed equal-aggregate redistribution acquired public authority", code, issue)
	}
	actual, err := os.ReadFile(f.checkpoint)
	if err != nil || !bytes.Equal(actual, append(raw, '\n')) {
		t.Fatal("refusal rewrote original tampered evidence", err)
	}
	after := countReads()
	if before != after {
		t.Fatal("unadmitted checkpoint reached new source reads", before, after)
	}
}

// Two original captures leave five and three atomic alpha unclaimed. The
// next epoch captures nothing, yet legitimately pays 103.32 alpha in total.
// Real ABI logs, signed bodies, receipt tries and block getters are read by the
// public adapter; no fixture supplies the reconciliation verdict.
func TestEconomicConservationPublicRootMissed308Pays309WithoutNewIncome(t *testing.T) {
	f := newEconomicConservationFixture(t, false, func(vault *monitorEvmFixture) {
		vault.policy.PoolIds = []string{"1", "2"}
		vault.policy.Coldkeys = nil
		for index := 0; index < 10; index++ {
			vault.policy.Coldkeys = append(vault.policy.Coldkeys, common.BytesToHash(bytes.Repeat([]byte{byte(0x41 + index)}, 32)).Hex())
		}
		snapshot := func(captured, paid, liability, carry1, carry2 string) monitorEconomicEvmSnapshot {
			credits := map[string]string{}
			for _, coldkey := range vault.policy.Coldkeys {
				credits[coldkey] = "0"
			}
			return monitorEconomicEvmSnapshot{Counters: map[string]string{"totalCaptured": captured, "totalPaid": paid, "pendingFunding": "0", "outstandingLiability": liability, "escrowAccounted": liability, "liveEscrowStake": liability}, Pools: map[string]string{"1": carry1, "2": carry2}, Credits: credits}
		}
		vault.blocks[0].snapshot = snapshot("0", "0", "0", "0", "0")
		vault.blocks[10].snapshot = snapshot("0", "0", "0", "0", "0")
		vault.blocks[11].snapshot = snapshot("103320000008", "0", "103320000008", "51660000005", "51660000003")
		vault.blocks[12].snapshot = snapshot("103320000008", "103320000000", "8", "0", "0")
		vault.blocks[13].snapshot = vault.blocks[12].snapshot.clone()
		vault.blocks[11].funded = map[string]string{"308/1": "51660000005", "308/2": "51660000003"}
		vault.blocks[12].funded = map[string]string{"309/1": "0", "309/2": "0"}
		economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
			address := common.HexToAddress(vault.policy.Address)
			sender := common.HexToAddress(vault.blocks[number].receipts[0]["from"].(string))
			receipt.Logs = nil
			for index, amount := range []string{"51660000005", "51660000003"} {
				pool := mustMonitorEvmInteger([]string{"1", "2"}[index])
				if number == 11 {
					hotkey := "0x" + strings.Repeat([]string{"11", "22"}[index], 32)
					receipt.Logs = append(receipt.Logs, monitorEvmTestLog(t, vault.contract, address, "EmissionCaptured", mustMonitorEvmInteger("308"), pool, [32]byte(common.HexToHash(hotkey)), mustMonitorEvmInteger(amount)), monitorEvmTestLog(t, vault.contract, address, "RootMissed", mustMonitorEvmInteger("308"), pool, mustMonitorEvmInteger(amount)))
				} else if number == 12 {
					receipt.Logs = append(receipt.Logs, monitorEvmTestLog(t, vault.contract, address, "EntitlementFinalized", mustMonitorEvmInteger("309"), pool, [32]byte(common.HexToHash("0x"+strings.Repeat("73", 32))), [32]byte(common.HexToHash("0x"+strings.Repeat("74", 32))), mustMonitorEvmInteger(amount), uint64(900)))
					for _, coldkey := range vault.policy.Coldkeys {
						receipt.Logs = append(receipt.Logs, monitorEvmTestLog(t, vault.contract, address, "Claimed", mustMonitorEvmInteger("309"), pool, [32]byte(common.HexToHash(coldkey)), mustMonitorEvmInteger("1000"), mustMonitorEvmInteger("5166000000"), sender))
					}
				}
			}
			if number == 12 {
				for _, coldkey := range vault.policy.Coldkeys {
					receipt.Logs = append(receipt.Logs, monitorEvmTestLog(t, vault.contract, address, "ClaimPaid", [32]byte(common.HexToHash(coldkey)), mustMonitorEvmInteger("10332000000"), sender))
				}
			}
		})
	})
	f.policy.Claims = nil
	f.writePolicy(t)
	if _, code, issue := f.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("actual capture/missed-root baseline", code, issue)
	}
	before := f.state(t)
	if len(before.Captures) != 2 || len(before.Payments) != 0 {
		t.Fatal("fixture missed the original308 source", before)
	}
	summary, code, issue := f.run(t, monitorServiceHooks{})
	state := f.state(t)
	if code != 0 || summary.VaultState == nil || summary.VaultState.Counters["totalPaid"] != "103320000000" || summary.VaultState.Counters["outstandingLiability"] != "8" || len(state.Captures) != 2 || len(state.Claims) != 20 || len(state.Payments) != 10 || len(state.Lots) != len(before.Lots) {
		t.Fatal("zero-capture309 repeated earnings or erased original308 carry/residue", code, issue, summary)
	}
	if err := state.index(); err != nil {
		t.Fatal(err)
	}
	for index, pool := range []string{"1", "2"} {
		earlier, later := state.entitlement("308", pool), state.entitlement("309", pool)
		if later.Funded != "0" || later.Total == nil || *later.Total != earlier.Funded || later.Claimed != "51660000000" || len(later.Sources) != 3 || later.Sources[2].Id != earlier.Id || earlier.Sources[0].Id != before.Captures[index].Id {
			t.Fatal("payment epoch replaced source entitlement", earlier, later)
		}
		if *later.Total != []string{"51660000005", "51660000003"}[index] {
			t.Fatal("five/three original residues changed", later)
		}
	}
	for _, payment := range state.Payments {
		if len(payment.Credit.Claims) != 2 || payment.Credit.Opening != "0" {
			t.Fatal("aggregate receipt lost cross-pool original claims", payment)
		}
	}
	if state.Captures[1].KnownLiquidAlpha != nil || state.Captures[1].Status != "independent-native-pool-route-unavailable" || summary.TargetMet != nil {
		t.Fatal("unmatched pool became zero or full target evidence", state.Captures[1], summary)
	}
}

func TestEconomicConservationPublicClaimContradictionRetainsOriginalReceipt(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	if _, code, issue := f.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("baseline", code, issue)
	}
	f.claimFault.Store(true)
	summary, code, issue := f.run(t, monitorServiceHooks{})
	state := f.state(t)
	if code != 3 || !summary.NativeCurrent || !summary.VaultCurrent || summary.VaultCursor.Number != 12 || len(summary.ClaimStatuses) != 1 || summary.ClaimStatuses[0] != "contradiction" || len(state.Receipts) != 1 || state.Receipts[0].Observation.AcceptedAmountRao != "7" || len(state.Payments) != 1 {
		t.Fatal("changed original receipt replaced retained claim or stopped healthy vault", code, issue, summary, state.Receipts)
	}
}

func TestEconomicConservationPublicFollowJoinsOwnedReadsAndOutput(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output, diagnostic bytes.Buffer
	var samples, closed atomic.Int32
	args := append(f.args(t), "--follow")
	hooks := monitorServiceHooks{afterEvent: func(_ context.Context, role string) {
		if role == economicConservationRole && samples.Add(1) == 2 {
			cancel()
		}
	}, wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil }, afterClose: func(_, _ string, file *os.File) error {
		closed.Add(1)
		_, err := file.Stat()
		if !errors.Is(err, os.ErrClosed) {
			return errors.New("live owner")
		}
		return nil
	}}
	code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 0 || samples.Load() != 2 || closed.Load() != 1 || bytes.Count(output.Bytes(), []byte{'\n'}) != 2 {
		t.Fatal("public continuous owner did not join after real second sample", code, samples.Load(), closed.Load(), diagnostic.String())
	}
	state := f.state(t)
	if state.Vault.Cursor.Number != 12 || len(state.Payments) != 1 || len(state.Lots) != 2 {
		t.Fatal("follow reset or double-counted original facts", state)
	}
}

func TestEconomicConservationPublicInputsCannotImportProjectionOrChangeDomain(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	var output, diagnostic bytes.Buffer
	args := append(f.args(t), "--projection-file", filepath.Join(filepath.Dir(f.path), "self-sealed.json"))
	if code := runMonitorStorageTestWithHooks(t, t.Context(), args, &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{}); code != 2 || output.Len() != 0 {
		t.Fatal("public ingress accepted a caller projection", code, diagnostic.String())
	}
	if _, code, issue := f.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("actual baseline", code, issue)
	}
	before, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	f.policy.Routes[0].Coldkey = "0x" + strings.Repeat("44", 32)
	f.writePolicy(t)
	if _, code, issue := f.run(t, monitorServiceHooks{}); code != 3 || !strings.Contains(issue, "policy") {
		t.Fatal("changed economic domain reopened original cursor", code, issue)
	}
	after, err := os.ReadFile(f.checkpoint)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("foreign domain mutated original checkpoint", err)
	}
}

func TestEconomicNativeRecipientProjectionPreservesLegacyCompletionAndAuthority(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, nil)
	observation, code, issue := nativeExecutionTestCommand(t, f)
	if code != 0 || len(observation.Blocks) != 2 || observation.Blocks[0].ExecutionOutcome == nil {
		t.Fatal("actual projection baseline", code, issue)
	}
	outcome := *observation.Blocks[0].ExecutionOutcome
	projection := outcome.RecipientEffects
	if err := projection.validate(outcome); err != nil || len(projection.Effects) != 2 || projection.Effects[0].Gross != "9" || projection.Effects[0].Liquid != "6" || projection.Effects[0].Collateral != "3" || projection.Effects[1].Recycled != "89" || projection.Parent != f.policy.From {
		t.Fatal("original effect projection absent", projection, err)
	}
	legacy := outcome
	legacy.RecipientEffects, legacy.ContentHash = nil, ""
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if monitorReadDigest(raw) != outcome.ContentHash {
		t.Fatal("additive companion changed original serialized completion grammar")
	}
	outcome.ProducerAuthorityHash, outcome.FinalityProofHash = rootObjectHash("independent producer authority"), rootObjectHash("certified finality")
	outcome.ContentHash = outcome.hash()
	if err := projection.validate(outcome); err != nil {
		t.Fatal("later producer attachment invalidated original amounts", err)
	}
	changed := *projection
	changed.Effects = append([]nativeExecutionEffect(nil), projection.Effects...)
	changed.Effects[0].Recipient.Registered++
	changed.ContentHash = changed.hash()
	if err := changed.validate(outcome); err == nil {
		t.Fatal("equal aggregates admitted a replaced original recipient generation")
	}
	legacy.RecipientEffects = nil
	if err := legacy.RecipientEffects.validate(legacy); err == nil {
		t.Fatal("missing old projection became explicit zero")
	}
}

func TestEconomicConservationPublicRefusesDuplicateCaptureAfterCompleteVaultRead(t *testing.T) {
	f := newEconomicConservationFixture(t, false, func(vault *monitorEvmFixture) {
		coldkey := vault.policy.Coldkeys[0]
		vault.blocks[11].snapshot = monitorEvmVaultSnapshot("140", "0", "0", "140", "160", "70", "12", coldkey)
		vault.blocks[11].funded["2/1"] = "40"
		economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
			if number != 11 {
				return
			}
			address := common.HexToAddress(vault.policy.Address)
			duplicate := monitorEvmTestLog(t, vault.contract, address, "EmissionCaptured", mustMonitorEvmInteger("2"), mustMonitorEvmInteger("1"), [32]byte(common.HexToHash("0x"+strings.Repeat("11", 32))), mustMonitorEvmInteger("20"))
			missed := monitorEvmTestLog(t, vault.contract, address, "RootMissed", mustMonitorEvmInteger("2"), mustMonitorEvmInteger("1"), mustMonitorEvmInteger("40"))
			receipt.Logs = append([]*types.Log{receipt.Logs[0], duplicate, missed}, receipt.Logs[2:]...)
		})
	})
	summary, code, issue := f.run(t, monitorServiceHooks{})
	state := f.state(t)
	if code != 3 || !summary.NativeCurrent || !summary.VaultHeld || !strings.Contains(summary.VaultIssue, "capture repeated an original epoch") || state.Vault.Cursor != f.policy.Vault.From || len(state.Captures) != 0 || len(state.Lots) != 2 {
		t.Fatal("repeated epoch escaped exact event/refusal boundary", code, issue, summary)
	}
}

func TestEconomicConservationPublicMissingMappingCannotUseNearbyHeight(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	// Both sources are independently complete, but the new replay input no
	// longer has Frontier digests. Same sample time/nearby height is no join.
	parent := f.native.chain.byHeight[100]
	for number := uint64(101); number <= 102; number++ {
		old := f.native.chain.byHeight[number]
		header, hash := rootReceiptHeaderFixture(t, parent, number, nil, false)
		f.native.chain.headers[hash], f.native.chain.bodies[hash], f.native.storageKVs[hash] = header, []string{}, f.native.storageKVs[old]
		delete(f.native.chain.headers, old)
		delete(f.native.chain.bodies, old)
		delete(f.native.storageKVs, old)
		f.native.chain.byHeight[number], parent = hash, hash
	}
	f.native.chain.finalized, f.native.policy.Through.Hash = parent, parent
	nativeExecutionTestConfigure(t, f.native, nil)
	f.policy.Native.Observation = f.native.policy
	f.writePolicy(t)
	summary, code, issue := f.run(t, monitorServiceHooks{})
	state := f.state(t)
	if code != 0 || !summary.NativeCurrent || !summary.VaultCurrent || summary.MatchedCaptures != 0 || len(state.Mappings) != 0 || len(state.Captures) != 1 || state.Captures[0].KnownLiquidAlpha != nil || state.Captures[0].Native != nil || summary.TargetMet != nil {
		t.Fatal("timestamp/height proximity fabricated native capture authority", code, issue, summary, state.Captures)
	}
}

func TestEconomicConservationPublicMatchingAmountDoesNotProvePrincipalFeesOrTarget(t *testing.T) {
	f := newEconomicConservationFixture(t, false, func(vault *monitorEvmFixture) {
		vault.blocks[11].snapshot = monitorEvmVaultSnapshot("106", "0", "0", "106", "126", "36", "12", vault.policy.Coldkeys[0])
		vault.blocks[11].funded["2/1"] = "6"
		economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
			if number != 11 {
				return
			}
			address := common.HexToAddress(vault.policy.Address)
			receipt.Logs[0] = monitorEvmTestLog(t, vault.contract, address, "EmissionCaptured", mustMonitorEvmInteger("2"), mustMonitorEvmInteger("1"), [32]byte(common.HexToHash("0x"+strings.Repeat("11", 32))), mustMonitorEvmInteger("6"))
			receipt.Logs[1] = monitorEvmTestLog(t, vault.contract, address, "RootMissed", mustMonitorEvmInteger("2"), mustMonitorEvmInteger("1"), mustMonitorEvmInteger("6"))
		})
	})
	summary, code, issue := f.run(t, monitorServiceHooks{})
	state := f.state(t)
	if code != 0 || len(state.Captures) != 1 || state.Captures[0].AmountDifferenceAlpha == nil || *state.Captures[0].AmountDifferenceAlpha != "0" || state.Captures[0].Status != "amount-equal-principal-and-stake-effects-unproved" {
		t.Fatal("exact public candidate amount did not reconcile", code, issue, state.Captures)
	}
	if summary.TargetMet != nil || summary.OpeningPrincipalAlpha != nil || summary.NativeFeeWithdrawalRao != nil || summary.NativeFeeRefundRao != nil || summary.FullQuantizationToleranceAlpha != nil || summary.ActualNativeOutcomeVerified || summary.ActivationReady {
		t.Fatal("matching amount invented complete economic authority", summary)
	}
}

func TestEconomicConservationPublicCapacityRetainsOriginalLiabilities(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	// The deliberately small original profile admits exactly four events and
	// one fee from block11. Block12 must hold without losing carry or credit.
	f.policy.Vault.HistoryEntries = 5
	f.writePolicy(t)
	first, code, issue := f.run(t, monitorServiceHooks{})
	if code != 0 || !first.CapacityWarning {
		t.Fatal("bounded public baseline", code, issue, first)
	}
	before := f.state(t)
	second, code, issue := f.run(t, monitorServiceHooks{})
	after := f.state(t)
	if code != 3 || second.VaultCurrent || !second.NativeCurrent || second.VaultHeld || second.VaultIssue == "" || !second.CapacityWarning || after.Vault.Cursor != before.Vault.Cursor || after.Vault.BatchChainHash != before.Vault.BatchChainHash || len(after.Captures) != 1 || after.Captures[0].Id != before.Captures[0].Id || len(after.Payments) != 0 || after.Vault.Snapshot.Credits[f.policy.Vault.Coldkeys[0]] != "12" || after.Vault.Snapshot.Pools["1"] != "50" {
		t.Fatal("capacity became zero debt, history eviction or a false identity conflict", code, issue, second)
	}
}

func TestEconomicConservationPublicCancellationJoinsInFlightReadWithoutAmounts(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	entered := make(chan struct{})
	var seen atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return
		}
		var call struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			http.Error(w, "decode", 400)
			return
		}
		if call.Method == "chain_getFinalizedHead" {
			if seen.CompareAndSwap(false, true) {
				close(entered)
			}
			<-r.Context().Done()
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		f.native.chain.serve(w, r)
	}))
	defer server.Close()
	f.native.client.url = server.URL
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output, diagnostic bytes.Buffer
	var closed atomic.Int32
	args := f.args(t)
	ctx = monitorTestStorageContext(t, ctx, args)
	done := make(chan struct{})
	var code int
	go func() {
		defer close(done)
		code = runMainWithMonitorHooks(ctx, args, &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{afterClose: func(_, _ string, _ *os.File) error { closed.Add(1); return nil }})
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("owned command did not join during fixture cleanup")
		}
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatal("cancellation fixture did not reach actual read", code, diagnostic.String())
	case <-time.After(30 * time.Second):
		t.Fatal("actual source request did not start")
	}
	cancel()
	select {
	case <-done:
		if code != 0 || output.Len() != 0 || closed.Load() != 1 {
			t.Fatal("canceled sample acquired/published amounts or leaked custody", code, closed.Load(), diagnostic.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("canceled sample did not join source readers")
	}
	if _, err := os.Stat(f.checkpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled first sample published a false empty checkpoint", err)
	}
}
