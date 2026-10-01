// Escrow fixtures execute the real vault with instance-local EVM contracts that
// model the reviewed neuron/metagraph boundary. They do not qualify live native
// burn, existential-deposit behavior, or runtime deployment observations.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"maps"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// A zero-burn native model lets genuine vault execution return all supplied
// surplus. UID zero and uint16-max exercise valid mapping boundaries.
func newEvmEscrowFixture(t *testing.T, uid uint16) *evmCreateFixture {
	t.Helper()
	f := newEvmCoordinatorFixture(t)
	sender := f.config.Plan.Actions[0].Sender
	vault := crypto.CreateAddress(sender, 1)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "escrow-register", Sender: sender, Nonce: 3, To: &vault, Data: "0x" + hex.EncodeToString(stabi.NewSTSettlementVault().PackRegisterEscrow(2)), ValueWei: "2000000000", Gas: 1_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "2170000000"
	f.config.Plan.MaximumAttempts = 5
	f.publishConfig()
	// PUSH selector; compare calldata selector; jump to the patched body.
	dispatch := func(code []byte, signature string) ([]byte, int) {
		code = append(code, 0x60, 0x00, 0x35, 0x60, 0xe0, 0x1c, 0x63)
		code = append(code, crypto.Keccak256([]byte(signature))[:4]...)
		code = append(code, 0x14, 0x60, 0x00, 0x57)
		return code, len(code) - 2
	}
	neuron, uidJump := dispatch(nil, "getUid(uint16,bytes32)")
	neuron, registerJump := dispatch(neuron, "registerLimit(uint16,bytes32,uint64)")
	neuron = append(neuron, 0x60, 0x00, 0x60, 0x00, 0xfd)
	neuron[uidJump] = byte(len(neuron))
	// The UID word stays zero until registration; exists comes from storage.
	neuron = append(neuron, 0x5b, 0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x00, 0x54, 0x61, byte(uid>>8), byte(uid), 0x02, 0x60, 0x20, 0x52, 0x60, 0x40, 0x60, 0x00, 0xf3)
	neuron[registerJump] = byte(len(neuron))
	neuron = append(neuron, 0x5b, 0x60, 0x01, 0x60, 0x00, 0x55, 0x00)
	f.state.SetCode(common.HexToAddress("0x804"), neuron, tracing.CodeChangeUnspecified)
	metagraph, hotkeyJump := dispatch(nil, "getHotkey(uint16,uint16)")
	metagraph, coldkeyJump := dispatch(metagraph, "getColdkey(uint16,uint16)")
	metagraph = append(metagraph, 0x60, 0x00, 0x60, 0x00, 0xfd)
	mirror := ss58.EvmMirrorPubkey(vault)
	for _, branch := range []struct {
		jump int
		word [32]byte
	}{{jump: hotkeyJump, word: [32]byte{32}}, {jump: coldkeyJump, word: mirror}} {
		metagraph[branch.jump] = byte(len(metagraph))
		metagraph = append(metagraph, 0x5b, 0x7f)
		metagraph = append(metagraph, branch.word[:]...)
		metagraph = append(metagraph, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3)
	}
	f.state.SetCode(common.HexToAddress("0x802"), metagraph, tracing.CodeChangeUnspecified)
	f.state.SetBalance(sender, uint256.NewInt(10_000_000_000_000), tracing.BalanceChangeUnspecified)
	for hash := range f.history.states {
		f.history.states[hash] = f.state.Copy()
	}
	return f
}

// Three real completed ancestors precede the first escrow signature import.
func (self *evmCreateFixture) prepareEscrowPrerequisites() {
	self.t.Helper()
	self.prepareCoordinatorSigned()
	if _, code, diagnostic := self.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
		self.t.Fatal(diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "coordinator-create", "--online")
	if code != 0 || result.Status != "coordinator-created" {
		self.t.Fatalf("coordinator prerequisite: %+v %d %s", result, code, diagnostic)
	}
	plan, err := selectEvmCreatePlan(context.Background(), *self.plan.Reserve, "escrow-register", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan, self.receipt = plan, nil
	self.signSelectedAction()
}

// Offline preparation retains only the fourth original approved envelope.
func (self *evmCreateFixture) prepareEscrowSigned() {
	self.t.Helper()
	self.prepareEscrowPrerequisites()
	counts := maps.Clone(self.counts)
	if result, code, diagnostic := self.command("apply", "--action", "escrow-register"); code != 0 || result.Status != "signature-awaiting-import" || result.EscrowRegistration == nil {
		self.t.Fatalf("escrow preparation: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "escrow-register", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() || !maps.Equal(counts, self.counts) {
		self.t.Fatalf("escrow import: %+v %d %s", result, code, diagnostic)
	}
}

// Each retained ancestor is opened under its own held lock. Tests use the same
// ordering as the command for crash boundaries and exact lineage mutations.
func (self *evmCreateFixture) openEscrowAncestors() ([]*evmActionStore, []evmActionRecord) {
	self.t.Helper()
	stores := []*evmActionStore{}
	records := []evmActionRecord{}
	for index := 0; index < 3; index++ {
		var store *evmActionStore
		var err error
		switch index {
		case 0:
			store, err = openEvmActionStore(self.config, false, nil)
		case 1:
			store, err = openEvmVaultActionStore(*self.plan.Vault, records[0], false, nil)
		case 2:
			store, err = openEvmCoordinatorActionStore(*self.plan.Coordinator, records[0], records[1], false, nil)
		}
		if err != nil {
			self.t.Fatal(err)
		}
		self.t.Cleanup(func() { _ = store.close() })
		record, err := store.load()
		if err != nil {
			self.t.Fatal(err)
		}
		stores, records = append(stores, store), append(records, record)
	}
	return stores, records
}

// Test calldata words are independent of the production precompile reader.
func evmEscrowTestWord(value uint16) string {
	word := make([]byte, 32)
	binary.BigEndian.PutUint16(word[30:], value)
	return "0x" + hex.EncodeToString(word)
}
