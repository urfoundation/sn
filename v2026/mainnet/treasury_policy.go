// Registration produces a reviewable public projection only after original
// native ownership/UID generations exist. Local device paths never leave it.
package main

import (
	"encoding/hex"
	"errors"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// The validator's public policy hash is the only economic treasury digest.
func (self treasuryAction) validateTreasuryPolicy() error {
	if self.Treasury == nil {
		if self.TreasuryPolicyHash != "" || self.Inner.Kind != "register_limit" {
			return errors.New("treasury spend requires the independently approved public treasury policy")
		}
		return nil
	}
	hash, err := self.Treasury.Hash()
	if err != nil || self.TreasuryPolicyHash != "0x"+hex.EncodeToString(hash[:]) {
		return errors.Join(errors.New("treasury public policy hash differs"), err)
	}
	p := self.Treasury
	if "0x"+hex.EncodeToString(p.MultisigAccount[:]) != self.Descriptor.Multisig.AccountId || p.Threshold != self.Descriptor.Multisig.Threshold || len(p.Signatories) != len(self.Descriptor.Multisig.Signatories) || len(p.Recipients) != len(self.Descriptor.RecipientHotkeys) {
		return errors.New("treasury custody and approved public policy disagree")
	}
	for i, signer := range p.Signatories {
		if "0x"+hex.EncodeToString(signer[:]) != self.Descriptor.Multisig.Signatories[i].AccountId {
			return errors.New("treasury public policy signer set changed")
		}
	}
	recipients := map[string]bool{}
	for _, r := range p.Recipients {
		recipients["0x"+hex.EncodeToString(r.Hotkey[:])] = true
	}
	for _, r := range self.Descriptor.RecipientHotkeys {
		if !recipients[r.AccountId] {
			return errors.New("treasury public policy recipient set differs")
		}
	}
	return nil
}

// This unsigned template must be admitted by the independent economic workflow.
func treasuryPublicPolicy(descriptor treasuryDescriptor, f treasuryFacts) (validator.TreasuryPolicy, error) {
	var p validator.TreasuryPolicy
	if err := descriptor.validate(); err != nil {
		return p, err
	}
	if f.Missing != 0 || len(f.Recipients) != len(descriptor.RecipientHotkeys) {
		return p, errors.New("treasury public policy requires every original registered generation")
	}
	p.Schema = validator.TreasuryPolicySchema
	p.Threshold = descriptor.Multisig.Threshold
	p.ProviderShare = protocol.Rational{Numerator: 1, Denominator: 10}
	p.TreasuryShare = protocol.Rational{Numerator: 9, Denominator: 10}
	p.MaxWeightLimitU16 = 32768
	raw, _ := hex.DecodeString(descriptor.Multisig.AccountId[2:])
	copy(p.MultisigAccount[:], raw)
	for _, signer := range descriptor.Multisig.Signatories {
		var account [32]byte
		raw, _ := hex.DecodeString(signer.AccountId[2:])
		copy(account[:], raw)
		p.Signatories = append(p.Signatories, account)
	}
	for _, r := range f.Recipients {
		var hotkey [32]byte
		raw, _ := hex.DecodeString(r.Hotkey[2:])
		copy(hotkey[:], raw)
		p.Recipients = append(p.Recipients, validator.TreasuryRecipient{Uid: r.Uid, Hotkey: hotkey, RegistrationBlock: r.RegistrationBlock})
	}
	if f.AutoStakeDestination != "" {
		var account [32]byte
		raw, _ := hex.DecodeString(f.AutoStakeDestination[2:])
		copy(account[:], raw)
		p.AutoStakeDestination = &account
	}
	return p, p.Validate()
}
