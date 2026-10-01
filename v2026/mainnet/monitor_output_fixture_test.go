// Existing command fixtures use completed output as a positive sample barrier.
// The real exporter now runs independently, so the injected wait joins that
// actual delivery before a fixture changes its clock or local RPC response.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"
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
func runMonitorTestWithClock(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
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
	return runMainWithMonitorHooks(ctx, args, output, stderr, now, hooks)
}

// Wall-clock fixtures keep the same real timing and merely join publication.
func runMonitorTest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runMonitorTestWithClock(ctx, args, stdout, stderr, time.Now)
}
