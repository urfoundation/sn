// Actual replay/append/reopen barriers cancel only the owning context. No hook
// supplies a result or substitutes a financial observation for original bytes.
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func economicYumaTestCancellation(t *testing.T, phase string, archived bool) {
	t.Helper()
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	if archived {
		f.sample(t, monitorServiceHooks{})
		f.reset(t)
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("actual original archive prerequisite", code, issue)
		}
	}
	before, beforeErr := os.ReadFile(f.source.checkpoint)
	if beforeErr != nil && !errors.Is(beforeErr, os.ErrNotExist) {
		t.Fatal(beforeErr)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	var reached atomic.Uint64
	ctx = context.WithValue(ctx, nativeYumaOwnerHooksKey{}, nativeYumaOwnerHooks{before: func(actual string) {
		if actual == phase {
			reached.Add(1)
			cancel()
		}
	}})
	var output, issue bytes.Buffer
	code := runMainWithMonitorHooks(ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{})
	after, afterErr := os.ReadFile(f.source.checkpoint)
	if code != 0 || output.Len() != 0 || issue.Len() != 0 || reached.Load() != 1 || !bytes.Equal(before, after) || errors.Is(beforeErr, os.ErrNotExist) != errors.Is(afterErr, os.ErrNotExist) {
		t.Fatal("canceled original Yuma owner manufactured integrity, publication or another cursor", phase, code, reached.Load(), output.String(), issue.String(), beforeErr, afterErr)
	}
	// Reopen with the same original policy and authority; any completed producer
	// bytes remain the original immutable job, not a replacement approval.
	summary := f.sample(t, monitorServiceHooks{})
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.NativeHeld || summary.Yuma == nil || !summary.Yuma.Current || summary.NativeIssue != "" || summary.TargetMet != nil || f.source.claimReads.Load() == 0 {
		t.Fatal("healthy continuation inherited a canceled Yuma integrity hold", phase, summary)
	}
}

func TestEconomicConservationPublicYumaCanceledDerivationKeepsSameAuthority(t *testing.T) {
	economicYumaTestCancellation(t, "derive", false)
}

func TestEconomicConservationPublicYumaCanceledAppendKeepsOriginalCheckpoint(t *testing.T) {
	economicYumaTestCancellation(t, "append", false)
}

func TestEconomicConservationPublicYumaCanceledArchiveReopenKeepsOriginalEvidence(t *testing.T) {
	economicYumaTestCancellation(t, "validate", true)
}

func TestEconomicConservationPublicYumaReadCancellationKeepsHealthyContinuation(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	var output, issue bytes.Buffer
	var reached atomic.Uint64
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{beforeEconomicNativeAppend: func(ctx context.Context, cancel context.CancelFunc) {
		if ctx.Err() != nil {
			t.Error("original observation already lost its read owner", ctx.Err())
		}
		reached.Add(1)
		cancel()
	}})
	var first economicConservationSummary
	if err := json.Unmarshal(output.Bytes(), &first); err != nil {
		t.Fatal(err, code, issue.String())
	}
	if code != 3 || reached.Load() != 1 || first.NativeCurrent || first.NativeHeld || !first.VaultCurrent || first.NativeCursor != f.source.policy.Native.Observation.From || first.NativeIssue == "" || f.source.claimReads.Load() == 0 || f.ctx.Err() != nil {
		t.Fatal("local read cancellation became permanent integrity or stopped healthy siblings", code, first, issue.String())
	}
	second := f.sample(t, monitorServiceHooks{})
	if !second.NativeCurrent || second.NativeHeld || second.NativeIssue != "" || !second.VaultCurrent || second.Yuma == nil || !second.Yuma.Current || second.NativeCursor != f.source.policy.Native.Observation.Through || second.VaultCursor.Number <= first.VaultCursor.Number {
		t.Fatal("same original approval could not continue after local read cancellation", first, second)
	}
}

func economicYumaTestRepinAuthority(t *testing.T, f *economicConservationArchiveFixture, producer *nativeProducerPublicFixture) {
	t.Helper()
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
}

func TestEconomicConservationPublicYumaWitnessCapacityKeepsRetryAndSiblingProgress(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	// This is feasible by the complete-UID minimum but smaller than the actual
	// original stack/host envelopes. The real retained wire-size guard decides.
	producer.source.policy.MaximumUids = 2
	f.source.policy.Native.Observation.MaximumUids = 2
	producer.authority.Yuma.MaximumWitnessBytes = 6144
	economicYumaTestRepinAuthority(t, f, producer)
	var previous economicConservationSummary
	for attempt := 0; attempt < 2; attempt++ {
		var output, issue bytes.Buffer
		code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{})
		var summary economicConservationSummary
		if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
			t.Fatal("actual capacity sample was not published", err, code, issue.String())
		}
		if code != 3 || summary.NativeCurrent || summary.NativeHeld || !summary.VaultCurrent || summary.NativeCursor != f.source.policy.Native.Observation.From || !strings.Contains(summary.NativeIssue, "original witness exceeds its independently admitted retained byte capacity") || summary.Yuma == nil || len(summary.Yuma.Active) != 0 || summary.TargetMet != nil || f.source.claimReads.Load() == 0 {
			t.Fatal("actual witness capacity became a retained integrity verdict or blocked siblings", attempt, code, summary)
		}
		if attempt != 0 && summary.VaultCursor.Number <= previous.VaultCursor.Number {
			t.Fatal("same-authority capacity retry blocked the healthy vault continuation", previous, summary)
		}
		previous = summary
	}
}

func TestEconomicConservationPublicYumaChangedAuthorityStillHoldsOnlyNative(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	producer.authority.Yuma.ReviewSha256 = monitorReadDigest([]byte("synthetic unapproved original allocation review"))
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
	var output, issue bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err, code, issue.String())
	}
	if code != 3 || summary.NativeCurrent || !summary.NativeHeld || !summary.VaultCurrent || !strings.Contains(summary.NativeIssue, "independent authority signature is invalid") || f.source.claimReads.Load() == 0 {
		t.Fatal("genuine original allocation conflict lost quarantine or stopped siblings", code, summary)
	}
}

func TestNativeExecutionDerivationPreservesOperationalCauseTree(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("original operation: %w", context.Canceled), errors.Join(context.Canceled, context.DeadlineExceeded), fmt.Errorf("original witness bytes: %w", errMonitorEconomicCapacity)} {
		actual := nativeExecutionDerivationError(err)
		if actual != err || errors.Is(actual, errRpcIntegrity) || economicConservationAppendHeld(actual) {
			t.Fatal("typed operational derivation acquired synthetic integrity", err, actual)
		}
	}
}

func TestNativeExecutionDerivationIntegrityDominatesJoinedOperationalCause(t *testing.T) {
	for _, cause := range []error{errRpcIntegrity, errRpcIdentityMismatch, durablevolume.ErrIdentity} {
		err := errors.Join(context.Canceled, errMonitorEconomicCapacity, cause)
		actual := nativeExecutionDerivationError(err)
		if actual != err || !errors.Is(actual, cause) || !economicConservationAppendHeld(actual) {
			t.Fatal("joined cancellation or capacity hid an original evidence conflict", actual)
		}
	}
	conflict := errors.New("original amount contradicts actual replay")
	if actual := nativeExecutionDerivationError(conflict); !errors.Is(actual, errRpcIntegrity) || !errors.Is(actual, conflict) || !economicConservationAppendHeld(actual) {
		t.Fatal("untyped original derivation contradiction lost its integrity boundary", actual)
	}
}
