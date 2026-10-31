//go:build linux || darwin

// The settlement v2 audit's failures: a pass that cannot read what it needs
// from chain, the operator or custody returns its error, decides nothing,
// and is retried at the next pass; the audit loop reports it and goes on.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/stabi"
)

// requireUndecided requires that pool 1's seq has no decision yet.
func (self *settlementV2AuditTestFixture) requireUndecided(t *testing.T, seq uint64) {
	t.Helper()
	var record settlementV2AuditRecord
	if ok, err := self.audit.custody.readJSON(self.audit.custody.path("audits", fmt.Sprintf("1-%d.json", seq)), &record); err != nil || ok && record.Decision != "" {
		t.Fatalf("a failed pass decided %q: %v", record.Decision, err)
	}
}

// VA-2: each read of an honest root's audit failing fails the pass without a
// decision; once the read recovers, the next pass approves.
func TestSettlementV2AuditReadFailuresRetry(t *testing.T) {
	t.Parallel()
	for _, read := range []string{"eth_getBlockByNumber:finalized", "pools", "pending", "CHALLENGE_BLOCKS", "rootCount", "roots", "fundedThrough",
		"eth_getLogs", "epochStartBlock", "epochEndBlock", "eth_getBlockByNumber", "bindingAt"} {
		t.Run(read, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			f := settlementV2AuditTestF(t)
			p := settlementV2AuditTestP(t, f)
			fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
			fixture.chain.lock.Lock()
			fixture.chain.failures[read] = fixture.chain.counts[read] + 1
			fixture.chain.lock.Unlock()
			if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "synthetic "+read+" failure") {
				t.Fatalf("pass with %s failing: %v", read, err)
			}
			fixture.requireUndecided(t, 2)
			fixture.chain.lock.Lock()
			delete(fixture.chain.failures, read)
			fixture.chain.lock.Unlock()
			fixture.mustPass(t)
			fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
		})
	}
}

// VA-2: a pool vault2 has not registered, or without a pending root, is not
// audited and leaves no record.
func TestSettlementV2AuditSkipsPoolsWithoutPendingRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	fixture.setPool(stabi.ISTSettlementVault2Pending{Total: new(big.Int)})
	fixture.mustPass(t)
	fixture.chain.lock.Lock()
	fixture.chain.pools[settlementV2AuditTestNoID] = stabi.ISTSettlementVault2Pool{}
	fixture.chain.lock.Unlock()
	fixture.mustPass(t)
	if entries, err := os.ReadDir(fixture.audit.custody.path("audits", "")); !errors.Is(err, os.ErrNotExist) || len(entries) != 0 || fixture.chain.count("pending") != 1 {
		t.Fatalf("records %v, %d pending reads: %v", entries, fixture.chain.count("pending"), err)
	}
}

// VA-4: a demand that cannot read its history, finds no finalization of the
// previous root, decodes a foreign log or gets a log outside its query, or
// cannot draw its randomness decides nothing.
func TestSettlementV2AuditDemandFailuresRetry(t *testing.T) {
	t.Parallel()
	demanded := settlementV2VaultABI.Events["PathDemanded"].ID
	for name, c := range map[string]struct {
		setup func(*settlementV2AuditTestFixture)
		want  string
	}{
		"history unreadable": {func(fixture *settlementV2AuditTestFixture) { fixture.chain.failures["eth_getLogs"] = 1 }, "synthetic eth_getLogs failure"},
		"no finalization": {func(fixture *settlementV2AuditTestFixture) {
			fixture.chain.served = func(logs []types.Log) []types.Log { return nil }
		}, "vault2 events up to block 605 hold no DistributionFinalized of root 0 of pool 1"},
		"foreign log": {func(fixture *settlementV2AuditTestFixture) {
			fixture.chain.served = func(logs []types.Log) []types.Log {
				return append(logs, types.Log{Address: settlementV2TestVault, Topics: []common.Hash{{0x01}}, BlockNumber: 605})
			}
		}, "not an event of the demand history"},
		"malformed log": {func(fixture *settlementV2AuditTestFixture) {
			fixture.chain.served = func(logs []types.Log) []types.Log {
				return append(logs, types.Log{Address: settlementV2TestVault, Topics: []common.Hash{demanded}, BlockNumber: 605})
			}
		}, "vault2 log 0 of block 605"},
		"log of another contract": {func(fixture *settlementV2AuditTestFixture) {
			fixture.chain.served = func(logs []types.Log) []types.Log {
				return append(logs, types.Log{Address: settlementV2TestSettlement, Topics: []common.Hash{demanded}, BlockNumber: 605})
			}
		}, "returned log 2 outside the query"},
		"randomness": {func(fixture *settlementV2AuditTestFixture) { fixture.audit.random = bytes.NewReader(nil) }, "demand sample randomness"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2AuditTestFixture(t)
			f := settlementV2AuditTestF(t)
			p := settlementV2AuditTestP(t, f)
			fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
			fixture.chain.lock.Lock()
			c.setup(fixture)
			fixture.chain.lock.Unlock()
			if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("pass: %v; want %q", err, c.want)
			}
			fixture.requireUndecided(t, 2)
			if len(fixture.chain.accepted()) != 0 {
				t.Fatal("a failed decision sent demands")
			}
		})
	}
}

// VA-2: a FleetBound log that does not decode, or a scan checkpoint that is
// no longer canonical or cannot be read, fails the head bindings.
func TestSettlementV2AuditFleetScanFailures(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
	fixture.chain.lock.Lock()
	fixture.chain.served = func(logs []types.Log) []types.Log {
		return append(logs, types.Log{Address: settlementV2TestCoordinator, Topics: []common.Hash{settlementV2CoordinatorABI.Events["FleetBound"].ID}, BlockNumber: 190})
	}
	fixture.chain.lock.Unlock()
	if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "FleetBound log") {
		t.Fatalf("a malformed FleetBound log: %v", err)
	}
	fixture.requireUndecided(t, 2)

	reader := fixture.audit.reader.(*settlementV2AuditChain)
	index := settlementV2FleetIndex{scannedThrough: 600, scannedHash: [32]byte{0x01}}
	if err := index.extend(t.Context(), reader.chain, 1, 610, settlementV2TestBlockHash(610)); err == nil || !strings.Contains(err.Error(), "no longer canonical") {
		t.Fatalf("a moved checkpoint: %v", err)
	}
	fixture.chain.lock.Lock()
	fixture.chain.failures["eth_getBlockByNumber"] = fixture.chain.counts["eth_getBlockByNumber"] + 1
	fixture.chain.lock.Unlock()
	if err := index.extend(t.Context(), reader.chain, 1, 610, settlementV2TestBlockHash(610)); err == nil {
		t.Fatal("an unreadable checkpoint extended the scan")
	}
}

// VA-2: head bindings are read only for epochs closed at the pinned block.
func TestSettlementV2AuditHeadBindingsNeedClosedEpoch(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	fixture.chain.advanceTo(450)
	oracle := fixture.audit.reader.headOracle(&settlementV2PoolState{block: 450, blockHash: settlementV2TestBlockHash(450)})
	if _, err := oracle.HeadBindings(t.Context(), 4); err == nil || !strings.Contains(err.Error(), "epoch 4 boundaries [400,500] are not closed at finalized block 450") {
		t.Fatalf("an open epoch: %v", err)
	}
	if bound, err := oracle.HeadBindings(t.Context(), 9); err != nil || len(bound) != 0 {
		t.Fatalf("an epoch without candidates: %v %v", bound, err)
	}
}

// VA-2: objects are read from /sn/ledger/object on the operator's API host,
// as /sn/artifact is, whatever path its api_url names. An endpoint that is
// unreachable or answers other than 200 leaves the object missing; a
// malformed endpoint is refused at startup.
func TestSettlementV2AuditObjectEndpoint(t *testing.T) {
	t.Parallel()
	if _, err := newSettlementV2Objects("http://[::1"); err == nil {
		t.Fatal("a malformed api_url was accepted")
	}
	objects := newSettlementV2AuditTestObjects(t)
	fetcher, err := newSettlementV2Objects(objects.url + "/api/")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("object")
	objects.publish(ledger.Objects{ledger.RefOf(data): data})
	body, err := fetcher.Fetch(t.Context(), ledger.RefOf(data))
	if err != nil {
		t.Fatal(err)
	}
	read := make([]byte, 16)
	n, _ := body.Read(read)
	body.Close()
	if !bytes.Equal(read[:n], data) {
		t.Fatalf("fetched %q", read[:n])
	}
	if _, err := fetcher.Fetch(t.Context(), ledger.RefOf([]byte("absent"))); err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("an absent object: %v", err)
	}
	// A redirect never moves the audit to another origin.
	redirect := httptest.NewServer(http.RedirectHandler(objects.url+"/sn/ledger/object?hash="+ledger.RefOf(data).String(), http.StatusFound))
	t.Cleanup(redirect.Close)
	moved, err := newSettlementV2Objects(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := moved.Fetch(t.Context(), ledger.RefOf(data)); err == nil || !strings.Contains(err.Error(), "status 302") {
		t.Fatalf("a redirected object: %v", err)
	}
	if _, err := fetcher.Fetch(nil, ledger.RefOf(data)); err == nil {
		t.Fatal("a nil context fetched")
	}
	unreachable, err := newSettlementV2Objects("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unreachable.Fetch(t.Context(), ledger.RefOf(data)); err == nil {
		t.Fatal("an unreachable endpoint fetched")
	}
}

// VA-7: custody that cannot be read or written fails the pass: a record
// that does not decode, an object that no longer matches its reference, and
// a record that cannot be kept, before or after sending.
func TestSettlementV2AuditCustodyFailures(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*settlementV2AuditTestFixture, *ledger.Ledger, *ledger.Ledger) {
		fixture := newSettlementV2AuditTestFixture(t)
		f := settlementV2AuditTestF(t)
		p := settlementV2AuditTestP(t, f)
		fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
		return fixture, f, p
	}
	t.Run("record", func(t *testing.T) {
		fixture, _, _ := setup(t)
		if err := atomicStateWrite(fixture.audit.custody.path("audits", "1-2.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := fixture.pass(t); err == nil {
			t.Fatal("a corrupt record was audited")
		}
	})
	t.Run("object", func(t *testing.T) {
		fixture, f, _ := setup(t)
		if err := os.WriteFile(fixture.audit.custody.path("objects", strings.TrimPrefix(ledger.Ref(f.Commit.LedgerHash).String(), "sha256:")), []byte("tampered"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "does not match its reference") {
			t.Fatalf("a corrupt custody object: %v", err)
		}
	})
	// Custody's root becomes read-only: objects and rows still go to their
	// existing directories, but no record directory can be made.
	readOnlyRoot := func(t *testing.T, fixture *settlementV2AuditTestFixture) {
		if err := os.Chmod(fixture.audit.custody.directory, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(fixture.audit.custody.directory, 0o700) })
	}
	t.Run("decision", func(t *testing.T) {
		fixture, _, p := setup(t)
		if err := fixture.audit.custody.KeepValidRows(t.Context(), settlementV2AuditTestNoID, 9, nil); err != nil {
			t.Fatal(err)
		}
		for ref, data := range p.Objects {
			if err := fixture.audit.custody.Keep(t.Context(), ref, data); err != nil {
				t.Fatal(err)
			}
		}
		readOnlyRoot(t, fixture)
		if err := fixture.pass(t); err == nil || len(fixture.chain.accepted()) != 0 {
			t.Fatalf("an unrecorded decision was sent: %v", err)
		}
	})
	t.Run("waiting", func(t *testing.T) {
		fixture := newSettlementV2AuditTestFixture(t)
		f := settlementV2AuditTestF(t)
		p := settlementV2AuditTestP(t, f)
		fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 610)
		readOnlyRoot(t, fixture)
		if err := fixture.pass(t); err == nil {
			t.Fatal("an unrecorded wait passed")
		}
	})
	t.Run("outcome", func(t *testing.T) {
		fixture := newSettlementV2AuditTestFixture(t)
		f := settlementV2AuditTestF(t)
		p := settlementV2AuditTestP(t, f)
		fixture.useScenario(t, f, p.Commit, ledger.Objects{}, true, 630)
		sender := &settlementV2AuditTestSender{}
		sender.before = func() {
			if len(sender.demands) == 0 {
				settlementV2CustodyTestBlock(t, fixture.audit.custody.path("audits", ""))
			}
		}
		fixture.audit.sender = sender
		if err := fixture.pass(t); err == nil || len(sender.demands) != len(f.Accounts) {
			t.Fatalf("an unrecorded outcome passed: %v", err)
		}
	})
}

// VA-4: a decided challenge or demand whose trees cannot be rebuilt again
// sends nothing, and a fallback whose older root cannot be read fails.
func TestSettlementV2AuditDecisionNeedsItsTrees(t *testing.T) {
	t.Parallel()
	t.Run("challenge", func(t *testing.T) {
		fixture := newSettlementV2AuditTestFixture(t)
		f := settlementV2AuditTestF(t)
		p := settlementV2AuditTestP(t, f)
		pub := settlementV2AuditTestPublish(t, p)
		accounts := append([]merkle.LedgerAccount(nil), p.Accounts...)
		accounts[1].Coldkey = settlementV2AuditTestColdkey(99)
		pub.setAccounts(t, accounts)
		commit := pub.seal(t)
		fixture.useScenario(t, f, commit, pub.objects, true, 610)
		failing := &settlementV2AuditTestSender{err: errors.New("synthetic send failure")}
		fixture.audit.sender = failing
		if err := fixture.pass(t); !errors.Is(err, failing.err) {
			t.Fatalf("pass: %v", err)
		}
		chunk := pub.header.Accounts[0]
		fixture.objects.withdraw(chunk)
		if err := os.Remove(fixture.audit.custody.path("objects", strings.TrimPrefix(chunk.String(), "sha256:"))); err != nil {
			t.Fatal(err)
		}
		fixture.audit.sender = &settlementV2AuditTestSender{}
		if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "no longer rebuilds the trees of its challenge against root 0") {
			t.Fatalf("a challenge without its trees: %v", err)
		}
	})
	t.Run("challenge with a corrupt pending object", func(t *testing.T) {
		fixture := newSettlementV2AuditTestFixture(t)
		f := settlementV2AuditTestF(t)
		p := settlementV2AuditTestP(t, f)
		pub := settlementV2AuditTestPublish(t, p)
		accounts := append([]merkle.LedgerAccount(nil), p.Accounts...)
		accounts[1].Coldkey = settlementV2AuditTestColdkey(99)
		pub.setAccounts(t, accounts)
		commit := pub.seal(t)
		fixture.useScenario(t, f, commit, pub.objects, true, 610)
		failing := &settlementV2AuditTestSender{err: errors.New("synthetic send failure")}
		fixture.audit.sender = failing
		if err := fixture.pass(t); !errors.Is(err, failing.err) {
			t.Fatalf("pass: %v", err)
		}
		if err := os.WriteFile(fixture.audit.custody.path("objects", strings.TrimPrefix(ledger.Ref(commit.LedgerHash).String(), "sha256:")), []byte("tampered"), 0o600); err != nil {
			t.Fatal(err)
		}
		sender := &settlementV2AuditTestSender{}
		fixture.audit.sender = sender
		if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "does not match its reference") || len(sender.challenges) != 0 {
			t.Fatalf("a challenge over a corrupt pending object: %v", err)
		}
	})
	t.Run("older root unreadable", func(t *testing.T) {
		fixture := newSettlementV2AuditTestFixture(t)
		fixture.useOlderScenario(t, true, nil)
		fixture.chain.advanceTo(730)
		fixture.chain.lock.Lock()
		fixture.chain.failures["roots"] = fixture.chain.counts["roots"] + 2
		fixture.chain.lock.Unlock()
		if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "synthetic roots failure") {
			t.Fatalf("an unreadable older root: %v", err)
		}
		fixture.requireUndecided(t, 3)
	})
	t.Run("older root unreadable when sending", func(t *testing.T) {
		fixture := newSettlementV2AuditTestFixture(t)
		fixture.useOlderScenario(t, true, nil)
		fixture.chain.advanceTo(730)
		failing := &settlementV2AuditTestSender{err: errors.New("synthetic send failure")}
		fixture.audit.sender = failing
		if err := fixture.pass(t); !errors.Is(err, failing.err) {
			t.Fatalf("pass: %v", err)
		}
		fixture.chain.lock.Lock()
		fixture.chain.failures["roots"] = fixture.chain.counts["roots"] + 2
		fixture.chain.lock.Unlock()
		fixture.audit.sender = &settlementV2AuditTestSender{}
		if err := fixture.pass(t); err == nil || !strings.Contains(err.Error(), "synthetic roots failure") {
			t.Fatalf("an unreadable older root when sending: %v", err)
		}
	})
	t.Run("older root unreadable in custody", func(t *testing.T) {
		fixture := newSettlementV2AuditTestFixture(t)
		r, _, _ := fixture.useOlderScenario(t, true, nil)
		fixture.chain.advanceTo(730)
		path := fixture.audit.custody.path("objects", strings.TrimPrefix(ledger.Ref(r.Commit.LedgerHash).String(), "sha256:"))
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := fixture.pass(t); err == nil {
			t.Fatal("an unreadable older root in custody decided")
		}
	})
}

// settlementV2AuditTestReader fails pool 1's reads and finds no pool 2.
type settlementV2AuditTestReader struct{ seen chan uint64 }

func (self *settlementV2AuditTestReader) poolState(ctx context.Context, noID uint64) (*settlementV2PoolState, error) {
	self.seen <- noID
	if noID == 1 {
		return nil, errors.New("synthetic pool read failure")
	}
	return &settlementV2PoolState{noID: noID}, nil
}

func (self *settlementV2AuditTestReader) finalizedRoot(context.Context, *settlementV2PoolState, uint64) (stabi.ISTSettlementVault2Root, error) {
	return stabi.ISTSettlementVault2Root{}, nil
}

func (self *settlementV2AuditTestReader) headOracle(*settlementV2PoolState) ledger.HeadOracle {
	return nil
}

func (self *settlementV2AuditTestReader) demandHistory(context.Context, *settlementV2PoolState) (uint, []uint64, error) {
	return 0, nil, nil
}

func (self *settlementV2AuditTestReader) recoveries(context.Context, *settlementV2PoolState) (map[uint64][]ledger.Recovery, error) {
	return nil, nil
}

// VA-2: the audit loop reports a pool whose pass failed, goes on with the
// other pools, and ends only with its context.
func TestSettlementV2AuditRunReportsAndContinues(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	ctx, owner, err := newReleaseDiagnostics(t.Context(), &output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	reader := &settlementV2AuditTestReader{seen: make(chan uint64, 4)}
	audit := newSettlementV2Audit(reader, newSettlementV2CustodyTest(t), nil, []settlementV2AuditPool{{noID: 1}, {noID: 2}}, DefaultLedgerMaxBytes)
	done := make(chan error, 1)
	go func() { done <- audit.run(ctx) }()
	if first, second := <-reader.seen, <-reader.seen; first != 1 || second != 2 {
		t.Fatalf("pools audited in order %d, %d", first, second)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run ended with %v", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if text := output.String(); !strings.Contains(text, "domain=operator code=ledger_audit_retrying") || !strings.Contains(text, "operator_id=1 operator_known=true") || strings.Contains(text, "operator_id=2") {
		t.Fatalf("diagnostics %q", text)
	}
}

// settlementV2AuditTestLines delivers each diagnostic record as written.
type settlementV2AuditTestLines chan string

func (self settlementV2AuditTestLines) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}

func (self settlementV2AuditTestLines) WriteContext(ctx context.Context, raw []byte) (int, error) {
	select {
	case self <- string(raw):
		return len(raw), nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// VA-6, VA-9: the audit runs no pass before the chain confirms the
// configured contracts. A failed confirmation is reported and reads no pool;
// once confirmed, the audit never confirms again.
func TestSettlementV2AuditConfirmsContractsFirst(t *testing.T) {
	t.Parallel()
	lines := make(settlementV2AuditTestLines, 8)
	ctx, owner, err := newReleaseDiagnostics(t.Context(), lines, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	reader := &settlementV2AuditTestReader{seen: make(chan uint64, 4)}
	audit := newSettlementV2Audit(reader, newSettlementV2CustodyTest(t), nil, []settlementV2AuditPool{{noID: 2}}, DefaultLedgerMaxBytes)
	refused := errors.New("synthetic identity mismatch")
	audit.confirm = func(context.Context) error { return refused }
	done := make(chan error, 1)
	go func() { done <- audit.run(ctx) }()
	select {
	case line := <-lines:
		if !strings.Contains(line, "domain=runtime code=ledger_audit_retrying") || !strings.Contains(line, "operator_known=false") {
			t.Fatalf("diagnostic %q", line)
		}
	case <-time.After(time.Minute):
		t.Fatal("a failed confirmation was not reported")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run ended with %v", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	select {
	case noID := <-reader.seen:
		t.Fatalf("pool %d was audited before the contracts were confirmed", noID)
	default:
	}
	calls := 0
	audit.confirm = func(context.Context) error {
		if calls++; calls == 1 {
			return refused
		}
		return nil
	}
	if err := audit.confirmed(t.Context()); !errors.Is(err, refused) || audit.confirm == nil {
		t.Fatalf("a refused confirmation: %v", err)
	}
	for range 2 {
		if err := audit.confirmed(t.Context()); err != nil || audit.confirm != nil {
			t.Fatalf("a confirmed audit: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("confirmed %d times", calls)
	}
}

// The ledger audit runs as a runtime worker beside steering and is joined on
// shutdown; a validator without the settlement_v2 block builds none.
func TestReleaseRuntimeRunsLedgerAudit(t *testing.T) {
	t.Parallel()
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	started, stopped := make(chan struct{}), make(chan struct{})
	operations := releaseShutdownTestOperations(func() { <-started; cancel() })
	operations.ledgerAudit = func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}
	if err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("the ledger audit was not joined")
	}
	audit, err := newReleaseSettlementV2Audit(t.Context(), cfg, nil, nil)
	if err != nil || audit != nil || audit.worker() != nil {
		t.Fatalf("an audit without the settlement_v2 block: %v", err)
	}
	if (&settlementV2Audit{}).worker() == nil {
		t.Fatal("an audit has no worker")
	}
}
