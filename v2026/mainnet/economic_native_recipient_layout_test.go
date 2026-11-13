// Synthetic protocol originals exercise the public Go observer and its causal
// decoder. They do not claim execution of a production runtime or live custody.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/validator"
)

func nativeRecipientLayoutTestMetadata(metadata *types.Metadata) {
	for index := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[index]
		if pallet.Name != "SubtensorModule" {
			continue
		}
		entries := map[string]types.StorageEntryMetadataV14{}
		for _, entry := range pallet.Storage.Items {
			entries[string(entry.Name)] = entry
		}
		put := func(entry types.StorageEntryMetadataV14) {
			for i := range pallet.Storage.Items {
				if pallet.Storage.Items[i].Name == entry.Name {
					pallet.Storage.Items[i] = entry
					return
				}
			}
			pallet.Storage.Items = append(pallet.Storage.Items, entry)
		}
		recycle := entries["PendingServerEmission"]
		recycle.Name = "SubnetAlphaOut"
		put(recycle)
		owner := entries["PendingServerEmission"]
		owner.Name = "SubnetOwnerHotkey"
		owner.Type.AsMap.Value = entries["Owner"].Type.AsMap.Value
		owner.Fallback = make(types.Bytes, 32)
		put(owner)
		auto := entries["TotalHotkeyAlpha"]
		auto.Name = "AutoStakeDestination"
		auto.Type.AsMap.Value = entries["Owner"].Type.AsMap.Value
		auto.Modifier = types.StorageFunctionModifierV0{IsOptional: true}
		auto.Fallback = types.Bytes{0}
		put(auto)
	}
}

func nativeRecipientLayoutTestRecords(t *testing.T, profile *historicalReplayObservationProfile, metadata *types.Metadata, mode string) []historicalReplayObservation {
	t.Helper()
	layoutMode := nativeRecipientStorageLayoutSchema
	profile.RecipientLayout = &layoutMode
	layout, err := newNativeRecipientStorageLayout(profile, metadata, 25)
	if err != nil {
		t.Fatal(err)
	}
	key := func(name, account string) string {
		value, err := layout.key(name, account)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	account := func(b byte) string { return nativeExecutionTestHex(bytes.Repeat([]byte{b}, 32)) }
	fields := func(hotkey, coldkey byte, gross uint64) []nativeExecutionTestField {
		value := []nativeExecutionTestField{{name: "netuid", raw: []byte{25, 0}}, {name: "hotkey", raw: bytes.Repeat([]byte{hotkey}, 32)}, {name: "gross", raw: nativeExecutionTestWords(gross)}, {name: "subnet-owner", raw: bytes.Repeat([]byte{0x55}, 32)}, {name: "owner-hotkeys", raw: bytes.Repeat([]byte{0x22}, 32)}}
		if coldkey != 0 {
			value = append(value, nativeExecutionTestField{name: "coldkey", raw: bytes.Repeat([]byte{coldkey}, 32)})
		}
		return value
	}
	record := func(purpose, operation string, function uint32, values []nativeExecutionTestField) historicalReplayObservation {
		rule, value := nativeExecutionTestRecord(purpose, function, values)
		leaf := function + 100
		if operation == "set" {
			leaf += 100
		}
		rule.StorageCall = &historicalStorageCall{Operation: operation, Path: []historicalStorageCallsite{{FunctionIndex: leaf, FunctionBodySha256: historicalReplayDigest{byte(leaf)}, OffsetStart: 0, OffsetEnd: 4}, {FunctionIndex: function, FunctionBodySha256: rule.FunctionBodySha256, OffsetStart: 0, OffsetEnd: 4}}}
		rule.RecipientOwner = purpose == "native-owner-recycle"
		value.Stack = append([]historicalReplayFrame{{FunctionIndex: leaf, FunctionOffset: 1}}, value.Stack...)
		value.Operation = operation
		profile.Rules = append(profile.Rules, rule)
		return value
	}
	context := record("native-recipient-owner-hotkey", "get", 20, []nativeExecutionTestField{{name: "netuid", raw: []byte{25, 0}}})
	context.KeyHex = key("SubnetOwnerHotkey", "")
	context.StorageReturn = &historicalStorageReturn{Present: false}
	records := []historicalReplayObservation{context}
	owner := record("native-recipient-owner", "get", 21, fields(0x11, 0, 9))
	owner.KeyHex = key("Owner", account(0x11))
	coldkey := account(0x33)
	owner.StorageReturn = &historicalStorageReturn{Present: true, ValueHex: &coldkey}
	records = append(records, owner)
	pair := func(purpose string, function uint32, hotkey, coldkey byte, gross, from, to uint64) []historicalReplayObservation {
		base := fields(hotkey, coldkey, gross)
		if purpose != "native-owner-recycle" {
			base = append(base, nativeExecutionTestField{name: "stake-destination", raw: bytes.Repeat([]byte{hotkey}, 32)})
		}
		if purpose == "native-miner-credit" {
			base = append(base, nativeExecutionTestField{name: "liquid", raw: nativeExecutionTestWords(to - from)})
		}
		before := record(purpose, "get", function, base)
		afterFields := append([]nativeExecutionTestField(nil), base...)
		if purpose != "native-owner-recycle" {
			afterFields = append(afterFields, nativeExecutionTestField{name: "pool-after", raw: nativeExecutionTestWords(to)})
		}
		after := record(purpose, "set", function, afterFields)
		pool := key("TotalHotkeyAlpha", account(hotkey))
		if purpose == "native-owner-recycle" {
			pool = key("SubnetAlphaOut", "")
			owner := account(0x34)
			for _, value := range []*historicalReplayObservation{&before, &after} {
				state := []historicalExecutionStateValue{{KeyHex: key("Owner", account(hotkey)), ValueHex: &owner}}
				value.Native.ExecutionState = &state
			}
		}
		before.KeyHex, after.KeyHex = pool, pool
		old, next := nativeExecutionTestHex(nativeExecutionTestWords(from)), nativeExecutionTestHex(nativeExecutionTestWords(to))
		before.StorageReturn = &historicalStorageReturn{Present: true, ValueHex: &old}
		after.ValueHex = &next
		return []historicalReplayObservation{before, after}
	}
	captured := uint64(3)
	if mode == "full" {
		captured = 9
	} else if mode == "liquid" {
		captured = 0
	}
	if captured != 0 {
		records = append(records, pair("native-miner-capture", 22, 0x11, 0x33, 9, 100, 100+captured)...)
	}
	if captured != 9 {
		auto := record("native-recipient-auto-stake", "get", 23, append(fields(0x11, 0x33, 9), nativeExecutionTestField{name: "liquid", raw: nativeExecutionTestWords(9 - captured)}))
		auto.KeyHex = key("AutoStakeDestination", account(0x33))
		auto.StorageReturn = &historicalStorageReturn{Present: false}
		records = append(records, auto)
		records = append(records, pair("native-miner-credit", 24, 0x11, 0x33, 9, 100+captured, 109)...)
	}
	records = append(records, pair("native-owner-recycle", 25, 0x22, 0, 89, 1000, 911)...)
	for index := range records {
		records[index].Ordinal = uint64(index + 1)
	}
	return records
}

// Re-sign only synthetic fixture authority after faults are injected, so a
// wrong original cause cannot pass a test merely by tripping a stale hash.
func nativeRecipientLayoutTestConfigure(t *testing.T, mode string, fault func([]historicalReplayObservation)) *economicEmissionFixture {
	t.Helper()
	source := newEconomicEmissionFixture(t)
	metadata, metadataHex, metadataHash := economicEmissionTestMetadata(t, nativeRecipientLayoutTestMetadata)
	source.chain.metadata, source.chain.metadataHex = metadata, metadataHex
	source.policy.Runtime.RuntimeMetadataHash = metadataHash
	source.chain.profile.RuntimeMetadataHash = metadataHash
	nativeExecutionTestConfigure(t, source, nil)
	seed := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x21}, ed25519.SeedSize))
	for number := uint64(101); number <= 102; number++ {
		base := filepath.Join(source.policy.Execution.Directory, strings.TrimPrefix(source.chain.byHeight[number], "0x"))
		jobRaw, err := os.ReadFile(base + ".job")
		if err != nil {
			t.Fatal(err)
		}
		var job historicalReplayJob
		if err := json.Unmarshal(jobRaw, &job); err != nil {
			t.Fatal(err)
		}
		trace := historicalNativeExecutionTestTrace(job)
		job.ObservationProfile.Rules = job.ObservationProfile.Rules[:3]
		records := nativeRecipientLayoutTestRecords(t, job.ObservationProfile, metadata, mode)
		if fault != nil {
			fault(records)
		}
		if number == 101 {
			trace.Observations = append(trace.Observations[:5], records...)
			for index := range trace.Observations {
				trace.Observations[index].Ordinal = uint64(index + 1)
			}
			trace.HostCalls = uint64(len(trace.Observations))
		}
		profileRaw, err := json.Marshal(job.ObservationProfile)
		if err != nil {
			t.Fatal(err)
		}
		trace.ProfileSha256 = historicalReplayDigest(sha256.Sum256(profileRaw))
		traceRaw, err := json.Marshal(trace)
		if err != nil {
			t.Fatal(err)
		}
		job.ProofNodesHex = []string{nativeExecutionTestHex(traceRaw)}
		jobRaw, err = json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(base+".job", jobRaw, 0600); err != nil {
			t.Fatal(err)
		}
		admissionRaw, err := os.ReadFile(base + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var admission nativeExecutionAdmission
		if err := json.Unmarshal(admissionRaw, &admission); err != nil {
			t.Fatal(err)
		}
		admission.ProfileSha256 = monitorReadDigest(profileRaw)
		admission.Job.Sha256 = monitorReadDigest(jobRaw)
		message, err := admission.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		admission.Signature = hex.EncodeToString(ed25519.Sign(seed, message))
		admissionRaw, err = json.Marshal(admission)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(base+".json", admissionRaw, 0600); err != nil {
			t.Fatal(err)
		}
		source.policy.Execution.ProfileSha256 = admission.ProfileSha256
	}
	return source
}

func TestEconomicNativeRecipientStoragePairsPublicSplitCreditAndRecycle(t *testing.T) {
	source := nativeRecipientLayoutTestConfigure(t, "split", nil)
	result, code, issue := nativeExecutionTestCommand(t, source)
	if code != 0 || result.ExecutionWindow == nil || result.ProviderEntitlementAlpha == nil || *result.ProviderEntitlementAlpha != "9" || result.OwnerRecycledAlpha == nil || *result.OwnerRecycledAlpha != "89" {
		t.Fatal("original split recipient causes failed public derivation", code, issue, result)
	}
	outcome := result.Blocks[0].ExecutionOutcome
	if outcome == nil || outcome.CollateralCapture != "3" || len(outcome.RecipientInputs) != 2 || len(outcome.RecipientInputs[0].Components) != 2 || outcome.RecipientInputs[0].OwnerOrdinal == nil || outcome.RecipientInputs[0].AutoStakeOrdinal == nil || outcome.RecipientEffects.Effects[0].Liquid != "6" {
		t.Fatal("split originals lost cause or conservation", outcome)
	}
}

func TestEconomicNativeRecipientStoragePairsPublicFullyCapturedHasNoLiquidCall(t *testing.T) {
	source := nativeRecipientLayoutTestConfigure(t, "full", nil)
	result, code, issue := nativeExecutionTestCommand(t, source)
	if code != 0 || result.ExecutionWindow == nil || len(result.Blocks) == 0 || result.Blocks[0].ExecutionOutcome == nil {
		t.Fatal(code, issue, result)
	}
	outcome := result.Blocks[0].ExecutionOutcome
	if outcome.ProviderEntitlement != "9" || outcome.CollateralCapture != "9" || outcome.RecipientEffects.Effects[0].Liquid != "0" || len(outcome.RecipientInputs[0].Components) != 1 || outcome.RecipientInputs[0].Components[0].Purpose != "native-miner-capture" || outcome.RecipientInputs[0].AutoStakeOrdinal != nil {
		t.Fatal("fully captured credit invented liquid evidence", outcome)
	}
}

func TestEconomicNativeRecipientStoragePairsPublicLiquidOnlyHasNoCaptureCall(t *testing.T) {
	source := nativeRecipientLayoutTestConfigure(t, "liquid", nil)
	result, code, issue := nativeExecutionTestCommand(t, source)
	if code != 0 || result.ExecutionWindow == nil || len(result.Blocks) == 0 || result.Blocks[0].ExecutionOutcome == nil {
		t.Fatal(code, issue, result)
	}
	outcome := result.Blocks[0].ExecutionOutcome
	if outcome.ProviderEntitlement != "9" || outcome.CollateralCapture != "0" || outcome.RecipientEffects.Effects[0].Liquid != "9" || len(outcome.RecipientInputs[0].Components) != 1 || outcome.RecipientInputs[0].Components[0].Purpose != "native-miner-credit" {
		t.Fatal("liquid-only original borrowed a collateral call", outcome)
	}
}

func TestEconomicNativeRecipientStoragePairsResealedWrongCausesRefuse(t *testing.T) {
	for _, fault := range []struct {
		name   string
		mutate func([]historicalReplayObservation)
	}{
		{name: "coldkey", mutate: func(records []historicalReplayObservation) {
			for i := range records[3].Native.Memory {
				if records[3].Native.Memory[i].Name == "coldkey" {
					records[3].Native.Memory[i].BytesHex = nativeExecutionTestHex(bytes.Repeat([]byte{0x77}, 32))
				}
			}
		}},
		{name: "pool-key", mutate: func(records []historicalReplayObservation) { records[3].KeyHex = records[8].KeyHex }},
		{name: "pool-after", mutate: func(records []historicalReplayObservation) {
			value := nativeExecutionTestHex(nativeExecutionTestWords(104))
			records[3].ValueHex = &value
		}},
		{name: "owner-omitted", mutate: func(records []historicalReplayObservation) { records[8].Native.ExecutionState = nil }},
		{name: "owner-substituted", mutate: func(records []historicalReplayObservation) {
			value := nativeExecutionTestHex(bytes.Repeat([]byte{0x66}, 32))
			(*records[8].Native.ExecutionState)[0].ValueHex = &value
		}},
		{name: "call-path", mutate: func(records []historicalReplayObservation) { records[3].Stack[0].FunctionOffset = 4 }},
	} {
		source := nativeRecipientLayoutTestConfigure(t, "split", fault.mutate)
		result, code, issue := nativeExecutionTestCommand(t, source)
		if code == 0 || result.ExecutionWindow != nil || result.NativeMinerAllocationAlpha != nil {
			t.Fatal("resealed wrong original cause became an amount", fault.name, code, issue, result)
		}
	}
}

func nativeRecipientLayoutTestDecoder(t *testing.T, mode string) (*nativeRecipientStorageLayout, []historicalReplayObservation) {
	t.Helper()
	metadata, _, _ := economicEmissionTestMetadata(t, nativeRecipientLayoutTestMetadata)
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema}
	records := nativeRecipientLayoutTestRecords(t, profile, metadata, mode)
	layout, err := newNativeRecipientStorageLayout(profile, metadata, 25)
	if err != nil {
		t.Fatal(err)
	}
	return layout, records
}

func TestNativeRecipientStoragePairsIncompleteOrReorderedComponentsRefuse(t *testing.T) {
	for _, kind := range []string{"missing-set", "missing-liquid", "repeated-capture", "crossed-cause", "missing-owner", "auto-wrong-owner", "recycle-increase"} {
		layout, records := nativeRecipientLayoutTestDecoder(t, "split")
		switch kind {
		case "missing-set":
			records = records[:3]
		case "missing-liquid":
			records = append(records[:4], records[7:]...)
		case "repeated-capture":
			records = append(append(records[:4:4], records[2:4]...), records[4:]...)
		case "crossed-cause":
			records[3], records[5] = records[5], records[3]
		case "missing-owner":
			records = append(records[:1:1], records[2:]...)
		case "auto-wrong-owner":
			records[4].KeyHex = records[0].KeyHex
		case "recycle-increase":
			value := nativeExecutionTestHex(nativeExecutionTestWords(1001))
			records[8].ValueHex = &value
		}
		for index := range records {
			records[index].Ordinal = uint64(index + 1)
		}
		if result, err := layout.decode(records); err == nil || result != nil {
			t.Fatal("incomplete original sequence produced recipients", kind, result, err)
		}
	}
}

func TestNativeRecipientStoragePairsForeignContextCannotHideWrongPhaseOrNamespace(t *testing.T) {
	for _, kind := range []string{"phase", "namespace"} {
		layout, records := nativeRecipientLayoutTestDecoder(t, "split")
		record := records[0]
		raw, err := historicalReplayHex(record.KeyHex, 34)
		if err != nil {
			t.Fatal(err)
		}
		raw[32] = 26
		if kind == "phase" {
			phase := "0x01"
			record.Native.ExecutionPhaseHex = &phase
		} else {
			raw[0] ^= 1
		}
		record.KeyHex = nativeExecutionTestHex(raw)
		if _, err := layout.netuidFor(record); err == nil {
			t.Fatal("foreign subnet hid invalid original context", kind)
		}
	}
}

func TestNativeRecipientStoragePairsAuthenticatedAbsenceIsNotMissingRead(t *testing.T) {
	layout, records := nativeRecipientLayoutTestDecoder(t, "full")
	records[2].StorageReturn = &historicalStorageReturn{Present: false}
	value := nativeExecutionTestHex(nativeExecutionTestWords(9))
	records[3].ValueHex = &value
	for i := range records[3].Native.Memory {
		if records[3].Native.Memory[i].Name == "pool-after" {
			records[3].Native.Memory[i].BytesHex = value
		}
	}
	result, err := layout.decode(records)
	if err != nil || len(result) != 2 || result[0].captured != 9 {
		t.Fatal("explicit absent pool lost authenticated zero default", result, err)
	}
	records[2].StorageReturn = nil
	if result, err := layout.decode(records); err == nil || result != nil {
		t.Fatal("missing pool observation borrowed a default", result, err)
	}
}

func TestNativeRecipientStoragePairsZeroEntitlementRetainsOnlyOriginalOwnerGet(t *testing.T) {
	layout, records := nativeRecipientLayoutTestDecoder(t, "full")
	records = records[:2]
	for i := range records[1].Native.Memory {
		if records[1].Native.Memory[i].Name == "gross" {
			records[1].Native.Memory[i].BytesHex = nativeExecutionTestHex(nativeExecutionTestWords(0))
		}
	}
	result, err := layout.decode(records)
	if err != nil || len(result) != 1 || result[0].gross != 0 || result[0].captured != 0 || result[0].liquid != 0 || result[0].record.Operation != "get" || !result[0].provenance.ZeroEntitlement || len(result[0].provenance.Components) != 0 || result[0].provenance.OwnerOrdinal == nil || *result[0].provenance.OwnerOrdinal != records[1].Ordinal {
		t.Fatal("zero entitlement invented a credit mutation", result, err)
	}
	records[1].StorageReturn = &historicalStorageReturn{Present: false}
	if result, err := layout.decode(records); err == nil || result != nil {
		t.Fatal("absent ordinary Owner became zero-entitlement custody", result, err)
	}
}

func TestNativeRecipientStoragePairsRejectCapturedMemorySubstitution(t *testing.T) {
	layout, records := nativeRecipientLayoutTestDecoder(t, "full")
	records[3].Native.Memory = append(records[3].Native.Memory, historicalNativeMemory{Name: "captured", BytesHex: nativeExecutionTestHex(nativeExecutionTestWords(9))})
	if result, err := layout.decode(records); err == nil || result != nil {
		t.Fatal("typed pool delta accepted fabricated captured memory", result, err)
	}
}

func TestNativeRecipientStoragePairsLegacyNilWireRemainsUnchanged(t *testing.T) {
	source := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, source, nil)
	result, code, issue := nativeExecutionTestCommand(t, source)
	if code != 0 || result.ExecutionWindow == nil || result.ProviderEntitlementAlpha == nil || *result.ProviderEntitlementAlpha != "9" || result.OwnerRecycledAlpha == nil || *result.OwnerRecycledAlpha != "89" {
		t.Fatal(code, issue, result)
	}
	outcome := result.Blocks[0].ExecutionOutcome
	raw, err := json.Marshal(outcome)
	if err != nil || len(outcome.RecipientInputs) != 0 || bytes.Contains(raw, []byte("original_recipient_inputs")) {
		t.Fatal("nil recipient layout changed historical wire", string(raw), err)
	}
}

func TestNativeTreasuryStoragePairsSplitFullAndZeroKeepOriginalCustody(t *testing.T) {
	// This exercises the inner custody contract. Public signature admission is
	// exercised separately; no unsigned policy is granted execution authority.
	authority := &nativeTreasuryAuthority{Policy: validator.TreasuryPolicy{MultisigAccount: nativeTreasuryTestAccount(0x33), Recipients: []validator.TreasuryRecipient{{Uid: 0, Hotkey: nativeTreasuryTestAccount(0x11), RegistrationBlock: 20}}}}
	for _, mode := range []string{"split", "full", "zero"} {
		fixtureMode := mode
		if mode == "zero" {
			fixtureMode = "full"
		}
		layout, records := nativeRecipientLayoutTestDecoder(t, fixtureMode)
		if mode == "zero" {
			records = records[:2]
			for index := range records[1].Native.Memory {
				if records[1].Native.Memory[index].Name == "gross" {
					records[1].Native.Memory[index].BytesHex = nativeExecutionTestHex(nativeExecutionTestWords(0))
				}
			}
		}
		inputs, err := layout.decode(records)
		if err != nil || len(inputs) == 0 {
			t.Fatal(mode, inputs, err)
		}
		input := inputs[0]
		effect := nativeExecutionEffect{Ordinal: input.record.Ordinal, Recipient: nativeExecutionRecipient{Uid: 0, Hotkey: input.hotkey, Coldkey: input.coldkey, Registered: 20}, Branch: input.branch, Gross: fmt.Sprint(input.gross), Liquid: fmt.Sprint(input.liquid), Collateral: fmt.Sprint(input.captured), Recycled: "0"}
		custody, err := deriveNativeStorageTreasuryRecipient(authority, input, effect)
		if err != nil || custody == nil || custody.SubnetOwnerHotkey != nil || len(custody.OwnerHotkeys) != 1 || (custody.StakeDestination != nil) != (input.liquid != 0) || custody.AutoStakeDestination != nil {
			t.Fatal("original custody differs for split/full/zero", mode, custody, err)
		}
		if mode == "zero" && (!input.provenance.ZeroEntitlement || input.record.Operation != "get") {
			t.Fatal("zero treasury custody invented a mutation", input)
		}
		input.ownerHotkeyObserved = false
		if value, err := deriveNativeStorageTreasuryRecipient(authority, input, effect); err == nil || value != nil {
			t.Fatal("missing original owner option was treated as None", mode, value, err)
		}
	}
}

func TestNativeTreasuryStorageReaderEnrollmentRequiresCompleteOriginalBranches(t *testing.T) {
	layout, _ := nativeRecipientLayoutTestDecoder(t, "split")
	profile := layout.profile
	epochMode, host := nativeEpochStorageLayoutSchema, "ext_allocator_malloc_version_1"
	profile.EpochLayout = &epochMode
	profile.Rules = append(profile.Rules,
		historicalReplayHookRule{Purpose: "native-epoch", HostSnapshot: &host},
		historicalReplayHookRule{Purpose: "native-epoch-index"},
	)
	authority := nativeProducerAuthority{Treasury: &nativeTreasuryAuthority{}, Profile: profile}
	if err := validateNativeTreasuryProfile(authority); err != nil {
		t.Fatal("original epoch/recipient storage reader failed enrollment", err)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"counter", "zero-owner", "capture-set", "owner-census", "legacy-epoch-memory"} {
		var changed historicalReplayObservationProfile
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		for index := range changed.Rules {
			rule := &changed.Rules[index]
			remove := missing == "counter" && rule.Purpose == "native-epoch-index" || missing == "zero-owner" && rule.Purpose == "native-recipient-owner" || missing == "capture-set" && rule.Purpose == "native-miner-capture" && rule.StorageCall.Operation == "set"
			if remove {
				changed.Rules = append(changed.Rules[:index], changed.Rules[index+1:]...)
				break
			}
			if missing == "owner-census" && rule.Purpose == "native-miner-capture" && rule.StorageCall.Operation == "set" {
				for field := range rule.Memory {
					if rule.Memory[field].Name == "owner-hotkeys" {
						rule.Memory = append(rule.Memory[:field], rule.Memory[field+1:]...)
						break
					}
				}
				break
			}
			if missing == "legacy-epoch-memory" && rule.Purpose == "native-epoch" {
				rule.Memory = []historicalNativeCapture{{Name: "subnet-epoch", Bytes: 8}}
				break
			}
		}
		authority.Profile = &changed
		if err := validateNativeTreasuryProfile(authority); err == nil {
			t.Fatal("incomplete or substituted treasury reader enrolled", missing)
		}
	}
}
