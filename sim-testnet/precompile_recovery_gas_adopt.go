// Adoption runs under the existing exclusive writer and closes the crash
// window between persisting signed bytes and appending their journal record.
package main

import (
	"errors"
	"os"
	"path/filepath"
)

// The published revision is immutable. A crash leaves either the exact old
// evidence or the complete new authority/action pair, and both are resumable.
func (self *Executor) adoptPrecompileRecoveryGasRevision(evidence *PrecompileConformanceEvidence) error {
	if evidence.Recovery.Authorization.Request.GasRevision != nil {
		if self.journal == nil {
			return errors.New("probe gas revision has no retained journal")
		}
		return validatePrecompileRecoveryGasRevisionJournal(self.plan, evidence, self.journal.Entries())
	}
	var authorization PrecompileRecoveryAuthorization
	path := filepath.Join(self.stateDir, precompileRecoveryGasRevisionFilename)
	if err := validatePrecompileRecoveryArtifactPath(self.stateDir, path); err != nil {
		return err
	}
	if err := readJSONFile(path, &authorization); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if self.journal == nil || self.journal.file == nil || self.journal.lock == nil {
		return errors.New("probe gas revision adoption requires exclusive journal ownership")
	}
	if err := validatePrecompileRecoveryPlan(self.plan, evidence, &authorization); err != nil {
		return err
	}
	revision := authorization.Request.GasRevision
	if revision == nil || evidence.EvidenceHash != revision.Evidence.EvidenceHash {
		return errors.New("probe gas revision no longer matches the exact pending evidence")
	}
	current := *evidence
	current.EvidenceHash = ""
	hash, err := canonicalHashHex(current)
	if err != nil || hash != revision.Evidence.EvidenceHash {
		return errors.New("probe gas revision pending evidence bytes changed")
	}
	entries := self.journal.Entries()
	if err := requireUnsignedPrecompileRecovery(self.stateDir, evidence, entries); err != nil {
		return err
	}
	current = *evidence
	current.Recovery = &PrecompileRecoveryEvidence{Authorization: authorization, Steps: append([]PrecompileRecoveryStep(nil), evidence.Recovery.Steps...)}
	current.Recovery.Steps[0].Action, _, err = precompileRecoveryAction(&authorization, 0, current.Recovery.Steps[0])
	if err != nil {
		return err
	}
	if err := validatePrecompileRecoveryGasRevisionJournal(self.plan, &current, entries); err != nil {
		return err
	}
	if _, _, _, err := precompileRecoveryPositions(&current); err != nil {
		return err
	}
	if err := self.validatePrecompileRecoveryBudget(&current); err != nil {
		return err
	}
	if err := writePrecompileEvidence(self.stateDir, &current); err != nil {
		return err
	}
	*evidence = current
	return nil
}
