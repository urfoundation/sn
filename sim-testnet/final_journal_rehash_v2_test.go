//go:build linux || darwin

// Final rehash retains the initial journal's exact byte owner, while ordinary
// sources and untrusted aliases remain within their independent capacities.
package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// A real complete chain crosses the ordinary 64 MiB intent owner. Final rehash
// must read it again, and a byte changed after initial replay must still fail.
func TestFinalJournalRehashRetainsDedicatedCapacityAndExactBytes(t *testing.T) {
	const intentLimit = 64 * 1024 * 1024
	raw := finalJournalCapacityFixture(t, 65, 1024*1024)
	if len(raw) <= intentLimit || len(raw) >= maximumFinalJournalBytes {
		t.Fatal("journal fixture did not cross the independent replay owner")
	}
	digest := bytesSHA256(raw)
	source := FinalCollectedValidatorSourceV2{
		Source:   validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-journal", Name: "journal.jsonl"},
		Artifact: FinalArtifactLocator{Kind: "validator-evidence-v2-source", URI: finalJournalCapturePrefixV2 + strings.TrimPrefix(digest, "sha256:") + ".jsonl", ContentHash: digest, SizeBytes: uint64(len(raw))},
	}
	reads := 0
	load := func(ctx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		reads++
		if locator != source.Artifact {
			return nil, errors.New("foreign journal locator")
		}
		return raw, ctx.Err()
	}
	initial, err := loadFinalCapturedSourceV2(t.Context(), load, source, intentLimit)
	if err != nil || !bytes.Equal(initial, raw) || reads != 1 {
		t.Fatal("initial journal read lost exact byte ownership", err)
	}
	if err := validateFinalJournalArtifactBytes(source.Artifact.URI, initial); err != nil {
		t.Fatal("fixture was not a complete authenticated journal", err)
	}
	if err := rehashFinalCapturedSourcesV2(t.Context(), load, []FinalCollectedValidatorSourceV2{source}, intentLimit); err != nil || reads != 2 {
		t.Fatalf("final rehash rejected an admitted journal: reads=%d error=%v", reads, err)
	}
	// No new allocation or concurrent write is needed to force this exact
	// between-read mutation; the completed initial read is the explicit barrier.
	raw[len(raw)/2] ^= 1
	if err := rehashFinalCapturedSourcesV2(t.Context(), load, []FinalCollectedValidatorSourceV2{source}, intentLimit); err == nil || reads != 3 {
		t.Fatalf("final rehash trusted an earlier success: reads=%d error=%v", reads, err)
	}
}

// Metadata admission is tested at the exact capacity without allocating an
// oversized body. Wrong identities, generic aliases and overages fail pre-read.
func TestFinalJournalRehashRefusesForeignOrOversizedOwnersBeforeRead(t *testing.T) {
	const intentLimit = 64 * 1024 * 1024
	digest := "sha256:" + strings.Repeat("12", 32)
	base := FinalCollectedValidatorSourceV2{
		Source:   validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-journal", Name: "journal.jsonl"},
		Artifact: FinalArtifactLocator{Kind: "validator-evidence-v2-source", URI: finalJournalCapturePrefixV2 + strings.TrimPrefix(digest, "sha256:") + ".jsonl", ContentHash: digest, SizeBytes: maximumFinalJournalBytes},
	}
	reads := 0
	reached := errors.New("synthetic loader reached its exact byte owner")
	load := func(context.Context, FinalArtifactLocator) ([]byte, error) {
		reads++
		return nil, reached
	}
	if err := rehashFinalCapturedSourcesV2(t.Context(), load, []FinalCollectedValidatorSourceV2{base}, intentLimit); !errors.Is(err, reached) || reads != 1 {
		t.Fatal("exact journal capacity did not reach the bounded reader", err)
	}
	for _, kind := range []string{"oversized", "ordinary", "name", "origin", "hash", "generic-journal", "generic-proof"} {
		source := base
		switch kind {
		case "oversized":
			source.Artifact.SizeBytes++
		case "ordinary":
			source.Source.Kind = "private"
		case "name":
			source.Source.Name = "other.jsonl"
		case "origin":
			source.Source.Origin = "https://synthetic.example"
		case "hash":
			source.Artifact.ContentHash = "sha256:" + strings.Repeat("34", 32)
		case "generic-journal", "generic-proof":
			source.Artifact.SizeBytes = intentLimit + 1
			source.Artifact.URI = "final-inputs/validators/v2/" + strings.TrimPrefix(digest, "sha256:") + ".bin"
			if kind == "generic-proof" {
				source.Source.Kind, source.Source.Name = "private", "steering-intents.json"
			}
		}
		before := reads
		if err := rehashFinalCapturedSourcesV2(t.Context(), load, []FinalCollectedValidatorSourceV2{source}, intentLimit); err == nil || errors.Is(err, reached) || reads != before {
			t.Fatalf("%s borrowed journal capacity: reads=%d error=%v", kind, reads, err)
		}
	}
}

// The common rehash path still reads ordinary sources and honors cancellation;
// journal capacity never converts a successful early read into a cache waiver.
func TestFinalJournalRehashRetainsOrdinarySourcesAndCancellation(t *testing.T) {
	raw := []byte("synthetic ordinary source")
	digest := bytesSHA256(raw)
	source := FinalCollectedValidatorSourceV2{
		Source:   validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "steering-intents.json"},
		Artifact: FinalArtifactLocator{Kind: "validator-evidence-v2-source", URI: "final-inputs/validators/v2/" + strings.TrimPrefix(digest, "sha256:") + ".bin", ContentHash: digest, SizeBytes: uint64(len(raw))},
	}
	reads := 0
	load := func(ctx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		reads++
		if locator != source.Artifact {
			return nil, errors.New("foreign ordinary source")
		}
		return raw, ctx.Err()
	}
	if err := rehashFinalCapturedSourcesV2(t.Context(), load, []FinalCollectedValidatorSourceV2{source}, 64*1024*1024); err != nil || reads != 1 {
		t.Fatal("ordinary source stopped rehashing", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := rehashFinalCapturedSourcesV2(canceled, load, []FinalCollectedValidatorSourceV2{source}, 64*1024*1024); !errors.Is(err, context.Canceled) || reads != 1 {
		t.Fatal("canceled rehash read another source", err)
	}
	raw[0] ^= 1
	if err := rehashFinalCapturedSourcesV2(t.Context(), load, []FinalCollectedValidatorSourceV2{source}, 64*1024*1024); err == nil || reads != 2 {
		t.Fatal("ordinary source mutation was waived", err)
	}
}
