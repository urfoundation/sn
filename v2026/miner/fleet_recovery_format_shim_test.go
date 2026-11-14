package miner

// The pre-tag form of a publish intent: no format, so its id is the id the
// record had before the fleet tag existed.
func fleetRecoveryTestLegacyIntent(intent fleetRecoveryIntent) fleetRecoveryIntent {
	intent.Format = ""
	return intent
}
