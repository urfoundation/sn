// Operator observation surfaces have independent bounded read owners, so a
// temporary endpoint outage cannot discard another surface's completed read.
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

const (
	scenarioOperatorStatus = iota
	scenarioOperatorKeys
	scenarioOperatorStats
	scenarioOperatorProofs
	scenarioOperatorSurfaceCount
)

const scenarioOperatorReadConcurrency = 4

type scenarioOperatorRead struct {
	data              []byte
	status            int
	err               error
	attempts          uint64
	transientFailures uint64
}

type scenarioOperatorSurfaces [scenarioOperatorSurfaceCount]scenarioOperatorRead

// Independent observation endpoints share one bounded worker pool across all
// operators. In particular, four slow stats/proof reads share one retry
// interval. Every response and error retains its original operator and surface;
// a timeout never supplies cached counters or authenticated success.
func (self *liveScenarioProbe) readOperatorSurfaces(ctx context.Context, bases []string) []scenarioOperatorSurfaces {
	requests := [scenarioOperatorSurfaceCount]struct {
		path  string
		limit int64
	}{
		{path: "/status", limit: 1024 * 1024},
		{path: "/verify/keys", limit: 1024 * 1024},
		{path: "/verify/stats?limit=100000", limit: 32 * 1024 * 1024},
		{path: "/verify/proofs?limit=10000", limit: 32 * 1024 * 1024},
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
				result[operator][surface] = self.readOperatorSurface(ctx, strings.TrimSuffix(bases[operator], "/")+request.path, request.limit)
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
