package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// scenarioCampaignAnalyzer performs the read-only semantic reconstruction,
// public-chain replay, and authenticated semantic commit for one already
// closed scenario capture. Implementations must honor ctx so a failure in
// either phase can stop a live successor without leaking a worker.
type scenarioCampaignAnalyzer func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error
type scenarioCampaignPreflight func(context.Context, *ResolvedConfig, string) error

type scenarioCampaignAnalysisResult struct {
	phase string
	err   error
}

const finalArchiveReviewerSafetyWindow = 24 * time.Hour

func finalArchiveReviewerSafetyMargin(cfg *ResolvedConfig) (uint64, error) {
	if cfg == nil || cfg.Public == nil || cfg.Public.Chain.ExpectedBlockSeconds == 0 {
		return 0, errors.New("archive-retention reviewer margin configuration is incomplete")
	}
	seconds := uint64(finalArchiveReviewerSafetyWindow / time.Second)
	blockSeconds := cfg.Public.Chain.ExpectedBlockSeconds
	margin := seconds / blockSeconds
	if seconds%blockSeconds != 0 {
		margin++
	}
	if margin == 0 {
		return 0, errors.New("archive-retention reviewer margin is empty")
	}
	return margin, nil
}

// runFinalCampaignArchivePreflight proves that both public readers retain the
// oldest authenticated setup checkpoint plus the complete future campaign and
// a full day for evidence publication/peer review. Its immutable receipt is
// subsequently swept into the closed semantic input graph.
func runFinalCampaignArchivePreflight(ctx context.Context, cfg *ResolvedConfig, stateDir string) error {
	if ctx == nil || cfg == nil || stateDir == "" {
		return errors.New("archive-retention campaign preflight context is incomplete")
	}
	_, public, err := loadDeploymentReference(ctx, stateDir, filepath.Join(stateDir, "public.json"))
	if err != nil || public == nil {
		return stateMismatchError(err, "load archive-retention public deployment manifest")
	}
	margin, err := finalArchiveReviewerSafetyMargin(cfg)
	if err != nil {
		return err
	}
	value, locator, err := RunFinalCompositeArchiveRetentionPreflight(ctx, cfg, stateDir, public, margin)
	if err != nil {
		return err
	}
	if value == nil || locator.Kind != "archive-retention-preflight" {
		return errors.New("archive-retention preflight produced no immutable receipt")
	}
	absolute := filepath.Join(stateDir, filepath.FromSlash(locator.URI))
	wire, err := os.ReadFile(absolute)
	if err != nil {
		return fmt.Errorf("read archive-retention preflight receipt: %w", err)
	}
	if err := verifyFinalArtifact("archive-retention preflight receipt", locator, "archive-retention-preflight"); err != nil || uint64(len(wire)) != locator.SizeBytes || bytesSHA256(wire) != locator.ContentHash {
		return stateMismatchError(err, "archive-retention preflight receipt differs from its locator")
	}
	var persisted FinalArchiveRetentionPreflight
	if err := decodeStrictJSONBytes(wire, &persisted); err != nil {
		return fmt.Errorf("decode archive-retention preflight receipt: %w", err)
	}
	if err := verifyFinalArchiveRetentionPreflight(&persisted); err != nil {
		return err
	}
	want, err := json.Marshal(value)
	if err != nil {
		return err
	}
	got, err := json.Marshal(&persisted)
	if err != nil || string(got) != string(want) {
		return stateMismatchError(err, "archive-retention persisted receipt changed after probing")
	}
	return nil
}

// runReleaseCandidateCampaignWithAnalyzer separates the immutable live
// capture boundary from semantic interpretation. The release analyzer runs
// concurrently with the production live window, but both authenticated
// semantic commits remain mandatory before the composite campaign succeeds.
func runReleaseCandidateCampaignWithAnalyzer(ctx context.Context, cfg *ResolvedConfig, stateDir string, journal *Journal, executor *Executor, roles *RoleSecrets, runner scenarioCampaignRunner, preflight scenarioCampaignPreflight, analyzer scenarioCampaignAnalyzer) error {
	if ctx == nil || cfg == nil || cfg.Config == nil || journal == nil || executor == nil || executor.plan == nil || !validCanonicalHashHex(executor.plan.PlanHash) || roles == nil || runner == nil || preflight == nil || analyzer == nil {
		return errors.New("release-candidate analyzed campaign context is incomplete")
	}
	for _, name := range []string{"release-1.0", "production-soak"} {
		if _, err := scenarioDefinitionFor(cfg, name); err != nil {
			return fmt.Errorf("release-candidate %s definition: %w", name, err)
		}
	}

	campaignCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan scenarioCampaignAnalysisResult, 2)
	pending := 0
	startAnalyzer := func(phase string, result *ScenarioResult) {
		pending++
		runDir := filepath.Join(stateDir, "runs", result.RunID)
		go func() {
			err := analyzer(campaignCtx, cfg, stateDir, runDir, roles, result)
			if err != nil {
				cancel()
				err = fmt.Errorf("%s semantic analysis: %w", phase, err)
			}
			results <- scenarioCampaignAnalysisResult{phase: phase, err: err}
		}()
	}
	waitAnalyzers := func() error {
		var joined error
		for pending > 0 {
			result := <-results
			pending--
			joined = errors.Join(joined, result.err)
		}
		return joined
	}
	completedAnalyzerFailure := func() error {
		var joined error
		for {
			select {
			case result := <-results:
				pending--
				joined = errors.Join(joined, result.err)
			default:
				return joined
			}
		}
	}

	releaseAttempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, executor.plan.PlanHash, "release-1.0", nil, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("open durable release-1.0 attempt: %w", err)
	}
	release, _, err := loadCompletedScenarioCampaignByRunID(cfg, stateDir, roles, "release-1.0", releaseAttempt.payload.RunID)
	if err != nil {
		if !errors.Is(err, errNoCompletedScenarioCampaign) {
			return err
		}
		if err := preflight(campaignCtx, cfg, stateDir); err != nil {
			return fmt.Errorf("release-candidate archive-retention preflight: %w", err)
		}
		if err := runner(campaignCtx, cfg, stateDir, "release-1.0", journal, executor, releaseAttempt); err != nil {
			return err
		}
		release, _, err = loadCompletedScenarioCampaignByRunID(cfg, stateDir, roles, "release-1.0", releaseAttempt.payload.RunID)
		if err != nil {
			return fmt.Errorf("authenticate completed release-1.0 handoff: %w", err)
		}
	}
	releaseGate, err := validateReleaseCampaignComplete(cfg, roles, filepath.Join(stateDir, "runs", release.RunID), release)
	if err != nil {
		return fmt.Errorf("authenticate exact release-1.0 gate: %w", err)
	}
	startAnalyzer("release-1.0", release)

	productionAttempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, executor.plan.PlanHash, "production-soak", releaseGate, time.Now().UTC())
	if err != nil {
		cancel()
		return errors.Join(fmt.Errorf("open durable production-soak attempt: %w", err), waitAnalyzers())
	}
	production, _, err := loadCompletedScenarioCampaignByRunID(cfg, stateDir, roles, "production-soak", productionAttempt.payload.RunID)
	if err != nil {
		if !errors.Is(err, errNoCompletedScenarioCampaign) {
			cancel()
			return errors.Join(err, waitAnalyzers())
		}
		// Re-probe immediately before the second live phase. A resumed campaign
		// may be hours or days old, and the release phase itself advances the
		// pruning horizon; its earlier receipt cannot prove production retention.
		if err := preflight(campaignCtx, cfg, stateDir); err != nil {
			cancel()
			return errors.Join(fmt.Errorf("production-soak archive-retention preflight: %w", err), waitAnalyzers())
		}
		if err := runner(campaignCtx, cfg, stateDir, "production-soak", journal, executor, productionAttempt); err != nil {
			cancel()
			return errors.Join(err, waitAnalyzers())
		}
		if analyzerErr := completedAnalyzerFailure(); analyzerErr != nil {
			cancel()
			return errors.Join(analyzerErr, waitAnalyzers())
		}
		production, _, err = loadCompletedScenarioCampaignByRunID(cfg, stateDir, roles, "production-soak", productionAttempt.payload.RunID)
		if err != nil {
			cancel()
			return errors.Join(fmt.Errorf("authenticate completed production-soak handoff: %w", err), waitAnalyzers())
		}
	}
	startAnalyzer("production-soak", production)
	return waitAnalyzers()
}
