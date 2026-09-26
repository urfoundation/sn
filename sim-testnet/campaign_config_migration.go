// An explicit signed campaign-only timeout migration retains the original
// configuration bytes and evidence owners without granting release acceptance.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

const campaignConfigMigrationSchema = "urnetwork-sim-campaign-timeout-migration-v1"
const campaignConfigMigrationMaximumBytes = 4 * 1024 * 1024

// Preserve both exact yaml snapshots, including the original source spelling.
// The complete semantic delta is independently checked before signing and use.
type campaignConfigMigrationRequest struct {
	Schema                string `json:"schema"`
	StateDir              string `json:"state_dir"`
	DeploymentId          string `json:"deployment_id"`
	SourcePlanHash        string `json:"source_plan_hash"`
	SourcePlanBytesSha256 string `json:"source_plan_bytes_sha256"`
	SourceConfigHash      string `json:"source_config_hash"`
	ConfigHash            string `json:"config_hash"`
	PolicyHash            string `json:"policy_hash"`
	SourceConfigBytes     []byte `json:"source_config_bytes"`
	ConfigBytes           []byte `json:"config_bytes"`
	Provisional           bool   `json:"provisional"`
	FinalAcceptance       bool   `json:"final_acceptance"`
}

// The owner signature binds the request and the complete resulting setup plan.
// Hash and signature are omitted only while calculating the signed digest.
type campaignConfigMigrationReceipt struct {
	Request   campaignConfigMigrationRequest `json:"request"`
	PlanHash  string                         `json:"plan_hash"`
	Hash      string                         `json:"hash,omitempty"`
	Signature string                         `json:"owner_signature,omitempty"`
}

// Keep migration records separate from immutable activation/source-role files.
func campaignConfigMigrationRoot(stateDir, hash string) string {
	return filepath.Join(stateDir, "campaign-config-migrations", stringsTrim0x(hash))
}

// Decode the archived syntax without making an obsolete timeout runnable.
// Only the new configuration is admitted by today's complete validation.
func decodeCampaignConfigMigrationConfig(raw []byte) (*HarnessConfig, error) {
	if len(raw) == 0 || len(raw) > campaignConfigMigrationMaximumBytes/2 {
		return nil, errors.New("campaign config snapshot is empty or exceeds its bound")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var config HarnessConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("campaign config snapshot contains trailing yaml")
	}
	return &config, nil
}

// Hash the actual archived configs with the unchanged manifest inputs. Never
// assign an archived fingerprint to current config values to make them match.
func validateCampaignConfigMigrationConfigs(cfg *ResolvedConfig, request *campaignConfigMigrationRequest) (*ResolvedConfig, error) {
	if cfg == nil || cfg.Config == nil || request == nil || request.Schema != campaignConfigMigrationSchema ||
		!request.Provisional || request.FinalAcceptance || !filepath.IsAbs(request.StateDir) || filepath.Clean(request.StateDir) != request.StateDir ||
		request.DeploymentId != cfg.Config.Deployment.DeploymentID || request.PolicyHash != cfg.PolicyHash || request.ConfigHash != cfg.ConfigHash ||
		!validCanonicalHashHex(request.SourcePlanHash) || !validCanonicalHashHex(request.SourceConfigHash) || !validSHA256ContentHash(request.SourcePlanBytesSha256) {
		return nil, errors.New("campaign config migration identity is incomplete or accepting")
	}
	sourceConfig, err := decodeCampaignConfigMigrationConfig(request.SourceConfigBytes)
	if err != nil {
		return nil, err
	}
	config, err := decodeCampaignConfigMigrationConfig(request.ConfigBytes)
	if err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	sourceAdversaries, targetAdversaries := sourceConfig.Scenarios.Adversaries, config.Scenarios.Adversaries
	if sourceAdversaries.RequestTimeoutMilliseconds != 10_000 || sourceAdversaries.MaximumP99LatencyMilliseconds != 15_000 ||
		targetAdversaries.RequestTimeoutMilliseconds != 60_000 || targetAdversaries.MaximumP99LatencyMilliseconds != 60_000 {
		return nil, errors.New("campaign config migration requires exact timeout/p99 10000/15000 to 60000/60000")
	}
	normalized := *config
	normalized.Scenarios.Adversaries.RequestTimeoutMilliseconds = sourceAdversaries.RequestTimeoutMilliseconds
	normalized.Scenarios.Adversaries.MaximumP99LatencyMilliseconds = sourceAdversaries.MaximumP99LatencyMilliseconds
	if !evidenceRelayContinuationSameJSON(&normalized, sourceConfig) {
		return nil, errors.New("campaign config migration changed fields outside timeout and p99")
	}
	sourceHash, err := releaseConfigHash(sourceConfig, cfg.Public, cfg.Hyperparameters)
	if err != nil || sourceHash != request.SourceConfigHash {
		return nil, errors.Join(errors.New("campaign config migration source snapshot hash differs"), err)
	}
	targetHash, err := releaseConfigHash(config, cfg.Public, cfg.Hyperparameters)
	if err != nil || targetHash != request.ConfigHash || sourceHash == targetHash {
		return nil, errors.Join(errors.New("campaign config migration target snapshot hash differs"), err)
	}
	source := *cfg
	source.Config, source.ConfigHash, source.runtimePlanReads = sourceConfig, sourceHash, nil
	return &source, nil
}

// This sole deterministic plan transform retains every chain action, allowance,
// route, generation and release lock. The two local runtime stamps explicitly
// carry their old receipts because neither timeout changes rendered inputs.
func appendCampaignConfigMigrationPlan(source *SetupPlan, request campaignConfigMigrationRequest) (*SetupPlan, error) {
	if source == nil || source.CampaignConfigMigrationHash != "" || source.PlanHash != request.SourcePlanHash || source.ConfigHash != request.SourceConfigHash || source.PolicyHash != request.PolicyHash || source.DeploymentID != request.DeploymentId {
		return nil, errors.New("campaign config migration source approval differs")
	}
	hash, err := canonicalHashHex(request)
	if err != nil {
		return nil, err
	}
	current, err := rebindFleetRenewalRuntimePlan(source, request.ConfigHash, source.ResolvedInputsHash)
	if err != nil {
		return nil, err
	}
	for index := range current.Actions {
		action := &current.Actions[index]
		if action.ID != "config.render" && action.ID != "topology.launch" {
			continue
		}
		previous := source.Actions[index]
		action.AcceptedPriorIntentHashes = append(slices.Clone(previous.AcceptedPriorIntentHashes), previous.IntentHash)
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return nil, err
		}
	}
	current.CampaignConfigMigrationHash = hash
	current.PriorPlanHashes = append(slices.Clone(source.PriorPlanHashes), source.PlanHash)
	current.PlanHash, err = current.hash()
	return current, err
}

// Authenticate the owner-signed migration, both complete config snapshots,
// the exact predecessor archive and the deterministic successor approval.
// An ancestor-only relationship never authorizes a config difference.
func authenticatedCampaignConfigMigrationSource(ctx context.Context, cfg *ResolvedConfig, stateDir string, current *SetupPlan, sourcePlanHash string) (*ResolvedConfig, *SetupPlan, *campaignConfigMigrationReceipt, error) {
	if ctx == nil || cfg == nil || current == nil || !validCanonicalHashHex(current.CampaignConfigMigrationHash) || cfg.ConfigHash != current.ConfigHash {
		return nil, nil, nil, errors.New("retained config difference has no explicit campaign migration")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	root := campaignConfigMigrationRoot(stateDir, current.CampaignConfigMigrationHash)
	relative, err := filepath.Rel(stateDir, filepath.Join(root, "receipt.json"))
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, relative, campaignConfigMigrationMaximumBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	var receipt campaignConfigMigrationReceipt
	if err := decodeStrictJSONBytes(raw, &receipt); err != nil {
		return nil, nil, nil, err
	}
	unsigned := receipt
	unsigned.Hash, unsigned.Signature = "", ""
	if err := verifyCoordinatorRepairSignature(unsigned, receipt.Hash, receipt.Signature, common.HexToAddress(current.Roles.Owner)); err != nil {
		return nil, nil, nil, err
	}
	request := &receipt.Request
	hash, err := canonicalHashHex(request)
	if err != nil || hash != current.CampaignConfigMigrationHash || request.StateDir != stateDir {
		return nil, nil, nil, errors.Join(errors.New("campaign migration receipt differs from its approved namespace"), err)
	}
	sourceCfg, err := validateCampaignConfigMigrationConfigs(cfg, request)
	if err != nil {
		return nil, nil, nil, err
	}
	sourceBytes, err := readSetupPlanBytes(stateDir, "plans/"+stringsTrim0x(request.SourcePlanHash)+".json")
	if err != nil || bytesSHA256(sourceBytes) != request.SourcePlanBytesSha256 {
		return nil, nil, nil, errors.Join(errors.New("campaign migration original plan bytes changed"), err)
	}
	source, err := decodePersistedPlanWire(sourceBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	expected, err := appendCampaignConfigMigrationPlan(source, *request)
	if err != nil || expected.PlanHash != receipt.PlanHash || !current.allowedPlanHashes()[expected.PlanHash] || current.PolicyHash != source.PolicyHash || current.OwnedRPCAuthority != source.OwnedRPCAuthority || current.ResolvedInputsHash != source.ResolvedInputsHash {
		return nil, nil, nil, errors.Join(errors.New("campaign migration signed successor or operational authority differs"), err)
	}
	approved, err := readValidatorEvidenceHistoricalPlan(stateDir, receipt.PlanHash)
	if err != nil || !evidenceRelayContinuationSameJSON(expected, approved) {
		return nil, nil, nil, errors.Join(errors.New("campaign migration successor archive changed"), err)
	}
	if current.PlanHash == receipt.PlanHash && !evidenceRelayContinuationSameJSON(current, approved) {
		return nil, nil, nil, errors.New("campaign migration current plan differs from its signed successor")
	}
	if sourcePlanHash != "" && sourcePlanHash != source.PlanHash {
		if !source.allowedPlanHashes()[sourcePlanHash] {
			return nil, nil, nil, errors.New("campaign migration retained source is outside the original lineage")
		}
		source, err = readValidatorEvidenceHistoricalPlan(stateDir, sourcePlanHash)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if source.ConfigHash != sourceCfg.ConfigHash || source.PolicyHash != current.PolicyHash || source.OwnedRPCAuthority != current.OwnedRPCAuthority {
		return nil, nil, nil, errors.New("campaign migration retained source changed config, policy or route")
	}
	if err := validatorEvidenceSourcePlanMatches(current, source); err != nil {
		return nil, nil, nil, fmt.Errorf("campaign migration retained custody: %w", err)
	}
	return sourceCfg, source, &receipt, ctx.Err()
}
