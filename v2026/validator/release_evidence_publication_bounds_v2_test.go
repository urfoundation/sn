//go:build linux || darwin

// Typed canonical Json preserves the protocol's complete unsigned integer
// range; a private locator must not impose a floating-point wire limit.
package validator

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

func TestValidatorEvidencePublicationV2ManifestPreservesProtocolUint64Bounds(t *testing.T) {
	for _, epoch := range []uint64{0, 9007199254740992, ^uint64(0)} {
		stateDir := filepath.Join(t.TempDir(), "state")
		path, err := ValidatorEvidencePublicationV2ManifestPath(stateDir, epoch)
		if err != nil {
			t.Fatal(err)
		}
		manifest := ValidatorEvidencePublicationV2Manifest{Schema: ValidatorEvidencePublicationV2Schema, Kind: protocol.ValidatorEvidenceClosedCensus, Epoch: epoch,
			Origins: [2]string{"https://one.example", "https://two.example"}, CensusHash: [32]byte{1}, CensusBytes: 1,
			Members: []ValidatorEvidencePublicationV2MemberReference{{NoId: ^uint64(0), SignedArtifactHash: [32]byte{2}, SignedArtifactBytes: 1}}}
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := writeReleaseMeasurementInputV2Context(t.Context(), path, append(encoded, '\n'), 4096, releaseMeasurementInputV2ReadHooks{}); err != nil {
			t.Fatal(err)
		}
		retained, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, 4096, 1)
		if err != nil || retained.Epoch != epoch || retained.Members[0].NoId != ^uint64(0) {
			t.Fatalf("protocol-sized typed locator was narrowed or rounded: %v", err)
		}
	}
}
