// Exercise artifact discovery's parsing bound and exact JSON semantics without
// replacing any release-scale cryptographic or public replay qualification.
package main

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Nested objects and arrays must not submit their already parsed bytes again.
func TestFinalSemanticCampaignArtifactReferencesDecodeEachByteOnce(t *testing.T) {
	t.Parallel()
	hash := "sha256:" + strings.Repeat("ab", 32)
	raw := []byte(fmt.Sprintf(`{"kind":"proof","uri":"proofs/current.json","content_sha256":%q,"size_bytes":1,"padding":%q}`, hash, strings.Repeat("x", 8192)))
	for range 16 {
		raw = append(append([]byte(`{"nested":[null,true,"leaf",`), raw...), []byte(`]}`)...)
	}
	references := map[string]campaignArtifactReference{}
	decodeCalls, decodeBytes := 0, 0
	err := collectCampaignArtifactReferencesWithDecode(raw, references, 0, func(data []byte, value any) error {
		decodeCalls++
		decodeBytes += len(data)
		return decodeCampaignArtifactReferenceJSON(data, value)
	})
	want := map[string]campaignArtifactReference{"proofs/current.json": {Kind: "proof", URI: "proofs/current.json", ContentHash: hash, Size: 1}}
	if err != nil || !reflect.DeepEqual(references, want) {
		t.Fatalf("complete nested locator census = %#v, error = %v", references, err)
	}
	if decodeCalls != 1 || decodeBytes != len(raw) {
		t.Fatalf("artifact JSON parsing work = %d calls / %d bytes, want 1 call / %d input bytes", decodeCalls, decodeBytes, len(raw))
	}
}

// Discovery must own decoded values and inspect fresh bytes on every call.
func TestFinalSemanticCampaignArtifactReferencesOwnDecodedInput(t *testing.T) {
	t.Parallel()
	hash := "sha256:" + strings.Repeat("ab", 32)
	raw := []byte(fmt.Sprintf(`[{"kind":"proof","uri":"proofs/current.json","content_sha256":%q,"size_bytes":1}]`, hash))
	references := map[string]campaignArtifactReference{}
	cleared := false
	err := collectCampaignArtifactReferencesWithDecode(raw, references, 0, func(data []byte, value any) error {
		if err := decodeCampaignArtifactReferenceJSON(data, value); err != nil {
			return err
		}
		if !cleared {
			clear(raw)
			cleared = true
		}
		return nil
	})
	want := campaignArtifactReference{Kind: "proof", URI: "proofs/current.json", ContentHash: hash, Size: 1}
	if err != nil || !cleared || len(references) != 1 || references[want.URI] != want {
		t.Fatalf("caller buffer mutation reached decoded locators: references = %#v, error = %v", references, err)
	}
	replacement := []byte(fmt.Sprintf(`{"kind":"other-proof","uri":"proofs/current.json","content_sha256":%q,"size_bytes":1}`, hash))
	if err := collectCampaignArtifactReferences(replacement, references, 0); err == nil || !strings.Contains(err.Error(), "conflicting identities") {
		t.Fatalf("new caller bytes reused a prior locator verdict: %v", err)
	}
}

// Object names remain case-sensitive, duplicate keys retain their last value,
// and sizes preserve the integer-only uint64 decoder contract.
func TestFinalSemanticCampaignArtifactReferencesPreserveJSONValues(t *testing.T) {
	t.Parallel()
	hash := "sha256:" + strings.Repeat("ab", 32)
	for _, item := range []struct {
		name      string
		fields    string
		wantSize  uint64
		wantError bool
	}{
		{name: "integer", fields: `"kind":"proof","size_bytes":1`, wantSize: 1},
		{name: "maximum integer", fields: fmt.Sprintf(`"kind":"proof","size_bytes":%d`, maximumCampaignEvidenceRawFileBytes), wantSize: maximumCampaignEvidenceRawFileBytes},
		{name: "last kind", fields: `"kind":"wrong","kind":"proof","size_bytes":1`, wantSize: 1},
		{name: "last size", fields: `"kind":"proof","size_bytes":0,"size_bytes":2`, wantSize: 2},
		{name: "escaped last name", fields: `"kind":"proof","size_bytes":0,"size_\u0062ytes":3`, wantSize: 3},
		{name: "zero", fields: `"kind":"proof","size_bytes":0`, wantError: true},
		{name: "negative zero", fields: `"kind":"proof","size_bytes":-0`, wantError: true},
		{name: "negative", fields: `"kind":"proof","size_bytes":-1`, wantError: true},
		{name: "decimal", fields: `"kind":"proof","size_bytes":1.0`, wantError: true},
		{name: "fraction", fields: `"kind":"proof","size_bytes":1.5`, wantError: true},
		{name: "exponent", fields: `"kind":"proof","size_bytes":1e0`, wantError: true},
		{name: "quoted", fields: `"kind":"proof","size_bytes":"1"`, wantError: true},
		{name: "null", fields: `"kind":"proof","size_bytes":null`, wantError: true},
		{name: "array", fields: `"kind":"proof","size_bytes":[]`, wantError: true},
		{name: "object", fields: `"kind":"proof","size_bytes":{}`, wantError: true},
		{name: "overflow", fields: `"kind":"proof","size_bytes":18446744073709551616`, wantError: true},
		{name: "oversize", fields: fmt.Sprintf(`"kind":"proof","size_bytes":%d`, maximumCampaignEvidenceRawFileBytes+1), wantError: true},
		{name: "wrong case", fields: `"Kind":"proof","Size_Bytes":1`, wantError: true},
		{name: "last kind is null", fields: `"kind":"proof","kind":null,"size_bytes":1`, wantError: true},
		{name: "last size is invalid", fields: `"kind":"proof","size_bytes":1,"size_bytes":null`, wantError: true},
	} {
		raw := []byte(fmt.Sprintf(`{"uri":"proofs/current.json","content_sha256":%q,%s}`, strings.ToUpper(hash), item.fields))
		references := map[string]campaignArtifactReference{}
		err := collectCampaignArtifactReferences(raw, references, 0)
		if item.wantError {
			if err == nil || !strings.Contains(err.Error(), "locator is incomplete") {
				t.Errorf("%s: invalid locator error = %v", item.name, err)
			}
			continue
		}
		want := campaignArtifactReference{Kind: "proof", URI: "proofs/current.json", ContentHash: hash, Size: item.wantSize}
		if err != nil || len(references) != 1 || references[want.URI] != want {
			t.Errorf("%s: references = %#v, error = %v", item.name, references, err)
		}
	}
}

// Invalid documents contribute no locators; valid nested values keep the
// existing depth fence, including scalar children of the deepest object.
func TestFinalSemanticCampaignArtifactReferencesKeepDocumentAndDepthBounds(t *testing.T) {
	t.Parallel()
	hash := "sha256:" + strings.Repeat("ab", 32)
	locator := fmt.Sprintf(`{"kind":"proof","uri":"proofs/current.json","content_sha256":%q,"size_bytes":1}`, hash)
	for _, raw := range []string{"", "null", "false", `"string"`, "1e99999", locator + `{}`, locator + `broken`, `{"value":` + locator + `,}`, `{"size_bytes":18446744073709551616}`} {
		references := map[string]campaignArtifactReference{}
		if err := collectCampaignArtifactReferences([]byte(raw), references, 0); err != nil || len(references) != 0 {
			t.Errorf("non-locator document %q produced %#v, error = %v", raw, references, err)
		}
	}
	for _, item := range []struct {
		layers    int
		leaf      string
		wantError bool
	}{
		{layers: maximumCampaignEvidenceJSONDepth, leaf: "0"},
		{layers: maximumCampaignEvidenceJSONDepth + 1, leaf: "0", wantError: true},
		{layers: maximumCampaignEvidenceJSONDepth - 1, leaf: locator},
		{layers: maximumCampaignEvidenceJSONDepth, leaf: locator, wantError: true},
	} {
		raw := []byte(strings.Repeat("[", item.layers) + item.leaf + strings.Repeat("]", item.layers))
		err := collectCampaignArtifactReferences(raw, map[string]campaignArtifactReference{}, 0)
		if item.wantError != (err != nil) || err != nil && !strings.Contains(err.Error(), "maximum depth") {
			t.Errorf("depth %d with leaf %q: error = %v", item.layers, item.leaf, err)
		}
	}
	if err := collectCampaignArtifactReferences(nil, map[string]campaignArtifactReference{}, maximumCampaignEvidenceJSONDepth+1); err == nil || !strings.Contains(err.Error(), "maximum depth") {
		t.Fatalf("initial depth fence was ignored: %v", err)
	}
	// Only the retained value of a duplicate field participates in discovery.
	deep := strings.Repeat("[", maximumCampaignEvidenceJSONDepth+1) + "0" + strings.Repeat("]", maximumCampaignEvidenceJSONDepth+1)
	raw := []byte(`{"ignored":` + deep + `,"ignored":null,"proof":` + locator + `}`)
	if err := collectCampaignArtifactReferences(raw, map[string]campaignArtifactReference{}, 0); err != nil {
		t.Fatalf("superseded duplicate field entered the locator graph: %v", err)
	}
}

// JSONL records merge their complete locator graph and reject conflicting
// identities even when the repeated locator arrives in a later record.
func TestFinalSemanticCampaignArtifactReferencesJoinEveryRecord(t *testing.T) {
	t.Parallel()
	hash := "sha256:" + strings.Repeat("ab", 32)
	first := []byte(fmt.Sprintf(`{"proof":{"kind":"proof","uri":"proofs/first.json","content_sha256":%q,"size_bytes":1}}`, hash))
	second := bytes.ReplaceAll(first, []byte("first.json"), []byte("second.json"))
	files := map[string][]byte{"proofs.jsonl": bytes.Join([][]byte{first, second, first}, []byte{'\n'})}
	references, err := campaignArtifactReferences(files)
	if err != nil || len(references) != 2 {
		t.Fatalf("complete JSONL locator census = %#v, error = %v", references, err)
	}
	conflicting := bytes.ReplaceAll(first, []byte(`"size_bytes":1`), []byte(`"size_bytes":2`))
	files["proofs.jsonl"] = bytes.Join([][]byte{first, second, conflicting}, []byte{'\n'})
	if _, err := campaignArtifactReferences(files); err == nil || !strings.Contains(err.Error(), "line 3") || !strings.Contains(err.Error(), "conflicting identities") {
		t.Fatalf("later conflicting JSONL locator was accepted: %v", err)
	}
}
