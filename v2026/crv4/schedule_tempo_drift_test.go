// Exact source transition vectors distinguish v470's tempo drift fallback from
// the retained legacy scheduler, including pre-reveal and post-coinbase phases.
package crv4

import (
	"encoding/json"
	"testing"
	"time"
)

// JSON supplies the explicit new profile without making baseline tests depend
// on a new Go symbol. Before the fix that profile was silently ineffective.
func tempoDriftScheduleFixture(t *testing.T, blocksSince uint64) EpochScheduleState {
	t.Helper()
	state := EpochScheduleState{LastEpochBlock: 1000, SubnetEpochIndex: 7, Tempo: 100, BlocksSinceLastStep: blocksSince, CurrentBlock: 1000}
	if err := json.Unmarshal([]byte(`{"epoch_schedule_profile":"urnetwork-subtensor-tempo-drift-v1"}`), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// Runtime coinbase increments before testing strict >tempo. At equality,
// reveal lookahead remains epoch 7 but initialization advances the commit to 8.
func TestTempoDriftScheduleUsesStrictTempoAndExecutionPhase(t *testing.T) {
	for _, count := range []uint64{99, 100, 101, 150} {
		state := tempoDriftScheduleFixture(t, count)
		beforeEpoch := currentEpochPreRunCoinbase(&state, 1001)
		after := simulateRunCoinbase(&state, 1001)
		wantBefore, wantAfter := uint64(7), uint64(7)
		if count > 100 {
			wantBefore = 8
		}
		if count >= 100 {
			wantAfter = 8
		}
		if beforeEpoch != wantBefore || after.SubnetEpochIndex != wantAfter || count >= 100 && (after.LastEpochBlock != 1001 || after.BlocksSinceLastStep != 0) {
			t.Fatalf("tempo drift count=%d pre=%d post=%+v; want pre=%d post epoch=%d", count, beforeEpoch, after, wantBefore, wantAfter)
		}
	}
}

// Literal expectations follow block_step: reveal before coinbase; commit after
// initialization. A change of threshold alone incorrectly returns 1002 at B==tempo.
func TestTempoDriftSchedulePredictsRevealAfterTempoReset(t *testing.T) {
	for _, vector := range []struct {
		count  uint64
		period uint64
		want   uint64
	}{{count: 99, period: 1, want: 1003}, {count: 100, period: 1, want: 1101}, {count: 101, period: 1, want: 1101}, {count: 150, period: 2, want: 1201}} {
		state := tempoDriftScheduleFixture(t, vector.count)
		reveal, err := PredictFirstRevealBlock(&state, vector.period)
		if err != nil || reveal != vector.want {
			t.Fatalf("tempo drift count=%d period=%d reveal=%d want=%d err=%v", vector.count, vector.period, reveal, vector.want, err)
		}
	}
}

// The corrected reveal block must reach the real timelock-round calculation;
// identical wall time, 12-second blocks and three-block offset give round 416.
func TestTempoDriftScheduleSelectsExactDrandRound(t *testing.T) {
	state := tempoDriftScheduleFixture(t, 100)
	round, reveal, err := RevealRound(time.Unix(int64(DrandGenesisTime), 0), &state, 1, 12)
	if err != nil || reveal != 1101 || round != 416 {
		t.Fatalf("tempo drift timelock target round=%d reveal=%d err=%v", round, reveal, err)
	}
}
