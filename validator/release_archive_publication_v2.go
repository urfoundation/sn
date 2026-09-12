//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
)

type releaseArchivePublicationV2 struct {
	window      protocol.ValidatorEvidenceWindow
	closed      *ValidatorEvidencePublicationV2Manifest
	audit       *ValidatorEvidenceDepositAuditV2Manifest
	publication *ValidatorEvidenceCensusV2Publication
}

type releaseArchivePublicationReaderV2 struct {
	owner  *releaseEvidenceV2ArchiveOwner
	origin string
	kinds  [3]string
}

func decodeReleaseArchivePublicationManifestV2(ctx context.Context, raw []byte, limit uint64, value any) error {
	if err := decodeAttemptStreamV2JSON(raw, limit, value); err != nil {
		return err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, value, limit, true, true)
	if err != nil || !bytes.Equal(raw, canonical) {
		return errors.Join(errors.New("archive publication manifest differs from original canonical private bytes"), err)
	}
	return nil
}

func (self releaseArchivePublicationReaderV2) ReadMetadata(ctx context.Context, hash string, size uint64) ([]byte, error) {
	var selected ReleaseEvidenceV2CaptureSource
	for _, kind := range self.kinds {
		source := ReleaseEvidenceV2CaptureSource{Kind: kind, Name: hash, Origin: self.origin}
		if _, found := self.owner.sources[source]; found {
			if selected.Kind != "" {
				return nil, errors.New("archive publication content has ambiguous provenance")
			}
			selected = source
		}
	}
	raw, err := self.owner.read(ctx, selected, size)
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) != size {
		return nil, errors.New("archive publication metadata size differs")
	}
	return raw, nil
}

func sameReleaseArchivePublicationV2(a, b *ValidatorEvidenceCensusV2Publication) bool {
	if a == nil || b == nil || a.Origins != b.Origins || a.CensusHash != b.CensusHash || !bytes.Equal(a.Census, b.Census) || len(a.Members) != len(b.Members) {
		return false
	}
	for i, member := range a.Members {
		other := b.Members[i]
		if !bytes.Equal(member.Payload, other.Payload) || !bytes.Equal(member.SignedArtifact, other.SignedArtifact) || !bytes.Equal(member.Calldata, other.Calldata) {
			return false
		}
	}
	return true
}

func (self *ReleaseEvidenceV2Archive) publicationOptionsV2(window protocol.ValidatorEvidenceWindow) ValidatorEvidencePublicationV2ReadOptions {
	options := ValidatorEvidencePublicationV2ReadOptions{Window: window, Origins: self.owner.origins, Bounds: self.owner.cfg.EvidenceV2.Bounds}
	for _, input := range self.inputs {
		options.Activations = append(options.Activations, input.Context.Activation)
	}
	return options
}

// This join consumes the same typed public decoder as the real HTTP reader,
// then compares its original payload and counts with the completed trail and
// terminal replay. A signature or self-declared count cannot create a closure.
func (self *ReleaseEvidenceV2Archive) terminalPublicationV2(ctx context.Context, window protocol.ValidatorEvidenceWindow) (*releaseArchivePublicationV2, error) {
	options := self.publicationOptionsV2(window)
	path, err := ValidatorEvidencePublicationV2ManifestPath(self.owner.cfg.StateDir, window.Epoch)
	if err != nil {
		return nil, err
	}
	raw, err := self.owner.readPrivate(ctx, path, options.Bounds.MaxClosureBytes, false)
	if err != nil {
		return nil, err
	}
	var manifest ValidatorEvidencePublicationV2Manifest
	if err := decodeReleaseArchivePublicationManifestV2(ctx, raw, options.Bounds.MaxClosureBytes, &manifest); err != nil {
		return nil, err
	}
	if err := manifest.validate(options.Bounds.MaxClosureBytes, options.Bounds.MaxParticipants); err != nil {
		return nil, err
	}
	if manifest.Epoch != window.Epoch || manifest.Origins != options.Origins || len(manifest.Members) != len(options.Activations) {
		return nil, errors.New("archive terminal publication escaped its original window or members")
	}
	for index, member := range manifest.Members {
		if member.NoId != options.Activations[index].NoID {
			return nil, errors.New("archive terminal manifest operator order differs")
		}
	}
	closure := self.history.terminals[window.Epoch]
	if closure == nil || len(closure.Transitions) != len(manifest.Members) {
		return nil, errors.New("archive terminal publication has no complete replayed closure")
	}
	var publication *ValidatorEvidenceCensusV2Publication
	limit := max(attemptStreamV2MetadataBytes(options.Bounds.Cut), options.Bounds.MaxTransitionBytes)
	for _, origin := range options.Origins {
		reader := releaseArchivePublicationReaderV2{owner: self.owner, origin: origin, kinds: [3]string{"closed-census", "signed-evidence", "terminal-payload"}}
		observed, err := readValidatorEvidencePublicationV2Origin(ctx, reader, &manifest, options, limit)
		if err != nil {
			return nil, err
		}
		if publication != nil && !sameReleaseArchivePublicationV2(publication, observed) {
			return nil, errors.New("archive terminal publication replicas differ")
		}
		publication = observed
	}
	var census ValidatorEvidenceCensusV2
	if err := decodeValidatorEvidencePublicationV2Json(ctx, publication.Census, options.Bounds.MaxClosureBytes, &census); err != nil {
		return nil, err
	}
	for index, transition := range closure.Transitions {
		member := census.Members[index]
		if transition.Identity.NoID != options.Activations[index].NoID || member.NoID != transition.Identity.NoID || census.Boundary != transition.FromBoundary || member.RecordCount != transition.Cut.RecordCount || member.CompleteCount != transition.Cut.CompleteCount || member.FailedCount != transition.Cut.FailedCount {
			return nil, errors.New("archive published census differs from complete terminal replay")
		}
		payload, err := marshalAttemptSettlementV2JSON(ctx, transition, options.Bounds.MaxTransitionBytes, false, true)
		if err != nil || !bytes.Equal(payload, publication.Members[index].Payload) {
			return nil, errors.Join(errors.New("archive published terminal payload differs from original replayed transition"), err)
		}
	}
	return &releaseArchivePublicationV2{window: window, closed: &manifest, publication: publication}, self.owner.check(ctx)
}

func (self *ReleaseEvidenceV2Archive) auditPublicationV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, window protocol.ValidatorEvidenceWindow) (*releaseArchivePublicationV2, error) {
	options := self.publicationOptionsV2(window)
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(self.owner.cfg.StateDir, window.Epoch, window.Subject)
	if err != nil {
		return nil, err
	}
	raw, err := self.owner.readPrivate(ctx, path, options.Bounds.MaxClosureBytes, false)
	if err != nil {
		return nil, err
	}
	var manifest ValidatorEvidenceDepositAuditV2Manifest
	if err := decodeReleaseArchivePublicationManifestV2(ctx, raw, options.Bounds.MaxClosureBytes, &manifest); err != nil {
		return nil, err
	}
	if _, err := admitValidatorEvidenceDepositAuditV2(&manifest, options); err != nil {
		return nil, err
	}
	decision := releaseMeasurementV2Decision(artifact)
	decision.PreviousArtifactHash = ""
	if manifest.Decision != decision || len(artifact.DepositAudits) != len(options.Activations) {
		return nil, errors.New("archive deposit publication differs from the fully replayed decision")
	}
	var publication *ValidatorEvidenceCensusV2Publication
	for _, origin := range options.Origins {
		reader := releaseArchivePublicationReaderV2{owner: self.owner, origin: origin, kinds: [3]string{"audit-census", "signed-audit", "audit-payload"}}
		observed, err := readValidatorEvidenceDepositAuditV2Origin(ctx, reader, &manifest, options)
		if err != nil {
			return nil, err
		}
		if publication != nil && !sameReleaseArchivePublicationV2(publication, observed) {
			return nil, errors.New("archive audit publication replicas differ")
		}
		publication = observed
	}
	for index, member := range publication.Members {
		payload, err := decodeValidatorEvidenceDepositAuditV2Payload(ctx, member.Payload, options.Bounds.MaxTransitionBytes)
		if err != nil {
			return nil, err
		}
		if payload.Audit != artifact.DepositAudits[index] || payload.Decision != decision {
			return nil, errors.New("archive signed deposit payload differs from independently replayed source observations")
		}
		if payload.Audit.HttpObservationHash != "" {
			var observation releaseArtifactHttpObservationV2
			if err := json.Unmarshal(payload.HttpObservation, &observation); err != nil {
				return nil, err
			}
			var origin string
			for _, operator := range self.owner.cfg.Operators {
				if operator.NoID == payload.Audit.NoID {
					origin = operator.APIURL
				}
			}
			if observation.Origin != origin {
				return nil, errors.New("archive audit HTTP origin differs from original configuration")
			}
			reader, err := NewHTTPArtifactReader(origin, self.owner.cfg.DeploymentID, self.owner.cfg.Netuid)
			if err != nil {
				return nil, err
			}
			_, path, err := releaseArtifactHttpRequestV2(&self.owner.cfg, self.inputs[index].Context.Activation.Hotkey, artifact, payload.Audit.NoID, payload.Audit.SourceEpoch, reader)
			if err != nil {
				return nil, err
			}
			original, err := self.owner.readPrivate(ctx, path, options.Bounds.MaxTransitionBytes, false)
			if err != nil || !bytes.Equal(original, payload.HttpObservation) {
				return nil, errors.Join(errors.New("archive audit replaced the original retained HTTP exchange"), err)
			}
		}
	}
	return &releaseArchivePublicationV2{window: window, audit: &manifest, publication: publication}, self.owner.check(ctx)
}

// Every settlement retains a terminal. Audit subjects follow the actual
// native decisions; a different native tempo cannot invent an audit decision
// for a settlement in which no native decision occurred.
func (self *ReleaseEvidenceV2Archive) publicationsV2(ctx context.Context, firstEpoch, epochCount uint64, window func(uint64) (protocol.ValidatorEvidenceWindow, error)) ([]*releaseArchivePublicationV2, error) {
	if ctx == nil || self == nil || self.closed || self.history == nil || self.measurements == nil || epochCount == 0 || firstEpoch > ^uint64(0)-epochCount || epochCount > uint64(len(self.history.terminals)) {
		return nil, errors.New("archive publication replay owner or complete finite window differs")
	}
	if err := self.owner.check(ctx); err != nil {
		return nil, err
	}
	var result []*releaseArchivePublicationV2
	for epoch := firstEpoch; epoch < firstEpoch+epochCount; epoch++ {
		geometry, err := window(epoch)
		if err != nil {
			return nil, err
		}
		item, err := self.terminalPublicationV2(ctx, geometry)
		if err != nil {
			return nil, fmt.Errorf("archive terminal epoch %d: %w", epoch, err)
		}
		result = append(result, item)
	}
	seen := map[protocol.ValidatorEvidenceSubject]bool{}
	for _, item := range self.intents {
		if item.Intent.Status != "applied" || item.Intent.SettlementEpoch < firstEpoch || item.Intent.SettlementEpoch-firstEpoch >= epochCount {
			continue
		}
		artifact, _, err := self.Measurement(item.Intent.MeasurementArtifactHash)
		if err != nil {
			return nil, err
		}
		lag := self.owner.cfg.Policy.Deposit.UsageLagEpochs
		if lag == 0 || artifact.SettlementEpoch < lag {
			return nil, errors.New("archive deposit source epoch underflows its original policy")
		}
		geometry, err := window(artifact.SettlementEpoch - lag)
		if err != nil {
			return nil, err
		}
		geometry.Subject = protocol.ValidatorEvidenceSubject{ObservationEpoch: artifact.SettlementEpoch, NativeEpoch: artifact.SubnetEpoch}
		if seen[geometry.Subject] {
			return nil, errors.New("archive repeats a native deposit-audit subject")
		}
		seen[geometry.Subject] = true
		publication, err := self.auditPublicationV2(ctx, artifact, geometry)
		if err != nil {
			return nil, fmt.Errorf("archive audit native epoch %d: %w", artifact.SubnetEpoch, err)
		}
		result = append(result, publication)
	}
	return result, self.owner.check(ctx)
}

// ReplayPublicationsV2 verifies original signed public bytes using the final
// archive's independently pinned settlement geometry. It does not prove that
// a companion contract contains a slot; AuthenticatePublicationsV2 does that.
func (self *ReleaseEvidenceV2Archive) ReplayPublicationsV2(ctx context.Context, firstEpoch, epochCount, startBlock, epochBlocks, terminalBlock uint64) error {
	if epochBlocks == 0 {
		return errors.New("archive publication epoch span is absent")
	}
	window := func(epoch uint64) (protocol.ValidatorEvidenceWindow, error) {
		start := startBlock
		if epoch >= firstEpoch {
			delta := epoch - firstEpoch
			if delta > (^uint64(0)-start)/epochBlocks {
				return protocol.ValidatorEvidenceWindow{}, errors.New("archive publication epoch geometry overflows")
			}
			start += delta * epochBlocks
		} else {
			delta := firstEpoch - epoch
			if delta > start/epochBlocks {
				return protocol.ValidatorEvidenceWindow{}, errors.New("archive publication prior epoch geometry underflows")
			}
			start -= delta * epochBlocks
		}
		if start == 0 || start > ^uint64(0)-epochBlocks || start+epochBlocks > terminalBlock {
			return protocol.ValidatorEvidenceWindow{}, errors.New("archive publication is outside the final closed window")
		}
		return protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: start, EndBlock: start + epochBlocks, FinalizedBlock: terminalBlock}, nil
	}
	_, err := self.publicationsV2(ctx, firstEpoch, epochCount, window)
	return err
}

// AuthenticatePublicationsV2 uses the caller's actual recorded chain client.
// Each epoch boundary is read at the canonical terminal; every original dual
// consent and payload is fetched again from both independently approved public
// origins, then its exact stable companion slot and runtime are authenticated.
func (self *ReleaseEvidenceV2Archive) AuthenticatePublicationsV2(ctx context.Context, chain *ChainClient, firstEpoch, epochCount, terminalBlock uint64, terminalHash [32]byte) error {
	if chain == nil || terminalBlock == 0 || terminalHash == ([32]byte{}) {
		return errors.New("archive publication canonical terminal reader is absent")
	}
	window := func(epoch uint64) (protocol.ValidatorEvidenceWindow, error) {
		start, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, terminalBlock, terminalHash, new(big.Int).SetUint64(epoch))
		if err != nil {
			return protocol.ValidatorEvidenceWindow{}, err
		}
		end, err := chain.ReleaseEpochEndBlockAtHashContext(ctx, terminalBlock, terminalHash, new(big.Int).SetUint64(epoch))
		if err != nil {
			return protocol.ValidatorEvidenceWindow{}, err
		}
		return protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: start, EndBlock: end, FinalizedBlock: terminalBlock}, nil
	}
	publications, err := self.publicationsV2(ctx, firstEpoch, epochCount, window)
	if err != nil {
		return err
	}
	for _, item := range publications {
		options := self.publicationOptionsV2(item.window)
		var actual *ValidatorEvidenceCensusV2Publication
		if item.closed != nil {
			actual, err = ReadValidatorEvidencePublicationV2(ctx, item.closed, options)
		} else {
			actual, err = ReadValidatorEvidenceDepositAuditV2(ctx, item.audit, options)
		}
		if err != nil {
			return err
		}
		if !sameReleaseArchivePublicationV2(item.publication, actual) {
			return errors.New("archive publication differs from independently read public origins")
		}
		for index, member := range actual.Members {
			initial := self.inputs[index].Context
			if _, err := chain.ValidatorEvidenceAtHashContext(ctx, common.Address(initial.Journal), initial.RuntimeHash, initial.Activation, member.Evidence.Header, item.window, terminalBlock, terminalHash); err != nil {
				return err
			}
		}
	}
	return self.owner.check(ctx)
}
