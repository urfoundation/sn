// Capture admission uses the matching Rust whole-response ceiling. Principal
// queries select additional evidence within that ceiling, not a larger worker.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reach the real sealed-engine handoff for each admitted profile, then cancel
// deterministically before launch. The original surcharge refused native
// effects before this boundary, even with a small complete request.
func TestHistoricalCapturePrincipalProfilesReachOwnedWorker(t *testing.T) {
	request, original := historicalCaptureTestRequest(t, 2, 0xe0, true)
	query := historicalPrincipalTestValue(14).Query
	for _, entry := range []struct {
		name    string
		schema  string
		queries bool
		effects bool
	}{
		{name: "plain", schema: "", queries: false, effects: false},
		{name: "plain-principal", schema: "", queries: true, effects: true},
		{name: "legacy-principal", schema: "urnetwork-original-wasm-hook-observation-v1", queries: true, effects: true},
		{name: "native-opening", schema: historicalNativeProfileSchema, queries: true, effects: false},
		{name: "native-effects", schema: historicalNativeProfileSchema, queries: true, effects: true},
	} {
		input := original
		if entry.schema == "" {
			input.ObservationProfile = nil
		} else {
			profile := *original.ObservationProfile
			profile.Schema = entry.schema
			input.ObservationProfile = &profile
		}
		if entry.queries {
			input.PrincipalQueries = []historicalPrincipalQuery{query}
		}
		input.PrincipalEffects = entry.effects
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request.Input.Path = filepath.Join(filepath.Dir(request.Input.Path), entry.name+".json")
		request.Input.Sha256 = monitorReadDigest(raw)
		if err := os.WriteFile(request.Input.Path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		admitted, started := false, false
		report, err := runHistoricalCapture(ctx, request, historicalReplayHooks{
			beforeStart: func(context.Context, *os.File) { admitted = true; cancel() },
			afterStart:  func(context.Context, int) { started = true },
		})
		cancel()
		if !admitted || started || report != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("declared principal capture did not reach its owned worker", entry.name, admitted, started, err)
		}
	}
}

// The accepted inclusive ceiling and first disallowed byte exercise actual
// admission, without allocating a giant fake report or weakening pipe limits.
func TestHistoricalCaptureFixedWorkerCeilingRemainsExact(t *testing.T) {
	base := historicalReplayTestRequest(t, "0x00")
	raw, err := os.ReadFile(base.Job.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, maximum := range []int{historicalNativeCaptureReportLimit, historicalNativeCaptureReportLimit + 1, 0, -1} {
		ctx, cancel := context.WithCancel(t.Context())
		admitted := false
		output, err := runHistoricalProofWorker(ctx, cancel, historicalProofWorkerRequest{Engine: base.Engine, Input: raw, Directory: filepath.Dir(base.Job.Path), MaximumReport: maximum}, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { admitted = true; cancel() }})
		cancel()
		if maximum == historicalNativeCaptureReportLimit {
			if !admitted || output != nil || !errors.Is(err, context.Canceled) {
				t.Fatal("exact complete capture ceiling was not admitted", admitted, err)
			}
		} else if admitted || output != nil || err == nil || !strings.Contains(err.Error(), "exceeds its fixed profile") {
			t.Fatal("out-of-profile worker declaration crossed engine admission", maximum, admitted, err)
		}
	}
}

// Required original query identity is checked before a child is admitted;
// aligning the output cap does not make an incomplete effects request valid.
func TestHistoricalCapturePrincipalEffectsStillRequireOriginalQueries(t *testing.T) {
	request, input := historicalCaptureTestRequest(t, 2, 0xe0, true)
	input.ObservationProfile.Schema = historicalNativeProfileSchema
	input.PrincipalEffects = true
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request.Input.Sha256 = monitorReadDigest(raw)
	if err := os.WriteFile(request.Input.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	admitted := false
	report, err := runHistoricalCapture(t.Context(), request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { admitted = true }})
	if admitted || report != nil || err == nil || !strings.Contains(err.Error(), "omitted their original query census") {
		t.Fatal("capture bound correction bypassed original principal census", admitted, err)
	}
}
