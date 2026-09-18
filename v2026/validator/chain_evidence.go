// Read immutable validator evidence through the real release chain client.
// Every view and code lookup uses one canonical EVM hash. Callers select a
// finalized hash and separately authenticate native eligibility and proof bytes;
// this reader never converts a matching commitment into a truth assertion.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Absence is retryable by a publisher; an RPC failure or conflicting record is
// not absence and must never authorize a replacement transaction.
var ErrValidatorEvidenceAbsent = errors.New("validator evidence is not published at the captured block")

// The inclusion height comes from the authenticated contract state, not a
// caller-supplied receipt. Finalized transaction/log evidence is captured too
// by the campaign; this value alone is not that complete evidence record.
type ValidatorEvidenceActivationPublication struct {
	Record         protocol.ValidatorEvidenceActivation
	PublishedBlock uint64
}

// A stable slot retains exactly one header even when another relayer retries.
type ValidatorEvidencePublication struct {
	Header         protocol.ValidatorEvidenceHeader
	PublishedBlock uint64
}

// Authenticate the approved companion, its executable identity, the one-time
// coordinator anchor and every immutable domain field before decoding records.
// Calls are fixed-size and share one batch, with no current-state fallback.
func (self *ChainClient) validatorEvidenceViewsAtHashContext(ctx context.Context, journal common.Address, runtimeHash [32]byte, domain protocol.ValidatorEvidenceActivationDomain, block uint64, blockHash [32]byte, recordCalls ...[]byte) ([][]byte, error) {
	if ctx == nil || self == nil || self.client == nil || self.chainId == nil || len(recordCalls) == 0 || len(recordCalls) > 2 {
		return nil, errors.New("validator evidence reader is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	if err := domain.Validate(); err != nil {
		return nil, err
	}
	if self.chainId.Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 || self.contractAddr != common.Address(domain.Coordinator) ||
		journal == (common.Address{}) || journal == self.contractAddr || journal == common.Address(domain.SettlementVault) || runtimeHash == ([32]byte{}) {
		return nil, errors.New("validator evidence deployment differs from the release client")
	}
	if err := self.validateBlockIdentityContext(ctx, block, blockHash); err != nil {
		return nil, err
	}
	selector, err := chainBlockHashSelector(block, blockHash)
	if err != nil {
		return nil, err
	}
	var code hexutil.Bytes
	callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	err = self.client.Client().CallContext(callCtx, &code, "eth_getCode", journal, selector)
	err = errors.Join(err, callCtx.Err())
	cancel()
	if err != nil {
		return nil, fmt.Errorf("validator evidence runtime at canonical block: %w", err)
	}
	if len(code) == 0 || len(code) > 24*1024 || [32]byte(crypto.Keccak256Hash(code)) != runtimeHash {
		return nil, errors.New("validator evidence runtime differs from the approved deployed bytecode")
	}
	contract := stabi.NewSTValidatorEvidence()
	calls := []chainBatchCall{
		{address: self.contractAddr, calldata: self.coordinator.PackValidatorEvidence()},
		{address: journal, calldata: contract.PackCoordinator()},
		{address: journal, calldata: contract.PackSettlementVault()},
		{address: journal, calldata: contract.PackChainId()},
		{address: journal, calldata: contract.PackNetuid()},
		{address: journal, calldata: contract.PackGenesisHash()},
		{address: journal, calldata: contract.PackDeploymentIdHash()},
	}
	for _, calldata := range recordCalls {
		calls = append(calls, chainBatchCall{address: journal, calldata: calldata})
	}
	outputs, err := self.batchCallsAtHashContext(ctx, block, blockHash, calls)
	if err != nil {
		return nil, err
	}
	// Exact expected ABI words also reject noncanonical padding and trailing
	// data that a permissive ABI decoder might otherwise discard.
	expected := [][32]byte{
		[32]byte(common.BytesToHash(journal[:])),
		[32]byte(common.BytesToHash(domain.Coordinator[:])),
		[32]byte(common.BytesToHash(domain.SettlementVault[:])),
		[32]byte(common.BigToHash(new(big.Int).SetUint64(domain.ChainID))),
		[32]byte(common.BigToHash(new(big.Int).SetUint64(uint64(domain.Netuid)))),
		domain.GenesisHash, domain.DeploymentIDHash,
	}
	for index, word := range expected {
		if !bytes.Equal(outputs[index], word[:]) {
			return nil, fmt.Errorf("validator evidence anchor/immutable field %d differs from approved deployment", index)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return outputs[len(expected):], nil
}

// Read back the entire independently expected activation. The two snapshot
// clocks are never ordered by comparing their numerical block heights.
func (self *ChainClient) ValidatorEvidenceActivationAtHashContext(ctx context.Context, journal common.Address, runtimeHash [32]byte, expected protocol.ValidatorEvidenceActivation, block uint64, blockHash [32]byte) (ValidatorEvidenceActivationPublication, error) {
	var zero ValidatorEvidenceActivationPublication
	digest, err := expected.Digest()
	if err != nil {
		return zero, err
	}
	contract := stabi.NewSTValidatorEvidence()
	outputs, err := self.validatorEvidenceViewsAtHashContext(ctx, journal, runtimeHash, expected.Domain, block, blockHash, contract.PackActivation(digest))
	if err != nil {
		return zero, err
	}
	publication, err := decodeValidatorEvidenceActivation(contract, outputs[0], expected, block)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return publication, nil
}

// The generated decoder may ignore high padding bits or trailing values.
// Re-encoding the fixed tuple rejects every alternative byte representation.
func validatorEvidenceCanonicalOutput(method string, data []byte, value any) error {
	contractABI, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		return err
	}
	canonical, err := contractABI.Methods[method].Outputs.Pack(value)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		return errors.New("validator evidence record ABI is noncanonical")
	}
	return nil
}

// A zero publication marker means absent, not an all-zero valid activation.
func decodeValidatorEvidenceActivation(contract *stabi.STValidatorEvidence, data []byte, expected protocol.ValidatorEvidenceActivation, block uint64) (ValidatorEvidenceActivationPublication, error) {
	var zero ValidatorEvidenceActivationPublication
	if len(data) != 18*32 {
		return zero, errors.New("validator evidence activation ABI length differs")
	}
	stored, err := contract.UnpackActivation(data)
	if err != nil {
		return zero, fmt.Errorf("decode validator evidence activation: %w", err)
	}
	if err := validatorEvidenceCanonicalOutput("activation", data, stored); err != nil {
		return zero, err
	}
	if stored.PublishedBlock == 0 {
		if !bytes.Equal(data, make([]byte, 18*32)) {
			return zero, errors.New("unpublished validator evidence activation contains state")
		}
		return zero, ErrValidatorEvidenceAbsent
	}
	if stored.Record.ProtocolRecord() != expected || stored.PublishedBlock <= expected.EVMBlock || stored.PublishedBlock > block {
		return zero, errors.New("validator evidence activation differs from authenticated authority or inclusion window")
	}
	return ValidatorEvidenceActivationPublication{Record: expected, PublishedBlock: stored.PublishedBlock}, nil
}

// Read the stable slot and its earlier activation in one exact-block batch.
// Matching header bytes are not consent verification or public payload replay;
// callers also use the signed protocol verifier and the immutable API objects.
func (self *ChainClient) ValidatorEvidenceAtHashContext(ctx context.Context, journal common.Address, runtimeHash [32]byte, activation protocol.ValidatorEvidenceActivation, expected protocol.ValidatorEvidenceHeader, window protocol.ValidatorEvidenceWindow, block uint64, blockHash [32]byte) (ValidatorEvidencePublication, error) {
	var zero ValidatorEvidencePublication
	domain, err := activation.EvidenceDomain()
	if err != nil {
		return zero, err
	}
	if err := expected.ValidateAt(domain, window); err != nil {
		return zero, err
	}
	if expected.Hotkey != activation.Hotkey || expected.VPK != activation.VPK || expected.NoID != activation.NoID || window.FinalizedBlock != block {
		return zero, errors.New("validator evidence header differs from activation or captured observation")
	}
	slot, err := expected.SlotKey()
	if err != nil {
		return zero, err
	}
	contract := stabi.NewSTValidatorEvidence()
	outputs, err := self.validatorEvidenceViewsAtHashContext(ctx, journal, runtimeHash, activation.Domain, block, blockHash, contract.PackActivation(domain.ActivationHash), contract.PackCommitment(slot))
	if err != nil {
		return zero, err
	}
	prior, activationErr := decodeValidatorEvidenceActivation(contract, outputs[0], activation, block)
	if len(outputs[1]) != 22*32 {
		return zero, errors.New("validator evidence commitment ABI length differs")
	}
	stored, err := contract.UnpackCommitment(outputs[1])
	if err != nil {
		return zero, fmt.Errorf("decode validator evidence commitment: %w", err)
	}
	if err := validatorEvidenceCanonicalOutput("commitment", outputs[1], stored); err != nil {
		return zero, err
	}
	if stored.PublishedBlock == 0 {
		if !bytes.Equal(outputs[1], make([]byte, 22*32)) {
			return zero, errors.New("unpublished validator evidence commitment contains state")
		}
	}
	// Both tuples are checked before absence classification. Stable slots omit
	// activation revisions, so an absent digest cannot hide an occupied slot.
	if activationErr != nil && !errors.Is(activationErr, ErrValidatorEvidenceAbsent) {
		return zero, activationErr
	}
	if stored.PublishedBlock == 0 {
		return zero, ErrValidatorEvidenceAbsent
	}
	if activationErr != nil {
		return zero, errors.New("published validator evidence commitment has no matching activation")
	}
	if stored.Header.ProtocolHeader() != expected || prior.PublishedBlock > expected.BoundaryBlock || stored.PublishedBlock <= expected.BoundaryBlock ||
		stored.PublishedBlock <= window.EndBlock || stored.PublishedBlock > block {
		return zero, errors.New("validator evidence commitment conflicts with expected header or inclusion window")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ValidatorEvidencePublication{Header: expected, PublishedBlock: stored.PublishedBlock}, nil
}
