//go:build linux || darwin

// Read-only completed-census discovery shares the same private descriptor
// ownership as audit discovery. Locators never authorize payloads or spending.
package validator

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// Enumerate every retained closed locator, including a delayed or unexpected
// future epoch. A bounded numeric probe loop could silently omit such files.
func DiscoverValidatorEvidencePublicationV2Manifests(ctx context.Context, stateDir string, bounds ReleaseEvidenceV2Bounds) ([]ValidatorEvidencePublicationV2Manifest, error) {
	return discoverValidatorEvidencePublicationV2Manifests(ctx, stateDir, bounds, nil)
}

// The per-call seam only brackets the genuine private reader; it cannot
// substitute metadata or bypass the charged outer inode/size witness.
func discoverValidatorEvidencePublicationV2Manifests(ctx context.Context, stateDir string, bounds ReleaseEvidenceV2Bounds, step func(operation, path string) error) (result []ValidatorEvidencePublicationV2Manifest, resultErr error) {
	if ctx == nil || bounds.MaxHistoryBytes == 0 || bounds.MaxParticipants == 0 {
		return nil, errors.New("closed publication discovery owner or bounds are absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	probe, err := ValidatorEvidencePublicationV2ManifestPath(stateDir, 0)
	if err != nil {
		return nil, err
	}
	if err := validateReleaseMeasurementInputV2Limit(bounds.MaxClosureBytes); err != nil {
		return nil, err
	}
	directory, err := openAttemptPrivateDirectory(filepath.Dir(probe))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, directory.check(), directory.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	remaining := bounds.MaxHistoryBytes
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.file.ReadDir(1)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		for _, entry := range entries {
			state, err := directory.stat(entry.Name())
			if err != nil || !state.regular() || state.uid != uint32(os.Geteuid()) || state.mode&0o077 != 0 || state.size <= 0 || uint64(state.size) > min(remaining, bounds.MaxClosureBytes) || len(result) >= 16384 {
				return nil, errors.Join(errors.New("closed publication discovery exceeds its private history census"), err)
			}
			remaining -= uint64(state.size)
			path := filepath.Join(directory.path, entry.Name())
			if step != nil {
				if err := step("before-read", path); err != nil {
					return nil, err
				}
			}
			manifest, err := ReadValidatorEvidencePublicationV2Manifest(ctx, path, bounds.MaxClosureBytes, bounds.MaxParticipants)
			if err != nil {
				return nil, err
			}
			if step != nil {
				if err := step("after-read", path); err != nil {
					return nil, err
				}
			}
			after, err := directory.stat(entry.Name())
			if err != nil || after != state {
				return nil, errors.Join(errors.New("closed publication changed during bounded discovery"), err)
			}
			if err := directory.check(); err != nil {
				return nil, err
			}
			result = append(result, *manifest)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	slices.SortFunc(result, func(left, right ValidatorEvidencePublicationV2Manifest) int {
		if left.Epoch < right.Epoch {
			return -1
		}
		if left.Epoch > right.Epoch {
			return 1
		}
		return 0
	})
	return result, nil
}
