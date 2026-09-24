// A controlled contract reader exercises the real status and snapshot retry
// owners without a live chain, transport sleeps or inferred error messages.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scenarioStatusRetryTestProbe struct {
	cfg           *ResolvedConfig
	stateDir      string
	readContracts func(context.Context, *ResolvedConfig, string, string) (*ContractView, error)
}

func (self *scenarioStatusRetryTestProbe) Snapshot(ctx context.Context) (*ScenarioObservation, error) {
	status, err := statusWithContractReader(ctx, self.cfg, self.stateDir, self.readContracts)
	if err != nil {
		return nil, err
	}
	return &ScenarioObservation{Status: status}, nil
}

func scenarioStatusRetryTestDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "public", "contracts.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScenarioStatusTransientContractReadReachesSnapshotRecovery(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		snapshotRetryTestTransportError(),
		&evmReadRpcExhaustedError{operation: "synthetic contract batch", attempts: 4, attemptTimeout: 30 * time.Second, cause: context.DeadlineExceeded},
	} {
		cfg := testResolvedConfig(t)
		dir := scenarioStatusRetryTestDirectory(t)
		state := newProvisionalSnapshotRetryTestState(t)
		calls := 0
		view := &ContractView{ConservationHolds: true, RuntimeCodeMatches: true, CurrentEpoch: 7}
		probe := &scenarioStatusRetryTestProbe{cfg: cfg, stateDir: dir, readContracts: func(ctx context.Context, got *ResolvedConfig, root, manifest string) (*ContractView, error) {
			calls++
			if ctx == nil || got != cfg || root != dir || manifest != "" {
				t.Fatal("status changed the contract read owner")
			}
			if calls == 1 {
				return nil, failure
			}
			return view, nil
		}}
		observation, err := state.wrap(probe).Snapshot(t.Context())
		if err != nil || observation == nil || observation.Status.Contracts != view || calls != 2 {
			t.Fatalf("transient read became missing deployment: calls=%d observation=%+v error=%v", calls, observation, err)
		}
		if records := state.assertions(); len(records) != 1 || !records[0].Passed || !strings.Contains(records[0].Message, failure.Error()) {
			t.Fatalf("recovered contract failure lost durable diagnostics: %+v", records)
		}
	}
}

func TestScenarioStatusPreservesPartialDiagnosticsAndTypedCause(t *testing.T) {
	t.Parallel()
	failure := snapshotRetryTestTransportError()
	status, err := statusWithContractReader(t.Context(), testResolvedConfig(t), scenarioStatusRetryTestDirectory(t), func(context.Context, *ResolvedConfig, string, string) (*ContractView, error) {
		return nil, failure
	})
	if !errors.Is(err, failure) || status == nil || status.Healthy || status.Contracts != nil || !strings.Contains(strings.Join(status.Warnings, "\n"), failure.Error()) {
		t.Fatalf("partial status lost typed cause or warning: status=%+v error=%v", status, err)
	}
}

func TestScenarioStatusKeepsAbsentAndMalformedDeploymentDistinct(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	calls := 0
	status, err := statusWithContractReader(t.Context(), cfg, t.TempDir(), func(context.Context, *ResolvedConfig, string, string) (*ContractView, error) {
		calls++
		return nil, snapshotRetryTestTransportError()
	})
	if err != nil || status == nil || status.Contracts != nil || calls != 0 {
		t.Fatalf("absent deployment triggered a contract read: status=%+v calls=%d error=%v", status, calls, err)
	}
	for _, failure := range []error{&json.SyntaxError{Offset: 3}, errors.Join(snapshotRetryTestTransportError(), errors.New("synthetic contract evidence digest mismatch"))} {
		calls = 0
		status, err = statusWithContractReader(t.Context(), cfg, scenarioStatusRetryTestDirectory(t), func(context.Context, *ResolvedConfig, string, string) (*ContractView, error) {
			calls++
			return nil, failure
		})
		if err != nil || status == nil || status.Contracts != nil || status.Healthy || calls != 1 || !strings.Contains(strings.Join(status.Warnings, "\n"), failure.Error()) {
			t.Fatalf("permanent unavailable deployment changed behavior: status=%+v calls=%d error=%v", status, calls, err)
		}
	}
}

func TestScenarioStatusPropagatesCancellationAndLocalManifestFailure(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := statusWithContractReader(ctx, cfg, scenarioStatusRetryTestDirectory(t), func(context.Context, *ResolvedConfig, string, string) (*ContractView, error) {
		cancel()
		return nil, context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("owner cancellation was reported as absent deployment: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "public"), []byte("synthetic non-directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err = statusWithContractReader(t.Context(), cfg, dir, func(context.Context, *ResolvedConfig, string, string) (*ContractView, error) {
		calls++
		return nil, nil
	})
	var pathError *os.PathError
	if !errors.As(err, &pathError) || scenarioSnapshotTransportError(err, false) || calls != 0 {
		t.Fatalf("invalid manifest path was reported as absent or retried: calls=%d error=%v", calls, err)
	}
}

func TestScenarioStatusRejectsEmptySuccessfulContractRead(t *testing.T) {
	t.Parallel()
	_, err := statusWithContractReader(t.Context(), testResolvedConfig(t), scenarioStatusRetryTestDirectory(t), func(context.Context, *ResolvedConfig, string, string) (*ContractView, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "returned no view") || scenarioSnapshotTransportError(err, false) {
		t.Fatalf("empty success gained deployment authority: %v", err)
	}
}
