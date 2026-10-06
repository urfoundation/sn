//go:build !linux

package durableinspect

import (
	"context"
	"errors"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func Inspect(context.Context, durablevolume.Reference, []string) (Report, error) {
	return Report{}, errors.New("daemon storage inspection requires the qualified Linux host profile")
}

func orderedPaths([]string) ([]string, error) {
	return nil, errors.New("daemon storage inspection requires the qualified Linux host profile")
}
