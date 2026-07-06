package validator

// chain.go — the validator's EVM chain client (PLAN.md §7.2 / §11.1):
// ordered-failover ethclient dialing + STSubnet reads/writes through the
// sn/stabi abigen-v2 bindings (Pack/Unpack style, cribbed from stctl), plus
// the auxiliary piece the validator needs beyond the contract:
//
//   - IMetagraph (0x802) eth_calls with hand-encoded calldata (getUidCount /
//     getHotkey) to resolve an operator's minerHotkey -> live UID for
//     steering (the vendored interface in evm/src/interfaces/metagraph.sol
//     is the ABI source; unverified against the live runtime — SP-1).
//
// The effort-bounty wrappers (registerValidator / submitTrails / prove /
// reseed / claimValidator and their views) are deferred to the bounty phase
// (WHITEPAPER §9.3, D23); implementation parked at docs/parked/. The v1
// validator sends no transactions — sendAndWait stays as the generic signed
// tx path for when that phase lands.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/crypto/sha3"

	"github.com/urfoundation/sn/stabi"
)

const (
	chainDialTimeout      = 15 * time.Second
	chainCallTimeout      = 30 * time.Second
	chainSendTimeout      = 60 * time.Second
	chainWaitMinedTimeout = 5 * time.Minute
)

// metagraphAddress is the IMetagraph precompile (0x802).
var metagraphAddress = common.HexToAddress("0x0000000000000000000000000000000000000802")

// ChainClient is a live connection to one answering EVM rpc endpoint with
// the STSubnet binding attached.
type ChainClient struct {
	client       *ethclient.Client
	rpcUrl       string
	chainId      *big.Int
	st           *stabi.STSubnet
	contract     *bind.BoundContract
	contractAddr common.Address
}

// DialChain tries rpcUrls in order until one answers eth_chainId
// (§11.1: every chain consumer takes an ordered endpoint list).
func DialChain(rpcUrls []string, contractAddr common.Address) (*ChainClient, error) {
	if len(rpcUrls) == 0 {
		return nil, fmt.Errorf("no --rpc endpoints configured")
	}
	var errs []error
	for _, url := range rpcUrls {
		ctx, cancel := context.WithTimeout(context.Background(), chainDialTimeout)
		client, err := ethclient.DialContext(ctx, url)
		if err != nil {
			cancel()
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		chainId, err := client.ChainID(ctx)
		cancel()
		if err != nil {
			client.Close()
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		c := &ChainClient{
			client:       client,
			rpcUrl:       url,
			chainId:      chainId,
			st:           stabi.NewSTSubnet(),
			contractAddr: contractAddr,
		}
		c.contract = c.st.Instance(client, contractAddr)
		return c, nil
	}
	return nil, fmt.Errorf("no rpc endpoint answered: %w", errors.Join(errs...))
}

func (self *ChainClient) Close() {
	self.client.Close()
}

func (self *ChainClient) ChainId() *big.Int {
	return new(big.Int).Set(self.chainId)
}

func (self *ChainClient) RpcUrl() string {
	return self.rpcUrl
}

// chainView performs one read against the bound STSubnet contract.
func chainView[T any](c *ChainClient, calldata []byte, unpack func([]byte) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	return bind.Call(c.contract, &bind.CallOpts{Context: ctx}, calldata, unpack)
}

func (self *ChainClient) BlockNumber() (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	return self.client.BlockNumber(ctx)
}

// BlockHash returns the hash of a block by number.
func (self *ChainClient) BlockHash(number uint64) ([32]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	header, err := self.client.HeaderByNumber(ctx, new(big.Int).SetUint64(number))
	if err != nil {
		return [32]byte{}, err
	}
	return [32]byte(header.Hash()), nil
}

// --- STSubnet views ---

func (self *ChainClient) Epoch() (*big.Int, error) {
	return chainView(self, self.st.PackEpoch(), self.st.UnpackEpoch)
}

func (self *ChainClient) PendingEpoch() (*big.Int, error) {
	return chainView(self, self.st.PackPendingEpoch(), self.st.UnpackPendingEpoch)
}

func (self *ChainClient) Netuid() (uint16, error) {
	return chainView(self, self.st.PackNetuid(), self.st.UnpackNetuid)
}

func (self *ChainClient) EpochCloseBlock(e *big.Int) (uint64, error) {
	return chainView(self, self.st.PackEpochCloseBlock(e), self.st.UnpackEpochCloseBlock)
}

// EpochStartBlock reads epochStartBlock() — the block the open epoch began at.
// It is the lower bound of the open epoch's deposit window (§8.1, D25): every
// Deposited event at or after it belongs to the current epoch's demand signal.
func (self *ChainClient) EpochStartBlock() (uint64, error) {
	return chainView(self, self.st.PackEpochStartBlock(), self.st.UnpackEpochStartBlock)
}

func (self *ChainClient) TrailsWindowBlocks() (uint64, error) {
	return chainView(self, self.st.PackTrailsWindowBlocks(), self.st.UnpackTrailsWindowBlocks)
}

func (self *ChainClient) Finalized(e *big.Int) (bool, error) {
	return chainView(self, self.st.PackFinalized(e), self.st.UnpackFinalized)
}

func (self *ChainClient) OperatorCount() (*big.Int, error) {
	return chainView(self, self.st.PackOperatorCount(), self.st.UnpackOperatorCount)
}

func (self *ChainClient) OperatorIds(i *big.Int) (*big.Int, error) {
	return chainView(self, self.st.PackOperatorIds(i), self.st.UnpackOperatorIds)
}

func (self *ChainClient) Operators(noId *big.Int) (stabi.OperatorsOutput, error) {
	return chainView(self, self.st.PackOperators(noId), self.st.UnpackOperators)
}

// --- Deposited event log (D25) — the per-NO deposit record ---
//
// v0.4/D25 dropped the on-chain deposit ledger (DT/totalDT): the contract stakes
// each deposit into the locked reserve and emits Deposited(e, noId, from, amount)
// but computes no weight. The event log IS the authoritative, published per-NO
// deposit record (WHITEPAPER §7.5) — validators sum it themselves to weight the
// pools (§8.1): the open epoch's deposits for the demand signal, the all-time
// cumulative for the conviction tier (§7.2).

// depositedTopic0 is topic0 of the Deposited log — keccak256 of the canonical
// event signature (the goldens in chain_test cross-check it).
var depositedTopic0 = keccak256([]byte("Deposited(uint256,uint256,address,uint256)"))

// getLogsChunkBlocks caps a single eth_getLogs block span. Public RPCs commonly
// reject wider ranges, so an all-time conviction scan is chunked (and cached
// incrementally across tempos by the caller so it happens rarely).
const getLogsChunkBlocks = uint64(10_000)

// DepositSums are per-NO summed deposit amounts (rao), keyed by noId.String()
// (a *big.Int is not a map key). Missing NO ⇒ zero.
type DepositSums map[string]*big.Int

// add folds one deposit into the per-NO running total.
func (self DepositSums) add(noId *big.Int, amount *big.Int) {
	key := noId.String()
	if self[key] == nil {
		self[key] = new(big.Int)
	}
	self[key].Add(self[key], amount)
}

// Get returns the summed deposits for noId (zero when the NO never deposited).
func (self DepositSums) Get(noId *big.Int) *big.Int {
	if v, ok := self[noId.String()]; ok {
		return v
	}
	return new(big.Int)
}

// DepositedSums scans the Deposited event log over [fromBlock, toBlock] and sums
// `amount` per noId (WHITEPAPER §7.5). When epochFilter is non-nil only that
// epoch's events are summed (topic1 = e), which is exactly the open epoch's
// demand signal; nil sums every epoch (the all-time conviction total, §7.2).
// The range is chunked so a range-capped RPC still answers.
func (self *ChainClient) DepositedSums(fromBlock uint64, toBlock uint64, epochFilter *big.Int) (DepositSums, error) {
	sums := DepositSums{}
	if toBlock < fromBlock {
		return sums, nil
	}
	topics := [][]common.Hash{{common.Hash(depositedTopic0)}}
	if epochFilter != nil {
		topics = append(topics, []common.Hash{common.BigToHash(epochFilter)})
	}
	for from := fromBlock; ; from += getLogsChunkBlocks {
		to := from + getLogsChunkBlocks - 1
		if to > toBlock {
			to = toBlock
		}
		ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
		logs, err := self.client.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: []common.Address{self.contractAddr},
			Topics:    topics,
		})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d]: %w", from, to, err)
		}
		for i := range logs {
			event, err := self.st.UnpackDepositedEvent(&logs[i])
			if err != nil {
				// Not a Deposited log we can decode (topic0 collision / reorg
				// artifact) — skip it rather than fail the whole scan.
				continue
			}
			sums.add(event.NoId, event.Amount)
		}
		if to == toBlock {
			break
		}
	}
	return sums, nil
}

// HeadClientIdToHotkey reads the head binding headClientIdToHotkey(clientId)
// (§11.2/§11.4). clientId is a provider's 32-byte client Ed25519 key (ckey);
// the returned hotkey is the zero word when the ckey is not bound to a
// top-level miner. The reverse map headHotkeyToClientId exists too but is not
// needed at steer time — we walk from the measured provider's ckey inward.
func (self *ChainClient) HeadClientIdToHotkey(clientId [32]byte) ([32]byte, error) {
	return chainView(self, self.st.PackHeadClientIdToHotkey(clientId), self.st.UnpackHeadClientIdToHotkey)
}

// --- STSubnet transactions (no v1 callers — the effort-bounty writes are
// deferred to the bounty phase; the generic path stays) ---

// sendAndWait signs + broadcasts calldata to the contract and waits for the
// receipt, decoding revert reasons best-effort (stctl conventions).
func (self *ChainClient) sendAndWait(key *ecdsa.PrivateKey, calldata []byte) (*types.Receipt, error) {
	opts := bind.NewKeyedTransactor(key, self.chainId)
	sendCtx, cancelSend := context.WithTimeout(context.Background(), chainSendTimeout)
	defer cancelSend()
	opts.Context = sendCtx

	from := crypto.PubkeyToAddress(key.PublicKey)
	tx, err := self.contract.RawTransact(opts, calldata)
	if err != nil {
		return nil, fmt.Errorf("send tx to %s: %w", self.contractAddr, self.explainCallError(err))
	}
	fmt.Printf("tx sent: %s (from %s nonce %d, %d byte calldata)\n", tx.Hash(), from, tx.Nonce(), len(calldata))

	waitCtx, cancelWait := context.WithTimeout(context.Background(), chainWaitMinedTimeout)
	defer cancelWait()
	receipt, err := bind.WaitMined(waitCtx, self.client, tx.Hash())
	if err != nil {
		return nil, fmt.Errorf("wait mined %s: %w", tx.Hash(), err)
	}
	status := "SUCCESS"
	if receipt.Status != types.ReceiptStatusSuccessful {
		status = "REVERTED"
	}
	fmt.Printf("tx mined: block %d status %s gas %d\n", receipt.BlockNumber, status, receipt.GasUsed)
	if receipt.Status != types.ReceiptStatusSuccessful {
		reason := self.replayRevertReason(from, calldata, receipt.BlockNumber)
		if reason != "" {
			return receipt, fmt.Errorf("transaction %s reverted: %s", tx.Hash(), reason)
		}
		return receipt, fmt.Errorf("transaction %s reverted", tx.Hash())
	}
	return receipt, nil
}

func (self *ChainClient) replayRevertReason(from common.Address, calldata []byte, blockNumber *big.Int) string {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	msg := ethereum.CallMsg{From: from, To: &self.contractAddr, Data: calldata}
	_, err := self.client.CallContract(ctx, msg, blockNumber)
	if err == nil {
		return ""
	}
	return decodeRevertError(self.st, err)
}

func (self *ChainClient) explainCallError(err error) error {
	if reason := decodeRevertError(self.st, err); reason != "" {
		return fmt.Errorf("%w (revert: %s)", err, reason)
	}
	return err
}

// decodeRevertError extracts a revert reason from an rpc error:
// Error(string) via abi.UnpackRevert, then STSubnet custom errors.
func decodeRevertError(st *stabi.STSubnet, err error) string {
	var dataErr rpc.DataError
	if !errors.As(err, &dataErr) {
		return ""
	}
	dataHex, ok := dataErr.ErrorData().(string)
	if !ok {
		return ""
	}
	raw := common.FromHex(dataHex)
	if len(raw) < 4 {
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

// --- IMetagraph (0x802) reads — hand-encoded eth_calls ---

// keccak256 with legacy Keccak-256 (the EVM hash).
func keccak256(data ...[]byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	for _, d := range data {
		h.Write(d)
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// evmSelector returns the 4-byte selector of a canonical abi signature.
func evmSelector(signature string) [4]byte {
	hash := keccak256([]byte(signature))
	var selector [4]byte
	copy(selector[:], hash[:4])
	return selector
}

// evmUint16Word encodes a uint16 as one abi word.
func evmUint16Word(v uint16) [32]byte {
	var word [32]byte
	word[30] = byte(v >> 8)
	word[31] = byte(v)
	return word
}

func (self *ChainClient) ethCall(to common.Address, calldata []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	return self.client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: calldata}, nil)
}

// MetagraphUidCount calls IMetagraph.getUidCount(netuid) on 0x802.
func (self *ChainClient) MetagraphUidCount(netuid uint16) (uint16, error) {
	selector := evmSelector("getUidCount(uint16)")
	arg := evmUint16Word(netuid)
	out, err := self.ethCall(metagraphAddress, append(selector[:], arg[:]...))
	if err != nil {
		return 0, fmt.Errorf("metagraph getUidCount: %w", err)
	}
	if len(out) < 32 {
		return 0, fmt.Errorf("metagraph getUidCount: short return (%d bytes)", len(out))
	}
	return uint16(out[30])<<8 | uint16(out[31]), nil
}

// MetagraphHotkey calls IMetagraph.getHotkey(netuid, uid) on 0x802.
func (self *ChainClient) MetagraphHotkey(netuid uint16, uid uint16) ([32]byte, error) {
	selector := evmSelector("getHotkey(uint16,uint16)")
	argNetuid := evmUint16Word(netuid)
	argUid := evmUint16Word(uid)
	calldata := append(selector[:], argNetuid[:]...)
	calldata = append(calldata, argUid[:]...)
	out, err := self.ethCall(metagraphAddress, calldata)
	if err != nil {
		return [32]byte{}, fmt.Errorf("metagraph getHotkey: %w", err)
	}
	if len(out) < 32 {
		return [32]byte{}, fmt.Errorf("metagraph getHotkey: short return (%d bytes)", len(out))
	}
	var hotkey [32]byte
	copy(hotkey[:], out[:32])
	return hotkey, nil
}

// FindUidByHotkey linearly scans the metagraph for a hotkey — the same
// bounded scan STSubnet._findUid performs (max_uids <= 256). Returns
// (uid, true) when found.
func (self *ChainClient) FindUidByHotkey(netuid uint16, hotkey [32]byte) (uint16, bool, error) {
	n, err := self.MetagraphUidCount(netuid)
	if err != nil {
		return 0, false, err
	}
	for uid := uint16(0); uid < n; uid++ {
		hk, err := self.MetagraphHotkey(netuid, uid)
		if err != nil {
			return 0, false, err
		}
		if hk == hotkey {
			return uid, true, nil
		}
	}
	return 0, false, nil
}
