// These public consumers require separate actual capture/replay executables
// and Rust-exported original programs. Synthetic HTTP/finality only supplies
// local mechanics; it cannot invent the queried stake or grant live authority.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"golang.org/x/crypto/blake2b"
)

// Only synthetic header digests change here. The independently exported
// original runtime, API, state roots, native trace and proof nodes stay exact.
func newEconomicConservationPrincipalFixture(t *testing.T, name string, parentMapping bool, change func(*historicalReplayJob)) (*economicConservationArchiveFixture, *nativeProducerPublicFixture) {
	t.Helper()
	return newEconomicConservationPrincipalFixtureWithVault(t, name, parentMapping, change)
}

func newEconomicConservationPrincipalFixtureWithVault(t *testing.T, name string, parentMapping bool, change func(*historicalReplayJob), configureVault ...func(*monitorEvmFixture)) (*economicConservationArchiveFixture, *nativeProducerPublicFixture) {
	t.Helper()
	return newEconomicConservationPrincipalFixtureWithSequence(t, name, parentMapping, change, nil, configureVault...)
}

// A continuation is installed before RPC starts and must extend the exact
// mapped first job. All ordinary fixtures retain their single original job.
func newEconomicConservationPrincipalFixtureWithSequence(t *testing.T, name string, parentMapping bool, change func(*historicalReplayJob), next func(historicalReplayJob, *monitorEvmFixture) []historicalReplayJob, configureVault ...func(*monitorEvmFixture)) (*economicConservationArchiveFixture, *nativeProducerPublicFixture) {
	t.Helper()
	return economicConservationPrincipalFixtureWithSource(t, nil, name, parentMapping, change, next, configureVault...)
}

// Receipt/artifact fixtures may select their complete original public input
// before constructing the real native job. No admitted header is remapped.
func economicConservationPrincipalFixtureWithSource(t *testing.T, f *economicConservationFixture, name string, parentMapping bool, change func(*historicalReplayJob), next func(historicalReplayJob, *monitorEvmFixture) []historicalReplayJob, configureVault ...func(*monitorEvmFixture)) (*economicConservationArchiveFixture, *nativeProducerPublicFixture) {
	t.Helper()
	directory := os.Getenv("URNETWORK_NATIVE_PRINCIPAL_FIXTURE_DIR")
	if strings.HasPrefix(name, "effects-") {
		directory = os.Getenv("URNETWORK_NATIVE_PRINCIPAL_EFFECTS_FIXTURE_DIR")
	}
	if strings.HasPrefix(name, "capture") {
		directory = os.Getenv("URNETWORK_NATIVE_VAULT_CAPTURE_FIXTURE_DIR")
		if name == "capture-same-block" || name == "capture-next-block" {
			directory = os.Getenv("URNETWORK_NATIVE_VAULT_CAPTURE_SEQUENCE_FIXTURE_DIR")
		}
	}
	filename := "principal-" + name + ".json"
	runtimeRenewal := name == "runtime-renewal"
	if runtimeRenewal {
		directory = os.Getenv("URNETWORK_NATIVE_RUNTIME_RENEWAL_FIXTURE")
		filename = "native-job-101.json"
		t.Setenv("URNETWORK_NATIVE_PRODUCER_FIXTURE", directory)
	}
	wholeFee := strings.HasPrefix(name, "whole-fee-")
	if wholeFee {
		directory = os.Getenv("URNETWORK_NATIVE_WHOLE_FEE_FIXTURE_DIR")
		filename = name + ".json"
	}
	yuma := strings.HasPrefix(name, "yuma-")
	if yuma {
		directory = os.Getenv("URNETWORK_NATIVE_YUMA_FIXTURE_DIR")
		filename = name + ".json"
		if strings.HasPrefix(name, "yuma-populated-") {
			directory = os.Getenv("URNETWORK_NATIVE_YUMA_POPULATED_FIXTURE_DIR")
			t.Setenv("URNETWORK_NATIVE_YUMA_POPULATED_EXPECTATION", filepath.Join(directory, name+"-original-events.json"))
		}
		if strings.HasPrefix(name, "yuma-capacity-") {
			directory = os.Getenv("URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_DIR")
		}
	}
	if directory == "" || os.Getenv("URNETWORK_NATIVE_CAPTURE_ENGINE") == "" || os.Getenv("URNETWORK_NATIVE_EXECUTION_ENGINE") == "" {
		t.Fatal("principal scope requires explicit real Rust exports and two distinct owned engines")
	}
	if f == nil {
		f = newEconomicConservationFixture(t, false, configureVault...)
	} else if len(configureVault) != 0 {
		t.Fatal("preselected public fixture cannot also replace original vault inputs")
	}
	raw, _, err := readPlanFile(t.Context(), filepath.Join(directory, filename), historicalNativeJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := decodePlanJson(raw, &job); err != nil || !yuma && len(job.PrincipalQueries) != 1 {
		t.Fatal("principal original job is absent", err)
	}
	if change != nil {
		change(&job)
	}
	for index, encoded := range []string{job.ParentHeaderHex, job.ChildHeaderHex} {
		headerRaw, err := historicalReplayHex(encoded, 64*1024)
		if err != nil {
			t.Fatal(err)
		}
		var header types.Header
		if err := codec.Decode(headerRaw, &header); err != nil {
			t.Fatal(err)
		}
		if index == 1 {
			header.ParentHash = types.Hash(job.ParentHash)
			if strings.HasPrefix(name, "capture") {
				body := [][]byte{}
				for _, encoded := range job.ExtrinsicsHex {
					raw, err := historicalReplayHex(encoded, 8*1024*1024)
					if err != nil {
						t.Fatal(err)
					}
					body = append(body, raw)
				}
				// Admitted system versions zero and one use extrinsics layout zero.
				if job.ExecutionStateVersion > 1 {
					t.Fatal("fixture system version is unsupported")
				}
				root, err := rootExtrinsicsRoot(body, 0)
				if err != nil {
					t.Fatal(err)
				}
				hash, err := types.NewHashFromHexString(root)
				if err != nil {
					t.Fatal(err)
				}
				header.ExtrinsicsRoot = hash
			}
		}
		if index == 1 || parentMapping {
			mapping, err := hex.DecodeString(strings.TrimPrefix(mappingTestDigest(t, 3, f.vault.blocks[uint64(index+10)].header.Hash().Hex(), nil), "0x"))
			if err != nil {
				t.Fatal(err)
			}
			var item types.DigestItem
			if err := codec.Decode(mapping, &item); err != nil {
				t.Fatal(err)
			}
			header.Digest = append(header.Digest, item)
		}
		headerRaw, err = codec.Encode(header)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			job.ParentHeaderHex, job.ParentHash = nativeExecutionTestHex(headerRaw), historicalReplayDigest(blake2b.Sum256(headerRaw))
		} else {
			job.ChildHeaderHex, job.ChildHash = nativeExecutionTestHex(headerRaw), historicalReplayDigest(blake2b.Sum256(headerRaw))
		}
	}
	raw, err = json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "original-principal-job.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("URNETWORK_NATIVE_EXECUTION_FIXTURE", path)
	var additionalJobs []historicalReplayJob
	if next != nil {
		additionalJobs = next(job, f.vault)
	}
	var feePolicy *nativeFeeCensusPolicy
	if wholeFee {
		feePolicy = nativeWholeFeeTestAuthority(f.policy)
	}
	producer := nativeProducerPublicFixtureWithRuntime(t, runtimeRenewal, feePolicy, runtimeRenewal, additionalJobs...)
	f.native, f.policy.Native.Observation, f.policy.Native.BatchBlocks = producer.source, producer.source.policy, 1
	// Matching original proof parents are drained before per-block accrual.
	f.native.set(t, 100, "PendingServerEmission", make([]byte, 8))
	if yuma {
		f.policy.MaximumFacts = 4096
		if strings.HasPrefix(name, "yuma-capacity-") || strings.HasPrefix(name, "yuma-populated-") {
			f.policy.StorageProfile = economicConservationTestStorageProfile()
			f.policy.MaximumFacts = economicConservationMaximumFacts
			f.policy.ReadBudgetSeconds, f.policy.Native.ReadBudgetSeconds = 900, 900
		}
	}
	f.writePolicy(t)
	// A synthetic volume declaration does not provision a snapshot owner. The
	// combined checkpoint explicitly starts with an owned absent-head marker.
	if f.policy.StorageProfile == nil {
		provisionEconomicConservationPrincipalFixture(t, f.checkpoint)
	} else {
		provisionMonitorTestCustodyProfile(t, f.checkpoint, f.policy.storageKind(), int(f.policy.storageMaximum()))
	}
	storage := durablefixture.New(t, t.Context(), producer.source.policy.Execution.Directory, filepath.Dir(f.checkpoint))
	storage.Host.SetReserve(4*1024*1024*1024, 1024*1024)
	producer.ctx = storage.Context
	metadata := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, metadata)
	ctx := economicConservationTestArchiveReserves(t, storage.Context, metadata, f.policy)
	producer.ctx = ctx
	return &economicConservationArchiveFixture{source: f, ctx: ctx, metadata: metadata, storage: storage}, producer
}

// Provision only the fresh test fixture. Public observers never recreate a lost
// lock, reinterpret an empty committed file as absence, or repair its owner.
func provisionEconomicConservationPrincipalFixture(t *testing.T, checkpoint string) {
	t.Helper()
	provisionMonitorTestCustody(t, checkpoint)
}

// The first positive reaches actual replay and public conservation; the
// subsequent archive/public reopen must retain stock without earning it again.
func TestEconomicConservationPublicOriginalParentStockNeverBecomesIncome(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if summary.OpeningPrincipalAlpha == nil || *summary.OpeningPrincipalAlpha != "14" || summary.OpeningPrincipals == nil || summary.OpeningPrincipals.Parent != producer.authority.From || summary.OpeningPrincipals.EvmHash != f.source.policy.Vault.From.Hash || summary.OpeningPrincipals.Status != "original-parent-stock-observed-effects-unproved" || len(summary.OpeningPrincipals.Pools) != 1 {
		t.Fatal("actual original parent principal was not joined to its exact vault pool", summary)
	}
	if len(state.Captures) != 1 || state.Captures[0].OpeningPrincipalAlpha != nil || state.Captures[0].AmountDifferenceAlpha == nil || *state.Captures[0].AmountDifferenceAlpha != "14" || summary.TailGrossAlpha == nil || *summary.TailGrossAlpha != "9" || summary.TargetMet != nil || summary.ActualNativeOutcomeVerified || summary.FullQuantizationToleranceAlpha != nil {
		t.Fatal("opening stock was credited as income or erased unexplained execution effects", summary, state.Captures)
	}
	original := rootObjectHash(state.OpeningPrincipals)
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("principal archive admission", code, issue)
	}
	summary = f.sample(t, monitorServiceHooks{})
	state = f.source.state(t)
	if rootObjectHash(state.OpeningPrincipals) != original || summary.OpeningPrincipalAlpha == nil || *summary.OpeningPrincipalAlpha != "14" || summary.AggregatePayments != 1 || summary.TargetMet != nil || len(state.Captures) != 1 || *state.Captures[0].AmountDifferenceAlpha != "14" {
		t.Fatal("cold principal reopen recounted stock or changed original capture lineage", summary, state)
	}
	if err := state.index(); err != nil {
		t.Fatal(err)
	}
	missed, later := state.entitlement("2", "1"), state.entitlement("3", "1")
	if missed.Funded != "20" || later.Funded != "0" || later.Total == nil || *later.Total != "50" || len(later.Sources) != 3 || later.Sources[2].Id != missed.Id {
		t.Fatal("opening stock rewrote original root-missed funding or entitlement", missed, later)
	}
}

func TestEconomicConservationPublicPrincipalAbsentAndZeroSurviveArchive(t *testing.T) {
	for _, name := range []string{"absent", "zero"} {
		f, _ := newEconomicConservationPrincipalFixture(t, name, true, nil)
		first := f.sample(t, monitorServiceHooks{})
		if first.OpeningPrincipals == nil || len(first.OpeningPrincipals.Pools) != 1 || (first.OpeningPrincipalAlpha == nil) != (name == "absent") || name == "zero" && *first.OpeningPrincipalAlpha != "0" || first.TargetMet != nil {
			t.Fatal("original absent principal was relabelled zero or authority", name, first)
		}
		original := rootObjectHash(f.source.state(t).OpeningPrincipals)
		f.reset(t)
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("absent/zero principal archive", code, issue)
		}
		second := f.sample(t, monitorServiceHooks{})
		if !reflect.DeepEqual(first.OpeningPrincipals, second.OpeningPrincipals) || rootObjectHash(f.source.state(t).OpeningPrincipals) != original || second.TargetMet != nil {
			t.Fatal("archive restart changed principal knowledge", name, first, second)
		}
	}
}

func TestEconomicConservationPublicPrincipalNeedsExactParentFrontierMapping(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", false, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.OpeningPrincipals == nil || summary.OpeningPrincipals.Pools[0].Observation.OpeningStakeAlpha == nil || *summary.OpeningPrincipals.Pools[0].Observation.OpeningStakeAlpha != "14" || summary.OpeningPrincipalAlpha != nil || summary.OpeningPrincipals.Status != "original-parent-stock-vault-boundary-unjoined" || !summary.NativeCurrent || !summary.VaultCurrent || summary.MatchedReceipts != 1 {
		t.Fatal("principal borrowed a nearby native/EVM height or blocked healthy observations", summary)
	}
}

func TestEconomicConservationPublicPrincipalIncompletePoolCensusStaysUnknown(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	f.source.policy.Vault.PoolIds = append(f.source.policy.Vault.PoolIds, "2")
	f.source.vault.policy.PoolIds = append(f.source.vault.policy.PoolIds, "2")
	f.source.policy.Routes = append(f.source.policy.Routes, economicConservationRoute{Hotkey: "0x" + strings.Repeat("55", 32), Coldkey: "0x" + strings.Repeat("66", 32), Kind: "tail-pool", PoolId: "2"})
	for _, block := range f.source.vault.blocks {
		block.snapshot.Pools["2"] = "0"
	}
	f.source.writePolicy(t)
	summary := f.sample(t, monitorServiceHooks{})
	if summary.OpeningPrincipals == nil || summary.OpeningPrincipals.Status != "original-parent-stock-pool-census-incomplete" || summary.OpeningPrincipalAlpha != nil || !summary.NativeCurrent || !summary.VaultCurrent || summary.TargetMet != nil || summary.MatchedReceipts != 1 {
		t.Fatal("selected principal became complete pool authority or stopped siblings", summary)
	}
}

func TestEconomicConservationPublicPrincipalReviewCannotReplaceOriginalSignature(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	// The helper already admits the valid signed baseline through the actual
	// production decoder. Change a valid review digest without the private key.
	producer.authority.Principal.ReviewSha256 = monitorReadDigest([]byte("synthetic unapproved replacement API review"))
	raw, err := json.Marshal(producer.authority)
	if err != nil {
		t.Fatal(err)
	}
	path := f.source.policy.Native.Observation.Execution.Producer.Authority.Path
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.source.policy.Native.Observation.Execution.Producer.Authority.Sha256 = monitorReadDigest(raw)
	f.source.writePolicy(t)
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err, diagnostic.String())
	}
	if code != 3 || summary.NativeCurrent || summary.OpeningPrincipals != nil || !strings.Contains(summary.NativeIssue, "independent authority signature is invalid") || !summary.VaultCurrent || summary.VaultCursor.Number != 11 || f.source.claimReads.Load() == 0 {
		t.Fatal("principal API review enrolled itself without original signature or stopped siblings", code, diagnostic.String(), summary)
	}
}

func TestEconomicConservationPublicPrincipalForeignPoolRouteHoldsOnlyNative(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	f.source.policy.Routes[0].Coldkey = "0x" + strings.Repeat("44", 32)
	f.source.writePolicy(t)
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err, diagnostic.String())
	}
	if code != 3 || !summary.NativeHeld || !strings.Contains(summary.NativeIssue, "principal substituted original pool/hotkey/coldkey census") || summary.OpeningPrincipals != nil || summary.OpeningPrincipalAlpha != nil || summary.NativeCursor.Number != 100 || summary.VaultCursor.Number != 11 || !summary.VaultCurrent || f.source.claimReads.Load() == 0 {
		t.Fatal("foreign principal route was admitted or stopped healthy siblings", code, diagnostic.String(), summary)
	}
}

func TestEconomicConservationPublicPrincipalForeignApiIdentityHoldsOnlyNative(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", true, func(job *historicalReplayJob) {
		job.PrincipalQueries[0].Hotkey[0] = 0x44
	})
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err, diagnostic.String())
	}
	if code != 3 || summary.NativeCurrent || summary.OpeningPrincipalAlpha != nil || summary.NativeCursor.Number != 100 || summary.VaultCursor.Number != 11 || !summary.VaultCurrent || !strings.Contains(summary.NativeIssue, "principal") || f.source.claimReads.Load() == 0 {
		t.Fatal("original API identity substitution became principal or stopped siblings", code, diagnostic.String(), summary)
	}
}

// Missing optional runtime API is a native-source failure, not a reason to
// invent zero principal or stop the healthy vault and Claim observations.
func TestEconomicConservationPublicMissingPrincipalApiKeepsSiblings(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "missing-api", true, nil)
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err, diagnostic.String())
	}
	if code != 3 || summary.NativeCurrent || summary.OpeningPrincipals != nil || summary.OpeningPrincipalAlpha != nil || summary.NativeCursor.Number != 100 || summary.VaultCursor.Number != 11 || !summary.VaultCurrent || !strings.Contains(summary.NativeIssue, "principal") || f.source.claimReads.Load() == 0 {
		t.Fatal("missing original principal API became zero or stopped healthy siblings", code, diagnostic.String(), summary)
	}
}

func TestEconomicConservationPublicMissingPrincipalProofKeepsSiblings(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", true, func(job *historicalReplayJob) {
		job.ProofNodesHex = []string{}
	})
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err, diagnostic.String())
	}
	if code != 3 || summary.NativeCurrent || summary.OpeningPrincipalAlpha != nil || summary.NativeCursor.Number != 100 || summary.VaultCursor.Number != 11 || !summary.VaultCurrent || summary.NativeIssue == "" || f.source.claimReads.Load() == 0 {
		t.Fatal("missing original parent proof became zero or stopped sibling observation", code, diagnostic.String(), summary)
	}
}

func TestEconomicConservationPublicArchivedPrincipalCannotBeResealed(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("original principal archive", code, issue)
	}
	state := f.source.state(t)
	value := &state.OpeningPrincipals.Projection.Observations[0]
	changed := "13"
	value.OpeningStakeAlpha = &changed
	rawResult, err := historicalReplayHex(value.ResultHex, 256)
	if err != nil {
		t.Fatal(err)
	}
	rawResult[66] = 13 * 4
	value.ResultHex = nativeExecutionTestHex(rawResult)
	state.OpeningPrincipals.Projection.ContentHash = state.OpeningPrincipals.Projection.hash()
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(writer.publish(append(raw, '\n'), nil), writer.close()); err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	reads, nativeReads := f.source.claimReads.Load(), producer.requests.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "principal contradicts its first archived replay") || reads != f.source.claimReads.Load() || nativeReads != producer.requests.Load() || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("self-sealed principal replaced original archive or reached source reads", code, diagnostic.String())
	}
}
