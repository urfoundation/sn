//go:build runtime_rehearsal

package rehearsal

import (
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/chain"
)

const (
	// refusedGas is the gas limit of a transaction that must fail.
	refusedGas = 10_000_000
	// minimumTransferTaoRao is the runtime's DefaultMinTransfer (§7.3).
	minimumTransferTaoRao = 100_000
)

// probe is a deployed evm/test/rehearsal/RehearsalProbe.sol. Its mapped
// coldkey owns the stake it moves.
type probe struct {
	node     *node
	artifact artifact
	sender   evmKey
	address  common.Address
	coldkey  [32]byte
}

// gas is one precompile call's cost: inside the probe, the transaction's gas
// used, and the transaction's eth_estimateGas limit.
type gas struct {
	call, used, limit uint64
}

func deployProbe(t *testing.T, n *node) *probe {
	t.Helper()
	p := &probe{node: n, artifact: forgeArtifact(t, "test/rehearsal/RehearsalProbe.sol", "RehearsalProbe"), sender: newEVMKey(t)}
	n.fundEVM(t, p.sender.address, 100*tao)
	p.address = n.deploy(t, p.sender, p.artifact.creation)
	p.coldkey = mirror(p.address)
	return p
}

// forward has the probe make one precompile call in its own transaction.
func (self *probe) forward(t *testing.T, target common.Address, data []byte) gas {
	t.Helper()
	receipt := self.node.transact(t, self.sender, &self.address, nil, self.artifact.pack(t, "forward", target, data), 0)
	if receipt.Status != gethtypes.ReceiptStatusSuccessful {
		t.Fatalf("forward to %s failed", target)
	}
	tx, _, err := self.node.eth.TransactionByHash(self.node.ctx, receipt.TxHash)
	if err != nil {
		t.Fatal(err)
	}
	event := self.artifact.abi.Events["Forwarded"]
	for _, log := range receipt.Logs {
		if log.Address == self.address && len(log.Topics) == 2 && log.Topics[0] == event.ID {
			values, err := event.Inputs.NonIndexed().Unpack(log.Data)
			if err != nil {
				t.Fatal(err)
			}
			return gas{call: values[0].(*big.Int).Uint64(), used: receipt.GasUsed, limit: tx.Gas()}
		}
	}
	t.Fatal("forward emitted no Forwarded event")
	return gas{}
}

// refuse sends the probe's call in a transaction that must fail and returns
// the runtime's reason, read by an eth_call from the probe's address.
func (self *probe) refuse(t *testing.T, target common.Address, data []byte) string {
	t.Helper()
	receipt := self.node.transact(t, self.sender, &self.address, nil, self.artifact.pack(t, "forward", target, data), refusedGas)
	if receipt.Status != gethtypes.ReceiptStatusFailed {
		t.Fatalf("transaction %s succeeded; a refusal was expected", receipt.TxHash)
	}
	t.Logf("refused transaction used %d of %d gas", receipt.GasUsed, refusedGas)
	if _, err := self.node.ethCall(self.address, target, data); err != nil {
		return err.Error()
	}
	t.Fatal("eth_call of the refused call succeeded")
	return ""
}

// view runs a view precompile call through the probe and returns its output
// and the gas it used inside the probe.
func (self *probe) view(t *testing.T, target common.Address, data []byte) ([]byte, uint64) {
	t.Helper()
	out, err := self.node.ethCall(self.sender.address, self.address, self.artifact.pack(t, "forwardView", target, data))
	if err != nil {
		t.Fatalf("forwardView: %v", err)
	}
	values := self.artifact.unpack(t, "forwardView", out)
	return values[0].([]byte), values[1].(*big.Int).Uint64()
}

func amount(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}

// TestRehearsalSmoke checks, on the real staking and neuron precompiles of a
// node built from a reviewed runtime's source, the §7.3 runtime facts that
// need no settlement contract, and logs each precompile call's gas.
func TestRehearsalSmoke(t *testing.T) {
	n := startNode(t)
	p := deployProbe(t, n)

	// An emitting subnet; hotA and hotB belong to another coldkey. The probe
	// stakes on hotA, a third party on hotB.
	netuid := n.createSubnet(t, alice, newAccount(t))
	operator := newColdkey(t)
	n.fund(t, operator.PublicKey(), 100*tao)
	hotA, hotB, hotC := newAccount(t), newAccount(t), newAccount(t)
	n.registerHotkey(t, operator.Ring, netuid, hotA)
	n.registerHotkey(t, operator.Ring, netuid, hotB)
	n.fund(t, p.coldkey, 100*tao)
	n.stakeFor(t, alice, p.coldkey, hotA, netuid, 1_000*tao)
	n.stakeFor(t, alice, newAccount(t), hotB, netuid, 1_000*tao)

	// Two tempos of emission pay dividends to both hotkeys, so their share
	// price rises above 1. Tempo 0 then stops epochs, so every delta below is
	// the operation's alone, and hotC, registered after, keeps share price 1.
	n.advance(t, 22)
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_admin_freeze_window", types.NewU16(0)))
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(netuid), types.NewU16(0)))
	frozen := n.stake(t, hotA, p.coldkey, netuid)
	start := time.Now()
	n.advance(t, 100)
	t.Logf("sealed 100 empty blocks in %v", time.Since(start))
	if n.stake(t, hotA, p.coldkey, netuid) != frozen || n.hotkeyAlpha(t, hotA, netuid) <= frozen {
		t.Fatalf("hotA did not earn dividends and freeze: stake %d, then %d, TotalHotkeyAlpha %d",
			frozen, n.stake(t, hotA, p.coldkey, netuid), n.hotkeyAlpha(t, hotA, netuid))
	}
	n.registerHotkey(t, operator.Ring, netuid, hotC)
	gasUse := map[string]gas{}
	nu := amount(uint64(netuid))

	t.Run("getStake", func(t *testing.T) {
		out, used := p.view(t, stakingAddress, n.staking.pack(t, "getStake", hotA, p.coldkey, nu))
		stake := n.staking.unpack(t, "getStake", out)[0].(*big.Int).Uint64()
		take := n.stake(t, hotA, operator.PublicKey(), netuid)
		pool := n.hotkeyAlpha(t, hotA, netuid)
		// hotA's stakers are the probe and the operator, whose take is paid
		// as stake; each share-pool read may floor one rao.
		if stake == 0 || stake != n.stake(t, hotA, p.coldkey, netuid) || pool < stake+take || pool > stake+take+2 {
			t.Fatalf("getStake %d, direct %d, operator %d, TotalHotkeyAlpha %d", stake, n.stake(t, hotA, p.coldkey, netuid), take, pool)
		}
		gasUse["getStake"] = gas{call: used}
		t.Logf("getStake: %d rao; operator take %d; TotalHotkeyAlpha %d", stake, take, pool)
	})

	// The source always loses exactly the amount, in getStake and in
	// TotalHotkeyAlpha, and the destination's TotalHotkeyAlpha gains exactly
	// the amount. The destination's getStake gains exactly the amount on a
	// hotkey at share price 1, and the amount or one rao less otherwise.
	moved := func(t *testing.T, label, method string, sourceHot, destinationHot, destinationCold [32]byte, exact bool) {
		short := 0
		amounts := []uint64{1 * tao, 123_456_789, 2*tao + 1, 314_159_265, 100_000_007, 999_999_937, 5*tao + 3, 271_828_183}
		for _, value := range amounts {
			source, destination := n.stake(t, sourceHot, p.coldkey, netuid), n.stake(t, destinationHot, destinationCold, netuid)
			sourcePool, destinationPool := n.hotkeyAlpha(t, sourceHot, netuid), n.hotkeyAlpha(t, destinationHot, netuid)
			data := n.staking.pack(t, "transferStake", destinationCold, sourceHot, nu, nu, amount(value))
			if method == "moveStake" {
				data = n.staking.pack(t, "moveStake", sourceHot, destinationHot, nu, nu, amount(value))
			}
			if use := p.forward(t, stakingAddress, data); use.used > gasUse[method].used {
				gasUse[method] = use
			}
			sourceDelta := source - n.stake(t, sourceHot, p.coldkey, netuid)
			destinationDelta := n.stake(t, destinationHot, destinationCold, netuid) - destination
			if sourceDelta != value || destinationDelta != value && (exact || destinationDelta != value-1) {
				t.Fatalf("%s %d: source -%d, destination +%d", label, value, sourceDelta, destinationDelta)
			}
			wantSource, wantDestination := sourcePool-value, destinationPool+value
			if sourceHot == destinationHot {
				wantSource, wantDestination = sourcePool, sourcePool
			}
			if n.hotkeyAlpha(t, sourceHot, netuid) != wantSource || n.hotkeyAlpha(t, destinationHot, netuid) != wantDestination {
				t.Fatalf("%s %d: TotalHotkeyAlpha moved by other than the amount", label, value)
			}
			if destinationDelta != value {
				short++
			}
		}
		t.Logf("%s: destination one rao short in %d of %d", label, short, len(amounts))
	}

	t.Run("moveStake", func(t *testing.T) {
		moved(t, "moveStake to an earning hotkey", "moveStake", hotA, hotB, p.coldkey, false)
		moved(t, "moveStake to a hotkey at share price 1", "moveStake", hotA, hotC, p.coldkey, true)
	})

	coldD := newAccount(t)
	t.Run("transferStake", func(t *testing.T) {
		moved(t, "transferStake on an earning hotkey", "transferStake", hotA, hotA, coldD, false)
		moved(t, "transferStake on a hotkey at share price 1", "transferStake", hotC, hotC, newAccount(t), true)
	})

	// The smallest accepted amount, found by eth_call from the probe, moves
	// exactly; one rao less is refused with AmountTooLow; and the floor is
	// 100,000 rao of TAO at the subnet's alpha price.
	t.Run("floor", func(t *testing.T) {
		alpha := forgeArtifact(t, "src/interfaces/alpha.sol", "IAlpha")
		for _, call := range []struct {
			label string
			data  func(uint64) []byte
		}{
			{"moveStake", func(value uint64) []byte { return n.staking.pack(t, "moveStake", hotA, hotB, nu, nu, amount(value)) }},
			{"transferStake", func(value uint64) []byte {
				return n.staking.pack(t, "transferStake", coldD, hotA, nu, nu, amount(value))
			}},
		} {
			accepted := func(value uint64) bool {
				_, err := n.ethCall(p.address, stakingAddress, call.data(value))
				return err == nil
			}
			low, high := uint64(1), uint64(10*minimumTransferTaoRao)
			if accepted(low) || !accepted(high) {
				t.Fatalf("%s: the floor is not between %d and %d", call.label, low, high)
			}
			for high-low > 1 {
				if middle := low + (high-low)/2; accepted(middle) {
					high = middle
				} else {
					low = middle
				}
			}
			source := n.stake(t, hotA, p.coldkey, netuid)
			reason := p.refuse(t, stakingAddress, call.data(low))
			if !strings.Contains(reason, "AmountTooLow") || n.stake(t, hotA, p.coldkey, netuid) != source {
				t.Fatalf("%s %d refused with %q, want AmountTooLow and no change", call.label, low, reason)
			}
			p.forward(t, stakingAddress, call.data(high))
			if source-n.stake(t, hotA, p.coldkey, netuid) != high {
				t.Fatalf("%s %d did not move exactly", call.label, high)
			}
			out, err := n.ethCall(common.Address{}, alphaAddress, alpha.pack(t, "getAlphaPrice", netuid))
			if err != nil {
				t.Fatal(err)
			}
			// getAlphaPrice is floor(price × 10^9) rao in 18-decimal EVM units,
			// so the price lies in [priceQ9, priceQ9 + 1) / 10^9.
			priceQ9 := new(big.Int).Quo(alpha.unpack(t, "getAlphaPrice", out)[0].(*big.Int), big.NewInt(tao))
			below := new(big.Int).Quo(new(big.Int).Mul(priceQ9, amount(low)), big.NewInt(tao))
			at := new(big.Int).Quo(new(big.Int).Mul(new(big.Int).Add(priceQ9, big.NewInt(1)), amount(high)), big.NewInt(tao))
			if below.Uint64() >= minimumTransferTaoRao || at.Uint64() < minimumTransferTaoRao {
				t.Fatalf("%s: floor %d alpha rao at price %s/10^9 is not 100,000 rao of TAO", call.label, high, priceQ9)
			}
			t.Logf("%s: smallest amount %d alpha rao at price %s/10^9 TAO per alpha; %d refused: %s", call.label, high, priceQ9, low, reason)
		}
	})

	// 2^64−1 and every larger amount mean the whole position; 2^64−2 is
	// refused because it exceeds the position.
	t.Run("sentinel", func(t *testing.T) {
		whole := func(label, kind string, source, destinationHot, destinationCold [32]byte, data []byte) {
			position := n.stake(t, source, p.coldkey, netuid)
			before := n.stake(t, destinationHot, destinationCold, netuid)
			p.forward(t, stakingAddress, data)
			gained := n.stake(t, destinationHot, destinationCold, netuid) - before
			if position == 0 || n.stake(t, source, p.coldkey, netuid) != 0 || gained != position && gained != position-1 {
				t.Fatalf("%s %s: position %d, left %d, destination +%d", kind, label, position, n.stake(t, source, p.coldkey, netuid), gained)
			}
			t.Logf("%s %s moved the whole position, %d rao (destination +%d)", kind, label, position, gained)
		}
		whole("2^64-1", "moveStake", hotA, hotB, p.coldkey, n.staking.pack(t, "moveStake", hotA, hotB, nu, nu, amount(wholePosition)))
		whole("2^64", "moveStake", hotB, hotA, p.coldkey, n.staking.pack(t, "moveStake", hotB, hotA, nu, nu, new(big.Int).Lsh(big.NewInt(1), 64)))
		maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		whole("2^256-1", "moveStake", hotA, hotB, p.coldkey, n.staking.pack(t, "moveStake", hotA, hotB, nu, nu, maximum))
		position := n.stake(t, hotB, p.coldkey, netuid)
		reason := p.refuse(t, stakingAddress, n.staking.pack(t, "moveStake", hotB, hotA, nu, nu, amount(wholePosition-1)))
		if !strings.Contains(reason, "NotEnoughStakeToWithdraw") || n.stake(t, hotB, p.coldkey, netuid) != position {
			t.Fatalf("moveStake 2^64-2 refused with %q, want NotEnoughStakeToWithdraw and no change", reason)
		}
		recipient := newAccount(t)
		whole("2^64-1", "transferStake", hotB, hotB, recipient, n.staking.pack(t, "transferStake", recipient, hotB, nu, nu, amount(wholePosition)))
		// Give the probe stake again for the checks below.
		n.stakeFor(t, alice, p.coldkey, hotA, netuid, 100*tao)
	})

	// A transaction that reverts after a successful precompile call rolls the
	// runtime's effect back.
	t.Run("rollback", func(t *testing.T) {
		source, destination := n.stake(t, hotA, p.coldkey, netuid), n.stake(t, hotB, p.coldkey, netuid)
		data := p.artifact.pack(t, "forwardThenRevert", stakingAddress, n.staking.pack(t, "moveStake", hotA, hotB, nu, nu, amount(tao)))
		_, err := n.ethCall(p.sender.address, p.address, data)
		var reverted rpc.DataError
		rolledBack := p.artifact.abi.Errors["RolledBack"].ID
		if !errors.As(err, &reverted) || reverted.ErrorData() != hexutil.Encode(rolledBack[:4]) {
			t.Fatalf("forwardThenRevert did not revert with RolledBack after the call: %v", err)
		}
		if receipt := n.transact(t, p.sender, &p.address, nil, data, refusedGas); receipt.Status != gethtypes.ReceiptStatusFailed {
			t.Fatal("forwardThenRevert succeeded")
		}
		if n.stake(t, hotA, p.coldkey, netuid) != source || n.stake(t, hotB, p.coldkey, netuid) != destination {
			t.Fatal("a reverted transaction kept its moveStake")
		}
	})

	// The probe registers hotkeys under its mapped coldkey, which pays the
	// burn, and getUid resolves them.
	t.Run("registration", func(t *testing.T) {
		for _, label := range []string{"registerLimit", "burnedRegister"} {
			hotkey := newAccount(t)
			economics, err := chain.ReadRegistrationEconomicsAtContext(n.ctx, n.sub, netuid, n.head(t))
			if err != nil {
				t.Fatal(err)
			}
			balance := n.freeBalance(t, p.coldkey)
			data := n.neuron.pack(t, "burnedRegister", netuid, hotkey)
			if label == "registerLimit" {
				data = n.neuron.pack(t, "registerLimit", netuid, hotkey, economics.BurnRao)
			}
			gasUse[label] = p.forward(t, neuronAddress, data)
			uid, registered := n.uid(t, netuid, hotkey)
			burn := balance - n.freeBalance(t, p.coldkey)
			if !registered || n.owner(t, hotkey) != p.coldkey || burn == 0 || burn > economics.BurnRao {
				t.Fatalf("%s: registered %v, owner %x, burn %d, burn before %d", label, registered, n.owner(t, hotkey), burn, economics.BurnRao)
			}
			out, used := p.view(t, neuronAddress, n.neuron.pack(t, "getUid", netuid, hotkey))
			values := n.neuron.unpack(t, "getUid", out)
			if !values[0].(bool) || values[1].(uint16) != uid {
				t.Fatalf("getUid %v, storage UID %d", values, uid)
			}
			gasUse["getUid"] = gas{call: used}
			t.Logf("%s: UID %d, burn %d rao", label, uid, burn)
		}
		out, _ := p.view(t, neuronAddress, n.neuron.pack(t, "getUid", netuid, newAccount(t)))
		if values := n.neuron.unpack(t, "getUid", out); values[0].(bool) || values[1].(uint16) != 0 {
			t.Fatalf("getUid of an unregistered hotkey: %v", values)
		}
	})

	views := forgeArtifact(t, "test/rehearsal/RehearsalProbe.sol", "IRehearsalStaking")

	// With a collateral lock share, a registration from the probe locks part of
	// the price on the (hotkey, probe coldkey) position. getStake includes the
	// lock and getMinerCollateral reports it; a move or transfer that digs into
	// it fails with StakeUnavailable, and exactly getStake − locked moves.
	t.Run("collateral", func(t *testing.T) {
		collateral := func(hotkey [32]byte) []any {
			out, used := p.view(t, stakingAddress, views.pack(t, "getMinerCollateral", netuid, hotkey, p.coldkey))
			gasUse["getMinerCollateral"] = gas{call: used}
			return views.unpack(t, "getMinerCollateral", out)
		}
		if state := collateral(hotA); state[0].(bool) || state[1].(uint64) != 0 {
			t.Fatalf("getMinerCollateral of a position without collateral: %v", state)
		}
		n.sudo(t, n.call(t, "AdminUtils.sudo_set_collateral_lock_share", types.NewU16(netuid), types.NewU16(1<<15)))
		hotE := newAccount(t)
		economics, err := chain.ReadRegistrationEconomicsAtContext(n.ctx, n.sub, netuid, n.head(t))
		if err != nil {
			t.Fatal(err)
		}
		balance := n.freeBalance(t, p.coldkey)
		p.forward(t, neuronAddress, n.neuron.pack(t, "registerLimit", netuid, hotE, economics.BurnRao))
		state := collateral(hotE)
		locked := state[1].(uint64)
		// The drain ratio is U64F64 bits; the subnet default is 1.
		if !state[0].(bool) || locked == 0 || state[2].(*big.Int).Cmp(new(big.Int).Lsh(big.NewInt(1), 64)) != 0 ||
			state[3].(uint64) != 0 || state[4].(uint64) != 0 || n.stake(t, hotE, p.coldkey, netuid) != locked {
			t.Fatalf("getMinerCollateral %v, getStake %d", state, n.stake(t, hotE, p.coldkey, netuid))
		}
		t.Logf("registerLimit at lock share 2^15/65535: paid %d rao, locked %d alpha rao, getStake %d", balance-n.freeBalance(t, p.coldkey), locked, locked)
		for _, method := range []string{"moveStake", "transferStake"} {
			n.stakeFor(t, alice, p.coldkey, hotE, netuid, 10*tao)
			free := n.stake(t, hotE, p.coldkey, netuid) - locked
			data := func(value *big.Int) []byte {
				if method == "moveStake" {
					return n.staking.pack(t, "moveStake", hotE, hotA, nu, nu, value)
				}
				return n.staking.pack(t, "transferStake", coldD, hotE, nu, nu, value)
			}
			for _, value := range []*big.Int{amount(free + 1), amount(wholePosition)} {
				reason := p.refuse(t, stakingAddress, data(value))
				if !strings.Contains(reason, "StakeUnavailable") || n.stake(t, hotE, p.coldkey, netuid) != free+locked {
					t.Fatalf("%s %s refused with %q, want StakeUnavailable and no change", method, value, reason)
				}
			}
			p.forward(t, stakingAddress, data(amount(free)))
			if n.stake(t, hotE, p.coldkey, netuid) != locked || collateral(hotE)[1].(uint64) != locked {
				t.Fatalf("%s of the free %d rao left %d, want the lock %d", method, free, n.stake(t, hotE, p.coldkey, netuid), locked)
			}
			t.Logf("%s: free+1 and 2^64-1 refused with StakeUnavailable; the free %d rao moved", method, free)
		}
		n.sudo(t, n.call(t, "AdminUtils.sudo_set_collateral_lock_share", types.NewU16(netuid), types.NewU16(0)))
	})

	// Every coldkey, a contract's mapped coldkey included, rejects incoming
	// conviction-locked alpha by default: a third party's transfer that would
	// carry part of its lock fails with AccountRejectsLockedAlpha, while its
	// unlocked part moves.
	t.Run("locked alpha", func(t *testing.T) {
		out, _ := p.view(t, stakingAddress, views.pack(t, "getRejectLockedAlpha", p.coldkey))
		if !views.unpack(t, "getRejectLockedAlpha", out)[0].(bool) {
			t.Fatal("the probe's mapped coldkey accepts locked alpha")
		}
		holder := newColdkey(t)
		n.fund(t, holder.PublicKey(), 200*tao)
		add, err := chain.AddStakeCall(n.sub.Meta, netuid, hotA, 100*tao)
		if err != nil {
			t.Fatal(err)
		}
		n.submit(t, holder.Ring, add)
		stake := n.stake(t, hotA, holder.PublicKey(), netuid)
		n.submit(t, holder.Ring, n.call(t, "SubtensorModule.lock_stake", accountID(t, hotA), types.NewU16(netuid), types.NewU64(stake/2)))
		transfer := func(value uint64) types.Call {
			return n.call(t, "SubtensorModule.transfer_stake", accountID(t, p.coldkey), accountID(t, hotA),
				types.NewU16(netuid), types.NewU16(netuid), types.NewU64(value))
		}
		before := n.stake(t, hotA, p.coldkey, netuid)
		if _, err := n.trySubmit(t, holder.Ring, transfer(stake)); err == nil || !strings.Contains(err.Error(), "AccountRejectsLockedAlpha") ||
			n.stake(t, hotA, p.coldkey, netuid) != before {
			t.Fatalf("transfer of %d rao with %d locked: %v", stake, stake/2, err)
		}
		n.submit(t, holder.Ring, transfer(stake-stake/2))
		if gained := n.stake(t, hotA, p.coldkey, netuid) - before; gained != stake-stake/2 && gained != stake-stake/2-1 {
			t.Fatalf("the unlocked %d rao arrived as %d", stake-stake/2, gained)
		}
		t.Logf("locked alpha: %d of %d rao locked; the whole position refused with AccountRejectsLockedAlpha, the unlocked part moved", stake/2, stake)
	})

	// The runtime converts EVM value to rao by flooring (runtime/src/lib.rs,
	// SubtensorEvmBalanceConverter): a contract sees the full msg.value, but
	// its balance gains only whole rao.
	t.Run("value", func(t *testing.T) {
		event := p.artifact.abi.Events["ValueObserved"]
		for _, value := range []uint64{1, 999_999_999, 1_000_000_000, 1_999_999_999} {
			balance, err := n.eth.BalanceAt(n.ctx, p.address, nil)
			if err != nil {
				t.Fatal(err)
			}
			sender, err := n.eth.BalanceAt(n.ctx, p.sender.address, nil)
			if err != nil {
				t.Fatal(err)
			}
			mapped := n.freeBalance(t, p.coldkey)
			receipt := n.transact(t, p.sender, &p.address, amount(value), p.artifact.pack(t, "observeValue"), 0)
			if receipt.Status != gethtypes.ReceiptStatusSuccessful || len(receipt.Logs) != 1 || receipt.Logs[0].Topics[0] != event.ID {
				t.Fatalf("observeValue with %d wei failed", value)
			}
			values, err := event.Inputs.Unpack(receipt.Logs[0].Data)
			if err != nil {
				t.Fatal(err)
			}
			seen, observed := values[0].(*big.Int), values[1].(*big.Int)
			credited := value / tao
			if seen.Uint64() != value || new(big.Int).Sub(observed, balance).Uint64() != credited*tao || n.freeBalance(t, p.coldkey)-mapped != credited {
				t.Fatalf("value %d wei: msg.value %s, balance %s from %s, mapped account +%d rao", value, seen, observed, balance, n.freeBalance(t, p.coldkey)-mapped)
			}
			after, err := n.eth.BalanceAt(n.ctx, p.sender.address, nil)
			if err != nil {
				t.Fatal(err)
			}
			fee := new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice)
			debit := new(big.Int).Sub(new(big.Int).Sub(sender, after), fee)
			t.Logf("value %d wei: msg.value %s; the contract's balance and mapped account gained %d rao; the sender paid %s wei beyond the %s wei fee", value, seen, credited, debit, fee)
		}
	})

	for _, label := range []string{"getStake", "getUid", "getMinerCollateral"} {
		t.Logf("gas %-14s in probe %7d (view)", label, gasUse[label].call)
	}
	for _, label := range []string{"moveStake", "transferStake", "registerLimit", "burnedRegister"} {
		use := gasUse[label]
		t.Logf("gas %-14s in probe %7d, transaction used %7d, estimated limit %8d", label, use.call, use.used, use.limit)
	}
}
