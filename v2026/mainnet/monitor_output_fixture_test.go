// Existing command fixtures use completed output as a positive sample barrier.
// The real exporter now runs independently, so the injected wait joins that
// actual delivery before a fixture changes its clock or local RPC response.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Only the existing finite in-memory fixture writers are accepted here. This
// adapter is not production admission for an arbitrary synchronous Writer.
type monitorFixtureOutput struct {
	writer    io.Writer
	completed chan struct{}
}

// Required by the command signature; the exporter uses WriteContext instead.
func (self *monitorFixtureOutput) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}

// These fixture callbacks return immediately after bounded memory operations.
// Blocking sink tests use actual pipes or a separate cancellation-aware sink.
func (self *monitorFixtureOutput) WriteContext(ctx context.Context, raw []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	switch self.writer.(type) {
	case *bytes.Buffer, *cancelMonitorWriter, *outageMonitorWriter, *monitorMetricsTestWriter:
	default:
		return 0, errors.New("fixture writer has no finite memory contract")
	}
	n, err := self.writer.Write(raw)
	if err == nil {
		select {
		case self.completed <- struct{}{}:
		case <-ctx.Done():
			return n, ctx.Err()
		}
	}
	return n, err
}

// Preserve exact old barriers while exercising the public command and exporter.
func runMonitorTestWithClock(t *testing.T, ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	t.Helper()
	return prepareMonitorTestWithClock(t, ctx, args, stdout, stderr, now)()
}

// Admission runs on the caller before the returned command may own a worker.
func prepareMonitorTestWithClock(t *testing.T, ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) func() int {
	t.Helper()
	ctx = monitorTestStorageContext(t, ctx, args)
	output := &monitorFixtureOutput{writer: stdout, completed: make(chan struct{}, 1)}
	hooks := monitorServiceHooks{wait: func(ctx context.Context, _ string, duration time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-output.completed:
		}
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		}
	}}
	return func() int { return runMainWithMonitorHooks(ctx, args, output, stderr, now, hooks) }
}

// Wall-clock fixtures keep the same real timing and merely join publication.
func runMonitorTest(t *testing.T, ctx context.Context, args []string, stdout, stderr io.Writer) int {
	t.Helper()
	return runMonitorTestWithClock(t, ctx, args, stdout, stderr, time.Now)
}

// Instance-only facts accompany command hooks without changing runtime parsing.
func runMonitorStorageTestWithHooks(t *testing.T, ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) int {
	t.Helper()
	return runMainWithMonitorHooks(monitorTestStorageContext(t, ctx, args), args, stdout, stderr, now, hooks)
}

// Only test callers provision declarations; command arguments remain exact so
// aliases, collisions and missing parents still reach production validation.
func monitorTestStorageContext(t *testing.T, ctx context.Context, args []string) context.Context {
	t.Helper()
	if _, present := durablevolume.ReferenceFromContext(ctx); present {
		return ctx
	}
	var roots []string
	values := map[string]string{}
	for index, arg := range args {
		key, value, assigned := strings.Cut(arg, "=")
		if !assigned && index+1 < len(args) {
			value = args[index+1]
		}
		values[key] = value
	}
	provisionMonitorTestCustody(t, values["--checkpoint"])
	if policyBytes, err := os.ReadFile(values["--services"]); err == nil {
		var policy monitorServicesPolicy
		if json.Unmarshal(policyBytes, &policy) == nil {
			for _, role := range policy.Validators {
				path, _ := monitorValidatorPaths(values["--checkpoint"], values["--metrics-file"], role.Role)
				provisionMonitorTestCustody(t, path)
			}
			for _, role := range policy.Operators {
				path, _ := monitorOperatorPaths(values["--checkpoint"], values["--metrics-file"], role.Role)
				provisionMonitorTestCustody(t, path)
			}
			for _, role := range policy.Providers {
				path, _ := monitorProviderPaths(values["--checkpoint"], values["--metrics-file"], role.Role)
				provisionMonitorTestCustody(t, path)
			}
			for _, role := range policy.Claims {
				path, _ := monitorClaimPaths(values["--checkpoint"], values["--metrics-file"], role.Role)
				provisionMonitorTestCustody(t, path)
			}
		}
	}
	for index, arg := range args {
		key, path, assigned := strings.Cut(arg, "=")
		if key != "--checkpoint" && key != "--metrics-file" {
			continue
		}
		if !assigned && index+1 < len(args) {
			path = args[index+1]
		}
		root, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			continue
		}
		if info, err := os.Lstat(root); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return ctx
	}
	return durablefixture.New(t, ctx, roots...).Context
}
