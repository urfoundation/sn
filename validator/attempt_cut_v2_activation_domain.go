package validator

import "github.com/urfoundation/sn/protocol"

// Earlier activation consents bind each operator's identity and migration
// prefix, so their hashes are deliberately not a cross-operator invariant.
// This compares only common deployment fields. Callers must still compare each
// complete expected context, including its activation hash, before full replay.
func equalAttemptCutV2CommonDomain(left, right protocol.ValidatorEvidenceDomain) bool {
	return left.ChainID == right.ChainID && left.GenesisHash == right.GenesisHash && left.Netuid == right.Netuid &&
		left.Coordinator == right.Coordinator && left.SettlementVault == right.SettlementVault &&
		left.DeploymentIDHash == right.DeploymentIDHash && left.PolicyHash == right.PolicyHash && left.ActivationEpoch == right.ActivationEpoch
}
