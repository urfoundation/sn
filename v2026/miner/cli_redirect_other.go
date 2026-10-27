//go:build !linux && !darwin && !windows

package miner

import (
	"errors"
	"os"
)

// Elsewhere provider start keeps the miner's usual output.
func redirectStderrToFile(string) (*os.File, error) {
	return nil, errors.New("stderr redirection is not supported here")
}
