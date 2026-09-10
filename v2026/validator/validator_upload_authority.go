// Staging admission discovers existing activation anchors, then authenticates
// their executable domain and historical native eligibility. It is not a
// migration-prefix, EMA, ordinary measurement, or settlement truth verifier.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Independently approved immutable deployment/runtime pins, not a validator
// allowlist. Epoch, original operator, hotkey and VPK come from an anchored
// record and pass the historical readers before they can name a quota owner.
type ValidatorUploadDeployment struct {
	ChainID           uint64                       `json:"chain_id" yaml:"chain_id"`
	GenesisHash       [32]byte                     `json:"genesis_hash" yaml:"genesis_hash"`
	Netuid            uint16                       `json:"netuid" yaml:"netuid"`
	Coordinator       [20]byte                     `json:"coordinator" yaml:"coordinator"`
	SettlementVault   [20]byte                     `json:"settlement_vault" yaml:"settlement_vault"`
	DeploymentIDHash  [32]byte                     `json:"deployment_id_hash" yaml:"deployment_id_hash"`
	Journal           [20]byte                     `json:"journal" yaml:"journal"`
	RuntimeHash       [32]byte                     `json:"runtime_hash" yaml:"runtime_hash"`
	DeploymentBlock   uint64                       `json:"deployment_block" yaml:"deployment_block"`
	NativeRuntime     crv4.RuntimeArtifactIdentity `json:"native_runtime" yaml:"native_runtime"`
	MaximumSubnetUIDs uint32                       `json:"maximum_subnet_uids" yaml:"maximum_subnet_uids"`
}

// All transport/lookup limits are independent of a discovered activation.
func (self ValidatorUploadDeployment) Validate() error {
	if self.ChainID == 0 || self.GenesisHash == ([32]byte{}) || self.Netuid == 0 || self.Coordinator == ([20]byte{}) || self.SettlementVault == ([20]byte{}) ||
		self.Journal == ([20]byte{}) || self.Coordinator == self.SettlementVault || self.Journal == self.Coordinator || self.Journal == self.SettlementVault ||
		self.DeploymentIDHash == ([32]byte{}) || self.RuntimeHash == ([32]byte{}) || self.DeploymentBlock == 0 || self.DeploymentBlock > math.MaxInt64 ||
		self.MaximumSubnetUIDs == 0 || self.MaximumSubnetUIDs > math.MaxUint16 {
		return errors.New("validator staging deployment or historical census bounds are incomplete")
	}
	if self.NativeRuntime.Version.SpecName == "" || len(self.NativeRuntime.Version.SpecName) > 128 || self.NativeRuntime.Version.SpecVersion == 0 ||
		self.NativeRuntime.Version.TransactionVersion == 0 {
		return errors.New("validator staging native runtime is incomplete")
	}
	for _, value := range []string{self.NativeRuntime.CodeHash, self.NativeRuntime.MetadataHash} {
		if _, err := canonicalAttemptHex32("validator staging runtime", value, false); err != nil {
			return err
		}
	}
	return nil
}

// This timestamp is chain data, not the time a stalled endpoint answered.
// Cache admission compares it with its independently supplied freshness limit.
type ValidatorUploadObserver struct {
	Number    uint64
	Hash      [32]byte
	Timestamp uint64
}

// Native freshness uses Timestamp.Now from authenticated metadata at the
// actual finalized hash, never the unrelated EVM height or dial-time metadata.
type ValidatorUploadNativeObserver struct {
	Number          uint64
	Hash            types.Hash
	TimestampMillis uint64
}

// The fixed eight-byte native timestamp is retained only after a final
// canonical height/hash check. The schedule reader separately proves permit.
func ValidatorUploadNativeObserverContext(ctx context.Context, native *crv4.Chain, deployment ValidatorUploadDeployment) (result ValidatorUploadNativeObserver, resultErr error) {
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil {
		return result, errors.New("validator staging native observer is unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ValidatorUploadNativeObserver{}
		}
	}()
	if err := errors.Join(ctx.Err(), deployment.Validate()); err != nil {
		return result, err
	}
	if native.GenesisHash != types.Hash(deployment.GenesisHash) {
		return result, errors.New("validator staging native genesis differs")
	}
	hash, err := crv4.FinalizedHeadContext(ctx, native)
	if err != nil {
		return result, err
	}
	header, err := native.HeaderAtContext(ctx, hash)
	if err != nil {
		return result, err
	}
	if header == nil || header.Number == 0 {
		return result, errors.New("validator staging finalized native header is absent")
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, native, hash, deployment.NativeRuntime)
	if err != nil {
		return result, err
	}
	key, err := types.CreateStorageKey(artifact.Metadata, "Timestamp", "Now")
	if err != nil {
		return result, err
	}
	var raw json.RawMessage
	if err := native.API.Client.CallContext(ctx, &raw, "state_getStorage", key.Hex(), hash.Hex()); err != nil {
		return result, err
	}
	if len(raw) != 20 || raw[0] != '"' || raw[1] != '0' || raw[2] != 'x' || raw[19] != '"' {
		return result, errors.New("validator staging native timestamp is not an exact u64 storage value")
	}
	var encoded [8]byte
	if _, err := hex.Decode(encoded[:], raw[3:19]); err != nil {
		return result, err
	}
	if !bytes.Equal(raw[3:19], []byte(hex.EncodeToString(encoded[:]))) {
		return result, errors.New("validator staging native timestamp encoding is noncanonical")
	}
	timestamp := binary.LittleEndian.Uint64(encoded[:])
	if timestamp == 0 || timestamp > math.MaxInt64 {
		return result, errors.New("validator staging native timestamp exceeds its bound")
	}
	var canonical string
	if err := native.API.Client.CallContext(ctx, &canonical, "chain_getBlockHash", uint64(header.Number)); err != nil {
		return result, err
	}
	if canonical != hash.Hex() {
		return result, errors.New("validator staging native finalized hash changed")
	}
	return ValidatorUploadNativeObserver{Number: uint64(header.Number), Hash: hash, TimestampMillis: timestamp}, nil
}

// Reads one complete finalized header through the bounded real RPC transport.
// Pointer fields distinguish missing/null values from numeric zero.
func (self *ChainClient) ValidatorUploadObserverContext(ctx context.Context) (result ValidatorUploadObserver, resultErr error) {
	if ctx == nil || self == nil || self.client == nil {
		return result, errors.New("validator staging observer is unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ValidatorUploadObserver{}
		}
	}()
	if err := errors.Join(ctx.Err(), self.requireRelease()); err != nil {
		return result, err
	}
	var header *struct {
		Number    *hexutil.Uint64 `json:"number"`
		Hash      *common.Hash    `json:"hash"`
		Timestamp *hexutil.Uint64 `json:"timestamp"`
	}
	callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	if err := self.client.Client().CallContext(callCtx, &header, "eth_getBlockByNumber", "finalized", false); err != nil {
		return result, err
	}
	if err := callCtx.Err(); err != nil {
		return result, err
	}
	if header == nil || header.Number == nil || header.Hash == nil || header.Timestamp == nil || *header.Number == 0 || *header.Number > math.MaxInt64 ||
		*header.Hash == (common.Hash{}) || *header.Timestamp == 0 || *header.Timestamp > math.MaxInt64 {
		return result, errors.New("validator staging finalized header is incomplete")
	}
	return ValidatorUploadObserver{Number: uint64(*header.Number), Hash: [32]byte(*header.Hash), Timestamp: uint64(*header.Timestamp)}, nil
}

// A contract event is discovery only. Complete canonical record readback and
// native eligibility remain necessary before the digest can reserve anything.
type ValidatorUploadActivationEvent struct {
	Digest    [32]byte
	Hotkey    [32]byte
	NoID      uint64
	Epoch     uint64
	Block     uint64
	BlockHash [32]byte
	Index     uint64
}

// Hash-to-number caching authenticates identity, not continued canonicality.
// These staging boundaries always consult the actual numbered RPC header.
func (self *ChainClient) recheckValidatorUploadBlockContext(ctx context.Context, block uint64, expected [32]byte) error {
	actual, err := self.BlockHashContext(ctx, block)
	if err != nil || actual != expected {
		return errors.Join(errors.New("validator staging canonical EVM boundary changed"), err)
	}
	return ctx.Err()
}

// Every range is explicit and finite; the shared HTTP transport bounds the
// JSON allocation before this narrower event-count check. No provider limit
// or silently truncated page is treated as a complete discovery census.
func (self *ChainClient) ValidatorUploadActivationEventsContext(ctx context.Context, deployment ValidatorUploadDeployment, from, to, maximumBlocks, maximumEvents uint64, observer ValidatorUploadObserver) (result []ValidatorUploadActivationEvent, resultErr error) {
	if ctx == nil || self == nil || self.client == nil || self.chainId == nil {
		return nil, errors.New("validator staging event reader is unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := errors.Join(ctx.Err(), deployment.Validate(), self.requireRelease()); err != nil {
		return nil, err
	}
	if self.contractAddr != common.Address(deployment.Coordinator) || self.chainId.Cmp(new(big.Int).SetUint64(deployment.ChainID)) != 0 ||
		maximumBlocks == 0 || maximumBlocks > math.MaxInt64 || maximumEvents == 0 || maximumEvents > 65536 || from < deployment.DeploymentBlock || to < from || to-from >= maximumBlocks ||
		to > observer.Number || observer.Hash == ([32]byte{}) || observer.Number > math.MaxInt64 {
		return nil, errors.New("validator staging discovery range exceeds its approved domain or bounds")
	}
	if err := self.recheckValidatorUploadBlockContext(ctx, observer.Number, observer.Hash); err != nil {
		return nil, err
	}
	topic := crypto.Keccak256Hash([]byte("ActivationPublished(bytes32,bytes32,uint64,uint64)"))
	var rows []struct {
		Address     *common.Address `json:"address"`
		Topics      []common.Hash   `json:"topics"`
		Data        hexutil.Bytes   `json:"data"`
		BlockNumber *hexutil.Uint64 `json:"blockNumber"`
		BlockHash   *common.Hash    `json:"blockHash"`
		LogIndex    *hexutil.Uint64 `json:"logIndex"`
		Removed     *bool           `json:"removed"`
	}
	callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	filter := map[string]any{"address": common.Address(deployment.Journal), "fromBlock": hexutil.EncodeUint64(from), "toBlock": hexutil.EncodeUint64(to), "topics": []common.Hash{topic}}
	if err := self.client.Client().CallContext(callCtx, &rows, "eth_getLogs", filter); err != nil {
		return nil, err
	}
	if err := callCtx.Err(); err != nil {
		return nil, err
	}
	// JSON null is not an authenticated empty range. Geth leaves a nonnil
	// empty array nonnil, while null and a missing result are both refused.
	if rows == nil || uint64(len(rows)) > maximumEvents {
		return nil, errors.New("validator staging event result is absent or exceeds its bound")
	}
	result = make([]ValidatorUploadActivationEvent, 0, len(rows))
	seen := make(map[[32]byte]bool, len(rows))
	for index, row := range rows {
		if row.Address == nil || *row.Address != common.Address(deployment.Journal) || len(row.Topics) != 4 || row.Topics[0] != topic ||
			row.Topics[1] == (common.Hash{}) || row.Topics[2] == (common.Hash{}) || len(row.Data) != 32 || row.BlockNumber == nil || row.BlockHash == nil ||
			row.LogIndex == nil || row.Removed == nil || *row.Removed || *row.BlockHash == (common.Hash{}) || uint64(*row.BlockNumber) < from || uint64(*row.BlockNumber) > to {
			return nil, fmt.Errorf("validator staging event %d is incomplete, removed or outside its range", index)
		}
		noID, epoch := new(big.Int).SetBytes(row.Topics[3][:]), new(big.Int).SetBytes(row.Data)
		if !noID.IsUint64() || noID.Sign() == 0 || !epoch.IsUint64() {
			return nil, errors.New("validator staging event has noncanonical uint64 padding")
		}
		event := ValidatorUploadActivationEvent{Digest: [32]byte(row.Topics[1]), Hotkey: [32]byte(row.Topics[2]), NoID: noID.Uint64(), Epoch: epoch.Uint64(), Block: uint64(*row.BlockNumber), BlockHash: [32]byte(*row.BlockHash), Index: uint64(*row.LogIndex)}
		if seen[event.Digest] {
			return nil, errors.New("validator staging event digest is duplicated")
		}
		if index != 0 {
			prior := result[index-1]
			if event.Block < prior.Block || event.Block == prior.Block && (event.Index <= prior.Index || event.BlockHash != prior.BlockHash) {
				return nil, errors.New("validator staging events are not a strict canonical order")
			}
		}
		if index == 0 || event.Block != result[index-1].Block {
			if err := self.recheckValidatorUploadBlockContext(ctx, event.Block, event.BlockHash); err != nil {
				return nil, err
			}
		}
		seen[event.Digest] = true
		result = append(result, event)
	}
	if err := self.recheckValidatorUploadBlockContext(ctx, observer.Number, observer.Hash); err != nil {
		return nil, err
	}
	return result, nil
}

// Anchored consent plus exact native eligibility is sufficient to allocate
// staging, not to bless the record's migration prefix. The original approved
// runtime, not a current compiler build or record self-description, establishes
// that publishActivation checked both signatures before writing this digest.
func (self *ChainClient) AuthenticateValidatorUploadActivationContext(ctx context.Context, native *crv4.Chain, deployment ValidatorUploadDeployment, digest [32]byte, observer ValidatorUploadObserver) (result VerifiedReleaseActivationV2, resultErr error) {
	if ctx == nil || self == nil || self.client == nil || native == nil {
		return result, errors.New("validator staging activation owners are unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = VerifiedReleaseActivationV2{}
		}
	}()
	if err := errors.Join(ctx.Err(), deployment.Validate()); err != nil {
		return result, err
	}
	if digest == ([32]byte{}) || observer.Number < deployment.DeploymentBlock || observer.Hash == ([32]byte{}) {
		return result, errors.New("validator staging activation discovery is incomplete")
	}
	contract := stabi.NewSTValidatorEvidence()
	outputs, err := self.batchCallsAtHashContext(ctx, observer.Number, observer.Hash, []chainBatchCall{{address: common.Address(deployment.Journal), calldata: contract.PackActivation(digest)}})
	if err != nil {
		return result, err
	}
	if len(outputs) != 1 || len(outputs[0]) != 18*32 {
		return result, errors.New("validator staging activation has an invalid ABI width")
	}
	stored, err := contract.UnpackActivation(outputs[0])
	if err != nil {
		return result, err
	}
	if err := validatorEvidenceCanonicalOutput("activation", outputs[0], stored); err != nil {
		return result, err
	}
	if stored.PublishedBlock == 0 {
		if !bytes.Equal(outputs[0], make([]byte, 18*32)) {
			return result, errors.New("validator staging unpublished activation contains state")
		}
		return result, ErrValidatorEvidenceAbsent
	}
	record := stored.Record.ProtocolRecord()
	actual, err := record.Digest()
	if err != nil {
		return result, err
	}
	domain := record.Domain
	if actual != digest || domain.ChainID != deployment.ChainID || domain.GenesisHash != deployment.GenesisHash || domain.Netuid != deployment.Netuid ||
		domain.Coordinator != deployment.Coordinator || domain.SettlementVault != deployment.SettlementVault || domain.DeploymentIDHash != deployment.DeploymentIDHash ||
		stored.PublishedBlock < deployment.DeploymentBlock || stored.PublishedBlock <= record.EVMBlock || stored.PublishedBlock > observer.Number {
		return result, errors.New("validator staging activation conflicts with independent deployment or discovery digest")
	}
	publication, err := self.readReleaseActivationV2EVMContext(ctx, ReleaseActivationV2Authority{Expected: record, Journal: common.Address(deployment.Journal), RuntimeHash: deployment.RuntimeHash}, observer.Number, observer.Hash)
	if err != nil {
		return result, err
	}
	schedule, err := crv4.ReadValidatorScheduleAtContext(ctx, native, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(domain.GenesisHash), BlockHash: types.Hash(record.NativeHash),
		BlockNumber: record.NativeBlock, Netuid: domain.Netuid, Hotkey: record.Hotkey, MaximumSubnetUIDs: deployment.MaximumSubnetUIDs}, deployment.NativeRuntime)
	if err != nil {
		return result, err
	}
	if !schedule.Stake.MeetsNonSelfStakeAndPermit() {
		return result, errors.New("validator staging historical hotkey lacks non-self stake and permit")
	}
	if publication.Record != record || publication.PublishedBlock != stored.PublishedBlock {
		return result, errors.New("validator staging activation changed during authentication")
	}
	if err := self.recheckValidatorUploadBlockContext(ctx, observer.Number, observer.Hash); err != nil {
		return result, err
	}
	return VerifiedReleaseActivationV2{Publication: publication, Native: schedule.Stake, ObservedEVMBlock: observer.Number, ObservedEVMHash: observer.Hash}, nil
}
