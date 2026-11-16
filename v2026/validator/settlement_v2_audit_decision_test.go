//go:build linux || darwin

// The settlement v2 audit's §9.3 decision: a provable fault is challenged;
// missing or unreproducible objects lead to a refusal and the demand set;
// approvals, challenges and demands go through the auditor sender, once, and
// survive a restart.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// settlementV2AuditTestLargeF is a finalized root for epoch 3 of n accounts,
// coldkey i+1 at index i; index 0 holds about half of the value, the others
// value in proportion to their index. The funding is that of a root over it
// for epochs 4 and 5.
func settlementV2AuditTestLargeF(t *testing.T, n int) (*ledger.Ledger, map[uint64]uint256.Int) {
	t.Helper()
	rows := make([]protocol.LedgerUsageRow, n)
	var small uint64
	for i := 1; i < n; i++ {
		small += uint64(i + 1)
	}
	for i := range rows {
		usage := uint64(i + 1)
		if i == 0 {
			usage = small
		}
		rows[i] = settlementV2AuditTestRow(100+i, i+1, usage, false)
	}
	funded := map[uint64]uint256.Int{3: *uint256.NewInt(1_000_000_000_000), 4: *uint256.NewInt(1_000_001_600_000), 5: *uint256.NewInt(1_000_002_500_000)}
	return settlementV2AuditTestBuild(t, nil, []ledger.EpochInput{{Epoch: 3, Rows: rows}}, funded), funded
}

// useLargeScenario puts a large F on chain with a pending root over it whose
// objects are withheld, and a recording sender in place of the auditor's.
func (self *settlementV2AuditTestFixture) useLargeScenario(t *testing.T, n int) (*ledger.Ledger, *ledger.Ledger, *settlementV2AuditTestSender) {
	t.Helper()
	f, funded := settlementV2AuditTestLargeF(t, n)
	p := settlementV2AuditTestBuild(t, f, []ledger.EpochInput{{Epoch: 4, Rows: settlementV2AuditTestRows4()}, {Epoch: 5, Rows: settlementV2AuditTestRows5()}}, funded)
	self.useFunding(funded)
	sender := &settlementV2AuditTestSender{}
	self.audit.sender = sender
	return f, p, sender
}

// requireSentDemands requires that the recording sender was asked for exactly
// the demands of indexes, in order, each with its account of the finalized
// root rootIndex, f, and a proof that verifies against it.
func requireSettlementV2AuditDemands(t *testing.T, sender *settlementV2AuditTestSender, f *ledger.Ledger, rootIndex uint64, indexes []uint64) {
	t.Helper()
	if len(sender.demands) != len(indexes) || len(sender.approvals)+len(sender.challenges) != 0 {
		t.Fatalf("%d demands, %d approvals and %d challenges sent; want %d demands", len(sender.demands), len(sender.approvals), len(sender.challenges), len(indexes))
	}
	root, total := f.Tree.Root()
	for k, index := range indexes {
		account, want := sender.demands[k], f.Accounts[index]
		proof := make([]merkle.LedgerNode, len(sender.proofs[k]))
		for j, node := range sender.proofs[k] {
			proof[j] = merkle.LedgerNode{Hash: node.Hash, Sum: *uint256.MustFromBig(node.Sum)}
		}
		cumulative := uint256.MustFromBig(account.Cumulative)
		if sender.rootIndexes[k] != rootIndex || account.Index.Uint64() != index || account.Coldkey != want.Coldkey || !cumulative.Eq(&want.Cumulative) ||
			!merkle.VerifyLedgerProof(root, &total, f.Tree.LeafCount(), index, account.Coldkey, cumulative, proof) {
			t.Fatalf("demand %d is account %s, not account %d of F with its proof", k, account.Index, index)
		}
	}
}

// VA-4: a fault the published tree proves against F is challenged: checkPath
// carries F's account with its proof against F and the pending root's path,
// which shows the fault at the leaf. The challenge deletes the root, and a
// later pass finds nothing pending.
func TestSettlementV2AuditChallengesProvableFault(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	pub := settlementV2AuditTestPublish(t, p)
	accounts := slices.Clone(p.Accounts)
	accounts[2].Coldkey = settlementV2AuditTestColdkey(99)
	pub.setAccounts(t, accounts)
	commit := pub.seal(t)
	fixture.useScenario(t, f, commit, pub.objects, true, 610)
	fixture.chain.faults[2] = true
	fixture.mustPass(t)
	calls := fixture.sentCalls(t)
	if len(calls) != 1 || calls[0].method != "checkPath" || calls[0].to != settlementV2TestVault || calls[0].value.Sign() != 0 {
		t.Fatalf("sent %d transactions, want one checkPath", len(calls))
	}
	requireSettlementV2AuditOpening(t, calls[0], f, 0, 2)
	account := calls[0].account()
	tree, err := merkle.BuildLedgerSumTree(t.Context(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	root, total := tree.Root()
	fault, level, err := merkle.WalkLedgerPath(root, &total, tree.LeafCount(), 2, account.Coldkey, uint256.MustFromBig(account.Cumulative), calls[0].path())
	if err != nil || fault != merkle.LedgerFaultWrongLeaf || level != 0 {
		t.Fatalf("the path shows %v at level %d: %v", fault, level, err)
	}
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionChallenge || !record.Done || record.Outcome != "sent" || len(record.Faults) != 1 || record.Faults[0] != (settlementV2AuditFault{Index: 2, Kind: merkle.LedgerFaultWrongLeaf.String()}) {
		t.Fatalf("record %+v", record)
	}
	if fixture.chain.current().pending[settlementV2AuditTestNoID].Seq != 0 {
		t.Fatal("the challenge did not delete the root")
	}
	fixture.mustPass(t)
	if len(fixture.chain.accepted()) != 1 {
		t.Fatal("a deleted root was challenged again")
	}
}

// VA-4: objects the operator has not published are waited for until commit
// + CHALLENGE_BLOCKS/2 (7,200 blocks on mainnet); then the audit refuses
// approval and demands every account of a small F, each paying the demand
// fee with the account's proof against F.
func TestSettlementV2AuditWaitsForMissingObjectsThenDemands(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	objects := ledger.Objects{}
	for ref, data := range p.Objects {
		if ref != p.Commit.LedgerHash {
			objects[ref] = data
		}
	}
	fixture.useScenario(t, f, p.Commit, objects, true, 610)
	for _, head := range []uint64{610, 629} {
		fixture.chain.advanceTo(head)
		fixture.mustPass(t)
		record := fixture.record(t, 2)
		if record.Decision != "" || record.Done || record.Block != head || record.Check != "fetch" || !strings.Contains(record.Error, ledger.ErrObjectUnavailable.Error()) ||
			!slices.Equal(record.Missing, []string{ledger.Ref(p.Commit.LedgerHash).String()}) || len(fixture.chain.accepted()) != 0 {
			t.Fatalf("at block %d: record %+v, %d sent", head, record, len(fixture.chain.accepted()))
		}
	}
	fixture.chain.advanceTo(630)
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	want := settlementV2AuditTestWhole(t, f.Accounts)
	if record.Decision != settlementV2DecisionDemand || !record.Done || record.Outcome != "sent" || record.Doublings != 0 || !slices.Equal(record.Demands, want) {
		t.Fatalf("record %+v", record)
	}
	calls := fixture.sentCalls(t)
	if len(calls) != 4 {
		t.Fatalf("%d demands sent", len(calls))
	}
	for k, call := range calls {
		if call.method != "demandPath" || call.to != settlementV2TestVault || call.value.Cmp(settlementV2TestFee) != 0 {
			t.Fatalf("transaction %d is %s with value %s", k, call.method, call.value)
		}
		requireSettlementV2AuditOpening(t, call, f, 0, want[k])
	}
	if fixture.chain.current().pending[settlementV2AuditTestNoID].OpenDemands != 4 {
		t.Fatal("the demands are not open on chain")
	}
}

// VA-4: objects published before commit + CHALLENGE_BLOCKS/2 are audited as
// any other, and an honest root is approved.
func TestSettlementV2AuditApprovesObjectsPublishedInTime(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 610)
	fixture.mustPass(t)
	if record := fixture.record(t, 2); record.Decision != "" || len(record.Missing) != 1 {
		t.Fatalf("record %+v", record)
	}
	fixture.objects.publish(p.Objects)
	fixture.chain.advanceTo(629)
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	if record := fixture.record(t, 2); record.Decision != settlementV2DecisionApprove || record.Missing != nil {
		t.Fatalf("record %+v", record)
	}
}

// VA-4: objects that are all present but do not reproduce the root need no
// wait: the audit demands at once.
func TestSettlementV2AuditDemandsUnreproducibleRootAtOnce(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	commit := p.Commit
	commit.Root[0] ^= 1
	fixture.useScenario(t, f, commit, p.Objects, true, 610)
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionDemand || record.Check != "tree" || record.Missing != nil || len(fixture.sentCalls(t)) != 4 {
		t.Fatalf("record %+v, %d sent", record, len(fixture.chain.accepted()))
	}
}

// VA-4: the demand sample of a large F is 256 accounts drawn by value with
// randomness read after the commit was seen: exactly ledger.DemandSet over
// F's accounts with the audit's random stream, which always draws the
// account holding half of F's value.
func TestSettlementV2AuditDemandSampleIsWeightedFloor(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, sender := fixture.useLargeScenario(t, 600)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
	fixture.mustPass(t)
	want, err := ledger.DemandSet(f.Accounts, nil, 0, nil, settlementV2AuditTestRandom())
	if err != nil {
		t.Fatal(err)
	}
	record := fixture.record(t, 2)
	if len(want) != ledger.DemandSample || !slices.Equal(record.Demands, want) || !slices.Contains(want, 0) || record.Doublings != 0 || !record.Done {
		t.Fatalf("demand set of %d, record %v", len(want), record.Demands)
	}
	requireSettlementV2AuditDemands(t, sender, f, 0, want)
}

// VA-4: an F of 64 accounts is demanded whole: the sample takes every
// account, in the order drawn, when it holds fewer than 256.
func TestSettlementV2AuditDemandSampleTakesSmallRootWhole(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, sender := fixture.useLargeScenario(t, 64)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
	fixture.mustPass(t)
	all := settlementV2AuditTestWhole(t, f.Accounts)
	if record := fixture.record(t, 2); !slices.Equal(record.Demands, all) || slices.IsSorted(all) {
		t.Fatalf("demanded %v", record.Demands)
	}
	requireSettlementV2AuditDemands(t, sender, f, 0, all)
}

// VA-4: each earlier pending root over the same F that was deleted or
// replaced while a demand was open doubles the sample, and its unanswered
// indexes are demanded again. A root replaced with every demand answered,
// events before F was finalized, and an answer of the same index proven
// against another root do not count.
func TestSettlementV2AuditDemandSampleDoublesAfterFailedRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, sender := fixture.useLargeScenario(t, 600)
	id, demander := big.NewInt(settlementV2AuditTestNoID), common.Address{0xd1}
	vault := func(name string, block uint64, arguments ...any) {
		fixture.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, name, block, append([]any{id}, arguments...)...)
	}
	// Before F's finalization at 460, in the same log span: a failed root
	// over an older root.
	vault("PathDemanded", 450, uint64(9), big.NewInt(0), big.NewInt(11), demander)
	vault("DistributionDeleted", 451, uint64(9), big.NewInt(0), big.NewInt(11), uint8(1), big.NewInt(0), demander)
	// Seq 2 is deleted with index 3 open; seq 3 is replaced by seq 4 with
	// every demand answered.
	fixture.committed(t, 605, 2, p.Commit, 0)
	vault("PathDemanded", 610, uint64(2), big.NewInt(0), big.NewInt(3), demander)
	vault("PathDemanded", 611, uint64(2), big.NewInt(0), big.NewInt(4), demander)
	vault("PathAnswered", 612, uint64(2), big.NewInt(0), big.NewInt(4), demander)
	// An opening proven against another root answers another demand.
	vault("PathAnswered", 615, uint64(2), big.NewInt(1), big.NewInt(3), demander)
	vault("DistributionDeleted", 620, uint64(2), big.NewInt(0), big.NewInt(4), uint8(2), big.NewInt(0), demander)
	fixture.committed(t, 1405, 3, p.Commit, 0)
	vault("PathDemanded", 1410, uint64(3), big.NewInt(0), big.NewInt(5), demander)
	vault("PathAnswered", 1411, uint64(3), big.NewInt(0), big.NewInt(5), demander)
	fixture.committed(t, 2405, 4, p.Commit, 3)
	fixture.useScenarioAt(t, f, p.Commit, ledger.Objects{}, true, 4, 2405, 2430)
	fixture.mustPass(t)
	want, err := ledger.DemandSet(f.Accounts, nil, 1, []uint64{3}, settlementV2AuditTestRandom())
	if err != nil {
		t.Fatal(err)
	}
	record := fixture.record(t, 4)
	if record.Doublings != 1 || !slices.Equal(record.Demands, want) || !slices.Contains(want, 3) || len(want) < 2*ledger.DemandSample {
		t.Fatalf("doublings %d, %d demanded", record.Doublings, len(record.Demands))
	}
	requireSettlementV2AuditDemands(t, sender, f, 0, want)
}

// VA-4: a pending root over the same F replaced while one of its demands was
// open doubles the sample as a deleted one does, and its unanswered index is
// demanded again.
func TestSettlementV2AuditDemandSampleDoublesAfterReplacedRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, sender := fixture.useLargeScenario(t, 600)
	fixture.committed(t, 605, 2, p.Commit, 0)
	fixture.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "PathDemanded", 610, big.NewInt(settlementV2AuditTestNoID), uint64(2), big.NewInt(0), big.NewInt(7), common.Address{0xd1})
	fixture.committed(t, 1405, 3, p.Commit, 2)
	fixture.useScenarioAt(t, f, p.Commit, ledger.Objects{}, true, 3, 1405, 1430)
	fixture.mustPass(t)
	want := settlementV2AuditTestDemandSet(t, f.Accounts, 1, []uint64{7})
	record := fixture.record(t, 3)
	if record.Doublings != 1 || !slices.Equal(record.Demands, want) || !slices.Contains(want, 7) || len(want) < 2*ledger.DemandSample {
		t.Fatalf("doublings %d, %d demanded", record.Doublings, len(record.Demands))
	}
	requireSettlementV2AuditDemands(t, sender, f, 0, want)
}

// settlementV2AuditTestDemandSet is the demand set the audit draws with its
// random stream: ledger.DemandSet over accounts with the faulted indexes,
// the value-weighted sample over all of them in the order drawn, then each
// index of again and then each faulted index the sample did not draw.
func settlementV2AuditTestDemandSet(t *testing.T, accounts []merkle.LedgerAccount, doublings uint, again []uint64, faults ...uint64) []uint64 {
	t.Helper()
	faulted := make([]ledger.Fault, len(faults))
	for k, index := range faults {
		faulted[k] = ledger.Fault{Index: index, Kind: merkle.LedgerFaultWrongLeaf}
	}
	set, err := ledger.DemandSet(accounts, faulted, doublings, again, settlementV2AuditTestRandom())
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// settlementV2AuditTestWhole is the demand set the audit draws over a root of
// fewer than ledger.DemandSample accounts: every account, in the order its
// random stream draws them.
func settlementV2AuditTestWhole(t *testing.T, accounts []merkle.LedgerAccount) []uint64 {
	t.Helper()
	set := settlementV2AuditTestDemandSet(t, accounts, 0, nil)
	for i := range accounts {
		if len(set) != len(accounts) || !slices.Contains(set, uint64(i)) {
			t.Fatalf("the sample of %d accounts %v lacks account %d", len(accounts), set, i)
		}
	}
	return set
}

// VA-4: rows that show a fault are always demanded, whatever the sample
// draws: here a lowered account and a withheld one, under a commitment the
// published rows do not reproduce.
func TestSettlementV2AuditDemandsFaultRows(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, sender := fixture.useLargeScenario(t, 600)
	pub := settlementV2AuditTestPublish(t, p)
	accounts := slices.Clone(p.Accounts[:599])
	accounts[598].Cumulative.SubUint64(&accounts[598].Cumulative, 1)
	chunks, err := ledger.EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	pub.header.Accounts = pub.put(chunks)
	commit := pub.seal(t)
	fixture.useScenario(t, f, commit, pub.objects, true, 610)
	fixture.mustPass(t)
	want := settlementV2AuditTestDemandSet(t, f.Accounts, 0, nil, 598, 599)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionDemand || !slices.Equal(record.Demands, want) || !slices.Contains(want, 598) || !slices.Contains(want, 599) || len(want) < ledger.DemandSample ||
		len(record.Faults) != 2 || record.Faults[1] != (settlementV2AuditFault{Index: 599, Kind: merkle.LedgerFaultWrongLeaf.String()}) {
		t.Fatalf("record faults %v, %d demanded", record.Faults, len(record.Demands))
	}
	requireSettlementV2AuditDemands(t, sender, f, 0, want)
}

// VA-4, G2: published rows that fault every account of F neither shrink nor
// order the sample: the first demands are exactly the value-weighted sample
// over all of F's accounts, which always draws the account holding half of
// F's value, and every other account follows in index order. The demands
// sent before the challenge window closes are a sample even when the whole
// set cannot be sent.
func TestSettlementV2AuditDemandSampleLeadsRowsFaultingEveryAccount(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, sender := fixture.useLargeScenario(t, 600)
	pub := settlementV2AuditTestPublish(t, p)
	accounts := slices.Clone(p.Accounts)
	for i := range accounts {
		accounts[i].Coldkey = settlementV2AuditTestColdkey(10_000 + i)
	}
	chunks, err := ledger.EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	pub.header.Accounts = pub.put(chunks)
	commit := pub.seal(t)
	fixture.useScenario(t, f, commit, pub.objects, true, 610)
	fixture.mustPass(t)
	sample, err := ledger.DemandSet(f.Accounts, nil, 0, nil, settlementV2AuditTestRandom())
	if err != nil {
		t.Fatal(err)
	}
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionDemand || len(record.Faults) != len(f.Accounts) || len(sample) != ledger.DemandSample || !slices.Contains(sample, 0) ||
		len(record.Demands) != len(f.Accounts) || !slices.Equal(record.Demands[:len(sample)], sample) {
		t.Fatalf("%d faults, %d demanded", len(record.Faults), len(record.Demands))
	}
	rest := record.Demands[len(sample):]
	for k, index := range rest {
		if slices.Contains(sample, index) || k > 0 && index <= rest[k-1] {
			t.Fatalf("demand %d after the sample is account %d", len(sample)+k, index)
		}
	}
	requireSettlementV2AuditDemands(t, sender, f, 0, record.Demands)
}

// VA-4: the demand set is drawn once. A pass whose sends fail keeps the
// decision; a restarted validator with fresh randomness sends exactly the
// recorded set.
func TestSettlementV2AuditDemandSetDrawnOnce(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, sender := fixture.useLargeScenario(t, 600)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
	sender.err = errors.New("synthetic send failure")
	if err := fixture.pass(t); !errors.Is(err, sender.err) {
		t.Fatalf("pass: %v", err)
	}
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionDemand || record.Done || len(record.Demands) != ledger.DemandSample {
		t.Fatalf("record %+v", record)
	}
	fixture.restart(t)
	fixture.audit.random = rand.NewChaCha8([32]byte{0x01})
	again := &settlementV2AuditTestSender{}
	fixture.audit.sender = again
	fixture.mustPass(t)
	requireSettlementV2AuditDemands(t, again, f, 0, record.Demands)
	if record := fixture.record(t, 2); !record.Done || record.Outcome != "sent" {
		t.Fatalf("record %+v", record)
	}
}

// VA-4, G2: the challenge window ends the demands cleanly. The auditor's
// sender sends a demand only while the next block is inside the window, so
// with the head two blocks before the window's last block, the demands that
// land are the sample's first two draws, in the order drawn, and the rest
// of the set is not sent; the decision ends with the closed window as its
// outcome, and a later pass sends nothing.
func TestSettlementV2AuditDemandsStopWhenWindowCloses(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f, p, _ := fixture.useLargeScenario(t, 600)
	fixture.restart(t)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 652)
	fixture.mustPass(t)
	want := settlementV2AuditTestDemandSet(t, f.Accounts, 0, nil)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionDemand || !record.Done || record.Outcome != ErrSettlementV2ChallengeWindowClosed.Error() || !slices.Equal(record.Demands, want) || slices.IsSorted(want) {
		t.Fatalf("record %+v", record)
	}
	calls := fixture.sentCalls(t)
	if len(calls) != 2 || fixture.chain.current().pending[settlementV2AuditTestNoID].OpenDemands != 2 {
		t.Fatalf("%d demands sent", len(calls))
	}
	for k, call := range calls {
		requireSettlementV2AuditOpening(t, call, f, 0, want[k])
	}
	fixture.mustPass(t)
	if len(fixture.chain.accepted()) != 2 {
		t.Fatal("a pass after the window closed sent again")
	}
}

// VA-4: a sender's refusal that ends the decision is recorded as its
// outcome; any other failure leaves the decision to be sent again.
func TestSettlementV2AuditSendOutcomes(t *testing.T) {
	t.Parallel()
	for _, final := range []error{ErrSettlementV2RootNotPending, ErrSettlementV2ApprovalClosed, ErrSettlementV2ChallengeWindowClosed, ErrSettlementV2RootFinalized, ErrSettlementV2ChallengeAnswered} {
		t.Run(final.Error(), func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			f := settlementV2AuditTestF(t)
			p := settlementV2AuditTestP(t, f)
			fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
			sender := &settlementV2AuditTestSender{err: ErrSettlementV2SendPending}
			fixture.audit.sender = sender
			if err := fixture.pass(t); !errors.Is(err, ErrSettlementV2SendPending) {
				t.Fatalf("pass: %v", err)
			}
			if record := fixture.record(t, 2); record.Decision != settlementV2DecisionApprove || record.Done {
				t.Fatalf("record %+v", record)
			}
			sender.err = final
			fixture.mustPass(t)
			if record := fixture.record(t, 2); !record.Done || record.Outcome != final.Error() || len(sender.approvals) != 2 {
				t.Fatalf("record %+v after %d approvals", record, len(sender.approvals))
			}
		})
	}
}

// VA-4: without --ledger_auditor_key_file the audit decides and records,
// sends nothing, stays undone and audits a decided root no more; a validator
// restarted with the key sends the recorded decision.
func TestSettlementV2AuditWithoutAuditorKey(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
	fixture.audit.sender = nil
	fixture.mustPass(t)
	if record := fixture.record(t, 2); record.Decision != settlementV2DecisionDemand || record.Done || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v", record)
	}
	fetched := fixture.objects.fetched()
	fixture.mustPass(t)
	if fixture.objects.fetched() != fetched {
		t.Fatal("a decided root was audited again")
	}
	fixture.restart(t)
	fixture.mustPass(t)
	calls := fixture.sentCalls(t)
	if len(calls) != 4 {
		t.Fatalf("%d demands sent", len(calls))
	}
	for k, index := range settlementV2AuditTestWhole(t, f.Accounts) {
		requireSettlementV2AuditOpening(t, calls[k], f, 0, index)
	}
}

// VA-3: through the validator's audit worker: the loop's first pass
// approves the pending root before its deadline, and the worker stops with
// its context.
func TestSettlementV2AuditLoopApproves(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fixture.audit.worker()(ctx) }()
	approval := settlementV2TestApproval{noID: settlementV2AuditTestNoID, seq: 2, version: 1, auditor: fixture.auditor()}
	for deadline := time.Now().Add(time.Minute); !fixture.chain.current().approved[approval]; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the worker did not approve")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("the worker ended with %v", err)
	}
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
}

// VA-3: the first root and the next, each approved through the audit with
// exactly its commitment before its deadline: the first root has no
// previous root to protect, the next is audited against it.
func TestSettlementV2AuditApprovesEachRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.objects.publish(f.Objects)
	fixture.objects.publish(p.Objects)
	fixture.setPool(settlementV2AuditTestPending(1, f.Commit, 405))
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 1, f.Commit)
	if record := fixture.record(t, 1); record.Decision != settlementV2DecisionApprove || record.Done {
		t.Fatalf("record %+v", record)
	}
	fixture.chain.advanceTo(459)
	fixture.setPool(stabi.ISTSettlementVault2Pending{Total: new(big.Int)}, f.Commit)
	fixture.committed(t, 605, 2, p.Commit, 0)
	fixture.useScenarioAt(t, f, p.Commit, ledger.Objects{}, false, 2, 605, 610)
	fixed := fixture.objects.fetched()
	fixture.mustPass(t)
	calls := fixture.sentCalls(t)
	fixture.requireApproved(t, calls[1:], 2, p.Commit)
	// F's objects were audited with F: only P's are fetched now.
	if fetched := fixture.objects.fetched() - fixed; fetched != len(p.Objects) {
		t.Fatalf("fetched %d objects, want P's %d", fetched, len(p.Objects))
	}
}

// VA-3: an approval is idempotent across restarts: a validator stopped
// after signing reconciles the recorded transaction and never signs another.
func TestSettlementV2AuditApprovalIdempotentAcrossRestart(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
	fixture.chain.lock.Lock()
	fixture.chain.hold, fixture.chain.loseAck = true, true
	fixture.chain.lock.Unlock()
	if err := fixture.pass(t); err == nil {
		t.Fatal("an unacknowledged approval passed")
	}
	fixture.chain.lock.Lock()
	fixture.chain.loseAck = false
	fixture.chain.lock.Unlock()
	fixture.restart(t)
	if err := fixture.pass(t); !errors.Is(err, ErrSettlementV2SendPending) {
		t.Fatalf("restart while unmined: %v", err)
	}
	if record := fixture.record(t, 2); record.Decision != settlementV2DecisionApprove || record.Done {
		t.Fatalf("record %+v", record)
	}
	fixture.chain.mineHeld()
	fixture.restart(t)
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	if record := fixture.record(t, 2); record.Done || record.Outcome != "" {
		t.Fatalf("record %+v", record)
	}
	fixture.restart(t)
	fixture.mustPass(t)
	if len(fixture.chain.accepted()) != 1 {
		t.Fatal("a restart signed a second approval")
	}
}

// VA-3: an approval counts only at the auditor set version it was sent at.
// After the owner changes the auditor set before end(e+1), the next pass
// approves the root again, counted at the new version, and later passes send
// nothing. A change after end(e+1) ends the decision with the approval
// deadline as its outcome.
func TestSettlementV2AuditApprovesAgainAfterAuditorSetChange(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
	fixture.mustPass(t)
	fixture.chain.mutate(func(state *settlementV2TestState) { state.version = 2 })
	fixture.mustPass(t)
	fixture.mustPass(t)
	calls := fixture.sentCalls(t)
	if len(calls) != 2 {
		t.Fatalf("%d approvals sent, want 2", len(calls))
	}
	fixture.requireApproved(t, calls[:1], 2, p.Commit)
	fixture.requireApproved(t, calls[1:], 2, p.Commit)
	for _, version := range []uint64{1, 2} {
		if !fixture.chain.current().approved[settlementV2TestApproval{noID: settlementV2AuditTestNoID, seq: 2, version: version, auditor: fixture.auditor()}] {
			t.Fatalf("no approval counts at version %d", version)
		}
	}
	if record := fixture.record(t, 2); record.Done {
		t.Fatalf("record %+v", record)
	}
	fixture.chain.advanceTo(698)
	fixture.chain.mutate(func(state *settlementV2TestState) { state.version = 3 })
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	if !record.Done || record.Outcome != ErrSettlementV2ApprovalClosed.Error() || len(fixture.chain.accepted()) != 2 {
		t.Fatalf("record %+v, %d sent", record, len(fixture.chain.accepted()))
	}
}

// VA-3: an approval that can no longer land before end(e+1) is not sent;
// the decision ends with that outcome.
func TestSettlementV2AuditApprovalClosedAtDeadline(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 699)
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionApprove || !record.Done || record.Outcome != ErrSettlementV2ApprovalClosed.Error() || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v, %d sent", record, len(fixture.chain.accepted()))
	}
}

// VA-8: a challenge is idempotent: a root another challenger deleted between
// the audit's read and its send counts as success, and nothing is sent.
func TestSettlementV2AuditChallengeOfDeletedRootSucceeds(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	pub := settlementV2AuditTestPublish(t, p)
	accounts := slices.Clone(p.Accounts)
	accounts[0].Coldkey = settlementV2AuditTestColdkey(99)
	pub.setAccounts(t, accounts)
	commit := pub.seal(t)
	fixture.useScenario(t, f, commit, pub.objects, true, 610)
	fixture.chain.before = func(name string) {
		if name == "checkPath" {
			fixture.chain.before = nil
			fixture.setPool(stabi.ISTSettlementVault2Pending{Total: new(big.Int)}, f.Commit)
		}
	}
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionChallenge || !record.Done || record.Outcome != "sent" || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v, %d sent", record, len(fixture.chain.accepted()))
	}
}

// VA-8: an approval this auditor's key already counts for the pending root
// at the current auditor set version is success: the audit sends nothing.
func TestSettlementV2AuditApprovalAlreadyCountedSucceeds(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
	fixture.chain.mutate(func(state *settlementV2TestState) {
		state.approved[settlementV2TestApproval{noID: settlementV2AuditTestNoID, seq: 2, version: state.version, auditor: fixture.auditor()}] = true
	})
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionApprove || record.Done || record.Outcome != "" || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v, %d sent", record, len(fixture.chain.accepted()))
	}
}

// VA-8: demands are keyed by seq, root index and account index. An index
// another demander already demanded against the root the audit protects is
// success and is not demanded again; the same index demanded against another
// finalized root does not count, so it is demanded against this one.
func TestSettlementV2AuditDemandAlreadyDemandedSucceeds(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	r, _, _ := fixture.useOlderScenario(t, true, nil)
	id, watcher := big.NewInt(settlementV2AuditTestNoID), common.Address{0xd2}
	for _, demand := range []settlementV2TestDemand{{noID: settlementV2AuditTestNoID, seq: 3, rootIndex: 0, index: 2}, {noID: settlementV2AuditTestNoID, seq: 3, rootIndex: 1, index: 1}} {
		fixture.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "PathDemanded", 710, id, demand.seq, new(big.Int).SetUint64(demand.rootIndex), new(big.Int).SetUint64(demand.index), watcher)
		fixture.chain.mutate(func(state *settlementV2TestState) {
			state.demanded[demand] = settlementV2TestDemandOpen
			pending := state.pending[settlementV2AuditTestNoID]
			pending.OpenDemands++
			state.pending[settlementV2AuditTestNoID] = pending
		})
	}
	fixture.chain.advanceTo(730)
	fixture.mustPass(t)
	record := fixture.record(t, 3)
	want := settlementV2AuditTestWhole(t, r.Accounts)
	if record.Decision != settlementV2DecisionDemand || record.RootIndex != 0 || record.Doublings != 0 || !record.Done || record.Outcome != "sent" || !slices.Equal(record.Demands, want) {
		t.Fatalf("record %+v", record)
	}
	calls := fixture.sentCalls(t)
	if len(calls) != 3 {
		t.Fatalf("%d demands sent, want 3", len(calls))
	}
	for k, index := range slices.DeleteFunc(want, func(index uint64) bool { return index == 2 }) {
		requireSettlementV2AuditOpening(t, calls[k], r, 0, index)
	}
	state := fixture.chain.current()
	for index := uint64(0); index < 4; index++ {
		if state.demanded[settlementV2TestDemand{noID: settlementV2AuditTestNoID, seq: 3, rootIndex: 0, index: index}] != settlementV2TestDemandOpen {
			t.Fatalf("account %d of root 0 is not demanded", index)
		}
	}
	if state.pending[settlementV2AuditTestNoID].OpenDemands != 5 {
		t.Fatalf("%d open demands, want 5", state.pending[settlementV2AuditTestNoID].OpenDemands)
	}
}

// VA-8: a root replaced between the audit's read and its demands ends the
// decision: no demand is sent, and the outcome records why.
func TestSettlementV2AuditDemandOfReplacedRootEnds(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
	fixture.chain.before = func(name string) {
		if name == "demandFeeWei" {
			fixture.chain.before = nil
			fixture.setPool(settlementV2AuditTestPending(3, p.Commit, 629), f.Commit)
		}
	}
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionDemand || !record.Done || record.Outcome != ErrSettlementV2RootNotPending.Error() || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v, %d sent", record, len(fixture.chain.accepted()))
	}
}

// VA-4: a demand set's sends are idempotent: after a send the endpoint
// refused, a restarted validator rebroadcasts that exact transaction, sends
// the rest, and never sends a demand twice.
func TestSettlementV2AuditDemandsResumeAfterFailedSend(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
	fixture.chain.lock.Lock()
	fixture.chain.failures["eth_sendRawTransaction"] = 3
	fixture.chain.lock.Unlock()
	if err := fixture.pass(t); err == nil {
		t.Fatal("a pass with a refused send succeeded")
	}
	if len(fixture.chain.accepted()) != 2 {
		t.Fatalf("%d demands accepted before the refusal", len(fixture.chain.accepted()))
	}
	fixture.chain.lock.Lock()
	delete(fixture.chain.failures, "eth_sendRawTransaction")
	fixture.chain.lock.Unlock()
	fixture.restart(t)
	for attempt := 0; ; attempt++ {
		err := fixture.pass(t)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrSettlementV2SendPending) || attempt > 1 {
			t.Fatalf("pass %d: %v", attempt, err)
		}
	}
	calls := fixture.sentCalls(t)
	if len(calls) != 4 || fixture.chain.current().pending[settlementV2AuditTestNoID].OpenDemands != 4 {
		t.Fatalf("%d demands accepted", len(calls))
	}
	for k, index := range settlementV2AuditTestWhole(t, f.Accounts) {
		requireSettlementV2AuditOpening(t, calls[k], f, 0, index)
	}
}

// useOlderScenario puts two finalized roots on chain: R, root 0, for epoch 3
// (the standard F), and F, root 1, for epochs 4 and 5 (the standard P),
// finalized at block 660, with mutate applied to F as vault2 records it. The
// pending root P covers epoch 6 and is committed at block 705 as seq 3; the
// head is block 710. Custody holds R's objects when holdR; F's and P's are
// not published.
func (self *settlementV2AuditTestFixture) useOlderScenario(t *testing.T, holdR bool, mutate func(*ledger.Root)) (r, f, p *ledger.Ledger) {
	t.Helper()
	return self.useOlderScenarioOver(t, settlementV2AuditTestF(t), settlementV2AuditTestFunding(), holdR, mutate)
}

// useOlderScenarioOver is useOlderScenario with root 0 r, built over funded,
// which also funds F's epochs; epoch 6 adds 500,000.
func (self *settlementV2AuditTestFixture) useOlderScenarioOver(t *testing.T, r *ledger.Ledger, funded map[uint64]uint256.Int, holdR bool, mutate func(*ledger.Root)) (*ledger.Ledger, *ledger.Ledger, *ledger.Ledger) {
	t.Helper()
	five := funded[5]
	funded[6] = *new(uint256.Int).AddUint64(&five, 500_000)
	self.useFunding(funded)
	f := settlementV2AuditTestBuild(t, r, []ledger.EpochInput{{Epoch: 4, Rows: settlementV2AuditTestRows4()}, {Epoch: 5, Rows: settlementV2AuditTestRows5()}}, funded)
	p := settlementV2AuditTestBuild(t, f, []ledger.EpochInput{{Epoch: 6, Rows: []protocol.LedgerUsageRow{settlementV2AuditTestRow(3, 2, 100, false)}}}, funded)
	if holdR {
		self.holdF(t, r)
	}
	finalized := f.Commit
	if mutate != nil {
		mutate(&finalized)
	}
	self.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "DistributionFinalized", 660, big.NewInt(settlementV2AuditTestNoID), uint64(2), big.NewInt(1), finalized.Epoch, finalized.Root, finalized.Total.ToBig(), finalized.LeafCount, [32]byte(finalized.LedgerHash))
	self.committed(t, 705, 3, p.Commit, 0)
	self.chain.advanceTo(709)
	self.setPool(settlementV2AuditTestPending(3, p.Commit, 705), r.Commit, finalized)
	return r, f, p
}

// commitOlderPending replaces the older scenario's pending root with one
// whose published accounts are accounts, committed to their own tree when
// reproduce, and otherwise under P's commitment, which they do not
// reproduce. It publishes the root's header and account chunks.
func (self *settlementV2AuditTestFixture) commitOlderPending(t *testing.T, r, f, p *ledger.Ledger, accounts []merkle.LedgerAccount, reproduce bool) ledger.Root {
	t.Helper()
	pub := settlementV2AuditTestPublish(t, p)
	if reproduce {
		pub.setAccounts(t, accounts)
	} else {
		chunks, err := ledger.EncodeAccountChunks(accounts)
		if err != nil {
			t.Fatal(err)
		}
		pub.header.Accounts = pub.put(chunks)
	}
	commit := pub.seal(t)
	self.objects.publish(pub.objects)
	self.setPool(settlementV2AuditTestPending(3, commit, 705), r.Commit, f.Commit)
	return commit
}

// G2, VA-4: when the latest finalized root's objects are withheld, the audit
// waits until commit + CHALLENGE_BLOCKS/2, then demands against the latest
// older finalized root whose objects custody holds: each demand names that
// root's index and proves its account against it.
func TestSettlementV2AuditDemandsAgainstOlderRootWhenLatestWithheld(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	r, f, _ := fixture.useOlderScenario(t, true, nil)
	fixture.mustPass(t)
	if record := fixture.record(t, 3); record.Decision != "" || record.Check != "fetch" || !slices.Equal(record.Missing, []string{ledger.Ref(f.Commit.LedgerHash).String()}) {
		t.Fatalf("record %+v", record)
	}
	fixture.chain.advanceTo(730)
	fixture.mustPass(t)
	record := fixture.record(t, 3)
	want := settlementV2AuditTestWhole(t, r.Accounts)
	if record.Decision != settlementV2DecisionDemand || record.RootIndex != 0 || !record.Done || record.Outcome != "sent" || !slices.Equal(record.Demands, want) {
		t.Fatalf("record %+v", record)
	}
	calls := fixture.sentCalls(t)
	if len(calls) != len(r.Accounts) {
		t.Fatalf("%d demands sent", len(calls))
	}
	for k, call := range calls {
		requireSettlementV2AuditOpening(t, call, r, 0, want[k])
	}
}

// G2, VA-4: a latest finalized root whose objects do not rebuild it is
// passed over at once for the older root custody holds.
func TestSettlementV2AuditDemandsAgainstOlderRootWhenLatestInvalid(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	r, f, _ := fixture.useOlderScenario(t, true, func(root *ledger.Root) { root.Total.AddUint64(&root.Total, 1) })
	for ref, data := range f.Objects {
		if err := fixture.audit.custody.Keep(t.Context(), ref, data); err != nil {
			t.Fatal(err)
		}
	}
	sender := &settlementV2AuditTestSender{}
	fixture.audit.sender = sender
	fixture.mustPass(t)
	record := fixture.record(t, 3)
	if record.Decision != settlementV2DecisionDemand || record.RootIndex != 0 || record.Check != "previous root" || !strings.Contains(record.Error, ledger.ErrPreviousNotTight.Error()) {
		t.Fatalf("record %+v", record)
	}
	requireSettlementV2AuditDemands(t, sender, r, 0, settlementV2AuditTestWhole(t, r.Accounts))
}

// G2, VA-4: without a finalized root whose objects custody holds, a root
// whose previous root cannot be verified is refused, and nothing is sent.
func TestSettlementV2AuditRefusesWithoutVerifiedRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	fixture.useOlderScenario(t, false, nil)
	fixture.chain.advanceTo(730)
	fixture.mustPass(t)
	record := fixture.record(t, 3)
	if record.Decision != settlementV2DecisionRefuse || !record.Done || record.Outcome != "refused" || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v", record)
	}
}

// G2: a demand decided against an older root is sent from custody after a
// restart; if custody no longer holds that root, nothing is sent and the
// pass fails, to be retried.
func TestSettlementV2AuditOlderRootSentAfterRestart(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	r, _, _ := fixture.useOlderScenario(t, true, nil)
	fixture.chain.advanceTo(730)
	failing := &settlementV2AuditTestSender{err: errors.New("synthetic send failure")}
	fixture.audit.sender = failing
	if err := fixture.pass(t); !errors.Is(err, failing.err) {
		t.Fatalf("pass: %v", err)
	}
	fixture.restart(t)
	again := &settlementV2AuditTestSender{}
	fixture.audit.sender = again
	fixture.mustPass(t)
	requireSettlementV2AuditDemands(t, again, r, 0, settlementV2AuditTestWhole(t, r.Accounts))

	lost := newSettlementV2AuditTestFixture(t)
	r, _, _ = lost.useOlderScenario(t, true, nil)
	lost.chain.advanceTo(730)
	lost.audit.sender = failing
	if err := lost.pass(t); !errors.Is(err, failing.err) {
		t.Fatalf("pass: %v", err)
	}
	if err := os.Remove(lost.audit.custody.path("objects", hex.EncodeToString(r.Commit.LedgerHash[:]))); err != nil {
		t.Fatal(err)
	}
	if err := lost.pass(t); err == nil || !strings.Contains(err.Error(), "no longer rebuilds the trees of its demand against root 0") {
		t.Fatalf("pass without the root in custody: %v", err)
	}
}

// G2, VA-4: when the latest finalized root's objects are withheld, the audit
// reads the pending root's published accounts itself and compares them with
// the older root it protects. A fault they prove while reproducing the
// pending root is challenged: checkPath names that root's index and carries
// its account, with its proof against it, and the pending root's path, which
// shows the fault at the leaf.
func TestSettlementV2AuditChallengesAgainstOlderRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	r, f, p := fixture.useOlderScenario(t, true, nil)
	accounts := slices.Clone(p.Accounts)
	accounts[1].Coldkey = settlementV2AuditTestColdkey(99)
	commit := fixture.commitOlderPending(t, r, f, p, accounts, true)
	fixture.chain.faults[1] = true
	fixture.chain.advanceTo(730)
	fixture.mustPass(t)
	record := fixture.record(t, 3)
	if record.Decision != settlementV2DecisionChallenge || record.RootIndex != 0 || !record.Done || record.Outcome != "sent" ||
		len(record.Faults) != 1 || record.Faults[0] != (settlementV2AuditFault{Index: 1, Kind: merkle.LedgerFaultWrongLeaf.String()}) {
		t.Fatalf("record %+v", record)
	}
	calls := fixture.sentCalls(t)
	if len(calls) != 1 || calls[0].method != "checkPath" || calls[0].to != settlementV2TestVault {
		t.Fatalf("sent %d transactions, want one checkPath", len(calls))
	}
	requireSettlementV2AuditOpening(t, calls[0], r, 0, 1)
	account := calls[0].account()
	fault, level, err := merkle.WalkLedgerPath(commit.Root, &commit.Total, commit.LeafCount, 1, account.Coldkey, uint256.MustFromBig(account.Cumulative), calls[0].path())
	if err != nil || fault != merkle.LedgerFaultWrongLeaf || level != 0 {
		t.Fatalf("the path shows %v at level %d: %v", fault, level, err)
	}
	if fixture.chain.current().pending[settlementV2AuditTestNoID].Seq != 0 {
		t.Fatal("the challenge did not delete the root")
	}
}

// G2, VA-4: published accounts that do not reproduce the pending root are
// compared with the older protected root too, and the rows they fault, here
// two lowered and a missing one, are always demanded against it: each one
// the sample over the older root's accounts did not draw follows it. The
// pending root's objects, kept when read, are not fetched again.
func TestSettlementV2AuditDemandsFaultRowsAgainstOlderRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	large, funded := settlementV2AuditTestLargeF(t, 600)
	r, f, p := fixture.useOlderScenarioOver(t, large, funded, true, nil)
	sender := &settlementV2AuditTestSender{}
	fixture.audit.sender = sender
	accounts := slices.Clone(p.Accounts[:599])
	for _, i := range []int{1, 2} {
		accounts[i].Cumulative.SubUint64(&r.Accounts[i].Cumulative, 1)
	}
	commit := fixture.commitOlderPending(t, r, f, p, accounts, false)
	fixture.chain.advanceTo(730)
	fixture.chain.lock.Lock()
	fixture.chain.failures["eth_getLogs"] = fixture.chain.counts["eth_getLogs"] + 1
	fixture.chain.lock.Unlock()
	if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "synthetic eth_getLogs failure") {
		t.Fatalf("pass: %v", err)
	}
	fixture.requireUndecided(t, 3)
	fixture.chain.lock.Lock()
	delete(fixture.chain.failures, "eth_getLogs")
	fixture.chain.lock.Unlock()
	fixture.mustPass(t)
	want := settlementV2AuditTestDemandSet(t, r.Accounts, 0, nil, 1, 2, 599)
	record := fixture.record(t, 3)
	if record.Decision != settlementV2DecisionDemand || record.RootIndex != 0 || !record.Done || len(want) < ledger.DemandSample || !slices.Contains(want[:ledger.DemandSample], 0) || !slices.Equal(record.Demands, want) ||
		!slices.Contains(want, 1) || !slices.Contains(want, 2) || !slices.Contains(want, 599) ||
		!slices.Equal(record.Faults, []settlementV2AuditFault{{Index: 1, Kind: merkle.LedgerFaultLowerValue.String()}, {Index: 2, Kind: merkle.LedgerFaultLowerValue.String()}, {Index: 599, Kind: merkle.LedgerFaultWrongLeaf.String()}}) {
		t.Fatalf("record %+v", record)
	}
	requireSettlementV2AuditDemands(t, sender, r, 0, want)
	header, err := ledger.DecodeHeader(fixture.objects.objects[commit.LedgerHash])
	if err != nil {
		t.Fatal(err)
	}
	if n := fixture.objects.fetched(append([]ledger.Ref{commit.LedgerHash}, header.Accounts...)...); n != 1+len(header.Accounts) {
		t.Fatalf("the pending root's objects were fetched %d times", n)
	}
}

// G2: a pending root whose published accounts reproduce it and keep every
// account of the older protected root is refused, as F cannot be verified,
// and nothing is sent: every account the audit can protect is intact.
func TestSettlementV2AuditRefusesIntactRootOverOlderRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	_, _, p := fixture.useOlderScenario(t, true, nil)
	fixture.objects.publish(p.Objects)
	fixture.chain.advanceTo(730)
	fixture.mustPass(t)
	record := fixture.record(t, 3)
	if record.Decision != settlementV2DecisionRefuse || !record.Done || record.Outcome != "refused" || record.Faults != nil || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v, %d sent", record, len(fixture.chain.accepted()))
	}
}

// G2, VA-4: a pending root whose published accounts the audit cannot read
// over an older protected root is demanded against it with no fault forced:
// its header beyond the byte budget, served bytes that do not match its
// reference, a header that does not decode, a chunk withheld, or chunks that
// do not decode. Each object it could not obtain is recorded as missing.
func TestSettlementV2AuditOlderRootDemandsUnreadablePendingAccounts(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*testing.T, *settlementV2AuditTestFixture, *ledger.Ledger, *ledger.Ledger, *ledger.Ledger) []ledger.Ref{
		"header beyond the byte budget": func(t *testing.T, fixture *settlementV2AuditTestFixture, r, f, p *ledger.Ledger) []ledger.Ref {
			fixture.objects.publish(p.Objects)
			fixture.audit.maxBytes = uint64(len(p.Header)) - 1
			return []ledger.Ref{p.Commit.LedgerHash}
		},
		"header bytes not its reference": func(t *testing.T, fixture *settlementV2AuditTestFixture, r, f, p *ledger.Ledger) []ledger.Ref {
			fixture.objects.publish(p.Objects)
			fixture.objects.publish(ledger.Objects{p.Commit.LedgerHash: f.Header})
			return []ledger.Ref{p.Commit.LedgerHash}
		},
		"header not canonical": func(t *testing.T, fixture *settlementV2AuditTestFixture, r, f, p *ledger.Ledger) []ledger.Ref {
			pub := settlementV2AuditTestPublish(t, p)
			commit := pub.sealBytes(bytes.Replace(p.Header, []byte(`"no_id":1`), []byte(`"no_id": 1`), 1))
			fixture.objects.publish(pub.objects)
			fixture.setPool(settlementV2AuditTestPending(3, commit, 705), r.Commit, f.Commit)
			return nil
		},
		"chunk withheld": func(t *testing.T, fixture *settlementV2AuditTestFixture, r, f, p *ledger.Ledger) []ledger.Ref {
			header, err := ledger.DecodeHeader(p.Header)
			if err != nil {
				t.Fatal(err)
			}
			fixture.objects.publish(p.Objects)
			fixture.objects.withdraw(header.Accounts[0])
			return []ledger.Ref{header.Accounts[0]}
		},
		"chunks do not decode": func(t *testing.T, fixture *settlementV2AuditTestFixture, r, f, p *ledger.Ledger) []ledger.Ref {
			pub := settlementV2AuditTestPublish(t, p)
			chunk := pub.objects[pub.header.Accounts[0]]
			pub.header.Accounts = pub.put([][]byte{chunk[:ledger.AccountRowBytes], chunk[ledger.AccountRowBytes:]})
			commit := pub.seal(t)
			fixture.objects.publish(pub.objects)
			fixture.setPool(settlementV2AuditTestPending(3, commit, 705), r.Commit, f.Commit)
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			r, f, p := fixture.useOlderScenario(t, true, nil)
			missing := []string{ledger.Ref(f.Commit.LedgerHash).String()}
			for _, ref := range mutate(t, fixture, r, f, p) {
				missing = append(missing, ref.String())
			}
			fixture.chain.advanceTo(730)
			fixture.mustPass(t)
			record := fixture.record(t, 3)
			want := settlementV2AuditTestWhole(t, r.Accounts)
			if record.Decision != settlementV2DecisionDemand || record.RootIndex != 0 || record.Faults != nil || !slices.Equal(record.Demands, want) || !slices.Equal(record.Missing, missing) {
				t.Fatalf("record %+v, want missing %v", record, missing)
			}
			calls := fixture.sentCalls(t)
			if len(calls) != len(r.Accounts) {
				t.Fatalf("%d demands sent", len(calls))
			}
			for k, call := range calls {
				requireSettlementV2AuditOpening(t, call, r, 0, want[k])
			}
		})
	}
}

// settlementV2AuditTestFetcher runs before ahead of each fetch.
type settlementV2AuditTestFetcher struct {
	ledger.Fetcher
	before func(ledger.Ref)
}

func (self settlementV2AuditTestFetcher) Fetch(ctx context.Context, ref ledger.Ref) (io.ReadCloser, error) {
	self.before(ref)
	return self.Fetcher.Fetch(ctx, ref)
}

// G2: reading the pending root's accounts over an older root fails the pass,
// deciding nothing, when custody cannot read or keep an object or the pass's
// context ends while fetching one.
func TestSettlementV2AuditOlderRootPendingReadFailures(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*settlementV2AuditTestFixture, *ledger.Ledger) {
		fixture := newSettlementV2AuditTestFixture(t)
		_, _, p := fixture.useOlderScenario(t, true, nil)
		fixture.objects.publish(p.Objects)
		fixture.chain.advanceTo(730)
		return fixture, p
	}
	t.Run("custody unreadable", func(t *testing.T) {
		fixture, p := setup(t)
		if err := os.Mkdir(fixture.audit.custody.path("objects", hex.EncodeToString(p.Commit.LedgerHash[:])), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "is a directory") {
			t.Fatalf("an unreadable custody object: %v", err)
		}
		fixture.requireUndecided(t, 3)
	})
	t.Run("custody cannot keep", func(t *testing.T) {
		fixture, p := setup(t)
		fixture.audit.pools[0].objects = settlementV2AuditTestFetcher{Fetcher: fixture.audit.pools[0].objects, before: func(ref ledger.Ref) {
			if ref == p.Commit.LedgerHash {
				settlementV2CustodyTestBlock(t, fixture.audit.custody.path("objects", ""))
			}
		}}
		if err := fixture.pass(t); err == nil {
			t.Fatal("an object custody could not keep was read")
		}
		fixture.requireUndecided(t, 3)
	})
	t.Run("context ends", func(t *testing.T) {
		fixture, p := setup(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		fixture.audit.pools[0].objects = settlementV2AuditTestFetcher{Fetcher: fixture.audit.pools[0].objects, before: func(ref ledger.Ref) {
			if ref == p.Commit.LedgerHash {
				cancel()
			}
		}}
		if err := fixture.audit.audit(ctx, fixture.audit.pools[0]); !errors.Is(err, context.Canceled) {
			t.Fatalf("a pass whose context ended: %v", err)
		}
		fixture.requireUndecided(t, 3)
	})
}
