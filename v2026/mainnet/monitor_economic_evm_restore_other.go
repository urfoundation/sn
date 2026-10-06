//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
)

func runMonitorEvmArchiveRestoreRequest(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "EVM history restore requires the qualified Linux storage profile")
	return 2
}

func runMonitorEvmArchiveRestoreCohort(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "EVM history cohort restore requires the qualified Linux storage profile")
	return 2
}
