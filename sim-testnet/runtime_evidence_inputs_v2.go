//go:build linux || darwin

// Prepared signatures and one finalized boundary resolve the fixed simulator
// inputs. These local bytes are discovery only; actual setup postconditions
// and RunRelease independently authenticate both historical chains.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Explicit fresh provisioning accepts capacities and the complete source
// census, never fabricated file hashes or an implicit migration prefix.
func validateRuntimeEvidenceProvisionTemplateV2(cfg *HarnessConfig) error {
	if cfg == nil {
		return nil
	}
	if !cfg.ProvisionValidatorEvidenceV2 {
		if cfg.ValidatorEvidenceActivationGasUnits != 0 {
			return errors.New("activation gas requires explicit fresh evidence provisioning")
		}
		return nil
	}
	if cfg.ValidatorEvidenceActivationGasUnits == 0 {
		return errors.New("fresh evidence provisioning requires explicit activation gas units per fixed source action")
	}
	if err := validateSimulatorEvidenceV2Census(cfg); err != nil {
		return err
	}
	for _, configured := range cfg.ValidatorEvidenceV2 {
		for _, operator := range configured.Evidence.Operators {
			if operator != (validatorcomponent.ReleaseEvidenceV2OperatorConfig{NoID: operator.NoID}) {
				return errors.New("fresh validator evidence provisioning requires empty file references and scratch paths in the approved template")
			}
		}
	}
	return nil
}

// The approved template retains each exact byte/count limit and intent lifetime.
// Resolved file locations are deterministically reconstructed, not new policy.
func runtimeEvidenceTemplateV2(values []validatorcomponent.ReleaseValidatorEvidenceV2Config) []validatorcomponent.ReleaseValidatorEvidenceV2Config {
	result := slices.Clone(values)
	for index := range result {
		result[index].Evidence.Operators = slices.Clone(result[index].Evidence.Operators)
		for member := range result[index].Evidence.Operators {
			result[index].Evidence.Operators[member] = validatorcomponent.ReleaseEvidenceV2OperatorConfig{NoID: result[index].Evidence.Operators[member].NoID}
		}
	}
	return result
}

// File metadata cannot change a source identity, prefix or signed domain.
// Historical uid and checkpoint finality are separately authenticated online.
func validateRuntimeEvidencePreparedInputsV2(cfg *ResolvedConfig, plan *SetupPlan, roles *RoleSecrets, prepared *runtimeEvidenceActivationPreparedV2) error {
	if cfg == nil || cfg.Config == nil || plan == nil || plan.ValidatorEvidence == nil || prepared == nil {
		return errors.New("evidence setup input owners are incomplete")
	}
	if err := validateSimulatorEvidenceV2Census(cfg.Config); err != nil {
		return err
	}
	if prepared.Schema != "urnetwork-sim-evidence-activation-prepared-v2" || prepared.PlanHash != plan.PlanHash || prepared.ConfigHash != cfg.ConfigHash || prepared.PolicyHash != cfg.PolicyHash ||
		prepared.Epoch == 0 || prepared.Epoch == ^uint64(0) || prepared.Evm.Number == 0 || prepared.Native.Number == 0 || len(prepared.Members) != cfg.Config.Topology.Validators*cfg.Config.Topology.Operators {
		return errors.New("evidence setup preparation differs from the approved census or policy")
	}
	policy, err := decodeHex32("evidence setup policy", cfg.PolicyHash)
	if err != nil {
		return err
	}
	evmHash, err := decodeHex32("evidence setup Evm snapshot", prepared.Evm.Hash)
	if err != nil {
		return err
	}
	nativeHash, err := decodeHex32("evidence setup native snapshot", prepared.Native.Hash)
	if err != nil {
		return err
	}
	for index, member := range prepared.Members {
		validatorId, noId := uint64(index/cfg.Config.Topology.Operators+1), uint64(index%cfg.Config.Topology.Operators+1)
		if member.ValidatorId != validatorId || member.NoId != noId {
			return errors.New("evidence setup member order or census differs")
		}
		hotkey, key, err := runtimeEvidenceActivationKeysV2(roles, validatorId, noId)
		if err != nil {
			return err
		}
		companion := plan.ValidatorEvidence
		expected := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: testnetChainID, GenesisHash: [32]byte(companion.GenesisHash), Netuid: cfg.Netuid,
			Coordinator: [20]byte(companion.Coordinator), SettlementVault: [20]byte(companion.SettlementVault), DeploymentIDHash: [32]byte(companion.DeploymentIDHash), PolicyHash: policy, Epoch: prepared.Epoch},
			Hotkey: hotkey.PublicKey(), NoID: noId, VPK: [32]byte(key[ed25519.SeedSize:]), FirstSequence: 1,
			NativeBlock: prepared.Native.Number, NativeHash: nativeHash, EVMBlock: prepared.Evm.Number, EVMHash: evmHash}
		if err := member.Activation.Verify(expected, member.VpkSignature, member.HotkeySignature); err != nil {
			return err
		}
	}
	return nil
}

// Build the exact five files for every source without touching the filesystem.
// An immutable completion locator stores only a deterministic public boundary.
func runtimeEvidenceFixedInputsV2(cfg *ResolvedConfig, plan *SetupPlan, stateDir string, roles *RoleSecrets, prepared *runtimeEvidenceActivationPreparedV2, completed *runtimeEvidenceActivationCompletedV2) ([]validatorcomponent.ReleaseValidatorEvidenceV2Config, map[string][]byte, error) {
	if err := validateRuntimeEvidencePreparedInputsV2(cfg, plan, roles, prepared); err != nil {
		return nil, nil, err
	}
	if completed == nil || completed.Schema != "urnetwork-sim-evidence-activation-completed-v2" || completed.PlanHash != plan.PlanHash || completed.Boundary.Number <= prepared.Evm.Number {
		return nil, nil, errors.New("evidence setup completion has no later public boundary")
	}
	hash, err := decodeHex32("evidence setup boundary", completed.Boundary.Hash)
	if err != nil {
		return nil, nil, err
	}
	result := runtimeEvidenceTemplateV2(cfg.Config.ValidatorEvidenceV2)
	inputs := map[string][]byte{}
	for _, member := range prepared.Members {
		domain, err := member.Activation.EvidenceDomain()
		if err != nil {
			return nil, nil, err
		}
		value := validatorcomponent.ReleaseEvidenceV2ActivationContext{Schema: validatorcomponent.ReleaseEvidenceV2ActivationContextSchema, Activation: member.Activation,
			InitialCut: validatorcomponent.AttemptCutV2Context{Identity: validatorcomponent.AttemptLedgerIdentity{DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: testnetChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, ValidatorID: member.ValidatorId, ValidatorUID: member.ValidatorUid, NoID: member.NoId, ValidatorVPK: fmt.Sprintf("0x%x", member.Activation.VPK)},
				Activation:    validatorcomponent.AttemptCutV2Activation{Domain: domain, Hotkey: member.Activation.Hotkey, FirstSequence: 1, PriorRoot: fmt.Sprintf("0x%x", [32]byte{})},
				Boundary:      validatorcomponent.AttemptBoundary{SettlementEpoch: prepared.Epoch, EVMBlock: completed.Boundary.Number, EVMBlockHash: common.Hash(hash).Hex()},
				FirstSequence: 1, EgressFirstSequence: 1, EgressGeneration: 1, PriorRoot: fmt.Sprintf("0x%x", [32]byte{})},
			ValidatorUID: member.ValidatorUid, Journal: [20]byte(plan.ValidatorEvidence.Address), RuntimeHash: [32]byte(plan.ValidatorEvidence.RuntimeCodeHash), ObservedEVMBlock: completed.Boundary.Number, ObservedEVMHash: hash}
		configured := &result[member.ValidatorId-1].Evidence
		contextBytes, err := value.CanonicalJSON(configured.Bounds.Cut.MaxHeaderBytes)
		if err != nil {
			return nil, nil, err
		}
		historyBytes, err := (validatorcomponent.ReleaseEvidenceV2ActivationHistory{Schema: validatorcomponent.ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}).CanonicalJSON(configured.Bounds.MaxHistoryBytes)
		if err != nil {
			return nil, nil, err
		}
		activationBytes, err := member.Activation.Payload()
		if err != nil {
			return nil, nil, err
		}
		paths, replay, seal := runtimeEvidenceV2Paths(stateDir, int(member.ValidatorId), member.NoId)
		files := make([]validatorcomponent.ReleaseEvidenceV2File, len(paths))
		for index, data := range [][]byte{activationBytes, member.VpkSignature, member.HotkeySignature, contextBytes, historyBytes} {
			if uint64(len(data)) > runtimeEvidenceV2ReferenceLimit(configured.Bounds, index) {
				return nil, nil, errors.New("evidence setup fixed input exceeds its explicit bound")
			}
			inputs[paths[index]] = slices.Clone(data)
			files[index] = validatorcomponent.ReleaseEvidenceV2File{Path: paths[index], Bytes: uint64(len(data)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(data))}
		}
		configured.Operators[member.NoId-1] = validatorcomponent.ReleaseEvidenceV2OperatorConfig{NoID: member.NoId, Activation: files[0], VPKSignature: files[1], HotkeySignature: files[2], Context: files[3], History: files[4], ReplayScratchRoot: replay, SealScratchRoot: seal}
	}
	return result, inputs, nil
}

// Rendering and inventory hashing both resolve the same immutable output.
// This never mutates the approved template, creates inputs, or trusts claimed
// hashes; every exact file is read under its original role-specific bound.
func runtimeEvidenceV2ResolvedConfig(cfg *ResolvedConfig, stateDir string) (*ResolvedConfig, error) {
	if cfg == nil || cfg.Config == nil {
		return nil, errors.New("evidence setup configuration is absent")
	}
	if !cfg.Config.ProvisionValidatorEvidenceV2 {
		return cfg, nil
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	var prepared runtimeEvidenceActivationPreparedV2
	encoded, err := readRuntimeEvidenceSetupV2(context.Background(), filepath.Join(stateDir, "evidence-v2-setup", "prepared.json"), limit, &prepared)
	if err != nil {
		return nil, err
	}
	var completed runtimeEvidenceActivationCompletedV2
	if _, err := readRuntimeEvidenceSetupV2(context.Background(), filepath.Join(stateDir, "evidence-v2-setup", "completed.json"), limit, &completed); err != nil {
		return nil, err
	}
	if completed.PreparedHash != fmt.Sprintf("0x%x", sha256.Sum256(encoded)) {
		return nil, errors.New("evidence setup completion references different retained signatures")
	}
	if prepared.PlanHash != plan.PlanHash {
		entries, err := readJournalEntries(stateDir)
		if err != nil {
			return nil, err
		}
		plan, err = runtimeEvidenceSetupSourcePlanV2(cfg, plan, stateDir, roles, &prepared, encoded, &completed, entries)
		if err != nil {
			return nil, err
		}
	}
	values, inputs, err := runtimeEvidenceFixedInputsV2(cfg, plan, stateDir, roles, &prepared, &completed)
	if err != nil {
		return nil, err
	}
	for _, configured := range values {
		for _, operator := range configured.Evidence.Operators {
			for index, reference := range operator.Files() {
				actual, err := validatorcomponent.ReadReleaseEvidenceV2File(context.Background(), reference, runtimeEvidenceV2ReferenceLimit(configured.Evidence.Bounds, index))
				if err != nil || !bytes.Equal(actual, inputs[reference.Path]) {
					return nil, errors.Join(errors.New("evidence setup fixed input differs from its authenticated role"), err)
				}
			}
		}
	}
	resolved, copied := *cfg, *cfg.Config
	copied.ValidatorEvidenceV2 = values
	resolved.Config = &copied
	return &resolved, nil
}
