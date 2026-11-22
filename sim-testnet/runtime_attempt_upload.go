package main

// Admission and rendering use the same explicit server-owned quota schema.
// A storage budget is necessary capacity, not validator eligibility or a
// resilience guarantee against many independently authenticated accounts.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/server/v2026/model"
	"gopkg.in/yaml.v3"
)

// Operator settings are small control input, independent of artifact capacity.
const maximumRuntimeAttemptUploadConfigBytes = 1024 * 1024

// Copy finite limits before renderer I/O. No vault profile fallback, derived
// default or alias to a mutable caller budget enters the operator st.yml map.
func runtimeAttemptUploadBudget(cfg *ResolvedConfig) (model.StAttemptUploadBudget, error) {
	if cfg == nil || cfg.Config == nil || cfg.Config.Artifacts.AttemptUpload == nil {
		return model.StAttemptUploadBudget{}, errors.New("runtime attempt upload requires explicit artifacts.attempt_upload limits")
	}
	budget := *cfg.Config.Artifacts.AttemptUpload
	if err := budget.Validate(); err != nil {
		return model.StAttemptUploadBudget{}, fmt.Errorf("runtime attempt upload capacity: %w", err)
	}
	if len(cfg.Config.ValidatorEvidenceV2) != 0 {
		if err := validateRuntimeClientKeyUploadCapacity(cfg, budget); err != nil {
			return model.StAttemptUploadBudget{}, err
		}
	}
	return budget, nil
}

// Bind the actual supplied values separately from the cached source hash.
// Omission preserves historical planning/manifest wire shape, never launch
// admission: RenderRuntimeConfigs requires runtimeAttemptUploadBudget first.
func runtimeAttemptUploadIdentity(cfg *ResolvedConfig) (string, error) {
	if cfg == nil || cfg.Config == nil {
		return "", errors.New("runtime attempt upload identity has no configuration")
	}
	if cfg.Config.Artifacts.AttemptUpload == nil {
		if len(cfg.Config.Artifacts.ReservedAttemptUploads) != 0 {
			return "", errors.New("reserved upload identity has no ordinary capacity owner")
		}
		return "", nil
	}
	budget, err := runtimeAttemptUploadBudget(cfg)
	if err != nil {
		return "", err
	}
	if len(cfg.Config.Artifacts.ReservedAttemptUploads) == 0 {
		return canonicalHashHex(budget)
	}
	if err := validateRuntimeReservedAttemptUploadCensus(cfg.Config); err != nil {
		return "", err
	}
	return canonicalHashHex(struct {
		Ordinary model.StAttemptUploadBudget `json:"ordinary"`
		Reserved any                         `json:"reserved"`
	}{Ordinary: budget, Reserved: cfg.Config.Artifacts.ReservedAttemptUploads})
}

// Original nodes must spell each finite counter exactly once. Aliases, merge
// keys, fractional scalars and decimal-to-float conversions are not capacity.
func runtimeAttemptUploadNode(node *yaml.Node) (model.StAttemptUploadBudget, error) {
	var budget model.StAttemptUploadBudget
	if node == nil || node.Kind != yaml.MappingNode || node.Tag != "!!map" || len(node.Content) != 8 {
		return budget, errors.New("attempt upload requires exactly four explicit integer limits")
	}
	// The real server budget owns scalar and field admission for both users.
	if err := node.Decode(&budget); err != nil {
		return model.StAttemptUploadBudget{}, err
	}
	if err := budget.Validate(); err != nil {
		return model.StAttemptUploadBudget{}, err
	}
	return budget, nil
}

// Strict harness decoding already checks unrelated keys. This additionally
// validates original quota nodes before YAML coercion can erase their spelling.
func validateRuntimeAttemptUploadDocument(wire []byte) error {
	root, err := runtimeAttemptUploadYAML(wire)
	if err != nil {
		return err
	}
	var configured *yaml.Node
	for index := 0; index < len(root.Content); index += 2 {
		if root.Content[index].Value == "<<" {
			return errors.New("attempt upload configuration cannot inherit merged capacity")
		}
		if root.Content[index].Value != "artifacts" {
			continue
		}
		artifacts := root.Content[index+1]
		if artifacts.Kind != yaml.MappingNode {
			return errors.New("attempt upload artifacts must be an explicit mapping")
		}
		for field := 0; field < len(artifacts.Content); field += 2 {
			if artifacts.Content[field].Value == "<<" {
				return errors.New("attempt upload artifacts cannot inherit merged capacity")
			}
			if artifacts.Content[field].Value == "attempt_upload" {
				configured = artifacts.Content[field+1]
			}
		}
	}
	if configured != nil {
		_, err := runtimeAttemptUploadNode(configured)
		return err
	}
	return nil
}

// One complete mapping document is retained; extra YAML documents cannot
// create an interpretation different from the bytes bound into the manifest.
func runtimeAttemptUploadYAML(wire []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(wire))
	var document, trailing yaml.Node
	if err := decoder.Decode(&document); err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("attempt upload configuration requires one YAML mapping")
	}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("attempt upload configuration contains trailing YAML")
	}
	return document.Content[0], nil
}

// Quota inputs are checked and hashed from the same actual owned descriptor.
// Rehashing a changed st.yml cannot authorize capacity different from the
// approved harness. Other runtime inputs retain their existing verification.
func runtimeManifestInputDigest(cfg *ResolvedConfig, stateDir, relative string) (string, os.FileMode, error) {
	if strings.Contains(relative, "/evidence-v2/") || strings.HasPrefix(relative, "evidence-v2-setup/") {
		resolved, resolveErr := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
		if resolveErr != nil {
			return "", 0, resolveErr
		}
		cfg = resolved
	}
	if cfg != nil && cfg.Config != nil && cfg.Config.Artifacts.AttemptUpload != nil {
		for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
			path := filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("operator-%d", operator), "vault", "st.yml"))
			if relative == path {
				return runtimeAttemptUploadConfigDigest(cfg, stateDir, relative)
			}
		}
	}
	return runtimeConfigInputDigest(cfg, filepath.Join(stateDir, filepath.FromSlash(relative)))
}

// Existing no-follow directory/leaf acquisition also refuses FIFOs before a
// blocking open. Only this small control document is retained in memory.
func runtimeAttemptUploadConfigDigest(cfg *ResolvedConfig, stateDir, relative string) (result string, mode os.FileMode, resultErr error) {
	want, err := runtimeAttemptUploadBudget(cfg)
	if err != nil {
		return "", 0, err
	}
	file, err := openFinalCollectedFile(stateDir, filepath.FromSlash(relative))
	if err != nil {
		return "", 0, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			result, mode = "", 0
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 0 || info.Size() > maximumRuntimeAttemptUploadConfigBytes {
		return "", 0, stateMismatchError(err, "runtime attempt upload config is not a bounded private regular file")
	}
	wire, err := io.ReadAll(io.LimitReader(file, maximumRuntimeAttemptUploadConfigBytes+1))
	if err != nil {
		return "", 0, err
	}
	if len(wire) > maximumRuntimeAttemptUploadConfigBytes {
		return "", 0, errors.New("runtime attempt upload config exceeds its control bound")
	}
	root, err := runtimeAttemptUploadYAML(wire)
	if err != nil {
		return "", 0, err
	}
	seen := map[string]bool{}
	var profile, enabled bool
	var configured *yaml.Node
	for index := 0; index < len(root.Content); index += 2 {
		key, value := root.Content[index], root.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] || key.Value == "<<" || key.Value == "attempt_upload" || key.Value == "reserved_attempt_upload" {
			return "", 0, errors.New("runtime attempt upload settings contain duplicate, merged or mainnet keys")
		}
		seen[key.Value] = true
		switch key.Value {
		case "profile":
			profile = value.Kind == yaml.ScalarNode && value.Tag == "!!str" && value.Value == "testnet"
		case "testnet-enabled":
			enabled = value.Kind == yaml.ScalarNode && value.Tag == "!!bool" && value.Value == "true"
		case "testnet-attempt-upload":
			configured = value
		}
	}
	actual, err := runtimeAttemptUploadNode(configured)
	if err != nil || !profile || !enabled || actual != want {
		return "", 0, errors.New("runtime attempt upload settings differ from the approved testnet capacity")
	}
	if err := validateRuntimeReservedAttemptUploadNode(cfg, stateDir, relative, root); err != nil {
		return "", 0, err
	}
	digest := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(digest[:]), info.Mode().Perm(), nil
}
