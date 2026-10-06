//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
)

// The physical daemon inspection backend is explicitly Linux-scoped.
func runValidatorCapacityPreview(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "validator capacity preview requires the qualified Linux physical custody backend")
	return 2
}

func runValidatorCapacityConfig(_ context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "capacity document completion is not qualified on this platform")
	return 2
}
