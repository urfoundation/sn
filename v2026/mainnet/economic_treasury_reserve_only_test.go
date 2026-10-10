// Synthetic original inputs and windows exercise reserve-only accounting
// without replay authority. Only actual weight inputs, never an outcome,
// identify a reserve-only epoch; ordinary epochs keep the 10/90 split.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

// UIDs 0 and 1 are the approved treasury generations, UID 2 the one active
// validator and UID 3 an ordinary provider.
func reserveOnlyYumaTestInput() (nativeYumaInput, []*big.Rat) {
	hotkey := func(value byte) string {
		account := nativeTreasuryTestAccount(value)
		return "0x" + hex.EncodeToString(account[:])
	}
	input := nativeYumaInput{Count: 4, CurrentBlock: 100, OwnerUid: 65535,
		Nodes: []nativeYumaNode{
			{Uid: 0, Hotkey: hotkey(0x11), Registered: 20},
			{Uid: 1, Hotkey: hotkey(0x22), Registered: 21},
			{Uid: 2, Hotkey: hotkey(0x80), Registered: 5, LastUpdate: 90, Permit: true},
			{Uid: 3, Hotkey: hotkey(0x60), Registered: 6, LastUpdate: 90},
		},
		Weights: [][]nativeYumaEdge{{}, {}, {{Column: 0, Value: 65535}, {Column: 1, Value: 65535}}, {}},
		Bonds:   make([][]nativeYumaEdge, 4),
	}
	return input, []*big.Rat{new(big.Rat), new(big.Rat), big.NewRat(1, 1), new(big.Rat)}
}

// Every active voter naming exactly the treasury generations at one equal
// weight selects a reserve-only epoch. A provider, partial, unequal, masked or
// second ordinary vote does not; inactive and self-only rows cast no vote.
func TestNativeYumaReserveOnlyRowsUseActualWeightInputs(t *testing.T) {
	authority, _ := nativeTreasuryTestAuthority(t)
	treasury := newNativeTreasuryAmounts(authority)
	for _, test := range []struct {
		name   string
		mutate func(*nativeYumaInput, []*big.Rat, **nativeTreasuryAmounts)
		want   bool
	}{
		{name: "reserve-only", want: true},
		{name: "inactive-ordinary-row", want: true, mutate: func(input *nativeYumaInput, _ []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Weights[3] = []nativeYumaEdge{{Column: 2, Value: 65535}}
		}},
		{name: "self-only-row", want: true, mutate: func(input *nativeYumaInput, active []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Weights[3], active[3] = []nativeYumaEdge{{Column: 3, Value: 65535}}, big.NewRat(1, 2)
		}},
		{name: "provider", mutate: func(input *nativeYumaInput, _ []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Weights[2] = append(input.Weights[2], nativeYumaEdge{Column: 3, Value: 65535})
		}},
		{name: "unequal", mutate: func(input *nativeYumaInput, _ []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Weights[2][1].Value = 32768
		}},
		{name: "zero", mutate: func(input *nativeYumaInput, _ []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Weights[2][1].Value = 0
		}},
		{name: "partial", mutate: func(input *nativeYumaInput, _ []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Weights[2] = input.Weights[2][:1]
		}},
		{name: "outdated-recipient", mutate: func(input *nativeYumaInput, _ []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Nodes[2].LastUpdate = 21
		}},
		{name: "second-ordinary-voter", mutate: func(input *nativeYumaInput, active []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Weights[3], active[3] = []nativeYumaEdge{{Column: 2, Value: 65535}}, big.NewRat(1, 2)
		}},
		{name: "no-voter", mutate: func(_ *nativeYumaInput, active []*big.Rat, _ **nativeTreasuryAmounts) {
			active[2] = new(big.Rat)
		}},
		{name: "replaced-generation", mutate: func(input *nativeYumaInput, _ []*big.Rat, _ **nativeTreasuryAmounts) {
			input.Nodes[1].Registered = 22
		}},
		{name: "owner-recycle", mutate: func(_ *nativeYumaInput, _ []*big.Rat, selected **nativeTreasuryAmounts) {
			*selected = nil
		}},
	} {
		input, active := reserveOnlyYumaTestInput()
		selected := treasury
		if test.mutate != nil {
			test.mutate(&input, active, &selected)
		}
		if got := nativeYumaReserveOnlyRows(input, active, selected); got != test.want {
			t.Fatalf("%s reserve-only classification = %t, want %t", test.name, got, test.want)
		}
	}
}

// Ordinary witnesses and archives keep their original bytes; reserve-only
// tranches retire into one canonical positive cumulative amount.
func TestEconomicYumaArchiveCarriesReserveOnlyTranche(t *testing.T) {
	projection := func(parent, boundary byte, reserve *string) nativeYumaProjection {
		denominator, tolerance := "98", "2"
		return nativeYumaProjection{Parent: economicEmissionBoundary{Number: uint64(parent), Hash: "0x" + strings.Repeat(hex.EncodeToString([]byte{parent}), 32)},
			Boundary:         economicEmissionBoundary{Number: uint64(boundary), Hash: "0x" + strings.Repeat(hex.EncodeToString([]byte{boundary}), 32)},
			MinerDenominator: &denominator, FullQuantizationTolerance: &tolerance, ReserveOnlyAllocation: reserve}
	}
	raw, err := json.Marshal(projection(10, 11, nil))
	if err != nil || bytes.Contains(raw, []byte("reserve_only")) {
		t.Fatalf("ordinary witness acquired a reserve-only field: %v", err)
	}
	ordinary, err := mergeEconomicYumaArchive(nil, projection(10, 11, nil))
	if err != nil || ordinary.ReserveOnlyAllocation != "" {
		t.Fatal("ordinary retirement invented a reserve-only tranche", err)
	}
	raw, err = json.Marshal(ordinary)
	if err != nil || bytes.Contains(raw, []byte("reserve_only")) {
		t.Fatalf("ordinary archive bytes changed: %v", err)
	}
	tranche := "100"
	reserve, err := mergeEconomicYumaArchive(ordinary, projection(11, 12, &tranche))
	if err != nil || reserve.ReserveOnlyAllocation != "100" || reserve.Blocks != 2 {
		t.Fatal("reserve-only tranche was not retired", reserve, err)
	}
	next, err := mergeEconomicYumaArchive(reserve, projection(12, 13, &tranche))
	if err != nil || next.ReserveOnlyAllocation != "200" || next.MinerDenominator != "294" || next.Blocks != 3 {
		t.Fatal("reserve-only tranches did not accumulate exactly", next, err)
	}
	malformed := "0100"
	if _, err := mergeEconomicYumaArchive(next, projection(13, 14, &malformed)); err == nil {
		t.Fatal("noncanonical reserve-only tranche was retired")
	}
}

// A reserve-only tranche owes providers nothing and the treasury all of it,
// while tenths still carry across ordinary tranches. Without witnessed
// reserve-only rows the same all-treasury outcome remains a contradiction.
func TestEconomicTreasuryConformanceAccountsForReserveOnlyTranche(t *testing.T) {
	authority, policy := nativeTreasuryTestAuthority(t)
	window := func(miner, provider, gross, difference string) *nativeExecutionWindow {
		t.Helper()
		value := nativeExecutionEmpty(policy.From)
		value.Treasury = newNativeTreasuryAmounts(authority)
		value.Treasury.Gross, value.Treasury.Liquid = gross, gross
		value.MinerAllocation, value.ProviderEntitlement, value.AllocationDifference = miner, provider, difference
		value.Blocks, value.Through = 1, economicEmissionBoundary{Number: policy.From.Number + 1, Hash: "0x" + strings.Repeat("41", 32)}
		if err := value.references(); err != nil {
			t.Fatal(err)
		}
		if err := value.validate(); err != nil {
			t.Fatal(err)
		}
		return &value
	}
	summary := func(execution *nativeExecutionWindow, reserve *string) *economicConservationSummary {
		t.Helper()
		denominator, err := economicConservationSum(execution.ProviderEntitlement, execution.Treasury.Gross)
		if err != nil {
			t.Fatal(err)
		}
		tolerance := "2"
		return &economicConservationSummary{NativeCursor: execution.Through, Execution: execution,
			Yuma:    &economicConservationYumaSummary{Through: execution.Through, Current: true, MinerDenominator: &denominator, FullQuantizationTolerance: &tolerance, ReserveOnlyAllocation: reserve},
			Funding: &economicConservationFundingSummary{Captured: zeroEconomicFunding(), Accepted: zeroEconomicFunding(), Paid: zeroEconomicFunding()}}
	}
	label := "complete-native-miner-tranche-contradicts-provider-ten-treasury-remainder"
	whole, half := "100", "50"
	for _, test := range []struct {
		name               string
		summary            *economicConservationSummary
		within             bool
		provider, treasury string
	}{
		{name: "reserve-only", summary: summary(window("100", "0", "98", "2"), &whole), within: true, provider: "0", treasury: "-20"},
		{name: "unwitnessed", summary: summary(window("100", "0", "98", "2"), nil), provider: "-100", treasury: "80"},
		{name: "mixed", summary: summary(window("150", "10", "138", "2"), &half), within: true, provider: "0", treasury: "-20"},
		{name: "ordinary-shortfall", summary: summary(window("150", "0", "148", "2"), &half), provider: "-100", treasury: "80"},
	} {
		if err := test.summary.assessConformance(); err != nil {
			t.Fatal(test.name, err)
		}
		conformance := test.summary.Conformance
		if conformance.NativeSplitWithinTolerance == nil || *conformance.NativeSplitWithinTolerance != test.within || conformance.TreasuryWithinTolerance == nil || *conformance.TreasuryWithinTolerance != test.within ||
			*conformance.ProviderDeviationNumerator != test.provider || *conformance.TreasuryDeviationNumerator != test.treasury {
			t.Fatalf("%s treasury split differs: %+v", test.name, conformance)
		}
		contradicted := strings.Contains(strings.Join(conformance.Contradictions, ","), label)
		if contradicted == test.within || test.summary.Yuma.ReserveOnlyAllocation != nil && (conformance.ReserveOnlyAllocation == nil || *conformance.ReserveOnlyAllocation != *test.summary.Yuma.ReserveOnlyAllocation) ||
			test.summary.Yuma.ReserveOnlyAllocation == nil && conformance.ReserveOnlyAllocation != nil || !test.within && (test.summary.TargetMet == nil || *test.summary.TargetMet) {
			t.Fatalf("%s contradiction or reserve-only accounting differs: %+v", test.name, conformance)
		}
	}
	excess := "151"
	if err := summary(window("150", "10", "138", "2"), &excess).assessConformance(); err == nil {
		t.Fatal("reserve-only tranche exceeded the original miner allocation")
	}
	denominator, tolerance := "100", "2"
	legacy := economicConservationSummary{
		Execution: &nativeExecutionWindow{ProviderEntitlement: "10", OwnerRecycled: "90", ResidualEntitlement: "0"},
		Yuma:      &economicConservationYumaSummary{Current: true, MinerDenominator: &denominator, FullQuantizationTolerance: &tolerance, ReserveOnlyAllocation: &whole},
		Funding:   &economicConservationFundingSummary{Captured: zeroEconomicFunding(), Accepted: zeroEconomicFunding(), Paid: zeroEconomicFunding()},
	}
	if err := legacy.assessConformance(); err == nil {
		t.Fatal("owner-recycle conformance admitted a treasury reserve-only tranche")
	}
	if within, provider, treasury, _, err := economicTreasurySplit(*window("100", "0", "98", "2"), "2"); err != nil || within || provider != "-100" || treasury != "80" {
		t.Fatal("unwitnessed treasury split changed", within, provider, treasury, err)
	}
}

// Reference intervals marked reserve-only owe providers nothing; provider
// tenths carry only across ordinary intervals and ordinary bytes are unchanged.
func TestEconomicReferenceReserveOnlyIntervalsOweProvidersNothing(t *testing.T) {
	authority, _ := nativeTreasuryTestAuthority(t)
	input := economicReferenceInput{Schema: economicTreasuryReferenceInputSchema, TreasuryPolicy: &authority.Policy, NativeMinerAllocations: []string{"9", "50", "1"}, ReserveOnlyIntervals: []int{1}}
	reference, err := calculateEconomicReference(input, "synthetic-input")
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []struct {
		reserveOnly                                      bool
		provider, treasury, providerTotal, treasuryTotal string
	}{
		{provider: "0", treasury: "9", providerTotal: "0", treasuryTotal: "9"},
		{reserveOnly: true, provider: "0", treasury: "50", providerTotal: "0", treasuryTotal: "59"},
		{provider: "1", treasury: "0", providerTotal: "1", treasuryTotal: "59"},
	} {
		interval := reference.Intervals[index]
		if interval.ReserveOnly != want.reserveOnly || interval.ProviderAlpha != want.provider || *interval.TreasuryAlpha != want.treasury ||
			interval.ProviderTotalAlpha != want.providerTotal || *interval.TreasuryTotalAlpha != want.treasuryTotal || interval.OwnerRecycleAlpha != "0" {
			t.Fatalf("reserve-only reference interval %d differs: %+v", index, interval)
		}
	}
	plain := input
	plain.ReserveOnlyIntervals = nil
	ordinary, err := calculateEconomicReference(plain, "synthetic-input")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ordinary)
	if err != nil || bytes.Contains(raw, []byte("reserve_only")) || ordinary.Intervals[1].ProviderAlpha != "5" {
		t.Fatalf("ordinary reference changed: %v", err)
	}
	for _, intervals := range [][]int{{3}, {-1}, {1, 1}, {2, 1}} {
		changed := input
		changed.ReserveOnlyIntervals = intervals
		if _, err := calculateEconomicReference(changed, "synthetic-input"); err == nil {
			t.Fatalf("reserve-only intervals %v were admitted", intervals)
		}
	}
	legacy := economicReferenceInput{Schema: economicReferenceInputSchema, NativeMinerAllocations: []string{"9", "1"}, ReserveOnlyIntervals: []int{0}}
	if _, err := calculateEconomicReference(legacy, "synthetic-input"); err == nil {
		t.Fatal("owner-recycle reference admitted a reserve-only interval")
	}
}

// A synthetic original join, not a real Wasm run: the one active voter sends
// the given row, and treasury credits are the closed fixed emissions of that
// same input, so the witness can derive and revalidate its complete census.
func reserveOnlyYumaWitnessFixture(t *testing.T, row []nativeYumaEdge) (economicEmissionPolicy, nativeExecutionAdmission, *historicalReplayReport, nativeExecutionOutcome) {
	t.Helper()
	authority, _ := nativeTreasuryTestAuthority(t)
	input, _ := reserveOnlyYumaTestInput()
	input.Netuid, input.CurrentBlock, input.Tempo, input.ActivityCutoff, input.LastStep = 25, 101, 10, 300, 90
	input.Kappa, input.BondsPenalty, input.MovingAverage, input.AlphaLow, input.AlphaHigh = 32768, 65535, 1000000, 16384, 49152
	for index := range input.Nodes {
		input.Nodes[index].CommitBlock = ^uint64(0)
	}
	input.Nodes[2].Alpha, input.Nodes[2].LastUpdate = 100, 99
	input.Weights[2], input.Bonds = row, [][]nativeYumaEdge{{}, {}, {}, {}}
	yuma := &nativeYumaPolicy{Schema: nativeYumaSchema, LayoutSha256: monitorReadDigest([]byte(nativeYumaLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic-reserve-only-review")), MaximumWitnessBytes: 1024 * 1024, HotBlockReserve: 2, MaximumEdges: 32, MaximumOperations: 400000}
	policy := economicEmissionPolicy{Netuid: input.Netuid, Execution: &nativeExecutionPolicy{Yuma: yuma}}
	parent := economicEmissionBoundary{Number: 100, Hash: "0x" + strings.Repeat("a", 64)}
	boundary := economicEmissionBoundary{Number: 101, Hash: "0x" + strings.Repeat("b", 64)}
	phase, ordinal := "0x02", uint64(9)
	bytesField := func(name string, raw []byte) historicalNativeMemory {
		return historicalNativeMemory{Name: name, BytesHex: "0x" + hex.EncodeToString(raw)}
	}
	integers := func(name string, width int, values ...uint64) historicalNativeMemory {
		raw := make([]byte, 0, width*len(values))
		for _, value := range values {
			raw = append(raw, binary.LittleEndian.AppendUint64(nil, value)[:width]...)
		}
		return bytesField(name, raw)
	}
	observation := func(purpose string, fields ...historicalNativeMemory) historicalReplayObservation {
		ordinal++
		return historicalReplayObservation{Ordinal: ordinal, Purpose: purpose, Operation: "host", Native: &historicalNativeObservation{ExecutionPhaseHex: &phase, Memory: append([]historicalNativeMemory{integers("netuid", 2, uint64(input.Netuid))}, fields...)}}
	}
	records := []historicalReplayObservation{
		observation("native-yuma-meta", integers("uid-count", 2, uint64(input.Count)), integers("current-block", 8, input.CurrentBlock), integers("tempo", 8, input.Tempo), integers("activity-cutoff", 8, input.ActivityCutoff), integers("last-step", 8, input.LastStep), integers("minimum-stake", 8, input.MinimumStake), integers("owner-uid", 2, uint64(input.OwnerUid)), integers("tao-weight", 8, input.TaoWeight), integers("kappa", 2, uint64(input.Kappa)), integers("bonds-penalty", 2, uint64(input.BondsPenalty)), integers("moving-average", 8, input.MovingAverage)),
		observation("native-yuma-settings", integers("yuma3", 1, 0), integers("liquid-alpha", 1, 0), integers("commit-reveal", 1, 0), integers("consensus-mode", 1, 0), integers("alpha-low", 2, uint64(input.AlphaLow)), integers("alpha-high", 2, uint64(input.AlphaHigh)), integers("steepness", 2, 0), integers("previous-consensus", 2)),
	}
	var hotkeys []byte
	var registered []uint64
	recipients := make([]nativeExecutionRecipient, len(input.Nodes))
	for index, node := range input.Nodes {
		raw, err := hex.DecodeString(strings.TrimPrefix(node.Hotkey, "0x"))
		if err != nil {
			t.Fatal(err)
		}
		hotkeys, registered = append(hotkeys, raw...), append(registered, node.Registered)
		permit := uint64(0)
		if node.Permit {
			permit = 1
		}
		records = append(records, observation("native-yuma-node", integers("uid", 2, uint64(node.Uid)), bytesField("hotkey", raw), integers("registered", 8, node.Registered), integers("last-update", 8, node.LastUpdate), integers("permit", 1, permit), integers("alpha", 8, node.Alpha), integers("tao", 8, node.Tao), integers("commit-block", 8, node.CommitBlock),
			integers("parent-proportions", 8), bytesField("parent-hotkeys", nil), integers("parent-alpha", 8), integers("parent-tao", 8), integers("child-proportions", 8), bytesField("child-hotkeys", nil)))
		recipients[index] = nativeExecutionRecipient{Uid: node.Uid, Hotkey: node.Hotkey, Registered: node.Registered, Coldkey: "0x" + strings.Repeat("3", 64)}
		if nativeTreasuryRecipient(authority.Policy, recipients[index]) {
			recipients[index].Coldkey = "0x" + hex.EncodeToString(authority.Policy.MultisigAccount[:])
		}
	}
	for _, rows := range []struct {
		purpose string
		matrix  [][]nativeYumaEdge
	}{{purpose: "native-yuma-weights", matrix: input.Weights}, {purpose: "native-yuma-bonds", matrix: input.Bonds}} {
		for index, edges := range rows.matrix {
			var columns, values []uint64
			for _, edge := range edges {
				columns, values = append(columns, uint64(edge.Column)), append(values, uint64(edge.Value))
			}
			records = append(records, observation(rows.purpose, integers("uid", 2, uint64(index)), integers("columns", 2, columns...), integers("values", 2, values...)))
		}
	}
	fixed, err := evaluateNativeYuma(t.Context(), input, 200, false, yuma.MaximumOperations)
	if err != nil {
		t.Fatal(err)
	}
	epoch := observation("native-epoch", integers("registered", 8, registered...), integers("total-alpha", 8, 200), bytesField("hotkeys", hotkeys))
	for _, stage := range []struct {
		name     string
		values   []*big.Rat
		fraction uint
	}{
		{name: "stake-q32", values: fixed.stake, fraction: 32}, {name: "active-stake-q32", values: fixed.active, fraction: 32},
		{name: "consensus-q32", values: fixed.consensus, fraction: 32}, {name: "incentive-q32", values: fixed.incentive, fraction: 32},
		{name: "dividends-q32", values: fixed.dividend, fraction: 32}, {name: "normalized-q32", values: fixed.server, fraction: 32},
		{name: "validator-normalized-q32", values: fixed.validator, fraction: 32}, {name: "emission", values: fixed.serverAlpha},
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
	outcome := nativeExecutionOutcome{Treasury: newNativeTreasuryAmounts(authority), Boundary: boundary, AdmissionHash: monitorReadDigest([]byte("synthetic-admission")), JobHash: monitorReadDigest([]byte("synthetic-job")), TraceHash: monitorReadDigest([]byte("synthetic-trace")),
		MinerAllocation: "100", ProviderEntitlement: "0", OwnerRecycled: "0", ResidualEntitlement: "0", CollateralCapture: "0", FixedPointDust: "0", FixedPointTolerance: "0", RedirectedToValidators: "0", AllocationDifference: "0", Recipients: recipients, AmountsAuthenticated: true}
	var effects []nativeExecutionEffect
	owner, ownerHotkey := "0x"+strings.Repeat("70", 32), "0x"+strings.Repeat("71", 32)
	for _, recipient := range recipients {
		gross := fixed.serverAlpha[recipient.Uid].Num().String()
		if gross == "0" {
			continue
		}
		if !nativeTreasuryRecipient(authority.Policy, recipient) {
			t.Fatal("synthetic reserve-only epoch credited a non-treasury generation", recipient.Uid)
		}
		stake := recipient.Hotkey
		effect := nativeExecutionEffect{Ordinal: uint64(60 + recipient.Uid), Recipient: recipient, Branch: "native-miner-credit", Gross: gross, Liquid: gross, Collateral: "0", Recycled: "0",
			Treasury: &nativeTreasuryRecipientEffect{SubnetOwner: owner, SubnetOwnerHotkey: &ownerHotkey, OwnerHotkeys: []string{ownerHotkey}, StakeDestination: &stake}}
		for _, target := range []*string{&outcome.Treasury.Gross, &outcome.Treasury.Liquid} {
			if err := nativeExecutionAdd(target, gross, false); err != nil {
				t.Fatal(err)
			}
		}
		effects = append(effects, effect)
	}
	outcome.ContentHash = outcome.hash()
	eventIndex, emissionOrdinal := uint64(0), uint64(59)
	outcome.RecipientEffects = newNativeExecutionEffects(parent, outcome, &eventIndex, &emissionOrdinal, effects)
	report := &historicalReplayReport{HookObservations: &historicalReplayObservations{Observations: append(records, epoch)}}
	return policy, nativeExecutionAdmission{Yuma: yuma, Parent: parent}, report, outcome
}

// The complete allocation witness derives the reserve-only tranche from the
// decoded original weight inputs, and revalidation recomputes it exactly. An
// unequal treasury row keeps the ordinary witness bytes and cannot claim it.
func TestNativeYumaWitnessMarksReserveOnlyTranche(t *testing.T) {
	policy, admission, report, outcome := reserveOnlyYumaWitnessFixture(t, []nativeYumaEdge{{Column: 0, Value: 65535}, {Column: 1, Value: 65535}})
	projection, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
	if err != nil || projection == nil || projection.Issue != "" || projection.MinerDenominator == nil || projection.ReserveOnlyAllocation == nil || *projection.ReserveOnlyAllocation != outcome.MinerAllocation {
		t.Fatal("reserve-only original epoch lost its witnessed tranche", projection, err)
	}
	if err := projection.validate(t.Context(), policy, outcome); err != nil {
		t.Fatal(err)
	}
	policy, admission, report, outcome = reserveOnlyYumaWitnessFixture(t, []nativeYumaEdge{{Column: 0, Value: 65535}, {Column: 1, Value: 32768}})
	ordinary, err := deriveNativeYuma(t.Context(), policy, admission, report, outcome)
	if err != nil || ordinary == nil || ordinary.Issue != "" || ordinary.MinerDenominator == nil || ordinary.ReserveOnlyAllocation != nil {
		t.Fatal("unequal treasury row was witnessed as reserve-only", ordinary, err)
	}
	raw, err := json.Marshal(ordinary)
	if err != nil || bytes.Contains(raw, []byte("reserve_only")) {
		t.Fatalf("ordinary witness bytes changed: %v", err)
	}
	claimed := *ordinary
	tranche := outcome.MinerAllocation
	claimed.ReserveOnlyAllocation = &tranche
	claimed.ContentHash = claimed.hash()
	if err := claimed.validate(t.Context(), policy, outcome); err == nil {
		t.Fatal("a retained witness claimed a reserve-only tranche its inputs do not show")
	}
}
