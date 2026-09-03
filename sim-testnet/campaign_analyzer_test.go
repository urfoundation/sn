package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func noOpCampaignPreflight(context.Context, *ResolvedConfig, string) error { return nil }

func TestFinalArchiveReviewerMarginIsOneDayAtConfiguredCadence(t *testing.T) {
	cfg := testResolvedConfig(t)
	margin, err := finalArchiveReviewerSafetyMargin(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64((24 * time.Hour) / (time.Duration(cfg.Public.Chain.ExpectedBlockSeconds) * time.Second))
	if margin != want || margin != 7_200 {
		t.Fatalf("reviewer safety margin=%d want=%d", margin, want)
	}
}

func TestReleaseAnalyzerDoesNotDelayProductionWindow(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	journal := openCampaignTestJournal(t, stateDir)
	releaseAnalysisStarted := make(chan struct{})
	allowReleaseAnalysis := make(chan struct{})
	productionStarted := make(chan struct{})
	runner := func(_ context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
		switch name {
		case "release-1.0":
			writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 26, 32)
		case "production-soak":
			close(productionStarted)
			writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 32, 36)
		default:
			return fmt.Errorf("unexpected phase %s", name)
		}
		return nil
	}
	analyzer := func(ctx context.Context, _ *ResolvedConfig, _, _ string, _ *RoleSecrets, result *ScenarioResult) error {
		if result.Name != "release-1.0" {
			return nil
		}
		close(releaseAnalysisStarted)
		select {
		case <-allowReleaseAnalysis:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, analyzer)
	}()
	select {
	case <-releaseAnalysisStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("release analyzer did not start")
	}
	select {
	case <-productionStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("production waited for release semantic analysis")
	}
	close(allowReleaseAnalysis)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReleaseAnalyzerFailureCancelsLiveProduction(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	journal := openCampaignTestJournal(t, stateDir)
	productionStarted := make(chan struct{})
	failure := errors.New("public semantic replay disagrees")
	runner := func(ctx context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
		if name == "release-1.0" {
			writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 26, 32)
			return nil
		}
		close(productionStarted)
		<-ctx.Done()
		return ctx.Err()
	}
	analyzer := func(ctx context.Context, _ *ResolvedConfig, _, _ string, _ *RoleSecrets, result *ScenarioResult) error {
		if result.Name != "release-1.0" {
			return nil
		}
		select {
		case <-productionStarted:
			return failure
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err = runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, analyzer)
	if !errors.Is(err, failure) || !errors.Is(err, context.Canceled) {
		t.Fatalf("analyzer cancellation error=%v", err)
	}
}

func TestCompletedCampaignStillRequiresBothSemanticAnalyzers(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	writeScenarioCampaignFixture(t, cfg, stateDir, "production-soak", 32, 36)
	journal := openCampaignTestJournal(t, stateDir)
	var mu sync.Mutex
	var analyzed []string
	analyzer := func(_ context.Context, _ *ResolvedConfig, _, _ string, _ *RoleSecrets, result *ScenarioResult) error {
		mu.Lock()
		defer mu.Unlock()
		analyzed = append(analyzed, result.Name)
		return nil
	}
	runner := func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor, *scenarioCampaignAttempt) error {
		return errors.New("completed live phase was rerun")
	}
	if err := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, analyzer); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(analyzed) != 2 || !(strings.Join(analyzed, ",") == "release-1.0,production-soak" || strings.Join(analyzed, ",") == "production-soak,release-1.0") {
		t.Fatalf("semantic analyzers=%v", analyzed)
	}
}

func TestCompletedCampaignRejectsEitherSemanticAnalyzerFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	writeScenarioCampaignFixture(t, cfg, stateDir, "production-soak", 32, 36)
	journal := openCampaignTestJournal(t, stateDir)
	failure := errors.New("semantic commit is absent")
	analyzer := func(_ context.Context, _ *ResolvedConfig, _, _ string, _ *RoleSecrets, result *ScenarioResult) error {
		if result.Name == "production-soak" {
			return failure
		}
		return nil
	}
	err := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor, *scenarioCampaignAttempt) error {
		return errors.New("completed live phase was rerun")
	}, noOpCampaignPreflight, analyzer)
	if !errors.Is(err, failure) {
		t.Fatalf("semantic failure error=%v", err)
	}
}

func TestArchivePreflightRunsOnlyBeforeMissingReleasePhase(t *testing.T) {
	cfg := testResolvedConfig(t)
	for _, test := range []struct {
		name          string
		seedRelease   bool
		wantPreflight int
	}{
		{name: "new campaign", wantPreflight: 2},
		{name: "release already captured", seedRelease: true, wantPreflight: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			roles, err := BuildRoleSecrets(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if test.seedRelease {
				_, roles, _ = writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
			}
			journal := openCampaignTestJournal(t, stateDir)
			preflights := 0
			preflight := func(context.Context, *ResolvedConfig, string) error {
				preflights++
				return nil
			}
			runner := func(_ context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
				if name == "release-1.0" {
					writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 26, 32)
				} else {
					writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 32, 36)
				}
				return nil
			}
			if err := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, preflight, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if preflights != test.wantPreflight {
				t.Fatalf("archive preflights=%d want=%d", preflights, test.wantPreflight)
			}
		})
	}
}

func TestArchivePreflightFailurePreventsEveryLiveCampaignWrite(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	journal := openCampaignTestJournal(t, stateDir)
	failure := errors.New("public reader pruned required history")
	runnerCalled := false
	analyzerCalled := false
	err = runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor, *scenarioCampaignAttempt) error {
		runnerCalled = true
		return nil
	}, func(context.Context, *ResolvedConfig, string) error {
		return failure
	}, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
		analyzerCalled = true
		return nil
	})
	if !errors.Is(err, failure) || runnerCalled || analyzerCalled {
		t.Fatalf("preflight failure error=%v runner_called=%t analyzer_called=%t", err, runnerCalled, analyzerCalled)
	}
}

func TestProductionArchivePreflightFailureCancelsReleaseAnalyzer(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	journal := openCampaignTestJournal(t, stateDir)
	failure := errors.New("production archive history is too shallow")
	releaseAnalyzerStarted := make(chan struct{})
	releaseAnalyzerStopped := make(chan struct{})
	preflights := 0
	err = runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, func(_ context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
		if name != "release-1.0" {
			return fmt.Errorf("unexpected live phase %s", name)
		}
		writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 26, 32)
		return nil
	}, func(context.Context, *ResolvedConfig, string) error {
		preflights++
		if preflights == 2 {
			return failure
		}
		return nil
	}, func(ctx context.Context, _ *ResolvedConfig, _, _ string, _ *RoleSecrets, result *ScenarioResult) error {
		if result.Name != "release-1.0" {
			return fmt.Errorf("unexpected analyzer phase %s", result.Name)
		}
		close(releaseAnalyzerStarted)
		<-ctx.Done()
		close(releaseAnalyzerStopped)
		return ctx.Err()
	})
	if !errors.Is(err, failure) || !errors.Is(err, context.Canceled) {
		t.Fatalf("production preflight cancellation error=%v", err)
	}
	if preflights != 2 {
		t.Fatalf("archive preflights=%d want=2", preflights)
	}
	select {
	case <-releaseAnalyzerStarted:
	default:
		t.Fatal("release analyzer did not start")
	}
	select {
	case <-releaseAnalyzerStopped:
	default:
		t.Fatal("release analyzer was not joined after cancellation")
	}
}

func TestCampaignParentCancellationJoinsSemanticAnalyzer(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	journal := openCampaignTestJournal(t, stateDir)
	ctx, cancel := context.WithCancel(context.Background())
	analyzerStarted := make(chan struct{})
	analyzerStopped := make(chan struct{})
	productionStarted := make(chan struct{})
	runner := func(runCtx context.Context, _ *ResolvedConfig, _, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
		if name != "production-soak" {
			return fmt.Errorf("unexpected live phase %s", name)
		}
		close(productionStarted)
		<-runCtx.Done()
		return runCtx.Err()
	}
	analyzer := func(runCtx context.Context, _ *ResolvedConfig, _, _ string, _ *RoleSecrets, result *ScenarioResult) error {
		if result.Name != "release-1.0" {
			return fmt.Errorf("unexpected analyzer phase %s", result.Name)
		}
		close(analyzerStarted)
		<-runCtx.Done()
		close(analyzerStopped)
		return runCtx.Err()
	}
	done := make(chan error, 1)
	go func() {
		done <- runReleaseCandidateCampaignWithAnalyzer(ctx, cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, analyzer)
	}()
	select {
	case <-analyzerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("release analyzer did not start")
	}
	select {
	case <-productionStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("production did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("parent cancellation error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("campaign did not join canceled work")
	}
	select {
	case <-analyzerStopped:
	default:
		t.Fatal("semantic analyzer was not joined")
	}
}
