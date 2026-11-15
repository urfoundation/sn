// Exercise the production HTTP/RPC reader with explicit canonical selectors.
// The fixture is transport-only: its synthetic bytecode and unsigned records
// do not claim real chain inclusion or historical validator eligibility.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// All faults are fixed before the HTTP listener starts. Tests never mutate
// fixture state concurrently with a live RPC callback.
type evidenceChainFault struct {
	field           string
	word            int
	publication     uint64
	recordByte      bool
	trailing        bool
	padding         bool
	missing         bool
	dirtyAbsent     bool
	priorActivation bool
	runtime         bool
	reorg           bool
	rpcError        bool
}

// Every request is matched by target and complete calldata, including the
// activation digest or stable evidence slot, not merely a method selector.
type evidenceChainFixture struct {
	chain           *ChainClient
	journal         common.Address
	runtimeHash     [32]byte
	activation      protocol.ValidatorEvidenceActivation
	priorActivation protocol.ValidatorEvidenceActivation
	header          protocol.ValidatorEvidenceHeader
	window          protocol.ValidatorEvidenceWindow
	block           uint64
	blockHash       [32]byte
	viewCalls       atomic.Uint64
	codeCalls       atomic.Uint64
}

// Construct fixed ABI outputs before serving, then reverse batch responses to
// ensure results are joined by their actual JSON-RPC ids.
func newEvidenceChainFixture(t *testing.T, faults ...evidenceChainFault) *evidenceChainFixture {
	t.Helper()
	fixture := &evidenceChainFixture{
		journal: common.Address{0x33}, block: 1200, blockHash: [32]byte{0xa1},
		activation: protocol.ValidatorEvidenceActivation{
			Domain: protocol.ValidatorEvidenceActivationDomain{
				ChainID: 945, GenesisHash: [32]byte{0x11}, Netuid: 521,
				Coordinator: [20]byte{0x12}, SettlementVault: [20]byte{0x13},
				DeploymentIDHash: [32]byte{0x14}, PolicyHash: [32]byte{0x15}, Epoch: 7,
			},
			Hotkey: [32]byte{0x21}, NoID: 2, VPK: [32]byte{0x22}, FirstSequence: 5, PriorRoot: [32]byte{0x23},
			NativeBlock: 9000, NativeHash: [32]byte{0x24}, EVMBlock: 1000, EVMHash: [32]byte{0x25},
		},
		window: protocol.ValidatorEvidenceWindow{Epoch: 7, StartBlock: 1000, EndBlock: 1100, FinalizedBlock: 1200},
	}
	domain, err := fixture.activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	fixture.header = protocol.ValidatorEvidenceHeader{
		Domain: domain, Hotkey: fixture.activation.Hotkey, NoID: fixture.activation.NoID, Epoch: 7,
		Kind: protocol.ValidatorEvidenceClosedCensus, VPK: fixture.activation.VPK, BoundaryBlock: 1099,
		BoundaryHash: [32]byte{0x26}, CensusHash: [32]byte{0x27}, PayloadHash: [32]byte{0x28}, PayloadBytes: 5000,
	}
	code := []byte{0x60, 0x00, 0x00}
	fixture.runtimeHash = [32]byte(crypto.Keccak256Hash(code))
	for _, fault := range faults {
		if fault.runtime {
			code = append(code, 0x00)
		}
	}
	contract := stabi.NewSTValidatorEvidence()
	evidenceABI, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	slot, err := fixture.header.SlotKey()
	if err != nil {
		t.Fatal(err)
	}
	activation := stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(fixture.activation), PublishedBlock: 1001}
	commitment := stabi.STValidatorEvidenceCommitment{Header: stabi.ValidatorEvidenceHeaderFromProtocol(fixture.header), PublishedBlock: 1101}
	for _, fault := range faults {
		if fault.field == "activation" {
			if fault.missing {
				activation = stabi.STValidatorEvidenceActivation{}
			} else if fault.dirtyAbsent {
				activation.PublishedBlock = 0
			} else if fault.publication != 0 {
				activation.PublishedBlock = fault.publication
			}
		}
		if fault.field == "commitment" {
			if fault.priorActivation {
				fixture.priorActivation = fixture.activation
				fixture.priorActivation.EVMBlock--
				fixture.priorActivation.EVMHash[0] ^= 1
				priorHeader := fixture.header
				priorHeader.Domain, err = fixture.priorActivation.EvidenceDomain()
				if err != nil {
					t.Fatal(err)
				}
				priorSlot, err := priorHeader.SlotKey()
				if err != nil || priorSlot != slot || priorHeader.Domain.ActivationHash == domain.ActivationHash {
					t.Fatalf("earlier activation must occupy the same stable slot: %v", err)
				}
				commitment.Header = stabi.ValidatorEvidenceHeaderFromProtocol(priorHeader)
			}
			if fault.missing {
				commitment = stabi.STValidatorEvidenceCommitment{}
			} else if fault.dirtyAbsent {
				commitment.PublishedBlock = 0
			} else if fault.publication != 0 {
				commitment.PublishedBlock = fault.publication
			}
		}
	}
	responses := map[string][]byte{}
	type view struct {
		name     string
		calldata []byte
		value    any
	}
	views := []view{
		{name: "validatorEvidence", calldata: coordinator.PackValidatorEvidence(), value: fixture.journal},
		{name: "coordinator", calldata: contract.PackCoordinator(), value: common.Address(domain.Coordinator)},
		{name: "settlementVault", calldata: contract.PackSettlementVault(), value: common.Address(domain.SettlementVault)},
		{name: "chainId", calldata: contract.PackChainId(), value: domain.ChainID},
		{name: "netuid", calldata: contract.PackNetuid(), value: domain.Netuid},
		{name: "genesisHash", calldata: contract.PackGenesisHash(), value: domain.GenesisHash},
		{name: "deploymentIdHash", calldata: contract.PackDeploymentIdHash(), value: domain.DeploymentIDHash},
		{name: "activation", calldata: contract.PackActivation(domain.ActivationHash), value: activation},
		{name: "commitment", calldata: contract.PackCommitment(slot), value: commitment},
	}
	if fixture.priorActivation != (protocol.ValidatorEvidenceActivation{}) {
		priorDigest, err := fixture.priorActivation.Digest()
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, view{name: "activation", calldata: contract.PackActivation(priorDigest), value: stabi.STValidatorEvidenceActivation{
			Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(fixture.priorActivation), PublishedBlock: fixture.activation.EVMBlock,
		}})
	}
	for _, call := range views {
		target, parsed := fixture.journal, evidenceABI
		if call.name == "validatorEvidence" {
			target, parsed = common.Address(domain.Coordinator), coordinatorABI
		}
		encoded, err := parsed.Methods[call.name].Outputs.Pack(call.value)
		if err != nil {
			t.Fatal(err)
		}
		for _, fault := range faults {
			if call.name == fault.field {
				if fault.recordByte {
					encoded[fault.word*32+31] ^= 1
				}
				if fault.padding {
					encoded[fault.word*32] = 1
				}
				if fault.trailing {
					encoded = append(encoded, make([]byte, 32)...)
				}
			}
		}
		responses[target.Hex()+":"+hexutil.Encode(call.calldata)] = encoded
	}
	respond := func(request chainBatchRPCRequest) map[string]any {
		result := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		fail := func(message string) map[string]any {
			result["error"] = map[string]any{"code": -32000, "message": message}
			return result
		}
		switch request.Method {
		case "eth_chainId":
			result["result"] = "0x3b1"
		case "eth_getBlockByHash":
			var hash common.Hash
			if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &hash) != nil || hash != common.Hash(fixture.blockHash) {
				return fail("unexpected block lookup")
			}
			result["result"] = map[string]any{"number": hexutil.EncodeUint64(fixture.block), "hash": common.Hash(fixture.blockHash)}
		case "eth_getCode", "eth_call":
			var selector gethrpc.BlockNumberOrHash
			if len(request.Params) != 2 || json.Unmarshal(request.Params[1], &selector) != nil || selector.BlockHash == nil ||
				*selector.BlockHash != common.Hash(fixture.blockHash) || !selector.RequireCanonical || selector.BlockNumber != nil {
				return fail("not one canonical captured hash")
			}
			for _, fault := range faults {
				if fault.reorg {
					return fail("captured block is no longer canonical")
				}
				if fault.rpcError {
					return fail("evidence RPC unavailable")
				}
			}
			if request.Method == "eth_getCode" {
				fixture.codeCalls.Add(1)
				var target common.Address
				if json.Unmarshal(request.Params[0], &target) != nil || target != fixture.journal {
					return fail("wrong code target")
				}
				result["result"] = hexutil.Encode(code)
				return result
			}
			fixture.viewCalls.Add(1)
			var call struct {
				To    common.Address `json:"to"`
				Input hexutil.Bytes  `json:"input"`
			}
			if json.Unmarshal(request.Params[0], &call) != nil {
				return fail("malformed evidence call")
			}
			data, found := responses[call.To.Hex()+":"+hexutil.Encode(call.Input)]
			if !found {
				return fail("wrong target or complete evidence calldata")
			}
			result["result"] = hexutil.Encode(data)
		default:
			return fail("unexpected RPC method " + request.Method)
		}
		return result
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil || len(data) == 0 {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if data[0] == '[' {
			var requests []chainBatchRPCRequest
			if err := json.Unmarshal(data, &requests); err != nil {
				http.Error(w, "invalid batch", http.StatusBadRequest)
				return
			}
			batch := make([]map[string]any, len(requests))
			for index, request := range requests {
				batch[len(requests)-1-index] = respond(request)
			}
			_ = json.NewEncoder(w).Encode(batch)
		} else {
			var request chainBatchRPCRequest
			if err := json.Unmarshal(data, &request); err != nil {
				http.Error(w, "invalid call", http.StatusBadRequest)
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

// Exercise both production methods with the same immutable snapshot, including
// repeated reads needed by transaction recovery and another neutral relayer.
func TestChainEvidenceReadsExactCanonicalActivationAndCommitment(t *testing.T) {
	fixture := newEvidenceChainFixture(t, evidenceChainFault{})
	for index := 0; index < 2; index++ {
		activation, err := fixture.chain.ValidatorEvidenceActivationAtHashContext(t.Context(), fixture.journal, fixture.runtimeHash, fixture.activation, fixture.block, fixture.blockHash)
		if err != nil || activation.Record != fixture.activation || activation.PublishedBlock != 1001 {
			t.Fatalf("activation readback: %+v %v", activation, err)
		}
		commitment, err := fixture.readCommitment(t.Context())
		if err != nil || commitment.Header != fixture.header || commitment.PublishedBlock != 1101 {
			t.Fatalf("commitment readback: %+v %v", commitment, err)
		}
	}
	if fixture.codeCalls.Load() != 4 || fixture.viewCalls.Load() != 34 {
		t.Fatalf("unexpected code/view request counts: %d/%d", fixture.codeCalls.Load(), fixture.viewCalls.Load())
	}
}

// Invoke the actual public reader, not a fake decoder or acceptance flag.
func (self *evidenceChainFixture) readCommitment(ctx context.Context) (ValidatorEvidencePublication, error) {
	return self.chain.ValidatorEvidenceAtHashContext(ctx, self.journal, self.runtimeHash, self.activation, self.header, self.window, self.block, self.blockHash)
}

// Wrong code cannot progress to contract views, even if they would match.
func TestChainEvidenceRejectsRuntimeBeforeRecordReads(t *testing.T) {
	fixture := newEvidenceChainFixture(t, evidenceChainFault{runtime: true})
	if _, err := fixture.readCommitment(t.Context()); err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) {
		t.Fatalf("wrong runtime accepted or classified as absent: %v", err)
	}
	if fixture.codeCalls.Load() != 1 || fixture.viewCalls.Load() != 0 {
		t.Fatal("wrong runtime reached record views")
	}
}

// Check all seven independent anchors and reject alternative ABI encodings.
func TestChainEvidenceRejectsEveryAnchorAndImmutableMismatch(t *testing.T) {
	for _, field := range []string{"validatorEvidence", "coordinator", "settlementVault", "chainId", "netuid", "genesisHash", "deploymentIdHash"} {
		for _, fault := range []evidenceChainFault{{field: field, recordByte: true}, {field: field, trailing: true}} {
			fixture := newEvidenceChainFixture(t, fault)
			if _, err := fixture.readCommitment(t.Context()); err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) {
				t.Fatalf("%s wrong anchor accepted or treated as absence: %v", field, err)
			}
		}
	}
}

// Every static record word is independently changed without updating trusted
// expected authority; aliases in a conversion cannot hide that discrepancy.
func TestChainEvidenceRejectsEveryActivationAndCommitmentField(t *testing.T) {
	for field, count := range map[string]int{"activation": 17, "commitment": 21} {
		for word := 0; word < count; word++ {
			fixture := newEvidenceChainFixture(t, evidenceChainFault{field: field, word: word, recordByte: true})
			if _, err := fixture.readCommitment(t.Context()); err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) {
				t.Fatalf("%s word %d mismatch accepted or treated as absence: %v", field, word, err)
			}
		}
	}
}

// Tight tuple lengths and canonical integer padding cover adjacent decoder
// ambiguity rather than relying only on decoded-value equality.
func TestChainEvidenceRejectsNoncanonicalRecordABI(t *testing.T) {
	for _, field := range []string{"activation", "commitment"} {
		for _, fault := range []evidenceChainFault{{field: field, trailing: true}, {field: field, padding: true}} {
			fixture := newEvidenceChainFixture(t, fault)
			if _, err := fixture.readCommitment(t.Context()); err == nil {
				t.Fatalf("%s alternative tuple encoding accepted", field)
			}
		}
	}
}

// No numerical native/EVM clock comparison is invented: the happy fixture's
// native height is 9000 while these actual EVM inclusion bounds are near 1000.
func TestChainEvidenceRequiresEarlierActivationAndClosedPublication(t *testing.T) {
	for _, fault := range []evidenceChainFault{
		{field: "activation", publication: 1000},
		{field: "activation", publication: 1100},
		{field: "activation", publication: 1201},
		{field: "commitment", publication: 1099},
		{field: "commitment", publication: 1100},
		{field: "commitment", publication: 1201},
	} {
		fixture := newEvidenceChainFixture(t, fault)
		if _, err := fixture.readCommitment(t.Context()); err == nil {
			t.Fatalf("%s inclusion at %d accepted", fault.field, fault.publication)
		}
	}
}

// Only an actually empty mapping entry is retryable absence. RPC failure,
// reorg, malformed state or conflicting data must not authorize re-publication.
func TestChainEvidenceDistinguishesAbsenceFromFailures(t *testing.T) {
	for _, field := range []string{"activation", "commitment"} {
		fixture := newEvidenceChainFixture(t, evidenceChainFault{field: field, missing: true})
		_, err := fixture.readCommitment(t.Context())
		if field == "activation" {
			if err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) {
				t.Fatalf("populated commitment without activation became absence: %v", err)
			}
		} else if !errors.Is(err, ErrValidatorEvidenceAbsent) {
			t.Fatalf("%s missing record: %v", field, err)
		}
	}
	for _, fault := range []evidenceChainFault{{field: "activation", dirtyAbsent: true}, {field: "commitment", dirtyAbsent: true}, {rpcError: true}, {reorg: true}} {
		fixture := newEvidenceChainFixture(t, fault)
		if _, err := fixture.readCommitment(t.Context()); err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) {
			t.Fatalf("failure became successful/absent read: %+v %v", fault, err)
		}
	}
}

// A valid earlier activation/header is still readable at the real stable
// slot. An unpublished revision cannot classify that occupied slot as absent.
func TestChainEvidenceRejectsEarlierActivationInOccupiedStableSlot(t *testing.T) {
	fixture := newEvidenceChainFixture(t,
		evidenceChainFault{field: "activation", missing: true},
		evidenceChainFault{field: "commitment", priorActivation: true},
	)
	priorHeader := fixture.header
	var err error
	priorHeader.Domain, err = fixture.priorActivation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	priorSlot, err := priorHeader.SlotKey()
	if err != nil {
		t.Fatal(err)
	}
	expectedSlot, err := fixture.header.SlotKey()
	if err != nil || priorSlot != expectedSlot || priorHeader.Domain.ActivationHash == fixture.header.Domain.ActivationHash {
		t.Fatalf("activation revisions did not share one actual stable slot: %v", err)
	}
	publication, err := fixture.chain.ValidatorEvidenceAtHashContext(t.Context(), fixture.journal, fixture.runtimeHash, fixture.priorActivation, priorHeader, fixture.window, fixture.block, fixture.blockHash)
	if err != nil || publication.Header != priorHeader || publication.PublishedBlock != 1101 {
		t.Fatalf("coherent earlier activation/commitment could not be read: %v", err)
	}
	if activation, err := fixture.chain.ValidatorEvidenceActivationAtHashContext(t.Context(), fixture.journal, fixture.runtimeHash, fixture.activation, fixture.block, fixture.blockHash); !errors.Is(err, ErrValidatorEvidenceAbsent) || activation != (ValidatorEvidenceActivationPublication{}) {
		t.Fatalf("new activation digest was not actually absent: %+v %v", activation, err)
	}
	publication, err = fixture.readCommitment(t.Context())
	if err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) || publication != (ValidatorEvidencePublication{}) {
		t.Fatalf("earlier activation's occupied stable slot became absent: %+v %v", publication, err)
	}
}

// Both truly empty records and a missing commitment with a valid activation
// are retryable absence. The activation-only API retains its narrower meaning.
func TestChainEvidenceAbsenceRequiresConsistentEmptySlots(t *testing.T) {
	for _, activationMissing := range []bool{false, true} {
		faults := []evidenceChainFault{{field: "commitment", missing: true}}
		if activationMissing {
			faults = append(faults, evidenceChainFault{field: "activation", missing: true})
		}
		fixture := newEvidenceChainFixture(t, faults...)
		activation, activationErr := fixture.chain.ValidatorEvidenceActivationAtHashContext(t.Context(), fixture.journal, fixture.runtimeHash, fixture.activation, fixture.block, fixture.blockHash)
		if activationMissing {
			if !errors.Is(activationErr, ErrValidatorEvidenceAbsent) || activation != (ValidatorEvidenceActivationPublication{}) {
				t.Fatalf("empty activation-only lookup: %+v %v", activation, activationErr)
			}
		} else if activationErr != nil || activation.Record != fixture.activation || activation.PublishedBlock != 1001 {
			t.Fatalf("present activation-only lookup: %+v %v", activation, activationErr)
		}
		publication, err := fixture.readCommitment(t.Context())
		if !errors.Is(err, ErrValidatorEvidenceAbsent) || publication != (ValidatorEvidencePublication{}) {
			t.Fatalf("consistent empty slot activationMissing=%t: %+v %v", activationMissing, publication, err)
		}
	}
}

// A zero result on one side cannot mask malformed, dirty or conflicting data
// on the other, nor an RPC failure while acquiring the same canonical batch.
func TestChainEvidenceChecksBothTuplesBeforeAbsence(t *testing.T) {
	for _, fixtureCase := range []struct {
		name   string
		faults []evidenceChainFault
	}{
		{name: "missing activation dirty commitment", faults: []evidenceChainFault{{field: "activation", missing: true}, {field: "commitment", dirtyAbsent: true}}},
		{name: "missing activation trailing commitment", faults: []evidenceChainFault{{field: "activation", missing: true}, {field: "commitment", trailing: true}}},
		{name: "missing activation padded commitment", faults: []evidenceChainFault{{field: "activation", missing: true}, {field: "commitment", padding: true}}},
		{name: "missing activation conflicting commitment", faults: []evidenceChainFault{{field: "activation", missing: true}, {field: "commitment", word: 19, recordByte: true}}},
		{name: "missing activation future commitment", faults: []evidenceChainFault{{field: "activation", missing: true}, {field: "commitment", publication: 1201}}},
		{name: "dirty activation missing commitment", faults: []evidenceChainFault{{field: "activation", dirtyAbsent: true}, {field: "commitment", missing: true}}},
		{name: "trailing activation missing commitment", faults: []evidenceChainFault{{field: "activation", trailing: true}, {field: "commitment", missing: true}}},
		{name: "padded activation missing commitment", faults: []evidenceChainFault{{field: "activation", padding: true}, {field: "commitment", missing: true}}},
		{name: "conflicting activation missing commitment", faults: []evidenceChainFault{{field: "activation", word: 11, recordByte: true}, {field: "commitment", missing: true}}},
		{name: "RPC failure with both missing", faults: []evidenceChainFault{{field: "activation", missing: true}, {field: "commitment", missing: true}, {rpcError: true}}},
		{name: "reorg with both missing", faults: []evidenceChainFault{{field: "activation", missing: true}, {field: "commitment", missing: true}, {reorg: true}}},
	} {
		fixture := newEvidenceChainFixture(t, fixtureCase.faults...)
		publication, err := fixture.readCommitment(t.Context())
		if err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) || publication != (ValidatorEvidencePublication{}) {
			t.Fatalf("%s became successful/absent evidence: %+v %v", fixtureCase.name, publication, err)
		}
	}
}

// Admission fails synchronously before network work for canceled ownership or
// a changed independent activation/observation, without timing-based checks.
func TestChainEvidenceRejectsCanceledAndForeignAuthorityBeforeRPC(t *testing.T) {
	fixture := newEvidenceChainFixture(t, evidenceChainFault{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.readCommitment(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled evidence read: %v", err)
	}
	for _, edit := range []func(*protocol.ValidatorEvidenceActivation, *protocol.ValidatorEvidenceWindow){
		func(record *protocol.ValidatorEvidenceActivation, window *protocol.ValidatorEvidenceWindow) {
			record.NoID++
		},
		func(record *protocol.ValidatorEvidenceActivation, window *protocol.ValidatorEvidenceWindow) {
			record.PriorRoot[0]++
		},
		func(record *protocol.ValidatorEvidenceActivation, window *protocol.ValidatorEvidenceWindow) {
			window.FinalizedBlock++
		},
	} {
		activation, window := fixture.activation, fixture.window
		edit(&activation, &window)
		if _, err := fixture.chain.ValidatorEvidenceAtHashContext(t.Context(), fixture.journal, fixture.runtimeHash, activation, fixture.header, window, fixture.block, fixture.blockHash); err == nil {
			t.Fatal("foreign independent authority accepted")
		}
	}
	if fixture.codeCalls.Load() != 0 || fixture.viewCalls.Load() != 0 {
		t.Fatalf("refused request reached network: %d/%d", fixture.codeCalls.Load(), fixture.viewCalls.Load())
	}
}

// A shortened or padded record cannot be mistaken for a zero mapping entry.
func TestChainEvidenceActivationDecoderRejectsAdjacentEmptyEncodings(t *testing.T) {
	fixture := newEvidenceChainFixture(t, evidenceChainFault{})
	for _, size := range []int{0, 17 * 32, 18*32 + 1, 19 * 32} {
		if _, err := decodeValidatorEvidenceActivation(stabi.NewSTValidatorEvidence(), make([]byte, size), fixture.activation, fixture.block); err == nil || errors.Is(err, ErrValidatorEvidenceAbsent) {
			t.Fatalf("%d-byte activation treated as valid/absent: %v", size, err)
		}
	}
	for _, method := range []string{"activation", "commitment"} {
		var data []byte
		var value any
		if method == "activation" {
			data, value = make([]byte, 18*32), stabi.STValidatorEvidenceActivation{}
		} else {
			data, value = make([]byte, 22*32), stabi.STValidatorEvidenceCommitment{}
		}
		if err := validatorEvidenceCanonicalOutput(method, data, value); err != nil {
			t.Fatalf("canonical empty %s: %v", method, err)
		}
		changed := bytes.Clone(data)
		changed[0] = 1
		if err := validatorEvidenceCanonicalOutput(method, changed, value); err == nil {
			t.Fatalf("%s padding drift accepted", method)
		}
	}
}
