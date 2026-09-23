// Serialized synthetic node errors reproduce preflight selection without any
// live endpoint, writer, timers or scheduler-dependent failures.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// The same fields become a typed error directly or a real JSON-RPC response.
type precompileRecoveryRpcFailure struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Keeps the provider's message separate from its code and revert bytes.
func (self precompileRecoveryRpcFailure) Error() string { return self.Message }

// Implements the pinned client's RPC error contract.
func (self precompileRecoveryRpcFailure) ErrorCode() int { return self.Code }

// Implements the pinned client's optional revert-data contract.
func (self precompileRecoveryRpcFailure) ErrorData() any { return self.Data }

// Only recognized execution envelopes can select an approved funding step.
func TestPrecompileRecoveryRevertClassification(t *testing.T) {
	t.Parallel()
	ownedRevert := precompileRecoveryRpcFailure{Code: -32603, Message: "VM Exception while processing transaction: revert", Data: "0x"}
	for _, failure := range []error{
		ownedRevert,
		precompileRecoveryRpcFailure{Code: -32603, Message: ownedRevert.Message + " synthetic reason", Data: "0x01020304"},
		precompileRecoveryRpcFailure{Code: -32603, Message: ownedRevert.Message + ": synthetic reason"},
		precompileRecoveryRpcFailure{Code: -32000, Message: "execution reverted: synthetic reason", Data: "0x"},
		precompileRecoveryRpcFailure{Code: 3, Message: "execution reverted"},
		precompileRecoveryRpcFailure{Code: 3, Message: "synthetic custom error", Data: "0x01020304"},
		finalSemanticTestRPCError{code: -32000, message: "execution reverted"},
		fmt.Errorf("preflight: %w", ownedRevert),
		errors.Join(ownedRevert, fmt.Errorf("second revert: %w", ownedRevert)),
	} {
		if !precompileRecoveryCallReverted(failure) || evmReadRpcErrorIsTransient(failure) {
			t.Errorf("actual revert was not kept distinct: %v", failure)
		}
	}
	for _, failure := range []error{
		nil,
		context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF,
		errors.New("execution reverted"), errors.New(ownedRevert.Message),
		precompileRecoveryRpcFailure{Code: -32603, Message: "diagnostic mentions execution reverted", Data: "0x"},
		precompileRecoveryRpcFailure{Code: -32603, Message: "VM Exception while processing transaction: out of gas", Data: "0x"},
		precompileRecoveryRpcFailure{Code: -32603, Message: "VM Exception while processing transaction: revertedness", Data: "0x"},
		precompileRecoveryRpcFailure{Code: -32602, Message: "execution reverted", Data: "0x"},
		precompileRecoveryRpcFailure{Code: -32002, Message: "request timed out", Data: "0x"},
		precompileRecoveryRpcFailure{Code: -32000, Message: "upstream overloaded", Data: "0x"},
		precompileRecoveryRpcFailure{Code: 3, Message: "execution reverted", Data: "0x0"},
		precompileRecoveryRpcFailure{Code: -32603, Message: ownedRevert.Message, Data: "0xzz"},
		precompileRecoveryRpcFailure{Code: -32603, Message: ownedRevert.Message, Data: "missing prefix"},
		precompileRecoveryRpcFailure{Code: -32603, Message: ownedRevert.Message, Data: map[string]any{"unrelated": "0x"}},
		errors.Join(ownedRevert, context.Canceled),
		errors.Join(ownedRevert, context.DeadlineExceeded),
		errors.Join(ownedRevert, errors.New("signed custody mismatch")),
		fmt.Errorf("execution reverted: %w", context.DeadlineExceeded),
	} {
		if precompileRecoveryCallReverted(failure) {
			t.Errorf("non-revert selected funding: %v", failure)
		}
	}
	for _, code := range []int{-32002, -32005, -32016} {
		failure := precompileRecoveryRpcFailure{Code: code, Message: ownedRevert.Message + " timeout", Data: "0x"}
		if precompileRecoveryCallReverted(failure) || evmReadRpcErrorIsTransient(failure) {
			t.Errorf("conflicting RPC envelope was treated as funding or capacity: %v", failure)
		}
	}
}

// Exercises the actual executor through serialized RPC replies. One signed
// fixture serves every bounded variation; no receipt or authorization changes.
func TestPrecompileRecoveryPreflightSelectsOnlyApprovedFunding(t *testing.T) {
	f := precompileRecoveryTestFixtureWithSampleResidual(t, true)
	ownedRevert := precompileRecoveryRpcFailure{Code: -32603, Message: "VM Exception while processing transaction: revert", Data: "0x"}
	temporary := precompileRecoveryRpcFailure{Code: -32002, Message: "request timed out"}
	hard := precompileRecoveryRpcFailure{Code: -32602, Message: "invalid params"}
	for _, c := range []struct {
		name      string
		after     bool
		funded    bool
		failures  []error
		operation string
		pending   bool
		transient bool
		cause     error
	}{
		{name: "successful move sweep", failures: []error{nil}, operation: "recover-move"},
		{name: "owned node top-up", failures: []error{ownedRevert, nil}, operation: "top-up"},
		{name: "geth top-up", failures: []error{precompileRecoveryRpcFailure{Code: 3, Message: "execution reverted", Data: "0x"}, nil}, operation: "top-up"},
		{name: "successful sample sweep", after: true, failures: []error{nil}, operation: "recover-sample"},
		{name: "owned node reseed", after: true, failures: []error{ownedRevert, nil}, operation: "reseed-sample"},
		{name: "sweep capacity", failures: []error{temporary}, transient: true},
		{name: "sweep timeout", failures: []error{context.DeadlineExceeded}, transient: true, cause: context.DeadlineExceeded},
		{name: "sweep cancellation", failures: []error{context.Canceled}, cause: context.Canceled},
		{name: "sweep malformed params", failures: []error{hard}},
		{name: "funding capacity", failures: []error{ownedRevert, temporary}, transient: true},
		{name: "funding malformed params", failures: []error{ownedRevert, hard}},
		{name: "funding revert", failures: []error{ownedRevert, ownedRevert}, pending: true},
		{name: "funded move refuses repeat top-up", funded: true, failures: []error{ownedRevert}, pending: true},
		{name: "funded sample refuses repeat reseed", after: true, funded: true, failures: []error{ownedRevert}, pending: true},
	} {
		evidence := *f.original
		recovery := *f.evidence.Recovery
		if c.after {
			evidence = *f.evidence
			recovery.Steps = recovery.Steps[:2]
			if c.funded {
				recovery.Steps = f.evidence.Recovery.Steps[:3]
			}
		} else {
			recovery = PrecompileRecoveryEvidence{Authorization: recovery.Authorization}
			if c.funded {
				recovery.Steps = f.evidence.Recovery.Steps[:1]
			}
		}
		evidence.Recovery = &recovery
		evidence.Complete = false
		before, err := json.Marshal(&evidence)
		if err != nil {
			t.Fatal(err)
		}
		code := f.base.payloads.ExpectedRuntime[common.HexToAddress(evidence.ProbeAddress)]
		client, calls := precompileRecoveryPreflightClient(t, &evidence, code, c.failures)
		executor := &Executor{cfg: f.cfg, deployer: &EvmTxManager{client: client}}
		step, err := executor.preparePrecompileRecoveryStep(t.Context(), &evidence)
		client.Close()
		if *calls != len(c.failures) || step.Operation != c.operation || errors.Is(err, errPrecompileRecoveryPending) != c.pending || historicalPreparationReadIsTransient(err) != c.transient || (c.cause != nil && !errors.Is(err, c.cause)) {
			t.Fatalf("%s: calls=%d operation=%q error=%v", c.name, *calls, step.Operation, err)
		}
		if c.operation == "" {
			if err == nil || !reflect.DeepEqual(step, PrecompileRecoveryStep{}) {
				t.Fatalf("%s: failed preflight returned a usable step", c.name)
			}
			if expected, ok := c.failures[len(c.failures)-1].(precompileRecoveryRpcFailure); ok && !c.pending {
				var rpcError rpc.Error
				if !errors.As(err, &rpcError) || rpcError.ErrorCode() != expected.Code {
					t.Fatalf("%s: lost the original RPC cause: %v", c.name, err)
				}
			}
		} else if err != nil || step.Action.Parameters["recovery_authorization_hash"] != recovery.Authorization.Hash || step.Action.Parameters["recovery_operation"] != c.operation {
			t.Fatalf("%s: approved action changed: step=%+v error=%v", c.name, step, err)
		}
		if c.operation == "top-up" && (step.Move.AmountRao != recovery.Authorization.Request.TopUpRao || step.Action.Parameters["recovery_value_wei"] != "0") {
			t.Fatalf("%s: top-up increased approved value", c.name)
		}
		if c.operation == "reseed-sample" && (step.Seed == nil || step.Seed.TAORao != recovery.Authorization.Request.ReseedTaoRao || step.Action.Parameters["recovery_value_wei"] != step.Seed.ValueWei) {
			t.Fatalf("%s: reseed changed approved value", c.name)
		}
		after, err := json.Marshal(&evidence)
		if err != nil || string(before) != string(after) {
			t.Fatalf("%s: read-only preflight changed durable evidence: %v", c.name, err)
		}
	}
}

// Answers only the pinned code/stake reads and the requested probe calls.
// Any nonce, fee estimate or broadcast is an immediate fixture failure.
func precompileRecoveryPreflightClient(t *testing.T, evidence *PrecompileConformanceEvidence, code []byte, failures []error) (*ethclient.Client, *int) {
	t.Helper()
	sample, move, settled, err := precompileRecoveryPositions(evidence)
	if err != nil || !settled {
		t.Fatalf("preflight fixture accounting: %v", err)
	}
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		t.Fatal(err)
	}
	if len(code) == 0 || crypto.Keccak256Hash(code).Hex() != evidence.Recovery.Authorization.Request.ProbeRuntimeHash {
		t.Fatal("synthetic probe code differs from signed authority")
	}
	calls := 0
	client, err := rpc.DialOptions(t.Context(), "http://recovery.example", rpc.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.Id}
		if call.Method == "eth_getBlockByNumber" {
			if string(call.Params[0]) != `"finalized"` {
				t.Fatal("preflight used an unfinalized head")
			}
			response["result"] = map[string]any{"number": "0x12c", "hash": common.Hash{99}.Hex()}
		} else {
			if len(call.Params) != 2 || string(call.Params[1]) != `"0x12c"` {
				t.Fatalf("%s did not use the pinned quote head", call.Method)
			}
			switch call.Method {
			case "eth_getCode":
				response["result"] = hexutil.Encode(code)
			case "eth_call":
				var args struct {
					To    common.Address `json:"to"`
					Input hexutil.Bytes  `json:"input"`
				}
				if err := json.Unmarshal(call.Params[0], &args); err != nil {
					t.Fatal(err)
				}
				if args.To == stakingPrecompileAddress {
					values, err := parsed.Methods["getStake"].Inputs.Unpack(args.Input[4:])
					if err != nil {
						t.Fatal(err)
					}
					balance := uint64(50)
					if values[1].([32]byte) == ss58Mirror(common.HexToAddress(evidence.ProbeAddress)) {
						balance = move
						hotkey := values[0].([32]byte)
						if hexBytesValue(hotkey[:]) == evidence.SampleHotkey {
							balance = sample
						}
					}
					response["result"] = hexutil.Encode(common.LeftPadBytes(new(big.Int).SetUint64(balance).Bytes(), 32))
				} else if args.To == common.HexToAddress(evidence.ProbeAddress) {
					if calls >= len(failures) {
						t.Fatal("preflight exceeded its expected operation count")
					}
					failure := failures[calls]
					calls++
					if rpcFailure, ok := failure.(precompileRecoveryRpcFailure); ok {
						response["error"] = rpcFailure
					} else if failure != nil {
						return nil, failure
					} else {
						response["result"] = "0x"
					}
				} else {
					t.Fatal("preflight called an unapproved target")
				}
			default:
				t.Fatalf("preflight attempted unexpected RPC %s", call.Method)
			}
		}
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	return ethclient.NewClient(client), &calls
}
