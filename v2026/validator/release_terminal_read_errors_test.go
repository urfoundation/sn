//go:build linux || darwin

// Terminal geometry is meaningful only after every exact pinned read succeeds.
// Faults cross the actual RPC client without replacing an authenticated verdict.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/stabi"
)

// A small serialized coordinator serves two exact epoch boundaries. Every
// selector is independently checked, including the historical terminal epoch.
type releaseTerminalReadTestFixture struct {
	chain         *ChainClient
	snapshot      *ReleaseSnapshot
	terminalHash  [32]byte
	start         *big.Int
	end           *big.Int
	terminalEpoch *big.Int
	currentEpoch  *big.Int
	reads         []string
	before        func(context.Context, string) error
}

func newReleaseTerminalReadTestFixture(t *testing.T) *releaseTerminalReadTestFixture {
	t.Helper()
	fixture := &releaseTerminalReadTestFixture{
		snapshot:     &ReleaseSnapshot{BlockNumber: 200, BlockHash: [32]byte{0x21}, Epoch: big.NewInt(8)},
		terminalHash: [32]byte{0x22}, start: big.NewInt(100), end: big.NewInt(150),
		terminalEpoch: big.NewInt(7), currentEpoch: big.NewInt(8),
	}
	prior := &ChainClient{rpcUrl: "http://geometry.example", chainId: big.NewInt(31337), coordinator: stabi.NewSTCoordinator(), contractAddr: common.Address{0x31}, release: true}
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	classify := func(call chainBatchRPCRequest) string {
		t.Helper()
		if len(call.Params) != 2 {
			t.Fatalf("unexpected terminal fixture request: %+v", call)
		}
		switch call.Method {
		case "eth_getBlockByHash":
			if string(call.Params[0]) != `"`+common.Hash(fixture.snapshot.BlockHash).Hex()+`"` || string(call.Params[1]) != "false" {
				t.Fatalf("uncaptured snapshot identity: %s", call.Params)
			}
			return "head"
		case "eth_getBlockByNumber":
			if string(call.Params[1]) != "false" {
				t.Fatal("terminal read requested an unrelated full block")
			}
			if string(call.Params[0]) == `"finalized"` {
				return "head"
			}
			if string(call.Params[0]) == `"0x95"` {
				return "terminal-hash"
			}
		case "eth_call":
			var input map[string]hexutil.Bytes
			var selector gethrpc.BlockNumberOrHash
			if err := json.Unmarshal(call.Params[0], &input); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(call.Params[1], &selector); err != nil {
				t.Fatal(err)
			}
			if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical || common.BytesToAddress(input["to"]) != prior.contractAddr {
				t.Fatal("terminal reader abandoned its exact canonical selector")
			}
			if *selector.BlockHash == common.Hash(fixture.snapshot.BlockHash) {
				if bytes.Equal(input["input"], prior.coordinator.PackEpochStartBlock(big.NewInt(7))) {
					return "epoch-start"
				}
				if bytes.Equal(input["input"], prior.coordinator.PackEpochStartBlock(big.NewInt(8))) {
					return "epoch-end"
				}
				if bytes.Equal(input["input"], prior.coordinator.PackCurrentEpoch()) {
					return "current-epoch"
				}
			} else if *selector.BlockHash == common.Hash(fixture.terminalHash) && bytes.Equal(input["input"], prior.coordinator.PackCurrentEpoch()) {
				return "terminal-epoch"
			}
		}
		t.Fatalf("unexpected terminal fixture request: %+v", call)
		return ""
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call chainBatchRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		var result any
		method, value := "", (*big.Int)(nil)
		switch classify(call) {
		case "head":
			result = map[string]any{"number": "0xc8", "hash": common.Hash(fixture.snapshot.BlockHash).Hex()}
		case "terminal-hash":
			result = map[string]any{"number": "0x95", "hash": common.Hash(fixture.terminalHash).Hex()}
		case "epoch-start":
			method, value = "epochStartBlock", fixture.start
		case "epoch-end":
			method, value = "epochStartBlock", fixture.end
		case "terminal-epoch":
			method, value = "currentEpoch", fixture.terminalEpoch
		case "current-epoch":
			method, value = "currentEpoch", fixture.currentEpoch
		}
		if method != "" {
			raw, err := parsed.Methods[method].Outputs.Pack(value)
			if err != nil {
				t.Fatal(err)
			}
			result = hexutil.Encode(raw)
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}); err != nil {
			t.Fatal(err)
		}
	})
	fixture.chain = canonicalReadbackTestClient(t, prior, func(ctx context.Context, calls []chainBatchRPCRequest) error {
		for _, call := range calls {
			name := classify(call)
			fixture.reads = append(fixture.reads, name)
			if fixture.before != nil {
				if err := fixture.before(ctx, name); err != nil {
					return err
				}
			}
		}
		return nil
	}, handler)
	return fixture
}

func (self *releaseTerminalReadTestFixture) window(ctx context.Context) error {
	runtime := &releaseRuntimeV2{chain: self.chain, history: &releaseEvidenceV2StartupHistory{}}
	_, _, err := runtime.window(ctx, self.snapshot, 7)
	return err
}

func TestReleaseTerminalReadErrorsPreservePinnedGeometry(t *testing.T) {
	for _, target := range []string{"epoch-start", "epoch-end", "terminal-hash"} {
		for _, failure := range []error{context.DeadlineExceeded, context.Canceled, io.ErrUnexpectedEOF, &gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable}, errors.Join(context.DeadlineExceeded, errors.New("synthetic integrity failure"))} {
			fixture := newReleaseTerminalReadTestFixture(t)
			fixture.before = func(_ context.Context, name string) error {
				if name == target {
					return failure
				}
				return nil
			}
			err := fixture.window(t.Context())
			wantRetry := failure == io.ErrUnexpectedEOF || RetryableEvidenceTransportError(failure)
			if !errors.Is(err, failure) || RetryableEvidenceTransportError(err) != wantRetry || strings.Contains(err.Error(), "geometry differs") {
				t.Fatalf("%s read manufactured a geometry verdict or lost its cause: %v", target, err)
			}
			fixture.before = nil
			if err := fixture.window(t.Context()); err != nil {
				t.Fatalf("%s exact pinned retry did not recover: %v", target, err)
			}
		}
	}
}

// More interrupted reads than the permanent failure budget retain the same
// snapshot. The existing provisional owner completes without epoch replacement.
func TestReleaseTerminalReadErrorsRetryThroughExistingOwner(t *testing.T) {
	fixture := newReleaseTerminalReadTestFixture(t)
	reads, submissions := 0, 0
	fixture.before = func(_ context.Context, name string) error {
		if name == "epoch-end" {
			reads++
			if reads <= releaseSteeringFailureLimit+2 {
				return context.DeadlineExceeded
			}
		}
		return nil
	}
	err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 7, nil }, func() error {
		submissions++
		return fixture.window(t.Context())
	}, func() bool { return reads <= releaseSteeringFailureLimit+2 }, true)
	if err != nil || reads != releaseSteeringFailureLimit+3 || submissions != reads {
		t.Fatalf("interrupted terminal read exhausted permanent failure budget: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}

func TestReleaseTerminalReadErrorsHonorOwnerCancellation(t *testing.T) {
	fixture := newReleaseTerminalReadTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.before = func(readCtx context.Context, name string) error {
		if name == "epoch-end" {
			cancel()
			return readCtx.Err()
		}
		return nil
	}
	submissions := 0
	var readErr error
	err := runReleaseSteeringLoopWithWaitAndDeferral(ctx, func() (uint64, error) { return 7, nil }, func() error {
		submissions++
		readErr = fixture.window(ctx)
		return readErr
	}, func() bool { return true }, true)
	if err != nil || submissions != 1 || !errors.Is(readErr, context.Canceled) || RetryableEvidenceTransportError(readErr) || strings.Contains(readErr.Error(), "geometry differs") || slices.Contains(fixture.reads, "terminal-hash") {
		t.Fatalf("owner shutdown became a geometry failure or repeated work: submissions=%d read=%v result=%v", submissions, readErr, err)
	}
}

func TestReleaseTerminalReadErrorsRejectReturnedGeometry(t *testing.T) {
	for _, geometry := range [][2]int64{{0, 150}, {100, 100}, {100, 99}, {100, 201}} {
		fixture := newReleaseTerminalReadTestFixture(t)
		fixture.start, fixture.end = big.NewInt(geometry[0]), big.NewInt(geometry[1])
		err := fixture.window(t.Context())
		if err == nil || !strings.Contains(err.Error(), "terminal geometry differs") || RetryableEvidenceTransportError(err) || slices.Contains(fixture.reads, "terminal-hash") {
			t.Fatalf("returned invalid geometry %v became retryable: %v", geometry, err)
		}
	}
}

func TestReleasePriorSettlementReadErrorsPreserveTransport(t *testing.T) {
	for _, target := range []string{"epoch-end", "terminal-hash", "terminal-epoch"} {
		for _, failure := range []error{context.DeadlineExceeded, context.Canceled, io.ErrUnexpectedEOF} {
			fixture := newReleaseTerminalReadTestFixture(t)
			fixture.before = func(_ context.Context, name string) error {
				if name == target {
					return failure
				}
				return nil
			}
			boundary, err := releasePriorSettlementBoundary(t.Context(), fixture.chain, fixture.snapshot)
			if boundary != (AttemptBoundary{}) || !errors.Is(err, failure) || RetryableEvidenceTransportError(err) != (failure != context.Canceled) || strings.Contains(err.Error(), "wrong epoch") {
				t.Fatalf("%s unavailable terminal became wrong epoch: %+v, %v", target, boundary, err)
			}
			fixture.before = nil
			boundary, err = releasePriorSettlementBoundary(t.Context(), fixture.chain, fixture.snapshot)
			want := AttemptBoundary{SettlementEpoch: 7, EVMBlock: 149, EVMBlockHash: attemptHex32(fixture.terminalHash)}
			if err != nil || boundary != want {
				t.Fatalf("%s exact prior terminal did not recover: %+v, %v", target, boundary, err)
			}
		}
	}
}

func TestReleasePriorSettlementReadErrorsRejectReturnedEpoch(t *testing.T) {
	for _, epoch := range []*big.Int{big.NewInt(8), new(big.Int).SetUint64(^uint64(0)), new(big.Int).Lsh(big.NewInt(1), 64)} {
		fixture := newReleaseTerminalReadTestFixture(t)
		fixture.terminalEpoch = epoch
		boundary, err := releasePriorSettlementBoundary(t.Context(), fixture.chain, fixture.snapshot)
		if boundary != (AttemptBoundary{}) || err == nil || !strings.Contains(err.Error(), "wrong epoch") || RetryableEvidenceTransportError(err) {
			t.Fatalf("returned terminal epoch %s was admitted: %+v, %v", epoch, boundary, err)
		}
	}
	fixture := newReleaseTerminalReadTestFixture(t)
	fixture.end = big.NewInt(0)
	if _, err := releasePriorSettlementBoundary(t.Context(), fixture.chain, fixture.snapshot); err == nil || !strings.Contains(err.Error(), "start block is zero") || strings.Contains(err.Error(), "%!w") || RetryableEvidenceTransportError(err) {
		t.Fatalf("zero start was presented as an RPC error: %v", err)
	}
}

func TestAttemptBoundarySnapshotReadErrorsSeparateTransportFromEpoch(t *testing.T) {
	fixture := newReleaseTerminalReadTestFixture(t)
	reader := &chainAttemptBoundaryRPC{chain: fixture.chain}
	fixture.before = func(_ context.Context, name string) error {
		if name == "current-epoch" {
			return context.DeadlineExceeded
		}
		return nil
	}
	if _, err := reader.Snapshot(t.Context()); !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
		t.Fatalf("finalized epoch transport error was masked: %v", err)
	}
	fixture.before = nil
	fixture.currentEpoch = new(big.Int).Lsh(big.NewInt(1), 64)
	if _, err := reader.Snapshot(t.Context()); err == nil || !strings.Contains(err.Error(), "epoch is outside uint64") || strings.Contains(err.Error(), "%!w") || RetryableEvidenceTransportError(err) {
		t.Fatalf("returned invalid epoch was presented as an RPC error: %v", err)
	}
	fixture.currentEpoch = big.NewInt(8)
	if boundary, err := reader.Snapshot(t.Context()); err != nil || boundary.SettlementEpoch != 8 || boundary.EVMBlockHash != attemptHex32(fixture.snapshot.BlockHash) {
		t.Fatalf("finalized epoch retry changed its snapshot: %+v, %v", boundary, err)
	}
}
