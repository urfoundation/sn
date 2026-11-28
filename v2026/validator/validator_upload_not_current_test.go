//go:build linux || darwin

// Past the expiry of its last publish, an admission is not current. A
// transient refresh failure that the admission retained, and a refresh still
// in flight, both leave no error behind, so readiness must report the expiry
// itself. The api answers 503 only when readiness fails; otherwise a refused
// request is a 403, which counts against the validator as an authorization
// refusal and is not retried on a sibling.
package validator

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Before, readiness stayed nil from the expiry until the next refresh failed,
// and each upload in that window was refused 403 "admission expired".
func TestValidatorUploadAdmissionPastExpiryIsNotCurrent(t *testing.T) {
	t.Parallel()
	timeout := &url.Error{Op: "Post", URL: "http://rpc.example", Err: context.DeadlineExceeded}
	for _, retained := range []bool{true, false} {
		t.Run(fmt.Sprintf("retained=%t", retained), func(t *testing.T) {
			t.Parallel()
			fixture := newValidatorUploadAuthorityTestFixture(t)
			owner := validatorUploadAdmissionTestState(t, fixture)
			// The production loop closes ready after its first attempt.
			close(owner.ready)
			validUntil := owner.validUntil
			now := fixture.now
			owner.clock = func() time.Time { return now }
			if retained && !owner.retainAfterTransientRefreshFailure(timeout) {
				t.Fatal("prerequisite: a transient failure retains the admission")
			}
			if err := owner.WaitReady(t.Context()); err != nil {
				t.Fatalf("a current admission reported not ready: %v", err)
			}
			if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, now); err != nil {
				t.Fatalf("a current admission refused its validator: %v", err)
			}
			// At its expiry, with no fresh publish, it is not current.
			now = validUntil
			if err := owner.WaitReady(t.Context()); err == nil || !strings.Contains(err.Error(), "no refresh has published since") {
				t.Fatalf("an expired admission reported ready: %v", err)
			}
			if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, now); err == nil {
				t.Fatal("an expired admission admitted a request")
			}
			// A fresh publish makes it current again.
			advanceValidatorUploadSilentDropTestHeads(fixture, validUntil.Add(time.Second))
			now = fixture.now
			if err := owner.refresh(t.Context(), now); err != nil {
				t.Fatal(err)
			}
			if err := owner.WaitReady(t.Context()); err != nil {
				t.Fatalf("a fresh publish did not restore readiness: %v", err)
			}
			if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, now); err != nil {
				t.Fatalf("a fresh publish did not admit the validator: %v", err)
			}
		})
	}
}
