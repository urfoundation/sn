//go:build linux || darwin

package validator

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"testing"
)

// Actual ordinary cuts and terminal publication are replayed by the same
// dormant-state startup owner used in production before testing deferral.
func newProvisionalClosedNativeInputFixture(t *testing.T) (*releaseStartupV2TestFixture, *releaseEvidenceV2StartupHistory, *ReleaseSnapshot) {
	t.Helper()
	fixture := newReleaseStartupV2TestFixture(t, true)
	for index := range fixture.disk.participants {
		fixture.ordinary(t, index, 1, false)
	}
	fixture.terminal(t, false)
	fixture.reopen(t)
	fixture.cfg.ProvisionalDeferClosedNativeInput = true
	var history *releaseEvidenceV2StartupHistory
	err := startReleaseEvidenceV2DiskStateOwned(t.Context(), &fixture.cfg, fixture.chain, fixture.nativeFixture.chain, fixture.inputs, fixture.keys, [2]string{fixture.replicas[0].Origin, fixture.replicas[1].Origin}, fixture.disk, fixture.nativeFixture.expected, attemptSettlementV2PhysicalIO(), &history)
	if err != nil || history == nil {
		t.Fatalf("actual closed input startup: %v", err)
	}
	return fixture, history, &ReleaseSnapshot{Epoch: big.NewInt(8), BlockNumber: fixture.finalized, BlockHash: fixture.blocks[fixture.finalized]}
}

func TestProvisionalClosedNativeInputAuthenticatesAndPreservesClosedHistory(t *testing.T) {
	fixture, history, snapshot := newProvisionalClosedNativeInputFixture(t)
	before := releaseStartupV2TestDiskImages(t, fixture.disk)
	journal := history.inputByEpoch[1][fixture.disk.participants[0].NoID]
	nativeBlock, nativeHash := journal.MeasurementInput.CutNativeBlock, journal.MeasurementInput.CutNativeBlockHash
	err := history.provisionalClosedInputDeferral(t.Context(), nil, 1, nativeBlock, nativeHash, snapshot)
	var deferred *provisionalClosedNativeInput
	if !errors.As(err, &deferred) || deferred.nativeEpoch != 1 || deferred.activeSettlement != 8 {
		t.Fatalf("authenticated closed input was not deferred: %v", err)
	}
	for index, after := range releaseStartupV2TestDiskImages(t, fixture.disk) {
		if !bytes.Equal(before[index], after) {
			t.Fatal("deferral rewrote current statistics")
		}
	}
	for noID, input := range history.inputByEpoch[1] {
		actual, err := os.ReadFile(releaseMeasurementInputV2Path(fixture.cfg.StateDir, 1, noID))
		if err != nil {
			t.Fatal(err)
		}
		want, err := canonicalReleaseMeasurementInputBytes(input)
		if err != nil || !bytes.Equal(actual, want) {
			t.Fatalf("deferral rewrote signed input: %v", err)
		}
	}
	if err := history.provisionalClosedInputDeferral(t.Context(), nil, 2, nativeBlock+1, nativeHash, snapshot); err != nil {
		t.Fatalf("next native epoch did not resume ordinary collection: %v", err)
	}
	history.cfg.ProvisionalDeferClosedNativeInput = false
	if err := history.provisionalClosedInputDeferral(t.Context(), nil, 1, nativeBlock, nativeHash, snapshot); err != nil {
		t.Fatalf("strict path was intercepted: %v", err)
	}
}

func TestProvisionalClosedNativeInputRejectsUnclosedUnsafeAndChangedEvidence(t *testing.T) {
	fixture, history, snapshot := newProvisionalClosedNativeInputFixture(t)
	noID := fixture.disk.participants[0].NoID
	journal := history.inputByEpoch[1][noID]
	nativeBlock, nativeHash := journal.MeasurementInput.CutNativeBlock, journal.MeasurementInput.CutNativeBlockHash
	requireRejected := func(t *testing.T, intent *SteeringIntent) {
		t.Helper()
		err := history.provisionalClosedInputDeferral(t.Context(), intent, 1, nativeBlock, nativeHash, snapshot)
		if err == nil || errors.Is(err, errProvisionalClosedNativeInput) {
			t.Fatalf("unsafe input received deferral: %v", err)
		}
	}
	t.Run("same epoch intent", func(t *testing.T) { requireRejected(t, &SteeringIntent{SubnetEpoch: 1, Status: "pending"}) })
	t.Run("deployment mismatch", func(t *testing.T) {
		original := journal.DeploymentID
		journal.DeploymentID = "other-deployment"
		defer func() { journal.DeploymentID = original }()
		requireRejected(t, nil)
	})
	t.Run("missing terminal", func(t *testing.T) {
		original := history.terminals[7]
		delete(history.terminals, 7)
		defer func() { history.terminals[7] = original }()
		requireRejected(t, nil)
	})
	t.Run("wrong active owner", func(t *testing.T) {
		original := history.current[noID]
		changed := original
		changed.epoch = 7
		history.current[noID] = changed
		defer func() { history.current[noID] = original }()
		requireRejected(t, nil)
	})
	t.Run("bad signed cut", func(t *testing.T) {
		journal.MeasurementInput.AttemptCutV2.Signature[0] ^= 1
		defer func() { journal.MeasurementInput.AttemptCutV2.Signature[0] ^= 1 }()
		requireRejected(t, nil)
	})
	t.Run("changed physical journal", func(t *testing.T) {
		path := releaseMeasurementInputV2Path(fixture.cfg.StateDir, 1, noID)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(bytes.Clone(original), ' '), 0o600); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Error(err)
			}
		}()
		requireRejected(t, nil)
	})
}
