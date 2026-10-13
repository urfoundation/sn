//go:build linux || darwin

// Real trail transport barriers and original physical checkpoint replay prove
// the window fence independently of its producer's returned projection.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// A real active send prevents closure; after it joins, a delayed new send in
// that epoch is fenced before reaching its original transport.
func TestProviderRequestClosedWindowDrainsActualSendAndFencesLateTrail(t *testing.T) {
	engine, journal, preparation, _ := providerRequestTestOwner(t, 32)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	finished := make(chan struct{})
	first := true
	engine.transport = providerRequestTestTransport{base: engine.transport, before: func(owner context.Context, _ connect.Id, _ []byte) error {
		if first {
			first = false
			close(entered)
			select {
			case <-release:
			case <-owner.Done():
				return owner.Err()
			}
		}
		return nil
	}}
	go func() { defer close(finished); _, err := engine.RunTrail(ctx); done <- err }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-release:
		default:
			close(release)
		}
		select {
		case <-finished:
		case <-time.After(time.Minute):
			t.Error("actual request fixture failed to join cleanup")
		}
	})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual send did not enter", ctx.Err())
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: preparation.Birth.SettlementEpoch, StartBlock: preparation.Birth.EVMBlock, EndBlock: preparation.Birth.EVMBlock + 1, FinalizedBlock: preparation.Birth.EVMBlock + 1}
	if cut, err := journal.SealWindow(ctx, window, 1024*1024); !errors.Is(err, protocol.ErrProviderAttemptsUnavailable) || cut != nil {
		t.Fatalf("active send was closed: %v", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("actual send did not join", ctx.Err())
	}
	cut, err := journal.SealWindow(ctx, window, 1024*1024)
	if err != nil || cut == nil || len(cut.Records) != 8 {
		t.Fatal("actual closed full request census", err)
	}
	want, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	again, err := journal.SealWindow(ctx, window, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(again)
	if !bytes.Equal(want, got) {
		t.Fatal("lost close output changed original signed bytes")
	}
	sent := false
	engine.transport = providerRequestTestTransport{base: engine.transport, before: func(context.Context, connect.Id, []byte) error { sent = true; return nil }}
	if proof, err := engine.RunTrail(ctx); err == nil || proof != nil || sent {
		t.Fatalf("closed epoch sent another original: sent=%t error=%v", sent, err)
	}
}

// Closing metadata must name the original prefix on cold reopening, and a
// caller cannot mutate the stored signature through a returned checkpoint.
func TestProviderRequestClosedCheckpointColdPrefixAndDetachedRestore(t *testing.T) {
	engine, journal, preparation, path := providerRequestTestOwner(t, 32)
	if _, err := engine.RunTrail(t.Context()); err != nil {
		t.Fatal(err)
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: preparation.Birth.SettlementEpoch, StartBlock: preparation.Birth.EVMBlock, EndBlock: preparation.Birth.EVMBlock + 1, FinalizedBlock: preparation.Birth.EVMBlock + 1}
	cut, err := journal.SealWindow(t.Context(), window, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := *journal.checkpoint
	raw, err := os.ReadFile(filepath.Join(path, ProviderAttemptRequestJournalName))
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyProviderAttemptRequestPrefix(t.Context(), checkpoint, preparation, bytes.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	copy := verified.Checkpoint()
	copy.Closed.Signature[0] ^= 1
	if bytes.Equal(copy.Closed.Signature, verified.Checkpoint().Closed.Signature) {
		t.Fatal("restored checkpoint leaked mutable signature")
	}
	changed := copyClosedProviderRequestTestHead(cut.Header)
	checkpoint.Closed = &changed
	checkpoint.Closed.End.Hash[0] ^= 1
	if admitted, err := VerifyProviderAttemptRequestPrefix(t.Context(), checkpoint, preparation, bytes.NewReader(raw), nil); err == nil || admitted != nil {
		t.Fatal("foreign closed frontier survived original-prefix proof")
	}
}

// Test-only copying avoids changing the actual journal's retained header.
func copyClosedProviderRequestTestHead(value ProviderAttemptRequestClosedHead) ProviderAttemptRequestClosedHead {
	value.Signature = bytes.Clone(value.Signature)
	return value
}

// Future complete-frame sizing uses the actual escaped original identity,
// including both retained preparation copies, before any birth is published.
func TestProviderRequestPreparationForecastsCompleteClosedPendingFrame(t *testing.T) {
	_, _, preparation, _ := providerRequestTestOwner(t, 32)
	if err := preparation.Validate(); err != nil {
		t.Fatal(err)
	}
	preparation.Identity.Ledger.DeploymentID = strings.Repeat("escaped\\\"identity", 150)
	if err := preparation.Validate(); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatal("unpublishable future custody frame admitted", err)
	}
}
