// Signed resource revisions exercise the real configuration/history loader.
// Synthetic forecasts do not grant a new economic window or signing allowance.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"gopkg.in/yaml.v3"
)

// Increase only the explicit aggregate dimensions. Worst-case page counts
// stay independently finite even when a row closes a chunk by itself.
func productionCapacityTestBounds(original ReleaseEvidenceV2Bounds) ReleaseEvidenceV2Bounds {
	result := original
	var grow func(string, reflect.Value)
	grow = func(prefix string, value reflect.Value) {
		if value.Kind() == reflect.Struct {
			for index := 0; index < value.NumField(); index++ {
				name := value.Type().Field(index).Name
				if prefix != "" {
					name = prefix + "." + name
				}
				grow(name, value.Field(index))
			}
		} else if productionCapacityAggregate(prefix) {
			value.SetUint(value.Uint() * 4)
		}
	}
	grow("", reflect.ValueOf(&result).Elem())
	result.MaxCaptureFiles = original.CaptureFileLimit() * 4
	result.MaxHistoryBytes = max(result.MaxHistoryBytes, 32*1024*1024)
	result.Cut.Records.MaxPages = result.Cut.Records.MaxChunks
	result.Cut.Proofs.MaxPages = result.Cut.Proofs.MaxChunks
	return result
}

// This helper supplies reviewed synthetic counters, then uses the same actual
// independent envelope and public loader as deployed configuration. Public
// physical planning is exercised separately through the command entry point.
func productionCapacityTestDraft(t *testing.T, original *ReleaseConfig, approved OwnerRecycleApproval) (*ReleaseConfig, OwnerRecycleApproval) {
	t.Helper()
	bundle, err := BuildOwnerRecycleProductionAuthority(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	root := identityTestStateDir(t)
	reference, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(root, "original-authority.json"), bundle, maximumProductionAuthorityBundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var cfg ReleaseConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(approved)
	if err != nil {
		t.Fatal(err)
	}
	var next OwnerRecycleApproval
	if err := json.Unmarshal(raw, &next); err != nil {
		t.Fatal(err)
	}
	cfg.OwnerRecycleApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(root, "successor-approval.json")}
	cfg.ProductionAuthorityHistory = append(cfg.ProductionAuthorityHistory, reference)
	cfg.EvidenceV2.Bounds = productionCapacityTestBounds(original.EvidenceV2.Bounds)
	revision := &ProductionCapacityRevision{Schema: ProductionCapacityRevisionSchema, PredecessorConfigHash: attemptHex32(approved.ConfigHash),
		PredecessorApprovalSha256: original.OwnerRecycleApproval.Approval.SHA256, EconomicApprovalSha256: productionCapacityEconomicHash(approved),
		ValidThroughNativeBlock: approved.ValidThroughNativeBlock, ValidThroughNativeEpoch: approved.Production.ValidThroughNativeEpoch,
		Margin: 2, RetainedHistoryBytes: 1024, RetainedCaptureFiles: 8}
	for index, operator := range original.Operators {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0xb1 + index)}, ed25519.SeedSize))
		identity := AttemptLedgerIdentity{DeploymentID: original.DeploymentID, ChainID: original.ChainID, GenesisHash: original.GenesisHash,
			Netuid: original.Netuid, ValidatorID: original.ValidatorID, ValidatorUID: uint16(index), NoID: operator.NoID,
			ValidatorVPK: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))}
		source := ProductionCapacitySource{Identity: identity, Head: AttemptLedgerHead{Root: zeroAttemptHash()}, CensusSha256: attemptHex32(sha256.Sum256([]byte("synthetic-capacity-census"))),
			FutureRecords: 4, FutureTrails: 1, FutureRecordBytes: 4 * original.EvidenceV2.Bounds.Disk.MaxRecordBytes, FutureProofBytes: original.EvidenceV2.Bounds.Replay.MaxProofBytes,
			StorageBytes: 1024, StorageFiles: 8, FutureStorageFiles: 8}
		source.FutureStorageBytes = source.FutureRecordBytes + source.FutureProofBytes
		revision.FutureHistoryBytes += source.FutureStorageBytes
		revision.FutureCaptureFiles += source.FutureStorageFiles
		revision.Sources = append(revision.Sources, source)
	}
	cfg.ProductionCapacityRevision = revision
	return &cfg, next
}

// Signing is fixture-only. Loading remains the production function so a
// missing continuity check cannot be hidden behind a test-created capsule.
func productionCapacityTestSign(t *testing.T, cfg *ReleaseConfig, approval OwnerRecycleApproval, private ed25519.PrivateKey) *productionRuntimeTestFixture {
	t.Helper()
	fixture := &productionRuntimeTestFixture{cfg: cfg, approval: approval, private: private}
	fixture.signConfig(t)
	return fixture
}

func TestProductionCapacityLoaderPreservesOriginalAuthority(t *testing.T) {
	original := newProductionRuntimeTestFixture(t, false)
	originalBytes := bytes.Clone(original.cfg.ownerRecycleProduction.encoded)
	cfg, approval := productionCapacityTestDraft(t, original.cfg, original.approval)
	signed := productionCapacityTestSign(t, cfg, approval, original.private)
	loaded, err := LoadReleaseConfig(signed.path)
	if err != nil {
		t.Fatal("independently signed resource-only successor was refused", err)
	}
	if len(loaded.productionAuthorityHistory.entries) != 1 || !bytes.Equal(loaded.productionAuthorityHistory.entries[0].config.ownerRecycleProduction.encoded, originalBytes) ||
		productionCapacityEconomicHash(signed.approval) != productionCapacityEconomicHash(original.approval) ||
		loaded.EvidenceV2.Bounds.Disk.MaxRecordCount <= original.cfg.EvidenceV2.Bounds.Disk.MaxRecordCount ||
		loaded.EvidenceV2.Bounds.MaxProviders != original.cfg.EvidenceV2.Bounds.MaxProviders {
		t.Fatal("capacity successor changed original approval, economic authority or active population")
	}
	if _, err := os.Stat(loaded.HotkeySeedFile); !os.IsNotExist(err) {
		t.Fatal("config loading opened or provisioned a signing key", err)
	}
	if err := ownerRecycleProductionBoundary(loaded.productionAuthorityHistory.entries[0].config); err == nil {
		t.Fatal("retained original config acquired a fresh writer")
	}
}

func TestProductionCapacityRejectsIncompleteOrUnderbudgetedCensus(t *testing.T) {
	original := newProductionRuntimeTestFixture(t, false)
	for _, fault := range []string{"omitted", "duplicate", "foreign", "prefix", "margin", "overflow", "record-bytes", "future-history", "future-files", "disk", "geometry", "predecessor", "no-history"} {
		cfg, approval := productionCapacityTestDraft(t, original.cfg, original.approval)
		revision := cfg.ProductionCapacityRevision
		switch fault {
		case "omitted":
			revision.Sources = revision.Sources[:1]
		case "duplicate":
			revision.Sources[1] = revision.Sources[0]
		case "foreign":
			revision.Sources[0].Identity.GenesisHash = attemptHex32([32]byte{0x81})
		case "prefix":
			revision.Sources[0].Head.Root = attemptHex32([32]byte{0x82})
		case "margin":
			revision.Margin = 1
		case "overflow":
			revision.Sources[0].FutureRecords = ^uint64(0)
		case "record-bytes":
			revision.Sources[0].FutureRecordBytes--
		case "future-history":
			revision.FutureHistoryBytes--
		case "future-files":
			revision.FutureCaptureFiles--
		case "disk":
			revision.Sources[0].StorageBytes = cfg.EvidenceV2.Bounds.Disk.MaxStorageBytes
		case "geometry":
			cfg.EvidenceV2.Bounds.Cut.Records.MaxPages = original.cfg.EvidenceV2.Bounds.Cut.Records.MaxPages
		case "predecessor":
			revision.PredecessorConfigHash = attemptHex32([32]byte{0x83})
		case "no-history":
			cfg.ProductionAuthorityHistory = nil
		}
		signed := productionCapacityTestSign(t, cfg, approval, original.private)
		if loaded, err := LoadReleaseConfig(signed.path); err == nil || loaded != nil {
			t.Fatalf("signed %s capacity request bypassed complete original-prefix accounting: %v", fault, err)
		}
	}
}

func TestProductionCapacityRejectsEconomicAndResidentChanges(t *testing.T) {
	original := newProductionRuntimeTestFixture(t, false)
	for _, fault := range []string{"window", "activation", "hotkeys", "row", "provider", "resident", "policy", "reduce"} {
		cfg, approval := productionCapacityTestDraft(t, original.cfg, original.approval)
		switch fault {
		case "window":
			approval.ValidThroughNativeBlock++
		case "activation":
			approval.Production.ActivationNativeHash[0] ^= 1
		case "hotkeys":
			approval.MaximumOwnedHotkeys++
		case "row":
			cfg.EvidenceV2.Bounds.Disk.MaxRecordBytes++
		case "provider":
			cfg.EvidenceV2.Bounds.MaxProviders++
		case "resident":
			cfg.EvidenceV2.Bounds.Cut.Records.MaxPageBytes++
		case "policy":
			cfg.Policy.PolicyID++
			cfg.PolicyHash, _ = cfg.Policy.HashHex()
		case "reduce":
			cfg.EvidenceV2.Bounds.Disk.MaxRecordCount = original.cfg.EvidenceV2.Bounds.Disk.MaxRecordCount - 1
		}
		signed := productionCapacityTestSign(t, cfg, approval, original.private)
		if loaded, err := LoadReleaseConfig(signed.path); err == nil || loaded != nil {
			t.Fatalf("resource revision admitted %s authority or fixed-bound change: %v", fault, err)
		}
	}
}

func TestProductionCapacityHistoryRequiresEveryImmediatePredecessor(t *testing.T) {
	original := newProductionRuntimeTestFixture(t, false)
	cfg, approval := productionCapacityTestDraft(t, original.cfg, original.approval)
	first := productionCapacityTestSign(t, cfg, approval, original.private)
	loaded, err := LoadReleaseConfig(first.path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, approval = productionCapacityTestDraft(t, loaded, first.approval)
	second := productionCapacityTestSign(t, cfg, approval, original.private)
	current, err := LoadReleaseConfig(second.path)
	if err != nil || len(current.productionAuthorityHistory.entries) != 2 {
		t.Fatal("second revision did not retain both authenticated predecessor links", err)
	}
	current.ProductionCapacityRevision.PredecessorConfigHash = attemptHex32(original.approval.ConfigHash)
	second = productionCapacityTestSign(t, current, second.approval, original.private)
	if _, err := LoadReleaseConfig(second.path); err == nil {
		t.Fatal("capacity successor skipped its immediate signed predecessor")
	}
}

// The existing signed measurement/sidecar reader selects its original config
// after a resource-only revision. It neither rewrites proof bytes nor creates
// another fresh authorization under that historical config.
func TestProductionCapacityRetainsOriginalSignedSidecar(t *testing.T) {
	// The measurement-only fixture deliberately omits deployable operator and
	// evidence paths. Complete them before the first production approval, then
	// require the public loader to admit the original as well as its successor.
	fixture := newOwnerRecycleProductionTestFixtureWithInputs(t, func(t *testing.T, hotkey [32]byte) *recycleOperatorFixture {
		return newRecycleOperatorFixtureWithInputs(t, hotkey, 2, nil, func(admission *recycleAdmissionFixture, provider *releaseMeasurementV2TestFixture) {
			template := validReleaseConfig(t)
			cfg := admission.cfg
			for index := range cfg.Operators {
				noId := cfg.Operators[index].NoID
				cfg.Operators[index] = template.Operators[index]
				cfg.Operators[index].NoID = noId
			}
			cfg.TrailDepth, cfg.PollSeconds = cfg.Policy.Verify.TrailDepth, 2
			cfg.EvidenceV2 = releaseEvidenceV2TestConfig(filepath.Dir(template.StateDir), cfg.Operators)
			if err := cfg.normalize(filepath.Dir(cfg.StateDir)); err != nil {
				t.Fatal("original operational paths were not normalized before signing", err)
			}
		})
	}, func(fixture *ownerRecycleProductionTestFixture) {
		// The measurement constructor intentionally narrows only two metadata
		// fields after the operator hook. Use the entire already loadable
		// release profile here, after all those overrides; never combine its
		// 1 MiB artifact cap with another profile's 4 MiB closure allowance.
		template := validReleaseConfig(t)
		validated, err := LoadReleaseConfig(writeReleaseConfig(t, template))
		if err != nil {
			t.Fatal("complete operational capacity template failed admission", err)
		}
		fixture.cfg.EvidenceV2.Bounds = validated.EvidenceV2.Bounds
		fixture.cfg.EvidenceV2.Bounds.MaxOperators = uint64(len(fixture.cfg.Operators))
		if err := errors.Join(fixture.cfg.Policy.Validate(),
			fixture.cfg.EvidenceV2.Bounds.Validate(uint64(len(fixture.cfg.Operators))),
			fixture.cfg.EvidenceV2.Validate(fixture.cfg.Operators, fixture.cfg.StateDir, fixture.cfg.HotkeySeedFile)); err != nil {
			t.Fatal("complete original evidence profile failed pre-sign admission", err)
		}
		// The independent fixture approver must sign the same document the
		// public loader sees, including YAML's concrete empty slice values.
		// This happens before the original production approval and sidecar.
		raw, err := yaml.Marshal(fixture.cfg)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := decodeReleaseConfigDocument(filepath.Join(fixture.cfg.StateDir, "original-config.yml"), raw)
		if err != nil {
			t.Fatal("original config did not satisfy strict document decoding before approval", err)
		}
		resolved.Coordinator = strings.ToLower(resolved.Coordinator)
		resolved.SettlementVault = strings.ToLower(resolved.SettlementVault)
		*fixture.cfg = *resolved
		raw, err = yaml.Marshal(fixture.cfg)
		if err != nil {
			t.Fatal(err)
		}
		reloaded, err := decodeReleaseConfigDocument(filepath.Join(fixture.cfg.StateDir, "original-config.yml"), raw)
		if err != nil {
			t.Fatal(err)
		}
		original, originalErr := OwnerRecycleConfigHash(fixture.cfg)
		second, secondErr := OwnerRecycleConfigHash(reloaded)
		if originalErr != nil || secondErr != nil || original != second {
			t.Fatal("original signing document was not wire-idempotent", originalErr, secondErr)
		}
	})
	originalConfigPath := writeReleaseConfig(t, *fixture.cfg)
	loadedOriginal, err := LoadReleaseConfig(originalConfigPath)
	if err != nil {
		t.Fatal("complete original sidecar config failed the public loader", err)
	}
	originalHash, err := OwnerRecycleConfigHash(loadedOriginal)
	if err != nil || originalHash != fixture.operator.measurement.admission.approval.ConfigHash {
		t.Fatal("public normalization changed originally approved configuration", err)
	}
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	cfg, approval := productionCapacityTestDraft(t, fixture.cfg, measurement.admission.approval)
	signed := productionCapacityTestSign(t, cfg, approval, measurement.admission.private)
	current, err := LoadReleaseConfig(signed.path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := productionConfigForIntent(current, intent)
	if err != nil {
		t.Fatal(err)
	}
	native := &crv4.Chain{API: measurement.admission.chain.API, GenesisHash: measurement.admission.chain.GenesisHash}
	replayed, err := prepareOwnerRecycleProductionDecision(t.Context(), original, native, fixture.operator.chain,
		measurement.encoded, measurement.provider.artifact, provider, measurement.provider.options(t))
	if err != nil || !bytes.Equal(replayed.encoded, stage.encoded) {
		t.Fatal("resource revision changed original retained source proof", err)
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), original, replayed, intent, measurement.encoded, measurement.provider.artifact, provider); err != nil {
		t.Fatal("original signed sidecar cannot continue under exact historical authority", err)
	}
	if ownerRecycleProductionBoundary(original) == nil {
		t.Fatal("capacity history granted its old producer a current writer")
	}
	if _, err := sealOwnerRecycleProductionIntent(t.Context(), replayed, fixture.hotkey, intent.Prepared, intent.MeasurementEnvelopeHash); err == nil {
		t.Fatal("historical capacity prefix signed another sidecar")
	}
}

// Existing configs omit the optional field completely, including their config
// hash input. An explicit null or permissive numeric YAML spelling is refused.
func TestProductionCapacityLegacyAndStrictOptionalGrammar(t *testing.T) {
	original := newProductionRuntimeTestFixture(t, false)
	raw, err := json.Marshal(original.cfg)
	if err != nil || bytes.Contains(raw, []byte("production_capacity_revision")) {
		t.Fatal("legacy signed config acquired a new serialized field", err)
	}
	cfg, approval := productionCapacityTestDraft(t, original.cfg, original.approval)
	signed := productionCapacityTestSign(t, cfg, approval, original.private)
	encoded, err := os.ReadFile(signed.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"null", "quoted", "leading", "negative", "uid-overflow", "unknown"} {
		candidate := bytes.Clone(encoded)
		switch fault {
		case "null":
			legacy, _ := yaml.Marshal(original.cfg)
			candidate = append(legacy, []byte("production_capacity_revision: null\n")...)
		case "quoted":
			candidate = bytes.Replace(candidate, []byte("margin: 2"), []byte("margin: \"2\""), 1)
		case "leading":
			candidate = bytes.Replace(candidate, []byte("margin: 2"), []byte("margin: 02"), 1)
		case "negative":
			candidate = bytes.Replace(candidate, []byte("future_records: 4"), []byte("future_records: -1"), 1)
		case "uid-overflow":
			candidate = bytes.Replace(candidate, []byte("validatoruid: 0"), []byte("validatoruid: 65536"), 1)
		case "unknown":
			candidate = bytes.Replace(candidate, []byte("margin: 2"), []byte("margin: 2\n    active_funded_miners: 2048"), 1)
		}
		if bytes.Equal(candidate, encoded) {
			t.Fatal("numeric fault did not alter actual YAML", fault)
		}
		path := filepath.Join(identityTestStateDir(t), "invalid.yml")
		if err := os.WriteFile(path, candidate, 0600); err != nil {
			t.Fatal(err)
		}
		if loaded, err := LoadReleaseConfig(path); err == nil || loaded != nil || !strings.Contains(err.Error(), "YAML") && !strings.Contains(err.Error(), "decimal") && !strings.Contains(err.Error(), "uint64") {
			t.Fatalf("%s did not fail strict grammar before signature admission: %v", fault, err)
		}
	}
}
