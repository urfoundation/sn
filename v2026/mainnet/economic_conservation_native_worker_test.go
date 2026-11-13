// Continuous producer controls use two actual Rust executables, the original
// SDK proof/feed and public HTTP receipt/artifact readers. Barriers delay real
// network operations; they never supply a replay result or an amount.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Hold one actual producer body request. Every other RPC is forwarded with
// its exact input and owner cancellation to the existing source fixture.
func economicNativeFollowBlock(t *testing.T, f *economicConservationArchiveFixture) (<-chan struct{}, func(), <-chan struct{}) {
	t.Helper()
	upstream, err := url.Parse(f.source.native.client.url)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 64*1024+1))
		if err != nil || len(raw) > 64*1024 {
			t.Error("native public input could not be bounded", err)
			http.Error(w, "invalid public input", http.StatusBadRequest)
			return
		}
		var call struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			t.Error(err)
			http.Error(w, "invalid public input", http.StatusBadRequest)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		owned := false
		if call.Method == "chain_getBlock" {
			once.Do(func() { owned = true; close(entered) })
		}
		if owned {
			defer close(exited)
			select {
			case <-release:
			case <-request.Context().Done():
				return
			}
		}
		proxy.ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	f.source.native.client = client
	return entered, unblock, exited
}

func economicNativeFollowAwait(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Minute):
		t.Fatal("owned native producer did not reach", description)
	}
}

func TestEconomicConservationPublicNativeCapturePendingKeepsEntitlementProgress(t *testing.T) {
	entitlement := newEconomicEntitlementFixture(t)
	f, producer := economicConservationPrincipalFixtureWithSource(t, entitlement.source, "present", true, nil, nil)
	entered, release, exited := economicNativeFollowBlock(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
	defer cancel()
	nativeDone, entitlementDone := make(chan struct{}), make(chan struct{})
	var nativeOnce, entitlementOnce sync.Once
	publications := 0
	var originalCensus string
	hooks := monitorServiceHooks{
		afterConservationNativeRead: func(_ context.Context, err error) {
			if err != nil && ctx.Err() == nil {
				t.Error("actual native capture/replay failed", err)
			}
			nativeOnce.Do(func() { close(nativeDone) })
		},
		afterEntitlementRead: func(_ context.Context, _ string, err error) {
			if err != nil {
				t.Error("actual original artifact read", err)
			}
			entitlementOnce.Do(func() { close(entitlementDone) })
		},
		afterEvent: func(_ context.Context, role string) {
			if role != economicConservationRole {
				return
			}
			publications++
			state := f.source.state(t)
			if state.Vault.Cursor.Number != min(uint64(10+publications), uint64(13)) || state.ClaimStates[0].Status != "ok" || state.VaultHeld || state.NativeHeld {
				t.Fatal("pending native producer stopped healthy public siblings", publications, state)
			}
			if publications <= 3 {
				if !state.NativePending || state.Native.Cursor != f.source.policy.Native.Observation.From || len(state.Lots) != 0 || producer.proofs.Load() != 0 {
					t.Fatal("unfinished actual producer invented native progress", publications, state.Native)
				}
			}
			if publications == 1 {
				economicNativeFollowAwait(t, entered, "actual original block request")
			}
			if publications == 3 {
				record := economicEntitlementRecord(t, f.source)
				if record.Census == nil || entitlement.artifactReads.Load() != 2 || len(state.Payments) != 1 {
					t.Fatal("original entitlement did not complete while native capture was pending", record)
				}
				originalCensus = record.Census.ContentHash
				release()
			}
			if publications == 4 {
				if state.NativePending || state.Native.Cursor.Number != 101 || state.Native.ExecutionProducer == nil || state.Native.ExecutionProducer.Completed != 1 || state.OpeningPrincipals == nil || len(state.Lots) != 2 || len(state.Payments) != 1 || economicEntitlementRecord(t, f.source).Census.ContentHash != originalCensus || producer.proofs.Load() == 0 {
					t.Fatal("actual capture/replay handoff rewound siblings or lost original principal", state)
				}
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			if publications == 2 {
				economicNativeFollowAwait(t, entitlementDone, "complete original artifact")
			}
			if publications == 3 {
				economicNativeFollowAwait(t, nativeDone, "actual original capture/replay")
				economicNativeFollowAwait(t, exited, "released producer request")
			}
			return ctx.Err() == nil
		},
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	if code != 0 || publications != 4 || bytes.Count(output.Bytes(), []byte{'\n'}) != 4 {
		t.Fatal("unattended public pipeline failed to join its original workers", code, publications, diagnostic.String())
	}
	var last economicConservationSummary
	rows := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	if err := decodePlanJson(rows[len(rows)-1], &last); err != nil {
		t.Fatal(err)
	}
	if !last.NativeCurrent || last.NativePending || last.TargetMet != nil || last.ActivationReady || last.OpeningPrincipalAlpha == nil || *last.OpeningPrincipalAlpha != "14" {
		t.Fatal("observed unattended progress fabricated current or economic authority", last)
	}
}

func TestEconomicConservationPublicNativeCaptureCancellationJoinsAfterSiblingPublication(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	entered, release, exited := economicNativeFollowBlock(t, f)
	defer release()
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	publications, closed := 0, 0
	hooks := monitorServiceHooks{
		afterEvent: func(_ context.Context, _ string) {
			publications++
			economicNativeFollowAwait(t, entered, "owned original capture request")
			state := f.source.state(t)
			if !state.NativePending || state.NativeHeld || state.Native.Cursor.Number != 100 || state.Vault.Cursor.Number != 11 || state.ClaimStates[0].Status != "ok" {
				t.Fatal("pending capture prevented its sibling publication", state)
			}
			cancel()
		},
		afterClose: func(_, _ string, file *os.File) error {
			closed++
			_, err := file.Stat()
			if !errors.Is(err, os.ErrClosed) {
				return errors.New("outer custody closed before owned native shutdown")
			}
			return nil
		},
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	economicNativeFollowAwait(t, exited, "canceled real HTTP request")
	if code != 0 || publications != 1 || closed != 1 || producer.proofs.Load() != 0 {
		t.Fatal("parent cancellation left an owned producer or lost healthy output", code, publications, closed, diagnostic.String())
	}
	release()
	result := f.sample(t, monitorServiceHooks{})
	if !result.NativeCurrent || result.NativePending || result.NativeCursor.Number != 101 || result.VaultCursor.Number != 12 || len(f.source.state(t).Lots) != 2 {
		t.Fatal("restart did not resume the original unaccounted native boundary", result)
	}
}

func TestEconomicConservationPublicNativeContradictionHoldsOnlyOriginalProducer(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	producer.corruptCertificate.Store(true)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan struct{})
	var once sync.Once
	publications, blocks := 0, int64(0)
	hooks := monitorServiceHooks{
		afterConservationNativeRead: func(_ context.Context, err error) {
			if err == nil || monitorEconomicNativeReadCode(err) != "identity-conflict" {
				t.Error("actual contradictory original certificate was not hard", err)
			}
			once.Do(func() { close(done) })
		},
		afterEvent: func(_ context.Context, _ string) {
			publications++
			state := f.source.state(t)
			if state.Vault.Cursor.Number != min(uint64(10+publications), uint64(13)) || state.VaultHeld || state.ClaimStates[0].Status != "ok" || state.Native.Cursor.Number != 100 {
				t.Fatal("native authority refusal changed healthy independent cursors", state)
			}
			if publications == 2 {
				if !state.NativeHeld || state.NativePending || state.NativeIssue == "" || len(state.Lots) != 0 {
					t.Fatal("completed native contradiction did not remain held", state)
				}
				blocks = producer.blocks.Load()
				producer.corruptCertificate.Store(false)
			}
			if publications == 3 {
				if !state.NativeHeld || producer.blocks.Load() != blocks || len(state.Payments) != 1 {
					t.Fatal("native contradiction silently re-enrolled original authority", state)
				}
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			if publications == 1 {
				economicNativeFollowAwait(t, done, "actual contradictory certificate")
			}
			return ctx.Err() == nil
		},
	}
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks); code != 0 || publications != 3 {
		t.Fatal("native quarantine stopped healthy follow lifetime", code, publications, diagnostic.String())
	}
}

func TestEconomicConservationPublicNativeCompletionLostAckReusesOriginalJob(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan struct{})
	var once sync.Once
	var lost atomic.Bool
	hooks := monitorServiceHooks{
		afterConservationNativeRead: func(_ context.Context, err error) {
			if err != nil {
				t.Error("actual producer before checkpoint acknowledgement", err)
			}
			once.Do(func() { close(done) })
		},
		syncDirectory: func(_, _ string, file *os.File) error {
			err := file.Sync()
			if err != nil {
				return err
			}
			state := f.source.state(t)
			if state.Native.ExecutionProducer != nil && state.Native.ExecutionProducer.Completed == 1 && lost.CompareAndSwap(false, true) {
				return syscall.EIO
			}
			return nil
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			economicNativeFollowAwait(t, done, "completed original job")
			return ctx.Err() == nil
		},
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	if code != 3 || !lost.Load() {
		t.Fatal("lost acknowledgement did not reach actual producer publication", code, diagnostic.String())
	}
	before := f.source.state(t)
	files := nativeProducerRestoreTestFiles(t, producer.source.policy.Execution.Directory)
	proofs, blocks := producer.proofs.Load(), producer.blocks.Load()
	result := f.sample(t, monitorServiceHooks{})
	after := f.source.state(t)
	if !result.NativeCurrent || !reflect.DeepEqual(before.Native.ExecutionProducer, after.Native.ExecutionProducer) || len(after.Lots) != len(before.Lots) || producer.proofs.Load() != proofs || producer.blocks.Load() != blocks || !reflect.DeepEqual(files, nativeProducerRestoreTestFiles(t, producer.source.policy.Execution.Directory)) {
		t.Fatal("lost checkpoint acknowledgement recaptured or reset original completion", result, before.Native, after.Native)
	}
}

// This is a real owned observation followed by a legitimate independent
// sample. Reusing the former native result must fail before candidate effects.
func TestEconomicConservationNativeHandoffRefusesChangedOriginalCursor(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	prior := newEconomicConservationState(f.policy)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	worker := newEconomicConservationNativeWorker(ctx, f.policy, f.native.client, monitorServiceHooks{})
	if err := worker.start(prior); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := worker.close(); err != nil {
			t.Error(err)
		}
	}()
	economicNativeFollowAwait(t, worker.done, "actual detached original observation")
	if _, code, issue := f.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("actual newer cursor baseline", code, issue)
	}
	current := f.state(t)
	before := rootObjectHash(current)
	vault, err := newRpcClient(f.vault.url, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer vault.httpClient.CloseIdleConnections()
	next, _, _, err := sampleEconomicConservationWithNativeWorker(ctx, f.policy, &current, f.native.client, vault, f.now, monitorServiceHooks{}, worker)
	if next != nil || err == nil || !strings.Contains(err.Error(), "original cursor or admitted policy") || rootObjectHash(current) != before {
		t.Fatal("stale native result rewound a completed original checkpoint", next, err)
	}
}

func TestEconomicConservationNativeHandoffKeepsLatestIndependentPublication(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	if _, code, issue := f.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("original public baseline", code, issue)
	}
	prior := f.state(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	worker := newEconomicConservationNativeWorker(ctx, f.policy, f.native.client, monitorServiceHooks{})
	if err := worker.start(&prior); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := worker.close(); err != nil {
			t.Error(err)
		}
	}()
	economicNativeFollowAwait(t, worker.done, "actual caught-up native observation")
	if _, code, issue := f.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("newer independent vault and Claim publication", code, issue)
	}
	current := f.state(t)
	if current.Vault.Cursor.Number != 12 || len(current.Payments) != 1 || current.Native.Cursor != prior.Native.Cursor {
		t.Fatal("handoff fixture did not advance only its sibling domains", current)
	}
	vault, err := newRpcClient(f.vault.url, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer vault.httpClient.CloseIdleConnections()
	next, nativeCurrent, vaultCurrent, err := sampleEconomicConservationWithNativeWorker(ctx, f.policy, &current, f.native.client, vault, f.now, monitorServiceHooks{}, worker)
	if err != nil || next == nil || !nativeCurrent || !vaultCurrent || next.NativePending || next.Native.Cursor != current.Native.Cursor || next.Vault.Cursor.Number != 13 || len(next.Payments) != 1 || rootObjectHash(next.Payments) != rootObjectHash(current.Payments) || rootObjectHash(next.Lots) != rootObjectHash(current.Lots) {
		t.Fatal("completed native handoff replaced the latest sibling facts", next, err)
	}
}

func TestEconomicConservationPublicTransientNativeReadRecoversOriginalBoundary(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	upstream, err := url.Parse(f.source.native.client.url)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var faults, recoveries atomic.Uint64
	faultEntered, faultRelease := make(chan struct{}), make(chan struct{})
	recoveryEntered, recoveryRelease := make(chan struct{}), make(chan struct{})
	var faultOnce, recoveryOnce sync.Once
	releaseFault := func() { faultOnce.Do(func() { close(faultRelease) }) }
	releaseRecovery := func() { recoveryOnce.Do(func() { close(recoveryRelease) }) }
	defer releaseFault()
	defer releaseRecovery()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if faults.CompareAndSwap(0, 1) {
			close(faultEntered)
			select {
			case <-faultRelease:
			case <-request.Context().Done():
				return
			}
			http.Error(w, "temporary archive outage", http.StatusServiceUnavailable)
			return
		}
		if recoveries.CompareAndSwap(0, 1) {
			close(recoveryEntered)
			select {
			case <-recoveryRelease:
			case <-request.Context().Done():
				return
			}
		}
		proxy.ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	f.source.native.client = client
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	results := make(chan error, 2)
	var waits atomic.Uint64
	publications := 0
	hooks := monitorServiceHooks{
		rpcWait: func(_ context.Context, role string, _ time.Duration) error {
			if role != f.source.policy.Native.Role {
				return errors.New("unexpected sibling transient in fixture")
			}
			waits.Add(1)
			return context.DeadlineExceeded
		},
		afterConservationNativeRead: func(_ context.Context, err error) { results <- err },
		afterEvent: func(_ context.Context, _ string) {
			publications++
			state := f.source.state(t)
			if state.NativeHeld || state.VaultHeld || state.Vault.Cursor.Number != min(uint64(10+publications), uint64(13)) || state.ClaimStates[0].Status != "ok" {
				t.Fatal("unavailable native read quarantined independent authority", publications, state)
			}
			if publications == 1 {
				economicNativeFollowAwait(t, faultEntered, "actual transient RPC")
				releaseFault()
			}
			if publications == 3 {
				economicNativeFollowAwait(t, recoveryEntered, "same-owner recovery RPC")
				if state.Native.Cursor.Number != 100 || !state.NativePending || state.NativeIssue == "" {
					t.Fatal("pending retry cleared its original unavailable observation", state)
				}
				releaseRecovery()
			}
			if publications == 2 && (state.Native.Cursor.Number != 100 || state.NativeIssue == "" || state.NativePending || waits.Load() != 1 || producer.proofs.Load() != 0) {
				t.Fatal("transient native read became a completed financial boundary", state)
			}
			if publications == 4 {
				if state.Native.Cursor.Number != 101 || state.NativeIssue != "" || state.NativePending || state.Native.ExecutionProducer == nil || state.Native.ExecutionProducer.Completed != 1 || len(state.Lots) != 2 || len(state.Payments) != 1 || faults.Load() != 1 {
					t.Fatal("unattended native retry did not recover the original cursor exactly once", state)
				}
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			if publications == 1 || publications == 3 {
				select {
				case err := <-results:
					if publications == 1 && !errors.Is(err, context.DeadlineExceeded) || publications == 3 && err != nil {
						t.Fatal("actual transient disposition or recovery differs", publications, err)
					}
				case <-time.After(2 * time.Minute):
					t.Fatal("owned native attempt did not complete")
				}
			}
			return ctx.Err() == nil
		},
	}
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks); code != 0 || publications != 4 {
		t.Fatal("public native retry stopped its retained owner", code, publications, diagnostic.String())
	}
}
