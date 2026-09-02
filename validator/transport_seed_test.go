package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

type seedDiscoveryRequest struct {
	Specs []struct {
		BestAvailable bool `json:"best_available"`
	} `json:"specs"`
	Count            int      `json:"count"`
	ExcludeClientIDs []string `json:"exclude_client_ids"`
	RankMode         string   `json:"rank_mode"`
	ForceMinimum     bool     `json:"force_minimum"`
}

// A new operator's connected providers have no latency/speed history until a
// validator can send trails through them. Exercise the real SDK wire binding
// and pin the bootstrap flag that breaks that circular dependency.
func TestFindProvidersSeedPickerRequestsBootstrapCandidates(t *testing.T) {
	selfID := connect.NewId()
	providerID := connect.NewId()
	requests := make(chan seedDiscoveryRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/hello" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if request.URL.Path != "/network/find-providers2" || request.Method != http.MethodPost {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var decoded seedDiscoveryRequest
		if json.Unmarshal(body, &decoded) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- decoded
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"providers":[{"client_id":%q}]}`, providerID.String())
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	settings := connect.DefaultClientStrategySettings()
	settings.EnableNormal = true
	settings.EnableResilient = false
	settings.RequestTimeout = 5 * time.Second
	strategy := connect.NewClientStrategy(ctx, settings)
	api := sdk.NewApi(ctx, strategy, server.URL)
	api.SetByJwt("validator-test-jwt")
	t.Cleanup(func() {
		api.Close()
		cancel()
		_ = api.CloseAndWait(context.Background())
		strategy.Close()
	})

	picked, err := NewFindProvidersSeedPicker(api, selfID)(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if picked != providerID {
		t.Fatalf("picked provider = %s, want %s", picked, providerID)
	}
	request := <-requests
	if request.Count != 8 || request.RankMode != "quality" || !request.ForceMinimum || len(request.Specs) != 1 || !request.Specs[0].BestAvailable {
		t.Fatalf("seed discovery request = %+v", request)
	}
	if len(request.ExcludeClientIDs) != 1 || request.ExcludeClientIDs[0] != selfID.String() {
		t.Fatalf("seed discovery exclusions = %v, want [%s]", request.ExcludeClientIDs, selfID)
	}
}
