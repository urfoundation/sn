package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
)

func (f *e2eFixture) coordinatorLog(name string, args ...any) e2eLog {
	return e2eLog{address: f.coordinator, abi: &e2eCatalogArtifact(f.t, "Coordinator").abi, name: name, args: args}
}

// E-6: pause stops settlement, and only settlement (§4.3 S5). While the
// guardian has the v1 coordinator paused, STSettlement2 refuses close,
// commit and finalize with SettlementPaused, while approvals, claims,
// demands, the operator's answers and the watcher's challenge all go
// through. Only the owner unpauses, and settlement resumes where it
// stopped.
func TestSettlementV2E2EPauseStopsSettlementOnly(t *testing.T) {
	f := newE2EFixture(t, e2eDefaultFirst)
	alice, bob := e2eKey("alice"), e2eKey("bob")
	root0 := f.epoch(f.firstEpoch, 1_450_000_000_000, e2eRows(f.firstEpoch))
	a, b := e2eAccountOf(t, root0.Accounts, alice), e2eAccountOf(t, root0.Accounts, bob)
	e1, e2 := f.firstEpoch+1, f.firstEpoch+2
	f.rows[e1], f.rows[e2] = e2eRows(e1), e2eRows(e2)
	f.emitV2(1_460_000_000_000)
	f.closeV2(e1)
	p1, err := f.build(e1)
	if err != nil {
		t.Fatal(err)
	}
	f.publisher.publish(p1.Objects, p1.Accounts, p1.Tree)
	seq := f.commit(f.operator, e1, p1.Commit)

	receipt := f.must(f.guardian, f.coordinator, nil, e2eCoordinatorBinding.PackSetPaused(true))
	f.requireLogs(receipt, f.coordinatorLog("PausedSet", true, f.guardian.address))

	// Approvals continue.
	f.approve()
	// Claims continue: anyone claims Alice's root-0 balance to her coldkey.
	account, proof := e2eProof(t, root0.Tree, root0.Accounts, a)
	receipt = f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackClaim(e2eRao(e2eNoID), e2eRao(0), account, proof))
	f.requireLogs(receipt, f.vault2Log("Claimed", e2eRao(e2eNoID), e2eRao(0), e2eRao(a), alice, account.Cumulative, account.Cumulative, f.anyone.address))
	if f.chain.stake(f.escrowV2, alice).Cmp(account.Cumulative) != 0 {
		t.Fatal("the claim during the pause did not pay Alice")
	}
	// Demands continue, and so does the operator's answer.
	account, proof = e2eProof(t, root0.Tree, root0.Accounts, b)
	receipt = f.must(f.watcher, f.vault2, e2eRao(e2eDemandFeeWei), e2eVault2Binding.PackDemandPath(e2eRao(e2eNoID), e2eRao(0), account, proof))
	f.requireLogs(receipt, f.vault2Log("PathDemanded", e2eRao(e2eNoID), seq, e2eRao(0), e2eRao(b), f.watcher.address))
	receipt = f.must(f.operator, f.vault2, nil, e2eVault2Binding.PackCheckPath(e2eRao(e2eNoID), e2eRao(0), account, proof, e2ePath(e2eOpening(t, p1.Tree, b))))
	f.requireLogs(receipt, f.vault2Log("PathAnswered", e2eRao(e2eNoID), seq, e2eRao(0), e2eRao(b), f.operator.address))

	// Settlement stops: a commit inside the window, a finalize once the
	// window has passed, and the next epoch's close.
	f.refused(f.operator, f.settlement, nil, f.commitData(e1, p1.Commit), "SettlementPaused")
	f.chain.rollTo(f.pending().CommittedBlock + e2eWindow)
	f.refused(f.anyone, f.settlement, nil, e2eSettlementBinding.PackFinalizeDistribution(e2eRao(e2eNoID)), "SettlementPaused")
	f.emitV2(1_470_000_000_000)
	f.chain.rollTo(f.end(e2))
	f.refused(f.anyone, f.settlement, nil, e2eSettlementBinding.PackCloseOperatorEpoch(e2eRao(e2), e2eRao(e2eNoID)), "SettlementPaused")
	if f.pending().Seq != seq || f.rootCount() != 1 || f.fundedThrough(e2).Sign() != 0 {
		t.Fatal("settlement moved while paused")
	}

	// Only the owner unpauses; settlement resumes where it stopped.
	f.refused(f.guardian, f.coordinator, nil, e2eCoordinatorBinding.PackSetPaused(false), "Unauthorized")
	receipt = f.must(f.owner, f.coordinator, nil, e2eCoordinatorBinding.PackSetPaused(false))
	f.requireLogs(receipt, f.coordinatorLog("PausedSet", false, f.owner.address))
	f.finalize()
	f.closeV2(e2)
	honest, err := f.build(e2)
	if err != nil {
		t.Fatal(err)
	}
	// A faulty successor: Alice's account falls one rao below its root-1
	// value, and Bob takes the difference.
	accounts := slices.Clone(honest.Accounts)
	lowered := new(uint256.Int).SubUint64(&p1.Accounts[a].Cumulative, 1)
	accounts[b].Cumulative.Add(&accounts[b].Cumulative, new(uint256.Int).Sub(&accounts[a].Cumulative, lowered))
	accounts[a].Cumulative = *lowered
	commitment, objects, tree := f.forge(honest, accounts)
	f.publisher.publish(objects, accounts, tree)
	seq = f.commit(f.operator, e2, commitment)

	// Paused again, the watcher's challenge still deletes it.
	f.must(f.guardian, f.coordinator, nil, e2eCoordinatorBinding.PackSetPaused(true))
	output, stderr, code := f.snclaim(f.snclaimArgs("v2", "check", "--all", "--no_id=1", "--challenge", "--key_file="+f.watcher.keyFile(t))...)
	want := fmt.Sprintf("DistributionDeleted: pool 1, seq %d\n  account:     %d of finalized root 1\n  fault:       lower_value at level 0\n  challenger:  %s\n", seq, a, f.watcher.address.Hex())
	if code != 0 || !strings.HasSuffix(output, want) {
		t.Fatalf("challenge while paused (exit %d):\n%s%s", code, output, stderr)
	}
	sent := f.chain.sentBy(f.watcher.address)
	f.requireLogs(f.chain.receipt(sent[len(sent)-1].Hash()), f.vault2Log("DistributionDeleted", e2eRao(e2eNoID), seq, e2eRao(1), e2eRao(a), uint8(merkle.LedgerFaultLowerValue), e2eRao(0), f.watcher.address))
	if f.pending().Seq != 0 || f.rootCount() != 2 {
		t.Fatal("the challenged root was not deleted")
	}
	f.requireConservation()
}
