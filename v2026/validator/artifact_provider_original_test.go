// Raw artifact acquisition is an explicit stage of the independently selected
// full-provider consumer. It cannot turn optional supplied rows into authority.
package validator

import (
	"bytes"
	"crypto/ed25519"
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
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Independently expected idle providers need no manufactured zero-credit SQL
// row. Legacy readers remain strict until the complete original owner joins.
func TestArtifactOriginalProviderReadDefersOnlyOptionalClosedWorkAdmission(t *testing.T) {
	original, inventory, expected := wholeWorkReaderFixture(t)
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{ClosedWork: original.ClosedWork, DeploymentID: original.DeploymentID, GenesisHash: original.GenesisHash, PolicyHash: original.PolicyHash, ChainID: original.ChainID, Netuid: original.Netuid, Coordinator: original.Coordinator, SettlementVault: original.SettlementVault, Epoch: original.Epoch, NoID: original.NoID, Start: original.Start, End: original.End, OperatorSnapshotHash: original.OperatorSnapshotHash, FleetSnapshotHash: original.FleetSnapshotHash, Providers: []payoutartifact.ProviderInput{{ClientID: [16]byte{91}, NetworkID: [16]byte{92}, Coldkey: [32]byte{93}, ExclusionReason: "reliability_exposure_floor"}}, ReliabilityAMin: original.ReliabilityAMin, CreatedAt: inventory.Clock.EndTime})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := crypto.HexToECDSA(strings.Repeat("41", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, publisher); err != nil {
		t.Fatal(err)
	}
	raw, err := payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sn/artifacts":
			key := fmt.Sprintf("blob/st/v1/history/%s/%d/%d/%d/%s.json", artifact.DeploymentID, artifact.Netuid, artifact.Epoch, artifact.NoID, strings.TrimPrefix(artifact.ContentHash, "sha256:"))
			_ = json.NewEncoder(w).Encode(artifactHistoryResponse{Schema: "urnetwork-payout-artifact-history-v1", Objects: []artifactHistoryObject{{Key: key, Size: int64(len(raw)), ContentHash: artifact.ContentHash}}})
		case "/sn/artifact":
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseIdleConnections()
	if got, err := reader.ReadProviderCensus(t.Context(), artifact.Epoch, artifact.NoID, 8); got != nil || !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) {
		t.Fatal("legacy closed-work admission was weakened", got, err)
	}
	got, err := reader.ReadProviderOriginalCensus(t.Context(), artifact.Epoch, artifact.NoID, 8)
	if err != nil || got == nil || got.ContentHash != artifact.ContentHash {
		t.Fatal("actual canonical original could not reach its independent full consumer", got, err)
	}
	if complete, err := payoutartifact.VerifyWholeWorkInventoryWithWitness(t.Context(), got, inventory, expected); err == nil || complete != nil {
		t.Fatal("raw optional rows became independently expected providers", complete, err)
	}
	authority, err := payoutartifact.DecodeWholeWorkAuthority(t.Context(), inventory.Authority, expected.AuthoritySigner)
	if err != nil {
		t.Fatal(err)
	}
	authority.ExpectedProviders = []payoutartifact.WholeWorkExpectedProvider{{ClientId: [16]byte{91}, NetworkId: [16]byte{92}}}
	ownerKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{94}, 32))
	requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{95}, 32))
	authority.RequestPublicKey = [32]byte(requestKey[32:])
	owner := payoutartifact.WholeWorkOwner{ClientId: [16]byte{91}, NetworkId: [16]byte{92}, Generation: [16]byte{96}, PublicKey: [32]byte(ownerKey[32:])}
	authority.Owners = []payoutartifact.WholeWorkOwner{owner}
	domainHash, err := authority.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	pair := payoutartifact.WholeWorkOwnerCuts{}
	for side, boundary := range []payoutartifact.Boundary{artifact.Start, artifact.End} {
		cut, err := coreprotocol.SignOriginalWorkCut(t.Context(), coreprotocol.OriginalWorkCut{DomainHash: domainHash, ClientId: owner.ClientId, Generation: owner.Generation, Epoch: artifact.Epoch, Block: boundary.Number, BlockHash: common.HexToHash(boundary.Hash), Complete: true, Contracts: []coreprotocol.OriginalWorkContract{}}, ownerKey)
		if err != nil {
			t.Fatal(err)
		}
		kind := "start"
		if side == 1 {
			kind = "end"
		}
		stamp := []time.Time{inventory.Clock.StartTime, inventory.Clock.EndTime}[side].Unix()
		request, err := coreprotocol.SignOriginalWorkRequest(coreprotocol.OriginalWorkRequest{RequestId: [16]byte{97, byte(side + 1)}, DomainHash: domainHash, ClientId: owner.ClientId, Generation: owner.Generation, PublicKey: owner.PublicKey, Epoch: artifact.Epoch, Kind: kind, Block: boundary.Number, BlockHash: cut.BlockHash, IssuedAtUnix: stamp, ExpiresAtUnix: stamp + 300}, requestKey)
		if err != nil {
			t.Fatal(err)
		}
		rawCut, err := cut.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		rawRequest, err := request.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if side == 0 {
			pair.Start, pair.StartRequest = rawCut, rawRequest
		} else {
			pair.End, pair.EndRequest = rawCut, rawRequest
		}
	}
	inventory.Owners = []payoutartifact.WholeWorkOwnerCuts{pair}
	approver, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := payoutartifact.SignWholeWorkAuthority(t.Context(), authority, approver)
	if err != nil {
		t.Fatal(err)
	}
	inventory.Authority, err = signed.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	complete, err := payoutartifact.VerifyWholeWorkInventoryWithWitness(t.Context(), got, inventory, expected)
	if err != nil || complete == nil || !complete.Complete || !complete.AttributionComplete || complete.Reports == nil || len(complete.ExpectedProviders) != 1 || complete.ExpectedProviders[0].UsageBytes != 0 {
		t.Fatal("independently known idle original did not reach full work verification", complete, err)
	}
}
