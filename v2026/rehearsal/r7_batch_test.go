//go:build runtime_rehearsal

package rehearsal

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/miner/onchain"
)

// batchAccounts is the number of accounts R-7's root holds: enough for the
// single claims, the fixed batches, of up to the claim tools' 32, more than
// the most one transaction holds, which was 418 and grows as claims get
// cheaper, and a mixed batch. The last account holds less than the runtime's
// transfer floor.
const batchAccounts = 700

// claimItem is STClaimBatcher.Claim.
type claimItem struct {
	NoId      *big.Int
	RootIndex *big.Int
	Account   account
	Proof     []proofNode
}

// R-7. TestR7ClaimBatch measures the claim batcher (VERSION2-FINAL.md §10.2,
// §15.8) against single claims, on a ledger of 2^29 accounts: the block gas
// limit and the most gas a transaction may name, 13/15 of it less 1,500 gas;
// for single claims the gas used, the gas limit eth_estimateGas gives and the
// least gas limit that pays; for batches of 1, 5, 10, 20 and the claim tools'
// 32 claims and for the most one block holds, the gas used and the least gas
// limit at which the batch attempts every item. Each batch caps each claim at the
// tooling's default gas per claim, is sent with exactly its least limit, and
// pays every item. A last batch mixes two refusals, a broken proof and a
// claim below the transfer floor, with claims that pay.
func TestR7ClaimBatch(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	s := deployStack(t, n, stackConfig{operators: 1})
	operator := s.operators[1]
	// Epochs stop (tempo 0), so no dividend reaches a position between reads.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(s.netuid), types.NewU16(0)))
	relayer := newEVMKey(t)
	n.fundEVM(t, relayer.address, 10_000*tao)
	batcherArtifact := forgeArtifact(t, "src/STClaimBatcher.sol", "STClaimBatcher")
	batcher := n.deploy(t, relayer, batcherArtifact.creation)

	// Epoch 1 funds the accounts of root 0.
	n.stakeFor(t, alice, s.vaultColdkey, operator.pool, s.netuid, 1_000*tao)
	s.close(t, 1, 1)
	funded := s.vaultUint(t, "fundedThrough", id(1), id(1)).Uint64()
	alpha := forgeArtifact(t, "src/interfaces/alpha.sol", "IAlpha")
	priceQ9 := new(big.Int).Quo(n.read(t, alphaAddress, alpha, "getAlphaPrice", s.netuid)[0].(*big.Int), big.NewInt(tao)).Uint64()
	dust := minimumTransferTaoRao * tao / priceQ9 / 2
	share := (funded - dust) / (batchAccounts - 1)
	accounts := make([]merkle.LedgerAccount, batchAccounts)
	for i := range accounts {
		amount := share
		switch i {
		case 0:
			amount = funded - dust - share*(batchAccounts-2)
		case batchAccounts - 1:
			amount = dust
		}
		accounts[i] = ledgerAccount(newAccount(t), amount)
	}
	l := newLedger(t, accounts, deepLeafCount)
	committed := l.commitment("ledger header epoch 1")
	n.send(t, operator.rootSigner, s.settlement, s.settlementArtifact, nil, "commitDistribution", id(1), id(1), id(0), committed)
	n.send(t, s.auditor, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(1), committed.Root, committed.LedgerHash)
	n.sealUntil(t, s.pending(t, 1).CommittedBlock+challengeBlocks)
	n.send(t, relayer, s.settlement, s.settlementArtifact, nil, "finalizeDistribution", id(1))

	header, err := n.eth.HeaderByNumber(n.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A transaction may name less gas than the block gas limit: the pool
	// refuses more than the normal dispatch class's extrinsic weight limit.
	// Found to the gas with value transfers, it must be at least the
	// tooling's onchain.ClaimBatchMaxGas, which sizes batches.
	accepted := func(gas uint64) bool {
		err := n.sendTx(t, relayer, &relayer.address, nil, gas)
		if err != nil && !strings.Contains(err.Error(), "exceeds block gas limit") {
			t.Fatalf("a transfer naming %d gas: %v", gas, err)
		}
		return err == nil
	}
	low, high := onchain.ClaimBatchMaxGas, header.GasLimit+1
	if !accepted(low) || accepted(high) {
		t.Fatalf("transfers naming %d and %d gas", low, high)
	}
	for high-low > 1 {
		if middle := (low + high) / 2; accepted(middle) {
			low = middle
		} else {
			high = middle
		}
	}
	// The pool takes the normal class's extrinsic weight limit, 13/15 of the
	// block's, less the extension weight, as the claim tools assume.
	if low != header.GasLimit/15*13-1_500 {
		t.Fatalf("the pool takes %d gas of a %d block gas limit, not 13/15 of it less 1,500", low, header.GasLimit)
	}
	maxGas := onchain.ClaimBatchMaxGas
	t.Logf("block gas limit %d; most gas one transaction may name %d; batches sized at %d", header.GasLimit, low, maxGas)

	// least returns the least gas limit, to the gas, at which a transaction
	// of data from the relayer to `to` satisfies ok in an eth_call.
	least := func(to common.Address, data []byte, high uint64, ok func([]byte, error) bool) uint64 {
		return n.least(t, ethereum.CallMsg{From: relayer.address, To: &to, Data: data}, high, ok)
	}
	attempted := func(out []byte, err error) uint64 {
		if err != nil {
			return 0
		}
		return batcherArtifact.unpack(t, "claimMany", out)[0].(*big.Int).Uint64()
	}
	// paid requires account index's claimed watermark at its cumulative.
	paid := func(t *testing.T, index uint64) {
		if got := s.vaultUint(t, "claimed", id(1), id(index)); got.Cmp(l.account(index).Cumulative) != 0 {
			t.Fatalf("account %d claimed %s of %s", index, got, l.account(index).Cumulative)
		}
	}
	next := uint64(0)

	// Single claims: eth_estimateGas's limit, the least limit that pays and
	// the gas used. The pool's first claim also writes its first claimed
	// and paid totals.
	var singleUsed []uint64
	var singleEstimate, singleLeast uint64
	for range 5 {
		a := l.account(next)
		data := s.vaultArtifact.pack(t, "claim", id(1), id(0), a, l.proof(next))
		estimate, err := n.estimate(relayer.address, s.vault, nil, data)
		if err != nil {
			t.Fatalf("estimate a claim: %s", reason(err))
		}
		exact := least(s.vault, data, estimate, func(_ []byte, err error) bool { return err == nil })
		receipt := n.transact(t, relayer, &s.vault, nil, data, exact)
		if receipt.Status != gethtypes.ReceiptStatusSuccessful {
			t.Fatalf("a claim sent with its least limit %d failed", exact)
		}
		paid(t, next)
		singleUsed, singleEstimate, singleLeast = append(singleUsed, receipt.GasUsed), max(singleEstimate, estimate), max(singleLeast, exact)
		next++
	}
	t.Logf("single claims: used %v, eth_estimateGas limit up to %d, least limit up to %d", singleUsed, singleEstimate, singleLeast)
	// The least limit covers the transaction's intrinsic gas besides the
	// claim's own, so a cap at the default leaves the claim at least as much.
	if singleLeast > onchain.ClaimBatchGasPerClaim {
		t.Fatalf("a single claim's least limit %d exceeds the default gas per claim %d", singleLeast, onchain.ClaimBatchGasPerClaim)
	}

	// batch sends the next count accounts with the least limit that attempts
	// all of them and requires each to pay.
	batch := func(t *testing.T, count uint64, high uint64) (used, limit uint64) {
		items := make([]claimItem, count)
		for i := range items {
			index := next + uint64(i)
			items[i] = claimItem{NoId: id(1), RootIndex: id(0), Account: l.account(index), Proof: l.proof(index)}
		}
		data := batcherArtifact.pack(t, "claimMany", s.vault, items, id(onchain.ClaimBatchGasPerClaim))
		limit = least(batcher, data, high, func(out []byte, err error) bool { return attempted(out, err) == count })
		receipt := n.transact(t, relayer, &batcher, nil, data, limit)
		if receipt.Status != gethtypes.ReceiptStatusSuccessful {
			t.Fatalf("a batch of %d sent with its least limit %d failed", count, limit)
		}
		paidEvent := batcherArtifact.abi.Events["ClaimPaid"]
		events := 0
		for _, log := range receipt.Logs {
			if log.Address != batcher {
				continue
			}
			if log.Topics[0] != paidEvent.ID {
				t.Fatalf("batch of %d: event %s", count, log.Topics[0])
			}
			values, err := paidEvent.Inputs.Unpack(log.Data)
			if err != nil || values[0].(*big.Int).Uint64() != uint64(events) || values[1].(*big.Int).Cmp(items[events].Account.Cumulative) != 0 {
				t.Fatalf("batch of %d: ClaimPaid %v (%v), want item %d", count, values, err, events)
			}
			events++
		}
		if events != int(count) {
			t.Fatalf("batch of %d: %d ClaimPaid events", count, events)
		}
		for i := range count {
			paid(t, next+i)
		}
		next += count
		s.requireConservation(t)
		return receipt.GasUsed, limit
	}
	for _, count := range []uint64{1, 5, 10, 20, onchain.ClaimBatchMaxClaims} {
		used, limit := batch(t, count, maxGas)
		t.Logf("batch of %d: used %d (%d per claim), least limit %d", count, used, used/count, limit)
	}

	// The most one transaction holds: the longest batch of the remaining
	// accounts that an eth_call at the most gas a transaction may name
	// attempts in full. Offering more accounts would also spend their
	// calldata's intrinsic gas, so the size is searched, not read off one call.
	fits := func(count uint64) bool {
		items := make([]claimItem, count)
		for i := range items {
			index := next + uint64(i)
			items[i] = claimItem{NoId: id(1), RootIndex: id(0), Account: l.account(index), Proof: l.proof(index)}
		}
		data := batcherArtifact.pack(t, "claimMany", s.vault, items, id(onchain.ClaimBatchGasPerClaim))
		return attempted(n.eth.CallContract(n.ctx, ethereum.CallMsg{From: relayer.address, To: &batcher, Data: data, Gas: maxGas}, nil)) == count
	}
	most, over := uint64(1), batchAccounts-next
	if !fits(most) || fits(over) {
		t.Fatalf("one transaction holds all of the %d accounts left, or none", over)
	}
	for over-most > 1 {
		if middle := (most + over) / 2; fits(middle) {
			most = middle
		} else {
			over = middle
		}
	}
	used, limit := batch(t, most, maxGas)
	t.Logf("batch of %d, the most one transaction holds: used %d (%d per claim), least limit %d of %d", most, used, used/most, limit, maxGas)

	// A batch whose second claim has a broken proof and whose third is below
	// the runtime's transfer floor: each is refused alone, vault2 reverting
	// with InvalidProof and the runtime's refusal, which returns nothing,
	// reported as selector zero; the claims around them pay. vault2's
	// reentrancy flag is persistent, so the revert leaves it clear: runtime
	// 475 would keep a transient one (transient_test.go).
	broken := l.proof(next + 1)
	broken[0].Hash[0] ^= 1
	items := []claimItem{
		{NoId: id(1), RootIndex: id(0), Account: l.account(next), Proof: l.proof(next)},
		{NoId: id(1), RootIndex: id(0), Account: l.account(next + 1), Proof: broken},
		{NoId: id(1), RootIndex: id(0), Account: l.account(batchAccounts - 1), Proof: l.proof(batchAccounts - 1)},
		{NoId: id(1), RootIndex: id(0), Account: l.account(next + 2), Proof: l.proof(next + 2)},
	}
	data := batcherArtifact.pack(t, "claimMany", s.vault, items, id(onchain.ClaimBatchGasPerClaim))
	limit = least(batcher, data, maxGas, func(out []byte, err error) bool { return attempted(out, err) == uint64(len(items)) })
	receipt := n.transact(t, relayer, &batcher, nil, data, limit)
	if receipt.Status != gethtypes.ReceiptStatusSuccessful {
		t.Fatalf("the mixed batch sent with its least limit %d failed", limit)
	}
	var invalidProof [4]byte
	invalidProofId := s.vaultArtifact.abi.Errors["InvalidProof"].ID
	copy(invalidProof[:], invalidProofId[:4])
	want := []struct {
		event  string
		fields map[string]any
	}{
		{"ClaimPaid", map[string]any{"item": id(0), "amount": items[0].Account.Cumulative}},
		{"ClaimRefused", map[string]any{"item": id(1), "selector": invalidProof}},
		{"ClaimRefused", map[string]any{"item": id(2), "selector": [4]byte{}}},
		{"ClaimPaid", map[string]any{"item": id(3), "amount": items[3].Account.Cumulative}},
	}
	var got []map[string]any
	for _, log := range receipt.Logs {
		if log.Address != batcher {
			continue
		}
		event, err := batcherArtifact.abi.EventByID(log.Topics[0])
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]any{"event": event.Name}
		if err := event.Inputs.UnpackIntoMap(fields, log.Data); err != nil {
			t.Fatal(err)
		}
		got = append(got, fields)
	}
	if len(got) != len(want) {
		t.Fatalf("the mixed batch's events: %v", got)
	}
	for i, w := range want {
		w.fields["event"] = w.event
		if fmt.Sprint(got[i]) != fmt.Sprint(w.fields) {
			t.Fatalf("the mixed batch's event %d: %v, want %v", i, got[i], w.fields)
		}
	}
	paid(t, next)
	paid(t, next+2)
	for _, index := range []uint64{next + 1, batchAccounts - 1} {
		if claimed := s.vaultUint(t, "claimed", id(1), id(index)); claimed.Sign() != 0 {
			t.Fatalf("refused account %d claimed %s", index, claimed)
		}
	}
	s.requireConservation(t)
	t.Logf("batch of 4, the second with a broken proof and the third a %d rao claim below the transfer floor: used %d, least limit %d", dust, receipt.GasUsed, limit)
}

// R-7: the most gas a transaction may name does not depend on its calldata,
// so a batch the claim tools sized and signed stays admissible however many
// claims, at about 2,100 bytes each at depth 29, it carries.
func TestR7MaxGasDoesNotDependOnCalldata(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	sender, target := newEVMKey(t), newEVMKey(t)
	n.fundEVM(t, sender.address, 1_000*tao)
	for _, size := range []int{0, 100_000, 900_000} {
		data := make([]byte, size)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		if err := n.sendTx(t, sender, &target.address, data, onchain.ClaimBatchMaxGas+1); err == nil || !strings.Contains(err.Error(), "exceeds block gas limit") {
			t.Fatalf("%d bytes naming %d gas: %v", size, onchain.ClaimBatchMaxGas+1, err)
		}
		if err := n.sendTx(t, sender, &target.address, data, onchain.ClaimBatchMaxGas); err != nil {
			t.Fatalf("%d bytes naming %d gas: %v", size, onchain.ClaimBatchMaxGas, err)
		}
		t.Logf("%d bytes of calldata: the pool takes %d gas and refuses %d", size, onchain.ClaimBatchMaxGas, onchain.ClaimBatchMaxGas+1)
	}
}
