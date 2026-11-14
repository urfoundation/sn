package validator

// V6 snapshots retain one immutable compact terminal plus the actual activated
// prefix. Existing mutable cursors stay authoritative; older v1 history remains
// byte-compatible in its original slot. Local decoding is not activation.

import (
	"errors"
	"fmt"
)

// Replaced as a whole under the Stats write owner, never mutated after publish.
// Terminal references typed streams rather than materializing ledger history.
type attemptStatsV2State struct {
	Activation          AttemptCutV2Activation         `json:"activation"`
	SettlementPriorRoot string                         `json:"settlement_prior_root"`
	Terminal            *AttemptSettlementTransitionV2 `json:"terminal,omitempty"`
}

// Rejects downgrade/omission and local cursor/EMA inconsistencies. Public
// startup still has to authenticate every member and referenced stream under
// its independent context/policy before any engine can admit a trail.
func validateAttemptStatsV2Snapshot(snapshot statsSnapshot) error {
	if snapshot.Version != 6 {
		if snapshot.AttemptV2 != nil {
			return errors.New("legacy statistics snapshot contains compact activation state")
		}
		return nil
	}
	state := snapshot.AttemptV2
	if state == nil || snapshot.SettlementEpoch == nil || snapshot.EgressGeneration == 0 || snapshot.AttemptLastAppliedSequence == ^uint64(0) || snapshot.AttemptSettlementFirstSequence == 0 || snapshot.AttemptEgressFirstSequence < snapshot.AttemptSettlementFirstSequence || snapshot.AttemptEgressFirstSequence > snapshot.AttemptLastAppliedSequence+1 {
		return errors.New("compact statistics activation or cursor state is incomplete")
	}
	activation := state.Activation
	if err := activation.Domain.Validate(); err != nil {
		return err
	}
	if activation.Hotkey == ([32]byte{}) || activation.FirstSequence == 0 || activation.FirstSequence == ^uint64(0) || snapshot.AttemptSettlementFirstSequence < activation.FirstSequence || *snapshot.SettlementEpoch < activation.Domain.ActivationEpoch {
		return errors.New("compact statistics precede their activation")
	}
	activationRoot, err := canonicalAttemptHex32("compact statistics activation root", activation.PriorRoot, true)
	if err != nil {
		return err
	}
	root, err := canonicalAttemptHex32("compact statistics settlement prior root", state.SettlementPriorRoot, true)
	if err != nil {
		return err
	}
	if (activation.FirstSequence == 1) != (activationRoot == ([32]byte{})) || (snapshot.AttemptSettlementFirstSequence == 1) != (root == ([32]byte{})) {
		return errors.New("compact statistics prior root differs from its sequence")
	}
	for provider := range snapshot.Ema {
		if _, exists := snapshot.EmaPPM[provider]; !exists {
			return errors.New("compact statistics cannot manufacture exact EMA from reporting floats")
		}
	}
	if len(snapshot.Ema) != len(snapshot.EmaPPM) {
		return errors.New("compact statistics reporting and exact EMA census differs")
	}
	var prior []AttemptSettlementQuality
	if terminal := state.Terminal; terminal != nil {
		cut := terminal.Cut
		if terminal.Schema != AttemptSettlementTransitionV2Schema || terminal.ToEpoch != *snapshot.SettlementEpoch || terminal.FromBoundary.SettlementEpoch == ^uint64(0) || terminal.ToEpoch != terminal.FromBoundary.SettlementEpoch+1 || terminal.Identity != cut.Context.Identity || terminal.FromBoundary != cut.Context.Boundary || cut.Context.Activation != activation || cut.LastSequence == ^uint64(0) || cut.Context.EgressGeneration == ^uint64(0) || snapshot.AttemptSettlementFirstSequence != cut.LastSequence+1 || state.SettlementPriorRoot != cut.Root || snapshot.EgressGeneration <= cut.Context.EgressGeneration || terminal.PreFold.AttemptCut != nil || terminal.PreFold.SettlementTransition != nil {
			return errors.New("compact statistics terminal or successor generation differs")
		}
		if err := cut.Context.Validate(); err != nil {
			return err
		}
		prior = terminal.PostFold
	} else {
		if *snapshot.SettlementEpoch != activation.Domain.ActivationEpoch || snapshot.AttemptSettlementFirstSequence != activation.FirstSequence || state.SettlementPriorRoot != activation.PriorRoot {
			return errors.New("compact statistics lost their first terminal history")
		}
		if legacy := snapshot.SettlementTransition; legacy != nil {
			if legacy.ToEpoch != *snapshot.SettlementEpoch {
				return errors.New("compact activation legacy prior belongs to another epoch")
			}
			prior = legacy.PostFold
		}
	}
	if len(prior) != len(snapshot.EmaPPM) {
		return errors.New("compact statistics exact prior EMA census differs")
	}
	previous := ""
	for _, quality := range prior {
		value, exists := snapshot.EmaPPM[quality.ClientID]
		if quality.ClientID <= previous || !quality.HasQuality || !exists || value != quality.QualityPPM || value > 1_000_000 {
			return fmt.Errorf("compact statistics prior EMA differs for %s", quality.ClientID)
		}
		previous = quality.ClientID
	}
	return nil
}
