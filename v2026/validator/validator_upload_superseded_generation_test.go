//go:build linux || darwin

// A discovered successor revokes the prior upload key even when its own
// historical proof is unavailable. Publication order includes the log index.
package validator

import (
	"crypto/ed25519"
	"testing"
	"time"
)

// Exercise both freshly reauthenticated and retained prior entries, with the
// successor in the same block and in a later block. No clock expiry revokes it.
func TestValidatorUploadAdmissionSuccessorReadFailureCannotKeepPriorGeneration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		block        uint64
		currentFault string
	}{
		{block: 1002},
		{block: 1001},
		{block: 1002, currentFault: "current-schedule-unavailable"},
		{block: 1001, currentFault: "current-schedule-unavailable"},
	} {
		fixture := newValidatorUploadAuthorityTestFixture(t)
		owner, reports := validatorUploadSilentDropTestState(t, fixture)
		start := fixture.now
		if err := owner.refresh(t.Context(), start); err != nil {
			t.Fatal(err)
		}
		header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
		prior, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, start)
		if err != nil {
			t.Fatal(err)
		}
		key := ed25519.NewKeyFromSeed(append([]byte{0x42}, make([]byte, 31)...))
		successor := fixture.authority.Expected
		copy(successor.VPK[:], key[ed25519.SeedSize:])
		fixture.publish(t, successor, test.block, key)
		fixture.releaseActivationV2TestFixture.fault = "historical-state-pruned"
		fixture.fault = test.currentFault
		advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(30*time.Second))
		owner.clock = func() time.Time { return fixture.now }
		if err := owner.refresh(t.Context(), fixture.now); err != nil {
			prior.Close()
			t.Fatal(err)
		}
		retired := prior.entry.ctx.Err() != nil
		prior.Close()
		if !retired {
			t.Errorf("block=%d current fault=%q: successor with unavailable history kept the prior generation", test.block, test.currentFault)
		}
		header, sessionHash, digest = validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
		if lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now); lease != nil || err == nil {
			if lease != nil {
				lease.Close()
			}
			t.Errorf("block=%d current fault=%q: prior generation still admits new requests: %v", test.block, test.currentFault, err)
		}
		if len(*reports) != 2 || (*reports)[1].admitted != 0 || (*reports)[1].retained != 0 {
			t.Errorf("block=%d current fault=%q: superseded generation was published: %+v", test.block, test.currentFault, *reports)
		}
		fixture.releaseActivationV2TestFixture.fault, fixture.fault = "", ""
		if err := owner.refresh(t.Context(), fixture.now); err != nil {
			t.Fatal(err)
		}
		header, sessionHash, digest = validatorUploadAdmissionTestHeader(t, fixture, successor, 0x42, "current-api-client", []byte("object"))
		lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now)
		if err != nil {
			t.Fatalf("block=%d: fully verified successor was not admitted after recovery: %v", test.block, err)
		}
		lease.Close()
	}
}
