// Proof-history observations must exclude a saturated oldest-first prefix while
// retaining exact-one validation and rejecting pages that may be truncated.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// The endpoint's real oldest-first limit excludes a fresh proof unless the
// caller binds its query to the signed walk. No timing or network is involved.
func TestVerifyProofHistoryBoundsFreshWalkAgainstSaturatedHistory(t *testing.T) {
	started := time.Date(2030, 1, 2, 3, 4, 5, 600000000, time.FixedZone("synthetic", 3*60*60))
	completed := started.Add(2500 * time.Millisecond)
	trailId := connect.Id{0xff, 0xee}
	type row struct {
		TrailId    connect.Id `json:"trail_id"`
		CreateTime time.Time  `json:"create_time"`
	}
	rows := make([]row, 10001)
	for index := range rows[:10000] {
		var id connect.Id
		binary.BigEndian.PutUint64(id[8:], uint64(index+1))
		rows[index] = row{TrailId: id, CreateTime: started.Add(-24 * time.Hour).Add(time.Duration(index) * time.Millisecond)}
	}
	rows[10000] = row{TrailId: trailId, CreateTime: started.Add(time.Millisecond)}
	var observedFrom, observedTo time.Time
	calls := 0
	actor := &verifyAdversary{http: adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.Path != "/verify/proofs" {
			t.Fatalf("unexpected proof request: %s %s", request.Method, request.URL.Path)
		}
		query := request.URL.Query()
		from, to := started.Add(-31*24*time.Hour), completed.Add(time.Second)
		var err error
		if query.Get("from") != "" {
			from, err = time.Parse(time.RFC3339, query.Get("from"))
			if err != nil {
				t.Fatal(err)
			}
			observedFrom = from
		}
		if query.Get("to") != "" {
			to, err = time.Parse(time.RFC3339, query.Get("to"))
			if err != nil {
				t.Fatal(err)
			}
			observedTo = to
		}
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil || limit != 10000 {
			t.Fatalf("invalid bounded proof limit: %q", query.Get("limit"))
		}
		var selected []row
		for _, value := range rows {
			if !value.CreateTime.Before(from) && value.CreateTime.Before(to) {
				selected = append(selected, value)
				if len(selected) == limit {
					break
				}
			}
		}
		body, err := json.Marshal(map[string]any{"schema": "urnetwork-verify-proof-index-v1", "rows": selected})
		if err != nil {
			t.Fatal(err)
		}
		return adversaryGetTestResponse(http.StatusOK, string(body)), nil
	})}
	requests, err := actor.requireUniqueProof(t.Context(), 1, trailId, started, completed)
	if err != nil || requests != 1 || calls != 1 {
		t.Fatalf("fresh signed walk proof lost behind old history: requests=%d calls=%d err=%v", requests, calls, err)
	}
	if !observedFrom.Equal(started.UTC().Truncate(time.Second)) || !observedTo.Equal(completed.UTC().Truncate(time.Second).Add(time.Second)) || observedFrom.Location() != time.UTC || observedTo.Location() != time.UTC {
		t.Fatalf("walk range was not rounded outward in UTC: %v..%v", observedFrom, observedTo)
	}
}

// A full bounded page cannot prove uniqueness; missing/duplicate results also
// remain integrity failures even while that operator has a scheduled outage.
func TestVerifyProofHistoryRetainsStrictUniquenessAndLimit(t *testing.T) {
	trailId := connect.Id{0xee, 0xff}
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	window := newAdversaryFaultWindow(time.Minute)
	window.Update([]string{"operator-1-api"})
	for _, test := range []struct {
		name    string
		rows    int
		matches int
		want    string
	}{
		{name: "missing", rows: 5, matches: 0, want: "appears 0 times"},
		{name: "duplicate", rows: 5, matches: 2, want: "appears 2 times"},
		{name: "saturated", rows: 10000, matches: 1, want: "reached its row limit"},
	} {
		rows := make([]map[string]connect.Id, test.rows)
		for index := range rows {
			id := connect.Id{0x11}
			binary.BigEndian.PutUint64(id[8:], uint64(index+1))
			if index < test.matches {
				id = trailId
			}
			rows[index] = map[string]connect.Id{"trail_id": id}
		}
		body, err := json.Marshal(map[string]any{"schema": "urnetwork-verify-proof-index-v1", "rows": rows})
		if err != nil {
			t.Fatal(err)
		}
		actor := &verifyAdversary{faults: window, http: adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
			return adversaryGetTestResponse(http.StatusOK, string(body)), nil
		})}
		requests, err := actor.requireUniqueProof(t.Context(), 1, trailId, at, at.Add(time.Second))
		var integrity *adversaryReadIntegrityError
		if !errors.As(err, &integrity) || !strings.Contains(err.Error(), test.want) || requests != 1 {
			t.Fatalf("%s proof result lost integrity: requests=%d err=%v", test.name, requests, err)
		}
		if actor.sampleError(1, err, requests, 1).Outcome != adversaryOutcomeError {
			t.Fatalf("%s proof integrity was attributed to a fault", test.name)
		}
	}
}

// Clock rollback and absent start identity cannot select an unbounded history.
func TestVerifyProofHistoryRejectsInvalidWalkRange(t *testing.T) {
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	actor := &verifyAdversary{http: adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid walk performed a request")
		return nil, nil
	})}
	for _, test := range []struct{ started, completed time.Time }{
		{started: time.Time{}, completed: at},
		{started: at, completed: at.Add(-time.Second)},
		{started: at, completed: at.Add(94 * 24 * time.Hour)},
	} {
		requests, err := actor.requireUniqueProof(t.Context(), 1, connect.Id{}, test.started, test.completed)
		if err == nil || requests != 0 {
			t.Fatalf("invalid walk range accepted: requests=%d err=%v", requests, err)
		}
	}
}
