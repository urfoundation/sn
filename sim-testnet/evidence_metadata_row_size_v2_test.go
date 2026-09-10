// Independent standard Json oracles pin typed row accounting to the original
// wire, and actual admission/publication controls exercise the connected paths.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Include each raw byte as a string, malformed multibyte forms, Html escaping
// and both Javascript separators. These are data, not accepted identities.
func campaignMetadataRowStringsTestV2() []string {
	values := []string{"", "synthetic", "<>&\"\\\b\f\n\r\t", "\u2028\u2029\ufffd", "\u0080\u07ff\u0800\U00010000\U0010ffff", string([]byte{0xe2, 0x82}), string([]byte{0xed, 0xa0, 0x80}), string([]byte{0xf4, 0x90, 0x80, 0x80}), strings.Repeat("synthetic-<>&\u2028", 4096)}
	for value := 0; value < 256; value++ {
		values = append(values, string([]byte{byte(value)}))
	}
	return values
}

// Count exact Json string content, not Go quoting or Utf8 rune counts.
func TestCampaignEvidenceCapacityV2MetadataRowStringsMatchLegacyJson(t *testing.T) {
	t.Parallel()
	for index, value := range campaignMetadataRowStringsTestV2() {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if got := campaignMetadataStringContentBytesV2(value); got != uint64(len(encoded)-2) {
			t.Fatalf("string width differs from legacy Json at case%d: got%d want%d", index, got, len(encoded)-2)
		}
	}
}

// Every varying leaf, optional Origin, nil/empty suffix and base64 quantum
// retains the exact old deepest-indent row charge, including its twelve bytes.
func TestCampaignEvidenceCapacityV2MetadataRowWidthsMatchLegacyJson(t *testing.T) {
	t.Parallel()
	sizer, err := newCampaignMetadataRowSizerV2()
	if err != nil {
		t.Fatal(err)
	}
	integers := []uint64{0, 1, 9, 10, 99, 100, 9999999999999999999, 10000000000000000000, ^uint64(0)}
	for index, value := range campaignMetadataRowStringsTestV2() {
		source := FinalCollectedValidatorSourceV2{Source: validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: value, Name: value, Origin: value}, Artifact: FinalArtifactLocator{Kind: value, URI: value, ContentHash: value, SizeBytes: integers[index%len(integers)]}}
		encoded, err := json.MarshalIndent(source, "          ", "  ")
		if err != nil || sizer.sourceBytes(source) != uint64(len(encoded)+12) {
			t.Fatalf("source row%d differs: got%d want%d err=%v", index, sizer.sourceBytes(source), len(encoded)+12, err)
		}
		carrier := FinalCollectedPriorCarrierV2{Scope: value, Path: value, EnvelopeHash: value, WireHash: value, LocalHash: value, WireBytes: integers[index%len(integers)], LocalBytes: integers[(index+1)%len(integers)]}
		for _, suffix := range [][]byte{nil, {}, {0}, {0, 1}, {0, 1, 2}, bytes.Repeat([]byte{0xff}, 64*1024+1)} {
			carrier.LocalSuffix = suffix
			encoded, err := json.MarshalIndent(carrier, "          ", "  ")
			if err != nil || sizer.carrierBytes(carrier) != uint64(len(encoded)+12) {
				t.Fatalf("carrier row%d suffix%d differs: got%d want%d err=%v", index, len(suffix), sizer.carrierBytes(carrier), len(encoded)+12, err)
			}
		}
	}
}

// A schema/codec change must update the exact size logic instead of silently
// acquiring zero-width fields in a standard-derived prototype.
func TestCampaignEvidenceCapacityV2MetadataRowSizerPinsEveryField(t *testing.T) {
	t.Parallel()
	marshaler := reflect.TypeFor[json.Marshaler]()
	for _, row := range []struct {
		typeOf reflect.Type
		fields string
	}{
		{typeOf: reflect.TypeFor[FinalCollectedValidatorSourceV2](), fields: "Source:validator.ReleaseEvidenceV2CaptureSource:source|Artifact:main.FinalArtifactLocator:artifact"},
		{typeOf: reflect.TypeFor[validatorpkg.ReleaseEvidenceV2CaptureSource](), fields: "Kind:string:kind|Name:string:name|Origin:string:origin,omitempty"},
		{typeOf: reflect.TypeFor[FinalArtifactLocator](), fields: "Kind:string:kind|URI:string:uri|ContentHash:string:content_sha256|SizeBytes:uint64:size_bytes"},
		{typeOf: reflect.TypeFor[FinalCollectedPriorCarrierV2](), fields: "Scope:string:scope|Path:string:path|EnvelopeHash:string:envelope_hash|WireHash:string:wire_sha256|WireBytes:uint64:wire_bytes|LocalHash:string:local_sha256|LocalBytes:uint64:local_bytes|LocalSuffix:[]uint8:local_suffix"},
	} {
		if row.typeOf.Implements(marshaler) || reflect.PointerTo(row.typeOf).Implements(marshaler) {
			t.Fatal("row size owner acquired an independent custom codec", row.typeOf)
		}
		var fields []string
		for index := 0; index < row.typeOf.NumField(); index++ {
			field := row.typeOf.Field(index)
			if field.Anonymous || field.PkgPath != "" {
				t.Fatal("row size owner acquired a hidden or embedded field", row.typeOf, field.Name)
			}
			fields = append(fields, field.Name+":"+field.Type.String()+":"+field.Tag.Get("json"))
		}
		if strings.Join(fields, "|") != row.fields {
			t.Fatal("row size field inventory changed", row.typeOf, fields)
		}
	}
}

// Only metadata shape is needed: no synthetic row is accepted as a semantic
// proof. Repeated strings remain borrowed inputs to allocation measurements.
func campaignMetadataRowsValueTestV2(count int) *FinalSemanticCollectedInputs {
	value := &FinalSemanticCollectedInputs{Schema: finalSemanticCollectedInputsSchema, Phase: "synthetic-phase", RunID: "synthetic-run", Validators: []FinalCollectedValidatorInputs{{ValidatorID: 1, EvidenceV2: &FinalCollectedValidatorEvidenceV2{Schema: finalCollectedValidatorEvidenceV2Schema}}}, PriorPhase: &FinalCollectedPriorPhaseInputs{Phase: "synthetic-prior", RunID: "synthetic-prior-run"}}
	for index := 0; index < count; index++ {
		value.Validators[0].EvidenceV2.Sources = append(value.Validators[0].EvidenceV2.Sources, FinalCollectedValidatorSourceV2{Source: validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "synthetic-source", Name: "synthetic-\"<>&", Origin: "https://origin.example"}, Artifact: FinalArtifactLocator{Kind: "synthetic-artifact", URI: fmt.Sprintf("synthetic/object-%d.bin", index), ContentHash: "sha256:" + strings.Repeat("1", 64), SizeBytes: uint64(index + 1)}})
		value.PriorPhase.PublicCarriers = append(value.PriorPhase.PublicCarriers, FinalCollectedPriorCarrierV2{Scope: "run", Path: fmt.Sprintf("synthetic/prior-%d.bin", index), EnvelopeHash: "sha256:" + strings.Repeat("2", 64), WireHash: "sha256:" + strings.Repeat("3", 64), WireBytes: uint64(index + 2), LocalHash: "sha256:" + strings.Repeat("4", 64), LocalBytes: uint64(index + 3), LocalSuffix: []byte{'\n'}})
	}
	return value
}

// The independent old encoder measures every actual leaf and the exact
// original shell. It supplies test expectations, never production admission.
func campaignMetadataLegacyRowsAndResidualTestV2(t *testing.T, value *FinalSemanticCollectedInputs) (uint64, uint64) {
	t.Helper()
	copyValue := *value
	copyValue.Validators = append([]FinalCollectedValidatorInputs(nil), value.Validators...)
	var rows uint64
	measure := func(row any) {
		raw, err := json.MarshalIndent(row, "          ", "  ")
		if err != nil {
			t.Fatal(err)
		}
		rows += uint64(len(raw) + 12)
	}
	for index, validator := range value.Validators {
		if validator.EvidenceV2 == nil {
			continue
		}
		for _, row := range validator.EvidenceV2.Sources {
			measure(row)
		}
		copyEvidence := *validator.EvidenceV2
		copyEvidence.Sources = nil
		copyValue.Validators[index].EvidenceV2 = &copyEvidence
	}
	if value.PriorPhase != nil {
		for _, row := range value.PriorPhase.PublicCarriers {
			measure(row)
		}
		copyPrior := *value.PriorPhase
		copyPrior.PublicCarriers = nil
		copyValue.PriorPhase = &copyPrior
	}
	raw, err := json.MarshalIndent(&copyValue, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return rows, uint64(len(raw) + 1)
}

// The same production admission helper owns exact row-plus-residual bytes,
// both independent censuses and nil-owner refusal; no unsigned assertion fits.
func TestCampaignEvidenceCapacityV2MetadataRowsKeepExactAdmissionBounds(t *testing.T) {
	value := campaignMetadataRowsValueTestV2(2)
	before, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := campaignEvidenceLimitsForConfig(campaignMetadataConfigTestV2(t))
	if err != nil {
		t.Fatal(err)
	}
	metadata := *limits.metadata
	limits.metadata = &metadata
	limits.maximumObjects = 2
	rows, residual := campaignMetadataLegacyRowsAndResidualTestV2(t, value)
	for _, difference := range []int64{-1, 0, 1} {
		metadata.indexBytes = uint64(int64(rows+residual) + difference)
		err := validateCollectedMetadataWithLimitsV2(limits, value)
		if (err == nil) != (difference >= 0) {
			t.Fatalf("legacy exact total admission changed by%d: %v", difference, err)
		}
	}
	metadata.indexBytes = rows + residual
	limits.maximumObjects = 1
	if err := validateCollectedMetadataWithLimitsV2(limits, value); err == nil {
		t.Fatal("one-over source census reached encoding")
	}
	value.Validators[0].EvidenceV2.Sources = value.Validators[0].EvidenceV2.Sources[:1]
	if err := validateCollectedMetadataWithLimitsV2(limits, value); err == nil {
		t.Fatal("one-over independent prior-carrier census reached encoding")
	}
	value.Validators[0].EvidenceV2.Sources = value.Validators[0].EvidenceV2.Sources[:2]
	if err := validateCollectedMetadataWithLimitsV2(limits, nil); err == nil {
		t.Fatal("absent typed metadata owner accepted")
	}
	after, err := json.Marshal(value)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("row admission changed its borrowed object", err)
	}
}

// The ordinary residual owns its original 32 MiB even though designated rows
// have the much larger independent full-census allowance.
func TestCampaignEvidenceCapacityV2MetadataRowsKeepOrdinaryResidualBound(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	value := campaignMetadataRowsValueTestV2(0)
	value.RunID = ""
	_, residual := campaignMetadataLegacyRowsAndResidualTestV2(t, value)
	value.RunID = strings.Repeat("x", maximumCampaignEvidenceRawFileBytes-int(residual))
	if err := validateCollectedMetadataForConfigV2(cfg, value); err != nil {
		t.Fatal("exact original residual refused", err)
	}
	value.RunID += "x"
	if err := validateCollectedMetadataForConfigV2(cfg, value); err == nil {
		t.Fatal("one-over ordinary residual borrowed row capacity")
	}
}

// A restored per-row MarshalIndent necessarily allocates the serialized row
// corpus. Measure the actual cfg/default admission, not an unused width helper
// or elapsed time. This root is serial to keep the byte observation isolated.
func TestCampaignEvidenceCapacityV2MetadataAdmissionKeepsAllocationBelowRows(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	value := campaignMetadataRowsValueTestV2(1024)
	for index := range value.Validators[0].EvidenceV2.Sources {
		value.Validators[0].EvidenceV2.Sources[index].Source.Name = strings.Repeat("synthetic", 128)
	}
	rows, _ := campaignMetadataLegacyRowsAndResidualTestV2(t, value)
	var validationErr error
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			if err := validateCollectedMetadataForConfigV2(cfg, value); err != nil {
				validationErr = err
				return
			}
		}
	})
	if validationErr != nil || result.N == 0 {
		t.Fatal("actual default metadata admission did not complete", validationErr, result.N)
	}
	if allocated := result.AllocedBytesPerOp(); allocated < 0 || uint64(allocated) >= rows/8 {
		t.Fatalf("metadata admission allocated its row corpus: got%d limit%d", allocated, rows/8)
	}
}

// The full-census test uses this same production entrypoint. A demanding
// base64 carrier still matches independently encoded complete legacy wire,
// its old unsigned hash, exact local bytes, and fresh prior authentication.
func TestCampaignEvidenceCapacityV2MetadataActualWriterKeepsLegacyCarrierWire(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	owner := evidenceEncodingOwnerTest(t)
	root := t.TempDir()
	raw := []byte(`{"synthetic":"` + strings.Repeat("x", 1024*1024) + `"}`)
	before := bytes.Clone(raw)
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "synthetic-carrier", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
	localPath := filepath.Join(root, "published", "synthetic.evidence.json")
	envelope, wire, err := prepareLocalEvidence(cfg, root, localPath, campaignEvidenceFileKind, payload.RunID, payload, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(envelope)
	if err != nil || !bytes.Equal(wire, legacy) {
		t.Fatal("actual writer changed the complete legacy carrier", err)
	}
	unsigned, err := evidenceUnsignedBytes(envelope)
	if err != nil || envelope.ContentHash != fmt.Sprintf("sha256:%x", sha256.Sum256(unsigned)) {
		t.Fatal("actual writer changed the independently encoded signature digest", err)
	}
	stored, err := os.ReadFile(localPath)
	if err != nil || !bytes.Equal(stored, append(bytes.Clone(legacy), '\n')) || !bytes.Equal(raw, before) {
		t.Fatal("actual writer changed local or borrowed bytes", err)
	}
	entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
	if err := verifyFinalPriorCarrierWireV2(cfg, payload.RunID, payload.Scope, entry, envelope.Signer, wire); err != nil {
		t.Fatal("actual writer failed independent prior authentication", err)
	}
	retried, retryWire, err := prepareLocalEvidence(cfg, root, localPath, campaignEvidenceFileKind, payload.RunID, payload, owner, 1)
	if err != nil || retried == nil || retried.ContentHash != envelope.ContentHash || !bytes.Equal(retryWire, legacy) {
		t.Fatal("actual immutable retry changed legacy carrier bytes", err)
	}
	raw[len(raw)-3] ^= 1
	if changed, changedWire, err := prepareLocalEvidence(cfg, root, localPath, campaignEvidenceFileKind, payload.RunID, payload, owner, 1); err == nil || changed != nil || changedWire != nil {
		t.Fatal("actual writer reused raw-source authentication after mutation", err)
	}
	stored, err = os.ReadFile(localPath)
	if err != nil || !bytes.Equal(stored, append(bytes.Clone(legacy), '\n')) {
		t.Fatal("refused source mutation replaced retained wire", err)
	}
}
