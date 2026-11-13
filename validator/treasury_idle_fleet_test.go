//go:build linux || darwin

// A bound fleet with no hops keeps its seat while its EMA decays by 3/4 a tempo
// and never reaches zero, so its share of the row falls below one wire unit.
// These tests take real treasury authority from a signed fixture and exercise
// the measured row through the derivation that submission and replay share.
package validator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/protocol/headscore"
)

const idleFleetTestPoolUid, idleFleetTestUid = uint16(67), uint16(200)

// The two head shapes of the original report: tempos below leftOutTempo round
// the idle fleet to at least one wire unit, and from there it rounds to zero.
var idleFleetTestCases = []struct {
	name         string
	otherUids    []uint16
	otherScore   *big.Rat
	idleStart    *big.Rat
	leftOutTempo int
	// SHA-256 of every row the code before this rule accepted, tempo by tempo:
	// computed on sn addb74e8, whose rows cannot change.
	acceptedRowsHash string
}{
	{name: "one other fleet at EMA 1", otherUids: []uint16{145}, otherScore: big.NewRat(1, 1), idleStart: big.NewRat(1, 1), leftOutTempo: 32,
		acceptedRowsHash: "720fda17a704be64d08a4a175155a032aef5eb61391d91d936edae83d5b82d8b"},
	{name: "199 other fleets at EMA 3", otherUids: idleFleetTestOtherUids(), otherScore: big.NewRat(3, 1), idleStart: big.NewRat(3, 1), leftOutTempo: 14,
		acceptedRowsHash: "eecefe71d28524805bd26160a321b5731ae17ec350538a77ef6c1798637ba958"},
}

func idleFleetTestOtherUids() []uint16 {
	uids := make([]uint16, 0, 199)
	for uid := uint16(300); uid < 499; uid++ {
		uids = append(uids, uid)
	}
	return uids
}

// The head of one tempo: every other fleet at its score, the idle fleet at its.
func idleFleetTestHead(otherUids []uint16, otherScore, idleScore *big.Rat) []ExactWeightInput {
	head := make([]ExactWeightInput, 0, len(otherUids)+1)
	for _, uid := range otherUids {
		head = append(head, ExactWeightInput{UID: uid, Score: otherScore})
	}
	return append(head, ExactWeightInput{UID: idleFleetTestUid, Score: idleScore})
}

// One tempo's provider decision over a genuine replay's masks: the channels are
// ranked and combined by the production selection and vector math.
func idleFleetTestDecision(t *testing.T, provider *VerifiedReleaseMeasurement, theta protocol.Rational, pools, head []ExactWeightInput) *VerifiedReleaseMeasurement {
	t.Helper()
	selection, err := selectHeadFleets(head, 200)
	if err != nil {
		t.Fatal(err)
	}
	decision := *provider
	decision.EligibleHead = append(slices.Clone(selection.Selected), selection.Rejected...)
	decision.SelectedHead, decision.RejectedHead = selection.Selected, selection.Rejected
	decision.Pools = make([]VerifiedReleasePool, len(pools))
	for index, pool := range pools {
		decision.Pools[index] = VerifiedReleasePool{NoID: uint64(index + 1), UID: pool.UID, Eligible: true, Score: pool.Score}
	}
	masked := map[uint16]bool{}
	for _, uid := range provider.MaskedUIDs {
		masked[uid] = true
	}
	decision.UIDs, decision.Scores, err = BuildWeightVectorExact(pools, selection.Selected, theta, masked)
	if err != nil {
		t.Fatal(err)
	}
	return &decision
}

// The row's exact shares are the signed split whatever the wire rounds away:
// 9/20 to each recipient, 7/100 to the pool and 3/100 to the head, and every
// recipient keeps at least one wire unit.
func requireIdleFleetTestShares(t *testing.T, row OwnerRecycleMeasuredRow, tempo int) {
	t.Helper()
	if len(row.Uids) != len(row.Scores) || len(row.Uids) != len(row.Values) {
		t.Fatalf("tempo %d: row lost an entry: %d uids, %d scores, %d values", tempo, len(row.Uids), len(row.Scores), len(row.Values))
	}
	head := new(big.Rat)
	for index, uid := range row.Uids {
		score, err := decodeRationalJSON(row.Scores[index])
		if err != nil {
			t.Fatal(err)
		}
		if row.Values[index] == 0 {
			t.Fatalf("tempo %d: uid %d kept an exact share with a zero wire value", tempo, uid)
		}
		switch {
		case slices.Contains(row.TreasuryUids, uid):
			if score.Cmp(big.NewRat(9, 20)) != 0 {
				t.Fatalf("tempo %d: recipient %d share %s", tempo, uid, score)
			}
		case uid == idleFleetTestPoolUid:
			if score.Cmp(big.NewRat(7, 100)) != 0 {
				t.Fatalf("tempo %d: pool share %s", tempo, score)
			}
		default:
			head.Add(head, score)
		}
	}
	if head.Cmp(big.NewRat(3, 100)) != 0 {
		t.Fatalf("tempo %d: head channel share %s, want exactly 3/100", tempo, head)
	}
}

// The wire name is the contract, so tests read it from the row's own bytes.
func idleFleetTestExcludedUids(t *testing.T, row OwnerRecycleMeasuredRow) []uint16 {
	t.Helper()
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var marked struct {
		Uids []uint16 `json:"excluded_head_uids"`
	}
	if err := json.Unmarshal(raw, &marked); err != nil {
		t.Fatal(err)
	}
	return marked.Uids
}

// One idle fleet must not refuse the row for every other fleet and pool. From
// the tempo it would round to zero it is left out, named in the row, and the
// other fleets take its exact share, for as long as the binding can last.
func TestTreasuryRowSurvivesIdleFleetDecay(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	artifact := fixture.operator.measurement.provider.artifact
	theta := artifact.Policy.Steering.Theta
	pools := []ExactWeightInput{{UID: idleFleetTestPoolUid, Score: big.NewRat(1, 1)}}
	for _, test := range idleFleetTestCases {
		t.Run(test.name, func(t *testing.T) {
			idle := new(big.Rat).Set(test.idleStart)
			for tempo := 0; tempo <= test.leftOutTempo+8; tempo++ {
				decision := idleFleetTestDecision(t, provider, theta, pools, idleFleetTestHead(test.otherUids, test.otherScore, idle))
				row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, decision)
				if err != nil {
					t.Fatalf("idle for %d tempos (%.1f h) refused the whole row: %v", tempo, float64(tempo)*1.2, err)
				}
				requireIdleFleetTestShares(t, row, tempo)
				if !slices.Contains(headSelectionUIDs(decision.SelectedHead), idleFleetTestUid) {
					t.Fatalf("tempo %d: the idle fleet lost its seat", tempo)
				}
				for _, uid := range test.otherUids {
					if !slices.Contains(row.Uids, uid) {
						t.Fatalf("tempo %d: busy fleet %d left the row", tempo, uid)
					}
				}
				leftOut := tempo >= test.leftOutTempo
				if slices.Contains(row.Uids, idleFleetTestUid) == leftOut {
					t.Fatalf("tempo %d: idle fleet in row %v, want left out from tempo %d", tempo, row.Uids, test.leftOutTempo)
				}
				if excluded := idleFleetTestExcludedUids(t, row); leftOut != slices.Equal(excluded, []uint16{idleFleetTestUid}) || !leftOut && excluded != nil {
					t.Fatalf("tempo %d: row names %v as left out", tempo, excluded)
				}
				idle.Mul(idle, big.NewRat(3, 4))
			}
			// The longest binding is 12 epochs: 1,680 tempos of decay still steer.
			lifetime := new(big.Rat).SetFrac(new(big.Int).Exp(big.NewInt(3), big.NewInt(1680), nil), new(big.Int).Exp(big.NewInt(4), big.NewInt(1680), nil))
			lifetime.Mul(lifetime, test.idleStart)
			row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, idleFleetTestDecision(t, provider, theta, pools, idleFleetTestHead(test.otherUids, test.otherScore, lifetime)))
			if err != nil {
				t.Fatalf("idle for a whole binding refused the row: %v", err)
			}
			requireIdleFleetTestShares(t, row, 1680)
			// A rule that keeps no state: its first tempo back returns the fleet.
			back, err := headscore.Next(big.NewRat(1, 1), lifetime, true, headEMAWeight(artifact.Policy.Steering.HeadScoreEMA))
			if err != nil {
				t.Fatal(err)
			}
			row, err = deriveOwnerRecycleMeasuredRow(stage.authority, artifact, idleFleetTestDecision(t, provider, theta, pools, idleFleetTestHead(test.otherUids, test.otherScore, back)))
			if err != nil || !slices.Contains(row.Uids, idleFleetTestUid) || idleFleetTestExcludedUids(t, row) != nil {
				t.Fatalf("a returning fleet stayed out: %v %v", row.Uids, err)
			}
		})
	}
}

// Half a wire unit rounds up to one and stays. Anything below it is left out,
// and the exact share it held moves to the other head fleets.
func TestTreasuryRowExclusionBoundaryIsHalfAWireUnit(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	artifact := fixture.operator.measurement.provider.artifact
	theta := artifact.Policy.Steering.Theta
	pools := []ExactWeightInput{{UID: idleFleetTestPoolUid, Score: big.NewRat(1, 1)}}
	derive := func(t *testing.T, head []ExactWeightInput) (OwnerRecycleMeasuredRow, map[uint16]uint16) {
		t.Helper()
		row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, idleFleetTestDecision(t, provider, theta, pools, head))
		if err != nil {
			t.Fatal(err)
		}
		requireIdleFleetTestShares(t, row, 0)
		values := map[uint16]uint16{}
		for index, uid := range row.Uids {
			values[uid] = row.Values[index]
		}
		return row, values
	}
	// At theta 3/10 with two recipients the head channel is 4,369 wire units, so
	// a share of exactly 1/8,738 is exactly half a unit.
	row, values := derive(t, []ExactWeightInput{{UID: 145, Score: big.NewRat(8737, 1)}, {UID: idleFleetTestUid, Score: big.NewRat(1, 1)}})
	if values[idleFleetTestUid] != 1 || values[145] != 4369 || idleFleetTestExcludedUids(t, row) != nil {
		t.Fatalf("exactly half a unit: %v excluded %v", values, idleFleetTestExcludedUids(t, row))
	}
	// One part in 8,739 is below it. The fleet is out and 145 holds the channel.
	row, values = derive(t, []ExactWeightInput{{UID: 145, Score: big.NewRat(8738, 1)}, {UID: idleFleetTestUid, Score: big.NewRat(1, 1)}})
	if _, found := values[idleFleetTestUid]; found || values[145] != 4369 || !slices.Equal(idleFleetTestExcludedUids(t, row), []uint16{idleFleetTestUid}) {
		t.Fatalf("below half a unit: %v excluded %v", values, idleFleetTestExcludedUids(t, row))
	}
	// Ranks leave from the bottom, because each departure raises the others'
	// shares: 201 ties 200 and ranks after it, and leaving it alone brings 200
	// back to exactly half a unit.
	row, values = derive(t, []ExactWeightInput{{UID: 145, Score: big.NewRat(8737, 1)}, {UID: idleFleetTestUid, Score: big.NewRat(1, 1)}, {UID: 201, Score: big.NewRat(1, 1)}})
	if _, found := values[201]; found || values[idleFleetTestUid] != 1 || values[145] != 4369 || !slices.Equal(idleFleetTestExcludedUids(t, row), []uint16{201}) {
		t.Fatalf("lowest rank first: %v excluded %v", values, idleFleetTestExcludedUids(t, row))
	}
	// Several below it leave together, listed by uid whatever their rank.
	row, values = derive(t, []ExactWeightInput{{UID: 145, Score: big.NewRat(1_000_000, 1)}, {UID: 203, Score: big.NewRat(2, 1)}, {UID: 201, Score: big.NewRat(1, 1)}, {UID: 202, Score: big.NewRat(3, 1)}})
	if len(values) != 4 || values[145] != 4369 || !slices.Equal(idleFleetTestExcludedUids(t, row), []uint16{201, 202, 203}) {
		t.Fatalf("several below: %v excluded %v", values, idleFleetTestExcludedUids(t, row))
	}
}

// A real loss still refuses, as TestOwnerRecyclePreviewRejectsQuantizedRecipientLoss
// requires of the preview: only a selected head fleet may be left out, and only
// while another head fleet keeps the channel. Other authority keeps every refusal.
func TestTreasuryRowStillRefusesARealLoss(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	artifact := fixture.operator.measurement.provider.artifact
	theta := artifact.Policy.Steering.Theta
	onePool := []ExactWeightInput{{UID: idleFleetTestPoolUid, Score: big.NewRat(1, 1)}}
	belowHalf := []ExactWeightInput{{UID: 145, Score: big.NewRat(1, 1)}, {UID: idleFleetTestUid, Score: big.NewRat(1, 1_000_000_000)}}
	requireRefusal := func(t *testing.T, authority *OwnerRecycleMeasurementAuthority, source *ReleaseMeasurementArtifact, decision *VerifiedReleaseMeasurement, text string) {
		t.Helper()
		row, err := deriveOwnerRecycleMeasuredRow(authority, source, decision)
		if err == nil || !strings.Contains(err.Error(), text) || !reflect.DeepEqual(row, OwnerRecycleMeasuredRow{}) {
			t.Fatalf("row %+v error %v, want refusal containing %q", row, err, text)
		}
	}
	t.Run("a tiny pool", func(t *testing.T) {
		pools := []ExactWeightInput{{UID: idleFleetTestPoolUid, Score: big.NewRat(1, 1)}, {UID: 68, Score: big.NewRat(1, 1_000_000_000)}}
		requireRefusal(t, stage.authority, artifact, idleFleetTestDecision(t, provider, theta, pools, []ExactWeightInput{{UID: 145, Score: big.NewRat(1, 1)}}), "quantization loses")
	})
	t.Run("a tiny pool beside a fleet that could leave", func(t *testing.T) {
		pools := []ExactWeightInput{{UID: idleFleetTestPoolUid, Score: big.NewRat(1, 1)}, {UID: 68, Score: big.NewRat(1, 1_000_000_000)}}
		requireRefusal(t, stage.authority, artifact, idleFleetTestDecision(t, provider, theta, pools, belowHalf), "quantization loses")
	})
	t.Run("a uid that is both a tiny pool and a tiny fleet", func(t *testing.T) {
		pools := []ExactWeightInput{{UID: idleFleetTestPoolUid, Score: big.NewRat(1, 1)}, {UID: idleFleetTestUid, Score: big.NewRat(1, 1_000_000_000)}}
		requireRefusal(t, stage.authority, artifact, idleFleetTestDecision(t, provider, theta, pools, belowHalf), "quantization loses")
	})
	t.Run("the whole head channel", func(t *testing.T) {
		// Every fleet rounds to zero, top rank included; ceding its channel to the
		// pool is not this rule's to do.
		tiny := protocol.Rational{Numerator: 1, Denominator: 1_000_000}
		source := *artifact
		source.Policy.Steering.Theta = tiny
		requireRefusal(t, stage.authority, &source, idleFleetTestDecision(t, provider, tiny, onePool, []ExactWeightInput{{UID: 145, Score: big.NewRat(2, 1)}, {UID: 146, Score: big.NewRat(1, 1)}}), "quantization loses")
	})
	t.Run("the last unmasked fleet below a masked one", func(t *testing.T) {
		tiny := protocol.Rational{Numerator: 1, Denominator: 1_000_000}
		source := *artifact
		source.Policy.Steering.Theta = tiny
		masked := provider.MaskedUIDs[0]
		requireRefusal(t, stage.authority, &source, idleFleetTestDecision(t, provider, tiny, onePool, []ExactWeightInput{{UID: masked, Score: big.NewRat(2, 1)}, {UID: 145, Score: big.NewRat(1, 1)}}), "quantization loses")
	})
	t.Run("a provider row its projections do not reproduce", func(t *testing.T) {
		decision := idleFleetTestDecision(t, provider, theta, onePool, belowHalf)
		decision.SelectedHead = decision.SelectedHead[:1]
		requireRefusal(t, stage.authority, artifact, decision, "")
	})
	t.Run("owner-recycle authority", func(t *testing.T) {
		owner := newOwnerRecycleProductionTestFixture(t)
		ownerStage, ownerProvider := owner.stage(t)
		ownerArtifact := owner.operator.measurement.provider.artifact
		decision := idleFleetTestDecision(t, ownerProvider, ownerArtifact.Policy.Steering.Theta, onePool, belowHalf)
		requireRefusal(t, ownerStage.authority, ownerArtifact, decision, "quantization loses")
	})
}

// Every row the code before this rule accepted has its exact bytes. The hashes
// were computed on sn addb74e8, so this passes there and here; a replay of a
// retained intent compares exactly these bytes.
func TestTreasuryRowBytesOfOlderMeasurements(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	artifact := fixture.operator.measurement.provider.artifact
	theta := artifact.Policy.Steering.Theta
	pools := []ExactWeightInput{{UID: idleFleetTestPoolUid, Score: big.NewRat(1, 1)}}
	for _, test := range idleFleetTestCases {
		t.Run(test.name, func(t *testing.T) {
			hash := sha256.New()
			idle := new(big.Rat).Set(test.idleStart)
			for tempo := 0; tempo < test.leftOutTempo; tempo++ {
				decision := idleFleetTestDecision(t, provider, theta, pools, idleFleetTestHead(test.otherUids, test.otherScore, idle))
				row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, decision)
				if err != nil {
					t.Fatalf("tempo %d: %v", tempo, err)
				}
				raw, err := json.Marshal(row)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(raw, []byte("excluded")) || idleFleetTestExcludedUids(t, row) != nil {
					t.Fatalf("tempo %d: an accepted row changed its wire: %s", tempo, raw)
				}
				hash.Write(raw)
				idle.Mul(idle, big.NewRat(3, 4))
			}
			if got := hex.EncodeToString(hash.Sum(nil)); got != test.acceptedRowsHash {
				t.Fatalf("accepted rows hash %s, want %s", got, test.acceptedRowsHash)
			}
		})
	}
}

// The original row wire: a signed measurement from the code before this rule,
// byte for byte, through a capsule assembled in the original layouts.
func TestOlderTreasuryCapsuleReplaysByteForByte(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	measurement := fixture.operator.measurement
	stage, provider := fixture.stage(t)
	if len(provider.SelectedHead) != 1 {
		t.Fatalf("fixture head changed: %v", headSelectionUIDs(provider.SelectedHead))
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
		Fallback           string         `json:"fallback,omitempty"`
	}
	row := stage.proof.Row
	original := originalRow{Uids: row.Uids, Scores: row.Scores, Values: row.Values, OwnerUids: row.OwnerUids, MaskedOwnerUids: row.MaskedOwnerUids,
		TreasuryUids: row.TreasuryUids, WireProviderShare: row.WireProviderShare, ProviderShareError: row.ProviderShareError, Fallback: row.Fallback}
	wire, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	// Captured from sn addb74e8's stage for this fixture's one-fleet head.
	const addb74e8Row = `{"uids":[0,1,4],"scores":[{"numerator":"9","denominator":"20"},{"numerator":"1","denominator":"10"},{"numerator":"9","denominator":"20"}],"values":[65535,14563,65535],"owner_uids":null,"masked_owner_uids":null,"treasury_uids":[0,4],"wire_provider_share":{"numerator":"14563","denominator":"145633"},"provider_share_error":"-3/1456330"}`
	current, err := json.Marshal(row)
	if err != nil || !bytes.Equal(current, wire) || string(wire) != addb74e8Row {
		t.Fatalf("row wire changed: %v\n%s", err, current)
	}
	capsule := struct {
		Schema              string                     `json:"schema"`
		Status              OwnerRecycleDecisionStatus `json:"status"`
		Approval            []byte                     `json:"approval"`
		Census              []byte                     `json:"census"`
		ProviderMeasurement []byte                     `json:"provider_measurement"`
		OperatorEvidence    []byte                     `json:"operator_evidence,omitempty"`
		Row                 originalRow                `json:"row"`
	}{treasuryMeasurementSchema, OwnerRecycleDecisionBlocked, stage.authority.approval, stage.authority.census, measurement.encoded, stage.authority.operatorEvidence, original}
	older, err := json.Marshal(capsule)
	if err != nil {
		t.Fatal(err)
	}
	older = append(older, '\n')
	// What the current code seals equals the older layout, and replay accepts it.
	sealed, decision, err := SealTreasuryMeasurement(t.Context(), stage.authority, measurement.encoded, fixture.providerOptions(t))
	if err != nil || !bytes.Equal(sealed, older) {
		t.Fatalf("sealed capsule differs from the older layout: %v", err)
	}
	replayed, err := ReplayTreasuryMeasurement(t.Context(), stage.authority, older, fixture.providerOptions(t))
	if err != nil || !reflect.DeepEqual(replayed, decision) {
		t.Fatalf("older capsule did not replay: %v", err)
	}
	if err := VerifyOwnerRecycleDecisionIntent(t.Context(), stage.authority, older, fixture.providerOptions(t), decision); err != nil {
		t.Fatal(err)
	}
	// The signed sidecar, its source commitment and vector hash keep their bytes.
	intent := fixture.intent(t, stage, provider)
	raw, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SteeringIntent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("excluded_head_uids")) || bytes.Contains(stage.encoded, []byte("excluded_head_uids")) {
		t.Fatal("an ordinary sidecar acquired the exclusion marker")
	}
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, &decoded, measurement.encoded, measurement.provider.artifact, provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseMeasurementIntent(&decoded, measurement.provider.artifact, verified); err != nil {
		t.Fatal(err)
	}
}

// A second bound fleet whose provider's signed work was measured under the first
// binding: live, with no hops, and an EMA far below one wire unit.
func idleFleetSignedTestSetup(_ *recycleAdmissionFixture, provider *releaseMeasurementV2TestFixture) {
	fleet, hotkey := [32]byte{2, 31: 2}, [32]byte{3, 31: 2}
	for index := range provider.artifact.Bindings {
		binding := &provider.artifact.Bindings[index]
		if binding.NoID != 9 || !binding.Active {
			continue
		}
		binding.FleetID, binding.Hotkey, binding.Generation = releaseHex32(fleet), releaseHex32(hotkey), 2
		binding.RecordUID, binding.LiveUID = 50, 50
		provider.headEMAPriorKVs = map[FleetScoreKey]*big.Rat{{FleetID: fleet, Hotkey: hotkey, Generation: 2, UID: 50}: big.NewRat(1, 1_000_000)}
		return
	}
}

// A genuine signed measurement with an idle fleet goes through preparation, the
// signed sidecar, the prepared vector and capsule replay. The head result stays
// what every reader of the selection expects, and the row cannot be swapped.
func TestTreasurySignedMeasurementWithIdleFleet(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixtureWithInputs(t, func(t *testing.T, hotkey [32]byte) *recycleOperatorFixture {
		return newRecycleOperatorFixtureWithInputs(t, hotkey, 2, nil, idleFleetSignedTestSetup)
	}, func(fixture *ownerRecycleProductionTestFixture) {
		configureTreasuryProductionTest(t, fixture)
	})
	measurement := fixture.operator.measurement
	artifact := measurement.provider.artifact
	stage, provider := fixture.stage(t)
	if !slices.Equal(headSelectionUIDs(provider.SelectedHead), []uint16{1, 50}) || !slices.Contains(provider.UIDs, 50) {
		t.Fatalf("fixture did not select the idle fleet: %v", headSelectionUIDs(provider.SelectedHead))
	}
	row := stage.proof.Row
	if slices.Contains(row.Uids, 50) || !slices.Contains(row.Uids, 1) || !slices.Equal(idleFleetTestExcludedUids(t, row), []uint16{50}) {
		t.Fatalf("row %v does not leave out and name the idle fleet", row.Uids)
	}
	if !bytes.Contains(stage.encoded, []byte(`"excluded_head_uids":[50]`)) {
		t.Fatal("the proof the source commitment hashes does not name the idle fleet")
	}
	// The public head result still ranks and seats it at the policy maximum.
	intent, envelope := fixture.intentAndEnvelope(t, stage, provider)
	if !slices.Equal(intent.SelectedHeadUIDs, []uint16{1, 50}) || !slices.Equal(intent.UIDs, row.Uids) || !slices.Equal(intent.Prepared.UIDs, row.Uids) || !slices.Equal(intent.Prepared.Values, row.Values) {
		t.Fatalf("intent selected %v uids %v prepared %v", intent.SelectedHeadUIDs, intent.UIDs, intent.Prepared.UIDs)
	}
	if err := ValidateHeadSelectionEvidence(intent.EligibleHeadUIDs, intent.EligibleHeadScores, intent.SelectedHeadUIDs, intent.RejectedHeadUIDs, artifact.Policy.Steering.MaximumHeadFleets); err != nil {
		t.Fatal(err)
	}
	// Every step a restart runs on a retained intent, from a fresh replay of the
	// provider bytes that must rederive the proof byte for byte.
	decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), envelope, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	_, replayed, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), decoded, measurement.encoded, fixture.hotkey.PublicKey(), intent.SelfUID, intent.Prepared.ExtrinsicHash, fixture.providerOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := prepareOwnerRecycleProductionDecision(t.Context(), fixture.cfg, measurement.admission.chain, fixture.operator.chain, measurement.encoded, artifact, replayed.Decision, fixture.providerOptions(t))
	if err != nil || !bytes.Equal(fresh.encoded, stage.encoded) || fresh.sourceHash != stage.sourceHash {
		t.Fatalf("a fresh replay did not rederive the proof: %v", err)
	}
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, fresh, intent, measurement.encoded, artifact, replayed.Decision)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, artifact, verified); err != nil {
		t.Fatal(err)
	}
	if err := measurement.admission.chain.ValidatePreparedSourceWeightsContext(t.Context(), intent.Prepared, verified.UIDs, verified.Scores, releaseSubmitOptions(fixture.cfg)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	var reread SteeringIntent
	if err := json.Unmarshal(raw, &reread); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, &reread, measurement.encoded, artifact, provider); err != nil {
		t.Fatalf("the durable form of the intent did not replay: %v", err)
	}
	encoded, decision, err := SealTreasuryMeasurement(t.Context(), stage.authority, measurement.encoded, fixture.providerOptions(t))
	if err != nil || !reflect.DeepEqual(decision.Row, row) {
		t.Fatalf("capsule row differs: %v", err)
	}
	if err := VerifyOwnerRecycleDecisionIntent(t.Context(), stage.authority, encoded, fixture.providerOptions(t), decision); err != nil {
		t.Fatal(err)
	}
	// A sidecar that hides the exclusion, names another fleet or seats the fleet
	// back in the row differs from the replayed row; the vector hash binds it too.
	original, err := intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"unmarked", "marked-for-another", "fleet-back"} {
		var forged SteeringIntent
		if err := json.Unmarshal(raw, &forged); err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(forged.Treasury.Proof.Row)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(wire, &fields); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "unmarked":
			delete(fields, "excluded_head_uids")
		case "marked-for-another":
			fields["excluded_head_uids"] = json.RawMessage(`[1]`)
		case "fleet-back":
			delete(fields, "excluded_head_uids")
			back, err := json.Marshal(append(slices.Clone(row.Uids), 50))
			if err != nil {
				t.Fatal(err)
			}
			fields["uids"] = back
		}
		wire, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		var changed OwnerRecycleMeasuredRow
		if err := json.Unmarshal(wire, &changed); err != nil {
			t.Fatal(err)
		}
		forged.Treasury.Proof.Row = changed
		if got, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, &forged, measurement.encoded, artifact, provider); got != nil || err == nil {
			t.Fatalf("%s row replaced the replayed one", fault)
		}
		if hash, err := forged.ReconstructedVectorHash(); err != nil || hash == original {
			t.Fatalf("%s row kept the vector hash: %v", fault, err)
		}
	}
}

// The steering journal reports how many fleets a row left out; the row itself
// names them. An unlisted code would reach the journal as diagnostic_unknown.
func TestTreasuryRowExclusionReachesTheJournal(t *testing.T) {
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 1)}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), output, func() time.Time { return time.Unix(1_800_000_000, 0) })
	if err != nil {
		t.Fatal(err)
	}
	releaseDiagnostic(ctx, "steering", "head_fleets_excluded", 26100, true, 2)
	<-output.written
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if line := output.String(); !strings.Contains(line, "domain=steering code=head_fleets_excluded native_epoch=26100 epoch_known=true attempt=2") {
		t.Fatalf("journal line %q", line)
	}
}

// From E_c capture the flag's shares replace theta and an operator's CTF pool
// joins the row (CAPTURETHEFLAG.md §2.1), and an idle fleet must not refuse that
// row either. It is left out and named once it rounds to zero, a tempo or two
// later than at theta: the head channel is 5/100 of the row (7,282 wire units)
// with the CTF pool weighted, and 5/90 (8,091) with the ctf setting off. Every
// other share stays exact, and a lost pool still refuses.
func TestCaptureTheFlagTreasuryRowSurvivesIdleFleetDecay(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	source := fixture.operator.measurement.provider.artifact
	registrations := map[uint16]OwnerRecycleRegistration{}
	for _, registration := range stage.authority.observation.Snapshot.Registrations {
		registrations[registration.Uid] = registration
	}
	const poolUid, ctfPoolUid = uint16(109), uint16(50)
	if _, spare := registrations[ctfPoolUid]; !spare || slices.Contains(stage.proof.Row.TreasuryUids, ctfPoolUid) || source.Pools[0].NoID != 9 || source.Pools[0].UID != poolUid {
		t.Fatal("treasury fixture census changed")
	}
	masked := map[uint16]bool{}
	for _, uid := range provider.MaskedUIDs {
		masked[uid] = true
	}
	// Operator 9's configured entry from epoch 8. Its CTF pool is weighted only
	// under support, as newReleaseCaptureTheFlag resolves it.
	withCtf := func(support bool) *ReleaseMeasurementArtifact {
		artifact := cloneReleaseMeasurementArtifact(t, source)
		artifact.CaptureTheFlag = &ReleaseCaptureTheFlag{Support: support, Pools: []ReleasePoolMeasurement{},
			Entries: []ReleaseAmendmentEntry{ctfTestWire(t, ctfTestEntry(t, artifact.ChainID, artifact.Netuid, 9, 8, nil))}}
		if support {
			artifact.CaptureTheFlag.Pools = []ReleasePoolMeasurement{{NoID: 9, UID: ctfPoolUid, PoolHotkey: releaseHex32(registrations[ctfPoolUid].Hotkey)}}
		}
		return artifact
	}
	// One tempo's decision as assembleReleaseMeasurement makes it from E_c.
	decide := func(pools, ctf, head []ExactWeightInput) *VerifiedReleaseMeasurement {
		selection, err := selectHeadFleets(head, 200)
		if err != nil {
			t.Fatal(err)
		}
		decision := *provider
		decision.EligibleHead = append(slices.Clone(selection.Selected), selection.Rejected...)
		decision.SelectedHead, decision.RejectedHead, decision.Ctf = selection.Selected, selection.Rejected, ctf
		decision.Pools = make([]VerifiedReleasePool, len(pools))
		for index, pool := range pools {
			decision.Pools[index] = VerifiedReleasePool{NoID: uint64(9 + index), UID: pool.UID, Eligible: true, Score: pool.Score}
		}
		decision.UIDs, decision.Scores, err = BuildWeightVectorCtf(pools, ctf, selection.Selected, ctfTestShares, masked)
		if err != nil {
			t.Fatal(err)
		}
		return &decision
	}
	pools := []ExactWeightInput{{UID: poolUid, Score: big.NewRat(1, 1)}}
	weighted := []ExactWeightInput{{UID: ctfPoolUid, Score: big.NewRat(1, 1)}}
	for _, test := range []struct {
		name         string
		support      bool
		ctf          []ExactWeightInput
		otherUids    []uint16
		otherScore   *big.Rat
		poolShare    *big.Rat
		ctfShare     *big.Rat
		headShare    *big.Rat
		leftOutTempo int
	}{
		{name: "CTF pool weighted, one other fleet at EMA 1", support: true, ctf: weighted, otherUids: []uint16{145}, otherScore: big.NewRat(1, 1),
			poolShare: big.NewRat(4, 100), ctfShare: big.NewRat(1, 100), headShare: big.NewRat(5, 100), leftOutTempo: 34},
		{name: "CTF pool weighted, 199 other fleets at EMA 3", support: true, ctf: weighted, otherUids: idleFleetTestOtherUids(), otherScore: big.NewRat(3, 1),
			poolShare: big.NewRat(4, 100), ctfShare: big.NewRat(1, 100), headShare: big.NewRat(5, 100), leftOutTempo: 15},
		{name: "ctf off, one other fleet at EMA 1", support: false, ctf: []ExactWeightInput{}, otherUids: []uint16{145}, otherScore: big.NewRat(1, 1),
			poolShare: big.NewRat(4, 90), ctfShare: new(big.Rat), headShare: big.NewRat(5, 90), leftOutTempo: 34},
	} {
		artifact := withCtf(test.support)
		idle := new(big.Rat).Set(test.otherScore)
		for tempo := 0; tempo <= test.leftOutTempo+4; tempo++ {
			row, err := deriveOwnerRecycleMeasuredRow(stage.authority, artifact, decide(pools, test.ctf, idleFleetTestHead(test.otherUids, test.otherScore, idle)))
			if err != nil {
				t.Fatalf("%s: idle for %d tempos refused the whole row: %v", test.name, tempo, err)
			}
			pool, ctf, head := new(big.Rat), new(big.Rat), new(big.Rat)
			for index, uid := range row.Uids {
				score, err := decodeRationalJSON(row.Scores[index])
				if err != nil {
					t.Fatal(err)
				}
				if row.Values[index] == 0 {
					t.Fatalf("%s: tempo %d: uid %d kept an exact share with a zero wire value", test.name, tempo, uid)
				}
				switch {
				case slices.Contains(row.TreasuryUids, uid):
					if score.Cmp(big.NewRat(9, 20)) != 0 {
						t.Fatalf("%s: tempo %d: recipient %d share %s", test.name, tempo, uid, score)
					}
				case uid == poolUid:
					pool.Add(pool, score)
				case uid == ctfPoolUid:
					ctf.Add(ctf, score)
				default:
					head.Add(head, score)
				}
			}
			if pool.Cmp(test.poolShare) != 0 || ctf.Cmp(test.ctfShare) != 0 || head.Cmp(test.headShare) != 0 {
				t.Fatalf("%s: tempo %d: pool %s, CTF %s, head %s", test.name, tempo, pool, ctf, head)
			}
			for _, uid := range test.otherUids {
				if !slices.Contains(row.Uids, uid) {
					t.Fatalf("%s: tempo %d: busy fleet %d left the row", test.name, tempo, uid)
				}
			}
			leftOut := tempo >= test.leftOutTempo
			if slices.Contains(row.Uids, idleFleetTestUid) == leftOut {
				t.Fatalf("%s: tempo %d: idle fleet in row %v, want left out from tempo %d", test.name, tempo, row.Uids, test.leftOutTempo)
			}
			if excluded := idleFleetTestExcludedUids(t, row); leftOut != slices.Equal(excluded, []uint16{idleFleetTestUid}) || !leftOut && excluded != nil {
				t.Fatalf("%s: tempo %d: row names %v as left out", test.name, tempo, excluded)
			}
			idle.Mul(idle, big.NewRat(3, 4))
		}
	}
	// A lost pool is a real loss beside capture the flag too.
	tiny := append(slices.Clone(pools), ExactWeightInput{UID: 68, Score: big.NewRat(1, 1_000_000_000)})
	belowHalf := []ExactWeightInput{{UID: 145, Score: big.NewRat(1, 1)}, {UID: idleFleetTestUid, Score: big.NewRat(1, 1_000_000_000)}}
	if row, err := deriveOwnerRecycleMeasuredRow(stage.authority, withCtf(true), decide(tiny, weighted, belowHalf)); err == nil || !strings.Contains(err.Error(), "quantization loses") || !reflect.DeepEqual(row, OwnerRecycleMeasuredRow{}) {
		t.Fatalf("a tiny pool beside capture the flag was admitted: %+v %v", row, err)
	}
}
