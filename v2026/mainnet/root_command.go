// A bounded root observer reuses finalized continuity and durable checkpoints.
// It has no signer, transaction constructor or submission capability.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const rootEventSchema = "urnetwork-mainnet-root-monitor-event-v1"
const rootDiagnosticEventSchema = "urnetwork-mainnet-root-monitor-event-v2"

// Failed samples never retain a prior ready observation as their current state.
type rootMonitorEvent struct {
	Schema      string                        `json:"schema"`
	Sample      int                           `json:"sample"`
	ObservedAt  string                        `json:"observed_at"`
	Status      string                        `json:"status"`
	Detail      string                        `json:"detail,omitempty"`
	Snapshot    *rootPreviewEnvelope          `json:"snapshot,omitempty"`
	Observation *rootMonitorObservation       `json:"observation,omitempty"`
	Publication string                        `json:"publication,omitempty"`
	Diagnostics *monitorDiagnosticObservation `json:"diagnostics,omitempty"`
	ReadPhase   string                        `json:"read_phase,omitempty"`
	ReadCause   string                        `json:"read_cause,omitempty"`
}

// Read-only readiness is a narrow observation result, never activation approval.
func runRootCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runRootCommandWithMonitorHooks(ctx, args, stdout, stderr, time.Now, monitorServiceHooks{})
}

// Long-lived diagnostics have a separate owner even before flag admission.
// Finite preview output retains its original complete snapshot and I/O contract.
func runRootCommandWithMonitorHooks(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) (result int) {
	return runRootCommandWithPolicy(ctx, args, stdout, stderr, now, hooks, nil, "", nil)
}

// A signed passive-service config supplies the already authenticated policy in
// memory, so an intervening pathname edit cannot replace the approved bytes.
func runRootCommandWithPolicy(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks, approvedPolicy *rootValidatorPolicy, approvedHash string, preparation *rootPassivePreparation) (result int) {
	if ctx == nil || len(args) == 0 || args[0] != "root-monitor" && args[0] != "root-preview" {
		return 2
	}
	if ctx.Err() != nil {
		return 0
	}
	command := args[0]
	monitoring := command == "root-monitor"
	if monitoring {
		output, err := newMonitorOutput(ctx, stdout, stderr, 0, now)
		if err != nil {
			return 3
		}
		defer func() {
			if output.close() != nil {
				result = 3
			}
		}()
		stdout, stderr = output.events.Writer("chain"), output.errors.Writer("diagnostic")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	policyPath := flags.String("policy", "", "independently approved root observer policy JSON")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "one bounded complete sample window, at least 60 seconds")
	interval := flags.Duration("interval", 30*time.Second, "delay after each completed root monitor sample")
	samples := flags.Int("samples", 1, "finite monitor sample count, 1 through 10000")
	stallAfter := flags.Duration("stall-after", 5*time.Minute, "finalized progress alert threshold")
	checkpointPath := flags.String("checkpoint", "", "absolute durable finalized checkpoint path for root-monitor")
	metricsPath := flags.String("metrics-file", "", "optional absolute .prom output for root-monitor")
	metricsRole := flags.String("metrics-role", "", "independent bounded role label, required with metrics-file")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" || *policyPath == "" || *retryWindow < 60*time.Second || *retryWindow > 15*time.Minute || *samples < 1 || *samples > 10000 || *interval <= 0 || *stallAfter <= 0 {
		fmt.Fprintln(stderr, "root command requires --rpc URL --policy FILE; retry-window must be 60s..15m, samples 1..10000 and intervals positive")
		return 2
	}
	if command == "root-preview" && (*samples != 1 || *checkpointPath != "" || *metricsPath != "" || *metricsRole != "") {
		fmt.Fprintln(stderr, "root-preview takes one sample and no checkpoint or metrics")
		return 2
	}
	if (*metricsPath == "") != (*metricsRole == "") || *metricsRole != "" && !monitorRolePattern.MatchString(*metricsRole) {
		fmt.Fprintln(stderr, "root-monitor metrics require a file and independent bounded role label")
		return 2
	}
	if *metricsPath != "" && (!filepath.IsAbs(*metricsPath) || filepath.Clean(*metricsPath) != *metricsPath || !strings.HasSuffix(*metricsPath, ".prom")) {
		fmt.Fprintln(stderr, "root-monitor metrics require an absolute canonical .prom path")
		return 2
	}
	if *checkpointPath != "" || *metricsPath != "" {
		if err := durablepath.Require(ctx); err != nil {
			fmt.Fprintln(stderr, "root observer durable custody:", err)
			return 2
		}
	}
	// Preparation observation shares the complete sample deadline with Rpc.
	// A finite retry count also bounds deterministic/accelerated wait hooks.
	// No retry replaces the retained preparation or checkpoint owner.
	var sampleCtx context.Context
	sampleCancel := func() {}
	observationRetries := 0
	renewSample := func() {
		sampleCancel()
		current, cancel := context.WithTimeout(ctx, *retryWindow)
		sampleCtx, sampleCancel = current, cancel
		observationRetries = 0
	}
	defer func() { sampleCancel() }()
	renewSample()
	startupDeadline := now().Add(*retryWindow)
	checkPreparation := func() error {
		if preparation == nil {
			return ctx.Err()
		}
		backoff := time.Second
		for {
			if err := sampleCtx.Err(); err != nil {
				return err
			}
			err := preparation.check()
			if err == nil || !errors.Is(err, durablevolume.ErrUnavailable) || !rootMonitorStartupPending(err) {
				return err
			}
			if observationRetries == 64 {
				return mainnetDurableUnavailable("passive preparation observation budget exhausted", nil)
			}
			observationRetries++
			fmt.Fprintln(stderr, "passive preparation observation unavailable; retrying retained custody")
			if !waitMonitorService(sampleCtx, "root", backoff, hooks) {
				return errors.Join(durablevolume.ErrUnavailable, sampleCtx.Err())
			}
			backoff = min(2*backoff, 30*time.Second)
		}
	}
	preparationExit := func(err error) int {
		if !rootMonitorStartupPending(err) {
			fmt.Fprintln(stderr, "passive preparation retained custody is invalid")
			return 3
		}
		if ctx.Err() != nil {
			return 0
		}
		if errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintln(stderr, "passive preparation observation remains unavailable")
			return 1
		}
		fmt.Fprintln(stderr, "passive preparation retained custody is invalid")
		return 3
	}
	if err := checkPreparation(); err != nil {
		return preparationExit(err)
	}
	var policy rootValidatorPolicy
	policyHash, err := "", error(nil)
	if approvedPolicy == nil {
		if monitoring {
			policy, policyHash, err = readRootMonitorStartupPolicy(sampleCtx, *policyPath, startupDeadline, now, hooks, stderr)
		} else {
			policyHash, err = readEconomicInput(*policyPath, &policy)
		}
	} else {
		policy, policyHash = copyRootPassivePolicy(*approvedPolicy), approvedHash
		if !planSha256(policyHash) || policyHash != rootObjectHash(policy) || policy.Schema != rootPassivePolicySchema {
			err = errors.New("passive service policy identity changed")
		}
	}
	if err == nil {
		err = policy.validate()
	}
	if err != nil {
		fmt.Fprintln(stderr, rootCommandErrorDetail(err, "root policy unavailable or invalid", monitoring))
		if monitoring {
			return rootMonitorStartupExit(ctx, err, 2)
		}
		return 2
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, rootCommandErrorDetail(err, "root RPC configuration invalid", monitoring))
		return 2
	}
	defer client.httpClient.CloseIdleConnections()
	state := &monitorState{}
	var checkpoint *monitorCheckpointStore
	if *checkpointPath != "" {
		err = retryRootMonitorStartup(sampleCtx, startupDeadline, now, hooks, stderr, func() error {
			var openErr error
			checkpoint, openErr = openMonitorCheckpoint(*checkpointPath, identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}, ctx)
			return openErr
		})
		if err != nil {
			fmt.Fprintln(stderr, rootCommandErrorDetail(err, "root checkpoint admission failed", monitoring))
			if checkpoint != nil {
				err = monitorAdmissionFailure(err, checkpoint.close())
			}
			return rootMonitorStartupExit(ctx, err, 3)
		}
		defer func() {
			file := checkpoint.lock
			closeErr := checkpoint.close()
			if hooks.afterClose != nil {
				closeErr = errors.Join(closeErr, hooks.afterClose("root", "checkpoint", file))
			}
			if closeErr != nil {
				fmt.Fprintln(stderr, "root checkpoint cleanup failed")
				result = 3
			}
		}()
		if hooks.afterCheckpointOpen != nil {
			hooks.afterCheckpointOpen(ctx, "root", checkpoint.lock)
		}
		if hooks.syncDirectory != nil {
			checkpoint.syncDirectory = func(file *os.File) error { return hooks.syncDirectory("root", "checkpoint", file) }
		}
		err = retryRootMonitorStartup(sampleCtx, startupDeadline, now, hooks, stderr, func() error {
			var loadErr error
			state, loadErr = checkpoint.load()
			return loadErr
		})
		if err != nil {
			fmt.Fprintln(stderr, rootCommandErrorDetail(err, "root retained checkpoint invalid", monitoring))
			return rootMonitorStartupExit(ctx, err, 3)
		}
	}
	publication := &rootMonitorPublication{outcome: "unconfigured"}
	if *metricsPath != "" {
		publication.admit = func() (*monitorMetricsStore, error) {
			store, openErr := openMonitorMetrics(*metricsPath, ctx)
			if store != nil && hooks.syncDirectory != nil {
				store.syncDirectory = func(file *os.File) error { return hooks.syncDirectory("root", "metrics", file) }
			}
			return store, openErr
		}
		// A soft initial outage must not disable publication for the remaining
		// finite run. Later actual samples retry only this optional admission.
		publication.store, err = publication.admit()
		if err != nil {
			fmt.Fprintln(stderr, "root metrics admission failed")
			publication.outcome, publication.disabled = "retrying", !rootMonitorStartupPending(err)
			if publication.disabled {
				publication.outcome = "unavailable"
			}
		}
		defer func() {
			if publication.store == nil {
				return
			}
			file := publication.store.lock
			closeErr := publication.store.close()
			if hooks.afterClose != nil {
				closeErr = errors.Join(closeErr, hooks.afterClose("root", "metrics", file))
			}
			if closeErr != nil {
				fmt.Fprintln(stderr, "root metrics cleanup failed")
				result = 3
			}
		}()
	}
	if publication.store != nil {
		publication.outcome = "starting"
		// Existing output retains its original age until a new sample completes.
		if _, statErr := os.Lstat(*metricsPath); errors.Is(statErr, os.ErrNotExist) {
			publication.publish(rootMonitorEvent{Status: "starting", Diagnostics: monitorDiagnosticSnapshot(stdout, stderr)}, state, *metricsRole, time.Time{})
		} else if statErr != nil {
			fmt.Fprintln(stderr, "root metrics file unavailable")
			publication.outcome = "retrying"
		}
	}
	encoder := json.NewEncoder(stdout)
	// Exhausting one preparation read is a failed sample, not a new custody
	// owner or permission to discard the remaining signed sample allowance.
	// This path changes no checkpoint and never publishes a ready observation.
	finishUnavailableSample := func(sample int, err error, priorFatalExit int) (bool, int) {
		exit := preparationExit(err)
		if exit != 1 {
			return false, exit
		}
		// Missing current preparation facts cannot erase a separately observed
		// integrity failure or uncertainty from an actual checkpoint write.
		if priorFatalExit != 0 {
			return false, priorFatalExit
		}
		sampledAt := now().UTC()
		event := rootMonitorEvent{Schema: rootEventSchema, Sample: sample,
			ObservedAt: sampledAt.Format(time.RFC3339Nano), Status: "storage-unavailable",
			Detail: "passive preparation observation remains unavailable", ReadPhase: "preparation", ReadCause: "unavailable"}
		if errors.Is(err, context.DeadlineExceeded) {
			event.ReadCause = "timeout"
		}
		if monitoring {
			event.Schema = rootDiagnosticEventSchema
			event.Diagnostics = monitorDiagnosticSnapshot(stdout, stderr)
			publication.publish(event, state, *metricsRole, sampledAt)
			event.Publication = publication.outcome
		}
		if err := encoder.Encode(event); err != nil {
			fmt.Fprintln(stderr, rootCommandErrorDetail(err, "root event encoding failed", monitoring))
			return false, 1
		}
		if monitoring && hooks.afterEvent != nil {
			hooks.afterEvent(ctx, "root")
		}
		if ctx.Err() != nil {
			return false, 0
		}
		if sample == *samples {
			return false, 1
		}
		sampleCancel()
		if !waitMonitorService(ctx, "root", *interval, hooks) {
			return false, 0
		}
		return true, 1
	}
	lastExit := 0
samplesLoop:
	for sample := 1; sample <= *samples; sample++ {
		if ctx.Err() != nil {
			return 0
		}
		renewSample()
		if err := checkPreparation(); err != nil {
			resume, exit := finishUnavailableSample(sample, err, 0)
			if resume {
				lastExit = exit
				continue samplesLoop
			}
			return exit
		}
		event := rootMonitorEvent{Schema: rootEventSchema, Sample: sample}
		preview, readErr := client.readRootPreview(sampleCtx, policy, policyHash)
		if err := checkPreparation(); err != nil {
			priorFatalExit := 0
			if errors.Is(readErr, errRpcIntegrity) {
				priorFatalExit = 3
			}
			resume, exit := finishUnavailableSample(sample, err, priorFatalExit)
			if resume {
				lastExit = exit
				continue samplesLoop
			}
			return exit
		}
		sampledAt := now().UTC()
		event.ObservedAt = sampledAt.Format(time.RFC3339Nano)
		lastExit = 0
		if readErr != nil {
			if ctx.Err() != nil {
				return 0
			}
			event.Status, event.Detail = "rpc-error", rootCommandErrorDetail(readErr, "root sample unavailable", monitoring)
			if monitoring {
				event.ReadPhase, event.ReadCause = "sample", rootMonitorReadCause(readErr)
			}
			lastExit = 1
			if errors.Is(readErr, errRpcIntegrity) {
				event.Status = "rpc-integrity"
				lastExit = 3
			}
		} else {
			// Continuity has the same bounded budget as existing mainnet monitor
			// reads and rechecks the retained height, not the previous tip.
			continuous, continuityErr := client.priorFinalizedMatches(sampleCtx, state, preview.Identity)
			if continuityErr != nil {
				event.Status, event.Detail = "rpc-error", rootCommandErrorDetail(continuityErr, "root continuity read unavailable", monitoring)
				if monitoring {
					event.ReadPhase, event.ReadCause = "continuity", rootMonitorReadCause(continuityErr)
				}
				lastExit = 1
				if errors.Is(continuityErr, errRpcIntegrity) {
					event.Status = "rpc-integrity"
					lastExit = 3
				}
			} else if !continuous {
				event.Status, event.Detail = "finality-conflict", "previous finalized block changed at its retained height"
				lastExit = 3
			} else {
				previousHash, previousNumber := state.lastHash, state.lastNumber
				if err := checkPreparation(); err != nil {
					resume, exit := finishUnavailableSample(sample, err, 0)
					if resume {
						lastExit = exit
						continue samplesLoop
					}
					return exit
				}
				status, observeErr := state.observe(sampledAt, preview.Identity, *stallAfter)
				if observeErr != nil {
					event.Status, event.Detail = status, rootCommandErrorDetail(observeErr, "root finalized continuity changed", monitoring)
					lastExit = 3
				} else {
					event.Status = preview.Status
					if !preview.ReadOnlyReady {
						lastExit = 3
					}
					if status == "finality-stalled" {
						event.Status = status
						preview.ReadOnlyReady, preview.Status = false, "blocked"
						preview.Blockers = append(preview.Blockers, "ROOT_FINALITY_STALLED")
						lastExit = 3
					}
					if checkpoint != nil && (state.lastHash != previousHash || state.lastNumber != previousNumber) {
						if saveErr := checkpoint.save(state); saveErr != nil {
							event.Status, event.Detail = "checkpoint-error", rootCommandErrorDetail(saveErr, "root checkpoint write failed", monitoring)
							preview.ReadOnlyReady, preview.Status = false, "blocked"
							preview.Blockers = append(preview.Blockers, "ROOT_FINALITY_CHECKPOINT_FAILED")
							lastExit = 1
						}
					}
					sealed, sealErr := sealRootPreview(preview)
					if sealErr != nil {
						fmt.Fprintln(stderr, rootCommandErrorDetail(sealErr, "root snapshot sealing failed", monitoring))
						return 1
					}
					event.Snapshot = &sealed
				}
			}
		}
		if monitoring && ctx.Err() != nil {
			return 0
		}
		priorFatalExit := 0
		if event.Status == "rpc-integrity" || event.Status == "finality-conflict" || event.Status == "checkpoint-error" {
			priorFatalExit = lastExit
		}
		if err := checkPreparation(); err != nil {
			resume, exit := finishUnavailableSample(sample, err, priorFatalExit)
			if resume {
				lastExit = exit
				continue samplesLoop
			}
			return exit
		}
		if monitoring {
			event.Schema = rootDiagnosticEventSchema
			event.Observation = projectRootMonitorObservation(event.Snapshot)
			event.Snapshot = nil
			event.Diagnostics = monitorDiagnosticSnapshot(stdout, stderr)
			publication.publish(event, state, *metricsRole, sampledAt)
			event.Publication = publication.outcome
		}
		if err := checkPreparation(); err != nil {
			resume, exit := finishUnavailableSample(sample, err, priorFatalExit)
			if resume {
				lastExit = exit
				continue samplesLoop
			}
			return exit
		}
		if err := encoder.Encode(event); err != nil {
			fmt.Fprintln(stderr, rootCommandErrorDetail(err, "root event encoding failed", monitoring))
			return 1
		}
		if monitoring && hooks.afterEvent != nil {
			hooks.afterEvent(ctx, "root")
		}
		if event.Status == "rpc-integrity" || event.Status == "finality-conflict" || event.Status == "checkpoint-error" {
			return lastExit
		}
		if sample == *samples {
			return lastExit
		}
		sampleCancel()
		if !waitMonitorService(ctx, "root", *interval, hooks) {
			return 0
		}
	}
	return lastExit
}

// Daemon logs use fixed causes; finite previews retain their operator details.
func rootCommandErrorDetail(err error, code string, monitoring bool) string {
	if monitoring {
		return code
	}
	return err.Error()
}

// Preserve available typed facts without parsing strings or changing retries.
func rootMonitorReadCause(err error) string {
	if errors.Is(err, errRpcIntegrity) {
		return "integrity"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var call *rpcCallError
	if errors.As(err, &call) {
		return "unavailable"
	}
	var transport *url.Error
	if errors.As(err, &transport) {
		return "transport"
	}
	return "unknown"
}
