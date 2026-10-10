// Public command controls distinguish refused admission from a retryable read.
// Real filesystem faults and completed output fence every state transition.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
)

// A joined command must retain every healthy observation without promoting
// optional publication state into activation or full-snapshot authority.
func rootMonitorAdmissionEvents(t *testing.T, stdout *bytes.Buffer, publications []string) {
	t.Helper()
	decoder := json.NewDecoder(stdout)
	for index, publication := range publications {
		var event rootMonitorEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Schema != rootDiagnosticEventSchema || event.Sample != index+1 || event.Status != "ready" || event.Publication != publication || event.Observation == nil || !event.Observation.ReadOnlyReady || event.Observation.ActivationReady || event.Snapshot != nil {
			t.Fatalf("admission sample %d: publication=%q, want %q; status=%q observation=%+v", index+1, event.Publication, publication, event.Status, event.Observation)
		}
	}
	var extra rootMonitorEvent
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatal("admission command emitted an extra or malformed sample", err)
	}
}

// Fixing a hard initial refusal cannot authorize a publisher which this
// invocation permanently disabled. Both typed ownership and mode refusals stay
// unavailable, preserve retained bytes and release any partially opened lock.
func TestRootMonitorPublicationInitialHardRefusalRemainsUnavailable(t *testing.T) {
	for _, unsafeDirectory := range []bool{false, true} {
		f := newRootMonitorStartupFixture(t, "retained")
		server := rootFixtureServer(t, f.rpc)
		retained := []byte("synthetic retained admission output\n")
		if unsafeDirectory {
			if err := os.WriteFile(f.metrics, retained, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Dir(f.metrics), 0770); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Mkdir(f.metrics, 0700); err != nil {
			t.Fatal(err)
		}
		refused, err := openMonitorMetrics(f.metrics, f.checkpoint.storage.Context)
		if refused != nil {
			t.Fatal("hard fixture admitted a metrics owner", errors.Join(err, refused.close()))
		}
		var ownership *monitorOutputOwnershipError
		if err == nil || rootMonitorStartupPending(err) || errors.As(err, &ownership) != !unsafeDirectory {
			t.Fatal("hard fixture reached the wrong admission class", unsafeDirectory, err)
		}
		ctx, cancel := context.WithTimeout(f.checkpoint.storage.Context, 30*time.Second)
		defer cancel()
		var stdout bytes.Buffer
		output := &monitorFixtureOutput{writer: &stdout, completed: make(chan struct{}, 1)}
		samples, metricCloses := 0, 0
		hooks := monitorServiceHooks{
			afterEvent: func(ctx context.Context, _ string) {
				select {
				case <-output.completed:
				case <-ctx.Done():
					t.Error("hard refusal event did not complete", ctx.Err())
					return
				}
				samples++
				if unsafeDirectory {
					raw, err := os.ReadFile(f.metrics)
					if err != nil || !bytes.Equal(raw, retained) {
						t.Fatal("hard admission replaced retained metrics", err)
					}
					if samples == 1 {
						if err := os.Chmod(filepath.Dir(f.metrics), 0700); err != nil {
							t.Fatal(err)
						}
					}
				} else if samples == 1 {
					entries, err := os.ReadDir(f.metrics)
					if err != nil || len(entries) != 0 {
						t.Fatal("hard admission modified refused directory", err)
					}
					if err := os.Remove(f.metrics); err != nil {
						t.Fatal(err)
					}
				} else if _, err := os.Lstat(f.metrics); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("disabled publisher adopted a repaired target", err)
				}
			},
			afterClose: func(_ string, kind string, _ *os.File) error {
				if kind == "metrics" {
					metricCloses++
				}
				return nil
			},
			wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil },
		}
		args := append(f.arguments(server.URL), "--samples", "3", "--metrics-file", f.metrics, "--metrics-role", "root-a")
		code := runMainWithMonitorHooks(ctx, args, output, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
		if code != 0 || samples != 3 || metricCloses != 0 || f.rpc.count("chain_getFinalizedHead") != 12 {
			t.Fatal("hard optional admission changed sampling or retained an owner", unsafeDirectory, code, samples, metricCloses)
		}
		rootMonitorAdmissionEvents(t, &stdout, []string{"unavailable", "unavailable", "unavailable"})
		f.checkpoint.unchanged(t)
		store, err := openMonitorMetrics(f.metrics, ctx)
		if err != nil {
			t.Fatal("hard admission failed to release its rejected owner", err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
}

// Actual EIO at initial admission and the first sample may recover at the next
// sample. The written acknowledgment lags success, and the late owner closes
// once without replacing the independently retained finalized checkpoint.
func TestRootMonitorPublicationSoftAdmissionRecoversWithLaggedAcknowledgment(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	failing, faults, samples, metricCloses := true, 0, 0, 0
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(file *os.File) error {
		if file.Name() == filepath.Dir(f.metrics) && failing {
			if _, err := file.Stat(); err != nil {
				return err
			}
			faults++
			return syscall.EIO
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(durablepath.WithHost(f.checkpoint.storage.Context, host), 30*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	output := &monitorFixtureOutput{writer: &stdout, completed: make(chan struct{}, 1)}
	hooks := monitorServiceHooks{
		afterEvent: func(ctx context.Context, _ string) {
			select {
			case <-output.completed:
			case <-ctx.Done():
				t.Error("soft admission event did not complete", ctx.Err())
				return
			}
			samples++
			if samples == 1 {
				if faults < 2 {
					t.Fatal("soft fault did not reach initial and sampled admission", faults)
				}
				if _, err := os.Lstat(f.metrics); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed admission fabricated a metrics file", err)
				}
				failing = false
				return
			}
			values := monitorOutputTestMetrics(t, f.metrics)
			wantStatus, wantAcknowledged := float64(3), float64(0)
			if samples == 3 {
				wantStatus, wantAcknowledged = 2, float64(f.checkpoint.stamp.Unix())
			}
			for name, want := range map[string]float64{
				"current_observation": 1, "read_only_ready": 1,
				"publication_status": wantStatus, "publication_last_success_timestamp_seconds": wantAcknowledged,
				"last_read_success_timestamp_seconds":  float64(f.checkpoint.stamp.Unix()),
				"finalized_progress_timestamp_seconds": float64(f.checkpoint.stamp.Unix()),
			} {
				key := "sn_mainnet_root_monitor_" + name + "{role=\"root-a\"}"
				if got, present := values[key]; !present || got != want {
					t.Fatalf("sample %d metric %s=%v present=%v, want %v", samples, key, got, present, want)
				}
			}
		},
		afterClose: func(_ string, kind string, file *os.File) error {
			if kind == "metrics" {
				metricCloses++
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.Join(errors.New("recovered publisher remained open"), err)
				}
			}
			return nil
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil },
	}
	args := append(f.arguments(server.URL), "--samples", "3", "--metrics-file", f.metrics, "--metrics-role", "root-a")
	code := runMainWithMonitorHooks(ctx, args, output, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if code != 0 || samples != 3 || metricCloses != 1 || faults < 2 || f.rpc.count("chain_getFinalizedHead") != 12 {
		t.Fatal("soft admission did not recover and join exactly one owner", code, samples, metricCloses, faults)
	}
	rootMonitorAdmissionEvents(t, &stdout, []string{"retrying", "published", "published"})
	f.checkpoint.unchanged(t)
}

// A retry which encounters a real ownership refusal terminates publication.
// Removing that later collision does not reopen admission or falsify sampling.
func TestRootMonitorPublicationLaterHardRefusalDisablesReadmission(t *testing.T) {
	f := newRootMonitorStartupFixture(t, "retained")
	server := rootFixtureServer(t, f.rpc)
	failing, faults, samples, metricCloses := true, 0, 0, 0
	host := &compositionObservationHost{Host: f.checkpoint.storage.Host, observe: func(file *os.File) error {
		if file.Name() == filepath.Dir(f.metrics) && failing {
			if _, err := file.Stat(); err != nil {
				return err
			}
			faults++
			return syscall.EIO
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(durablepath.WithHost(f.checkpoint.storage.Context, host), 30*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	output := &monitorFixtureOutput{writer: &stdout, completed: make(chan struct{}, 1)}
	hooks := monitorServiceHooks{
		afterEvent: func(ctx context.Context, _ string) {
			select {
			case <-output.completed:
			case <-ctx.Done():
				t.Error("later refusal event did not complete", ctx.Err())
				return
			}
			samples++
			switch samples {
			case 1:
				if faults < 2 {
					t.Fatal("later refusal never followed actual admission retries", faults)
				}
				failing = false
				if err := os.Mkdir(f.metrics, 0700); err != nil {
					t.Fatal(err)
				}
			case 2:
				entries, err := os.ReadDir(f.metrics)
				if err != nil || len(entries) != 0 {
					t.Fatal("later hard refusal modified the collided target", err)
				}
				if err := os.Remove(f.metrics); err != nil {
					t.Fatal(err)
				}
			case 3:
				if _, err := os.Lstat(f.metrics); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("hard-disabled publisher retried after collision removal", err)
				}
			}
		},
		afterClose: func(_ string, kind string, _ *os.File) error {
			if kind == "metrics" {
				metricCloses++
			}
			return nil
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil },
	}
	args := append(f.arguments(server.URL), "--samples", "3", "--metrics-file", f.metrics, "--metrics-role", "root-a")
	code := runMainWithMonitorHooks(ctx, args, output, io.Discard, func() time.Time { return f.checkpoint.stamp }, hooks)
	if code != 0 || samples != 3 || metricCloses != 0 || faults < 2 || f.rpc.count("chain_getFinalizedHead") != 12 {
		t.Fatal("later refusal changed observation or retained a rejected owner", code, samples, metricCloses, faults)
	}
	rootMonitorAdmissionEvents(t, &stdout, []string{"retrying", "ownership-error", "ownership-error"})
	f.checkpoint.unchanged(t)
	store, err := openMonitorMetrics(f.metrics, ctx)
	if err != nil {
		t.Fatal("later admission failed to release its rejected owner", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
}
