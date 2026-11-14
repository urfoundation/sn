//go:build linux || darwin

package validator

// This real M8 publication proves the normalized private configuration reaches
// both public header consumers without enlarging any page, data or trail cap.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseStartupV2FixturePublishesActualHeaderWithinMetadataAllowance(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	participant := fixture.disk.participants[0]
	bounds := fixture.cfg.EvidenceV2.Bounds
	options := AttemptCutV2ReplicaOptions{ReplayBounds: bounds.Replay, ScratchDirectory: filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "public-header"), ServerKeys: fixture.keys[participant.NoID], Replicas: fixture.replicas}
	publication, err := SealReplicatedAttemptCutV2(t.Context(), participant.Ledger, fixture.inputs[0].Context.InitialCut, fixture.cfg.Policy, fixture.inputs[0].PrivateKey, bounds.Cut, options)
	if err != nil || publication == nil {
		t.Fatalf("startup fixture real public sealer rejected its finite header budget: %v", err)
	}
	encoded, err := publication.Cut.CanonicalJSON(bounds.Cut)
	if err != nil || publication.Size != uint64(len(encoded)) || publication.Cut.RecordCount != 8 || publication.Cut.CompleteCount != 1 || publication.Cut.FailedCount != 0 {
		t.Fatalf("real header publication changed its genuine M8 census: %v", err)
	}
	var before [2][2]int
	var metadataBytes uint64
	for index, replica := range fixture.replicas {
		reader, err := NewHTTPAttemptStreamV2Reader(replica.Origin, bounds.Cut)
		if err != nil {
			t.Fatal(err)
		}
		if bounds.Cut.MaxHeaderBytes != reader.metadataBytes || publication.Size > reader.metadataBytes {
			t.Fatal("startup fixture header is outside the unchanged actual metadata allowance")
		}
		metadataBytes = reader.metadataBytes
		actual, err := reader.ReadMetadata(t.Context(), publication.ContentHash, publication.Size)
		if err != nil || !bytes.Equal(actual, encoded) {
			t.Fatalf("actual origin %d did not retain the exact complete signed header: %v", index, err)
		}
		_, before[index][0], before[index][1] = fixture.stores[index].snapshot()
	}
	// The same actual sealer refuses cap+1 before creating scratch or invoking
	// either immutable writer. A successful narrower fixture cannot waive it.
	incompatible := bounds.Cut
	incompatible.MaxHeaderBytes = metadataBytes + 1
	options.ScratchDirectory = filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "refused-header")
	publication, err = SealReplicatedAttemptCutV2(t.Context(), participant.Ledger, fixture.inputs[0].Context.InitialCut, fixture.cfg.Policy, fixture.inputs[0].PrivateKey, incompatible, options)
	if err == nil || publication != nil || !strings.Contains(err.Error(), "replicated attempt header exceeds its public metadata bound") {
		t.Fatalf("actual sealer admitted an incompatible header cap: %v", err)
	}
	for index, store := range fixture.stores {
		_, writes, reads := store.snapshot()
		if writes != before[index][0] || reads != before[index][1] {
			t.Fatal("refused header capacity reached a real public origin")
		}
	}
	if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused header capacity created replay scratch: %v", err)
	}
}
