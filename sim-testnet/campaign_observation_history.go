package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// An observation grows for the full release interval. Its owner-signed
// history can exceed the ordinary 32 MiB artifact limit without enlarging
// any artifact, plan, or published-envelope limit.
const maximumCampaignObservationHistoryBytes = 128 * 1024 * 1024

func readCampaignObservationHistory(stateDir, name string) ([]byte, error) {
	return readBoundedHistoricalFileObserved(stateDir, name, maximumCampaignObservationHistoryBytes, nil)
}

// Recovery binds the complete failed predecessor, including any suffix after
// the signed acceptance cut. Hash it in fixed-size chunks without allocating
// the whole historical log just to populate the successor's signed lineage.
func hashCampaignObservationHistory(stateDir, name string) (string, uint64, error) {
	if err := validateCampaignEvidencePath(name); err != nil {
		return "", 0, err
	}
	file, err := openFinalCollectedFile(stateDir, filepath.FromSlash(name))
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximumCampaignObservationHistoryBytes {
		return "", 0, fmt.Errorf("historical observation log %s is not regular or exceeds its %d-byte bound", name, maximumCampaignObservationHistoryBytes)
	}
	hash := sha256.New()
	count, err := io.CopyBuffer(hash, io.LimitReader(file, maximumCampaignObservationHistoryBytes+1), make([]byte, 1024*1024))
	if err != nil {
		return "", 0, err
	}
	after, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if count != before.Size() || count > maximumCampaignObservationHistoryBytes || after.Size() != before.Size() || !os.SameFile(before, after) {
		return "", 0, errors.New("historical observation log changed size or exceeded its bound")
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), uint64(count), nil
}
