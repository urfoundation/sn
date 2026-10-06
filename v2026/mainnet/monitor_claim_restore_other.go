//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
)

func runMonitorClaimArchiveRestoreRequest(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "Claim history restore requires the qualified Linux storage profile")
	return 2
}

func runMonitorClaimArchiveRestoreCohort(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "Claim history cohort restore requires the qualified Linux storage profile")
	return 2
}
