//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

// This observation names the actual reader, never a replacement release or
// signing identity. It stays in the hashed plan until strict re-capture.
type EvidenceRelayProvisionalCapture struct {
	Record       provisionalResumeRecord `json:"record"`
	RecordPath   string                  `json:"record_path"`
	RecordSHA256 string                  `json:"record_sha256"`
}

func validateProvisionalRelayCaptureOptions(command string, options cliOptions) error {
	if !options.ProvisionalCapture {
		return nil
	}
	if command != "relay-continuation" || options.Apply || options.ProvisionalResume || options.Detach || options.PrepareOnly || options.ThenReleaseCandidate || options.RelayContinuationPlan != "" || options.Name != "" || options.Manifest != "" || options.ProvisionalRPCAuthority != "" || options.RelayEndBlock == 0 || !validCanonicalHashHex(options.PlanHash) || options.OwnedRPCAuthority == "" {
		return errors.New("--provisional-capture requires a read-only relay-continuation, exact active --plan-hash, fixed end and owned LAN RPC")
	}
	return validateOwnedRPCAuthority(options.OwnedRPCAuthority)
}

func provisionalRelayCaptureEnabled(cfg *ResolvedConfig) bool {
	return provisionalResumeEnabled(cfg) && cfg.provisionalResume.Record.ReadOnly && cfg.provisionalResume.Record.Command == "relay-continuation"
}

// Old local intent bytes are checked against the original policy/custody view.
// The current observer's compatibility permission is not a historical config
// fact. This view is never written or handed to a running validator; the hashed
// continuation separately retains the real non-accepting observer provenance.
func relayContinuationHistoryConfig(cfg *ResolvedConfig) (*ResolvedConfig, error) {
	if cfg == nil {
		return nil, errors.New("relay history configuration is unavailable")
	}
	if !provisionalResumeEnabled(cfg) {
		return cfg, nil
	}
	if !cfg.readOnlyAudit || !provisionalRelayCaptureEnabled(cfg) {
		return nil, errors.New("relay history view cannot remove live provisional authority")
	}
	history := *cfg
	history.provisionalResume = nil
	return &history, nil
}

func validateProvisionalRelayCaptureContext(cfg *ResolvedConfig, plan *SetupPlan) error {
	if !provisionalRelayCaptureEnabled(cfg) || !cfg.readOnlyAudit || cfg.Config == nil || cfg.Public == nil || plan == nil || !ownedRPCOnly(cfg) {
		return errors.New("relay preview has no exact read-only owned-node admission")
	}
	if err := validateOwnedRPCPlan(cfg, plan); err != nil {
		return err
	}
	record := cfg.provisionalResume.Record
	if record.Schema != "urnetwork-sim-provisional-resume-v1" || !record.Provisional || record.FinalAcceptance || record.PlanHash != plan.PlanHash || record.ConfigHash != cfg.ConfigHash || record.ConfigHash != plan.ConfigHash || record.DeploymentID != plan.DeploymentID || record.DeploymentID != cfg.Config.Deployment.DeploymentID || record.ReleaseLockHash != plan.ReleaseLockHash || cfg.ChainID != testnetChainID || plan.ChainID != testnetChainID || plan.GenesisHash != testnetGenesis || cfg.Public.Chain.GenesisHash != testnetGenesis || !releaseSHA256.MatchString(cfg.provisionalResume.RecordHash) {
		return errors.New("relay preview changed its non-accepting source authority")
	}
	return nil
}

// Authenticate every existing operational input before creating any provenance.
// Only the release driver may differ, exactly as in explicit provisional setup.
func prepareProvisionalRelayCapture(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) (*ResolvedConfig, *SetupPlan, error) {
	if ctx == nil || cfg == nil || cfg.provisionalResume == nil {
		return nil, nil, errors.New("relay preview actual driver is unavailable")
	}
	if err := validateProvisionalRelayCaptureOptions("relay-continuation", options); err != nil || !options.ProvisionalCapture {
		return nil, nil, errors.Join(errors.New("relay preview explicit admission is required"), err)
	}
	if options.OwnedRPCAuthority != cfg.ownedRPCAuthority {
		return nil, nil, errors.New("relay preview requested route differs from resolved authority")
	}
	plan, err := loadPersistedPlanIdentity(cfg, stateDir, true)
	if err != nil {
		return nil, nil, err
	}
	if err := requireApproved(true, options.PlanHash, plan.PlanHash); err != nil {
		return nil, nil, err
	}
	if err := validateOwnedRPCPlan(cfg, plan); err != nil {
		return nil, nil, err
	}
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return nil, nil, err
	}
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return nil, nil, err
		}
	}
	reader := *cfg
	provenance := *cfg.provisionalResume
	reader.provisionalResume = &provenance
	reader.readOnlyAudit = true
	if err := prepareProvisionalResume(ctx, &reader, stateDir, "relay-continuation", options, plan); err != nil {
		return nil, nil, err
	}
	if err := validateProvisionalRelayCaptureContext(&reader, plan); err != nil {
		return nil, nil, err
	}
	return &reader, plan, nil
}

func validateProvisionalRelayCaptureMarker(plan *SetupPlan) error {
	if plan == nil || plan.EvidenceRelayContinuation == nil || plan.EvidenceRelayContinuation.ProvisionalCapture == nil {
		return nil
	}
	capture := plan.EvidenceRelayContinuation.ProvisionalCapture
	record := capture.Record
	if record.Schema != "urnetwork-sim-provisional-resume-v1" || !record.Provisional || record.FinalAcceptance || !record.ReadOnly || record.Command != "relay-continuation" || record.Scenario != "" || record.PlanHash != plan.EvidenceRelayContinuation.SourcePlanHash || !plan.allowedPlanHashes()[record.PlanHash] || !validCanonicalHashHex(record.ConfigHash) || record.DeploymentID != plan.DeploymentID || !validCanonicalHashHex(record.ReleaseLockHash) || !filepath.IsAbs(capture.RecordPath) || filepath.Clean(capture.RecordPath) != capture.RecordPath || !releaseSHA256.MatchString(capture.RecordSHA256) || !releaseSHA256.MatchString(record.Driver.ExecutableSHA256) || !releaseGitCommit.MatchString(record.Driver.Build.Revision) || record.Driver.Build.PackagePath != "github.com/urfoundation/sn/sim-testnet" || record.Driver.Build.ModulePath != "github.com/urfoundation/sn" || !filepath.IsAbs(record.Driver.ExecutablePath) || plan.OwnedRPCAuthority == "" || plan.ChainID != testnetChainID || plan.GenesisHash != testnetGenesis {
		return errors.New("relay preview provenance grants authority or changes its exact source")
	}
	return validateOwnedRPCAuthority(plan.OwnedRPCAuthority)
}

// A warm plan admission must retain the same private, bounded observation; its
// path cannot borrow another deployment or an unowned filesystem namespace.
func validateProvisionalRelayCaptureSource(stateDir string, plan *SetupPlan) error {
	if err := validateProvisionalRelayCaptureMarker(plan); err != nil {
		return err
	}
	if plan == nil || plan.EvidenceRelayContinuation == nil || plan.EvidenceRelayContinuation.ProvisionalCapture == nil {
		return nil
	}
	capture := plan.EvidenceRelayContinuation.ProvisionalCapture
	relative, err := filepath.Rel(stateDir, capture.RecordPath)
	if err != nil || !strings.HasPrefix(relative, "provisional-resumes"+string(filepath.Separator)) {
		return errors.New("relay preview provenance escapes its source deployment")
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, relative, 64*1024)
	if err != nil {
		return err
	}
	want, err := json.MarshalIndent(capture.Record, "", "  ")
	if err != nil || bytesSHA256(raw) != capture.RecordSHA256 || !bytes.Equal(raw, append(want, '\n')) {
		return errors.Join(errors.New("relay preview actual-driver provenance changed"), err)
	}
	return nil
}

// Strict reconciliation admits a pending marker only to this read-only source
// reader. Its original release, every current input and all capture proofs are
// rechecked. A successful strict capture emits a new marker-free plan; import
// repeats the complete census before adoption.
func prepareStrictRelayCapture(cfg *ResolvedConfig, stateDir string) (*ResolvedConfig, *SetupPlan, error) {
	if cfg == nil || provisionalResumeEnabled(cfg) {
		return nil, nil, errors.New("strict relay capture cannot borrow provisional runtime authority")
	}
	raw, err := readSetupPlanBytes(stateDir, "plan.json")
	if err != nil {
		return nil, nil, err
	}
	source, err := decodePersistedPlanWire(raw)
	if err != nil {
		return nil, nil, err
	}
	reader := *cfg
	reader.readOnlyAudit = true
	reader.relayCapturePlanHash = source.PlanHash
	plan, err := loadPlanIdentityBytes(&reader, raw, false)
	if err != nil {
		return nil, nil, err
	}
	return &reader, plan, nil
}
