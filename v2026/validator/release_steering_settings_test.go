package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"gopkg.in/yaml.v3"
)

func releaseSteeringSettingsTestContext(t *testing.T, args ...string) context.Context {
	t.Helper()
	opts, err := parseValidatorArgsForTest(t, append([]string{"run", "--config=/tmp/synthetic-release.yml"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	options, err := settlementV2RuntimeOptionsFromOpts(opts)
	if err != nil {
		t.Fatal(err)
	}
	return WithSettlementV2RuntimeOptions(t.Context(), options)
}

// These serial stages exceed the former transport-derived ten-minute total.
// The actual configured operation owner must leave time for every consumer;
// this test does not replace or grant any cryptographic replay verdict.
func TestReleaseSteeringBudgetCompletesSerialPreparation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := releaseSteeringSettingsTestContext(t)
		started, completed := time.Now(), 0
		stages := []time.Duration{180 * time.Second, 480 * time.Second, 30 * time.Second, 120 * time.Second, 60 * time.Second, 90 * time.Second, 120 * time.Second, 120 * time.Second}
		err := runConfiguredReleaseSteeringOperation(ctx, func(operation context.Context) error {
			for _, stage := range stages {
				select {
				case <-operation.Done():
					return operation.Err()
				case <-time.After(stage):
					completed++
				}
			}
			return nil
		})
		if err != nil || completed != len(stages) || time.Since(started) != 20*time.Minute || ctx.Err() != nil {
			t.Fatalf("healthy serial preparation truncated: completed=%d elapsed=%s error=%v", completed, time.Since(started), err)
		}
	})
}

// An explicit operator override reaches the exact owner used by production.
func TestReleaseSteeringBudgetOverrideReachesOperationOwner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := releaseSteeringSettingsTestContext(t, "--steering-operation-timeout=35m")
		started := time.Now()
		err := runConfiguredReleaseSteeringOperation(ctx, func(operation context.Context) error {
			deadline, bounded := operation.Deadline()
			if !bounded || deadline.Sub(started) != 35*time.Minute {
				t.Fatal("runtime override did not reach the production operation owner", deadline, bounded)
			}
			select {
			case <-operation.Done():
				return operation.Err()
			case <-time.After(31 * time.Minute):
				return nil
			}
		})
		if err != nil || time.Since(started) != 31*time.Minute || ctx.Err() != nil {
			t.Fatal("explicit finite budget was ignored", time.Since(started), err)
		}
	})
}

// A stalled operation is still joined when the finite default expires.
func TestReleaseSteeringBudgetExhaustionJoinsOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := releaseSteeringSettingsTestContext(t)
		started, joined := time.Now(), false
		err := runConfiguredReleaseSteeringOperation(ctx, func(operation context.Context) error {
			defer func() { joined = true }()
			<-operation.Done()
			return operation.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) || !joined || time.Since(started) != DefaultReleaseSteeringSettings().OperationTimeout || ctx.Err() != nil {
			t.Fatal("finite operation deadline did not join its owner", time.Since(started), joined, err)
		}
	})
}

func TestReleaseSteeringBudgetEarlierCallerDeadlineWins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(releaseSteeringSettingsTestContext(t, "--steering-operation-timeout=35m"), 7*time.Minute)
		defer cancel()
		started, joined := time.Now(), false
		err := runConfiguredReleaseSteeringOperation(ctx, func(operation context.Context) error {
			defer func() { joined = true }()
			<-operation.Done()
			return operation.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) || !joined || time.Since(started) != 7*time.Minute || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal("operation outlived its caller deadline", time.Since(started), joined, err)
		}
	})
}

func TestReleaseSteeringBudgetServiceCancellationJoinsOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(releaseSteeringSettingsTestContext(t))
		defer cancel()
		entered, result := make(chan struct{}), make(chan error, 1)
		joined := false
		go func() {
			result <- runConfiguredReleaseSteeringOperation(ctx, func(operation context.Context) error {
				defer func() { joined = true }()
				close(entered)
				<-operation.Done()
				return operation.Err()
			})
		}()
		<-entered
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) || !joined {
			t.Fatal("service cancellation did not join the owned operation", joined, err)
		}
	})
}

func TestReleaseSteeringBudgetFlagsRefuseInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "0s", "-1s", "4m59s", "unbounded", "999999999999999h"} {
		opts, err := parseValidatorArgsForTest(t, []string{"run", "--steering-operation-timeout=" + value})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := settlementV2RuntimeOptionsFromOpts(opts); err == nil {
			t.Errorf("invalid operation budget %q accepted", value)
		}
	}
	ctx := releaseSteeringSettingsTestContext(t, "--steering-operation-timeout=5m")
	if got := releaseSteeringOperationTimeout(ctx); got != 5*time.Minute {
		t.Fatal("minimum operation budget was changed", got)
	}
}

func TestReleaseSteeringBudgetLeavesSignedConfigUnchanged(t *testing.T) {
	cfg := &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion, DeploymentID: "synthetic-steering-budget", PollSeconds: 10}
	beforeJson, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	beforeYaml, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash, err := OwnerRecycleConfigHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--steering-operation-timeout=35m"}} {
		ctx := releaseSteeringSettingsTestContext(t, args...)
		if err := runConfiguredReleaseSteeringOperation(ctx, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	afterJson, jsonErr := json.Marshal(cfg)
	afterYaml, yamlErr := yaml.Marshal(cfg)
	afterHash, hashErr := OwnerRecycleConfigHash(cfg)
	if err := errors.Join(jsonErr, yamlErr, hashErr); err != nil || !bytes.Equal(beforeJson, afterJson) || !bytes.Equal(beforeYaml, afterYaml) || beforeHash != afterHash || bytes.Contains(afterJson, []byte("steering_operation_timeout")) {
		t.Fatal("runtime operation budget changed signed configuration", err)
	}
}
