//go:build linux || darwin

// The real ChainClient observes hash-pinned ABI tuples. Independently selected
// initial contexts and altered RPC bytes exercise temporal and ownership checks.
package validator

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The underlying fixture owns actual signatures and native observations;
// this endpoint adds the independent initial-boundary coordinator views.
type releaseInitialBoundaryV2TestFixture struct {
	*releaseBootstrapV2TestFixture
	chain          *ChainClient
	fault          string
	views          atomic.Uint64
	beforeOperator func(context.Context, uint64) error
}

func newReleaseInitialBoundaryV2TestFixture(t *testing.T, fault string) *releaseInitialBoundaryV2TestFixture {
	t.Helper()
	fixture := &releaseInitialBoundaryV2TestFixture{releaseBootstrapV2TestFixture: newReleaseBootstrapV2TestFixture(t), fault: fault}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", fixture); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	chain, err := DialReleaseChainContext(t.Context(), []string{httpServer.URL}, common.HexToAddress(fixture.cfg.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	fixture.chain = chain
	t.Cleanup(chain.Close)
	return fixture
}

// Both initial contexts deliberately share one finalized EVM boundary.
func (self *releaseInitialBoundaryV2TestFixture) GetBlockByNumber(ctx context.Context, block gethrpc.BlockNumber, full bool) (map[string]any, error) {
	result, err := self.releaseBootstrapV2TestFixture.GetBlockByNumber(ctx, block, full)
	if err != nil {
		return nil, err
	}
	switch self.fault {
	case "unfinalized":
		result["number"] = hexutil.EncodeUint64(self.contexts[0].InitialCut.Boundary.EVMBlock - 1)
	case "finalized-hash":
		result["hash"] = common.Hash{0xb1}
	}
	return result, nil
}

// The current epoch and policy cadence must agree with the actual pinned
// initial block. A future-scheduled activation snapshot is not that boundary.
func (self *releaseInitialBoundaryV2TestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	self.views.Add(1)
	expected := self.contexts[0]
	if selector.BlockHash == nil || *selector.BlockHash != common.Hash(expected.ObservedEVMHash) || !selector.RequireCanonical || selector.BlockNumber != nil || common.BytesToAddress(call["to"]) != common.Address(expected.Activation.Domain.Coordinator) {
		return nil, errors.New("initial fixture requires exact canonical boundary and coordinator")
	}
	coordinator := stabi.NewSTCoordinator()
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	epoch := new(big.Int).SetUint64(expected.Activation.Domain.Epoch)
	current := new(big.Int).Set(epoch)
	policy := stabi.STCoordinatorPolicySnapshot{PolicyHash: expected.Activation.Domain.PolicyHash, EffectiveEpoch: expected.Activation.Domain.Epoch, EffectiveBlock: expected.Activation.EVMBlock + 1, EpochBlocks: 500, EpochDepositCapRao: big.NewInt(1000), CampaignDepositCapRao: big.NewInt(5000)}
	operator := stabi.STCoordinatorOperatorVersion{Active: true, EffectiveEpoch: expected.Activation.Domain.Epoch}
	switch self.fault {
	case "epoch":
		current.Add(current, big.NewInt(1))
	case "policy":
		policy.PolicyHash[0] ^= 1
	case "future-policy":
		policy.EffectiveEpoch++
	case "zero-cadence":
		policy.EpochBlocks = 0
	case "after-start":
		policy.EffectiveBlock = expected.InitialCut.Boundary.EVMBlock + 1
	case "at-start":
		policy.EffectiveBlock = expected.InitialCut.Boundary.EVMBlock
	case "at-end":
		policy.EpochBlocks = expected.InitialCut.Boundary.EVMBlock - policy.EffectiveBlock
	case "overflow-cadence":
		policy.EffectiveEpoch = 0
		policy.EpochBlocks = ^uint64(0)
	case "inactive":
		operator.Active = false
	case "future-operator":
		operator.EffectiveEpoch++
	}
	var method string
	var value any
	switch {
	case bytes.Equal(call["input"], coordinator.PackCurrentEpoch()):
		method, value = "currentEpoch", current
	case bytes.Equal(call["input"], coordinator.PackPolicyAt(epoch)):
		method, value = "policyAt", policy
	default:
		for _, configured := range self.contexts {
			noID := configured.Activation.NoID
			if bytes.Equal(call["input"], coordinator.PackOperatorAt(new(big.Int).SetUint64(noID), epoch)) {
				if self.beforeOperator != nil {
					if err := self.beforeOperator(ctx, noID); err != nil {
						return nil, err
					}
				}
				method, value = "operatorAt", operator
				break
			}
		}
	}
	if method == "" {
		return nil, errors.New("initial fixture received unknown calldata")
	}
	encoded, err := parsed.Methods[method].Outputs.Pack(value)
	if err != nil {
		return nil, err
	}
	if self.fault == method+"-trailing" {
		encoded = append(encoded, 0)
	}
	if self.fault == "policyAt-padding" && method == "policyAt" {
		encoded[32] = 1
	}
	if self.fault == "operatorAt-padding" && method == "operatorAt" {
		encoded[5*32] = 1
	}
	return encoded, ctx.Err()
}

func (self *releaseInitialBoundaryV2TestFixture) inputs(t *testing.T) []releaseEvidenceV2ActivationInput {
	t.Helper()
	history := ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}
	encoded, err := history.CanonicalJSON(self.cfg.EvidenceV2.Bounds.MaxHistoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	for index := range self.cfg.EvidenceV2.Operators {
		reference := &self.cfg.EvidenceV2.Operators[index].History
		*reference = writeReleaseBootstrapV2TestFile(t, reference.Path, encoded)
	}
	inputs, err := self.releaseBootstrapV2TestFixture.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return inputs
}

func TestReleaseInitialBoundaryV2RealCanonicalViews(t *testing.T) {
	for _, fault := range []string{"", "at-start"} {
		fixture := newReleaseInitialBoundaryV2TestFixture(t, fault)
		if err := fixture.chain.authenticateReleaseInitialBoundaryV2Context(t.Context(), fixture.contexts[0], fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes); err != nil {
			t.Fatalf("%s genuine boundary: %v", fault, err)
		}
		if fixture.views.Load() != 3 {
			t.Fatal("initial boundary omitted a real coordinator view")
		}
	}
}

func TestReleaseInitialBoundaryV2RefusesPolicyEpochAndOperatorDrift(t *testing.T) {
	for _, fault := range []string{"epoch", "policy", "future-policy", "zero-cadence", "after-start", "at-end", "overflow-cadence", "inactive", "future-operator"} {
		fixture := newReleaseInitialBoundaryV2TestFixture(t, fault)
		if err := fixture.chain.authenticateReleaseInitialBoundaryV2Context(t.Context(), fixture.contexts[0], fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes); err == nil {
			t.Fatalf("%s initial boundary was accepted", fault)
		}
	}
}

func TestReleaseInitialBoundaryV2RefusesNoncanonicalTuples(t *testing.T) {
	for _, fault := range []string{"currentEpoch-trailing", "policyAt-trailing", "operatorAt-trailing", "policyAt-padding", "operatorAt-padding"} {
		fixture := newReleaseInitialBoundaryV2TestFixture(t, fault)
		if err := fixture.chain.authenticateReleaseInitialBoundaryV2Context(t.Context(), fixture.contexts[0], fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes); err == nil {
			t.Fatalf("%s noncanonical initial view was accepted", fault)
		}
	}
}

func TestReleaseInitialBoundaryV2FinalityPrecedesViews(t *testing.T) {
	for _, fault := range []string{"unfinalized", "finalized-hash"} {
		fixture := newReleaseInitialBoundaryV2TestFixture(t, fault)
		err := fixture.chain.authenticateReleaseInitialBoundaryV2Context(t.Context(), fixture.contexts[0], fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
		if err == nil || fixture.views.Load() != 0 {
			t.Fatalf("%s initial boundary bypassed finality: %v", fault, err)
		}
	}
}

func TestReleaseInitialBoundaryV2CompleteConfiguredHistoryAndViews(t *testing.T) {
	fixture := newReleaseInitialBoundaryV2TestFixture(t, "")
	inputs := fixture.inputs(t)
	last, err := authenticateReleaseEvidenceV2InitialHistory(t.Context(), &fixture.cfg, fixture.chain, inputs, nil)
	if err != nil || last != nil {
		t.Fatalf("actual files/native/activation/history/initial views: %v", err)
	}
	if fixture.views.Load() != 6 {
		t.Fatal("initial startup authority omitted an operator")
	}
}

func TestReleaseInitialBoundaryV2HistoryRefusalPrecedesRPC(t *testing.T) {
	fixture := newReleaseInitialBoundaryV2TestFixture(t, "")
	inputs := fixture.inputs(t)
	for index := range inputs {
		inputs[index].HistoryBytes = []byte("the same changed history for every operator")
	}
	if last, err := authenticateReleaseEvidenceV2InitialHistory(t.Context(), &fixture.cfg, fixture.chain, inputs, nil); err == nil || last != nil || fixture.views.Load() != 0 {
		t.Fatalf("unverified history started historical views: %v", err)
	}
}

func TestReleaseInitialBoundaryV2ParallelFailureCancelsSibling(t *testing.T) {
	prior := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(prior)
	fixture := newReleaseInitialBoundaryV2TestFixture(t, "")
	inputs := fixture.inputs(t)
	barrier, canceled := make(chan struct{}), make(chan struct{})
	var arrived atomic.Uint64
	fixture.beforeOperator = func(ctx context.Context, noID uint64) error {
		if arrived.Add(1) == 2 {
			close(barrier)
		}
		select {
		case <-barrier:
		case <-ctx.Done():
			return ctx.Err()
		}
		if noID == inputs[0].Config.NoID {
			return errors.New("forced initial operator observation failure")
		}
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}
	last, err := authenticateReleaseEvidenceV2InitialHistory(t.Context(), &fixture.cfg, fixture.chain, inputs, nil)
	if err == nil || last != nil || !strings.Contains(err.Error(), "forced initial operator observation failure") || arrived.Load() != 2 {
		t.Fatalf("parallel initial failure lost its cause or census: %v", err)
	}
	select {
	case <-canceled:
	case <-t.Context().Done():
		t.Fatal("canceled sibling HTTP observer did not join")
	}
}

func TestReleaseInitialBoundaryV2PreCancellationHasNoRPC(t *testing.T) {
	fixture := newReleaseInitialBoundaryV2TestFixture(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := fixture.chain.authenticateReleaseInitialBoundaryV2Context(ctx, fixture.contexts[0], fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
	if !errors.Is(err, context.Canceled) || fixture.views.Load() != 0 {
		t.Fatalf("canceled initial boundary performed RPC: %v", err)
	}
}
