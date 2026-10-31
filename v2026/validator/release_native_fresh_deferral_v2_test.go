//go:build linux || darwin

// Genuine signed journals and terminal closure remain immutable when an empty
// fresh generation crosses the settlement clock before its first native intent.
package validator

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

// Exercise real evidence authentication separately from the testnet permission
// check: this fixture deliberately uses synthetic native-chain identities.
func TestFreshNativeClosedInputRetainsEvidenceAndWaitsForNextEpoch(t *testing.T) {
	fixture, history, snapshot := newProvisionalClosedNativeInputFixture(t)
	history.cfg.ProvisionalDeferClosedNativeInput = false
	noId := fixture.disk.participants[0].NoID
	journal := history.inputByEpoch[1][noId]
	input := journal.MeasurementInput
	path := releaseMeasurementInputV2Path(fixture.cfg.StateDir, 1, noId)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reads, attempts := 0, 0
	err = runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
		reads++
		if reads <= releaseSteeringFailureLimit+2 {
			return 1, nil
		}
		return 2, nil
	}, func() error {
		attempts++
		if attempts == 1 {
			return history.authenticateClosedNativeInputDeferral(t.Context(), nil, 1, input.CutNativeBlock, input.CutNativeBlockHash, snapshot, true)
		}
		if reads != releaseSteeringFailureLimit+3 {
			t.Fatal("closed input was submitted again in its native epoch")
		}
		return history.authenticateClosedNativeInputDeferral(t.Context(), nil, 2, input.CutNativeBlock+1, input.CutNativeBlockHash, snapshot, true)
	}, func() bool { return reads < releaseSteeringFailureLimit+3 }, false, true)
	if err != nil || attempts != 2 {
		t.Fatalf("fresh closed input consumed restart budget: attempts=%d err=%v", attempts, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("fresh deferral rewrote immutable signed input")
	}
	for _, prior := range []*SteeringIntent{{SubnetEpoch: 1, Status: "pending"}, {SubnetEpoch: 0, Status: "applied"}, {SubnetEpoch: 0, Status: "failed"}} {
		if err := history.authenticateClosedNativeInputDeferral(t.Context(), prior, 1, input.CutNativeBlock, input.CutNativeBlockHash, snapshot, true); err == nil || errors.Is(err, errProvisionalClosedNativeInput) {
			t.Fatalf("prior native intent acquired fresh deferral: %v", err)
		}
	}
	closure := history.terminals[input.SettlementEpoch]
	delete(history.terminals, input.SettlementEpoch)
	if err := history.authenticateClosedNativeInputDeferral(t.Context(), nil, 1, input.CutNativeBlock, input.CutNativeBlockHash, snapshot, true); err == nil || errors.Is(err, errProvisionalClosedNativeInput) {
		t.Fatalf("missing terminal received deferral: %v", err)
	}
	history.terminals[input.SettlementEpoch] = closure
	journal.MeasurementInput.AttemptCutV2.Signature[0] ^= 1
	if err := history.authenticateClosedNativeInputDeferral(t.Context(), nil, 1, input.CutNativeBlock, input.CutNativeBlockHash, snapshot, true); err == nil || errors.Is(err, errProvisionalClosedNativeInput) {
		t.Fatalf("malformed signed cut received deferral: %v", err)
	}
}

// Fresh permission cannot consume the historical gap marker or erase a prior
// real error when a completed terminal later makes a deferral possible.
func TestFreshNativeClosedInputLoopRejectsUnownedAndMixedDeferral(t *testing.T) {
	for _, first := range []error{&provisionalClosedNativeInput{nativeEpoch: 1, activeSettlement: 8}, errors.New("actual journal failure")} {
		attempts := 0
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 1, nil }, func() error {
			attempts++
			if attempts == 1 {
				return first
			}
			return &provisionalClosedNativeInput{nativeEpoch: 1, activeSettlement: 8, beforeFirstIntent: true}
		}, func() bool { return attempts <= releaseSteeringFailureLimit }, false, true)
		if err == nil || attempts != releaseSteeringFailureLimit {
			t.Fatalf("fresh deferral erased prior failure: attempts=%d err=%v", attempts, err)
		}
	}
}
