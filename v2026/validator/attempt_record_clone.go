// Ledger clones copy only owned mutable fields. No serialization, signature
// check or durable-file read is replaced by a shared or memoized record.
package validator

import (
	"bytes"
	"slices"
	"unicode/utf8"
)

// Preserve the original Json round trip's replacement of each invalid UTF-8
// byte. Valid immutable strings require no allocation or transformation.
func attemptCloneString(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}

// All slices/pointers are detached, including nil versus non-nil empty
// owners. Value fields retain their original widths and exact representation.
func cloneAttemptRecord(record AttemptRecord) (AttemptRecord, error) {
	cloned := record
	cloned.Schema = attemptCloneString(record.Schema)
	cloned.Identity.DeploymentID = attemptCloneString(record.Identity.DeploymentID)
	cloned.Identity.GenesisHash = attemptCloneString(record.Identity.GenesisHash)
	cloned.Identity.ValidatorVPK = attemptCloneString(record.Identity.ValidatorVPK)
	cloned.PreviousHash = attemptCloneString(record.PreviousHash)
	cloned.Boundary.EVMBlockHash = attemptCloneString(record.Boundary.EVMBlockHash)
	cloned.Disposition = attemptCloneString(record.Disposition)
	cloned.RecordHash = attemptCloneString(record.RecordHash)
	cloned.ServerNonce = bytes.Clone(record.ServerNonce)
	cloned.VPK = bytes.Clone(record.VPK)
	cloned.Signature = bytes.Clone(record.Signature)
	cloned.Assignments = slices.Clone(record.Assignments)
	for index := range cloned.Assignments {
		assignment := &cloned.Assignments[index]
		assignment.Trail = slices.Clone(assignment.Trail)
		assignment.AssignMessage = bytes.Clone(assignment.AssignMessage)
		assignment.AssignSignature = bytes.Clone(assignment.AssignSignature)
		assignment.Binding.FleetID = attemptCloneString(assignment.Binding.FleetID)
		assignment.Binding.Hotkey = attemptCloneString(assignment.Binding.Hotkey)
	}
	if record.Proof != nil {
		proof := *record.Proof
		proof.ServerNonce = bytes.Clone(record.Proof.ServerNonce)
		proof.Vpk = bytes.Clone(record.Proof.Vpk)
		proof.Hops = slices.Clone(record.Proof.Hops)
		proof.FinalSig = bytes.Clone(record.Proof.FinalSig)
		proof.VerifierSig = bytes.Clone(record.Proof.VerifierSig)
		proof.FinalDigest = bytes.Clone(record.Proof.FinalDigest)
		proof.VpkSig = bytes.Clone(record.Proof.VpkSig)
		proof.PathId = bytes.Clone(record.Proof.PathId)
		cloned.Proof = &proof
	}
	return cloned, nil
}
