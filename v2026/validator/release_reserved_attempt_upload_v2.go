//go:build linux || darwin

// One source activation owns its VPK, while each replica owns an independent
// current API session. No destination can replace the original evidence owner.
package validator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
)

// Startup still authenticates both consents, immutable companion/runtime and
// historical native eligibility. This boundary preserves that exact identity;
// the destination server independently refreshes current eligibility.
func newReleaseAttemptUploadSourceV2(cfg *ReleaseConfig, input releaseEvidenceV2ActivationInput) (*releaseAttemptUploadSourceV2, error) {
	if cfg == nil || cfg.EvidenceV2.UploadIntentSeconds == 0 || cfg.EvidenceV2.UploadIntentSeconds > 3600 {
		return nil, errors.New("reserved upload runtime requires an explicit finite intent lifetime")
	}
	record := input.Context.Activation
	if input.Config.NoID != record.NoID || input.Candidate != record || input.Observation.Publication.Record != record ||
		input.Observation.Publication.PublishedBlock <= record.EVMBlock || input.Observation.ObservedEVMBlock < input.Observation.Publication.PublishedBlock ||
		input.Observation.ObservedEVMHash == ([32]byte{}) || input.Observation.Native.Identity.Hotkey != record.Hotkey || !input.Observation.Native.MeetsNonSelfStakeAndPermit() {
		return nil, errors.New("reserved upload source differs from authenticated activation startup")
	}
	if err := record.Verify(record, input.VPKSignature, input.HotkeySignature); err != nil {
		return nil, err
	}
	if err := validateReleaseMeasurementInputV2Context(cfg, record.NoID, input.Context.InitialCut); err != nil {
		return nil, err
	}
	signer, err := newValidatorAttemptUploadSigner(record, record.NoID, cfg.EvidenceV2.UploadIntentSeconds, input.PrivateKey)
	if err != nil {
		return nil, err
	}
	return &releaseAttemptUploadSourceV2{activation: record, maximumIntentSeconds: cfg.EvidenceV2.UploadIntentSeconds, privateKey: signer.privateKey}, nil
}

// The ordinary census validator admits every actual destination owner before
// credentials or HTTP are used. Protected callbacks retain the original source
// activation for either origin, including a different operator's JWT session.
func releaseReservedAttemptUploadReplicasV2(cfg *ReleaseConfig, source *releaseAttemptUploadSourceV2, origins [2]string, runtimes []*releaseOperatorRuntime) ([2]AttemptCutV2Replica, error) {
	var zero [2]AttemptCutV2Replica
	if source == nil {
		return zero, errors.New("reserved upload source is unavailable")
	}
	replicas, err := releaseAttemptUploadReplicasV2(cfg, origins, runtimes)
	if err != nil {
		return zero, err
	}
	for index, origin := range origins {
		var owner *releaseAttemptUploadV2
		for _, runtime := range runtimes {
			if runtime.attemptUpload.origin == origin {
				owner = runtime.attemptUpload
				break
			}
		}
		if owner == nil {
			return zero, errors.New("reserved upload destination disappeared after census admission")
		}
		signer, err := newValidatorAttemptUploadSigner(source.activation, owner.noID, source.maximumIntentSeconds, ed25519.PrivateKey(source.privateKey[:]))
		if err != nil {
			return zero, err
		}
		writer := func(kind string) AttemptStreamV2ObjectWriter {
			return func(ctx context.Context, hash string, raw []byte) error {
				return owner.writeWithSigner(ctx, kind, hash, raw, signer)
			}
		}
		replicas[index] = AttemptCutV2Replica{Origin: origin, WriteRecords: writer(AttemptStreamV2Records), WriteProofs: writer(AttemptStreamV2Proofs), WriteMetadata: writer("metadata")}
	}
	return replicas, nil
}

// A closed census uploads every member with its own original activation/VPK.
// The shared census body uses the first admitted member's reservation, while
// payloads and consents keep their actual member owner at both destinations.
func releaseReservedAttemptCensusReplicasV2(cfg *ReleaseConfig, origins [2]string, runtimes []*releaseOperatorRuntime) (map[uint64][2]AttemptCutV2Replica, error) {
	if _, err := releaseAttemptUploadReplicasV2(cfg, origins, runtimes); err != nil {
		return nil, err
	}
	result := make(map[uint64][2]AttemptCutV2Replica, len(runtimes))
	for _, runtime := range runtimes {
		source := runtime.attemptSource
		if source == nil || source.activation.NoID != runtime.measurement.NoID {
			return nil, errors.New("reserved census source differs from its original runtime")
		}
		replicas, err := releaseReservedAttemptUploadReplicasV2(cfg, source, origins, runtimes)
		if err != nil {
			return nil, err
		}
		result[source.activation.NoID] = replicas
	}
	return result, nil
}

// Runtime sealing binds its actual ledger/source before delegating to the
// complete sealer and independent dual public readback. This does not remove
// the separate startup/submission/closed-census integration fence.
func sealReleaseRuntimeAttemptCutV2(ctx context.Context, cfg *ReleaseConfig, source *releaseOperatorRuntime, expected AttemptCutV2Context, origins [2]string, runtimes []*releaseOperatorRuntime, serverKeys map[byte]ed25519.PublicKey, scratchDirectory string) (*AttemptCutV2Publication, error) {
	if ctx == nil || cfg == nil || source == nil || source.attemptSource == nil || source.attemptLedger == nil || source.measurement == nil {
		return nil, errors.New("reserved runtime sealing ownership is incomplete")
	}
	activation := source.attemptSource.activation
	domain, err := activation.EvidenceDomain()
	if err != nil {
		return nil, err
	}
	if source.measurement.NoID != activation.NoID || expected.Identity.NoID != activation.NoID || expected.Identity.ValidatorVPK != attemptHex32(activation.VPK) || expected.Activation.Domain != domain || expected.Activation.Hotkey != activation.Hotkey {
		return nil, errors.New("reserved runtime cut differs from its original source activation")
	}
	if err := validateReleaseMeasurementInputV2Context(cfg, activation.NoID, expected); err != nil {
		return nil, err
	}
	replicas, err := releaseReservedAttemptUploadReplicasV2(cfg, source.attemptSource, origins, runtimes)
	if err != nil {
		return nil, fmt.Errorf("reserved runtime replicas: %w", err)
	}
	return SealReplicatedAttemptCutV2(ctx, source.attemptLedger, expected, cfg.Policy, ed25519.PrivateKey(source.attemptSource.privateKey[:]), cfg.EvidenceV2.Bounds.Cut,
		AttemptCutV2ReplicaOptions{ReplayBounds: cfg.EvidenceV2.Bounds.Replay, ScratchDirectory: scratchDirectory, ServerKeys: serverKeys, Replicas: replicas})
}
