// A replacement provisional driver retires an interrupted acceptance interval
// before opening live workers. Signed evidence and the deployment remain intact;
// only the new interval receives a fresh process session and observation window.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Recovery requires the live exclusive journal owner and the exact non-accepting
// approval. A read-only auditor must never turn a foreign session into a write.
func validateScenarioCampaignRecoveryOwner(cfg *ResolvedConfig, stateDir, planHash string, journal *Journal) error {
	if cfg == nil || cfg.Config == nil || journal == nil || cfg.readOnlyAudit || !provisionalResumeEnabled(cfg) || cfg.provisionalResume.Record == nil || cfg.provisionalResume.Record.PlanHash != planHash || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance {
		return errors.New("campaign recovery requires the exact provisional non-accepting approval")
	}
	owned := func() bool {
		journal.mu.Lock()
		defer journal.mu.Unlock()
		return journal.lock != nil && journal.file != nil && journal.path == filepath.Join(stateDir, "journal.jsonl")
	}()
	if !owned {
		return errors.New("campaign recovery has no live exclusive deployment journal owner")
	}
	return nil
}

// A missing marker permits recovery; any existing entry, including a malformed
// or dangling one, protects completed work from being replaced.
func requireScenarioCampaignRecoveryAbsent(path, detail string) error {
	if _, err := os.Lstat(path); err == nil {
		return errors.New(detail)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Run after checking completion and before preflight or worker construction.
// The phase lock covers invalidation and successor publication. If interrupted
// between them, the next invocation finishes the existing recovery protocol.
// An unstarted attempt keeps its identity and completed preparation unchanged.
func recoverScenarioCampaignProcessSession(ctx context.Context, attempt *scenarioCampaignAttempt, journal *Journal, processSessionId string, now time.Time) (*scenarioCampaignAttempt, error) {
	if ctx == nil {
		return nil, errors.New("campaign process recovery context is absent")
	}
	if attempt == nil || attempt.payload.AcceptanceBoundary == nil || !provisionalResumeEnabled(attempt.cfg) || attempt.payload.Phase != "release-1.0" || attempt.payload.Succession == nil && attempt.payload.Recovery == nil {
		return attempt, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validCanonicalHashHex(processSessionId) {
		return nil, errors.New("campaign recovery process session identity is noncanonical")
	}
	if err := validateScenarioCampaignRecoveryOwner(attempt.cfg, attempt.stateDir, attempt.payload.PlanHash, journal); err != nil {
		return nil, err
	}
	var next *scenarioCampaignAttempt
	err := withScenarioCampaignAttemptLock(attempt.stateDir, attempt.payload.Phase, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := readScenarioCampaignAttempt(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, attempt.payload.Phase)
		if err != nil {
			return err
		}
		if current.payload.RunID != attempt.payload.RunID {
			// Concurrent retry callers may observe the one successor just written
			// by the journal owner. Never advance it again or adopt another lineage.
			if current.payload.AcceptanceBoundary != nil {
				return errors.New("campaign process recovery changed to another acceptance interval")
			}
			if scenarioCampaignAttemptNeedsRecovery(current) {
				return errors.New("campaign process recovery changed to another terminal interval")
			}
			if err := validateScenarioCampaignRecoveryAncestor(current, attempt.payload.RunID); err != nil {
				return err
			}
			next = current
			return nil
		}
		if current.payload.AcceptanceBoundary == nil {
			return errors.New("campaign process recovery lost its signed acceptance boundary")
		}
		if current.payload.AcceptanceInvalidation == "" && strings.EqualFold(current.payload.AcceptanceBoundary.ProcessSessionID, processSessionId) {
			return errors.New("scenario campaign acceptance was interrupted (attempt-reentered-after-acceptance); the current process session cannot replace its own interval")
		}
		runDir := filepath.Join(current.stateDir, "runs", current.payload.RunID)
		if err := requireScenarioCampaignRecoveryAbsent(filepath.Join(runDir, "complete.json"), "campaign recovery cannot replace a completed release"); err != nil {
			return err
		}
		if err := requireScenarioCampaignRecoveryAbsent(scenarioCampaignAttemptPath(current.stateDir, "production-soak"), "campaign recovery cannot replace a release with a production descendant"); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(runDir, "result.json")); err == nil {
			var terminal ScenarioResult
			if err := decodeStrictJSONFile(filepath.Join(runDir, "result.json"), &terminal); err != nil {
				return fmt.Errorf("campaign process recovery terminal result: %w", err)
			}
			if terminal.Result != "fail" {
				return errors.New("campaign process recovery cannot replace a successful unsigned result")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if current.payload.AcceptanceInvalidation == "" {
			current.payload.AcceptanceInvalidation = "process-session-changed"
			current.payload.AcceptanceInvalidatedAt = now.UTC().Format(time.RFC3339Nano)
			if err := writeScenarioCampaignAttempt(current); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		next, err = createScenarioCampaignRecovery(current.cfg, current.stateDir, current.roles, current.payload.PlanHash, now.Add(time.Nanosecond), journal)
		if err != nil {
			return fmt.Errorf("recover interrupted campaign process session: %w", err)
		}
		fmt.Fprintf(os.Stderr, "sim-testnet: provisional campaign process recovery; prior_run=%s next_run=%s deployment_retained=true acceptance_window_reset=true final_acceptance=false\n", current.payload.RunID, next.payload.RunID)
		return nil
	})
	return next, err
}
