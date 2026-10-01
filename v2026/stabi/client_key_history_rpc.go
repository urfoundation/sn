// Exact key-history authorities share bounded read batches, not signatures or
// latest-state verdicts. Every member retains its concrete canonical selector.
package stabi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
)

const MaxClientKeyAuthorityRpcBatchMembers = 50

// Only this pre-I/O work admission error permits the caller's one bounded
// smaller-client fallback. Transport, identity and signature failures do not.
var ErrClientKeyAuthorityRpcWork = errors.New("client-key authority batch exceeds its explicit work allowance")

// Each member is supplied by independent configuration and a signed source
// boundary, not a proposed operator root or a caller's preferred current key.
type ClientKeyAuthorityQuery struct {
	Domain   protocol.ClientKeyHistoryDomain
	Boundary protocol.ClientKeyEffectiveBoundary
}

// This is the observed operator version, never an acceptance of a client key.
type ClientKeyAuthorityObservation struct {
	Query    ClientKeyAuthorityQuery
	Operator STCoordinatorOperatorVersion
}

// Http admissions and logical method work are separately bounded and reported.
// The existing transport gate retains its Http request unit and pacing policy.
type ClientKeyAuthorityRpcLimits struct {
	MaximumRequests uint64
	MaximumMethods  uint64
	MaximumBytes    uint64
}

// Counts are routing/capacity information, not chain authority.
type ClientKeyAuthorityRpcWork struct {
	Requests uint64
	Methods  uint64
	Bytes    uint64
}

// Raw Subtensor Evm hashes are explicit; Ethereum header reconstruction is not
// a substitute for the canonical number/hash pair the endpoint actually returns.
type clientKeyAuthorityRpcBlock struct {
	Number *hexutil.Big `json:"number"`
	Hash   common.Hash  `json:"hash"`
}

// A missing/null or numerically malformed header has no identity.
func (self *clientKeyAuthorityRpcBlock) identity() (uint64, [32]byte, error) {
	if self == nil || self.Number == nil || !(*big.Int)(self.Number).IsUint64() || (*big.Int)(self.Number).Sign() <= 0 || self.Hash == (common.Hash{}) {
		return 0, [32]byte{}, errors.New("client-key authority block identity is absent or invalid")
	}
	return (*big.Int)(self.Number).Uint64(), [32]byte(self.Hash), nil
}

// One private synchronous owner enforces the plan at every actual send.
type clientKeyAuthorityRpcOwner struct {
	ctx    context.Context
	client *ethclient.Client
	limits ClientKeyAuthorityRpcLimits
	work   ClientKeyAuthorityRpcWork
}

// Limits never synthesize an implicit provider, history or memory allowance.
func (self ClientKeyAuthorityRpcLimits) validate() error {
	if self.MaximumRequests == 0 || self.MaximumRequests > protocol.MaxClientKeyObservationBatchRpcRequests ||
		self.MaximumMethods == 0 || self.MaximumMethods > protocol.MaxClientKeyObservationBatchRpcMethods ||
		self.MaximumBytes == 0 || self.MaximumBytes > protocol.MaxClientKeyObservationBatchControlBytes {
		return errors.New("client-key authority Rpc limits are incomplete or excessive")
	}
	return nil
}

// Complete domain equality excludes only operator id. Shared immutable and
// policy getters cannot mix chain, genesis, deployment, vault or policy sources.
func planClientKeyAuthorityRpc(queries []ClientKeyAuthorityQuery, limits ClientKeyAuthorityRpcLimits) (ClientKeyAuthorityRpcWork, []protocol.ClientKeyEffectiveBoundary, error) {
	var work ClientKeyAuthorityRpcWork
	if err := limits.validate(); err != nil {
		return work, nil, err
	}
	if len(queries) == 0 || uint64(len(queries)) > limits.MaximumMethods {
		return work, nil, ErrClientKeyAuthorityRpcWork
	}
	width := uint64(2*reflect.TypeFor[ClientKeyAuthorityQuery]().Size() + reflect.TypeFor[ClientKeyAuthorityObservation]().Size() + 512)
	if uint64(len(queries)) > limits.MaximumBytes/width {
		return work, nil, ErrClientKeyAuthorityRpcWork
	}
	work.Bytes = uint64(len(queries)) * width
	shared := queries[0].Domain
	shared.NoID = 0
	queryKVs := make(map[ClientKeyAuthorityQuery]bool, len(queries))
	boundaryKVs := make(map[protocol.ClientKeyEffectiveBoundary]bool, len(queries))
	boundaries := make([]protocol.ClientKeyEffectiveBoundary, 0, len(queries))
	for _, query := range queries {
		if err := errors.Join(query.Domain.Validate(), query.Boundary.Validate()); err != nil {
			return ClientKeyAuthorityRpcWork{}, nil, err
		}
		domain := query.Domain
		domain.NoID = 0
		if domain != shared || queryKVs[query] {
			return ClientKeyAuthorityRpcWork{}, nil, errors.New("client-key authority query repeats or changes its complete deployment")
		}
		queryKVs[query] = true
		if !boundaryKVs[query.Boundary] {
			boundaryKVs[query.Boundary] = true
			boundaries = append(boundaries, query.Boundary)
		}
	}
	count, operators := uint64(len(boundaries)), uint64(len(queries))
	batches := func(methods uint64) uint64 {
		return (methods + MaxClientKeyAuthorityRpcBatchMembers - 1) / MaxClientKeyAuthorityRpcBatchMembers
	}
	work.Methods = 2*(3+count) + 11*count + operators
	work.Requests = 2*batches(3+count) + batches(5*count+operators) + batches(6*count)
	if work.Methods > limits.MaximumMethods || work.Requests > limits.MaximumRequests {
		return ClientKeyAuthorityRpcWork{}, nil, ErrClientKeyAuthorityRpcWork
	}
	return work, boundaries, nil
}

// This pure preflight cannot confer authority. The real reader independently
// recreates the same plan from its copied queries before the first transport.
func PlanClientKeyAuthorityRpc(queries []ClientKeyAuthorityQuery, limits ClientKeyAuthorityRpcLimits) (ClientKeyAuthorityRpcWork, error) {
	work, _, err := planClientKeyAuthorityRpc(queries, limits)
	return work, err
}

// At most fifty real members share one actual Http request. Every member error
// and returned byte is counted before any decoded value can enter authority.
func (self *clientKeyAuthorityRpcOwner) batch(calls []rpc.BatchElem) ([]json.RawMessage, error) {
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	if uint64(len(calls)) > self.limits.MaximumMethods-self.work.Methods {
		return nil, ErrClientKeyAuthorityRpcWork
	}
	outputs := make([]json.RawMessage, len(calls))
	for start := 0; start < len(calls); start += MaxClientKeyAuthorityRpcBatchMembers {
		end := min(start+MaxClientKeyAuthorityRpcBatchMembers, len(calls))
		if self.work.Requests == self.limits.MaximumRequests {
			return nil, ErrClientKeyAuthorityRpcWork
		}
		batch := calls[start:end]
		for index := range batch {
			batch[index].Result = &outputs[start+index]
		}
		self.work.Requests++
		self.work.Methods += uint64(len(batch))
		if err := self.client.Client().BatchCallContext(self.ctx, batch); err != nil {
			return nil, errors.Join(err, self.ctx.Err())
		}
		for index := range batch {
			if err := batch[index].Error; err != nil {
				return nil, err
			}
			size := uint64(len(outputs[start+index]))
			if size == 0 || self.work.Bytes > self.limits.MaximumBytes || size > (self.limits.MaximumBytes-self.work.Bytes)/4 {
				return nil, errors.New("client-key authority Rpc response exceeds its remaining byte allowance")
			}
			self.work.Bytes += 4 * size
		}
	}
	return outputs, self.ctx.Err()
}

// Identity checks are repeated after all views, so no successful shared value
// can conceal a changed native network, finality or canonical numbered block.
func (self *clientKeyAuthorityRpcOwner) witness(domain protocol.ClientKeyHistoryDomain, boundaries []protocol.ClientKeyEffectiveBoundary) error {
	calls := []rpc.BatchElem{
		{Method: "eth_chainId"},
		{Method: "chain_getBlockHash", Args: []any{uint64(0)}},
		{Method: "eth_getBlockByNumber", Args: []any{"finalized", false}},
	}
	for _, boundary := range boundaries {
		calls = append(calls, rpc.BatchElem{Method: "eth_getBlockByNumber", Args: []any{hexutil.EncodeUint64(boundary.Block), false}})
	}
	encoded, err := self.batch(calls)
	if err != nil {
		return err
	}
	var chainId hexutil.Big
	if err := json.Unmarshal(encoded[0], &chainId); err != nil || (*big.Int)(&chainId).Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 {
		return errors.Join(errors.New("client-key authority Rpc chain identity differs"), err)
	}
	var genesis *common.Hash
	if err := json.Unmarshal(encoded[1], &genesis); err != nil || genesis == nil || [32]byte(*genesis) != domain.GenesisHash {
		return errors.Join(errors.New("client-key authority native genesis is unavailable or different"), err)
	}
	var finalized *clientKeyAuthorityRpcBlock
	if err := json.Unmarshal(encoded[2], &finalized); err != nil {
		return err
	}
	finalizedNumber, finalizedHash, err := finalized.identity()
	if err != nil {
		return err
	}
	for index, boundary := range boundaries {
		var header *clientKeyAuthorityRpcBlock
		if err := json.Unmarshal(encoded[3+index], &header); err != nil {
			return err
		}
		number, hash, err := header.identity()
		if err != nil || number != boundary.Block || hash != boundary.Hash || finalizedNumber < boundary.Block || finalizedNumber == boundary.Block && finalizedHash != boundary.Hash {
			return errors.Join(errors.New("client-key authority canonical/finalized boundary differs"), err)
		}
	}
	return self.ctx.Err()
}

// A final operation witness uses bounded real transport members as well. It
// cannot supply an operator root or substitute for any immutable contract view.
func WitnessClientKeyAuthorityBoundariesContext(ctx context.Context, client *ethclient.Client, domain protocol.ClientKeyHistoryDomain, boundaries []protocol.ClientKeyEffectiveBoundary, limits ClientKeyAuthorityRpcLimits) (work ClientKeyAuthorityRpcWork, resultErr error) {
	if ctx == nil || client == nil {
		return work, errors.New("client-key witness has no concrete owner")
	}
	if err := errors.Join(ctx.Err(), domain.Validate(), limits.validate()); err != nil {
		return work, err
	}
	methods := uint64(len(boundaries)) + 3
	requests := (methods + MaxClientKeyAuthorityRpcBatchMembers - 1) / MaxClientKeyAuthorityRpcBatchMembers
	width := uint64(reflect.TypeFor[protocol.ClientKeyEffectiveBoundary]().Size()) + 512
	if len(boundaries) == 0 || methods > limits.MaximumMethods || requests > limits.MaximumRequests || uint64(len(boundaries)) > limits.MaximumBytes/width {
		return work, ErrClientKeyAuthorityRpcWork
	}
	for _, boundary := range boundaries {
		if err := boundary.Validate(); err != nil {
			return work, err
		}
	}
	owner := &clientKeyAuthorityRpcOwner{ctx: ctx, client: client, limits: limits, work: ClientKeyAuthorityRpcWork{Bytes: uint64(len(boundaries)) * width}}
	err := owner.witness(domain, slices.Clone(boundaries))
	return owner.work, errors.Join(err, ctx.Err())
}

// All read selectors preserve the independently supplied hash and explicit
// canonical requirement; no height-only eth_call is constructed.
func clientKeyAuthorityRpcCall(address common.Address, boundary protocol.ClientKeyEffectiveBoundary, data []byte) rpc.BatchElem {
	return rpc.BatchElem{Method: "eth_call", Args: []any{
		map[string]any{"to": address, "data": hexutil.Bytes(data)},
		rpc.BlockNumberOrHashWithHash(common.Hash(boundary.Hash), true),
	}}
}

// Canonical ABI re-encoding rejects trailing, aliased or malformed values.
func decodeClientKeyAuthorityRpcView(method string, encoded json.RawMessage) ([]byte, error) {
	if len(encoded) == 0 || len(encoded) > 2+2*(14*32)+2 {
		return nil, errors.New("client-key authority fixed view exceeds its ABI allowance")
	}
	var raw hexutil.Bytes
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, err
	}
	parsed, err := STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	view, found := parsed.Methods[method]
	if !found {
		return nil, errors.New("client-key authority method has no canonical ABI")
	}
	values, err := view.Outputs.Unpack(raw)
	if err != nil {
		return nil, err
	}
	canonical, err := view.Outputs.Pack(values...)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, errors.Join(errors.New("client-key authority view is not canonical"), err)
	}
	return raw, nil
}

// Returns only after all complete views and the final witness. One failed
// method clears the whole result; callers cannot consume a successful prefix.
func ReadClientKeyAuthoritiesContext(ctx context.Context, client *ethclient.Client, queries []ClientKeyAuthorityQuery, limits ClientKeyAuthorityRpcLimits) (result []ClientKeyAuthorityObservation, work ClientKeyAuthorityRpcWork, resultErr error) {
	if ctx == nil || client == nil {
		return nil, work, errors.New("client-key authority batch has no concrete owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, work, err
	}
	planned, boundaries, err := planClientKeyAuthorityRpc(queries, limits)
	if err != nil {
		return nil, work, err
	}
	queries = slices.Clone(queries)
	owner := &clientKeyAuthorityRpcOwner{ctx: ctx, client: client, limits: limits, work: ClientKeyAuthorityRpcWork{Bytes: planned.Bytes}}
	defer func() {
		work = owner.work
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	domain := queries[0].Domain
	if err := owner.witness(domain, boundaries); err != nil {
		return nil, work, err
	}
	coordinator := NewSTCoordinator()
	calls := make([]rpc.BatchElem, 0, 5*len(boundaries)+len(queries))
	for _, boundary := range boundaries {
		epoch := new(big.Int).SetUint64(boundary.Epoch)
		for _, data := range [][]byte{coordinator.PackCurrentEpoch(), coordinator.PackNetuid(), coordinator.PackSettlementVault(), coordinator.PackPolicyAt(epoch), coordinator.PackValidatorEvidence()} {
			calls = append(calls, clientKeyAuthorityRpcCall(domain.Coordinator, boundary, data))
		}
	}
	for _, query := range queries {
		calls = append(calls, clientKeyAuthorityRpcCall(domain.Coordinator, query.Boundary, coordinator.PackOperatorAt(new(big.Int).SetUint64(query.Domain.NoID), new(big.Int).SetUint64(query.Boundary.Epoch))))
	}
	encoded, err := owner.batch(calls)
	if err != nil {
		return nil, work, err
	}
	anchors := make([]common.Address, len(boundaries))
	for index, boundary := range boundaries {
		methods := []string{"currentEpoch", "netuid", "settlementVault", "policyAt", "validatorEvidence"}
		values := make([][]byte, len(methods))
		for field, method := range methods {
			values[field], err = decodeClientKeyAuthorityRpcView(method, encoded[5*index+field])
			if err != nil {
				return nil, work, err
			}
		}
		epoch, epochErr := coordinator.UnpackCurrentEpoch(values[0])
		netuid, netuidErr := coordinator.UnpackNetuid(values[1])
		vault, vaultErr := coordinator.UnpackSettlementVault(values[2])
		policy, policyErr := coordinator.UnpackPolicyAt(values[3])
		anchor, anchorErr := coordinator.UnpackValidatorEvidence(values[4])
		if err := errors.Join(epochErr, netuidErr, vaultErr, policyErr, anchorErr); err != nil {
			return nil, work, err
		}
		if epoch == nil || !epoch.IsUint64() || epoch.Uint64() != boundary.Epoch || netuid != domain.Netuid || vault != domain.SettlementVault || policy.PolicyHash != domain.PolicyHash || policy.EffectiveBlock > boundary.Block || policy.EffectiveEpoch > boundary.Epoch ||
			anchor == (common.Address{}) || anchor == domain.Coordinator || anchor == domain.SettlementVault {
			return nil, work, errors.New("client-key authority actual coordinator domain or boundary differs")
		}
		anchors[index] = anchor
	}
	result = make([]ClientKeyAuthorityObservation, len(queries))
	for index, query := range queries {
		raw, err := decodeClientKeyAuthorityRpcView("operatorAt", encoded[5*len(boundaries)+index])
		if err != nil {
			return nil, work, err
		}
		operator, err := coordinator.UnpackOperatorAt(raw)
		if err != nil || !operator.Active || operator.RootSigner == (common.Address{}) || operator.EffectiveEpoch > query.Boundary.Epoch {
			return nil, work, errors.Join(errors.New("client-key operator has no real root authority at the boundary"), err)
		}
		result[index] = ClientKeyAuthorityObservation{Query: query, Operator: operator}
	}
	companion := NewSTValidatorEvidence()
	fields := []struct {
		data  []byte
		value [32]byte
	}{
		{data: companion.PackCoordinator(), value: [32]byte(common.BytesToHash(domain.Coordinator[:]))},
		{data: companion.PackSettlementVault(), value: [32]byte(common.BytesToHash(domain.SettlementVault[:]))},
		{data: companion.PackChainId(), value: [32]byte(common.BigToHash(new(big.Int).SetUint64(domain.ChainID)))},
		{data: companion.PackNetuid(), value: [32]byte(common.BigToHash(new(big.Int).SetUint64(uint64(domain.Netuid))))},
		{data: companion.PackGenesisHash(), value: domain.GenesisHash},
		{data: companion.PackDeploymentIdHash(), value: domain.DeploymentIDHash},
	}
	calls = make([]rpc.BatchElem, 0, 6*len(boundaries))
	for index, boundary := range boundaries {
		for _, field := range fields {
			calls = append(calls, clientKeyAuthorityRpcCall(anchors[index], boundary, field.data))
		}
	}
	encoded, err = owner.batch(calls)
	if err != nil {
		return nil, work, err
	}
	for index := range boundaries {
		for fieldIndex, field := range fields {
			var actual hexutil.Bytes
			value := encoded[6*index+fieldIndex]
			if len(value) > 68 {
				return nil, work, errors.New("client-key companion word exceeds its exact ABI allowance")
			}
			if err := json.Unmarshal(value, &actual); err != nil || !bytes.Equal(actual, field.value[:]) {
				return nil, work, errors.Join(errors.New("client-key companion immutable identity differs"), err)
			}
		}
	}
	if err := owner.witness(domain, boundaries); err != nil {
		return nil, work, err
	}
	if owner.work.Requests != planned.Requests || owner.work.Methods != planned.Methods {
		return nil, work, fmt.Errorf("client-key authority executed %d/%d requests/methods, planned %d/%d", owner.work.Requests, owner.work.Methods, planned.Requests, planned.Methods)
	}
	return result, owner.work, nil
}

// Registration discovers one actual finalized boundary before any signature.
// Twenty logical methods use four admitted Http requests, including a complete
// final network/canonical witness; no caller supplies a future/current verdict.
func ReadCurrentClientKeyAuthorityContext(ctx context.Context, client *ethclient.Client, domain protocol.ClientKeyHistoryDomain, limits ClientKeyAuthorityRpcLimits) (result ClientKeyAuthorityObservation, work ClientKeyAuthorityRpcWork, resultErr error) {
	if ctx == nil || client == nil {
		return result, work, errors.New("current client-key authority has no concrete owner")
	}
	if err := errors.Join(ctx.Err(), domain.Validate(), limits.validate()); err != nil {
		return result, work, err
	}
	if limits.MaximumRequests < 4 || limits.MaximumMethods < 20 || limits.MaximumBytes < 16*1024 {
		return result, work, ErrClientKeyAuthorityRpcWork
	}
	owner := &clientKeyAuthorityRpcOwner{ctx: ctx, client: client, limits: limits, work: ClientKeyAuthorityRpcWork{Bytes: 16 * 1024}}
	defer func() {
		work = owner.work
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ClientKeyAuthorityObservation{}
		}
	}()
	encoded, err := owner.batch([]rpc.BatchElem{
		{Method: "eth_chainId"},
		{Method: "chain_getBlockHash", Args: []any{uint64(0)}},
		{Method: "eth_getBlockByNumber", Args: []any{"finalized", false}},
	})
	if err != nil {
		return result, work, err
	}
	var chainId hexutil.Big
	var genesis *common.Hash
	var finalized *clientKeyAuthorityRpcBlock
	if err := errors.Join(json.Unmarshal(encoded[0], &chainId), json.Unmarshal(encoded[1], &genesis), json.Unmarshal(encoded[2], &finalized)); err != nil {
		return result, work, err
	}
	if (*big.Int)(&chainId).Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 || genesis == nil || [32]byte(*genesis) != domain.GenesisHash {
		return result, work, errors.New("current client-key network identity differs")
	}
	block, hash, err := finalized.identity()
	if err != nil {
		return result, work, err
	}
	boundary := protocol.ClientKeyEffectiveBoundary{Block: block, Hash: hash}
	coordinator := NewSTCoordinator()
	calls := []rpc.BatchElem{{Method: "eth_getBlockByNumber", Args: []any{hexutil.EncodeUint64(block), false}}}
	for _, data := range [][]byte{coordinator.PackCurrentEpoch(), coordinator.PackValidatorEvidence(), coordinator.PackNetuid(), coordinator.PackSettlementVault()} {
		calls = append(calls, clientKeyAuthorityRpcCall(domain.Coordinator, boundary, data))
	}
	encoded, err = owner.batch(calls)
	if err != nil {
		return result, work, err
	}
	var canonical *clientKeyAuthorityRpcBlock
	if err := json.Unmarshal(encoded[0], &canonical); err != nil {
		return result, work, err
	}
	actualBlock, actualHash, err := canonical.identity()
	if err != nil || actualBlock != block || actualHash != hash {
		return result, work, errors.Join(errors.New("current client-key finalized and canonical blocks differ"), err)
	}
	methods := []string{"currentEpoch", "validatorEvidence", "netuid", "settlementVault"}
	values := make([][]byte, len(methods))
	for index, method := range methods {
		values[index], err = decodeClientKeyAuthorityRpcView(method, encoded[index+1])
		if err != nil {
			return result, work, err
		}
	}
	epoch, epochErr := coordinator.UnpackCurrentEpoch(values[0])
	anchor, anchorErr := coordinator.UnpackValidatorEvidence(values[1])
	netuid, netuidErr := coordinator.UnpackNetuid(values[2])
	vault, vaultErr := coordinator.UnpackSettlementVault(values[3])
	if err := errors.Join(epochErr, anchorErr, netuidErr, vaultErr); err != nil {
		return result, work, err
	}
	if epoch == nil || !epoch.IsUint64() || anchor == (common.Address{}) || anchor == domain.Coordinator || anchor == domain.SettlementVault || netuid != domain.Netuid || vault != domain.SettlementVault {
		return result, work, errors.New("current client-key coordinator domain is absent or differs")
	}
	boundary.Epoch = epoch.Uint64()
	if err := boundary.Validate(); err != nil {
		return result, work, err
	}
	companion := NewSTValidatorEvidence()
	fields := []struct {
		data     []byte
		expected [32]byte
	}{
		{data: companion.PackCoordinator(), expected: [32]byte(common.BytesToHash(domain.Coordinator[:]))},
		{data: companion.PackSettlementVault(), expected: [32]byte(common.BytesToHash(domain.SettlementVault[:]))},
		{data: companion.PackChainId(), expected: [32]byte(common.BigToHash(new(big.Int).SetUint64(domain.ChainID)))},
		{data: companion.PackNetuid(), expected: [32]byte(common.BigToHash(new(big.Int).SetUint64(uint64(domain.Netuid))))},
		{data: companion.PackGenesisHash(), expected: domain.GenesisHash},
		{data: companion.PackDeploymentIdHash(), expected: domain.DeploymentIDHash},
	}
	calls = []rpc.BatchElem{
		clientKeyAuthorityRpcCall(domain.Coordinator, boundary, coordinator.PackPolicyAt(epoch)),
		clientKeyAuthorityRpcCall(domain.Coordinator, boundary, coordinator.PackOperatorAt(new(big.Int).SetUint64(domain.NoID), epoch)),
	}
	for _, field := range fields {
		calls = append(calls, clientKeyAuthorityRpcCall(anchor, boundary, field.data))
	}
	encoded, err = owner.batch(calls)
	if err != nil {
		return result, work, err
	}
	policyBytes, policyErr := decodeClientKeyAuthorityRpcView("policyAt", encoded[0])
	operatorBytes, operatorErr := decodeClientKeyAuthorityRpcView("operatorAt", encoded[1])
	if err := errors.Join(policyErr, operatorErr); err != nil {
		return result, work, err
	}
	policy, policyErr := coordinator.UnpackPolicyAt(policyBytes)
	operator, operatorErr := coordinator.UnpackOperatorAt(operatorBytes)
	if err := errors.Join(policyErr, operatorErr); err != nil {
		return result, work, err
	}
	if policy.PolicyHash != domain.PolicyHash || policy.EffectiveBlock > block || policy.EffectiveEpoch > boundary.Epoch || !operator.Active || operator.RootSigner == (common.Address{}) || operator.EffectiveEpoch > boundary.Epoch {
		return result, work, errors.New("current client-key policy or operator authority differs")
	}
	for index, field := range fields {
		var value hexutil.Bytes
		if len(encoded[index+2]) > 68 {
			return result, work, errors.New("current client-key companion word is excessive")
		}
		if err := json.Unmarshal(encoded[index+2], &value); err != nil || !bytes.Equal(value, field.expected[:]) {
			return result, work, errors.Join(errors.New("current client-key companion identity differs"), err)
		}
	}
	if err := owner.witness(domain, []protocol.ClientKeyEffectiveBoundary{boundary}); err != nil {
		return result, work, err
	}
	if owner.work.Requests != 4 || owner.work.Methods != 20 {
		return result, work, errors.New("current client-key authority executed work differs from its plan")
	}
	return ClientKeyAuthorityObservation{Query: ClientKeyAuthorityQuery{Domain: domain, Boundary: boundary}, Operator: operator}, owner.work, nil
}
