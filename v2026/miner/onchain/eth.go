package onchain

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/evmrpc"
)

const (
	dialTimeout        = 15 * time.Second
	callTimeout        = 30 * time.Second
	minedTimeout       = 10 * time.Minute
	minedPollEvery     = 3 * time.Second
	finalizedPollEvery = 3 * time.Second
)

// dialFirst tries each --rpc URL in order (failover) and returns the first
// endpoint that dials and answers eth_chainId.
func dialFirst(ctx context.Context, urls []string) (*ethclient.Client, *big.Int, string, error) {
	if ctx == nil {
		return nil, nil, "", errors.New("onchain dial owner is unavailable")
	}
	var errs []error
	for _, url := range urls {
		client, chainID, err := dialOne(ctx, url)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			if ctx.Err() != nil || !retryableOnchainRead(err, false) {
				break
			}
			continue
		}
		return client, chainID, url, nil
	}
	return nil, nil, "", fmt.Errorf("no rpc endpoint reachable: %w", errors.Join(errs...))
}

func dialOne(ctx context.Context, url string) (*ethclient.Client, *big.Int, error) {
	var client *ethclient.Client
	chainID, err := retryOnchainRead(ctx, func(ctx context.Context) (*big.Int, error) {
		dctx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		var err error
		client, err = evmrpc.DialContext(dctx, url)
		if err != nil {
			return nil, err
		}
		chainID, err := client.ChainID(dctx)
		if err != nil {
			client.Close()
			client = nil
		}
		return chainID, err
	})
	if err != nil {
		if client != nil {
			client.Close()
		}
		return nil, nil, err
	}
	return client, chainID, nil
}

// ethCall runs eth_call against the contract and surfaces revert reasons.
func ethCall(ctx context.Context, client *ethclient.Client, contract common.Address, data []byte) ([]byte, error) {
	message := ethereum.CallMsg{To: &contract, Data: append([]byte(nil), data...)}
	out, err := retryOnchainRead(ctx, func(ctx context.Context) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return client.CallContract(cctx, message, nil)
	})
	if err != nil {
		return nil, revertError(err)
	}
	return out, nil
}

// revertError augments an eth_call/eth_estimateGas error with the decoded
// revert payload when the endpoint returned one: Error(string) require
// reasons, Panic(uint256), or a custom error known to the settlement-vault ABI.
func revertError(err error) error {
	data := onchainRevertData(err)
	if len(data) == 0 {
		return err
	}
	if reason, uerr := abi.UnpackRevert(data); uerr == nil {
		return fmt.Errorf("%w: revert %q", err, reason)
	}
	if len(data) >= 4 {
		if pabi, aerr := parsedABI(); aerr == nil {
			if custom, cerr := pabi.ErrorByID([4]byte(data[:4])); cerr == nil {
				if vals, verr := custom.Unpack(data); verr == nil {
					return fmt.Errorf("%w: revert %s%v", err, custom.Name, vals)
				}
				return fmt.Errorf("%w: revert %s", err, custom.Name)
			}
		}
	}
	return fmt.Errorf("%w: revert data 0x%s", err, hex.EncodeToString(data))
}

func hexErrorData(v interface{}) []byte {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(s), "0x"))
	if err != nil {
		return nil
	}
	return b
}

func estimateGas(ctx context.Context, client interface {
	EstimateGas(context.Context, ethereum.CallMsg) (uint64, error)
}, msg ethereum.CallMsg) (uint64, error) {
	return retryOnchainRead(ctx, func(ctx context.Context) (uint64, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return client.EstimateGas(cctx, msg)
	})
}

// txRequest is a prepared contract call for runTx.
type txRequest struct {
	contract        common.Address
	from            common.Address
	key             *ecdsa.PrivateKey
	calldata        []byte
	nonceFloor      uint64
	gasLimit        uint64 // 0 = estimate + 20% headroom
	dryRun          bool
	prepared        func(common.Hash, []byte) error
	beforeBroadcast func(common.Hash) error
	broadcast       func(common.Hash) error
	admitRuntime    func(context.Context, *ethclient.Client, *big.Int) error
}

// runTx runs the submit lifecycle shared by every relayed transaction: an
// eth_call preflight (surfacing revert reasons before spending gas), a gas
// estimate, the caller's intent block via printIntent, a stop on --dry-run,
// and otherwise sign + send + wait-mined. It returns the mined receipt (nil on
// dry run) for the caller to decode command-specific events. printIntent is
// passed the resolved gas estimate; gasErr is non-nil when estimation failed
// and an explicit --gas_limit is being used instead.
func runTx(
	ctx context.Context,
	client *ethclient.Client,
	chainID *big.Int,
	req txRequest,
	printIntent func(gasEst uint64, gasErr error),
) (*types.Receipt, error) {
	msg := ethereum.CallMsg{From: req.from, To: &req.contract, Data: req.calldata}
	if _, err := retryOnchainRead(ctx, func(ctx context.Context) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return client.CallContract(cctx, msg, nil)
	}); err != nil {
		return nil, fmt.Errorf("preflight eth_call failed: %w", revertError(err))
	}
	gasEst, estErr := estimateGas(ctx, client, msg)
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(estErr, err)
	}
	if estErr != nil && req.gasLimit == 0 {
		return nil, fmt.Errorf("estimateGas: %w", revertError(estErr))
	}

	printIntent(gasEst, estErr)

	if req.dryRun {
		fmt.Println("dry run: preflight ok, nothing sent")
		return nil, nil
	}

	gasLimit := req.gasLimit
	if gasLimit == 0 {
		gasLimit = gasEst + gasEst/5 // +20% headroom
	}
	nonce, err := retryOnchainRead(ctx, func(ctx context.Context) (uint64, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return client.PendingNonceAt(cctx, req.from)
	})
	if err != nil {
		return nil, fmt.Errorf("pending nonce: %w", err)
	}
	// Another endpoint may not yet see our previous durable signed intent.
	// Never reuse that nonce merely because its send acknowledgment timed out.
	nonce = max(nonce, req.nonceFloor)
	gasPrice, err := retryOnchainRead(ctx, func(ctx context.Context) (*big.Int, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return client.SuggestGasPrice(cctx)
	})
	if err != nil {
		return nil, fmt.Errorf("gas price: %w", err)
	}
	if req.admitRuntime != nil {
		if err := req.admitRuntime(ctx, client, nil); err != nil {
			return nil, fmt.Errorf("runtime admission before EVM signing: %w", err)
		}
	}
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &req.contract,
		Value:    big.NewInt(0),
		Data:     req.calldata,
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), req.key)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("encode signed transaction: %w", err)
	}
	if req.prepared != nil {
		if err := req.prepared(signed.Hash(), append([]byte(nil), raw...)); err != nil {
			return nil, fmt.Errorf("persist prepared transaction: %w", err)
		}
	} else if _, err := fmt.Printf("prepared: tx %s raw 0x%x\n", signed.Hash(), raw); err != nil {
		return nil, fmt.Errorf("print prepared transaction: %w", err)
	}
	if req.admitRuntime != nil {
		if err := req.admitRuntime(ctx, client, nil); err != nil {
			return nil, fmt.Errorf("runtime admission before EVM broadcast: %w", err)
		}
	}
	if req.beforeBroadcast != nil {
		if err := req.beforeBroadcast(signed.Hash()); err != nil {
			return nil, fmt.Errorf("persist uncertain transaction: %w", err)
		}
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return nil, fmt.Errorf("send: %w", revertError(err))
	}
	if req.broadcast != nil {
		if err := req.broadcast(signed.Hash()); err != nil {
			return nil, fmt.Errorf("persist broadcast transaction: %w", err)
		}
	}
	fmt.Printf("sent: tx %s (nonce %d, gas %d, gasPrice %s)\n", signed.Hash(), nonce, gasLimit, gasPrice)
	fmt.Println("waiting to be mined...")

	wctx, cancel := context.WithTimeout(ctx, minedTimeout)
	defer cancel()
	receipt, err := waitMined(wctx, client, signed.Hash())
	if err != nil {
		return nil, err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, fmt.Errorf("tx %s reverted on-chain (status 0, block %s, gas used %d)",
			signed.Hash(), receipt.BlockNumber, receipt.GasUsed)
	}
	fmt.Printf("mined: block %s, status success, gas used %d; waiting for finality...\n", receipt.BlockNumber, receipt.GasUsed)
	if err := waitFinalized(wctx, client, receipt); err != nil {
		return nil, err
	}
	fmt.Printf("finalized: tx %s in canonical block %s (%s)\n", signed.Hash(), receipt.BlockNumber, receipt.BlockHash)
	return receipt, nil
}

// Receipt absence and transient transport failures retain the original mined
// deadline. Neither retries the transaction or consumes another signed nonce.
func waitMined(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	if ctx == nil || client == nil {
		return nil, errors.New("onchain receipt reader is unavailable")
	}
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if ownerErr := ctx.Err(); ownerErr != nil {
			return nil, errors.Join(err, ownerErr)
		}
		if err == nil {
			return receipt, nil
		}
		if !retryableOnchainRead(err, true) {
			return nil, err
		}
		hooks, _ := ctx.Value(onchainReadRetryHooksKey{}).(onchainReadRetryHooks)
		if waitErr := waitOnchainRead(ctx, hooks, minedPollEvery); waitErr != nil {
			return nil, fmt.Errorf("tx %s not mined yet: %w (it may still land — check the explorer before retrying)",
				txHash, errors.Join(err, waitErr))
		}
	}
}

// waitFinalized closes both the canonical receipt and the actual finalized
// frontier. Transient reads retry within the original deadline; only a decoded
// mismatched identity is evidence that the original block changed.
// A timeout is deliberately ambiguous and callers must not blindly retry the
// same intent/nonce.
func waitFinalized(ctx context.Context, client *ethclient.Client, receipt *types.Receipt) error {
	if receipt == nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Sign() <= 0 || receipt.BlockHash == (common.Hash{}) {
		return errors.New("cannot finalize an incomplete receipt")
	}
	if ctx == nil || client == nil {
		return errors.New("EVM finality reader is unavailable")
	}
	for {
		ready, err := func() (bool, error) {
			head, err := ReadEVMBlockIdentity(ctx, client, big.NewInt(int64(rpc.FinalizedBlockNumber)))
			if err != nil {
				return false, fmt.Errorf("read finalized EVM head: %w", err)
			}
			if head.Number < receipt.BlockNumber.Uint64() {
				return false, nil
			}
			canonical, canonicalErr := ReadEVMBlockIdentity(ctx, client, receipt.BlockNumber)
			if canonicalErr != nil {
				return false, fmt.Errorf("read canonical inclusion block %s: %w", receipt.BlockNumber, canonicalErr)
			}
			if canonical.Hash != receipt.BlockHash {
				return false, fmt.Errorf("tx inclusion block %s was reorged: receipt %s canonical %s", receipt.BlockNumber, receipt.BlockHash, canonical.Hash)
			}
			closing, err := ReadEVMBlockIdentity(ctx, client, big.NewInt(int64(rpc.FinalizedBlockNumber)))
			if err != nil {
				return false, fmt.Errorf("read closing finalized EVM head: %w", err)
			}
			if closing.Number < head.Number {
				// A stale frontier supplies no finality, but is not a proven
				// canonical replacement. Keep the original receipt pending.
				return false, nil
			}
			// Advancing finality may have a different hash. Both its canonical
			// identity and the original witness must still agree with the Rpc.
			witnesses := []EVMBlockIdentity{closing}
			if closing != head {
				witnesses = append(witnesses, head)
			}
			for _, witness := range witnesses {
				observed, err := ReadEVMBlockIdentity(ctx, client, new(big.Int).SetUint64(witness.Number))
				if err != nil {
					return false, fmt.Errorf("read canonical finalized witness %d: %w", witness.Number, err)
				}
				if observed != witness {
					return false, fmt.Errorf("finalized EVM witness %d is not canonical at its original hash", witness.Number)
				}
			}
			canonical, err = ReadEVMBlockIdentity(ctx, client, receipt.BlockNumber)
			if err != nil {
				return false, fmt.Errorf("read closing canonical inclusion block %s: %w", receipt.BlockNumber, err)
			}
			if canonical.Hash != receipt.BlockHash {
				return false, fmt.Errorf("tx inclusion block %s was reorged during finality observation", receipt.BlockNumber)
			}
			return true, nil
		}()
		if ctx.Err() != nil {
			return fmt.Errorf("tx %s mined but finality was not observed: %w (do not retry without checking its nonce and chain state)", receipt.TxHash, ctx.Err())
		}
		if ready {
			return nil
		}
		if err != nil && !retryableOnchainRead(err, true) {
			return err
		}
		hooks, _ := ctx.Value(onchainReadRetryHooksKey{}).(onchainReadRetryHooks)
		if waitErr := waitOnchainRead(ctx, hooks, finalizedPollEvery); waitErr != nil {
			return fmt.Errorf("tx %s mined but finality was not observed: %w (do not retry without checking its nonce and chain state)", receipt.TxHash, errors.Join(err, waitErr))
		}
	}
}
