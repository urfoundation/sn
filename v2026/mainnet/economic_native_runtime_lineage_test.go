//go:build linux || darwin

// These public consumers execute two original programs through both real
// engines. RPC fixtures expose their independently exported roots and code;
// they cannot supply a principal amount, execution projection or renewal ack.
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

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

func nativeRuntimeRenewalTestJob(t *testing.T, number string) historicalReplayJob {
	t.Helper()
	directory := os.Getenv("URNETWORK_NATIVE_RUNTIME_RENEWAL_FIXTURE")
	if !filepath.IsAbs(directory) {
		t.Fatal("runtime renewal requires independently exported actual original jobs")
	}
	raw, _, err := readPlanFile(t.Context(), filepath.Join(directory, "native-job-"+number+".json"), historicalNativeJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := decodePlanJson(raw, &job); err != nil {
		t.Fatal(err)
	}
	return job
}

func nativeRuntimeRenewalTestNext(t *testing.T, producer *nativeProducerPublicFixture) nativeProducerAuthority {
	t.Helper()
	job := nativeRuntimeRenewalTestJob(t, "103")
	next := producer.authority
	next.Signature = ""
	next.Runtime.RuntimeVersion.SpecVersion++
	next.Runtime.RuntimeCodeHash = nativeExecutionTestHex(job.RuntimeCodeBlake2b256[:])
	next.Profile = job.ObservationProfile
	next.ReviewSha256 = "sha256:" + strings.TrimPrefix(nativeExecutionTestHex(job.ObservationProfile.SourceReviewSha256[:]), "0x")
	if next.Runtime == producer.authority.Runtime || next.Profile.RuntimeCodeSha256 == producer.authority.Profile.RuntimeCodeSha256 {
		t.Fatal("runtime renewal reused the original program")
	}
	return next
}

func nativeRuntimeRenewalTestReview(t *testing.T, producer *nativeProducerPublicFixture, state *nativeExecutionProducerState) (nativeProducerRenewal, planFileReference) {
	t.Helper()
	value := nativeProducerRenewal{Schema: nativeProducerRenewalSchema, Original: producer.source.policy.Execution.Producer.Authority, Previous: producer.source.policy.Execution.Producer.Authority, Ordinal: 1, After: state.Cursor, Completed: state.Completed, CompletionChain: state.CompletionChain, Next: nativeRuntimeRenewalTestNext(t, producer)}
	nativeRenewalTestSign(t, &value)
	return value, nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(producer.policy), "actual-runtime-renewal.json"), value)
}

func nativeRuntimeRenewalTestPolicy(t *testing.T, policy economicEmissionPolicy, reference planFileReference) economicEmissionPolicy {
	t.Helper()
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	var result economicEmissionPolicy
	if err := decodePlanJson(raw, &result); err != nil {
		t.Fatal(err)
	}
	result.Execution.Producer.Renewals = []planFileReference{reference}
	return result
}

func TestNativeRuntimeRenewalPublicMonitorUpgradeAndColdRestart(t *testing.T) {
	directory := os.Getenv("URNETWORK_NATIVE_RUNTIME_RENEWAL_FIXTURE")
	nativeRuntimeRenewalTestJob(t, "101")
	t.Setenv("URNETWORK_NATIVE_PRODUCER_FIXTURE", directory)
	t.Setenv("URNETWORK_NATIVE_EXECUTION_FIXTURE", filepath.Join(directory, "native-job-101.json"))
	producer := nativeProducerPublicFixtureWithRuntime(t, true, nil, true)
	f := &monitorEconomicTestFixture{source: producer.source, services: newMonitorServicesFixture(t), url: producer.source.client.url, policy: monitorEconomicNativePolicy{Role: "native-a", Observation: producer.source.policy, BatchBlocks: 1, HistoryEntries: 32, StallSeconds: 60, HistoricalFinality: "owned-rpc-assertion"}}
	f.prepare(t)
	f.ctx = durablefixture.New(t, t.Context(), f.services.directory, producer.source.policy.Execution.Directory).Context
	identity := f.policy.identityHash()
	run := f.start(t, monitorServiceHooks{})
	run.next(t)
	run.stop(t)
	first := f.record(t)
	if first.State.Cursor.Number != 101 || first.State.ExecutionProducer == nil {
		t.Fatal("actual original monitor did not complete first program", first.State)
	}
	t.Log("runtime renewal monitor reached actual initial capture/replay completion 101")
	review, reference := nativeRuntimeRenewalTestReview(t, producer, first.State.ExecutionProducer)
	f.policy.Observation = nativeRuntimeRenewalTestPolicy(t, f.policy.Observation, reference)
	f.services.policy.NativeEconomics = []monitorEconomicNativePolicy{f.policy}
	f.services.writePolicy(t)
	producer.source.chain.finalized = producer.source.chain.byHeight[105]
	for number := uint64(102); number <= 104; number++ {
		run = f.start(t, monitorServiceHooks{})
		run.next(t)
		run.stop(t)
		record := f.record(t)
		if record.State.Cursor.Number != number || record.State.ExecutionProducer == nil || record.State.ExecutionProducer.Completed != number-100 || record.PolicyHash != identity || len(record.RuntimeCatalog) != 0 {
			t.Fatal("public monitor lost renewed runtime, original identity or implicit catalog", number, record.State)
		}
		if number == 102 {
			if *record.State.LastExecutionRuntime != producer.authority.Runtime || *record.State.LastPostStateRuntime != review.Next.Runtime || len(record.State.ExecutionProducer.AuthorityRevisions) != 0 {
				t.Fatal("code-upgrade block selected the next execution authority too early", record.State)
			}
		} else if *record.State.LastExecutionRuntime != review.Next.Runtime || len(record.State.ExecutionProducer.AuthorityRevisions) != 1 || record.State.ExecutionProducer.AuthorityRevisions[0].AdoptedAfter.Number != 102 {
			t.Fatal("cold monitor lost actual delayed runtime adoption", record.State)
		}
	}
}

// An explicit read catalog lets the principal regression reach its own old
// equality check even when the separate implicit-catalog fix is reverted.
func nativeRuntimeRenewalTestConservation(t *testing.T) (*economicConservationArchiveFixture, *nativeProducerPublicFixture, nativeProducerRenewal, planFileReference, economicConservationState) {
	t.Helper()
	f, producer := newEconomicConservationPrincipalFixture(t, "runtime-renewal", true, nil)
	next := nativeRuntimeRenewalTestNext(t, producer)
	f.source.policy.Native.RuntimeCatalog = []monitorEconomicRuntimeEntry{
		{Profile: producer.authority.Runtime, ReviewSha256: producer.authority.ReviewSha256, Purposes: []string{economicRuntimeStatePurpose, economicRuntimeEventsPurpose, economicRuntimeFeePurpose}},
		{Profile: next.Runtime, ReviewSha256: next.ReviewSha256, Purposes: []string{economicRuntimeStatePurpose, economicRuntimeEventsPurpose, economicRuntimeFeePurpose}},
	}
	f.source.writePolicy(t)
	first := f.sample(t, monitorServiceHooks{})
	before := f.source.state(t)
	if before.NativeHeld || before.Native.Cursor.Number != 101 || first.PrincipalEffects == nil || !first.PrincipalEffects.Current || first.OpeningPrincipalAlpha == nil || *first.OpeningPrincipalAlpha != "14" {
		t.Fatal("actual original principal baseline did not admit", first, before.NativeIssue)
	}
	t.Log("runtime renewal principal reached actual initial capture/replay completion 101")
	review, reference := nativeRuntimeRenewalTestReview(t, producer, before.Native.ExecutionProducer)
	f.reset(t)
	_, _, args := economicConservationNativeRenewalTestPlan(t, f, []planFileReference{reference}, monitorReadDigest([]byte("synthetic actual runtime consumer renewal")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public signed runtime adoption", code, issue)
	}
	producer.source.chain.finalized = producer.source.chain.byHeight[105]
	// Complete the old-engine upgrade job through the public producer, without
	// acknowledging it in the combined checkpoint. Restart must replay it once.
	producer.source.policy = nativeRuntimeRenewalTestPolicy(t, producer.source.policy, reference)
	nativeRenewalTestPublicNext(t, producer, f.ctx, before.Native.ExecutionProducer, 102)
	pending, code, issue := producer.command(t)
	if code != 0 || !pending.Complete || pending.ExecutionProducer == nil || len(pending.ExecutionProducer.AuthorityRevisions) != 0 || pending.ExecutionProducer.Cursor.Number != 102 {
		t.Fatal("actual old-engine pending completion did not retain its authority", code, issue)
	}
	proofs := producer.proofs.Load()
	f.sample(t, monitorServiceHooks{})
	recovered := f.source.state(t)
	if recovered.NativeHeld || recovered.Native.Cursor.Number != 102 || producer.proofs.Load() != proofs || len(recovered.Native.ExecutionProducer.AuthorityRevisions) != 0 {
		t.Fatal("combined public restart recaptured or rejected the old original completion", recovered.NativeIssue, recovered.Native.ExecutionProducer)
	}
	// Lose the outer acknowledgement again after the new program has actually
	// executed. Its exact first completion must select R2 on retained replay.
	nativeRenewalTestPublicNext(t, producer, f.ctx, recovered.Native.ExecutionProducer, 103)
	renewed, code, issue := producer.command(t)
	if code != 0 || !renewed.Complete || renewed.ExecutionProducer == nil || renewed.ExecutionProducer.AuthorityHash != reference.Sha256 || len(renewed.ExecutionProducer.AuthorityRevisions) != 1 || renewed.ExecutionProducer.AuthorityRevisions[0].AdoptedAfter.Number != 102 {
		t.Fatal("actual renewed pending completion did not retain its signed runtime", code, issue)
	}
	proofs = producer.proofs.Load()
	completion, err := os.ReadFile(renewed.ExecutionProducer.Completion.Path)
	if err != nil {
		t.Fatal(err)
	}
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	retained, err := os.ReadFile(renewed.ExecutionProducer.Completion.Path)
	if err != nil || !bytes.Equal(completion, retained) || producer.proofs.Load() != proofs || state.NativeHeld || state.NativeIssue != "" || state.Native.Cursor.Number != 103 || state.Native.ExecutionProducer.AuthorityHash != reference.Sha256 || len(state.PrincipalExecutions) != 2 || state.PrincipalExecutions[0].Projection.Runtime != producer.authority.Runtime || state.PrincipalExecutions[1].Projection.Runtime != review.Next.Runtime || summary.PrincipalEffects == nil || !summary.PrincipalEffects.Current {
		t.Fatal("actual renewed original principal execution was held", state.NativeIssue, state.Native.ExecutionProducer, summary.PrincipalEffects)
	}
	return f, producer, review, reference, state
}

func TestNativeRuntimeRenewalPublicPrincipalMixedHistoryAndArchive(t *testing.T) {
	f, producer, _, _, state := nativeRuntimeRenewalTestConservation(t)
	original, err := os.ReadFile(f.source.path)
	if err != nil {
		t.Fatal(err)
	}
	opening := rootObjectHash(state.OpeningPrincipals)
	if state.PrincipalExecutions[1].Projection.Before[0].OpeningStakeAlpha == nil || state.PrincipalExecutions[1].Projection.After[0].OpeningStakeAlpha == nil || *state.PrincipalExecutions[1].Projection.Before[0].OpeningStakeAlpha != "28" || *state.PrincipalExecutions[1].Projection.After[0].OpeningStakeAlpha != "32" {
		t.Fatal("renewed original principal lost its queried stock")
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("mixed-runtime original principal archive", code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	after := f.source.state(t)
	policy, err := os.ReadFile(f.source.path)
	if err != nil || !bytes.Equal(original, policy) || after.PolicyHash != state.PolicyHash || rootObjectHash(after.OpeningPrincipals) != opening || after.Native.Cursor.Number != 104 || after.NativeHeld || summary.PrincipalEffects == nil || !summary.PrincipalEffects.Current || summary.PrincipalEffects.Archived == nil || summary.PrincipalEffects.Archived.Blocks != 3 || summary.PrincipalEffects.Archived.Pools[0].NativeEarnings != "6" || summary.TargetMet != nil {
		t.Fatal("cold mixed-runtime archive changed original stock, authority or amounts", err, after.NativeIssue, summary)
	}
	if after.PrincipalExecutions[0].Projection.Runtime == producer.authority.Runtime {
		t.Fatal("cold archive resumed the initial execution profile")
	}
}

func TestNativeRuntimeRenewalConsumersRejectWrongAuthorityAndAdoption(t *testing.T) {
	f, producer, _, _, valid := nativeRuntimeRenewalTestConservation(t)
	for _, fault := range []string{"unselected-runtime", "wrong-outcome-authority", "false-adoption-boundary", "unselected-monitor-runtime"} {
		raw, err := json.Marshal(valid)
		if err != nil {
			t.Fatal(err)
		}
		var state economicConservationState
		if err := decodePlanJson(raw, &state); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "unselected-runtime":
			state.PrincipalExecutions[0].Projection.Runtime = state.PrincipalExecutions[1].Projection.Runtime
			state.PrincipalExecutions[0].Projection.ContentHash = state.PrincipalExecutions[0].Projection.hash()
		case "wrong-outcome-authority":
			state.PrincipalExecutions[1].Outcome.ProducerAuthorityHash = producer.source.policy.Execution.Producer.Authority.Sha256
			state.PrincipalExecutions[1].Outcome.ContentHash = state.PrincipalExecutions[1].Outcome.hash()
		case "false-adoption-boundary":
			ack := &state.Native.ExecutionProducer.AuthorityRevisions[0]
			ack.AdoptedAfter, ack.AdoptedCompleted, ack.AdoptedChain = ack.After, ack.Completed, ack.CompletionChain
		case "unselected-monitor-runtime":
			old := producer.authority.Runtime
			state.Native.LastExecutionRuntime = &old
		}
		state.ContentHash = state.hash()
		raw, err = json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeEconomicConservation(f.ctx, raw, f.source.policy); err == nil {
			t.Fatal("self-sealed consumer accepted a different original runtime lineage", fault)
		}
	}
}

func TestNativeRuntimeRenewalColdAdmissionRequiresSignedOriginalsAndReadPurposes(t *testing.T) {
	f, producer, review, reference, valid := nativeRuntimeRenewalTestConservation(t)
	operating, err := valid.operatingPolicy(f.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	var cold monitorEconomicNativeState
	raw, err := json.Marshal(valid.Native)
	if err != nil || decodePlanJson(raw, &cold) != nil {
		t.Fatal("cold original native state", err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if err := cold.admitRuntime(ctx, operating.Native); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled original runtime admission acquired authority", err)
	}
	if err := os.Rename(reference.Path, reference.Path+".retained"); err != nil {
		t.Fatal(err)
	}
	err = cold.admitRuntime(f.ctx, operating.Native)
	if restoreErr := os.Rename(reference.Path+".retained", reference.Path); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil {
		t.Fatal("cold runtime admission discarded the original signed approval")
	}
	foreign := review
	foreign.Signature = strings.Repeat("0", 128)
	foreignRef := nativeRenewalTestWrite(t, filepath.Join(f.metadata, "foreign-runtime-approval.json"), foreign)
	foreignPolicy := nativeRuntimeRenewalTestPolicy(t, operating.Native.Observation, foreignRef)
	if _, err := loadNativeProducerAuthorities(f.ctx, foreignPolicy); err == nil {
		t.Fatal("foreign signer acquired a runtime catalog or principal execution policy")
	}
	if err := cold.admitRuntime(f.ctx, operating.Native); err != nil {
		t.Fatal("restored original signed runtime could not be admitted", err)
	}
	limited := operating.Native
	limited.RuntimeCatalog = append([]monitorEconomicRuntimeEntry{}, operating.Native.RuntimeCatalog[:1]...)
	if err := cold.validate(limited); err == nil {
		t.Fatal("producer fallback bypassed an explicit original read catalog")
	}
	admitted, err := cold.runtimeReadPolicy(operating.Native)
	if err != nil || !reflect.DeepEqual(admitted.RuntimeCatalog, operating.Native.RuntimeCatalog) {
		t.Fatal("signed producer changed explicit original read purposes", err)
	}
	limited = operating.Native
	limited.RuntimeCatalog = append([]monitorEconomicRuntimeEntry{}, operating.Native.RuntimeCatalog...)
	limited.RuntimeCatalog[1].Purposes = []string{economicRuntimeStatePurpose}
	before, proofs := rootObjectHash(cold), producer.proofs.Load()
	if _, err := observeMonitorEconomicNative(f.ctx, producer.source.client, limited, &cold); err == nil || !strings.Contains(err.Error(), "purpose") || rootObjectHash(cold) != before || producer.proofs.Load() != proofs {
		t.Fatal("signed runtime bypassed explicit event read purpose before original capture", err)
	}
}

// Signed future runtime approval is not a monitor capacity renewal. This
// grammar control uses original signed documents and grants no executed stock.
func TestNativeRuntimeRenewalFallbackRetainsReviewedCatalogCapacity(t *testing.T) {
	ctx, policy, _, first, files := nativeRenewalTestInputs(t)
	previous := first
	for ordinal := uint64(2); ordinal <= 9; ordinal++ {
		value := previous
		value.Previous = policy.Execution.Producer.Renewals[len(policy.Execution.Producer.Renewals)-1]
		value.Ordinal, value.Completed = ordinal, ordinal
		value.After = economicEmissionBoundary{Number: 100 + ordinal, Hash: "0x" + fmt.Sprintf("%064x", ordinal)}
		value.CompletionChain = monitorReadDigest([]byte(fmt.Sprintf("synthetic reviewed future completion %d", ordinal)))
		value.Next.Runtime.RuntimeVersion.SpecVersion++
		nativeRenewalTestSign(t, &value)
		reference := nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(files.path), fmt.Sprintf("future-runtime-%d.json", ordinal)), value)
		policy.Execution.Producer.Renewals = append(policy.Execution.Producer.Renewals, reference)
		previous = value
	}
	monitor := monitorEconomicNativePolicy{Observation: policy}
	original := rootObjectHash(monitor)
	state := newMonitorEconomicNativeState(monitor)
	if err := state.admitRuntime(ctx, monitor); err != nil {
		t.Fatal("exact future signed runtime documents did not admit", err)
	}
	if _, err := state.runtimeReadPolicy(monitor); err == nil {
		t.Fatal("signed producer runtimes exceeded the unchanged eight-artifact monitor capacity")
	}
	if rootObjectHash(monitor) != original || monitor.RuntimeCapacity != nil || monitor.RuntimeCatalog != nil {
		t.Fatal("derived runtime admission mutated original serialized monitor policy")
	}
	monitor.RuntimeCapacity = &monitorEconomicRuntimeCapacity{Entries: 9, Bytes: 8 * 1024}
	admitted, err := state.runtimeReadPolicy(monitor)
	if err != nil || len(admitted.RuntimeCatalog) != 9 {
		t.Fatal("independently increased monitor capacity lost a signed runtime", err, len(admitted.RuntimeCatalog))
	}
}
