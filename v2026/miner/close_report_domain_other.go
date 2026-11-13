//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly)

// A platform without the admitted no-follow descriptor path leaves this
// optional evidence unavailable; ordinary providing remains supported.
package miner

import (
	"errors"
	"os"
)

// Do not replace a missing safe primitive with a potentially blocking open.
func openProviderCloseReportDomain(string) (*os.File, error) {
	return nil, errors.New("optional close-report domain descriptor admission is unsupported on this platform")
}
