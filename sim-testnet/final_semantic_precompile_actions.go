// Completed probe recovery contributes only its original signed actions and
// finalized receipt identities to historical capture and offline replay.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const finalPrecompileConformancePath = "public/precompile-conformance.json"

// Keeps original source documents distinct from the action map derived from
// them. Every consumer independently validates their signatures and journal.
type finalHistoricalJournalSources struct {
	relayRequests      map[evidenceRelayRequestKey][]byte
	precompileRecovery *finalPrecompileRecoverySource
}

// The signed completion authenticates the complete conformance hash, including
// embedded v1/v2 approvals. Separate public approval copies add no authority.
type finalPrecompileRecoverySource struct {
	evidence   PrecompileConformanceEvidence
	completion PrecompileRecoveryCompletion
}

// A prefix selects a required proof file; it never admits a journal action.
// An unsigned refusal alone cannot widen the historical Evm capture boundary.
func finalPrecompileRecoveryRequired(plans map[string]*SetupPlan, entries []JournalEntry) bool {
	for _, entry := range entries {
		if plans[entry.PlanHash] != nil && entry.Stage == StageFinalized && strings.HasPrefix(entry.ActionID, precompileRecoveryActionPrefix) {
			return true
		}
	}
	return false
}

// Reads only the two exact public paths selected by a finalized recovery.
// The reader may retain these original bytes in the closed lineage artifact.
func finalPrecompileRecoveryForJournal(current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, read func(string) ([]byte, error)) (*finalPrecompileRecoverySource, error) {
	if !finalPrecompileRecoveryRequired(plans, entries) {
		return nil, nil
	}
	if read == nil {
		return nil, errors.New("historical precompile recovery reader is absent")
	}
	result := &finalPrecompileRecoverySource{}
	for _, document := range []struct {
		path  string
		value any
	}{
		{path: finalPrecompileConformancePath, value: &result.evidence},
		{path: precompileRecoveryCompletionFilename, value: &result.completion},
	} {
		raw, err := read(document.path)
		if err != nil {
			return nil, fmt.Errorf("historical precompile recovery source %s: %w", document.path, err)
		}
		if len(raw) == 0 || len(raw) > maximumCampaignEvidenceRawFileBytes {
			return nil, fmt.Errorf("historical precompile recovery source %s exceeds its bounded size", document.path)
		}
		if err := decodeStrictJSONBytes(raw, document.value); err != nil {
			return nil, fmt.Errorf("decode historical precompile recovery source %s: %w", document.path, err)
		}
	}
	if _, err := finalPrecompileRecoveryActions(current, plans, entries, result); err != nil {
		return nil, err
	}
	return result, nil
}

// The live census uses bounded local reads. It cannot open a writer or infer
// approval from a mutable action map or an unrelated file in the directory.
func finalHistoricalJournalSourcesFromState(ctx context.Context, stateRoot string, current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry) (finalHistoricalJournalSources, error) {
	requests, err := evidenceRelayRequestsFromState(ctx, stateRoot, plans, entries)
	if err != nil {
		return finalHistoricalJournalSources{}, err
	}
	recovery, err := finalPrecompileRecoveryForJournal(current, plans, entries, func(name string) ([]byte, error) {
		return validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(stateRoot, name), maximumCampaignEvidenceRawFileBytes)
	})
	if err != nil {
		return finalHistoricalJournalSources{}, err
	}
	return finalHistoricalJournalSources{relayRequests: requests, precompileRecovery: recovery}, nil
}

// Archived replay selects the same exact public paths as live capture.
func finalHistoricalJournalSourcesFromFiles(current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, files map[string][]byte) (finalHistoricalJournalSources, error) {
	requests, err := finalRelayRequestsFromFiles(plans, entries, files)
	if err != nil {
		return finalHistoricalJournalSources{}, err
	}
	recovery, err := finalPrecompileRecoveryForJournal(current, plans, entries, func(name string) ([]byte, error) {
		raw, found := files[name]
		if !found {
			return nil, fmt.Errorf("closed historical source %s is absent", name)
		}
		return raw, nil
	})
	if err != nil {
		return finalHistoricalJournalSources{}, err
	}
	return finalHistoricalJournalSources{relayRequests: requests, precompileRecovery: recovery}, nil
}

// Both dynamic source families travel with the independently authenticated
// fleet lineage, so offline chronology never reads mutable state files.
func finalHistoricalJournalSourcesFromArtifact(evidence *FinalSemanticEvidence, current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, cache map[string][]byte) (finalHistoricalJournalSources, error) {
	if evidence == nil || evidence.FleetGeneration == nil {
		return finalHistoricalJournalSources{}, errors.New("historical journal fleet-lineage artifact is absent")
	}
	raw, found := cache[evidence.FleetGeneration.Artifact.URI]
	if !found {
		return finalHistoricalJournalSources{}, errors.New("historical journal fleet-lineage bytes are not loaded")
	}
	files, err := finalFleetGenerationArtifactFiles(evidence, raw)
	if err != nil {
		return finalHistoricalJournalSources{}, err
	}
	return finalHistoricalJournalSourcesFromFiles(current, plans, entries, files)
}

// Admission binds custody, finite signed authority, completed accounting,
// journal anchors and exactly one original finalization for each rebuilt step.
func finalPrecompileRecoveryActions(current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, recovery *finalPrecompileRecoverySource) (map[evidenceRelayRequestKey]Action, error) {
	actions := make(map[evidenceRelayRequestKey]Action)
	if recovery == nil {
		if finalPrecompileRecoveryRequired(plans, entries) {
			return nil, errors.New("historical precompile recovery source is absent")
		}
		return actions, nil
	}
	evidence := &recovery.evidence
	if current == nil || evidence.Recovery == nil {
		return nil, errors.New("historical precompile recovery has no approved owner")
	}
	authorization := &evidence.Recovery.Authorization
	source := plans[authorization.Request.PlanHash]
	if source == nil || source.PlanHash != authorization.Request.PlanHash || !current.allowedPlanHashes()[source.PlanHash] || source.ConfigHash != evidence.ConfigHash || source.PolicyHash != evidence.PolicyHash || source.DeploymentID != evidence.DeploymentID || source.ChainID != evidence.ChainID || source.GenesisHash != evidence.GenesisHash || source.Netuid != evidence.Netuid {
		return nil, errors.New("historical precompile recovery source plan differs from its signed identity")
	}
	probe := common.HexToAddress(evidence.ProbeAddress)
	probeColdkey := ss58Mirror(probe)
	if evidence.Schema != "urnetwork-precompile-conformance-v1" || !strings.EqualFold(evidence.ProbeColdkey, hexBytesValue(probeColdkey[:])) {
		return nil, errors.New("historical precompile recovery changed its conformance identity")
	}
	if err := validatePrecompileEvidenceCarryScope(current, source, probe); err != nil {
		return nil, err
	}
	if err := verifyPrecompileRecoveryCompletion(source, evidence, &recovery.completion); err != nil {
		return nil, err
	}
	if err := validatePrecompileRecoveryGasRevisionJournal(source, evidence, entries); err != nil {
		return nil, err
	}
	var budgetSequence, completionSequence uint64
	for _, entry := range entries {
		if entry.PlanHash != source.PlanHash || entry.DeploymentID != source.DeploymentID {
			continue
		}
		if entry.EntryHash == authorization.Request.Budget.JournalHash {
			if budgetSequence != 0 {
				return nil, errors.New("historical precompile recovery budget checkpoint is duplicated")
			}
			budgetSequence = entry.Sequence
		}
		if entry.EntryHash == recovery.completion.Record.JournalHash {
			if completionSequence != 0 {
				return nil, errors.New("historical precompile recovery completion checkpoint is duplicated")
			}
			completionSequence = entry.Sequence
		}
	}
	if budgetSequence == 0 || completionSequence == 0 || completionSequence <= budgetSequence {
		return nil, errors.New("historical precompile recovery lost its signed journal checkpoints")
	}
	for index, step := range evidence.Recovery.Steps {
		action, _, err := precompileRecoveryAction(authorization, index, step)
		if err != nil || !precompileRecoveryActionEqual(action, step.Action) {
			return nil, stateMismatchError(err, "historical precompile recovery step %d differs from its signed authority", index+1)
		}
		for _, planned := range source.Actions {
			if planned.ID == action.ID {
				return nil, errors.New("historical precompile recovery conflicts with a static plan action")
			}
		}
		transactionHash, blockNumber, blockHash := precompileRecoveryReceipt(step)
		matches := 0
		for _, entry := range entries {
			if entry.PlanHash != source.PlanHash || entry.ActionID != action.ID {
				continue
			}
			if entry.DeploymentID != source.DeploymentID || entry.IntentHash != action.IntentHash || entry.Sequence <= budgetSequence || entry.Sequence > completionSequence {
				return nil, fmt.Errorf("historical precompile recovery action %s differs from its signed scope", action.ID)
			}
			if entry.Stage != StageFinalized {
				continue
			}
			if entry.TransactionHash != transactionHash || entry.BlockNumber != blockNumber || entry.BlockHash != blockHash {
				return nil, fmt.Errorf("historical precompile recovery action %s differs from its signed finalization", action.ID)
			}
			matches++
		}
		if matches != 1 {
			return nil, fmt.Errorf("historical precompile recovery action %s has %d exact finalizations", action.ID, matches)
		}
		actions[evidenceRelayRequestKey{planHash: source.PlanHash, actionId: action.ID}] = action
	}
	for _, entry := range entries {
		if plans[entry.PlanHash] != nil && entry.Stage == StageFinalized && strings.HasPrefix(entry.ActionID, precompileRecoveryActionPrefix) {
			if _, found := actions[evidenceRelayRequestKey{planHash: entry.PlanHash, actionId: entry.ActionID}]; !found {
				return nil, errors.New("historical precompile recovery has an unapproved finalized step")
			}
		}
	}
	return actions, nil
}
