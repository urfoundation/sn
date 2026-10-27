// Public follow tests hold real read/process boundaries while the native,
// vault and Claim readers publish. No hook supplies a fee or a source verdict.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type economicConservationFeeWorkerFixture struct {
	observer  *economicConservationFixture
	request   economicNativeFeeRequest
	reference planFileReference
	raw       []byte
}

// The exporter signs synthetic originals for chain964. The public policy's
// network admission remains exact; old31337 component fixtures are separate.
func newEconomicConservationFeeWorkerFixture(t *testing.T) economicConservationFeeWorkerFixture {
	t.Helper()
	directory := os.Getenv("URNETWORK_ECONOMIC_FEE_FIXTURE_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("exact Server chain964 fee export directory is required")
	}
	input, _, job := historicalFeeContextTestFixtureAt(t, filepath.Join(directory, "success.json"), "success", "pair")
	request, _, _ := economicNativeFeeTestRequestForContext(t, input, job)
	f := newEconomicConservationFixture(t, false)
	f.policy.FeeAuthority = &request.Policy
	f.writePolicy(t)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	reference := planFileReference{Path: filepath.Join(filepath.Dir(f.path), "native-fee-request.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return economicConservationFeeWorkerFixture{observer: f, request: request, reference: reference, raw: raw}
}

func (self economicConservationFeeWorkerFixture) args(t *testing.T) []string {
	t.Helper()
	return append(self.observer.args(t), "--native-fee-request", self.reference.Path, "--native-fee-request-sha256", self.reference.Sha256, "--follow")
}

// A channel handoff, not elapsed wall time, releases every deliberately held
// read. The test deadline is only the ordinary deadlock safety net.
func TestEconomicConservationPublicSlowFeeReplayDoesNotStallSiblingProgress(t *testing.T) {
	f := newEconomicConservationFeeWorkerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	completed := make(chan error, 1)
	var once sync.Once
	var publications int
	var firstLot string
	var output, diagnostic bytes.Buffer
	hooks := monitorServiceHooks{
		nativeFeeReplay: historicalReplayHooks{beforeStart: func(owner context.Context, _ *os.File) {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-owner.Done():
			}
		}},
		afterNativeFeeRead: func(_ context.Context, err error) { completed <- err },
		afterEvent: func(_ context.Context, role string) {
			if role != economicConservationRole {
				return
			}
			publications++
			state := f.observer.state(t)
			if state.Native.Cursor.Number != 102 || state.Vault.Cursor.Number != uint64(10+publications) || state.NativeHeld || state.VaultHeld || state.NativeIssue != "" || state.VaultIssue != "" || f.observer.claimReads.Load() < uint64(publications) {
				t.Fatal("held fee replay stopped healthy original domain advancement", publications, state.Native.Cursor, state.Vault.Cursor)
			}
			if publications == 1 {
				select {
				case <-entered:
				case err := <-completed:
					t.Fatal("actual fee replay did not enter its owned process boundary", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if len(state.Lots) == 0 {
					t.Fatal("native source produced no retained occurrence")
				}
				firstLot = state.Lots[0].Id
			}
			if publications <= 2 && (!state.NativeFeePending || len(state.NativeFees) != 0) {
				t.Fatal("held fee replay published a partial fee result")
			}
			if publications == 2 {
				close(release)
			}
			if publications == 3 {
				fees, err := state.feeSummary(f.observer.policy)
				if err != nil || fees == nil || fees.AuthenticatedFees != 1 || fees.WholeProviderCensus || state.NativeFeePending || state.NativeFeeIssue != "" || len(state.NativeFees) != 1 || len(state.Lots) != 2 || state.Lots[0].Id != firstLot || len(state.Payments) != 1 {
					t.Fatal("completed fee result replaced latest native/vault/Claim progress", err, fees, state)
				}
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			if publications == 2 {
				if err := <-completed; err != nil {
					t.Fatal("released original fee verifier failed", err)
				}
			}
			return ctx.Err() == nil
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, f.args(t), &output, &diagnostic, func() time.Time { return f.observer.now }, hooks)
	if code != 0 || publications != 3 || bytes.Count(output.Bytes(), []byte{'\n'}) != 3 {
		t.Fatal("public owner did not publish through held fee replay", code, publications, diagnostic.String())
	}
}

func TestEconomicConservationPublicMissingFeeRequestRecoversWithoutCursorRollback(t *testing.T) {
	f := newEconomicConservationFeeWorkerFixture(t)
	if err := os.Remove(f.reference.Path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	completed := make(chan error, 1)
	var reads atomic.Uint64
	var publications int
	var originalLot string
	var output, diagnostic bytes.Buffer
	hooks := monitorServiceHooks{
		beforeNativeFeeRead: func(owner context.Context, _ context.CancelFunc) {
			if reads.Add(1) == 1 {
				select {
				case <-release:
				case <-owner.Done():
				}
			}
		},
		afterNativeFeeRead: func(_ context.Context, err error) { completed <- err },
		afterEvent: func(_ context.Context, _ string) {
			publications++
			state := f.observer.state(t)
			if state.Vault.Cursor.Number != uint64(10+publications) || state.Native.Cursor.Number != 102 || len(state.Lots) != 2 || state.NativeHeld || state.VaultHeld || f.observer.claimReads.Load() < uint64(publications) {
				t.Fatal("optional missing fee input erased healthy observations", state)
			}
			switch publications {
			case 1:
				originalLot = state.Lots[0].Id
				close(release)
			case 2:
				if state.NativeFeeIssue == "" || state.NativeFeeHeldRequest != "" || len(state.NativeFees) != 0 || state.NativeFeePending {
					t.Fatal("transient missing fee input was hidden or held as a contradiction", state)
				}
				if err := os.WriteFile(f.reference.Path, f.raw, 0600); err != nil {
					t.Fatal(err)
				}
			case 3:
				if state.NativeFeeIssue != "" || state.NativeFeeHeldRequest != "" || state.NativeFeePending || len(state.NativeFees) != 1 || state.Lots[0].Id != originalLot || len(state.Payments) != 1 {
					t.Fatal("fee recovery lost original evidence or restored a stale cursor", state)
				}
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			if publications <= 2 {
				err := <-completed
				if publications == 1 && !errors.Is(err, os.ErrNotExist) || publications == 2 && err != nil {
					t.Fatal("real fee read did not retain missing/recovered source outcome", publications, err)
				}
			}
			return ctx.Err() == nil
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, f.args(t), &output, &diagnostic, func() time.Time { return f.observer.now }, hooks)
	if code != 0 || publications != 3 || reads.Load() != 2 {
		t.Fatal("public fee retry did not converge with exactly two owned attempts", code, publications, reads.Load(), diagnostic.String())
	}
}

func TestEconomicConservationPublicFeeIntegrityQuarantinesOnlyExactRequest(t *testing.T) {
	f := newEconomicConservationFeeWorkerFixture(t)
	if err := os.WriteFile(f.reference.Path, append(bytes.Clone(f.raw), ' '), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	completed := make(chan error, 1)
	var reads atomic.Uint64
	var publications int
	var output, diagnostic bytes.Buffer
	hooks := monitorServiceHooks{
		beforeNativeFeeRead: func(owner context.Context, _ context.CancelFunc) {
			reads.Add(1)
			select {
			case <-release:
			case <-owner.Done():
			}
		},
		afterNativeFeeRead: func(_ context.Context, err error) { completed <- err },
		afterEvent: func(_ context.Context, _ string) {
			publications++
			state := f.observer.state(t)
			if state.Vault.Cursor.Number != uint64(10+publications) || state.Native.Cursor.Number != 102 || len(state.Lots) != 2 || f.observer.claimReads.Load() < uint64(publications) || state.NativeHeld || state.VaultHeld {
				t.Fatal("fee integrity refusal stopped an independent healthy domain", state)
			}
			if publications == 1 {
				close(release)
			} else if state.NativeFeeIssue == "" || state.NativeFeeHeldRequest != rootObjectHash(f.reference) || state.NativeFeePending || len(state.NativeFees) != 0 {
				t.Fatal("changed fee bytes did not remain quarantined under their original request", state)
			}
			if publications == 3 {
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			if publications == 1 {
				if err := <-completed; !errors.Is(err, errEconomicNativeFeeIntegrity) {
					t.Fatal("real changed request did not preserve its integrity cause", err)
				}
			}
			return ctx.Err() == nil
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, f.args(t), &output, &diagnostic, func() time.Time { return f.observer.now }, hooks)
	if code != 0 || publications != 3 || reads.Load() != 1 {
		t.Fatal("held fee request repeated its read or globally rejected the owner", code, publications, reads.Load(), diagnostic.String())
	}
}

func TestEconomicConservationPublicCancelJoinsHeldFeeReplayAfterHealthyPublication(t *testing.T) {
	f := newEconomicConservationFeeWorkerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	completed := make(chan error, 1)
	var joined atomic.Bool
	var publications int
	var output, diagnostic bytes.Buffer
	hooks := monitorServiceHooks{
		nativeFeeReplay: historicalReplayHooks{beforeStart: func(owner context.Context, _ *os.File) {
			close(entered)
			<-owner.Done()
		}},
		afterNativeFeeRead: func(_ context.Context, err error) { joined.Store(true); completed <- err },
		afterEvent: func(_ context.Context, _ string) {
			publications++
			state := f.observer.state(t)
			if state.Vault.Cursor.Number != 11 || state.Native.Cursor.Number != 102 || len(state.Lots) != 2 || !state.NativeFeePending || len(state.NativeFees) != 0 {
				t.Fatal("cancellation fixture never published healthy siblings before fee completion", state)
			}
			select {
			case <-entered:
			case err := <-completed:
				t.Fatal("owned fee verifier failed before cancellation barrier", err)
			}
			cancel()
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil },
	}
	code := runMonitorStorageTestWithHooks(t, ctx, f.args(t), &output, &diagnostic, func() time.Time { return f.observer.now }, hooks)
	if code != 0 || publications != 1 || !joined.Load() {
		t.Fatal("owner returned before joining its held fee verifier", code, publications, joined.Load(), diagnostic.String())
	}
	if err := <-completed; !errors.Is(err, context.Canceled) || !monitorOnlyCancellationCauses(err, 0) {
		t.Fatal("owned fee cancellation gained an invented integrity failure", err)
	}
	state := f.observer.state(t)
	if state.Vault.Cursor.Number != 11 || len(state.NativeFees) != 0 || len(state.Lots) != 2 {
		t.Fatal("fee cancellation erased published facts or invented partial evidence", state)
	}
}

// The standard fake clock advances only after the owned read and its joining
// caller are durably blocked. No short timeout or elapsed sleep is evidence.
func TestEconomicConservationFeeWorkerUsesOriginalFiniteReadBudget(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	reference := planFileReference{Path: filepath.Join(directory, "not-read-after-deadline.json"), Sha256: monitorReadDigest([]byte("synthetic original request"))}
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		deadlines := make(chan time.Time, 1)
		worker := newEconomicConservationFeeWorker(context.Background(), economicConservationPolicy{}, reference, 300*time.Second, monitorServiceHooks{beforeNativeFeeRead: func(ctx context.Context, _ context.CancelFunc) {
			deadline, _ := ctx.Deadline()
			deadlines <- deadline
			<-ctx.Done()
		}})
		state := &economicConservationState{Native: monitorEconomicNativeState{Cursor: economicEmissionBoundary{Number: 102}}, Vault: monitorEconomicEvmState{Cursor: economicEmissionBoundary{Number: 13}}}
		worker.start(state)
		result, ok := worker.take(true)
		if !ok || result.evidence != nil || !errors.Is(result.err, context.DeadlineExceeded) || errors.Is(result.err, errEconomicNativeFeeIntegrity) || (<-deadlines).Sub(started) != 300*time.Second || time.Since(started) != 300*time.Second {
			t.Fatal("owned fee read did not honor its exact finite budget", result.err, time.Since(started))
		}
		next, err := applyEconomicConservationFeeResult(context.Background(), economicConservationPolicy{}, state, result)
		if err != nil || next != state || next.Native.Cursor.Number != 102 || next.Vault.Cursor.Number != 13 || next.NativeFeeIssue == "" || next.NativeFeeHeldRequest != "" || next.NativeFeePending || worker.active {
			t.Fatal("fee deadline discarded latest unrelated progress or became permanent quarantine", err, next)
		}
		if err := worker.close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestEconomicConservationFeeHandoffConflictPreservesLatestKnownProof(t *testing.T) {
	policy, _, state := economicConservationFeeTestAdmit(t, "pair")
	request, _, _ := economicNativeFeeTestRequest(t, "success", "zero")
	value, err := runEconomicNativeFeeEvidence(t.Context(), request, time.Minute, historicalReplayHooks{})
	if err != nil {
		t.Fatal(err)
	}
	state.Native.Cursor.Number, state.Vault.Cursor.Number = 102, 13
	original := state.NativeFees[0].Evidence.ContentHash
	reference := planFileReference{Path: request.Approval.Path, Sha256: request.Approval.Sha256}
	next, err := applyEconomicConservationFeeResult(t.Context(), policy, state, economicConservationFeeResult{reference: reference, request: &request, evidence: value})
	if err != nil || next != state || next.Native.Cursor.Number != 102 || next.Vault.Cursor.Number != 13 || next.NativeFeeHeldRequest != rootObjectHash(reference) || next.NativeFeeIssue == "" || len(next.NativeFees) != 1 || next.NativeFees[0].Evidence.ContentHash != original {
		t.Fatal("conflicting late fee result replaced latest original evidence", err, next)
	}
}
