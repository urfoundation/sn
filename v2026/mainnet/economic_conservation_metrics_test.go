// These public controls use real checkpoint/metrics owners and completed-sample
// barriers. They do not turn synthetic amount observations into launch authority.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Parse the numeric text contract independently of its renderer.
func readEconomicMetricsTest(t *testing.T, raw []byte) map[string]uint64 {
	t.Helper()
	values := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "sn_mainnet_conservation_") || strings.ContainsAny(fields[0], "{}\"") {
			t.Fatal("economic metrics disclosed an unbounded label or invalid record", line)
		}
		name := strings.TrimPrefix(fields[0], "sn_mainnet_conservation_")
		if _, exists := values[name]; exists {
			t.Fatal("duplicate economic metric", name)
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		values[name] = value
	}
	if len(values) != 72 || len(raw) > 16*1024 || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("economic metrics escaped their exact finite shape", len(values), len(raw))
	}
	return values
}

func economicMetricsTestFile(t *testing.T, path string) map[string]uint64 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return readEconomicMetricsTest(t, raw)
}

func TestEconomicMetricsUnknownConformanceCannotBecomeHealthyAcceptance(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	summary := economicConservationSummary{Schema: "urnetwork-economic-conservation-sample-v1", PolicyHash: "sha256:" + strings.Repeat("1", 64), CheckpointHash: "sha256:" + strings.Repeat("2", 64), SampleAt: now, NativeCurrent: true, VaultCurrent: true, ClaimStatuses: []string{"ok"}, NativeCursor: economicEmissionBoundary{Number: 101}, VaultCursor: economicEmissionBoundary{Number: 11}}
	for _, state := range []string{"unknown", "false", "true", "held", "unknown-again"} {
		summary.TargetMet = nil
		summary.Conformance = nil
		summary.NativeHeld = false
		if state == "false" || state == "true" {
			met := state == "true"
			summary.TargetMet = &met
			summary.Conformance = &economicConservationConformance{CompleteEvidence: met}
		}
		if state == "held" {
			summary.NativeHeld = true
		}
		raw, err := renderEconomicConservationMetrics(&summary)
		if err != nil {
			t.Fatal(err)
		}
		values := readEconomicMetricsTest(t, raw)
		known, met, healthy := uint64(0), uint64(0), uint64(1)
		if state == "false" || state == "true" {
			known = 1
		}
		if state == "true" {
			met = 1
		}
		if state == "held" {
			healthy = 0
		}
		if values["target_known"] != known || values["target_met"] != met || values["complete_evidence"] != met || values["sample_healthy"] != healthy || values["sample_timestamp_seconds"] != uint64(now.Unix()) {
			t.Fatal("economic metrics reused or invented conformance", state, values)
		}
	}
	summary.SampleAt = time.Time{}
	if _, err := renderEconomicConservationMetrics(&summary); err == nil {
		t.Fatal("unobserved economic summary acquired a fresh metric")
	}
}

func TestEconomicMetricsPublicFollowPublishesOutageAndRecoveryBeforeOutput(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output, diagnostic bytes.Buffer
	var samples int
	var original int64
	hooks := monitorServiceHooks{
		rpcWait: economicConservationTestWait,
		afterEvent: func(_ context.Context, role string) {
			if role != economicConservationRole {
				return
			}
			samples++
			values := economicMetricsTestFile(t, path)
			if values["sample_timestamp_seconds"] != uint64(f.now.Unix()) || values["target_known"] != 0 || values["target_met"] != 0 {
				t.Fatal("public output preceded original bounded metrics", values)
			}
			switch samples {
			case 1:
				original = f.now.Unix()
				if values["sample_healthy"] != 1 {
					t.Fatal("original complete sample was not observable", values)
				}
				f.vault.unavailable.Store(true)
			case 2:
				if values["sample_healthy"] != 0 || values["vault_current"] != 0 || values["native_current"] != 1 {
					t.Fatal("vault outage hid native progress or became healthy", values)
				}
				f.vault.unavailable.Store(false)
			case 3:
				if values["sample_healthy"] != 1 || values["vault_current"] != 1 || values["sample_timestamp_seconds"] <= uint64(original) {
					t.Fatal("original vault recovery did not refresh metrics", values)
				}
				cancel()
			default:
				t.Fatal("canceled economic owner continued publication")
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			f.now = f.now.Add(time.Second)
			return ctx.Err() == nil
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 0 || samples != 3 || bytes.Count(output.Bytes(), []byte{'\n'}) != 3 {
		t.Fatal("combined metrics lifecycle did not join exact public samples", code, samples, diagnostic.String())
	}
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ownerCtx := monitorTestStorageContext(t, t.Context(), args)
	owner, err := openMonitorMetrics(path, ownerCtx)
	if err != nil {
		t.Fatal("economic metrics owner did not close", err)
	}
	if err := initializeEconomicConservationMetrics(owner); err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(retained, again) {
		t.Fatal("restart erased retained metrics or refreshed sample time", err)
	}
}

func TestEconomicMetricsPublicLostAckKeepsCheckpointAndSuppressesOutput(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path)
	var output, diagnostic bytes.Buffer
	var syncs, closed int
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		if role == economicConservationRole && kind == "metrics" {
			syncs++
			if syncs == 2 {
				return errors.Join(file.Sync(), syscall.EIO)
			}
		}
		return file.Sync()
	}, afterClose: func(role, kind string, file *os.File) error {
		if role == economicConservationRole && (kind == "metrics" || kind == "checkpoint") {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("economic publication owner was not joined")
			}
			closed++
		}
		return nil
	}}
	code := runMonitorStorageTestWithHooks(t, t.Context(), args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	before := f.state(t)
	if code != 3 || syncs != 2 || closed != 2 || output.Len() != 0 || before.Native.Cursor.Number != 102 || before.Vault.Cursor.Number != 11 {
		t.Fatal("metrics acknowledgment failure published success or lost committed facts", code, syncs, closed, output.String(), diagnostic.String())
	}
	output.Reset()
	diagnostic.Reset()
	code = runMonitorStorageTestWithHooks(t, t.Context(), args, &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{})
	after := f.state(t)
	if code != 0 || output.Len() == 0 || len(after.Lots) != len(before.Lots) || len(after.Captures) != len(before.Captures) || after.Captures[0].Id != before.Captures[0].Id {
		t.Fatal("metrics retry replayed original financial effects", code, diagnostic.String())
	}
}

func TestEconomicMetricsPublicEqualByteLockReplacementCannotPublish(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path)
	var output, diagnostic bytes.Buffer
	replaced := false
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		if role == economicConservationRole && kind == "checkpoint" && !replaced {
			replaced = true
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
		}
		return file.Sync()
	}}
	code := runMonitorStorageTestWithHooks(t, t.Context(), args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	values := economicMetricsTestFile(t, path)
	if code != 3 || !replaced || output.Len() != 0 || !strings.Contains(diagnostic.String(), "metrics lock changed") || values["sample_timestamp_seconds"] != 0 || f.state(t).Native.Cursor.Number != 102 {
		t.Fatal("equal-byte replacement refreshed unowned metrics", code, replaced, values, diagnostic.String())
	}
}

func TestEconomicMetricsPublicInputCollisionRefusesBeforeEffects(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	original, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, t.Context(), append(f.args(t), "--metrics-file", f.path), &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{})
	retained, err := os.ReadFile(f.path)
	if code != 2 || output.Len() != 0 || err != nil || !bytes.Equal(original, retained) || f.claimReads.Load() != 0 {
		t.Fatal("metrics output replaced original policy or started reads", code, diagnostic.String(), err)
	}
	if _, err := os.Stat(f.checkpoint); !os.IsNotExist(err) {
		t.Fatal("refused output collision created economic checkpoint", err)
	}
}
