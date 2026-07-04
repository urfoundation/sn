package main

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
)

const (
	dialTimeout    = 15 * time.Second
	callTimeout    = 30 * time.Second
	minedTimeout   = 10 * time.Minute
	minedPollEvery = 3 * time.Second
)

// dialFirst tries each --rpc URL in order (failover) and returns the first
// endpoint that dials and answers eth_chainId.
func dialFirst(ctx context.Context, urls []string) (*ethclient.Client, *big.Int, string, error) {
	var errs []error
	for _, url := range urls {
		client, chainID, err := dialOne(ctx, url)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		return client, chainID, url, nil
	}
	return nil, nil, "", fmt.Errorf("no rpc endpoint reachable: %w", errors.Join(errs...))
}

func dialOne(ctx context.Context, url string) (*ethclient.Client, *big.Int, error) {
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	client, err := ethclient.DialContext(dctx, url)
	if err != nil {
		return nil, nil, err
	}
	chainID, err := client.ChainID(dctx)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return client, chainID, nil
}

// ethCall runs eth_call against the contract and surfaces revert reasons.
func ethCall(ctx context.Context, client *ethclient.Client, contract common.Address, data []byte) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := client.CallContract(cctx, ethereum.CallMsg{To: &contract, Data: data}, nil)
	if err != nil {
		return nil, revertError(err)
	}
	return out, nil
}

// revertError augments an eth_call/eth_estimateGas error with the decoded
// revert payload when the endpoint returned one: Error(string) require
// reasons, Panic(uint256), or a custom error known to the STSubnet ABI.
func revertError(err error) error {
	var de rpc.DataError
	if !errors.As(err, &de) {
		return err
	}
	data := hexErrorData(de.ErrorData())
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
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return client.EstimateGas(cctx, msg)
}

// txRequest is a prepared contract call for runTx.
type txRequest struct {
	contract common.Address
	from     common.Address
	key      *ecdsa.PrivateKey
	calldata []byte
	gasLimit uint64 // 0 = estimate + 20% headroom
	dryRun   bool
}

// runTx runs the submit lifecycle shared by submit/bind-head/unbind-head: an
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
	{
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		_, cerr := client.CallContract(cctx, msg, nil)
		cancel()
		if cerr != nil {
			return nil, fmt.Errorf("preflight eth_call failed: %w", revertError(cerr))
		}
	}
	gasEst, estErr := estimateGas(ctx, client, msg)
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
	nonce, err := client.PendingNonceAt(ctx, req.from)
	if err != nil {
		return nil, fmt.Errorf("pending nonce: %w", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("gas price: %w", err)
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
	if err := client.SendTransaction(ctx, signed); err != nil {
		return nil, fmt.Errorf("send: %w", revertError(err))
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
	fmt.Printf("mined: block %s, status success, gas used %d\n", receipt.BlockNumber, receipt.GasUsed)
	return receipt, nil
}

// waitMined polls for the receipt of txHash until it is mined or ctx expires.
func waitMined(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, ethereum.NotFound) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("tx %s not mined yet: %w (it may still land — check the explorer before retrying)",
				txHash, ctx.Err())
		case <-time.After(minedPollEvery):
		}
	}
}
