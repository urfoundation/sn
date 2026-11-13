//go:build linux || darwin

// Closed evidence publication binds the complete real settlement closure to
// immutable member hashes. Every public replica is replayed before any new
// consent is signed. Sending calldata, finality and historical chain authority
// remain the responsibility of the release owner and its independent readers.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

const (
	ValidatorEvidenceCensusV2Schema = "urnetwork-validator-evidence-census-v2"
	ValidatorEvidenceSignedV2Schema = "urnetwork-validator-evidence-signed-v2"
)

// A payload is the existing canonical signed transition, not an evidence
// header. Its signatures predate this census and do not include CensusHash.
// Counts are checked by both complete stream replays, never taken on trust.
type ValidatorEvidenceCensusV2Member struct {
	Domain        protocol.ValidatorEvidenceDomain `json:"domain"`
	NoID          uint64                           `json:"no_id"`
	VPK           [32]byte                         `json:"vpk"`
	PayloadHash   [32]byte                         `json:"payload_hash"`
	PayloadBytes  uint64                           `json:"payload_bytes"`
	RecordCount   uint64                           `json:"record_count"`
	CompleteCount uint64                           `json:"complete_count"`
	FailedCount   uint64                           `json:"failed_count"`
}

// This unsigned body is hashed before the containing headers and their new
// key consents exist. It includes idle and no-payout members without consulting
// an operator's payout root, its cooperation or a later deposit-audit outcome.
type ValidatorEvidenceCensusV2 struct {
	Schema   string                            `json:"schema"`
	Hotkey   [32]byte                          `json:"hotkey"`
	Boundary AttemptBoundary                   `json:"boundary"`
	Members  []ValidatorEvidenceCensusV2Member `json:"members"`
}

// The header's hashes locate the unsigned census and signed terminal payload.
// This separate public object retains both new consents for API/history users;
// the same consents also appear in the eventual contract calldata.
type ValidatorEvidenceSignedV2 struct {
	Schema          string                           `json:"schema"`
	Header          protocol.ValidatorEvidenceHeader `json:"header"`
	VPKSignature    []byte                           `json:"vpk_signature"`
	HotkeySignature []byte                           `json:"hotkey_signature"`
}

// Each invocation owns its fresh scratch names; no replica may reuse any
// participant's scratch. Stream/header/transition/closure limits are the
// already configured limits, not defaults or an implicit capacity increase.
// Keep inputs stable during admission. Publisher callbacks receive owned bytes
// and no signing key, and must honor the replica lifecycle contract.
type ValidatorEvidenceCensusV2Options struct {
	Settlement  AttemptSettlementV2Options
	Window      protocol.ValidatorEvidenceWindow
	PrivateKeys map[uint64]ed25519.PrivateKey
	Hotkey      *crv4.Keypair
	Replicas    []AttemptCutV2Replica
	// Protected uploads preserve each original source activation independently
	// of the destination sessions. When supplied, this complete map replaces
	// Replicas; every member must retain the same public origins.
	ReplicasByOperator map[uint64][]AttemptCutV2Replica
	// The second origin's independent replay owns one fresh scratch for every
	// participant. A single configured origin has no second replay and none.
	SecondReplicaScratchDirectories map[uint64]string
}

// Returned bytes are caller-owned, and no partial member escapes on failure.
// Calldata is authorized content, not a broadcast, receipt or finality proof.
type ValidatorEvidenceMemberV2Publication struct {
	Evidence           ValidatorEvidenceSignedV2
	Payload            []byte
	SignedArtifact     []byte
	SignedArtifactHash [32]byte
	Calldata           []byte
}

// A successful publication observed every stream and metadata object at every
// public origin. It does not promise future availability or establish that
// the independently supplied activation/history was authentic on-chain.
type ValidatorEvidenceCensusV2Publication struct {
	Census     []byte
	CensusHash [32]byte
	Members    []ValidatorEvidenceMemberV2Publication
	Origins    []string
}

// Admit the complete census and own candidate, authority and private keys
// before any external I/O. Real HTTP readers replace candidate/local transports;
// independently supplied policy, historical server keys and bounds remain.
func PublishValidatorEvidenceClosedCensusV2(ctx context.Context, closure *AttemptSettlementClosureV2, options ValidatorEvidenceCensusV2Options) (publication *ValidatorEvidenceCensusV2Publication, resultErr error) {
	return publishValidatorEvidenceClosedCensusV2(ctx, closure, options, nil, ValidatorEvidencePublicationV2ReadOptions{})
}

// A retained locator is discovery only. Rebuild the unsigned census and every
// expected header from the independently admitted closure, replay every public
// stream set, then reuse the exact consent bytes without another signature.
func publishValidatorEvidenceClosedCensusV2(ctx context.Context, closure *AttemptSettlementClosureV2, options ValidatorEvidenceCensusV2Options, retained *ValidatorEvidencePublicationV2Manifest, retainedOptions ValidatorEvidencePublicationV2ReadOptions) (publication *ValidatorEvidenceCensusV2Publication, resultErr error) {
	if ctx == nil {
		return nil, errors.New("validator evidence census context is missing")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			publication = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if retained != nil {
		if err := retained.validate(options.Settlement.MaxClosureBytes, options.Settlement.MaxParticipants); err != nil {
			return nil, err
		}
		owned := *retained
		owned.Origins = slices.Clone(retained.Origins)
		owned.Members = append([]ValidatorEvidencePublicationV2MemberReference(nil), retained.Members...)
		retained = &owned
		retainedOptions.Activations = append([]protocol.ValidatorEvidenceActivation(nil), retainedOptions.Activations...)
	}
	operation, err := admitAttemptSettlementV2(ctx, closure, options.Settlement, true)
	if err != nil {
		return nil, err
	}
	replicaCount := len(options.Replicas)
	if options.ReplicasByOperator != nil {
		if len(options.ReplicasByOperator) != len(operation.closure.Transitions) {
			return nil, errors.New("validator evidence census replica owners are incomplete")
		}
		if len(options.Replicas) != 0 {
			return nil, errors.New("validator evidence census mixes shared and source-owned replicas")
		}
		replicaCount = len(options.ReplicasByOperator[operation.closure.Transitions[0].Identity.NoID])
	}
	if err := validateReleaseEvidenceV2ReplicaCount(replicaCount); err != nil {
		return nil, err
	}
	secondScratch := 0
	if replicaCount == 2 {
		secondScratch = len(operation.closure.Transitions)
	}
	if options.Window.Subject != (protocol.ValidatorEvidenceSubject{}) || len(options.PrivateKeys) != len(operation.closure.Transitions) || len(options.SecondReplicaScratchDirectories) != secondScratch {
		return nil, errors.New("validator evidence census signer, scratch or terminal window differs")
	}
	hotkey, publicKey, err := ownReleaseMeasurementEnvelopeV2Hotkey(options.Hotkey)
	if err != nil {
		return nil, err
	}
	window := options.Window
	result := &ValidatorEvidenceCensusV2Publication{Members: make([]ValidatorEvidenceMemberV2Publication, len(operation.closure.Transitions))}
	census := ValidatorEvidenceCensusV2{Schema: ValidatorEvidenceCensusV2Schema, Hotkey: publicKey, Boundary: operation.closure.Transitions[0].FromBoundary, Members: make([]ValidatorEvidenceCensusV2Member, len(result.Members))}
	keys := make([]ed25519.PrivateKey, len(result.Members))
	publishers := make([]*attemptCutV2Replicas, len(result.Members))
	var paths []string
	var secondOptions AttemptSettlementV2Options
	if replicaCount == 2 {
		secondOptions, err = ownAttemptSettlementV2Options(ctx, operation.options)
		if err != nil {
			return nil, err
		}
	}
	metadataLimit := operation.options.MaxClosureBytes
	for index, transition := range operation.closure.Transitions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		noID := transition.Identity.NoID
		operator := operation.options.Operators[noID]
		if operator.Expected.Activation.Hotkey != publicKey {
			return nil, errors.New("validator evidence census hotkey differs from activation")
		}
		vpk, err := canonicalAttemptHex32("validator evidence census vpk", operator.Expected.Identity.ValidatorVPK, false)
		if err != nil {
			return nil, err
		}
		if err := attemptCutV2PrivateKey(options.PrivateKeys[noID], vpk[:]); err != nil {
			return nil, err
		}
		keys[index] = bytes.Clone(options.PrivateKeys[noID])
		replicas := options.Replicas
		if options.ReplicasByOperator != nil {
			var found bool
			replicas, found = options.ReplicasByOperator[noID]
			if !found {
				return nil, errors.New("validator evidence census omits a source replica owner")
			}
		}
		publishers[index], err = newAttemptCutV2ReplicasWithMetadataLimit(operator.Bounds, replicas, max(attemptStreamV2MetadataBytes(operator.Bounds), operation.options.MaxTransitionBytes))
		if err != nil {
			return nil, err
		}
		origins := publishers[index].origins()
		if index == 0 {
			result.Origins = origins
		} else if !slices.Equal(result.Origins, origins) {
			return nil, errors.New("validator evidence census source replicas have different public origins")
		}
		metadataLimit = min(metadataLimit, publishers[index].readers[0].metadataBytes)
		payload, err := marshalAttemptSettlementV2JSON(ctx, transition, min(operation.options.MaxTransitionBytes, publishers[index].readers[0].metadataBytes), false, true)
		if err != nil {
			return nil, err
		}
		boundaryHash, err := canonicalAttemptHex32("validator evidence terminal boundary", operator.Expected.Boundary.EVMBlockHash, false)
		if err != nil {
			return nil, err
		}
		member := ValidatorEvidenceCensusV2Member{Domain: operator.Expected.Activation.Domain, NoID: noID, VPK: vpk, PayloadHash: sha256.Sum256(payload), PayloadBytes: uint64(len(payload)), RecordCount: transition.Cut.RecordCount, CompleteCount: transition.Cut.CompleteCount, FailedCount: transition.Cut.FailedCount}
		census.Members[index] = member
		header := protocol.ValidatorEvidenceHeader{Domain: member.Domain, Hotkey: publicKey, NoID: noID, Epoch: operation.closure.Epoch, Kind: protocol.ValidatorEvidenceClosedCensus, VPK: vpk, BoundaryBlock: operator.Expected.Boundary.EVMBlock, BoundaryHash: boundaryHash, CensusHash: [32]byte{1}, PayloadHash: member.PayloadHash, PayloadBytes: member.PayloadBytes}
		// The temporary nonzero census value is only structural admission.
		// The actual unsigned census hash replaces it before signing or I/O.
		if err := header.ValidateAt(member.Domain, window); err != nil {
			return nil, err
		}
		result.Members[index] = ValidatorEvidenceMemberV2Publication{Evidence: ValidatorEvidenceSignedV2{Schema: ValidatorEvidenceSignedV2Schema, Header: header}, Payload: payload}
		paths = append(paths, operator.Measurement.Replay.ScratchDirectory)
		operator.Measurement.Replay.ReadMetadata = publishers[index].readers[0].ReadMetadata
		operator.Measurement.Replay.OpenData = publishers[index].readers[0].OpenData
		operation.options.Operators[noID] = operator
		if replicaCount == 2 {
			second := secondOptions.Operators[noID]
			second.Measurement.Replay.ScratchDirectory = options.SecondReplicaScratchDirectories[noID]
			paths = append(paths, second.Measurement.Replay.ScratchDirectory)
			second.Measurement.Replay.ReadMetadata = publishers[index].readers[1].ReadMetadata
			second.Measurement.Replay.OpenData = publishers[index].readers[1].OpenData
			secondOptions.Operators[noID] = second
		}
	}
	if err := validateAttemptSettlementV2Paths(paths); err != nil {
		return nil, err
	}
	result.Census, err = marshalAttemptSettlementV2JSON(ctx, census, metadataLimit, false, true)
	if err != nil {
		return nil, err
	}
	result.CensusHash = sha256.Sum256(result.Census)
	if retained != nil && (retained.Epoch != operation.closure.Epoch || !slices.Equal(retained.Origins, result.Origins) || retained.CensusHash != result.CensusHash || retained.CensusBytes != uint64(len(result.Census)) || len(retained.Members) != len(result.Members)) {
		return nil, errors.New("retained evidence publication differs from independently reconstructed census")
	}
	for index := range result.Members {
		member := &result.Members[index]
		member.Evidence.Header.CensusHash = result.CensusHash
		if retained != nil && retained.Members[index].NoId != member.Evidence.Header.NoID {
			return nil, errors.New("retained evidence publication differs from independently reconstructed member")
		}
		// Reserve the exact fixed-length signature encoding before I/O. The
		// placeholders cannot reach storage, calldata or a successful result.
		sized := member.Evidence
		sized.VPKSignature, sized.HotkeySignature = make([]byte, ed25519.SignatureSize), make([]byte, 64)
		bounds := operation.options.Operators[member.Evidence.Header.NoID].Bounds
		if _, err := marshalAttemptSettlementV2JSON(ctx, sized, min(bounds.MaxHeaderBytes, publishers[index].readers[0].metadataBytes), false, true); err != nil {
			return nil, err
		}
	}
	// One complete independent public replay per origin runs concurrently. A
	// missing, truncated or invalid last object prevents every new consent, and
	// all workers and physical scratch owners join before the operation returns.
	operations := []*attemptSettlementV2Operation{operation}
	if replicaCount == 2 {
		operations = append(operations, &attemptSettlementV2Operation{closure: operation.closure, options: secondOptions})
	}
	ownedCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var joined sync.WaitGroup
	failures := make([]error, len(operations))
	for index, replay := range operations {
		joined.Add(1)
		go func() {
			outcome := errors.New("validator evidence replica replay did not complete")
			defer func() {
				if err := errors.Join(outcome, ownedCtx.Err()); err != nil {
					failures[index] = fmt.Errorf("validator evidence replica %d: %w", index+1, err)
					cancel()
				}
				joined.Done()
			}()
			_, outcome = replay.replay(ownedCtx, nil, false)
		}()
	}
	joined.Wait()
	if err := joinReplicaPublicationErrors(ctx.Err(), failures); err != nil {
		return nil, err
	}
	if retained != nil {
		observed, err := ReadValidatorEvidencePublicationV2(ctx, retained, retainedOptions)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(observed.Census, result.Census) || len(observed.Members) != len(result.Members) {
			return nil, errors.New("retained public census differs from reconstructed terminal closure")
		}
		for index, member := range result.Members {
			other := observed.Members[index]
			if member.Evidence.Header != other.Evidence.Header || !bytes.Equal(member.Payload, other.Payload) {
				return nil, errors.New("retained public consent differs from independently replayed terminal member")
			}
		}
		return observed, nil
	}
	for index := range result.Members {
		member := &result.Members[index]
		if err := publishers[index].writer("metadata")(ctx, attemptHex32(member.Evidence.Header.PayloadHash), member.Payload); err != nil {
			return nil, err
		}
	}
	if err := publishers[0].writer("metadata")(ctx, attemptHex32(result.CensusHash), result.Census); err != nil {
		return nil, err
	}
	for index := range result.Members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		member := &result.Members[index]
		member.Evidence.VPKSignature, err = member.Evidence.Header.SignVPK(keys[index])
		if err != nil {
			return nil, err
		}
		digest, err := member.Evidence.Header.Digest()
		if err != nil {
			return nil, err
		}
		member.Evidence.HotkeySignature, err = hotkey.Sign(digest[:])
		if err != nil {
			return nil, err
		}
		member.Calldata, err = stabi.PackValidatorEvidenceCommitment(member.Evidence.Header.Domain, window, member.Evidence.Header, member.Evidence.VPKSignature, member.Evidence.HotkeySignature)
		if err != nil {
			return nil, err
		}
		bounds := operation.options.Operators[member.Evidence.Header.NoID].Bounds
		member.SignedArtifact, err = marshalAttemptSettlementV2JSON(ctx, member.Evidence, min(bounds.MaxHeaderBytes, publishers[index].readers[0].metadataBytes), false, true)
		if err != nil {
			return nil, err
		}
		member.SignedArtifactHash = sha256.Sum256(member.SignedArtifact)
	}
	for index := range result.Members {
		member := &result.Members[index]
		if err := publishers[index].writer("metadata")(ctx, attemptHex32(member.SignedArtifactHash), member.SignedArtifact); err != nil {
			return nil, err
		}
	}
	return result, nil
}
