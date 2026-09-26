//go:build linux || darwin

// Terminal diagnostics retain original evidence in a separate output tree.
// They never sign campaign evidence, advance an attempt, or grant acceptance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

const terminalDiagnosticSchema = "urnetwork-sim-terminal-diagnostics-v1"

// Each check owns its outcome; a missing prerequisite cannot look like a pass.
type terminalDiagnosticCheck struct {
	Id       string `json:"id"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Evidence any    `json:"evidence,omitempty"`
}

// An observed absence is a finding, not a failed evidence reader.
type terminalDiagnosticFinding string

func (self terminalDiagnosticFinding) Error() string { return string(self) }

// Source references describe exact original bytes, including failed evidence.
type terminalDiagnosticOriginal struct {
	Path   string               `json:"path"`
	Sha256 string               `json:"sha256"`
	Bytes  uint64               `json:"bytes"`
	Copy   FinalArtifactLocator `json:"copy"`
}

// This schema is deliberately distinct from a result, completion or supplement.
type terminalDiagnosticReport struct {
	Schema             string                       `json:"schema"`
	ReadOnly           bool                         `json:"read_only"`
	FinalAcceptance    bool                         `json:"final_acceptance"`
	Status             string                       `json:"status"`
	RunId              string                       `json:"run_id"`
	Phase              string                       `json:"phase"`
	PlanHash           string                       `json:"plan_hash"`
	DeploymentId       string                       `json:"deployment_id"`
	StartedAt          string                       `json:"started_at"`
	CompletedAt        string                       `json:"completed_at,omitempty"`
	OutputDirectory    string                       `json:"output_directory"`
	Driver             provisionalDriverProvenance  `json:"actual_driver"`
	Window             *ScenarioAcceptanceWindow    `json:"acceptance_window,omitempty"`
	LifecycleException any                          `json:"lifecycle_exception,omitempty"`
	Originals          []terminalDiagnosticOriginal `json:"originals"`
	Checks             []terminalDiagnosticCheck    `json:"checks"`
	EvidenceHash       string                       `json:"evidence_hash,omitempty"`
}

// One invocation owns the output and all check state; no methods are concurrent.
type terminalDiagnosticCollector struct {
	ctx        context.Context
	report     *terminalDiagnosticReport
	output     string
	persistErr error
}

// Keep accumulating after failed checks, including after caller cancellation.
func (self *terminalDiagnosticCollector) check(id, prerequisite string, timeout time.Duration, run func(context.Context) (any, error)) bool {
	check := terminalDiagnosticCheck{Id: id, Status: "unavailable"}
	if prerequisite != "" {
		check.Detail = prerequisite
	} else if err := self.ctx.Err(); err != nil {
		check.Detail = err.Error()
	} else {
		ctx, cancel := context.WithTimeout(self.ctx, timeout)
		evidence, err := func() (evidence any, runErr error) {
			defer func() {
				if failure := recover(); failure != nil {
					evidence, runErr = nil, fmt.Errorf("diagnostic check panicked: %v", failure)
				}
			}()
			return run(ctx)
		}()
		check.Evidence = evidence
		cancel()
		check.Status = "pass"
		if err != nil {
			check.Status, check.Detail = "fail", err.Error()
			var finding terminalDiagnosticFinding
			if errors.As(err, &finding) {
				check.Status = "finding"
			}
			if errors.Is(err, os.ErrNotExist) {
				check.Status = "unavailable"
			}
		}
	}
	self.report.Checks = append(self.report.Checks, check)
	if self.output != "" {
		self.persistErr = errors.Join(self.persistErr, writePublicJSON(filepath.Join(self.output, "progress.json"), self.report))
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: terminal diagnostic %s: %s; final_acceptance=false\n", id, check.Status)
	return check.Status == "pass"
}

// An external append-only copy preserves the original bytes and their location.
func (self *terminalDiagnosticCollector) retain(path string, raw []byte) error {
	hash := bytesSHA256(raw)
	if len(raw) > maximumCampaignObservationHistoryBytes {
		return errors.New("diagnostic original exceeds retained observation bound")
	}
	locator, err := persistFinalCollectedArtifact(self.output, "terminal-diagnostic-original", "originals/"+strings.TrimPrefix(hash, "sha256:")+".bin", raw)
	if err != nil {
		return err
	}
	self.report.Originals = append(self.report.Originals, terminalDiagnosticOriginal{Path: path, Sha256: hash, Bytes: uint64(len(raw)), Copy: locator})
	return nil
}

// A diagnostic can read a live deployment but cannot create output within it.
func validateTerminalDiagnosticOutput(stateDir, output string) error {
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || !filepath.IsAbs(output) || filepath.Clean(output) != output || output == stateDir || pathWithinRoot(stateDir, output) || pathWithinRoot(output, stateDir) {
		return errors.New("terminal diagnostic output must be a new canonical directory outside deployment state")
	}
	stateRoot, err := filepath.EvalSymlinks(stateDir)
	if err != nil || stateRoot != stateDir {
		return errors.Join(errors.New("terminal diagnostic state root is missing or traverses a symlink"), err)
	}
	parent := filepath.Dir(output)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return errors.Join(errors.New("terminal diagnostic output parent is missing or traverses a symlink"), err)
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errors.New("terminal diagnostic output already exists or is inaccessible"), err)
	}
	return nil
}

// CLI admission is read-only even when another command would consume --apply.
func validateTerminalDiagnosticOptions(command string, options cliOptions) error {
	if command != "terminal-diagnostics" {
		if options.DiagnosticOutput != "" || options.WaitForTerminal {
			return errors.New("diagnostic output/wait options require terminal-diagnostics")
		}
		return nil
	}
	if options.Apply || options.Detach || options.PrepareOnly || options.ThenReleaseCandidate || options.ProvisionalCapture || !options.ProvisionalResume || options.Manifest != "" || options.StrictHistoryAdoption != "" || options.ProvisionalRPCAuthority != "" || options.OwnedRPCAuthority == "" || !validCanonicalHashHex(options.PlanHash) || !filepath.IsAbs(options.DiagnosticOutput) || options.RunID == "" || options.RunID != strings.TrimSpace(options.RunID) || strings.ContainsAny(options.RunID, "/\\\r\n\x00") || options.Name != "release-1.0" && options.Name != "production-soak" {
		return errors.New("terminal-diagnostics requires exact --run-id, --name, --plan-hash, --provisional-resume and external --diagnostic-output; process/chain mutation options are forbidden")
	}
	return nil
}

// Only external provenance is written. The retained plan and role files are
// authenticated through the same read-only identity readers as continuation.
func prepareTerminalDiagnosticConfig(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) (*ResolvedConfig, *SetupPlan, *RoleSecrets, error) {
	if err := validateTerminalDiagnosticOptions("terminal-diagnostics", options); err != nil {
		return nil, nil, nil, err
	}
	if cfg == nil || cfg.provisionalResume == nil || !releaseSHA256.MatchString(cfg.provisionalResume.Driver.ExecutableSHA256) {
		return nil, nil, nil, errors.New("terminal diagnostic driver is not authenticated")
	}
	if err := validateTerminalDiagnosticOutput(stateDir, options.DiagnosticOutput); err != nil {
		return nil, nil, nil, err
	}
	plan, err := loadInvocationPlan(cfg, stateDir, "terminal-diagnostics", options)
	if err != nil {
		return nil, nil, nil, err
	}
	accepted, err := provisionalAcceptedPlanHashes(plan)
	if err != nil {
		return nil, nil, nil, err
	}
	resolved := *cfg
	if plan.PolicyRateAmendment != nil {
		amended, err := configWithPolicyRateAmendment(&resolved, plan)
		if err != nil {
			return nil, nil, nil, err
		}
		resolved = *amended
	}
	roles, err := loadExistingProvisionalRoles(&resolved, stateDir)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if err := os.Mkdir(options.DiagnosticOutput, 0o700); err != nil {
		return nil, nil, nil, err
	}
	driver := cfg.provisionalResume.Driver
	record := &provisionalResumeRecord{Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false, ReadOnly: true,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Command: "terminal-diagnostics", Scenario: options.Name,
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: plan.PlanHash, ConfigHash: cfg.ConfigHash, ReleaseLockHash: plan.ReleaseLockHash, RetainedSNRepo: cfg.Repos.SN, Driver: driver}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}
	raw = append(raw, '\n')
	path := filepath.Join(options.DiagnosticOutput, "provenance.json")
	if err := atomicWrite(path, raw, 0o600); err != nil {
		return nil, nil, nil, err
	}
	resolved.readOnlyAudit = true
	resolved.provisionalResume = &provisionalResumeState{Driver: driver, Record: record, RecordPath: path, RecordHash: bytesSHA256(raw), AcceptedPlanHashes: accepted}
	return runtimePlanReadScopeConfig(&resolved), plan, roles, nil
}

// The signed start determines the terminal block. Only finalized read calls
// are made while waiting, and cancellation or a 24-hour ceiling ends the wait.
func waitTerminalDiagnosticHead(ctx context.Context, cfg *ResolvedConfig, plan *SetupPlan, window *ScenarioAcceptanceWindow) error {
	if window == nil || window.TerminalBlock == 0 {
		return errors.New("terminal diagnostic wait lacks an authenticated window")
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	for {
		attempt, done := context.WithTimeout(ctx, 30*time.Second)
		chain, err := validatorpkg.DialReleaseChainContext(attempt, []string{cfg.OperationalEVM}, plan.Deployment.CoordinatorProxy)
		if err == nil {
			var number uint64
			number, _, err = chain.FinalizedBlockContext(attempt)
			chain.Close()
			if err == nil && number >= window.TerminalBlock {
				done()
				return nil
			}
			if err == nil {
				fmt.Fprintf(os.Stderr, "sim-testnet: terminal diagnostic waiting at block %d for %d\n", number, window.TerminalBlock)
			}
		}
		done()
		if err != nil {
			fmt.Fprintf(os.Stderr, "sim-testnet: terminal diagnostic head read: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
		}
	}
}

// Authentication failures remain findings. Every independent terminal class
// is attempted, and the original run is never relabelled or completed here.
func runTerminalDiagnostics(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) (*terminalDiagnosticReport, error) {
	reader, plan, roles, err := prepareTerminalDiagnosticConfig(ctx, cfg, stateDir, options)
	if err != nil {
		return nil, err
	}
	report := &terminalDiagnosticReport{Schema: terminalDiagnosticSchema, ReadOnly: true, FinalAcceptance: false, Status: "running", RunId: options.RunID, Phase: options.Name,
		PlanHash: plan.PlanHash, DeploymentId: plan.DeploymentID, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), OutputDirectory: options.DiagnosticOutput, Driver: reader.provisionalResume.Driver}
	collector := &terminalDiagnosticCollector{ctx: ctx, report: report, output: options.DiagnosticOutput}
	runDir := filepath.Join(stateDir, "runs", options.RunID)
	var start, attempt *scenarioCampaignAttempt
	var startRaw, attemptRaw []byte
	startOk := collector.check("signed-acceptance-start", "", 5*time.Minute, func(context.Context) (any, error) {
		var err error
		path := filepath.Join(runDir, scenarioCampaignStartFilename)
		start, startRaw, err = readScenarioCampaignAttemptAt(reader, stateDir, roles, plan.PlanHash, options.Name, path)
		if err != nil {
			return nil, err
		}
		if start.payload.RunID != options.RunID || start.payload.AcceptanceBoundary == nil || start.payload.AcceptanceInvalidation != "" {
			return nil, errors.New("signed start differs from exact requested non-invalidated run")
		}
		report.Window = &start.payload.AcceptanceBoundary.AcceptanceWindow
		return start.payload, collector.retain(path, startRaw)
	})
	if options.WaitForTerminal && startOk {
		waited := collector.check("wait-finalized-terminal", "", 24*time.Hour, func(ctx context.Context) (any, error) {
			return report.Window, waitTerminalDiagnosticHead(ctx, reader, plan, report.Window)
		})
		if waited {
			collector.check("wait-original-terminal-result", "", 16*time.Minute, func(ctx context.Context) (any, error) {
				return nil, waitTerminalDiagnosticResult(ctx, stateDir, options.RunID)
			})
		}
	}
	prerequisite := func(ok bool, detail string) string {
		if ok {
			return ""
		}
		return detail
	}
	attemptOk := collector.check("signed-latest-checkpoint", prerequisite(startOk, "signed acceptance start unavailable"), 5*time.Minute, func(context.Context) (any, error) {
		var err error
		attempt, attemptRaw, err = readScenarioCampaignAttemptAt(reader, stateDir, roles, plan.PlanHash, options.Name, start.path())
		if err != nil {
			return nil, err
		}
		if attempt.payload.RunID != options.RunID || !scenarioCampaignRecoveryStaticBoundaryMatches(start.payload.AcceptanceBoundary, attempt.payload.AcceptanceBoundary) {
			return nil, errors.New("latest checkpoint replaces the signed start identity")
		}
		if err := collector.retain(start.path(), attemptRaw); err != nil {
			return nil, err
		}
		return attempt.payload, nil
	})
	collector.check("acceptance-not-invalidated", prerequisite(attemptOk, "signed checkpoint unavailable"), time.Minute, func(context.Context) (any, error) {
		if attempt.payload.AcceptanceInvalidation != "" {
			return attempt.payload, errors.New("acceptance invalidated: " + attempt.payload.AcceptanceInvalidation)
		}
		return nil, nil
	})
	collector.check("full-retained-campaign-lineage", prerequisite(attemptOk, "signed checkpoint unavailable"), 10*time.Minute, func(context.Context) (any, error) {
		current, err := readScenarioCampaignAttempt(reader, stateDir, roles, plan.PlanHash, options.Name)
		if err != nil {
			return nil, err
		}
		if current.payload.RunID != options.RunID {
			return nil, errors.New("current campaign owner no longer selects the requested run")
		}
		return map[string]any{"run_id": current.payload.RunID, "recovery": current.payload.Recovery}, nil
	})
	var history []*ScenarioObservation
	var baseline, terminal *ScenarioObservation
	historyOk := collector.check("signed-observation-prefix", prerequisite(attemptOk, "signed checkpoint unavailable"), 5*time.Minute, func(context.Context) (any, error) {
		path := filepath.ToSlash(filepath.Join("runs", options.RunID, "observations.jsonl"))
		raw, err := readCampaignObservationHistory(stateDir, path)
		if err != nil {
			return nil, err
		}
		if err := collector.retain(filepath.Join(stateDir, filepath.FromSlash(path)), raw); err != nil {
			return nil, err
		}
		boundary := attempt.payload.AcceptanceBoundary
		if boundary == nil || boundary.ObservationLogBytes > uint64(len(raw)) {
			return nil, errors.New("observation log is shorter than the signed checkpoint")
		}
		history, _, baseline, terminal, err = validateScenarioAttemptObservationHistory(boundary, raw[:boundary.ObservationLogBytes])
		if err != nil {
			return nil, err
		}
		return map[string]any{"observations": len(history), "authenticated_prefix_bytes": boundary.ObservationLogBytes, "uncredited_suffix_bytes": uint64(len(raw)) - boundary.ObservationLogBytes, "terminal_head": boundary.LastObservationHead}, nil
	})
	terminalOk := collector.check("complete-epoch-terminal", prerequisite(historyOk, "authenticated observation history unavailable"), time.Minute, func(context.Context) (any, error) {
		if !scenarioAcceptanceIntervalObserved(report.Window, terminal) {
			return report.Window, errors.New("signed observation prefix has not reached terminal finalization")
		}
		return terminal.Status.Contracts.FinalizedHead, nil
	})
	var result *ScenarioResult
	resultOk := collector.check("original-scenario-result", "", time.Minute, func(context.Context) (any, error) {
		path := filepath.ToSlash(filepath.Join("runs", options.RunID, "result.json"))
		raw, err := readValidatorEvidenceHistoricalFile(stateDir, path, maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return nil, err
		}
		if err := collector.retain(filepath.Join(stateDir, filepath.FromSlash(path)), raw); err != nil {
			return nil, err
		}
		var value ScenarioResult
		if err := decodeStrictJSONBytes(raw, &value); err != nil {
			return nil, err
		}
		hash, err := canonicalScenarioResultHash(&value)
		if err != nil || hash != value.EvidenceHash || value.RunID != options.RunID || value.Name != options.Name || value.ConfigHash != reader.ConfigHash || value.PolicyHash != reader.PolicyHash || value.DeploymentID != reader.Config.Deployment.DeploymentID {
			return nil, errors.Join(errors.New("original scenario result hash or identity differs"), err)
		}
		result = &value
		return result, nil
	})
	collector.check("owner-signed-completion", prerequisite(resultOk, "original result unavailable"), 10*time.Minute, func(ctx context.Context) (any, error) {
		_, complete, err := loadCompletedScenarioCampaignByRunIdContext(ctx, reader, stateDir, roles, options.Name, options.RunID)
		return complete, err
	})
	collector.check("result-start-and-fault-binding", prerequisite(resultOk && startOk, "original result or signed start unavailable"), time.Minute, func(context.Context) (any, error) {
		return nil, validateScenarioCampaignStartMarkerBytes(reader, result, options.Name, roles.EVM["testnet-owner"].Address, startRaw)
	})
	definition, definitionErr := scenarioDefinitionFor(reader, options.Name)
	collectTerminalDiagnosticEpochObservationFields(collector, terminal, prerequisite(terminalOk, "authenticated terminal observation unavailable"))
	collector.check("terminal-scenario-assertions", prerequisite(terminalOk && definitionErr == nil, "terminal observation or scenario definition unavailable"), 5*time.Minute, func(context.Context) (any, error) {
		started, err := time.Parse(time.RFC3339Nano, attempt.payload.AcceptanceBoundary.AcceptanceStartedAt)
		if err != nil {
			return nil, err
		}
		assertions := evaluateScenarioInterval(reader, definition, baseline, terminal, report.Window, attempt.payload.AcceptanceBoundary.Faults, started)
		var failures []error
		for _, assertion := range assertions {
			if !assertion.Passed {
				failures = append(failures, fmt.Errorf("%s: %s", assertion.ID, assertion.Message))
			}
		}
		return assertions, errors.Join(failures...)
	})
	collector.check("strict-final-acceptance-gate", prerequisite(resultOk, "original terminal result unavailable; diagnostics cannot grant acceptance"), time.Minute, func(context.Context) (any, error) {
		if result == nil {
			return nil, errors.New("no terminal result; diagnostic invocation cannot grant acceptance")
		}
		strict := *reader
		strict.provisionalResume = nil
		return nil, validateScenarioCampaignResult(&strict, result, options.Name)
	})
	if historyOk && terminal.FleetLifecycle != nil && terminal.FleetLifecycle.ProvisionalBypass != nil {
		report.LifecycleException = terminal.FleetLifecycle.ProvisionalBypass
		report.Checks = append(report.Checks, terminalDiagnosticCheck{Id: "inherited-lifecycle-exception", Status: "exception", Detail: "Inherited provisional lifecycle bypass remains original evidence; no prune/re-register qualification is claimed.", Evidence: terminal.FleetLifecycle.ProvisionalBypass})
	}
	collectTerminalDiagnosticSources(collector, reader, stateDir, runDir, attempt, result, terminal, history, terminalOk)
	report.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	report.Status = "complete"
	for _, check := range report.Checks {
		if check.Status != "pass" {
			report.Status = "complete-with-findings"
			break
		}
	}
	report.EvidenceHash, err = canonicalHashHex(report)
	if err == nil {
		err = writePublicJSON(filepath.Join(options.DiagnosticOutput, "report.json"), report)
	}
	return report, errors.Join(err, collector.persistErr)
}

// A terminal head can precede the runner's final local write by one observation
// cycle. Wait finitely for that original result, without acquiring its writer.
func waitTerminalDiagnosticResult(ctx context.Context, stateDir, runId string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	name := filepath.ToSlash(filepath.Join("runs", runId, "result.json"))
	for {
		raw, err := readValidatorEvidenceHistoricalFile(stateDir, name, maximumCampaignEvidenceRawFileBytes)
		if err == nil {
			var result ScenarioResult
			if err := decodeStrictJSONBytes(raw, &result); err != nil {
				return err
			}
			if result.RunID != runId || result.CompletedAt == "" || result.Result != "pass" && result.Result != "fail" {
				return errors.New("terminal result has an incomplete or different run identity")
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return terminalDiagnosticFinding("terminal block reached; original result remains unavailable after bounded wait: " + ctx.Err().Error())
		case <-time.After(10 * time.Second):
		}
	}
}
