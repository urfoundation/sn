// Receiving native rewards needs a public destination and registered hotkeys,
// not authority to sign or spend from the receiving account.
package main

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

const treasuryDestinationSchema = "urnetwork-native-treasury-destination-v1"

// An empty roster describes a known account only; routing requires observed
// registered recipients. No signer, threshold or device field is accepted.
type treasuryDestination struct {
	Schema           string   `json:"schema" yaml:"schema"`
	Profile          string   `json:"profile" yaml:"profile"`
	Netuid           uint16   `json:"netuid" yaml:"netuid"`
	GenesisHash      string   `json:"genesis_hash" yaml:"genesis_hash"`
	AccountId        string   `json:"account_id" yaml:"account_id"`
	RecipientHotkeys []string `json:"recipient_hotkeys" yaml:"recipient_hotkeys"`
}

// Public identity admission makes no statement about control of this account.
func (self treasuryDestination) validate() error {
	if self.Schema != treasuryDestinationSchema || self.Profile != "mainnet" || self.Netuid != 25 || !rootCanonicalHash(self.GenesisHash) || !rootCanonicalHash(self.AccountId) || len(self.RecipientHotkeys) > 100 {
		return errors.New("treasury destination requires mainnet identity, a native account and a bounded public roster")
	}
	previous := ""
	for _, hotkey := range self.RecipientHotkeys {
		if !rootCanonicalHash(hotkey) || hotkey == self.AccountId || hotkey <= previous {
			return errors.New("treasury destination hotkeys must be sorted, unique and distinct from the receiving account")
		}
		previous = hotkey
	}
	return nil
}

// Parsing rejects signing details and private material as unknown fields.
func decodeTreasuryDestination(raw []byte) (treasuryDestination, error) {
	var result treasuryDestination
	if err := decodeTreasuryYaml(raw, &result); err != nil {
		return result, err
	}
	return result, result.validate()
}

// An explicit independent pin binds the complete public destination file.
func readTreasuryDestination(ctx context.Context, path, expected string) (treasuryDestination, error) {
	var result treasuryDestination
	if !planSha256(expected) {
		return result, errors.New("treasury destination requires an independent file SHA-256")
	}
	raw, actual, err := readBootstrapRootFile(ctx, path, treasuryDescriptorLimit)
	if err != nil || actual != expected {
		return result, errors.Join(errors.New("treasury destination file differs from independent pin"), err)
	}
	return decodeTreasuryDestination(raw)
}

// Optional send custody projects the same receiving identities without devices.
func (self treasuryDescriptor) destination() treasuryDestination {
	result := treasuryDestination{Schema: treasuryDestinationSchema, Profile: self.Profile, Netuid: self.Netuid, GenesisHash: self.GenesisHash, AccountId: self.Multisig.AccountId}
	for _, hotkey := range self.RecipientHotkeys {
		result.RecipientHotkeys = append(result.RecipientHotkeys, hotkey.AccountId)
	}
	return result
}

// Native ownership and complete generations precede an unsigned routing policy.
func (self treasuryDestination) policy(facts treasuryFacts) (validator.TreasuryPolicy, error) {
	var result validator.TreasuryPolicy
	if err := self.validate(); err != nil {
		return result, err
	}
	if len(self.RecipientHotkeys) < 2 || facts.Missing != 0 || len(facts.Recipients) != len(self.RecipientHotkeys) || !rootCanonicalHash(facts.SubnetOwner) || facts.SubnetOwner == self.AccountId {
		return result, errors.New("treasury receiving policy requires every registered recipient and a distinct subnet owner")
	}
	result.Schema = validator.TreasuryReceivePolicySchema
	result.ProviderShare = protocol.Rational{Numerator: 1, Denominator: 10}
	result.TreasuryShare = protocol.Rational{Numerator: 9, Denominator: 10}
	result.MaxWeightLimitU16 = 32768
	raw, _ := hex.DecodeString(self.AccountId[2:])
	copy(result.MultisigAccount[:], raw)
	hotkeyKVs := make(map[string]bool, len(self.RecipientHotkeys))
	for _, hotkey := range self.RecipientHotkeys {
		hotkeyKVs[hotkey] = true
	}
	for _, recipient := range facts.Recipients {
		if !hotkeyKVs[recipient.Hotkey] || recipient.Coldkey != self.AccountId {
			return result, errors.New("treasury receiving policy differs from original hotkey ownership")
		}
		delete(hotkeyKVs, recipient.Hotkey)
		var hotkey [32]byte
		raw, _ := hex.DecodeString(recipient.Hotkey[2:])
		copy(hotkey[:], raw)
		result.Recipients = append(result.Recipients, validator.TreasuryRecipient{Uid: recipient.Uid, Hotkey: hotkey, RegistrationBlock: recipient.RegistrationBlock})
	}
	if facts.AutoStakeDestination != "" {
		if !rootCanonicalHash(facts.AutoStakeDestination) {
			return result, errors.New("treasury receiving policy has an invalid auto-stake destination")
		}
		var account [32]byte
		raw, _ := hex.DecodeString(facts.AutoStakeDestination[2:])
		copy(account[:], raw)
		result.AutoStakeDestination = &account
	}
	return result, result.Validate()
}
