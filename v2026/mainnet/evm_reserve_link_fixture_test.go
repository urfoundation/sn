// Binding fixtures execute the reviewed reserve call after all five original
// predecessors, using real EVM runtime and only synthetic identities.
package main

import (
	"context"
	"encoding/hex"
	"maps"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The full approved prefix is sealed before any ancestor journal is created.
func newEvmReserveLinkFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmProxyFixture(t)
	sender := f.config.Plan.Actions[0].Sender
	reserve, proxy := crypto.CreateAddress(sender, 0), crypto.CreateAddress(sender, 4)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "reserve-link", Sender: sender, Nonce: 5, To: &reserve, Data: "0x" + hex.EncodeToString(stabi.NewSTReserveSink().PackSetRecorderOnce(proxy)), ValueWei: "0", Gas: 150_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumAttempts, f.config.Plan.MaximumTotalWei = 7, "2191500000"
	f.publishConfig()
	return f
}

// Real atomic initialization is retained before reserve-link can acquire any
// custody. The historical proxy policy keeps its original EVM block number.
func (self *evmCreateFixture) prepareReserveLinkPrerequisites() {
	self.t.Helper()
	self.prepareProxySigned()
	if _, code, diagnostic := self.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		self.t.Fatal(diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "proxy-create", "--online")
	if code != 0 || result.Status != "proxy-created-initialized" {
		self.t.Fatalf("proxy prerequisite: %+v %d %s", result, code, diagnostic)
	}
	plan, err := selectEvmCreatePlan(context.Background(), *self.plan.Reserve, "reserve-link", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan, self.receipt = plan, nil
	self.signSelectedAction()
}

// Preparing and importing the original binding signature never opens its route.
func (self *evmCreateFixture) prepareReserveLinkSigned() {
	self.t.Helper()
	self.prepareReserveLinkPrerequisites()
	counts := maps.Clone(self.counts)
	if result, code, diagnostic := self.command("apply", "--action", "reserve-link"); code != 0 || result.Status != "signature-awaiting-import" || result.ReserveBinding == nil || *result.ReserveBinding != *self.plan.ReserveBinding {
		self.t.Fatalf("reserve binding preparation: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "reserve-link", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() || !maps.Equal(counts, self.counts) {
		self.t.Fatalf("reserve binding import: %+v %d %s", result, code, diagnostic)
	}
}

// Publication faults borrow the same five retained locks as the real command.
func (self *evmCreateFixture) openReserveLinkAncestors() ([]*evmActionStore, []evmActionRecord) {
	self.t.Helper()
	stores, records := self.openProxyAncestors()
	store, err := openEvmProxyActionStore(*self.plan.Proxy, records[0], records[1], records[2], records[3], false, nil, self.storage.Context)
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

// Faults distinguish the current mapped EVM head from all historical receipts.
func (self *evmCreateFixture) newestEvmHash() string {
	var newest uint64
	var result string
	for hash, header := range self.evmHeaders {
		if number := header.Number.Uint64(); number >= newest {
			newest, result = number, hash
		}
	}
	return result
}
