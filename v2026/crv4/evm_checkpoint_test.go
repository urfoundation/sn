package crv4

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func newEVMCheckpointTestFixture(t *testing.T) (*validatorIdentityTestFixture, EVMCheckpointQuery, *json.RawMessage) {
	t.Helper()
	f := newValidatorIdentityTestFixture(t)
	f.metadata.AsMetadataV14.Pallets = append(f.metadata.AsMetadataV14.Pallets, types.PalletMetadataV14{Name: "Ethereum", HasStorage: true, Storage: types.StorageMetadataV14{Prefix: "Ethereum", Items: []types.StorageEntryMetadataV14{{Name: "BlockHash", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsTwox64Concat: true}}}}}}}})
	f.publishMetadata(t)
	parentHeader, parent := receiptTestHeader(t, types.Hash{5}, 99, nil, 1)
	header, block := receiptTestHeader(t, parent, 100, nil, 1)
	delete(f.headers, f.query.BlockHash.Hex())
	f.query.BlockHash = block
	f.blockHashes[100] = block
	f.headers[block.Hex()] = header
	f.headers[parent.Hex()] = parentHeader
	f.blockHashes[99] = parent
	query := EVMCheckpointQuery{GenesisHash: f.query.GenesisHash, NativeHash: block, NativeNumber: 100, EVMHash: types.Hash{77}, EVMNumber: 70}
	arg := make([]byte, 32)
	binary.LittleEndian.PutUint64(arg, 70)
	key, err := types.CreateStorageKey(f.metadata, "Ethereum", "BlockHash", arg)
	if err != nil {
		t.Fatal(err)
	}
	f.keyNames[key.Hex()] = "Ethereum.BlockHash"
	f.storage["Ethereum.BlockHash"] = validatorIdentityTestHex(query.EVMHash[:])
	parentValue := json.RawMessage("null")
	f.hook = func(ctx context.Context, out any, method string, args ...any) (bool, error) {
		if len(args) == 0 || args[len(args)-1] != parent.Hex() {
			return false, nil
		}
		switch method {
		case "state_getRuntimeVersion":
			return true, setRuntimeIdentityTestResult(out, f.version)
		case "state_getStorageHash":
			return true, setRuntimeIdentityTestResult(out, f.codeHash)
		case "state_getMetadata":
			return true, setRuntimeIdentityTestResult(out, f.metadataHex)
		case "state_getStorage":
			if len(args) != 2 || args[0] != key.Hex() {
				return true, errors.New("parent mapping query changed key")
			}
			*(out.(*json.RawMessage)) = append(json.RawMessage(nil), parentValue...)
			return true, nil
		}
		return false, nil
	}
	return f, query, &parentValue
}

func TestEVMCheckpointBindsFirstInsertionAcrossIndependentClocks(t *testing.T) {
	f, query, parent := newEVMCheckpointTestFixture(t)
	observation, err := ReadEVMCheckpointAtContext(f.ctx, f.chain, query, f.allowed...)
	if err != nil || observation.Query != query || observation.NativeParentHash != f.headers[query.NativeHash.Hex()].ParentHash || observation.Runtime != f.allowed[0] || observation.ParentRuntime != f.allowed[0] {
		t.Fatalf("exact native100/EVM70 bridge: %+v: %v", observation, err)
	}
	*parent = validatorIdentityTestHex(make([]byte, 32))
	if _, err := ReadEVMCheckpointAtContext(f.ctx, f.chain, query, f.allowed...); err != nil {
		t.Fatalf("explicit ValueQuery zero parent: %v", err)
	}
	if !reflect.DeepEqual(f.storageCalls, []string{"Ethereum.BlockHash", "Ethereum.BlockHash", "Ethereum.BlockHash", "Ethereum.BlockHash"}) {
		t.Fatalf("wrong mapping read census: %v", f.storageCalls)
	}
	if f.chain.Runtime.SpecVersion != 999 {
		t.Fatal("mapping changed original dial-time runtime")
	}
}

func TestEVMCheckpointRejectsHistoricalPresenceReorgAndCancellation(t *testing.T) {
	for _, name := range []string{"parent already present", "wrong current hash", "numeric height assumption", "parent reorg", "cancellation"} {
		t.Run(name, func(t *testing.T) {
			f, query, parent := newEVMCheckpointTestFixture(t)
			switch name {
			case "parent already present":
				*parent = validatorIdentityTestHex(query.EVMHash[:])
			case "wrong current hash":
				f.storage["Ethereum.BlockHash"] = validatorIdentityTestHex(make([]byte, 32))
			case "numeric height assumption":
				query.EVMNumber = query.NativeNumber
			case "parent reorg":
				f.blockHashes[99] = types.Hash{9}
			case "cancellation":
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f.ctx = ctx
				f.after = func(method string, args ...any) {
					if method == "state_getStorage" {
						cancel()
					}
				}
			}
			got, err := ReadEVMCheckpointAtContext(f.ctx, f.chain, query, f.allowed...)
			if err == nil || got != (EVMCheckpointObservation{}) {
				t.Fatalf("accepted partial/changed checkpoint: %+v: %v", got, err)
			}
		})
	}
}
