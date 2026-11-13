// Evidence fixtures extend the already approved graph without changing earlier
// identities. All eight effects execute reviewed bytecode in the local EVM.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Fixture constructor bytes come directly from the generated ABI and release;
// the production evidence-domain builder is not used to form its own input.
func newEvmEvidenceFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmVaultLinkFixture(t)
	var creation []byte
	for _, artifact := range evmTestRelease(t).Artifacts {
		if artifact.Name == "ValidatorEvidence" {
			var err error
			creation, err = hex.DecodeString(artifact.Creation)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(creation) == 0 {
		t.Fatal("reviewed evidence creation bytes are missing")
	}
	sender := f.config.Plan.Actions[0].Sender
	deployment := sha256.Sum256([]byte(f.config.Plan.DeploymentId))
	arguments := stabi.NewSTValidatorEvidence().PackConstructor(crypto.CreateAddress(sender, 4), common.HexToHash(f.config.Plan.Network.GenesisHash), deployment)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "evidence-create", Sender: sender, Nonce: 7, Data: "0x" + hex.EncodeToString(append(creation, arguments...)), ValueWei: "0", Gas: 5_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumAttempts, f.config.Plan.MaximumTotalWei = 8, "2243000000"
	f.publishConfig()
	return f
}

// Real vault binding is retained before the eighth action can acquire custody.
func (self *evmCreateFixture) prepareEvidencePrerequisites() {
	self.t.Helper()
	self.prepareVaultLinkSigned()
	if _, code, diagnostic := self.command("resume", "--action", "vault-link", "--online", "--submit"); code != 0 {
		self.t.Fatal(diagnostic)
	}
	if result, code, diagnostic := self.command("resume", "--action", "vault-link", "--online"); code != 0 || result.Status != "vault-coordinator-bound" {
		self.t.Fatalf("vault binding prerequisite: %+v %d %s", result, code, diagnostic)
	}
	plan, err := selectEvmCreatePlan(context.Background(), *self.plan.Reserve, "evidence-create", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan, self.receipt = plan, nil
	self.signSelectedAction()
}

// Offline preparation/import keeps the same original public transaction bytes.
func (self *evmCreateFixture) prepareEvidenceSigned() {
	self.t.Helper()
	self.prepareEvidencePrerequisites()
	counts := maps.Clone(self.counts)
	if result, code, diagnostic := self.command("apply", "--action", "evidence-create"); code != 0 || result.Status != "signature-awaiting-import" || result.EvidenceConstructor == nil || *result.EvidenceConstructor != *self.plan.EvidenceConstructor {
		self.t.Fatalf("evidence preparation: %+v %d %s", result, code, diagnostic)
	}
	if result, code, diagnostic := self.command("resume", "--action", "evidence-create", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash); code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() || !maps.Equal(counts, self.counts) {
		self.t.Fatalf("evidence import: %+v %d %s", result, code, diagnostic)
	}
}

// All seven ancestors remain locked while tests exercise the evidence journal.
func (self *evmCreateFixture) openEvidenceAncestors() ([]*evmActionStore, []evmActionRecord) {
	self.t.Helper()
	stores, records := self.openVaultLinkAncestors()
	store, err := openEvmVaultLinkActionStore(*self.plan.VaultLink, records[0], records[1], records[2], records[3], records[4], records[5], false, nil, self.storage.Context)
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
