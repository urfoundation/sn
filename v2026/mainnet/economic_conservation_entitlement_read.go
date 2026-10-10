// The acquisition path uses complete signed transaction and receipt tries for
// both coordinator events. All getters and code reads use exact original block
// hashes; it never infers authorization from the artifact's recovered signer.
package main

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urfoundation/sn/v2026/validator"
)

// Extra receipt events are visible only after block() has verified the entire
// original receipt root. Ordinary economic readers leave this collector nil.
type economicEntitlementReceiptObserver struct {
	address  common.Address
	contract *abi.ABI
	events   []monitorEconomicEvmEvent
}

func economicEntitlementCode(ctx context.Context, reader *monitorEvmReader, hash, address, expected string) error {
	var value string
	if err := reader.read(ctx, "eth_getCode", []any{address, map[string]any{"blockHash": hash, "requireCanonical": true}}, &value); err != nil {
		return err
	}
	raw, err := hexutil.Decode(value)
	if err != nil || len(raw) == 0 || crypto.Keccak256Hash(raw).Hex() != expected {
		return monitorEvmIntegrity("entitlement coordinator code differs from original reviewed purpose")
	}
	return nil
}

func economicEntitlementScalar(ctx context.Context, reader *monitorEvmReader, hash, method string, args ...any) (any, error) {
	values, err := reader.getter(ctx, hash, method, args...)
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, monitorEvmIntegrity("entitlement getter arity differs")
	}
	return values[0], nil
}

// The artifact stays unsigned-input data until both complete receipt censuses
// and the exact commitment state agree with its reconstructed canonical bytes.
func readEconomicEntitlementCensus(ctx context.Context, policy economicConservationPolicy, source economicConservationEntitlementSource, record economicConservationEntitlement, client *rpcClient) (*economicConservationEntitlementCensus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if record.Finalization == nil || record.Total == nil {
		return nil, monitorEvmIntegrity("entitlement has no original observed finalization")
	}
	epoch, e1 := monitorEconomicInteger(record.Epoch)
	pool, e2 := monitorEconomicInteger(record.PoolId)
	if e1 != nil || e2 != nil || !epoch.IsUint64() || !pool.IsUint64() {
		return nil, monitorEvmIntegrity("entitlement epoch or pool exceeds original artifact grammar")
	}
	contract, err := monitorEvmAbi(policy.Vault.ContractKind)
	if err != nil {
		return nil, err
	}
	coordinator, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	reader := &monitorEvmReader{client: client, policy: policy.Vault, contract: contract, entitlementObserver: &economicEntitlementReceiptObserver{address: common.HexToAddress(source.Coordinator), contract: coordinator}}
	finalHeader, err := reader.header(ctx, record.Finalization.Block.Number)
	if err != nil {
		return nil, err
	}
	if finalHeader.Hash().Hex() != record.Finalization.Block.Hash {
		return nil, monitorEvmIntegrity("entitlement finalization canonical identity changed")
	}
	if err := economicEntitlementCode(ctx, reader, finalHeader.Hash().Hex(), source.Coordinator, source.CoordinatorCodeHash); err != nil {
		return nil, err
	}
	finalBlock, err := reader.block(ctx, finalHeader)
	if err != nil {
		return nil, err
	}
	finalMatches, coordinatorMatches := 0, 0
	var coordinatorFinal monitorEconomicEvmEvent
	for _, event := range finalBlock.Events {
		if rootObjectHash(event) == rootObjectHash(*record.Finalization) {
			finalMatches++
		}
	}
	for _, event := range reader.entitlementObserver.events {
		if event.Name == "OperatorEpochFinalized" && event.Values["epoch"] == record.Epoch && event.Values["noId"] == record.PoolId && event.TransactionHash == record.Finalization.TransactionHash && event.Values["rootPresent"] == "true" {
			coordinatorMatches++
			coordinatorFinal = event
		}
	}
	if finalMatches != 1 || coordinatorMatches != 1 || finalBlock.Funded[record.Id] != record.Funded {
		return nil, monitorEvmIntegrity("entitlement finalization receipt or original funding differs")
	}
	coordinatorReader := &monitorEvmReader{client: client, policy: policy.Vault, contract: coordinator, used: reader.used}
	coordinatorReader.policy.Address = source.Coordinator
	values, err := coordinatorReader.getter(ctx, finalHeader.Hash().Hex(), "rootCommitments", epoch, pool)
	if err != nil {
		return nil, err
	}
	if len(values) != 4 {
		return nil, monitorEvmIntegrity("entitlement commitment getter arity differs")
	}
	commitRoot, ok1 := values[0].([32]byte)
	artifactHash, ok2 := values[1].([32]byte)
	committer, ok3 := values[2].(common.Address)
	commitNumber, ok4 := values[3].(uint64)
	if !ok1 || !ok2 || !ok3 || !ok4 || commitNumber == 0 || commitNumber > finalHeader.Number.Uint64() || common.Hash(commitRoot).Hex() != record.PayoutRoot || common.Hash(artifactHash).Hex() != record.ArtifactHash || committer == (common.Address{}) {
		return nil, monitorEvmIntegrity("entitlement commitment differs from exact finalized root")
	}
	reader.used = coordinatorReader.used
	commitHeader, err := reader.header(ctx, commitNumber)
	if err != nil {
		return nil, err
	}
	if err := economicEntitlementCode(ctx, reader, commitHeader.Hash().Hex(), source.Coordinator, source.CoordinatorCodeHash); err != nil {
		return nil, err
	}
	reader.entitlementObserver.events = nil
	commitBlock, err := reader.block(ctx, commitHeader)
	if err != nil {
		return nil, err
	}
	var commitment monitorEconomicEvmEvent
	commitMatches := 0
	for _, event := range reader.entitlementObserver.events {
		if event.Name == "OperatorRootCommitted" && event.Values["epoch"] == record.Epoch && event.Values["noId"] == record.PoolId {
			commitMatches++
			commitment = event
		}
	}
	if commitMatches != 1 || commitment.Values["payoutRoot"] != record.PayoutRoot || commitment.Values["artifactHash"] != record.ArtifactHash || commitment.Values["committer"] != committer.Hex() {
		return nil, monitorEvmIntegrity("entitlement authorized commitment receipt differs")
	}
	coordinatorReader.used = reader.used
	opValue, err := economicEntitlementScalar(ctx, coordinatorReader, commitHeader.Hash().Hex(), "operatorAt", pool, epoch)
	if err != nil {
		return nil, err
	}
	op := *abi.ConvertType(opValue, new(stabi.STCoordinatorOperatorVersion)).(*stabi.STCoordinatorOperatorVersion)
	if !op.Active || op.RootSigner != committer || op.EffectiveEpoch > epoch.Uint64() {
		return nil, monitorEvmIntegrity("entitlement commitment was not made by original epoch authority")
	}
	policyValue, err := economicEntitlementScalar(ctx, coordinatorReader, commitHeader.Hash().Hex(), "policyAt", epoch)
	if err != nil {
		return nil, err
	}
	epochPolicy := *abi.ConvertType(policyValue, new(stabi.STCoordinatorPolicySnapshot)).(*stabi.STCoordinatorPolicySnapshot)
	vaultValue, err := economicEntitlementScalar(ctx, coordinatorReader, commitHeader.Hash().Hex(), "settlementVault")
	if err != nil {
		return nil, err
	}
	if vaultAddress, ok := vaultValue.(common.Address); !ok || vaultAddress != common.HexToAddress(policy.Vault.Address) {
		return nil, monitorEvmIntegrity("entitlement coordinator belongs to another vault")
	}
	windowClock := &payoutartifact.ClosedWorkWindowClock{}
	boundary := func(method string) (payoutartifact.Boundary, error) {
		value, err := economicEntitlementScalar(ctx, coordinatorReader, commitHeader.Hash().Hex(), method, epoch)
		if err != nil {
			return payoutartifact.Boundary{}, err
		}
		n, ok := value.(*big.Int)
		if !ok || n == nil || !n.IsUint64() || n.Uint64() > commitNumber {
			return payoutartifact.Boundary{}, monitorEvmIntegrity("entitlement original epoch boundary is unavailable")
		}
		header, err := coordinatorReader.header(ctx, n.Uint64())
		if err != nil {
			return payoutartifact.Boundary{}, err
		}
		observed := payoutartifact.Boundary{Number: n.Uint64(), Hash: header.Hash().Hex()}
		if header.Time > math.MaxInt64 {
			return payoutartifact.Boundary{}, monitorEvmIntegrity("entitlement epoch clock exceeds original timestamp domain")
		}
		raw, err := rlp.EncodeToBytes(header)
		if err != nil {
			return payoutartifact.Boundary{}, err
		}
		if method == "epochStartBlock" {
			windowClock.Start, windowClock.StartTime, windowClock.StartHeader = observed, time.Unix(int64(header.Time), 0).UTC(), raw
		} else {
			windowClock.End, windowClock.EndTime, windowClock.EndHeader = observed, time.Unix(int64(header.Time), 0).UTC(), raw
		}
		return observed, nil
	}
	start, err := boundary("epochStartBlock")
	if err != nil {
		return nil, err
	}
	end, err := boundary("epochEndBlock")
	if err != nil {
		return nil, err
	}
	if end.Number < start.Number || epochPolicy.RootCommitWindowBlocks == 0 || commitNumber-end.Number > epochPolicy.RootCommitWindowBlocks {
		return nil, monitorEvmIntegrity("entitlement commitment lies outside original epoch window")
	}
	artifactReader, err := validator.NewHTTPArtifactReader(source.Endpoint, source.DeploymentId, policy.Vault.Netuid)
	if err != nil {
		return nil, err
	}
	defer artifactReader.CloseIdleConnections()
	var artifact *payoutartifact.Artifact
	if source.ProviderMeasurements == nil {
		artifact, err = artifactReader.ReadProviderCensus(ctx, epoch.Uint64(), pool.Uint64(), maximumEconomicEntitlementProviders)
	} else {
		artifact, err = artifactReader.ReadProviderOriginalCensus(ctx, epoch.Uint64(), pool.Uint64(), maximumEconomicEntitlementProviders)
	}
	if err != nil {
		if errors.Is(err, validator.ErrArtifactCapacity) || errors.Is(err, payoutartifact.ErrClosedWorkCapacity) {
			return nil, errMonitorEconomicCapacity
		}
		if validator.ArtifactObservationPending(err) || ctx.Err() != nil && monitorOnlyCancellationCauses(err, 0) {
			return nil, err
		}
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if artifact == nil {
		return nil, monitorEvmIntegrity("economic original artifact reader returned no complete artifact")
	}
	result := &economicConservationEntitlementCensus{Schema: economicEntitlementCensusSchema, Entitlement: record.Id, Finalization: *record.Finalization, Commitment: commitment, CoordinatorFinalization: coordinatorFinal, CoordinatorCodeHash: source.CoordinatorCodeHash, CommitmentReceiptsRoot: commitBlock.ReceiptsRoot, FinalizationReceiptsRoot: finalBlock.ReceiptsRoot, RootSigner: committer.Hex(), OperatorColdkey: common.Hash(op.Coldkey).Hex(), OperatorEffectiveEpoch: op.EffectiveEpoch, PolicyHash: common.Hash(epochPolicy.PolicyHash).Hex(), Start: start, End: end, FundingHash: economicEntitlementFundingHash(record), Artifact: *artifact}
	// Decode already verifies the whole canonical original. These preliminary
	// pins keep an equal-total artifact from another root out of the amount path.
	if artifact.ContentHash != "sha256:"+strings.TrimPrefix(record.ArtifactHash, "0x") || common.Hash(artifact.PayoutRoot).Hex() != record.PayoutRoot {
		return nil, monitorEvmIntegrity("entitlement original artifact content differs from authorized commitment")
	}
	total, err := monitorEconomicInteger(*record.Total)
	if err != nil {
		return nil, err
	}
	allocated := new(big.Int)
	for _, leaf := range artifact.Leaves {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		allocated.Add(allocated, new(big.Int).Quo(new(big.Int).Mul(total, new(big.Int).SetUint64(leaf.ShareBPS)), big.NewInt(10000)))
	}
	result.LeafObligationsAlpha = allocated.String()
	result.FloorResidueAlpha = new(big.Int).Sub(total, allocated).String()
	if source.ProviderMeasurements == nil {
		result.ClosedWork, err = readEconomicClosedWork(ctx, artifact, committer, windowClock)
		if result.ClosedWork != nil && result.ClosedWork.WindowHash != "" {
			result.WindowClock = windowClock
		}
		if err != nil {
			return nil, economicEntitlementEvidenceError(err)
		}
	}
	result.ProviderOriginals, result.providerAttempts, err = readEconomicProviderOriginals(ctx, source, result, coordinatorReader)
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	result.ContentHash = result.hash()
	if err := result.validate(ctx, policy, record); err != nil {
		return nil, economicEntitlementEvidenceError(err)
	}
	// Canonicality is observed again after HTTP acquisition without advancing
	// either domain cursor or substituting a later state for the original one.
	again, err := coordinatorReader.header(ctx, finalHeader.Number.Uint64())
	if err != nil {
		return nil, err
	}
	if again.Hash() != finalHeader.Hash() {
		return nil, monitorEvmIntegrity("entitlement finalization changed during original artifact acquisition")
	}
	return result, ctx.Err()
}
