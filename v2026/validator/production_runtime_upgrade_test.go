//go:build linux || darwin

// The production decision path, the public startup root and server staging
// cross a compatible runtime upgrade under the signed opt-in. Real provider
// replay, CRv4 preparation, signatures and intent custody run unchanged.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The successor adds unrelated storage, so its metadata hash differs while
// every consumed interface is unchanged. change may then alter a consumed one.
func productionUpgradeTestMetadata(t *testing.T, wire string, change func(*types.Metadata)) string {
	t.Helper()
	metadata, _, err := crv4.DecodeRuntimeMetadata(wire)
	if err != nil {
		t.Fatal(err)
	}
	for index := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[index]
		if pallet.Name != crv4.PalletName {
			continue
		}
		var extra types.StorageEntryMetadataV14
		for _, item := range pallet.Storage.Items {
			if item.Name == "Tempo" {
				extra = item
			}
		}
		extra.Name, extra.Documentation = "SuccessorOnlyStorage", nil
		pallet.Storage.Items = append(pallet.Storage.Items, extra)
	}
	if change != nil {
		change(metadata)
	}
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := crv4.DecodeRuntimeMetadata(encoded); err != nil {
		t.Fatal(err)
	}
	return encoded
}

type productionUpgradeTestCall = func(context.Context, any, string, ...any) error

// Every block other than the drained activation block 100 executes after the
// upgrade (set_code lands in block 101). Runtime identity stays bound to the
// exact queried block; every other read reaches the original transcript.
func productionUpgradeTestTranscript(activation string, spec uint32, code, metadata string, original productionUpgradeTestCall) productionUpgradeTestCall {
	return func(ctx context.Context, target any, method string, args ...any) error {
		upgraded := len(args) != 0 && fmt.Sprint(args[len(args)-1]) != activation
		assign := func(value any) error {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, target)
		}
		switch {
		case upgraded && method == "state_getRuntimeVersion" && len(args) == 1:
			return assign(map[string]any{"specName": "node-subtensor", "specVersion": spec, "transactionVersion": 1, "stateVersion": 1,
				"apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		case upgraded && method == "state_getStorageHash" && len(args) == 2 && args[0] == "0x3a636f6465":
			return assign(code)
		case upgraded && method == "state_getMetadata" && len(args) == 1:
			return assign(metadata)
		}
		return original(ctx, target, method, args...)
	}
}

func installProductionUpgradeTest(t *testing.T, fixture *ownerRecycleProductionTestFixture, spec uint32, code, metadata string) {
	t.Helper()
	client := fixture.operator.measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	client.callContext = productionUpgradeTestTranscript(fixture.block(100).Hex(), spec, code, metadata, client.callContext)
}

// Moves the genuine provider measurement to the next native epoch at block
// 101, exactly as an ongoing signed production window continues.
func advanceProductionUpgradeTestEpoch(t *testing.T, fixture *ownerRecycleProductionTestFixture) *ReleaseMeasurementArtifact {
	t.Helper()
	measurement := fixture.operator.measurement
	artifact := measurement.provider.artifact
	previous := ReleaseMeasurementContentHash(measurement.encoded)
	fixture.head, fixture.epoch = 101, artifact.SubnetEpoch+1
	artifact.SubnetEpoch, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash = fixture.epoch, 101, fixture.block(101).Hex()
	artifact.PreviousArtifactHash = previous
	for index := range artifact.Inputs {
		artifact.Inputs[index].CutNativeBlock, artifact.Inputs[index].CutNativeBlockHash = 101, fixture.block(101).Hex()
	}
	var err error
	measurement.encoded, _, err = SealReleaseMeasurementArtifactV2(t.Context(), artifact, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func newTreasuryUpgradeTestFixture(t *testing.T, optIn bool) *ownerRecycleProductionTestFixture {
	t.Helper()
	fixture := newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
		configureTreasuryProductionTest(t, fixture)
		if optIn {
			fixture.cfg.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
		}
	})
	// Production installs this at its native dial, before any shared read.
	if err := enableReleaseRuntimeSuccession(fixture.operator.measurement.admission.chain, fixture.cfg); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// The treasury validator decides under its approved runtime, the chain then
// upgrades compatibly, and the next epoch's decision is measured, prepared and
// signed under the successor. A freshly installed connection still verifies
// the first decision under its original runtime and the second under its own.
func TestTreasuryProductionDecidesAcrossCompatibleRuntimeUpgrade(t *testing.T) {
	fixture := newTreasuryUpgradeTestFixture(t, true)
	measurement := fixture.operator.measurement
	native := measurement.admission.chain
	approved := fixture.cfg.RuntimeSpec
	first, firstProvider := fixture.stage(t)
	firstIntent := fixture.intent(t, first, firstProvider)
	firstArtifact, err := decodeReleaseMeasurementV2Bytes(t.Context(), bytes.Clone(measurement.encoded), fixture.cfg.EvidenceV2.Bounds.MaxArtifactBytes, fixture.cfg.EvidenceV2.Bounds.MaxOperators)
	if err != nil {
		t.Fatal(err)
	}
	if firstIntent.Treasury == nil || firstIntent.Prepared.SourceCommitment.RuntimeSpec != approved {
		t.Fatal("first treasury decision did not use the approved runtime")
	}
	installProductionUpgradeTest(t, fixture, approved+1, releaseHex32([32]byte{0x47, 0x01}), productionUpgradeTestMetadata(t, measurement.admission.metadata, nil))
	artifact := advanceProductionUpgradeTestEpoch(t, fixture)
	next, nextProvider := fixture.stage(t)
	nextIntent := fixture.intent(t, next, nextProvider)
	if nextIntent.Treasury == nil || nextIntent.SubnetEpoch != firstIntent.SubnetEpoch+1 || nextIntent.Prepared.SourceCommitment.RuntimeSpec != approved+1 || uint32(native.Runtime.SpecVersion) != approved+1 {
		t.Fatalf("successor decision did not prepare and sign under the actual upgraded runtime: %d", nextIntent.Prepared.SourceCommitment.RuntimeSpec)
	}
	if next.proof.Eligibility.ActivationHash != first.proof.Eligibility.ActivationHash {
		t.Fatal("successor decision lost the original drained activation")
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, next, nextIntent, measurement.encoded, artifact, nextProvider); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseNativeSigningRuntime(native, fixture.cfg); err != nil {
		t.Fatalf("successor producer view failed the signing boundary: %v", err)
	}
	cold := &crv4.Chain{API: native.API, GenesisHash: native.GenesisHash}
	if err := enableReleaseRuntimeSuccession(cold, fixture.cfg); err != nil {
		t.Fatal(err)
	}
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), cold, fixture.cfg, firstIntent, firstArtifact); err != nil {
		t.Fatalf("decision under the approved runtime no longer verifies after the upgrade: %v", err)
	}
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), cold, fixture.cfg, nextIntent, artifact); err != nil {
		t.Fatalf("decision under the admitted successor does not verify after restart: %v", err)
	}
	exact := &crv4.Chain{API: native.API, GenesisHash: native.GenesisHash}
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), exact, fixture.cfg, nextIntent, artifact); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("a connection without installed admission verified a successor decision: %v", err)
	}
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), exact, fixture.cfg, firstIntent, firstArtifact); err != nil {
		t.Fatalf("exact historical verification depends on successor admission: %v", err)
	}
}

// A changed consumed interface outside the crv4 producer profile, or a signed
// config without the opt-in, halts the next decision with a precise refusal.
func TestTreasuryProductionHaltsOnIncompatibleOrUnapprovedUpgrade(t *testing.T) {
	for _, fault := range []string{"owner-storage", "no-opt-in"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newTreasuryUpgradeTestFixture(t, fault != "no-opt-in")
			measurement := fixture.operator.measurement
			native := measurement.admission.chain
			first, firstProvider := fixture.stage(t)
			fixture.intent(t, first, firstProvider)
			var change func(*types.Metadata)
			want := "unreviewed identity"
			if fault == "owner-storage" {
				want = "not an admitted successor of approved node-subtensor/470/1/1: owner-recycle storage OwnedHotkeys"
				change = func(metadata *types.Metadata) {
					for index := range metadata.AsMetadataV14.Pallets {
						pallet := &metadata.AsMetadataV14.Pallets[index]
						for itemIndex := range pallet.Storage.Items {
							if item := &pallet.Storage.Items[itemIndex]; pallet.Name == crv4.PalletName && item.Name == "OwnedHotkeys" {
								item.Type.AsMap.Hashers = []types.StorageHasherV10{{IsTwox64Concat: true}}
							}
						}
					}
				}
			}
			installProductionUpgradeTest(t, fixture, fixture.cfg.RuntimeSpec+1, releaseHex32([32]byte{0x47, 0x02}), productionUpgradeTestMetadata(t, measurement.admission.metadata, change))
			artifact := advanceProductionUpgradeTestEpoch(t, fixture)
			_, provider, err := DecodeReleaseMeasurementArtifactV2(t.Context(), measurement.encoded, fixture.providerOptions(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepareOwnerRecycleProductionDecision(t.Context(), fixture.cfg, native, fixture.operator.chain, measurement.encoded, artifact, provider.Decision, fixture.providerOptions(t)); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s upgrade reached a production decision: %v", fault, err)
			}
			if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, fixture.cfg, fixture.block(101)); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s upgrade reached the signing view: %v", fault, err)
			}
		})
	}
}

// The public root installs successor admission at its native dial. The approved
// runtime executes the original pending bytes in block 101, whose post-state
// and every later block run a compatible successor. Startup still reconciles
// that receipt and the applied row and reaches current preparation, without
// another send or a latest-metadata read.
func TestProductionStartupRunReleaseReconcilesAcrossCompatibleUpgrade(t *testing.T) {
	fixture := newProductionStartupTestFixtureWithConfig(t, false, func(cfg *ReleaseConfig) {
		cfg.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
	})
	continuation := fixture.continuation
	production := continuation.production
	pending := continuation.beginAndLoseAcknowledgement(t, fixture.native)
	successor := productionUpgradeTestMetadata(t, production.operator.measurement.admission.metadata, nil)
	fixture.native.call = productionUpgradeTestTranscript(production.block(100).Hex(), continuation.steerer.cfg.RuntimeSpec+1, releaseHex32([32]byte{0x47, 0x05}), successor, fixture.native.call)
	fixture.native.receiptNumber, fixture.native.applied = 101, true
	production.head = max(uint64(104), pending.Prepared.RevealBlock)
	production.extrinsicsKVs = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}
	production.epoch++
	production.operator.blocks[110] = [32]byte{0xa2}
	production.operator.finalized = 110
	fixture.closePreparation(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	var result error
	select {
	case result = <-done:
		t.Fatalf("RunRelease stopped across a compatible upgrade before current preparation: %v", result)
	case <-fixture.latestRead:
		cancel()
		result = <-done
		t.Fatalf("RunRelease queried latest metadata across the upgrade: %v", result)
	case <-fixture.evm.freshRead:
		cancel()
		result = <-done
	case <-t.Context().Done():
		cancel()
		result = <-done
		t.Fatalf("RunRelease did not reach current preparation across the upgrade: %v", result)
	}
	if result != nil && !errors.Is(result, context.Canceled) {
		t.Fatalf("cancellation across the upgrade invented a terminal failure: %v", result)
	}
	var stored steeringIntentFile
	if err := json.Unmarshal(fixture.storedIntentBytes(t), &stored); err != nil {
		t.Fatal(err)
	}
	applied := stored.Current
	if applied == nil || applied.Status != "applied" || applied.VectorHash != pending.VectorHash || applied.FinalizedBlock != 101 || applied.ApplicationBlock == 0 ||
		applied.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || len(fixture.native.broadcasts) != 1 {
		t.Fatalf("startup across the upgrade lost the original receipt/application or signed again: %+v sends=%d", applied, len(fixture.native.broadcasts))
	}
	fixture.stateLock.Lock()
	writeRequests := fixture.writeRequests
	fixture.stateLock.Unlock()
	if writeRequests != 0 {
		t.Fatal("startup across the upgrade sent a native request")
	}
}

// Server staging loads the same signed config. Its opt-in lets the refresh
// follow the validator across a compatible upgrade; without it the exact
// refusal and cache invalidation remain.
func TestValidatorUploadProductionRuntimeFollowsSignedSuccessorOptIn(t *testing.T) {
	for _, optIn := range []bool{true, false} {
		t.Run(fmt.Sprintf("opt-in=%t", optIn), func(t *testing.T) {
			fixture := newValidatorUploadProductionTestFixture(t)
			production := fixture.production
			if optIn {
				production.cfg.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
				production.resignAndLoad(t)
				raw, err := os.ReadFile(production.path)
				if err != nil {
					t.Fatal(err)
				}
				fixture.config.ProductionRuntimeConfig = mainnetRuntimeTestWriteBytes(t, filepath.Join(identityTestStateDir(t), "successor-production.yml"), raw)
			}
			successor := productionUpgradeTestMetadata(t, production.rpc.metadata, nil)
			inner := production.rpc.client.callContext
			production.rpc.client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
				if (method == "state_getRuntimeVersion" || method == "state_getStorageHash" || method == "state_getMetadata") && len(args) != 0 {
					hash, err := types.NewHashFromHexString(fmt.Sprint(args[len(args)-1]))
					if err != nil {
						return err
					}
					if number, err := mainnetRuntimeTestNumber(hash); err == nil && number >= productionSuccessorTestUpgradeBlock {
						switch method {
						case "state_getRuntimeVersion":
							return setValidatorRuntimeIdentityTestResult(result, map[string]any{"specName": "node-subtensor", "specVersion": production.cfg.RuntimeSpec + 1,
								"transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
						case "state_getStorageHash":
							return setValidatorRuntimeIdentityTestResult(result, releaseHex32([32]byte{0x47, 0x04}))
						default:
							return setValidatorRuntimeIdentityTestResult(result, successor)
						}
					}
				}
				return inner(ctx, result, method, args...)
			}
			owner := fixture.owner(t)
			if err := owner.refresh(t.Context(), fixture.upload.now); err != nil || len(owner.entries) != 1 {
				t.Fatalf("approved runtime staging refresh: %v", err)
			}
			if production.rpc.native.RuntimeSuccessionEnabled() != optIn {
				t.Fatal("staging successor admission does not follow the signed opt-in")
			}
			production.rpc.head = 170
			err := owner.refresh(t.Context(), fixture.upload.now)
			if optIn {
				if err != nil || len(owner.entries) != 1 {
					t.Fatalf("compatible upgrade stopped validator staging: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
				t.Fatalf("staging admitted an upgrade without the signed opt-in: %v", err)
			}
		})
	}
}
