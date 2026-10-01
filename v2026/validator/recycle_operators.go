//go:build linux || darwin

// Operator admission joins real compact proof replay to the production
// canonical coordinator reader. Active registry state is not API health,
// historical key custody or permission to sign the proposed successor.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

const ownerRecycleOperatorEvidenceSchema = "urnetwork-owner-recycle-operator-evidence-v1"

// These are exact coordinator facts. Decimal strings retain uint256 values;
// no observed status claims successful HTTP delivery or operator independence.
type OwnerRecycleOperatorState struct {
	NoId                uint64                             `json:"no_id"`
	Current             stabi.STCoordinatorOperatorVersion `json:"current"`
	Source              stabi.STCoordinatorOperatorVersion `json:"source"`
	SourceCommitment    stabi.RootCommitmentsOutput        `json:"source_commitment"`
	DepositRao          string                             `json:"deposit_rao"`
	ConvictionAddedRao  string                             `json:"conviction_added_rao"`
	ConvictionRao       string                             `json:"conviction_rao"`
	ConvictionBeforeRao string                             `json:"conviction_before_rao"`
}

// The current native and EVM hashes remain distinct independently selected
// boundaries. This record does not infer Frontier mapping from equal heights.
type OwnerRecycleOperatorEvidence struct {
	Schema                  string                            `json:"schema"`
	ApprovalHash            string                            `json:"approval_hash"`
	ProviderMeasurementHash string                            `json:"provider_measurement_hash"`
	Decision                ReleaseMeasurementV2Decision      `json:"decision"`
	Policy                  stabi.STCoordinatorPolicySnapshot `json:"policy"`
	EpochStart              uint64                            `json:"epoch_start"`
	ArtifactDeadline        uint64                            `json:"artifact_deadline"`
	SourceEpoch             uint64                            `json:"source_epoch"`
	SourceStart             uint64                            `json:"source_start"`
	SourceStartHash         [32]byte                          `json:"source_start_hash"`
	SourceEnd               uint64                            `json:"source_end"`
	SourceEndHash           [32]byte                          `json:"source_end_hash"`
	Operators               []OwnerRecycleOperatorState       `json:"operators"`
}

// Upgrades only this exact immutable measurement authority after actual V2
// replay and coordinator reads. Callers own the immutable connection lifecycle;
// the returned owner is detached and the input authority is never modified.
// Fresh retries preserve the original hashes; no historical outage is inferred.
func ObserveOwnerRecycleMeasurementOperators(ctx context.Context, authority *OwnerRecycleMeasurementAuthority, chain *ChainClient, providerBytes []byte, options ReleaseMeasurementV2Options) (result *OwnerRecycleMeasurementAuthority, resultErr error) {
	limit, err := ownerRecycleMeasurementLimit(ctx, authority, options)
	if err != nil {
		return nil, err
	}
	if chain == nil || !slices.Contains(authority.config.RPC, chain.rpcUrl) || chain.rpcUrl == "" || authority.expected.ChainID != 964 {
		return nil, errors.New("owner-recycle operator reader requires its independently approved mainnet route")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if uint64(len(providerBytes))+uint64(len(authority.approval))+uint64(len(authority.census)) > limit {
		return nil, errors.New("owner-recycle operator inputs exceed the existing measurement allowance")
	}
	providerBytes = bytes.Clone(providerBytes)
	// Capture the real connection before proof-reader callbacks. The domain's
	// activation hash comes from replay authority, never a fabricated sentinel.
	if len(options.Operators) == 0 || len(authority.config.Operators) == 0 {
		return nil, errors.New("owner-recycle operator proof census is absent")
	}
	first := options.Operators[authority.config.Operators[0].NoID]
	domain := first.Expected.Activation.Domain
	if domain.ChainID != authority.expected.ChainID || domain.Netuid != authority.expected.Netuid ||
		releaseHex32(domain.GenesisHash) != authority.expected.GenesisHash ||
		common.Address(domain.Coordinator) != common.HexToAddress(authority.expected.Coordinator) ||
		common.Address(domain.SettlementVault) != common.HexToAddress(authority.expected.SettlementVault) ||
		releaseHex32(domain.PolicyHash) != authority.expected.PolicyHash {
		return nil, errors.New("owner-recycle operator activation and approved decision domains differ")
	}
	ownedChain, err := chain.ownReleaseDecisionChainV2Client(domain)
	if err != nil {
		return nil, err
	}
	ownedChain.readRetryHooks = chain.readRetryHooks
	// The cached chain id from dialing is not authority after proxy retargeting.
	checkRoute := func() error {
		return ownedChain.retryChainRead(ctx, func(callCtx context.Context) error {
			chainId, err := ownedChain.client.ChainID(callCtx)
			if err != nil {
				return err
			}
			var genesis common.Hash
			if err := ownedChain.client.Client().CallContext(callCtx, &genesis, "chain_getBlockHash", uint64(0)); err != nil {
				return err
			}
			if chainId == nil || chainId.Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 || genesis != common.Hash(domain.GenesisHash) {
				return errors.New("owner-recycle operator RPC changed mainnet chain or native genesis")
			}
			return callCtx.Err()
		})
	}
	if err := checkRoute(); err != nil {
		return nil, err
	}
	artifact, verified, err := DecodeReleaseMeasurementArtifactV2(ctx, providerBytes, options)
	if err != nil {
		return nil, err
	}
	if _, err := deriveOwnerRecycleMeasuredRow(authority, artifact, verified.Decision); err != nil {
		return nil, err
	}
	query := releaseDecisionChainV2Query{domain: domain, policy: authority.config.Policy,
		boundary:     AttemptBoundary{SettlementEpoch: artifact.SettlementEpoch, EVMBlock: artifact.EVMSnapshotBlock, EVMBlockHash: artifact.EVMSnapshotHash},
		maxOperators: options.MaxOperators, maxControlBytes: options.MaxControlBytes}
	for _, input := range artifact.Inputs {
		operator := releaseDecisionChainV2OperatorQuery{noID: input.NoID}
		// The full verifier already bounded and authenticated this membership.
		for _, provider := range input.Stats.Providers {
			id, err := connect.ParseId(provider.ClientID)
			if err != nil || id.String() != provider.ClientID {
				return nil, errors.Join(errors.New("owner-recycle provider identity is not canonical"), err)
			}
			operator.providerIDs = append(operator.providerIDs, id)
		}
		sort.Slice(operator.providerIDs, func(i, j int) bool { return operator.providerIDs[i].LessThan(operator.providerIDs[j]) })
		query.operators = append(query.operators, operator)
		query.maxProviders = max(query.maxProviders, uint64(len(operator.providerIDs)))
	}
	query.maxProviders = max(query.maxProviders, 1)
	observed, err := ownedChain.readReleaseDecisionChainV2Context(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(observed.hotkeyUIDs) != len(authority.observation.Snapshot.Registrations) {
		return nil, errors.New("owner-recycle native and decision EVM registration censuses differ")
	}
	for _, registration := range authority.observation.Snapshot.Registrations {
		if uid, found := observed.hotkeyUIDs[registration.Hotkey]; !found || uid != registration.Uid {
			return nil, errors.New("owner-recycle native and decision EVM hotkey/UID mappings differ")
		}
	}
	if len(artifact.Bindings) != len(observed.bindings) || !slices.Equal(artifact.Pools, observed.pools) || len(artifact.DepositAudits) != len(observed.operators) {
		return nil, errors.New("owner-recycle measured operator, binding, pool or deposit census differs from chain")
	}
	for index, actual := range observed.bindings {
		claimed := artifact.Bindings[index]
		// Local API observations still require their own retained key authority.
		claimed.LocalClientKey, claimed.ClientKeyObservationHash = releaseHex32([32]byte{}), ""
		if claimed != actual {
			return nil, errors.New("owner-recycle measured provider binding differs from decision chain state")
		}
	}
	evidence := OwnerRecycleOperatorEvidence{Schema: ownerRecycleOperatorEvidenceSchema, ApprovalHash: authority.observation.ApprovalHash,
		ProviderMeasurementHash: ReleaseMeasurementContentHash(providerBytes), Decision: authority.expected,
		Policy: observed.policy, EpochStart: observed.epochStart, ArtifactDeadline: observed.artifactDeadline,
		SourceEpoch: observed.sourceEpoch, SourceStart: observed.sourceStart, SourceStartHash: observed.sourceStartHash,
		SourceEnd: observed.sourceEnd, SourceEndHash: observed.sourceEndHash}
	for index, operator := range observed.operators {
		if !operator.version.Active {
			return nil, fmt.Errorf("owner-recycle operator %d was inactive at the decision", operator.noID)
		}
		claimed := artifact.DepositAudits[index]
		if claimed.NoID != operator.noID || claimed.Epoch != artifact.SettlementEpoch || claimed.SourceEpoch != observed.sourceEpoch ||
			claimed.ObservedAtBlock != observed.boundary.EVMBlock || claimed.ArtifactDeadlineBlock != observed.artifactDeadline ||
			claimed.ObservedDepositRao != operator.deposit.String() || claimed.ConvictionBeforeRao != operator.convictionBefore.String() {
			return nil, errors.New("owner-recycle deposit audit differs from exact decision amounts or window")
		}
		// Positive and priced mismatch audits carry a complete commitment.
		// Internal self-consistency of those claims is not on-chain authority.
		// Failure-only audits still need a later historical outage/key owner.
		if claimed.Status == DepositAuditCompliant || claimed.Status == DepositAuditMismatch && artifact.SettlementEpoch >= artifact.Policy.Deposit.UsageLagEpochs {
			if claimed.CommittedArtifactHash != releaseHex32(operator.commitment.ArtifactHash) || claimed.PayoutRoot != releaseHex32(operator.commitment.PayoutRoot) ||
				claimed.RootCommitter != strings.ToLower(operator.commitment.Committer.Hex()) || claimed.RootSigner != strings.ToLower(operator.sourceVersion.RootSigner.Hex()) ||
				claimed.RootCommitBlock != operator.commitment.CommitBlock || claimed.SourceStartBlock != observed.sourceStart || claimed.SourceEndBlock != observed.sourceEnd ||
				claimed.SourceStartHash != releaseHex32(observed.sourceStartHash) || claimed.SourceEndHash != releaseHex32(observed.sourceEndHash) {
				return nil, errors.New("owner-recycle payout audit differs from its canonical source commitment or boundaries")
			}
		}
		evidence.Operators = append(evidence.Operators, OwnerRecycleOperatorState{NoId: operator.noID,
			Current: operator.version, Source: operator.sourceVersion, SourceCommitment: operator.commitment,
			DepositRao: operator.deposit.String(), ConvictionAddedRao: operator.convictionAdded.String(),
			ConvictionRao: operator.conviction.String(), ConvictionBeforeRao: operator.convictionBefore.String()})
	}
	if err := checkRoute(); err != nil {
		return nil, err
	}
	// A route check may itself retarget a proxy. Recheck the exact decision
	// header after it, without using the client's immutable pairing cache.
	hash, err := ownedChain.BlockHashContext(ctx, artifact.EVMSnapshotBlock)
	if err != nil {
		return nil, err
	}
	if releaseHex32(hash) != artifact.EVMSnapshotHash {
		return nil, errors.New("owner-recycle operator canonical decision changed before retention")
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if uint64(len(encoded))+uint64(len(providerBytes))+uint64(len(authority.approval))+uint64(len(authority.census)) > limit {
		return nil, errors.New("owner-recycle operator evidence exceeds the complete measurement allowance")
	}
	owned := *authority
	owned.operatorEvidence, owned.operatorProviderHash = encoded, evidence.ProviderMeasurementHash
	return &owned, ctx.Err()
}
