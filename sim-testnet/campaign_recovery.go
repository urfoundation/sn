// A recovery retires one terminal failed provisional interval and starts a new
// generation under the current approval while retaining exact custody and all
// predecessor evidence. Approved configuration changes require a fresh interval.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const scenarioCampaignRecoverySchema = "urnetwork-sim-campaign-recovery-v1"

// Signed lineage binds one replacement campaign to every retained source of
// the exact failed predecessor. Zero generation fields encode legacy generation one.
type scenarioCampaignRecovery struct {
	Schema                     string `json:"schema"`
	Generation                 uint64 `json:"generation,omitempty"`
	PriorGeneration            uint64 `json:"prior_generation,omitempty"`
	PriorAttemptPath           string `json:"prior_attempt_path,omitempty"`
	PriorRunID                 string `json:"prior_run_id"`
	PriorAttemptSha256         string `json:"prior_attempt_sha256"`
	PriorCampaignStartSha256   string `json:"prior_campaign_start_sha256"`
	PriorResultSha256          string `json:"prior_result_sha256"`
	PriorObservationLogSha256  string `json:"prior_observation_log_sha256"`
	PriorObservationLogBytes   uint64 `json:"prior_observation_log_bytes"`
	PriorProcessLogSha256      string `json:"prior_process_log_sha256"`
	PriorJournalSha256         string `json:"prior_journal_sha256,omitempty"`
	PriorJournalBytes          uint64 `json:"prior_journal_bytes,omitempty"`
	ApprovedPlanSha256         string `json:"approved_plan_sha256"`
	InheritedPreparationSha256 string `json:"inherited_preparation_attempt_sha256,omitempty"`
}

// The first generation keeps the filename already deployed by the recovery release.
func scenarioCampaignRecoveryPath(stateDir string) string {
	return filepath.Join(stateDir, "campaign-attempts", "release-1.0.recovery.evidence.json")
}

// Later generations use canonical decimal suffixes without replacing earlier records.
func scenarioCampaignRecoveryGenerationPath(stateDir string, generation uint64) string {
	if generation <= 1 {
		return scenarioCampaignRecoveryPath(stateDir)
	}
	return filepath.Join(stateDir, "campaign-attempts", fmt.Sprintf("release-1.0.recovery.%d.evidence.json", generation))
}

// Signed relative paths remain independent of the local state-directory location.
func scenarioCampaignRecoveryRelativePath(generation uint64) string {
	return filepath.ToSlash(filepath.Join("campaign-attempts", filepath.Base(scenarioCampaignRecoveryGenerationPath("", generation))))
}

// Generation one predates explicit lineage fields. Its fixed filename and
// implicit successor predecessor remain its exact canonical representation.
func scenarioCampaignRecoveryGeneration(recovery *scenarioCampaignRecovery) (uint64, error) {
	if recovery == nil {
		return 0, errors.New("campaign recovery lineage is unavailable")
	}
	if recovery.Generation == 0 {
		if recovery.PriorGeneration != 0 || recovery.PriorAttemptPath != "" {
			return 0, errors.New("legacy campaign recovery has future lineage fields")
		}
		return 1, nil
	}
	if recovery.Generation < 2 || recovery.PriorGeneration != recovery.Generation-1 || recovery.PriorAttemptPath != scenarioCampaignRecoveryRelativePath(recovery.PriorGeneration) {
		return 0, errors.New("campaign recovery generation or prior path is noncanonical")
	}
	return recovery.Generation, nil
}

// One canonical file location corresponds to each chain generation.
type scenarioCampaignRecoveryFile struct {
	generation   uint64
	path         string
	relativePath string
}

// A validated chain link retains both its decoded attempt and exact signed bytes.
type scenarioCampaignRecoveryRecord struct {
	file    scenarioCampaignRecoveryFile
	attempt *scenarioCampaignAttempt
	raw     []byte
}

// Only a failed signed release interval can advance to another recovery generation.
// Before acceptance, a durable result is the terminal marker; after acceptance,
// the signed invalidation remains mandatory.
func scenarioCampaignAttemptNeedsRecovery(attempt *scenarioCampaignAttempt) bool {
	if attempt == nil || attempt.payload.Phase != "release-1.0" || attempt.payload.Succession == nil && attempt.payload.Recovery == nil {
		return false
	}
	if attempt.payload.AcceptanceBoundary != nil {
		return attempt.payload.AcceptanceInvalidation != ""
	}
	_, err := os.Lstat(filepath.Join(attempt.stateDir, "runs", attempt.payload.RunID, "result.json"))
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

// Recovery records are an append-only contiguous namespace. Rejecting gaps,
// aliases and duplicates prevents a later file from hiding a failed link.
func scenarioCampaignRecoveryFiles(stateDir string) ([]scenarioCampaignRecoveryFile, error) {
	directory := filepath.Join(stateDir, "campaign-attempts")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	const prefix = "release-1.0.recovery."
	const suffix = ".evidence.json"
	byGeneration := map[uint64]scenarioCampaignRecoveryFile{}
	var noncanonicalFilename error
	for _, entry := range entries {
		name := entry.Name()
		generation := uint64(0)
		switch {
		case name == filepath.Base(scenarioCampaignRecoveryPath(stateDir)):
			generation = 1
		case strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix):
			value := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
			parsed, parseErr := strconv.ParseUint(value, 10, 64)
			if parseErr != nil || parsed == 0 {
				return nil, fmt.Errorf("campaign recovery filename %q has a noncanonical generation", name)
			}
			generation = parsed
		default:
			continue
		}
		if _, duplicate := byGeneration[generation]; duplicate {
			return nil, fmt.Errorf("campaign recovery generation %d is duplicated", generation)
		}
		expected := filepath.Base(scenarioCampaignRecoveryGenerationPath(stateDir, generation))
		if name != expected {
			noncanonicalFilename = fmt.Errorf("campaign recovery generation %d uses noncanonical filename %q", generation, name)
		}
		path := filepath.Join(directory, name)
		byGeneration[generation] = scenarioCampaignRecoveryFile{generation: generation, path: path, relativePath: scenarioCampaignRecoveryRelativePath(generation)}
	}
	if noncanonicalFilename != nil {
		return nil, noncanonicalFilename
	}
	if len(byGeneration) == 0 {
		return nil, nil
	}
	maximum := uint64(0)
	for generation := range byGeneration {
		if generation > maximum {
			maximum = generation
		}
	}
	if maximum != uint64(len(byGeneration)) {
		return nil, errors.New("campaign recovery generations are not contiguous from the legacy first record")
	}
	files := make([]scenarioCampaignRecoveryFile, 0, len(byGeneration))
	for generation := uint64(1); generation <= maximum; generation++ {
		file, present := byGeneration[generation]
		if !present {
			return nil, errors.New("campaign recovery generations contain a gap")
		}
		files = append(files, file)
	}
	return files, nil
}

// Only fields that existed at acceptance start must remain fixed through invalidation.
func scenarioCampaignRecoveryStaticBoundaryMatches(start, final *scenarioCampaignAcceptanceBoundary) bool {
	return start != nil && final != nil && start.ProcessSessionID == final.ProcessSessionID && start.AcceptanceStartedAt == final.AcceptanceStartedAt &&
		start.ScenarioDefinitionHash == final.ScenarioDefinitionHash && start.AdversarialMatrixHash == final.AdversarialMatrixHash &&
		start.AdversaryStartedAt == final.AdversaryStartedAt && start.AdversaryHappyPathStartedAt == final.AdversaryHappyPathStartedAt &&
		start.CampaignStartHead == final.CampaignStartHead && start.CampaignStartEpoch == final.CampaignStartEpoch &&
		start.CampaignStartObservationHash == final.CampaignStartObservationHash && reflect.DeepEqual(start.AcceptanceWindow, final.AcceptanceWindow) &&
		start.RetainedObservationLogContentHash == final.RetainedObservationLogContentHash && start.RetainedObservationLogBytes == final.RetainedObservationLogBytes &&
		start.ProcessLogBoundaryHash == final.ProcessLogBoundaryHash
}

// A recoverable result is a terminal provisional failure with no accepted descendant.
func readScenarioCampaignRecoveryResult(cfg *ResolvedConfig, stateDir string, prior *scenarioCampaignAttempt) (*ScenarioResult, []byte, error) {
	if prior == nil {
		return nil, nil, errors.New("campaign recovery predecessor is unavailable")
	}
	preAcceptance := prior.payload.AcceptanceBoundary == nil
	if preAcceptance {
		if prior.payload.AcceptanceInvalidation != "" || prior.payload.AcceptanceInvalidatedAt != "" {
			return nil, nil, errors.New("campaign recovery predecessor has a pre-acceptance invalidation")
		}
	} else if prior.payload.AcceptanceInvalidation == "" || prior.payload.AcceptanceInvalidatedAt == "" {
		return nil, nil, errors.New("campaign recovery predecessor has no terminal invalidated acceptance")
	}
	run := filepath.Join("runs", prior.payload.RunID)
	if _, err := os.Lstat(filepath.Join(stateDir, run, "complete.json")); err == nil {
		return nil, nil, errors.New("campaign recovery cannot replace a completed release")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if _, err := os.Lstat(scenarioCampaignAttemptPath(stateDir, "production-soak")); err == nil {
		return nil, nil, errors.New("campaign recovery cannot replace a release with a production descendant")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(filepath.Join(run, "result.json")), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, nil, errors.Join(errors.New("campaign recovery has no terminal failed result"), err)
	}
	var result ScenarioResult
	if err := decodeStrictJSONBytes(raw, &result); err != nil {
		return nil, nil, err
	}
	hash, hashErr := canonicalScenarioResultHash(&result)
	terminalStartedAt := prior.payload.StartedAt
	if !preAcceptance {
		terminalStartedAt = prior.payload.AcceptanceBoundary.AcceptanceStartedAt
	}
	terminalStarted, startErr := time.Parse(time.RFC3339Nano, terminalStartedAt)
	completed, completedErr := time.Parse(time.RFC3339Nano, result.CompletedAt)
	failed := false
	failedCount := 0
	for _, assertion := range result.Assertions {
		failed = failed || !assertion.Passed
		if !assertion.Passed {
			failedCount++
		}
	}
	if hashErr != nil || hash != result.EvidenceHash || startErr != nil || completedErr != nil || completed.Before(terminalStarted) || result.StartedAt != prior.payload.StartedAt || result.Schema != "urnetwork-sim-scenario-result-v1" || result.Release != "1.0" || result.RunID != prior.payload.RunID || result.Name != "release-1.0" || result.Result != "fail" || !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance || result.DeploymentID != cfg.Config.Deployment.DeploymentID || result.ConfigHash != cfg.ConfigHash || result.PolicyHash != cfg.PolicyHash || result.ChainID != cfg.ChainID || result.Netuid != cfg.Netuid || !strings.EqualFold(result.GenesisHash, cfg.Public.Chain.GenesisHash) || !failed || result.AssertionCount != len(result.Assertions) || result.FailedAssertionCount != failedCount || result.PriorRelease != nil {
		return nil, nil, errors.Join(errors.New("campaign recovery result differs from its invalidated provisional source"), hashErr)
	}
	return &result, raw, nil
}

// Only a succession or already validated recovery may precede another generation.
func scenarioCampaignRecoveryPredecessorIdentity(prior *scenarioCampaignAttempt) (uint64, string, error) {
	if prior == nil {
		return 0, "", errors.New("campaign recovery predecessor is unavailable")
	}
	if prior.payload.Succession != nil && prior.payload.Recovery == nil {
		return 0, filepath.ToSlash(filepath.Join("campaign-attempts", filepath.Base(scenarioCampaignSuccessorPath("")))), nil
	}
	if prior.payload.Succession != nil || prior.payload.Recovery == nil {
		return 0, "", errors.New("campaign recovery predecessor is not an invalidated succession or recovery")
	}
	generation, err := scenarioCampaignRecoveryGeneration(prior.payload.Recovery)
	if err != nil {
		return 0, "", err
	}
	return generation, scenarioCampaignRecoveryRelativePath(generation), nil
}

// Rebuild the expected lineage from the predecessor's exact authenticated sources.
func readScenarioCampaignRecoverySources(attempt, prior *scenarioCampaignAttempt, priorRelativePath string, priorRaw []byte) (*scenarioCampaignRecovery, time.Time, error) {
	if attempt == nil {
		return nil, time.Time{}, errors.New("campaign recovery source is unavailable")
	}
	return readScenarioCampaignRecoverySourcesWithPlans(attempt, prior, priorRelativePath, priorRaw, &scenarioCampaignPlanLookup{stateDir: attempt.stateDir})
}

// Preserve every source digest while sharing only unchanged plan validation.
func readScenarioCampaignRecoverySourcesWithPlans(attempt, prior *scenarioCampaignAttempt, priorRelativePath string, priorRaw []byte, plans *scenarioCampaignPlanLookup) (result *scenarioCampaignRecovery, completedAt time.Time, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, plans.check())
		if resultErr != nil {
			result, completedAt = nil, time.Time{}
		}
	}()
	if attempt == nil || prior == nil || len(priorRaw) == 0 {
		return nil, time.Time{}, errors.New("campaign recovery source is unavailable")
	}
	priorGeneration, expectedPriorPath, err := scenarioCampaignRecoveryPredecessorIdentity(prior)
	if err != nil {
		return nil, time.Time{}, err
	}
	if priorRelativePath != expectedPriorPath {
		return nil, time.Time{}, errors.New("campaign recovery predecessor path differs from its exact generation")
	}
	runRelative := filepath.ToSlash(filepath.Join("runs", prior.payload.RunID))
	startPath := filepath.Join(attempt.stateDir, filepath.FromSlash(runRelative), scenarioCampaignStartFilename)
	preAcceptance := prior.payload.AcceptanceBoundary == nil
	var startRaw []byte
	if preAcceptance {
		if _, err := os.Lstat(startPath); err == nil {
			return nil, time.Time{}, errors.New("campaign recovery pre-acceptance predecessor has a campaign-start marker")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, time.Time{}, err
		}
	} else {
		start, raw, err := readScenarioCampaignAttemptAtContext(prior.cfg, attempt.stateDir, attempt.roles, prior.payload.PlanHash, "release-1.0", startPath, prior.historicalEvidence)
		if err != nil {
			return nil, time.Time{}, err
		}
		if start.payload.RunID != prior.payload.RunID || start.payload.AcceptanceInvalidation != "" || !scenarioCampaignRecoveryStaticBoundaryMatches(start.payload.AcceptanceBoundary, prior.payload.AcceptanceBoundary) {
			return nil, time.Time{}, errors.New("campaign recovery start marker differs from the invalidated attempt")
		}
		if prior.historicalEvidence {
			if err := validateScenarioFaultProgress(start.payload.AcceptanceBoundary.Faults, prior.payload.AcceptanceBoundary.Faults); err != nil {
				return nil, time.Time{}, fmt.Errorf("historical campaign recovery fault schedule or progress changed: %w", err)
			}
		}
		startRaw = raw
	}
	terminalResult, resultRaw, err := readScenarioCampaignRecoveryResult(prior.cfg, attempt.stateDir, prior)
	if err != nil {
		return nil, time.Time{}, err
	}
	observationName := runRelative + "/observations.jsonl"
	var observationHash string
	var observationBytes uint64
	if preAcceptance {
		observationRaw, err := readValidatorEvidenceHistoricalFile(attempt.stateDir, observationName, maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return nil, time.Time{}, err
		}
		if _, err := decodeScenarioObservationLog(observationRaw); err != nil {
			return nil, time.Time{}, err
		}
		observationHash, observationBytes = bytesSHA256(observationRaw), uint64(len(observationRaw))
	} else {
		observationHash, observationBytes, err = hashCampaignObservationHistory(attempt.stateDir, observationName)
		if err != nil {
			return nil, time.Time{}, err
		}
		if _, _, _, _, _, err := prior.loadAuthenticatedRecoveryRuntimeForensics(filepath.Join(attempt.stateDir, filepath.FromSlash(runRelative))); err != nil {
			return nil, time.Time{}, err
		}
	}
	processLogRaw, err := readValidatorEvidenceHistoricalFile(attempt.stateDir, runRelative+"/"+processLogEvidenceFilename, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, time.Time{}, err
	}
	var processLogs processLogGateState
	if err := decodeStrictJSONBytes(processLogRaw, &processLogs); err != nil {
		return nil, time.Time{}, err
	}
	if processLogs.DeploymentID != attempt.cfg.Config.Deployment.DeploymentID {
		return nil, time.Time{}, errors.New("campaign recovery process-log evidence has the wrong deployment")
	}
	if err := validatePersistedProcessLogGate(processLogs); err != nil {
		return nil, time.Time{}, fmt.Errorf("campaign recovery process-log evidence is invalid: %w", err)
	}
	if preAcceptance {
		if processLogs.AcceptanceBoundary != nil {
			return nil, time.Time{}, errors.New("campaign recovery pre-acceptance process log has an acceptance boundary")
		}
	} else if boundaryHash := prior.payload.AcceptanceBoundary.ProcessLogBoundaryHash; boundaryHash != "" && (processLogs.AcceptanceBoundary == nil || !strings.EqualFold(processLogs.AcceptanceBoundary.ContentHash, boundaryHash)) {
		return nil, time.Time{}, errors.New("campaign recovery process-log boundary differs from the signed acceptance")
	}
	plan, planRawHash, err := plans.read(attempt.stateDir, attempt.payload.PlanHash)
	if err != nil {
		return nil, time.Time{}, err
	}
	completed, _ := time.Parse(time.RFC3339Nano, terminalResult.CompletedAt)
	terminal := completed
	var journalCut *scenarioCampaignJournalCut
	if preAcceptance {
		verifiedSource := false
		allowedPlanHashes := plan.allowedPlanHashes()
		journalCut, err = readScenarioCampaignRecoveryJournalPrefix(attempt, prior, func(entry JournalEntry) {
			verifiedSource = verifiedSource || entry.DeploymentID == plan.DeploymentID && entry.Stage == StageVerified && (entry.PlanHash == plan.PlanHash || allowedPlanHashes[entry.PlanHash])
		})
		if err != nil {
			return nil, time.Time{}, err
		}
		if !verifiedSource {
			return nil, time.Time{}, errors.New("campaign recovery journal prefix has no retained verified plan source")
		}
	} else {
		invalidated, invalidatedErr := time.Parse(time.RFC3339Nano, prior.payload.AcceptanceInvalidatedAt)
		if invalidatedErr != nil {
			return nil, time.Time{}, errors.New("campaign recovery predecessor invalidation time is malformed")
		}
		if invalidated.After(terminal) {
			terminal = invalidated
		}
	}
	recovery := &scenarioCampaignRecovery{
		Schema: scenarioCampaignRecoverySchema, PriorRunID: prior.payload.RunID,
		PriorAttemptSha256:        bytesSHA256(priorRaw),
		PriorResultSha256:         bytesSHA256(resultRaw),
		PriorObservationLogSha256: observationHash, PriorObservationLogBytes: observationBytes,
		PriorProcessLogSha256: bytesSHA256(processLogRaw), ApprovedPlanSha256: planRawHash,
	}
	if preAcceptance {
		recovery.PriorJournalSha256 = journalCut.SHA256
		recovery.PriorJournalBytes = journalCut.Bytes
	} else {
		recovery.PriorCampaignStartSha256 = bytesSHA256(startRaw)
	}
	if priorGeneration != 0 {
		recovery.Generation = priorGeneration + 1
		recovery.PriorGeneration = priorGeneration
		recovery.PriorAttemptPath = priorRelativePath
	}
	if attempt.payload.Recovery != nil && attempt.payload.Recovery.InheritedPreparationSha256 != "" {
		if attempt.payload.PlanHash != prior.payload.PlanHash || attempt.payload.ConfigHash != prior.payload.ConfigHash {
			return nil, time.Time{}, errors.New("campaign recovery cannot inherit preparation across an approval change")
		}
		if !attempt.payload.PreparationComplete {
			return nil, time.Time{}, errors.New("campaign recovery inherited preparation has no completed pre-acceptance predecessor")
		}
		if preAcceptance {
			if !prior.payload.PreparationComplete {
				return nil, time.Time{}, errors.New("campaign recovery inherited preparation has no completed pre-acceptance predecessor")
			}
			recovery.InheritedPreparationSha256 = bytesSHA256(priorRaw)
		} else if prior.payload.Recovery == nil || prior.payload.Recovery.InheritedPreparationSha256 == "" {
			return nil, time.Time{}, errors.New("campaign recovery inherited preparation has no completed pre-acceptance predecessor")
		} else {
			recovery.InheritedPreparationSha256 = prior.payload.Recovery.InheritedPreparationSha256
		}
	}
	return recovery, terminal, nil
}

// A candidate must equal the rebuilt lineage and start after both terminal times.
func validateScenarioCampaignRecoveryFromPrior(attempt, prior *scenarioCampaignAttempt, priorRelativePath string, priorRaw []byte) error {
	if attempt == nil {
		return errors.New("campaign recovery has no exact signed predecessor")
	}
	return validateScenarioCampaignRecoveryFromPriorWithPlans(attempt, prior, priorRelativePath, priorRaw, &scenarioCampaignPlanLookup{stateDir: attempt.stateDir})
}

// Envelope order, historical source bytes and cross-approval compatibility all
// remain checked; the shared lookup only removes repeated plan decoding.
func validateScenarioCampaignRecoveryFromPriorWithPlans(attempt, prior *scenarioCampaignAttempt, priorRelativePath string, priorRaw []byte, plans *scenarioCampaignPlanLookup) error {
	if attempt == nil || attempt.payload.Recovery == nil || attempt.payload.Phase != "release-1.0" || attempt.payload.Succession != nil {
		return errors.New("campaign recovery has no exact signed predecessor")
	}
	if prior == nil {
		return errors.New("campaign recovery predecessor is unavailable")
	}
	if err := validateScenarioCampaignLineageEdgeWithPlans(attempt, prior, plans); err != nil {
		return err
	}
	want, terminal, err := readScenarioCampaignRecoverySourcesWithPlans(attempt, prior, priorRelativePath, priorRaw, plans)
	if err != nil {
		return err
	}
	started, startErr := time.Parse(time.RFC3339Nano, attempt.payload.StartedAt)
	if startErr != nil || !started.After(terminal) || !reflect.DeepEqual(attempt.payload.Recovery, want) || bytesSHA256(priorRaw) != want.PriorAttemptSha256 {
		return errors.New("campaign recovery changed its retained predecessor or ordering")
	}
	return nil
}

// Generation one retains the exact signed succession as its implicit root.
func readScenarioCampaignRecoveryRoot(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string) (*scenarioCampaignAttempt, []byte, string, error) {
	reader, err := newScenarioCampaignLineageReader(cfg, stateDir, roles, planHash)
	if err != nil {
		return nil, nil, "", err
	}
	return reader.readRoot()
}

// A traversal reuses its authenticated approval inventory for every generation.
func (self *scenarioCampaignLineageReader) readRoot() (*scenarioCampaignAttempt, []byte, string, error) {
	path := scenarioCampaignSuccessorPath(self.stateDir)
	prior, raw, err := self.read(path)
	if err != nil {
		return nil, nil, "", err
	}
	if err := validateScenarioCampaignSuccessionWithPlans(prior, self.plans); err != nil {
		return nil, nil, "", err
	}
	relative := filepath.ToSlash(filepath.Join("campaign-attempts", filepath.Base(path)))
	return prior, raw, relative, nil
}

// Every link is authenticated from the root. An exact invocation-local proof
// may project unchanged history without repeating its large source reads.
func readScenarioCampaignRecoveryChain(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, files []scenarioCampaignRecoveryFile) ([]scenarioCampaignRecoveryRecord, error) {
	return readScenarioCampaignRecoveryChainMemo(cfg, stateDir, roles, planHash, files, readScenarioCampaignRecoveryChainFrom)
}

// A memoized prefix has already passed the same validator and source fence.
// Each appended generation still authenticates its signed envelope and edge.
func readScenarioCampaignRecoveryChainFrom(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, files []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
	reader, err := newScenarioCampaignLineageReader(cfg, stateDir, roles, planHash)
	if err != nil {
		return nil, err
	}
	return reader.readRecoveryChain(files, prefix)
}

// The real traversal owns its plan memo and emits progress only after each
// complete authenticated edge. A final source fence guards earlier approvals.
func (self *scenarioCampaignLineageReader) readRecoveryChain(files []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) (result []scenarioCampaignRecoveryRecord, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, self.plans.check())
		if resultErr != nil {
			result = nil
		}
	}()
	if len(prefix) > len(files) || len(prefix) != 0 && !provisionalResumeEnabled(self.cfg) {
		return nil, errors.New("campaign recovery prefix has no provisional chain context")
	}
	for index, record := range prefix {
		if record.file != files[index] || record.attempt == nil || len(record.raw) == 0 {
			return nil, errors.New("campaign recovery prefix differs from selected source files")
		}
	}
	started := time.Now()
	fmt.Fprintf(os.Stderr, "sim-testnet: campaign recovery lineage; authenticated_generations=%d total_generations=%d phase=validating\n", len(prefix), len(files))
	var err error
	var prior *scenarioCampaignAttempt
	var priorRaw []byte
	var priorRelativePath string
	if len(prefix) == 0 {
		prior, priorRaw, priorRelativePath, err = self.readRoot()
		if err != nil {
			return nil, err
		}
	} else {
		last := prefix[len(prefix)-1]
		prior, priorRaw, priorRelativePath = last.attempt, last.raw, last.file.relativePath
	}
	records := make([]scenarioCampaignRecoveryRecord, 0, len(files))
	records = append(records, prefix...)
	for index, file := range files[len(prefix):] {
		attempt, raw, err := self.read(file.path)
		if err != nil {
			return nil, err
		}
		generation, err := scenarioCampaignRecoveryGeneration(attempt.payload.Recovery)
		if err != nil || generation != file.generation {
			return nil, errors.Join(fmt.Errorf("campaign recovery file generation %d differs from its signed payload", file.generation), err)
		}
		if err := validateScenarioCampaignRecoveryFromPriorWithPlans(attempt, prior, priorRelativePath, priorRaw, self.plans); err != nil {
			return nil, fmt.Errorf("validate campaign recovery generation %d: %w", file.generation, err)
		}
		record := scenarioCampaignRecoveryRecord{file: file, attempt: attempt, raw: raw}
		records = append(records, record)
		prior, priorRaw, priorRelativePath = attempt, raw, file.relativePath
		fmt.Fprintf(os.Stderr, "sim-testnet: campaign recovery lineage; authenticated_generations=%d total_generations=%d generation=%d cached_plans=%d elapsed=%s\n", len(prefix)+index+1, len(files), file.generation, len(self.plans.proofKVs), time.Since(started).Round(time.Millisecond))
	}
	return records, nil
}

// The highest record is usable only after the full contiguous chain validates.
func readLatestScenarioCampaignRecovery(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string) (*scenarioCampaignAttempt, bool, error) {
	files, err := scenarioCampaignRecoveryFiles(stateDir)
	if err != nil || len(files) == 0 {
		return nil, false, err
	}
	records, err := readScenarioCampaignRecoveryChain(cfg, stateDir, roles, planHash, files)
	if err != nil {
		return nil, false, err
	}
	latest := records[len(records)-1].attempt
	if latest.payload.PlanHash != planHash || latest.payload.ConfigHash != cfg.ConfigHash {
		return nil, true, errScenarioCampaignHistoricalLineage
	}
	return latest, true, nil
}

// Persisted and not-yet-written candidates share the same full-chain validation.
func validateScenarioCampaignRecovery(attempt *scenarioCampaignAttempt) error {
	if attempt == nil || attempt.payload.Recovery == nil {
		return errors.New("campaign recovery has no exact signed predecessor")
	}
	generation, err := scenarioCampaignRecoveryGeneration(attempt.payload.Recovery)
	if err != nil {
		return err
	}
	files, err := scenarioCampaignRecoveryFiles(attempt.stateDir)
	if err != nil {
		return err
	}
	if generation <= uint64(len(files)) {
		records, err := readScenarioCampaignRecoveryChain(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, files)
		if err != nil {
			return err
		}
		if uint64(len(records)) != generation || !reflect.DeepEqual(records[generation-1].attempt.payload, attempt.payload) {
			return errors.New("campaign recovery is not the highest exact signed generation")
		}
		return nil
	}
	if uint64(len(files)) != generation-1 {
		return errors.New("campaign recovery predecessor generations are incomplete")
	}
	prior, priorRaw, priorRelativePath, err := readScenarioCampaignRecoveryRoot(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash)
	if err != nil {
		return err
	}
	if len(files) != 0 {
		records, chainErr := readScenarioCampaignRecoveryChain(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, files)
		if chainErr != nil {
			return chainErr
		}
		last := records[len(records)-1]
		prior, priorRaw, priorRelativePath = last.attempt, last.raw, last.file.relativePath
	}
	return validateScenarioCampaignRecoveryFromPrior(attempt, prior, priorRelativePath, priorRaw)
}

// validateScenarioCampaignRecoveryAncestor permits a retained release artifact
// to cross only an exact owner-signed recovery chain. The current attempt must
// still be pre-acceptance, and a production descendant closes the carry path.
func validateScenarioCampaignRecoveryAncestor(attempt *scenarioCampaignAttempt, ancestorRunID string) error {
	if attempt == nil || attempt.payload.Recovery == nil || attempt.payload.AcceptanceBoundary != nil || ancestorRunID == "" || ancestorRunID == attempt.payload.RunID {
		return errors.New("campaign recovery has no distinct pre-acceptance ancestor")
	}
	ancestors, err := scenarioCampaignRecoveryAncestors(attempt, validateScenarioCampaignRecovery)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(scenarioCampaignAttemptPath(attempt.stateDir, "production-soak")); err == nil {
		return errors.New("campaign recovery ancestor has a production descendant")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if ancestors[ancestorRunID] {
		return nil
	}
	return errors.New("retained release run is not an authenticated campaign recovery ancestor")
}

// Extend the authenticated chain by one fresh full campaign after a failed interval.
func createScenarioCampaignRecovery(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, now time.Time, journal *Journal) (*scenarioCampaignAttempt, error) {
	if roles == nil {
		return nil, errors.New("campaign recovery requires the exact provisional non-accepting approval")
	}
	if err := validateScenarioCampaignRecoveryOwner(cfg, stateDir, planHash, journal); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(scenarioCampaignAttemptPath(stateDir, "production-soak")); err == nil {
		return nil, errors.New("campaign recovery cannot replace a release with a production descendant")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	plan, err := loadRuntimePersistedPlan(cfg, stateDir)
	if err != nil || plan.PlanHash != planHash {
		return nil, errors.Join(errors.New("campaign recovery approved plan is unavailable"), err)
	}
	retainedRoles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil || !reflect.DeepEqual(retainedRoles, roles) {
		return nil, errors.Join(errors.New("campaign recovery retained custody changed"), err)
	}
	ownedSource := false
	for _, entry := range journal.Entries() {
		ownedSource = ownedSource || entry.DeploymentID == plan.DeploymentID && entry.Stage == StageVerified && (entry.PlanHash == planHash || plan.allowedPlanHashes()[entry.PlanHash])
	}
	if !ownedSource {
		return nil, errors.New("campaign recovery has no retained verified journal source")
	}
	files, err := scenarioCampaignRecoveryFiles(stateDir)
	if err != nil {
		return nil, err
	}
	prior, priorRaw, priorRelativePath, err := readScenarioCampaignRecoveryRoot(cfg, stateDir, roles, planHash)
	if err != nil {
		return nil, fmt.Errorf("campaign recovery read root: %w", err)
	}
	if len(files) != 0 {
		records, chainErr := readScenarioCampaignRecoveryChain(cfg, stateDir, roles, planHash, files)
		if chainErr != nil {
			return nil, fmt.Errorf("campaign recovery read lineage: %w", chainErr)
		}
		last := records[len(records)-1]
		prior, priorRaw, priorRelativePath = last.attempt, last.raw, last.file.relativePath
	}
	// A signed acceptance invalidation proves that its process session cannot
	// continue. If the process was killed between invalidation and result
	// persistence, materialize the terminal provisional failure here. This is
	// deliberately limited to that signed state.  A provisional-only exception
	// below covers an interrupted preparation owner that never created its run
	// directory, so it cannot erase an acceptance interval or scenario output.
	if prior.payload.AcceptanceBoundary != nil && prior.payload.AcceptanceInvalidation != "" {
		runDir := filepath.Join(stateDir, "runs", prior.payload.RunID)
		resultPath := filepath.Join(runDir, "result.json")
		if _, err := os.Lstat(resultPath); errors.Is(err, os.ErrNotExist) {
			if prior.payload.ConfigHash != cfg.ConfigHash {
				return nil, errors.New("historical campaign terminal result is missing; current configuration cannot synthesize historical evidence")
			}
			definition, definitionErr := scenarioDefinitionFor(cfg, prior.payload.Phase)
			definitionHash, hashErr := scenarioDefinitionHash(definition)
			started, startErr := time.Parse(time.RFC3339Nano, prior.payload.StartedAt)
			if definitionErr != nil || hashErr != nil || startErr != nil {
				return nil, errors.Join(errors.New("campaign recovery cannot materialize interrupted terminal result"), definitionErr, hashErr, startErr)
			}
			interrupted := fmt.Errorf("scenario campaign acceptance was interrupted (%s); terminal result materialized by recovery after process exit", prior.payload.AcceptanceInvalidation)
			result, _ := writeInitialScenarioFailure(cfg, runDir, prior.payload.RunID, definitionHash, definition, started.UTC(), nil, prior, interrupted)
			if result == nil {
				return nil, errors.New("campaign recovery could not materialize interrupted terminal result")
			}
			if _, err := os.Lstat(resultPath); err != nil {
				return nil, fmt.Errorf("campaign recovery interrupted terminal result: %w", err)
			}
			// The terminal result uses the durable write time. Advance the
			// successor clock after that write so ordering is checked against the
			// actual terminal boundary rather than this function's entry time.
			now = time.Now().UTC()
		} else if err != nil {
			return nil, err
		}
	} else if prior.payload.AcceptanceBoundary == nil && provisionalResumeEnabled(cfg) &&
		cfg.provisionalResume.Record != nil && cfg.provisionalResume.Record.Command == "scenario" &&
		cfg.provisionalResume.Record.Scenario == "release-candidate" && prior.payload.PlanHash != planHash &&
		plan.allowedPlanHashes()[prior.payload.PlanHash] {
		runDir := filepath.Join(stateDir, "runs", prior.payload.RunID)
		if _, err := os.Lstat(runDir); errors.Is(err, os.ErrNotExist) {
			if prior.payload.ConfigHash != cfg.ConfigHash || prior.payload.PolicyHash != cfg.PolicyHash {
				return nil, errors.New("interrupted pre-acceptance owner has another configuration identity")
			}
			definition, definitionErr := scenarioDefinitionFor(cfg, prior.payload.Phase)
			definitionHash, hashErr := scenarioDefinitionHash(definition)
			started, startErr := time.Parse(time.RFC3339Nano, prior.payload.StartedAt)
			if definitionErr != nil || hashErr != nil || startErr != nil {
				return nil, errors.Join(errors.New("campaign recovery cannot materialize absent pre-acceptance result"), definitionErr, hashErr, startErr)
			}
			gate, err := loadLiveProcessLogGate(stateDir)
			if err != nil {
				return nil, fmt.Errorf("campaign recovery load live process-log gate: %w", err)
			}
			if err := publishAbsentCampaignRecoveryRun(runDir, func(staging string) error {
				if err := atomicWrite(filepath.Join(staging, "observations.jsonl"), []byte(preAcceptanceInterruptedObservationMarker), 0o600); err != nil {
					return fmt.Errorf("campaign recovery create absent pre-acceptance observations: %w", err)
				}
				if err := gate.WritePreAcceptanceEvidence(staging); err != nil {
					return fmt.Errorf("campaign recovery write absent pre-acceptance process-log evidence: %w", err)
				}
				failure := errors.New("provisional pre-acceptance owner was interrupted before its scenario directory was created")
				result, writeErr := writeInitialScenarioFailure(cfg, staging, prior.payload.RunID, definitionHash, definition, started.UTC(), nil, prior, failure)
				// The expected scenario failure is returned only after every output
				// succeeds; any other error is an incomplete publication.
				if result == nil || writeErr != failure {
					return errors.Join(errors.New("campaign recovery could not materialize absent pre-acceptance result"), writeErr)
				}
				return nil
			}); err != nil {
				return nil, err
			}
			now = time.Now().UTC()
		} else if err != nil {
			return nil, err
		}
	}
	// Legacy initial-read failures persisted a terminal result before creating
	// any observation file. Only this authenticated writer may add the explicit
	// empty marker; all existing result/observation evidence remains immutable.
	if err := materializeMissingPreAcceptanceObservationLog(stateDir, prior); err != nil {
		return nil, err
	}
	probe := &scenarioCampaignAttempt{cfg: cfg, stateDir: stateDir, roles: roles, payload: scenarioCampaignAttemptPayload{PlanHash: planHash}}
	plans := &scenarioCampaignPlanLookup{stateDir: stateDir}
	recovery, terminal, err := readScenarioCampaignRecoverySourcesWithPlans(probe, prior, priorRelativePath, priorRaw, plans)
	if err != nil {
		return nil, err
	}
	if !now.After(terminal) {
		return nil, errors.New("campaign recovery cannot start before predecessor completion and invalidation")
	}
	// An accepted failed interval may already carry an authenticated earlier
	// preparation checkpoint. Preserve that exact source through a same-plan
	// recovery; its acceptance window, faults and runtime evidence still reset.
	carryPreparation := false
	if prior.payload.PreparationComplete && prior.payload.PlanHash == planHash && prior.payload.ConfigHash == cfg.ConfigHash {
		if prior.payload.AcceptanceBoundary == nil {
			recovery.InheritedPreparationSha256 = recovery.PriorAttemptSha256
			carryPreparation = true
		} else if prior.payload.Recovery != nil && prior.payload.Recovery.InheritedPreparationSha256 != "" {
			recovery.InheritedPreparationSha256 = prior.payload.Recovery.InheritedPreparationSha256
			carryPreparation = true
		}
	}
	started := now.UTC()
	attempt := &scenarioCampaignAttempt{cfg: cfg, stateDir: stateDir, roles: roles, payload: scenarioCampaignAttemptPayload{
		Schema: scenarioCampaignAttemptSchema, Phase: "release-1.0", RunID: fmt.Sprintf("%s-release-1.0", started.Format("20060102T150405.000000000Z")), StartedAt: started.Format(time.RFC3339Nano),
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, PlanHash: planHash, Recovery: recovery,
		PreparationComplete: carryPreparation,
	}}
	generation, err := scenarioCampaignRecoveryGeneration(recovery)
	if err != nil {
		return nil, err
	}
	if generation != uint64(len(files)+1) {
		return nil, errors.New("campaign recovery generation does not extend the contiguous lineage")
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "runs", attempt.payload.RunID)); err == nil {
		return nil, errors.New("campaign recovery run directory already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := validateScenarioCampaignRecoveryFromPriorWithPlans(attempt, prior, priorRelativePath, priorRaw, plans); err != nil {
		return nil, err
	}
	if err := writeScenarioCampaignAttempt(attempt); err != nil {
		return nil, err
	}
	return attempt, nil
}
