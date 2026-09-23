// Fake time and explicit checkpoint failures force disk admission to outlast
// the former shared deadline. No sleep relies on wall-clock scheduling.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

func TestMinerControlAdmissionSlowFlushCompletesNinetySixTargets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newMinerControlTestFixture(t)
		perSwarm := fixture.driver.cfg.Config.Topology.Miners / fixture.driver.cfg.Config.Topology.MinerSwarmProcesses
		for index := 0; index < 96; index++ {
			miner := (index%20)*perSwarm + index/20 + 1
			target := fmt.Sprintf("miner-%d", miner)
			fixture.fault.Targets = append(fixture.fault.Targets, target)
			fixture.states[target] = "running"
		}
		installMinerControlTestSwarms(t, fixture)
		fixture.driver.minerControlParallel = 0
		flushes := 0
		fixture.driver.minerControlPersist = func(path string, raw []byte) error {
			flushes++
			// Every flush takes longer than the old entire-round allowance.
			// The synctest clock advances only while workers are durably blocked.
			time.Sleep(11 * time.Second)
			return atomicWrite(path, raw, 0o600)
		}
		var requests atomic.Int64
		fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			deadline, bounded := request.Context().Deadline()
			if !bounded || deadline.Sub(time.Now()) != minerControlRequestTimeout || request.Context().Err() != nil {
				t.Errorf("admission consumed the actual request budget: bounded=%t remaining=%s err=%v", bounded, deadline.Sub(time.Now()), request.Context().Err())
			}
			target := strings.Split(strings.Trim(request.URL.Path, "/"), "/")[1]
			progress := fixture.progress(t)
			if !slices.Contains(progress.PendingTargets, target) || request.Method == http.MethodPost && progress.Attempts == 0 {
				t.Errorf("request preceded durable target/attempt admission: %s %+v", target, progress)
			}
			return fixture.roundTrip(request)
		})
		original, err := canonicalHashHex(fixture.fault)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil {
			t.Fatal(err)
		}
		progress := fixture.progress(t)
		if progress.Phase != "active" || progress.CompletedCount != 96 || progress.Attempts != 96 || len(progress.PendingTargets) != 0 || progress.FaultHash != original || requests.Load() != 192 {
			t.Fatalf("slow durable admission lost work or changed the fault: %+v requests=%d", progress, requests.Load())
		}
		if flushes >= 96 {
			t.Fatalf("checkpoints still scale as per-target fsyncs: %d", flushes)
		}
		fixture.stateLock.Lock()
		defer fixture.stateLock.Unlock()
		seen := map[string]bool{}
		for _, post := range fixture.posts {
			if seen[post] {
				t.Fatalf("duplicated a completed control: %s", post)
			}
			seen[post] = true
		}
		if len(seen) != 96 {
			t.Fatalf("completed side effects=%d, want 96", len(seen))
		}
	})
}

func TestMinerControlAdmissionCompletionFlushFailureReconcilesAfterRestart(t *testing.T) {
	for _, afterRename := range []bool{false, true} {
		fixture := newMinerControlTestFixture(t, "miner-1", "miner-2", "miner-3", "miner-4", "miner-5", "miner-6")
		fixture.driver.minerControlParallel = 4
		failed := false
		fixture.driver.minerControlPersist = func(path string, raw []byte) error {
			var active activeFaultFile
			if err := json.Unmarshal(raw, &active); err != nil {
				return err
			}
			if !failed && active.MinerControls[0].CompletedCount == 4 {
				failed = true
				if afterRename {
					if err := atomicWrite(path, raw, 0o600); err != nil {
						return err
					}
				}
				return &os.PathError{Op: "sync", Path: path, Err: syscall.EIO}
			}
			return atomicWrite(path, raw, 0o600)
		}
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); !minerControlPending(err) || !errors.Is(err, syscall.EIO) {
			t.Fatalf("completion storage retry became terminal afterRename=%t: %v", afterRename, err)
		}
		if !failed {
			t.Fatal("fixture missed the four-member completion checkpoint")
		}
		reopened := *fixture.driver
		reopened.minerControlPersist = nil
		reopened.minerControlReconciled = nil
		if _, err := reopened.Apply(t.Context(), fixture.fault); err != nil {
			t.Fatal(err)
		}
		if progress := fixture.progress(t); progress.Phase != "active" || progress.CompletedCount != 6 || progress.Attempts != 6 {
			t.Fatalf("restart changed retained attempts or completion: %+v", progress)
		}
		fixture.stateLock.Lock()
		posts := slices.Clone(fixture.posts)
		fixture.stateLock.Unlock()
		slices.Sort(posts)
		want := []string{"/control/miner-1/disable", "/control/miner-2/disable", "/control/miner-3/disable", "/control/miner-4/disable", "/control/miner-5/disable", "/control/miner-6/disable"}
		if !slices.Equal(posts, want) {
			t.Fatalf("restart repeated a side effect afterRename=%t: %v", afterRename, posts)
		}
	}
}

func TestMinerControlAdmissionAttemptStorageFailurePreventsMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2", "miner-3", "miner-4")
	fixture.driver.minerControlParallel = 4
	fixture.driver.minerControlPersist = func(path string, raw []byte) error {
		var active activeFaultFile
		if err := json.Unmarshal(raw, &active); err != nil {
			return err
		}
		if active.MinerControls[0].Attempts != 0 {
			return &os.PathError{Op: "sync", Path: path, Err: syscall.ENOSPC}
		}
		return atomicWrite(path, raw, 0o600)
	}
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); !minerControlPending(err) || !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("storage admission error=%v", err)
	}
	fixture.requirePosts(t)
	progress := fixture.progress(t)
	if progress.Attempts != 0 || len(progress.PendingTargets) != 4 {
		t.Fatalf("failed attempt commit lost durable intent: %+v", progress)
	}
	fixture.driver.minerControlPersist = nil
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	if fixture.progress(t).Attempts != 4 {
		t.Fatal("retry counted or repeated uncommitted attempts")
	}
}

func TestMinerControlAdmissionOuterDeadlineRetainsPartialFaultSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newMinerControlTestFixture(t, "miner-1")
		fixture.fault.TriggerOffsetBlocks, fixture.fault.DurationBlocks = 1, 20
		specs := []scenarioFaultSpec{fixture.fault}
		records, err := initializeFaultRecords(100, specs)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		fixture.driver.minerControlPersist = func(path string, raw []byte) error {
			var active activeFaultFile
			if err := json.Unmarshal(raw, &active); err != nil {
				return err
			}
			if active.MinerControls[0].CompletedCount == 1 {
				time.Sleep(2 * time.Second)
			}
			return atomicWrite(path, raw, 0o600)
		}
		head := ChainHead{Number: 101, Hash: "0x" + strings.Repeat("1", 64)}
		if err := advanceFaults(ctx, head, specs, records, fixture.driver); err != nil {
			t.Fatal(err)
		}
		if records[0].Status != "pending" || records[0].AppliedBlock != 0 || records[0].ControlPendingRounds != 1 || records[0].ControlStartedBlock != 101 {
			t.Fatalf("heartbeat expiration terminalized or backdated successful partial work: %+v", records[0])
		}
		if err := validateScenarioCampaignMinerControl(records[0]); err != nil {
			t.Fatal(err)
		}
		fixture.driver.minerControlPersist = nil
		actual := ChainHead{Number: 108, Hash: "0x" + strings.Repeat("2", 64)}
		fixture.driver.minerControlHead = func(context.Context) (ChainHead, error) { return actual, nil }
		head.Number = 105
		if err := advanceFaults(t.Context(), head, specs, records, fixture.driver); err != nil {
			t.Fatal(err)
		}
		if records[0].Status != "active" || records[0].AppliedBlock != 108 || records[0].TriggerBlock != 101 || records[0].RestoreBlock != 121 || records[0].Error != "" {
			t.Fatalf("continuation changed the signed schedule or actual completion: %+v", records[0])
		}
		fixture.requirePosts(t, "/control/miner-1/disable")
	})
}

func TestMinerControlAdmissionPendingLifecycleHasBoundedPolls(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.states["miner-1"] = "starting"
	reads := 0
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		reads++
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); !minerControlPending(err) || !errors.Is(err, errMinerControlLifecyclePending) {
		t.Fatalf("bounded lifecycle poll=%v", err)
	}
	if reads != minerControlMaximumPolls {
		t.Fatalf("lifecycle reads=%d, want %d", reads, minerControlMaximumPolls)
	}
	fixture.requirePosts(t)
}

func TestMinerControlAdmissionRetryClassificationPreservesIntegrity(t *testing.T) {
	for _, test := range []struct {
		err       error
		retryable bool
	}{
		{err: &os.PathError{Op: "sync", Path: "synthetic-checkpoint", Err: syscall.EIO}, retryable: true},
		{err: syscall.ENOSPC, retryable: true},
		{err: errors.Join(syscall.EINTR, syscall.EAGAIN), retryable: true},
		{err: syscall.EACCES},
		{err: syscall.EISDIR},
		{err: errors.Join(syscall.EIO, errors.New("synthetic changed intent"))},
	} {
		if got := minerControlStorageRetryable(test.err); got != test.retryable {
			t.Fatalf("storage retry=%t want=%t: %v", got, test.retryable, test.err)
		}
	}
	if minerControlRoundRetryable(errors.Join(errMinerControlLifecyclePending, errors.New("synthetic invalid status")), true) {
		t.Fatal("pending lifecycle masked independent integrity error")
	}
	fixture := newMinerControlTestFixture(t, "miner-1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(*http.Request) (*http.Response, error) {
		t.Error("canceled dispatch reached the transport")
		return nil, errors.New("unexpected request")
	})
	if _, _, err := fixture.driver.minerControlRequest(ctx, http.MethodPost, "http://swarm.example/control/miner-1/disable"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// A failed diagnostic checkpoint cannot erase the malformed response that
// caused it. Both causes remain visible and no control mutation is admitted.
func TestMinerControlAdmissionFailedDiagnosticPreservesSemanticError(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(*http.Request) (*http.Response, error) {
		return minerControlTestResponse(http.StatusOK, map[string]string{"id": "foreign-miner", "state": "running"}), nil
	})
	fixture.driver.minerControlPersist = func(path string, raw []byte) error {
		var active activeFaultFile
		if err := json.Unmarshal(raw, &active); err != nil {
			return err
		}
		if active.MinerControls[0].LastError != "" {
			return &os.PathError{Op: "sync", Path: path, Err: syscall.EIO}
		}
		return atomicWrite(path, raw, 0o600)
	}
	_, err := fixture.driver.Apply(t.Context(), fixture.fault)
	var invalid *minerControlInvalidStatusError
	if err == nil || minerControlPending(err) || !errors.As(err, &invalid) || !errors.Is(err, syscall.EIO) {
		t.Fatalf("diagnostic failure concealed malformed status: %v", err)
	}
	fixture.requirePosts(t)
	if !minerControlRetryable(errors.Join(context.Canceled, syscall.EIO, context.DeadlineExceeded), true) {
		t.Fatal("joined transient checkpoint cancellation became hard")
	}
	if minerControlRetryable(errors.Join(context.Canceled, syscall.EIO, invalid), true) {
		t.Fatal("joined semantic checkpoint failure became pending")
	}
}
