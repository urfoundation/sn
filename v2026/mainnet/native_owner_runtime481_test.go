// Runtime 481 metadata capabilities are checked independently of source admission.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

const runtime481TestMetadataPath = "../crv4/testdata/runtime481-metadata.scale.gz.base64"
const runtime481TestMetadataSize = 364065
const runtime481TestMetadataSha256 = "4dc0d25bc11d1ea15118b433ba34e78f40ae2bfbaa188757e6f67f0a3240266a"
const runtime481TestMetadataHash = "0x40db5b7feac3a8e9ab2ab000234257428826d9827ab326034a3329fdd0d50088"

// Decompression is bounded and the exact size, SHA-256 and BLAKE2b-256 pins are
// checked before any interface is interpreted.
func runtime481TestMetadata(t *testing.T) ([]byte, *types.Metadata) {
	t.Helper()
	encoded, err := os.ReadFile(runtime481TestMetadataPath)
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime481TestMetadataSize+1))
	if err != nil || len(raw) != runtime481TestMetadataSize {
		t.Fatalf("runtime481 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime481 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime481TestMetadataSha256 {
		t.Fatal("runtime481 metadata SHA-256 differs from its review pin")
	}
	metadata, hash, err := nativePinnedMetadata("0x"+hex.EncodeToString(raw), runtime481TestMetadataHash)
	if err != nil || hash != runtime481TestMetadataHash {
		t.Fatal("runtime481 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return raw, metadata
}

// Metadata compatibility is planning information, independent of deployed
// artifact identity and approval of any native transaction.
func TestNativeOwnerRuntime481MetadataCapabilities(t *testing.T) {
	raw, metadata := runtime481TestMetadata(t)
	nativeOwnerMetadataCapabilityChecks(t, metadata)
	nativeOwnerMetadataExtendedChecks(t, metadata)
	digest := sha256.Sum256(raw)
	report, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), crv4.NativeOwnerSource481)
	if err != nil || !report.MetadataInterfaceCompatible || !report.PlanningOnly || report.RuntimeSourceProven || report.CurrentRuntimeVerified || report.ActivationReady || report.NativeSigning {
		t.Fatal("runtime481 root capability report changed or invented authority", report, err)
	}
	if _, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), strings.Repeat("c", 40)); err == nil {
		t.Fatal("unreviewed source interpreted runtime481 root capabilities")
	}
}

// The complete structural inventory grows from 1,599 to 1,616 items: one
// admin call, twelve errors, one event and three storage items. Existing items
// change only in the System.Version value, the appended rate-limit enum member,
// and types embedding the expanded RuntimeCall/RuntimeEvent. Hashes pin the
// complete rendered wrappers without copying their megabytes into this test.
// Metadata wire compatibility does not assert unchanged economic semantics.
func TestNativeOwnerRuntime481MetadataDeltaFrom479(t *testing.T) {
	_, old := runtime479TestMetadata(t)
	_, current := runtime481TestMetadata(t)
	before, after := nativeOwnerMetadataItems(old), nativeOwnerMetadataItems(current)
	added := map[string]string{
		"call AdminUtils.sudo_set_subnet_emission_weights":            "index 114 {netuid:subtensor_runtime_common::NetUid{prim4},uids:Vec<prim4>,values:Vec<prim6>}",
		"error SubtensorModule.BurnEmissionMustBeZero":                "index 180 {}",
		"error SubtensorModule.EmissionWeightIndexDuplicated":         "index 178 {}",
		"error SubtensorModule.EmissionWeightIndexOutOfRange":         "index 177 {}",
		"error SubtensorModule.EmissionWeightsDoNotSumToOne":          "index 179 {}",
		"error SubtensorModule.EmissionWeightsLengthMismatch":         "index 176 {}",
		"error SubtensorModule.EmissionWeightsNotSettableOnRoot":      "index 187 {}",
		"error SubtensorModule.LeasedSubnetOwnerCutBelowMinimum":      "index 183 {}",
		"error SubtensorModule.MinerEmissionBelowMinimum":             "index 186 {}",
		"error SubtensorModule.OwnerEmissionAboveMaximum":             "index 182 {}",
		"error SubtensorModule.RecycleEmissionMustBeZero":             "index 181 {}",
		"error SubtensorModule.ValidatorEmissionAboveMaximum":         "index 184 {}",
		"error SubtensorModule.ValidatorEmissionTooLowToPreserveRoot": "index 185 {}",
		"event SubtensorModule.SubnetEmissionWeightsSet":              "index 158 {netuid:subtensor_runtime_common::NetUid{prim4},weights:Vec<prim6>}",
		"storage SubtensorModule.SubnetCreatedViaLease":               "prefix SubtensorModule modifier default map Identity subtensor_runtime_common::NetUid{prim4} -> prim0 fallback 00",
		"storage SubtensorModule.SubnetEmissionWeights":               "prefix SubtensorModule modifier default map Identity subtensor_runtime_common::NetUid{prim4} -> bounded_collections::bounded_vec::BoundedVec{Vec<prim6>}[T=prim6] fallback 1400000000000000000000000000000000142e142e142e142ef5e8f5e8f5e8f568f6e8f5e8f5e8f568",
		"storage SubtensorModule.SubnetOwnerCutAccrued":               "prefix SubtensorModule modifier default map Identity subtensor_runtime_common::NetUid{prim4} -> subtensor_runtime_common::currency::AlphaBalance{prim6} fallback 0000000000000000",
	}
	changed := map[string][2]string{
		"call Multisig.as_multi":                       {"c92f6cb87d6f8efe7afbb7cc60d5b917e797cd0f7203d8fde488b6c3732b4232", "cf37a4941f99e7fbc9ff062c91e261a6edc11299d4471bdf1143515c1a2e101f"},
		"call Multisig.as_multi_threshold_1":           {"dea56f3b16f9fc9ef3ecd4a72b1fdc2f772b6f962fc00d4ea80e31f44863eb52", "170f11402be6b8fb1623d89bc93030ec4d497ac123aa0ea63f48d81620174792"},
		"call Proxy.proxy":                             {"0e3d34397a79521a3d9ab6a88ee5340f5e55b1c254fa6b29c3f75a2f112481a8", "63b1242799932672194507eb4719c1fedd6fa5b3e28a991b41facd30694077e1"},
		"call Proxy.proxy_announced":                   {"459fde6c9419d2da6bfa1a51beb69ee34ed0e7b4974adc70360ad40a221cebc6", "6c482391c42502f12291db39db99b5f2389030093cd72790b53061e666b287c4"},
		"call Scheduler.schedule":                      {"f0ec385bfe98118b6bebf9fac6224d36724e8ad43f8eb95128a876e12a396168", "ef70c274477d3f91c9e6dd41247472362c06b2dc63e9276e149252b531cf20e3"},
		"call Scheduler.schedule_after":                {"a2e232dc8892559a807a8b3938d2571c2a353b0dc34404f07218ca6dee05f9b1", "0f3d1a33bd567875f9a3a11a7cf49ff8f4bc3c04e6cb53cc498305d3b9f3768a"},
		"call Scheduler.schedule_named":                {"ef5d7f26419801d23cfdb951815059edbb9086fbd0a53d9d5f0e998d9a089b59", "123a6140722f40fe5182030a3d13df2ff871fb862d0c71ae80c23e405cf36a79"},
		"call Scheduler.schedule_named_after":          {"8984bff60131fc584d8789529d02b9dc43a283bc6005e2789532cf97f6b2a34b", "a4d9832f95094c3731068db224d7061ecf4a552ad6e0b2f142cb58dbdb61609c"},
		"call Sudo.sudo":                               {"1937794d6c6466446f3802ef89feaf4ef8fb503ecae0db8cf6e24eb4465f99c7", "d100ad78f62063d1958c23bd467e030207d9edc1096e7ce5763b42c25270f7b6"},
		"call Sudo.sudo_as":                            {"d702f7d60343d809abea6c196bfb01929f58c5244b4dc91000161f8de0ed5b6b", "9e1fba61b9d61ffb2a80303b0eb22825521c5c4d769b79f5319208a36430c26d"},
		"call Sudo.sudo_unchecked_weight":              {"8b7eaee87cb111aeb87c291bf612a56c568d79ab2b92824028b239c98d374fb2", "56de73dbf9699f8558a50c926d51875e80f5b45130e6e2f0dc74212418972369"},
		"call Utility.as_derivative":                   {"615bc9d5e505e8e3cd98264790b540ff4f8bbae5c4442dafd1f267d5d1c13f57", "3f9fcb8a28aa6b5aa2436233b2cbbf9f9e5d03a5b1c0ff95c6afff31ced318ae"},
		"call Utility.batch":                           {"99da05cfa6d27067704ddd276bd36a1e082da1a33d9da4fe0605b7c31374fa0c", "98626eec66fa5994c54bbfecaaa3e4bab61f847208ac1233dbeca9ee32052482"},
		"call Utility.batch_all":                       {"496f6562fd01a63974a99d7c1dafeb9bfce0dc42afe1ffe152c1633ae83ad1e9", "c6fd2178a4cf20b028e1b2558a7abae4d3de23614d1ebab1e46ffdde6fc88aac"},
		"call Utility.dispatch_as":                     {"c2d32d580e58162a569a0fbdfa3b185dccc9ddcc7c23a2bce1c60e41a7e10fa3", "f1029fdfe3ddf2b2eb8dc2146319b85f98d192a8be45865d58370ef646c65098"},
		"call Utility.dispatch_as_fallible":            {"dc77b4c121f1d1cc73f2dec99759232585a94e3d3f5121d0a6d5134828302890", "304d6c348a6cb7028d7a6247aec759996a2a7d71448015550b45d65a8e8b10b0"},
		"call Utility.force_batch":                     {"5d58176d51dba349417b49e3c48e416c523a02b059bd0bfa0d2ea14608d60f4f", "c60e2f08759bdae671fc7e63e0f80651a927e6e4dafcfba8ce39fee8d26dd4d5"},
		"call Utility.if_else":                         {"ce0fa1387efce3be4b8a56cc676335b882d535944c388a050e03d11601c95307", "04c86bba41a87f71c05f6157593703700a22efa846cc0cb95b8fa6ce80438e5a"},
		"call Utility.with_weight":                     {"db15af56facf9a511a13043b1c54cd86ddd5600caa5aac21810e6dd038bec477", "c8110158a2eac9996127f13810dcf9cf01237f05238b0349e0f8669f26396f68"},
		"constant System.Version":                      {"444ee08adc2db5ea1425c36b59a5325a46090906eb64868ca95c88155b86280a", "d8ada7645be8e28ca0c92ca94d52da53282f2927d276b6c678bc25d7390252bc"},
		"extrinsic":                                    {"118aabffd27275edac29a12fe65a48ab924f4a51368d908a6dda479604b4e4dd", "7428c141263d40b1b5d7bf49bc8a393d900e53f3e53b1681233ba92de2d5bc06"},
		"storage Crowdloan.Crowdloans":                 {"6890ad0165c23f8cb73af5ca8ac14e736c55fff258fcc3acce23fb5343f63ac1", "42c5bd09417247621367befcd847e307dbf77df3bf3954e3008ca5cabc2daf62"},
		"storage Scheduler.Agenda":                     {"70cc570b058d4bd0656f4a19faca114fc96eb3d9bf947a49b15cf0ed244d25cb", "d8e63c15890da8e209a986972c1c47426495bc6d0494c5172fb8da81516aca10"},
		"storage SubtensorModule.LastRateLimitedBlock": {"73344a78ec597f83f5df84bf56840ca7dd8f7c83a6d85f808987921e8baf34eb", "e01820b018f0097166c6e17bb3841427ccb70b0cd4627230e0b787d7f27e25b8"},
		"storage System.Events":                        {"1f82443f773e64d0c5d7ffadf0abcfefd55bef91e98d65a346ec3b0a8adb139c", "0284353c00a19086cace794785311fd30336931c72f1bb0e0cf0f1fba86fd452"},
	}
	digest := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	if len(before) != 1599 || len(after) != 1616 {
		t.Fatal("runtime481 complete structural inventory differs", len(before), len(after))
	}
	for name, prior := range before {
		next, present := after[name]
		pins, reviewedChange := changed[name]
		switch {
		case !present:
			t.Error("runtime481 removed an existing metadata item", name)
		case reviewedChange && (digest(prior) != pins[0] || digest(next) != pins[1] || prior == next):
			t.Error("runtime481 reviewed structural change differs", name)
		case !reviewedChange && prior != next:
			t.Error("runtime481 changed an unreviewed metadata item", name)
		}
	}
	for name, shape := range after {
		if _, existed := before[name]; existed {
			continue
		}
		if want, reviewed := added[name]; !reviewed || shape != want {
			t.Error("runtime481 added an unreviewed metadata item or shape", name, shape)
		}
	}
	for name := range changed {
		if _, present := before[name]; !present {
			t.Error("reviewed predecessor metadata item is missing", name)
		}
	}
	for name, shape := range added {
		if _, existed := before[name]; existed || after[name] != shape {
			t.Error("runtime481 reviewed addition is absent or existed in its predecessor", name)
		}
	}
	// The old keys keep their exact encodings. Only the final hyperparameter
	// variant39 is appended; no existing rate-limit slot can be relabelled.
	name := "storage SubtensorModule.LastRateLimitedBlock"
	expected := strings.Replace(before[name], "|38:Difficulty{}>", "|38:Difficulty{}|39:EmissionWeights{}>", 1)
	if expected == before[name] || after[name] != expected {
		t.Fatal("runtime481 rate-limit key changed more than the reviewed appended variant")
	}
}
