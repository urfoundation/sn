// Causal economic observation tests separate read completeness from economic
// proof and preserve unknowns through zero terms, missing data and chain forks.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// None of these fields can become an economic or launch approval by observing
// a finite archive range. Content hashing also binds partial/error artifacts.
func economicEmissionAssertUnresolved(t *testing.T, result economicEmissionObservation) {
	t.Helper()
	if result.NativeMinerAllocationAlpha != nil || result.ProviderEntitlementAlpha != nil || result.OwnerRecycledAlpha != nil || result.QuantizationToleranceAlpha != nil || result.TargetMet != nil || result.ActualNativeOutcomeVerified || result.ActivationReady || result.RuntimeSourceProven || result.IndependentStorageProof || len(result.Blockers) == 0 {
		t.Fatal("native incentive observation manufactured economic authority or an amount")
	}
	digest := result.ContentHash
	result.ContentHash = ""
	if !planSha256(digest) || rootObjectHash(result) != digest {
		t.Fatal("native incentive content hash does not bind retained evidence")
	}
	for _, block := range result.Blocks {
		if block.Denominator.Complete || block.Denominator.NativeMinerAllocationAlpha != nil || block.Denominator.RuntimeTruncationDustAlpha != nil || len(block.Denominator.Blockers) == 0 {
			t.Fatal("incentive event was promoted to complete denominator or dust proof")
		}
	}
}

// A successful read retains exact ancestry, execution metadata, event position
// and budgets without calling emitted10 the pre-withholding pending90 tranche.
func TestEconomicEmissionCollectsContiguousEvidenceWithoutClaimingTarget(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || result.Status != "observed-economic-outcome-unresolved" || len(result.Blocks) != 2 || result.AttemptedBlock != nil || result.ObservedIncentiveTotalAlpha != "10" {
		t.Fatalf("complete synthetic range failed: complete=%t blocks=%d amount=%s err=%v", result.Complete, len(result.Blocks), result.ObservedIncentiveTotalAlpha, err)
	}
	first := result.Blocks[0]
	if len(result.Ancestry) != 3 || len(result.ClosingAncestry) != 1 || result.Finalized != fixture.policy.Through || result.ClosingFinalized != result.Finalized || result.MetadataHex != fixture.chain.metadataHex || first.Header.ParentHash != fixture.policy.From.Hash || first.Events[0].EventIndex != 0 || first.Events[0].AlphaByUid[0] != "9" || first.Denominator.PriorPendingServerAlpha != "90" || first.Denominator.AfterPendingServerAlpha != "0" || result.Blocks[1].Before.Boundary != first.Boundary {
		t.Fatal("contiguous raw/budget/event lineage was not retained")
	}
	economicEmissionAssertUnresolved(t, result)
}

// Zero incentive can redirect a positive miner tranche to dividends. The
// zero-event sum must leave both reference entitlement and dust undefined.
func TestEconomicEmissionZeroIncentiveDoesNotEraseMinerTranche(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	fixture.incentive(t, 101, 25, 0, 0)
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || result.ObservedIncentiveTotalAlpha != "0" || !result.Blocks[0].Denominator.ZeroIncentiveFallback || result.Blocks[0].Denominator.PriorPendingServerAlpha != "90" {
		t.Fatalf("zero-incentive evidence lost fallback uncertainty: %v", err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// Summing independent u64 rewards needs arbitrary precision. A large exact
// amount still cannot yield a runtime-specific Q or a percentage tolerance.
func TestEconomicEmissionUsesExactAlphaBeyondUint64AndLeavesRoundingUnknown(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	fixture.incentive(t, 101, 25, math.MaxUint64, math.MaxUint64)
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || result.ObservedIncentiveTotalAlpha != "36893488147419103230" || result.Blocks[0].Events[0].AlphaByUid[1] != "18446744073709551615" {
		t.Fatalf("exact event sum lost integer precision: %s %v", result.ObservedIncentiveTotalAlpha, err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// Indices are selected from approved metadata, never a remembered runtime
// index. A compatible event reindex must survive an independently updated hash.
func TestEconomicEmissionAuthenticatesCompatibleEventReindex(t *testing.T) {
	metadata, _, originalHash := economicEmissionTestMetadata(t, nil)
	changed, _, changedHash := economicEmissionTestMetadata(t, func(metadata *types.Metadata) {
		for _, pallet := range metadata.AsMetadataV14.Pallets {
			if pallet.Name != "SubtensorModule" {
				continue
			}
			variants := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()].Def.Variant.Variants
			used := map[byte]bool{}
			for _, variant := range variants {
				used[byte(variant.Index)] = true
			}
			for index := range variants {
				if variants[index].Name != "IncentiveAlphaEmittedToMiners" {
					continue
				}
				for candidate := 255; candidate >= 0; candidate-- {
					if !used[byte(candidate)] {
						variants[index].Index = types.U8(candidate)
						return
					}
				}
			}
		}
		t.Fatal("no spare synthetic event index")
	})
	if originalHash == changedHash {
		t.Fatal("reindexed fixture did not change its independently encoded metadata hash")
	}
	for _, artifact := range []*types.Metadata{metadata, changed} {
		vector := binary.LittleEndian.AppendUint64([]byte{4}, 7)
		raw := append([]byte{4}, economicEmissionTestEvent(t, artifact, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, vector)...)
		events, _, err := decodeEconomicEmissionEvents(artifact, raw, 0, 25, 8, 101)
		if err != nil || len(events) != 1 || events[0].TotalAlpha != "7" {
			t.Fatalf("compatible authenticated event reindex failed: %v", err)
		}
	}
}

// Semantic changes to the envelope and selected fields cannot inherit the
// source meaning simply because their independently supplied metadata hashes.
func TestEconomicEmissionRejectsChangedMetadataSemantics(t *testing.T) {
	for _, change := range []string{"emission-width", "duplicate-name", "topics-width", "phase-index", "storage-hasher"} {
		metadata, _, _ := economicEmissionTestMetadata(t, func(metadata *types.Metadata) {
			for palletIndex := range metadata.AsMetadataV14.Pallets {
				pallet := &metadata.AsMetadataV14.Pallets[palletIndex]
				if pallet.Name == "SubtensorModule" {
					variants := &metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()].Def.Variant.Variants
					for index := range *variants {
						if (*variants)[index].Name == "IncentiveAlphaEmittedToMiners" {
							if change == "emission-width" {
								(*variants)[index].Fields[1].Type = (*variants)[index].Fields[0].Type
							} else if change == "duplicate-name" {
								copy := (*variants)[index]
								copy.Index = (*variants)[(index+1)%len(*variants)].Index
								*variants = append(*variants, copy)
							}
							break
						}
					}
					if change == "storage-hasher" {
						for index := range pallet.Storage.Items {
							if pallet.Storage.Items[index].Name == "MechanismCountCurrent" {
								pallet.Storage.Items[index].Type.AsMap.Hashers[0] = types.StorageHasherV10{IsIdentity: true}
							}
						}
					}
				}
			}
			entry, err := rootSystemEntry(metadata, "Events")
			if err != nil {
				t.Fatal(err)
			}
			sequence := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsPlainType.Int64()]
			record := metadata.AsMetadataV14.EfficientLookup[sequence.Def.Sequence.Type.Int64()]
			if change == "topics-width" {
				record.Def.Composite.Fields[2].Type = record.Def.Composite.Fields[0].Type
			}
			if change == "phase-index" {
				phase := metadata.AsMetadataV14.EfficientLookup[record.Def.Composite.Fields[0].Type.Int64()]
				phase.Def.Variant.Variants[0].Index = 9
			}
		})
		_, err := economicEmissionEventProfile(metadata)
		if change == "storage-hasher" {
			_, err = observationStorageProfile(metadata, economicEmissionStorageSpecs)
		}
		if err == nil {
			t.Fatalf("changed %s metadata inherited economic semantics", change)
		}
	}
}

// Finalization cannot masquerade as initialization. Netuid storage indices for
// mechanism1 belong to subnet25, not an unrelated subnet that can be ignored.
func TestEconomicEmissionRejectsWrongPhaseAndAdditionalMechanism(t *testing.T) {
	metadata, _, _ := economicEmissionTestMetadata(t, nil)
	vector := binary.LittleEndian.AppendUint64([]byte{4}, 9)
	for _, netuid := range []uint16{25, 25 + economicEmissionMechanismStride} {
		raw := append([]byte{4}, economicEmissionTestEvent(t, metadata, "IncentiveAlphaEmittedToMiners", binary.LittleEndian.AppendUint16(nil, netuid), vector)...)
		if netuid == 25 {
			raw[1] = 1
		}
		if _, _, err := decodeEconomicEmissionEvents(metadata, raw, 0, 25, 8, 101); err == nil {
			t.Fatalf("wrong phase or mechanism accepted for index %d", netuid)
		}
	}
	other := append([]byte{4}, economicEmissionTestEvent(t, metadata, "IncentiveAlphaEmittedToMiners", []byte{26, 0}, vector)...)
	events, _, err := decodeEconomicEmissionEvents(metadata, other, 0, 25, 8, 101)
	if err != nil || len(events) != 0 {
		t.Fatalf("unrelated valid subnet event was not traversed: %v", err)
	}
}

// Full-vector traversal and duplicate detection prevent hidden or double-counted
// terms. Malformed tails after a valid target event are equally disqualifying.
func TestEconomicEmissionRejectsDuplicateTruncatedAndOverboundEvents(t *testing.T) {
	metadata, _, _ := economicEmissionTestMetadata(t, nil)
	vector := binary.LittleEndian.AppendUint64([]byte{4}, 9)
	event := economicEmissionTestEvent(t, metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, vector)
	valid := append([]byte{4}, event...)
	duplicate := append(append([]byte{8}, event...), event...)
	for index, raw := range [][]byte{duplicate, valid[:len(valid)-1], append(append([]byte(nil), valid...), 0), {1, 0}, rootCompact(economicEmissionEventLimit + 1), make([]byte, economicEmissionEventBytesLimit+1)} {
		if _, _, err := decodeEconomicEmissionEvents(metadata, raw, 0, 25, 8, 101); err == nil {
			t.Fatalf("malformed or duplicate event case %d accepted", index)
		}
	}
	if _, _, err := decodeEconomicEmissionEvents(metadata, valid, 0, 25, 0, 101); err == nil {
		t.Fatal("zero UID budget accepted")
	}
	oversized := append([]byte{4}, economicEmissionTestEvent(t, metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, append([]byte{8}, make([]byte, 16)...))...)
	if _, _, err := decodeEconomicEmissionEvents(metadata, oversized, 0, 25, 1, 101); err == nil {
		t.Fatal("event vector exceeded independent UID bound")
	}
}

// Skipped slots consume the epoch counter; deferred slots retain it. Neither
// kind invents an emitted vector or declares pending miner allocation zero.
func TestEconomicEmissionRetainsSkippedAndDeferredEpochs(t *testing.T) {
	for _, kind := range []string{"EpochSkipped", "EpochDeferred"} {
		fixture := newEconomicEmissionFixture(t)
		fields := [][]byte{{25, 0}, binary.LittleEndian.AppendUint64(nil, 101)}
		if kind == "EpochDeferred" {
			fields = append(fields, binary.LittleEndian.AppendUint64(nil, 102))
			fixture.set(t, 101, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 7))
			fixture.set(t, 101, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 99))
			fixture.set(t, 101, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 102))
		}
		fixture.set(t, 101, "PendingServerEmission", binary.LittleEndian.AppendUint64(nil, 100))
		fixture.set(t, 101, "Events", append([]byte{4}, economicEmissionTestEvent(t, fixture.chain.metadata, kind, fields...)...))
		fixture.policy.Through = economicEmissionBoundary{Number: 101, Hash: fixture.chain.byHeight[101]}
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if err != nil || !result.Complete || len(result.Blocks) != 1 || result.Blocks[0].Events[0].Kind != "SubtensorModule."+kind || result.ObservedIncentiveTotalAlpha != "0" || result.Blocks[0].Denominator.AfterPendingServerAlpha != "100" {
			t.Fatalf("%s evidence lost: %v", kind, err)
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// Explicit null uses the authenticated empty Events default; an omitted result
// stops at the previous complete block and retains that prefix's exact amount.
func TestEconomicEmissionDistinguishesNullEventsFromMissingResult(t *testing.T) {
	for _, missing := range []bool{false, true} {
		fixture := newEconomicEmissionFixture(t)
		fixture.storageKVs[fixture.chain.byHeight[102]][fixture.eventsKey] = nil
		if missing {
			fixture.omitAt = fixture.chain.byHeight[102]
		}
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if missing {
			if err == nil || !strings.Contains(err.Error(), "missing result") || result.Complete || len(result.Blocks) != 1 || result.AttemptedBlock == nil || result.AttemptedBlock.Boundary.Number != 102 {
				t.Fatalf("missing result became empty emission: blocks=%d err=%v", len(result.Blocks), err)
			}
		} else if err != nil || !result.Complete || len(result.Blocks) != 2 || result.Blocks[1].RawEvents != "0x00" || result.Blocks[1].RawEventsStorage != nil {
			t.Fatalf("valid null default was not retained: %v", err)
		}
		if result.ObservedIncentiveTotalAlpha != "10" {
			t.Fatal("partial/empty block destroyed prior exact amount")
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// A new subnet generation and unsupported mechanism count cannot inherit the
// old event interpretation. The attempted block's raw bundle remains available.
func TestEconomicEmissionRejectsGenerationChangeAndUnapprovedMechanism(t *testing.T) {
	for _, name := range []string{"RegisteredSubnetCounter", "MechanismCountCurrent", "NetworkRegisteredAt"} {
		fixture := newEconomicEmissionFixture(t)
		raw := binary.LittleEndian.AppendUint64(nil, 4)
		if name == "MechanismCountCurrent" {
			raw = []byte{2}
		}
		key := fixture.set(t, 101, name, raw)
		if name == "NetworkRegisteredAt" {
			fixture.storageKVs[fixture.chain.byHeight[101]][key] = nil
		}
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if err == nil || result.Complete || len(result.Blocks) != 0 || result.AttemptedBlock == nil || result.AttemptedBlock.RawEvents == "" || result.AttemptedBlock.After == nil {
			t.Fatalf("changed %s inherited authority or erased raw evidence: %v", name, err)
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// Burn is an observed fact with a blocker, never silently substituted Recycle.
func TestEconomicEmissionObservesBurnWithoutPassingRecycleGate(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	key := fixture.set(t, 101, "RecycleOrBurn", []byte{0})
	fixture.storageKVs[fixture.chain.byHeight[101]][key] = nil
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || result.Blocks[0].After.RecycleMode != "Burn" || !strings.Contains(strings.Join(result.Blocks[0].Denominator.Blockers, " "), "observed Burn") {
		t.Fatalf("Burn mode observation was misrepresented: %v", err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// Conflicting anchors and a route's later canonical retraction both stop
// completeness. A retained prefix is evidence, not a successful result.
func TestEconomicEmissionRejectsAnchorAndClosingCanonicalConflicts(t *testing.T) {
	for _, closing := range []bool{false, true} {
		fixture := newEconomicEmissionFixture(t)
		if !closing {
			fixture.policy.From.Hash = "0x" + strings.Repeat("dd", 32)
		} else {
			prior := fixture.chain.fault
			var closingStarted atomic.Bool
			fixture.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
				if method == "chain_getFinalizedHead" && count == 2 {
					closingStarted.Store(true)
				}
				if closingStarted.Load() && method == "chain_getBlockHash" && len(params) == 1 && string(params[0]) == "102" {
					return "0x" + strings.Repeat("dd", 32), true
				}
				return prior(method, params, count)
			}
		}
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if !errors.Is(err, errRpcIntegrity) || result.Complete {
			t.Fatalf("finalized conflict passed: %v", err)
		}
		if closing && (len(result.Blocks) != 2 || result.ObservedIncentiveTotalAlpha != "10") {
			t.Fatal("closing conflict erased observed evidence")
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// An upgrade block uses parent metadata; unknown post-state code prevents its
// full-state claim while preserving events already decoded under that parent.
func TestEconomicEmissionUsesParentRuntimeAndRetainsEventsAtUnknownUpgrade(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	changed := fixture.policy.Runtime
	changed.RuntimeCodeHash = "0x" + strings.Repeat("dd", 32)
	fixture.chain.runtimeKVs[fixture.chain.byHeight[101]] = changed
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err == nil || result.Complete || len(result.Blocks) != 0 || result.AttemptedBlock == nil || len(result.AttemptedBlock.Events) != 1 || result.AttemptedBlock.Events[0].TotalAlpha != "10" || result.ObservedIncentiveTotalAlpha != "0" {
		t.Fatalf("unknown post-runtime erased parent event evidence or entered total: %v", err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// A complete event bundle cannot excuse a body-root mismatch, and a later
// failure cannot be presented as a successful empty interval.
func TestEconomicEmissionRejectsBodyCommitmentAndEpochCorrespondenceConflicts(t *testing.T) {
	for _, bodyConflict := range []bool{true, false} {
		fixture := newEconomicEmissionFixture(t)
		if bodyConflict {
			fixture.chain.bodies[fixture.chain.byHeight[101]] = []string{"0x080400"}
		} else {
			fixture.set(t, 101, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 9))
		}
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if err == nil || result.Complete || len(result.Blocks) != 0 || result.AttemptedBlock == nil {
			t.Fatalf("body/epoch conflict passed: %v", err)
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// An empty or unrelated-only Events bundle cannot silently hide a consumed
// target epoch. Retain the contradictory state instead of declaring zero M.
func TestEconomicEmissionRejectsMissingTerminalEventForConsumedEpoch(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	fixture.set(t, 101, "Events", []byte{0})
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err == nil || !strings.Contains(err.Error(), "advanced without") || result.Complete || len(result.Blocks) != 0 || result.AttemptedBlock == nil || result.AttemptedBlock.After == nil {
		t.Fatalf("missing epoch event became a zero-emission interval: %v", err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// Bounds are checked before opening any route; cancellation has one operation
// lifetime and yields a sealed unresolved artifact, never stale cached success.
func TestEconomicEmissionRejectsUnboundedPolicyAndPreservesCancellation(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	for _, mutate := range []func(*economicEmissionPolicy){
		func(policy *economicEmissionPolicy) {
			policy.Through.Number = policy.From.Number + economicEmissionBlockLimit + 1
		},
		func(policy *economicEmissionPolicy) { policy.SubnetGeneration = nil },
		func(policy *economicEmissionPolicy) { policy.MaximumUids = rootCensusLimit + 1 },
		func(policy *economicEmissionPolicy) { policy.Runtime.RuntimeSourceCommit = strings.Repeat("1", 40) },
	} {
		policy := fixture.policy
		mutate(&policy)
		result, err := observeEconomicEmission(t.Context(), nil, policy, rootObjectHash(policy))
		if err == nil || result.Complete {
			t.Fatal("unbounded/unapproved policy reached observation")
		}
		economicEmissionAssertUnresolved(t, result)
	}
	budget := economicEmissionBudget{used: economicEmissionBytesLimit - 1}
	if err := budget.retain("two"); err == nil {
		t.Fatal("retained-byte budget was not enforced")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := observeEconomicEmission(ctx, fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err == nil || result.Complete || len(result.Blocks) != 0 {
		t.Fatal("cancelled operation returned stale completeness")
	}
	economicEmissionAssertUnresolved(t, result)
}

// The public command publishes structured partial evidence and a nonzero exit
// after malformed archive data. It exposes no custody/sign/submit flags.
func TestEconomicEmissionCommandKeepsSuccessAndPartialOutcomeGatesUnresolved(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		fixture := newEconomicEmissionFixture(t)
		if incomplete {
			fixture.omitAt = fixture.chain.byHeight[102]
		}
		policyRaw, err := json.Marshal(fixture.policy)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "policy.json")
		if err := os.WriteFile(path, policyRaw, 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		// The client route is a disposable localhost httptest listener.
		code := runMain(t.Context(), []string{"observe-native-miner-emission", "--rpc", fixture.client.url, "--policy", path}, &stdout, &stderr)
		var result economicEmissionObservation
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("command lost structured evidence: %v %s", err, stderr.String())
		}
		if incomplete && (code != 3 || result.Complete || result.AttemptedBlock == nil) || !incomplete && (code != 0 || !result.Complete) {
			t.Fatalf("wrong read exit/status: %d %s", code, stderr.String())
		}
		economicEmissionAssertUnresolved(t, result)
		stdout.Reset()
		if code := runMain(t.Context(), []string{"observe-native-miner-emission", "--sign"}, &stdout, &stderr); code != 2 {
			t.Fatal("observation accepted a signing option")
		}
	}
}

// A well-formed other-subnet event still cannot hide malformed trailing target
// data. Event parsing always consumes the whole original SCALE storage value.
func TestEconomicEmissionTraversesUnrelatedEventsBeforeReturningTarget(t *testing.T) {
	metadata, _, _ := economicEmissionTestMetadata(t, nil)
	vector := binary.LittleEndian.AppendUint64([]byte{4}, 7)
	other := economicEmissionTestEvent(t, metadata, "IncentiveAlphaEmittedToMiners", []byte{26, 0}, vector)
	target := economicEmissionTestEvent(t, metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, vector)
	raw := append(append([]byte{8}, other...), target...)
	events, _, err := decodeEconomicEmissionEvents(metadata, raw, 0, 25, 8, 101)
	if err != nil || len(events) != 1 || events[0].EventIndex != 1 || events[0].TotalAlpha != "7" {
		t.Fatalf("target event index lost after other subnet: %v", err)
	}
	bad := append(append([]byte{8}, target...), other[:len(other)-1]...)
	if _, _, err := decodeEconomicEmissionEvents(metadata, bad, 0, 25, 8, 101); err == nil {
		t.Fatalf("malformed suffix accepted: %s", hex.EncodeToString(bad))
	}
}
