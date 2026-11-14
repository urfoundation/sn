// Original receipt authorization joins client signatures through the public owner.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Finish original report, key and receipt bytes before the public sample starts.
// The fixture supplies no reliability, eligibility or canonical-window certificate.
func configureEconomicCloseReports(t *testing.T, source *economicConservationFixture, change func(*payoutartifact.ClosedWorkReports)) *economicEntitlementFixture {
	t.Helper()
	root, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	f := configureEconomicClosedWorkFixture(t, source, func(c *payoutartifact.ClosedWorkCensus) {
		domain, err := payoutartifact.ClosedWorkReportDomain(&payoutartifact.Artifact{DeploymentID: c.DeploymentId, ChainID: c.ChainId, GenesisHash: c.GenesisHash, Netuid: c.Netuid, Coordinator: c.Coordinator, SettlementVault: c.SettlementVault, NoID: c.NoId, PolicyHash: c.PolicyHash})
		if err != nil {
			t.Fatal(err)
		}
		domainHash, _ := domain.Digest()
		for index := range c.Records {
			row := &c.Records[index]
			var snapshot struct {
				ByteCount uint64 `json:"byte_count"`
			}
			if err := json.Unmarshal(row.Original, &snapshot); err != nil {
				t.Fatal(err)
			}
			census := payoutartifact.ClosedWorkReports{Schema: payoutartifact.ClosedWorkReportsSchema, Count: 2}
			for party := 0; party < 2; party++ {
				clientId, reportId := [16]byte{byte(party + 1)}, [16]byte{byte(index + 10), byte(party + 1)}
				key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(40 + party)}, 32))
				original, err := coreprotocol.SignOriginalCloseReport(coreprotocol.OriginalCloseReport{DomainHash: domainHash, ClientId: clientId, ContractId: row.ContractId, ReportId: reportId, AckedByteCount: snapshot.ByteCount}, key)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := original.Bytes()
				registration := protocol.ClientKeyRegistration{Domain: domain, ClientID: clientId, NetworkID: [16]byte{byte(50 + party)}, Generation: 1, Present: true, PublicKey: [32]byte(key[32:]), EffectiveBoundary: protocol.ClientKeyEffectiveBoundary{Epoch: c.Epoch, Block: 1, Hash: [32]byte{22}}}
				if err := protocol.SignClientKeyRegistration(&registration, root); err != nil {
					t.Fatal(err)
				}
				registered, _ := registration.Bytes()
				amount, unacked, checkpoint := snapshot.ByteCount, uint64(0), false
				partyName := "source"
				if party == 1 {
					partyName = "destination"
				}
				idText := func(id [16]byte) string {
					return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
				}
				census.Reports = append(census.Reports, payoutartifact.ClosedWorkReport{ClientId: idText(clientId), ReportId: idText(reportId), Party: partyName, AckedBytes: &amount, UnackedBytes: &unacked, Checkpoint: &checkpoint, Original: raw, KeyRegistration: registered})
			}
			if change != nil {
				change(&census)
			}
			row.OriginalReports, err = json.Marshal(census)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	f.rootSigner = crypto.PubkeyToAddress(root.PublicKey)
	economicConservationTestEvmRehash(t, source.vault, func(number uint64, receipt *types.Receipt) {
		if number == 11 {
			receipt.Logs[len(receipt.Logs)-1] = monitorEvmTestLog(t, f.coordinator, f.address, "OperatorRootCommitted", big.NewInt(3), big.NewInt(1), f.committedRoot, f.committedHash, f.rootSigner)
		}
	})
	economicConservationTestNativeMapping(t, source.native, source.vault)
	nativeExecutionTestConfigure(t, source.native, nil)
	source.policy.Native.Observation = source.native.policy
	source.policy.Vault = source.vault.policy
	source.policy.Vault.BatchBlocks = 1
	source.writePolicy(t)
	return f
}

// The original on-chain committer, not the artifact publisher, authenticates
// registration. The remaining whole-provider obligations still prevent TargetMet.
func TestEconomicCloseReportsPublicOriginalRootJoinsClientSignatures(t *testing.T) {
	f := configureEconomicCloseReports(t, newEconomicConservationFixture(t, false), nil)
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 0 || record.Census == nil || record.Census.ClosedWork == nil || record.Census.ClosedWork.SignedCloseReports != 4 || record.Census.ClosedWork.RegisteredCloseReports != 4 || record.Census.ClosedWork.CloseAmountJoins != 2 {
		t.Fatal("public original root/client signature join failed", code, issue, record)
	}
	if summary.OriginalEntitlements.SignedCloseReports != 4 || summary.OriginalEntitlements.RegisteredCloseReports != 4 || summary.OriginalEntitlements.CloseAmountJoins != 2 || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil {
		t.Fatal("individual report evidence became whole provider conformance", summary)
	}
}

// Missing/future evidence leaves independently healthy native/vault/Claim work
// running and cannot authenticate a report through the containing payout root.
func TestEconomicCloseReportsPublicMissingAndFutureStayUnknown(t *testing.T) {
	for _, change := range []func(*payoutartifact.ClosedWorkReports){
		func(c *payoutartifact.ClosedWorkReports) { c.Schema = "synthetic-future-close-reports-v2" },
		func(c *payoutartifact.ClosedWorkReports) {
			for i := range c.Reports {
				c.Reports[i].Original, c.Reports[i].KeyRegistration = nil, nil
			}
		},
	} {
		f := configureEconomicCloseReports(t, newEconomicConservationFixture(t, false), change)
		economicEntitlementFirstPage(t, f)
		summary, code, issue := f.source.run(t, monitorServiceHooks{})
		record := economicEntitlementRecord(t, f.source)
		if code != 0 || record.Census == nil || record.CensusHeld || !summary.NativeCurrent || !summary.VaultCurrent || summary.OriginalEntitlements.RegisteredCloseReports != 0 || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil {
			t.Fatal("missing optional client source blocked siblings or acquired authority", code, issue, record)
		}
	}
}

// Exact signed receipt/root agreement cannot hide a changed client report.
func TestEconomicCloseReportsPublicFalseOriginalHoldsOnlyEntitlement(t *testing.T) {
	f := configureEconomicCloseReports(t, newEconomicConservationFixture(t, false), func(c *payoutartifact.ClosedWorkReports) { c.Reports[0].Original[len(c.Reports[0].Original)-1] ^= 1 })
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 3 || !record.CensusHeld || record.Census != nil || !strings.Contains(record.CensusIssue, "client signature differs") || !summary.NativeCurrent || !summary.VaultCurrent || len(f.source.state(t).Payments) != 1 {
		t.Fatal("false client original gained root authority or stopped healthy sources", code, issue, record)
	}
}

// Cold counters are rederived from the exact archived signed originals under
// the original root; a restart does not replace them with current registrations.
func TestEconomicCloseReportsPublicArchiveRetainsOriginalAuthority(t *testing.T) {
	var entitlement *economicEntitlementFixture
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) { entitlement = configureEconomicCloseReports(t, source, nil) })
	f.sample(t, monitorServiceHooks{})
	before := economicEntitlementRecord(t, f.source)
	if before.Census == nil || before.Census.ClosedWork == nil || before.Census.ClosedWork.RegisteredCloseReports != 4 {
		t.Fatal("original client reports never reached archive admission")
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public signed close archive", code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	after := economicEntitlementRecord(t, f.source)
	if after.Census != nil || after.CensusReference == nil || after.CensusReference.ClosedWork != *before.Census.ClosedWork || summary.OriginalEntitlements.RegisteredCloseReports != 4 || entitlement.artifactReads.Load() != 2 || summary.TargetMet != nil {
		t.Fatal("cold original client authority changed or was reread", summary, after)
	}
	if common.HexToAddress(before.Census.RootSigner) == before.Census.Artifact.Signer {
		t.Fatal("fixture did not distinguish root from artifact authority")
	}
}
