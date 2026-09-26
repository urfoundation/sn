// Operator observation surfaces have independent bounded read owners, so a
// temporary endpoint outage cannot discard another surface's completed read.
package main

import (
	"context"
	"fmt"
	"net/url"
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

const (
	scenarioOperatorStatsMaximumRows = 100000
	scenarioOperatorProofMaximumRows = 10000
	scenarioOperatorStatsPeriod      = 15 * time.Minute
)

type scenarioOperatorRead struct {
	data              []byte
	status            int
	err               error
	attempts          uint64
	transientFailures uint64
	from              time.Time
	to                time.Time
}

type scenarioOperatorSurfaces [scenarioOperatorSurfaceCount]scenarioOperatorRead

// Independent observation endpoints share one bounded worker pool across all
// operators. In particular, four slow stats/proof reads share one retry
// interval. Every response and error retains its original operator and surface;
// a timeout never supplies cached counters or authenticated success.
func (self *liveScenarioProbe) readOperatorSurfaces(ctx context.Context, bases []string) []scenarioOperatorSurfaces {
	now := time.Now
	if self.operatorEvidenceNow != nil {
		now = self.operatorEvidenceNow
	}
	to := now().UTC()
	if self.operatorEvidenceStartedAt.IsZero() {
		self.operatorEvidenceStartedAt = to
	}
	// Stats are overlapping 15-minute rollups. Retain the preceding complete
	// period for the baseline, then keep the same start throughout this run.
	statsFrom := self.operatorEvidenceStartedAt.Truncate(scenarioOperatorStatsPeriod).Add(-scenarioOperatorStatsPeriod)
	proofsFrom := to.Add(-scenarioOperatorStatsPeriod)
	requests := [scenarioOperatorSurfaceCount]struct {
		path  string
		limit int64
		rows  int
		from  time.Time
	}{
		{path: "/status", limit: 1024 * 1024},
		{path: "/verify/keys", limit: 1024 * 1024},
		{path: "/verify/stats", limit: 32 * 1024 * 1024, rows: scenarioOperatorStatsMaximumRows, from: statsFrom},
		{path: "/verify/proofs", limit: 32 * 1024 * 1024, rows: scenarioOperatorProofMaximumRows, from: proofsFrom},
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
				if request.rows != 0 {
					if to.IsZero() || to.Before(self.operatorEvidenceStartedAt) || !request.from.Before(to) || to.Sub(request.from) > 93*24*time.Hour {
						result[operator][surface] = scenarioOperatorRead{err: fmt.Errorf("%s observation has an invalid time range", request.path), from: request.from, to: to}
						continue
					}
					query := url.Values{"from": {request.from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}, "limit": {fmt.Sprint(request.rows)}}
					request.path += "?" + query.Encode()
				}
				read := self.readOperatorSurface(ctx, strings.TrimSuffix(bases[operator], "/")+request.path, request.limit)
				if request.rows != 0 {
					read.from, read.to = request.from, to
				}
				result[operator][surface] = read
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
