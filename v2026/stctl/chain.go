package main

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/stabi"
)

const (
	// dialTimeout bounds each per-endpoint dial + chain-id probe.
	dialTimeout = 15 * time.Second
	// callTimeout bounds each view call.
	callTimeout = 30 * time.Second
	// sendTimeout bounds building + broadcasting one transaction.
	sendTimeout = 60 * time.Second
	// waitMinedTimeout bounds waiting for a receipt (~12s blocks).
	waitMinedTimeout = 5 * time.Minute
)

// session is a live connection to one answering rpc endpoint, with the
// STSubnet binding attached at the configured contract address.
type session struct {
	cfg          *Config
	client       *ethclient.Client
	rpcUrl       string
	chainId      *big.Int
	st           *stabi.STSubnet
	contract     *bind.BoundContract
	contractAddr common.Address
}

// dialSession dials cfg.RpcUrls in order until one answers eth_chainId,
// then asserts the chain id against the config. Endpoints that fail to
// dial or answer are skipped (transient failures roll to the next url).
func dialSession(cfg *Config) (*session, error) {
	var errs []error
	for _, url := range cfg.RpcUrls {
		ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
		client, err := ethclient.DialContext(ctx, url)
		if err != nil {
			cancel()
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			vlog(1, "rpc %s: dial failed: %v", url, err)
			continue
		}
		chainId, err := client.ChainID(ctx)
		cancel()
		if err != nil {
			client.Close()
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			vlog(1, "rpc %s: eth_chainId failed: %v", url, err)
			continue
		}
		if chainId.Cmp(new(big.Int).SetUint64(cfg.ChainId)) != 0 {
			client.Close()
			return nil, fmt.Errorf(
				"rpc %s reports chain id %s, config expects %d (945 testnet, 964 mainnet)",
				url, chainId, cfg.ChainId,
			)
		}
		vlog(1, "rpc %s: connected (chain id %s)", url, chainId)
		s := &session{
			cfg:     cfg,
			client:  client,
			rpcUrl:  url,
			chainId: chainId,
			st:      stabi.NewSTSubnet(),
		}
		if addr, err := cfg.contractAddr(); err == nil {
			s.contractAddr = addr
			s.contract = s.st.Instance(client, addr)
		}
		return s, nil
	}
	return nil, fmt.Errorf("no rpc endpoint answered: %w", errors.Join(errs...))
}

func (s *session) close() {
	s.client.Close()
}

// requireContract errors when the config has no usable contract_address.
func (s *session) requireContract() error {
	if s.contract == nil {
		_, err := s.cfg.contractAddr()
		return err
	}
	return nil
}

// view performs a read against the bound STSubnet contract.
func view[T any](s *session, calldata []byte, unpack func([]byte) (T, error)) (T, error) {
	if err := s.requireContract(); err != nil {
		var zero T
		return zero, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return bind.Call(s.contract, &bind.CallOpts{Context: ctx}, calldata, unpack)
}

// blockNumber returns the current head block number.
func (s *session) blockNumber() (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return s.client.BlockNumber(ctx)
}

// loadKey reads the hex-encoded 32-byte EVM private key from cfg.KeyFile.
func loadKey(cfg *Config) (*ecdsa.PrivateKey, common.Address, error) {
	if cfg.KeyFile == "" {
		return nil, common.Address{}, fmt.Errorf("config: key_file is not set (required for state-changing commands)")
	}
	path := expandHome(cfg.KeyFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("key_file: %w", err)
	}
	hexKey := strings.TrimSpace(string(raw))
	hexKey = strings.TrimPrefix(strings.TrimPrefix(hexKey, "0x"), "0X")
	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("key_file %s: expected a hex-encoded 32-byte key: %w", path, err)
	}
	return key, crypto.PubkeyToAddress(key.PublicKey), nil
}

// sendAndWait signs and broadcasts calldata to the given bound target,
// waits for the receipt, prints hash/status/gas, decodes STSubnet events,
// and errors (after best-effort revert decoding) when the tx reverted.
func (s *session) sendAndWait(key *ecdsa.PrivateKey, target *bind.BoundContract, targetAddr common.Address, calldata []byte) (*types.Receipt, error) {
	opts := bind.NewKeyedTransactor(key, s.chainId)
	sendCtx, cancelSend := context.WithTimeout(context.Background(), sendTimeout)
	defer cancelSend()
	opts.Context = sendCtx

	from := crypto.PubkeyToAddress(key.PublicKey)
	tx, err := target.RawTransact(opts, calldata)
	if err != nil {
		return nil, fmt.Errorf("send tx to %s: %w", targetAddr, explainCallError(s, err))
	}
	fmt.Printf("tx sent:   %s\n", tx.Hash())
	fmt.Printf("  from:    %s  nonce %d\n", from, tx.Nonce())
	fmt.Printf("  to:      %s  (%d byte calldata)\n", targetAddr, len(calldata))

	waitCtx, cancelWait := context.WithTimeout(context.Background(), waitMinedTimeout)
	defer cancelWait()
	receipt, err := bind.WaitMined(waitCtx, s.client, tx.Hash())
	if err != nil {
		return nil, fmt.Errorf("wait mined %s: %w", tx.Hash(), err)
	}
	status := "SUCCESS"
	if receipt.Status != types.ReceiptStatusSuccessful {
		status = "REVERTED"
	}
	fmt.Printf("  mined:   block %d  status %s  gas used %d\n",
		receipt.BlockNumber, status, receipt.GasUsed)
	s.printEvents(receipt)

	if receipt.Status != types.ReceiptStatusSuccessful {
		reason := s.replayRevertReason(from, targetAddr, calldata, receipt.BlockNumber)
		if reason != "" {
			return receipt, fmt.Errorf("transaction %s reverted: %s", tx.Hash(), reason)
		}
		return receipt, fmt.Errorf("transaction %s reverted", tx.Hash())
	}
	return receipt, nil
}

// sendContractTx sends calldata to the configured STSubnet contract.
func (s *session) sendContractTx(key *ecdsa.PrivateKey, calldata []byte) (*types.Receipt, error) {
	if err := s.requireContract(); err != nil {
		return nil, err
	}
	return s.sendAndWait(key, s.contract, s.contractAddr, calldata)
}

// sendRawTx sends calldata to an arbitrary address (e.g. a precompile).
// The empty ABI is fine: RawTransact never consults it.
func (s *session) sendRawTx(key *ecdsa.PrivateKey, to common.Address, calldata []byte) (*types.Receipt, error) {
	bound := bind.NewBoundContract(to, abi.ABI{}, s.client, s.client, s.client)
	return s.sendAndWait(key, bound, to, calldata)
}

// replayRevertReason re-executes a reverted tx as an eth_call at its block
// to recover the revert reason. Best effort: returns "" when unavailable.
func (s *session) replayRevertReason(from, to common.Address, calldata []byte, blockNumber *big.Int) string {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	msg := ethereum.CallMsg{From: from, To: &to, Data: calldata}
	_, err := s.client.CallContract(ctx, msg, blockNumber)
	if err == nil {
		return ""
	}
	return decodeRevertError(s.st, err)
}

// explainCallError augments gas-estimation failures (which execute the call
// and surface reverts) with a decoded revert reason where possible.
func explainCallError(s *session, err error) error {
	if reason := decodeRevertError(s.st, err); reason != "" {
		return fmt.Errorf("%w (revert: %s)", err, reason)
	}
	return err
}

// decodeRevertError extracts and decodes revert data carried in an rpc
// error: Error(string) via abi.UnpackRevert, then STSubnet custom errors.
func decodeRevertError(st *stabi.STSubnet, err error) string {
	var dataErr rpc.DataError
	if !errors.As(err, &dataErr) {
		return ""
	}
	dataHex, ok := dataErr.ErrorData().(string)
	if !ok {
		return ""
	}
	raw, decodeErr := parseHexBytes("revert data", dataHex)
	if decodeErr != nil || len(raw) < 4 {
		return ""
	}
	if reason, unpackErr := abi.UnpackRevert(raw); unpackErr == nil {
		return fmt.Sprintf("%q", reason)
	}
	if custom, unpackErr := st.UnpackError(raw); unpackErr == nil {
		return fmt.Sprintf("%T%+v", custom, custom)
	}
	return fmt.Sprintf("raw revert data 0x%x", raw)
}

// printEvents decodes and prints the STSubnet events in a receipt.
func (s *session) printEvents(receipt *types.Receipt) {
	if s.contract == nil {
		return
	}
	for _, log := range receipt.Logs {
		if log.Address != s.contractAddr {
			continue
		}
		if line, ok := s.decodeEvent(log); ok {
			fmt.Printf("  event:   %s\n", line)
		} else if verbosity >= 1 {
			fmt.Printf("  event:   (undecoded log, topic0 %s)\n", log.Topics[0])
		}
	}
}

// decodeEvent tries the known STSubnet event decoders against one log.
func (s *session) decodeEvent(log *types.Log) (string, bool) {
	st := s.st
	decoders := []func(*types.Log) (string, bool){
		event(st.UnpackDepositedEvent, func(e *stabi.STSubnetDeposited) string {
			return fmt.Sprintf("Deposited epoch=%s noId=%s from=%s amount=%s",
				e.E, e.NoId, e.From, formatAlpha(e.Amount))
		}),
		event(st.UnpackBuybackReservedEvent, func(e *stabi.STSubnetBuybackReserved) string {
			return fmt.Sprintf("BuybackReserved epoch=%s noId=%s amount=%s buybackTotal=%s",
				e.E, e.NoId, formatAlpha(e.Amount), formatAlpha(e.BuybackTotal))
		}),
		event(st.UnpackOperatorRegisteredEvent, func(e *stabi.STSubnetOperatorRegistered) string {
			return fmt.Sprintf("OperatorRegistered noId=%s coldkey=%s minerUid=%d minerHotkey=%s",
				e.NoId, formatKey32(e.Coldkey), e.MinerUid, formatKey32(e.MinerHotkey))
		}),
		event(st.UnpackOperatorCommittedEvent, func(e *stabi.STSubnetOperatorCommitted) string {
			return fmt.Sprintf("OperatorCommitted epoch=%s noId=%s payoutRoot=0x%x off=0x%x",
				e.E, e.NoId, e.PayoutRoot, e.Off)
		}),
		event(st.UnpackEpochFinalizedEvent, func(e *stabi.STSubnetEpochFinalized) string {
			return fmt.Sprintf("EpochFinalized epoch=%s", e.E)
		}),
		event(st.UnpackEpochRolledEvent, func(e *stabi.STSubnetEpochRolled) string {
			return fmt.Sprintf("EpochRolled closedEpoch=%s newEpoch=%s closeBlock=%d",
				e.ClosedEpoch, e.NewEpoch, e.CloseBlock)
		}),
		event(st.UnpackPoolFinalizedEvent, func(e *stabi.STSubnetPoolFinalized) string {
			return fmt.Sprintf("PoolFinalized epoch=%s noId=%s poolTotal=%s", e.E, e.NoId, formatAlpha(e.PoolTotal))
		}),
		event(st.UnpackPoolCarriedEvent, func(e *stabi.STSubnetPoolCarried) string {
			return fmt.Sprintf("PoolCarried epoch=%s noId=%s carried=%s", e.E, e.NoId, formatAlpha(e.Carried))
		}),
		event(st.UnpackPoolSweptEvent, func(e *stabi.STSubnetPoolSwept) string {
			return fmt.Sprintf("PoolSwept noId=%s measured=%s swept=%s moveOk=%t",
				e.NoId, formatAlpha(e.Measured), formatAlpha(e.Swept), e.MoveOk)
		}),
		event(st.UnpackMinerClaimedEvent, func(e *stabi.STSubnetMinerClaimed) string {
			return fmt.Sprintf("MinerClaimed epoch=%s noId=%s coldkey=%s shareBps=%s amount=%s caller=%s",
				e.E, e.NoId, formatKey32(e.Coldkey), e.ShareBps, formatAlpha(e.Amount), e.Caller)
		}),
	}
	for _, decode := range decoders {
		if line, ok := decode(log); ok {
			return line, true
		}
	}
	return "", false
}

// event adapts a generated Unpack<Name>Event func + formatter into a
// decoder that reports whether the log matched.
func event[E any](unpack func(*types.Log) (*E, error), format func(*E) string) func(*types.Log) (string, bool) {
	return func(log *types.Log) (string, bool) {
		parsed, err := unpack(log)
		if err != nil {
			return "", false
		}
		return format(parsed), true
	}
}

// vlog prints when verbosity is at least level.
func vlog(level int, format string, args ...any) {
	if verbosity >= level {
		fmt.Fprintf(os.Stderr, "stctl: "+format+"\n", args...)
	}
}
