// Refused public preflight reads still pass through revert diagnostics. Faults
// enter only after a real HTTP response has completed under its original owner.
package onchain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// A matching callback counts forbidden dispatch without stranding the test.
type onchainRevertTestMatch struct{ calls *atomic.Int32 }

// Formatting cannot itself recurse through an error graph.
func (self *onchainRevertTestMatch) Error() string { return "synthetic foreign revert match" }

// A generic errors.As lookup would invoke this foreign implementation.
func (self *onchainRevertTestMatch) As(any) bool { self.calls.Add(1); return false }

// A foreign data interface cannot impersonate a decoded geth remote reply.
type onchainRevertTestData struct{ calls *atomic.Int32 }

// Keep the diagnostic independent of the optional foreign data method.
func (self *onchainRevertTestData) Error() string { return "synthetic foreign revert data" }

// The original generic interface lookup would call this on the public path.
func (self *onchainRevertTestData) ErrorData() any { self.calls.Add(1); return "0x" }

// A finite escape makes an omitted traversal bound fail by count, not by a
// hanging job. The production path must stop long before this backstop.
type onchainRevertTestCycle struct{ visits *atomic.Int32 }

// Error rendering never follows the deliberate cycle.
func (self *onchainRevertTestCycle) Error() string { return "synthetic revert cycle" }

// Each visitor has at most32 levels; the old unrestricted visitor exceeds1024.
func (self *onchainRevertTestCycle) Unwrap() error {
	if self.visits.Add(1) > 1024 {
		return io.EOF
	}
	return self
}

// Individually small branches cannot renew aggregate diagnostic work.
type onchainRevertTestBranch struct {
	causes []error
	visits *atomic.Int32
}

// Formatting a refused branch does not walk its leaves.
func (self *onchainRevertTestBranch) Error() string { return "synthetic revert branch" }

// The observation includes every branch edge examined by either owner.
func (self *onchainRevertTestBranch) Unwrap() []error {
	self.visits.Add(1)
	return self.causes
}

// A transport leaf lets admission inspect enough of a wide graph to exhaust
// its allowance; diagnostics must then use a separate finite allowance.
type onchainRevertTestLeaf struct{ visits *atomic.Int32 }

// No recursive formatting supplies the work-bound assertion.
func (self *onchainRevertTestLeaf) Error() string { return "synthetic revert transport leaf" }

// The leaf's explicit child is real transport availability, not a match method.
func (self *onchainRevertTestLeaf) Unwrap() error { self.visits.Add(1); return io.EOF }

// One physical unavailable preflight is followed by hard graph refusal before
// any estimate or signature. The hook can add a cause but cannot erase the API.
func onchainRevertPreflightRefusal(t *testing.T, cause error) error {
	t.Helper()
	fixture := newOnchainReadTestServer(t, func(method string, _ int) int {
		if method == "eth_call" {
			return http.StatusServiceUnavailable
		}
		return 0
	})
	parent, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	waits, faults, prepared := 0, 0, 0
	ctx, _ := onchainReadTestContext(t, parent, func(context.Context, time.Duration) error {
		waits++
		return errors.New("synthetic unexpected preflight replay")
	})
	hooks := ctx.Value(onchainReadRetryHooksKey{}).(onchainReadRetryHooks)
	hooks.additionalReadError = func(error) error { faults++; return cause }
	ctx = context.WithValue(ctx, onchainReadRetryHooksKey{}, hooks)
	result, err := SubmitWithHooks(ctx, fixture.params(false), SubmitHooks{Prepared: func(common.Hash, []byte) error { prepared++; return nil }})
	if result != nil || err == nil || faults != 1 || waits != 0 || prepared != 0 || fixture.count("eth_chainId") != 1 || fixture.count("eth_call") != 1 || fixture.count("eth_estimateGas") != 0 || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatalf("public preflight escaped its original hard graph: faults=%d waits=%d prepared=%d", faults, waits, prepared)
	}
	return err
}

// Both a foreign matching callback and a foreign data interface remain inert
// after the retry classifier refuses the actual failed public preflight.
func TestOnchainSubmitPreflightNeverCallsForeignRevertMethods(t *testing.T) {
	var matches, data atomic.Int32
	for _, cause := range []error{&onchainRevertTestMatch{calls: &matches}, &onchainRevertTestData{calls: &data}, (*onchainRevertTestData)(nil)} {
		err := onchainRevertPreflightRefusal(t, cause)
		if matches.Load() != 0 || data.Load() != 0 || !errors.Is(err, cause) {
			t.Fatal("public preflight invoked foreign revert authority or lost its original cause")
		}
	}
}

// The public diagnostic path must not restart unbounded inspection after the
// read policy already exhausted its own independent allowance.
func TestOnchainSubmitPreflightBoundsRevertDiagnosticGraph(t *testing.T) {
	var cycleVisits, branchVisits atomic.Int32
	cycle := &onchainRevertTestCycle{visits: &cycleVisits}
	branches := make([]error, 32)
	for index := range branches {
		leaves := make([]error, 32)
		for leaf := range leaves {
			leaves[leaf] = &onchainRevertTestLeaf{visits: &branchVisits}
		}
		branches[index] = &onchainRevertTestBranch{causes: leaves, visits: &branchVisits}
	}
	wide := &onchainRevertTestBranch{causes: branches, visits: &branchVisits}
	for _, cause := range []error{cycle, wide} {
		err := onchainRevertPreflightRefusal(t, cause)
		if !errors.Is(err, cause) {
			t.Fatal("bounded public revert diagnostics discarded the original graph")
		}
	}
	if cycleVisits.Load() == 0 || cycleVisits.Load() > 66 || branchVisits.Load() == 0 || branchVisits.Load() > 256 {
		t.Fatalf("public revert diagnostics exceeded two finite visitors: cycle=%d branch=%d", cycleVisits.Load(), branchVisits.Load())
	}
}

// Actual geth JSON-RPC errors retain their real ABI revert data. No foreign
// implementation or test callback supplies the decoded result or verdict.
func TestOnchainSubmitPreflightRetainsDecodedRemoteRevert(t *testing.T) {
	textType, err := abi.NewType("string", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (abi.Arguments{{Type: textType}}).Pack("synthetic contract refusal")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append([]byte{0x08, 0xc3, 0x79, 0xa0}, encoded...)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		if call.Method == "eth_chainId" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "0x67932"})
			return
		}
		calls.Add(1)
		if call.Method != "eth_call" {
			t.Error("decoded revert admitted a later public operation")
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "error": map[string]any{"code": -32000, "message": "synthetic original completed refusal", "data": "0x" + hex.EncodeToString(encoded)}})
	}))
	defer server.Close()
	fixture := newOnchainReadTestServer(t, nil)
	params := fixture.params(false)
	params.Rpcs = []string{server.URL}
	parent, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	waits, prepared := 0, 0
	ctx, _ := onchainReadTestContext(t, parent, func(context.Context, time.Duration) error {
		waits++
		return errors.New("synthetic forbidden revert retry")
	})
	result, err := SubmitWithHooks(ctx, params, SubmitHooks{Prepared: func(common.Hash, []byte) error { prepared++; return nil }})
	if result != nil || err == nil || calls.Load() != 1 || waits != 0 || prepared != 0 || !strings.Contains(err.Error(), "synthetic original completed refusal") || !strings.Contains(err.Error(), `revert "synthetic contract refusal"`) {
		t.Fatalf("public preflight lost its original decoded geth revert: calls=%d waits=%d prepared=%d err=%v", calls.Load(), waits, prepared, err)
	}
}
