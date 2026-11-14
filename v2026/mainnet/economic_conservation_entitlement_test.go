// Public commands obtain the complete census through real receipt/header and
// artifact HTTP readers. Expected monetary values are independently chosen.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

func economicEntitlementRecord(t *testing.T, f *economicConservationFixture) economicConservationEntitlement {
	t.Helper()
	state := f.state(t)
	if err := state.index(); err != nil {
		t.Fatal(err)
	}
	index, found := state.entitlementIds["3/1"]
	if !found {
		t.Fatal("actual finalization did not create original entitlement")
	}
	return state.Entitlements[index]
}

func economicEntitlementFirstPage(t *testing.T, f *economicEntitlementFixture) {
	t.Helper()
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	if code != 0 || summary.VaultCursor.Number != 11 || !summary.NativeCurrent || f.artifactReads.Load() != 0 {
		t.Fatal("original missed-root page failed before artifact admission", code, issue, summary)
	}
}

func TestEconomicEntitlementPublicOriginalCommitterAndCompleteLeafCensus(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 0 || record.Census == nil || f.artifactReads.Load() != 2 {
		t.Fatal("public original entitlement did not complete actual receipt/artifact acquisition", code, issue, record)
	}
	census := record.Census
	if census.RootSigner != f.rootSigner.Hex() || census.Artifact.Signer == f.rootSigner || census.Artifact.PayoutRoot != f.committedRoot || census.LeafObligationsAlpha != "50" || census.FloorResidueAlpha != "0" || len(census.Artifact.Providers) != 2 || len(census.Artifact.Leaves) != 2 || census.Commitment.Block.Number != 11 || census.Finalization.Block.Number != 12 {
		t.Fatal("complete leaf census borrowed artifact signer, changed a leaf or lost original blocks", census)
	}
	if record.Funded != "0" || record.Total == nil || *record.Total != "50" || len(record.Sources) != 3 || record.Sources[2].Kind != "root-missed" || record.Sources[2].Id != "2/1" || record.Sources[2].Amount != "20" {
		t.Fatal("later zero-funded root lost original missed-root income", record)
	}
	if summary.OriginalEntitlements == nil || summary.OriginalEntitlements.CompleteRoots != 1 || summary.OriginalEntitlements.UnknownOriginalRoots != 1 || summary.OriginalEntitlements.RootsWithUnattributedFunding != 1 || summary.OriginalEntitlements.CompleteObservedCensus || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.OriginalEntitlements.IndependentFinalityAuthenticated || summary.OriginalEntitlements.NativeIncomeFundingAlpha != nil || summary.OriginalEntitlements.CapitalFundingAlpha != nil || summary.OriginalEntitlements.CapitalSubsidyAuthorized || summary.TargetMet != nil {
		t.Fatal("artifact mathematics promoted opening liabilities, usage, finality or economic conformance", summary)
	}
	before := census.ContentHash
	again, code, issue := f.source.run(t, monitorServiceHooks{})
	if code != 0 || f.artifactReads.Load() != 2 || economicEntitlementRecord(t, f.source).Census.ContentHash != before || again.VaultCursor.Number != 13 {
		t.Fatal("restart fetched or reassigned an already admitted original root", code, issue, again)
	}
}

func TestEconomicEntitlementPublicAcceptedClaimMustMatchOriginalLeaf(t *testing.T) {
	f := newEconomicEntitlementFixture(t, 8)
	economicEntitlementFirstPage(t, f)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 3 || record.Census != nil || !record.CensusHeld || !strings.Contains(record.CensusIssue, "complete original payout leaf census") || !summary.NativeCurrent || !summary.VaultCurrent || len(f.source.state(t).Payments) != 1 {
		t.Fatal("complete authorized artifact hid a different contract-accepted recipient share", code, issue, record)
	}
	if f.artifact.SharesTotalBPS != 10000 || f.artifact.Leaves[0].ShareBPS != 800 {
		t.Fatal("original artifact negative did not establish a valid complete distinct allocation")
	}
}

func TestEconomicEntitlementPublicForeignPoolCarryRefusesBeforeRead(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	economicEntitlementFirstPage(t, f)
	if _, code, issue := f.source.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("original funding baseline", code, issue)
	}
	state := f.source.state(t)
	if err := state.index(); err != nil {
		t.Fatal(err)
	}
	prior := state.entitlementIds["2/1"]
	foreign := state.Entitlements[prior]
	foreign.PoolId, foreign.Id = "2", "2/2"
	foreign.Sources = []economicConservationBacking{{Kind: "opening-funding-unattributed", Id: foreign.Id, Amount: foreign.Funded}}
	originalCarry := *foreign.CarryEvent
	originalCarry.Values = map[string]string{"epoch": "2", "noId": "2", "carried": "20"}
	foreign.CarryEvent = &originalCarry
	state.Entitlements = append(state.Entitlements, foreign)
	state.Entitlements[state.entitlementIds["3/1"]].Sources[2].Id = "2/2"
	// Remove the census so the independent source-pool check, rather than its
	// old commitment digest, must reject the self-sealed funding replacement.
	state.Entitlements[state.entitlementIds["3/1"]].Census = nil
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	ctx := monitorTestStorageContext(t, t.Context(), f.source.args(t))
	owner, err := openMonitorHistorySnapshot(ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(append(raw, '\n'), nil), owner.close()); err != nil {
		t.Fatal(err)
	}
	reads, claims := f.artifactReads.Load(), f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || f.artifactReads.Load() != reads || f.source.claimReads.Load() != claims || !strings.Contains(diagnostic.String(), "original source epoch and pool receipt") {
		t.Fatal("equal-valued carry crossed original operator pool authority", code, diagnostic.String())
	}
}

func TestEconomicEntitlementPublicMissingArtifactPreservesCreditAndRecovers(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	f.missing.Store(true)
	economicEntitlementFirstPage(t, f)
	before := f.source.state(t)
	summary, code, issue := f.source.run(t, monitorServiceHooks{})
	state := f.source.state(t)
	record := economicEntitlementRecord(t, f.source)
	if code != 3 || !summary.NativeCurrent || !summary.VaultCurrent || summary.VaultCursor.Number != 12 || summary.MatchedReceipts != 1 || record.Census != nil || record.CensusHeld || record.CensusIssue == "" || len(state.Payments) != 1 || len(state.Lots) != len(before.Lots) || state.EntitlementReadAfter["1"] != record.Id {
		t.Fatal("unavailable original artifact erased financial progress or blocked healthy siblings", code, issue, summary, record)
	}
	f.missing.Store(false)
	summary, code, issue = f.source.run(t, monitorServiceHooks{})
	record = economicEntitlementRecord(t, f.source)
	if code != 0 || record.Census == nil || record.CensusIssue != "" || record.CensusHeld || summary.AggregatePayments != 1 || len(f.source.state(t).Payments) != 1 {
		t.Fatal("same original root did not recover without replaying financial events", code, issue, record)
	}
}

func TestEconomicEntitlementPublicWrongCommitterOrRootIsHeld(t *testing.T) {
	for _, mode := range []string{"committer", "root", "http-authority"} {
		f := newEconomicEntitlementFixture(t)
		economicEntitlementFirstPage(t, f)
		switch mode {
		case "committer":
			f.wrongSigner.Store(true)
		case "root":
			f.wrongRoot.Store(true)
		case "http-authority":
			f.unauthorized.Store(true)
		}
		summary, code, issue := f.source.run(t, monitorServiceHooks{})
		record := economicEntitlementRecord(t, f.source)
		if code != 3 || record.Census != nil || !record.CensusHeld || record.CensusIssue == "" || !summary.NativeCurrent || !summary.VaultCurrent || summary.OriginalEntitlements.HeldRoots != 1 {
			t.Fatal("foreign entitlement authority did not hold only its original evidence", mode, code, issue, record)
		}
		reads := f.artifactReads.Load()
		f.wrongSigner.Store(false)
		f.wrongRoot.Store(false)
		f.unauthorized.Store(false)
		_, code, issue = f.source.run(t, monitorServiceHooks{})
		if code != 3 || f.artifactReads.Load() != reads || !economicEntitlementRecord(t, f.source).CensusHeld {
			t.Fatal("contradictory original authority was silently retried or replaced", mode, code, issue)
		}
	}
}

func TestEconomicEntitlementPublicEqualAggregateDifferentRecipientRefuses(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	economicEntitlementFirstPage(t, f)
	original := f.artifact
	providers := append([]payoutartifact.ProviderInput(nil), original.Providers...)
	providers[0].UsageBytes, providers[1].UsageBytes = 8, 92
	other, err := payoutartifact.Build(payoutartifact.BuildInput{DeploymentID: original.DeploymentID, GenesisHash: original.GenesisHash, ChainID: original.ChainID, Netuid: original.Netuid, Coordinator: original.Coordinator, SettlementVault: original.SettlementVault, Epoch: original.Epoch, NoID: original.NoID, PolicyHash: original.PolicyHash, Start: original.Start, End: original.End, OperatorSnapshotHash: original.OperatorSnapshotHash, FleetSnapshotHash: original.FleetSnapshotHash, ReliabilityAMin: original.ReliabilityAMin, Providers: providers, CreatedAt: f.source.now})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.Repeat("26", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(other, key); err != nil {
		t.Fatal(err)
	}
	f.raw, err = payoutartifact.Bytes(other)
	if err != nil {
		t.Fatal(err)
	}
	f.artifact = other
	if other.SharesTotalBPS != original.SharesTotalBPS || other.PayoutRoot == original.PayoutRoot || other.Leaves[0].ShareBPS == original.Leaves[0].ShareBPS {
		t.Fatal("negative fixture did not retain aggregate and change original recipient allocation")
	}
	_, code, issue := f.source.run(t, monitorServiceHooks{})
	record := economicEntitlementRecord(t, f.source)
	if code != 3 || record.Census != nil || !record.CensusHeld || record.PayoutRoot != common.Hash(f.committedRoot).Hex() || !strings.Contains(record.CensusIssue, "authorized commitment") {
		t.Fatal("equal-total uncommitted original artifact acquired public economic authority", code, issue, record)
	}
}

func TestEconomicEntitlementPublicLostCheckpointAckKeepsOriginalCensus(t *testing.T) {
	for _, mode := range []string{"ack", "output"} {
		f := newEconomicEntitlementFixture(t)
		economicEntitlementFirstPage(t, f)
		var output io.Writer = &bytes.Buffer{}
		hooks := monitorServiceHooks{}
		if mode == "output" {
			output = economicConservationShortWriter{}
		} else {
			hooks.syncDirectory = func(_, _ string, file *os.File) error { return errors.Join(file.Sync(), syscall.EIO) }
		}
		var diagnostic bytes.Buffer
		code := runMonitorStorageTestWithHooks(t, t.Context(), f.source.args(t), output, &diagnostic, func() time.Time { return f.source.now }, hooks)
		before := economicEntitlementRecord(t, f.source)
		if code != 3 || before.Census == nil || f.artifactReads.Load() != 2 {
			t.Fatal("lost acknowledgment did not reach actual complete census publication", mode, code, diagnostic.String(), before)
		}
		summary, code, issue := f.source.run(t, monitorServiceHooks{})
		after := economicEntitlementRecord(t, f.source)
		if code != 0 || f.artifactReads.Load() != 2 || !reflect.DeepEqual(before, after) || summary.AggregatePayments != 1 {
			t.Fatal("lost acknowledgment repeated original artifact or payment", mode, code, issue)
		}
	}
}

func TestEconomicEntitlementPublicCancellationJoinsActualArtifactRead(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	economicEntitlementFirstPage(t, f)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var entered, exited atomic.Uint64
	joined := make(chan struct{})
	f.artifactHook = func(_ http.ResponseWriter, request *http.Request) bool {
		if request.URL.Path != "/sn/artifact" {
			return false
		}
		entered.Add(1)
		defer close(joined)
		cancel()
		<-request.Context().Done()
		exited.Add(1)
		return true
	}
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	state := f.source.state(t)
	if code != 0 || entered.Load() != 1 || state.Vault.Cursor.Number != 11 || output.Len() != 0 {
		t.Fatal("canceled artifact observation became published amounts or an integrity conflict", code, diagnostic.String(), state)
	}
	select {
	case <-joined:
	case <-time.After(time.Minute):
		t.Fatal("actual artifact request outlived owner cancellation")
	}
	if exited.Load() != 1 {
		t.Fatal("artifact cancellation did not join the actual server read")
	}
}

func TestEconomicEntitlementPublicHeldHttpKeepsNativeVaultAndClaimProgress(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	economicEntitlementFirstPage(t, f)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	completed := make(chan error, 1)
	var once sync.Once
	f.artifactHook = func(_ http.ResponseWriter, request *http.Request) bool {
		if request.URL.Path != "/sn/artifact" {
			return false
		}
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return false
		case <-request.Context().Done():
			return true
		}
	}
	publications := 0
	var originalLot string
	hooks := monitorServiceHooks{
		afterEntitlementRead: func(_ context.Context, _ string, err error) { completed <- err },
		afterEvent: func(_ context.Context, role string) {
			if role != economicConservationRole {
				return
			}
			publications++
			state := f.source.state(t)
			if state.Native.Cursor.Number != 102 || state.Vault.Cursor.Number != min(uint64(11+publications), uint64(13)) || f.source.claimReads.Load() < uint64(publications+1) || state.NativeHeld || state.VaultHeld || len(state.Payments) != 1 || len(state.Lots) != 2 {
				t.Fatal("held artifact HTTP stopped healthy original domain progression", publications, state)
			}
			record := economicEntitlementRecord(t, f.source)
			if publications == 1 {
				originalLot = state.Lots[0].Id
				select {
				case <-entered:
				case err := <-completed:
					t.Fatal("actual original artifact HTTP boundary was not reached", err)
				case <-time.After(time.Minute):
					t.Fatal("artifact liveness guard expired before actual HTTP entry")
				}
			}
			if publications <= 2 && (record.Census != nil || record.CensusHeld) {
				t.Fatal("held artifact published incomplete authority", record)
			}
			if publications == 2 {
				close(release)
			}
			if publications == 3 {
				if record.Census == nil || record.CensusIssue != "" || state.Lots[0].Id != originalLot || f.artifactReads.Load() != 2 {
					t.Fatal("completed artifact restored stale cursors or duplicated original reads", record)
				}
				cancel()
			}
		},
		wait: func(owner context.Context, _ string, _ time.Duration) bool {
			if publications == 2 {
				select {
				case err := <-completed:
					if err != nil {
						t.Fatal("actual released artifact acquisition failed", err)
					}
				case <-time.After(time.Minute):
					t.Fatal("released artifact did not join its read owner")
				}
			}
			return owner.Err() == nil
		},
	}
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	if code != 0 || publications != 3 || bytes.Count(output.Bytes(), []byte{'\n'}) != 3 {
		t.Fatal("continuous public owner did not join original artifact work", code, publications, diagnostic.String())
	}
}

func TestEconomicEntitlementDuplicateFundingCannotEraseOriginalCause(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	economicEntitlementFirstPage(t, f)
	if _, code, issue := f.source.run(t, monitorServiceHooks{}); code != 0 {
		t.Fatal("original funding baseline", code, issue)
	}
	record := economicEntitlementRecord(t, f.source)
	duplicate := record
	duplicate.Sources = append(append([]economicConservationBacking(nil), record.Sources...), record.Sources[2])
	duplicate.Sources[2].Amount, duplicate.Sources[3].Amount = "10", "10"
	if err := validateEconomicEntitlementFunding(duplicate); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Fatal("equal-total duplicate original funding edge acquired authority", err)
	}
	if err := validateEconomicEntitlementFunding(record); err != nil {
		t.Fatal("valid original RootMissed carry was lost", err)
	}
	raw, err := json.Marshal(record.Census)
	if err != nil || !bytes.Contains(raw, []byte("original_commitment")) {
		t.Fatal("original commitment evidence is not retained", err)
	}
}
