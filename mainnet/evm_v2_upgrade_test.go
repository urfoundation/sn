// The upgrade review runs against the e2e chain: the reviewed v1 and v2
// stacks in geth's EVM; the pinned Safe 1.4.1 SafeL2 behind its SafeProxy, with
// one owner at threshold one, as STSettlement2's owner; and the deterministic
// deployment proxy's exact code at its canonical address. The implementation
// is the forge build of STSettlement2PhaseC, and the owner sends exactly the
// execTransaction review prints.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/stabi"
)

// installCode sets code at an address as its own block, as genesis would.
func (self *e2eChain) installCode(address common.Address, code []byte) {
	self.do(func() {
		db := self.stateLocked(self.head)
		db.SetCode(address, code, tracing.CodeChangeUnspecified)
		db.Finalise(true)
		self.commitLocked(db, self.head+1, nil, nil)
	})
}

func (self *e2eChain) codeAt(address common.Address) []byte {
	var code []byte
	self.do(func() { code = self.stateLocked(self.head).GetCode(address) })
	return code
}

func (self *e2eChain) wordAt(address common.Address, slot common.Hash) common.Hash {
	var word common.Hash
	self.do(func() { word = self.stateLocked(self.head).GetState(address, slot) })
	return word
}

type upgradeTestChain struct {
	*e2eFixture
	safeOwner *e2eActor
	safe      common.Address
	safeAbi   abi.ABI
	phaseC    *e2eArtifact
	artifact  string
}

func newUpgradeTestChain(t *testing.T) *upgradeTestChain {
	t.Helper()
	safeOwner := newE2EActor("safe-owner")
	self := &upgradeTestChain{e2eFixture: newE2EDeployment(t, e2eDefaultFirst, safeOwner), safeOwner: safeOwner}
	pin, _, members := safeReleaseTestInputs(t, "1.4.1", "SafeL2")
	_, singleton, proxy := safeExecutionOracleArtifacts(t, pin, "SafeL2", members)
	var err error
	if self.safeAbi, err = abi.JSON(bytes.NewReader(singleton.Abi)); err != nil {
		t.Fatal(err)
	}
	proxyAbi, err := abi.JSON(bytes.NewReader(proxy.Abi))
	if err != nil {
		t.Fatal(err)
	}
	singletonAddress := self.deploy(self.deployer, &e2eArtifact{abi: self.safeAbi, creation: common.FromHex(singleton.Creation)}, nil)
	self.safe = self.deploy(self.deployer, &e2eArtifact{abi: proxyAbi, creation: common.FromHex(proxy.Creation)}, nil, singletonAddress)
	setup, err := self.safeAbi.Pack("setup", []common.Address{safeOwner.address}, big.NewInt(1), common.Address{}, []byte{},
		common.Address{}, common.Address{}, big.NewInt(0), common.Address{})
	if err != nil {
		t.Fatal(err)
	}
	self.must(self.deployer, self.safe, nil, setup)
	self.must(self.owner, self.settlement, nil, stabi.NewSTSettlement2().PackTransferOwnership(self.safe))
	self.chain.installCode(upgradeCreate2Factory, hexutil.MustDecode(upgradeCreate2FactoryRuntime))
	self.phaseC = e2eForgeArtifact(t, "STSettlement2PhaseC")
	self.artifact = filepath.Join(t.TempDir(), "STSettlement2PhaseC.json")
	if err := os.WriteFile(self.artifact, self.phaseC.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return self
}

// review runs the command over the chain's inputs, each flag named in changes
// replaced by the value after it.
func (self *upgradeTestChain) review(changes ...string) (settlement2UpgradeReview, int, string) {
	self.t.Helper()
	inputs := [][2]string{{"rpc", self.chain.server.URL}, {"settlement2", self.settlement.Hex()}, {"safe", self.safe.Hex()},
		{"artifact", self.artifact}, {"salt", common.Hash{}.Hex()}}
	for i := 0; i+1 < len(changes); i += 2 {
		for k := range inputs {
			if inputs[k][0] == changes[i] {
				inputs[k][1] = changes[i+1]
			}
		}
	}
	args := []string{"review"}
	for _, input := range inputs {
		if input[1] != "" {
			args = append(args, "--"+input[0], input[1])
		}
	}
	var stdout, stderr bytes.Buffer
	code := runUpgradeSettlement2Command(context.Background(), args, &stdout, &stderr)
	var result settlement2UpgradeReview
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			self.t.Fatalf("review output: %v\n%s", err, stdout.String())
		}
	}
	return result, code, stderr.String()
}

// The Safe's own EIP-712 hash of the reviewed Safe transaction.
func (self *upgradeTestChain) safeHash(review settlement2UpgradeReview) common.Hash {
	self.t.Helper()
	nonce, _ := new(big.Int).SetString(review.SafeTx.Nonce, 10)
	data, err := self.safeAbi.Pack("getTransactionHash", common.HexToAddress(review.SafeTx.To), new(big.Int), hexutil.MustDecode(review.SafeTx.Data),
		review.SafeTx.Operation, new(big.Int), new(big.Int), new(big.Int), common.Address{}, common.Address{}, nonce)
	if err != nil {
		self.t.Fatal(err)
	}
	out, err := self.chain.call(self.safe, data)
	if err != nil {
		self.t.Fatal(err)
	}
	return common.BytesToHash(out)
}

// UP-1: review prints the CREATE2 creation and the Safe transaction; the
// implementation created through the factory has exactly the reviewed code;
// review then preflights the owner's execTransaction, which the owner sends
// as printed, and the proxy runs the phase C implementation with every slot
// kept. A second review refuses.
func TestUpgradeSettlement2ReviewThroughTheSafe(t *testing.T) {
	f := newUpgradeTestChain(t)
	settlement := stabi.NewSTSettlement2()
	implementation := crypto.CreateAddress2(upgradeCreate2Factory, common.Hash{}, crypto.Keccak256(f.phaseC.creation))
	phaseA := common.BytesToAddress(f.chain.wordAt(f.settlement, common.HexToHash(contractImplementationSlot)).Bytes())

	before, code, stderr := f.review()
	if code != 0 {
		t.Fatalf("review before the creation: exit %d\n%s", code, stderr)
	}
	execData, err := upgradeSafeAbi.Pack("execTransaction", f.settlement, new(big.Int), settlement.PackUpgradeToAndCall(implementation, []byte{}), uint8(0),
		new(big.Int), new(big.Int), new(big.Int), common.Address{}, common.Address{}, upgradeOwnerSignature(f.safeOwner.address))
	if err != nil {
		t.Fatal(err)
	}
	if before.Schema != settlement2UpgradeReviewSchema || before.ChainId != 964 || before.Implementation.Created || before.Preflight.Ran ||
		before.Implementation.Address != implementation.Hex() || before.Implementation.CreateTo != upgradeCreate2Factory.Hex() ||
		before.Implementation.CreateData != hexutil.Encode(append(common.Hash{}.Bytes(), f.phaseC.creation...)) ||
		before.Settlement2.Owner != f.safe.Hex() || before.Settlement2.CurrentImplementation != phaseA.Hex() ||
		before.Safe.Version != "1.4.1" || before.Safe.Threshold != 1 || !slices.Equal(before.Safe.Owners, []string{f.safeOwner.address.Hex()}) ||
		before.Safe.Nonce != "0" || len(before.Safe.Modules) != 0 || before.Safe.Guard != (common.Address{}).Hex() ||
		before.SafeTx.To != f.settlement.Hex() || before.SafeTx.ValueWei != "0" || before.SafeTx.Operation != 0 ||
		before.SafeTx.Data != hexutil.Encode(settlement.PackUpgradeToAndCall(implementation, []byte{})) ||
		before.SafeTx.Args["newImplementation"] != implementation.Hex() || before.SafeTx.Args["data"] != "0x" ||
		before.SafeTx.SafeTxHash != f.safeHash(before).Hex() ||
		before.OwnerTx.From != f.safeOwner.address.Hex() || before.OwnerTx.To != f.safe.Hex() || before.OwnerTx.Data != hexutil.Encode(execData) ||
		before.OwnerTx.DataKeccak != crypto.Keccak256Hash(execData).Hex() {
		t.Fatalf("review before the creation: %+v", before)
	}

	// Anyone creates the implementation through the factory.
	receipt := f.must(f.anyone, upgradeCreate2Factory, nil, hexutil.MustDecode(before.Implementation.CreateData))
	if created := f.chain.codeAt(implementation); crypto.Keccak256Hash(created).Hex() != before.Implementation.RuntimeCodeHash || len(created) != before.Implementation.RuntimeBytes {
		t.Fatalf("created code hash %s, reviewed %s", crypto.Keccak256Hash(created), before.Implementation.RuntimeCodeHash)
	}
	t.Logf("CREATE2 of the implementation: gas %d", receipt.GasUsed)

	after, code, stderr := f.review()
	if code != 0 || !after.Implementation.Created || !after.Preflight.Ran || after.SafeTx.SafeTxHash != before.SafeTx.SafeTxHash || after.OwnerTx.Data != before.OwnerTx.Data {
		t.Fatalf("review after the creation: exit %d %+v\n%s", code, after, stderr)
	}

	// The owner sends exactly the printed execTransaction.
	slots := make([]common.Hash, 51)
	for i := range slots {
		slots[i] = f.chain.wordAt(f.settlement, common.BigToHash(big.NewInt(int64(i))))
	}
	receipt = f.must(f.safeOwner, f.safe, nil, hexutil.MustDecode(after.OwnerTx.Data))
	var upgraded bool
	for _, log := range receipt.Logs {
		if event, err := settlement.UnpackUpgradedEvent(log); err == nil && log.Address == f.settlement && event.Implementation == implementation {
			upgraded = true
		}
	}
	if !upgraded || common.BytesToAddress(f.chain.wordAt(f.settlement, common.HexToHash(contractImplementationSlot)).Bytes()) != implementation {
		t.Fatal("the owner's execTransaction did not upgrade the proxy to the implementation")
	}
	for i := range slots {
		if f.chain.wordAt(f.settlement, common.BigToHash(big.NewInt(int64(i)))) != slots[i] {
			t.Fatalf("slot %d changed in the upgrade", i)
		}
	}
	query, err := f.phaseC.abi.Pack("v1Sweep", big.NewInt(e2eNoID))
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := f.chain.call(f.settlement, query)
	if err != nil || len(sweep) != 96 || !bytes.Equal(sweep, make([]byte, 96)) {
		t.Fatalf("v1Sweep through the upgraded proxy: %x %v", sweep, err)
	}
	t.Logf("execTransaction(upgradeToAndCall): gas %d", receipt.GasUsed)

	if _, code, stderr := f.review(); code != 3 || !strings.Contains(stderr, "already runs the phase C implementation") {
		t.Fatalf("review after the upgrade: exit %d\n%s", code, stderr)
	}
}

// UP-2: review refuses, exit 3, a Safe that does not own STSettlement2, a
// factory whose code differs and other code at the implementation's address;
// it rejects, exit 2, an artifact that is not the phase C implementation and
// missing flags, before any read.
func TestUpgradeSettlement2ReviewRefusals(t *testing.T) {
	f := newUpgradeTestChain(t)
	implementation := crypto.CreateAddress2(upgradeCreate2Factory, common.Hash{}, crypto.Keccak256(f.phaseC.creation))
	refused := func(name, want string, changes ...string) {
		t.Helper()
		if _, code, stderr := f.review(changes...); code != 3 || !strings.Contains(stderr, want) {
			t.Fatalf("%s: exit %d, want 3 naming %q\n%s", name, code, want, stderr)
		}
	}
	refused("another Safe", "STSettlement2's owner is", "safe", f.anyone.address.Hex())

	f.chain.installCode(upgradeCreate2Factory, []byte{0x00})
	refused("a different factory", "the deterministic deployment proxy is missing or its code differs")
	f.chain.installCode(upgradeCreate2Factory, hexutil.MustDecode(upgradeCreate2FactoryRuntime))

	f.chain.installCode(implementation, []byte{0x60, 0x00, 0xf3})
	refused("other code at the address", "is not the reviewed implementation's")

	var artifact map[string]any
	if err := json.Unmarshal(f.phaseC.raw, &artifact); err != nil {
		t.Fatal(err)
	}
	var methods []any
	for _, entry := range artifact["abi"].([]any) {
		if entry.(map[string]any)["name"] != "v1Sweep" {
			methods = append(methods, entry)
		}
	}
	artifact["abi"] = methods
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	phaseA := filepath.Join(t.TempDir(), "phase-a.json")
	if err := os.WriteFile(phaseA, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code, stderr := f.review("artifact", phaseA); code != 2 || !strings.Contains(stderr, "it is not the phase C implementation") {
		t.Fatalf("an artifact without v1Sweep: exit %d\n%s", code, stderr)
	}
	for _, flag := range []string{"rpc", "settlement2", "safe", "artifact", "salt"} {
		if _, code, _ := f.review(flag, ""); code != 2 {
			t.Fatalf("without --%s: exit %d, want 2", flag, code)
		}
	}
	if _, code, _ := f.review("salt", "0x01"); code != 2 {
		t.Fatalf("a short salt: exit %d, want 2", code)
	}
}

// UP-3: the runtime code review expects is the artifact's with UUPS's __self
// immutable holding the created address at each of its references.
func TestUpgradeArtifactRuntimeHoldsItsOwnAddress(t *testing.T) {
	f := &upgradeTestChain{phaseC: e2eForgeArtifact(t, "STSettlement2PhaseC")}
	artifact, err := loadUpgradeArtifact(f.phaseC.raw)
	if err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	code := artifact.runtimeAt(address)
	if len(artifact.self) == 0 || len(code) != len(artifact.runtime) {
		t.Fatalf("%d references", len(artifact.self))
	}
	for i := 0; i < len(code); i++ {
		inside := false
		for _, start := range artifact.self {
			if i >= start && i < start+32 {
				inside = true
				if want := common.LeftPadBytes(address.Bytes(), 32)[i-start]; code[i] != want {
					t.Fatalf("byte %d is %x, want %x", i, code[i], want)
				}
			}
		}
		if !inside && code[i] != artifact.runtime[i] {
			t.Fatalf("byte %d outside __self changed", i)
		}
	}
	if !bytes.Equal(upgradeOwnerSignature(address), append(append(common.LeftPadBytes(address.Bytes(), 32), make([]byte, 32)...), 1)) {
		t.Fatal("the owner's approved-hash signature differs")
	}
}
