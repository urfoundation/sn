// A production sidecar preserves the original V2 provider transcript while
// binding its approved owner-recycle row through native preparation, hotkey
// signature, durable intent and independently observed archive replay.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"slices"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

const ownerRecycleProductionDecisionSchema = "urnetwork-owner-recycle-production-decision-v1"
const maximumOwnerRecycleProductionProofBytes = uint64(2 * 1024 * 1024)

// The pre-preparation body contains no transaction or signature, avoiding a
// circular commitment. Provider measurements retain their original V2 bytes.
type OwnerRecycleProductionProof struct {
	Schema                  string                            `json:"schema"`
	Approval                []byte                            `json:"approval"`
	Census                  []byte                            `json:"census"`
	OperatorEvidence        []byte                            `json:"operator_evidence"`
	ProviderMeasurementHash string                            `json:"provider_measurement_hash"`
	Decision                ReleaseMeasurementV2Decision      `json:"decision"`
	Eligibility             OwnerRecycleProductionEligibility `json:"eligibility"`
	Row                     OwnerRecycleMeasuredRow           `json:"row"`
}

// This is signed in its own domain after the native source batch is prepared.
// Old provider envelopes still authenticate their unchanged original artifact.
type OwnerRecycleProductionIntent struct {
	Proof                 OwnerRecycleProductionProof `json:"proof"`
	Hotkey                string                      `json:"hotkey"`
	PreparedExtrinsicHash string                      `json:"prepared_extrinsic_hash"`
	ProviderEnvelopeHash  string                      `json:"provider_envelope_hash"`
	Signature             string                      `json:"signature"`
}

// Only authenticated native/operator observations create this transient owner.
// Candidate sidecar bytes cannot create a runtime or archive signing grant.
type ownerRecycleProductionStage struct {
	authority  *OwnerRecycleMeasurementAuthority
	proof      OwnerRecycleProductionProof
	encoded    []byte
	sourceHash [32]byte
}

// Both the in-memory census and complete serialized proof are bounded before
// callbacks or hashing; no larger implicit intent allowance is introduced.
func ownerRecycleProductionProofBytes(ctx context.Context, proof *OwnerRecycleProductionProof, limit uint64) ([]byte, error) {
	if ctx == nil || proof == nil || limit == 0 {
		return nil, errors.New("owner-recycle production proof owner or bound is absent")
	}
	limit = min(limit, maximumOwnerRecycleProductionProofBytes)
	remaining := limit
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(proof), &remaining); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(proof)
	if err != nil || uint64(len(encoded))+1 > limit || proof.Schema != ownerRecycleProductionDecisionSchema {
		return nil, errors.Join(errors.New("owner-recycle production proof schema or complete byte bound differs"), err)
	}
	return append(encoded, '\n'), ctx.Err()
}

// Full provider proof replay precedes this call. The independent operator
// observer replays it again under its own fresh bounded stream owner.
func prepareOwnerRecycleProductionDecision(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, chain *ChainClient,
	measurement []byte, artifact *ReleaseMeasurementArtifact, verified *VerifiedReleaseMeasurement, options ReleaseMeasurementV2Options,
) (*ownerRecycleProductionStage, error) {
	if !isOwnerRecycleProductionConfig(cfg) || artifact == nil || verified == nil {
		return nil, errors.New("owner-recycle production decision lacks its actual provider replay")
	}
	authority, err := ObserveOwnerRecycleMeasurementAuthority(ctx, cfg, native, options.Expected)
	if err != nil {
		return nil, err
	}
	authority, err = ObserveOwnerRecycleMeasurementOperators(ctx, authority, chain, measurement, options)
	if err != nil {
		return nil, err
	}
	eligibility, err := observeOwnerRecycleProductionEligibility(ctx, cfg, native, authority)
	if err != nil {
		return nil, err
	}
	row, err := deriveOwnerRecycleMeasuredRow(authority, artifact, verified)
	if err != nil {
		return nil, err
	}
	proof := OwnerRecycleProductionProof{Schema: ownerRecycleProductionDecisionSchema,
		Approval: bytes.Clone(authority.approval), Census: bytes.Clone(authority.census), OperatorEvidence: bytes.Clone(authority.operatorEvidence),
		ProviderMeasurementHash: ReleaseMeasurementContentHash(measurement), Decision: options.Expected, Eligibility: *eligibility, Row: row}
	encoded, err := ownerRecycleProductionProofBytes(ctx, &proof, options.MaxControlBytes)
	if err != nil {
		return nil, err
	}
	return &ownerRecycleProductionStage{authority: authority, proof: proof, encoded: encoded, sourceHash: ownerRecycleProductionSourceHash(measurement, encoded)}, nil
}

// Length framing and a distinct domain prevent either swapping the unchanged
// provider artifact or interpreting a V2 parent source as the successor row.
func ownerRecycleProductionSourceHash(measurement, proof []byte) [32]byte {
	hash := sha256.New()
	hash.Write([]byte("urnetwork-validator-owner-recycle-native-source-v1\x00"))
	hash.Write(binary.LittleEndian.AppendUint64(nil, uint64(len(measurement))))
	hash.Write(measurement)
	hash.Write(binary.LittleEndian.AppendUint64(nil, uint64(len(proof))))
	hash.Write(proof)
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

// Source identity is selected by the explicit sidecar wire, not runtime version.
// Authority and the exact resulting weights are authenticated by its caller.
func releaseIntentNativeSourceHash(ctx context.Context, measurement []byte, intent *SteeringIntent) ([32]byte, error) {
	if intent == nil || intent.OwnerRecycle == nil {
		return releaseNativeSourceHashV2(measurement), nil
	}
	proof, err := ownerRecycleProductionProofBytes(ctx, &intent.OwnerRecycle.Proof, maximumOwnerRecycleProductionProofBytes)
	if err != nil {
		return [32]byte{}, err
	}
	return ownerRecycleProductionSourceHash(measurement, proof), nil
}

// Hash only the canonical unsigned sidecar, after all bounded proof inputs have
// been owned. The existing provider envelope hash binds the other hotkey seal.
func ownerRecycleProductionSigningDigest(sidecar *OwnerRecycleProductionIntent) ([32]byte, error) {
	copy := *sidecar
	copy.Signature = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte("urnetwork-validator-owner-recycle-envelope-v1\n"), raw...)), nil
}

// Only this private stage can reach the signer. The unsigned proposal format
// and observer approval cannot be passed as a production signing stage.
func sealOwnerRecycleProductionIntent(ctx context.Context, stage *ownerRecycleProductionStage, hotkey *crv4.Keypair, prepared *crv4.PreparedSubmission, providerEnvelopeHash string) (*OwnerRecycleProductionIntent, error) {
	if stage == nil || stage.authority == nil || prepared == nil || hotkey == nil {
		return nil, errors.New("owner-recycle production seal lacks authenticated preparation")
	}
	approval, err := ownerRecycleProductionApproval(&stage.authority.config)
	if err != nil || stage.authority.config.ownerRecycleProduction.historicalOnly || approval.Approval.ValidatorHotkey != hotkey.PublicKey() || prepared.HotkeyHex != releaseHex32(hotkey.PublicKey()) ||
		prepared.SourceCommitment == nil || prepared.SourceCommitment.Hash != releaseHex32(stage.sourceHash) || prepared.SubnetEpoch != stage.proof.Decision.SubnetEpoch || !slices.Equal(prepared.UIDs, stage.proof.Row.Uids) ||
		!slices.Equal(prepared.Values, stage.proof.Row.Values) {
		return nil, errors.Join(errors.New("owner-recycle production signer, native epoch or prepared row differs"), err)
	}
	if _, err := parseReleaseContentHash(providerEnvelopeHash); err != nil {
		return nil, err
	}
	sidecar := &OwnerRecycleProductionIntent{Proof: stage.proof, Hotkey: releaseHex32(hotkey.PublicKey()), PreparedExtrinsicHash: prepared.ExtrinsicHash, ProviderEnvelopeHash: providerEnvelopeHash}
	digest, err := ownerRecycleProductionSigningDigest(sidecar)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	signature, err := hotkey.Sign(digest[:])
	if err != nil {
		return nil, err
	}
	sidecar.Signature = "0x" + hex.EncodeToString(signature)
	return sidecar, ctx.Err()
}

// Signature checking does not infer native truth. The required private stage
// came from the actual exact-block readers, including archive ObserveSources.
func verifyOwnerRecycleProductionIntent(ctx context.Context, cfg *ReleaseConfig, stage *ownerRecycleProductionStage, intent *SteeringIntent,
	measurement []byte, artifact *ReleaseMeasurementArtifact, provider *VerifiedReleaseMeasurement,
) (*VerifiedReleaseMeasurement, error) {
	if !isOwnerRecycleProductionConfig(cfg) {
		if intent.OwnerRecycle != nil {
			return nil, errors.New("owner-recycle sidecar cannot select production authority for a legacy configuration")
		}
		return provider, nil
	}
	if stage == nil || stage.authority == nil || intent.OwnerRecycle == nil || intent.Prepared == nil || intent.Prepared.SourceCommitment == nil {
		return nil, errors.New("owner-recycle production intent lacks its independently observed signed sidecar")
	}
	approval, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return nil, err
	}
	if stage.authority.config.ownerRecycleProduction == nil ||
		stage.authority.config.ownerRecycleProduction.configHash != cfg.ownerRecycleProduction.configHash ||
		!bytes.Equal(stage.authority.approval, cfg.ownerRecycleProduction.encoded) {
		return nil, errors.New("owner-recycle original production approval cannot be reinterpreted under another configuration")
	}
	sidecar := intent.OwnerRecycle
	encoded, err := ownerRecycleProductionProofBytes(ctx, &sidecar.Proof, cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil || !bytes.Equal(encoded, stage.encoded) || sidecar.Hotkey != releaseHex32(approval.Approval.ValidatorHotkey) ||
		sidecar.Hotkey != intent.Prepared.HotkeyHex || sidecar.PreparedExtrinsicHash != intent.Prepared.ExtrinsicHash ||
		sidecar.ProviderEnvelopeHash != intent.MeasurementEnvelopeHash || sidecar.Proof.ProviderMeasurementHash != ReleaseMeasurementContentHash(measurement) ||
		sidecar.Proof.Decision != releaseMeasurementV2Decision(artifact) ||
		intent.Prepared.SourceCommitment.Hash != releaseHex32(ownerRecycleProductionSourceHash(measurement, encoded)) {
		return nil, errors.Join(errors.New("owner-recycle production intent differs from its exact proof, source or hotkey envelope"), err)
	}
	digest, err := ownerRecycleProductionSigningDigest(sidecar)
	if err != nil {
		return nil, err
	}
	signature, err := parseReleaseMeasurementEnvelopeSignature(sidecar.Signature)
	if err != nil {
		return nil, err
	}
	public, err := (sr25519.Scheme{}).FromPublicKey(approval.Approval.ValidatorHotkey[:])
	if err != nil || !public.Verify(digest[:], signature) {
		return nil, errors.New("owner-recycle production hotkey signature is invalid")
	}
	row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, provider)
	if err != nil || !reflect.DeepEqual(row, sidecar.Proof.Row) || !slices.Equal(intent.Prepared.Values, row.Values) {
		return nil, errors.Join(errors.New("owner-recycle production row differs from complete provider proof replay"), err)
	}
	return ownerRecycleProductionRowDecision(provider, row)
}

// Other provider proof projections remain unchanged; only the explicitly
// approved final native row replaces the parent provider allocation.
func ownerRecycleProductionRowDecision(provider *VerifiedReleaseMeasurement, row OwnerRecycleMeasuredRow) (*VerifiedReleaseMeasurement, error) {
	result := *provider
	result.UIDs = slices.Clone(row.Uids)
	result.Scores = make([]*big.Rat, len(row.Scores))
	for index, score := range row.Scores {
		value, ok := new(big.Rat).SetString(score.Numerator + "/" + score.Denominator)
		if !ok || value.Sign() <= 0 {
			return nil, errors.New("owner-recycle production exact rational is invalid")
		}
		result.Scores[index] = value
	}
	return &result, nil
}
