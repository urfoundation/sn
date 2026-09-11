package main

// A provisional controller may adopt a live generation it did not create.
// It never rebuilds its images, rewrites runtime inputs, or owns its teardown.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type provisionalLiveTopology struct {
	Schema                              string                      `json:"schema"`
	Provisional                         bool                        `json:"provisional"`
	FinalAcceptance                     bool                        `json:"final_acceptance"`
	FullDoctorSkipped                   bool                        `json:"full_doctor_skipped,omitempty"`
	DeploymentEvidencePublicationWaived bool                        `json:"deployment_evidence_publication_waived"`
	PlanHash                            string                      `json:"plan_hash"`
	StartedAt                           string                      `json:"started_at"`
	CompletedAt                         string                      `json:"completed_at,omitempty"`
	ManifestHash                        string                      `json:"supervisor_manifest_hash"`
	ManifestBytesSHA256                 string                      `json:"supervisor_manifest_bytes_sha256"`
	SupervisorBinarySHA256              string                      `json:"supervisor_binary_sha256"`
	SupervisorPID                       int                         `json:"supervisor_pid"`
	SupervisorStartTimeTicks            uint64                      `json:"supervisor_start_time_ticks"`
	Driver                              provisionalDriverProvenance `json:"actual_driver"`
	ProvenancePath                      string                      `json:"driver_provenance_path"`
	ProcessLogGatePath                  string                      `json:"process_log_gate_path"`
	ProofBaseline                       map[string]int              `json:"proof_baseline"`
	FreshProofStartupWaived             bool                        `json:"fresh_proof_startup_waived"`
	ObservedProofCounts                 map[string]int              `json:"observed_proof_counts,omitempty"`
	ObservedProofCountsAt               string                      `json:"observed_proof_counts_at,omitempty"`
	ObservedProofsVerified              bool                        `json:"observed_proof_counts_verified"`
	VerifiedProofCounts                 map[string]int              `json:"verified_proof_counts,omitempty"`
	PriorRestarts                       map[string]int              `json:"prior_process_restarts"`
	manifest                            SupervisorFile
}

// The full plan retains campaign/retirement reserves after setup is done.
// This guard instead covers every action live adoption can execute. The
// caller has authenticated all verified receipts before consulting it.
func provisionalLiveResumeNeedsDoctor(executor *Executor) (bool, error) {
	if executor == nil || executor.plan == nil || executor.journal == nil || !provisionalResumeEnabled(executor.cfg) {
		return true, nil
	}
	if _, err := postTopologyTournamentActions(executor.plan); err != nil {
		return true, err
	}
	for _, action := range executor.plan.Actions {
		if action.ID == "topology.launch" || action.ID == "churn.tournament-complete" {
			if action.Kind != "local" || !spendIsZero(action.Spend) {
				return true, nil
			}
			if action.ID == "churn.tournament-complete" {
				return false, nil
			}
			continue
		}
		// Even a zero-declared-spend commitment may consume transaction fees.
		// Require its exact verified intent, including every setup prefix.
		if _, verified := executor.verifiedActionEntry(action); !verified {
			return true, nil
		}
	}
	return true, errors.New("live adoption has no completed action boundary")
}

func prepareProvisionalLiveTopology(cfg *ResolvedConfig, stateDir, command string) (*provisionalLiveTopology, error) {
	if command != "resume" || !provisionalResumeEnabled(cfg) {
		return nil, nil
	}
	live, err := liveRecordedSupervisor(stateDir)
	if err != nil || live == nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "supervisor.json"))
	if err != nil {
		return nil, err
	}
	var manifest SupervisorFile
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		return nil, err
	}
	if manifest.Schema != "urnetwork-sim-supervisor-v1" || manifest.DeploymentID != cfg.Config.Deployment.DeploymentID || hash != live.ManifestHash || len(manifest.Specs) == 0 {
		return nil, errors.New("provisional live supervisor manifest identity differs")
	}
	binaryHash, err := fileSHA256(fmt.Sprintf("/proc/%d/exe", live.SupervisorPID))
	if err != nil {
		return nil, err
	}
	if binaryHash != manifest.BinaryHash {
		return nil, errors.New("live supervisor executable differs from its retained manifest")
	}
	if err := validateSupervisorGeneration(*live); err != nil {
		return nil, err
	}
	baseline, err := releaseTopologyProofCounts(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	priorRestarts := map[string]int{}
	for _, process := range live.Processes {
		priorRestarts[process.ID] = process.Restarts
	}
	adoption := &provisionalLiveTopology{
		Schema: "urnetwork-sim-provisional-live-topology-v1", Provisional: true, FinalAcceptance: false,
		DeploymentEvidencePublicationWaived: true,
		PlanHash:                            cfg.provisionalResume.Record.PlanHash, StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ManifestHash: hash, ManifestBytesSHA256: bytesSHA256(raw), SupervisorBinarySHA256: binaryHash,
		SupervisorPID: live.SupervisorPID, SupervisorStartTimeTicks: live.SupervisorStartTimeTicks,
		Driver: cfg.provisionalResume.Driver, ProvenancePath: cfg.provisionalResume.RecordPath,
		ProcessLogGatePath: filepath.Join(filepath.Dir(cfg.provisionalResume.RecordPath), "process-log-gate.json"),
		ProofBaseline:      baseline, FreshProofStartupWaived: true,
		ObservedProofCounts: baseline, ObservedProofCountsAt: time.Now().UTC().Format(time.RFC3339Nano),
		PriorRestarts: priorRestarts, manifest: manifest,
	}
	if err := writeProvisionalLiveTopologyRecord(adoption); err != nil {
		return nil, err
	}
	return adoption, nil
}

func writeProvisionalLiveTopologyRecord(adoption *provisionalLiveTopology) error {
	raw, err := json.MarshalIndent(adoption, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(filepath.Dir(adoption.ProvenancePath), "live-topology.json"), append(raw, '\n'), 0600)
}

func loadExistingProvisionalRoles(cfg *ResolvedConfig, stateDir string) (*RoleSecrets, error) {
	var got RoleSecrets
	if err := readJSONFile(filepath.Join(stateDir, "secrets", "roles.json"), &got); err != nil {
		return nil, err
	}
	expected, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	roles, changed, err := extendRoleSecretsWithContractGenerations(cfg.Config.Topology, &got, expected)
	if err != nil {
		return nil, err
	}
	if changed {
		return nil, errors.New("live topology adoption cannot extend existing role secrets")
	}
	return roles, nil
}

func provisionalAdoptionGeneration(adoption *provisionalLiveTopology, state SupervisorState) error {
	if state.ManifestHash != adoption.ManifestHash || state.SupervisorPID != adoption.SupervisorPID || state.SupervisorStartTimeTicks != adoption.SupervisorStartTimeTicks {
		return errors.New("provisional adopted supervisor generation changed")
	}
	return validateSupervisorGeneration(state)
}

// Provisional launch admits live provider swarms while their members catch up.
// Keep their recorded health unchanged: full health remains a scenario assertion.
// Every retained identity and PID must still be present and alive, and every
// non-provider process must retain its ordinary health requirement.
func provisionalSupervisorStateReady(state SupervisorState, wantHash string, specs []ProcessSpec) bool {
	if state.Schema != "urnetwork-sim-supervisor-state-v1" || state.ManifestHash != wantHash || validateSupervisorGeneration(state) != nil || len(state.Processes) != len(specs) {
		return false
	}
	want := make(map[string]ProcessSpec, len(specs))
	for _, spec := range specs {
		if spec.ID == "" || want[spec.ID].ID != "" {
			return false
		}
		want[spec.ID] = spec
	}
	for _, process := range state.Processes {
		spec, ok := want[process.ID]
		if !ok || process.Role != spec.Role || process.Identity != spec.Identity || process.PID <= 1 || syscall.Kill(process.PID, syscall.Signal(0)) != nil {
			return false
		}
		if spec.Role != "miner-swarm" && !process.Healthy {
			return false
		}
		delete(want, process.ID)
	}
	return len(want) == 0
}

func provisionalSupervisorReadyNow(stateDir string, want SupervisorFile) (bool, error) {
	hash, err := canonicalHashHex(want)
	if err != nil {
		return false, err
	}
	var state SupervisorState
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.state.json"), &state); err != nil {
		return false, err
	}
	return provisionalSupervisorStateReady(state, hash, want.Specs), nil
}

func provisionalProofsAdvanced(baseline, current map[string]int) bool {
	if len(baseline) == 0 || len(current) != len(baseline) {
		return false
	}
	for identity, count := range baseline {
		if now, present := current[identity]; !present || now <= count {
			return false
		}
	}
	return true
}

func newProvisionalProcessLogGate(stateDir string, adoption *provisionalLiveTopology) (*processLogGate, error) {
	cursors, err := processLogCursors(stateDir, adoption.manifest)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	gate := &processLogGate{stateDir: stateDir, path: adoption.ProcessLogGatePath, provisionalObservationOnly: true, state: processLogGateState{
		Schema: processLogGateSchema, Classifier: processLogClassifierVersion, DeploymentID: adoption.manifest.DeploymentID,
		ManifestHash: adoption.ManifestHash, SupervisorPID: adoption.SupervisorPID, SupervisorStartTimeTicks: adoption.SupervisorStartTimeTicks,
		GeneratedAt: now, UpdatedAt: now, Cursors: cursors, Findings: []ProcessLogFinding{},
	}}
	if err := gate.persistWithLock(); err != nil {
		return nil, err
	}
	return gate, nil
}

func provisionalVerifiedProofCounts(ctx context.Context, cfg *ResolvedConfig, stateDir string, manifest SupervisorFile) (map[string]int, error) {
	operators := make([]OperatorObservation, 0, cfg.Config.Topology.Operators)
	client := &http.Client{Timeout: 5 * time.Second}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		base := ""
		for _, spec := range manifest.Specs {
			if spec.ID == fmt.Sprintf("operator-%d-api", operator) {
				base = strings.TrimSuffix(spec.HealthURL, "/status")
			}
		}
		if base == "" {
			return nil, fmt.Errorf("operator %d API endpoint absent from retained topology", operator)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/verify/keys", nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		var keys struct {
			Keys []VerifyKeyObservation `json:"keys"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&keys)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || closeErr != nil || len(keys.Keys) == 0 {
			return nil, errors.New("operator verification key history unavailable")
		}
		operators = append(operators, OperatorObservation{NoID: operator, VerifyKeys: keys.Keys})
	}
	counts := map[string]int{}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		verified, err := inspectValidatorPathProofs(cfg, stateDir, validator, operators)
		if err != nil {
			return nil, err
		}
		for operator, count := range verified {
			counts[fmt.Sprintf("validator-%d/no-%d", validator, operator)] = count
		}
	}
	return counts, nil
}

func adoptProvisionalLiveTopology(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, roles *RoleSecrets, executor *Executor, adoption *provisionalLiveTopology) error {
	var topology *Action
	for i := range plan.Actions {
		action := plan.Actions[i]
		if action.ID == "topology.launch" {
			topology = &plan.Actions[i]
			break
		}
		prior, ok := executor.verifiedActionEntry(action)
		if !ok {
			return fmt.Errorf("live adoption requires already verified setup action %s", action.ID)
		}
		if err := executor.authenticateProvisionalReceipt(action, prior); err != nil {
			return err
		}
	}
	if topology == nil {
		return errors.New("approved plan has no topology.launch action")
	}
	gate, err := newProvisionalProcessLogGate(stateDir, adoption)
	if err != nil {
		return err
	}
	// Bound only actual process readiness. Journal and topology actions below
	// keep the caller's context rather than inheriting this short startup bound.
	readinessCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	adoption.FreshProofStartupWaived = true
	adoption.ObservedProofsVerified = false
	adoption.VerifiedProofCounts = nil
	fmt.Fprintf(os.Stderr, "sim-testnet: adopting provisional live topology pid=%d start=%d; waiting up to 30s for live processes and non-provider health; fresh_proof_startup_waived=true; final_acceptance=false; prior generation retained on failure\n", adoption.SupervisorPID, adoption.SupervisorStartTimeTicks)
	lastProviderHealth := ""
	for {
		var live SupervisorState
		if err := readJSONFile(filepath.Join(stateDir, "supervisor.state.json"), &live); err != nil {
			return err
		}
		if err := provisionalAdoptionGeneration(adoption, live); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(stateDir, "supervisor.json"))
		if err != nil {
			return err
		}
		if bytesSHA256(raw) != adoption.ManifestBytesSHA256 {
			return errors.New("adopted supervisor manifest bytes changed")
		}
		current, err := releaseTopologyProofCounts(cfg, stateDir)
		if err != nil {
			return err
		}
		adoption.ObservedProofCounts = current
		adoption.ObservedProofCountsAt = time.Now().UTC().Format(time.RFC3339Nano)
		liveReady := provisionalSupervisorStateReady(live, adoption.ManifestHash, adoption.manifest.Specs)
		if liveReady {
			healthy, total := 0, 0
			for _, process := range live.Processes {
				if process.Role == "miner-swarm" {
					total++
					if process.Healthy {
						healthy++
					}
				}
			}
			observed := fmt.Sprintf("%d/%d", healthy, total)
			if healthy < total && observed != lastProviderHealth {
				fmt.Fprintf(os.Stderr, "sim-testnet: provisional partial provider readiness; healthy_swarms=%s; all retained processes alive; final_acceptance=false\n", observed)
			}
			lastProviderHealth = observed
		}
		// Proof production is actual run progress, not provisional admission.
		// Preserve the baseline and unverified counts; scenario observation and
		// completion still validate actual proofs without manufacturing coverage.
		if liveReady {
			var healthSpecs []ProcessSpec
			for _, spec := range adoption.manifest.Specs {
				if spec.HealthURL != "" && spec.Role != "miner-swarm" {
					healthSpecs = append(healthSpecs, spec)
				}
			}
			if err := waitSpecsReady(readinessCtx, healthSpecs, 30*time.Second); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "sim-testnet: provisional fresh proof startup waived; proof_baseline=%v observed_proof_counts=%v observed_proof_counts_verified=false; final_acceptance=false\n", adoption.ProofBaseline, adoption.ObservedProofCounts)
			break
		}
		select {
		case <-readinessCtx.Done():
			return fmt.Errorf("provisional live topology process readiness: %w", readinessCtx.Err())
		case <-time.After(time.Second):
		}
	}
	cancel()
	if err := gate.RequireClean(false); err != nil {
		return err
	}
	if err := executor.Execute(ctx, *topology); err != nil {
		return err
	}
	// Keep the approved tournament writes, while omitting its strict second
	// proof-generation wait under the explicit provisional testnet waiver.
	if err := executePostTopologyTournament(ctx, plan, executor); err != nil {
		return err
	}
	// Public deployment publication revalidates superseded historical evidence.
	// The provisional run-first waiver preserves those files and prior errors
	// without creating a replacement manifest or claiming publication succeeded.
	adoption.DeploymentEvidencePublicationWaived = true
	fmt.Fprintln(os.Stderr, "sim-testnet: provisional deployment_evidence_publication_waived=true; retained public evidence unchanged; final_acceptance=false")
	adoption.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeProvisionalLiveTopologyRecord(adoption); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(adoption, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(stateDir, "provisional-resumes", "live-topology.json"), append(raw, '\n'), 0600)
}

func loadProvisionalOrStrictProcessLogGate(cfg *ResolvedConfig, stateDir string) (*processLogGate, error) {
	gate, err := loadProvisionalOrStrictProcessLogGateState(cfg, stateDir)
	if err == nil && gate != nil {
		gate.provisionalObservationOnly = provisionalResumeEnabled(cfg)
	}
	return gate, err
}

func loadProvisionalOrStrictProcessLogGateState(cfg *ResolvedConfig, stateDir string) (*processLogGate, error) {
	if !provisionalResumeEnabled(cfg) {
		return loadLiveProcessLogGate(stateDir)
	}
	var adoption provisionalLiveTopology
	if err := readJSONFile(filepath.Join(stateDir, "provisional-resumes", "live-topology.json"), &adoption); errors.Is(err, os.ErrNotExist) {
		return loadLiveProcessLogGate(stateDir)
	} else if err != nil {
		return nil, err
	}
	if adoption.Schema != "urnetwork-sim-provisional-live-topology-v1" || !adoption.Provisional || adoption.FinalAcceptance || adoption.PlanHash != cfg.provisionalResume.Record.PlanHash || adoption.CompletedAt == "" {
		return nil, errors.New("provisional topology handoff is incomplete or differs from this plan")
	}
	relative, err := filepath.Rel(filepath.Join(stateDir, "provisional-resumes"), adoption.ProcessLogGatePath)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return nil, errors.New("provisional process log gate is outside retained provenance")
	}
	var manifest SupervisorFile
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.json"), &manifest); err != nil {
		return nil, err
	}
	var live SupervisorState
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.state.json"), &live); err != nil {
		return nil, err
	}
	if err := provisionalAdoptionGeneration(&adoption, live); err != nil {
		return nil, err
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		return nil, err
	}
	if hash != adoption.ManifestHash || !provisionalSupervisorStateReady(live, hash, manifest.Specs) {
		return nil, errors.New("provisional log gate requires its exact live supervisor and healthy non-provider processes")
	}
	var state processLogGateState
	if err := readJSONFile(adoption.ProcessLogGatePath, &state); err != nil {
		return nil, err
	}
	if state.Schema != processLogGateSchema || state.Classifier != processLogClassifierVersion || state.DeploymentID != manifest.DeploymentID || state.ManifestHash != hash {
		return nil, errors.New("provisional process log gate identity differs")
	}
	if err := validatePersistedProcessLogGate(state); err != nil {
		return nil, err
	}
	expected, err := processLogCursorsWithoutOffsets(stateDir, manifest)
	if err != nil {
		return nil, err
	}
	if !sameProcessLogCursorInventory(state.Cursors, expected) {
		return nil, errors.New("provisional process log cursor inventory differs")
	}
	gate := &processLogGate{stateDir: stateDir, path: adoption.ProcessLogGatePath, state: state}
	if err := gate.bindWithLock(live); err != nil {
		return nil, err
	}
	return gate, nil
}
