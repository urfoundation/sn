// The census bound is exercised with complete canonical provider/leaf bytes.
// Availability controls preserve real HTTP outcomes and bounded typed causes.
package validator

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

func TestHTTPArtifactProviderCensusActuallyContains4096OriginalLeaves(t *testing.T) {
	providers := make([]payoutartifact.ProviderInput, 4096)
	for index := range providers {
		var client [16]byte
		var coldkey [32]byte
		binary.BigEndian.PutUint64(client[8:], uint64(index+1))
		binary.BigEndian.PutUint64(coldkey[24:], uint64(index+1))
		providers[index] = payoutartifact.ProviderInput{ClientID: client, Coldkey: coldkey, UsageBytes: 1, Assignments: 1, Confirmations: 1, Eligible: true}
	}
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{DeploymentID: "synthetic-census", GenesisHash: "0x" + strings.Repeat("21", 32), PolicyHash: "0x" + strings.Repeat("22", 32), ChainID: 964, Netuid: 25, Coordinator: common.HexToAddress("0x1234"), SettlementVault: common.HexToAddress("0x2345"), Epoch: 9, NoID: 1, Start: payoutartifact.Boundary{Number: 10, Hash: "0x" + strings.Repeat("23", 32)}, End: payoutartifact.Boundary{Number: 20, Hash: "0x" + strings.Repeat("24", 32)}, OperatorSnapshotHash: "sha256:" + strings.Repeat("25", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("26", 32), Providers: providers, ReliabilityAMin: 1, CreatedAt: time.Unix(1700000000, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.Repeat("27", 32))
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
	if len(artifact.Leaves) != 4096 || len(raw)*2 > maximumPayoutArtifactBytes {
		t.Fatal("populated original provider proof lacks its two-times byte margin", len(artifact.Leaves), len(raw))
	}
	var reads atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		reads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/sn/artifact" {
			if _, err := w.Write(raw); err != nil {
				return
			}
			return
		}
		if request.URL.Path != "/sn/artifacts" {
			http.NotFound(w, request)
			return
		}
		object := artifactHistoryObject{Key: "blob/st/v1/history/synthetic-census/25/9/1/" + strings.TrimPrefix(artifact.ContentHash, "sha256:") + ".json", Size: int64(len(raw)), ContentHash: artifact.ContentHash}
		if err := json.NewEncoder(w).Encode(artifactHistoryResponse{Schema: "urnetwork-payout-artifact-history-v1", Objects: []artifactHistoryObject{object}}); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)
	reader, err := NewHTTPArtifactReader(server.URL, "synthetic-census", 25)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.CloseIdleConnections)
	result, err := reader.ReadProviderCensus(t.Context(), 9, 1, 4096)
	if err != nil || result == nil || result.ContentHash != artifact.ContentHash || len(result.Leaves) != 4096 || reads.Load() != 2 {
		t.Fatal("actual complete HTTP census did not fit admitted original provider capacity", err, reads.Load())
	}
	result, err = reader.ReadProviderCensus(t.Context(), 9, 1, 4095)
	if result != nil || err != ErrArtifactCapacity || ArtifactObservationPending(err) || reads.Load() != 4 {
		t.Fatal("actual oversized provider vector bypassed original resident census", err, reads.Load())
	}
}

type artifactCensusCause struct {
	children []error
	calls    *int
}

func (self *artifactCensusCause) Error() string { return "synthetic observed cause" }
func (self *artifactCensusCause) Unwrap() []error {
	*self.calls++
	return self.children
}

type artifactCensusForgedCause struct{}

func (*artifactCensusForgedCause) Error() string { return "synthetic unrecognized authority" }
func (*artifactCensusForgedCause) Is(error) bool { panic("custom Is is not observation") }
func (*artifactCensusForgedCause) As(any) bool   { panic("custom As is not observation") }

func TestArtifactObservationPendingRequiresBoundedNonnilTypedCauses(t *testing.T) {
	calls := 0
	cycle := &artifactCensusCause{calls: &calls}
	cycle.children = []error{cycle}
	var nilCause *artifactCensusCause
	wide := &artifactCensusCause{children: make([]error, 129), calls: &calls}
	for index := range wide.children {
		wide.children[index] = syscall.ECONNRESET
	}
	for _, value := range []error{nil, nilCause, cycle, wide, &artifactCensusCause{children: []error{nil}, calls: &calls}, &artifactCensusForgedCause{}, errors.Join(syscall.ECONNRESET, &artifactCensusForgedCause{}), errors.Join(syscall.ECONNRESET, context.Canceled)} {
		before := calls
		if artifactObservationPendingCause(value) || calls-before > 33 {
			t.Fatal("unknown, mixed, nil or excessive cause acquired artifact retry authority", fmt.Sprintf("%T", value), calls-before)
		}
	}
	for _, value := range []error{syscall.ENETUNREACH, &url.Error{Op: "Get", URL: "https://artifact.example", Err: syscall.ECONNRESET}, errors.Join(syscall.ECONNRESET, context.DeadlineExceeded)} {
		if !ArtifactObservationPending(newArtifactUnavailable(value)) {
			t.Fatal("actual bounded outage lost pending original obligation", value)
		}
	}
}

func TestHTTPArtifactCensusActualMissingAndAuthorityHaveDifferentDisposition(t *testing.T) {
	var status atomic.Int64
	var reads atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		reads.Add(1)
		if code := status.Load(); code != 0 {
			http.Error(w, "synthetic original observation", int(code))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(artifactHistoryResponse{Schema: "urnetwork-payout-artifact-history-v1", Objects: []artifactHistoryObject{}}); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)
	reader, err := NewHTTPArtifactReader(server.URL, "synthetic-availability", 25)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.CloseIdleConnections)
	for _, test := range []struct {
		status  int
		pending bool
	}{{status: 0, pending: true}, {status: 404, pending: true}, {status: 403, pending: false}, {status: 401, pending: false}} {
		status.Store(int64(test.status))
		before := reads.Load()
		artifact, err := reader.ReadProviderCensus(t.Context(), 9, 1, 4096)
		if artifact != nil || err == nil || !errors.Is(err, ErrArtifactUnavailable) || ArtifactObservationPending(err) != test.pending || reads.Load() != before+1 {
			t.Fatal("actual HTTP observation invented a complete artifact or wrong availability", test, err, reads.Load()-before)
		}
	}
}
