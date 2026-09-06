// Chronological lanes stop at a fully authenticated measurement. The ten
// independent envelope/native-intent completions own keys and staged outputs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// A lane transfers owned measurement/cycle inputs after the full public
// SealArtifact call. The decision is read-only; SealEnvelope still fully
// decodes and authenticates the original bytes independently.
type finalSemanticFixtureMeasurementJob struct {
	validatorID      uint64
	cycle            FinalCRv4Cycle
	measurementBytes []byte
	measurementHash  string
	verified         *validatorpkg.VerifiedReleaseMeasurement
	policyHash       string
	hotkeySeed       [32]byte
	expectedHotkey   [32]byte
}

// No output is visible until all ten owner jobs have joined successfully.
type finalSemanticFixtureMeasurementOutput struct {
	validatorID uint64
	cycle       FinalCRv4Cycle
	artifacts   finalSemanticFixtureArtifacts
}

// Preserve the original cycle clone's complete JSON ownership semantics.
func cloneFinalSemanticFixtureCycleResult(source FinalCRv4Cycle) (FinalCRv4Cycle, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return FinalCRv4Cycle{}, err
	}
	var result FinalCRv4Cycle
	if err := json.Unmarshal(encoded, &result); err != nil {
		return FinalCRv4Cycle{}, err
	}
	return result, nil
}

// This is the original exact weight/prepared-call, full envelope signing and
// native-intent body. Only its ownership and scheduling boundary are new.
func completeFinalSemanticFixtureMeasurement(job finalSemanticFixtureMeasurementJob, work finalSemanticFixtureWorkControl) (finalSemanticFixtureMeasurementOutput, error) {
	if work.ctx == nil || job.validatorID < 1 || job.validatorID > 2 || job.cycle.SettlementEpoch < 10 || job.cycle.SettlementEpoch > 14 || job.verified == nil || len(job.measurementBytes) == 0 || job.measurementHash != validatorpkg.ReleaseMeasurementContentHash(job.measurementBytes) {
		return finalSemanticFixtureMeasurementOutput{}, errors.New("fixture measurement completion identity or sealed bytes are invalid")
	}
	if err := work.ctx.Err(); err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	hotkey, err := crv4.KeypairFromSeed(job.hotkeySeed)
	if err != nil || hotkey.PublicKey() != job.expectedHotkey {
		return finalSemanticFixtureMeasurementOutput{}, fmt.Errorf("fixture measurement hotkey identity differs: %v", err)
	}
	cycle, err := cloneFinalSemanticFixtureCycleResult(job.cycle)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	validatorID, measurementBytes, measurementHash, verified, policyHash := job.validatorID, job.measurementBytes, job.measurementHash, job.verified, job.policyHash
	artifacts := finalSemanticFixtureArtifacts{}
	fleetByUID := make(map[uint16]uint64, len(cycle.Candidates))
	for _, candidate := range cycle.Candidates {
		if candidate.UID == 0 || fleetByUID[candidate.UID] != 0 {
			return finalSemanticFixtureMeasurementOutput{}, fmt.Errorf("fixture candidate UID %d is zero or duplicated", candidate.UID)
		}
		fleetByUID[candidate.UID] = candidate.FleetID
	}
	selected := make(map[uint16]bool, len(verified.SelectedHead))
	for _, head := range verified.SelectedHead {
		selected[head.UID] = true
	}
	cycle.Candidates = make([]FinalHeadCandidateEvidence, 0, len(verified.EligibleHead))
	for rank, head := range verified.EligibleHead {
		fleetID := fleetByUID[head.UID]
		if fleetID == 0 {
			return finalSemanticFixtureMeasurementOutput{}, fmt.Errorf("verified fixture candidate UID %d has no fleet", head.UID)
		}
		cycle.Candidates = append(cycle.Candidates, FinalHeadCandidateEvidence{
			FleetID: fleetID, Rank: uint16(rank + 1), UID: head.UID,
			RawScore: finalRationalFromBig(head.Score), Selected: selected[head.UID],
		})
	}
	valueUIDs := append([]uint16(nil), verified.UIDs...)
	scores := make([]*big.Rat, len(verified.Scores))
	for index, score := range verified.Scores {
		scores[index] = new(big.Rat).Set(score)
	}
	capped, err := crv4.ApplyMaxWeightLimitRational(scores, cycle.MaxWeightLimitU16)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	valueUIDs, values, err := crv4.NormalizeRationalToU16(valueUIDs, capped)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	if err := finalRepairMaxWeightLimitU16(valueUIDs, values, cycle.MaxWeightLimitU16); err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	valueByUID := map[uint16]uint16{}
	cycle.Submitted = nil
	cycle.RealizedHeadValue, cycle.RealizedPoolValue, cycle.RealizedTotalValue = 0, 0, 0
	for index, uid := range valueUIDs {
		valueByUID[uid] = values[index]
		cycle.Submitted = append(cycle.Submitted, FinalSubmittedWeight{UID: uid, Score: finalRationalFromBig(scores[index]), Value: values[index]})
	}
	for index := range cycle.Candidates {
		cycle.Candidates[index].AppliedWeight = valueByUID[cycle.Candidates[index].UID]
		if cycle.Candidates[index].Selected {
			cycle.RealizedHeadValue += uint64(cycle.Candidates[index].AppliedWeight)
		}
	}
	for index := range cycle.Pools {
		cycle.Pools[index].AppliedWeight = valueByUID[cycle.Pools[index].UID]
		cycle.RealizedPoolValue += uint64(cycle.Pools[index].AppliedWeight)
	}
	for _, submitted := range cycle.Submitted {
		cycle.RealizedTotalValue += uint64(submitted.Value)
	}
	encodedValues, err := json.Marshal(values)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	cycle.ValuesHash = bytesSHA256(encodedValues)
	eligibleUIDs := make([]uint16, len(cycle.Candidates))
	eligibleScores := make([]validatorpkg.RationalJSON, len(cycle.Candidates))
	for index, candidate := range cycle.Candidates {
		eligibleUIDs[index] = candidate.UID
		eligibleScores[index] = validatorpkg.RationalJSON{Numerator: candidate.RawScore.Numerator, Denominator: candidate.RawScore.Denominator}
	}
	selectedUIDs, rejectedUIDs := finalCandidateUIDs(cycle.Candidates)
	intentScores := make([]validatorpkg.RationalJSON, len(cycle.Submitted))
	for index, submitted := range cycle.Submitted {
		intentScores[index] = validatorpkg.RationalJSON{Numerator: submitted.Score.Numerator, Denominator: submitted.Score.Denominator}
	}
	cycle.MaskedUIDs = append([]uint16(nil), verified.MaskedUIDs...)
	cycle.MeasurementArtifact = artifacts.artifact("validator-release-measurement", fmt.Sprintf("validator-%d-measurement-%d.json", validatorID, cycle.SettlementEpoch), measurementBytes)
	prepared, err := finalTestPreparedSubmissionResult(valueUIDs, values, cycle, hotkey.PublicKey())
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	cycle.Commit.ExtrinsicHash = prepared.ExtrinsicHash
	leave, err := work.enterMeasurement(finalSemanticFixtureEnvelopeStart, validatorID, cycle.SettlementEpoch, 0)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	defer leave()
	work.observeMeasurement(finalSemanticFixtureEnvelopeStart, validatorID, cycle.SettlementEpoch, 0)
	envelopeBytes, envelopeHash, envelope, err := validatorpkg.SealReleaseMeasurementEnvelope(measurementBytes, uint16(10+2*validatorID), hotkey, prepared.ExtrinsicHash, time.Unix(1_700_000_000+int64(cycle.SettlementEpoch), 0).UTC())
	work.observeMeasurement(finalSemanticFixtureEnvelopeEnd, validatorID, cycle.SettlementEpoch, 0)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	if envelope == nil || envelope.ValidatorID != validatorID || envelope.SettlementEpoch != cycle.SettlementEpoch || envelope.SubnetEpoch != cycle.SubnetEpoch || envelope.NativeSnapshotBlock != cycle.NativeSnapshot.Number || envelope.NativeSnapshotHash != cycle.NativeSnapshot.Hash || envelope.EVMSnapshotBlock != cycle.EVMSnapshot.Number || envelope.EVMSnapshotHash != cycle.EVMSnapshot.Hash || envelope.PolicyHash != policyHash {
		return finalSemanticFixtureMeasurementOutput{}, errors.New("fixture completed envelope differs from its owned measurement context")
	}
	cycle.MeasurementEnvelope = artifacts.artifact("validator-release-measurement-envelope", fmt.Sprintf("validator-%d-measurement-envelope-%d.json", validatorID, cycle.SettlementEpoch), envelopeBytes)
	audits := make([]validatorpkg.DepositAudit, len(cycle.Pools))
	for i, pool := range cycle.Pools {
		audits[i] = finalDepositAuditFromPool(cycle.SettlementEpoch, &pool)
	}
	intent := validatorpkg.SteeringIntent{
		Schema: validatorpkg.SteeringIntentSchema, ValidatorID: validatorID, Netuid: 521,
		SubnetEpoch: cycle.SubnetEpoch, NativeSnapshotBlock: cycle.NativeSnapshot.Number, NativeSnapshotHash: cycle.NativeSnapshot.Hash,
		EVMSnapshotBlock: cycle.EVMSnapshot.Number, EVMSnapshotHash: cycle.EVMSnapshot.Hash, SettlementEpoch: cycle.SettlementEpoch,
		PolicyHash: policyHash, MeasurementArtifactPath: "measurements/" + strings.TrimPrefix(measurementHash, "sha256:") + ".json",
		MeasurementArtifactHash: measurementHash, MeasurementArtifactSize: uint64(len(measurementBytes)), SelfUID: uint16(10 + 2*validatorID),
		MeasurementEnvelopePath: "measurements/envelopes/" + strings.TrimPrefix(envelopeHash, "sha256:") + ".json", MeasurementEnvelopeHash: envelopeHash, MeasurementEnvelopeSize: uint64(len(envelopeBytes)),
		MaskedUIDs: cycle.MaskedUIDs, EligibleHeadUIDs: eligibleUIDs, EligibleHeadScores: eligibleScores,
		SelectedHeadUIDs: selectedUIDs, RejectedHeadUIDs: rejectedUIDs, DepositAudits: audits,
		UIDs: valueUIDs, Scores: intentScores, Prepared: prepared,
	}
	vectorHash, err := intent.ReconstructedVectorHash()
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	intent.VectorHash, intent.Status, intent.Values = vectorHash, "applied", values
	intent.ExtrinsicHash, intent.FinalizedBlock, intent.FinalizedBlockHash = cycle.Commit.ExtrinsicHash, cycle.Commit.Block.Number, cycle.Commit.Block.Hash
	intent.RevealBlock, intent.ApplicationBlock, intent.ApplicationBlockHash = cycle.Reveal.Block.Number, cycle.Application.Block.Number, cycle.Application.Block.Hash
	commitCall, err := finalNativeIntentCallEvidence(&intent, finalNativeOperationCommit)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	revealCall, err := finalNativeIntentCallEvidence(&intent, finalNativeOperationReveal)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	applicationCall, err := finalNativeIntentCallEvidence(&intent, finalNativeOperationApplication)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	cycle.Commit.Call, cycle.Reveal.Call, cycle.Application.Call = &commitCall, &revealCall, &applicationCall
	intentBytes, err := json.Marshal(intent)
	if err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	cycle.IntentVectorHash = vectorHash
	cycle.IntentArtifact = artifacts.artifact("steering-intent", fmt.Sprintf("steering-intent-%d-%d.json", validatorID, cycle.SettlementEpoch), intentBytes)
	if artifacts.err != nil {
		return finalSemanticFixtureMeasurementOutput{}, artifacts.err
	}
	if err := work.ctx.Err(); err != nil {
		return finalSemanticFixtureMeasurementOutput{}, err
	}
	return finalSemanticFixtureMeasurementOutput{validatorID: validatorID, cycle: cycle, artifacts: artifacts}, nil
}

// Sort a detached job list before the bounded fork. Missing or repeated
// validator/epoch slots fail before any signer or artifact owner is admitted.
func completeFinalSemanticFixtureMeasurements(jobs []finalSemanticFixtureMeasurementJob, work finalSemanticFixtureWorkControl) ([]finalSemanticFixtureMeasurementOutput, error) {
	if work.ctx == nil || len(jobs) != 10 {
		return nil, errors.New("fixture measurement completion requires all ten owners")
	}
	if err := work.ctx.Err(); err != nil {
		return nil, err
	}
	ordered := append([]finalSemanticFixtureMeasurementJob(nil), jobs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].validatorID != ordered[j].validatorID {
			return ordered[i].validatorID < ordered[j].validatorID
		}
		return ordered[i].cycle.SettlementEpoch < ordered[j].cycle.SettlementEpoch
	})
	for index, job := range ordered {
		if job.validatorID != uint64(index/5+1) || job.cycle.SettlementEpoch != uint64(index%5+10) {
			return nil, errors.New("fixture measurement completion owner census is missing or duplicated")
		}
	}
	outputs := make([]finalSemanticFixtureMeasurementOutput, len(ordered))
	envelopeWork := work
	envelopeWork.observer = nil
	if err := envelopeWork.run(finalSemanticFixtureEnvelopeStart, len(ordered), 4, func(ctx context.Context, index int) error {
		ownedWork := envelopeWork
		ownedWork.ctx = ctx
		output, err := completeFinalSemanticFixtureMeasurement(ordered[index], ownedWork)
		if err != nil {
			return err
		}
		outputs[index] = output
		return ctx.Err()
	}); err != nil {
		return nil, err
	}
	if err := work.ctx.Err(); err != nil {
		return nil, err
	}
	return outputs, nil
}
