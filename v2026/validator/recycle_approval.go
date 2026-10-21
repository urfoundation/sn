// A separately selected signer approves an exact mainnet successor and config.
// Approval custody and finalized admission never authorize a weight broadcast.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

const ownerRecycleApprovalSchema = "urnetwork-owner-recycle-approval-v1"
const maximumOwnerRecycleApprovalBytes = 64 * 1024
const maximumOwnerRecycleApprovedHotkeys = 256
const retainedOwnerRecycleApprovalName = "owner-recycle-approved-successor.json"

// The public key and exact bytes are independent deployment inputs. A signer
// embedded in a downloaded approval cannot select its own trust anchor.
type ReleaseOwnerRecycleApprovalConfig struct {
	Approval ReleaseEvidenceV2File `json:"approval" yaml:"approval"`
	Signer   string                `json:"signer" yaml:"signer"`
}

// Binds the unchanged parent, complete release configuration, independently
// reviewed artifact and exact owner destinations. Epochs and blocks are native
// unless named by Proposal, whose effective epoch retains policy semantics.
type OwnerRecycleApproval struct {
	Schema                  string                          `json:"schema"`
	ConfigHash              [32]byte                        `json:"config_hash"`
	Proposal                OwnerRecycleProposal            `json:"proposal"`
	NativeChain             string                          `json:"native_chain"`
	RuntimeReviewHash       [32]byte                        `json:"runtime_review_hash"`
	ValidatorHotkey         [32]byte                        `json:"validator_hotkey"`
	SubnetOwner             [32]byte                        `json:"subnet_owner"`
	OwnerHotkeys            [][32]byte                      `json:"owner_hotkeys"`
	FirstNativeEpoch        uint64                          `json:"first_native_epoch"`
	ValidFromNativeBlock    uint64                          `json:"valid_from_native_block"`
	ValidThroughNativeBlock uint64                          `json:"valid_through_native_block"`
	MaximumSubnetUids       uint32                          `json:"maximum_subnet_uids"`
	MaximumOwnedHotkeys     uint32                          `json:"maximum_owned_hotkeys"`
	Production              *OwnerRecycleProductionApproval `json:"production,omitempty"`
}

// The signature covers a domain-separated canonical body. The exact envelope
// bytes are additionally selected by the config's content-addressed reference.
type OwnerRecycleApprovalEnvelope struct {
	Approval  OwnerRecycleApproval `json:"approval"`
	Signature string               `json:"signature"`
}

// Omitting only the self-referential envelope reference preserves every parent
// policy, identity, signer, route, mask, custody and evidence configuration field.
func OwnerRecycleConfigHash(cfg *ReleaseConfig) ([32]byte, error) {
	if cfg == nil {
		return [32]byte{}, errors.New("owner-recycle release configuration is absent")
	}
	if cfg.TreasuryApproval != nil {
		return TreasuryConfigHash(cfg)
	}
	copy := *cfg
	if productionEconomicSelection(cfg) != nil {
		copy.OwnerRecycleApproval = &ReleaseOwnerRecycleApprovalConfig{Signer: productionEconomicSelection(cfg).Signer}
	}
	raw, err := json.Marshal(copy)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte("urnetwork-owner-recycle-config-v1\n"), raw...)), nil
}

// Produces a bounded message for an external approval signer; it does not load
// a key or infer that the caller is authorized to approve the deployment.
func (self OwnerRecycleApproval) SigningMessage() ([]byte, error) {
	if (self.Schema != ownerRecycleApprovalSchema && self.Schema != ownerRecycleProductionApprovalSchema && self.Schema != TreasuryApprovalSchema) ||
		(self.Schema == ownerRecycleApprovalSchema) != (self.Production == nil) || len(self.OwnerHotkeys) > maximumOwnerRecycleApprovedHotkeys {
		return nil, errors.New("owner-recycle approval schema or owner bound differs")
	}
	if (self.Schema == TreasuryApprovalSchema) != (self.Proposal.Treasury != nil) {
		return nil, errors.New("treasury authority cannot be inserted into an owner-recycle signature domain")
	}
	if self.Proposal.Treasury != nil {
		if err := self.Proposal.Treasury.Validate(); err != nil {
			return nil, err
		}
	}
	if self.Production != nil && len(self.Production.ValidatorHotkeys) > maximumOwnerRecycleApprovedHotkeys {
		return nil, errors.New("owner-recycle production approval exceeds its validator bound")
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > maximumOwnerRecycleApprovalBytes/2 {
		return nil, errors.Join(errors.New("owner-recycle approval body is unavailable or oversized"), err)
	}
	domain := "urnetwork-owner-recycle-approval-signature-v1\n"
	if self.Production != nil {
		domain = "urnetwork-owner-recycle-production-approval-signature-v2\n"
	}
	if self.Schema == TreasuryApprovalSchema {
		domain = TreasuryApprovalSignatureDomain
	}
	digest := sha256.Sum256(append([]byte(domain), raw...))
	return digest[:], nil
}

// Configuration scope can be resolved before an envelope exists, without an
// invented reference. It never grants retention, observation or submission.
func validateOwnerRecycleApprovalScope(cfg *ReleaseConfig) error {
	if cfg == nil || productionEconomicSelection(cfg) == nil {
		return errors.New("owner-recycle approved successor selection is absent")
	}
	if cfg.TreasuryApproval != nil && (cfg.OwnerRecycleApproval != nil || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion) {
		return errors.New("treasury approval requires an exclusive schema 3 production selector")
	}
	if (cfg.SchemaVersion != ReleaseValidatorSchemaVersion && cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion) || cfg.Release != "1.0" || cfg.ValidatorID == 0 || cfg.DeployBlock == 0 ||
		strings.TrimSpace(cfg.DeploymentID) == "" || strings.ContainsAny(cfg.DeploymentID, "/\\.") ||
		!common.IsHexAddress(cfg.Coordinator) || common.HexToAddress(cfg.Coordinator) == (common.Address{}) ||
		!common.IsHexAddress(cfg.SettlementVault) || common.HexToAddress(cfg.SettlementVault) == (common.Address{}) {
		return errors.New("owner-recycle approval lacks exact release, deployment, validator or contract identity")
	}
	if !cfg.Production || cfg.Policy.NetworkProfile != "mainnet" || cfg.ChainID != 964 || cfg.Netuid != 25 ||
		strings.EqualFold(cfg.GenesisHash, provisionalRuntimeTestnetGenesis) || cfg.ProvisionalRuntimeCompatibility != "" ||
		cfg.ProvisionalDeferClosedNativeInput || cfg.historyAdoptionV2 != nil || cfg.PreviousPolicy != nil || cfg.SourceRolePredecessorV2 != nil {
		return errors.New("owner-recycle approval requires independent mainnet identity and no inherited provisional or policy history")
	}
	if _, err := parseHash32("owner-recycle mainnet genesis", cfg.GenesisHash); err != nil {
		return err
	}
	if err := validateReleaseRuntimeSuccessorProfile(cfg); err != nil {
		return err
	}
	if len(cfg.Substrate) == 0 || len(cfg.RPC) == 0 {
		return errors.New("owner-recycle approval needs explicit native and EVM routes")
	}
	for _, endpoint := range cfg.Substrate {
		if err := validateEndpoint("owner-recycle native route", endpoint, "ws", "wss"); err != nil {
			return err
		}
	}
	for _, endpoint := range cfg.RPC {
		if err := validateEndpoint("owner-recycle EVM route", endpoint, "http", "https"); err != nil {
			return err
		}
	}
	if err := cfg.Policy.Validate(); err != nil {
		return err
	}
	parentHash, err := cfg.Policy.Hash()
	if err != nil || cfg.PolicyHash != releaseHex32(parentHash) {
		return errors.Join(errors.New("owner-recycle configured parent policy hash differs"), err)
	}
	if !filepath.IsAbs(cfg.StateDir) || filepath.Clean(cfg.StateDir) != cfg.StateDir {
		return errors.New("owner-recycle retained approval needs a canonical absolute state directory")
	}
	_, err = canonicalAttemptHex32("owner-recycle independent approval signer", productionEconomicSelection(cfg).Signer, false)
	return err
}

// Custody and observation additionally need exact bytes selected by the config.
// Neither the scope nor the selection inherits provisional runtime/history.
// The signed source may lie in custody this account cannot search; readers
// then use the retained copy, and bootstrap's strict read still refuses.
func validateOwnerRecycleApprovalSelection(cfg *ReleaseConfig) error {
	if err := validateOwnerRecycleApprovalScope(cfg); err != nil {
		return err
	}
	return validateRetainedSourceReference(productionEconomicSelection(cfg).Approval, maximumOwnerRecycleApprovalBytes)
}

// Signature admission precedes all storage observations and durable mutation.
func decodeOwnerRecycleApproval(cfg *ReleaseConfig, encoded []byte) (*OwnerRecycleApprovalEnvelope, error) {
	if err := validateOwnerRecycleApprovalSelection(cfg); err != nil {
		return nil, err
	}
	if len(encoded) == 0 || len(encoded) > maximumOwnerRecycleApprovalBytes {
		return nil, errors.New("owner-recycle approval envelope exceeds its byte bound")
	}
	if err := protocol.ValidateUniqueJsonKeys(encoded); err != nil {
		return nil, err
	}
	var envelope OwnerRecycleApprovalEnvelope
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("owner-recycle approval: %w", err)
	}
	canonical, err := json.Marshal(envelope)
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("owner-recycle approval must use canonical envelope bytes and one final newline"), err)
	}
	message, err := admitOwnerRecycleApproval(cfg, &envelope.Approval)
	if err != nil {
		return nil, err
	}
	key, _ := canonicalAttemptHex32("owner-recycle signer", productionEconomicSelection(cfg).Signer, false)
	signature, err := hex.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || envelope.Signature != hex.EncodeToString(signature) || !ed25519.Verify(key[:], message, signature) {
		return nil, errors.New("owner-recycle successor approval signature differs from the independently pinned signer")
	}
	return &envelope, nil
}

// Every config-bound check precedes the signature, so an unsigned draft is
// refused by the same rules before an external signer is asked to approve it.
// Returns the exact message that signer must sign.
func admitOwnerRecycleApproval(cfg *ReleaseConfig, approval *OwnerRecycleApproval) ([]byte, error) {
	if err := validateOwnerRecycleProductionApproval(cfg, approval); err != nil {
		return nil, err
	}
	if err := approval.Proposal.Validate(cfg.Policy); err != nil {
		return nil, err
	}
	configHash, err := OwnerRecycleConfigHash(cfg)
	if err != nil || configHash != approval.ConfigHash {
		return nil, errors.Join(errors.New("owner-recycle approval names a different complete configuration"), err)
	}
	if approval.NativeChain == "" || strings.TrimSpace(approval.NativeChain) != approval.NativeChain || len(approval.NativeChain) > 128 ||
		approval.RuntimeReviewHash == ([32]byte{}) || approval.ValidatorHotkey == ([32]byte{}) || approval.SubnetOwner == ([32]byte{}) ||
		approval.FirstNativeEpoch == 0 || approval.ValidFromNativeBlock == 0 || approval.ValidThroughNativeBlock < approval.ValidFromNativeBlock ||
		approval.MaximumSubnetUids == 0 || approval.MaximumSubnetUids > 65535 || approval.MaximumOwnedHotkeys == 0 || approval.MaximumOwnedHotkeys > maximumOwnerRecycleCensusEntries {
		return nil, errors.New("owner-recycle approval lacks exact review, identity, native activation window or finite census bounds")
	}
	pin := approval.Proposal.Runtime
	if releaseHex32(pin.GenesisHash) != cfg.GenesisHash || pin.Netuid != cfg.Netuid || pin.Version.SpecName != "node-subtensor" || pin.Version.SpecVersion != cfg.RuntimeSpec ||
		pin.Version.TransactionVersion != cfg.TransactionVersion || pin.Version.StateVersion != cfg.StateVersion ||
		releaseHex32(pin.CodeHash) != cfg.RuntimeCodeHash || releaseHex32(pin.MetadataHash) != cfg.RuntimeMetadataHash {
		return nil, errors.New("owner-recycle approved runtime and configured mainnet tuple differ")
	}
	if len(approval.OwnerHotkeys) > maximumOwnerRecycleApprovedHotkeys || approval.Proposal.Treasury == nil &&
		(len(approval.OwnerHotkeys) == 0 || uint64(len(approval.OwnerHotkeys))*10*uint64(cfg.Policy.Steering.MaxWeightLimitU16) < 9*65535) {
		return nil, errors.New("owner-recycle approved destinations cannot fit the unchanged signed weight cap")
	}
	if policy := approval.Proposal.Treasury; policy != nil {
		if policy.MultisigAccount == approval.SubnetOwner || treasuryRecipientHotkey(policy, approval.ValidatorHotkey) {
			return nil, errors.New("treasury custody or recipients overlap subnet owner or validator self")
		}
		for _, recipient := range policy.Recipients {
			if uint32(recipient.Uid) >= approval.MaximumSubnetUids || recipient.RegistrationBlock > approval.ValidFromNativeBlock {
				return nil, errors.New("treasury approved registration is outside the signed census or activation")
			}
		}
		for _, owner := range approval.OwnerHotkeys {
			if treasuryRecipientHotkey(policy, owner) {
				return nil, errors.New("treasury approved recipient is recognized as a subnet owner")
			}
		}
	}
	// Owner-recycle weights its owner census, so its validator is never in it.
	// A treasury row never weights an owner: its census may hold the validator
	// itself, which the census reader admits only as the explicit subnet owner
	// hotkey under the subnet owner coldkey.
	for index, hotkey := range approval.OwnerHotkeys {
		if hotkey == ([32]byte{}) || hotkey == approval.ValidatorHotkey && approval.Proposal.Treasury == nil || index > 0 && bytes.Compare(approval.OwnerHotkeys[index-1][:], hotkey[:]) >= 0 {
			return nil, errors.New("owner-recycle approved owner identities must be ordered, unique, nonzero and exclude validator self unless treasury-approved")
		}
	}
	return approval.SigningMessage()
}

// Restarts consume the original immutable retained bytes, not a changed source
// file. A new approval hash cannot silently replace this fixed custody record.
func readRetainedOwnerRecycleApproval(ctx context.Context, cfg *ReleaseConfig) (*OwnerRecycleApprovalEnvelope, error) {
	if isOwnerRecycleProductionConfig(cfg) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return ownerRecycleProductionApproval(cfg)
	}
	if err := validateOwnerRecycleApprovalSelection(cfg); err != nil {
		return nil, err
	}
	reference := productionEconomicSelection(cfg).Approval
	reference.Path = filepath.Join(cfg.StateDir, retainedOwnerRecycleApprovalName)
	raw, err := ReadReleaseEvidenceV2File(ctx, reference, maximumOwnerRecycleApprovalBytes)
	if err != nil {
		return nil, fmt.Errorf("owner-recycle durable approval is unavailable: %w", err)
	}
	return decodeOwnerRecycleApproval(cfg, raw)
}

// Current schema-3 writers require the independently authenticated production
// authority and its history. Legacy observation approvals remain read-only;
// neither they nor a historical production owner can open a current writer.
func ownerRecycleProductionBoundary(cfg *ReleaseConfig) error {
	if isOwnerRecycleProductionConfig(cfg) {
		if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
			return err
		}
		if cfg.ownerRecycleProduction.historicalOnly {
			return errors.New("original production authority cannot start a current writer")
		}
		return nil
	}
	if cfg != nil && (cfg.Policy.NetworkProfile == "mainnet" || cfg.ChainID == 964 || productionEconomicSelection(cfg) != nil) {
		return errors.New("owner-recycle successor activation is blocked for this legacy configuration: current writes require independently authenticated schema-3 production authority and the concrete V2 measurement, envelope, durable intent and archive owners")
	}
	return nil
}
