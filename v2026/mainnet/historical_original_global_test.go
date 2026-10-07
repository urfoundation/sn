// Alias shape tests exercise the public profile boundary; actual original
// section equality and Wasmtime capture are covered in the Rust engine.
package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func originalGlobalTestProfile() historicalReplayObservationProfile {
	name := "__urnetwork_observe_global_0"
	return historicalReplayObservationProfile{
		Schema:            historicalNativeProfileSchema,
		RuntimeCodeSha256: historicalReplayDigest{1}, SourceReviewSha256: historicalReplayDigest{2},
		Rules:           []historicalReplayHookRule{{Purpose: "native-epoch", FunctionIndex: 1, FunctionBodySha256: historicalReplayDigest{3}, OffsetStart: 1, OffsetEnd: 7, Memory: []historicalNativeCapture{{Name: "netuid", Global: &name, Bytes: 2}}}},
		OriginalGlobals: []historicalOriginalGlobal{{GlobalIndex: 0, ExportName: name}},
	}
}

func TestHistoricalOriginalGlobalProfileRetainsExactOptionalWire(t *testing.T) {
	profile := originalGlobalTestProfile()
	if err := profile.validate(historicalReplayJob{RuntimeCodeSha256: profile.RuntimeCodeSha256}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profile)
	if err != nil || !bytes.HasSuffix(raw, []byte(`,"original_globals":[{"global_index":0,"export_name":"__urnetwork_observe_global_0"}]}`)) {
		t.Fatal("alias serialization differs", string(raw), err)
	}
	profile.OriginalGlobals = nil
	profile.Rules[0].Memory[0].Global = nil
	raw, err = json.Marshal(profile)
	if err != nil || bytes.Contains(raw, []byte("original_globals")) {
		t.Fatal("legacy bytes acquired an alias field", err)
	}
	if err := profile.validate(historicalReplayJob{RuntimeCodeSha256: profile.RuntimeCodeSha256}); err != nil {
		t.Fatal("old profile refused", err)
	}
}

func TestHistoricalOriginalGlobalProfileRefusesMissingUnusedAndReorderedAliases(t *testing.T) {
	for _, mutate := range []func(*historicalReplayObservationProfile){
		func(p *historicalReplayObservationProfile) { p.OriginalGlobals = nil },
		func(p *historicalReplayObservationProfile) { p.Rules[0].Memory[0].Global = nil },
		func(p *historicalReplayObservationProfile) { p.OriginalGlobals[0].ExportName = "__stack_pointer" },
		func(p *historicalReplayObservationProfile) {
			p.OriginalGlobals = append(p.OriginalGlobals, p.OriginalGlobals[0])
		},
		func(p *historicalReplayObservationProfile) { p.OriginalGlobals[0].GlobalIndex = 1 },
		func(p *historicalReplayObservationProfile) {
			p.OriginalGlobals = append(p.OriginalGlobals, make([]historicalOriginalGlobal, 4)...)
		},
		func(p *historicalReplayObservationProfile) { p.Schema = "urnetwork-original-wasm-hook-observation-v1" },
	} {
		profile := originalGlobalTestProfile()
		mutate(&profile)
		if err := profile.validate(historicalReplayJob{RuntimeCodeSha256: profile.RuntimeCodeSha256}); err == nil {
			t.Fatal("invalid original-global declaration became an admitted profile")
		}
	}
}
