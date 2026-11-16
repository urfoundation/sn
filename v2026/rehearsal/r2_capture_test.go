//go:build runtime_rehearsal

package rehearsal

import (
	"math/big"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/chain"
)

// R-2. TestR2Capture: captures through closeOperatorEpoch on the real
// runtime: a zero epoch, exact deltas, a dust refusal and its later capture,
// and the movable stake beside registration collateral (VERSION2-FINAL.md
// §15.8, SPEC A3).
func TestR2Capture(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	s := deployStack(t, n, stackConfig{operators: 2, shortFrom: 2})
	pool1, pool2 := s.operators[1].pool, s.operators[2].pool

	// Pool 2 joins while the subnet locks half of each registration price as
	// collateral on the registering coldkey's position.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_collateral_lock_share", types.NewU16(s.netuid), types.NewU16(1<<15)))
	s.registerPool(t, 2, 1)
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_collateral_lock_share", types.NewU16(s.netuid), types.NewU16(0)))
	locked := s.locked(t, pool2)
	if locked == 0 || n.stake(t, pool2, s.vaultColdkey, s.netuid) != locked {
		t.Fatalf("pool 2 collateral %d, getStake %d", locked, n.stake(t, pool2, s.vaultColdkey, s.netuid))
	}
	// Epochs stop (tempo 0), so no dividend reaches a position between reads.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(s.netuid), types.NewU16(0)))

	// capture closes epoch for pool noId and requires that the pool lose
	// exactly its movable stake, getStake − locked, and escrow gain that or
	// one rao less, recorded exactly as funding.
	capture := func(t *testing.T, epoch, noId uint64, hotkey [32]byte) uint64 {
		pool, escrow := n.stake(t, hotkey, s.vaultColdkey, s.netuid), n.stake(t, s.escrow, s.vaultColdkey, s.netuid)
		lock := s.locked(t, hotkey)
		movable := pool - lock
		captured := s.vaultUint(t, "capturedTotal", id(noId)).Uint64()
		receipt := s.close(t, epoch, noId)
		gained := n.stake(t, s.escrow, s.vaultColdkey, s.netuid) - escrow
		if n.stake(t, hotkey, s.vaultColdkey, s.netuid) != lock || s.locked(t, hotkey) != lock || gained != movable && gained != movable-1 {
			t.Fatalf("epoch %d pool %d: pool %d -> %d (lock %d), escrow +%d", epoch, noId, pool, n.stake(t, hotkey, s.vaultColdkey, s.netuid), lock, gained)
		}
		funded := id(captured + gained)
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "EmissionCaptured", map[string]any{
			"epoch": id(epoch), "noId": id(noId), "observed": id(movable), "amount": id(gained), "fundedThrough": funded,
		})
		if s.vaultUint(t, "lastFundedEpoch", id(noId)).Uint64() != epoch || s.vaultUint(t, "fundedThrough", id(noId), id(epoch)).Cmp(funded) != 0 ||
			s.vaultUint(t, "capturedTotal", id(noId)).Cmp(funded) != 0 {
			t.Fatalf("epoch %d pool %d: funding state differs from the capture", epoch, noId)
		}
		s.requireConservation(t)
		t.Logf("epoch %d pool %d: moved %d of getStake %d (lock %d), escrow +%d, gas %d", epoch, noId, movable, pool, lock, gained, receipt.GasUsed)
		return gained
	}

	t.Run("zero epoch", func(t *testing.T) {
		// Nothing is movable: pool 1 holds nothing and pool 2 only its lock.
		if capture(t, 1, 1, pool1) != 0 || capture(t, 1, 2, pool2) != 0 {
			t.Fatal("a zero epoch funded something")
		}
	})

	t.Run("exact deltas", func(t *testing.T) {
		n.stakeFor(t, alice, s.vaultColdkey, pool1, s.netuid, 100*tao)
		n.stakeFor(t, alice, s.vaultColdkey, pool2, s.netuid, 50*tao)
		capture(t, 2, 1, pool1)
		// Capture beside collateral moves exactly getStake − locked.
		capture(t, 2, 2, pool2)
	})

	alpha := forgeArtifact(t, "src/interfaces/alpha.sol", "IAlpha")
	priceQ9 := func(t *testing.T) uint64 {
		price := n.read(t, alphaAddress, alpha, "getAlphaPrice", s.netuid)[0].(*big.Int)
		return new(big.Int).Quo(price, big.NewInt(tao)).Uint64()
	}
	t.Run("dust", func(t *testing.T) {
		// A whale's stake lifts the price; a transfer to vault2 at that price
		// clears the 100,000-rao floor; the whale's unstake drops the price,
		// leaving the transferred alpha below the floor.
		whale := newColdkey(t)
		n.fund(t, whale.PublicKey(), 20_000*tao)
		reserves, err := chain.PoolReservesAtContext(n.ctx, n.sub, s.netuid, n.head(t))
		if err != nil {
			t.Fatal(err)
		}
		add, err := chain.AddStakeCall(n.sub.Meta, s.netuid, s.ownerHotkey.PublicKey(), 3*reserves.TaoRao)
		if err != nil {
			t.Fatal(err)
		}
		n.submit(t, whale.Ring, add)
		high := priceQ9(t)
		// 120,000 rao of TAO at the high price.
		dust := (120_000*tao + high - 1) / high
		donor := newColdkey(t)
		n.fund(t, donor.PublicKey(), 100*tao)
		add, err = chain.AddStakeCall(n.sub.Meta, s.netuid, pool1, tao)
		if err != nil {
			t.Fatal(err)
		}
		n.submit(t, donor.Ring, add)
		n.submit(t, donor.Ring, n.call(t, "SubtensorModule.transfer_stake", accountID(t, s.vaultColdkey), accountID(t, pool1),
			types.NewU16(s.netuid), types.NewU16(s.netuid), types.NewU64(dust)))
		whaleStake := n.stake(t, s.ownerHotkey.PublicKey(), whale.PublicKey(), s.netuid)
		n.submit(t, whale.Ring, n.call(t, "SubtensorModule.remove_stake", accountID(t, s.ownerHotkey.PublicKey()),
			types.NewU16(s.netuid), types.NewU64(whaleStake)))
		low := priceQ9(t)
		held := n.stake(t, pool1, s.vaultColdkey, s.netuid)
		if held != dust || held*low/tao >= minimumTransferTaoRao {
			t.Fatalf("pool 1 holds %d alpha rao at price %d/10^9: not dust", held, low)
		}

		// The close reverts with the runtime's refusal and records nothing.
		s.inWindow(t, 3)
		n.refused(t, s.owner, s.settlement, s.settlementArtifact, nil, "closeOperatorEpoch", id(3), id(1))
		_, err = n.ethCall(s.vault, stakingAddress, n.staking.pack(t, "moveStake", pool1, s.escrow, id(uint64(s.netuid)), id(uint64(s.netuid)), id(dust)))
		if err == nil || !strings.Contains(err.Error(), "AmountTooLow") || s.vaultUint(t, "lastFundedEpoch", id(1)).Uint64() != 2 ||
			n.stake(t, pool1, s.vaultColdkey, s.netuid) != dust {
			t.Fatalf("dust capture: runtime reason %v, lastFundedEpoch %s", err, s.vaultUint(t, "lastFundedEpoch", id(1)))
		}
		t.Logf("dust: %d alpha rao at price %d/10^9 (%d at the high price %d) refused: %v", dust, low, dust*high/tao, high, err)

		// The next close defers epoch 3 and captures the dust with a top-up.
		n.stakeFor(t, alice, s.vaultColdkey, pool1, s.netuid, 10*tao)
		funded := s.vaultUint(t, "capturedTotal", id(1))
		s.inWindow(t, 4)
		receipt := n.send(t, s.owner, s.settlement, s.settlementArtifact, nil, "closeOperatorEpoch", id(4), id(1))
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "EmissionDeferred", map[string]any{
			"epoch": id(3), "noId": id(1), "fundedThrough": funded,
		})
		if n.stake(t, pool1, s.vaultColdkey, s.netuid) != 0 || s.vaultUint(t, "fundedThrough", id(1), id(3)).Cmp(funded) != 0 ||
			s.vaultUint(t, "lastFundedEpoch", id(1)).Uint64() != 4 {
			t.Fatal("epoch 4 did not defer epoch 3 and capture the dust")
		}
		s.requireConservation(t)
	})
}
