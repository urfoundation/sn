// Original signed producer configs define each provider's complete key-history
// namespace. Inspection cannot fall back to a changed pathname or unsigned fields.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Independent expected fields match the operator registration domain grammar;
// an artifact signer, endpoint or mutable current UID never substitutes for NoID.
func TestProductionProviderDomainsUseCompleteOriginalOperatorNamespace(t *testing.T) {
	f := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := InspectProductionProviderDomains(t.Context(), f.path, raw)
	if err != nil || len(domains) != len(f.cfg.Operators) {
		t.Fatal("provider domains omitted original operator census", err)
	}
	for index, actual := range domains {
		op := f.cfg.Operators[index]
		expected := protocol.ClientKeyHistoryDomain{ChainID: f.cfg.ChainID, GenesisHash: common.HexToHash(f.cfg.GenesisHash), Netuid: f.cfg.Netuid,
			Coordinator: common.HexToAddress(f.cfg.Coordinator), SettlementVault: common.HexToAddress(f.cfg.SettlementVault), DeploymentIDHash: sha256.Sum256([]byte(f.cfg.DeploymentID)), PolicyHash: common.HexToHash(f.cfg.PolicyHash), NoID: op.NoID}
		if actual.Domain != expected || actual.NoId != op.NoID || actual.ApiUrl != op.APIURL || actual.ConnectUrl != op.ConnectURL {
			t.Fatal("provider signing namespace differs from original enrollment", index)
		}
		got, _ := actual.Domain.Digest()
		if got != releaseCloseReportDomain(f.cfg, op.NoID) {
			t.Fatal("provider and actual validator tunnel domains diverged")
		}
	}
}

// The shared immutable byte input is authoritative even if its pathname is
// replaced before invocation. The independent original approval is still read.
func TestProductionProviderDomainsConsumePinnedBytesAndRejectMissingApproval(t *testing.T) {
	f := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.path, []byte("schema_version: 3\ninvalid: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if domains, err := InspectProductionProviderDomains(t.Context(), f.path, raw); err != nil || len(domains) == 0 {
		t.Fatal("provider launch reopened a changed config instead of original bytes", err)
	}
	if err := os.Remove(f.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	if domains, err := InspectProductionProviderDomains(t.Context(), f.path, raw); err == nil || domains != nil {
		t.Fatal("unsigned decoded provider fields acquired original authority")
	}
}

// Cancellation is the actual caller's cause, not a new domain-integrity verdict.
func TestProductionProviderDomainsKeepCanceledAndAbsentOwners(t *testing.T) {
	f := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if domains, err := InspectProductionProviderDomains(ctx, f.path, raw); !errors.Is(err, context.Canceled) || domains != nil {
		t.Fatal("provider inspection lost original owner cancellation", err)
	}
	if domains, err := InspectProductionProviderDomains(nil, f.path, raw); err == nil || domains != nil {
		t.Fatal("provider inspection admitted an absent owner")
	}
}
