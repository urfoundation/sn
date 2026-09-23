// Embedded contract evidence is shared by strict source observation and the
// retained-state recovery gate; neither path may omit a generated artifact.
package main

import (
	"errors"
	"fmt"
)

// Return only build identities embedded in the actual driver. Filesystem
// source, compiler tooling and dependency revisions are observed separately.
func generatedReleaseBuildObservation() map[string]string {
	return map[string]string{
		"reserve_sink_runtime_hash":                     ReserveSinkRuntimeBytecodeHash,
		"settlement_vault_runtime_hash":                 SettlementVaultRuntimeBytecodeHash,
		"coordinator_implementation_runtime_hash":       CoordinatorRuntimeBytecodeHash,
		"coordinator_proxy_runtime_hash":                ERC1967ProxyRuntimeBytecodeHash,
		"governance_drill_implementation_runtime_hash":  CoordinatorAdversaryRuntimeBytecodeHash,
		"precompile_probe_runtime_hash":                 SubnetProbeRuntimeBytecodeHash,
		"fleet_batcher_runtime_hash":                    FleetBatcherRuntimeBytecodeHash,
		"reserve_sink_artifact_hash":                    ReserveSinkFoundryArtifactHash,
		"settlement_vault_artifact_hash":                SettlementVaultFoundryArtifactHash,
		"coordinator_implementation_artifact_hash":      CoordinatorFoundryArtifactHash,
		"coordinator_proxy_artifact_hash":               ERC1967ProxyFoundryArtifactHash,
		"governance_drill_implementation_artifact_hash": CoordinatorAdversaryFoundryArtifactHash,
		"precompile_probe_artifact_hash":                SubnetProbeFoundryArtifactHash,
		"fleet_batcher_artifact_hash":                   FleetBatcherFoundryArtifactHash,
		"governance_drill_storage_layout_hash":          CoordinatorAdversaryStorageLayoutHash,
		"fleet_batcher_storage_layout_hash":             FleetBatcherStorageLayoutHash,
		"validator_evidence_runtime_hash":               ValidatorEvidenceRuntimeBytecodeHash,
		"validator_evidence_artifact_hash":              ValidatorEvidenceFoundryArtifactHash,
		"validator_evidence_storage_layout_hash":        ValidatorEvidenceStorageLayoutHash,
		"abi_hash":                                      generatedABIHash(),
		"coordinator_storage_layout_hash":               CoordinatorStorageLayoutHash,
	}
}

// A provisional driver may change its Go implementation while retaining the
// approved contract bytecode, ABI and compiler artifact identities exactly.
func validateGeneratedReleaseBuild(lock *ReleaseLock) error {
	if lock == nil {
		return errors.New("generated release build has no approved lock")
	}
	for key, expected := range generatedReleaseBuildObservation() {
		actual, err := lockString(lock.EVMBuild, key)
		if err != nil || actual != expected {
			return errors.Join(fmt.Errorf("retained release generated build %s differs from the running driver", key), err)
		}
	}
	artifact, err := currentValidatorEvidenceArtifact()
	if err != nil {
		return err
	}
	return validateValidatorEvidenceSourceArtifact(artifact)
}
