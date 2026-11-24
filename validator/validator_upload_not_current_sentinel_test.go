//go:build linux || darwin

// Two refusals say nothing about the validator, yet the admission as a whole
// is current, so readiness cannot report them: an admitted activation whose
// verification expired with no fresh one, and an activation an instance did
// not look for while its permit census was unavailable. Each carries
// ErrValidatorUploadAuthorityNotCurrent for the api to answer 503; a refusal
// of the validator itself does not.
package validator

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A refresh whose current eligibility read fails keeps the entry only to its
// own expiry, earlier than the admission's. Past it, the refusal is not
// current. Before, it carried no mark, and the api answered 403.
func TestValidatorUploadAdmissionExpiredRetainedEntryIsNotCurrent(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, _ := validatorUploadSilentDropTestState(t, fixture)
	start := fixture.now
	window := time.Duration(fixture.config().MaximumHeadAgeSeconds) * time.Second
	if err := owner.refresh(t.Context(), start); err != nil {
		t.Fatal(err)
	}
	fixture.fault = "current-schedule-unavailable"
	advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(30*time.Second))
	owner.clock = func() time.Time { return fixture.now }
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(window))
	close(owner.ready)
	if err := owner.WaitReady(t.Context()); err != nil {
		t.Fatalf("prerequisite: the admission as a whole is current: %v", err)
	}
	err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now)
	if err == nil || !strings.Contains(err.Error(), "current native eligibility read") {
		t.Fatalf("prerequisite: the expired retained entry is refused with its read's cause: %v", err)
	}
	if !errors.Is(err, ErrValidatorUploadAuthorityNotCurrent) {
		t.Fatalf("an expired retained entry's refusal is not marked not current: %v", err)
	}
}

// While the permit census is unavailable, discovery follows only the
// admitted hotkeys, so a validator not admitted before is not looked for. Its
// refusal is not current. With the census read, a hotkey without a permit is
// refused as the validator's own, unmarked.
func TestValidatorUploadAdmissionUndiscoveredWithoutCensusIsNotCurrent(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		fault      string
		permit     bool
		notCurrent bool
	}{
		{name: "census unavailable", fault: "census-batch-unavailable", permit: true, notCurrent: true},
		{name: "no permit", permit: false, notCurrent: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newValidatorUploadAuthorityTestFixture(t)
			fixture.fault, fixture.currentPermit = test.fault, test.permit
			owner, reports := validatorUploadSilentDropTestState(t, fixture)
			if err := owner.refresh(t.Context(), fixture.now); err != nil {
				t.Fatal(err)
			}
			if len(*reports) != 1 || (*reports)[0].admitted != 0 {
				t.Fatalf("prerequisite: nothing admitted: %+v", *reports)
			}
			close(owner.ready)
			owner.clock = func() time.Time { return fixture.now }
			if err := owner.WaitReady(t.Context()); err != nil {
				t.Fatalf("prerequisite: the admission as a whole is current: %v", err)
			}
			err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now)
			if err == nil || !strings.Contains(err.Error(), "is not admitted") {
				t.Fatalf("prerequisite: the activation is not admitted: %v", err)
			}
			if errors.Is(err, ErrValidatorUploadAuthorityNotCurrent) != test.notCurrent {
				t.Fatalf("refusal marked not current %t, want %t: %v", errors.Is(err, ErrValidatorUploadAuthorityNotCurrent), test.notCurrent, err)
			}
		})
	}
}

// A refusal that is not about currency stays unmarked: a closed admission.
func TestValidatorUploadAdmissionRefusalOfTheValidatorIsNotMarked(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadAdmissionTestState(t, fixture)
	header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now)
	if err != nil {
		t.Fatalf("prerequisite: the admitted activation begins: %v", err)
	}
	lease.Close()
	owner.closed = true
	_, err = owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now)
	if err == nil || errors.Is(err, ErrValidatorUploadAuthorityNotCurrent) {
		t.Fatalf("a closed admission's refusal was marked not current: %v", err)
	}
}
