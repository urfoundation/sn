// Whole-body coverage is independent of selected transaction requests. These
// deterministic reducer controls preserve unknown original fees through cold
// retirement; separate public tests below use both actual original-Wasm ELFs.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/blake2b"
)

// This synthetic roster is fixed before signing the original producer scope.
// The observed fee payer belongs to it; the unrelated native payer does not.
func nativeWholeFeeTestAuthority(policy economicConservationPolicy) *nativeFeeCensusPolicy {
	accounts := append([]string(nil), policy.Native.Observation.FeePayers...)
	accounts = append(accounts, policy.Vault.Coldkeys...)
	for _, route := range policy.Routes {
		accounts = append(accounts, route.Hotkey, route.Coldkey)
	}
	for _, payer := range policy.Vault.FeePayers {
		address := common.HexToAddress(payer)
		digest := blake2b.Sum256(append([]byte("evm:"), address[:]...))
		accounts = append(accounts, economicNativeFeeHash(historicalReplayDigest(digest)))
	}
	for _, value := range []byte{0x11, 0x22, 0x33, 0x34} {
		accounts = append(accounts, nativeExecutionTestHex(bytes.Repeat([]byte{value}, 32)))
	}
	digest := blake2b.Sum256(append([]byte("evm:"), bytes.Repeat([]byte{29}, 20)...))
	accounts = append(accounts, economicNativeFeeHash(historicalReplayDigest(digest)))
	slices.Sort(accounts)
	return &nativeFeeCensusPolicy{Schema: nativeFeeCensusSchema, ReviewSha256: monitorReadDigest([]byte("synthetic complete original fee paths and participant authority")), Participants: slices.Compact(accounts)}
}

// Independent synthetic typed events exercise the retained arithmetic grammar,
// not new public ingress. Public ingress below always derives them in the VM.
func nativeWholeFeeTestProjection(t *testing.T, parent economicEmissionBoundary, mode string) (*nativeFeeCensusPolicy, nativeFeeCensusProjection) {
	t.Helper()
	policy := nativeWholeFeeTestAuthority(economicConservationPolicy{})
	source := historicalReplayAddress{}
	copy(source[:], bytes.Repeat([]byte{29}, 20))
	payer := historicalReplayDigest(blake2b.Sum256(append([]byte("evm:"), source[:]...)))
	transaction := historicalReplayDigest{41}
	amount, refund := "1000", "250"
	index := uint32(0)
	events := []historicalReplayFeeEvent{{ObservationOrdinal: 1, Purpose: "fee-withdraw", Phase: "apply-extrinsic", ExtrinsicIndex: &index, Event: "Balances.Withdraw", Payer: &payer, AmountRao: &amount, EventSha256: historicalReplayDigest{1}}}
	if mode == "complete" {
		events = append(events, historicalReplayFeeEvent{ObservationOrdinal: 2, Purpose: "fee-refund", Phase: "apply-extrinsic", ExtrinsicIndex: &index, Event: "Balances.Deposit", Payer: &payer, AmountRao: &refund, EventSha256: historicalReplayDigest{2}})
	}
	events = append(events, historicalReplayFeeEvent{ObservationOrdinal: 3, Purpose: "ethereum-executed", Phase: "apply-extrinsic", ExtrinsicIndex: &index, Event: "Ethereum.Executed", Source: &source, TransactionHash: &transaction, EventSha256: historicalReplayDigest{3}})
	entry := nativeFeeCensusExtrinsic{Index: 0, Hash: "0x" + strings.Repeat("7", 64), Events: events}
	if mode == "zero-refund" {
		entry.RefundZero = nativeWholeFeeTestBranch(0, 4, false)
	}
	entry, err := classifyNativeFeeExtrinsic(policy, entry)
	if err != nil {
		t.Fatal(err)
	}
	entries := []nativeFeeCensusExtrinsic{entry}
	if mode == "quiet" {
		entries = []nativeFeeCensusExtrinsic{}
	}
	projection := nativeFeeCensusProjection{Schema: nativeFeeCensusSchema, PolicyHash: rootObjectHash(policy), Parent: parent, Boundary: economicEmissionBoundary{Number: parent.Number + 1, Hash: rootExtrinsicHash([]byte(fmt.Sprintf("original synthetic fee child %d", parent.Number+1)))}, OutcomeHash: monitorReadDigest([]byte("original outcome")), JobHash: monitorReadDigest([]byte("original job")), TraceHash: monitorReadDigest([]byte("original trace")), Extrinsics: entries, Unplaced: []historicalReplayFeeEvent{}}
	projection.ContentHash = projection.hash()
	if err := projection.validate(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	return policy, projection
}

// Original branch phase and captured result are explicit, never nil-as-zero.
func nativeWholeFeeTestBranch(index uint32, ordinal uint64, exempt bool) *historicalReplayObservation {
	phase := make([]byte, 5)
	binary.LittleEndian.PutUint32(phase[1:], index)
	phaseHex, returned := nativeExecutionTestHex(phase), "0x01"
	purpose, name, memory := "native-fee-refund-zero", "refund", "0x0000000000000000"
	if exempt {
		purpose, name, memory = "native-fee-exempt", "pays-fee", "0x01"
	}
	return &historicalReplayObservation{Ordinal: ordinal, Purpose: purpose, Operation: "get", KeyHex: "0x01", StorageReturn: &historicalStorageReturn{Present: true, ValueHex: &returned}, Native: &historicalNativeObservation{ExecutionPhaseHex: &phaseHex, Memory: []historicalNativeMemory{{Name: name, Address: 100, BytesHex: memory}}}}
}

// A private empty owner admits only the supplied original interval. No helper
// promotes independent measurement or finality authority for conformance.
func nativeWholeFeeTestState(t *testing.T, mode string) (economicConservationPolicy, *economicConservationState) {
	t.Helper()
	from := economicEmissionBoundary{Number: 100, Hash: "0x" + strings.Repeat("6", 64)}
	fees, projection := nativeWholeFeeTestProjection(t, from, mode)
	policy := economicConservationPolicy{Native: monitorEconomicNativePolicy{Observation: economicEmissionPolicy{From: from, Execution: &nativeExecutionPolicy{FeeCensus: fees}}}, MaximumFacts: 1024}
	view := newEconomicConservationArchiveView(policy.initialResources())
	view.finalityPolicy = &policy.Native.Observation
	state := &economicConservationState{Native: monitorEconomicNativeState{Cursor: projection.Boundary}, OriginalFees: []nativeFeeCensusProjection{projection}, archiveView: view}
	return policy, state
}

// Quiet original bodies are complete, but no observed block is not a census.
func TestEconomicWholeFeeQuietAndMissingRefundStayDistinct(t *testing.T) {
	for _, mode := range []string{"quiet", "missing-refund", "zero-refund", "complete"} {
		policy, state := nativeWholeFeeTestState(t, mode)
		value, err := state.wholeFeeSummary(t.Context(), policy)
		if err != nil || value == nil || value.Complete != (mode != "missing-refund") || value.Head.Blocks != 1 {
			t.Fatal("original body completeness changed", mode, value, err)
		}
		if mode == "missing-refund" {
			if value.WithdrawalRao != nil || value.RefundRao != nil || value.Head.Missing != 1 {
				t.Fatal("unobserved refund became zero", value)
			}
		} else if value.WithdrawalRao == nil || value.RefundRao == nil || value.DebitRao == nil {
			t.Fatal("complete original body lost exact totals", mode, value)
		}
		if mode == "zero-refund" && (*value.RefundRao != "0" || *value.DebitRao != "1000") {
			t.Fatal("observed zero branch lost original debit", value)
		}
		if mode == "quiet" && (*value.WithdrawalRao != "0" || value.Head.Extrinsics != 0) {
			t.Fatal("quiet body invented participant fees", value)
		}
	}
}

// Every complete body entry needs an observed original charge or Pays::No.
func TestEconomicWholeFeeUnobservedExtrinsicCannotBorrowSelectedTransaction(t *testing.T) {
	policy, state := nativeWholeFeeTestState(t, "complete")
	block := &state.OriginalFees[0]
	entry, err := classifyNativeFeeExtrinsic(policy.Native.Observation.Execution.FeeCensus, nativeFeeCensusExtrinsic{Index: 1, Hash: "0x" + strings.Repeat("8", 64), Events: []historicalReplayFeeEvent{}})
	if err != nil {
		t.Fatal(err)
	}
	block.Extrinsics = append(block.Extrinsics, entry)
	block.ContentHash = block.hash()
	value, err := state.wholeFeeSummary(t.Context(), policy)
	if err != nil || value.Complete || value.Head.Missing != 1 || value.WithdrawalRao != nil {
		t.Fatal("selected original fee pair covered an unobserved body entry", value, err)
	}
	block.Extrinsics[1].Exemption = nativeWholeFeeTestBranch(1, 4, true)
	entry, err = classifyNativeFeeExtrinsic(policy.Native.Observation.Execution.FeeCensus, block.Extrinsics[1])
	if err != nil {
		t.Fatal(err)
	}
	block.Extrinsics[1], block.ContentHash = entry, ""
	block.ContentHash = block.hash()
	value, err = state.wholeFeeSummary(t.Context(), policy)
	if err != nil || !value.Complete || value.Head.Extrinsics != 2 || *value.WithdrawalRao != "1000" || *value.DebitRao != "750" {
		t.Fatal("actual original no-fee branch failed complete coverage", value, err)
	}
}

// Equal totals cannot replace original payer, placement, phase or ordinal.
func TestEconomicWholeFeeContradictoryOriginalEventsAreRefused(t *testing.T) {
	for _, cause := range []string{"payer", "ordinal", "placement", "exemption", "zero", "body"} {
		policy, state := nativeWholeFeeTestState(t, "complete")
		block := &state.OriginalFees[0]
		switch cause {
		case "payer":
			payer := historicalReplayDigest{5}
			block.Extrinsics[0].Events[1].Payer = &payer
		case "ordinal":
			block.Extrinsics[0].Events[1].ObservationOrdinal = 1
		case "placement":
			index := uint32(1)
			block.Extrinsics[0].Events[1].ExtrinsicIndex = &index
		case "exemption":
			block.Extrinsics[0].Exemption = nativeWholeFeeTestBranch(0, 4, true)
		case "zero":
			block.Extrinsics[0].RefundZero = nativeWholeFeeTestBranch(0, 4, false)
		case "body":
			block.Extrinsics[0].Index = 1
		}
		block.ContentHash = block.hash()
		if value, err := state.wholeFeeSummary(t.Context(), policy); err == nil || value != nil {
			t.Fatal("contradictory original fee census became complete", cause, value, err)
		}
	}
}

// A missing original interval and an unplaced fee are different deficiencies;
// neither is repaired by a matching selected total from another boundary.
func TestEconomicWholeFeeExactIntervalAndUnplacedEventsRefuseFalseCoverage(t *testing.T) {
	policy, state := nativeWholeFeeTestState(t, "complete")
	state.Native.Cursor.Number++
	if value, err := state.wholeFeeSummary(t.Context(), policy); err == nil || value != nil {
		t.Fatal("original body gap acquired complete interval coverage", value, err)
	}
	state.Native.Cursor = state.OriginalFees[0].Boundary
	block := &state.OriginalFees[0]
	event := block.Extrinsics[0].Events[0]
	event.Phase, event.ExtrinsicIndex, event.ObservationOrdinal = "finalization", nil, 4
	block.Unplaced = append(block.Unplaced, event)
	block.ContentHash = block.hash()
	value, err := state.wholeFeeSummary(t.Context(), policy)
	if err != nil || value.Complete || value.Head.Unplaced != 1 || value.DebitRao != nil {
		t.Fatal("unplaced original fee was assigned a convenient body index", value, err)
	}
}

// Retirement keeps a pending original block while complete later blocks use
// one admitted prefix. Repeated reads charge no history and repeat no amounts.
func TestEconomicWholeFeeArchiveRetainsUnknownAndUsesBoundedPrefix(t *testing.T) {
	policy, state := nativeWholeFeeTestState(t, "missing-refund")
	originalView := state.archiveView
	compacted := *state
	compacted.Archive = &economicConservationArchive{}
	if err := compacted.retireWholeFees(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if err := originalView.retainWholeFees(t.Context(), state, &compacted); err != nil {
		t.Fatal(err)
	}
	compacted.archiveView = originalView
	_, next := nativeWholeFeeTestProjection(t, state.Native.Cursor, "complete")
	compacted.OriginalFees = append(compacted.OriginalFees, next)
	compacted.Native.Cursor = next.Boundary
	var prior *economicWholeFeeSummary
	chargedEntries, chargedBytes := originalView.entries, originalView.bytes
	for range 3 {
		value, err := compacted.wholeFeeSummary(t.Context(), policy)
		if err != nil || value.Complete || value.Head.Blocks != 2 || value.Head.Missing != 1 || value.Head.WithdrawalRao != "1000" || originalView.entries != chargedEntries || originalView.bytes != chargedBytes {
			t.Fatal("retained unknown was dropped or prefix was recounted", value, err)
		}
		if prior != nil && !reflect.DeepEqual(prior, value) {
			t.Fatal("same original fee read changed totals", prior, value)
		}
		prior = value
	}
	compacted.OriginalFees = compacted.OriginalFees[1:]
	if value, err := compacted.wholeFeeSummary(t.Context(), policy); err == nil || value != nil {
		t.Fatal("archive accepted omission of unresolved original fee", value, err)
	}
}

// Capacity refusal and a lost publication candidate leave the held original
// index unchanged; no shared map mutation can convert retry into double debt.
func TestEconomicWholeFeeRefusedCandidateCannotMutateOriginalPrefix(t *testing.T) {
	_, state := nativeWholeFeeTestState(t, "missing-refund")
	view := state.archiveView
	compacted := *state
	compacted.Archive = &economicConservationArchive{}
	if err := compacted.retireWholeFees(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	entries, bytes := view.entries, view.bytes
	view.resources.IndexEntries = 2
	if err := view.retainWholeFees(t.Context(), state, &compacted); !errors.Is(err, errMonitorEconomicCapacity) || view.wholeFees != nil || entries != view.entries || bytes != view.bytes {
		t.Fatal("partial pending fee admission changed original index", err, view.wholeFees)
	}
	view.resources.IndexEntries = 1024
	if err := view.retainWholeFees(t.Context(), state, &compacted); err != nil || len(view.wholeFees.pending) != 1 {
		t.Fatal("exact retry did not retain one original pending block", err)
	}
}

// Late cancellation or owner loss cannot publish an apparent completed read.
func TestEconomicWholeFeeLateOwnerLossAndCancellationPublishNothing(t *testing.T) {
	for _, closeOwner := range []bool{false, true} {
		policy, state := nativeWholeFeeTestState(t, "complete")
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		state.archiveView.wholeFeeWork = func(context.Context, uint64) {
			calls++
			if closeOwner {
				if err := state.archiveView.close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
		}
		value, err := state.wholeFeeSummary(ctx, policy)
		cancel()
		if value != nil || err == nil || calls != 1 || closeOwner && !errors.Is(err, os.ErrClosed) || !closeOwner && !errors.Is(err, context.Canceled) || state.archiveView.wholeFees != nil {
			t.Fatal("late original fee custody refusal published a result", value, err, calls)
		}
		otherPolicy, other := nativeWholeFeeTestState(t, "complete")
		if value, err := other.wholeFeeSummary(t.Context(), otherPolicy); err != nil || !value.Complete {
			t.Fatal("unrelated original fee owner was stopped", value, err)
		}
	}
}

// A validly signed capacity/runtime continuation cannot replace the original
// payer domain. The original economic generation must admit that domain first.
func TestNativeWholeFeeAuthorityCannotBeEnrolledByRuntimeRenewal(t *testing.T) {
	_, policy, authorities, renewal, _ := nativeRenewalTestInputs(t)
	renewal.Next.FeeCensus = nativeWholeFeeTestAuthority(economicConservationPolicy{})
	nativeRenewalTestSign(t, &renewal)
	if err := renewal.validate(policy, authorities[0], 1); err == nil || !strings.Contains(err.Error(), "original economic") {
		t.Fatal("signed runtime renewal enrolled a new original fee domain", err)
	}
}

// Full-domain admission covers provider hotkeys, coldkeys and selected native
// and EVM payers independently of whichever fees happen to appear first.
func TestEconomicWholeFeeAuthorityRefusesOmittedOriginalParticipant(t *testing.T) {
	account := "0x" + strings.Repeat("33", 32)
	policy := economicConservationPolicy{Routes: []economicConservationRoute{{Hotkey: "0x" + strings.Repeat("11", 32), Coldkey: account}}}
	authority := nativeWholeFeeTestAuthority(policy)
	policy.Native.Observation.Execution = &nativeExecutionPolicy{FeeCensus: authority}
	if err := policy.validateWholeFeeAuthority(); err != nil {
		t.Fatal("complete original roster was refused", err)
	}
	copyAuthority := *authority
	copyAuthority.Participants = slices.DeleteFunc(append([]string(nil), authority.Participants...), func(value string) bool { return value == account })
	policy.Native.Observation.Execution.FeeCensus = &copyAuthority
	if err := policy.validateWholeFeeAuthority(); err == nil || !strings.Contains(err.Error(), "omits an original") {
		t.Fatal("unobserved original provider account was silently outside fee census", err)
	}
}

// The old selected-request census remains useful evidence, but even an
// internal completeness flag cannot replace the original complete body path.
func TestEconomicWholeFeeSelectedRequestsCannotGrantWholeCostAuthority(t *testing.T) {
	zero := "0"
	summary := economicConservationSummary{Funding: &economicConservationFundingSummary{}, AdmittedNativeFees: &economicConservationFeeSummary{SelectedCensusComplete: true, WholeProviderCensus: true, WithdrawalRao: &zero, RefundRao: &zero}}
	if err := summary.assessConformance(); err != nil {
		t.Fatal(err)
	}
	if summary.NativeFeeWithdrawalRao != nil || summary.NativeFeeRefundRao != nil || summary.TargetMet != nil || !strings.Contains(strings.Join(summary.MissingEvidence, ","), "whole-original-provider-native-fee-withdrawal-refund-census") {
		t.Fatal("selected fee requests promoted complete original costs", summary)
	}
}

// The maximum provider census is populated with distinct original identities.
// Its duplicated signed participant scope fits the explicit frame with 2x
// margin; the legacy reader retains its original one-MiB refusal.
func TestNativeWholeFeeFullProviderAuthorityUsesExplicitDocumentProfile(t *testing.T) {
	authority := nativeProducerAuthority{FeeCensus: &nativeFeeCensusPolicy{Schema: nativeFeeCensusSchema, ReviewSha256: monitorReadDigest([]byte("synthetic full fee domain"))}}
	for index := 0; index < rootCensusLimit; index++ {
		hotkey, coldkey := fmt.Sprintf("0x%064x", 2*index+1), fmt.Sprintf("0x%064x", 2*index+2)
		authority.Providers = append(authority.Providers, nativeProducerProvider{Hotkey: hotkey, Coldkey: coldkey})
		authority.FeeCensus.Participants = append(authority.FeeCensus.Participants, hotkey, coldkey)
	}
	if err := authority.FeeCensus.validate(); err != nil {
		t.Fatal(err)
	}
	message, err := authority.signingBytes()
	if err != nil || len(message) <= nativeProducerAuthorityLimit || 2*len(message) > nativeProducerAuthorityMaximum(authority.FeeCensus) {
		t.Fatal("populated original 4096-provider authority lacks explicit twofold capacity", len(message), err)
	}
	raw, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "synthetic-original-authority.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reference := planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
	if value, err := nativeProducerReadApprovalFor(t.Context(), reference, authority.FeeCensus); err != nil || !bytes.Equal(value, raw) {
		t.Fatal("actual original profile reader refused admitted provider census", err)
	}
	if _, err := nativeProducerReadApproval(t.Context(), reference); err == nil {
		t.Fatal("legacy approval reader silently borrowed the whole-fee profile")
	}
}

// The finite byte edge is shared by the actual approval reader. No parser or
// profile flag can accept one extra byte beyond its fixed admitted capacity.
func TestNativeWholeFeeApprovalReadersRefuseExactProfileOverflow(t *testing.T) {
	for _, fees := range []*nativeFeeCensusPolicy{nil, nativeWholeFeeTestAuthority(economicConservationPolicy{})} {
		maximum := nativeProducerAuthorityMaximum(fees)
		path := filepath.Join(t.TempDir(), "synthetic-approval-frame.json")
		for _, extra := range []int{0, 1} {
			raw := bytes.Repeat([]byte{' '}, maximum+extra)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			reference := planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
			value, err := nativeProducerReadApprovalFor(t.Context(), reference, fees)
			if extra == 0 && (err != nil || len(value) != maximum) || extra != 0 && (err == nil || value != nil) {
				t.Fatal("actual approval reader changed its exact admitted frame edge", maximum, extra, err)
			}
		}
	}
}
