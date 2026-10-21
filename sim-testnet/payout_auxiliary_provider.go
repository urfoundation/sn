package main

import "github.com/urfoundation/sn/v2026/payoutartifact"

// Validator consumers can contribute signed usage without becoming miners.
// Only their exact unpaid shape is outside the miner payout census. Callers
// still verify the complete signed artifact, totals, duplicate IDs and leaves.
func unpaidAuxiliaryPayoutProvider(provider payoutartifact.ProviderInput, operatorNetworks map[[16]byte]bool) bool {
	return provider.ClientID != ([16]byte{}) && provider.NetworkID != ([16]byte{}) && operatorNetworks[provider.NetworkID] &&
		provider.Coldkey == ([32]byte{}) && !provider.Eligible && !provider.HeadExcluded && provider.BindingGeneration == 0 &&
		provider.ExclusionReason == "missing_payout_wallet" && provider.Assignments == 0 && provider.Confirmations == 0 && provider.ReliabilityPPM == 0
}
