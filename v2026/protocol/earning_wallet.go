// Earning-wallet selection between a provider client's own consent chain, its
// network's chain and its network's hotkey delegation. The server's settlement
// and the independent economic verifier both call SelectEarningWallet, or
// ResolveHotkeyEarningWallet and SelectEarningWalletWithHotkey, so the two
// cannot diverge.
package protocol

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

const EarningWalletModeProvider = "provider"
const EarningWalletModeNetwork = "network"
const EarningWalletModeHotkey = "hotkey"

// The wallet that earns for one provider client at one epoch, and the consent
// that selected it.
type EarningWallet struct {
	// EarningWalletModeProvider, EarningWalletModeNetwork or EarningWalletModeHotkey
	Mode      string
	ClientId  [16]byte
	NetworkId [16]byte
	Coldkey   [32]byte
	// the selected original consent and its generation in its chain; in hotkey
	// mode the selected delegation
	OriginalHash [32]byte
	Generation   uint64
	// the chain head the roster pinned, and that head's generation
	HeadHash       [32]byte
	HeadGeneration uint64
	// Hotkey mode only: the delegated hotkey, the global consent selected at
	// the epoch with its generation, and the consent head the delegation pins
	// with that head's generation. Omitted from JSON otherwise, so the other
	// modes keep their exact encoding.
	Hotkey                [32]byte `json:",omitzero"`
	ConsentOriginalHash   [32]byte `json:",omitzero"`
	ConsentGeneration     uint64   `json:",omitzero"`
	ConsentHeadHash       [32]byte `json:",omitzero"`
	ConsentHeadGeneration uint64   `json:",omitzero"`
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

// A hotkey delegation and the global consent it pins, both verified and the
// delegation through its earning gate, as the earning wallet of one provider
// client of the delegated network. OriginalHash, Generation, HeadHash and
// HeadGeneration describe the delegation chain, the chain the roster pins.
func HotkeyEarningWallet(clientId [16]byte, delegation *VerifiedHotkeyNetworkDelegation, consent *VerifiedHotkeyWalletMapping) *EarningWallet {
	if delegation == nil || consent == nil {
		return nil
	}
	return &EarningWallet{
		Mode:                  EarningWalletModeHotkey,
		ClientId:              clientId,
		NetworkId:             delegation.Statement.NetworkId,
		Coldkey:               consent.Statement.Coldkey,
		OriginalHash:          delegation.OriginalHash,
		Generation:            delegation.Statement.Generation,
		HeadHash:              delegation.HeadHash,
		HeadGeneration:        delegation.Generation,
		Hotkey:                delegation.Statement.Hotkey,
		ConsentOriginalHash:   consent.OriginalHash,
		ConsentGeneration:     consent.Statement.Generation,
		ConsentHeadHash:       consent.HeadHash,
		ConsentHeadGeneration: consent.Generation,
	}
}

// The retained originals hotkey mode reads for one network. A zero
// DelegationExpected head and generation means the roster pins no delegation.
type HotkeyEarningWalletEvidence struct {
	Delegations        []WalletMappingConsent
	DelegationExpected HotkeyNetworkDelegationHistoryExpectation
	// the global chain from generation 1 through the delegation's consent head
	Consents []HotkeyWalletMappingConsent
}

// Delegation history, then the prospective gate, then the global chain through
// the delegation's ConsentHeadHash and ConsentGeneration, with the subnet of
// DelegationExpected.Domain, the delegation's hotkey and the same epoch. A
// network the roster pins no delegation for is WalletMappingAbsentError(), and
// originals supplied without a pinned delegation are integrity failures. Past
// that, each refusal is the failing step's own error.
func ResolveHotkeyEarningWallet(ctx context.Context, clientId [16]byte, evidence HotkeyEarningWalletEvidence, signer common.Address, startBlock uint64, startUnix int64) (*EarningWallet, error) {
	if ctx == nil {
		return nil, ErrWalletMappingUnavailable
	}
	if clientId == ([16]byte{}) {
		return nil, ErrWalletMappingIntegrity
	}
	expected := evidence.DelegationExpected
	if expected.HeadHash == ([32]byte{}) && expected.Generation == 0 {
		// originals without a pinned head are not evidence of anything
		if len(evidence.Delegations) != 0 || len(evidence.Consents) != 0 {
			return nil, ErrWalletMappingIntegrity
		}
		return nil, WalletMappingAbsentError()
	}
	delegation, err := VerifyHotkeyNetworkDelegationHistory(ctx, evidence.Delegations, expected)
	if err != nil {
		return nil, err
	}
	if err := VerifyProspectiveHotkeyNetworkDelegation(ctx, delegation, signer, startBlock, startUnix); err != nil {
		return nil, err
	}
	consent, err := VerifyHotkeyWalletMappingHistory(ctx, evidence.Consents, HotkeyWalletMappingHistoryExpectation{
		Subnet:     expected.Domain.HotkeySubnet(),
		Hotkey:     delegation.Statement.Hotkey,
		HeadHash:   delegation.Statement.ConsentHeadHash,
		Generation: delegation.Statement.ConsentGeneration,
		Epoch:      expected.Epoch,
	})
	if err != nil {
		return nil, err
	}
	return HotkeyEarningWallet(clientId, delegation, consent), nil
}

// Extends the precedence to provider > network > hotkey; the provider and
// network steps are SelectEarningWallet's. The hotkey delegation is evaluated
// only when the network chain itself falls back, absent
// (ErrWalletMappingAbsent) or with no consent effective at the epoch
// (ErrWalletMappingNotEffective). Any other outcome is returned unchanged, and
// the hotkey outcome is final. A nil network evaluates neither chain, as in
// SelectEarningWallet, and a nil hotkey behaves exactly like SelectEarningWallet.
func SelectEarningWalletWithHotkey(provider *EarningWallet, providerErr error, network func() (*EarningWallet, error), hotkey func() (*EarningWallet, error)) (*EarningWallet, error) {
	wallet, err := SelectEarningWallet(provider, providerErr, network)
	// with a network chain, a fallback outcome here can only be the network's
	if hotkey == nil || network == nil || err == nil || !errors.Is(err, ErrWalletMappingAbsent) && !errors.Is(err, ErrWalletMappingNotEffective) {
		return wallet, err
	}
	wallet, err = hotkey()
	if err != nil {
		return nil, err
	}
	if wallet == nil || wallet.Mode != EarningWalletModeHotkey {
		return nil, ErrWalletMappingIntegrity
	}
	return wallet, nil
}
