//go:build linux || darwin

// Public audit objects retain the validator's exact later source observations.
// Dual consent commits bytes, not universal availability or deposit truth;
// production and final semantic replay retain the independent chain obligation.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

const (
	ValidatorEvidenceDepositAuditV2Schema       = "urnetwork-validator-deposit-audit-v2"
	ValidatorEvidenceDepositAuditV2CensusSchema = "urnetwork-validator-deposit-audit-census-v2"
)

// Counts do not import terminal trail semantics into a later deposit audit.
// All members share the same source epoch and actual observation coordinates.
type ValidatorEvidenceDepositAuditV2Member struct {
	Domain       protocol.ValidatorEvidenceDomain `json:"domain"`
	NoId         uint64                           `json:"no_id"`
	Vpk          [32]byte                         `json:"vpk"`
	PayloadHash  [32]byte                         `json:"payload_hash"`
	PayloadBytes uint64                           `json:"payload_bytes"`
}

// This unsigned census is complete before any containing header is signed.
// Previous intent/artifact hashes are deliberately excluded from its decision.
type ValidatorEvidenceDepositAuditV2Census struct {
	Schema   string                                  `json:"schema"`
	Epoch    uint64                                  `json:"epoch"`
	Subject  protocol.ValidatorEvidenceSubject       `json:"subject"`
	Decision ReleaseMeasurementV2Decision            `json:"decision"`
	Hotkey   [32]byte                                `json:"hotkey"`
	Members  []ValidatorEvidenceDepositAuditV2Member `json:"members"`
}

// The exact signed request/response/error record is present only when an actual
// payout request occurred. No-root/no-payout observations must not invent one.
type ValidatorEvidenceDepositAuditV2Payload struct {
	Schema          string                       `json:"schema"`
	Decision        ReleaseMeasurementV2Decision `json:"decision"`
	Audit           DepositAudit                 `json:"audit"`
	HttpObservation []byte                       `json:"http_observation,omitempty"`
}

// Canonical decision identity is checked against independent activation pins;
// native schedule and coordinator observation truth are not inferred here.
func validateValidatorEvidenceDepositAuditV2Decision(decision ReleaseMeasurementV2Decision, domain protocol.ValidatorEvidenceDomain, window protocol.ValidatorEvidenceWindow) error {
	genesis, err := canonicalAttemptHex32("deposit audit native genesis", decision.GenesisHash, false)
	if err != nil {
		return err
	}
	policy, err := canonicalAttemptHex32("deposit audit policy", decision.PolicyHash, false)
	if err != nil {
		return err
	}
	if _, err := canonicalAttemptHex32("deposit audit native observation", decision.NativeSnapshotHash, false); err != nil {
		return err
	}
	if _, err := canonicalAttemptHex32("deposit audit Evm observation", decision.EVMSnapshotHash, false); err != nil {
		return err
	}
	if decision.DeploymentID == "" || sha256.Sum256([]byte(decision.DeploymentID)) != domain.DeploymentIDHash || decision.ChainID != domain.ChainID || genesis != domain.GenesisHash || decision.Netuid != domain.Netuid || policy != domain.PolicyHash ||
		!common.IsHexAddress(decision.Coordinator) || common.HexToAddress(decision.Coordinator) != common.Address(domain.Coordinator) || !common.IsHexAddress(decision.SettlementVault) || common.HexToAddress(decision.SettlementVault) != common.Address(domain.SettlementVault) ||
		decision.ValidatorID == 0 || decision.NativeSnapshotBlock == 0 || decision.EVMSnapshotBlock < window.EndBlock || decision.EVMSnapshotBlock > window.FinalizedBlock || decision.PreviousArtifactHash != "" || decision.SettlementEpoch != window.Subject.ObservationEpoch || decision.SubnetEpoch != window.Subject.NativeEpoch || window.Subject.ObservationEpoch <= window.Epoch {
		return errors.New("deposit audit decision differs from the exact deployment and later coordinates")
	}
	return nil
}

// Admit bounds and the complete configured activation census before public I/O.
func admitValidatorEvidenceDepositAuditV2(manifest *ValidatorEvidenceDepositAuditV2Manifest, options ValidatorEvidencePublicationV2ReadOptions) (uint64, error) {
	if err := manifest.validate(options.Bounds.MaxClosureBytes, options.Bounds.MaxParticipants); err != nil {
		return 0, err
	}
	if err := options.Bounds.Cut.Records.Validate(); err != nil {
		return 0, err
	}
	if err := options.Bounds.Cut.Proofs.Validate(); err != nil {
		return 0, err
	}
	for _, limit := range []uint64{options.Bounds.Cut.MaxHeaderBytes, options.Bounds.MaxTransitionBytes, options.Bounds.MaxClosureBytes} {
		if err := validateReleaseMeasurementInputV2Limit(limit); err != nil {
			return 0, err
		}
	}
	if len(options.Activations) == 0 || len(manifest.Members) != len(options.Activations) || uint64(len(options.Activations)) > options.Bounds.MaxParticipants || options.Bounds.MaxTransitionBytes > options.Bounds.MaxClosureBytes || manifest.Epoch != options.Window.Epoch || manifest.Subject != options.Window.Subject || manifest.Origins != options.Origins {
		return 0, errors.New("deposit audit locator differs from complete configured authority")
	}
	if options.Bounds.MaxArtifactBytes == 0 || options.Bounds.MaxControlBytes/8 == 0 {
		return 0, errors.New("deposit audit public byte ownership has no finite aggregate allowance")
	}
	limit := max(attemptStreamV2MetadataBytes(options.Bounds.Cut), options.Bounds.MaxTransitionBytes)
	if manifest.CensusBytes > min(limit, options.Bounds.MaxClosureBytes) {
		return 0, errors.New("deposit audit census exceeds public metadata capacity")
	}
	for index, activation := range options.Activations {
		domain, err := activation.EvidenceDomain()
		if err != nil {
			return 0, err
		}
		if activation.Hotkey != options.Activations[0].Hotkey || manifest.Members[index].NoId != activation.NoID || index > 0 && activation.NoID <= options.Activations[index-1].NoID || manifest.Members[index].SignedArtifactBytes > min(limit, options.Bounds.Cut.MaxHeaderBytes) {
			return 0, errors.New("deposit audit activation or member census differs")
		}
		if err := validateValidatorEvidenceDepositAuditV2Decision(manifest.Decision, domain, options.Window); err != nil {
			return 0, err
		}
	}
	return limit, nil
}

// Verify exact public payload/census/consent bytes with the same decoder used
// for private prepared recovery. No signing or network callback can replace it.
func validateValidatorEvidenceDepositAuditV2Publication(ctx context.Context, publication *ValidatorEvidenceCensusV2Publication, manifest *ValidatorEvidenceDepositAuditV2Manifest, options ValidatorEvidencePublicationV2ReadOptions) error {
	limit, err := admitValidatorEvidenceDepositAuditV2(manifest, options)
	if err != nil {
		return err
	}
	if publication == nil || publication.Origins != options.Origins || publication.CensusHash != manifest.CensusHash || sha256.Sum256(publication.Census) != manifest.CensusHash || uint64(len(publication.Census)) != manifest.CensusBytes || len(publication.Members) != len(manifest.Members) {
		return errors.New("deposit audit publication differs from its exact locator")
	}
	var census ValidatorEvidenceDepositAuditV2Census
	if err := decodeValidatorEvidencePublicationV2Json(ctx, publication.Census, min(limit, options.Bounds.MaxClosureBytes), &census); err != nil {
		return err
	}
	if census.Schema != ValidatorEvidenceDepositAuditV2CensusSchema || census.Decision != manifest.Decision || census.Epoch != manifest.Epoch || census.Subject != manifest.Subject || census.Hotkey != options.Activations[0].Hotkey || len(census.Members) != len(manifest.Members) {
		return errors.New("deposit audit shared unsigned census differs")
	}
	for index := range publication.Members {
		member, reference, entry := &publication.Members[index], manifest.Members[index], census.Members[index]
		activation := options.Activations[index]
		domain, err := activation.EvidenceDomain()
		if err != nil {
			return err
		}
		if uint64(len(member.SignedArtifact)) != reference.SignedArtifactBytes || sha256.Sum256(member.SignedArtifact) != reference.SignedArtifactHash || member.SignedArtifactHash != reference.SignedArtifactHash {
			return errors.New("deposit audit consent bytes differ from their exact hash")
		}
		var signed ValidatorEvidenceSignedV2
		if err := decodeValidatorEvidencePublicationV2Json(ctx, member.SignedArtifact, min(limit, options.Bounds.Cut.MaxHeaderBytes), &signed); err != nil {
			return err
		}
		header := signed.Header
		if signed.Schema != ValidatorEvidenceSignedV2Schema || header.Hotkey != activation.Hotkey || header.NoID != activation.NoID || header.VPK != activation.VPK || header.Kind != protocol.ValidatorEvidenceDepositAudit || header.Epoch != manifest.Epoch || header.Subject != manifest.Subject || header.CensusHash != manifest.CensusHash || header.BoundaryBlock != census.Decision.EVMSnapshotBlock || attemptHex32(header.BoundaryHash) != census.Decision.EVMSnapshotHash ||
			entry.Domain != domain || entry.NoId != activation.NoID || entry.Vpk != activation.VPK || entry.PayloadHash != header.PayloadHash || entry.PayloadBytes != header.PayloadBytes || header.PayloadBytes == 0 || header.PayloadBytes > min(limit, options.Bounds.MaxTransitionBytes) || uint64(len(member.Payload)) != header.PayloadBytes || sha256.Sum256(member.Payload) != header.PayloadHash {
			return errors.New("deposit audit source escaped its exact slot, observation or payload")
		}
		calldata, err := stabi.PackValidatorEvidenceCommitment(domain, options.Window, header, signed.VPKSignature, signed.HotkeySignature)
		if err != nil {
			return err
		}
		if member.Evidence.Header != signed.Header || member.Evidence.Schema != signed.Schema || !bytes.Equal(member.Evidence.VPKSignature, signed.VPKSignature) || !bytes.Equal(member.Evidence.HotkeySignature, signed.HotkeySignature) || member.Calldata != nil && !bytes.Equal(member.Calldata, calldata) {
			return errors.New("deposit audit retained signed fields differ from exact consent bytes")
		}
		var payload ValidatorEvidenceDepositAuditV2Payload
		if err := decodeValidatorEvidencePublicationV2Json(ctx, member.Payload, min(limit, options.Bounds.MaxTransitionBytes), &payload); err != nil {
			return err
		}
		if payload.Schema != ValidatorEvidenceDepositAuditV2Schema || payload.Decision != census.Decision || payload.Audit.NoID != activation.NoID || payload.Audit.Epoch != manifest.Subject.ObservationEpoch || payload.Audit.SourceEpoch != manifest.Epoch || payload.Audit.ObservedAtBlock != header.BoundaryBlock {
			return errors.New("deposit audit payload differs from the containing actual observation")
		}
		if len(payload.HttpObservation) == 0 {
			if payload.Audit.HttpObservationHash != "" {
				return errors.New("deposit audit omits its retained actual request")
			}
		} else {
			if ReleaseMeasurementContentHash(payload.HttpObservation) != payload.Audit.HttpObservationHash {
				return errors.New("deposit audit retained request hash differs")
			}
			var observation releaseArtifactHttpObservationV2
			if err := decodeAttemptStreamV2JSON(payload.HttpObservation, options.Bounds.MaxTransitionBytes, &observation); err != nil {
				return err
			}
			expected := releaseArtifactHttpObservationV2{Schema: releaseArtifactHttpObservationSchemaV2, Decision: census.Decision, ValidatorHotkey: releaseHex32(activation.Hotkey), NoId: activation.NoID, SourceEpoch: manifest.Epoch, Origin: observation.Origin}
			if _, err := decodeArtifactHttpObservationV2(ctx, payload.HttpObservation, options.Bounds.MaxTransitionBytes, expected); err != nil {
				return err
			}
		}
		member.Evidence, member.Calldata = signed, calldata
	}
	return ctx.Err()
}

// Both approved public origins must return every exact object. The result is
// signed content ready for an independently bounded relay, not an audit verdict.
func ReadValidatorEvidenceDepositAuditV2(ctx context.Context, suppliedManifest *ValidatorEvidenceDepositAuditV2Manifest, supplied ValidatorEvidencePublicationV2ReadOptions) (publication *ValidatorEvidenceCensusV2Publication, resultErr error) {
	if ctx == nil {
		return nil, errors.New("deposit audit public read context is absent")
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
	options := supplied
	options.Activations = slices.Clone(supplied.Activations)
	if _, err := admitValidatorEvidenceDepositAuditV2(suppliedManifest, options); err != nil {
		return nil, err
	}
	manifest := *suppliedManifest
	manifest.Members = slices.Clone(suppliedManifest.Members)
	readers, err := newReleaseEvidenceV2ReadersWithMetadataLimit(options.Origins, options.Bounds.Cut, max(attemptStreamV2MetadataBytes(options.Bounds.Cut), options.Bounds.MaxTransitionBytes))
	if err != nil {
		return nil, err
	}
	operationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var joined sync.WaitGroup
	var observed [2]*ValidatorEvidenceCensusV2Publication
	var failures [2]error
	for index, reader := range readers {
		joined.Add(1)
		go func() {
			outcome := errors.New("deposit audit origin read did not complete")
			defer func() {
				failures[index] = errors.Join(outcome, operationCtx.Err())
				if failures[index] != nil {
					cancel()
				}
				joined.Done()
			}()
			observed[index], outcome = readValidatorEvidenceDepositAuditV2Origin(operationCtx, reader, &manifest, options)
		}()
	}
	joined.Wait()
	if err := errors.Join(failures[0], failures[1], ctx.Err()); err != nil {
		return nil, err
	}
	first, second := observed[0], observed[1]
	if first == nil || second == nil || !bytes.Equal(first.Census, second.Census) || len(first.Members) != len(second.Members) {
		return nil, errors.New("deposit audit public replicas differ")
	}
	for index, member := range first.Members {
		if !bytes.Equal(member.Payload, second.Members[index].Payload) || !bytes.Equal(member.SignedArtifact, second.Members[index].SignedArtifact) {
			return nil, errors.New("deposit audit member replicas differ")
		}
	}
	return first, nil
}

// Header size is admitted before its payload is fetched, and the final shared
// decoder checks every reference and consent before returning any calldata.
func readValidatorEvidenceDepositAuditV2Origin(ctx context.Context, reader *HTTPAttemptStreamV2Reader, manifest *ValidatorEvidenceDepositAuditV2Manifest, options ValidatorEvidencePublicationV2ReadOptions) (*ValidatorEvidenceCensusV2Publication, error) {
	remaining := min(options.Bounds.MaxArtifactBytes, options.Bounds.MaxControlBytes/8)
	if manifest.CensusBytes > remaining {
		return nil, errors.New("deposit audit shared census exceeds aggregate public bytes")
	}
	remaining -= manifest.CensusBytes
	census, err := reader.ReadMetadata(ctx, attemptHex32(manifest.CensusHash), manifest.CensusBytes)
	if err != nil {
		return nil, err
	}
	result := &ValidatorEvidenceCensusV2Publication{Census: census, CensusHash: manifest.CensusHash, Origins: options.Origins, Members: make([]ValidatorEvidenceMemberV2Publication, len(manifest.Members))}
	for index, reference := range manifest.Members {
		if reference.SignedArtifactBytes > remaining {
			return nil, errors.New("deposit audit consent census exceeds aggregate public bytes")
		}
		remaining -= reference.SignedArtifactBytes
		raw, err := reader.ReadMetadata(ctx, attemptHex32(reference.SignedArtifactHash), reference.SignedArtifactBytes)
		if err != nil {
			return nil, err
		}
		var signed ValidatorEvidenceSignedV2
		if err := decodeValidatorEvidencePublicationV2Json(ctx, raw, options.Bounds.Cut.MaxHeaderBytes, &signed); err != nil {
			return nil, err
		}
		if signed.Header.PayloadBytes == 0 || signed.Header.PayloadBytes > min(options.Bounds.MaxTransitionBytes, reader.metadataBytes) {
			return nil, errors.New("deposit audit public payload exceeds its admitted metadata bound")
		}
		if signed.Header.PayloadBytes > remaining {
			return nil, errors.New("deposit audit payload census exceeds aggregate public bytes")
		}
		remaining -= signed.Header.PayloadBytes
		payload, err := reader.ReadMetadata(ctx, attemptHex32(signed.Header.PayloadHash), signed.Header.PayloadBytes)
		if err != nil {
			return nil, err
		}
		result.Members[index] = ValidatorEvidenceMemberV2Publication{Evidence: signed, Payload: payload, SignedArtifact: raw, SignedArtifactHash: reference.SignedArtifactHash}
	}
	if err := validateValidatorEvidenceDepositAuditV2Publication(ctx, result, manifest, options); err != nil {
		return nil, err
	}
	return result, ctx.Err()
}

// Reconstruct the compact canonical payload without retaining a mutable alias.
func decodeValidatorEvidenceDepositAuditV2Payload(ctx context.Context, raw []byte, maximum uint64) (ValidatorEvidenceDepositAuditV2Payload, error) {
	var payload ValidatorEvidenceDepositAuditV2Payload
	if err := decodeValidatorEvidencePublicationV2Json(ctx, raw, maximum, &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

// Re-encoding is useful to compare an authenticated source projection with the
// exact independently replicated bytes, never to canonicalize hostile input.
func validatorEvidenceDepositAuditV2PayloadBytes(ctx context.Context, payload ValidatorEvidenceDepositAuditV2Payload, maximum uint64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if len(encoded) == 0 || uint64(len(encoded)) > maximum {
		return nil, errors.New("deposit audit payload exceeds its finite public bound")
	}
	return encoded, nil
}
