// CL-1, CL-3, E-4, VA-4: `snclaim v2 check --all` against v2TestChain.
package onchain

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"maps"
	"math/big"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// v2Row is a provider row; cold 0 is the zero coldkey.
func v2Row(client, cold int, usage uint64) protocol.LedgerUsageRow {
	row := protocol.LedgerUsageRow{UsageBytes: usage}
	binary.BigEndian.PutUint64(row.ClientID[8:], uint64(client))
	if cold != 0 {
		row.Coldkey = v2TestKey(cold)
	}
	return row
}

func v2Funded(pairs ...uint64) map[uint64]uint256.Int {
	out := map[uint64]uint256.Int{}
	for i := 0; i < len(pairs); i += 2 {
		out[pairs[i]] = *uint256.NewInt(pairs[i+1])
	}
	return out
}

// v2CheckScenario is pool 3, first epoch 10: the finalized root F (seq 1,
// root 0) covers epoch 10 with four accounts; the pending root P (seq 2)
// covers epochs 11 and 12 and adds three. Both are built by ledger.Build and
// published.
type v2CheckScenario struct {
	c    *v2TestChain
	f, p *ledger.Ledger
}

func newV2CheckScenario(t *testing.T) *v2CheckScenario {
	t.Helper()
	ctx := context.Background()
	funded := v2Funded(10, 1_000_003, 11, 1_600_000, 12, 2_500_000)
	f, err := ledger.Build(ctx, ledger.BuildInput{NoID: v2TestPool, Epoch: 10, FirstEpoch: 10, FundedThrough: funded, Epochs: []ledger.EpochInput{{Epoch: 10, Rows: []protocol.LedgerUsageRow{
		v2Row(1, 1, 300), v2Row(2, 1, 100), v2Row(3, 2, 200), v2Row(4, 0, 500), v2Row(5, 3, 400), v2Row(6, 4, 1),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := ledger.Build(ctx, ledger.BuildInput{NoID: v2TestPool, Epoch: 12, FirstEpoch: 10, FundedThrough: funded, Previous: &ledger.Previous{Root: f.Commit, Accounts: f.Accounts}, Epochs: []ledger.EpochInput{
		{Epoch: 11, Rows: []protocol.LedgerUsageRow{v2Row(1, 1, 250), v2Row(3, 2, 250), v2Row(5, 3, 100), v2Row(8, 5, 300), v2Row(10, 0, 50)}},
		{Epoch: 12, Rows: []protocol.LedgerUsageRow{v2Row(3, 2, 500), v2Row(11, 7, 500), v2Row(12, 5, 10), v2Row(13, 8, 7)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Accounts) != 4 || len(p.Accounts) != 7 {
		t.Fatalf("scenario has %d and %d accounts", len(f.Accounts), len(p.Accounts))
	}
	s := &v2CheckScenario{c: newV2TestChain(t), f: f, p: p}
	s.c.roots = []stabi.ISTSettlementVault2Root{v2TestRoot(f.Tree, 10, f.Commit.LedgerHash)}
	for epoch, value := range funded {
		s.c.funded[epoch] = value.ToBig()
	}
	s.c.objects = maps.Clone(f.Objects)
	maps.Copy(s.c.objects, p.Objects)
	s.c.committed(v2TestHead-40_000, 1, f.Commit, 0)
	s.c.finalized(v2TestHead-20_000, 1, 0, f.Commit)
	s.setPending(2, p.Commit)
	return s
}

// setPending commits root as pending seq at v2TestHead − 1,000.
func (s *v2CheckScenario) setPending(seq uint64, root ledger.Root) {
	s.c.pending = stabi.ISTSettlementVault2Pending{Seq: seq, Root: root.Root, Total: root.Total.ToBig(), LeafCount: root.LeafCount, Epoch: root.Epoch, LedgerHash: root.LedgerHash, CommittedBlock: v2TestHead - 1_000}
	s.c.committed(v2TestHead-1_000, seq, root, 0)
}

// publish publishes accounts under P's covered epochs, as an operator
// committing their tree would, and returns the commitment and the tree.
func (s *v2CheckScenario) publish(t *testing.T, accounts []merkle.LedgerAccount) (ledger.Root, *merkle.LedgerSumTree) {
	t.Helper()
	return s.publishUnder(t, s.p.Header, accounts)
}

// publishUnder publishes accounts under the covered epochs of a header.
func (s *v2CheckScenario) publishUnder(t *testing.T, headerBytes []byte, accounts []merkle.LedgerAccount) (ledger.Root, *merkle.LedgerSumTree) {
	t.Helper()
	chunks, err := ledger.EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	header, err := ledger.DecodeHeader(headerBytes)
	if err != nil {
		t.Fatal(err)
	}
	header.Accounts = nil
	for _, chunk := range chunks {
		header.Accounts = append(header.Accounts, ledger.RefOf(chunk))
		s.c.objects[ledger.RefOf(chunk)] = chunk
	}
	data, err := ledger.EncodeHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	s.c.objects[ledger.RefOf(data)] = data
	tree := v2TestTree(t, accounts)
	root, total := tree.Root()
	return ledger.Root{Epoch: header.Epoch, Root: root, Total: total, LeafCount: tree.LeafCount(), LedgerHash: ledger.RefOf(data)}, tree
}

// withhold removes P's header from the endpoint.
func (s *v2CheckScenario) withhold() {
	delete(s.c.objects, s.c.pending.LedgerHash)
}

// seedV2Random makes the demand sample's randomness a seeded stream.
func seedV2Random(t *testing.T, seed byte) {
	v2Random = rand.NewChaCha8([32]byte{seed})
	t.Cleanup(func() { v2Random = crand.Reader })
}

// v2DemandSet is the demand set a check draws after seedV2Random(t, seed),
// in its order: ledger.DemandSet with that stream.
func v2DemandSet(t *testing.T, accounts []merkle.LedgerAccount, faults []ledger.Fault, doublings uint, again []uint64, seed byte) []uint64 {
	t.Helper()
	set, err := ledger.DemandSet(accounts, faults, doublings, again, rand.NewChaCha8([32]byte{seed}))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// checkPathCalldata is checkPath for account i of finalized root root,
// whose tree and accounts are given, packed from vault2's ABI.
func checkPathCalldata(t *testing.T, root uint64, tree *merkle.LedgerSumTree, accounts []merkle.LedgerAccount, i uint64, path []merkle.LedgerPathNode) []byte {
	t.Helper()
	proof, err := tree.Proof(i)
	if err != nil {
		t.Fatal(err)
	}
	data, err := v2TestABI.Pack("checkPath", big.NewInt(v2TestPool), new(big.Int).SetUint64(root), v2LedgerAccount(i, &accounts[i]), v2ProofNodes(proof), v2PathNodes(path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// open opens account i of finalized root root on the pending root with
// path, as its operator answering a demand would, and returns the events.
func (c *v2TestChain) open(t *testing.T, root uint64, tree *merkle.LedgerSumTree, accounts []merkle.LedgerAccount, i uint64, path []merkle.LedgerPathNode) []*types.Log {
	t.Helper()
	data := checkPathCalldata(t, root, tree, accounts, i, path)
	var logs []*types.Log
	c.do(func() {
		var revert []byte
		if _, logs, revert = c.execute(common.Address{0x0a}, nil, data, true); revert != nil {
			t.Fatalf("an answer of account %d reverts 0x%x", i, revert)
		}
	})
	return logs
}

// requireDeleted checks one DistributionDeleted of seq with exact fields.
func requireDeleted(t *testing.T, logs []*types.Log, seq, root, index uint64, fault merkle.LedgerFault, level int64, challenger common.Address) {
	t.Helper()
	if len(logs) != 1 {
		t.Fatalf("%d events, want one DistributionDeleted", len(logs))
	}
	event, err := vault2.UnpackDistributionDeletedEvent(logs[0])
	if err != nil || logs[0].Address != common.HexToAddress(v2TestVault) || event.NoId.Cmp(big.NewInt(v2TestPool)) != 0 || event.Seq != seq || event.RootIndex.Cmp(new(big.Int).SetUint64(root)) != 0 ||
		event.Index.Cmp(new(big.Int).SetUint64(index)) != 0 || event.Fault != uint8(fault) || event.Level.Cmp(big.NewInt(level)) != 0 || event.Challenger != challenger {
		t.Fatalf("DistributionDeleted %+v (%v), want seq %d root %d index %d fault %v level %d challenger %s", event, err, seq, root, index, fault, level, challenger)
	}
}

// demandedIndexes returns the indexes of the PathDemanded events snclaim's
// transactions emitted, checking their exact fields.
func (s *v2CheckScenario) demandedIndexes(t *testing.T, seq, root uint64) []uint64 {
	t.Helper()
	var indexes []uint64
	for _, log := range s.c.emitted {
		event, err := vault2.UnpackPathDemandedEvent(log)
		if err != nil || event.NoId.Cmp(big.NewInt(v2TestPool)) != 0 || event.Seq != seq || event.RootIndex.Cmp(new(big.Int).SetUint64(root)) != 0 || event.Demander != common.HexToAddress(testKeyAddr) || !event.Index.IsUint64() {
			t.Fatalf("PathDemanded %+v (%v), want pool 3 seq %d root %d by the relayer", event, err, seq, root)
		}
		indexes = append(indexes, event.Index.Uint64())
	}
	return indexes
}

// requireDemands checks that every demand sent pays exactly the fee and
// carries the finalized root's index, the account and its proof against
// that root (SPEC A2).
func (s *v2CheckScenario) requireDemands(t *testing.T, root uint64, tree *merkle.LedgerSumTree, accounts []merkle.LedgerAccount, indexes []uint64) {
	t.Helper()
	sent := s.c.transactions()
	if len(sent) != len(indexes) {
		t.Fatalf("%d transactions for %d demands", len(sent), len(indexes))
	}
	for k, i := range indexes {
		proof, err := tree.Proof(i)
		if err != nil {
			t.Fatal(err)
		}
		data, err := v2TestABI.Pack("demandPath", big.NewInt(v2TestPool), new(big.Int).SetUint64(root), v2LedgerAccount(i, &accounts[i]), v2ProofNodes(proof))
		if err != nil {
			t.Fatal(err)
		}
		requireV2Tx(t, sent[k], 7+uint64(k), 60_000, v2TestFee, data)
	}
}

// CL-1: an honest pending root passes every check (but head exclusion,
// which needs archive state): every account of F has no fault, nothing is
// sent, and the chain is read at the finalized block.
func TestV2CheckHonestRoot(t *testing.T) {
	s := newV2CheckScenario(t)
	output, err := s.c.run(s.c.checkArgv("--challenge", "--demand", "--chain_id=945"))
	want := "settlement v2 ledger check — vault " + common.HexToAddress(v2TestVault).Hex() + ", pool 3 (chain id 945, rpc " + s.c.rpc.URL + ")\n" +
		"  finalized block:   200000 (" + v2TestBlockHash(v2TestHead).Hex() + ")\n" +
		"  pending root:      seq 2, epoch 12, committed at block 199000, 0 open demands\n" +
		fmt.Sprintf("                     root 0x%x, total 0.002500000 α (2500000 rao), 7 accounts, ledger %v\n", s.p.Commit.Root, ledger.Ref(s.p.Commit.LedgerHash)) +
		fmt.Sprintf("  finalized root:    index 0, epoch 10, root 0x%x, total 0.001000003 α (1000003 rao), 4 accounts\n", s.f.Commit.Root) +
		"  audit:             approve\n" +
		"  previous accounts: 4 of root 0, no fault\n"
	reads := "eth_call:fundedThrough=2 eth_call:pending=1 eth_call:pools=1 eth_call:rootCount=1 eth_call:roots=1 eth_chainId=1 eth_getBlockByNumber:finalized=1"
	if err != nil || output != want || s.c.observed() != reads || len(s.c.transactions()) != 0 {
		t.Fatalf("honest root: %v\n%s\nwant:\n%s\nobserved %s", err, output, want, s.c.observed())
	}
	for _, block := range s.c.blocks {
		if !strings.HasSuffix(block, "@0x30d40") {
			t.Fatalf("a check read is not at the finalized block: %v", s.c.blocks)
		}
	}
	// Both header and every chunk were fetched from the object endpoint.
	if s.c.apiCalls["/sn/ledger/object"] != len(s.f.Objects)-len(s.f.Epochs[0].Providers)+len(s.p.Objects) {
		t.Fatalf("fetched %d objects", s.c.apiCalls["/sn/ledger/object"])
	}
}

// CL-1: objects are fetched from GET /sn/ledger/object on the API host, as
// the validator fetches them: a path in --api_url is replaced, not
// extended, so the check of an honest root under such a URL reads every
// object there (the endpoint refuses any other path).
func TestV2CheckObjectsOnAPIHost(t *testing.T) {
	s := newV2CheckScenario(t)
	output, err := s.c.run(s.c.checkArgv("--api_url=" + s.c.api.URL + "/operator/v1/"))
	if err != nil || !strings.Contains(output, "  audit:             approve\n") || !strings.HasSuffix(output, "  previous accounts: 4 of root 0, no fault\n") ||
		len(s.c.apiCalls) != 1 || s.c.apiCalls["/sn/ledger/object"] != len(s.f.Objects)-len(s.f.Epochs[0].Providers)+len(s.p.Objects) {
		t.Fatalf("objects under an API path: %v; fetched %v\n%s", err, s.c.apiCalls, output)
	}
}

// E-4: a published faulty root is caught and challenged for each fault
// kind its account rows can show: an omitted account, a lowered one, a
// garbage leaf and a moved coldkey. checkPath for the first fault, with F's
// account, its proof against F and the published tree's path, deletes the
// root; vault2's DistributionDeleted names the fault at the leaf.
func TestV2CheckChallengesPublishedFaults(t *testing.T) {
	attacker := merkle.LedgerAccount{Coldkey: v2TestKey(99)}
	cases := []struct {
		name   string
		mutate func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount
		index  uint64
	}{
		{"omitted account", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			omitted := p[1]
			attacker.Cumulative = omitted.Cumulative
			return append(slices.Delete(slices.Clone(p), 1, 2), attacker)
		}, 1},
		{"lowered account", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			p = slices.Clone(p)
			p[6].Cumulative.Add(&p[6].Cumulative, new(uint256.Int).Sub(&p[0].Cumulative, &f[0].Cumulative))
			p[6].Cumulative.AddUint64(&p[6].Cumulative, 1)
			p[0].Cumulative.SubUint64(&f[0].Cumulative, 1)
			return p
		}, 0},
		{"garbage leaf", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			p = slices.Clone(p)
			p[2].Coldkey = [32]byte{0xde, 0xad, 0xbe, 0xef}
			return p
		}, 2},
		{"moved coldkey", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			p = slices.Clone(p)
			p[1].Coldkey, p[3].Coldkey = p[3].Coldkey, p[1].Coldkey
			return p
		}, 1},
	}
	for _, c := range cases {
		s := newV2CheckScenario(t)
		accounts := c.mutate(s.f.Accounts, s.p.Accounts)
		root, tree := s.publish(t, accounts)
		s.setPending(2, root)
		want := merkle.LedgerFaultWrongLeaf
		if accounts[c.index].Cumulative.Lt(&s.f.Accounts[c.index].Cumulative) {
			want = merkle.LedgerFaultLowerValue
		}
		output, err := s.c.run(s.c.checkArgv("--challenge"))
		if err != nil || s.c.pending.Seq != 0 {
			t.Fatalf("%s: %v; pending seq %d\n%s", c.name, err, s.c.pending.Seq, output)
		}
		requireDeleted(t, s.c.emitted, 2, 0, c.index, want, 0, common.HexToAddress(testKeyAddr))
		path, _ := tree.Path(c.index)
		requireV2Tx(t, s.c.transactions()[0], 7, 60_000, 0, checkPathCalldata(t, 0, s.f.Tree, s.f.Accounts, c.index, path))
		for _, line := range []string{
			"  audit:             challenge (accounts check: ledger: a previous ",
			fmt.Sprintf("    account %d:      %v\n", c.index, want),
			fmt.Sprintf("\n  account:     %d of finalized root 0: coldkey 0x%x, cumulative ", c.index, s.f.Accounts[c.index].Coldkey),
			fmt.Sprintf("\n  fault:       %v\n", want),
			fmt.Sprintf("\nDistributionDeleted: pool 3, seq 2\n  account:     %d of finalized root 0\n  fault:       %v at level 0\n  challenger:  %s\n", c.index, want, testKeyAddr),
		} {
			if !strings.Contains(output, line) {
				t.Fatalf("%s: output lacks %q:\n%s", c.name, line, output)
			}
		}
		if c.name == "lowered account" && !strings.Contains(output, "  previous accounts: 4 of root 0, 1 with a provable fault, 3 with no fault\n") {
			t.Fatalf("lowered account report:\n%s", output)
		}
	}
}

// E-4: the over-allocation hidden in an inconsistent subtree cannot be
// published as account rows: rows carrying the over-allocation do not
// reproduce the root, so the check reports F's accounts unverifiable and
// demands them all. Its operator can answer the demands outside the
// subtree, but opening any account under it shows the inconsistent node and
// deletes the root, so the demands block finalization.
func TestV2CheckInconsistentSubtree(t *testing.T) {
	s := newV2CheckScenario(t)
	const hidden = 1_000
	rows := slices.Clone(s.p.Accounts)
	rows[6].Cumulative.AddUint64(&rows[6].Cumulative, hidden)
	committed := newV2TestRawTree(t, rows)
	// Level 2, position 0 covers F's four accounts: it states their sum
	// less the over-allocation, so the root's total is still the funding.
	stated := committed.levels[2][0].Sum
	stated.SubUint64(&stated, hidden)
	committed.misstate(2, 0, stated)
	root, total := committed.root()
	if want, _ := s.p.Tree.Root(); total != s.p.Commit.Total || root == want {
		t.Fatalf("inconsistent root total %v", total.Dec())
	}
	published, _ := s.publish(t, rows)
	s.setPending(2, ledger.Root{Epoch: 12, Root: root, Total: total, LeafCount: 7, LedgerHash: published.LedgerHash})

	output, err := s.c.run(s.c.checkArgv("--challenge"))
	if err == nil || err.Error() != "pending root seq 2 leaves the accounts of finalized root 0 unverifiable; --demand sends the demand set" || len(s.c.transactions()) != 0 ||
		!strings.Contains(output, "  audit:             demand (tree check: ledger: account rows do not sum to the committed total: ") ||
		!strings.HasSuffix(output, "  previous accounts: 4 of root 0, unverifiable: the published objects are missing or do not reproduce the pending root\n") || strings.Contains(output, "no fault") {
		t.Fatalf("inconsistent subtree without --demand: %v\n%s", err, output)
	}
	seedV2Random(t, 1)
	if output, err = s.c.run(s.c.checkArgv("--challenge", "--demand")); err != nil || !strings.Contains(output, "demand set: 4 accounts of root 0 (0 doublings, 0 forced), 0 already demanded\n") {
		t.Fatalf("inconsistent subtree demands: %v\n%s", err, output)
	}
	want := v2DemandSet(t, s.f.Accounts, nil, 0, nil, 1)
	if got := s.demandedIndexes(t, 2, 0); !slices.Equal(got, want) || !slices.Equal(slices.Sorted(slices.Values(got)), []uint64{0, 1, 2, 3}) || s.c.pending.OpenDemands != 4 {
		t.Fatalf("demanded %v with %d open", got, s.c.pending.OpenDemands)
	}
	s.requireDemands(t, 0, s.f.Tree, s.f.Accounts, want)
	// Every opening of F's accounts crosses the inconsistent node.
	logs := s.c.open(t, 0, s.f.Tree, s.f.Accounts, 2, committed.path(2))
	requireDeleted(t, logs, 2, 0, 2, merkle.LedgerFaultInconsistentNode, 2, common.Address{0x0a})
}

// E-4: when the tree is withheld, every fault kind is blocked by a demand:
// F's four accounts are all demanded, in the order drawn, each paying the
// fee with its proof against F; its operator answers the accounts the root
// keeps, and opening a faulted account deletes the root with its fault.
func TestV2CheckWithheldFaults(t *testing.T) {
	// whole is F's four accounts in the order the seeded stream draws them.
	whole := func(s *v2CheckScenario) []uint64 {
		set := v2DemandSet(t, s.f.Accounts, nil, 0, nil, 2)
		if !slices.Equal(slices.Sorted(slices.Values(set)), []uint64{0, 1, 2, 3}) {
			t.Fatalf("the sample of F's four accounts is %v", set)
		}
		seedV2Random(t, 2)
		return set
	}
	cases := []struct {
		name   string
		mutate func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount
		faulty uint64
		fault  merkle.LedgerFault
	}{
		{"omitted account", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			return append(slices.Delete(slices.Clone(p), 3, 4), merkle.LedgerAccount{Coldkey: v2TestKey(99), Cumulative: p[3].Cumulative})
		}, 3, merkle.LedgerFaultWrongLeaf},
		{"lowered account", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			p = slices.Clone(p)
			p[2].Cumulative.SubUint64(&f[2].Cumulative, 1)
			return p
		}, 2, merkle.LedgerFaultLowerValue},
		{"garbage leaf", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			p = slices.Clone(p)
			p[0].Coldkey = [32]byte{0xde, 0xad}
			return p
		}, 0, merkle.LedgerFaultWrongLeaf},
		{"moved coldkey", func(f, p []merkle.LedgerAccount) []merkle.LedgerAccount {
			p = slices.Clone(p)
			p[1].Coldkey = p[5].Coldkey
			return p
		}, 1, merkle.LedgerFaultWrongLeaf},
	}
	for _, c := range cases {
		s := newV2CheckScenario(t)
		accounts := c.mutate(s.f.Accounts, s.p.Accounts)
		root, tree := s.publish(t, accounts)
		s.setPending(2, root)
		s.withhold()
		want := whole(s)
		output, err := s.c.run(s.c.checkArgv("--demand"))
		if err != nil || !strings.Contains(output, "  object missing:    "+ledger.Ref(root.LedgerHash).String()+"\n") || !strings.Contains(output, "  previous accounts: 4 of root 0, unverifiable") {
			t.Fatalf("%s: %v\n%s", c.name, err, output)
		}
		if got := s.demandedIndexes(t, 2, 0); !slices.Equal(got, want) {
			t.Fatalf("%s: demanded %v", c.name, got)
		}
		s.requireDemands(t, 0, s.f.Tree, s.f.Accounts, want)
		for i := range uint64(4) {
			if i == c.faulty {
				continue
			}
			path, _ := tree.Path(i)
			if logs := s.c.open(t, 0, s.f.Tree, s.f.Accounts, i, path); len(logs) != 1 || logs[0].Topics[0] != v2TestABI.Events["PathAnswered"].ID {
				t.Fatalf("%s: answering %d: %v", c.name, i, logs)
			}
		}
		if s.c.pending.OpenDemands != 1 {
			t.Fatalf("%s: %d demands open after the answers", c.name, s.c.pending.OpenDemands)
		}
		path, _ := tree.Path(c.faulty)
		requireDeleted(t, s.c.open(t, 0, s.f.Tree, s.f.Accounts, c.faulty, path), 2, 0, c.faulty, c.fault, 0, common.Address{0x0a})
	}
	// The inconsistent subtree, withheld, is demanded the same way.
	s := newV2CheckScenario(t)
	committed := newV2TestRawTree(t, s.p.Accounts)
	stated := committed.levels[1][1].Sum
	stated.SubUint64(&stated, 1)
	committed.misstate(1, 1, stated)
	root, total := committed.root()
	s.setPending(2, ledger.Root{Epoch: 12, Root: root, Total: total, LeafCount: 7, LedgerHash: [32]byte{0x99}})
	want := whole(s)
	if _, err := s.c.run(s.c.checkArgv("--demand")); err != nil || !slices.Equal(s.demandedIndexes(t, 2, 0), want) {
		t.Fatalf("withheld inconsistent subtree: %v", err)
	}
	requireDeleted(t, s.c.open(t, 0, s.f.Tree, s.f.Accounts, 3, committed.path(3)), 2, 0, 3, merkle.LedgerFaultInconsistentNode, 1, common.Address{0x0a})
}

// lowered publishes P with F's account 0 lowered by one rao below F, a
// provable fault, and returns its tree.
func (s *v2CheckScenario) lowered(t *testing.T) *merkle.LedgerSumTree {
	t.Helper()
	accounts := slices.Clone(s.p.Accounts)
	accounts[0].Cumulative.SubUint64(&s.f.Accounts[0].Cumulative, 1)
	root, tree := s.publish(t, accounts)
	s.setPending(2, root)
	return tree
}

// CL-1: provable faults without --challenge are reported and nothing is
// sent; --dry-run preflights the challenge and sends nothing.
func TestV2CheckFaultWithoutChallenge(t *testing.T) {
	s := newV2CheckScenario(t)
	s.lowered(t)
	output, err := s.c.run(s.c.checkArgv("--demand"))
	if err == nil || err.Error() != "pending root seq 2 faults 1 accounts of finalized root 0; --challenge deletes it" || len(s.c.transactions()) != 0 ||
		!strings.HasSuffix(output, "  previous accounts: 4 of root 0, 1 with a provable fault, 3 with no fault\n    account 0:      lower_value\n") {
		t.Fatalf("fault without --challenge: %v\n%s", err, output)
	}
	s = newV2CheckScenario(t)
	s.lowered(t)
	output, err = s.c.run(s.c.checkArgv("--challenge", "--dry-run"))
	if err != nil || len(s.c.transactions()) != 0 || s.c.pending.Seq != 2 || s.c.calls["eth_estimateGas:checkPath"] != 1 || !strings.HasSuffix(output, "dry run: preflight ok, nothing sent\n") {
		t.Fatalf("challenge dry run: %v; observed %s\n%s", err, s.c.observed(), output)
	}
}

// CL-3, SPEC A6: a challenge against a root that left the pending slot is
// done: replaced before the preflight, so that the path no longer opens the
// pending root, or deleted by another challenger. A challenge that reverts
// while the root is still pending fails, as does one whose receipt has no
// DistributionDeleted, or a send that fails.
func TestV2CheckChallengeReplacedRoot(t *testing.T) {
	for _, c := range []struct {
		name string
		at   string
		act  func(*v2CheckScenario)
		want string
		done bool
	}{
		{"replaced", "eth_call:checkPath", func(s *v2CheckScenario) {
			s.c.pending = stabi.ISTSettlementVault2Pending{Seq: 3, Root: s.p.Commit.Root, Total: s.p.Commit.Total.ToBig(), LeafCount: 7, Epoch: 13, LedgerHash: s.p.Commit.LedgerHash, CommittedBlock: v2TestHead}
		}, "pending root seq 2 is no longer pending (deleted or replaced); done\n", true},
		{"deleted", "eth_call:checkPath", func(s *v2CheckScenario) { s.c.pending = stabi.ISTSettlementVault2Pending{Total: new(big.Int)} }, "pending root seq 2 is no longer pending (deleted or replaced); done\n", true},
		{"deleted before inclusion", "send:checkPath", func(s *v2CheckScenario) { s.c.pending = stabi.ISTSettlementVault2Pending{Total: new(big.Int)} }, "pending root seq 2 is no longer pending (deleted or replaced); done\n", true},
		{"reverted while pending", "send:checkPath", func(s *v2CheckScenario) { s.c.roots[0].Root[0] ^= 1 }, " reverted on-chain (status 0, block 200000, gas used 50000)", false},
		{"no event", "send:checkPath", func(s *v2CheckScenario) { s.c.mute = true }, " succeeded but " + common.HexToAddress(v2TestVault).Hex() + " emitted no DistributionDeleted for seq 2", false},
		{"send refused", "eth_call:checkPath", func(s *v2CheckScenario) { s.c.failAt["eth_gasPrice"] = 1 }, "gas price: synthetic gas price refusal", false},
	} {
		s := newV2CheckScenario(t)
		s.lowered(t)
		s.c.before = func(key string) {
			if key == c.at {
				c.act(s)
			}
		}
		output, err := s.c.run(s.c.checkArgv("--challenge"))
		if c.done && (err != nil || !strings.HasSuffix(output, c.want) || len(s.c.emitted) != 0) {
			t.Fatalf("%s: %v\n%s", c.name, err, output)
		}
		if !c.done && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Fatalf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// A failed re-read of the pending root joins the refusal.
	s := newV2CheckScenario(t)
	s.lowered(t)
	s.c.revert["checkPath"] = "NoPendingRoot"
	s.c.failAt["eth_call:pending"] = 2
	if _, err := s.c.run(s.c.checkArgv("--challenge")); err == nil || !strings.Contains(err.Error(), "revert NoPendingRoot[]") || !strings.Contains(err.Error(), "\npending(3): synthetic refusal of eth_call:pending") {
		t.Fatalf("failed re-read: %v", err)
	}
}

// CL-1: published rows that reproduce a root with fewer accounts than F
// leave no path to open at a missing index.
func TestV2CheckChallengeMissingIndex(t *testing.T) {
	s := newV2CheckScenario(t)
	root, _ := s.publish(t, s.p.Accounts[:3])
	s.setPending(2, root)
	if _, err := s.c.run(s.c.checkArgv("--challenge")); err == nil || err.Error() != "the pending root has no account 3 to open: merkle: ledger account index out of range" || len(s.c.transactions()) != 0 {
		t.Fatalf("missing index: %v", err)
	}
}

// CL-3: when objects are missing, the accounts of F are reported
// unverifiable, never as having no fault, even when the published accounts
// reproduce the root; without --demand the check fails.
func TestV2CheckReportsUnverifiable(t *testing.T) {
	for _, missing := range []func(*v2CheckScenario) ledger.Ref{
		func(s *v2CheckScenario) ledger.Ref { return s.p.Epochs[1].Providers[0] },
		func(s *v2CheckScenario) ledger.Ref { return s.p.Commit.LedgerHash },
	} {
		s := newV2CheckScenario(t)
		ref := missing(s)
		delete(s.c.objects, ref)
		output, err := s.c.run(s.c.checkArgv("--challenge"))
		if err == nil || err.Error() != "pending root seq 2 leaves the accounts of finalized root 0 unverifiable; --demand sends the demand set" ||
			!strings.Contains(output, "  object missing:    "+ref.String()+"\n") || !strings.HasSuffix(output, "  previous accounts: 4 of root 0, unverifiable: the published objects are missing or do not reproduce the pending root\n") ||
			strings.Contains(output, "no fault") || len(s.c.transactions()) != 0 {
			t.Fatalf("missing %v: %v\n%s", ref, err, output)
		}
	}
}

// CL-3, VA-4: published rows that show a fault but do not reproduce the
// root leave F's accounts unverifiable; the rows' faults are reported and
// always demanded.
func TestV2CheckUnreproducedFaultyRows(t *testing.T) {
	s := newV2CheckScenario(t)
	s.lowered(t)
	s.c.pending.Root, s.c.pending.Total = s.p.Commit.Root, s.p.Commit.Total.ToBig()
	seedV2Random(t, 3)
	output, err := s.c.run(s.c.checkArgv("--challenge", "--demand"))
	want := v2DemandSet(t, s.f.Accounts, []ledger.Fault{{Index: 0, Kind: merkle.LedgerFaultLowerValue}}, 0, nil, 3)
	if err != nil || !strings.Contains(output, "  previous accounts: 4 of root 0, unverifiable: the published objects are missing or do not reproduce the pending root\n    account 0:      the published row shows lower_value\n") ||
		!strings.Contains(output, "demand set: 4 accounts of root 0 (0 doublings, 1 forced), 0 already demanded\n") || !slices.Equal(s.demandedIndexes(t, 2, 0), want) || !slices.Contains(want, 0) {
		t.Fatalf("unreproduced faulty rows: %v\n%s", err, output)
	}
	// After a root deleted with accounts 0 and 2 unanswered, the faulted
	// account 0 is forced once, beside account 2.
	s = newV2CheckScenario(t)
	other := common.Address{0xbb}
	s.c.demands(v2TestHead-990, 2, 0, other, false, 0, 2)
	s.c.addLog(v2TestHead-980, s.c.event("DistributionDeleted", s.c.seqTopics(2), big.NewInt(0), big.NewInt(3), uint8(2), big.NewInt(0), other))
	accounts := slices.Clone(s.p.Accounts)
	accounts[0].Cumulative.SubUint64(&s.f.Accounts[0].Cumulative, 1)
	root, _ := s.publish(t, accounts)
	s.setPending(3, root)
	s.c.pending.Root, s.c.pending.Total = s.p.Commit.Root, s.p.Commit.Total.ToBig()
	seedV2Random(t, 3)
	output, err = s.c.run(s.c.checkArgv("--challenge", "--demand"))
	want = v2DemandSet(t, s.f.Accounts, []ledger.Fault{{Index: 0, Kind: merkle.LedgerFaultLowerValue}}, 1, []uint64{0, 2}, 3)
	if err != nil || !strings.Contains(output, "demand set: 4 accounts of root 0 (1 doublings, 2 forced), 0 already demanded\n") || !slices.Equal(s.demandedIndexes(t, 3, 0), want) {
		t.Fatalf("faulted and unanswered before: %v\n%s", err, output)
	}
}

// CL-1: the receipt's events are matched to the transaction: logs of other
// addresses, other events and other roots or accounts are passed over.
func TestV2CheckReceiptEvents(t *testing.T) {
	foreign := func(s *v2CheckScenario, extra func() []*types.Log) {
		s.c.before = func(key string) {
			if key == "eth_getTransactionReceipt" {
				for _, receipt := range s.c.receipts {
					if len(receipt.Logs) == 1 {
						other := *receipt.Logs[0]
						other.Address = common.Address{0x99}
						receipt.Logs = append(append([]*types.Log{&other}, extra()...), receipt.Logs...)
					}
				}
			}
		}
	}
	s := newV2CheckScenario(t)
	s.lowered(t)
	foreign(s, func() []*types.Log {
		return []*types.Log{
			s.c.event("PathAnswered", s.c.seqTopics(2), big.NewInt(0), big.NewInt(0), common.Address{}),
			s.c.event("DistributionDeleted", s.c.seqTopics(1), big.NewInt(0), big.NewInt(0), uint8(2), big.NewInt(0), common.Address{}),
		}
	})
	if output, err := s.c.run(s.c.checkArgv("--challenge")); err != nil || !strings.Contains(output, "\nDistributionDeleted: pool 3, seq 2\n  account:     0 of finalized root 0\n") {
		t.Fatalf("challenge receipt: %v\n%s", err, output)
	}
	s = newV2CheckScenario(t)
	s.withhold()
	foreign(s, func() []*types.Log {
		return []*types.Log{
			s.c.event("PathAnswered", s.c.seqTopics(2), big.NewInt(0), big.NewInt(0), common.Address{}),
			s.c.event("PathDemanded", s.c.seqTopics(1), big.NewInt(0), big.NewInt(0), common.Address{}),
			s.c.event("PathDemanded", s.c.seqTopics(2), big.NewInt(0), big.NewInt(9), common.Address{}),
			s.c.event("PathDemanded", s.c.seqTopics(2), big.NewInt(1), big.NewInt(0), common.Address{}),
			s.c.event("PathDemanded", s.c.seqTopics(2), big.NewInt(1), big.NewInt(1), common.Address{}),
			s.c.event("PathDemanded", s.c.seqTopics(2), big.NewInt(1), big.NewInt(2), common.Address{}),
			s.c.event("PathDemanded", s.c.seqTopics(2), big.NewInt(1), big.NewInt(3), common.Address{}),
		}
	})
	if output, err := s.c.run(s.c.checkArgv("--demand")); err != nil || strings.Count(output, "PathDemanded: pool 3, seq 2, account ") != 4 {
		t.Fatalf("demand receipts: %v\n%s", err, output)
	}
}

// CL-1: a pending root that fails a check the watcher cannot act on, here
// an allocation that does not follow the funding, leaves F's accounts with
// no fault when the published accounts reproduce it.
func TestV2CheckRefusedRootWithoutFault(t *testing.T) {
	s := newV2CheckScenario(t)
	s.c.funded[11] = big.NewInt(1_600_001)
	output, err := s.c.run(s.c.checkArgv())
	if err != nil || !strings.Contains(output, "  audit:             refuse (allocation check: ledger: an account's increase is not its coldkey's delta: ") || !strings.HasSuffix(output, "  previous accounts: 4 of root 0, no fault\n") {
		t.Fatalf("refused root: %v\n%s", err, output)
	}
}

// CL-1: no pending root, a pool's first root (nothing to protect, even
// unpublished), and an unregistered pool.
func TestV2CheckWithoutPreviousRoot(t *testing.T) {
	s := newV2CheckScenario(t)
	s.c.pending = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
	output, err := s.c.run(s.c.checkArgv("--challenge", "--demand"))
	if err != nil || !strings.HasSuffix(output, "  pending root:      none; nothing to check\n") || s.c.observed() != "eth_call:pending=1 eth_call:pools=1 eth_chainId=1 eth_getBlockByNumber:finalized=1" {
		t.Fatalf("no pending root: %v; observed %s\n%s", err, s.c.observed(), output)
	}
	for _, publish := range []bool{true, false} {
		s := newV2CheckScenario(t)
		s.c.roots = nil
		s.setPending(1, s.f.Commit)
		if !publish {
			delete(s.c.objects, s.f.Commit.LedgerHash)
		}
		output, err := s.c.run(s.c.checkArgv("--challenge", "--demand"))
		want := map[bool]string{true: "  audit:             approve\n", false: "  audit:             refuse (fetch check: ledger: object unavailable: "}[publish]
		if err != nil || !strings.Contains(output, "  finalized root:    none; the pending root is the pool's first\n"+want) || strings.Contains(output, "previous accounts") || len(s.c.transactions()) != 0 {
			t.Fatalf("first root published %v: %v\n%s", publish, err, output)
		}
	}
	s = newV2CheckScenario(t)
	if _, err := s.c.run(s.c.checkArgv("--no_id=4")); err == nil || err.Error() != "pool 4 is not registered in vault2 "+common.HexToAddress(v2TestVault).Hex() {
		t.Fatalf("unregistered pool: %v", err)
	}
}

// CL-1: F's own objects missing or not rebuilding it leave its accounts
// impossible to check, challenge or demand.
func TestV2CheckPreviousUnavailable(t *testing.T) {
	s := newV2CheckScenario(t)
	delete(s.c.objects, s.f.Commit.LedgerHash)
	output, err := s.c.run(s.c.checkArgv("--challenge", "--demand"))
	if err == nil || err.Error() != "no finalized root of pool 3 has objects that rebuild it: its accounts cannot be checked, challenged or demanded" ||
		!strings.Contains(output, "  audit:             refuse (fetch check: ") || strings.Contains(output, "previous accounts") {
		t.Fatalf("previous unavailable: %v\n%s", err, output)
	}
}

// CL-1: failed state reads are refused, each with its view, as is a
// funding snapshot the verifier refuses; a covered range too long to
// verify is not read.
func TestV2CheckStateRefusals(t *testing.T) {
	for _, c := range []struct {
		key  string
		want string
	}{
		{"eth_getBlockByNumber:finalized", "read the finalized EVM head: synthetic block refusal"},
		{"eth_call:pools", "pools(3): synthetic refusal of eth_call:pools"},
		{"eth_call:pending", "pending(3): synthetic refusal of eth_call:pending"},
		{"eth_call:rootCount", "rootCount(3): synthetic refusal of eth_call:rootCount"},
		{"eth_call:roots", "roots(3,0): synthetic refusal of eth_call:roots"},
		{"eth_call:fundedThrough", "fundedThrough(3,11): synthetic refusal of eth_call:fundedThrough"},
	} {
		s := newV2CheckScenario(t)
		s.c.failAt[c.key] = 1
		if _, err := s.c.run(s.c.checkArgv()); err == nil || err.Error() != c.want {
			t.Fatalf("%s: %v, want %q", c.key, err, c.want)
		}
	}
	s := newV2CheckScenario(t)
	s.c.funded[12] = big.NewInt(1)
	if _, err := s.c.run(s.c.checkArgv()); err == nil || err.Error() != "verify the pending root: ledger: funding snapshots are missing, decreasing or not below 2^128: epoch 12 decreases" {
		t.Fatalf("decreasing funding: %v", err)
	}
	s = newV2CheckScenario(t)
	s.c.pending.Epoch = 10 + ledger.MaxCoveredEpochs + 1
	output, err := s.c.run(s.c.checkArgv())
	if err == nil || s.c.calls["eth_call:fundedThrough"] != 0 || !strings.Contains(output, "  audit:             demand (header check: ledger: header epoch is not the pending root's") {
		t.Fatalf("long range: %v; observed %s\n%s", err, s.c.observed(), output)
	}
}

// CL-1: malformed flags are refused before any RPC, --challenge and
// --demand need a relayer key, and an endpoint on another chain than
// --chain_id is refused before any read.
func TestV2CheckRefusesFlags(t *testing.T) {
	for _, c := range []struct {
		flags   []string
		refusal string
	}{
		{[]string{"--vault=0x12"}, `--vault: "0x12" is not a 20-byte hex address`},
		{[]string{"--no_id=-3"}, `--no_id: "-3" is not an unsigned 64-bit integer`},
		{[]string{"--api_url=api.example"}, `--api_url: "api.example" is not an http(s) URL`},
		{[]string{"--chain_id=0xz"}, `--chain_id: "0xz" is not a non-negative integer`},
		{[]string{"--gas_limit=-1"}, "--gas_limit: "},
		{[]string{"--challenge", "!--key_file"}, "--challenge and --demand send transactions: --key_file is required"},
		{[]string{"--demand", "!--key_file"}, "--challenge and --demand send transactions: --key_file is required"},
		{[]string{"--demand", "--key_file=/nonexistent/relayer.key"}, "key_file: "},
	} {
		s := newV2CheckScenario(t)
		if _, err := s.c.run(s.c.checkArgv(c.flags...)); err == nil || !strings.HasPrefix(err.Error(), c.refusal) || s.c.observed() != "" {
			t.Fatalf("%q: %v; observed %s", c.flags, err, s.c.observed())
		}
	}
	s := newV2CheckScenario(t)
	if _, err := s.c.run(s.c.checkArgv("--chain_id=964")); err == nil || err.Error() != "chain id mismatch: --chain_id=964 but "+s.c.rpc.URL+" reports 945" || s.c.observed() != "eth_chainId=1" {
		t.Fatalf("chain mismatch: %v; observed %s", err, s.c.observed())
	}
	s.c.rpc.Close()
	if _, err := s.c.run(s.c.checkArgv()); err == nil || !strings.HasPrefix(err.Error(), "no rpc endpoint reachable: ") {
		t.Fatalf("unreachable rpc: %v", err)
	}
}

// v2SampleScenario is pool 3 whose finalized root F (seq 2, root 0) has 600
// accounts, account i worth (i+1)·1,000,003 rao, finalized at block
// 120,000, and whose pending root (seq 5) is withheld.
type v2SampleScenario struct {
	c        *v2TestChain
	accounts []merkle.LedgerAccount
	tree     *merkle.LedgerSumTree
	f        ledger.Root
}

func newV2SampleScenario(t *testing.T) *v2SampleScenario {
	t.Helper()
	values := make([]uint64, 600)
	for i := range values {
		values[i] = uint64(i+1) * 1_000_003
	}
	s := &v2SampleScenario{c: newV2TestChain(t), accounts: v2TestAccounts(values...)}
	s.tree = v2TestTree(t, s.accounts)
	chunks, err := ledger.EncodeAccountChunks(s.accounts)
	if err != nil {
		t.Fatal(err)
	}
	header := &ledger.Header{NoID: v2TestPool, Epoch: 10, Epochs: []ledger.HeaderEpoch{{Epoch: 10, Providers: []ledger.Ref{}}}, Accounts: []ledger.Ref{ledger.RefOf(chunks[0])}}
	data, err := ledger.EncodeHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	s.c.objects[ledger.RefOf(chunks[0])], s.c.objects[ledger.RefOf(data)] = chunks[0], data
	root, total := s.tree.Root()
	s.f = ledger.Root{Epoch: 10, Root: root, Total: total, LeafCount: 600, LedgerHash: ledger.RefOf(data)}
	s.c.roots = []stabi.ISTSettlementVault2Root{v2TestRoot(s.tree, 10, s.f.LedgerHash)}
	for epoch := uint64(10); epoch <= 12; epoch++ {
		s.c.funded[epoch] = new(big.Int).Mul(total.ToBig(), new(big.Int).SetUint64(epoch-9))
	}
	s.c.committed(105_000, 2, s.f, 0)
	s.c.finalized(120_000, 2, 0, s.f)
	pending := ledger.Root{Epoch: 12, Root: [32]byte{0x55}, Total: *new(uint256.Int).Mul(&total, uint256.NewInt(3)), LeafCount: 650, LedgerHash: [32]byte{0x56}}
	s.c.pending = stabi.ISTSettlementVault2Pending{Seq: 5, Root: pending.Root, Total: pending.Total.ToBig(), LeafCount: 650, Epoch: 12, LedgerHash: pending.LedgerHash, CommittedBlock: v2TestHead - 1_000}
	s.c.committed(v2TestHead-1_000, 5, pending, 0)
	return s
}

// demands records demands on seq, against finalized root root, at block by
// demander, open or answered.
func (c *v2TestChain) demands(block, seq, root uint64, demander common.Address, answered bool, indexes ...uint64) {
	for _, i := range indexes {
		c.addLog(block, c.event("PathDemanded", c.seqTopics(seq), new(big.Int).SetUint64(root), new(big.Int).SetUint64(i), demander))
		if answered {
			c.addLog(block+1, c.event("PathAnswered", c.seqTopics(seq), new(big.Int).SetUint64(root), new(big.Int).SetUint64(i), demander))
		}
		if seq == c.pending.Seq {
			c.demanded[[3]uint64{seq, root, i}] = 1
			c.pending.OpenDemands++
		}
	}
}

func (s *v2SampleScenario) run(t *testing.T, flags ...string) ([]uint64, string) {
	t.Helper()
	seedV2Random(t, 9)
	output, err := s.c.run(s.c.checkArgv(append([]string{"--demand"}, flags...)...))
	if err != nil {
		t.Fatalf("demand: %v\n%s", err, output)
	}
	cs := &v2CheckScenario{c: s.c}
	return cs.demandedIndexes(t, 5, 0), output
}

// VA-4, E-4: a withheld tree is demanded with a value-weighted sample of
// 256 of F's accounts, in the order drawn, each paying the fee with its
// proof against F; the scan reads vault2's events in spans of at most 1,000
// blocks back to F's finalization.
func TestV2CheckDemandSample(t *testing.T) {
	s := newV2SampleScenario(t)
	indexes, output := s.run(t)
	if !slices.Equal(indexes, v2DemandSet(t, s.accounts, nil, 0, nil, 9)) || len(indexes) != 256 || slices.IsSorted(indexes) ||
		!strings.Contains(output, "demand set: 256 accounts of root 0 (0 doublings, 0 forced), 0 already demanded\n") || s.c.pending.OpenDemands != 256 {
		t.Fatalf("withheld root: %d demands, %d open\n%s", len(indexes), s.c.pending.OpenDemands, output)
	}
	(&v2CheckScenario{c: s.c}).requireDemands(t, 0, s.tree, s.accounts, indexes)
	// Weighted by value: the upper half of the accounts holds 3/4 of it.
	upper := 0
	for _, i := range indexes {
		if i >= 300 {
			upper++
		}
	}
	if upper < 160 {
		t.Fatalf("%d of 256 demands in the upper half", upper)
	}
	if spans := s.c.spans; len(spans) != 81 || spans[0] != [2]uint64{199_001, 200_000} || spans[80] != [2]uint64{119_001, 120_000} {
		t.Fatalf("log spans %v", spans)
	}
	// A watcher run again on the same root draws no new sample.
	s.c.calls, s.c.spans = map[string]int{}, nil
	sent := len(s.c.transactions())
	if output, err := s.c.run(s.c.checkArgv("--demand")); err != nil || len(s.c.transactions()) != sent || !strings.Contains(output, "demand set: 0 accounts of root 0 (0 doublings, 0 forced), 256 already demanded\n") {
		t.Fatalf("rerun: %v\n%s", err, output)
	}
}

// VA-4, E-4: the sample doubles for each earlier pending root over F that
// was replaced or deleted while a demand was open, read from
// DistributionCommitted.replacedSeq, DistributionDeleted, PathDemanded and
// PathAnswered since F's finalization, and every index such a root left
// unanswered is demanded again, after the sample when the sample does not
// draw it; it never exceeds F's accounts.
func TestV2CheckDemandDoubling(t *testing.T) {
	relayer, other := common.HexToAddress(testKeyAddr), common.Address{0xbb}
	earlier := ledger.Root{Epoch: 11, Root: [32]byte{0x33}, Total: *uint256.NewInt(1), LeafCount: 600, LedgerHash: [32]byte{0x34}}
	for _, c := range []struct {
		name      string
		history   func(*v2SampleScenario)
		count     int
		doublings uint
		forced    []uint64
		report    string
	}{
		{"replaced with an open demand", func(s *v2SampleScenario) {
			s.c.committed(130_000, 3, earlier, 0)
			s.c.demands(131_000, 3, 0, other, false, 7)
			s.c.demands(131_000, 3, 0, other, true, 9)
			s.c.committed(150_000, 4, earlier, 3)
			s.c.addLog(160_000, s.c.event("DistributionDeleted", s.c.seqTopics(4), big.NewInt(0), big.NewInt(1), uint8(2), big.NewInt(0), other))
		}, 513, 1, []uint64{7}, "demand set: 513 accounts of root 0 (1 doublings, 1 forced), 0 already demanded\n"},
		{"replaced and deleted with open demands", func(s *v2SampleScenario) {
			s.c.committed(130_000, 3, earlier, 0)
			s.c.demands(131_000, 3, 0, relayer, false, 7)
			s.c.committed(150_000, 4, earlier, 3)
			s.c.demands(151_000, 4, 0, other, false, 11, 7)
			s.c.addLog(160_000, s.c.event("DistributionDeleted", s.c.seqTopics(4), big.NewInt(0), big.NewInt(11), uint8(2), big.NewInt(0), other))
			// A repeated log does not count a root twice.
			s.c.addLog(160_000, s.c.event("DistributionDeleted", s.c.seqTopics(4), big.NewInt(0), big.NewInt(11), uint8(2), big.NewInt(0), other))
		}, 600, 2, []uint64{7, 11}, "demand set: 600 accounts of root 0 (2 doublings, 2 forced), 0 already demanded\n"},
		{"answered demands and history before F", func(s *v2SampleScenario) {
			s.c.demands(100_000, 1, 0, other, false, 5)
			s.c.committed(101_000, 6, earlier, 1)
			s.c.committed(130_000, 3, earlier, 0)
			s.c.demands(131_000, 3, 0, other, true, 9, 10)
			s.c.committed(150_000, 4, earlier, 3)
		}, 256, 0, nil, "demand set: 256 accounts of root 0 (0 doublings, 0 forced), 0 already demanded\n"},
	} {
		s := newV2SampleScenario(t)
		c.history(s)
		indexes, output := s.run(t)
		if len(indexes) != c.count || !slices.Equal(indexes, v2DemandSet(t, s.accounts, nil, c.doublings, c.forced, 9)) || !strings.Contains(output, c.report) {
			t.Fatalf("%s: %d demands\n%s", c.name, len(indexes), output)
		}
		for _, i := range c.forced {
			if !slices.Contains(indexes, i) {
				t.Fatalf("%s: unanswered index %d not demanded again", c.name, i)
			}
		}
	}
}

// VA-4, SPEC A6: indexes already demanded on the pending root, by anyone,
// are not demanded again, and a sender that holds fewer demands there than
// the sample size draws a new sample for the rest, sent in the order drawn.
// One that holds as many, earlier unanswered indexes included, draws none.
func TestV2CheckDemandTopsUp(t *testing.T) {
	relayer := common.HexToAddress(testKeyAddr)
	s := newV2SampleScenario(t)
	var mine, theirs []uint64
	for i := range uint64(100) {
		mine = append(mine, i)
		theirs = append(theirs, 599-i)
	}
	s.c.demands(v2TestHead-500, 5, 0, relayer, false, mine...)
	s.c.demands(v2TestHead-500, 5, 0, common.Address{0xbb}, false, theirs...)
	indexes, output := s.run(t)
	want := slices.DeleteFunc(v2DemandSet(t, s.accounts, nil, 0, nil, 9), func(i uint64) bool { return i < 100 || i >= 500 })
	if len(indexes) == 0 || !slices.Equal(indexes, want) || !strings.Contains(output, "demand set: 256 accounts of root 0 (0 doublings, 0 forced), 200 already demanded\n") {
		t.Fatalf("top-up: %d demands\n%s", len(indexes), output)
	}
	// A root replaced with accounts 0 … 99 unanswered doubles the sample to
	// 512, and the relayer already demanded accounts 0 … 511 on this one.
	s = newV2SampleScenario(t)
	earlier := ledger.Root{Epoch: 11, Root: [32]byte{0x33}, Total: *uint256.NewInt(1), LeafCount: 600, LedgerHash: [32]byte{0x34}}
	s.c.committed(130_000, 3, earlier, 0)
	s.c.demands(131_000, 3, 0, common.Address{0xbb}, false, mine...)
	s.c.committed(150_000, 4, earlier, 3)
	mine = nil
	for i := range uint64(512) {
		mine = append(mine, i)
	}
	s.c.demands(v2TestHead-500, 5, 0, relayer, false, mine...)
	if indexes, output = s.run(t); len(indexes) != 0 || !strings.Contains(output, "demand set: 100 accounts of root 0 (1 doublings, 100 forced), 512 already demanded\n") {
		t.Fatalf("a held sample: %d demands\n%s", len(indexes), output)
	}
}

// CL-1, SPEC A6: an index demanded by another sender between the scan and
// the preflight is done; a root that leaves the pending slot ends the
// demands; a preflight revert, a mined revert, a receipt without
// PathDemanded, a failed send, failed reads and a failed random source are
// refusals; --dry-run sends nothing. The seeded sample draws F's four
// accounts in the order order.
func TestV2CheckDemandRefusals(t *testing.T) {
	t.Cleanup(func() { v2Random = crand.Reader })
	order := v2DemandSet(t, newV2CheckScenario(t).f.Accounts, nil, 0, nil, 1)
	withheld := func(t *testing.T) *v2CheckScenario {
		s := newV2CheckScenario(t)
		s.withhold()
		seedV2Random(t, 1)
		return s
	}
	s := withheld(t)
	s.c.before = func(key string) {
		if key == "eth_call:demandPath" && s.c.calls[key] == 1 {
			s.c.demanded[[3]uint64{2, 0, order[0]}] = 1
		}
	}
	output, err := s.c.run(s.c.checkArgv("--demand"))
	if err != nil || !strings.Contains(output, fmt.Sprintf("account %d of root 0: already demanded on seq 2; done\n", order[0])) || !slices.Equal(s.demandedIndexes(t, 2, 0), order[1:]) || order[0] == 0 {
		t.Fatalf("demanded first: %v\n%s", err, output)
	}
	s = withheld(t)
	s.c.before = func(key string) {
		if key == "eth_call:demandPath" && s.c.calls[key] == 3 {
			s.c.pending = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
		}
	}
	output, err = s.c.run(s.c.checkArgv("--demand"))
	if err != nil || !strings.HasSuffix(output, "pending root seq 2 is no longer pending (deleted or replaced); no more demands\n") || !slices.Equal(s.demandedIndexes(t, 2, 0), order[:2]) {
		t.Fatalf("deleted root: %v\n%s", err, output)
	}
	s = withheld(t)
	output, err = s.c.run(s.c.checkArgv("--demand", "--dry-run"))
	if err != nil || len(s.c.transactions()) != 0 || s.c.calls["eth_estimateGas:demandPath"] != 4 || strings.Count(output, "dry run: preflight ok, nothing sent\n") != 4 || !strings.Contains(output, fmt.Sprintf("\n  fee:         %d wei\n", v2TestFee)) {
		t.Fatalf("dry run: %v; observed %s\n%s", err, s.c.observed(), output)
	}
	for _, c := range []struct {
		name  string
		setup func(*v2CheckScenario)
		want  string
	}{
		{"preflight revert", func(s *v2CheckScenario) { s.c.revert["demandPath"] = "ChallengeWindowClosed" }, "preflight eth_call failed: execution reverted: revert ChallengeWindowClosed[]"},
		{"mined revert", func(s *v2CheckScenario) {
			s.c.before = func(key string) {
				if key == "send:demandPath" {
					s.c.fee = big.NewInt(1)
				}
			}
		}, " reverted on-chain (status 0, block 200000, gas used 50000)"},
		{"no event", func(s *v2CheckScenario) { s.c.mute = true }, fmt.Sprintf(" emitted no PathDemanded for seq 2 account %d of root 0", order[0])},
		{"send refused", func(s *v2CheckScenario) { s.c.failAt["eth_gasPrice"] = 1 }, "gas price: synthetic gas price refusal"},
		{"fee read", func(s *v2CheckScenario) { s.c.failAt["eth_call:demandFeeWei"] = 1 }, "demandFeeWei(): synthetic refusal of eth_call:demandFeeWei"},
		{"window read", func(s *v2CheckScenario) { s.c.failAt["eth_call:CHALLENGE_BLOCKS"] = 1 }, "CHALLENGE_BLOCKS(): synthetic refusal of eth_call:CHALLENGE_BLOCKS"},
		{"latest block read", func(s *v2CheckScenario) { s.c.failAt["eth_getBlockByNumber:latest"] = 1 }, "read the latest EVM block: synthetic block refusal"},
		{"pending re-read", func(s *v2CheckScenario) {
			s.c.revert["demandPath"] = "ChallengeWindowClosed"
			s.c.failAt["eth_call:pending"] = 2
		}, "\npending(3): synthetic refusal of eth_call:pending"},
		{"demanded re-read", func(s *v2CheckScenario) {
			s.c.revert["demandPath"] = "ChallengeWindowClosed"
			s.c.failAt["eth_call:demanded"] = 1
		}, fmt.Sprintf("\ndemanded(3,2,0,%d): synthetic refusal of eth_call:demanded", order[0])},
		{"log refused", func(s *v2CheckScenario) { s.c.failAt["eth_getLogs"] = 3 }, "eth_getLogs [197001,198000]: synthetic log refusal"},
		{"malformed log", func(s *v2CheckScenario) {
			log := s.c.event("PathDemanded", s.c.seqTopics(2), big.NewInt(0), big.NewInt(0), common.Address{})
			log.Data = log.Data[:10]
			s.c.addLog(v2TestHead-10, log)
		}, "vault2 log 3 of tx 0x7e03000000000000000000000000000000000000000000000000000000000000 at block 199990: abi: "},
		{"no finalization", func(s *v2CheckScenario) { s.c.logs = s.c.logs[2:] }, "vault2 logs up to block 200000 hold no DistributionFinalized of root 0 of pool 3"},
		{"random source", func(s *v2CheckScenario) { v2Random = io.MultiReader() }, "ledger: demand sample randomness: EOF"},
	} {
		s := withheld(t)
		c.setup(s)
		if _, err := s.c.run(s.c.checkArgv("--demand")); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// CL-1, E-4 (§5 Senders): a demand's preflight runs at the latest block and
// the demand in a later one, so demands are sent only while the next block
// is inside the challenge window, and its end stops them cleanly: the check
// says so and succeeds. With the latest block two before the window's last,
// each demand mined in the next block, the two that run inside the window
// are sent, the first two of the set in the order drawn, and the rest is
// left unsent; with the window's last block latest, where a preflight still
// passes, or the window already closed, nothing is preflighted or sent.
func TestV2CheckDemandsOnlyInsideWindow(t *testing.T) {
	s := newV2CheckScenario(t)
	s.withhold()
	s.c.pending.CommittedBlock = v2TestHead + 3 - v2TestChallengeBlocks
	s.c.before = func(key string) {
		if key == "send:demandPath" {
			s.c.head++
		}
	}
	seedV2Random(t, 1)
	output, err := s.c.run(s.c.checkArgv("--demand"))
	if want := fmt.Sprintf("challenge window closing: pending root seq 2 takes demands through block %d, and the next block is %d; no more demands sent\n", v2TestHead+2, v2TestHead+3); err != nil || !strings.HasSuffix(output, want) ||
		!slices.Equal(s.demandedIndexes(t, 2, 0), v2DemandSet(t, s.f.Accounts, nil, 0, nil, 1)[:2]) || s.c.calls["eth_call:demandPath"] != 2 || s.c.pending.OpenDemands != 2 {
		t.Fatalf("closing window: %v\n%s", err, output)
	}
	for _, c := range []struct {
		name      string
		committed uint64
		want      string
	}{
		{"window's last block latest", v2TestHead + 1 - v2TestChallengeBlocks, fmt.Sprintf("through block %d, and the next block is %d", v2TestHead, v2TestHead+1)},
		{"window closed", v2TestHead - v2TestChallengeBlocks, fmt.Sprintf("through block %d, and the next block is %d", v2TestHead-1, v2TestHead+1)},
	} {
		s = newV2CheckScenario(t)
		s.withhold()
		s.c.pending.CommittedBlock = c.committed
		output, err = s.c.run(s.c.checkArgv("--demand"))
		if err != nil || !strings.HasSuffix(output, "challenge window closing: pending root seq 2 takes demands "+c.want+"; no more demands sent\n") ||
			len(s.c.transactions()) != 0 || s.c.calls["eth_call:demandPath"]+s.c.calls["eth_estimateGas:demandPath"] != 0 {
			t.Fatalf("%s: %v\n%s", c.name, err, output)
		}
	}
}

// CL-1: a log the scan cannot place in the demand history is refused.
func TestV2CheckForeignLog(t *testing.T) {
	for _, log := range []types.Log{
		{Address: common.HexToAddress(v2TestVault), BlockNumber: v2TestHead - 5, TxHash: common.Hash{0x7f}},
		{Address: common.HexToAddress(v2TestVault), BlockNumber: v2TestHead - 5, TxHash: common.Hash{0x7f}, Topics: []common.Hash{{0x01}}},
	} {
		if _, err := decodeV2Event(&log); err == nil || err.Error() != "vault2 log 0 of tx 0x7f00000000000000000000000000000000000000000000000000000000000000 at block 199995: not an event of the demand history" {
			t.Fatalf("foreign log %v: %v", log.Topics, err)
		}
	}
	for _, name := range []string{"DistributionCommitted", "DistributionDeleted", "PathDemanded", "PathAnswered", "DistributionFinalized"} {
		log := types.Log{Address: common.HexToAddress(v2TestVault), Topics: []common.Hash{v2TestABI.Events[name].ID, common.BigToHash(big.NewInt(v2TestPool)), {}}, Data: []byte{1}}
		if _, err := decodeV2Event(&log); err == nil || !strings.HasPrefix(err.Error(), "vault2 log 0 of tx 0x0000000000000000000000000000000000000000000000000000000000000000 at block 0: abi: ") {
			t.Fatalf("malformed %s: %v", name, err)
		}
	}
}

// CL-1: each reason vault2 refuses a challenge or a demand is decoded by
// name from the preflight; with the pending root still pending and the
// index not demanded, the refusal stands and nothing is sent.
func TestV2CheckDecodesRevert(t *testing.T) {
	for _, reason := range []string{"NoPendingRoot", "UnknownRoot", "IndexOutOfRange", "InvalidProof", "NoFault", "Reentrancy"} {
		s := newV2CheckScenario(t)
		s.lowered(t)
		s.c.revert["checkPath"] = reason
		if _, err := s.c.run(s.c.checkArgv("--challenge")); err == nil || !strings.HasPrefix(err.Error(), "preflight eth_call failed: execution reverted: ") || !strings.HasSuffix(err.Error(), "revert "+reason+"[]") || len(s.c.transactions()) != 0 {
			t.Fatalf("checkPath %s: %v", reason, err)
		}
	}
	for _, reason := range []string{"InvalidDemandFee", "NoPendingRoot", "UnknownRoot", "ChallengeWindowClosed", "IndexOutOfRange", "InvalidProof", "AlreadyDemanded"} {
		s := newV2CheckScenario(t)
		s.withhold()
		s.c.revert["demandPath"] = reason
		if _, err := s.c.run(s.c.checkArgv("--demand")); err == nil || !strings.HasPrefix(err.Error(), "preflight eth_call failed: execution reverted: ") || !strings.HasSuffix(err.Error(), "revert "+reason+"[]") || len(s.c.transactions()) != 0 {
			t.Fatalf("demandPath %s: %v", reason, err)
		}
	}
}

// newV2FallbackScenario is the check scenario a root later: F of epoch 10
// is root 0 and P of epochs 11 and 12 is root 1, the latest, finalized by
// seq 2 at v2TestHead − 10,000. Q, the honest root of epoch 13 over P, is
// returned unpublished; the pending root is set by each test.
func newV2FallbackScenario(t *testing.T) (*v2CheckScenario, *ledger.Ledger) {
	t.Helper()
	s := newV2CheckScenario(t)
	s.c.roots = append(s.c.roots, v2TestRoot(s.p.Tree, 12, s.p.Commit.LedgerHash))
	s.c.finalized(v2TestHead-10_000, 2, 1, s.p.Commit)
	s.c.funded[13] = big.NewInt(3_000_000)
	q, err := ledger.Build(context.Background(), ledger.BuildInput{NoID: v2TestPool, Epoch: 13, FirstEpoch: 10, FundedThrough: v2Funded(10, 1_000_003, 11, 1_600_000, 12, 2_500_000, 13, 3_000_000),
		Previous: &ledger.Previous{Root: s.p.Commit, Accounts: s.p.Accounts},
		Epochs:   []ledger.EpochInput{{Epoch: 13, Rows: []protocol.LedgerUsageRow{v2Row(1, 1, 100), v2Row(14, 9, 50)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Accounts) != 8 {
		t.Fatalf("Q has %d accounts", len(q.Accounts))
	}
	return s, q
}

// H1, E-4: while the latest finalized root's objects are available, the
// watcher protects its accounts, including those it added: a pending root
// that lowers one of them is challenged against root 1.
func TestV2CheckProtectsLatestRoot(t *testing.T) {
	s, q := newV2FallbackScenario(t)
	accounts := slices.Clone(q.Accounts)
	accounts[5].Cumulative.SubUint64(&s.p.Accounts[5].Cumulative, 1)
	root, tree := s.publishUnder(t, q.Header, accounts)
	s.setPending(3, root)
	output, err := s.c.run(s.c.checkArgv("--challenge"))
	if err != nil || strings.Contains(output, "fallback") || !strings.Contains(output, "  previous accounts: 7 of root 1, 1 with a provable fault, 6 with no fault\n    account 5:      lower_value\n") ||
		!strings.Contains(output, "\nDistributionDeleted: pool 3, seq 3\n  account:     5 of finalized root 1\n  fault:       lower_value at level 0\n") {
		t.Fatalf("latest root: %v\n%s", err, output)
	}
	requireDeleted(t, s.c.emitted, 3, 1, 5, merkle.LedgerFaultLowerValue, 0, common.HexToAddress(testKeyAddr))
	path, _ := tree.Path(5)
	requireV2Tx(t, s.c.transactions()[0], 7, 60_000, 0, checkPathCalldata(t, 1, s.p.Tree, s.p.Accounts, 5, path))
}

// H1, E-4: when the latest finalized root's objects are withheld, the
// watcher says so and protects the newest older root whose objects verify:
// a withheld pending root is demanded against root 0, each demand naming
// root 0 with its account and proof. Demands are keyed by seq, root and
// index: one on root 0 is skipped, one on root 1 is not, and an earlier
// failed root's unanswered demands are demanded again where root 0 holds
// the account.
func TestV2CheckFallbackDemands(t *testing.T) {
	s, _ := newV2FallbackScenario(t)
	delete(s.c.objects, s.p.Commit.LedgerHash)
	earlier := ledger.Root{Epoch: 13, Root: [32]byte{0x33}, Total: *uint256.NewInt(1), LeafCount: 8, LedgerHash: [32]byte{0x34}}
	s.c.committed(v2TestHead-9_000, 3, earlier, 0)
	s.c.demands(v2TestHead-8_900, 3, 1, common.Address{0xbb}, false, 5, 2)
	s.c.demands(v2TestHead-8_900, 3, 0, common.Address{0xbb}, true, 3)
	s.c.committed(v2TestHead-6_000, 4, earlier, 3)
	s.c.addLog(v2TestHead-5_000, s.c.event("DistributionDeleted", s.c.seqTopics(4), big.NewInt(1), big.NewInt(6), uint8(2), big.NewInt(0), common.Address{0xbb}))
	s.setPending(5, ledger.Root{Epoch: 13, Root: [32]byte{0x13}, Total: *uint256.NewInt(3_000_000), LeafCount: 8, LedgerHash: [32]byte{0x31}})
	s.c.demands(v2TestHead-900, 5, 1, common.HexToAddress(testKeyAddr), false, 2)
	s.c.demands(v2TestHead-900, 5, 0, common.Address{0xbb}, false, 1)
	seedV2Random(t, 4)
	output, err := s.c.run(s.c.checkArgv("--demand"))
	for _, line := range []string{
		"  audit:             refuse (fetch check: ledger: object unavailable: " + ledger.Ref(s.p.Commit.LedgerHash).String(),
		"  fallback:          finalized root 1's objects are missing or do not rebuild it; protecting finalized root 0, the newest whose objects do\n",
		"  pending accounts:  unavailable: object " + ledger.Ref([32]byte{0x31}).String() + ": GET ",
		"  previous accounts: 4 of root 0, unverifiable: the published objects are missing or do not reproduce the pending root\n",
		"demand set: 4 accounts of root 0 (1 doublings, 1 forced), 1 already demanded\n  1 unanswered earlier demands name accounts that root 0 does not hold\n",
		"\n  account:     0 of finalized root 0: coldkey ",
	} {
		if err != nil || !strings.Contains(output, line) {
			t.Fatalf("fallback demands: %v; output lacks %q:\n%s", err, line, output)
		}
	}
	want := slices.DeleteFunc(v2DemandSet(t, s.f.Accounts, nil, 1, []uint64{2}, 4), func(i uint64) bool { return i == 1 })
	if got := s.demandedIndexes(t, 5, 0); !slices.Equal(got, want) || !slices.Equal(slices.Sorted(slices.Values(want)), []uint64{0, 2, 3}) {
		t.Fatalf("demanded %v on root 0", got)
	}
	s.requireDemands(t, 0, s.f.Tree, s.f.Accounts, want)
}

// H1, E-4: with the latest root withheld, a published pending root whose
// rows fault an account of the older root it protects is challenged against
// that root; without --challenge the fault is reported.
func TestV2CheckFallbackChallenge(t *testing.T) {
	setup := func(t *testing.T) (*v2CheckScenario, *merkle.LedgerSumTree) {
		s, q := newV2FallbackScenario(t)
		delete(s.c.objects, s.p.Commit.LedgerHash)
		accounts := slices.Clone(q.Accounts)
		accounts[1].Cumulative.SubUint64(&s.f.Accounts[1].Cumulative, 1)
		root, tree := s.publishUnder(t, q.Header, accounts)
		s.setPending(3, root)
		return s, tree
	}
	s, _ := setup(t)
	output, err := s.c.run(s.c.checkArgv())
	if err == nil || err.Error() != "pending root seq 3 faults 1 accounts of finalized root 0; --challenge deletes it" || len(s.c.transactions()) != 0 ||
		!strings.Contains(output, "protecting finalized root 0, the newest whose objects do\n  previous accounts: 4 of root 0, 1 with a provable fault, 3 with no fault\n    account 1:      lower_value\n") {
		t.Fatalf("fallback fault: %v\n%s", err, output)
	}
	s, tree := setup(t)
	output, err = s.c.run(s.c.checkArgv("--challenge"))
	if err != nil || !strings.Contains(output, "\nDistributionDeleted: pool 3, seq 3\n  account:     1 of finalized root 0\n") {
		t.Fatalf("fallback challenge: %v\n%s", err, output)
	}
	requireDeleted(t, s.c.emitted, 3, 0, 1, merkle.LedgerFaultLowerValue, 0, common.HexToAddress(testKeyAddr))
	path, _ := tree.Path(1)
	requireV2Tx(t, s.c.transactions()[0], 7, 60_000, 0, checkPathCalldata(t, 0, s.f.Tree, s.f.Accounts, 1, path))
}

// H1: a published pending root that keeps every account of the protected
// older root shows no fault there, but the accounts the withheld latest
// root added cannot be checked, which fails the check.
func TestV2CheckFallbackWithoutFault(t *testing.T) {
	s, q := newV2FallbackScenario(t)
	delete(s.c.objects, s.p.Commit.LedgerHash)
	root, _ := s.publishUnder(t, q.Header, q.Accounts)
	s.setPending(3, root)
	output, err := s.c.run(s.c.checkArgv("--challenge", "--demand"))
	if err == nil || err.Error() != "finalized root 1's objects are missing or do not rebuild it: the accounts added after root 0 cannot be checked" ||
		!strings.HasSuffix(output, "  previous accounts: 4 of root 0, no fault\n") || len(s.c.transactions()) != 0 {
		t.Fatalf("fallback without fault: %v\n%s", err, output)
	}
}

// H1: the fallback passes over every older root whose objects cannot be
// read or do not rebuild it, saying why: a missing header, bytes that do
// not match their reference, a header that does not decode, a missing or
// malformed account chunk, no accounts, and accounts of another root. With
// no root left, nothing can be protected; a failed root read is refused.
func TestV2CheckFallbackRefusals(t *testing.T) {
	cases := []struct {
		name   string
		break1 func(s *v2CheckScenario) []byte // returns a new header for root 1, or nil
		want   string
	}{
		{"header missing", func(s *v2CheckScenario) []byte { delete(s.c.objects, s.p.Commit.LedgerHash); return nil }, ": 404 Not Found"},
		{"header bytes", func(s *v2CheckScenario) []byte { s.c.objects[s.p.Commit.LedgerHash] = []byte("{}"); return nil }, ": the bytes served do not match it"},
		{"header decode", func(s *v2CheckScenario) []byte { return []byte(`{"schema":`) }, ": ledger: header does not decode"},
		{"chunk missing", func(s *v2CheckScenario) []byte {
			header, _ := ledger.DecodeHeader(s.p.Header)
			delete(s.c.objects, header.Accounts[0])
			return nil
		}, ": 404 Not Found"},
		{"chunk length", func(s *v2CheckScenario) []byte {
			header, _ := ledger.DecodeHeader(s.p.Header)
			chunk := []byte{1, 2, 3}
			s.c.objects[ledger.RefOf(chunk)] = chunk
			header.Accounts = []ledger.Ref{ledger.RefOf(chunk)}
			data, _ := ledger.EncodeHeader(header)
			return data
		}, ": ledger: chunk is not 1 to 131,072 whole rows"},
		{"no accounts", func(s *v2CheckScenario) []byte {
			header, _ := ledger.DecodeHeader(s.p.Header)
			header.Accounts = []ledger.Ref{}
			data, _ := ledger.EncodeHeader(header)
			return data
		}, ": merkle: ledger tree needs 1 to 2^29 accounts"},
		{"another root's accounts", func(s *v2CheckScenario) []byte { return s.f.Header }, "its published accounts do not rebuild it"},
	}
	for _, c := range cases {
		s, q := newV2FallbackScenario(t)
		// Q is root 2, the latest, finalized and withheld.
		s.c.roots = append(s.c.roots, v2TestRoot(q.Tree, 13, q.Commit.LedgerHash))
		s.c.finalized(v2TestHead-5_000, 3, 2, q.Commit)
		s.c.funded[14] = big.NewInt(3_500_000)
		s.setPending(4, ledger.Root{Epoch: 14, Root: [32]byte{0x14}, Total: *uint256.NewInt(3_500_000), LeafCount: 8, LedgerHash: [32]byte{0x41}})
		if header := c.break1(s); header != nil {
			s.c.objects[ledger.RefOf(header)] = header
			s.c.roots[1].LedgerHash = ledger.RefOf(header)
		}
		output, err := s.c.run(s.c.checkArgv())
		_, reason, _ := strings.Cut(output, "  fallback:          finalized root 1 is unverifiable too: ")
		reason, rest, _ := strings.Cut(reason, "\n")
		if err == nil || err.Error() != "pending root seq 4 leaves the accounts of finalized root 0 unverifiable; --demand sends the demand set" || !strings.Contains(reason, c.want) ||
			!strings.HasPrefix(rest, "  fallback:          finalized root 2's objects are missing or do not rebuild it; protecting finalized root 0, the newest whose objects do\n") {
			t.Fatalf("%s: %v\n%s", c.name, err, output)
		}
	}
	// No finalized root left to protect.
	s, _ := newV2FallbackScenario(t)
	delete(s.c.objects, s.p.Commit.LedgerHash)
	delete(s.c.objects, s.f.Commit.LedgerHash)
	s.setPending(3, ledger.Root{Epoch: 13, Root: [32]byte{0x13}, Total: *uint256.NewInt(1), LeafCount: 8, LedgerHash: [32]byte{0x31}})
	output, err := s.c.run(s.c.checkArgv("--demand"))
	if err == nil || err.Error() != "no finalized root of pool 3 has objects that rebuild it: its accounts cannot be checked, challenged or demanded" || !strings.Contains(output, "  fallback:          finalized root 0 is unverifiable too: object ") || strings.Contains(output, "previous accounts") {
		t.Fatalf("no root: %v\n%s", err, output)
	}
	s, _ = newV2FallbackScenario(t)
	delete(s.c.objects, s.p.Commit.LedgerHash)
	s.c.failAt["eth_call:roots"] = 2
	if _, err := s.c.run(s.c.checkArgv()); err == nil || err.Error() != "roots(3,0): synthetic refusal of eth_call:roots" {
		t.Fatalf("root read: %v", err)
	}
}

// H1, CL-1: a receipt's PathDemanded must name the protected root: one of
// the same seq and index on another root is not the demand sent.
func TestV2CheckDemandReceiptRoot(t *testing.T) {
	s := newV2CheckScenario(t)
	s.withhold()
	seedV2Random(t, 1)
	first := v2DemandSet(t, s.f.Accounts, nil, 0, nil, 1)[0]
	s.c.before = func(key string) {
		if key == "eth_getTransactionReceipt" {
			for _, receipt := range s.c.receipts {
				receipt.Logs = []*types.Log{s.c.event("PathDemanded", s.c.seqTopics(2), big.NewInt(1), new(big.Int).SetUint64(first), common.HexToAddress(testKeyAddr))}
			}
		}
	}
	if _, err := s.c.run(s.c.checkArgv("--demand")); err == nil || !strings.HasSuffix(err.Error(), fmt.Sprintf(" emitted no PathDemanded for seq 2 account %d of root 0", first)) {
		t.Fatalf("demand receipt on another root: %v", err)
	}
}
