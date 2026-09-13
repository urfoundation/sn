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
	for _, waveSize := range []int{0, 1, 2} {
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
			if waveSize == 2 {
				want = []string{"start:api", "ready:api", "start:miner-1", "start:miner-2", "ready:miner-1,miner-2", "start:miner-3", "ready:miner-3", "start:validator", "start:claim"}
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
	for _, waveSize := range []int{0, 1, 2} {
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
		if waveSize == 2 {
			want = []string{"start:api", "ready:api", "start:miner-1", "start:miner-2", "ready:miner-1,miner-2", "start:miner-3", "ready:miner-3", "start:worker", "ready:worker", "start:validator", "start:claim"}
		}
		if err != nil || !reflect.DeepEqual(events, want) {
			t.Fatalf("wave=%d events=%v want=%v err=%v", waveSize, events, want, err)
		}
	}
}

func TestSupervisorStartupProviderWavesRespectRPCModeAndProvisionalAdmission(t *testing.T) {
	manifest := SupervisorFile{Specs: providerWaveTestSpecs()}
	for _, cfg := range []*ResolvedConfig{
		nil,
		{},
		{OperationalRPCMode: rpcModePrivateAuthority},
		{provisionalResume: &provisionalResumeState{}},
		{provisionalResume: &provisionalResumeState{Record: &provisionalResumeRecord{}}},
	} {
		if size := providerStartupWaveSize(cfg); size != 0 {
			t.Fatalf("unadmitted invocation selected wave size %d", size)
		}
	}
	strictWire, err := json.Marshal(manifest)
	if err != nil || strings.Contains(string(strictWire), "provider_startup_wave_size") || supervisorStartupReadinessTimeout(manifest) != 3*time.Minute {
		t.Fatalf("strict startup manifest/deadline changed: %s %v", strictWire, err)
	}
	cfg := &ResolvedConfig{OperationalRPCMode: rpcModePrivateAuthority, provisionalResume: &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}}
	manifest.ProviderStartupWaveSize = providerStartupWaveSize(cfg)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var reopened SupervisorFile
	if err := json.Unmarshal(encoded, &reopened); err != nil || reopened.ProviderStartupWaveSize != 2 {
		t.Fatalf("internal supervisor lost provisional waves: %v", err)
	}
	if got := supervisorStartupReadinessTimeout(reopened); got != 7*time.Minute {
		t.Fatalf("outer timeout=%s, want two 2-minute waves + 2-minute prerequisites + 1-minute buffer", got)
	}
	for i := 4; i <= 20; i++ {
		reopened.Specs = append(reopened.Specs, ProcessSpec{ID: fmt.Sprintf("miner-%d", i), Role: "miner-swarm"})
	}
	if got := supervisorStartupReadinessTimeout(reopened); got != 23*time.Minute {
		t.Fatalf("20-swarm outer timeout=%s, want all bounded phases covered", got)
	}
	reopened.Specs = append(reopened.Specs, ProcessSpec{ID: "worker", Role: "operator-taskworker"})
	if got := supervisorStartupReadinessTimeout(reopened); got != 25*time.Minute {
		t.Fatalf("outer timeout=%s, want deferred taskworker readiness covered too", got)
	}
}

func TestSupervisorStartupPublicRPCWavesRetainCompleteReadiness(t *testing.T) {
	cfg := &ResolvedConfig{OperationalRPCMode: rpcModePublicOverride}
	manifest := SupervisorFile{ProviderStartupWaveSize: providerStartupWaveSize(cfg)}
	for i := 1; i <= 20; i++ {
		manifest.Specs = append(manifest.Specs, ProcessSpec{ID: fmt.Sprintf("miner-%d", i), Role: "miner-swarm", HealthURL: "http://provider/status"})
	}
	manifest.Specs = append(manifest.Specs,
		ProcessSpec{ID: "api", Role: "operator-api", HealthURL: "http://api/status"},
		ProcessSpec{ID: "worker", Role: "operator-taskworker", HealthURL: "http://worker/status"},
		ProcessSpec{ID: "validator", Role: "validator"},
	)
	wire, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var reopened SupervisorFile
	if err := json.Unmarshal(wire, &reopened); err != nil || reopened.ProviderStartupWaveSize != 2 {
		t.Fatalf("public RPC supervisor lost the two-swarm bound: %v", err)
	}
	started, readyProviders, readyWorker := map[string]bool{}, 0, false
	err = startSupervisorSpecsWithProviderWaves(reopened.Specs, reopened.ProviderStartupWaveSize, func(spec ProcessSpec) error {
		if started[spec.ID] {
			t.Fatalf("duplicate startup: %s", spec.ID)
		}
		if supervisorStartupProvider(spec) && len(started)-1-readyProviders >= 2 {
			t.Fatal("next provider wave started before prior readiness")
		}
		if (spec.ID == "worker" || spec.ID == "validator") && readyProviders != 20 {
			t.Fatal("consumer started before the complete provider population")
		}
		if spec.ID == "validator" && !readyWorker {
			t.Fatal("validator started before worker readiness")
		}
		started[spec.ID] = true
		return nil
	}, func(phase []ProcessSpec) error {
		if supervisorStartupProvider(phase[0]) {
			if len(phase) != 2 {
				t.Fatalf("public RPC provider wave contains %d swarms", len(phase))
			}
			readyProviders += len(phase)
		}
		if phase[0].ID == "worker" {
			readyWorker = true
		}
		return nil
	})
	if err != nil || len(started) != len(reopened.Specs) || readyProviders != 20 {
		t.Fatalf("incomplete public RPC startup: started=%d ready=%d err=%v", len(started), readyProviders, err)
	}
	if got := supervisorStartupReadinessTimeout(reopened); got != 25*time.Minute {
		t.Fatalf("startup timeout %s does not cover ten waves, prerequisites and worker readiness", got)
	}
	if releaseTopologyStartupReadinessTimeout(cfg) != 30*time.Minute || releaseTopologyStartupReadinessTimeout(nil) != 5*time.Minute || releaseTopologyStartupReadinessTimeout(&ResolvedConfig{}) != 5*time.Minute {
		t.Fatal("warm-up budget did not distinguish public RPC from the default")
	}
	// Admission failure keeps both the remaining population and validators shut.
	started = map[string]bool{}
	failure := errors.New("provider wave is not ready")
	err = startSupervisorSpecsWithProviderWaves(reopened.Specs, reopened.ProviderStartupWaveSize, func(spec ProcessSpec) error {
		started[spec.ID] = true
		return nil
	}, func(phase []ProcessSpec) error {
		if supervisorStartupProvider(phase[0]) {
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) || len(started) != 3 || started["worker"] || started["validator"] || started["miner-3"] {
		t.Fatalf("failed first wave admitted later processes: %v, %v", started, err)
	}
}
