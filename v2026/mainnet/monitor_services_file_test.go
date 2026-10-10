// File and checkpoint regressions use the real bounded readers and shared
// atomic writers. No admission callback supplies a successful source verdict.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Missing, empty, partial, aliased and oversized files are different from a
// successfully observed empty intent. Identity is never learned from a file.
func TestMonitorServicesCommandRejectsInvalidFilesAndEverySourceMismatch(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	validator := fixture.policy.Validators[0]
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := run.next(t)
	for _, raw := range [][]byte{nil, []byte(`{"schema":`), bytes.Repeat([]byte(" "), protocol.MaxValidatorProgressBytes+1), []byte(`{"schema":"a","schema":"b"}`)} {
		if err := os.WriteFile(validator.ProgressFile, raw, 0644); err != nil {
			t.Fatal(err)
		}
		run.again(t, "alpha")
		event := run.next(t)
		if event.Status != "invalid" || event.State.Record == nil || !event.State.LastReadSuccessAt.Equal(first.State.LastReadSuccessAt) {
			t.Fatal("invalid file became an empty healthy record", event)
		}
	}
	for _, mutate := range []func(*protocol.ValidatorProgressSource){
		func(source *protocol.ValidatorProgressSource) {
			source.ConfigHash = "sha256:" + strings.Repeat("9", 64)
		},
		func(source *protocol.ValidatorProgressSource) { source.DeploymentId = "different-synthetic-deployment" },
		func(source *protocol.ValidatorProgressSource) { source.ValidatorId++ },
		func(source *protocol.ValidatorProgressSource) { source.ChainId++ },
		func(source *protocol.ValidatorProgressSource) { source.GenesisHash = "0x" + strings.Repeat("9", 64) },
		func(source *protocol.ValidatorProgressSource) { source.Netuid++ },
	} {
		value := monitorServicesTestRecord(fixture.clock.now(), 1)
		mutate(&value.Source)
		monitorServicesTestWrite(t, validator.ProgressFile, value)
		run.again(t, "alpha")
		event := run.next(t)
		if event.Status != "identity" || event.State.Record.Source != validator.ExpectedSource {
			t.Fatal("candidate supplied its own expected source", event)
		}
	}
	if err := os.Remove(validator.ProgressFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.policyPath, validator.ProgressFile); err != nil {
		t.Fatal(err)
	}
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "invalid" {
		t.Fatal("aliased source was followed", event)
	}
}

// An atomic replacement during a read invalidates that sample. A later fresh
// read can accept the complete successor without permanently disabling its role.
func TestMonitorServicesCommandRejectsReplacedSourceThenReadsSuccessor(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	path := fixture.policy.Validators[0].ProgressFile
	changed := false
	hooks := monitorServiceHooks{read: func(string) monitorServiceReadHooks {
		return monitorServiceReadHooks{afterRead: func(*os.File) error {
			if !changed {
				changed = true
				value := monitorServicesTestRecord(fixture.clock.now(), 1)
				raw, err := value.Encode()
				if err != nil {
					return err
				}
				return publishMonitorFile(path, raw, 0644, nil)
			}
			return nil
		}}
	}}
	run := fixture.start(t, url, hooks)
	if event := run.next(t); event.Status != "changed" || event.State.Record != nil {
		t.Fatal("changed file became current evidence", event)
	}
	run.again(t, "alpha")
	if event := run.next(t); event.Status != "observed" {
		t.Fatal("complete successor could not be read", event)
	}
}

// The actual descriptor close precedes its fault observer; joined errors and
// cancellation cannot leave a successful byte result behind.
func TestMonitorServicesFileCloseFailureAndCancellationWithholdBytes(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	path := fixture.policy.Validators[0].ProgressFile
	cause := errors.New("synthetic source close fault")
	closed := false
	raw, err := readMonitorServiceFile(t.Context(), path, protocol.MaxValidatorProgressBytes, false, monitorServiceReadHooks{afterClose: func(file *os.File) error {
		_, statErr := file.Stat()
		if !errors.Is(statErr, os.ErrClosed) {
			t.Error("source observer replaced physical close", statErr)
		}
		closed = true
		return cause
	}})
	if raw != nil || !closed || !errors.Is(err, cause) {
		t.Fatal("late close fault became successful bytes", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	raw, err = readMonitorServiceFile(ctx, path, protocol.MaxValidatorProgressBytes, false, monitorServiceReadHooks{afterRead: func(*os.File) error { cancel(); return nil }})
	if raw != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation published candidate bytes", err)
	}
}

// A stale restart cannot refresh the role's existing textfile while its first
// physical read is still owned. Metrics remain available to independent alerts.
func TestMonitorServicesCommandRestartPreservesMetricsUntilActualRead(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	run.next(t)
	run.cancel()
	<-run.done
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	hooks := monitorServiceHooks{read: func(string) monitorServiceReadHooks {
		return monitorServiceReadHooks{afterRead: func(*os.File) error { close(entered); <-release; return nil }}
	}}
	restarted := fixture.start(t, url, hooks)
	// This defer precedes registered cleanup, so blocked physical I/O can finish
	// before the command's real cancellation join.
	defer close(release)
	restarted.barrier(t, entered)
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, retained) {
		t.Fatal("restart refreshed prior output before read completion", err)
	}
	restarted.cancel()
}

// Initial outages survive restart before any accepted producer record exists.
// Checkpoint corruption is terminal and cannot silently reset accumulated age.
func TestMonitorServicesCommandInitialOutageAndCheckpointCorruption(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := run.next(t)
	run.cancel()
	<-run.done
	fixture.clock.seconds.Add(301)
	again := fixture.start(t, url, monitorServiceHooks{})
	retained := again.next(t)
	if retained.Severity != "critical" || retained.State.Record != nil || !retained.State.OutageSince.Equal(first.State.OutageSince) {
		t.Fatal("initial no-success restart became healthy", retained)
	}
	again.cancel()
	<-again.done
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"read_status":"missing"`), []byte(`"read_status":"invalid"`), 1)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	terminal := make(chan int, 1)
	broken := fixture.start(t, url, monitorServiceHooks{afterWorker: func(role string, exit int) {
		if role == "alpha" {
			terminal <- exit
		}
	}})
	if exit := <-terminal; exit != 3 {
		t.Fatal("corrupt owner did not stop its role", exit)
	}
	broken.cancel()
	<-broken.done
	if broken.exit != 3 || !strings.Contains(broken.stderr.String(), "snapshot bytes differ from the acknowledged head") {
		t.Fatal("corrupt continuity was reset", broken.exit, broken.stderr.String())
	}
}

// Same-vector pending age is stable even when a candidate tries to rewrite its
// creation time or progress timestamp under the correct current source config.
func TestMonitorServicesCommandRejectsSameIntentAgeRewrite(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	value := monitorServicesTestRecord(fixture.clock.now(), 1)
	old := fixture.clock.now().Add(-time.Hour).Format(time.RFC3339Nano)
	value.Intent.Value = &protocol.ValidatorIntentProgress{ConfigHash: value.Source.ConfigHash, VectorHash: "0x" + strings.Repeat("3", 64), Status: "pending", CreatedAt: old, ProgressAt: old}
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	run.next(t)
	value.Intent.Value.ProgressAt = fixture.clock.now().Format(time.RFC3339Nano)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	event := run.next(t)
	if event.Status != "invalid" || event.State.Record.Intent.Value.ProgressAt != old {
		t.Fatal("unchanged pending vector reset its progress age", event)
	}
}

// Source/output collisions include derived role files and their private locks.
// The real command rejects these before any RPC or role workers can start.
func TestMonitorServicesCommandRejectsRoleOutputCollisionBeforeReads(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	fixture.policy.Validators[0].ProgressFile = path + ".lock"
	fixture.writePolicy(t)
	server, count := testRpcServerWithIdentity(t, "fixture-mainnet", "0x3c4", "")
	var stdout, stderr bytes.Buffer
	exit := runMainWithClock(t.Context(), fixture.args(server.URL), &stdout, &stderr, fixture.clock.now)
	if exit != 2 || count("system_chain") != 0 || stdout.Len() != 0 {
		t.Fatal("role output collision acquired an observer", exit, stderr.String())
	}
}

// Checkpoint durability uncertainty cannot become confirmed export success;
// the next actual read/atomic write resolves it while old source evidence ages.
func TestMonitorServicesCommandAmbiguousCheckpointDoesNotConfirmExport(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	url, _, _ := monitorServicesBlockedChain(t)
	attempt := 0
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if kind == "checkpoint" {
			attempt++
			if attempt == 1 {
				return errors.Join(err, errors.New("synthetic checkpoint sync uncertainty"))
			}
		}
		return err
	}}
	run := fixture.start(t, url, hooks)
	first := run.next(t)
	_, path := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	values := monitorServicesGauges(t, path, "alpha")
	if first.Publication != "retrying" || values["checkpoint_current"] != 0 || values["export_last_success_timestamp_seconds"] != 0 {
		t.Fatal("ambiguous checkpoint confirmed export", first, values)
	}
	run.again(t, "alpha")
	if event := run.next(t); event.Publication != "published" {
		t.Fatal("complete checkpoint could not be republished", event)
	}
}

// The strict expected policy rejects unknown/ambiguous wire and an unbounded
// role census before a source or candidate can influence a routing decision.
func TestMonitorServicesPolicyBoundsAndStrictWire(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	raw, err := json.Marshal(fixture.policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range [][]byte{append(bytes.Clone(raw), []byte(" {}")...), bytes.Replace(raw, []byte(`"schema":`), []byte(`"unknown":true,"schema":`), 1), bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"other","schema":`), 1), bytes.Repeat([]byte(" "), maxMonitorServicesBytes+1)} {
		if err := os.WriteFile(fixture.policyPath, candidate, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); err == nil {
			t.Fatal("ambiguous or unbounded policy admitted")
		}
	}
	fixture.policy.Validators[0].Role = `alpha"bad`
	fixture.writePolicy(t)
	if _, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); err == nil {
		t.Fatal("unbounded label admitted")
	}
	if _, err := os.Stat(filepath.Join(fixture.directory, "monitor.validator-alpha.prom")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("policy-only refusal created an output")
	}
	for _, size := range []int{0, maxMonitorValidatorRoles + 1} {
		fixture.policy.Validators = make([]monitorValidatorPolicy, size)
		fixture.writePolicy(t)
		if _, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); !errors.Is(err, errMonitorServicesCensus) {
			t.Fatal("policy census escaped its explicit bound", size, err)
		}
	}
}
