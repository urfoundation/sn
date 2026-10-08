// The public observer fixture is an explicitly synthetic protocol peer. These
// tests exercise original-value joins and refusal causes, not runtime admission.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func nativeEpochLayoutTestRecords(t *testing.T, source *economicEmissionFixture, profile *historicalReplayObservationProfile, drains []historicalReplayObservation, epoch *historicalReplayObservation) []historicalReplayObservation {
	t.Helper()
	mode := nativeEpochStorageLayoutSchema
	profile.EpochLayout = &mode
	keys := []string{}
	values := []historicalExecutionStateValue{}
	for _, entry := range []struct {
		name  string
		value uint64
	}{{"NetworkRegisteredAt", *source.policy.SubnetRegistrationBlock}, {"RegisteredSubnetCounter", *source.policy.SubnetGeneration}} {
		key, err := types.CreateStorageKey(source.chain.metadata, "SubtensorModule", entry.name, []byte{25, 0})
		if err != nil {
			t.Fatal(err)
		}
		value := nativeExecutionTestHex(nativeExecutionTestWords(entry.value))
		keys = append(keys, key.Hex())
		values = append(values, historicalExecutionStateValue{KeyHex: key.Hex(), ValueHex: &value})
	}
	profile.Rules[0].Memory = nil
	profile.Rules[0].StateReads = keys
	for index := range drains {
		copy := *drains[index].Native
		copy.Memory = []historicalNativeMemory{}
		pair := append([]historicalExecutionStateValue(nil), values...)
		copy.ExecutionState = &pair
		drains[index].Native = &copy
	}
	oldFields := profile.Rules[1].Memory
	oldMemory := epoch.Native.Memory
	profile.Rules[1].Memory = nil
	epoch.Native.Memory = []historicalNativeMemory{}
	for index, field := range oldFields {
		switch field.Name {
		case "total-alpha", "hotkeys", "uids", "subnet-epoch":
			continue
		}
		profile.Rules[1].Memory = append(profile.Rules[1].Memory, field)
		epoch.Native.Memory = append(epoch.Native.Memory, oldMemory[index])
	}
	for index, name := range []string{"mechanism-count-present", "mechanism-count"} {
		address := uint32(6000 + index)
		profile.Rules[1].Memory = append(profile.Rules[1].Memory, historicalNativeCapture{Name: name, Address: address, Bytes: 1})
		epoch.Native.Memory = append(epoch.Native.Memory, historicalNativeMemory{Name: name, Address: address, BytesHex: "0x01"})
	}
	host := "ext_allocator_malloc_version_1"
	profile.Rules[1].HostSnapshot = &host
	epoch.Operation, epoch.KeyHex, epoch.ValueHex = "host", nativeExecutionTestHex([]byte(host)), nil
	epochRule, write := nativeExecutionTestRecord("native-epoch-index", 7, nil)
	key, err := types.CreateStorageKey(source.chain.metadata, "SubtensorModule", "SubnetEpochIndex", []byte{25, 0})
	if err != nil {
		t.Fatal(err)
	}
	value := nativeExecutionTestHex(nativeExecutionTestWords(8))
	write.Operation, write.KeyHex, write.ValueHex = "set", key.Hex(), &value
	uidRule, _ := nativeExecutionTestRecord("native-uid-census", 6, nil)
	profile.Rules = append(profile.Rules, epochRule, uidRule)
	result := []historicalReplayObservation{write}
	// Deliberately reverse UID order. Identity trie iteration is not a claim
	// that hotkey order or the trace order is the output's dense UID order.
	for _, uid := range []uint16{1, 0} {
		key, err := types.CreateStorageKey(source.chain.metadata, "SubtensorModule", "Keys", []byte{25, 0}, binary.LittleEndian.AppendUint16(nil, uid))
		if err != nil {
			t.Fatal(err)
		}
		_, read := nativeExecutionTestRecord("native-uid-census", 6, nil)
		value := nativeExecutionTestHex(bytes.Repeat([]byte{byte(0x11 + 0x11*uid)}, 32))
		read.Operation, read.KeyHex, read.StorageReturn = "get", key.Hex(), &historicalStorageReturn{Present: true, ValueHex: &value}
		result = append(result, read)
	}
	return result
}

func nativeEpochLayoutTestOriginal(t *testing.T, source *economicEmissionFixture) (historicalReplayJob, *historicalReplayReport) {
	t.Helper()
	path := filepath.Join(source.policy.Execution.Directory, strings.TrimPrefix(source.chain.byHeight[101], "0x")+".job")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	return job, &historicalReplayReport{HookObservations: historicalNativeExecutionTestTrace(job)}
}

func TestEconomicNativeEpochStorageJoinPreservesOriginalAmountsAndProvenance(t *testing.T) {
	source := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, source, nil, true)
	result, code, issue := nativeExecutionTestCommand(t, source)
	if code != 0 || result.ExecutionWindow == nil || len(result.Blocks) != 2 || result.Blocks[0].ExecutionOutcome == nil {
		t.Fatal("valid storage-joined original execution refused", code, issue, result)
	}
	value := result.Blocks[0].ExecutionOutcome
	if value.ProviderEntitlement != "9" || value.OwnerRecycled != "89" || value.MinerAllocation != "100" || value.EpochInputs == nil || value.EpochInputs.TotalAlpha != "200" || !reflect.DeepEqual(value.EpochInputs.DrainOrdinals, []uint64{1, 2, 3}) || value.EpochInputs.EpochWriteOrdinal != 4 || !reflect.DeepEqual(value.EpochInputs.UidReadOrdinals, []uint64{6, 5}) || value.EpochInputs.EpochObservationOrdinal != 7 {
		t.Fatal("original tranche, UID order or provenance changed", value)
	}
	if value.Recipients[0].Uid != 0 || value.Recipients[0].Hotkey != nativeExecutionTestHex(bytes.Repeat([]byte{0x11}, 32)) || value.Recipients[1].Uid != 1 || result.ActivationReady || result.ActualNativeOutcomeVerified {
		t.Fatal("actual get join lost identity or invented authority", value, result)
	}
	job, report := nativeEpochLayoutTestOriginal(t, source)
	authority := &nativeProducerAuthority{Providers: []nativeProducerProvider{{Hotkey: value.Recipients[0].Hotkey, Coldkey: value.Recipients[0].Coldkey}}}
	providers, err := nativeProducerRecipients(source.policy, authority, report, job.ObservationProfile, source.chain.metadata)
	if err != nil || len(providers) != 1 || providers[0] != value.Recipients[0] {
		t.Fatal("producer and admitted replay used different original roster", providers, err, value.Recipients)
	}
	oldHash := value.hash()
	value.EpochInputs.UidReadOrdinals[0]++
	if value.hash() == oldHash {
		t.Fatal("original epoch provenance escaped aggregate binding")
	}
}

func TestEconomicNativeEpochStorageJoinRejectsMissingReorderedAndSubstitutedOriginals(t *testing.T) {
	faults := []struct{ name, want string }{
		{"uid-duplicate", "repeats a registration"}, {"uid-absent", "actual present Keys get"}, {"uid-wrong-hotkey", "aliases an original hotkey"}, {"uid-wrong-namespace", "Keys namespace"}, {"uid-late", "complete and ordered"},
		{"epoch-write-missing", "incomplete or out of order"}, {"epoch-write-early", "drain does not precede"}, {"mechanisms", "single mechanism"},
		{"second-phase", "Initialization phase"}, {"third-phase", "Initialization phase"}, {"state-omitted", "execution-state reads"}, {"registration-absent", "observed present subnet registration"}, {"generation-changed", "original generation differs"}, {"state-key", "selected key"},
	}
	for _, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			source := newEconomicEmissionFixture(t)
			nativeExecutionTestConfigure(t, source, func(number uint64, trace *historicalReplayObservations) {
				if number != 101 {
					return
				}
				switch fault.name {
				case "uid-duplicate":
					trace.Observations[5] = trace.Observations[4]
				case "uid-absent":
					trace.Observations[4].StorageReturn = &historicalStorageReturn{Present: false}
				case "uid-wrong-hotkey":
					copy := *trace.Observations[4].StorageReturn
					copy.ValueHex = trace.Observations[5].StorageReturn.ValueHex
					trace.Observations[4].StorageReturn = &copy
				case "uid-wrong-namespace":
					trace.Observations[4].KeyHex = "0x" + strings.Repeat("00", 36)
				case "uid-late":
					trace.Observations[5], trace.Observations[6] = trace.Observations[6], trace.Observations[5]
				case "epoch-write-missing":
					trace.Observations = append(trace.Observations[:3], trace.Observations[4:]...)
				case "epoch-write-early":
					trace.Observations[2], trace.Observations[3] = trace.Observations[3], trace.Observations[2]
				case "mechanisms":
					for i := range trace.Observations[6].Native.Memory {
						if trace.Observations[6].Native.Memory[i].Name == "mechanism-count" {
							trace.Observations[6].Native.Memory[i].BytesHex = "0x02"
						}
					}
				case "second-phase":
					phase := "0x01"
					trace.Observations[1].Native.ExecutionPhaseHex = &phase
				case "third-phase":
					trace.Observations[2].Native.ExecutionPhaseHex = nil
				case "state-omitted":
					trace.Observations[0].Native.ExecutionState = nil
				case "registration-absent":
					(*trace.Observations[0].Native.ExecutionState)[0].ValueHex = nil
				case "generation-changed":
					value := nativeExecutionTestHex(nativeExecutionTestWords(100))
					(*trace.Observations[0].Native.ExecutionState)[1].ValueHex = &value
				case "state-key":
					(*trace.Observations[0].Native.ExecutionState)[1].KeyHex = "0x12"
				}
			}, true)
			result, code, issue := nativeExecutionTestCommand(t, source)
			if code != 3 || result.ExecutionWindow != nil || result.NativeMinerAllocationAlpha != nil || !strings.Contains(issue, fault.want) {
				t.Fatal("wrong original join acquired authority or failed at unrelated gate", code, issue, fault.want, result)
			}
		})
	}
}

func TestEconomicNativeEpochStorageJoinUsesOnlyAuthenticatedAbsenceDefaults(t *testing.T) {
	source := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, source, nil, true)
	job, report := nativeEpochLayoutTestOriginal(t, source)
	layout, err := newNativeEpochStorageLayout(job.ObservationProfile, source.chain.metadata, 25)
	if err != nil {
		t.Fatal(err)
	}
	record := report.HookObservations.Observations[0]
	policy := source.policy
	zero := uint64(0)
	policy.SubnetGeneration = &zero
	(*record.Native.ExecutionState)[1].ValueHex = nil
	if err := layout.validateGeneration(record, policy); err != nil {
		t.Fatal("explicit observed absence lost metadata default", err)
	}
	record.Native.ExecutionState = nil
	if err := layout.validateGeneration(record, policy); err == nil {
		t.Fatal("missing observation borrowed metadata default")
	}
	// None selects the original count fallback1 and ignores uninitialized
	// Option payload bytes; present0 remains an unsupported real count.
	for i := range report.HookObservations.Observations[6].Native.Memory {
		value := &report.HookObservations.Observations[6].Native.Memory[i]
		if value.Name == "mechanism-count-present" {
			value.BytesHex = "0x00"
		}
		if value.Name == "mechanism-count" {
			value.BytesHex = "0xff"
		}
	}
	for _, record := range report.HookObservations.Observations[3:6] {
		if err := layout.collect(record); err != nil {
			t.Fatal(err)
		}
	}
	drains := []*historicalReplayObservation{&report.HookObservations.Observations[0], &report.HookObservations.Observations[1], &report.HookObservations.Observations[2]}
	if _, _, _, err := layout.roster(report.HookObservations.Observations[6], 2, drains); err != nil {
		t.Fatal("original None fallback was replaced by stale payload", err)
	}
}

func TestHistoricalEpochStorageLayoutRejectsLegacySubstitutionsAndUnboundedStateReads(t *testing.T) {
	source := newEconomicEmissionFixture(t)
	nativeExecutionTestConfigure(t, source, nil, true)
	job, _ := nativeEpochLayoutTestOriginal(t, source)
	raw, err := json.Marshal(job.ObservationProfile)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"mode", "legacy-memory", "missing-host", "three-keys", "duplicate-key", "uppercase", "different-pair", "census-memory"} {
		t.Run(kind, func(t *testing.T) {
			var profile historicalReplayObservationProfile
			if err := json.Unmarshal(raw, &profile); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "mode":
				profile.EpochLayout = nil
			case "legacy-memory":
				profile.Rules[1].Memory = append(profile.Rules[1].Memory, historicalNativeCapture{Name: "hotkeys", Address: 1, Bytes: 32})
			case "missing-host":
				profile.Rules[1].HostSnapshot = nil
			case "three-keys":
				profile.Rules[0].StateReads = append(profile.Rules[0].StateReads, "0x01")
			case "duplicate-key":
				profile.Rules[0].StateReads[1] = profile.Rules[0].StateReads[0]
			case "uppercase":
				profile.Rules[0].StateReads[0] = "0xAA"
			case "different-pair":
				rule := profile.Rules[0]
				rule.StateReads = []string{"0x01", "0x02"}
				profile.Rules = append(profile.Rules, rule)
			case "census-memory":
				profile.Rules[5].Memory = []historicalNativeCapture{{Name: "uids", Address: 1, Bytes: 2}}
			}
			if err := profile.validateEpochLayout(); err == nil {
				t.Fatal("unreviewed state/memory grammar admitted", kind)
			}
		})
	}
}
