// Earning-wallet selection between a provider client's own consent chain and
// its network's chain. The server's settlement and the independent economic
// verifier both call SelectEarningWallet, so the two cannot diverge.
package protocol

import (
	"errors"
)

const EarningWalletModeProvider = "provider"
const EarningWalletModeNetwork = "network"

// The wallet that earns for one provider client at one epoch, and the consent
// that selected it.
type EarningWallet struct {
	// EarningWalletModeProvider or EarningWalletModeNetwork
	Mode      string
	ClientId  [16]byte
	NetworkId [16]byte
	Coldkey   [32]byte
	// the selected original consent and its generation in its chain
	OriginalHash [32]byte
	Generation   uint64
	// the chain head the roster pinned, and that head's generation
	HeadHash       [32]byte
	HeadGeneration uint64
}

// A provider consent, verified and through the earning gate, as an earning
// wallet.
func ProviderEarningWallet(mapping *VerifiedWalletMapping) *EarningWallet {
	if mapping == nil {
		return nil
	}
	return &EarningWallet{
		Mode:           EarningWalletModeProvider,
		ClientId:       mapping.Statement.ClientId,
		NetworkId:      mapping.Statement.NetworkId,
		Coldkey:        mapping.Statement.Coldkey,
		OriginalHash:   mapping.OriginalHash,
		Generation:     mapping.Statement.Generation,
		HeadHash:       mapping.HeadHash,
		HeadGeneration: mapping.Generation,
	}
}

// A network consent, verified and through the earning gate, as the earning
// wallet of one provider client of that network.
func NetworkEarningWallet(clientId [16]byte, mapping *VerifiedNetworkWalletMapping) *EarningWallet {
	if mapping == nil {
		return nil
	}
	return &EarningWallet{
		Mode:           EarningWalletModeNetwork,
		ClientId:       clientId,
		NetworkId:      mapping.Statement.NetworkId,
		Coldkey:        mapping.Statement.Coldkey,
		OriginalHash:   mapping.OriginalHash,
		Generation:     mapping.Statement.Generation,
		HeadHash:       mapping.HeadHash,
		HeadGeneration: mapping.Generation,
	}
}

// Applies the precedence to the provider chain's outcome. A provider consent
// effective at the epoch wins, so an independent signer keeps control of its
// client. A provider chain that is absent (ErrWalletMappingAbsent) or verified
// with no consent effective at the epoch (ErrWalletMappingNotEffective) falls
// back to the network chain, which is evaluated only then. Any other provider
// failure is returned unchanged: falling back on missing or contradictory
// evidence could pay another wallet. Neither chain effective is unavailable.
func SelectEarningWallet(provider *EarningWallet, providerErr error, network func() (*EarningWallet, error)) (*EarningWallet, error) {
	if providerErr == nil {
		if provider == nil || provider.Mode != EarningWalletModeProvider {
			return nil, ErrWalletMappingIntegrity
		}
		return provider, nil
	}
	if !errors.Is(providerErr, ErrWalletMappingAbsent) && !errors.Is(providerErr, ErrWalletMappingNotEffective) {
		return nil, providerErr
	}
	if network == nil {
		return nil, providerErr
	}
	wallet, err := network()
	if err != nil {
		return nil, err
	}
	if wallet == nil || wallet.Mode != EarningWalletModeNetwork {
		return nil, ErrWalletMappingIntegrity
	}
	return wallet, nil
}

// The absent-chain outcome, joined so that existing unavailable checks hold.
func WalletMappingAbsentError() error {
	return errors.Join(ErrWalletMappingUnavailable, ErrWalletMappingAbsent)
}
