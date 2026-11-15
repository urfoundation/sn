//go:build runtime_rehearsal

package rehearsal

import (
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	ethereum "github.com/ethereum/go-ethereum"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
)

// Runtime 478's weights behind the staking precompile's admission of
// moveStake and transferStake (upstream 850f2755): one gas is 40,000 of
// weight (runtime/src/lib.rs, weight_per_gas), a storage read weighs
// 25,000,000 and a write 100,000,000 (RocksDbWeight), a StakingHotkeys walk
// reads 14 times per entry and once more (STAKING_HOTKEYS_WALK_READS_PER_ENTRY,
// staking_hotkeys_walk_weight), and move_stake and transfer_stake weigh
// 260,676,000 and 313,389,000 with 29 and 27 reads and 12 and 11 writes
// (pallets/subtensor/src/weights.rs). Every call's declared weight also holds
// the coldkey-swap guard's, 10,000,000 and 2 reads (check_coldkey_swap_extension;
// the runtime's DispatchExtension), which no dispatch is charged.
const (
	weightPerGas        = 40_000
	readWeight          = 25_000_000
	writeWeight         = 100_000_000
	walkReadsPerEntry   = 14
	maxStakingHotkeys   = 256
	guardWeight         = 10_000_000 + 2*readWeight
	moveStakeWeight     = 260_676_000 + 29*readWeight + 12*writeWeight
	transferStakeWeight = 313_389_000 + 27*readWeight + 11*writeWeight
)

// admissionGas is the least gas at which runtime 478 admits a stake exit of
// weight `call` from a caller whose StakingHotkeys list holds `entries`: the
// guard's weight, the call's own and a walk of one entry more, at most the 256
// a list may hold (precompiles/src/extensions.rs, try_dispatch_stake_exit).
// Runtimes 475 and 477 admitted every caller against the walk of 256.
func admissionGas(call uint64, entries int) uint64 {
	walked := min(entries+1, maxStakingHotkeys)
	weight := guardWeight + call + readWeight*uint64(walkReadsPerEntry*walked+1)
	return (weight + weightPerGas - 1) / weightPerGas
}

// R-6. TestRehearsalStakeExitAdmission measures, by eth_call from a
// contract's address straight to the staking precompile, the least gas at
// which runtime 478 admits moveStake and transferStake, as third parties
// lengthen the contract's own StakingHotkeys list up to the 128 entries they
// may add (VERSION2-FINAL.md §7.3, §17). It is exactly the call's declared
// weight with its walk cut to the caller's list plus one entry: 8,750 gas an
// entry, where runtimes 475 and 477 needed the walk of 256 entries, 2,240,625
// gas, whatever the list. A move onto a hotkey new to the list needs no more:
// the walk it is charged, of the list it leaves, is the one admitted. A call
// sent with one gas less is refused and consumes all the gas forwarded to it.
func TestRehearsalStakeExitAdmission(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	p := deployProbe(t, n)

	// hotA and hotB hold the probe's stake; hotC is registered and not on
	// the probe's list. Epochs stop (tempo 0), so the list changes only here.
	netuid := n.createSubnet(t, alice, newAccount(t))
	operator := newColdkey(t)
	n.fund(t, operator.PublicKey(), 100*tao)
	hotA, hotB, hotC := newAccount(t), newAccount(t), newAccount(t)
	for _, hotkey := range [][32]byte{hotA, hotB, hotC} {
		n.registerHotkey(t, operator.Ring, netuid, hotkey)
	}
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_admin_freeze_window", types.NewU16(0)))
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(netuid), types.NewU16(0)))
	n.stakeFor(t, alice, p.coldkey, hotA, netuid, 1_000*tao)
	n.stakeFor(t, alice, p.coldkey, hotB, netuid, 10*tao)
	nu := amount(uint64(netuid))
	calls := []struct {
		label  string
		data   []byte
		weight uint64
	}{
		{"moveStake", n.staking.pack(t, "moveStake", hotA, hotB, nu, nu, amount(tao)), moveStakeWeight},
		{"moveStake to a new hotkey", n.staking.pack(t, "moveStake", hotA, hotC, nu, nu, amount(tao)), moveStakeWeight},
		{"transferStake", n.staking.pack(t, "transferStake", newAccount(t), hotA, nu, nu, amount(tao)), transferStakeWeight},
	}
	succeeds := func(_ []byte, err error) bool { return err == nil }

	donor := newColdkey(t)
	n.fund(t, donor.PublicKey(), 1_000*tao)
	failed := false
	for _, entries := range []int{2, 3, 4, 16, 64, 128} {
		for len(n.stakingHotkeys(t, p.coldkey)) < entries {
			hotkey := newAccount(t)
			n.submit(t, donor.Ring, n.batch(t, n.call(t, "SubtensorModule.try_associate_hotkey", accountID(t, hotkey)),
				n.addStakeCall(t, hotkey, netuid, 3_000_000), n.transferStakeCall(t, p.coldkey, hotkey, netuid, wholePosition)))
		}
		if got := len(n.stakingHotkeys(t, p.coldkey)); got != entries {
			t.Fatalf("the probe's StakingHotkeys holds %d entries, want %d", got, entries)
		}
		for _, call := range calls {
			// The precompile runs with the call's gas less the transaction's
			// intrinsic gas: 21,000 and 16 or 4 gas a calldata byte.
			intrinsic := uint64(21_000)
			for _, b := range call.data {
				if b == 0 {
					intrinsic += 4
				} else {
					intrinsic += 16
				}
			}
			least := n.least(t, ethereum.CallMsg{From: p.address, To: &stakingAddress, Data: call.data}, refusedGas, succeeds) - intrinsic
			want := admissionGas(call.weight, entries)
			t.Logf("%d entries: %s admitted with %d gas, want %d", entries, call.label, least, want)
			failed = failed || least != want
		}
	}
	if failed {
		t.Fatal("the precompile's admission differs from runtime 478's rule")
	}

	// One gas below its least limit, the probe's moveStake fails in a
	// transaction and keeps at most the 1/64 the probe held back (EIP-150).
	data := p.artifact.pack(t, "forward", stakingAddress, calls[0].data)
	least := n.least(t, ethereum.CallMsg{From: p.sender.address, To: &p.address, Data: data}, refusedGas, succeeds)
	receipt := n.transact(t, p.sender, &p.address, nil, data, least-1)
	if receipt.Status != gethtypes.ReceiptStatusFailed || receipt.GasUsed < least-1-(least-1)/64 {
		t.Fatalf("moveStake sent with %d gas, one below its least limit: status %d, used %d", least-1, receipt.Status, receipt.GasUsed)
	}
	t.Logf("with 128 entries the probe's moveStake needs a limit of %d; sent with one gas less it failed and used %d", least, receipt.GasUsed)
	if receipt = n.transact(t, p.sender, &p.address, nil, data, least); receipt.Status != gethtypes.ReceiptStatusSuccessful {
		t.Fatalf("moveStake sent with its least limit %d failed", least)
	}
	t.Logf("sent with %d it used %d", least, receipt.GasUsed)
}
