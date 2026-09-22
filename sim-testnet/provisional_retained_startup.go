//go:build linux || darwin

package main

// Restart an authenticated topology after a local-only successor approval.
// Runtime inputs, chain actions and signed validator namespaces stay retained.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The startup dependency owns processes only; there is no setup-action callback.
type retainedProvisionalStarter func(context.Context, *Executor, *provisionalStoppedTopology, map[string]string) error

// A crash between publishing a manifest and starting its owner is recoverable
// only while the original stopped state is byte-identical and no owner exists.
type retainedStartupPublication struct {
	Schema         string                      `json:"schema"`
	PlanHash       string                      `json:"plan_hash"`
	ProvenancePath string                      `json:"provenance_path"`
	ProvenanceHash string                      `json:"provenance_hash"`
	Source         *provisionalStoppedTopology `json:"source"`
	StateBytes     []byte                      `json:"stopped_state_bytes"`
	Original       []byte                      `json:"original_manifest"`
	Successor      []byte                      `json:"successor_manifest"`
}

// Recover publication only, never a running generation or a transaction. The
// ordinary exact stopped/startup admission runs again after this rollback.
func recoverRetainedProvisionalPublication(ctx context.Context, self *Executor) error {
	if !provisionalResumeEnabled(self.cfg) || self.cfg.provisionalResume.Record.Command != "resume" {
		return nil
	}
	raw, err := readValidatorEvidenceHistoricalFile(self.stateDir, "provisional-resumes/retained-start-publication.json", maximumCampaignEvidenceRawFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var publication retainedStartupPublication
	if err := json.Unmarshal(raw, &publication); err != nil {
		return err
	}
	if publication.Schema != "urnetwork-sim-retained-start-publication-v1" || publication.PlanHash != self.plan.PlanHash {
		return nil
	}
	active, err := readValidatorEvidenceHistoricalFile(self.stateDir, "plan.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	if admitted, err := self.authenticateProvisionalPlanOnlyAdoption(ctx, active); err != nil || !admitted {
		return errors.Join(errors.New("retained publication has no current successor authority"), err)
	}
	relative, err := filepath.Rel(self.stateDir, publication.ProvenancePath)
	if err != nil {
		return err
	}
	provenance, err := readValidatorEvidenceHistoricalFile(self.stateDir, filepath.ToSlash(relative), maximumCampaignEvidenceRawFileBytes)
	var record provisionalResumeRecord
	if err != nil || bytesSHA256(provenance) != publication.ProvenanceHash {
		return errors.Join(errors.New("retained publication provenance changed"), err)
	}
	if err := json.Unmarshal(provenance, &record); err != nil || !record.Provisional || record.FinalAcceptance || record.Command != "resume" || record.PlanHash != self.plan.PlanHash || record.ConfigHash != self.cfg.ConfigHash || record.ReleaseLockHash != self.plan.ReleaseLockHash || record.DeploymentID != self.plan.DeploymentID {
		return errors.Join(errors.New("retained publication approval differs"), err)
	}
	if publication.Source == nil || bytesSHA256(publication.Original) != publication.Source.ManifestBytesSHA256 {
		return errors.New("retained publication original manifest changed")
	}
	var oldManifest, successor SupervisorFile
	if err := json.Unmarshal(publication.Original, &oldManifest); err != nil {
		return err
	}
	if err := json.Unmarshal(publication.Successor, &successor); err != nil {
		return err
	}
	oldHash, err := canonicalHashHex(oldManifest)
	if err != nil || oldHash != publication.Source.ManifestHash || successor.BinaryHash != record.Driver.ExecutableSHA256 || successor.DeploymentID != self.plan.DeploymentID {
		return errors.Join(errors.New("retained publication supervisor identity differs"), err)
	}
	currentManifest, err := readValidatorEvidenceHistoricalFile(self.stateDir, "supervisor.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	if bytes.Equal(currentManifest, publication.Original) {
		return nil
	}
	if !bytes.Equal(currentManifest, publication.Successor) {
		return errors.New("retained publication manifest differs from both recorded generations")
	}
	currentState, err := readValidatorEvidenceHistoricalFile(self.stateDir, "supervisor.state.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(currentState, publication.StateBytes) {
		return nil
	}
	if err := strictHistorySupervisorStopped(self.stateDir); err != nil {
		return err
	}
	var state SupervisorState
	if err := json.Unmarshal(currentState, &state); err != nil {
		return err
	}
	name, err := persistentSupervisorServiceName(self.plan.DeploymentID)
	if err != nil {
		return err
	}
	service, err := readSupervisorServiceStatus(ctx, SupervisorService{Schema: "urnetwork-sim-supervisor-service-v1", Name: name})
	if err != nil {
		return err
	}
	if err := provisionalStoppedTopologyEligible(self.cfg, "resume", oldManifest, oldHash, state, service); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(self.stateDir, "supervisor.json"), publication.Original, 0o600)
}

// A current approved non-transaction successor can restart retained processes
// without interpreting unfinished setup as an instruction to reconcile it.
func executeRetainedProvisionalResume(ctx context.Context, self *Executor, stopped *provisionalStoppedTopology, bins map[string]string, start retainedProvisionalStarter) error {
	if ctx == nil || self == nil || stopped == nil || start == nil || !provisionalResumeEnabled(self.cfg) || self.cfg.provisionalResume.Record.Command != "resume" || self.cfg.strictHistoryAdoption != nil {
		return errors.New("retained startup requires explicit non-accepting resume")
	}
	active, err := readValidatorEvidenceHistoricalFile(self.stateDir, "plan.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	admitted, err := self.authenticateProvisionalPlanOnlyAdoption(ctx, active)
	if err != nil || !admitted {
		return errors.Join(errors.New("retained startup lost its exact local-only successor approval"), err)
	}
	if err := self.verifyProvisionalActionHistory(ctx); err != nil {
		return err
	}
	if err := validateRetainedProvisionalTransactionOutcomes(self.plan, self.journal.Entries()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return start(ctx, self, stopped, bins)
}

// Preserve every process input except the actual qualified executable, its
// explicit current API approval, and the separately authenticated V2 handoff.
func retainedProvisionalSupervisor(cfg *ResolvedConfig, plan *SetupPlan, stopped *provisionalStoppedTopology, raw []byte, bins map[string]string) (SupervisorFile, error) {
	var manifest SupervisorFile
	if !provisionalResumeEnabled(cfg) || plan == nil || stopped == nil || cfg.provisionalResume.Record.PlanHash != plan.PlanHash || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance || bytesSHA256(raw) != stopped.ManifestBytesSHA256 {
		return manifest, errors.New("retained supervisor lost its exact non-accepting manifest authority")
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, err
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil || hash != stopped.ManifestHash || manifest.BinaryHash != stopped.SupervisorBinarySHA256 || manifest.DeploymentID != plan.DeploymentID {
		return manifest, errors.Join(errors.New("retained supervisor manifest identity changed"), err)
	}
	for _, name := range []string{"sim-testnet", connectServerBinaryName} {
		if bins[name] == "" {
			return manifest, errors.New("retained startup lacks qualified executable")
		}
		hash, err := fileSHA256(bins[name])
		if err != nil || hash != cfg.provisionalResume.Driver.ExecutableSHA256 {
			return manifest, errors.Join(errors.New("retained startup executable differs from admitted driver"), err)
		}
	}
	for index := range manifest.Specs {
		spec := &manifest.Specs[index]
		if spec.Command != bins["sim-testnet"] && spec.Command != bins[connectServerBinaryName] {
			return manifest, fmt.Errorf("retained process %s has an unexpected executable owner", spec.ID)
		}
		if spec.Role == "operator-api" {
			if !plan.allowedPlanHashes()[spec.Env[validatorViewFilterPlanHashEnv]] {
				return manifest, errors.New("retained operator API approval is outside current lineage")
			}
			spec.Env[validatorViewFilterPlanHashEnv] = plan.PlanHash
		}
		if spec.Role == "validator" {
			args := make([]string, 0, len(spec.Args))
			for _, arg := range spec.Args {
				if strings.HasPrefix(arg, "--strict-history-adoption") {
					return manifest, errors.New("retained provisional startup cannot substitute strict history adoption")
				}
				if !strings.HasPrefix(arg, "--provisional-activation-setup=") && !strings.HasPrefix(arg, "--provisional-activation-setup-sha256=") {
					args = append(args, arg)
				}
			}
			spec.Args = args
		}
	}
	manifest.BinaryHash = cfg.provisionalResume.Driver.ExecutableSHA256
	manifest.ProvisionalProviderStartupObservationOnly = true
	return manifest, nil
}

// Reuse exact static inputs and refresh only process ownership/handoff. The
// new supervisor performs its normal local cleanup and independent child start.
func launchRetainedProvisionalTopology(ctx context.Context, self *Executor, stopped *provisionalStoppedTopology, bins map[string]string) (returnErr error) {
	cfg, stateDir := self.cfg, self.stateDir
	current, err := prepareStoppedProvisionalTopology(ctx, cfg, stateDir, "resume")
	if err != nil {
		return err
	}
	if err := provisionalStoppedAdoptionGeneration(stopped, current); err != nil {
		return err
	}
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return err
	}
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return err
		}
	}
	verified, err := verifyRetainedProvisionalRuntimeConfigManifest(cfg, stateDir, self.plan)
	if err != nil {
		return fmt.Errorf("retained runtime inputs: %w", err)
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "supervisor.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	manifest, err := retainedProvisionalSupervisor(cfg, self.plan, stopped, raw, bins)
	if err != nil {
		return err
	}
	if err := attachProvisionalActivationSetup(cfg, stateDir, self.plan, self.roles, manifest.Specs); err != nil {
		return err
	}
	boundary, err := processLogCursors(stateDir, manifest)
	if err != nil {
		return err
	}
	current, err = prepareStoppedProvisionalTopology(ctx, cfg, stateDir, "resume")
	if err != nil {
		return err
	}
	if err := provisionalStoppedAdoptionGeneration(stopped, current); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root := filepath.Dir(cfg.provisionalResume.RecordPath)
	if err := atomicWrite(filepath.Join(root, "retained-supervisor.json"), raw, 0o600); err != nil {
		return err
	}
	record := struct {
		Schema                 string                      `json:"schema"`
		Provisional            bool                        `json:"provisional"`
		FinalAcceptance        bool                        `json:"final_acceptance"`
		PlanHash               string                      `json:"plan_hash"`
		Source                 *provisionalStoppedTopology `json:"stopped_source"`
		RuntimeManifestHash    string                      `json:"runtime_manifest_hash"`
		SetupActionsDispatched int                         `json:"setup_actions_dispatched"`
	}{Schema: "urnetwork-sim-retained-topology-start-v1", Provisional: true, PlanHash: self.plan.PlanHash, Source: stopped, RuntimeManifestHash: verified.ManifestHash}
	if err := writePublicJSON(filepath.Join(root, "retained-topology-start.json"), record); err != nil {
		return err
	}
	wire, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	wire = append(wire, '\n')
	stateBytes, err := readValidatorEvidenceHistoricalFile(stateDir, "supervisor.state.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	publication := retainedStartupPublication{Schema: "urnetwork-sim-retained-start-publication-v1", PlanHash: self.plan.PlanHash,
		ProvenancePath: cfg.provisionalResume.RecordPath, ProvenanceHash: cfg.provisionalResume.RecordHash,
		Source: stopped, StateBytes: stateBytes, Original: raw, Successor: wire}
	publicationBytes, err := json.MarshalIndent(publication, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(stateDir, "provisional-resumes", "retained-start-publication.json"), append(publicationBytes, '\n'), 0o600); err != nil {
		return err
	}
	specPath := filepath.Join(stateDir, "supervisor.json")
	if err := atomicWrite(specPath, wire, 0o600); err != nil {
		return err
	}
	processLogs, err := initializeProcessLogGateAtBoundary(stateDir, manifest, boundary)
	if err != nil {
		return err
	}
	processLogs.provisionalObservationOnly = true
	started := true
	defer func() { returnErr = cleanupFailedPersistentLaunch(stateDir, started, returnErr, StopDeployment) }()
	service, err := startPersistentSupervisor(ctx, cfg, bins["sim-testnet"], stateDir, specPath)
	if err != nil {
		return err
	}
	ready, err := waitSupervisorReadyWithService(ctx, stateDir, manifest, processLogs, supervisorStartupReadinessTimeout(manifest), service, readSupervisorServiceStatus)
	if err != nil {
		preserved, preserveErr := preserveRecoverableProvisionalStartup(ctx, cfg, stateDir, manifest, service, err, false, liveRecordedSupervisor, readSupervisorServiceStatus)
		if preserveErr != nil {
			return errors.Join(err, preserveErr)
		}
		if preserved {
			started = false
		}
		return err
	}
	adoption, err := prepareProvisionalLiveTopology(cfg, stateDir, "resume")
	if err != nil || adoption == nil {
		return errors.Join(errors.New("retained successor supervisor is not live"), err)
	}
	if err := provisionalAdoptionGeneration(adoption, *ready); err != nil {
		return err
	}
	if err := adoptProvisionalLiveTopology(ctx, cfg, stateDir, self.plan, self.roles, self, adoption, true); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "sim-testnet: retained successor topology started; setup_actions_dispatched=0; runtime inputs retained; final_acceptance=false")
	return nil
}
