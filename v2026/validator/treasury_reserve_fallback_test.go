//go:build linux || darwin

// Genuine signed M8 work with every current binding unbound leaves no head
// fleet, and two completed trails keep each pool below its quality minimum.
// The provider allocation is then empty; only treasury authority may answer it
// with the reserve-only row, and only while no provider weight exists.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Bindings are unbound before any proof is signed. Historical work remains in
// each operator pool, but too little of it to reach a positive quality.
func unbindReleaseMeasurementTestHead(_ *recycleAdmissionFixture, provider *releaseMeasurementV2TestFixture) {
	for index := range provider.artifact.Bindings {
		binding := &provider.artifact.Bindings[index]
		binding.Active, binding.LiveUIDFound, binding.LiveUID = false, false, 0
		binding.LocalClientKey = releaseHex32([32]byte{})
	}
}

// The configure callback selects owner-recycle (nil) or treasury authority
// before the independent production approval is signed.
func newEmptyAllocationProductionTestFixture(t *testing.T, configure func(*ownerRecycleProductionTestFixture)) *ownerRecycleProductionTestFixture {
	t.Helper()
	return newOwnerRecycleProductionTestFixtureWithInputs(t, func(t *testing.T, hotkey [32]byte) *recycleOperatorFixture {
		return newRecycleOperatorFixtureWithInputs(t, hotkey, 2, nil, unbindReleaseMeasurementTestHead)
	}, configure)
}

func newTreasuryReserveOnlyTestFixture(t *testing.T) *ownerRecycleProductionTestFixture {
	t.Helper()
	return newEmptyAllocationProductionTestFixture(t, func(fixture *ownerRecycleProductionTestFixture) {
		configureTreasuryProductionTest(t, fixture)
	})
}

// Two and three approved recipients share the whole row equally; providers
// receive nothing. Production cap water-filling and u16 normalization leave
// the exact row unchanged, so no integer repair can redistribute it.
func TestTreasuryReserveOnlyRowQuantizesEqualRecipients(t *testing.T) {
	parent := recycleTestInput(t).ParentPolicy
	for _, test := range []struct {
		owners []uint16
		share  *big.Rat
	}{
		{owners: []uint16{90, 91}, share: big.NewRat(1, 2)},
		{owners: []uint16{90, 91, 92}, share: big.NewRat(1, 3)},
	} {
		ownerKVs := map[uint16]bool{}
		for _, uid := range test.owners {
			ownerKVs[uid] = true
		}
		preview := &OwnerRecyclePreview{OwnerUids: slices.Clone(test.owners)}
		if err := completeOwnerRecycleRow(preview, parent, nil, nil, ownerKVs, true); err != nil {
			t.Fatalf("%d recipients: %v", len(test.owners), err)
		}
		if !slices.Equal(preview.Uids, test.owners) || len(preview.WireValues) != len(test.owners) {
			t.Fatalf("%d recipients lost or reordered a destination: %v %v", len(test.owners), preview.Uids, preview.WireValues)
		}
		for index, score := range preview.Scores {
			if score.Cmp(test.share) != 0 || preview.WireValues[index] != 65535 {
				t.Fatalf("%d recipients: share %s value %d", len(test.owners), score, preview.WireValues[index])
			}
		}
		if preview.WireProviderShare.Sign() != 0 || preview.ProviderShareError.Sign() != 0 {
			t.Fatalf("reserve-only row carries provider share %s or error %s", preview.WireProviderShare, preview.ProviderShareError)
		}
		capped, err := crv4.ApplyMaxWeightLimitRational(preview.Scores, parent.Steering.MaxWeightLimitU16)
		if err != nil {
			t.Fatal(err)
		}
		uids, values, err := crv4.NormalizeRationalToU16(preview.Uids, capped)
		if err != nil || !slices.Equal(uids, preview.Uids) || !slices.Equal(values, preview.WireValues) {
			t.Fatalf("production cap or quantization changed the reserve-only row: %v %v %v", uids, values, err)
		}
	}
	// One recipient would need the whole row, above the unchanged signed cap.
	single := &OwnerRecyclePreview{OwnerUids: []uint16{90}}
	if err := completeOwnerRecycleRow(single, parent, nil, nil, map[uint16]bool{90: true}, true); err == nil || !strings.Contains(err.Error(), "signed weight cap") {
		t.Fatalf("one reserve recipient exceeded the cap: %v", err)
	}
	// A reserve-only request never carries provider weight, and an ordinary
	// request still refuses an empty provider row with its original error.
	mixed := &OwnerRecyclePreview{OwnerUids: []uint16{90, 91}}
	if err := completeOwnerRecycleRow(mixed, parent, []uint16{10}, []*big.Rat{big.NewRat(1, 1)}, map[uint16]bool{90: true, 91: true}, true); err == nil || len(mixed.Uids) != 0 {
		t.Fatalf("reserve-only row mixed in provider weight: %v", err)
	}
	ordinary := &OwnerRecyclePreview{OwnerUids: []uint16{90, 91}}
	if err := completeOwnerRecycleRow(ordinary, parent, nil, nil, map[uint16]bool{90: true, 91: true}, false); err == nil || !strings.Contains(err.Error(), "lacks providers") {
		t.Fatalf("ordinary row lost its empty-provider refusal: %v", err)
	}
}

// The preview planner remains owner-recycle only. Its empty allocation keeps
// the original refusal, and a treasury proposal is still refused outright.
func TestOwnerRecyclePreviewKeepsRefusalForEmptyAllocation(t *testing.T) {
	input := recycleTestInput(t)
	input.Pools, input.Head = nil, nil
	requireRecycleRefusal(t, input, "owner-recycle provider allocation unavailable; no owner-only or zero-incentive fallback")
	policy := treasuryPolicyTestValue(t)
	input.Proposal.Schema, input.Proposal.Treasury = TreasuryProposalSchema, &policy
	input.Proposal.Remainder, input.Proposal.OwnerAllocation = "ordinary_treasury_credit", "equal_exact_registered"
	requireRecycleRefusal(t, input, "treasury preview requires the authenticated ordinary-recipient census")
	normal, err := PreviewOwnerRecycle(recycleTestInput(t))
	if err != nil || normal.ProviderShareError.Cmp(big.NewRat(-3, 1456330)) != 0 {
		t.Fatalf("normal preview changed its 1/10 share error: %v", err)
	}
}

// The genuine empty replay yields every approved recipient at an equal share,
// providers none, and the explicit marker. The signed sidecar, prepared row,
// envelope and intent all verify; the native source commits to the marker.
func TestTreasuryReserveOnlyMeasuredRowFromEmptyReplay(t *testing.T) {
	fixture := newTreasuryReserveOnlyTestFixture(t)
	measurement := fixture.operator.measurement
	stage, provider := fixture.stage(t)
	policy := measurement.admission.approval.Proposal.Treasury
	recipients := []uint16{policy.Recipients[0].Uid, policy.Recipients[1].Uid}
	if len(provider.UIDs) != 0 || len(provider.Scores) != 0 || len(provider.SelectedHead) != 0 || !slices.Contains(provider.MaskedUIDs, measurement.provider.artifact.SelfUID) {
		t.Fatalf("fixture does not replay an empty, self-masked provider allocation: %v", provider.UIDs)
	}
	half := RationalJSON{Numerator: "1", Denominator: "2"}
	row := stage.proof.Row
	if row.Fallback != TreasuryReserveOnlyFallback || !slices.Equal(row.Uids, recipients) || !slices.Equal(row.TreasuryUids, recipients) ||
		len(row.OwnerUids) != 0 || len(row.MaskedOwnerUids) != 0 || !slices.Equal(row.Values, []uint16{65535, 65535}) ||
		!slices.Equal(row.Scores, []RationalJSON{half, half}) || row.WireProviderShare != (RationalJSON{Numerator: "0", Denominator: "1"}) || row.ProviderShareError != "0" {
		t.Fatalf("reserve-only row differs: %+v", row)
	}
	if stage.proof.Schema != treasuryProductionDecisionSchema || !bytes.Contains(stage.encoded, []byte(`"fallback":"`+TreasuryReserveOnlyFallback+`"`)) {
		t.Fatal("treasury proof omitted its explicit reserve-only marker")
	}
	intent := fixture.intent(t, stage, provider)
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, intent, measurement.encoded, measurement.provider.artifact, provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, measurement.provider.artifact, verified); err != nil {
		t.Fatal(err)
	}
	if intent.Treasury == nil || intent.OwnerRecycle != nil || !slices.Equal(intent.UIDs, recipients) || !slices.Equal(intent.Prepared.UIDs, recipients) ||
		!slices.Equal(intent.Prepared.Values, row.Values) || intent.Prepared.SourceCommitment.Hash != releaseHex32(productionEconomicSourceHash(measurement.encoded, stage.encoded, treasuryProductionDecisionSchema)) {
		t.Fatal("reserve-only intent lost its recipients, prepared row or marked source commitment")
	}
	// The review capsule replays the same row under the observed authority,
	// whatever reserve choice the caller's options carry.
	options := measurement.provider.options(t)
	encoded, decision, err := SealTreasuryMeasurement(t.Context(), stage.authority, measurement.encoded, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decision.Row, row) || decision.Schema != treasuryDecisionIntentSchema || !strings.Contains(decision.Blockers[3], "reserve-only native outcome") {
		t.Fatalf("reserve-only review capsule differs: %+v", decision)
	}
	if err := VerifyOwnerRecycleDecisionIntent(t.Context(), stage.authority, encoded, measurement.provider.options(t), decision); err != nil {
		t.Fatal(err)
	}
}

// With any provider weight the fallback is refused: the derivation, the
// signed sidecar comparison and the shared arithmetic never mix or replace
// the normal 1/10 row, and a hidden positive weight cannot borrow it.
func TestTreasuryReserveOnlyRefusedWhileProviderWeightExists(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	measurement := fixture.operator.measurement
	stage, provider := fixture.stage(t)
	if stage.proof.Row.Fallback != "" || len(provider.UIDs) == 0 {
		t.Fatal("normal treasury replay selected the reserve-only row")
	}
	// Dropping the replayed provider row does not select the fallback while
	// the measurement's own pool or head projection still carries weight.
	candidate := *provider
	candidate.UIDs, candidate.Scores = nil, nil
	row, err := deriveOwnerRecycleMeasuredRow(stage.authority, measurement.provider.artifact, &candidate)
	if err == nil || len(row.Uids) != 0 || !strings.Contains(err.Error(), "a provider weight remains") {
		t.Fatalf("hidden provider weight admitted a reserve-only row: %v", err)
	}
	// A reserve-only sidecar cannot replace the replayed normal decision.
	valid := fixture.intent(t, stage, provider)
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var forged SteeringIntent
	if err := json.Unmarshal(raw, &forged); err != nil {
		t.Fatal(err)
	}
	recipients := forged.Treasury.Proof.Row.TreasuryUids
	half := RationalJSON{Numerator: "1", Denominator: "2"}
	forged.Treasury.Proof.Row = OwnerRecycleMeasuredRow{Uids: recipients, Scores: []RationalJSON{half, half}, Values: []uint16{65535, 65535}, TreasuryUids: recipients,
		WireProviderShare: RationalJSON{Numerator: "0", Denominator: "1"}, ProviderShareError: "0", Fallback: TreasuryReserveOnlyFallback}
	if got, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, &forged, measurement.encoded, measurement.provider.artifact, provider); got != nil || err == nil {
		t.Fatal("a reserve-only sidecar replaced a replayed provider allocation")
	}
	// On the genuinely empty replay, any positive unmasked pool or head
	// weight in the verified projections still refuses the fallback.
	empty := newTreasuryReserveOnlyTestFixture(t)
	emptyStage, emptyProvider := empty.stage(t)
	artifact := empty.operator.measurement.provider.artifact
	for _, fault := range []string{"pool", "head", "mask"} {
		candidate := *emptyProvider
		candidate.Pools = slices.Clone(emptyProvider.Pools)
		switch fault {
		case "pool":
			candidate.Pools[0].Eligible, candidate.Pools[0].Score = true, big.NewRat(1, 1)
		case "head":
			candidate.SelectedHead = []ExactWeightInput{{UID: candidate.Pools[0].UID, Score: big.NewRat(1, 1)}}
		case "mask":
			candidate.MaskedUIDs = append(slices.Clone(candidate.MaskedUIDs), emptyStage.proof.Row.TreasuryUids[0])
			slices.Sort(candidate.MaskedUIDs)
		}
		if row, err := deriveOwnerRecycleMeasuredRow(emptyStage.authority, artifact, &candidate); err == nil || len(row.Uids) != 0 {
			t.Fatalf("%s fault admitted a reserve-only row", fault)
		}
	}
}

// Owner-recycle production and testnet keep the original no-positive-weight
// refusal for the same genuine empty allocation; only treasury selects it.
func TestReserveOnlyFallbackRefusedOutsideTreasury(t *testing.T) {
	fixture := newEmptyAllocationProductionTestFixture(t, nil)
	measurement := fixture.operator.measurement
	if treasuryReserveFallbackConfig(fixture.cfg) || fixture.cfg.TreasuryApproval != nil {
		t.Fatal("owner-recycle fixture selected treasury authority")
	}
	if _, _, err := DecodeReleaseMeasurementArtifactV2(t.Context(), measurement.encoded, fixture.providerOptions(t)); !errors.Is(err, errNoPositiveUnmaskedWeights) {
		t.Fatalf("owner-recycle replay lost its empty-allocation refusal: %v", err)
	}
	// A caller that admits the empty measurement still cannot obtain an
	// owner-only row: production observation and derivation both refuse.
	options := measurement.provider.options(t)
	options.treasuryReserveFallback = true
	_, empty, err := DecodeReleaseMeasurementArtifactV2(t.Context(), measurement.encoded, options)
	if err != nil || len(empty.Decision.UIDs) != 0 {
		t.Fatalf("treasury replay of the same bytes differs: %v", err)
	}
	options = measurement.provider.options(t)
	options.treasuryReserveFallback = true
	if stage, err := prepareOwnerRecycleProductionDecision(t.Context(), fixture.cfg, measurement.admission.chain, fixture.operator.chain,
		measurement.encoded, measurement.provider.artifact, empty.Decision, options); stage != nil || !errors.Is(err, errNoPositiveUnmaskedWeights) {
		t.Fatalf("owner-recycle production borrowed the treasury fallback: %v", err)
	}
	if row, err := deriveOwnerRecycleMeasuredRow(measurement.authority, measurement.provider.artifact, empty.Decision); err == nil || len(row.Uids) != 0 || !strings.Contains(err.Error(), "lacks providers") {
		t.Fatalf("owner-recycle derivation produced an owner-only row: %v", err)
	}
	// Testnet artifacts never admit an empty allocation, whatever the option.
	testnet := newReleaseMeasurementV2TestFixture(t, 2)
	if testnet.artifact.Policy.NetworkProfile == "mainnet" {
		t.Fatal("testnet fixture selected the mainnet profile")
	}
	unbindReleaseMeasurementTestHead(nil, testnet)
	testnet.rebuildLegacy(t)
	testnetOptions := testnet.options(t)
	testnetOptions.treasuryReserveFallback = true
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), testnet.artifact, testnetOptions); !errors.Is(err, errNoPositiveUnmaskedWeights) {
		t.Fatalf("testnet measurement admitted an empty allocation: %v", err)
	}
}

// Only an exclusive mainnet schema-3 treasury selection enables the fallback.
func TestTreasuryReserveFallbackRequiresExclusiveMainnetTreasury(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	if !treasuryReserveFallbackConfig(fixture.cfg) {
		t.Fatal("treasury production did not select its reserve fallback")
	}
	for _, fault := range []string{"nil", "owner-recycle", "both", "schema", "testnet"} {
		changed := *fixture.cfg
		switch fault {
		case "nil":
			if treasuryReserveFallbackConfig(nil) {
				t.Fatal("absent configuration selected the reserve fallback")
			}
			continue
		case "owner-recycle":
			changed.OwnerRecycleApproval, changed.TreasuryApproval = changed.TreasuryApproval, nil
		case "both":
			changed.OwnerRecycleApproval = changed.TreasuryApproval
		case "schema":
			changed.SchemaVersion = ReleaseValidatorSchemaVersion
		case "testnet":
			changed.Policy.NetworkProfile = "testnet"
		}
		if treasuryReserveFallbackConfig(&changed) {
			t.Fatalf("%s configuration selected the reserve fallback", fault)
		}
	}
}

// Normal treasury rows omit the marker: their row, proof and intent bytes are
// exactly the original wire, so existing content hashes remain unchanged.
func TestTreasuryNormalRowBytesOmitReserveFallback(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	for _, value := range []any{stage.proof.Row, stage.proof, intent} {
		raw, err := json.Marshal(value)
		if err != nil || bytes.Contains(raw, []byte(`"fallback"`)) || bytes.Contains(raw, []byte("reserve_only")) {
			t.Fatalf("normal treasury wire acquired the reserve-only marker: %v", err)
		}
	}
	type originalRow struct {
		Uids               []uint16       `json:"uids"`
		Scores             []RationalJSON `json:"scores"`
		Values             []uint16       `json:"values"`
		OwnerUids          []uint16       `json:"owner_uids"`
		MaskedOwnerUids    []uint16       `json:"masked_owner_uids"`
		TreasuryUids       []uint16       `json:"treasury_uids,omitempty"`
		WireProviderShare  RationalJSON   `json:"wire_provider_share"`
		ProviderShareError string         `json:"provider_share_error"`
	}
	row := stage.proof.Row
	current, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	original, err := json.Marshal(originalRow{Uids: row.Uids, Scores: row.Scores, Values: row.Values, OwnerUids: row.OwnerUids, MaskedOwnerUids: row.MaskedOwnerUids,
		TreasuryUids: row.TreasuryUids, WireProviderShare: row.WireProviderShare, ProviderShareError: row.ProviderShareError})
	if err != nil || !bytes.Equal(current, original) {
		t.Fatalf("normal treasury row bytes changed: %v", err)
	}
	share, ok := new(big.Rat).SetString(row.WireProviderShare.Numerator + "/" + row.WireProviderShare.Denominator)
	shareError, errorOK := new(big.Rat).SetString(row.ProviderShareError)
	if !ok || !errorOK || new(big.Rat).Sub(share, big.NewRat(1, 10)).Cmp(shareError) != 0 {
		t.Fatal("normal treasury share error is no longer measured against 1/10")
	}
}

// A durable begin and every fresh restart replay the reserve-only intent under
// the intent's own treasury authority: measurement, envelope, sidecar row,
// native source and vector hash all retain the explicit fallback, through
// receipt reconciliation and the applied native row comparison.
func TestTreasuryReserveOnlyIntentReplaysThroughApplication(t *testing.T) {
	fixture := newProductionContinuationTestFixtureWithWork(t, 200, 2, nil, func(production *ownerRecycleProductionTestFixture, _ *productionContinuationTestFixture) {
		configureTreasuryProductionTest(t, production)
	})
	if fixture.intent.Treasury == nil || fixture.intent.Treasury.Proof.Row.Fallback != TreasuryReserveOnlyFallback {
		t.Fatal("continuation fixture did not prepare a reserve-only treasury intent")
	}
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	restarted := fixture.restart(t)
	if restarted.VectorHash != pending.VectorHash || restarted.Treasury == nil || restarted.Treasury.Proof.Row.Fallback != TreasuryReserveOnlyFallback ||
		!slices.Equal(restarted.UIDs, restarted.Treasury.Proof.Row.TreasuryUids) || restarted.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex {
		t.Fatal("restart changed the reserve-only intent or its marker")
	}
	unmarked := *restarted
	treasury := *restarted.Treasury
	treasury.Proof.Row.Fallback = ""
	unmarked.Treasury = &treasury
	if hash, err := unmarked.ReconstructedVectorHash(); err != nil || hash == restarted.VectorHash {
		t.Fatalf("vector hash does not bind the reserve-only marker: %v", err)
	}
	native.receiptNumber, fixture.production.head = 101, 102
	fixture.production.extrinsicsKVs = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("reserve-only receipt did not reconcile: %v", err)
	}
	finalized := fixture.restart(t)
	native.applied, fixture.production.head = true, max(uint64(102), finalized.RevealBlock)
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("reserve-only applied native row did not reconcile: %v", err)
	}
	applied := fixture.restart(t)
	if applied.Status != "applied" || applied.Treasury == nil || applied.Treasury.Proof.Row.Fallback != TreasuryReserveOnlyFallback ||
		applied.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || len(native.broadcasts) != 1 {
		t.Fatal("reserve-only recovery lost its original transaction, marker or one-send boundary")
	}
}
