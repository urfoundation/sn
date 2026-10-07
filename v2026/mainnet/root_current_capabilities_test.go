// Public, identity-free metadata exercises the current root call catalogue.
// These tests never construct a signer, chain route or custody approval.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// The retained public projection keeps the exact call type graph and indices.
func rootCurrentTestMetadata(t *testing.T) ([]byte, *types.Metadata, string, string) {
	t.Helper()
	metadataHex, metadata := ownerRecycleTestMetadata(t)
	raw, err := hex.DecodeString(strings.TrimPrefix(metadataHex, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return raw, metadata, rootExtrinsicHash(raw), "sha256:" + hex.EncodeToString(digest[:])
}

// Mutation cases own a fresh decoded graph and edit only its call registry.
func rootCurrentTestVariants(t *testing.T, metadata *types.Metadata) *[]types.Si1Variant {
	t.Helper()
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name == "SubtensorModule" {
			entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
			if entry != nil && entry.Def.IsVariant {
				return &entry.Def.Variant.Variants
			}
		}
	}
	t.Fatal("public projection lacks SubtensorModule calls")
	return nil
}

// A missing fixture call is a setup failure, never a successful refusal test.
func rootCurrentTestVariant(t *testing.T, metadata *types.Metadata, name string) *types.Si1Variant {
	t.Helper()
	variants := rootCurrentTestVariants(t, metadata)
	for index := range *variants {
		if string((*variants)[index].Name) == name {
			return &(*variants)[index]
		}
	}
	t.Fatalf("public projection lacks %s", name)
	return nil
}

// The report must name each interface even when a call is absent or changed.
func rootCurrentTestCall(t *testing.T, report rootCurrentCapabilities, name string) rootCurrentCallCapability {
	t.Helper()
	for _, call := range report.Calls {
		if call.Call == name {
			return call
		}
	}
	t.Fatalf("report lacks %s", name)
	return rootCurrentCallCapability{}
}

// A current public type graph supports coldkey operations and no retired call.
func TestRootCurrentCapabilitiesMetadataCatalog(t *testing.T) {
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
	if err != nil {
		t.Fatal(err)
	}
	expectedIndexKVs := map[string]uint8{"root_register": 62, "add_stake": 2, "remove_stake": 3, "claim_root": 121, "claim_root_with_hotkey": 148, "stake_into_basket": 147, "swap_basket": 150, "swap_basket_many": 151, "set_auto_parent_delegation_enabled": 135, "decrease_take": 65, "increase_take": 66, "set_children": 67}
	if len(report.Calls) != len(expectedIndexKVs) || !report.MetadataInterfaceCompatible || report.Status != "metadata-compatible-current-state-unknown" {
		t.Fatalf("current call catalogue unavailable: %+v", report)
	}
	for name, index := range expectedIndexKVs {
		call := rootCurrentTestCall(t, report, name)
		if call.PalletIndex != 7 || call.ExpectedCallIndex != index || call.ObservedCallIndex == nil || *call.ObservedCallIndex != index || call.MetadataStatus != "shape-supported" || call.MutationAdapter != "not-implemented-for-current-root" {
			t.Fatalf("incorrect %s interface: %+v", name, call)
		}
	}
	if rootCurrentTestCall(t, report, "root_register").ReviewedSignedOrigin != "root-owning-coldkey" || rootCurrentTestCall(t, report, "claim_root_with_hotkey").ReviewedSignedOrigin != "staker-coldkey-or-explicit-root-claim-proxy" || rootCurrentTestCall(t, report, "swap_basket").ReviewedSignedOrigin != "root-owning-coldkey-or-explicit-basket-trading-proxy" {
		t.Fatal("root hotkey or subnet owner custody was substituted for the current coldkey origins")
	}
	if report.RootWeightCallStatus != "absent-retired-in-reviewed-source" || report.RetiredClaimControlStatus != "absent-retired-in-reviewed-source" || report.RootWeightsProxy != "denies-all-calls-in-reviewed-source" || report.Participant.PeriodicHotkeyCall != "not-required-for-unchanged-accumulation-under-reviewed-source" {
		t.Fatalf("retired root behavior was reintroduced: %+v", report)
	}
	if report.AuditedMetadataMatched || report.RuntimeSourceProven || report.CurrentRuntimeVerified || report.NativeSigning || report.NetworkEffects || report.ActivationReady || !report.PlanningOnly {
		t.Fatal("a compatible public projection acquired live, custody or source authority")
	}
	second, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
	if err != nil || !reflect.DeepEqual(report, second) {
		t.Fatal("identical metadata changed the report")
	}
}

// Matching names must not hide changed indices, widths, order or missing names.
func TestRootCurrentCapabilitiesRejectsChangedCallShape(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.Si1Variant, *types.Metadata)
	}{
		{name: "index", mutate: func(call *types.Si1Variant, metadata *types.Metadata) { call.Index = 146 }},
		{name: "field-count", mutate: func(call *types.Si1Variant, metadata *types.Metadata) { call.Fields = call.Fields[:4] }},
		{name: "field-name", mutate: func(call *types.Si1Variant, metadata *types.Metadata) { call.Fields[3].Name = "amount_staked" }},
		{name: "unnamed-field", mutate: func(call *types.Si1Variant, metadata *types.Metadata) { call.Fields[3].HasName = false }},
		{name: "field-order", mutate: func(call *types.Si1Variant, metadata *types.Metadata) {
			call.Fields[1], call.Fields[2] = call.Fields[2], call.Fields[1]
		}},
		{name: "amount-width", mutate: func(call *types.Si1Variant, metadata *types.Metadata) { call.Fields[3].Type = call.Fields[1].Type }},
		{name: "account-shape", mutate: func(call *types.Si1Variant, metadata *types.Metadata) { call.Fields[0].Type = call.Fields[3].Type }},
		{name: "missing-type", mutate: func(call *types.Si1Variant, metadata *types.Metadata) {
			delete(metadata.AsMetadataV14.EfficientLookup, call.Fields[3].Type.Int64())
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
			testCase.mutate(rootCurrentTestVariant(t, metadata, "swap_basket"), metadata)
			report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
			if err != nil {
				t.Fatal(err)
			}
			if report.MetadataInterfaceCompatible || report.Status != "metadata-interface-unavailable" || rootCurrentTestCall(t, report, "swap_basket").MetadataStatus != "schema-mismatch" || report.Participant.PeriodicHotkeyCall != "unknown-for-supplied-metadata" {
				t.Fatalf("changed interface passed: %+v", report)
			}
		})
	}
}

// Multi-trade metadata must retain all four tuple members and their widths.
func TestRootCurrentCapabilitiesRejectsBasketLegShape(t *testing.T) {
	for _, name := range []string{"not-sequence", "short-tuple", "amount-width", "recursive-type"} {
		t.Run(name, func(t *testing.T) {
			_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
			call := rootCurrentTestVariant(t, metadata, "swap_basket_many")
			id := call.Fields[1].Type
			entry := metadata.AsMetadataV14.EfficientLookup[id.Int64()]
			for entry != nil && entry.Def.IsComposite && len(entry.Def.Composite.Fields) == 1 {
				id = entry.Def.Composite.Fields[0].Type
				entry = metadata.AsMetadataV14.EfficientLookup[id.Int64()]
			}
			if entry == nil || !entry.Def.IsSequence {
				t.Fatal("public projection lacks basket leg sequence")
			}
			leg := metadata.AsMetadataV14.EfficientLookup[entry.Def.Sequence.Type.Int64()]
			if leg == nil || !leg.Def.IsTuple || len(leg.Def.Tuple) != 4 {
				t.Fatal("public projection lacks four-part basket leg")
			}
			switch name {
			case "not-sequence":
				call.Fields[1].Type = call.Fields[0].Type
			case "short-tuple":
				leg.Def.Tuple = leg.Def.Tuple[:3]
			case "amount-width":
				leg.Def.Tuple[2] = leg.Def.Tuple[0]
			case "recursive-type":
				entry.Def.IsSequence, entry.Def.IsComposite = false, true
				entry.Def.Composite.Fields = []types.Si1Field{{Type: id}}
			}
			report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
			if err != nil || report.MetadataInterfaceCompatible || rootCurrentTestCall(t, report, "swap_basket_many").MetadataStatus != "schema-mismatch" {
				t.Fatalf("unsupported basket legs passed: %+v, %v", report, err)
			}
		})
	}
}

// A reviewed source name cannot fill in a call missing from supplied metadata.
func TestRootCurrentCapabilitiesMissingCallRemainsUnavailable(t *testing.T) {
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	variants := rootCurrentTestVariants(t, metadata)
	rootCurrentTestVariant(t, metadata, "root_register")
	for index, variant := range *variants {
		if variant.Name == "root_register" {
			*variants = append((*variants)[:index], (*variants)[index+1:]...)
			break
		}
	}
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
	if err != nil {
		t.Fatal(err)
	}
	call := rootCurrentTestCall(t, report, "root_register")
	if report.MetadataInterfaceCompatible || call.MetadataStatus != "absent" || call.ObservedCallIndex != nil || report.ActivationReady {
		t.Fatalf("source fabricated a missing registration call: %+v", report)
	}
}

// Retired calls cannot silently create a hybrid current/legacy profile.
func TestRootCurrentCapabilitiesRetiredCallsRefuseProfile(t *testing.T) {
	for name, index := range map[string]types.U8{"set_root_weights": 146, "set_root_claim_type": 122, "sudo_set_num_root_claims": 123} {
		t.Run(name, func(t *testing.T) {
			_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
			variants := rootCurrentTestVariants(t, metadata)
			*variants = append(*variants, types.Si1Variant{Name: types.Text(name), Index: index})
			report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
			if err != nil || report.MetadataInterfaceCompatible || report.Participant.PeriodicHotkeyCall != "unknown-for-supplied-metadata" || report.ActivationReady {
				t.Fatalf("retired call passed: %+v, %v", report, err)
			}
			if name == "set_root_weights" && report.RootWeightCallStatus != "unexpected-present" || name != "set_root_weights" && report.RetiredClaimControlStatus != "unexpected-present" {
				t.Fatal("retired call was not reported")
			}
		})
	}
}

// Duplicate registries and changed pallet identity have no unambiguous report.
func TestRootCurrentCapabilitiesRejectsAmbiguousRegistry(t *testing.T) {
	for _, name := range []string{"duplicate-name", "duplicate-index", "duplicate-pallet", "duplicate-pallet-index", "pallet-index", "no-calls"} {
		t.Run(name, func(t *testing.T) {
			_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
			variants := rootCurrentTestVariants(t, metadata)
			copyCall := *rootCurrentTestVariant(t, metadata, "root_register")
			switch name {
			case "duplicate-name":
				copyCall.Index = 146
				*variants = append(*variants, copyCall)
			case "duplicate-index":
				copyCall.Name = "different_name"
				*variants = append(*variants, copyCall)
			default:
				for index, pallet := range metadata.AsMetadataV14.Pallets {
					if pallet.Name != "SubtensorModule" {
						continue
					}
					switch name {
					case "duplicate-pallet":
						metadata.AsMetadataV14.Pallets = append(metadata.AsMetadataV14.Pallets, pallet)
					case "duplicate-pallet-index":
						pallet.Name = "OtherPallet"
						metadata.AsMetadataV14.Pallets = append(metadata.AsMetadataV14.Pallets, pallet)
					case "pallet-index":
						metadata.AsMetadataV14.Pallets[index].Index = 6
					case "no-calls":
						metadata.AsMetadataV14.Pallets[index].HasCalls = false
					}
					break
				}
			}
			if _, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource); err == nil {
				t.Fatal("ambiguous current root metadata was accepted")
			}
		})
	}
}

// Metadata supplies neither live capacity nor stake, even with compatible calls.
func TestRootCurrentCapabilitiesCapacityAndStakeRemainUnknown(t *testing.T) {
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
	if err != nil {
		t.Fatal(err)
	}
	registration := report.Registration
	if registration.CurrentEligibility != "unknown" || registration.CurrentMaximumSeats != nil || registration.CurrentApplicantStake != nil || registration.CurrentCandidateStake != nil {
		t.Fatal("metadata fabricated capacity, applicant stake or a pruning candidate")
	}
	if registration.FreeCapacityStakeRule != "no-displacement-stake-comparison" || registration.FullCapacityStakeRule != "applicant-root-stake-at-least-lowest-nonimmune-member;no-nonimmune-member-refuses-registration" || registration.BurnProtection != "root_register-has-no-maximum-burn-argument" {
		t.Fatal("registration planning hid full-capacity stake admission or uncapped native burn")
	}
	participant := report.Participant
	if participant.Membership != "unknown" || participant.RootStake != "unknown" || participant.BasketAccrual != "unknown" || participant.Custody != "unknown" || participant.ObservationService != "unverified" || participant.RootHotkeyDevice != "unspecified-separate-hardware" || participant.RootColdkeyDevice != "unspecified-independent-custody" || participant.StakeClaimColdkeyDevice != "unspecified-independent-custody" {
		t.Fatal("offline report fabricated participation or borrowed subnet owner custody")
	}
	raw, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"current_maximum_seats", "current_applicant_root_stake_rao", "current_pruning_candidate_root_stake_rao"} {
		if !bytes.Contains(raw, []byte(`"`+field+`":null`)) {
			t.Fatalf("unknown %s is not explicit JSON null: %s", field, raw)
		}
	}
}

// Unknown hardware is not an admission gate for an unchanged existing fund.
// A separately approved mutation still needs its own identified custody path.
func TestRootCurrentCapabilitiesPassiveNeedsNoMutationCustody(t *testing.T) {
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
	if err != nil {
		t.Fatal(err)
	}
	if report.MutationCustodyRequirement != "separately-approved-mutations-only;no-native-device-required-for-observation-or-unchanged-participation-under-reviewed-source" || report.MutationExecution != "not-implemented;required-only-for-separately-approved-mutations" {
		t.Fatal("unchanged participation acquired a mutation custody prerequisite")
	}
	for _, blocker := range report.Blockers {
		if strings.Contains(blocker, "CUSTODY") || strings.Contains(blocker, "SIGN") || strings.Contains(blocker, "WEIGHT") || strings.Contains(blocker, "NONCE") {
			t.Fatalf("passive participation demands native mutation authority: %s", blocker)
		}
	}
	if report.Participant.Custody != "unknown" || report.Participant.RootColdkeyDevice != "unspecified-independent-custody" || report.Participant.RootHotkeyDevice != "unspecified-separate-hardware" || report.Participant.Membership != "unknown" || report.Participant.RootStake != "unknown" || report.Participant.BasketAccrual != "unknown" || report.ActivationReady || report.NativeSigning || report.NetworkEffects {
		t.Fatal("removing a false custody gate fabricated hardware or participation evidence")
	}
}

// The public route can only read its input and print a non-authoritative report.
func TestRootCurrentCapabilitiesPublicCommandIsReadOnly(t *testing.T) {
	raw, _, digest, fileHash := rootCurrentTestMetadata(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "public-metadata.scale")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"root-capabilities", "--metadata", path, "--metadata-hash", digest, "--runtime-source-commit", rootPassiveSource}
	if exitCode := runMain(context.Background(), args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("public offline route returned %d: %s", exitCode, &stderr)
	}
	var report rootCurrentCapabilities
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != rootCurrentCapabilitiesSchema || report.MetadataHash != digest || report.MetadataFileHash != fileHash || !report.PlanningOnly || report.ActivationReady || report.NativeSigning || report.NetworkEffects {
		t.Fatalf("public command changed authority: %+v", report)
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, retained) {
		t.Fatal("public metadata input was modified")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("offline report created state beside its input")
	}
	if mainnetRequiresDurableVolumes(args) {
		t.Fatal("offline capability inspection entered durable custody")
	}
}

// A wrong pin must refuse even malformed SCALE before attempting its decoder.
func TestRootCurrentCapabilitiesWrongPinRejectedBeforeScale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed.scale")
	if err := os.WriteFile(path, []byte{0xff, 0xff, 0xff, 0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"--metadata", path, "--metadata-hash", "0x" + strings.Repeat("ab", 32), "--runtime-source-commit", rootPassiveSource}
	if exitCode := runRootCurrentCapabilitiesCommand(context.Background(), args, &stdout, &stderr); exitCode != 3 || stdout.Len() != 0 || !strings.Contains(stderr.String(), errNativeMetadataPin.Error()) {
		t.Fatalf("wrong pin was not refused before decoding: %d, %s, %s", exitCode, &stdout, &stderr)
	}
}

// Mutation inputs have no command surface and cannot select a signer or route.
func TestRootCurrentCapabilitiesRejectsMutationFlags(t *testing.T) {
	for _, option := range []string{"--sign", "--rpc", "--device", "--broadcast", "--approval"} {
		t.Run(option, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if exitCode := runRootCurrentCapabilitiesCommand(context.Background(), []string{option, "synthetic"}, &stdout, &stderr); exitCode != 2 || stdout.Len() != 0 {
				t.Fatalf("mutation input was accepted: %d, %s", exitCode, &stdout)
			}
		})
	}
}

// A historical source and a cancelled read cannot produce a current report.
func TestRootCurrentCapabilitiesSourceAndCancellationRefuse(t *testing.T) {
	raw, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	if _, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootProfileSource); err == nil {
		t.Fatal("historical root-weights source acquired current semantics")
	}
	path := filepath.Join(t.TempDir(), "public-metadata.scale")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--metadata", path, "--metadata-hash", digest, "--runtime-source-commit", rootPassiveSource}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if exitCode := runRootCurrentCapabilitiesCommand(ctx, args, &stdout, &stderr); exitCode == 0 || stdout.Len() != 0 {
		t.Fatalf("cancelled read produced a report: %d, %s", exitCode, &stdout)
	}
	args[len(args)-1] = rootProfileSource
	stdout.Reset()
	stderr.Reset()
	if exitCode := runRootCurrentCapabilitiesCommand(context.Background(), args, &stdout, &stderr); exitCode != 2 || stdout.Len() != 0 {
		t.Fatalf("public route accepted historical source: %d, %s", exitCode, &stdout)
	}
}
