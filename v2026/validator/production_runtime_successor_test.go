// Signed production configs opt in to successor admission explicitly. The real
// mainnet 473 and 475 metadata cross the actual production runtime gate; no
// approval verdict, key or network is supplied by these tests.
package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Public mainnet artifacts, identified in crv4/testdata/runtime-successor-metadata.md.
var (
	productionSuccessorTest473 = crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 473, TransactionVersion: 1, StateVersion: 1},
		CodeHash: "0x7773f5c0a6d6e9ea9ff347edcc491246eec08a5cf441d964ee96f40d7fa65a08", MetadataHash: "0xa97219740ed3b034a06463c783cd5b794788652e0692c1edb03eed34c8968172"}
	productionSuccessorTest475 = crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 475, TransactionVersion: 1, StateVersion: 1},
		CodeHash: "0x557634c8c31bc639ea6552e297dcfb781cd7d9bdd491a5352c151305db33d3a0", MetadataHash: "0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff"}
)

// The upgrade boundary: blocks 101 through 159 run the approved 473 artifact,
// later blocks run 475, all inside the signed 101..200 production window.
const productionSuccessorTestUpgradeBlock = 160

type productionSuccessorTestFixture struct {
	*productionRuntimeTestFixture
	successorVersion map[string]any
	successorCode    string
	successorWire    string
}

// The approval signs the real 473 artifact; optIn adds the config field before
// that independent signature, exactly as an operator's config would carry it.
func newProductionSuccessorTestFixture(t *testing.T, optIn bool) *productionSuccessorTestFixture {
	t.Helper()
	fixture := newProductionRuntimeTestFixture(t, false)
	_, wire473 := provisionalValidatorMetadataTest(t, "../crv4/testdata/runtime473-metadata.scale.gz.base64", productionSuccessorTest473.MetadataHash)
	_, wire475 := provisionalValidatorMetadataTest(t, "../crv4/testdata/runtime475-metadata.scale.gz.base64", productionSuccessorTest475.MetadataHash)
	rpc := fixture.rpc
	rpc.metadata, rpc.versions[1], rpc.codes[1] = wire473, productionSuccessorTest473.Version, productionSuccessorTest473.CodeHash
	cfg := fixture.cfg
	cfg.RuntimeSpec, cfg.RuntimeCodeHash, cfg.RuntimeMetadataHash = 473, productionSuccessorTest473.CodeHash, productionSuccessorTest473.MetadataHash
	fixture.approval.Proposal.Runtime.Version = productionSuccessorTest473.Version
	fixture.approval.Proposal.Runtime.CodeHash, _ = parseHash32("approved code", productionSuccessorTest473.CodeHash)
	fixture.approval.Proposal.Runtime.MetadataHash, _ = parseHash32("approved metadata", productionSuccessorTest473.MetadataHash)
	if optIn {
		cfg.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
	}
	fixture.resignAndLoad(t)
	self := &productionSuccessorTestFixture{productionRuntimeTestFixture: fixture, successorCode: productionSuccessorTest475.CodeHash, successorWire: wire475,
		successorVersion: map[string]any{"specName": "node-subtensor", "specVersion": 475, "transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}}}
	inner := rpc.client.callContext
	rpc.client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if (method == "state_getRuntimeVersion" || method == "state_getStorageHash" || method == "state_getMetadata") && len(args) != 0 {
			hash, err := types.NewHashFromHexString(fmt.Sprint(args[len(args)-1]))
			if err != nil {
				return err
			}
			if number, err := mainnetRuntimeTestNumber(hash); err == nil && number >= productionSuccessorTestUpgradeBlock {
				switch method {
				case "state_getRuntimeVersion":
					return setValidatorRuntimeIdentityTestResult(result, self.successorVersion)
				case "state_getStorageHash":
					return setValidatorRuntimeIdentityTestResult(result, self.successorCode)
				default:
					return setValidatorRuntimeIdentityTestResult(result, self.successorWire)
				}
			}
		}
		return inner(ctx, result, method, args...)
	}
	rpc.head = 180
	return self
}

// Re-encodes a deliberately changed copy of the real 475 metadata.
func productionSuccessorChangedMetadata(t *testing.T, change func(*types.Metadata)) string {
	t.Helper()
	metadata, _ := provisionalValidatorMetadataTest(t, "../crv4/testdata/runtime475-metadata.scale.gz.base64", productionSuccessorTest475.MetadataHash)
	change(metadata)
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// The opt-in names only the producer interface profile, only on a signed
// schema-3 mainnet production config, and the approval's config hash covers
// it in both directions. The policy keeps stop_on_runtime_change mandatory.
func TestProductionRuntimeSuccessorOptInIsSignedMainnetConfig(t *testing.T) {
	fixture := newProductionSuccessorTestFixture(t, true)
	if fixture.cfg.RuntimeSuccessorProfile != crv4.ValidatorProducerRuntimeProfile {
		t.Fatal("signed opt-in was lost by the producer loader")
	}
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, *fixture.cfg)); err != nil {
		t.Fatalf("unchanged signed opt-in did not reload: %v", err)
	}
	withdrawn := *fixture.cfg
	withdrawn.RuntimeSuccessorProfile = ""
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, withdrawn)); err == nil {
		t.Fatal("removing the signed opt-in kept the original approval")
	}
	exact := newProductionSuccessorTestFixture(t, false)
	added := *exact.cfg
	added.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, added)); err == nil {
		t.Fatal("an unsigned opt-in was admitted under an exact-pin approval")
	}
	for _, profile := range []string{crv4.ProvisionalRuntimeCompatibilityProfile, "urnetwork-validator-producer-interface-v2"} {
		wrong := newProductionSuccessorTestFixture(t, false)
		wrong.cfg.RuntimeSuccessorProfile = profile
		wrong.signConfig(t)
		if _, err := LoadReleaseConfig(wrong.path); err == nil || !strings.Contains(err.Error(), "runtime_successor_profile") {
			t.Fatalf("signed profile %q was admitted: %v", profile, err)
		}
	}
	legacy := validReleaseConfig(t)
	legacy.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
	if err := legacy.Validate(); err == nil || !strings.Contains(err.Error(), "runtime_successor_profile") {
		t.Fatalf("testnet/legacy config admitted mainnet successor admission: %v", err)
	}
	policy := fixture.cfg.Policy
	policy.Safety.StopOnRuntimeChange = false
	if policy.Validate() == nil {
		t.Fatal("policy stopped requiring stop_on_runtime_change")
	}
}

// A production validator crosses the real 473-to-475 upgrade: it keeps a fresh
// producer signing view on 475, historical reads keep their own artifacts, and
// old signed bytes cannot be replayed across the replacement.
func TestProductionRuntimeSuccessorCrossesRealMainnetUpgrade(t *testing.T) {
	fixture := newProductionSuccessorTestFixture(t, true)
	native, cfg := fixture.rpc.native, fixture.cfg
	approved := releaseNativeRuntimeIdentity(cfg)
	if approved != productionSuccessorTest473 {
		t.Fatalf("fixture approved %+v", approved)
	}
	if err := enableReleaseRuntimeSuccession(native, cfg); err != nil || !native.RuntimeSuccessionEnabled() || native.ProvisionalRuntimeCompatibilityEnabled() {
		t.Fatalf("signed opt-in did not install mainnet successor admission: %v", err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(150)); err != nil {
		t.Fatal(err)
	}
	if uint32(native.Runtime.SpecVersion) != 473 || validateReleaseNativeSigningRuntime(native, cfg) != nil {
		t.Fatal("approved exact artifact lost production signing")
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(170)); err != nil {
		t.Fatalf("compatible 475 successor halted production: %v", err)
	}
	if uint32(native.Runtime.SpecVersion) != 475 {
		t.Fatalf("signing view did not bind the actual successor: %d", native.Runtime.SpecVersion)
	}
	if err := validateReleaseNativeSigningRuntime(native, cfg); err != nil {
		t.Fatalf("successor producer view failed the signing boundary: %v", err)
	}
	if native.ValidateValidatorProducerRuntime(approved) == nil {
		t.Fatal("successor view satisfied the exact approved pin itself")
	}
	view := *native
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), &view, cfg, mainnetRuntimeTestBlock(150)); err != nil {
		t.Fatalf("historical decision under the approved runtime no longer verifies: %v", err)
	}
	if uint32(view.Runtime.SpecVersion) != 473 || validateReleaseNativeSigningRuntime(&view, cfg) == nil {
		t.Fatal("historical read lost its original artifact or gained signing authority")
	}
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), &view, cfg, mainnetRuntimeTestBlock(170)); err != nil || uint32(view.Runtime.SpecVersion) != 475 {
		t.Fatalf("historical decision under the admitted successor does not verify: %v", err)
	}
	if err := validatePreparedNativeRuntimeContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(150), mainnetRuntimeTestBlock(170)); err == nil {
		t.Fatal("bytes signed under 473 were admitted for replay under 475")
	}
	if err := validatePreparedNativeRuntimeContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(165), mainnetRuntimeTestBlock(170)); err != nil {
		t.Fatalf("bytes signed under the admitted successor lost replay: %v", err)
	}
	// The signed block window still bounds current authority.
	fixture.rpc.head = 205
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(205)); err == nil {
		t.Fatal("successor admission widened the signed production window")
	}
}

// Without the signed opt-in, or when a consumed interface changes, the upgrade
// halts with a precise error and leaves the previously bound view intact.
func TestProductionRuntimeSuccessorHaltsWithoutOptInOrOnChangedInterface(t *testing.T) {
	for _, fault := range []string{"no-opt-in", "owner-storage", "producer-storage", "transaction-version"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newProductionSuccessorTestFixture(t, fault != "no-opt-in")
			want := "not an admitted successor"
			switch fault {
			case "no-opt-in":
				want = "unreviewed identity"
			case "owner-storage", "producer-storage":
				name := "OwnedHotkeys"
				if fault == "producer-storage" {
					name = "SubnetEpochIndex"
				}
				fixture.successorWire = productionSuccessorChangedMetadata(t, func(metadata *types.Metadata) {
					for index := range metadata.AsMetadataV14.Pallets {
						pallet := &metadata.AsMetadataV14.Pallets[index]
						for itemIndex := range pallet.Storage.Items {
							if item := &pallet.Storage.Items[itemIndex]; pallet.Name == crv4.PalletName && string(item.Name) == name {
								item.Type.AsMap.Hashers = []types.StorageHasherV10{{IsTwox64Concat: true}}
							}
						}
					}
				})
				want += " of approved node-subtensor/473/1/1: " + map[string]string{"owner-storage": "owner-recycle storage OwnedHotkeys", "producer-storage": "validator producer consumed interface storage/SubtensorModule.SubnetEpochIndex changed"}[fault]
			case "transaction-version":
				fixture.successorVersion["transactionVersion"] = 2
				want = "not a successor of approved node-subtensor/473/1/1"
			}
			native, cfg := fixture.rpc.native, fixture.cfg
			if err := enableReleaseRuntimeSuccession(native, cfg); err != nil {
				t.Fatal(err)
			}
			if native.RuntimeSuccessionEnabled() != (fault != "no-opt-in") {
				t.Fatal("successor admission installation does not follow the signed opt-in")
			}
			if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(150)); err != nil {
				t.Fatal(err)
			}
			oldMeta, oldRuntime := native.Meta, native.Runtime
			err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(170))
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s upgrade was not halted with %q: %v", fault, want, err)
			}
			if native.Meta != oldMeta || native.Runtime != oldRuntime || validateReleaseNativeSigningRuntime(native, cfg) != nil {
				t.Fatal("refused upgrade changed the retained approved signing view")
			}
		})
	}
}

// An independently signed renewal over a changed runtime, as an operator would
// produce it. The load error is returned so continuity refusals are observable.
func productionSuccessorRenewal(t *testing.T, original *ReleaseConfig, approved OwnerRecycleApproval, private ed25519.PrivateKey, profile string) (*ReleaseConfig, error) {
	t.Helper()
	raw, err := BuildOwnerRecycleProductionAuthority(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(identityTestStateDir(t), "original-authority.json"), raw, maximumProductionAuthorityBundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	var cfg ReleaseConfig
	var approval OwnerRecycleApproval
	for _, pair := range []struct{ from, to any }{{original, &cfg}, {approved, &approval}} {
		raw, err := json.Marshal(pair.from)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, pair.to); err != nil {
			t.Fatal(err)
		}
	}
	cfg.ProductionAuthorityHistory = append(cfg.ProductionAuthorityHistory, reference)
	cfg.OwnerRecycleApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "successor-approval.json")}
	cfg.RuntimeSuccessorProfile = profile
	cfg.RuntimeSpec++
	cfg.RuntimeCodeHash = releaseHex32([32]byte{0x95, 0x01})
	approval.Production.ActivationNativeBlock = ownerRecycleActivationBlock(&approval)
	approval.ValidFromNativeBlock++
	approval.Proposal.Runtime.Version.SpecVersion = cfg.RuntimeSpec
	approval.Proposal.Runtime.CodeHash, _ = parseHash32("renewed code", cfg.RuntimeCodeHash)
	approval.RuntimeReviewHash = recycleTestId(4300)
	approval.ConfigHash, err = OwnerRecycleConfigHash(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	approver := recycleAdmissionFixture{cfg: &cfg, approval: approval, private: private}
	approver.sign(t)
	if err := loadOwnerRecycleProductionConfig(&cfg); err != nil {
		return nil, err
	}
	if err := loadReleaseProductionRuntimeHistory(&cfg); err != nil {
		return nil, err
	}
	if err := loadReleaseProductionAuthorityHistory(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Decisions an opted-in original made under a successor must keep verifying,
// so a renewal cannot withdraw the opt-in. A renewal may newly opt in; only
// opted-in configs contribute their approved artifact as an anchor.
func TestProductionRuntimeSuccessorRenewalKeepsOriginalOptIn(t *testing.T) {
	profile := crv4.ValidatorProducerRuntimeProfile
	for _, test := range []struct {
		name, original, renewed string
	}{{"kept", profile, profile}, {"withdrawn", profile, ""}, {"added", "", profile}, {"exact", "", ""}} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
				fixture.cfg.RuntimeSuccessorProfile = test.original
			})
			admission := fixture.operator.measurement.admission
			renewed, err := productionSuccessorRenewal(t, fixture.cfg, admission.approval, admission.private, test.renewed)
			if test.name == "withdrawn" {
				if err == nil || !strings.Contains(err.Error(), "cannot withdraw or change runtime successor admission") {
					t.Fatalf("renewal withdrew an original successor opt-in: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			anchors, err := releaseRuntimeSuccessionAnchors(renewed)
			if err != nil {
				t.Fatal(err)
			}
			var want []crv4.RuntimeArtifactIdentity
			if test.renewed != "" {
				want = append(want, releaseNativeRuntimeIdentity(renewed))
			}
			if test.original != "" {
				want = append(want, releaseNativeRuntimeIdentity(fixture.cfg))
			}
			if !slices.Equal(anchors, want) {
				t.Fatalf("renewal anchors %+v, want %+v", anchors, want)
			}
		})
	}
}
