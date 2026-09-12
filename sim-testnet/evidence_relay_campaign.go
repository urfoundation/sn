//go:build linux || darwin

// Evidence relay lifetime is inside the campaign lifetime. Terminal closure
// waiting also waits for all accepted source slots before the final collector
// can publish a passing result; teardown joins before keeper resources close.
package main

import (
	"context"
	"errors"
	"time"
)

// Other focused scenarios retain their ordinary execution path. Full release
// phases cannot omit the actual funded worker through an optional test toggle.
func runScenarioWithEvidenceRelay(ctx context.Context, cfg *ResolvedConfig, stateDir string, definition scenarioDefinition, probe scenarioProbe, options scenarioRunOptions, executor *Executor) (result *ScenarioResult, resultErr error) {
	if definition.Name != "release-1.0" && definition.Name != "production-soak" {
		return runScenarioWithProbe(ctx, cfg, stateDir, definition, probe, options)
	}
	if ctx == nil {
		return nil, errors.New("evidence relay campaign context is absent")
	}
	if err := validateRuntimeEvidenceSourceCapacity(cfg); err != nil {
		return nil, err
	}
	phaseCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	prepared := options.Attempt != nil && options.Attempt.payload.PreparationComplete
	relay, err := newEvidenceRelayRuntime(phaseCtx, cfg, executor, definition.Name, prepared, func(err error) { cancel(err) })
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, relay.Close(), context.Cause(phaseCtx)) }()
	if err := relay.WaitReady(phaseCtx); err != nil {
		return nil, err
	}
	if scenarioNeedsNativeWarmupV2(cfg, definition.Name) {
		// Production always installs the concrete actual-chain owner here; a
		// caller cannot turn a ready boolean into a native source receipt.
		options.NativeWarmupV2 = liveScenarioNativeWarmupV2(cfg, executor, relay.chain, definition.Name)
		options.NativeWarmupCompleteV2 = relay.RequireNativeWarmup
		if relay.nativeWarmupBudget == nil {
			return nil, errors.New("funded native readiness has no original preparation window")
		}
		budget := *relay.nativeWarmupBudget
		options.NativeWarmupBudgetV2 = &budget
	}
	// Resumed preparation retains its original completion; it cannot buy a
	// new horizon. Fresh preparation rechecks actual clocks before observation.
	if prepared {
		if err := relay.RequirePrepared(phaseCtx); err != nil {
			return nil, err
		}
	} else {
		prepare := options.Prepare
		options.Prepare = func(prepareCtx context.Context) error {
			if prepare != nil {
				if err := prepare(prepareCtx); err != nil {
					return err
				}
			}
			return relay.RequirePrepared(prepareCtx)
		}
	}
	waitClosures := options.WaitFinalSettlementClosures
	if waitClosures == nil {
		waitClosures = waitFinalValidatorSettlementClosures
	}
	options.WaitFinalSettlementClosures = func(waitCtx context.Context, resolved *ResolvedConfig, root string, current *ScenarioObservation, window *ScenarioAcceptanceWindow, deadline time.Time, poll time.Duration) error {
		if err := waitClosures(waitCtx, resolved, root, current, window, deadline, poll); err != nil {
			return err
		}
		if window == nil || window.EpochCount == 0 {
			return errors.New("evidence relay completion has no accepted epoch census")
		}
		end, ok := checkedAdd(window.FirstEpoch, window.EpochCount-1)
		if !ok {
			return errors.New("evidence relay accepted epoch range overflows")
		}
		bounded, cancel := context.WithDeadline(waitCtx, deadline)
		defer cancel()
		if err := relay.WaitThrough(bounded, end); err != nil {
			return err
		}
		if err := relay.WaitAuditPass(bounded); err != nil {
			return err
		}
		// Finish the worker before evidence collection finalizes the happy path.
		// Its already-complete source slots remain independently replayable.
		return relay.Close()
	}
	return runScenarioWithProbe(phaseCtx, cfg, stateDir, definition, probe, options)
}
