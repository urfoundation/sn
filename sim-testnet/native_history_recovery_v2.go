//go:build linux || darwin

// Native recovery has its own owner-signed approval and immutable child pins.
// It creates no chain transaction, funding allowance, or final acceptance.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const nativeHistoryRecoverySchemaV2 = "urnetwork-sim-native-history-recovery-v2"
const nativeHistoryRecoveryKindV2 = "native-history-recovery-v2"
const nativeHistoryRecoveryMaximumBytesV2 = 1024 * 1024

// Every source is immutable and independently authenticated. A later native
// runtime migration or archive exception is deliberately absent from this wire.
type nativeHistoryRecoveryPlanV2 struct {
	ConfigMigration       *nativeHistoryRecoveryConfigMigrationV2             `json:"config_migration,omitempty"`
	Schema                string                                              `json:"schema"`
	PlanHash              string                                              `json:"plan_hash"`
	BasePlanHash          string                                              `json:"base_plan_hash"`
	DeploymentId          string                                              `json:"deployment_id"`
	StateDir              string                                              `json:"state_dir"`
	ConfigHash            string                                              `json:"config_hash"`
	PolicyHash            string                                              `json:"policy_hash"`
	Generation            uint64                                              `json:"generation"`
	RolloverPlanHash      string                                              `json:"rollover_plan_hash"`
	RolloverHandoffSha256 string                                              `json:"rollover_handoff_sha256"`
	SourceRole            validatorcomponent.ReleaseEvidenceV2File            `json:"source_role"`
	Driver                provisionalDriverProvenance                         `json:"driver"`
	Terminal              validatorcomponent.ReleaseEvidenceV2File            `json:"terminal"`
	Result                validatorcomponent.ReleaseEvidenceV2File            `json:"result"`
	RunId                 string                                              `json:"run_id"`
	Native                ChainHead                                           `json:"native"`
	NativeEpoch           uint64                                              `json:"native_epoch"`
	FirstNativeEpoch      uint64                                              `json:"first_native_epoch"`
	Runtime               crv4.RuntimeArtifactIdentity                        `json:"runtime"`
	Provisional           bool                                                `json:"provisional"`
	FinalAcceptance       bool                                                `json:"final_acceptance"`
	StateImported         bool                                                `json:"state_imported"`
	Validators            []validatorcomponent.ReleaseNativeHistoryRecoveryV2 `json:"validators"`
}

// The current recovery approval separately binds the signed timeout migration
// and its exact immediate predecessor. An arbitrary ancestor is insufficient.
type nativeHistoryRecoveryConfigMigrationV2 struct {
	RequestHash    string `json:"request_hash"`
	ReceiptHash    string `json:"receipt_hash"`
	PlanHash       string `json:"plan_hash"`
	SourcePlanHash string `json:"source_plan_hash"`
}

func nativeRecoveryPredecessorScopeV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan) (*ResolvedConfig, *SetupPlan, *nativeHistoryRecoveryConfigMigrationV2, error) {
	if cfg == nil || base == nil {
		return nil, nil, nil, errors.New("native recovery lacks its current approved scope")
	}
	if base.CampaignConfigMigrationHash == "" {
		return cfg, base, nil, nil
	}
	sourceCfg, source, receipt, err := authenticatedCampaignConfigMigrationSource(ctx, cfg, stateDir, base, "")
	if err != nil {
		return nil, nil, nil, err
	}
	if receipt.PlanHash != base.PlanHash || source.PlanHash != receipt.Request.SourcePlanHash {
		return nil, nil, nil, errors.New("native recovery requires the exact immediate campaign config migration")
	}
	pin := &nativeHistoryRecoveryConfigMigrationV2{RequestHash: base.CampaignConfigMigrationHash, ReceiptHash: receipt.Hash, PlanHash: receipt.PlanHash, SourcePlanHash: source.PlanHash}
	return sourceCfg, source, pin, nil
}

// The optional selection is invocation-owned. Ordinary retained restarts read
// the already selected immutable request from the authenticated supervisor.
type nativeHistoryRecoverySelectionV2 struct{ path, sha256 string }

// hash excludes only its own digest; all child requests are included in review.
func (self nativeHistoryRecoveryPlanV2) hash() (string, error) {
	self.PlanHash = ""
	return canonicalHashHex(self)
}

// nativeHistoryRecoveryRootV2 keeps distinct reviewed attempts immutable,
// including an unused request whose chosen native epoch was missed.
func nativeHistoryRecoveryRootV2(stateDir, hash string) string {
	return filepath.Join(stateDir, "native-history-recoveries", strings.TrimPrefix(hash, "0x"))
}

// nativeHistoryRecoveryRequestBytesV2 preserves the child decoder's canonical
// exact-hash domain instead of embedding a differently indented raw message.
func nativeHistoryRecoveryRequestBytesV2(request validatorcomponent.ReleaseNativeHistoryRecoveryV2) ([]byte, error) {
	raw, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// validateNativeHistoryRecoveryOptionsV2 separates read-only capture, explicit
// exact-hash receipt publication, and provisional process selection.
func validateNativeHistoryRecoveryOptionsV2(command string, o cliOptions) error {
	selection := o.NativeRecoveryHandoff != "" || o.NativeRecoveryHandoffSha256 != ""
	planning := o.NativeRecoverySource != "" || o.NativeRecoveryPlan != "" || o.NativeRecoveryPlanHash != ""
	if selection && (command != "resume" || !o.Apply || !o.ProvisionalResume || o.NativeRecoveryHandoff == "" || !validCanonicalHashHex(o.NativeRecoveryHandoffSha256) || o.StrictHistoryAdoption != "" || o.ThenReleaseCandidate || o.PrepareOnly) {
		return errors.New("native recovery handoff requires an exact path/hash and provisional resume --apply")
	}
	if command != "native-history-recovery" {
		if planning {
			return errors.New("native recovery planning flags require native-history-recovery")
		}
		return nil
	}
	if !o.ProvisionalResume || !validCanonicalHashHex(o.PlanHash) || o.Detach || o.PrepareOnly || o.ThenReleaseCandidate || o.StrictHistoryAdoption != "" || o.Name != "" || o.Manifest != "" || selection {
		return errors.New("native recovery requires the exact current testnet plan and provisional mode without process or acceptance actions")
	}
	if o.Apply {
		if o.NativeRecoveryPlan == "" || !validCanonicalHashHex(o.NativeRecoveryPlanHash) || o.NativeRecoverySource != "" || o.FirstNativeEpoch != 0 {
			return errors.New("native recovery apply requires the saved plan and its exact new hash, without replacement source or epoch")
		}
	} else if o.NativeRecoveryPlan != "" || o.NativeRecoveryPlanHash != "" || o.NativeRecoverySource == "" || o.FirstNativeEpoch == 0 || o.FirstNativeEpoch == ^uint64(0) {
		return errors.New("native recovery capture requires a sealed source and an explicit future native epoch")
	}
	for _, path := range []string{o.NativeRecoverySource, o.NativeRecoveryPlan, o.NativeRecoveryHandoff} {
		if path != "" && (!filepath.IsAbs(path) || filepath.Clean(path) != path) {
			return errors.New("native recovery path must be absolute and clean")
		}
	}
	return nil
}

// authenticateNativeRecoveryTerminalV2 authenticates the sealed failed source
// and keeps its result bytes unchanged. It does not adopt any failed assertion.
func authenticateNativeRecoveryTerminalV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, path string) (terminal, result validatorcomponent.ReleaseEvidenceV2File, runId string, resultErr error) {
	cfg, base, _, err := nativeRecoveryPredecessorScopeV2(ctx, cfg, stateDir, base)
	if err != nil {
		return terminal, result, "", err
	}
	relative, err := filepath.Rel(stateDir, path)
	if err != nil || filepath.Dir(relative) != "campaign-attempts" {
		return terminal, result, "", errors.New("native recovery requires an original campaign-attempt source")
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(relative), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return terminal, result, "", err
	}
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(raw, &envelope); err != nil {
		return terminal, result, "", err
	}
	if err := verifyEvidence(&envelope, nil); err != nil {
		return terminal, result, "", err
	}
	var source scenarioCampaignAttemptPayload
	if err := decodeStrictJSONBytes(envelope.Payload, &source); err != nil {
		return terminal, result, "", err
	}
	if envelope.Kind != scenarioCampaignAttemptEvidenceKind || envelope.Signer != common.HexToAddress(base.Roles.Owner) || envelope.RunID != source.RunID || envelope.DeploymentID != base.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || envelope.GenesisHash != cfg.Public.Chain.GenesisHash || source.Schema != scenarioCampaignAttemptSchema || !source.PreparationComplete || source.PlanHash != base.PlanHash || source.Phase != "release-1.0" || source.ConfigHash != cfg.ConfigHash || source.PolicyHash != cfg.PolicyHash || source.RunID == "" || filepath.Base(source.RunID) != source.RunID || source.AcceptanceBoundary == nil || source.AcceptanceInvalidation == "" || source.AcceptanceInvalidatedAt == "" {
		return terminal, result, "", errors.New("native recovery source is not the exact owner-signed invalidated release")
	}
	boundary := source.AcceptanceBoundary
	if boundary.LastObservationHead.Number == 0 || !validCanonicalHashHex(boundary.LastObservationHead.Hash) || boundary.AcceptanceWindow.TerminalBlock == 0 {
		return terminal, result, "", errors.New("native recovery sealed source lacks its actual observed boundary")
	}
	resultPath := filepath.Join(stateDir, "runs", source.RunID, "result.json")
	resultRaw, err := readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(filepath.Join("runs", source.RunID, "result.json")), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return terminal, result, "", err
	}
	var outcome ScenarioResult
	if err := decodeStrictJSONBytes(resultRaw, &outcome); err != nil {
		return terminal, result, "", err
	}
	hash, err := canonicalScenarioResultHash(&outcome)
	if err != nil || hash != outcome.EvidenceHash || outcome.Schema != "urnetwork-sim-scenario-result-v1" || outcome.Release != "1.0" || outcome.RunID != source.RunID || outcome.StartedAt != source.StartedAt || outcome.Result != "fail" || outcome.Name != "release-1.0" || outcome.ConfigHash != cfg.ConfigHash || outcome.PolicyHash != cfg.PolicyHash || outcome.DeploymentID != base.DeploymentID || outcome.ChainID != cfg.ChainID || outcome.Netuid != cfg.Netuid || outcome.GenesisHash != cfg.Public.Chain.GenesisHash || !outcome.Provisional || outcome.FinalAcceptance == nil || *outcome.FinalAcceptance || outcome.EndHead != boundary.LastObservationHead || outcome.EndEpoch != boundary.LastObservationEpoch || outcome.CampaignStartHead != boundary.CampaignStartHead || outcome.CampaignStartEpoch != boundary.CampaignStartEpoch || outcome.ScenarioDefinition != boundary.ScenarioDefinitionHash || !scenarioAcceptanceWindowsEqual(outcome.AcceptanceWindow, &boundary.AcceptanceWindow) || !reflect.DeepEqual(outcome.Faults, boundary.Faults) {
		return terminal, result, "", errors.Join(errors.New("native recovery result differs from its sealed failed interval"), err)
	}
	if _, err := provisionalProductionFailedAssertions(&outcome); err != nil {
		return terminal, result, "", err
	}
	started, startErr := time.Parse(time.RFC3339Nano, source.StartedAt)
	completed, completedErr := time.Parse(time.RFC3339Nano, outcome.CompletedAt)
	invalidated, invalidationErr := time.Parse(time.RFC3339Nano, source.AcceptanceInvalidatedAt)
	if startErr != nil || completedErr != nil || invalidationErr != nil || completed.Before(started) || invalidated.Before(completed) {
		return terminal, result, "", errors.New("native recovery predecessor is unfinished or has inconsistent sealing times")
	}
	// A sealed partial failure is a valid predecessor, not completed coverage.
	// Preserve its active/pending faults and cleanup after the last observation;
	// the independent cutover gate requires current perturbations to be restored.
	if boundary.LastObservationHead.Number < boundary.AcceptanceWindow.TerminalBlock {
		interrupted := false
		for _, assertion := range outcome.Assertions {
			if assertion.ID == "acceptance_interval_observed" && !assertion.Passed {
				interrupted = true
			}
		}
		if source.AcceptanceInvalidation != "execution-exited-before-completion" || !interrupted {
			return terminal, result, "", errors.New("native recovery partial predecessor lacks its exact signed interruption and failed coverage assertion")
		}
	}
	return policyRolloverFile(path, raw), policyRolloverFile(resultPath, resultRaw), source.RunID, ctx.Err()
}

// validateNativeHistoryRecoveryPlanV2 checks immutable public authority and
// local source bytes without creating keys, rewriting a config, or opening RPC.
func validateNativeHistoryRecoveryPlanV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, p *nativeHistoryRecoveryPlanV2, exact bool) error {
	if cfg == nil || base == nil || h == nil || p == nil || !provisionalResumeEnabled(cfg) || h.SourceRoleOverlay == nil {
		return errors.New("native recovery requires the selected provisional generation and source-role overlay")
	}
	hash, err := p.hash()
	if err != nil || p.Schema != nativeHistoryRecoverySchemaV2 || p.PlanHash != hash || p.BasePlanHash != base.PlanHash || p.StateDir != stateDir || p.DeploymentId != base.DeploymentID || p.ConfigHash != cfg.ConfigHash || p.PolicyHash != cfg.PolicyHash || p.Generation != 2 || p.Generation != h.Generation || p.RolloverPlanHash != h.PlanHash || p.RolloverHandoffSha256 != h.sourceSHA256 || p.SourceRole != *h.SourceRoleOverlay || !p.Provisional || p.FinalAcceptance || p.StateImported || len(p.Validators) != 2 || len(h.Validators) != 2 || p.FirstNativeEpoch <= p.NativeEpoch || p.Native.Number == 0 || !validCanonicalHashHex(p.Native.Hash) || p.Driver != cfg.provisionalResume.Driver {
		return errors.Join(errors.New("native recovery plan changes its approval, runtime driver, generation or scope"), err)
	}
	_, _, migration, err := nativeRecoveryPredecessorScopeV2(ctx, cfg, stateDir, base)
	if err != nil || !reflect.DeepEqual(migration, p.ConfigMigration) {
		return errors.Join(errors.New("native recovery campaign config migration approval changed"), err)
	}
	terminal, result, runId, err := authenticateNativeRecoveryTerminalV2(ctx, cfg, stateDir, base, p.Terminal.Path)
	if err != nil || terminal != p.Terminal || result != p.Result || runId != p.RunId {
		return errors.Join(errors.New("native recovery sealed predecessor changed"), err)
	}
	for index, request := range p.Validators {
		selected := h.Validators[index]
		if request.Adoption.ValidatorID != uint64(index+1) || request.Adoption.ValidatorID != selected.ValidatorID || request.ConfigPath != selected.Config.Path || request.Adoption.ConfigSHA256 != "sha256:"+strings.TrimPrefix(selected.Config.SHA256, "0x") || request.Adoption.CoordinatorStateDir != selected.StateDir || request.Adoption.ApprovedPlanHash != base.PlanHash || request.Adoption.SourcePlanHash != h.PlanHash || request.Adoption.FirstNativeEpoch != p.FirstNativeEpoch || request.Adoption.LastNativeEpoch > p.NativeEpoch || request.StateDir != stateDir || request.Runtime != p.Runtime {
			return errors.New("native recovery child differs from the exact selected generation")
		}
		raw, err := nativeHistoryRecoveryRequestBytesV2(request)
		if err != nil {
			return err
		}
		if err := validatorcomponent.CheckReleaseNativeHistoryRecoveryV2Source(ctx, raw, bytesSHA256(raw), exact); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// readNativeHistoryRecoveryHandoffV2 authenticates the deployment signature,
// immutable reviewed plan, and exact local prefix before child argv can change.
func readNativeHistoryRecoveryHandoffV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, path, expectedSha256 string) (*nativeHistoryRecoveryPlanV2, error) {
	var signed ReleaseEvidenceEnvelope
	raw, err := readRuntimeEvidenceSetupV2(ctx, path, nativeHistoryRecoveryMaximumBytesV2, &signed)
	if err != nil {
		return nil, err
	}
	if expectedSha256 != "" && policyRolloverFile(path, raw).SHA256 != expectedSha256 {
		return nil, errors.New("native recovery handoff differs from its exact approved pin")
	}
	if err := verifyEvidence(&signed, nil); err != nil {
		return nil, err
	}
	var p nativeHistoryRecoveryPlanV2
	if err := decodeStrictJSONBytes(signed.Payload, &p); err != nil {
		return nil, err
	}
	root := nativeHistoryRecoveryRootV2(stateDir, p.PlanHash)
	if path != filepath.Join(root, "handoff.evidence.json") || signed.Kind != nativeHistoryRecoveryKindV2 || signed.RunID != p.PlanHash || signed.Signer != common.HexToAddress(base.Roles.Owner) || signed.DeploymentID != base.DeploymentID || signed.ChainID != cfg.ChainID || signed.Netuid != cfg.Netuid || signed.GenesisHash != cfg.Public.Chain.GenesisHash {
		return nil, errors.New("native recovery handoff lacks its exact deployment owner approval")
	}
	var reviewed nativeHistoryRecoveryPlanV2
	if _, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "plan.json"), nativeHistoryRecoveryMaximumBytesV2, &reviewed); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(p, reviewed) {
		return nil, errors.New("native recovery signature differs from the immutable reviewed plan")
	}
	if err := validateNativeHistoryRecoveryPlanV2(ctx, cfg, stateDir, base, h, &p, false); err != nil {
		return nil, err
	}
	return &p, nil
}

// nativeHistoryRecoveryArgumentsV2 rejects partial, duplicate and unknown
// recovery arguments rather than repairing a changed retained supervisor.
func nativeHistoryRecoveryArgumentsV2(args []string) (path, hash string, resultErr error) {
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--native-history-recovery="):
			if path != "" {
				return "", "", errors.New("native recovery child path is duplicated")
			}
			path = strings.TrimPrefix(arg, "--native-history-recovery=")
			if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return "", "", errors.New("native recovery child path is not canonical")
			}
		case strings.HasPrefix(arg, "--native-history-recovery-sha256="):
			if hash != "" {
				return "", "", errors.New("native recovery child hash is duplicated")
			}
			hash = strings.TrimPrefix(arg, "--native-history-recovery-sha256=")
			if !validSHA256ContentHash(hash) {
				return "", "", errors.New("native recovery child hash is not canonical")
			}
		case strings.HasPrefix(arg, "--native-history-recovery"):
			return "", "", errors.New("native recovery child has an unknown argument")
		}
	}
	if (path == "") != (hash == "") {
		return "", "", errors.New("native recovery child has a partial request pair")
	}
	return path, hash, nil
}

// attachNativeHistoryRecoveryV2 runs after source-role/generation projection.
// A selected immutable receipt can replace only this distinct argument pair.
func attachNativeHistoryRecoveryV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, specs []ProcessSpec) error {
	selection := cfg.nativeHistoryRecoveryV2
	explicit := selection != nil
	for _, spec := range specs {
		path, _, err := nativeHistoryRecoveryArgumentsV2(spec.Args)
		if err != nil {
			return err
		}
		if path == "" {
			continue
		}
		if spec.Role != "validator" {
			return errors.New("native recovery request appears on another process role")
		}
		if !explicit {
			path = filepath.Join(filepath.Dir(path), "handoff.evidence.json")
			if selection != nil && selection.path != path {
				return errors.New("native recovery process requests select different approvals")
			}
			selection = &nativeHistoryRecoverySelectionV2{path: path}
		}
	}
	if selection == nil {
		return nil
	}
	if h == nil || base == nil {
		return errors.New("native recovery requires authenticated generation authority")
	}
	p, err := readNativeHistoryRecoveryHandoffV2(ctx, cfg, stateDir, base, h, selection.path, selection.sha256)
	if err != nil {
		return err
	}
	for index, request := range p.Validators {
		raw, err := nativeHistoryRecoveryRequestBytesV2(request)
		if err != nil {
			return err
		}
		path := filepath.Join(nativeHistoryRecoveryRootV2(stateDir, p.PlanHash), fmt.Sprintf("validator-%d.json", request.Adoption.ValidatorID))
		retained, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, policyRolloverFile(path, raw), validatorcomponent.ReleaseNativeHistoryRecoveryV2MaximumBytes)
		if err != nil || !bytes.Equal(raw, retained) {
			return errors.Join(errors.New("native recovery child request changed"), err)
		}
		found := false
		for member := range specs {
			if specs[member].ID != fmt.Sprintf("validator-%d", index+1) {
				continue
			}
			if found || specs[member].Role != "validator" {
				return errors.New("native recovery validator process census differs")
			}
			found = true
			oldPath, oldHash, err := nativeHistoryRecoveryArgumentsV2(specs[member].Args)
			if err != nil {
				return err
			}
			if !explicit && (oldPath != path || oldHash != bytesSHA256(raw)) {
				return errors.New("retained native recovery child differs from its signed request")
			}
			args := []string{}
			configCount := 0
			for _, arg := range specs[member].Args {
				if strings.HasPrefix(arg, "--config=") {
					if arg != "--config="+request.ConfigPath {
						return errors.New("native recovery selected config was overwritten")
					}
					configCount++
				}
				if strings.HasPrefix(arg, "--provisional-activation-setup") || strings.HasPrefix(arg, "--strict-history-adoption") {
					return errors.New("native recovery cannot compose startup authorities")
				}
				if !strings.HasPrefix(arg, "--native-history-recovery") {
					args = append(args, arg)
				}
			}
			if configCount != 1 {
				return errors.New("native recovery has no sole selected config")
			}
			specs[member].Args = append(args, "--native-history-recovery="+path, "--native-history-recovery-sha256="+bytesSHA256(raw))
		}
		if !found {
			return errors.New("native recovery lacks a validator process")
		}
	}
	return nil
}

// Fresh launch must authenticate explicit authority before database migration,
// account provisioning or rendering. Child argv is checked again after render.
func preflightNativeHistoryRecoverySelectionV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan) error {
	if cfg.nativeHistoryRecoveryV2 == nil {
		return nil
	}
	h, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if err != nil || h == nil {
		return errors.Join(errors.New("native recovery requires authenticated generation authority"), err)
	}
	selection := cfg.nativeHistoryRecoveryV2
	if _, err := readNativeHistoryRecoveryHandoffV2(ctx, cfg, stateDir, base, h, selection.path, selection.sha256); err != nil {
		return fmt.Errorf("native recovery launch admission: %w", err)
	}
	return nil
}

// Already-running processes cannot receive new argv. Authenticate both an
// explicit selector and retained pins, then require their exact existing pair.
func checkNativeHistoryRecoveryLiveArgumentsV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, specs []ProcessSpec) error {
	copy := make([]ProcessSpec, len(specs))
	for index, spec := range specs {
		copy[index] = spec
		copy[index].Args = append([]string(nil), spec.Args...)
	}
	if err := attachNativeHistoryRecoveryV2(ctx, cfg, stateDir, base, h, copy); err != nil {
		return fmt.Errorf("native recovery live admission: %w", err)
	}
	for index := range specs {
		if !reflect.DeepEqual(specs[index].Args, copy[index].Args) {
			return errors.New("native recovery selection would change a live process; stopped topology is required")
		}
	}
	return nil
}

func preflightNativeHistoryRecoveryLiveV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, specs []ProcessSpec) error {
	selected := cfg.nativeHistoryRecoveryV2 != nil
	for _, spec := range specs {
		path, _, err := nativeHistoryRecoveryArgumentsV2(spec.Args)
		if err != nil {
			return err
		}
		selected = selected || path != ""
	}
	if !selected {
		return nil
	}
	if base == nil {
		return errors.New("native recovery live admission lacks its approved plan")
	}
	h, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if err != nil {
		return fmt.Errorf("native recovery live generation: %w", err)
	}
	return checkNativeHistoryRecoveryLiveArgumentsV2(ctx, cfg, stateDir, base, h, specs)
}
