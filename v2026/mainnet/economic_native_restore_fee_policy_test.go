//go:build linux

// Full provider and fee authority documents share a real prepared root. These
// tests restore original custody with no completed VM job; the separate native
// continuation controls execute both engines and resume original completions.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The real original-key signature covers all 4096 pairs and 8192 accounts.
// Synthetic callsite and checkpoint inputs grant no execution or fee evidence.
func nativeRestoreFullFeeTestScope(t *testing.T, root string) storageNativeProducerScope {
	t.Helper()
	_, policy, original, _, files := nativeRenewalTestInputs(t)
	defer files.close()
	policy.MaximumUids = rootCensusLimit
	execution := policy.Execution
	execution.Directory = filepath.Join(root, "runtime", "native")
	execution.Producer.Nodes = filepath.Join(execution.Directory, "nodes")
	execution.Producer.Renewals = nil
	fees := &nativeFeeCensusPolicy{Schema: nativeFeeCensusSchema, ReviewSha256: monitorReadDigest([]byte("synthetic complete original fee roster"))}
	authority := original[0].value
	authority.Directory, authority.Nodes, authority.Providers = execution.Directory, execution.Producer.Nodes, nil
	for index := range rootCensusLimit {
		hotkey, coldkey := fmt.Sprintf("0x%064x", 2*index+1), fmt.Sprintf("0x%064x", 2*index+2)
		authority.Providers = append(authority.Providers, nativeProducerProvider{Hotkey: hotkey, Coldkey: coldkey})
		fees.Participants = append(fees.Participants, hotkey, coldkey)
	}
	metadata := historicalReplayDigest{17}
	authority.Profile.MetadataSha256 = &metadata
	for index, purpose := range []string{"fee-withdraw", "fee-refund", "ethereum-executed"} {
		authority.Profile.Rules = append(authority.Profile.Rules, historicalReplayHookRule{Purpose: purpose, FunctionIndex: uint32(100 + index), FunctionBodySha256: historicalReplayDigest{byte(index + 1)}, OffsetStart: 0, OffsetEnd: 4})
	}
	profile, err := json.Marshal(authority.Profile)
	if err != nil {
		t.Fatal(err)
	}
	execution.ProfileSha256, execution.FeeCensus, authority.FeeCensus = monitorReadDigest(profile), fees, fees
	message, err := authority.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	authority.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	execution.Producer.Authority = nativeRenewalTestWrite(t, filepath.Join(root, "original-approval.json"), authority)
	raw, err := nativeProducerReadApprovalFor(t.Context(), execution.Producer.Authority, fees)
	if err != nil || len(raw) <= nativeProducerAuthorityLimit {
		t.Fatal("populated authority did not require its original explicit fee frame", len(raw), err)
	}
	if _, err := loadNativeProducerAuthorities(t.Context(), policy); err != nil {
		t.Fatal("full original signed provider and fee roster failed baseline admission", err)
	}
	checkpoint := []byte("synthetic original checkpoint")
	return storageNativeProducerScope{Schema: storageNativeProducerSchema, Policy: policy, Cursor: policy.From, Checkpoint: monitorHistoryReference{Path: filepath.Join(root, "monitor.json"), Sha256: monitorReadDigest(checkpoint), Bytes: uint64(len(checkpoint))}, ApprovalSources: []storageNativeApprovalSource{{Reference: execution.Producer.Authority, Bytes: uint64(len(raw))}}}
}

// Both fixed native owners coexist with an independent snapshot on the same
// exported volume. The public preparation reader retains its one-MiB limit.
func TestNativeProducerRestoreFullFeeRosterSameRootPublicPreparation(t *testing.T) {
	source := newStoragePreparationCommandFixture(t)
	peer := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "monitor.json", maxRpcReplyBytes)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{peer})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	scope := nativeRestoreFullFeeTestScope(t, source.root)
	if err := os.MkdirAll(scope.Policy.Execution.Producer.Nodes, 0700); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(scope.Policy.Execution.Producer.Nodes, strings.Repeat("e", 64)+".pending")
	if err := os.WriteFile(pending, []byte{1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(scope.Policy.Execution.Producer.Authority.Path)
	if err != nil {
		t.Fatal(err)
	}
	target := storageSnapshotRestoreTarget(t, source, ctx, peer, false)
	raw, err := os.ReadFile(target.target.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := decodePlanJson(raw, &request); err != nil {
		t.Fatal(err)
	}
	request.Owners[0].RestoreCoverage = durablevolume.PreparationCompleteUnion
	inventory, err := durablevolume.LoadPhysicalInventory(target.target.ctx, request.RestoreSource.Inventory)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]durablevolume.StateRootSpec{request.RootPath: inventory.StateRoot}
	review, err := newMonitorHistoryRestoreRootReview(target.target.ctx, request, declared)
	if err != nil {
		t.Fatal(err)
	}
	if err := reviewEconomicNativeProducerRestore(target.target.ctx, scope.Policy, scope.Checkpoint, nil, monitorEconomicNativeState{Cursor: scope.Cursor}, []*monitorHistoryRestoreRootReview{review}, declared); err != nil {
		t.Fatal("full fee roster did not derive complete original same-root ownership", err)
	}
	request = review.request
	raw, err = json.Marshal(request)
	if err != nil || len(raw) > maxRpcReplyBytes {
		t.Fatal("same-root compact restore exceeds unchanged Core request capacity", len(raw), err)
	}
	if len(request.Owners) != 3 {
		t.Fatal("same-root public review omitted original snapshot, artifacts or approvals")
	}
	inline := request
	inline.Owners = append([]durablevolume.PreparationOwner(nil), request.Owners...)
	for index := range inline.Owners {
		owner := &inline.Owners[index]
		switch owner.Kind {
		case storageNativeProducerKind:
			var compact storageNativeProducerScope
			if err := decodePlanJson(owner.Inputs, &compact); err != nil {
				t.Fatal(err)
			}
			expanded, _, err := loadStorageNativeProducerScope(target.target.ctx, compact)
			if err != nil || compact.FeePolicyHash != rootObjectHash(scope.Policy.Execution.FeeCensus) || compact.Policy.Execution.FeeCensus.Participants != nil || !reflect.DeepEqual(expanded.Policy, scope.Policy) {
				t.Fatal("artifact owner lost complete original policy identity", err)
			}
			owner.Inputs, err = json.Marshal(expanded)
			if err != nil {
				t.Fatal(err)
			}
		case storageNativeApprovalKind:
			var approval storageNativeApprovalScope
			if err := decodePlanJson(owner.Inputs, &approval); err != nil {
				t.Fatal(err)
			}
			expanded, _, err := loadStorageNativeProducerScope(target.target.ctx, approval.Native)
			if err != nil || !reflect.DeepEqual(expanded.Policy, scope.Policy) {
				t.Fatal("approval owner lost complete original policy identity", err)
			}
			approval.Native = expanded
			owner.Inputs, err = json.Marshal(approval)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	inlineRaw, err := json.Marshal(inline)
	if err != nil || len(inlineRaw) <= maxRpcReplyBytes {
		t.Fatal("full original duplicated roster did not cross Core request limit", len(inlineRaw), err)
	}
	if err := os.WriteFile(target.target.requestPath, inlineRaw, 0600); err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, target.target.root)
	var output, diagnostic bytes.Buffer
	if code := runMain(target.target.ctx, []string{"storage-prepare", "plan", "--request", target.target.requestPath, "--request-sha256", monitorReadDigest(inlineRaw)}, &output, &diagnostic); code == 0 || output.Len() != 0 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, target.target.root)) {
		t.Fatal("unchanged Core request bound admitted duplicated roster or changed target", code, diagnostic.String())
	}
	if err := os.WriteFile(target.target.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	target.target.requestHash = monitorReadDigest(raw)
	restored := target.apply(t)
	// The actual accepted plan is idempotent; restored custody gains no jobs.
	output.Reset()
	diagnostic.Reset()
	planPath := filepath.Join(target.target.metadata, "continuation-plan.json")
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := runMain(target.target.ctx, []string{"storage-prepare", "apply", "--plan", planPath, "--plan-sha256", monitorReadDigest(planRaw)}, &output, &diagnostic); code != 0 {
		t.Fatal("same original compact plan did not reconcile", code, diagnostic.String())
	}
	if authorities, err := loadNativeProducerAuthorities(restored, scope.Policy); err != nil || len(authorities) != 1 || len(authorities[0].value.FeeCensus.Participants) != 2*rootCensusLimit {
		t.Fatal("restored original roster changed its independent signature domain", err)
	}
	if raw, err := os.ReadFile(scope.Policy.Execution.Producer.Authority.Path); err != nil || !bytes.Equal(raw, original) {
		t.Fatal("same-root restore changed exact original large approval", err)
	}
	if raw, err := os.ReadFile(pending); err != nil || !bytes.Equal(raw, []byte{1, 2, 3}) {
		t.Fatal("same-root restore discarded unknown pending bytes", err)
	}
}

// A hash-bound projection is neither a replacement roster nor signature
// authority. Every refusal leaves original copied custody byte-identical.
func TestNativeProducerRestoreCompactFeePolicyRequiresExactOriginalRoster(t *testing.T) {
	root := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, root)
	original := nativeRestoreFullFeeTestScope(t, root)
	compact, err := compactStorageNativeProducerScope(original)
	if err != nil {
		t.Fatal(err)
	}
	expanded, authorities, err := loadStorageNativeProducerScope(t.Context(), compact)
	if err != nil || !reflect.DeepEqual(expanded, original) || len(authorities) != 1 || len(original.Policy.Execution.FeeCensus.Participants) != 2*rootCensusLimit {
		t.Fatal("compact original scope changed full economic identity or caller policy", err)
	}
	for _, fault := range []string{"missing-hash", "foreign-hash", "missing-descriptor", "foreign-review", "inline-roster", "empty-roster", "mixed-bytes", "missing-source", "changed-roster", "foreign-signature", "cancel"} {
		raw, err := json.Marshal(compact)
		if err != nil {
			t.Fatal(err)
		}
		var changed storageNativeProducerScope
		if err := decodePlanJson(raw, &changed); err != nil {
			t.Fatal(err)
		}
		readContext, cancel := context.WithCancel(t.Context())
		switch fault {
		case "missing-hash":
			changed.FeePolicyHash = ""
		case "foreign-hash":
			changed.FeePolicyHash = monitorReadDigest([]byte("foreign original fee roster"))
		case "missing-descriptor":
			changed.Policy.Execution.FeeCensus = nil
		case "foreign-review":
			changed.Policy.Execution.FeeCensus.ReviewSha256 = monitorReadDigest([]byte("foreign original fee review"))
		case "inline-roster":
			changed.Policy.Execution.FeeCensus.Participants = original.Policy.Execution.FeeCensus.Participants
		case "empty-roster":
			changed.Policy.Execution.FeeCensus.Participants = []string{}
		case "mixed-bytes":
			changed.Approvals = [][]byte{}
		case "missing-source":
			changed.ApprovalSources = nil
		case "changed-roster", "foreign-signature":
			var authority nativeProducerAuthority
			raw, err := os.ReadFile(original.ApprovalSources[0].Reference.Path)
			if err != nil || decodePlanJson(raw, &authority) != nil {
				t.Fatal("original approval fixture unreadable", err)
			}
			key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
			if fault == "changed-roster" {
				authority.FeeCensus.Participants = authority.FeeCensus.Participants[1:]
			} else {
				key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize))
			}
			message, err := authority.signingBytes()
			if err != nil {
				t.Fatal(err)
			}
			authority.Signature = hex.EncodeToString(ed25519.Sign(key, message))
			reference := nativeRenewalTestWrite(t, filepath.Join(root, fault+".json"), authority)
			raw, err = os.ReadFile(reference.Path)
			if err != nil {
				t.Fatal(err)
			}
			changed.Policy.Execution.Producer.Authority = reference
			changed.ApprovalSources[0] = storageNativeApprovalSource{Reference: reference, Bytes: uint64(len(raw))}
		case "cancel":
			cancel()
		}
		before := mainnetNamespaceTest(t, root)
		value, authorities, err := loadStorageNativeProducerScope(readContext, changed)
		cancel()
		if err == nil || authorities != nil || value.Policy.Execution != nil || !reflect.DeepEqual(before, mainnetNamespaceTest(t, root)) {
			t.Fatal("changed compact roster acquired original authority or effects", fault, err)
		}
	}
	if expanded, _, err := loadStorageNativeProducerScope(t.Context(), compact); err != nil || !reflect.DeepEqual(expanded.Policy, original.Policy) {
		t.Fatal("refused compact projection stopped exact original admission", err)
	}
}

// Legacy nil fee policy retains the exact serialized inputs and approval
// profile. A stray projection hash cannot turn nil into fee-enabled authority.
func TestNativeProducerRestoreCompactFeePolicyKeepsLegacyInputs(t *testing.T) {
	ctx, policy, expected, _, files := nativeRenewalTestInputs(t)
	defer files.close()
	scope := storageNativeProducerScope{Schema: storageNativeProducerSchema, Policy: policy, Cursor: policy.From, Checkpoint: monitorHistoryReference{Path: filepath.Join(filepath.Dir(files.path), "checkpoint.json"), Sha256: monitorReadDigest([]byte("synthetic original checkpoint")), Bytes: 29}}
	for _, reference := range append([]planFileReference{policy.Execution.Producer.Authority}, policy.Execution.Producer.Renewals...) {
		raw, err := nativeProducerReadApproval(ctx, reference)
		if err != nil {
			t.Fatal(err)
		}
		scope.Approvals = append(scope.Approvals, raw)
	}
	before, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := compactStorageNativeProducerScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(compact)
	if err != nil || !bytes.Equal(before, after) || bytes.Contains(after, []byte("original_fee_policy_hash")) {
		t.Fatal("legacy nil fee profile changed serialized restore scope", err)
	}
	if actual, err := storageNativeProducerAuthorities(ctx, compact); err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatal("legacy exact original signed lineage changed", err)
	}
	compact.FeePolicyHash = monitorReadDigest([]byte("foreign original fee scope"))
	if actual, err := storageNativeProducerAuthorities(ctx, compact); err == nil || actual != nil {
		t.Fatal("legacy nil policy borrowed compact fee-enabled authority", err)
	}
}
