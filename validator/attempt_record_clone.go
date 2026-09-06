package validator

// Ownership copies avoid serializing authenticated records a second time.
// Invalid UTF-8 retains the original JSON roundtrip's exact replacement rules.

import (
	"encoding/json"
	"slices"
	"unicode/utf8"
)

// Call-local observation sits at the actual compatibility serialization boundary;
// production supplies no callback and introduces no shared instrumentation.
type attemptRecordCloneWork struct {
	serialized func()
}

// Every string field must survive JSON unchanged before typed copying is safe.
func attemptRecordCloneStringsValid(record *AttemptRecord) bool {
	if !utf8.ValidString(record.Schema) ||
		!utf8.ValidString(record.Identity.DeploymentID) ||
		!utf8.ValidString(record.Identity.GenesisHash) ||
		!utf8.ValidString(record.Identity.ValidatorVPK) ||
		!utf8.ValidString(record.PreviousHash) ||
		!utf8.ValidString(record.Boundary.EVMBlockHash) ||
		!utf8.ValidString(record.Disposition) ||
		!utf8.ValidString(record.RecordHash) {
		return false
	}
	for index := range record.Assignments {
		binding := &record.Assignments[index].Binding
		if !utf8.ValidString(binding.FleetID) || !utf8.ValidString(binding.Hotkey) {
			return false
		}
	}
	return true
}

// Copies each mutable owner, including nonnil empty slices and the proof
// pointer. This is not semantic validation and changes no signed or JSONL work.
func (self attemptRecordCloneWork) clone(record AttemptRecord) (AttemptRecord, error) {
	if attemptRecordCloneStringsValid(&record) {
		cloned := record
		cloned.ServerNonce = slices.Clone(record.ServerNonce)
		cloned.VPK = slices.Clone(record.VPK)
		cloned.Signature = slices.Clone(record.Signature)
		cloned.Assignments = slices.Clone(record.Assignments)
		for index := range cloned.Assignments {
			cloned.Assignments[index].Trail = slices.Clone(record.Assignments[index].Trail)
			cloned.Assignments[index].AssignMessage = slices.Clone(record.Assignments[index].AssignMessage)
			cloned.Assignments[index].AssignSignature = slices.Clone(record.Assignments[index].AssignSignature)
		}
		if record.Proof != nil {
			proof := *record.Proof
			proof.ServerNonce = slices.Clone(record.Proof.ServerNonce)
			proof.Vpk = slices.Clone(record.Proof.Vpk)
			proof.Hops = slices.Clone(record.Proof.Hops)
			proof.FinalSig = slices.Clone(record.Proof.FinalSig)
			proof.VerifierSig = slices.Clone(record.Proof.VerifierSig)
			proof.FinalDigest = slices.Clone(record.Proof.FinalDigest)
			proof.VpkSig = slices.Clone(record.Proof.VpkSig)
			proof.PathId = slices.Clone(record.Proof.PathId)
			cloned.Proof = &proof
		}
		return cloned, nil
	}
	if self.serialized != nil {
		self.serialized()
	}
	encoded, err := json.Marshal(&record)
	if err != nil {
		return AttemptRecord{}, err
	}
	var cloned AttemptRecord
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return AttemptRecord{}, err
	}
	return cloned, nil
}
