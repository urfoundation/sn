// The timelock mode of the upgrade and owner-call reviews runs on the e2e
// chain with mainnet's configuration: the pinned OpenZeppelin 5.6.1
// TimelockController from sn/evm's build, with a 72 h minimum delay, the
// pinned Safe 1.4.1 as its only proposer and canceller, execution open to
// anyone and no admin but itself, owning STSettlement2 after the Safe's
// transferOwnership. Its runtime is exactly the mainnet timelock's. The Safe's
// owner sends exactly the execTransaction review prints, and a stranger the
// execute transaction.
package main

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/stabi"
)

// The mainnet timelock's CBOR metadata, the last 53 bytes of its code. Its
// IPFS hash covers the absolute library paths of the directory the timelock
// was built in, so no other build reproduces it; with the executable bytes of
// any build of the pinned source it is exactly the mainnet code.
const timelockTestMainnetMetadata = "0xa264697066735822122005822dc92c778c13b13b822e19d00cc0c6b5919f0c86d97ba134de54c6d829dc64736f6c63430008180033"

// deployTimelock creates sn/evm's build of the pinned TimelockController from
// the deployer with these roles and delay, as the mainnet one was.
func (self *upgradeTestChain) deployTimelock(t *testing.T, delay int64, proposers, executors []common.Address, admin common.Address) common.Address {
	t.Helper()
	return self.deploy(self.deployer, e2eForgeArtifact(t, "TimelockController"), nil, big.NewInt(delay), proposers, executors, admin)
}

// installTimelock deploys the timelock, then gives it exactly the mainnet
// runtime: the build's executable bytes, which must hash with the mainnet
// metadata to the reviewed code hash, and the mainnet metadata.
func (self *upgradeTestChain) installTimelock(t *testing.T, delay int64, proposers, executors []common.Address, admin common.Address) common.Address {
	t.Helper()
	address := self.deployTimelock(t, delay, proposers, executors, admin)
	code := self.chain.codeAt(address)
	metadata := int(code[len(code)-2])<<8 | int(code[len(code)-1]) + 2
	mainnet := append(slices.Clone(code[:len(code)-metadata]), hexutil.MustDecode(timelockTestMainnetMetadata)...)
	if hash := crypto.Keccak256Hash(mainnet); hash != settlement2TimelockCodeHash || len(mainnet) != len(code) {
		t.Fatalf("the build's TimelockController with the mainnet metadata hashes %s, not the reviewed %s", hash, settlement2TimelockCodeHash)
	}
	self.chain.installCode(address, mainnet)
	return address
}

// handToTimelock has the Safe's owner execute transferOwnership(timelock) on
// STSettlement2, as the mainnet Safe's transaction of 2026-11-06 did.
func (self *upgradeTestChain) handToTimelock(t *testing.T, timelock common.Address) {
	t.Helper()
	data, err := upgradeSafeAbi.Pack("execTransaction", self.settlement, new(big.Int), stabi.NewSTSettlement2().PackTransferOwnership(timelock), uint8(0),
		new(big.Int), new(big.Int), new(big.Int), common.Address{}, common.Address{}, upgradeOwnerSignature(self.safeOwner.address))
	if err != nil {
		t.Fatal(err)
	}
	self.must(self.safeOwner, self.safe, nil, data)
	if owner := common.BytesToAddress(self.chain.wordAt(self.settlement, common.HexToHash(contractOwnableSlot)).Bytes()); owner != timelock {
		t.Fatalf("STSettlement2's owner is %s after the transfer", owner)
	}
}

// installOwner writes STSettlement2's owner word as a transfer to owner leaves
// it, as its own block.
func (self *upgradeTestChain) installOwner(owner common.Address) {
	self.chain.do(func() {
		db := self.chain.stateLocked(self.chain.head)
		db.SetState(self.settlement, common.HexToHash(contractOwnableSlot), common.BytesToHash(owner.Bytes()))
		db.Finalise(true)
		self.chain.commitLocked(db, self.chain.head+1, nil, nil)
	})
}

// timelockReadyBlock is the first block whose time is at least ready.
func timelockReadyBlock(ready uint64) uint64 {
	return (ready - e2eGenesisTime + e2eBlockSeconds - 1) / e2eBlockSeconds
}

func timelockUtc(seconds uint64) string {
	return time.Unix(int64(seconds), 0).UTC().Format(time.RFC3339)
}

// timelockRevert names a revert by the TimelockController's custom errors.
func (self *upgradeTestChain) timelockRevert(revert []byte) string {
	self.t.Helper()
	if len(revert) >= 4 {
		if failure, err := e2eForgeArtifact(self.t, "TimelockController").abi.ErrorByID([4]byte(revert[:4])); err == nil {
			return failure.Name
		}
	}
	return hexutil.Encode(revert)
}

// requireScheduled checks that the receipt logged the timelock's
// CallScheduled of the call with the minimum delay.
func (self *upgradeTestChain) requireScheduled(receipt *types.Receipt, timelock common.Address, id common.Hash, data []byte) {
	self.t.Helper()
	timelockAbi := e2eForgeArtifact(self.t, "TimelockController").abi
	self.requireLog(receipt, e2eLog{address: timelock, abi: &timelockAbi, name: "CallScheduled",
		args: []any{id, new(big.Int), self.settlement, new(big.Int), data, [32]byte{}, big.NewInt(settlement2TimelockMinDelay)}})
}

// requireExecuted checks that the receipt logged the timelock's CallExecuted
// of the call.
func (self *upgradeTestChain) requireExecuted(receipt *types.Receipt, timelock common.Address, id common.Hash, data []byte) {
	self.t.Helper()
	timelockAbi := e2eForgeArtifact(self.t, "TimelockController").abi
	self.requireLog(receipt, e2eLog{address: timelock, abi: &timelockAbi, name: "CallExecuted", args: []any{id, new(big.Int), self.settlement, new(big.Int), data}})
}

// requireLog checks that the receipt holds the event with every field.
func (self *upgradeTestChain) requireLog(receipt *types.Receipt, want e2eLog) {
	self.t.Helper()
	topics, data := want.encode(self.t)
	for _, log := range receipt.Logs {
		if log.Address == want.address && slices.Equal(log.Topics, topics) && slices.Equal(log.Data, data) {
			return
		}
	}
	self.t.Fatalf("tx %s logged no %s%v from %s", receipt.TxHash, want.name, want.args, want.address)
}

// UP-5: with STSettlement2 behind the timelock in mainnet's configuration, the
// review without --timelock names the timelock to pass. With it, the record
// and the assignment of the v1 sweep each go out as the Safe's schedule,
// exactly as printed, after the call's own preflight from the timelock; each
// is refused while it waits, and after 72 h a stranger sends exactly the
// execute transaction printed at the schedule, which the execute step's review
// preflights. An executed operation is refused, and the same call again takes
// another --timelock-salt.
func TestOwnerCallSettlement2ThroughTheTimelock(t *testing.T) {
	safeOwner, stranger := newE2EActor("safe-owner"), newE2EActor("stranger")
	f := &upgradeTestChain{e2eFixture: newE2EFixture(t, e2eDefaultFirst, safeOwner, stranger), safeOwner: safeOwner}
	f.installSafe(t)
	f.upgrade(t)
	timelock := f.installTimelock(t, settlement2TimelockMinDelay, []common.Address{f.safe}, []common.Address{{}}, common.Address{})
	f.handToTimelock(t, timelock)

	// The sweep, as in UP-4: the single-leaf v1 root committed for the pool's
	// first epoch, and the swept stake absorbed into vault2's escrow.
	sweep := f.firstEpoch
	f.chain.rollTo(f.end(sweep))
	root := [32]byte(merkle.PayoutLeaf(f.vault2Coldkey, big.NewInt(10_000)))
	record := sha256.Sum256([]byte("e2e v1 sweep record"))
	f.commitV1(sweep, root, record)
	f.chain.emit(f.escrowV1, f.vault2Coldkey, e2eRao(5_000_000_000))
	f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackAbsorbStake(f.escrowV1))
	surplus := new(big.Int).Sub(e2eView(f.e2eFixture, f.vault2, e2eVault2Binding.PackLiveEscrowStake(), e2eVault2Binding.UnpackLiveEscrowStake), f.vault2Counter(e2eVault2Binding.PackEscrowAccounted))

	recordArgs := []string{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", common.Hash(record).Hex()}
	if _, code, stderr := f.ownerCall(recordArgs...); code != 3 || !strings.Contains(stderr, "STSettlement2's owner is "+timelock.Hex()+", not the Safe; it answers getMinDelay() as a TimelockController does: review the call through it with --timelock "+timelock.Hex()) {
		t.Fatalf("a review without --timelock: exit %d\n%s", code, stderr)
	}
	recordArgs = append(recordArgs, "--timelock", timelock.Hex())
	recordData, err := settlement2PhaseCAbi.Pack("recordV1Sweep", big.NewInt(e2eNoID), big.NewInt(2), common.Hash(record))
	if err != nil {
		t.Fatal(err)
	}
	id := settlement2TimelockOperation(f.settlement, new(big.Int), recordData, common.Hash{})
	schedule, err := settlement2TimelockAbi.Pack("schedule", f.settlement, new(big.Int), recordData, [32]byte{}, [32]byte{}, big.NewInt(settlement2TimelockMinDelay))
	if err != nil {
		t.Fatal(err)
	}
	execute, err := settlement2TimelockAbi.Pack("execute", f.settlement, new(big.Int), recordData, [32]byte{}, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	timelockArtifact := e2eForgeArtifact(t, "TimelockController")
	query, err := timelockArtifact.abi.Pack("hashOperation", f.settlement, new(big.Int), recordData, [32]byte{}, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if hashed, err := f.chain.call(timelock, query); err != nil || common.BytesToHash(hashed) != id {
		t.Fatalf("hashOperation %x, review's %s: %v", hashed, id, err)
	}

	// The schedule, as printed.
	head := f.chain.next() - 1
	review, code, stderr := f.ownerCall(recordArgs...)
	if code != 0 || review.Schema != settlement2OwnerCallTimelockReviewSchema || review.Settlement2.Owner != timelock.Hex() || review.Timelock == nil {
		t.Fatalf("schedule review: exit %d %+v\n%s", code, review, stderr)
	}
	section := review.Timelock
	roles := map[string][]string{"proposer": {f.safe.Hex()}, "canceller": {f.safe.Hex()}, "executor": {common.Address{}.Hex()}, "admin": {timelock.Hex()}}
	for role, holders := range roles {
		if !slices.Equal(section.Roles[role], holders) {
			t.Fatalf("roles: %v, want %v", section.Roles, roles)
		}
	}
	if section.Address != timelock.Hex() || section.CodeHash != settlement2TimelockCodeHash.Hex() || section.MinDelaySeconds != "259200" || len(section.Roles) != 4 ||
		section.Executor != "open" || section.Step != "schedule" || section.OperationId != id.Hex() || section.OperationState != "unset" ||
		section.Salt != (common.Hash{}).Hex() || section.Predecessor != (common.Hash{}).Hex() ||
		section.Call.Target != f.settlement.Hex() || section.Call.Data != hexutil.Encode(recordData) || section.Call.Function != "recordV1Sweep(uint256,uint256,bytes32)" ||
		section.Call.Args["artifactHash"] != common.Hash(record).Hex() ||
		section.EarliestExecutionUnix != new(big.Int).SetUint64(e2eBlockTime(head)+settlement2TimelockMinDelay).String() ||
		section.EarliestExecution != timelockUtc(e2eBlockTime(head)+settlement2TimelockMinDelay) ||
		section.ExecuteTx.To != timelock.Hex() || section.ExecuteTx.Data != hexutil.Encode(execute) || section.ExecuteTx.DataKeccak != crypto.Keccak256Hash(execute).Hex() ||
		!section.CallPreflight.Ran || !review.Preflight.Ran || review.Chain["committed_root"] != common.Hash(root).Hex() {
		t.Fatalf("schedule review timelock section: %+v", section)
	}
	if review.Safe == nil || review.Safe.Nonce != "2" || review.SafeTx == nil || review.SafeTx.To != timelock.Hex() || review.SafeTx.Data != hexutil.Encode(schedule) ||
		review.SafeTx.Function != "schedule(address,uint256,bytes,bytes32,bytes32,uint256)" || review.SafeTx.Args["delay"] != "259200" || review.SafeTx.Args["data"] != hexutil.Encode(recordData) ||
		review.SafeTx.SafeTxHash != f.safeHash(settlement2UpgradeReview{SafeTx: review.SafeTx}).Hex() || review.OwnerTx == nil || review.OwnerTx.To != f.safe.Hex() {
		t.Fatalf("schedule review Safe transaction: %+v %+v", review.SafeTx, review.OwnerTx)
	}
	receipt := f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(review.OwnerTx.Data))
	f.requireScheduled(receipt, timelock, id, recordData)
	ready := e2eBlockTime(receipt.BlockNumber.Uint64()) + settlement2TimelockMinDelay

	// While it waits, neither step passes, and a stranger cannot execute.
	if _, code, stderr := f.ownerCall(recordArgs...); code != 3 || !strings.Contains(stderr, "the operation "+id.Hex()+" is already scheduled, ready at "+timelockUtc(ready)) {
		t.Fatalf("a second schedule: exit %d\n%s", code, stderr)
	}
	if _, code, stderr := f.ownerCall(append(slices.Clone(recordArgs), "--execute")...); code != 3 || !strings.Contains(stderr, "the operation "+id.Hex()+" is not ready until "+timelockUtc(ready)) {
		t.Fatalf("an early execute: exit %d\n%s", code, stderr)
	}
	if failed, revert := f.chain.send(stranger.key, &timelock, nil, execute); failed.Status != types.ReceiptStatusFailed || f.timelockRevert(revert) != "TimelockUnexpectedOperationState" {
		t.Fatalf("an early execute on chain: status %d revert %s", failed.Status, f.timelockRevert(revert))
	}

	// After the delay, a stranger sends exactly the printed execute.
	f.chain.rollTo(timelockReadyBlock(ready) + 1)
	if _, code, stderr := f.ownerCall(recordArgs...); code != 3 || !strings.Contains(stderr, "the operation "+id.Hex()+" is already scheduled and ready: review its execute step with --execute") {
		t.Fatalf("a schedule of a ready operation: exit %d\n%s", code, stderr)
	}
	executeReview, code, stderr := f.ownerCall(append(slices.Clone(recordArgs), "--execute")...)
	if code != 0 || executeReview.Timelock == nil || executeReview.Timelock.Step != "execute" || executeReview.Timelock.OperationState != "ready" ||
		executeReview.Timelock.EarliestExecutionUnix != new(big.Int).SetUint64(ready).String() || executeReview.Timelock.ExecuteTx != section.ExecuteTx ||
		!executeReview.Timelock.CallPreflight.Ran || !executeReview.Preflight.Ran || executeReview.Safe != nil || executeReview.SafeTx != nil || executeReview.OwnerTx != nil {
		t.Fatalf("execute review: exit %d %+v %+v\n%s", code, executeReview, executeReview.Timelock, stderr)
	}
	receipt = f.must(stranger, timelock, nil, hexutil.MustDecode(executeReview.Timelock.ExecuteTx.Data))
	f.requireExecuted(receipt, timelock, id, recordData)
	if !f.logged(receipt, f.settlement, f.phaseC.abi.Events["V1SweepRecorded"].ID, common.BigToHash(big.NewInt(e2eNoID)), common.BigToHash(new(big.Int).SetUint64(sweep))) {
		t.Fatal("the execute recorded no V1SweepRecorded")
	}
	if _, code, stderr := f.ownerCall(append(slices.Clone(recordArgs), "--execute")...); code != 3 || !strings.Contains(stderr, "already records its v1 sweep at epoch 2") {
		t.Fatalf("an execute after the record: exit %d\n%s", code, stderr)
	}

	// Half the surplus, scheduled and executed the same way.
	half := new(big.Int).Rsh(surplus, 1)
	assignArgs := []string{"--function", "assignSurplus", "--no-id", "1", "--epoch", "2", "--amount", half.String(), "--timelock", timelock.Hex()}
	assignData, err := settlement2PhaseCAbi.Pack("assignSurplus", big.NewInt(e2eNoID), big.NewInt(2), half)
	if err != nil {
		t.Fatal(err)
	}
	assignId := settlement2TimelockOperation(f.settlement, new(big.Int), assignData, common.Hash{})
	review, code, stderr = f.ownerCall(assignArgs...)
	if code != 0 || review.Timelock.OperationId != assignId.Hex() || review.Safe.Nonce != "3" || review.Chain["surplus"] != surplus.String() || !review.Timelock.CallPreflight.Ran {
		t.Fatalf("assign schedule review: exit %d %+v\n%s", code, review, stderr)
	}
	receipt = f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(review.OwnerTx.Data))
	f.requireScheduled(receipt, timelock, assignId, assignData)
	f.chain.rollTo(timelockReadyBlock(e2eBlockTime(receipt.BlockNumber.Uint64())+settlement2TimelockMinDelay) + 1)
	executeReview, code, stderr = f.ownerCall(append(slices.Clone(assignArgs), "--execute")...)
	if code != 0 || executeReview.Timelock.ExecuteTx != review.Timelock.ExecuteTx {
		t.Fatalf("assign execute review: exit %d %+v\n%s", code, executeReview.Timelock, stderr)
	}
	receipt = f.must(stranger, timelock, nil, hexutil.MustDecode(executeReview.Timelock.ExecuteTx.Data))
	f.requireExecuted(receipt, timelock, assignId, assignData)
	if !f.logged(receipt, f.vault2, f.vault2ABI.Events["SurplusAssigned"].ID, common.BigToHash(big.NewInt(e2eNoID)), common.BigToHash(big.NewInt(2))) {
		t.Fatal("the execute assigned nothing")
	}

	// The same assignment again is the executed operation; another salt makes
	// it a new one.
	if _, code, stderr := f.ownerCall(append(slices.Clone(assignArgs), "--execute")...); code != 3 || !strings.Contains(stderr, "the operation "+assignId.Hex()+" has already executed") {
		t.Fatalf("an executed operation's execute: exit %d\n%s", code, stderr)
	}
	if _, code, stderr := f.ownerCall(assignArgs...); code != 3 || !strings.Contains(stderr, "the operation "+assignId.Hex()+" has executed; scheduling the same call again takes another --timelock-salt") {
		t.Fatalf("an executed operation's schedule: exit %d\n%s", code, stderr)
	}
	salt := common.HexToHash("0x01")
	review, code, stderr = f.ownerCall(append(slices.Clone(assignArgs), "--timelock-salt", salt.Hex())...)
	if code != 0 || review.Timelock.Salt != salt.Hex() || review.Timelock.OperationId != settlement2TimelockOperation(f.settlement, new(big.Int), assignData, salt).Hex() || review.SafeTx.Args["salt"] != salt.Hex() {
		t.Fatalf("a salted schedule: exit %d %+v\n%s", code, review.Timelock, stderr)
	}
	f.requireConservation()
}

// UP-6: the phase C upgrade through a timelock that only the Safe executes.
// Before the implementation exists the schedule is printed with neither
// preflight run, and the execute step of an unscheduled operation is refused.
// Once it exists, the owner schedules exactly as printed; after the delay the
// execute step is the Safe's execTransaction of the execute printed at the
// schedule, which a stranger cannot send in its place, and the proxy runs the
// implementation.
func TestUpgradeSettlement2ThroughTheTimelock(t *testing.T) {
	f := newUpgradeTestChain(t)
	timelock := f.installTimelock(t, settlement2TimelockMinDelay, []common.Address{f.safe}, []common.Address{f.safe}, common.Address{})
	f.handToTimelock(t, timelock)
	implementation := crypto.CreateAddress2(upgradeCreate2Factory, common.Hash{}, crypto.Keccak256(f.phaseC.creation))
	upgradeData := stabi.NewSTSettlement2().PackUpgradeToAndCall(implementation, []byte{})
	id := settlement2TimelockOperation(f.settlement, new(big.Int), upgradeData, common.Hash{})

	if _, code, stderr := f.review(); code != 3 || !strings.Contains(stderr, "review the call through it with --timelock "+timelock.Hex()) {
		t.Fatalf("a review without --timelock: exit %d\n%s", code, stderr)
	}
	before, code, stderr := f.review("timelock", timelock.Hex())
	if code != 0 || before.Schema != settlement2UpgradeTimelockReviewSchema || before.Implementation.Created || before.Preflight.Ran || before.Timelock.CallPreflight.Ran ||
		!strings.HasPrefix(before.Preflight.Result, "not run: the implementation does not exist yet") || before.Timelock.Executor != "safe" ||
		!slices.Equal(before.Timelock.Roles["executor"], []string{f.safe.Hex()}) || before.Timelock.OperationId != id.Hex() || before.Timelock.Call.Data != hexutil.Encode(upgradeData) ||
		before.Settlement2.Owner != timelock.Hex() || before.SafeTx.To != timelock.Hex() || before.SafeTx.Nonce != "1" {
		t.Fatalf("review before the creation: exit %d %+v %+v\n%s", code, before, before.Timelock, stderr)
	}
	if _, code, stderr := f.review("timelock", timelock.Hex(), "execute", "true"); code != 3 || !strings.Contains(stderr, "the operation "+id.Hex()+" is not scheduled") {
		t.Fatalf("an execute before the schedule: exit %d\n%s", code, stderr)
	}

	f.must(f.anyone, upgradeCreate2Factory, nil, hexutil.MustDecode(before.Implementation.CreateData))
	after, code, stderr := f.review("timelock", timelock.Hex())
	if code != 0 || !after.Preflight.Ran || !after.Timelock.CallPreflight.Ran || after.SafeTx.SafeTxHash != before.SafeTx.SafeTxHash ||
		after.SafeTx.SafeTxHash != f.safeHash(after).Hex() || after.OwnerTx.Data != before.OwnerTx.Data {
		t.Fatalf("review after the creation: exit %d %+v\n%s", code, after, stderr)
	}
	receipt := f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(after.OwnerTx.Data))
	f.requireScheduled(receipt, timelock, id, upgradeData)
	f.chain.rollTo(timelockReadyBlock(e2eBlockTime(receipt.BlockNumber.Uint64())+settlement2TimelockMinDelay) + 1)

	execute, code, stderr := f.review("timelock", timelock.Hex(), "execute", "true")
	if code != 0 || execute.Timelock.Step != "execute" || execute.Safe == nil || execute.Safe.Nonce != "2" || execute.SafeTx.To != timelock.Hex() ||
		execute.SafeTx.Data != before.Timelock.ExecuteTx.Data || execute.SafeTx.Function != "execute(address,uint256,bytes,bytes32,bytes32)" ||
		execute.SafeTx.Args["payload"] != hexutil.Encode(upgradeData) || execute.SafeTx.SafeTxHash != f.safeHash(execute).Hex() || !execute.Preflight.Ran {
		t.Fatalf("execute review: exit %d %+v %+v\n%s", code, execute, execute.SafeTx, stderr)
	}
	if failed, revert := f.chain.send(f.anyone.key, &timelock, nil, hexutil.MustDecode(before.Timelock.ExecuteTx.Data)); failed.Status != types.ReceiptStatusFailed || f.timelockRevert(revert) != "AccessControlUnauthorizedAccount" {
		t.Fatalf("a stranger's execute: status %d revert %s", failed.Status, f.timelockRevert(revert))
	}
	receipt = f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(execute.OwnerTx.Data))
	f.requireExecuted(receipt, timelock, id, upgradeData)
	if current := common.BytesToAddress(f.chain.wordAt(f.settlement, common.HexToHash(contractImplementationSlot)).Bytes()); current != implementation {
		t.Fatalf("the proxy runs %s after the execute", current)
	}
	if _, code, stderr := f.review("timelock", timelock.Hex(), "execute", "true"); code != 3 || !strings.Contains(stderr, "already runs the phase C implementation") {
		t.Fatalf("a review after the upgrade: exit %d\n%s", code, stderr)
	}
}

// UP-7: timelock mode refuses, exit 3, a timelock that does not own
// STSettlement2, another build's code, a minimum delay under 72 h, a Safe
// that does not propose or does not cancel, an executor that is neither open
// nor the Safe, and a call the timelock's execution would see revert, which
// only the call's own preflight from the timelock catches. It rejects, exit 2,
// timelock flags without --timelock, a malformed salt and the Safe as the
// timelock.
func TestSettlement2TimelockReviewRefusals(t *testing.T) {
	safeOwner := newE2EActor("safe-owner")
	f := &upgradeTestChain{e2eFixture: newE2EFixture(t, e2eDefaultFirst, safeOwner), safeOwner: safeOwner}
	f.installSafe(t)
	f.upgrade(t)
	f.chain.emit(f.escrowV1, f.vault2Coldkey, e2eRao(5_000_000_000))
	f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackAbsorbStake(f.escrowV1))
	surplus := new(big.Int).Sub(e2eView(f.e2eFixture, f.vault2, e2eVault2Binding.PackLiveEscrowStake(), e2eVault2Binding.UnpackLiveEscrowStake), f.vault2Counter(e2eVault2Binding.PackEscrowAccounted))
	safe, nobody, delay := []common.Address{f.safe}, []common.Address{f.anyone.address}, int64(settlement2TimelockMinDelay)
	reviewed := f.installTimelock(t, delay, safe, []common.Address{{}}, common.Address{})
	assign := func(timelock common.Address, extra ...string) (int, string) {
		t.Helper()
		_, code, stderr := f.ownerCall(append([]string{"--function", "assignSurplus", "--no-id", "1", "--epoch", "2", "--amount", surplus.String(), "--timelock", timelock.Hex()}, extra...)...)
		return code, stderr
	}
	refused := func(name string, owner, timelock common.Address, want string) {
		t.Helper()
		f.installOwner(owner)
		if code, stderr := assign(timelock); code != 3 || !strings.Contains(stderr, want) {
			t.Fatalf("%s: exit %d, want 3 naming %q\n%s", name, code, want, stderr)
		}
	}
	refused("the Safe still owns STSettlement2", f.safe, reviewed, "STSettlement2's owner is "+f.safe.Hex()+", not the timelock "+reviewed.Hex())
	other := f.deployTimelock(t, delay, safe, []common.Address{{}}, common.Address{})
	refused("another build", other, other, "the timelock's runtime code hash is "+crypto.Keccak256Hash(f.chain.codeAt(other)).Hex()+", not "+settlement2TimelockCodeHash.Hex())
	short := f.installTimelock(t, delay-1, safe, []common.Address{{}}, common.Address{})
	refused("a short delay", short, short, "the timelock's minimum delay is 259199 s, below the reviewed 259200 s (72 h)")
	foreign := f.installTimelock(t, delay, nobody, []common.Address{{}}, common.Address{})
	refused("another proposer", foreign, foreign, "the Safe does not hold the timelock's PROPOSER_ROLE")
	uncancellable := f.installTimelock(t, delay, safe, []common.Address{{}}, f.deployer.address)
	revoke, err := e2eForgeArtifact(t, "TimelockController").abi.Pack("revokeRole", crypto.Keccak256Hash([]byte("CANCELLER_ROLE")), f.safe)
	if err != nil {
		t.Fatal(err)
	}
	f.must(f.deployer, uncancellable, nil, revoke)
	refused("no Safe canceller", uncancellable, uncancellable, "the Safe does not hold the timelock's CANCELLER_ROLE")
	closed := f.installTimelock(t, delay, safe, nobody, common.Address{})
	refused("another executor", closed, closed, "neither the zero address, which opens execution to anyone, nor the Safe holds the timelock's EXECUTOR_ROLE")

	// The reviewed timelock passes; then a stake the e2e runtime sets to 2^128
	// makes the same checks pass for an assignment vault2's uint128 capture
	// accounting refuses, a stand-in for any refusal review does not model.
	f.installOwner(reviewed)
	if code, stderr := assign(reviewed); code != 0 {
		t.Fatalf("the reviewed timelock: exit %d\n%s", code, stderr)
	}
	f.chain.native(func(rt *e2eRuntime) { rt.setStake(f.escrowV2, f.vault2Coldkey, new(big.Int).Lsh(common.Big1, 128)) })
	surplus = new(big.Int).Sub(e2eView(f.e2eFixture, f.vault2, e2eVault2Binding.PackLiveEscrowStake(), e2eVault2Binding.UnpackLiveEscrowStake), f.vault2Counter(e2eVault2Binding.PackEscrowAccounted))
	if code, stderr := assign(reviewed); code != 3 || !strings.Contains(stderr, "the call from the timelock: ") ||
		!strings.Contains(stderr, fmt.Sprintf("call 0x%x to %s failed", settlement2PhaseCAbi.Methods["assignSurplus"].ID, f.settlement.Hex())) {
		t.Fatalf("a call that reverts from the timelock: exit %d\n%s", code, stderr)
	}

	for _, usage := range [][]string{
		{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", common.Hash{1}.Hex(), "--execute"},
		{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", common.Hash{1}.Hex(), "--timelock-salt", common.Hash{}.Hex()},
		{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", common.Hash{1}.Hex(), "--timelock", reviewed.Hex(), "--timelock-salt", "0x01"},
		{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", common.Hash{1}.Hex(), "--timelock", f.safe.Hex()},
		{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", common.Hash{1}.Hex(), "--timelock", common.Address{}.Hex()},
	} {
		if _, code, _ := f.ownerCall(usage...); code != 2 {
			t.Fatalf("%v: exit %d, want 2", usage, code)
		}
	}
	for _, usage := range [][]string{{"execute", "true"}, {"timelock-salt", common.Hash{}.Hex()}, {"timelock", reviewed.Hex(), "timelock-salt", "0x01"}} {
		if _, code, _ := f.review(usage...); code != 2 {
			t.Fatalf("upgrade review %v: exit %d, want 2", usage, code)
		}
	}
}
