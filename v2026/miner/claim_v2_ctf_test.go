//go:build linux || darwin

// CAPTURETHEFLAG.md §10, VERSION2-FINAL.md §10.1: a capture the flag account
// is an account of vault2 pool 2^32 + n, operator n's CTF pool, so the v2
// claim daemon claims it unchanged, as its own (vault, noId, index) unit
// beside the coldkey's account in the operator's pool.
package miner

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/amendment"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

// claimV2TestCtfPool is the fixture operator 7's CTF pool, 2^32 + 7.
var claimV2TestCtfPool = func() uint64 {
	pool, ok := amendment.CtfPoolID(7)
	if !ok {
		panic("operator 7 has no CTF pool id")
	}
	return pool
}()

// newClaimV2CtfTest is the coldkey's account 1 of pool 7 at 5 α and account 1
// of the CTF pool at ctfAlpha, the same index on purpose, each in its pool's
// first finalized root beside another coldkey's account 0.
func newClaimV2CtfTest(t *testing.T, ctfAlpha float64) *claimV2TestChain {
	t.Helper()
	chain := newClaimV2TestChain(t)
	chain.finalize(claimV2TestAccount(claimV2TestOther, 1), claimV2TestAccount(claimV2TestColdkey, 5))
	chain.finalizePool(claimV2TestCtfPool, claimV2TestAccount(claimV2TestOther, 3), claimV2TestAccount(claimV2TestColdkey, ctfAlpha))
	return chain
}

// The claim a sent transaction makes of vault2, which it must address.
func (self *claimV2TestDaemon) sentClaim(raw string) *claimIntentV2 {
	self.t.Helper()
	tx := claimV2TestSent(self.t, raw)
	intent, err := decodeClaimV2Calldata(tx.Data())
	if err != nil || tx.To() == nil || *tx.To() != common.HexToAddress(self.cfg.Vault) {
		self.t.Fatalf("not a claim of the vault: to %v, %v", tx.To(), err)
	}
	return intent
}

// Whether a claim's proof verifies against its root index of pool noID.
func (self *claimV2TestChain) proves(intent *claimIntentV2, noID uint64) bool {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	trees := self.pool(noID).trees
	if !intent.RootIndex.IsUint64() || intent.RootIndex.Uint64() >= uint64(len(trees)) {
		return false
	}
	tree := trees[intent.RootIndex.Uint64()]
	root, total := tree.Root()
	account := intent.account(claimV2TestVault)
	return merkle.VerifyLedgerProof(root, &total, tree.LeafCount(), account.Index, account.Coldkey, &account.Cumulative, account.Proof)
}

// requireIdleIn requires the unit of account 1 of pool noID idle and paid up
// to alpha at its pool's root rootIndex, with that watermark on chain.
func (self *claimV2TestDaemon) requireIdleIn(noID uint64, rootIndex uint64, alpha float64) {
	self.t.Helper()
	unit, claimed := self.durableIn(noID, 1), self.chain.claimedIn(noID, 1)
	want := claimV2TestDec(alpha)
	if unit.Vault != self.cfg.Vault || unit.NoID != noID || unit.Index != 1 || unit.Status != "idle" || unit.RootIndex != rootIndex || unit.Cumulative != want || unit.Claimed != want ||
		claimed.Dec() != want || unit.Demand || unit.Attempts != 0 || unit.TxHash != "" || unit.RawTxHex != "" || unit.LastError != "" {
		self.t.Fatalf("unit of pool %d: %+v, claimed %s, want idle at %s of root %d", noID, unit, claimed.Dec(), want, rootIndex)
	}
}

// The coldkey's accounts at index 1 of pool 7 and of its CTF pool are two
// units, keyed by pool. Each is verified against its own pool's finalized
// root and watermark and claimed by its own transaction, whose proof opens
// its own pool's root and not the other's; the pool account's claim is byte
// for byte the one the endpoint's account gives, as without a CTF account.
// A newer CTF root reopens the CTF unit alone.
func TestClaimV2CtfAccountIsItsOwnUnit(t *testing.T) {
	chain := newClaimV2CtfTest(t, 2)
	daemon := newClaimV2TestDaemon(t, chain, ClaimV2DefaultThresholdRao)
	pool, ctf := daemon.account(1), daemon.accountIn(claimV2TestCtfPool, 1)
	daemon.step(false)
	daemon.step(false)
	sends := chain.sent()
	if len(sends) != 2 || claimV2TestNonce(t, sends[1]) != claimV2TestNonce(t, sends[0])+1 {
		t.Fatalf("two accounts: %d sends", len(sends))
	}
	claims := map[uint64][]byte{}
	for _, raw := range sends {
		intent := daemon.sentClaim(raw)
		noID := intent.NoID.Uint64()
		other := map[uint64]uint64{7: claimV2TestCtfPool, claimV2TestCtfPool: 7}[noID]
		if other == 0 || claims[noID] != nil || intent.RootIndex.Uint64() != 0 || intent.Account.Index.Uint64() != 1 || !chain.proves(intent, noID) || chain.proves(intent, other) {
			t.Fatalf("claim of pool %d, root %s, account %s: proof against its pool %t, the other %t", noID, intent.RootIndex, intent.Account.Index, chain.proves(intent, noID), chain.proves(intent, other))
		}
		claims[noID] = claimV2TestSent(t, raw).Data()
	}
	if !bytes.Equal(claims[7], claimV2Calldata(pool)) || !bytes.Equal(claims[claimV2TestCtfPool], claimV2Calldata(ctf)) {
		t.Fatal("a claim is not its account's own")
	}
	if len(daemon.queue.Units) != 2 || daemon.queue.Units[claimUnitV2Key(daemon.cfg.Vault, 7, 1)] == nil || daemon.queue.Units[claimUnitV2Key(daemon.cfg.Vault, claimV2TestCtfPool, 1)] == nil {
		t.Fatalf("units: %v", daemon.queue.Units)
	}
	daemon.requireIdleIn(7, 0, 5)
	daemon.requireIdleIn(claimV2TestCtfPool, 0, 2)

	chain.finalizePool(claimV2TestCtfPool, claimV2TestAccount(claimV2TestOther, 4), claimV2TestAccount(claimV2TestColdkey, 4.5))
	daemon.step(false)
	daemon.step(false)
	sends = chain.sent()
	if len(sends) != 3 {
		t.Fatalf("newer CTF root: %d sends", len(sends))
	}
	if intent := daemon.sentClaim(sends[2]); intent.NoID.Uint64() != claimV2TestCtfPool || intent.RootIndex.Uint64() != 1 || !chain.proves(intent, claimV2TestCtfPool) {
		t.Fatalf("newer CTF root claimed pool %s root %s", intent.NoID, intent.RootIndex)
	}
	daemon.requireIdleIn(7, 0, 5)
	daemon.requireIdleIn(claimV2TestCtfPool, 1, 4.5)
}

// The threshold applies per unit: a CTF account below it stays idle, with
// nothing sent for it, while the pool account is claimed; a demand, as
// --claim-now, then claims it alone.
func TestClaimV2CtfAccountThresholdIsPerUnit(t *testing.T) {
	chain := newClaimV2CtfTest(t, 1)
	daemon := newClaimV2TestDaemon(t, chain, ClaimV2DefaultThresholdRao)
	daemon.step(false)
	daemon.advance(time.Hour)
	daemon.step(false)
	sends := chain.sent()
	if len(sends) != 1 || daemon.sentClaim(sends[0]).NoID.Uint64() != 7 {
		t.Fatalf("below the threshold: %d sends", len(sends))
	}
	daemon.requireIdleIn(7, 0, 5)
	if unit, claimed := daemon.durableIn(claimV2TestCtfPool, 1), chain.claimedIn(claimV2TestCtfPool, 1); unit.Status != "idle" || unit.Claimed != "0" || unit.Attempts != 0 || !claimed.IsZero() {
		t.Fatalf("CTF unit below the threshold: %+v, claimed %s", unit, claimed.Dec())
	}
	daemon.step(true)
	sends = chain.sent()
	if len(sends) != 2 || daemon.sentClaim(sends[1]).NoID.Uint64() != claimV2TestCtfPool {
		t.Fatalf("demand: %d sends", len(sends))
	}
	daemon.requireIdleIn(claimV2TestCtfPool, 0, 1)
	daemon.requireIdleIn(7, 0, 5)
}

// With the claim batcher, the coldkey's due pool and CTF accounts go in one
// claimMany, its items naming each its own pool, in unit key order; each
// unit settles from its own Claimed event of vault2, with the batcher as
// caller.
func TestClaimV2BatchesPoolAndCtfAccounts(t *testing.T) {
	chain := newClaimV2CtfTest(t, 2)
	chain.batch = &claimV2TestBatcherModel{address: common.HexToAddress(claimV2TestBatcher)}
	daemon := newClaimV2TestDaemon(t, chain, ClaimV2DefaultThresholdRao)
	daemon.cfg.Batcher = claimV2TestBatcher
	pool, ctf := daemon.account(1), daemon.accountIn(claimV2TestCtfPool, 1)
	daemon.step(false)
	sends := chain.sent()
	if len(sends) != 1 {
		t.Fatalf("batch: %d sends", len(sends))
	}
	daemon.requireBatch(sends[0], 2, stClaimBatcher.PackClaimMany(common.HexToAddress(daemon.cfg.Vault), []stabi.STClaimBatcherClaim{claimV2Item(ctf), claimV2Item(pool)}, new(big.Int).SetUint64(claimV2TestBatchCap)))
	daemon.requireIdleIn(7, 0, 5)
	daemon.requireIdleIn(claimV2TestCtfPool, 0, 2)
	tx := claimV2TestSent(t, sends[0])
	var receipt *types.Receipt
	chain.with(func() { receipt = chain.receipts[tx.Hash()] })
	paid := map[uint64]bool{}
	for _, log := range receipt.Logs {
		if event, err := stSettlementVault2.UnpackClaimedEvent(log); err == nil && log.Address == chain.vault && event.Index.Uint64() == 1 && event.Caller == common.HexToAddress(claimV2TestBatcher) {
			paid[event.NoId.Uint64()] = true
		}
	}
	if len(paid) != 2 || !paid[7] || !paid[claimV2TestCtfPool] {
		t.Fatalf("Claimed events by pool: %v", paid)
	}
}
