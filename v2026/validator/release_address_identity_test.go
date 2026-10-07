//go:build linux || darwin

// Checksum configuration and canonical signed sources name one deployment.
// Tests traverse capture/request admission without granting a replay verdict.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urnetwork/connect/v2026"
)

// Both operator activations sign the same synthetic checksum address bytes.
func newReleaseAddressIdentityTestFixture(t *testing.T) (*releaseBootstrapV2TestFixture, *ReleaseMeasurementArtifact) {
	t.Helper()
	f := newReleaseBootstrapV2TestFixture(t, func(cfg *ReleaseConfig) {
		cfg.Coordinator = common.Address{0xab, 0xcd, 0xef, 0x12, 0x34}.Hex()
		cfg.SettlementVault = common.Address{0xfe, 0xdc, 0xba, 0x56, 0x78}.Hex()
	})
	if f.cfg.Coordinator == strings.ToLower(f.cfg.Coordinator) || f.cfg.SettlementVault == strings.ToLower(f.cfg.SettlementVault) {
		t.Fatal("fixture does not distinguish checksum configuration from signed spelling")
	}
	a := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchemaV2, DeploymentID: f.cfg.DeploymentID, ChainID: f.cfg.ChainID,
		GenesisHash: f.cfg.GenesisHash, Coordinator: strings.ToLower(f.cfg.Coordinator), SettlementVault: strings.ToLower(f.cfg.SettlementVault),
		ValidatorID: f.cfg.ValidatorID, Netuid: f.cfg.Netuid, SubnetEpoch: 7, SettlementEpoch: 3, SelfUID: 5,
		NativeSnapshotBlock: 100, NativeSnapshotHash: releaseHex32([32]byte{0x31}),
		EVMSnapshotBlock: 90, EVMSnapshotHash: releaseHex32([32]byte{0x32}), PolicyHash: f.cfg.PolicyHash, Policy: f.cfg.Policy,
		ControlledNOIDs: []uint64{}, Inputs: []ReleaseMeasurementInput{}, Bindings: []ReleaseBindingMeasurement{},
		HeadEMA: []HeadEMAMeasurement{}, Pools: []ReleasePoolMeasurement{}, DepositAudits: []DepositAudit{},
	}
	return f, a
}

// Retain canonical measurement bytes and a hash-bound next source. The sink
// stops at envelope custody, before any historical replay or native read.
func persistReleaseAddressCaptureTestIntent(t *testing.T, f *releaseBootstrapV2TestFixture, a *ReleaseMeasurementArtifact) []byte {
	t.Helper()
	measurement, err := canonicalReleaseMeasurementBytes(a)
	if err != nil {
		t.Fatal(err)
	}
	i := &SteeringIntent{
		Schema: SteeringIntentSchema, ValidatorID: a.ValidatorID, Netuid: a.Netuid, SubnetEpoch: a.SubnetEpoch,
		NativeSnapshotBlock: a.NativeSnapshotBlock, NativeSnapshotHash: a.NativeSnapshotHash,
		EVMSnapshotBlock: a.EVMSnapshotBlock, EVMSnapshotHash: a.EVMSnapshotHash,
		SettlementEpoch: a.SettlementEpoch, PolicyHash: a.PolicyHash, SelfUID: a.SelfUID, DepositAudits: a.DepositAudits,
		Prepared:                &crv4.PreparedSubmission{HotkeyHex: releaseHex32(f.contexts[0].Activation.Hotkey)},
		MeasurementArtifactHash: ReleaseMeasurementContentHash(measurement),
	}
	i.MeasurementArtifactPath, i.MeasurementArtifactSize, err = persistReleaseMeasurementArtifact(f.cfg.StateDir, measurement, i.MeasurementArtifactHash)
	if err != nil {
		t.Fatal(err)
	}
	envelope := []byte("{}\n")
	i.MeasurementEnvelopeHash = ReleaseMeasurementEnvelopeContentHash(envelope)
	i.MeasurementEnvelopePath, i.MeasurementEnvelopeSize, err = persistReleaseMeasurementEnvelope(f.cfg.StateDir, envelope, i.MeasurementEnvelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	i.VectorHash, err = i.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(steeringIntentFile{Schema: SteeringIntentSchema, Current: i, History: []SteeringIntent{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cfg.StateDir, "steering-intents.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return measurement
}

// The real capture used to stop before the next source solely because its
// independently authenticated configuration retained checksum capitalization.
func TestReleaseAddressIdentityCapturePreservesChecksumAndSignedBytes(t *testing.T) {
	f, a := newReleaseAddressIdentityTestFixture(t)
	measurement := persistReleaseAddressCaptureTestIntent(t, f, a)
	coordinator, vault := f.cfg.Coordinator, f.cfg.SettlementVault
	before := f.calls()
	want := errors.New("synthetic envelope sink refusal")
	var capturedMeasurement []byte
	envelopes := 0
	result, err := CaptureReleaseEvidenceV2(t.Context(), &f.cfg, f.chain, f.native, releaseCaptureV2TestOptions(f), func(_ context.Context, source ReleaseEvidenceV2CaptureSource, raw []byte) error {
		if strings.HasPrefix(source.Name, "measurements/envelopes/") {
			envelopes++
			return want
		}
		if strings.HasPrefix(source.Name, "measurements/") {
			capturedMeasurement = bytes.Clone(raw)
		}
		return nil
	})
	if !errors.Is(err, want) || result != nil || envelopes != 1 || f.calls() != before || !bytes.Equal(measurement, capturedMeasurement) || f.cfg.Coordinator != coordinator || f.cfg.SettlementVault != vault {
		t.Fatalf("address spelling changed source admission/custody: envelopes=%d calls=%d/%d error=%v", envelopes, f.calls(), before, err)
	}
}

// A valid content hash and vector cannot substitute another contract or chain;
// rejection still precedes the next source and every historical chain read.
func TestReleaseAddressIdentityCaptureRejectsDifferentDeployment(t *testing.T) {
	f, original := newReleaseAddressIdentityTestFixture(t)
	for _, field := range []string{"coordinator", "vault", "chain", "genesis"} {
		a := *original
		switch field {
		case "coordinator":
			a.Coordinator = strings.ToLower(common.Address{0x79}.Hex())
		case "vault":
			a.SettlementVault = strings.ToLower(common.Address{0x79}.Hex())
		case "chain":
			a.ChainID++
		case "genesis":
			a.GenesisHash = releaseHex32([32]byte{0x79})
		}
		persistReleaseAddressCaptureTestIntent(t, f, &a)
		before, envelopes := f.calls(), 0
		_, err := CaptureReleaseEvidenceV2(t.Context(), &f.cfg, f.chain, f.native, releaseCaptureV2TestOptions(f), func(_ context.Context, source ReleaseEvidenceV2CaptureSource, _ []byte) error {
			if strings.HasPrefix(source.Name, "measurements/envelopes/") {
				envelopes++
			}
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "configured deployment or exact intent") || envelopes != 0 || f.calls() != before {
			t.Fatalf("changed %s escaped exact capture authority: envelopes=%d error=%v", field, envelopes, err)
		}
	}
}

// Request domains use exact address bytes while their immutable source keeps
// canonical spelling. Both neighboring live/retained readers use this boundary.
func TestReleaseAddressIdentityObservationRequestsUseExactContractBytes(t *testing.T) {
	f, a := newReleaseAddressIdentityTestFixture(t)
	hotkey := f.contexts[0].Activation.Hotkey
	noId := f.cfg.Operators[0].NoID
	reader, err := NewHTTPArtifactReader("https://operator.example", f.cfg.DeploymentID, f.cfg.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	domain, request, err := releaseClientKeyDecisionV2(&f.cfg, noId, hotkey, a, connect.Id{0x55})
	if err != nil || domain.Coordinator != common.HexToAddress(f.cfg.Coordinator) || domain.SettlementVault != common.HexToAddress(f.cfg.SettlementVault) || request.NativeBlock != a.NativeSnapshotBlock {
		t.Fatalf("client-key request rejected unchanged contract bytes: %v", err)
	}
	observation, _, err := releaseArtifactHttpRequestV2(&f.cfg, hotkey, a, noId, a.SettlementEpoch-f.cfg.Policy.Deposit.UsageLagEpochs, reader)
	if err != nil || observation.Decision.Coordinator != a.Coordinator || observation.Decision.SettlementVault != a.SettlementVault {
		t.Fatalf("artifact request changed canonical signed decision: %v", err)
	}
	for _, field := range []string{"coordinator", "vault", "chain", "genesis"} {
		changed := *a
		switch field {
		case "coordinator":
			changed.Coordinator = strings.ToLower(common.Address{0x75}.Hex())
		case "vault":
			changed.SettlementVault = strings.ToLower(common.Address{0x75}.Hex())
		case "chain":
			changed.ChainID++
		case "genesis":
			changed.GenesisHash = releaseHex32([32]byte{0x75})
		}
		if _, _, err := releaseClientKeyDecisionV2(&f.cfg, noId, hotkey, &changed, connect.Id{0x55}); err == nil {
			t.Fatalf("client-key request accepted foreign %s", field)
		}
		if _, _, err := releaseArtifactHttpRequestV2(&f.cfg, hotkey, &changed, noId, changed.SettlementEpoch-f.cfg.Policy.Deposit.UsageLagEpochs, reader); err == nil {
			t.Fatalf("artifact request accepted foreign %s", field)
		}
	}
}

// Hex decoders may pad/truncate malformed values; those are not alternate
// encodings of an approved address, even when both malformed strings match.
func TestReleaseAddressIdentityRejectsMalformedAliases(t *testing.T) {
	address := common.Address{0xab, 0xcd, 0xef}.Hex()
	if !releaseAddressIdentityMatches(address, strings.ToLower(address)) {
		t.Fatal("checksum spelling changed address identity")
	}
	for _, malformed := range []string{"", "0xab", address + "00", "00" + address, "0xzz" + address[4:], " " + address} {
		if releaseAddressIdentityMatches(malformed, address) || releaseAddressIdentityMatches(address, malformed) || releaseAddressIdentityMatches(malformed, malformed) {
			t.Fatalf("malformed address alias accepted: %q", malformed)
		}
	}
}
