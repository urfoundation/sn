// Endpoint verification spans multiple signed cleanup writes. It must bind the
// latest owner checkpoint and both observation cuts, rather than applying the
// adjacent-write rule directly from the original pending ledger to completion.
package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Legacy faults without terminal lifecycle cleanup keep their original path.
func scenarioHasLifecycleCleanup(faults []ScenarioFaultRecord) bool {
	for _, fault := range faults {
		if fault.LifecycleCleanup != nil {
			return true
		}
	}
	return false
}

// History must already have passed validateScenarioAttemptObservationHistory
// for this checkpoint's exact byte prefix. The envelope is verified again here
// because the unsigned result alone cannot grant completed cleanup authority.
func validateScenarioCampaignStartMarkerHistory(cfg *ResolvedConfig, result *ScenarioResult, name, owner string, startRaw, checkpointRaw []byte, history []*ScenarioObservation) error {
	if result == nil || !result.Provisional || result.Result != "fail" || result.FinalAcceptance == nil || *result.FinalAcceptance || name != "release-1.0" {
		return validateScenarioCampaignStartMarkerBytes(cfg, result, name, owner, startRaw)
	}
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(checkpointRaw, &envelope); err != nil {
		return fmt.Errorf("scenario cleanup runtime checkpoint: %w", err)
	}
	if err := verifyEvidence(&envelope, nil); err != nil || !strings.EqualFold(envelope.Signer.Hex(), owner) {
		return stateMismatchError(err, "scenario cleanup runtime checkpoint signature or owner differs")
	}
	if envelope.Kind != scenarioCampaignAttemptEvidenceKind || envelope.RunID != result.RunID || envelope.DeploymentID != result.DeploymentID || envelope.ChainID != result.ChainID || envelope.Netuid != result.Netuid || !strings.EqualFold(envelope.GenesisHash, result.GenesisHash) {
		return errors.New("scenario cleanup runtime checkpoint belongs to another run or deployment")
	}
	var payload scenarioCampaignAttemptPayload
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return err
	}
	if err := validateScenarioCampaignAttemptPayload(cfg, payload.PlanHash, name, &payload); err != nil {
		return err
	}
	if payload.RunID != result.RunID || payload.AcceptanceBoundary == nil {
		return errors.New("scenario cleanup runtime checkpoint has no exact run boundary")
	}
	return validateScenarioCampaignStartMarkerWithProgress(cfg, result, name, owner, startRaw, func(startPayload *scenarioCampaignAttemptPayload, faults []ScenarioFaultRecord) error {
		start := startPayload.AcceptanceBoundary
		initialIdentity, finalIdentity := *startPayload, payload
		initialIdentity.AcceptanceBoundary, finalIdentity.AcceptanceBoundary = nil, nil
		initialIdentity.AcceptanceInvalidation, finalIdentity.AcceptanceInvalidation = "", ""
		initialIdentity.AcceptanceInvalidatedAt, finalIdentity.AcceptanceInvalidatedAt = "", ""
		if !reflect.DeepEqual(initialIdentity, finalIdentity) || !scenarioCampaignRecoveryStaticBoundaryMatches(start, payload.AcceptanceBoundary) {
			return errors.New("scenario cleanup checkpoint replaced its signed start")
		}
		if err := validateScenarioAuthenticatedFaultHistory(cfg, result, start.Faults, payload.AcceptanceBoundary, history); err != nil {
			return err
		}
		return validateScenarioFaultProgress(payload.AcceptanceBoundary.Faults, faults)
	})
}

// Only callers holding authenticated endpoint envelopes and the exact signed
// observation prefix may span intermediate writes. The ordinary writer still
// uses the strict adjacent validator before every cleanup side effect.
func validateScenarioAuthenticatedFaultHistory(cfg *ResolvedConfig, result *ScenarioResult, before []ScenarioFaultRecord, checkpoint *scenarioCampaignAcceptanceBoundary, history []*ScenarioObservation) error {
	if checkpoint == nil || result == nil || len(history) == 0 || !scenarioAcceptanceWindowsEqual(result.AcceptanceWindow, &checkpoint.AcceptanceWindow) {
		return errors.New("scenario cleanup cumulative history is incomplete")
	}
	if err := validateScenarioFaultProgressWithMode(before, checkpoint.Faults, false); err != nil {
		return err
	}
	for _, observation := range history {
		head, epoch, _, err := scenarioObservationIdentity(observation)
		if err != nil || head.Number > checkpoint.LastObservationHead.Number || epoch > checkpoint.LastObservationEpoch {
			return stateMismatchError(err, "scenario cleanup history exceeds its signed checkpoint")
		}
	}
	last := history[len(history)-1]
	if last.ObservationHash != checkpoint.LastObservationHash || last.Status.Contracts.FinalizedHead != checkpoint.LastObservationHead || last.Status.Contracts.CurrentEpoch != checkpoint.LastObservationEpoch {
		return errors.New("scenario cleanup history does not end at its signed checkpoint")
	}
	// The result's handoff is bound by the signed cleanup hash and by the exact
	// canonical lifecycle bytes in both request and completion observations.
	signed := *result
	signed.Faults = checkpoint.Faults
	if err := validateProvisionalLifecycleCleanupHistory(cfg, &signed, history); err != nil {
		return err
	}
	return nil
}
