// Upload expiry must remain a retryable owned operation through error capture.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The real client expires after the server accepts the request but withholds
// headers. No synthetic timeout error can bypass the actual transport wrapper.
func TestAttemptStreamV2HttpWriteTimeoutSurvivesCapture(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
	}))
	defer endpoint.Close()
	defer close(release)
	writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return "synthetic-session" })
	data := []byte("synthetic-upload\n")
	err := writer.Write(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	<-entered
	if err == nil || calls.Load() != 1 {
		t.Fatalf("upload did not expire exactly once: calls=%d error=%v", calls.Load(), err)
	}
	if !retryableProductionSteeringRead(err) {
		t.Errorf("raw owned upload timeout became hard: %v", err)
	}
	observed := observeProductionSteeringRead(err, false)
	if !retryableProductionSteeringRead(observed) {
		t.Errorf("captured owned upload timeout became hard: %v", observed)
	}
	if !errors.Is(observed, context.DeadlineExceeded) {
		t.Error("captured timeout lost owned expiry")
	}
	hard := errors.New("synthetic independent hard refusal")
	if retryableProductionSteeringRead(observeProductionSteeringRead(errors.Join(err, hard), false)) {
		t.Error("timeout hid independent hard refusal")
	}
	causes := make([]error, 6)
	for i := range causes {
		branch := make([]error, 100)
		for j := range branch {
			branch[j] = err
		}
		causes[i] = errors.Join(branch...)
	}
	collapsed := observeProductionSteeringRead(errors.Join(causes...), false)
	if !retryableProductionSteeringRead(collapsed) {
		t.Errorf("owned deadline census became hard: %v", collapsed)
	}
}
