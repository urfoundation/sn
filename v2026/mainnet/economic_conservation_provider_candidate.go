// Live candidates may verify before a later capacity or Claim refusal. Only
// this serial parent transaction owns their private cache additions and charges.
package main

import "errors"

// The undo journal records changed keys, never copies the historical index.
// Original facts and physical custody counters are not rolled back.
type economicProviderCandidate struct {
	view         *economicConservationArchiveView
	entries      uint64
	bytes        uint64
	providerNil  bool
	contractsNil bool
	verifiedNil  bool
	leavesNil    bool
	closed       bool
	censuses     map[string]economicProviderCandidateCensus
	contracts    map[economicProviderCandidateContract]economicProviderCandidateMembership
}

type economicProviderCandidateCensus struct {
	provider        *economicProviderAdmitted
	providerPresent bool
	verified        string
	verifiedPresent bool
	leaves          map[string]string
	leavesPresent   bool
}

type economicProviderCandidateContract struct {
	contract economicProviderContractKey
	census   string
}

type economicProviderCandidateMembership struct {
	indexPresent  bool
	censusPresent bool
}

// A detached entitlement worker never enters this serial parent operation.
// Cold admission already owns its private whole-view discard-on-error boundary.
func (self *economicConservationArchiveView) beginProviderCandidate() (*economicProviderCandidate, error) {
	if self == nil || self.providerCandidate != nil {
		return nil, errors.New("economic provider candidate requires its single original parent owner")
	}
	value := &economicProviderCandidate{view: self, entries: self.entries, bytes: self.bytes,
		providerNil: self.providerCensuses == nil, contractsNil: self.providerContracts == nil,
		verifiedNil: self.entitlementVerified == nil, leavesNil: self.entitlementLeaves == nil,
		censuses: map[string]economicProviderCandidateCensus{}, contracts: map[economicProviderCandidateContract]economicProviderCandidateMembership{}}
	self.providerCandidate = value
	return value, nil
}

// Every affected cache write captures its original value once, even if the
// same exact candidate is checked more than once before publication.
func (self *economicProviderCandidate) captureCensus(key string) {
	if self == nil {
		return
	}
	if _, exists := self.censuses[key]; exists {
		return
	}
	value := economicProviderCandidateCensus{}
	value.provider, value.providerPresent = self.view.providerCensuses[key]
	value.verified, value.verifiedPresent = self.view.entitlementVerified[key]
	value.leaves, value.leavesPresent = self.view.entitlementLeaves[key]
	self.censuses[key] = value
}

// A contract can already have a different admitted original. Its existing
// membership map and every unrelated census remain untouched on rollback.
func (self *economicProviderCandidate) captureProvider(key string, value *economicProviderAdmitted) {
	if self == nil || value == nil {
		return
	}
	self.captureCensus(key)
	for contractId := range value.NewContracts {
		contract := economicProviderContractKey{Domain: value.Domain, ContractId: contractId}
		entry := economicProviderCandidateContract{contract: contract, census: key}
		if _, exists := self.contracts[entry]; exists {
			continue
		}
		members, indexPresent := self.view.providerContracts[contract]
		_, censusPresent := members[key]
		self.contracts[entry] = economicProviderCandidateMembership{indexPresent: indexPresent, censusPresent: censusPresent}
	}
}

// Finish is idempotent so every early return and explicit refusal cleanup uses
// the same transaction. Commit is selected only after the candidate validates.
func (self *economicProviderCandidate) finish(commit bool) {
	if self == nil || self.closed {
		return
	}
	self.closed = true
	view := self.view
	view.providerCandidate = nil
	if commit {
		return
	}
	view.entries, view.bytes = self.entries, self.bytes
	for key, old := range self.censuses {
		if old.providerPresent {
			view.providerCensuses[key] = old.provider
		} else {
			delete(view.providerCensuses, key)
		}
		if old.verifiedPresent {
			view.entitlementVerified[key] = old.verified
		} else {
			delete(view.entitlementVerified, key)
		}
		if old.leavesPresent {
			view.entitlementLeaves[key] = old.leaves
		} else {
			delete(view.entitlementLeaves, key)
		}
	}
	for key, old := range self.contracts {
		if !old.indexPresent {
			delete(view.providerContracts, key.contract)
		} else if !old.censusPresent {
			delete(view.providerContracts[key.contract], key.census)
		}
	}
	if self.providerNil {
		view.providerCensuses = nil
	}
	if self.contractsNil {
		view.providerContracts = nil
	}
	if self.verifiedNil {
		view.entitlementVerified = nil
	}
	if self.leavesNil {
		view.entitlementLeaves = nil
	}
}
