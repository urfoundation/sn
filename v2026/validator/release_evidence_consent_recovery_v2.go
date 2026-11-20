//go:build linux || darwin

package validator

// An explicit operator recovery can replace only a lost delivery envelope.
// Every signed history byte, census byte and protocol header stays unchanged.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Independent pins identify the only old locator and header this operation
// may repair. Signer handles belong to the ordinary managed release loader.
type ValidatorEvidenceConsentRecoveryV2Options struct {
	StateDir                   string
	OperatorStateDir           string
	ExpectedManifestHash       [32]byte
	ExpectedManifestBytes      uint64
	OriginalConsentUnavailable bool
	ExpectedHeader             protocol.ValidatorEvidenceHeader
	ActivationContext          ReleaseEvidenceV2ActivationContext
	Census                     ValidatorEvidenceCensusV2Options
	Chain                      *ChainClient
}

// Completion describes delivery recovery, never replacement of signed history
// or proof of a broadcast. The original locator remains in the audit preimage.
type ValidatorEvidenceConsentRecoveryV2Result struct {
	OldManifestHash    [32]byte `json:"old_manifest_hash"`
	NewManifestHash    [32]byte `json:"new_manifest_hash"`
	SignedArtifactHash [32]byte `json:"signed_artifact_hash"`
	HeaderDigest       [32]byte `json:"header_digest"`
	Slot               [32]byte `json:"slot"`
	PreparedPath       string   `json:"prepared_path"`
	CompletionPath     string   `json:"completion_path"`
	Completed          bool     `json:"completed"`
}

// A private implementation seam lets synthetic tests vary chain transport
// observations without offering production callers a replacement verifier.
type evidenceConsentRecoveryChain interface {
	authenticate(context.Context, ValidatorEvidenceConsentRecoveryV2Options, bool) (uint64, [32]byte, error)
}

type evidenceConsentRecoveryProductionChain struct{ chain *ChainClient }

// The same real fixed-hash readers used by relay admission authenticate code,
// anchors, activation, epoch geometry, policy era and an entirely empty slot.
func (self evidenceConsentRecoveryProductionChain) authenticate(ctx context.Context, options ValidatorEvidenceConsentRecoveryV2Options, alreadyAdopted bool) (uint64, [32]byte, error) {
	chain, authority, header := self.chain, options.ActivationContext, options.ExpectedHeader
	if chain == nil || chain.client == nil {
		return 0, [32]byte{}, errors.New("consent recovery release chain is unavailable")
	}
	chainId, err := chain.client.ChainID(ctx)
	if err != nil || chainId == nil || chainId.Cmp(new(big.Int).SetUint64(header.Domain.ChainID)) != 0 {
		return 0, [32]byte{}, errors.Join(errors.New("consent recovery fresh EVM chain ID differs"), err)
	}
	var genesis common.Hash
	if err := chain.client.Client().CallContext(ctx, &genesis, "chain_getBlockHash", uint64(0)); err != nil {
		return 0, [32]byte{}, err
	}
	if genesis != common.Hash(header.Domain.GenesisHash) {
		return 0, [32]byte{}, errors.New("consent recovery native genesis differs")
	}
	block, hash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return 0, [32]byte{}, err
	}
	window := options.Census.Window
	window.FinalizedBlock = block
	if err := header.ValidateAt(header.Domain, window); err != nil {
		return 0, [32]byte{}, err
	}
	journal := common.Address(authority.Journal)
	if _, err := chain.ValidatorEvidenceActivationAtHashContext(ctx, journal, authority.RuntimeHash, authority.Activation, block, hash); err != nil {
		return 0, [32]byte{}, err
	}
	if err := chain.validateValidatorEvidencePolicyEraAtHashV2(ctx, header, block, hash); err != nil {
		return 0, [32]byte{}, err
	}
	expected := ValidatorEvidenceTransactionV2Expected{Journal: journal, RuntimeHash: authority.RuntimeHash, Activation: authority.Activation, Window: window, Evidence: ValidatorEvidenceSignedV2{Schema: ValidatorEvidenceSignedV2Schema, Header: header}}
	if err := chain.validateValidatorEvidenceEpochGeometryV2Context(ctx, expected, block, hash); err != nil {
		return 0, [32]byte{}, err
	}
	_, err = chain.ValidatorEvidenceAtHashContext(ctx, journal, authority.RuntimeHash, authority.Activation, header, window, block, hash)
	if err == nil && alreadyAdopted {
		return block, hash, ctx.Err()
	}
	if err != ErrValidatorEvidenceAbsent {
		return 0, [32]byte{}, errors.Join(errors.New("consent recovery requires an authenticated empty finalized slot"), err)
	}
	return block, hash, ctx.Err()
}

// Production accepts only the real release chain client. No ordinary retry or
// immutable publication writer gains replacement behavior from this entrypoint.
func RepairValidatorEvidenceConsentV2(ctx context.Context, closure *AttemptSettlementClosureV2, options ValidatorEvidenceConsentRecoveryV2Options) (*ValidatorEvidenceConsentRecoveryV2Result, error) {
	return repairValidatorEvidenceConsentV2(ctx, closure, options, evidenceConsentRecoveryProductionChain{chain: options.Chain}, evidenceConsentRecoveryHooks{})
}

// Inspects only chain eligibility using the real release readers. The supplied
// original header, activation, journal runtime and epoch window are public.
// This does not sign, replay history, acquire a writer or alter any locator.
func InspectValidatorEvidenceConsentRecoveryChainV2(ctx context.Context, options ValidatorEvidenceConsentRecoveryV2Options) (uint64, [32]byte, error) {
	if ctx == nil {
		return 0, [32]byte{}, errors.New("consent recovery chain inspection context is absent")
	}
	domain, err := options.ActivationContext.Activation.EvidenceDomain()
	header := options.ExpectedHeader
	if err != nil || domain != header.Domain || options.ActivationContext.Activation.NoID != header.NoID || options.ActivationContext.Activation.Hotkey != header.Hotkey || options.ActivationContext.Activation.VPK != header.VPK {
		return 0, [32]byte{}, errors.Join(errors.New("chain inspection original activation differs from pinned header"), err)
	}
	return (evidenceConsentRecoveryProductionChain{chain: options.Chain}).authenticate(ctx, options, false)
}

// Rebuilds the one independently owned member through the production admission
// and complete policy-aware public replay before returning an unsigned header.
func prepareEvidenceConsentRecoveryV2(ctx context.Context, closure *AttemptSettlementClosureV2, options ValidatorEvidenceConsentRecoveryV2Options) (*ValidatorEvidenceCensusV2Publication, *attemptCutV2Replicas, error) {
	if ctx == nil || options.ExpectedManifestHash == ([32]byte{}) || closure == nil || len(closure.Transitions) != 1 || len(options.Census.Settlement.Operators) != 1 || len(options.Census.PrivateKeys) != 1 || options.Census.Hotkey == nil {
		return nil, nil, errors.New("consent recovery requires one complete independently pinned member")
	}
	operation, err := admitAttemptSettlementV2(ctx, closure, options.Census.Settlement, true)
	if err != nil {
		return nil, nil, err
	}
	transition := operation.closure.Transitions[0]
	noId := transition.Identity.NoID
	operator := operation.options.Operators[noId]
	header := options.ExpectedHeader
	domain, err := options.ActivationContext.Activation.EvidenceDomain()
	if err != nil || domain != operator.Expected.Activation.Domain || domain != header.Domain || options.ActivationContext.Activation.NoID != noId || options.ActivationContext.Activation.Hotkey != header.Hotkey || options.ActivationContext.Activation.VPK != header.VPK {
		return nil, nil, errors.New("consent recovery activation differs from original member")
	}
	if err := header.ValidateAt(domain, options.Census.Window); err != nil {
		return nil, nil, err
	}
	if header.Kind != protocol.ValidatorEvidenceClosedCensus || header.Subject != (protocol.ValidatorEvidenceSubject{}) || header.Epoch != closure.Epoch || header.NoID != noId || header.Hotkey != options.Census.Hotkey.PublicKey() {
		return nil, nil, errors.New("consent recovery cannot alter the original header or logical slot")
	}
	if err := attemptCutV2PrivateKey(options.Census.PrivateKeys[noId], header.VPK[:]); err != nil {
		return nil, nil, err
	}
	replicas := options.Census.Replicas
	if options.Census.ReplicasByOperator != nil {
		if len(replicas) != 0 || len(options.Census.ReplicasByOperator) != 1 {
			return nil, nil, errors.New("consent recovery replica ownership differs")
		}
		replicas = options.Census.ReplicasByOperator[noId]
	}
	if len(replicas) != 1 || len(options.Census.SecondReplicaScratchDirectories) != 0 {
		return nil, nil, errors.New("single-member consent recovery requires its one original replica")
	}
	publisher, err := newAttemptCutV2ReplicasWithMetadataLimit(operator.Bounds, replicas, max(attemptStreamV2MetadataBytes(operator.Bounds), operation.options.MaxTransitionBytes))
	if err != nil {
		return nil, nil, err
	}
	payload, err := marshalAttemptSettlementV2JSON(ctx, transition, operation.options.MaxTransitionBytes, false, true)
	if err != nil {
		return nil, nil, err
	}
	vpk, err := canonicalAttemptHex32("consent recovery validator", transition.Identity.ValidatorVPK, false)
	if err != nil {
		return nil, nil, err
	}
	census := ValidatorEvidenceCensusV2{Schema: ValidatorEvidenceCensusV2Schema, Hotkey: header.Hotkey, Boundary: transition.FromBoundary, Members: []ValidatorEvidenceCensusV2Member{{Domain: operator.Expected.Activation.Domain, NoID: noId, VPK: vpk, PayloadHash: sha256.Sum256(payload), PayloadBytes: uint64(len(payload)), RecordCount: transition.Cut.RecordCount, CompleteCount: transition.Cut.CompleteCount, FailedCount: transition.Cut.FailedCount}}}
	censusRaw, err := marshalAttemptSettlementV2JSON(ctx, census, operation.options.MaxClosureBytes, false, true)
	if err != nil {
		return nil, nil, err
	}
	boundaryHash, err := canonicalAttemptHex32("consent recovery boundary", operator.Expected.Boundary.EVMBlockHash, false)
	if err != nil {
		return nil, nil, err
	}
	reconstructed := protocol.ValidatorEvidenceHeader{Domain: census.Members[0].Domain, Hotkey: census.Hotkey, NoID: noId, Epoch: closure.Epoch, Kind: protocol.ValidatorEvidenceClosedCensus, VPK: vpk, BoundaryBlock: operator.Expected.Boundary.EVMBlock, BoundaryHash: boundaryHash, CensusHash: sha256.Sum256(censusRaw), PayloadHash: sha256.Sum256(payload), PayloadBytes: uint64(len(payload))}
	if reconstructed != header {
		return nil, nil, errors.New("replayed original closure reconstructs a different consent header")
	}
	operator.Measurement.Replay.ReadMetadata = publisher.readers[0].ReadMetadata
	operator.Measurement.Replay.OpenData = publisher.readers[0].OpenData
	operation.options.Operators[noId] = operator
	if _, err := operation.replay(ctx, nil, false); err != nil {
		return nil, nil, err
	}
	for _, object := range []struct {
		hash [32]byte
		raw  []byte
	}{{hash: header.CensusHash, raw: censusRaw}, {hash: header.PayloadHash, raw: payload}} {
		observed, err := publisher.readers[0].ReadMetadata(ctx, attemptHex32(object.hash), uint64(len(object.raw)))
		if err != nil || !bytes.Equal(observed, object.raw) {
			return nil, nil, errors.Join(errors.New("consent recovery requires the exact existing public census and payload"), err)
		}
	}
	result := &ValidatorEvidenceCensusV2Publication{Census: censusRaw, CensusHash: header.CensusHash, Origins: slices.Clone(publisher.origins()), Members: []ValidatorEvidenceMemberV2Publication{{Evidence: ValidatorEvidenceSignedV2{Schema: ValidatorEvidenceSignedV2Schema, Header: header}, Payload: payload}}}
	return result, publisher, nil
}

// Both managed owners consent only to the fully replayed identical header.
func signEvidenceConsentRecoveryV2(ctx context.Context, publication *ValidatorEvidenceCensusV2Publication, options ValidatorEvidenceConsentRecoveryV2Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	member := &publication.Members[0]
	var err error
	member.Evidence.VPKSignature, err = member.Evidence.Header.SignVPK(options.Census.PrivateKeys[member.Evidence.Header.NoID])
	if err != nil {
		return err
	}
	digest, err := member.Evidence.Header.Digest()
	if err != nil {
		return err
	}
	member.Evidence.HotkeySignature, err = options.Census.Hotkey.Sign(digest[:])
	if err != nil {
		return err
	}
	member.Calldata, err = stabi.PackValidatorEvidenceCommitment(member.Evidence.Header.Domain, options.Census.Window, member.Evidence.Header, member.Evidence.VPKSignature, member.Evidence.HotkeySignature)
	if err != nil {
		return err
	}
	member.SignedArtifact, err = marshalAttemptSettlementV2JSON(ctx, member.Evidence, options.Census.Settlement.Operators[member.Evidence.Header.NoID].Bounds.MaxHeaderBytes, false, true)
	if err != nil {
		return err
	}
	member.SignedArtifactHash = sha256.Sum256(member.SignedArtifact)
	return ctx.Err()
}

// The durable prepared record can be reauthenticated without either signer.
func validatePreparedEvidenceConsentRecoveryV2(raw []byte, header protocol.ValidatorEvidenceHeader, options ValidatorEvidenceConsentRecoveryV2Options) (ValidatorEvidenceSignedV2, error) {
	var signed ValidatorEvidenceSignedV2
	if err := json.Unmarshal(raw, &signed); err != nil {
		return signed, err
	}
	canonical, err := marshalAttemptSettlementV2JSON(context.Background(), signed, options.Census.Settlement.Operators[header.NoID].Bounds.MaxHeaderBytes, false, true)
	if err != nil || !bytes.Equal(canonical, raw) || signed.Schema != ValidatorEvidenceSignedV2Schema || signed.Header != header {
		return signed, errors.Join(errors.New("prepared consent bytes differ from exact original header"), err)
	}
	if err := signed.Header.Verify(header.Domain, options.Census.Window, signed.VPKSignature, signed.HotkeySignature); err != nil {
		return signed, fmt.Errorf("prepared original-header consent: %w", err)
	}
	return signed, nil
}

// Returns only already-durable PUBLIC consent bytes to a separately reviewed
// managed publisher. No signer, API session or chain client is loaded here.
// The caller's independent old-locator/header pins remain mandatory.
func ReadPreparedValidatorEvidenceConsentRecoveryV2(ctx context.Context, options ValidatorEvidenceConsentRecoveryV2Options) (result []byte, resultErr error) {
	if ctx == nil || options.ExpectedManifestHash == ([32]byte{}) || options.ExpectedManifestBytes == 0 || options.Census.Settlement.MaxClosureBytes == 0 || options.Census.Settlement.MaxClosureBytes > (^uint64(0))/4 {
		return nil, errors.New("public prepared-consent projection pins or limits are incomplete")
	}
	owner, err := openEvidenceConsentRecoveryOwner(ctx, options, false)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, owner.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	base := fmt.Sprintf("epoch-%020d-%x", options.ExpectedHeader.Epoch, options.ExpectedManifestHash)
	maximum := 4 * options.Census.Settlement.MaxClosureBytes
	raw, exists, err := readEvidenceConsentRecoveryFile(ctx, owner.audit, base+".prepared.json", maximum)
	if err != nil || !exists {
		return nil, errors.Join(errors.New("original-header consent has not been durably prepared"), err)
	}
	var prepared evidenceConsentRecoveryPrepared
	if err := json.Unmarshal(raw, &prepared); err != nil {
		return nil, err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, prepared, maximum, true, true)
	if err != nil || !bytes.Equal(canonical, raw) || prepared.Schema != "urnetwork-validator-consent-recovery-prepared-v1" || sha256.Sum256(prepared.OldManifest) != options.ExpectedManifestHash || uint64(len(prepared.OldManifest)) != options.ExpectedManifestBytes || prepared.ExchangeName != base+".displaced.json" || prepared.FinalizedBlock == 0 || prepared.FinalizedHash == ([32]byte{}) {
		return nil, errors.Join(errors.New("prepared public-consent projection ownership differs"), err)
	}
	var signed ValidatorEvidenceSignedV2
	if err := json.Unmarshal(prepared.SignedArtifact, &signed); err != nil {
		return nil, err
	}
	canonical, err = marshalAttemptSettlementV2JSON(ctx, signed, options.Census.Settlement.MaxClosureBytes, false, true)
	if err != nil || !bytes.Equal(canonical, prepared.SignedArtifact) || signed.Schema != ValidatorEvidenceSignedV2Schema || signed.Header != options.ExpectedHeader || !signed.Header.VerifyVPK(signed.VPKSignature) || !signed.Header.VerifyHotkey(signed.HotkeySignature) {
		return nil, errors.Join(errors.New("prepared public-consent projection changed header or original signatures"), err)
	}
	var old ValidatorEvidencePublicationV2Manifest
	if err := json.Unmarshal(prepared.OldManifest, &old); err != nil {
		return nil, err
	}
	if err := old.validate(options.Census.Settlement.MaxClosureBytes, 1); err != nil {
		return nil, err
	}
	if old.Epoch != signed.Header.Epoch || old.CensusHash != signed.Header.CensusHash || old.Members[0].NoId != signed.Header.NoID {
		return nil, errors.New("prepared public-consent projection escaped original locator")
	}
	updated := old
	updated.Members = []ValidatorEvidencePublicationV2MemberReference{{NoId: signed.Header.NoID, SignedArtifactHash: sha256.Sum256(prepared.SignedArtifact), SignedArtifactBytes: uint64(len(prepared.SignedArtifact))}}
	canonical, err = marshalAttemptSettlementV2JSON(ctx, updated, options.Census.Settlement.MaxClosureBytes, true, true)
	if err != nil || !bytes.Equal(canonical, prepared.NewManifest) {
		return nil, errors.Join(errors.New("prepared public-consent projection changed another locator field"), err)
	}
	return bytes.Clone(prepared.SignedArtifact), nil
}
