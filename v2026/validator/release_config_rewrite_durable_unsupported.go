//go:build !linux && !darwin

package validator

import (
	"context"
	"errors"
	"os"
)

// Unsupported hosts cannot fall back to a pathname publication.
func rewriteReleaseConfigDurable(context.Context, string, []byte, []byte, os.FileMode) error {
	return errors.New("guarded activation configuration publication is unsupported on this platform")
}
