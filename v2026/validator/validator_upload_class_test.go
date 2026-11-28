//go:build linux || darwin

// A validator's rate class is minted only for an activation the reserved
// upload admission admits now, on the checks an upload consent passes. A
// de-permitted validator is refused as the validator's own refusal (403); an
// instance that is not current refuses as not current (503).
package validator

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

func validatorUploadClassTestHeader(t *testing.T, fixture *validatorUploadAuthorityTestFixture, credential string, now time.Time) (string, [32]byte) {
	t.Helper()
	privateKey := ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...))
	signer, err := newValidatorAttemptUploadSigner(fixture.authority.Expected, fixture.config().ReplicaNoID, fixture.config().MaximumIntentSeconds, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	header, err := signer.classHeaderAt(credential, now)
	if err != nil {
		t.Fatal(err)
	}
	sessionHash, err := protocol.ValidatorAttemptUploadSessionHash(credential)
	if err != nil {
		t.Fatal(err)
	}
	return header, sessionHash
}

// An admitted activation's class names its owner and the admission's expiry,
// and reserves nothing.
func TestValidatorUploadClassOfAnAdmittedValidator(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadAdmissionTestState(t, fixture)
	header, sessionHash := validatorUploadClassTestHeader(t, fixture, "current-api-client", fixture.now)
	class, err := owner.classAt(t.Context(), header, sessionHash, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := fixture.authority.Expected.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if class.Owner.Hotkey != fixture.authority.Expected.Hotkey || class.Owner.OperatorNoID != fixture.authority.Expected.NoID || class.Activation != digest || !class.ValidUntil.Equal(owner.validUntil) {
		t.Fatalf("class %+v differs from the admitted activation", class)
	}
	for range 3 {
		if _, err := owner.classAt(t.Context(), header, sessionHash, fixture.now); err != nil {
			t.Fatal(err)
		}
	}
	// no lease was taken: the cleanup's wait for leases would hang otherwise
}

// A validator that lost its permit has no class, and the refusal is the
// validator's own, not a not-current one. Before the class existed there was
// nothing to refuse; this pins that a de-permit ends the class.
func TestValidatorUploadClassEndsWithThePermit(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadAdmissionTestState(t, fixture)
	fixture.currentPermit = false
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	header, sessionHash := validatorUploadClassTestHeader(t, fixture, "current-api-client", fixture.now)
	_, err := owner.classAt(t.Context(), header, sessionHash, fixture.now)
	if err == nil || !strings.Contains(err.Error(), "permit") || errors.Is(err, ErrValidatorUploadAuthorityNotCurrent) {
		t.Fatalf("a de-permitted validator's class was not refused as its own: %v", err)
	}
}

// Past the admission's expiry the class is refused as not current.
func TestValidatorUploadClassPastExpiryIsNotCurrent(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadAdmissionTestState(t, fixture)
	now := owner.validUntil
	header, sessionHash := validatorUploadClassTestHeader(t, fixture, "current-api-client", now)
	_, err := owner.classAt(t.Context(), header, sessionHash, now)
	if err == nil || !errors.Is(err, ErrValidatorUploadAuthorityNotCurrent) {
		t.Fatalf("an expired admission's class was not refused as not current: %v", err)
	}
}

// A consent for another session, replica or time, or an upload consent, is
// refused before any admission state is read.
func TestValidatorUploadClassRefusesAnotherConsent(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadAdmissionTestState(t, fixture)
	header, sessionHash := validatorUploadClassTestHeader(t, fixture, "current-api-client", fixture.now)
	_, otherSession := validatorUploadClassTestHeader(t, fixture, "another-api-client", fixture.now)
	if _, err := owner.classAt(t.Context(), header, otherSession, fixture.now); err == nil {
		t.Fatal("a class consent for another session was accepted")
	}
	stale := fixture.now.Add(time.Duration(fixture.config().MaximumIntentSeconds+1) * time.Second)
	if _, err := owner.classAt(t.Context(), header, sessionHash, stale); err == nil {
		t.Fatal("an expired class consent was accepted")
	}
	uploadHeader, uploadSession, _ := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	if _, err := owner.classAt(t.Context(), uploadHeader, uploadSession, fixture.now); err == nil {
		t.Fatal("an upload consent was accepted as a class consent")
	}
	replicaFixture := newValidatorUploadAuthorityTestFixture(t)
	privateKey := ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...))
	signer, err := newValidatorAttemptUploadSigner(replicaFixture.authority.Expected, fixture.config().ReplicaNoID+1, fixture.config().MaximumIntentSeconds, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	otherReplica, err := signer.classHeaderAt("current-api-client", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.classAt(t.Context(), otherReplica, sessionHash, fixture.now); err == nil {
		t.Fatal("a class consent for another replica was accepted")
	}
}
