// Synthetic root approvers bind public service configuration only. The root
// role fixture never supplies native custody, RPC or an activated service port.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"testing"
)

// The service approver deliberately differs from the original action approver
// so the independent role pins cannot accidentally be conflated.
type bootstrapChainRootFixture struct {
	chain    *bootstrapChainFixture
	path     string
	private  ed25519.PrivateKey
	approval bootstrapChainRootApproval
}

// Explicit role selection comes from the fixture's independent chain input.
func newBootstrapChainRootFixture(t *testing.T, chain *bootstrapChainFixture) *bootstrapChainRootFixture {
	t.Helper()
	scope := chain.root.plan.Service.Packet.Action.Scope
	f := &bootstrapChainRootFixture{chain: chain, path: filepath.Join(filepath.Dir(chain.path), "root-service-approval.json"),
		private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x7c}, ed25519.SeedSize))}
	chain.config.RootValidator = &bootstrapChainRootValidator{Role: scope.Role, Netuid: new(uint16(0)), Implementation: "sn/mainnet/root-service",
		Hotkey: scope.Hotkey, Coldkey: scope.Coldkey, Seat: new(scope.Seat), Strategy: scope.Strategy,
		ActionApprovalPublicKey: chain.root.plan.Service.CustodyTrust.ApprovalPublicKey,
		ApprovalPublicKey:       "0x" + hex.EncodeToString(f.private.Public().(ed25519.PublicKey))}
	f.approve(t)
	return f
}

// A new complete approval is issued only when the test explicitly requests it.
func (self *bootstrapChainRootFixture) approve(t *testing.T) {
	t.Helper()
	self.approval = bootstrapChainRootApproval{Schema: bootstrapChainRootApprovalSchema, DeploymentId: self.chain.config.DeploymentId,
		RootPlanHash: self.chain.root.plan.ContentHash, ServiceConfigHash: rootObjectHash(self.chain.root.plan.Service)}
	self.sign(t)
}

// Negative tests can sign inconsistent facts to reach semantic binding checks.
func (self *bootstrapChainRootFixture) sign(t *testing.T) {
	t.Helper()
	message, err := self.approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	self.approval.Signature = hex.EncodeToString(ed25519.Sign(self.private, message))
	self.write(t)
}

// Updating the outer byte pin never repairs a missing or wrong signature.
func (self *bootstrapChainRootFixture) write(t *testing.T) {
	t.Helper()
	self.chain.config.RootValidator.Approval = bootstrapRootTestWrite(t, self.path, self.approval)
}

// Republishing a valid child configuration changes its exact plan, but does
// not silently grant another full-service approval or change independent keys.
func (self *bootstrapChainRootFixture) service(t *testing.T, config rootServiceConfig) {
	t.Helper()
	root := self.chain.root
	root.config.RootService = bootstrapRootTestWrite(t, root.config.RootService.Path, config)
	self.chain.config.Root = bootstrapRootTestWrite(t, root.configPath, root.config)
	var err error
	root.plan, err = loadBootstrapRootPlan(t.Context(), root.configPath)
	if err != nil {
		t.Fatal(err)
	}
}
