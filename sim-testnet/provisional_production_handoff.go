package main

// A failed, fully observed testnet interval can authorize only an explicitly
// provisional successor. The original result and all failures remain unchanged;
// this separate owner-signed record is never a scenario-complete marker.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const provisionalProductionGateSchema = "urnetwork-sim-provisional-production-gate-v1"
const provisionalProductionHandoffSchema = "urnetwork-sim-provisional-production-handoff-v1"
const provisionalProductionHandoffKind = "provisional-production-handoff"

type provisionalProductionHandoff struct {
	Schema            string                   `json:"schema"`
	Provisional       bool                     `json:"provisional"`
	FinalAcceptance   bool                     `json:"final_acceptance"`
	PlanHash          string                   `json:"plan_hash"`
	ConfigHash        string                   `json:"config_hash"`
	PolicyHash        string                   `json:"policy_hash"`
	RunID             string                   `json:"run_id"`
	ResultHash        string                   `json:"result_hash"`
	SourceAttemptPath string                   `json:"source_attempt_path"`
	SourceAttemptHash string                   `json:"source_attempt_hash"`
	Files             map[string]string        `json:"files"`
	ObservationBytes  uint64                   `json:"observation_bytes"`
	FailedAssertions  []AssertionRecord        `json:"failed_assertions"`
	LifecycleHandoff  ScenarioLifecycleHandoff `json:"lifecycle_handoff"`
}

func validateProvisionalProductionOptions(command string, options cliOptions) error {
	if options.ProvisionalReleaseRunID == "" {
		return nil
	}
	runID := options.ProvisionalReleaseRunID
	if command != "scenario" || options.Name != "production-soak" || !options.Apply || !options.ProvisionalResume || options.ProvisionalCapture || options.PrepareOnly || options.ThenReleaseCandidate || options.StrictHistoryAdoption != "" || filepath.Base(runID) != runID || strings.TrimSpace(runID) != runID || strings.ContainsAny(runID, "\\\r\n\x00") || !strings.HasSuffix(runID, "-release-1.0") {
		return errors.New("--provisional-release-run-id requires an exact release run and scenario --name production-soak --apply --provisional-resume")
	}
	return nil
}

func provisionalProductionHandoffPath(stateDir, runID string) string {
	return filepath.Join(stateDir, "runs", runID, "provisional-production-handoff.evidence.json")
}

func validateProvisionalProductionAuthority(cfg *ResolvedConfig, planHash string) error {
	if cfg == nil || cfg.Config == nil || !provisionalResumeEnabled(cfg) || cfg.provisionalResume.Record == nil || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance || cfg.provisionalResume.Record.PlanHash != planHash || cfg.provisionalResume.Record.ConfigHash != cfg.ConfigHash || cfg.provisionalResume.Record.DeploymentID != cfg.Config.Deployment.DeploymentID || !validCanonicalHashHex(planHash) {
		return errors.New("provisional production handoff has no exact non-accepting testnet approval")
	}
	return nil
}

func validateProvisionalProductionGateShape(cfg *ResolvedConfig, gate *ReleaseCampaignGate) error {
	if gate == nil || gate.Schema != provisionalProductionGateSchema || gate.RunID == "" || filepath.Base(gate.RunID) != gate.RunID || !validCanonicalHashHex(gate.ResultHash) || gate.CompleteContentHash != "" || !validSHA256ContentHash(gate.ProvisionalHandoffHash) || gate.StartEpoch == 0 || gate.EndEpoch < gate.StartEpoch {
		return errors.New("provisional production gate is incomplete or claims a strict completion")
	}
	if cfg == nil || cfg.provisionalResume == nil || cfg.provisionalResume.Record == nil {
		return errors.New("provisional production gate cannot authorize strict acceptance")
	}
	if err := validateProvisionalProductionAuthority(cfg, cfg.provisionalResume.Record.PlanHash); err != nil {
		return err
	}
	// Reuse the ordinary lifecycle shape/provenance checks without allowing a
	// provisional operational record to masquerade as a strict completion.
	shape := *gate
	shape.Schema, shape.CompleteContentHash, shape.ProvisionalHandoffHash = releaseCampaignGateSchema, gate.ProvisionalHandoffHash, ""
	return validateReleaseCampaignGateShape(cfg, &shape)
}

func provisionalProductionFailedAssertions(result *ScenarioResult) ([]AssertionRecord, error) {
	seen := map[string]bool{}
	failed := []AssertionRecord{}
	for _, assertion := range result.Assertions {
		if assertion.ID == "" || seen[assertion.ID] {
			return nil, errors.New("provisional terminal result has duplicate or missing assertion identities")
		}
		seen[assertion.ID] = true
		if !assertion.Passed {
			failed = append(failed, assertion)
		}
	}
	if result.AssertionCount != len(result.Assertions) || result.FailedAssertionCount != len(failed) || len(failed) == 0 {
		return nil, errors.New("provisional terminal result failure inventory is inconsistent")
	}
	return failed, nil
}

// These checks establish the ownership and chain identity needed for later
// mutations. Other failures remain findings; none is converted into a pass.
func provisionalProductionIntegrityAssertionIDs() []string {
	return []string{"contracts_installed", "native_custody_hotkeys", "policy_hash_matches", "runtime_code_matches", "public_identities", "rao_conservation", "runtime_transfer_minimum_bound"}
}

func provisionalProductionIntegrityAssertions(result *ScenarioResult) error {
	for _, id := range provisionalProductionIntegrityAssertionIDs() {
		found := false
		for _, assertion := range result.Assertions {
			if assertion.ID == id {
				found = assertion.Passed
				break
			}
		}
		if !found {
			return fmt.Errorf("provisional production cannot defer integrity or custody assertion %s", id)
		}
	}
	return nil
}

func validateProvisionalProductionTerminal(cfg *ResolvedConfig, source *scenarioCampaignAttempt, result *ScenarioResult) error {
	if source == nil || result == nil || source.payload.Phase != "release-1.0" || source.payload.AcceptanceBoundary == nil || source.payload.AcceptanceInvalidation == "" || source.payload.AcceptanceInvalidatedAt == "" {
		return errors.New("provisional production requires a terminal owner-signed release interval")
	}
	if err := validateProvisionalProductionAuthority(cfg, source.payload.PlanHash); err != nil {
		return err
	}
	hash, err := canonicalScenarioResultHash(result)
	if err != nil || hash != result.EvidenceHash || result.Schema != "urnetwork-sim-scenario-result-v1" || result.Release != "1.0" || result.RunID != source.payload.RunID || result.Name != "release-1.0" || result.Result != "fail" || !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance || result.StartedAt != source.payload.StartedAt || result.ConfigHash != cfg.ConfigHash || result.PolicyHash != cfg.PolicyHash || result.DeploymentID != cfg.Config.Deployment.DeploymentID || result.ChainID != cfg.ChainID || result.Netuid != cfg.Netuid || !strings.EqualFold(result.GenesisHash, cfg.Public.Chain.GenesisHash) || result.PriorRelease != nil || result.LifecycleHandoff == nil {
		return errors.Join(errors.New("provisional production result differs from its exact failed release source"), err)
	}
	if _, err := provisionalProductionFailedAssertions(result); err != nil {
		return err
	}
	if err := provisionalProductionIntegrityAssertions(result); err != nil {
		return err
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		return err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		return err
	}
	boundary := source.payload.AcceptanceBoundary
	if result.ScenarioDefinition != definitionHash || result.ScenarioMatrix != definition.MatrixHash || result.AdversarialMatrix != definition.AdversarialMatrixHash || boundary.ScenarioDefinitionHash != definitionHash || !scenarioAcceptanceWindowsEqual(result.AcceptanceWindow, &boundary.AcceptanceWindow) || result.CampaignStartHead != boundary.CampaignStartHead || result.CampaignStartEpoch != boundary.CampaignStartEpoch || result.EndHead != boundary.LastObservationHead || result.EndEpoch != boundary.LastObservationEpoch || boundary.LastObservationHead.Number < boundary.AcceptanceWindow.TerminalBlock {
		return errors.New("provisional production requires the entire exact signed release interval through terminal finalization")
	}
	// Fault success remains in the unmodified result. Geometry is still the
	// canonical five complete epochs plus their finalization offset.
	geometry := *result
	geometry.Faults = nil
	if err := validateScenarioAcceptanceResult(cfg, definition, &geometry); err != nil {
		return err
	}
	if err := validateScenarioFaultProgress(boundary.Faults, result.Faults); err != nil {
		return err
	}
	started, startErr := time.Parse(time.RFC3339Nano, boundary.AcceptanceStartedAt)
	completed, endErr := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if startErr != nil || endErr != nil || completed.Before(started) {
		return errors.New("provisional terminal interval has invalid timestamps")
	}
	return nil
}

func readProvisionalProductionSources(ctx context.Context, cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash, runID, sourcePath string, replay bool) (*provisionalProductionHandoff, *ScenarioResult, []byte, error) {
	if ctx == nil {
		return nil, nil, nil, errors.New("provisional production source context absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if runID == "" || filepath.Base(runID) != runID || filepath.IsAbs(sourcePath) || filepath.Clean(sourcePath) != sourcePath || !strings.HasPrefix(sourcePath, "campaign-attempts/") {
		return nil, nil, nil, errors.New("provisional production source paths are noncanonical")
	}
	source, sourceRaw, err := readScenarioCampaignAttemptAtContext(cfg, stateDir, roles, planHash, "release-1.0", filepath.Join(stateDir, sourcePath), false)
	if err != nil {
		return nil, nil, nil, err
	}
	if source.payload.RunID != runID {
		return nil, nil, nil, errors.New("provisional production source run changed")
	}
	runDir := filepath.Join(stateDir, "runs", runID)
	read := func(name string) ([]byte, error) {
		return readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(filepath.Join("runs", runID, name)), maximumCampaignEvidenceRawFileBytes)
	}
	resultRaw, err := read("result.json")
	if err != nil {
		return nil, nil, nil, err
	}
	var result ScenarioResult
	if err := decodeStrictJSONBytes(resultRaw, &result); err != nil {
		return nil, nil, nil, err
	}
	if err := validateProvisionalProductionTerminal(cfg, source, &result); err != nil {
		return nil, nil, nil, err
	}
	start, startRaw, err := readScenarioCampaignAttemptAtContext(cfg, stateDir, roles, planHash, "release-1.0", filepath.Join(runDir, scenarioCampaignStartFilename), false)
	if err != nil {
		return nil, nil, nil, err
	}
	if start.payload.RunID != runID || start.payload.AcceptanceInvalidation != "" || !scenarioCampaignRecoveryStaticBoundaryMatches(start.payload.AcceptanceBoundary, source.payload.AcceptanceBoundary) {
		return nil, nil, nil, errors.New("provisional production start differs from signed terminal source")
	}
	if replay {
		_, _, baseline, terminal, _, err := source.loadAuthenticatedRecoveryRuntimeForensics(runDir)
		if err != nil {
			return nil, nil, nil, err
		}
		// A terminal result is local until this owner signs its handoff. Its
		// claimed passing safety checks cannot override the already signed
		// observation. Reevaluate these checks from that exact terminal prefix.
		definition, err := scenarioDefinitionFor(cfg, "release-1.0")
		if err != nil {
			return nil, nil, nil, err
		}
		for _, id := range provisionalProductionIntegrityAssertionIDs() {
			verified := false
			for _, check := range definition.Checks {
				if check.ID == id {
					verified, _ = check.Check(&scenarioEvaluation{Cfg: cfg, Start: baseline, Current: terminal, Window: result.AcceptanceWindow, Definition: definition})
					break
				}
			}
			if !verified {
				return nil, nil, nil, fmt.Errorf("signed terminal observation fails provisional production integrity assertion %s", id)
			}
		}
	}
	observationHash, observationBytes, err := hashCampaignObservationHistory(stateDir, filepath.ToSlash(filepath.Join("runs", runID, "observations.jsonl")))
	if err != nil {
		return nil, nil, nil, err
	}
	handoff, err := validateScenarioLifecycleHandoffFile(cfg, runDir, *result.LifecycleHandoff)
	if err != nil {
		return nil, nil, nil, err
	}
	failed, _ := provisionalProductionFailedAssertions(&result)
	payload := &provisionalProductionHandoff{Schema: provisionalProductionHandoffSchema, Provisional: true, FinalAcceptance: false, PlanHash: planHash, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, RunID: runID, ResultHash: result.EvidenceHash, SourceAttemptPath: sourcePath, SourceAttemptHash: bytesSHA256(sourceRaw), Files: map[string]string{"result.json": bytesSHA256(resultRaw), scenarioCampaignStartFilename: bytesSHA256(startRaw), "observations.jsonl": observationHash, result.LifecycleHandoff.File: bytesSHA256(handoff)}, ObservationBytes: observationBytes, FailedAssertions: failed, LifecycleHandoff: *result.LifecycleHandoff}
	for _, name := range []string{"assertions.json", "anomalies.json", "faults.json", "adversaries.json", processLogEvidenceFilename} {
		raw, err := read(name)
		if err != nil {
			return nil, nil, nil, err
		}
		payload.Files[name] = bytesSHA256(raw)
	}
	return payload, &result, handoff, ctx.Err()
}

func prepareProvisionalProductionHandoff(ctx context.Context, cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, plan *SetupPlan, journal *Journal, runID string) (*ReleaseCampaignGate, error) {
	if plan == nil || journal == nil || journal.lock == nil || journal.file == nil || journal.path != filepath.Join(stateDir, "journal.jsonl") || cfg.provisionalProductionSourceRunID != runID {
		return nil, errors.New("provisional production handoff requires the explicit source and exclusive deployment journal writer")
	}
	if err := validateProvisionalProductionAuthority(cfg, plan.PlanHash); err != nil {
		return nil, err
	}
	path := provisionalProductionHandoffPath(stateDir, runID)
	if _, err := os.Lstat(path); err == nil {
		gate, _, _, err := readProvisionalProductionHandoff(ctx, cfg, stateDir, roles, runID, nil)
		return gate, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, err := os.Lstat(scenarioCampaignAttemptPath(stateDir, "production-soak")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(errors.New("production already has a different durable predecessor"), err)
	}
	source, err := readScenarioCampaignAttempt(cfg, stateDir, roles, plan.PlanHash, "release-1.0")
	if err != nil {
		return nil, err
	}
	if source.payload.RunID != runID {
		return nil, errors.New("provisional production may adopt only the current exact terminal release attempt")
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "runs", runID, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(errors.New("completed release must use the ordinary production handoff"), err)
	}
	relative, err := filepath.Rel(stateDir, source.path())
	if err != nil {
		return nil, err
	}
	payload, _, _, err := readProvisionalProductionSources(ctx, cfg, stateDir, roles, plan.PlanHash, runID, filepath.ToSlash(relative), true)
	if err != nil {
		return nil, err
	}
	envelope, err := signEvidence(cfg, provisionalProductionHandoffKind, runID, payload, roles.EVM["testnet-owner"])
	if err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, path, raw, maximumCampaignEvidenceRawFileBytes); err != nil {
		return nil, err
	}
	gate, _, _, err := readProvisionalProductionHandoff(ctx, cfg, stateDir, roles, runID, nil)
	return gate, err
}

func readProvisionalProductionHandoff(ctx context.Context, cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, runID string, want *ReleaseCampaignGate) (*ReleaseCampaignGate, *ScenarioResult, []byte, error) {
	if cfg == nil || cfg.provisionalResume == nil || cfg.provisionalResume.Record == nil || roles == nil || runID == "" || filepath.Base(runID) != runID {
		return nil, nil, nil, errors.New("provisional production handoff is unavailable in strict mode")
	}
	planHash := cfg.provisionalResume.Record.PlanHash
	if err := validateProvisionalProductionAuthority(cfg, planHash); err != nil {
		return nil, nil, nil, err
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(filepath.Join("runs", runID, "provisional-production-handoff.evidence.json")), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(raw, &envelope); err != nil {
		return nil, nil, nil, err
	}
	role, err := roles.EVMKey("testnet-owner")
	if err != nil {
		return nil, nil, nil, err
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, nil, nil, err
	}
	if err := verifyEvidence(&envelope, &key.PublicKey); err != nil {
		return nil, nil, nil, err
	}
	if envelope.Kind != provisionalProductionHandoffKind || envelope.RunID != runID || envelope.DeploymentID != cfg.Config.Deployment.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return nil, nil, nil, errors.New("provisional production handoff envelope identity differs")
	}
	var payload provisionalProductionHandoff
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return nil, nil, nil, err
	}
	actual, result, handoff, err := readProvisionalProductionSources(ctx, cfg, stateDir, roles, planHash, runID, payload.SourceAttemptPath, false)
	if err != nil {
		return nil, nil, nil, err
	}
	if !reflect.DeepEqual(payload, *actual) {
		return nil, nil, nil, errors.New("provisional production handoff differs from exact terminal source bytes or failures")
	}
	gate := &ReleaseCampaignGate{Schema: provisionalProductionGateSchema, RunID: runID, ResultHash: result.EvidenceHash, ProvisionalHandoffHash: envelope.ContentHash, StartEpoch: result.StartEpoch, EndEpoch: result.EndEpoch, LifecycleHandoff: payload.LifecycleHandoff}
	if err := validateProvisionalProductionGateShape(cfg, gate); err != nil {
		return nil, nil, nil, err
	}
	if want != nil && !releaseCampaignGatesEqual(gate, want) {
		return nil, nil, nil, errors.New("provisional production gate was retargeted")
	}
	canonical, err := json.Marshal(actual)
	var compact bytes.Buffer
	compactErr := json.Compact(&compact, envelope.Payload)
	if err != nil || compactErr != nil || !bytes.Equal(canonical, compact.Bytes()) {
		return nil, nil, nil, errors.New("provisional production handoff payload is noncanonical")
	}
	return gate, result, handoff, nil
}
