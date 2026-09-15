// Original companion approvals retain their reviewed runtime provenance;
// carrying them does not grant authority to launch an older runtime.
package main

import (
	"errors"
	"strings"
)

// The companion source envelope first shipped with runtime455. Earlier
// runtime evidence uses the existing pre-companion plan and artifact readers.
// Callers authenticate this lock's canonical approval hash before admission.
func validateValidatorEvidenceHistoricalReleaseLock(lock *ReleaseLock) error {
	if lock == nil || lock.SchemaVersion != 1 || lock.Release != "1.0" {
		return errors.New("release lock schema or release is not the reviewed 1.0 release")
	}
	if lock.Runtime.SpecVersion == reviewedRuntimeSpecVersion {
		if err := validateReviewedRuntimeIdentity(lock); err != nil {
			return err
		}
	} else {
		runtime := lock.Runtime
		if runtime.SourceRepository != "https://github.com/RaoFoundation/subtensor" ||
			runtime.SourceTag != "" || runtime.SourceRefKind != "commit" ||
			runtime.SourceRefName != "67dcf7f791dc495064c293f080a0702cb433e51e" ||
			runtime.SourceCommit != "67dcf7f791dc495064c293f080a0702cb433e51e" ||
			runtime.SpecVersion != 455 || runtime.TransactionVersion != 1 || runtime.StateVersion != 1 ||
			!strings.EqualFold(runtime.CodeHash, "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a") ||
			!strings.EqualFold(runtime.MetadataHash, "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc") ||
			!strings.EqualFold(runtime.CompressedWasmSHA256, "0x232bfc0d65ec2dbe4280b152e23f13879df9692d2286dd08c6ba14483deee00f") ||
			runtime.UpstreamReleaseCallHash != "" || runtime.UpstreamReleaseTimepoint != "" {
			return errors.New("validator evidence archived runtime identity is not a reviewed companion release")
		}
	}
	return validateReleaseLockStaticFields(lock)
}
