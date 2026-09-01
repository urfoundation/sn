package main

import (
	"bytes"
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

// Validate a result independently of its complete marker. This binds the
// release identity, exact executable scenario definition, live epoch span,
// assertions, anomaly ledger, and continuous adversary evidence.
func validateReleaseCampaignResult(cfg *ResolvedConfig, result *ScenarioResult) error {
	if cfg == nil || cfg.Config == nil || result == nil {
		return errors.New("release campaign result context is incomplete")
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		return err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		return err
	}
	if result.Schema != "urnetwork-sim-scenario-result-v1" || result.Release != "1.0" || result.Name != "release-1.0" || result.RunID == "" || result.DeploymentID != cfg.Config.Deployment.DeploymentID || result.ChainID != cfg.ChainID || result.Netuid != cfg.Netuid || !strings.EqualFold(result.GenesisHash, cfg.Public.Chain.GenesisHash) || !strings.EqualFold(result.ConfigHash, cfg.ConfigHash) || !strings.EqualFold(result.PolicyHash, cfg.PolicyHash) {
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
	if result.StartEpoch > ^uint64(0)-definition.GoalEpochs || result.EndEpoch < result.StartEpoch+definition.GoalEpochs {
		return fmt.Errorf("release campaign spans epochs [%d,%d], require %d live epochs", result.StartEpoch, result.EndEpoch, definition.GoalEpochs)
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

func validateReleaseCampaignComplete(cfg *ResolvedConfig, roles *RoleSecrets, runDir string, result *ScenarioResult) (*ReleaseCampaignGate, error) {
	if err := validateReleaseCampaignResult(cfg, result); err != nil {
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
	return &ReleaseCampaignGate{RunID: result.RunID, ResultHash: result.EvidenceHash, CompleteContentHash: complete.ContentHash, StartEpoch: result.StartEpoch, EndEpoch: result.EndEpoch}, nil
}

// Load the newest fully signed passing release result. Failed runs have no
// complete marker and remain available for root-cause analysis, but can never
// authorize production. A malformed signed candidate fails closed.
func loadReleaseCampaignGate(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) (*ReleaseCampaignGate, error) {
	runsDir := filepath.Join(stateDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return nil, fmt.Errorf("read release campaign runs: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var selected *ReleaseCampaignGate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runDir := filepath.Join(runsDir, entry.Name())
		if _, statErr := os.Stat(filepath.Join(runDir, "complete.json")); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return nil, statErr
		}
		var result ScenarioResult
		if err := decodeStrictJSONFile(filepath.Join(runDir, "result.json"), &result); err != nil {
			return nil, fmt.Errorf("decode completed scenario %s: %w", entry.Name(), err)
		}
		if result.Name != "release-1.0" {
			continue
		}
		gate, err := validateReleaseCampaignComplete(cfg, roles, runDir, &result)
		if err != nil {
			return nil, fmt.Errorf("validate completed release campaign %s: %w", entry.Name(), err)
		}
		if selected == nil || gate.EndEpoch > selected.EndEpoch || gate.EndEpoch == selected.EndEpoch && gate.RunID > selected.RunID {
			selected = gate
		}
	}
	if selected == nil {
		return nil, errors.New("no signed, anomaly-clean release-1.0 campaign is complete")
	}
	return selected, nil
}
