// Independent whole-work authority enumerates every expected SDK owner before
// a consumer can interpret an empty or partial operator database as complete.
package payoutartifact

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

const WholeWorkAuthoritySchema = "urnetwork-whole-work-authority-v1"

// A roster that pins network wallet chains. A roster without them keeps the v1
// schema and its exact canonical bytes.
const WholeWorkAuthorityNetworkWalletSchema = "urnetwork-whole-work-authority-v2"
const WholeWorkInventorySchema = "urnetwork-whole-work-inventory-v1"
const MaxWholeWorkOwners = 8192
const MaxWholeWorkSources = 64
const MaxWholeWorkAuthorityBytes = 2 * 1024 * 1024
const MaxWholeWorkInventoryBytes = 8 * 1024 * 1024

// The external authority names an exact lifecycle, not merely a reusable key.
type WholeWorkOwner struct {
	ClientId   [16]byte `json:"client_id"`
	NetworkId  [16]byte `json:"network_id"`
	Generation [16]byte `json:"generation"`
	PublicKey  [32]byte `json:"public_key"`
}

// A new signed roster may cover each future epoch under the admitted signer.
// Every owner, including owners with no work, owes both original boundary cuts.
type WholeWorkAuthority struct {
	Schema            string                          `json:"schema"`
	Domain            protocol.ClientKeyHistoryDomain `json:"domain"`
	Epoch             uint64                          `json:"epoch"`
	Start             Boundary                        `json:"start"`
	End               Boundary                        `json:"end"`
	RequestPublicKey  [32]byte                        `json:"request_public_key"`
	ClockProfile      string                          `json:"clock_profile"`
	PriorContracts    []WholeWorkPriorContract        `json:"prior_contracts"`
	Owners            []WholeWorkOwner                `json:"owners"`
	ExpectedProviders []WholeWorkExpectedProvider     `json:"expected_providers"`
	// the network consent chain heads, strictly ordered by network; v2 only
	NetworkWallets []WholeWorkNetworkWallet               `json:"network_wallets,omitempty"`
	WorkSources    []protocol.ProviderWorkSourceAuthority `json:"work_sources,omitempty"`
	Signer         common.Address                         `json:"signer"`
	Signature      [65]byte                               `json:"signature"`
}

// The independent authority enumerates providers even when their SDK generated
// no earning rows. A missing wallet checkpoint leaves consent unknown only.
type WholeWorkExpectedProvider struct {
	ClientId         [16]byte `json:"client_id"`
	NetworkId        [16]byte `json:"network_id"`
	WalletHeadHash   string   `json:"wallet_head_hash"`
	WalletGeneration uint64   `json:"wallet_generation"`
}

// The pinned head of a network's consent chain. Every expected provider of the
// network whose own chain is absent or not effective at the epoch earns to the
// network consent effective at that epoch (protocol.SelectEarningWallet).
type WholeWorkNetworkWallet struct {
	NetworkId        [16]byte `json:"network_id"`
	WalletHeadHash   string   `json:"wallet_head_hash"`
	WalletGeneration uint64   `json:"wallet_generation"`
}

// The pinned network chain head for networkId, if the roster has one.
func (self WholeWorkAuthority) NetworkWallet(networkId [16]byte) (WholeWorkNetworkWallet, bool) {
	for _, wallet := range self.NetworkWallets {
		if wallet.NetworkId == networkId {
			return wallet, true
		}
	}
	return WholeWorkNetworkWallet{}, false
}

// Asynchronous start cuts cannot date a terminal contract. Excluding prior
// work therefore requires this independent explicit checkpoint of a previously
// reconciled window, with exact original reservation and terminal-head hashes.
type WholeWorkPriorContract struct {
	ContractId               [16]byte `json:"contract_id"`
	ReconciledEpoch          uint64   `json:"reconciled_epoch"`
	InventoryHash            string   `json:"inventory_hash"`
	StoredContractHash       [32]byte `json:"stored_contract_hash"`
	SourceInventoryHash      [32]byte `json:"source_inventory_hash"`
	DestinationInventoryHash [32]byte `json:"destination_inventory_hash"`
}

// These values come from separately admitted policy, never from the witness.
// An optional hash restricts a policy to one exact independently signed roster.
type WholeWorkExpectation struct {
	AuthorityHash       string                     `json:"authority_hash,omitempty"`
	AuthoritySigner     common.Address             `json:"authority_signer"`
	ClientKeyRootSigner common.Address             `json:"client_key_root_signer"`
	AttributionSigner   common.Address             `json:"attribution_signer,omitempty"`
	EarningSelection    *WholeWorkEarningSelection `json:"earning_selection,omitempty"`
	// Derived only from previously verified retained originals in this domain.
	// It cannot be populated from the current authority's proposed exclusions.
	PriorContracts []WholeWorkPriorContract    `json:"-"`
	PriorCreations []WholeWorkRetainedCreation `json:"-"`
}

// An independently retained complete admission binds these exact original
// source bytes to its reconciled checkpoint. Current witnesses cannot add one.
type WholeWorkRetainedCreation struct {
	Checkpoint WholeWorkPriorContract            `json:"checkpoint"`
	Owner      WholeWorkOwner                    `json:"owner"`
	Original   coreprotocol.OriginalWorkContract `json:"original"`
}

// The original deployment policy selects earning time independently of the
// publisher. Full epoch originals remain required on both sides of the cutoff.
type WholeWorkEarningSelection struct {
	StartTime  time.Time `json:"start_time"`
	PolicyHash string    `json:"policy_hash"`
}

// Public transport carries canonical originals and never selects authority.
type WholeWorkOwnerCuts struct {
	StartRequest []byte `json:"start_request"`
	Start        []byte `json:"start"`
	EndRequest   []byte `json:"end_request"`
	End          []byte `json:"end"`
}

// The separate window supports known-empty epochs without inventing a dummy
// earning row solely to carry original inventory evidence.
type WholeWorkInventory struct {
	Schema               string                 `json:"schema"`
	Authority            []byte                 `json:"authority"`
	Owners               []WholeWorkOwnerCuts   `json:"owners"`
	Window               *ClosedWorkWindow      `json:"window"`
	Clock                *ClosedWorkWindowClock `json:"clock"`
	AttributionOriginals [][]byte               `json:"attribution_originals,omitempty"`
}

// Reconstructed provider identities and amounts are joined with independently
// complete validator trials and eligibility by the economic consumer.
type WholeWorkProvider struct {
	ClientId         [16]byte `json:"client_id"`
	NetworkId        [16]byte `json:"network_id"`
	UsageBytes       uint64   `json:"usage_bytes"`
	WalletHeadHash   string   `json:"wallet_head_hash"`
	WalletGeneration uint64   `json:"wallet_generation"`
}

// This component authenticates whole contract coverage, not provider eligibility
// or native outcome. Consumers must still join the other original components.
type VerifiedWholeWorkInventory struct {
	Complete            bool
	AttributionComplete bool
	Domain              protocol.ClientKeyHistoryDomain
	Epoch               uint64
	Start               Boundary
	End                 Boundary
	AuthorityHash       string
	InventoryHash       string
	WindowHash          string
	Contracts           uint64
	Credited            uint64
	Canceled            uint64
	Open                uint64
	ExpectedProviders   []WholeWorkProvider
	ReconciledContracts []WholeWorkPriorContract
	RetainedCreations   []WholeWorkRetainedCreation
	Reports             *VerifiedClosedWorkReports
}

// Stable canonical bytes bind the entire roster, including its exact ordering.
func (self WholeWorkAuthority) digest(ctx context.Context) ([32]byte, error) {
	if ctx == nil {
		return [32]byte{}, errors.New("whole-work authority requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return [32]byte{}, err
	}
	// v1 never pins network chains and v2 always does, so each roster has one
	// canonical schema
	schemaValid := self.Schema == WholeWorkAuthoritySchema && len(self.NetworkWallets) == 0 || self.Schema == WholeWorkAuthorityNetworkWalletSchema && 0 < len(self.NetworkWallets)
	if self.Domain.Validate() != nil || !schemaValid || self.Start.Number == 0 || self.End.Number <= self.Start.Number || !IsDigest(self.Start.Hash, "0x") || !IsDigest(self.End.Hash, "0x") || self.RequestPublicKey == ([32]byte{}) || self.Signer == (common.Address{}) || self.Owners == nil || len(self.Owners) > MaxWholeWorkOwners {
		return [32]byte{}, ErrClosedWorkIntegrity
	}
	if len(self.WorkSources) > MaxWholeWorkSources {
		return [32]byte{}, ErrClosedWorkCapacity
	}
	domainHash, _ := self.Domain.Digest()
	priorSource := ""
	for _, source := range self.WorkSources {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		identity := source.SourceId + "/" + source.Generation
		if source.Validate() != nil || source.DomainHash != domainHash || identity <= priorSource {
			return [32]byte{}, ErrClosedWorkIntegrity
		}
		priorSource = identity
	}
	var prior [16]byte
	var priorOwner WholeWorkOwner
	for i, owner := range self.Owners {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		order := bytes.Compare(priorOwner.ClientId[:], owner.ClientId[:])
		if owner.ClientId == ([16]byte{}) || owner.NetworkId == ([16]byte{}) || owner.Generation == ([16]byte{}) || owner.PublicKey == ([32]byte{}) || i > 0 && (order > 0 || order == 0 && (bytes.Compare(priorOwner.Generation[:], owner.Generation[:]) >= 0 || priorOwner.NetworkId != owner.NetworkId)) {
			return [32]byte{}, ErrClosedWorkIntegrity
		}
		priorOwner = owner
	}
	if len(self.ExpectedProviders) > MaxWholeWorkOwners {
		return [32]byte{}, ErrClosedWorkCapacity
	}
	owners := make(map[[16]byte]WholeWorkOwner, len(self.Owners))
	for _, owner := range self.Owners {
		owners[owner.ClientId] = owner
	}
	prior = [16]byte{}
	for index, provider := range self.ExpectedProviders {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		owner, exists := owners[provider.ClientId]
		if !exists || owner.NetworkId != provider.NetworkId || index > 0 && bytes.Compare(prior[:], provider.ClientId[:]) >= 0 {
			return [32]byte{}, ErrClosedWorkIntegrity
		}
		if provider.WalletHeadHash == "" {
			if provider.WalletGeneration != 0 {
				return [32]byte{}, ErrClosedWorkIntegrity
			}
		} else if !canonicalClosedWorkDigest("sha256:"+provider.WalletHeadHash) || provider.WalletGeneration == 0 || provider.WalletGeneration > 4096 {
			return [32]byte{}, ErrClosedWorkIntegrity
		}
		prior = provider.ClientId
	}
	if len(self.NetworkWallets) > MaxWholeWorkOwners {
		return [32]byte{}, ErrClosedWorkCapacity
	}
	providerNetworkIds := make(map[[16]byte]bool, len(self.ExpectedProviders))
	for _, provider := range self.ExpectedProviders {
		providerNetworkIds[provider.NetworkId] = true
	}
	var priorNetworkId [16]byte
	for index, wallet := range self.NetworkWallets {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		// a network chain is pinned only for a network with an expected provider
		if wallet.NetworkId == ([16]byte{}) || !providerNetworkIds[wallet.NetworkId] || index > 0 && bytes.Compare(priorNetworkId[:], wallet.NetworkId[:]) >= 0 {
			return [32]byte{}, ErrClosedWorkIntegrity
		}
		if !canonicalClosedWorkDigest("sha256:"+wallet.WalletHeadHash) || wallet.WalletGeneration == 0 || wallet.WalletGeneration > protocol.MaxWalletMappingHistory {
			return [32]byte{}, ErrClosedWorkIntegrity
		}
		priorNetworkId = wallet.NetworkId
	}
	if self.ClockProfile != "" && self.ClockProfile != FrontierWindowClockProfile || self.PriorContracts == nil || len(self.PriorContracts) > MaxClosedWorkRecords {
		return [32]byte{}, ErrClosedWorkIntegrity
	}
	prior = [16]byte{}
	for index, contract := range self.PriorContracts {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		if contract.ContractId == ([16]byte{}) || contract.ReconciledEpoch >= self.Epoch || !canonicalClosedWorkDigest(contract.InventoryHash) || contract.StoredContractHash == ([32]byte{}) || contract.SourceInventoryHash == ([32]byte{}) || index > 0 && bytes.Compare(prior[:], contract.ContractId[:]) >= 0 {
			return [32]byte{}, ErrClosedWorkIntegrity
		}
		prior = contract.ContractId
	}
	self.Signature = [65]byte{}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > MaxWholeWorkAuthorityBytes {
		return [32]byte{}, ErrClosedWorkCapacity
	}
	return sha256.Sum256(raw), nil
}

// The independently reviewed authority owner supplies this key; it is never
// available to the ordinary artifact publisher or SDK transport worker.
func SignWholeWorkAuthority(ctx context.Context, authority WholeWorkAuthority, key *ecdsa.PrivateKey) (WholeWorkAuthority, error) {
	if key == nil || key.D == nil || key.Curve != crypto.S256() || key.X == nil || key.Y == nil || key.D.Sign() <= 0 || key.D.Cmp(crypto.S256().Params().N) >= 0 {
		return WholeWorkAuthority{}, ErrClosedWorkIntegrity
	}
	x, y := crypto.S256().ScalarBaseMult(key.D.Bytes())
	if x.Cmp(key.X) != 0 || y.Cmp(key.Y) != 0 {
		return WholeWorkAuthority{}, ErrClosedWorkIntegrity
	}
	authority.Schema, authority.Signer = WholeWorkAuthoritySchema, crypto.PubkeyToAddress(key.PublicKey)
	if 0 < len(authority.NetworkWallets) {
		authority.Schema = WholeWorkAuthorityNetworkWalletSchema
	}
	digest, err := authority.digest(ctx)
	if err != nil {
		return WholeWorkAuthority{}, err
	}
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		return WholeWorkAuthority{}, err
	}
	copy(authority.Signature[:], signature)
	return authority, nil
}

// Recover only canonical low-s signatures under an independently expected key.
func (self WholeWorkAuthority) Verify(ctx context.Context, expected common.Address) error {
	digest, err := self.digest(ctx)
	if err != nil {
		return err
	}
	if expected == (common.Address{}) || self.Signer != expected || !crypto.ValidateSignatureValues(self.Signature[64], new(big.Int).SetBytes(self.Signature[:32]), new(big.Int).SetBytes(self.Signature[32:64]), true) {
		return ErrClosedWorkIntegrity
	}
	key, err := crypto.SigToPub(digest[:], self.Signature[:])
	if err != nil || crypto.PubkeyToAddress(*key) != expected {
		return ErrClosedWorkIntegrity
	}
	return ctx.Err()
}

// Exact signed authority bytes can be transported independently of an artifact.
func (self WholeWorkAuthority) Bytes(ctx context.Context) ([]byte, error) {
	if err := self.Verify(ctx, self.Signer); err != nil {
		return nil, err
	}
	return json.Marshal(self)
}

// No alternate spellings, duplicate keys or unknown fields gain authority.
func DecodeWholeWorkAuthority(ctx context.Context, raw []byte, expected common.Address) (WholeWorkAuthority, error) {
	var authority WholeWorkAuthority
	if ctx == nil {
		return authority, errors.New("whole-work authority requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return authority, err
	}
	if len(raw) == 0 {
		return authority, ErrClosedWorkUnavailable
	}
	if len(raw) > MaxWholeWorkAuthorityBytes {
		return authority, ErrClosedWorkCapacity
	}
	if err := json.Unmarshal(raw, &authority); err != nil {
		return WholeWorkAuthority{}, errors.Join(ErrClosedWorkIntegrity, err)
	}
	if err := authority.Verify(ctx, expected); err != nil {
		return WholeWorkAuthority{}, err
	}
	canonical, _ := authority.Bytes(ctx)
	if !bytes.Equal(canonical, raw) {
		return WholeWorkAuthority{}, ErrClosedWorkIntegrity
	}
	return authority, nil
}

// Capacity is checked before signing/decoding can recursively copy raw cuts.
func cloneWholeWorkInventory(ctx context.Context, inventory *WholeWorkInventory) (*WholeWorkInventory, error) {
	if inventory == nil {
		return nil, nil
	}
	if ctx == nil {
		return nil, errors.New("whole-work inventory requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(inventory.Owners) > MaxWholeWorkOwners || len(inventory.Authority) > MaxWholeWorkAuthorityBytes || len(inventory.AttributionOriginals) > MaxClosedWorkRecords {
		return nil, ErrClosedWorkCapacity
	}
	used := len(inventory.Authority)
	for _, raw := range inventory.AttributionOriginals {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(raw) > protocol.MaximumProviderWorkReceiptBytes || len(raw) > MaxWholeWorkInventoryBytes-used {
			return nil, ErrClosedWorkCapacity
		}
		used += len(raw)
	}
	for _, owner := range inventory.Owners {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, raw := range [][]byte{owner.StartRequest, owner.Start, owner.EndRequest, owner.End} {
			if len(raw) > MaxWholeWorkInventoryBytes-used {
				return nil, ErrClosedWorkCapacity
			}
			used += len(raw)
		}
	}
	if inventory.Window != nil {
		if len(inventory.Window.Records) > MaxClosedWorkRecords || len(inventory.Window.Schema) > 128 || len(inventory.Window.Start) > 128 || len(inventory.Window.End) > 128 {
			return nil, ErrClosedWorkCapacity
		}
		for _, record := range inventory.Window.Records {
			if len(record.ContractId) > 36 || len(record.Disposition) > 32 || record.ClosedAt != nil && len(*record.ClosedAt) > 128 {
				return nil, ErrClosedWorkCapacity
			}
			if len(record.Original) > MaxWholeWorkInventoryBytes-used {
				return nil, ErrClosedWorkCapacity
			}
			used += len(record.Original)
		}
	}
	if inventory.Clock != nil && (len(inventory.Clock.StartHeader) > 64*1024 || len(inventory.Clock.EndHeader) > 64*1024) {
		return nil, ErrClosedWorkCapacity
	}
	raw, err := json.Marshal(inventory)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxWholeWorkInventoryBytes {
		return nil, ErrClosedWorkCapacity
	}
	var result WholeWorkInventory
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, ctx.Err()
}

// Public exact-hash reads use this same canonical retained byte identity.
func wholeWorkBytesHash(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}
