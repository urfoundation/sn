//go:build linux || darwin

// Missing originals, incomplete lanes and signature conflicts remain distinct
// from a known empty full registry through the actual public/cold reader.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// A genuine empty cut for every independently registered owner is proven zero,
// while original policy supplies reliability and no current HTTP is needed later.
func TestProviderAttemptSourceActualEmptyRegistryColdReplay(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	raw, result, err := fixture.source.Read(t.Context(), fixture.artifact)
	if err != nil || result == nil || !result.CutCensusComplete || !result.OwnedRequestsComplete || result.Validators != 2 || result.OperatorLanes != 4 || len(result.Providers) != 0 || result.ReliabilityAMin == 0 || result.OriginalHash != sha256.Sum256(raw) {
		t.Fatalf("complete actual empty originals: %+v %v", result, err)
	}
	fixture.stateLock.Lock()
	fixture.closed = true
	reads := fixture.reads
	fixture.stateLock.Unlock()
	cold, err := fixture.source.VerifyRetained(t.Context(), fixture.artifact, raw)
	if err != nil || cold == nil || cold.OriginalHash != result.OriginalHash || cold.WindowHash != result.WindowHash || !cold.OwnedRequestsComplete {
		t.Fatalf("cold original replay changed: %+v %v", cold, err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if fixture.reads != reads {
		t.Fatal("cold replay consulted a live source")
	}
}

// Original first-response exposure survives a failed next send, and the
// never-received next request needs its separately signed durable fence.
func TestProviderAttemptSourceActualFailedExposureAndIdleOwners(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, true)
	raw, result, err := fixture.source.Read(t.Context(), fixture.artifact)
	if err != nil || result == nil || !result.OwnedRequestsComplete {
		t.Fatalf("actual failed/idle originals: %+v %v", result, err)
	}
	var assignments, confirmations uint64
	for _, row := range result.Providers {
		assignments += row.Assignments
		confirmations += row.Confirmations
	}
	if assignments != 1 || confirmations != 0 || result.FailedTrails != 1 || result.Validators != 2 || result.OperatorLanes != 4 {
		t.Fatalf("original failed exposure was lost or idle owner invented %+v", result)
	}
	var original ProviderAttemptOriginal
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	var received, closed int
	for _, response := range original.Receipts {
		if response.Receipt != nil {
			received++
		}
		if response.ClosedUnreceived != nil {
			closed++
		}
	}
	if received != 1 || closed != 1 {
		t.Fatalf("actual original/fenced request census %d/%d", received, closed)
	}
	if cold, err := fixture.source.VerifyRetained(t.Context(), fixture.artifact, raw); err != nil || cold == nil || cold.WindowHash != result.WindowHash {
		t.Fatal("failed original cold replay", err)
	}
}

// Removing a whole idle lane must not turn a complete registry into a subset.
func TestProviderAttemptSourceMissingRequestLaneIsUnknown(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	fixture.response.Requests = fixture.response.Requests[:len(fixture.response.Requests)-1]
	if raw, result, err := fixture.source.Read(t.Context(), fixture.artifact); !errors.Is(err, protocol.ErrProviderAttemptsUnavailable) || raw != nil || result != nil {
		t.Fatalf("missing idle lane became complete: %v", err)
	}
}

// Duplicate lane/cut data is contradictory original identity, not known zero.
func TestProviderAttemptSourceDuplicateLaneIsIntegrity(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	fixture.response.Requests = append(fixture.response.Requests, fixture.response.Requests[0])
	if raw, result, err := fixture.source.Read(t.Context(), fixture.artifact); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || raw != nil || result != nil {
		t.Fatalf("duplicate original lane admitted: %v", err)
	}
}

// A changed independent authority cannot borrow an otherwise valid cut set.
func TestProviderAttemptSourceForeignAuthorityAndWindowRefused(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	fixture.response.Authority.Signature[0] ^= 1
	if raw, result, err := fixture.source.Read(t.Context(), fixture.artifact); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || raw != nil || result != nil {
		t.Fatalf("foreign window authority admitted: %v", err)
	}
	fixture.resign(t)
	changed := *fixture.artifact
	changed.End.Number++
	if raw, result, err := fixture.source.Read(t.Context(), &changed); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || raw != nil || result != nil {
		t.Fatalf("cross-window authority admitted: %v", err)
	}
}

// Even an independently signed declaration cannot choose a convenient Server
// clock boundary whose original canonical header hash differs from the artifact.
func TestProviderAttemptSourceOriginalClockCannotBeReplaced(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	var header map[string]any
	if err := json.Unmarshal(fixture.response.Authority.StartHeader, &header); err != nil {
		t.Fatal(err)
	}
	header["timestamp"] = "0x1"
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	fixture.response.Authority.StartHeader = raw
	fixture.resign(t)
	if raw, result, err := fixture.source.Read(t.Context(), fixture.artifact); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || raw != nil || result != nil {
		t.Fatalf("clock substitution admitted: %v", err)
	}
}

// A cold archive retains exact bytes, not a previous producer's success flag.
func TestProviderAttemptSourceColdOriginalMutationAndMissingResponseRefused(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, true)
	raw, _, err := fixture.source.Read(t.Context(), fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	var original ProviderAttemptOriginal
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	copy := original
	copy.Receipts = copy.Receipts[:1]
	missing, err := json.Marshal(copy)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.source.VerifyRetained(t.Context(), fixture.artifact, missing); !errors.Is(err, protocol.ErrProviderAttemptsUnavailable) || result != nil {
		t.Fatalf("missing response became no exposure: %v", err)
	}
	original.Objects[0].Body[0] ^= 1
	changed, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.source.VerifyRetained(t.Context(), fixture.artifact, changed); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || result != nil {
		t.Fatalf("changed original body admitted: %v", err)
	}
}

// Owner cancellation survives both entry points without a manufactured source
// conflict or returning previously staged complete counts.
func TestProviderAttemptSourceCancellationReturnsNoAuthority(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if raw, result, err := fixture.source.Read(ctx, fixture.artifact); !errors.Is(err, context.Canceled) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || raw != nil || result != nil {
		t.Fatalf("canceled original read changed classification: %v", err)
	}
	if result, err := fixture.source.VerifyRetained(ctx, fixture.artifact, json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("canceled cold read returned authority: %v", err)
	}
}

// Explicit full-owner limits reject the independently impossible 1TiB+1TiB
// combination without changing generic Core inventory capacity.
func TestProviderAttemptRequestAggregateRootCapacity(t *testing.T) {
	limits := attemptLedgerDiskTestLimits()
	request := ProviderAttemptRequestLimits{MaxRecords: 1000, MaxRecordBytes: 8192, MaxJournalBytes: 1024 * 1024}
	if err := ValidateProviderAttemptRequestRootCapacity(limits, request); err != nil {
		t.Fatal(err)
	}
	limits.MaxStorageBytes = 1024 * 1024 * 1024 * 1024
	if err := ValidateProviderAttemptRequestRootCapacity(limits, request); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatal("impossible combined root admitted", err)
	}
}

// Optional export is a real selected test result, not an author-created success
// artifact. Sol supplies an owned directory and retains all producer bindings.
func TestProviderAttemptSourceExportsActualFailedIdleOriginals(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, true)
	raw, result, err := fixture.source.Read(t.Context(), fixture.artifact)
	if err != nil || result == nil || !result.OwnedRequestsComplete {
		t.Fatal("actual original export", err)
	}
	root := os.Getenv("URNETWORK_PROVIDER_ATTEMPT_FIXTURE_DIR")
	if root == "" {
		return
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		t.Fatal("original export directory must be absolute")
	}
	if err := os.Mkdir(root, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("original export directory is not protected", err)
	}
	artifact, err := json.Marshal(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"authority.json": fixture.authorityRaw, "artifact.json": artifact, "original.json": raw} {
		mode := os.FileMode(0400)
		if name == "authority.json" {
			mode = 0600
		}
		file, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(data)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("original export readback", err)
		}
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		t.Fatal(err)
	}
}
