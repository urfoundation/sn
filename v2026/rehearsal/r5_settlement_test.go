//go:build runtime_rehearsal

package rehearsal

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/merkle"
)

// deepLeafCount is the most accounts a vault2 root can hold, 2^29: a tree of
// depth 29, the longest proof and path vault2 verifies.
const deepLeafCount = 1 << 29

// R-5, R-6. TestR5Settlement runs settlement cycles through STSettlement2 at
// the shortest cadence it accepts, on ledgers of 2^29 accounts: close,
// commit, approval from an auditor address, finalize and claims; then a
// second root with a demand, paid with demandFeeWei, and its answer. It
// sends each measured transaction with the least gas limit that succeeds and
// records that limit, the gas used and the limit eth_estimateGas gives,
// against the operator gas policy's maximum_gas (VERSION2-FINAL.md §15.8).
func TestR5Settlement(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	s := deployStack(t, n, stackConfig{operators: 1, shortFrom: 3})
	operator := s.operators[1]
	// Epochs stop (tempo 0), so no dividend reaches a position between reads.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(s.netuid), types.NewU16(0)))
	relayer := newEVMKey(t)
	n.fundEVM(t, relayer.address, 1_000*tao)
	maximumGas := operatorMaximumGas(t)

	type measure struct {
		used, least, estimate uint64
		operator              bool
	}
	gas := map[string]measure{}
	measured := func(t *testing.T, label string, operator bool, from evmKey, to common.Address, contract artifact, value *big.Int, method string, args ...any) *gethtypes.Receipt {
		receipt, estimate, least := n.sendLeast(t, from, to, contract, value, method, args...)
		gas[label] = measure{used: receipt.GasUsed, least: least, estimate: estimate, operator: operator}
		return receipt
	}
	closed := func(t *testing.T, label string, epoch uint64) {
		s.inWindow(t, epoch)
		measured(t, label, true, s.owner, s.settlement, s.settlementArtifact, nil, "closeOperatorEpoch", id(epoch), id(1))
	}
	claim := func(t *testing.T, l *ledger, rootIndex, index uint64, label string) {
		a := l.account(index)
		before := n.stake(t, s.escrow, a.Coldkey, s.netuid)
		paid := s.vaultUint(t, "claimed", id(1), id(index))
		amount := new(big.Int).Sub(a.Cumulative, paid).Uint64()
		escrow := n.stake(t, s.escrow, s.vaultColdkey, s.netuid)
		receipt := measured(t, label, false, relayer, s.vault, s.vaultArtifact, nil, "claim", id(1), id(rootIndex), a, l.proof(index))
		gained := n.stake(t, s.escrow, a.Coldkey, s.netuid) - before
		if escrow-n.stake(t, s.escrow, s.vaultColdkey, s.netuid) != amount || gained != amount && gained != amount-1 {
			t.Fatalf("claim of %d rao: escrow -%d, account +%d", amount, escrow-n.stake(t, s.escrow, s.vaultColdkey, s.netuid), gained)
		}
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "Claimed", map[string]any{
			"noId": id(1), "rootIndex": id(rootIndex), "index": id(index), "coldkey": a.Coldkey,
			"cumulative": a.Cumulative, "amount": id(amount), "caller": relayer.address,
		})
		if got := n.refused(t, relayer, s.vault, s.vaultArtifact, nil, "claim", id(1), id(rootIndex), a, l.proof(index)); got != "NothingToClaim" {
			t.Fatalf("a repeated claim refused with %s", got)
		}
		s.requireConservation(t)
	}

	// Epoch 1: the pool's stake is captured and committed as the first root.
	coldkeys := [][32]byte{newAccount(t), newAccount(t), newAccount(t), newAccount(t)}
	n.stakeFor(t, alice, s.vaultColdkey, operator.pool, s.netuid, 200*tao)
	closed(t, "close (capture)", 1)
	funded := s.vaultUint(t, "fundedThrough", id(1), id(1)).Uint64()
	first := newLedger(t, split(coldkeys, funded), deepLeafCount)
	commit1 := first.commitment("ledger header epoch 1")
	if got := n.refused(t, relayer, s.settlement, s.settlementArtifact, nil, "commitDistribution", id(1), id(1), id(0), commit1); got != "Unauthorized" {
		t.Fatalf("a commit from another account refused with %s", got)
	}
	receipt := measured(t, "commit", true, operator.rootSigner, s.settlement, s.settlementArtifact, nil, "commitDistribution", id(1), id(1), id(0), commit1)
	requireEvent(t, receipt, s.vaultArtifact, s.vault, "DistributionCommitted", map[string]any{
		"noId": id(1), "seq": uint64(1), "epoch": uint64(1), "root": commit1.Root, "total": commit1.Total,
		"leafCount": uint64(deepLeafCount), "ledgerHash": commit1.LedgerHash, "replacedSeq": uint64(0),
	})

	// R-5: approvals come only from an auditor, once each, for the exact
	// pending root; finalize needs the quorum and then the challenge window.
	t.Run("approval", func(t *testing.T) {
		if got := n.refused(t, relayer, s.settlement, s.settlementArtifact, nil, "finalizeDistribution", id(1)); got != "QuorumNotReached" {
			t.Fatalf("finalize without approval refused with %s", got)
		}
		if got := n.refused(t, relayer, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(1), commit1.Root, commit1.LedgerHash); got != "Unauthorized" {
			t.Fatalf("approval from a non-auditor refused with %s", got)
		}
		if got := n.refused(t, s.auditor, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(1), commit1.Root, [32]byte{1}); got != "PendingMismatch" {
			t.Fatalf("approval of another ledger hash refused with %s", got)
		}
		receipt := measured(t, "approve", false, s.auditor, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(1), commit1.Root, commit1.LedgerHash)
		requireEvent(t, receipt, s.settlementArtifact, s.settlement, "DistributionApproved", map[string]any{
			"noId": id(1), "seq": uint64(1), "auditor": s.auditor.address, "auditorSetVersion": uint64(1), "approvals": id(1),
		})
		if got := n.refused(t, s.auditor, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(1), commit1.Root, commit1.LedgerHash); got != "AlreadyApproved" {
			t.Fatalf("a second approval refused with %s", got)
		}
		if n.read(t, s.settlement, s.settlementArtifact, "approvalCount", id(1))[0].(*big.Int).Uint64() != 1 ||
			!n.read(t, s.settlement, s.settlementArtifact, "hasApproved", id(1), s.auditor.address)[0].(bool) {
			t.Fatal("approval views differ")
		}
		if got := n.refused(t, relayer, s.settlement, s.settlementArtifact, nil, "finalizeDistribution", id(1)); got != "ChallengeWindowOpen" {
			t.Fatalf("finalize inside the window refused with %s", got)
		}
		n.sealUntil(t, s.pending(t, 1).CommittedBlock+challengeBlocks)
		receipt = measured(t, "finalize", true, relayer, s.settlement, s.settlementArtifact, nil, "finalizeDistribution", id(1))
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "DistributionFinalized", map[string]any{
			"noId": id(1), "seq": uint64(1), "rootIndex": id(0), "epoch": uint64(1), "root": commit1.Root,
			"total": commit1.Total, "leafCount": uint64(deepLeafCount), "ledgerHash": commit1.LedgerHash,
		})
	})

	// Epoch 2, at the same cadence: a second capture and root extending the first.
	n.stakeFor(t, alice, s.vaultColdkey, operator.pool, s.netuid, 100*tao)
	s.close(t, 2, 1)
	funded = s.vaultUint(t, "fundedThrough", id(1), id(2)).Uint64()
	grown := split(coldkeys, funded)
	for i := range grown {
		if grown[i].Cumulative.Lt(&first.accounts[i].Cumulative) {
			t.Fatalf("account %d shrank", i)
		}
	}
	second := newLedger(t, grown, deepLeafCount)
	commit2 := second.commitment("ledger header epoch 2")
	n.send(t, operator.rootSigner, s.settlement, s.settlementArtifact, nil, "commitDistribution", id(2), id(1), id(1), commit2)

	// R-6 records four kinds of claim: vault2's first; an account's first,
	// right after its neighbour's; a second claim; and the first claim of an
	// account whose neighbour has not claimed.
	t.Run("claims", func(t *testing.T) {
		claim(t, first, 0, 0, "claim")
		claim(t, first, 0, 1, "claim (paired)")
	})

	// R-5: a demand proves an account against a finalized root, here root 0,
	// pays exactly demandFeeWei, which vault2 keeps, and blocks finalization
	// until the operator answers it with the account's path in the pending
	// root.
	t.Run("demand and answer", func(t *testing.T) {
		demander := newEVMKey(t)
		n.fundEVM(t, demander.address, 100*tao)
		a := first.account(2)
		short := new(big.Int).Sub(demandFeeWei, big.NewInt(tao))
		if got := n.refused(t, demander, s.vault, s.vaultArtifact, short, "demandPath", id(1), id(0), a, first.proof(2)); got != "InvalidDemandFee" {
			t.Fatalf("a short fee refused with %s", got)
		}
		kept := n.freeBalance(t, s.vaultColdkey)
		receipt := measured(t, "demand", false, demander, s.vault, s.vaultArtifact, demandFeeWei, "demandPath", id(1), id(0), a, first.proof(2))
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "PathDemanded", map[string]any{
			"noId": id(1), "seq": uint64(2), "rootIndex": id(0), "index": id(2), "demander": demander.address,
		})
		if n.freeBalance(t, s.vaultColdkey)-kept != new(big.Int).Quo(demandFeeWei, big.NewInt(tao)).Uint64() || s.pending(t, 1).OpenDemands != 1 {
			t.Fatalf("vault2 kept %d rao of the fee; open demands %d", n.freeBalance(t, s.vaultColdkey)-kept, s.pending(t, 1).OpenDemands)
		}
		if got := n.refused(t, demander, s.vault, s.vaultArtifact, demandFeeWei, "demandPath", id(1), id(0), a, first.proof(2)); got != "AlreadyDemanded" {
			t.Fatalf("a second demand refused with %s", got)
		}
		n.send(t, s.auditor, s.settlement, s.settlementArtifact, nil, "approveDistribution", id(1), uint64(2), commit2.Root, commit2.LedgerHash)
		n.sealUntil(t, s.pending(t, 1).CommittedBlock+challengeBlocks)
		if got := n.refused(t, relayer, s.settlement, s.settlementArtifact, nil, "finalizeDistribution", id(1)); got != "DemandOpen" {
			t.Fatalf("finalize with an open demand refused with %s", got)
		}
		receipt = measured(t, "answer", true, operator.rootSigner, s.vault, s.vaultArtifact, nil, "checkPath", id(1), id(0), a, first.proof(2), second.path(2))
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "PathAnswered", map[string]any{
			"noId": id(1), "seq": uint64(2), "rootIndex": id(0), "index": id(2), "answerer": operator.rootSigner.address,
		})
		if got := n.refused(t, relayer, s.vault, s.vaultArtifact, nil, "checkPath", id(1), id(0), a, first.proof(2), second.path(2)); got != "NoFault" {
			t.Fatalf("a second answer refused with %s", got)
		}
		receipt = n.send(t, relayer, s.settlement, s.settlementArtifact, nil, "finalizeDistribution", id(1))
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "DistributionFinalized", map[string]any{
			"noId": id(1), "seq": uint64(2), "rootIndex": id(1), "epoch": uint64(2), "root": commit2.Root,
			"total": commit2.Total, "leafCount": uint64(deepLeafCount), "ledgerHash": commit2.LedgerHash,
		})
		// Claims pay each account its increment since its last claim.
		claim(t, second, 1, 0, "claim (repeat)")
		claim(t, second, 1, 2, "claim (new)")
	})

	// R-6: a close defers every epoch since the pool's last funded one. Epochs
	// from 3 are short; the pool holds nothing movable, so the closes measure
	// the loop alone.
	t.Run("gap loop", func(t *testing.T) {
		current := n.read(t, s.v1, s.coordinatorArtifact, "currentEpoch")[0].(*big.Int).Uint64()
		for _, epoch := range []uint64{current, current + 2, current + 3} {
			gap := epoch - s.vaultUint(t, "lastFundedEpoch", id(1)).Uint64() - 1
			closed(t, fmt.Sprintf("close (gap %d)", gap), epoch)
		}
	})

	labels := make([]string, 0, len(gas))
	for label := range gas {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		m := gas[label]
		verdict := ""
		if m.operator && m.estimate > maximumGas {
			verdict = fmt.Sprintf("; above the operator policy's maximum_gas %d", maximumGas)
		}
		t.Logf("gas %-16s used %9d, least limit %9d, eth_estimateGas limit %9d%s", label, m.used, m.least, m.estimate, verdict)
	}
}

// split divides total among coldkeys: half to the first, half of the rest
// to the next, and what remains to the last.
func split(coldkeys [][32]byte, total uint64) []merkle.LedgerAccount {
	accounts := make([]merkle.LedgerAccount, len(coldkeys))
	for i, coldkey := range coldkeys {
		share := total / 2
		if i == len(coldkeys)-1 {
			share = total
		}
		accounts[i] = ledgerAccount(coldkey, share)
		total -= share
	}
	return accounts
}

// operatorMaximumGas reads maximum_gas from the operator gas policy SN25
// launched with (server/local/sn25golive/sn25-mainnet-gas-params.json).
func operatorMaximumGas(t *testing.T) uint64 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "server", "local", "sn25golive", "sn25-mainnet-gas-params.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		MaximumGas uint64 `json:"maximum_gas"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil || policy.MaximumGas == 0 {
		t.Fatalf("operator gas policy: %v", err)
	}
	return policy.MaximumGas
}
