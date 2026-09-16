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
		var commit, codeHash, metadataHash, compressedSha256 string
		switch runtime.SpecVersion {
		case 455:
			commit = "67dcf7f791dc495064c293f080a0702cb433e51e"
			codeHash = "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a"
			metadataHash = "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"
			compressedSha256 = "0x232bfc0d65ec2dbe4280b152e23f13879df9692d2286dd08c6ba14483deee00f"
		case 458:
			commit = "a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7"
			codeHash = "0x2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a"
			metadataHash = "0x040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d"
			compressedSha256 = "0xd763c0210bbd113c065a4e8d538cdd3f5e9b40ba259a5136b77e0a495c364241"
		case 459:
			commit = "70378404b56c12a85bc8cd163aca2f32cf4d1b80"
			codeHash = "0x558275958401c026fa4a4159466d49eabd08c761f0c801390593fcba91dee69b"
			metadataHash = "0xcf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef"
			compressedSha256 = "0xc78bef5489149655254d5fb01a0e8c5c61846b0b322a54cb9ca2c86a14df8284"
		case 460:
			commit = "8d5f20ec1a5e5d90295d43046dacdefc54aaed06"
			codeHash = "0xa2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d"
			metadataHash = "0x98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c"
			compressedSha256 = "0x12b9affec176cbb79c7e5db253d3d0e47f4cb575ce4501ef10de6578afbb817f"
		default:
			return errors.New("validator evidence archived runtime version is not a reviewed companion release")
		}
		if runtime.SourceRepository != "https://github.com/RaoFoundation/subtensor" ||
			runtime.SourceTag != "" || runtime.SourceRefKind != "commit" ||
			runtime.SourceRefName != commit || runtime.SourceCommit != commit ||
			runtime.TransactionVersion != 1 || runtime.StateVersion != 1 ||
			!strings.EqualFold(runtime.CodeHash, codeHash) ||
			!strings.EqualFold(runtime.MetadataHash, metadataHash) ||
			!strings.EqualFold(runtime.CompressedWasmSHA256, compressedSha256) ||
			runtime.UpstreamReleaseCallHash != "" || runtime.UpstreamReleaseTimepoint != "" {
			return errors.New("validator evidence archived runtime identity is not a reviewed companion release")
		}
	}
	return validateReleaseLockStaticFields(lock)
}
