// Real producer boundaries supply each public observation. Synthetic RPC
// replies prove only the configured-RPC assertion scope exposed on the wire.
package miner

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/sdk/v2026"
	"gopkg.in/yaml.v3"
)

func TestClaimProgressPaymentAmbiguityCannotRewriteSignedOutcome(t *testing.T) {
	for _, fault := range []string{"paid-below-accepted", "deferred-below-accepted", "paid-wrong-relayer", "payment-wrong-block", "payment-wrong-transaction", "payment-wrong-index", "payment-removed", "payment-duplicate", "payment-both", "payment-truncated", "payment-missing-coldkey"} {
		cfg, claim, entry, receipt, block := signedClaimFixture(t, 70, 23)
		_, _, from, err := authenticateSignedClaim(cfg, entry)
		if err != nil {
			t.Fatal(err)
		}
		payment := claimEventLog(t, common.HexToAddress(claim.ContractAddress), "ClaimPaid", []common.Hash{common.BytesToHash(claim.Coldkey), common.BytesToHash(from.Bytes())}, big.NewInt(125))
		if fault == "paid-below-accepted" {
			payment = claimEventLog(t, common.HexToAddress(claim.ContractAddress), "ClaimPaid", payment.Topics[1:], big.NewInt(24))
		}
		if fault == "deferred-below-accepted" {
			payment = claimEventLog(t, common.HexToAddress(claim.ContractAddress), "ClaimPaymentDeferred", []common.Hash{common.BytesToHash(claim.Coldkey)}, big.NewInt(24), big.NewInt(90), uint64(100), uint8(0))
		}
		appendClaimProgressEvent(t, receipt, payment)
		switch fault {
		case "paid-wrong-relayer":
			payment.Topics[2] = common.Hash{0xcc}
		case "payment-wrong-block":
			payment.BlockHash = common.Hash{0xcc}
		case "payment-wrong-transaction":
			payment.TxHash = common.Hash{0xcc}
		case "payment-wrong-index":
			payment.TxIndex++
		case "payment-removed":
			payment.Removed = true
		case "payment-duplicate":
			duplicate := *payment
			appendClaimProgressEvent(t, receipt, &duplicate)
		case "payment-both":
			appendClaimProgressEvent(t, receipt, claimEventLog(t, common.HexToAddress(claim.ContractAddress), "ClaimPaymentDeferred", []common.Hash{common.BytesToHash(claim.Coldkey)}, big.NewInt(125), big.NewInt(90), uint64(100), uint8(0)))
		case "payment-truncated":
			payment.Data = payment.Data[:3]
		case "payment-missing-coldkey":
			payment.Topics = payment.Topics[:1]
		}
		cfg.RPC = []string{claimReceiptIdentityTestRPC(t, receipt, block)}
		beforeRaw, beforeHash := entry.RawTxHex, entry.TxHash
		status, err := reconcileSignedClaimTest(t, t.Context(), cfg, entry)
		observation := entry.PublicObservation
		if err != nil || status != "finalized" || observation == nil || observation.PaymentStatus != "invalid" || observation.AcceptedAmountRao != "25" || observation.AggregatePaidRao != "" || observation.UnpaidCreditRao != "" || entry.RawTxHex != beforeRaw || entry.TxHash != beforeHash {
			t.Fatalf("%s changed original claim authority or invented payment: status=%s err=%v observation=%+v", fault, status, err, observation)
		}
	}
}

func TestClaimProgressUnsignedLeafCannotInventReceiptOrPayment(t *testing.T) {
	fixture := newClaimClockTestRPC(t, nil)
	entry := &ClaimQueueEntry{Epoch: 70, Status: "retry"}
	api := claimApiFunction(func(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error) { return fixture.claim, nil })
	status, err := reconcileClaimEntryTest(t, t.Context(), fixture.cfg, api, entry)
	observation := entry.PublicObservation
	if err != nil || status != "finalized" || observation == nil || observation.EvidenceKind != "finalized-leaf" || observation.ProofStatus != "merkle-verified" || observation.LeafClaimed == nil || !*observation.LeafClaimed || observation.TransactionHash != "" || observation.AcceptedAmountRao != "" || observation.AggregatePaidRao != "" || observation.PaymentStatus != "unknown" || observation.Authority != "configured-rpc-assertion" || observation.GenesisStatus != "unverified" {
		t.Fatal("unsigned leaf became a paid receipt or independent finality", status, err, observation)
	}
	native, state, _, sends := fixture.evidence()
	if native != 0 || len(state) != 2 || len(sends) != 0 {
		t.Fatal("observation added a native authority lookup or send", native, state, sends)
	}
}

func TestClaimProgressApiAbsenceKeepsPaymentAndGenesisUnknown(t *testing.T) {
	cfg, _, _, _, _ := signedClaimFixture(t, 70, 23)
	entry := &ClaimQueueEntry{Epoch: 70, Status: "retry"}
	api := claimApiFunction(func(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error) {
		return &sdk.SnPoolClaimResult{Epoch: 70}, nil
	})
	status, err := reconcileClaimEntryTest(t, t.Context(), cfg, api, entry)
	observation := entry.PublicObservation
	if err != nil || status != "no-claim" || observation == nil || observation.Authority != "api-assertion" || observation.GenesisStatus != "unverified" || observation.Pool != (protocol.ClaimProgressPool{}) || observation.AcceptedAmountRao != "" || observation.AggregatePaidRao != "" || observation.PaymentStatus != "unknown" || observation.Validate() != nil {
		t.Fatal("API absence invented chain zero/payment/identity", status, err, observation)
	}
}

func TestClaimProgressChangedFinalizedWitnessCannotPublishObservation(t *testing.T) {
	fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
		fixture.reply = func(_ context.Context, method string, _ []json.RawMessage, value any) (any, error) {
			if method == "eth_call" && len(fixture.stateSelectors) == 2 {
				fixture.canonical[70] = common.Hash{0xee}
			}
			return value, nil
		}
	})
	prior := absentClaimObservation(70)
	entry := &ClaimQueueEntry{Epoch: 70, Status: "retry", PublicObservation: prior}
	api := claimApiFunction(func(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error) { return fixture.claim, nil })
	status, err := reconcileClaimEntryTest(t, t.Context(), fixture.cfg, api, entry)
	if err == nil || status != "" || entry.PublicObservation != prior {
		t.Fatal("failed closing witness advanced public evidence", status, err)
	}
	_, _, _, sends := fixture.evidence()
	if len(sends) != 0 {
		t.Fatal("read observation sent a transaction")
	}
}

func TestClaimProgressPublicSwarmUsesActualWriterAndJoinedLifecycle(t *testing.T) {
	read := make(chan struct{})
	var once sync.Once
	path, cfg, _ := claimQueueOwnerDaemonFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(read) })
		_, _ = writer.Write([]byte(`{"epoch":0}`))
	})
	cfg.ProgressPool = &protocol.ClaimProgressPool{ChainId: 945, Vault: "0x1111111111111111111111111111111111111111", NoId: "7", Coldkey: "0x1111111111111111111111111111111111111111111111111111111111111111"}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	swarm, err := NewClaimSwarm(&ClaimSwarmConfig{Schema: ClaimSwarmSchema, ListenAddress: address, Members: []ClaimSwarmMember{{ID: "actual", ConfigPath: path}}})
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(t.Context())
	ctx := claimSwarmTestContext(t, parent, swarm)
	done := make(chan struct{})
	var runErr error
	go func() { defer close(done); runErr = swarm.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-read:
	case <-done:
		t.Fatal("actual daemon stopped before initial read", runErr)
	case <-t.Context().Done():
		t.Fatal("actual daemon did not reach public read")
	}
	response := httptest.NewRecorder()
	swarm.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/claim-progress?id=actual", nil))
	value, err := protocol.DecodeClaimProgress(response.Body.Bytes())
	if err != nil || response.Code != http.StatusOK || value.Sequence == 0 || value.QueueSha256 == "" || value.DeclaredPool == nil || *value.DeclaredPool != *cfg.ProgressPool {
		t.Fatal("actual queue acknowledgement not exposed by public handler", response.Code, err, response.Body.String())
	}
	cancel()
	<-done
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		t.Fatal(runErr)
	}
	response = httptest.NewRecorder()
	swarm.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/claim-progress?id=actual", nil))
	closed, err := protocol.DecodeClaimProgress(response.Body.Bytes())
	if err != nil || response.Code != http.StatusServiceUnavailable || closed.Status != "closed" || closed.InstanceId != value.InstanceId || closed.Sequence < value.Sequence {
		t.Fatal("joined public worker left a live or invented generation", err, response.Body.String())
	}
}

// This uses actual producer receipt coordinates, then an independently supplied
// expected pool. A foreign payout identity never becomes a transport identity.
func TestClaimProgressDeclaredPoolMismatchRemainsVisible(t *testing.T) {
	cfg, _, entry, receipt, block := signedClaimFixture(t, 70, 23)
	cfg.RPC = []string{claimReceiptIdentityTestRPC(t, receipt, block)}
	status, err := reconcileSignedClaimTest(t, t.Context(), cfg, entry)
	if err != nil || status != "finalized" {
		t.Fatal(status, err)
	}
	pool := entry.PublicObservation.Pool
	pool.NoId = "999"
	store, err := newClaimQueueStore(cfg.StateDir, claimQueueTestContext(t, t.Context(), cfg.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	store.progress = newClaimProgressOwner("unrelated-client-id", &pool)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	value, active := claimProgressSnapshot(t, store.progress)
	if !active || value.Entries[0].DomainStatus != "identity" || value.Entries[0].Observation.Pool.NoId == pool.NoId || value.Entries[0].Observation.GenesisStatus != "unverified" {
		t.Fatal("configured pool overwrote actual observation", value)
	}
}
