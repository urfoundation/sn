// Provider admission retains original portable evidence, never a caller's
// complete flag. The independently selected per-pool policy supplies authority;
// the existing receipt reader supplies exact original epoch and contract pins.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

const economicProviderMeasurementPolicySchema = "urnetwork-economic-provider-measurements-v1"

// The signer approves a prospective complete SDK/provider roster; the pinned
// attempt source supplies independent policy/registry/key history. Neither is
// selected from an artifact or from the observed current database directory.
type economicProviderMeasurementPolicy struct {
	Schema                   string                                   `json:"schema"`
	AttributionSigner        string                                   `json:"attribution_signer,omitempty"`
	WholeWorkAuthoritySigner string                                   `json:"whole_work_authority_signer"`
	EarningIdentity          *payoutartifact.WholeWorkEarningIdentity `json:"earning_identity,omitempty"`
	WalletEndpoint           string                                   `json:"wallet_endpoint"`
	AttemptAuthority         validator.ReleaseEvidenceV2File          `json:"attempt_authority"`
	MaximumOriginalBytes     uint64                                   `json:"maximum_original_bytes"`
}

// Each chain is independently selected by the signed expected-provider head.
// The complete raw history remains available after the API becomes unavailable.
type economicProviderWalletOriginal struct {
	ClientId  [16]byte                        `json:"client_id"`
	Originals []protocol.WalletMappingConsent `json:"originals"`
}

// A network consent chain selected by the roster's network head. It is
// retained only for networks with a provider that falls back to it.
type economicNetworkWalletOriginal struct {
	NetworkId [16]byte                        `json:"network_id"`
	Originals []protocol.WalletMappingConsent `json:"originals"`
}

// A hotkey delegation chain selected by the roster's delegation head. It is
// retained only for networks with a provider that falls back past the network
// consent to it.
type economicHotkeyDelegationOriginal struct {
	NetworkId [16]byte                        `json:"network_id"`
	Originals []protocol.WalletMappingConsent `json:"originals"`
}

// The global hotkey consent chain from generation 1 through the head that the
// network's delegation effective at the epoch names, retained beside that
// delegation chain.
type economicHotkeyConsentOriginal struct {
	NetworkId [16]byte                              `json:"network_id"`
	Originals []protocol.HotkeyWalletMappingConsent `json:"originals"`
}

// Portable originals are bounded by the original physical and logical profile.
// A compact projection cannot replace any one of these independent components.
// Wallets holds one entry per expected provider with a provider head, in
// roster order; NetworkWallets one per consulted network, ordered by network,
// and is omitted when empty so evidence without network consents is unchanged.
// HotkeyDelegations and HotkeyConsents hold one entry each per network that
// falls back to hotkey mode, ordered by network, and are omitted when empty in
// the same way.
type economicProviderOriginals struct {
	Work              *payoutartifact.WholeWorkInventory        `json:"work"`
	Wallets           []economicProviderWalletOriginal          `json:"wallets"`
	NetworkWallets    []economicNetworkWalletOriginal           `json:"network_wallets,omitempty"`
	HotkeyDelegations []economicHotkeyDelegationOriginal        `json:"hotkey_delegations,omitempty"`
	HotkeyConsents    []economicHotkeyConsentOriginal           `json:"hotkey_consents,omitempty"`
	Bindings          *validator.ProviderAttemptBindingOriginal `json:"bindings"`
	Attempts          json.RawMessage                           `json:"attempts"`
}

// Private cached results are reachable only through an admitted original
// census identity. Retaining only newly reconciled terminal contracts keeps
// historical index cost proportional to real originals rather than windows.
type economicProviderAdmitted struct {
	Domain        protocol.ClientKeyHistoryDomain
	Epoch         uint64
	InventoryHash string
	Measurement   economicConservationProviderMeasurement
	ClosedWork    economicConservationClosedWork
	NewContracts  map[[16]byte]payoutartifact.WholeWorkPriorContract
	NewCreations  map[[16]byte]payoutartifact.WholeWorkRetainedCreation
}

// The index is private to the held admission owner. An entry selects a retained
// census identity, never a proposed exclusion or a mutable publisher directory.
type economicProviderContractKey struct {
	Domain     protocol.ClientKeyHistoryDomain
	ContractId [16]byte
}

// Nil preserves exact legacy policy bytes and its unknown measurement result.
func (self *economicProviderMeasurementPolicy) validate(policy economicConservationPolicy) error {
	if self == nil {
		return nil
	}
	if self.AttributionSigner != "" && !monitorEvmAddress(self.AttributionSigner) {
		return errors.New("economic provider attribution authority is invalid")
	}
	if self.Schema != economicProviderMeasurementPolicySchema || !monitorEvmAddress(self.WholeWorkAuthoritySigner) || self.MaximumOriginalBytes == 0 || self.MaximumOriginalBytes > uint64(policy.storageMaximum()) {
		return errors.New("economic provider original authority or finite physical profile is incomplete")
	}
	if _, _, err := self.attemptReference(); err != nil {
		return err
	}
	if _, err := self.earningSelection(); err != nil {
		return err
	}
	if identity := self.EarningIdentity; identity != nil && (identity.ChainId != policy.Vault.Network.EvmChainId || identity.GenesisHash != policy.Vault.Network.GenesisHash || identity.Netuid != policy.Vault.Netuid) {
		return errors.New("economic earning identity differs from the original network policy")
	}
	_, err := validator.NewHttpWalletMappingReader(self.WalletEndpoint)
	return err
}

// Economic policy retains its canonical sha256: commitment. Only the adapter
// uses the validator reader's 0x representation; path, length and bytes are exact.
func (self *economicProviderMeasurementPolicy) attemptReference() (validator.ReleaseEvidenceV2File, [32]byte, error) {
	if self == nil || !planSha256(self.AttemptAuthority.SHA256) {
		return validator.ReleaseEvidenceV2File{}, [32]byte{}, errors.New("economic provider attempt authority digest is not canonical")
	}
	reference := self.AttemptAuthority
	if !filepath.IsAbs(reference.Path) || filepath.Clean(reference.Path) != reference.Path {
		return validator.ReleaseEvidenceV2File{}, [32]byte{}, errors.New("economic provider attempt authority path is not canonical")
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(reference.SHA256, "sha256:"))
	if err != nil || len(digest) != 32 {
		return validator.ReleaseEvidenceV2File{}, [32]byte{}, errors.New("economic provider attempt authority digest differs")
	}
	reference.SHA256 = "0x" + hex.EncodeToString(digest)
	if err := reference.Validate(16 * 1024 * 1024); err != nil {
		return validator.ReleaseEvidenceV2File{}, [32]byte{}, err
	}
	return reference, [32]byte(digest), nil
}

// Live acquisition and cold replay use the same exact protected reader seam.
func (self *economicProviderMeasurementPolicy) openAttemptSource(ctx context.Context) (*validator.ProviderAttemptAuthoritySource, error) {
	reference, _, err := self.attemptReference()
	if err != nil {
		return nil, err
	}
	return validator.NewProviderAttemptAuthoritySource(ctx, reference)
}

// This independently reviewed identity is stable across readiness revisions.
// The artifact's declaration never supplies a missing cutoff or policy digest.
func (self *economicProviderMeasurementPolicy) earningSelection() (*payoutartifact.WholeWorkEarningSelection, error) {
	if self == nil || self.EarningIdentity == nil {
		return nil, nil
	}
	identity := *self.EarningIdentity
	if identity.Schema != "urnetwork-provider-payout-transition-v1" || identity.CutoffUtc != "2026-10-06T00:00:00Z" || identity.Attribution != "settled_contract_close_time" || identity.LegacyUsdc != "finish_pre_cutoff_obligations" || identity.Profile != "mainnet" {
		return nil, errors.New("economic earning identity differs from the approved mainnet transition")
	}
	return payoutartifact.NewWholeWorkEarningSelection(identity)
}

// Live acquisition and cold admission derive every expectation from original
// policy or independently retained predecessors, never the supplied witness.
func (self *economicProviderMeasurementPolicy) workExpectation(artifact *payoutartifact.Artifact, rootSigner string, priors []payoutartifact.WholeWorkPriorContract) (payoutartifact.WholeWorkExpectation, error) {
	if self == nil || artifact == nil {
		return payoutartifact.WholeWorkExpectation{}, payoutartifact.ErrClosedWorkUnavailable
	}
	selection, err := self.earningSelection()
	if err != nil {
		return payoutartifact.WholeWorkExpectation{}, err
	}
	if identity := self.EarningIdentity; identity != nil && (identity.ChainId != artifact.ChainID || identity.GenesisHash != artifact.GenesisHash || identity.Netuid != artifact.Netuid) {
		return payoutartifact.WholeWorkExpectation{}, errors.Join(errRpcIntegrity, payoutartifact.ErrClosedWorkIntegrity)
	}
	return payoutartifact.WholeWorkExpectation{AttributionSigner: common.HexToAddress(self.AttributionSigner), AuthoritySigner: common.HexToAddress(self.WholeWorkAuthoritySigner), ClientKeyRootSigner: common.HexToAddress(rootSigner), EarningSelection: selection, PriorContracts: priors}, nil
}

// The selected provider head is independent of both transport and the payout
// row. Missing original mapping never becomes a zero coldkey or a missing wallet;
// a provider without a head reports an absent chain, from which earning-wallet
// selection may fall back to its network's consent.
func economicProviderWalletExpected(authority *payoutartifact.WholeWorkAuthority, member payoutartifact.WholeWorkExpectedProvider) (protocol.WalletMappingHistoryExpectation, error) {
	if member.WalletHeadHash == "" && member.WalletGeneration == 0 {
		return protocol.WalletMappingHistoryExpectation{}, protocol.WalletMappingAbsentError()
	}
	if member.WalletHeadHash == "" || member.WalletGeneration == 0 {
		return protocol.WalletMappingHistoryExpectation{}, protocol.ErrWalletMappingIntegrity
	}
	raw, err := hex.DecodeString(member.WalletHeadHash)
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != member.WalletHeadHash || member.WalletGeneration > protocol.MaxWalletMappingHistory {
		return protocol.WalletMappingHistoryExpectation{}, protocol.ErrWalletMappingIntegrity
	}
	return protocol.WalletMappingHistoryExpectation{Domain: authority.Domain, ClientId: member.ClientId, HeadHash: [32]byte(raw), Generation: member.WalletGeneration, Epoch: authority.Epoch}, nil
}

// The roster's network head for networkId. A network without a pinned chain
// reports an absent chain.
func economicNetworkWalletExpected(authority *payoutartifact.WholeWorkAuthority, networkId [16]byte) (protocol.NetworkWalletMappingHistoryExpectation, error) {
	wallet, found := authority.NetworkWallet(networkId)
	if !found {
		return protocol.NetworkWalletMappingHistoryExpectation{}, protocol.WalletMappingAbsentError()
	}
	raw, err := hex.DecodeString(wallet.WalletHeadHash)
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != wallet.WalletHeadHash || wallet.WalletGeneration == 0 || wallet.WalletGeneration > protocol.MaxWalletMappingHistory {
		return protocol.NetworkWalletMappingHistoryExpectation{}, protocol.ErrWalletMappingIntegrity
	}
	return protocol.NetworkWalletMappingHistoryExpectation{Domain: authority.Domain, NetworkId: networkId, HeadHash: [32]byte(raw), Generation: wallet.WalletGeneration, Epoch: authority.Epoch}, nil
}

// The roster's delegation head for networkId. A network without a pinned
// delegation reports an absent chain with the zero expectation, which hotkey
// resolution also reads as absent.
func economicHotkeyDelegationExpected(authority *payoutartifact.WholeWorkAuthority, networkId [16]byte) (protocol.HotkeyNetworkDelegationHistoryExpectation, error) {
	delegation, found := authority.HotkeyDelegation(networkId)
	if !found {
		return protocol.HotkeyNetworkDelegationHistoryExpectation{}, protocol.WalletMappingAbsentError()
	}
	raw, err := hex.DecodeString(delegation.DelegationHeadHash)
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != delegation.DelegationHeadHash || delegation.DelegationGeneration == 0 || delegation.DelegationGeneration > protocol.MaxWalletMappingHistory {
		return protocol.HotkeyNetworkDelegationHistoryExpectation{}, protocol.ErrWalletMappingIntegrity
	}
	return protocol.HotkeyNetworkDelegationHistoryExpectation{Domain: authority.Domain, NetworkId: networkId, HeadHash: [32]byte(raw), Generation: delegation.DelegationGeneration, Epoch: authority.Epoch}, nil
}

// Resolves every expected provider's earning wallet from the retained
// originals with settlement's precedence (protocol.SelectEarningWalletWithHotkey):
// the provider's own chain first; its network's chain only when the provider
// chain is absent or has no consent effective at the epoch; and its network's
// hotkey delegation only when the network chain is in turn absent or not
// effective (protocol.ResolveHotkeyEarningWallet). Every kind passes the same
// prospective gate. The evidence must be exactly what the resolution consults:
// one provider chain per expected provider with a head, in roster order; one
// network chain per network some provider fell back to; and one delegation
// chain and one global consent chain per network some provider fell back to
// hotkey mode for; each of the last three ordered by network.
func economicProviderEarningWallets(ctx context.Context, authority *payoutartifact.WholeWorkAuthority, originals *economicProviderOriginals, rootSigner common.Address, startBlock uint64, startUnix int64) (map[[16]byte]*protocol.EarningWallet, error) {
	if authority == nil || originals == nil {
		return nil, protocol.ErrWalletMappingUnavailable
	}
	networkOriginals := map[[16]byte][]protocol.WalletMappingConsent{}
	var priorNetworkId [16]byte
	for index, original := range originals.NetworkWallets {
		if index > 0 && bytes.Compare(priorNetworkId[:], original.NetworkId[:]) >= 0 {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
		networkOriginals[original.NetworkId] = original.Originals
		priorNetworkId = original.NetworkId
	}
	delegationOriginals := map[[16]byte][]protocol.WalletMappingConsent{}
	for index, original := range originals.HotkeyDelegations {
		if index > 0 && bytes.Compare(priorNetworkId[:], original.NetworkId[:]) >= 0 {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
		delegationOriginals[original.NetworkId] = original.Originals
		priorNetworkId = original.NetworkId
	}
	consentOriginals := map[[16]byte][]protocol.HotkeyWalletMappingConsent{}
	for index, original := range originals.HotkeyConsents {
		if index > 0 && bytes.Compare(priorNetworkId[:], original.NetworkId[:]) >= 0 {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
		consentOriginals[original.NetworkId] = original.Originals
		priorNetworkId = original.NetworkId
	}
	networkWallets := map[[16]byte]*protocol.VerifiedNetworkWalletMapping{}
	networkErrs := map[[16]byte]error{}
	consultedNetworkIds := map[[16]byte]bool{}
	// verifies a network chain once, however many providers fall back to it
	networkWallet := func(clientId [16]byte, networkId [16]byte) (*protocol.EarningWallet, error) {
		consultedNetworkIds[networkId] = true
		if err, failed := networkErrs[networkId]; failed {
			return nil, err
		}
		verified := networkWallets[networkId]
		if verified == nil {
			err := func() error {
				expected, err := economicNetworkWalletExpected(authority, networkId)
				if err != nil {
					return err
				}
				history, found := networkOriginals[networkId]
				if !found {
					return protocol.ErrWalletMappingUnavailable
				}
				verified, err = protocol.VerifyNetworkWalletMappingHistory(ctx, history, expected)
				if err != nil {
					return err
				}
				return protocol.VerifyProspectiveNetworkWalletMapping(ctx, verified, rootSigner, startBlock, startUnix)
			}()
			if err != nil {
				networkErrs[networkId] = err
				return nil, err
			}
			networkWallets[networkId] = verified
		}
		return protocol.NetworkEarningWallet(clientId, verified), nil
	}
	hotkeyWallets := map[[16]byte]*protocol.EarningWallet{}
	hotkeyErrs := map[[16]byte]error{}
	consultedHotkeyNetworkIds := map[[16]byte]bool{}
	// resolves a network's hotkey evidence once, however many providers fall
	// back to it; a hotkey wallet differs between the network's providers only
	// in its client
	hotkeyWallet := func(clientId [16]byte, networkId [16]byte) (*protocol.EarningWallet, error) {
		consultedHotkeyNetworkIds[networkId] = true
		if err, failed := hotkeyErrs[networkId]; failed {
			return nil, err
		}
		wallet := hotkeyWallets[networkId]
		if wallet == nil {
			expected, err := economicHotkeyDelegationExpected(authority, networkId)
			if err != nil && !errors.Is(err, protocol.ErrWalletMappingAbsent) {
				hotkeyErrs[networkId] = err
				return nil, err
			}
			// an unpinned network keeps the zero expectation: resolution reads it
			// as absent, and refuses any originals supplied for it
			wallet, err = protocol.ResolveHotkeyEarningWallet(ctx, clientId, protocol.HotkeyEarningWalletEvidence{Delegations: delegationOriginals[networkId], DelegationExpected: expected, Consents: consentOriginals[networkId]}, rootSigner, startBlock, startUnix)
			if err != nil {
				hotkeyErrs[networkId] = err
				return nil, err
			}
			hotkeyWallets[networkId] = wallet
		}
		owned := *wallet
		owned.ClientId = clientId
		return &owned, nil
	}
	wallets := make(map[[16]byte]*protocol.EarningWallet, len(authority.ExpectedProviders))
	walletIndex := 0
	for _, provider := range authority.ExpectedProviders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		providerWallet, providerErr := func() (*protocol.EarningWallet, error) {
			expected, err := economicProviderWalletExpected(authority, provider)
			if err != nil {
				return nil, err
			}
			if walletIndex >= len(originals.Wallets) || originals.Wallets[walletIndex].ClientId != provider.ClientId {
				return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
			}
			history := originals.Wallets[walletIndex].Originals
			walletIndex += 1
			verified, err := protocol.VerifyWalletMappingHistory(ctx, history, expected)
			if err != nil {
				return nil, err
			}
			if verified.Statement.NetworkId != provider.NetworkId {
				return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
			}
			if err := protocol.VerifyProspectiveWalletMapping(ctx, verified, rootSigner, startBlock, startUnix); err != nil {
				return nil, err
			}
			return protocol.ProviderEarningWallet(verified), nil
		}()
		wallet, err := protocol.SelectEarningWalletWithHotkey(providerWallet, providerErr, func() (*protocol.EarningWallet, error) {
			return networkWallet(provider.ClientId, provider.NetworkId)
		}, func() (*protocol.EarningWallet, error) {
			return hotkeyWallet(provider.ClientId, provider.NetworkId)
		})
		if err != nil {
			return nil, economicProviderEvidenceError(err)
		}
		if wallet.ClientId != provider.ClientId || wallet.NetworkId != provider.NetworkId {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
		wallets[provider.ClientId] = wallet
	}
	if walletIndex != len(originals.Wallets) {
		return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
	}
	// no unconsulted network or hotkey evidence rides along
	for networkId := range networkOriginals {
		if !consultedNetworkIds[networkId] {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
	}
	for networkId := range delegationOriginals {
		if !consultedHotkeyNetworkIds[networkId] {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
	}
	for networkId := range consentOriginals {
		if !consultedHotkeyNetworkIds[networkId] {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
	}
	return wallets, ctx.Err()
}

// Reserve actual serialized evidence, including JSON framing, before keeping
// another external response in the single per-pool handoff slot.
func (self *economicProviderOriginals) bounded(maximum uint64) error {
	raw, err := json.Marshal(self)
	if err != nil {
		return err
	}
	if maximum == 0 || uint64(len(raw)) > maximum {
		return errMonitorEconomicCapacity
	}
	return nil
}

// Every current window contract is looked up independently, including contracts
// omitted from the proposed exclusion list. Only a published original census
// may establish prior credit; failed candidate caches cannot acquire that role.
func (self *economicConservationArchiveView) providerPriorContracts(ctx context.Context, state *economicConservationState, authority *payoutartifact.WholeWorkAuthority, inventory *payoutartifact.WholeWorkInventory) ([]payoutartifact.WholeWorkPriorContract, error) {
	contracts, _, err := self.providerPriorOriginals(ctx, state, authority, inventory, nil)
	return contracts, err
}

// Requested stream origins come from independently authenticated source receipts.
// Lookup visits only those IDs and current contracts, never a historical prefix.
func (self *economicConservationArchiveView) providerPriorOriginals(ctx context.Context, state *economicConservationState, authority *payoutartifact.WholeWorkAuthority, inventory *payoutartifact.WholeWorkInventory, requested [][16]byte) ([]payoutartifact.WholeWorkPriorContract, []payoutartifact.WholeWorkRetainedCreation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if self == nil || state == nil || authority == nil || inventory == nil || inventory.Window == nil {
		return nil, nil, payoutartifact.ErrClosedWorkUnavailable
	}
	candidates := make(map[[16]byte]struct{}, len(inventory.Window.Records)+len(authority.PriorContracts)+len(requested))
	for _, row := range inventory.Window.Records {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		id := row.ContractId
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || strings.ToLower(id) != id {
			return nil, nil, payoutartifact.ErrClosedWorkIntegrity
		}
		raw, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
		if err != nil || len(raw) != 16 || [16]byte(raw) == ([16]byte{}) {
			return nil, nil, payoutartifact.ErrClosedWorkIntegrity
		}
		candidates[[16]byte(raw)] = struct{}{}
	}
	for _, proposed := range authority.PriorContracts {
		candidates[proposed.ContractId] = struct{}{}
	}
	requestedIds := make(map[[16]byte]struct{}, len(requested))
	for _, id := range requested {
		if id == ([16]byte{}) {
			return nil, nil, payoutartifact.ErrClosedWorkIntegrity
		}
		candidates[id], requestedIds[id] = struct{}{}, struct{}{}
	}
	if len(candidates) > payoutartifact.MaxClosedWorkRecords {
		return nil, nil, payoutartifact.ErrClosedWorkCapacity
	}
	knownCreations := make(map[[16]byte]payoutartifact.WholeWorkRetainedCreation, len(requestedIds))
	knownContracts := make(map[[16]byte]payoutartifact.WholeWorkPriorContract, len(candidates))
	for contractId := range candidates {
		for key := range self.providerContracts[economicProviderContractKey{Domain: authority.Domain, ContractId: contractId}] {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			known := self.providerCensuses[key]
			if known == nil || known.Domain != authority.Domain {
				return nil, nil, payoutartifact.ErrClosedWorkIntegrity
			}
			id := economicConservationEntitlementId(strconv.FormatUint(known.Epoch, 10), strconv.FormatUint(authority.Domain.NoID, 10))
			var record economicConservationEntitlement
			if index, ok := state.entitlementIds[id]; ok {
				record = state.Entitlements[index]
			} else {
				record = self.entitlements[id]
			}
			if record.Id != id || record.censusHash() != key || self.entitlementVerified[key] != economicEntitlementFundingHash(record) {
				continue
			}
			original, exists := known.NewContracts[contractId]
			if !exists || original.ReconciledEpoch != known.Epoch || original.InventoryHash != known.InventoryHash || known.Epoch >= authority.Epoch {
				return nil, nil, payoutartifact.ErrClosedWorkIntegrity
			}
			if prior, exists := knownContracts[contractId]; exists && prior != original {
				return nil, nil, payoutartifact.ErrClosedWorkIntegrity
			}
			knownContracts[contractId] = original
			if _, needed := requestedIds[contractId]; needed {
				if creation, exists := known.NewCreations[contractId]; exists {
					if creation.Checkpoint != original || creation.Original.ContractId != contractId {
						return nil, nil, payoutartifact.ErrClosedWorkIntegrity
					}
					if prior, exists := knownCreations[contractId]; exists && !reflect.DeepEqual(prior, creation) {
						return nil, nil, payoutartifact.ErrClosedWorkIntegrity
					}
					knownCreations[contractId] = creation
				}
			}
		}
	}
	for _, proposed := range authority.PriorContracts {
		original, exists := knownContracts[proposed.ContractId]
		if !exists {
			return nil, nil, payoutartifact.ErrClosedWorkUnavailable
		}
		if original != proposed {
			return nil, nil, payoutartifact.ErrClosedWorkIntegrity
		}
	}
	result := make([]payoutartifact.WholeWorkPriorContract, 0, len(knownContracts))
	for _, original := range knownContracts {
		result = append(result, original)
	}
	slices.SortFunc(result, func(a, b payoutartifact.WholeWorkPriorContract) int {
		return bytes.Compare(a.ContractId[:], b.ContractId[:])
	})
	creations := make([]payoutartifact.WholeWorkRetainedCreation, 0, len(knownCreations))
	used := 0
	for _, checkpoint := range result {
		if original, exists := knownCreations[checkpoint.ContractId]; exists {
			byteCount := len(original.Original.StoredContract) + len(original.Original.LatestInventory) + len(original.Original.OriginalCreation)
			if byteCount > payoutartifact.MaxWholeWorkInventoryBytes-used {
				return nil, nil, payoutartifact.ErrClosedWorkCapacity
			}
			used += byteCount
			creations = append(creations, cloneEconomicProviderCreation(original))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return result, creations, nil
}

// A caller cannot mutate the private original index through a returned witness.
func cloneEconomicProviderCreation(value payoutartifact.WholeWorkRetainedCreation) payoutartifact.WholeWorkRetainedCreation {
	value.Original.StoredContract = bytes.Clone(value.Original.StoredContract)
	value.Original.LatestInventory = bytes.Clone(value.Original.LatestInventory)
	value.Original.OriginalCreation = bytes.Clone(value.Original.OriginalCreation)
	return value
}

// Convert original row authorities only after all expected owners and requests
// were verified. The complete work roster supplies idle and failed providers;
// absence from the complete trial census then means exactly zero exposure.
func economicProviderTrialProjection(ctx context.Context, artifact *payoutartifact.Artifact, work *payoutartifact.VerifiedWholeWorkInventory, attempts *validator.VerifiedProviderAttemptMeasurement, wallets map[[16]byte]*protocol.EarningWallet, bindings []validator.VerifiedProviderAttemptBinding) (*economicProviderTrialValues, error) {
	if attempts == nil || attempts.VerifiedProviderAttemptWindow == nil || !attempts.CutCensusComplete || !attempts.OwnedRequestsComplete || attempts.ReliabilityAMin == 0 {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	domain := work.Domain
	if attempts.Domain != economicProviderAttemptDomain(domain) || attempts.Window.Epoch != artifact.Epoch || attempts.Window.StartBlock != artifact.Start.Number || attempts.Window.EndBlock != artifact.End.Number || attempts.ReliabilityAMin != artifact.ReliabilityAMin {
		return nil, protocol.ErrProviderAttemptsIntegrity
	}
	if len(bindings) != len(work.ExpectedProviders) || len(wallets) != len(work.ExpectedProviders) {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	counts := map[[16]byte]economicProviderTrialValue{}
	for _, expected := range work.ExpectedProviders {
		counts[expected.ClientId] = economicProviderTrialValue{clientId: expected.ClientId, networkId: expected.NetworkId}
	}
	for _, row := range attempts.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if row.NoId != artifact.NoID {
			continue
		}
		value, exists := counts[row.ClientId]
		if !exists || row.Confirmations > row.Assignments || row.Assignments > math.MaxUint64-value.assignments || row.Confirmations > math.MaxUint64-value.confirmations {
			return nil, protocol.ErrProviderAttemptsIntegrity
		}
		value.assignments += row.Assignments
		value.confirmations += row.Confirmations
		counts[row.ClientId] = value
	}
	result := &economicProviderTrialValues{artifactHash: artifact.ContentHash, registryHash: "sha256:" + hex.EncodeToString(attempts.RegistryHash[:]), windowHash: "sha256:" + hex.EncodeToString(attempts.WindowHash[:]), minimumAssignments: attempts.ReliabilityAMin, complete: true}
	for index, provider := range work.ExpectedProviders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wallet, binding := wallets[provider.ClientId], bindings[index]
		if wallet == nil || wallet.ClientId != provider.ClientId || wallet.NetworkId != provider.NetworkId || binding.ClientId != provider.ClientId {
			return nil, protocol.ErrProviderAttemptsIntegrity
		}
		value := counts[provider.ClientId]
		value.coldkey, value.headExcluded, value.bindingGeneration = wallet.Coldkey, binding.HeadExcluded, binding.BindingGeneration
		value.eligible = provider.UsageBytes > 0 && value.assignments >= attempts.ReliabilityAMin && value.confirmations > 0 && !value.headExcluded
		if value.headExcluded {
			value.exclusionReason = "head_fleet_active"
		} else if !value.eligible {
			value.exclusionReason = "reliability_exposure_floor"
		}
		result.providers = append(result.providers, value)
	}
	return result, ctx.Err()
}

// Registry scope excludes the per-operator lane; the full original replay
// verifies all lanes before the selected pool's counts are projected above.
func economicProviderAttemptDomain(domain protocol.ClientKeyHistoryDomain) protocol.ProviderAttemptDomain {
	return protocol.ProviderAttemptDomain{ChainId: domain.ChainID, GenesisHash: domain.GenesisHash, Netuid: domain.Netuid, Coordinator: domain.Coordinator, SettlementVault: domain.SettlementVault, DeploymentIdHash: domain.DeploymentIDHash, PolicyHash: domain.PolicyHash}
}

// Stable numeric ordering ensures older present originals are admitted before
// a later census names them, independent of active array or SQL ordering.
func economicProviderAdmissionOrder(records []economicConservationEntitlement) []economicConservationEntitlement {
	result := slices.Clone(records)
	slices.SortFunc(result, func(a, b economicConservationEntitlement) int {
		aEpoch, bEpoch := uint64(math.MaxUint64), uint64(math.MaxUint64)
		if a.Census != nil {
			aEpoch = a.Census.Artifact.Epoch
		}
		if b.Census != nil {
			bEpoch = b.Census.Artifact.Epoch
		}
		if aEpoch < bEpoch {
			return -1
		}
		if aEpoch > bEpoch {
			return 1
		}
		return bytes.Compare([]byte(a.Id), []byte(b.Id))
	})
	return result
}
