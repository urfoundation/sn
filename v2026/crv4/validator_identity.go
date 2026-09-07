// Historical validator observations bind registration in both directions,
// ownership, alpha stake and permit to one caller-selected finalized block.
// They are inputs to activation verification, not proof of activation inclusion.
package crv4

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// The caller supplies its independent chain/block selection and census bound.
// The bound applies before a permit vector is decoded; it is not a UID filter.
type ValidatorIdentityQuery struct {
	GenesisHash       types.Hash
	BlockHash         types.Hash
	BlockNumber       uint64
	Netuid            uint16
	UID               uint16
	MaximumSubnetUIDs uint32
}

// Owns only value fields. A false permit or zero stake remains an observation,
// never an eligible-validator verdict. Finality is the queried RPC's assertion;
// independent-provider agreement and activation signatures are separate checks.
type ValidatorIdentityObservation struct {
	GenesisHash         types.Hash
	BlockHash           types.Hash
	BlockNumber         uint64
	FinalizedHash       types.Hash
	FinalizedNumber     uint64
	Netuid              uint16
	UID                 uint16
	SubnetUIDs          uint16
	Hotkey              [32]byte
	Coldkey             [32]byte
	StakeAlphaRao       uint64
	ValidatorPermit     bool
	Runtime             RuntimeArtifactIdentity
}

// Reads only from authenticated metadata for the requested historical block;
// it neither reads nor rebinds Chain.Meta/Runtime. Calls are safe concurrently
// on an otherwise immutable Chain. RPC transport response limits remain the
// transport's responsibility; bounded storage decoding adds no unbounded copy.
func ReadValidatorIdentityAtContext(ctx context.Context, chain *Chain, query ValidatorIdentityQuery, allowed ...RuntimeArtifactIdentity) (ValidatorIdentityObservation, error) {
	empty := ValidatorIdentityObservation{}
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil {
		return empty, errors.New("validator identity context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if query.GenesisHash == (types.Hash{}) || chain.GenesisHash != query.GenesisHash || query.BlockHash == (types.Hash{}) ||
		query.BlockNumber == 0 || query.BlockNumber > math.MaxUint32 || query.Netuid == 0 ||
		query.MaximumSubnetUIDs == 0 || query.MaximumSubnetUIDs > math.MaxUint16 || uint32(query.UID) >= query.MaximumSubnetUIDs {
		return empty, errors.New("validator identity chain, block or census bounds are invalid")
	}
	// Admit caller policy before the first RPC, including duplicate versions.
	if len(allowed) == 0 || len(allowed) > maximumRuntimeMetadataArtifactsPerChain {
		return empty, errors.New("validator identity runtime allowlist is invalid")
	}
	versions := map[RuntimeVersionIdentity]bool{}
	for _, identity := range allowed {
		if _, err := canonicalRuntimeArtifactIdentity(identity); err != nil {
			return empty, err
		}
		if versions[identity.Version] {
			return empty, errors.New("validator identity runtime allowlist duplicates a version")
		}
		versions[identity.Version] = true
	}
	genesis, err := validatorIdentityBlockHashAtContext(ctx, chain, 0)
	if err != nil {
		return empty, err
	}
	if genesis != query.GenesisHash {
		return empty, errors.New("validator identity RPC genesis differs from the independent pin")
	}
	canonical, err := validatorIdentityBlockHashAtContext(ctx, chain, query.BlockNumber)
	if err != nil {
		return empty, err
	}
	if canonical != query.BlockHash {
		return empty, errors.New("validator identity block is not canonical at its pinned height")
	}
	header, err := chain.HeaderAtContext(ctx, query.BlockHash)
	if err != nil {
		return empty, err
	}
	if uint64(header.Number) != query.BlockNumber {
		return empty, errors.New("validator identity header number differs from the independent pin")
	}
	finalized, err := FinalizedHeadContext(ctx, chain)
	if err != nil {
		return empty, err
	}
	finalizedHeader, err := chain.HeaderAtContext(ctx, finalized)
	if err != nil {
		return empty, err
	}
	if uint64(finalizedHeader.Number) < query.BlockNumber {
		return empty, errors.New("validator identity block is not finalized")
	}
	finalizedCanonical, err := validatorIdentityBlockHashAtContext(ctx, chain, uint64(finalizedHeader.Number))
	if err != nil {
		return empty, err
	}
	if finalizedCanonical != finalized {
		return empty, errors.New("validator identity finalized head is not canonical")
	}
	artifact, err := AuthenticateRuntimeArtifactAtContext(ctx, chain, query.BlockHash, allowed...)
	if err != nil {
		return empty, err
	}
	read := func(name string, maximum int, optional bool, args ...[]byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key, err := types.CreateStorageKey(artifact.Metadata, PalletName, name, args...)
		if err != nil {
			return nil, fmt.Errorf("validator identity %s key: %w", name, err)
		}
		var raw json.RawMessage
		if err := chain.API.Client.CallContext(ctx, &raw, "state_getStorage", key.Hex(), query.BlockHash.Hex()); err != nil {
			return nil, fmt.Errorf("validator identity %s read: %w", name, err)
		}
		value, err := decodeValidatorIdentityHexResult(raw, maximum, optional)
		if err != nil {
			return nil, fmt.Errorf("validator identity %s: %w", name, err)
		}
		return value, nil
	}
	netuid := encodeNetuid(query.Netuid)
	census, err := read("SubnetworkN", 2, false, netuid)
	if err != nil {
		return empty, err
	}
	if len(census) != 2 {
		return empty, errors.New("validator identity SubnetworkN is not an exact u16")
	}
	count := binary.LittleEndian.Uint16(census)
	if count == 0 || uint32(count) > query.MaximumSubnetUIDs || query.UID >= count {
		return empty, errors.New("validator identity subnet census or UID exceeds its bound")
	}
	hotkey, err := read("Keys", 32, false, netuid, encodeNetuid(query.UID))
	if err != nil {
		return empty, err
	}
	if len(hotkey) != 32 {
		return empty, errors.New("validator identity hotkey is not an exact AccountId32")
	}
	uid, err := read("Uids", 2, false, netuid, hotkey)
	if err != nil {
		return empty, err
	}
	if len(uid) != 2 || binary.LittleEndian.Uint16(uid) != query.UID {
		return empty, errors.New("validator identity reverse hotkey/UID registration differs")
	}
	coldkey, err := read("Owner", 32, false, hotkey)
	if err != nil {
		return empty, err
	}
	if len(coldkey) != 32 {
		return empty, errors.New("validator identity coldkey is not an exact AccountId32")
	}
	// TotalHotkeyAlpha is ValueQuery with DefaultZeroAlpha in the pinned runtime.
	// Missing registration/owner/permit data does not have this zero fallback.
	stake, err := read("TotalHotkeyAlpha", 8, true, hotkey, netuid)
	if err != nil {
		return empty, err
	}
	var stakeAlphaRao uint64
	if stake != nil {
		if len(stake) != 8 {
			return empty, errors.New("validator identity alpha stake is not an exact u64")
		}
		stakeAlphaRao = binary.LittleEndian.Uint64(stake)
	}
	permits, err := read("ValidatorPermit", int(count)+4, false, netuid)
	if err != nil {
		return empty, err
	}
	permit, err := decodeValidatorIdentityPermit(permits, count, query.UID)
	if err != nil {
		return empty, err
	}
	// A provider switch or inconsistent canonical view cannot publish a mixed
	// observation. Do not reconstruct native hashes from Ethereum/header fields.
	canonical, err = validatorIdentityBlockHashAtContext(ctx, chain, query.BlockNumber)
	if err != nil {
		return empty, err
	}
	if canonical != query.BlockHash {
		return empty, errors.New("validator identity canonical block changed during observation")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	observation := ValidatorIdentityObservation{
		GenesisHash: query.GenesisHash, BlockHash: query.BlockHash, BlockNumber: query.BlockNumber,
		FinalizedHash: finalized, FinalizedNumber: uint64(finalizedHeader.Number),
		Netuid: query.Netuid, UID: query.UID, SubnetUIDs: count,
		StakeAlphaRao: stakeAlphaRao, ValidatorPermit: permit,
		Runtime: RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash},
	}
	copy(observation.Hotkey[:], hotkey)
	copy(observation.Coldkey[:], coldkey)
	return observation, nil
}

// Checks exact native RPC identity without the synthetic-header hashing path.
func validatorIdentityBlockHashAtContext(ctx context.Context, chain *Chain, number uint64) (types.Hash, error) {
	if err := ctx.Err(); err != nil {
		return types.Hash{}, err
	}
	var raw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &raw, "chain_getBlockHash", number); err != nil {
		return types.Hash{}, fmt.Errorf("validator identity block %d: %w", number, err)
	}
	decoded, err := decodeValidatorIdentityHexResult(raw, 32, false)
	if err != nil {
		return types.Hash{}, fmt.Errorf("validator identity block hash: %w", err)
	}
	if len(decoded) != 32 {
		return types.Hash{}, errors.New("validator identity block hash is not exactly 32 bytes")
	}
	var hash types.Hash
	copy(hash[:], decoded)
	if hash == (types.Hash{}) {
		return types.Hash{}, errors.New("validator identity block hash is zero")
	}
	return hash, nil
}

// Admits the bounded canonical JSON hex representation before string/hex
// allocation. null is accepted only for explicitly optional runtime defaults.
func decodeValidatorIdentityHexResult(raw json.RawMessage, maximum int, optional bool) ([]byte, error) {
	if maximum <= 0 || maximum > math.MaxUint16+4 {
		return nil, errors.New("validator identity storage byte bound is invalid")
	}
	if optional && bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if len(raw) < 4 || len(raw) > maximum*2+4 || raw[0] != '"' || raw[len(raw)-1] != '"' || raw[1] != '0' || raw[2] != 'x' {
		return nil, errors.New("validator identity storage is missing, malformed or exceeds its byte bound")
	}
	// Requiring raw hex also rejects JSON escapes before decoder allocation.
	encoded := raw[3 : len(raw)-1]
	if len(encoded)%2 != 0 {
		return nil, errors.New("validator identity storage has odd hex length")
	}
	for _, character := range encoded {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return nil, errors.New("validator identity storage has non-hex bytes")
		}
	}
	decoded := make([]byte, len(encoded)/2)
	if _, err := hex.Decode(decoded, encoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// Checks the entire canonical SCALE Vec<bool> without allocating a slice from
// an untrusted compact length. A malformed other UID invalidates the vector.
func decodeValidatorIdentityPermit(raw []byte, count, uid uint16) (bool, error) {
	if count == 0 || uid >= count || len(raw) == 0 {
		return false, errors.New("validator identity permit census is invalid")
	}
	prefix := appendCompact(nil, uint64(count))
	if len(raw) != len(prefix)+int(count) || !bytes.Equal(raw[:min(len(raw), len(prefix))], prefix) {
		return false, errors.New("validator identity permit vector does not exactly match subnet census")
	}
	for _, value := range raw[len(prefix):] {
		if value > 1 {
			return false, errors.New("validator identity permit vector contains a non-boolean value")
		}
	}
	return raw[len(prefix)+int(uid)] == 1, nil
}
