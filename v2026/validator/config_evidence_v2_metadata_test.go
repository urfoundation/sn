//go:build linux || darwin

package validator

// Header capacity must agree with the real typed public reader and replicated
// sealer before a release configuration is admitted. No metadata cap is raised.

import (
	"strings"
	"testing"
)

// The exact existing metadata allowance is usable, while one extra advertised
// header byte is rejected by configuration and the actual publisher factory.
func TestReleaseEvidenceV2ConfigHeaderBoundMatchesActualPublicMetadata(t *testing.T) {
	t.Parallel()
	config := validReleaseConfig(t)
	bounds := config.EvidenceV2.Bounds
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	reader, err := NewHTTPAttemptStreamV2Reader(replicas[0].Origin, bounds.Cut)
	if err != nil {
		t.Fatal(err)
	}
	if bounds.Cut.MaxHeaderBytes != reader.metadataBytes {
		t.Fatal("generic release fixture advertises a different public header allowance")
	}
	if err := bounds.Validate(uint64(len(config.Operators))); err != nil {
		t.Fatalf("exact real public allowance was rejected: %v", err)
	}
	if _, err := newAttemptCutV2Replicas(bounds.Cut, replicas); err != nil {
		t.Fatalf("exact real public factory refused admitted bounds: %v", err)
	}
	bounds.Cut.MaxHeaderBytes = reader.metadataBytes + 1
	if err := bounds.Validate(uint64(len(config.Operators))); err == nil || !strings.Contains(err.Error(), "header allowance exceeds its public metadata bound") {
		t.Fatalf("configuration admitted a header above actual public metadata allowance: %v", err)
	}
	if _, err := newAttemptCutV2Replicas(bounds.Cut, replicas); err == nil || !strings.Contains(err.Error(), "replicated attempt header exceeds its public metadata bound") {
		t.Fatalf("real replica factory no longer refuses the same incompatible bound: %v", err)
	}
	for _, store := range stores {
		objects, writes, reads := store.snapshot()
		if len(objects) != 0 || writes != 0 || reads != 0 {
			t.Fatal("header capacity admission invoked a publisher or public reader")
		}
	}
}

// The real strict configuration loader preserves the exact coherent budget and
// refuses an impossible successor, rather than passing it to later startup I/O.
func TestReleaseEvidenceV2ConfigLoaderRejectsUnpublishableHeaderAllowance(t *testing.T) {
	t.Parallel()
	config := validReleaseConfig(t)
	loaded, err := LoadReleaseConfig(writeReleaseConfig(t, config))
	if err != nil || loaded == nil || loaded.EvidenceV2.Bounds != config.EvidenceV2.Bounds {
		t.Fatalf("coherent header budget changed in actual configuration loading: %v", err)
	}
	config.EvidenceV2.Bounds.Cut.MaxHeaderBytes++
	loaded, err = LoadReleaseConfig(writeReleaseConfig(t, config))
	if err == nil || loaded != nil || !strings.Contains(err.Error(), "header allowance exceeds its public metadata bound") {
		t.Fatalf("configuration loader returned an unpublishable header allowance: %v", err)
	}
}
