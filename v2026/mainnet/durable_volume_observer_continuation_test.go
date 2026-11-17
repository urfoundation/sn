// A signed finite passive service spends one sample on an observation outage,
// not all of its remaining samples. The physical owners stay joined to the run.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"golang.org/x/sys/unix"
)

func TestDurableCompositionPassiveUnavailableSampleContinuesBeforeRpc(t *testing.T) {
	compositionPassiveUnavailableSample(t, false, "recover")
}

func TestDurableCompositionPassiveUnavailableSampleContinuesAfterRpc(t *testing.T) {
	compositionPassiveUnavailableSample(t, true, "recover")
}

func TestDurableCompositionPassiveUnavailableSampleCancellationJoins(t *testing.T) {
	compositionPassiveUnavailableSample(t, false, "cancel")
}

func TestDurableCompositionPassiveUnavailableSampleIdentityLossStops(t *testing.T) {
	compositionPassiveUnavailableSample(t, false, "identity")
}

// The two-sample limit is approved before any local state is prepared. Tests
// cannot extend an already authenticated service's sample authority at runtime.
func compositionTwoSampleFixture(t *testing.T) (*bootstrapChainFixture, []string) {
	t.Helper()
	fixture := newBootstrapRootPassiveFixture(t)
	service := *fixture.root.plan.PassiveService
	service.MaximumSamples = 2
	fixture.root.config.RootService = bootstrapRootTestWrite(t, fixture.root.config.RootService.Path, service)
	fixture.config.Root = bootstrapRootTestWrite(t, fixture.root.configPath, fixture.root.config)
	var err error
	fixture.root.plan, err = loadBootstrapRootPlan(t.Context(), fixture.root.configPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.rootRole.approval.RootPlanHash = fixture.root.plan.ContentHash
	fixture.rootRole.approval.ServiceConfigHash = fixture.root.plan.serviceHash()
	fixture.rootRole.sign(t)
	bootstrapRootTestWrite(t, fixture.path, fixture.config)
	fixture.preparation, err = loadBootstrapChainPreparation(t.Context(), fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, compositionPassiveArguments(t, fixture)
}

// Each variant crosses the actual public two-sample command. Only the kernel
// fact observation and owned wait are injected; signing uses synthetic keys
// before preparation, and all root, lock, head and checkpoint bytes are real.
func compositionPassiveUnavailableSample(t *testing.T, afterRpc bool, transition string) {
	t.Helper()
	fixture, args := compositionTwoSampleFixture(t)
	service := fixture.root.plan.PassiveService
	methodCensus := func() map[string]int {
		fixture.census.stateLock.Lock()
		defer fixture.census.stateLock.Unlock()
		return maps.Clone(fixture.census.methodCounts)
	}
	methodDelta := func(after, before map[string]int) map[string]int {
		result := map[string]int{}
		for method, count := range after {
			if count < before[method] {
				t.Fatal("method census moved backwards", method)
			}
			if count != before[method] {
				result[method] = count - before[method]
			}
		}
		return result
	}
	// Independently measure one complete production preview's method vector.
	// Several finalized-head reads belong to that one bounded preview. The
	// public command must do zero extra work during the outage, then exactly
	// this vector once for its single recovered sample.
	beforeReference := methodCensus()
	referenceClient, err := newRpcClient(service.RpcUrl, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := referenceClient.readRootPreview(t.Context(), service.Policy, rootObjectHash(service.Policy))
	referenceClient.httpClient.CloseIdleConnections()
	if err != nil || !reference.ReadOnlyReady {
		t.Fatal("reference complete preview unavailable", err)
	}
	beforeMethods := methodCensus()
	expectedMethods := methodDelta(beforeMethods, beforeReference)
	if expectedMethods["chain_getFinalizedHead"] == 0 {
		t.Fatal("reference preview omitted finalized identity")
	}
	beforeReads := compositionRpcReads(fixture.census)
	path := filepath.Join(fixture.root.plan.RunDirectory, bootstrapRootProgressFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	var preparationFile, checkpointFile *os.File
	var preparationIdentity, checkpointIdentity os.FileInfo
	failed, recovered := false, false
	failedSampleReads := -1
	var failedSampleMethods map[string]int
	host := &compositionObservationHost{Host: fixture.root.storage.Host}
	host.observe = func(file *os.File) error {
		if preparationFile == nil {
			preparationFile = file
			preparationIdentity, err = file.Stat()
			if err != nil {
				t.Fatal(err)
			}
		} else if file != preparationFile && checkpointFile == nil {
			checkpointFile = file
			checkpointIdentity, err = file.Stat()
			if err != nil {
				t.Fatal(err)
			}
		}
		boundary := checkpointFile != nil
		if afterRpc {
			boundary = compositionRpcReads(fixture.census) > beforeReads
		}
		if file == preparationFile && boundary && !recovered {
			if !failed {
				// A complete root preview may check the finalized head more
				// than once. The outage must add no reads after this exact
				// completed preview boundary, rather than assuming its size.
				failedSampleReads = compositionRpcReads(fixture.census)
				failedSampleMethods = methodCensus()
			}
			failed = true
			return unix.EIO
		}
		return nil
	}
	checkRetainedOwners := func() {
		t.Helper()
		if preparationFile == nil || checkpointFile == nil {
			t.Fatal("sample did not retain both physical owners")
		}
		for _, owner := range []struct {
			file     *os.File
			identity os.FileInfo
		}{{file: preparationFile, identity: preparationIdentity}, {file: checkpointFile, identity: checkpointIdentity}} {
			current, err := owner.file.Stat()
			if err != nil || !os.SameFile(current, owner.identity) {
				t.Fatal("sample replaced or closed its retained owner", err)
			}
		}
	}
	waits, events := 0, 0
	var output, diagnostics bytes.Buffer
	delivery := &monitorFixtureOutput{writer: &output, completed: make(chan struct{}, 1)}
	hooks := monitorServiceHooks{
		wait: func(waitCtx context.Context, role string, delay time.Duration) bool {
			waits++
			if !failed || waitCtx.Err() != nil || role != "root" || delay <= 0 || delay > 30*time.Second {
				t.Fatal("invalid bounded sample wait", failed, waitCtx.Err(), role, delay)
			}
			checkRetainedOwners()
			if failedSampleMethods == nil || !maps.Equal(methodCensus(), failedSampleMethods) {
				t.Fatal("unavailable sample issued Rpc calls during its retained wait")
			}
			if events == 0 {
				if waits > 64 || recovered {
					t.Fatal("sample exceeded its retry budget", waits, recovered)
				}
			} else if events == 1 {
				if waits != 65 || delay != time.Second {
					t.Fatal("soft failure skipped the signed inter-sample interval", waits, delay)
				}
				if transition == "cancel" {
					cancel()
					return false
				}
				recovered = true
				if transition == "identity" {
					if err := os.Rename(path, path+".retained"); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				t.Fatal("command exceeded its signed two-sample allowance", events)
			}
			return true
		},
		afterEvent: func(ctx context.Context, _ string) {
			select {
			case <-delivery.completed:
			case <-ctx.Done():
				t.Fatal("sample canceled before its bounded diagnostic delivery")
			}
			events++
			checkRetainedOwners()
			if events == 1 {
				if waits != 64 {
					t.Fatal("failed sample was not bounded", waits)
				}
				if _, err := os.Lstat(service.CheckpointPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("unavailable preparation changed finalized checkpoint custody", err)
				}
				if failedSampleReads < beforeReads || !afterRpc && failedSampleReads != beforeReads || afterRpc && failedSampleReads == beforeReads {
					t.Fatal("outage missed its exact network boundary", beforeReads, failedSampleReads)
				}
				if reads := compositionRpcReads(fixture.census); reads != failedSampleReads {
					t.Fatal("unavailable sample invented an extra network observation", reads, failedSampleReads)
				}
				if !maps.Equal(methodCensus(), failedSampleMethods) {
					t.Fatal("failed sample changed the exact observed method vector")
				}
				wantMethods := map[string]int{}
				if afterRpc {
					wantMethods = expectedMethods
				}
				if got := methodDelta(failedSampleMethods, beforeMethods); !maps.Equal(got, wantMethods) {
					t.Fatal("failed sample did not stop at its exact complete-preview boundary", got, wantMethods)
				}
			} else if got := methodDelta(methodCensus(), failedSampleMethods); !maps.Equal(got, expectedMethods) {
				t.Fatal("recovered sample issued more than one complete preview", got, expectedMethods)
			}
		},
	}
	ctx := durablepath.WithHost(fixture.storageContext(parent), host)
	code := runMainWithMonitorHooks(ctx, args, delivery, &diagnostics, time.Now, hooks)
	wantCode, wantEvents := 0, 2
	if transition == "cancel" {
		wantEvents = 1
	} else if transition == "identity" {
		wantCode, wantEvents = 3, 1
	}
	if code != wantCode || waits != 65 || events != wantEvents || !failed {
		t.Fatal("soft preparation outage abandoned unused signed samples or custody", code, waits, events, failed, diagnostics.String())
	}
	for _, file := range []*os.File{preparationFile, checkpointFile} {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("terminal public run did not join its physical owners", err)
		}
	}
	decoder := json.NewDecoder(&output)
	var first rootMonitorEvent
	if err := decoder.Decode(&first); err != nil || first.Sample != 1 || first.Status != "storage-unavailable" || first.Observation != nil || first.Snapshot != nil || first.ReadPhase != "preparation" || first.ReadCause != "unavailable" {
		t.Fatal("failed sample invented freshness or omitted its bounded cause", first, err)
	}
	metrics := string(renderRootMonitorMetrics(first, &monitorState{}, "synthetic-root", "unconfigured", time.Time{}))
	for _, value := range []string{"sn_mainnet_root_monitor_status{role=\"synthetic-root\"} 8\n", "sn_mainnet_root_monitor_current_observation{role=\"synthetic-root\"} 0\n", "sn_mainnet_root_monitor_read_only_ready{role=\"synthetic-root\"} 0\n", "sn_mainnet_root_monitor_finalized_block{role=\"synthetic-root\"} 0\n"} {
		if !strings.Contains(metrics, value) {
			t.Fatal("storage outage telemetry fabricated accepted progress", value)
		}
	}
	if transition == "recover" {
		var second rootMonitorEvent
		if err := decoder.Decode(&second); err != nil || second.Sample != 2 || second.Status != "ready" || second.Observation == nil || !second.Observation.ReadOnlyReady {
			t.Fatal("next signed sample did not recover on retained custody", second, err)
		}
		if raw, err := os.ReadFile(service.CheckpointPath); err != nil || len(raw) == 0 {
			t.Fatal("recovered sample did not retain its finalized checkpoint", err)
		}
	} else if _, err := os.Lstat(service.CheckpointPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("terminal cancellation or loss published a checkpoint", err)
	}
	if err := decoder.Decode(&rootMonitorEvent{}); !errors.Is(err, io.EOF) {
		t.Fatal("command exceeded its signed sample allowance", err)
	}
	if transition == "identity" {
		path += ".retained"
	}
	if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(before, raw) {
		t.Fatal("sample continuation changed original preparation bytes", err)
	}
}

func TestDurableCompositionPassiveUnavailableCannotHideRpcIntegrity(t *testing.T) {
	compositionPassiveUnavailableAfterFatal(t, true)
}

func TestDurableCompositionPassiveUnavailableCannotRetryUncertainCheckpoint(t *testing.T) {
	compositionPassiveUnavailableAfterFatal(t, false)
}

// A real wrong-chain response or lost acknowledgement from a real checkpoint
// sync already requires this owner to stop. A concurrent observation outage
// cannot consume another signed sample or reopen uncertain writer custody.
func compositionPassiveUnavailableAfterFatal(t *testing.T, integrity bool) {
	t.Helper()
	fixture, args := compositionTwoSampleFixture(t)
	beforeReads := fixture.census.count("system_chain")
	if integrity {
		fixture.census.evmChainHex = "0x3b1"
	}
	var preparationFile *os.File
	uncertain := false
	host := &compositionObservationHost{Host: fixture.root.storage.Host}
	host.observe = func(file *os.File) error {
		if preparationFile == nil {
			preparationFile = file
		}
		if file == preparationFile && (uncertain || integrity && fixture.census.count("system_chain") > beforeReads) {
			return unix.EIO
		}
		return nil
	}
	waits, events, writes := 0, 0, 0
	hooks := monitorServiceHooks{
		syncDirectory: func(role, kind string, file *os.File) error {
			if integrity || role != "root" || kind != "checkpoint" || writes != 0 {
				t.Fatal("unexpected physical publication after terminal evidence", role, kind, writes)
			}
			if err := file.Sync(); err != nil {
				t.Fatal(err)
			}
			writes++
			uncertain = true
			return unix.EIO
		},
		wait: func(ctx context.Context, role string, delay time.Duration) bool {
			waits++
			if ctx.Err() != nil || role != "root" || delay <= 0 || delay > 30*time.Second || waits > 64 || events != 0 {
				t.Fatal("soft outage continued after independently terminal evidence", waits, events, ctx.Err())
			}
			return true
		},
		afterEvent: func(context.Context, string) { events++ },
	}
	ctx := durablepath.WithHost(fixture.storageContext(t.Context()), host)
	var output, diagnostics bytes.Buffer
	code := runMainWithMonitorHooks(ctx, args, &output, &diagnostics, time.Now, hooks)
	wantCode, wantWrites := 1, 1
	if integrity {
		wantCode, wantWrites = 3, 0
	}
	if code != wantCode || writes != wantWrites || events != 0 || waits != 64 || fixture.census.count("system_chain") != beforeReads+1 {
		t.Fatal("observation unavailability erased a terminal result", code, writes, events, waits, diagnostics.String())
	}
	if _, err := preparationFile.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("terminal evidence did not join its retained preparation", err)
	}
	if integrity {
		if _, err := os.Lstat(fixture.root.plan.PassiveService.CheckpointPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("wrong-chain response created a checkpoint", err)
		}
	} else if raw, err := os.ReadFile(fixture.root.plan.PassiveService.CheckpointPath); err != nil || len(raw) == 0 {
		t.Fatal("lost acknowledgement discarded actual retained bytes", err)
	}
}
