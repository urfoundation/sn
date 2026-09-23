//go:build linux || darwin

// Real signed files and a nonce-only Rpc fixture reproduce stopped worker
// evidence gaps and changes occurring between preflight and final sealing.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// All three attempts use one synthetic role; the replacement keeps nonce one
// while increasing its independent maximum fee liability.
type evidenceRelayContinuationTransactionFixture struct {
	executor     *Executor
	reader       *evidenceRelayNonceRpcFixture
	transactions []*ethTypes.Transaction
	raw          [][]byte
}

func newEvidenceRelayContinuationTransactionFixture(t *testing.T) evidenceRelayContinuationTransactionFixture {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("7", 64))
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	chain := big.NewInt(945)
	reader := &evidenceRelayNonceRpcFixture{nonces: map[string]uint64{}}
	for _, selector := range []string{"0x28", "latest", "pending"} {
		reader.nonces[address.Hex()+"/"+selector] = 2
	}
	fixture := evidenceRelayContinuationTransactionFixture{reader: reader}
	fixture.executor = &Executor{
		cfg:      &ResolvedConfig{Config: &HarnessConfig{}, OperationalRPCMode: rpcModeOwnedNode},
		stateDir: t.TempDir(), plan: &SetupPlan{ChainID: chain.Uint64(), PlanHash: common.Hash{0x41}.Hex()},
		roles:   &RoleSecrets{EVM: map[string]EVMRoleSecret{"operator-root": {Address: address.Hex()}}},
		journal: &Journal{}, keeper: &EvmTxManager{client: reader.client(t)},
	}
	for index, nonce := range []uint64{0, 1, 1} {
		transaction, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: chain, Nonce: nonce, GasTipCap: new(big.Int), GasFeeCap: big.NewInt(int64(10 + index)), Gas: 21000, To: &address, Value: big.NewInt(3)}), ethTypes.LatestSignerForChainID(chain), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		fixture.transactions = append(fixture.transactions, transaction)
		fixture.raw = append(fixture.raw, raw)
	}
	fixture.retain(t, 0)
	return fixture
}

// Retention copies the original signature with exclusive creation and never
// rewrites an existing transaction, journal or mutable worker status.
func (self evidenceRelayContinuationTransactionFixture) retain(t *testing.T, index int) {
	t.Helper()
	path := filepath.Join(self.executor.stateDir, "transactions", stringsTrim0x(self.transactions[index].Hash().Hex())+".rlp")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(self.raw[index])
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceRelayContinuationTransactionsRequireOriginalWorkerSignature(t *testing.T) {
	fixture := newEvidenceRelayContinuationTransactionFixture(t)
	executor := fixture.executor
	before, err := os.ReadFile(filepath.Join(executor.stateDir, "transactions", stringsTrim0x(fixture.transactions[0].Hash().Hex())+".rlp"))
	if err != nil {
		t.Fatal(err)
	}
	if census, err := executor.readEvidenceRelayContinuationTransactionCensus(t.Context(), 40); census != nil || err == nil || !strings.Contains(err.Error(), "role operator-root nonce 1 has no retained signed transaction") {
		t.Fatalf("missing worker signature survived preflight: census=%v error=%v", census, err)
	}
	fixture.retain(t, 1)
	fixture.retain(t, 2)
	census, err := executor.readEvidenceRelayContinuationTransactionCensus(t.Context(), 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(census.exposure.Transactions) != 3 || census.exposure.Liability != "693009" || len(census.nonces) != 1 || census.nonces[0].Pending != 2 {
		t.Fatalf("original/replacement signatures lost their complete liabilities: %+v", census)
	}
	for index, transaction := range fixture.transactions {
		raw, err := census.exposure.Transactions[transaction.Hash()].MarshalBinary()
		if err != nil || !bytes.Equal(raw, fixture.raw[index]) {
			t.Fatalf("retained attempt %d changed signed bytes: %v", index, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(executor.stateDir, "transactions", stringsTrim0x(fixture.transactions[0].Hash().Hex())+".rlp"))
	if err != nil || !bytes.Equal(before, after) || len(executor.journal.Entries()) != 0 {
		t.Fatalf("read-only census changed existing evidence: %v", err)
	}
	if err := executor.recheckEvidenceRelayContinuationTransactionCensus(t.Context(), 40, census); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceRelayContinuationTransactionsRetainOriginalQueuesAndRenewals(t *testing.T) {
	fixture := newEvidenceRelayContinuationTransactionFixture(t)
	executor := fixture.executor
	executor.cfg.Config.Topology.Miners = 1
	queued := map[string]any{"schema": "urnetwork-provider-claim-queue-v1", "entries": map[string]any{"signed": map[string]string{"tx_hash": fixture.transactions[1].Hash().Hex(), "raw_tx_hex": "0x" + hex.EncodeToString(fixture.raw[1])}}}
	raw, err := json.Marshal(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(executor.stateDir, "runtime/miner-1/claims/claim-queue.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	executor.plan.FleetRenewals = []FleetRenewal{{TransactionEvidence: []string{"0x" + hex.EncodeToString(fixture.raw[2])}}}
	census, err := executor.readEvidenceRelayContinuationTransactionCensus(t.Context(), 40)
	if err != nil || len(census.exposure.Transactions) != 3 || census.exposure.Liability != "693009" {
		t.Fatalf("original claim/renewal signatures escaped the union: %v", err)
	}
	// Restoring a byte-identical duplicate must not change the sealed union.
	fixture.retain(t, 1)
	if err := executor.recheckEvidenceRelayContinuationTransactionCensus(t.Context(), 40, census); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceRelayContinuationTransactionsRejectChangesDuringReplay(t *testing.T) {
	for _, mutation := range []string{"replacement", "removed", "tampered", "nonce", "cancelled"} {
		fixture := newEvidenceRelayContinuationTransactionFixture(t)
		fixture.retain(t, 1)
		executor := fixture.executor
		census, err := executor.readEvidenceRelayContinuationTransactionCensus(t.Context(), 40)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		path := filepath.Join(executor.stateDir, "transactions", stringsTrim0x(fixture.transactions[1].Hash().Hex())+".rlp")
		switch mutation {
		case "replacement":
			fixture.retain(t, 2)
		case "removed":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "tampered":
			if err := os.WriteFile(path, fixture.raw[2], 0o600); err != nil {
				t.Fatal(err)
			}
		case "nonce":
			for key := range fixture.reader.nonces {
				fixture.reader.nonces[key] = 3
			}
		case "cancelled":
			cancel()
		}
		err = executor.recheckEvidenceRelayContinuationTransactionCensus(ctx, 40, census)
		cancel()
		if err == nil || mutation == "replacement" && !strings.Contains(err.Error(), "changed during capture") || mutation == "cancelled" && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s changed sealed transaction evidence: %v", mutation, err)
		}
	}
}
