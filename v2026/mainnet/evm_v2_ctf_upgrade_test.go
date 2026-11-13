// The capture the flag upgrade review runs on the e2e chain in mainnet's
// configuration (CAPTURETHEFLAG.md §3.1, §11.2): pool 1 registered for v2 from
// epoch 2; the proxy upgraded to phase C through the pinned Safe 1.4.1 as the
// UP tests do; then owned by the reviewed timelock, with a 72 h minimum delay,
// the Safe its only proposer and canceller and execution open to anyone. The
// implementation is the forge build of STSettlement2Ctf, and one timelock
// operation upgrades the proxy to it and registers pool 1's CTF pool. The
// Safe's owner sends exactly the schedule review prints, and a stranger the
// execute transaction. The fake runtime's registration burns 0.5 TAO from
// vault2's balance; e2eBlockHash is the BLOCKHASH the chain serves.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// The registration the tests review: pool 1's CTF pool from epoch 3, with
// e2eBurnLimit as its burn ceiling, so the call's value is 1 TAO.
const (
	ctfTestFirstEpoch     = 3
	ctfTestMaximumBurnRao = e2eBurnLimit
)

var (
	ctfTestPoolId = new(big.Int).Add(new(big.Int).Lsh(common.Big1, 32), big.NewInt(e2eNoID))
	ctfTestValue  = new(big.Int).Mul(big.NewInt(ctfTestMaximumBurnRao), big.NewInt(e2eRaoWei))
	ctfTestBurn   = new(big.Int).Mul(big.NewInt(e2eBurnRao), big.NewInt(e2eRaoWei))
)

type ctfTestChain struct {
	*upgradeTestChain
	stranger *e2eActor
	timelock common.Address
	ctf      *e2eArtifact
	ctfPath  string
	// rpc is the chain's JSON-RPC with eth_getBalance.
	rpc string
}

// newCtfTestDeployment deploys the stack with pool 1 registered for v2 from
// epoch 2, the pinned Safe owning the proxy, still on phase A, and the
// reviewed timelock in mainnet's configuration; and writes the
// STSettlement2Ctf artifact for review.
func newCtfTestDeployment(t *testing.T) *ctfTestChain {
	t.Helper()
	safeOwner, stranger := newE2EActor("safe-owner"), newE2EActor("stranger")
	f := &ctfTestChain{upgradeTestChain: &upgradeTestChain{e2eFixture: newE2EFixture(t, e2eDefaultFirst, safeOwner, stranger), safeOwner: safeOwner}, stranger: stranger}
	f.installSafe(t)
	f.timelock = f.installTimelock(t, settlement2TimelockMinDelay, []common.Address{f.safe}, []common.Address{{}}, common.Address{})
	f.ctf = e2eForgeArtifact(t, "STSettlement2Ctf")
	f.ctfPath = filepath.Join(t.TempDir(), "STSettlement2Ctf.json")
	if err := os.WriteFile(f.ctfPath, f.ctf.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	f.rpc = ctfTestRpc(t, f.chain)
	return f
}

// newCtfTestChain is mainnet's configuration: the proxy upgraded to phase C
// through the Safe, then handed to the timelock.
func newCtfTestChain(t *testing.T) *ctfTestChain {
	t.Helper()
	f := newCtfTestDeployment(t)
	f.upgrade(t)
	f.handToTimelock(t, f.timelock)
	return f
}

// ctfTestRpc serves the e2e chain's JSON-RPC and answers eth_getBalance at a
// block, which the capture the flag reviews read for the timelock, the
// deterministic deployment proxy and the carry: the balance the EVM can
// spend, as Frontier answers it.
func ctfTestRpc(t *testing.T, chain *e2eChain) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		var call e2eRPCRequest
		if json.Unmarshal(body, &call) != nil || call.Method != "eth_getBalance" {
			request.Body = io.NopCloser(bytes.NewReader(body))
			chain.ServeHTTP(writer, request)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		chain.do(func() {
			var address common.Address
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &address) != nil {
				response["error"] = e2eInvalidParams(errors.New("an address and a block"))
				return
			}
			number, ok, err := chain.blockLocked(call.Params[1])
			if err != nil || !ok {
				response["error"] = e2eInvalidParams(fmt.Errorf("block %v", err))
				return
			}
			response["result"] = (*hexutil.Big)(chain.runtime.spendable(chain.stateLocked(number), address).ToBig())
		})
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// ctfReview runs the review of the capture the flag upgrade's schedule: the
// STSettlement2Ctf artifact, the timelock and the registration of pool 1's CTF
// pool from epoch 3. changes replace or add flags as review's do.
func (self *ctfTestChain) ctfReview(changes ...string) (settlement2UpgradeReview, int, string) {
	self.t.Helper()
	inputs := []string{"rpc", self.rpc, "artifact", self.ctfPath, "timelock", self.timelock.Hex(), "no-id", "1",
		"maximum-burn-rao", strconv.FormatUint(ctfTestMaximumBurnRao, 10), "first-epoch", strconv.Itoa(ctfTestFirstEpoch)}
	return self.review(append(inputs, changes...)...)
}

// refused requires a review refused with exit 3, naming want.
func (self *ctfTestChain) refused(name, want string, changes ...string) {
	self.t.Helper()
	if _, code, stderr := self.ctfReview(changes...); code != 3 || !strings.Contains(stderr, want) {
		self.t.Fatalf("%s: exit %d, want 3 naming %q\n%s", name, code, want, stderr)
	}
}

// ctfTestOperation is the reviewed operation, encoded from the forge
// artifacts' ABIs: registerCtfPool(1, 10^9, 3) as the call of
// upgradeToAndCall, with 1 TAO, and the timelock's id, schedule and execute
// of it.
type ctfTestOperation struct {
	implementation                       common.Address
	register, upgrade, schedule, execute []byte
	id                                   common.Hash
}

func (self *ctfTestChain) operation() ctfTestOperation {
	self.t.Helper()
	var operation ctfTestOperation
	operation.implementation = crypto.CreateAddress2(upgradeCreate2Factory, common.Hash{}, crypto.Keccak256(self.ctf.creation))
	timelockAbi := e2eForgeArtifact(self.t, "TimelockController").abi
	pack := func(packed []byte, packErr error) []byte {
		if packErr != nil {
			self.t.Fatal(packErr)
		}
		return packed
	}
	operation.register = pack(self.ctf.abi.Pack("registerCtfPool", big.NewInt(e2eNoID), uint64(ctfTestMaximumBurnRao), uint64(ctfTestFirstEpoch)))
	operation.upgrade = pack(self.phaseC.abi.Pack("upgradeToAndCall", operation.implementation, operation.register))
	operation.schedule = pack(timelockAbi.Pack("schedule", self.settlement, ctfTestValue, operation.upgrade, [32]byte{}, [32]byte{}, big.NewInt(settlement2TimelockMinDelay)))
	operation.execute = pack(timelockAbi.Pack("execute", self.settlement, ctfTestValue, operation.upgrade, [32]byte{}, [32]byte{}))
	hashed, err := self.chain.call(self.timelock, pack(timelockAbi.Pack("hashOperation", self.settlement, ctfTestValue, operation.upgrade, [32]byte{}, [32]byte{})))
	if err != nil {
		self.t.Fatal(err)
	}
	operation.id = common.BytesToHash(hashed)
	return operation
}

// fund sends the timelock wei in a plain transfer, as anyone may.
func (self *ctfTestChain) fund(wei *big.Int) {
	self.t.Helper()
	if receipt, revert := self.chain.send(self.anyone.key, &self.timelock, wei, nil); receipt.Status != types.ReceiptStatusSuccessful {
		self.t.Fatalf("funding the timelock reverted 0x%x", revert)
	}
}

// state is the timelock's getOperationState of id.
func (self *ctfTestChain) state(id common.Hash) string {
	self.t.Helper()
	query, err := settlement2TimelockAbi.Pack("getOperationState", id)
	if err != nil {
		self.t.Fatal(err)
	}
	out, err := self.chain.call(self.timelock, query)
	if err != nil {
		self.t.Fatal(err)
	}
	return settlement2TimelockStates[new(big.Int).SetBytes(out).Uint64()]
}

// schedule creates the implementation, funds the timelock with the call's
// value and has the Safe's owner send the reviewed schedule, then rolls to
// the first block in which the operation is ready.
func (self *ctfTestChain) schedule(operation ctfTestOperation) {
	self.t.Helper()
	before, code, stderr := self.ctfReview()
	if code != 0 {
		self.t.Fatalf("review before the creation: exit %d\n%s", code, stderr)
	}
	self.must(self.anyone, upgradeCreate2Factory, nil, hexutil.MustDecode(before.Implementation.CreateData))
	self.fund(ctfTestValue)
	review, code, stderr := self.ctfReview()
	if code != 0 || !review.Timelock.CallPreflight.Ran || review.SafeTx.Data != hexutil.Encode(operation.schedule) {
		self.t.Fatalf("schedule review: exit %d %+v\n%s", code, review.Timelock, stderr)
	}
	receipt := self.must(self.safeOwner, self.safe, nil, hexutil.MustDecode(review.OwnerTx.Data))
	self.requireLog(receipt, e2eLog{address: self.timelock, abi: &e2eForgeArtifact(self.t, "TimelockController").abi, name: "CallScheduled",
		args: []any{operation.id, new(big.Int), self.settlement, ctfTestValue, operation.upgrade, [32]byte{}, big.NewInt(settlement2TimelockMinDelay)}})
	self.chain.rollTo(timelockReadyBlock(e2eBlockTime(receipt.BlockNumber.Uint64())+settlement2TimelockMinDelay) + 1)
	if state := self.state(operation.id); state != "ready" {
		self.t.Fatalf("the operation is %s after the delay", state)
	}
}

// ctfHotkey is registerCtfPool's hotkey for pool 1's CTF pool in block
// number: keccak256(abi.encode(proxy, 2^32 + 1, number, blockhash(number - 1))).
func (self *ctfTestChain) ctfHotkey(number uint64) common.Hash {
	return crypto.Keccak256Hash(common.LeftPadBytes(self.settlement.Bytes(), 32), common.BigToHash(ctfTestPoolId).Bytes(),
		common.BigToHash(new(big.Int).SetUint64(number)).Bytes(), e2eBlockHash(number-1).Bytes())
}

// epochStart is the v1 coordinator's epochStartBlock(epoch).
func (self *ctfTestChain) epochStart(epoch uint64) uint64 {
	self.t.Helper()
	return e2eView(self.e2eFixture, self.coordinator, e2eCoordinatorBinding.PackEpochStartBlock(new(big.Int).SetUint64(epoch)), e2eCoordinatorBinding.UnpackEpochStartBlock).Uint64()
}

// implementation is the proxy's ERC-1967 implementation.
func (self *ctfTestChain) implementation() common.Address {
	return common.BytesToAddress(self.chain.wordAt(self.settlement, common.HexToHash(contractImplementationSlot)).Bytes())
}

// ctfPool is vault2's record of pool 1's CTF pool.
func (self *ctfTestChain) ctfPool() (common.Hash, uint64, bool) {
	self.t.Helper()
	pool := e2eView(self.e2eFixture, self.vault2, e2eVault2Binding.PackPools(ctfTestPoolId), e2eVault2Binding.UnpackPools)
	return common.Hash(pool.Hotkey), pool.FirstEpoch, pool.Registered
}

// requireUnchanged checks that a failed execution left the proxy on phase C,
// vault2 without the CTF pool, the operation ready and the timelock's balance
// whole.
func (self *ctfTestChain) requireUnchanged(phaseC common.Address, id common.Hash, balance *big.Int) {
	self.t.Helper()
	_, _, registered := self.ctfPool()
	if current := self.implementation(); current != phaseC || registered || self.state(id) != "ready" || self.chain.balance(self.timelock).Cmp(balance) != 0 {
		self.t.Fatalf("after a failed execution: implementation %s, CTF pool registered %t, operation %s, timelock balance %s", current, registered, self.state(id), self.chain.balance(self.timelock))
	}
}

// UP-8: the capture the flag upgrade goes out as one timelock operation in
// mainnet's configuration. Before the implementation exists, review prints
// the folded call, upgradeToAndCall(implementation, registerCtfPool(1, 10^9,
// 3)) with 1 TAO, its schedule and execute, and the ctf section, with neither
// preflight run; the implementation created through the factory has exactly
// the reviewed code; review then refuses until the timelock holds the value,
// naming the shortfall, and preflights the call from the timelock with it. The
// Safe's owner schedules exactly as printed; while the operation waits a
// second schedule and an early execute are refused, and a stranger's execute
// reverts. After 72 h the execute step's review preflights the execute printed
// at the schedule, which a stranger sends: the proxy runs STSettlement2Ctf
// with slots 0 to 50 unchanged, vault2 holds pool 2^32 + 1 from epoch 3 under
// the hotkey derived from the proxy, the pool id, the execution block and its
// parent's hash, CallExecuted names the value, the timelock keeps the value
// less the 0.5 TAO burn, and ctfSettlementAt resolves the pool. A review after
// it is refused.
func TestCtfUpgradeThroughTheTimelock(t *testing.T) {
	f := newCtfTestChain(t)
	phaseC := f.implementation()
	operation := f.operation()
	start := f.epochStart(ctfTestFirstEpoch)

	// The phase C artifact's review is the phase C upgrade, which the proxy
	// already runs; the capture the flag artifact's prints the folded call.
	if _, code, stderr := f.review("timelock", f.timelock.Hex()); code != 3 || !strings.Contains(stderr, "the proxy already runs the phase C implementation") {
		t.Fatalf("the phase C review: exit %d\n%s", code, stderr)
	}
	before, code, stderr := f.ctfReview()
	if code != 0 || before.Schema != settlement2CtfUpgradeReviewSchema || before.Implementation.Created || before.Preflight.Ran || before.Timelock.CallPreflight.Ran ||
		!strings.HasPrefix(before.Preflight.Result, "not run: the implementation does not exist yet and the timelock holds less than the call's value; create it, send the timelock at least 1000000000000000000 wei") ||
		before.Implementation.Address != operation.implementation.Hex() || before.Implementation.CreateData != hexutil.Encode(append(common.Hash{}.Bytes(), f.ctf.creation...)) ||
		before.Settlement2.Owner != f.timelock.Hex() || before.Settlement2.CurrentImplementation != phaseC.Hex() {
		t.Fatalf("review before the creation: exit %d %+v\n%s", code, before, stderr)
	}
	want := settlement2CtfUpgrade{NoId: 1, PoolId: "4294967297", MaximumBurnRao: "1000000000", ValueWei: "1000000000000000000", FirstEpoch: ctfTestFirstEpoch,
		FirstEpochStartBlock: start, BlocksRemaining: start - before.HeadBlock, CurrentEpoch: 0, Vault2: f.vault2.Hex(), V1Coordinator: f.coordinator.Hex(),
		ProviderPoolHotkey: common.Hash(f.poolV2).Hex(), TimelockBalanceWei: "0", HotkeyDerivation: "keccak256(abi.encode(proxy, pool_id, block.number, blockhash(block.number - 1))) at execution"}
	if before.Ctf == nil || *before.Ctf != want {
		t.Fatalf("ctf section: %+v, want %+v", before.Ctf, want)
	}
	section := before.Timelock
	if section.Step != "schedule" || section.OperationId != operation.id.Hex() || section.OperationState != "unset" || section.Executor != "open" ||
		section.Call.Target != f.settlement.Hex() || section.Call.ValueWei != "1000000000000000000" || section.Call.Data != hexutil.Encode(operation.upgrade) ||
		section.Call.Function != "upgradeToAndCall(address,bytes)" || section.Call.Args["newImplementation"] != operation.implementation.Hex() ||
		section.Call.Args["data"] != hexutil.Encode(operation.register) ||
		section.EarliestExecutionUnix != new(big.Int).SetUint64(e2eBlockTime(before.HeadBlock)+settlement2TimelockMinDelay).String() ||
		section.ExecuteTx.To != f.timelock.Hex() || section.ExecuteTx.ValueWei != "0" || section.ExecuteTx.Data != hexutil.Encode(operation.execute) ||
		section.ExecuteTx.DataKeccak != crypto.Keccak256Hash(operation.execute).Hex() {
		t.Fatalf("timelock section: %+v", section)
	}
	if before.Safe == nil || before.Safe.Nonce != "2" || before.SafeTx.To != f.timelock.Hex() || before.SafeTx.ValueWei != "0" || before.SafeTx.Data != hexutil.Encode(operation.schedule) ||
		before.SafeTx.Function != "schedule(address,uint256,bytes,bytes32,bytes32,uint256)" || before.SafeTx.Args["value"] != "1000000000000000000" ||
		before.SafeTx.Args["data"] != hexutil.Encode(operation.upgrade) || before.SafeTx.Args["delay"] != "259200" ||
		before.SafeTx.SafeTxHash != f.safeHash(before).Hex() || before.OwnerTx.From != f.safeOwner.address.Hex() || before.OwnerTx.To != f.safe.Hex() {
		t.Fatalf("Safe transaction: %+v", before.SafeTx)
	}

	// Anyone creates the implementation through the factory; review then
	// requires the timelock to hold the value.
	f.must(f.anyone, upgradeCreate2Factory, nil, hexutil.MustDecode(before.Implementation.CreateData))
	if created := f.chain.codeAt(operation.implementation); crypto.Keccak256Hash(created).Hex() != before.Implementation.RuntimeCodeHash || len(created) != before.Implementation.RuntimeBytes {
		t.Fatalf("created code hash %s, reviewed %s", crypto.Keccak256Hash(created), before.Implementation.RuntimeCodeHash)
	}
	f.refused("an unfunded timelock", "the timelock "+f.timelock.Hex()+" holds 0 wei, less than the call's value of 1000000000000000000 wei, which it pays from its own balance at execution: send it at least 1000000000000000000 wei first")
	f.fund(new(big.Int).Sub(ctfTestValue, common.Big1))
	f.refused("a timelock 1 wei short", "holds 999999999999999999 wei, less than the call's value of 1000000000000000000 wei, which it pays from its own balance at execution: send it at least 1 wei first")
	f.fund(common.Big1)

	// The schedule, as printed.
	after, code, stderr := f.ctfReview()
	if code != 0 || !after.Implementation.Created || !after.Preflight.Ran || !after.Timelock.CallPreflight.Ran ||
		after.Timelock.CallPreflight.Result != "the call from the timelock, with its value, succeeds at the head" ||
		after.SafeTx.SafeTxHash != before.SafeTx.SafeTxHash || after.OwnerTx.Data != before.OwnerTx.Data || after.Ctf.TimelockBalanceWei != "1000000000000000000" {
		t.Fatalf("review after the creation: exit %d %+v %+v\n%s", code, after, after.Timelock, stderr)
	}
	receipt := f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(after.OwnerTx.Data))
	timelockAbi := e2eForgeArtifact(t, "TimelockController").abi
	f.requireLog(receipt, e2eLog{address: f.timelock, abi: &timelockAbi, name: "CallScheduled",
		args: []any{operation.id, new(big.Int), f.settlement, ctfTestValue, operation.upgrade, [32]byte{}, big.NewInt(settlement2TimelockMinDelay)}})
	ready := e2eBlockTime(receipt.BlockNumber.Uint64()) + settlement2TimelockMinDelay

	// While it waits, neither step passes, and a stranger cannot execute.
	f.refused("a second schedule", "the operation "+operation.id.Hex()+" is already scheduled, ready at "+timelockUtc(ready))
	f.refused("an early execute", "the operation "+operation.id.Hex()+" is not ready until "+timelockUtc(ready), "execute", "true")
	if failed, revert := f.chain.send(f.stranger.key, &f.timelock, nil, operation.execute); failed.Status != types.ReceiptStatusFailed || f.timelockRevert(revert) != "TimelockUnexpectedOperationState" {
		t.Fatalf("an early execute on chain: status %d revert %s", failed.Status, f.timelockRevert(revert))
	}

	// After the delay, a stranger sends exactly the printed execute.
	f.chain.rollTo(timelockReadyBlock(ready) + 1)
	execute, code, stderr := f.ctfReview("execute", "true")
	if code != 0 || execute.Schema != settlement2CtfUpgradeReviewSchema || execute.Timelock.Step != "execute" || execute.Timelock.OperationState != "ready" ||
		execute.Timelock.EarliestExecutionUnix != strconv.FormatUint(ready, 10) || execute.Timelock.ExecuteTx != section.ExecuteTx ||
		!execute.Timelock.CallPreflight.Ran || !execute.Preflight.Ran || execute.Preflight.Result != "execute from any sender succeeds at the head" ||
		execute.Safe != nil || execute.SafeTx != nil || execute.OwnerTx != nil || execute.Ctf.CurrentEpoch != 1 || execute.Ctf.BlocksRemaining != start-execute.HeadBlock {
		t.Fatalf("execute review: exit %d %+v %+v\n%s", code, execute, execute.Timelock, stderr)
	}
	slots := make([]common.Hash, 51)
	for i := range slots {
		slots[i] = f.chain.wordAt(f.settlement, common.BigToHash(big.NewInt(int64(i))))
	}
	receipt = f.must(f.stranger, f.timelock, nil, hexutil.MustDecode(execute.Timelock.ExecuteTx.Data))
	executed := receipt.BlockNumber.Uint64()
	hotkey := f.ctfHotkey(executed)
	t.Logf("execute(upgradeToAndCall(registerCtfPool)): gas %d", receipt.GasUsed)

	// The proxy runs the implementation with phase C's slots kept, and vault2
	// holds the CTF pool under the hotkey derived at execution.
	f.requireLog(receipt, e2eLog{address: f.timelock, abi: &timelockAbi, name: "CallExecuted", args: []any{operation.id, new(big.Int), f.settlement, ctfTestValue, operation.upgrade}})
	if !f.logged(receipt, f.settlement, f.ctf.abi.Events["Upgraded"].ID, common.BytesToHash(operation.implementation.Bytes())) || f.implementation() != operation.implementation {
		t.Fatalf("the execute did not upgrade the proxy: it runs %s", f.implementation())
	}
	for i := range slots {
		if f.chain.wordAt(f.settlement, common.BigToHash(big.NewInt(int64(i)))) != slots[i] {
			t.Fatalf("slot %d changed in the upgrade", i)
		}
	}
	var uid uint16
	var registered bool
	var owner common.Hash
	f.chain.do(func() {
		f.chain.runtime.db = f.chain.stateLocked(f.chain.head)
		uid, registered = f.chain.runtime.uid(hotkey)
		owner = f.chain.runtime.db.GetState(e2eNeuron, e2eRuntimeSlot("owner", hotkey))
	})
	if !registered || owner != common.Hash(f.vault2Coldkey) {
		t.Fatalf("the runtime registered hotkey %s: %t, to %s", hotkey, registered, owner)
	}
	f.requireLog(receipt, e2eLog{address: f.vault2, abi: &f.vault2ABI, name: "PoolRegistered", args: []any{ctfTestPoolId, hotkey, uid, uint64(ctfTestFirstEpoch)}})
	if poolHotkey, firstEpoch, ok := f.ctfPool(); !ok || poolHotkey != hotkey || firstEpoch != ctfTestFirstEpoch {
		t.Fatalf("vault2.pools(2^32 + 1): %s from %d, registered %t; want %s from %d", poolHotkey, firstEpoch, ok, hotkey, ctfTestFirstEpoch)
	}
	provider := e2eView(f.e2eFixture, f.vault2, e2eVault2Binding.PackPools(big.NewInt(e2eNoID)), e2eVault2Binding.UnpackPools)
	if !provider.Registered || provider.Hotkey != f.poolV2 || provider.FirstEpoch != f.firstEpoch {
		t.Fatalf("the provider pool changed: %+v", provider)
	}

	// The timelock paid the value and got the burn's surplus back; neither the
	// proxy nor vault2 keeps any.
	if balance := f.chain.balance(f.timelock); balance.Cmp(new(big.Int).Sub(ctfTestValue, ctfTestBurn)) != 0 || f.chain.balance(f.settlement).Sign() != 0 || f.chain.balance(f.vault2).Sign() != 0 {
		t.Fatalf("balances: timelock %s, proxy %s, vault2 %s", balance, f.chain.balance(f.settlement), f.chain.balance(f.vault2))
	}
	at := e2eView(f.e2eFixture, f.settlement, e2eSettlementBinding.PackCtfSettlementAt(big.NewInt(e2eNoID), big.NewInt(ctfTestFirstEpoch)), e2eSettlementBinding.UnpackCtfSettlementAt)
	early := e2eView(f.e2eFixture, f.settlement, e2eSettlementBinding.PackCtfSettlementAt(big.NewInt(e2eNoID), big.NewInt(ctfTestFirstEpoch-1)), e2eSettlementBinding.UnpackCtfSettlementAt)
	poolId := e2eView(f.e2eFixture, f.settlement, e2eSettlementBinding.PackCtfPoolId(big.NewInt(e2eNoID)), e2eSettlementBinding.UnpackCtfPoolId)
	if at.Vault != f.vault2 || common.Hash(at.Hotkey) != hotkey || at.FirstEpoch != ctfTestFirstEpoch || early.Vault != (common.Address{}) || early.FirstEpoch != 0 || poolId.Cmp(ctfTestPoolId) != 0 {
		t.Fatalf("ctfSettlementAt(1, 3) %+v, (1, 2) %+v, ctfPoolId(1) %s", at, early, poolId)
	}
	if state := f.state(operation.id); state != "done" {
		t.Fatalf("the operation is %s after the execute", state)
	}
	f.refused("a review after the upgrade", "the proxy already runs the capture the flag implementation", "execute", "true")
	f.refused("a schedule after the upgrade", "the proxy already runs the capture the flag implementation")
	// Nor can the phase C artifact's review downgrade it.
	if _, code, stderr := f.review("timelock", f.timelock.Hex()); code != 3 || !strings.Contains(stderr, "the proxy runs the capture the flag implementation") {
		t.Fatalf("a phase C review after the upgrade: exit %d\n%s", code, stderr)
	}
	f.requireConservation()
}

// UP-9: the capture the flag review refuses, exit 3, what registerCtfPool
// would refuse at execution, read at the head through the proxy's vault() and
// v1Coordinator(): a proxy that does not run phase C, an operator without a
// provider pool in vault2, a CTF pool vault2 already holds, a first epoch
// whose cadence is too short, a first epoch that starts at the next block or
// has started, and an operator inactive in the current epoch. At the schedule
// step it refuses an operation that would be ready only at or after the first
// epoch's estimated start, the head's time plus its remaining blocks at 12 s;
// and, once the call's preflight runs, a timelock that holds less than the
// value.
func TestCtfUpgradeReviewRefusals(t *testing.T) {
	f := newCtfTestDeployment(t)

	// The proxy, on phase A, owned by the timelock.
	f.installOwner(f.timelock)
	f.refused("a proxy on phase A", "the proxy does not run the phase C implementation, on which the capture the flag upgrade is built")
	f.installOwner(f.safe)
	f.upgrade(t)
	f.handToTimelock(t, f.timelock)

	f.refused("no provider pool", "vault2 has no provider pool 2, which registerCtfPool requires (UnknownPool)", "no-id", "2")

	// A CTF pool vault2 already holds, written as registerPool leaves it:
	// vault2's _pools mapping is at slot 2, and a PoolState keeps its hotkey in
	// its first word and its first epoch at byte 16 of its third.
	held := crypto.Keccak256Hash([]byte("held CTF pool hotkey"))
	record := crypto.Keccak256Hash(common.BigToHash(ctfTestPoolId).Bytes(), common.BigToHash(big.NewInt(2)).Bytes())
	install := func(hotkey common.Hash, firstEpoch uint64) {
		f.chain.do(func() {
			db := f.chain.stateLocked(f.chain.head)
			db.SetState(f.vault2, record, hotkey)
			db.SetState(f.vault2, common.BigToHash(new(big.Int).Add(record.Big(), big.NewInt(2))), common.BigToHash(new(big.Int).Lsh(new(big.Int).SetUint64(firstEpoch), 128)))
			db.Finalise(true)
			f.chain.commitLocked(db, f.chain.head+1, nil, nil)
		})
	}
	install(held, 7)
	if hotkey, firstEpoch, ok := f.ctfPool(); !ok || hotkey != held || firstEpoch != 7 {
		t.Fatalf("the written CTF pool reads %s from %d, registered %t", hotkey, firstEpoch, ok)
	}
	f.refused("a held CTF pool", "vault2 already holds the CTF pool 4294967297, 2^32 + 1, from epoch 7 under hotkey "+held.Hex())
	install(common.Hash{}, 0)

	// A first epoch whose policy's epochs are shorter than the commit window
	// plus vault2's challenge window.
	short := f.policy(e2eCommitWindow+e2eWindow-1, e2eWindow)
	short.EffectiveEpoch = 5
	f.must(f.owner, f.coordinator, nil, e2eCoordinatorBinding.PackSchedulePolicy(short))
	f.refused("a short cadence", "the first epoch's v1 policy has 15599-block epochs, shorter than its 1200-block commit window plus vault2's 14400-block challenge window (CadenceTooShort)", "first-epoch", "5")

	// Epoch 1, 7,200 blocks after the coordinator's deployment, starts sooner
	// than 72 h after the head.
	epoch1 := f.epochStart(1)
	if _, code, stderr := f.ctfReview("first-epoch", "1"); code != 3 || !strings.Contains(stderr, fmt.Sprintf("at or after the first epoch 1's estimated start at %s (block %d, ", timelockUtc(e2eBlockTime(epoch1)), epoch1)) ||
		!strings.Contains(stderr, "at 12 s each), when registerCtfPool reverts InvalidEpoch: schedule it sooner or choose a later first epoch") {
		t.Fatalf("a schedule ready after epoch 1 starts: exit %d\n%s", code, stderr)
	}

	// Once the implementation exists, the call's preflight needs the value in
	// the timelock.
	before, code, stderr := f.ctfReview()
	if code != 0 {
		t.Fatalf("review before the creation: exit %d\n%s", code, stderr)
	}
	f.must(f.anyone, upgradeCreate2Factory, nil, hexutil.MustDecode(before.Implementation.CreateData))
	f.refused("an unfunded timelock", "the timelock "+f.timelock.Hex()+" holds 0 wei, less than the call's value of 1000000000000000000 wei, which it pays from its own balance at execution: send it at least 1000000000000000000 wei first")
	f.fund(ctfTestValue)

	// Epoch 2 starts 21,600 blocks at 12 s, the 72 h delay, after block last:
	// a schedule at the head before it passes, and one at that head is ready
	// only as the epoch starts.
	epoch2 := f.epochStart(2)
	last := epoch2 - settlement2TimelockMinDelay/settlement2BlockSeconds
	f.chain.rollTo(last)
	if review, code, stderr := f.ctfReview("first-epoch", "2"); code != 0 || review.Ctf.BlocksRemaining != epoch2-last+1 || !review.Timelock.CallPreflight.Ran {
		t.Fatalf("a schedule ready a block before epoch 2 starts: exit %d %+v\n%s", code, review.Ctf, stderr)
	}
	f.chain.rollTo(last + 1)
	f.refused("a schedule ready as epoch 2 starts", "the operation is ready at "+timelockUtc(e2eBlockTime(last)+settlement2TimelockMinDelay)+" at the earliest, at or after the first epoch 2's estimated start at "+timelockUtc(e2eBlockTime(epoch2))+fmt.Sprintf(" (block %d, 21600 blocks after the head at 12 s each)", epoch2), "first-epoch", "2")

	// An execution lands at the next block at the earliest.
	f.chain.rollTo(epoch2)
	f.refused("a first epoch starting at the next block", fmt.Sprintf("the first epoch 2 starts at block %d, the next block: no execution lands before it, and registerCtfPool would revert InvalidEpoch", epoch2), "first-epoch", "2")
	f.chain.rollTo(epoch2 + 1)
	f.refused("a first epoch that has started", "the first epoch 2 has started: the current epoch is 2, and registerCtfPool requires a later first epoch (InvalidEpoch)", "first-epoch", "2")
	f.refused("a first epoch that has passed", "the first epoch 1 has started: the current epoch is 2", "first-epoch", "1")

	// Operator 1 inactive from epoch 3.
	f.must(f.owner, f.coordinator, nil, e2eCoordinatorBinding.PackScheduleOperator(big.NewInt(e2eNoID), f.deposit, f.depositSigner.address, f.operator.address, false, 3))
	f.chain.rollTo(f.epochStart(3) + 1)
	f.refused("an inactive operator", "the v1 operator 1 is not active in the current epoch 3 (InactiveOperator)", "first-epoch", "4")
}

// UP-10: review rejects, exit 2 and before any read, the capture the flag
// artifact without --no-id, --maximum-burn-rao, --first-epoch or --timelock;
// any of those three flags with the phase C artifact; a no_id of 0 or from
// 2^32, a zero burn ceiling or first epoch, and a malformed value; and an
// artifact that declares only some of the capture the flag functions.
func TestCtfUpgradeReviewUsage(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Errorf("a usage error read the RPC")
		http.Error(writer, "no read is expected", http.StatusInternalServerError)
	}))
	defer rpc.Close()
	directory := t.TempDir()
	write := func(name string, raw []byte) string {
		t.Helper()
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	ctfRaw := e2eForgeArtifact(t, "STSettlement2Ctf").raw
	var artifact map[string]any
	if err := json.Unmarshal(ctfRaw, &artifact); err != nil {
		t.Fatal(err)
	}
	artifact["abi"] = slices.DeleteFunc(artifact["abi"].([]any), func(entry any) bool { return entry.(map[string]any)["name"] == "ctfPoolId" })
	partialRaw, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	ctf, phaseC, partial := write("ctf.json", ctfRaw), write("phase-c.json", e2eForgeArtifact(t, "STSettlement2PhaseC").raw), write("partial.json", partialRaw)
	// Any nonzero addresses: nothing is read.
	settlement, safe, timelock := common.HexToAddress("0x5e771e2000000000000000000000000000000001"), common.HexToAddress("0x5afe000000000000000000000000000000000002"), common.HexToAddress("0x71e10c0000000000000000000000000000000003")
	run := func(artifact string, flags ...string) (int, string) {
		t.Helper()
		args := append([]string{"review", "--rpc", rpc.URL, "--settlement2", settlement.Hex(), "--safe", safe.Hex(), "--salt", common.Hash{}.Hex(), "--artifact", artifact}, flags...)
		var stdout, stderr bytes.Buffer
		code := runUpgradeSettlement2Command(context.Background(), args, &stdout, &stderr)
		if stdout.Len() != 0 {
			t.Fatalf("%v printed %s", flags, stdout.String())
		}
		return code, stderr.String()
	}
	registration := map[string]string{"--timelock": timelock.Hex(), "--no-id": "1", "--maximum-burn-rao": "1000000000", "--first-epoch": "8"}
	without := func(dropped string, replaced ...string) []string {
		var flags []string
		for _, name := range []string{"--timelock", "--no-id", "--maximum-burn-rao", "--first-epoch"} {
			if name != dropped {
				flags = append(flags, name, registration[name])
			}
		}
		return append(flags, replaced...)
	}
	const missing = "the STSettlement2Ctf artifact's upgrade registers a CTF pool in the same timelock operation: it requires --no-id, --maximum-burn-rao, --first-epoch and --timelock"
	const outOfRange = "--no-id is 1 to 2^32 - 1, and --maximum-burn-rao and --first-epoch are positive decimal integers"
	for _, usage := range []struct {
		name, artifact, want string
		flags                []string
	}{
		{"no --no-id", ctf, missing, without("--no-id")},
		{"no --maximum-burn-rao", ctf, missing, without("--maximum-burn-rao")},
		{"no --first-epoch", ctf, missing, without("--first-epoch")},
		{"no --timelock", ctf, missing, without("--timelock")},
		{"no registration", ctf, missing, []string{"--timelock", timelock.Hex()}},
		{"--no-id with phase C", phaseC, "--no-id, --maximum-burn-rao and --first-epoch go only with the STSettlement2Ctf artifact", []string{"--timelock", timelock.Hex(), "--no-id", "1"}},
		{"the registration with phase C", phaseC, "go only with the STSettlement2Ctf artifact", without("")},
		{"--first-epoch with phase C through the Safe", phaseC, "go only with the STSettlement2Ctf artifact", []string{"--first-epoch", "8"}},
		{"no_id 0", ctf, outOfRange, without("", "--no-id", "0")},
		{"no_id 2^32", ctf, outOfRange, without("", "--no-id", "4294967296")},
		{"a zero burn ceiling", ctf, outOfRange, without("", "--maximum-burn-rao", "0")},
		{"a zero first epoch", ctf, outOfRange, without("", "--first-epoch", "0")},
		{"a malformed no_id", ctf, "invalid value", without("", "--no-id", "one")},
		{"a negative burn ceiling", ctf, "invalid value", without("", "--maximum-burn-rao", "-1")},
		{"a partial artifact", partial, "it is neither the phase C nor the capture the flag implementation", without("")},
	} {
		if code, stderr := run(usage.artifact, usage.flags...); code != 2 || !strings.Contains(stderr, usage.want) {
			t.Fatalf("%s: exit %d, want 2 naming %q\n%s", usage.name, code, usage.want, stderr)
		}
	}
	artifactKind, err := loadUpgradeArtifact(ctfRaw)
	if err != nil || !artifactKind.ctf || artifactKind.kind() != "capture the flag" {
		t.Fatalf("the STSettlement2Ctf artifact loads as %q: %v", artifactKind.kind(), err)
	}
	if artifactKind, err = loadUpgradeArtifact(e2eForgeArtifact(t, "STSettlement2PhaseC").raw); err != nil || artifactKind.ctf {
		t.Fatalf("the phase C artifact loads as %q: %v", artifactKind.kind(), err)
	}
}

// UP-11: a failed execution changes nothing and leaves the operation ready.
// When the runtime refuses the registration, here because someone registered
// the hotkey the execution block derives first, the stranger's execute
// reverts: the proxy stays on phase C, vault2 holds no CTF pool and the
// timelock keeps the value. Review at that head refuses the execute step, its
// call preflight deriving the taken hotkey; a block later it passes, and the
// execute registers the pool under that block's hotkey. On a second chain,
// once the first epoch is reached, review refuses the execute step, the
// stranger's execute reverts InvalidEpoch and the operation stays ready, with
// the proxy on phase C.
func TestCtfUpgradeFailedExecutionStaysReady(t *testing.T) {
	t.Run("hotkey taken", func(t *testing.T) {
		f := newCtfTestChain(t)
		phaseC := f.implementation()
		operation := f.operation()
		f.schedule(operation)

		// In the next block someone registers, under another coldkey, the
		// hotkey that the block after it derives, where the stranger's execute
		// lands. The runtime refuses vault2's registration, and the delegate
		// call's empty revert reaches the timelock as FailedCall.
		landing := f.chain.next() + 1
		taken := f.ctfHotkey(landing)
		f.chain.native(func(rt *e2eRuntime) {
			rt.setWord(e2eNeuron, e2eRuntimeSlot("uid", taken), big.NewInt(200))
			rt.db.SetState(e2eNeuron, e2eRuntimeSlot("owner", taken), common.Hash(e2eKey("front-runner")))
		})
		failed, revert := f.chain.send(f.stranger.key, &f.timelock, nil, operation.execute)
		if failed.Status != types.ReceiptStatusFailed || failed.BlockNumber.Uint64() != landing || f.timelockRevert(revert) != "FailedCall" {
			t.Fatalf("the execute under a taken hotkey: status %d in block %d, revert %s; want FailedCall in %d", failed.Status, failed.BlockNumber, f.timelockRevert(revert), landing)
		}
		f.requireUnchanged(phaseC, operation.id, ctfTestValue)
		if _, code, stderr := f.ctfReview("execute", "true"); code != 3 || !strings.Contains(stderr, "the call from the timelock: ") ||
			!strings.Contains(stderr, fmt.Sprintf("call 0x%x to %s with 1000000000000000000 wei failed", operation.upgrade[:4], f.settlement.Hex())) {
			t.Fatalf("the execute review at the head whose hotkey is taken: exit %d\n%s", code, stderr)
		}

		// A block later the hotkey differs, and the same execute registers.
		f.chain.rollTo(f.chain.next() + 1)
		if _, code, stderr := f.ctfReview("execute", "true"); code != 0 {
			t.Fatalf("the execute review a block later: exit %d\n%s", code, stderr)
		}
		receipt := f.must(f.stranger, f.timelock, nil, operation.execute)
		hotkey := f.ctfHotkey(receipt.BlockNumber.Uint64())
		if poolHotkey, firstEpoch, ok := f.ctfPool(); !ok || poolHotkey != hotkey || poolHotkey == taken || firstEpoch != ctfTestFirstEpoch || f.implementation() != operation.implementation {
			t.Fatalf("the retried execute: pool %s from %d, registered %t, proxy on %s", poolHotkey, firstEpoch, ok, f.implementation())
		}
		if f.state(operation.id) != "done" || f.chain.balance(f.timelock).Cmp(new(big.Int).Sub(ctfTestValue, ctfTestBurn)) != 0 {
			t.Fatalf("after the retry: operation %s, timelock balance %s", f.state(operation.id), f.chain.balance(f.timelock))
		}
		f.requireConservation()
	})

	t.Run("first epoch reached", func(t *testing.T) {
		f := newCtfTestChain(t)
		phaseC := f.implementation()
		operation := f.operation()
		f.schedule(operation)
		start := f.epochStart(ctfTestFirstEpoch)

		// At the block before the first epoch, an execution can only land in it.
		f.chain.rollTo(start)
		f.refused("the execute step a block before the first epoch", fmt.Sprintf("the first epoch 3 starts at block %d, the next block: no execution lands before it", start), "execute", "true")
		failed, revert := f.chain.send(f.stranger.key, &f.timelock, nil, operation.execute)
		if failed.Status != types.ReceiptStatusFailed || failed.BlockNumber.Uint64() != start || f.errorName(revert) != "InvalidEpoch" {
			t.Fatalf("the execute in the first epoch: status %d in block %d, revert %s", failed.Status, failed.BlockNumber, f.errorName(revert))
		}
		f.requireUnchanged(phaseC, operation.id, ctfTestValue)
		f.refused("the execute step in the first epoch", "the first epoch 3 has started: the current epoch is 3", "execute", "true")
	})
}
