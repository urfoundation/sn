// Public conservation tests execute distinct actual capture/replay images over
// Rust-exported programs. Synthetic source peers cannot supply stake amounts.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEconomicConservationPublicPrincipalOriginalCausesSeparateStockAndIncome(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-causes", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.PrincipalEffects == nil || !summary.PrincipalEffects.Current || len(summary.PrincipalEffects.Active) != 1 {
		t.Fatal("actual original causal execution was not consumed", summary)
	}
	pool := summary.PrincipalEffects.Active[0].Pools[0]
	if !pool.Complete || *pool.Before != "14" || *pool.After != "24" || *pool.StockChange != "10" || pool.Deposits != "5" || pool.Withdrawals != "3" || pool.Refunds != "2" || pool.NativeEarnings != "6" || *pool.Residual != "0" {
		t.Fatal("principal cause census collapsed deposit/refund/withdrawal into earnings", pool)
	}
	state := f.source.state(t)
	if summary.OpeningPrincipalAlpha == nil || *summary.OpeningPrincipalAlpha != "14" || summary.TargetMet != nil || summary.ActualNativeOutcomeVerified || summary.FullQuantizationToleranceAlpha != nil || summary.TailGrossAlpha == nil || *summary.TailGrossAlpha != "9" || len(state.Captures) != 1 || state.Captures[0].OpeningPrincipalAlpha != nil || *state.Captures[0].AmountDifferenceAlpha != "14" {
		t.Fatal("principal stock or causal observation became an unproved vault/target certificate", summary, state.Captures)
	}
}

func TestEconomicConservationPublicUnclassifiedNetZeroPrincipalMutationsStayUnknown(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-unclassified", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.PrincipalEffects == nil || summary.PrincipalEffects.Current || len(summary.PrincipalEffects.Active) != 1 {
		t.Fatal("unclassified original mutation census became complete", summary)
	}
	value := summary.PrincipalEffects.Active[0]
	if len(value.UnmatchedMutations) != 2 || value.Pools[0].Complete || value.Pools[0].Residual == nil || *value.Pools[0].Residual != "0" || *value.Pools[0].StockChange != "6" {
		t.Fatal("net-zero unclassified deposit/withdrawal was hidden by boundary equality", value)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if len(state.PrincipalExecutions) != 1 || state.Archive.PrincipalEffects != nil {
		t.Fatal("unresolved original principal work was retired as complete", state)
	}
}

func TestEconomicConservationPublicPrincipalResidualCannotBeRoundedAway(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-residual", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.PrincipalEffects == nil || summary.PrincipalEffects.Current {
		t.Fatal("unexplained actual stock change became complete", summary)
	}
	value := summary.PrincipalEffects.Active[0]
	if len(value.UnmatchedMutations) != 1 || value.Pools[0].Residual == nil || *value.Pools[0].Residual != "1" || value.Pools[0].NativeEarnings != "6" || summary.TargetMet != nil {
		t.Fatal("residual was relabelled as native income or rounding", value)
	}
}

func TestEconomicConservationPublicRolledBackPrincipalDepositNeverBecomesIncome(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-rollback", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.PrincipalEffects == nil || !summary.PrincipalEffects.Current {
		t.Fatal("rollback changed committed original cause census", summary)
	}
	pool := summary.PrincipalEffects.Active[0].Pools[0]
	state := f.source.state(t)
	if pool.Deposits != "0" || pool.NativeEarnings != "6" || *pool.After != "20" || len(state.PrincipalExecutions[0].Projection.Mutations) != 1 || len(state.PrincipalExecutions[0].Projection.Effects) != 1 {
		t.Fatal("rolled-back original deposit survived into economic evidence", pool, state.PrincipalExecutions)
	}
}

func TestEconomicConservationPublicPrincipalZeroAndAbsentEffectsRetireDifferently(t *testing.T) {
	for _, name := range []string{"zero", "absent"} {
		f, _ := newEconomicConservationPrincipalFixture(t, "effects-"+name, true, nil)
		summary := f.sample(t, monitorServiceHooks{})
		if summary.PrincipalEffects == nil || summary.PrincipalEffects.Current != (name == "zero") || (summary.PrincipalEffects.Active[0].Pools[0].Before == nil) != (name == "absent") {
			t.Fatal("absent original API stock became observed zero", name, summary)
		}
		f.reset(t)
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal(code, issue)
		}
		state := f.source.state(t)
		if (state.Archive.PrincipalEffects != nil) != (name == "zero") || (len(state.PrincipalExecutions) == 0) != (name == "zero") {
			t.Fatal("principal retirement lost known versus unknown evidence", name, state)
		}
	}
}

func TestEconomicConservationPublicPrincipalCompleteEffectsSurviveColdArchive(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-causes", true, nil)
	first := f.sample(t, monitorServiceHooks{})
	original := f.source.state(t).PrincipalExecutions[0]
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	second := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if second.PrincipalEffects == nil || !second.PrincipalEffects.Current || second.PrincipalEffects.Archived == nil || second.PrincipalEffects.Archived.Blocks != 1 || len(state.PrincipalExecutions) != 0 || second.PrincipalEffects.Archived.Pools[0].Deposits != first.PrincipalEffects.Active[0].Pools[0].Deposits || *second.PrincipalEffects.Archived.Pools[0].After != "24" || second.PrincipalEffects.Archived.Through != original.Projection.Boundary || second.TargetMet != nil {
		t.Fatal("cold archive lost original principal cause evidence or recounted stock", second, state)
	}
}

func TestEconomicConservationPublicOmittedPrincipalCallsiteStaysVisible(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-causes", true, func(job *historicalReplayJob) {
		rules := job.ObservationProfile.Rules[:0]
		for _, rule := range job.ObservationProfile.Rules {
			if rule.Purpose != "native-principal-deposit" {
				rules = append(rules, rule)
			}
		}
		job.ObservationProfile.Rules = rules
	})
	summary := f.sample(t, monitorServiceHooks{})
	if summary.PrincipalEffects == nil || summary.PrincipalEffects.Current || len(summary.PrincipalEffects.Active[0].UnmatchedMutations) != 1 || summary.PrincipalEffects.Active[0].Pools[0].Complete {
		t.Fatal("omitted original causal callsite hid a committed deposit", summary)
	}
}

func TestEconomicConservationPublicPrincipalDepositCannotBorrowNativeEarningLabel(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-causes", true, func(job *historicalReplayJob) {
		for index := range job.ObservationProfile.Rules {
			if job.ObservationProfile.Rules[index].Purpose == "native-principal-deposit" {
				job.ObservationProfile.Rules[index].Purpose = "native-principal-earning"
			}
		}
	})
	summary := f.sample(t, monitorServiceHooks{})
	if summary.PrincipalEffects == nil || summary.PrincipalEffects.Current || summary.PrincipalEffects.Active[0].Pools[0].Complete || summary.TargetMet != nil {
		t.Fatal("a reviewed label contradicted actual native recipient earnings without refusal", summary)
	}
	pool := summary.PrincipalEffects.Active[0].Pools[0]
	if pool.NativeEarnings != "11" || *pool.Residual != "0" || len(summary.PrincipalEffects.Active[0].UnmatchedMutations) != 0 {
		t.Fatal("fixture did not reach the independent native earning cross-check", pool)
	}
}

func TestEconomicConservationPublicPrincipalArchiveSummaryCannotInventEarnings(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "effects-causes", true, nil)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	state.Archive.PrincipalEffects.Pools[0].NativeEarnings = "99"
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(writer.publish(append(raw, '\n'), nil), writer.close()); err != nil {
		t.Fatal(err)
	}
	before := f.source.claimReads.Load()
	var output, issues bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issues, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || !strings.Contains(issues.String(), "economic archive summary differs from exact original checkpoints") || f.source.claimReads.Load() != before {
		t.Fatal("self-sealed principal archive summary replaced original execution", code, output.String(), issues.String())
	}
}

func TestEconomicConservationPublicPrincipalScopeReviewNeedsOriginalSignature(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "effects-causes", true, nil)
	producer.authority.Principal.Effects.ReviewSha256 = monitorReadDigest([]byte("synthetic unapproved replacement causal scope"))
	raw, err := json.Marshal(producer.authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.source.policy.Native.Observation.Execution.Producer.Authority.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.source.policy.Native.Observation.Execution.Principal = producer.authority.Principal
	f.source.policy.Native.Observation.Execution.Producer.Authority.Sha256 = monitorReadDigest(raw)
	f.source.writePolicy(t)
	var output, issues bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issues, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err, issues.String())
	}
	if code != 3 || summary.NativeCurrent || !strings.Contains(summary.NativeIssue, "independent authority signature is invalid") || !summary.VaultCurrent || f.source.claimReads.Load() == 0 {
		t.Fatal("principal causal scope enrolled itself or blocked healthy siblings", code, summary)
	}
}
