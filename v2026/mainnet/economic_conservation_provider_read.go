// The existing isolated entitlement worker acquires each independently selected
// original component. The parent alone resolves retained predecessors and
// publishes a census, so an unavailable provider cannot park sibling observers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// The same existing RPC owner reads exact original boundary code and ABI bytes;
// semantic projection is delegated to the complete canonical binding decoder.
func readEconomicProviderBindings(ctx context.Context, reader *monitorEvmReader, census *economicConservationEntitlementCensus, authority *payoutartifact.WholeWorkAuthority) (*validator.ProviderAttemptBindingOriginal, error) {
	ids := make([][16]byte, len(authority.ExpectedProviders))
	for index, provider := range authority.ExpectedProviders {
		ids[index] = provider.ClientId
	}
	expected := economicProviderBindingExpectation(census, authority.Domain, ids)
	result := &validator.ProviderAttemptBindingOriginal{Domain: expected.Domain, Epoch: expected.Epoch, StartBlock: expected.StartBlock, StartHash: expected.StartHash, EndBlock: expected.EndBlock, EndHash: expected.EndHash, CoordinatorRuntimeHash: expected.CoordinatorRuntimeHash, ClientIds: ids}
	for _, point := range []struct {
		boundary payoutartifact.Boundary
		rows     *[][]byte
	}{{boundary: census.Start, rows: &result.StartResponses}, {boundary: census.End, rows: &result.EndResponses}} {
		header, err := reader.header(ctx, point.boundary.Number)
		if err != nil {
			return nil, err
		}
		if header.Hash().Hex() != point.boundary.Hash {
			return nil, monitorEvmIntegrity("provider binding original epoch boundary changed")
		}
		if err := economicEntitlementCode(ctx, reader, point.boundary.Hash, reader.policy.Address, census.CoordinatorCodeHash); err != nil {
			return nil, err
		}
		for _, id := range ids {
			input, err := reader.contract.Pack("bindingAt", id, new(big.Int).SetUint64(census.Artifact.Epoch))
			if err != nil {
				return nil, err
			}
			var encoded string
			if err := reader.read(ctx, "eth_call", []any{map[string]any{"to": reader.policy.Address, "data": hexutil.Encode(input)}, map[string]any{"blockHash": point.boundary.Hash, "requireCanonical": true}}, &encoded); err != nil {
				return nil, err
			}
			raw, err := hexutil.Decode(encoded)
			if err != nil || len(raw) != 11*32 {
				return nil, errors.Join(errRpcIntegrity, err, errors.New("provider binding original ABI frame differs"))
			}
			*point.rows = append(*point.rows, raw)
		}
	}
	if _, _, err := validator.VerifyProviderAttemptBindings(ctx, result, expected); err != nil {
		return nil, err
	}
	return result, ctx.Err()
}

// Consumer-selected receipt/header pins are not imported from supplied binding
// responses. Independent consensus coverage is checked separately at summary.
func economicProviderBindingExpectation(census *economicConservationEntitlementCensus, domain protocol.ClientKeyHistoryDomain, ids [][16]byte) validator.ProviderAttemptBindingExpectation {
	return validator.ProviderAttemptBindingExpectation{Domain: economicProviderAttemptDomain(domain), Epoch: census.Artifact.Epoch, StartBlock: census.Start.Number, StartHash: common.HexToHash(census.Start.Hash), EndBlock: census.End.Number, EndHash: common.HexToHash(census.End.Hash), CoordinatorRuntimeHash: common.HexToHash(census.CoordinatorCodeHash), ClientIds: ids, MaxProviders: maximumEconomicEntitlementProviders}
}

// Raw evidence carries no publication authority. In particular a signed claim
// about prior contracts must still resolve through the parent's retained state.
func readEconomicProviderOriginals(ctx context.Context, source economicConservationEntitlementSource, census *economicConservationEntitlementCensus, reader *monitorEvmReader) (*economicProviderOriginals, *validator.VerifiedProviderAttemptMeasurement, error) {
	policy := source.ProviderMeasurements
	if policy == nil {
		return nil, nil, nil
	}
	workReader, err := validator.NewHttpWholeWorkInventoryReader(source.Endpoint, source.DeploymentId, census.Artifact.Netuid)
	if err != nil {
		return nil, nil, err
	}
	defer workReader.CloseIdleConnections()
	expected, err := policy.workExpectation(&census.Artifact, census.RootSigner, nil)
	if err != nil {
		return nil, nil, economicProviderEvidenceError(err)
	}
	work, err := workReader.ReadOriginal(ctx, &census.Artifact, expected)
	if err != nil {
		return nil, nil, economicProviderEvidenceError(err)
	}
	authority, err := payoutartifact.DecodeWholeWorkAuthority(ctx, work.Authority, expected.AuthoritySigner)
	if err != nil {
		return nil, nil, economicProviderEvidenceError(err)
	}
	if authority.ExpectedProviders == nil || len(authority.ExpectedProviders) > maximumEconomicEntitlementProviders {
		return nil, nil, payoutartifact.ErrClosedWorkUnavailable
	}
	remaining := policy.MaximumOriginalBytes
	charge := func(value any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		used := uint64(len(raw)) + 256
		if used > remaining {
			return errMonitorEconomicCapacity
		}
		remaining -= used
		return nil
	}
	if err := charge(work); err != nil {
		return nil, nil, err
	}
	result := &economicProviderOriginals{Work: work, Wallets: make([]economicProviderWalletOriginal, 0, len(authority.ExpectedProviders))}
	walletReader, err := validator.NewHttpWalletMappingReader(policy.WalletEndpoint)
	if err != nil {
		return nil, nil, err
	}
	defer walletReader.CloseIdleConnections()
	// the networks whose consent some provider falls back to: its own chain is
	// absent, or verified with no consent effective at the epoch
	fallbackNetworkIds := map[[16]byte]bool{}
	for _, member := range authority.ExpectedProviders {
		expected, err := economicProviderWalletExpected(&authority, member)
		if errors.Is(err, protocol.ErrWalletMappingAbsent) {
			fallbackNetworkIds[member.NetworkId] = true
			continue
		}
		if err != nil {
			return nil, nil, economicProviderEvidenceError(err)
		}
		originals, verified, err := walletReader.ReadBounded(ctx, expected, remaining)
		if errors.Is(err, protocol.ErrWalletMappingNotEffective) {
			fallbackNetworkIds[member.NetworkId] = true
		} else if err != nil {
			return nil, nil, economicProviderEvidenceError(err)
		} else if verified.Statement.NetworkId != member.NetworkId {
			return nil, nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
		value := economicProviderWalletOriginal{ClientId: member.ClientId, Originals: originals}
		if err := charge(value); err != nil {
			return nil, nil, err
		}
		result.Wallets = append(result.Wallets, value)
	}
	networkIds := make([][16]byte, 0, len(fallbackNetworkIds))
	for networkId := range fallbackNetworkIds {
		networkIds = append(networkIds, networkId)
	}
	slices.SortFunc(networkIds, func(a [16]byte, b [16]byte) int {
		return bytes.Compare(a[:], b[:])
	})
	for _, networkId := range networkIds {
		expected, err := economicNetworkWalletExpected(&authority, networkId)
		if err != nil {
			return nil, nil, economicProviderEvidenceError(err)
		}
		originals, _, err := walletReader.ReadNetworkBounded(ctx, expected, remaining)
		if err != nil {
			return nil, nil, economicProviderEvidenceError(err)
		}
		value := economicNetworkWalletOriginal{NetworkId: networkId, Originals: originals}
		if err := charge(value); err != nil {
			return nil, nil, err
		}
		result.NetworkWallets = append(result.NetworkWallets, value)
	}
	result.Bindings, err = readEconomicProviderBindings(ctx, reader, census, &authority)
	if err != nil {
		return nil, nil, economicProviderEvidenceError(err)
	}
	if err := charge(result.Bindings); err != nil {
		return nil, nil, err
	}
	attempts, err := policy.openAttemptSource(ctx)
	if err != nil {
		return nil, nil, economicProviderEvidenceError(err)
	}
	var verified *validator.VerifiedProviderAttemptMeasurement
	result.Attempts, verified, err = attempts.ReadBounded(ctx, &census.Artifact, remaining)
	if err != nil {
		return nil, nil, economicProviderEvidenceError(err)
	}
	if verified == nil || verified.VerifiedProviderAttemptWindow == nil || !verified.CutCensusComplete || !verified.OwnedRequestsComplete {
		return nil, nil, protocol.ErrProviderAttemptsUnavailable
	}
	if err := charge(result.Attempts); err != nil {
		return nil, nil, err
	}
	if err := result.bounded(policy.MaximumOriginalBytes); err != nil {
		return nil, nil, err
	}
	return result, verified, ctx.Err()
}

// Unavailable original authority is retryable observation, not a contradicted
// financial fact. Capacity and known contradictions retain their typed causes.
func economicProviderEvidenceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || errors.Is(err, protocol.ErrWalletMappingIntegrity) {
		return errors.Join(errRpcIntegrity, err)
	}
	if errors.Is(err, payoutartifact.ErrClosedWorkCapacity) || errors.Is(err, protocol.ErrProviderAttemptsCapacity) || errors.Is(err, protocol.ErrWalletMappingCapacity) {
		return errors.Join(errMonitorEconomicCapacity, err)
	}
	return err
}
