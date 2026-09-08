//go:build !linux && !darwin

package validator

// No pathname-only fallback may claim the bounded native startup contract.

import (
	"context"
	"errors"
)

// Native no-follow startup custody has only Linux and Darwin implementations;
// unsupported hosts must refuse activation without a pathname fallback.
func NewHeadEMAStoreV2(ctx context.Context, stateDir string, limits HeadEMAStoreV2Limits) (*HeadEMAStore, error) {
	return nil, errors.New("bounded head EMA startup requires Linux or Darwin")
}
