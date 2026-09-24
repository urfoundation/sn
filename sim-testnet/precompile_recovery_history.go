// Completed supplemental recovery stays owned by its original dual-signed
// approval. Later compatible plans may replay it, but gain no spending authority.
package main

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

func readPrecompileRecoveryHistoryPlan(cfg *ResolvedConfig, stateDir string, current *SetupPlan, entries []JournalEntry, evidence *PrecompileConformanceEvidence) (*SetupPlan, error) {
	if current == nil {
		return nil, errors.New("probe recovery history has no current approval")
	}
	if evidence == nil || evidence.Recovery == nil {
		return current, nil
	}
	authorization := &evidence.Recovery.Authorization
	if authorization.Request.PlanHash == current.PlanHash {
		return current, validatePrecompileRecoveryPlan(current, evidence, authorization)
	}
	if !current.allowedPlanHashes()[authorization.Request.PlanHash] || !precompileEvidenceComplete(evidence) {
		return nil, errors.New("probe recovery history requires a completed approved ancestor")
	}
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, authorization.Request.PlanHash)
	if err != nil {
		return nil, err
	}
	if err := validatePrecompileRecoveryPlan(source, evidence, authorization); err != nil {
		return nil, err
	}
	if err := validatePrecompileEvidenceCarry(cfg, current, source, common.HexToAddress(evidence.ProbeAddress), evidence); err != nil {
		return nil, err
	}
	if err := validatePrecompileRecoveryGasRevisionJournal(source, evidence, entries); err != nil {
		return nil, err
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, precompileRecoveryCompletionFilename, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	var completion PrecompileRecoveryCompletion
	if err := decodeStrictJSONBytes(raw, &completion); err != nil {
		return nil, err
	}
	if err := verifyPrecompileRecoveryCompletion(source, evidence, &completion); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.EntryHash == completion.Record.JournalHash && entry.DeploymentID == source.DeploymentID && source.allowedPlanHashes()[entry.PlanHash] {
			return source, nil
		}
	}
	return nil, errors.New("probe recovery completion lost its exact source journal checkpoint")
}
