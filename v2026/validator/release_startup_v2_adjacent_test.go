//go:build linux || darwin

// Adjacent startup controls preserve actual client transport and signed legacy
// history. They cannot replace a real replay or activation verdict.
package validator

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

type releaseStartupV2TestRoundTripper func(*http.Request) (*http.Response, error)

func (self releaseStartupV2TestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// Geth's actual HTTP client invokes this seam synchronously in the boundary
// worker. The real response body closes before Goexit, leaving no fake verdict
// or abandoned transport. A zero error slot is not completed chain observation.
func TestReleaseStartupV2RejectsLostInitialBoundaryCompletion(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, false)
	var once sync.Once
	transport := releaseStartupV2TestRoundTripper(func(request *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(request)
		if err != nil {
			return response, err
		}
		once.Do(func() {
			_ = response.Body.Close()
			runtime.Goexit()
		})
		return response, nil
	})
	client, err := gethrpc.DialOptions(t.Context(), fixture.chain.rpcUrl, gethrpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	prior := fixture.chain
	fixture.chain = &ChainClient{client: ethclient.NewClient(client), rpcUrl: prior.rpcUrl, chainId: new(big.Int).Set(prior.chainId), coordinator: prior.coordinator, contractAddr: prior.contractAddr, release: true}
	writes := 0
	physical := attemptSettlementV2PhysicalIO()
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, encoded []byte) error {
		writes++
		return writeAttemptSettlementV2OwnedState(root, name, encoded)
	}
	err = fixture.start(t.Context(), physical)
	if err == nil || !strings.Contains(err.Error(), "worker did not complete") || writes != 0 {
		t.Fatalf("lost initial-history worker activated startup: %v/%d", err, writes)
	}
	requireReleaseStartupV2Dormant(t, fixture.disk)
}

// The real legacy coordinator closes a complete positive M8 window; the real
// disk importer and bounded history replayer precede exact image admission.
// Initialization must preserve those original bytes rather than paste its EMA.
func TestReleaseStartupV2InitialImagePreservesActualPositiveLegacyHistory(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 1)
	last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options)
	if err != nil || last == nil || len(last.Transitions[0].PostFold) == 0 {
		t.Fatalf("actual signed positive legacy history: %v", err)
	}
	operators := make([]OperatorConfig, len(fixture.runtime.participants))
	for index, participant := range fixture.runtime.participants {
		operators[index] = OperatorConfig{NoID: participant.NoID, StateDir: participant.StateDir}
	}
	cfg := ReleaseConfig{StateDir: fixture.runtime.coordinator, Operators: operators, Policy: fixture.runtime.fixtures[0].policy, EvidenceV2: releaseEvidenceV2TestConfig(newAttemptSettlementRuntimeV2TestStateDir(t), operators)}
	cfg.EvidenceV2.Bounds.MaxHistoryBytes = fixture.options.MaxBytes
	history := &releaseEvidenceV2StartupHistory{cfg: cfg, initial: map[uint64]ReleaseEvidenceV2ActivationContext{}, keys: fixture.options.ServerKeys, activationHistory: last, current: map[uint64]releaseEvidenceV2StartupCursor{}}
	replicas, _ := newAttemptCutV2ReplicaTestStores(t)
	history.readers, err = newReleaseEvidenceV2StartupReaders([2]string{replicas[0].Origin, replicas[1].Origin}, cfg.EvidenceV2.Bounds.Cut)
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([][]byte, len(operators))
	present := make([]bool, len(operators))
	for index, participant := range fixture.runtime.participants {
		expected := fixture.options.Expected[index]
		history.initial[participant.NoID] = ReleaseEvidenceV2ActivationContext{InitialCut: expected}
		history.current[participant.NoID] = releaseEvidenceV2StartupCursor{epoch: expected.Boundary.SettlementEpoch, first: expected.FirstSequence, egressFirst: expected.EgressFirstSequence, generation: expected.EgressGeneration, priorRoot: expected.PriorRoot, lastSequence: expected.FirstSequence - 1, lastRoot: expected.PriorRoot, lastBoundary: expected.Boundary, prior: last.Transitions[index].PostFold}
		participant.Stats = NewStatsEngine(participant.Stats.cfg)
		history.participants = append(history.participants, participant)
		encoded[index], err = os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
		present[index] = true
	}
	history.images, err = newAttemptSettlementV2StartupImages(t.Context(), cfg.StateDir, history.participants, encoded, present, nil, false, cfg.EvidenceV2.Bounds.Persistence)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := history.preflightImages(t.Context())
	if err != nil || len(initial) != len(operators) {
		t.Fatalf("actual retained legacy image preflight: %v", err)
	}
	// An explicit omission is refused even though the same actual snapshots,
	// keys, signed ledger and independently pinned nonempty prefix remain.
	history.activationHistory = nil
	if _, err := history.preflightImages(t.Context()); err == nil || !strings.Contains(err.Error(), "retained statistics transition") {
		t.Fatalf("missing independent legacy history was accepted: %v", err)
	}
	history.activationHistory = last
	for index := range history.participants {
		initial[index].attemptLedger = history.participants[index].Ledger
		history.participants[index].Stats = initial[index]
	}
	authority, err := history.recoveryAuthority(t.Context(), "legacy-initialize")
	if err != nil {
		t.Fatal(err)
	}
	physical := attemptSettlementV2PhysicalIO()
	physical.startupImages = history.images
	if err := initializeAttemptSettlementEpochV2(t.Context(), cfg.StateDir, history.participants, initial[0].settlementEpoch, authority, cfg.EvidenceV2.Bounds.Persistence, physical); err != nil {
		t.Fatal(err)
	}
	for index, participant := range history.participants {
		snapshot := participant.Stats.snapshotStats()
		if snapshot.AttemptV2 == nil || snapshot.SettlementTransition == nil {
			t.Fatal("actual legacy initialization omitted its preserved history")
		}
		snapshot.AttemptV2, snapshot.Version = nil, 5
		actual, err := encodeStatsSnapshot(snapshot)
		if err != nil || !bytes.Equal(encoded[index], actual) {
			t.Fatalf("startup legacy initialization rewrote the original exact prior: %v", err)
		}
	}
}

// Precanceled initial-history admission itself must not construct a completion
// census that accidentally masks the original cancellation cause.
func TestReleaseStartupV2InitialBoundaryPrecancelPreservesCause(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	last, err := authenticateReleaseEvidenceV2InitialHistory(ctx, &fixture.cfg, fixture.chain, fixture.inputs, fixture.keys)
	if !errors.Is(err, context.Canceled) || last != nil {
		t.Fatalf("initial boundary lost original cancellation: %v", err)
	}
}
