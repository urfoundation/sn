// Runtime execution context changes scheduling anchors and UID cardinality
// without establishing a miner denominator, recipient generation or payment.
package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Rebuild the terminal header/body commitment before adding an extrinsic event.
// The archive remains canonical, so the failure isolates event interpretation.
func economicEmissionContextBody(t *testing.T, fixture *economicEmissionFixture) {
	t.Helper()
	old := fixture.chain.byHeight[102]
	header, hash := rootReceiptHeaderFixture(t, fixture.chain.byHeight[101], 102, [][]byte{{8, 4, 0}}, false)
	fixture.chain.headers[hash], fixture.chain.byHeight[102], fixture.chain.bodies[hash] = header, hash, []string{"0x080400"}
	fixture.storageKVs[hash] = fixture.storageKVs[old]
	fixture.chain.finalized, fixture.policy.Through.Hash = hash, hash
}

// AdminUtils resets LastEpochBlock but does not consume an epoch. The ordinary
// TempoSet event plus unchanged epoch index must not abort a complete read.
func TestEconomicEmissionObservesTempoResetWithoutConsumedEpoch(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	economicEmissionContextBody(t, fixture)
	fixture.set(t, 102, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 102))
	fixture.set(t, 102, "Tempo", []byte{100, 0})
	event := rootReceiptEventFixture(t, fixture.chain.metadata, "SubtensorModule.TempoSet", 0, []byte{25, 0}, []byte{100, 0})
	fixture.set(t, 102, "Events", append([]byte{4}, event...))
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || len(result.Blocks) != 2 || result.ObservedIncentiveTotalAlpha != "10" || result.Blocks[1].After.Epoch != 8 {
		t.Fatalf("valid tempo reset aborted the contiguous observation: complete=%t blocks=%d amount=%s err=%v", result.Complete, len(result.Blocks), result.ObservedIncentiveTotalAlpha, err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// The tempo setter respects a deferred epoch freeze for owner and root. An
// anchor reset after deferral remains a conflict even if TempoSet is present.
func TestEconomicEmissionRejectsDeferredEpochAnchorReset(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	economicEmissionContextBody(t, fixture)
	fixture.set(t, 102, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 102))
	fixture.set(t, 102, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 103))
	fixture.set(t, 102, "Tempo", []byte{100, 0})
	deferred := economicEmissionTestEvent(t, fixture.chain.metadata, "EpochDeferred", []byte{25, 0}, binary.LittleEndian.AppendUint64(nil, 102), binary.LittleEndian.AppendUint64(nil, 103))
	tempo := rootReceiptEventFixture(t, fixture.chain.metadata, "SubtensorModule.TempoSet", 0, []byte{25, 0}, []byte{100, 0})
	fixture.set(t, 102, "Events", append(append([]byte{8}, deferred...), tempo...))
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err == nil || result.Complete || len(result.Blocks) != 1 || result.AttemptedBlock == nil || result.ObservedIncentiveTotalAlpha != "10" {
		t.Fatalf("deferred epoch anchor reset escaped the reviewed freeze: %v", err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// v470's conviction takeover can append one owner neuron after budget drain
// and before Yuma. SubnetOwnerChanged, not NeuronRegistered, witnesses that path.
func TestEconomicEmissionObservesOwnerTakeoverUidAppend(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	fixture.set(t, 101, "SubnetworkN", []byte{3, 0})
	fixture.set(t, 102, "SubnetworkN", []byte{3, 0})
	owner := economicEmissionTestEvent(t, fixture.chain.metadata, "SubnetOwnerChanged", []byte{25, 0}, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	vector := []byte{12}
	for _, value := range []uint64{9, 1, 0} {
		vector = binary.LittleEndian.AppendUint64(vector, value)
	}
	incentive := economicEmissionTestEvent(t, fixture.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, vector)
	fixture.set(t, 101, "Events", append(append([]byte{8}, owner...), incentive...))
	result, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !result.Complete || len(result.Blocks) != 2 || len(result.Blocks[0].Events[0].AlphaByUid) != 3 || result.ObservedIncentiveTotalAlpha != "10" {
		t.Fatalf("valid initialization takeover append aborted observation: %v", err)
	}
	economicEmissionAssertUnresolved(t, result)
}
