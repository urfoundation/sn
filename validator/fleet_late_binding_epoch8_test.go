//go:build linux || darwin

package validator

import (
	"context"
	"encoding/binary"
	"errors"
	"math/big"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/stabi"
)

// The SN25 mainnet coordinator and chain, whose epoch 8 has the late-binding
// window. These are literals, not the protocol constants, so the tests read
// the same against the code from before the window.
var (
	lateBindingTestCoordinator = common.HexToAddress("0xC18925925E2B7bb9059b7d696b8c92762AE86406")
	lateBindingTestChainID     = big.NewInt(964)
)

// One binding version of the simulated coordinator: the block it landed in
// and its signed validity.
type lateBindingTestVersion struct {
	landed, generation, validFrom, validTo uint64
}

// lateBindingTestChain answers bindingAt(client, epoch) at any block as
// STCoordinator does, with FleetBound logs for every version, block headers
// for every height, and epoch 8's boundaries.
type lateBindingTestChain struct {
	lock      sync.Mutex
	versions  map[[16]byte][]lateBindingTestVersion
	finalized uint64
}

func lateBindingTestHash(block uint64) common.Hash {
	var hash common.Hash
	hash[0] = 0xb1
	binary.BigEndian.PutUint64(hash[24:], block)
	return hash
}

func lateBindingTestBlockOf(hash common.Hash) (uint64, bool) {
	return binary.BigEndian.Uint64(hash[24:]), hash[0] == 0xb1 && hash == lateBindingTestHash(binary.BigEndian.Uint64(hash[24:]))
}

func newLateBindingTestChain(t *testing.T, finalized uint64, versions map[[16]byte][]lateBindingTestVersion) (*lateBindingTestChain, *ChainClient) {
	t.Helper()
	fake := &lateBindingTestChain{versions: versions, finalized: finalized}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", fake); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	client, err := ethclient.Dial(httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); httpServer.Close(); server.Stop() })
	return fake, &ChainClient{client: client, coordinator: stabi.NewSTCoordinator(), contractAddr: lateBindingTestCoordinator, chainId: new(big.Int).Set(lateBindingTestChainID), release: true}
}

func (self *lateBindingTestChain) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	hash, ok := selector.Hash()
	if !ok {
		return nil, errors.New("late binding test requires a hash-pinned call")
	}
	block, ok := lateBindingTestBlockOf(hash)
	if !ok || block > self.finalized || common.BytesToAddress(call["to"]) != lateBindingTestCoordinator {
		return nil, errors.New("late binding test call is not at a known block of the coordinator")
	}
	input := call["input"]
	if len(input) == 0 {
		input = call["data"]
	}
	contractABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	method, err := contractABI.MethodById(input)
	if err != nil {
		return nil, err
	}
	arguments, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	switch method.Name {
	case "epochStartBlock", "epochEndBlock":
		if arguments[0].(*big.Int).Uint64() != 8 {
			return nil, errors.New("late binding test knows epoch 8's boundaries only")
		}
		if method.Name == "epochStartBlock" {
			return method.Outputs.Pack(big.NewInt(9_600_028))
		}
		return method.Outputs.Pack(big.NewInt(9_650_428))
	case "bindingAt":
		active, record := self.bindingAt(arguments[0].([16]byte), arguments[1].(*big.Int).Uint64(), block)
		return method.Outputs.Pack(active, record)
	}
	return nil, errors.New("late binding test answers no " + method.Name)
}

// bindingAt is STCoordinator.bindingAt at block: the newest version landed by
// then with validFrom at most epoch, active within its validity.
func (self *lateBindingTestChain) bindingAt(client [16]byte, epoch, block uint64) (bool, stabi.STCoordinatorBindingRecord) {
	self.lock.Lock()
	defer self.lock.Unlock()
	versions := self.versions[client]
	for i := len(versions) - 1; 0 <= i; i-- {
		version := versions[i]
		if block < version.landed || epoch < version.validFrom {
			continue
		}
		return epoch <= version.validTo, stabi.STCoordinatorBindingRecord{FleetId: [32]byte{0xf1}, Hotkey: [32]byte{0xf2}, ClientKey: [32]byte{0xf3}, CommitmentHash: [32]byte{0xf4}, Generation: version.generation, ValidFromEpoch: version.validFrom, ValidToEpoch: version.validTo, Uid: 9}
	}
	return false, stabi.STCoordinatorBindingRecord{}
}

func (self *lateBindingTestChain) GetBlockByNumber(ctx context.Context, number gethrpc.BlockNumber, full bool) (map[string]any, error) {
	block := uint64(number)
	if number == gethrpc.FinalizedBlockNumber || number == gethrpc.LatestBlockNumber {
		block = self.finalized
	} else if number < 0 || block > self.finalized {
		return nil, nil
	}
	return map[string]any{"number": hexutil.EncodeUint64(block), "hash": lateBindingTestHash(block)}, nil
}

func (self *lateBindingTestChain) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (map[string]any, error) {
	block, ok := lateBindingTestBlockOf(hash)
	if !ok || block > self.finalized {
		return nil, errors.New("late binding test has no such block")
	}
	return map[string]any{"number": hexutil.EncodeUint64(block), "hash": hash}, nil
}

// The eth_getLogs filter ethclient sends.
type lateBindingTestLogQuery struct {
	FromBlock *hexutil.Big     `json:"fromBlock"`
	ToBlock   *hexutil.Big     `json:"toBlock"`
	Address   []common.Address `json:"address"`
	Topics    [][]common.Hash  `json:"topics"`
}

// GetLogs returns a FleetBound log for every version landed in the range.
func (self *lateBindingTestChain) GetLogs(query lateBindingTestLogQuery) ([]types.Log, error) {
	from, to := query.FromBlock.ToInt().Uint64(), query.ToBlock.ToInt().Uint64()
	event := settlementV2CoordinatorABI.Events["FleetBound"]
	self.lock.Lock()
	defer self.lock.Unlock()
	logs := []types.Log{}
	for client, versions := range self.versions {
		for _, version := range versions {
			if version.landed < from || to < version.landed || !slices.Contains(query.Address, lateBindingTestCoordinator) {
				continue
			}
			topics, err := abi.MakeTopics([]any{client}, []any{[32]byte{0xf1}}, []any{[32]byte{0xf2}})
			if err != nil {
				return nil, err
			}
			data, err := event.Inputs.NonIndexed().Pack(uint16(9), version.generation, version.validFrom, version.validTo)
			if err != nil {
				return nil, err
			}
			logs = append(logs, types.Log{Address: lateBindingTestCoordinator, Topics: []common.Hash{event.ID, topics[0][0], topics[1][0], topics[2][0]}, Data: data, BlockNumber: version.landed, BlockHash: lateBindingTestHash(version.landed), TxHash: common.Hash{0x7a, byte(version.generation), client[0]}})
		}
	}
	slices.SortFunc(logs, func(a, b types.Log) int { return int(a.BlockNumber) - int(b.BlockNumber) })
	for index := range logs {
		logs[index].Index = uint(index)
	}
	return logs, nil
}

var (
	lateBindingTestLate   = [16]byte{0x01, 0x0a}
	lateBindingTestLast   = [16]byte{0x02, 0x0b}
	lateBindingTestBefore = [16]byte{0x03, 0x0c}
	lateBindingTestAfter  = [16]byte{0x04, 0x0d}
	lateBindingTestNormal = [16]byte{0x05, 0x0e}
)

// Bindings landing at 9,600,029 and 9,615,060 are in the window; one landing
// at 9,600,027 is before epoch 8 and one at 9,615,061 is past the deadline,
// both signed from epoch 9; one bound in epoch 7 from epoch 8 is ordinary.
func lateBindingTestVersions() map[[16]byte][]lateBindingTestVersion {
	return map[[16]byte][]lateBindingTestVersion{
		lateBindingTestLate:   {{landed: 9_600_029, generation: 1, validFrom: 9, validTo: 20}},
		lateBindingTestLast:   {{landed: 9_615_060, generation: 1, validFrom: 9, validTo: 20}},
		lateBindingTestBefore: {{landed: 9_600_027, generation: 1, validFrom: 9, validTo: 20}},
		lateBindingTestAfter:  {{landed: 9_615_061, generation: 1, validFrom: 9, validTo: 20}},
		lateBindingTestNormal: {{landed: 9_560_000, generation: 1, validFrom: 8, validTo: 19}},
	}
}

func lateBindingTestClients() [][16]byte {
	return [][16]byte{lateBindingTestLate, lateBindingTestLast, lateBindingTestBefore, lateBindingTestAfter, lateBindingTestNormal}
}

// The head census counts a window binding for epoch 8 from the block it
// lands in, and only a window binding: the trail credits it, the census
// keeps it as eligible, and the observer's own check admits it.
func TestEpoch8LateBindingHeadCensus(t *testing.T) {
	_, chain := newLateBindingTestChain(t, 9_650_500, lateBindingTestVersions())
	ids := lateBindingTestClients()
	for _, c := range []struct {
		block uint64
		bound []bool
	}{
		{9_600_028, []bool{false, false, false, false, true}},
		{9_600_029, []bool{true, false, false, false, true}},
		{9_615_060, []bool{true, true, false, false, true}},
		{9_615_100, []bool{true, true, false, false, true}},
		{9_650_427, []bool{true, true, false, false, true}},
	} {
		bindings, err := chain.readReleaseProviderBindingsV2Context(t.Context(), c.block, lateBindingTestHash(c.block), ids, 8, 8)
		if err != nil {
			t.Fatalf("at block %d: %v", c.block, err)
		}
		for index, binding := range bindings {
			if binding.Active != c.bound[index] {
				t.Fatalf("at block %d client %x: active %t, want %t (%+v)", c.block, ids[index][:2], binding.Active, c.bound[index], binding.Record)
			}
			if binding.Active && (binding.Record.ValidFromEpoch > 8 || binding.Record.ValidToEpoch < 8) {
				t.Fatalf("at block %d client %x: an active record outside epoch 8: %+v", c.block, ids[index][:2], binding.Record)
			}
		}
	}
}

// Epoch 9's reads are the coordinator's own: the window bindings count from
// epoch 9 as signed, and nothing else changes.
func TestEpoch8LateBindingLeavesEpoch9(t *testing.T) {
	_, chain := newLateBindingTestChain(t, 9_650_500, lateBindingTestVersions())
	bindings, err := chain.readReleaseProviderBindingsV2Context(t.Context(), 9_650_428, lateBindingTestHash(9_650_428), lateBindingTestClients(), 9, 8)
	if err != nil {
		t.Fatal(err)
	}
	for index, binding := range bindings {
		if !binding.Active || binding.Record.ValidFromEpoch != []uint64{9, 9, 9, 9, 8}[index] {
			t.Fatalf("client %d at epoch 9: %+v", index, binding)
		}
	}
}

// A trail hop records the window binding as the census does, so its work earns
// head credit for the fleet.
func TestEpoch8LateBindingAttemptBinding(t *testing.T) {
	_, chain := newLateBindingTestChain(t, 9_650_500, lateBindingTestVersions())
	rpc := &chainAttemptBoundaryRPC{chain: chain, netuid: 25}
	boundary := AttemptBoundary{SettlementEpoch: 8, EVMBlock: 9_600_100, EVMBlockHash: attemptHex32(lateBindingTestHash(9_600_100))}
	resolver := newCachedAttemptBoundaryResolverWithLifecycle(t.Context(), rpc, releaseNativeEndpointTimeout(nil))
	defer resolver.close()
	block := &attemptBoundaryBlock{boundary: boundary, hotkeys: map[[32]byte]uint16{{0xf2}: 9}, bindings: map[connect.Id]AttemptBinding{}}
	late, err := resolver.binding(t.Context(), block, connect.Id(lateBindingTestLate))
	if err != nil {
		t.Fatal(err)
	}
	if !late.Active || !late.UIDFound || late.UID != 9 || late.Generation != 1 || late.FleetID != attemptHex32([32]byte{0xf1}) {
		t.Fatalf("the window binding's hop: %+v", late)
	}
	before, err := resolver.binding(t.Context(), block, connect.Id(lateBindingTestBefore))
	if err != nil || before.Active {
		t.Fatalf("a binding from before epoch 8's hop: %+v %v", before, err)
	}
}

// The settlement audit's head oracle counts a window binding as bound for
// epoch 8, as the network operator's ledger marks it head-excluded, and only
// a window binding.
func TestEpoch8LateBindingAuditHeadBindings(t *testing.T) {
	_, chain := newLateBindingTestChain(t, 9_650_500, lateBindingTestVersions())
	reader := &settlementV2AuditChain{chain: chain, deployBlock: 9_550_001}
	oracle := reader.headOracle(&settlementV2PoolState{block: 9_650_500, blockHash: lateBindingTestHash(9_650_500)})
	bound, err := oracle.HeadBindings(t.Context(), 8)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[16]byte]bool{lateBindingTestLate: true, lateBindingTestLast: true, lateBindingTestNormal: true}
	for _, client := range lateBindingTestClients() {
		if bound[client] != want[client] {
			t.Fatalf("client %x: bound %t, want %t (%v)", client[:2], bound[client], want[client], bound)
		}
	}
}

// History recorded by a validator from before the window still matches: a
// decision row equal to the coordinator's own answer is accepted, as is one
// equal to the window's answer, and nothing else.
func TestEpoch8LateBindingRecordedBinding(t *testing.T) {
	windowed := ReleaseBindingMeasurement{NoID: 1, ClientID: "a", Active: true, Generation: 1, ValidFromEpoch: 8, ValidToEpoch: 20}
	coordinator := ReleaseBindingMeasurement{NoID: 1, ClientID: "a"}
	observed := &releaseDecisionChainV2Observation{bindings: []ReleaseBindingMeasurement{windowed}, coordinatorBindings: []ReleaseBindingMeasurement{coordinator}}
	if row, found := observed.recordedBinding(0, windowed); !found || row != windowed {
		t.Fatal("the window's row is not accepted")
	}
	if row, found := observed.recordedBinding(0, coordinator); !found || row != coordinator {
		t.Fatal("the coordinator's own row is not accepted")
	}
	other := windowed
	other.Generation = 2
	if _, found := observed.recordedBinding(0, other); found {
		t.Fatal("a row neither answer gave is accepted")
	}
	if _, found := observed.recordedBinding(1, windowed); found {
		t.Fatal("a row outside the census is accepted")
	}
}
