// These roots consume actual independently owned capture/replay executables
// and Rust-exported original programs through the public combined owner.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEconomicConservationPublicCompleteYumaUsesWholeMinerDenominator(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.Yuma == nil || !summary.Yuma.Current || summary.Yuma.MinerDenominator == nil || *summary.Yuma.MinerDenominator != "98" || summary.FullQuantizationToleranceAlpha == nil || *summary.FullQuantizationToleranceAlpha != "2" || len(summary.Yuma.Active) != 1 || len(summary.Yuma.Active[0].Allocations) != 2 {
		t.Fatal("complete original Yuma denominator/tolerance is unavailable", summary)
	}
	allocation := summary.Yuma.Active[0].Allocations
	if allocation[0].ActualMiner != "9" || allocation[1].ActualMiner != "89" || allocation[0].ReferenceMiner != "10" || allocation[1].ReferenceMiner != "90" || summary.Execution.MinerAllocation != "100" || summary.TailGrossAlpha == nil || *summary.TailGrossAlpha != "9" {
		t.Fatal("selected provider subtotal replaced complete original miner denominator", summary)
	}
	if summary.TargetMet != nil || summary.ActualNativeOutcomeVerified || summary.ActivationReady {
		t.Fatal("allocation arithmetic fabricated complete economic or activation authority", summary)
	}
}

func TestEconomicConservationPublicYuma3BindsOriginalBranch(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-yuma3", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.Yuma == nil || !summary.Yuma.Current || len(summary.Yuma.Active) != 1 || len(summary.Yuma.Active[0].Allocations) != 2 || summary.Yuma.Active[0].Allocations[0].ActualValidator != "0" || summary.Yuma.Active[0].Allocations[1].ActualValidator != "100" || *summary.Yuma.MinerDenominator != "98" {
		t.Fatal("actual Yuma3 active-stake dividend branch was not closed", summary)
	}
}

func TestEconomicConservationPublicLiquidYumaBindsOriginalBranch(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-liquid", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.Yuma == nil || !summary.Yuma.Current || summary.FullQuantizationToleranceAlpha == nil || *summary.FullQuantizationToleranceAlpha != "2" || summary.Yuma.Active[0].Allocations[1].ActualValidator != "100" {
		t.Fatal("actual liquid-alpha original branch was not closed", summary)
	}
}

func TestEconomicConservationPublicMissingYumaRowsRemainUnknown(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, func(job *historicalReplayJob) {
		rules := job.ObservationProfile.Rules[:0]
		for _, rule := range job.ObservationProfile.Rules {
			if rule.Purpose != "native-yuma-bonds" {
				rules = append(rules, rule)
			}
		}
		job.ObservationProfile.Rules = rules
	})
	summary := f.sample(t, monitorServiceHooks{})
	if summary.Yuma == nil || summary.Yuma.Current || summary.FullQuantizationToleranceAlpha != nil || summary.Yuma.MinerDenominator != nil || summary.Yuma.Active[0].Issue == "" || summary.NativeCursor != f.source.policy.Native.Observation.Through || summary.VaultCursor.Number == f.source.policy.Vault.From.Number {
		t.Fatal("missing Yuma row became zero or blocked healthy original observations", summary)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if state.Archive.Yuma != nil || len(state.Yuma) != 1 {
		t.Fatal("unknown complete allocation evidence was retired", state)
	}
}

func TestEconomicConservationPublicChangedYumaInputCannotBorrowFinalTolerance(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, func(job *historicalReplayJob) {
		for index := range job.ObservationProfile.Rules {
			rule := &job.ObservationProfile.Rules[index]
			if rule.Purpose == "native-yuma-weights" {
				for j := range rule.Memory {
					if rule.Memory[j].Name == "values" {
						rule.Memory[j].Address = 8300
					}
				}
			}
		}
	})
	summary := f.sample(t, monitorServiceHooks{})
	if summary.Yuma == nil || summary.Yuma.Current || summary.FullQuantizationToleranceAlpha != nil || summary.Execution.FixedPointTolerance == "" || !strings.Contains(summary.Yuma.Active[0].Issue, "differs from complete fixed source calculation") {
		t.Fatal("final cast tolerance hid a different original Yuma input", summary)
	}
}

func TestEconomicConservationPublicCompleteYumaSurvivesColdArchive(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	first := f.sample(t, monitorServiceHooks{})
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	second := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if second.Yuma == nil || !second.Yuma.Current || second.Yuma.Archived == nil || second.Yuma.Archived.Blocks != 1 || len(state.Yuma) != 0 || *second.Yuma.MinerDenominator != *first.Yuma.MinerDenominator || *second.FullQuantizationToleranceAlpha != *first.FullQuantizationToleranceAlpha {
		t.Fatal("cold archive lost or recounted the complete allocation witness", second, state)
	}
}

func TestEconomicConservationPublicYumaArchiveCannotSelfSealTolerance(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	state.Archive.Yuma.FullQuantizationTolerance = "0"
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
	before, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reads := f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	after, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 || output.Len() != 0 || reads != f.source.claimReads.Load() || !bytes.Equal(before, after) || !strings.Contains(diagnostic.String(), "economic archive summary differs from exact original checkpoints") {
		t.Fatal("self-sealed Yuma tolerance bypassed original archive admission", code, diagnostic.String())
	}
}

func TestNativeProducerPublicYumaSourceReviewRequiresOriginalSignature(t *testing.T) {
	_, producer := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	positive, code, issue := producer.command(t)
	if code != 0 || !positive.Complete || len(positive.Blocks) != 1 || positive.Blocks[0].ExecutionOutcome.Yuma == nil || positive.Blocks[0].ExecutionOutcome.Yuma.FullQuantizationTolerance == nil {
		t.Fatal("original signed Yuma producer did not reach actual allocation", code, issue)
	}
	producer.authority.Yuma.ReviewSha256 = monitorReadDigest([]byte("synthetic replacement without original signature"))
	producer.source.policy.Execution.Yuma = producer.authority.Yuma
	raw, err := json.Marshal(producer.authority)
	if err != nil {
		t.Fatal(err)
	}
	reference := &producer.source.policy.Execution.Producer.Authority
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reference.Sha256 = monitorReadDigest(raw)
	negative, code, issue := producer.command(t)
	if code == 0 || negative.Complete || !strings.Contains(issue, "native producer independent authority signature is invalid") {
		t.Fatal("changed full Yuma source authority borrowed an earlier signature", code, issue)
	}
}

// A cold original witness must remain owned by the caller during its potentially
// expensive arithmetic, even when its hashes and authority are already valid.
func TestEconomicConservationYumaOriginalValidationKeepsCanceledOwner(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if len(state.Yuma) != 1 {
		t.Fatal("actual public original allocation witness is absent", state)
	}
	value := state.Yuma[0]
	if err := value.Projection.validate(t.Context(), f.source.policy.Native.Observation, value.Outcome); err != nil {
		t.Fatal("positive original witness was not admitted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := value.Projection.validate(ctx, f.source.policy.Native.Observation, value.Outcome); !errors.Is(err, context.Canceled) {
		t.Fatal("original allocation validation discarded its canceled owner", err)
	}
	if _, err := state.yumaSummary(ctx, f.source.policy); !errors.Is(err, context.Canceled) {
		t.Fatal("allocation summary discarded its canceled owner", err)
	}
}

func TestEconomicConservationPublicYumaWorkLimitKeepsHealthySiblings(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	producer.authority.Yuma.MaximumOperations = 1
	producer.source.policy.Execution.Yuma = producer.authority.Yuma
	message, err := producer.authority.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	producer.authority.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	raw, err := json.Marshal(producer.authority)
	if err != nil {
		t.Fatal(err)
	}
	reference := &producer.source.policy.Execution.Producer.Authority
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reference.Sha256 = monitorReadDigest(raw)
	f.source.policy.Native.Observation.Execution.Yuma = producer.authority.Yuma
	f.source.writePolicy(t)
	var previous economicConservationSummary
	for attempt := 0; attempt < 2; attempt++ {
		var output, diagnostic bytes.Buffer
		code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
		var summary economicConservationSummary
		if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
			t.Fatal(code, diagnostic.String(), err)
		}
		if code != 3 || summary.NativeCurrent || summary.NativeHeld || !summary.VaultCurrent || summary.FullQuantizationToleranceAlpha != nil || !strings.Contains(summary.NativeIssue, "exceeds admitted finite work") || summary.NativeCursor != f.source.policy.Native.Observation.From || summary.VaultCursor.Number == f.source.policy.Vault.From.Number || f.source.claimReads.Load() == 0 {
			t.Fatal("exhausted allocation arithmetic blocked siblings or invented tolerance", code, summary)
		}
		if attempt != 0 && summary.VaultCursor.Number <= previous.VaultCursor.Number {
			t.Fatal("same-authority arithmetic capacity retry blocked healthy vault advancement", summary)
		}
		previous = summary
	}
}

// This is a byte-capacity fixture, not a synthetic financial proof. Its shape
// expands real captured node/matrix envelopes to a complete realistic census;
// the public command must refuse that resource plan before any source read.
func TestEconomicConservationPublicYumaCompleteCensusSizingRefusesBeforeRpc(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	f.sample(t, monitorServiceHooks{})
	original := f.source.state(t).Yuma[0]
	sized := original
	sized.Projection.Records = append([]historicalReplayObservation{}, original.Projection.Records[:2]...)
	sized.Projection.Allocations = nil
	sized.Outcome.Recipients = nil
	for index := 0; index < 1024; index++ {
		// Each UID retains its own original node plus explicit weights/bonds rows.
		for _, source := range []int{2, 4, 6} {
			record := original.Projection.Records[source]
			record.Ordinal = uint64(len(sized.Projection.Records) + 1)
			sized.Projection.Records = append(sized.Projection.Records, record)
		}
		allocation := original.Projection.Allocations[0]
		allocation.Uid, allocation.Hotkey = uint16(index), fmt.Sprintf("0x%064x", index+1)
		sized.Projection.Allocations = append(sized.Projection.Allocations, allocation)
		recipient := original.Outcome.Recipients[0]
		recipient.Uid, recipient.Hotkey = allocation.Uid, allocation.Hotkey
		sized.Outcome.Recipients = append(sized.Outcome.Recipients, recipient)
	}
	fullBytes, err := nativeYumaWitnessBytes(sized.Projection, sized.Outcome)
	if err != nil || fullBytes <= maxRpcReplyBytes {
		t.Fatal("realistic complete UID witness failed to exercise physical head capacity", fullBytes, err)
	}
	f.source.policy.Native.Observation.MaximumUids = 1024
	f.source.policy.Native.Observation.Execution.Yuma.MaximumWitnessBytes = fullBytes
	reads, requests := f.source.claimReads.Load(), producer.requests.Load()
	raw, err := json.Marshal(f.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.source.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 2 || output.Len() != 0 || reads != f.source.claimReads.Load() || requests != producer.requests.Load() || !strings.Contains(diagnostic.String(), "complete witness reserve exceeds provisioned checkpoint profile") {
		t.Fatal("impossible full UID head profile reached replay or borrowed a smaller census", code, diagnostic.String(), fullBytes)
	}
}

func TestEconomicConservationPublicYumaUnresolvedBlockReserveCannotBorrowCapacity(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	if err := f.source.policy.validate(); err != nil {
		t.Fatal("one-block positive sizing authority was invalid", err)
	}
	f.source.policy.Native.Observation.Execution.Yuma.HotBlockReserve = 2
	reads, requests := f.source.claimReads.Load(), producer.requests.Load()
	raw, err := json.Marshal(f.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.source.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 2 || output.Len() != 0 || reads != f.source.claimReads.Load() || requests != producer.requests.Load() || !strings.Contains(diagnostic.String(), "complete witness reserve exceeds provisioned checkpoint profile") {
		t.Fatal("unresolved original block forecast silently borrowed another witness capacity", code, diagnostic.String())
	}
}
