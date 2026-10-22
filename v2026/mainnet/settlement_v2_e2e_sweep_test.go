package main

import (
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/merkle"
)

func (f *e2eFixture) vault1Log(name string, args ...any) e2eLog {
	return e2eLog{address: f.vault1, abi: &e2eCatalogArtifact(f.t, "SettlementVault").abi, name: name, args: args}
}

// emitV1 stakes emission on the v1 pool hotkey, owned by vault1's coldkey.
func (f *e2eFixture) emitV1(amount uint64) {
	f.chain.emit(f.poolV1, f.vault1Coldkey, e2eRao(amount))
}

// closeV1 closes v1 epoch at its end on the v1 coordinator, as anyone may,
// and checks the capture of the whole pool position.
func (f *e2eFixture) closeV1(epoch uint64) *big.Int {
	f.t.Helper()
	f.chain.rollTo(f.end(epoch))
	observed := f.chain.stake(f.poolV1, f.vault1Coldkey)
	receipt := f.must(f.anyone, f.coordinator, nil, e2eCoordinatorBinding.PackCloseOperatorEpoch(e2eRao(epoch), e2eRao(e2eNoID)))
	f.requireLogs(receipt, f.vault1Log("EmissionCaptured", e2eRao(epoch), e2eRao(e2eNoID), f.poolV1, observed))
	return observed
}

// commitV1 commits a v1 payout root for epoch as the operator's root signer.
func (f *e2eFixture) commitV1(epoch uint64, root, artifact [32]byte) {
	f.t.Helper()
	receipt := f.must(f.operator, f.coordinator, nil, e2eCoordinatorBinding.PackCommitOperatorRoot(e2eRao(epoch), e2eRao(e2eNoID), root, artifact))
	f.requireLogs(receipt, f.coordinatorLog("OperatorRootCommitted", e2eRao(epoch), e2eRao(e2eNoID), root, artifact, f.operator.address))
}

// finalizeV1 finalizes v1 epoch once its finalize offset has passed.
func (f *e2eFixture) finalizeV1(epoch uint64) *types.Receipt {
	f.t.Helper()
	if at := f.end(epoch) + e2eWindow; f.chain.next() < at {
		f.chain.rollTo(at)
	}
	return f.must(f.anyone, f.coordinator, nil, e2eCoordinatorBinding.PackFinalizeOperatorEpoch(e2eRao(epoch), e2eRao(e2eNoID)))
}

func (f *e2eFixture) entitlement(epoch uint64) (total, claimed *big.Int, expiry uint64) {
	value := e2eView(f, f.vault1, e2eVault1Binding.PackEntitlement(e2eRao(epoch), e2eRao(e2eNoID)), e2eVault1Binding.UnpackEntitlement)
	return value.Total, value.Claimed, value.ExpiryBlock
}

func (f *e2eFixture) vault1Counter(pack func() []byte) *big.Int {
	return e2eView(f, f.vault1, pack(), func(out []byte) (*big.Int, error) { return new(big.Int).SetBytes(out), nil })
}

// E-7: the phase C sweep path on phase-A bytecode (§16, §12.2). v1 epoch 1's
// root is finalized and partly claimed, and v1 epoch 2 misses its root; v2
// takes over from epoch 3 while reveal-lag emission still reaches the v1
// pool hotkey. Once epoch 1's root expires, its remainder joins the carry;
// the sweep epoch's v1 close captures the reveal-lag stake, and its
// single-leaf root pays everything to vault2's coldkey. absorbStake moves that
// stake from the v1 escrow hotkey into vault2's escrow as surplus, and a test
// upgrade of STSettlement2 assigns it to the pool, whose next funding
// snapshot holds it. Conservation holds in each vault and across both.
func TestSettlementV2E2ESweepPath(t *testing.T) {
	const first = 3
	f := newE2EFixture(t, first)
	carol, dave, alice := e2eKey("carol"), e2eKey("dave"), e2eKey("alice")
	var injected int64

	// v1 epoch 1: Carol 60%, Dave 40%; Carol claims, Dave never does.
	f.emitV1(2_000_000_000_000)
	injected += 2_000_000_000_000
	funded1 := f.closeV1(1)
	leaves := []merkle.Leaf{merkle.PayoutLeaf(carol, big.NewInt(6_000)), merkle.PayoutLeaf(dave, big.NewInt(4_000))}
	tree, err := merkle.NewTree(leaves)
	if err != nil {
		t.Fatal(err)
	}
	f.commitV1(1, tree.Root(), sha256.Sum256([]byte("v1 epoch 1 artifact")))
	receipt := f.finalizeV1(1)
	total1, _, expiry1 := f.entitlement(1)
	if total1.Cmp(funded1) != 0 || expiry1 != f.end(10)-1 {
		t.Fatalf("v1 epoch 1 entitlement %s expires at %d", total1, expiry1)
	}
	f.requireLogs(receipt, f.vault1Log("EntitlementFinalized", e2eRao(1), e2eRao(e2eNoID), tree.Root(), sha256.Sum256([]byte("v1 epoch 1 artifact")), total1, expiry1),
		f.coordinatorLog("OperatorEpochFinalized", e2eRao(1), e2eRao(e2eNoID), true))
	proof, err := tree.Proof(leaves[0])
	if err != nil {
		t.Fatal(err)
	}
	carolShare := new(big.Int).Div(new(big.Int).Mul(total1, big.NewInt(6_000)), big.NewInt(10_000))
	receipt = f.must(f.anyone, f.vault1, nil, e2eVault1Binding.PackClaim(e2eRao(1), e2eRao(e2eNoID), carol, big.NewInt(6_000), proof))
	f.requireLogs(receipt, f.vault1Log("Claimed", e2eRao(1), e2eRao(e2eNoID), carol, big.NewInt(6_000), carolShare, f.anyone.address), f.vault1Log("ClaimPaid", carol, carolShare, f.anyone.address))

	// v1 epoch 2, the last before E_s, misses its root: its funding carries.
	f.emitV1(1_000_000_000_000)
	injected += 1_000_000_000_000
	funded2 := f.closeV1(2)
	receipt = f.finalizeV1(2)
	f.requireLogs(receipt, f.vault1Log("RootMissed", e2eRao(2), e2eRao(e2eNoID), funded2), f.coordinatorLog("OperatorEpochFinalized", e2eRao(2), e2eRao(e2eNoID), false))

	// v2 from E_s; reveal-lag emission still reaches the v1 pool hotkey.
	root0 := f.epoch(first, 1_450_000_000_000, e2eRows(first))
	injected += 1_450_000_000_000
	f.emitV1(333_000_000_000)
	injected += 333_000_000_000
	a := e2eAccountOf(t, root0.Accounts, alice)
	account, v2proof := e2eProof(t, root0.Tree, root0.Accounts, a)
	f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackClaim(e2eRao(e2eNoID), e2eRao(0), account, v2proof))

	// v1 roots expire: epoch 1's unclaimed remainder joins the carry.
	f.chain.rollTo(expiry1 + 1)
	remainder := new(big.Int).Sub(total1, carolShare)
	receipt = f.must(f.anyone, f.vault1, nil, e2eVault1Binding.PackExpireEntitlement(e2eRao(1), e2eRao(e2eNoID)))
	carry := new(big.Int).Add(remainder, funded2)
	f.requireLogs(receipt, f.vault1Log("EntitlementExpired", e2eRao(1), e2eRao(e2eNoID), remainder, carry))

	// The sweep, E_sweep = E_s + 8: the v1 close captures the reveal-lag
	// stake, and the single-leaf root pays the funding and the carry to vault2.
	const sweep = first + 8
	lag := f.closeV1(sweep)
	if lag.Cmp(big.NewInt(333_000_000_000)) != 0 {
		t.Fatalf("the sweep close captured %s", lag)
	}
	sweepRoot := [32]byte(merkle.PayoutLeaf(f.vault2Coldkey, big.NewInt(10_000)))
	record := sha256.Sum256([]byte("v1 sweep record"))
	f.commitV1(sweep, sweepRoot, record)
	receipt = f.finalizeV1(sweep)
	swept := new(big.Int).Add(lag, carry)
	totalSweep, _, expirySweep := f.entitlement(sweep)
	f.requireLogs(receipt, f.vault1Log("EntitlementFinalized", e2eRao(sweep), e2eRao(e2eNoID), sweepRoot, record, swept, expirySweep),
		f.coordinatorLog("OperatorEpochFinalized", e2eRao(sweep), e2eRao(e2eNoID), true))
	if totalSweep.Cmp(swept) != 0 {
		t.Fatalf("the sweep root holds %s, want %s", totalSweep, swept)
	}
	receipt = f.must(f.anyone, f.vault1, nil, e2eVault1Binding.PackClaim(e2eRao(sweep), e2eRao(e2eNoID), f.vault2Coldkey, big.NewInt(10_000), nil))
	f.requireLogs(receipt, f.vault1Log("Claimed", e2eRao(sweep), e2eRao(e2eNoID), f.vault2Coldkey, big.NewInt(10_000), swept, f.anyone.address), f.vault1Log("ClaimPaid", f.vault2Coldkey, swept, f.anyone.address))
	if f.chain.stake(f.escrowV1, f.vault2Coldkey).Cmp(swept) != 0 {
		t.Fatal("the sweep did not pay vault2's coldkey on the v1 escrow hotkey")
	}

	// absorbStake moves it into vault2's escrow, accounted to nothing.
	accounted := f.vault2Counter(e2eVault2Binding.PackEscrowAccounted)
	receipt = f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackAbsorbStake(f.escrowV1))
	f.requireLogs(receipt, f.vault2Log("StakeAbsorbed", f.escrowV1, swept, swept, f.anyone.address))
	surplus := new(big.Int).Sub(e2eView(f, f.vault2, e2eVault2Binding.PackLiveEscrowStake(), e2eVault2Binding.UnpackLiveEscrowStake), f.vault2Counter(e2eVault2Binding.PackEscrowAccounted))
	if surplus.Cmp(swept) != 0 || f.vault2Counter(e2eVault2Binding.PackEscrowAccounted).Cmp(accounted) != 0 || f.chain.stake(f.escrowV1, f.vault2Coldkey).Sign() != 0 {
		t.Fatalf("absorbed surplus %s, want %s", surplus, swept)
	}

	// The test coordinator upgrade, the I suite's STSettlement2Surplus with
	// the phase C wrapper, assigns it: only through the owner, and never more
	// than the surplus.
	wrapper := e2eForgeArtifact(t, "STSettlement2Surplus")
	upgrade := f.deploy(f.deployer, wrapper, nil)
	upgradeABI := wrapper.abi
	pack := func(method string, args ...any) []byte {
		data, err := upgradeABI.Pack(method, args...)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	receipt = f.must(f.owner, f.settlement, nil, pack("upgradeToAndCall", upgrade, []byte{}))
	f.requireLogs(receipt, f.settlementLog("Upgraded", upgrade))
	f.refused(f.anyone, f.settlement, nil, pack("assignSurplus", e2eRao(e2eNoID), swept), "OwnableUnauthorizedAccount")
	f.refused(f.owner, f.settlement, nil, pack("assignSurplus", e2eRao(e2eNoID), new(big.Int).Add(swept, common.Big1)), "InvalidSurplus")
	next := e2eView(f, f.vault2, e2eVault2Binding.PackLastFundedEpoch(e2eRao(e2eNoID)), e2eVault2Binding.UnpackLastFundedEpoch).Uint64() + 1
	receipt = f.must(f.owner, f.settlement, nil, pack("assignSurplus", e2eRao(e2eNoID), swept))
	f.requireLogs(receipt, f.vault2Log("SurplusAssigned", e2eRao(e2eNoID), e2eRao(next), swept))
	f.requireConservation()

	// The pool's next close defers the epochs between and snapshots the
	// surplus with its own capture.
	f.emitV2(1_000_000_000_000)
	injected += 1_000_000_000_000
	captured := e2eView(f, f.vault2, e2eVault2Binding.PackCapturedTotal(e2eRao(e2eNoID)), e2eVault2Binding.UnpackCapturedTotal)
	closing := uint64(sweep + 1)
	f.chain.rollTo(f.end(closing))
	receipt = f.must(f.anyone, f.settlement, nil, e2eSettlementBinding.PackCloseOperatorEpoch(e2eRao(closing), e2eRao(e2eNoID)))
	var expected []e2eLog
	for epoch := next; epoch < closing; epoch++ {
		expected = append(expected, f.vault2Log("EmissionDeferred", e2eRao(epoch), e2eRao(e2eNoID), captured))
	}
	funded := new(big.Int).Add(captured, big.NewInt(1_000_000_000_000))
	f.requireLogs(receipt, append(expected, f.vault2Log("EmissionCaptured", e2eRao(closing), e2eRao(e2eNoID), big.NewInt(1_000_000_000_000), big.NewInt(1_000_000_000_000), funded))...)

	// Conservation, in each vault and across both: every rao of emission is
	// paid to a provider coldkey or backed by a vault's accounted escrow.
	if !e2eView(f, f.vault1, e2eVault1Binding.PackConservationHolds(), e2eVault1Binding.UnpackConservationHolds) {
		t.Fatal("vault1 conservation does not hold")
	}
	f.requireConservation()
	paid1, accounted1 := f.vault1Counter(e2eVault1Binding.PackTotalPaid), f.vault1Counter(e2eVault1Binding.PackEscrowAccounted)
	paid2, accounted2 := f.vault2Counter(e2eVault2Binding.PackTotalPaid), f.vault2Counter(e2eVault2Binding.PackEscrowAccounted)
	providers := new(big.Int).Add(new(big.Int).Sub(paid1, swept), paid2)
	held := new(big.Int).Add(new(big.Int).Add(providers, accounted1), accounted2)
	if held.Cmp(big.NewInt(injected)) != 0 || f.chain.stake(f.escrowV1, f.vault1Coldkey).Cmp(accounted1) != 0 || f.chain.stake(f.escrowV2, f.vault2Coldkey).Cmp(accounted2) != 0 ||
		f.chain.stake(f.escrowV1, carol).Cmp(carolShare) != 0 || f.chain.stake(f.escrowV2, alice).Cmp(account.Cumulative) != 0 || paid2.Cmp(account.Cumulative) != 0 {
		t.Fatalf("across both vaults: injected %d, providers %s, accounted %s and %s", injected, providers, accounted1, accounted2)
	}
}
