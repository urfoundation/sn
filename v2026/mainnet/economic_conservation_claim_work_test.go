// Live Claim work distinguishes bounded hot epochs from complete physical
// custody fences. Original payloads are read and decoded only during admission.
package main

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

// The callback can run in a joined source worker. Snapshot every actual stage
// under one lock; the complete total includes all physical owner checks.
type economicConservationClaimWorkSample struct {
	reads  uint64
	stages map[string]uint64
}

// The same public follow path is exercised by small histories and the129 gate.
// Every reported unit remains accounted for, including mandatory owner checks.
func economicConservationClaimFollowCensus(t *testing.T, f *economicConservationArchiveFixture, pages uint64) map[string]uint64 {
	t.Helper()
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	var stateLock sync.Mutex
	work := economicConservationClaimWorkSample{stages: map[string]uint64{}}
	var samples []economicConservationClaimWorkSample
	hooks := monitorServiceHooks{
		historyRead: func(role, stage string) {
			if role == economicConservationRole && stage == "conservation-archive-admission" {
				stateLock.Lock()
				work.reads++
				stateLock.Unlock()
			}
		},
		economicClaimWork: func(_ string, stage string, units uint64) {
			stateLock.Lock()
			work.stages[stage] += units
			stateLock.Unlock()
		},
		afterEvent: func(context.Context, string) {
			stateLock.Lock()
			sample := economicConservationClaimWorkSample{reads: work.reads, stages: map[string]uint64{}}
			for stage, units := range work.stages {
				sample.stages[stage] = units
			}
			stateLock.Unlock()
			samples = append(samples, sample)
			if len(samples) == 3 {
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			f.source.now = f.source.now.Add(time.Second)
			return ctx.Err() == nil
		},
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	if code != 0 || len(samples) != 3 {
		t.Fatal("Claim follow did not join three original samples", pages, code, diagnostic.String(), samples)
	}
	var previous map[string]uint64
	for index, sample := range samples {
		if sample.reads != pages || sample.stages["archive-checkpoint-decoded"] != pages || sample.stages["archive-index-ready"] != 1 {
			t.Fatal("Claim follow repeated or omitted original payload admission", pages, index, sample)
		}
		if index == 0 {
			continue
		}
		delta := map[string]uint64{}
		var hot uint64
		for stage, units := range sample.stages {
			prior := samples[index-1].stages[stage]
			if units < prior {
				t.Fatal("Claim work counter regressed", stage, units, prior)
			}
			if units != prior {
				delta[stage] = units - prior
			}
			if stage != "archive-custody-check" {
				hot += units - prior
			}
		}
		// This finite no-producer fixture has four complete live owner fences.
		// Their units remain inside the unchanged16*128 total foreground limit.
		if delta["archive-custody-check"] != 4*pages || hot == 0 || hot+delta["archive-custody-check"] > 16*128 {
			t.Fatal("Claim follow changed complete custody fences or bounded hot work", pages, index, hot, delta)
		}
		if previous != nil && !reflect.DeepEqual(previous, delta) {
			t.Fatal("Claim follow work changed between unchanged live samples", pages, previous, delta)
		}
		previous = delta
		t.Logf("Claim follow complete census: pages=%d sample=%d total=%d hot=%d stages=%v", pages, index, hot+delta["archive-custody-check"], hot, delta)
	}
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	var summary economicConservationSummary
	if len(lines) != 3 || decodePlanJson(lines[2], &summary) != nil || summary.MatchedReceipts != 1 || summary.TargetMet != nil || !summary.NativeCurrent || !summary.VaultCurrent {
		t.Fatal("long reviewed Claim history did not reopen original combined progress", summary, output.String())
	}
	delete(previous, "archive-custody-check")
	return previous
}

// Each history contains the same128-epoch window and127 unresolved originals.
// Increasing retained reviews must add only complete physical owner fences.
func TestEconomicConservationClaimFollowSeparatesPhysicalCustodyFromHotEpochWork(t *testing.T) {
	var previous map[string]uint64
	for _, pages := range []int{2, 4, 8} {
		f := newEconomicConservationClaimWindowFixture(t, true, false)
		for ordinal := 1; ordinal <= pages; ordinal++ {
			_, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte(fmt.Sprintf("synthetic live Claim work review %d", ordinal))))
			if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
				t.Fatal("actual Claim work history preparation", ordinal, code, issue)
			}
			f.reset(t)
		}
		stages := economicConservationClaimFollowCensus(t, f, uint64(pages))
		if previous != nil && !reflect.DeepEqual(previous, stages) {
			t.Fatal("Claim hot epoch work scaled with retained review history", pages, previous, stages)
		}
		previous = stages
	}
}
