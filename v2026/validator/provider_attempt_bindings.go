// Original coordinator responses prove head exclusion independently of an
// operator's artifact rows. Both exact window boundaries retain complete ABI
// bytes; missing transport or one malformed row never becomes inactive zero.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// These independent deployment/window anchors are selected before any reads.
// Finality/native mapping of the anchors belongs to the containing chain owner.
type ProviderAttemptBindingExpectation struct {
	Domain                 protocol.ProviderAttemptDomain
	Epoch                  uint64
	StartBlock             uint64
	StartHash              [32]byte
	EndBlock               uint64
	EndHash                [32]byte
	CoordinatorRuntimeHash [32]byte
	ClientIds              [][16]byte
	MaxProviders           uint64
}

// Original response bytes remain available for exact custody and later replay.
// The commitment is meaningful only with the separate retained chain anchors.
type ProviderAttemptBindingOriginal struct {
	Domain                 protocol.ProviderAttemptDomain `json:"domain"`
	Epoch                  uint64                         `json:"epoch"`
	StartBlock             uint64                         `json:"start_block"`
	StartHash              [32]byte                       `json:"start_hash"`
	EndBlock               uint64                         `json:"end_block"`
	EndHash                [32]byte                       `json:"end_hash"`
	CoordinatorRuntimeHash [32]byte                       `json:"coordinator_runtime_hash"`
	ClientIds              [][16]byte                     `json:"client_ids"`
	StartResponses         [][]byte                       `json:"start_responses"`
	EndResponses           [][]byte                       `json:"end_responses"`
}

// No wallet/network attribution is inferred from a coordinator binding. An
// inactive original is a proven head-exclusion zero, not a missing provider.
type VerifiedProviderAttemptBinding struct {
	ClientId          [16]byte                         `json:"client_id"`
	HeadExcluded      bool                             `json:"head_excluded"`
	BindingGeneration uint64                           `json:"binding_generation"`
	Record            stabi.STCoordinatorBindingRecord `json:"record"`
}

// Admit a canonical complete expected client union before acquiring responses.
func ownProviderAttemptBindingExpectation(ctx context.Context, expected ProviderAttemptBindingExpectation) (ProviderAttemptBindingExpectation, error) {
	if ctx == nil {
		return ProviderAttemptBindingExpectation{}, errors.New("provider binding owner context is absent")
	}
	if err := ctx.Err(); err != nil {
		return ProviderAttemptBindingExpectation{}, err
	}
	if err := expected.Domain.Validate(); err != nil {
		return ProviderAttemptBindingExpectation{}, err
	}
	if expected.MaxProviders == 0 || uint64(len(expected.ClientIds)) > expected.MaxProviders {
		return ProviderAttemptBindingExpectation{}, protocol.ErrProviderAttemptsCapacity
	}
	if expected.StartBlock == 0 || expected.StartHash == ([32]byte{}) || expected.EndBlock <= expected.StartBlock || expected.EndHash == ([32]byte{}) || expected.CoordinatorRuntimeHash == ([32]byte{}) {
		return ProviderAttemptBindingExpectation{}, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding exact window authority is incomplete"))
	}
	for index, id := range expected.ClientIds {
		if err := ctx.Err(); err != nil {
			return ProviderAttemptBindingExpectation{}, err
		}
		if id == ([16]byte{}) || index > 0 && bytes.Compare(expected.ClientIds[index-1][:], id[:]) >= 0 {
			return ProviderAttemptBindingExpectation{}, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding expected client union is not unique and ordered"))
		}
	}
	expected.ClientIds = slices.Clone(expected.ClientIds)
	return expected, nil
}

// The live reader uses original hash-pinned calls at both immutable boundaries,
// including exact executable code. No candidate-supplied callbacks waive reads.
func (self *ChainClient) ReadProviderAttemptBindings(ctx context.Context, supplied ProviderAttemptBindingExpectation) (result *ProviderAttemptBindingOriginal, resultErr error) {
	expected, err := ownProviderAttemptBindingExpectation(ctx, supplied)
	if err != nil {
		return nil, err
	}
	if self == nil || self.client == nil || self.chainId == nil || self.coordinator == nil || self.contractAddr != common.Address(expected.Domain.Coordinator) || self.chainId.Cmp(new(big.Int).SetUint64(expected.Domain.ChainId)) != 0 {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding reader belongs to another chain owner"))
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, chainReadOperationKey{}, true)
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	result = &ProviderAttemptBindingOriginal{Domain: expected.Domain, Epoch: expected.Epoch, StartBlock: expected.StartBlock, StartHash: expected.StartHash, EndBlock: expected.EndBlock, EndHash: expected.EndHash, CoordinatorRuntimeHash: expected.CoordinatorRuntimeHash, ClientIds: expected.ClientIds}
	for _, point := range []struct {
		block       uint64
		hash        [32]byte
		destination *[][]byte
	}{{block: expected.StartBlock, hash: expected.StartHash, destination: &result.StartResponses}, {block: expected.EndBlock, hash: expected.EndHash, destination: &result.EndResponses}} {
		if err := self.validateBlockIdentityContext(ctx, point.block, point.hash); err != nil {
			return nil, err
		}
		selector, err := chainBlockHashSelector(point.block, point.hash)
		if err != nil {
			return nil, err
		}
		var code hexutil.Bytes
		if err := self.retryChainRead(ctx, func(readCtx context.Context) error {
			code = nil
			return self.client.Client().CallContext(readCtx, &code, "eth_getCode", self.contractAddr, selector)
		}); err != nil {
			return nil, err
		}
		if len(code) == 0 || len(code) > 24*1024 || [32]byte(crypto.Keccak256Hash(code)) != expected.CoordinatorRuntimeHash {
			return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding coordinator executable differs"))
		}
		calls := make([]chainBatchCall, len(expected.ClientIds))
		for index, id := range expected.ClientIds {
			calls[index] = chainBatchCall{address: self.contractAddr, calldata: self.coordinator.PackBindingAt(id, new(big.Int).SetUint64(expected.Epoch))}
		}
		responses, err := self.batchCallsAtHashContext(ctx, point.block, point.hash, calls)
		if err != nil {
			return nil, err
		}
		*point.destination = responses
	}
	if _, _, err := VerifyProviderAttemptBindings(ctx, result, expected); err != nil {
		return nil, err
	}
	return result, nil
}

// Reconstruct every expected row from original ABI bytes. The caller authenticates
// the retained response custody against its own captured canonical chain owner;
// this decoder alone does not turn arbitrary supplied RPC bytes into finality.
func VerifyProviderAttemptBindings(ctx context.Context, original *ProviderAttemptBindingOriginal, supplied ProviderAttemptBindingExpectation) (result []VerifiedProviderAttemptBinding, commitment [32]byte, resultErr error) {
	expected, err := ownProviderAttemptBindingExpectation(ctx, supplied)
	if err != nil {
		return nil, commitment, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result, commitment = nil, [32]byte{}
		}
	}()
	if original == nil {
		return nil, commitment, protocol.ErrProviderAttemptsUnavailable
	}
	if original.Domain != expected.Domain || original.Epoch != expected.Epoch || original.StartBlock != expected.StartBlock || original.StartHash != expected.StartHash || original.EndBlock != expected.EndBlock || original.EndHash != expected.EndHash || original.CoordinatorRuntimeHash != expected.CoordinatorRuntimeHash || !slices.Equal(original.ClientIds, expected.ClientIds) || len(original.StartResponses) != len(expected.ClientIds) || len(original.EndResponses) != len(expected.ClientIds) {
		return nil, commitment, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding original census or window differs"))
	}
	coordinator := stabi.NewSTCoordinator()
	contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, commitment, err
	}
	result = make([]VerifiedProviderAttemptBinding, len(expected.ClientIds))
	for index, id := range expected.ClientIds {
		if err := ctx.Err(); err != nil {
			return nil, commitment, err
		}
		var states [2]stabi.BindingAtOutput
		for position, raw := range [][]byte{original.StartResponses[index], original.EndResponses[index]} {
			if len(raw) != 11*32 {
				return nil, commitment, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding original ABI length differs"))
			}
			state, err := coordinator.UnpackBindingAt(raw)
			if err != nil {
				return nil, commitment, errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
			}
			canonical, err := contractAbi.Methods["bindingAt"].Outputs.Pack(state.Active, state.Record)
			if err != nil || !bytes.Equal(raw, canonical) {
				return nil, commitment, errors.Join(protocol.ErrProviderAttemptsIntegrity, err, errors.New("provider binding original ABI is not canonical"))
			}
			if state.Active {
				r := state.Record
				if r.FleetId == ([32]byte{}) || r.Hotkey == ([32]byte{}) || r.ClientKey == ([32]byte{}) || r.CommitmentHash == ([32]byte{}) || r.Generation == 0 || r.ValidFromEpoch > expected.Epoch || r.ValidToEpoch < expected.Epoch || r.Cleaned && (r.CleanedAtEpoch == 0 || r.CleanedAtEpoch <= expected.Epoch) || !r.Cleaned && r.CleanedAtEpoch != 0 {
					return nil, commitment, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding active original is impossible"))
				}
			}
			states[position] = state
		}
		start, end := states[0], states[1]
		if start.Active && end.Active {
			left, right := start.Record, end.Record
			left.Cleaned, left.CleanedAtEpoch, right.Cleaned, right.CleanedAtEpoch = false, 0, false, 0
			if left != right {
				return nil, commitment, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider binding has contradictory active original boundaries"))
			}
		}
		selected := end
		if start.Active {
			selected = start
		}
		result[index] = VerifiedProviderAttemptBinding{ClientId: id, HeadExcluded: start.Active || end.Active, Record: selected.Record}
		if result[index].HeadExcluded {
			result[index].BindingGeneration = selected.Record.Generation
		}
	}
	raw, err := json.Marshal(original)
	if err != nil {
		return nil, commitment, err
	}
	return result, sha256.Sum256(raw), nil
}
