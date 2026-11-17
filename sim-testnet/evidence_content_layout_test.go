// An operator's publication receipt names the sharded evidence content key
// operators write now, or the legacy flat key of evidence published before
// that layout (server startifact, 2026-11). Any other content key is outside
// the receipt's signed scope. Synthetic operators, keys and origins.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishedEvidenceStorageKeysAcceptShardedAndLegacyContentKeys(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "layout-run"
	envelope, err := signEvidence(cfg, "scenario-bundle", runID, map[string]any{"release": "1.0"}, roles.EVM["operator-1-artifact"])
	if err != nil {
		t.Fatal(err)
	}
	h := strings.TrimPrefix(envelope.ContentHash, "sha256:")
	content := "fixture/st/v1/evidence/content/sha256/"
	historyKey := "fixture/st/v1/evidence/history/" + cfg.Config.Deployment.DeploymentID + "/" + fmt.Sprint(cfg.Netuid) + "/" + envelope.Kind + "/" + runID + "/" + h + ".json"
	for _, contentKey := range []string{
		content + h[0:2] + "/" + h[2:4] + "/" + h + ".json",
		content + h + ".json",
		"st/v1/evidence/content/sha256/" + h[0:2] + "/" + h[2:4] + "/" + h + ".json",
	} {
		published := PublishedEvidence{ContentHash: envelope.ContentHash, ContentKey: contentKey, HistoryKey: historyKey}
		if gotRunID, err := validatePublishedEvidenceStorageKeys(published, envelope); err != nil || gotRunID != runID {
			t.Fatalf("canonical content key %q: run %q, %v", contentKey, gotRunID, err)
		}
	}
	other := strings.Repeat("ab", 32)
	for _, contentKey := range []string{
		content + h[0:2] + "/" + h + ".json",
		content + h[2:4] + "/" + h[0:2] + "/" + h + ".json",
		content + h[0:2] + "/" + h[2:4] + "/" + h[4:6] + "/" + h + ".json",
		content + other[0:2] + "/" + other[2:4] + "/" + other + ".json",
		"fixture/st/v1/evidence/content/sha512/" + h[0:2] + "/" + h[2:4] + "/" + h + ".json",
		"fixture/st/v1/evidence/content/sha256/" + h[0:2] + "/" + h[2:4] + "/../" + h + ".json",
		"/st/v1/evidence/content/sha256/" + h[0:2] + "/" + h[2:4] + "/" + h + ".json",
		"fixturest/v1/evidence/content/sha256/" + h + ".json.old",
	} {
		published := PublishedEvidence{ContentHash: envelope.ContentHash, ContentKey: contentKey, HistoryKey: historyKey}
		if _, err := validatePublishedEvidenceStorageKeys(published, envelope); err == nil || !strings.Contains(err.Error(), "canonical signed scope") {
			t.Fatalf("noncanonical content key %q was accepted: %v", contentKey, err)
		}
	}
}

// The whole origin check accepts an operator's sharded receipt: the content is
// served by hash and the history copy is listed exactly.
func TestVerifyPublishedEvidenceOriginAcceptsShardedContentKey(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runID := "sharded-run"
	envelope, err := signEvidence(cfg, "scenario-bundle", runID, map[string]any{"release": "1.0"}, roles.EVM["operator-1-artifact"])
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	h := strings.TrimPrefix(envelope.ContentHash, "sha256:")
	contentKey := "fixture/st/v1/evidence/content/sha256/" + h[0:2] + "/" + h[2:4] + "/" + h + ".json"
	historyKey := "fixture/st/v1/evidence/history/" + cfg.Config.Deployment.DeploymentID + "/" + fmt.Sprint(cfg.Netuid) + "/" + envelope.Kind + "/" + runID + "/" + h + ".json"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sn/evidence":
			if r.URL.Query().Get("hash") != envelope.ContentHash {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(encoded)
		case "/sn/evidence/history":
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": "urnetwork-release-evidence-history-v1", "objects": []map[string]string{{"key": historyKey}}, "more": false, "next_after": ""})
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	published := PublishedEvidence{ContentHash: envelope.ContentHash, ContentKey: contentKey, HistoryKey: historyKey}
	if err := verifyPublishedEvidenceOrigin(context.Background(), cfg, roles, 1, origin.URL, published); err != nil {
		t.Fatalf("sharded receipt rejected: %v", err)
	}
}
