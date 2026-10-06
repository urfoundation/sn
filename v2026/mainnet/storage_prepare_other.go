//go:build !linux

// Physical preparation currently has Linux qualification only. Portable owner
// inspection commands remain separate and never enroll custody through fallback.
package main

import (
	"context"
	"fmt"
	"io"
)

// No portable fallback can turn an unsupported host into physical preparation.
func runStoragePreparationCommand(_ context.Context, _ []string, _ io.Writer, stderr io.Writer, _ bool) int {
	fmt.Fprintln(stderr, "storage preparation requires the qualified Linux physical-custody profile")
	return 2
}
