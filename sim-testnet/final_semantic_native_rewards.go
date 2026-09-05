package main

// This file verifies native emission causality at the exact Subtensor epoch
// block. Broad before/after stake snapshots are useful accounting evidence but
// can be inflated by an unrelated stake transaction. The public reader also
// proves the parent-to-epoch stake change and rejects every relevant manual
// stake mutation in that one-block interval.

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

const finalPublicNativePayoutAuditSchema = "urnetwork-final-public-native-payout-audit-v1"

// Seals the exact epoch/UID scope whose event, storage channels, and
// parent-to-reveal stake transitions were replayed. The
// generic transcript hash authenticates bytes; this summary prevents an older
// same-version producer from silently omitting the payout replay entirely.
type FinalPublicNativePayoutAudit struct {
	Schema            string `json:"schema"`
	Epochs            uint64 `json:"epochs"`
	UIDRows           uint64 `json:"uid_rows"`
	ParentTransitions uint64 `json:"parent_transitions"`
	ProjectionHash    string `json:"projection_hash"`
}

// Defines the canonical hash input for all exact native payout replay jobs.
type finalPublicNativePayoutProjection struct {
	Schema string                                   `json:"schema"`
	Netuid uint16                                   `json:"netuid"`
	Epochs []finalPublicNativePayoutEpochProjection `json:"epochs"`
}

// Binds one settlement epoch to its automatic native reveal/coinbase block and
// complete managed UID set.
type finalPublicNativePayoutEpochProjection struct {
	SettlementEpoch uint64                                    `json:"settlement_epoch"`
	SubnetEpoch     uint64                                    `json:"subnet_epoch"`
	Reveal          ChainHead                                 `json:"reveal"`
	Rewards         []finalPublicNativePayoutRewardProjection `json:"rewards"`
}

// Contains every source-derived value compared to the event and storage
// channels for one managed UID.
type finalPublicNativePayoutRewardProjection struct {
	Role                string    `json:"role"`
	SubjectID           uint64    `json:"subject_id"`
	UID                 uint16    `json:"uid"`
	Hotkey              string    `json:"hotkey_public_key"`
	CombinedEmissionRao string    `json:"combined_emission_rao"`
	IncentiveU16        uint16    `json:"incentive_u16"`
	DividendsU16        uint16    `json:"dividends_u16"`
	Expected            string    `json:"expected"`
	Before              ChainHead `json:"before"`
	After               ChainHead `json:"after"`
}

// Derives the exact replay projection independently during sealing and offline
// verification.
func finalPublicNativePayoutAuditForEvidence(evidence *FinalSemanticEvidence) (FinalPublicNativePayoutAudit, error) {
	if evidence == nil || evidence.Window.EpochCount == 0 {
		return FinalPublicNativePayoutAudit{}, errors.New("public native payout audit has no acceptance epochs")
	}
	projection := finalPublicNativePayoutProjection{Schema: finalPublicNativePayoutAuditSchema, Netuid: evidence.Netuid, Epochs: make([]finalPublicNativePayoutEpochProjection, 0, evidence.Window.EpochCount)}
	var rowCount uint64
	for epoch := evidence.Window.FirstEpoch; epoch < evidence.Window.FirstEpoch+evidence.Window.EpochCount; epoch++ {
		reveal, subnetEpoch, err := finalNativeEpochReveal(evidence, epoch)
		if err != nil {
			return FinalPublicNativePayoutAudit{}, err
		}
		rows, err := finalNativeEpochRewardRows(evidence, epoch)
		if err != nil {
			return FinalPublicNativePayoutAudit{}, err
		}
		if uint64(len(rows)) > ^uint64(0)-rowCount {
			return FinalPublicNativePayoutAudit{}, errors.New("public native payout row count overflows uint64")
		}
		rowCount += uint64(len(rows))
		item := finalPublicNativePayoutEpochProjection{SettlementEpoch: epoch, SubnetEpoch: subnetEpoch, Reveal: reveal, Rewards: make([]finalPublicNativePayoutRewardProjection, 0, len(rows))}
		for _, row := range rows {
			item.Rewards = append(item.Rewards, finalPublicNativePayoutRewardProjection{
				Role: row.Role, SubjectID: row.SubjectID, UID: row.UID, Hotkey: row.Hotkey,
				CombinedEmissionRao: row.AfterRao, IncentiveU16: row.AfterIncentiveU16, DividendsU16: row.AfterDividendsU16,
				Expected: row.Expected, Before: row.Before, After: row.After,
			})
		}
		projection.Epochs = append(projection.Epochs, item)
	}
	if rowCount == 0 {
		return FinalPublicNativePayoutAudit{}, errors.New("public native payout audit has no UID rows")
	}
	hash, err := canonicalHashHex(projection)
	if err != nil {
		return FinalPublicNativePayoutAudit{}, err
	}
	return FinalPublicNativePayoutAudit{Schema: finalPublicNativePayoutAuditSchema, Epochs: evidence.Window.EpochCount, UIDRows: rowCount, ParentTransitions: evidence.Window.EpochCount, ProjectionHash: hash}, nil
}

// Rejects structurally incomplete or internally inconsistent summaries before
// evidence-specific comparison.
func verifyFinalPublicNativePayoutAuditShape(audit FinalPublicNativePayoutAudit) error {
	if audit.Schema != finalPublicNativePayoutAuditSchema || audit.Epochs == 0 || audit.UIDRows == 0 || audit.ParentTransitions != audit.Epochs {
		return errors.New("public native payout audit summary is incomplete")
	}
	return requireFinalHex32("public native payout projection hash", audit.ProjectionHash)
}

// Rejects missing, stale, truncated, or substituted payout replay scope during
// offline evidence verification.
func verifyFinalPublicNativePayoutAudit(evidence *FinalSemanticEvidence, got FinalPublicNativePayoutAudit) error {
	if err := verifyFinalPublicNativePayoutAuditShape(got); err != nil {
		return err
	}
	want, err := finalPublicNativePayoutAuditForEvidence(evidence)
	if err != nil {
		return fmt.Errorf("public native payout projection: %w", err)
	}
	if got != want {
		return errors.New("public native payout audit summary differs from sealed projection")
	}
	return nil
}

// Ensures the public transcript retains one structurally valid decoded
// observation per claimed payout boundary. Exact evidence identity is checked
// separately because this helper is also used while hashing the generic
// transcript.
func verifyFinalPublicNativePayoutObservationShape(audit FinalPublicNativePayoutAudit, states []FinalNativeEpochPayoutState) error {
	if uint64(len(states)) != audit.Epochs {
		return fmt.Errorf("public native payout observations=%d, want %d", len(states), audit.Epochs)
	}
	var rows uint64
	for index, state := range states {
		if index > 0 && state.SettlementEpoch <= states[index-1].SettlementEpoch {
			return errors.New("public native payout observations are not canonical")
		}
		if err := verifyFinalHead("public native payout parent", state.Parent); err != nil {
			return err
		}
		if err := verifyFinalHead("public native payout block", state.Block); err != nil {
			return err
		}
		if state.Parent.Number == ^uint64(0) || state.Parent.Number+1 != state.Block.Number || len(state.UIDs) == 0 {
			return fmt.Errorf("public native payout epoch %d has an incomplete parent transition", state.SettlementEpoch)
		}
		if uint64(len(state.UIDs)) > ^uint64(0)-rows {
			return errors.New("public native payout observation row count overflows uint64")
		}
		rows += uint64(len(state.UIDs))
	}
	if rows != audit.UIDRows || uint64(len(states)) != audit.ParentTransitions {
		return errors.New("public native payout observation counts differ from audit")
	}
	return nil
}

// Binds every decoded parent/reveal value retained in the v5 public transcript
// to the signed semantic evidence. This makes exact event, storage, and
// stake-transition values independently inspectable without trusting the
// producer's transient in-memory replay.
func verifyFinalPublicNativePayoutStates(evidence *FinalSemanticEvidence, audit FinalPublicNativePayoutAudit, states []FinalNativeEpochPayoutState) error {
	if err := verifyFinalPublicNativePayoutObservationShape(audit, states); err != nil {
		return err
	}
	for index, state := range states {
		wantEpoch := evidence.Window.FirstEpoch + uint64(index)
		if state.SettlementEpoch != wantEpoch {
			return fmt.Errorf("public native payout observation %d has epoch %d, want %d", index, state.SettlementEpoch, wantEpoch)
		}
		if err := verifyFinalNativeEpochPayout(evidence, state); err != nil {
			return fmt.Errorf("public native payout observation epoch %d: %w", state.SettlementEpoch, err)
		}
	}
	return nil
}

// Captures one public-chain payout observation at an epoch boundary. Combined
// emission and score channels come from pinned Subtensor storage; server
// emission comes from the runtime epoch event.
type FinalNativeEpochPayoutUIDState struct {
	UID                 uint16 `json:"uid"`
	Hotkey              string `json:"hotkey_public_key"`
	CombinedEmissionRao string `json:"combined_emission_rao"`
	ServerEmissionRao   string `json:"server_emission_rao"`
	StakeBeforeRao      string `json:"stake_before_rao"`
	StakeAfterRao       string `json:"stake_after_rao"`
	IncentiveU16        uint16 `json:"incentive_u16"`
	DividendsU16        uint16 `json:"dividends_u16"`
}

// Binds all managed payout subjects to the exact automatic reveal/epoch block.
// ManualStakeMutations counts only stake events touching one of these hotkeys
// on this netuid.
type FinalNativeEpochPayoutState struct {
	SettlementEpoch      uint64                           `json:"settlement_epoch"`
	SubnetEpoch          uint64                           `json:"subnet_epoch"`
	Netuid               uint16                           `json:"netuid"`
	Parent               ChainHead                        `json:"parent"`
	Block                ChainHead                        `json:"block"`
	ManualStakeMutations uint64                           `json:"manual_stake_mutations"`
	UIDs                 []FinalNativeEpochPayoutUIDState `json:"uids"`
}

// Returns the one reveal boundary shared by every validator for a settlement
// epoch. Runtime v453 reveals matured CRv4 weights
// before running coinbase in the same on_initialize hook, so this is the exact
// block whose epoch event and stake mutations prove payout causality. The
// later Application block is only when a validator poll first observed the
// already-applied storage row and is not a runtime boundary. Differing reveal
// boundaries make
// a single payout attribution ambiguous and therefore fail closed.
func finalNativeEpochReveal(evidence *FinalSemanticEvidence, epoch uint64) (ChainHead, uint64, error) {
	if evidence == nil || len(evidence.Validators) == 0 {
		return ChainHead{}, 0, errors.New("native epoch payout has no validator evidence")
	}
	var reveal ChainHead
	var subnetEpoch uint64
	for _, validator := range evidence.Validators {
		matches := 0
		for _, cycle := range validator.Cycles {
			if cycle.SettlementEpoch != epoch {
				continue
			}
			matches++
			if reveal == (ChainHead{}) {
				reveal = cycle.Reveal.Block
				subnetEpoch = cycle.SubnetEpoch
			} else if cycle.Reveal.Block != reveal || cycle.SubnetEpoch != subnetEpoch {
				return ChainHead{}, 0, fmt.Errorf("native payout epoch %d has divergent validator reveal boundaries", epoch)
			}
		}
		if matches != 1 {
			return ChainHead{}, 0, fmt.Errorf("validator %d has %d native payout cycles for epoch %d", validator.ValidatorID, matches, epoch)
		}
	}
	if subnetEpoch == 0 {
		return ChainHead{}, 0, fmt.Errorf("native payout epoch %d has zero subnet epoch", epoch)
	}
	if err := verifyFinalHead("native payout reveal", reveal); err != nil {
		return ChainHead{}, 0, err
	}
	return reveal, subnetEpoch, nil
}

// Proves the runtime channels and exact one-block stake effect for every
// managed role. The public reader is responsible for
// authenticating the parent hash and deriving ManualStakeMutations from the
// complete pinned System.Events value.
func verifyFinalNativeEpochPayout(evidence *FinalSemanticEvidence, state FinalNativeEpochPayoutState) error {
	if evidence == nil || state.Netuid != evidence.Netuid || state.SettlementEpoch < evidence.Window.FirstEpoch || state.SettlementEpoch >= evidence.Window.FirstEpoch+evidence.Window.EpochCount {
		return errors.New("native epoch payout identity is outside the accepted window")
	}
	reveal, subnetEpoch, err := finalNativeEpochReveal(evidence, state.SettlementEpoch)
	if err != nil {
		return err
	}
	if state.Block != reveal || state.SubnetEpoch != subnetEpoch {
		return fmt.Errorf("native payout epoch %d does not use its exact reveal boundary", state.SettlementEpoch)
	}
	if err := verifyFinalHead("native payout parent", state.Parent); err != nil {
		return err
	}
	if state.Parent.Number == ^uint64(0) || state.Parent.Number+1 != state.Block.Number {
		return fmt.Errorf("native payout epoch %d parent is not immediately before reveal", state.SettlementEpoch)
	}
	if state.ManualStakeMutations != 0 {
		return fmt.Errorf("native payout epoch %d has %d relevant manual stake mutations", state.SettlementEpoch, state.ManualStakeMutations)
	}

	expected := make(map[uint16]FinalNativeRewardDelta)
	for _, reward := range evidence.NativeRewards {
		if reward.Epoch != state.SettlementEpoch {
			continue
		}
		if reward.Before.Number >= state.Block.Number || reward.After.Number < state.Block.Number {
			return fmt.Errorf("native reward %s/%d does not span epoch %d reveal", reward.Role, reward.SubjectID, state.SettlementEpoch)
		}
		if _, exists := expected[reward.UID]; exists {
			return fmt.Errorf("native payout epoch %d repeats managed UID %d", state.SettlementEpoch, reward.UID)
		}
		expected[reward.UID] = reward
	}
	if len(expected) == 0 || len(state.UIDs) != len(expected) {
		return fmt.Errorf("native payout epoch %d UID rows=%d, want %d", state.SettlementEpoch, len(state.UIDs), len(expected))
	}

	seen := make(map[uint16]bool, len(state.UIDs))
	for index, uidState := range state.UIDs {
		if index > 0 && uidState.UID <= state.UIDs[index-1].UID {
			return fmt.Errorf("native payout epoch %d UID rows are not canonical", state.SettlementEpoch)
		}
		reward, ok := expected[uidState.UID]
		if !ok || seen[uidState.UID] {
			return fmt.Errorf("native payout epoch %d has unexpected or duplicate UID %d", state.SettlementEpoch, uidState.UID)
		}
		seen[uidState.UID] = true
		if !strings.EqualFold(uidState.Hotkey, reward.Hotkey) || uidState.Hotkey != strings.ToLower(uidState.Hotkey) {
			return fmt.Errorf("native payout epoch %d UID %d hotkey differs from signed role identity", state.SettlementEpoch, uidState.UID)
		}
		combined, err := finalNonnegativeInteger("native combined emission", uidState.CombinedEmissionRao)
		if err != nil {
			return err
		}
		server, err := finalNonnegativeInteger("native server emission", uidState.ServerEmissionRao)
		if err != nil {
			return err
		}
		before, err := finalNonnegativeInteger("native epoch stake before", uidState.StakeBeforeRao)
		if err != nil {
			return err
		}
		after, err := finalNonnegativeInteger("native epoch stake after", uidState.StakeAfterRao)
		if err != nil {
			return err
		}
		if server.Cmp(combined) > 0 || combined.String() != reward.AfterRao || uidState.IncentiveU16 != reward.AfterIncentiveU16 || uidState.DividendsU16 != reward.AfterDividendsU16 {
			return fmt.Errorf("native payout epoch %d UID %d runtime channels differ from signed reward evidence", state.SettlementEpoch, uidState.UID)
		}
		stakeDelta := new(big.Int).Sub(after, before)
		positive := combined.Sign() > 0
		switch reward.Role {
		case "head", "pool":
			if uidState.DividendsU16 != 0 || server.Cmp(combined) != 0 || (positive != (uidState.IncentiveU16 > 0)) || stakeDelta.Cmp(server) != 0 {
				return fmt.Errorf("native payout epoch %d UID %d miner emission is not causally reflected in stake", state.SettlementEpoch, uidState.UID)
			}
		case "validator":
			if server.Sign() != 0 || uidState.IncentiveU16 != 0 || !positive || uidState.DividendsU16 == 0 || stakeDelta.Sign() <= 0 {
				return fmt.Errorf("native payout epoch %d UID %d validator dividend is not causally reflected in stake", state.SettlementEpoch, uidState.UID)
			}
		default:
			return fmt.Errorf("native payout epoch %d UID %d has unsupported role %q", state.SettlementEpoch, uidState.UID, reward.Role)
		}
		if reward.Expected == "positive" && !positive || reward.Expected == "zero" && positive {
			return fmt.Errorf("native payout epoch %d UID %d contradicts validator consensus", state.SettlementEpoch, uidState.UID)
		}
	}
	for uid := range expected {
		if !seen[uid] {
			return fmt.Errorf("native payout epoch %d omits managed UID %d", state.SettlementEpoch, uid)
		}
	}
	return nil
}
