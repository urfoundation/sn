// Hotkey-enabled rosters pin each complete delegation chain once. Their review
// preview resolves hotkey mode with the resolver that settlement and the
// independent verifier use, and only after the network consent falls back.
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

// A delegated network's retained evidence and, once resolved, its outcome. A
// resolved wallet differs between the network's providers only in its client.
type hotkeyCandidate struct {
	evidence protocol.HotkeyEarningWalletEvidence
	resolved bool
	wallet   *protocol.EarningWallet
	err      error
}

// A supplied delegation entry must carry its full original chain and, when a
// delegation is effective at the epoch, exactly the global chain it names.
// Missing fetches cannot become an unavailable selection; the caller must
// leave the request pending. A verified head remains retained even when no
// delegation is effective at the epoch.
func delegationHead(ctx context.Context, config Config, input Input, delegation HotkeyDelegationInput) (payoutartifact.WholeWorkHotkeyDelegation, protocol.HotkeyEarningWalletEvidence, error) {
	head := payoutartifact.WholeWorkHotkeyDelegation{NetworkId: delegation.NetworkId}
	if delegation.NetworkId == ([16]byte{}) || len(delegation.DelegationConsents) == 0 {
		return head, protocol.HotkeyEarningWalletEvidence{}, protocol.ErrWalletMappingUnavailable
	}
	if len(delegation.DelegationConsents) > protocol.MaxWalletMappingHistory || len(delegation.HotkeyConsents) > protocol.MaxWalletMappingHistory {
		return head, protocol.HotkeyEarningWalletEvidence{}, protocol.ErrWalletMappingCapacity
	}
	for _, original := range delegation.DelegationConsents {
		if err := ctx.Err(); err != nil {
			return head, protocol.HotkeyEarningWalletEvidence{}, err
		}
		statement, err := protocol.DecodeHotkeyNetworkDelegationStatement(original.Message)
		if err != nil || statement.NetworkId != delegation.NetworkId || statement.Prospective.Signer != config.ClientKeyRootSigner {
			return head, protocol.HotkeyEarningWalletEvidence{}, errors.Join(protocol.ErrWalletMappingIntegrity, err)
		}
	}
	last, hash, err := protocol.VerifyHotkeyNetworkDelegation(ctx, delegation.DelegationConsents[len(delegation.DelegationConsents)-1])
	if err != nil {
		return head, protocol.HotkeyEarningWalletEvidence{}, err
	}
	if last.Generation != uint64(len(delegation.DelegationConsents)) {
		return head, protocol.HotkeyEarningWalletEvidence{}, protocol.ErrWalletMappingIntegrity
	}
	expected := protocol.HotkeyNetworkDelegationHistoryExpectation{Domain: config.Domain, NetworkId: delegation.NetworkId, HeadHash: hash, Generation: last.Generation, Epoch: input.Epoch}
	selected, err := protocol.VerifyHotkeyNetworkDelegationHistory(ctx, delegation.DelegationConsents, expected)
	if err != nil && !errors.Is(err, protocol.ErrWalletMappingNotEffective) {
		return head, protocol.HotkeyEarningWalletEvidence{}, err
	}
	if selected == nil {
		if len(delegation.HotkeyConsents) != 0 {
			return head, protocol.HotkeyEarningWalletEvidence{}, errors.Join(protocol.ErrWalletMappingIntegrity, errors.New("no delegation effective at the epoch names a global hotkey consent"))
		}
	} else {
		consent, consentHash, err := protocol.VerifyHotkeyWalletMappingLineage(ctx, delegation.HotkeyConsents)
		if err != nil {
			return head, protocol.HotkeyEarningWalletEvidence{}, errors.Join(err, errors.New("the delegation effective at the epoch requires the global hotkey consent chain through the head it names"))
		}
		if consent.Subnet != config.Domain.HotkeySubnet() || consent.Hotkey != selected.Statement.Hotkey || consentHash != selected.Statement.ConsentHeadHash || consent.Generation != selected.Statement.ConsentGeneration {
			return head, protocol.HotkeyEarningWalletEvidence{}, errors.Join(protocol.ErrWalletMappingIntegrity, errors.New("the global hotkey consent chain differs from the head the effective delegation names"))
		}
	}
	head.DelegationHeadHash, head.DelegationGeneration = hex.EncodeToString(hash[:]), last.Generation
	return head, protocol.HotkeyEarningWalletEvidence{Delegations: delegation.DelegationConsents, DelegationExpected: expected, Consents: delegation.HotkeyConsents}, nil
}

// Derives every delegation head into the authority, which then has the v3
// schema, and returns each delegated network's candidate. A delegation needs an
// expected provider in its network, occurs once, and states its network's
// consent chain: supplied in network_wallets, or asserted absent after review,
// so a missing fetch cannot let hotkey mode displace a network consent.
func prepareHotkeyDelegations(ctx context.Context, config Config, input Input, authority *payoutartifact.WholeWorkAuthority, providerNetworkIds map[[16]byte]bool) (map[[16]byte]*hotkeyCandidate, error) {
	if len(input.HotkeyDelegations) == 0 {
		return nil, nil
	}
	if len(input.HotkeyDelegations) > payoutartifact.MaxWholeWorkOwners {
		return nil, payoutartifact.ErrClosedWorkCapacity
	}
	networkInputIds := make(map[[16]byte]bool, len(input.NetworkWallets))
	for _, network := range input.NetworkWallets {
		networkInputIds[network.NetworkId] = true
	}
	candidateNetworkIds := make(map[[16]byte]*hotkeyCandidate, len(input.HotkeyDelegations))
	for _, delegation := range input.HotkeyDelegations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := candidateNetworkIds[delegation.NetworkId]; exists || !providerNetworkIds[delegation.NetworkId] {
			return nil, errors.Join(protocol.ErrWalletMappingIntegrity, errors.New("hotkey delegation roster entry is duplicate or has no expected provider"))
		}
		if delegation.NetworkWalletAbsent && networkInputIds[delegation.NetworkId] {
			return nil, errors.Join(protocol.ErrWalletMappingIntegrity, errors.New("a supplied network consent chain contradicts its asserted absence"))
		}
		if !delegation.NetworkWalletAbsent && !networkInputIds[delegation.NetworkId] {
			return nil, errors.Join(protocol.ErrWalletMappingUnavailable, errors.New("hotkey fallback requires the network's consent chain in network_wallets; network_wallet_absent asserts reviewed absence"))
		}
		head, evidence, err := delegationHead(ctx, config, input, delegation)
		if err != nil {
			return nil, fmt.Errorf("roster hotkey delegation: %w", err)
		}
		candidateNetworkIds[delegation.NetworkId] = &hotkeyCandidate{evidence: evidence}
		authority.HotkeyDelegations = append(authority.HotkeyDelegations, head)
	}
	sort.Slice(authority.HotkeyDelegations, func(i, j int) bool {
		return bytes.Compare(authority.HotkeyDelegations[i].NetworkId[:], authority.HotkeyDelegations[j].NetworkId[:]) < 0
	})
	authority.Schema = payoutartifact.WholeWorkAuthorityHotkeyDelegationSchema
	return candidateNetworkIds, nil
}

// The provider's hotkey-mode wallet, resolving the network's evidence on first
// use. A network without a delegation is absent.
func hotkeyEarningWallet(ctx context.Context, config Config, input Input, candidateNetworkIds map[[16]byte]*hotkeyCandidate, clientId [16]byte, networkId [16]byte) (*protocol.EarningWallet, error) {
	candidate, exists := candidateNetworkIds[networkId]
	if !exists {
		return nil, protocol.WalletMappingAbsentError()
	}
	if !candidate.resolved {
		candidate.wallet, candidate.err = protocol.ResolveHotkeyEarningWallet(ctx, clientId, candidate.evidence, config.ClientKeyRootSigner, input.Clock.Start.Number, input.Clock.StartTime.Unix())
		candidate.resolved = true
	}
	if candidate.err != nil {
		return nil, candidate.err
	}
	owned := *candidate.wallet
	owned.ClientId = clientId
	return &owned, nil
}
