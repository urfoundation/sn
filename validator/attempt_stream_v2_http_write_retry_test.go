// Actual acknowledgement reads keep their transport provenance without
// turning bytes, protocol refusals or independent close errors into retries.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"testing"
)

// No bytes arrived, so a timeout or interrupted response is an unresolved
// transport operation. The writer still issues exactly one bounded request.
func TestAttemptStreamV2HttpWriteAcknowledgementReadInterruption(t *testing.T) {
	t.Parallel()
	data := []byte("synthetic immutable upload")
	hash := attemptHex32(sha256.Sum256(data))
	for _, status := range []int{http.StatusNoContent, http.StatusInternalServerError, http.StatusBadGateway, http.StatusConflict} {
		for _, cause := range []error{context.DeadlineExceeded, io.ErrUnexpectedEOF, context.Canceled} {
			calls := 0
			body := &attemptStreamV2HTTPTestBody{read: func([]byte) (int, error) { return 0, cause }}
			writer := newAttemptStreamV2HTTPTestWriter(t, "https://publication.example", func() string { return "synthetic-session" })
			writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Etag": {`"` + hash + `"`}}, Body: body}, nil
			})
			err := writer.Write(t.Context(), "metadata", hash, data)
			wantRetry := status != http.StatusConflict && cause != context.Canceled
			if !errors.Is(err, cause) || RetryableEvidenceTransportError(err) != wantRetry || calls != 1 || body.closes != 1 {
				t.Fatalf("response interruption changed retry or request ownership: status=%d cause=%v calls=%d closes=%d error=%v", status, cause, calls, body.closes, err)
			}
		}
	}
}

// A malformed acknowledgement remains hard even when the same read times out,
// and a valid-looking empty response cannot conceal an independent close error.
func TestAttemptStreamV2HttpWriteAcknowledgementRetainsIntegrityAndCloseErrors(t *testing.T) {
	t.Parallel()
	data := []byte("synthetic immutable upload")
	hash := attemptHex32(sha256.Sum256(data))
	broken := errors.New("synthetic acknowledgement close failed")
	for _, nonempty := range []bool{false, true} {
		for _, closeFailure := range []error{nil, broken} {
			body := &attemptStreamV2HTTPTestBody{
				read: func(buffer []byte) (int, error) {
					if nonempty {
						return copy(buffer, "x"), context.DeadlineExceeded
					}
					return 0, context.DeadlineExceeded
				},
				close: func() error { return closeFailure },
			}
			writer := newAttemptStreamV2HTTPTestWriter(t, "https://publication.example", func() string { return "synthetic-session" })
			writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{"Etag": {`"` + hash + `"`}}, Body: body}, nil
			})
			err := writer.Write(t.Context(), "metadata", hash, data)
			if !errors.Is(err, context.DeadlineExceeded) || closeFailure != nil && !errors.Is(err, closeFailure) || RetryableEvidenceTransportError(err) != (!nonempty && closeFailure == nil) || body.closes != 1 {
				t.Fatalf("response read hid integrity or close failure: nonempty=%t close=%v error=%v", nonempty, closeFailure, err)
			}
		}
	}
}
