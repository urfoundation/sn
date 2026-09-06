// Dependency controls observe the same full cold graph as the wire goldens;
// they add no seal, proof shortcut, reduced population or out-of-band warmup.
package main

import (
	"reflect"
	"testing"
)

// Require every real owner and boundary before interpreting dependency order.
// This also pins the historical two-validator/five-epoch/two-operator topology.
func finalSemanticFixtureMeasurementTestEvents(t *testing.T) []finalSemanticFixtureMeasurementWorkEvent {
	t.Helper()
	source, _ := finalSemanticFixture(t)
	if source.ExpectedMiners != 1000 || source.ExpectedCandidates != 202 || source.ExpectedHeadSlots != 200 || source.ExpectedOperators != 2 || source.ExpectedValidators != 2 || len(source.Validators) != 2 {
		t.Fatal("measurement dependency fixture lost the complete release census")
	}
	for index, validator := range source.Validators {
		if validator.ValidatorID != uint64(index+1) || len(validator.Cycles) != 5 {
			t.Fatal("measurement dependency fixture changed validator or epoch ownership")
		}
		for cycleIndex, cycle := range validator.Cycles {
			if cycle.SettlementEpoch != uint64(10+cycleIndex) {
				t.Fatal("measurement dependency fixture changed chronological epochs")
			}
		}
	}
	events := finalSemanticFixtureMeasurementEvents()
	if len(events) != 70 {
		t.Fatalf("measurement dependency events=%d, want complete70", len(events))
	}
	seen := map[finalSemanticFixtureMeasurementWorkEvent]bool{}
	counts := map[string]int{}
	for _, event := range events {
		if event.validatorID < 1 || event.validatorID > 2 || event.settlementEpoch < 10 || event.settlementEpoch > 14 || seen[event] {
			t.Fatalf("measurement observation owner is invalid or repeated: %+v", event)
		}
		prior := event
		switch event.stage {
		case finalSemanticFixtureMeasurementReady:
			if event.noID != 0 {
				t.Fatalf("measurement has an operator-only identity: %+v", event)
			}
			if event.settlementEpoch > 10 {
				prior.settlementEpoch--
				if !seen[prior] {
					t.Fatalf("measurement chronology skipped its predecessor: %+v", event)
				}
			}
		case finalSemanticFixtureEnvelopeStart:
			prior.stage = finalSemanticFixtureMeasurementReady
			if event.noID != 0 || !seen[prior] {
				t.Fatalf("envelope entered without its full measurement seal: %+v", event)
			}
		case finalSemanticFixtureEnvelopeEnd:
			prior.stage = finalSemanticFixtureEnvelopeStart
			if event.noID != 0 || !seen[prior] {
				t.Fatalf("envelope completed without its actual entry: %+v", event)
			}
		case finalSemanticFixtureOperatorPrepared:
			if event.noID < 1 || event.noID > 2 {
				t.Fatalf("operator preparation identity is invalid: %+v", event)
			}
		case finalSemanticFixtureOperatorBody:
			prior.stage = finalSemanticFixtureOperatorPrepared
			if event.noID < 1 || event.noID > 2 || !seen[prior] {
				t.Fatalf("operator body entered without its private identity: %+v", event)
			}
		default:
			t.Fatalf("unknown measurement boundary: %+v", event)
		}
		seen[event] = true
		counts[event.stage]++
	}
	for stage, count := range map[string]int{finalSemanticFixtureMeasurementReady: 10, finalSemanticFixtureEnvelopeStart: 10, finalSemanticFixtureEnvelopeEnd: 10, finalSemanticFixtureOperatorPrepared: 20, finalSemanticFixtureOperatorBody: 20} {
		if counts[stage] != count {
			t.Fatalf("measurement boundary %s=%d, want complete%d", stage, counts[stage], count)
		}
	}
	return events
}

// The following epoch depends on the fully sealed measurement, not on the
// independently authenticated envelope or native intent for an earlier epoch.
func TestFinalSemanticFixtureMeasurementLanesDetachEnvelopeWork(t *testing.T) {
	t.Parallel()
	events := finalSemanticFixtureMeasurementTestEvents(t)
	ready := 0
	for _, event := range events {
		if event.stage == finalSemanticFixtureMeasurementReady {
			ready++
		}
		if event.stage != finalSemanticFixtureEnvelopeStart {
			continue
		}
		t.Logf("actual first envelope: validator=%d epoch=%d authenticated_measurements_ready=%d of10", event.validatorID, event.settlementEpoch, ready)
		if ready != 10 {
			t.Fatalf("fixture envelope authentication still blocks chronological measurement preparation: ready=%d, want10", ready)
		}
		return
	}
	t.Fatal("complete measurement graph has no actual envelope entry")
}

// Both private inputs can be admitted after complete shared batch verification
// and before either independent operator consumes its ledger/record body.
func TestFinalSemanticFixturePreparesBothOperatorOwnersBeforeRecordWork(t *testing.T) {
	t.Parallel()
	events := finalSemanticFixtureMeasurementTestEvents(t)
	prepared := map[[2]uint64]int{}
	firstBody := map[[2]uint64]bool{}
	violations := 0
	for _, event := range events {
		key := [2]uint64{event.validatorID, event.settlementEpoch}
		if event.stage == finalSemanticFixtureOperatorPrepared {
			prepared[key]++
		}
		if event.stage != finalSemanticFixtureOperatorBody || firstBody[key] {
			continue
		}
		firstBody[key] = true
		t.Logf("actual first operator body: validator=%d epoch=%d no=%d private_owners_prepared=%d of2", event.validatorID, event.settlementEpoch, event.noID, prepared[key])
		if prepared[key] != 2 {
			violations++
		}
	}
	if len(firstBody) != 10 {
		t.Fatalf("operator record-body owner epochs=%d, want10", len(firstBody))
	}
	if violations != 0 {
		t.Fatalf("fixture operator record work starts before both private input owners are prepared: violations=%d of10", violations)
	}
}

// Exact reached boundaries and reader ownership are independent controls for
// the two causal assertions; no callback can replace a validator's result.
func TestFinalSemanticFixtureMeasurementObservationIsCompleteAndDetached(t *testing.T) {
	t.Parallel()
	first := finalSemanticFixtureMeasurementTestEvents(t)
	second := finalSemanticFixtureMeasurementEvents()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("measurement readers differ before any mutation")
	}
	first[0].stage = "mutated-reader"
	first[0].validatorID++
	first[0].settlementEpoch++
	first[0].noID++
	if !reflect.DeepEqual(second, finalSemanticFixtureMeasurementEvents()) {
		t.Fatal("measurement observation reader changed the published cold graph")
	}
	audit := &finalSemanticFixtureMeasurementAudit{}
	audit.observe(second[0])
	owned := audit.snapshot()
	owned[0].stage = "mutated-private-snapshot"
	if !reflect.DeepEqual(audit.snapshot(), second[:1]) {
		t.Fatal("measurement audit snapshot aliases its call-local owner")
	}
}
