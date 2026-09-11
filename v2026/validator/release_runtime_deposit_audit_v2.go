//go:build linux || darwin

// Publication begins only for a durable native measurement. The first actual
// observation of a fixed epoch/native subject owns its signatures; interruption
// replays that retained observation and resumes existing protected uploads.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"path/filepath"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"golang.org/x/sys/unix"
)

const releaseDepositAuditPreparedV2Schema = "urnetwork-validator-deposit-audit-prepared-v2"

// This private all-member bundle is synced and closed before the first public
// write. Randomized hotkey signatures therefore survive partial publication.
type releaseDepositAuditPreparedV2 struct {
	Schema      string                                   `json:"schema"`
	Manifest    *ValidatorEvidenceDepositAuditV2Manifest `json:"manifest"`
	Publication *ValidatorEvidenceCensusV2Publication    `json:"publication"`
}

// References are derived from complete owned bytes, never copied from a
// previous manifest or an upload response claiming successful publication.
func validatorEvidenceDepositAuditV2Manifest(publication *ValidatorEvidenceCensusV2Publication, decision ReleaseMeasurementV2Decision, window protocol.ValidatorEvidenceWindow) (*ValidatorEvidenceDepositAuditV2Manifest, error) {
	if publication == nil || len(publication.Members) == 0 {
		return nil, errors.New("deposit audit publication has no complete source census")
	}
	manifest := &ValidatorEvidenceDepositAuditV2Manifest{Schema: ValidatorEvidenceDepositAuditV2ManifestSchema, Kind: protocol.ValidatorEvidenceDepositAudit, Epoch: window.Epoch, Subject: window.Subject, Decision: decision,
		Origins: publication.Origins, CensusHash: sha256.Sum256(publication.Census), CensusBytes: uint64(len(publication.Census)), Members: make([]ValidatorEvidencePublicationV2MemberReference, len(publication.Members))}
	for index, member := range publication.Members {
		manifest.Members[index] = ValidatorEvidencePublicationV2MemberReference{NoId: member.Evidence.Header.NoID, SignedArtifactHash: sha256.Sum256(member.SignedArtifact), SignedArtifactBytes: uint64(len(member.SignedArtifact))}
	}
	return manifest, nil
}

// Build only from the independently re-observed source projection. All sizes,
// census members and private signers are admitted before new public consent.
func (self *releaseRuntimeV2) buildDepositAuditPublicationV2(ctx context.Context, payloads []ValidatorEvidenceDepositAuditV2Payload, options ValidatorEvidencePublicationV2ReadOptions) (*releaseDepositAuditPreparedV2, error) {
	if len(payloads) == 0 || len(payloads) != len(options.Activations) {
		return nil, errors.New("deposit audit canonical source census is incomplete")
	}
	limit := options.Bounds.MaxTransitionBytes
	publication := &ValidatorEvidenceCensusV2Publication{Origins: options.Origins, Members: make([]ValidatorEvidenceMemberV2Publication, len(payloads))}
	census := ValidatorEvidenceDepositAuditV2Census{Schema: ValidatorEvidenceDepositAuditV2CensusSchema, Epoch: options.Window.Epoch, Subject: options.Window.Subject, Decision: payloads[0].Decision, Hotkey: self.hotkey.PublicKey(), Members: make([]ValidatorEvidenceDepositAuditV2Member, len(payloads))}
	keys := make([]ed25519.PrivateKey, len(payloads))
	for index, payload := range payloads {
		activation := options.Activations[index]
		domain, err := activation.EvidenceDomain()
		if err != nil {
			return nil, err
		}
		if payload.Decision != census.Decision || payload.Audit.NoID != activation.NoID || activation.Hotkey != census.Hotkey || self.sources[activation.NoID] == nil {
			return nil, errors.New("deposit audit signing source differs from original activation")
		}
		keys[index] = bytes.Clone(self.sources[activation.NoID].privateKey[:])
		if err := attemptCutV2PrivateKey(keys[index], activation.VPK[:]); err != nil {
			return nil, err
		}
		encoded, err := validatorEvidenceDepositAuditV2PayloadBytes(ctx, payload, limit)
		if err != nil {
			return nil, err
		}
		entry := ValidatorEvidenceDepositAuditV2Member{Domain: domain, NoId: activation.NoID, Vpk: activation.VPK, PayloadHash: sha256.Sum256(encoded), PayloadBytes: uint64(len(encoded))}
		census.Members[index] = entry
		boundaryHash, err := canonicalAttemptHex32("deposit audit observed Evm boundary", census.Decision.EVMSnapshotHash, false)
		if err != nil {
			return nil, err
		}
		header := protocol.ValidatorEvidenceHeader{Domain: domain, Hotkey: census.Hotkey, NoID: activation.NoID, VPK: activation.VPK, Epoch: options.Window.Epoch, Kind: protocol.ValidatorEvidenceDepositAudit, Subject: options.Window.Subject,
			BoundaryBlock: census.Decision.EVMSnapshotBlock, BoundaryHash: boundaryHash, PayloadHash: entry.PayloadHash, PayloadBytes: entry.PayloadBytes, CensusHash: [32]byte{1}}
		if err := header.ValidateAt(domain, options.Window); err != nil {
			return nil, err
		}
		publication.Members[index] = ValidatorEvidenceMemberV2Publication{Evidence: ValidatorEvidenceSignedV2{Schema: ValidatorEvidenceSignedV2Schema, Header: header}, Payload: encoded}
	}
	var err error
	publication.Census, err = marshalAttemptSettlementV2JSON(ctx, census, min(limit, options.Bounds.MaxClosureBytes), false, true)
	if err != nil {
		return nil, err
	}
	publication.CensusHash = sha256.Sum256(publication.Census)
	for index := range publication.Members {
		member := &publication.Members[index]
		member.Evidence.Header.CensusHash = publication.CensusHash
		member.Evidence.VPKSignature, member.Evidence.HotkeySignature = make([]byte, 64), make([]byte, 64)
		if _, err := marshalAttemptSettlementV2JSON(ctx, member.Evidence, min(limit, options.Bounds.Cut.MaxHeaderBytes), false, true); err != nil {
			return nil, err
		}
	}
	for index := range publication.Members {
		member := &publication.Members[index]
		member.Evidence.VPKSignature, err = member.Evidence.Header.SignVPK(keys[index])
		if err != nil {
			return nil, err
		}
		digest, err := member.Evidence.Header.Digest()
		if err != nil {
			return nil, err
		}
		member.Evidence.HotkeySignature, err = self.hotkey.Sign(digest[:])
		if err != nil {
			return nil, err
		}
		member.Calldata, err = stabi.PackValidatorEvidenceCommitment(member.Evidence.Header.Domain, options.Window, member.Evidence.Header, member.Evidence.VPKSignature, member.Evidence.HotkeySignature)
		if err != nil {
			return nil, err
		}
		member.SignedArtifact, err = marshalAttemptSettlementV2JSON(ctx, member.Evidence, min(limit, options.Bounds.Cut.MaxHeaderBytes), false, true)
		if err != nil {
			return nil, err
		}
		member.SignedArtifactHash = sha256.Sum256(member.SignedArtifact)
	}
	manifest, err := validatorEvidenceDepositAuditV2Manifest(publication, census.Decision, options.Window)
	if err != nil {
		return nil, err
	}
	if err := validateValidatorEvidenceDepositAuditV2Publication(ctx, publication, manifest, options); err != nil {
		return nil, err
	}
	return &releaseDepositAuditPreparedV2{Schema: releaseDepositAuditPreparedV2Schema, Manifest: manifest, Publication: publication}, nil
}

// The existing exact subject, when present, is replayed at its original pins.
// A failed native attempt cannot manufacture a second observation by changing
// its intent lineage or today's chain head within that same immutable subject.
func (self *releaseRuntimeV2) prepareDepositAuditPublicationV2(ctx context.Context, decision ReleaseMeasurementV2Decision, audits []DepositAudit, options ValidatorEvidencePublicationV2ReadOptions, hooks releaseMeasurementInputV2ReadHooks) (result *releaseDepositAuditPreparedV2, observedOptions ValidatorEvidencePublicationV2ReadOptions, resultErr error) {
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(self.cfg.StateDir, options.Window.Epoch, options.Window.Subject)
	if err != nil {
		return nil, options, err
	}
	preparedPath := filepath.Join(self.cfg.StateDir, "evidence-deposit-audit-prepared", filepath.Base(path))
	maximum := min(options.Bounds.MaxArtifactBytes, options.Bounds.MaxControlBytes/8, options.Bounds.MaxHistoryBytes)
	if maximum == 0 {
		return nil, options, errors.New("deposit audit prepared storage has no finite allowance")
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, preparedPath, maximum, releaseMeasurementInputV2ReadHooks{}, true)
	defer func() {
		resultErr = errors.Join(resultErr, owner.finish(), ctx.Err())
		if resultErr != nil {
			result, observedOptions = nil, ValidatorEvidencePublicationV2ReadOptions{}
		}
	}()
	if err != nil {
		return nil, options, err
	}
	if err := unix.Flock(int(owner.directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, options, err
	}
	used, count, err := censusReleaseEvidenceCapturesV2(ctx, owner.directory, options.Bounds.MaxHistoryBytes, options.Bounds.CaptureFileLimit())
	if err != nil {
		return nil, options, err
	}
	encoded, err := owner.read()
	if err == nil {
		var retained releaseDepositAuditPreparedV2
		if err := decodeValidatorEvidencePublicationV2Json(ctx, encoded, maximum, &retained); err != nil {
			return nil, options, err
		}
		if retained.Schema != releaseDepositAuditPreparedV2Schema || retained.Manifest == nil || retained.Publication == nil || retained.Manifest.Epoch != options.Window.Epoch || retained.Manifest.Subject != options.Window.Subject || len(retained.Publication.Members) != len(options.Activations) {
			return nil, options, errors.New("deposit audit prepared custody escaped its original coordinate slot")
		}
		decision, audits = retained.Manifest.Decision, nil
		for _, member := range retained.Publication.Members {
			payload, err := decodeValidatorEvidenceDepositAuditV2Payload(ctx, member.Payload, options.Bounds.MaxTransitionBytes)
			if err != nil {
				return nil, options, err
			}
			audits = append(audits, payload.Audit)
		}
		payloads, window, err := self.depositAuditSourcesV2(ctx, decision, audits)
		if err != nil {
			return nil, options, err
		}
		options.Window = window
		if err := validateValidatorEvidenceDepositAuditV2Publication(ctx, retained.Publication, retained.Manifest, options); err != nil {
			return nil, options, err
		}
		for index, payload := range payloads {
			actual, err := validatorEvidenceDepositAuditV2PayloadBytes(ctx, payload, options.Bounds.MaxTransitionBytes)
			if err != nil || !bytes.Equal(actual, retained.Publication.Members[index].Payload) {
				return nil, options, errors.Join(errors.New("deposit audit prepared payload differs from original independent source replay"), err)
			}
		}
		return &retained, options, owner.check()
	}
	if !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(err) {
		return nil, options, err
	}
	if _, err := readValidatorEvidenceDepositAuditV2Manifest(ctx, path, options.Bounds.MaxClosureBytes, options.Bounds.MaxParticipants, hooks); !releaseMeasurementInputV2InitialAbsence(err) {
		return nil, options, errors.Join(errors.New("deposit audit completed locator has no original prepared signature custody"), err)
	}
	if count >= options.Bounds.CaptureFileLimit() || used >= options.Bounds.MaxHistoryBytes {
		return nil, options, errors.New("deposit audit prepared history exceeds its finite census")
	}
	payloads, window, err := self.depositAuditSourcesV2(ctx, decision, audits)
	if err != nil {
		return nil, options, err
	}
	options.Window = window
	result, err = self.buildDepositAuditPublicationV2(ctx, payloads, options)
	if err != nil {
		return nil, options, err
	}
	encoded, err = marshalAttemptSettlementV2JSON(ctx, result, min(maximum, options.Bounds.MaxHistoryBytes-used), false, true)
	if err != nil {
		return nil, options, err
	}
	if err := owner.write(encoded); err != nil {
		return nil, options, err
	}
	return result, options, owner.check()
}

// The actual SubmitOnce and retained-intent branch call this before native
// broadcast. Existing protected writers preserve source and destination keys.
func (self *releaseRuntimeV2) publishDepositAuditV2(ctx context.Context, artifact *ReleaseMeasurementArtifact) (resultErr error) {
	return self.publishDepositAuditV2WithReadHooks(ctx, artifact, releaseMeasurementInputV2ReadHooks{})
}

// Hooks observe only actual locator custody. Signing, source replay and both
// protected replicas are always the production implementations.
func (self *releaseRuntimeV2) publishDepositAuditV2WithReadHooks(ctx context.Context, artifact *ReleaseMeasurementArtifact, hooks releaseMeasurementInputV2ReadHooks) (resultErr error) {
	release, err := self.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	if artifact == nil || artifact.Schema != ReleaseMeasurementSchemaV2 || len(self.history.participants) == 0 || len(self.runtimes) == 0 {
		return errors.New("deposit audit actual runtime or retained measurement is absent")
	}
	decision := releaseMeasurementV2Decision(artifact)
	decision.PreviousArtifactHash = ""
	lag := self.cfg.Policy.Deposit.UsageLagEpochs
	if lag == 0 {
		return errors.New("deposit audit policy has no later usage lag")
	}
	if decision.SettlementEpoch < lag {
		return ctx.Err()
	}
	epoch := decision.SettlementEpoch - lag
	options := ValidatorEvidencePublicationV2ReadOptions{Origins: self.origins, Bounds: self.cfg.EvidenceV2.Bounds, Window: protocol.ValidatorEvidenceWindow{Epoch: epoch, Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: decision.SettlementEpoch, NativeEpoch: decision.SubnetEpoch}}}
	for _, participant := range self.history.participants {
		source := self.sources[participant.NoID]
		if source == nil {
			return errors.New("deposit audit has no original source owner")
		}
		if epoch < source.activation.Domain.Epoch {
			return ctx.Err()
		}
		options.Activations = append(options.Activations, source.activation)
	}
	replicas, err := releaseReservedAttemptCensusReplicasV2(&self.cfg, self.origins, self.runtimes)
	if err != nil {
		return err
	}
	prepared, options, err := self.prepareDepositAuditPublicationV2(ctx, decision, artifact.DepositAudits, options, hooks)
	if err != nil {
		return err
	}
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(self.cfg.StateDir, prepared.Manifest.Epoch, prepared.Manifest.Subject)
	if err != nil {
		return err
	}
	manifest, err := readValidatorEvidenceDepositAuditV2Manifest(ctx, path, options.Bounds.MaxClosureBytes, options.Bounds.MaxParticipants, hooks)
	if err == nil {
		publication, err := ReadValidatorEvidenceDepositAuditV2(ctx, manifest, options)
		if err != nil {
			return err
		}
		want, err := marshalAttemptSettlementV2JSON(ctx, prepared.Publication, options.Bounds.MaxArtifactBytes, false, true)
		if err != nil {
			return err
		}
		actual, err := marshalAttemptSettlementV2JSON(ctx, publication, options.Bounds.MaxArtifactBytes, false, true)
		if err != nil || !bytes.Equal(want, actual) {
			return errors.Join(errors.New("deposit audit completed public bytes differ from original prepared consent"), err)
		}
		return ctx.Err()
	}
	if !releaseMeasurementInputV2InitialAbsence(err) {
		return err
	}
	for index, member := range prepared.Publication.Members {
		publisher, err := newAttemptCutV2ReplicasWithMetadataLimit(options.Bounds.Cut, replicas[member.Evidence.Header.NoID], max(attemptStreamV2MetadataBytes(options.Bounds.Cut), options.Bounds.MaxTransitionBytes))
		if err != nil {
			return err
		}
		if err := publisher.writer("metadata")(ctx, attemptHex32(member.Evidence.Header.PayloadHash), member.Payload); err != nil {
			return err
		}
		if index == 0 {
			if err := publisher.writer("metadata")(ctx, attemptHex32(prepared.Publication.CensusHash), prepared.Publication.Census); err != nil {
				return err
			}
		}
		if err := publisher.writer("metadata")(ctx, attemptHex32(member.SignedArtifactHash), member.SignedArtifact); err != nil {
			return err
		}
	}
	if _, err := ReadValidatorEvidenceDepositAuditV2(ctx, prepared.Manifest, options); err != nil {
		return err
	}
	encoded, err := marshalAttemptSettlementV2JSON(ctx, prepared.Manifest, options.Bounds.MaxClosureBytes, true, true)
	if err != nil {
		return err
	}
	if err := writeReleaseMeasurementInputV2Context(ctx, path, encoded, options.Bounds.MaxClosureBytes, releaseMeasurementInputV2ReadHooks{}); err != nil {
		return err
	}
	_, err = ReadValidatorEvidenceDepositAuditV2Manifest(ctx, path, options.Bounds.MaxClosureBytes, options.Bounds.MaxParticipants)
	return errors.Join(err, ctx.Err())
}
