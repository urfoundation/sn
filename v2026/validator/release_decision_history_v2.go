//go:build linux || darwin

// Retained intents must match actual historical chain observations. This
// refuses the historical API facts that the current wire never preserved;
// neither current HTTP responses nor signed artifact claims fill those gaps.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

var (
	errReleaseHistoricalClientKeyV2 = errors.New("historical client public-key observation is not independently retained")
	errReleaseHistoricalArtifactV2  = errors.New("historical negative artifact response or history evidence is not independently retained")
)

// Native registration/stake and EVM state have separate pinned hashes. The
// native observation cannot be replaced with a claimed EVM-height identity.
func readReleaseDecisionV2Context(ctx context.Context, chain *ChainClient, native *crv4.Chain, query releaseDecisionChainV2Query, schedule crv4.ValidatorScheduleQuery, runtime crv4.RuntimeArtifactIdentity) (result *releaseDecisionChainV2Observation, observed crv4.ValidatorScheduleObservation, resultErr error) {
	if ctx == nil || schedule.GenesisHash != types.Hash(query.domain.GenesisHash) || schedule.Netuid != query.domain.Netuid {
		return nil, observed, errors.New("decision native and EVM deployment authorities differ")
	}
	// Capture all query slices before even the native reader's first callback.
	query, budget, err := ownReleaseDecisionChainV2Query(ctx, query)
	if err != nil {
		return nil, observed, err
	}
	lag := query.boundary.EVMBlock
	if schedule.BlockNumber > lag {
		lag = schedule.BlockNumber - lag
	} else {
		lag -= schedule.BlockNumber
	}
	if query.policy.Safety.MaximumFinalizedHeadLagBlocks < 0 || lag > uint64(query.policy.Safety.MaximumFinalizedHeadLagBlocks) {
		return nil, observed, errors.New("decision native and EVM boundaries exceed the canonical lag allowance")
	}
	chain, err = chain.ownReleaseDecisionChainV2Client(query.domain)
	if err != nil {
		return nil, observed, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result, observed = nil, crv4.ValidatorScheduleObservation{}
		}
	}()
	observed, err = crv4.ReadValidatorScheduleAtContext(ctx, native, schedule, runtime)
	if err != nil || !observed.Stake.MeetsNonSelfStakeAndPermit() {
		return nil, observed, errors.Join(errors.New("decision native signer lacks real stake/permit authority"), err)
	}
	result, err = chain.readOwnedReleaseDecisionChainV2Context(ctx, query, budget)
	if err != nil {
		return nil, observed, err
	}
	uid, found := result.hotkeyUIDs[schedule.Hotkey]
	if !found || uid != observed.Stake.Identity.UID {
		return nil, observed, errors.New("decision native signer and pinned EVM metagraph disagree")
	}
	return result, observed, ctx.Err()
}

// Immutable replayed journals provide the provider census. Artifact fields
// are compared only after independent native and complete EVM observations.
func (self *releaseEvidenceV2StartupHistory) authenticateIntentChainReference(ctx context.Context, chain *ChainClient, native *crv4.Chain, runtime crv4.RuntimeArtifactIdentity, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact) error {
	if self == nil {
		return errors.New("historical decision reference owner is absent")
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: self.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	err := self.authenticateIntentChainReferenceWithKeyCustody(ctx, chain, native, runtime, intent, artifact, custody)
	return errors.Join(err, custody.close())
}

// Startup passes its existing aggregate byte owner so every capture remains
// physically retained through the complete history's final admission.
func (self *releaseEvidenceV2StartupHistory) authenticateIntentChainReferenceWithKeyCustody(ctx context.Context, chain *ChainClient, native *crv4.Chain, runtime crv4.RuntimeArtifactIdentity, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact, custody *releaseEvidenceV2StartupReferences) error {
	_, err := self.readIntentDecisionSourcesV2(ctx, chain, native, runtime, intent, artifact, custody)
	if err != nil {
		return err
	}
	if artifact.Schema == ReleaseMeasurementSchemaV2 {
		return authenticateReleaseNativeSourceReferenceV2(ctx, native, &self.cfg, intent, artifact)
	}
	return nil
}

// These are real independently reconstructed sources, not a head verdict.
// Current bindings include only keys proven by their retained root statements.
type releaseIntentDecisionSourcesV2 struct {
	decision ReleaseMeasurementV2Decision
	bindings []ReleaseBindingMeasurement
	pools    []ReleasePoolMeasurement
	audits   []DepositAudit
}

func (self *releaseEvidenceV2StartupHistory) readIntentDecisionSourcesV2(ctx context.Context, chain *ChainClient, native *crv4.Chain, runtime crv4.RuntimeArtifactIdentity, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact, custody *releaseEvidenceV2StartupReferences) (result releaseIntentDecisionSourcesV2, resultErr error) {
	if ctx == nil || self == nil || intent == nil || artifact == nil || custody == nil || len(self.participants) == 0 {
		return result, errors.New("historical decision reference owner is absent")
	}
	defer func() {
		if resultErr != nil {
			result = releaseIntentDecisionSourcesV2{}
		}
	}()
	first := self.initial[self.participants[0].NoID].InitialCut
	bounds := self.cfg.EvidenceV2.Bounds
	query := releaseDecisionChainV2Query{domain: first.Activation.Domain, boundary: AttemptBoundary{SettlementEpoch: artifact.SettlementEpoch, EVMBlock: artifact.EVMSnapshotBlock, EVMBlockHash: artifact.EVMSnapshotHash}, policy: self.cfg.Policy, maxOperators: bounds.MaxOperators, maxProviders: bounds.MaxProviders, maxControlBytes: bounds.MaxControlBytes}
	if uint64(len(self.participants)) > bounds.MaxOperators {
		return result, errors.New("historical decision operator census exceeds its bound")
	}
	admission := releaseHeadV2Budget{limit: bounds.MaxControlBytes}
	if err := admission.charge(uint64(len(self.participants)), uint64(reflect.TypeFor[releaseDecisionChainV2OperatorQuery]().Size())); err != nil {
		return result, err
	}
	for _, participant := range self.participants {
		journal := self.inputByEpoch[intent.SubnetEpoch][participant.NoID]
		if journal == nil || uint64(len(journal.MeasurementInput.Stats.Providers)) > bounds.MaxProviders {
			return result, errors.New("historical decision has no bounded independently replayed provider census")
		}
		if err := admission.charge(uint64(len(journal.MeasurementInput.Stats.Providers)), 16); err != nil {
			return result, err
		}
	}
	for _, participant := range self.participants {
		journal := self.inputByEpoch[intent.SubnetEpoch][participant.NoID]
		if journal == nil || uint64(len(journal.MeasurementInput.Stats.Providers)) > bounds.MaxProviders {
			return result, errors.New("historical decision has no bounded independently replayed provider census")
		}
		operator := releaseDecisionChainV2OperatorQuery{noID: participant.NoID, providerIDs: make([]connect.Id, len(journal.MeasurementInput.Stats.Providers))}
		for index, provider := range journal.MeasurementInput.Stats.Providers {
			id, err := connect.ParseId(provider.ClientID)
			if err != nil || id.String() != provider.ClientID {
				return result, errors.Join(errors.New("historical decision provider id is noncanonical"), err)
			}
			operator.providerIDs[index] = id
		}
		sort.Slice(operator.providerIDs, func(i, j int) bool { return operator.providerIDs[i].LessThan(operator.providerIDs[j]) })
		query.operators = append(query.operators, operator)
	}
	hash, err := canonicalAttemptHex32("historical decision native hash", artifact.NativeSnapshotHash, false)
	if err != nil {
		return result, err
	}
	observationCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(&self.cfg))
	observed, schedule, err := readReleaseDecisionV2Context(observationCtx, chain, native, query, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(query.domain.GenesisHash), BlockHash: types.Hash(hash), BlockNumber: artifact.NativeSnapshotBlock, Netuid: query.domain.Netuid, Hotkey: first.Activation.Hotkey, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, runtime)
	cancel()
	if err != nil || schedule.SubnetEpochIndex != intent.SubnetEpoch || schedule.Stake.Identity.UID != intent.SelfUID {
		return result, errors.Join(errors.New("historical decision signer or epoch differs from real chain observations"), err)
	}
	// Complete chain observations retain inactive registry members; each intent
	// participant still needs eligibility at this exact decision boundary.
	for _, operator := range observed.operators {
		if !operator.version.Active {
			return result, fmt.Errorf("historical decision operator %d was inactive at the referenced boundary", operator.noID)
		}
	}
	if len(artifact.Bindings) != len(observed.bindings) || !slices.Equal(artifact.Pools, observed.pools) {
		return result, errors.New("historical decision binding or pool census differs from actual chain state")
	}
	result.bindings = slices.Clone(observed.bindings)
	result.pools = slices.Clone(observed.pools)
	result.decision = ReleaseMeasurementV2Decision{DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: self.cfg.GenesisHash, Coordinator: self.cfg.Coordinator, SettlementVault: self.cfg.SettlementVault, ValidatorID: self.cfg.ValidatorID, Netuid: self.cfg.Netuid, SubnetEpoch: schedule.SubnetEpochIndex, NativeSnapshotBlock: artifact.NativeSnapshotBlock, NativeSnapshotHash: artifact.NativeSnapshotHash, EVMSnapshotBlock: observed.boundary.EVMBlock, EVMSnapshotHash: observed.boundary.EVMBlockHash, SettlementEpoch: observed.boundary.SettlementEpoch, PolicyHash: self.cfg.PolicyHash, PreviousArtifactHash: artifact.PreviousArtifactHash, SelfUID: schedule.Stake.Identity.UID}
	keyCtx, keyReads, err := newReleaseClientKeyAuthorityV2Reads(ctx, chain, &self.cfg, artifact, first.Activation.Hotkey, &admission)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = keyReads.finish(resultErr)
		if resultErr != nil {
			result = releaseIntentDecisionSourcesV2{}
		}
	}()
	if err := admission.charge(uint64(len(observed.bindings)), uint64(reflect.TypeFor[releaseClientKeyCapturedV2Response]().Size())+8); err != nil {
		return result, err
	}
	keySources := make([]releaseClientKeyCapturedV2Response, 0, len(observed.bindings))
	keyPositions := make([]int, 0, len(observed.bindings))
	keyQueries := []stabi.ClientKeyAuthorityQuery{}
	keyQueryKVs := make(map[stabi.ClientKeyAuthorityQuery]bool)
	for index, actual := range observed.bindings {
		claimed := artifact.Bindings[index]
		claimed.LocalClientKey = releaseHex32([32]byte{})
		claimed.ClientKeyObservationHash = ""
		if claimed != actual {
			return result, errors.New("historical decision binding identity differs from actual chain state")
		}
		if actual.Active {
			clientID, err := connect.ParseId(actual.ClientID)
			if err != nil {
				return result, err
			}
			domain, request, err := releaseClientKeyDecisionV2(&self.cfg, actual.NoID, first.Activation.Hotkey, artifact, clientID)
			if err != nil {
				return result, err
			}
			maximum, err := releaseClientKeyCaptureV2Maximum(keyCtx, min(uint64(protocol.MaxClientKeyHistoryResponseBytes), bounds.MaxArtifactBytes, bounds.MaxControlBytes/8))
			if err != nil {
				return result, err
			}
			contentHash := artifact.Bindings[index].ClientKeyObservationHash
			if _, err := parseReleaseContentHash(contentHash); err != nil {
				return result, err
			}
			path, err := releaseClientKeyCaptureV2Path(self.cfg.StateDir, domain, request)
			if err != nil {
				return result, err
			}
			encoded, err := custody.read(keyCtx, path, maximum, false)
			if err != nil || ReleaseMeasurementContentHash(encoded) != contentHash {
				return result, errors.Join(errReleaseHistoricalClientKeyV2, errors.New("retained client-key capture differs from its complete byte identity"), err)
			}
			if err := keyReads.reserveResponse(keyCtx, chain, domain, request, uint64(len(encoded))); err != nil {
				return result, err
			}
			source := releaseClientKeyCapturedV2Response{encoded: encoded, maximum: maximum, domain: domain, request: request}
			queries, err := releaseClientKeyBatchV2Queries(keyCtx, source, true)
			if err != nil {
				return result, err
			}
			for _, query := range queries {
				if !keyQueryKVs[query] {
					keyQueryKVs[query] = true
					keyQueries = append(keyQueries, query)
				}
			}
			keySources = append(keySources, source)
			keyPositions = append(keyPositions, index)
			continue
		}
		if artifact.Bindings[index].LocalClientKey != releaseHex32([32]byte{}) || artifact.Bindings[index].ClientKeyObservationHash != "" {
			return result, errors.New("historical inactive binding invents a local key observation")
		}
	}
	if err := keyReads.prefetch(keyCtx, chain, keyQueries); err != nil {
		return result, err
	}
	for index, source := range keySources {
		registration, err := verifyReservedReleaseClientKeyCaptureV2(keyCtx, chain, source.encoded, source.maximum, source.domain, source.request, true)
		position := keyPositions[index]
		actual := observed.bindings[position]
		if err != nil || !registration.Present || releaseHex32(registration.PublicKey) != actual.ClientKey || artifact.Bindings[position].LocalClientKey != actual.ClientKey {
			return result, errors.Join(errors.New("historical client key differs from its retained operator-signed capture"), err)
		}
		result.bindings[position].LocalClientKey = actual.ClientKey
		result.bindings[position].ClientKeyObservationHash = artifact.Bindings[position].ClientKeyObservationHash
	}
	if err := custody.check(); err != nil {
		return result, err
	}
	if len(artifact.DepositAudits) != len(observed.pools) {
		return result, errors.New("historical decision audit census differs from active operators")
	}
	auditIndex := 0
	for _, operator := range observed.operators {
		if !operator.version.Active {
			continue
		}
		claimed := artifact.DepositAudits[auditIndex]
		auditIndex++
		audit, err := self.historicalDepositAuditWithCaptureV2(ctx, observed, operator, claimed, artifact, custody)
		if err != nil {
			return result, err
		}
		result.audits = append(result.audits, audit)
		if claimed != audit {
			return result, errors.New("historical deposit audit differs from independently observed source evidence")
		}
	}
	return result, ctx.Err()
}

// Root absence and bootstrap are reproducible from pinned chain state alone.
// Positive signed content is fetched by the independent on-chain digest, not a
// candidate's path or the API's current mutable history listing.
func (self *releaseEvidenceV2StartupHistory) historicalDepositAudit(ctx context.Context, observed *releaseDecisionChainV2Observation, operator releaseDecisionChainV2Operator, status string) (DepositAudit, error) {
	epoch := observed.boundary.SettlementEpoch
	var audit DepositAudit
	if epoch < self.cfg.Policy.Deposit.UsageLagEpochs {
		audit = baseDepositAudit(epoch, 0, operator.noID, operator.deposit, operator.convictionBefore)
		audit.Status, audit.Disposition = DepositAuditBootstrap, "zero_pool_weight_bootstrap"
		if operator.deposit.Sign() == 0 {
			audit.Compliant = true
		} else {
			audit.Status, audit.Disposition, audit.Error = DepositAuditMismatch, "zero_pool_weight", "bootstrap epoch deposit must be zero without a prior usage artifact"
		}
	} else if operator.commitment.CommitBlock == 0 {
		status := DepositAuditUnavailablePending
		if observed.boundary.EVMBlock > observed.artifactDeadline {
			status = DepositAuditUnavailable
		}
		audit = FailedDepositAudit(epoch, observed.sourceEpoch, operator.noID, operator.deposit, operator.convictionBefore, status, errors.New("source payout root is not committed on chain"))
	} else {
		if status != DepositAuditCompliant && status != DepositAuditMismatch {
			return DepositAudit{}, errReleaseHistoricalArtifactV2
		}
		var config *OperatorConfig
		for index := range self.cfg.Operators {
			if self.cfg.Operators[index].NoID == operator.noID {
				config = &self.cfg.Operators[index]
				break
			}
		}
		if config == nil || !common.IsHexAddress(config.ArtifactSigner) || common.HexToAddress(config.ArtifactSigner) == (common.Address{}) {
			return DepositAudit{}, errors.New("historical payout signer has no configured authority")
		}
		reader, err := NewHTTPArtifactReader(config.APIURL, self.cfg.DeploymentID, self.cfg.Netuid)
		if err != nil {
			return DepositAudit{}, err
		}
		artifact, err := reader.readCommittedReleaseDecisionV2Artifact(ctx, operator.commitment.ArtifactHash, min(self.cfg.EvidenceV2.Bounds.MaxArtifactBytes, maximumPayoutArtifactBytes))
		if err != nil {
			return DepositAudit{}, errors.Join(errors.New("historical committed payout bytes are unavailable or invalid; current failure is not past outage evidence"), err)
		}
		audit = EvaluateDepositArtifact(artifact, DepositArtifactExpectation{DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: self.cfg.GenesisHash, Netuid: self.cfg.Netuid, Coordinator: common.HexToAddress(self.cfg.Coordinator), SettlementVault: common.HexToAddress(self.cfg.SettlementVault), PolicyHash: self.cfg.PolicyHash, Epoch: observed.sourceEpoch, NoID: operator.noID, Signer: common.HexToAddress(config.ArtifactSigner), Start: payoutartifact.Boundary{Number: observed.sourceStart, Hash: releaseHex32(observed.sourceStartHash)}, End: payoutartifact.Boundary{Number: observed.sourceEnd, Hash: releaseHex32(observed.sourceEndHash)}, PayoutRoot: operator.commitment.PayoutRoot, ArtifactHash: operator.commitment.ArtifactHash, Committer: operator.commitment.Committer, RootSigner: operator.sourceVersion.RootSigner, CommitBlock: operator.commitment.CommitBlock}, epoch, operator.deposit, operator.convictionBefore, self.cfg.Policy.Deposit)
	}
	audit.ObservedAtBlock, audit.ArtifactDeadlineBlock = observed.boundary.EVMBlock, observed.artifactDeadline
	return audit, ctx.Err()
}

// Exact content has an independent commitment and a finite caller allowance.
// Join the actual response body's Close, including cancellation at EOF; never
// route a late failure through the historical unavailable-audit branch.
func (self *HTTPArtifactReader) readCommittedReleaseDecisionV2Artifact(ctx context.Context, hash [32]byte, maximum uint64) (result *payoutartifact.Artifact, resultErr error) {
	if ctx == nil || self == nil || self.baseURL == nil || self.client == nil || hash == ([32]byte{}) || maximum == 0 || maximum > maximumPayoutArtifactBytes {
		return nil, errors.New("historical committed artifact reader or bound is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	contentHash := fmt.Sprintf("sha256:%x", hash)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, self.endpoint("/sn/artifact", url.Values{"hash": []string{contentHash}}), nil)
	if err != nil {
		return nil, err
	}
	response, err := self.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, response.Body.Close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if response.StatusCode != http.StatusOK || response.ContentLength > int64(maximum) || strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])) != "application/json" {
		return nil, errors.New("historical committed artifact response has invalid status, media type or size")
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, int64(maximum)+1))
	if err != nil || uint64(len(encoded)) > maximum {
		return nil, errors.Join(errors.New("historical committed artifact exceeds its exact byte allowance"), err)
	}
	artifact, err := payoutartifact.Decode(encoded)
	if err != nil || artifact == nil || artifact.ContentHash != contentHash {
		return nil, errors.Join(errors.New("historical payout content differs from its independently committed digest"), err)
	}
	return artifact, ctx.Err()
}
