//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

// This fixture exercises readiness decisions over separately authenticated
// observation inputs. Real signed source/native HTTP readers have their own
// source-custody and SCALE regression roots; a ready flag grants no such proof.
func scenarioNativeWarmupFixtureV2(t *testing.T) (*ResolvedConfig, *ScenarioObservation, *ScenarioObservation, FinalNativeCheckpointV2) {
	t.Helper()
	cfg, baseline, current := headDecisionHistoryFixture(t)
	cfg.Config.ProvisionValidatorEvidenceV2 = true
	baseline.NativeRewards = &NativeRewardObservation{FinalizedHead: ChainHead{Number: 100, Hash: finalTestHex(0x10)}}
	for index := range current.Validators {
		v := &current.Validators[index]
		v.SelfUID = uint16(index + 1)
		v.NativeSourceScopeV2 = "signed-source-and-canonical-native-receipts"
		v.NativeSourceStoreSHA256 = fmt.Sprintf("sha256:%064x", index+1)
		d := &v.HeadDecisions[1]
		d.MeasurementArtifactHash = fmt.Sprintf("sha256:%064x", index+11)
		d.NativeSnapshot = ChainHead{Number: 105, Hash: finalTestHex(0x11)}
		d.FinalizedBlock, d.FinalizedBlockHash = 111, finalTestHex(0x12)
		d.RevealBlock, d.RevealBlockHash = 150, finalTestHex(0x13)
		d.ApplicationBlock, d.ApplicationBlockHash = 152, finalTestHex(0x14)
		d.CommitNativeEpoch, d.RevealNativeEpoch, d.ApplicationNativeEpoch = 1406, 1407, 1407
		v.NativeCommitsV2 = []FinalNativeCoverageCommitV2{{MeasurementHash: d.MeasurementArtifactHash, Block: ChainHead{Number: 111, Hash: finalTestHex(0x12)}, NativeEpoch: 1406},
			{MeasurementHash: fmt.Sprintf("sha256:%064x", index+21), Block: ChainHead{Number: 155, Hash: finalTestHex(0x15)}, NativeEpoch: 1407}}
	}
	var checkpoint FinalNativeCheckpointV2
	checkpoint.Mapping.Query.NativeNumber = 160
	checkpoint.RevealPeriodEpochs = 1
	checkpoint.Identity.SubnetEpochIndex = 1407
	checkpoint.PayoutHead = ChainHead{Number: 150, Hash: finalTestHex(0x13)}
	checkpoint.PayoutParent = ChainHead{Number: 149, Hash: finalTestHex(0x16)}
	checkpoint.Weights.ValidatorUID, checkpoint.Weights.LastUpdate = 1, 155
	for _, w := range current.Validators[0].HeadDecisions[1].AppliedWeights {
		checkpoint.Weights.UIDs = append(checkpoint.Weights.UIDs, w.UID)
		checkpoint.Weights.Values = append(checkpoint.Weights.Values, w.Value)
	}
	return cfg, baseline, current, checkpoint
}

func TestScenarioNativeWarmupV2RequiresFreshSourcesAppliedRowsAndPayout(t *testing.T) {
	cfg, baseline, current, checkpoint := scenarioNativeWarmupFixtureV2(t)
	decision, detail, err := scenarioNativeWarmupDecisionV2(cfg, baseline, current, current.Validators[0], checkpoint)
	if err != nil || decision == nil || detail != "" {
		t.Fatal("actual fresh source with later commit did not admit readiness", detail, err)
	}
	for _, name := range []string{"old signed history", "different applied row", "stale native epoch", "different latest commit", "payout before reveal", "provisional local counters"} {
		t.Run(name, func(t *testing.T) {
			cfg, baseline, current, point := scenarioNativeWarmupFixtureV2(t)
			switch name {
			case "old signed history":
				current.Validators[0].HeadDecisions[1].NativeSnapshot.Number = 99
			case "different applied row":
				point.Weights.Values = slices.Clone(point.Weights.Values)
				point.Weights.Values[0]++
			case "stale native epoch":
				point.Identity.SubnetEpochIndex += 2
			case "different latest commit":
				point.Weights.LastUpdate--
			case "payout before reveal":
				point.PayoutHead.Number--
			case "provisional local counters":
				current.Validators[0].NativeSourceScopeV2 = "local-runtime-observation"
			}
			got, _, err := scenarioNativeWarmupDecisionV2(cfg, baseline, current, current.Validators[0], point)
			if got != nil || name == "provisional local counters" && err == nil {
				t.Fatal("incomplete readiness was admitted", got, err)
			}
		})
	}
	before := &NativeRewardObservation{FinalizedHead: checkpoint.PayoutParent, TotalHotkeyAlphaRao: []string{"0", "10", "10", "0", "0", "10", "10"}}
	after := &NativeRewardObservation{FinalizedHead: checkpoint.PayoutHead, TotalHotkeyAlphaRao: []string{"0", "12", "10", "0", "0", "10", "10"}, EmissionRao: []string{"0", "2", "0", "0", "0", "0", "0"}, Incentive: []uint16{0, 1, 0, 0, 0, 0, 0}}
	if ready, err := scenarioNativeWarmupPayoutV2(before, after, []*HeadDecisionObservation{decision}); err != nil || !ready {
		t.Fatal("exact positive native parent transition refused", err)
	}
	after.TotalHotkeyAlphaRao[1] = "10"
	if ready, err := scenarioNativeWarmupPayoutV2(before, after, []*HeadDecisionObservation{decision}); err != nil || ready {
		t.Fatal("old emission without current stake growth became payout readiness", err)
	}
	after.FinalizedHead.Number++
	if ready, err := scenarioNativeWarmupPayoutV2(before, after, []*HeadDecisionObservation{decision}); err == nil || ready {
		t.Fatal("broad stake interval became exact native payout", err)
	}
}

func TestScenarioNativeWarmupV2WaitsBeforeChoosingAcceptanceAndPreservesHistory(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.ProvisionValidatorEvidenceV2 = true
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	baseline, first, ready := testScenarioObservation(cfg, 9), testScenarioObservation(cfg, 10), testScenarioObservation(cfg, 12)
	first.ObservationHash, _ = canonicalHashHex(first)
	originalHash := first.ObservationHash
	if _, err := buildScenarioAcceptanceWindow(cfg, definition, first); err == nil {
		t.Fatal("next EVM boundary bypassed native readiness")
	}
	probe := &staticScenarioProbe{observations: []*ScenarioObservation{ready}}
	calls, completed := 0, false
	var retained []*ScenarioObservation
	options := scenarioRunOptions{PollInterval: time.Microsecond, NativeWarmupV2: func(ctx context.Context, start, current *ScenarioObservation) (*ScenarioNativeWarmupV2, error) {
		if start != baseline {
			t.Fatal("warm-up changed the original campaign baseline")
		}
		calls++
		return &ScenarioNativeWarmupV2{Schema: "urnetwork-sim-native-warmup-v2", Phase: "release-1.0", Ready: calls == 2, Detail: "controlled readiness state"}, ctx.Err()
	}, NativeWarmupCompleteV2: func(context.Context) error { completed = true; return nil }}
	options.NativeWarmupBudgetV2, err = newScenarioNativeWarmupBudgetV2(cfg, definition.Name, false, first.Status.Contracts.FinalizedHead, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	current, err := waitScenarioNativeWarmupV2(t.Context(), cfg, definition.Name, baseline, first, probe, options, func(observation *ScenarioObservation) error { retained = append(retained, observation); return nil })
	if err != nil || !completed || calls != 2 || len(retained) != 2 || retained[0].NativeWarmupV2.Ready || !retained[1].NativeWarmupV2.Ready {
		t.Fatal("readiness skipped real waiting or lost its retained history", err)
	}
	if first.NativeWarmupV2 != nil || first.ObservationHash != originalHash {
		t.Fatal("warm-up rewrote the original pre-preparation observation")
	}
	window, err := buildScenarioAcceptanceWindow(cfg, definition, current)
	if err != nil || window.FirstEpoch != 13 || window.EpochCount != 5 {
		t.Fatal("warm-up counted partial or missing accepted epochs", window, err)
	}
}

func TestScenarioNativeWarmupV2CancellationCannotCommitAcceptance(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.ProvisionValidatorEvidenceV2 = true
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observation := testScenarioObservation(cfg, 10)
	completed := false
	options := scenarioRunOptions{PollInterval: time.Millisecond, NativeWarmupV2: func(context.Context, *ScenarioObservation, *ScenarioObservation) (*ScenarioNativeWarmupV2, error) {
		cancel()
		return &ScenarioNativeWarmupV2{Schema: "urnetwork-sim-native-warmup-v2", Phase: "release-1.0", Detail: "awaiting both actual applications"}, nil
	}, NativeWarmupCompleteV2: func(context.Context) error { completed = true; return nil }}
	var err error
	options.NativeWarmupBudgetV2, err = newScenarioNativeWarmupBudgetV2(cfg, "release-1.0", false, observation.Status.Contracts.FinalizedHead, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	current, err := waitScenarioNativeWarmupV2(ctx, cfg, "release-1.0", observation, observation, &staticScenarioProbe{}, options, func(*ScenarioObservation) error { return nil })
	if !errors.Is(err, context.Canceled) || completed || current.NativeWarmupV2.Ready {
		t.Fatal("cancelled warm-up admitted acceptance or lost cancellation", err)
	}
}

func TestScenarioNativeWarmupV2ChargesBothPhaseAndResumedPreparationWork(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		phase         string
		warmup, after uint64
	}{{"release-1.0", 1530, 6040}, {"production-soak", 1620, 1640}} {
		span, err := scenarioNativeWarmupBlocksV2(cfg, entry.phase)
		if err != nil || span != entry.warmup {
			t.Fatal("actual warm-up cadence differs", span, err)
		}
		before, err := work.remaining(entry.phase, true)
		if err != nil {
			t.Fatal(err)
		}
		after, err := work.afterWarmup(entry.phase)
		if err != nil || after != entry.after || before-after != span {
			t.Fatal("resumed preparation omitted or released unperformed warm-up", before, after, err)
		}
	}
	cfg.Hyperparameters.OwnerControlled["commit_reveal_period"] = ^uint64(0)
	if _, err := scenarioNativeWarmupBlocksV2(cfg, "release-1.0"); err == nil {
		t.Fatal("overflowed warm-up became a finite forecast")
	}
}

func TestScenarioNativeWarmupV2SharesPreparationDeadlineWithoutBorrowingAcceptance(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	head := ChainHead{Number: 9000, Hash: finalTestHex(0x29)}
	for _, phase := range []string{"release-1.0", "production-soak"} {
		t.Run(phase, func(t *testing.T) {
			budget, err := newScenarioNativeWarmupBudgetV2(cfg, phase, false, head, started)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := work.remaining(phase, false)
			if err != nil {
				t.Fatal(err)
			}
			preparedAt := head.Number + work.releasePreparation
			if phase == "production-soak" {
				preparedAt = head.Number + work.productionPreparation
			}
			remaining, err := work.duringNativePreparation(phase, budget, preparedAt, started.Add(time.Second))
			if err != nil || preparedAt+remaining != head.Number+initial {
				t.Fatal("completed preparation restarted a full native wait", preparedAt, remaining, initial, err)
			}
			after, err := work.afterWarmup(phase)
			if err != nil {
				t.Fatal(err)
			}
			last, err := work.duringNativePreparation(phase, budget, budget.EndBlock, started.Add(time.Second))
			if err != nil || last != after {
				t.Fatal("readiness consumed a required acceptance block", last, after, err)
			}
			if _, err := work.duringNativePreparation(phase, budget, budget.EndBlock+1, started.Add(time.Second)); err == nil {
				t.Fatal("slow preparation borrowed from later accepted epochs")
			}
			if _, err := work.duringNativePreparation(phase, budget, preparedAt, budget.Deadline); err == nil {
				t.Fatal("stalled chain restarted the wall watchdog")
			}
			resumed, err := newScenarioNativeWarmupBudgetV2(cfg, phase, true, head, started)
			if err != nil || resumed.EndBlock != budget.EndBlock {
				t.Fatal("already completed preparation bypassed actual native readiness", resumed, err)
			}
		})
	}

	// Even a source callback that would return ready cannot publish readiness
	// after preparation consumed the original deadline. No timing sleep is used.
	observation := testScenarioObservation(cfg, 10)
	budget, err := newScenarioNativeWarmupBudgetV2(cfg, "release-1.0", false, observation.Status.Contracts.FinalizedHead, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	called, completed, retained := false, false, false
	options := scenarioRunOptions{NativeWarmupBudgetV2: budget, NativeWarmupV2: func(context.Context, *ScenarioObservation, *ScenarioObservation) (*ScenarioNativeWarmupV2, error) {
		called = true
		return &ScenarioNativeWarmupV2{Schema: "urnetwork-sim-native-warmup-v2", Phase: "release-1.0", Ready: true}, nil
	}, NativeWarmupCompleteV2: func(context.Context) error { completed = true; return nil }}
	if _, err := waitScenarioNativeWarmupV2(t.Context(), cfg, "release-1.0", observation, observation, &staticScenarioProbe{}, options, func(*ScenarioObservation) error { retained = true; return nil }); !errors.Is(err, context.DeadlineExceeded) || called || completed || retained {
		t.Fatal("expired preparation created a new readiness or acceptance window", err, called, completed, retained)
	}
}
