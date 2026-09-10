package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func providerWaveTestSpecs() []ProcessSpec {
	return []ProcessSpec{
		{ID: "validator", Role: "validator"},
		{ID: "miner-1", Role: "miner-swarm", HealthURL: "http://miner-1/status"},
		{ID: "api", Role: "operator-api", HealthURL: "http://api/status"},
		{ID: "miner-2", Role: "miner-swarm", HealthURL: "http://miner-2/status"},
		{ID: "miner-3", Role: "miner-swarm", HealthURL: "http://miner-3/status"},
		{ID: "claim", Role: "claim-relayer"},
	}
}

func TestSupervisorStartupProviderWavesWaitBeforeNextSwarm(t *testing.T) {
	for _, waveSize := range []int{0, 1} {
		t.Run(fmt.Sprintf("wave-%d", waveSize), func(t *testing.T) {
			var events []string
			start := func(spec ProcessSpec) error {
				events = append(events, "start:"+spec.ID)
				return nil
			}
			wait := func(specs []ProcessSpec) error {
				var ids []string
				for _, spec := range specs {
					ids = append(ids, spec.ID)
				}
				events = append(events, "ready:"+strings.Join(ids, ","))
				return nil
			}
			var err error
			if waveSize == 0 {
				err = startSupervisorSpecsWithReadiness(providerWaveTestSpecs(), start, wait)
			} else {
				err = startSupervisorSpecsWithProviderWaves(providerWaveTestSpecs(), waveSize, start, wait)
			}
			want := []string{"start:api", "ready:api", "start:miner-1", "start:miner-2", "start:miner-3", "ready:miner-1,miner-2,miner-3", "start:validator", "start:claim"}
			if waveSize == 1 {
				want = []string{"start:api", "ready:api", "start:miner-1", "ready:miner-1", "start:miner-2", "ready:miner-2", "start:miner-3", "ready:miner-3", "start:validator", "start:claim"}
			}
			if err != nil || !reflect.DeepEqual(events, want) {
				t.Fatalf("startup events=%v want=%v err=%v", events, want, err)
			}
		})
	}
}

func TestSupervisorStartupProviderWaveFailureStopsLaterSwarmsAndDependents(t *testing.T) {
	for _, failStart := range []bool{false, true} {
		var starts []string
		failure := errors.New("second swarm has no processed registration")
		err := startSupervisorSpecsWithProviderWaves(providerWaveTestSpecs(), 1, func(spec ProcessSpec) error {
			starts = append(starts, spec.ID)
			if failStart && spec.ID == "miner-2" {
				return failure
			}
			return nil
		}, func(phase []ProcessSpec) error {
			if !failStart && phase[0].ID == "miner-2" {
				return failure
			}
			return nil
		})
		if !errors.Is(err, failure) || !reflect.DeepEqual(starts, []string{"api", "miner-1", "miner-2"}) {
			t.Fatalf("failed wave started later work: starts=%v err=%v", starts, err)
		}
	}
}

func TestSupervisorStartupProviderWavesDeferBackgroundRPCWorkers(t *testing.T) {
	for _, waveSize := range []int{0, 1} {
		var events []string
		specs := append(providerWaveTestSpecs(), ProcessSpec{ID: "worker", Role: "operator-taskworker", HealthURL: "http://worker/status"})
		err := startSupervisorSpecsWithProviderWaves(specs, waveSize, func(spec ProcessSpec) error {
			events = append(events, "start:"+spec.ID)
			return nil
		}, func(phase []ProcessSpec) error {
			var ids []string
			for _, spec := range phase {
				ids = append(ids, spec.ID)
			}
			events = append(events, "ready:"+strings.Join(ids, ","))
			return nil
		})
		want := []string{"start:api", "start:worker", "ready:api,worker", "start:miner-1", "start:miner-2", "start:miner-3", "ready:miner-1,miner-2,miner-3", "start:validator", "start:claim"}
		if waveSize == 1 {
			want = []string{"start:api", "ready:api", "start:miner-1", "ready:miner-1", "start:miner-2", "ready:miner-2", "start:miner-3", "ready:miner-3", "start:worker", "ready:worker", "start:validator", "start:claim"}
		}
		if err != nil || !reflect.DeepEqual(events, want) {
			t.Fatalf("wave=%d events=%v want=%v err=%v", waveSize, events, want, err)
		}
	}
}

func TestSupervisorStartupProviderWavesRequireExplicitProvisionalAdmission(t *testing.T) {
	manifest := SupervisorFile{Specs: providerWaveTestSpecs()}
	for _, cfg := range []*ResolvedConfig{nil, {}, {provisionalResume: &provisionalResumeState{}}} {
		if size := provisionalProviderStartupWaveSize(cfg); size != 0 {
			t.Fatalf("unadmitted invocation selected wave size %d", size)
		}
	}
	strictWire, err := json.Marshal(manifest)
	if err != nil || strings.Contains(string(strictWire), "provider_startup_wave_size") || supervisorStartupReadinessTimeout(manifest) != 3*time.Minute {
		t.Fatalf("strict startup manifest/deadline changed: %s %v", strictWire, err)
	}
	cfg := &ResolvedConfig{provisionalResume: &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}}
	manifest.ProviderStartupWaveSize = provisionalProviderStartupWaveSize(cfg)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var reopened SupervisorFile
	if err := json.Unmarshal(encoded, &reopened); err != nil || reopened.ProviderStartupWaveSize != 1 {
		t.Fatalf("internal supervisor lost provisional waves: %v", err)
	}
	if got := supervisorStartupReadinessTimeout(reopened); got != 9*time.Minute {
		t.Fatalf("outer timeout=%s, want three 2-minute waves + 2-minute prerequisites + 1-minute buffer", got)
	}
	for i := 4; i <= 20; i++ {
		reopened.Specs = append(reopened.Specs, ProcessSpec{ID: fmt.Sprintf("miner-%d", i), Role: "miner-swarm"})
	}
	if got := supervisorStartupReadinessTimeout(reopened); got != 43*time.Minute {
		t.Fatalf("20-swarm outer timeout=%s, want all bounded phases covered", got)
	}
	reopened.Specs = append(reopened.Specs, ProcessSpec{ID: "worker", Role: "operator-taskworker"})
	if got := supervisorStartupReadinessTimeout(reopened); got != 45*time.Minute {
		t.Fatalf("outer timeout=%s, want deferred taskworker readiness covered too", got)
	}
}
