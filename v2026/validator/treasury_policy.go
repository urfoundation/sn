// Public treasury policy commits a native receiving account and the complete
// ordinary-credit roster. Only the original custody schema proves its public
// multisig derivation; neither schema grants signing or withdrawal authority.
package validator

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const TreasuryPolicySchema = "urnetwork-native-treasury-policy-v1"
const TreasuryReceivePolicySchema = "urnetwork-native-treasury-receive-policy-v1"
const TreasuryProposalSchema = "urnetwork-native-treasury-proposal-v1"
const TreasuryApprovalSchema = "urnetwork-native-treasury-approval-v1"
const TreasuryProductionScope = "urnetwork-native-treasury-production-v1"
const treasuryProductionDecisionSchema = "urnetwork-native-treasury-production-decision-v1"
const maximumTreasuryRecipients = 256

// Registration generation prevents a reused UID from receiving a stale grant.
type TreasuryRecipient struct {
	Uid               uint16   `json:"uid"`
	Hotkey            [32]byte `json:"hotkey"`
	RegistrationBlock uint64   `json:"registration_block"`
}

// MultisigAccount retains its original wire name for both schemas. The receive
// schema treats it only as a public destination and rejects signing metadata;
// the custody schema requires its exact public threshold and signatory set.
// A nil destination grants only an absent AutoStakeDestination storage value.
type TreasuryPolicy struct {
	Schema               string              `json:"schema"`
	MultisigAccount      [32]byte            `json:"multisig_account"`
	Threshold            uint16              `json:"threshold"`
	Signatories          [][32]byte          `json:"signatories"`
	Recipients           []TreasuryRecipient `json:"recipients"`
	ProviderShare        protocol.Rational   `json:"provider_share"`
	TreasuryShare        protocol.Rational   `json:"treasury_share"`
	MaxWeightLimitU16    uint16              `json:"max_weight_limit_u16"`
	AutoStakeDestination *[32]byte           `json:"auto_stake_destination,omitempty"`
}

// The first successor fixes its split and current cap; a missing recipient may
// never cause a smaller roster, a provider-only row or an increased cap.
func (self TreasuryPolicy) Validate() error {
	if (self.Schema != TreasuryPolicySchema && self.Schema != TreasuryReceivePolicySchema) || self.ProviderShare != (protocol.Rational{Numerator: 1, Denominator: 10}) ||
		self.TreasuryShare != (protocol.Rational{Numerator: 9, Denominator: 10}) || self.MaxWeightLimitU16 != 32768 ||
		len(self.Recipients) < 2 || len(self.Recipients) > maximumTreasuryRecipients {
		return errors.New("treasury policy requires its exact schema, 10/90 split, current cap and bounded complete roster")
	}
	if self.Schema == TreasuryReceivePolicySchema {
		if self.MultisigAccount == ([32]byte{}) || self.Threshold != 0 || self.Signatories != nil {
			return errors.New("treasury receiving policy requires a nonzero public account and no signing metadata")
		}
	} else {
		account, err := crv4.DeriveNativeMultisigAccount(self.Signatories, self.Threshold)
		if err != nil || account == ([32]byte{}) || account != self.MultisigAccount {
			return errors.Join(errors.New("treasury public multisig derivation differs"), err)
		}
	}
	hotkeyKVs := make(map[[32]byte]bool, len(self.Recipients))
	for index, recipient := range self.Recipients {
		if recipient.Hotkey == ([32]byte{}) || recipient.Hotkey == self.MultisigAccount || hotkeyKVs[recipient.Hotkey] ||
			recipient.RegistrationBlock == 0 || index > 0 && recipient.Uid <= self.Recipients[index-1].Uid {
			return errors.New("treasury recipients must be ordered unique registrations with nonzero distinct hotkeys")
		}
		hotkeyKVs[recipient.Hotkey] = true
	}
	if self.AutoStakeDestination != nil && *self.AutoStakeDestination == ([32]byte{}) {
		return errors.New("treasury auto-stake destination is zero")
	}
	return nil
}

// This canonical public hash is bound by the economic approval and native
// accounting authority; it does not independently grant execution or signing.
func (self TreasuryPolicy) Hash() ([32]byte, error) {
	if err := self.Validate(); err != nil {
		return [32]byte{}, err
	}
	raw, err := json.Marshal(self)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte(self.Schema+"\n"), raw...)), nil
}

// Successors share bounded carrier layouts, but require distinct explicit
// schemas, configuration selection and signature/source domains at admission.
type TreasuryProposal = OwnerRecycleProposal
type TreasuryApproval = OwnerRecycleApproval
type TreasuryApprovalEnvelope = OwnerRecycleApprovalEnvelope
type ReleaseTreasuryApprovalConfig = ReleaseOwnerRecycleApprovalConfig
type TreasuryProductionApproval = OwnerRecycleProductionApproval
type TreasuryProductionIntent = OwnerRecycleProductionIntent

// Configuration selection never infers a successor from a runtime number.
func productionEconomicSelection(cfg *ReleaseConfig) *ReleaseOwnerRecycleApprovalConfig {
	if cfg == nil {
		return nil
	}
	if cfg.TreasuryApproval != nil {
		return cfg.TreasuryApproval
	}
	return cfg.OwnerRecycleApproval
}

// Only an exclusive treasury selection on the mainnet schema-3 production path
// may answer an empty provider allocation with its reserve-only row. Testnet,
// the original owner-recycle successor and legacy configurations never can.
func treasuryReserveFallbackConfig(cfg *ReleaseConfig) bool {
	return cfg != nil && cfg.TreasuryApproval != nil && cfg.OwnerRecycleApproval == nil &&
		cfg.SchemaVersion == ReleaseMainnetProductionSchemaVersion && cfg.Policy.NetworkProfile == "mainnet"
}

// One carrier can hold only one economic authority, including during replay.
func productionEconomicIntent(intent *SteeringIntent) *OwnerRecycleProductionIntent {
	if intent == nil || intent.OwnerRecycle != nil && intent.Treasury != nil {
		return nil
	}
	if intent.Treasury != nil {
		return intent.Treasury
	}
	return intent.OwnerRecycle
}

// Old schema bytes retain their original digest; treasury selection uses an
// independent domain and removes only its own self-referential content address.
func TreasuryConfigHash(cfg *ReleaseConfig) ([32]byte, error) {
	if cfg == nil || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return [32]byte{}, errors.New("treasury config requires one explicit treasury approval selector")
	}
	owned := *cfg
	owned.TreasuryApproval = &ReleaseTreasuryApprovalConfig{Signer: cfg.TreasuryApproval.Signer}
	raw, err := json.Marshal(owned)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte("urnetwork-native-treasury-config-v1\n"), raw...)), nil
}

// Exact roster matching is shared by signed-policy validation and observations.
func treasuryRecipientHotkey(policy *TreasuryPolicy, hotkey [32]byte) bool {
	if policy != nil {
		for _, recipient := range policy.Recipients {
			if bytes.Equal(recipient.Hotkey[:], hotkey[:]) {
				return true
			}
		}
	}
	return false
}
