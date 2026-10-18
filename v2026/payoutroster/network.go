// Network-enabled rosters pin each complete network chain once. Their review
// preview uses the same wallet precedence as settlement and independent replay.
package payoutroster

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A verified head remains retained even when its effective consent is unknown.
// The selection error distinguishes unavailable originals from no effective era.
type networkCandidate struct {
	mapping *protocol.VerifiedNetworkWalletMapping
	err     error
}

// A supplied network entry must carry its full original chain. Missing fetches
// cannot become reviewed absence; the caller must leave the request pending.
func networkHead(ctx context.Context, config Config, input Input, network NetworkWalletInput) (payoutartifact.WholeWorkNetworkWallet, networkCandidate, error) {
	head := payoutartifact.WholeWorkNetworkWallet{NetworkId: network.NetworkId}
	candidate := networkCandidate{}
	if network.NetworkId == ([16]byte{}) || len(network.WalletConsents) == 0 {
		return head, candidate, protocol.ErrWalletMappingUnavailable
	}
	if len(network.WalletConsents) > protocol.MaxWalletMappingHistory {
		return head, candidate, protocol.ErrWalletMappingCapacity
	}
	for _, original := range network.WalletConsents {
		if err := ctx.Err(); err != nil {
			return head, candidate, err
		}
		statement, err := protocol.DecodeNetworkWalletMappingStatement(original.Message)
		if err != nil || statement.NetworkId != network.NetworkId || statement.Prospective.Signer != config.ClientKeyRootSigner {
			return head, candidate, errors.Join(protocol.ErrWalletMappingIntegrity, err)
		}
	}
	last, hash, err := protocol.VerifyNetworkWalletMappingConsent(ctx, network.WalletConsents[len(network.WalletConsents)-1])
	if err != nil {
		return head, candidate, err
	}
	if last.Generation != uint64(len(network.WalletConsents)) {
		return head, candidate, protocol.ErrWalletMappingIntegrity
	}
	candidate.mapping, candidate.err = protocol.VerifyNetworkWalletMappingHistory(ctx, network.WalletConsents, protocol.NetworkWalletMappingHistoryExpectation{Domain: config.Domain, NetworkId: network.NetworkId, HeadHash: hash, Generation: last.Generation, Epoch: input.Epoch})
	if candidate.err != nil && !errors.Is(candidate.err, protocol.ErrWalletMappingNotEffective) {
		return head, candidate, candidate.err
	}
	if candidate.mapping != nil {
		candidate.err = protocol.VerifyProspectiveNetworkWalletMapping(ctx, candidate.mapping, config.ClientKeyRootSigner, input.Clock.Start.Number, input.Clock.StartTime.Unix())
		if candidate.err != nil && !errors.Is(candidate.err, protocol.ErrWalletMappingUnavailable) {
			return head, candidate, candidate.err
		}
	}
	head.WalletHeadHash, head.WalletGeneration = hex.EncodeToString(hash[:]), last.Generation
	return head, candidate, nil
}

// Empty network and hotkey input preserves v1 request and authority bytes, and
// network input alone preserves the v2 bytes. Otherwise each provider's
// outcome is derived without weakening any unavailable-own or
// unavailable-network gate.
func prepareNetworkWallets(ctx context.Context, config Config, input Input, authority *payoutartifact.WholeWorkAuthority) ([]WalletSelection, error) {
	if len(input.NetworkWallets) == 0 && len(input.HotkeyDelegations) == 0 {
		return nil, nil
	}
	if len(input.NetworkWallets) > payoutartifact.MaxWholeWorkOwners {
		return nil, payoutartifact.ErrClosedWorkCapacity
	}
	for _, provider := range input.Providers {
		if provider.WalletConsents == nil {
			return nil, errors.Join(protocol.ErrWalletMappingUnavailable, errors.New("network or hotkey fallback requires an explicit provider consent array; [] asserts reviewed absence"))
		}
	}
	providerHeadKVs := make(map[[16]byte]payoutartifact.WholeWorkExpectedProvider, len(authority.ExpectedProviders))
	providerNetworkIds := make(map[[16]byte]bool, len(authority.ExpectedProviders))
	for _, provider := range authority.ExpectedProviders {
		providerHeadKVs[provider.ClientId] = provider
		providerNetworkIds[provider.NetworkId] = true
	}
	networkKVs := make(map[[16]byte]networkCandidate, len(input.NetworkWallets))
	for _, network := range input.NetworkWallets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := networkKVs[network.NetworkId]; exists || !providerNetworkIds[network.NetworkId] {
			return nil, errors.Join(protocol.ErrWalletMappingIntegrity, errors.New("network roster entry is duplicate or has no expected provider"))
		}
		head, candidate, err := networkHead(ctx, config, input, network)
		if err != nil {
			return nil, fmt.Errorf("roster network consent: %w", err)
		}
		networkKVs[network.NetworkId] = candidate
		authority.NetworkWallets = append(authority.NetworkWallets, head)
	}
	sort.Slice(authority.NetworkWallets, func(i, j int) bool {
		return bytes.Compare(authority.NetworkWallets[i].NetworkId[:], authority.NetworkWallets[j].NetworkId[:]) < 0
	})
	if 0 < len(authority.NetworkWallets) {
		authority.Schema = payoutartifact.WholeWorkAuthorityNetworkWalletSchema
	}
	hotkeyCandidateNetworkIds, err := prepareHotkeyDelegations(ctx, config, input, authority, providerNetworkIds)
	if err != nil {
		return nil, err
	}
	selections := make([]WalletSelection, 0, len(input.Providers))
	for _, provider := range input.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var own *protocol.EarningWallet
		ownErr := protocol.WalletMappingAbsentError()
		if len(provider.WalletConsents) != 0 {
			head := providerHeadKVs[provider.ClientId]
			decoded, err := hex.DecodeString(head.WalletHeadHash)
			if err != nil || len(decoded) != 32 {
				return nil, protocol.ErrWalletMappingIntegrity
			}
			var mapping *protocol.VerifiedWalletMapping
			mapping, ownErr = protocol.VerifyWalletMappingHistory(ctx, provider.WalletConsents, protocol.WalletMappingHistoryExpectation{Domain: config.Domain, ClientId: provider.ClientId, HeadHash: [32]byte(decoded), Generation: head.WalletGeneration, Epoch: input.Epoch})
			if ownErr == nil {
				ownErr = protocol.VerifyProspectiveWalletMapping(ctx, mapping, config.ClientKeyRootSigner, input.Clock.Start.Number, input.Clock.StartTime.Unix())
				if ownErr == nil {
					own = protocol.ProviderEarningWallet(mapping)
				}
			}
		}
		// without hotkey input the selection is exactly SelectEarningWallet's
		var hotkey func() (*protocol.EarningWallet, error)
		if hotkeyCandidateNetworkIds != nil {
			hotkey = func() (*protocol.EarningWallet, error) {
				return hotkeyEarningWallet(ctx, config, input, hotkeyCandidateNetworkIds, provider.ClientId, provider.NetworkId)
			}
		}
		wallet, err := protocol.SelectEarningWalletWithHotkey(own, ownErr, func() (*protocol.EarningWallet, error) {
			network, exists := networkKVs[provider.NetworkId]
			if !exists {
				return nil, protocol.WalletMappingAbsentError()
			}
			if network.err != nil {
				return nil, network.err
			}
			return protocol.NetworkEarningWallet(provider.ClientId, network.mapping), nil
		}, hotkey)
		selection := WalletSelection{ClientId: provider.ClientId, NetworkId: provider.NetworkId, Wallet: wallet}
		if err != nil {
			if !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
				return nil, err
			}
			selection.Unavailable = true
		} else if wallet == nil || wallet.ClientId != provider.ClientId || wallet.NetworkId != provider.NetworkId {
			return nil, protocol.ErrWalletMappingIntegrity
		}
		selections = append(selections, selection)
	}
	sort.Slice(selections, func(i, j int) bool { return bytes.Compare(selections[i].ClientId[:], selections[j].ClientId[:]) < 0 })
	return selections, nil
}
