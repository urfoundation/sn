package onchain

// v2_batch.go — claims through the optional claim batcher, STClaimBatcher
// (VERSION2-FINAL.md §10.2): one transaction calls vault2's claim for each
// item with a gas cap and reports each item's outcome. vault2 pays each
// leaf's coldkey whoever calls, so the batcher can redirect nothing. The
// batcher attempts only the items its gas left covers, so a batch succeeds
// at any gas limit and eth_estimateGas cannot size it; its limit comes from
// eth_call, whose answer counts the items attempted.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/v2026/stabi"
)

// ClaimBatchGasPerClaim is the gas a claim is given: the default cap of each
// batched claim and the least limit of a claim sent alone. A claim uses
// 170,000 to 188,000 gas, but the runtime admits its staking-precompile
// transfer only if the gas forwarded covers the transfer's declared weight.
// On runtimes 475 and 477 that held a walk of a full 256-entry StakingHotkeys
// list: at depth 29 a single claim's least gas limit was 2,443,953, and
// eth_estimateGas, whose search stops within 10%, gave up to 2,574,029 (R-6,
// R-7). Runtime 478 sizes the walk to vault2's own list, 8,750 gas an entry,
// so a claim's least limit is about 192,000 while the list is short, and its
// estimate follows the list: third parties can lengthen it to 128 entries
// between an estimate and the claim's inclusion (§7.3). This size covers the
// 256 a list may hold on either runtime. A batch caps each item at this or at
// the largest single-claim estimate of the batch, whichever is higher; a
// claim sent alone takes this or its estimate with 20%, whichever is higher.
const ClaimBatchGasPerClaim uint64 = 2_600_000

// ClaimBatchMaxGas is the most gas a transaction may name on runtime 475,
// measured by R-7: the normal dispatch class's extrinsic weight limit, the
// 75% normal share of the 4 s block less the 10% kept for block
// initialization, at 40,000 weight per gas, less the transaction's own
// extension weight. The pool refuses a larger limit as exceeding the block
// gas limit, although eth_getBlockByNumber reports 75,000,000.
const ClaimBatchMaxGas uint64 = 64_998_500

// ClaimBatchMaxClaims is the most claims the claim tools put in one batch.
// The daemon keeps a batch's signed transaction in every unit it claims, so
// its queue grows with the square of the batch: at depth 29, 2,112 bytes of
// calldata a claim, 62 claims fill the queue's 16 MiB durable head and 32
// about half of it. R-7 measured 147,740 gas a claim in a batch of 32,
// against 146,435 in the 418 one transaction holds.
const ClaimBatchMaxClaims = 32

// claimBatchGasMargin is the gas the claim tools keep below the most a
// transaction may name, against a change of the runtime constants that set it.
const claimBatchGasMargin = 10_000

// claimBatchTolerance is how close, in gas, SizeClaimBatch comes to the least
// gas limit at which a batch attempts every item.
const claimBatchTolerance = 10_000

// claimBatchCeiling is the most gas the claim tools name for a batch when the
// latest block reports blockGasLimit. The pool takes a transaction that names
// at most the normal dispatch class's extrinsic weight limit, 13/15 of the
// block's, less 1,500 gas of extension weight (R-7: 64,998,500 of 75,000,000,
// exact to the gas), and at most ClaimBatchMaxGas; the tools keep
// claimBatchGasMargin below that.
func claimBatchCeiling(blockGasLimit uint64) uint64 {
	normal := blockGasLimit/15*13 + blockGasLimit%15*13/15
	takes := min(ClaimBatchMaxGas, normal-min(normal, 1_500))
	return takes - min(takes, claimBatchGasMargin)
}

// VerifyClaimBatcher refuses a batcher address whose code is not exactly the
// release claim batcher's, ClaimBatcherRuntime: any other would refuse or
// misreport every batch.
func VerifyClaimBatcher(ctx context.Context, client *ethclient.Client, batcher common.Address) error {
	code, err := retryOnchainRead(ctx, func(ctx context.Context) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return client.CodeAt(cctx, batcher, nil)
	})
	if err != nil {
		return fmt.Errorf("claim batcher %s code: %w", batcher.Hex(), err)
	}
	if len(code) == 0 {
		return fmt.Errorf("claim batcher %s has no code", batcher.Hex())
	}
	if want := common.FromHex(ClaimBatcherRuntime); !bytes.Equal(code, want) {
		return fmt.Errorf("%s is not the release claim batcher: its code hash is %s, the release runtime's %s", batcher.Hex(), crypto.Keccak256Hash(code).Hex(), crypto.Keccak256Hash(want).Hex())
	}
	return nil
}

// claimBatcher packs and decodes STClaimBatcher calls and events.
var claimBatcher = stabi.NewSTClaimBatcher()

// SizeClaimBatch sizes a batch of items for from to send through batcher:
// how many of them, from the first and at most ClaimBatchMaxClaims, a
// transaction offering them attempts at the most gas the tools name,
// claimBatchCeiling of the latest block's gas limit (a prefix that surely
// fits once the others' calldata is gone, and the others wait for a later
// batch), the calldata claiming those, and the gas limit to send it with: the
// least within claimBatchTolerance at which an eth_call attempts them all,
// plus the 20% headroom of every estimate, up to that most.
func SizeClaimBatch(ctx context.Context, client *ethclient.Client, from, batcher, vault common.Address, items []stabi.STClaimBatcherClaim, gasPerClaim uint64) (int, []byte, uint64, error) {
	// Only the gas limit is read: Subtensor's EVM headers are not geth's.
	var latest struct {
		GasLimit *hexutil.Uint64 `json:"gasLimit"`
	}
	if _, err := retryOnchainRead(ctx, func(ctx context.Context) (struct{}, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return struct{}{}, client.Client().CallContext(cctx, &latest, "eth_getBlockByNumber", "latest", false)
	}); err != nil {
		return 0, nil, 0, fmt.Errorf("block gas limit: %w", err)
	}
	if latest.GasLimit == nil {
		return 0, nil, 0, errors.New("block gas limit: the latest block names none")
	}
	ceiling := claimBatchCeiling(uint64(*latest.GasLimit))
	items = items[:min(len(items), ClaimBatchMaxClaims)]
	attempted := func(data []byte, gas uint64) (uint64, error) {
		out, err := retryOnchainRead(ctx, func(ctx context.Context) ([]byte, error) {
			cctx, cancel := context.WithTimeout(ctx, callTimeout)
			defer cancel()
			return client.CallContract(cctx, ethereum.CallMsg{From: from, To: &batcher, Data: data, Gas: gas}, nil)
		})
		if err != nil {
			return 0, revertError(err)
		}
		count, err := claimBatcher.UnpackClaimMany(out)
		if err != nil || !count.IsUint64() {
			return 0, fmt.Errorf("%s is not a claim batcher: claimMany answered 0x%x", batcher.Hex(), out)
		}
		return count.Uint64(), nil
	}
	perClaim := new(big.Int).SetUint64(gasPerClaim)
	data := claimBatcher.PackClaimMany(vault, items, perClaim)
	fit, err := attempted(data, ceiling)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("batch preflight at %d gas: %w", ceiling, err)
	}
	if fit == 0 || fit > uint64(len(items)) {
		return 0, nil, 0, fmt.Errorf("a batch of %d claims attempts %d at %d gas", len(items), fit, ceiling)
	}
	if fit < uint64(len(items)) {
		data = claimBatcher.PackClaimMany(vault, items[:fit], perClaim)
	}
	low, high := uint64(0), ceiling
	for high-low > claimBatchTolerance {
		middle := low + (high-low)/2
		count, err := attempted(data, middle)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, nil, 0, errors.Join(err, ctxErr)
		}
		if err == nil && count == fit {
			high = middle
		} else {
			low = middle
		}
	}
	return int(fit), data, min(high+high/5, ceiling), nil
}

// ClaimBatch is a batch PrepareClaimBatch sized: the indexes of the items it
// claims, in order, each item's gas cap, its calldata and its gas limit.
type ClaimBatch struct {
	Items       []int
	GasPerClaim uint64
	Calldata    []byte
	GasLimit    uint64
}

// PrepareClaimBatch preflights each of items alone, as the vault2 claim from
// `from` it holds, with eth_estimateGas, then sizes a batch of the items that
// pass with SizeClaimBatch, each capped at the larger of ClaimBatchGasPerClaim
// and the largest of their estimates. It returns each item's preflight
// refusal, nil for one that passed, and, when two or more passed, the batch.
func PrepareClaimBatch(ctx context.Context, client *ethclient.Client, from, batcher, vault common.Address, items []stabi.STClaimBatcherClaim) ([]error, *ClaimBatch, error) {
	refusals := make([]error, len(items))
	batch := &ClaimBatch{GasPerClaim: ClaimBatchGasPerClaim}
	var offered []stabi.STClaimBatcherClaim
	for i, item := range items {
		estimate, err := estimateGas(ctx, client, ethereum.CallMsg{From: from, To: &vault, Data: vault2.PackClaim(item.NoId, item.RootIndex, item.Account, item.Proof)})
		if err != nil {
			refusals[i] = revertError(err)
			continue
		}
		batch.GasPerClaim = max(batch.GasPerClaim, estimate)
		batch.Items, offered = append(batch.Items, i), append(offered, item)
	}
	if len(offered) < 2 {
		return refusals, nil, nil
	}
	count, calldata, gasLimit, err := SizeClaimBatch(ctx, client, from, batcher, vault, offered, batch.GasPerClaim)
	if err != nil {
		return refusals, nil, err
	}
	batch.Items, batch.Calldata, batch.GasLimit = batch.Items[:count], calldata, gasLimit
	return refusals, batch, nil
}

// ClaimBatchOutcome is one batch item's outcome: attempted and paid Amount,
// attempted and refused with revert data starting Selector, or not attempted.
type ClaimBatchOutcome struct {
	Attempted bool
	Amount    *big.Int
	Selector  [4]byte
}

// ClaimBatchOutcomes reads the outcome of each of a batch's items from the
// batcher's events in its successful receipt: one ClaimPaid or ClaimRefused
// per attempted item, in order, then one ClaimsNotAttempted for the rest.
// Any other batcher event refuses the receipt.
func ClaimBatchOutcomes(receipt *types.Receipt, batcher common.Address, items int) ([]ClaimBatchOutcome, error) {
	outcomes := make([]ClaimBatchOutcome, 0, items)
	for _, log := range receipt.Logs {
		if log == nil || log.Address != batcher {
			continue
		}
		var item *big.Int
		outcome := ClaimBatchOutcome{Attempted: true}
		if paid, err := claimBatcher.UnpackClaimPaidEvent(log); err == nil {
			item, outcome.Amount = paid.Item, paid.Amount
		} else if refused, err := claimBatcher.UnpackClaimRefusedEvent(log); err == nil {
			item, outcome.Selector = refused.Item, refused.Selector
		} else if rest, err := claimBatcher.UnpackClaimsNotAttemptedEvent(log); err == nil {
			item, outcome.Attempted = rest.Item, false
		}
		if item == nil || len(outcomes) == items || item.Cmp(big.NewInt(int64(len(outcomes)))) != 0 {
			return nil, fmt.Errorf("batch receipt %s has a batcher event out of order for its %d items", receipt.TxHash.Hex(), items)
		}
		outcomes = append(outcomes, outcome)
		for !outcome.Attempted && len(outcomes) < items {
			outcomes = append(outcomes, outcome)
		}
	}
	if len(outcomes) != items {
		return nil, fmt.Errorf("batch receipt %s reports %d of its %d items", receipt.TxHash.Hex(), len(outcomes), items)
	}
	return outcomes, nil
}
