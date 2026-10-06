//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
)

// Physical restore request admission has no portable enrollment fallback.
func runMonitorNativeArchiveRestoreRequest(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "native history restore requires the qualified Linux storage profile")
	return 2
}

// Cross-root restoration retains the same qualified physical platform boundary.
func runMonitorNativeArchiveRestoreCohort(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "native history cohort restore requires the qualified Linux storage profile")
	return 2
}
