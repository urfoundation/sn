//go:build linux || darwin

// Capture admission uses real private activation files and signed identities.
// Missing later sources never cause a historical query or legacy fallback.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Test-only limits use the already explicit fixture policy.
func releaseCaptureV2TestOptions(fixture *releaseBootstrapV2TestFixture) ReleaseEvidenceV2CaptureOptions {
	return ReleaseEvidenceV2CaptureOptions{Hotkey: fixture.contexts[0].Activation.Hotkey, Origins: [2]string{fixture.cfg.Operators[0].APIURL, fixture.cfg.Operators[1].APIURL}, MaximumBytes: fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes, MaximumObjects: 4096, ThroughEpoch: 42}
}

// Both complete signed activation sources are copied before a missing
// original intent store can fail capture, and no native/Evm history is queried.
func TestReleaseCaptureV2RetainsOriginalSetupBeforeHistoricalReads(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	before := fixture.calls()
	var sources []ReleaseEvidenceV2CaptureSource
	result, err := CaptureReleaseEvidenceV2(t.Context(), &fixture.cfg, fixture.chain, fixture.native, releaseCaptureV2TestOptions(fixture), func(_ context.Context, source ReleaseEvidenceV2CaptureSource, raw []byte) error {
		if len(raw) == 0 {
			t.Fatal("empty original setup source")
		}
		sources = append(sources, source)
		return nil
	})
	if err == nil || result != nil || len(sources) != 10 || fixture.calls() != before {
		t.Fatalf("capture queried history or lost original setup sources: sources=%d calls=%d/%d result=%v err=%v", len(sources), fixture.calls(), before, result, err)
	}
	for _, source := range sources {
		if source.Kind != "setup" || source.Origin != "" {
			t.Fatalf("unexpected pre-history source: %+v", source)
		}
	}
}

// The exact v6 file is retained even when its empty intent census cannot be
// promoted to a complete capture. No schema rewrite or fabricated intent occurs.
func TestReleaseCaptureV2PreservesExactV6StoreBeforeRejectingEmptyCensus(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	original := []byte("{\n  \"schema\": \"" + SteeringIntentSchema + "\",\n  \"history\": []\n}\n")
	if err := os.MkdirAll(fixture.cfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.cfg.StateDir, "steering-intents.json"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	before := fixture.calls()
	var retained []byte
	result, err := CaptureReleaseEvidenceV2(t.Context(), &fixture.cfg, fixture.chain, fixture.native, releaseCaptureV2TestOptions(fixture), func(_ context.Context, source ReleaseEvidenceV2CaptureSource, raw []byte) error {
		if source.Kind == "private" && source.Name == "steering-intents.json" {
			retained = bytes.Clone(raw)
		}
		return nil
	})
	if err == nil || result != nil || !bytes.Equal(original, retained) || fixture.calls() != before {
		t.Fatalf("v6 original custody changed: retained=%q calls=%d/%d err=%v", retained, fixture.calls(), before, err)
	}
}

// The archive callback cannot turn a failed durable write into successful
// acquisition or permit a subsequent real chain request.
func TestReleaseCaptureV2ArchiveRefusalStopsBeforeHistoricalReads(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	want := errors.New("closed archive write refused")
	before := fixture.calls()
	writes := 0
	result, err := CaptureReleaseEvidenceV2(t.Context(), &fixture.cfg, fixture.chain, fixture.native, releaseCaptureV2TestOptions(fixture), func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error { writes++; return want })
	if !errors.Is(err, want) || result != nil || writes != 1 || fixture.calls() != before {
		t.Fatalf("archive refusal lost causal ownership: writes=%d calls=%d/%d err=%v", writes, fixture.calls(), before, err)
	}
}

// A completed source is never returned after cancellation, including before
// setup file acquisition; original producer state is not opened exclusively.
func TestReleaseCaptureV2CancellationDoesNotReadOrWrite(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := fixture.calls()
	writes := 0
	result, err := CaptureReleaseEvidenceV2(ctx, &fixture.cfg, fixture.chain, fixture.native, releaseCaptureV2TestOptions(fixture), func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error { writes++; return nil })
	if !errors.Is(err, context.Canceled) || result != nil || writes != 0 || fixture.calls() != before {
		t.Fatalf("canceled capture escaped: writes=%d calls=%d/%d err=%v", writes, fixture.calls(), before, err)
	}
}

// Explicit finite object admission precedes the second archive side effect.
func TestReleaseCaptureV2RejectsArchiveCensusOverflow(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	options := releaseCaptureV2TestOptions(fixture)
	options.MaximumObjects = 1
	before := fixture.calls()
	writes := 0
	result, err := CaptureReleaseEvidenceV2(t.Context(), &fixture.cfg, fixture.chain, fixture.native, options, func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error { writes++; return nil })
	if err == nil || result != nil || writes != 1 || fixture.calls() != before {
		t.Fatalf("archive census cap was not enforced: writes=%d calls=%d/%d err=%v", writes, fixture.calls(), before, err)
	}
}

// Missing full native authority cannot be replaced by a nonnil callback.
func TestReleaseCaptureV2RejectsNativeSourceOwnerBeforeRetention(t *testing.T) {
	if err := CaptureReleaseNativeSourceV2(t.Context(), &crv4.Chain{}, &ReleaseConfig{}, &SteeringIntent{}, []byte("{}"), func(context.Context, ReleaseEvidenceV2NativeRead) error {
		t.Fatal("missing source owner retained a result")
		return nil
	}); err == nil {
		t.Fatal("missing original prepared source was accepted")
	}
}
