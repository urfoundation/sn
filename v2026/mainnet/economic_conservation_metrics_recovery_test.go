// These controls inject actual textfile directory-sync failures after the
// financial checkpoint has been acknowledged. They keep the public command,
// original retained custody and real native/vault/Claim readers in the path.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
)

func TestEconomicMetricsFollowInitialAvailabilityAllowsSameOwnerRestart(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	ctx = monitorTestStorageContext(t, ctx, args)
	var output, diagnostic bytes.Buffer
	syncs, closed := 0, 0
	hooks := monitorServiceHooks{
		syncDirectory: func(role, kind string, file *os.File) error {
			if role == economicConservationRole && kind == "metrics" {
				syncs++
				return errors.Join(file.Sync(), syscall.EIO)
			}
			return file.Sync()
		},
		afterClose: func(role, kind string, file *os.File) error {
			if role == economicConservationRole && (kind == "metrics" || kind == "checkpoint") {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("startup metrics retry did not join its original owner")
				}
				closed++
			}
			return nil
		},
	}
	code := runMainWithMonitorHooks(ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 1 || syncs != 1 || closed != 2 || output.Len() != 0 || f.claimReads.Load() != 0 || !strings.Contains(diagnostic.String(), syscall.EIO.Error()) {
		t.Fatal("startup availability became a permanent hold or started reads", code, syncs, closed, diagnostic.String())
	}
	if _, err := os.Stat(f.checkpoint); !os.IsNotExist(err) {
		t.Fatal("failed metrics admission published financial progress", err)
	}
	if economicMetricsTestFile(t, path)["sample_timestamp_seconds"] != 0 {
		t.Fatal("unobserved startup acquired a current metrics sample")
	}
	diagnostic.Reset()
	code = runMainWithMonitorHooks(ctx, args, &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{
		rpcWait: economicConservationTestWait,
		wait:    func(context.Context, string, time.Duration) bool { return false },
	})
	if code != 0 || bytes.Count(output.Bytes(), []byte{'\n'}) != 1 || f.state(t).Native.Cursor.Number != 102 || economicMetricsTestFile(t, path)["sample_healthy"] != 1 {
		t.Fatal("same original custody did not recover from metrics admission I/O", code, diagnostic.String())
	}
}

func TestEconomicMetricsFollowAvailabilityKeepsCommittedProgress(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var output, diagnostic bytes.Buffer
	opened, closed, samples, syncs, events := 0, 0, 0, 0, 0
	var fault error
	var originalCapture string
	var originalLots, originalCaptures int
	hooks := monitorServiceHooks{
		rpcWait: economicConservationTestWait,
		afterCheckpointOpen: func(_ context.Context, role string, _ *os.File) {
			if role == economicConservationRole {
				opened++
			}
		},
		syncDirectory: func(role, kind string, file *os.File) error {
			if role == economicConservationRole && kind == "metrics" {
				syncs++
				return errors.Join(file.Sync(), fault)
			}
			return file.Sync()
		},
		afterEvent: func(_ context.Context, role string) {
			if role == economicConservationRole {
				events++
			}
		},
		wait: func(owner context.Context, role string, interval time.Duration) bool {
			if role != economicConservationRole || interval != time.Second {
				t.Error("metrics retry changed the original bounded sample schedule", role, interval)
				return false
			}
			samples++
			state := f.state(t)
			if state.Native.Cursor.Number != 102 || state.Vault.Cursor.Number != min(uint64(10+samples), uint64(13)) || state.NativeHeld || state.VaultHeld || state.ClaimStates[0].Status != "ok" {
				t.Error("metrics I/O stopped independent completed financial reads", samples, state.Native.Cursor, state.Vault.Cursor, state.ClaimStates)
				return false
			}
			if samples == 1 {
				if len(state.Captures) == 0 {
					t.Error("positive control did not retain an original capture")
					return false
				}
				originalCapture, originalLots, originalCaptures = state.Captures[0].Id, len(state.Lots), len(state.Captures)
			} else if len(state.Lots) != originalLots || len(state.Captures) != originalCaptures || state.Captures[0].Id != originalCapture {
				t.Error("metrics retry replayed or replaced original financial effects", samples)
				return false
			}
			wantEvents := 1
			if samples == 4 {
				wantEvents = 2
			}
			if events != wantEvents || bytes.Count(output.Bytes(), []byte{'\n'}) != wantEvents {
				t.Error("failed textfile acknowledgement emitted a success event", samples, events, output.String())
				return false
			}
			switch samples {
			case 1:
				fault = syscall.EIO
			case 2:
				fault = syscall.ENOSPC
			case 3:
				fault = nil
			case 4:
				cancel()
				return false
			default:
				t.Error("metrics recovery exceeded its explicit sample bound")
				return false
			}
			return owner.Err() == nil
		},
		afterClose: func(role, kind string, file *os.File) error {
			if role == economicConservationRole && (kind == "metrics" || kind == "checkpoint") {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("metrics recovery retained an unjoined owner")
				}
				closed++
			}
			return nil
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 0 || opened != 1 || closed != 2 || samples != 4 || syncs != 5 || events != 2 || f.claimReads.Load() != 4 {
		t.Fatal("follow metrics availability restarted or stopped original readers", code, opened, closed, samples, syncs, events, f.claimReads.Load(), diagnostic.String())
	}
	if !strings.Contains(diagnostic.String(), syscall.EIO.Error()) || !strings.Contains(diagnostic.String(), syscall.ENOSPC.Error()) {
		t.Fatal("metrics availability causes disappeared", diagnostic.String())
	}
	values := economicMetricsTestFile(t, path)
	if values["vault_finalized_block"] != 13 || values["native_finalized_block"] != 102 || values["sample_healthy"] != 1 || values["target_known"] != 0 {
		t.Fatal("recovered metrics lost original progress or fabricated conformance", values)
	}
	decoder := json.NewDecoder(&output)
	var first, recovered economicConservationSummary
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&recovered); err != nil {
		t.Fatal(err)
	}
	if first.Progress == nil || recovered.Progress == nil || first.Progress.SampleAttempts != 1 || recovered.Progress.SampleAttempts != 4 || first.Progress.ProcessId != recovered.Progress.ProcessId || !first.Progress.StartedAt.Equal(recovered.Progress.StartedAt) || recovered.Progress.CheckpointHash != recovered.CheckpointHash {
		t.Fatal("metrics I/O recreated the operational owner or lost acknowledged attempts", first.Progress, recovered.Progress)
	}
}

func TestEconomicMetricsFollowMetricsOwnershipLossRemainsTerminal(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var output, diagnostic bytes.Buffer
	syncs, waits := 0, 0
	hooks := monitorServiceHooks{
		rpcWait: economicConservationTestWait,
		syncDirectory: func(role, kind string, file *os.File) error {
			if role != economicConservationRole || kind != "metrics" {
				return file.Sync()
			}
			syncs++
			if syncs != 2 {
				return file.Sync()
			}
			lock := path + ".lock"
			raw, err := os.ReadFile(lock)
			if err != nil {
				return err
			}
			if err := os.Rename(lock, lock+".original"); err != nil {
				return err
			}
			if err := os.WriteFile(lock, raw, 0600); err != nil {
				return err
			}
			return errors.Join(file.Sync(), syscall.EIO)
		},
		wait: func(context.Context, string, time.Duration) bool {
			waits++
			return false
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 3 || syncs != 2 || waits != 0 || output.Len() != 0 || f.state(t).Native.Cursor.Number != 102 || !strings.Contains(diagnostic.String(), "metrics lock changed") || !strings.Contains(diagnostic.String(), syscall.EIO.Error()) {
		t.Fatal("known I/O hid actual equal-byte ownership replacement", code, syncs, waits, diagnostic.String())
	}
}

func TestEconomicMetricsFollowMixedMetricsFailureRemainsTerminal(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		cancel bool
	}{
		{name: "unknown", cause: errors.New("synthetic completed publication refusal")},
		{name: "uncertain", cause: durablehead.ErrUncertain},
		{name: "canceled-unknown", cause: errors.New("synthetic completed publication refusal"), cancel: true},
	} {
		f := newEconomicConservationFixture(t, false)
		path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
		args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		var output, diagnostic bytes.Buffer
		syncs, waits := 0, 0
		hooks := monitorServiceHooks{
			rpcWait: economicConservationTestWait,
			syncDirectory: func(role, kind string, file *os.File) error {
				if role == economicConservationRole && kind == "metrics" {
					syncs++
					if syncs == 2 {
						if test.cancel {
							cancel()
						}
						return errors.Join(file.Sync(), syscall.EIO, test.cause)
					}
				}
				return file.Sync()
			},
			wait: func(context.Context, string, time.Duration) bool {
				waits++
				return false
			},
		}
		code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
		cancel()
		if code != 3 || syncs != 2 || waits != 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), test.cause.Error()) || !strings.Contains(diagnostic.String(), syscall.EIO.Error()) {
			t.Fatal("availability or cancellation erased a completed hard cause", test.name, code, syncs, waits, diagnostic.String())
		}
	}
}

func TestEconomicMetricsFollowCheckpointPublicationRemainsHeld(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var output, diagnostic bytes.Buffer
	checkpointSyncs, metricSyncs, waits := 0, 0, 0
	hooks := monitorServiceHooks{
		rpcWait: economicConservationTestWait,
		syncDirectory: func(role, kind string, file *os.File) error {
			if role == economicConservationRole {
				if kind == "checkpoint" {
					checkpointSyncs++
					return errors.Join(file.Sync(), syscall.EIO)
				}
				if kind == "metrics" {
					metricSyncs++
				}
			}
			return file.Sync()
		},
		wait: func(context.Context, string, time.Duration) bool {
			waits++
			return false
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 3 || checkpointSyncs == 0 || metricSyncs != 1 || waits != 0 || output.Len() != 0 || economicMetricsTestFile(t, path)["sample_timestamp_seconds"] != 0 || !strings.Contains(diagnostic.String(), syscall.EIO.Error()) {
		t.Fatal("metrics recovery admitted an uncertain financial checkpoint", code, checkpointSyncs, metricSyncs, waits, diagnostic.String())
	}
}

func TestEconomicMetricsFollowCancellationJoinsMetricsRetry(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var output, diagnostic bytes.Buffer
	syncs, closed, waits := 0, 0, 0
	hooks := monitorServiceHooks{
		rpcWait: economicConservationTestWait,
		syncDirectory: func(role, kind string, file *os.File) error {
			if role == economicConservationRole && kind == "metrics" {
				syncs++
				if syncs == 2 {
					return errors.Join(file.Sync(), syscall.EIO)
				}
			}
			return file.Sync()
		},
		wait: func(context.Context, string, time.Duration) bool {
			waits++
			cancel()
			return false
		},
		afterClose: func(role, kind string, file *os.File) error {
			if role == economicConservationRole && (kind == "metrics" || kind == "checkpoint") {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("canceled metrics retry did not join publication owners")
				}
				closed++
			}
			return nil
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 0 || syncs != 2 || waits != 1 || closed != 2 || output.Len() != 0 || f.claimReads.Load() != 1 || f.state(t).Native.Cursor.Number != 102 || !strings.Contains(diagnostic.String(), syscall.EIO.Error()) {
		t.Fatal("canceled metrics retry published late output or lost original progress", code, syncs, waits, closed, diagnostic.String())
	}
}
