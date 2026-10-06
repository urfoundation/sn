//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
)

func runEconomicConservationRestore(_ context.Context, _ []string, _ io.Writer, stderr io.Writer, _ monitorServiceHooks) int {
	fmt.Fprintln(stderr, "economic conservation restore requires linux durable owner support")
	return 2
}
