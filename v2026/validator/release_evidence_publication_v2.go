//go:build linux || darwin

// The permissionless relay discovers a local locator, then obtains the actual
// dual-signed bytes from both approved public origins. A locator is not proof
// authority; the sender still authenticates chain state and exact inclusion.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Activations and epoch geometry are independently supplied deployment/chain
// inputs. They must not be constructed by copying the discovered artifacts.
type ValidatorEvidencePublicationV2ReadOptions struct {
	Activations []protocol.ValidatorEvidenceActivation
	Window      protocol.ValidatorEvidenceWindow
	Origins     [2]string
	Bounds      ReleaseEvidenceV2Bounds
}

// The real publisher uses compact JSON with one final newline. Exact
// re-encoding rejects duplicate fields, aliases and trailing documents even
// where encoding/json's ordinary decoder would otherwise accept them.
func decodeValidatorEvidencePublicationV2Json(ctx context.Context, raw []byte, limit uint64, value any) error {
	if ctx == nil {
		return errors.New("evidence publication decode context is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if limit == 0 || len(raw) == 0 || uint64(len(raw)) > limit {
		return errors.New("evidence publication exceeds its byte bound")
	}
	if err := decodeAttemptStreamV2JSON(raw, limit, value); err != nil {
		return err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(raw, canonical) {
		return errors.New("evidence publication JSON differs from the actual canonical publisher bytes")
	}
	return ctx.Err()
}

// Reads both entire origin views concurrently and joins them before returning
// any calldata. This verifies transport, complete membership and both consents,
// not the truth of trails (the production publisher and final replay own that).
func ReadValidatorEvidencePublicationV2(ctx context.Context, suppliedManifest *ValidatorEvidencePublicationV2Manifest, supplied ValidatorEvidencePublicationV2ReadOptions) (publication *ValidatorEvidenceCensusV2Publication, resultErr error) {
	if ctx == nil {
		return nil, errors.New("evidence publication read context is absent")
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
	if err := options.Bounds.Cut.Records.Validate(); err != nil {
		return nil, err
	}
	if err := options.Bounds.Cut.Proofs.Validate(); err != nil {
		return nil, err
	}
	if err := validateReleaseMeasurementInputV2Limit(options.Bounds.Cut.MaxHeaderBytes); err != nil {
		return nil, err
	}
	for _, limit := range []uint64{options.Bounds.MaxClosureBytes, options.Bounds.MaxTransitionBytes} {
		if err := validateReleaseMeasurementInputV2Limit(limit); err != nil {
			return nil, err
		}
	}
	if options.Bounds.MaxParticipants == 0 || uint64(len(options.Activations)) > options.Bounds.MaxParticipants || options.Bounds.MaxTransitionBytes > options.Bounds.MaxClosureBytes {
		return nil, errors.New("evidence publication read bounds are incomplete")
	}
	if len(options.Activations) == 0 || options.Window.Subject != (protocol.ValidatorEvidenceSubject{}) {
		return nil, errors.New("evidence publication has no complete closed-epoch authority")
	}
	for index, activation := range options.Activations {
		if _, err := activation.EvidenceDomain(); err != nil {
			return nil, err
		}
		if activation.Hotkey != options.Activations[0].Hotkey || index > 0 && activation.NoID <= options.Activations[index-1].NoID {
			return nil, errors.New("evidence publication activation census differs from one sorted validator")
		}
	}
	if err := suppliedManifest.validate(options.Bounds.MaxClosureBytes, options.Bounds.MaxParticipants); err != nil {
		return nil, err
	}
	ownedManifest := *suppliedManifest
	ownedManifest.Members = slices.Clone(suppliedManifest.Members)
	manifest := &ownedManifest
	if manifest.Epoch != options.Window.Epoch || manifest.Origins != options.Origins || len(manifest.Members) != len(options.Activations) {
		return nil, errors.New("evidence publication locator differs from configured epoch, origins or complete membership")
	}
	metadataLimit := max(options.Bounds.Cut.Records.MaxManifestBytes, options.Bounds.Cut.Records.MaxPageBytes, options.Bounds.Cut.Proofs.MaxManifestBytes, options.Bounds.Cut.Proofs.MaxPageBytes)
	if manifest.CensusBytes > metadataLimit {
		return nil, errors.New("evidence publication census exceeds the approved public metadata limit")
	}
	readers, err := newReleaseEvidenceV2StartupReaders(options.Origins, options.Bounds.Cut)
	if err != nil {
		return nil, err
	}
	for index, member := range manifest.Members {
		if member.NoId != options.Activations[index].NoID || member.SignedArtifactBytes > min(options.Bounds.Cut.MaxHeaderBytes, metadataLimit) {
			return nil, errors.New("evidence publication member locator differs from approved operator or capacity")
		}
	}
	operationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var joined sync.WaitGroup
	var observed [2]*ValidatorEvidenceCensusV2Publication
	var failures [2]error
	for index, reader := range readers {
		joined.Add(1)
		go func() {
			outcome := errors.New("evidence publication origin reader did not complete")
			defer func() {
				failures[index] = errors.Join(outcome, operationCtx.Err())
				if failures[index] != nil {
					cancel()
				}
				joined.Done()
			}()
			observed[index], outcome = readValidatorEvidencePublicationV2Origin(operationCtx, reader, manifest, options, metadataLimit)
		}()
	}
	joined.Wait()
	if err := errors.Join(failures[0], failures[1], ctx.Err()); err != nil {
		return nil, err
	}
	first, second := observed[0], observed[1]
	if first == nil || second == nil || !bytes.Equal(first.Census, second.Census) || len(first.Members) != len(second.Members) {
		return nil, errors.New("evidence publication public census replicas differ")
	}
	for index, member := range first.Members {
		other := second.Members[index]
		if !bytes.Equal(member.Payload, other.Payload) || !bytes.Equal(member.SignedArtifact, other.SignedArtifact) || !bytes.Equal(member.Calldata, other.Calldata) {
			return nil, errors.New("evidence publication public member replicas differ")
		}
	}
	return first, nil
}

// One bounded view includes the shared unsigned census, every public consent
// object and every referenced terminal payload. Missing final members fail the
// whole result; payload hashes alone are not a substitute for fetching bytes.
func readValidatorEvidencePublicationV2Origin(ctx context.Context, reader *HTTPAttemptStreamV2Reader, manifest *ValidatorEvidencePublicationV2Manifest, options ValidatorEvidencePublicationV2ReadOptions, metadataLimit uint64) (*ValidatorEvidenceCensusV2Publication, error) {
	censusBytes, err := reader.ReadMetadata(ctx, fmt.Sprintf("0x%x", manifest.CensusHash), manifest.CensusBytes)
	if err != nil {
		return nil, err
	}
	if sha256.Sum256(censusBytes) != manifest.CensusHash {
		return nil, errors.New("evidence publication public census hash differs")
	}
	var census ValidatorEvidenceCensusV2
	if err := decodeValidatorEvidencePublicationV2Json(ctx, censusBytes, min(options.Bounds.MaxClosureBytes, metadataLimit), &census); err != nil {
		return nil, err
	}
	if census.Schema != ValidatorEvidenceCensusV2Schema || census.Hotkey != options.Activations[0].Hotkey || census.Boundary.SettlementEpoch != manifest.Epoch || len(census.Members) != len(manifest.Members) {
		return nil, errors.New("evidence publication public census differs from its configured closed epoch")
	}
	result := &ValidatorEvidenceCensusV2Publication{Census: censusBytes, CensusHash: manifest.CensusHash, Origins: options.Origins, Members: make([]ValidatorEvidenceMemberV2Publication, len(manifest.Members))}
	for index, reference := range manifest.Members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		activation := options.Activations[index]
		domain, err := activation.EvidenceDomain()
		if err != nil {
			return nil, err
		}
		raw, err := reader.ReadMetadata(ctx, fmt.Sprintf("0x%x", reference.SignedArtifactHash), reference.SignedArtifactBytes)
		if err != nil {
			return nil, err
		}
		if sha256.Sum256(raw) != reference.SignedArtifactHash {
			return nil, errors.New("evidence publication public consent hash differs")
		}
		var signed ValidatorEvidenceSignedV2
		if err := decodeValidatorEvidencePublicationV2Json(ctx, raw, min(options.Bounds.Cut.MaxHeaderBytes, metadataLimit), &signed); err != nil {
			return nil, err
		}
		header, member := signed.Header, census.Members[index]
		if signed.Schema != ValidatorEvidenceSignedV2Schema || header.Hotkey != activation.Hotkey || header.NoID != activation.NoID || header.VPK != activation.VPK || header.Epoch != manifest.Epoch || header.Kind != protocol.ValidatorEvidenceClosedCensus || header.Subject != (protocol.ValidatorEvidenceSubject{}) || header.CensusHash != manifest.CensusHash || header.BoundaryBlock != census.Boundary.EVMBlock || fmt.Sprintf("0x%x", header.BoundaryHash) != census.Boundary.EVMBlockHash ||
			member.Domain != domain || member.NoID != activation.NoID || member.VPK != activation.VPK || member.PayloadHash != header.PayloadHash || member.PayloadBytes != header.PayloadBytes || member.CompleteCount > member.RecordCount || member.FailedCount > member.RecordCount-member.CompleteCount {
			return nil, errors.New("evidence publication member escaped its exact source, census or closed slot")
		}
		calldata, err := stabi.PackValidatorEvidenceCommitment(domain, options.Window, header, signed.VPKSignature, signed.HotkeySignature)
		if err != nil {
			return nil, err
		}
		if header.PayloadBytes == 0 || header.PayloadBytes > min(options.Bounds.MaxTransitionBytes, metadataLimit) {
			return nil, errors.New("evidence publication terminal payload exceeds its approved byte bound")
		}
		payload, err := reader.ReadMetadata(ctx, fmt.Sprintf("0x%x", header.PayloadHash), header.PayloadBytes)
		if err != nil {
			return nil, err
		}
		if sha256.Sum256(payload) != header.PayloadHash {
			return nil, errors.New("evidence publication terminal payload hash differs")
		}
		result.Members[index] = ValidatorEvidenceMemberV2Publication{Evidence: signed, Payload: payload, SignedArtifact: raw, SignedArtifactHash: reference.SignedArtifactHash, Calldata: calldata}
	}
	return result, ctx.Err()
}
