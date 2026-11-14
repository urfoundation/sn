package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"
)

// This synthetic internal join fixture uses the independently asserted
// two-UID math case. It exercises observation custody, not a real Wasm run.
func nativeYumaEpochJoinTestFixture(t *testing.T, joined bool) (economicEmissionPolicy, nativeExecutionAdmission, *historicalReplayReport, nativeExecutionOutcome) {
	t.Helper()
	input := nativeYumaTestInput()
	authority := &nativeYumaPolicy{Schema: nativeYumaSchema, LayoutSha256: monitorReadDigest([]byte(nativeYumaLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic-yuma-join-review")), MaximumWitnessBytes: 1024 * 1024, HotBlockReserve: 2, MaximumEdges: 16, MaximumOperations: 400000}
	policy := economicEmissionPolicy{Netuid: input.Netuid, Execution: &nativeExecutionPolicy{Yuma: authority}}
	parent := economicEmissionBoundary{Number: 100, Hash: "0x" + strings.Repeat("a", 64)}
	boundary := economicEmissionBoundary{Number: 101, Hash: "0x" + strings.Repeat("b", 64)}
	admission := nativeExecutionAdmission{Yuma: authority, Parent: parent}
	bytesField := func(name string, raw []byte) historicalNativeMemory {
		return historicalNativeMemory{Name: name, BytesHex: "0x" + hex.EncodeToString(raw)}
	}
	integers := func(name string, width int, values ...uint64) historicalNativeMemory {
		raw := make([]byte, 0, width*len(values))
		for _, value := range values {
			var word [8]byte
			binary.LittleEndian.PutUint64(word[:], value)
			raw = append(raw, word[:width]...)
		}
		return bytesField(name, raw)
	}
	phase := "0x02"
	observation := func(purpose string, ordinal uint64, fields ...historicalNativeMemory) historicalReplayObservation {
		return historicalReplayObservation{Ordinal: ordinal, Purpose: purpose, Operation: "host", Native: &historicalNativeObservation{ExecutionPhaseHex: &phase, Memory: append([]historicalNativeMemory{integers("netuid", 2, uint64(input.Netuid))}, fields...)}}
	}
	records := []historicalReplayObservation{
		observation("native-yuma-meta", 10,
			integers("uid-count", 2, uint64(input.Count)), integers("current-block", 8, input.CurrentBlock), integers("tempo", 8, input.Tempo), integers("activity-cutoff", 8, input.ActivityCutoff), integers("last-step", 8, input.LastStep), integers("minimum-stake", 8, input.MinimumStake), integers("owner-uid", 2, uint64(input.OwnerUid)), integers("tao-weight", 8, input.TaoWeight), integers("kappa", 2, uint64(input.Kappa)), integers("bonds-penalty", 2, uint64(input.BondsPenalty)), integers("moving-average", 8, input.MovingAverage)),
		observation("native-yuma-settings", 11,
			integers("yuma3", 1, 0), integers("liquid-alpha", 1, 0), integers("commit-reveal", 1, 0), integers("consensus-mode", 1, 0), integers("alpha-low", 2, uint64(input.AlphaLow)), integers("alpha-high", 2, uint64(input.AlphaHigh)), integers("steepness", 2, 0), integers("previous-consensus", 2)),
	}
	recipients := make([]nativeExecutionRecipient, len(input.Nodes))
	var hotkeys []byte
	var registered []uint64
	for index, node := range input.Nodes {
		raw, err := hex.DecodeString(strings.TrimPrefix(node.Hotkey, "0x"))
		if err != nil {
			t.Fatal(err)
		}
		hotkeys = append(hotkeys, raw...)
		registered = append(registered, node.Registered)
		permit := uint64(0)
		if node.Permit {
			permit = 1
		}
		records = append(records, observation("native-yuma-node", uint64(12+index),
			integers("uid", 2, uint64(node.Uid)), bytesField("hotkey", raw), integers("registered", 8, node.Registered), integers("last-update", 8, node.LastUpdate), integers("permit", 1, permit), integers("alpha", 8, node.Alpha), integers("tao", 8, node.Tao), integers("commit-block", 8, node.CommitBlock),
			integers("parent-proportions", 8), bytesField("parent-hotkeys", nil), integers("parent-alpha", 8), integers("parent-tao", 8), integers("child-proportions", 8), bytesField("child-hotkeys", nil)))
		recipients[index] = nativeExecutionRecipient{Uid: node.Uid, Hotkey: node.Hotkey, Registered: node.Registered, Coldkey: "0x" + strings.Repeat("3", 64)}
	}
	for offset, purpose := range []string{"native-yuma-weights", "native-yuma-bonds"} {
		matrix := input.Weights
		if offset == 1 {
			matrix = input.Bonds
		}
		for index, row := range matrix {
			var columns, values []uint64
			for _, edge := range row {
				columns = append(columns, uint64(edge.Column))
				values = append(values, uint64(edge.Value))
			}
			records = append(records, observation(purpose, uint64(14+offset*2+index), integers("uid", 2, uint64(index)), integers("columns", 2, columns...), integers("values", 2, values...)))
		}
	}
	epoch := observation("native-epoch", 50, integers("registered", 8, registered...))
	// Stage arithmetic is independently asserted in the existing math fixture;
	// here it supplies a coherent nonzero original shape for the new join.
	fixed, err := evaluateNativeYuma(t.Context(), input, 200, false, authority.MaximumOperations)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []struct {
		name     string
		values   []*big.Rat
		fraction uint
	}{
		{name: "stake-q32", values: fixed.stake, fraction: 32},
		{name: "active-stake-q32", values: fixed.active, fraction: 32},
		{name: "consensus-q32", values: fixed.consensus, fraction: 32},
		{name: "incentive-q32", values: fixed.incentive, fraction: 32},
		{name: "dividends-q32", values: fixed.dividend, fraction: 32},
		{name: "normalized-q32", values: fixed.server, fraction: 32},
		{name: "validator-normalized-q32", values: fixed.validator, fraction: 32},
		{name: "emission", values: fixed.serverAlpha},
		{name: "validator-emission", values: fixed.validatorAlpha},
	} {
		values := make([]uint64, len(stage.values))
		for index, value := range stage.values {
			bits := new(big.Rat).Mul(value, new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), stage.fraction)))
			if !bits.IsInt() || !bits.Num().IsUint64() {
				t.Fatal("synthetic original stage is not fixed-width", stage.name, bits)
			}
			values[index] = bits.Num().Uint64()
		}
		epoch.Native.Memory = append(epoch.Native.Memory, integers(stage.name, 8, values...))
	}
	outcome := nativeExecutionOutcome{Boundary: boundary, AdmissionHash: monitorReadDigest([]byte("synthetic-admission")), JobHash: monitorReadDigest([]byte("synthetic-job")), TraceHash: monitorReadDigest([]byte("synthetic-trace")), MinerAllocation: "100", ProviderEntitlement: "9", OwnerRecycled: "89", ResidualEntitlement: "0", CollateralCapture: "0", FixedPointDust: "2", FixedPointTolerance: "2", RedirectedToValidators: "0", AllocationDifference: "0", Recipients: recipients, AmountsAuthenticated: true}
	if joined {
		// UID-indexed trie reads need not occur in numeric UID order.
		outcome.EpochInputs = &nativeEpochInputProvenance{Schema: nativeEpochStorageLayoutSchema, EpochObservationOrdinal: epoch.Ordinal, DrainOrdinals: []uint64{1, 2, 3}, TotalAlpha: "200", UidReadOrdinals: []uint64{6, 5}, EpochWriteOrdinal: 4}
	} else {
		epoch.Native.Memory = append(epoch.Native.Memory, integers("total-alpha", 8, 200), bytesField("hotkeys", hotkeys))
	}
	outcome.ContentHash = outcome.hash()
	eventIndex, emissionOrdinal := uint64(0), uint64(51)
	outcome.RecipientEffects = newNativeExecutionEffects(parent, outcome, &eventIndex, &emissionOrdinal, []nativeExecutionEffect{
		{Ordinal: 52, Recipient: recipients[0], Branch: "native-miner-credit", Provider: true, Gross: "9", Liquid: "9", Collateral: "0", Recycled: "0"},
		{Ordinal: 53, Recipient: recipients[1], Branch: "native-owner-recycle", Gross: "89", Liquid: "0", Collateral: "0", Recycled: "89"},
	})
	report := &historicalReplayReport{HookObservations: &historicalReplayObservations{Observations: append(records, epoch)}}
	return policy, admission, report, outcome
}

func TestNativeYumaStorageJoinedEpochRetainsFullDenominator(t *testing.T) {
	policy, admission, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
	projection, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
	if err != nil || projection == nil || projection.Issue != "" || projection.MinerDenominator == nil || *projection.MinerDenominator != "98" || projection.FullQuantizationTolerance == nil || *projection.FullQuantizationTolerance != "2" || len(projection.Allocations) != 2 || projection.Allocations[0].ActualMiner != "9" || projection.Allocations[1].ActualMiner != "89" {
		t.Fatal("storage join lost the complete miner denominator or original quantization", projection, err)
	}
	if err := projection.validate(t.Context(), policy, outcome); err != nil {
		t.Fatal("retained storage join could not revalidate", err)
	}
	for _, field := range projection.Epoch.Native.Memory {
		if field.Name == "total-alpha" || field.Name == "hotkeys" || field.Name == "uids" || field.Name == "subnet-epoch" {
			t.Fatal("storage join manufactured an original memory field", field.Name)
		}
	}
}

func TestNativeYumaStorageJoinedEpochPreservesLegacyPath(t *testing.T) {
	policy, admission, report, outcome := nativeYumaEpochJoinTestFixture(t, false)
	legacy, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
	if err != nil || legacy == nil || legacy.Issue != "" || legacy.MinerDenominator == nil || *legacy.MinerDenominator != "98" {
		t.Fatal("legacy original memory stopped producing its full denominator", legacy, err)
	}
	if err := legacy.validate(t.Context(), policy, outcome); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(outcome)
	if err != nil || strings.Contains(string(raw), "original_epoch_inputs") {
		t.Fatal("legacy nil join changed the historical wire", string(raw), err)
	}
	policy, admission, report, outcome = nativeYumaEpochJoinTestFixture(t, true)
	joined, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
	if err != nil || joined == nil || joined.Issue != "" || !reflect.DeepEqual(joined.Allocations, legacy.Allocations) || !reflect.DeepEqual(joined.MinerDenominator, legacy.MinerDenominator) || !reflect.DeepEqual(joined.FullQuantizationTolerance, legacy.FullQuantizationTolerance) {
		t.Fatal("storage custody changed unchanged allocation arithmetic", joined, legacy, err)
	}
}

func TestNativeYumaStorageJoinedEpochRefusesMalformedProvenance(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*nativeEpochInputProvenance)
	}{
		{name: "schema", mutate: func(join *nativeEpochInputProvenance) { join.Schema = "unreviewed" }},
		{name: "epoch", mutate: func(join *nativeEpochInputProvenance) { join.EpochObservationOrdinal++ }},
		{name: "missing-drain", mutate: func(join *nativeEpochInputProvenance) { join.DrainOrdinals = join.DrainOrdinals[:2] }},
		{name: "drain-order", mutate: func(join *nativeEpochInputProvenance) {
			join.DrainOrdinals[0], join.DrainOrdinals[1] = join.DrainOrdinals[1], join.DrainOrdinals[0]
		}},
		{name: "zero-drain", mutate: func(join *nativeEpochInputProvenance) { join.DrainOrdinals[0] = 0 }},
		{name: "write-before-drain", mutate: func(join *nativeEpochInputProvenance) {
			join.DrainOrdinals = []uint64{2, 3, 4}
			join.EpochWriteOrdinal = 1
		}},
		{name: "write-after-uid", mutate: func(join *nativeEpochInputProvenance) { join.EpochWriteOrdinal = 7 }},
		{name: "uid-omission", mutate: func(join *nativeEpochInputProvenance) { join.UidReadOrdinals = join.UidReadOrdinals[:1] }},
		{name: "uid-alias", mutate: func(join *nativeEpochInputProvenance) { join.UidReadOrdinals[0] = join.UidReadOrdinals[1] }},
		{name: "uid-before-write", mutate: func(join *nativeEpochInputProvenance) { join.UidReadOrdinals[0] = join.EpochWriteOrdinal }},
		{name: "uid-after-epoch", mutate: func(join *nativeEpochInputProvenance) { join.UidReadOrdinals[0] = join.EpochObservationOrdinal + 1 }},
	} {
		policy, admission, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
		test.mutate(outcome.EpochInputs)
		// Keep summary hashes internally consistent so the causal refusal must
		// come from original provenance, rather than a stale digest.
		outcome.ContentHash = outcome.hash()
		outcome.RecipientEffects.AggregateHash = nativeExecutionEffectBasis(outcome)
		outcome.RecipientEffects.ContentHash = outcome.RecipientEffects.hash()
		if projection, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome); err == nil || projection != nil {
			t.Fatalf("%s admitted contradictory original provenance: %+v, %v", test.name, projection, err)
		}
	}
}

func TestNativeYumaStorageJoinedEpochRequiresCanonicalU64Total(t *testing.T) {
	for _, total := range []string{"", "0200", "+200", "-1", " 200", "200 ", "1.0", "18446744073709551616"} {
		_, _, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
		outcome.EpochInputs.TotalAlpha = total
		epoch := report.HookObservations.Observations[len(report.HookObservations.Observations)-1]
		if _, err := nativeYumaJoinedEpochTotal(epoch, outcome.EpochInputs, outcome.Recipients); err == nil {
			t.Fatal("noncanonical or overflowing original total accepted", total)
		}
	}
	for _, total := range []string{"0", "18446744073709551615"} {
		_, _, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
		outcome.EpochInputs.TotalAlpha = total
		epoch := report.HookObservations.Observations[len(report.HookObservations.Observations)-1]
		actual, err := nativeYumaJoinedEpochTotal(epoch, outcome.EpochInputs, outcome.Recipients)
		if err != nil || total == "0" && actual != 0 || total != "0" && actual != ^uint64(0) {
			t.Fatal("canonical saturated total boundary refused", total, actual, err)
		}
	}
}

func TestNativeYumaStorageJoinedEpochRefusesMixedMemory(t *testing.T) {
	for _, name := range []string{"total-alpha", "hotkeys", "uids", "subnet-epoch"} {
		policy, admission, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
		epoch := &report.HookObservations.Observations[len(report.HookObservations.Observations)-1]
		epoch.Native.Memory = append(epoch.Native.Memory, historicalNativeMemory{Name: name, BytesHex: "0x"})
		if projection, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome); err == nil || projection != nil {
			t.Fatal("storage provenance accepted a legacy memory substitute", name, projection, err)
		}
	}
}

func TestNativeYumaStorageJoinedEpochDoesNotBorrowRecipientGeneration(t *testing.T) {
	for _, mutate := range []func(*nativeExecutionRecipient){
		func(recipient *nativeExecutionRecipient) { recipient.Uid = 1 },
		func(recipient *nativeExecutionRecipient) { recipient.Hotkey = "0x" + strings.Repeat("4", 64) },
		func(recipient *nativeExecutionRecipient) { recipient.Registered++ },
	} {
		policy, admission, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
		mutate(&outcome.Recipients[0])
		outcome.ContentHash = outcome.hash()
		outcome.RecipientEffects.Effects[0].Recipient = outcome.Recipients[0]
		outcome.RecipientEffects.AggregateHash = nativeExecutionEffectBasis(outcome)
		outcome.RecipientEffects.ContentHash = outcome.RecipientEffects.hash()
		projection, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
		if err == nil && (projection == nil || projection.MinerDenominator != nil || projection.Issue == "") {
			t.Fatal("self-consistent summaries replaced the original UID generation", projection)
		}
	}
}

func TestNativeYumaStorageJoinedEpochHashRejectsRetainedSubstitution(t *testing.T) {
	policy, admission, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
	projection, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
	if err != nil || projection == nil || projection.Issue != "" {
		t.Fatal(projection, err)
	}
	originalHash := outcome.ContentHash
	outcome.EpochInputs.UidReadOrdinals[0] = 7
	if outcome.hash() == originalHash {
		t.Fatal("original UID provenance is absent from the aggregate hash")
	}
	outcome.ContentHash = outcome.hash()
	if err := projection.validate(t.Context(), policy, outcome); err == nil {
		t.Fatal("retained calculation accepted a resealed original-read substitution")
	}
}

func TestNativeYumaStorageJoinedEpochRequiresEveryOriginalStage(t *testing.T) {
	policy, admission, report, outcome := nativeYumaEpochJoinTestFixture(t, true)
	epoch := &report.HookObservations.Observations[len(report.HookObservations.Observations)-1]
	for index, field := range epoch.Native.Memory {
		if field.Name == "validator-emission" {
			epoch.Native.Memory = append(epoch.Native.Memory[:index], epoch.Native.Memory[index+1:]...)
			break
		}
	}
	projection, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
	if err != nil || projection == nil || projection.MinerDenominator != nil || !strings.Contains(projection.Issue, "validator-emission") {
		t.Fatal("joined total replaced a missing original allocation stage", projection, err)
	}
}

func TestNativeYumaStorageJoinedEpochCannotClaimQuietZero(t *testing.T) {
	_, _, _, outcome := nativeYumaEpochJoinTestFixture(t, true)
	outcome.Recipients, outcome.MinerAllocation = nil, "0"
	projection := &nativeYumaProjection{}
	if err := projection.calculate(t.Context(), 25, outcome); err == nil || projection.MinerDenominator != nil {
		t.Fatal("orphan original epoch provenance was treated as a quiet block", projection, err)
	}
}
