// Runtime renewals consume independently configured artifacts and never infer
// signing compatibility, source builds or finality from a successful RPC read.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func monitorEconomicTestReview(profile rootReceiptProfile) monitorEconomicRuntimeEntry {
	return monitorEconomicRuntimeEntry{Profile: profile, ReviewSha256: "sha256:" + strings.Repeat("7", 64), Purposes: []string{economicRuntimeStatePurpose, economicRuntimeEventsPurpose, economicRuntimeFeePurpose}}
}

// Install only before a run, or after its joined stop. The new envelope is
// intentionally unsuitable for root signing, but its consumed read types match.
func monitorEconomicTestUpgrade(t *testing.T, fixture *monitorEconomicTestFixture, from uint64) rootReceiptProfile {
	t.Helper()
	_, encoded, digest := economicEmissionTestMetadata(t, func(metadata *types.Metadata) {
		metadata.AsMetadataV14.Extrinsic.Version = 5
	})
	profile := fixture.policy.Observation.Runtime
	profile.RuntimeVersion.SpecVersion++
	profile.RuntimeCodeHash = "0x" + strings.Repeat("d", 64)
	profile.RuntimeMetadataHash = digest
	for number := from; number <= 102; number++ {
		hash := fixture.source.chain.byHeight[number]
		fixture.source.chain.runtimeKVs[hash] = profile
		fixture.source.chain.metadataKVs[hash] = encoded
	}
	return profile
}

func monitorEconomicTestPolicyWrite(t *testing.T, fixture *monitorEconomicTestFixture) {
	t.Helper()
	fixture.services.policy.NativeEconomics = []monitorEconomicNativePolicy{fixture.policy}
	fixture.services.writePolicy(t)
}

func TestMonitorEconomicRuntimePublicUpgradeKeepsExecutionAndPostState(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	upgraded := monitorEconomicTestUpgrade(t, fixture, 101)
	original := fixture.policy.Observation.Runtime
	fixture.policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(original), monitorEconomicTestReview(upgraded)}
	// The old finite command still refuses this newly observed artifact.
	if result, err := observeEconomicEmission(t.Context(), fixture.source.client, fixture.source.policy, rootObjectHash(fixture.source.policy)); err == nil || result.Complete {
		t.Fatal("read renewal widened the original finite authority", err)
	}
	run := fixture.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.ObservedAlpha != "10" || monitorEconomicTestFee(event) != "12" || event.State.RuntimeAcknowledgement == nil || event.State.RuntimeAcknowledgement.Entries != 2 {
		t.Fatal("reviewed public runtime upgrade did not retain complete read evidence", event)
	}
	run.stop(t)
	record := fixture.record(t)
	if record.State.RuntimeBoundFrom != 101 || len(record.State.History) != 2 {
		t.Fatal("runtime upgrade lost its original bound history", record)
	}
	first, last := record.State.History[0], record.State.History[1]
	if first.ExecutionRuntime == nil || first.PostStateRuntime == nil || last.ExecutionRuntime == nil || last.PostStateRuntime == nil || *first.ExecutionRuntime != original || *first.PostStateRuntime != upgraded || *last.ExecutionRuntime != upgraded || *last.PostStateRuntime != upgraded {
		t.Fatal("upgrade block borrowed post-state code as execution authority", first, last)
	}
	if event.State.Authority != "owned-rpc-assertion" || event.State.ActualNativeOutcomeVerified || event.State.NativeMinerAllocationAlpha != nil {
		t.Fatal("reviewed runtime read manufactured economic proof", event)
	}
}

func TestMonitorEconomicRuntimePublicRenewalRetainsCursorAndPeers(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, true)
	fixture.policy.Observation.Through = economicEmissionBoundary{Number: 101, Hash: fixture.source.chain.byHeight[101]}
	fixture.policy.BatchBlocks = 1
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current || event.State.Cursor.Number != 101 {
		t.Fatal("original first page did not complete", event)
	}
	first.stop(t)
	original := fixture.record(t)
	upgraded := monitorEconomicTestUpgrade(t, fixture, 102)
	held := fixture.start(t, monitorServiceHooks{})
	event := held.next(t)
	if event.Current || event.Status != "runtime-unavailable" || event.State.Cursor != original.State.Cursor || event.State.BatchChainHash != original.State.BatchChainHash || event.State.ObservedAlpha != "10" {
		t.Fatal("unknown runtime discarded or advanced the original cursor", event)
	}
	select {
	case <-held.sink.peers:
	case <-held.done:
		t.Fatal("runtime hold stopped independent validator peer")
	case <-time.After(30 * time.Second):
		t.Fatal("independent validator peer did not sample")
	}
	held.peerResume <- struct{}{}
	select {
	case <-held.sink.peers:
	case <-held.done:
		t.Fatal("runtime hold stopped the next independent peer sample")
	case <-time.After(30 * time.Second):
		t.Fatal("independent peer did not continue")
	}
	held.stop(t)
	if held.exit != 0 {
		t.Fatal("canceled runtime hold did not join", held.exit)
	}
	fixture.policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(fixture.policy.Observation.Runtime), monitorEconomicTestReview(upgraded)}
	monitorEconomicTestPolicyWrite(t, fixture)
	resumed := fixture.start(t, monitorServiceHooks{})
	event = resumed.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || event.State.HistoryEntries != 2 || event.State.ObservedAlpha != "10" || monitorEconomicTestFee(event) != "12" {
		t.Fatal("reviewed renewal could not resume the original public owner", event)
	}
	resumed.stop(t)
	after := fixture.record(t)
	if rootObjectHash(after.State.History[0]) != rootObjectHash(original.State.History[0]) || after.PolicyHash != original.PolicyHash {
		t.Fatal("renewal relabeled original authority or a retained event")
	}
}

func TestMonitorEconomicRuntimePurposeRenewalDoesNotBorrowSigning(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	upgraded := monitorEconomicTestUpgrade(t, fixture, 101)
	stateOnly := monitorEconomicTestReview(upgraded)
	stateOnly.Purposes = []string{economicRuntimeStatePurpose}
	fixture.policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(fixture.policy.Observation.Runtime), stateOnly}
	run := fixture.start(t, monitorServiceHooks{})
	if event := run.next(t); event.Current || event.Status != "runtime-unavailable" || event.State.Cursor.Number != 100 || event.State.HistoryEntries != 0 {
		t.Fatal("state-only review granted event or fee authority", event)
	}
	run.stop(t)
	extra := monitorEconomicTestReview(upgraded)
	extra.Purposes = []string{economicRuntimeEventsPurpose, economicRuntimeFeePurpose}
	fixture.policy.RuntimeCatalog = append(fixture.policy.RuntimeCatalog, extra)
	monitorEconomicTestPolicyWrite(t, fixture)
	resumed := fixture.start(t, monitorServiceHooks{})
	if event := resumed.next(t); !event.Current || event.State.Cursor.Number != 102 || monitorEconomicTestFee(event) != "12" {
		t.Fatal("appended purpose review did not enable its exact read", event)
	}
	resumed.stop(t)
	chain, err := newRootCanonicalChain(fixture.source.client, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, fixture.policy.profiles())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := economicRuntimeFor(t.Context(), chain, fixture.source.chain.byHeight[102], fixture.policy.Observation, fixture.policy.RuntimeCatalog, economicRuntimeEventsPurpose); err != nil {
		t.Fatal("purpose-selected artifact did not enter bounded raw cache", err)
	}
	if _, err := chain.nativeRuntimeAt(t.Context(), fixture.source.chain.byHeight[102]); err == nil {
		t.Fatal("cached observational artifact bypassed root signing envelope")
	}
	if _, err := chain.nativeRuntimeAt(t.Context(), fixture.source.chain.byHeight[100]); err != nil {
		t.Fatal("unchanged original signing artifact no longer works", err)
	}
	if _, err := economicRuntimeFor(t.Context(), chain, fixture.source.chain.byHeight[100], fixture.policy.Observation, fixture.policy.RuntimeCatalog, "unknown-purpose"); !errors.Is(err, errRootReceiptProfileUnavailable) {
		t.Fatal("unknown consumed purpose became authority", err)
	}
}

func TestMonitorEconomicRuntimeCatalogCapacityCanGrowWithoutHistoryLoss(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	for index := 0; index < 8; index++ {
		profile := fixture.policy.Observation.Runtime
		profile.RuntimeVersion.SpecVersion += uint32(index)
		profile.RuntimeCodeHash = fmt.Sprintf("0x%064x", index+100)
		if index == 0 {
			profile = fixture.policy.Observation.Runtime
		}
		fixture.policy.RuntimeCatalog = append(fixture.policy.RuntimeCatalog, monitorEconomicTestReview(profile))
	}
	run := fixture.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current {
		t.Fatal("eight-artifact public catalog did not run", event)
	}
	run.stop(t)
	original := fixture.record(t)
	_, metrics := monitorEconomicNativePaths(fixture.services.checkpointPath, fixture.services.metricsPath, fixture.policy.Role)
	raw, err := os.ReadFile(metrics)
	if err != nil || !bytes.Contains(raw, []byte("sn_mainnet_native_economic_runtime_catalog_capacity_warning{role=\"native-a\"} 1")) {
		t.Fatal("reviewed catalog did not warn before its finite limit", err)
	}
	profile := fixture.policy.Observation.Runtime
	profile.RuntimeVersion.SpecVersion += 20
	profile.RuntimeCodeHash = "0x" + strings.Repeat("f", 64)
	fixture.policy.RuntimeCatalog = append(fixture.policy.RuntimeCatalog, monitorEconomicTestReview(profile))
	if err := fixture.policy.validateCatalog(); err == nil {
		t.Fatal("ninth artifact silently widened default capacity")
	}
	fixture.policy.RuntimeCapacity = &monitorEconomicRuntimeCapacity{Entries: 16, Bytes: 16 * 1024}
	monitorEconomicTestPolicyWrite(t, fixture)
	resumed := fixture.start(t, monitorServiceHooks{})
	event := resumed.next(t)
	if !event.Current || event.State.RuntimeAcknowledgement == nil || event.State.RuntimeAcknowledgement.Entries != 9 || event.State.RuntimeAcknowledgement.Capacity.Entries != 16 || event.State.Cursor != original.State.Cursor || event.State.BatchChainHash != original.State.BatchChainHash {
		t.Fatal("reviewed larger catalog discarded progress or could not reopen", event)
	}
	resumed.stop(t)
	if rootObjectHash(fixture.record(t).State.History) != rootObjectHash(original.State.History) {
		t.Fatal("capacity revision rewrote original runtime-bound history")
	}
}

func TestMonitorEconomicRuntimeRenewalAcknowledgesOnlyDurableSettings(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current {
		t.Fatal(event)
	}
	first.stop(t)
	original := fixture.record(t)
	basis := uint64(0)
	fixture.policy.ReadBudgetBasisSeconds, fixture.policy.ReadBudgetSeconds = &basis, 600
	fixture.policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(fixture.policy.Observation.Runtime)}
	monitorEconomicTestPolicyWrite(t, fixture)
	var failed atomic.Bool
	resumed := fixture.start(t, monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == fixture.policy.Role && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
			return errors.Join(err, syscall.EIO)
		}
		return err
	}})
	event := resumed.next(t)
	if event.Current || event.CheckpointCurrent || event.State.RuntimeAcknowledgement == nil || event.State.RuntimeAcknowledgement.Entries != 0 || event.State.RuntimeAcknowledgement.ReadBudgetSeconds != 300 || event.State.ConfiguredRuntimeEntries != 1 || event.State.ConfiguredReadBudgetSeconds != 600 || event.State.Cursor != original.State.Cursor {
		t.Fatal("unacknowledged renewal was advertised as committed", event)
	}
	resumed.resume <- struct{}{}
	event = resumed.next(t)
	if !event.Current || event.State.RuntimeAcknowledgement == nil || event.State.RuntimeAcknowledgement.Entries != 1 || event.State.RuntimeAcknowledgement.ReadBudgetSeconds != 600 || event.State.BatchCount != original.State.BatchCount {
		t.Fatal("lost-ack reopen did not join original cursor and renewed settings", event)
	}
	resumed.stop(t)
}

func TestMonitorEconomicRuntimeDecodedCacheIsBoundedAndCancellationJoins(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	profiles := []rootReceiptProfile{}
	blocks := []string{}
	for index := 0; index < 3; index++ {
		profile := fixture.policy.Observation.Runtime
		profile.RuntimeVersion.SpecVersion += uint32(index)
		profile.RuntimeCodeHash = fmt.Sprintf("0x%064x", index+200)
		block := fixture.source.chain.byHeight[uint64(100+index)]
		fixture.source.chain.runtimeKVs[block] = profile
		profiles, blocks = append(profiles, profile), append(blocks, block)
	}
	chain, err := newRootCanonicalChainBounded(fixture.source.client, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, profiles, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 1, 2, 0} {
		if _, err := chain.authenticatedRuntimeAt(t.Context(), blocks[index]); err != nil {
			t.Fatal("bounded immutable artifact could not be reread", err)
		}
		if len(chain.runtimeKVs) > 2 || len(chain.profiles) != 3 {
			t.Fatal("decoded cache grew or evicted retained review", len(chain.runtimeKVs), len(chain.profiles))
		}
	}
	fixture.source.chain.stateLock.Lock()
	reads := fixture.source.chain.counts["state_getMetadata"]
	fixture.source.chain.stateLock.Unlock()
	if reads != 4 {
		t.Fatal("evicted artifact did not require its authenticated bytes", reads)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := chain.authenticatedRuntimeAt(ctx, blocks[0]); !errors.Is(err, context.Canceled) {
		t.Fatal("cached read ignored owner cancellation", err)
	}
}

func TestMonitorEconomicRuntimeReviewsAreBoundedAndMonotonic(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	policy := fixture.policy
	policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(policy.Observation.Runtime)}
	capacity, seconds := policy.runtimeCapacity(), uint64(600)
	record := monitorEconomicNativeCheckpoint{RuntimeCatalog: policy.RuntimeCatalog, RuntimeCapacity: &capacity, ReadBudgetSeconds: &seconds}
	for _, change := range []string{"replacement", "removal", "unknown-purpose", "duplicate-purpose", "missing-review", "capacity-max", "byte-min", "retry-shrink", "history", "generation", "fee-payers", "initial-window", "stall"} {
		candidate := policy
		candidate.RuntimeCatalog = append([]monitorEconomicRuntimeEntry(nil), policy.RuntimeCatalog...)
		candidate.RuntimeCatalog[0].Purposes = append([]string(nil), policy.RuntimeCatalog[0].Purposes...)
		candidate.ReadBudgetSeconds = 600
		basis := uint64(0)
		candidate.ReadBudgetBasisSeconds = &basis
		switch change {
		case "replacement":
			candidate.RuntimeCatalog[0].ReviewSha256 = "sha256:" + strings.Repeat("8", 64)
		case "removal":
			candidate.RuntimeCatalog = nil
		case "unknown-purpose":
			candidate.RuntimeCatalog[0].Purposes[0] = "unreviewed-v2"
		case "duplicate-purpose":
			candidate.RuntimeCatalog[0].Purposes[1] = economicRuntimeStatePurpose
		case "missing-review":
			candidate.RuntimeCatalog[0].ReviewSha256 = ""
		case "capacity-max":
			candidate.RuntimeCapacity = &monitorEconomicRuntimeCapacity{Entries: 65, Bytes: 64 * 1024}
		case "byte-min":
			candidate.RuntimeCapacity = &monitorEconomicRuntimeCapacity{Entries: 8, Bytes: 1024}
		case "retry-shrink":
			candidate.ReadBudgetSeconds = 300
		case "history":
			candidate.HistoryEntries++
		case "generation":
			generation := *candidate.Observation.SubnetGeneration + 1
			candidate.Observation.SubnetGeneration = &generation
		case "fee-payers":
			candidate.Observation.FeePayers = nil
		case "initial-window":
			candidate.Observation.Through.Number--
		case "stall":
			candidate.StallSeconds++
		}
		if candidate.identityHash() == policy.identityHash() && candidate.retainsRuntimePolicy(record) == nil {
			t.Fatal("renewal changed retained semantics or shrank admission", change)
		}
	}
}
