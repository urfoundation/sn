// Proxy fixtures execute the real constructor's delegatecall and initialized
// coordinator against the completed escrow model. All identities are synthetic.
package main

import (
	"context"
	"encoding/hex"
	"maps"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Nonzero ignored policy words prove that approval bytes survive normalization;
// initialization observes the EVM block and rewrites only those two fields.
func newEvmProxyFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmEscrowFixture(t, 0)
	sender := f.config.Plan.Actions[0].Sender
	var proxy contractReleaseArtifact
	for _, artifact := range evmTestRelease(t).Artifacts {
		if artifact.Name == "ERC1967Proxy" {
			proxy = artifact
		}
	}
	parsed, err := abi.JSON(strings.NewReader(proxy.Abi))
	if err != nil {
		t.Fatal(err)
	}
	policy := stabi.STCoordinatorPolicySnapshot{PolicyHash: [32]byte{77}, EffectiveEpoch: 99, EffectiveBlock: 999, EpochBlocks: 3600, RootCommitWindowBlocks: 60, FinalizeOffsetBlocks: 120, CloseGraceBlocks: 30, ClaimTTLEpochs: 2, ClaimGraceEpochs: 1, MaximumBindingValidityEpochs: 4, CommitmentMaxAgeBlocks: 7200, EpochDepositCapRao: big.NewInt(1_000_000), CampaignDepositCapRao: big.NewInt(1_000_000_000)}
	initializer := stabi.NewSTCoordinator().PackInitialize(25, common.Address{41}, common.Address{42}, ss58.EvmMirrorPubkey(crypto.CreateAddress(sender, 4)), crypto.CreateAddress(sender, 1), crypto.CreateAddress(sender, 0), common.Address{43}, policy)
	arguments, err := parsed.Pack("", crypto.CreateAddress(sender, 2), initializer)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "proxy-create", Sender: sender, Nonce: 4, Data: "0x" + proxy.Creation + hex.EncodeToString(arguments), ValueWei: "0", Gas: 2_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumAttempts, f.config.Plan.MaximumTotalWei = 6, "2190000000"
	f.publishConfig()
	return f
}

// The escrow call must genuinely execute and retain its canonical event/mapping
// outcome before any proxy journal or original signature import is available.
func (self *evmCreateFixture) prepareProxyPrerequisites() {
	self.t.Helper()
	self.prepareEscrowSigned()
	if _, code, diagnostic := self.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		self.t.Fatal(diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "escrow-register", "--online")
	if code != 0 || result.Status != "escrow-registered" {
		self.t.Fatalf("escrow prerequisite: %+v %d %s", result, code, diagnostic)
	}
	plan, err := selectEvmCreatePlan(context.Background(), *self.plan.Reserve, "proxy-create", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan, self.receipt = plan, nil
	self.signSelectedAction()
}

// Offline child intent/import performs no route reads after historical custody.
func (self *evmCreateFixture) prepareProxySigned() {
	self.t.Helper()
	self.prepareProxyPrerequisites()
	counts := maps.Clone(self.counts)
	if result, code, diagnostic := self.command("apply", "--action", "proxy-create"); code != 0 || result.Status != "signature-awaiting-import" || result.ProxyAddress != self.plan.Address.Hex() || result.ProxyConstructor == nil {
		self.t.Fatalf("proxy preparation: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "proxy-create", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() || !maps.Equal(counts, self.counts) {
		self.t.Fatalf("proxy import: %+v %d %s", result, code, diagnostic)
	}
}

// Tests open the same four held ancestor locks as the command, preserving the
// exact graph prefix for deterministic publication and missing-custody faults.
func (self *evmCreateFixture) openProxyAncestors() ([]*evmActionStore, []evmActionRecord) {
	self.t.Helper()
	stores, records := self.openEscrowAncestors()
	store, err := openEvmEscrowActionStore(*self.plan.Escrow, records[0], records[1], records[2], false, nil)
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
