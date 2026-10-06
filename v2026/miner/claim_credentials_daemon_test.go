//go:build linux

// Real daemon startup reconciles retained original transaction bytes even when
// provider credentials are absent. Fresh proof/signing work remains held.
package miner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestClaimDaemonMissingProviderCredentialPreservesSignedReconciliation(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		cfg, _, signed, receipt, block := signedClaimFixture(t, 70, 23)
		cfg.SchemaVersion, cfg.Release, cfg.PollSeconds, cfg.LookbackEpochs = 1, "1.0", 5, 2
		cfg.RPC = []string{claimReceiptIdentityTestRPC(t, receipt, block)}
		state := t.TempDir()
		t.Setenv("URNETWORK_STATE_DIR", state)
		if err := os.WriteFile(filepath.Join(state, "jwt"), []byte(financialTestJwt(t, "", "legacy-bootstrap")), 0600); err != nil {
			t.Fatal(err)
		}
		if explicit {
			cfg.JWTFile = filepath.Join(state, "selected-provider.jwt")
		}
		var apiRequests atomic.Int32
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hello" {
				return
			}
			if r.Method != http.MethodGet || r.URL.Path != "/sn/epoch" || r.Header.Get("Authorization") != "" {
				apiRequests.Add(1)
				t.Error("credential-less daemon reached private API", r.URL.Path)
				http.Error(w, "held", 403)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]int{"epoch": 72})
		}))
		cfg.APIURL = api.URL
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		ctx = claimQueueTestContext(t, ctx, cfg.StateDir)
		store, err := newClaimQueueStore(cfg.StateDir, ctx)
		if err != nil {
			cancel()
			api.Close()
			t.Fatal(err)
		}
		originalHash, originalRaw := signed.TxHash, signed.RawTxHex
		queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 71, Entries: map[string]*ClaimQueueEntry{"70": signed, "71": {Epoch: 71, Status: "pending"}}}
		if err := store.save(queue); err != nil {
			t.Fatal(err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "claim.yml")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		completed := false
		ctx = context.WithValue(ctx, claimDaemonCheckpointHooksKey{}, claimDaemonCheckpointHooks{afterCheckpoint: func(q *ClaimQueue) {
			old, fresh := q.Entries["70"], q.Entries["71"]
			if old != nil && old.Status == "finalized" && fresh != nil && strings.Contains(fresh.LastError, "credential") {
				completed = true
				if old.TxHash != originalHash || old.RawTxHex != originalRaw || old.Attempts != 1 || fresh.Attempts != 0 || fresh.Status == "no-claim" {
					t.Error("daemon changed original obligation or treated unknown identity as zero")
				}
				cancel()
			}
		}})
		ready := false
		err = runClaimDaemonWithAdmission(ctx, path, &claimAdmission{}, 0, func() { ready = true })
		cancel()
		api.Close()
		if err != nil && !errors.Is(err, context.Canceled) || !completed || ready || apiRequests.Load() != 0 {
			t.Fatal("missing credential blocked original reconciliation", explicit, completed, ready, err, apiRequests.Load())
		}
		restarted := newClaimQueueTestStore(t, cfg.StateDir)
		retained, err := restarted.load()
		if err != nil || retained.Entries["70"].TxHash != originalHash || retained.Entries["70"].RawTxHex != originalRaw || retained.Entries["70"].Status != "finalized" {
			t.Fatal("joined restart lost original settlement", err)
		}
	}
}
