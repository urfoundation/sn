// Signed campaign checkpoints may retain an incomplete restoration independently
// of the completed apply/restore proofs. Decode those historical fields exactly
// while keeping unknown fields, impossible boundaries, and backward progress fatal.
package main

import "fmt"

// Older records omit retry metadata. A retained retry must belong to a supported
// process fault and begin no earlier than its scheduled or conditional boundary.
func validateScenarioCampaignFaultRestore(record ScenarioFaultRecord) error {
	if record.RestorePendingRounds == 0 {
		if record.RestoreStartedBlock != 0 || record.RestoreStartedBlockHash != "" {
			return fmt.Errorf("scenario campaign fault %q has a restore boundary without retry rounds", record.ID)
		}
		return nil
	}
	if !faultCompletionKind(record.Kind) || (record.Status != "active" && record.Status != "restored") {
		return fmt.Errorf("scenario campaign fault %q has foreign restore retry evidence", record.ID)
	}
	minimum := record.MinimumDurationBlocks
	if record.RestoreCondition == "" {
		if record.RestoreBlock < record.TriggerBlock {
			return fmt.Errorf("scenario campaign fault %q has an invalid restore schedule", record.ID)
		}
		minimum = record.RestoreBlock - record.TriggerBlock
	}
	boundary, ok := checkedAdd(record.AppliedBlock, minimum)
	if !ok || record.AppliedBlock == 0 || record.RestoreStartedBlock < boundary ||
		!validCanonicalHashHex(record.RestoreStartedBlockHash) ||
		record.Status == "active" && record.Error == "" ||
		record.Status == "restored" && record.RestoredBlock < record.RestoreStartedBlock {
		return fmt.Errorf("scenario campaign fault %q has malformed restore retry evidence", record.ID)
	}
	return nil
}
