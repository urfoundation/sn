// The exact runtime-479 metadata, captured read-only from Finney, must expose
// every interface that a reviewed native owner source selects. The fixture
// carries no deployed identity, account state, signature, key or authority.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/validator"
)

const runtime479TestMetadataPath = "../crv4/testdata/runtime479-metadata.scale.gz.base64"
const runtime479TestMetadataSize = 359018
const runtime479TestMetadataSha256 = "075ee9614fee27fb83a44bda737f6803bd3f6642d57ab492321aa47dad359080"
const runtime479TestMetadataHash = "0x3ef17424a54b53d3630c2fde92ebff918364a2f4730c9c23e22ebdd7c21575d1"

// Decompression is bounded and the exact size, SHA-256 and BLAKE2b-256 pins are
// checked before any interface is interpreted.
func runtime479TestMetadata(t *testing.T) ([]byte, *types.Metadata) {
	t.Helper()
	encoded, err := os.ReadFile(runtime479TestMetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	reader.Multistream(false)
	raw, err := io.ReadAll(io.LimitReader(reader, runtime479TestMetadataSize+1))
	if err != nil || len(raw) != runtime479TestMetadataSize {
		t.Fatalf("runtime479 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime479 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime479TestMetadataSha256 {
		t.Fatal("runtime479 metadata SHA-256 differs from its review pin")
	}
	metadata, hash, err := nativePinnedMetadata("0x"+hex.EncodeToString(raw), runtime479TestMetadataHash)
	if err != nil || hash != runtime479TestMetadataHash {
		t.Fatal("runtime479 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return raw, metadata
}

// The pinned bytes, not a version number, select every reviewed interface.
func TestNativeOwnerRuntime479MetadataCapabilities(t *testing.T) {
	raw, metadata := runtime479TestMetadata(t)
	nativeOwnerMetadataCapabilityChecks(t, metadata)
	nativeOwnerMetadataExtendedChecks(t, metadata)
	digest := sha256.Sum256(raw)
	report, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), crv4.NativeOwnerSource479)
	if err != nil || !report.MetadataInterfaceCompatible || !report.PlanningOnly || report.RuntimeSourceProven || report.CurrentRuntimeVerified || report.ActivationReady || report.NativeSigning {
		t.Fatal("runtime479 root capability report changed or invented authority", report, err)
	}
	if _, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), strings.Repeat("c", 40)); err == nil {
		t.Fatal("unreviewed source interpreted runtime479 root capabilities")
	}
}

// 479 adds one call, two errors, two events and two storage items, and
// changes nothing else that SN reads. Every other difference from 478 is the
// RuntimeCall or RuntimeEvent enum growing by those variants, carried into the
// items that embed it, and the spec version. Items are compared structurally,
// by name, index and fully rendered type, since type IDs renumber.
func TestNativeOwnerRuntime479MetadataDeltaFrom478(t *testing.T) {
	// Each pallet, storage item, call, event, error and constant, and the
	// extrinsic format, keyed by kind and name, with its type spelled out in full.
	metadataItemKVs := func(metadata *types.Metadata) map[string]string {
		v14 := metadata.AsMetadataV14
		typeKVs := map[int64]types.Si1Type{}
		for _, entry := range v14.Lookup.Types {
			typeKVs[entry.ID.Int64()] = entry.Type
		}
		renderedKVs, activeKVs := map[int64]string{}, map[int64]bool{}
		var render func(int64) string
		fields := func(list []types.Si1Field) string {
			renderedFields := []string{}
			for _, field := range list {
				name := ""
				if field.HasName {
					name = string(field.Name) + ":"
				}
				renderedFields = append(renderedFields, name+render(field.Type.Int64()))
			}
			return "{" + strings.Join(renderedFields, ",") + "}"
		}
		render = func(id int64) string {
			if text, ok := renderedKVs[id]; ok {
				return text
			}
			entry := typeKVs[id]
			segments := []string{}
			for _, segment := range entry.Path {
				segments = append(segments, string(segment))
			}
			path := strings.Join(segments, "::")
			// A type already being rendered further up is named, not expanded, so a
			// recursive type terminates.
			if activeKVs[id] {
				return "@" + path
			}
			activeKVs[id] = true
			defer delete(activeKVs, id)
			var text string
			switch def := entry.Def; {
			case def.IsComposite:
				text = path + fields(def.Composite.Fields)
			case def.IsVariant:
				variants := []string{}
				for _, variant := range def.Variant.Variants {
					variants = append(variants, fmt.Sprintf("%d:%s%s", variant.Index, variant.Name, fields(variant.Fields)))
				}
				text = path + "<" + strings.Join(variants, "|") + ">"
			case def.IsSequence:
				text = "Vec<" + render(def.Sequence.Type.Int64()) + ">"
			case def.IsArray:
				text = fmt.Sprintf("[%s;%d]", render(def.Array.Type.Int64()), def.Array.Len)
			case def.IsTuple:
				members := []string{}
				for _, member := range def.Tuple {
					members = append(members, render(member.Int64()))
				}
				text = "(" + strings.Join(members, ",") + ")"
			case def.IsPrimitive:
				text = fmt.Sprintf("prim%d", def.Primitive.Si0TypeDefPrimitive)
			case def.IsCompact:
				text = "Compact<" + render(def.Compact.Type.Int64()) + ">"
			case def.IsBitSequence:
				text = "BitSeq<" + render(def.BitSequence.BitStoreType.Int64()) + "," + render(def.BitSequence.BitOrderType.Int64()) + ">"
			default:
				text = "?" + path
			}
			params := []string{}
			for _, param := range entry.Params {
				if param.HasType {
					params = append(params, string(param.Name)+"="+render(param.Type.Int64()))
				}
			}
			if len(params) != 0 {
				text += "[" + strings.Join(params, ",") + "]"
			}
			renderedKVs[id] = text
			return text
		}
		hasher := func(h types.StorageHasherV10) string {
			switch {
			case h.IsBlake2_128:
				return "Blake2_128"
			case h.IsBlake2_256:
				return "Blake2_256"
			case h.IsBlake2_128Concat:
				return "Blake2_128Concat"
			case h.IsTwox128:
				return "Twox128"
			case h.IsTwox256:
				return "Twox256"
			case h.IsTwox64Concat:
				return "Twox64Concat"
			case h.IsIdentity:
				return "Identity"
			}
			return "?"
		}
		itemKVs := map[string]string{}
		for _, pallet := range v14.Pallets {
			name := string(pallet.Name)
			itemKVs["pallet "+name] = fmt.Sprintf("index %d", pallet.Index)
			if pallet.HasStorage {
				for _, item := range pallet.Storage.Items {
					modifier := "default"
					if item.Modifier.IsOptional {
						modifier = "optional"
					}
					shape := "plain " + render(item.Type.AsPlainType.Int64())
					if item.Type.IsMap {
						hashers := []string{}
						for _, h := range item.Type.AsMap.Hashers {
							hashers = append(hashers, hasher(h))
						}
						shape = "map " + strings.Join(hashers, ",") + " " + render(item.Type.AsMap.Key.Int64()) + " -> " + render(item.Type.AsMap.Value.Int64())
					}
					itemKVs["storage "+name+"."+string(item.Name)] = fmt.Sprintf("prefix %s modifier %s %s fallback %x", pallet.Storage.Prefix, modifier, shape, []byte(item.Fallback))
				}
			}
			for _, kind := range []struct {
				label string
				has   bool
				id    int64
			}{
				{label: "call", has: bool(pallet.HasCalls), id: pallet.Calls.Type.Int64()},
				{label: "event", has: bool(pallet.HasEvents), id: pallet.Events.Type.Int64()},
				{label: "error", has: bool(pallet.HasErrors), id: pallet.Errors.Type.Int64()},
			} {
				if !kind.has {
					continue
				}
				for _, variant := range typeKVs[kind.id].Def.Variant.Variants {
					itemKVs[kind.label+" "+name+"."+string(variant.Name)] = fmt.Sprintf("index %d %s", variant.Index, fields(variant.Fields))
				}
			}
			for _, constant := range pallet.Constants {
				itemKVs["constant "+name+"."+string(constant.Name)] = render(constant.Type.Int64()) + " = " + fmt.Sprintf("%x", []byte(constant.Value))
			}
		}
		extensions := []string{}
		for _, extension := range v14.Extrinsic.SignedExtensions {
			extensions = append(extensions, string(extension.Identifier)+":"+render(extension.Type.Int64())+"/"+render(extension.AdditionalSigned.Int64()))
		}
		itemKVs["extrinsic"] = fmt.Sprintf("version %d type %s ext [%s]", v14.Extrinsic.Version, render(v14.Extrinsic.Type.Int64()), strings.Join(extensions, ";"))
		return itemKVs
	}
	_, previous := runtime478TestMetadata(t)
	_, current := runtime479TestMetadata(t)
	beforeItemKVs, afterItemKVs := metadataItemKVs(previous), metadataItemKVs(current)
	addedItemKVs := map[string]string{
		"call SubtensorModule.set_reject_staking_hotkey_growth":   "index 153 {enabled:prim0}",
		"error EVM.AuthorizationListTooLarge":                     "index 15 {}",
		"error SubtensorModule.AccountRejectsStakingHotkeyGrowth": "index 175 {}",
		"event SubtensorModule.RejectStakingHotkeyGrowthUpdated":  "index 156 {coldkey:sp_core::crypto::AccountId32{[prim3;32]},enabled:prim0}",
		"event SubtensorModule.HotkeySwapStakeRetained":           "index 157 {coldkey:sp_core::crypto::AccountId32{[prim3;32]},old_hotkey:sp_core::crypto::AccountId32{[prim3;32]},new_hotkey:sp_core::crypto::AccountId32{[prim3;32]},netuid:subtensor_runtime_common::NetUid{prim4}}",
		"storage EVM.AccountNonces":                               "",
		"storage SubtensorModule.HotkeySwapBasketCredits":         "",
	}
	changedItemKVs := map[string]bool{"constant System.Version": true, "extrinsic": true, "storage System.Events": true, "storage Scheduler.Agenda": true, "storage Crowdloan.Crowdloans": true}
	for _, name := range []string{"Multisig.as_multi", "Multisig.as_multi_threshold_1", "Proxy.proxy", "Proxy.proxy_announced", "Scheduler.schedule", "Scheduler.schedule_after",
		"Scheduler.schedule_named", "Scheduler.schedule_named_after", "Sudo.sudo", "Sudo.sudo_as", "Sudo.sudo_unchecked_weight", "Utility.as_derivative", "Utility.batch",
		"Utility.batch_all", "Utility.dispatch_as", "Utility.dispatch_as_fallible", "Utility.force_batch", "Utility.if_else", "Utility.with_weight"} {
		changedItemKVs["call "+name] = true
	}
	for name, value := range beforeItemKVs {
		next, kept := afterItemKVs[name]
		switch {
		case !kept:
			t.Error("runtime479 removed", name)
		case next == value && changedItemKVs[name]:
			t.Error("runtime479 kept an item the review records as changed", name)
		case next != value && !changedItemKVs[name]:
			t.Error("runtime479 changed an item the review records as unchanged", name)
		case next != value && name != "constant System.Version" && !strings.Contains(next, "node_subtensor_runtime::RuntimeCall") && !strings.Contains(next, "node_subtensor_runtime::RuntimeEvent"):
			t.Error("runtime479 changed an item that embeds neither RuntimeCall nor RuntimeEvent", name)
		}
	}
	for name, value := range afterItemKVs {
		if _, existed := beforeItemKVs[name]; existed {
			continue
		}
		want, expected := addedItemKVs[name]
		if !expected || want != "" && value != want {
			t.Error("runtime479 added an item the review does not record", name, value)
		}
	}
	if len(afterItemKVs) != len(beforeItemKVs)+len(addedItemKVs) {
		t.Fatal("runtime479 item count differs from the reviewed delta", len(beforeItemKVs), len(afterItemKVs))
	}
	if !strings.HasPrefix(afterItemKVs["storage EVM.AccountNonces"], "prefix EVM modifier default map Blake2_128Concat sp_core::crypto::AccountId32{[prim3;32]} -> prim5 fallback 00000000") ||
		!strings.HasPrefix(afterItemKVs["storage SubtensorModule.HotkeySwapBasketCredits"], "prefix SubtensorModule modifier optional map Blake2_128Concat,Blake2_128Concat (sp_core::crypto::AccountId32{[prim3;32]},sp_core::crypto::AccountId32{[prim3;32]}) -> () fallback 00") {
		t.Fatal("runtime479's new storage items differ from the reviewed source", afterItemKVs["storage EVM.AccountNonces"], afterItemKVs["storage SubtensorModule.HotkeySwapBasketCredits"])
	}
}

// A runtime-479 treasury approval also opens only on a proof, at its own
// original activation, that the Alpha migration completed; its predecessor's
// authority and a historical 470 authority cannot be relabeled to it.
func TestNativeTreasuryMigrationRuntime479RequiresCompletionProof(t *testing.T) {
	authority, policy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource479, 479)
	if err := authority.validateScope(policy, policy.Through, nil); err != nil {
		t.Fatal("completed runtime479 opening with fresh approval refused", err)
	}
	if nativeExecutionRuntimeSource(authority) != crv4.NativeOwnerSource479 {
		t.Fatal("producer lost the independently approved runtime479 source")
	}
	missing := *authority
	missing.MigrationComplete = nil
	if err := missing.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime479 acquired economic authority without a completion proof")
	}
	previous, previousPolicy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource478, 478)
	if err := previous.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("the predecessor's approval served the runtime479 policy")
	}
	if err := authority.validateScope(previousPolicy, previousPolicy.Through, nil); err == nil {
		t.Fatal("runtime479 approval served its predecessor's policy")
	}
	for index, marker := range [][]byte{nil, {}, {0}, {1, 0}, {2}} {
		witness, code := nativeMigrationTestWitness(t, marker)
		incomplete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
		if err := incomplete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource479, CodeHash: code}); err == nil {
			t.Fatal("incomplete original state accepted for runtime479", index)
		}
	}
	witness, code := nativeMigrationTestWitness(t, []byte{1})
	complete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource479, CodeHash: code}); err != nil {
		t.Fatal("bounded runtime479 completion proof refused", err)
	}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource470, CodeHash: code}); err == nil {
		t.Fatal("historical runtime470 authority inherited a migration completion")
	}
	wrongCode := code
	wrongCode[0] ^= 1
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource479, CodeHash: wrongCode}); err == nil {
		t.Fatal("runtime479 completion proof served another runtime artifact")
	}
}

// Root registration admits 479 only on its own spec number. A policy or
// observation sealed for 473 never serves the 479 registration domain.
func TestRootRegisterRuntime479PolicyAndEligibility(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	base := f.config.Action.Policy
	successor := base
	successor.RuntimeSourceCommit, successor.RuntimeVersion.SpecVersion = crv4.NativeOwnerSource479, 479
	if err := successor.validate(); err != nil {
		t.Fatal("reviewed runtime479 registration policy refused", err)
	}
	for _, mismatch := range []struct {
		source string
		spec   uint32
	}{
		{source: crv4.NativeOwnerSource479, spec: 478}, {source: crv4.NativeOwnerSource478, spec: 479}, {source: crv4.NativeOwnerSource479, spec: 473}, {source: crv4.NativeOwnerSource473, spec: 479},
		{source: crv4.NativeOwnerSource470, spec: 470}, {source: crv4.NativeOwnerSource470, spec: 479}, {source: strings.Repeat("d", 40), spec: 479}, {source: crv4.NativeOwnerSource479, spec: 477}, {source: crv4.NativeOwnerSource479, spec: 480},
	} {
		policy := base
		policy.RuntimeSourceCommit, policy.RuntimeVersion.SpecVersion = mismatch.source, mismatch.spec
		if err := policy.validate(); err == nil {
			t.Fatal("registration source and spec were not bound together", mismatch.source, mismatch.spec)
		}
	}
	observation := f.input.Observation
	if _, err := observation.eligibility(successor, f.metadata); err == nil {
		t.Fatal("runtime473 observation seal served the runtime479 policy")
	}
	observation.PolicyHash = rootObjectHash(successor)
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(observation)
	eligibility, err := observation.eligibility(successor, f.metadata)
	if err != nil || !eligibility.Eligible || eligibility.ExistingSeat != nil || eligibility.BurnRao != 10 || eligibility.ConservativeReducibleRao != 99500 {
		t.Fatalf("runtime479 free root slot differs from the shared reviewed rules: %+v %v", eligibility, err)
	}
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, crv4.NativeOwnerSource479)
	if err != nil {
		t.Fatal(err)
	}
	registration := rootCurrentTestCall(t, report, "root_register")
	if !strings.Contains(registration.MutationAdapter, "requires-separate-479-policy") || !strings.Contains(report.MutationExecution, "separate-479-root-register-domain") ||
		report.NativeSigning || report.NetworkEffects || report.ActivationReady || report.CurrentRuntimeVerified || !report.PlanningOnly {
		t.Fatal("runtime479 registration catalogue lost its separate execution boundary", registration, report)
	}
}
