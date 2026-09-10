//go:build linux || darwin

// Genuine signatures, bounded contract responses and disk-backed journal rows
// exercise the funded sender through its real transport and recovery path.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Only serialized endpoint state changes; no acceptance callback is supplied
// to either the funded sender or the independent evidence verifier.
type evidenceRelayRpcFixture struct {
	manager               *EvmTxManager
	chain                 *validatorcomponent.ChainClient
	expected              validatorcomponent.ValidatorEvidenceTransactionV2Expected
	action                Action
	planHash              string
	stateDir              string
	key                   *ecdsa.PrivateKey
	transaction           *types.Transaction
	thirdPartyTransaction *types.Transaction
	transactionBytes      []byte
	calldata              []byte
	publicationKey        string
	responses             map[string][]byte
	stateLock             sync.Mutex
	mode                  string
	finalizedBlock        uint64
	finalizedHash         common.Hash
	winner                *types.Transaction
	receipts              map[common.Hash]*types.Receipt
	requestCounts         map[string]int
	sentBytes             [][]byte
	pendingNonceReads     int
}

// Preserve request identifiers even when a real client batches mixed methods.
type evidenceRelayRpcRequest struct {
	Id     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// Immutable chain identity and genuinely dual-signed evidence are generated
// before either real client is dialed. Every send must already exist on disk.
func newEvidenceRelayRpcFixture(t *testing.T, mode string) *evidenceRelayRpcFixture {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x71})
	if err != nil {
		t.Fatal(err)
	}
	vpk := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, 32))
	activation := protocol.ValidatorEvidenceActivation{
		Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: 945, GenesisHash: [32]byte{0x11}, Netuid: 521, Coordinator: [20]byte{0x12}, SettlementVault: [20]byte{0x13}, DeploymentIDHash: [32]byte{0x14}, PolicyHash: [32]byte{0x15}, Epoch: 7},
		Hotkey: hotkey.PublicKey(), NoID: 2, VPK: [32]byte(vpk[32:]), FirstSequence: 5, PriorRoot: [32]byte{0x23}, NativeBlock: 9000, NativeHash: [32]byte{0x24}, EVMBlock: 1000, EVMHash: [32]byte{0x25},
	}
	domain, err := activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	header := protocol.ValidatorEvidenceHeader{Domain: domain, Hotkey: activation.Hotkey, NoID: 2, Epoch: 7, Kind: protocol.ValidatorEvidenceClosedCensus, VPK: activation.VPK, BoundaryBlock: 1099, BoundaryHash: [32]byte{0x26}, CensusHash: [32]byte{0x27}, PayloadHash: [32]byte{0x28}, PayloadBytes: 5000}
	evidence := validatorcomponent.ValidatorEvidenceSignedV2{Schema: validatorcomponent.ValidatorEvidenceSignedV2Schema, Header: header}
	evidence.VPKSignature, err = header.SignVPK(vpk)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := header.Digest()
	if err != nil {
		t.Fatal(err)
	}
	evidence.HotkeySignature, err = hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: 7, StartBlock: 1000, EndBlock: 1100, FinalizedBlock: 1200}
	calldata, err := stabi.PackValidatorEvidenceCommitment(domain, window, header, evidence.VPKSignature, evidence.HotkeySignature)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{0x73}, 32))
	if err != nil {
		t.Fatal(err)
	}
	thirdPartyKey, err := crypto.ToECDSA(bytes.Repeat([]byte{0x74}, 32))
	if err != nil {
		t.Fatal(err)
	}
	journalAddress := common.Address{0x33}
	unsigned := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(945), Nonce: 4, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(100), Gas: 100000, To: &journalAddress, Value: new(big.Int), Data: calldata})
	transaction, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(945)), key)
	if err != nil {
		t.Fatal(err)
	}
	thirdPartyTransaction, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(945)), thirdPartyKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	code := []byte{0x60, 0x00, 0x00}
	expected := validatorcomponent.ValidatorEvidenceTransactionV2Expected{Journal: journalAddress, RuntimeHash: [32]byte(crypto.Keccak256Hash(code)), Activation: activation, Window: window, Evidence: evidence, Relayer: crypto.PubkeyToAddress(key.PublicKey), MaxTransactionBytes: 16 * 1024, MaxReceiptLogs: 4, MaxGas: 100000, MaxFeePerGas: 100}
	slot, err := header.SlotKey()
	if err != nil {
		t.Fatal(err)
	}
	action := Action{ID: fmt.Sprintf("evidence.relay.%x", slot), Kind: "evm-transaction", Target: journalAddress.Hex(), Description: "Publish exact immutable evidence", Parameters: map[string]string{
		"validator_evidence_slot": fmt.Sprintf("0x%x", slot), "validator_evidence_header_hash": fmt.Sprintf("0x%x", digest),
		evmMaximumGasUnitsParameter: "100000", evmMaximumFeePerGasParameter: "100",
	}, Spend: Spend{EVMGasWei: DecimalUint("10000000")}}
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	// TempDir's numbered child inherits the process umask. The real journal
	// requires private state regardless of its test capture's launch policy.
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(stateDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("relay fixture state root is not an actual private directory: %v", err)
	}
	fixture := &evidenceRelayRpcFixture{expected: expected, action: action, planHash: common.Hash{0x61}.Hex(), stateDir: stateDir, key: key, transaction: transaction, thirdPartyTransaction: thirdPartyTransaction, transactionBytes: raw, calldata: calldata, mode: mode, finalizedBlock: 1200, finalizedHash: common.Hash{0xa1}, responses: map[string][]byte{}, receipts: map[common.Hash]*types.Receipt{}, requestCounts: map[string]int{}}
	contract := stabi.NewSTValidatorEvidence()
	contractAbi, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []struct {
		name        string
		calldata    []byte
		value       any
		coordinator bool
	}{
		{name: "validatorEvidence", calldata: coordinator.PackValidatorEvidence(), value: journalAddress, coordinator: true},
		{name: "epochStartBlock", calldata: coordinator.PackEpochStartBlock(big.NewInt(7)), value: big.NewInt(1000), coordinator: true},
		{name: "epochEndBlock", calldata: coordinator.PackEpochEndBlock(big.NewInt(7)), value: big.NewInt(1100), coordinator: true},
		{name: "coordinator", calldata: contract.PackCoordinator(), value: common.Address(domain.Coordinator)},
		{name: "settlementVault", calldata: contract.PackSettlementVault(), value: common.Address(domain.SettlementVault)},
		{name: "chainId", calldata: contract.PackChainId(), value: domain.ChainID},
		{name: "netuid", calldata: contract.PackNetuid(), value: domain.Netuid},
		{name: "genesisHash", calldata: contract.PackGenesisHash(), value: domain.GenesisHash},
		{name: "deploymentIdHash", calldata: contract.PackDeploymentIdHash(), value: domain.DeploymentIDHash},
		{name: "activation", calldata: contract.PackActivation(domain.ActivationHash), value: stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(activation), PublishedBlock: 1001}},
	} {
		parsed, target := contractAbi, journalAddress
		if view.coordinator {
			parsed, target = coordinatorAbi, common.Address(domain.Coordinator)
		}
		encoded, err := parsed.Methods[view.name].Outputs.Pack(view.value)
		if err != nil {
			t.Fatal(err)
		}
		fixture.responses[target.Hex()+":"+hexutil.Encode(view.calldata)] = encoded
	}
	fixture.publicationKey = journalAddress.Hex() + ":" + hexutil.Encode(contract.PackCommitment(slot))
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHttp))
	t.Cleanup(server.Close)
	fixture.chain, err = validatorcomponent.DialReleaseChainContext(t.Context(), []string{server.URL}, common.Address(domain.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.chain.Close)
	client, err := ethclient.DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	fixture.manager = &EvmTxManager{client: client, chainID: big.NewInt(945), deploymentID: "relay-test-deployment", stateDir: fixture.stateDir, journal: journal, key: key}
	t.Cleanup(func() { _ = fixture.manager.journal.Close() })
	return fixture
}

// The real file is checked outside the fixture state lock, after the sender
// has durably published both its exact bytes and original broadcast metadata.
func (self *evidenceRelayRpcFixture) validateSend(raw []byte) error {
	if !bytes.Equal(raw, self.transactionBytes) {
		return errors.New("send changed the exact funded signed transaction")
	}
	persisted, err := readValidatorEvidenceHistoricalFile(self.stateDir, "transactions/"+stringsTrim0x(self.transaction.Hash().Hex())+".rlp", 64*1024)
	if err != nil || !bytes.Equal(raw, persisted) {
		return errors.Join(errors.New("send preceded exact durable signed bytes"), err)
	}
	entries, err := readJournalEntries(self.stateDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Stage == StageBroadcast && entry.DeploymentID == "relay-test-deployment" && entry.PlanHash == self.planHash && entry.ActionID == self.action.ID && entry.IntentHash == self.action.IntentHash && entry.TransactionHash == self.transaction.Hash().Hex() && entry.Signer == self.expected.Relayer.Hex() && entry.Nonce == "4" && entry.RecoveryBlock == 1200 && entry.RecoveryBlockHash == (common.Hash{0xa1}).Hex() {
			return nil
		}
	}
	return errors.New("send preceded the exact original durable broadcast")
}

// One explicit send changes endpoint state; the production polling loop then
// reads the actual receipt. A transport interruption never fabricates finality.
func (self *evidenceRelayRpcFixture) installPublicationWithLock(mode string) error {
	contractAbi, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		return err
	}
	header := self.expected.Evidence.Header
	digest, err := header.Digest()
	if err != nil {
		return err
	}
	slot, err := header.SlotKey()
	if err != nil {
		return err
	}
	event := contractAbi.Events[stabi.STValidatorEvidenceEvidenceCommittedEventName]
	data, err := event.Inputs.NonIndexed().Pack(digest, header.PayloadHash, header.CensusHash, header.PayloadBytes)
	if err != nil {
		return err
	}
	winner := self.transaction
	if mode == "third-party" || strings.HasPrefix(mode, "race") {
		winner = self.thirdPartyTransaction
	}
	index := uint(3)
	if winner == self.thirdPartyTransaction {
		index = 4
	}
	receipt := &types.Receipt{Type: winner.Type(), Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 80000, TxHash: winner.Hash(), GasUsed: 80000, EffectiveGasPrice: big.NewInt(50), BlockHash: common.Hash{0xb1}, BlockNumber: big.NewInt(1201), TransactionIndex: index}
	receipt.Logs = []*types.Log{{Address: self.expected.Journal, Topics: []common.Hash{event.ID, common.Hash(slot), common.BigToHash(big.NewInt(2)), common.BigToHash(big.NewInt(7))}, Data: data, BlockNumber: 1201, TxHash: winner.Hash(), TxIndex: index, BlockHash: receipt.BlockHash, Index: 4}}
	self.receipts[winner.Hash()] = receipt
	if strings.HasPrefix(mode, "race") {
		failed := &types.Receipt{Type: self.transaction.Type(), Status: types.ReceiptStatusFailed, CumulativeGasUsed: 80000, TxHash: self.transaction.Hash(), GasUsed: 80000, EffectiveGasPrice: big.NewInt(50), BlockHash: common.Hash{0xb2}, BlockNumber: big.NewInt(1202), TransactionIndex: 3, Logs: []*types.Log{}}
		switch mode {
		case "race-cost":
			failed.EffectiveGasPrice = big.NewInt(101)
		case "race-logs":
			failed.Logs = receipt.Logs
		case "race-no-winner":
			delete(self.receipts, winner.Hash())
			winner = nil
		}
		self.receipts[self.transaction.Hash()] = failed
	}
	self.winner, self.finalizedBlock, self.finalizedHash = winner, 1210, common.Hash{0xa2}
	return nil
}

// Test-side transitions and counters never borrow mutable server-owned maps.
func (self *evidenceRelayRpcFixture) installPublication(t *testing.T, mode string) {
	t.Helper()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := self.installPublicationWithLock(mode); err != nil {
		t.Fatal(err)
	}
}

// Count all actual requests, including chain identity calls made during dial.
func (self *evidenceRelayRpcFixture) requestCount(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if method == "pending-nonce" {
		return self.pendingNonceReads
	}
	if method != "" {
		return self.requestCounts[method]
	}
	count := 0
	for _, value := range self.requestCounts {
		count += value
	}
	return count
}

// Reopening takes the real deployment lock and verifies every hash-chained row.
func (self *evidenceRelayRpcFixture) reopen(t *testing.T) {
	t.Helper()
	if err := self.manager.journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(self.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	self.manager = &EvmTxManager{client: self.manager.client, chainID: big.NewInt(945), deploymentID: "relay-test-deployment", stateDir: self.stateDir, journal: journal, key: self.key}
}

// Produces realistic interrupted custody without bypassing journal validation.
func (self *evidenceRelayRpcFixture) retain(t *testing.T, transaction *types.Transaction, signer, nonce string, later JournalStage) {
	t.Helper()
	raw, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{DeploymentID: self.manager.deploymentID, PlanHash: self.planHash, ActionID: self.action.ID, IntentHash: self.action.IntentHash, Stage: StageIntent}
	if err := self.manager.journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	entry.Stage, entry.TransactionHash, entry.Signer, entry.Nonce = StageBroadcast, transaction.Hash().Hex(), signer, nonce
	entry.RecoveryBlock, entry.RecoveryBlockHash = 1200, (common.Hash{0xa1}).Hex()
	if err := self.manager.journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	if later != "" {
		entry.Stage, entry.Signer, entry.Nonce, entry.RecoveryBlock, entry.RecoveryBlockHash = later, "", "", 0, ""
		entry.BlockNumber, entry.BlockHash = 1201, (common.Hash{0xb1}).Hex()
		if later == StageFailed {
			entry.BlockNumber, entry.BlockHash, entry.Error = 1202, (common.Hash{0xb2}).Hex(), "interrupted canonical publication race"
		}
		if err := self.manager.journal.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
}

// The transport supports actual mixed batches with deliberately reversed ids.
func (self *evidenceRelayRpcFixture) serveHttp(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil || len(body) == 0 {
		http.Error(w, "invalid request", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if body[0] == '[' {
		var requests []evidenceRelayRpcRequest
		if json.Unmarshal(body, &requests) != nil {
			http.Error(w, "invalid batch", 400)
			return
		}
		responses := make([]json.RawMessage, len(requests))
		for index, request := range requests {
			responses[len(requests)-1-index] = self.respond(request)
		}
		_ = json.NewEncoder(w).Encode(responses)
		return
	}
	var request evidenceRelayRpcRequest
	if json.Unmarshal(body, &request) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	_, _ = w.Write(self.respond(request))
}

// File custody is observed outside the fixture lock; responses are encoded
// while owned so another request cannot mutate a partially serialized answer.
func (self *evidenceRelayRpcFixture) respond(request evidenceRelayRpcRequest) []byte {
	var sendBytes hexutil.Bytes
	var custodyErr error
	if request.Method == "eth_sendRawTransaction" {
		if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &sendBytes) != nil {
			custodyErr = errors.New("invalid raw transaction request")
		} else {
			custodyErr = self.validateSend(sendBytes)
		}
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.requestCounts[request.Method]++
	response := map[string]any{"jsonrpc": "2.0", "id": request.Id}
	value, err := self.resultWithLock(request, sendBytes, custodyErr)
	if err != nil {
		response["error"] = map[string]any{"code": -32000, "message": err.Error()}
	} else {
		response["result"] = value
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"fixture encoding failed"}}`)
	}
	return encoded
}

// Each branch supplies protocol bytes, never a verifier or sender verdict.
func (self *evidenceRelayRpcFixture) resultWithLock(request evidenceRelayRpcRequest, sendBytes []byte, custodyErr error) (any, error) {
	fail := func(message string) (any, error) { return nil, errors.New(message) }
	switch request.Method {
	case "eth_chainId":
		return "0x3b1", nil
	case "eth_getBlockByNumber":
		var selector string
		if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &selector) != nil {
			return fail("bad numbered lookup")
		}
		number, hash := self.finalizedBlock, self.finalizedHash
		if selector != "finalized" && selector != "latest" {
			var err error
			number, err = hexutil.DecodeUint64(selector)
			if err != nil {
				return nil, err
			}
			switch number {
			case 1200:
				hash = common.Hash{0xa1}
			case 1201:
				hash = common.Hash{0xb1}
			case 1202:
				if self.mode == "race-canonical-error" {
					return fail("own reverted inclusion canonical lookup failed")
				}
				hash = common.Hash{0xb2}
			case 1210:
				hash = common.Hash{0xa2}
			default:
				return fail("unexpected numbered block")
			}
		}
		header := &types.Header{Number: new(big.Int).SetUint64(number), Difficulty: new(big.Int), GasLimit: 30000000, Time: number, BaseFee: big.NewInt(49)}
		encoded, err := header.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var value map[string]any
		if err := json.Unmarshal(encoded, &value); err != nil {
			return nil, err
		}
		value["hash"] = hash
		return value, nil
	case "eth_getBlockByHash":
		var hash common.Hash
		if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &hash) != nil || hash != self.finalizedHash {
			return fail("unexpected finalized block hash")
		}
		return map[string]any{"number": hexutil.EncodeUint64(self.finalizedBlock), "hash": hash}, nil
	case "eth_maxPriorityFeePerGas":
		return "0x2", nil
	case "eth_getBalance", "eth_getTransactionCount":
		var address common.Address
		var selector string
		if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &address) != nil || address != self.expected.Relayer || json.Unmarshal(request.Params[1], &selector) != nil {
			return fail("unbound funded account lookup")
		}
		if request.Method == "eth_getBalance" {
			if selector != "latest" {
				return fail("unexpected balance selector")
			}
			return "0x100000000", nil
		}
		if selector == "pending" {
			self.pendingNonceReads++
		} else if selector != hexutil.EncodeUint64(self.finalizedBlock) {
			return fail("nonce recovery did not use the exact finalized number")
		}
		return "0x4", nil
	case "eth_estimateGas":
		var call struct {
			From  common.Address `json:"from"`
			To    common.Address `json:"to"`
			Input hexutil.Bytes  `json:"input"`
			Value hexutil.Big    `json:"value"`
		}
		if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &call) != nil || call.From != self.expected.Relayer || call.To != self.expected.Journal || !bytes.Equal(call.Input, self.calldata) || (*big.Int)(&call.Value).Sign() != 0 {
			return fail("estimate escaped exact funded zero-value evidence call")
		}
		return "0xf424", nil
	case "eth_sendRawTransaction":
		if custodyErr != nil {
			return nil, custodyErr
		}
		self.sentBytes = append(self.sentBytes, bytes.Clone(sendBytes))
		if self.mode == "send-error" {
			return fail("deterministic transport interruption after durable send")
		}
		if err := self.installPublicationWithLock(self.mode); err != nil {
			return nil, err
		}
		return self.transaction.Hash(), nil
	case "eth_getTransactionReceipt":
		var hash common.Hash
		if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &hash) != nil || (hash != self.transaction.Hash() && hash != self.thirdPartyTransaction.Hash()) {
			return fail("receipt did not use an exact signed transaction hash")
		}
		if receipt := self.receipts[hash]; receipt != nil {
			return receipt, nil
		}
		return nil, nil
	case "eth_getTransactionByHash":
		var hash common.Hash
		if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &hash) != nil {
			return fail("bad transaction hash")
		}
		transaction := self.transaction
		if hash == self.thirdPartyTransaction.Hash() {
			transaction = self.thirdPartyTransaction
		} else if hash != transaction.Hash() {
			return fail("wrong transaction hash")
		}
		receipt := self.receipts[hash]
		if receipt == nil {
			return nil, nil
		}
		encoded, err := transaction.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var value map[string]any
		if err := json.Unmarshal(encoded, &value); err != nil {
			return nil, err
		}
		value["blockNumber"], value["blockHash"], value["transactionIndex"] = hexutil.EncodeBig(receipt.BlockNumber), receipt.BlockHash, hexutil.EncodeUint64(uint64(receipt.TransactionIndex))
		return value, nil
	case "eth_getTransactionByBlockHashAndIndex":
		var hash common.Hash
		var index hexutil.Uint64
		if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &hash) != nil || json.Unmarshal(request.Params[1], &index) != nil {
			return fail("bad transaction inclusion position")
		}
		for _, transaction := range []*types.Transaction{self.transaction, self.thirdPartyTransaction} {
			receipt := self.receipts[transaction.Hash()]
			if receipt != nil && receipt.BlockHash == hash && uint64(receipt.TransactionIndex) == uint64(index) {
				if self.mode == "race-body" && transaction == self.transaction {
					return self.thirdPartyTransaction, nil
				}
				return transaction, nil
			}
		}
		return fail("transaction inclusion did not bind exact block and index")
	case "eth_getLogs":
		var filter struct {
			BlockHash common.Hash      `json:"blockHash"`
			Addresses []common.Address `json:"address"`
			Topics    [][]common.Hash  `json:"topics"`
		}
		if self.winner == nil || len(request.Params) != 1 || json.Unmarshal(request.Params[0], &filter) != nil {
			return fail("unexpected winner event lookup")
		}
		receipt := self.receipts[self.winner.Hash()]
		if filter.BlockHash != receipt.BlockHash || len(filter.Addresses) != 1 || filter.Addresses[0] != self.expected.Journal || len(filter.Topics) != 4 {
			return fail("unbound winner event query")
		}
		for index, topic := range receipt.Logs[0].Topics {
			if len(filter.Topics[index]) != 1 || filter.Topics[index][0] != topic {
				return fail("wrong winner topic")
			}
		}
		return receipt.Logs, nil
	case "eth_getCode", "eth_call":
		var selector gethrpc.BlockNumberOrHash
		if len(request.Params) != 2 || json.Unmarshal(request.Params[1], &selector) != nil || selector.BlockHash == nil || *selector.BlockHash != self.finalizedHash || !selector.RequireCanonical || selector.BlockNumber != nil {
			return fail("state observation omitted exact canonical finalized hash")
		}
		if request.Method == "eth_getCode" {
			var address common.Address
			if json.Unmarshal(request.Params[0], &address) != nil || address != self.expected.Journal {
				return fail("wrong immutable runtime target")
			}
			return "0x600000", nil
		}
		var call struct {
			To    common.Address `json:"to"`
			Input hexutil.Bytes  `json:"input"`
		}
		if json.Unmarshal(request.Params[0], &call) != nil {
			return fail("invalid state call")
		}
		key := call.To.Hex() + ":" + hexutil.Encode(call.Input)
		if key == self.publicationKey {
			stored := stabi.STValidatorEvidenceCommitment{}
			if self.winner != nil {
				stored = stabi.STValidatorEvidenceCommitment{Header: stabi.ValidatorEvidenceHeaderFromProtocol(self.expected.Evidence.Header), PublishedBlock: 1201}
			}
			contractAbi, err := stabi.STValidatorEvidenceMetaData.ParseABI()
			if err != nil {
				return nil, err
			}
			encoded, err := contractAbi.Methods["commitment"].Outputs.Pack(stored)
			return hexutil.Encode(encoded), err
		}
		encoded, found := self.responses[key]
		if !found {
			return fail("wrong complete state calldata or target")
		}
		return hexutil.Encode(encoded), nil
	default:
		return fail("unexpected method " + request.Method + " with " + strconv.Itoa(len(request.Params)) + " parameters")
	}
}
