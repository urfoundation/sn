// The registerOperatorPool review and readback run against the in-repo sim
// chain: the reviewed v1 and v2 contracts in geth's EVM beside fake runtime
// precompiles, with pool 1 not yet registered for v2. A route in front of
// the chain lets a test replace one answer. The Safe is the owner's account.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/v2026/stabi"
)

// A test's replacement for the index-th answer since the last takeMethods,
// given the chain's own answer.
type poolTestOverride func(index int, method string, params []json.RawMessage, result any, failure *e2eRPCError) (any, *e2eRPCError)

// A raw HTTP reply in place of a JSON-RPC answer.
type poolTestReply struct {
	status int
	body   string
}

// Replaces the at-th answer since the last takeMethods.
func poolTestAnswerAt(at int, result any, failure *e2eRPCError) poolTestOverride {
	return func(index int, _ string, _ []json.RawMessage, original any, originalFailure *e2eRPCError) (any, *e2eRPCError) {
		if index == at {
			return result, failure
		}
		return original, originalFailure
	}
}

// The deployed stack behind its own JSON-RPC route.
type poolTestChain struct {
	*e2eFixture
	url         string
	lock        sync.Mutex
	override    poolTestOverride
	methods     []string
	transaction common.Hash
}

func newPoolTestChain(t *testing.T) *poolTestChain {
	t.Helper()
	f := &poolTestChain{e2eFixture: newE2EDeployment(t, e2eDefaultFirst)}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	f.url = server.URL
	return f
}

// Answers one request as the chain does, unless the override replaces it.
func (self *poolTestChain) serve(writer http.ResponseWriter, request *http.Request) {
	var call e2eRPCRequest
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	result, failure := self.chain.rpc(call.Method, call.Params)
	self.lock.Lock()
	index, override := len(self.methods), self.override
	self.methods = append(self.methods, call.Method)
	self.lock.Unlock()
	if override != nil {
		result, failure = override(index, call.Method, call.Params, result, failure)
	}
	if raw, ok := result.(poolTestReply); ok {
		writer.WriteHeader(raw.status)
		_, _ = writer.Write([]byte(raw.body))
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}
	if failure != nil {
		delete(response, "result")
		response["error"] = failure
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response)
}

func (self *poolTestChain) setOverride(override poolTestOverride) {
	self.lock.Lock()
	defer self.lock.Unlock()
	self.override = override
}

// The methods the route answered since the last call, which restarts the count.
func (self *poolTestChain) takeMethods() []string {
	self.lock.Lock()
	defer self.lock.Unlock()
	methods := self.methods
	self.methods = nil
	return methods
}

// One command of mode over the stack's inputs, each flag named in changes
// replaced by the value after it; an empty value drops the flag.
func (self *poolTestChain) run(mode string, changes ...string) (string, int, string) {
	self.t.Helper()
	inputs := [][2]string{{"rpc", self.url}, {"settlement2", self.settlement.Hex()}, {"no-id", "1"}, {"pool-hotkey", common.Hash(self.poolV2).Hex()}, {"first-epoch", "2"}}
	if mode == "review" {
		inputs = append(inputs, [2]string{"safe", self.owner.address.Hex()}, [2]string{"maximum-burn-rao", "1000000000"}, [2]string{"value-wei", "1000000000000000000"})
	} else {
		inputs = append(inputs, [2]string{"transaction-hash", self.transaction.Hex()})
	}
	args := []string{"register-operator-pool", mode}
	for _, input := range inputs {
		for index := 0; index+1 < len(changes); index += 2 {
			if changes[index] == input[0] {
				input[1] = changes[index+1]
			}
		}
		if input[1] != "" {
			args = append(args, "--"+input[0], input[1])
		}
	}
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), args, &stdout, &stderr)
	return stdout.String(), code, stderr.String()
}

// A review that must pass.
func (self *poolTestChain) review(changes ...string) poolRegistrationReview {
	self.t.Helper()
	stdout, code, diagnostic := self.run("review", changes...)
	var review poolRegistrationReview
	if code != 0 || json.Unmarshal([]byte(stdout), &review) != nil {
		self.t.Fatalf("review %v: %d %s %s", changes, code, stdout, diagnostic)
	}
	return review
}

// The Safe sends the reviewed call: exactly its to, value and data.
func (self *poolTestChain) send(review poolRegistrationReview) *types.Receipt {
	self.t.Helper()
	value, ok := new(big.Int).SetString(review.ValueWei, 10)
	data, err := hexutil.Decode(review.Data)
	if !ok || err != nil {
		self.t.Fatalf("reviewed value or data: %v", err)
	}
	receipt := self.must(self.owner, common.HexToAddress(review.To), value, data)
	self.transaction = receipt.TxHash
	return receipt
}

// The sender and data of an eth_call request; none for other methods.
func poolTestCall(method string, params []json.RawMessage) (*common.Address, []byte) {
	var call e2eCallArgs
	if method != "eth_call" || json.Unmarshal(params[0], &call) != nil {
		return nil, nil
	}
	_, _, data := call.message()
	return call.From, data
}

// The messages of named refusals or differences, as the command prints them.
func poolTestTexts(errs ...error) []string {
	texts := []string{}
	for _, err := range errs {
		texts = append(texts, err.Error())
	}
	return texts
}

// Named call failures as review refuses them.
func poolTestRefused(names ...string) []string {
	texts := []string{}
	for _, name := range names {
		texts = append(texts, poolRefusal(name).Error())
	}
	return texts
}

// A refusal or difference that must exit 3 with exactly the wanted messages
// among all and print nothing.
func requirePoolRefusal(t *testing.T, name, stdout string, code int, diagnostic string, want, all []string) {
	t.Helper()
	if code != 3 || stdout != "" {
		t.Fatalf("%s: %d %s %s", name, code, stdout, diagnostic)
	}
	for _, candidate := range all {
		if strings.Contains(diagnostic, candidate) != slices.Contains(want, candidate) {
			t.Fatalf("%s: %s, want exactly %v", name, diagnostic, want)
		}
	}
}

// PR-1: review prints the Safe's exact call, the binding's calldata with its
// decoded arguments, beside the contracts STSettlement2 names, the head, E_s's
// start and the blocks remaining, after an eth_call preflight from the Safe;
// it reads only and sends nothing. The Safe's send of the printed call
// registers pool 1 with the UID the preflight predicted, readback confirms the
// event, vault2.pools and settlementAt, and a second review is refused.
func TestPoolRegistrationReviewSendReadback(t *testing.T) {
	f := newPoolTestChain(t)
	head := f.chain.next() - 1
	review := f.review()
	data := e2eSettlementBinding.PackRegisterOperatorPool(e2eRao(e2eNoID), f.poolV2, e2eBurnLimit, 2)
	parsed, err := stabi.STSettlement2MetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	method := parsed.Methods["registerOperatorPool"]
	words := slices.Concat(method.ID, common.LeftPadBytes([]byte{1}, 32), f.poolV2[:], common.LeftPadBytes(e2eRao(e2eBurnLimit).Bytes(), 32), common.LeftPadBytes([]byte{2}, 32))
	if review.Data != hexutil.Encode(data) || !bytes.Equal(data, words) || method.Sig != poolRegistrationFunction {
		t.Fatalf("calldata %s is not the binding's %x for %s", review.Data, data, method.Sig)
	}
	start := e2eView(f.e2eFixture, f.coordinator, e2eCoordinatorBinding.PackEpochStartBlock(e2eRao(2)), e2eCoordinatorBinding.UnpackEpochStartBlock)
	want := poolRegistrationReview{Schema: poolRegistrationReviewSchema, ChainId: 964, Safe: f.owner.address.Hex(), To: f.settlement.Hex(), ValueWei: "1000000000000000000", Data: hexutil.Encode(data), Function: poolRegistrationFunction,
		Args:   poolRegistrationArgs{NoId: "1", Hotkey: common.Hash(f.poolV2).Hex(), MaximumBurnRao: "1000000000", FirstEpoch: "2"},
		Vault2: f.vault2.Hex(), EscrowHotkey: common.Hash(f.escrowV2).Hex(), V1Coordinator: f.coordinator.Hex(), HeadBlock: head, FirstEpochStartBlock: start, BlocksRemaining: new(big.Int).Sub(start, e2eRao(head)), PreflightUid: 3}
	if rootObjectHash(review) != rootObjectHash(want) || start.Uint64() <= head {
		t.Fatalf("review %+v, want %+v", review, want)
	}
	if methods := f.takeMethods(); !slices.Equal(methods, []string{"eth_chainId", "eth_blockNumber", "eth_call", "eth_call", "eth_call", "eth_call", "eth_call"}) || f.chain.next()-1 != head {
		t.Fatalf("review read %v or changed the chain", methods)
	}
	receipt := f.send(review)
	f.requireLogs(receipt, f.vault2Log("PoolRegistered", e2eRao(e2eNoID), f.poolV2, review.PreflightUid, uint64(2)))
	stdout, code, diagnostic := f.run("readback")
	var readback poolRegistrationReadback
	if code != 0 || json.Unmarshal([]byte(stdout), &readback) != nil {
		t.Fatalf("readback: %d %s %s", code, stdout, diagnostic)
	}
	wanted := poolRegistrationReadback{Schema: poolRegistrationReadbackSchema, ChainId: 964, Settlement2: f.settlement.Hex(), Vault2: f.vault2.Hex(), NoId: 1, PoolHotkey: common.Hash(f.poolV2).Hex(), FirstEpoch: 2,
		Uid: review.PreflightUid, TransactionHash: receipt.TxHash.Hex(), BlockNumber: receipt.BlockNumber, HeadBlock: receipt.BlockNumber.Uint64()}
	if rootObjectHash(readback) != rootObjectHash(wanted) {
		t.Fatalf("readback %+v, want %+v", readback, wanted)
	}
	if methods := f.takeMethods(); !slices.Equal(methods, []string{"eth_chainId", "eth_getTransactionReceipt", "eth_blockNumber", "eth_call", "eth_call", "eth_call"}) {
		t.Fatalf("readback read %v", methods)
	}
	stdout, code, diagnostic = f.run("review")
	requirePoolRefusal(t, "second review", stdout, code, diagnostic, poolTestRefused(poolInvalidConfiguration), poolTestRefused(poolInvalidConfiguration))
}

// PR-2: review refuses, before any read, a zero pool hotkey and a value other
// than maximumBurnRao × 10^9 wei; then a route that is not EVM chain 964,
// vault2's escrow hotkey as the pool hotkey, and a first epoch whose start
// block the head has reached, each before the preflight. Each exits 3 with
// its named refusal and prints nothing. One block before E_s starts, review
// passes with one block remaining; at its start it is refused.
func TestPoolRegistrationReviewRefusesLocally(t *testing.T) {
	f := newPoolTestChain(t)
	local := poolTestTexts(errPoolZeroHotkey, errPoolValue, errPoolChain, errPoolEscrowHotkey, errPoolEpochStarted)
	for _, refusal := range []struct {
		name    string
		changes []string
		chain   string
		want    error
		reads   int
	}{
		{"zero-hotkey", []string{"pool-hotkey", common.Hash{}.Hex()}, "", errPoolZeroHotkey, 0},
		{"value-below", []string{"value-wei", "999999999999999999"}, "", errPoolValue, 0},
		{"value-above", []string{"value-wei", "1000000000000000001"}, "", errPoolValue, 0},
		{"value-other-cap", []string{"maximum-burn-rao", "2000000000"}, "", errPoolValue, 0},
		{"chain", nil, "0x1", errPoolChain, 1},
		{"escrow-hotkey", []string{"pool-hotkey", common.Hash(f.escrowV2).Hex()}, "", errPoolEscrowHotkey, 4},
		{"started", []string{"first-epoch", "0"}, "", errPoolEpochStarted, 6},
	} {
		f.setOverride(func(_ int, method string, _ []json.RawMessage, result any, failure *e2eRPCError) (any, *e2eRPCError) {
			if method == "eth_chainId" && refusal.chain != "" {
				return refusal.chain, nil
			}
			return result, failure
		})
		stdout, code, diagnostic := f.run("review", refusal.changes...)
		requirePoolRefusal(t, refusal.name, stdout, code, diagnostic, poolTestTexts(refusal.want), local)
		if reads := f.takeMethods(); len(reads) != refusal.reads {
			t.Fatalf("%s read %v before refusing", refusal.name, reads)
		}
	}
	f.setOverride(nil)
	start := e2eView(f.e2eFixture, f.coordinator, e2eCoordinatorBinding.PackEpochStartBlock(e2eRao(1)), e2eCoordinatorBinding.UnpackEpochStartBlock).Uint64()
	f.chain.rollTo(start)
	if review := f.review("first-epoch", "1"); review.HeadBlock != start-1 || review.FirstEpochStartBlock.Uint64() != start || review.BlocksRemaining.Uint64() != 1 {
		t.Fatalf("review one block before E_s: %+v", review)
	}
	f.chain.rollTo(start + 1)
	stdout, code, diagnostic := f.run("review", "first-epoch", "1")
	requirePoolRefusal(t, "E_s's start", stdout, code, diagnostic, poolTestTexts(errPoolEpochStarted), local)
}

// PR-2: a mode other than review and readback, a missing, unknown or extra
// argument, or a malformed address, hotkey, integer, transaction hash or
// route exits 2 before any read.
func TestPoolRegistrationUsage(t *testing.T) {
	f := newPoolTestChain(t)
	for _, usage := range []struct {
		mode    string
		changes []string
	}{
		{"send", nil},
		{"review", []string{"first-epoch", ""}},
		{"review", []string{"safe", ""}},
		{"review", []string{"settlement2", strings.TrimPrefix(f.settlement.Hex(), "0x")}},
		{"review", []string{"settlement2", common.Address{}.Hex()}},
		{"review", []string{"safe", "0x1234"}},
		{"review", []string{"pool-hotkey", "0x1234"}},
		{"review", []string{"value-wei", "01"}},
		{"review", []string{"value-wei", "1e18"}},
		{"review", []string{"no-id", "-1"}},
		{"review", []string{"rpc", "ftp://127.0.0.1/"}},
		{"readback", []string{"transaction-hash", "0x1234"}},
		{"readback", []string{"transaction-hash", ""}},
	} {
		if stdout, code, diagnostic := f.run(usage.mode, usage.changes...); code != 2 || stdout != "" {
			t.Fatalf("%s %v: %d %s %s", usage.mode, usage.changes, code, stdout, diagnostic)
		}
	}
	for _, args := range [][]string{{"register-operator-pool"}, {"register-operator-pool", "review", "--rpc", f.url, "extra"}, {"register-operator-pool", "readback", "--safe", f.owner.address.Hex()}} {
		var stdout, stderr bytes.Buffer
		if code := runMain(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
	}
	if reads := f.takeMethods(); len(reads) != 0 {
		t.Fatalf("usage errors read %v", reads)
	}
}

// PR-3: the preflight from the Safe names each refusal registerOperatorPool
// raises, each from the real contracts where the chain can reach it: a
// deactivated v1 operator, a first epoch whose v1 policy is too short, a Safe
// that refuses the burn surplus, a zero burn cap, a Safe that does not own
// STSettlement2 and a no_id that is not a v1 operator. InvalidEpoch, which
// the local check pre-empts, is injected. Each exits 3 and prints nothing.
func TestPoolRegistrationPreflightNamesReverts(t *testing.T) {
	for _, revert := range []struct {
		name    string
		prepare func(*poolTestChain) []string
		inject  *e2eRPCError
		want    string
	}{
		{"inactive-operator", func(f *poolTestChain) []string {
			f.must(f.owner, f.coordinator, nil, e2eCoordinatorBinding.PackScheduleOperator(e2eRao(e2eNoID), f.deposit, f.depositSigner.address, f.operator.address, false, 1))
			f.chain.rollTo(f.end(0) + 1)
			return nil
		}, nil, poolInactiveOperator},
		{"invalid-epoch", nil, &e2eRPCError{Code: 3, Message: "execution reverted", Data: hexutil.Encode(stabi.STSettlement2InvalidEpochErrorID().Bytes()[:4])}, poolInvalidEpoch},
		{"cadence", func(f *poolTestChain) []string {
			short := f.policy(15_000, 2_400)
			short.EffectiveEpoch = 2
			f.must(f.owner, f.coordinator, nil, e2eCoordinatorBinding.PackSchedulePolicy(short))
			return nil
		}, nil, poolCadenceTooShort},
		{"refund", func(f *poolTestChain) []string {
			f.chain.native(func(runtime *e2eRuntime) {
				runtime.db.SetCode(f.owner.address, []byte{0x60, 0x00, 0x60, 0x00, 0xfd}, tracing.CodeChangeUnspecified)
			})
			return nil
		}, nil, poolNativeRefundFailed},
		{"configuration", func(*poolTestChain) []string { return []string{"maximum-burn-rao", "0", "value-wei", "0"} }, nil, poolInvalidConfiguration},
		{"unauthorized", func(f *poolTestChain) []string { return []string{"safe", f.anyone.address.Hex()} }, nil, poolUnauthorized},
		{"unknown-operator", func(*poolTestChain) []string { return []string{"no-id", "2"} }, nil, poolUnknownOperator},
	} {
		f := newPoolTestChain(t)
		var changes []string
		if revert.prepare != nil {
			changes = revert.prepare(f)
		}
		f.setOverride(poolTestPreflightAnswer(revert.inject))
		stdout, code, diagnostic := f.run("review", changes...)
		requirePoolRefusal(t, revert.name, stdout, code, diagnostic, poolTestRefused(revert.want), poolTestNamedFailures)
	}
}

// Every named call failure, as review refuses it.
var poolTestNamedFailures = poolTestRefused(poolInactiveOperator, poolInvalidEpoch, poolCadenceTooShort, poolNativeRefundFailed, poolInvalidConfiguration, poolUnauthorized, poolUnknownOperator, poolOutOfFund)

// Replaces the node's answer to the preflight, the one eth_call with a sender.
func poolTestPreflightAnswer(failure *e2eRPCError) poolTestOverride {
	return func(_ int, method string, params []json.RawMessage, result any, original *e2eRPCError) (any, *e2eRPCError) {
		if from, _ := poolTestCall(method, params); from != nil && failure != nil {
			return nil, failure
		}
		return result, original
	}
}

// PR-6: a call the chain ran and failed is review's refusal or readback's
// difference, exit 3, whatever shape the node gives its answer; no rerun
// changes it. The shapes are subtensor's node's, as the rehearsal node gives
// them: a revert as code -32603 with the revert's bytes as hex data, none for
// a revert without a reason, and an EVM error such as OutOfFund with empty
// data; the same without the data; and other nodes' code 3. A revert is named
// by its selector and a Safe short of the value as OutOfFund; any other
// failure keeps the node's message and data. Only an error without that
// evidence is a failed read, exit 1.
func TestPoolRegistrationCallFailureShapes(t *testing.T) {
	f := newPoolTestChain(t)
	const reverted = "VM Exception while processing transaction: revert"
	unauthorized := hexutil.Encode(slices.Concat(stabi.STSettlement2OwnableUnauthorizedAccountErrorID().Bytes()[:4], common.LeftPadBytes(f.anyone.address[:], 32)))
	for _, shape := range []struct {
		name   string
		answer e2eRPCError
		code   int
		named  string
		text   string
	}{
		{"subtensor revert", e2eRPCError{Code: -32603, Message: reverted, Data: "0xc52a9bd3"}, 3, poolInvalidConfiguration, ""},
		{"subtensor revert with an argument", e2eRPCError{Code: -32603, Message: reverted, Data: unauthorized}, 3, poolUnauthorized, ""},
		{"subtensor unnamed revert", e2eRPCError{Code: -32603, Message: reverted, Data: "0xdeadbeef"}, 3, "", `the call failed with "VM Exception while processing transaction: revert" and data 0xdeadbeef`},
		{"subtensor revert below a selector", e2eRPCError{Code: -32603, Message: reverted, Data: "0xc52a9b"}, 3, "", `the call failed with "VM Exception while processing transaction: revert" and data 0xc52a9b`},
		{"subtensor revert without a reason", e2eRPCError{Code: -32603, Message: reverted, Data: "0x"}, 3, "", `the call failed with "VM Exception while processing transaction: revert" and data 0x`},
		{"subtensor revert without data", e2eRPCError{Code: -32603, Message: reverted}, 3, "", `the call failed with "VM Exception while processing transaction: revert" and data 0x`},
		{"subtensor OutOfFund", e2eRPCError{Code: -32603, Message: "evm error: OutOfFund", Data: "0x"}, 3, poolOutOfFund, ""},
		{"subtensor OutOfFund without data", e2eRPCError{Code: -32603, Message: "evm error: OutOfFund"}, 3, poolOutOfFund, ""},
		{"subtensor EVM error", e2eRPCError{Code: -32603, Message: "evm error: InvalidCode(Opcode(254))", Data: "0x"}, 3, "", `the call failed with "evm error: InvalidCode(Opcode(254))" and data 0x`},
		{"code 3 revert", e2eRPCError{Code: 3, Message: "execution reverted", Data: hexutil.Encode(stabi.STSettlement2CadenceTooShortErrorID().Bytes()[:4])}, 3, poolCadenceTooShort, ""},
		{"code 3 unnamed revert", e2eRPCError{Code: 3, Message: "execution reverted", Data: "0xdeadbeef"}, 3, "", `the call failed with "execution reverted" and data 0xdeadbeef`},
		{"code 3 without data", e2eRPCError{Code: 3, Message: "anything"}, 3, "", `the call failed with "anything" and data 0x`},
		{"read: header not found", e2eRPCError{Code: -32000, Message: "header not found"}, 1, "", "eth_call: RPC error -32000: header not found"},
		{"read: method not found", e2eRPCError{Code: -32601, Message: "Method not found"}, 1, "", "eth_call: RPC error -32601: Method not found"},
		{"read: internal error", e2eRPCError{Code: -32603, Message: "Failed to retrieve Runtime Api version"}, 1, "", "eth_call: RPC error -32603: Failed to retrieve Runtime Api version"},
		{"read: data that is no hex", e2eRPCError{Code: -32603, Message: "Failed to retrieve Runtime Api version", Data: "unavailable"}, 1, "", "eth_call: RPC error -32603: Failed to retrieve Runtime Api version"},
	} {
		f.setOverride(poolTestPreflightAnswer(&shape.answer))
		stdout, code, diagnostic := f.run("review")
		if shape.code == 3 {
			want := []string{}
			if shape.named != "" {
				want = poolTestRefused(shape.named)
			}
			requirePoolRefusal(t, shape.name, stdout, code, diagnostic, want, poolTestNamedFailures)
		}
		if code != shape.code || stdout != "" || !strings.Contains(diagnostic, shape.text) || strings.Contains(diagnostic, errPoolRefused.Error()) != (shape.code == 3) {
			t.Fatalf("%s: %d %s %s", shape.name, code, stdout, diagnostic)
		}
	}

	// A view the chain ran and failed, or answered as its contract would
	// not, is the mode's outcome too: review's refusal and readback's
	// difference.
	f.setOverride(nil)
	f.transaction = f.send(f.review()).TxHash
	for _, view := range []struct {
		name, mode string
		override   poolTestOverride
		changes    []string
		want       string
	}{
		{"review's reverted view", "review", poolTestAnswerAt(2, nil, &e2eRPCError{Code: -32603, Message: reverted, Data: "0x"}), nil, `registerOperatorPool refused: the call failed with "VM Exception while processing transaction: revert" and data 0x`},
		{"review's view of no contract", "review", poolTestAnswerAt(2, "0x", nil), nil, "registerOperatorPool refused: the call answered 0x, not as its contract does"},
		{"review's preflight of no contract", "review", poolTestAnswerAt(6, "0x", nil), nil, "registerOperatorPool refused: the preflight answered 0x, not the UID registerOperatorPool returns"},
		{"readback's reverted view", "readback", poolTestAnswerAt(5, nil, &e2eRPCError{Code: -32603, Message: reverted, Data: "0xdeadbeef"}), nil, `registerOperatorPool readback differs: the call failed with "VM Exception while processing transaction: revert" and data 0xdeadbeef`},
		{"readback's view of no contract", "readback", poolTestAnswerAt(4, "0x01", nil), nil, "registerOperatorPool readback differs: the call answered 0x01, not as its contract does"},
		{"readback of no v1 operator", "readback", nil, []string{"no-id", "2"}, "registerOperatorPool readback differs: " + poolUnknownOperator},
	} {
		f.takeMethods()
		f.setOverride(view.override)
		stdout, code, diagnostic := f.run(view.mode, view.changes...)
		if code != 3 || stdout != "" || !strings.Contains(diagnostic, view.want) {
			t.Fatalf("%s: %d %s %s", view.name, code, stdout, diagnostic)
		}
	}
}

// PR-4: readback names every difference between the chain and the reviewed
// registration: a transaction without a receipt or one that reverted, a
// receipt without vault2's PoolRegistered event for noId, or whose event has
// another hotkey or firstEpoch, vault2.pools(noId) unregistered or with
// another first epoch or hotkey, and settlementAt(noId, E_s) with another
// vault, hotkey or version. Each exits 3 with exactly its differences and
// prints nothing.
func TestPoolRegistrationReadbackNamesDifferences(t *testing.T) {
	f := newPoolTestChain(t)
	review := f.review()
	registered := f.send(review).TxHash
	data, _ := hexutil.Decode(review.Data)
	reverted := f.refused(f.anyone, f.settlement, e2eWei(1), data, "OwnableUnauthorizedAccount").TxHash
	unrelated := f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackConservationHolds()).TxHash
	all := poolTestTexts(errPoolNoReceipt, errPoolReverted, errPoolNoEvent, errPoolEventHotkey, errPoolEventFirstEpoch, errPoolUnregistered, errPoolFirstEpoch, errPoolHotkey, errPoolSettlementVault, errPoolSettlementHotkey, errPoolSettlementVersion)
	other := e2eKey("another-hotkey")
	word := func(value uint64) []byte { return common.LeftPadBytes(new(big.Int).SetUint64(value).Bytes(), 32) }
	pools := func(hotkey [32]byte, firstEpoch uint64, registered bool) string {
		flag := uint64(0)
		if registered {
			flag = 1
		}
		return hexutil.Encode(slices.Concat(hotkey[:], word(firstEpoch), word(flag)))
	}
	settlementAt := func(vault common.Address, hotkey [32]byte, version uint64) string {
		return hexutil.Encode(slices.Concat(common.LeftPadBytes(vault[:], 32), hotkey[:], word(version)))
	}
	// Replaces the answer of the view whose selector starts packed.
	view := func(packed []byte, answer string) poolTestOverride {
		return func(_ int, method string, params []json.RawMessage, result any, failure *e2eRPCError) (any, *e2eRPCError) {
			if _, data := poolTestCall(method, params); len(data) >= 4 && bytes.Equal(data[:4], packed[:4]) {
				return answer, nil
			}
			return result, failure
		}
	}
	// Edits a copy of the receipt's one log.
	receipt := func(edit func(*types.Log)) poolTestOverride {
		return func(_ int, method string, _ []json.RawMessage, result any, failure *e2eRPCError) (any, *e2eRPCError) {
			if method != "eth_getTransactionReceipt" {
				return result, failure
			}
			copied := *result.(*types.Receipt)
			log := *copied.Logs[0]
			log.Topics, log.Data = slices.Clone(log.Topics), slices.Clone(log.Data)
			edit(&log)
			copied.Logs = []*types.Log{&log}
			return &copied, nil
		}
	}
	poolsCall, settlementCall := e2eVault2Binding.PackPools(e2eRao(e2eNoID)), e2eSettlementBinding.PackSettlementAt(e2eRao(e2eNoID), e2eRao(2))
	for _, difference := range []struct {
		name        string
		transaction common.Hash
		override    poolTestOverride
		changes     []string
		want        []error
	}{
		{"no-receipt", common.Hash{9}, nil, nil, []error{errPoolNoReceipt}},
		{"reverted", reverted, nil, nil, []error{errPoolReverted}},
		{"no-event", unrelated, nil, nil, []error{errPoolNoEvent}},
		{"event-emitter", registered, receipt(func(log *types.Log) { log.Address = f.settlement }), nil, []error{errPoolNoEvent}},
		{"event-no-id", registered, receipt(func(log *types.Log) { log.Topics[1] = common.BigToHash(big.NewInt(2)) }), nil, []error{errPoolNoEvent}},
		{"event-malformed", registered, receipt(func(log *types.Log) { log.Data = log.Data[:32] }), nil, []error{errPoolNoEvent}},
		{"event-hotkey", registered, receipt(func(log *types.Log) { log.Topics[2] = other }), nil, []error{errPoolEventHotkey}},
		{"event-first-epoch", registered, receipt(func(log *types.Log) { log.Data[63] = 3 }), nil, []error{errPoolEventFirstEpoch}},
		{"unregistered", registered, view(poolsCall, pools(f.poolV2, 2, false)), nil, []error{errPoolUnregistered}},
		{"pool-first-epoch", registered, view(poolsCall, pools(f.poolV2, 3, true)), nil, []error{errPoolFirstEpoch}},
		{"pool-hotkey", registered, view(poolsCall, pools(other, 2, true)), nil, []error{errPoolHotkey}},
		{"settlement-vault", registered, view(settlementCall, settlementAt(f.vault1, f.poolV2, 2)), nil, []error{errPoolSettlementVault}},
		{"settlement-hotkey", registered, view(settlementCall, settlementAt(f.vault2, other, 2)), nil, []error{errPoolSettlementHotkey}},
		{"settlement-version", registered, view(settlementCall, settlementAt(f.vault2, f.poolV2, 1)), nil, []error{errPoolSettlementVersion}},
		{"later-epoch", registered, nil, []string{"first-epoch", "3"}, []error{errPoolEventFirstEpoch, errPoolFirstEpoch}},
		{"earlier-epoch", registered, nil, []string{"first-epoch", "1"}, []error{errPoolEventFirstEpoch, errPoolFirstEpoch, errPoolSettlementVault, errPoolSettlementHotkey, errPoolSettlementVersion}},
	} {
		f.transaction = difference.transaction
		f.setOverride(difference.override)
		stdout, code, diagnostic := f.run("readback", difference.changes...)
		requirePoolRefusal(t, difference.name, stdout, code, diagnostic, poolTestTexts(difference.want...), all)
	}
}

// A writer that always fails.
type poolTestBrokenWriter struct{}

func (poolTestBrokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic broken output")
}

// PR-5: only a failed read stops review and readback with exit 1 and no
// output: at each read in turn, an RPC error without evidence of a failed
// call, an HTTP refusal and a reply that is no JSON-RPC; and an output that
// cannot be written. The read profile refuses every other method, so neither
// command can send.
func TestPoolRegistrationReadFailuresStop(t *testing.T) {
	f := newPoolTestChain(t)
	// Readback reads a real registration; each failure stops review before
	// the preflight's own answer matters.
	f.transaction = f.send(f.review()).TxHash
	for _, mode := range []struct {
		name  string
		reads int
	}{{"review", 7}, {"readback", 6}} {
		for at := range mode.reads {
			for _, failure := range []struct {
				result  any
				failure *e2eRPCError
				message string
			}{
				{nil, &e2eRPCError{Code: -32601, Message: "synthetic unavailable method"}, "RPC error -32601: synthetic unavailable method"},
				{poolTestReply{http.StatusBadRequest, "synthetic refusal"}, nil, "HTTP 400"},
				{poolTestReply{http.StatusOK, "synthetic text"}, nil, errRpcIntegrity.Error()},
			} {
				f.takeMethods()
				f.setOverride(poolTestAnswerAt(at, failure.result, failure.failure))
				stdout, code, diagnostic := f.run(mode.name)
				if code != 1 || stdout != "" || !strings.Contains(diagnostic, failure.message) {
					t.Fatalf("%s read %d: %d %s %s", mode.name, at, code, stdout, diagnostic)
				}
			}
		}
	}
	f.setOverride(nil)
	var stderr bytes.Buffer
	if code := runRegisterOperatorPoolCommand(context.Background(), []string{"readback", "--rpc", f.url, "--settlement2", f.settlement.Hex(), "--no-id", "1", "--pool-hotkey", common.Hash(f.poolV2).Hex(), "--first-epoch", "2", "--transaction-hash", f.transaction.Hex()}, poolTestBrokenWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "register-operator-pool readback output: synthetic broken output") {
		t.Fatalf("broken output: %d %s", code, stderr.String())
	}
	f.takeMethods()
	client, err := newRpcClient(f.url, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.callPoolRegistrationRead(context.Background(), "eth_sendRawTransaction", []any{"0x00"}, new(string)); err == nil || err.Error() != "method is outside the pool registration read profile" || len(f.takeMethods()) != 0 {
		t.Fatalf("the read profile admitted a send: %v", err)
	}
}
