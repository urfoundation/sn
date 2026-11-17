//go:build linux || darwin

// Regressions for refusals that looked like a successful refresh: a state
// published after its own window, and an activation dropped by a failed or
// pruned read. The real refresh, readers and fixtures run; only the publication
// clock and exact RPC faults are controlled.
package validator

import (
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// No refresh has run. Reports are collected only by the joined test caller.
func validatorUploadSilentDropTestState(t *testing.T, fixture *validatorUploadAuthorityTestFixture) (*ValidatorUploadAdmission, *[]validatorUploadRefreshReport) {
	t.Helper()
	owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
	reports := &[]validatorUploadRefreshReport{}
	owner.refreshReport = func(report validatorUploadRefreshReport) { *reports = append(*reports, report) }
	return owner, reports
}

// Both chain clocks advance together, as finalized heads do between refreshes.
func advanceValidatorUploadSilentDropTestHeads(fixture *validatorUploadAuthorityTestFixture, at time.Time) {
	fixture.now = at
	fixture.currentMillis = uint64(at.UnixMilli())
}

// The observers are captured at the start. A refresh that finishes a full
// window later must fail with its measured cause, not publish an expired state
// whose generic refusal hides why every upload is denied.
func TestValidatorUploadAdmissionRefusesBornExpiredPublication(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, reports := validatorUploadSilentDropTestState(t, fixture)
	window := time.Duration(fixture.config().MaximumHeadAgeSeconds) * time.Second
	owner.clock = func() time.Time {
		if fixture.logCalls.Load() == 0 {
			return fixture.now
		}
		return fixture.now.Add(window)
	}
	err := owner.refresh(t.Context(), fixture.now)
	if err == nil || !strings.Contains(err.Error(), "finished after its freshness window") {
		t.Fatalf("refresh published a state that expired before publication: %v", err)
	}
	owner.invalidate(err)
	header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	if lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now); lease != nil || err == nil || !strings.Contains(err.Error(), "finished after its freshness window") {
		if lease != nil {
			lease.Close()
		}
		t.Fatalf("expired publication refused without its cause: %v", err)
	}
	// One second inside the same window publishes and admits.
	owner.clock = func() time.Time { return fixture.now.Add(window - time.Second) }
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now)
	if err != nil {
		t.Fatalf("publication inside its window was refused: %v", err)
	}
	lease.Close()
	if len(*reports) != 2 || (*reports)[0].err == nil || (*reports)[1].err != nil || (*reports)[1].admitted != 1 || (*reports)[1].events != 1 || (*reports)[1].ranges != 1 {
		t.Fatalf("refresh outcomes were not reported: %+v", *reports)
	}
}

// A state-pruned provider fails the activation's historical read. The refresh
// still completes, so the refusal and the report must name the dropped cause.
func TestValidatorUploadAdmissionSurfacesDroppedActivationCause(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, reports := validatorUploadSilentDropTestState(t, fixture)
	fixture.releaseActivationV2TestFixture.fault = "historical-state-pruned"
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now)
	if lease != nil || err == nil || !strings.Contains(err.Error(), "historical activation authentication") || !strings.Contains(err.Error(), "failed to retrieve Runtime Api version") {
		if lease != nil {
			lease.Close()
		}
		t.Fatalf("dropped activation was refused without its cause: %v", err)
	}
	if len(*reports) != 1 || (*reports)[0].err != nil || (*reports)[0].admitted != 0 || len((*reports)[0].activationFailures) != 1 {
		t.Fatalf("dropped activation was absent from the refresh report: %+v", *reports)
	}
}

// After one complete verification, a later refresh whose current eligibility
// read fails keeps the entry only until that verification's own expiry, then
// refuses with the read's cause.
func TestValidatorUploadAdmissionRetainsVerifiedEntryThroughFailedRead(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, reports := validatorUploadSilentDropTestState(t, fixture)
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
	header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now)
	if err != nil {
		t.Fatalf("failed current read dropped a verified activation: %v", err)
	}
	if deadline, ok := lease.Context().Deadline(); !ok || deadline.After(start.Add(window)) {
		t.Fatalf("retained lease outlived its last complete verification: %v", deadline)
	}
	lease.Close()
	advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(window))
	header, sessionHash, digest = validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	if lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now); lease != nil || err == nil || !strings.Contains(err.Error(), "current native eligibility read") {
		if lease != nil {
			lease.Close()
		}
		t.Fatalf("expired retained entry was refused without its cause: %v", err)
	}
	if len(*reports) != 2 || (*reports)[1].retained != 1 || (*reports)[1].admitted != 0 {
		t.Fatalf("retained entry was absent from the refresh report: %+v", *reports)
	}
}

// A finalized anchor's immutable historical proof is reused, so a provider
// that has pruned that state cannot drop an already verified activation, and
// its expiry still advances with each current eligibility read.
func TestValidatorUploadAdmissionReusesVerifiedHistoryWithoutArchiveState(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, reports := validatorUploadSilentDropTestState(t, fixture)
	start := fixture.now
	window := time.Duration(fixture.config().MaximumHeadAgeSeconds) * time.Second
	if err := owner.refresh(t.Context(), start); err != nil {
		t.Fatal(err)
	}
	fixture.releaseActivationV2TestFixture.fault = "historical-state-pruned"
	advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(30*time.Second))
	owner.clock = func() time.Time { return fixture.now }
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	digest, err := fixture.authority.Expected.Digest()
	if err != nil {
		t.Fatal(err)
	}
	entry := owner.entries[digest]
	if entry == nil || !entry.validUntil.Equal(start.Add(30*time.Second+window)) || len(*reports) != 2 || (*reports)[1].admitted != 1 || (*reports)[1].retained != 0 {
		t.Fatalf("pruned historical state dropped or stalled a verified activation: %+v / %+v", entry, *reports)
	}
}

// A provider that returns no rows for old ranges cannot silently drop an
// activation this owner already authenticated; it is checked again directly.
func TestValidatorUploadAdmissionRechecksAdmittedActivationOmittedByDiscovery(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, reports := validatorUploadSilentDropTestState(t, fixture)
	start := fixture.now
	window := time.Duration(fixture.config().MaximumHeadAgeSeconds) * time.Second
	if err := owner.refresh(t.Context(), start); err != nil {
		t.Fatal(err)
	}
	fixture.fault = "empty-logs"
	advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(30*time.Second))
	owner.clock = func() time.Time { return fixture.now }
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	digest, err := fixture.authority.Expected.Digest()
	if err != nil {
		t.Fatal(err)
	}
	entry := owner.entries[digest]
	if entry == nil || !entry.validUntil.Equal(start.Add(30*time.Second+window)) || len(*reports) != 2 || (*reports)[1].events != 0 || (*reports)[1].admitted != 1 {
		t.Fatalf("empty discovery dropped an admitted activation: %+v / %+v", entry, *reports)
	}
}

// A later publication of the same owner supersedes an entry whose current
// eligibility read fails: a successor that is authenticated, only discovered,
// or an authenticated seed without discovery. The superseded generation is
// retired, not retained.
func TestValidatorUploadAdmissionRetiresSupersededEntryThroughFailedRead(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		seeded         bool
		successorFault string
	}{
		{},
		{successorFault: "historical-state-pruned"},
		{seeded: true},
	} {
		fixture := newValidatorUploadAuthorityTestFixture(t)
		key := ed25519.NewKeyFromSeed(append([]byte{0x42}, make([]byte, 31)...))
		successor := fixture.authority.Expected
		copy(successor.VPK[:], key[32:])
		config := fixture.config()
		if test.seeded {
			config = validatorUploadSeededTestConfig(t, fixture)
			encoded, err := ReadReleaseEvidenceV2File(t.Context(), config.ActivationContexts[0], config.MaximumContextBytes)
			if err != nil {
				t.Fatal(err)
			}
			successorContext, err := decodeReleaseEvidenceV2ActivationContext(encoded, config.MaximumContextBytes)
			if err != nil {
				t.Fatal(err)
			}
			successorContext.Activation, successorContext.InitialCut.Identity.ValidatorVPK = successor, attemptHex32(successor.VPK)
			if successorContext.InitialCut.Activation.Domain, err = successor.EvidenceDomain(); err != nil {
				t.Fatal(err)
			}
			if encoded, err = successorContext.CanonicalJSON(config.MaximumContextBytes); err != nil {
				t.Fatal(err)
			}
			config.ActivationContexts = append(config.ActivationContexts, writeReleaseBootstrapV2TestFile(t, filepath.Join(t.TempDir(), "owner", "successor.json"), encoded))
		}
		owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
		reports := []validatorUploadRefreshReport{}
		owner.refreshReport = func(report validatorUploadRefreshReport) { reports = append(reports, report) }
		start := fixture.now
		if err := owner.refresh(t.Context(), start); err != nil {
			t.Fatal(err)
		}
		header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
		prior, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, start)
		if err != nil {
			t.Fatal(err)
		}
		defer prior.Close()
		fixture.publish(t, successor, 1002, key)
		// Only the successor's own historical read fails in the second case.
		fixture.releaseActivationV2TestFixture.fault = test.successorFault
		fixture.fault = "current-schedule-unavailable"
		advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(30*time.Second))
		owner.clock = func() time.Time { return fixture.now }
		if err := owner.refresh(t.Context(), fixture.now); err != nil {
			t.Fatal(err)
		}
		if prior.entry.ctx.Err() == nil {
			t.Fatalf("%+v successor left the superseded generation admitted", test)
		}
		header, sessionHash, digest = validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
		if lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now); lease != nil || err == nil || !strings.Contains(err.Error(), "current native eligibility read") {
			if lease != nil {
				lease.Close()
			}
			t.Fatalf("%+v superseded entry was retained or refused without its cause: %v", test, err)
		}
		if len(reports) != 2 || reports[1].err != nil || reports[1].retained != 0 || reports[1].admitted != 0 {
			t.Fatalf("%+v superseded entry was reported as retained: %+v", test, reports)
		}
	}
}

// A deregistered hotkey has no current registration to read. That observes
// ineligibility, so the entry is retired at once instead of being retained.
func TestValidatorUploadAdmissionRetiresUnregisteredHotkeyAtOnce(t *testing.T) {
	t.Parallel()
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
	defer prior.Close()
	fixture.fault = "current-hotkey-unregistered"
	advanceValidatorUploadSilentDropTestHeads(fixture, start.Add(30*time.Second))
	owner.clock = func() time.Time { return fixture.now }
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if prior.entry.ctx.Err() == nil {
		t.Fatal("deregistered hotkey kept its admitted generation")
	}
	header, sessionHash, digest = validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	if lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now); lease != nil || !errors.Is(err, crv4.ErrValidatorScheduleHotkeyUnregistered) {
		if lease != nil {
			lease.Close()
		}
		t.Fatalf("deregistered hotkey was retained or refused without its cause: %v", err)
	}
	if len(*reports) != 2 || (*reports)[1].err != nil || (*reports)[1].retained != 0 || (*reports)[1].admitted != 0 {
		t.Fatalf("deregistered hotkey was reported as retained: %+v", *reports)
	}
}

// A changed canonical anchor is a contradiction, not an unavailable read: the
// verified entry is retired at once and the refusal names the change.
func TestValidatorUploadAdmissionRetiresVerifiedEntryWhenAnchorChanges(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, _ := validatorUploadSilentDropTestState(t, fixture)
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	prior, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	defer prior.Close()
	fixture.fault = "native-anchor-changed"
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	<-prior.Context().Done()
	if lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, fixture.now); lease != nil || err == nil || !strings.Contains(err.Error(), "canonical block changed") {
		if lease != nil {
			lease.Close()
		}
		t.Fatalf("changed anchor retained admission or hid its cause: %v", err)
	}
}

// The next refresh starts early enough to publish before the current state
// expires, measured by the last refresh, and never more often than each second.
func TestValidatorUploadAdmissionSchedulesRefreshBeforeExpiry(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, _ := validatorUploadSilentDropTestState(t, fixture)
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	// The window is 60s and the configured interval 10s.
	owner.lastRefreshDuration = 40 * time.Second
	owner.clock = func() time.Time { return fixture.now.Add(5 * time.Second) }
	if delay := owner.nextRefreshDelay(); delay != 5*time.Second {
		t.Fatalf("a slow refresh was scheduled to finish after expiry: %v", delay)
	}
	owner.clock = func() time.Time { return fixture.now.Add(20 * time.Second) }
	if delay := owner.nextRefreshDelay(); delay != time.Second {
		t.Fatalf("an unfinishable window was not refreshed at the bounded rate: %v", delay)
	}
	owner.lastRefreshDuration = time.Second
	if delay := owner.nextRefreshDelay(); delay != 10*time.Second {
		t.Fatalf("a fast refresh lost its configured interval: %v", delay)
	}
}
