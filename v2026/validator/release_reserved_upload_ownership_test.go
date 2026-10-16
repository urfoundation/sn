//go:build linux || darwin

package validator

// Real startup/source owners remain authentic after session withdrawal. These
// checks cannot authorize a write, and malformed retained ownership stays hard.

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestReleaseReservedUploadOwnershipSurvivesWithdrawnSessions(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	cfg := &fixture.runtime.cfg
	origins, runtimes := fixture.origins, fixture.runtimes
	admitted, err := releaseReservedAttemptCensusReplicasV2(cfg, origins, runtimes)
	if err != nil {
		t.Fatal(err)
	}
	before := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	for _, runtime := range runtimes {
		runtime.attemptUpload.close()
	}
	if err := validateReleaseReservedAttemptCensusOwnershipV2(cfg, origins, runtimes); err != nil {
		t.Fatalf("withdrawal invalidated retained source ownership: %v", err)
	}
	if _, err := releaseReservedAttemptCensusReplicasV2(cfg, origins, runtimes); !errors.Is(err, context.Canceled) {
		t.Fatalf("withdrawn sessions regained publication admission: %v", err)
	}
	for _, replicas := range admitted {
		for _, replica := range replicas {
			if err := replica.WriteMetadata(t.Context(), "synthetic-unpublished-object", []byte("synthetic retained bytes")); !errors.Is(err, context.Canceled) {
				t.Fatalf("previously admitted callback escaped original session withdrawal: %v", err)
			}
		}
	}
	for _, fault := range []string{"missing-source", "different-source", "private-key", "writer", "route", "bounds"} {
		owners := slices.Clone(runtimes)
		owner := *owners[0]
		source, upload := *owner.attemptSource, *owner.attemptUpload
		owner.attemptSource, owner.attemptUpload, owners[0] = &source, &upload, &owner
		switch fault {
		case "missing-source":
			owner.attemptSource = nil
		case "different-source":
			source.activation.NoID++
		case "private-key":
			source.privateKey[0] ^= 1
		case "writer":
			upload.writer = nil
		case "route":
			upload.origin = "https://synthetic-other.example"
		case "bounds":
			upload.maxTransitionBytes++
		}
		err := validateReleaseReservedAttemptCensusOwnershipV2(cfg, origins, owners)
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("withdrawal hid %s retained integrity failure: %v", fault, err)
		}
	}
	for index, store := range fixture.stores {
		if !reflect.DeepEqual(before[index], store.counts()) {
			t.Fatal("retained ownership validation or closed callback published API bytes")
		}
	}
}
