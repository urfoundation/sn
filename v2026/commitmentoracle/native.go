package commitmentoracle

// Finalized native reads through the sn crv4 Substrate client. Every storage
// read names an explicit block hash; nothing reads "latest". Methods are not
// safe for concurrent use.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

// A finalized native head.
type nativeHead struct {
	number uint64
	hash   [32]byte
}

// Finalized-state reads of the subtensor chain. Implementations return the
// state at the hash they are given; callers only ever pass the finalized head.
type nativeReader interface {
	// chain_getFinalizedHead and its header number.
	finalizedHead(ctx context.Context) (nativeHead, error)
	// Every CommitmentOf(netuid, *) entry at blockHash.
	commitments(ctx context.Context, netuid uint16, blockHash [32]byte) ([]rawCommitment, error)
	// CommitmentOf(netuid, hotkey) at blockHash, nil when absent.
	commitment(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) ([]byte, error)
	// LastCommitment(netuid, hotkey) at blockHash, nil when absent.
	lastCommitment(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) ([]byte, error)
	// chain_getBlockHash(number): the canonical hash at that height.
	blockHash(ctx context.Context, number uint64) ([32]byte, error)
	// The number in the header of blockHash.
	headerNumber(ctx context.Context, blockHash [32]byte) (uint64, error)
	// The hotkey's registration and validator permit on netuid at blockHash.
	role(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) (neuronRole, error)
	// The extrinsics of the block at blockHash, authenticated against it.
	blockExtrinsics(ctx context.Context, blockHash [32]byte) ([][]byte, error)
	// The SCALE call Commitments.set_commitment(netuid, [Sha256(hash)]).
	fleetCommitmentCall(netuid uint16, hash [32]byte) ([]byte, error)
	close()
}

// Keys per state_getKeysPaged page and per state_queryStorageAt batch.
const (
	nativeKeysPage    = 512
	nativeValuesBatch = 64
)

// The crv4-backed reader.
type substrateReader struct {
	endpoint string
	chain    *crv4.Chain
}

func dialSubstrateReader(ctx context.Context, endpoint string) (*substrateReader, error) {
	chain, err := crv4.DialChainContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("dial %s failed", endpointLabel(endpoint))
	}
	return &substrateReader{endpoint: endpointLabel(endpoint), chain: chain}, nil
}

func (self *substrateReader) close() {
	if self.chain != nil && self.chain.API != nil && self.chain.API.Client != nil {
		self.chain.API.Client.Close()
	}
}

func (self *substrateReader) call(ctx context.Context, result any, method string, args ...any) error {
	if err := self.chain.API.Client.CallContext(ctx, result, method, args...); err != nil {
		return fmt.Errorf("%s %s: %w", self.endpoint, method, err)
	}
	return nil
}

func (self *substrateReader) finalizedHead(ctx context.Context) (nativeHead, error) {
	hash, err := crv4.FinalizedHeadContext(ctx, self.chain)
	if err != nil {
		return nativeHead{}, err
	}
	number, err := self.headerNumber(ctx, hash)
	if err != nil {
		return nativeHead{}, err
	}
	return nativeHead{number: number, hash: hash}, nil
}

func (self *substrateReader) headerNumber(ctx context.Context, blockHash [32]byte) (uint64, error) {
	header, err := self.chain.HeaderAtContext(ctx, types.Hash(blockHash))
	if err != nil {
		return 0, err
	}
	return uint64(header.Number), nil
}

func (self *substrateReader) blockHash(ctx context.Context, number uint64) ([32]byte, error) {
	var encoded *string
	if err := self.call(ctx, &encoded, "chain_getBlockHash", number); err != nil {
		return [32]byte{}, err
	}
	if encoded == nil {
		return [32]byte{}, fmt.Errorf("%s has no block %d", self.endpoint, number)
	}
	hash, err := types.NewHashFromHexString(*encoded)
	if err != nil {
		return [32]byte{}, err
	}
	return [32]byte(hash), nil
}

func (self *substrateReader) storageKey(item string, netuid uint16, hotkey [32]byte) (types.StorageKey, error) {
	return types.CreateStorageKey(self.chain.Meta, crv4.CommitmentsPalletName, item, encodeNetuid(netuid), hotkey[:])
}

func (self *substrateReader) storage(ctx context.Context, key types.StorageKey, blockHash [32]byte) ([]byte, error) {
	var encoded *string
	if err := self.call(ctx, &encoded, "state_getStorage", key.Hex(), types.Hash(blockHash).Hex()); err != nil {
		return nil, err
	}
	if encoded == nil {
		return nil, nil
	}
	return codec.HexDecodeString(*encoded)
}

func (self *substrateReader) commitment(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) ([]byte, error) {
	key, err := self.storageKey("CommitmentOf", netuid, hotkey)
	if err != nil {
		return nil, err
	}
	return self.storage(ctx, key, blockHash)
}

func (self *substrateReader) subtensorStorage(ctx context.Context, item string, blockHash [32]byte, args ...[]byte) ([]byte, error) {
	key, err := types.CreateStorageKey(self.chain.Meta, crv4.PalletName, item, args...)
	if err != nil {
		return nil, err
	}
	return self.storage(ctx, key, blockHash)
}

func (self *substrateReader) role(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) (neuronRole, error) {
	subnetworkN, err := self.subtensorStorage(ctx, "SubnetworkN", blockHash, encodeNetuid(netuid))
	if err != nil {
		return neuronRole{}, err
	}
	uid, err := self.subtensorStorage(ctx, "Uids", blockHash, encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return neuronRole{}, err
	}
	permits, err := self.subtensorStorage(ctx, "ValidatorPermit", blockHash, encodeNetuid(netuid))
	if err != nil {
		return neuronRole{}, err
	}
	keyOfUid := func(uid uint16) ([]byte, error) {
		return self.subtensorStorage(ctx, "Keys", blockHash, encodeNetuid(netuid), encodeNetuid(uid))
	}
	role, err := decodeNeuronRole(hotkey, subnetworkN, uid, keyOfUid, permits)
	if err != nil {
		return neuronRole{}, fmt.Errorf("%s role of 0x%x: %w", self.endpoint, hotkey, err)
	}
	return role, nil
}

func (self *substrateReader) blockExtrinsics(ctx context.Context, blockHash [32]byte) ([][]byte, error) {
	_, extrinsics, err := self.chain.BlockExtrinsicsAtContext(ctx, types.Hash(blockHash))
	if err != nil {
		return nil, fmt.Errorf("%s block 0x%x: %w", self.endpoint, blockHash, err)
	}
	return extrinsics, nil
}

func (self *substrateReader) fleetCommitmentCall(netuid uint16, hash [32]byte) ([]byte, error) {
	call, err := self.chain.NewSetFleetCommitmentCall(netuid, hash)
	if err != nil {
		return nil, err
	}
	return codec.Encode(call)
}

func (self *substrateReader) lastCommitment(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) ([]byte, error) {
	key, err := self.storageKey("LastCommitment", netuid, hotkey)
	if err != nil {
		return nil, err
	}
	return self.storage(ctx, key, blockHash)
}

// Lists CommitmentOf(netuid, *) with state_getKeysPaged under the netuid's
// prefix and reads the values with state_queryStorageAt, both at blockHash.
// The hotkey is the key's trailing 32 bytes, and each key must equal the key
// the metadata builds for (netuid, hotkey), so a non-concat hasher or a
// foreign key fails closed instead of yielding a wrong hotkey.
func (self *substrateReader) commitments(ctx context.Context, netuid uint16, blockHash [32]byte) ([]rawCommitment, error) {
	prefix, err := self.commitmentPrefix(netuid)
	if err != nil {
		return nil, err
	}
	at := types.Hash(blockHash).Hex()
	prefixHex := codec.HexEncodeToString(prefix)
	keyHexs := []string{}
	for {
		args := []any{prefixHex, nativeKeysPage}
		if len(keyHexs) == 0 {
			args = append(args, nil, at)
		} else {
			args = append(args, keyHexs[len(keyHexs)-1], at)
		}
		var page []string
		if err := self.call(ctx, &page, "state_getKeysPaged", args...); err != nil {
			return nil, err
		}
		keyHexs = append(keyHexs, page...)
		if len(page) < nativeKeysPage {
			break
		}
	}
	keyHotkeys := map[string][32]byte{}
	for _, keyHex := range keyHexs {
		key, err := codec.HexDecodeString(keyHex)
		if err != nil {
			return nil, err
		}
		if len(key) < len(prefix)+32 || !bytes.HasPrefix(key, prefix) {
			return nil, fmt.Errorf("commitment key %s is outside the netuid %d prefix", keyHex, netuid)
		}
		hotkey := [32]byte(key[len(key)-32:])
		expected, err := self.storageKey("CommitmentOf", netuid, hotkey)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(expected, key) {
			return nil, fmt.Errorf("commitment key %s does not end in its hotkey", keyHex)
		}
		keyHotkeys[strings.ToLower(codec.HexEncodeToString(key))] = hotkey
	}

	commitments := []rawCommitment{}
	for start := 0; start < len(keyHexs); start += nativeValuesBatch {
		end := min(start+nativeValuesBatch, len(keyHexs))
		var changeSets []struct {
			Block   string       `json:"block"`
			Changes [][2]*string `json:"changes"`
		}
		if err := self.call(ctx, &changeSets, "state_queryStorageAt", keyHexs[start:end], at); err != nil {
			return nil, err
		}
		if len(changeSets) != 1 || !strings.EqualFold(changeSets[0].Block, at) {
			return nil, fmt.Errorf("state_queryStorageAt returned %d change sets or another block", len(changeSets))
		}
		if len(changeSets[0].Changes) != end-start {
			return nil, fmt.Errorf("state_queryStorageAt returned %d values, want %d", len(changeSets[0].Changes), end-start)
		}
		for _, change := range changeSets[0].Changes {
			if change[0] == nil {
				return nil, errors.New("state_queryStorageAt returned a change without a key")
			}
			hotkey, ok := keyHotkeys[strings.ToLower(*change[0])]
			if !ok {
				return nil, fmt.Errorf("state_queryStorageAt returned unrequested key %s", *change[0])
			}
			delete(keyHotkeys, strings.ToLower(*change[0]))
			if change[1] == nil {
				// Removed between the key listing and the read: absent at this block.
				continue
			}
			value, err := codec.HexDecodeString(*change[1])
			if err != nil {
				return nil, err
			}
			commitments = append(commitments, rawCommitment{hotkey: hotkey, raw: value})
		}
	}
	return commitments, nil
}

// The storage prefix of CommitmentOf(netuid, *): the pallet and item hashes,
// then the first hasher's output for the netuid.
func (self *substrateReader) commitmentPrefix(netuid uint16) ([]byte, error) {
	entry, err := self.chain.Meta.FindStorageEntryMetadata(crv4.CommitmentsPalletName, "CommitmentOf")
	if err != nil {
		return nil, err
	}
	hashers, err := entry.Hashers()
	if err != nil {
		return nil, err
	}
	if len(hashers) != 2 {
		return nil, fmt.Errorf("CommitmentOf has %d hashers, want 2", len(hashers))
	}
	full, err := self.storageKey("CommitmentOf", netuid, [32]byte{1})
	if err != nil {
		return nil, err
	}
	if _, err := hashers[0].Write(encodeNetuid(netuid)); err != nil {
		return nil, err
	}
	prefix := append(append([]byte{}, full[:32]...), hashers[0].Sum(nil)...)
	if !bytes.HasPrefix(full, prefix) {
		return nil, errors.New("CommitmentOf netuid prefix does not prefix its keys")
	}
	return prefix, nil
}

func encodeNetuid(netuid uint16) []byte {
	var encoded [2]byte
	binary.LittleEndian.PutUint16(encoded[:], netuid)
	return encoded[:]
}
