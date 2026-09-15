package main

// Closed archives bind exact plan bytes, approved lineage and original release
// locks before rebuilding evidence. They do not authorize a live launch.
func decodeFinalHistoricalPlanBytes(data []byte) (*SetupPlan, error) {
	return decodePersistedPlanBytesForHistory(data, true)
}

func canonicalFinalHistoricalReleaseLockBytes(lock *ReleaseLock) ([]byte, error) {
	if err := validateValidatorEvidenceHistoricalReleaseLock(lock); err != nil {
		return nil, err
	}
	return canonicalValidatedReleaseLockBytes(lock)
}
