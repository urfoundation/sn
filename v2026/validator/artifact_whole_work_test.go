// The real public HTTP consumer pins exact original artifact/window authority
// and preserves unknown evidence, transport ownership and cancellation causes.
package validator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

// A genuinely empty independent roster is signed separately from its payout.
func wholeWorkReaderFixture(t *testing.T) (*payoutartifact.Artifact, *payoutartifact.WholeWorkInventory, payoutartifact.WholeWorkExpectation) {
	t.Helper()
	old, _ := validatorTestArtifact(t)
	start := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	startHeader := &types.Header{Number: new(big.Int).SetUint64(old.Start.Number), Time: uint64(start.Unix())}
	endHeader := &types.Header{Number: new(big.Int).SetUint64(old.End.Number), Time: uint64(end.Unix())}
	old.Start.Hash, old.End.Hash = startHeader.Hash().Hex(), endHeader.Hash().Hex()
	census := &payoutartifact.ClosedWorkCensus{Schema: payoutartifact.ClosedWorkSchema, DeploymentId: old.DeploymentID, ChainId: old.ChainID, GenesisHash: old.GenesisHash, Netuid: old.Netuid, Coordinator: old.Coordinator, SettlementVault: old.SettlementVault, Epoch: old.Epoch, NoId: old.NoID, PolicyHash: old.PolicyHash, Start: old.Start, End: old.End, WindowStart: start.Format(time.RFC3339Nano), WindowEnd: end.Format(time.RFC3339Nano), Records: []payoutartifact.ClosedWorkRecord{}}
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{ClosedWork: census, DeploymentID: old.DeploymentID, GenesisHash: old.GenesisHash, PolicyHash: old.PolicyHash, ChainID: old.ChainID, Netuid: old.Netuid, Coordinator: old.Coordinator, SettlementVault: old.SettlementVault, Epoch: old.Epoch, NoID: old.NoID, Start: old.Start, End: old.End, OperatorSnapshotHash: old.OperatorSnapshotHash, FleetSnapshotHash: old.FleetSnapshotHash, Providers: []payoutartifact.ProviderInput{}, ReliabilityAMin: old.ReliabilityAMin, CreatedAt: end})
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
	root, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	domain, err := payoutartifact.ClosedWorkReportDomain(artifact)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := payoutartifact.SignWholeWorkAuthority(t.Context(), payoutartifact.WholeWorkAuthority{Domain: domain, Epoch: artifact.Epoch, Start: artifact.Start, End: artifact.End, RequestPublicKey: [32]byte{43}, PriorContracts: []payoutartifact.WholeWorkPriorContract{}, Owners: []payoutartifact.WholeWorkOwner{}, ExpectedProviders: []payoutartifact.WholeWorkExpectedProvider{}}, root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := authority.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clock := &payoutartifact.ClosedWorkWindowClock{Start: artifact.Start, End: artifact.End, StartTime: start, EndTime: end}
	clock.StartHeader, _ = rlp.EncodeToBytes(startHeader)
	clock.EndHeader, _ = rlp.EncodeToBytes(endHeader)
	inventory := &payoutartifact.WholeWorkInventory{Schema: payoutartifact.WholeWorkInventorySchema, Authority: raw, Owners: []payoutartifact.WholeWorkOwnerCuts{}, Window: &payoutartifact.ClosedWorkWindow{Schema: payoutartifact.ClosedWorkWindowSchema, Start: census.WindowStart, End: census.WindowEnd, Records: []payoutartifact.ClosedWorkWindowRecord{}}, Clock: clock}
	expected := payoutartifact.WholeWorkExpectation{AuthoritySigner: crypto.PubkeyToAddress(root.PublicKey), ClientKeyRootSigner: crypto.PubkeyToAddress(root.PublicKey)}
	return artifact, inventory, expected
}

func TestWholeWorkActualPublicReaderPinsExactArtifactAndIndependentAuthority(t *testing.T) {
	artifact, inventory, expected := wholeWorkReaderFixture(t)
	seen := make(chan string, 1)
	domain, _ := payoutartifact.ClosedWorkReportDomain(artifact)
	domainHash, _ := domain.Digest()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.URL.Path != "/provider-work/v1/windows" || query.Get("domain") != hex.EncodeToString(domainHash[:]) || query.Get("epoch") != "4" || query.Get("artifact") != strings.TrimPrefix(artifact.ContentHash, "sha256:") || query.Get("authority") != "" || len(query) != 3 {
			http.Error(writer, "wrong exact scope", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(inventory)
		seen <- request.URL.RawQuery
	}))
	defer server.Close()
	reader, err := NewHttpWholeWorkInventoryReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	originalHash := artifact.ContentHash
	actual, verified, err := reader.Read(t.Context(), artifact, expected)
	if err != nil || actual == nil || verified == nil || !verified.Complete || verified.Contracts != 0 || verified.ExpectedProviders == nil {
		t.Fatal("actual known-empty companion not acquired", verified, err)
	}
	if artifact.ContentHash != originalHash || artifact.ClosedWork.WholeInventory != nil {
		t.Fatal("public acquisition changed original signed payout")
	}
	select {
	case <-seen:
	default:
		t.Fatal("actual public endpoint was not called")
	}
}

func TestWholeWorkPublicReaderMissingAndForeignOriginalsNeverAgree(t *testing.T) {
	artifact, inventory, expected := wholeWorkReaderFixture(t)
	for _, status := range []int{http.StatusNotFound, http.StatusNoContent, http.StatusOK} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(status)
			if status == http.StatusOK {
				json.NewEncoder(writer).Encode(inventory)
			}
		}))
		reader, err := NewHttpWholeWorkInventoryReader(server.URL, artifact.DeploymentID, artifact.Netuid)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		selection := expected
		want := payoutartifact.ErrClosedWorkUnavailable
		if status == http.StatusOK {
			selection.AuthorityHash = "sha256:" + strings.Repeat("12", 32)
			want = payoutartifact.ErrClosedWorkIntegrity
		}
		_, _, err = reader.Read(t.Context(), artifact, selection)
		server.Close()
		if !errors.Is(err, want) {
			t.Fatal("missing or substituted source got wrong disposition", status, err)
		}
	}
}

func TestWholeWorkPublicReaderActualPendingGetJoinsCancellationCause(t *testing.T) {
	artifact, _, expected := wholeWorkReaderFixture(t)
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(entered)
		<-request.Context().Done()
	}))
	defer server.Close()
	reader, err := NewHttpWholeWorkInventoryReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	finished := make(chan error, 1)
	go func() { _, _, err := reader.Read(ctx, artifact, expected); finished <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual owned GET never entered")
	}
	cause := errors.New("synthetic whole-work read owner closed")
	cancel(cause)
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
			t.Fatal("original cancellation cause lost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending whole-work acquisition escaped owner")
	}
}
