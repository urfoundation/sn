// CL-1: `snclaim v2 claim --all [--batch]` against v2TestChain, with a model
// of STClaimBatcher.claimMany, and the claim batcher helpers.
package onchain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

const (
	v2TestBatcher = "0x5555555555555555555555555555555555555555"
	// The batch model spends v2TestBatchBase outside its items,
	// v2TestItemGas on each paid item and v2TestRevertGas on each that vault2
	// refuses; one the runtime refuses consumes its whole cap.
	v2TestBatchBase = 60_000
	v2TestItemGas   = 240_000
	v2TestRevertGas = 30_000
	// v2TestBatchNeeded is the gas the batcher requires for an item capped
	// at ClaimBatchGasPerClaim.
	v2TestBatchNeeded = ClaimBatchGasPerClaim + ClaimBatchGasPerClaim/63 + 20_000
)

// v2TestBatcherABI is the claim batcher's ABI.
var v2TestBatcherABI = func() *abi.ABI {
	parsed, err := stabi.STClaimBatcherMetaData.ParseABI()
	if err != nil {
		panic(err)
	}
	return parsed
}()

// answerBatch answers eth_call and eth_estimateGas of claimMany. A call
// counts the items the model attempts with its gas, the block gas limit when
// it names none. An estimate succeeds at any gas, as the batcher's does, so
// it is the gas of a batch that attempts nothing.
func (c *v2TestChain) answerBatch(method string, gas *hexutil.Uint64, input []byte) (any, map[string]any) {
	key := method + ":claimMany"
	if c.count(key) {
		return nil, rpcError(-32000, "synthetic refusal of "+key, nil)
	}
	if c.garbage["claimMany"] {
		return hexutil.Bytes{}, nil
	}
	if method == "eth_estimateGas" {
		return hexutil.EncodeUint64(v2TestBatchBase), nil
	}
	limit := c.gasLimit
	if gas != nil {
		limit = uint64(*gas)
	}
	out, _, _ := c.batch(input, limit, false)
	if c.batchAnswer != nil {
		out, _ = v2TestBatcherABI.Methods["claimMany"].Outputs.Pack(c.batchAnswer)
	}
	return hexutil.Bytes(out), nil
}

// sendBatch mines a claimMany transaction on the model.
func (c *v2TestChain) sendBatch(tx *types.Transaction) common.Hash {
	c.sent = append(c.sent, tx)
	c.count("send:claimMany")
	receipt := &types.Receipt{Status: types.ReceiptStatusFailed, TxHash: tx.Hash(), EffectiveGasPrice: big.NewInt(2), BlockHash: v2TestBlockHash(v2TestHead), BlockNumber: big.NewInt(v2TestHead), Logs: []*types.Log{}}
	if !c.revertBatch {
		_, logs, used := c.batch(tx.Data(), tx.Gas(), true)
		receipt.Status, receipt.GasUsed, receipt.CumulativeGasUsed = types.ReceiptStatusSuccessful, used, used
		for _, log := range logs {
			if c.mute || c.muteVault && log.Address == c.vault {
				continue
			}
			log.TxHash, log.BlockHash, log.BlockNumber, log.Index = tx.Hash(), receipt.BlockHash, v2TestHead, uint(len(c.logs))
			c.logs = append(c.logs, *log)
			c.emitted = append(c.emitted, log)
			receipt.Logs = append(receipt.Logs, log)
		}
	}
	c.receipts[tx.Hash()] = receipt
	return tx.Hash()
}

// batch runs claimMany on the model with gas: an item is attempted while the
// gas left covers its cap as the batcher requires; a paid item uses itemGas
// (v2TestItemGas when unset), one vault2 refuses v2TestRevertGas, and one
// refused without revert data, as the runtime refuses, its whole cap. It
// returns the output, the events and the gas used; only a send commits.
func (c *v2TestChain) batch(input []byte, gas uint64, commit bool) ([]byte, []*types.Log, uint64) {
	method := v2TestBatcherABI.Methods["claimMany"]
	if len(input) < 4 || !bytes.Equal(input[:4], method.ID) {
		c.t.Errorf("batcher call 0x%x is not claimMany", input)
		return nil, nil, 0
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil || args[0].(common.Address) != c.vault {
		c.t.Errorf("claimMany arguments %v do not name vault2: %v", args, err)
		return nil, nil, 0
	}
	items := *abi.ConvertType(args[1], new([]stabi.STClaimBatcherClaim)).(*[]stabi.STClaimBatcherClaim)
	perClaim := args[2].(*big.Int).Uint64()
	base, itemGas := max(c.batchBase, v2TestBatchBase), uint64(v2TestItemGas)
	if c.itemGas != 0 {
		itemGas = c.itemGas
	}
	left := gas - min(gas, base)
	claim := v2TestABI.Methods["claim"]
	pack := func(values ...any) []byte {
		out, err := claim.Outputs.Pack(values...)
		if err != nil {
			c.t.Error(err)
		}
		return out
	}
	var logs []*types.Log
	attempted := 0
	for ; attempted < len(items); attempted++ {
		if left < perClaim+perClaim/63+20_000 {
			logs = append(logs, c.batcherEvent("ClaimsNotAttempted", big.NewInt(int64(attempted))))
			break
		}
		item := items[attempted]
		out, paid, revert := c.claim(c.batcher, item.NoId, item.RootIndex, item.Account, item.Proof, commit, pack)
		if revert != nil {
			var selector [4]byte
			copy(selector[:], revert)
			left -= min(left, v2TestRevertGas)
			if len(revert) == 0 {
				left -= perClaim - v2TestRevertGas
			}
			logs = append(logs, c.batcherEvent("ClaimRefused", big.NewInt(int64(attempted)), selector))
			continue
		}
		left -= min(left, itemGas)
		amount, err := claim.Outputs.Unpack(out)
		if err != nil {
			c.t.Error(err)
		}
		logs = append(append(logs, paid...), c.batcherEvent("ClaimPaid", big.NewInt(int64(attempted)), amount[0]))
	}
	out, err := method.Outputs.Pack(big.NewInt(int64(attempted)))
	if err != nil {
		c.t.Error(err)
	}
	return out, logs, gas - left
}

// batcherEvent encodes a claim batcher event.
func (c *v2TestChain) batcherEvent(name string, data ...any) *types.Log {
	event := v2TestBatcherABI.Events[name]
	packed, err := event.Inputs.Pack(data...)
	if err != nil {
		c.t.Fatalf("%s data does not pack: %v", name, err)
	}
	return &types.Log{Address: c.batcher, Topics: []common.Hash{event.ID}, Data: packed}
}

// v2BatchScenario is alice as account 1 of one finalized root in each of
// pools 3, 4 and 6, with anything claimable, and in pool 5, claimed in full;
// the batcher is at v2TestBatcher.
type v2BatchScenario struct {
	c     *v2TestChain
	alice [32]byte
	trees map[uint64]*merkle.LedgerSumTree
}

// v2BatchPools are alice's pools with anything claimable, in endpoint order.
var v2BatchPools = []uint64{3, 4, 6}

func newV2BatchScenario(t *testing.T) *v2BatchScenario {
	t.Helper()
	s := &v2BatchScenario{c: newV2TestChain(t), alice: [32]byte(common.FromHex(aliceHex)), trees: map[uint64]*merkle.LedgerSumTree{}}
	s.c.batcher = common.HexToAddress(v2TestBatcher)
	var entries []v2TestEntry
	for _, pool := range []uint64{3, 4, 5, 6} {
		tree := v2TestTree(t, []merkle.LedgerAccount{{Coldkey: v2TestKey(1), Cumulative: *uint256.NewInt(1_000_000_000)}, {Coldkey: s.alice, Cumulative: *uint256.NewInt(pool * 1_000_000_000)}})
		s.trees[pool] = tree
		roots := []stabi.ISTSettlementVault2Root{v2TestRoot(tree, 10, [32]byte{byte(pool)})}
		if pool == v2TestPool {
			s.c.roots = roots
		} else {
			s.c.pools[pool] = roots
		}
		entries = append(entries, v2TestEntry{Vault: strings.ToLower(v2TestVault), NoID: pool, Index: 1, RootIndex: v2Ptr(uint64(0)), Cumulative: fmt.Sprint(pool * 1_000_000_000), Proof: v2TestProofJSON(t, tree, 1), Claimed: "0"})
	}
	s.c.claimed[[2]uint64{5, 1}] = big.NewInt(5_000_000_000)
	s.c.setAccounts(entries...)
	return s
}

// item is alice's claim in pool as the batcher takes it.
func (s *v2BatchScenario) item(t *testing.T, pool uint64) stabi.STClaimBatcherClaim {
	t.Helper()
	proof, err := s.trees[pool].Proof(1)
	if err != nil {
		t.Fatal(err)
	}
	return stabi.STClaimBatcherClaim{NoId: new(big.Int).SetUint64(pool), RootIndex: big.NewInt(0),
		Account: stabi.LedgerTreeAccount{Index: big.NewInt(1), Coldkey: s.alice, Cumulative: new(big.Int).SetUint64(pool * 1_000_000_000)}, Proof: v2ProofNodes(proof)}
}

// calldata is claimMany of alice's claims in pools, each capped at perClaim.
func (s *v2BatchScenario) calldata(t *testing.T, perClaim uint64, pools ...uint64) []byte {
	t.Helper()
	items := make([]stabi.STClaimBatcherClaim, len(pools))
	for i, pool := range pools {
		items[i] = s.item(t, pool)
	}
	return claimBatcher.PackClaimMany(common.HexToAddress(v2TestVault), items, new(big.Int).SetUint64(perClaim))
}

// single is alice's claim in pool as vault2 takes it.
func (s *v2BatchScenario) single(t *testing.T, pool uint64) []byte {
	t.Helper()
	item := s.item(t, pool)
	return vault2.PackClaim(item.NoId, item.RootIndex, item.Account, item.Proof)
}

// requireBatchTx checks a sent batch: the relayer's legacy transaction at
// nonce to the batcher, without value, with exactly calldata and a gas limit
// that the least limit attempting its items, plus 20%, can give.
func requireBatchTx(t *testing.T, tx *types.Transaction, nonce uint64, least uint64, calldata []byte) {
	t.Helper()
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(v2TestChainID)), tx)
	if err != nil || sender != common.HexToAddress(testKeyAddr) || tx.Type() != types.LegacyTxType || tx.To() == nil || *tx.To() != common.HexToAddress(v2TestBatcher) ||
		tx.Nonce() != nonce || tx.Value().Sign() != 0 || tx.Gas() < least+least/5 || tx.Gas() > least+claimBatchTolerance+(least+claimBatchTolerance)/5 || !bytes.Equal(tx.Data(), calldata) {
		t.Fatalf("signed the wrong batch: sender %s (%v) to %v nonce %d gas %d value %s data 0x%x, want nonce %d gas from %d data 0x%x",
			sender, err, tx.To(), tx.Nonce(), tx.Gas(), tx.Value(), tx.Data(), nonce, least+least/5, calldata)
	}
}

// v2BatchLeast is the least gas limit at which the model attempts every one
// of n items at v2TestItemGas each.
func v2BatchLeast(n uint64) uint64 {
	return v2TestBatchBase + (n-1)*v2TestItemGas + v2TestBatchNeeded
}

// v2BatchBlock is a block gas limit at which the tools name exactly ceiling
// gas for a batch.
func v2BatchBlock(t *testing.T, ceiling uint64) uint64 {
	t.Helper()
	limit := (ceiling + 1_500 + claimBatchGasMargin) * 15 / 13
	for claimBatchCeiling(limit) < ceiling {
		limit++
	}
	if claimBatchCeiling(limit) != ceiling {
		t.Fatalf("no block gas limit gives a ceiling of %d", ceiling)
	}
	return limit
}

// The output lines of alice's claim in pool, paid by caller.
func v2BatchPaid(pool uint64, caller string) string {
	return fmt.Sprintf("Claimed: pool %d, account 1, root 0\n  coldkey:     %s (0x%s)\n  cumulative:  %d.000000000 α (%d000000000 rao)\n  paid:        %d.000000000 α (%d000000000 rao)\n  caller:      %s\n",
		pool, aliceSS58, aliceHex, pool, pool, pool, pool, common.HexToAddress(caller).Hex())
}

// CL-1: --all without --batch claims each account with anything claimable
// alone, in endpoint order, after verifying every one; a claimed account is
// done.
func TestV2ClaimAllClaimsEachAccount(t *testing.T) {
	s := newV2BatchScenario(t)
	output, err := s.c.run(s.c.claimArgv("--all"))
	if err != nil {
		t.Fatalf("claim --all: %v\n%s", err, output)
	}
	sent := s.c.transactions()
	if len(sent) != 3 {
		t.Fatalf("claim --all sent %d transactions", len(sent))
	}
	for i, pool := range v2BatchPools {
		requireV2Tx(t, sent[i], 7+uint64(i), ClaimBatchGasPerClaim, 0, s.single(t, pool))
		if !strings.Contains(output, v2BatchPaid(pool, testKeyAddr)) {
			t.Fatalf("output lacks pool %d's claim:\n%s", pool, output)
		}
	}
	if !strings.Contains(output, "pool 5 account 1: claimed 5.000000000 α (5000000000 rao) already reaches the cumulative 5.000000000 α (5000000000 rao); nothing to claim\n") ||
		s.c.observed("eth_call", "eth_estimateGas", "send") != "eth_call:claim=3 eth_call:claimed=4 eth_call:roots=4 eth_estimateGas:claim=3 send:claim=3" {
		t.Fatalf("observed %s\n%s", s.c.observed(), output)
	}
}

// CL-1: --all --batch verifies every account, preflights each claim alone,
// then claims them in one transaction to the batcher with the least gas
// limit that attempts all, plus 20%. vault2 pays each account's coldkey,
// with the batcher as caller.
func TestV2ClaimAllBatchesTheAccounts(t *testing.T) {
	s := newV2BatchScenario(t)
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	if err != nil {
		t.Fatalf("claim --all --batch: %v\n%s", err, output)
	}
	sent := s.c.transactions()
	if len(sent) != 1 {
		t.Fatalf("batch sent %d transactions", len(sent))
	}
	requireBatchTx(t, sent[0], 7, v2BatchLeast(3), s.calldata(t, ClaimBatchGasPerClaim, v2BatchPools...))
	for _, pool := range v2BatchPools {
		if got := s.c.claimed[[2]uint64{pool, 1}]; got == nil || got.Uint64() != pool*1_000_000_000 || !strings.Contains(output, v2BatchPaid(pool, v2TestBatcher)) {
			t.Fatalf("pool %d claimed %v:\n%s", pool, got, output)
		}
	}
	for _, want := range []string{
		"settlement v2 batch claim intent\n  vault:       " + common.HexToAddress(v2TestVault).Hex() + " (chain id 945, rpc " + s.c.rpc.URL + ")\n  batcher:     " + common.HexToAddress(v2TestBatcher).Hex() + "\n  from:        " + testKeyAddr + "\n  account:     pool 3, account 1, root 0\n",
		"  claims:      3 of 3, each capped at 2600000 gas\n  gas limit:   " + fmt.Sprint(sent[0].Gas()) + " (the least that attempts every claim, by eth_call, + 20%)\n",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output lacks %q:\n%s", want, output)
		}
	}
	// The batcher's code is read once; each claim's estimate is its
	// preflight; the batch is offered at the most gas the tools name, its
	// limit found in 13 more calls, and the send preflights it once more.
	if s.c.observed() != "eth_call:claimMany=15 eth_call:claimed=4 eth_call:roots=4 eth_chainId=1 eth_estimateGas:claim=3 eth_estimateGas:claimMany=1 eth_gasPrice=1 eth_getBlockByNumber:0x30d40=3 eth_getBlockByNumber:finalized=2 eth_getBlockByNumber:latest=1 eth_getCode=1 eth_getTransactionCount=1 eth_getTransactionReceipt=1 send:claimMany=1" {
		t.Fatalf("observed %s", s.c.observed())
	}
}

// CL-1: a claim's estimate above the default raises every item's cap.
func TestV2ClaimAllBatchCapsAtTheLargestEstimate(t *testing.T) {
	s := newV2BatchScenario(t)
	s.c.claimEstimate = 2_700_000
	if output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher)); err != nil || len(s.c.transactions()) != 1 ||
		!bytes.Equal(s.c.transactions()[0].Data(), s.calldata(t, 2_700_000, v2BatchPools...)) || !strings.Contains(output, "each capped at 2700000 gas") {
		t.Fatalf("estimate above the default: %v\n%s", err, output)
	}
}

// CL-1: claims beyond what the most gas the tools name for a batch attempts,
// 13/15 of the block gas limit less 1,500 gas and the margin, go in the next
// transaction, and a last one alone to vault2.
func TestV2ClaimAllBatchSplitsAtTheBlockGasLimit(t *testing.T) {
	s := newV2BatchScenario(t)
	s.c.gasLimit = v2BatchBlock(t, v2BatchLeast(3)-1)
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	sent := s.c.transactions()
	if err != nil || len(sent) != 2 || !strings.Contains(output, "  claims:      2 of 3, each capped at 2600000 gas\n") {
		t.Fatalf("split: %v, %d transactions\n%s", err, len(sent), output)
	}
	if sent[0].Gas() != v2BatchLeast(3)-1 || !bytes.Equal(sent[0].Data(), s.calldata(t, ClaimBatchGasPerClaim, 3, 4)) || *sent[0].To() != common.HexToAddress(v2TestBatcher) {
		t.Fatalf("first transaction: gas %d to %s data 0x%x", sent[0].Gas(), sent[0].To(), sent[0].Data())
	}
	requireV2Tx(t, sent[1], 8, ClaimBatchGasPerClaim, 0, s.single(t, 6))
	for _, want := range []string{v2BatchPaid(3, v2TestBatcher), v2BatchPaid(4, v2TestBatcher), v2BatchPaid(6, testKeyAddr)} {
		if !strings.Contains(output, want) {
			t.Fatalf("output lacks %q:\n%s", want, output)
		}
	}
}

// CL-1: items a batch did not attempt, because its claims used more gas
// than its preflight, go out in the next batch.
func TestV2ClaimAllBatchRetriesItemsNotAttempted(t *testing.T) {
	s := newV2BatchScenario(t)
	s.c.before = func(key string) {
		if key == "send:claimMany" && s.c.calls[key] == 1 {
			s.c.itemGas = 1_200_000
		}
	}
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	sent := s.c.transactions()
	if err != nil || len(sent) != 2 {
		t.Fatalf("retry: %v, %d transactions\n%s", err, len(sent), output)
	}
	requireBatchTx(t, sent[0], 7, v2BatchLeast(3), s.calldata(t, ClaimBatchGasPerClaim, 3, 4, 6))
	requireBatchTx(t, sent[1], 8, v2TestBatchBase+1_200_000+v2TestBatchNeeded, s.calldata(t, ClaimBatchGasPerClaim, 4, 6))
	for _, pool := range v2BatchPools {
		if !strings.Contains(output, v2BatchPaid(pool, v2TestBatcher)) {
			t.Fatalf("output lacks pool %d's claim:\n%s", pool, output)
		}
	}
}

// CL-1, SPEC A6: in a batch, a claim a third party made first is done; one
// the runtime refused fails, alone, and stays claimable; the other pays.
func TestV2ClaimAllBatchReportsEachRefusal(t *testing.T) {
	s := newV2BatchScenario(t)
	s.c.before = func(key string) {
		if key == "send:claimMany" {
			s.c.claimed[[2]uint64{4, 1}] = big.NewInt(4_000_000_000)
			s.c.exhaust[[2]uint64{6, 1}] = true
		}
	}
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	if err == nil || len(s.c.transactions()) != 1 || !strings.HasPrefix(err.Error(), "pool 6 account 1: tx "+s.c.transactions()[0].Hash().Hex()+" refused its claim: no revert data (out of gas, as a runtime refusal leaves a claim)") {
		t.Fatalf("refusals: %v\n%s", err, output)
	}
	if !strings.Contains(output, v2BatchPaid(3, v2TestBatcher)) || !strings.Contains(output, "pool 4 account 1: another sender claimed it first (claimed 4.000000000 α (4000000000 rao)); done\n") || s.c.claimed[[2]uint64{6, 1}] != nil {
		t.Fatalf("outcomes:\n%s", output)
	}
	// A refusal whose re-read fails reports both.
	s = newV2BatchScenario(t)
	s.c.before = func(key string) {
		if key == "send:claimMany" {
			s.c.exhaust[[2]uint64{6, 1}] = true
			s.c.failAt["eth_call:claimed"] = s.c.calls["eth_call:claimed"] + 1
		}
	}
	if _, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher)); err == nil || !strings.Contains(err.Error(), "refused its claim: no revert data") || !strings.Contains(err.Error(), "\nclaimed(6,1): synthetic refusal of eth_call:claimed") {
		t.Fatalf("failed re-read: %v", err)
	}
	// A vault2 error is named.
	s = newV2BatchScenario(t)
	s.c.before = func(key string) {
		if key == "send:claimMany" {
			s.c.roots[0].Root[0] ^= 1
		}
	}
	if _, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher)); err == nil || !strings.Contains(err.Error(), "pool 3 account 1: tx ") || !strings.HasSuffix(err.Error(), " refused its claim: InvalidProof") {
		t.Fatalf("vault2 refusal: %v", err)
	}
}

// CL-1, SPEC A6: each claim's preflight decides its place in the batch: a
// claim a third party made first is done, a refused one fails alone, and a
// last claim goes out alone.
func TestV2ClaimAllBatchPreflightsEachClaim(t *testing.T) {
	s := newV2BatchScenario(t)
	s.c.exhaust[[2]uint64{6, 1}] = true
	s.c.before = func(key string) {
		if key == "eth_estimateGas:claim" && s.c.calls[key] == 2 {
			s.c.claimed[[2]uint64{4, 1}] = big.NewInt(4_000_000_000)
		}
	}
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	if err == nil || !strings.HasPrefix(err.Error(), "pool 6 account 1: preflight: ") || len(s.c.transactions()) != 1 || s.c.calls["eth_call:claimMany"] != 0 {
		t.Fatalf("preflights: %v; observed %s\n%s", err, s.c.observed(), output)
	}
	requireV2Tx(t, s.c.transactions()[0], 7, ClaimBatchGasPerClaim, 0, s.single(t, 3))
	if !strings.Contains(output, "pool 4 account 1: another sender claimed it first (claimed 4.000000000 α (4000000000 rao)); done\n") || !strings.Contains(output, v2BatchPaid(3, testKeyAddr)) {
		t.Fatalf("outcomes:\n%s", output)
	}
	// A refused preflight whose re-read fails reports both.
	s = newV2BatchScenario(t)
	s.c.exhaust[[2]uint64{6, 1}] = true
	s.c.failAt["eth_call:claimed"] = 5
	if _, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher)); err == nil || !strings.Contains(err.Error(), "pool 6 account 1: preflight: ") || !strings.Contains(err.Error(), "\nclaimed(6,1): synthetic refusal of eth_call:claimed") {
		t.Fatalf("failed re-read: %v", err)
	}
	// An estimate refused without revert data fails its claim alone.
	s = newV2BatchScenario(t)
	s.c.failAt["eth_estimateGas:claim"] = 1
	if _, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher)); err == nil || err.Error() != "pool 3 account 1: preflight: synthetic refusal of eth_estimateGas:claim" || len(s.c.transactions()) != 1 {
		t.Fatalf("refused estimate: %v", err)
	}
}

// CL-1: a dry run verifies, preflights and sizes the batch, prints it and
// sends nothing; --gas_limit applies to single claims only.
func TestV2ClaimAllBatchDryRun(t *testing.T) {
	s := newV2BatchScenario(t)
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher, "--dry-run", "--gas_limit=90000"))
	if err != nil || len(s.c.transactions()) != 0 || !strings.Contains(output, "settlement v2 batch claim intent\n") || !strings.HasSuffix(output, "dry run: preflight ok, nothing sent\n") ||
		!strings.Contains(output, "  claims:      3 of 3, each capped at 2600000 gas\n  gas limit:   ") || s.c.calls["eth_call:claimMany"] == 0 {
		t.Fatalf("dry run: %v\n%s", err, output)
	}
}

// CL-1: --all with one account to claim sends it alone even with --batch,
// and with no finalized account says so; --batch must be an address.
func TestV2ClaimAllEdges(t *testing.T) {
	s := newV2BatchScenario(t)
	for _, pool := range []uint64{4, 6} {
		s.c.claimed[[2]uint64{pool, 1}] = new(big.Int).SetUint64(pool * 1_000_000_000)
	}
	if output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher)); err != nil || len(s.c.transactions()) != 1 || s.c.calls["eth_call:claimMany"] != 0 {
		t.Fatalf("one account: %v\n%s", err, output)
	}
	requireV2Tx(t, s.c.transactions()[0], 7, ClaimBatchGasPerClaim, 0, s.single(t, 3))

	s = newV2BatchScenario(t)
	s.c.setAccounts(v2TestEntry{Vault: strings.ToLower(v2TestVault), NoID: 9, Index: 0, Cumulative: "0", Claimed: "0", PendingCumulative: v2Ptr("7")})
	if output, err := s.c.run(s.c.claimArgv("--all")); err != nil || output != "coldkey "+aliceSS58+" (0x"+aliceHex+") has no finalized account; nothing to claim\n" || s.c.observed() != "eth_chainId=1" {
		t.Fatalf("no finalized account: %v; observed %s\n%s", err, s.c.observed(), output)
	}

	s = newV2BatchScenario(t)
	if _, err := s.c.run(s.c.claimArgv("--all", "--batch=0x12")); err == nil || err.Error() != `--batch: "0x12" is not a 20-byte hex address` || s.c.observed() != "" {
		t.Fatalf("--batch=0x12: %v; observed %s", err, s.c.observed())
	}
	// A proof that does not verify refuses every claim before any send.
	s = newV2BatchScenario(t)
	s.c.pools[6][0].Root[0] ^= 1
	if _, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher)); err == nil || !strings.HasPrefix(err.Error(), "the ledger endpoint's proof for pool 6 account 1") || s.c.calls["eth_estimateGas:claim"] != 0 {
		t.Fatalf("unverified proof: %v", err)
	}
}

// CL-1: a batch is refused, and nothing more sent, when its sizing or its
// receipt fails: the block gas limit, the batcher's answer, a block too small
// for one claim, a reverted batch, a batch without events or without vault2's
// Claimed for a paid item, and one that attempted nothing.
func TestV2ClaimAllBatchRefusals(t *testing.T) {
	small := v2BatchBlock(t, v2BatchLeast(1)-1)
	for _, c := range []struct {
		name   string
		mutate func(*v2BatchScenario)
		want   string
		sent   int
	}{
		{"block gas limit", func(s *v2BatchScenario) { s.c.failAt["eth_getBlockByNumber:latest"] = 1 }, "block gas limit: synthetic block refusal", 0},
		{"no block gas limit", func(s *v2BatchScenario) { s.c.gasLimit = 0 }, "block gas limit: the latest block names none", 0},
		{"batch preflight", func(s *v2BatchScenario) { s.c.failAt["eth_call:claimMany"] = 1 }, fmt.Sprintf("batch preflight at %d gas: synthetic refusal of eth_call:claimMany", claimBatchCeiling(75_000_000)), 0},
		{"not a batcher", func(s *v2BatchScenario) { s.c.garbage["claimMany"] = true }, fmt.Sprintf("batch preflight at %d gas: %s is not a claim batcher: claimMany answered 0x", claimBatchCeiling(75_000_000), common.HexToAddress(v2TestBatcher).Hex()), 0},
		{"overcount", func(s *v2BatchScenario) { s.c.batchAnswer = big.NewInt(4) }, fmt.Sprintf("a batch of 3 claims attempts 4 at %d gas", claimBatchCeiling(75_000_000)), 0},
		{"too small a block", func(s *v2BatchScenario) { s.c.gasLimit = small }, fmt.Sprintf("a batch of 3 claims attempts 0 at %d gas", v2BatchLeast(1)-1), 0},
		{"batcher code", func(s *v2BatchScenario) { s.c.failAt["eth_getCode"] = 1 }, "--batch: claim batcher " + common.HexToAddress(v2TestBatcher).Hex() + " code: synthetic code refusal", 0},
		{"no batcher code", func(s *v2BatchScenario) { s.c.batcherCode = []byte{} }, "--batch: claim batcher " + common.HexToAddress(v2TestBatcher).Hex() + " has no code", 0},
		{"another contract's code", func(s *v2BatchScenario) { s.c.batcherCode = []byte{0x60, 0x00} }, "--batch: " + common.HexToAddress(v2TestBatcher).Hex() + " is not the release claim batcher: its code hash is " +
			crypto.Keccak256Hash([]byte{0x60, 0x00}).Hex() + ", the release runtime's 0x2964c2882e0e4f85c2295563da92ead2fa8669d0045059eefbefa187ffd22c26", 0},
		{"reverted", func(s *v2BatchScenario) { s.c.revertBatch = true }, " reverted on-chain (status 0, block 200000, gas used 0)", 1},
		{"no events", func(s *v2BatchScenario) { s.c.mute = true }, " reports 0 of its 3 items", 1},
		{"no Claimed", func(s *v2BatchScenario) { s.c.muteVault = true }, " but " + common.HexToAddress(v2TestVault).Hex() + " emitted no Claimed for it", 1},
		{"nothing attempted", func(s *v2BatchScenario) {
			s.c.before = func(key string) {
				if key == "send:claimMany" {
					s.c.batchBase = 75_000_000
				}
			}
		}, " attempted none of its 3 claims", 1},
	} {
		s := newV2BatchScenario(t)
		c.mutate(s)
		output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
		if err == nil || !strings.Contains(err.Error(), c.want) || len(s.c.transactions()) != c.sent {
			t.Fatalf("%s: %v, want %q; sent %d\n%s", c.name, err, c.want, len(s.c.transactions()), output)
		}
	}
}

// CL-1, CB-EV: the batcher's events of a receipt, as ClaimBatchOutcomes reads
// them.
func TestV2ClaimBatchOutcomes(t *testing.T) {
	c := newV2TestChain(t)
	c.batcher = common.HexToAddress(v2TestBatcher)
	paid := func(item, amount int64) *types.Log {
		return c.batcherEvent("ClaimPaid", big.NewInt(item), big.NewInt(amount))
	}
	refused := func(item int64, selector [4]byte) *types.Log {
		return c.batcherEvent("ClaimRefused", big.NewInt(item), selector)
	}
	rest := func(item int64) *types.Log { return c.batcherEvent("ClaimsNotAttempted", big.NewInt(item)) }
	foreign := paid(1, 1)
	foreign.Address = common.Address{0x99}
	vaultLog := c.event("Claimed", []common.Hash{{}, {}, {}}, big.NewInt(0), big.NewInt(1), big.NewInt(1), c.batcher)
	outcomes, err := ClaimBatchOutcomes(&types.Receipt{Logs: []*types.Log{vaultLog, paid(0, 5), foreign, refused(1, [4]byte{0xde, 0xad, 0xbe, 0xef}), nil, rest(2)}}, c.batcher, 4)
	want := []ClaimBatchOutcome{{Attempted: true, Amount: big.NewInt(5)}, {Attempted: true, Selector: [4]byte{0xde, 0xad, 0xbe, 0xef}}, {}, {}}
	if err != nil || fmt.Sprint(outcomes) != fmt.Sprint(want) {
		t.Fatalf("outcomes %v (%v), want %v", outcomes, err, want)
	}
	if outcomes, err := ClaimBatchOutcomes(&types.Receipt{Logs: []*types.Log{paid(0, 5), refused(1, [4]byte{})}}, c.batcher, 2); err != nil || len(outcomes) != 2 {
		t.Fatalf("every item attempted: %v (%v)", outcomes, err)
	}
	unknown := paid(0, 5)
	unknown.Topics[0] = common.Hash{0x01}
	empty := paid(0, 5)
	empty.Data = nil
	for _, c := range []struct {
		name string
		logs []*types.Log
		want string
	}{
		{"out of order", []*types.Log{paid(1, 5)}, "has a batcher event out of order for its 2 items"},
		{"repeated", []*types.Log{paid(0, 5), paid(0, 5)}, "has a batcher event out of order"},
		{"after the rest", []*types.Log{rest(0), paid(0, 5)}, "has a batcher event out of order"},
		{"rest out of order", []*types.Log{paid(0, 5), rest(0)}, "has a batcher event out of order"},
		{"past the items", []*types.Log{paid(0, 5), paid(1, 5), paid(2, 5)}, "has a batcher event out of order"},
		{"unknown event", []*types.Log{unknown}, "has a batcher event out of order"},
		{"no data", []*types.Log{empty}, "has a batcher event out of order"},
		{"missing", []*types.Log{paid(0, 5)}, "reports 1 of its 2 items"},
	} {
		if _, err := ClaimBatchOutcomes(&types.Receipt{Logs: c.logs, TxHash: common.Hash{0x7}}, common.HexToAddress(v2TestBatcher), 2); err == nil || !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "batch receipt "+common.Hash{0x7}.Hex()) {
			t.Fatalf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// CL-1: SizeClaimBatch stops with its context, whose error it reports.
func TestV2SizeClaimBatchStopsWithItsContext(t *testing.T) {
	s := newV2BatchScenario(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.c.before = func(key string) {
		if key == "eth_call:claimMany" && s.c.calls[key] == 3 {
			cancel()
		}
	}
	client, _, err := dialOne(ctx, s.c.rpc.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	items := []stabi.STClaimBatcherClaim{s.item(t, 3), s.item(t, 4)}
	if _, _, _, err := SizeClaimBatch(ctx, client, common.HexToAddress(testKeyAddr), s.c.batcher, s.c.vault, items, ClaimBatchGasPerClaim); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled sizing: %v", err)
	}
}

// CL-1: a batched claim's refusal is named by vault2's error, by its bytes
// when it is none of vault2's, and as a runtime refusal when it has none.
func TestV2RefusalName(t *testing.T) {
	for selector, want := range map[[4]byte]string{
		[4]byte(v2TestError("NothingToClaim")): "NothingToClaim",
		{0xde, 0xad, 0xbe, 0xef}:               "0xdeadbeef",
		{}:                                     "no revert data (out of gas, as a runtime refusal leaves a claim)",
	} {
		if got := v2RefusalName(selector); got != want {
			t.Fatalf("v2RefusalName(%x) = %q, want %q", selector, got, want)
		}
	}
}

// CL-1: the last claim, sent alone after a batch, fails alone.
func TestV2ClaimAllBatchLastClaimFails(t *testing.T) {
	s := newV2BatchScenario(t)
	s.c.gasLimit = v2BatchBlock(t, v2BatchLeast(3)-1)
	s.c.before = func(key string) {
		if key == "send:claimMany" {
			s.c.exhaust[[2]uint64{6, 1}] = true
		}
	}
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	if err == nil || err.Error() != "preflight eth_call failed: execution reverted" || len(s.c.transactions()) != 1 ||
		!strings.Contains(output, v2BatchPaid(3, v2TestBatcher)) || !strings.Contains(output, v2BatchPaid(4, v2TestBatcher)) {
		t.Fatalf("last claim: %v\n%s", err, output)
	}
}

// CL-1: --all parses with and without --batch, and the usage refuses --batch
// without --all, and --all with a selected account.
func TestV2ClaimAllUsage(t *testing.T) {
	c := newV2TestChain(t)
	for _, flags := range [][]string{{"--all"}, {"--all", "--batch=" + v2TestBatcher, "--gas_limit=90000", "--dry-run"}} {
		opts, err := parseArgv(t, c.claimArgv(flags...))
		if err != nil || !boolOpt(opts, "--all") || len(flags) > 1 && strOpt(opts, "--batch") != v2TestBatcher {
			t.Fatalf("%q: %v %v", flags, opts, err)
		}
	}
	for _, flags := range [][]string{{"--batch=" + v2TestBatcher}, {"--all", "--no_id=3"}, {"--all", "--index=1"}} {
		if _, err := parseArgv(t, c.claimArgv(flags...)); err == nil {
			t.Fatalf("usage accepted %q", flags)
		}
	}
}

// CL-1: the most gas the tools name for a batch is 13/15 of the block gas
// limit less 1,500 gas, as the pool takes (R-7), and at most ClaimBatchMaxGas,
// less claimBatchGasMargin, without overflow or underflow.
func TestV2ClaimBatchCeiling(t *testing.T) {
	for limit, want := range map[uint64]uint64{
		75_000_000:     64_988_500,
		30_000_000:     25_988_500,
		90_000_000:     64_988_500,
		13_846:         499,
		13_000:         0,
		0:              0,
		math.MaxUint64: 64_988_500,
	} {
		if got := claimBatchCeiling(limit); got != want {
			t.Fatalf("ceiling at a block gas limit of %d: %d, want %d", limit, got, want)
		}
	}
	if claimBatchCeiling(75_000_000)+claimBatchGasMargin != ClaimBatchMaxGas {
		t.Fatal("at runtime 475's block gas limit the pool takes ClaimBatchMaxGas")
	}
}

// CL-1 (review of 77f86ead): when the block gas limit binds, a batch names no
// more gas than the pool of that limit takes, 13/15 of it less 1,500 gas,
// rather than the block gas limit itself, which the pool refuses.
func TestV2ClaimAllBatchNamesNoMoreGasThanThePoolTakes(t *testing.T) {
	s := newV2BatchScenario(t)
	s.c.gasLimit = v2BatchLeast(3) - 1
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	sent := s.c.transactions()
	if err != nil || len(sent) == 0 {
		t.Fatalf("claim --all --batch: %v\n%s", err, output)
	}
	takes := s.c.gasLimit*13/15 - 1_500
	if gas := sent[0].Gas(); gas > takes {
		t.Fatalf("the batch names %d gas; a pool of that block gas limit takes at most %d", gas, takes)
	}
}

// CL-1: SizeClaimBatch sizes at most ClaimBatchMaxClaims of its items, which
// the gas would allow.
func TestV2SizeClaimBatchHoldsAtMostTheCap(t *testing.T) {
	s := newV2BatchScenario(t)
	client, _, err := dialOne(t.Context(), s.c.rpc.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	items := make([]stabi.STClaimBatcherClaim, ClaimBatchMaxClaims+8)
	for i := range items {
		items[i] = s.item(t, 3)
	}
	count, calldata, gasLimit, err := SizeClaimBatch(t.Context(), client, common.HexToAddress(testKeyAddr), s.c.batcher, s.c.vault, items, ClaimBatchGasPerClaim)
	// The model's eth_call pays each copy, as it commits nothing.
	least := v2BatchLeast(ClaimBatchMaxClaims)
	if err != nil || count != ClaimBatchMaxClaims || !bytes.Equal(calldata, claimBatcher.PackClaimMany(s.c.vault, items[:ClaimBatchMaxClaims], new(big.Int).SetUint64(ClaimBatchGasPerClaim))) ||
		gasLimit < least+least/5 || gasLimit > least+claimBatchTolerance+(least+claimBatchTolerance)/5 {
		t.Fatalf("a batch of %d items: %d items, gas %d (least %d), %v", len(items), count, gasLimit, least, err)
	}
}

// newV2ManyPoolScenario is alice as account 1 of a finalized root in each of
// the 34 pools 100 to 133, each with anything claimable.
func newV2ManyPoolScenario(t *testing.T) (*v2BatchScenario, []uint64) {
	t.Helper()
	s := newV2BatchScenario(t)
	var entries []v2TestEntry
	var pools []uint64
	for pool := uint64(100); pool < 134; pool++ {
		tree := v2TestTree(t, []merkle.LedgerAccount{{Coldkey: v2TestKey(1), Cumulative: *uint256.NewInt(1_000_000_000)}, {Coldkey: s.alice, Cumulative: *uint256.NewInt(pool * 1_000_000_000)}})
		s.trees[pool] = tree
		s.c.pools[pool] = []stabi.ISTSettlementVault2Root{v2TestRoot(tree, 10, [32]byte{byte(pool)})}
		entries = append(entries, v2TestEntry{Vault: strings.ToLower(v2TestVault), NoID: pool, Index: 1, RootIndex: v2Ptr(uint64(0)), Cumulative: fmt.Sprint(pool * 1_000_000_000), Proof: v2TestProofJSON(t, tree, 1), Claimed: "0"})
		pools = append(pools, pool)
	}
	s.c.setAccounts(entries...)
	return s, pools
}

// CL-1: --all --batch sends at most ClaimBatchMaxClaims claims a
// transaction: 34 accounts go out as a batch of 32, then one of 2.
func TestV2ClaimAllBatchSendsAtMostTheCapATransaction(t *testing.T) {
	s, pools := newV2ManyPoolScenario(t)
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	sent := s.c.transactions()
	if err != nil || len(sent) != 2 || !bytes.Equal(sent[0].Data(), s.calldata(t, ClaimBatchGasPerClaim, pools[:ClaimBatchMaxClaims]...)) ||
		!bytes.Equal(sent[1].Data(), s.calldata(t, ClaimBatchGasPerClaim, pools[ClaimBatchMaxClaims:]...)) ||
		!strings.Contains(output, "  claims:      32 of 32, each capped at 2600000 gas\n") || !strings.Contains(output, "  claims:      2 of 2, each capped at 2600000 gas\n") {
		t.Fatalf("34 accounts: %v, %d transactions\n%s", err, len(sent), output)
	}
	for _, pool := range pools {
		if !strings.Contains(output, v2BatchPaid(pool, v2TestBatcher)) {
			t.Fatalf("pool %d was not paid:\n%s", pool, output)
		}
	}
}

// CL-1, SPEC A6: a round whose preflights leave fewer than two claims sends no
// batch; the claim that passed waits for the next round with the claims
// after the round. 31 of the first 32 accounts refused, the one left and the
// last two go out in one batch, and each refusal fails its account alone.
func TestV2ClaimAllBatchRoundWithoutABatchMovesOn(t *testing.T) {
	s, pools := newV2ManyPoolScenario(t)
	for _, pool := range pools[1:ClaimBatchMaxClaims] {
		s.c.exhaust[[2]uint64{pool, 1}] = true
	}
	output, err := s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	sent := s.c.transactions()
	if err == nil || strings.Count(err.Error(), ": preflight: ") != ClaimBatchMaxClaims-1 || len(sent) != 1 ||
		!bytes.Equal(sent[0].Data(), s.calldata(t, ClaimBatchGasPerClaim, pools[0], pools[32], pools[33])) {
		t.Fatalf("refused round: %v, %d transactions\n%s", err, len(sent), output)
	}
	for _, pool := range []uint64{pools[0], pools[32], pools[33]} {
		if !strings.Contains(output, v2BatchPaid(pool, v2TestBatcher)) {
			t.Fatalf("pool %d was not paid:\n%s", pool, output)
		}
	}
}
