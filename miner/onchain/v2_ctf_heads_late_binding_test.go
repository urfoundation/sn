package onchain

import (
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

var v2LateBindingCoordinator = common.HexToAddress("0xC18925925E2B7bb9059b7d696b8c92762AE86406")

// One binding version: the block it landed in and its signed validity.
type v2LateBindingVersion struct {
	landed, generation, validFrom, validTo uint64
}

// v2LateBindingChain answers the coordinator's bindingAt at any block number
// as STCoordinator does, and epoch 8's boundaries.
type v2LateBindingChain struct {
	versions map[[16]byte][]v2LateBindingVersion
}

func (self *v2LateBindingChain) Call(ctx context.Context, call map[string]hexutil.Bytes, block gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	number, ok := block.Number()
	if !ok || number < 0 || common.BytesToAddress(call["to"]) != v2LateBindingCoordinator {
		return nil, errors.New("v2 late binding test requires a block-number call to the coordinator")
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
	case "epochStartBlock":
		return method.Outputs.Pack(big.NewInt(9_600_028))
	case "epochEndBlock":
		return method.Outputs.Pack(big.NewInt(9_650_428))
	case "bindingAt":
		client, epoch := arguments[0].([16]byte), arguments[1].(*big.Int).Uint64()
		versions := self.versions[client]
		for i := len(versions) - 1; 0 <= i; i-- {
			version := versions[i]
			if uint64(number) < version.landed || epoch < version.validFrom {
				continue
			}
			record := stabi.STCoordinatorBindingRecord{FleetId: [32]byte{0xf1}, Hotkey: [32]byte{0xf2}, ClientKey: [32]byte{0xf3}, CommitmentHash: [32]byte{0xf4}, Generation: version.generation, ValidFromEpoch: version.validFrom, ValidToEpoch: version.validTo, Uid: 9}
			return method.Outputs.Pack(epoch <= version.validTo, record)
		}
		return method.Outputs.Pack(false, stabi.STCoordinatorBindingRecord{})
	}
	return nil, errors.New("v2 late binding test answers no " + method.Name)
}

// snclaim's head oracle counts a binding signed from epoch 9 that landed in
// the epoch 8 late-binding window as bound for epoch 8, as the validator's
// does; one from before epoch 8 or past the deadline is not a candidate.
func TestV2CtfHeadsEpoch8LateBinding(t *testing.T) {
	late, before, after, normal := [16]byte{0x01}, [16]byte{0x02}, [16]byte{0x03}, [16]byte{0x04}
	fake := &v2LateBindingChain{versions: map[[16]byte][]v2LateBindingVersion{
		late:   {{landed: 9_600_029, generation: 1, validFrom: 9, validTo: 20}},
		before: {{landed: 9_600_027, generation: 1, validFrom: 9, validTo: 20}},
		after:  {{landed: 9_615_061, generation: 1, validFrom: 9, validTo: 20}},
		normal: {{landed: 9_560_000, generation: 1, validFrom: 8, validTo: 19}},
	}}
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
	heads := newV2CtfHeads(&v2Reader{client: client, block: big.NewInt(9_650_500)}, common.Address{}, 964)
	heads.scanned, heads.coordinator = true, v2LateBindingCoordinator
	for client, versions := range fake.versions {
		for _, version := range versions {
			heads.bindings = append(heads.bindings, v2FleetBinding{client: client, validFrom: version.validFrom, validTo: version.validTo, block: version.landed})
		}
	}
	bound, err := heads.HeadBindings(t.Context(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(bound) != 2 || !bound[late] || !bound[normal] || bound[before] || bound[after] {
		t.Fatalf("epoch 8's head bindings: %v", bound)
	}
	// on another chain the window does not apply
	other := newV2CtfHeads(&v2Reader{client: client, block: big.NewInt(9_650_500)}, common.Address{}, 945)
	other.scanned, other.coordinator, other.bindings = true, v2LateBindingCoordinator, heads.bindings
	if bound, err := other.HeadBindings(t.Context(), 8); err != nil || len(bound) != 1 || !bound[normal] {
		t.Fatalf("epoch 8's head bindings on another chain: %v %v", bound, err)
	}
}
