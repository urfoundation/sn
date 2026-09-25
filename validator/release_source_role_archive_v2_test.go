//go:build linux || darwin

// Archive reads preserve exact pins and signatures without reopening the old
// source directory. A decoded descriptor alone never grants native finality.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func TestReleaseSourceRoleArchiveV2UsesOnlyPinnedDetachedBytes(t *testing.T) {
	f := newSourceRolePredecessorTestFixtureV2(t)
	cfg := &f.historical.config
	proof, err := DecodeReleaseSourceRolePredecessorV2(t.Context(), cfg, f.encoded)
	if err != nil {
		t.Fatal(err)
	}
	files := map[ReleaseEvidenceV2File][]byte{}
	for _, ref := range []ReleaseEvidenceV2File{proof.Measurement, proof.Envelope} {
		raw, err := os.ReadFile(ref.Path)
		if err != nil {
			t.Fatal(err)
		}
		files[ref] = raw
	}
	retired := f.previous + "-retired"
	if err := os.Rename(f.previous, retired); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(retired, f.previous); err != nil {
			t.Error(err)
		}
	})
	reads := 0
	read := func(ctx context.Context, ref ReleaseEvidenceV2File, limit uint64) ([]byte, error) {
		reads++
		raw, ok := files[ref]
		if !ok {
			return nil, errors.New("foreign source")
		}
		if uint64(len(raw)) > limit {
			return nil, errors.New("oversize source")
		}
		return bytes.Clone(raw), ctx.Err()
	}
	got, err := DecodeReleaseSourceRolePredecessorArchiveV2(t.Context(), cfg, f.encoded, read)
	if err != nil || got == nil || got.Intent.ExtrinsicHash != proof.Intent.ExtrinsicHash || reads != 2 {
		t.Fatalf("detached source role failed: reads=%d %v", reads, err)
	}
	if _, err := DecodeReleaseSourceRolePredecessorV2(t.Context(), cfg, f.encoded); err == nil {
		t.Fatal("fixture did not remove live source")
	}
	if f.receipt.submissions != 0 || f.receipt.subscriptions != 0 || f.receipt.eventReads != 0 {
		t.Fatal("archive decoding used live native authority")
	}
	for _, ref := range []ReleaseEvidenceV2File{proof.Measurement, proof.Envelope} {
		original := files[ref]
		files[ref] = append(bytes.Clone(original), '\n')
		if _, err := DecodeReleaseSourceRolePredecessorArchiveV2(t.Context(), cfg, f.encoded, read); err == nil {
			t.Fatal("changed archive payload passed exact pin")
		}
		files[ref] = original
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	before := reads
	if _, err := DecodeReleaseSourceRolePredecessorArchiveV2(canceled, cfg, f.encoded, read); !errors.Is(err, context.Canceled) || reads != before {
		t.Fatal("canceled archive replay read a source", err)
	}
}
