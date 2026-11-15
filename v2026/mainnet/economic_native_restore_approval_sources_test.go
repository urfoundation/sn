//go:build linux || darwin

// Compact restore inputs retain original signed-file authority and physical
// copied custody. They do not increase Core's one-MiB owner-input envelope.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func TestNativeProducerRestoreCompactSourcesKeepLargeOriginalApprovals(t *testing.T) {
	f := newNativeProducerRestoreNamespaceFixture(t, 2, true, "large-approvals")
	if len(f.producer.authority.Providers) != rootCensusLimit {
		t.Fatal("large original approval omitted an admitted provider pair")
	}
	cohortReference := f.combined.plan(t)
	raw, err := os.ReadFile(cohortReference.Path)
	if err != nil {
		t.Fatal(err)
	}
	var cohort durablevolume.PreparationCohort
	if err := decodePlanJson(raw, &cohort); err != nil {
		t.Fatal(err)
	}
	checked := false
	for _, reference := range cohort.Plans {
		raw, err := os.ReadFile(reference.Path)
		if err != nil || monitorReadDigest(raw) != reference.Sha256 {
			t.Fatal("original retained native plan changed", err)
		}
		var plan durablevolume.PreparationPlan
		if err := decodePlanJson(raw, &plan); err != nil {
			t.Fatal(err)
		}
		for _, owner := range plan.Owners {
			if len(owner.Owner.Inputs) > maxRpcReplyBytes {
				t.Fatal("compact native plan exceeded the unchanged Core owner-input bound")
			}
			if owner.Owner.Kind != storageNativeProducerKind {
				continue
			}
			var scope storageNativeProducerScope
			if err := decodePlanJson(owner.Owner.Inputs, &scope); err != nil {
				t.Fatal(err)
			}
			if scope.Approvals != nil || len(scope.ApprovalSources) != 2 {
				t.Fatal("compact source lost original authority or renewal")
			}
			for _, source := range scope.ApprovalSources {
				raw, err := nativeProducerReadApproval(f.combined.archive.ctx, source.Reference)
				if err != nil || uint64(len(raw)) != source.Bytes {
					t.Fatal("retained compact source changed its exact bytes", err)
				}
				scope.Approvals = append(scope.Approvals, raw)
			}
			scope.ApprovalSources = nil
			inline, err := json.Marshal(scope)
			if err != nil || len(inline) <= maxRpcReplyBytes {
				t.Fatal("large original documents did not exceed legacy inline owner capacity", len(inline), err)
			}
			checked = true
		}
	}
	if !checked {
		t.Fatal("complete public restore plan omitted original native owner")
	}
	f.combined.apply(t, cohortReference, true)
	p := f.producer
	p.ctx = context.WithValue(f.combined.archive.ctx, nativeProducerStateKey{}, f.combined.state.Native.ExecutionProducer)
	observation, code, issue := p.command(t)
	if code != 0 || !observation.Complete || observation.ExecutionProducer == nil || observation.ExecutionProducer.Completed != 3 || observation.ExecutionProducer.Cursor.Number != 103 || len(observation.ExecutionProducer.AuthorityRevisions) != 1 || p.proofs.Load() != f.proofs || p.blocks.Load()-f.blocks != 1 {
		t.Fatal("compact original approval restore did not resume exact retained work", code, issue)
	}
}

func TestNativeProducerRestoreApprovalSourcesRequireExactOriginalBytes(t *testing.T) {
	ctx, policy, original, _, files := nativeRenewalTestInputs(t)
	defer files.close()
	root := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, root)
	scope := storageNativeProducerScope{Schema: storageNativeProducerSchema, Policy: policy, Cursor: policy.From, Checkpoint: monitorHistoryReference{Path: filepath.Join(root, "checkpoint.json"), Sha256: monitorReadDigest([]byte("synthetic original checkpoint")), Bytes: 29}}
	var originals [][]byte
	for index, reference := range append([]planFileReference{policy.Execution.Producer.Authority}, policy.Execution.Producer.Renewals...) {
		raw, err := nativeProducerReadApproval(ctx, reference)
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, raw)
		path := filepath.Join(root, []string{"original.json", "renewal.json"}[index])
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		scope.ApprovalSources = append(scope.ApprovalSources, storageNativeApprovalSource{Reference: planFileReference{Path: path, Sha256: reference.Sha256}, Bytes: uint64(len(raw))})
	}
	if loaded, err := storageNativeProducerAuthorities(ctx, scope); err != nil || !reflect.DeepEqual(loaded, original) {
		t.Fatal("exact compact approval sources differ from original semantic reader", err)
	}
	for _, fault := range []string{"mixed", "omitted", "length", "pin", "bytes", "missing", "symlink", "cancel"} {
		changed := scope
		changed.ApprovalSources = append([]storageNativeApprovalSource(nil), scope.ApprovalSources...)
		first := &changed.ApprovalSources[0]
		readContext, cancel := context.WithCancel(ctx)
		switch fault {
		case "mixed":
			changed.Approvals = originals
		case "omitted":
			changed.ApprovalSources = changed.ApprovalSources[:1]
		case "length":
			first.Bytes++
		case "pin":
			first.Reference.Sha256 = "sha256:" + strings.Repeat("f", 64)
		case "bytes":
			first.Reference.Path = filepath.Join(root, "changed.json")
			if err := os.WriteFile(first.Reference.Path, append([]byte("!"), originals[0][1:]...), 0600); err != nil {
				t.Fatal(err)
			}
		case "missing":
			first.Reference.Path = filepath.Join(root, "missing.json")
		case "symlink":
			first.Reference.Path = filepath.Join(root, "linked.json")
			if err := os.Symlink(scope.ApprovalSources[0].Reference.Path, first.Reference.Path); err != nil {
				t.Fatal(err)
			}
		case "cancel":
			cancel()
		}
		before := mainnetNamespaceTest(t, root)
		loaded, err := storageNativeProducerAuthorities(readContext, changed)
		cancel()
		if err == nil || loaded != nil || !reflect.DeepEqual(before, mainnetNamespaceTest(t, root)) {
			t.Fatal("changed compact approval sources acquired authority or effects", fault, err)
		}
	}
	for index, source := range scope.ApprovalSources {
		if raw, err := os.ReadFile(source.Reference.Path); err != nil || !bytes.Equal(raw, originals[index]) {
			t.Fatal("refused compact source changed original copied bytes", err)
		}
	}
	if loaded, err := storageNativeProducerAuthorities(ctx, scope); err != nil || !reflect.DeepEqual(loaded, original) {
		t.Fatal("refused source stopped original signed approval admission", err)
	}
}
