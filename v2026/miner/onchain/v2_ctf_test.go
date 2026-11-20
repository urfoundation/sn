// CAPTURETHEFLAG.md §8, §10: `snclaim v2 check --all` of a capture the flag
// pool, vault2 pool 2^32 + n, against v2TestChain with the capture the flag
// contracts, a drand relay and the signed amendments (v2_ctf.go); and
// `snclaim v2 status` and `v2 claim` of a coldkey's capture the flag account
// beside its pool account, each its own (vault, noId, index).
package onchain

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/amendment"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ctf"
	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

const (
	// v2CtfTestNoID is the operator, and v2CtfTestPool its capture the flag
	// pool 2^32 + 1.
	v2CtfTestNoID   = 1
	v2CtfTestPool   = 1<<32 + v2CtfTestNoID
	v2CtfTestNetuid = 25
	// v2CtfTestCoordinator is STSettlement2, and v2CtfTestCarry the carry
	// contract.
	v2CtfTestCoordinator = "0x5e77000000000000000000000000000000000002"
	v2CtfTestCarry       = "0xca77000000000000000000000000000000000001"
	// v2CtfTestCensusBlock is block B_9, which committed epoch 9's census.
	v2CtfTestCensusBlock = 150_000
	// v2CtfTestV1Coordinator is the v1 coordinator, STSettlement2's
	// v1Coordinator(). Its epoch k spans blocks [v2CtfTestGenesis + 5,000k,
	// v2CtfTestGenesis + 5,000(k + 1)), so epoch 9 ends at B_9.
	v2CtfTestV1Coordinator = "0xc0de000000000000000000000000000000000001"
	v2CtfTestGenesis       = 100_000
	v2CtfTestEpochBlocks   = 5_000
	// v2CtfTestRound is drand quicknet's round 1000, and v2CtfTestSignature
	// its real signature (crv4/golden_test.go).
	v2CtfTestRound     = 1000
	v2CtfTestSignature = "b44679b9a59af2ec876b1a6b1ad52ea9b1615fc3982b19576350f93447cb1125e342b73a8dd2bacbe47e4b6b63ed5e39"
)

var (
	// v2CtfTestTime is B_9's EVM timestamp: its census draws with round
	// 1000, the first at least 60 seconds after it.
	v2CtfTestTime = ctf.RoundTime(v2CtfTestRound) - ctf.DrawDelay
	// v2CtfTestCarryColdkey is the carry contract's coldkey.
	v2CtfTestCarryColdkey = [32]byte{0xca, 0x77, 31: 0x01}
)

// v2TestCensus is a census commitment, ctfCensus(n, k).
type v2TestCensus struct {
	hash  [32]byte
	block uint64
}

// v2TestCtfModel is STSettlement2Ctf at coordinator and STCtfCarry at
// carry, whose vault2 answers coordinator() and netuid() from it, with the
// census blocks' timestamps. absent leaves the carry address without code.
// v1 is the v1 coordinator, with its FleetBound events and its archive
// bindingAt answers. pruned refuses its code at a past block, and codeless
// answers none, as Frontier's eth_getCode does on a node without archive
// state; unclosed puts epochs' ends past the finalized block.
type v2TestCtfModel struct {
	coordinator common.Address
	carry       common.Address
	netuid      uint16
	census      map[uint64]v2TestCensus
	times       map[uint64]uint64
	coldkey     [32]byte
	carryVault  common.Address
	carryPool   uint64
	recycled    map[uint64]*big.Int
	absent      bool
	v1          common.Address
	fleet       []v2TestFleetBound
	active      map[v2TestBindingAt]bool
	pruned      bool
	codeless    bool
	unclosed    bool
}

// v2TestFleetBound is a FleetBound event of the v1 coordinator.
type v2TestFleetBound struct {
	block              uint64
	client             [16]byte
	validFrom, validTo uint64
}

// v2TestBindingAt is a bindingAt(client, epoch) read at a block.
type v2TestBindingAt struct {
	client       [16]byte
	epoch, block uint64
}

func (m *v2TestCtfModel) serves(to common.Address) bool {
	return m != nil && (to == m.coordinator || to == m.carry || to == m.v1)
}

// v2TestV1Settlement and v2TestV1Coordinator decode the reads of the head
// bindings: STSettlement2's v1Coordinator() and the v1 coordinator's views.
var (
	v2TestV1Settlement  = stabi.NewSTSettlement2()
	v2TestV1Coordinator = func() abi.ABI {
		parsed, err := stabi.STCoordinatorMetaData.ParseABI()
		if err != nil {
			panic(err)
		}
		return *parsed
	}()
)

// answerV1 answers an eth_call of STSettlement2's v1Coordinator() or of the
// v1 coordinator's epoch boundaries and bindingAt.
func (c *v2TestChain) answerV1(to common.Address, input []byte, block string) (any, map[string]any) {
	m := c.ctf
	method, err := v2TestV1Coordinator.MethodById(input[:4])
	if to == m.coordinator {
		method, err = &abi.Method{Name: "v1Coordinator"}, nil
	}
	if err != nil {
		c.t.Errorf("call of an unknown v1 coordinator view 0x%x", input)
		return nil, rpcError(3, "execution reverted", nil)
	}
	c.blocks = append(c.blocks, method.Name+"@"+block)
	if c.count("eth_call:" + method.Name) {
		return nil, rpcError(-32000, "synthetic refusal of eth_call:"+method.Name, nil)
	}
	if to == m.coordinator {
		return hexutil.Bytes(common.LeftPadBytes(m.v1.Bytes(), 32)), nil
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		c.t.Errorf("%s arguments do not decode: %v", method.Name, err)
	}
	var out []byte
	switch method.Name {
	case "epochStartBlock":
		out, err = method.Outputs.Pack(new(big.Int).SetUint64(v2CtfTestGenesis + v2CtfTestEpochBlocks*args[0].(*big.Int).Uint64()))
	case "epochEndBlock":
		end := v2CtfTestGenesis + v2CtfTestEpochBlocks*(args[0].(*big.Int).Uint64()+1)
		if m.unclosed {
			end = v2TestHead + 1
		}
		out, err = method.Outputs.Pack(new(big.Int).SetUint64(end))
	case "bindingAt":
		at := v2TestBindingAt{client: args[0].([16]byte), epoch: args[1].(*big.Int).Uint64(), block: hexutil.MustDecodeUint64(block)}
		out, err = method.Outputs.Pack(m.active[at], stabi.STCoordinatorBindingRecord{})
	default:
		c.t.Errorf("unexpected v1 coordinator view %s", method.Name)
		return nil, rpcError(3, "execution reverted", nil)
	}
	if err != nil {
		c.t.Errorf("%s outputs do not pack: %v", method.Name, err)
	}
	return hexutil.Bytes(out), nil
}

// fleetBound is the v1 coordinator's FleetBound log of binding.
func (m *v2TestCtfModel) fleetBound(binding v2TestFleetBound) types.Log {
	event := v2TestV1Coordinator.Events["FleetBound"]
	topics, err := abi.MakeTopics([]any{binding.client}, []any{[32]byte{0xf1}}, []any{[32]byte{0x40}})
	if err != nil {
		panic(err)
	}
	data, err := event.Inputs.NonIndexed().Pack(uint16(7), uint64(1), binding.validFrom, binding.validTo)
	if err != nil {
		panic(err)
	}
	return types.Log{Address: m.v1, Topics: append([]common.Hash{event.ID}, topics[0][0], topics[1][0], topics[2][0]), Data: data, BlockNumber: binding.block}
}

func (m *v2TestCtfModel) blockTime(number uint64) (uint64, bool) {
	if m == nil {
		return 0, false
	}
	time, ok := m.times[number]
	return time, ok
}

// answerCtf answers an eth_call of a capture the flag view.
func (c *v2TestChain) answerCtf(method string, to common.Address, params []json.RawMessage) (any, map[string]any) {
	var message struct {
		Input hexutil.Bytes `json:"input"`
	}
	var block string
	views, _ := v2CtfViews()
	if method != "eth_call" || len(params) != 2 || json.Unmarshal(params[0], &message) != nil || json.Unmarshal(params[1], &block) != nil || len(message.Input) < 4 {
		c.t.Errorf("%s of a capture the flag view: %s", method, params)
		return nil, rpcError(-32602, "malformed call", nil)
	}
	if to == c.ctf.v1 || to == c.ctf.coordinator && [4]byte(message.Input[:4]) == [4]byte(v2TestV1Settlement.PackV1Coordinator()) {
		return c.answerV1(to, message.Input, block)
	}
	view, err := views.MethodById(message.Input[:4])
	if err != nil {
		c.t.Errorf("call of an unknown capture the flag view 0x%x", message.Input)
		return nil, rpcError(3, "execution reverted", nil)
	}
	c.blocks = append(c.blocks, view.Name+"@"+block)
	if c.count(method + ":" + view.Name) {
		return nil, rpcError(-32000, "synthetic refusal of "+method+":"+view.Name, nil)
	}
	args, err := view.Inputs.Unpack(message.Input[4:])
	if err != nil {
		c.t.Errorf("%s arguments do not decode: %v", view.Name, err)
	}
	m := c.ctf
	var out []byte
	switch {
	case to == m.coordinator && view.Name == "ctfCensus":
		var census v2TestCensus
		if args[0].(*big.Int).Cmp(big.NewInt(v2CtfTestNoID)) == 0 {
			census = m.census[args[1].(*big.Int).Uint64()]
		}
		out, err = view.Outputs.Pack(census.hash, census.block)
	case to == m.carry && m.absent:
		return hexutil.Bytes{}, nil
	case to == m.carry && view.Name == "coldkey":
		out, err = view.Outputs.Pack(m.coldkey)
	case to == m.carry && view.Name == "vault":
		out, err = view.Outputs.Pack(m.carryVault)
	case to == m.carry && view.Name == "poolId":
		out, err = view.Outputs.Pack(new(big.Int).SetUint64(m.carryPool))
	case to == m.carry && view.Name == "recycledFor":
		recycled := m.recycled[args[0].(*big.Int).Uint64()]
		if recycled == nil {
			recycled = new(big.Int)
		}
		out, err = view.Outputs.Pack(recycled)
	default:
		c.t.Errorf("unexpected capture the flag view %s of %s", view.Name, to)
		return nil, rpcError(3, "execution reverted", nil)
	}
	if err != nil {
		c.t.Errorf("%s outputs do not pack: %v", view.Name, err)
	}
	return hexutil.Bytes(out), nil
}

// v2TestCtfWeb serves the signed amendments at /amendments.yml, as ur.xyz
// would, and drand quicknet pulses at /<chain hash>/public/<round>, as a
// relay does.
type v2TestCtfWeb struct {
	t      *testing.T
	server *httptest.Server
	lock   sync.Mutex
	// amendments is the file served, nil for none; pulses holds a relay's
	// answer by round, and a round without one is not served.
	amendments []byte
	pulses     map[uint64]string
	calls      map[string]int
}

func newV2TestCtfWeb(t *testing.T) *v2TestCtfWeb {
	t.Helper()
	w := &v2TestCtfWeb{t: t, pulses: map[uint64]string{}, calls: map[string]int{}}
	w.server = httptest.NewServer(w)
	t.Cleanup(w.server.Close)
	return w
}

func (w *v2TestCtfWeb) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.calls[request.URL.Path]++
	if request.URL.Path == "/amendments.yml" {
		if w.amendments == nil {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(w.amendments)
		return
	}
	if round, ok := strings.CutPrefix(request.URL.Path, "/"+crv4.QuicknetChainHash+"/public/"); ok {
		number, err := strconv.ParseUint(round, 10, 64)
		body, served := w.pulses[number]
		if err != nil || !served {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, body)
		return
	}
	w.t.Errorf("unexpected request %s", request.URL)
	http.NotFound(writer, request)
}

func (w *v2TestCtfWeb) do(f func()) {
	w.lock.Lock()
	defer w.lock.Unlock()
	f()
}

// v2TestPulse is a relay's answer for round: its randomness and signature.
func v2TestPulse(round uint64, signature string) string {
	raw, _ := hex.DecodeString(signature)
	return fmt.Sprintf(`{"round":%d,"randomness":"%x","signature":"%s"}`, round, sha256.Sum256(raw), signature)
}

// v2CtfTestAmendments is an amendments file of the entries, each signed by
// key, as cli/amendment attach writes them.
func v2CtfTestAmendments(t *testing.T, key *ecdsa.PrivateKey, entries ...*amendment.Entry) []byte {
	t.Helper()
	var file strings.Builder
	fmt.Fprintf(&file, "sn: %d\n", v2CtfTestNetuid)
	if len(entries) > 0 {
		file.WriteString("signed_amendments:\n")
	}
	for _, entry := range entries {
		if err := entry.Sign(key); err != nil {
			t.Fatal(err)
		}
		line, err := entry.Line()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&file, "  - line: \"%s\"\n    signature: \"0x%s\"\n", line, hex.EncodeToString(entry.Signature))
	}
	return []byte(file.String())
}

// v2CtfTestEntry is the capture the flag entry of operator 1 from epoch
// with carry.
func v2CtfTestEntry(epoch uint64, carry string) *amendment.Entry {
	return amendment.NewCaptureTheFlag(v2TestChainID, v2CtfTestNetuid, v2CtfTestNoID, epoch, common.HexToAddress(carry))
}

// v2CtfTestCandidates is epoch 9's census: six provider units in DE, of
// which five win; in IS two units, one of two clients, which both win; and
// one extender in TM. Client n's unit is n but for client 22, in unit 21.
func v2CtfTestCandidates(t *testing.T) []ctf.Candidate {
	t.Helper()
	country := func(code string) ctf.Country {
		c, ok := ctf.ParseCountry(code)
		if !ok {
			t.Fatalf("%s is not in L", code)
		}
		return c
	}
	candidate := func(role ctf.Role, code string, unit, client int) ctf.Candidate {
		out := ctf.Candidate{Role: role, Country: country(code), Family: ctf.FamilyIPv4}
		out.Unit[0], out.Unit[31] = 0x0b, byte(unit)
		out.ClientID[15] = byte(client)
		return out
	}
	var candidates []ctf.Candidate
	for n := 11; n <= 16; n++ {
		candidates = append(candidates, candidate(ctf.RoleProvider, "de", n, n))
	}
	return append(candidates, candidate(ctf.RoleProvider, "is", 21, 21), candidate(ctf.RoleProvider, "is", 21, 22),
		candidate(ctf.RoleProvider, "is", 23, 23), candidate(ctf.RoleExtender, "tm", 31, 31))
}

// v2CtfTestColdkeys gives each census client n the payout coldkey n.
func v2CtfTestColdkeys(candidates []ctf.Candidate) map[[16]byte][32]byte {
	coldkeys := map[[16]byte][32]byte{}
	for _, candidate := range candidates {
		coldkeys[candidate.ClientID] = v2TestKey(int(candidate.ClientID[15]))
	}
	return coldkeys
}

// v2CtfScenario is operator 1's capture the flag pool 2^32 + 1, first
// epoch 8, in force from epoch 8 by a signed amendment entry. The finalized
// root F (seq 1, root 0) covers epoch 8, which had no census, so its one
// account is the carry's, holding all of the epoch's funding. The pending
// root P (seq 2) covers epoch 9, whose census was committed at block
// 150,000 and draws with quicknet round 1000 after the carry returned F's
// account in full, and epoch 10, which had no census. Both roots are built
// by ctf.Build and published.
type v2CtfScenario struct {
	c      *v2TestChain
	web    *v2TestCtfWeb
	key    *ecdsa.PrivateKey
	funded map[uint64]uint256.Int
	census *ctf.CensusObjects
	f, p   *ctf.Ledger
}

func newV2CtfScenario(t *testing.T) *v2CtfScenario {
	t.Helper()
	ctx := context.Background()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signer := crypto.PubkeyToAddress(key.PublicKey)
	saved := v2AmendmentSigner
	v2AmendmentSigner = func(chainID uint64, netuid uint16) (common.Address, bool) {
		return signer, chainID == v2TestChainID && netuid == v2CtfTestNetuid
	}
	t.Cleanup(func() { v2AmendmentSigner = saved })

	funded := v2Funded(8, 2_490_000_000, 9, 6_225_000_000, 10, 7_225_000_007)
	f, err := ctf.Build(ctx, ctf.BuildInput{NoID: v2CtfTestNoID, ChainID: v2TestChainID, Netuid: v2CtfTestNetuid, Epoch: 8, FirstEpoch: 8,
		CarryColdkey: v2CtfTestCarryColdkey, Epochs: []ctf.EpochInput{{Epoch: 8}}, FundedThrough: funded})
	if err != nil {
		t.Fatal(err)
	}
	candidates := v2CtfTestCandidates(t)
	census, err := ctf.EncodeCensus(&ctf.Census{NoID: v2CtfTestNoID, Epoch: 9, Candidates: candidates,
		Excluded: []ctf.Exclusion{{Country: candidates[6].Country, Role: ctf.RoleProvider, Reason: ctf.ReasonPresence, Count: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := hex.DecodeString(v2CtfTestSignature)
	p, err := ctf.Build(ctx, ctf.BuildInput{NoID: v2CtfTestNoID, ChainID: v2TestChainID, Netuid: v2CtfTestNetuid, Epoch: 10, FirstEpoch: 8,
		CarryColdkey: v2CtfTestCarryColdkey, Previous: &ctf.Previous{Root: f.Commit, Accounts: f.Accounts, State: f.State},
		Epochs: []ctf.EpochInput{
			{Epoch: 9, Census: &ctf.CensusInput{Hash: census.Hash, Block: v2CtfTestCensusBlock, Time: v2CtfTestTime, Objects: census.Objects},
				Pulse: &ctf.Pulse{Round: v2CtfTestRound, Signature: signature}, Recycled: f.Commit.Total, Coldkeys: v2CtfTestColdkeys(candidates)},
			{Epoch: 10},
		}, FundedThrough: funded})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Accounts) != 1 || len(p.Accounts) != 9 || !p.Epochs[0].Drawn || len(p.Epochs[0].Awards) != 8 || !p.Epochs[0].Matured {
		t.Fatalf("scenario has %d and %d accounts, epoch 9 drawn %v with %d awards", len(f.Accounts), len(p.Accounts), p.Epochs[0].Drawn, len(p.Epochs[0].Awards))
	}
	s := &v2CtfScenario{c: newV2TestChain(t), web: newV2TestCtfWeb(t), key: key, funded: funded, census: census, f: f, p: p}
	s.c.poolID, s.c.pool.FirstEpoch = v2CtfTestPool, 8
	s.c.roots = []stabi.ISTSettlementVault2Root{v2TestRoot(f.Tree, 8, f.Commit.LedgerHash)}
	for epoch, value := range funded {
		s.c.funded[epoch] = value.ToBig()
	}
	s.c.objects = maps.Clone(f.Objects)
	maps.Copy(s.c.objects, p.Objects)
	s.c.ctf = &v2TestCtfModel{
		coordinator: common.HexToAddress(v2CtfTestCoordinator), carry: common.HexToAddress(v2CtfTestCarry), netuid: v2CtfTestNetuid,
		census:  map[uint64]v2TestCensus{9: {hash: census.Hash, block: v2CtfTestCensusBlock}},
		times:   map[uint64]uint64{v2CtfTestCensusBlock: v2CtfTestTime},
		coldkey: v2CtfTestCarryColdkey, carryVault: s.c.vault, carryPool: v2CtfTestPool,
		recycled: map[uint64]*big.Int{9: f.Commit.Total.ToBig()},
		v1:       common.HexToAddress(v2CtfTestV1Coordinator), active: map[v2TestBindingAt]bool{},
	}
	s.c.committed(v2TestHead-40_000, 1, f.Commit, 0)
	s.c.finalized(v2TestHead-20_000, 1, 0, f.Commit)
	s.setPending(2, p.Commit)
	s.web.amendments = v2CtfTestAmendments(t, key, v2CtfTestEntry(8, v2CtfTestCarry))
	s.web.pulses[v2CtfTestRound] = v2TestPulse(v2CtfTestRound, v2CtfTestSignature)
	return s
}

// setPending commits root as pending seq at v2TestHead − 1,000.
func (s *v2CtfScenario) setPending(seq uint64, root ledger.Root) {
	s.c.pending = stabi.ISTSettlementVault2Pending{Seq: seq, Root: root.Root, Total: root.Total.ToBig(), LeafCount: root.LeafCount, Epoch: root.Epoch, LedgerHash: root.LedgerHash, CommittedBlock: v2TestHead - 1_000}
	s.c.committed(v2TestHead-1_000, seq, root, 0)
}

// argv is `v2 check --all` of the capture the flag pool with the
// scenario's amendments and relay.
func (s *v2CtfScenario) argv(flags ...string) []string {
	return s.c.checkArgv(append([]string{fmt.Sprintf("--no_id=%d", v2CtfTestPool), "--amendments_url=" + s.web.server.URL + "/amendments.yml", "--drand_url=" + s.web.server.URL}, flags...)...)
}

// epochLines are the check's lines for P's covered epochs as Build reports
// them; epoch 9's census and draw are spelled out.
func (s *v2CtfScenario) epochLines() string {
	nine, ten := s.p.Epochs[0], s.p.Epochs[1]
	paid := func(e *ctf.EpochResult) string { v := e.Paid(); return v.Dec() }
	return fmt.Sprintf("  epoch 9:           census %v at block 150000, 10 candidates; drand round 1000, 8 winners\n", s.census.Hash) +
		"                     funding 3735000000, returned 2490000000 of 2490000000 owed (matured), fresh 1245000000, slot 500000 rao\n" +
		fmt.Sprintf("                     paid %s, pending 0, carried %s rao\n", paid(nine), nine.Carried.Dec()) +
		"  epoch 10:          no census committed: no draw\n" +
		"                     funding 1000000007, returned 0 of 0 owed (matured), fresh 1000000007, slot 401606 rao\n" +
		fmt.Sprintf("                     paid %s, pending 0, carried %s rao\n", paid(ten), ten.Carried.Dec())
}

// header is the check's first lines and the terms of the audit.
func (s *v2CtfScenario) header() string {
	return "settlement v2 ledger check — vault " + common.HexToAddress(v2TestVault).Hex() + ", pool 4294967297 (chain id 945, rpc " + s.c.rpc.URL + ")\n" +
		"  finalized block:   200000 (" + v2TestBlockHash(v2TestHead).Hex() + ")\n" +
		"  pending root:      seq 2, epoch 10, committed at block 199000, 0 open demands\n" +
		fmt.Sprintf("                     root 0x%x, total 7.225000007 α (7225000007 rao), 9 accounts, ledger %v\n", s.p.Commit.Root, ledger.Ref(s.p.Commit.LedgerHash)) +
		fmt.Sprintf("  finalized root:    index 0, epoch 8, root 0x%x, total 2.490000000 α (2490000000 rao), 1 accounts\n", s.f.Commit.Root) +
		"  capture the flag:  operator 1, from epoch 8 by the signed amendment entry, carry contract " + common.HexToAddress(v2CtfTestCarry).Hex() + "\n" +
		fmt.Sprintf("                     carry coldkey 0x%x\n", v2CtfTestCarryColdkey) +
		v2CtfTestHeads(len(s.c.ctf.fleet))
}

// v2CtfTestHeads is the check's line on where the head bindings came from:
// the v1 coordinator's FleetBound events from its first block.
func v2CtfTestHeads(events int) string {
	return fmt.Sprintf("  head bindings:     %d FleetBound events of v1 coordinator %s from block %d\n", events, common.HexToAddress(v2CtfTestV1Coordinator).Hex(), v2CtfTestGenesis)
}

// CAPTURETHEFLAG.md §8: an honest capture the flag root passes the CTF
// audit. The check reads, at the finalized block, vault2's coordinator and
// netuid, the carry contract's pool, coldkey and returns, and epoch 9's
// census commitment and its block's time; it takes the carry contract and
// E_c from the signed amendment entry, and round 1000's pulse from the
// relay, verified against quicknet's key. It recomputes the draw and the
// amounts, approves, and finds no fault in F's accounts. Epoch 10 had no
// census, so its commitment is read but no block time or pulse.
func TestV2CheckCtfVerifies(t *testing.T) {
	s := newV2CtfScenario(t)
	output, err := s.c.run(s.argv("--challenge", "--demand", "--chain_id=945"))
	want := s.header() + s.epochLines() +
		"  audit:             approve\n" +
		"  previous accounts: 1 of root 0, no fault\n"
	if err != nil || output != want || len(s.c.transactions()) != 0 {
		t.Fatalf("honest root: %v\n%s\nwant:\n%s", err, output, want)
	}
	reads := "eth_call:coldkey=1 eth_call:coordinator=1 eth_call:ctfCensus=2 eth_call:epochStartBlock=1 eth_call:fundedThrough=2 eth_call:netuid=1 eth_call:pending=1 eth_call:poolId=1 eth_call:pools=1 eth_call:recycledFor=2 eth_call:rootCount=1 eth_call:roots=1 eth_call:v1Coordinator=1 eth_call:vault=1 eth_chainId=1 eth_getBlockByNumber:0x249f0=1 eth_getBlockByNumber:finalized=1 eth_getCode=1 eth_getLogs:FleetBound=101"
	if s.c.observed() != reads {
		t.Fatalf("observed %s\nwant     %s", s.c.observed(), reads)
	}
	// The head bindings: the v1 coordinator's FleetBound events from its
	// first block through the finalized block, in contiguous spans the public
	// endpoint answers; none covers epoch 9, so no bindingAt is read.
	for i, span := range s.c.fleetSpans {
		if span[0] != v2CtfTestGenesis+uint64(i)*v2LogChunkBlocks || span[1] != min(span[0]+v2LogChunkBlocks-1, v2TestHead) {
			t.Fatalf("FleetBound span %d is %v", i, span)
		}
	}
	for _, block := range s.c.blocks {
		if !strings.HasSuffix(block, "@0x30d40") {
			t.Fatalf("a check read is not at the finalized block: %v", s.c.blocks)
		}
	}
	pulse := "/" + crv4.QuicknetChainHash + "/public/1000"
	if !maps.Equal(s.web.calls, map[string]int{"/amendments.yml": 1, pulse: 1}) {
		t.Fatalf("web reads %v", s.web.calls)
	}
	// The objects came from the operator API's object endpoint, no more
	// reads of it than both roots have objects.
	if s.c.apiCalls["/sn/ledger/object"] == 0 || s.c.apiCalls["/sn/ledger/object"] > len(s.f.Objects)+len(s.p.Objects) {
		t.Fatalf("fetched %d objects", s.c.apiCalls["/sn/ledger/object"])
	}
}

// CAPTURETHEFLAG.md §8: a root the chain's facts do not reproduce is
// refused, with the failing check: a census commitment other than the one
// the root drew, none at all, or a carry that returned nothing, so the pot
// shares the root paid were owed as pending awards. The published accounts
// reproduce the root and fault none of F's, so the watcher has nothing to
// challenge or demand; unlike a provider pool's refused root, the check
// fails, since a wrong draw or wrong amounts never fault an account and the
// failed run is the watcher's alert.
func TestV2CheckCtfRefusesWhatTheChainDoesNotReproduce(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(s *v2CtfScenario)
		want   string
	}{
		{"another census committed", func(s *v2CtfScenario) {
			s.c.ctf.census[9] = v2TestCensus{hash: [32]byte{0xc3}, block: v2CtfTestCensusBlock}
		}, "  audit:             refuse (census check: ctf: header census is not the committed census: epoch 9, committed sha256:c3"},
		{"no census committed", func(s *v2CtfScenario) {
			delete(s.c.ctf.census, 9)
		}, "  audit:             refuse (census check: ctf: header census is not the committed census: epoch 9 lists "},
		{"the carry returned nothing", func(s *v2CtfScenario) {
			delete(s.c.ctf.recycled, 9)
		}, "  audit:             refuse (amounts check: ctf: an award's amounts are not the walk's: epoch 9 row 0 "},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newV2CtfScenario(t)
			c.change(s)
			output, err := s.c.run(s.argv("--challenge", "--demand"))
			refusal := "the capture the flag audit refuses pending root seq 2 (" + strings.TrimPrefix(c.want, "  audit:             refuse (")
			if err == nil || !strings.HasPrefix(err.Error(), refusal) || !strings.Contains(output, c.want) || strings.Contains(output, "approve") ||
				!strings.HasSuffix(output, "  previous accounts: 1 of root 0, no fault\n") || len(s.c.transactions()) != 0 {
				t.Fatalf("%v\n%s\nwant %q", err, output, c.want)
			}
		})
	}
}

// CAPTURETHEFLAG.md §4.6, §8: a header that names a carry coldkey other
// than the carry contract's is refused at the header, before its accounts
// are read, so F's accounts are unverifiable and demanded.
func TestV2CheckCtfDemandsOnAnotherCarryColdkey(t *testing.T) {
	s := newV2CtfScenario(t)
	s.c.ctf.coldkey = [32]byte{0x0c}
	output, err := s.c.run(s.argv())
	if err == nil || err.Error() != "pending root seq 2 leaves the accounts of finalized root 0 unverifiable; --demand sends the demand set" ||
		!strings.Contains(output, "  audit:             demand (header check: ctf: header carry_coldkey is not the carry contract's: ") ||
		!strings.HasSuffix(output, "  previous accounts: 1 of root 0, unverifiable: the published objects are missing or do not reproduce the pending root\n") {
		t.Fatalf("another carry coldkey: %v\n%s", err, output)
	}
}

// CAPTURETHEFLAG.md §8: the audit fails closed. When the amendments in
// force, the carry contract, a census read or the draw's pulse cannot be
// had, or a pulse does not verify against quicknet's key, the check says the
// audit is unavailable, never approves, still finds F's accounts without
// fault from the published objects, and fails.
func TestV2CheckCtfFailsClosed(t *testing.T) {
	other, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	flipped := []byte(v2CtfTestSignature)
	flipped[95] ^= 1 // the last hex digit
	for _, c := range []struct {
		name   string
		change func(s *v2CtfScenario)
		want   string
	}{
		{"amendments not served", func(s *v2CtfScenario) { s.web.amendments = nil },
			"the signed amendments at %s/amendments.yml: amendment: HTTP 404; " + amendment.ErrUnknown.Error()},
		{"amendments signed by another key", func(s *v2CtfScenario) {
			s.web.amendments = v2CtfTestAmendments(t, other, v2CtfTestEntry(8, v2CtfTestCarry))
		}, "the signed amendments at %s/amendments.yml: amendment: "},
		{"no deployment signer", func(s *v2CtfScenario) {
			v2AmendmentSigner = func(uint64, uint16) (common.Address, bool) { return common.Address{}, false }
		}, "no signed amendment puts capture the flag in force for operator 1 at epoch 10, the pending root's, so its carry contract is unknown"},
		{"capture the flag in force only later", func(s *v2CtfScenario) {
			s.web.amendments = v2CtfTestAmendments(t, s.key, v2CtfTestEntry(11, v2CtfTestCarry))
		}, "no signed amendment puts capture the flag in force for operator 1 at epoch 10, the pending root's, so its carry contract is unknown"},
		{"two carry contracts", func(s *v2CtfScenario) {
			s.web.amendments = v2CtfTestAmendments(t, s.key, v2CtfTestEntry(8, "0xca77000000000000000000000000000000000009"), v2CtfTestEntry(10, v2CtfTestCarry))
		}, "the signed capture the flag entries of operator 1 name another carry contract at epoch 9 than at epoch 10: one root cannot be audited against two"},
		{"the carry contract of another pool", func(s *v2CtfScenario) { s.c.ctf.carryPool = v2CtfTestPool + 1 },
			"carry contract " + common.HexToAddress(v2CtfTestCarry).Hex() + " serves vault " + common.HexToAddress(v2TestVault).Hex() + " pool 4294967298, not vault2 " + common.HexToAddress(v2TestVault).Hex() + " pool 4294967297"},
		{"no carry contract", func(s *v2CtfScenario) { s.c.ctf.absent = true },
			"carry contract " + common.HexToAddress(v2CtfTestCarry).Hex() + " vault(): "},
		{"a census read refused", func(s *v2CtfScenario) { s.c.failAt["eth_call:ctfCensus"] = 1 },
			"coordinator " + common.HexToAddress(v2CtfTestCoordinator).Hex() + " ctfCensus(1,9): synthetic refusal of eth_call:ctfCensus"},
		{"the census block refused", func(s *v2CtfScenario) { s.c.failAt["eth_getBlockByNumber:0x249f0"] = 1 },
			"epoch 9 census block 150000: synthetic block refusal"},
		{"no pulse served", func(s *v2CtfScenario) { delete(s.web.pulses, v2CtfTestRound) },
			"epoch 9 draw: GET %s/" + crv4.QuicknetChainHash + "/public/1000: 404 Not Found"},
		{"a pulse that does not verify", func(s *v2CtfScenario) { s.web.pulses[v2CtfTestRound] = v2TestPulse(v2CtfTestRound, string(flipped)) },
			"epoch 9 draw: GET %s/" + crv4.QuicknetChainHash + "/public/1000: ctf: drand pulse does not verify against quicknet's key: round 1000"},
		{"another round's pulse", func(s *v2CtfScenario) {
			s.web.pulses[v2CtfTestRound] = v2TestPulse(v2CtfTestRound+1, v2CtfTestSignature)
		},
			"epoch 9 draw: GET %s/" + crv4.QuicknetChainHash + "/public/1000 answered round 1001 with signature"},
		{"the census committed later", func(s *v2CtfScenario) { s.c.ctf.times[v2CtfTestCensusBlock] += 3 },
			"epoch 9 draw: GET %s/" + crv4.QuicknetChainHash + "/public/1001: 404 Not Found"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newV2CtfScenario(t)
			c.change(s)
			output, err := s.c.run(s.argv("--challenge", "--demand"))
			want := "the capture the flag audit could not run: " + strings.ReplaceAll(c.want, "%s", s.web.server.URL)
			if err == nil || !strings.HasPrefix(err.Error(), want) || !strings.Contains(output, "  audit:             unavailable: "+want) ||
				strings.Contains(output, "approve") || !strings.HasSuffix(output, "  previous accounts: 1 of root 0, no fault\n") || len(s.c.transactions()) != 0 {
				t.Fatalf("%v\n%s\nwant %q", err, output, want)
			}
		})
	}
}

// v2CtfTestBound is the head-bound client of boundScenario: provider client
// 41 in TM, alone in its unit, so its draw changes the award rows.
var v2CtfTestBound = [16]byte{15: 41}

// boundScenario makes epoch 9's census also list v2CtfTestBound, whose
// FleetBound event at block 120,000 covers epochs 8 to 10, and rebuilds P
// leaving it out of the draw, as the operator's builder and the validator
// do. The v1 coordinator answers bindingAt(41, 9) as active at the
// boundaries of epoch 9 listed in active.
func (s *v2CtfScenario) boundScenario(t *testing.T, active ...uint64) {
	t.Helper()
	candidates := append(v2CtfTestCandidates(t), ctf.Candidate{Role: ctf.RoleProvider, Country: [2]byte{'t', 'm'}, Unit: [32]byte{0x0b, 31: 41}, Family: ctf.FamilyIPv4, ClientID: v2CtfTestBound})
	census, err := ctf.EncodeCensus(&ctf.Census{NoID: v2CtfTestNoID, Epoch: 9, Candidates: candidates,
		Excluded: []ctf.Exclusion{{Country: candidates[6].Country, Role: ctf.RoleProvider, Reason: ctf.ReasonPresence, Count: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := hex.DecodeString(v2CtfTestSignature)
	p, err := ctf.Build(context.Background(), ctf.BuildInput{NoID: v2CtfTestNoID, ChainID: v2TestChainID, Netuid: v2CtfTestNetuid, Epoch: 10, FirstEpoch: 8,
		CarryColdkey: v2CtfTestCarryColdkey, Previous: &ctf.Previous{Root: s.f.Commit, Accounts: s.f.Accounts, State: s.f.State},
		Epochs: []ctf.EpochInput{
			{Epoch: 9, Census: &ctf.CensusInput{Hash: census.Hash, Block: v2CtfTestCensusBlock, Time: v2CtfTestTime, Objects: census.Objects},
				Pulse: &ctf.Pulse{Round: v2CtfTestRound, Signature: signature}, Recycled: s.f.Commit.Total, Coldkeys: v2CtfTestColdkeys(candidates),
				Bound: map[[16]byte]bool{v2CtfTestBound: true}},
			{Epoch: 10},
		}, FundedThrough: s.funded})
	if err != nil {
		t.Fatal(err)
	}
	if bound := p.Epochs[0].Bound; len(bound) != 1 || bound[0].ClientID != v2CtfTestBound || len(p.Epochs[0].Awards) != 8 {
		t.Fatalf("epoch 9 left out %v and drew %d winners", bound, len(p.Epochs[0].Awards))
	}
	s.census, s.p = census, p
	maps.Copy(s.c.objects, p.Objects)
	s.c.ctf.census[9] = v2TestCensus{hash: census.Hash, block: v2CtfTestCensusBlock}
	s.c.logs = nil
	s.c.committed(v2TestHead-40_000, 1, s.f.Commit, 0)
	s.c.finalized(v2TestHead-20_000, 1, 0, s.f.Commit)
	s.setPending(2, p.Commit)
	s.c.ctf.fleet = []v2TestFleetBound{{block: 120_000, client: v2CtfTestBound, validFrom: 8, validTo: 10}}
	for _, block := range active {
		s.c.ctf.active[v2TestBindingAt{client: v2CtfTestBound, epoch: 9, block: block}] = true
	}
}

// CAPTURETHEFLAG.md §4.4, §8 check 3: a census that lists a head-bound
// client, which P5 keeps out of every census, draws as the validator draws
// it. The check reads the v1 coordinator's FleetBound events from its first
// block, then, for the client whose binding covers epoch 9, bindingAt at
// epoch 9's start and end blocks, and leaves the client out when either is
// active: the root built that way is approved, and the check names the
// client. A client the chain does not show bound at either boundary, by an
// inactive binding, no event or one whose validity misses the epoch, is
// drawn, so the root that left it out is refused, and the check fails. Epoch
// 10, also covered by the binding, has no census, so nothing is read for it.
func TestV2CheckCtfLeavesOutHeadBoundCandidates(t *testing.T) {
	start, end := uint64(v2CtfTestGenesis+9*v2CtfTestEpochBlocks), uint64(v2CtfTestGenesis+10*v2CtfTestEpochBlocks)
	left := fmt.Sprintf("                     left out 1 head-bound candidates: 0x%x (provider, tm)\n", v2CtfTestBound)
	for _, c := range []struct {
		name   string
		active []uint64
		change func(s *v2CtfScenario)
		reads  int
	}{
		{"bound at both boundaries", []uint64{start, end}, nil, 2},
		{"bound only at the start", []uint64{start}, nil, 2},
		{"bound only at the end", []uint64{end}, nil, 2},
		{"a binding inactive at both boundaries", nil, nil, 2},
		{"no FleetBound event", []uint64{start, end}, func(s *v2CtfScenario) { s.c.ctf.fleet = nil }, 0},
		{"a binding whose validity misses the epoch", []uint64{start, end}, func(s *v2CtfScenario) { s.c.ctf.fleet[0].validFrom = 10 }, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newV2CtfScenario(t)
			s.boundScenario(t, c.active...)
			if c.change != nil {
				c.change(s)
			}
			output, err := s.c.run(s.argv("--challenge", "--demand"))
			heads := v2CtfTestHeads(len(s.c.ctf.fleet))
			bindingAt := slices.DeleteFunc(slices.Clone(s.c.blocks), func(block string) bool { return !strings.HasPrefix(block, "bindingAt@") })
			if c.reads > 0 && !slices.Equal(bindingAt, []string{"bindingAt@" + hexutil.EncodeUint64(start), "bindingAt@" + hexutil.EncodeUint64(end)}) || c.reads == 0 && len(bindingAt) != 0 {
				t.Fatalf("bindingAt reads %v", bindingAt)
			}
			if len(c.active) > 0 && c.change == nil {
				if err != nil || !strings.Contains(output, heads) || !strings.Contains(output, left) || !strings.HasSuffix(output, "  audit:             approve\n  previous accounts: 1 of root 0, no fault\n") ||
					len(s.c.transactions()) != 0 {
					t.Fatalf("%v\n%s", err, output)
				}
				return
			}
			refusal := "the capture the flag audit refuses pending root seq 2 (draw check: ctf: award rows are not the draw's winners: epoch 9 "
			if err == nil || !strings.HasPrefix(err.Error(), refusal) || !strings.Contains(output, heads) || strings.Contains(output, "left out") ||
				!strings.Contains(output, "  audit:             refuse (draw check: ") || len(s.c.transactions()) != 0 {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}

// CAPTURETHEFLAG.md §8: the head bindings fail closed like every other
// input. A node without archive state, which answers the coordinator's code
// at its first block with an error or with none, as Frontier does; a refused
// read of STSettlement2's v1 coordinator, its epochs' boundaries, its
// FleetBound events or bindingAt; or an epoch whose end is past the
// finalized block makes the audit unavailable. F's accounts are still found
// without fault from the published objects, and the check fails.
func TestV2CheckCtfHeadBindingsFailClosed(t *testing.T) {
	coordinator := common.HexToAddress(v2CtfTestV1Coordinator).Hex()
	for _, c := range []struct {
		name   string
		change func(s *v2CtfScenario)
		want   string
	}{
		{"a node without archive state", func(s *v2CtfScenario) { s.c.ctf.pruned = true },
			"the head bindings need an archive RPC: v1 coordinator " + coordinator + "'s code at its first block 100000: State already discarded for block"},
		{"the v1 coordinator unread", func(s *v2CtfScenario) { s.c.failAt["eth_call:v1Coordinator"] = 1 },
			"coordinator " + common.HexToAddress(v2CtfTestCoordinator).Hex() + " v1Coordinator(): synthetic refusal of eth_call:v1Coordinator"},
		{"a node answering no code at the first block", func(s *v2CtfScenario) { s.c.ctf.codeless = true },
			"the head bindings need an archive RPC: v1 coordinator " + coordinator + "'s code at its first block 100000: the RPC answered no code"},
		{"its first block unread", func(s *v2CtfScenario) { s.c.failAt["eth_call:epochStartBlock"] = 1 },
			"v1 coordinator " + coordinator + " epochStartBlock(0): synthetic refusal of eth_call:epochStartBlock"},
		{"an epoch's start unread", func(s *v2CtfScenario) { s.c.failAt["eth_call:epochStartBlock"] = 2 },
			"v1 coordinator " + coordinator + " epochStartBlock(9): synthetic refusal of eth_call:epochStartBlock"},
		{"an epoch not closed at the finalized block", func(s *v2CtfScenario) { s.c.ctf.unclosed = true },
			"epoch 9 boundaries [145000,200001] are not closed at the finalized block 200000"},
		{"its FleetBound events refused", func(s *v2CtfScenario) { s.c.failAt["eth_getLogs:FleetBound"] = 3 },
			"v1 coordinator " + coordinator + " FleetBound events: eth_getLogs [102000,102999]: synthetic log refusal"},
		{"an epoch's end unread", func(s *v2CtfScenario) { s.c.failAt["eth_call:epochEndBlock"] = 1 },
			"v1 coordinator " + coordinator + " epochEndBlock(9): synthetic refusal of eth_call:epochEndBlock"},
		{"a bindingAt refused", func(s *v2CtfScenario) { s.c.failAt["eth_call:bindingAt"] = 2 },
			fmt.Sprintf("v1 coordinator %s bindingAt(0x%x,9) at block 150000: synthetic refusal of eth_call:bindingAt", coordinator, v2CtfTestBound)},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newV2CtfScenario(t)
			s.boundScenario(t, v2CtfTestGenesis+9*v2CtfTestEpochBlocks)
			c.change(s)
			output, err := s.c.run(s.argv("--challenge", "--demand"))
			want := "the capture the flag audit could not run: " + c.want
			if err == nil || err.Error() != want || !strings.Contains(output, "  audit:             unavailable: "+want+"\n") ||
				strings.Contains(output, "approve") || !strings.HasSuffix(output, "  previous accounts: 1 of root 0, no fault\n") || len(s.c.transactions()) != 0 {
				t.Fatalf("%v\n%s\nwant %q", err, output, want)
			}
		})
	}
}

// ctfFaulty publishes P's accounts with F's carry account lowered by one
// rao, under P's header, and makes the root they build pending as seq 2: a
// provable fault.
func (s *v2CtfScenario) ctfFaulty(t *testing.T) *merkle.LedgerSumTree {
	t.Helper()
	accounts := slices.Clone(s.p.Accounts)
	accounts[0].Cumulative.SubUint64(&accounts[0].Cumulative, s.p.Accounts[0].Cumulative.Uint64()-s.f.Accounts[0].Cumulative.Uint64()+1)
	chunks, err := ledger.EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	header, err := ctf.DecodeHeader(s.p.Header)
	if err != nil {
		t.Fatal(err)
	}
	header.Accounts = nil
	for _, chunk := range chunks {
		header.Accounts = append(header.Accounts, ledger.RefOf(chunk))
		s.c.objects[ledger.RefOf(chunk)] = chunk
	}
	data, err := ctf.EncodeHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	s.c.objects[ledger.RefOf(data)] = data
	tree := v2TestTree(t, accounts)
	root, total := tree.Root()
	s.c.logs = nil
	s.c.committed(v2TestHead-40_000, 1, s.f.Commit, 0)
	s.c.finalized(v2TestHead-20_000, 1, 0, s.f.Commit)
	s.setPending(2, ledger.Root{Epoch: 10, Root: root, Total: total, LeafCount: tree.LeafCount(), LedgerHash: ledger.RefOf(data)})
	return tree
}

// CAPTURETHEFLAG.md §3.3, VERSION2-FINAL.md §9.3: challenges protect every
// account of a capture the flag pool as in the provider pool. A pending CTF
// root that lowers F's carry account is challenged with checkPath of the
// CTF pool, which deletes it; it is challenged as well when the audit is
// unavailable, since the accounts need none of its inputs, and the check
// then fails all the same.
func TestV2CheckCtfChallengesAFault(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		s := newV2CtfScenario(t)
		tree := s.ctfFaulty(t)
		if unavailable {
			s.web.amendments = nil
		}
		path, err := tree.Path(0)
		if err != nil {
			t.Fatal(err)
		}
		output, err := s.c.run(s.argv("--challenge"))
		audit, failed := "  audit:             challenge (accounts check: ", ""
		if unavailable {
			failed = "the capture the flag audit could not run: the signed amendments at " + s.web.server.URL + "/amendments.yml: amendment: HTTP 404; " + amendment.ErrUnknown.Error()
			audit = "  audit:             unavailable: " + failed
		}
		if (err == nil) != !unavailable || unavailable && err.Error() != failed || !strings.Contains(output, audit) || !strings.Contains(output, fmt.Sprintf("  previous accounts: 1 of root 0, 1 with a provable fault, 0 with no fault\n    account 0:      %v\n", merkle.LedgerFaultLowerValue)) ||
			!strings.Contains(output, "DistributionDeleted: pool 4294967297, seq 2\n") || len(s.c.transactions()) != 1 {
			t.Fatalf("unavailable %v: %v\n%s", unavailable, err, output)
		}
		proof, _ := s.f.Tree.Proof(0)
		want := vault2.PackCheckPath(big.NewInt(v2CtfTestPool), big.NewInt(0), v2LedgerAccount(0, &s.f.Accounts[0]), v2ProofNodes(proof), v2PathNodes(path))
		requireV2Tx(t, s.c.transactions()[0], 7, 60_000, 0, want)
		event, err := vault2.UnpackDistributionDeletedEvent(s.c.emitted[0])
		if len(s.c.emitted) != 1 || err != nil || event.NoId.Cmp(big.NewInt(v2CtfTestPool)) != 0 || event.Seq != 2 || event.RootIndex.Sign() != 0 || event.Index.Sign() != 0 ||
			event.Fault != uint8(merkle.LedgerFaultLowerValue) || event.Challenger != common.HexToAddress(testKeyAddr) {
			t.Fatalf("DistributionDeleted %+v (%v) of %d events", event, err, len(s.c.emitted))
		}
	}
}

// CAPTURETHEFLAG.md §2.4, §5, §8 check 1: E_c comes from the signed
// amendment entry and changes nothing in the audit: a committed census is
// drawn as committed. With the pool registered from epoch 8 and capture the
// flag in force from epoch 9, the pool's first root covers epoch 8, for which
// a census was committed. The root that draws it is approved; one that leaves
// it undrawn is refused at the census, and the check fails, since nothing in
// a first root can be challenged.
func TestV2CheckCtfDrawsEveryCommittedCensus(t *testing.T) {
	signature, _ := hex.DecodeString(v2CtfTestSignature)
	pulse := &ctf.Pulse{Round: v2CtfTestRound, Signature: signature}
	const block8 = v2CtfTestCensusBlock - 50_400
	for _, drawn := range []bool{true, false} {
		s := newV2CtfScenario(t)
		s.web.amendments = v2CtfTestAmendments(t, s.key, v2CtfTestEntry(9, v2CtfTestCarry))
		candidates := v2CtfTestCandidates(t)
		eight, err := ctf.EncodeCensus(&ctf.Census{NoID: v2CtfTestNoID, Epoch: 8, Candidates: candidates})
		if err != nil {
			t.Fatal(err)
		}
		inputs := []ctf.EpochInput{{Epoch: 8}, {Epoch: 9, Census: &ctf.CensusInput{Hash: s.census.Hash, Block: v2CtfTestCensusBlock, Time: v2CtfTestTime, Objects: s.p.Objects},
			Pulse: pulse, Coldkeys: v2CtfTestColdkeys(candidates)}}
		if drawn {
			inputs[0] = ctf.EpochInput{Epoch: 8, Census: &ctf.CensusInput{Hash: eight.Hash, Block: block8, Time: v2CtfTestTime, Objects: eight.Objects}, Pulse: pulse, Coldkeys: v2CtfTestColdkeys(candidates)}
		}
		first, err := ctf.Build(context.Background(), ctf.BuildInput{NoID: v2CtfTestNoID, ChainID: v2TestChainID, Netuid: v2CtfTestNetuid, Epoch: 9, FirstEpoch: 8,
			CarryColdkey: v2CtfTestCarryColdkey, Epochs: inputs, FundedThrough: s.funded})
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(s.c.objects, first.Objects)
		maps.Copy(s.c.objects, eight.Objects)
		// Epoch 8's census was committed; a first root has no carry out to
		// return.
		s.c.ctf.census[8] = v2TestCensus{hash: eight.Hash, block: block8}
		s.c.ctf.times[block8] = v2CtfTestTime
		s.c.ctf.recycled = map[uint64]*big.Int{}
		s.c.roots, s.c.logs = nil, nil
		s.setPending(1, first.Commit)
		output, err := s.c.run(s.argv())
		terms := "  finalized root:    none; the pending root is the pool's first\n" +
			"  capture the flag:  operator 1, from epoch 9 by the signed amendment entry, carry contract " + common.HexToAddress(v2CtfTestCarry).Hex() + "\n" +
			fmt.Sprintf("                     carry coldkey 0x%x\n", v2CtfTestCarryColdkey)
		if drawn {
			want := terms + v2CtfTestHeads(0) + fmt.Sprintf("  epoch 8:           census %v at block %d, 10 candidates; drand round 1000, 8 winners\n", eight.Hash, block8)
			if err != nil || !strings.Contains(output, want) || !strings.HasSuffix(output, "  audit:             approve\n") {
				t.Fatalf("drawn: %v\n%s\nwant:\n%s", err, output, want)
			}
		} else {
			refusal := fmt.Sprintf("census check: ctf: header census is not the committed census: epoch 8, committed %v", eight.Hash)
			// The audit refused before any draw, so it read no head bindings.
			if err == nil || err.Error() != "the capture the flag audit refuses pending root seq 1 ("+refusal+")" || !strings.HasSuffix(output, terms+"  audit:             refuse ("+refusal+")\n") ||
				s.c.calls["eth_call:v1Coordinator"] != 0 || len(s.c.fleetSpans) != 0 {
				t.Fatalf("undrawn: %v\n%s", err, output)
			}
		}
		if s.c.calls["eth_call:ctfCensus"] != 2 || s.c.calls["eth_getBlockByNumber:"+hexutil.EncodeUint64(block8)] != 1 || len(s.c.transactions()) != 0 {
			t.Fatalf("drawn %v: observed %s", drawn, s.c.observed())
		}
	}
}

// CAPTURETHEFLAG.md §8: a pool's first root has no accounts to protect. The
// check succeeds when the audit approves it, and fails, with nothing sent,
// when the audit refuses it or could not run.
func TestV2CheckCtfFirstRoot(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(s *v2CtfScenario)
		audit  string
		failed string
	}{
		{"approved", func(*v2CtfScenario) {}, "approve", ""},
		{"refused", func(s *v2CtfScenario) {
			s.c.ctf.census[8] = v2TestCensus{hash: s.census.Hash, block: v2CtfTestCensusBlock}
		},
			"refuse (census check: ctf: header census is not the committed census: epoch 8, committed %v)", "the capture the flag audit refuses pending root seq 1 (census check: ctf: header census is not the committed census: epoch 8, committed %v)"},
		{"unavailable", func(s *v2CtfScenario) { s.web.amendments = nil },
			"unavailable: the capture the flag audit could not run: the signed amendments at %s/amendments.yml: amendment: HTTP 404; " + amendment.ErrUnknown.Error(),
			"the capture the flag audit could not run: the signed amendments at %s/amendments.yml: amendment: HTTP 404; " + amendment.ErrUnknown.Error()},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newV2CtfScenario(t)
			s.c.roots, s.c.logs = nil, nil
			s.setPending(1, s.f.Commit)
			c.change(s)
			output, err := s.c.run(s.argv("--challenge", "--demand"))
			fill := func(text string) string {
				return strings.NewReplacer("%v", s.census.Hash.String(), "%s", s.web.server.URL).Replace(text)
			}
			if (err == nil) != (c.failed == "") || err != nil && err.Error() != fill(c.failed) || !strings.HasSuffix(output, "  audit:             "+fill(c.audit)+"\n") ||
				!strings.Contains(output, "  finalized root:    none; the pending root is the pool's first\n") || len(s.c.transactions()) != 0 {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}

// CAPTURETHEFLAG.md §8, VERSION2-FINAL.md §9.3: with the audit unavailable
// and the pending root's header withheld, F's accounts are unverifiable from
// the published objects alone: --demand demands them, each paying the fee
// with its proof against F, and the check fails either way.
func TestV2CheckCtfUnavailableStillDemands(t *testing.T) {
	for _, send := range []bool{false, true} {
		s := newV2CtfScenario(t)
		s.web.amendments = nil
		delete(s.c.objects, s.p.Commit.LedgerHash)
		var flags []string
		if send {
			flags = append(flags, "--demand")
		}
		output, err := s.c.run(s.argv(flags...))
		failed := "the capture the flag audit could not run: the signed amendments at " + s.web.server.URL + "/amendments.yml: amendment: HTTP 404; " + amendment.ErrUnknown.Error()
		if !send {
			failed = "pending root seq 2 leaves the accounts of finalized root 0 unverifiable; --demand sends the demand set\n" + failed
		}
		if err == nil || err.Error() != failed || !strings.Contains(output, "  previous accounts: 1 of root 0, unverifiable: the published objects are missing or do not reproduce the pending root\n") {
			t.Fatalf("--demand %v: %v\n%s", send, err, output)
		}
		if !send {
			if len(s.c.transactions()) != 0 {
				t.Fatal("demanded without --demand")
			}
			continue
		}
		proof, _ := s.f.Tree.Proof(0)
		data, err := v2TestABI.Pack("demandPath", big.NewInt(v2CtfTestPool), big.NewInt(0), v2LedgerAccount(0, &s.f.Accounts[0]), v2ProofNodes(proof))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.c.transactions()) != 1 || !strings.Contains(output, "PathDemanded: pool 4294967297, seq 2, account 0 of root 0\n") {
			t.Fatalf("%d demands\n%s", len(s.c.transactions()), output)
		}
		requireV2Tx(t, s.c.transactions()[0], 7, 60_000, v2TestFee, data)
	}
}

// thirdRoot finalizes P as root 1 and commits P2, which covers epoch 11, for
// which no census was committed, as pending seq 3.
func (s *v2CtfScenario) thirdRoot(t *testing.T) *ctf.Ledger {
	t.Helper()
	s.funded[11] = *uint256.NewInt(8_225_000_007)
	s.c.funded[11] = big.NewInt(8_225_000_007)
	p2, err := ctf.Build(context.Background(), ctf.BuildInput{NoID: v2CtfTestNoID, ChainID: v2TestChainID, Netuid: v2CtfTestNetuid, Epoch: 11, FirstEpoch: 8,
		CarryColdkey: v2CtfTestCarryColdkey, Previous: &ctf.Previous{Root: s.p.Commit, Accounts: s.p.Accounts, State: s.p.State},
		Epochs: []ctf.EpochInput{{Epoch: 11}}, FundedThrough: s.funded})
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(s.c.objects, p2.Objects)
	s.c.roots = append(s.c.roots, v2TestRoot(s.p.Tree, 10, s.p.Commit.LedgerHash))
	s.c.logs = nil
	s.c.committed(v2TestHead-40_000, 1, s.f.Commit, 0)
	s.c.finalized(v2TestHead-30_000, 1, 0, s.f.Commit)
	s.c.committed(v2TestHead-20_000, 2, s.p.Commit, 0)
	s.c.finalized(v2TestHead-10_000, 2, 1, s.p.Commit)
	s.setPending(3, p2.Commit)
	return p2
}

// CAPTURETHEFLAG.md §8: the fallback reads the capture the flag headers of
// the pool's roots. When the latest finalized root's objects are missing,
// the check protects the newest older root whose objects rebuild it,
// whether the audit ran or could not, and fails, since the accounts added
// after it cannot be checked; with no such root it says so, and fails.
func TestV2CheckCtfFallsBack(t *testing.T) {
	for _, c := range []struct {
		name        string
		older       bool
		unavailable bool
	}{
		{"no older root", false, false},
		{"no older root, audit unavailable", false, true},
		{"an older root", true, false},
		{"an older root, audit unavailable", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newV2CtfScenario(t)
			missing := s.f.Commit.LedgerHash
			if c.older {
				s.thirdRoot(t)
				missing = s.p.Commit.LedgerHash
			}
			delete(s.c.objects, missing)
			failed := ""
			if c.unavailable {
				s.web.amendments = nil
				failed = "\nthe capture the flag audit could not run: the signed amendments at " + s.web.server.URL + "/amendments.yml: amendment: HTTP 404; " + amendment.ErrUnknown.Error()
			}
			output, err := s.c.run(s.argv("--challenge", "--demand"))
			want := "no finalized root of pool 4294967297 has objects that rebuild it: its accounts cannot be checked, challenged or demanded" + failed
			lines := []string{}
			if c.older {
				want = "finalized root 1's objects are missing or do not rebuild it: the accounts added after root 0 cannot be checked" + failed
				lines = append(lines, "  fallback:          finalized root 1's objects are missing or do not rebuild it; protecting finalized root 0, the newest whose objects do\n  previous accounts: 1 of root 0, no fault\n")
			}
			if c.unavailable {
				lines = append(lines, fmt.Sprintf("  fallback:          finalized root %d is unverifiable: object %v: GET ", len(s.c.roots)-1, ledger.Ref(missing)))
			} else {
				lines = append(lines, "  audit:             refuse (fetch check: ")
			}
			if err == nil || err.Error() != want || len(s.c.transactions()) != 0 {
				t.Fatalf("%v\nwant %q\n%s", err, want, output)
			}
			for _, line := range lines {
				if !strings.Contains(output, line) {
					t.Fatalf("output lacks %q:\n%s", line, output)
				}
			}
		})
	}
}

// --no_id at or above 2^32 is a capture the flag pool only as 2^32 + n with
// 1 <= n < 2^32; any other is refused before any RPC, as are malformed
// --amendments_url and --drand_url. Their defaults are ur.xyz's file and
// drand's own relay.
func TestV2CheckCtfRefusesFlags(t *testing.T) {
	for _, c := range []struct {
		flags   []string
		refusal string
	}{
		{[]string{"--no_id=4294967296"}, "--no_id: 4294967296 is neither an operator's pool, below 2^32, nor a capture the flag pool 2^32 + n with 1 <= n < 2^32"},
		{[]string{"--no_id=8589934592"}, "--no_id: 8589934592 is neither an operator's pool, below 2^32, nor a capture the flag pool 2^32 + n with 1 <= n < 2^32"},
		{[]string{"--no_id=4294967297", "--amendments_url=ur.xyz/amendments.yml"}, `--amendments_url: "ur.xyz/amendments.yml" is not an http(s) URL without a query`},
		{[]string{"--no_id=4294967297", "--drand_url=https://api.drand.sh?x=1"}, `--drand_url: "https://api.drand.sh?x=1" is not an http(s) URL without a query`},
	} {
		s := newV2CheckScenario(t)
		if _, err := s.c.run(s.c.checkArgv(c.flags...)); err == nil || err.Error() != c.refusal || s.c.observed() != "" {
			t.Fatalf("%q: %v; observed %s", c.flags, err, s.c.observed())
		}
	}
	opts, err := parseArgv(t, []string{"v2", "check", "--all", "--vault=" + v2TestVault, "--no_id=4294967297", "--rpc=u"})
	if err != nil || strOpt(opts, "--amendments_url") != amendment.URL || strOpt(opts, "--drand_url") != "https://api.drand.sh" {
		t.Fatalf("defaults %q and %q, %v", strOpt(opts, "--amendments_url"), strOpt(opts, "--drand_url"), err)
	}
}

// The local ABI of the capture the flag views has the contracts' selectors
// (sn-contracts' STSettlement2Ctf and STCtfCarry).
func TestV2CtfViewSelectors(t *testing.T) {
	views, err := v2CtfViews()
	if err != nil {
		t.Fatal(err)
	}
	for name, selector := range map[string]string{"ctfCensus": "4396736d", "recycledFor": "02ed4e54", "coldkey": "6d9f69f2", "vault": "fbfa77cf", "poolId": "3e0dc34e"} {
		if got := hex.EncodeToString(views.Methods[name].ID); got != selector {
			t.Errorf("%s selector %s, want %s", name, got, selector)
		}
	}
}

// newV2CtfClaimScenario is v2ClaimScenario, alice as account 1 of pool 3's
// root 1, with alice also account 1 of the one finalized root of pool 3's
// capture the flag pool 2^32 + 3, at 0.4 α: the same index in two pools.
func newV2CtfClaimScenario(t *testing.T) (*v2ClaimScenario, uint64, *merkle.LedgerSumTree) {
	t.Helper()
	s := newV2ClaimScenario(t)
	pool, _ := amendment.CtfPoolID(v2TestPool)
	tree := v2TestTree(t, []merkle.LedgerAccount{{Coldkey: v2TestKey(1), Cumulative: *uint256.NewInt(100_000_000)}, {Coldkey: s.alice, Cumulative: *uint256.NewInt(400_000_000)}})
	s.c.pools[pool] = []stabi.ISTSettlementVault2Root{v2TestRoot(tree, 8, [32]byte{0xc7})}
	s.c.setAccounts(s.entry, v2TestEntry{Vault: strings.ToLower(v2TestVault), NoID: pool, Index: 1, RootIndex: v2Ptr(uint64(0)), Cumulative: "400000000", Proof: v2TestProofJSON(t, tree, 1), Claimed: "0"})
	return s, pool, tree
}

// ctfItem is alice's capture the flag claim as vault2 and the batcher take it.
func ctfItem(t *testing.T, pool uint64, tree *merkle.LedgerSumTree, alice [32]byte) stabi.STClaimBatcherClaim {
	t.Helper()
	proof, err := tree.Proof(1)
	if err != nil {
		t.Fatal(err)
	}
	return stabi.STClaimBatcherClaim{NoId: new(big.Int).SetUint64(pool), RootIndex: big.NewInt(0),
		Account: stabi.LedgerTreeAccount{Index: big.NewInt(1), Coldkey: alice, Cumulative: big.NewInt(400_000_000)}, Proof: v2ProofNodes(proof)}
}

// CAPTURETHEFLAG.md §10: status lists a coldkey's capture the flag account
// as a pool of its own, with its finalized cumulative, root and claimed
// watermark read from vault2 for that pool; the pool account's lines are
// unchanged.
func TestV2StatusListsCtfAccounts(t *testing.T) {
	s, _, _ := newV2CtfClaimScenario(t)
	output, err := s.c.run(s.c.statusArgv())
	want := "  pool 3, account 1:\n" +
		"    finalized:  5.000000007 α (5000000007 rao) at root 1\n" +
		"    claimed:    1.000000000 α (1000000000 rao)\n" +
		"    claimable:  4.000000007 α (4000000007 rao)\n" +
		"    pending:    6.000000000 α (6000000000 rao)\n" +
		"  pool 4294967299, account 1:\n" +
		"    finalized:  0.400000000 α (400000000 rao) at root 0\n" +
		"    claimed:    0.000000000 α (0 rao)\n" +
		"    claimable:  0.400000000 α (400000000 rao)\n" +
		"    pending:    none\n"
	if err != nil || !strings.HasSuffix(output, want) || s.c.observed() != "eth_call:claimed=2 eth_chainId=1" {
		t.Fatalf("status = %v:\n%s\nwant suffix:\n%s\nobserved %s", err, output, want, s.c.observed())
	}
}

// CAPTURETHEFLAG.md §10: snclaim treats each (vault, noId, index) as its own
// unit, so a capture the flag account is claimed from vault2 like any pool's.
// At the same index as the coldkey's pool account, --index alone picks
// neither; --no_id=2^32 + 3 claims the capture the flag account alone,
// verified against that pool's root; --all claims both, the pool account's
// claim exactly what it is alone; and --batch claims both in one claimMany.
func TestV2ClaimCtfAccountAsItsOwnUnit(t *testing.T) {
	s, pool, tree := newV2CtfClaimScenario(t)
	_, err := s.c.run(s.c.claimArgv("--index=1"))
	if err == nil || err.Error() != "coldkey "+aliceSS58+" (0x"+aliceHex+") has 2 finalized accounts matching the selection [--no_id=3 --index=1, --no_id=4294967299 --index=1]; claim exactly one" || len(s.c.transactions()) != 0 {
		t.Fatalf("--index alone: %v", err)
	}

	output, err := s.c.run(s.c.claimArgv("--no_id=4294967299"))
	item := ctfItem(t, pool, tree, s.alice)
	if err != nil || len(s.c.transactions()) != 1 || !strings.Contains(output, "Claimed: pool 4294967299, account 1, root 0\n") ||
		s.c.claimed[[2]uint64{pool, 1}].Cmp(big.NewInt(400_000_000)) != 0 || s.c.claimed[[2]uint64{v2TestPool, 1}].Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatalf("--no_id of the capture the flag pool: %v\n%s", err, output)
	}
	requireV2Tx(t, s.c.transactions()[0], 7, ClaimBatchGasPerClaim, 0, vault2.PackClaim(item.NoId, item.RootIndex, item.Account, item.Proof))

	s, pool, tree = newV2CtfClaimScenario(t)
	output, err = s.c.run(s.c.claimArgv("--all"))
	item = ctfItem(t, pool, tree, s.alice)
	if err != nil || len(s.c.transactions()) != 2 || !strings.Contains(output, "Claimed: pool 3, account 1, root 1\n") || !strings.Contains(output, "Claimed: pool 4294967299, account 1, root 0\n") ||
		s.c.claimed[[2]uint64{pool, 1}].Cmp(big.NewInt(400_000_000)) != 0 || s.c.claimed[[2]uint64{v2TestPool, 1}].Cmp(big.NewInt(5_000_000_007)) != 0 {
		t.Fatalf("--all: %v\n%s", err, output)
	}
	requireV2Tx(t, s.c.transactions()[0], 7, ClaimBatchGasPerClaim, 0, s.calldata(t))
	requireV2Tx(t, s.c.transactions()[1], 8, ClaimBatchGasPerClaim, 0, vault2.PackClaim(item.NoId, item.RootIndex, item.Account, item.Proof))

	s, pool, tree = newV2CtfClaimScenario(t)
	s.c.batcher = common.HexToAddress(v2TestBatcher)
	output, err = s.c.run(s.c.claimArgv("--all", "--batch="+v2TestBatcher))
	proof, _ := s.tree.Proof(1)
	poolItem := stabi.STClaimBatcherClaim{NoId: big.NewInt(v2TestPool), RootIndex: big.NewInt(1), Account: stabi.LedgerTreeAccount{Index: big.NewInt(1), Coldkey: s.alice, Cumulative: big.NewInt(5_000_000_007)}, Proof: v2ProofNodes(proof)}
	if err != nil || len(s.c.transactions()) != 1 || s.c.claimed[[2]uint64{pool, 1}].Cmp(big.NewInt(400_000_000)) != 0 || s.c.claimed[[2]uint64{v2TestPool, 1}].Cmp(big.NewInt(5_000_000_007)) != 0 {
		t.Fatalf("--all --batch: %v\n%s", err, output)
	}
	requireBatchTx(t, s.c.transactions()[0], 7, v2BatchLeast(2), claimBatcher.PackClaimMany(common.HexToAddress(v2TestVault), []stabi.STClaimBatcherClaim{poolItem, ctfItem(t, pool, tree, s.alice)}, new(big.Int).SetUint64(ClaimBatchGasPerClaim)))
}
