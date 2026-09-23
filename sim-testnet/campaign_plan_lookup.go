// One recovery traversal authenticates each unique archived plan once. The
// decoded approval and exact raw-byte digest are private to that traversal;
// file/directory witnesses fence every reuse and the traversal's final return.
package main

import (
	"errors"
	"reflect"
)

// Entries never retain a second large raw buffer. Lineage validation borrows
// the parsed value read-only; a final consumer may take it when traversal ends.
type scenarioCampaignPlanProof struct {
	plan      *SetupPlan
	rawHash   string
	witnesses []fleetCensusFileWitness
}

// A single synchronous traversal owns this lookup; no global cache or lock can
// let another caller replace its approval context. Over-budget misses stay cold.
type scenarioCampaignPlanLookup struct {
	stateDir         string
	proofKVs         map[string]scenarioCampaignPlanProof
	retainedBytes    uint64
	afterReadForTest func(string)
}

// Directory identity and leaf ctime reject substitution and restored-mtime
// writes. Unavailable metadata disables reuse, never the full strict reader.
func (self *scenarioCampaignPlanLookup) witnesses(hash string) ([]fleetCensusFileWitness, bool) {
	var result []fleetCensusFileWitness
	for _, item := range []struct {
		name      string
		directory bool
	}{{name: ".", directory: true}, {name: "plans", directory: true}, {name: "plans/" + stringsTrim0x(hash) + ".json"}} {
		witness, ok := scenarioCampaignRecoverySourceWitness(self.stateDir, item.name, item.directory)
		if !ok {
			return nil, false
		}
		result = append(result, witness)
	}
	return result, true
}

// Warm reads preserve the exact original digest; they never manufacture a
// hash from the decoded value or accept changed bytes under an unchanged name.
func (self *scenarioCampaignPlanLookup) read(stateDir, hash string) (*SetupPlan, string, error) {
	if self == nil || self.stateDir != stateDir || !validCanonicalHashHex(hash) {
		return nil, "", errors.New("campaign plan lookup has another source owner or invalid hash")
	}
	before, safeBefore := self.witnesses(hash)
	if proof, exists := self.proofKVs[hash]; exists {
		if !safeBefore || !reflect.DeepEqual(before, proof.witnesses) {
			return nil, "", errors.New("campaign archived plan changed during its authenticated traversal")
		}
		return proof.plan, proof.rawHash, nil
	}
	plan, raw, err := readScenarioSuccessionPlan(stateDir, hash)
	if err != nil {
		return nil, "", err
	}
	rawHash := bytesSHA256(raw)
	if self.afterReadForTest != nil {
		self.afterReadForTest(hash)
	}
	after, safeAfter := self.witnesses(hash)
	if safeBefore && (!safeAfter || !reflect.DeepEqual(before, after)) {
		return nil, "", errors.New("campaign archived plan changed while it was authenticated")
	}
	if safeBefore && safeAfter && self.retainedBytes <= maximumCampaignEvidenceAggregateBytes && uint64(len(raw)) <= maximumCampaignEvidenceAggregateBytes-self.retainedBytes {
		if self.proofKVs == nil {
			self.proofKVs = map[string]scenarioCampaignPlanProof{}
		}
		self.proofKVs[hash] = scenarioCampaignPlanProof{plan: plan, rawHash: rawHash, witnesses: after}
		self.retainedBytes += uint64(len(raw))
	}
	return plan, rawHash, nil
}

// A plan used early in a traversal must still exist unchanged after all later
// source checks. This also catches directory replacement between two lookups.
func (self *scenarioCampaignPlanLookup) check() error {
	if self == nil {
		return errors.New("campaign plan lookup owner is absent")
	}
	for hash, proof := range self.proofKVs {
		actual, safe := self.witnesses(hash)
		if !safe || !reflect.DeepEqual(actual, proof.witnesses) {
			return errors.New("campaign archived plan changed before traversal completion")
		}
	}
	return nil
}
