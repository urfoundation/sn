//go:build darwin

// Darwin has no sealed anonymous executable or subreaper supervision, and the
// approved engine is a Linux ELF artifact. Replay refuses before any execution
// rather than running weaker, unpinned code.
package main

import (
	"context"
	"errors"
	"os"
)

func historicalReplayEngine(context.Context, planFileReference) (*os.File, error) {
	return nil, errors.New("historical replay requires the Linux sealed-engine supervisor")
}
