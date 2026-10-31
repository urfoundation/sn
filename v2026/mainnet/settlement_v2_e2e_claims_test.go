package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
)

// e2eRows is an epoch's provider rows: Alice and Bob every epoch, Carol in
// even epochs, a provider without a paying wallet and a head-excluded one,
// whom the allocation must skip.
func e2eRows(epoch uint64) []protocol.LedgerUsageRow {
	rows := []protocol.LedgerUsageRow{
		e2eRow(1, e2eKey("alice"), 3_000),
		e2eRow(2, e2eKey("bob"), 1_000+100*epoch),
		e2eRow(4, [32]byte{}, 777),
		e2eRow(5, e2eKey("head-fleet"), 5_000),
	}
	rows[3].HeadExcluded = true
	if epoch%2 == 0 {
		rows = append(rows[:2], append([]protocol.LedgerUsageRow{e2eRow(3, e2eKey("carol"), 500)}, rows[2:]...)...)
	}
	return rows
}

// e2eShare is coldkey's allocation of amount over rows by the §6.3 rule.
func e2eShare(t testing.TB, amount uint64, rows []protocol.LedgerUsageRow, coldkey [32]byte) uint64 {
	t.Helper()
	deltas, err := protocol.AllocateLedgerEpoch(context.Background(), uint256.NewInt(amount), rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, delta := range deltas {
		if delta.Coldkey == coldkey {
			return delta.Delta.Uint64()
		}
	}
	return 0
}

// E-2: ten epochs, each closed, committed, approved and finalized through
// STSettlement2, then one claim, sent by a third party with snclaim v2 claim,
// pays Alice everything the ten roots accrued, to her coldkey (G3, G4). A
// later root's claim pays only the increment; the paid root then has nothing
// to claim, and a repeat claim sends nothing.
func TestSettlementV2E2ETenEpochsThenOneClaim(t *testing.T) {
	f := newE2EFixture(t, e2eDefaultFirst)
	alice := e2eKey("alice")
	var expected, last, a uint64
	for epoch := f.firstEpoch; epoch < f.firstEpoch+10; epoch++ {
		emission := 1_450_000_000_000 + epoch*1_000_000_007
		built := f.epoch(epoch, emission, e2eRows(epoch))
		expected += e2eShare(t, emission, e2eRows(epoch), alice)
		// Alice's account keeps the position her first root gave it.
		index := e2eAccountOf(t, built.Accounts, alice)
		if epoch == f.firstEpoch {
			a = index
		}
		if index != a || built.Accounts[index].Cumulative.Uint64() != expected || len(built.Epochs) != 1 {
			t.Fatalf("epoch %d: Alice's account %d holds %s, want %d at %d", epoch, index, built.Accounts[index].Cumulative.Dec(), expected, a)
		}
		last = epoch
	}
	if count := f.rootCount(); count != 10 || f.claimed(a).Sign() != 0 {
		t.Fatalf("%d finalized roots, Alice claimed %s", count, f.claimed(a))
	}
	f.requireConservation()

	// One claim pays all ten epochs against the latest root, 9.
	status, _, code := f.snclaim(f.snclaimArgs("v2", "status", fmt.Sprintf("--coldkey=0x%x", alice))...)
	if code != 0 || !strings.Contains(status, fmt.Sprintf("  pool 1, account %d:\n    finalized:  %s at root 9\n    claimed:    %s\n    claimable:  %s\n    pending:    none\n", a, e2eAlphaText(expected), e2eAlphaText(0), e2eAlphaText(expected))) {
		t.Fatalf("status before the claim (exit %d):\n%s", code, status)
	}
	relayer := f.anyone.keyFile(t)
	output, stderr, code := f.snclaim(f.snclaimArgs("v2", "claim", fmt.Sprintf("--coldkey=0x%x", alice), "--key_file="+relayer)...)
	if code != 0 || !strings.Contains(output, fmt.Sprintf("Claimed: pool 1, account %d, root 9\n  coldkey:     %s\n  cumulative:  %s\n  paid:        %s\n  caller:      %s\n", a, e2eColdkeyText(t, alice), e2eAlphaText(expected), e2eAlphaText(expected), f.anyone.address.Hex())) {
		t.Fatalf("one claim (exit %d):\n%s%s", code, output, stderr)
	}
	sent := f.chain.sentBy(f.anyone.address)
	if len(sent) != 1 {
		t.Fatalf("%d transactions for one claim", len(sent))
	}
	f.requireLogs(f.chain.receipt(sent[0].Hash()), f.vault2Log("Claimed", e2eRao(e2eNoID), e2eRao(9), e2eRao(a), alice, e2eRao(expected), e2eRao(expected), f.anyone.address))
	if f.claimed(a).Uint64() != expected || f.chain.stake(f.escrowV2, alice).Uint64() != expected {
		t.Fatalf("Alice claimed %s and holds %s, want %d", f.claimed(a), f.chain.stake(f.escrowV2, alice), expected)
	}
	f.requireConservation()

	// A later root: only the increment is paid.
	paid := expected
	emission := uint64(1_500_000_000_001)
	f.epoch(last+1, emission, e2eRows(last+1))
	expected += e2eShare(t, emission, e2eRows(last+1), alice)
	output, stderr, code = f.snclaim(f.snclaimArgs("v2", "claim", fmt.Sprintf("--coldkey=0x%x", alice), "--key_file="+relayer)...)
	increment := expected - paid
	if code != 0 || !strings.Contains(output, fmt.Sprintf("Claimed: pool 1, account %d, root 10\n  coldkey:     %s\n  cumulative:  %s\n  paid:        %s\n", a, e2eColdkeyText(t, alice), e2eAlphaText(expected), e2eAlphaText(increment))) {
		t.Fatalf("the increment (exit %d):\n%s%s", code, output, stderr)
	}
	sent = f.chain.sentBy(f.anyone.address)
	f.requireLogs(f.chain.receipt(sent[1].Hash()), f.vault2Log("Claimed", e2eRao(e2eNoID), e2eRao(10), e2eRao(a), alice, e2eRao(expected), e2eRao(increment), f.anyone.address))
	if f.claimed(a).Uint64() != expected || f.chain.stake(f.escrowV2, alice).Uint64() != expected {
		t.Fatalf("after the increment Alice claimed %s and holds %s, want %d", f.claimed(a), f.chain.stake(f.escrowV2, alice), expected)
	}
	f.requireConservation()

	// Nothing is left: the paid root and the older one refuse, and snclaim
	// sends nothing.
	for _, rootIndex := range []uint64{10, 9} {
		view := f.publisher.view(f.root(rootIndex).Root)
		account, proof := e2eProof(t, view.tree, view.accounts, a)
		f.refused(f.anyone, f.vault2, nil, e2eVault2Binding.PackClaim(e2eRao(e2eNoID), e2eRao(rootIndex), account, proof), "NothingToClaim")
	}
	output, _, code = f.snclaim(f.snclaimArgs("v2", "claim", fmt.Sprintf("--coldkey=0x%x", alice), "--key_file="+relayer)...)
	if code != 0 || !strings.HasSuffix(output, fmt.Sprintf("pool 1 account %d: claimed %s already reaches the cumulative %s; nothing to claim\n", a, e2eAlphaText(expected), e2eAlphaText(expected))) || len(f.chain.sentBy(f.anyone.address)) != 2 {
		t.Fatalf("a repeat claim (exit %d):\n%s", code, output)
	}
}

// e2eColdkeyText renders a coldkey as snclaim prints it.
func e2eColdkeyText(t testing.TB, coldkey [32]byte) string {
	t.Helper()
	address, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s (0x%x)", address, coldkey)
}

// e2eAlphaText renders rao as snclaim prints it.
func e2eAlphaText(rao uint64) string {
	return fmt.Sprintf("%d.%09d α (%d rao)", rao/1e9, rao%1e9, rao)
}
