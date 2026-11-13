// These arithmetic and durable-owner fixtures exercise cold retention without
// claiming runtime execution. Separate public roots below consume the actual
// capture/replay assets and the unchanged original checkpoint admission path.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// The synthetic execution has a stable stock and no recipient earnings. Only
// the first requested block has an unclassified committed mutation at net zero.
func economicPrincipalRetentionTestValue(parent economicEmissionBoundary, unknown bool) economicConservationPrincipalExecution {
	query := historicalPrincipalQuery{Hotkey: [32]byte{1}, Coldkey: [32]byte{2}, Netuid: 25, Availability: true}
	observation := nativeTreasuryTestObservation(query, 10, 10, 0, 10)
	boundary := economicEmissionBoundary{Number: parent.Number + 1, Hash: fmt.Sprintf("0x%064x", parent.Number+1)}
	value := economicConservationPrincipalExecution{
		Projection: nativePrincipalExecutionProjection{Authority: nativePrincipalPolicy{Queries: []historicalPrincipalQuery{query}}, Parent: parent, Boundary: boundary, Before: []historicalPrincipalObservation{observation}, After: []historicalPrincipalObservation{observation}},
		Outcome:    nativeExecutionOutcome{Boundary: boundary, RecipientEffects: &nativeExecutionEffectProjection{Effects: []nativeExecutionEffect{}}},
	}
	if unknown {
		value.Projection.Mutations = []historicalPrincipalMutation{{Ordinal: 1, Operation: "set", KeyHex: "0x01"}}
	}
	return value
}

// Serialize a synthetic checkpoint exactly as the real writer does. The
// descriptor tests do not pretend these synthetic records pass runtime admission.
func economicPrincipalRetentionTestSegment(t *testing.T, state *economicConservationState, path string) ([]byte, monitorHistoryReference) {
	t.Helper()
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	return raw, monitorHistoryReference{Path: path, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
}

func TestEconomicPrincipalRetentionUnknownPrefixBoundsLongContinuation(t *testing.T) {
	from := economicEmissionBoundary{Number: 100, Hash: fmt.Sprintf("0x%064x", 100)}
	state := &economicConservationState{Archive: &economicConservationArchive{}, Captures: []economicConservationCapture{{Id: "unresolved-original-capture"}}}
	view := newEconomicConservationArchiveView(economicConservationResources{IndexEntries: 1024, IndexBytes: 8 * 1024 * 1024})
	parent := from
	for segment := 0; segment < 16; segment++ {
		for block := 0; block < 16; block++ {
			value := economicPrincipalRetentionTestValue(parent, segment == 0 && block == 0)
			state.PrincipalExecutions = append(state.PrincipalExecutions, value)
			parent = value.Projection.Boundary
		}
		original := *state
		archive := *state.Archive
		state.Archive = &archive
		_, reference := economicPrincipalRetentionTestSegment(t, &original, filepath.Join(t.TempDir(), "original.json"))
		state.Archive.Segments = append(append([]monitorHistoryReference{}, state.Archive.Segments...), reference)
		if err := state.retainPrincipalOriginals(reference, true); err != nil {
			t.Fatal(err)
		}
		if err := view.indexPrincipalRetentions(&original, state); err != nil {
			t.Fatal(err)
		}
		if state.principalEffectFacts() != 0 || len(state.Captures) != 1 || state.Captures[0].Id != "unresolved-original-capture" || len(view.principalExecutions) != 0 || len(view.principalExecutionHashes) != 0 || view.principalCache != nil {
			t.Fatal("cold principal evidence remained resident or retired a capture", segment)
		}
	}
	head := state.Archive.PrincipalRetained
	if head.Blocks != 256 || head.UnresolvedBlocks != 1 || head.CompleteThrough != from || head.Through != parent || head.Pools[0].Complete || *head.Pools[0].Residual != "0" || *head.Pools[0].After != "10" || len(view.principalSegments) != 16 || view.entries != 16 || view.bytes+view.principalReservedBytes >= view.resources.IndexBytes/2 {
		t.Fatal("healthy continuation healed an unknown prefix or exceeded its bounded index", head, view.entries, view.bytes)
	}
}

func TestEconomicPrincipalRetentionCompletePrefixStopsAtFirstUnknown(t *testing.T) {
	parent := economicEmissionBoundary{Number: 20, Hash: fmt.Sprintf("0x%064x", 20)}
	var head *economicConservationPrincipalRetained
	var complete economicEmissionBoundary
	for index := 0; index < 8; index++ {
		value := economicPrincipalRetentionTestValue(parent, index == 2)
		var err error
		head, err = mergeEconomicPrincipalRetained(head, value)
		if err != nil {
			t.Fatal(err)
		}
		parent = value.Projection.Boundary
		if index == 1 {
			complete = parent
		}
	}
	if head.UnresolvedBlocks != 1 || head.CompleteThrough != complete || head.Through.Number != 28 || head.Pools[0].Complete {
		t.Fatal("later reconciled blocks crossed the first unknown original", head)
	}
}

func TestEconomicPrincipalRetentionOppositeResidualsDoNotCancelCauses(t *testing.T) {
	parent := economicEmissionBoundary{Number: 30, Hash: fmt.Sprintf("0x%064x", 30)}
	first := economicPrincipalRetentionTestValue(parent, true)
	query := first.Projection.Authority.Queries[0]
	secondQuery := query
	secondQuery.Hotkey[0]++
	first.Projection.Authority.Queries = append(first.Projection.Authority.Queries, secondQuery)
	first.Projection.Before = append(first.Projection.Before, nativeTreasuryTestObservation(secondQuery, 10, 20, 0, 20))
	first.Projection.After = []historicalPrincipalObservation{nativeTreasuryTestObservation(query, 11, 20, 0, 20), nativeTreasuryTestObservation(secondQuery, 9, 20, 0, 20)}
	head, err := mergeEconomicPrincipalRetained(nil, first)
	if err != nil || head.UnresolvedBlocks != 1 || *head.Pools[0].Residual != "1" || *head.Pools[1].Residual != "-1" {
		t.Fatal("different positions canceled an original stock discrepancy", head, err)
	}
	second := first
	second.Projection.Parent, second.Projection.Boundary = first.Projection.Boundary, economicEmissionBoundary{Number: 32, Hash: fmt.Sprintf("0x%064x", 32)}
	second.Projection.Before, second.Projection.After = first.Projection.After, first.Projection.Before
	second.Outcome.Boundary = second.Projection.Boundary
	head, err = mergeEconomicPrincipalRetained(head, second)
	if err != nil || head.UnresolvedBlocks != 2 || head.CompleteThrough != parent || *head.Pools[0].Residual != "0" || *head.Pools[1].Residual != "0" || head.Pools[0].Complete || head.Pools[1].Complete || head.Pools[0].NativeEarnings != "0" {
		t.Fatal("net-zero later stock concealed an incomplete cause census", head, err)
	}
	changed := second
	changed.Projection.Parent = head.Through
	changed.Projection.Boundary = economicEmissionBoundary{Number: 33, Hash: fmt.Sprintf("0x%064x", 33)}
	changed.Projection.Before = append([]historicalPrincipalObservation{}, second.Projection.After...)
	changed.Projection.Before[0].Query.Coldkey[0]++
	if _, err := mergeEconomicPrincipalRetained(head, changed); err == nil {
		t.Fatal("retention accepted a changed original coldkey lineage")
	}
}

func TestEconomicPrincipalRetentionAbsentApiDoesNotBecomeZero(t *testing.T) {
	value := economicPrincipalRetentionTestValue(economicEmissionBoundary{Number: 40, Hash: fmt.Sprintf("0x%064x", 40)}, false)
	value.Projection.Before[0].OpeningStakeAlpha, value.Projection.After[0].OpeningStakeAlpha = nil, nil
	head, err := mergeEconomicPrincipalRetained(nil, value)
	if err != nil || head.UnresolvedBlocks != 1 || head.Pools[0].Before != nil || head.Pools[0].After != nil || head.Pools[0].Residual != nil || head.Pools[0].Complete {
		t.Fatal("cold retention manufactured zero stock from missing API data", head, err)
	}
}

func TestEconomicPrincipalRetentionTreasuryUnknownPreservesAvailability(t *testing.T) {
	state, policy := nativeTreasuryTestArchivedState(t)
	original := state.Archive.PrincipalEffects
	head := &economicConservationPrincipalRetained{Schema: economicPrincipalRetentionSchema, From: policy.Native.Observation.Execution.Principal.Parent, Through: original.Through, CompleteThrough: policy.Native.Observation.Execution.Principal.Parent, Blocks: 1, UnresolvedBlocks: 1, ChainHash: monitorReadDigest([]byte("synthetic unknown original chain")), After: original.After, Pools: append([]nativePrincipalPoolEffects{}, original.Pools...)}
	for _, value := range original.After {
		head.Before = append(head.Before, nativeTreasuryTestObservation(value.Query, 0, 0, 0, 0))
	}
	head.Pools[0].Complete = false
	state.Archive.PrincipalEffects, state.Archive.PrincipalRetained = nil, head
	summary, err := state.treasurySummary(policy)
	if err != nil || summary.CauseCensusComplete || !summary.AvailabilityKnown || !summary.StockCovered || *summary.PositionStockAlpha != "98" || *summary.Availability.AvailableAlpha != "95" || summary.SpendableIncomeLowerAlpha != nil || summary.SpendableIncomeUpperAlpha != nil || summary.IncomeUnlockedOrSpent != nil || summary.Income.Gross != "98" || summary.Income.Collateral != "3" {
		t.Fatal("retention lost original availability or manufactured spendable income", summary, err)
	}
}

// Two separate owned original files exercise a segment boundary, cache eviction,
// cancellation and loss of physical custody without any source recapture.
func TestEconomicPrincipalRetentionHydratesOneOwnedSegmentAcrossBoundaries(t *testing.T) {
	directory := t.TempDir()
	storage := durablefixture.New(t, t.Context(), directory)
	view := newEconomicConservationArchiveView(economicConservationResources{IndexEntries: 64, IndexBytes: 4 * 1024 * 1024})
	t.Cleanup(func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	})
	parent := economicEmissionBoundary{Number: 50, Hash: fmt.Sprintf("0x%064x", 50)}
	var prior *economicConservationPrincipalRetained
	for index := 0; index < 2; index++ {
		value := economicPrincipalRetentionTestValue(parent, index == 0)
		original := &economicConservationState{PrincipalExecutions: []economicConservationPrincipalExecution{value}}
		path := filepath.Join(directory, fmt.Sprintf("segment-%d.json", index))
		provisionMonitorTestCustody(t, path)
		raw, reference := economicPrincipalRetentionTestSegment(t, original, path)
		writer, err := openMonitorHistorySnapshot(storage.Context, path, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(writer.publish(raw, nil), writer.close()); err != nil {
			t.Fatal(err)
		}
		reader, _, err := openMonitorHistoryReader(storage.Context, reference)
		if err != nil {
			t.Fatal(err)
		}
		view.owners = append(view.owners, reader)
		head, err := mergeEconomicPrincipalRetained(prior, value)
		if err != nil {
			t.Fatal(err)
		}
		compacted := &economicConservationState{Archive: &economicConservationArchive{Segments: []monitorHistoryReference{reference}, PrincipalRetentions: []monitorHistoryReference{reference}, PrincipalRetained: head}}
		if err := view.indexPrincipalRetentions(original, compacted); err != nil {
			t.Fatal(err)
		}
		prior, parent = head, value.Projection.Boundary
	}
	for _, number := range []uint64{51, 52, 51} {
		value, found, err := view.principalExecution(storage.Context, number)
		if err != nil || !found || value.Projection.Boundary.Number != number || view.principalCache == nil || len(view.principalCache.Values) != 1 || len(view.principalExecutions) != 0 || len(view.principalExecutionHashes) != 0 {
			t.Fatal("cold sequential lookup lost a boundary or retained extra payloads", number, value, found, err)
		}
	}
	cache := view.principalCache
	canceled, cancel := context.WithCancel(storage.Context)
	cancel()
	if _, found, err := view.principalExecution(canceled, 52); !errors.Is(err, context.Canceled) || found || view.principalCache != cache {
		t.Fatal("canceled cold lookup changed its admitted cache", found, err)
	}
	if err := view.owners[0].close(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := view.principalExecution(storage.Context, 51); err == nil || found {
		t.Fatal("warm cache concealed loss of original physical custody", found, err)
	}
}

func TestEconomicPrincipalRetentionCapacityRefusesBeforePublication(t *testing.T) {
	value := economicPrincipalRetentionTestValue(economicEmissionBoundary{Number: 60, Hash: fmt.Sprintf("0x%064x", 60)}, true)
	original := &economicConservationState{PrincipalExecutions: []economicConservationPrincipalExecution{value}}
	_, reference := economicPrincipalRetentionTestSegment(t, original, filepath.Join(t.TempDir(), "original.json"))
	head, err := mergeEconomicPrincipalRetained(nil, value)
	if err != nil {
		t.Fatal(err)
	}
	compacted := &economicConservationState{Archive: &economicConservationArchive{Segments: []monitorHistoryReference{reference}, PrincipalRetentions: []monitorHistoryReference{reference}, PrincipalRetained: head}}
	view := newEconomicConservationArchiveView(economicConservationResources{IndexEntries: 16, IndexBytes: 2 * reference.Bytes})
	if err := view.indexPrincipalRetentions(original, compacted); !errors.Is(err, errMonitorEconomicCapacity) || len(view.principalSegments) != 0 || view.principalRetained != nil || len(original.PrincipalExecutions) != 1 {
		t.Fatal("cold retention published a partial index above its hydration capacity", err)
	}
}

func TestEconomicPrincipalRetentionProvisionalCannotPublishOrOutliveOriginal(t *testing.T) {
	value := economicPrincipalRetentionTestValue(economicEmissionBoundary{Number: 80, Hash: fmt.Sprintf("0x%064x", 80)}, true)
	values := []economicConservationPrincipalExecution{value}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reference := monitorHistoryReference{Path: filepath.Join(t.TempDir(), "original.json"), Bytes: 1, Sha256: monitorReadDigest([]byte("original"))}
	state := &economicConservationState{Archive: &economicConservationArchive{PrincipalRetentions: []monitorHistoryReference{reference}}, principalProvisional: &economicConservationPrincipalProvisional{ctx: ctx, reference: reference, originalHash: monitorReadDigest([]byte("validated checkpoint")), valuesHash: rootObjectHash(values), values: values}}
	lookup, err := state.captureExecutionLookup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	loaded, found, err := lookup(81)
	if err != nil || !found || !reflect.DeepEqual(loaded, value) {
		t.Fatal("candidate lost its admitted original", found, err)
	}
	if err := saveEconomicConservation(ctx, nil, economicConservationPolicy{}, state); err == nil || !strings.Contains(err.Error(), "provisional") {
		t.Fatal("candidate-only original acquired publication authority", err)
	}
	state.principalProvisional.values[0].Projection.Boundary.Hash = "0x" + strings.Repeat("ff", 32)
	if _, err := state.captureExecutionLookup(ctx); err == nil {
		t.Fatal("changed candidate original survived its content binding")
	}
	state.principalProvisional.values[0] = value
	cancel()
	if _, found, err := lookup(81); !errors.Is(err, context.Canceled) || found {
		t.Fatal("expired candidate reopened its original interval", found, err)
	}
}

// The actual vault getter identity forces capture lookup during plan validation,
// before the proposed archive exists. Unknown must survive that real caller.
func TestEconomicConservationPublicUnknownCapturePlansPrincipalRetention(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture-unclassified", false, nil)
	first := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if len(state.Captures) != 1 || state.Captures[0].Event.CaptureIdentity == nil || state.Captures[0].KnownLiquidAlpha == nil || state.Captures[0].Native == nil || state.Captures[0].PrincipalEffects != nil || first.CausallyJoinedCaptures != 0 {
		t.Fatal("fixture did not reach the original unknown capture lookup", first, state.Captures)
	}
	before := state.PrincipalExecutions[0]
	f.reset(t)
	f.request.RetainPrincipalOriginals = true
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	reopened := f.sample(t, monitorServiceHooks{})
	state = f.source.state(t)
	if reopened.PrincipalEffects == nil || reopened.PrincipalEffects.Current || reopened.PrincipalEffects.Retained == nil || reopened.PrincipalEffects.Retained.UnresolvedBlocks != 1 || len(state.PrincipalExecutions) != 0 || len(state.Captures) != 1 || state.Captures[0].PrincipalEffects != nil || reopened.CausallyJoinedCaptures != 0 {
		t.Fatal("provisional original lookup manufactured a capture certificate", reopened, state.Captures)
	}
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	}()
	value, found, err := view.principalExecution(f.ctx, before.Projection.Boundary.Number)
	if err != nil || !found || !reflect.DeepEqual(value, before) {
		t.Fatal("candidate reference replaced the actual archived original", found, err)
	}
}

func TestEconomicConservationPublicUnknownPrincipalColdArchiveReopensOriginals(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-unclassified", true, nil)
	first := f.sample(t, monitorServiceHooks{})
	original := f.source.state(t).PrincipalExecutions[0]
	f.reset(t)
	f.request.RetainPrincipalOriginals = true
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	second := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if second.PrincipalEffects == nil || second.PrincipalEffects.Current || second.PrincipalEffects.Through != first.PrincipalEffects.Through || second.PrincipalEffects.Retained == nil || second.PrincipalEffects.Retained.UnresolvedBlocks != 1 || len(state.PrincipalExecutions) != 0 || len(state.Captures) != 1 || state.Captures[0].PrincipalEffects != nil || second.TargetMet != nil {
		t.Fatal("cold archive resolved missing causes or dropped an unresolved capture", second, state)
	}
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	})
	value, found, err := view.principalExecution(f.ctx, original.Projection.Boundary.Number)
	if err != nil || !found || !reflect.DeepEqual(value, original) || len(view.principalExecutions) != 0 || len(view.principalExecutionHashes) != 0 {
		t.Fatal("reopened owned checkpoint changed the exact original execution", found, err)
	}
	view.principalCache = nil
	value, found, err = view.principalExecution(f.ctx, original.Projection.Boundary.Number)
	if err != nil || !found || !reflect.DeepEqual(value, original) {
		t.Fatal("cache eviction changed original evidence", found, err)
	}
}

func TestEconomicConservationPublicFollowArchivesUnknownBeforeNextNativeRead(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "effects-unclassified", true, nil)
	path := filepath.Join(filepath.Dir(f.source.checkpoint), "automatic-principal-001.json")
	provisionMonitorTestCustody(t, path)
	f.source.policy.PrincipalRetention = &economicConservationPrincipalRetentionPolicy{Schema: economicPrincipalRetentionSchema, TriggerFacts: 1, ArchivePaths: []string{path}}
	f.source.writePolicy(t)
	first := f.sample(t, monitorServiceHooks{})
	original, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	proofs := producer.proofs.Load()
	var output, diagnostic bytes.Buffer
	args := append(f.source.args(t), "--follow")
	code := runMainWithMonitorHooks(f.ctx, args, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{wait: func(context.Context, string, time.Duration) bool { return false }})
	if code != 0 {
		t.Fatal("automatic follow retention failed", code, diagnostic.String())
	}
	var second economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(path)
	state := f.source.state(t)
	if err != nil || !bytes.Equal(retained, original) || second.NativeCursor != first.NativeCursor || second.PrincipalEffects == nil || second.PrincipalEffects.Current || second.PrincipalEffects.Retained == nil || second.PrincipalEffects.Retained.UnresolvedBlocks != 1 || len(state.PrincipalExecutions) != 0 || second.FactsRemaining <= first.FactsRemaining || producer.proofs.Load() != proofs {
		t.Fatal("continuous maintenance failed exact original retention before successor read", second, err)
	}
	// Reopen after the maintenance attempt's timeout owner has been canceled.
	reopened := f.sample(t, monitorServiceHooks{})
	if reopened.NativeCursor != second.NativeCursor || reopened.PrincipalEffects.Current || reopened.PrincipalEffects.Retained.ChainHash != second.PrincipalEffects.Retained.ChainHash {
		t.Fatal("maintenance retained a canceled reader or changed restart evidence", reopened)
	}
}

func TestEconomicConservationPublicCorruptPrincipalArchiveDoesNotRecapture(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "effects-unclassified", true, nil)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	f.request.RetainPrincipalOriginals = true
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	before, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	proofs := producer.proofs.Load()
	if err := os.WriteFile(f.request.ArchivePath, []byte("{\"malformed_original\":true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	after, err := os.ReadFile(f.source.checkpoint)
	if code != 3 || err != nil || !bytes.Equal(before, after) || output.Len() != 0 || producer.proofs.Load() != proofs {
		t.Fatal("corrupt cold original became empty evidence or a fresh capture", code, diagnostic.String(), err)
	}
}

// Two actual native jobs retire into distinct owned segments while the first
// vault receipt is unavailable. Recovery must hydrate both original intervals.
func TestEconomicConservationPublicDelayedCapturesCrossPrincipalColdSegments(t *testing.T) {
	f, producer, _ := newEconomicCaptureSequenceFixture(t, false)
	f.source.vault.unavailable.Store(true)
	var last economicConservationSummary
	for index := 0; index < 2; index++ {
		var output, diagnostic bytes.Buffer
		code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
			if role != f.source.policy.Vault.Role {
				t.Error("vault outage blocked another original source", role)
			}
			return context.DeadlineExceeded
		}})
		if err := decodePlanJson(output.Bytes(), &last); err != nil {
			t.Fatal(err, diagnostic.String())
		}
		if code != 3 || !last.NativeCurrent || last.VaultHeld || last.Captures != 0 || last.NativeCursor.Number != uint64(101+index) {
			t.Fatal("actual native continuation did not precede cold retention", code, last, diagnostic.String())
		}
		f.reset(t)
		f.request.RetainPrincipalOriginals = true
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal(code, issue)
		}
		if index == 0 {
			economicCaptureSequenceAdvance(producer)
		}
	}
	state := f.source.state(t)
	if len(state.PrincipalExecutions) != 0 || state.Archive.PrincipalEffects != nil || len(state.Archive.PrincipalRetentions) != 2 || state.Archive.PrincipalRetained.UnresolvedBlocks != 0 {
		t.Fatal("actual capture prerequisites did not move into two cold segments", state)
	}
	proofs := producer.proofs.Load()
	f.source.vault.unavailable.Store(false)
	first := f.sample(t, monitorServiceHooks{})
	second := f.sample(t, monitorServiceHooks{})
	state = f.source.state(t)
	if first.CausallyJoinedCaptures != 1 || second.CausallyJoinedCaptures != 2 || second.VaultCursor.Number != 12 || second.NativeCursor != last.NativeCursor || len(state.Captures) != 2 || state.Captures[1].PrincipalEffects == nil || state.Captures[1].PrincipalEffects.PreviousCapture != state.Captures[0].Id || state.Captures[1].PrincipalEffects.OpeningStock != "0" || len(state.PrincipalExecutions) != 0 || producer.proofs.Load() != proofs || *second.TailGrossAlpha != "18" {
		t.Fatal("cold segment boundary became false absence or repeated original income", first, second, state.Captures)
	}
}

func TestEconomicPrincipalRetentionLegacyNilWireAndExplicitThreshold(t *testing.T) {
	raw, err := json.Marshal(economicConservationPolicy{})
	if err != nil || bytes.Contains(raw, []byte("principal_retention")) {
		t.Fatal("legacy policy bytes acquired a retention selector", err)
	}
	raw, err = json.Marshal(economicConservationArchive{})
	if err != nil || bytes.Contains(raw, []byte("retained_original_principal")) || bytes.Contains(raw, []byte("principal_retention_segments")) {
		t.Fatal("legacy archive bytes changed", err)
	}
	policy := economicConservationPolicy{MaximumFacts: 64, Native: monitorEconomicNativePolicy{Observation: economicEmissionPolicy{Execution: &nativeExecutionPolicy{Principal: &nativePrincipalPolicy{Effects: &nativePrincipalEffectsPolicy{}}}}}}
	selection := &economicConservationPrincipalRetentionPolicy{Schema: economicPrincipalRetentionSchema, TriggerFacts: 8, ArchivePaths: []string{filepath.Join(t.TempDir(), "original.json")}}
	if err := selection.validate(policy); err != nil {
		t.Fatal(err)
	}
	state := &economicConservationState{PrincipalExecutions: []economicConservationPrincipalExecution{economicPrincipalRetentionTestValue(economicEmissionBoundary{Number: 70, Hash: "0x" + strings.Repeat("70", 32)}, true)}, Native: monitorEconomicNativeState{CapacityRemaining: 128}}
	if selection.due(policy, state) {
		t.Fatal("early threshold control did not remain below the selected trigger")
	}
	selection.TriggerFacts = 1
	if !selection.due(policy, state) {
		t.Fatal("unknown principal work did not trigger automatic retention")
	}
	if err := selection.validatePaths(selection.ArchivePaths[0] + ".lock"); err == nil {
		t.Fatal("archive slot borrowed a live writer lock")
	}
	selection.TriggerFacts = 64
	if err := selection.validate(policy); err == nil {
		t.Fatal("automatic threshold waited until MaximumFacts was exhausted")
	}
}
