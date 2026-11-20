//go:build !linux && !darwin

package validator

// Constructor-owned native runtime is unavailable, never a pathname fallback.

import (
	"context"
	"errors"
)

// A legacy-created store cannot acquire v2 ownership through these stubs.
func witnessHeadEMAStoreV2Runtime(ctx context.Context, path string, namespace headEMAStoreV2Namespace, hooks headEMAStoreV2RuntimeHooks) error {
	return errors.New("bounded head EMA runtime requires Linux or Darwin")
}

// Unsupported platforms never publish or silently downgrade custody.
func writeHeadEMAStoreV2(ctx context.Context, path string, encoded []byte, prior headEMAStoreV2Namespace, limits HeadEMAStoreV2Limits, hooks headEMAStoreV2RuntimeHooks) (headEMAStoreV2Namespace, bool, error) {
	return headEMAStoreV2Namespace{}, false, errors.New("bounded head EMA runtime requires Linux or Darwin")
}
