// Capture and apply the separate reviewed campaign timeout migration under
// the deployment lock. Signing changes no chain, runtime config or ledger.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Require an explicit source approval and a separate reviewed migration hash.
func validateCampaignConfigMigrationOptions(command string, options cliOptions) error {
	if command != "campaign-config-migration" {
		if options.ConfigMigrationSourceConfig != "" || options.ConfigMigrationHash != "" {
			return errors.New("config migration flags require campaign-config-migration")
		}
		return nil
	}
	if !options.ProvisionalResume || !validCanonicalHashHex(options.PlanHash) || options.Detach || options.PrepareOnly || options.ThenReleaseCandidate || options.Name != "" || options.Manifest != "" || options.StrictHistoryAdoption != "" || options.ProvisionalRPCAuthority != "" {
		return errors.New("campaign config migration requires explicit non-accepting source approval without launch or route changes")
	}
	if options.Apply {
		if !validCanonicalHashHex(options.ConfigMigrationHash) || options.ConfigMigrationSourceConfig != "" {
			return errors.New("campaign config migration apply requires only its exact --config-migration-hash")
		}
	} else if !filepath.IsAbs(options.ConfigMigrationSourceConfig) || filepath.Clean(options.ConfigMigrationSourceConfig) != options.ConfigMigrationSourceConfig || options.ConfigMigrationHash != "" {
		return errors.New("campaign config migration capture requires --config-migration-source-config with the unchanged absolute old yaml path")
	}
	return nil
}

// Build a review using real predecessor bytes; an obsolete config is decoded
// only here and must differ from a valid new config by exactly the two fields.
func prepareCampaignConfigMigration(cfg *ResolvedConfig, stateDir string, sourceBytes, sourceConfigBytes, configBytes []byte) (*campaignConfigMigrationRequest, *SetupPlan, error) {
	source, err := decodePersistedPlanWire(sourceBytes)
	if err != nil {
		return nil, nil, err
	}
	request := &campaignConfigMigrationRequest{Schema: campaignConfigMigrationSchema, StateDir: stateDir, DeploymentId: source.DeploymentID,
		SourcePlanHash: source.PlanHash, SourcePlanBytesSha256: bytesSHA256(sourceBytes), SourceConfigHash: source.ConfigHash, ConfigHash: cfg.ConfigHash,
		PolicyHash: cfg.PolicyHash, SourceConfigBytes: bytes.Clone(sourceConfigBytes), ConfigBytes: bytes.Clone(configBytes), Provisional: true}
	sourceCfg, err := validateCampaignConfigMigrationConfigs(cfg, request)
	if err != nil {
		return nil, nil, err
	}
	if _, err := loadPlanIdentityBytes(sourceCfg, sourceBytes, true); err != nil {
		return nil, nil, err
	}
	current, err := appendCampaignConfigMigrationPlan(source, *request)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, nil, err
	}
	if _, err := loadPlanIdentityBytes(cfg, raw, true); err != nil {
		return nil, nil, err
	}
	return request, current, nil
}

// Review writes only immutable planning artifacts. Apply holds the deployment
// lock, requires stopped owners, publishes the signature before plan.json, and
// can retry after either publication without replacing original archives.
func runCampaignConfigMigration(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) error {
	if err := validateCampaignConfigMigrationOptions("campaign-config-migration", options); err != nil {
		return err
	}
	configBytes, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, cfg.ConfigPath, campaignConfigMigrationMaximumBytes/2)
	if err != nil {
		return err
	}
	if !options.Apply {
		sourceBytes, err := readSetupPlanBytes(stateDir, "plan.json")
		if err != nil {
			return err
		}
		source, err := decodePersistedPlanWire(sourceBytes)
		if err != nil {
			return err
		}
		if err := requireApproved(true, options.PlanHash, source.PlanHash); err != nil {
			return err
		}
		sourceConfigBytes, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, options.ConfigMigrationSourceConfig, campaignConfigMigrationMaximumBytes/2)
		if err != nil {
			return err
		}
		// The first archived bytes remain authoritative even if an active plan
		// was later formatted differently without changing its approval hash.
		sourceBytes, err = archiveReviewedSetupPlanBytes(stateDir, source.PlanHash, sourceBytes)
		if err != nil {
			return err
		}
		request, current, err := prepareCampaignConfigMigration(cfg, stateDir, sourceBytes, sourceConfigBytes, configBytes)
		if err != nil {
			return err
		}
		if _, err := archiveReviewedSetupPlan(stateDir, current); err != nil {
			return err
		}
		path := filepath.Join(campaignConfigMigrationRoot(stateDir, current.CampaignConfigMigrationHash), "request.json")
		if _, err := writeRuntimeEvidenceSetupV2(ctx, path, request, campaignConfigMigrationMaximumBytes); err != nil {
			return err
		}
		return printResult(options.Format, map[string]any{"request": request, "migration_hash": current.CampaignConfigMigrationHash, "plan_hash": current.PlanHash, "request_path": path}, nil)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		return err
	}
	defer journal.Close()
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return err
	}
	for id := 1; id <= 2; id++ {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return err
		}
	}
	root := campaignConfigMigrationRoot(stateDir, options.ConfigMigrationHash)
	var request campaignConfigMigrationRequest
	if _, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "request.json"), campaignConfigMigrationMaximumBytes, &request); err != nil {
		return err
	}
	requestHash, err := canonicalHashHex(request)
	if err != nil || requestHash != options.ConfigMigrationHash || request.SourcePlanHash != options.PlanHash || !bytes.Equal(configBytes, request.ConfigBytes) || request.StateDir != stateDir {
		return errors.Join(errors.New("campaign migration review or target yaml changed before apply"), err)
	}
	sourceBytes, err := readSetupPlanBytes(stateDir, "plans/"+stringsTrim0x(request.SourcePlanHash)+".json")
	if err != nil || bytesSHA256(sourceBytes) != request.SourcePlanBytesSha256 {
		return errors.Join(errors.New("campaign migration source archive changed before apply"), err)
	}
	prepared, current, err := prepareCampaignConfigMigration(cfg, stateDir, sourceBytes, request.SourceConfigBytes, request.ConfigBytes)
	if err != nil || !evidenceRelayContinuationSameJSON(prepared, &request) {
		return errors.Join(errors.New("campaign migration differs from its exact prepared transform"), err)
	}
	reviewedBytes, err := readSetupPlanBytes(stateDir, "plans/"+stringsTrim0x(current.PlanHash)+".json")
	if err != nil {
		return err
	}
	reviewed, err := decodePersistedPlanWire(reviewedBytes)
	if err != nil || !evidenceRelayContinuationSameJSON(current, reviewed) {
		return errors.Join(errors.New("campaign migration reviewed successor changed"), err)
	}
	activeBytes, err := readSetupPlanBytes(stateDir, "plan.json")
	if err != nil {
		return err
	}
	active, err := decodePersistedPlanWire(activeBytes)
	if err != nil || (active.PlanHash != request.SourcePlanHash && active.PlanHash != current.PlanHash) {
		return errors.Join(errors.New("campaign migration active plan changed before apply"), err)
	}
	if cfg.provisionalResume == nil {
		return errors.New("campaign migration lacks authenticated provisional driver provenance")
	}
	provenanceOptions := options
	provenanceOptions.PlanHash = current.PlanHash
	if err := prepareProvisionalResume(ctx, cfg, stateDir, "campaign-config-migration", provenanceOptions, current); err != nil {
		return err
	}
	receipt, err := publishCampaignConfigMigration(ctx, cfg, stateDir, current, request, activeBytes, reviewedBytes,
		func() (*RoleSecrets, error) { return loadExistingProvisionalRoles(cfg, stateDir) }, ctx.Err)
	if err != nil {
		return err
	}
	return printResult(options.Format, receipt, nil)
}

// The caller holds deployment.lock and has authenticated both stopped owners.
// The boundary callback permits deterministic interruption after the immutable
// receipt is durable and before the active pointer changes; it grants no authority.
func publishCampaignConfigMigration(ctx context.Context, cfg *ResolvedConfig, stateDir string, current *SetupPlan, request campaignConfigMigrationRequest, activeBytes, reviewedBytes []byte, loadRoles func() (*RoleSecrets, error), beforeAdvance func() error) (*campaignConfigMigrationReceipt, error) {
	if ctx == nil || current == nil || loadRoles == nil || beforeAdvance == nil {
		return nil, errors.New("campaign migration publication context is incomplete")
	}
	active, err := decodePersistedPlanWire(activeBytes)
	if err != nil || (active.PlanHash != request.SourcePlanHash && active.PlanHash != current.PlanHash) {
		return nil, errors.Join(errors.New("campaign migration publication has a different active approval"), err)
	}
	reviewed, err := decodePersistedPlanWire(reviewedBytes)
	if err != nil || !evidenceRelayContinuationSameJSON(current, reviewed) {
		return nil, errors.Join(errors.New("campaign migration publication has different reviewed bytes"), err)
	}
	root := campaignConfigMigrationRoot(stateDir, current.CampaignConfigMigrationHash)
	// Existing receipt wins after a crash. Its signature is authenticated
	// below before the active plan can be published or reported as applied.
	_, receiptErr := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(root, "receipt.json"), campaignConfigMigrationMaximumBytes)
	if validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(receiptErr) {
		roles, err := loadRoles()
		if err != nil {
			return nil, err
		}
		key, err := crypto.HexToECDSA(strings.TrimPrefix(roles.EVM["testnet-owner"].PrivateKeyHex, "0x"))
		if err != nil || crypto.PubkeyToAddress(key.PublicKey) != common.HexToAddress(current.Roles.Owner) {
			return nil, errors.Join(errors.New("campaign migration signing owner differs"), err)
		}
		receipt := campaignConfigMigrationReceipt{Request: request, PlanHash: current.PlanHash}
		receipt.Hash, receipt.Signature, err = coordinatorRepairSignature(receipt, key)
		if err != nil {
			return nil, err
		}
		if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "receipt.json"), &receipt, campaignConfigMigrationMaximumBytes); err != nil {
			return nil, err
		}
	} else if receiptErr != nil {
		return nil, receiptErr
	}
	_, _, receipt, err := authenticatedCampaignConfigMigrationSource(ctx, cfg, stateDir, current, request.SourcePlanHash)
	if err != nil {
		return nil, err
	}
	latest, err := readSetupPlanBytes(stateDir, "plan.json")
	if err != nil || !bytes.Equal(latest, activeBytes) {
		return nil, errors.Join(errors.New("campaign migration active plan changed during apply"), err)
	}
	if err := beforeAdvance(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), reviewedBytes, 0o600); err != nil {
		return nil, err
	}
	return receipt, nil
}

// Keep strict acceptance independent of this explicitly provisional migration.
func validateCampaignConfigMigrationAdmission(plan *SetupPlan, retainRelease bool) error {
	if plan != nil && plan.CampaignConfigMigrationHash != "" && !retainRelease {
		return errors.New("campaign config migration permits provisional continuation only")
	}
	return nil
}
