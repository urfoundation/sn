//go:build runtime_rehearsal

package rehearsal

import (
	"math/big"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	ethereum "github.com/ethereum/go-ethereum"
	gethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/miner/onchain"
)

// mainnetTempo is SN25's tempo, so that the activity cutoff (factor × tempo)
// spans a challenge window.
const mainnetTempo = 360

// R-3. TestR3Claim: claims with a real transferStake; a floor refusal and a
// staking-hotkey cap refusal, each leaving the account claimable; third-party
// stake operations on the escrow hotkey, which never lower vault2's position;
// the escrow hotkey's earnings across a tempo with a weight-setting validator
// and without weights; the whole-position sentinel; and the staking-hotkey cap
// of vault2's own coldkey (VERSION2-FINAL.md §15.8, SPEC A4).
func TestR3Claim(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	s := deployStack(t, n, stackConfig{operators: 1, shortFrom: 2})
	operator := s.operators[1]
	nu := id(uint64(s.netuid))

	// SN25's tempo and the widest activity cutoff, 18,000 blocks; weights
	// without commit-reveal, rate limit or minimum count. The subnet owner
	// hotkey validates with most of the stake, as SN25's validators hold
	// most of its stake; a fresh hotkey mines.
	validator := s.ownerHotkey
	for _, call := range []types.Call{
		n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(s.netuid), types.NewU16(mainnetTempo)),
		n.call(t, "AdminUtils.sudo_set_activity_cutoff_factor", types.NewU16(s.netuid), types.NewU32(50_000)),
		n.call(t, "AdminUtils.sudo_set_commit_reveal_weights_enabled", types.NewU16(s.netuid), types.NewBool(false)),
		n.call(t, "AdminUtils.sudo_set_weights_set_rate_limit", types.NewU16(s.netuid), types.NewU64(0)),
		n.call(t, "AdminUtils.sudo_set_min_allowed_weights", types.NewU16(s.netuid), types.NewU16(1)),
	} {
		n.sudo(t, call)
	}
	miner, minerOwner := newAccount(t), newColdkey(t)
	n.fund(t, minerOwner.PublicKey(), 100*tao)
	minerUID := n.registerHotkey(t, minerOwner.Ring, s.netuid, miner)
	n.submit(t, alice, n.addStakeCall(t, validator.PublicKey(), s.netuid, 2_000*tao))
	setWeights := func(t *testing.T, uids ...uint16) {
		values := make([]types.U16, len(uids))
		dests := make([]types.U16, len(uids))
		for i, uid := range uids {
			dests[i], values[i] = types.NewU16(uid), types.NewU16(65535)
		}
		n.submit(t, validator.Ring, n.call(t, "SubtensorModule.set_weights", types.NewU16(s.netuid), dests, values, types.NewU64(0)))
	}

	// Epoch 1 captures the pool's stake; the root commits four accounts: a
	// large one, one below the transfer floor, one whose coldkey will hold
	// 128 staking hotkeys, and another.
	n.stakeFor(t, alice, s.vaultColdkey, operator.pool, s.netuid, 200*tao)
	s.close(t, 1, 1)
	funded := s.vaultUint(t, "fundedThrough", id(1), id(1)).Uint64()
	alpha := forgeArtifact(t, "src/interfaces/alpha.sol", "IAlpha")
	priceQ9 := new(big.Int).Quo(n.read(t, alphaAddress, alpha, "getAlphaPrice", s.netuid)[0].(*big.Int), big.NewInt(tao)).Uint64()
	dust := minimumTransferTaoRao * tao / priceQ9 / 2
	capped := newColdkey(t)
	large, other := newAccount(t), newAccount(t)
	accounts := []merkle.LedgerAccount{
		ledgerAccount(large, funded-dust-2*tao-tao), ledgerAccount(newAccount(t), dust),
		ledgerAccount(capped.PublicKey(), 2*tao), ledgerAccount(other, tao),
	}
	root := newLedger(t, accounts, uint64(len(accounts)))
	committed := root.commitment("ledger header epoch 1")
	n.send(t, operator.rootSigner, s.settlement, s.settlementArtifact, nil, "commitDistribution", id(1), id(1), id(0), committed)
	n.send(t, s.auditor, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(1), committed.Root, committed.LedgerHash)
	position := func(t *testing.T) uint64 { return n.stake(t, s.escrow, s.vaultColdkey, s.netuid) }

	// R-3: without weights, Yuma's fallback pays validator emission by stake
	// to active permit holders, the escrow hotkey included once an epoch has
	// given it a permit; once a validator sets weights, the escrow hotkey
	// earns nothing.
	t.Run("escrow earnings", func(t *testing.T) {
		before := position(t)
		n.advance(t, 2*mainnetTempo+1)
		without := position(t) - before
		setWeights(t, minerUID)
		before = position(t)
		minerBefore := n.hotkeyAlpha(t, miner, s.netuid)
		n.advance(t, 2*mainnetTempo+1)
		if position(t) != before || n.hotkeyAlpha(t, miner, s.netuid) <= minerBefore {
			t.Fatalf("with weights the escrow position went %d -> %d and the miner %d -> %d",
				before, position(t), minerBefore, n.hotkeyAlpha(t, miner, s.netuid))
		}
		s.requireConservation(t)
		t.Logf("escrow hotkey: +%d rao over two tempos without weights; +0 over two tempos while a validator weights a miner", without)
	})

	// R-3: no third-party stake operation on the escrow hotkey lowers
	// vault2's position there; a transfer of conviction-locked alpha to
	// vault2's coldkey is refused.
	t.Run("third parties", func(t *testing.T) {
		third := newColdkey(t)
		n.fund(t, third.PublicKey(), 1_000*tao)
		require := func(label string, increase bool, call types.Call) {
			before := position(t)
			n.submit(t, third.Ring, call)
			if after := position(t); after < before || !increase && after != before {
				t.Fatalf("%s moved vault2's escrow position %d -> %d", label, before, after)
			}
		}
		require("add_stake", false, n.addStakeCall(t, s.escrow, s.netuid, 100*tao))
		held := n.stake(t, s.escrow, third.PublicKey(), s.netuid)
		require("move_stake off", false, n.call(t, "SubtensorModule.move_stake", accountID(t, s.escrow), accountID(t, miner), types.NewU16(s.netuid), types.NewU16(s.netuid), types.NewU64(held/4)))
		require("move_stake on", false, n.call(t, "SubtensorModule.move_stake", accountID(t, miner), accountID(t, s.escrow), types.NewU16(s.netuid), types.NewU16(s.netuid), types.NewU64(wholePosition)))
		require("remove_stake", false, n.call(t, "SubtensorModule.remove_stake", accountID(t, s.escrow), types.NewU16(s.netuid), types.NewU64(held/4)))
		require("transfer_stake to vault2", true, n.transferStakeCall(t, s.vaultColdkey, s.escrow, s.netuid, held/4))
		remaining := n.stake(t, s.escrow, third.PublicKey(), s.netuid)
		n.submit(t, third.Ring, n.call(t, "SubtensorModule.lock_stake", accountID(t, s.escrow), types.NewU16(s.netuid), types.NewU64(remaining/2)))
		before := position(t)
		if _, err := n.trySubmit(t, third.Ring, n.transferStakeCall(t, s.vaultColdkey, s.escrow, s.netuid, remaining)); err == nil ||
			!strings.Contains(err.Error(), "AccountRejectsLockedAlpha") || position(t) != before {
			t.Fatalf("transfer of locked alpha to vault2: %v", err)
		}
		s.requireConservation(t)
	})

	// R-3: the claimant's coldkey holds MAX_THIRD_PARTY_STAKING_HOTKEYS (128)
	// staking hotkeys, so the claim's transfer, which would add the escrow
	// hotkey, is refused.
	n.fund(t, capped.PublicKey(), 100*tao)
	for batch := 0; batch < 16; batch++ {
		var calls []types.Call
		for range 8 {
			hotkey := newAccount(t)
			calls = append(calls, n.call(t, "SubtensorModule.try_associate_hotkey", accountID(t, hotkey)), n.addStakeCall(t, hotkey, s.netuid, 3_000_000))
		}
		n.submit(t, capped.Ring, n.batch(t, calls...))
	}
	if got := len(n.stakingHotkeys(t, capped.PublicKey())); got != 128 {
		t.Fatalf("the capped coldkey holds %d staking hotkeys", got)
	}

	n.sealUntil(t, s.pending(t, 1).CommittedBlock+challengeBlocks)
	n.send(t, operator.rootSigner, s.settlement, s.settlementArtifact, nil, "finalizeDistribution", id(1))
	relayer := newEVMKey(t)
	n.fundEVM(t, relayer.address, 1_000*tao)
	// claim pays account index with the least gas limit that succeeds and
	// returns the gas used and that limit.
	claim := func(t *testing.T, index uint64) (used, least uint64) {
		a := root.account(index)
		before, escrow := n.stake(t, s.escrow, a.Coldkey, s.netuid), position(t)
		receipt, _, least := n.sendLeast(t, relayer, s.vault, s.vaultArtifact, nil, "claim", id(1), id(0), a, root.proof(index))
		amount := a.Cumulative.Uint64()
		gained := n.stake(t, s.escrow, a.Coldkey, s.netuid) - before
		if escrow-position(t) != amount || gained != amount && gained != amount-1 {
			t.Fatalf("claim %d of %d rao: escrow -%d, account +%d", index, amount, escrow-position(t), gained)
		}
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "Claimed", map[string]any{
			"noId": id(1), "rootIndex": id(0), "index": id(index), "coldkey": a.Coldkey,
			"cumulative": a.Cumulative, "amount": id(amount), "caller": relayer.address,
		})
		s.requireConservation(t)
		t.Logf("claim %d: %d rao, account +%d, gas %d, least limit %d", index, amount, gained, receipt.GasUsed, least)
		return receipt.GasUsed, least
	}
	// refusedClaim requires the claim to revert with the runtime's reason
	// for the transfer, and the account to stay claimable.
	refusedClaim := func(t *testing.T, index uint64, want string) {
		a := root.account(index)
		n.refused(t, relayer, s.vault, s.vaultArtifact, nil, "claim", id(1), id(0), a, root.proof(index))
		_, err := n.ethCall(s.vault, stakingAddress, n.staking.pack(t, "transferStake", a.Coldkey, s.escrow, nu, nu, a.Cumulative))
		if err == nil || !strings.Contains(err.Error(), want) || s.vaultUint(t, "claimed", id(1), id(index)).Sign() != 0 {
			t.Fatalf("claim %d: runtime reason %v, want %s and the account claimable", index, err, want)
		}
	}

	var short, shortLeast uint64
	t.Run("claim", func(t *testing.T) {
		short, shortLeast = claim(t, 3)
	})

	// R-3: third parties can add hotkeys to vault2's own StakingHotkeys only
	// until it holds 128; vault2's own moves face the larger cap of 256. On
	// runtime 478 the gas a claim needs follows that list (§7.3), so a claim
	// signed before third parties fill it is outgrown at an estimate with
	// 20%, while the claim tools' size, at least onchain.ClaimBatchGasPerClaim,
	// still pays.
	t.Run("own cap", func(t *testing.T) {
		a := root.account(0)
		data := s.vaultArtifact.pack(t, "claim", id(1), id(0), a, root.proof(0))
		estimate, err := n.estimate(relayer.address, s.vault, nil, data)
		if err != nil {
			t.Fatalf("estimate a claim: %s", reason(err))
		}
		price, err := n.eth.SuggestGasPrice(n.ctx)
		if err != nil {
			t.Fatal(err)
		}
		price.Mul(price, big.NewInt(2))
		estimated, sized := newEVMKey(t), newEVMKey(t)
		n.fundEVM(t, estimated.address, 100*tao)
		n.fundEVM(t, sized.address, 100*tao)
		outgrown := n.sign(t, estimated, &s.vault, nil, data, estimate+estimate/5, price)
		covered := n.sign(t, sized, &s.vault, nil, data, max(estimate+estimate/5, onchain.ClaimBatchGasPerClaim), price)

		var foreign [32]byte
		donor := newColdkey(t)
		n.fund(t, donor.PublicKey(), 1_000*tao)
		for {
			if len(n.stakingHotkeys(t, s.vaultColdkey)) > 128 {
				t.Fatal("third parties filled vault2's list past 128")
			}
			hotkey := newAccount(t)
			_, err := n.trySubmit(t, donor.Ring, n.batch(t, n.call(t, "SubtensorModule.try_associate_hotkey", accountID(t, hotkey)),
				n.addStakeCall(t, hotkey, s.netuid, 3_000_000), n.transferStakeCall(t, s.vaultColdkey, hotkey, s.netuid, wholePosition)))
			if err != nil {
				if !strings.Contains(err.Error(), "TooManyStakingHotkeys") {
					t.Fatalf("third-party transfer to vault2: %v", err)
				}
				break
			}
			foreign = hotkey
		}
		count := len(n.stakingHotkeys(t, s.vaultColdkey))
		// vault2 moves its stake off a third-party hotkey into escrow.
		held := n.stake(t, foreign, s.vaultColdkey, s.netuid)
		live := s.vaultUint(t, "liveEscrowStake").Uint64()
		receipt, _, absorbLeast := n.sendLeast(t, operator.rootSigner, s.vault, s.vaultArtifact, nil, "absorbStake", foreign)
		if gained := s.vaultUint(t, "liveEscrowStake").Uint64() - live; gained != held && gained != held-1 {
			t.Fatalf("absorbStake with a full list moved %d of %d", gained, held)
		}
		// A claim, a transfer out of vault2's coldkey, still pays, but only
		// with the gas the longer list needs.
		fullLeast := n.least(t, ethereum.CallMsg{From: relayer.address, To: &s.vault, Data: data}, refusedGas, func(_ []byte, err error) bool { return err == nil })
		if failed := n.sendSigned(t, outgrown); failed.Status != gethtypes.ReceiptStatusFailed || s.vaultUint(t, "claimed", id(1), id(0)).Sign() != 0 {
			t.Fatalf("a claim signed at %d gas before the list filled: status %d, claimed %s", outgrown.Gas(), failed.Status, s.vaultUint(t, "claimed", id(1), id(0)))
		}
		before, escrow := n.stake(t, s.escrow, a.Coldkey, s.netuid), position(t)
		paid := n.sendSigned(t, covered)
		amount := a.Cumulative.Uint64()
		gained := n.stake(t, s.escrow, a.Coldkey, s.netuid) - before
		if paid.Status != gethtypes.ReceiptStatusSuccessful || escrow-position(t) != amount || gained != amount && gained != amount-1 {
			t.Fatalf("a claim signed at %d gas before the list filled: status %d, escrow -%d, account +%d", covered.Gas(), paid.Status, escrow-position(t), gained)
		}
		requireEvent(t, paid, s.vaultArtifact, s.vault, "Claimed", map[string]any{
			"noId": id(1), "rootIndex": id(0), "index": id(0), "coldkey": a.Coldkey,
			"cumulative": a.Cumulative, "amount": id(amount), "caller": sized.address,
		})
		s.requireConservation(t)
		t.Logf("vault2's StakingHotkeys reached %d through third parties and the next transfer was refused; absorbStake still moved %d rao (gas %d, least limit %d); a claim then used %d gas (least limit %d) against %d (least limit %d) before the list filled",
			count, held, receipt.GasUsed, absorbLeast, paid.GasUsed, fullLeast, short, shortLeast)
		t.Logf("a claim estimated at %d gas before the list filled failed signed at %d and paid signed at %d", estimate, outgrown.Gas(), covered.Gas())
	})

	t.Run("floor refusal", func(t *testing.T) {
		refusedClaim(t, 1, "AmountTooLow")
		t.Logf("a %d rao claim at price %d/10^9 was refused below the 100,000-rao floor", dust, priceQ9)
	})
	t.Run("cap refusal", func(t *testing.T) {
		refusedClaim(t, 2, "TooManyStakingHotkeys")
		// Freeing one entry makes the account's claim go through.
		hotkey := n.stakingHotkeys(t, capped.PublicKey())[0]
		n.submit(t, capped.Ring, n.call(t, "SubtensorModule.remove_stake", hotkey, types.NewU16(s.netuid),
			types.NewU64(n.stake(t, [32]byte(hotkey), capped.PublicKey(), s.netuid))))
		claim(t, 2)
	})

	// R-3: the runtime reads 2^64 − 1 as the whole position, which vault2
	// never passes: every amount it sends is a captured or claimed amount,
	// and a pool can never fund 2^64 − 1 rao.
	t.Run("whole-position sentinel", func(t *testing.T) {
		whole := position(t)
		if _, err := n.ethCall(s.vault, stakingAddress, n.staking.pack(t, "transferStake", newAccount(t), s.escrow, nu, nu, id(wholePosition))); err != nil {
			t.Fatalf("the sentinel transfer simulated from vault2: %v", err)
		}
		_, err := n.ethCall(s.vault, stakingAddress, n.staking.pack(t, "transferStake", newAccount(t), s.escrow, nu, nu, id(wholePosition-1)))
		if err == nil || !strings.Contains(err.Error(), "NotEnoughStakeToWithdraw") {
			t.Fatalf("2^64 − 2 simulated from vault2: %v", err)
		}
		t.Logf("simulated from vault2, transferStake(2^64 − 1) would move the whole escrow position, %d rao; 2^64 − 2 is refused", whole)
	})
}
