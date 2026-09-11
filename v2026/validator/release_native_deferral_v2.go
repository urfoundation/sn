//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"strings"
)

// Called under the live settlement owner after its current snapshot has been
// advanced. Startup/live replay supplied these contexts; on-disk input bytes
// and both ordinary/terminal signatures are checked again before deferral.
// A second input is never manufactured for the same native epoch.
func (self *releaseEvidenceV2StartupHistory) provisionalClosedInputDeferral(ctx context.Context, current *SteeringIntent, nativeEpoch, nativeBlock uint64, nativeHash string, snapshot *ReleaseSnapshot) error {
	if !provisionalClosedNativeInputEnabled(&self.cfg) {
		return nil
	}
	if ctx == nil || snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() {
		return errors.New("provisional native input decision is incomplete")
	}
	active := snapshot.Epoch.Uint64()
	inputs := self.inputByEpoch[nativeEpoch]
	closed := false
	for _, input := range inputs {
		closed = closed || input != nil && input.MeasurementInput.SettlementEpoch < active
	}
	if !closed {
		return ctx.Err()
	}
	if current != nil && (current.SubnetEpoch >= nativeEpoch || current.Status == "pending") {
		return errors.New("provisional native input deferral cannot replace an existing native intent")
	}
	if len(inputs) != len(self.participants) || len(inputs) == 0 {
		return errors.New("provisional native input deferral requires the complete retained operator census")
	}
	for _, participant := range self.participants {
		noID := participant.NoID
		journal := inputs[noID]
		if journal == nil {
			return errors.New("provisional native input deferral is missing an operator")
		}
		input := journal.MeasurementInput
		cursor, owned := self.current[noID]
		if journal.Schema != releaseMeasurementInputV2Schema || journal.DeploymentID != self.cfg.DeploymentID || journal.ChainID != self.cfg.ChainID || !strings.EqualFold(journal.GenesisHash, self.cfg.GenesisHash) || !strings.EqualFold(journal.Coordinator, self.cfg.Coordinator) || journal.ValidatorID != self.cfg.ValidatorID || journal.Netuid != self.cfg.Netuid || journal.SubnetEpoch != nativeEpoch || !strings.EqualFold(journal.PolicyHash, self.cfg.PolicyHash) || input.NoID != noID || input.SettlementEpoch >= active || !owned || cursor.epoch != active || !releaseBlockAtOrBefore(input.CutNativeBlock, input.CutNativeBlockHash, nativeBlock, nativeHash) || !releaseBlockAtOrBefore(input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash, snapshot.BlockNumber, releaseHex32(snapshot.BlockHash)) {
			return errors.New("provisional native input differs beyond its closed settlement")
		}
		cut := input.AttemptCutV2
		expected, authenticated := self.inputContextsByEpoch[nativeEpoch][noID]
		if !authenticated || cut == nil || cut.Context.Boundary.SettlementEpoch != input.SettlementEpoch || cut.Context.Boundary.EVMBlock != input.CutEVMSnapshotBlock || cut.Context.Boundary.EVMBlockHash != input.CutEVMSnapshotHash || cut.Context.EgressGeneration != input.EgressGeneration {
			return errors.New("provisional native input lacks its authenticated signed context")
		}
		if err := cut.VerifyHeader(expected, self.cfg.EvidenceV2.Bounds.Cut); err != nil {
			return err
		}
		if err := validateReleaseMeasurementInputV2Context(&self.cfg, noID, expected); err != nil {
			return err
		}
		encoded, err := readReleaseMeasurementInputV2Context(ctx, releaseMeasurementInputV2Path(self.cfg.StateDir, nativeEpoch, noID), self.cfg.EvidenceV2.Bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{})
		if err != nil {
			return err
		}
		retained, err := canonicalReleaseMeasurementInputBytes(journal)
		if err != nil || !bytes.Equal(encoded, retained) {
			return errors.Join(errors.New("provisional native input bytes differ from authenticated history"), err)
		}
		closure := self.terminals[input.SettlementEpoch]
		if closure == nil || closure.Epoch != input.SettlementEpoch || len(closure.Transitions) != len(self.participants) {
			return errors.New("provisional native input has no complete authenticated terminal closure")
		}
		var terminal *AttemptCutV2
		for _, transition := range closure.Transitions {
			if transition != nil && transition.Identity.NoID == noID {
				terminal = &transition.Cut
			}
		}
		terminalContext, authenticated := self.terminalContexts[input.SettlementEpoch][noID]
		if !authenticated || terminal == nil || terminal.Context.Identity != cut.Context.Identity || terminal.Context.EgressGeneration <= cut.Context.EgressGeneration || terminal.LastSequence < cut.LastSequence || terminal.Context.Boundary.SettlementEpoch != input.SettlementEpoch {
			return errors.New("provisional native input is not covered by its authenticated terminal")
		}
		if err := terminal.VerifyHeader(terminalContext, self.cfg.EvidenceV2.Bounds.Cut); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return &provisionalClosedNativeInput{nativeEpoch: nativeEpoch, activeSettlement: active}
}
