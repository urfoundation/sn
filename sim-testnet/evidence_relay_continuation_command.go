//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"path/filepath"
)

func validateEvidenceRelayContinuationOptions(command string, o cliOptions) error {
	used := o.RelayContinuationPlan != "" || o.RelayEndBlock != 0 || o.RelaySlots != 0 || o.RelaySourceLimitMultiplier != 0 || o.ProvisionalCapture
	if command != "relay-continuation" {
		if used {
			return errors.New("relay continuation options require relay-continuation")
		}
		return nil
	}
	if err := validateProvisionalRelayCaptureOptions(command, o); err != nil {
		return err
	}
	if o.ProvisionalResume || o.Detach || o.Name != "" || o.Manifest != "" {
		return errors.New("relay continuation requires strict stopped operation and cannot start a topology")
	}
	if o.RelaySlots != 0 && o.RelaySlots != evidenceRelayContinuationExpandedSlots {
		return errors.New("relay funding revision requires exactly --relay-slots 2048")
	}
	if o.RelaySourceLimitMultiplier != 0 && o.RelaySourceLimitMultiplier != 2 {
		return errors.New("relay source lifetime revision requires exactly --relay-source-limit-multiplier 2")
	}
	if o.RelayContinuationPlan != "" {
		if !filepath.IsAbs(o.RelayContinuationPlan) || filepath.Clean(o.RelayContinuationPlan) != o.RelayContinuationPlan || o.RelayEndBlock != 0 || o.RelaySlots != 0 || o.RelaySourceLimitMultiplier != 0 {
			return errors.New("imported relay continuation requires one canonical absolute plan and no replacement end")
		}
	} else if o.RelayEndBlock == 0 || o.Apply {
		return errors.New("relay continuation capture requires --relay-end-block; apply requires --relay-continuation-plan")
	}
	if o.Apply && !validCanonicalHashHex(o.PlanHash) {
		return errors.New("relay continuation apply requires the exact approved --plan-hash")
	}
	return nil
}

func runEvidenceRelayContinuation(ctx context.Context, cfg *ResolvedConfig, stateDir string, o cliOptions) error {
	if err := validateEvidenceRelayContinuationOptions("relay-continuation", o); err != nil {
		return err
	}
	commandConfig := cfg
	var base *SetupPlan
	var err error
	if o.ProvisionalCapture {
		cfg, base, err = prepareProvisionalRelayCapture(ctx, cfg, stateDir, o)
	} else {
		cfg, base, err = prepareStrictRelayCapture(cfg, stateDir)
	}
	if err != nil {
		return err
	}
	var plan *SetupPlan
	if o.RelayContinuationPlan == "" {
		plan, err = captureEvidenceRelayContinuationWithLimitsAt(ctx, cfg, stateDir, base, o.RelayEndBlock, o.RelaySlots, o.RelaySourceLimitMultiplier, nil)
	} else {
		raw, readErr := readSetupPlanFileBytes(o.RelayContinuationPlan)
		if readErr != nil {
			return readErr
		}
		plan, err = decodePersistedPlanBytes(raw)
		if err == nil && plan.EvidenceRelayContinuation == nil {
			err = errors.New("approved plan contains no relay continuation")
		}
	}
	if err != nil {
		return err
	}
	if !o.Apply {
		if o.ProvisionalCapture {
			if _, err := archiveReviewedSetupPlan(stateDir, plan); err != nil {
				return err
			}
		}
		return printResult(o.Format, plan, nil)
	}
	if err := requireApproved(true, o.PlanHash, plan.PlanHash); err != nil {
		return err
	}
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return err
	}
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return err
		}
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		return err
	}
	defer journal.Close()
	if base.PlanHash == plan.PlanHash {
		if err := validateEvidenceRelayContinuationSource(stateDir, base, journal.Entries()); err != nil {
			return err
		}
		if base.EvidenceRelayContinuation.ActiveGeneration != nil {
			if _, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, base); err != nil {
				return err
			}
		}
		return printResult(o.Format, map[string]any{"command": "relay-continuation", "plan_hash": plan.PlanHash, "end_block": plan.EvidenceRelayContinuation.EndBlock, "status": "already_adopted"}, nil)
	}
	if base.PlanHash != plan.EvidenceRelayContinuation.SourcePlanHash {
		return errors.New("relay continuation source approval changed before apply")
	}
	// Repeat the complete stopped-source, signature, public payload and nonce
	// census at the original approved anchors. A fresh observation may consume
	// time but cannot substitute another plan, subject or end for the approval.
	fresh, err := captureEvidenceRelayContinuationAt(ctx, cfg, stateDir, base, plan.EvidenceRelayContinuation.EndBlock, plan.EvidenceRelayContinuation)
	if err != nil {
		return err
	}
	if fresh.PlanHash != plan.PlanHash {
		return errors.New("relay continuation source bytes, liabilities or lifetime counters changed after approval")
	}
	roles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := installEvidenceRelayGenerationRuntime(ctx, plan); err != nil {
		return err
	}
	if err := writeRunInputs(commandConfig, stateDir, plan, roles); err != nil {
		return err
	}
	return printResult(o.Format, map[string]any{"command": "relay-continuation", "plan_hash": plan.PlanHash, "end_block": plan.EvidenceRelayContinuation.EndBlock, "status": "adopted", "chain_transactions": 0}, nil)
}
