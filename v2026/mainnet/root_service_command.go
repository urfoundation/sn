// Public root-service execution observes the approved basket and recovers old
// signed liabilities. Fresh activation is a closed concrete capability gate.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/urfoundation/sn/v2026/diagnostics"
)

var errRootServiceRuntimeClose = errors.New("root runtime owner close did not acknowledge")

// A scalar status never exports custody packets or implies live activation.
type rootServiceRuntimeResult struct {
	Schema                   string                `json:"schema"`
	RuntimeSha256            string                `json:"runtime_sha256"`
	RootPlanHash             string                `json:"root_plan_hash"`
	ServiceConfigHash        string                `json:"service_config_hash"`
	SubmissionConfigHash     string                `json:"submission_config_hash"`
	Status                   string                `json:"status"`
	ServicePhase             string                `json:"service_phase,omitempty"`
	ActionPhase              string                `json:"action_phase,omitempty"`
	Observations             uint32                `json:"observations"`
	ObservationPending       bool                  `json:"observation_pending"`
	RecoveredSignature       bool                  `json:"recovered_signature"`
	Broadcasts               uint8                 `json:"broadcast_attempts"`
	SubmissionAttempts       int                   `json:"retained_submission_attempts"`
	ExtrinsicHash            string                `json:"extrinsic_hash,omitempty"`
	SignatureStatus          string                `json:"signature_status"`
	CurrentAuthorityVerified bool                  `json:"current_authority_verified"`
	NativeSigning            bool                  `json:"native_signing"`
	NetworkEffects           bool                  `json:"network_effects"`
	ActivationReady          bool                  `json:"activation_ready"`
	ActivationBlockers       []string              `json:"activation_blockers"`
	Failed                   bool                  `json:"failed"`
	RecoveryApprovalHash     string                `json:"recovery_approval_hash,omitempty"`
	RecoveryOpenAttempts     uint32                `json:"recovery_open_attempts,omitempty"`
	Diagnostics              *diagnostics.Snapshot `json:"run_diagnostics,omitempty"`
}

// No config, environment flag or retained approval can instantiate a missing
// authority or signing device. Current reads remain necessary but insufficient.
func rootServiceRuntimeStatus(preparation rootServiceRuntimePreparation) rootServiceRuntimeResult {
	return rootServiceRuntimeResult{Schema: "urnetwork-mainnet-root-service-runtime-result-v1", RuntimeSha256: preparation.Input.Sha256,
		RootPlanHash: preparation.Root.ContentHash, ServiceConfigHash: rootObjectHash(preparation.Root.Service), SubmissionConfigHash: rootObjectHash(preparation.Submission),
		Status: "planned", SignatureStatus: "unresolved", ActivationBlockers: []string{
			"ROOT_CURRENT_EFFECTIVE_ELIGIBILITY_AUTHORITY_UNAVAILABLE", "ROOT_SIGNING_DEVICE_AND_GLOBAL_HOTKEY_NONCE_FENCE_UNAVAILABLE",
			"ROOT_PENDING_SEAT_EXCLUSION_UNAVAILABLE", "ROOT_ENFORCEABLE_FEE_EXPOSURE_UNAVAILABLE", "ROOT_LIVE_ROUTE_AND_RUNTIME_QUALIFICATION_REQUIRED",
		}}
}

// Every mutation requires exact acceptance of the independently provisioned
// runtime bytes. Planning and blocked activation touch no retained ownership.
func runRootServiceCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (code int) {
	if ctx == nil || len(args) == 0 {
		fmt.Fprintln(stderr, "root-service requires plan|prepare|status|run|activate|prepare-recovery|run-recovery and a context")
		return 2
	}
	operation := args[0]
	recovery := operation == "prepare-recovery" || operation == "run-recovery"
	if operation != "plan" && operation != "prepare" && operation != "status" && operation != "run" && operation != "activate" && !recovery {
		fmt.Fprintln(stderr, "root-service requires plan|prepare|status|run|activate|prepare-recovery|run-recovery")
		return 2
	}
	flags := flag.NewFlagSet("root-service "+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "canonical private root runtime config with exact independent input pins")
	accepted := flags.String("accept-runtime-sha256", "", "sha256 of the complete runtime config bytes")
	var recoveryPath, recoveryHash string
	if recovery {
		flags.StringVar(&recoveryPath, "recovery-approval", "", "separate independently signed bounded recovery approval")
		flags.StringVar(&recoveryHash, "recovery-approval-sha256", "", "exact sha256 of the recovery approval bytes")
	}
	maximumSteps, interval := uint(1), 30*time.Second
	if operation == "run" {
		flags.UintVar(&maximumSteps, "maximum-steps", 1, "finite 1..10000 observation/reconciliation steps")
		flags.DurationVar(&interval, "interval", 30*time.Second, "joined 1s..1h step cadence")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" ||
		operation == "plan" && *accepted != "" || operation != "plan" && !planSha256(*accepted) ||
		maximumSteps == 0 || maximumSteps > 10000 || interval < time.Second || interval > time.Hour {
		fmt.Fprintln(stderr, "root-service needs --config; prepare/status/run/activate also need --accept-runtime-sha256; run accepts only bounded steps and cadence")
		return 2
	}
	if recovery && (!bootstrapRootAbsolutePath(recoveryPath) || !planSha256(recoveryHash)) {
		fmt.Fprintln(stderr, "root-service recovery requires --recovery-approval and --recovery-approval-sha256")
		return 2
	}
	preparation, err := loadRootServiceRuntime(ctx, *path)
	if err != nil {
		fmt.Fprintln(stderr, "root-service independent inputs:", err)
		return 2
	}
	if operation != "plan" && *accepted != preparation.Input.Sha256 {
		fmt.Fprintln(stderr, "root-service accepted runtime bytes differ; no journal or route opened")
		return 3
	}
	result := rootServiceRuntimeStatus(preparation)
	if operation == "plan" || operation == "activate" {
		if operation == "activate" {
			result.Status = "activation-blocked"
		}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintln(stderr, "root-service plan/gate output:", err)
			return 1
		}
		if operation == "activate" {
			return 3
		}
		return 0
	}
	if recovery {
		approval, err := loadRootServiceRecovery(ctx, planFileReference{Path: recoveryPath, Sha256: recoveryHash}, preparation, time.Now())
		if err != nil {
			fmt.Fprintln(stderr, "root-service recovery independent approval:", err)
			return 3
		}
		if operation == "prepare-recovery" {
			err = prepareRootServiceRecovery(ctx, preparation, approval, time.Now())
			result.Status, result.RecoveryApprovalHash = "recovery-prepared", rootObjectHash(approval)
		} else {
			result, err = runRootServiceRecovery(ctx, preparation, approval, stdout, rootServiceRecoveryPorts{open: openRootServiceRuntime, now: time.Now, wait: waitRootService})
		}
		return finishRootServiceRuntime(ctx, result, err, stdout, stderr)
	}
	runtime, err := openRootServiceRuntime(ctx, preparation, operation == "prepare")
	if err != nil {
		fmt.Fprintln(stderr, "root-service original ownership unavailable; retain all journals:", err)
		return 3
	}
	if operation == "run" {
		return runRootServiceRuntime(ctx, runtime, result, uint32(maximumSteps), interval, stdout, stderr)
	}
	defer func() {
		if err := runtime.close(); err != nil {
			fmt.Fprintln(stderr, "root-service close:", err)
			code = 1
		}
	}()
	result.Status = "retained"
	if operation == "prepare" {
		result.Status = "submission-journal-prepared"
	}
	if err := runtime.status(&result); err != nil {
		fmt.Fprintln(stderr, "root-service retained state:", err)
		return 3
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "root-service retained state output:", err)
		return 1
	}
	return 0
}

// Called only outside Run while this command holds all three process locks.
// A valid signature in any journal is reported even before recovery copies it.
func (self *rootServiceRuntime) status(result *rootServiceRuntimeResult) error {
	record, err := self.serviceStore.load()
	if err != nil {
		return err
	}
	submission, err := self.submissionStore.load()
	if err != nil {
		return err
	}
	signature, broadcasts, err := self.issuedSignature()
	if err != nil {
		return err
	}
	result.ServicePhase, result.ActionPhase = record.Phase, record.Action.Phase
	result.Observations, result.ObservationPending = record.Observations, record.ObservationPending
	result.RecoveredSignature, result.Broadcasts, result.SubmissionAttempts = record.RecoveredSignature, broadcasts, len(submission.Attempts)
	if len(signature) != 0 {
		raw, err := record.Action.Action.signed(signature)
		if err != nil {
			return err
		}
		result.SignatureStatus, result.ExtrinsicHash = "issued", rootExtrinsicHash(raw)
	}
	return nil
}

// The public run consumes and closes every owner, including on recovery/output
// admission failure. Final diagnostics are separately bounded after Run joins;
// aliased destinations cannot acquire concurrent exporter workers.
func runRootServiceRuntime(ctx context.Context, runtime *rootServiceRuntime, result rootServiceRuntimeResult, steps uint32, interval time.Duration, stdout, stderr io.Writer) int {
	result, err := executeRootServiceRuntime(ctx, runtime, result, steps, interval, stdout)
	return finishRootServiceRuntime(ctx, result, err, stdout, stderr)
}

// The typed operation result remains available to the separately approved
// controller. Every original owner closes before a caller can decide to reopen.
func executeRootServiceRuntime(ctx context.Context, runtime *rootServiceRuntime, result rootServiceRuntimeResult, steps uint32, interval time.Duration, stdout io.Writer) (rootServiceRuntimeResult, error) {
	err := runtime.recoverIssued(ctx)
	if err == nil {
		var output *rootServiceOutput
		output, err = newRootServiceOutput(ctx, stdout)
		if err == nil {
			err = runtime.service.Run(ctx, steps, interval, output)
			snapshot := output.snapshot()
			result.Diagnostics = &snapshot
		}
	}
	err = errors.Join(err, runtime.status(&result))
	if closeErr := runtime.close(); closeErr != nil {
		err = errors.Join(err, errRootServiceRuntimeClose, closeErr)
	}
	result.Status, result.Failed = "bounded-run-finished", err != nil
	if err != nil {
		result.Status = "blocked-or-unresolved"
	}
	if result.ServicePhase == "complete" {
		result.Status = "liability-reconciled"
		if result.ActionPhase != "finalized" && result.ActionPhase != "expired" {
			result.Failed = true
		}
	}
	return result, err
}

// Output failure cannot trigger owner recovery or replenish its allowance.
func finishRootServiceRuntime(ctx context.Context, result rootServiceRuntimeResult, err error, stdout, stderr io.Writer) int {
	if err != nil {
		result.Failed = true
		result.Status = "blocked-or-unresolved"
	}
	output, outputErr := newMonitorOutput(ctx, stdout, stderr, 0, time.Now)
	if outputErr != nil {
		return 1
	}
	encodeErr := json.NewEncoder(output.events.Writer("chain")).Encode(result)
	if result.Failed {
		fmt.Fprintln(output.errors.Writer("diagnostic"), "root-service stopped; original liability and allowance journals retained; fresh signing/submission capabilities remain unavailable")
	}
	if errors.Join(encodeErr, output.close()) != nil {
		return 1
	}
	if result.Failed {
		return 3
	}
	return 0
}
