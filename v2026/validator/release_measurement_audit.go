package validator

// A semantic replay authenticates each signed measurement before comparing
// adjacent epochs. Retain only its detached lineage fields for that comparison.

import (
	"bytes"
	"errors"
	"slices"
)

type releaseMeasurementEnvelopeVerifier func(*ReleaseMeasurementEnvelope, []byte, [32]byte, uint16, string) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, error)

// ReleaseMeasurementAudit owns one replay's authenticated lineage snapshots.
// Construct a new audit for every replay; its zero value cannot issue or use
// snapshots. Every VerifyEnvelope call still authenticates its full input.
type ReleaseMeasurementAudit struct {
	owner *releaseMeasurementAuditOwner
}

// This allocation must have nonzero size so independent audits have distinct
// identities even when their measurements and signature options are equal.
type releaseMeasurementAuditOwner struct{ marker byte }

// ReleaseMeasurementSnapshot is opaque and contains no caller-owned storage.
// Only the audit that authenticated it can use its completed lineage work.
type ReleaseMeasurementSnapshot struct {
	owner       *releaseMeasurementAuditOwner
	contentHash string
	hotkey      [32]byte
	uid         uint16
	lineage     ReleaseMeasurementArtifact
}

func NewReleaseMeasurementAudit() *ReleaseMeasurementAudit {
	return &ReleaseMeasurementAudit{owner: &releaseMeasurementAuditOwner{}}
}

// VerifyEnvelope keeps the existing signature, canonical-byte, expected-hotkey,
// UID, prepared-extrinsic and full measurement checks. The returned artifact and
// decision remain caller-owned; mutating them cannot change the snapshot.
func (audit *ReleaseMeasurementAudit) VerifyEnvelope(envelope *ReleaseMeasurementEnvelope, measurement []byte, expectedHotkey [32]byte, expectedUID uint16, expectedPreparedExtrinsicHash string) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, *ReleaseMeasurementSnapshot, error) {
	return audit.verifyEnvelope(envelope, measurement, expectedHotkey, expectedUID, expectedPreparedExtrinsicHash, VerifyReleaseMeasurementEnvelope)
}

// The private callback observes real full-body authentication in deterministic
// work-bound tests. Public callers cannot replace the verifier.
func (audit *ReleaseMeasurementAudit) verifyEnvelope(envelope *ReleaseMeasurementEnvelope, measurement []byte, expectedHotkey [32]byte, expectedUID uint16, expectedPreparedExtrinsicHash string, verify releaseMeasurementEnvelopeVerifier) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, *ReleaseMeasurementSnapshot, error) {
	if audit == nil || audit.owner == nil || envelope == nil || verify == nil {
		return nil, nil, nil, errors.New("release measurement audit input is unavailable")
	}
	if len(measurement) == 0 || len(measurement) > releaseMeasurementEnvelopeMaxArtifactSize {
		return nil, nil, nil, errors.New("release measurement envelope artifact size differs")
	}
	// Envelope fields are values and immutable strings. Own the body before
	// both its authentication and content hash, then discard the large bytes.
	ownedEnvelope := *envelope
	owned := bytes.Clone(measurement)
	artifact, verified, err := verify(&ownedEnvelope, owned, expectedHotkey, expectedUID, expectedPreparedExtrinsicHash)
	if err != nil {
		return nil, nil, nil, err
	}
	if artifact == nil || verified == nil {
		return nil, nil, nil, errors.New("release measurement audit has no completed authentication")
	}
	snapshot := &ReleaseMeasurementSnapshot{
		owner: audit.owner, contentHash: ReleaseMeasurementContentHash(owned),
		hotkey: expectedHotkey, uid: expectedUID,
		lineage: releaseMeasurementLineageSnapshot(artifact),
	}
	return artifact, verified, snapshot, nil
}

// VerifyLineage performs every existing cross-record comparison on completed
// snapshots without decoding or authenticating either full body again. The
// semantic caller separately pins each cycle's prepared extrinsic and intent.
func (audit *ReleaseMeasurementAudit) VerifyLineage(previous, current *ReleaseMeasurementSnapshot) error {
	if audit == nil || audit.owner == nil || previous == nil || current == nil || previous.owner != audit.owner || current.owner != audit.owner {
		return errors.New("release measurement lineage snapshots belong to another audit or are unavailable")
	}
	if previous.hotkey != current.hotkey || previous.uid != current.uid {
		return errors.New("release measurement lineage envelope authority changed")
	}
	return verifyReleaseMeasurementLineage(&previous.lineage, &current.lineage, previous.contentHash)
}

// This projection is never returned as a measurement or accepted by a body
// verifier. Copy exactly the fields consumed by the shared lineage checks:
// domain, clocks, pool EMA bases, signed cut prefixes and head EMA transitions.
func releaseMeasurementLineageSnapshot(artifact *ReleaseMeasurementArtifact) ReleaseMeasurementArtifact {
	value := ReleaseMeasurementArtifact{
		DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID,
		GenesisHash: artifact.GenesisHash, Coordinator: artifact.Coordinator,
		SettlementVault: artifact.SettlementVault, ValidatorID: artifact.ValidatorID,
		Netuid: artifact.Netuid, SubnetEpoch: artifact.SubnetEpoch,
		SettlementEpoch: artifact.SettlementEpoch, PreviousArtifactHash: artifact.PreviousArtifactHash,
		HeadEMA: slices.Clone(artifact.HeadEMA),
		Inputs:  make([]ReleaseMeasurementInput, len(artifact.Inputs)),
	}
	for index, input := range artifact.Inputs {
		value.Inputs[index] = ReleaseMeasurementInput{NoID: input.NoID, Stats: releaseStatsLineageSnapshot(input.Stats)}
		if transition := input.Stats.SettlementTransition; transition != nil {
			value.Inputs[index].Stats.SettlementTransition = &AttemptSettlementTransition{
				FromBoundary: transition.FromBoundary, ToEpoch: transition.ToEpoch,
				PreFold: releaseStatsLineageSnapshot(transition.PreFold),
			}
		}
	}
	return value
}

func releaseStatsLineageSnapshot(stats ReleaseStatsMeasurement) ReleaseStatsMeasurement {
	value := ReleaseStatsMeasurement{Providers: make([]ReleaseProviderMeasurement, len(stats.Providers))}
	for index, provider := range stats.Providers {
		value.Providers[index] = ReleaseProviderMeasurement{
			ClientID: provider.ClientID, HasPriorQuality: provider.HasPriorQuality,
			PriorQualityPPM: provider.PriorQualityPPM,
		}
	}
	if cut := stats.AttemptCut; cut != nil {
		value.AttemptCut = &AttemptLedgerCut{
			Identity: cut.Identity, Boundary: cut.Boundary, FirstSequence: cut.FirstSequence,
			LastSequence: cut.LastSequence, PriorRoot: cut.PriorRoot, Root: cut.Root,
			Records: make([]AttemptRecord, len(cut.Records)),
		}
		for index, record := range cut.Records {
			value.AttemptCut.Records[index].RecordHash = record.RecordHash
		}
	}
	return value
}
