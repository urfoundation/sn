package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// One explicitly approved descendant can own a new full campaign after an
// unsuccessful pre-acceptance campaign. The original record is never moved or
// rewritten. The signed successor pins those original bytes, not their verdict
// as accepted evidence, and owns its own preparation and acceptance boundary.
type scenarioCampaignSuccession struct {
	Schema             string `json:"schema"`
	PriorPlanHash      string `json:"prior_plan_hash"`
	PriorRunID         string `json:"prior_run_id"`
	PriorAttemptSHA256 string `json:"prior_attempt_sha256"`
	PriorResultSHA256  string `json:"prior_result_sha256"`
	PriorPlanSHA256    string `json:"prior_plan_sha256"`
	ApprovedPlanSHA256 string `json:"approved_plan_sha256"`
}

func scenarioCampaignSuccessorPath(stateDir string) string {
	return filepath.Join(stateDir, "campaign-attempts", "release-1.0.successor.evidence.json")
}

func (attempt *scenarioCampaignAttempt) path() string {
	if attempt.payload.Recovery != nil {
		generation, err := scenarioCampaignRecoveryGeneration(attempt.payload.Recovery)
		if err == nil {
			return scenarioCampaignRecoveryGenerationPath(attempt.stateDir, generation)
		}
		return scenarioCampaignRecoveryPath(attempt.stateDir)
	}
	if attempt.payload.Succession != nil {
		return scenarioCampaignSuccessorPath(attempt.stateDir)
	}
	return scenarioCampaignAttemptPath(attempt.stateDir, attempt.payload.Phase)
}

func readScenarioSuccessionPlan(stateDir, hash string) (*SetupPlan, []byte, error) {
	if !validCanonicalHashHex(hash) {
		return nil, nil, errors.New("campaign succession plan hash is invalid")
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "plans/"+stringsTrim0x(hash)+".json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, nil, err
	}
	plan, err := decodePersistedPlanWire(raw)
	if err != nil || plan.PlanHash != hash {
		return nil, nil, errors.Join(errors.New("campaign succession plan differs from its retained hash"), err)
	}
	return plan, raw, nil
}

func scenarioSuccessionPlansMatch(current, prior *SetupPlan) bool {
	return current != nil && prior != nil && current.PlanHash != prior.PlanHash && current.allowedPlanHashes()[prior.PlanHash] &&
		current.DeploymentID == prior.DeploymentID && current.ChainID == prior.ChainID && current.Netuid == prior.Netuid &&
		current.GenesisHash == prior.GenesisHash && current.Owner == prior.Owner && current.ConfigHash == prior.ConfigHash &&
		current.PolicyHash == prior.PolicyHash && reflect.DeepEqual(current.Roles, prior.Roles) && contractDeploymentAddressesEqual(current.Deployment, prior.Deployment)
}

// The source result was not a signed completion. Authenticate its exact
// self-hash and pin its bytes in the new owner signature without promoting it
// into a successful phase or carrying preparation into the successor.
func readScenarioSuccessionFailure(cfg *ResolvedConfig, stateDir string, prior *scenarioCampaignAttempt) (*ScenarioResult, []byte, error) {
	if prior.payload.Phase != "release-1.0" || prior.payload.Succession != nil || prior.payload.AcceptanceBoundary != nil || prior.payload.AcceptanceInvalidatedAt != "" || prior.payload.AcceptanceInvalidation != "" || prior.payload.PriorRelease != nil || prior.payload.HandoffAuthenticated {
		return nil, nil, errors.New("campaign succession requires an original pre-acceptance release attempt")
	}
	run := filepath.Join("runs", prior.payload.RunID)
	for _, name := range []string{scenarioCampaignStartFilename, "complete.json", scenarioLifecycleHandoffFilename} {
		if _, err := os.Lstat(filepath.Join(stateDir, run, name)); err == nil {
			return nil, nil, errors.New("campaign succession cannot replace an accepted or handed-off predecessor")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(filepath.Join(run, "result.json")), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, nil, errors.Join(errors.New("campaign succession has no terminal failed predecessor result"), err)
	}
	var result ScenarioResult
	if err := decodeStrictJSONBytes(raw, &result); err != nil {
		return nil, nil, err
	}
	hash, err := canonicalScenarioResultHash(&result)
	if err != nil || hash != result.EvidenceHash || result.Schema != "urnetwork-sim-scenario-result-v1" || result.Release != "1.0" || result.Result != "fail" || result.FinalAcceptance != nil && *result.FinalAcceptance || result.RunID != prior.payload.RunID || result.Name != prior.payload.Phase || result.StartedAt != prior.payload.StartedAt || result.ConfigHash != prior.payload.ConfigHash || result.PolicyHash != prior.payload.PolicyHash || result.DeploymentID != cfg.Config.Deployment.DeploymentID || result.ChainID != cfg.ChainID || result.Netuid != cfg.Netuid || !strings.EqualFold(result.GenesisHash, cfg.Public.Chain.GenesisHash) || result.AcceptanceWindow != nil || result.PriorRelease != nil || result.LifecycleHandoff != nil {
		return nil, nil, errors.Join(errors.New("campaign succession failed result differs from its original source"), err)
	}
	started, startErr := time.Parse(time.RFC3339Nano, result.StartedAt)
	finished, finishErr := time.Parse(time.RFC3339Nano, result.CompletedAt)
	failed := false
	for _, assertion := range result.Assertions {
		failed = failed || !assertion.Passed
	}
	if startErr != nil || finishErr != nil || finished.Before(started) || !failed {
		return nil, nil, errors.New("campaign succession predecessor is active or has no actual terminal failure")
	}
	return &result, raw, nil
}

// This check also runs whenever the successor is reopened or its signed start
// marker is updated. Bounded raw plan reads check their hashes and exact
// lineage; full current release/budget/custody admission is done before creation.
func validateScenarioCampaignSuccession(attempt *scenarioCampaignAttempt) error {
	if attempt == nil || attempt.cfg == nil || attempt.payload.Succession == nil || attempt.payload.Phase != "release-1.0" {
		return errors.New("campaign successor has no exact signed source")
	}
	s := attempt.payload.Succession
	if s.Schema != "urnetwork-sim-campaign-succession-v1" || s.PriorRunID == attempt.payload.RunID || !validCanonicalHashHex(s.PriorPlanHash) {
		return errors.New("campaign succession identity is invalid")
	}
	current, currentRaw, err := readScenarioSuccessionPlan(attempt.stateDir, attempt.payload.PlanHash)
	if err != nil {
		return err
	}
	priorPlan, priorRaw, err := readScenarioSuccessionPlan(attempt.stateDir, s.PriorPlanHash)
	if err != nil {
		return err
	}
	if !scenarioSuccessionPlansMatch(current, priorPlan) || bytesSHA256(currentRaw) != s.ApprovedPlanSHA256 || bytesSHA256(priorRaw) != s.PriorPlanSHA256 || current.ConfigHash != attempt.cfg.ConfigHash || current.PolicyHash != attempt.cfg.PolicyHash || !strings.EqualFold(current.Roles.Owner, attempt.roles.EVM["testnet-owner"].Address) {
		return errors.New("campaign succession changed its approved lineage or custody")
	}
	prior, raw, err := readScenarioCampaignAttemptAt(attempt.cfg, attempt.stateDir, attempt.roles, s.PriorPlanHash, "release-1.0", scenarioCampaignAttemptPath(attempt.stateDir, "release-1.0"))
	if err != nil {
		return err
	}
	if prior.payload.RunID != s.PriorRunID || bytesSHA256(raw) != s.PriorAttemptSHA256 {
		return errors.New("campaign succession changed its original signed attempt")
	}
	result, resultRaw, err := readScenarioSuccessionFailure(attempt.cfg, attempt.stateDir, prior)
	if err != nil {
		return err
	}
	finished, _ := time.Parse(time.RFC3339Nano, result.CompletedAt)
	started, err := time.Parse(time.RFC3339Nano, attempt.payload.StartedAt)
	if err != nil || !started.After(finished) || bytesSHA256(resultRaw) != s.PriorResultSHA256 {
		return errors.New("campaign succession does not follow its exact failed result")
	}
	return nil
}

func createScenarioCampaignSuccessor(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, now time.Time, journal *Journal) (*scenarioCampaignAttempt, error) {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || roles == nil || journal == nil {
		return nil, errors.New("campaign succession requires the approved deployment owner")
	}
	if cfg.provisionalResume != nil && (!provisionalResumeEnabled(cfg) || cfg.provisionalResume.Record.PlanHash != planHash || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance) {
		return nil, errors.New("provisional campaign succession requires the exact non-accepting approval")
	}
	// OpenJournal's nonblocking exclusive lease is held by both real scenario
	// entry points throughout execution. An active predecessor therefore blocks
	// admission before this function; a closed or borrowed journal cannot sign.
	journal.mu.Lock()
	owned := journal.lock != nil && journal.file != nil && journal.path == filepath.Join(stateDir, "journal.jsonl")
	journal.mu.Unlock()
	if !owned {
		return nil, errors.New("campaign succession has no live exclusive deployment journal owner")
	}
	for _, path := range []string{scenarioCampaignSuccessorPath(stateDir), scenarioCampaignAttemptPath(stateDir, "production-soak")} {
		if _, err := os.Lstat(path); err == nil {
			return nil, errors.New("campaign succession cannot replace an existing successor or production attempt")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	current, err := loadRuntimePersistedPlan(cfg, stateDir)
	if err != nil || current.PlanHash != planHash {
		return nil, errors.Join(errors.New("campaign succession current approval is unavailable"), err)
	}
	retainedRoles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil || !reflect.DeepEqual(retainedRoles, roles) {
		return nil, errors.Join(errors.New("campaign succession retained custody is incomplete or changed"), err)
	}
	seenClients := map[string]bool{}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		client, found := retainedRoles.Clients[fmt.Sprintf("miner-%d", miner)]
		_, valid := evidenceFixedHex("0x"+client.ClientIDHex, 16)
		id := strings.ToLower(client.ClientIDHex)
		if !found || !valid || strings.Trim(id, "0") == "" || seenClients[id] {
			return nil, errors.New("campaign succession retained provider custody is incomplete or duplicated")
		}
		seenClients[id] = true
	}
	// Read the envelope before selecting its plan, then authenticate it with
	// that exact archived source. Untrusted payload fields grant no authority.
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "campaign-attempts/release-1.0.evidence.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	var envelope ReleaseEvidenceEnvelope
	var payload scenarioCampaignAttemptPayload
	if err := decodeStrictJSONBytes(raw, &envelope); err != nil {
		return nil, err
	}
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return nil, err
	}
	priorPlan, err := readValidatorEvidenceHistoricalPlan(stateDir, payload.PlanHash)
	if err != nil || !scenarioSuccessionPlansMatch(current, priorPlan) {
		return nil, errors.Join(errors.New("campaign succession predecessor is outside the approved lineage"), err)
	}
	entries := journal.Entries()
	ownedSource := false
	for _, entry := range entries {
		ownedSource = ownedSource || entry.PlanHash == priorPlan.PlanHash && entry.DeploymentID == current.DeploymentID && entry.Stage == StageVerified
	}
	if !ownedSource {
		return nil, errors.New("campaign succession predecessor has no retained verified journal source")
	}
	if err := validateFleetLifecycleRenewalPending(stateDir, current, entries); err != nil {
		return nil, err
	}
	prior, priorRaw, err := readScenarioCampaignAttemptAt(cfg, stateDir, roles, priorPlan.PlanHash, "release-1.0", scenarioCampaignAttemptPath(stateDir, "release-1.0"))
	if err != nil {
		return nil, err
	}
	result, resultRaw, err := readScenarioSuccessionFailure(cfg, stateDir, prior)
	if err != nil {
		return nil, err
	}
	finished, _ := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if !now.After(finished) {
		return nil, errors.New("campaign successor cannot start before predecessor completion")
	}
	_, priorPlanRaw, err := readScenarioSuccessionPlan(stateDir, priorPlan.PlanHash)
	if err != nil {
		return nil, err
	}
	_, currentRaw, err := readScenarioSuccessionPlan(stateDir, planHash)
	if err != nil {
		return nil, err
	}
	started := now.UTC()
	attempt := &scenarioCampaignAttempt{cfg: cfg, stateDir: stateDir, roles: roles, payload: scenarioCampaignAttemptPayload{
		Schema: scenarioCampaignAttemptSchema, Phase: "release-1.0", RunID: fmt.Sprintf("%s-release-1.0", started.Format("20060102T150405.000000000Z")), StartedAt: started.Format(time.RFC3339Nano),
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, PlanHash: planHash,
		Succession: &scenarioCampaignSuccession{Schema: "urnetwork-sim-campaign-succession-v1", PriorPlanHash: priorPlan.PlanHash, PriorRunID: prior.payload.RunID,
			PriorAttemptSHA256: bytesSHA256(priorRaw), PriorResultSHA256: bytesSHA256(resultRaw), PriorPlanSHA256: bytesSHA256(priorPlanRaw), ApprovedPlanSHA256: bytesSHA256(currentRaw)},
	}}
	if _, err := os.Lstat(filepath.Join(stateDir, "runs", attempt.payload.RunID)); err == nil {
		return nil, errors.New("campaign successor run directory already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := validateScenarioCampaignSuccession(attempt); err != nil {
		return nil, err
	}
	if err := writeScenarioCampaignAttempt(attempt); err != nil {
		return nil, err
	}
	return attempt, nil
}
