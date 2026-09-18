//go:build !linux && !darwin

package validator

// No pathname fallback may claim the retained native recovery contract.
func recoverAttemptSettlementEpochOwned(string, []AttemptSettlementParticipant, func(string) error, func(string) error) error {
	return errAttemptPrivateDirectoryPlatform
}
