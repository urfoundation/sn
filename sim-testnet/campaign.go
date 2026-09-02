package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

const releaseCandidateCampaignName = "release-candidate"

// ReleaseCampaignGate is the authenticated live-topology interval which must
// complete before the simulator may schedule production cadence.
type ReleaseCampaignGate struct {
	RunID               string
	ResultHash          string
	CompleteContentHash string
	StartEpoch          uint64
	EndEpoch            uint64
}

type scenarioCompletePayload struct {
	ResultHash string            `json:"result_hash"`
	Files      map[string]string `json:"files"`
}

func decodeStrictJSONBytes(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON has a trailing value")
		}
		return err
	}
	return nil
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if !strings.EqualFold(value, b[key]) {
			return false
		}
	}
	return true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// validateScenarioAcceptanceResult independently reconstructs the complete
// epoch and terminal-finalization boundaries authenticated by a campaign
// result. It rejects partial baseline epochs and faults outside the window.
func validateScenarioAcceptanceResult(cfg *ResolvedConfig, definition scenarioDefinition, result *ScenarioResult) error {
	if cfg == nil || cfg.Policy == nil || result == nil || result.AcceptanceWindow == nil {
		return errors.New("release campaign has no complete-epoch acceptance window")
	}
	window := result.AcceptanceWindow
	wantEpochs := uint64(cfg.Config.Scenarios.ShortEpochs)
	wantBlocks := cfg.Policy.Settlement.EpochBlocks
	wantFinalize := cfg.Policy.Settlement.FinalizeOffsetBlocks
	if definition.Name == "production-soak" {
		wantEpochs = uint64(cfg.Config.Scenarios.ProductionEpochs)
		wantBlocks = cfg.Policy.ProductionCadence.EpochBlocks
		wantFinalize = cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
		if window.PolicyEffectiveEpoch == 0 || window.PolicyEffectiveEpoch > window.BaselineEpoch {
			return errors.New("production acceptance window does not bind an active production policy")
		}
	}
	if window.Schema != "urnetwork-sim-acceptance-window-v1" || window.EpochCount != wantEpochs || definition.GoalEpochs != wantEpochs || window.EpochBlocks != wantBlocks || window.FinalizeOffsetBlocks != wantFinalize {
		return fmt.Errorf("release acceptance geometry=%s/%d/%d/%d want epochs/blocks/finalize=%d/%d/%d", window.Schema, window.EpochCount, window.EpochBlocks, window.FinalizeOffsetBlocks, wantEpochs, wantBlocks, wantFinalize)
	}
	firstEpoch, ok := checkedAdd(window.BaselineEpoch, 1)
	if !ok || window.FirstEpoch != firstEpoch || window.BaselineEpoch != result.StartEpoch || window.BaselineHead != result.StartHead || window.BaselineObservationHash == "" {
		return errors.New("release acceptance baseline does not match the result start")
	}
	if _, err := decodeHex32("release acceptance baseline observation hash", window.BaselineObservationHash); err != nil {
		return err
	}
	if window.StartBlock <= window.BaselineHead.Number || window.StartBlock-window.BaselineHead.Number > window.EpochBlocks {
		return errors.New("release acceptance start is not the next complete epoch boundary")
	}
	span, ok := checkedMul(window.EpochCount, window.EpochBlocks)
	if !ok {
		return errors.New("release acceptance span overflows uint64")
	}
	endBlock, ok := checkedAdd(window.StartBlock, span)
	if !ok || window.EndBlock != endBlock {
		return errors.New("release acceptance end block is not canonical")
	}
	terminalBlock, ok := checkedAdd(window.EndBlock, window.FinalizeOffsetBlocks)
	if !ok || window.TerminalBlock != terminalBlock || result.EndHead.Number < terminalBlock {
		return errors.New("release acceptance terminal finalization is incomplete")
	}
	endEpoch, ok := checkedAdd(window.FirstEpoch, window.EpochCount)
	if !ok || result.EndEpoch < endEpoch {
		return fmt.Errorf("release campaign end epoch %d precedes accepted boundary %d", result.EndEpoch, endEpoch)
	}
	if result.CampaignStartHead.Number == 0 || result.CampaignStartHead.Number > result.StartHead.Number || result.CampaignStartEpoch > result.StartEpoch {
		return errors.New("release campaign preparation interval is invalid")
	}
	if _, err := decodeHex32("release campaign initial finalized hash", result.CampaignStartHead.Hash); err != nil {
		return err
	}
	for _, fault := range result.Faults {
		if fault.TriggerBlock < window.StartBlock || fault.RestoreBlock > window.EndBlock || fault.AppliedBlock < window.StartBlock || fault.RestoredBlock > window.EndBlock {
			return fmt.Errorf("release fault %s is outside the complete-epoch acceptance window", fault.ID)
		}
	}
	return nil
}

// validateScenarioCampaignResult validates one release-phase result
// independently of its complete marker. The expected name selects the exact
// executable definition, epoch span, faults, and adversarial evidence.
func validateScenarioCampaignResult(cfg *ResolvedConfig, result *ScenarioResult, name string) error {
	if cfg == nil || cfg.Config == nil || result == nil || strings.TrimSpace(name) == "" {
		return errors.New("release campaign result context is incomplete")
	}
	definition, err := scenarioDefinitionFor(cfg, name)
	if err != nil {
		return err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		return err
	}
	if result.Schema != "urnetwork-sim-scenario-result-v1" || result.Release != "1.0" || result.Name != name || result.RunID == "" || result.DeploymentID != cfg.Config.Deployment.DeploymentID || result.ChainID != cfg.ChainID || result.Netuid != cfg.Netuid || !strings.EqualFold(result.GenesisHash, cfg.Public.Chain.GenesisHash) || !strings.EqualFold(result.ConfigHash, cfg.ConfigHash) || !strings.EqualFold(result.PolicyHash, cfg.PolicyHash) {
		return errors.New("release campaign result identity does not match the approved deployment")
	}
	if !strings.EqualFold(result.ScenarioDefinition, definitionHash) || !strings.EqualFold(result.ScenarioMatrix, definition.MatrixHash) || !strings.EqualFold(result.AdversarialMatrix, definition.AdversarialMatrixHash) || result.Adversaries == nil {
		return errors.New("release campaign result does not bind the approved checks, faults, and adversaries")
	}
	adversaries := result.Adversaries
	adversaryConfig := cfg.Config.Scenarios.Adversaries
	if adversaries.Schema != "urnetwork-adversary-campaign-v1" || adversaries.Release != "1.0" || adversaries.Status != "stopped" || !strings.EqualFold(adversaries.MatrixHash, definition.AdversarialMatrixHash) || adversaries.Seed != adversaryConfig.Seed || adversaries.MinimumSamplesPerActor != adversaryConfig.MinimumSamplesPerActor || adversaries.MaximumActorErrorRatePPM != adversaryConfig.MaximumActorErrorRatePPM || adversaries.MaximumP99Milliseconds != adversaryConfig.MaximumP99LatencyMilliseconds || adversaries.MaximumAttackControlRatio != adversaryConfig.MaximumAttackControlP95Ratio || adversaries.OperatorRequestCeilingQPS != adversaryConfig.MaximumOperatorRequestsPerSec || adversaries.RPCRequestCeilingQPS != adversaryConfig.MaximumRPCRequestsPerSec {
		return errors.New("release campaign adversary evidence does not match the approved limits")
	}
	if err := validateScenarioAcceptanceResult(cfg, definition, result); err != nil {
		return err
	}
	if result.StartHead.Number == 0 || result.EndHead.Number < result.StartHead.Number {
		return errors.New("release campaign finalized-head interval is invalid")
	}
	if _, err := decodeHex32("release start finalized hash", result.StartHead.Hash); err != nil {
		return err
	}
	if _, err := decodeHex32("release end finalized hash", result.EndHead.Hash); err != nil {
		return err
	}
	started, startErr := time.Parse(time.RFC3339Nano, result.StartedAt)
	completed, completeErr := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if startErr != nil || completeErr != nil || completed.Before(started) {
		return errors.New("release campaign wall-clock interval is invalid")
	}
	if result.Result != "pass" || result.AssertionCount != len(result.Assertions) || result.FailedAssertionCount != 0 || len(result.Assertions) == 0 {
		return errors.New("release campaign result is not an assertion-clean pass")
	}
	seenAssertions := map[string]bool{}
	for _, assertion := range result.Assertions {
		if assertion.ID == "" || seenAssertions[assertion.ID] || !assertion.Passed {
			return errors.New("release campaign has a missing, duplicate, or failed assertion")
		}
		seenAssertions[assertion.ID] = true
	}
	for _, check := range definition.Checks {
		if !seenAssertions[check.ID] {
			return fmt.Errorf("release campaign is missing approved assertion %s", check.ID)
		}
	}
	if !seenAssertions["faults_within_acceptance_window"] {
		return errors.New("release campaign is missing its fault-window assertion")
	}
	if len(result.Faults) != len(definition.Faults) {
		return fmt.Errorf("release campaign has %d fault records, want %d", len(result.Faults), len(definition.Faults))
	}
	faultSpecs := make(map[string]scenarioFaultSpec, len(definition.Faults))
	for _, fault := range definition.Faults {
		faultSpecs[fault.ID] = fault
		if !seenAssertions["fault_"+fault.ID] {
			return fmt.Errorf("release campaign is missing fault assertion %s", fault.ID)
		}
	}
	for _, record := range result.Faults {
		spec, ok := faultSpecs[record.ID]
		if !ok || record.Kind != spec.Kind || !stringSlicesEqual(record.Targets, spec.Targets) || !stringSlicesEqual(record.Impacts, spec.Impacts) {
			return fmt.Errorf("release campaign fault record %s does not match its approved spec", record.ID)
		}
	}
	for _, assertion := range appendFaultAssertions(nil, result.Faults, started, &ScenarioObservation{}) {
		if !assertion.Passed || !seenAssertions[assertion.ID] {
			return fmt.Errorf("release campaign fault evidence failed or omitted assertion %s", assertion.ID)
		}
	}
	for _, assertion := range adversaryAssertions(result.Adversaries, started, "") {
		if !assertion.Passed || !seenAssertions[assertion.ID] {
			return fmt.Errorf("release campaign adversary evidence failed or omitted assertion %s", assertion.ID)
		}
	}
	for _, required := range []string{"adversaries_overlap_happy_path", "adversary_matrix_coverage", anomalyGateAssertionID} {
		if !seenAssertions[required] {
			return fmt.Errorf("release campaign is missing required assertion %s", required)
		}
	}
	if result.Anomalies == nil || result.Anomalies.Schema != "urnetwork-sim-anomaly-ledger-v1" || result.Anomalies.Release != "1.0" || result.Anomalies.RunID != result.RunID || result.Anomalies.Status != "clean" || len(result.Anomalies.Entries) != 0 || !seenAssertions[anomalyGateAssertionID] {
		return errors.New("release campaign anomaly ledger is not clean and complete")
	}
	wantHash, err := canonicalScenarioResultHash(result)
	if err != nil || !strings.EqualFold(result.EvidenceHash, wantHash) {
		return stateMismatchError(err, "release campaign result hash does not match its canonical content")
	}
	return nil
}

// validateReleaseCampaignResult retains the production-policy gate's narrow
// release-1.0 entry point while sharing the exact verifier with M3 adoption.
func validateReleaseCampaignResult(cfg *ResolvedConfig, result *ScenarioResult) error {
	return validateScenarioCampaignResult(cfg, result, "release-1.0")
}

// validateScenarioCampaignComplete authenticates the result plus the signed
// complete marker and every immutable evidence file named by that marker.
func validateScenarioCampaignComplete(cfg *ResolvedConfig, roles *RoleSecrets, runDir string, result *ScenarioResult, name string) (*ReleaseEvidenceEnvelope, error) {
	if err := validateScenarioCampaignResult(cfg, result, name); err != nil {
		return nil, err
	}
	if roles == nil {
		return nil, errors.New("release campaign gate requires role secrets")
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return nil, errors.New("release campaign gate has no testnet owner role")
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, err
	}
	var complete ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(filepath.Join(runDir, "complete.json"), &complete); err != nil {
		return nil, fmt.Errorf("release complete marker: %w", err)
	}
	if err := verifyEvidence(&complete, &ownerKey.PublicKey); err != nil {
		return nil, fmt.Errorf("release complete marker: %w", err)
	}
	if complete.Kind != "scenario-complete" || complete.RunID != result.RunID || complete.DeploymentID != cfg.Config.Deployment.DeploymentID || complete.ChainID != cfg.ChainID || complete.Netuid != cfg.Netuid || !strings.EqualFold(complete.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return nil, errors.New("release complete marker identity does not match its result")
	}
	var payload scenarioCompletePayload
	if err := decodeStrictJSONBytes(complete.Payload, &payload); err != nil {
		return nil, fmt.Errorf("release complete payload: %w", err)
	}
	if !strings.EqualFold(payload.ResultHash, result.EvidenceHash) || len(payload.Files) == 0 {
		return nil, errors.New("release complete marker does not bind the result hash and files")
	}
	hashes, err := evidenceFileHashes(runDir)
	if err != nil {
		return nil, err
	}
	if !stringMapsEqual(payload.Files, hashes) || hashes["result.json"] == "" || hashes["assertions.json"] == "" || hashes["anomalies.json"] == "" || hashes["adversaries.json"] == "" {
		return nil, errors.New("release complete marker file hashes do not match the immutable run directory")
	}
	return &complete, nil
}

// validateReleaseCampaignComplete converts one fully authenticated release
// result into the narrow gate consumed by production policy scheduling.
func validateReleaseCampaignComplete(cfg *ResolvedConfig, roles *RoleSecrets, runDir string, result *ScenarioResult) (*ReleaseCampaignGate, error) {
	complete, err := validateScenarioCampaignComplete(cfg, roles, runDir, result, "release-1.0")
	if err != nil {
		return nil, err
	}
	return &ReleaseCampaignGate{RunID: result.RunID, ResultHash: result.EvidenceHash, CompleteContentHash: complete.ContentHash, StartEpoch: result.StartEpoch, EndEpoch: result.EndEpoch}, nil
}

var errNoCompletedScenarioCampaign = errors.New("no signed anomaly-clean scenario campaign is complete")

// loadCompletedScenarioCampaign loads the newest fully signed passing result
// for one exact scenario definition. Failed runs have no complete marker and
// remain available for root-cause analysis; a malformed completed candidate
// fails closed instead of being skipped.
func loadCompletedScenarioCampaign(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, name string) (*ScenarioResult, *ReleaseEvidenceEnvelope, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil, errors.New("completed scenario name is empty")
	}
	runsDir := filepath.Join(stateDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("%w: %s", errNoCompletedScenarioCampaign, name)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read scenario campaign runs: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var selected *ScenarioResult
	var selectedComplete *ReleaseEvidenceEnvelope
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runDir := filepath.Join(runsDir, entry.Name())
		if _, statErr := os.Stat(filepath.Join(runDir, "complete.json")); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return nil, nil, statErr
		}
		var result ScenarioResult
		if err := decodeStrictJSONFile(filepath.Join(runDir, "result.json"), &result); err != nil {
			return nil, nil, fmt.Errorf("decode completed scenario %s: %w", entry.Name(), err)
		}
		if result.Name != name {
			continue
		}
		complete, err := validateScenarioCampaignComplete(cfg, roles, runDir, &result, name)
		if err != nil {
			return nil, nil, fmt.Errorf("validate completed %s campaign %s: %w", name, entry.Name(), err)
		}
		if selected == nil || result.EndEpoch > selected.EndEpoch || result.EndEpoch == selected.EndEpoch && result.RunID > selected.RunID {
			copyResult := result
			copyComplete := *complete
			selected = &copyResult
			selectedComplete = &copyComplete
		}
	}
	if selected == nil {
		return nil, nil, fmt.Errorf("%w: %s", errNoCompletedScenarioCampaign, name)
	}
	return selected, selectedComplete, nil
}

// Load the newest fully signed passing release result and convert it into the
// production-policy authorization gate.
func loadReleaseCampaignGate(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) (*ReleaseCampaignGate, error) {
	result, complete, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "release-1.0")
	if err != nil {
		return nil, err
	}
	return &ReleaseCampaignGate{RunID: result.RunID, ResultHash: result.EvidenceHash, CompleteContentHash: complete.ContentHash, StartEpoch: result.StartEpoch, EndEpoch: result.EndEpoch}, nil
}

// scenarioCampaignRunner executes one named scenario with the already-open
// release journal and transaction-capable executor.
type scenarioCampaignRunner func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor) error

// runReleaseCandidateCampaign adopts only fully authenticated completed
// phases, executes the first missing phase, and revalidates its signed marker
// before crossing the M2-to-M3 boundary. A failed M3 run remains immutable and
// a later invocation starts a fresh three-block soak while retaining M2.
func runReleaseCandidateCampaign(ctx context.Context, cfg *ResolvedConfig, stateDir string, journal *Journal, executor *Executor, roles *RoleSecrets, runner scenarioCampaignRunner) error {
	if cfg == nil || cfg.Config == nil || journal == nil || executor == nil || roles == nil || runner == nil {
		return errors.New("release-candidate campaign context is incomplete")
	}
	for _, name := range []string{"release-1.0", "production-soak"} {
		if _, err := scenarioDefinitionFor(cfg, name); err != nil {
			return fmt.Errorf("release-candidate %s definition: %w", name, err)
		}
	}
	if _, _, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "release-1.0"); err != nil {
		if !errors.Is(err, errNoCompletedScenarioCampaign) {
			return err
		}
		if err := runner(ctx, cfg, stateDir, "release-1.0", journal, executor); err != nil {
			return err
		}
		if _, _, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "release-1.0"); err != nil {
			return fmt.Errorf("authenticate completed release-1.0 handoff: %w", err)
		}
	}
	if _, _, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "production-soak"); err == nil {
		return nil
	} else if !errors.Is(err, errNoCompletedScenarioCampaign) {
		return err
	}
	if err := runner(ctx, cfg, stateDir, "production-soak", journal, executor); err != nil {
		return err
	}
	if _, _, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "production-soak"); err != nil {
		return fmt.Errorf("authenticate completed production-soak handoff: %w", err)
	}
	return nil
}
