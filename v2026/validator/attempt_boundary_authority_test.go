// Cached immutable attribution must still close against current canonical
// finality. Transport faults cannot replace the originally reserved boundary.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/stabi"
)

// Controls only mutable authority and post-read cancellation; census and
// binding requests use the same counted fixture as the cache controls.
type attemptBoundaryAuthorityRpc struct {
	*attemptBoundaryRPCCounters
	validateError error
	afterBinding  func()
	afterValidate func()
}

// Records every fresh check before returning the original failure unchanged.
func (self *attemptBoundaryAuthorityRpc) Validate(ctx context.Context, boundary AttemptBoundary) error {
	if err := self.attemptBoundaryRPCCounters.Validate(ctx, boundary); err != nil {
		return err
	}
	if self.afterValidate != nil {
		self.afterValidate()
	}
	return self.validateError
}

// A late caller cancellation must not install an unchecked successful entry.
func (self *attemptBoundaryAuthorityRpc) Binding(ctx context.Context, boundary AttemptBoundary, clientId connect.Id) (stabi.BindingAtOutput, error) {
	binding, err := self.attemptBoundaryRPCCounters.Binding(ctx, boundary, clientId)
	if self.afterBinding != nil {
		self.afterBinding()
	}
	return binding, err
}

// Both the shared latest view and a trail's retained pin must refuse a stale
// cached success. Recovery reuses immutable bytes at exactly the same block.
func TestAttemptBoundaryCacheClosesSuccessfulCacheHits(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		for _, failure := range []error{errors.New("canonical hash changed"), errors.New("finalized head regressed"), context.DeadlineExceeded} {
			boundary, clientId := attemptLedgerTestBoundary(), connect.NewId()
			rpc := &attemptBoundaryAuthorityRpc{attemptBoundaryRPCCounters: &attemptBoundaryRPCCounters{
				boundary: boundary, hotkeys: map[[32]byte]uint16{}, bindings: map[connect.Id]stabi.BindingAtOutput{}, reads: map[connect.Id]int{},
			}}
			resolver := newCachedAttemptBoundaryResolverWithLifecycle(t.Context(), rpc, time.Minute)
			defer resolver.close()
			observed, bindings, err := resolver.Resolve(t.Context(), nil, []connect.Id{clientId})
			if err != nil || observed != boundary || len(bindings) != 1 {
				t.Fatalf("initial cache control: boundary=%+v bindings=%v error=%v", observed, bindings, err)
			}
			var selected *AttemptBoundary
			if pinned {
				selected = &boundary
			}
			rpc.validateError = failure
			observed, bindings, err = resolver.Resolve(t.Context(), selected, []connect.Id{clientId})
			if !errors.Is(err, failure) || observed != (AttemptBoundary{}) || bindings != nil || retryableAttemptBindingReadError(err) != (failure == context.DeadlineExceeded) {
				t.Fatalf("pinned=%t stale cache published evidence or changed cause: boundary=%+v bindings=%v error=%v", pinned, observed, bindings, err)
			}
			rpc.validateError = nil
			rpc.boundary.EVMBlock++
			rpc.boundary.EVMBlockHash = attemptHex32([32]byte{99})
			observed, bindings, err = resolver.Resolve(t.Context(), selected, []connect.Id{clientId})
			if err != nil || observed != boundary || len(bindings) != 1 || rpc.snapshots != 1 || rpc.scans != 1 || rpc.reads[clientId] != 1 || rpc.validates != 3 {
				t.Fatalf("pinned=%t recovery reselected or repeated immutable work: boundary=%+v bindings=%v snapshots=%d scans=%d reads=%d validates=%d error=%v", pinned, observed, bindings, rpc.snapshots, rpc.scans, rpc.reads[clientId], rpc.validates, err)
			}
		}
	}
}

// No cancellation point may publish a success, including the last read before
// returning a previously cached binding to an assignment owner.
func TestAttemptBoundaryCacheRejectsLateReadCancellation(t *testing.T) {
	for _, phase := range []string{"binding", "authority"} {
		boundary, clientId := attemptLedgerTestBoundary(), connect.NewId()
		rpc := &attemptBoundaryAuthorityRpc{attemptBoundaryRPCCounters: &attemptBoundaryRPCCounters{
			boundary: boundary, hotkeys: map[[32]byte]uint16{}, bindings: map[connect.Id]stabi.BindingAtOutput{}, reads: map[connect.Id]int{},
		}}
		resolver := newCachedAttemptBoundaryResolverWithLifecycle(t.Context(), rpc, time.Minute)
		defer resolver.close()
		if _, _, err := resolver.Resolve(t.Context(), nil, nil); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		if phase == "binding" {
			rpc.afterBinding = cancel
		} else {
			rpc.afterValidate = cancel
		}
		observed, bindings, err := resolver.Resolve(ctx, &boundary, []connect.Id{clientId})
		cancel()
		if !errors.Is(err, context.Canceled) || observed != (AttemptBoundary{}) || bindings != nil {
			t.Fatalf("%s cancellation published evidence: boundary=%+v bindings=%v error=%v", phase, observed, bindings, err)
		}
		if phase == "binding" && len(resolver.blocks[boundary.EVMBlock].bindings) != 0 {
			t.Fatal("canceled binding read entered the success cache")
		}
		rpc.afterBinding, rpc.afterValidate = nil, nil
		observed, bindings, err = resolver.Resolve(t.Context(), &boundary, []connect.Id{clientId})
		wantReads := 1
		if phase == "binding" {
			wantReads = 2
		}
		if err != nil || observed != boundary || len(bindings) != 1 || rpc.reads[clientId] != wantReads {
			t.Fatalf("%s cancellation prevented original-pin recovery: boundary=%+v bindings=%v reads=%d error=%v", phase, observed, bindings, rpc.reads[clientId], err)
		}
	}
}

// The actual geth transport and coordinator decoder consume explicit wire
// identities. A synthetic Ethereum hash is never reconstructed from a header.
func TestAttemptBoundaryAuthorityClosesFinalizedWitness(t *testing.T) {
	for _, fault := range []string{"unchanged", "advanced", "not finalized", "closing witness changed", "closing boundary changed", "missing finalized"} {
		boundary := AttemptBoundary{SettlementEpoch: 42, EVMBlock: 7, EVMBlockHash: attemptHex32(chainTestBlockIdentityHash(7))}
		byNumberReads, finalizedReads, epochReads := 0, 0, 0
		client := chainHTTPTestRPC(t, "http://attempt-finality.invalid", chainHTTPResponseLimit, func(http.RoundTripper) http.RoundTripper {
			return chainHTTPTestRoundTripper(func(request *http.Request) (*http.Response, error) {
				var call chainBatchRPCRequest
				if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
					t.Fatal(err)
				}
				response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
				switch call.Method {
				case "eth_getBlockByNumber":
					var selector string
					if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
						t.Fatalf("%s malformed block selector: %s", fault, call.Params)
					}
					var number uint64
					if selector == "finalized" {
						finalizedReads++
						if epochReads == 0 {
							t.Fatal("finality did not close after the pinned state read")
						}
						number = 7
						if fault == "advanced" || fault == "closing witness changed" || fault == "closing boundary changed" {
							number = 9
						} else if fault == "not finalized" {
							number = 6
						} else if fault == "missing finalized" {
							response["result"] = nil
							return chainReadRetryResponse(t, http.StatusOK, response), nil
						}
					} else {
						byNumberReads++
						var err error
						number, err = hexutil.DecodeUint64(selector)
						if err != nil || number != 7 && number != 9 {
							t.Fatalf("%s changed original block: %s", fault, selector)
						}
					}
					hash := chainTestBlockIdentityHash(number)
					if selector != "finalized" && finalizedReads != 0 && (fault == "closing witness changed" && number == 9 || fault == "closing boundary changed" && number == 7) {
						hash = [32]byte{99}
					}
					response["result"] = map[string]any{"number": hexutil.EncodeUint64(number), "hash": common.Hash(hash)}
				case "eth_call":
					epochReads++
					var selector gethrpc.BlockNumberOrHash
					if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil || selector.BlockHash == nil || selector.BlockHash.Hex() != boundary.EVMBlockHash || !selector.RequireCanonical {
						t.Fatalf("%s epoch read lost original canonical hash: %s", fault, call.Params)
					}
					response["result"] = fmt.Sprintf("0x%064x", boundary.SettlementEpoch)
				default:
					t.Fatalf("%s unexpected rpc call %s", fault, call.Method)
				}
				return chainReadRetryResponse(t, http.StatusOK, response), nil
			})
		})
		chain := &ChainClient{client: ethclient.NewClient(client), coordinator: stabi.NewSTCoordinator(), contractAddr: common.Address{1}, release: true}
		err := (&chainAttemptBoundaryRPC{chain: chain}).Validate(t.Context(), boundary)
		wantSuccess := fault == "unchanged" || fault == "advanced"
		if (err == nil) != wantSuccess || epochReads != 1 {
			t.Fatalf("%s authority success=%t epoch=%d finality=%d canonical=%d error=%v", fault, wantSuccess, epochReads, finalizedReads, byNumberReads, err)
		}
	}
}
