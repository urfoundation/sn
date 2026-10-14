// Narrow deterministic roots isolate canonical authority, parser and Safe
// runtime barriers without repeating the expensive full native metadata graph.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Neither the old execution signature nor an edited policy authorizes RPC
// assumptions. Evidence is independently pinned and reread on every checkpoint.
func TestBootstrapSuccessorCanonicalIndependentAuthority(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	approval := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := approval.validate(t.Context(), f.approval.Plan); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*bootstrapSuccessorCanonicalApproval)
	}{
		{name: "old signature domain", mutate: func(a *bootstrapSuccessorCanonicalApproval) { a.Signature = f.approval.Signature }},
		{name: "edited signature", mutate: func(a *bootstrapSuccessorCanonicalApproval) { a.Signature = string(bytes.Repeat([]byte("00"), 64)) }},
		{name: "different execution", mutate: func(a *bootstrapSuccessorCanonicalApproval) {
			a.Authorization.ExecutionPlanHash = rootObjectHash("synthetic other execution")
		}},
		{name: "empty global pool assumption", mutate: func(a *bootstrapSuccessorCanonicalApproval) {
			a.Authorization.Policy = "owned-rpc-finality-and-empty-pool-assertions"
		}},
		{name: "missing cutover", mutate: func(a *bootstrapSuccessorCanonicalApproval) { a.Authorization.CutoverEvidence = planFileReference{} }},
		{name: "missing runtime review", mutate: func(a *bootstrapSuccessorCanonicalApproval) { a.Authorization.RuntimeEvidence = planFileReference{} }},
		{name: "edited current runtime", mutate: func(a *bootstrapSuccessorCanonicalApproval) {
			a.Authorization.CurrentRuntime.RuntimeVersion.SpecVersion++
		}},
		{name: "unreviewed runtime codec", mutate: func(a *bootstrapSuccessorCanonicalApproval) {
			a.Authorization.CurrentRuntime.RuntimeSourceCommit = "synthetic-unreviewed-codec"
		}},
	}
	for _, c := range cases {
		changed := approval
		c.mutate(&changed)
		if err := changed.validate(t.Context(), f.approval.Plan); err == nil {
			t.Fatal("canonical authority accepted", c.name)
		}
	}
	if err := os.WriteFile(approval.Authorization.CutoverEvidence.Path, []byte("synthetic replacement cutover"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := approval.validate(t.Context(), f.approval.Plan); err == nil {
		t.Fatal("canonical authority accepted changed evidence")
	}
}

// Once counted, a missing authority cannot be recreated from a newly supplied
// approval, even if the original execution and all nonce claims remain intact.
func TestBootstrapSuccessorCanonicalCountedAuthoritySurvivesRestart(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	approval := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), approval); err != nil {
		t.Fatal(err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	if owner.last.CanonicalAuthorityHash != rootObjectHash(approval) {
		t.Fatal("counted attempt omitted canonical authority")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	other := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), other); err == nil {
		t.Fatal("counted authority changed after restart")
	}
	path := filepath.Join(owner.local.path, bootstrapSuccessorCanonicalFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reopened, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
	if reopened != nil {
		reopened.close()
	}
	if err == nil {
		t.Fatal("missing counted authority renewed execution custody")
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	if owner.last.CumulativeAttempts != 9 || owner.canonicalAuthorityHash != rootObjectHash(approval) {
		t.Fatal("authority repair changed original counted custody")
	}
}

// An interrupted immutable authority publication resumes under the same exact
// execution claimant, before any attempt can obtain write permission.
func TestBootstrapSuccessorCanonicalInterruptedAuthorityPublication(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	approval := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	fired := false
	owner.local.hook = func(stage string) error {
		if stage == bootstrapSuccessorCanonicalFile+":name-synced" {
			fired = true
			return errors.New("synthetic authority interruption")
		}
		return nil
	}
	if err := owner.retainCanonicalAuthority(t.Context(), approval); err == nil || !fired {
		t.Fatal("canonical authority interruption was not reached", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	if err := owner.retainCanonicalAuthority(t.Context(), approval); err != nil {
		t.Fatal(err)
	}
	if owner.last.CumulativeAttempts != 8 || owner.last.Phase != "adopted" || owner.canonicalAuthorityHash != rootObjectHash(approval) {
		t.Fatal("authority recovery consumed or lost custody")
	}
}

// Actual published code supplies the positive getters. Faults alter exactly one
// authority fact in the RPC response while leaving all other facts available.
func TestBootstrapSuccessorCanonicalSafeAuthority(t *testing.T) {
	adapter, model, chain := bootstrapSuccessorCanonicalSafeFixture(t)
	plan := model.approval.Plan
	if state, err := adapter.safeState(t.Context(), plan, "pending"); err != nil || state.SafeNonce != "17" || state.Threshold != 2 {
		t.Fatal("canonical Safe positive authority", state, err)
	}
	methodData := func(name string) string { return "0x" + common.Bytes2Hex(model.oracle.oracleAbi.Methods[name].ID) }
	cases := []struct {
		name   string
		method string
		fault  func([]any, any) any
	}{
		{name: "proxy runtime", method: "eth_getCode", fault: func(p []any, result any) any {
			if p[0] == plan.Review.Transaction.Safe.Hex() {
				return "0x00"
			}
			return result
		}},
		{name: "singleton pointer", method: "eth_getStorageAt", fault: func(p []any, result any) any {
			if p[1] == (common.Hash{}).Hex() {
				return common.Hash{1}.Hex()
			}
			return result
		}},
		{name: "owners", method: "eth_call", fault: func(p []any, result any) any {
			if p[0].(map[string]any)["data"] == methodData("getOwners") {
				return "0x" + common.Bytes2Hex(make([]byte, 160))
			}
			return result
		}},
		{name: "threshold", method: "eth_call", fault: func(p []any, result any) any {
			if p[0].(map[string]any)["data"] == methodData("getThreshold") {
				return common.BigToHash(big.NewInt(1)).Hex()
			}
			return result
		}},
		{name: "owner storage", method: "eth_getStorageAt", fault: func(p []any, result any) any {
			if p[1] == common.BigToHash(big.NewInt(3)).Hex() {
				return common.Hash{}.Hex()
			}
			return result
		}},
		{name: "nonce storage", method: "eth_getStorageAt", fault: func(p []any, result any) any {
			if p[1] == common.BigToHash(big.NewInt(5)).Hex() {
				return common.BigToHash(big.NewInt(42)).Hex()
			}
			return result
		}},
		{name: "module census", method: "eth_call", fault: func(p []any, result any) any {
			if bytes.HasPrefix([]byte(p[0].(map[string]any)["data"].(string)), []byte(methodData("getModulesPaginated"))) {
				return "0x" + common.Bytes2Hex(make([]byte, 128))
			}
			return result
		}},
		{name: "guard", method: "eth_getStorageAt", fault: func(p []any, result any) any {
			if p[1] == "0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8" {
				return common.Hash{1}.Hex()
			}
			return result
		}},
		{name: "module guard", method: "eth_getStorageAt", fault: func(p []any, result any) any {
			if p[1] == "0xb104e0b93118902c651344349b610029d694cfdec91c589c91ebafbcd0289947" {
				return common.Hash{1}.Hex()
			}
			return result
		}},
		{name: "fallback", method: "eth_getStorageAt", fault: func(p []any, result any) any {
			if p[1] == "0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5" {
				return common.Hash{1}.Hex()
			}
			return result
		}},
		{name: "inner digest", method: "eth_call", fault: func(p []any, result any) any {
			if bytes.HasPrefix([]byte(p[0].(map[string]any)["data"].(string)), []byte(methodData("getTransactionHash"))) {
				return common.Hash{1}.Hex()
			}
			return result
		}},
	}
	for _, c := range cases {
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			chain.override = func(method string, p []any, result any) any {
				if method == c.method {
					return c.fault(p, result)
				}
				return result
			}
		}()
		if _, err := adapter.safeState(t.Context(), plan, "pending"); err == nil {
			t.Fatal("canonical Safe authority admitted", c.name)
		}
	}
}

// The retained transaction lookup is account-scoped, and unsupported RPC never
// proves absence. Unrelated transactions cannot be returned as the exact hash.
func TestBootstrapSuccessorCanonicalExactPendingLookup(t *testing.T) {
	adapter, model, chain := bootstrapSuccessorCanonicalSafeFixture(t)
	plan := model.approval.Plan
	var retained types.Transaction
	if err := retained.UnmarshalBinary(common.FromHex(plan.SignedRelayer)); err != nil {
		t.Fatal(err)
	}
	if known, err := adapter.retainedTransactionKnown(t.Context(), plan); err != nil || known {
		t.Fatal("unused retained hash was not absent", known, err)
	}
	for _, c := range []struct {
		name   string
		result any
		known  bool
		failed bool
	}{
		{name: "retained pending", result: &retained, known: true},
		{name: "unsupported", result: mappingFixtureRpcError{code: -32601}, failed: true},
		{name: "other signed transaction", result: chain.tx, failed: true},
	} {
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			chain.override = func(method string, _ []any, result any) any {
				if method == "eth_getTransactionByHash" {
					return c.result
				}
				return result
			}
		}()
		known, err := adapter.retainedTransactionKnown(t.Context(), plan)
		if known != c.known || (err != nil) != c.failed {
			t.Fatal("exact pending lookup lost scoped disposition", c.name, known, err)
		}
	}
}

// Required financial and position fields are validated before a receipt reaches
// the Safe outcome classifier. RPC defaults cannot become successful evidence.
func TestBootstrapSuccessorCanonicalReceiptFields(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	plan, receipt := f.approval.Plan, f.receipt().Receipt
	base := map[string]any{"transactionHash": plan.TransactionHash.Hex(), "blockHash": receipt.BlockHash.Hex(), "blockNumber": "0x77", "transactionIndex": "0x0",
		"from": plan.Review.Relayer.Sender.Hex(), "to": plan.Review.Transaction.Safe.Hex(), "contractAddress": nil, "status": "0x1", "gasUsed": "0x100", "effectiveGasPrice": "0x2", "logs": receipt.Logs}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := bootstrapSuccessorCanonicalReceiptFacts(raw, plan)
	if err != nil {
		t.Fatal("canonical receipt positive parser", err)
	}
	if outcome, err := f.profile.classifyReceipt(plan.transaction(), plan.TransactionHash, decoded); err != nil || outcome.Outcome != "safe-inner-success" {
		t.Fatal("canonical receipt positive inner outcome", outcome, err)
	}
	for _, field := range []string{"transactionHash", "blockHash", "blockNumber", "transactionIndex", "from", "to", "contractAddress", "status", "gasUsed", "effectiveGasPrice", "logs"} {
		changed := bootstrapSuccessorExecutionTestCopy(t, base)
		delete(changed, field)
		raw, _ := json.Marshal(changed)
		if _, err := bootstrapSuccessorCanonicalReceiptFacts(raw, plan); err == nil {
			t.Fatal("canonical receipt accepted omitted field", field)
		}
	}
	for _, c := range []struct {
		name  string
		field string
		value any
	}{
		{name: "other sender", field: "from", value: common.Address{1}.Hex()},
		{name: "other target", field: "to", value: common.Address{1}.Hex()},
		{name: "other transaction", field: "transactionHash", value: crypto.Keccak256Hash([]byte("synthetic other transaction")).Hex()},
		{name: "gas bound", field: "gasUsed", value: "0xffffffff"},
		{name: "fee bound", field: "effectiveGasPrice", value: "0xffffffff"},
		{name: "noncanonical nonce", field: "transactionIndex", value: "0x00"},
		{name: "missing logs", field: "logs", value: nil},
	} {
		changed := bootstrapSuccessorExecutionTestCopy(t, base)
		changed[c.field] = c.value
		raw, _ := json.Marshal(changed)
		if _, err := bootstrapSuccessorCanonicalReceiptFacts(raw, plan); err == nil {
			t.Fatal("canonical receipt accepted", c.name)
		}
	}
}
