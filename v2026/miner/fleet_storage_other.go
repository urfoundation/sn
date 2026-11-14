//go:build !linux && !darwin

// Physical preparation is qualified on Linux and macOS only, and mainnet fleet
// writes refuse elsewhere. No portable fallback enrolls custody.
package miner

import (
	"context"
	"errors"
	"io"

	"github.com/docopt/docopt-go"
)

// No portable fallback can turn an unsupported host into physical preparation.
func fleetStorage(context.Context, docopt.Opts, io.Writer, io.Writer) error {
	return errors.New("fleet storage preparation requires the qualified Linux or macOS physical-custody profile")
}
