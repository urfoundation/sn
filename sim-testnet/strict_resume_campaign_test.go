package main

// Exercise the real writer handoff and campaign gates with synthetic approvals.
// No process supervisor, RPC endpoint or wall-clock epoch is needed.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Keep an actual journal lock while substituting only launch and campaign I/O.
func strictResumeCampaignTestWriter(t *testing.T, ctx context.Context) (*Executor, cliOptions) {
	t.Helper()
	executor := launchPreparationTestExecutor(t)
	options := cliOptions{ThenReleaseCandidate: true, Apply: true, Detach: true, PlanHash: executor.plan.PlanHash,
		StrictHistoryAdoption: filepath.Join(executor.stateDir, "history-adoption.json"), StrictHistoryAdoptionSHA256: "sha256:" + strings.Repeat("31", 32)}
	executor.cfg.strictHistoryAdoption = &strictHistoryAdoptionState{path: options.StrictHistoryAdoption, hash: options.StrictHistoryAdoptionSHA256,
		bundle: strictHistoryAdoptionBundle{ApprovedPlanHash: options.PlanHash}, invocationContext: ctx}
	return executor, options
}

// Both command parsing and direct mutation reject ambiguity before any state
// access. Ordinary standalone commands keep their previous option contract.
func TestStrictHistoryAdoptionCampaignOptionsPrecedeWrites(t *testing.T) {
	_, options := strictResumeCampaignTestWriter(t, context.Background())
	args := []string{"resume", "--then-release-candidate", "--apply", "--detach", "--plan-hash", options.PlanHash,
		"--strict-history-adoption", options.StrictHistoryAdoption, "--strict-history-adoption-sha256", options.StrictHistoryAdoptionSHA256}
	command, parsed, err := parseCLI(args)
	if err != nil || command != "resume" || !parsed.ThenReleaseCandidate || parsed.PlanHash != options.PlanHash {
		t.Fatalf("combined command was not admitted: command=%s options=%+v err=%v", command, parsed, err)
	}
	for _, test := range []struct {
		name    string
		command string
		mutate  func(*cliOptions)
	}{
		{name: "standalone scenario", command: "scenario"},
		{name: "launch", command: "launch"},
		{name: "setup", command: "setup"},
		{name: "dry run", mutate: func(o *cliOptions) { o.Apply = false }},
		{name: "foreground supervisor", mutate: func(o *cliOptions) { o.Detach = false }},
		{name: "missing plan", mutate: func(o *cliOptions) { o.PlanHash = "" }},
		{name: "noncanonical plan", mutate: func(o *cliOptions) { o.PlanHash = "sha256:approved" }},
		{name: "missing history", mutate: func(o *cliOptions) { o.StrictHistoryAdoption = "" }},
		{name: "missing history hash", mutate: func(o *cliOptions) { o.StrictHistoryAdoptionSHA256 = "" }},
		{name: "noncanonical history hash", mutate: func(o *cliOptions) { o.StrictHistoryAdoptionSHA256 = "sha256:approved" }},
		{name: "preparation only", mutate: func(o *cliOptions) { o.PrepareOnly = true }},
		{name: "provisional", mutate: func(o *cliOptions) { o.ProvisionalResume = true }},
		{name: "provisional route", mutate: func(o *cliOptions) { o.ProvisionalRPCAuthority = "127.0.0.1:9944" }},
		{name: "provisional time", mutate: func(o *cliOptions) { o.ProvisionalObservationTimeout = 1 }},
		{name: "partial scenario", mutate: func(o *cliOptions) { o.Name = "epoch" }},
	} {
		candidate := options
		if test.mutate != nil {
			test.mutate(&candidate)
		}
		command := test.command
		if command == "" {
			command = "resume"
		}
		if err := runMutation(context.Background(), command, nil, "", candidate); err == nil || !strings.Contains(err.Error(), "--then-release-candidate") {
			t.Fatalf("%s reached mutation preparation: %v", test.name, err)
		}
	}
	for _, command := range []string{"setup", "launch", "resume", "scenario"} {
		if _, parsed, err := parseCLI([]string{command}); err != nil || parsed.ThenReleaseCandidate {
			t.Fatalf("ordinary %s changed: options=%+v err=%v", command, parsed, err)
		}
		invalid := append([]string{command}, args[1:]...)
		if command != "resume" {
			if _, _, err := parseCLI(invalid); err == nil {
				t.Fatalf("combined flag accepted on %s", command)
			}
		}
	}
}

// A bad second phase must fail before even opening the missing plan or writer.
func TestStrictHistoryAdoptionCampaignDefinitionsPrecedePreparation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*HarnessConfig)
		want   string
	}{
		{name: "release", mutate: func(cfg *HarnessConfig) { cfg.Scenarios.ShortEpochs = 0 }, want: "short_epochs"},
		{name: "production", mutate: func(cfg *HarnessConfig) { cfg.Scenarios.ProductionEpochs = 2 }, want: "three complete"},
	} {
		executor, options := strictResumeCampaignTestWriter(t, context.Background())
		budget := runtimeAttemptUploadTestBudget()
		executor.cfg.Config.Artifacts.AttemptUpload = &budget
		test.mutate(executor.cfg.Config)
		stateDir := filepath.Join(t.TempDir(), "untouched")
		err := runMutation(context.Background(), "resume", executor.cfg, stateDir, options)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s definition did not reject before preparation: %v", test.name, err)
		}
		if _, err := os.Stat(stateDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s definition touched state: %v", test.name, err)
		}
	}
}

// The actual deployment lock, executor and authenticated carry survive exactly
// one launch-to-campaign boundary. Only combined completion yields a result.
func TestStrictHistoryAdoptionCampaignReusesPreparedWriterUntilComplete(t *testing.T) {
	ctx := context.Background()
	executor, options := strictResumeCampaignTestWriter(t, ctx)
	executor.carriedVerificationKeys = map[string]bool{"verified ancestor": true}
	binaries := map[string]string{"validator": "retained-validator"}
	var order []string
	checkWriter := func() {
		other, err := OpenJournal(executor.stateDir)
		if err == nil {
			other.Close()
			t.Fatal("handoff released its deployment writer lock")
		}
		if !strings.Contains(err.Error(), "locked by another process") || !executor.carriedVerificationKeys["verified ancestor"] {
			t.Fatalf("prepared writer was not retained: %v", err)
		}
	}
	result, err := runStrictResumeCampaign(ctx, options, executor, binaries,
		func(gotCtx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, roles *RoleSecrets, got *Executor, gotBinaries map[string]string, detach bool) error {
			if gotCtx != ctx || cfg != executor.cfg || stateDir != executor.stateDir || plan != executor.plan || roles != executor.roles || got != executor || !reflect.DeepEqual(gotBinaries, binaries) || !detach || len(order) != 0 {
				t.Fatal("launch did not receive the original strict writer")
			}
			checkWriter()
			order = append(order, "launch")
			return nil
		},
		func(gotCtx context.Context, cfg *ResolvedConfig, stateDir string, journal *Journal, got *Executor, roles *RoleSecrets, runner scenarioCampaignRunner) error {
			if gotCtx != ctx || cfg != executor.cfg || stateDir != executor.stateDir || journal != executor.journal || got != executor || roles != executor.roles || runner == nil || !reflect.DeepEqual(order, []string{"launch"}) {
				t.Fatal("campaign did not follow launch with the original writer")
			}
			checkWriter()
			order = append(order, "release-candidate")
			return nil
		})
	if err != nil || !reflect.DeepEqual(order, []string{"launch", "release-candidate"}) || result["command"] != "resume" || result["scenario"] != releaseCandidateCampaignName || result["campaign_complete"] != true || result["plan_hash"] != options.PlanHash {
		t.Fatalf("combined completion lost its handoff: order=%v result=%v err=%v", order, result, err)
	}
	checkWriter()
}

// Cancellation never advances to the next owner, and neither startup nor
// campaign failure can publish the standalone resume success result.
func TestStrictHistoryAdoptionCampaignStopsOnFailureOrCancellation(t *testing.T) {
	failure := errors.New("synthetic owned phase failure")
	for _, test := range []struct {
		name         string
		cancelAt     string
		failAt       string
		wantLaunch   int
		wantCampaign int
	}{
		{name: "already canceled", cancelAt: "before", wantLaunch: 0, wantCampaign: 0},
		{name: "startup failed", failAt: "launch", wantLaunch: 1, wantCampaign: 0},
		{name: "startup canceled", cancelAt: "launch", wantLaunch: 1, wantCampaign: 0},
		{name: "campaign failed", failAt: "campaign", wantLaunch: 1, wantCampaign: 1},
		{name: "campaign canceled", cancelAt: "campaign", wantLaunch: 1, wantCampaign: 1},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		executor, options := strictResumeCampaignTestWriter(t, ctx)
		if test.cancelAt == "before" {
			cancel()
		}
		launchCalls, campaignCalls := 0, 0
		result, err := runStrictResumeCampaign(ctx, options, executor, nil,
			func(gotCtx context.Context, _ *ResolvedConfig, _ string, _ *SetupPlan, _ *RoleSecrets, _ *Executor, _ map[string]string, _ bool) error {
				launchCalls++
				if gotCtx != ctx {
					t.Fatal("startup detached from parent cancellation")
				}
				if test.cancelAt == "launch" {
					cancel()
				}
				if test.failAt == "launch" {
					return failure
				}
				return nil
			},
			func(gotCtx context.Context, _ *ResolvedConfig, _ string, _ *Journal, _ *Executor, _ *RoleSecrets, _ scenarioCampaignRunner) error {
				campaignCalls++
				if gotCtx != ctx {
					t.Fatal("campaign detached from parent cancellation")
				}
				if test.cancelAt == "campaign" {
					cancel()
				}
				if test.failAt == "campaign" {
					return failure
				}
				return nil
			})
		cancel()
		want := error(context.Canceled)
		if test.failAt != "" {
			want = failure
		}
		if !errors.Is(err, want) || result != nil || launchCalls != test.wantLaunch || campaignCalls != test.wantCampaign {
			t.Fatalf("%s: launch=%d campaign=%d result=%v err=%v", test.name, launchCalls, campaignCalls, result, err)
		}
	}
}

// An authenticated request and its writer cannot be replaced by a partial,
// provisional or differently approved owner before or during startup.
func TestStrictHistoryAdoptionCampaignRefusesChangedPreparedOwner(t *testing.T) {
	for _, test := range []struct {
		name         string
		duringLaunch bool
		mutate       func(*Executor)
	}{
		{name: "incomplete preparation", mutate: func(e *Executor) { e.preparationIncomplete = true }},
		{name: "missing adoption", mutate: func(e *Executor) { e.cfg.strictHistoryAdoption = nil }},
		{name: "provisional owner", mutate: func(e *Executor) {
			e.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{}}
		}},
		{name: "changed request", mutate: func(e *Executor) { e.cfg.strictHistoryAdoption.path += ".other" }},
		{name: "changed request hash", mutate: func(e *Executor) { e.cfg.strictHistoryAdoption.hash = "sha256:" + strings.Repeat("32", 32) }},
		{name: "changed approved plan", mutate: func(e *Executor) { e.plan.PlanHash = "0x" + strings.Repeat("33", 32) }},
		{name: "changed request approval", mutate: func(e *Executor) {
			e.cfg.strictHistoryAdoption.bundle.ApprovedPlanHash = "0x" + strings.Repeat("33", 32)
		}},
		{name: "journal replaced during startup", duringLaunch: true, mutate: func(e *Executor) { e.journal = &Journal{} }},
		{name: "plan replaced during startup", duringLaunch: true, mutate: func(e *Executor) { copy := *e.plan; e.plan = &copy }},
		{name: "roles replaced during startup", duringLaunch: true, mutate: func(e *Executor) { e.roles = &RoleSecrets{} }},
		{name: "approval changed during startup", duringLaunch: true, mutate: func(e *Executor) { e.plan.PlanHash = "0x" + strings.Repeat("33", 32) }},
		{name: "request changed during startup", duringLaunch: true, mutate: func(e *Executor) { e.cfg.strictHistoryAdoption.path += ".other" }},
		{name: "request hash changed during startup", duringLaunch: true, mutate: func(e *Executor) { e.cfg.strictHistoryAdoption.hash = "sha256:" + strings.Repeat("32", 32) }},
	} {
		executor, options := strictResumeCampaignTestWriter(t, context.Background())
		if !test.duringLaunch {
			test.mutate(executor)
		}
		launchCalls, campaignCalls := 0, 0
		result, err := runStrictResumeCampaign(context.Background(), options, executor, nil,
			func(context.Context, *ResolvedConfig, string, *SetupPlan, *RoleSecrets, *Executor, map[string]string, bool) error {
				launchCalls++
				test.mutate(executor)
				return nil
			},
			func(context.Context, *ResolvedConfig, string, *Journal, *Executor, *RoleSecrets, scenarioCampaignRunner) error {
				campaignCalls++
				return nil
			})
		wantLaunch := 0
		if test.duringLaunch {
			wantLaunch = 1
		}
		if err == nil || result != nil || launchCalls != wantLaunch || campaignCalls != 0 {
			t.Fatalf("%s: launch=%d campaign=%d result=%v err=%v", test.name, launchCalls, campaignCalls, result, err)
		}
	}
}

// The handoff enters the real durable campaign boundary: archive refusal and
// missing final semantic analysis remain failures after a successful launch.
func TestStrictHistoryAdoptionCampaignPreservesFullCampaignGates(t *testing.T) {
	for _, completed := range []bool{false, true} {
		ctx := context.Background()
		executor, options := strictResumeCampaignTestWriter(t, ctx)
		executor.plan.PlanHash, options.PlanHash = campaignTestPlanHash, campaignTestPlanHash
		executor.cfg.strictHistoryAdoption.bundle.ApprovedPlanHash = options.PlanHash
		var err error
		if completed {
			_, executor.roles, _ = writeReleaseCampaignFixture(t, executor.cfg, executor.stateDir, 26, 32)
			writeScenarioCampaignFixture(t, executor.cfg, executor.stateDir, "production-soak", 32, 36)
		} else {
			executor.roles, err = BuildRoleSecrets(executor.cfg)
			if err != nil {
				t.Fatal(err)
			}
		}
		failure := errors.New("required campaign evidence is unavailable")
		result, err := runStrictResumeCampaign(ctx, options, executor, nil,
			func(context.Context, *ResolvedConfig, string, *SetupPlan, *RoleSecrets, *Executor, map[string]string, bool) error {
				return nil
			},
			func(ctx context.Context, cfg *ResolvedConfig, stateDir string, journal *Journal, executor *Executor, roles *RoleSecrets, _ scenarioCampaignRunner) error {
				return runReleaseCandidateCampaignWithAnalyzer(ctx, cfg, stateDir, journal, executor, roles,
					func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor, *scenarioCampaignAttempt) error {
						return errors.New("unexpected live phase dispatch")
					},
					func(context.Context, *ResolvedConfig, string) error { return failure },
					func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
						return failure
					})
			})
		if !errors.Is(err, failure) || result != nil {
			t.Fatalf("completed=%t: campaign evidence failure was bypassed: result=%v err=%v", completed, result, err)
		}
	}
}
