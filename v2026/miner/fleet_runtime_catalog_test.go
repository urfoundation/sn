package miner

// Reviewed catalogs select only artifacts consumed by an operation. The local
// endpoint supplies observations, never approval; no test broadcasts a write.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Raw JSON keeps the old-source causal test compile-valid before v2 exists.
func writeFleetRuntimeCatalogFixture(t *testing.T, fixture *fleetMainnetTestFixture) {
	t.Helper()
	old, current := fixture.version, fixture.version
	current.SpecVersion++
	entries := []any{}
	for _, version := range []crv4.RuntimeVersionIdentity{old, current} {
		entries = append(entries, map[string]any{
			"runtime_source_commit": fixture.authority.RuntimeSourceCommit, "runtime_review_sha256": fixture.authority.RuntimeReviewSha256,
			"runtime_version": version, "runtime_code_hash": fixture.authority.RuntimeCodeHash, "runtime_metadata_hash": fixture.authority.RuntimeMetadataHash,
			"purposes": []string{"fleet-commitment-read-v1", "fleet-frontier-read-v1"},
		})
	}
	raw, err := json.Marshal(map[string]any{"schema": "urnetwork-mainnet-fleet-runtime-authority-v2", "native_chain": fixture.authority.NativeChain, "genesis_hash": fixture.authority.GenesisHash, "evm_chain_id": fixture.authority.EvmChainId, "netuid": fixture.authority.Netuid, "coordinator": fixture.authority.Coordinator, "runtime_catalog": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(fleetOpt(fixture.opts, "--mainnet-runtime-authority"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	fixture.opts["--mainnet-runtime-authority-sha256"] = hex.EncodeToString(digest[:])
	fixture.stateLock.Lock()
	fixture.historicalVersions[fixture.head.Hex()] = old
	fixture.version = current
	fixture.finalizedNumber = 103
	fixture.stateLock.Unlock()
}

func TestFleetRuntimeCatalogCurrentAndHistoricalReads(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal("reviewed multi-artifact read authority was refused", err)
	}
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	previousMeta, previousRuntime := chain.Meta, chain.Runtime
	current, block, err := authority.finalizedView(t.Context(), chain)
	if err != nil || block != fixture.nativeBlocks[103] || current.Runtime.SpecVersion != 8002 {
		t.Fatal("reviewed current selection failed", block, err)
	}
	old, err := authority.viewAt(t.Context(), chain, fixture.head)
	if err != nil || old.Runtime.SpecVersion != 8001 {
		t.Fatal("reviewed historical selection failed", err)
	}
	if chain.Meta != previousMeta || chain.Runtime != previousRuntime || current.Runtime.SpecVersion != 8002 {
		t.Fatal("historical selection mutated a concurrent current view")
	}
	if _, err = authority.commitmentFinalized(t.Context(), chain, fixture.manifest.Netuid, fixture.manifest.Hotkey); err != nil {
		t.Fatal("current readable capability did not continue", err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 0 || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("read-only catalog test sent a transaction")
	}
}

func TestFleetRuntimeCatalogPublicStatusHonorsCancellation(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	ctx, cancel := context.WithCancel(fixture.durable.Context)
	cancel()
	err := fleetStatus(ctx, fixture.opts, fixture.manifest)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("public fleet status discarded caller cancellation", err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if len(fixture.calls) != 0 {
		t.Fatal("canceled public status reached the endpoint", fixture.calls)
	}
}

// Mutations are explicit synthetic review inputs, then content-pinned again.
func reviseFleetRuntimeCatalogFixture(t *testing.T, fixture *fleetMainnetTestFixture, change func(*fleetMainnetRuntimeAuthority)) *fleetMainnetRuntimeAuthority {
	t.Helper()
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	change(authority)
	raw, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(fleetOpt(fixture.opts, "--mainnet-runtime-authority"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	fixture.opts["--mainnet-runtime-authority-sha256"] = hex.EncodeToString(digest[:])
	authority, err = loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func TestFleetRuntimeCatalogReadOnlyStatusSurvivesWriteRefusal(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if _, _, err = authority.finalizedFor(t.Context(), chain, crv4.FleetCommitmentWrite); err == nil {
		t.Fatal("read review permitted a write capability")
	}
	if err = fleetStatus(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal("write refusal stopped independently readable public status", err)
	}
	if fixture.count("payment_queryInfo") != 0 || fixture.count("author_submitAndWatchExtrinsic") != 0 || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("readable status reached transaction preparation")
	}
}

func TestFleetRuntimeCatalogCurrentReadDoesNotRequireHistoricalArtifact(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority := reviseFleetRuntimeCatalogFixture(t, fixture, func(value *fleetMainnetRuntimeAuthority) { value.RuntimeCatalog = value.RuntimeCatalog[1:] })
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if _, err = authority.viewFor(t.Context(), chain, fixture.head, crv4.FleetCommitmentRead); err == nil {
		t.Fatal("absent historical review was learned from the endpoint")
	}
	if err = fleetStatus(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal("unconsumed historical artifact stopped current status", err)
	}
}

func TestFleetRuntimeCatalogUnknownPurposeRefusesBeforeRpc(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = authority.authenticateFor(t.Context(), nil, fixture.head, "future-unknown-purpose"); err == nil {
		t.Fatal("unknown purpose inherited authority")
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if len(fixture.calls) != 0 {
		t.Fatal("unknown purpose performed network work")
	}
}

func TestFleetRuntimeCatalogApprovalRefusesAmbiguousAndUnboundedEntries(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*fleetMainnetRuntimeAuthority){
		func(v *fleetMainnetRuntimeAuthority) { v.RuntimeCodeHash = fixture.code },
		func(v *fleetMainnetRuntimeAuthority) { v.RuntimeCatalog = nil },
		func(v *fleetMainnetRuntimeAuthority) {
			v.RuntimeCatalog = append(v.RuntimeCatalog, v.RuntimeCatalog[0])
		},
		func(v *fleetMainnetRuntimeAuthority) {
			v.RuntimeCatalog[0].Purposes = []crv4.FleetRuntimePurpose{"unknown"}
		},
		func(v *fleetMainnetRuntimeAuthority) {
			v.RuntimeCatalog[0].Purposes = []crv4.FleetRuntimePurpose{crv4.FleetCommitmentRead, crv4.FleetCommitmentRead}
		},
		func(v *fleetMainnetRuntimeAuthority) { v.RuntimeCatalog[0].RuntimeReviewSha256 = "" },
		func(v *fleetMainnetRuntimeAuthority) {
			v.RuntimeCatalog = make([]fleetRuntimeCatalogEntry, fleetRuntimeCatalogLimit+1)
		},
	} {
		copy := *authority
		copy.RuntimeCatalog = append([]fleetRuntimeCatalogEntry(nil), authority.RuntimeCatalog...)
		mutate(&copy)
		if err := copy.validate(fixture.manifest); err == nil {
			t.Fatal("malformed catalog inherited reviewed authority")
		}
	}
}

func TestFleetRuntimeCatalogRetainedAuthorityNeverReloadsNewApproval(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := fleetRecoveryNewIntent("publish", authority, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	record := fleetRecoveryPrepared(intent, authority, fixture.head, 100)
	original := append([]byte(nil), record.Authority...)
	digest := record.AuthoritySha256
	reviseFleetRuntimeCatalogFixture(t, fixture, func(value *fleetMainnetRuntimeAuthority) { value.RuntimeCatalog = value.RuntimeCatalog[1:] })
	retained, _, err := record.authority()
	if err != nil || len(retained.RuntimeCatalog) != 2 || !bytes.Equal(retained.document, original) || !bytes.Equal(record.Authority, original) || record.AuthoritySha256 != digest {
		t.Fatal("new approval rewrote original custody", err)
	}
	if err := fixture.authority.validate(fixture.manifest); err != nil {
		t.Fatal("retained v1 review stopped parsing", err)
	}
}

func TestFleetRuntimeCatalogApprovedUpgradeStillRefusesOldSignatureDomain(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority := reviseFleetRuntimeCatalogFixture(t, fixture, func(value *fleetMainnetRuntimeAuthority) {
		for i := range value.RuntimeCatalog {
			value.RuntimeCatalog[i].Purposes = append(value.RuntimeCatalog[i].Purposes, crv4.FleetCommitmentWrite, crv4.FleetDispatchRead)
		}
	})
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if err = authority.signingAdmission(t.Context(), chain, fixture.head, crv4.FleetCommitmentWrite); err == nil || !strings.Contains(err.Error(), "signing artifact changed") {
		t.Fatal("two approved artifacts relabeled one signature domain", err)
	}
	if err = authority.signingAdmission(t.Context(), chain, fixture.nativeBlocks[103], crv4.FleetCommitmentWrite); err != nil {
		t.Fatal("same current artifact did not remain eligible", err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("admission-only control sent a transaction")
	}
}

func TestFleetRuntimeCatalogExecutionAndPostStateStaySeparate(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority := reviseFleetRuntimeCatalogFixture(t, fixture, func(value *fleetMainnetRuntimeAuthority) {
		value.RuntimeCatalog[0].Purposes = append(value.RuntimeCatalog[0].Purposes, crv4.FleetCommitmentWrite, crv4.FleetDispatchRead)
	})
	fixture.stateLock.Lock()
	fixture.historicalVersions[fixture.nativeBlocks[101].Hex()] = fixture.authority.RuntimeVersion
	fixture.stateLock.Unlock()
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	execution, err := authority.executionAdmission(chain, fixture.head, crv4.FleetCommitmentWrite)(t.Context(), fixture.nativeBlocks[101])
	if err != nil || execution.Version.SpecVersion != 8001 {
		t.Fatal("historical execution did not retain original artifact", err)
	}
	post, err := authority.viewFor(t.Context(), chain, fixture.nativeBlocks[102], crv4.FleetCommitmentRead)
	if err != nil || post.Runtime.SpecVersion != 8002 {
		t.Fatal("post-state did not select its own artifact", err)
	}
	before := fixture.count("state_getStorage")
	if _, err = authority.executionAdmission(chain, fixture.head, crv4.FleetCommitmentWrite)(t.Context(), fixture.nativeBlocks[102]); err == nil {
		t.Fatal("post-state impersonated approved execution")
	}
	if fixture.count("state_getStorage") != before {
		t.Fatal("failed execution admission reached storage decoding")
	}
}

func TestFleetRuntimeCatalogConsumedReadIgnoresUnrelatedWriteShape(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if string(pallet.Name) == "Commitments" && pallet.HasCalls {
			item := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
			for i := range item.Def.Variant.Variants {
				if string(item.Def.Variant.Variants[i].Name) == "set_commitment" {
					item.Def.Variant.Variants[i].Index++
					changed = true
				}
			}
		}
	}
	if !changed {
		t.Fatal("fixture did not alter consumed write shape")
	}
	if err = crv4.ValidateFleetRuntimeMetadata(metadata, crv4.FleetCommitmentRead); err != nil {
		t.Fatal("unconsumed call changed read capability", err)
	}
	if err = crv4.ValidateFleetRuntimeMetadata(metadata, crv4.FleetCommitmentWrite); err == nil {
		t.Fatal("changed consumed call inherited write capability")
	}
}

func TestFleetRuntimeCatalogForgedBlockViewIsNotAuthentication(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	artifact, err := fixture.authority.authenticateAt(t.Context(), chain, fixture.head)
	if err != nil {
		t.Fatal(err)
	}
	if err = crv4.ValidateRuntimeArtifactOwnerContext(t.Context(), chain, artifact); err != nil {
		t.Fatal(err)
	}
	artifact.BlockHash = types.Hash{99}
	if err = crv4.ValidateRuntimeArtifactOwnerContext(t.Context(), chain, artifact); err == nil {
		t.Fatal("exported block field transferred authentication")
	}
}

// The synthetic websocket finalizes an unsigned fixture extrinsic. It uses
// the real watch/header/body/event decoder without loading or using a key.
func testFleetCatalogReceiptWatch(t *testing.T, refusal string) {
	t.Helper()
	fixture := newFleetMainnetTestFixture(t)
	writeFleetRuntimeCatalogFixture(t, fixture)
	authority := reviseFleetRuntimeCatalogFixture(t, fixture, func(value *fleetMainnetRuntimeAuthority) {
		value.RuntimeCatalog[0].Purposes = append(value.RuntimeCatalog[0].Purposes, crv4.FleetCommitmentWrite, crv4.FleetDispatchRead)
	})
	fixture.stateLock.Lock()
	fixture.finalizedNumber = 100
	fixture.historicalVersions[fixture.nativeBlocks[101].Hex()] = fixture.authority.RuntimeVersion
	prepared := fixture.head
	fixture.stateLock.Unlock()
	endpoint := fixture.nativeWebsocket(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	chain, err := crv4.DialChainContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	view, err := authority.viewFor(ctx, chain, prepared, crv4.FleetCommitmentWrite)
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.count("state_getStorage")
	called := false
	admit := authority.executionAdmission(chain, prepared, crv4.FleetCommitmentWrite)
	selector := func(readCtx context.Context, parent types.Hash) (crv4.AuthenticatedRuntimeArtifact, error) {
		called = true
		fixture.stateLock.Lock()
		wantParent, finalizedBlock := fixture.nativeBlocks[101], fixture.receiptBlock
		fixture.stateLock.Unlock()
		if parent != wantParent || fixture.count("state_getStorage") != before {
			t.Error("receipt decoding preceded its exact execution parent admission")
		}
		artifact, err := admit(readCtx, parent)
		if refusal == "wrong-block" {
			artifact.BlockHash = finalizedBlock
		}
		if refusal == "cancel" {
			cancel()
		}
		return artifact, err
	}
	receipt, err := view.SubmitRawAndWatchFinalizedRuntime(ctx, "0x1004010203", selector)
	if !called {
		t.Fatal("watch skipped the execution admission callback", err)
	}
	if refusal != "" {
		if err == nil || receipt != nil || fixture.count("state_getStorage") != before {
			t.Fatal("refused execution view decoded or acknowledged a receipt", receipt, err)
		}
		if refusal == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatal("execution callback cancellation was lost", err)
		}
		return
	}
	fixture.stateLock.Lock()
	finalizedBlock := fixture.receiptBlock
	fixture.stateLock.Unlock()
	if err != nil || receipt == nil || receipt.BlockHash != finalizedBlock || receipt.BlockNumber != 102 {
		t.Fatal("reviewed execution-parent receipt failed", receipt, err)
	}
	hash, _ := fixture.manifest.CommitmentHash()
	observed, err := authority.commitmentWrite(ctx, chain, fixture.manifest.Netuid, fixture.manifest.Hotkey, hash, receipt)
	if err != nil || observed.CommitmentBlock != 102 {
		t.Fatal("receipt post-state did not select the reviewed successor", observed, err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 1 || fixture.count("payment_queryInfo") != 0 {
		t.Fatal("watch fixture signed, retried or relabeled an extrinsic")
	}
}

func TestFleetRuntimeCatalogWatchBindsExecutionBeforePostState(t *testing.T) {
	testFleetCatalogReceiptWatch(t, "")
}

func TestFleetRuntimeCatalogWatchRejectsWrongExecutionBlock(t *testing.T) {
	testFleetCatalogReceiptWatch(t, "wrong-block")
}

func TestFleetRuntimeCatalogWatchHonorsCallbackCancellation(t *testing.T) {
	testFleetCatalogReceiptWatch(t, "cancel")
}
