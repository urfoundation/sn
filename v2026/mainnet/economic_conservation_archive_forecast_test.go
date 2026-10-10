//go:build linux || darwin

// Reviewed capacity covers serialized accepted paths. These public planners
// must reserve the bytes an original successor really needs after escaping.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func economicConservationForecastTestReferenceBytes(t *testing.T) uint64 {
	t.Helper()
	path := "/" + strings.Repeat(strings.Repeat("\x01", 220)+"/", 4) + strings.Repeat("\x01", 139)
	reference := monitorHistoryReference{Path: path, Sha256: monitorReadDigest([]byte("synthetic maximum escaped future original")), Bytes: economicConservationStorageMaximum}
	if len(path) != maximumMonitorHistoryPath || reference.validateLimit(economicConservationStorageMaximum) != nil {
		t.Fatal("future reference fixture does not reach the admitted path limit")
	}
	raw, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	return uint64(len(raw)) + 1
}

func TestEconomicConservationArchiveForecastCoversEscapedOriginalPaths(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, true)
	f.request.FutureSegments = 3
	plan, args := f.plan(t)
	minimum := 2 * (plan.Next.Bytes + f.request.FutureSegments*economicConservationForecastTestReferenceBytes(t))
	if plan.RequiredHeadBytes < minimum {
		t.Fatal("combined forecast cannot retain admitted escaped original paths", plan.RequiredHeadBytes, minimum)
	}
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("escaped-path reserve stopped valid original archive progress", code, issue)
	}
	f.sample(t, monitorServiceHooks{})
}

func TestEconomicConservationArchiveEscapedForecastRefusesBeforeEffects(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, true)
	// Admit the physical reserve independently so the actual serialized head
	// limit, rather than a smaller byte floor, decides this proposed forecast.
	reference, present := durablevolume.ReferenceFromContext(f.ctx)
	if !present {
		t.Fatal("original forecast declaration is absent")
	}
	config, err := durablevolume.Load(reference)
	if err != nil {
		t.Fatal(err)
	}
	for index := range config.Volumes {
		config.Volumes[index].MinAvailableBytes = 512 * 1024 * 1024
	}
	declaration, pin := f.document(t, "forecast-volumes.json", config)
	f.ctx = durablevolume.WithReference(f.ctx, durablevolume.Reference{Path: declaration, Sha256: pin})
	f.request.FutureSegments = 100
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	path, pin := f.document(t, "escaped-head-capacity-request.json", f.request)
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", pin}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "segment/index/head forecast") || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("escaped future head exhaustion created archive authority or changed original custody", code, diagnostic.String())
	}
	f.request.FutureSegments = 1
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("refused future head demand stopped smaller original progress", code, issue)
	}
	f.sample(t, monitorServiceHooks{})
}

func TestEconomicConservationArchiveForecastMeasuresOriginalNativeApprovalHead(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	if len(references) != 1 {
		t.Fatal("native approval fixture changed its exact original review census")
	}
	raw, err := os.ReadFile(references[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	directory := f.metadata
	for range 6 {
		directory = filepath.Join(directory, strings.Repeat("\x01", 240))
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	references[0].Path = filepath.Join(directory, "original-renewal.json")
	if len(references[0].Path) <= maximumMonitorHistoryPath || !bootstrapRootAbsolutePath(references[0].Path) {
		t.Fatal("native review fixture did not exercise its own accepted path grammar")
	}
	if err := os.WriteFile(references[0].Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	adoption, plan, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic long escaped native review")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("original escaped native review could not be adopted", code, issue)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("original escaped native review could not be retained", code, issue)
	}
	state := f.source.state(t)
	if state.NativeRenewal != nil || state.Archive.NativeApprovalHead == nil || state.Archive.NativeApprovalHead.Hash != rootObjectHash(adoption) || !reflect.DeepEqual(state.Archive.NativeApprovalHead.Renewals, references) {
		t.Fatal("native head forecast changed original adopted lineage")
	}
	head, err := json.Marshal(state.Archive.NativeApprovalHead)
	if err != nil {
		t.Fatal(err)
	}
	minimum := 2 * (plan.Next.Bytes + plan.Request.FutureSegments*economicConservationForecastTestReferenceBytes(t) + uint64(len(head)))
	if plan.RequiredHeadBytes < minimum {
		t.Fatal("native forecast omitted actual escaped original approval head", plan.RequiredHeadBytes, minimum)
	}
}
