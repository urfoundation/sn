//go:build linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/validator"

	"github.com/urnetwork/server/v2026/controller"
)

// E-1: the switch from v1 to v2 at E_s = 2, through the operator server and
// the public readers (§12.1).
//   - v1 settles epoch 1, the last before E_s, in its normal windows: the
//     close captures the pool's emission into vault1, the operator commits
//     the root of the leaves v1's close recorded, and it finalizes. The
//     server's mirror marks the epoch finalized and mails each network its v1
//     share.
//   - From E_s the server's v2 driver closes epoch 2, builds the pool's first
//     root with the real builder from the providers' settled usage and own
//     consents, publishes it at the ledger endpoints and commits it. The
//     validator audits the published objects and approves through its
//     sender; the driver finalizes the root after its window and records it
//     for the readers, which mail each network its v2 amount.
//   - Weights follow §9.1 through the validator's shared resolver: epoch 1
//     pays the v1 pool hotkey, epoch 2 vault2's.
//   - A permissionless v1 close of epoch 2 puts the reveal-lag stake on the v1
//     pool hotkey into v1 funding, which carries when v1 finalizes epoch 2
//     without a root; the server's v1 driver may not touch epoch 2, and that
//     finalization mails nobody.
//   - GET /sn/ledger/account serves the pending account, then the finalized
//     account with a proof that verifies against vault2's root, and
//     GET /account/epochs shows epoch 2's v2 amount above epoch 1's v1 share.
//   - Alice claims her v2 account with snclaim against the server's
//     endpoint, and her v1 leaf on vault1: v1 claims still work.
func TestSettlementV2E2ESwitchThroughServer(t *testing.T) {
	e2eServerEnv(t, func(t testing.TB) {
		f := newE2EFixture(t, e2eDefaultFirst)
		s := newE2EServer(t, f)
		ctx := context.Background()
		alice, bob := s.provider("alice", 1), s.provider("bob", 1)

		// v1 epoch 1, the last before E_s.
		f.chain.rollTo(f.end(0) + 1)
		if ran := s.turn(); len(ran) != 0 {
			t.Fatalf("the v2 driver planned %d steps before E_s", len(ran))
		}
		// Their usage in epoch 1 is v1's: the first v2 root covers epoch 2 only.
		s.settle(1, map[*e2eProvider]uint64{alice: 3_000, bob: 1_000})
		tree := s.v1Leaves(1, map[*e2eProvider]int{alice: 7_500, bob: 2_500})
		f.emitV1(2_000_000_000_000)
		funded1 := f.closeV1(1)
		artifact := sha256.Sum256([]byte("v1 epoch 1 artifact"))
		f.commitV1(1, tree.Root(), artifact)

		// v2 epoch 2: the pool earns, and reveal-lag emission still reaches the
		// v1 pool hotkey after E_s.
		usage2 := map[*e2eProvider]uint64{alice: 2_000, bob: 6_000}
		s.settle(2, usage2)
		emission2 := uint64(1_450_000_000_007)
		f.emitV2(emission2)
		lag := uint64(333_000_000_000)
		f.emitV1(lag)

		// v1 finalizes epoch 1; the server's mirror marks it finalized and
		// mails each network its share of the v1 pool.
		receipt := f.finalizeV1(1)
		total1, _, expiry1 := f.entitlement(1)
		f.requireLogs(receipt, f.vault1Log("EntitlementFinalized", e2eRao(1), e2eRao(e2eNoID), tree.Root(), artifact, total1, expiry1),
			f.coordinatorLog("OperatorEpochFinalized", e2eRao(1), e2eRao(e2eNoID), true))
		if total1.Cmp(funded1) != 0 {
			t.Fatalf("v1 epoch 1 holds %s of %s funded", total1, funded1)
		}
		s.turn()
		share1 := func(bps int64) *big.Int {
			return new(big.Int).Div(new(big.Int).Mul(total1, big.NewInt(bps)), big.NewInt(10_000))
		}
		s.emails.require(t, fmt.Sprintf("epoch 1 %s share 7500 alpha %s", alice.userAuth, share1(7_500)), fmt.Sprintf("epoch 1 %s share 2500 alpha %s", bob.userAuth, share1(2_500)))

		// At end(2) anyone closes v1 epoch 2, capturing the reveal-lag stake
		// into its v1 funding. The server's v1 driver settles epoch 1 and never
		// epoch 2 (§8.1 v1 cutoff).
		lagFunded := f.closeV1(2)
		if lagFunded.Uint64() != lag {
			t.Fatalf("the v1 close of epoch 2 captured %s", lagFunded)
		}
		for epoch, want := range map[uint64]bool{1: true, 2: false} {
			if allowed, err := controller.StV1EpochAllowed(ctx, epoch); err != nil || allowed != want {
				t.Fatalf("v1 driver may touch epoch %d: %t %v", epoch, allowed, err)
			}
		}

		// The server's v2 driver: close, build, publish, commit.
		pending, _ := s.commit(2)
		s.covers(pending.LedgerHash, 2)
		if pending.Seq != 1 || pending.Total.Uint64() != emission2 || pending.LeafCount != 2 {
			t.Fatalf("the driver committed %+v", pending)
		}
		rows := e2eRowsOf(usage2)
		aliceAmount, bobAmount := e2eShare(t, emission2, rows, alice.coldkey), e2eShare(t, emission2, rows, bob.coldkey)
		// New accounts are appended in ascending coldkey order.
		a, b := uint64(0), uint64(1)
		if strings.Compare(string(bob.coldkey[:]), string(alice.coldkey[:])) < 0 {
			a, b = 1, 0
		}
		pendingOnly := func(p *e2eProvider, index uint64, amount uint64) {
			result := s.ledgerAccount(p.coldkey)
			if len(result.Accounts) != 1 {
				t.Fatalf("%s's pending account: %+v", p.name, result)
			}
			entry := result.Accounts[0]
			if entry.Index != index || entry.RootIndex != nil || entry.Cumulative != "0" || entry.Proof != nil || entry.Claimed != "0" || entry.PendingCumulative == nil || *entry.PendingCumulative != fmt.Sprintf("%d", amount) {
				t.Fatalf("%s's pending account: %+v", p.name, entry)
			}
		}
		s.mirror()
		pendingOnly(alice, a, aliceAmount)
		pendingOnly(bob, b, bobAmount)

		// The validator audits what the endpoint serves and approves.
		s.approve()

		// v1 finalizes epoch 2 without a root, so its reveal-lag funding
		// carries. After the window the driver finalizes the v2 root and
		// records it for the readers, which mail the v2 amounts; v1's
		// finalization of the v2 epoch mails nobody.
		receipt = f.finalizeV1(2)
		f.requireLogs(receipt, f.vault1Log("RootMissed", e2eRao(2), e2eRao(e2eNoID), lagFunded), f.coordinatorLog("OperatorEpochFinalized", e2eRao(2), e2eRao(e2eNoID), false))
		if index := s.finalize(); index != 0 {
			t.Fatalf("the first root is root %d", index)
		}
		amountTotal := aliceAmount + bobAmount
		s.emails.require(t, e2eMailed(2, map[*e2eProvider]uint64{alice: aliceAmount, bob: bobAmount})...)

		// The public readers.
		s.requireAccount(alice.coldkey, 0, a, aliceAmount, 0)
		s.requireAccount(bob.coldkey, 0, b, bobAmount, 0)
		epochs := s.accountEpochs(alice)
		if len(epochs) != 2 || epochs[0].Epoch != 2 || epochs[0].AmountRao != fmt.Sprintf("%d", aliceAmount) || epochs[0].ShareBps != int(aliceAmount*10_000/amountTotal) ||
			epochs[1].Epoch != 1 || epochs[1].AmountRao != "" || epochs[1].ShareBps != 7_500 {
			t.Fatalf("GET /account/epochs for Alice: %+v", epochs)
		}

		// Weights follow §9.1 through the validator's shared resolver.
		addresses := validator.SettlementV2Addresses{Settlement: f.settlement, Vault: f.vault2}
		for epoch, want := range map[uint64][32]byte{1: f.poolV1, 2: f.poolV2, 3: f.poolV2} {
			operator := e2eView(f, f.coordinator, e2eCoordinatorBinding.PackOperatorAt(e2eRao(e2eNoID), e2eRao(epoch)), e2eCoordinatorBinding.UnpackOperatorAt)
			if hotkey, err := s.chain.SettlementV2PoolHotkeyContext(ctx, addresses, e2eNoID, epoch, operator.PoolHotkey); err != nil || hotkey != want {
				t.Fatalf("epoch %d weights go to 0x%x: %v", epoch, hotkey, err)
			}
		}

		// Alice claims her v2 account with snclaim against the server's
		// endpoint, and her v1 leaf on vault1.
		relayer := f.anyone.keyFile(t)
		args := []string{"v2", "claim", fmt.Sprintf("--coldkey=0x%x", alice.coldkey), "--key_file=" + relayer, "--vault=" + f.vault2.Hex(), "--rpc=" + f.chain.server.URL, "--api_url=" + s.api.URL, fmt.Sprintf("--chain_id=%d", e2eChainID)}
		output, stderr, code := f.snclaim(args...)
		if code != 0 || !strings.Contains(output, fmt.Sprintf("Claimed: pool 1, account %d, root 0\n  coldkey:     %s\n  cumulative:  %s\n  paid:        %s\n", a, e2eColdkeyText(t, alice.coldkey), e2eAlphaText(aliceAmount), e2eAlphaText(aliceAmount))) {
			t.Fatalf("Alice's v2 claim (exit %d):\n%s%s", code, output, stderr)
		}
		sent := f.chain.sentBy(f.anyone.address)
		f.requireLogs(f.chain.receipt(sent[len(sent)-1].Hash()), f.vault2Log("Claimed", e2eRao(e2eNoID), e2eRao(0), e2eRao(a), alice.coldkey, e2eRao(aliceAmount), e2eRao(aliceAmount), f.anyone.address))
		s.requireAccount(alice.coldkey, 0, a, aliceAmount, aliceAmount)
		proof, err := tree.Proof(merkle.PayoutLeaf(alice.coldkey, big.NewInt(7_500)))
		if err != nil {
			t.Fatal(err)
		}
		receipt = f.must(f.anyone, f.vault1, nil, e2eVault1Binding.PackClaim(e2eRao(1), e2eRao(e2eNoID), alice.coldkey, big.NewInt(7_500), proof))
		f.requireLogs(receipt, f.vault1Log("Claimed", e2eRao(1), e2eRao(e2eNoID), alice.coldkey, big.NewInt(7_500), share1(7_500), f.anyone.address), f.vault1Log("ClaimPaid", alice.coldkey, share1(7_500), f.anyone.address))
		if f.chain.stake(f.escrowV2, alice.coldkey).Uint64() != aliceAmount || f.chain.stake(f.escrowV1, alice.coldkey).Cmp(share1(7_500)) != 0 {
			t.Fatal("Alice's claims did not pay her coldkey")
		}
		f.requireConservation()
		if !e2eView(f, f.vault1, e2eVault1Binding.PackConservationHolds(), e2eVault1Binding.UnpackConservationHolds) {
			t.Fatal("vault1 conservation does not hold")
		}
	})
}
