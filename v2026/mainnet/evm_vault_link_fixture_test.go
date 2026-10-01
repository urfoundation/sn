// Vault binding fixtures execute the reviewed setter after six genuine EVM
// predecessors; all configuration and identities remain synthetic.
package main

import (
	"context"
	"encoding/hex"
	"maps"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The seventh envelope belongs to the same graph before any ancestor custody.
func newEvmVaultLinkFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmReserveLinkFixture(t)
	sender := f.config.Plan.Actions[0].Sender
	vault, proxy := crypto.CreateAddress(sender, 1), crypto.CreateAddress(sender, 4)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "vault-link", Sender: sender, Nonce: 6, To: &vault, Data: "0x" + hex.EncodeToString(stabi.NewSTSettlementVault().PackSetCoordinatorOnce(proxy)), ValueWei: "0", Gas: 150_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumAttempts, f.config.Plan.MaximumTotalWei = 8, "2193000000"
	f.publishConfig()
	return f
}

// Real reserve binding is retained before the vault acquires its own journal.
func (self *evmCreateFixture) prepareVaultLinkPrerequisites() {
	self.t.Helper()
	self.prepareReserveLinkSigned()
	if _, code, diagnostic := self.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		self.t.Fatal(diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "reserve-link", "--online")
	if code != 0 || result.Status != "reserve-recorder-bound" {
		self.t.Fatalf("reserve binding prerequisite: %+v %d %s", result, code, diagnostic)
	}
	plan, err := selectEvmCreatePlan(context.Background(), *self.plan.Reserve, "vault-link", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan, self.receipt = plan, nil
	self.signSelectedAction()
	self.callIntrinsicGas = true
}

// Preparation/import keeps the original public signature offline.
func (self *evmCreateFixture) prepareVaultLinkSigned() {
	self.t.Helper()
	self.prepareVaultLinkPrerequisites()
	counts := maps.Clone(self.counts)
	if result, code, diagnostic := self.command("apply", "--action", "vault-link"); code != 0 || result.Status != "signature-awaiting-import" || result.VaultBinding == nil || *result.VaultBinding != *self.plan.VaultBinding {
		self.t.Fatalf("vault binding preparation: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "vault-link", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() || !maps.Equal(counts, self.counts) {
		self.t.Fatalf("vault binding import: %+v %d %s", result, code, diagnostic)
	}
}

// Tests retain all six ancestor locks while exercising the seventh journal.
func (self *evmCreateFixture) openVaultLinkAncestors() ([]*evmActionStore, []evmActionRecord) {
	self.t.Helper()
	stores, records := self.openReserveLinkAncestors()
	store, err := openEvmReserveLinkActionStore(*self.plan.ReserveLink, records[0], records[1], records[2], records[3], records[4], false, nil)
	if err != nil {
		self.t.Fatal(err)
	}
	self.t.Cleanup(func() { _ = store.close() })
	record, err := store.load()
	if err != nil {
		self.t.Fatal(err)
	}
	return append(stores, store), append(records, record)
}
