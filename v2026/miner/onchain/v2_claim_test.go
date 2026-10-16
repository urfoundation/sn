// CL-1: `snclaim v2 status` and `snclaim v2 claim` against v2TestChain.
package onchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

// v2TestEntry is an entry of GET /sn/ledger/account as the server pins it.
type v2TestEntry struct {
	Vault             string              `json:"vault"`
	NoID              uint64              `json:"no_id"`
	Index             uint64              `json:"index"`
	RootIndex         *uint64             `json:"root_index"`
	Cumulative        string              `json:"cumulative"`
	Proof             []map[string]string `json:"proof"`
	Claimed           string              `json:"claimed"`
	PendingCumulative *string             `json:"pending_cumulative"`
}

func v2TestProofJSON(t *testing.T, tree *merkle.LedgerSumTree, index uint64) []map[string]string {
	t.Helper()
	proof, err := tree.Proof(index)
	if err != nil {
		t.Fatal(err)
	}
	out := []map[string]string{}
	for _, node := range proof {
		out = append(out, map[string]string{"hash": fmt.Sprintf("0x%x", node.Hash), "sum": node.Sum.Dec()})
	}
	return out
}

// setAccounts sets the account endpoint's reply for alice.
func (c *v2TestChain) setAccounts(entries ...v2TestEntry) {
	data, err := json.Marshal(map[string]any{"coldkey": "0x" + aliceHex, "accounts": append([]v2TestEntry{}, entries...)})
	if err != nil {
		c.t.Fatal(err)
	}
	c.accounts["0x"+aliceHex] = string(data)
}

func v2Ptr[T any](v T) *T { return &v }

// v2ClaimScenario is pool 3 with two finalized roots. Alice is account 1 of
// the latest, root 1, at 5.000000007 α, of which 1 α is claimed, and 6 α
// pending.
type v2ClaimScenario struct {
	c     *v2TestChain
	alice [32]byte
	tree  *merkle.LedgerSumTree
	entry v2TestEntry
}

func newV2ClaimScenario(t *testing.T) *v2ClaimScenario {
	t.Helper()
	s := &v2ClaimScenario{c: newV2TestChain(t), alice: [32]byte(common.FromHex(aliceHex))}
	older := v2TestTree(t, []merkle.LedgerAccount{{Coldkey: v2TestKey(1), Cumulative: *uint256.NewInt(1_000_000_000)}, {Coldkey: s.alice, Cumulative: *uint256.NewInt(3_000_000_000)}})
	s.tree = v2TestTree(t, []merkle.LedgerAccount{{Coldkey: v2TestKey(1), Cumulative: *uint256.NewInt(4_000_000_000)}, {Coldkey: s.alice, Cumulative: *uint256.NewInt(5_000_000_007)}, {Coldkey: v2TestKey(3), Cumulative: *uint256.NewInt(2_000_000_000)}})
	s.c.roots = []stabi.ISTSettlementVault2Root{v2TestRoot(older, 10, [32]byte{0x01}), v2TestRoot(s.tree, 11, [32]byte{0x02})}
	s.c.claimed[[2]uint64{v2TestPool, 1}] = big.NewInt(1_000_000_000)
	s.entry = v2TestEntry{Vault: strings.ToLower(v2TestVault), NoID: v2TestPool, Index: 1, RootIndex: v2Ptr(uint64(1)), Cumulative: "5000000007", Proof: v2TestProofJSON(t, s.tree, 1), Claimed: "1000000000", PendingCumulative: v2Ptr("6000000000")}
	s.c.setAccounts(s.entry)
	return s
}

// calldata is alice's claim against root 1, packed from vault2's ABI.
func (s *v2ClaimScenario) calldata(t *testing.T) []byte {
	t.Helper()
	proof, _ := s.tree.Proof(1)
	nodes := make([]stabi.LedgerTreeProofNode, len(proof))
	for k := range proof {
		nodes[k] = stabi.LedgerTreeProofNode{Hash: proof[k].Hash, Sum: proof[k].Sum.ToBig()}
	}
	data, err := v2TestABI.Pack("claim", big.NewInt(v2TestPool), big.NewInt(1), stabi.LedgerTreeAccount{Index: big.NewInt(1), Coldkey: s.alice, Cumulative: big.NewInt(5_000_000_007)}, nodes)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

const (
	// The calls of a claim refused after its reads, one refused by its
	// preflight, and one sent and finalized.
	v2ClaimRead      = "eth_call:claimed=1 eth_call:roots=1 eth_chainId=1"
	v2ClaimPreflight = "eth_call:claim=1 eth_call:claimed=1 eth_call:roots=1 eth_chainId=1"
	v2ClaimSent      = "eth_call:claim=1 eth_call:claimed=1 eth_call:roots=1 eth_chainId=1 eth_estimateGas:claim=1 eth_gasPrice=1 eth_getBlockByNumber:0x30d40=3 eth_getBlockByNumber:finalized=2 eth_getTransactionCount=1 eth_getTransactionReceipt=1 send:claim=1"
)

// CL-1: the v2 commands parse in both option forms with a repeatable
// --rpc, and each refuses a missing required flag; `v2 status` also sets
// "status", which Run dispatches after v2.
func TestV2Usage(t *testing.T) {
	for _, argv := range [][]string{
		{"v2", "status", "--coldkey", aliceSS58, "--vault", v2TestVault, "--rpc", "https://a.example", "--rpc", "https://b.example"},
		{"v2", "status", "--coldkey=0x" + aliceHex, "--vault=" + v2TestVault, "--rpc=https://a.example", "--api_url=https://api.example", "--chain_id=945"},
	} {
		opts, err := parseArgv(t, argv)
		if err != nil || !boolOpt(opts, "v2") || !boolOpt(opts, "status") || boolOpt(opts, "claim") || boolOpt(opts, "check") || strOpt(opts, "--vault") != v2TestVault || len(strsOpt(opts, "--rpc")) == 0 {
			t.Fatalf("v2 status usage %q = %v, %v", argv, opts, err)
		}
	}
	opts, err := parseArgv(t, []string{"v2", "status", "--coldkey=" + aliceSS58, "--vault=" + v2TestVault, "--rpc=u"})
	if err != nil || strOpt(opts, "--api_url") != "https://api.bringyour.com" {
		t.Fatalf("--api_url default = %q, %v", strOpt(opts, "--api_url"), err)
	}
	claim := []string{"v2", "claim", "--coldkey=" + aliceSS58, "--no_id=3", "--index=1", "--vault=" + v2TestVault, "--rpc=u", "--rpc=w", "--key_file=/tmp/k", "--api_url=https://api.example", "--chain_id=945", "--gas_limit=90000", "--dry-run"}
	if opts, err := parseArgv(t, claim); err != nil || !boolOpt(opts, "v2") || !boolOpt(opts, "claim") || boolOpt(opts, "status") || strOpt(opts, "--no_id") != "3" || strOpt(opts, "--index") != "1" || !boolOpt(opts, "--dry-run") {
		t.Fatalf("v2 claim usage = %v, %v", opts, err)
	}
	check := []string{"v2", "check", "--all", "--vault=" + v2TestVault, "--no_id=3", "--rpc=u", "--challenge", "--demand", "--key_file=/tmp/k", "--api_url=https://api.example", "--chain_id=945", "--gas_limit=90000", "--dry-run"}
	if opts, err := parseArgv(t, check); err != nil || !boolOpt(opts, "v2") || !boolOpt(opts, "check") || !boolOpt(opts, "--all") || !boolOpt(opts, "--challenge") || !boolOpt(opts, "--demand") {
		t.Fatalf("v2 check usage = %v, %v", opts, err)
	}
	for _, c := range []struct {
		argv     []string
		required []string
	}{
		{[]string{"v2", "status", "--coldkey=" + aliceSS58, "--vault=" + v2TestVault, "--rpc=u"}, []string{"--coldkey", "--vault", "--rpc"}},
		{claim, []string{"--coldkey", "--vault", "--rpc", "--key_file"}},
		{check, []string{"--all", "--vault", "--no_id", "--rpc"}},
	} {
		for _, required := range c.required {
			argv := slices.DeleteFunc(slices.Clone(c.argv), func(arg string) bool { return arg == required || strings.HasPrefix(arg, required+"=") })
			if _, err := parseArgv(t, argv); err == nil {
				t.Errorf("%s without %s parsed", c.argv[1], required)
			}
		}
	}
	if _, err := parseArgv(t, []string{"v2", "status", "--epoch=7", "--coldkey=" + aliceSS58, "--vault=" + v2TestVault, "--rpc=u"}); err == nil {
		t.Error("v2 status took v1's --epoch")
	}
}

// CL-1: status shows, per pool, the account index, the finalized
// cumulative and its root, claimed read from vault2 (not the endpoint's),
// claimable and the pending cumulative; an account only in the pending
// root has no finalized cumulative and nothing claimable.
func TestV2StatusReportsAccounts(t *testing.T) {
	s := newV2ClaimScenario(t)
	pendingOnly := v2TestEntry{Vault: strings.ToLower(v2TestVault), NoID: 4, Index: 0, Cumulative: "0", Claimed: "0", PendingCumulative: v2Ptr("7")}
	overclaimed := s.entry
	overclaimed.NoID, overclaimed.Claimed, overclaimed.PendingCumulative = 5, "999", nil
	s.c.claimed[[2]uint64{5, 1}] = big.NewInt(6_000_000_000)
	s.c.setAccounts(s.entry, pendingOnly, overclaimed)
	output, err := s.c.run(s.c.statusArgv("--chain_id=945"))
	want := "settlement v2 account status — vault " + common.HexToAddress(v2TestVault).Hex() + " (chain id 945, rpc " + s.c.rpc.URL + ")\n" +
		"  coldkey:      " + aliceSS58 + " (0x" + aliceHex + ")\n" +
		"  pool 3, account 1:\n" +
		"    finalized:  5.000000007 α (5000000007 rao) at root 1\n" +
		"    claimed:    1.000000000 α (1000000000 rao)\n" +
		"    claimable:  4.000000007 α (4000000007 rao)\n" +
		"    pending:    6.000000000 α (6000000000 rao)\n" +
		"  pool 4, account 0:\n" +
		"    finalized:  none (the account is only in the pending root)\n" +
		"    claimed:    0.000000000 α (0 rao)\n" +
		"    claimable:  0.000000000 α (0 rao)\n" +
		"    pending:    0.000000007 α (7 rao)\n" +
		"  pool 5, account 1:\n" +
		"    finalized:  5.000000007 α (5000000007 rao) at root 1\n" +
		"    claimed:    6.000000000 α (6000000000 rao)\n" +
		"    claimable:  0.000000000 α (0 rao)\n" +
		"    pending:    none\n"
	if err != nil || output != want || s.c.observed() != "eth_call:claimed=3 eth_chainId=1" || !slices.Equal(s.c.blocks, []string{"claimed@latest", "claimed@latest", "claimed@latest"}) {
		t.Fatalf("status = %v:\n%s\nwant:\n%s\nobserved %s %v", err, output, want, s.c.observed(), s.c.blocks)
	}
}

// CL-1, CL-2: an endpoint that cannot answer shows "unavailable", never
// zero, and the command succeeds; a coldkey without accounts shows none.
func TestV2StatusUnavailable(t *testing.T) {
	s := newV2ClaimScenario(t)
	s.c.accountStatus = http.StatusServiceUnavailable
	output, err := s.c.run(s.c.statusArgv())
	if err != nil || !strings.HasSuffix(output, "  accounts:     unavailable (GET "+s.c.api.URL+"/sn/ledger/account?coldkey=0x"+aliceHex+": 503 Service Unavailable)\n") || strings.Contains(output, "0.000000000") {
		t.Fatalf("refusing endpoint: %v\n%s", err, output)
	}
	closed := newV2ClaimScenario(t)
	closed.c.api.Close()
	if output, err := closed.c.run(closed.c.statusArgv()); err != nil || !strings.Contains(output, "  accounts:     unavailable (Get \""+closed.c.api.URL+"/sn/ledger/account?coldkey=0x"+aliceHex+"\": dial tcp ") {
		t.Fatalf("unreachable endpoint: %v\n%s", err, output)
	}
	s = newV2ClaimScenario(t)
	delete(s.c.accounts, "0x"+aliceHex)
	if output, err := s.c.run(s.c.statusArgv()); err != nil || !strings.HasSuffix(output, "  accounts:     none\n") || s.c.observed() != "eth_chainId=1" {
		t.Fatalf("unknown coldkey: %v\n%s", err, output)
	}
}

// CL-1: status refuses an entry of another vault and malformed amounts,
// and fails on a failed claimed read.
func TestV2StatusRefusals(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*v2ClaimScenario)
		want   string
	}{
		{"another vault", func(s *v2ClaimScenario) { s.entry.Vault = "0x3333333333333333333333333333333333333333" }, `the ledger endpoint lists pool 3 in vault "0x3333333333333333333333333333333333333333", not --vault ` + common.HexToAddress(v2TestVault).Hex()},
		{"no vault", func(s *v2ClaimScenario) { s.entry.Vault = "vault2" }, `the ledger endpoint lists pool 3 in vault "vault2"`},
		{"cumulative", func(s *v2ClaimScenario) { s.entry.Cumulative = "5e9" }, `ledger account cumulative "5e9": `},
		{"pending", func(s *v2ClaimScenario) { s.entry.PendingCumulative = v2Ptr("-1") }, `ledger account pending_cumulative "-1": `},
		{"claimed read", func(s *v2ClaimScenario) { s.c.failAt["eth_call:claimed"] = 1 }, "claimed(3,1): synthetic refusal of eth_call:claimed"},
		{"claimed answer", func(s *v2ClaimScenario) { s.c.garbage["claimed"] = true }, "abi: attempting to unmarshal an empty string"},
	} {
		s := newV2ClaimScenario(t)
		c.mutate(s)
		s.c.setAccounts(s.entry)
		if _, err := s.c.run(s.c.statusArgv()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// CL-1: malformed flags are refused before any RPC, an endpoint on another
// chain than --chain_id before any read, and an unreachable RPC.
func TestV2StatusAndClaimRefuseFlags(t *testing.T) {
	refusing := newV2TestChain(t)
	refusing.rpc.Close()
	for _, c := range []struct {
		flag    string
		refusal string
	}{
		{"--coldkey=0x1234", "coldkey: "},
		{"--vault=v2", `--vault: "v2" is not a 20-byte hex address`},
		{"--api_url=ftp://api.example", `--api_url: "ftp://api.example" is not an http(s) URL without a query`},
		{"--api_url=https://api.example/?x=1", `--api_url: "https://api.example/?x=1" is not an http(s) URL`},
		{"--api_url=https://[::1", `--api_url: "https://[::1" is not`},
		{"--chain_id=forty", `--chain_id: "forty" is not a non-negative integer`},
		{"--rpc=" + refusing.rpc.URL, "no rpc endpoint reachable: "},
	} {
		s := newV2ClaimScenario(t)
		if _, err := s.c.run(s.c.statusArgv(c.flag)); err == nil || !strings.HasPrefix(err.Error(), c.refusal) || s.c.observed() != "" {
			t.Fatalf("status %s: %v; observed %s", c.flag, err, s.c.observed())
		}
		if _, err := s.c.run(s.c.claimArgv(c.flag)); err == nil || !strings.HasPrefix(err.Error(), c.refusal) || s.c.observed() != "" {
			t.Fatalf("claim %s: %v; observed %s", c.flag, err, s.c.observed())
		}
	}
	for _, c := range []struct {
		flag    string
		refusal string
	}{
		{"--no_id=three", `--no_id: "three" is not an unsigned 64-bit integer`},
		{"--index=-1", `--index: "-1" is not an unsigned 64-bit integer`},
		{"--gas_limit=lots", "--gas_limit: "},
		{"--key_file=" + filepath.Join(t.TempDir(), "missing.key"), "key_file: "},
	} {
		s := newV2ClaimScenario(t)
		if _, err := s.c.run(s.c.claimArgv(c.flag)); err == nil || !strings.HasPrefix(err.Error(), c.refusal) || s.c.observed() != "" {
			t.Fatalf("claim %s: %v; observed %s", c.flag, err, s.c.observed())
		}
	}
	s := newV2ClaimScenario(t)
	for _, argv := range [][]string{s.c.statusArgv("--chain_id=964"), s.c.claimArgv("--chain_id=964")} {
		if _, err := s.c.run(argv); err == nil || err.Error() != "chain id mismatch: --chain_id=964 but "+s.c.rpc.URL+" reports 945" {
			t.Fatalf("chain mismatch: %v", err)
		}
	}
	if s.c.observed() != "eth_chainId=2" || s.c.apiCalls["/sn/ledger/account"] != 0 {
		t.Fatalf("chain mismatch read on: %s, %v", s.c.observed(), s.c.apiCalls)
	}
}

// CL-1: a claim verifies the endpoint's proof against vault2's root, then
// sends one claim(noId, rootIndex, account, proof) to vault2 at the
// relayer's pending nonce and the suggested fee, and reports the Claimed
// event once final. Its gas limit is the estimate with 20% headroom and never
// below ClaimBatchGasPerClaim: runtime 478's estimate follows vault2's own
// staking-hotkey list, which third parties can lengthen before the claim is
// included, while an estimate of runtime 475's size keeps its headroom. An
// explicit gas limit replaces both, and an unavailable estimate.
func TestV2ClaimSendsOneFinalizedTransaction(t *testing.T) {
	for _, c := range []struct {
		flags    []string
		estimate uint64 // eth_estimateGas's answer; 0 refuses it
		gas      uint64
	}{
		{nil, 50_000, ClaimBatchGasPerClaim},
		{nil, 206_967, ClaimBatchGasPerClaim},
		{nil, 2_572_776, 2_572_776 + 2_572_776/5},
		{[]string{"--coldkey=0x" + aliceHex, "--no_id=3", "--index=1", "--chain_id=945", "--gas_limit=90000"}, 50_000, 90_000},
		{[]string{"--gas_limit=90000"}, 0, 90_000},
	} {
		s := newV2ClaimScenario(t)
		s.c.claimEstimate = c.estimate
		if c.estimate == 0 {
			s.c.failAt["eth_estimateGas:claim"] = 1
		}
		output, err := s.c.run(s.c.claimArgv(c.flags...))
		if err != nil || s.c.observed() != v2ClaimSent {
			t.Fatalf("claim %q: %v; observed %s\n%s", c.flags, err, s.c.observed(), output)
		}
		sent := s.c.transactions()
		if len(sent) != 1 {
			t.Fatalf("claim sent %d transactions", len(sent))
		}
		requireV2Tx(t, sent[0], 7, c.gas, 0, s.calldata(t))
		// The model paid alice 4.000000007 α from her account's claimed.
		if got := s.c.claimed[[2]uint64{v2TestPool, 1}]; got.Cmp(big.NewInt(5_000_000_007)) != 0 || len(s.c.emitted) != 1 {
			t.Fatalf("claimed %s with %d events", got, len(s.c.emitted))
		}
		for _, want := range []string{
			"settlement v2 claim intent\n  vault:       " + common.HexToAddress(v2TestVault).Hex() + " (chain id 945, rpc " + s.c.rpc.URL + ")\n  from:        " + testKeyAddr + "\n" +
				"  account:     pool 3, account 1, root 1\n  coldkey:     " + aliceSS58 + " (0x" + aliceHex + ")\n" +
				"  cumulative:  5.000000007 α (5000000007 rao)\n  claimed:     1.000000000 α (1000000000 rao)\n  claimable:   4.000000007 α (4000000007 rao)\n" +
				"  proof:       2 siblings, verified against root ",
			map[bool]string{true: fmt.Sprintf("\n  gas (est):   %d\n", c.estimate), false: "\n  gas (est):   unavailable (synthetic refusal of eth_estimateGas:claim); using --gas_limit=90000\n"}[c.estimate != 0],
			"\nClaimed: pool 3, account 1, root 1\n  coldkey:     " + aliceSS58 + " (0x" + aliceHex + ")\n  cumulative:  5.000000007 α (5000000007 rao)\n  paid:        4.000000007 α (4000000007 rao)\n  caller:      " + testKeyAddr + "\n",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("output lacks %q:\n%s", want, output)
			}
		}
	}
}

// CL-1: --dry-run verifies, reads, preflights and estimates, then stops
// before the nonce, fee, signature and send.
func TestV2ClaimDryRun(t *testing.T) {
	s := newV2ClaimScenario(t)
	output, err := s.c.run(s.c.claimArgv("--dry-run"))
	if err != nil || s.c.observed() != "eth_call:claim=1 eth_call:claimed=1 eth_call:roots=1 eth_chainId=1 eth_estimateGas:claim=1" || len(s.c.transactions()) != 0 || !strings.HasSuffix(output, "dry run: preflight ok, nothing sent\n") {
		t.Fatalf("dry run: %v; observed %s\n%s", err, s.c.observed(), output)
	}
}

// CL-1: one account per call. Several finalized accounts are refused with
// the flags that pick each; --no_id and --index pick; an account only in
// the pending root, or none, has nothing to claim.
func TestV2ClaimPicksOneAccount(t *testing.T) {
	s := newV2ClaimScenario(t)
	other := s.entry
	other.NoID, other.Index = 4, 0
	pendingOnly := v2TestEntry{Vault: strings.ToLower(v2TestVault), NoID: 5, Index: 2, Cumulative: "0", Claimed: "0", PendingCumulative: v2Ptr("7")}
	s.c.setAccounts(s.entry, other, pendingOnly)
	_, err := s.c.run(s.c.claimArgv())
	if err == nil || err.Error() != "coldkey "+aliceSS58+" (0x"+aliceHex+") has 2 finalized accounts matching the selection [--no_id=3 --index=1, --no_id=4 --index=0]; claim exactly one" || s.c.observed() != "eth_chainId=1" {
		t.Fatalf("two accounts: %v; observed %s", err, s.c.observed())
	}
	for _, flags := range [][]string{{"--no_id=3"}, {"--index=1"}, {"--no_id=0x3", "--index=1"}} {
		s := newV2ClaimScenario(t)
		s.c.setAccounts(s.entry, other, pendingOnly)
		if _, err := s.c.run(s.c.claimArgv(flags...)); err != nil || len(s.c.transactions()) != 1 {
			t.Fatalf("%q did not pick pool 3: %v", flags, err)
		}
		requireV2Tx(t, s.c.transactions()[0], 7, ClaimBatchGasPerClaim, 0, s.calldata(t))
	}
	for _, flags := range [][]string{{"--no_id=5"}, {"--no_id=3", "--index=0"}, {"--no_id=6"}} {
		s := newV2ClaimScenario(t)
		s.c.setAccounts(s.entry, other, pendingOnly)
		if _, err := s.c.run(s.c.claimArgv(flags...)); err == nil || !strings.HasSuffix(err.Error(), " has 0 finalized accounts matching the selection []; claim exactly one") {
			t.Fatalf("%q: %v", flags, err)
		}
	}
	s = newV2ClaimScenario(t)
	s.c.setAccounts()
	if _, err := s.c.run(s.c.claimArgv()); err == nil || !strings.Contains(err.Error(), " has 0 finalized accounts") || s.c.observed() != "eth_chainId=1" {
		t.Fatalf("no accounts: %v", err)
	}
}

// CL-1: a proof that does not verify against vault2's root is refused
// before anything is sent, whichever part the endpoint changed.
func TestV2ClaimRefusesUnverifiedProof(t *testing.T) {
	for _, mutate := range []func(*v2TestEntry){
		func(e *v2TestEntry) { e.Cumulative = "5000000008" },
		func(e *v2TestEntry) { e.Index = 0 },
		func(e *v2TestEntry) { e.RootIndex = v2Ptr(uint64(0)) },
		func(e *v2TestEntry) { e.Proof[0]["sum"] = "4000000001" },
		func(e *v2TestEntry) { e.Proof = e.Proof[:1] },
	} {
		s := newV2ClaimScenario(t)
		mutate(&s.entry)
		s.c.setAccounts(s.entry)
		_, err := s.c.run(s.c.claimArgv())
		if err == nil || !strings.HasPrefix(err.Error(), "the ledger endpoint's proof for pool 3 account ") || !strings.HasSuffix(err.Error(), " in vault2; nothing sent") || s.c.observed() != "eth_call:roots=1 eth_chainId=1" {
			t.Fatalf("entry %+v: %v; observed %s", s.entry, err, s.c.observed())
		}
	}
}

// CL-1: the endpoint's answer is refused when it is unreachable, refused,
// malformed or for another coldkey, and an entry of another vault or with a
// malformed amount or proof, before any chain read.
func TestV2ClaimRefusesEndpoint(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*v2ClaimScenario)
		want   string
	}{
		{"refused", func(s *v2ClaimScenario) { s.c.accountStatus = http.StatusInternalServerError }, "ledger endpoint: GET " + "%s/sn/ledger/account?coldkey=0x" + aliceHex + ": 500 Internal Server Error"},
		{"unreachable", func(s *v2ClaimScenario) { s.c.api.Close() }, "ledger endpoint: Get \"%s/sn/ledger/account?coldkey=0x" + aliceHex + "\": dial tcp "},
		{"malformed", func(s *v2ClaimScenario) { s.c.accounts["0x"+aliceHex] = `{"coldkey":` }, "ledger endpoint: GET %s/sn/ledger/account?coldkey=0x" + aliceHex + ": unexpected EOF"},
		{"another coldkey", func(s *v2ClaimScenario) { s.c.accounts["0x"+aliceHex] = `{"coldkey":"0x00","accounts":[]}` }, `ledger endpoint: GET %s/sn/ledger/account?coldkey=0x` + aliceHex + ` answered for coldkey "0x00" with accounts []`},
		{"null accounts", func(s *v2ClaimScenario) {
			s.c.accounts["0x"+aliceHex] = `{"coldkey":"0x` + aliceHex + `","accounts":null}`
		}, `ledger endpoint: GET %s/sn/ledger/account?coldkey=0x` + aliceHex + ` answered for coldkey "0x` + aliceHex + `" with accounts []`},
		{"another vault", func(s *v2ClaimScenario) { s.entry.Vault = strings.ToLower(withdrawTestVault); s.c.setAccounts(s.entry) }, `the ledger endpoint lists pool 3 in vault "` + strings.ToLower(withdrawTestVault) + `"`},
		{"cumulative", func(s *v2ClaimScenario) { s.entry.Cumulative = "0x12"; s.c.setAccounts(s.entry) }, `ledger account cumulative "0x12": `},
		{"proof hash", func(s *v2ClaimScenario) { s.entry.Proof[1]["hash"] = "0x1234"; s.c.setAccounts(s.entry) }, `ledger account proof node 1 hash "0x1234" is not 32 bytes of 0x hex`},
		{"proof sum", func(s *v2ClaimScenario) { s.entry.Proof[0]["sum"] = ""; s.c.setAccounts(s.entry) }, `ledger account proof node 0 sum "": `},
	} {
		s := newV2ClaimScenario(t)
		c.mutate(s)
		want := c.want
		if strings.Contains(want, "%s") {
			want = fmt.Sprintf(want, s.c.api.URL)
		}
		if _, err := s.c.run(s.c.claimArgv()); err == nil || !strings.HasPrefix(err.Error(), want) || s.c.observed() != "eth_chainId=1" {
			t.Fatalf("%s: %v, want %q; observed %s", c.name, err, want, s.c.observed())
		}
	}
}

// CL-1: failed reads of the root and of claimed are refused before
// anything is signed.
func TestV2ClaimRefusesFailedReads(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*v2TestChain)
		want   string
	}{
		{"roots refused", func(c *v2TestChain) { c.failAt["eth_call:roots"] = 1 }, "roots(3,1): synthetic refusal of eth_call:roots"},
		{"unknown root", func(c *v2TestChain) { c.roots = c.roots[:1] }, "roots(3,1): execution reverted: revert UnknownRoot[]"},
		{"roots answer", func(c *v2TestChain) { c.garbage["roots"] = true }, "abi: attempting to unmarshal an empty string"},
		{"claimed refused", func(c *v2TestChain) { c.failAt["eth_call:claimed"] = 1 }, "claimed(3,1): synthetic refusal of eth_call:claimed"},
	} {
		s := newV2ClaimScenario(t)
		c.mutate(s.c)
		if _, err := s.c.run(s.c.claimArgv()); err == nil || !strings.Contains(err.Error(), c.want) || len(s.c.transactions()) != 0 || s.c.calls["eth_call:claim"] != 0 {
			t.Fatalf("%s: %v; observed %s", c.name, err, s.c.observed())
		}
	}
}

// CL-1, SPEC A6: an account already claimed up to its cumulative, by anyone,
// is done before any preflight.
func TestV2ClaimAlreadyClaimed(t *testing.T) {
	s := newV2ClaimScenario(t)
	s.c.claimed[[2]uint64{v2TestPool, 1}] = big.NewInt(5_000_000_007)
	output, err := s.c.run(s.c.claimArgv())
	if err != nil || output != "pool 3 account 1: claimed 5.000000007 α (5000000007 rao) already reaches the cumulative 5.000000007 α (5000000007 rao); nothing to claim\n" || s.c.observed() != v2ClaimRead {
		t.Fatalf("already claimed: %v; observed %s\n%s", err, s.c.observed(), output)
	}
}

// CL-1, SPEC A6: a third party claiming first is done: after the claimed
// read, so that the preflight reverts NothingToClaim, and after the
// preflight, so that the mined claim reverts. A mined revert that leaves
// the account unclaimed is a failure.
func TestV2ClaimFrontRun(t *testing.T) {
	thirdParty := func(c *v2TestChain) { c.claimed[[2]uint64{v2TestPool, 1}] = big.NewInt(5_000_000_007) }
	for _, c := range []struct {
		name     string
		at       string
		act      func(*v2TestChain)
		observed string
		done     bool
	}{
		{"before the preflight", "eth_call:claim", thirdParty, "eth_call:claim=1 eth_call:claimed=2 eth_call:roots=1 eth_chainId=1", true},
		{"before the inclusion", "send:claim", thirdParty, "eth_call:claim=1 eth_call:claimed=2 eth_call:roots=1 eth_chainId=1 eth_estimateGas:claim=1 eth_gasPrice=1 eth_getTransactionCount=1 eth_getTransactionReceipt=1 send:claim=1", true},
		{"a root replaced", "send:claim", func(c *v2TestChain) { c.roots[1].Root[0] ^= 1 }, "eth_call:claim=1 eth_call:claimed=2 eth_call:roots=1 eth_chainId=1 eth_estimateGas:claim=1 eth_gasPrice=1 eth_getTransactionCount=1 eth_getTransactionReceipt=1 send:claim=1", false},
	} {
		s := newV2ClaimScenario(t)
		s.c.before = func(key string) {
			if key == c.at {
				c.act(s.c)
			}
		}
		output, err := s.c.run(s.c.claimArgv())
		if c.done && (err != nil || !strings.HasSuffix(output, "pool 3 account 1: another sender claimed it first (claimed 5.000000007 α (5000000007 rao)); done\n")) {
			t.Fatalf("%s: %v\n%s", c.name, err, output)
		}
		if !c.done && (err == nil || !strings.HasSuffix(err.Error(), " reverted on-chain (status 0, block 200000, gas used 50000)")) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if s.c.observed() != c.observed {
			t.Fatalf("%s: observed %s", c.name, s.c.observed())
		}
	}
	// A failed re-read joins the refusal.
	s := newV2ClaimScenario(t)
	s.c.before = func(key string) {
		if key == "send:claim" {
			thirdParty(s.c)
		}
	}
	s.c.failAt["eth_call:claimed"] = 2
	if _, err := s.c.run(s.c.claimArgv()); err == nil || !strings.Contains(err.Error(), " reverted on-chain (status 0") || !strings.Contains(err.Error(), "\nclaimed(3,1): synthetic refusal of eth_call:claimed") {
		t.Fatalf("failed re-read: %v", err)
	}
}

// CL-1: each reason vault2 refuses a claim is decoded by name from the
// preflight, nothing is estimated, signed or sent, and the claim fails
// unless the account's claimed reaches its cumulative.
func TestV2ClaimDecodesRevert(t *testing.T) {
	for _, reason := range []string{"UnknownRoot", "InvalidColdkey", "InvalidProof", "NothingToClaim", "AmountTooLarge", "PoolCapExceeded", "RuntimeAccountingMismatch", "Underfunded", "Reentrancy"} {
		s := newV2ClaimScenario(t)
		s.c.revert["claim"] = reason
		_, err := s.c.run(s.c.claimArgv())
		if err == nil || !strings.HasPrefix(err.Error(), "preflight eth_call failed: execution reverted: ") || !strings.HasSuffix(err.Error(), "revert "+reason+"[]") || s.c.observed() != "eth_call:claim=1 eth_call:claimed=2 eth_call:roots=1 eth_chainId=1" {
			t.Fatalf("%s: %v; observed %s", reason, err, s.c.observed())
		}
	}
	// A refusal without revert data is not re-read.
	s := newV2ClaimScenario(t)
	s.c.failAt["eth_estimateGas:claim"] = 1
	if _, err := s.c.run(s.c.claimArgv()); err == nil || err.Error() != "estimateGas: synthetic refusal of eth_estimateGas:claim" || s.c.observed() != "eth_call:claim=1 eth_call:claimed=1 eth_call:roots=1 eth_chainId=1 eth_estimateGas:claim=1" {
		t.Fatalf("estimate refusal: %v; observed %s", err, s.c.observed())
	}
}

// CL-1: a finalized claim whose receipt has no Claimed of this account from
// vault2 fails after its one send.
func TestV2ClaimRequiresClaimed(t *testing.T) {
	s := newV2ClaimScenario(t)
	s.c.mute = true
	if _, err := s.c.run(s.c.claimArgv()); err == nil || !strings.HasSuffix(err.Error(), " succeeded but "+common.HexToAddress(v2TestVault).Hex()+" emitted no Claimed for pool 3 account 1") || s.c.observed() != v2ClaimSent {
		t.Fatalf("muted claim: %v; observed %s", err, s.c.observed())
	}
	// Claimed of another account, and logs of another address, are not it.
	s = newV2ClaimScenario(t)
	s.c.before = func(key string) {
		if key == "eth_getTransactionReceipt" {
			for _, receipt := range s.c.receipts {
				other := *receipt.Logs[0]
				other.Address = common.Address{0x99}
				foreign := s.c.event("Claimed", []common.Hash{common.BigToHash(big.NewInt(v2TestPool)), common.BigToHash(big.NewInt(2)), s.alice}, big.NewInt(1), big.NewInt(1), big.NewInt(1), common.Address{})
				receipt.Logs = []*types.Log{&other, foreign}
			}
		}
	}
	if _, err := s.c.run(s.c.claimArgv()); err == nil || !strings.HasSuffix(err.Error(), " emitted no Claimed for pool 3 account 1") {
		t.Fatalf("foreign Claimed: %v", err)
	}
}

// The child half of TestV2RunReports: the snclaim entry point itself.
func TestV2RunChild(t *testing.T) {
	if argv := os.Getenv("SNCLAIM_V2_TEST_ARGV"); argv != "" {
		Run(strings.Split(argv, "\n"))
	}
}

// CL-1: the snclaim entry point dispatches v2 before v1's status: v2 status
// prints the accounts and exits 0; a refused claim exits 1.
func TestV2RunReports(t *testing.T) {
	s := newV2ClaimScenario(t)
	s.c.do(func() {}) // orders the setup before the handlers
	for _, c := range []struct {
		argv []string
		exit int
		want string
	}{
		{s.c.statusArgv(), 0, "    claimable:  4.000000007 α (4000000007 rao)\n"},
		{s.c.claimArgv("--no_id=9"), 1, "snclaim: coldkey " + aliceSS58 + " (0x" + aliceHex + ") has 0 finalized accounts matching the selection []; claim exactly one\n"},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestV2RunChild$", "-test.timeout=50s")
		cmd.Env = append(os.Environ(), "SNCLAIM_V2_TEST_ARGV="+strings.Join(c.argv, "\n"))
		output, err := cmd.CombinedOutput()
		cancel()
		exit := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if exit != c.exit || !strings.Contains(string(output), c.want) {
			t.Fatalf("%q: exit %d, want %d with %q:\n%s", c.argv, exit, c.exit, c.want, output)
		}
	}
}
