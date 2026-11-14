// These tests exercise the actual Go process owner, public RPC command and
// checkpoint worker with a deliberately synthetic protocol peer. Rust's
// original-Wasm tests separately exercise the producer, proof and memory paths.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

func historicalNativeExecutionTestTrace(job historicalReplayJob) *historicalReplayObservations {
	raw, err := historicalReplayHex(job.ProofNodesHex[0], 2*1024*1024)
	if err != nil {
		panic(err)
	}
	var trace historicalReplayObservations
	if err := decodePlanJson(raw, &trace); err != nil {
		panic(err)
	}
	return &trace
}

func nativeExecutionTestHex(raw []byte) string { return "0x" + hex.EncodeToString(raw) }
func nativeExecutionTestWords(values ...uint64) []byte {
	raw := []byte{}
	for _, value := range values {
		raw = binary.LittleEndian.AppendUint64(raw, value)
	}
	return raw
}

type nativeExecutionTestField struct {
	name string
	raw  []byte
}

const nativeTestEpoch = 3
const nativeTestEmission = 4
const nativeTestProvider = 5
const nativeTestOwner = 6

func nativeExecutionTestRecord(purpose string, function uint32, fields []nativeExecutionTestField) (historicalReplayHookRule, historicalReplayObservation) {
	phase := "0x02"
	rule := historicalReplayHookRule{Purpose: purpose, FunctionIndex: function, FunctionBodySha256: historicalReplayDigest{byte(function)}, OffsetStart: 0, OffsetEnd: 4}
	record := historicalReplayObservation{Purpose: purpose, Stack: []historicalReplayFrame{{FunctionIndex: function, FunctionOffset: 1}}, Native: &historicalNativeObservation{ExecutionPhaseHex: &phase, Memory: []historicalNativeMemory{}}}
	for index, field := range fields {
		address := uint32(4000 + 128*index)
		rule.Memory = append(rule.Memory, historicalNativeCapture{Name: field.name, Address: address, Bytes: uint32(len(field.raw))})
		record.Native.Memory = append(record.Native.Memory, historicalNativeMemory{Name: field.name, Address: address, BytesHex: nativeExecutionTestHex(field.raw)})
	}
	return rule, record
}

// Every complete fixture emits and signs the exact original wire frame before
// a fault is injected. Missing or contradictory data cannot become a negative
// test pass at an unrelated setup boundary.
func nativeExecutionTestConfigure(t *testing.T, source *economicEmissionFixture, fault func(uint64, *historicalReplayObservations), storageJoin ...bool) {
	t.Helper()
	request := historicalReplayTestRequest(t, "0xf7")
	root := filepath.Dir(request.Job.Path)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x21}, ed25519.SeedSize))
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema, RuntimeCodeSha256: historicalReplayDigest(sha256.Sum256([]byte{0xf7})), SourceReviewSha256: historicalReplayDigest{31}}
	hotkeys := append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...)
	drainRule, drain := nativeExecutionTestRecord("native-drain", 1, []nativeExecutionTestField{{"netuid", []byte{25, 0}}, {"subnet-registered", nativeExecutionTestWords(*source.policy.SubnetRegistrationBlock)}, {"subnet-generation", nativeExecutionTestWords(*source.policy.SubnetGeneration)}})
	var drains []historicalReplayObservation
	for index, name := range []string{"PendingServerEmission", "PendingValidatorEmission", "PendingRootAlphaDivs"} {
		pendingKey, err := types.CreateStorageKey(source.chain.metadata, "SubtensorModule", name, []byte{25, 0})
		if err != nil {
			t.Fatal(err)
		}
		tranche := nativeExecutionTestHex(nativeExecutionTestWords([]uint64{100, 100, 0}[index]))
		value := drain
		value.Operation, value.KeyHex, value.StorageReturn = "get", pendingKey.Hex(), &historicalStorageReturn{Present: true, ValueHex: &tranche}
		drains = append(drains, value)
	}
	epochRule, epoch := nativeExecutionTestRecord("native-epoch", 2, []nativeExecutionTestField{{"netuid", []byte{25, 0}}, {"total-alpha", nativeExecutionTestWords(200)}, {"incentive-q32", nativeExecutionTestWords(429496729, 3865470567)}, {"dividends-q32", nativeExecutionTestWords(1<<32, 0)}, {"normalized-q32", nativeExecutionTestWords(214748364, 1932735283)}, {"emission", nativeExecutionTestWords(9, 89)}, {"uids", []byte{0, 0, 1, 0}}, {"hotkeys", hotkeys}, {"registered", nativeExecutionTestWords(20, 21)}})
	eventKey, err := types.CreateStorageKey(source.chain.metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	value := nativeExecutionTestHex(economicEmissionTestEvent(t, source.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, append([]byte{8}, nativeExecutionTestWords(9, 89)...)))
	emissionRule, emission := nativeExecutionTestRecord("native-emission", 3, []nativeExecutionTestField{{"netuid", []byte{25, 0}}})
	emission.Operation, emission.KeyHex, emission.ValueHex = "append", eventKey.Hex(), &value
	stored := "0x01"
	epoch.Operation, epoch.KeyHex, epoch.ValueHex = "set", "0x03", &stored
	recipientFields := func(uid byte, gross uint64) []nativeExecutionTestField {
		return []nativeExecutionTestField{{"netuid", []byte{25, 0}}, {"hotkey", hotkeys[int(uid)*32 : (int(uid)+1)*32]}, {"coldkey", bytes.Repeat([]byte{0x33 + uid}, 32)}, {"gross", nativeExecutionTestWords(gross)}}
	}
	providerRule, provider := nativeExecutionTestRecord("native-miner-credit", 4, append(recipientFields(0, 9), nativeExecutionTestField{"captured", nativeExecutionTestWords(3)}, nativeExecutionTestField{"liquid", nativeExecutionTestWords(6)}))
	ownerRule, owner := nativeExecutionTestRecord("native-owner-recycle", 5, append(recipientFields(1, 89), nativeExecutionTestField{"recycled", nativeExecutionTestWords(89)}))
	provider.Operation, provider.KeyHex, provider.ValueHex = "set", "0x01", &stored
	owner.Operation, owner.KeyHex, owner.ValueHex = "set", "0x02", &stored
	profile.Rules = []historicalReplayHookRule{drainRule, epochRule, emissionRule, providerRule, ownerRule}
	var census []historicalReplayObservation
	if len(storageJoin) != 0 && storageJoin[0] {
		census = nativeEpochLayoutTestRecords(t, source, profile, drains, &epoch)
	}
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	source.policy.Runtime.RuntimeSourceCommit = frontierMappingSourceCommit
	codeHash := blake2b.Sum256([]byte{0xf7})
	source.policy.Runtime.RuntimeCodeHash = nativeExecutionTestHex(codeHash[:])
	source.chain.profile = source.policy.Runtime
	source.policy.Execution = &nativeExecutionPolicy{Schema: nativeExecutionPolicySchema, ApprovalPublicKey: nativeExecutionTestHex(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + hex.EncodeToString(profile.SourceReviewSha256[:]), ProfileSha256: monitorReadDigest(profileRaw), Engine: request.Engine, Directory: root}
	source.incentive(t, 101, 25, 9, 89)
	for number := uint64(101); number <= 102; number++ {
		trace := historicalReplayObservations{ProfileSha256: historicalReplayDigest(sha256.Sum256(profileRaw)), SourceReviewSha256: profile.SourceReviewSha256, Authority: "caller-supplied-unapproved-callsite-profile", OriginalFunctionBodiesPreserved: true, Observations: []historicalReplayObservation{}}
		if number == 101 {
			trace.Observations = append(append(append([]historicalReplayObservation{}, drains...), census...), epoch, emission, provider, owner)
			trace.HostCalls = uint64(len(trace.Observations))
			for index := range trace.Observations {
				trace.Observations[index].Ordinal = uint64(index + 1)
			}
		}
		if fault != nil {
			fault(number, &trace)
		}
		for index := range trace.Observations {
			trace.Observations[index].Ordinal = uint64(index + 1)
		}
		trace.HostCalls = max(trace.HostCalls, uint64(len(trace.Observations)))
		traceRaw, err := json.Marshal(trace)
		if err != nil {
			t.Fatal(err)
		}
		job := historicalReplayJob{Schema: historicalReplaySchema, ParentHeaderHex: "0x00", ChildHeaderHex: "0x00", RuntimeCodeHex: "0xf7", RuntimeCodeSha256: profile.RuntimeCodeSha256, RuntimeCodeBlake2b256: historicalReplayDigest(codeHash), ExecutionStateVersion: 1, ObservationProfile: profile, ProofNodesHex: []string{nativeExecutionTestHex(traceRaw)}, ExtrinsicsHex: source.chain.bodies[source.chain.byHeight[number]]}
		for _, entry := range []struct {
			value  string
			target *historicalReplayDigest
		}{{source.chain.byHeight[number-1], &job.ParentHash}, {source.chain.byHeight[number], &job.ChildHash}} {
			raw, err := rootReceiptHex(entry.value, 32)
			if err != nil {
				t.Fatal(err)
			}
			copy(entry.target[:], raw)
		}
		jobRaw, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		jobPath := filepath.Join(root, strings.TrimPrefix(source.chain.byHeight[number], "0x")+".job")
		if err := os.WriteFile(jobPath, jobRaw, 0600); err != nil {
			t.Fatal(err)
		}
		admission := nativeExecutionAdmission{Schema: nativeExecutionAdmissionSchema, Network: source.policy.Network, Netuid: 25, Registration: *source.policy.SubnetRegistrationBlock, Generation: *source.policy.SubnetGeneration, Parent: economicEmissionBoundary{Number: number - 1, Hash: source.chain.byHeight[number-1]}, Child: economicEmissionBoundary{Number: number, Hash: source.chain.byHeight[number]}, Runtime: source.policy.Runtime, ReviewSha256: source.policy.Execution.ReviewSha256, ProfileSha256: source.policy.Execution.ProfileSha256, EngineSha256: request.Engine.Sha256, Job: planFileReference{Path: jobPath, Sha256: monitorReadDigest(jobRaw)}, Providers: []nativeExecutionRecipient{{Uid: 0, Hotkey: nativeExecutionTestHex(hotkeys[:32]), Registered: 20, Coldkey: nativeExecutionTestHex(bytes.Repeat([]byte{0x33}, 32))}}, FinalityAuthority: "independently-reviewed-finalized-boundary"}
		message, err := admission.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		admission.Signature = hex.EncodeToString(ed25519.Sign(key, message))
		raw, err := json.Marshal(admission)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, strings.TrimPrefix(admission.Child.Hash, "0x")+".json"), append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.policy.validate(); err != nil {
		t.Fatal("complete native fixture policy refused", err)
	}
}

func nativeExecutionTestCommand(t *testing.T, source *economicEmissionFixture) (economicEmissionObservation, int, string) {
	t.Helper()
	raw, err := json.Marshal(source.policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "native-policy.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(t.Context(), []string{"observe-native-miner-emission", "--policy", path, "--rpc", source.client.url}, &stdout, &stderr)
	var result economicEmissionObservation
	if stdout.Len() != 0 {
		if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	return result, code, stderr.String()
}

func TestEconomicNativeExecutionPublicObserverDerivesOriginalDrainAndRecipients(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, nil)
	result, code, issue := nativeExecutionTestCommand(t, f)
	if code != 0 || result.ExecutionWindow == nil || result.NativeMinerAllocationAlpha == nil || *result.NativeMinerAllocationAlpha != "100" || *result.ProviderEntitlementAlpha != "9" || *result.OwnerRecycledAlpha != "89" {
		t.Fatal("public original execution amounts missing", code, issue, result)
	}
	window := result.ExecutionWindow
	if result.Blocks[0].Before.PendingServerAlpha != "90" || window.ProviderReference != "10" || window.OwnerRecycleReference != "90" || window.FixedPointDust != "2" || window.CollateralCapture != "3" || window.AllocationDifference != "2" {
		t.Fatal("snapshot, capture or rounding replaced actual execution", window)
	}
	if result.ActualNativeOutcomeVerified || result.ActivationReady || result.TargetMet != nil || result.QuantizationToleranceAlpha != nil || result.RuntimeSourceProven {
		t.Fatal("partial runtime quantization became full activation or build authority", result)
	}
}

func TestEconomicNativeExecutionMissingAdmissionLeavesAmountsUnknown(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, nil)
	path := filepath.Join(f.policy.Execution.Directory, strings.TrimPrefix(f.chain.byHeight[101], "0x")+".json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, code, issue := nativeExecutionTestCommand(t, f)
	if code != 3 || result.Complete || result.NativeMinerAllocationAlpha != nil || result.ExecutionWindow != nil || !strings.Contains(issue, "no such file") {
		t.Fatal("missing approval became a zero native outcome", code, issue, result)
	}
}

func TestEconomicNativeExecutionRefusesOmittedReorderedAndForgedCapture(t *testing.T) {
	baseline := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, baseline, nil)
	if result, code, issue := nativeExecutionTestCommand(t, baseline); code != 0 || result.ExecutionWindow == nil {
		t.Fatal("valid public capture baseline failed before fault cases", code, issue)
	}
	for _, fault := range []string{"return", "phase", "order", "drain-repeat", "emission-order", "width", "normalization", "emission", "generation", "owner", "capture", "recycle", "profile"} {
		f := newEconomicEmissionFixture(t)
		nativeExecutionTestConfigure(t, f, func(number uint64, trace *historicalReplayObservations) {
			if number != 101 {
				return
			}
			change := func(record int, name string, raw []byte) {
				for index := range trace.Observations[record].Native.Memory {
					if trace.Observations[record].Native.Memory[index].Name == name {
						trace.Observations[record].Native.Memory[index].BytesHex = nativeExecutionTestHex(raw)
						return
					}
				}
				t.Fatal("missing fixture field", name)
			}
			switch fault {
			case "return":
				trace.Observations[0].StorageReturn = nil
			case "phase":
				trace.Observations[0].Native.ExecutionPhaseHex = nil
			case "order":
				trace.Observations[0], trace.Observations[1] = trace.Observations[1], trace.Observations[0]
			case "drain-repeat":
				trace.Observations[1] = trace.Observations[0]
			case "emission-order":
				trace.Observations[nativeTestEpoch], trace.Observations[nativeTestEmission] = trace.Observations[nativeTestEmission], trace.Observations[nativeTestEpoch]
			case "width":
				change(nativeTestEpoch, "normalized-q32", []byte{1})
			case "normalization":
				change(nativeTestEpoch, "normalized-q32", nativeExecutionTestWords(214748365, 1932735283))
			case "emission":
				change(nativeTestEpoch, "emission", nativeExecutionTestWords(10, 89))
			case "generation":
				change(nativeTestEpoch, "registered", nativeExecutionTestWords(21, 21))
			case "owner":
				change(nativeTestProvider, "coldkey", bytes.Repeat([]byte{0x55}, 32))
			case "capture":
				change(nativeTestProvider, "captured", nativeExecutionTestWords(4))
			case "recycle":
				change(nativeTestOwner, "recycled", nativeExecutionTestWords(88))
			case "profile":
				trace.ProfileSha256[0] ^= 1
			}
		})
		result, code, issue := nativeExecutionTestCommand(t, f)
		if code != 3 || result.NativeMinerAllocationAlpha != nil || result.ExecutionWindow != nil || issue == "" {
			t.Fatal("contradictory original capture acquired economic amounts", fault, code, issue, result)
		}
	}
}

func TestEconomicNativeExecutionZeroIncentiveRetainsPositiveTranche(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, func(number uint64, trace *historicalReplayObservations) {
		if number != 101 {
			return
		}
		trace.Observations = trace.Observations[:nativeTestEmission+1]
		for index := range trace.Observations[nativeTestEpoch].Native.Memory {
			capture := &trace.Observations[nativeTestEpoch].Native.Memory[index]
			if capture.Name == "incentive-q32" || capture.Name == "dividends-q32" || capture.Name == "normalized-q32" || capture.Name == "emission" {
				capture.BytesHex = nativeExecutionTestHex(make([]byte, 16))
			}
		}
		value := nativeExecutionTestHex(economicEmissionTestEvent(t, f.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, append([]byte{8}, make([]byte, 16)...)))
		trace.Observations[nativeTestEmission].ValueHex = &value
	})
	f.incentive(t, 101, 25, 0, 0)
	result, code, issue := nativeExecutionTestCommand(t, f)
	if code != 0 || result.ExecutionWindow == nil || result.ExecutionWindow.MinerAllocation != "100" || result.ExecutionWindow.RedirectedToValidators != "100" || result.ExecutionWindow.FixedPointTolerance != "0" || result.ExecutionWindow.ProviderEntitlement != "0" {
		t.Fatal("fallback erased denominator or became rounding tolerance", code, issue, result)
	}
}

func TestMonitorNativeExecutionPublicRestartRetainsCumulativeAmounts(t *testing.T) {
	f := newMonitorEconomicTestFixture(t, false)
	f.source.set(t, 100, "PendingServerEmission", make([]byte, 8))
	nativeExecutionTestConfigure(t, f.source, nil)
	f.policy.Observation = f.source.policy
	f.policy.Observation.Through = economicEmissionBoundary{Number: 101, Hash: f.source.chain.byHeight[101]}
	f.policy.BatchBlocks = 1
	first := f.start(t, monitorServiceHooks{})
	event := first.next(t)
	if !event.Current || event.State.ExecutionAccounting == nil || event.State.ExecutionAccounting.MinerAllocation != "100" || event.State.NativeMinerAllocationAlpha == nil {
		first.stop(t)
		t.Fatal("public worker omitted admitted original execution", event)
	}
	first.stop(t)
	second := f.start(t, monitorServiceHooks{})
	event = second.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.ExecutionAccounting == nil || event.State.ExecutionAccounting.MinerAllocation != "100" || event.State.ExecutionAccounting.ProviderReference != "10" || event.State.BatchCount != 2 || event.State.ActualNativeOutcomeVerified {
		second.stop(t)
		t.Fatal("restart repeated economic tranche or changed authority", event)
	}
	second.stop(t)
}

func TestNativeExecutionAccountingCarriesTenthsAndRefusesRepeatedWindow(t *testing.T) {
	from := economicEmissionBoundary{Number: 100, Hash: testGenesisHash}
	makeWindow := func(parent economicEmissionBoundary, child string, amount string) nativeExecutionWindow {
		value := nativeExecutionEmpty(parent)
		value.Through = economicEmissionBoundary{Number: parent.Number + 1, Hash: child}
		value.Blocks = 1
		value.MinerAllocation = amount
		value.AllocationDifference = amount
		value.EvidenceChain = rootObjectHash([]string{parent.Hash, child, amount})
		if err := value.references(); err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := makeWindow(from, "0x"+strings.Repeat("11", 32), "9")
	second := makeWindow(first.Through, "0x"+strings.Repeat("22", 32), "1")
	combined, err := appendNativeExecution(&first, economicEmissionObservation{ExecutionWindow: &second}, from)
	if err != nil || first.ProviderReference != "0" || combined.ProviderReference != "1" || combined.OwnerRecycleReference != "9" {
		t.Fatal("cumulative integer policy rounded each interval", combined, err)
	}
	if _, err := appendNativeExecution(combined, economicEmissionObservation{ExecutionWindow: &second}, from); err == nil {
		t.Fatal("original execution counted twice")
	}
}

func nativeExecutionTestChange(t *testing.T, record *historicalReplayObservation, name string, raw []byte) {
	t.Helper()
	for index := range record.Native.Memory {
		if record.Native.Memory[index].Name == name {
			record.Native.Memory[index].BytesHex = nativeExecutionTestHex(raw)
			return
		}
	}
	t.Fatal("missing original fixture capture", name)
}

func TestEconomicNativeExecutionConservingWrongSplitStaysExplicitlyUnapproved(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, func(number uint64, trace *historicalReplayObservations) {
		if number != 101 {
			return
		}
		change := func(record int, name string, values ...uint64) {
			nativeExecutionTestChange(t, &trace.Observations[record], name, nativeExecutionTestWords(values...))
		}
		change(nativeTestEpoch, "incentive-q32", 1<<30, 3<<30)
		change(nativeTestEpoch, "normalized-q32", 1<<29, 3<<29)
		change(nativeTestEpoch, "emission", 25, 75)
		change(nativeTestProvider, "gross", 25)
		change(nativeTestProvider, "liquid", 22)
		change(nativeTestOwner, "gross", 75)
		change(nativeTestOwner, "recycled", 75)
		raw := nativeExecutionTestHex(economicEmissionTestEvent(t, f.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, append([]byte{8}, nativeExecutionTestWords(25, 75)...)))
		trace.Observations[nativeTestEmission].ValueHex = &raw
	})
	f.incentive(t, 101, 25, 25, 75)
	result, code, issue := nativeExecutionTestCommand(t, f)
	if code != 0 || result.ExecutionWindow == nil {
		t.Fatal("valid wrong-split observation was unavailable", code, issue)
	}
	window := result.ExecutionWindow
	if window.ProviderDeviation != "15" || window.OwnerRecycleDeviation != "-15" || window.FixedPointTolerance != "0" || window.AllocationDifference != "0" || window.PolicyConformance != "unresolved-full-runtime-tolerance-and-conservation" || result.TargetMet != nil || result.ActualNativeOutcomeVerified || result.ActivationReady {
		t.Fatal("conservation was promoted into a 10/90 policy approval", result)
	}
}

func TestEconomicNativeExecutionFiltersOtherSubnetAndRetainsNoEmissionBlock(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	foreign := economicEmissionTestEvent(t, f.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{26, 0}, append([]byte{8}, nativeExecutionTestWords(9, 89)...))
	nativeExecutionTestConfigure(t, f, func(number uint64, trace *historicalReplayObservations) {
		if number != 101 {
			return
		}
		raw, err := json.Marshal(trace.Observations)
		if err != nil {
			t.Fatal(err)
		}
		var other []historicalReplayObservation
		if err := json.Unmarshal(raw, &other); err != nil {
			t.Fatal(err)
		}
		for index := range other {
			nativeExecutionTestChange(t, &other[index], "netuid", []byte{26, 0})
			if index < 3 {
				key, err := types.CreateStorageKey(f.chain.metadata, "SubtensorModule", []string{"PendingServerEmission", "PendingValidatorEmission", "PendingRootAlphaDivs"}[index], []byte{26, 0})
				if err != nil {
					t.Fatal(err)
				}
				other[index].KeyHex = key.Hex()
			}
		}
		value := nativeExecutionTestHex(foreign)
		other[nativeTestEmission].ValueHex = &value
		trace.Observations = append(other, trace.Observations...)
	})
	target := economicEmissionTestEvent(t, f.chain.metadata, "IncentiveAlphaEmittedToMiners", []byte{25, 0}, append([]byte{8}, nativeExecutionTestWords(9, 89)...))
	f.set(t, 101, "Events", append(append([]byte{8}, foreign...), target...))
	result, code, issue := nativeExecutionTestCommand(t, f)
	if code != 0 || result.ExecutionWindow == nil || result.ExecutionWindow.Blocks != 2 || result.ExecutionWindow.MinerAllocation != "100" || len(result.Blocks) != 2 || result.Blocks[0].Events[0].EventIndex != 1 || result.Blocks[1].ExecutionOutcome == nil || result.Blocks[1].ExecutionOutcome.MinerAllocation != "0" {
		t.Fatal("other subnet or no-emission block changed target accounting", code, issue, result)
	}
}

func TestEconomicNativeExecutionObservedAbsenceUsesOnlyAuthenticatedDefault(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, func(number uint64, trace *historicalReplayObservations) {
		if number == 101 {
			trace.Observations[2].StorageReturn = &historicalStorageReturn{Present: false}
		}
	})
	result, code, issue := nativeExecutionTestCommand(t, f)
	if code != 0 || result.ExecutionWindow == nil || result.ExecutionWindow.MinerAllocation != "100" {
		t.Fatal("actual absent root tranche lost authenticated zero default", code, issue, result)
	}
}

func TestEconomicNativeExecutionPublicApprovalDriftCannotEnrollAuthority(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, nil)
	path := filepath.Join(f.policy.Execution.Directory, strings.TrimPrefix(f.chain.byHeight[101], "0x")+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(f.policy.Execution.Directory, strings.TrimPrefix(f.chain.byHeight[101], "0x")+".job")
	jobBefore, err := os.ReadFile(jobPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"signer", "runtime", "boundary", "review", "profile", "engine", "provider", "job"} {
		var admission nativeExecutionAdmission
		if err := decodePlanJson(original, &admission); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "signer":
			admission.Signature = strings.Repeat("00", 64)
		case "runtime":
			admission.Runtime.RuntimeVersion.SpecVersion++
		case "boundary":
			admission.Child.Hash = "0x" + strings.Repeat("aa", 32)
		case "review":
			admission.ReviewSha256 = "sha256:" + strings.Repeat("bb", 32)
		case "profile":
			admission.ProfileSha256 = "sha256:" + strings.Repeat("cc", 32)
		case "engine":
			admission.EngineSha256 = "sha256:" + strings.Repeat("dd", 32)
		case "provider":
			admission.Providers[0].Registered++
		case "job":
			admission.Job.Sha256 = "sha256:" + strings.Repeat("ee", 32)
		}
		raw, err := json.Marshal(admission)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		result, code, issue := nativeExecutionTestCommand(t, f)
		if code != 3 || result.ExecutionWindow != nil || result.NativeMinerAllocationAlpha != nil || !strings.Contains(issue, "native execution") {
			t.Fatal("changed independent authority reached native accounting", fault, code, issue)
		}
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	result, code, issue := nativeExecutionTestCommand(t, f)
	if code != 0 || result.ExecutionWindow == nil {
		t.Fatal("original approved input could not continue after refusal", code, issue)
	}
	jobAfter, err := os.ReadFile(jobPath)
	f.chain.stateLock.Lock()
	submitted := f.chain.counts["author_submitExtrinsic"]
	f.chain.stateLock.Unlock()
	if err != nil || !bytes.Equal(jobBefore, jobAfter) || submitted != 0 {
		t.Fatal("read-only native authority gate changed original input or sent an effect", err)
	}
}

func TestEconomicNativeExecutionCanceledAdmissionKeepsAmountsUnknown(t *testing.T) {
	f := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, f, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	block := economicEmissionBlock{Boundary: economicEmissionBoundary{Number: 101, Hash: f.chain.byHeight[101]}}
	result, err := observeNativeExecution(ctx, f.client, f.policy, block, f.policy.Runtime, f.chain.metadata)
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled original execution admission acquired amounts", result, err)
	}
}

func TestNativeExecutionArchiveRetainsExactCumulativePrefix(t *testing.T) {
	from := economicEmissionBoundary{Number: 100, Hash: testGenesisHash}
	prefix := nativeExecutionEmpty(from)
	prefix.Through = economicEmissionBoundary{Number: 101, Hash: "0x" + strings.Repeat("11", 32)}
	prefix.Blocks = 1
	prefix.MinerAllocation = "100"
	prefix.ProviderEntitlement = "9"
	prefix.OwnerRecycled = "89"
	prefix.AllocationDifference = "2"
	prefix.EvidenceChain = rootObjectHash("original executed prefix")
	if err := prefix.references(); err != nil {
		t.Fatal(err)
	}
	retained := prefix
	if err := nativeExecutionRetains(&retained, &prefix); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"same-boundary-chain", "next-boundary-reset", "missing-prefix"} {
		value := prefix
		if fault == "same-boundary-chain" {
			value.EvidenceChain = rootObjectHash("new chain")
		} else {
			value.Through = economicEmissionBoundary{Number: 102, Hash: "0x" + strings.Repeat("22", 32)}
			value.Blocks = 2
			value.MinerAllocation = "0"
			value.ProviderEntitlement = "0"
			value.OwnerRecycled = "0"
			value.AllocationDifference = "0"
		}
		if err := value.references(); err != nil {
			t.Fatal(err)
		}
		prior := &prefix
		if fault == "missing-prefix" {
			prior = nil
		}
		if err := nativeExecutionRetains(&value, prior); err == nil {
			t.Fatal("archived accounting prefix was replaced", fault)
		}
	}
}

// The qualification runner supplies the exact owned Rust image and the job
// exported by historical_native_execution_exports_actual_original_program_for_go_consumer.
// A normal package run without those separately built artifacts cannot claim
// this cross-engine scope; the explicit gate requires this root to PASS.
func TestEconomicNativeExecutionConsumesActualOriginalWasmReplay(t *testing.T) {
	enginePath, fixturePath := os.Getenv("URNETWORK_NATIVE_EXECUTION_ENGINE"), os.Getenv("URNETWORK_NATIVE_EXECUTION_FIXTURE")
	if enginePath == "" && fixturePath == "" {
		t.Skip("requires explicit original-Wasm executable fixture gate")
	}
	_, engineHash, err := readPlanFile(t.Context(), enginePath, historicalReplayEngineLimit)
	if err != nil {
		t.Fatal(err)
	}
	raw, jobHash, err := readPlanFile(t.Context(), fixturePath, historicalReplayJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := decodePlanJson(raw, &job); err != nil {
		t.Fatal(err)
	}
	request := historicalReplayRequest{Engine: planFileReference{Path: enginePath, Sha256: engineHash}, Job: planFileReference{Path: fixturePath, Sha256: jobHash}, Budget: time.Minute}
	report, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
	if err != nil {
		t.Fatal("actual owned original-Wasm replay refused", err)
	}
	f := newEconomicEmissionFixture(t)
	f.policy.Runtime.RuntimeSourceCommit = frontierMappingSourceCommit
	f.policy.Runtime.RuntimeCodeHash = nativeExecutionTestHex(job.RuntimeCodeBlake2b256[:])
	profileRaw, err := json.Marshal(job.ObservationProfile)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize))
	f.policy.From = economicEmissionBoundary{Number: 100, Hash: nativeExecutionTestHex(job.ParentHash[:])}
	f.policy.Through = economicEmissionBoundary{Number: 101, Hash: nativeExecutionTestHex(job.ChildHash[:])}
	f.policy.Execution = &nativeExecutionPolicy{Schema: nativeExecutionPolicySchema, ApprovalPublicKey: nativeExecutionTestHex(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + hex.EncodeToString(job.ObservationProfile.SourceReviewSha256[:]), ProfileSha256: monitorReadDigest(profileRaw), Engine: request.Engine, Directory: filepath.Dir(fixturePath)}
	block := economicEmissionBlock{Boundary: f.policy.Through, Header: rootReceiptHeader{ParentHash: f.policy.From.Hash}, Events: []economicEmissionEvent{{Kind: "SubtensorModule.IncentiveAlphaEmittedToMiners", Netuid: 25, AlphaByUid: []string{"9", "89"}, TotalAlpha: "98"}}}
	admission := nativeExecutionAdmission{Schema: nativeExecutionAdmissionSchema, Network: f.policy.Network, Netuid: 25, Registration: *f.policy.SubnetRegistrationBlock, Generation: *f.policy.SubnetGeneration, Parent: f.policy.From, Child: f.policy.Through, Runtime: f.policy.Runtime, ReviewSha256: f.policy.Execution.ReviewSha256, ProfileSha256: f.policy.Execution.ProfileSha256, EngineSha256: engineHash, Job: request.Job, Providers: []nativeExecutionRecipient{{Uid: 0, Hotkey: nativeExecutionTestHex(bytes.Repeat([]byte{0x11}, 32)), Registered: 20, Coldkey: nativeExecutionTestHex(bytes.Repeat([]byte{0x33}, 32))}}, FinalityAuthority: "independently-reviewed-finalized-boundary"}
	message, err := admission.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	admission.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	if err := f.policy.validate(); err != nil {
		t.Fatal(err)
	}
	if err := admission.validate(f.policy, block, f.policy.Runtime); err != nil {
		t.Fatal(err)
	}
	var drains [3]nativeExecutionDrain
	for index, name := range []string{"PendingServerEmission", "PendingValidatorEmission", "PendingRootAlphaDivs"} {
		storageKey, err := types.CreateStorageKey(f.chain.metadata, "SubtensorModule", name, []byte{25, 0})
		if err != nil {
			t.Fatal(err)
		}
		drains[index] = nativeExecutionDrain{Key: storageKey.Hex(), Fallback: make([]byte, 8)}
	}
	outcome, err := deriveNativeExecution(f.policy, admission, block, job, *report, drains, nil)
	if err != nil || outcome == nil || outcome.MinerAllocation != "100" || outcome.ProviderEntitlement != "9" || outcome.OwnerRecycled != "89" || outcome.CollateralCapture != "3" || outcome.FixedPointDust != "2" || report.RuntimeAdmitted || report.NativeFeeWithdrawalRefund {
		t.Fatal("actual original program did not feed the accounting consumer", outcome, err)
	}
	// Omission after a real positive baseline cannot become a proof of absence.
	report.HookObservations.Observations[0].StorageReturn = nil
	if _, err := deriveNativeExecution(f.policy, admission, block, job, *report, drains, nil); err == nil {
		t.Fatal("caller-omitted actual return acquired original-Wasm amounts")
	}
}
