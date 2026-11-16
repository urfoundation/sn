// The executable supplies cancellation and exit status to the roster library.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfoundation/sn/v2026/payoutroster"
)

// Interrupt and service stop both cancel the active operation and inbox poll.
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := payoutroster.RunCommand(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "payoutroster:", err)
		os.Exit(1)
	}
}
