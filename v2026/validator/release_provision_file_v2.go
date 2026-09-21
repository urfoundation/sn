//go:build linux || darwin

// Setup uses the same bounded immutable descriptor owner as release inputs.
// These bytes remain discovery until actual startup authenticates both chains.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
)

// Never replaces an existing file. Exact retries still recheck the private
// physical descriptor and final close before returning its pinned reference.
func WriteReleaseEvidenceV2File(ctx context.Context, path string, encoded []byte, maximumBytes uint64) (ReleaseEvidenceV2File, error) {
	if err := writeReleaseMeasurementInputV2Context(ctx, path, encoded, maximumBytes, releaseMeasurementInputV2ReadHooks{}); err != nil {
		return ReleaseEvidenceV2File{}, err
	}
	value := ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(encoded)), SHA256: attemptHex32(sha256.Sum256(encoded))}
	if _, err := ReadReleaseEvidenceV2File(ctx, value, maximumBytes); err != nil {
		return ReleaseEvidenceV2File{}, err
	}
	return value, nil
}

// Setup's fixed plan-owned filenames can be discovered before a hash exists.
// The caller must independently authenticate every decoded field after reading.
func ReadReleaseEvidenceV2SetupFile(ctx context.Context, path string, maximumBytes uint64) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("release evidence setup read context is absent")
	}
	return readReleaseMeasurementInputV2Context(ctx, path, maximumBytes, releaseMeasurementInputV2ReadHooks{})
}
