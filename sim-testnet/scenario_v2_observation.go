//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

const scenarioNativeSourceScopeV2 = "signed-source-and-canonical-native-receipts"

// Strict runtime observation uses the exact approved rendered config and
// adopted namespace. Original V2 sources and native receipts are observed here;
// complete archive/math/public replay remains the independent final gate.
func inspectValidatorIntentV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, validatorID int) ValidatorObservation {
	failure := func(err error) ValidatorObservation {
		return ValidatorObservation{ValidatorID: validatorID, Error: err.Error()}
	}
	if ctx == nil || cfg == nil || !finalUsesEvidenceV2(cfg) || validatorID < 1 || validatorID > cfg.Config.Topology.Validators {
		return failure(errors.New("V2 scenario validator owner is invalid"))
	}
	release, _, adoptionRaw, err := finalReleaseCaptureConfigWithAdoptionV2(ctx, cfg, stateRoot, uint64(validatorID))
	if err != nil {
		return failure(err)
	}
	var adoption *validatorpkg.ReleaseHistoryAdoptionV2
	if len(adoptionRaw) != 0 {
		adoption, err = validatorpkg.DecodeReleaseHistoryAdoptionV2(adoptionRaw, bytesSHA256(adoptionRaw))
		if err != nil {
			return failure(err)
		}
	}
	authority, err := loadFinalOperatorPathAuthority(cfg, stateRoot, []uint64{uint64(validatorID)})
	if err != nil {
		return failure(err)
	}
	hotkey, err := decodeHex32("V2 scenario original validator hotkey", authority.identities.Substrate[validatorHotkeyLabel(validatorID)].PublicKey)
	if err != nil {
		return failure(err)
	}
	native, err := crv4.DialChainContext(ctx, cfg.OperationalSubstrate)
	if err != nil {
		return failure(err)
	}
	defer native.API.Client.Close()
	source, err := validatorpkg.ObserveReleaseNativeSourcesV2(ctx, release, native, hotkey, adoption)
	if err != nil {
		return failure(err)
	}
	result, err := projectScenarioNativeSourcesV2(source, validatorID, cfg.Config.Topology.HeadSlots, cfg.Config.Topology.fleetCandidates())
	if err != nil {
		return failure(err)
	}
	return result
}

// Project source observations without manufacturing a Verified decision or
// loading legacy intent files. Any incomplete source makes the whole result
// unavailable, including its progress counters.
func projectScenarioNativeSourcesV2(source *validatorpkg.ReleaseNativeSourceObservationV2, validatorID, headSlots, candidates int) (ValidatorObservation, error) {
	result := ValidatorObservation{ValidatorID: validatorID}
	if source == nil || !validSHA256ContentHash(source.StoreSHA256) {
		return result, errors.New("V2 scenario has no exact original native source")
	}
	result.NativeSourceScopeV2, result.NativeSourceStoreSHA256 = scenarioNativeSourceScopeV2, source.StoreSHA256
	all := make([]validatorpkg.SteeringIntent, 0, len(source.References))
	var latest *validatorpkg.SteeringIntent
	for _, reference := range source.References {
		intent, lifecycle := reference.Intent, reference.Lifecycle
		if intent.ValidatorID != uint64(validatorID) || reference.Artifact == nil || intent.MeasurementArtifactHash != lifecycle.MeasurementHash {
			return ValidatorObservation{}, errors.New("V2 scenario native source census differs")
		}
		all = append(all, intent)
		result.CurrentStatus, result.CurrentEpoch, result.VectorHash = intent.Status, intent.SubnetEpoch, intent.VectorHash
		result.IntentHashes = append(result.IntentHashes, intent.VectorHash)
		if intent.Status == "finalized" || intent.Status == "applied" {
			result.FinalizedIntents++
		}
		if intent.FinalizedBlock != 0 {
			if lifecycle.CommitNativeEpoch == 0 {
				return ValidatorObservation{}, errors.New("V2 scenario lacks its canonical native commit epoch")
			}
			result.NativeCommitsV2 = append(result.NativeCommitsV2, FinalNativeCoverageCommitV2{MeasurementHash: intent.MeasurementArtifactHash, Block: ChainHead{Number: intent.FinalizedBlock, Hash: strings.ToLower(intent.FinalizedBlockHash)}, NativeEpoch: lifecycle.CommitNativeEpoch})
		}
		if intent.Status != "applied" {
			continue
		}
		if intent.ApplicationBlock == 0 || lifecycle.RevealNativeEpoch == 0 || lifecycle.ApplicationNativeEpoch < lifecycle.RevealNativeEpoch || len(intent.UIDs) != len(intent.Values) || len(intent.UIDs) != len(intent.Scores) {
			return ValidatorObservation{}, errors.New("V2 scenario has an incomplete applied native observation")
		}
		result.AppliedIntents++
		decision := HeadDecisionObservation{VectorHash: intent.VectorHash, ExtrinsicHash: intent.ExtrinsicHash, SettlementEpoch: intent.SettlementEpoch,
			NativeSnapshot: ChainHead{Number: intent.NativeSnapshotBlock, Hash: strings.ToLower(intent.NativeSnapshotHash)}, EVMSnapshot: ChainHead{Number: intent.EVMSnapshotBlock, Hash: strings.ToLower(intent.EVMSnapshotHash)},
			FinalizedBlock: intent.FinalizedBlock, FinalizedBlockHash: intent.FinalizedBlockHash, RevealBlock: intent.RevealBlock, SubnetEpoch: intent.SubnetEpoch, ApplicationBlock: intent.ApplicationBlock, ApplicationBlockHash: intent.ApplicationBlockHash,
			CommitNativeEpoch: lifecycle.CommitNativeEpoch, RevealNativeEpoch: lifecycle.RevealNativeEpoch, ApplicationNativeEpoch: lifecycle.ApplicationNativeEpoch, MeasurementArtifactHash: intent.MeasurementArtifactHash,
			MaskedUIDs: append([]uint16(nil), intent.MaskedUIDs...), EligibleHeadUIDs: append([]uint16(nil), intent.EligibleHeadUIDs...), EligibleHeadScores: append([]validatorpkg.RationalJSON(nil), intent.EligibleHeadScores...),
			SelectedHeadUIDs: append([]uint16(nil), intent.SelectedHeadUIDs...), RejectedHeadUIDs: append([]uint16(nil), intent.RejectedHeadUIDs...), StaleHeadBindings: len(intent.StaleHeadBindings)}
		var err error
		decision.CandidateFleetUIDs, decision.CandidateFleetHotkeys, err = headDecisionCandidateIdentities(reference.Artifact, intent.EligibleHeadUIDs)
		if err != nil {
			return ValidatorObservation{}, err
		}
		for index, uid := range intent.UIDs {
			decision.AppliedWeights = append(decision.AppliedWeights, IntentWeightObservation{UID: uid, Numerator: intent.Scores[index].Numerator, Denominator: intent.Scores[index].Denominator, Value: intent.Values[index]})
		}
		result.HeadDecisions = append(result.HeadDecisions, decision)
		latest = &all[len(all)-1]
		values, err := json.Marshal(intent.Values)
		if err != nil {
			return ValidatorObservation{}, err
		}
		result.ValuesHash = bytesSHA256(values)
	}
	if latest != nil {
		result.SelfUID, result.StaleHeadBindings = latest.SelfUID, len(latest.StaleHeadBindings)
		result.MaskedUIDs = append([]uint16(nil), latest.MaskedUIDs...)
		result.EligibleHeadUIDs = append([]uint16(nil), latest.EligibleHeadUIDs...)
		result.EligibleHeadScores = append([]validatorpkg.RationalJSON(nil), latest.EligibleHeadScores...)
		result.SelectedHeadUIDs = append([]uint16(nil), latest.SelectedHeadUIDs...)
		result.RejectedHeadUIDs = append([]uint16(nil), latest.RejectedHeadUIDs...)
		result.DepositAudits = append([]validatorpkg.DepositAudit(nil), latest.DepositAudits...)
		result.AppliedWeights = append([]IntentWeightObservation(nil), result.HeadDecisions[len(result.HeadDecisions)-1].AppliedWeights...)
	}
	history := summarizeHeadSelectionHistory(all, headSlots, candidates)
	result.HeadDecisionEpochs, result.HeadTransitions = history.DecisionEpochs, history.Transitions
	result.PromotedHeadUIDs, result.DemotedHeadUIDs = history.Promoted, history.Demoted
	sort.Strings(result.IntentHashes)
	return result, nil
}
