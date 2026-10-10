//go:build linux || darwin

package validator

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validatorUploadSeededTestConfig(t *testing.T, fixture *validatorUploadAuthorityTestFixture) ValidatorUploadAdmissionConfig {
	t.Helper()
	record := fixture.authority.Expected
	domain, err := record.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	context := ReleaseEvidenceV2ActivationContext{
		Schema: ReleaseEvidenceV2ActivationContextSchema, Activation: record,
		InitialCut: AttemptCutV2Context{
			Identity: AttemptLedgerIdentity{DeploymentID: "real-activation-reader-fixture", ChainID: record.Domain.ChainID, GenesisHash: attemptHex32(record.Domain.GenesisHash), Netuid: record.Domain.Netuid,
				ValidatorID: 1, ValidatorUID: fixture.authority.ValidatorUID, NoID: record.NoID, ValidatorVPK: attemptHex32(record.VPK)},
			Activation:    AttemptCutV2Activation{Domain: domain, Hotkey: record.Hotkey, FirstSequence: record.FirstSequence, PriorRoot: attemptHex32(record.PriorRoot)},
			Boundary:      AttemptBoundary{SettlementEpoch: record.Domain.Epoch, EVMBlock: fixture.block, EVMBlockHash: attemptHex32(fixture.blockHash)},
			FirstSequence: 1, EgressFirstSequence: 1, EgressGeneration: 1, PriorRoot: zeroAttemptHash(),
		},
		ValidatorUID: fixture.authority.ValidatorUID, Journal: [20]byte(fixture.authority.Journal), RuntimeHash: fixture.authority.RuntimeHash,
		ObservedEVMBlock: fixture.block, ObservedEVMHash: fixture.blockHash,
	}
	config := fixture.config()
	encoded, err := context.CanonicalJSON(config.MaximumContextBytes)
	if err != nil {
		t.Fatal(err)
	}
	config.ActivationContexts = []ReleaseEvidenceV2File{writeReleaseBootstrapV2TestFile(t, filepath.Join(t.TempDir(), "owner", "context.json"), encoded)}
	config.ProvisionalSeededDiscoveryOnly = true
	return config
}

// The real HTTP fixture denies discovery exactly as the public provider did.
// Explicit seeds still pass real activation ABI and native schedule readers.
func TestValidatorUploadAdmissionProvisionalSeededDiscovery(t *testing.T) {
	for _, test := range []string{"strict-denied", "provisional-admitted", "wrong-activation", "current-permit-lost"} {
		t.Run(test, func(t *testing.T) {
			fixture := newValidatorUploadAuthorityTestFixture(t)
			config := validatorUploadSeededTestConfig(t, fixture)
			fixture.fault = "logs-denied"
			switch test {
			case "strict-denied":
				config.ProvisionalSeededDiscoveryOnly = false
			case "wrong-activation":
				fixture.fault = "wrong-digest"
			case "current-permit-lost":
				fixture.currentPermit = false
			}
			owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
			err = owner.refresh(t.Context(), fixture.now)
			if test == "strict-denied" {
				if err == nil || !strings.Contains(err.Error(), "Method not allowed") || fixture.logCalls.Load() != 1 {
					t.Fatalf("strict discovery did not retain actual RPC refusal: calls=%d err=%v", fixture.logCalls.Load(), err)
				}
				return
			}
			if err != nil || fixture.logCalls.Load() != 0 {
				t.Fatalf("seeded discovery issued a log scan or failed: calls=%d err=%v", fixture.logCalls.Load(), err)
			}
			header, session, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
			lease, err := owner.beginAt(t.Context(), header, session, 1, digest, 6, fixture.now)
			if lease != nil {
				defer lease.Close()
			}
			if test == "provisional-admitted" {
				if err != nil || lease == nil {
					t.Fatalf("authenticated seeded owner was refused: %v", err)
				}
			} else if err == nil || lease != nil {
				t.Fatal("seeded discovery bypassed activation or current native eligibility")
			}
		})
	}
}

func TestValidatorUploadAdmissionProvisionalSeededConfigAndCustody(t *testing.T) {
	fixture := newValidatorUploadAuthorityTestFixture(t)
	config := validatorUploadSeededTestConfig(t, fixture)
	for _, test := range []string{"other-chain", "no-seeds", "changed-file-hash"} {
		t.Run(test, func(t *testing.T) {
			candidate := config
			candidate.ActivationContexts = append([]ReleaseEvidenceV2File(nil), config.ActivationContexts...)
			switch test {
			case "other-chain":
				candidate.Deployment.ChainID = 1
			case "no-seeds":
				candidate.ActivationContexts = nil
			case "changed-file-hash":
				candidate.ActivationContexts[0].SHA256 = attemptHex32([32]byte{1})
			}
			if owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, candidate); err == nil || owner != nil {
				if owner != nil {
					owner.cancel()
				}
				t.Fatalf("%s allowed provisional seed authority", test)
			}
		})
	}
}

func TestValidatorUploadAdmissionSeededHistoryReuseKeepsCurrentEligibilityFresh(t *testing.T) {
	fixture := newValidatorUploadAuthorityTestFixture(t)
	config := validatorUploadSeededTestConfig(t, fixture)
	config.RefreshSeconds, config.MaximumHeadAgeSeconds = 120, 600
	owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	historicalReads, currentReads := fixture.activationReads.Load(), fixture.currentCanonicalCalls
	if historicalReads == 0 || len(owner.seededHistory) != 1 || owner.nextRefreshDelay() != 120*time.Second {
		t.Fatal("successful seeded history was not retained with its normal refresh interval")
	}
	header, session, digest := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	lease, err := owner.beginAt(t.Context(), header, session, 1, digest, 6, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if fixture.activationReads.Load() != historicalReads || fixture.currentCanonicalCalls <= currentReads {
		t.Fatal("warm refresh repeated historical activation calls or skipped current native observation")
	}
	fixture.currentPermit = false
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("fresh native permit loss did not retire the cached owner's lease")
	}
	if next, err := owner.beginAt(t.Context(), header, session, 1, digest, 6, fixture.now); err == nil || next != nil {
		if next != nil {
			next.Close()
		}
		t.Fatal("historical success authorized a currently ineligible owner")
	}
	if fixture.activationReads.Load() != historicalReads || len(owner.seededHistory) != 1 || owner.nextRefreshDelay() != 30*time.Second {
		t.Fatal("permit loss discarded immutable history or delayed the incomplete refresh")
	}
	fixture.currentPermit = true
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if fixture.activationReads.Load() != historicalReads || len(owner.seededFailures) != 0 || owner.nextRefreshDelay() != 120*time.Second {
		t.Fatal("recovered current eligibility did not reuse history and clear its failure")
	}
}

// One real seed succeeds while a second well-formed configured record is not
// published. Retry must preserve the first proof and actually reread the second.
func TestValidatorUploadAdmissionSeededPartialHistoryRetriesOnlyUnverifiedSeed(t *testing.T) {
	fixture := newValidatorUploadAuthorityTestFixture(t)
	config := validatorUploadSeededTestConfig(t, fixture)
	config.RefreshSeconds, config.MaximumHeadAgeSeconds = 120, 600
	encoded, err := ReadReleaseEvidenceV2File(t.Context(), config.ActivationContexts[0], config.MaximumContextBytes)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := decodeReleaseEvidenceV2ActivationContext(encoded, config.MaximumContextBytes)
	if err != nil {
		t.Fatal(err)
	}
	missing.Activation.NoID = 3
	missing.InitialCut.Identity.NoID = 3
	missing.InitialCut.Activation.Domain, err = missing.Activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = missing.CanonicalJSON(config.MaximumContextBytes)
	if err != nil {
		t.Fatal(err)
	}
	config.ActivationContexts = append(config.ActivationContexts, writeReleaseBootstrapV2TestFile(t, filepath.Join(t.TempDir(), "owner", "pending.json"), encoded))
	owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	close(owner.ready) // The owned loop signals its first completed pass here.
	if err := owner.WaitReady(t.Context()); !errors.Is(err, ErrValidatorEvidenceAbsent) {
		t.Fatalf("partial seeded admission reported complete readiness: %v", err)
	}
	if len(owner.seededHistory) != 1 || len(owner.entries) != 1 || owner.nextRefreshDelay() != 30*time.Second {
		t.Fatal("partial refresh lost its success, admitted the missing seed, or failed to schedule prompt retry")
	}
	header, session, digest := validatorUploadAdmissionTestHeader(t, fixture, missing.Activation, 0x41, "current-api-client", []byte("object"))
	if lease, err := owner.beginAt(t.Context(), header, session, 1, digest, 6, fixture.now); lease != nil || !errors.Is(err, ErrValidatorEvidenceAbsent) || !strings.Contains(err.Error(), "historical activation authentication") {
		if lease != nil {
			lease.Close()
		}
		t.Fatalf("absent seed did not retain its actual authentication error: %v", err)
	}
	reads, absent := fixture.activationReads.Load(), fixture.absentActivationReads.Load()
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if fixture.activationReads.Load() != reads+1 || fixture.absentActivationReads.Load() != absent+1 || len(owner.seededHistory) != 1 {
		t.Fatal("partial retry cached an unverified seed or repeated the successful seed's history")
	}
}

func TestValidatorUploadAdmissionSeededHistoryRejectsChangedCanonicalAnchor(t *testing.T) {
	fixture := newValidatorUploadAuthorityTestFixture(t)
	config := validatorUploadSeededTestConfig(t, fixture)
	owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	fixture.observerReorg.Store(true)
	if err := owner.refresh(t.Context(), fixture.now); err == nil {
		t.Fatal("changed canonical EVM anchor passed a warm refresh")
	}
	if len(owner.seededHistory) != 0 {
		t.Fatal("historical success survived a changed canonical anchor")
	}
}

func TestValidatorUploadAdmissionSeededHistorySurvivesUnavailableAnchor(t *testing.T) {
	fixture := newValidatorUploadAuthorityTestFixture(t)
	config := validatorUploadSeededTestConfig(t, fixture)
	owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	reads := fixture.activationReads.Load()
	fixture.fault = "canonical-unavailable"
	if err := owner.refresh(t.Context(), fixture.now); err == nil {
		t.Fatal("unavailable current canonical read admitted a refresh")
	} else {
		owner.invalidate(err)
	}
	if len(owner.seededHistory) != 1 || len(owner.entries) != 0 {
		t.Fatal("read unavailability erased immutable proof or retained current admission")
	}
	fixture.fault = ""
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if len(owner.entries) != 1 || fixture.activationReads.Load() != reads {
		t.Fatal("recovered canonical reads failed to reuse successful historical proof")
	}
}
