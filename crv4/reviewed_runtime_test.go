// Catalog regression checks bind shared runtime authority to retained artifact
// provenance and keep returned snapshots from mutating later admission.
package crv4

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The independently pinned artifact manifest includes every predecessor. A
// moving current alias must never replace a retained historical catalog row.
func TestRuntimeArtifactMetadataCatalogPreservesExactManifest(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-metadata-artifacts.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Artifacts []struct {
			SpecVersion  uint32 `json:"spec_version"`
			CodeHash     string `json:"code_blake2b_256"`
			MetadataHash string `json:"metadata_blake2b_256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	wantSpecs := []uint32{451, 452, 453, 454, 455, 458, 459, 460, 461}
	artifacts := ReviewedRuntimeArtifacts()
	if len(artifacts) != len(manifest.Artifacts) || len(artifacts) != len(wantSpecs) {
		t.Fatal("catalog lost an exact retained artifact")
	}
	for index, row := range manifest.Artifacts {
		want := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: row.SpecVersion, TransactionVersion: 1, StateVersion: 1}, CodeHash: row.CodeHash, MetadataHash: row.MetadataHash}
		if row.SpecVersion != wantSpecs[index] || artifacts[index] != want {
			t.Fatalf("catalog artifact%d changed: %+v", index, artifacts[index])
		}
		if got, ok := ReviewedRuntimeArtifact(want.Version); !ok || got != want {
			t.Fatal("exact catalog lookup differs")
		}
		for _, version := range []RuntimeVersionIdentity{
			{SpecName: "synthetic-foreign", SpecVersion: row.SpecVersion, TransactionVersion: 1, StateVersion: 1},
			{SpecName: "node-subtensor", SpecVersion: row.SpecVersion, TransactionVersion: 2, StateVersion: 1},
			{SpecName: "node-subtensor", SpecVersion: row.SpecVersion, TransactionVersion: 1, StateVersion: 2},
		} {
			if _, ok := ReviewedRuntimeArtifact(version); ok {
				t.Fatal("changed version inherited a reviewed artifact")
			}
		}
	}
	current, ok := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: ReviewedRuntimeSpecVersion, TransactionVersion: 1, StateVersion: 1})
	if !ok || current != artifacts[len(artifacts)-1] || current.CodeHash != ReviewedRuntimeCodeHash || current.MetadataHash != ReviewedRuntimeMetadataHash {
		t.Fatal("current selection is not the exact latest catalog artifact")
	}
	copyArtifacts := append([]RuntimeArtifactIdentity(nil), artifacts...)
	artifacts[0] = RuntimeArtifactIdentity{}
	if !reflect.DeepEqual(ReviewedRuntimeArtifacts(), copyArtifacts) {
		t.Fatal("caller mutated reviewed artifact authority")
	}
	for _, spec := range []uint32{456, 457, 462} {
		if _, ok := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: spec, TransactionVersion: 1, StateVersion: 1}); ok {
			t.Fatal("unreviewed runtime entered the catalog")
		}
	}
}
