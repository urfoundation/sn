//go:build linux || darwin

// Real signed disk tails and canonical local Rpc views distinguish historical
// boundary authority from a locally consistent signed record or projection.
package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Both pending and complete trails occupy two older canonical boundaries.
// The current ceiling alone cannot authenticate either historical hash.
func productionBootstrapUnsealedBoundaryTestFixture(t *testing.T) (releaseArchiveV2TestFixture, *ReleaseEvidenceV2Archive, *ProductionBootstrapCommittedObservation, *ProductionBootstrapUnsealedObservation, *productionBootstrapUnsealedOwner) {
	t.Helper()
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	for i := range 2 {
		f.startup.boundary.EVMBlock++
		f.startup.boundary.EVMBlockHash = attemptHex32([32]byte{0xc1 + byte(i)})
		f.startup.blocks[f.startup.boundary.EVMBlock] = [32]byte{0xc1 + byte(i)}
		for index := range 2 {
			if i == 0 {
				f.startup.trail(t, index)
			} else {
				productionBootstrapUnsealedTestPending(t, f, index)
			}
		}
	}
	f.startup.finalized = f.startup.boundary.EVMBlock + 1
	f.startup.blocks[f.startup.finalized] = [32]byte{0xc3}
	current.EvmBlock, current.EvmHash = f.startup.finalized, attemptHex32([32]byte{0xc3})
	ownedCtx, cancel := context.WithCancel(context.Background())
	inventory, owner, err := readProductionBootstrapUnsealed(ownedCtx, f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer cancel()
		if !owner.closed {
			if err := owner.close(); err != nil {
				t.Error(err)
			}
		}
	})
	return f, archive, current, inventory, owner
}

// Completed ABI responses can contradict otherwise valid signed tail bytes.
// Counts are atomic because the real Rpc server dispatches batched views.
type productionBootstrapUnsealedBoundaryRpcFixture struct {
	*releaseStartupV2TestFixture
	fault         string
	operatorReads atomic.Uint64
}

// Every unmodified request uses the existing canonical historical fixture.
func (self *productionBootstrapUnsealedBoundaryRpcFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	raw, err := self.releaseStartupV2TestFixture.Call(ctx, call, selector)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	epoch := new(big.Int).SetUint64(self.boundary.SettlementEpoch)
	method := ""
	var value any
	switch {
	case bytes.Equal(call["input"], coordinator.PackCurrentEpoch()):
		if self.fault == "epoch" {
			method, value = "currentEpoch", new(big.Int).Add(epoch, big.NewInt(1))
		}
	case bytes.Equal(call["input"], coordinator.PackPolicyAt(epoch)):
		policy, err := coordinator.UnpackPolicyAt(raw)
		if err != nil {
			return nil, err
		}
		switch self.fault {
		case "policy":
			policy.PolicyHash[0] ^= 1
		case "window":
			policy.EffectiveBlock = self.finalized + 1
		case "trailing":
			return append(raw, make([]byte, 32)...), nil
		default:
			return raw, nil
		}
		method, value = "policyAt", policy
	default:
		for i, input := range self.inputs {
			if bytes.Equal(call["input"], coordinator.PackOperatorAt(new(big.Int).SetUint64(input.Config.NoID), epoch)) {
				self.operatorReads.Add(1)
				if i == 1 && (self.fault == "inactive" || self.fault == "future-operator") {
					operator, err := coordinator.UnpackOperatorAt(raw)
					if err != nil {
						return nil, err
					}
					if self.fault == "inactive" {
						operator.Active = false
					} else {
						operator.EffectiveEpoch = epoch.Uint64() + 1
					}
					method, value = "operatorAt", operator
				}
			}
		}
	}
	if method == "" {
		return raw, nil
	}
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	return parsed.Methods[method].Outputs.Pack(value)
}

// Transport faults are selected at the actual local Http handler after dial.
// The hooks control only request timing/failure, never a verification verdict.
func productionBootstrapUnsealedBoundaryTestChain(t *testing.T, fixture *productionBootstrapUnsealedBoundaryRpcFixture, intercept func(http.ResponseWriter, *http.Request) bool) *ChainClient {
	t.Helper()
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", fixture); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if intercept != nil && intercept(w, r) {
			return
		}
		server.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	chain, err := DialReleaseChainContext(t.Context(), []string{httpServer.URL}, common.HexToAddress(fixture.cfg.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.Close)
	chain.readRetryHooks = chainReadRetryTestHooks(chainReadTestFailureAttempts)
	return chain
}

// Both operators require independent views even when their exact blocks match.
// All nine records per operator are covered, including its unfinished trail.
func TestProductionBootstrapUnsealedBoundariesAuthenticateCompleteCensus(t *testing.T) {
	f, archive, current, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t)
	rpcFixture := &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}
	chain := productionBootstrapUnsealedBoundaryTestChain(t, rpcFixture, nil)
	before := mustArchiveV2JSONTest(t, inventory)
	got, err := owner.authenticateTailBoundaries(t.Context(), archive, inventory, chain)
	if err != nil || got == nil || rpcFixture.operatorReads.Load() != 4 {
		t.Fatal("complete historical census", got, rpcFixture.operatorReads.Load(), err)
	}
	if err := got.Validate(current.Prefixes); err != nil {
		t.Fatal(err)
	}
	for _, ledger := range got.Ledgers {
		proof := ledger.TailBoundaryProof
		if proof == nil || !proof.HistoricalSources || len(proof.Boundaries) != 2 || ledger.UnsealedRecords != 9 || ledger.PendingTrails != 1 || proof.Boundaries[0].Records != 8 || proof.Boundaries[1].Records != 1 {
			t.Fatal("boundary proof lost signed tail coverage", ledger)
		}
	}
	if !bytes.Equal(before, mustArchiveV2JSONTest(t, inventory)) {
		t.Fatal("historical observation rewrote its retained local projection")
	}
}

// An empty tail receives an explicit complete empty proof, preserving the
// distinction from an older inventory whose history was never observed.
func TestProductionBootstrapUnsealedBoundariesAuthenticateEmptyTail(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	inventory, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	}()
	rpcFixture := &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}
	chain := productionBootstrapUnsealedBoundaryTestChain(t, rpcFixture, nil)
	got, err := owner.authenticateTailBoundaries(t.Context(), archive, inventory, chain)
	if err != nil || got == nil || rpcFixture.operatorReads.Load() != 0 {
		t.Fatal("explicit empty tail", err)
	}
	for _, ledger := range got.Ledgers {
		if ledger.TailBoundaryProof == nil || !ledger.TailBoundaryProof.HistoricalSources || len(ledger.TailBoundaryProof.Boundaries) != 0 {
			t.Fatal("empty tail lacked its independently closed scope")
		}
	}
}

// The old local-only proof admits these signed tails. Canonical chain changes
// must refuse them, including a later operator after the first has completed.
func TestProductionBootstrapUnsealedBoundariesRejectHistoricalContradictions(t *testing.T) {
	for _, fault := range []string{"hash", "second-hash", "unfinalized", "epoch", "policy", "window", "inactive", "future-operator", "trailing"} {
		f, archive, _, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t)
		rpcFixture := &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup, fault: fault}
		if fault == "hash" {
			f.startup.blocks[owner.tails[0].boundaries[0].Boundary.EVMBlock] = [32]byte{0xff}
		} else if fault == "second-hash" {
			f.startup.blocks[owner.tails[0].boundaries[1].Boundary.EVMBlock] = [32]byte{0xff}
		} else if fault == "unfinalized" {
			f.startup.finalized = owner.tails[0].boundaries[0].Boundary.EVMBlock - 1
		}
		chain := productionBootstrapUnsealedBoundaryTestChain(t, rpcFixture, nil)
		got, err := owner.authenticateTailBoundaries(t.Context(), archive, inventory, chain)
		if got != nil || err == nil || retryableProductionSteeringRead(err) {
			t.Fatal("historical contradiction admitted or classified as transport", fault, err)
		}
		if (fault == "inactive" || fault == "future-operator") && rpcFixture.operatorReads.Load() != 3 {
			t.Fatal("second operator did not refuse after the complete first operator", fault, rpcFixture.operatorReads.Load())
		}
		for _, ledger := range inventory.Ledgers {
			if ledger.TailBoundaryProof != nil {
				t.Fatal("partial historical proof escaped into the original inventory", fault)
			}
		}
	}
}

// A real remote timeout exhausts its inner allowance, then the single outer
// retry resumes against the same signed replay and held source identities.
func TestProductionBootstrapUnsealedBoundariesRetryPureTransport(t *testing.T) {
	f, archive, _, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t)
	var unavailable atomic.Bool
	var failures atomic.Uint64
	chain := productionBootstrapUnsealedBoundaryTestChain(t, &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}, func(w http.ResponseWriter, _ *http.Request) bool {
		if unavailable.Load() {
			failures.Add(1)
			w.WriteHeader(http.StatusGatewayTimeout)
			return true
		}
		return false
	})
	unavailable.Store(true)
	waits := 0
	var got *ProductionBootstrapUnsealedObservation
	err := retryProductionBootstrapPrefixRead(t.Context(), func(ctx context.Context) error {
		var err error
		got, err = owner.authenticateTailBoundaries(ctx, archive, inventory, chain)
		return err
	}, releaseHttpGetRetryHooks{wait: func(ctx context.Context, _ time.Duration) error {
		waits++
		if got != nil {
			t.Fatal("remote failure returned a partial proof")
		}
		unavailable.Store(false)
		return ctx.Err()
	}})
	if err != nil || got == nil || waits != 1 || failures.Load() == 0 {
		t.Fatal("pure transport retry failed", waits, failures.Load(), err)
	}
	if got.CensusHash != inventory.CensusHash || got.SourceCount != inventory.SourceCount || got.SourceBytes != inventory.SourceBytes {
		t.Fatal("retry recaptured or changed its completed local work")
	}
}

// An actual Http timeout and source mutation reach the same attempt. The
// local custody contradiction must join the timeout before retry selection.
func TestProductionBootstrapUnsealedBoundariesRejectMixedCustodyFailure(t *testing.T) {
	f, archive, _, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t)
	path := filepath.Join(f.options.Config.Operators[0].StateDir, attemptLedgerReadyName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var armed, changed atomic.Bool
	chain := productionBootstrapUnsealedBoundaryTestChain(t, &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}, func(w http.ResponseWriter, _ *http.Request) bool {
		if armed.Load() {
			if changed.CompareAndSwap(false, true) {
				if err := os.WriteFile(path, append(bytes.Clone(raw), '\n'), 0o600); err != nil {
					t.Error(err)
				}
			}
			w.WriteHeader(http.StatusGatewayTimeout)
			return true
		}
		return false
	})
	armed.Store(true)
	waits := 0
	err = retryProductionBootstrapPrefixRead(t.Context(), func(ctx context.Context) error {
		got, err := owner.authenticateTailBoundaries(ctx, archive, inventory, chain)
		if got != nil {
			t.Fatal("mixed custody failure returned a proof")
		}
		return err
	}, releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error { waits++; return errors.New("unexpected retry") }})
	var status gethrpc.HTTPError
	if err == nil || !errors.As(err, &status) || status.StatusCode != http.StatusGatewayTimeout || !changed.Load() || waits != 0 || retryableProductionSteeringRead(err) {
		t.Fatal("mixed local and remote failure retried", waits, err)
	}
	// The already-refused owner must retain its close error for every caller.
	closeErr := owner.close()
	if closeErr == nil || owner.close() != closeErr {
		t.Fatal("close failure was discarded")
	}
}

// Source owners remain required after replay; cancellation and an already
// closed owner refuse before any historical contract request is dispatched.
func TestProductionBootstrapUnsealedBoundariesRejectAbsentOwner(t *testing.T) {
	f, archive, _, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t)
	rpcFixture := &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}
	chain := productionBootstrapUnsealedBoundaryTestChain(t, rpcFixture, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := owner.authenticateTailBoundaries(ctx, archive, inventory, chain); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled read returned historical authority", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if got, err := owner.authenticateTailBoundaries(t.Context(), archive, inventory, chain); got != nil || err == nil || rpcFixture.operatorReads.Load() != 0 {
		t.Fatal("closed source owner authorized a new historical observation", err)
	}
}

// An actual in-flight request observes caller cancellation before the owner
// returns. The exact barrier also proves no partial authority was published.
func TestProductionBootstrapUnsealedBoundariesCancellationJoinsRpc(t *testing.T) {
	f, archive, _, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t)
	var armed atomic.Bool
	entered, left := make(chan struct{}), make(chan struct{})
	chain := productionBootstrapUnsealedBoundaryTestChain(t, &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}, func(_ http.ResponseWriter, r *http.Request) bool {
		if armed.Load() {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			close(entered)
			<-r.Context().Done()
			close(left)
			return true
		}
		return false
	})
	armed.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		got, err := owner.authenticateTailBoundaries(ctx, archive, inventory, chain)
		if got != nil {
			err = errors.Join(err, errors.New("canceled historical request returned a partial proof"))
		}
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatal("request returned before cancellation barrier", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || retryableProductionSteeringRead(err) {
		t.Fatal("in-flight cancellation was retried or erased", err)
	}
	<-left
}

// Root provisions only a synthetic foreign-owned fixture. Historical reads
// consume that signed service's real retained census without repairing it.
func TestProductionBootstrapUnsealedBoundariesForeignUidReadOnly(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root-owned fixture provisioning")
	}
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	productionBootstrapUnsealedTestPending(t, f, 0)
	const uid = uint32(65534)
	paths := productionBootstrapUnsealedTestChown(t, f, uid)
	inventory, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uid, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := owner.authenticateTailBoundaries(t.Context(), archive, inventory, f.startup.chain)
	if err := errors.Join(readErr, owner.close()); err != nil || got == nil || len(got.Ledgers[0].TailBoundaryProof.Boundaries) != 1 || len(got.Ledgers[1].TailBoundaryProof.Boundaries) != 0 {
		t.Fatal("foreign-owned historical tail", err)
	}
	for _, path := range paths {
		directory, err := openAttemptPrivateDirectory(path)
		if err != nil {
			t.Fatal(err)
		}
		if directory.anchor.uid != uid || directory.anchor.mode&0o077 != 0 {
			t.Fatal("historical reader changed the signed service owner")
		}
		if err := directory.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Changed projections cannot reuse the record-derived census, even when a
// caller computes a new outer hash or keeps plausible signed-tail counters.
func TestProductionBootstrapUnsealedBoundariesRejectProjectionSubstitution(t *testing.T) {
	f, archive, _, inventory, owner := productionBootstrapUnsealedBoundaryTestFixture(t)
	rpcFixture := &productionBootstrapUnsealedBoundaryRpcFixture{releaseStartupV2TestFixture: f.startup}
	chain := productionBootstrapUnsealedBoundaryTestChain(t, rpcFixture, nil)
	changed := *inventory
	changed.Ledgers = slices.Clone(inventory.Ledgers)
	changed.Ledgers[0].Head.Root = attemptHex32([32]byte{0xff})
	if got, err := owner.authenticateTailBoundaries(t.Context(), archive, &changed, chain); got != nil || err == nil || rpcFixture.operatorReads.Load() != 0 {
		t.Fatal("substituted ledger projection reused historical work", err)
	}
}

// Exact finite admission accepts duplicates within a boundary and refuses the
// next distinct one. Conflicting hashes cannot hide behind deduplication.
func TestProductionBootstrapUnsealedBoundariesFiniteCensus(t *testing.T) {
	census := &productionBootstrapUnsealedBoundaryCensus{}
	for i := productionBootstrapUnsealedMaximumBoundaries; i > 0; i-- {
		boundary := AttemptBoundary{SettlementEpoch: 9, EVMBlock: uint64(i), EVMBlockHash: attemptHex32([32]byte{0xaa})}
		if err := census.add(boundary); err != nil {
			t.Fatal(err)
		}
	}
	members := census.sorted()
	if err := census.add(members[0].Boundary); err != nil || census.sorted()[0].Records != 2 {
		t.Fatal("duplicate boundary consumed another slot", err)
	}
	for _, boundary := range []AttemptBoundary{
		{SettlementEpoch: 9, EVMBlock: 257, EVMBlockHash: attemptHex32([32]byte{0xaa})},
		{SettlementEpoch: 9, EVMBlock: 1, EVMBlockHash: attemptHex32([32]byte{0xbb})},
	} {
		if err := census.add(boundary); err == nil {
			t.Fatal("overflow or conflicting historical boundary admitted")
		}
	}
	proof := ProductionBootstrapUnsealedBoundaryProof{Schema: ProductionBootstrapUnsealedBoundarySchema, HistoricalSources: true, Boundaries: census.sorted()}
	prefix := ProductionBootstrapOperatorPrefix{Epoch: 9}
	if err := proof.validate(prefix, 257); err != nil {
		t.Fatal("exact complete bounded census refused", err)
	}
	for _, fault := range []string{"count", "duplicate", "clock", "unobserved", "schema"} {
		changed := proof
		changed.Boundaries = slices.Clone(proof.Boundaries)
		switch fault {
		case "count":
			changed.Boundaries[0].Records--
		case "duplicate":
			changed.Boundaries[1] = changed.Boundaries[0]
		case "clock":
			changed.Boundaries[0].Boundary.SettlementEpoch++
		case "unobserved":
			changed.HistoricalSources = false
		case "schema":
			changed.Schema = "unsupported"
		}
		if err := changed.validate(prefix, 257); err == nil {
			t.Fatal("incomplete or changed reopened proof accepted", fault)
		}
	}
	if members[0].Boundary.EVMBlock != 1 || members[len(members)-1].Boundary.EVMBlock != 256 {
		t.Fatal("census lost its canonical numerical order")
	}
}
