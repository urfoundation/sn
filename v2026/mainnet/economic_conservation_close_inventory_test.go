// Public original receipt acquisition and cold admission retain signed report
// chains without mistaking a complete contract chain for complete traffic truth.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Each actual terminal original gets its own same-key inventory companion.
func economicCloseInventoryTestSign(t *testing.T, census *payoutartifact.ClosedWorkReports) {
	t.Helper()
	census.Schema = payoutartifact.ClosedWorkInventoryReportsSchema
	for index := range census.Reports {
		row := &census.Reports[index]
		original, err := coreprotocol.DecodeOriginalCloseReport(row.Original)
		if err != nil {
			t.Fatal(err)
		}
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(40 + index)}, 32))
		inventory, err := coreprotocol.SignOriginalCloseInventory(coreprotocol.OriginalCloseInventory{DomainHash: original.DomainHash, ClientId: original.ClientId, ContractId: original.ContractId, ReportHash: sha256.Sum256(row.Original), Sequence: 1, CumulativeAckedBytes: original.AckedByteCount, Terminal: true}, key)
		if err != nil {
			t.Fatal(err)
		}
		row.Inventory, err = inventory.Bytes()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestEconomicCloseInventoryPublicOriginalChainsRemainComponentEvidence(t *testing.T) {
	f := configureEconomicCloseReports(t, newEconomicConservationFixture(t, false), func(c *payoutartifact.ClosedWorkReports) { economicCloseInventoryTestSign(t, c) })
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 0 || record.Census == nil || record.Census.ClosedWork == nil || record.Census.ClosedWork.CompleteReportInventories != 2 || summary.OriginalEntitlements.InventoryReports != 4 || summary.OriginalEntitlements.CompleteReportInventories != 2 {
		t.Fatal("public original report inventory was not reconstructed", code, issue, record)
	}
	if summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil || !summary.NativeCurrent || !summary.VaultCurrent {
		t.Fatal("complete contract chains replaced missing whole-provider authority", summary)
	}
}

func TestEconomicCloseInventoryPublicMissingAndForeignRemainIsolated(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		f := configureEconomicCloseReports(t, newEconomicConservationFixture(t, false), func(c *payoutartifact.ClosedWorkReports) {
			economicCloseInventoryTestSign(t, c)
			if corrupt {
				c.Reports[0].Inventory[len(c.Reports[0].Inventory)-1] ^= 1
			} else {
				c.Reports[0].Inventory = nil
			}
		})
		economicEntitlementFirstPage(t, f)
		summary, code, issue := f.source.run(t, monitorServiceHooks{})
		record := economicEntitlementRecord(t, f.source)
		if corrupt {
			if code != 3 || !record.CensusHeld || record.Census != nil || !strings.Contains(record.CensusIssue, "inventory") {
				t.Fatal("false inventory was not scoped to entitlement", code, issue, record)
			}
		} else if code != 0 || record.Census == nil || summary.OriginalEntitlements.CompleteReportInventories != 0 {
			t.Fatal("missing companion became a complete chain", code, issue, record)
		}
		if !summary.NativeCurrent || !summary.VaultCurrent || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil {
			t.Fatal("optional inventory stopped siblings or claimed economic authority", summary)
		}
	}
}

func TestEconomicCloseInventoryPublicArchiveRetainsOriginalChain(t *testing.T) {
	var entitlement *economicEntitlementFixture
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) {
		entitlement = configureEconomicCloseReports(t, source, func(c *payoutartifact.ClosedWorkReports) { economicCloseInventoryTestSign(t, c) })
	})
	f.sample(t, monitorServiceHooks{})
	before := economicEntitlementRecord(t, f.source)
	if before.Census == nil || before.Census.ClosedWork == nil || before.Census.ClosedWork.CompleteReportInventories != 2 {
		t.Fatal("original chain never reached cold admission")
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public inventory archive", code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	after := economicEntitlementRecord(t, f.source)
	if after.Census != nil || after.CensusReference == nil || after.CensusReference.ClosedWork != *before.Census.ClosedWork || summary.OriginalEntitlements.CompleteReportInventories != 2 || entitlement.artifactReads.Load() != 2 || summary.TargetMet != nil {
		t.Fatal("cold original inventory changed or was reacquired", summary, after)
	}
}

// The window is built before artifact signing and before all real receipt tries.
func economicCloseWindowTestConfigure(t *testing.T, source *economicConservationFixture, omit bool, changes ...func(*payoutartifact.ClosedWorkCensus)) *economicEntitlementFixture {
	t.Helper()
	return configureEconomicClosedWorkFixture(t, source, func(c *payoutartifact.ClosedWorkCensus) {
		for _, change := range changes {
			change(c)
		}
		window := &payoutartifact.ClosedWorkWindow{Schema: payoutartifact.ClosedWorkWindowSchema, Start: c.WindowStart, End: c.WindowEnd, Records: []payoutartifact.ClosedWorkWindowRecord{}}
		for _, row := range c.Records {
			id, closed := row.ContractId, row.ClosedAt
			window.Records = append(window.Records, payoutartifact.ClosedWorkWindowRecord{ContractId: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), Disposition: "credited", ClosedAt: &closed, Original: bytes.Clone(row.Original)})
		}
		if omit {
			window.Records = window.Records[1:]
		}
		raw, err := json.Marshal(payoutartifact.ClosedWorkReports{Schema: payoutartifact.ClosedWorkInventoryReportsSchema, Window: window, Reports: []payoutartifact.ClosedWorkReport{}})
		if err != nil {
			t.Fatal(err)
		}
		c.Records[0].OriginalReports = raw
	})
}

func TestEconomicCloseWindowPublicOriginalClockCannotBeClaimedByArtifact(t *testing.T) {
	f := economicCloseWindowTestConfigure(t, newEconomicConservationFixture(t, false), false)
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 0 || record.Census == nil || record.Census.WindowClock == nil || record.Census.ClosedWork == nil || record.Census.ClosedWork.WindowHash == "" || summary.OriginalEntitlements.ClosedWorkWindows != 1 {
		t.Fatal("public window lost its original boundary read", code, issue, record)
	}
	if record.Census.ClosedWork.WindowClockMatched || summary.OriginalEntitlements.ClockMatchedWindows != 0 || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil {
		t.Fatal("artifact clock assertion replaced original epoch clock", summary)
	}
}

func TestEconomicCloseWindowPublicOmittedCreditHoldsOnlyAffectedCensus(t *testing.T) {
	f := economicCloseWindowTestConfigure(t, newEconomicConservationFixture(t, false), true)
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 3 || !record.CensusHeld || record.Census != nil || !strings.Contains(record.CensusIssue, "window omitted an original earning contract") || !summary.NativeCurrent || !summary.VaultCurrent {
		t.Fatal("omitted window original gained authority or stopped siblings", code, issue, record)
	}
}

func TestEconomicCloseWindowRetainedClockCannotSelfSealAuthority(t *testing.T) {
	f := economicCloseWindowTestConfigure(t, newEconomicConservationFixture(t, false), false)
	economicEntitlementFirstPage(t, f)
	if _, code, issue := f.source.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	record := economicEntitlementRecord(t, f.source)
	census := record.Census
	if census == nil || census.WindowClock == nil || census.ClosedWork == nil {
		t.Fatal("original window clock was never retained")
	}
	census.WindowClock.StartTime, _ = time.Parse(time.RFC3339Nano, census.Artifact.ClosedWork.WindowStart)
	census.WindowClock.EndTime, _ = time.Parse(time.RFC3339Nano, census.Artifact.ClosedWork.WindowEnd)
	census.ClosedWork.WindowClockMatched = true
	census.ContentHash = census.hash()
	if err := census.validate(t.Context(), f.source.policy, record); err == nil || !strings.Contains(err.Error(), "counters differ") {
		t.Fatal("self-sealed timestamps replaced exact original epoch headers", err)
	}
}

// Snow's finalized Finney block 9,278,937 stores 1,791,949,860,000 ms, and its
// 12-second Aura slots leave no remainder. The vault blocks keep that cadence.
func economicCloseWindowMainnetTimes(t *testing.T) func(*monitorEvmFixture) {
	t.Helper()
	return func(vault *monitorEvmFixture) {
		for number, block := range vault.blocks {
			block.header.Time = 1791949860000 + number*12000
		}
		economicConservationTestEvmRehash(t, vault, nil)
	}
}

// PH-15: the public clock reads raw Frontier milliseconds at mainnet scale.
// Boundary blocks 0 and 10 store 1,791,949,860,000 and 1,791,949,980,000 ms,
// 2026-10-14T03:51:00Z and 03:53:00Z. A window of exactly those instants is
// matched, persisted, reloaded and revalidated on restart without a new read.
func TestEconomicCloseWindowPublicClockMatchesMainnetMilliseconds(t *testing.T) {
	f := economicCloseWindowTestConfigure(t, newEconomicConservationFixture(t, false, economicCloseWindowMainnetTimes(t)), false, func(c *payoutartifact.ClosedWorkCensus) {
		c.WindowStart, c.WindowEnd = "2026-10-14T03:51:00Z", "2026-10-14T03:53:00Z"
		for index := range c.Records {
			c.Records[index].ClosedAt = "2026-10-14T03:52:00Z"
		}
	})
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 0 || record.Census == nil || record.Census.WindowClock == nil || record.Census.ClosedWork == nil {
		t.Fatal("mainnet millisecond clock was not retained", code, issue, record)
	}
	clock := record.Census.WindowClock
	if clock.HeaderProfile != payoutartifact.FrontierWindowClockProfile || !clock.StartTime.Equal(time.Date(2026, time.October, 14, 3, 51, 0, 0, time.UTC)) || !clock.EndTime.Equal(time.Date(2026, time.October, 14, 3, 53, 0, 0, time.UTC)) {
		t.Fatal("raw Frontier milliseconds lost their original instants", clock)
	}
	if !record.Census.ClosedWork.WindowClockMatched || summary.OriginalEntitlements.ClockMatchedWindows != 1 || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil {
		t.Fatal("exact original window was not matched or became provider authority", summary)
	}
	if err := record.Census.validate(t.Context(), f.source.policy, record); err != nil {
		t.Fatal("reloaded millisecond census failed its own validation", err)
	}
	before := record.Census.ContentHash
	again, code, issue := f.source.run(t, monitorServiceHooks{})
	if code != 0 || f.artifactReads.Load() != 2 || economicEntitlementRecord(t, f.source).Census.ContentHash != before || again.OriginalEntitlements.ClockMatchedWindows != 1 {
		t.Fatal("restart lost or reacquired the retained millisecond clock", code, issue, again)
	}
}

// PH-15: a retained public clock is the mapped Frontier read. Without this
// refusal an absent or unknown profile would silently re-verify as unmatched.
func TestEconomicCloseWindowRetainedClockRefusesAnotherHeaderProfile(t *testing.T) {
	f := economicCloseWindowTestConfigure(t, newEconomicConservationFixture(t, false), false)
	economicEntitlementFirstPage(t, f)
	if _, code, issue := f.source.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	record := economicEntitlementRecord(t, f.source)
	if record.Census == nil || record.Census.WindowClock == nil || record.Census.ClosedWork == nil || record.Census.ClosedWork.WindowClockMatched {
		t.Fatal("unmatched original window clock was never retained")
	}
	if err := record.Census.validate(t.Context(), f.source.policy, record); err != nil {
		t.Fatal("retained Frontier clock failed its own validation", err)
	}
	for _, profile := range []string{"", "frontier-legacy-rlp15-seconds"} {
		census, clock := *record.Census, *record.Census.WindowClock
		clock.HeaderProfile = profile
		census.WindowClock = &clock
		census.ContentHash = census.hash()
		if err := census.validate(t.Context(), f.source.policy, record); err == nil || !strings.Contains(err.Error(), "declares another header profile") {
			t.Fatal("retained clock was reread under another unit", profile, err)
		}
	}
}

// PH-15: a provider census keeps the clock its producer declared, including the
// ordinary EVM seconds profile; only a public clock must be the mapped Frontier.
func TestEconomicCloseWindowProviderClockKeepsProducerProfile(t *testing.T) {
	for _, profile := range []string{"", payoutartifact.FrontierWindowClockProfile} {
		clock := &payoutartifact.ClosedWorkWindowClock{HeaderProfile: profile}
		census := &economicConservationEntitlementCensus{ProviderOriginals: &economicProviderOriginals{Work: &payoutartifact.WholeWorkInventory{Clock: clock}}, WindowClock: clock}
		if err := census.validateClosedWork(t.Context(), true); err != nil {
			t.Fatal("provider path reread its producer-declared clock", profile, err)
		}
	}
	census := &economicConservationEntitlementCensus{WindowClock: &payoutartifact.ClosedWorkWindowClock{}}
	if err := census.validateClosedWork(t.Context(), false); err == nil || !strings.Contains(err.Error(), "declares another header profile") {
		t.Fatal("public path accepted an undeclared clock profile", err)
	}
}
