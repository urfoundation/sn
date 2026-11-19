// Negative context controls keep archive completeness separate from economic
// authority and prevent unrelated, late or malformed events excusing conflicts.
package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// An unchecked tempo write need not move the anchor. A scheduled reset can
// happen in initialization; neither case proves an additional consumed epoch.
func TestEconomicEmissionTempoContextPreservesUncheckedAndScheduledUpdates(t *testing.T) {
	for _, reset := range []bool{false, true} {
		fixture := newEconomicEmissionFixture(t)
		fixture.set(t, 102, "Tempo", []byte{200, 0})
		if reset {
			fixture.set(t, 102, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 102))
		}
		event := economicEmissionTestEvent(t, fixture.chain.metadata, "TempoSet", []byte{25, 0}, []byte{200, 0})
		fixture.set(t, 102, "Events", append([]byte{4}, event...))
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if err != nil || !result.Complete || result.ObservedIncentiveTotalAlpha != "10" {
			t.Fatalf("reviewed tempo update reset=%t failed: %v", reset, err)
		}
		block := result.Blocks[1]
		if len(block.ContextEvents) != 1 || block.ContextEvents[0].Phase != "Initialization" || block.ContextEvents[0].ExtrinsicIndex != nil || block.ContextEvents[0].Tempo == nil || *block.ContextEvents[0].Tempo != 200 || block.After.Tempo != 200 {
			t.Fatalf("tempo context or storage lost: %+v", block.ContextEvents)
		}
		if reset && block.Denominator.Status != "unresolved-schedule-reset-observed" || !reset && block.Denominator.Status != "unresolved-no-incentive-event" {
			t.Fatalf("tempo update misclassified reset=%t: %s", reset, block.Denominator.Status)
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// Reset correspondence requires the target event, current anchor, unchanged
// epoch index and the last emitted tempo. No one of those facts substitutes for another.
func TestEconomicEmissionTempoResetRejectsUnexplainedState(t *testing.T) {
	for _, fault := range []string{"missing", "other-subnet", "tempo-mismatch", "epoch-advanced", "noncurrent-anchor", "last-tempo-mismatch", "saturated-epoch"} {
		fixture := newEconomicEmissionFixture(t)
		economicEmissionContextBody(t, fixture)
		fixture.set(t, 102, "Tempo", []byte{100, 0})
		fixture.set(t, 102, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 102))
		netuid := byte(25)
		if fault == "other-subnet" {
			netuid = 26
		}
		event := rootReceiptEventFixture(t, fixture.chain.metadata, "SubtensorModule.TempoSet", 0, []byte{netuid, 0}, []byte{100, 0})
		raw := append([]byte{4}, event...)
		switch fault {
		case "missing":
			raw = []byte{0}
		case "tempo-mismatch":
			fixture.set(t, 102, "Tempo", []byte{99, 0})
		case "epoch-advanced":
			fixture.set(t, 102, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 9))
		case "noncurrent-anchor":
			fixture.set(t, 102, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 100))
		case "last-tempo-mismatch":
			later := rootReceiptEventFixture(t, fixture.chain.metadata, "SubtensorModule.TempoSet", 0, []byte{25, 0}, []byte{99, 0})
			raw = append(append([]byte{8}, event...), later...)
		case "saturated-epoch":
			fixture.set(t, 100, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, math.MaxUint64-1))
			fixture.set(t, 101, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, math.MaxUint64))
			fixture.set(t, 102, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, math.MaxUint64))
		}
		fixture.set(t, 102, "Events", raw)
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if err == nil || result.Complete || len(result.Blocks) != 1 || result.AttemptedBlock == nil || result.AttemptedBlock.After == nil || result.ObservedIncentiveTotalAlpha != "10" {
			t.Fatalf("unexplained %s tempo state passed: %v", fault, err)
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// Several valid writes in a block retain their ordered values and exact body
// index. Only the final emitted value corresponds to the final storage snapshot.
func TestEconomicEmissionTempoContextRetainsOrderedWrites(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	economicEmissionContextBody(t, fixture)
	fixture.set(t, 102, "Tempo", []byte{100, 0})
	fixture.set(t, 102, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 102))
	first := economicEmissionTestEvent(t, fixture.chain.metadata, "TempoSet", []byte{25, 0}, []byte{90, 0})
	last := rootReceiptEventFixture(t, fixture.chain.metadata, "SubtensorModule.TempoSet", 0, []byte{25, 0}, []byte{100, 0})
	fixture.set(t, 102, "Events", append(append([]byte{8}, first...), last...))
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || len(result.Blocks[1].ContextEvents) != 2 {
		t.Fatalf("ordered tempo writes were lost: %v", err)
	}
	contexts := result.Blocks[1].ContextEvents
	if contexts[0].EventIndex != 0 || contexts[0].Phase != "Initialization" || *contexts[0].Tempo != 90 || contexts[1].EventIndex != 1 || contexts[1].Phase != "ApplyExtrinsic" || contexts[1].ExtrinsicIndex == nil || *contexts[1].ExtrinsicIndex != 0 || *contexts[1].Tempo != 100 {
		t.Fatalf("tempo event positions or values were collapsed: %+v", contexts)
	}
	economicEmissionAssertUnresolved(t, result)
}

// A takeover's one optional append is witnessed before the incentive event.
// Later, duplicated, unrelated and oversized explanations cannot widen that bound.
func TestEconomicEmissionTakeoverRejectsUnexplainedUidVector(t *testing.T) {
	for _, fault := range []string{"missing", "other-subnet", "late", "duplicate", "finalization", "same-owner", "two-appends"} {
		fixture := newEconomicEmissionFixture(t)
		count, netuid := uint16(3), uint16(25)
		if fault == "other-subnet" {
			netuid = 26
		}
		if fault == "two-appends" {
			count = 4
		}
		fixture.set(t, 101, "SubnetworkN", binary.LittleEndian.AppendUint16(nil, count))
		oldOwner, newOwner := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
		if fault == "same-owner" {
			newOwner = oldOwner
		}
		owner := economicEmissionTestEvent(t, fixture.chain.metadata, "SubnetOwnerChanged", binary.LittleEndian.AppendUint16(nil, netuid), oldOwner, newOwner)
		if fault == "finalization" {
			owner[0] = 1
		}
		vector := append(rootCompact(uint64(count)), make([]byte, int(count)*8)...)
		binary.LittleEndian.PutUint64(vector[1:], 10)
		incentive := economicEmissionTestEvent(t, fixture.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, vector)
		raw := append(append([]byte{8}, owner...), incentive...)
		switch fault {
		case "missing":
			raw = append([]byte{4}, incentive...)
		case "late":
			raw = append(append([]byte{8}, incentive...), owner...)
		case "duplicate":
			raw = append(append(append([]byte{12}, owner...), owner...), incentive...)
		}
		fixture.set(t, 101, "Events", raw)
		result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
		if err == nil || result.Complete || len(result.Blocks) != 0 || result.AttemptedBlock == nil || result.ObservedIncentiveTotalAlpha != "0" {
			t.Fatalf("unexplained %s UID vector passed: %v", fault, err)
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// An already registered challenger can take ownership without changing count.
// The coldkeys are retained, but neither coldkey identifies the rewarded hotkey slots.
func TestEconomicEmissionTakeoverWithoutAppendRetainsRecipientUncertainty(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	owner := economicEmissionTestEvent(t, fixture.chain.metadata, "SubnetOwnerChanged", []byte{25, 0}, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	vector := binary.LittleEndian.AppendUint64([]byte{8}, 9)
	vector = binary.LittleEndian.AppendUint64(vector, 1)
	incentive := economicEmissionTestEvent(t, fixture.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, vector)
	fixture.set(t, 101, "Events", append(append([]byte{8}, owner...), incentive...))
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || len(result.Blocks[0].ContextEvents) != 1 || result.ObservedIncentiveTotalAlpha != "10" {
		t.Fatalf("takeover without append failed: %v", err)
	}
	context := result.Blocks[0].ContextEvents[0]
	if context.EventIndex != 0 || result.Blocks[0].Events[0].EventIndex != 1 || context.OldOwnerColdkey != "0x"+strings.Repeat("01", 32) || context.NewOwnerColdkey != "0x"+strings.Repeat("02", 32) || !strings.Contains(strings.Join(result.Blocks[0].Denominator.Blockers, " "), "conviction takeover") {
		t.Fatal("takeover provenance or unresolved recipient generation was lost")
	}
	economicEmissionAssertUnresolved(t, result)
}

// Compatible registry reindexing is supported; changed field identity or wire
// widths cannot inherit reviewed semantics merely by carrying a metadata hash.
func TestEconomicEmissionContextAuthenticatesMetadataSemantics(t *testing.T) {
	for _, change := range []string{"reindex", "tempo-width", "tempo-name", "owner-width", "owner-name", "owner-absent"} {
		metadata, _, _ := economicEmissionTestMetadata(t, func(metadata *types.Metadata) {
			for _, pallet := range metadata.AsMetadataV14.Pallets {
				if pallet.Name != "SubtensorModule" {
					continue
				}
				variants := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()].Def.Variant.Variants
				tempo, owner := -1, -1
				for index := range variants {
					if variants[index].Name == "TempoSet" {
						tempo = index
					}
					if variants[index].Name == "SubnetOwnerChanged" {
						owner = index
					}
				}
				if tempo < 0 || owner < 0 {
					t.Fatal("independent context fixture lacks required variants")
				}
				switch change {
				case "reindex":
					variants[tempo].Index, variants[owner].Index = variants[owner].Index, variants[tempo].Index
				case "tempo-width":
					variants[tempo].Fields[1].Type = variants[owner].Fields[1].Type
				case "tempo-name":
					variants[tempo].Fields[1].HasName, variants[tempo].Fields[1].Name = true, "tempo"
				case "owner-width":
					variants[owner].Fields[1].Type = variants[tempo].Fields[1].Type
				case "owner-name":
					variants[owner].Fields[1].Name = "hotkey"
				case "owner-absent":
					variants[owner].Name = "UnknownOwnerChange"
				}
			}
		})
		if change != "reindex" {
			if _, err := economicEmissionEventProfile(metadata); err == nil {
				t.Fatalf("context %s inherited reviewed semantics", change)
			}
			continue
		}
		owner := economicEmissionTestEvent(t, metadata, "SubnetOwnerChanged", []byte{25, 0}, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
		tempo := rootReceiptEventFixture(t, metadata, "SubtensorModule.TempoSet", 0, []byte{25, 0}, []byte{100, 0})
		events, contexts, err := decodeEconomicEmissionEvents(metadata, append(append([]byte{8}, owner...), tempo...), 1, 25, 8, 102)
		if err != nil || len(events) != 0 || len(contexts) != 2 || contexts[0].Kind != "SubtensorModule.SubnetOwnerChanged" || contexts[1].Tempo == nil || *contexts[1].Tempo != 100 {
			t.Fatalf("compatible context reindex failed: %v", err)
		}
	}
}

// Invalid context phases, out-of-body indices and a malformed suffix fail the
// whole traversal even when a preceding context event was otherwise valid.
func TestEconomicEmissionContextRejectsMalformedEnvelope(t *testing.T) {
	metadata, _, _ := economicEmissionTestMetadata(t, nil)
	for _, fault := range []string{"tempo-finalization", "owner-extrinsic", "body-index", "trailing-bytes", "truncated-owner"} {
		event := economicEmissionTestEvent(t, metadata, "TempoSet", []byte{25, 0}, []byte{100, 0})
		switch fault {
		case "tempo-finalization":
			event[0] = 1
		case "owner-extrinsic":
			event = rootReceiptEventFixture(t, metadata, "SubtensorModule.SubnetOwnerChanged", 0, []byte{25, 0}, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
		case "body-index":
			event = rootReceiptEventFixture(t, metadata, "SubtensorModule.TempoSet", 1, []byte{25, 0}, []byte{100, 0})
		}
		raw := append([]byte{4}, event...)
		if fault == "trailing-bytes" {
			raw = append(raw, 0)
		}
		if fault == "truncated-owner" {
			owner := economicEmissionTestEvent(t, metadata, "SubnetOwnerChanged", []byte{25, 0}, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
			raw = append(append([]byte{8}, event...), owner[:len(owner)-1]...)
		}
		if _, _, err := decodeEconomicEmissionEvents(metadata, raw, 1, 25, 8, 102); err == nil {
			t.Fatalf("context %s envelope accepted", fault)
		}
	}
}
