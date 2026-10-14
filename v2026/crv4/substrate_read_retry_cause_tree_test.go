// Composed consumers may classify independent readers, but native physical
// errors keep a sealed subtree so an inner integrity defect cannot be discarded.
package crv4

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

// Direct origin and recursive presence have different jobs. A native wrapper
// around mixed close causes must be rejected as a unit before generic unwrapping.
func TestSubstrateReadHttpCauseSubtreeRemainsOpaque(t *testing.T) {
	pure := &substrateReadHttpTransportError{cause: io.ErrUnexpectedEOF}
	if !IsSubstrateReadTransportCause(pure) || !HasSubstrateReadTransportCause(pure) || !RetryableSubstrateReadTransportError(pure) {
		t.Fatal("physical native read origin was lost")
	}
	for _, cause := range []error{
		errors.Join(io.ErrUnexpectedEOF, errors.New("synthetic physical frame contradiction")),
		&os.PathError{Op: "close", Path: "synthetic-custody", Err: context.DeadlineExceeded},
		errors.Join(context.DeadlineExceeded, context.Canceled),
	} {
		physical := &substrateReadHttpCloseError{cause: cause}
		if !IsSubstrateReadTransportCause(physical) || !HasSubstrateReadTransportCause(physical) || RetryableSubstrateReadTransportError(physical) {
			t.Fatalf("native close subtree lost its complete hard verdict: %v", cause)
		}
		joined := errors.Join(pure, physical)
		if IsSubstrateReadTransportCause(joined) || !HasSubstrateReadTransportCause(joined) || RetryableSubstrateReadTransportError(joined) {
			t.Fatal("independent outer join discarded the opaque native hard cause")
		}
	}
}
