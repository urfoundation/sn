//go:build linux || darwin

package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"

	"github.com/urnetwork/server/v2026/controller"
	"github.com/urnetwork/server/v2026/model"
)

// newE2EDrill is the failure drills' setting: the server and the validator
// have settled the pool's first v2 epoch, 2, whose finalized root 0 holds
// Carol's and Dave's accounts, and the server has mailed them. Every
// provider's own consent selects its coldkey from epoch 2.
func newE2EDrill(t testing.TB, names ...string) (*e2eServer, map[string]*e2eProvider) {
	t.Helper()
	f := newE2EFixture(t, e2eDefaultFirst)
	s := newE2EServer(t, f)
	providers := map[string]*e2eProvider{}
	for _, name := range append([]string{"carol", "dave"}, names...) {
		providers[name] = s.provider(name, f.firstEpoch)
	}
	carol, dave := providers["carol"], providers["dave"]
	usage, emission := map[*e2eProvider]uint64{carol: 4_000, dave: 1_000}, uint64(1_450_000_000_000)
	if index := s.epoch(f.firstEpoch, emission, usage); index != 0 {
		t.Fatalf("the first root is root %d", index)
	}
	rows := e2eRowsOf(usage)
	s.emails.require(t, e2eMailed(f.firstEpoch, map[*e2eProvider]uint64{carol: e2eShare(t, emission, rows, carol.coldkey), dave: e2eShare(t, emission, rows, dave.coldkey)})...)
	return s, providers
}

// e2eNewAccounts returns the indexes a root appends for new coldkeys after
// first existing accounts: ascending coldkey order.
func e2eNewAccounts(first uint64, providers ...*e2eProvider) map[*e2eProvider]uint64 {
	sorted := slices.Clone(providers)
	slices.SortFunc(sorted, func(a, b *e2eProvider) int { return strings.Compare(string(a.coldkey[:]), string(b.coldkey[:])) })
	indexes := map[*e2eProvider]uint64{}
	for k, p := range sorted {
		indexes[p] = first + uint64(k)
	}
	return indexes
}

// requireNoAccount checks that GET /sn/ledger/account lists no account for
// the provider's coldkey.
func (s *e2eServer) requireNoAccount(p *e2eProvider) {
	s.t.Helper()
	if result := s.ledgerAccount(p.coldkey); len(result.Accounts) != 0 {
		s.t.Fatalf("%s has an account: %+v", p.name, result.Accounts[0])
	}
}

// requireEpochAmount checks GET /account/epochs for the provider's network:
// epoch shows all of that epoch's amount.
func (s *e2eServer) requireEpochAmount(p *e2eProvider, epoch uint64, amount uint64) {
	s.t.Helper()
	epochs := s.accountEpochs(p)
	index := slices.IndexFunc(epochs, func(entry controller.AccountEpoch) bool { return entry.Epoch == epoch })
	if index < 0 || epochs[index].AmountRao != fmt.Sprintf("%d", amount) || epochs[index].ShareBps != 10_000 {
		s.t.Fatalf("GET /account/epochs for %s: %+v", p.name, epochs)
	}
}

// E-3: a missed close. Nobody closes epoch 3 inside its 1,200-block window
// while the server is down; its next turn plans nothing for the epoch, whose
// emission stays on the pool hotkey. The driver's close of epoch 4 defers
// epoch 3 with no funding and captures both epochs' emission, so the root
// covering epochs 3 and 4 pays epoch 3's emission to epoch 4's earners
// (§6.4): Erin, who worked only in epoch 3, gets nothing, and Frank, only in
// epoch 4, gets both epochs' emission, at the readers and in his email.
func TestSettlementV2E2EMissedCloseDefers(t *testing.T) {
	e2eServerEnv(t, func(t testing.TB) {
		s, p := newE2EDrill(t, "erin", "frank")
		f := s.f
		erin, frank := p["erin"], p["frank"]
		emission3, emission4 := uint64(1_000_000_000_001), uint64(2_000_000_000_003)
		s.settle(3, map[*e2eProvider]uint64{erin: 2_000})
		f.emitV2(emission3)
		f.chain.rollTo(f.end(3) + e2eCommitWindow + 2)
		if ran := s.turn(); len(ran) != 0 {
			t.Fatalf("the driver planned %d steps for a missed close", len(ran))
		}
		funded2 := f.fundedThrough(2)
		if lastFunded := e2eView(f, f.vault2, e2eVault2Binding.PackLastFundedEpoch(e2eRao(e2eNoID)), e2eVault2Binding.UnpackLastFundedEpoch); lastFunded.Uint64() != 2 ||
			f.chain.stake(f.poolV2, f.vault2Coldkey).Uint64() != emission3 {
			t.Fatal("a close outside its window moved the epoch's emission")
		}

		s.settle(4, map[*e2eProvider]uint64{frank: 3_000})
		f.emitV2(emission4)
		pending, _ := s.commit(4, 3)
		s.covers(pending.LedgerHash, 3, 4)
		if f.fundedThrough(3).Cmp(funded2) != 0 || pending.Total.Uint64() != funded2.Uint64()+emission3+emission4 || pending.LeafCount != 3 {
			t.Fatalf("epoch 3 is funded through %s after %s; the root is %+v", f.fundedThrough(3), funded2, pending)
		}
		s.approve()
		index := s.finalize()
		s.requireAccount(frank.coldkey, index, 2, emission3+emission4, 0)
		s.requireNoAccount(erin)
		s.requireEpochAmount(frank, 4, emission3+emission4)
		s.emails.require(t, e2eMailed(4, map[*e2eProvider]uint64{frank: emission3 + emission4})...)
		f.requireConservation()
	})
}

// E-3: a missed commit. Anyone closes epoch 3 inside its window, but the
// server is down until the window has passed: its next turn plans nothing
// for the epoch, whose funding waits in vault2. The driver's root for epoch 4
// covers epochs 3 and 4 and allocates each from its own usage (decision 3):
// Gina, who worked only in epoch 3, gets epoch 3's funding, and Hank, only
// in epoch 4, epoch 4's, at the readers and in their emails.
func TestSettlementV2E2EMissedCommitCarries(t *testing.T) {
	e2eServerEnv(t, func(t testing.TB) {
		s, p := newE2EDrill(t, "gina", "hank")
		f := s.f
		gina, hank := p["gina"], p["hank"]
		emission3, emission4 := uint64(1_000_000_000_001), uint64(2_000_000_000_003)
		s.settle(3, map[*e2eProvider]uint64{gina: 2_000})
		f.emitV2(emission3)
		f.closeV2(3)
		f.chain.rollTo(f.end(3) + e2eCommitWindow + 2)
		if ran := s.turn(); len(ran) != 0 {
			t.Fatalf("the driver planned %d steps after a missed commit window", len(ran))
		}
		if f.pending().Seq != 0 || f.rootCount() != 1 {
			t.Fatal("a root was committed for the missed epoch")
		}

		s.settle(4, map[*e2eProvider]uint64{hank: 3_000})
		f.emitV2(emission4)
		pending, _ := s.commit(4)
		s.covers(pending.LedgerHash, 3, 4)
		s.approve()
		index := s.finalize()
		indexes := e2eNewAccounts(2, gina, hank)
		s.requireAccount(gina.coldkey, index, indexes[gina], emission3, 0)
		s.requireAccount(hank.coldkey, index, indexes[hank], emission4, 0)
		s.requireEpochAmount(gina, 3, emission3)
		s.requireEpochAmount(hank, 4, emission4)
		s.emails.require(t, append(e2eMailed(3, map[*e2eProvider]uint64{gina: emission3}), e2eMailed(4, map[*e2eProvider]uint64{hank: emission4})...)...)
		f.requireConservation()
	})
}

// E-3: an unfinalized root replaced at end(e+1). The driver commits epoch 3's
// root, which no auditor approves: after its window the driver waits for
// approvals, and once they close at end(4) its commit of epoch 4 replaces
// the root (S3). The replacement covers epochs 3 and 4 from their own usage,
// listing epoch 3's recorded rows: Ivan, who worked only in epoch 3, and
// Judy, only in epoch 4, each get their own epoch's funding, at the readers
// and in their emails.
func TestSettlementV2E2EUnfinalizedRootReplaced(t *testing.T) {
	e2eServerEnv(t, func(t testing.TB) {
		s, p := newE2EDrill(t, "ivan", "judy")
		f := s.f
		ivan, judy := p["ivan"], p["judy"]
		emission3, emission4 := uint64(1_000_000_000_001), uint64(2_000_000_000_003)
		s.settle(3, map[*e2eProvider]uint64{ivan: 2_000})
		f.emitV2(emission3)
		replaced, _ := s.commit(3)
		dropped := s.covers(replaced.LedgerHash, 3)

		// No approval: after the window the driver waits for one.
		f.chain.rollTo(replaced.CommittedBlock + e2eWindow + 1)
		requireOutcome(t, e2eRan(t, s.turn(), controller.StLedgerV2StepFinalize, 3), model.StPublishStatusSkipped, "0 of 1 approvals")
		if f.rootCount() != 1 || f.pending().Seq != replaced.Seq {
			t.Fatal("an unapproved root finalized")
		}

		// At end(4) the driver replaces it; its finalize of epoch 3 finds the
		// slot taken by the replacement.
		s.settle(4, map[*e2eProvider]uint64{judy: 3_000})
		f.emitV2(emission4)
		pending, ran := s.commit(4)
		requireOutcome(t, e2eRan(t, ran, controller.StLedgerV2StepFinalize, 3), model.StPublishStatusSkipped, "no pending root for epoch 3")
		header := s.covers(pending.LedgerHash, 3, 4)
		if !slices.Equal(header.Epochs[0].Providers, dropped.Epochs[0].Providers) || pending.Seq != replaced.Seq+1 {
			t.Fatal("the replacement does not list epoch 3's recorded rows")
		}
		s.approve()
		index := s.finalize()
		indexes := e2eNewAccounts(2, ivan, judy)
		s.requireAccount(ivan.coldkey, index, indexes[ivan], emission3, 0)
		s.requireAccount(judy.coldkey, index, indexes[judy], emission4, 0)
		s.requireEpochAmount(ivan, 3, emission3)
		s.requireEpochAmount(judy, 4, emission4)
		s.emails.require(t, append(e2eMailed(3, map[*e2eProvider]uint64{ivan: emission3}), e2eMailed(4, map[*e2eProvider]uint64{judy: emission4})...)...)
		f.requireConservation()
	})
}

// E-3: a challenged root, and recovery with the Alice case (GL-4). Anyone
// closes epoch 3, and the root signer's key commits a root ahead of the
// server that moves value from Carol's account of root 0 to Alice, whose
// objects the operator's host publishes. The server's driver leaves the
// pending root alone, the validator's audit refuses it with the fault, and
// UR's watcher (snclaim v2 check --challenge) proves Carol's lowered account
// against root 0 and deletes it. The driver's root for epoch 4 then covers
// epochs 3 and 4: Alice, who worked only in the dropped epoch 3, and Bob,
// only in epoch 4, are each paid their own epoch's funding, at the readers
// and in their emails.
func TestSettlementV2E2EChallengedRootRecovers(t *testing.T) {
	e2eServerEnv(t, func(t testing.TB) {
		s, p := newE2EDrill(t, "alice", "bob")
		f := s.f
		carol, alice, bob := p["carol"], p["alice"], p["bob"]
		emission3, emission4 := uint64(1_000_000_000_001), uint64(2_000_000_000_003)
		usage3 := map[*e2eProvider]uint64{alice: 2_000}
		s.settle(3, usage3)
		f.emitV2(emission3)
		f.closeV2(3)

		// The forged root: the honest successor of root 0 with 7 rao moved from
		// Carol's account to Alice's new one, published at the operator's
		// endpoint and committed with the root signer's key.
		root0 := f.root(0)
		_, accounts0, err := controller.StLedgerV2Tree(s.ctx, root0.LedgerHash, root0.Root, uint256.MustFromBig(root0.Total), root0.LeafCount)
		if err != nil {
			t.Fatal(err)
		}
		honest, err := ledger.Build(s.ctx, ledger.BuildInput{NoID: e2eNoID, Epoch: 3, FundedThrough: map[uint64]uint256.Int{3: *uint256.MustFromBig(f.fundedThrough(3))},
			Previous: &ledger.Previous{Root: e2eLedgerRoot(root0.Epoch, root0.Root, root0.Total, root0.LeafCount, root0.LedgerHash), Accounts: accounts0},
			Epochs:   []ledger.EpochInput{{Epoch: 3, Rows: e2eRowsOf(usage3)}}})
		if err != nil {
			t.Fatal(err)
		}
		c := e2eAccountOf(t, accounts0, carol.coldkey)
		accounts := slices.Clone(honest.Accounts)
		accounts[c].Cumulative.SubUint64(&accounts[c].Cumulative, 7)
		a := e2eAccountOf(t, accounts, alice.coldkey)
		accounts[a].Cumulative.AddUint64(&accounts[a].Cumulative, 7)
		commitment, objects, _ := f.forge(honest, accounts)
		for _, object := range objects {
			if _, err := controller.PutStLedgerV2Object(s.ctx, object); err != nil {
				t.Fatal(err)
			}
		}
		seq := f.commit(f.operator, 3, commitment)

		// The server's driver leaves the pending root alone.
		requireOutcome(t, e2eRan(t, s.turn(), controller.StLedgerV2StepEpoch, 3), model.StPublishStatusSkipped, "a root for epoch 3 is already committed")
		// The validator's audit reproduces the published root and refuses it
		// with Carol's lowered account.
		if result := s.audit(); result.Action() != ledger.ActionChallenge || !slices.Equal(result.Faults, []ledger.Fault{{Index: c, Kind: merkle.LedgerFaultLowerValue}}) {
			t.Fatalf("the validator's audit of the forged root: %v %+v", result.Action(), result.Faults)
		}
		// UR's watcher deletes it.
		output, stderr, code := f.snclaim("v2", "check", "--all", "--no_id=1", "--challenge", "--key_file="+f.watcher.keyFile(t), "--vault="+f.vault2.Hex(), "--rpc="+f.chain.server.URL, "--api_url="+s.api.URL, fmt.Sprintf("--chain_id=%d", e2eChainID))
		want := fmt.Sprintf("DistributionDeleted: pool 1, seq %d\n  account:     %d of finalized root 0\n  fault:       lower_value at level 0\n  challenger:  %s\n", seq, c, f.watcher.address.Hex())
		if code != 0 || !strings.HasSuffix(output, want) {
			t.Fatalf("the watcher's challenge (exit %d):\n%s%s", code, output, stderr)
		}
		sent := f.chain.sentBy(f.watcher.address)
		f.requireLogs(f.chain.receipt(sent[len(sent)-1].Hash()), f.vault2Log("DistributionDeleted", e2eRao(e2eNoID), seq, e2eRao(0), e2eRao(c), uint8(merkle.LedgerFaultLowerValue), e2eRao(0), f.watcher.address))
		// The endpoint stops serving the deleted root's accounts.
		s.mirror()
		s.requireNoAccount(alice)
		s.requireAccount(carol.coldkey, 0, c, accounts0[c].Cumulative.Uint64(), 0)

		// Recovery: the driver's root for epoch 4 covers the dropped epoch 3.
		s.settle(4, map[*e2eProvider]uint64{bob: 3_000})
		f.emitV2(emission4)
		pending, _ := s.commit(4)
		s.covers(pending.LedgerHash, 3, 4)
		s.approve()
		index := s.finalize()
		indexes := e2eNewAccounts(2, alice, bob)
		s.requireAccount(alice.coldkey, index, indexes[alice], emission3, 0)
		s.requireAccount(bob.coldkey, index, indexes[bob], emission4, 0)
		s.requireAccount(carol.coldkey, index, c, accounts0[c].Cumulative.Uint64(), 0)
		s.requireEpochAmount(alice, 3, emission3)
		s.requireEpochAmount(bob, 4, emission4)
		s.emails.require(t, append(e2eMailed(3, map[*e2eProvider]uint64{alice: emission3}), e2eMailed(4, map[*e2eProvider]uint64{bob: emission4})...)...)
		f.requireConservation()
	})
}
