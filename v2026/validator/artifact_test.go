package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/payoutartifact"
)

func validatorTestArtifact(t *testing.T) (*payoutartifact.Artifact, []byte) {
	t.Helper()
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{
		DeploymentID: "test-deployment", GenesisHash: "0x" + strings.Repeat("ab", 32),
		PolicyHash: "0x" + strings.Repeat("cd", 32), ChainID: 945, Netuid: 521,
		Coordinator: common.HexToAddress("0x100"), SettlementVault: common.HexToAddress("0x200"),
		Epoch: 4, NoID: 1,
		Start:                payoutartifact.Boundary{Number: 100, Hash: "0x" + strings.Repeat("01", 32)},
		End:                  payoutartifact.Boundary{Number: 200, Hash: "0x" + strings.Repeat("02", 32)},
		OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32),
		FleetSnapshotHash:    "sha256:" + strings.Repeat("20", 32),
		Providers:            []payoutartifact.ProviderInput{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 3 * 1024 * 1024 * 1024, Assignments: 8, Confirmations: 8, Eligible: true}},
		ReliabilityAMin:      8, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	value, err := payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, value
}

func TestHTTPArtifactReaderScopesHistoryAndVerifiesCanonicalContent(t *testing.T) {
	artifact, value := validatorTestArtifact(t)
	hash := strings.TrimPrefix(artifact.ContentHash, "sha256:")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/sn/artifacts":
			if request.URL.Query().Get("deployment_id") != "test-deployment" || request.URL.Query().Get("netuid") != "521" || request.URL.Query().Get("epoch") != "4" || request.URL.Query().Get("no_id") != "1" {
				http.Error(response, "wrong scope", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"schema": "urnetwork-payout-artifact-history-v1", "objects": []map[string]any{{"key": "blob/operator-1/st/v1/history/test-deployment/521/4/1/" + hash + ".json", "size": len(value), "content_hash": artifact.ContentHash}}})
		case "/sn/artifact":
			if request.URL.Query().Get("hash") != artifact.ContentHash {
				http.Error(response, "wrong hash", http.StatusBadRequest)
				return
			}
			_, _ = response.Write(value)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, "test-deployment", 521)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Read(context.Background(), 4, 1)
	if err != nil || got.ContentHash != artifact.ContentHash || got.TotalUsageBytes != artifact.TotalUsageBytes {
		t.Fatalf("artifact read = %+v, %v", got, err)
	}
}

func TestHTTPArtifactReaderRejectsEquivocationBeforeFetchingContent(t *testing.T) {
	artifact, value := validatorTestArtifact(t)
	hash := strings.TrimPrefix(artifact.ContentHash, "sha256:")
	otherHash := strings.Repeat("ef", 32)
	contentFetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/sn/artifact" {
			contentFetches++
			_, _ = response.Write(value)
			return
		}
		objects := []map[string]any{
			{"key": fmt.Sprintf("blob/st/v1/history/test-deployment/521/4/1/%s.json", hash), "size": len(value), "content_hash": artifact.ContentHash},
			{"key": fmt.Sprintf("blob/st/v1/history/test-deployment/521/4/1/%s.json", otherHash), "size": len(value), "content_hash": "sha256:" + otherHash},
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"schema": "urnetwork-payout-artifact-history-v1", "objects": objects})
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, "test-deployment", 521)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), 4, 1); !errors.Is(err, ErrArtifactEquivocation) {
		t.Fatalf("equivocation error = %v", err)
	}
	if contentFetches != 0 {
		t.Fatalf("equivocating history selected content %d times", contentFetches)
	}
}

func TestHTTPArtifactReaderRejectsCrossEpochKeyAndSizeDrift(t *testing.T) {
	artifact, value := validatorTestArtifact(t)
	hash := strings.TrimPrefix(artifact.ContentHash, "sha256:")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"schema": "urnetwork-payout-artifact-history-v1", "objects": []map[string]any{{"key": "blob/st/v1/history/test-deployment/521/3/1/" + hash + ".json", "size": len(value) + 1, "content_hash": artifact.ContentHash}}})
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, "test-deployment", 521)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), 4, 1); err == nil {
		t.Fatal("cross-epoch history object was accepted")
	}
}

func TestHTTPArtifactReaderRejectsContentSizeDrift(t *testing.T) {
	artifact, value := validatorTestArtifact(t)
	hash := strings.TrimPrefix(artifact.ContentHash, "sha256:")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/sn/artifacts":
			_ = json.NewEncoder(response).Encode(map[string]any{"schema": "urnetwork-payout-artifact-history-v1", "objects": []map[string]any{{"key": "blob/st/v1/history/test-deployment/521/4/1/" + hash + ".json", "size": len(value) + 1, "content_hash": artifact.ContentHash}}})
		case "/sn/artifact":
			_, _ = response.Write(value)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, "test-deployment", 521)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), 4, 1); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("content size drift error = %v", err)
	}
}
