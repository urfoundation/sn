// Recovery diagnostics cannot substitute another fault, bypass fresh reads, or
// turn malformed control responses into transport retries.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestMinerControlRejectsSubstitutedDurableProgress(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*activeFaultFile)
	}{
		{name: "fault hash", mutate: func(active *activeFaultFile) { active.MinerControls[0].FaultHash = "synthetic-substitution" }},
		{name: "legacy adoption version", mutate: func(active *activeFaultFile) { active.Schema = "urnetwork-sim-active-faults-v1" }},
		{name: "target total", mutate: func(active *activeFaultFile) { active.MinerControls[0].Total++ }},
		{name: "completion count", mutate: func(active *activeFaultFile) { active.MinerControls[0].CompletedCount++ }},
		{name: "completed target", mutate: func(active *activeFaultFile) { active.MinerControls[0].Completed[0].ID = "miner-2" }},
		{name: "completed pid", mutate: func(active *activeFaultFile) { active.MinerControls[0].Completed[0].PID = 0 }},
		{name: "phase", mutate: func(active *activeFaultFile) { active.MinerControls[0].Phase = "synthetic-invalid-phase" }},
		{name: "pending overlap", mutate: func(active *activeFaultFile) { active.MinerControls[0].Pending = "miner-1" }},
		{name: "orphan fault", mutate: func(active *activeFaultFile) { active.MinerControls[0].FaultId = "synthetic-other-fault" }},
		{name: "duplicate fault", mutate: func(active *activeFaultFile) {
			active.MinerControls = append(active.MinerControls, active.MinerControls[0])
		}},
		{name: "incomplete active", mutate: func(active *activeFaultFile) {
			active.MinerControls[0].Completed = nil
			active.MinerControls[0].CompletedCount = 0
		}},
	}
	for _, test := range cases {
		fixture := newMinerControlTestFixture(t, "miner-1")
		fixture.apply(t)
		active, err := readActiveFaultFile(fixture.driver.activePath())
		if err != nil {
			t.Fatal(err)
		}
		test.mutate(&active)
		if err := writePublicJSON(fixture.driver.activePath(), active); err != nil {
			t.Fatal(err)
		}
		if err := fixture.driver.Recover(context.Background()); err == nil {
			t.Fatalf("accepted substituted progress: %s", test.name)
		}
		fixture.requirePosts(t)
	}
}

func TestMinerControlUpgradesLegacyRecoveryBeforeEnable(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	processes, err := fixture.driver.minerControlProcesses(fixture.fault, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendActiveFault(fixture.driver.activePath(), activeFaultFile{Schema: "urnetwork-sim-active-faults-v1"}, fixture.fault, processes); err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.states["miner-1"] = "disabled"
	fixture.stateLock.Unlock()
	observed := false
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		active, err := readActiveFaultFile(fixture.driver.activePath())
		if err != nil || active.Schema != "urnetwork-sim-active-faults-v2" || len(active.MinerControls) != 1 || active.MinerControls[0].Phase != "restoring" {
			t.Fatalf("legacy recovery was not upgraded before request: %+v, %v", active, err)
		}
		observed = true
		return fixture.roundTrip(request)
	})
	if err := fixture.driver.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("legacy recovery omitted fresh control state")
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
}

func TestMinerControlRemovalPreservesVersionedIndependentFault(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	fixture.fault.Targets = []string{"miner-1"}
	fixture.apply(t)
	other := fixture.fault
	other.ID = "synthetic-other-cohort"
	other.Targets = []string{"miner-2"}
	if _, err := fixture.driver.Apply(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	active, err := readActiveFaultFile(fixture.driver.activePath())
	if err != nil || active.Schema != "urnetwork-sim-active-faults-v2" || len(active.Faults) != 1 || active.Faults[0].ID != other.ID || len(active.MinerControls) != 1 || active.MinerControls[0].FaultId != other.ID || active.MinerControls[0].Phase != "active" {
		t.Fatalf("restoration discarded independent versioned fault: %+v, %v", active, err)
	}
	if _, err := fixture.driver.Apply(context.Background(), other); err != nil {
		t.Fatalf("complete versioned fault could not be adopted: %v", err)
	}
	if err := fixture.driver.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMinerControlMissingPopulationCannotConfirmSuccessfulMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost {
			_ = response.Body.Close()
			return minerControlTestResponse(http.StatusOK, map[string]any{"schema": "urnetwork-provider-swarm-v1", "configured": fixture.driver.cfg.Config.Topology.Miners / fixture.driver.cfg.Config.Topology.MinerSwarmProcesses}), nil
		}
		return response, err
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil || !strings.Contains(err.Error(), "omits its required population") {
		t.Fatalf("missing required response field = %v", err)
	}
	if fixture.progress(t).CompletedCount != 0 {
		t.Fatal("incomplete control response certified restoration")
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
}

func TestMinerControlMalformedStatusNeverRetriesAsTransport(t *testing.T) {
	for _, raw := range []string{"", "{", "{}", `{"schema":"urnetwork-provider-swarm-member-v1","id":"miner-1","state":"running","unexpected":true}`} {
		fixture := newMinerControlTestFixture(t, "miner-1")
		fixture.apply(t)
		reads := 0
		fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet {
				t.Fatal("malformed status authorized a mutation")
			}
			reads++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(raw))}, nil
		})
		if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil {
			t.Fatalf("accepted malformed status %q", raw)
		}
		if reads != 1 {
			t.Fatalf("malformed status %q retried %d reads", raw, reads)
		}
		fixture.requirePosts(t)
	}
}

func TestMinerControlTransportClassificationPreservesSemanticFailures(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		transient bool
	}{
		{name: "transport timeout", err: fmt.Errorf("control transport: %w", context.DeadlineExceeded), transient: true},
		{name: "transport eof", err: io.ErrUnexpectedEOF, transient: true},
		{name: "caller canceled", err: context.Canceled},
		{name: "semantic timeout text", err: errors.New("synthetic signature rejected after timeout")},
		{name: "decoded empty response", err: &minerControlInvalidStatusError{cause: io.EOF}},
		{name: "unavailable response", err: &minerControlHttpError{status: http.StatusServiceUnavailable}, transient: true},
		{name: "semantic response", err: &minerControlHttpError{status: http.StatusConflict}},
		{name: "server error", err: &minerControlHttpError{status: http.StatusInternalServerError}},
		{name: "mixed response", err: errors.Join(&minerControlHttpError{status: http.StatusServiceUnavailable}, errors.New("synthetic integrity failure"))},
		{name: "mixed body close", err: errors.Join(io.ErrUnexpectedEOF, errors.New("synthetic close failure"))},
	}
	for _, test := range cases {
		if got := minerControlTransientError(test.err); got != test.transient {
			t.Fatalf("%s transient=%t, want %t", test.name, got, test.transient)
		}
	}
}

func TestMinerControlDurableWriteFailurePreventsNextMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Fatal("request escaped after durable progress became unwritable")
		}
		// Preserve the actual prior ledger while making its atomic replacement
		// fail deterministically, independent of the test process's privileges.
		if err := os.Rename(fixture.driver.activePath(), fixture.driver.activePath()+".preserved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.driver.activePath(), 0o700); err != nil {
			t.Fatal(err)
		}
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil {
		t.Fatal("failed durable attempt write was ignored")
	}
	fixture.requirePosts(t)
	active, err := readActiveFaultFile(fixture.driver.activePath() + ".preserved")
	if err != nil || active.MinerControls[0].Phase != "restoring" || active.MinerControls[0].Pending != "miner-1" || active.MinerControls[0].Attempts != 0 {
		t.Fatalf("lost prior recovery intent: %+v, %v", active, err)
	}
}

func TestMinerControlLegacyAmbiguousDisableCannotCertifyTeardown(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.Path != "/status" {
			return minerControlTestResponse(http.StatusNotFound, "synthetic legacy status endpoint"), nil
		}
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost {
			_ = response.Body.Close()
			return nil, io.ErrUnexpectedEOF
		}
		return response, err
	})
	if _, err := fixture.driver.Apply(context.Background(), fixture.fault); err == nil || !strings.Contains(err.Error(), "cannot confirm teardown") {
		t.Fatalf("legacy ambiguous teardown = %v", err)
	}
	progress := fixture.progress(t)
	if progress.Phase != "applying" || progress.CompletedCount != 0 || progress.Pending != "miner-1" {
		t.Fatalf("legacy teardown became completed activation: %+v", progress)
	}
	fixture.requirePosts(t, "/control/miner-1/disable")
}

type minerControlTestBody struct {
	io.Reader
	readError  error
	closeError error
	closes     int
}

func (self *minerControlTestBody) Read(buffer []byte) (int, error) {
	if self.readError != nil {
		return 0, self.readError
	}
	return self.Reader.Read(buffer)
}

func (self *minerControlTestBody) Close() error {
	self.closes++
	return self.closeError
}

func TestMinerControlBodyCloseFailureDoesNotCertifyOrRetryStatus(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	body := &minerControlTestBody{Reader: strings.NewReader(`{"schema":"urnetwork-provider-swarm-member-v1","id":"miner-1","state":"running"}`), closeError: errors.New("synthetic close failure")}
	reads := 0
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(*http.Request) (*http.Response, error) {
		reads++
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil || !strings.Contains(err.Error(), "synthetic close failure") {
		t.Fatalf("body close failure = %v", err)
	}
	if reads != 1 || body.closes != 1 || fixture.progress(t).CompletedCount != 0 {
		t.Fatalf("body close ownership/read admission = %d/%d", reads, body.closes)
	}
	fixture.requirePosts(t)
}

func TestMinerControlIncompleteTransportBodyClosesBeforeRetry(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	body := &minerControlTestBody{readError: io.ErrUnexpectedEOF}
	reads := 0
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet {
			reads++
			if reads == 1 {
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}
			if body.closes != 1 {
				t.Fatal("retry started before the incomplete body was closed")
			}
		}
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	if reads != 2 || body.closes != 1 {
		t.Fatalf("incomplete body retry reads/closes = %d/%d", reads, body.closes)
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
}
