//go:build linux || darwin

// The phase C recovery component (VERSION2-FINAL.md §16 step 6) through the
// full validator path: the audit reads the pool's SurplusAssigned events and
// vault1's entitlement of the scheduled source epoch from chain, plans where
// the recovery lands, and approves a root only when it carries exactly that
// component, allocated by the source epoch's own rows.
package validator

import (
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

const (
	// settlementV2RecoveryAmount is the source epoch's v1 funding, recovered
	// by its rows; settlementV2RecoveryOrdinary is the rest of the swept
	// surplus (another missed epoch's share, reveal-lag stake), which is
	// ordinary funding of the receiving epoch.
	settlementV2RecoveryAmount   = 4_132_715_621_039
	settlementV2RecoveryOrdinary = 3_496_017_057_872
	// The source epoch is 2, before pool 1's first v2 epoch 3; the surplus is
	// assigned at block 550, after epoch 4's close, so it funds epoch 5.
	settlementV2RecoverySource = 2
	settlementV2RecoveryBlock  = 550
)

// settlementV2RecoveryRows are the source epoch's rows: client 15 bound to a
// fleet at the source epoch's start, client 16 a candidate without an active
// binding, client 17 without a wallet, coldkey 12 shared by clients 16 and 18,
// and coldkey 2, which F already holds.
func settlementV2RecoveryRows() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		settlementV2AuditTestRow(3, 2, 7_000_001, false), settlementV2AuditTestRow(15, 11, 5_000_000, true),
		settlementV2AuditTestRow(16, 12, 3_000_000, false), settlementV2AuditTestRow(17, 0, 9_000_000, false),
		settlementV2AuditTestRow(18, 12, 1_000_003, false),
	}
}

// settlementV2RecoveryFunding is the standard funding with the swept surplus
// in epoch 5.
func settlementV2RecoveryFunding() map[uint64]uint256.Int {
	funded := settlementV2AuditTestFunding()
	f5 := funded[5]
	funded[5] = *new(uint256.Int).AddUint64(&f5, settlementV2RecoveryAmount+settlementV2RecoveryOrdinary)
	return funded
}

// settlementV2RecoveryP builds the pending root over F for epochs 4 and 5,
// with component at epoch 5 (none when nil).
func settlementV2RecoveryP(t *testing.T, f *ledger.Ledger, component *ledger.RecoveryInput) *ledger.Ledger {
	t.Helper()
	at5 := ledger.EpochInput{Epoch: 5, Rows: settlementV2AuditTestRows5()}
	if component != nil {
		at5.Recovery = []ledger.RecoveryInput{*component}
	}
	return settlementV2AuditTestBuild(t, f, []ledger.EpochInput{{Epoch: 4, Rows: settlementV2AuditTestRows4()}, at5}, settlementV2RecoveryFunding())
}

func settlementV2RecoveryComponent(rows []protocol.LedgerUsageRow) *ledger.RecoveryInput {
	return &ledger.RecoveryInput{Recovery: ledger.Recovery{Epoch: settlementV2RecoverySource, Amount: settlementV2RecoveryAmount}, Rows: rows}
}

// useRecovery gives pool 1 a recovery schedule of the source epoch, vault1's
// RootMissed entitlement of it, the swept surplus assigned to epoch 5 and the
// source epoch's fleet binding, and puts the pending root on chain.
func (self *settlementV2AuditTestFixture) useRecovery(t *testing.T, f *ledger.Ledger, pending *ledger.Ledger, objects ledger.Objects) {
	t.Helper()
	self.useRecoverySchedule()
	self.chain.lock.Lock()
	self.chain.entitlements[settlementV2RecoverySource] = stabi.STSettlementVaultEntitlement{Funded: big.NewInt(settlementV2RecoveryAmount), Total: big.NewInt(settlementV2RecoveryAmount), Claimed: new(big.Int), Status: ledger.V1EpochRootMissed}
	self.chain.lock.Unlock()
	self.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "SurplusAssigned", settlementV2RecoveryBlock, big.NewInt(settlementV2AuditTestNoID), big.NewInt(5), new(big.Int).SetUint64(settlementV2RecoveryAmount+settlementV2RecoveryOrdinary))
	self.chain.fleetBound(t, 190, settlementV2AuditTestClient(15), 2, 2)
	self.chain.fleetBound(t, 195, settlementV2AuditTestClient(16), 2, 2)
	self.chain.setActive(settlementV2AuditTestClient(15), settlementV2RecoverySource, 200, true)
	self.useFunding(settlementV2RecoveryFunding())
	self.useScenario(t, f, pending.Commit, objects, true, 610)
}

// useRecoverySchedule points the audit's reader at the test v1 vault and
// gives pool 1 of the test vault2 a schedule recovering the source epoch.
func (self *settlementV2AuditTestFixture) useRecoverySchedule() {
	reader := self.audit.reader.(*settlementV2AuditChain)
	reader.settlementVault = settlementV2AuditTestV1Vault
	reader.schedule = func(vault [20]byte, noID uint64) *ledger.RecoverySchedule {
		if vault != settlementV2TestVault || noID != settlementV2AuditTestNoID {
			return nil
		}
		return &ledger.RecoverySchedule{Vault: vault, NoID: noID, Sources: []ledger.Recovery{{Epoch: settlementV2RecoverySource, Amount: settlementV2RecoveryAmount}}, FromBlock: 300, ThroughEpoch: 8}
	}
}

// The validator accepts a correct recovery root: the source epoch's share
// is allocated by the source epoch's rows, exactly, and the rest of the
// surplus is ordinary funding of epoch 5. It approves and keeps the source
// rows as fixed inputs.
func TestSettlementV2AuditRecoveryApproves(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2RecoveryP(t, f, settlementV2RecoveryComponent(settlementV2RecoveryRows()))
	component := p.Epochs[1].Recovery[0]
	var sum uint256.Int
	for _, d := range component.Deltas {
		sum.Add(&sum, &d.Delta)
	}
	if !component.Allocated || sum.Uint64() != settlementV2RecoveryAmount {
		t.Fatalf("component %+v sums to %s", component, sum.Dec())
	}
	fixture.useRecovery(t, f, p, p.Objects)
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionApprove || record.Check != "none" || record.Error != "" {
		t.Fatalf("record %+v", record)
	}
	if chunks, ok, err := fixture.audit.custody.ValidRows(t.Context(), settlementV2AuditTestNoID, settlementV2RecoverySource); err != nil || !ok || !slices.Equal(chunks, component.Providers) {
		t.Fatalf("source rows recorded as %v %v: %v", chunks, ok, err)
	}
	// vault1 was read once for the one placed source; the source epoch's two
	// candidates were read at both of its boundaries.
	if fixture.chain.count("entitlement") != 1 {
		t.Fatalf("%d entitlement reads", fixture.chain.count("entitlement"))
	}
}

// Each tampered recovery is refused through the full validator path, with no
// approval and no demand: the published accounts reproduce the root, so the
// dispute is over the allocation, not the data.
func TestSettlementV2AuditRecoveryRefusesTampering(t *testing.T) {
	t.Parallel()
	f := settlementV2AuditTestF(t)
	honest := settlementV2RecoveryP(t, f, settlementV2RecoveryComponent(settlementV2RecoveryRows()))
	cases := []struct {
		name  string
		build func(t *testing.T) (ledger.Root, ledger.Objects)
		check ledger.Check
		want  string
	}{
		{"leaves moved between recovery coldkeys", func(t *testing.T) (ledger.Root, ledger.Objects) {
			pub := settlementV2AuditTestPublish(t, honest)
			accounts := slices.Clone(honest.Accounts)
			from := slices.IndexFunc(accounts, func(a merkle.LedgerAccount) bool { return a.Coldkey == settlementV2AuditTestColdkey(12) })
			to := slices.IndexFunc(accounts, func(a merkle.LedgerAccount) bool { return a.Coldkey == settlementV2AuditTestColdkey(9) })
			accounts[from].Cumulative.SubUint64(&accounts[from].Cumulative, 1_000_000)
			accounts[to].Cumulative.AddUint64(&accounts[to].Cumulative, 1_000_000)
			pub.setAccounts(t, accounts)
			return pub.seal(t), pub.objects
		}, ledger.CheckAllocation, "increased"},
		{"recovery omitted: all of epoch 5 paid as ordinary", func(t *testing.T) (ledger.Root, ledger.Objects) {
			p := settlementV2RecoveryP(t, f, nil)
			return p.Commit, p.Objects
		}, ledger.CheckRecovery, "omits"},
		{"recovery amount inflated", func(t *testing.T) (ledger.Root, ledger.Objects) {
			component := settlementV2RecoveryComponent(settlementV2RecoveryRows())
			component.Amount += 1_000_000_000
			p := settlementV2RecoveryP(t, f, component)
			return p.Commit, p.Objects
		}, ledger.CheckRecovery, "amount"},
		{"head flag of the source epoch cleared", func(t *testing.T) (ledger.Root, ledger.Objects) {
			rows := settlementV2RecoveryRows()
			rows[1].HeadExcluded = false
			p := settlementV2RecoveryP(t, f, settlementV2RecoveryComponent(rows))
			return p.Commit, p.Objects
		}, ledger.CheckHeadExclusion, "cleared"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			commit, objects := c.build(t)
			fixture.useRecovery(t, f, &ledger.Ledger{Commit: commit}, objects)
			fixture.mustPass(t)
			record := fixture.record(t, 2)
			if record.Decision != settlementV2DecisionRefuse || record.Check != c.check.String() || !strings.Contains(record.Error, c.want) {
				t.Fatalf("record %+v, want a refusal at %v", record, c.check)
			}
			if calls := fixture.sentCalls(t); len(calls) != 0 {
				t.Fatalf("sent %d transactions", len(calls))
			}
		})
	}
}

// The zero-row edge case: a source epoch whose rows have no eligible usage
// leaves its share in epoch 5's ordinary funding, and the validator approves
// that root.
func TestSettlementV2AuditRecoveryZeroRows(t *testing.T) {
	t.Parallel()
	for name, rows := range map[string][]protocol.LedgerUsageRow{
		"no rows":           nil,
		"no eligible usage": {settlementV2AuditTestRow(15, 11, 5_000_000, true), settlementV2AuditTestRow(17, 0, 9_000_000, false)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			f := settlementV2AuditTestF(t)
			p := settlementV2RecoveryP(t, f, settlementV2RecoveryComponent(rows))
			if p.Epochs[1].Recovery[0].Allocated {
				t.Fatal("a source without eligible usage allocated")
			}
			funding := settlementV2RecoveryFunding()
			f4, f5 := funding[4], funding[5]
			if f5Funding := new(uint256.Int).Sub(&f5, &f4); !p.Epochs[1].Amount.Eq(f5Funding) {
				t.Fatalf("epoch 5 allocated %s, want all of f_5 %s", p.Epochs[1].Amount.Dec(), f5Funding.Dec())
			}
			fixture.useRecovery(t, f, p, p.Objects)
			fixture.mustPass(t)
			fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
		})
	}
}

// Recovery comes only from the chain: a root that carries one before any
// surplus is assigned is refused, and before vault1 shows the source epoch
// RootMissed with its funding the audit decides nothing and retries.
func TestSettlementV2AuditRecoveryNeedsChainFacts(t *testing.T) {
	t.Parallel()
	f := settlementV2AuditTestF(t)
	p := settlementV2RecoveryP(t, f, settlementV2RecoveryComponent(settlementV2RecoveryRows()))
	t.Run("no surplus assigned", func(t *testing.T) {
		t.Parallel()
		fixture := newSettlementV2AuditTestFixture(t)
		fixture.useRecoverySchedule()
		fixture.useFunding(settlementV2RecoveryFunding())
		fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
		fixture.mustPass(t)
		if record := fixture.record(t, 2); record.Decision != settlementV2DecisionRefuse || record.Check != ledger.CheckRecovery.String() {
			t.Fatalf("record %+v", record)
		}
		if fixture.chain.count("entitlement") != 0 {
			t.Fatal("vault1 read without a placed recovery")
		}
	})
	t.Run("source not yet RootMissed", func(t *testing.T) {
		t.Parallel()
		fixture := newSettlementV2AuditTestFixture(t)
		fixture.useRecovery(t, f, p, p.Objects)
		fixture.chain.lock.Lock()
		fixture.chain.entitlements[settlementV2RecoverySource] = stabi.STSettlementVaultEntitlement{Funded: big.NewInt(settlementV2RecoveryAmount), Status: 1}
		fixture.chain.lock.Unlock()
		if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "RootMissed") {
			t.Fatalf("pass: %v", err)
		}
		if len(fixture.sentCalls(t)) != 0 {
			t.Fatal("sent with an unconfirmed source")
		}
		// Once vault1 shows it, the next pass approves.
		fixture.chain.lock.Lock()
		fixture.chain.entitlements[settlementV2RecoverySource] = stabi.STSettlementVaultEntitlement{Funded: big.NewInt(settlementV2RecoveryAmount), Total: big.NewInt(settlementV2RecoveryAmount), Status: ledger.V1EpochRootMissed}
		fixture.chain.lock.Unlock()
		fixture.mustPass(t)
		fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	})
	t.Run("a pool without a schedule reads nothing", func(t *testing.T) {
		t.Parallel()
		fixture := newSettlementV2AuditTestFixture(t)
		plain := settlementV2AuditTestP(t, f)
		fixture.useScenario(t, f, plain.Commit, plain.Objects, true, 610)
		fixture.mustPass(t)
		fixture.requireApproved(t, fixture.sentCalls(t), 2, plain.Commit)
		if fixture.chain.count("entitlement") != 0 {
			t.Fatal("vault1 read for a pool without a schedule")
		}
	})
}

// surplusQueries returns the from blocks of the eth_getLogs filters for
// SurplusAssigned.
func (self *settlementV2AuditTestChain) surplusQueries() []uint64 {
	self.lock.Lock()
	defer self.lock.Unlock()
	var froms []uint64
	for _, query := range self.queries {
		if len(query.Topics) > 0 && slices.Contains(query.Topics[0], settlementV2VaultABI.Events["SurplusAssigned"].ID) {
			froms = append(froms, query.FromBlock.ToInt().Uint64())
		}
	}
	return froms
}

// A node whose log index lags the finalized head neither serves the commit
// block nor its logs: the audit then decides nothing, rather than refuse a
// correct root for a recovery it could not see, and approves once the node
// has caught up.
func TestSettlementV2AuditRecoveryWaitsForALaggingLogIndex(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2RecoveryP(t, f, settlementV2RecoveryComponent(settlementV2RecoveryRows()))
	fixture.useRecovery(t, f, p, p.Objects)
	fixture.chain.lock.Lock()
	fixture.chain.mappedThrough = 540
	fixture.chain.lock.Unlock()
	if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "SurplusAssigned") {
		t.Fatalf("pass over a lagging node: %v", err)
	}
	var record settlementV2AuditRecord
	if ok, err := fixture.audit.custody.readJSON(fixture.audit.custody.path("audits", fmt.Sprintf("%d-%d.json", settlementV2AuditTestNoID, 2)), &record); err != nil || (ok && record.Decision != "") || len(fixture.sentCalls(t)) != 0 {
		t.Fatalf("decided %q over a lagging node: %v", record.Decision, err)
	}
	fixture.chain.lock.Lock()
	fixture.chain.mappedThrough = 0
	fixture.chain.lock.Unlock()
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
}

// The surplus index scans once and keeps a checkpoint: a later pass over the
// same root reads no logs, and a later root scans only the blocks after the
// checkpoint, from the block after the earlier root's commit.
func TestSettlementV2AuditRecoveryScansFromItsCheckpoint(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2RecoveryP(t, f, settlementV2RecoveryComponent(settlementV2RecoveryRows()))
	// One provider chunk is withheld, so the first pass waits for it.
	withheld := p.Epochs[0].Providers[0]
	objects := maps.Clone(p.Objects)
	delete(objects, withheld)
	fixture.useRecovery(t, f, p, objects)
	fixture.mustPass(t)
	if record := fixture.record(t, 2); record.Decision != "" {
		t.Fatalf("decided %q without the chunk", record.Decision)
	}
	if froms := fixture.chain.surplusQueries(); !slices.Equal(froms, []uint64{300}) {
		t.Fatalf("surplus scans from %v", froms)
	}
	fixture.objects.publish(ledger.Objects{withheld: p.Objects[withheld]})
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	if froms := fixture.chain.surplusQueries(); !slices.Equal(froms, []uint64{300}) {
		t.Fatalf("the second pass scanned again: %v", froms)
	}
	// A replacement root committed later scans only what is new.
	fixture.committed(t, 705, 3, p.Commit, 2)
	fixture.chain.advanceTo(709)
	fixture.setPool(settlementV2AuditTestPending(3, p.Commit, 705), f.Commit)
	fixture.mustPass(t)
	if froms := fixture.chain.surplusQueries(); !slices.Equal(froms, []uint64{300, 606}) {
		t.Fatalf("surplus scans from %v", froms)
	}
	if record := fixture.record(t, 3); record.Decision != settlementV2DecisionApprove {
		t.Fatalf("record %+v", record)
	}
}
