// Settlement v2 activates when the governance Safe sends STSettlement2's
// registerOperatorPool (VERSION2-FINAL.md §12.1 step 5, §14) as a direct Safe
// call, as v1's registerOperator went. review builds that one call for review
// and Safe import, refuses what is wrong before the Safe signs and preflights
// it from the Safe; readback checks the registration after the send. Neither
// signs nor sends, and every read is pinned to one head block.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/v2026/stabi"
)

const poolRegistrationReviewSchema = "urnetwork-mainnet-settlement-v2-pool-registration-review-v1"
const poolRegistrationReadbackSchema = "urnetwork-mainnet-settlement-v2-pool-registration-readback-v1"

// The one call the Safe sends.
const poolRegistrationFunction = "registerOperatorPool(uint256,bytes32,uint64,uint64)"

// A refusal means the Safe must not send the call; a difference means the
// chain does not hold the registration that was reviewed. Both exit 3, and so
// does every call the chain ran and failed: no rerun changes those. Exit 1 is
// only a read that got no such answer, which decides nothing.
var errPoolRefused = errors.New("registerOperatorPool refused")
var errPoolDiffers = errors.New("registerOperatorPool readback differs")

func poolRefusal(reason string) error { return fmt.Errorf("%w: %s", errPoolRefused, reason) }

func poolDifference(reason string) error { return fmt.Errorf("%w: %s", errPoolDiffers, reason) }

// Review's local refusals, all made before the preflight.
var (
	errPoolZeroHotkey   = poolRefusal("the pool hotkey is zero")
	errPoolValue        = poolRefusal("the value is not maximumBurnRao × 10^9 wei")
	errPoolChain        = poolRefusal("the RPC is not EVM chain 964")
	errPoolEscrowHotkey = poolRefusal("the pool hotkey is vault2's escrow hotkey")
	errPoolEpochStarted = poolRefusal("the first epoch has already started")
)

// The named failures of a call the chain ran: the reverts registerOperatorPool
// raises, and a sender that cannot fund the call's value.
const (
	poolInactiveOperator     = "InactiveOperator: the v1 operator is not active in the current epoch"
	poolInvalidEpoch         = "InvalidEpoch: the first epoch is not after the current epoch"
	poolCadenceTooShort      = "CadenceTooShort: the first epoch's v1 policy is shorter than its commit window plus vault2's challenge window"
	poolNativeRefundFailed   = "NativeRefundFailed: the burn surplus could not be refunded to the Safe"
	poolInvalidConfiguration = "InvalidConfiguration: vault2 refuses the pool: an unregistered escrow, a no_id outside 1 to 2^64 - 1, a zero, escrow or used hotkey, a zero burn cap or first epoch, or a registered pool"
	poolUnauthorized         = "OwnableUnauthorizedAccount: the Safe does not own STSettlement2"
	poolUnknownOperator      = "UnknownOperator: no_id is not a v1 operator"
	poolOutOfFund            = "OutOfFund: the Safe holds less than the value"
)

// Reverts are named by selector. vault2's InvalidConfiguration and
// NativeRefundFailed share STSettlement2's selectors, and the v1
// coordinator's UnknownOperator passes through STSettlement2 unchanged.
var poolReverts = map[[4]byte]string{
	[4]byte(stabi.STSettlement2InactiveOperatorErrorID().Bytes()):           poolInactiveOperator,
	[4]byte(stabi.STSettlement2InvalidEpochErrorID().Bytes()):               poolInvalidEpoch,
	[4]byte(stabi.STSettlement2CadenceTooShortErrorID().Bytes()):            poolCadenceTooShort,
	[4]byte(stabi.STSettlement2NativeRefundFailedErrorID().Bytes()):         poolNativeRefundFailed,
	[4]byte(stabi.STSettlementVault2InvalidConfigurationErrorID().Bytes()):  poolInvalidConfiguration,
	[4]byte(stabi.STSettlement2OwnableUnauthorizedAccountErrorID().Bytes()): poolUnauthorized,
	[4]byte(stabi.STCoordinatorUnknownOperatorErrorID().Bytes()):            poolUnknownOperator,
}

// Readback's named differences.
var (
	errPoolNoReceipt         = poolDifference("the transaction has no receipt")
	errPoolReverted          = poolDifference("the transaction reverted")
	errPoolNoEvent           = poolDifference("the receipt has no PoolRegistered event of noId from vault2")
	errPoolEventHotkey       = poolDifference("the PoolRegistered hotkey differs")
	errPoolEventFirstEpoch   = poolDifference("the PoolRegistered firstEpoch differs")
	errPoolUnregistered      = poolDifference("vault2.pools(noId) is not registered")
	errPoolFirstEpoch        = poolDifference("vault2.pools(noId) firstEpoch differs")
	errPoolHotkey            = poolDifference("vault2.pools(noId) hotkey differs")
	errPoolSettlementVault   = poolDifference("settlementAt(noId, firstEpoch) vault is not vault2")
	errPoolSettlementHotkey  = poolDifference("settlementAt(noId, firstEpoch) hotkey differs")
	errPoolSettlementVersion = poolDifference("settlementAt(noId, firstEpoch) version is not 2")
)

// One pool registration as the owner fixes it (O8, O16 to O18).
type poolRegistration struct {
	settlement     common.Address
	safe           common.Address
	noId           uint64
	hotkey         common.Hash
	maximumBurnRao uint64
	valueWei       *big.Int
	firstEpoch     uint64
	transaction    common.Hash
}

// The call's arguments, decoded under their parameter names.
type poolRegistrationArgs struct {
	NoId           string `json:"noId"`
	Hotkey         string `json:"hotkey"`
	MaximumBurnRao string `json:"maximumBurnRao"`
	FirstEpoch     string `json:"firstEpoch"`
}

// The Safe's exact call, with what review checked it against: the contracts
// STSettlement2 names, the head, E_s's start and the UID the preflight
// registered at the head.
type poolRegistrationReview struct {
	Schema               string               `json:"schema"`
	ChainId              uint64               `json:"chain_id"`
	Safe                 string               `json:"safe"`
	To                   string               `json:"to"`
	ValueWei             string               `json:"value_wei"`
	Data                 string               `json:"data"`
	Function             string               `json:"function"`
	Args                 poolRegistrationArgs `json:"args"`
	Vault2               string               `json:"vault2"`
	EscrowHotkey         string               `json:"escrow_hotkey"`
	V1Coordinator        string               `json:"v1_coordinator"`
	HeadBlock            uint64               `json:"head_block"`
	FirstEpochStartBlock *big.Int             `json:"first_epoch_start_block"`
	BlocksRemaining      *big.Int             `json:"blocks_remaining"`
	PreflightUid         uint16               `json:"preflight_uid"`
}

// The registration the chain holds: vault2's event in the Safe's receipt,
// vault2.pools(noId) and settlementAt(noId, E_s).
type poolRegistrationReadback struct {
	Schema          string   `json:"schema"`
	ChainId         uint64   `json:"chain_id"`
	Settlement2     string   `json:"settlement2"`
	Vault2          string   `json:"vault2"`
	NoId            uint64   `json:"no_id"`
	PoolHotkey      string   `json:"pool_hotkey"`
	FirstEpoch      uint64   `json:"first_epoch"`
	Uid             uint16   `json:"uid"`
	TransactionHash string   `json:"transaction_hash"`
	BlockNumber     *big.Int `json:"block_number"`
	HeadBlock       uint64   `json:"head_block"`
}

// review prints the Safe's call after the local refusals and the preflight;
// readback checks the registration after the send. Every flag is required.
func runRegisterOperatorPoolCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "review" && args[0] != "readback" {
		fmt.Fprintln(stderr, "usage: register-operator-pool review --rpc URL --settlement2 ADDRESS --safe ADDRESS --no-id N --pool-hotkey 0xHOTKEY --maximum-burn-rao RAO --value-wei WEI --first-epoch EPOCH | readback --rpc URL --settlement2 ADDRESS --no-id N --pool-hotkey 0xHOTKEY --first-epoch EPOCH --transaction-hash 0xHASH")
		return 2
	}
	review := args[0] == "review"
	flags := flag.NewFlagSet("register-operator-pool "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	var pool poolRegistration
	var settlement, hotkey, safe, value, transaction string
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) EVM JSON-RPC URL")
	flags.StringVar(&settlement, "settlement2", "", "the STSettlement2 proxy")
	flags.Uint64Var(&pool.noId, "no-id", 0, "the v1 operator's no_id")
	flags.StringVar(&hotkey, "pool-hotkey", "", "the v2 pool hotkey: 0x and 32 bytes of hex")
	flags.Uint64Var(&pool.firstEpoch, "first-epoch", 0, "E_s, the pool's first v2 epoch")
	if review {
		flags.StringVar(&safe, "safe", "", "the governance Safe, which owns STSettlement2 and sends the call")
		flags.Uint64Var(&pool.maximumBurnRao, "maximum-burn-rao", 0, "the registration's burn ceiling in rao")
		flags.StringVar(&value, "value-wei", "", "the call's value: maximumBurnRao × 10^9 wei")
	} else {
		flags.StringVar(&transaction, "transaction-hash", "", "the Safe's transaction that sent the call")
	}
	complete := flags.Parse(args[1:]) == nil && flags.NArg() == 0
	set := map[string]bool{}
	flags.Visit(func(given *flag.Flag) { set[given.Name] = true })
	flags.VisitAll(func(defined *flag.Flag) { complete = complete && set[defined.Name] })
	var settlementOk, safeOk bool
	var valueErr error
	pool.settlement, settlementOk = poolAddress(settlement)
	pool.safe, safeOk = poolAddress(safe)
	pool.valueWei, valueErr = evmWei(value)
	pool.hotkey, pool.transaction = common.HexToHash(hotkey), common.HexToHash(transaction)
	if !complete || !settlementOk || !validHash(hotkey) || review && (!safeOk || valueErr != nil) || !review && !validHash(transaction) {
		fmt.Fprintln(stderr, "register-operator-pool "+args[0]+" requires every flag: nonzero 0x addresses, a 0x 32-byte hotkey and transaction hash, and decimal integers")
		return 2
	}
	client, err := newRpcClient(*rpcUrl, 60*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var output any
	if review {
		output, err = reviewPoolRegistration(ctx, client, pool)
	} else {
		output, err = readbackPoolRegistration(ctx, client, pool)
	}
	if err != nil {
		fmt.Fprintln(stderr, "register-operator-pool "+args[0]+":", err)
		if errors.Is(err, errPoolRefused) || errors.Is(err, errPoolDiffers) {
			return 3
		}
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(output); err != nil {
		fmt.Fprintln(stderr, "register-operator-pool "+args[0]+" output:", err)
		return 1
	}
	return 0
}

// A nonzero 0x-prefixed EVM address.
func poolAddress(value string) (common.Address, bool) {
	address := common.HexToAddress(value)
	return address, strings.HasPrefix(value, "0x") && common.IsHexAddress(value) && address != (common.Address{})
}

// Builds the call, refuses it locally, then preflights it from the Safe at
// the head. The preflight is an eth_call: it changes nothing.
func reviewPoolRegistration(ctx context.Context, client *rpcClient, pool poolRegistration) (poolRegistrationReview, error) {
	var result poolRegistrationReview
	if pool.hotkey == (common.Hash{}) {
		return result, errPoolZeroHotkey
	}
	if pool.valueWei.Cmp(new(big.Int).Mul(new(big.Int).SetUint64(pool.maximumBurnRao), big.NewInt(1_000_000_000))) != 0 {
		return result, errPoolValue
	}
	if err := poolRegistrationChainId(ctx, client); err != nil {
		return result, err
	}
	chain, err := poolRegistrationHead(ctx, client, poolRefusal)
	if err != nil {
		return result, err
	}
	settlement, vault, coordinator := stabi.NewSTSettlement2(), stabi.NewSTSettlementVault2(), stabi.NewSTCoordinator()
	vault2, err := poolView(ctx, chain, pool.settlement, settlement.PackVault(), settlement.UnpackVault)
	if err != nil {
		return result, err
	}
	escrow, err := poolView(ctx, chain, vault2, vault.PackEscrowHotkey(), vault.UnpackEscrowHotkey)
	if err != nil {
		return result, err
	}
	if common.Hash(escrow) == pool.hotkey {
		return result, errPoolEscrowHotkey
	}
	v1, err := poolView(ctx, chain, pool.settlement, settlement.PackV1Coordinator(), settlement.UnpackV1Coordinator)
	if err != nil {
		return result, err
	}
	start, err := poolView(ctx, chain, v1, coordinator.PackEpochStartBlock(new(big.Int).SetUint64(pool.firstEpoch)), coordinator.UnpackEpochStartBlock)
	if err != nil {
		return result, err
	}
	head := new(big.Int).SetUint64(chain.head)
	if start.Cmp(head) <= 0 {
		return result, errPoolEpochStarted
	}
	noId := new(big.Int).SetUint64(pool.noId)
	data := settlement.PackRegisterOperatorPool(noId, pool.hotkey, pool.maximumBurnRao, pool.firstEpoch)
	output, err := chain.call(ctx, &pool.safe, pool.valueWei, pool.settlement, data)
	if err != nil {
		return result, err
	}
	uid, err := settlement.UnpackRegisterOperatorPool(output)
	if err != nil {
		return result, poolRefusal(fmt.Sprintf("the preflight answered 0x%x, not the UID registerOperatorPool returns: %v", output, err))
	}
	args := poolRegistrationArgs{NoId: noId.String(), Hotkey: pool.hotkey.Hex(), MaximumBurnRao: strconv.FormatUint(pool.maximumBurnRao, 10), FirstEpoch: strconv.FormatUint(pool.firstEpoch, 10)}
	return poolRegistrationReview{Schema: poolRegistrationReviewSchema, ChainId: mainnetEvmChainId, Safe: pool.safe.Hex(), To: pool.settlement.Hex(), ValueWei: pool.valueWei.String(), Data: hexutil.Encode(data), Function: poolRegistrationFunction, Args: args,
		Vault2: vault2.Hex(), EscrowHotkey: common.Hash(escrow).Hex(), V1Coordinator: v1.Hex(), HeadBlock: chain.head, FirstEpochStartBlock: start, BlocksRemaining: new(big.Int).Sub(start, head), PreflightUid: uid}, ctx.Err()
}

// What the node answers when the chain ran a call and the call failed. A
// revert carries its bytes as hex data, under code -32603 from subtensor's
// node and code 3 from others, and an EVM error such as OutOfFund carries
// empty data. A revert is named by its selector and a sender short of the
// value by the node's own name for it; any other failure keeps the node's
// message and data. An error with no hex data, no code 3 and no word of a
// revert or an EVM error is a failed read, which this does not describe.
func poolCallFailure(err error) (string, bool) {
	var failure *rpcCallError
	var encoded string
	if !errors.As(err, &failure) {
		return "", false
	}
	_ = json.Unmarshal(failure.data, &encoded)
	revert, decodeErr := hexutil.Decode(encoded)
	if failure.code != 3 && decodeErr != nil && !strings.Contains(failure.message, "revert") && !strings.Contains(failure.message, "evm error") {
		return "", false
	}
	if len(revert) >= 4 && poolReverts[[4]byte(revert)] != "" {
		return poolReverts[[4]byte(revert)], true
	}
	if strings.Contains(failure.message, "OutOfFund") {
		return poolOutOfFund, true
	}
	return fmt.Sprintf("the call failed with %q and data 0x%x", failure.message, revert), true
}

// Reads the Safe's receipt, then vault2.pools(noId) and settlementAt(noId,
// E_s) at a head that includes it, and names every difference.
func readbackPoolRegistration(ctx context.Context, client *rpcClient, pool poolRegistration) (poolRegistrationReadback, error) {
	var result poolRegistrationReadback
	if err := poolRegistrationChainId(ctx, client); err != nil {
		return result, err
	}
	var receipt *types.Receipt
	if err := client.callPoolRegistrationRead(ctx, "eth_getTransactionReceipt", []any{pool.transaction.Hex()}, &receipt); err != nil {
		return result, err
	}
	chain, err := poolRegistrationHead(ctx, client, poolDifference)
	if err != nil {
		return result, err
	}
	settlement, vault := stabi.NewSTSettlement2(), stabi.NewSTSettlementVault2()
	vault2, err := poolView(ctx, chain, pool.settlement, settlement.PackVault(), settlement.UnpackVault)
	if err != nil {
		return result, err
	}
	noId, firstEpoch := new(big.Int).SetUint64(pool.noId), new(big.Int).SetUint64(pool.firstEpoch)
	record, err := poolView(ctx, chain, vault2, vault.PackPools(noId), vault.UnpackPools)
	if err != nil {
		return result, err
	}
	at, err := poolView(ctx, chain, pool.settlement, settlement.PackSettlementAt(noId, firstEpoch), settlement.UnpackSettlementAt)
	if err != nil {
		return result, err
	}
	var event *stabi.STSettlementVault2PoolRegistered
	if receipt != nil {
		for _, log := range receipt.Logs {
			if decoded, err := vault.UnpackPoolRegisteredEvent(log); err == nil && log.Address == vault2 && decoded.NoId.Cmp(noId) == 0 {
				event = decoded
			}
		}
	}
	succeeded := receipt != nil && receipt.Status == types.ReceiptStatusSuccessful
	differences := []error{}
	for _, check := range []struct {
		differs    bool
		difference error
	}{
		{receipt == nil, errPoolNoReceipt},
		{receipt != nil && !succeeded, errPoolReverted},
		{succeeded && event == nil, errPoolNoEvent},
		{event != nil && common.Hash(event.Hotkey) != pool.hotkey, errPoolEventHotkey},
		{event != nil && event.FirstEpoch != pool.firstEpoch, errPoolEventFirstEpoch},
		{!record.Registered, errPoolUnregistered},
		{record.FirstEpoch != pool.firstEpoch, errPoolFirstEpoch},
		{common.Hash(record.Hotkey) != pool.hotkey, errPoolHotkey},
		{at.Vault != vault2, errPoolSettlementVault},
		{common.Hash(at.Hotkey) != pool.hotkey, errPoolSettlementHotkey},
		{at.Version != 2, errPoolSettlementVersion},
	} {
		if check.differs {
			differences = append(differences, check.difference)
		}
	}
	if len(differences) != 0 {
		return result, errors.Join(differences...)
	}
	return poolRegistrationReadback{Schema: poolRegistrationReadbackSchema, ChainId: mainnetEvmChainId, Settlement2: pool.settlement.Hex(), Vault2: vault2.Hex(), NoId: pool.noId, PoolHotkey: pool.hotkey.Hex(), FirstEpoch: pool.firstEpoch,
		Uid: event.Uid, TransactionHash: pool.transaction.Hex(), BlockNumber: receipt.BlockNumber, HeadBlock: chain.head}, ctx.Err()
}

// The registration's reads, every eth_call at one head block. refuse makes a
// call the chain ran and failed the mode's own outcome: review's refusal or
// readback's difference.
type poolRegistrationChain struct {
	client *rpcClient
	head   uint64
	refuse func(string) error
}

// The registration reads only these methods; the profile cannot send.
func (self *rpcClient) callPoolRegistrationRead(ctx context.Context, method string, params []any, result any) error {
	switch method {
	case "eth_chainId", "eth_blockNumber", "eth_call":
		return self.callAdmittedRead(ctx, method, params, result, false, maxRpcReplyBytes)
	case "eth_getTransactionReceipt":
		return self.callAdmittedRead(ctx, method, params, result, true, maxRpcReplyBytes)
	}
	return errors.New("method is outside the pool registration read profile")
}

// The route must serve EVM chain 964.
func poolRegistrationChainId(ctx context.Context, client *rpcClient) error {
	var chainId hexutil.Uint64
	if err := client.callPoolRegistrationRead(ctx, "eth_chainId", []any{}, &chainId); err != nil {
		return err
	}
	if chainId != mainnetEvmChainId {
		return errPoolChain
	}
	return nil
}

// Pins every later read to the route's current head.
func poolRegistrationHead(ctx context.Context, client *rpcClient, refuse func(string) error) (*poolRegistrationChain, error) {
	var head hexutil.Uint64
	if err := client.callPoolRegistrationRead(ctx, "eth_blockNumber", []any{}, &head); err != nil {
		return nil, err
	}
	return &poolRegistrationChain{client: client, head: uint64(head), refuse: refuse}, nil
}

// One eth_call at the head: a view from no sender, or the Safe's call with
// its value. A call the chain ran and failed is the mode's outcome, never a
// failed read.
func (self *poolRegistrationChain) call(ctx context.Context, from *common.Address, value *big.Int, to common.Address, data []byte) ([]byte, error) {
	message := map[string]any{"to": to.Hex(), "data": hexutil.Encode(data)}
	if from != nil {
		message["from"], message["value"] = from.Hex(), hexutil.EncodeBig(value)
	}
	var output hexutil.Bytes
	err := self.client.callPoolRegistrationRead(ctx, "eth_call", []any{message, hexutil.EncodeUint64(self.head)}, &output)
	if failure, failed := poolCallFailure(err); failed {
		return nil, self.refuse(failure)
	}
	return output, err
}

// A view's answer at the head, decoded by its binding. An answer its contract
// would not give, as an address without the contract gives, is the mode's
// outcome too.
func poolView[T any](ctx context.Context, chain *poolRegistrationChain, to common.Address, data []byte, unpack func([]byte) (T, error)) (T, error) {
	output, err := chain.call(ctx, nil, nil, to, data)
	var value T
	if err == nil {
		if value, err = unpack(output); err != nil {
			err = chain.refuse(fmt.Sprintf("the call answered 0x%x, not as its contract does: %v", output, err))
		}
	}
	if err != nil {
		return value, fmt.Errorf("view 0x%x of %s: %w", data[:4], to.Hex(), err)
	}
	return value, nil
}
