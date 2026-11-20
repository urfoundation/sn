// Actual receipt reconciliation and the public swarm handler provide the
// observation witnesses. All chain replies, keys and amounts are synthetic.
package miner

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/miner/onchain"
)

// The event is part of the same actual synthetic canonical receipt, not an
// independently supplied monitoring answer.
func appendClaimProgressEvent(t *testing.T, receipt *types.Receipt, event *types.Log) {
	t.Helper()
	event.BlockNumber, event.BlockHash = receipt.BlockNumber.Uint64(), receipt.BlockHash
	event.TxHash, event.TxIndex, event.Index = receipt.TxHash, receipt.TransactionIndex, uint(len(receipt.Logs))
	receipt.Logs = append(receipt.Logs, event)
}

func TestClaimProgressActualReceiptRetainsAggregatePayment(t *testing.T) {
	cfg, claim, entry, receipt, block := signedClaimFixture(t, 70, 23)
	key, err := onchain.LoadKeyFile(cfg.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	relayer := crypto.PubkeyToAddress(key.PublicKey)
	appendClaimProgressEvent(t, receipt, claimEventLog(t, common.HexToAddress(claim.ContractAddress), "ClaimPaid", []common.Hash{common.BytesToHash(claim.Coldkey), common.BytesToHash(relayer.Bytes())}, big.NewInt(1901)))
	cfg.RPC = []string{claimReceiptIdentityTestRPC(t, receipt, block)}
	status, err := reconcileSignedClaimTest(t, t.Context(), cfg, entry)
	if err != nil || status != "finalized" {
		t.Fatal("actual aggregate payment reconciliation failed", status, err)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var retained struct {
		Observation map[string]any `json:"public_observation"`
	}
	if err := json.Unmarshal(raw, &retained); err != nil {
		t.Fatal(err)
	}
	if retained.Observation["payment_status"] != "aggregate-paid" || retained.Observation["aggregate_paid_rao"] != "1901" || retained.Observation["accepted_amount_rao"] != "25" || retained.Observation["unpaid_credit_rao"] != nil {
		t.Fatal("actual ClaimPaid was lost or allocated only to this epoch", retained.Observation)
	}
	if retained.Observation["authority"] != "configured-rpc-assertion" {
		t.Fatal("RPC receipt silently acquired independent authority", retained.Observation)
	}
}

func TestClaimProgressActualReceiptRetainsAcceptedAndDeferredCredit(t *testing.T) {
	cfg, claim, entry, receipt, block := signedClaimFixture(t, 70, 23)
	appendClaimProgressEvent(t, receipt, claimEventLog(t, common.HexToAddress(claim.ContractAddress), "ClaimPaymentDeferred", []common.Hash{common.BytesToHash(claim.Coldkey)}, big.NewInt(125), big.NewInt(90), uint64(100), uint8(0)))
	cfg.RPC = []string{claimReceiptIdentityTestRPC(t, receipt, block)}
	store := claimRetainedTestStore(t, cfg, entry)
	defer store.close()
	beforeRaw, beforeHash := entry.RawTxHex, entry.TxHash
	status, err := reconcileSignedClaim(t.Context(), cfg, entry, store)
	if err != nil || status != "finalized" {
		t.Fatal("actual receipt reconciliation failed before observation", status, err)
	}
	entry.Status = status
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: entry.Epoch, Entries: map[string]*ClaimQueueEntry{fmt.Sprint(entry.Epoch): entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	raw, present, err := store.head.Read()
	if err != nil || !present {
		t.Fatal("actual committed queue missing", err)
	}
	var retained struct {
		Entries map[string]struct {
			Observation map[string]any `json:"public_observation"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &retained); err != nil {
		t.Fatal(err)
	}
	observation := retained.Entries["70"].Observation
	if observation == nil {
		t.Fatal("actual finalized queue discarded accepted-credit and deferred-payment evidence")
	}
	if observation["evidence_kind"] != "signed-receipt" || observation["accepted_amount_rao"] != "25" || observation["unpaid_credit_rao"] != "125" || observation["payment_status"] != "deferred" || observation["aggregate_paid_rao"] != nil {
		t.Fatal("accepted credit was confused with paid funds", observation)
	}
	if entry.RawTxHex != beforeRaw || entry.TxHash != beforeHash {
		t.Fatal("reporting changed original signed custody")
	}
}

func TestClaimProgressPublicHandlerReportsUnknownBeforeWriterAdmission(t *testing.T) {
	swarm, err := NewClaimSwarm(&ClaimSwarmConfig{Schema: ClaimSwarmSchema, ListenAddress: "127.0.0.1:19091", Members: []ClaimSwarmMember{{ID: "synthetic-pool", ConfigPath: filepath.Join(t.TempDir(), "unopened.yml")}}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/claim-progress?id=synthetic-pool", nil)
	response := httptest.NewRecorder()
	swarm.ServeHTTP(response, request)
	var body map[string]any
	err = json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != http.StatusServiceUnavailable || err != nil || body["schema"] != "urnetwork-claim-progress-v1" || body["status"] != "unknown" {
		t.Fatal("actual public handler lacks bounded unknown claim projection", response.Code, err, response.Body.String())
	}
	if body["entries"] != nil || body["queue_sha256"] != nil {
		t.Fatal("unadmitted writer fabricated a durable observation", body)
	}
}
