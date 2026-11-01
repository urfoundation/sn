//go:build runtime_rehearsal

package rehearsal

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/merkle"
)

// create2FactoryRuntime is the runtime code of the canonical deterministic
// deployment proxy, which mainnet holds at
// 0x4e59b44847b379578588920ca78fbf26c0b4956c: called with a 32-byte salt and
// creation code, it CREATE2s the code and returns the new address.
const create2FactoryRuntime = "0x7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf3"

// implementationSlot is the ERC-1967 implementation slot.
var implementationSlot = common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")

// R-8. TestR8PhaseCUpgrade: the phase C upgrade of STSettlement2
// (VERSION2-FINAL.md §16; meta review A5) and the v1 carry sweep on the real
// runtime. As on mainnet, the sweep epoch is the pool's first v2 epoch:
// epoch 1 closes on v1, capturing reveal-lag stake, and on v2, whose root is
// committed and approved before the upgrade. The phase C implementation is
// created through the deterministic deployment proxy's code, and the owner
// upgrades the proxy with upgradeToAndCall(implementation, 0x). Every slot and
// approval survives; the single-leaf v1 root pays vault2's coldkey, which
// absorbStake turns into surplus; the owner records the sweep and assigns the
// surplus to epoch 2, whose close snapshots it; the pending epoch 1 root
// finalizes and pays through the new implementation.
func TestR8PhaseCUpgrade(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	s := deployStack(t, n, stackConfig{operators: 1})
	operator := s.operators[1]
	// Epochs stop (tempo 0), so no dividend reaches a position between reads.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(s.netuid), types.NewU16(0)))
	relayer := newEVMKey(t)
	n.fundEVM(t, relayer.address, 1_000*tao)
	phaseC := forgeArtifact(t, "src/STSettlement2PhaseC.sol", "STSettlement2PhaseC")
	v1VaultColdkey := mirror(s.v1Vault)
	const sweep = 1
	artifactHash := sha256.Sum256([]byte("urnetwork rehearsal v1 sweep record"))
	sweepRoot := [32]byte(merkle.PayoutLeaf(s.vaultColdkey, big.NewInt(10_000)))
	if got := n.read(t, s.v1Vault, s.v1VaultArtifact, "payoutLeaf", s.vaultColdkey, id(10_000))[0].([32]byte); got != sweepRoot {
		t.Fatalf("vault1 payoutLeaf %x, merkle.PayoutLeaf %x", got, sweepRoot)
	}

	// Epoch 1 on both versions. Reveal-lag stake reaches the v1 pool hotkey and
	// emission the v2 one. v1 closes first: its grace is one block.
	n.stakeFor(t, alice, v1VaultColdkey, operator.v1Pool, s.netuid, 40*tao)
	n.stakeFor(t, alice, s.vaultColdkey, operator.pool, s.netuid, 200*tao)
	s.inWindow(t, sweep)
	n.send(t, s.owner, s.v1, s.coordinatorArtifact, nil, "closeOperatorEpoch", id(sweep), id(1))
	lag := s.v1Entitlement(t, sweep).Funded
	if lag.Sign() == 0 || n.stake(t, operator.v1Pool, v1VaultColdkey, s.netuid) != 0 {
		t.Fatalf("the v1 close captured %s", lag)
	}
	s.close(t, sweep, 1)
	funded := s.vaultUint(t, "fundedThrough", id(1), id(sweep)).Uint64()
	coldkeys := [][32]byte{newAccount(t), newAccount(t), newAccount(t)}
	first := newLedger(t, split(coldkeys, funded), 4)
	commit1 := first.commitment("ledger header epoch 1")
	n.send(t, operator.rootSigner, s.settlement, s.settlementArtifact, nil, "commitDistribution", id(sweep), id(1), id(0), commit1)
	n.send(t, s.auditor, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(1), commit1.Root, commit1.LedgerHash)
	// The sweep root: one leaf, vault2's coldkey at 10,000 bps.
	n.send(t, operator.rootSigner, s.v1, s.coordinatorArtifact, nil, "commitOperatorRoot", id(sweep), id(1), sweepRoot, artifactHash)

	var implementation common.Address
	t.Run("upgrade", func(t *testing.T) {
		// The deterministic deployment proxy's exact code, at an address of this chain.
		runtime := hexutil.MustDecode(create2FactoryRuntime)
		initcode := append([]byte{0x60, byte(len(runtime)), 0x80, 0x60, 0x0b, 0x60, 0x00, 0x39, 0x60, 0x00, 0xf3}, runtime...)
		factory := n.deploy(t, s.deployer, initcode)
		if code, err := n.eth.CodeAt(n.ctx, factory, nil); err != nil || !bytes.Equal(code, runtime) {
			t.Fatalf("factory code %x (%v)", code, err)
		}
		salt := [32]byte{}
		implementation = crypto.CreateAddress2(factory, salt, crypto.Keccak256(phaseC.creation))
		deployed := n.transact(t, relayer, &factory, nil, append(salt[:], phaseC.creation...), 0)
		if deployed.Status != gethtypes.ReceiptStatusSuccessful {
			t.Fatalf("CREATE2 of the implementation failed in block %s", deployed.BlockNumber)
		}
		code, err := n.eth.CodeAt(n.ctx, implementation, nil)
		if err != nil || len(code) == 0 {
			t.Fatalf("no code at the predicted implementation %s (%v)", implementation, err)
		}
		// The implementation's own initializers are disabled.
		if got := n.refused(t, relayer, implementation, phaseC, nil, "initialize", relayer.address, s.v1, s.vault, []common.Address{relayer.address}, big.NewInt(1)); got != "InvalidInitialization" {
			t.Fatalf("initialize on the implementation refused with %s", got)
		}

		before := s.settlementSlots(t)
		if got := n.refused(t, relayer, s.settlement, phaseC, nil, "upgradeToAndCall", implementation, []byte{}); got != "OwnableUnauthorizedAccount" {
			t.Fatalf("an upgrade from another account refused with %s", got)
		}
		upgradeData := phaseC.pack(t, "upgradeToAndCall", implementation, []byte{})
		receipt := n.send(t, s.owner, s.settlement, phaseC, nil, "upgradeToAndCall", implementation, []byte{})
		requireEvent(t, receipt, phaseC, s.settlement, "Upgraded", map[string]any{"implementation": implementation})
		after := s.settlementSlots(t)
		if common.BytesToAddress(after[implementationSlot].Bytes()) != implementation {
			t.Fatalf("implementation slot holds %s", after[implementationSlot])
		}
		for slot, value := range before {
			if slot != implementationSlot && after[slot] != value {
				t.Fatalf("slot %s changed from %s to %s", slot, value, after[slot])
			}
		}
		if n.read(t, s.settlement, phaseC, "owner")[0].(common.Address) != s.owner.address ||
			n.read(t, s.settlement, phaseC, "approvalCount", id(1))[0].(*big.Int).Uint64() != 1 ||
			!n.read(t, s.settlement, phaseC, "hasApproved", id(1), s.auditor.address)[0].(bool) ||
			n.read(t, s.settlement, phaseC, "vault")[0].(common.Address) != s.vault {
			t.Fatal("state read through the new implementation differs")
		}
		t.Logf("CREATE2 of the implementation: %d bytes, gas %d; upgradeToAndCall: %d calldata bytes, gas %d",
			len(code), deployed.GasUsed, len(upgradeData), receipt.GasUsed)
	})

	// The v1 sweep finalizes, and its claim pays vault2's coldkey on the v1
	// escrow hotkey; absorbStake turns that into vault2 surplus.
	n.sealUntil(t, s.end(t, sweep)+2*window)
	n.send(t, relayer, s.v1, s.coordinatorArtifact, nil, "finalizeOperatorEpoch", id(sweep), id(1))
	total := s.v1Entitlement(t, sweep).Total
	if total.Cmp(lag) != 0 {
		t.Fatalf("the sweep entitlement holds %s, captured %s", total, lag)
	}
	n.send(t, relayer, s.v1Vault, s.v1VaultArtifact, nil, "claim", id(sweep), id(1), s.vaultColdkey, id(10_000), [][32]byte{})
	paid := n.stake(t, s.v1Escrow, s.vaultColdkey, s.netuid)
	if paid != total.Uint64() && paid != total.Uint64()-1 {
		t.Fatalf("the sweep claim paid vault2's coldkey %d of %s", paid, total)
	}
	n.send(t, relayer, s.vault, s.vaultArtifact, nil, "absorbStake", s.v1Escrow)
	surplus := new(big.Int).Sub(s.vaultUint(t, "liveEscrowStake"), s.vaultUint(t, "escrowAccounted"))
	if surplus.Sign() <= 0 || surplus.Uint64() > paid {
		t.Fatalf("absorbStake of %d rao left surplus %s", paid, surplus)
	}

	t.Run("recordV1Sweep", func(t *testing.T) {
		if got := n.refused(t, relayer, s.settlement, phaseC, nil, "recordV1Sweep", id(1), id(sweep), artifactHash); got != "OwnableUnauthorizedAccount" {
			t.Fatalf("a record from another account refused with %s", got)
		}
		if got := n.refused(t, s.owner, s.settlement, phaseC, nil, "recordV1Sweep", id(1), id(sweep), sha256.Sum256([]byte("another record"))); got != "NotV1SweepRoot" {
			t.Fatalf("a record of another artifact refused with %s", got)
		}
		if got := n.refused(t, s.owner, s.settlement, phaseC, nil, "recordV1Sweep", id(1), id(sweep+1), artifactHash); got != "NotV1SweepRoot" {
			t.Fatalf("a record of an uncommitted epoch refused with %s", got)
		}
		receipt := n.send(t, s.owner, s.settlement, phaseC, nil, "recordV1Sweep", id(1), id(sweep), artifactHash)
		requireEvent(t, receipt, phaseC, s.settlement, "V1SweepRecorded", map[string]any{
			"noId": id(1), "epoch": id(sweep), "payoutRoot": sweepRoot, "artifactHash": artifactHash,
		})
		recorded := n.read(t, s.settlement, phaseC, "v1Sweep", id(1))
		if recorded[0].(uint64) != sweep || recorded[1].([32]byte) != sweepRoot || recorded[2].([32]byte) != artifactHash {
			t.Fatalf("v1Sweep(1) reads %v", recorded)
		}
		if got := n.refused(t, s.owner, s.settlement, phaseC, nil, "recordV1Sweep", id(1), id(sweep), artifactHash); got != "V1SweepAlreadyRecorded" {
			t.Fatalf("a second record refused with %s", got)
		}
		t.Logf("recordV1Sweep: gas %d", receipt.GasUsed)
	})

	t.Run("assignSurplus", func(t *testing.T) {
		next := s.vaultUint(t, "lastFundedEpoch", id(1)).Uint64() + 1
		if next != sweep+1 {
			t.Fatalf("the next unfunded epoch is %d", next)
		}
		if got := n.refused(t, relayer, s.settlement, phaseC, nil, "assignSurplus", id(1), id(next), surplus); got != "OwnableUnauthorizedAccount" {
			t.Fatalf("an assignment from another account refused with %s", got)
		}
		if got := n.refused(t, s.owner, s.settlement, phaseC, nil, "assignSurplus", id(1), id(next+1), surplus); got != "InvalidEpoch" {
			t.Fatalf("an assignment to a later epoch refused with %s", got)
		}
		if got := n.refused(t, s.owner, s.settlement, phaseC, nil, "assignSurplus", id(2), id(next), surplus); got != "UnknownPool" {
			t.Fatalf("an assignment to an unregistered pool refused with %s", got)
		}
		if got := n.refused(t, s.owner, s.settlement, phaseC, nil, "assignSurplus", id(1), id(next), new(big.Int).Add(surplus, big.NewInt(1))); got != "InvalidSurplus" {
			t.Fatalf("an assignment above the surplus refused with %s", got)
		}
		captured := s.vaultUint(t, "capturedTotal", id(1))
		receipt := n.send(t, s.owner, s.settlement, phaseC, nil, "assignSurplus", id(1), id(next), surplus)
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "SurplusAssigned", map[string]any{"noId": id(1), "epoch": id(next), "amount": surplus})
		if got := n.refused(t, s.owner, s.settlement, phaseC, nil, "assignSurplus", id(1), id(next), surplus); got != "InvalidSurplus" {
			t.Fatalf("a second assignment refused with %s", got)
		}
		if s.vaultUint(t, "capturedTotal", id(1)).Cmp(new(big.Int).Add(captured, surplus)) != 0 || s.vaultUint(t, "fundedThrough", id(1), id(sweep)).Uint64() != funded {
			t.Fatal("the assignment changed another snapshot or missed the pool's funding")
		}
		s.requireConservation(t)
		t.Logf("assignSurplus: %s rao to epoch %d, gas %d", surplus, next, receipt.GasUsed)
	})

	// Phase A through the new implementation: the approved epoch 1 root
	// finalizes after its window and pays, and epoch 2's close snapshots the
	// surplus with its own capture.
	t.Run("phase A calls", func(t *testing.T) {
		n.sealUntil(t, s.pending(t, 1).CommittedBlock+challengeBlocks)
		n.send(t, relayer, s.settlement, phaseC, nil, "finalizeDistribution", id(1))
		a := first.account(0)
		n.send(t, relayer, s.vault, s.vaultArtifact, nil, "claim", id(1), id(0), a, first.proof(0))
		if got := n.stake(t, s.escrow, a.Coldkey, s.netuid); got != a.Cumulative.Uint64() && got != a.Cumulative.Uint64()-1 {
			t.Fatalf("the claim paid %d of %s", got, a.Cumulative)
		}
		n.stakeFor(t, alice, s.vaultColdkey, operator.pool, s.netuid, 30*tao)
		captured := s.vaultUint(t, "capturedTotal", id(1))
		escrow := n.stake(t, s.escrow, s.vaultColdkey, s.netuid)
		s.close(t, sweep+1, 1)
		gained := n.stake(t, s.escrow, s.vaultColdkey, s.netuid) - escrow
		if got := s.vaultUint(t, "fundedThrough", id(1), id(sweep+1)); got.Cmp(new(big.Int).Add(captured, id(gained))) != 0 || got.Uint64() <= funded+surplus.Uint64() {
			t.Fatalf("epoch 2's snapshot %s, captured before %s, gained %d", got, captured, gained)
		}
		s.requireConservation(t)
		if !n.read(t, s.v1Vault, s.v1VaultArtifact, "conservationHolds")[0].(bool) {
			t.Fatal("vault1 conservation fails")
		}
	})
}

// v1EntitlementRecord is STSettlementVault.Entitlement.
type v1EntitlementRecord struct {
	PayoutRoot   [32]byte
	ArtifactHash [32]byte
	Funded       *big.Int
	Total        *big.Int
	Claimed      *big.Int
	ExpiryBlock  uint64
	Status       uint8
}

func (self *stack) v1Entitlement(t *testing.T, epoch uint64) v1EntitlementRecord {
	t.Helper()
	out := self.n.read(t, self.v1Vault, self.v1VaultArtifact, "entitlement", id(epoch), id(1))
	return *abi.ConvertType(out[0], new(v1EntitlementRecord)).(*v1EntitlementRecord)
}

// settlementSlots reads the STSettlement2 proxy's slots 0 to 50 (its layout,
// gap included, and the phase C mapping's slot) and the ERC-1967
// implementation slot.
func (self *stack) settlementSlots(t *testing.T) map[common.Hash]common.Hash {
	t.Helper()
	slots := map[common.Hash]common.Hash{}
	read := func(slot common.Hash) {
		value, err := self.n.eth.StorageAt(self.n.ctx, self.settlement, slot, nil)
		if err != nil {
			t.Fatal(err)
		}
		slots[slot] = common.BytesToHash(value)
	}
	for i := int64(0); i <= 50; i++ {
		read(common.BigToHash(big.NewInt(i)))
	}
	read(implementationSlot)
	return slots
}
