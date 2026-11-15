//go:build linux || darwin

// The capture the flag audit through the settlement v2 audit's real decision,
// custody and auditor transactions, over CTF roots that the ctf package
// builds: a census, quicknet's real pulse of round 1000 and the walk.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/amendment"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ctf"
	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/stabi"
)

// quicknet's real signature of round 1000 (ctf/draw_test.go), and a census
// commitment time whose draw round it is.
const (
	settlementV2CtfTestRound1000 = "b44679b9a59af2ec876b1a6b1ad52ea9b1615fc3982b19576350f93447cb1125e342b73a8dd2bacbe47e4b6b63ed5e39"
	settlementV2CtfTestTime      = 1_692_806_304
)

const settlementV2CtfTestPoolID = 1<<32 + settlementV2AuditTestNoID

func settlementV2CtfTestPulse(t testing.TB) *ctf.Pulse {
	t.Helper()
	signature, err := hex.DecodeString(settlementV2CtfTestRound1000)
	if err != nil {
		t.Fatal(err)
	}
	pulse := &ctf.Pulse{Round: 1000, Signature: signature}
	if ctf.DrawRound(settlementV2CtfTestTime) != 1000 || pulse.Verify() != nil {
		t.Fatal("quicknet's round 1000 is not the test census's draw")
	}
	return pulse
}

// settlementV2CtfTestReader answers the chain facts of the test pool.
type settlementV2CtfTestReader struct {
	in    ctf.VerifyInput
	err   error
	reads atomic.Int32
}

func (self *settlementV2CtfTestReader) ctfInput(_ context.Context, state *settlementV2PoolState, noID uint64) (*ctf.VerifyInput, error) {
	self.reads.Add(1)
	if self.err != nil {
		return nil, self.err
	}
	if noID != settlementV2AuditTestNoID || state.noID != settlementV2CtfTestPoolID {
		return nil, fmt.Errorf("pool %d of operator %d", state.noID, noID)
	}
	in := self.in
	in.Epochs = map[uint64]ctf.EpochFacts{}
	for epoch, facts := range self.in.Epochs {
		in.Epochs[epoch] = facts
	}
	return &in, nil
}

// settlementV2CtfTestScenario is epoch 5's CTF root of operator 1, first
// epoch 5: eight provider units in IS, of which the draw takes five, and one
// extender in TM, with the funding and the facts the chain states.
type settlementV2CtfTestScenario struct {
	built  *ctf.Ledger
	census *ctf.CensusObjects
	facts  ctf.EpochFacts
	funded map[uint64]uint256.Int
	carry  [32]byte
}

func newSettlementV2CtfTestScenario(t *testing.T) *settlementV2CtfTestScenario {
	t.Helper()
	return newSettlementV2CtfTestScenarioBound(t, nil)
}

// newSettlementV2CtfTestScenarioBound builds the scenario's root with bound,
// the head-bound clients the operator's draw leaves out.
func newSettlementV2CtfTestScenarioBound(t *testing.T, bound map[[16]byte]bool) *settlementV2CtfTestScenario {
	t.Helper()
	is, _ := ctf.ParseCountry("is")
	tm, _ := ctf.ParseCountry("tm")
	candidate := func(role ctf.Role, country ctf.Country, unit, client byte) ctf.Candidate {
		return ctf.Candidate{Role: role, Country: country, Unit: [32]byte{unit}, Family: ctf.FamilyIPv4, ClientID: [16]byte{client}}
	}
	candidates := []ctf.Candidate{candidate(ctf.RoleExtender, tm, 9, 9)}
	coldkeys := map[[16]byte][32]byte{{9}: {0xc9}}
	for unit := byte(1); unit <= 8; unit++ {
		candidates = append(candidates, candidate(ctf.RoleProvider, is, unit, unit))
		coldkeys[[16]byte{unit}] = [32]byte{0xc0 + unit}
	}
	census, err := ctf.EncodeCensus(&ctf.Census{NoID: settlementV2AuditTestNoID, Epoch: 5, Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	scenario := &settlementV2CtfTestScenario{census: census, carry: [32]byte{0xca, 0x77}, funded: map[uint64]uint256.Int{5: *uint256.NewInt(119_583_893_572)}}
	scenario.facts = ctf.EpochFacts{Census: &ctf.CensusCommitment{Hash: census.Hash, Block: 601, Time: settlementV2CtfTestTime}, Pulse: settlementV2CtfTestPulse(t)}
	scenario.built, err = ctf.Build(t.Context(), ctf.BuildInput{NoID: settlementV2AuditTestNoID, ChainID: settlementV2TestChainID, Netuid: 25, Epoch: 5, FirstEpoch: 5, CarryColdkey: scenario.carry,
		Epochs:        []ctf.EpochInput{{Epoch: 5, Census: &ctf.CensusInput{Hash: census.Hash, Block: 601, Time: settlementV2CtfTestTime, Objects: census.Objects}, Pulse: scenario.facts.Pulse, Coldkeys: coldkeys, Bound: bound}},
		FundedThrough: scenario.funded})
	if err != nil {
		t.Fatal(err)
	}
	if result := scenario.built.Epochs[0]; !result.Drawn || len(result.Awards) != 6 || len(result.Bound) != len(bound) {
		t.Fatalf("the scenario drew %d winners and left out %d", len(result.Awards), len(result.Bound))
	}
	return scenario
}

// useCtf publishes the scenario, registers the CTF pool with its funding and
// pending root (seq 1, committed at 605, before end(6) at block 700), and gives
// the audit the CTF pool and the facts reader.
func (self *settlementV2AuditTestFixture) useCtf(t *testing.T, scenario *settlementV2CtfTestScenario, facts *settlementV2CtfTestReader) settlementV2AuditPool {
	t.Helper()
	self.objects.publish(scenario.census.Objects)
	self.objects.publish(scenario.built.Objects)
	self.chain.lock.Lock()
	self.chain.pools[settlementV2CtfTestPoolID] = stabi.ISTSettlementVault2Pool{Hotkey: [32]byte{0x9c}, FirstEpoch: 5, Registered: true}
	self.chain.funded[settlementV2CtfTestPoolID] = map[uint64]*big.Int{}
	for epoch, value := range scenario.funded {
		self.chain.funded[settlementV2CtfTestPoolID][epoch] = value.ToBig()
	}
	self.chain.lock.Unlock()
	self.chain.advanceTo(609)
	self.chain.mutate(func(state *settlementV2TestState) {
		state.pending[settlementV2CtfTestPoolID] = settlementV2AuditTestPending(1, scenario.built.Commit, 605)
	})
	self.audit.ctf = facts
	return settlementV2AuditPool{noID: settlementV2CtfTestPoolID, objects: self.audit.pools[0].objects, ctf: &settlementV2CtfPool{noID: settlementV2AuditTestNoID}}
}

func settlementV2CtfTestFacts(scenario *settlementV2CtfTestScenario) *settlementV2CtfTestReader {
	return &settlementV2CtfTestReader{in: ctf.VerifyInput{NoID: settlementV2AuditTestNoID, ChainID: settlementV2TestChainID, Netuid: 25, FirstEpoch: 5, CarryColdkey: scenario.carry,
		Epochs: map[uint64]ctf.EpochFacts{5: scenario.facts}}}
}

func (self *settlementV2AuditTestFixture) ctfRecord(t *testing.T, seq uint64) *settlementV2AuditRecord {
	t.Helper()
	var record settlementV2AuditRecord
	ok, err := self.audit.custody.readJSON(self.audit.custody.path("audits", fmt.Sprintf("%d-%d.json", uint64(settlementV2CtfTestPoolID), seq)), &record)
	if err != nil || !ok {
		t.Fatalf("audit record of CTF seq %d: %v %v", seq, ok, err)
	}
	return &record
}

// CAPTURETHEFLAG.md §8: a CTF root that its census, draw and walk reproduce
// exactly is approved with the auditor key, as the provider pool's is: one
// approveDistribution of pool 2^32 + 1. The awards are kept as the epoch's
// fixed inputs, under the CTF pool's id.
func TestSettlementV2CtfAuditApprovesReproducedRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	scenario := newSettlementV2CtfTestScenario(t)
	pool := fixture.useCtf(t, scenario, settlementV2CtfTestFacts(scenario))
	if err := fixture.audit.audit(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	record := fixture.ctfRecord(t, 1)
	if record.Decision != settlementV2DecisionApprove || record.Check != "none" || record.Error != "" || record.Done {
		t.Fatalf("record %+v", record)
	}
	calls := fixture.sentCalls(t)
	want := stabi.NewSTSettlement2().PackApproveDistribution(big.NewInt(settlementV2CtfTestPoolID), 1, scenario.built.Commit.Root, scenario.built.Commit.LedgerHash)
	if len(calls) != 1 || calls[0].to != settlementV2TestSettlement || !bytes.Equal(calls[0].data, want) {
		t.Fatalf("sent %d transactions, want the approval of the CTF root", len(calls))
	}
	if rows, ok, err := fixture.audit.custody.ValidRows(t.Context(), settlementV2CtfTestPoolID, 5); err != nil || !ok || len(rows) != 1 || rows[0] != scenario.built.Epochs[0].AwardsRef {
		t.Fatalf("the awards were not kept as epoch 5's fixed inputs: %v %v %v", rows, ok, err)
	}
	if _, ok, _ := fixture.audit.custody.ValidRows(t.Context(), settlementV2AuditTestNoID, 5); ok {
		t.Fatal("the CTF awards were kept under the provider pool")
	}
}

// The audit recomputes rather than trusts: a root whose census is not the one
// committed, whose draw is not the one the committed census, the chain and
// quicknet's pulse give, whose carry is another's or whose total is not
// vault2's funding is refused with nothing sent; a pulse that does not verify,
// or none, decides nothing and is tried again.
func TestSettlementV2CtfAuditRefusesWhatDoesNotReproduce(t *testing.T) {
	t.Parallel()
	pulse := settlementV2CtfTestPulse(t)
	epoch := func(in *ctf.VerifyInput, change func(*ctf.EpochFacts)) {
		facts := in.Epochs[5]
		if facts.Census != nil {
			census := *facts.Census
			facts.Census = &census
		}
		change(&facts)
		in.Epochs[5] = facts
	}
	for _, test := range []struct {
		name    string
		facts   func(*ctf.VerifyInput)
		funding uint64
		check   string
		retry   error
	}{
		{name: "another committed census", facts: func(in *ctf.VerifyInput) {
			epoch(in, func(facts *ctf.EpochFacts) { facts.Census.Hash = ledger.RefOf([]byte("another census")) })
		}, check: "census"},
		{name: "no census committed", facts: func(in *ctf.VerifyInput) {
			epoch(in, func(facts *ctf.EpochFacts) { facts.Census, facts.Pulse = nil, nil })
		}, check: "census"},
		{name: "a census committed a round later", facts: func(in *ctf.VerifyInput) {
			epoch(in, func(facts *ctf.EpochFacts) {
				facts.Census.Time += 3
				facts.Pulse = &ctf.Pulse{Round: 1001, Signature: pulse.Signature}
			})
		}, check: "seed"},
		{name: "another chain seeds another draw", facts: func(in *ctf.VerifyInput) { in.ChainID++ }, check: "draw"},
		{name: "another subnet seeds another draw", facts: func(in *ctf.VerifyInput) { in.Netuid++ }, check: "draw"},
		{name: "another carry coldkey", facts: func(in *ctf.VerifyInput) { in.CarryColdkey = [32]byte{0xbe} }, check: "header"},
		{name: "another funding", funding: 1, check: "amounts"},
		{name: "a pulse that does not verify", facts: func(in *ctf.VerifyInput) {
			epoch(in, func(facts *ctf.EpochFacts) {
				signature := bytes.Clone(pulse.Signature)
				signature[47] ^= 1
				facts.Pulse = &ctf.Pulse{Round: 1000, Signature: signature}
			})
		}, retry: ctf.ErrPulse},
		{name: "no pulse", facts: func(in *ctf.VerifyInput) { epoch(in, func(facts *ctf.EpochFacts) { facts.Pulse = nil }) }, retry: ctf.ErrInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			scenario := newSettlementV2CtfTestScenario(t)
			reader := settlementV2CtfTestFacts(scenario)
			if test.facts != nil {
				test.facts(&reader.in)
			}
			pool := fixture.useCtf(t, scenario, reader)
			if test.funding != 0 {
				fixture.chain.lock.Lock()
				funded := fixture.chain.funded[settlementV2CtfTestPoolID][5]
				fixture.chain.funded[settlementV2CtfTestPoolID][5] = new(big.Int).Add(funded, new(big.Int).SetUint64(test.funding))
				fixture.chain.lock.Unlock()
			}
			err := fixture.audit.audit(t.Context(), pool)
			if test.retry != nil {
				if !errors.Is(err, test.retry) || len(fixture.chain.accepted()) != 0 {
					t.Fatalf("an unverifiable draw decided: %v", err)
				}
				if record, err := fixture.audit.custody.auditRecord(settlementV2CtfTestPoolID, settlementV2AuditTestPending(1, scenario.built.Commit, 605)); err != nil || record.Decision != "" {
					t.Fatalf("an unverifiable draw recorded %+v %v", record, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			record := fixture.ctfRecord(t, 1)
			if record.Decision != settlementV2DecisionRefuse || record.Check != test.check || record.Error == "" || !record.Done || record.Outcome != "refused" {
				t.Fatalf("record %+v", record)
			}
			if len(fixture.chain.accepted()) != 0 {
				t.Fatal("a refused CTF root sent a transaction")
			}
		})
	}
	// Without capture the flag in force nothing is decided or sent.
	fixture := newSettlementV2AuditTestFixture(t)
	scenario := newSettlementV2CtfTestScenario(t)
	reader := settlementV2CtfTestFacts(scenario)
	reader.err = errSettlementV2CtfNotInForce
	pool := fixture.useCtf(t, scenario, reader)
	if err := fixture.audit.audit(t.Context(), pool); !errors.Is(err, errSettlementV2CtfNotInForce) || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("an audit without capture the flag in force: %v", err)
	}
	if record, err := fixture.audit.custody.auditRecord(settlementV2CtfTestPoolID, settlementV2AuditTestPending(1, scenario.built.Commit, 605)); err != nil || record.Decision != "" {
		t.Fatalf("an audit without capture the flag in force recorded %+v %v", record, err)
	}
}

// The draw leaves out the census's head-bound candidates, as the operator's
// must (ctf/doc.go): a root drawn without them is approved, and the census's
// breach of P5 (CAPTURETHEFLAG.md §4.2) is reported, since a committed census
// cannot be rebuilt and refusing it would stall every later root of the pool.
// A root whose draw kept the bound client is refused for its draw (§8 check 3).
func TestSettlementV2CtfAuditDrawsWithoutHeadBoundCandidates(t *testing.T) {
	t.Parallel()
	client := [16]byte{1}
	for _, test := range []struct {
		name     string
		bound    map[[16]byte]bool
		decision string
	}{
		{name: "left out of the draw", bound: map[[16]byte]bool{client: true}, decision: settlementV2DecisionApprove},
		{name: "kept in the draw", decision: settlementV2DecisionRefuse},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			ctx, owner, err := newReleaseDiagnostics(t.Context(), &output, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			fixture := newSettlementV2AuditTestFixture(t)
			// Client 1 holds a fleet binding at epoch 5's start boundary.
			fixture.chain.fleetBound(t, 190, client, 5, 5)
			fixture.chain.setActive(client, 5, 500, true)
			scenario := newSettlementV2CtfTestScenarioBound(t, test.bound)
			pool := fixture.useCtf(t, scenario, settlementV2CtfTestFacts(scenario))
			if err := fixture.audit.audit(ctx, pool); err != nil {
				t.Fatal(err)
			}
			// Closing the owner writes every record before the buffer is read.
			if err := owner.close(); err != nil {
				t.Fatal(err)
			}
			record := fixture.ctfRecord(t, 1)
			reported := strings.Contains(output.String(), "code=ledger_audit_ctf_census_bound native_epoch=0 epoch_known=false")
			switch test.decision {
			case settlementV2DecisionApprove:
				if record.Decision != test.decision || record.Check != "none" || !reported || len(fixture.chain.accepted()) != 1 {
					t.Fatalf("record %+v, reported %t: %s", record, reported, output.String())
				}
			default:
				if record.Decision != test.decision || record.Check != "draw" || !strings.Contains(record.Error, ctf.ErrAwardWinner.Error()) || len(fixture.chain.accepted()) != 0 {
					t.Fatalf("record %+v", record)
				}
			}
		})
	}
}

// The relay is trusted for nothing: a pulse is taken only for its own round,
// verified against quicknet's key, with any randomness it states the
// signature's sha256; a verified pulse is kept and never read again.
func TestSettlementV2CtfDrandPulse(t *testing.T) {
	t.Parallel()
	answer := fmt.Sprintf(`{"round":1000,"randomness":"fe290beca10872ef2fb164d2aa4442de4566183ec51c56ff3cd603d930e54fdd","signature":"%s"}`, settlementV2CtfTestRound1000)
	var served atomic.Value
	served.Store(answer)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/"+crv4.QuicknetChainHash+"/public/1000" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(served.Load().(string)))
	}))
	t.Cleanup(server.Close)
	reader := &settlementV2CtfChain{drand: []string{server.URL + "/"}, client: server.Client()}
	pulse, err := reader.pulse(t.Context(), 1000)
	if err != nil || pulse.Round != 1000 || hex.EncodeToString(pulse.Signature) != settlementV2CtfTestRound1000 {
		t.Fatalf("round 1000: %v", err)
	}
	served.Store("{}")
	if again, err := reader.pulse(t.Context(), 1000); err != nil || again != pulse || requests.Load() != 1 {
		t.Fatalf("a verified pulse was read again: %v", err)
	}
	for name, body := range map[string]string{
		"another round":      strings.Replace(answer, `"round":1000`, `"round":999`, 1),
		"a flipped bit":      strings.Replace(answer, `"signature":"b4`, `"signature":"b5`, 1),
		"another randomness": strings.Replace(answer, `"randomness":"fe`, `"randomness":"ff`, 1),
		"not hex":            strings.Replace(answer, `"signature":"b4`, `"signature":"zz`, 1),
		"not json":           "pulse",
	} {
		served.Store(body)
		if _, err := settlementV2DrandPulse(t.Context(), server.Client(), server.URL, 1000); err == nil {
			t.Fatalf("%s was taken", name)
		}
	}
	if _, err := reader.pulse(t.Context(), 1001); err == nil || !strings.Contains(err.Error(), "no drand relay served a verified pulse of round 1001") {
		t.Fatalf("an unserved round: %v", err)
	}
}

// A CTF root the audit cannot rebuild is demanded as the provider pool's is
// (VERSION2-FINAL.md §9.3): with epoch 5's root approved and finalized, a
// pending root over it whose objects do not reproduce its commitment is
// demanded at once, every account of the finalized root with its proof, read
// back from custody through the CTF header.
func TestSettlementV2CtfAuditDemandsUnreproducibleRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	scenario := newSettlementV2CtfTestScenario(t)
	reader := settlementV2CtfTestFacts(scenario)
	pool := fixture.useCtf(t, scenario, reader)
	if err := fixture.audit.audit(t.Context(), pool); err != nil || fixture.ctfRecord(t, 1).Decision != settlementV2DecisionApprove {
		t.Fatalf("epoch 5's root: %v", err)
	}
	first := scenario.built
	funded := map[uint64]uint256.Int{5: scenario.funded[5], 6: *new(uint256.Int).Add(ptrUint256(scenario.funded[5]), uint256.NewInt(119_583_893_572))}
	next, err := ctf.Build(t.Context(), ctf.BuildInput{NoID: settlementV2AuditTestNoID, ChainID: settlementV2TestChainID, Netuid: 25, Epoch: 6, FirstEpoch: 5, CarryColdkey: scenario.carry,
		Previous: &ctf.Previous{Root: first.Commit, Accounts: first.Accounts, State: first.State}, Epochs: []ctf.EpochInput{{Epoch: 6}}, FundedThrough: funded})
	if err != nil {
		t.Fatal(err)
	}
	commit := next.Commit
	commit.Root[0] ^= 1
	fixture.objects.publish(next.Objects)
	fixture.chain.lock.Lock()
	fixture.chain.funded[settlementV2CtfTestPoolID][6] = ptrUint256(funded[6]).ToBig()
	fixture.chain.lock.Unlock()
	fixture.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "DistributionFinalized", 660, big.NewInt(settlementV2CtfTestPoolID), uint64(1), big.NewInt(0), first.Commit.Epoch, first.Commit.Root, first.Commit.Total.ToBig(), first.Commit.LeafCount, [32]byte(first.Commit.LedgerHash))
	fixture.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "DistributionCommitted", 705, big.NewInt(settlementV2CtfTestPoolID), uint64(2), commit.Epoch, commit.Root, commit.Total.ToBig(), commit.LeafCount, [32]byte(commit.LedgerHash), uint64(0))
	fixture.chain.advanceTo(709)
	fixture.chain.mutate(func(state *settlementV2TestState) {
		state.roots[settlementV2CtfTestPoolID] = []stabi.ISTSettlementVault2Root{settlementV2AuditTestFinalized(first.Commit)}
		state.pending[settlementV2CtfTestPoolID] = settlementV2AuditTestPending(2, commit, 705)
	})
	reader.in.Epochs = map[uint64]ctf.EpochFacts{6: {}}
	sender := &settlementV2AuditTestSender{}
	fixture.audit.sender = sender
	if err := fixture.audit.audit(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	record := fixture.ctfRecord(t, 2)
	if record.Decision != settlementV2DecisionDemand || record.Check != "tree" || record.RootIndex != 0 || !record.Done || record.Outcome != "sent" {
		t.Fatalf("record %+v", record)
	}
	requireSettlementV2AuditDemands(t, sender, &ledger.Ledger{Accounts: first.Accounts, Tree: first.Tree}, 0, settlementV2AuditTestWhole(t, first.Accounts))
}

func ptrUint256(value uint256.Int) *uint256.Int {
	return &value
}

// ctfCensus's answer for one epoch.
type stabiCtfCensus struct {
	hash  [32]byte
	block uint64
}

// The resolver fixture's carry contract: what it answers.
type releaseCtfTestCarry struct {
	address  common.Address
	vault    common.Address
	poolID   uint64
	coldkey  [32]byte
	recycled map[uint64]uint64
}

// The production reader takes, at the audit's pinned block, the entry in
// force at the pending root's epoch, the carry contract it names (which must
// serve vault2's CTF pool), its coldkey and returns, and from E_c each epoch's
// committed census, the timestamp of its block and the pulse of its round
// from the relay. An epoch before E_c has no draw, whatever was committed; no
// entry in force, two carry contracts or a carry of another pool stop it.
func TestSettlementV2CtfChainReadsTheAuditFacts(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	chainID, netuid := fixture.chain.chainId.Uint64(), fixture.query.domain.Netuid
	carry := common.HexToAddress("0x00000000000000000000000000000000000Ca77F")
	block := fixture.query.boundary.EVMBlock
	fixture.carry = releaseCtfTestCarry{address: carry, vault: fixture.addresses.Vault, poolID: 1<<32 + 1, coldkey: [32]byte{0xc0, 0x1d}, recycled: map[uint64]uint64{5: 7, 6: 11}}
	census := ledger.RefOf([]byte("epoch 6 census"))
	fixture.ctfCensuses = map[uint64]stabiCtfCensus{5: {hash: [32]byte{0x55}, block: block}, 6: {hash: census, block: block}}
	fixture.times = map[uint64]uint64{block: settlementV2CtfTestTime}
	relay := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/"+crv4.QuicknetChainHash+"/public/1000" {
			http.NotFound(writer, request)
			return
		}
		_, _ = fmt.Fprintf(writer, `{"round":1000,"signature":"%s"}`, settlementV2CtfTestRound1000)
	}))
	t.Cleanup(relay.Close)
	file := func(entries ...*amendment.Entry) *amendment.File {
		t.Helper()
		raw := fmt.Sprintf("sn: %d\nsigned_amendments:\n", netuid)
		for _, entry := range entries {
			if err := entry.Sign(depositPriceTestKey); err != nil {
				t.Fatal(err)
			}
			line, err := entry.Line()
			if err != nil {
				t.Fatal(err)
			}
			raw += fmt.Sprintf("  - line: %q\n    signature: \"0x%x\"\n", line, entry.Signature)
		}
		parsed, err := amendment.ParseVerified([]byte(raw), chainID, netuid, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	reader := &settlementV2CtfChain{chain: fixture.chain, settlement: fixture.addresses.Settlement, vault: fixture.addresses.Vault, chainID: chainID, netuid: netuid,
		amendments: &releaseAmendments{file: file(amendment.NewCaptureTheFlag(chainID, netuid, 1, 6, carry))}, drand: []string{relay.URL}, client: relay.Client()}
	state := &settlementV2PoolState{noID: 1<<32 + 1, block: block, blockHash: fixture.blocks[block], registered: true, firstEpoch: 5, pending: stabi.ISTSettlementVault2Pending{Seq: 1, Epoch: 6}}
	in, err := reader.ctfInput(t.Context(), state, 1)
	if err != nil {
		t.Fatal(err)
	}
	if in.NoID != 1 || in.ChainID != chainID || in.Netuid != netuid || in.FirstEpoch != 5 || in.CarryColdkey != fixture.carry.coldkey || len(in.Epochs) != 2 {
		t.Fatalf("input %+v", in)
	}
	five, six := in.Epochs[5], in.Epochs[6]
	if five.Census == nil || five.Census.Hash != ([32]byte{0x55}) || five.Pulse == nil || five.Pulse.Round != 1000 || five.Recycled.Uint64() != 7 {
		t.Fatalf("epoch 5, before E_c, is not drawn as committed: %+v", five)
	}
	if six.Census == nil || six.Census.Hash != census || six.Census.Block != block || six.Census.Time != settlementV2CtfTestTime || six.Pulse == nil || six.Pulse.Round != 1000 || six.Recycled.Uint64() != 11 {
		t.Fatalf("epoch 6: %+v", six)
	}
	for name, test := range map[string]struct {
		change func()
		want   string
	}{
		"no entry in force": {func() {
			reader.amendments = &releaseAmendments{file: file(amendment.NewCaptureTheFlag(chainID, netuid, 1, 7, carry))}
		}, "not in force"},
		"unknown amendments": {func() { reader.amendments = &releaseAmendments{} }, "not in force"},
		"two carry contracts": {func() {
			earlier := common.HexToAddress("0x00000000000000000000000000000000000Ca770")
			reader.amendments = &releaseAmendments{file: file(amendment.NewCaptureTheFlag(chainID, netuid, 1, 5, earlier), amendment.NewCaptureTheFlag(chainID, netuid, 1, 6, carry))}
		}, "two"},
		"a carry of another vault": {func() { fixture.carry.vault = fixture.addresses.Settlement }, "serves vault"},
		"a carry of another pool":  {func() { fixture.carry.poolID++ }, "serves vault"},
		"a pulse no relay serves":  {func() { reader.drand = []string{relay.URL + "/missing"} }, "no drand relay served a verified pulse of round 1000"},
	} {
		saved, savedCarry, savedDrand := reader.amendments, fixture.carry, reader.drand
		test.change()
		reader.pulses = sync.Map{}
		if _, err := reader.ctfInput(t.Context(), state, 1); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: %v", name, err)
		}
		reader.amendments, fixture.carry, reader.drand = saved, savedCarry, savedDrand
	}
}

// The views the validator calls are STSettlement2Ctf's and STCtfCarry's, from
// the contracts' bindings, at the selectors of the contracts' interface.
func TestCaptureTheFlagViewSelectors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		metadata *bind.MetaData
		method   string
		selector string
	}{
		{&stabi.STSettlement2MetaData, "ctfSettlementAt", "076882a7"},
		{&stabi.STSettlement2MetaData, "ctfCensus", "4396736d"},
		{&stabi.STCtfCarryMetaData, "recycledFor", "02ed4e54"},
		{&stabi.STCtfCarryMetaData, "coldkey", "6d9f69f2"},
		{&stabi.STCtfCarryMetaData, "vault", hex.EncodeToString(crypto.Keccak256([]byte("vault()"))[:4])},
		{&stabi.STCtfCarryMetaData, "poolId", hex.EncodeToString(crypto.Keccak256([]byte("poolId()"))[:4])},
	} {
		parsed, err := test.metadata.ParseABI()
		if err != nil {
			t.Fatal(err)
		}
		if got := hex.EncodeToString(parsed.Methods[test.method].ID); got != test.selector {
			t.Fatalf("%s selector 0x%s, want 0x%s", test.method, got, test.selector)
		}
	}
}

// SettlementV2CtfAuditInput refuses, before any read, an operator that has no
// capture the flag pool, no amendments file, and a deployment without both
// settlement v2 addresses. E-8 (mainnet/settlement_v2_e2e_ctf_test.go) reads
// a pending CTF root's input through it on the e2e chain.
func TestSettlementV2CtfAuditInputRefusesItsArguments(t *testing.T) {
	t.Parallel()
	file, err := amendment.Unpublished(945, 25)
	if err != nil {
		t.Fatal(err)
	}
	addresses := SettlementV2Addresses{Settlement: settlementV2TestSettlement, Vault: common.HexToAddress("0x7a20000000000000000000000000000000000002")}
	for _, c := range []struct {
		name      string
		addresses SettlementV2Addresses
		file      *amendment.File
		noID      uint64
	}{
		{"operator 0", addresses, file, 0},
		{"an operator from 2^32", addresses, file, 1 << 32},
		{"no amendments file", addresses, nil, 1},
		{"no vault2", SettlementV2Addresses{Settlement: addresses.Settlement}, file, 1},
		{"no STSettlement2", SettlementV2Addresses{Vault: addresses.Vault}, file, 1},
	} {
		if _, err := SettlementV2CtfAuditInput(t.Context(), nil, c.addresses, 945, 25, 0, c.file, "", c.noID); err == nil || !strings.Contains(err.Error(), "a capture the flag audit input needs") {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}
