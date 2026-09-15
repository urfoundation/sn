package main

// Rendering preserves exact configured bounds and references; it neither
// creates activation evidence nor selects a chain storage/publication route.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/protocol"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// The validator routing key and complete pair census precede any indexing.
func validateSimulatorEvidenceV2Census(config *HarnessConfig) error {
	if config == nil || config.Topology.Validators < 1 || config.Topology.Operators < 1 || len(config.ValidatorEvidenceV2) != config.Topology.Validators {
		return errors.New("runtime rendering requires explicit validator_evidence_v2 for every validator")
	}
	for index, validator := range config.ValidatorEvidenceV2 {
		if validator.ValidatorID != uint64(index+1) || validator.Evidence.Schema != validatorpkg.ReleaseEvidenceV2ConfigSchema || len(validator.Evidence.Operators) != config.Topology.Operators {
			return errors.New("validator_evidence_v2 validator/operator census must be exact and sorted")
		}
		if err := validator.Evidence.Bounds.Validate(uint64(config.Topology.Operators)); err != nil {
			return err
		}
		for index, operator := range validator.Evidence.Operators {
			if operator.NoID != uint64(index+1) {
				return errors.New("validator_evidence_v2 operator census must be exact and sorted")
			}
		}
	}
	return nil
}

// The immutable identity covers the complete original values. File inventory
// hashing additionally binds actual referenced bytes, modes and rendered YAML.
func runtimeEvidenceV2Identity(cfg *ResolvedConfig) (string, error) {
	if cfg == nil || cfg.Config == nil {
		return "", errors.New("runtime evidence identity has no configuration")
	}
	if cfg.Config.ProvisionValidatorEvidenceV2 {
		return canonicalHashHex(struct {
			Provision  bool
			Validators []validatorpkg.ReleaseValidatorEvidenceV2Config
		}{Provision: true, Validators: runtimeEvidenceTemplateV2(cfg.Config.ValidatorEvidenceV2)})
	}
	return canonicalHashHex(cfg.Config.ValidatorEvidenceV2)
}

// References keep their explicit independent bound even when the manifest
// is rebuilt. A rehashed manifest cannot authorize changed referenced bytes.
func runtimeConfigInputDigest(cfg *ResolvedConfig, path string) (string, os.FileMode, error) {
	for _, validator := range cfg.Config.ValidatorEvidenceV2 {
		for _, operator := range validator.Evidence.Operators {
			for index, reference := range operator.Files() {
				if reference.Path == path {
					if _, err := validatorpkg.ReadReleaseEvidenceV2File(context.Background(), reference, runtimeEvidenceV2ReferenceLimit(validator.Evidence.Bounds, index)); err != nil {
						return "", 0, err
					}
					return "sha256:" + strings.TrimPrefix(reference.SHA256, "0x"), 0o600, nil
				}
			}
		}
	}
	return runtimeConfigFileDigest(path)
}

// Role widths are protocol constants; variable inputs retain caller policy.
func runtimeEvidenceV2ReferenceLimit(bounds validatorpkg.ReleaseEvidenceV2Bounds, index int) uint64 {
	switch index {
	case 0:
		return uint64(protocol.ValidatorEvidenceActivationPayloadSize)
	case 1, 2:
		return 64
	case 3:
		return bounds.Cut.MaxHeaderBytes
	case 4:
		return bounds.MaxHistoryBytes
	default:
		return 0
	}
}

// Simulator-owned references have fixed role paths, outside durable state and
// per-operation scratch. These names are not capacities or activation defaults.
func runtimeEvidenceV2Paths(stateDir string, validatorID int, noID uint64) ([]string, string, string) {
	root := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID))
	input := filepath.Join(root, "evidence-v2", fmt.Sprintf("no-%d", noID))
	files := []string{}
	for _, name := range []string{"activation.payload", "vpk.signature", "hotkey.signature", "context.json", "history.json"} {
		files = append(files, filepath.Join(input, name))
	}
	scratch := filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", noID))
	return files, filepath.Join(scratch, "replay"), filepath.Join(scratch, "seal")
}

// All pairs and every exact reference are checked before the first renderer
// mutation. This confirms bytes only, never historical eligibility or finality.
func preflightRuntimeEvidenceV2(cfg *ResolvedConfig, stateDir string) error {
	resolved, resolveErr := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if resolveErr != nil {
		return fmt.Errorf("evidence reference checks blocked by resolved activation inputs: %w", resolveErr)
	}
	cfg = resolved
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.Validators < 1 || cfg.Config.Topology.Operators < 1 || len(cfg.Config.ValidatorEvidenceV2) != cfg.Config.Topology.Validators {
		return errors.New("runtime rendering requires explicit validator_evidence_v2 for every validator")
	}
	if err := validatorpkg.ValidateReleaseEvidenceV2Path(stateDir); err != nil {
		return err
	}
	var failures []error
	for index, configured := range cfg.Config.ValidatorEvidenceV2 {
		validatorID := index + 1
		if configured.ValidatorID != uint64(validatorID) {
			failures = append(failures, fmt.Errorf("validator %d references blocked by non-canonical validator identity", validatorID))
			continue
		}
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state")
		operators := make([]validatorpkg.OperatorConfig, cfg.Config.Topology.Operators)
		for operator := range operators {
			state := filepath.Join(root, "operators", fmt.Sprintf("no-%d", operator+1))
			operators[operator] = validatorpkg.OperatorConfig{NoID: uint64(operator + 1), StateDir: state, NetworkJWTFile: filepath.Join(state, "network.jwt"), ClientJWTFile: filepath.Join(state, "client.jwt"), ClientKeySeedFile: filepath.Join(state, "client.key")}
		}
		// Invalid bounds, role census or overlapping ownership never authorize
		// a file read. Other independently configured validators still run.
		if err := configured.Evidence.Validate(operators, root, filepath.Join(stateDir, "secrets", fmt.Sprintf("validator-%d-hotkey.seed", validatorID))); err != nil {
			failures = append(failures, fmt.Errorf("validator %d references blocked by evidence configuration: %w", validatorID, err))
			continue
		}
		for _, operator := range configured.Evidence.Operators {
			paths, replay, seal := runtimeEvidenceV2Paths(stateDir, validatorID, operator.NoID)
			if operator.ReplayScratchRoot != replay || operator.SealScratchRoot != seal {
				failures = append(failures, fmt.Errorf("validator %d no_id %d runtime evidence scratch owner differs from its validator/operator pair", validatorID, operator.NoID))
			}
			for index, reference := range operator.Files() {
				if reference.Path != paths[index] {
					failures = append(failures, fmt.Errorf("validator %d no_id %d evidence reference %d blocked by incorrect role path", validatorID, operator.NoID, index))
					continue
				}
				if _, err := validatorpkg.ReadReleaseEvidenceV2File(context.Background(), reference, runtimeEvidenceV2ReferenceLimit(configured.Evidence.Bounds, index)); err != nil {
					failures = append(failures, fmt.Errorf("validator %d no_id %d evidence reference %d: %w", validatorID, operator.NoID, index, err))
				}
			}
		}
	}
	return errors.Join(failures...)
}
