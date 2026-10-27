// Trails fail every couple of seconds while a network problem lasts, so their
// stages are summarized instead of logged one by one: at most one line per
// window per release instance, with counts per kind and stage, closed causes,
// the hops' contract outcomes, HTTP statuses and the depth each failed trail
// reached. Every token in the line is a closed label or a number; no client
// id, address, URL or error text is ever written. A partial window still open
// at shutdown is not written.
package validator

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026"
	connectprotocol "github.com/urnetwork/connect/v2026/protocol"
)

const (
	trailDiagnosticSummaryWindow       = time.Minute
	trailDiagnosticSummaryMaximumBytes = 1024
	trailDiagnosticSummaryStatuses     = 4
)

// Indices of the closed kind labels used by trailFailureFacts.
var trailDiagnosticSummaryKinds = [...]string{"seed", "hop", "protocol", "unknown_final", "unknown"}

func trailDiagnosticSummaryKind(kind string) int {
	for index, name := range trailDiagnosticSummaryKinds {
		if name == kind {
			return index
		}
	}
	return len(trailDiagnosticSummaryKinds) - 1
}

var trailDiagnosticSummaryCauses = [...]string{"timeout", "transport", "canceled", "unknown"}

func trailDiagnosticSummaryCause(cause string) int {
	for index, name := range trailDiagnosticSummaryCauses {
		if name == cause {
			return index
		}
	}
	return len(trailDiagnosticSummaryCauses) - 1
}

// Contract outcomes: unanswered, granted, the six refusal codes, then other.
var trailDiagnosticSummaryContracts = [...]string{"unanswered", "granted", "no_permission", "insufficient_balance", "setup", "trust", "invalid", "reliability", "refused_other"}

func trailDiagnosticSummaryContract(facts *trailFailureFacts) (int, bool) {
	switch facts.contract {
	case trailContractUnanswered:
		return 0, true
	case trailContractGranted:
		return 1, true
	case trailContractRefused:
		code := int(facts.contractCode)
		if int(connectprotocol.ContractError_NoPermission) <= code && code <= int(connectprotocol.ContractError_Reliability) {
			return 2 + code, true
		}
		return len(trailDiagnosticSummaryContracts) - 1, true
	}
	return 0, false
}

type trailDiagnosticSummaryStatus struct {
	code  int
	count uint64
}

// One window's counts, guarded by the summary's mutex for concurrent workers.
type trailDiagnosticSummary struct {
	mutex  sync.Mutex
	window trailDiagnosticSummaryWindowCounts
}

type trailDiagnosticSummaryWindowCounts struct {
	started     time.Time
	complete    uint64
	failed      uint64
	stages      [len(trailDiagnosticSummaryKinds)][trailStageCount]uint64
	causes      [len(trailDiagnosticSummaryCauses)]uint64
	contracts   [len(trailDiagnosticSummaryContracts)]uint64
	statuses    []trailDiagnosticSummaryStatus
	otherStatus uint64
	reached     [connect.VerifyMMax + 1]uint64
}

// Counts one finished trail (facts nil for a completion) and returns the
// window's line once the window has elapsed, starting the next window.
func (summary *trailDiagnosticSummary) observe(now time.Time, facts *trailFailureFacts) []byte {
	summary.mutex.Lock()
	defer summary.mutex.Unlock()
	self := &summary.window
	if self.started.IsZero() {
		self.started = now
	}
	if facts == nil {
		self.complete++
	} else {
		self.failed++
		stage := facts.stage
		if trailStageCount <= stage {
			stage = trailStageUnknown
		}
		self.stages[trailDiagnosticSummaryKind(facts.kind)][stage]++
		self.causes[trailDiagnosticSummaryCause(facts.cause)]++
		if index, ok := trailDiagnosticSummaryContract(facts); ok {
			self.contracts[index]++
		}
		if facts.status != 0 {
			self.countStatus(facts.status)
		}
		self.reached[min(max(facts.depth, 0), len(self.reached)-1)]++
	}
	elapsed := now.Sub(self.started)
	if elapsed < trailDiagnosticSummaryWindow {
		return nil
	}
	line := self.format(elapsed)
	summary.window = trailDiagnosticSummaryWindowCounts{started: now}
	return line
}

func (self *trailDiagnosticSummaryWindowCounts) countStatus(code int) {
	for index := range self.statuses {
		if self.statuses[index].code == code {
			self.statuses[index].count++
			return
		}
	}
	if len(self.statuses) < trailDiagnosticSummaryStatuses && 100 <= code && code <= 599 {
		self.statuses = append(self.statuses, trailDiagnosticSummaryStatus{code: code, count: 1})
		return
	}
	self.otherStatus++
}

// Fixed order, nonzero counts only, segments dropped whole past the bound.
func (self *trailDiagnosticSummaryWindowCounts) format(elapsed time.Duration) []byte {
	segments := []string{}
	for kind, name := range trailDiagnosticSummaryKinds {
		pairs := []string{}
		for stage := trailStage(0); stage < trailStageCount; stage++ {
			if count := self.stages[kind][stage]; count != 0 {
				pairs = append(pairs, fmt.Sprintf("%s=%d", stage, count))
			}
		}
		if len(pairs) != 0 {
			segments = append(segments, name+":"+strings.Join(pairs, ","))
		}
	}
	appendCounts := func(label string, names []string, counts []uint64) {
		pairs := []string{}
		for index, count := range counts {
			if count != 0 {
				pairs = append(pairs, fmt.Sprintf("%s=%d", names[index], count))
			}
		}
		if len(pairs) != 0 {
			segments = append(segments, label+":"+strings.Join(pairs, ","))
		}
	}
	appendCounts("causes", trailDiagnosticSummaryCauses[:], self.causes[:])
	appendCounts("contracts", trailDiagnosticSummaryContracts[:], self.contracts[:])
	if len(self.statuses) != 0 || self.otherStatus != 0 {
		statuses := append([]trailDiagnosticSummaryStatus(nil), self.statuses...)
		sort.Slice(statuses, func(i, j int) bool { return statuses[i].code < statuses[j].code })
		pairs := make([]string, 0, len(statuses)+1)
		for _, status := range statuses {
			pairs = append(pairs, fmt.Sprintf("%d=%d", status.code, status.count))
		}
		if self.otherStatus != 0 {
			pairs = append(pairs, fmt.Sprintf("other=%d", self.otherStatus))
		}
		segments = append(segments, "http:"+strings.Join(pairs, ","))
	}
	reached := []string{}
	for depth, count := range self.reached {
		if count != 0 {
			reached = append(reached, fmt.Sprintf("%d=%d", depth, count))
		}
	}
	if len(reached) != 0 {
		segments = append(segments, "reached:"+strings.Join(reached, ","))
	}
	var line strings.Builder
	fmt.Fprintf(&line, "validator trail summary: window_s=%d complete=%d failed=%d", int64(elapsed/time.Second), self.complete, self.failed)
	for _, segment := range segments {
		if line.Len()+1+len(segment)+len(" truncated=true\n") > trailDiagnosticSummaryMaximumBytes {
			line.WriteString(" truncated=true")
			break
		}
		line.WriteString(" " + segment)
	}
	line.WriteString("\n")
	return []byte(line.String())
}
