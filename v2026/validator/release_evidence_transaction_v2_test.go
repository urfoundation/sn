//go:build linux || darwin

// Real signed Ethereum transactions, both evidence consents and complete RPC
// responses exercise finality/readback without replacing an acceptance verdict.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Faults change actual serialized endpoint responses, never a verifier result.
type evidenceTransactionV2Fixture struct {
	chain       *ChainClient
	expected    ValidatorEvidenceTransactionV2Expected
	receipt     *types.Receipt
	transaction *types.Transaction
	stateLock   sync.Mutex
	requests    map[string]int
}

// Counts are copied under their small lock, outside any production callback.
func (self *evidenceTransactionV2Fixture) requestCount(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.requests[method]
}

// Configure faults before the server starts. The only runtime hook is an
// explicit final numbered-header boundary used for cancellation/alias controls.
func newEvidenceTransactionV2Fixture(t *testing.T, fault string, boundary func()) *evidenceTransactionV2Fixture {
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
	evidence := ValidatorEvidenceSignedV2{Schema: ValidatorEvidenceSignedV2Schema, Header: header}
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
	journal := common.Address{0x33}
	transactionTarget, transactionData, transactionGas := &journal, calldata, uint64(100000)
	if fault == "winner-forwarded" || fault == "winner-other-slot" {
		forwarder := common.Address{0x34}
		transactionTarget, transactionData, transactionGas = &forwarder, []byte{0xde, 0xad}, 200000
	} else if fault == "winner-created" {
		transactionTarget, transactionData, transactionGas = nil, []byte{0x60, 0x00}, 200000
	}
	unsigned := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(945), Nonce: 4, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(100), Gas: transactionGas, To: transactionTarget, Value: new(big.Int), Data: transactionData})
	transaction, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(945)), key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	code := []byte{0x60, 0x00, 0x00}
	expected := ValidatorEvidenceTransactionV2Expected{Journal: journal, RuntimeHash: [32]byte(crypto.Keccak256Hash(code)), Activation: activation, Window: window, Evidence: evidence, Relayer: crypto.PubkeyToAddress(key.PublicKey), SignedTransaction: raw, MaxTransactionBytes: 16 * 1024, MaxReceiptLogs: 4, MaxGas: 100000, MaxFeePerGas: 100}
	contract := stabi.NewSTValidatorEvidence()
	contractABI, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	event := contractABI.Events[stabi.STValidatorEvidenceEvidenceCommittedEventName]
	slot, err := header.SlotKey()
	if err != nil {
		t.Fatal(err)
	}
	data, err := event.Inputs.NonIndexed().Pack(digest, header.PayloadHash, header.CensusHash, header.PayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	finalHash, includedHash := common.Hash{0xa1}, common.Hash{0xb1}
	receipt := &types.Receipt{Type: transaction.Type(), Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 80000, TxHash: transaction.Hash(), GasUsed: 80000, EffectiveGasPrice: big.NewInt(50), BlockHash: includedHash, BlockNumber: big.NewInt(1101), TransactionIndex: 3}
	receipt.Logs = []*types.Log{{Address: journal, Topics: []common.Hash{event.ID, common.Hash(slot), common.BigToHash(big.NewInt(2)), common.BigToHash(big.NewInt(7))}, Data: data, BlockNumber: 1101, TxHash: transaction.Hash(), TxIndex: 3, BlockHash: includedHash, Index: 4}}
	if fault == "winner-created" {
		receipt.ContractAddress = crypto.CreateAddress(expected.Relayer, transaction.Nonce())
	}
	if fault == "winner-other-slot" {
		other := *receipt.Logs[0]
		other.Topics = slices.Clone(other.Topics)
		other.Topics[1][0] ^= 1
		other.Index++
		receipt.Logs = append(receipt.Logs, &other)
	}
	fixture := &evidenceTransactionV2Fixture{expected: expected, receipt: receipt, transaction: transaction, requests: map[string]int{}}
	stored := stabi.STValidatorEvidenceCommitment{Header: stabi.ValidatorEvidenceHeaderFromProtocol(header), PublishedBlock: 1101}
	switch fault {
	case "receipt-hash":
		receipt.TxHash[0] ^= 1
	case "receipt-number":
		receipt.BlockNumber = new(big.Int).Lsh(big.NewInt(1), 65)
	case "receipt-zero-number":
		receipt.BlockNumber = new(big.Int)
	case "receipt-zero-hash":
		receipt.BlockHash = common.Hash{}
	case "receipt-type":
		receipt.Type = types.LegacyTxType
	case "receipt-create":
		receipt.ContractAddress = journal
	case "receipt-gas":
		receipt.GasUsed = transaction.Gas() + 1
	case "receipt-cumulative":
		receipt.CumulativeGasUsed = receipt.GasUsed - 1
	case "receipt-price":
		receipt.EffectiveGasPrice = big.NewInt(101)
	case "receipt-missing-price":
		receipt.EffectiveGasPrice = nil
	case "receipt-reverted":
		receipt.Status = types.ReceiptStatusFailed
	case "receipt-future":
		receipt.BlockNumber = big.NewInt(1201)
	case "event-missing":
		receipt.Logs = nil
	case "event-duplicate":
		copy := *receipt.Logs[0]
		receipt.Logs = append(receipt.Logs, &copy)
	case "event-suffix":
		receipt.Logs[0].Data = append(bytes.Clone(data), make([]byte, 32)...)
	case "event-padding":
		receipt.Logs[0].Data = bytes.Clone(data)
		receipt.Logs[0].Data[96] = 1
	case "event-topic":
		receipt.Logs[0].Topics[1][0] ^= 1
	case "event-count":
		receipt.Logs[0].Topics = receipt.Logs[0].Topics[:3]
	case "event-removed":
		receipt.Logs[0].Removed = true
	case "event-block":
		receipt.Logs[0].BlockHash[0] ^= 1
	case "event-transaction":
		receipt.Logs[0].TxHash[0] ^= 1
	case "event-index":
		receipt.Logs[0].TxIndex++
	case "event-address":
		receipt.Logs[0].Address[0] ^= 1
	case "stored-header":
		stored.Header.CensusHash[0] ^= 1
	case "stored-publication":
		stored.PublishedBlock++
	case "stored-absent":
		stored = stabi.STValidatorEvidenceCommitment{}
	case "runtime":
		code = append(code, 0)
	}
	responses := map[string][]byte{}
	for _, view := range []struct {
		name        string
		calldata    []byte
		value       any
		coordinator bool
	}{
		{name: "validatorEvidence", calldata: coordinator.PackValidatorEvidence(), value: journal, coordinator: true},
		{name: "epochStartBlock", calldata: coordinator.PackEpochStartBlock(big.NewInt(7)), value: big.NewInt(1000), coordinator: true},
		{name: "epochEndBlock", calldata: coordinator.PackEpochEndBlock(big.NewInt(7)), value: big.NewInt(1100), coordinator: true},
		{name: "coordinator", calldata: contract.PackCoordinator(), value: common.Address(domain.Coordinator)},
		{name: "settlementVault", calldata: contract.PackSettlementVault(), value: common.Address(domain.SettlementVault)},
		{name: "chainId", calldata: contract.PackChainId(), value: domain.ChainID},
		{name: "netuid", calldata: contract.PackNetuid(), value: domain.Netuid},
		{name: "genesisHash", calldata: contract.PackGenesisHash(), value: domain.GenesisHash},
		{name: "deploymentIdHash", calldata: contract.PackDeploymentIdHash(), value: domain.DeploymentIDHash},
		{name: "activation", calldata: contract.PackActivation(domain.ActivationHash), value: stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(activation), PublishedBlock: 1001}},
		{name: "commitment", calldata: contract.PackCommitment(slot), value: stored},
	} {
		parsed, target := contractABI, journal
		if view.coordinator {
			parsed, target = coordinatorABI, common.Address(domain.Coordinator)
		}
		encoded, err := parsed.Methods[view.name].Outputs.Pack(view.value)
		if err != nil {
			t.Fatal(err)
		}
		if fault == "window" && view.name == "epochStartBlock" {
			encoded[len(encoded)-1] ^= 1
		}
		if fault == "window-suffix" && view.name == "epochEndBlock" {
			encoded = append(encoded, make([]byte, 32)...)
		}
		responses[target.Hex()+":"+hexutil.Encode(view.calldata)] = encoded
	}
	respond := func(request chainBatchRPCRequest) map[string]any {
		count := func() int {
			fixture.stateLock.Lock()
			defer fixture.stateLock.Unlock()
			fixture.requests[request.Method]++
			return fixture.requests[request.Method]
		}()
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		fail := func(message string) map[string]any {
			response["error"] = map[string]any{"code": -32000, "message": message}
			return response
		}
		switch request.Method {
		case "eth_chainId":
			response["result"] = "0x3b1"
		case "eth_getBlockByNumber":
			var selector string
			if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &selector) != nil {
				return fail("bad numbered lookup")
			}
			switch selector {
			case "finalized", "0x4b0":
				if selector == "0x4b0" && boundary != nil {
					boundary()
				}
				hash := finalHash
				if selector == "0x4b0" && fault == "finalized-reorg" {
					hash[0] ^= 1
				}
				response["result"] = map[string]any{"number": "0x4b0", "hash": hash}
			case "0x44d":
				hash := includedHash
				if fault == "inclusion-reorg" || (fault == "late-inclusion-reorg" && count > 2) {
					hash[0] ^= 1
				}
				response["result"] = map[string]any{"number": "0x44d", "hash": hash}
			default:
				return fail("unexpected numbered block " + selector)
			}
		case "eth_getBlockByHash":
			var hash common.Hash
			if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &hash) != nil || hash != finalHash {
				return fail("unexpected block hash")
			}
			response["result"] = map[string]any{"number": "0x4b0", "hash": finalHash}
		case "eth_getTransactionReceipt":
			var hash common.Hash
			if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &hash) != nil || hash != transaction.Hash() {
				return fail("receipt did not use exact persisted hash")
			}
			if fault == "receipt-error" {
				return fail("actual receipt transport error")
			}
			if fault == "receipt-absent" {
				response["result"] = nil
			} else {
				response["result"] = receipt
			}
		case "eth_getLogs":
			var filter struct {
				BlockHash common.Hash      `json:"blockHash"`
				Addresses []common.Address `json:"address"`
				Topics    [][]common.Hash  `json:"topics"`
			}
			if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &filter) != nil || filter.BlockHash != includedHash || len(filter.Addresses) != 1 || filter.Addresses[0] != journal || len(filter.Topics) != 4 {
				return fail("unbound winner event query")
			}
			for index, topic := range []common.Hash{event.ID, common.Hash(slot), common.BigToHash(big.NewInt(2)), common.BigToHash(big.NewInt(7))} {
				if len(filter.Topics[index]) != 1 || filter.Topics[index][0] != topic {
					return fail("wrong winner topic")
				}
			}
			if fault == "winner-other-slot" {
				response["result"] = receipt.Logs[:1]
			} else {
				response["result"] = receipt.Logs
			}
		case "eth_getTransactionByHash":
			var hash common.Hash
			if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &hash) != nil || hash != transaction.Hash() {
				return fail("wrong discovered transaction hash")
			}
			encoded, err := transaction.MarshalJSON()
			if err != nil {
				return fail(err.Error())
			}
			var value map[string]any
			if err := json.Unmarshal(encoded, &value); err != nil {
				return fail(err.Error())
			}
			if fault != "winner-pending" {
				value["blockNumber"] = "0x44d"
			}
			value["blockHash"], value["transactionIndex"] = includedHash, "0x3"
			response["result"] = value
		case "eth_getTransactionByBlockHashAndIndex":
			var hash common.Hash
			var index hexutil.Uint64
			if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &hash) != nil || json.Unmarshal(request.Params[1], &index) != nil || hash != includedHash || index != 3 {
				return fail("wrong transaction position")
			}
			if fault == "transaction-absent" {
				response["result"] = nil
				break
			}
			included := transaction
			if fault == "transaction-other" {
				alternate := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(945), Nonce: 5, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(100), Gas: 100000, To: &journal, Value: new(big.Int), Data: calldata})
				var err error
				included, err = types.SignTx(alternate, types.LatestSignerForChainID(big.NewInt(945)), key)
				if err != nil {
					return fail(err.Error())
				}
			}
			response["result"] = included
		case "eth_getCode", "eth_call":
			var selector gethrpc.BlockNumberOrHash
			if len(request.Params) != 2 || json.Unmarshal(request.Params[1], &selector) != nil || selector.BlockHash == nil || *selector.BlockHash != finalHash || !selector.RequireCanonical || selector.BlockNumber != nil {
				return fail("unbound state observation")
			}
			if request.Method == "eth_getCode" {
				var target common.Address
				if json.Unmarshal(request.Params[0], &target) != nil || target != journal {
					return fail("wrong runtime target")
				}
				response["result"] = hexutil.Encode(code)
				break
			}
			var call struct {
				To    common.Address `json:"to"`
				Input hexutil.Bytes  `json:"input"`
			}
			if json.Unmarshal(request.Params[0], &call) != nil {
				return fail("invalid state call")
			}
			encoded, found := responses[call.To.Hex()+":"+hexutil.Encode(call.Input)]
			if !found {
				return fail("wrong state target or complete calldata")
			}
			response["result"] = hexutil.Encode(encoded)
		default:
			return fail("unexpected method " + request.Method)
		}
		return response
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil || len(body) == 0 {
			http.Error(w, "invalid request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if body[0] == '[' {
			var requests []chainBatchRPCRequest
			if json.Unmarshal(body, &requests) != nil {
				http.Error(w, "invalid batch", 400)
				return
			}
			responses := make([]map[string]any, len(requests))
			for index, request := range requests {
				responses[len(requests)-1-index] = respond(request)
			}
			_ = json.NewEncoder(w).Encode(responses)
		} else {
			var request chainBatchRPCRequest
			if json.Unmarshal(body, &request) != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(respond(request))
		}
	}))
	t.Cleanup(server.Close)
	fixture.chain, err = DialReleaseChainContext(t.Context(), []string{server.URL}, common.Address(domain.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.chain.Close)
	return fixture
}

// All three actual custody sources must be read before returning owned proof.
func TestValidatorEvidenceTransactionV2ConfirmsExactCanonicalPublication(t *testing.T) {
	fixture := newEvidenceTransactionV2Fixture(t, "", nil)
	result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
	if err != nil || result == nil {
		t.Fatalf("exact transaction confirmation: %v", err)
	}
	if result.Receipt.TxHash != fixture.transaction.Hash() || result.Publication.Header != fixture.expected.Evidence.Header || result.Publication.PublishedBlock != 1101 || result.FinalizedBlock != 1200 || result.FinalizedHash != ([32]byte{0xa1}) || !bytes.Equal(result.SignedTransaction, fixture.expected.SignedTransaction) {
		t.Fatal("confirmation lost exact immutable evidence")
	}
	if fixture.requestCount("eth_getTransactionReceipt") != 1 || fixture.requestCount("eth_getTransactionByBlockHashAndIndex") != 1 || fixture.requestCount("eth_getCode") != 1 || fixture.requestCount("eth_call") != 11 {
		t.Fatal("confirmation skipped an independent evidence source")
	}
	result.SignedTransaction[0] ^= 1
	if bytes.Equal(result.SignedTransaction, fixture.expected.SignedTransaction) {
		t.Fatal("returned transaction borrows caller bytes")
	}
}

// Receipt RPC does not itself bind the returned transactionHash to its argument.
func TestValidatorEvidenceTransactionV2RejectsWrongReceiptIdentity(t *testing.T) {
	fixture := newEvidenceTransactionV2Fixture(t, "receipt-hash", nil)
	result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
	if err == nil || result != nil || !strings.Contains(err.Error(), "receipt differs") || fixture.requestCount("eth_getTransactionByBlockHashAndIndex") != 0 {
		t.Fatalf("wrong receipt reached publication: %v", err)
	}
}

// Nil/malformed field, gas and fee variations use the same real JSON decoder.
func TestValidatorEvidenceTransactionV2RejectsAdjacentReceiptEnvelopeFaults(t *testing.T) {
	for _, fault := range []string{"receipt-number", "receipt-zero-number", "receipt-zero-hash", "receipt-type", "receipt-create", "receipt-gas", "receipt-cumulative", "receipt-price", "receipt-missing-price", "receipt-reverted"} {
		fixture := newEvidenceTransactionV2Fixture(t, fault, nil)
		result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
		if err == nil || result != nil || errors.Is(err, ErrValidatorEvidenceTransactionPending) {
			t.Fatalf("%s receipt admitted or mislabeled pending: %v", fault, err)
		}
	}
}

// Not found and a genuine future receipt are distinguished from transport errors.
func TestValidatorEvidenceTransactionV2DistinguishesPendingFromRPCFailure(t *testing.T) {
	for _, fault := range []string{"receipt-absent", "receipt-future", "receipt-error"} {
		fixture := newEvidenceTransactionV2Fixture(t, fault, nil)
		result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
		if err == nil || result != nil || errors.Is(err, ErrValidatorEvidenceTransactionPending) != (fault != "receipt-error") {
			t.Fatalf("%s pending classification: %v", fault, err)
		}
	}
}

// Exact block position and the final numbered hash rechecks are independent.
func TestValidatorEvidenceTransactionV2RejectsReorgAndWrongInclusion(t *testing.T) {
	for _, fault := range []string{"inclusion-reorg", "late-inclusion-reorg", "finalized-reorg", "transaction-absent", "transaction-other"} {
		fixture := newEvidenceTransactionV2Fixture(t, fault, nil)
		result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
		if err == nil || result != nil {
			t.Fatalf("%s inclusion admitted", fault)
		}
	}
}

// No ABI prefix, numeric padding or duplicated/transplanted log can attest a slot.
func TestValidatorEvidenceTransactionV2RejectsEveryEventSubstitution(t *testing.T) {
	for _, fault := range []string{"event-missing", "event-duplicate", "event-suffix", "event-padding", "event-topic", "event-count", "event-removed", "event-block", "event-transaction", "event-index", "event-address"} {
		fixture := newEvidenceTransactionV2Fixture(t, fault, nil)
		result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
		if err == nil || result != nil {
			t.Fatalf("%s event admitted", fault)
		}
	}
}

// A genuine receipt cannot substitute for immutable contract/domain/window state.
func TestValidatorEvidenceTransactionV2RequiresExactContractReadback(t *testing.T) {
	for _, fault := range []string{"stored-header", "stored-publication", "stored-absent", "runtime", "window", "window-suffix"} {
		fixture := newEvidenceTransactionV2Fixture(t, fault, nil)
		result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
		if err == nil || result != nil {
			t.Fatalf("%s contract state admitted", fault)
		}
	}
}

// Admission refuses invalid authority before a real network lookup occurs.
func TestValidatorEvidenceTransactionV2AdmissionPrecedesRPC(t *testing.T) {
	fixture := newEvidenceTransactionV2Fixture(t, "", nil)
	for _, fault := range []string{"raw-bound", "log-bound", "gas-bound", "fee-bound", "relayer", "consent", "calldata", "runtime", "domain"} {
		expected := fixture.expected
		expected.SignedTransaction = bytes.Clone(expected.SignedTransaction)
		expected.Evidence.VPKSignature = bytes.Clone(expected.Evidence.VPKSignature)
		switch fault {
		case "raw-bound":
			expected.MaxTransactionBytes = uint64(len(expected.SignedTransaction)) - 1
		case "log-bound":
			expected.MaxReceiptLogs = 0
		case "gas-bound":
			expected.MaxGas--
		case "fee-bound":
			expected.MaxFeePerGas--
		case "relayer":
			expected.Relayer[0] ^= 1
		case "consent":
			expected.Evidence.VPKSignature[0] ^= 1
		case "calldata":
			expected.SignedTransaction[len(expected.SignedTransaction)-1] ^= 1
		case "runtime":
			expected.RuntimeHash = [32]byte{}
		case "domain":
			expected.Activation.Domain.Netuid++
		}
		result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), expected)
		if err == nil || result != nil || fixture.requestCount("eth_getBlockByNumber") != 0 {
			t.Fatalf("%s authority reached RPC: %v", fault, err)
		}
	}
}

// Exact raw/log/gas/fee boundaries work; one-less limits refuse before publication.
func TestValidatorEvidenceTransactionV2BoundsAreExact(t *testing.T) {
	fixture := newEvidenceTransactionV2Fixture(t, "", nil)
	expected := fixture.expected
	expected.MaxTransactionBytes = uint64(len(expected.SignedTransaction))
	expected.MaxReceiptLogs = uint64(len(fixture.receipt.Logs))
	if result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), expected); err != nil || result == nil {
		t.Fatalf("exact bounds refused: %v", err)
	}
	duplicate := newEvidenceTransactionV2Fixture(t, "event-duplicate", nil)
	expected = duplicate.expected
	expected.MaxReceiptLogs = 1
	if result, err := duplicate.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), expected); err == nil || result != nil {
		t.Fatal("receipt log bound was ignored")
	}
}

// An explicit final-header callback cancels after all earlier observations.
func TestValidatorEvidenceTransactionV2JoinsLateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := newEvidenceTransactionV2Fixture(t, "", cancel)
	result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(ctx, fixture.expected)
	if !errors.Is(err, context.Canceled) || result != nil || fixture.requestCount("eth_getCode") != 1 {
		t.Fatalf("late cancellation published evidence: %v", err)
	}
}

// Explicit channels force mutation after admission and before final readback.
// The remote endpoint cannot make a borrowed caller slice become the evidence.
func TestValidatorEvidenceTransactionV2OwnsInputAcrossReadbackCallback(t *testing.T) {
	reached := make(chan struct{})
	resume := make(chan struct{})
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	defer release()
	fixture := newEvidenceTransactionV2Fixture(t, "", func() {
		close(reached)
		select {
		case <-resume:
		case <-t.Context().Done():
		}
	})
	want := slices.Clone(fixture.expected.SignedTransaction)
	type outcome struct {
		result *ValidatorEvidenceTransactionV2Finalized
		err    error
	}
	completed := make(chan outcome, 1)
	joined := make(chan struct{})
	defer func() { release(); <-joined }()
	go func() {
		defer close(joined)
		result, err := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context(t.Context(), fixture.expected)
		completed <- outcome{result: result, err: err}
	}()
	select {
	case <-reached:
	case result := <-completed:
		t.Fatalf("confirmation ended before the mutation barrier: %v", result.err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	fixture.expected.SignedTransaction[0] ^= 1
	fixture.expected.Evidence.VPKSignature[0] ^= 1
	release()
	result := <-completed
	if result.err != nil || result.result == nil || !bytes.Equal(result.result.SignedTransaction, want) {
		t.Fatalf("remote callback changed retained transaction: %v", result.err)
	}
}
