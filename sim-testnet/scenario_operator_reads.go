package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	scenarioOperatorStatus = iota
	scenarioOperatorKeys
	scenarioOperatorStats
	scenarioOperatorProofs
	scenarioOperatorSurfaceCount
)

const scenarioOperatorReadConcurrency = 4
const scenarioOperatorReadTimeout = 30 * time.Second

type scenarioOperatorRead struct {
	data   []byte
	status int
	err    error
}

type scenarioOperatorSurfaces [scenarioOperatorSurfaceCount]scenarioOperatorRead

// Independent observation endpoints share one bounded worker pool across all
// operators. In particular, four slow stats/proof reads consume one timeout
// interval. Every response and error retains its original operator and surface;
// a timeout never supplies cached counters or authenticated success.
func (p *liveScenarioProbe) readOperatorSurfaces(ctx context.Context, bases []string) []scenarioOperatorSurfaces {
	requests := [scenarioOperatorSurfaceCount]struct {
		path  string
		limit int64
	}{
		{path: "/status", limit: 1 << 20},
		{path: "/verify/keys", limit: 1 << 20},
		{path: "/verify/stats?limit=100000", limit: 32 << 20},
		{path: "/verify/proofs?limit=10000", limit: 32 << 20},
	}
	result := make([]scenarioOperatorSurfaces, len(bases))
	jobs := make(chan int, len(bases)*scenarioOperatorSurfaceCount)
	for index := 0; index < cap(jobs); index++ {
		jobs <- index
	}
	close(jobs)
	var workers sync.WaitGroup
	for worker := 0; worker < min(scenarioOperatorReadConcurrency, cap(jobs)); worker++ {
		workers.Go(func() {
			for index := range jobs {
				operator, surface := index/scenarioOperatorSurfaceCount, index%scenarioOperatorSurfaceCount
				request := requests[surface]
				requestCtx, cancel := context.WithTimeout(ctx, scenarioOperatorReadTimeout)
				data, status, err := p.get(requestCtx, strings.TrimSuffix(bases[operator], "/")+request.path, request.limit)
				cancel()
				result[operator][surface] = scenarioOperatorRead{data: data, status: status, err: err}
			}
		})
	}
	workers.Wait()
	return result
}

func (p *liveScenarioProbe) inspectOperators(ctx context.Context, contracts *ContractView, expectedSigners map[int]string, minerClients map[[16]byte]int) []OperatorObservation {
	bases := make([]string, p.cfg.Config.Topology.Operators)
	for index := range bases {
		bases[index] = fmt.Sprintf("http://127.0.0.1:%d", 18081+index)
	}
	surfaces := p.readOperatorSurfaces(ctx, bases)
	result := make([]OperatorObservation, len(bases))
	for index, base := range bases {
		// Projection and artifact authentication remain ordered. In particular,
		// the process-local payout cache still has only one owner.
		result[index] = p.inspectOperatorWithSurfaces(ctx, contracts, index+1, expectedSigners[index+1], base, minerClients, surfaces[index])
	}
	return result
}
