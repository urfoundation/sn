//go:build linux || darwin

// A fresh evidence generation can retain the previous native source's role
// proof without importing its measurements, policy authority, or intent state.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
)

const ReleaseSourceRolePredecessorV2Schema = "urnetwork-validator-source-role-predecessor-v2"
const ReleaseSourceRolePredecessorV2MaximumBytes = 1024 * 1024

// The approved descriptor pins existing signed bytes. It is an authentication
// candidate until the original native signature and finalized receipt verify.
type ReleaseSourceRolePredecessorV2 struct {
	Schema      string                `json:"schema"`
	Intent      SteeringIntent        `json:"intent"`
	Measurement ReleaseEvidenceV2File `json:"measurement"`
	Envelope    ReleaseEvidenceV2File `json:"envelope"`
}

// Only authenticated native finality creates this role witness. It carries
// no scheduling cursor, acceptance credit, economic output, or mutable state.
type releaseSourceRoleWitnessV2 struct {
	hotkey          [32]byte
	netuid          uint16
	sourceHash      string
	commitmentBlock uint64
}

// Read the exact previous store chosen by the signed generation handoff.
// Absence is allowed only before any intent exists; malformed history fails.
func PrepareReleaseSourceRolePredecessorV2(ctx context.Context, cfg *ReleaseConfig, previousStateDir string) ([]byte, error) {
	if ctx == nil || cfg == nil || cfg.EvidenceV2.Bounds.IntentFileLimit() == 0 {
		return nil, errors.New("source role predecessor preparation is incomplete")
	}
	if err := ValidateReleaseEvidenceV2Path(previousStateDir); err != nil {
		return nil, err
	}
	encoded, err := ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(previousStateDir, "steering-intents.json"), cfg.EvidenceV2.Bounds.IntentFileLimit())
	if ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file steeringIntentFile
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	canonical, err := json.MarshalIndent(file, "", "  ")
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) || file.Schema != steeringIntentSchema {
		return nil, errors.Join(errors.New("source role predecessor intent file is not canonical"), err)
	}
	if file.Current == nil {
		if len(file.History) != 0 {
			return nil, errors.New("source role predecessor history lost its current intent")
		}
		return nil, nil
	}
	intent := *file.Current
	if intent.Status != "finalized" && intent.Status != "applied" {
		return nil, errors.New("source role predecessor has an unresolved current intent")
	}
	if err := validateSteeringIntentLifecycle(&intent, false); err != nil {
		return nil, err
	}
	measurementName := filepath.Join("measurements", strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:")+".json")
	envelopeName := filepath.Join("measurements", "envelopes", strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:")+".json")
	if intent.MeasurementArtifactPath != filepath.ToSlash(measurementName) || intent.MeasurementEnvelopePath != filepath.ToSlash(envelopeName) {
		return nil, errors.New("source role predecessor references are not content addressed")
	}
	proof := ReleaseSourceRolePredecessorV2{
		Schema: ReleaseSourceRolePredecessorV2Schema, Intent: intent,
		Measurement: ReleaseEvidenceV2File{Path: filepath.Join(previousStateDir, measurementName), Bytes: intent.MeasurementArtifactSize, SHA256: "0x" + strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:")},
		Envelope:    ReleaseEvidenceV2File{Path: filepath.Join(previousStateDir, envelopeName), Bytes: intent.MeasurementEnvelopeSize, SHA256: "0x" + strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:")},
	}
	encoded, err = json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if _, err := DecodeReleaseSourceRolePredecessorV2(ctx, cfg, encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

// Rendering verifies exact bytes and signed actor identity. Native finality
// remains a separate runtime check; this decoder cannot create a role witness.
func DecodeReleaseSourceRolePredecessorV2(ctx context.Context, cfg *ReleaseConfig, encoded []byte) (*ReleaseSourceRolePredecessorV2, error) {
	proof, _, _, err := decodeReleaseSourceRolePredecessorV2(ctx, cfg, encoded)
	return proof, err
}

// Decode the finite canonical descriptor and its independently pinned signed
// references. The predecessor policy may differ; custody identity may not.
func decodeReleaseSourceRolePredecessorV2(ctx context.Context, cfg *ReleaseConfig, encoded []byte) (*ReleaseSourceRolePredecessorV2, *ReleaseMeasurementArtifact, *ReleaseMeasurementEnvelope, error) {
	if ctx == nil || cfg == nil || len(encoded) == 0 || len(encoded) > ReleaseSourceRolePredecessorV2MaximumBytes {
		return nil, nil, nil, errors.New("source role predecessor descriptor exceeds its bound")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	var proof ReleaseSourceRolePredecessorV2
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil {
		return nil, nil, nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, nil, errors.Join(errors.New("source role predecessor has trailing data"), err)
	}
	canonical, err := json.MarshalIndent(proof, "", "  ")
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) || proof.Schema != ReleaseSourceRolePredecessorV2Schema {
		return nil, nil, nil, errors.Join(errors.New("source role predecessor descriptor is not canonical"), err)
	}
	intent := &proof.Intent
	if intent.ValidatorID != cfg.ValidatorID || intent.Netuid != cfg.Netuid || intent.FinalizedBlock == 0 || intent.Status != "finalized" && intent.Status != "applied" {
		return nil, nil, nil, errors.New("source role predecessor lacks this validator's finalized intent")
	}
	if err := validateSteeringIntentLifecycle(intent, false); err != nil {
		return nil, nil, nil, err
	}
	if proof.Measurement.Bytes != intent.MeasurementArtifactSize || proof.Measurement.SHA256 != "0x"+strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:") || proof.Envelope.Bytes != intent.MeasurementEnvelopeSize || proof.Envelope.SHA256 != "0x"+strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:") {
		return nil, nil, nil, errors.New("source role predecessor references differ from the retained intent")
	}
	measurement, err := ReadReleaseEvidenceV2File(ctx, proof.Measurement, cfg.EvidenceV2.Bounds.MaxArtifactBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, cfg.EvidenceV2.Bounds.MaxArtifactBytes, cfg.EvidenceV2.Bounds.MaxOperators)
	if err != nil {
		return nil, nil, nil, err
	}
	envelopeBytes, err := ReadReleaseEvidenceV2File(ctx, proof.Envelope, min(cfg.EvidenceV2.Bounds.MaxControlBytes, ReleaseMeasurementEnvelopeV2MaximumBytes))
	if err != nil {
		return nil, nil, nil, err
	}
	envelope, err := DecodeReleaseMeasurementEnvelopeV2(ctx, envelopeBytes, cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	if !releaseMeasurementEnvelopeMatchesArtifact(envelope, artifact, intent.SelfUID) || envelope.DeploymentID != cfg.DeploymentID || envelope.ValidatorID != cfg.ValidatorID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.GenesisHash) || !strings.EqualFold(envelope.Coordinator, cfg.Coordinator) || !strings.EqualFold(envelope.SettlementVault, cfg.SettlementVault) || envelope.MeasurementArtifactHash != intent.MeasurementArtifactHash || envelope.MeasurementArtifactSize != proof.Measurement.Bytes || envelope.PreparedExtrinsicHash != intent.Prepared.ExtrinsicHash || envelope.ValidatorHotkey != intent.Prepared.HotkeyHex || intent.Prepared.Netuid != cfg.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || artifact.SubnetEpoch != intent.SubnetEpoch || artifact.PolicyHash != intent.PolicyHash {
		return nil, nil, nil, errors.New("source role predecessor signed domain differs from its custody owner")
	}
	return &proof, artifact, envelope, ctx.Err()
}

// Rollout authentication proves the historical write and the currently occupied
// slot before selecting a fresh child configuration. Runtime continuation uses
// its ordinary current-intent history after this predecessor is overwritten.
func VerifyReleaseSourceRolePredecessorV2(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, hotkey [32]byte) error {
	witness, err := authenticateReleaseSourceRolePredecessorV2(ctx, cfg, native, hotkey)
	if err != nil {
		return err
	}
	if native == nil || hotkey == ([32]byte{}) {
		return errors.New("source role predecessor native owner is incomplete")
	}
	own := *native
	head, err := authenticatePinnedNativeRuntimeContext(ctx, &own, cfg)
	if err != nil {
		return err
	}
	observed, err := own.SourceCommitmentSlotAtContext(ctx, cfg.Netuid, hotkey, head)
	if err != nil {
		return err
	}
	if witness == nil && observed == nil {
		return ctx.Err()
	}
	if !witness.matches(cfg.Netuid, hotkey, observed) {
		return errors.New("source role predecessor does not match the current native slot")
	}
	return ctx.Err()
}

// The original source signature, atomic batch shape, historical validator
// eligibility, canonical inclusion, and exact native write all precede authority.
func authenticateReleaseSourceRolePredecessorV2(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, hotkey [32]byte) (*releaseSourceRoleWitnessV2, error) {
	if cfg == nil || ctx == nil {
		return nil, errors.New("source role predecessor owner is incomplete")
	}
	if cfg.SourceRolePredecessorV2 == nil {
		return nil, ctx.Err()
	}
	encoded, err := ReadReleaseEvidenceV2File(ctx, *cfg.SourceRolePredecessorV2, ReleaseSourceRolePredecessorV2MaximumBytes)
	if err != nil {
		return nil, err
	}
	proof, artifact, envelope, err := decodeReleaseSourceRolePredecessorV2(ctx, cfg, encoded)
	if err != nil {
		return nil, err
	}
	if hotkey == ([32]byte{}) || envelope.ValidatorHotkey != releaseHex32(hotkey) {
		return nil, errors.New("source role predecessor is signed by another hotkey")
	}
	if err := authenticateReleaseNativeSourceReferenceV2(ctx, native, cfg, &proof.Intent, artifact); err != nil {
		return nil, fmt.Errorf("source role predecessor native proof: %w", err)
	}
	return &releaseSourceRoleWitnessV2{hotkey: hotkey, netuid: cfg.Netuid, sourceHash: proof.Intent.Prepared.SourceCommitment.Hash, commitmentBlock: proof.Intent.FinalizedBlock}, nil
}

// The retained witness is scoped to one role and exact finalized write. A
// newer source, another signer, or even the same hash at another block differs.
func (self *releaseSourceRoleWitnessV2) matches(netuid uint16, hotkey [32]byte, observed *crv4.FinalizedCommitment) bool {
	return self != nil && observed != nil && self.netuid == netuid && self.hotkey == hotkey && self.commitmentBlock != 0 && self.commitmentBlock == observed.CommitmentBlock && self.sourceHash == releaseHex32(observed.Hash)
}
