//go:build linux || darwin

// Retaining signed operator history does not create a native transaction.
// Retry the real binding census under its exact snapshot after that restart.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/crv4"
)

// Startup has authenticated an empty native store alongside retained operator
// history. Strict domains and any published native intent keep their guards.
func TestRetainedProvisionalPreparationRequiresEmptyNativeStore(t *testing.T) {
	cfg := &ReleaseConfig{ChainID: 945, GenesisHash: provisionalRuntimeTestnetGenesis, StateDir: newAttemptSettlementRuntimeV2TestStateDir(t), ProvisionalRuntimeCompatibility: crv4.ProvisionalRuntimeCompatibilityProfile}
	cfg.Policy.NetworkProfile = "testnet"
	cfg.EvidenceV2.Bounds = releaseEvidenceV2HistoryReadTestBounds()
	cfg.EvidenceV2.Bounds.MaxControlBytes = 4096
	history := &releaseEvidenceV2StartupHistory{retainedStartup: true}
	store, err := newReleaseIntentStoreV2(&releaseRuntimeV2{ctx: t.Context(), cfg: *cfg, history: history})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.currentV2(t.Context())
	if err != nil || current != nil {
		t.Fatalf("retained operator history manufactured a native intent: %+v %v", current, err)
	}
	if !provisionalFreshNativePreparationEnabled(cfg, history, current) || !provisionalNativeWeightRejectionEnabled(cfg, history, current) {
		t.Fatal("restart revoked pre-intent read or empty-weight retry permission")
	}
	for _, status := range []string{"pending", "finalized", "applied", "failed"} {
		current := &SteeringIntent{Status: status}
		if provisionalFreshNativePreparationEnabled(cfg, history, current) || provisionalNativeWeightRejectionEnabled(cfg, history, current) {
			t.Fatalf("%s native intent regained pre-intent permission", status)
		}
	}
	for _, fault := range []string{"strict", "genesis", "adoption", "history"} {
		changedCfg, changedHistory := *cfg, *history
		candidate := &changedHistory
		switch fault {
		case "strict":
			changedCfg.ProvisionalRuntimeCompatibility = ""
		case "genesis":
			changedCfg.GenesisHash = releaseHex32([32]byte{0x54})
		case "adoption":
			changedHistory.historyAdoption = &releaseHistoryAdoptionV2{}
		case "history":
			candidate = nil
		}
		if provisionalFreshNativePreparationEnabled(&changedCfg, candidate, nil) {
			t.Fatalf("%s acquired pre-intent authority", fault)
		}
	}
}

// Inject a typed transport deadline at bindingAt's real Http boundary. Every
// retry retains the exact canonical Evm hash; no client-key or stream replay
// proceeds on a failed census, and the final successful body is fully checked.
func TestRetainedProvisionalPreparationRetriesPinnedBindingCensus(t *testing.T) {
	testReleaseSteeringPinnedBindingCensus(t, true)
}

// A retained strict owner gets the same bounded census retry without receiving
// pre-intent deferral authority. All canonical observations remain required.
func TestReleaseSteeringTransportV2RetriesPinnedBindingCensus(t *testing.T) {
	testReleaseSteeringPinnedBindingCensus(t, false)
}

// Interrupt the actual census post twelve times before recovering its pinned
// result. Both scopes replay the complete collector and retain no partial head.
func testReleaseSteeringPinnedBindingCensus(t *testing.T, fresh bool) {
	t.Helper()
	fixture := newReleaseHeadV2TestFixture(t, 1)
	server := httptest.NewServer(fixture.rpc)
	t.Cleanup(server.Close)
	var interrupted atomic.Int32
	selector := fixture.steerer.chain.coordinator.PackBindingAt([16]byte{}, big.NewInt(1))[:4]
	transport := attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.GetBody == nil {
			return nil, errors.New("test binding request cannot be inspected")
		}
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		encoded, readErr := io.ReadAll(body)
		if err := errors.Join(readErr, body.Close()); err != nil {
			return nil, err
		}
		if raw := bytes.TrimSpace(encoded); len(raw) != 0 && raw[0] == '[' {
			var calls []chainBatchRPCRequest
			if err := json.Unmarshal(raw, &calls); err != nil {
				return nil, err
			}
			for _, call := range calls {
				if call.Method != "eth_call" || len(call.Params) != 2 {
					continue
				}
				var payload struct {
					Data  hexutil.Bytes
					Input hexutil.Bytes
				}
				if err := json.Unmarshal(call.Params[0], &payload); err != nil {
					return nil, err
				}
				data := payload.Data
				if len(data) == 0 {
					data = payload.Input
				}
				if len(data) < 4 || !bytes.Equal(data[:4], selector) {
					continue
				}
				var block gethrpc.BlockNumberOrHash
				if json.Unmarshal(call.Params[1], &block) != nil || block.BlockHash == nil || *block.BlockHash != common.HexToHash(fixture.measurement.artifact.EVMSnapshotHash) || !block.RequireCanonical {
					return nil, errors.New("retry replaced the original canonical binding snapshot")
				}
				if interrupted.Add(1) <= releaseSteeringFailureLimit+2 {
					return nil, context.DeadlineExceeded
				}
				break
			}
		}
		return http.DefaultTransport.RoundTrip(request)
	})
	client, err := gethrpc.DialOptions(t.Context(), server.URL, gethrpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	fixture.steerer.chain.client = ethclient.NewClient(client)
	cfg := &ReleaseConfig{ChainID: 945, GenesisHash: provisionalRuntimeTestnetGenesis, StateDir: newAttemptSettlementRuntimeV2TestStateDir(t), ProvisionalRuntimeCompatibility: crv4.ProvisionalRuntimeCompatibilityProfile}
	cfg.Policy.NetworkProfile = "testnet"
	history := &releaseEvidenceV2StartupHistory{retainedStartup: true}
	allow := fresh && provisionalFreshNativePreparationEnabled(cfg, history, nil)
	attempts := 0
	var result releaseHeadResult
	err = runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return fixture.measurement.artifact.SubnetEpoch, nil }, func() error {
		attempts++
		var err error
		result, err = fixture.gather(t.Context(), fixture.options(t))
		if attempts <= releaseSteeringFailureLimit+2 {
			if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "compact live head binding census") || !reflect.DeepEqual(result, releaseHeadResult{}) {
				t.Fatalf("binding interruption leaked results or lost provenance: %v", err)
			}
			if reads, _ := fixture.rpc.bindingCounts(); reads != 0 {
				t.Fatal("interrupted census published an observed binding")
			}
		}
		return classifyProvisionalNativeRead(allow, fixture.measurement.artifact.SubnetEpoch, err)
	}, func() bool { return attempts < releaseSteeringFailureLimit+3 }, false, allow)
	if err != nil || attempts != releaseSteeringFailureLimit+3 || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) || len(result.Bindings) == 0 {
		t.Fatalf("retained pre-intent census did not recover: attempts=%d err=%v", attempts, err)
	}
	fixture.assertNoEMACommit(t)
}

// A native epoch boundary may pass while an unsubmitted cut read retries.
// A mixed integrity failure must still survive that boundary and stop steering.
func TestRetainedProvisionalPreparationKeepsEpochAndIntegrityBoundaries(t *testing.T) {
	cfg := &ReleaseConfig{ChainID: 945, GenesisHash: provisionalRuntimeTestnetGenesis, StateDir: newAttemptSettlementRuntimeV2TestStateDir(t), ProvisionalRuntimeCompatibility: crv4.ProvisionalRuntimeCompatibilityProfile}
	cfg.Policy.NetworkProfile = "testnet"
	history := &releaseEvidenceV2StartupHistory{retainedStartup: true}
	allow := provisionalFreshNativePreparationEnabled(cfg, history, nil)
	broken := errors.New("retained signed input hash differs")
	for _, mixed := range []bool{false, true} {
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
			reads++
			return uint64(20 + reads), nil
		}, func() error {
			attempts++
			if attempts > 1 {
				return nil
			}
			var cause error = context.DeadlineExceeded
			if mixed {
				cause = errors.Join(cause, broken)
			}
			return classifyProvisionalNativeRead(allow, 21, cause)
		}, func() bool { return reads < 2 }, false, allow)
		if mixed {
			if !errors.Is(err, broken) || !strings.Contains(err.Error(), "incomplete epoch") || attempts != 1 {
				t.Fatalf("mixed corruption advanced a native epoch: attempts=%d err=%v", attempts, err)
			}
		} else if err != nil || attempts != 2 {
			t.Fatalf("pre-intent timeout consumed a restart at the next epoch: attempts=%d err=%v", attempts, err)
		}
	}
}
