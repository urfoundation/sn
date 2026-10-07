// The heavy roots exercise the command implementation, durable owner, canonical
// adapter, local Frontier mapping, published Safe and reviewed coordinator with
// a separately injected synthetic history capability. Public writes stay gated.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/v2026/stabi"
)

// A lost reply follows real inner execution under the fixture's history
// capability. Reopening must reconcile it without another transaction write.
func TestBootstrapSuccessorCanonicalCommandExecutesAndRecovers(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	chain := f.original.contracts
	original := bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)
	var publicOutput, publicError bytes.Buffer
	publicArgs := []string{"contract-successor-execution-resume", "--config", f.original.path, "--run-dir", f.original.config.RunDirectory, "--accept-plan-hash", f.original.preparation.Plan.ContentHash}
	publicArgs = append(append(publicArgs, f.paths...), f.approvalArgs...)
	publicArgs = append(publicArgs, "--online", "--submit", "--canonical-approval", f.canonicalRef.Path, "--canonical-approval-sha256", f.canonicalRef.Sha256)
	if code := runBootstrapSuccessorExecutionCommand(f.original.storageContext(t.Context()), publicArgs, &publicOutput, &publicError); code != 2 || publicOutput.Len() != 0 || !strings.Contains(publicError.String(), errBootstrapSuccessorSafeProvenanceUnavailable.Error()) {
		t.Fatal("complete signed review unlocked public submission", code, publicError.String())
	}
	if _, err := os.Stat(filepath.Join(f.original.config.RunDirectory, bootstrapSuccessorCanonicalFile)); !os.IsNotExist(err) {
		t.Fatal("public provenance refusal mutated canonical custody", err)
	}
	func() { chain.stateLock.Lock(); defer chain.stateLock.Unlock(); chain.loseReply = true }()
	var stdout bytes.Buffer
	if code, diagnostic := f.online(&stdout, true); code != 1 || stdout.Len() != 0 {
		t.Fatal("lost canonical acknowledgement did not retain unresolved custody", code, diagnostic)
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 || chain.receipt == nil || chain.receipt["status"] != "0x1" || !bytes.Equal(chain.writes[8], common.FromHex(f.approval.Plan.SignedRelayer)) {
			t.Fatal("canonical command did not execute exactly one retained Safe outer transaction", len(chain.writes), chain.receipt)
		}
		chain.loseReply = false
		chain.advanceEmpty()
		laterNative, originalOverride := chain.head, chain.override
		chain.override = func(method string, params []any, result any) any {
			result = originalOverride(method, params, result)
			if method == "state_getRuntimeVersion" && params[0] == chain.hashes[laterNative] {
				version := f.canonical.Authorization.CurrentRuntime.RuntimeVersion
				version.SpecVersion++
				return version
			}
			return result
		}
	}()
	for iteration := range 2 {
		stdout.Reset()
		if code, diagnostic := f.online(&stdout, iteration == 1); code != 0 {
			t.Fatal("canonical restart reconciliation", iteration, code, diagnostic)
		}
		var result bootstrapSuccessorExecutionResult
		if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "installed" || !result.InstallationComplete || !result.CanonicalAdoptionVerified || result.SubmissionAttempted || result.ActivationReady || result.CumulativeAttempts != 9 {
			t.Fatal("canonical restart lost exact successful installation or attempt custody", result)
		}
	}
	for name, raw := range original {
		if retained, err := os.ReadFile(filepath.Join(f.original.config.RunDirectory, name)); err != nil || string(retained) != raw {
			t.Fatal("canonical execution changed original custody", name, err)
		}
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 || chain.state.GetNonce(f.approval.Plan.Review.Relayer.Sender) != 43 {
			t.Fatal("canonical restart resent or confused outer nonce")
		}
		if chain.counts["txpool_content"] != 0 || chain.counts["author_pendingExtrinsics"] != 0 {
			t.Fatal("canonical command required unsupported global pool census")
		}
	}()
	owner, _ := f.open()
	if owner.last.CanonicalAuthorityHash != rootObjectHash(f.canonical) || owner.last.Receipt == nil || owner.last.Receipt.NativeNumber == owner.last.Receipt.Receipt.BlockNumber {
		t.Fatal("canonical terminal event omitted approval or equated native/EVM heights")
	}
}

// Capturing the actual request deadline proves renewal per original receipt.
// No wall-clock sleeps or scheduler timing are used to expire valid work.
type bootstrapSuccessorCanonicalDeadlineTransport struct {
	base           http.RoundTripper
	stateLock      sync.Mutex
	deadlines      []time.Time
	stateDeadlines []time.Time
}

// HTTP request contexts inherit their receipt operation's deadline. The body
// clone is read without consuming the request eventually sent by the adapter.
func (self *bootstrapSuccessorCanonicalDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var call struct {
		Method string `json:"method"`
	}
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
	}
	switch call.Method {
	case "eth_getTransactionReceipt", "eth_getCode", "eth_getStorageAt", "eth_call", "eth_getTransactionCount", "eth_getBalance", "eth_getTransactionByHash":
		deadline, _ := request.Context().Deadline()
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			if call.Method == "eth_getTransactionReceipt" {
				self.deadlines = append(self.deadlines, deadline)
			} else {
				self.stateDeadlines = append(self.stateDeadlines, deadline)
			}
		}()
	}
	return self.base.RoundTrip(request)
}

// Faults are installed between synchronous operations under the fixture lock.
// Each candidate changes one authority fact and must leave the send count zero.
func TestBootstrapSuccessorCanonicalAdapterBoundaries(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	owner, adapter := f.open()
	plan, chain := owner.planCopy(), f.original.contracts
	baseOverride := chain.override
	setFault := func(fault func(string, []any, any) any) {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		chain.override = func(method string, params []any, result any) any {
			result = baseOverride(method, params, result)
			if fault != nil {
				return fault(method, params, result)
			}
			return result
		}
	}
	setFault(func(method string, params []any, result any) any {
		if method == "eth_getTransactionReceipt" && params[0] == adapter.records[0].TransactionHash {
			return nil
		}
		return result
	})
	missingCtx, cancelMissing := context.WithCancel(t.Context())
	adapter.chain.client.retryWait = func(ctx context.Context, _ time.Duration) error {
		cancelMissing()
		return ctx.Err()
	}
	if _, err := adapter.authenticate(missingCtx, plan); !errors.Is(err, context.Canceled) || errors.Is(err, errRpcIntegrity) {
		t.Fatal("canonical adapter changed history after an unavailable original receipt", err)
	}
	cancelMissing()
	adapter.chain.client.retryWait = nil
	setFault(nil)
	transport := &bootstrapSuccessorCanonicalDeadlineTransport{base: adapter.chain.client.httpClient.Transport}
	adapter.chain.client.httpClient.Transport = transport
	seals, err := adapter.authenticate(t.Context(), plan)
	if err != nil || len(seals) != 8 {
		t.Fatal("canonical adapter positive original authentication", len(seals), err)
	}
	func() {
		transport.stateLock.Lock()
		defer transport.stateLock.Unlock()
		if len(transport.deadlines) != 8 {
			t.Fatal("canonical receipt deadline census differs", len(transport.deadlines))
		}
		for i, deadline := range transport.deadlines {
			if deadline.IsZero() || i > 0 && !deadline.After(transport.deadlines[i-1]) {
				t.Fatal("canonical original receipts share one aggregate timeout", i)
			}
		}
	}()
	func() { transport.stateLock.Lock(); defer transport.stateLock.Unlock(); transport.stateDeadlines = nil }()
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		chain.state.SetNonce(common.Address{19: 211}, 8, tracing.NonceChangeUnspecified)
	}()
	advances := 0
	setFault(func(method string, _ []any, result any) any {
		if method == "chain_getFinalizedHead" {
			chain.advanceEmpty()
			advances++
			return chain.hashes[chain.head]
		}
		return result
	})
	observation, err := adapter.observe(t.Context(), plan)
	setFault(nil)
	if err != nil {
		t.Fatal("unrelated pending account blocked canonical admission", err)
	}
	if advances < 2 || observation.NativeNumber <= plan.Review.Request.StartNativeNumber+1 {
		t.Fatal("canonical advancing head was not admitted with its retained snapshot", advances, observation.NativeNumber)
	}
	func() {
		transport.stateLock.Lock()
		defer transport.stateLock.Unlock()
		if len(transport.stateDeadlines) < 20 {
			t.Fatal("canonical current read deadline census is incomplete", len(transport.stateDeadlines))
		}
		for i, deadline := range transport.stateDeadlines {
			if deadline.IsZero() || i > 0 && !deadline.After(transport.stateDeadlines[i-1]) {
				t.Fatal("canonical current reads share one aggregate timeout", i)
			}
		}
	}()
	adapter.chain.client.httpClient.Transport = transport.base
	var retained types.Transaction
	if err := retained.UnmarshalBinary(common.FromHex(plan.SignedRelayer)); err != nil {
		t.Fatal(err)
	}
	changedAfterRead := false
	for _, c := range []struct {
		name  string
		fault func(string, []any, any) any
	}{
		{name: "unapproved current runtime", fault: func(method string, p []any, result any) any {
			if method == "state_getRuntimeVersion" && p[0] == chain.hashes[chain.head] {
				version := f.canonical.Authorization.CurrentRuntime.RuntimeVersion
				version.SpecVersion++
				return version
			}
			return result
		}},
		{name: "canonical hash changed after pending reads", fault: func(method string, p []any, result any) any {
			if method == "eth_getTransactionByHash" {
				changedAfterRead = true
			}
			if changedAfterRead && method == "chain_getBlockHash" && p[0] == float64(chain.head) {
				return (common.Hash{1}).Hex()
			}
			return result
		}},
		{name: "pending relayer nonce", fault: func(method string, p []any, result any) any {
			if method == "eth_getTransactionCount" && p[1] == "pending" {
				return "0x2b"
			}
			return result
		}},
		{name: "finalized relayer nonce", fault: func(method string, p []any, result any) any {
			if method == "eth_getTransactionCount" && p[1] != "pending" {
				return "0x2b"
			}
			return result
		}},
		{name: "relayer funding", fault: func(method string, _ []any, result any) any {
			if method == "eth_getBalance" {
				return "0x0"
			}
			return result
		}},
		{name: "pending Safe code", fault: func(method string, p []any, result any) any {
			if method == "eth_getCode" && p[0] == plan.Review.Transaction.Safe.Hex() && p[1] == "pending" {
				return "0x00"
			}
			return result
		}},
		{name: "evidence runtime", fault: func(method string, p []any, result any) any {
			if method == "eth_getCode" && p[0] == adapter.plans[7].Address.Hex() {
				return "0x00"
			}
			return result
		}},
		{name: "retained pending transaction", fault: func(method string, _ []any, result any) any {
			if method == "eth_getTransactionByHash" {
				return &retained
			}
			return result
		}},
		{name: "unsupported pending lookup", fault: func(method string, _ []any, result any) any {
			if method == "eth_getTransactionByHash" {
				return mappingFixtureRpcError{code: -32601}
			}
			return result
		}},
	} {
		setFault(c.fault)
		if _, err := adapter.observe(t.Context(), plan); err == nil {
			t.Fatal("canonical adapter admitted changed scoped authority", c.name)
		}
	}
	setFault(func(method string, _ []any, result any) any {
		if method == "eth_getTransactionByHash" {
			return &retained
		}
		return result
	})
	if result, err := adapter.reconcile(t.Context(), plan); err != nil || result.Status != "pending" {
		t.Fatal("canonical retained pool transaction was not pending", result, err)
	}
	setFault(nil)
	if result, err := adapter.reconcile(t.Context(), plan); err != nil || result.Status != "absent" {
		t.Fatal("canonical exact unused hash was not absent", result, err)
	}
	retainedEvent := rootObjectHash(owner.last)
	for _, c := range []struct {
		name       string
		diagnostic string
	}{
		{name: "Safe nonce", diagnostic: "successor canonical Safe pending authority or nonce differs"},
		{name: "relayer nonce", diagnostic: "successor execution current Safe authority, independent nonces, evidence state or native window differs"},
		{name: "current runtime", diagnostic: "successor canonical current runtime differs from its independent successor approval"},
	} {
		reached := false
		f.afterProvenance = func() {
			reached = true
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			switch c.name {
			case "Safe nonce":
				chain.state.SetState(plan.Review.Transaction.Safe, common.HexToHash("0x5"), common.HexToHash("0x12"))
			case "relayer nonce":
				chain.state.SetNonce(plan.Review.Relayer.Sender, 43, tracing.NonceChangeUnspecified)
			case "current runtime":
				chain.advanceEmpty()
				laterHash := chain.hashes[chain.head]
				chain.override = func(method string, params []any, result any) any {
					result = baseOverride(method, params, result)
					if method == "state_getRuntimeVersion" && params[0] == laterHash {
						version := f.canonical.Authorization.CurrentRuntime.RuntimeVersion
						version.SpecVersion++
						return version
					}
					return result
				}
			}
		}
		_, observationErr := adapter.observe(t.Context(), plan)
		f.afterProvenance = nil
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			chain.state.SetState(plan.Review.Transaction.Safe, common.HexToHash("0x5"), common.HexToHash("0x11"))
			chain.state.SetNonce(plan.Review.Relayer.Sender, 42, tracing.NonceChangeUnspecified)
			if len(chain.writes) != 8 {
				t.Fatal("canonical provenance refresh wrote a transaction", c.name, len(chain.writes))
			}
		}()
		setFault(nil)
		if !reached {
			t.Fatal("canonical provenance mutation barrier was not reached", c.name, observationErr)
		}
		if observationErr == nil || !strings.Contains(observationErr.Error(), c.diagnostic) || adapter.admitted {
			t.Fatal("canonical admission reused pending state from before provenance", c.name, observationErr)
		}
		if rootObjectHash(owner.last) != retainedEvent {
			t.Fatal("canonical provenance refresh changed retained attempt custody", c.name)
		}
	}
	if _, err := adapter.observe(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.submit(t.Context(), plan, common.FromHex(plan.SignedRelayer)); err == nil {
		t.Fatal("canonical submit reused admission from before reservation")
	}
	if _, err := adapter.observe(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := adapter.observe(canceled, plan); !errors.Is(err, context.Canceled) {
		t.Fatal("canonical canceled refresh did not report cancellation", err)
	}
	if err := adapter.submit(t.Context(), plan, common.FromHex(plan.SignedRelayer)); err == nil {
		t.Fatal("failed canonical refresh reused earlier send admission")
	}
	if _, err := adapter.observe(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if err := adapter.submit(t.Context(), plan, []byte("synthetic replacement")); err == nil {
		t.Fatal("canonical submit changed exact signed bytes")
	}
	if err := adapter.submit(t.Context(), plan, common.FromHex(plan.SignedRelayer)); err != nil {
		t.Fatal("canonical bounded exact send", err)
	}
	if err := adapter.submit(t.Context(), plan, common.FromHex(plan.SignedRelayer)); err == nil {
		t.Fatal("canonical adapter repeated one counted write")
	}
	result, err := adapter.reconcile(t.Context(), plan)
	if err != nil || result.Status != "included" || result.Receipt == nil {
		t.Fatal("canonical real Safe inclusion", result, err)
	}
	if phase, err := result.Receipt.phase(plan, f.profile); err != nil || phase != "installed" {
		t.Fatal("canonical real inner binding", phase, err)
	}
	for _, c := range []struct {
		name  string
		fault func(string, []any, any) any
	}{
		{name: "transaction position", fault: func(method string, _ []any, result any) any {
			if method == "eth_getTransactionByBlockHashAndIndex" {
				return types.NewTx(&types.LegacyTx{Nonce: 1})
			}
			return result
		}},
		{name: "missing Safe inner event", fault: func(method string, p []any, result any) any {
			if method == "eth_getTransactionReceipt" && p[0] == plan.TransactionHash.Hex() {
				value := bootstrapSuccessorExecutionTestCopy(t, result.(map[string]any))
				value["logs"] = []any{}
				return value
			}
			return result
		}},
		{name: "anchor readback", fault: func(method string, p []any, result any) any {
			if method == "eth_call" {
				input := p[0].(map[string]any)
				if input["to"] == plan.Review.Transaction.To.Hex() && input["data"] == "0x"+common.Bytes2Hex(stabi.NewSTCoordinator().PackValidatorEvidence()) {
					return common.Hash{}.Hex()
				}
			}
			return result
		}},
	} {
		setFault(c.fault)
		if _, err := adapter.reconcile(t.Context(), plan); err == nil {
			t.Fatal("canonical receipt accepted changed inclusion authority", c.name)
		}
	}
	setFault(nil)
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 {
			t.Fatal("canonical boundary checks wrote extra transactions", len(chain.writes))
		}
	}()
}
