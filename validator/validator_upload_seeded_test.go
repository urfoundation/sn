//go:build linux || darwin

package validator

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
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
