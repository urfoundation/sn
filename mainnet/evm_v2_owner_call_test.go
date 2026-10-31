// The owner-call review runs on the e2e chain after the phase C upgrade: pool
// 1 registered for v2, the pinned Safe 1.4.1 owning the upgraded proxy, a
// single-leaf v1 sweep root committed for the pool's first epoch and swept
// stake absorbed into vault2. The owner sends exactly the execTransaction
// review prints.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/merkle"
)

// ownerCall runs the owner-call review with the chain's addresses and args.
func (self *upgradeTestChain) ownerCall(args ...string) (settlement2OwnerCallReview, int, string) {
	self.t.Helper()
	full := append([]string{"review", "--rpc", self.chain.server.URL, "--settlement2", self.settlement.Hex(), "--safe", self.safe.Hex()}, args...)
	var stdout, stderr bytes.Buffer
	code := runSettlement2OwnerCallCommand(context.Background(), full, &stdout, &stderr)
	var result settlement2OwnerCallReview
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			self.t.Fatalf("owner-call output: %v\n%s", err, stdout.String())
		}
	}
	return result, code, stderr.String()
}

// logged reports whether the receipt holds event name of the phase C ABI or
// vault2's from `at`, with these topics after the signature.
func (self *upgradeTestChain) logged(receipt *types.Receipt, at common.Address, id common.Hash, topics ...common.Hash) bool {
	for _, log := range receipt.Logs {
		if log.Address == at && len(log.Topics) == len(topics)+1 && log.Topics[0] == id {
			match := true
			for i, topic := range topics {
				match = match && log.Topics[i+1] == topic
			}
			if match {
				return true
			}
		}
	}
	return false
}

// UP-4: after the upgrade, the owner records the committed sweep and assigns
// the absorbed surplus to the pool's next unfunded epoch, each through the
// Safe exactly as review prints it; review refuses each call the chain would
// refuse, and any call before the upgrade.
func TestOwnerCallSettlement2ThroughTheSafe(t *testing.T) {
	safeOwner := newE2EActor("safe-owner")
	f := &upgradeTestChain{e2eFixture: newE2EFixture(t, e2eDefaultFirst, safeOwner), safeOwner: safeOwner}
	f.installSafe(t)
	sweep := f.firstEpoch
	record := sha256.Sum256([]byte("e2e v1 sweep record"))
	recordHash := common.Hash(record).Hex()
	if _, code, stderr := f.ownerCall("--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", recordHash); code != 3 || !strings.Contains(stderr, "does not run the phase C implementation") {
		t.Fatalf("a call before the upgrade: exit %d\n%s", code, stderr)
	}
	f.upgrade(t)

	// The single-leaf v1 sweep root for the pool's first epoch, and the swept
	// stake on the v1 escrow hotkey absorbed into vault2's escrow.
	f.chain.rollTo(f.end(sweep))
	root := [32]byte(merkle.PayoutLeaf(f.vault2Coldkey, big.NewInt(10_000)))
	if _, code, stderr := f.ownerCall("--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", recordHash); code != 3 || !strings.Contains(stderr, "is not the single leaf") {
		t.Fatalf("a record before the commit: exit %d\n%s", code, stderr)
	}
	f.commitV1(sweep, root, record)
	f.chain.emit(f.escrowV1, f.vault2Coldkey, e2eRao(5_000_000_000))
	f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackAbsorbStake(f.escrowV1))
	surplus := new(big.Int).Sub(e2eView(f.e2eFixture, f.vault2, e2eVault2Binding.PackLiveEscrowStake(), e2eVault2Binding.UnpackLiveEscrowStake), f.vault2Counter(e2eVault2Binding.PackEscrowAccounted))
	if surplus.Sign() <= 0 {
		t.Fatalf("surplus %s", surplus)
	}

	other := common.Hash(sha256.Sum256([]byte("another record"))).Hex()
	for _, refusal := range []struct {
		name, want string
		args       []string
	}{
		{"another artifact", "the committed artifact hash is", []string{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", other}},
		{"a v1 epoch", "before the pool's first v2 epoch", []string{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "1", "--artifact-hash", recordHash}},
		{"an unregistered pool", "vault2 has no pool 2", []string{"--function", "assignSurplus", "--no-id", "2", "--epoch", "2", "--amount", surplus.String()}},
		{"a later epoch", "the pool's next unfunded epoch is 2, not 3", []string{"--function", "assignSurplus", "--no-id", "1", "--epoch", "3", "--amount", surplus.String()}},
		{"more than the surplus", "exceeds vault2's surplus", []string{"--function", "assignSurplus", "--no-id", "1", "--epoch", "2", "--amount", new(big.Int).Add(surplus, common.Big1).String()}},
	} {
		if _, code, stderr := f.ownerCall(refusal.args...); code != 3 || !strings.Contains(stderr, refusal.want) {
			t.Fatalf("%s: exit %d, want 3 naming %q\n%s", refusal.name, code, refusal.want, stderr)
		}
	}
	for _, usage := range [][]string{
		{"--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2"},
		{"--function", "assignSurplus", "--no-id", "1", "--epoch", "2", "--amount", "0"},
		{"--function", "assignSurplus", "--no-id", "1", "--epoch", "2", "--amount", "1", "--artifact-hash", recordHash},
		{"--function", "closeOperatorEpoch", "--no-id", "1", "--epoch", "2"},
	} {
		if _, code, _ := f.ownerCall(usage...); code != 2 {
			t.Fatalf("%v: exit %d, want 2", usage, code)
		}
	}

	// The record, as printed.
	recordReview, code, stderr := f.ownerCall("--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", recordHash)
	if code != 0 || !recordReview.Preflight.Ran || recordReview.Safe.Nonce != "1" || recordReview.SafeTx.To != f.settlement.Hex() ||
		recordReview.SafeTx.Function != "recordV1Sweep(uint256,uint256,bytes32)" || recordReview.Chain["committed_root"] != common.Hash(root).Hex() ||
		recordReview.SafeTx.SafeTxHash != f.safeHash(settlement2UpgradeReview{SafeTx: recordReview.SafeTx}).Hex() {
		t.Fatalf("record review: exit %d %+v\n%s", code, recordReview, stderr)
	}
	receipt := f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(recordReview.OwnerTx.Data))
	if !f.logged(receipt, f.settlement, f.phaseC.abi.Events["V1SweepRecorded"].ID, common.BigToHash(big.NewInt(e2eNoID)), common.BigToHash(new(big.Int).SetUint64(sweep))) {
		t.Fatal("the record emitted no V1SweepRecorded")
	}
	if _, code, stderr := f.ownerCall("--function", "recordV1Sweep", "--no-id", "1", "--epoch", "2", "--artifact-hash", recordHash); code != 3 || !strings.Contains(stderr, "already records its v1 sweep at epoch 2") {
		t.Fatalf("a second record: exit %d\n%s", code, stderr)
	}

	// The assignment, as printed.
	assignReview, code, stderr := f.ownerCall("--function", "assignSurplus", "--no-id", "1", "--epoch", "2", "--amount", surplus.String())
	if code != 0 || !assignReview.Preflight.Ran || assignReview.Safe.Nonce != "2" || assignReview.Chain["surplus"] != surplus.String() ||
		assignReview.SafeTx.Function != "assignSurplus(uint256,uint256,uint256)" || assignReview.SafeTx.Args["epoch"] != "2" {
		t.Fatalf("assign review: exit %d %+v\n%s", code, assignReview, stderr)
	}
	receipt = f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(assignReview.OwnerTx.Data))
	if !f.logged(receipt, f.vault2, f.vault2ABI.Events["SurplusAssigned"].ID, common.BigToHash(big.NewInt(e2eNoID)), common.BigToHash(big.NewInt(2))) {
		t.Fatal("the assignment emitted no SurplusAssigned")
	}
	if got := e2eView(f.e2eFixture, f.vault2, e2eVault2Binding.PackCapturedTotal(big.NewInt(e2eNoID)), e2eVault2Binding.UnpackCapturedTotal); got.Cmp(surplus) != 0 {
		t.Fatalf("captured total %s, want the surplus %s", got, surplus)
	}
	if _, code, stderr := f.ownerCall("--function", "assignSurplus", "--no-id", "1", "--epoch", "2", "--amount", "1"); code != 3 || !strings.Contains(stderr, "exceeds vault2's surplus 0") {
		t.Fatalf("a second assignment: exit %d\n%s", code, stderr)
	}
	f.requireConservation()
}
