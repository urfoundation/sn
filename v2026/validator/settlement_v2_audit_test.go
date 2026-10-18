//go:build linux || darwin

// The settlement v2 ledger audit through the full validator path: chain
// state pinned at the finalized head, head bindings from FleetBound events
// and archive bindingAt, objects from the operator's endpoint, durable
// custody, ledger.Verify, the §9.3 decision and the auditor sender.
package validator

import (
	"bytes"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// sentCalls decodes the auditor's accepted transactions as calls of to's ABI.
func (self *settlementV2AuditTestFixture) sentCalls(t *testing.T) []settlementV2AuditTestCall {
	t.Helper()
	var calls []settlementV2AuditTestCall
	for _, transaction := range self.chain.accepted() {
		sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(settlementV2TestChainID)), transaction)
		if err != nil || sender != self.auditor() {
			t.Fatalf("transaction from %s: %v", sender, err)
		}
		contractABI := settlementV2TestABI(*transaction.To())
		method, err := contractABI.MethodById(transaction.Data())
		if err != nil {
			t.Fatal(err)
		}
		arguments, err := method.Inputs.Unpack(transaction.Data()[4:])
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, settlementV2AuditTestCall{to: *transaction.To(), method: method.Name, arguments: arguments, value: transaction.Value(), data: transaction.Data()})
	}
	return calls
}

type settlementV2AuditTestCall struct {
	to        common.Address
	method    string
	arguments []any
	value     *big.Int
	data      []byte
}

// demandPath and checkPath take (noId, rootIndex, account, oldProof[,
// newPath]).
func (self settlementV2AuditTestCall) rootIndex() *big.Int {
	return self.arguments[1].(*big.Int)
}

func (self settlementV2AuditTestCall) account() stabi.LedgerTreeAccount {
	return *abi.ConvertType(self.arguments[2], new(stabi.LedgerTreeAccount)).(*stabi.LedgerTreeAccount)
}

func (self settlementV2AuditTestCall) proof() []merkle.LedgerNode {
	nodes := *abi.ConvertType(self.arguments[3], new([]stabi.LedgerTreeProofNode)).(*[]stabi.LedgerTreeProofNode)
	out := make([]merkle.LedgerNode, len(nodes))
	for k, node := range nodes {
		out[k] = merkle.LedgerNode{Hash: node.Hash, Sum: *uint256.MustFromBig(node.Sum)}
	}
	return out
}

func (self settlementV2AuditTestCall) path() []merkle.LedgerPathNode {
	nodes := *abi.ConvertType(self.arguments[4], new([]stabi.LedgerTreePathNode)).(*[]stabi.LedgerTreePathNode)
	out := make([]merkle.LedgerPathNode, len(nodes))
	for k, node := range nodes {
		out[k] = merkle.LedgerPathNode{LeftHash: node.LeftHash, LeftSum: *uint256.MustFromBig(node.LeftSum), RightHash: node.RightHash, RightSum: *uint256.MustFromBig(node.RightSum)}
	}
	return out
}

// requireApproved requires that the only transactions are one approval of
// exactly root as pool 1's seq.
func (self *settlementV2AuditTestFixture) requireApproved(t *testing.T, calls []settlementV2AuditTestCall, seq uint64, root ledger.Root) {
	t.Helper()
	want := stabi.NewSTSettlement2().PackApproveDistribution(big.NewInt(settlementV2AuditTestNoID), seq, root.Root, root.LedgerHash)
	if len(calls) != 1 || calls[0].to != settlementV2TestSettlement || !bytes.Equal(calls[0].data, want) || calls[0].value.Sign() != 0 {
		t.Fatalf("sent %d transactions, want exactly the approval of seq %d", len(calls), seq)
	}
}

// requireOpening requires that a demand or challenge carries account index of
// the finalized root rootIndex, f, with a proof that verifies against it.
func requireSettlementV2AuditOpening(t *testing.T, call settlementV2AuditTestCall, f *ledger.Ledger, rootIndex, index uint64) {
	t.Helper()
	account := call.account()
	want := f.Accounts[index]
	cumulative := uint256.MustFromBig(account.Cumulative)
	root, total := f.Tree.Root()
	if call.rootIndex().Cmp(new(big.Int).SetUint64(rootIndex)) != 0 || !account.Index.IsUint64() || account.Index.Uint64() != index || account.Coldkey != want.Coldkey || !cumulative.Eq(&want.Cumulative) ||
		!merkle.VerifyLedgerProof(root, &total, f.Tree.LeafCount(), index, account.Coldkey, cumulative, call.proof()) {
		t.Fatalf("%s carries account %s, not account %d of the finalized root with its proof", call.method, account.Index, index)
	}
}

// VA-2: an honest pending root passes every check through the full
// validator path, including head exclusion decided by the FleetBound
// candidates and archive bindingAt at both boundaries. The audit approves
// exactly the pending root, records its finding and decision, keeps the
// rows it found valid, and a second pass neither audits nor sends again.
func TestSettlementV2AuditApprovesHonestRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	record := fixture.record(t, 2)
	if record.Decision != settlementV2DecisionApprove || record.Done || record.Outcome != "" || record.Check != "none" || record.Error != "" || record.Missing != nil || record.Faults != nil || record.Block != 610 {
		t.Fatalf("record %+v", record)
	}
	for i, epoch := range []uint64{4, 5} {
		if chunks, ok, err := fixture.audit.custody.ValidRows(t.Context(), settlementV2AuditTestNoID, epoch); err != nil || !ok || !slices.Equal(chunks, p.Epochs[i].Providers) {
			t.Fatalf("epoch %d rows recorded as %v: %v", epoch, chunks, err)
		}
	}
	// Clients 5, 6 and 7 for epoch 4 and clients 5 and 7 for epoch 5, each at
	// both boundaries: client 8's binding never covers them, so it is never read.
	if fixture.chain.count("bindingAt") != 10 {
		t.Fatalf("%d bindingAt reads, want 10", fixture.chain.count("bindingAt"))
	}
	fetched := fixture.objects.fetched()
	fixture.mustPass(t)
	if len(fixture.chain.accepted()) != 1 || fixture.objects.fetched() != fetched || fixture.chain.count("bindingAt") != 10 {
		t.Fatal("a decided root was audited or approved again")
	}
}

// settlementV2AuditTestMutation is one GL-9 mutation of the standard pending
// root: it publishes what it changes and returns the root to commit.
type settlementV2AuditTestMutation struct {
	name     string
	mutate   func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root
	check    ledger.Check
	want     error
	decision string
	// faults are the indexes checkPath finds faulted.
	faults []uint64
}

func settlementV2AuditTestRowsMutation(i int, mutate func([]protocol.LedgerUsageRow)) func(*testing.T, *settlementV2AuditTestFixture, *ledger.Ledger, *ledger.Ledger) ledger.Root {
	return func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
		pub := settlementV2AuditTestPublish(t, p)
		rows := [][]protocol.LedgerUsageRow{settlementV2AuditTestRows4(), settlementV2AuditTestRows5()}[i]
		mutate(rows)
		pub.setRows(i, rows)
		commit := pub.seal(t)
		fixture.objects.publish(pub.objects)
		return commit
	}
}

func settlementV2AuditTestHeaderMutation(mutate func(*settlementV2AuditTestPublication)) func(*testing.T, *settlementV2AuditTestFixture, *ledger.Ledger, *ledger.Ledger) ledger.Root {
	return func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
		pub := settlementV2AuditTestPublish(t, p)
		mutate(pub)
		commit := pub.seal(t)
		fixture.objects.publish(pub.objects)
		return commit
	}
}

func settlementV2AuditTestAccountsMutation(mutate func(f, p *ledger.Ledger) []merkle.LedgerAccount) func(*testing.T, *settlementV2AuditTestFixture, *ledger.Ledger, *ledger.Ledger) ledger.Root {
	return func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
		pub := settlementV2AuditTestPublish(t, p)
		pub.setAccounts(t, mutate(f, p))
		commit := pub.seal(t)
		fixture.objects.publish(pub.objects)
		return commit
	}
}

func settlementV2AuditTestCommitMutation(mutate func(*ledger.Root)) func(*testing.T, *settlementV2AuditTestFixture, *ledger.Ledger, *ledger.Ledger) ledger.Root {
	return func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
		commit := p.Commit
		mutate(&commit)
		return commit
	}
}

// heldRows4 records the honest epoch 4 rows as valid, as an earlier root's
// audit would have.
func settlementV2AuditTestHeldRows4(t *testing.T, fixture *settlementV2AuditTestFixture, p *ledger.Ledger) {
	t.Helper()
	if err := fixture.audit.custody.KeepValidRows(t.Context(), settlementV2AuditTestNoID, 4, p.Epochs[0].Providers); err != nil {
		t.Fatal(err)
	}
}

func settlementV2AuditTestMutations() []settlementV2AuditTestMutation {
	accounts := func(p *ledger.Ledger) []merkle.LedgerAccount { return slices.Clone(p.Accounts) }
	return []settlementV2AuditTestMutation{
		// Check 1, header.
		{name: "header sha256 is not the ledger hash", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			fixture.objects.publish(ledger.Objects{p.Commit.LedgerHash: bytes.Replace(p.Header, []byte(`"no_id":1`), []byte(`"no_id":2`), 1)})
			return p.Commit
		}, check: ledger.CheckHeader, want: ledger.ErrLedgerHash, decision: settlementV2DecisionDemand},
		{name: "header bytes not canonical", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			pub := settlementV2AuditTestPublish(t, p)
			commit := pub.sealBytes(bytes.Replace(p.Header, []byte(`"no_id":1`), []byte(`"no_id": 1`), 1))
			fixture.objects.publish(pub.objects)
			return commit
		}, check: ledger.CheckHeader, want: ledger.ErrHeaderNotCanonical, decision: settlementV2DecisionDemand},
		{name: "header no_id", mutate: settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) { pub.header.NoID = 2 }),
			check: ledger.CheckHeader, want: ledger.ErrHeaderNoID, decision: settlementV2DecisionDemand},
		{name: "header epoch", mutate: settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) { pub.header.Epoch = 6 }),
			check: ledger.CheckHeader, want: ledger.ErrHeaderEpoch, decision: settlementV2DecisionDemand},
		{name: "covered range wrong", mutate: settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) { pub.header.Epochs = pub.header.Epochs[1:] }),
			check: ledger.CheckHeader, want: ledger.ErrCoveredEpochs, decision: settlementV2DecisionDemand},
		{name: "covered range too long", mutate: settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) {
			pub.header.Epoch, pub.commit.Epoch, pub.header.Epochs = 100, 100, nil
			for k := uint64(5); k <= 100; k++ {
				pub.header.Epochs = append(pub.header.Epochs, ledger.HeaderEpoch{Epoch: k})
			}
		}), check: ledger.CheckHeader, want: ledger.ErrTooManyEpochs, decision: settlementV2DecisionDemand},
		// Check 2, previous root.
		{name: "previous root not tight", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			previous := f.Commit
			previous.Total.AddUint64(&previous.Total, 1)
			fixture.setPool(settlementV2AuditTestPending(2, p.Commit, 605), previous)
			return p.Commit
		}, check: ledger.CheckPrevious, want: ledger.ErrPreviousNotTight, decision: settlementV2DecisionRefuse},
		{name: "previous root not rebuilt", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			previous := f.Commit
			previous.Root[5] ^= 1
			fixture.setPool(settlementV2AuditTestPending(2, p.Commit, 605), previous)
			return p.Commit
		}, check: ledger.CheckPrevious, want: ledger.ErrPreviousRebuild, decision: settlementV2DecisionRefuse},
		// Check 3, fixed inputs.
		{name: "fixed rows changed", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			settlementV2AuditTestHeldRows4(t, fixture, p)
			changed := settlementV2AuditTestBuild(t, f, []ledger.EpochInput{{Epoch: 4, Rows: append(settlementV2AuditTestRows4(), settlementV2AuditTestRow(15, 11, 40, false))},
				{Epoch: 5, Rows: settlementV2AuditTestRows5()}}, settlementV2AuditTestFunding())
			fixture.objects.publish(changed.Objects)
			return changed.Commit
		}, check: ledger.CheckFixedInputs, want: ledger.ErrFixedInputs, decision: settlementV2DecisionRefuse},
		{name: "null against held rows", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			settlementV2AuditTestHeldRows4(t, fixture, p)
			return settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) {
				pub.header.Epochs[0] = ledger.HeaderEpoch{Epoch: 4, Null: true}
			})(t, fixture, f, p)
		}, check: ledger.CheckFixedInputs, want: ledger.ErrNullAgainstValidRows, decision: settlementV2DecisionRefuse},
		// Check 4, chunks.
		{name: "chunk hash", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			fixture.objects.publish(ledger.Objects{p.Epochs[0].Providers[0]: ledger.EncodeProviderChunks(settlementV2AuditTestRows4()[1:])[0]})
			return p.Commit
		}, check: ledger.CheckChunks, want: ledger.ErrChunkHash, decision: settlementV2DecisionDemand},
		{name: "short middle chunk", mutate: settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) {
			chunk := ledger.EncodeProviderChunks(settlementV2AuditTestRows4())[0]
			pub.setChunks(0, [][]byte{chunk[:3*ledger.ProviderRowBytes], chunk[3*ledger.ProviderRowBytes:]})
		}), check: ledger.CheckChunks, want: ledger.ErrChunkShort, decision: settlementV2DecisionRefuse},
		{name: "empty last chunk", mutate: settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) { pub.setChunks(0, [][]byte{{}}) }),
			check: ledger.CheckChunks, want: ledger.ErrChunkEmpty, decision: settlementV2DecisionRefuse},
		{name: "bad row length", mutate: settlementV2AuditTestHeaderMutation(func(pub *settlementV2AuditTestPublication) {
			pub.setChunks(0, [][]byte{append(ledger.EncodeProviderChunks(settlementV2AuditTestRows4())[0], 0)})
		}), check: ledger.CheckChunks, want: ledger.ErrChunkLength, decision: settlementV2DecisionRefuse},
		// Check 5, rows.
		{name: "rows unsorted", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[0], r[1] = r[1], r[0] }),
			check: ledger.CheckRows, want: ledger.ErrRowsUnsorted, decision: settlementV2DecisionRefuse},
		{name: "row duplicate", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[1].ClientID = r[0].ClientID }),
			check: ledger.CheckRows, want: ledger.ErrRowDuplicate, decision: settlementV2DecisionRefuse},
		{name: "row zero id", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[0].ClientID = [16]byte{} }),
			check: ledger.CheckRows, want: ledger.ErrRowZeroClientID, decision: settlementV2DecisionRefuse},
		{name: "row zero usage", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[1].UsageBytes = 0 }),
			check: ledger.CheckRows, want: ledger.ErrRowZeroUsage, decision: settlementV2DecisionRefuse},
		{name: "rows total usage 2^64", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[5].UsageBytes = ^uint64(0) - 999 }),
			check: ledger.CheckRows, want: ledger.ErrUsageOverflow, decision: settlementV2DecisionRefuse},
		// Check 6, head exclusion.
		{name: "head flag set without a candidate", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[0].HeadExcluded = true }),
			check: ledger.CheckHeadExclusion, want: ledger.ErrHeadFlagSet, decision: settlementV2DecisionRefuse},
		{name: "head flag set for a candidate without a binding", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[3].HeadExcluded = true }),
			check: ledger.CheckHeadExclusion, want: ledger.ErrHeadFlagSet, decision: settlementV2DecisionRefuse},
		{name: "head flag set for a fleet member outside its validity", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[5].HeadExcluded = true }),
			check: ledger.CheckHeadExclusion, want: ledger.ErrHeadFlagSet, decision: settlementV2DecisionRefuse},
		{name: "head flag cleared with a binding at both boundaries", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[2].HeadExcluded = false }),
			check: ledger.CheckHeadExclusion, want: ledger.ErrHeadFlagCleared, decision: settlementV2DecisionRefuse},
		{name: "head flag cleared with a binding at the end boundary", mutate: settlementV2AuditTestRowsMutation(0, func(r []protocol.LedgerUsageRow) { r[4].HeadExcluded = false }),
			check: ledger.CheckHeadExclusion, want: ledger.ErrHeadFlagCleared, decision: settlementV2DecisionRefuse},
		// Check 7, allocation.
		{name: "allocation delta", mutate: settlementV2AuditTestAccountsMutation(func(f, p *ledger.Ledger) []merkle.LedgerAccount {
			a := accounts(p)
			a[0].Cumulative.SubUint64(&a[0].Cumulative, 1)
			a[1].Cumulative.AddUint64(&a[1].Cumulative, 1)
			return a
		}), check: ledger.CheckAllocation, want: ledger.ErrAccountDelta, decision: settlementV2DecisionRefuse},
		{name: "allocation omits a new account", mutate: settlementV2AuditTestAccountsMutation(func(f, p *ledger.Ledger) []merkle.LedgerAccount {
			a := accounts(p)
			a[6].Cumulative.Add(&a[6].Cumulative, &a[5].Cumulative)
			return slices.Delete(a, 5, 6)
		}), check: ledger.CheckAllocation, want: ledger.ErrAccountOmitted, decision: settlementV2DecisionRefuse},
		// Check 8, accounts.
		{name: "account coldkey moved", mutate: settlementV2AuditTestAccountsMutation(func(f, p *ledger.Ledger) []merkle.LedgerAccount {
			a := accounts(p)
			a[1].Coldkey = settlementV2AuditTestColdkey(99)
			return a
		}), check: ledger.CheckAccounts, want: ledger.ErrAccountMoved, decision: settlementV2DecisionChallenge, faults: []uint64{1}},
		{name: "account cumulative lowered", mutate: settlementV2AuditTestAccountsMutation(func(f, p *ledger.Ledger) []merkle.LedgerAccount {
			a := accounts(p)
			var moved uint256.Int
			moved.Sub(&a[0].Cumulative, &f.Accounts[0].Cumulative)
			moved.AddUint64(&moved, 1)
			a[0].Cumulative.Sub(&a[0].Cumulative, &moved)
			a[1].Cumulative.Add(&a[1].Cumulative, &moved)
			return a
		}), check: ledger.CheckAccounts, want: ledger.ErrAccountLowered, decision: settlementV2DecisionChallenge, faults: []uint64{0}},
		{name: "new accounts unsorted", mutate: settlementV2AuditTestAccountsMutation(func(f, p *ledger.Ledger) []merkle.LedgerAccount {
			a := accounts(p)
			a[4], a[5] = a[5], a[4]
			return a
		}), check: ledger.CheckAccounts, want: ledger.ErrAccountUnsorted, decision: settlementV2DecisionRefuse},
		{name: "coldkey duplicated", mutate: settlementV2AuditTestAccountsMutation(func(f, p *ledger.Ledger) []merkle.LedgerAccount {
			a := accounts(p)
			a[0].Cumulative.SubUint64(&a[0].Cumulative, 1)
			return slices.Insert(a, 5, merkle.LedgerAccount{Coldkey: settlementV2AuditTestColdkey(1), Cumulative: *uint256.NewInt(1)})
		}), check: ledger.CheckAccounts, want: ledger.ErrAccountDuplicate, decision: settlementV2DecisionRefuse},
		{name: "new account with a zero delta", mutate: settlementV2AuditTestAccountsMutation(func(f, p *ledger.Ledger) []merkle.LedgerAccount {
			return slices.Insert(accounts(p), 5, merkle.LedgerAccount{Coldkey: settlementV2AuditTestColdkey(5)})
		}), check: ledger.CheckAccounts, want: ledger.ErrAccountZeroDelta, decision: settlementV2DecisionRefuse},
		// Check 9, tree.
		{name: "tree root", mutate: settlementV2AuditTestCommitMutation(func(r *ledger.Root) { r.Root[0] ^= 1 }),
			check: ledger.CheckTree, want: ledger.ErrTreeRoot, decision: settlementV2DecisionDemand},
		{name: "tree total", mutate: settlementV2AuditTestCommitMutation(func(r *ledger.Root) { r.Total.AddUint64(&r.Total, 1) }),
			check: ledger.CheckTree, want: ledger.ErrTreeTotal, decision: settlementV2DecisionDemand},
		{name: "tree leaf count", mutate: settlementV2AuditTestCommitMutation(func(r *ledger.Root) { r.LeafCount++ }),
			check: ledger.CheckTree, want: ledger.ErrTreeLeafCount, decision: settlementV2DecisionDemand},
		// The byte budget, which the last provider chunk exceeds.
		{name: "byte budget", mutate: func(t *testing.T, fixture *settlementV2AuditTestFixture, f, p *ledger.Ledger) ledger.Root {
			var need uint64
			for _, data := range p.Objects {
				need += uint64(len(data))
			}
			fixture.audit.maxBytes = need - 1
			return p.Commit
		}, check: ledger.CheckFetch, want: ledger.ErrByteBudget, decision: settlementV2DecisionRefuse},
	}
}

// VA-2, GL-9: each §9.2 check, triggered through the full validator path by
// its own mutation, refuses approval: the audit records exactly that check
// and error, never approves, and takes the §9.3 action. The decisions are
// taken after commit + CHALLENGE_BLOCKS/2, so a root whose objects are
// missing is decided too.
func TestSettlementV2AuditRefusesEachMutation(t *testing.T) {
	t.Parallel()
	for _, mutation := range settlementV2AuditTestMutations() {
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			f := settlementV2AuditTestF(t)
			p := settlementV2AuditTestP(t, f)
			fixture.useScenario(t, f, p.Commit, p.Objects, true, 630)
			commit := mutation.mutate(t, fixture, f, p)
			if commit != p.Commit {
				fixture.chain.lock.Lock()
				previous := fixture.chain.states[fixture.chain.latest].roots[settlementV2AuditTestNoID][0]
				fixture.chain.lock.Unlock()
				fixture.chain.mutate(func(state *settlementV2TestState) {
					state.pending[settlementV2AuditTestNoID] = settlementV2AuditTestPending(2, commit, 605)
					state.roots[settlementV2AuditTestNoID] = []stabi.ISTSettlementVault2Root{previous}
				})
			}
			for _, index := range mutation.faults {
				fixture.chain.faults[index] = true
			}
			fixture.mustPass(t)
			record := fixture.record(t, 2)
			if record.Check != mutation.check.String() || !strings.Contains(record.Error, mutation.want.Error()) || record.Decision != mutation.decision || !record.Done {
				t.Fatalf("record: check %q, error %q, decision %q, done %v; want %v, %v, %s", record.Check, record.Error, record.Decision, record.Done, mutation.check, mutation.want, mutation.decision)
			}
			calls := fixture.sentCalls(t)
			for _, call := range calls {
				want := map[string]string{settlementV2DecisionDemand: "demandPath", settlementV2DecisionChallenge: "checkPath"}[mutation.decision]
				if call.method != want {
					t.Fatalf("sent %s for a %s decision", call.method, mutation.decision)
				}
			}
			switch mutation.decision {
			case settlementV2DecisionRefuse:
				if len(calls) != 0 || record.Outcome != "refused" {
					t.Fatalf("a refusal sent %d transactions, outcome %q", len(calls), record.Outcome)
				}
			case settlementV2DecisionChallenge:
				if len(calls) != 1 || record.Outcome != "sent" {
					t.Fatalf("%d challenges sent, outcome %q", len(calls), record.Outcome)
				}
				requireSettlementV2AuditOpening(t, calls[0], f, 0, mutation.faults[0])
			case settlementV2DecisionDemand:
				if len(calls) != len(f.Accounts) || record.Outcome != "sent" || !slices.Equal(record.Demands, settlementV2AuditTestWhole(t, f.Accounts)) {
					t.Fatalf("%d demands sent for %v, outcome %q", len(calls), record.Demands, record.Outcome)
				}
			}
		})
	}
}

// VA-7: a fresh validator fetches and verifies the previous root's header
// and account chunks, never its provider chunks, keeps every object it
// fetched with exactly its bytes, and records each covered epoch's valid
// rows. Restarted, it audits a replacement root that lists the same rows
// from custody, fetching only the new objects.
func TestSettlementV2AuditKeepsCustody(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, false, 610)
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	header, err := ledger.DecodeHeader(f.Header)
	if err != nil {
		t.Fatal(err)
	}
	kept := append([]ledger.Ref{f.Commit.LedgerHash}, header.Accounts...)
	for ref := range p.Objects {
		kept = append(kept, ref)
	}
	for _, ref := range kept {
		data, ok, err := fixture.audit.custody.Object(t.Context(), ref)
		if err != nil || !ok || fixture.objects.fetched(ref) != 1 || (!bytes.Equal(data, p.Objects[ref]) && !bytes.Equal(data, f.Objects[ref])) {
			t.Fatalf("object %v: held %v, fetched %d times: %v", ref, ok, fixture.objects.fetched(ref), err)
		}
	}
	if fixture.objects.fetched(f.Epochs[0].Providers...) != 0 {
		t.Fatal("the previous root's provider chunks were fetched")
	}
	if _, ok, err := fixture.audit.custody.ValidRows(t.Context(), settlementV2AuditTestNoID, 3); ok || err != nil {
		t.Fatalf("the previous root's rows were recorded: %v", err)
	}

	funded := settlementV2AuditTestFunding()
	funded[6] = *uint256.NewInt(3_000_000)
	fixture.useFunding(funded)
	replacement := settlementV2AuditTestBuild(t, f, []ledger.EpochInput{{Epoch: 4, Rows: settlementV2AuditTestRows4()}, {Epoch: 5, Rows: settlementV2AuditTestRows5()},
		{Epoch: 6, Rows: []protocol.LedgerUsageRow{settlementV2AuditTestRow(3, 2, 100, false)}}}, funded)
	if !slices.Equal(replacement.Epochs[0].Providers, p.Epochs[0].Providers) || !slices.Equal(replacement.Epochs[1].Providers, p.Epochs[1].Providers) {
		t.Fatal("the replacement does not list the same rows")
	}
	fixture.objects.publish(replacement.Objects)
	fixture.committed(t, 705, 3, replacement.Commit, 2)
	fixture.chain.advanceTo(709)
	fixture.setPool(settlementV2AuditTestPending(3, replacement.Commit, 705), f.Commit)
	fixture.restart(t)
	before := fixture.objects.fetched()
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t)[1:], 3, replacement.Commit)
	if fetched := fixture.objects.fetched() - before; fetched != len(replacement.Objects)-2 {
		t.Fatalf("fetched %d objects, want the replacement's %d new ones", fetched, len(replacement.Objects)-2)
	}
}

// VA-7: a null declaration for an epoch without valid rows is accepted and
// recorded with the declaring root's seq; no rows are recorded for it.
func TestSettlementV2AuditRecordsNullDeclaration(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	declared := settlementV2AuditTestBuild(t, f, []ledger.EpochInput{{Epoch: 4, Null: true}, {Epoch: 5, Rows: settlementV2AuditTestRows5()}}, settlementV2AuditTestFunding())
	fixture.useScenario(t, f, declared.Commit, declared.Objects, true, 610)
	fixture.mustPass(t)
	fixture.requireApproved(t, fixture.sentCalls(t), 2, declared.Commit)
	var null settlementV2NullRecord
	if ok, err := fixture.audit.custody.readJSON(fixture.audit.custody.path("nulls", "1-4-2.json"), &null); err != nil || !ok || null.Seq != 2 || null.Epoch != 4 || null.NoID != settlementV2AuditTestNoID {
		t.Fatalf("null declaration recorded as %+v, %v: %v", null, ok, err)
	}
	if _, ok, err := fixture.audit.custody.ValidRows(t.Context(), settlementV2AuditTestNoID, 4); ok || err != nil {
		t.Fatalf("rows recorded for a null epoch: %v", err)
	}
	if _, ok, err := fixture.audit.custody.ValidRows(t.Context(), settlementV2AuditTestNoID, 5); !ok || err != nil {
		t.Fatalf("epoch 5's rows not recorded: %v", err)
	}
}

// VA-7: a fresh validator whose operator serves previous-root chunks that do
// not match their references records them as missing and the finding, waits
// until commit + CHALLENGE_BLOCKS/2, then refuses: it holds no verified root
// to demand against.
func TestSettlementV2AuditFreshValidatorRefusesUnverifiablePrevious(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, false, 610)
	header, err := ledger.DecodeHeader(f.Header)
	if err != nil {
		t.Fatal(err)
	}
	chunk := header.Accounts[0]
	fixture.objects.publish(ledger.Objects{chunk: append(slices.Clone(f.Objects[chunk]), 0)})
	fixture.mustPass(t)
	record := fixture.record(t, 2)
	if record.Decision != "" || record.Check != ledger.CheckPrevious.String() || !strings.Contains(record.Error, ledger.ErrPreviousRebuild.Error()) ||
		!strings.Contains(record.Error, ledger.ErrChunkHash.Error()) || !slices.Equal(record.Missing, []string{chunk.String()}) {
		t.Fatalf("record %+v", record)
	}
	fixture.chain.advanceTo(630)
	fixture.mustPass(t)
	if record := fixture.record(t, 2); record.Decision != settlementV2DecisionRefuse || !record.Done || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("record %+v", record)
	}
}
