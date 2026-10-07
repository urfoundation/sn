// The real HTTP reader preserves a cancellation that arrives at original-byte reconstruction.
package validator

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/payoutartifact"
)

// Before arming, real HTTP transport uses the unchanged actual operation owner.
// After the complete original body arrives, the second owner check is Decode ingress.
type artifactDecodeCancelContext struct {
	context.Context
	cancel context.CancelFunc
	armed  atomic.Bool
	checks atomic.Int32
}

// A genuine owner cancellation is triggered by work boundaries, never elapsed time.
func (self *artifactDecodeCancelContext) Err() error {
	if self.armed.Load() && self.checks.Add(1) == 2 {
		self.cancel()
	}
	return self.Context.Err()
}

// The original history/body come from the actual HTTP endpoint. Only the existing
// raw-byte observation handoff arms cancellation immediately before reconstruction.
func TestArtifactClosedWorkActualHttpDecodeCancellationKeepsTypedCause(t *testing.T) {
	reader, original := validatorClosedWorkReader(t, nil)
	transportReader := *reader
	operation, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &artifactDecodeCancelContext{Context: operation, cancel: cancel}
	var completeBodies atomic.Int32
	reader.observedGet = func(owner context.Context, endpoint string, maximum int64) ([]byte, error) {
		raw, err := transportReader.get(owner, endpoint, maximum)
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil {
			return nil, parseErr
		}
		if err == nil && parsed.Path == "/sn/artifact" {
			completeBodies.Add(1)
			ctx.armed.Store(true)
		}
		return raw, err
	}
	artifact, err := reader.Read(ctx, 4, 1)
	if artifact != nil || !errors.Is(err, context.Canceled) || errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || !strings.HasPrefix(err.Error(), "artifact integrity:") || completeBodies.Load() != 1 || operation.Err() != context.Canceled {
		t.Fatal("actual HTTP decode cancellation lost its operational cause", artifact, err, completeBodies.Load())
	}
	// Reopen the same exact HTTP source under a new owner; no hold or source rewrite exists.
	reader.observedGet = nil
	artifact, err = reader.Read(t.Context(), 4, 1)
	if err != nil || artifact == nil || artifact.ContentHash != original.ContentHash {
		t.Fatal("healthy original HTTP reconstruction did not recover", err)
	}
}
