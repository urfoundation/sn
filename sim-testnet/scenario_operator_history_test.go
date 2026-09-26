// Oldest-first operator indexes need explicit observation ranges; otherwise a
// full retained prefix freezes live quality counters and hides fresh proofs.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Match the server's stats overlap and proof creation predicates exactly,
// including the mandatory oldest-first limits. All times and rows are synthetic.
func TestScenarioOperatorHistoryExcludesSaturatedRetainedPrefix(t *testing.T) {
	at := time.Date(2030, 1, 2, 3, 4, 5, 123000000, time.UTC)
	statsAssignments := uint64(42)
	proofKey := byte(7)
	probe := &liveScenarioProbe{operatorEvidenceNow: func() time.Time { return at }}
	probe.client = &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/verify/stats" && request.URL.Path != "/verify/proofs" {
			return adversaryGetTestResponse(http.StatusOK, `{}`), nil
		}
		query := request.URL.Query()
		from, to := at.Add(-31*24*time.Hour), at
		for key, dest := range map[string]*time.Time{"from": &from, "to": &to} {
			if raw := query.Get(key); raw != "" {
				parsed, err := time.Parse(time.RFC3339Nano, raw)
				if err != nil {
					return nil, err
				}
				*dest = parsed
			}
		}
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil || limit < 1 {
			return nil, fmt.Errorf("invalid index limit: %q", query.Get("limit"))
		}
		old := at.Add(-24 * time.Hour)
		rows := []map[string]any{}
		schema := "urnetwork-verify-proof-index-v1"
		if request.URL.Path == "/verify/stats" {
			schema = "urnetwork-verify-stats-index-v1"
			if old.Add(15*time.Minute).After(from) && old.Before(to) {
				for range limit {
					rows = append(rows, map[string]any{"assignments": 1, "confirmations": 1})
				}
			}
			if len(rows) < limit && at.Add(-time.Minute).Before(to) && at.Add(time.Minute).After(from) {
				rows = append(rows, map[string]any{"assignments": statsAssignments, "confirmations": statsAssignments - 2})
			}
		} else {
			if !old.Before(from) && old.Before(to) {
				for range limit {
					rows = append(rows, map[string]any{"server_key_id": 1})
				}
			}
			if len(rows) < limit && !at.Add(-time.Minute).Before(from) && at.Add(-time.Minute).Before(to) {
				rows = append(rows, map[string]any{"server_key_id": proofKey})
			}
		}
		body, err := json.Marshal(map[string]any{"schema": schema, "rows": rows})
		return adversaryGetTestResponse(http.StatusOK, string(body)), err
	})}
	for snapshot := 0; snapshot < 2; snapshot++ {
		responses := probe.readOperatorSurfaces(t.Context(), []string{"http://operator.example"})
		var stats struct {
			Rows []struct {
				Assignments uint64 `json:"assignments"`
			} `json:"rows"`
		}
		var proofs struct {
			Rows []struct {
				ServerKeyId byte `json:"server_key_id"`
			} `json:"rows"`
		}
		if err := json.Unmarshal(responses[0][scenarioOperatorStats].data, &stats); err != nil {
			t.Fatal(err)
		}
		if len(stats.Rows) != 1 || stats.Rows[0].Assignments != statsAssignments {
			t.Errorf("snapshot %d live quality counters hidden behind retained prefix: rows=%d", snapshot, len(stats.Rows))
		}
		if err := json.Unmarshal(responses[0][scenarioOperatorProofs].data, &proofs); err != nil {
			t.Fatal(err)
		}
		if len(proofs.Rows) != 1 || proofs.Rows[0].ServerKeyId != proofKey {
			t.Errorf("snapshot %d fresh proof hidden behind retained prefix: rows=%d", snapshot, len(proofs.Rows))
		}
		at = at.Add(20 * time.Minute)
		statsAssignments++
		proofKey++
	}
}

// Stats retain the initial rollup boundary while proof census moves forward.
// Both operators and every retry share the same exclusive upper bound.
func TestScenarioOperatorHistoryPinsBoundariesAcrossRetriesAndSnapshots(t *testing.T) {
	first := time.Date(2030, 1, 2, 3, 4, 5, 123000000, time.UTC)
	at := first
	from := time.Date(2030, 1, 2, 2, 45, 0, 0, time.UTC)
	var stateLock sync.Mutex
	attemptKVs := map[string]int{}
	probe := &liveScenarioProbe{operatorEvidenceNow: func() time.Time { return at }}
	probe.client = &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/verify/stats" && request.URL.Path != "/verify/proofs" {
			return adversaryGetTestResponse(http.StatusOK, `{}`), nil
		}
		wantFrom := from
		if request.URL.Path == "/verify/proofs" {
			wantFrom = at.Add(-15 * time.Minute)
		}
		query := request.URL.Query()
		if query.Get("from") != wantFrom.Format(time.RFC3339Nano) || query.Get("to") != at.Format(time.RFC3339Nano) {
			t.Errorf("operator history window changed: %s, want %s..%s", request.URL, wantFrom, at)
		}
		attempt := func() int {
			stateLock.Lock()
			defer stateLock.Unlock()
			attemptKVs[request.URL.String()]++
			return attemptKVs[request.URL.String()]
		}()
		if attempt == 1 {
			return adversaryGetTestResponse(http.StatusServiceUnavailable, "synthetic retry"), nil
		}
		return adversaryGetTestResponse(http.StatusOK, `{}`), nil
	})}
	for range 2 {
		responses := probe.readOperatorSurfaces(t.Context(), []string{"http://operator-one.example", "http://operator-two.example"})
		for _, surfaces := range responses {
			for _, index := range []int{scenarioOperatorStats, scenarioOperatorProofs} {
				read := surfaces[index]
				if read.err != nil || read.attempts != 2 || !read.to.Equal(at) {
					t.Fatalf("range or retry accounting lost: %+v", read)
				}
			}
		}
		at = at.Add(20 * time.Minute)
	}
}

// A full current page still cannot prove complete totals. The live observation
// retains its range but publishes no counters from a potentially truncated page.
func TestScenarioOperatorHistorySaturationRemainsIncomplete(t *testing.T) {
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	stats := `{"schema":"urnetwork-verify-stats-index-v1","rows":[` + strings.TrimSuffix(strings.Repeat(`{"assignments":1,"confirmations":1},`, 100000), ",") + `]}`
	proofs := `{"schema":"urnetwork-verify-proof-index-v1","rows":[` + strings.TrimSuffix(strings.Repeat(`{"server_key_id":1},`, 10000), ",") + `]}`
	probe := &liveScenarioProbe{cfg: testResolvedConfig(t), stateDir: t.TempDir(), operatorEvidenceNow: func() time.Time { return at }}
	probe.client = &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		switch request.URL.Path {
		case "/verify/stats":
			body = stats
		case "/verify/proofs":
			body = proofs
		case "/sn/artifacts":
			body = `{"schema":"urnetwork-payout-artifact-history-v1","objects":[]}`
		}
		return adversaryGetTestResponse(http.StatusOK, body), nil
	})}
	for _, observation := range probe.inspectOperators(t.Context(), nil, nil, nil) {
		if !strings.Contains(observation.Error, "quality totals are incomplete") || !strings.Contains(observation.Error, "proof census is incomplete") || observation.StatsRows != 0 || observation.ProofRows != 0 || observation.Assignments != 0 || observation.Confirmations != 0 {
			t.Fatalf("saturated evidence accepted: %+v", observation)
		}
		if observation.StatsFrom != "2030-01-02T02:45:00Z" || observation.StatsTo != at.Format(time.RFC3339Nano) || observation.ProofsFrom != "2030-01-02T02:49:05Z" || observation.ProofsTo != observation.StatsTo {
			t.Fatalf("incomplete evidence lost its exact range: %+v", observation)
		}
	}
}

// Clock rollback and overlong history fail before admitting an index read;
// independent status and keys remain observable.
func TestScenarioOperatorHistoryRejectsInvalidRange(t *testing.T) {
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, end := range []time.Time{at.Add(-time.Second), at.Add(94 * 24 * time.Hour)} {
		probe := &liveScenarioProbe{operatorEvidenceStartedAt: at, operatorEvidenceNow: func() time.Time { return end }}
		probe.client = &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/verify/stats" {
				t.Error("invalid stats range admitted a read")
			}
			return adversaryGetTestResponse(http.StatusOK, `{}`), nil
		})}
		responses := probe.readOperatorSurfaces(t.Context(), []string{"http://operator.example"})
		if read := responses[0][scenarioOperatorStats]; read.err == nil || read.attempts != 0 || read.data != nil {
			t.Fatalf("invalid range became accepted evidence: %+v", read)
		}
	}
}
