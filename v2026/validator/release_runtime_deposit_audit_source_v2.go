//go:build linux || darwin

// The actual later audit producer re-reads pinned native/Evm state and replays
// the retained real payout exchange. No intent verdict or public locator is an
// authority for source epoch, subject, payout eligibility or HTTP availability.
package validator

import (
	"context"
	"errors"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A private projection carries no prepared/native signature or chosen weights.
func releaseDepositAuditV2Artifact(decision ReleaseMeasurementV2Decision, audits []DepositAudit, policy protocol.Policy) *ReleaseMeasurementArtifact {
	return &ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, DeploymentID: decision.DeploymentID, ChainID: decision.ChainID, GenesisHash: decision.GenesisHash,
		Coordinator: decision.Coordinator, SettlementVault: decision.SettlementVault, ValidatorID: decision.ValidatorID, Netuid: decision.Netuid, SubnetEpoch: decision.SubnetEpoch,
		NativeSnapshotBlock: decision.NativeSnapshotBlock, NativeSnapshotHash: decision.NativeSnapshotHash, EVMSnapshotBlock: decision.EVMSnapshotBlock, EVMSnapshotHash: decision.EVMSnapshotHash,
		SettlementEpoch: decision.SettlementEpoch, PolicyHash: decision.PolicyHash, SelfUID: decision.SelfUID, Policy: policy, DepositAudits: slices.Clone(audits)}
}

// Only audit source fields are queried; provider/trail replay already belongs
// to the actual measurement and is not repeated merely to publish this slot.
func (self *releaseRuntimeV2) depositAuditSourcesV2(ctx context.Context, decision ReleaseMeasurementV2Decision, claimed []DepositAudit) (payloads []ValidatorEvidenceDepositAuditV2Payload, window protocol.ValidatorEvidenceWindow, resultErr error) {
	if ctx == nil || self == nil || self.history == nil || self.chain == nil || self.native == nil || self.hotkey == nil || len(self.history.participants) == 0 {
		return nil, window, errors.New("deposit audit source owner is incomplete")
	}
	bounds := self.cfg.EvidenceV2.Bounds
	if len(claimed) != len(self.history.participants) || uint64(len(claimed)) > bounds.MaxParticipants || decision.PreviousArtifactHash != "" || decision.ValidatorID != self.cfg.ValidatorID || self.cfg.Policy.Deposit.UsageLagEpochs == 0 || decision.SettlementEpoch < self.cfg.Policy.Deposit.UsageLagEpochs {
		return nil, window, errors.New("deposit audit source census or later usage lag differs")
	}
	claimed = slices.Clone(claimed)
	first := self.sources[self.history.participants[0].NoID]
	if first == nil {
		return nil, window, errors.New("deposit audit source has no original activation")
	}
	domain, err := first.activation.EvidenceDomain()
	if err != nil {
		return nil, window, err
	}
	query := releaseDecisionChainV2Query{domain: domain, boundary: AttemptBoundary{SettlementEpoch: decision.SettlementEpoch, EVMBlock: decision.EVMSnapshotBlock, EVMBlockHash: decision.EVMSnapshotHash}, policy: self.cfg.Policy,
		maxOperators: bounds.MaxOperators, maxProviders: bounds.MaxProviders, maxControlBytes: bounds.MaxControlBytes}
	for index, participant := range self.history.participants {
		if claimed[index].NoID != participant.NoID || self.sources[participant.NoID] == nil {
			return nil, window, errors.New("deposit audit omitted or reordered an actual source")
		}
		query.operators = append(query.operators, releaseDecisionChainV2OperatorQuery{noID: participant.NoID})
	}
	nativeHash, err := canonicalAttemptHex32("deposit audit actual native boundary", decision.NativeSnapshotHash, false)
	if err != nil {
		return nil, window, err
	}
	observationCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(&self.cfg))
	observed, schedule, err := readReleaseDecisionV2Context(observationCtx, self.chain, self.native, query, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(domain.GenesisHash), BlockHash: types.Hash(nativeHash), BlockNumber: decision.NativeSnapshotBlock, Netuid: domain.Netuid, Hotkey: self.hotkey.PublicKey(), MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, releaseRuntimeIdentityV2(&self.cfg))
	cancel()
	if err != nil || schedule.SubnetEpochIndex != decision.SubnetEpoch || schedule.Stake.Identity.UID != decision.SelfUID {
		return nil, window, errors.Join(errors.New("deposit audit native observation differs from its actual decision"), err)
	}
	window = protocol.ValidatorEvidenceWindow{Epoch: observed.sourceEpoch, StartBlock: observed.sourceStart, EndBlock: observed.sourceEnd, FinalizedBlock: observed.boundary.EVMBlock,
		Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: observed.boundary.SettlementEpoch, NativeEpoch: schedule.SubnetEpochIndex}}
	if err := validateValidatorEvidenceDepositAuditV2Decision(decision, domain, window); err != nil {
		return nil, window, err
	}
	projection := releaseDepositAuditV2Artifact(decision, claimed, self.cfg.Policy)
	custody := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
	defer func() {
		resultErr = errors.Join(resultErr, custody.close(), ctx.Err())
		if resultErr != nil {
			payloads, window = nil, protocol.ValidatorEvidenceWindow{}
		}
	}()
	for index, operator := range observed.operators {
		if !operator.version.Active {
			return nil, window, errors.New("deposit audit current source was inactive at its actual observation")
		}
		audit, err := self.history.historicalDepositAuditWithCaptureV2(ctx, observed, operator, claimed[index], projection, custody)
		if err != nil || audit != claimed[index] {
			return nil, window, errors.Join(errors.New("deposit audit differs from actual pinned chain and retained payout source replay"), err)
		}
		payload := ValidatorEvidenceDepositAuditV2Payload{Schema: ValidatorEvidenceDepositAuditV2Schema, Decision: decision, Audit: audit}
		if audit.HttpObservationHash != "" {
			var origin string
			for _, configured := range self.cfg.Operators {
				if configured.NoID == operator.noID {
					origin = configured.APIURL
					break
				}
			}
			reader, err := NewHTTPArtifactReader(origin, self.cfg.DeploymentID, self.cfg.Netuid)
			if err != nil {
				return nil, window, err
			}
			expected, path, err := releaseArtifactHttpRequestV2(&self.cfg, self.hotkey.PublicKey(), projection, operator.noID, observed.sourceEpoch, reader)
			if err != nil {
				return nil, window, err
			}
			maximum := min(bounds.MaxArtifactBytes, bounds.MaxControlBytes/8)
			raw, err := custody.read(ctx, path, maximum, false)
			if err != nil || ReleaseMeasurementContentHash(raw) != audit.HttpObservationHash {
				return nil, window, errors.Join(errors.New("deposit audit actual HTTP custody differs"), err)
			}
			if _, err := decodeArtifactHttpObservationV2(ctx, raw, maximum, expected); err != nil {
				return nil, window, err
			}
			payload.HttpObservation = raw
		}
		payloads = append(payloads, payload)
	}
	return payloads, window, custody.check()
}
