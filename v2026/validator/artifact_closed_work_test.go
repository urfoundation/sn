// The validator's actual public HTTP reader independently reconstructs the
// optional original-row component. A signature alone cannot validate its math.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

// Retain one canonical synthetic row and actual artifact/content-history bytes.
func validatorClosedWorkReader(t *testing.T, change func(*payoutartifact.ClosedWorkCensus)) (*HTTPArtifactReader, *payoutartifact.Artifact) {
	t.Helper()
	artifact, _ := validatorTestArtifact(t)
	artifact.Providers[0].NetworkID = [16]byte{2}
	artifact.ProviderSnapshotHash = payoutartifact.SnapshotHash(artifact.Providers)
	census := &payoutartifact.ClosedWorkCensus{Schema: payoutartifact.ClosedWorkSchema, DeploymentId: artifact.DeploymentID, ChainId: artifact.ChainID, GenesisHash: artifact.GenesisHash, Netuid: artifact.Netuid, Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, Epoch: artifact.Epoch, NoId: artifact.NoID, PolicyHash: artifact.PolicyHash, Start: artifact.Start, End: artifact.End, WindowStart: "2026-10-06T00:00:00Z", WindowEnd: "2026-10-06T01:00:00Z", Count: 1,
		Records: []payoutartifact.ClosedWorkRecord{{ContractId: [16]byte{1}, ClosedAt: "2026-10-06T00:30:00Z", Original: []byte(fmt.Sprintf(`{"version":1,"byte_count":%d,"providers":[{"client_id":"01000000-0000-0000-0000-000000000000","network_id":"02000000-0000-0000-0000-000000000000","byte_count":%d}]}`, artifact.TotalUsageBytes, artifact.TotalUsageBytes))}}}
	if change != nil {
		change(census)
	}
	artifact.ClosedWork = census
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	raw, err := payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/sn/artifacts" {
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": "urnetwork-payout-artifact-history-v1", "objects": []map[string]any{{"key": "blob/synthetic/st/v1/history/test-deployment/521/4/1/" + strings.TrimPrefix(artifact.ContentHash, "sha256:") + ".json", "size": len(raw), "content_hash": artifact.ContentHash}}, "more": false, "next_after": ""})
			return
		}
		if request.URL.Path != "/sn/artifact" || request.URL.Query().Get("hash") != artifact.ContentHash {
			http.Error(w, "wrong original content", 400)
			return
		}
		_, _ = w.Write(raw)
	}))
	t.Cleanup(endpoint.Close)
	reader, err := NewHTTPArtifactReader(endpoint.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.CloseIdleConnections)
	return reader, artifact
}

// Both selected and ordinary readers consume the same original component.
func TestArtifactClosedWorkPublicReaderReconstructsOriginalRows(t *testing.T) {
	reader, original := validatorClosedWorkReader(t, nil)
	artifact, err := reader.Read(t.Context(), 4, 1)
	if err != nil || artifact.ContentHash != original.ContentHash {
		t.Fatal("actual public artifact lost original census", err)
	}
	value, err := payoutartifact.VerifyClosedWork(t.Context(), artifact)
	if err != nil || value.Contracts != 1 || value.UsageBytes != 3*1024*1024*1024 {
		t.Fatal("original provider work did not reconstruct", value, err)
	}
	selected, err := reader.ReadProviderCensus(t.Context(), 4, 1, 1)
	if err != nil || selected.ContentHash != artifact.ContentHash {
		t.Fatal("selected reader changed original component", err)
	}
}

// Exact root/signature/content agreement is not a substitute for source math.
func TestArtifactClosedWorkPublicReaderRefusesResignedIdentity(t *testing.T) {
	reader, original := validatorClosedWorkReader(t, func(c *payoutartifact.ClosedWorkCensus) {
		c.Records[0].Original = bytes.Replace(c.Records[0].Original, []byte("02000000-0000"), []byte("03000000-0000"), 1)
	})
	if err := payoutartifact.Verify(original); err != nil {
		t.Fatal("negative lacks a valid original signature", err)
	}
	artifact, err := reader.Read(t.Context(), 4, 1)
	if artifact != nil || !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || ArtifactObservationPending(err) {
		t.Fatal("public reader trusted re-signed conflicting original identity", artifact, err)
	}
}

// A component for another authority is absent here, not evidence of a conflict.
func TestArtifactClosedWorkPublicForeignComponentRemainsUnknown(t *testing.T) {
	reader, _ := validatorClosedWorkReader(t, func(c *payoutartifact.ClosedWorkCensus) { c.NoId++ })
	artifact, err := reader.Read(t.Context(), 4, 1)
	if err != nil || artifact == nil {
		t.Fatal("foreign optional component blocked legacy artifact", err)
	}
	if value, err := payoutartifact.VerifyClosedWork(t.Context(), artifact); value != nil || !errors.Is(err, payoutartifact.ErrClosedWorkUnavailable) {
		t.Fatal("foreign component acquired requested authority", value, err)
	}
}
