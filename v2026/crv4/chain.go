package crv4

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	"github.com/centrifuge/go-substrate-rpc-client/v4/registry/retriever"
	registryState "github.com/centrifuge/go-substrate-rpc-client/v4/registry/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic/extensions"
	"golang.org/x/crypto/blake2b"
)

// Pallet / call names pinned against subtensor v3.4.9-424 (identical on
// main@14bc6f9), pallets/subtensor/src/macros/dispatches.rs:
//
//	call_index(113) commit_timelocked_weights(netuid: NetUid(u16),
//	    commit: BoundedVec<u8, 5000>, reveal_round: u64, commit_reveal_version: u16)
//	call_index(118) commit_timelocked_mechanism_weights(netuid, mecid: MechId(u8),
//	    commit, reveal_round, commit_reveal_version)
//
// The legacy commit_crv3_weights (call_index 99) is commented out of the
// runtime and no longer exists.
const (
	PalletName               = "SubtensorModule"
	CallCommitTimelocked     = "commit_timelocked_weights"
	CallCommitTimelockedMech = "commit_timelocked_mechanism_weights"

	// CommitRevealVersion4 is the current CommitRevealWeightsVersion the
	// chain requires (DefaultCommitRevealWeightsVersion = 4,
	// pallets/subtensor/src/lib.rs). do_commit_timelocked_weights rejects
	// commits whose commit_reveal_version does not equal the on-chain value,
	// so prefer reading it live via Chain.CommitRevealVersion.
	CommitRevealVersion4 uint16 = 4
)

// subtensorSignedExtensions registers payload mutators for subtensor's
// custom TxExtension entries so gsrpc's metadata-driven extrinsic
// construction accepts them (runtime/src/lib.rs TxExtension on v3.4.9-424).
// All of them encode zero bytes on the wire except
// ChargeTransactionPaymentWrapper, which wraps the standard
// ChargeTransactionPayment (compact tip). gsrpc resolves the extension name
// from the last path segment of the extension type.
func init() {
	fns := extrinsic.PayloadMutatorFns
	fns[extensions.SignedExtensionName("ChargeTransactionPaymentWrapper")] =
		fns[extensions.ChargeTransactionPaymentSignedExtension]
	for _, name := range []string{
		"SudoTransactionExtension",                                                                                        // runtime/src/sudo_wrapper.rs: PhantomData
		"CheckShieldedTxValidity",                                                                                         // pallets/shield/src/extension.rs: PhantomData
		"SubtensorTransactionExtension" /* pallets/subtensor/src/extensions/subtensor.rs: PhantomData */, "DrandPriority", // pallets/drand/src/drand_priority.rs: PhantomData
	} {
		fns[extensions.SignedExtensionName(name)] = func(payload *extrinsic.Payload) {}
	}
}

// Chain is a subtensor substrate connection with the metadata and chain
// constants needed for CRv4 commits.
type Chain struct {
	API         *gsrpc.SubstrateAPI
	Meta        *types.Metadata
	GenesisHash types.Hash
	Runtime     *types.RuntimeVersion
}

// FinalizedExtrinsic is the canonical receipt for a native Substrate write.
// A submission hash alone is intentionally not treated as success anywhere in
// the release-1.0 code: the containing block and its finalized hash are part of
// every durable intent/artifact.
type FinalizedExtrinsic struct {
	ExtrinsicHash types.Hash
	BlockHash     types.Hash
	BlockNumber   uint64
}

// extrinsicIndex returns the index of hash in the exact block body. Substrate
// extrinsic hashes are Blake2b-256 over the SCALE bytes, not the block's JSON
// string or an Ethereum-style Keccak hash.
func extrinsicIndex(encoded []string, hash types.Hash) (uint32, bool, error) {
	for index, value := range encoded {
		raw, err := codec.HexDecodeString(value)
		if err != nil {
			return 0, false, fmt.Errorf("crv4: decode block extrinsic %d: %w", index, err)
		}
		digest := blake2b.Sum256(raw)
		if types.Hash(digest) == hash {
			return uint32(index), true, nil
		}
	}
	return 0, false, nil
}

// VerifyFinalizedExtrinsic proves both canonical inclusion and successful
// dispatch. A finalized block is not sufficient: Substrate records dispatch
// errors as System.ExtrinsicFailed events while the containing block remains
// perfectly valid and finalized.
func (c *Chain) VerifyFinalizedExtrinsic(blockHash, extrinsicHash types.Hash) error {
	block, err := c.API.RPC.Chain.GetBlock(blockHash)
	if err != nil {
		return fmt.Errorf("crv4: finalized block %s: %w", blockHash.Hex(), err)
	}
	index, found, err := extrinsicIndex(block.Block.Extrinsics, extrinsicHash)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("crv4: extrinsic %s absent from finalized block %s", extrinsicHash.Hex(), blockHash.Hex())
	}
	events, err := retriever.NewDefaultEventRetriever(
		registryState.NewEventProvider(c.API.RPC.State), c.API.RPC.State,
	)
	if err != nil {
		return fmt.Errorf("crv4: initialize finalized event decoder: %w", err)
	}
	records, err := events.GetEvents(blockHash)
	if err != nil {
		return fmt.Errorf("crv4: decode finalized events at %s: %w", blockHash.Hex(), err)
	}
	success := false
	for _, event := range records {
		if event == nil || event.Phase == nil || !event.Phase.IsApplyExtrinsic || event.Phase.AsApplyExtrinsic != index {
			continue
		}
		switch event.Name {
		case "System.ExtrinsicFailed", "ExtrinsicFailed":
			return fmt.Errorf("crv4: extrinsic %s dispatch failed: %s", extrinsicHash.Hex(), formatDecodedEventFields(event.Fields))
		case "System.ExtrinsicSuccess", "ExtrinsicSuccess":
			success = true
		}
	}
	if !success {
		return fmt.Errorf("crv4: extrinsic %s has no System.ExtrinsicSuccess event", extrinsicHash.Hex())
	}
	return nil
}

// Preserve structured dispatch detail instead of rendering nested registry
// pointers, which hides the pallet index and error bytes needed to diagnose a
// failed finalized extrinsic.
func formatDecodedEventFields(fields registry.DecodedFields) string {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return fmt.Sprintf("event fields unavailable: %v", err)
	}
	return string(encoded)
}

// FindFinalizedExtrinsic searches canonical finalized block bodies beginning
// at fromBlock. It is the crash-recovery primitive for an exact previously
// signed extrinsic; callers should persist fromBlock before broadcasting.
func (c *Chain) FindFinalizedExtrinsic(ctx context.Context, extrinsicHash types.Hash, fromBlock uint64) (*FinalizedExtrinsic, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	finalizedHash, err := c.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return nil, false, err
	}
	header, err := c.API.RPC.Chain.GetHeader(finalizedHash)
	if err != nil {
		return nil, false, err
	}
	finalizedNumber := uint64(header.Number)
	if fromBlock > finalizedNumber {
		return nil, false, nil
	}
	for number := fromBlock; ; number++ {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		blockHash, err := c.API.RPC.Chain.GetBlockHash(number)
		if err != nil {
			return nil, false, fmt.Errorf("crv4: block hash %d: %w", number, err)
		}
		block, err := c.API.RPC.Chain.GetBlock(blockHash)
		if err != nil {
			return nil, false, fmt.Errorf("crv4: block %d: %w", number, err)
		}
		_, found, err := extrinsicIndex(block.Block.Extrinsics, extrinsicHash)
		if err != nil {
			return nil, false, err
		}
		if found {
			if err := c.VerifyFinalizedExtrinsic(blockHash, extrinsicHash); err != nil {
				return nil, false, err
			}
			return &FinalizedExtrinsic{ExtrinsicHash: extrinsicHash, BlockHash: blockHash, BlockNumber: number}, true, nil
		}
		if number == finalizedNumber {
			break
		}
	}
	return nil, false, nil
}

// DialChain connects to a substrate websocket endpoint (e.g.
// wss://test.finney.opentensor.ai:443) and loads metadata, genesis hash and
// runtime version.
func DialChain(wsURL string) (*Chain, error) {
	api, err := gsrpc.NewSubstrateAPI(wsURL)
	if err != nil {
		return nil, fmt.Errorf("crv4: dial %s: %w", wsURL, err)
	}
	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		return nil, fmt.Errorf("crv4: metadata: %w", err)
	}
	genesis, err := api.RPC.Chain.GetBlockHash(0)
	if err != nil {
		return nil, fmt.Errorf("crv4: genesis hash: %w", err)
	}
	rv, err := api.RPC.State.GetRuntimeVersionLatest()
	if err != nil {
		return nil, fmt.Errorf("crv4: runtime version: %w", err)
	}
	return &Chain{API: api, Meta: meta, GenesisHash: genesis, Runtime: rv}, nil
}

func encodeNetuid(netuid uint16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], netuid)
	return b[:]
}

// storageGet reads one storage value at blockHash (or latest when zero
// hash), returning ok=false when the key is unset (caller applies the
// on-chain default).
func (c *Chain) storageGet(target interface{}, blockHash types.Hash, item string, args ...[]byte) (bool, error) {
	key, err := types.CreateStorageKey(c.Meta, PalletName, item, args...)
	if err != nil {
		return false, fmt.Errorf("crv4: storage key %s.%s: %w", PalletName, item, err)
	}
	if (blockHash == types.Hash{}) {
		ok, err := c.API.RPC.State.GetStorageLatest(key, target)
		if err != nil {
			return false, fmt.Errorf("crv4: read %s: %w", item, err)
		}
		return ok, nil
	}
	ok, err := c.API.RPC.State.GetStorage(key, target, blockHash)
	if err != nil {
		return false, fmt.Errorf("crv4: read %s: %w", item, err)
	}
	return ok, nil
}

// Tempo reads SubtensorModule.Tempo(netuid).
func (c *Chain) Tempo(netuid uint16) (uint16, error) {
	var v types.U16
	if _, err := c.storageGet(&v, types.Hash{}, "Tempo", encodeNetuid(netuid)); err != nil {
		return 0, err
	}
	return uint16(v), nil
}

// RevealPeriodEpochs reads SubtensorModule.RevealPeriodEpochs(netuid), the
// commit_reveal_period hyperparameter (default 1).
func (c *Chain) RevealPeriodEpochs(netuid uint16) (uint64, error) {
	var v types.U64
	ok, err := c.storageGet(&v, types.Hash{}, "RevealPeriodEpochs", encodeNetuid(netuid))
	if err != nil {
		return 0, err
	}
	if !ok {
		return 1, nil // DefaultRevealPeriodEpochs
	}
	return uint64(v), nil
}

// CommitRevealEnabled reads SubtensorModule.CommitRevealWeightsEnabled(netuid)
// (default true).
func (c *Chain) CommitRevealEnabled(netuid uint16) (bool, error) {
	var v types.Bool
	ok, err := c.storageGet(&v, types.Hash{}, "CommitRevealWeightsEnabled", encodeNetuid(netuid))
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil // DefaultCommitRevealWeightsEnabled
	}
	return bool(v), nil
}

// CommitRevealVersion reads SubtensorModule.CommitRevealWeightsVersion (the
// commit_reveal_version the commit extrinsic must carry; default/current 4).
func (c *Chain) CommitRevealVersion() (uint16, error) {
	var v types.U16
	ok, err := c.storageGet(&v, types.Hash{}, "CommitRevealWeightsVersion")
	if err != nil {
		return 0, err
	}
	if !ok {
		return CommitRevealVersion4, nil
	}
	return uint16(v), nil
}

// MaxWeightsLimit reads SubtensorModule.MaxWeightsLimit(netuid).
func (c *Chain) MaxWeightsLimit(netuid uint16) (uint16, error) {
	var v types.U16
	ok, err := c.storageGet(&v, types.Hash{}, "MaxWeightsLimit", encodeNetuid(netuid))
	if err != nil {
		return 0, err
	}
	if !ok {
		return U16Max, nil
	}
	return uint16(v), nil
}

// WeightPair mirrors the SCALE tuple stored in SubtensorModule.Weights.
type WeightPair struct {
	UID   types.U16
	Value types.U16
}

// WeightsAtFinalized reads one validator UID's applied weight row at the
// canonical finalized head. It is used to distinguish a finalized CRv4 commit
// from a commit whose timelock payload has actually been revealed and applied.
func (c *Chain) WeightsAtFinalized(netuid, validatorUID uint16) ([]WeightPair, uint64, types.Hash, error) {
	hash, err := c.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return nil, 0, types.Hash{}, fmt.Errorf("crv4: finalized head: %w", err)
	}
	header, err := c.API.RPC.Chain.GetHeader(hash)
	if err != nil {
		return nil, 0, types.Hash{}, fmt.Errorf("crv4: finalized header: %w", err)
	}
	var row []WeightPair
	_, err = c.storageGet(&row, hash, "Weights", encodeNetuid(netuid), encodeNetuid(validatorUID))
	if err != nil {
		return nil, 0, types.Hash{}, err
	}
	return row, uint64(header.Number), hash, nil
}

// WeightsVersionKey reads SubtensorModule.WeightsVersionKey(netuid); the
// payload's version_key must be >= this for the weights to apply at reveal.
func (c *Chain) WeightsVersionKey(netuid uint16) (uint64, error) {
	var v types.U64
	if _, err := c.storageGet(&v, types.Hash{}, "WeightsVersionKey", encodeNetuid(netuid)); err != nil {
		return 0, err
	}
	return uint64(v), nil
}

// EpochScheduleState reads all epoch-schedule storage for netuid at one
// consistent block (mirrors the SDK's get_epoch_schedule_state: storage
// items LastEpochBlock, PendingEpochAt, SubnetEpochIndex, Tempo,
// BlocksSinceLastStep + the block number of the snapshot).
func (c *Chain) EpochScheduleState(netuid uint16) (*EpochScheduleState, error) {
	blockHash, err := c.API.RPC.Chain.GetBlockHashLatest()
	if err != nil {
		return nil, fmt.Errorf("crv4: chain head: %w", err)
	}
	return c.epochScheduleStateAt(netuid, blockHash)
}

// EpochScheduleStateFinalized reads all scheduling inputs and the block number
// at one canonical finalized head. Production schedulers must use this method;
// a best-head transition is never allowed to trigger an irreversible commit.
func (c *Chain) EpochScheduleStateFinalized(netuid uint16) (*EpochScheduleState, types.Hash, error) {
	blockHash, err := c.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return nil, types.Hash{}, fmt.Errorf("crv4: finalized head: %w", err)
	}
	state, err := c.epochScheduleStateAt(netuid, blockHash)
	return state, blockHash, err
}

func (c *Chain) epochScheduleStateAt(netuid uint16, blockHash types.Hash) (*EpochScheduleState, error) {
	header, err := c.API.RPC.Chain.GetHeader(blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: header: %w", err)
	}

	arg := encodeNetuid(netuid)
	var (
		lastEpochBlock, pendingEpochAt, subnetEpochIndex, blocksSince types.U64
		tempo                                                         types.U16
	)
	if _, err := c.storageGet(&lastEpochBlock, blockHash, "LastEpochBlock", arg); err != nil {
		return nil, err
	}
	if _, err := c.storageGet(&pendingEpochAt, blockHash, "PendingEpochAt", arg); err != nil {
		return nil, err
	}
	if _, err := c.storageGet(&subnetEpochIndex, blockHash, "SubnetEpochIndex", arg); err != nil {
		return nil, err
	}
	if _, err := c.storageGet(&blocksSince, blockHash, "BlocksSinceLastStep", arg); err != nil {
		return nil, err
	}
	if _, err := c.storageGet(&tempo, blockHash, "Tempo", arg); err != nil {
		return nil, err
	}

	return &EpochScheduleState{
		LastEpochBlock:      uint64(lastEpochBlock),
		PendingEpochAt:      uint64(pendingEpochAt),
		SubnetEpochIndex:    uint64(subnetEpochIndex),
		Tempo:               uint16(tempo),
		BlocksSinceLastStep: uint64(blocksSince),
		CurrentBlock:        uint64(header.Number),
	}, nil
}

// AccountNonce returns the next transaction index for the hotkey, including
// transactions pending in the pool (system_accountNextIndex).
func (c *Chain) AccountNonce(ss58Address string) (uint32, error) {
	var nonce uint32
	if err := c.API.Client.Call(&nonce, "system_accountNextIndex", ss58Address); err != nil {
		return 0, fmt.Errorf("crv4: account nonce: %w", err)
	}
	return nonce, nil
}

// FinalizedAccountNonce returns only the nonce committed in canonical state;
// unlike system_accountNextIndex it is unaffected by an exact transaction
// already waiting in the local pool. This distinction is essential when
// deciding whether a persisted transaction can still be replayed.
func (c *Chain) FinalizedAccountNonce(publicKey [32]byte) (uint32, types.Hash, uint64, error) {
	finalized, err := c.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return 0, types.Hash{}, 0, err
	}
	header, err := c.API.RPC.Chain.GetHeader(finalized)
	if err != nil {
		return 0, types.Hash{}, 0, err
	}
	key, err := types.CreateStorageKey(c.Meta, "System", "Account", publicKey[:])
	if err != nil {
		return 0, types.Hash{}, 0, err
	}
	var account types.AccountInfo
	present, err := c.API.RPC.State.GetStorage(key, &account, finalized)
	if err != nil {
		return 0, types.Hash{}, 0, err
	}
	if !present {
		return 0, finalized, uint64(header.Number), nil
	}
	return uint32(account.Nonce), finalized, uint64(header.Number), nil
}

// NewSignedExtrinsic signs an arbitrary runtime call with the same runtime-451
// signed-extension set used by CRv4. It is shared by CRv4 and the commitments
// pallet so both paths have one exact signing implementation.
func (c *Chain) NewSignedExtrinsic(kp *Keypair, call types.Call, nonce uint32) (*extrinsic.Extrinsic, error) {
	ext := extrinsic.NewExtrinsic(call)
	err := ext.Sign(kp.Ring, c.Meta,
		extrinsic.WithEra(types.ExtrinsicEra{IsImmortalEra: true}, c.GenesisHash),
		extrinsic.WithNonce(types.NewUCompactFromUInt(uint64(nonce))),
		extrinsic.WithTip(types.NewUCompactFromUInt(0)),
		extrinsic.WithSpecVersion(c.Runtime.SpecVersion),
		extrinsic.WithTransactionVersion(c.Runtime.TransactionVersion),
		extrinsic.WithGenesisHash(c.GenesisHash),
		extrinsic.WithMetadataMode(extensions.CheckMetadataModeDisabled, extensions.CheckMetadataHash{Hash: types.NewEmptyOption[types.H256]()}),
	)
	if err != nil {
		return nil, fmt.Errorf("crv4: sign extrinsic: %w", err)
	}
	return &ext, nil
}

// SignedExtrinsicUsesImmortalEra inspects the actual signed-extension field,
// rather than assuming the caller used NewSignedExtrinsic. Release CRv4
// persists and replays exact signed bytes, so an unexpected mortal era would
// create a hidden expiry boundary during finality lag (RaoFoundation/
// bittensor#3395). Exactly one explicit immortal era is required.
func SignedExtrinsicUsesImmortalEra(ext *extrinsic.Extrinsic) bool {
	if ext == nil || ext.Signature == nil {
		return false
	}
	seen := false
	for _, field := range ext.Signature.SignedFields {
		if field == nil || field.Name != extrinsic.EraSignedField {
			continue
		}
		if seen {
			return false
		}
		seen = true
		var era types.ExtrinsicEra
		switch value := field.Value.(type) {
		case types.ExtrinsicEra:
			era = value
		case *types.ExtrinsicEra:
			if value == nil {
				return false
			}
			era = *value
		default:
			return false
		}
		if !era.IsImmortalEra {
			return false
		}
	}
	return seen
}

// SubmitAndWatchFinalized broadcasts ext and waits for canonical finality. It
// rejects pool terminal states, retractions, and finalized dispatch failures.
// Callers should additionally verify their operation-specific storage
// postcondition where one exists.
func (c *Chain) SubmitAndWatchFinalized(ctx context.Context, ext *extrinsic.Extrinsic) (*FinalizedExtrinsic, error) {
	if ext == nil {
		return nil, fmt.Errorf("crv4: nil extrinsic")
	}
	encoded, err := codec.EncodeToHex(*ext)
	if err != nil {
		return nil, fmt.Errorf("crv4: encode extrinsic: %w", err)
	}
	return c.SubmitRawAndWatchFinalized(ctx, encoded)
}

// SubmitRawAndWatchFinalized is the restart-safe variant of
// SubmitAndWatchFinalized. Metadata-driven signed extension fields cannot be
// generically decoded back into gsrpc's Extrinsic type, so durable callers
// replay the exact persisted SCALE hex through the RPC subscription.
func (c *Chain) SubmitRawAndWatchFinalized(ctx context.Context, encoded string) (*FinalizedExtrinsic, error) {
	raw, err := codec.HexDecodeString(encoded)
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("crv4: malformed raw extrinsic")
	}
	h := blake2b.Sum256(raw)
	txHash := types.Hash(h)

	statuses := make(chan types.ExtrinsicStatus)
	sub, err := c.API.Client.Subscribe(
		ctx, "author", "submitAndWatchExtrinsic", "unwatchExtrinsic", "extrinsicUpdate",
		statuses, codec.HexEncodeToString(raw),
	)
	if err != nil {
		return nil, fmt.Errorf("crv4: submit/watch %s: %w", txHash.Hex(), err)
	}
	defer sub.Unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err, ok := <-sub.Err():
			if ok && err != nil {
				return nil, fmt.Errorf("crv4: watch %s: %w", txHash.Hex(), err)
			}
		case status, ok := <-statuses:
			if !ok {
				return nil, fmt.Errorf("crv4: watch %s closed before finality", txHash.Hex())
			}
			switch {
			case status.IsFinalized:
				header, err := c.API.RPC.Chain.GetHeader(status.AsFinalized)
				if err != nil {
					return nil, fmt.Errorf("crv4: finalized header %s: %w", status.AsFinalized.Hex(), err)
				}
				if err := c.VerifyFinalizedExtrinsic(status.AsFinalized, txHash); err != nil {
					return nil, err
				}
				return &FinalizedExtrinsic{ExtrinsicHash: txHash, BlockHash: status.AsFinalized, BlockNumber: uint64(header.Number)}, nil
			case status.IsDropped, status.IsInvalid, status.IsUsurped, status.IsFinalityTimeout, status.IsRetracted:
				return nil, fmt.Errorf("crv4: extrinsic %s failed before finality: %+v", txHash.Hex(), status)
			}
		}
	}
}

// NewCommitExtrinsic builds and signs (but does not submit) the CRv4 commit
// extrinsic. mecid selects commit_timelocked_mechanism_weights when non-nil;
// otherwise commit_timelocked_weights (MechId::MAIN) is used.
func (c *Chain) NewCommitExtrinsic(kp *Keypair, netuid uint16, mecid *uint8, ciphertext []byte, revealRound uint64, commitRevealVersion uint16, nonce uint32) (*extrinsic.Extrinsic, error) {
	if len(ciphertext) > MaxCommitSizeBytes {
		return nil, fmt.Errorf("crv4: ciphertext %d bytes exceeds MAX_CRV3_COMMIT_SIZE_BYTES %d", len(ciphertext), MaxCommitSizeBytes)
	}

	var (
		call types.Call
		err  error
	)
	if mecid == nil {
		call, err = types.NewCall(c.Meta, PalletName+"."+CallCommitTimelocked,
			types.NewU16(netuid), types.NewBytes(ciphertext), types.NewU64(revealRound), types.NewU16(commitRevealVersion))
	} else {
		call, err = types.NewCall(c.Meta, PalletName+"."+CallCommitTimelockedMech,
			types.NewU16(netuid), types.NewU8(*mecid), types.NewBytes(ciphertext), types.NewU64(revealRound), types.NewU16(commitRevealVersion))
	}
	if err != nil {
		return nil, fmt.Errorf("crv4: build call: %w", err)
	}

	return c.NewSignedExtrinsic(kp, call, nonce)
}

// Commit signs and submits the CRv4 commit extrinsic (hotkey-signed), waits
// for finalized inclusion, and returns the extrinsic hash. The nonce is fetched via
// system_accountNextIndex. ctx is honored between RPC steps (gsrpc calls
// are not context-aware internally).
func (c *Chain) Commit(ctx context.Context, kp *Keypair, netuid uint16, mecid *uint8, ciphertext []byte, revealRound uint64, commitRevealVersion uint16) (types.Hash, error) {
	receipt, err := c.CommitFinalized(ctx, kp, netuid, mecid, ciphertext, revealRound, commitRevealVersion)
	if err != nil {
		return types.Hash{}, err
	}
	return receipt.ExtrinsicHash, nil
}

// CommitFinalized is Commit with the full canonical receipt required by the
// validator's durable intent journal.
func (c *Chain) CommitFinalized(ctx context.Context, kp *Keypair, netuid uint16, mecid *uint8, ciphertext []byte, revealRound uint64, commitRevealVersion uint16) (*FinalizedExtrinsic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nonce, err := c.AccountNonce(kp.Address())
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ext, err := c.NewCommitExtrinsic(kp, netuid, mecid, ciphertext, revealRound, commitRevealVersion, nonce)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.SubmitAndWatchFinalized(ctx, ext)
}

// EncodeExtrinsic returns the SCALE hex of a signed extrinsic (what
// author_submitExtrinsic would receive) without submitting.
func EncodeExtrinsic(ext *extrinsic.Extrinsic) (string, error) {
	return codec.EncodeToHex(*ext)
}

// ---------------------------------------------------------------------------
// Metadata conformance checking
// ---------------------------------------------------------------------------

// CallReport describes one dispatchable's shape as found in live metadata.
type CallReport struct {
	Found     bool
	CallIndex uint8
	Args      []ArgReport
}

// ArgReport is one call argument: its metadata name and resolved type shape.
type ArgReport struct {
	Name     string
	TypeName string // as written in the runtime source, e.g. "NetUid"
	Shape    string // resolved primitive shape, e.g. "u16", "compact-vec<u8>"
}

// ExtensionReport describes one signed extension and whether this package
// can construct extrinsics for it.
type ExtensionReport struct {
	Identifier string
	TypeName   string
	Handled    bool
	ZeroSize   bool // encodes zero bytes on the wire (safe to skip)
}

// MetadataReport is the result of CheckMetadata: everything SP-2 needs to
// verify against a live chain before trusting Commit.
type MetadataReport struct {
	SpecName           string
	SpecVersion        uint32
	TransactionVersion uint32
	PalletIndex        uint8
	CommitTimelocked   CallReport
	CommitMechanism    CallReport
	LegacyCrv3Present  bool // commit_crv3_weights should NOT exist
	Extensions         []ExtensionReport
	StorageFound       map[string]bool
	Problems           []string
}

// DescribeCall returns the metadata-derived index and argument shapes for one
// dispatchable. It is used by release preflight code for calls outside CRv4
// (registration, custody setup, and stake transfer) without duplicating the
// metadata-v14 type resolver.
func (c *Chain) DescribeCall(palletName, callName string) (CallReport, error) {
	if c.Meta.Version != 14 {
		return CallReport{}, fmt.Errorf("crv4: expected metadata v14, got v%d", c.Meta.Version)
	}
	m := c.Meta.AsMetadataV14
	for i := range m.Pallets {
		pallet := &m.Pallets[i]
		if string(pallet.Name) != palletName {
			continue
		}
		if !pallet.HasCalls {
			return CallReport{}, nil
		}
		callsType, ok := m.EfficientLookup[pallet.Calls.Type.Int64()]
		if !ok || !callsType.Def.IsVariant {
			return CallReport{}, fmt.Errorf("crv4: pallet %s call type is unavailable", palletName)
		}
		for _, variant := range callsType.Def.Variant.Variants {
			if string(variant.Name) == callName {
				return c.describeCall(m, variant), nil
			}
		}
		return CallReport{}, nil
	}
	return CallReport{}, fmt.Errorf("crv4: pallet %s not found", palletName)
}

var requiredStorageItems = []string{
	"Tempo", "LastEpochBlock", "PendingEpochAt", "SubnetEpochIndex",
	"BlocksSinceLastStep", "RevealPeriodEpochs", "CommitRevealWeightsEnabled",
	"CommitRevealWeightsVersion", "MaxWeightsLimit", "WeightsVersionKey",
}

// CheckMetadata verifies the live chain's metadata against everything this
// package assumes: the commit calls and their argument codecs, the signed
// extension set, and the storage items the schedule/hyperparam readers use.
// Problems is empty when the chain is fully conformant.
func (c *Chain) CheckMetadata() (*MetadataReport, error) {
	if c.Meta.Version != 14 {
		return nil, fmt.Errorf("crv4: expected metadata v14, got v%d", c.Meta.Version)
	}
	m := c.Meta.AsMetadataV14

	report := &MetadataReport{
		SpecName:           c.Runtime.SpecName,
		SpecVersion:        uint32(c.Runtime.SpecVersion),
		TransactionVersion: uint32(c.Runtime.TransactionVersion),
		StorageFound:       map[string]bool{},
	}

	var pallet *types.PalletMetadataV14
	for i := range m.Pallets {
		if string(m.Pallets[i].Name) == PalletName {
			pallet = &m.Pallets[i]
			break
		}
	}
	if pallet == nil {
		report.Problems = append(report.Problems, "pallet "+PalletName+" not found")
		return report, nil
	}
	report.PalletIndex = uint8(pallet.Index)

	// --- calls ---
	if pallet.HasCalls {
		callsType, ok := m.EfficientLookup[pallet.Calls.Type.Int64()]
		if ok && callsType.Def.IsVariant {
			for _, v := range callsType.Def.Variant.Variants {
				switch string(v.Name) {
				case CallCommitTimelocked:
					report.CommitTimelocked = c.describeCall(m, v)
				case CallCommitTimelockedMech:
					report.CommitMechanism = c.describeCall(m, v)
				case "commit_crv3_weights":
					report.LegacyCrv3Present = true
				}
			}
		}
	}
	checkCall := func(name string, r CallReport, wantArgs []string, wantShapes []string) {
		if !r.Found {
			report.Problems = append(report.Problems, "call "+name+" not found")
			return
		}
		if len(r.Args) != len(wantArgs) {
			report.Problems = append(report.Problems, fmt.Sprintf("call %s has %d args, want %d", name, len(r.Args), len(wantArgs)))
			return
		}
		for i, a := range r.Args {
			if a.Name != wantArgs[i] {
				report.Problems = append(report.Problems, fmt.Sprintf("call %s arg %d is %q, want %q", name, i, a.Name, wantArgs[i]))
			}
			if a.Shape != wantShapes[i] {
				report.Problems = append(report.Problems, fmt.Sprintf("call %s arg %s has shape %q, want %q", name, a.Name, a.Shape, wantShapes[i]))
			}
		}
	}
	checkCall(CallCommitTimelocked, report.CommitTimelocked,
		[]string{"netuid", "commit", "reveal_round", "commit_reveal_version"},
		[]string{"u16", "vec<u8>", "u64", "u16"})
	checkCall(CallCommitTimelockedMech, report.CommitMechanism,
		[]string{"netuid", "mecid", "commit", "reveal_round", "commit_reveal_version"},
		[]string{"u16", "u8", "vec<u8>", "u64", "u16"})

	// --- signed extensions ---
	for _, se := range m.Extrinsic.SignedExtensions {
		er := ExtensionReport{Identifier: string(se.Identifier)}
		ty, ok := m.EfficientLookup[se.Type.Int64()]
		if !ok {
			report.Problems = append(report.Problems, "signed extension type not found: "+er.Identifier)
			report.Extensions = append(report.Extensions, er)
			continue
		}
		if len(ty.Path) > 0 {
			er.TypeName = string(ty.Path[len(ty.Path)-1])
		}
		_, er.Handled = extrinsic.PayloadMutatorFns[extensions.SignedExtensionName(er.TypeName)]
		er.ZeroSize = c.typeIsZeroSize(m, se.Type.Int64(), 0)
		if !er.Handled && !er.ZeroSize {
			report.Problems = append(report.Problems, fmt.Sprintf("signed extension %s (%s) is unhandled and non-zero-size", er.Identifier, er.TypeName))
		}
		report.Extensions = append(report.Extensions, er)
	}

	// --- storage items ---
	if pallet.HasStorage {
		found := map[string]bool{}
		for _, item := range pallet.Storage.Items {
			found[string(item.Name)] = true
		}
		for _, want := range requiredStorageItems {
			report.StorageFound[want] = found[want]
			if !found[want] {
				report.Problems = append(report.Problems, "storage item "+want+" not found")
			}
		}
	} else {
		report.Problems = append(report.Problems, "pallet has no storage")
	}

	sort.Strings(report.Problems)
	return report, nil
}

func (c *Chain) describeCall(m types.MetadataV14, v types.Si1Variant) CallReport {
	r := CallReport{Found: true, CallIndex: uint8(v.Index)}
	for _, f := range v.Fields {
		a := ArgReport{Name: string(f.Name), TypeName: string(f.TypeName)}
		a.Shape = c.typeShape(m, f.Type.Int64(), 0)
		r.Args = append(r.Args, a)
	}
	return r
}

// typeShape resolves a type to a short structural description, unwrapping
// single-field composites (newtypes like NetUid/MechId and BoundedVec).
func (c *Chain) typeShape(m types.MetadataV14, id int64, depth int) string {
	if depth > 8 {
		return "?deep"
	}
	ty, ok := m.EfficientLookup[id]
	if !ok {
		return "?unknown"
	}
	def := ty.Def
	switch {
	case def.IsPrimitive:
		return strings.ToLower(primitiveName(def.Primitive.Si0TypeDefPrimitive))
	case def.IsCompact:
		return "compact<" + c.typeShape(m, def.Compact.Type.Int64(), depth+1) + ">"
	case def.IsSequence:
		return "vec<" + c.typeShape(m, def.Sequence.Type.Int64(), depth+1) + ">"
	case def.IsArray:
		return fmt.Sprintf("[%s;%d]", c.typeShape(m, def.Array.Type.Int64(), depth+1), def.Array.Len)
	case def.IsComposite:
		if len(def.Composite.Fields) == 1 {
			return c.typeShape(m, def.Composite.Fields[0].Type.Int64(), depth+1)
		}
		if len(ty.Path) > 0 {
			return "composite:" + string(ty.Path[len(ty.Path)-1])
		}
		return "composite"
	case def.IsTuple:
		if len(def.Tuple) == 0 {
			return "()"
		}
		parts := make([]string, len(def.Tuple))
		for i, t := range def.Tuple {
			parts[i] = c.typeShape(m, t.Int64(), depth+1)
		}
		return "(" + strings.Join(parts, ",") + ")"
	default:
		return "?other"
	}
}

// typeIsZeroSize reports whether a type SCALE-encodes to zero bytes
// (PhantomData-style unit structs and empty tuples).
func (c *Chain) typeIsZeroSize(m types.MetadataV14, id int64, depth int) bool {
	if depth > 8 {
		return false
	}
	ty, ok := m.EfficientLookup[id]
	if !ok {
		return false
	}
	def := ty.Def
	switch {
	case def.IsComposite:
		for _, f := range def.Composite.Fields {
			if !c.typeIsZeroSize(m, f.Type.Int64(), depth+1) {
				return false
			}
		}
		return true
	case def.IsTuple:
		for _, t := range def.Tuple {
			if !c.typeIsZeroSize(m, t.Int64(), depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func primitiveName(p types.Si0TypeDefPrimitive) string {
	switch p {
	case types.IsBool:
		return "bool"
	case types.IsChar:
		return "char"
	case types.IsStr:
		return "str"
	case types.IsU8:
		return "u8"
	case types.IsU16:
		return "u16"
	case types.IsU32:
		return "u32"
	case types.IsU64:
		return "u64"
	case types.IsU128:
		return "u128"
	case types.IsU256:
		return "u256"
	case types.IsI8:
		return "i8"
	case types.IsI16:
		return "i16"
	case types.IsI32:
		return "i32"
	case types.IsI64:
		return "i64"
	case types.IsI128:
		return "i128"
	case types.IsI256:
		return "i256"
	default:
		return fmt.Sprintf("primitive-%d", p)
	}
}
