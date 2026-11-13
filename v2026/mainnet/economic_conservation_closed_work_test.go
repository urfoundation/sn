// Public original receipt/artifact readers derive this limited database
// component. Equal totals and root agreement never supply missing provenance.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

// Finish the synthetic signed originals before public owners start. Existing
// transaction/receipt tries and native mapping are regenerated from those bytes.
func configureEconomicClosedWorkFixture(t *testing.T, source *economicConservationFixture, change func(*payoutartifact.ClosedWorkCensus)) *economicEntitlementFixture {
	t.Helper()
	f := configureEconomicEntitlementFixture(t, source)
	artifact := f.artifact
	census := &payoutartifact.ClosedWorkCensus{Schema: payoutartifact.ClosedWorkSchema, DeploymentId: artifact.DeploymentID, ChainId: artifact.ChainID, GenesisHash: artifact.GenesisHash, Netuid: artifact.Netuid, Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, Epoch: artifact.Epoch, NoId: artifact.NoID, PolicyHash: artifact.PolicyHash, Start: artifact.Start, End: artifact.End, WindowStart: "2026-10-06T00:00:00Z", WindowEnd: "2026-10-06T01:00:00Z", Count: uint64(len(artifact.Providers))}
	for index := range artifact.Providers {
		provider := &artifact.Providers[index]
		provider.NetworkID = [16]byte{byte(50 + index)}
		client := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", provider.ClientID[:4], provider.ClientID[4:6], provider.ClientID[6:8], provider.ClientID[8:10], provider.ClientID[10:])
		network := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", provider.NetworkID[:4], provider.NetworkID[4:6], provider.NetworkID[6:8], provider.NetworkID[8:10], provider.NetworkID[10:])
		census.Records = append(census.Records, payoutartifact.ClosedWorkRecord{ContractId: [16]byte{byte(index + 1)}, ClosedAt: "2026-10-06T00:30:00Z", Original: []byte(fmt.Sprintf(`{"version":1,"byte_count":%d,"providers":[{"client_id":%q,"network_id":%q,"byte_count":%d}]}`, provider.UsageBytes, client, network, provider.UsageBytes))})
	}
	if change != nil {
		change(census)
	}
	artifact.ClosedWork = census
	artifact.ProviderSnapshotHash = payoutartifact.SnapshotHash(artifact.Providers)
	key, err := crypto.HexToECDSA(strings.Repeat("26", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	f.raw, err = payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	f.committedRoot, f.committedHash = artifact.PayoutRoot, [32]byte(common.HexToHash(strings.TrimPrefix(artifact.ContentHash, "sha256:")))
	vault := source.vault
	economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
		switch number {
		case 11:
			receipt.Logs[len(receipt.Logs)-1] = monitorEvmTestLog(t, f.coordinator, f.address, "OperatorRootCommitted", big.NewInt(3), big.NewInt(1), f.committedRoot, f.committedHash, f.rootSigner)
		case 12:
			receipt.Logs[0] = monitorEvmTestLog(t, vault.contract, common.HexToAddress(vault.policy.Address), "EntitlementFinalized", big.NewInt(3), big.NewInt(1), f.committedRoot, f.committedHash, big.NewInt(50), uint64(900))
		}
	})
	economicConservationTestNativeMapping(t, source.native, vault)
	nativeExecutionTestConfigure(t, source.native, nil)
	source.policy.Native.Observation = source.native.policy
	source.policy.Vault = vault.policy
	source.policy.Vault.BatchBlocks = 1
	source.writePolicy(t)
	return f
}

// Authenticated original math is useful, but the missing measurement producers
// still prevent a full provider or financial conformance conclusion.
func TestEconomicClosedWorkPublicOriginalRowsRemainComponentEvidence(t *testing.T) {
	f := configureEconomicClosedWorkFixture(t, newEconomicConservationFixture(t, false), nil)
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 0 || record.Census == nil || record.Census.ClosedWork == nil || record.Census.ClosedWork.Hash != f.artifact.ClosedWork.Hash() || record.Census.ClosedWork.Contracts != 2 || record.Census.ClosedWork.UsageBytes != 100 || f.artifactReads.Load() != 2 {
		t.Fatal("public source did not derive original closed-work component", code, issue, record)
	}
	if summary.OriginalEntitlements.ClosedWorkRoots != 1 || summary.OriginalEntitlements.ClosedWorkContracts != 2 || summary.OriginalEntitlements.ClosedWorkUsageBytes != "100" || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil {
		t.Fatal("signed original rows became whole measurement authority", summary)
	}
}

// Missing original data stays unknown while native/vault/Claim progress stays
// intact; the existing legacy entitlement neighbor covers complete omission.
func TestEconomicClosedWorkPublicPartialAndForeignStayUnknown(t *testing.T) {
	for _, change := range []func(*payoutartifact.ClosedWorkCensus){
		func(c *payoutartifact.ClosedWorkCensus) { c.NoId++ },
		func(c *payoutartifact.ClosedWorkCensus) { c.Schema = "synthetic-future-closed-work-v2" },
		func(c *payoutartifact.ClosedWorkCensus) { c.Count++ },
	} {
		f := configureEconomicClosedWorkFixture(t, newEconomicConservationFixture(t, false), change)
		economicEntitlementFirstPage(t, f)
		summary, code, issue := f.source.run(t, monitorServiceHooks{})
		record := economicEntitlementRecord(t, f.source)
		if code != 0 || record.Census == nil || record.Census.ClosedWork != nil || record.CensusHeld || summary.OriginalEntitlements.ClosedWorkRoots != 0 || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil || !summary.NativeCurrent || !summary.VaultCurrent {
			t.Fatal("unknown source component blocked healthy siblings or acquired authority", code, issue, record)
		}
	}
}

// Correct authorized root and total cannot hide different original recipients.
func TestEconomicClosedWorkPublicResignedWrongIdentityIsHeld(t *testing.T) {
	f := configureEconomicClosedWorkFixture(t, newEconomicConservationFixture(t, false), func(c *payoutartifact.ClosedWorkCensus) {
		c.Records[0].Original = bytes.Replace(c.Records[0].Original, []byte("32000000-0000"), []byte("34000000-0000"), 1)
	})
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 3 || !record.CensusHeld || record.Census != nil || !strings.Contains(record.CensusIssue, "identity or usage") || !summary.NativeCurrent || !summary.VaultCurrent || len(f.source.state(t).Payments) != 1 {
		t.Fatal("authorized root hid conflicting original provider identity or stopped siblings", code, issue, record)
	}
}

// A public archive/restart uses exact retained originals without another HTTP
// read; compact counters remain observations rather than a self-signed truth.
func TestEconomicClosedWorkPublicArchiveKeepsOriginalComponent(t *testing.T) {
	var entitlement *economicEntitlementFixture
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) {
		entitlement = configureEconomicClosedWorkFixture(t, source, nil)
	})
	f.sample(t, monitorServiceHooks{})
	before := economicEntitlementRecord(t, f.source)
	if before.Census == nil || before.Census.ClosedWork == nil {
		t.Fatal("archive fixture never reached original closed-work admission")
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public original closed-work archive", code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	after := economicEntitlementRecord(t, f.source)
	if after.Census != nil || after.CensusReference == nil || after.CensusReference.ClosedWork != *before.Census.ClosedWork || entitlement.artifactReads.Load() != 2 || summary.OriginalEntitlements.ClosedWorkRoots != 1 || summary.OriginalEntitlements.ClosedWorkUsageBytes != "100" || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil {
		t.Fatal("original archive lost or promoted closed-work component", summary, after)
	}
}

// The optional compact field is omitted for exact old reference bytes, while a
// forged new counter cannot validate against a real complete signed artifact.
func TestEconomicClosedWorkOriginalCountersCannotSelfSeal(t *testing.T) {
	legacy := economicConservationEntitlementReference{}
	raw, err := json.Marshal(legacy)
	if err != nil || bytes.Contains(raw, []byte("original_closed_work")) {
		t.Fatal("legacy compact reference gained new signed bytes", err)
	}
	f := configureEconomicClosedWorkFixture(t, newEconomicConservationFixture(t, false), nil)
	economicEntitlementFirstPage(t, f)
	if _, code, issue := f.source.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	record := economicEntitlementRecord(t, f.source)
	record.Census.ClosedWork.Contracts++
	record.Census.ContentHash = record.Census.hash()
	if err := record.Census.validate(t.Context(), f.source.policy, record); err == nil || !strings.Contains(err.Error(), "counters differ") {
		t.Fatal("self-sealed counters acquired authority without original rows", err)
	}
}
