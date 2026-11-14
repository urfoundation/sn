//go:build !linux && !darwin

package main

import (
	"context"
	"fmt"
	"io"
)

// The physical daemon inspection backend is Linux- and macOS-scoped.
func runValidatorCapacityPreview(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "validator capacity preview requires the qualified Linux or macOS physical custody backend")
	return 2
}

func runValidatorCapacityConfig(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "capacity document completion is not qualified on this platform")
	return 2
}
