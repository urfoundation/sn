// Deterministic synthetic censuses exercise proposal arithmetic and refusal
// boundaries. They provide no live-chain identity or activation evidence.
package validator

import (
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Generates visibly synthetic distinct account and artifact identifiers.
func recycleTestId(value uint16) [32]byte {
	return [32]byte{0: 0xee, 30: byte(value >> 8), 31: byte(value)}
}

// Two owner destinations fit the unchanged signed half-row cap. The two
// independent validator identities remain recipients of native dividends.
func recycleTestInput(t *testing.T) OwnerRecyclePreviewInput {
	t.Helper()
	parent := exactPolicy(t)
	parent.NetworkProfile = "mainnet"
	parent.ProductionCadence.EpochBlocks = 50_400
	parent.Steering.Theta = protocol.Rational{Numerator: 3, Denominator: 10}
	parentHash, err := parent.Hash()
	if err != nil {
		t.Fatal(err)
	}
	runtime := OwnerRecycleRuntimePin{
		GenesisHash: recycleTestId(1000), Netuid: 25,
		Version:  crv4.RuntimeVersionIdentity{SpecName: "synthetic-runtime", SpecVersion: 471, TransactionVersion: 1, StateVersion: 1},
		CodeHash: recycleTestId(1001), MetadataHash: recycleTestId(1002), SourceCommit: ownerRecycleSourceCommit,
	}
	registrations := []OwnerRecycleRegistration{}
	for _, uid := range []uint16{10, 20, 90, 91, 200, 201} {
		registrations = append(registrations, OwnerRecycleRegistration{Uid: uid, Hotkey: recycleTestId(uid), RegistrationBlock: 10})
	}
	return OwnerRecyclePreviewInput{
		ParentPolicy: parent,
		Proposal: OwnerRecycleProposal{
			Schema: ownerRecycleProposalSchema, ParentPolicyHash: parentHash,
			PolicyId: parent.PolicyID + 1, EffectiveEpoch: parent.EffectiveEpoch + 1,
			ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10},
			Remainder:     "recognized_owner_recycle", OwnerAllocation: "equal_unmasked_registered", Runtime: runtime,
		},
		Snapshot: OwnerRecycleSnapshot{
			Runtime: runtime, FinalizedHash: recycleTestId(1003), FinalizedNumber: 100, MechanismCount: 1,
			RecycleModeScale: []byte{1}, SubnetOwner: recycleTestId(1004),
			OwnedHotkeys: [][32]byte{recycleTestId(90), recycleTestId(91)}, Registrations: registrations,
			LiveValidatorUids: []uint16{200, 201}, HealthyOperators: [][32]byte{recycleTestId(1005), recycleTestId(1006)},
		},
		SelfUid: 200,
		Pools:   []OwnerRecycleProvider{{Uid: 10, Hotkey: recycleTestId(10), Score: big.NewRat(7, 1)}},
		Head:    []OwnerRecycleProvider{{Uid: 20, Hotkey: recycleTestId(20), Score: big.NewRat(3, 1)}},
	}
}

// A refusal never returns a partially usable candidate.
func requireRecycleRefusal(t *testing.T, input OwnerRecyclePreviewInput, text string) {
	t.Helper()
	preview, err := PreviewOwnerRecycle(input)
	if preview != nil || err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("preview=%+v error=%v, want refusal containing %q", preview, err, text)
	}
}

// The independent expected row fixes both theta-within-ten-percent and exact
// production half-up max scaling; sum-normalization would fail this vector.
func TestOwnerRecyclePreviewNormalizesAndQuantizes(t *testing.T) {
	input := recycleTestInput(t)
	preview, err := PreviewOwnerRecycle(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Uids, []uint16{10, 20, 90, 91}) || !reflect.DeepEqual(preview.WireValues, []uint16{10194, 4369, 65535, 65535}) {
		t.Fatalf("candidate = %v %v", preview.Uids, preview.WireValues)
	}
	wantScores := []*big.Rat{big.NewRat(7, 100), big.NewRat(3, 100), big.NewRat(9, 20), big.NewRat(9, 20)}
	for i, want := range wantScores {
		if preview.Scores[i].Cmp(want) != 0 {
			t.Fatalf("score %d = %s, want %s", i, preview.Scores[i], want)
		}
	}
	if preview.WireProviderShare.Cmp(big.NewRat(14563, 145633)) != 0 || preview.ProviderShareError.Cmp(big.NewRat(-3, 1456330)) != 0 {
		t.Fatalf("wire share=%s error=%s", preview.WireProviderShare, preview.ProviderShareError)
	}
	if preview.ProposalHash == ([32]byte{}) || preview.FinalizedHash != input.Snapshot.FinalizedHash || preview.AdmissionError() == nil {
		t.Fatal("preview lost its evidence binding or authorized submission")
	}
}

// A half-unit boundary rounds upward in the production exact normalizer.
func TestOwnerRecyclePreviewRoundsHalfUp(t *testing.T) {
	input := recycleTestInput(t)
	input.ParentPolicy.Steering.Theta = protocol.Rational{Numerator: 78633, Denominator: 262140}
	parentHash, err := input.ParentPolicy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	input.Proposal.ParentPolicyHash = parentHash
	preview, err := PreviewOwnerRecycle(input)
	if err != nil || preview.WireValues[1] != 4369 {
		t.Fatalf("half-up boundary: %+v %v", preview, err)
	}
	scaledHead := new(big.Rat).Mul(new(big.Rat).Quo(preview.Scores[1], preview.Scores[2]), big.NewRat(65535, 1))
	if scaledHead.Cmp(big.NewRat(8737, 2)) != 0 {
		t.Fatalf("fixture missed the half-unit boundary: %s", scaledHead)
	}
}

// Existing production capping must leave every successful exact preview alone.
func TestOwnerRecyclePreviewAgreesWithProductionCap(t *testing.T) {
	input := recycleTestInput(t)
	preview, err := PreviewOwnerRecycle(input)
	if err != nil {
		t.Fatal(err)
	}
	capped, err := crv4.ApplyMaxWeightLimitRational(preview.Scores, input.ParentPolicy.Steering.MaxWeightLimitU16)
	if err != nil {
		t.Fatal(err)
	}
	for i, score := range capped {
		if score.Cmp(preview.Scores[i]) != 0 {
			t.Fatalf("production cap changed candidate %d", i)
		}
	}
	uids, values, err := crv4.NormalizeRationalToU16(preview.Uids, capped)
	if err != nil || !reflect.DeepEqual(uids, preview.Uids) || !reflect.DeepEqual(values, preview.WireValues) {
		t.Fatalf("production quantization differs: %v %v %v", uids, values, err)
	}
}

// Reordering census and channel inputs cannot change signing candidate bytes.
func TestOwnerRecyclePreviewCanonicalOrderAndOwnedResults(t *testing.T) {
	input := recycleTestInput(t)
	first, err := PreviewOwnerRecycle(input)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(input.Snapshot.Registrations)-1; i < j; i, j = i+1, j-1 {
		input.Snapshot.Registrations[i], input.Snapshot.Registrations[j] = input.Snapshot.Registrations[j], input.Snapshot.Registrations[i]
	}
	input.Snapshot.OwnedHotkeys[0], input.Snapshot.OwnedHotkeys[1] = input.Snapshot.OwnedHotkeys[1], input.Snapshot.OwnedHotkeys[0]
	second, err := PreviewOwnerRecycle(input)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("ordering changed preview: %v", err)
	}
	second.Scores[0].SetInt64(9)
	second.WireValues[0] = 0
	second.OwnerUids[0] = 0
	if first.Scores[0].Cmp(big.NewRat(7, 100)) != 0 || input.Pools[0].Score.Cmp(big.NewRat(7, 1)) != 0 || first.WireValues[0] != 10194 || first.OwnerUids[0] != 90 {
		t.Fatal("preview aliases input or another preview")
	}
}

// A separate explicit owner hotkey is recognized even when OwnedHotkeys omits
// it; a hotkey present in both sources is counted exactly once.
func TestOwnerRecyclePreviewRecognizesExplicitOwnerHotkey(t *testing.T) {
	input := recycleTestInput(t)
	explicit := recycleTestId(91)
	input.Snapshot.SubnetOwnerHotkey = &explicit
	input.Snapshot.OwnedHotkeys = input.Snapshot.OwnedHotkeys[:1]
	preview, err := PreviewOwnerRecycle(input)
	if err != nil || !reflect.DeepEqual(preview.OwnerUids, []uint16{91, 90}) {
		t.Fatalf("explicit owner missing: %+v %v", preview, err)
	}
	input.Snapshot.OwnedHotkeys = append(input.Snapshot.OwnedHotkeys, explicit)
	preview, err = PreviewOwnerRecycle(input)
	if err != nil || len(preview.OwnerUids) != 2 {
		t.Fatalf("overlapping owner sources were double counted: %+v %v", preview, err)
	}
}

// Runtime owner recognition orders registrations by recency, then uid.
func TestOwnerRecyclePreviewOwnerRegistrationOrder(t *testing.T) {
	input := recycleTestInput(t)
	for i := range input.Snapshot.Registrations {
		if input.Snapshot.Registrations[i].Uid == 91 {
			input.Snapshot.Registrations[i].RegistrationBlock = 20
		}
	}
	preview, err := PreviewOwnerRecycle(input)
	if err != nil || !reflect.DeepEqual(preview.OwnerUids, []uint16{91, 90}) {
		t.Fatalf("owner registration order: %+v %v", preview, err)
	}
}

// Unregistered owned keys confer no destination and cannot improve cap fit.
func TestOwnerRecyclePreviewMissingOwnerUid(t *testing.T) {
	input := recycleTestInput(t)
	input.Snapshot.OwnedHotkeys = [][32]byte{recycleTestId(600)}
	explicit := recycleTestId(601)
	input.Snapshot.SubnetOwnerHotkey = &explicit
	requireRecycleRefusal(t, input, "no unmasked runtime-recognized")
}

// A registered arbitrary identity does not become a second owner recipient.
// The declared OwnedHotkeys[SubnetOwner] relation is required; authenticating
// that relation remains an activation blocker outside this pure preview.
func TestOwnerRecyclePreviewSecondOwnerRequiresOwnership(t *testing.T) {
	input := recycleTestInput(t)
	input.Snapshot.OwnedHotkeys = input.Snapshot.OwnedHotkeys[:1]
	requireRecycleRefusal(t, input, "signed weight cap")
	input.Snapshot.OwnedHotkeys = append(input.Snapshot.OwnedHotkeys, recycleTestId(91))
	preview, err := PreviewOwnerRecycle(input)
	if err != nil || !reflect.DeepEqual(preview.OwnerUids, []uint16{90, 91}) {
		t.Fatalf("second owned registered hotkey unrecognized: %+v %v", preview, err)
	}
}

// One owner would consume ninety percent, above the unchanged half-row cap.
func TestOwnerRecyclePreviewRejectsSingleOwnerCap(t *testing.T) {
	input := recycleTestInput(t)
	input.Snapshot.OwnedHotkeys = input.Snapshot.OwnedHotkeys[:1]
	requireRecycleRefusal(t, input, "signed weight cap")
}

// Neither a controlled mask nor the runtime owner self exception can override
// existing validator policy. Masked owners are excluded before cap feasibility.
func TestOwnerRecyclePreviewRetainsSelfAndControlledMasks(t *testing.T) {
	input := recycleTestInput(t)
	input.Snapshot.OwnedHotkeys = append(input.Snapshot.OwnedHotkeys, recycleTestId(200))
	input.MaskedUids = map[uint16]bool{90: true, 200: false}
	requireRecycleRefusal(t, input, "signed weight cap")
	delete(input.MaskedUids, 90)
	preview, err := PreviewOwnerRecycle(input)
	if err != nil || !reflect.DeepEqual(preview.MaskedOwnerUids, []uint16{200}) || !reflect.DeepEqual(preview.OwnerUids, []uint16{90, 91}) {
		t.Fatalf("self mask changed: %+v %v", preview, err)
	}
}

// Duplicate owner entries cannot create more cap slots.
func TestOwnerRecyclePreviewRejectsDuplicateOwners(t *testing.T) {
	input := recycleTestInput(t)
	input.Snapshot.OwnedHotkeys[1] = input.Snapshot.OwnedHotkeys[0]
	requireRecycleRefusal(t, input, "owned hotkey census")
}

// A provider/owner overlap would recycle a claimed provider payment.
func TestOwnerRecyclePreviewRejectsOwnerProviderOverlap(t *testing.T) {
	input := recycleTestInput(t)
	input.Pools[0].Uid = 90
	input.Pools[0].Hotkey = recycleTestId(90)
	input.Pools[0].Score.SetInt64(0)
	input.MaskedUids = map[uint16]bool{90: true}
	requireRecycleRefusal(t, input, "overlaps an owner")
}

// Duplicate channels cannot count one recipient twice or alter normalization.
func TestOwnerRecyclePreviewRejectsDuplicateProviders(t *testing.T) {
	input := recycleTestInput(t)
	input.Head = append(input.Head, input.Pools[0])
	requireRecycleRefusal(t, input, "duplicated or overlaps")
}

// Both directions of the registration map must be unambiguous.
func TestOwnerRecyclePreviewRejectsAmbiguousCensus(t *testing.T) {
	for _, sameUid := range []bool{true, false} {
		input := recycleTestInput(t)
		registration := OwnerRecycleRegistration{Uid: 50, Hotkey: recycleTestId(50), RegistrationBlock: 10}
		if sameUid {
			registration.Uid = 90
		} else {
			registration.Hotkey = recycleTestId(90)
		}
		input.Snapshot.Registrations = append(input.Snapshot.Registrations, registration)
		requireRecycleRefusal(t, input, "census is ambiguous")
	}
}

// The uid alone is insufficient to attribute measured provider service.
func TestOwnerRecyclePreviewRejectsStaleProviderHotkey(t *testing.T) {
	input := recycleTestInput(t)
	input.Head[0].Hotkey = recycleTestId(800)
	requireRecycleRefusal(t, input, "unregistered, stale")
}

// Empty head/tail channels cede only their provider tenth, never owner budget.
func TestOwnerRecyclePreviewEmptyProviderChannelCedesWithinTenth(t *testing.T) {
	for _, emptyHead := range []bool{true, false} {
		input := recycleTestInput(t)
		if emptyHead {
			input.Head = nil
		} else {
			input.Pools = nil
		}
		preview, err := PreviewOwnerRecycle(input)
		if err != nil || len(preview.Scores) != 3 || preview.Scores[0].Cmp(big.NewRat(1, 10)) != 0 || preview.Scores[1].Cmp(big.NewRat(9, 20)) != 0 {
			t.Fatalf("empty channel changed proposal: %+v %v", preview, err)
		}
	}
}

// A missing provider row never becomes an owner-only plan or a claim that a
// zero-incentive runtime epoch recycles the miner allocation.
func TestOwnerRecyclePreviewNoIncentiveFallback(t *testing.T) {
	input := recycleTestInput(t)
	input.Pools = nil
	input.Head = nil
	requireRecycleRefusal(t, input, "no owner-only or zero-incentive fallback")
	input = recycleTestInput(t)
	input.MaskedUids = map[uint16]bool{10: true, 20: true}
	requireRecycleRefusal(t, input, "no owner-only or zero-incentive fallback")
	input = recycleTestInput(t)
	input.Pools[0].Score.SetInt64(0)
	input.Head[0].Score.SetInt64(0)
	requireRecycleRefusal(t, input, "no owner-only or zero-incentive fallback")
}

// Raw missing, Burn and malformed values cannot inherit Recycle semantics.
func TestOwnerRecyclePreviewRejectsAbsentOrBurnMode(t *testing.T) {
	for _, mode := range [][]byte{nil, {}, {0}, {1, 0}, {2}} {
		input := recycleTestInput(t)
		input.Snapshot.RecycleModeScale = mode
		requireRecycleRefusal(t, input, "explicit Recycle")
	}
}

// A different runtime or incomplete finality claim cannot reuse the proposal.
func TestOwnerRecyclePreviewBindsRuntimeAndFinality(t *testing.T) {
	for _, mutate := range []func(*OwnerRecyclePreviewInput){
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.Runtime.MetadataHash = recycleTestId(800) },
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.Runtime.CodeHash = recycleTestId(800) },
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.Runtime.GenesisHash = recycleTestId(800) },
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.Runtime.Version.SpecVersion++ },
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.FinalizedHash = [32]byte{} },
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.FinalizedNumber = 0 },
	} {
		input := recycleTestInput(t)
		mutate(&input)
		requireRecycleRefusal(t, input, "finalized runtime pins")
	}
	input := recycleTestInput(t)
	input.Snapshot.Registrations[0].RegistrationBlock = input.Snapshot.FinalizedNumber + 1
	requireRecycleRefusal(t, input, "newer than its finalized hash")
}

// Changing the signed cap, theta or safety minimum changes the parent hash.
func TestOwnerRecycleProposalBindsUnchangedParent(t *testing.T) {
	for _, mutate := range []func(*protocol.Policy){
		func(parent *protocol.Policy) { parent.Steering.MaxWeightLimitU16++ },
		func(parent *protocol.Policy) { parent.Steering.Theta.Numerator++ },
		func(parent *protocol.Policy) { parent.Safety.MinimumLiveValidatorCount++ },
	} {
		input := recycleTestInput(t)
		mutate(&input.ParentPolicy)
		requireRecycleRefusal(t, input, "unchanged mainnet parent")
	}
}

// The schema's reviewed source profile is exact but no runtime number is given
// authority. Future source commits require a reviewed new profile.
func TestOwnerRecycleProposalRejectsIncompleteAuthority(t *testing.T) {
	for _, mutate := range []func(*OwnerRecycleProposal){
		func(proposal *OwnerRecycleProposal) { proposal.Runtime.CodeHash = [32]byte{} },
		func(proposal *OwnerRecycleProposal) { proposal.Runtime.SourceCommit = strings.Repeat("a", 40) },
		func(proposal *OwnerRecycleProposal) { proposal.Runtime.Version.StateVersion = 0 },
		func(proposal *OwnerRecycleProposal) { proposal.ProviderShare.Numerator = 2 },
		func(proposal *OwnerRecycleProposal) { proposal.Remainder = "burn" },
		func(proposal *OwnerRecycleProposal) { proposal.PolicyId = 0 },
		func(proposal *OwnerRecycleProposal) { proposal.EffectiveEpoch = 0 },
	} {
		input := recycleTestInput(t)
		mutate(&input.Proposal)
		if err := input.Proposal.Validate(input.ParentPolicy); err == nil {
			t.Fatal("incomplete proposal accepted")
		}
	}
}

// Each validator still measures and constructs its own provider row. Different
// measurements produce different candidates; neither implies a Yuma outcome.
func TestOwnerRecyclePreviewIndependentValidatorVariation(t *testing.T) {
	firstInput := recycleTestInput(t)
	firstInput.Head = nil
	firstInput.Pools = append(firstInput.Pools, OwnerRecycleProvider{Uid: 20, Hotkey: recycleTestId(20), Score: big.NewRat(1, 1)})
	first, err := PreviewOwnerRecycle(firstInput)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := recycleTestInput(t)
	secondInput.SelfUid = 201
	secondInput.Head = nil
	secondInput.Pools[0].Score.SetInt64(1)
	secondInput.Pools = append(secondInput.Pools, OwnerRecycleProvider{Uid: 20, Hotkey: recycleTestId(20), Score: big.NewRat(7, 1)})
	second, err := PreviewOwnerRecycle(secondInput)
	if err != nil || reflect.DeepEqual(first.WireValues, second.WireValues) || first.AdmissionError() == nil || second.AdmissionError() == nil {
		t.Fatalf("independent measurements collapsed or implied activation: %+v %v", second, err)
	}
	if first.Scores[0].Cmp(second.Scores[1]) != 0 || first.Scores[1].Cmp(second.Scores[0]) != 0 {
		t.Fatal("independent quality change did not reverse the provider allocation")
	}
}

// Counts cannot be satisfied by repeating a validator or removing operators.
func TestOwnerRecyclePreviewRetainsAdmissionMinimums(t *testing.T) {
	input := recycleTestInput(t)
	input.Snapshot.LiveValidatorUids = []uint16{200}
	requireRecycleRefusal(t, input, "live-validator minimum")
	input = recycleTestInput(t)
	input.Snapshot.LiveValidatorUids = []uint16{200, 200}
	requireRecycleRefusal(t, input, "validator census")
	input = recycleTestInput(t)
	input.Snapshot.HealthyOperators = input.Snapshot.HealthyOperators[:1]
	requireRecycleRefusal(t, input, "healthy-operator minimum")
}

// The successor cannot expand the existing single-mechanism head budget.
func TestOwnerRecyclePreviewRetainsMechanismAndHeadBounds(t *testing.T) {
	input := recycleTestInput(t)
	input.Snapshot.MechanismCount = 2
	requireRecycleRefusal(t, input, "one mechanism")
	input = recycleTestInput(t)
	input.ParentPolicy.Steering.MaximumHeadFleets = 1
	parentHash, err := input.ParentPolicy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	input.Proposal.ParentPolicyHash = parentHash
	input.Head = append(input.Head, input.Pools[0])
	requireRecycleRefusal(t, input, "maximum head-fleet count")
}

// Nil or negative provider scores remain invalid even behind a recipient mask.
func TestOwnerRecyclePreviewRejectsInvalidScores(t *testing.T) {
	for _, score := range []*big.Rat{nil, big.NewRat(-1, 1)} {
		input := recycleTestInput(t)
		input.Pools[0].Score = score
		input.MaskedUids = map[uint16]bool{10: true}
		requireRecycleRefusal(t, input, "nil or negative")
	}
}

// Refuse oversized claims before interpreting their zero/duplicate contents or
// allocating census maps. Even a global owned-hotkey list has a preview bound.
func TestOwnerRecyclePreviewRejectsOversizedCensus(t *testing.T) {
	for _, mutate := range []func(*OwnerRecyclePreviewInput){
		func(input *OwnerRecyclePreviewInput) {
			input.Snapshot.Registrations = make([]OwnerRecycleRegistration, 65537)
		},
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.OwnedHotkeys = make([][32]byte, 65537) },
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.LiveValidatorUids = make([]uint16, 65537) },
		func(input *OwnerRecyclePreviewInput) { input.Snapshot.HealthyOperators = make([][32]byte, 65537) },
		func(input *OwnerRecyclePreviewInput) { input.Pools = make([]OwnerRecycleProvider, 65537) },
		func(input *OwnerRecyclePreviewInput) { input.Pools = make([]OwnerRecycleProvider, 65536) },
		func(input *OwnerRecyclePreviewInput) { input.Head = make([]OwnerRecycleProvider, 65537) },
	} {
		input := recycleTestInput(t)
		mutate(&input)
		requireRecycleRefusal(t, input, "planning bound")
	}
}

// Tiny positive provider weights cannot silently disappear at the integer
// boundary even though their exact score was present in the tenth.
func TestOwnerRecyclePreviewRejectsQuantizedRecipientLoss(t *testing.T) {
	input := recycleTestInput(t)
	input.Head = nil
	input.Pools = append(input.Pools, OwnerRecycleProvider{Uid: 20, Hotkey: recycleTestId(20), Score: big.NewRat(1, 1_000_000_000)})
	requireRecycleRefusal(t, input, "quantization loses")
}

// 101 small entries each round down; their accumulated loss puts the largest
// wire weight over a cap which the exact 9/20 share satisfies.
func TestOwnerRecyclePreviewRejectsQuantizedCapViolation(t *testing.T) {
	input := recycleTestInput(t)
	input.ParentPolicy.Steering.MaxWeightLimitU16 = 29491
	input.ParentPolicy.Safety.MinimumHealthyNOCount = 3
	input.Snapshot.HealthyOperators = append(input.Snapshot.HealthyOperators, recycleTestId(1007))
	parentHash, err := input.ParentPolicy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	input.Proposal.ParentPolicyHash = parentHash
	input.Head = nil
	input.Pools = nil
	for uid := uint16(300); uid < 401; uid++ {
		input.Snapshot.Registrations = append(input.Snapshot.Registrations, OwnerRecycleRegistration{Uid: uid, Hotkey: recycleTestId(uid), RegistrationBlock: 10})
		input.Pools = append(input.Pools, OwnerRecycleProvider{Uid: uid, Hotkey: recycleTestId(uid), Score: big.NewRat(1, 1)})
	}
	requireRecycleRefusal(t, input, "quantized row exceeds signed cap")
}

// Neither fabricated preview values nor absent preview state are credentials.
func TestOwnerRecyclePreviewAlwaysRefusesActivation(t *testing.T) {
	for _, preview := range []*OwnerRecyclePreview{nil, {}, {WireProviderShare: big.NewRat(1, 10)}} {
		if err := preview.AdmissionError(); err == nil || !strings.Contains(err.Error(), "final Yuma/native-emission reconciliation") {
			t.Fatalf("preview authorized activation: %v", err)
		}
	}
}
