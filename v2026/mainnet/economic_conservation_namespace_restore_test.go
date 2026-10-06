//go:build linux

// Physical restore composes the complete combined Claim owner with the full
// admitted namespace. Every signed path is fixed before original publication.
package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func economicConservationNamespaceRestoreContinues(t *testing.T, profile string) {
	t.Helper()
	f := newEconomicConservationRestoreNamespaceFixture(t, t.TempDir(), 2, 2, profile, nil)
	original := f.state.Archive.Segments
	for _, reference := range original {
		relative, err := filepath.Rel(f.sources[1].root, reference.Path)
		if err != nil || len(strings.Split(relative, "/")) != 32 || profile == "longest" && (len(reference.Path) != maximumMonitorHistoryPath || len(filepath.Base(reference.Path)) != 155) {
			t.Fatal("combined original did not reach accepted namespace limits", err, len(reference.Path))
		}
	}
	f.apply(t, f.plan(t), true)
	summary := f.archive.sample(t, monitorServiceHooks{})
	state := f.archive.source.state(t)
	if summary.TargetMet != nil || summary.MatchedReceipts != 1 || summary.AggregatePayments != 1 || !summary.NativeCurrent || !summary.VaultCurrent || len(state.ClaimStates[0].Epochs) != 128 || state.ClaimStates[0].Epochs[0].Epoch != 2 || state.ClaimWindows[0].Ordinal != 2 || !reflect.DeepEqual(state.Archive.Segments, original) {
		t.Fatal("restored namespace lost original complete Claim progress", summary)
	}
	f.nextArchive(t, f.future)
	_, _, args := economicConservationClaimWindowTestPlan(t, f.archive, economicConservationClaimWindowTestNext(t, f.archive), monitorReadDigest([]byte("synthetic complete namespace continuation review")))
	if code, issue := f.archive.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("restored complete namespace cannot continue original signed Claim owner", code, issue)
	}
	state = f.archive.source.state(t)
	if state.ClaimWindows[0].Ordinal != 3 || state.ClaimWindows[0].Original.Path != f.request.Original.Path || len(state.ClaimStates[0].Epochs) != 128 || state.ClaimStates[0].Epochs[0].Epoch != 2 || state.PolicyHash != f.state.PolicyHash || state.Native.Cursor.Number < f.state.Native.Cursor.Number || state.Vault.Cursor.Number < f.state.Vault.Cursor.Number || len(state.Archive.Segments) != 3 || !reflect.DeepEqual(state.Archive.Segments[:2], original) || state.Archive.Segments[2].Path != f.future {
		t.Fatal("namespace continuation reset original signed authority, history or liabilities")
	}
	f.archive.sample(t, monitorServiceHooks{})
}

func TestEconomicConservationRestoreDeepestNamespaceKeepsCompleteClaimOwner(t *testing.T) {
	economicConservationNamespaceRestoreContinues(t, "deepest")
}

func TestEconomicConservationRestoreLongestEscapedNamespaceKeepsCompleteClaimOwner(t *testing.T) {
	economicConservationNamespaceRestoreContinues(t, "longest")
}

// A valid complete plan precedes each refused expansion. The same original
// physical targets remain untouched and can later apply that retained plan.
func TestEconomicConservationRestoreNamespaceOverflowRefusesBeforeEffects(t *testing.T) {
	f := newEconomicConservationRestoreNamespaceFixture(t, t.TempDir(), 2, 1, "deepest", nil)
	accepted := f.plan(t)
	valid, err := json.Marshal(f.request)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"depth", "original-path"} {
		var request economicConservationRestoreRequest
		if err := json.Unmarshal(valid, &request); err != nil {
			t.Fatal(err)
		}
		if fault == "depth" {
			request.Preparations[1].Limits.MaxDepth = 33
		} else {
			request.Original.Path = "/" + strings.Repeat("d", maximumMonitorHistoryPath)
		}
		var output, diagnostic bytes.Buffer
		if code := runMain(f.archive.ctx, f.args(t, request), &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("combined namespace overflow acquired target authority", fault, code, diagnostic.String())
		}
		f.unchanged(t)
	}
	f.apply(t, accepted, false)
	if summary := f.archive.sample(t, monitorServiceHooks{}); summary.MatchedReceipts != 1 || !summary.NativeCurrent || !summary.VaultCurrent {
		t.Fatal("refused namespace expansion stopped healthy original progress", summary)
	}
}
